package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"pvc/internal/version"
)

const (
	fullSHA  = "3f9a2c1d5e7b9a0c2d4e6f8a1b3c5d7e9f0a1b2c"
	otherSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// fakeGitHub serves what the client asks GitHub for. Every request is counted.
type fakeGitHub struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []string
	// JSON answers by API path, files by download path.
	api   map[string]string
	files map[string]string
	// status overrides the answer of a path.
	status map[string]int
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, api: map[string]string{}, files: map[string]string{}, status: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.Path)
		f.mu.Unlock()
		if code, ok := f.status[r.URL.Path]; ok {
			http.Error(w, "forced", code)
			return
		}
		if body, ok := f.api[r.URL.Path]; ok {
			if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "pve-console/") {
				t.Errorf("User-Agent = %q, want pve-console/<version> and nothing else", got)
			}
			fmt.Fprint(w, body)
			return
		}
		if body, ok := f.files[r.URL.Path]; ok {
			fmt.Fprint(w, body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) client() Client {
	c := NewClient("linux", "amd64")
	c.APIBase = f.server.URL
	c.DownloadBase = f.server.URL
	return c
}

func (f *fakeGitHub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

const (
	apiLatest = "/repos/CultureLinux/pve-console/releases/latest"
	apiEdge   = "/repos/CultureLinux/pve-console/releases/tags/latest"
)

func releaseJSON(tag string, body string) string {
	return fmt.Sprintf(`{"tag_name":%q,"body":%q,"html_url":"https://example.test/r/%s","target_commitish":"main"}`, tag, body, tag)
}

func sumOf(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// script is a stand-in for the binary: it answers `version`.
func script(said string) string {
	return "#!/bin/sh\n[ \"$1\" = \"version\" ] && echo '" + said + "'\n"
}

func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the fake binaries are shell scripts")
	}
}

func stable(v string) Build { return Build{Version: v, Commit: fullSHA} }
func edge() Build           { return Build{Version: "latest", Commit: fullSHA} }

func TestCheckStable(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		tag       string
		available bool
	}{
		{"newer release", "0.2.0", "v0.2.1", true},
		{"newer minor", "0.2.9", "v0.3.0", true},
		{"numeric, not alphabetical", "0.9.0", "v0.10.0", true},
		{"same", "0.2.0", "v0.2.0", false},
		{"running a newer one", "0.3.0", "v0.2.0", false},
		{"release after its candidate", "0.3.0-rc1", "v0.3.0", true},
		{"candidate after the release", "0.3.0", "v0.3.0-rc1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := newFakeGitHub(t)
			gh.api[apiLatest] = releaseJSON(tt.tag, "### Features\n\n- Menu Log\r\n")
			info, err := gh.client().Check(context.Background(), stable(tt.current))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Available != tt.available || info.Latest != tt.tag || info.Tag != tt.tag || info.Kind != version.KindStable {
				t.Fatalf("info = %+v", info)
			}
			if info.Current != "v"+tt.current {
				t.Fatalf("Current = %q", info.Current)
			}
			if !strings.Contains(info.Notes, "Menu Log") || strings.Contains(info.Notes, "\r") || info.URL == "" {
				t.Fatalf("notes and page: %+v", info)
			}
		})
	}
}

