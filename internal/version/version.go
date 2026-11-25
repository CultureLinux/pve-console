package version

import (
	"fmt"
	"regexp"
)

var (
	Name    = "pve-console"
	Version = "0.1.0"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	if Commit == "none" && Date == "unknown" {
		return fmt.Sprintf("%s %s", Name, Version)
	}

	return fmt.Sprintf("%s %s (%s, %s)", Name, Version, Commit, Date)
}

// Kind says what sort of build a binary is.
type Kind string

const (
	// KindStable is a versioned release: Version is X.Y.Z (maybe with a suffix) and the
	// commit is known.
	KindStable Kind = "stable"
	// KindEdge is the rolling build of main: Version is "latest" and the commit is known.
	KindEdge Kind = "edge"
	// KindDev is every other build (a local build, a CI build): it does not look for updates.
	KindDev Kind = "dev"
)

// semver matches X.Y.Z with an optional pre-release suffix, without the v.
var semver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

// Classify tells the kind of build from its version and commit, as injected by
// the linker.
func Classify(version string, commit string) Kind {
	if commit == "" || commit == "none" {
		return KindDev
	}
	switch {
	case version == "latest":
		return KindEdge
	case semver.MatchString(version):
		return KindStable
	}
	return KindDev
}

// DisplayOf is the short text shown for a build: "v0.2.0", "edge 3f9a2c1" or "dev".
func DisplayOf(version string, commit string) string {
	switch Classify(version, commit) {
	case KindStable:
		return "v" + version
	case KindEdge:
		if len(commit) > 7 {
			commit = commit[:7]
		}
		return "edge " + commit
	}
	return "dev"
}

// Display is DisplayOf for this binary.
func Display() string {
	return DisplayOf(Version, Commit)
}
