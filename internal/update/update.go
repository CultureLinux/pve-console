// Package update finds out whether a newer pve-console is published on GitHub and
// installs it: it downloads the binary of the release, checks its SHA-256 sum,
// checks that it runs, and replaces the running executable.
//
// The release that matches the running binary is followed: a versioned binary
// follows the stable releases, a binary of the rolling build follows that build.
// Any other binary (a local or a CI build) never updates itself.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pvc/internal/version"
)

// Asset is the name of the binary in a release.
const Asset = "pve-console_linux_amd64"

// Errors a caller treats on its own: they say why nothing was checked.
var (
	// ErrDev: this binary is a local or a CI build; it does not look for updates.
	ErrDev = errors.New("this is a development build: it does not look for updates")
	// ErrDisabled: the user turned the update check off.
	ErrDisabled = errors.New("the update check is turned off (--no-update-check or PVE_CONSOLE_NO_UPDATE_CHECK)")
	// ErrUnsupported: releases are only built for linux/amd64.
	ErrUnsupported = errors.New("self-update is only available on linux/amd64")
)

const (
	checkTimeout   = 10 * time.Second
	versionTimeout = 10 * time.Second
	maxSumsSize    = 1 << 20
	maxNotesLines  = 40
	maxNotesBytes  = 4000
)

// Build identifies the running binary, as injected at link time.
type Build struct {
	Version string
	Commit  string
}

// Kind is the kind of build: stable, edge or dev.
func (b Build) Kind() version.Kind { return version.Classify(b.Version, b.Commit) }

// Display is the short text shown for the build.
func (b Build) Display() string { return version.DisplayOf(b.Version, b.Commit) }

// Info is the answer of a check.
type Info struct {
	// Kind is the channel that was checked: stable or edge.
	Kind version.Kind
	// Current and Latest are short texts: "v0.2.0", "edge 3f9a2c1".
	Current string
	Latest  string
	// Tag is the release to download: "v0.2.1" or "latest".
	Tag string
	// Available is true when Latest is newer than the running build.
	Available bool
	// Notes are the release notes of a stable release, cut to a few lines.
	Notes string
	// URL is the page of the release.
	URL string
}

// Result describes an installation.
type Result struct {
	// Path is the executable that was replaced.
	Path string
	// Previous is where the old executable was kept; empty if it could not be.
	Previous string
	// Version is what the new binary says on `version`.
	Version string
}

// Client talks to GitHub. The zero value is not usable: use NewClient.
type Client struct {
	// Repo is "owner/name".
	Repo string
	// APIBase is the GitHub API, DownloadBase the site that serves release files.
	APIBase      string
	DownloadBase string
	HTTP         *http.Client
	// GOOS and GOARCH are the platform of the binary to install.
	GOOS, GOARCH string
	// MaxBinarySize bounds a download, in bytes.
	MaxBinarySize int64
}

// NewClient returns a client for the public repository.
func NewClient(goos string, goarch string) Client {
	c := Client{
		Repo:          "CultureLinux/pve-console",
		APIBase:       "https://api.github.com",
		DownloadBase:  "https://github.com",
		GOOS:          goos,
		GOARCH:        goarch,
		MaxBinarySize: 100 << 20,
	}
	c.HTTP = &http.Client{Timeout: 5 * time.Minute, CheckRedirect: redirectPolicy}
	return c
}

// secureURL accepts https, and http only toward the local machine (tests).
func secureURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", raw, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("refusing to use %s: only https is accepted", raw)
}

func redirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	return secureURL(req.URL.String())
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute, CheckRedirect: redirectPolicy}
}

func (c Client) get(ctx context.Context, rawURL string, accept string) (*http.Response, error) {
	if err := secureURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	// Nothing about the user or the node is sent: the program and its version only.
	req.Header.Set("User-Agent", "pve-console/"+version.Version)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to reach GitHub: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusNotFound:
			return nil, fmt.Errorf("not found on GitHub (%s)", resp.Status)
		case http.StatusForbidden, http.StatusTooManyRequests:
			return nil, fmt.Errorf("GitHub refused the request (%s): the rate limit may be reached, try again later", resp.Status)
		}
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	return resp, nil
}