func TestCheckStableErrors(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*fakeGitHub)
		want    string
	}{
		{"no release yet", func(g *fakeGitHub) {}, "not found"},
		{"rate limit", func(g *fakeGitHub) { g.status[apiLatest] = 403 }, "rate limit"},
		{"server error", func(g *fakeGitHub) { g.status[apiLatest] = 500 }, "500"},
		{"not json", func(g *fakeGitHub) { g.api[apiLatest] = "<html>" }, "unreadable"},
		{"tag that is not a version", func(g *fakeGitHub) { g.api[apiLatest] = releaseJSON("nightly", "") }, "not a version"},
		{"tag that tries to escape", func(g *fakeGitHub) { g.api[apiLatest] = releaseJSON("v1.0.0/../../x", "") }, "not a version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := newFakeGitHub(t)
			tt.prepare(gh)
			_, err := gh.client().Check(context.Background(), stable("0.2.0"))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestCheckEdge(t *testing.T) {
	notes := func(sha string) string {
		return "Automated linux/amd64 build of main (" + sha + "). This is not a versioned release."
	}
	tests := []struct {
		name      string
		build     Build
		body      string
		target    string
		available bool
		latest    string
	}{
		{"another commit", Build{"latest", fullSHA}, notes(otherSHA), "main", true, "edge aaaaaaa"},
		{"the same commit", Build{"latest", fullSHA}, notes(fullSHA), "main", false, "edge 3f9a2c1"},
		{"a shortened commit", Build{"latest", fullSHA[:7]}, notes(fullSHA), "main", false, "edge 3f9a2c1"},
		{"commit only in the target", Build{"latest", fullSHA}, "no sha here", otherSHA, true, "edge aaaaaaa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := newFakeGitHub(t)
			gh.api[apiEdge] = fmt.Sprintf(`{"tag_name":"latest","body":%q,"html_url":"x","target_commitish":%q}`, tt.body, tt.target)
			info, err := gh.client().Check(context.Background(), tt.build)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Available != tt.available || info.Latest != tt.latest || info.Tag != "latest" || info.Kind != version.KindEdge {
				t.Fatalf("info = %+v", info)
			}
		})
	}

	// Never a false « up to date »: without a commit, it is an error.
	gh := newFakeGitHub(t)
	gh.api[apiEdge] = `{"tag_name":"latest","body":"no commit","target_commitish":"main"}`
	if _, err := gh.client().Check(context.Background(), edge()); err == nil || !strings.Contains(err.Error(), "which commit") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckDevBuildMakesNoRequest(t *testing.T) {
	gh := newFakeGitHub(t)
	for _, b := range []Build{{"0.1.0", "none"}, {"ab12cd3", "none"}, {"latest", "none"}} {
		if _, err := gh.client().Check(context.Background(), b); !errors.Is(err, ErrDev) {
			t.Fatalf("%+v: error = %v, want ErrDev", b, err)
		}
	}
	if gh.count() != 0 {
		t.Fatalf("a dev build must not contact anything: %v", gh.requests)
	}
}

func TestUpdaterDisabledMakesNoRequest(t *testing.T) {
	gh := newFakeGitHub(t)
	u := Updater{Client: gh.client(), Build: stable("0.2.0"), Exe: "/nowhere", Disabled: true}
	if _, err := u.Check(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Check: %v", err)
	}
	if _, err := u.Install(context.Background(), Info{Tag: "v0.2.1"}, nil); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Install: %v", err)
	}
	if gh.count() != 0 {
		t.Fatal("a disabled updater must not contact anything")
	}
}

func TestOnlyHTTPSIsAcceptedOutsideTheLocalMachine(t *testing.T) {
	c := NewClient("linux", "amd64")
	c.APIBase = "http://example.com"
	if _, err := c.Check(context.Background(), stable("0.2.0")); err == nil || !strings.Contains(err.Error(), "only https") {
		t.Fatalf("error = %v", err)
	}
	for _, ok := range []string{"https://api.github.com", "http://127.0.0.1:8080", "http://localhost:1", "http://[::1]:9"} {
		if err := secureURL(ok); err != nil {
			t.Errorf("%s should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://example.com", "ftp://127.0.0.1/x", "file:///etc/passwd", "http://127.0.0.1.evil.test/"} {
		if err := secureURL(bad); err == nil {
			t.Errorf("%s should be refused", bad)
		}
	}
}

func TestRedirectToHTTPIsRefused(t *testing.T) {
	// A server that sends the client to a plain http address that is not local.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/evil", http.StatusFound)
	}))
	defer redirector.Close()
	c := NewClient("linux", "amd64")
	c.APIBase = redirector.URL
	if _, err := c.Check(context.Background(), stable("0.2.0")); err == nil || !strings.Contains(err.Error(), "only https") {
		t.Fatalf("error = %v", err)
	}
}

// installFixture is a directory with a running program, and a release to install.
type installFixture struct {
	gh     *fakeGitHub
	dir    string
	exe    string
	oldBin string
	newBin string
}

func newInstallFixture(t *testing.T) installFixture {
	t.Helper()
	requireLinux(t)
	f := installFixture{gh: newFakeGitHub(t), dir: t.TempDir()}
	f.oldBin = script("pve-console 0.2.0 (old, 2026-01-01)")
	f.newBin = script("pve-console 0.2.1 (new, 2026-02-01)")
	f.exe = filepath.Join(f.dir, "pve-console")
	if err := os.WriteFile(f.exe, []byte(f.oldBin), 0o755); err != nil {
		t.Fatal(err)
	}
	base := "/CultureLinux/pve-console/releases/download/v0.2.1/"
	f.gh.files[base+Asset] = f.newBin
	f.gh.files[base+"SHA256SUMS"] = sumOf(f.newBin) + "  " + Asset + "\n"
	return f
}

func (f installFixture) install(progress func(string)) (Result, error) {
	return f.gh.client().Install(context.Background(), Info{Tag: "v0.2.1", Kind: version.KindStable}, f.exe, progress)
}

