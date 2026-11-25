package update

import (
	"regexp"
	"strconv"
	"strings"
)

// tagPattern is what a release tag can be: a version, or the rolling build.
var tagPattern = regexp.MustCompile(`^(latest|v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?)$`)

var semverPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z][0-9A-Za-z.-]*))?$`)

// semver is a parsed X.Y.Z[-pre] version.
type semver struct {
	major, minor, patch int
	pre                 string
}

func parseSemver(text string) (semver, bool) {
	m := semverPattern.FindStringSubmatch(text)
	if m == nil {
		return semver{}, false
	}
	var v semver
	var err error
	if v.major, err = strconv.Atoi(m[1]); err != nil {
		return semver{}, false
	}
	if v.minor, err = strconv.Atoi(m[2]); err != nil {
		return semver{}, false
	}
	if v.patch, err = strconv.Atoi(m[3]); err != nil {
		return semver{}, false
	}
	v.pre = m[4]
	return v, true
}

// compare returns -1, 0 or 1. A release is newer than its own pre-release.
func (v semver) compare(o semver) int {
	for _, pair := range [][2]int{{v.major, o.major}, {v.minor, o.minor}, {v.patch, o.patch}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.pre == o.pre:
		return 0
	case v.pre == "":
		return 1
	case o.pre == "":
		return -1
	}
	return comparePre(v.pre, o.pre)
}

// comparePre orders two pre-release suffixes identifier by identifier: numbers
// by value, and before words; words alphabetically; the shorter list first.
func comparePre(a string, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aErr := strconv.Atoi(as[i])
		bn, bErr := strconv.Atoi(bs[i])
		switch {
		case aErr == nil && bErr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}
