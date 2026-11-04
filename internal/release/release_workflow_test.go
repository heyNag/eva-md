// Package release tests the release flow: scripts/release.sh, run against a
// temporary origin with a fake gh, and the release workflow.
package release

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every action the release workflow uses is pinned to a full commit, with
// its version in a comment, so a moved tag can't change what a release runs.
func TestReleaseWorkflowPinsEveryAction(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	uses := regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*(.*)$`)
	pinned := regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40} # v[0-9]+(\.[0-9]+)*$`)
	n := 0
	for i, line := range strings.Split(string(data), "\n") {
		m := uses.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n++
		if !pinned.MatchString(strings.TrimSpace(m[1])) {
			t.Errorf("line %d isn't pinned to a commit with its version: %s", i+1, strings.TrimSpace(line))
		}
	}
	if n == 0 {
		t.Fatal("the workflow uses no actions; the check read the wrong file")
	}
}

// The release workflow sums, attests and publishes the same files: the
// binaries it builds and the files it copies from scripts/, which
// scripts/release.sh checks main has. Once they are published, release.sh
// checks that SHA256SUMS lists exactly those files and verifies each one's
// provenance, so a file left out of one list would fail a pushed tag.
func TestReleaseWorkflowPublishesEveryAsset(t *testing.T) {
	read := func(path ...string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, path...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	workflow, script := read(".github", "workflows", "release.yml"), read("scripts", "release.sh")
	// fields is what re's group matches in text, split on spaces. Only one
	// line may match, so a second list can't go unchecked.
	fields := func(text, re string) []string {
		t.Helper()
		m := regexp.MustCompile(re).FindAllStringSubmatch(text, -1)
		if len(m) != 1 {
			t.Fatalf("%d lines match %s, not one", len(m), re)
		}
		return strings.Fields(m[0][1])
	}
	var want []string
	for _, arch := range fields(workflow, `(?m)^\s+for arch in (.+); do$`) {
		want = append(want, "eva-md-darwin-"+arch)
	}
	onMain := fields(script, `(?m)^for f in (.+); do$`)
	for _, f := range fields(workflow, `(?m)^\s+cp (.+) "\$out/"$`) {
		name, ok := strings.CutPrefix(f, "scripts/")
		if !ok {
			t.Errorf("the workflow copies %s, from outside scripts/", f)
		}
		if !slices.Contains(onMain, f) {
			t.Errorf("release.sh doesn't check that main has %s", f)
		}
		if _, err := os.Stat(filepath.Join("..", "..", f)); err != nil {
			t.Error(err)
		}
		want = append(want, name)
	}
	slices.Sort(want)
	var attested []string
	for _, f := range fields(workflow, `(?m)^\s+subject-path: \|\n((?:\s+release/\S+\n)+)`) {
		attested = append(attested, strings.TrimPrefix(f, "release/"))
	}
	for what, got := range map[string][]string{
		"release.sh checks":    fields(script, `(?m)^assets="(.+)"$`),
		"SHA256SUMS lists":     fields(workflow, `(?m)^\s+shasum -a 256 (.+) > SHA256SUMS$`),
		"the workflow attests": attested,
		"the release has":      fields(workflow, `--notes-file notes\.md \\\n\s+(.+) SHA256SUMS\n`),
	} {
		if got = slices.Sorted(slices.Values(got)); !slices.Equal(got, want) {
			t.Errorf("%s %v, not %v", what, got, want)
		}
	}
	if n := fields(workflow, `wc -l < SHA256SUMS \| tr -d ' '\)" = (\d+)`)[0]; n != strconv.Itoa(len(want)) {
		t.Errorf("the workflow checks that SHA256SUMS has %s lines, not %d", n, len(want))
	}
}