func (c Client) getJSON(ctx context.Context, rawURL string, out any) error {
	resp, err := c.get(ctx, rawURL, "application/vnd.github+json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("unreadable answer from GitHub: %w", err)
	}
	return nil
}

// release is the part of a GitHub release this package reads.
type release struct {
	TagName         string `json:"tag_name"`
	Body            string `json:"body"`
	HTMLURL         string `json:"html_url"`
	TargetCommitish string `json:"target_commitish"`
}

var shaPattern = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

// Check asks GitHub for the newest build of the channel of b.
func (c Client) Check(ctx context.Context, b Build) (Info, error) {
	kind := b.Kind()
	if kind == version.KindDev {
		return Info{}, ErrDev
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	info := Info{Kind: kind, Current: b.Display()}
	switch kind {
	case version.KindStable:
		var rel release
		if err := c.getJSON(ctx, c.APIBase+"/repos/"+c.Repo+"/releases/latest", &rel); err != nil {
			return Info{}, fmt.Errorf("unable to read the latest release: %w", err)
		}
		latest, ok := parseSemver(rel.TagName)
		if !ok || !tagPattern.MatchString(rel.TagName) {
			return Info{}, fmt.Errorf("the latest release is named %q, which is not a version", rel.TagName)
		}
		current, _ := parseSemver(b.Version)
		info.Latest, info.Tag, info.URL = rel.TagName, rel.TagName, rel.HTMLURL
		info.Available = latest.compare(current) > 0
		info.Notes = trimNotes(rel.Body)
	case version.KindEdge:
		var rel release
		if err := c.getJSON(ctx, c.APIBase+"/repos/"+c.Repo+"/releases/tags/latest", &rel); err != nil {
			return Info{}, fmt.Errorf("unable to read the latest build: %w", err)
		}
		// The workflow writes the commit in the notes; the target is the fallback.
		commit := shaPattern.FindString(rel.Body)
		if commit == "" && shaPattern.MatchString(rel.TargetCommitish) {
			commit = rel.TargetCommitish
		}
		if commit == "" {
			return Info{}, errors.New("the latest build does not say which commit it is: unable to compare")
		}
		info.Latest, info.Tag, info.URL = version.DisplayOf("latest", commit), "latest", rel.HTMLURL
		info.Available = !sameCommit(commit, b.Commit)
	}
	return info, nil
}

// sameCommit compares two commit ids, one of which may be shortened.
func sameCommit(a string, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// trimNotes keeps the first lines of the notes of a release.
func trimNotes(body string) string {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	lines := strings.Split(body, "\n")
	if len(lines) > maxNotesLines {
		lines = append(lines[:maxNotesLines], "…")
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxNotesBytes {
		out = out[:maxNotesBytes] + "…"
	}
	return out
}

// Install downloads the release of info, checks it, and replaces the executable
// at exe. Nothing is changed unless every check passed; the old executable is
// kept next to it as <exe>.prev. progress, if set, is told each step.
func (c Client) Install(ctx context.Context, info Info, exe string, progress func(string)) (Result, error) {
	say := func(text string) {
		if progress != nil {
			progress(text)
		}
	}
	if c.GOOS != "linux" || c.GOARCH != "amd64" {
		return Result{}, ErrUnsupported
	}
	if !tagPattern.MatchString(info.Tag) {
		return Result{}, fmt.Errorf("refusing to download the release %q: not a version", info.Tag)
	}

	// Resolve links first: it is the real file that is replaced.
	path, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return Result{}, fmt.Errorf("unable to find the running program %s: %w", exe, err)
	}
	// The new file lives next to the old one, so that the final rename is atomic. The
	// write permission is checked before anything is downloaded.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pve-console-update-*")
	if err != nil {
		return Result{}, fmt.Errorf("unable to write in %s (run as root, or install elsewhere): %w", filepath.Dir(path), err)
	}
	tmpPath := tmp.Name()
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	base := c.DownloadBase + "/" + c.Repo + "/releases/download/" + info.Tag
	say("downloading " + Asset + " (" + info.Tag + ")")
	sum, err := c.download(ctx, base+"/"+Asset, tmp)
	if err != nil {
		return Result{}, err
	}
	if err := tmp.Close(); err != nil {
		return Result{}, fmt.Errorf("unable to write %s: %w", tmpPath, err)
	}

	say("checking the SHA-256 sum")
	sums, err := c.fetchText(ctx, base+"/SHA256SUMS")
	if err != nil {
		return Result{}, fmt.Errorf("unable to read SHA256SUMS (a binary is never installed without its sum): %w", err)
	}
	want, ok := findSum(sums, Asset)
	if !ok {
		return Result{}, fmt.Errorf("%s is not listed in SHA256SUMS", Asset)
	}
	if !strings.EqualFold(want, sum) {
		return Result{}, fmt.Errorf("checksum mismatch for %s (expected %s, got %s): nothing installed", Asset, want, sum)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return Result{}, fmt.Errorf("unable to make %s executable: %w", tmpPath, err)
	}
	say("checking that the new binary runs")
	said, err := runVersion(ctx, tmpPath)
	if err != nil {
		return Result{}, err
	}

	say("installing")
	previous := path + ".prev"
	if err := os.Remove(previous); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("unable to replace the old backup %s: %w", previous, err)
	}
	// A hard link keeps the old file whole, at no cost, whatever happens next.
	if err := os.Link(path, previous); err != nil {
		previous = ""
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return Result{}, fmt.Errorf("unable to replace %s: %w", path, err)
	}
	done = true
	return Result{Path: path, Previous: previous, Version: said}, nil
}

// download copies a file to w and returns its SHA-256 sum in hex.
func (c Client) download(ctx context.Context, rawURL string, w io.Writer) (string, error) {
	resp, err := c.get(ctx, rawURL, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("unable to download %s: %w", filepath.Base(rawURL), err)
	}
	defer resp.Body.Close()

	limit := c.MaxBinarySize
	if limit <= 0 {
		limit = 100 << 20
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, hash), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", fmt.Errorf("download of %s interrupted: %w", filepath.Base(rawURL), err)
	}
	if n > limit {
		return "", fmt.Errorf("%s is larger than %d bytes: refusing it", filepath.Base(rawURL), limit)
	}
	if n == 0 {
		return "", fmt.Errorf("%s is empty", filepath.Base(rawURL))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (c Client) fetchText(ctx context.Context, rawURL string) (string, error) {
	resp, err := c.get(ctx, rawURL, "text/plain")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSumsSize))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// findSum reads a SHA256SUMS file ("<sum>  <name>" per line) and returns the sum of name.
func findSum(sums string, name string) (string, bool) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], true
		}
	}
	return "", false
}

// runVersion runs `<path> version` and checks that it answers like pve-console.
func runVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return "", fmt.Errorf("the downloaded binary does not run on this machine: %w", err)
	}
	said := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if !strings.HasPrefix(said, version.Name+" ") {
		return "", fmt.Errorf("the downloaded binary does not look like pve-console (it answers %q)", said)
	}
	return said, nil
}

// Updater is what the terminal UI uses: a client, the running build and the
// path of the running program.
type Updater struct {
	Client Client
	Build  Build
	Exe    string
	// Disabled turns the check off: Check answers ErrDisabled without any request.
	Disabled bool
}

// Check implements the UI's updater.
func (u Updater) Check(ctx context.Context) (Info, error) {
	if u.Disabled {
		return Info{}, ErrDisabled
	}
	return u.Client.Check(ctx, u.Build)
}

// Install implements the UI's updater.
func (u Updater) Install(ctx context.Context, info Info, progress func(string)) (Result, error) {
	if u.Disabled {
		return Result{}, ErrDisabled
	}
	return u.Client.Install(ctx, info, u.Exe, progress)
}
