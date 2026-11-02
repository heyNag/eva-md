package main

import (
	"runtime/debug"
	"testing"

	"github.com/heyNag/eva-md/internal/okfadapter"
)

// A build takes its version from the commit it was built from: the commit's
// day in UTC, then its UTC time, so builds of one day sort, its short hash
// and whether the tree had changes; a fetched module from its pseudo-version
// in each form Go writes; and anything else is "dev". Every version is a page
// author okf accepts, as the pages eva-md builds itself name it.
func TestCalver(t *testing.T) {
	checkout := func(at, modified string) *debug.BuildInfo {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: "9e4f609abcdef0123456789abcdef0123456789"},
			{Key: "vcs.time", Value: at},
			{Key: "vcs.modified", Value: modified},
		}}
	}
	module := func(v string) *debug.BuildInfo { return &debug.BuildInfo{Main: debug.Module{Version: v}} }
	for _, c := range []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"a checkout", checkout("2026-11-02T09:15:51Z", "false"), "2026.11.2+0915.9e4f609"},
		{"a changed tree", checkout("2026-11-02T09:15:51Z", "true"), "2026.11.2+0915.9e4f609.dirty"},
		{"a later commit of the day", checkout("2026-11-02T16:40:00Z", "false"), "2026.11.2+1640.9e4f609"},
		{"a commit made on the next day in its own zone", checkout("2026-11-03T01:30:00+05:30", "false"), "2026.11.2+2000.9e4f609"},
		{"December", checkout("2026-12-31T23:59:59Z", "false"), "2026.12.31+2359.9e4f609"},
		{"a fetched module", module("v0.0.0-20261102091551-9e4f609abcde"), "2026.11.2+0915.9e4f609"},
		{"after a tag", module("v1.2.4-0.20261231235959-abcdefabcdef"), "2026.12.31+2359.abcdefa"},
		{"after a prerelease", module("v1.2.3-pre.0.20260105000000-0123456789ab+incompatible"), "2026.1.5+0000.0123456"},
		{"a tagged module", module("v1.2.3"), "dev"},
		{"a local build Go recorded nothing of", module("(devel)"), "dev"},
		{"a bad commit time", checkout("yesterday", "false"), "dev"},
		{"no build info", nil, "dev"},
	} {
		got := calver(c.info)
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if a := okfadapter.Actor("eva-md", got); a != "eva-md/"+got {
			t.Errorf("%s: the author %q became %q", c.name, "eva-md/"+got, a)
		}
	}
}
