// Package release finds, downloads and checks eva-md's releases on GitHub,
// and orders their versions. Only eva-md upgrade uses it: no hook, update
// or daily job looks for a release.
package release

import (
	"fmt"
	"regexp"
	"strconv"
)

// tagRe is a release's tag: its commit's UTC day as year.month.day with no
// zero padding, then .2, .3 and so on for more releases the same day.
var tagRe = regexp.MustCompile(`^([0-9]{4})\.([1-9][0-9]?)\.([1-9][0-9]?)(\.[2-9]|\.[1-9][0-9]+)?$`)

// sourceRe is a source build's version: its commit's UTC day, then the
// commit's time and short hash as build metadata, with .dirty for a changed
// tree (see calver in cmd/eva-md).
var sourceRe = regexp.MustCompile(`^([0-9]{4})\.([1-9][0-9]?)\.([1-9][0-9]?)\+[0-9]{4}(\.[0-9a-f]{7}(\.dirty)?)?$`)

// Version is a release's tag or a build's version, as far as upgrade orders
// them.
type Version struct {
	Year, Month, Day int
	// N is the release's number that day: 1 for the first, 2 for .2 and so
	// on, and 0 for a source build.
	N      int
	Source bool // a build from a checkout, not a release
	Dev    bool // a build with no version: it orders before every other
	Text   string
}

// ValidTag reports whether tag is a release tag, such as 2026.11.3 or
// 2026.11.3.2.
func ValidTag(tag string) bool {
	_, err := ParseTag(tag)
	return err == nil
}

// ParseTag reads a release tag.
func ParseTag(tag string) (Version, error) {
	m := tagRe.FindStringSubmatch(tag)
	if m == nil {
		return Version{}, fmt.Errorf("%q isn't a release tag, such as 2026.11.3", tag)
	}
	v, err := day(m[1], m[2], m[3], tag)
	if err != nil {
		return Version{}, err
	}
	v.N = 1
	if m[4] != "" {
		v.N, _ = strconv.Atoi(m[4][1:])
	}
	return v, nil
}

// Parse reads a binary's version: a release tag, a source build's version,
// or anything else, such as dev, as a build with no version.
func Parse(s string) Version {
	if v, err := ParseTag(s); err == nil {
		return v
	}
	if m := sourceRe.FindStringSubmatch(s); m != nil {
		if v, err := day(m[1], m[2], m[3], s); err == nil {
			v.Source = true
			return v
		}
	}
	return Version{Dev: true, Text: s}
}

func day(y, m, d, text string) (Version, error) {
	v := Version{Text: text}
	v.Year, _ = strconv.Atoi(y)
	v.Month, _ = strconv.Atoi(m)
	v.Day, _ = strconv.Atoi(d)
	if v.Month > 12 || v.Day > 31 {
		return Version{}, fmt.Errorf("%q has no such month or day", text)
	}
	return v, nil
}

// Compare orders a and b by year, month, day and the number that day: -1
// when a is older, 0 when they are the same, 1 when a is newer. A build with
// no version is older than every other.
func Compare(a, b Version) int {
	switch {
	case a.Dev && b.Dev:
		return 0
	case a.Dev:
		return -1
	case b.Dev:
		return 1
	}
	for _, d := range [][2]int{{a.Year, b.Year}, {a.Month, b.Month}, {a.Day, b.Day}, {a.N, b.N}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Replaces reports whether release, a tag, replaces current without being
// forced: when it is newer. A source build counts as its day, so only a
// release of a later day replaces it, and a build with no version is
// replaced only when forced.
func Replaces(release, current Version) bool {
	switch {
	case current.Dev:
		return false
	case current.Source:
		return Compare(Version{Year: release.Year, Month: release.Month, Day: release.Day}, Version{Year: current.Year, Month: current.Month, Day: current.Day}) > 0
	}
	return Compare(release, current) > 0
}
