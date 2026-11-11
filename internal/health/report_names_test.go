package health

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/update"
)

// sourceNames are the names the source files of dir, tests left out, give
// in the first group of each of patterns, sorted, each once.
func sourceNames(t *testing.T, dir string, patterns ...string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no source in %s: %v", dir, err)
	}
	var names []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range patterns {
			for _, m := range regexp.MustCompile(p).FindAllStringSubmatch(string(data), -1) {
				names = append(names, m[1])
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// sorted is names sorted, each once.
func sorted(names ...[]string) []string {
	var all []string
	for _, n := range names {
		all = append(all, n...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

// The names the report counts by are the names eva-md's code writes, read
// from that code, so a name added there and not here fails: the marker
// kinds of both rule sets, the run log's outcomes, reasons and rules, the
// commit records' reasons, and the inbox's kinds of entry.
func TestTheReportsNamesAreTheCodes(t *testing.T) {
	check := func(what string, got, want []string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s: the report counts by\n%q\nand the code writes\n%q", what, got, want)
		}
	}
	marker := []string{`kind:\s*"([a-z_]+)"`, `Kind:\s*"([a-z_]+)"`, `replace\([^()\n]*,\s*"([a-z_]+)"\)`}
	check("marker kinds", sorted(markerKinds), sorted(sourceNames(t, "../redact", marker...), sourceNames(t, "../redact/baseline", marker...)))
	outcomes := sourceNames(t, "../update", `\bOutcome[A-Z]\w*\s*=\s*"([a-z_]+)"`)
	check("update outcomes", sorted(updateOutcomes, []string{update.OutcomeBusy}), outcomes)
	check("update reasons", sorted(updateReasons), sourceNames(t, "../update", `\bReason[A-Z]\w*\s*=\s*"([a-z_]+)"`))
	check("rejection rules", sorted(rejectionRules), sourceNames(t, "../update", `\bRule[A-Z]\w*\s*=\s*"([a-z_]+)"`))
	classes := sourceNames(t, "../wikigit", `\bClass[A-Z]\w*\s*=\s*"([a-z_]+)"`)
	for i, c := range classes {
		classes[i] = "commit_" + c
	}
	check("commit reasons", sorted(commitReasons), sorted(classes, sourceNames(t, "../wikigit",
		`(?:refuse|fail)\("([a-z_]+)"\)`, `(?:Failed|Busy|Refused), "([a-z_]+)"`, `\{"[A-Za-z_-]+", "([a-z_]+)"\}`, `ReasonPushOnly = "([a-z_]+)"`)))
	check("entry kinds", sorted(entryKinds), sorted(sourceNames(t, "../hook", `Kind = "([a-z_]+)"`, `role, kind = "[a-z]+", "([a-z]+)"`),
		sourceNames(t, "../catchup", `Kind = "([a-z_]+)"`), sourceNames(t, "../inbox", `Kind = "([a-z_]+)"`)))
}

// Every start-of-chat notice has its kind, and each kind a notice: each
// line of the notices' code that words one, its text pieces joined, is of
// a kind other than other, and of its own kind.
func TestEveryNoticeHasItsKind(t *testing.T) {
	literal := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	kinds := map[string]int{}
	for _, file := range []string{"health.go", "background.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(data), "\n") {
			if !strings.Contains(l, `"eva.md: `) {
				continue
			}
			var text string
			for _, q := range literal.FindAllString(l, -1) {
				s, err := strconv.Unquote(q)
				if err != nil {
					t.Fatal(err)
				}
				text += s
			}
			kinds[noticeKind(text)]++
			if noticeKind(text) == other {
				t.Errorf("%s: no kind for %q", file, text)
			}
		}
	}
	for _, n := range notices {
		if kinds[n[1]] == 0 {
			t.Errorf("no notice is of the kind %s", n[1])
		}
	}
	if kinds["trust_unreadable"] != 2 || len(kinds) != len(notices) {
		t.Errorf("the notices by kind: %v", kinds)
	}
	if noticeKind("") != "none" || noticeKind("eva.md: something new") != other {
		t.Error("an empty or unknown notice")
	}
}

// A tool line counts a run of eva-md system write, by any path to eva-md,
// in a subagent's line too, and an edit when a file it names is under one
// of the wiki's folders: not a folder that only starts like it, nor a
// relative path, which can't be placed.
func TestToolLinesOfTheWiki(t *testing.T) {
	wiki := []string{"/w/wiki", "/real/wiki"}
	for _, c := range []struct {
		line                    string
		writes, edits, unplaced int
	}{
		{"Bash eva-md system write x (exit 0)", 1, 0, 0},
		{"Bash (subagent) cd a && ~/.local/bin/eva-md system write --page x (exit 1)", 1, 0, 0},
		{"Bash eva-md system status (exit 0)", 0, 0, 0},
		{"Bash echo eva-md system (exit 0)", 0, 0, 0},
		{"Bash (exit 0)", 0, 0, 0},
		{"Edit /w/wiki/projects/p/threads/t.md (exit 0)", 0, 1, 0},
		{"Write (subagent) /real/wiki/index.md (exit unknown)", 0, 1, 0},
		{"Edit /w/wiki-two/a.md (exit 0)", 0, 0, 0},
		{"Edit /w/wiki (exit 0)", 0, 1, 0},
		{"NotebookEdit /w/wiki/a b.md (exit 0)", 0, 1, 0},
		{"apply_patch a.md, /w/wiki/b.md (exit 0)", 0, 1, 0},
		{"apply_patch a.md, b.md (exit 0)", 0, 0, 1},
		{"apply_patch /else/a.md (exit 0)", 0, 0, 0},
		{"Read /w/wiki/a.md (exit 0)", 0, 0, 0},
		{"Edit (exit 0)", 0, 0, 0},
	} {
		var u toolUse
		u.add(c.line, wiki)
		if u.SystemWrites != c.writes || u.Edits != c.edits || u.Unplaced != c.unplaced {
			t.Errorf("%q: %+v", c.line, u)
		}
	}
}

// Only a complete marker counts, by the kind the rules write, and any
// other kind as other.
func TestMarkersByKind(t *testing.T) {
	counts := map[string]int{}
	countMarkers(counts, "[REDACTED:jwt] [REDACTED:jwt][REDACTED:made_up] [REDACTED:Upper] [REDACTED:] [REDACTED:jwt [REDACTED:"+strings.Repeat("a", 33)+"] [REDACTED:password]")
	want := map[string]int{"jwt": 2, other: 1, "password": 1}
	if len(counts) != len(want) || counts["jwt"] != 2 || counts[other] != 1 || counts["password"] != 1 {
		t.Fatalf("%v, want %v", counts, want)
	}
}

// The bucket a percentile falls within, by the nearest rank.
func TestPercentileWithinABucket(t *testing.T) {
	ms := make([]int, len(DurationBounds)+1)
	if within(ms, 50) != 0 {
		t.Fatal("no run")
	}
	ms[0], ms[3], ms[len(ms)-1] = 10, 9, 1
	if got := []float64{within(ms, 50), within(ms, 95), within(ms, 100)}; !slices.Equal(got, []float64{2, 20, -1}) {
		t.Fatal(got)
	}
}