func (f installFixture) read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// leftovers lists the files of the directory other than the program and its backup.
func (f installFixture) leftovers(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.Name() != "pve-console" && e.Name() != "pve-console.prev" {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestInstallReplacesTheProgramAndKeepsTheOldOne(t *testing.T) {
	f := newInstallFixture(t)
	var steps []string
	res, err := f.install(func(s string) { steps = append(steps, s) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f.read(t, f.exe) != f.newBin {
		t.Fatal("the program must be the new binary")
	}
	if res.Previous != f.exe+".prev" || f.read(t, res.Previous) != f.oldBin {
		t.Fatalf("the old binary is kept whole as %s", res.Previous)
	}
	if res.Path != f.exe || res.Version != "pve-console 0.2.1 (new, 2026-02-01)" {
		t.Fatalf("result = %+v", res)
	}
	if st, _ := os.Stat(f.exe); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
	if left := f.leftovers(t); len(left) != 0 {
		t.Fatalf("no temporary file may stay: %v", left)
	}

	want := []string{"downloading", "checking the SHA-256", "checking that the new binary runs", "installing"}
	if len(steps) != len(want) {
		t.Fatalf("steps = %v", steps)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(steps[i], prefix) {
			t.Errorf("step %d = %q, want %q…", i, steps[i], prefix)
		}
	}
}

func TestInstallReplacesTheOldBackup(t *testing.T) {
	f := newInstallFixture(t)
	if err := os.WriteFile(f.exe+".prev", []byte("a much older binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := f.install(nil)
	if err != nil || f.read(t, res.Previous) != f.oldBin {
		t.Fatalf("the backup is the binary that was running: %v", err)
	}
}

func TestInstallFollowsALink(t *testing.T) {
	f := newInstallFixture(t)
	link := filepath.Join(t.TempDir(), "pve-console")
	if err := os.Symlink(f.exe, link); err != nil {
		t.Skip("symbolic links are not available")
	}
	f.exe = link
	res, err := f.install(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	real, _ := filepath.EvalSymlinks(link)
	if res.Path != real || f.read(t, real) != f.newBin {
		t.Fatalf("it is the real file that is replaced: %+v", res)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link must stay a link")
	}
}

// refused checks that an installation failed and changed nothing.
func (f installFixture) refused(t *testing.T, want string) {
	t.Helper()
	_, err := f.install(nil)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to mention %q", err, want)
	}
	if f.read(t, f.exe) != f.oldBin {
		t.Fatal("the installed program must be untouched")
	}
	if _, err := os.Stat(f.exe + ".prev"); err == nil {
		t.Fatal("no backup is made when nothing is replaced")
	}
	if left := f.leftovers(t); len(left) != 0 {
		t.Fatalf("no temporary file may stay: %v", left)
	}
}

func TestInstallRefusesAWrongChecksum(t *testing.T) {
	f := newInstallFixture(t)
	f.gh.files["/CultureLinux/pve-console/releases/download/v0.2.1/"+Asset] = f.newBin + "echo tampered\n"
	f.refused(t, "checksum mismatch")
}

func TestInstallRefusesWithoutTheSums(t *testing.T) {
	f := newInstallFixture(t)
	delete(f.gh.files, "/CultureLinux/pve-console/releases/download/v0.2.1/SHA256SUMS")
	f.refused(t, "never installed without its sum")
}

func TestInstallRefusesABinaryThatIsNotListed(t *testing.T) {
	f := newInstallFixture(t)
	f.gh.files["/CultureLinux/pve-console/releases/download/v0.2.1/SHA256SUMS"] = sumOf(f.newBin) + "  another-file\n"
	f.refused(t, "not listed")
}

func TestInstallRefusesABinaryThatDoesNotRun(t *testing.T) {
	f := newInstallFixture(t)
	broken := "not a program"
	f.gh.files["/CultureLinux/pve-console/releases/download/v0.2.1/"+Asset] = broken
	f.gh.files["/CultureLinux/pve-console/releases/download/v0.2.1/SHA256SUMS"] = sumOf(broken) + "  " + Asset + "\n"
	f.refused(t, "does not run")
}

func TestInstallRefusesAProgramThatIsNotPveConsole(t *testing.T) {
	f := newInstallFixture(t)
	other := script("something else 1.0")
	f.gh.files["/CultureLinux/pve-console/releases/download/v0.2.1/"+Asset] = other
	f.gh.files["/CultureLinux/pve-console/releases/download/v0.2.1/SHA256SUMS"] = sumOf(other) + "  " + Asset + "\n"
	f.refused(t, "does not look like pve-console")
}

func TestInstallRefusesAMissingRelease(t *testing.T) {
	f := newInstallFixture(t)
	f.gh.status["/CultureLinux/pve-console/releases/download/v0.2.1/"+Asset] = 404
	f.refused(t, "unable to download")
}

func TestInstallRefusesAnHugeDownload(t *testing.T) {
	f := newInstallFixture(t)
	c := f.gh.client()
	c.MaxBinarySize = 10
	_, err := c.Install(context.Background(), Info{Tag: "v0.2.1"}, f.exe, nil)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("error = %v", err)
	}
	if f.read(t, f.exe) != f.oldBin || len(f.leftovers(t)) != 0 {
		t.Fatal("nothing must change")
	}
}

func TestInstallChecksTheTagBeforeAnyRequest(t *testing.T) {
	f := newInstallFixture(t)
	for _, tag := range []string{"", "main", "v1.0.0/../x", "v1.0", "../../etc", "v1.0.0?x=1"} {
		_, err := f.gh.client().Install(context.Background(), Info{Tag: tag}, f.exe, nil)
		if err == nil || !strings.Contains(err.Error(), "not a version") {
			t.Errorf("tag %q: error = %v", tag, err)
		}
	}
	if f.gh.count() != 0 {
		t.Fatalf("no request for a bad tag: %v", f.gh.requests)
	}
}

func TestInstallRefusesOtherPlatformsBeforeAnyRequest(t *testing.T) {
	f := newInstallFixture(t)
	for _, p := range [][2]string{{"windows", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}} {
		c := f.gh.client()
		c.GOOS, c.GOARCH = p[0], p[1]
		if _, err := c.Install(context.Background(), Info{Tag: "v0.2.1"}, f.exe, nil); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%v: error = %v", p, err)
		}
	}
	if f.gh.count() != 0 {
		t.Fatal("no request on an unsupported platform")
	}
}

func TestInstallChecksTheDirectoryBeforeDownloading(t *testing.T) {
	f := newInstallFixture(t)
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	if err := os.Chmod(f.dir, 0o555); err != nil {
		t.Skip("cannot change the permissions")
	}
	t.Cleanup(func() { os.Chmod(f.dir, 0o755) })

	_, err := f.install(nil)
	if err == nil || !strings.Contains(err.Error(), "unable to write in") {
		t.Fatalf("error = %v", err)
	}
	if f.gh.count() != 0 {
		t.Fatalf("nothing is downloaded when the program cannot be replaced: %v", f.gh.requests)
	}
}

func TestInstallOfAMissingProgram(t *testing.T) {
	f := newInstallFixture(t)
	f.exe = filepath.Join(f.dir, "gone")
	if _, err := f.install(nil); err == nil || !strings.Contains(err.Error(), "unable to find the running program") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindSum(t *testing.T) {
	sums := "aaa  other\nbbb *" + Asset + "\ncc\n\n"
	if got, ok := findSum(sums, Asset); !ok || got != "bbb" {
		t.Fatalf("a binary-mode marker is accepted: %q %t", got, ok)
	}
	if _, ok := findSum(sums, "missing"); ok {
		t.Fatal("unknown name")
	}
	if _, ok := findSum("only-one-field\n", "only-one-field"); ok {
		t.Fatal("a malformed line is not a sum")
	}
}

func TestTrimNotes(t *testing.T) {
	many := strings.Repeat("line\n", 100)
	got := trimNotes(many)
	if lines := strings.Count(got, "\n") + 1; lines != maxNotesLines+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("notes are cut to %d lines, got %d", maxNotesLines, lines)
	}
	if got := trimNotes("  \r\n hello \r\n"); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := trimNotes(strings.Repeat("x", 9000)); len(got) > maxNotesBytes+4 {
		t.Fatalf("long notes are cut, got %d bytes", len(got))
	}
}

func TestSemverCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v0.2.0", "0.2.0", 0},
		{"v0.10.0", "v0.9.0", 1},
		{"v1.0.0", "v0.99.99", 1},
		{"v1.0.0-rc1", "v1.0.0", -1},
		{"v1.0.0-rc2", "v1.0.0-rc1", 1},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", 1},
		{"v1.0.0-1", "v1.0.0-alpha", -1},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1},
		{"v1.0.1-rc1", "v1.0.0", 1},
	}
	for _, tt := range tests {
		a, ok1 := parseSemver(tt.a)
		b, ok2 := parseSemver(tt.b)
		if !ok1 || !ok2 {
			t.Fatalf("unparsable: %s %s", tt.a, tt.b)
		}
		if got := a.compare(b); got != tt.want {
			t.Errorf("compare(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		if got := b.compare(a); got != -tt.want {
			t.Errorf("compare(%s, %s) = %d, want %d", tt.b, tt.a, got, -tt.want)
		}
	}
	for _, bad := range []string{"", "v1", "v1.2", "1.2.3.4", "latest", "v1.2.x", "v99999999999999999999.0.0"} {
		if _, ok := parseSemver(bad); ok {
			t.Errorf("%q is not a version", bad)
		}
	}
}
