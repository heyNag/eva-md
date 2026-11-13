package guard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture's ledger: (a) holds old/a.go, frozen on AA-1, old/b.go and
// v1.go; (b) new/i.go, which leaves, and new/j.go, which is kept; (c) the
// folder c, of whose Go files only c/x.go is neither built, a test nor
// under testdata.
const ledgerA = "| Path | Built lines | Deleted by | Stage | Named | Scheduled on |\n| --- | ---: | --- | --- | --- | --- |\n" +
	"| `old/a.go` | 5 | ST-13 | CUT-4 | file | AA-1 |\n" +
	"| `old/b.go` | 7 | DV-26 | CUT-4 | loose | none |\n" +
	"| `v1.go` | 3 | hosts' own | CUT-4 | file | none |\n"

const ledgerB = "| Path | Built lines | Instrument | Added by | Fate | Could take it over |\n| --- | ---: | --- | --- | --- | --- |\n" +
	"| `new/i.go` | 11 | a counter | CT-1, 4341736 | leaves at CUT-4 | HO-12b |\n" +
	"| `new/j.go` | 13 | a report | CT-4 | kept by EV-23 | EV-23 |\n"

const ledgerC = "| Path | Lines | Built by | First caller |\n| --- | ---: | --- | --- |\n" +
	"| `c/**` | 3 | ST-03a | ST-05 |\n"

const ledgerEnd = "```ledger\ncounted-at: 0123456789ab\nbuilt: 156\nleaves-v1: 15\nleaves-instruments: 11\nleaves: 26\nnot-yet-called: 3\nend-state: 133\nceiling: 126\n```\n"

func ledgerText(a, b, c, end string) string {
	return "# The size ledger\n\nA made-up ledger. `not/a/row.go` is prose.\n\n## (a)\n\n" + a + "\nUnder the table: `internal/x.go`.\n\n## (b)\n\n" + b + "\n## (c)\n\n" + c + "\n## The end state\n\n" + end
}

var ledgerBase = ledgerText(ledgerA, ledgerB, ledgerC, ledgerEnd)

// ledgerRepo writes the fixture's cards and files: AA-1, merged, schedules
// old/a.go; AA-2, still building, schedules other.go and needs no row.
func ledgerRepo(t *testing.T) string {
	t.Helper()
	index := "| Card | Package | State |\n| --- | --- | --- |\n" + row("AA-1", "merged") + row("AA-2", "building since today")
	repo := cardsRepo(t, index, map[string]string{"AA-1": deletes("old/a.go"), "AA-2": deletes("other.go")},
		"old/a.go", "old/b.go", "v1.go", "other.go", "new/i.go", "new/j.go", "c/sub/built.go")
	for name, content := range map[string]string{
		"c/x.go":          "package c\n\nvar X = 1\n",
		"c/x_test.go":     "package c\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
		"c/testdata/t.go": "package t\n\nvar T = 1\n",
		"c/doc.txt":       "not Go\n\n\n",
	} {
		write(t, filepath.Join(repo, filepath.FromSlash(name)), content)
	}
	return repo
}

func ledgerBuilt() map[string]int {
	return map[string]int{"old/a.go": 5, "old/b.go": 7, "v1.go": 3, "new/i.go": 11, "new/j.go": 13, "c/sub/built.go": 17, "keep.go": 100}
}

func ledgerV1() map[string]bool {
	return map[string]bool{"old/a.go": true, "old/b.go": true, "v1.go": true, "other.go": true, "keep.go": true}
}

func checkLedger(t *testing.T, repo, text string, built map[string]int, v1 map[string]bool) Ledger {
	t.Helper()
	var had func() (map[string]bool, error)
	if v1 != nil {
		had = func() (map[string]bool, error) { return v1, nil }
	}
	l, err := CheckLedger(repo, text, built, 126, had)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestTheFixturesLedgerHolds(t *testing.T) {
	repo := ledgerRepo(t)
	l := checkLedger(t, repo, ledgerBase, ledgerBuilt(), ledgerV1())
	if !l.Holds() {
		t.Fatalf("the fixture's ledger does not hold:\n%s", l.Report())
	}
	if l.CountedAt != "0123456789ab" || len(l.Rows) != 6 || len(l.Keys) != 7 {
		t.Fatalf("read %+v", l)
	}
	for _, r := range l.Rows {
		if r.Frozen != (r.Path == "old/a.go") || r.Leaves != (r.Path == "new/i.go") {
			t.Errorf("row %s: frozen %v, leaves %v", r.Path, r.Frozen, r.Leaves)
		}
	}
	report := l.Report()
	for _, want := range []string{"(a) old/a.go: 5 (frozen: scheduled on AA-1)\n", "(b) new/i.go: 11 (leaves at CUT-4)\n", "(b) new/j.go: 13 (kept)\n", "(c) c/**: 3\n", "end-state: 133\n", "ceiling: 126\n", "no fault"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
}

// Each fault fails the check, named, and the fixture's ledger, which
// differs from each case by the one thing the case breaks, holds.
func TestEachLedgerFault(t *testing.T) {
	replace := func(old, new string) func(string) string {
		return func(s string) string {
			if !strings.Contains(s, old) {
				t.Fatalf("the fixture lacks %q", old)
			}
			return strings.Replace(s, old, new, 1)
		}
	}
	// A row whose path or figure cannot be read is not counted, so the sum
	// it was in no longer adds up.
	const dropped = "leaves-v1 says 15, and the figures it sums give 12"
	for _, c := range []struct {
		name  string
		text  func(string) string
		repo  func(repo string)
		built func(map[string]int)
		v1    func(map[string]bool)
		want  []string
	}{
		{name: "a path that does not exist", repo: func(repo string) { os.Remove(filepath.Join(repo, "v1.go")) },
			want: []string{"row v1.go: the path does not exist"}},
		{name: "a folder that is a file", text: replace("| `v1.go` | 3 |", "| `old/a.go/**` | 0 |"), v1: func(m map[string]bool) { m["old/a.go/x.go"] = true },
			want: []string{"row old/a.go/**: the path does not exist", "leaves-v1 says 15, and the figures it sums give 12"}},
		{name: "an (a) row naming no file v1 had", v1: func(m map[string]bool) { delete(m, "v1.go") },
			want: []string{"row v1.go: it is in (a) and names no file v1 had"}},
		{name: "a (b) row naming a file v1 had", v1: func(m map[string]bool) { m["new/i.go"] = true },
			want: []string{"row new/i.go: it is in (b) and names 1 files v1 had"}},
		{name: "two rows over one file", text: replace("| `new/j.go` | 13 | a report | CT-4 | kept by EV-23 | EV-23 |\n", "| `new/j.go` | 13 | a report | CT-4 | kept by EV-23 | EV-23 |\n| `new/j.go` | 13 | again | CT-4 | kept by EV-23 | EV-23 |\n"),
			want: []string{"rows new/j.go and new/j.go: both hold the same files"}},
		{name: "a folder over a file", text: replace("| `c/**` | 3 | ST-03a | ST-05 |\n", "| `c/**` | 3 | ST-03a | ST-05 |\n| `old/**` | 0 | ST-03a | ST-05 |\n"),
			want: []string{"rows old/a.go and old/**: both hold the same files", "rows old/b.go and old/**: both hold the same files"}},
		{name: "a file under a folder that comes first", text: replace("| `old/a.go` | 5 |", "| `old/**` | 0 | DV-26 | CUT-4 | loose | none |\n| `old/a.go` | 5 |"),
			want: []string{"rows old/** and old/a.go: both hold the same files", "rows old/** and old/b.go: both hold the same files"}},
		{name: "a card twice in Scheduled on", text: replace("| file | AA-1 |", "| file | AA-1, AA-1 |"),
			want: []string{`row old/a.go: its Scheduled on, "AA-1, AA-1", is neither none nor cards' ids, each once, separated by commas`}},
		{name: "a word in Scheduled on that is no card", text: replace("| file | AA-1 |", "| file | AA-1, soon |"),
			want: []string{`row old/a.go: its Scheduled on, "AA-1, soon", is neither none nor cards' ids`}},
		{name: "a merged card's entry with no row scheduled on it", text: replace("| file | AA-1 |", "| file | none |"),
			want: []string{"AA-1's deletes block names old/a.go, and no (a) row of that path is scheduled on AA-1"}},
		{name: "scheduled on a card not merged", text: replace("| `v1.go` | 3 | hosts' own | CUT-4 | file | none |", "| `v1.go` | 3 | hosts' own | CUT-4 | file | AA-2 |"),
			want: []string{"row v1.go: it is scheduled on AA-2, which the index does not list as merged"}},
		{name: "scheduled on a card that does not exist", text: replace("| `v1.go` | 3 | hosts' own | CUT-4 | file | none |", "| `v1.go` | 3 | hosts' own | CUT-4 | file | ZZ-9 |"),
			want: []string{"row v1.go: it is scheduled on ZZ-9, which the index does not list as merged"}},
		{name: "scheduled on a card whose block lacks the path", text: replace("| loose | none |", "| loose | AA-1 |"),
			want: []string{"row old/b.go: it is scheduled on AA-1, whose deletes block does not name it"}},
		{name: "leaves-v1 that does not add up", text: replace("leaves-v1: 15", "leaves-v1: 16"),
			want: []string{"leaves-v1 says 16, and the figures it sums give 15", "leaves says 26, and the figures it sums give 27"}},
		{name: "leaves-instruments that does not add up", text: replace("leaves-instruments: 11", "leaves-instruments: 24"),
			want: []string{"leaves-instruments says 24, and the figures it sums give 11", "leaves says 26, and the figures it sums give 39"}},
		{name: "leaves that does not add up", text: replace("leaves: 26", "leaves: 25"),
			want: []string{"leaves says 25, and the figures it sums give 26", "end-state says 133, and the figures it sums give 134"}},
		{name: "not-yet-called that does not add up", text: replace("not-yet-called: 3", "not-yet-called: 4"),
			want: []string{"not-yet-called says 4, and the figures it sums give 3", "end-state says 133, and the figures it sums give 134"}},
		{name: "end-state that does not add up", text: replace("end-state: 133", "end-state: 134"),
			want: []string{"end-state says 134, and the figures it sums give 133"}},
		{name: "a ceiling that is not the ceiling", text: replace("ceiling: 126", "ceiling: 127"),
			want: []string{"ceiling says 127, and the figures it sums give 126"}},
		// A kept row adds nothing to leaves: given as leaving, it does.
		{name: "a kept row read as leaving", text: replace("| kept by EV-23 |", "| leaves at CUT-4 |"),
			want: []string{"leaves-instruments says 11, and the figures it sums give 24"}},
		{name: "a frozen row one line off", built: func(m map[string]int) { m["old/a.go"] = 6 },
			want: []string{"row old/a.go: says 5, counts 6, and the row is frozen: the deletes block of AA-1 has scheduled it"}},
		{name: "an unknown table", text: replace("## (c)", "| Path | Lines |\n| --- | --- |\n| `x.go` | 1 |\n\n## (c)"),
			want: []string{"a table whose header is not one of the ledger's three"}},
		{name: "a table missing", text: func(string) string { return ledgerText(ledgerA, "", ledgerC, ledgerEnd) },
			want: []string{"the ledger has no (b) table", "leaves-instruments says 11, and the figures it sums give 0"}},
		{name: "a table twice", text: func(string) string { return ledgerText(ledgerA, ledgerB, ledgerC+"\n"+ledgerC, ledgerEnd) },
			want: []string{"a second (c) table", "rows c/** and c/**", "not-yet-called says 3, and the figures it sums give 6"}},
		{name: "no separator", text: replace("| --- | ---: | --- | --- |\n", ""),
			want: []string{"the (c) table's header is not followed by its separator line", "not-yet-called says 3, and the figures it sums give 0"}},
		{name: "a path not in backticks", text: replace("| `v1.go` | 3 |", "| v1.go | 3 |"),
			want: []string{"the row's path, v1.go, is not in backticks", dropped}},
		{name: "a path that is a pattern", text: replace("| `v1.go` | 3 |", "| `*.go` | 3 |"),
			want: []string{`the row's path: the entry "*.go" uses a pattern`, dropped}},
		{name: "a figure that is not a number", text: replace("| `v1.go` | 3 |", "| `v1.go` | 3,0 |"),
			want: []string{`row v1.go: its figure, "3,0", is not a whole number`, dropped}},
		{name: "a row of too few cells", text: replace("| `v1.go` | 3 | hosts' own | CUT-4 | file | none |", "| `v1.go` | 3 | hosts' own | CUT-4 | file |"),
			want: []string{"a row of 5 cells in the (a) table, which has 6", dropped}},
		{name: "a row that does not close", text: replace("| `v1.go` | 3 | hosts' own | CUT-4 | file | none |", "| `v1.go` | 3 | hosts' own | CUT-4 | file | none"),
			want: []string{"a row that does not end with |", dropped}},
		{name: "a deleter that is no package", text: replace("| hosts' own |", "| someone |"),
			want: []string{`row v1.go: its Deleted by, "someone", is neither a plan id nor a track's own`}},
		{name: "a stage that is no package", text: replace("| ST-13 | CUT-4 |", "| ST-13 | later |"),
			want: []string{`row old/a.go: its Stage, "later", is not a plan id`}},
		{name: "a reading neither file nor loose", text: replace("| loose |", "| guessed |"),
			want: []string{`row old/b.go: its Named, "guessed", is neither file nor loose`}},
		{name: "a fate of neither kind", text: replace("| kept by EV-23 |", "| kept |"),
			want: []string{`row new/j.go: its Fate, "kept", is neither leaves at CUT-4 nor kept by a plan id`}},
		{name: "a first caller that is no package", text: replace("| ST-03a | ST-05 |", "| ST-03a | soon |"),
			want: []string{`row c/**: its First caller, "soon", is not a plan id`}},
		{name: "no end state", text: replace(ledgerEnd, ""),
			want: []string{"the ledger has no block of its end state"}},
		{name: "a key missing", text: replace("end-state: 133\n", ""),
			want: []string{"the end state does not give end-state"}},
		{name: "a key twice", text: replace("built: 156\n", "built: 156\nbuilt: 156\n"),
			want: []string{"the end state gives built twice"}},
		{name: "an unknown key", text: replace("built: 156\n", "built: 156\nheadroom: 3\n"),
			want: []string{`the end state's line "headroom: 3" is not one of its keys`}},
		{name: "counted-at that is no commit", text: replace("counted-at: 0123456789ab", "counted-at: today"),
			want: []string{`the end state's counted-at, "today", is not a commit's first 12 hexadecimal digits`}},
		{name: "a figure of the end state that is no number", text: replace("built: 156", "built: many"),
			want: []string{`the end state's built, "many", is not a whole number`}},
		{name: "a table after the end state", text: func(string) string { return ledgerText(ledgerA, ledgerB, "", ledgerEnd) + "\n" + ledgerC },
			want: []string{"a table after the end state's block, which comes last"}},
		{name: "two end states", text: func(s string) string { return s + "\n```ledger\n```\n" },
			want: []string{"a second block of the end state"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, text, built, v1 := ledgerRepo(t), ledgerBase, ledgerBuilt(), ledgerV1()
			if l := checkLedger(t, repo, text, built, v1); !l.Holds() {
				t.Fatalf("mended, the ledger does not hold:\n%s", l.Report())
			}
			if c.text != nil {
				text = c.text(text)
			}
			if c.repo != nil {
				c.repo(repo)
			}
			if c.built != nil {
				c.built(built)
			}
			if c.v1 != nil {
				c.v1(v1)
			}
			l := checkLedger(t, repo, text, built, v1)
			if l.Holds() || len(l.Faults) == 0 {
				t.Fatalf("no fault:\n%s", l.Report())
			}
			if len(l.Faults) != len(c.want) {
				t.Errorf("%d faults, want %d:\n%s", len(l.Faults), len(c.want), l.Report())
			}
			for _, want := range c.want {
				if !containsFault(l.Faults, want) {
					t.Errorf("no fault says %q:\n%s", want, l.Report())
				}
			}
		})
	}
}

func containsFault(faults []string, want string) bool {
	for _, f := range faults {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}

// A row one line off, not frozen, is a difference and not a fault, given
// with the line to write; so is each key it moves.
func TestARowOneLineOffIsADifference(t *testing.T) {
	repo, built := ledgerRepo(t), ledgerBuilt()
	built["old/b.go"] = 8
	l := checkLedger(t, repo, ledgerBase, built, ledgerV1())
	if len(l.Faults) != 0 || l.Holds() {
		t.Fatalf("faults or none:\n%s", l.Report())
	}
	want := []Difference{
		{"row old/b.go: says 7, counts 8", "| `old/b.go` | 8 | DV-26 | CUT-4 | loose | none |"},
		{"key built: says 156, counts 157", "built: 157"},
		{"key leaves-v1: says 15, counts 16", "leaves-v1: 16"},
		{"key leaves: says 26, counts 27", "leaves: 27"},
	}
	if len(l.Differences) != len(want) {
		t.Fatalf("differences %q, want %q", l.Differences, want)
	}
	for i := range want {
		if l.Differences[i] != want[i] {
			t.Errorf("difference %q, want %q", l.Differences[i], want[i])
		}
	}
	if !strings.Contains(l.Report(), "Difference: row old/b.go: says 7, counts 8\n  Write: | `old/b.go` | 8 |") {
		t.Errorf("report:\n%s", l.Report())
	}

	// A file of (c) that comes to be built leaves (c)'s count.
	repo, built = ledgerRepo(t), ledgerBuilt()
	built["c/x.go"] = 3
	l = checkLedger(t, repo, ledgerBase, built, ledgerV1())
	if len(l.Faults) != 0 || len(l.Differences) == 0 || l.Differences[0].Text != "row c/**: says 3, counts 0" {
		t.Fatalf("a (c) file built:\n%s", l.Report())
	}
}

// (c) counts the Go files under its row that are not built, not tests and
// not under testdata, and nothing else.
func TestNotYetCalledCountsOnlyUnbuiltCode(t *testing.T) {
	repo, built := ledgerRepo(t), ledgerBuilt()
	count := func() int {
		t.Helper()
		l := checkLedger(t, repo, ledgerBase, built, ledgerV1())
		for _, r := range l.Rows {
			if r.Table == "c" {
				return r.Counts
			}
		}
		t.Fatal("no (c) row")
		return 0
	}
	if n := count(); n != 3 {
		t.Fatalf("(c) counts %d, want 3", n)
	}
	// c/sub/built.go holds "package x\n": once nothing builds it, it counts.
	delete(built, "c/sub/built.go")
	if n := count(); n != 4 {
		t.Fatalf("(c) with its built file no longer built counts %d, want 4", n)
	}
	write(t, filepath.Join(repo, "c", "more_test.go"), strings.Repeat("\n", 50))
	write(t, filepath.Join(repo, "c", "deep", "testdata", "x", "y.go"), strings.Repeat("\n", 50))
	write(t, filepath.Join(repo, "c", "notes.md"), strings.Repeat("\n", 50))
	if n := count(); n != 4 {
		t.Fatalf("(c) counts a test, testdata or a file not Go: %d", n)
	}
	write(t, filepath.Join(repo, "c", "deep", "z.go"), "package deep\n\n")
	if n := count(); n != 6 {
		t.Fatalf("(c) misses a file in a folder below: %d", n)
	}
}

// With no v1, the faults that need v1's files are not looked for, and
// every other check still runs.
func TestALedgerWithNoV1SkipsItsFaults(t *testing.T) {
	repo, v1 := ledgerRepo(t), ledgerV1()
	delete(v1, "v1.go")
	v1["new/i.go"] = true
	if l := checkLedger(t, repo, ledgerBase, ledgerBuilt(), v1); len(l.Faults) != 2 {
		t.Fatalf("with v1:\n%s", l.Report())
	}
	if l := checkLedger(t, repo, ledgerBase, ledgerBuilt(), nil); !l.Holds() {
		t.Fatalf("with no v1:\n%s", l.Report())
	}
	broken := strings.Replace(ledgerBase, "| file | AA-1 |", "| file | none |", 1)
	if l := checkLedger(t, repo, broken, ledgerBuilt(), nil); len(l.Faults) != 1 {
		t.Fatalf("with no v1, a merged block's fault:\n%s", l.Report())
	}
	if _, err := CheckLedger(repo, ledgerBase, ledgerBuilt(), 126, func() (map[string]bool, error) { return nil, os.ErrNotExist }); err == nil {
		t.Error("v1's error is not returned")
	}
}

// The repository's ledger is well formed, its sums add up and its frozen
// rows give today's figures. A figure that is merely not today's is not
// this test's to fail: other packages move the count, and the lead writes
// the figures at each merge with eva-eval size --ledger. Nor is the path
// of a row no deletes block has frozen that a package has since moved: no
// builder's check may fail on the ledger (EV-11b, the lead's rulings of
// 2026-11-13), and the command fails on it. The lines to write are logged.
func TestTheSizeLedgerHoldsForThisRepository(t *testing.T) {
	goEnv(t, false)
	repo := filepath.Join("..", "..", "..")
	text, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(LedgerFile)))
	if err != nil {
		t.Fatal(err)
	}
	built, err := Built(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	l, err := CheckLedger(repo, string(text), built, Ceiling, nil)
	if err != nil {
		t.Fatal(err)
	}
	moved := map[string]bool{}
	for _, r := range l.Rows {
		if r.Gone && !r.Frozen {
			moved[GoneFault(r.Path)] = true
		}
	}
	for _, f := range l.Faults {
		if moved[f] {
			t.Logf("Fault, for eva-eval size --ledger to fail on: %s", f)
		} else {
			t.Errorf("Fault: %s", f)
		}
	}
	for _, d := range l.Differences {
		t.Logf("Difference: %s\n  Write: %s", d.Text, d.Write)
	}
	tables := map[string]int{}
	for _, r := range l.Rows {
		tables[r.Table]++
	}
	// A row per table at least: a package that builds what a row holds, or
	// gives (c)'s code its caller, drops the row, which fails nothing here.
	if tables["a"] < 1 || tables["b"] < 1 || tables["c"] < 1 || len(l.Keys) != len(ledgerKeys)-1 {
		t.Errorf("the ledger has rows %v and keys %v", tables, l.Keys)
	}
}

// A row whose path is gone says so, and says whether it was frozen: the
// test on the repository fails on a frozen row's, and only logs another's.
func TestARowWhosePathIsGone(t *testing.T) {
	repo := ledgerRepo(t)
	for _, f := range []string{"v1.go", "old/a.go"} {
		if err := os.Remove(filepath.Join(repo, f)); err != nil {
			t.Fatal(err)
		}
	}
	l := checkLedger(t, repo, ledgerBase, ledgerBuilt(), ledgerV1())
	gone := map[Pattern]bool{}
	for _, r := range l.Rows {
		if r.Gone {
			gone[r.Path] = r.Frozen
		}
	}
	if len(gone) != 2 || gone["v1.go"] || !gone["old/a.go"] {
		t.Fatalf("gone %v:\n%s", gone, l.Report())
	}
	if len(l.Faults) != 2 || !containsFault(l.Faults, GoneFault("v1.go")) || !containsFault(l.Faults, GoneFault("old/a.go")) {
		t.Fatalf("faults:\n%s", l.Report())
	}
}

// mergeCards rewrites the fixture's index with the cards given merged
// besides AA-1, each with the text given (a card of no block for ""), and
// AA-2 still building.
func mergeCards(t *testing.T, repo string, cards map[string]string) {
	t.Helper()
	index := "| Card | Package | State |\n| --- | --- | --- |\n" + row("AA-1", "merged") + row("AA-2", "building since today")
	for id, text := range cards {
		index += row(id, "merged")
		if text == "" {
			text = "# A card\n"
		}
		write(t, filepath.Join(repo, "docs", "v2", "cards", id+".md"), text)
	}
	write(t, filepath.Join(repo, "docs", "v2", "cards", "README.md"), index)
}

// Once a row's stage has merged the row is done: its path must be gone,
// and a frozen row whose path the stage deleted moves to 0 with a
// difference, not a fault.
func TestARowWhoseStageHasMerged(t *testing.T) {
	repo, built := ledgerRepo(t), ledgerBuilt()
	mergeCards(t, repo, map[string]string{CutOver: ""})
	l := checkLedger(t, repo, ledgerBase, built, ledgerV1())
	if len(l.Faults) != 3 || len(l.Differences) != 0 {
		t.Fatalf("the stage merged, its paths still there:\n%s", l.Report())
	}
	for _, f := range []string{"old/a.go", "old/b.go", "v1.go"} {
		if want := "row " + f + ": its stage, CUT-4, has merged and the path is still in the repository"; !containsFault(l.Faults, want) {
			t.Errorf("no fault says %q:\n%s", want, l.Report())
		}
	}

	for _, f := range []string{"old/a.go", "old/b.go", "v1.go"} {
		if err := os.Remove(filepath.Join(repo, f)); err != nil {
			t.Fatal(err)
		}
		delete(built, f)
	}
	l = checkLedger(t, repo, ledgerBase, built, ledgerV1())
	if len(l.Faults) != 0 || len(l.Differences) == 0 || l.Differences[0].Text != "row old/a.go: says 5, counts 0" {
		t.Fatalf("the stage merged, its paths gone:\n%s", l.Report())
	}
	for _, r := range l.Rows {
		if r.Table == "a" && (!r.Done || r.Frozen || !r.Gone) {
			t.Errorf("row %s: done %v, frozen %v, gone %v", r.Path, r.Done, r.Frozen, r.Gone)
		}
	}
	if !strings.Contains(l.Report(), "(a) old/a.go: 0 (done: CUT-4 has merged)\n") {
		t.Errorf("report:\n%s", l.Report())
	}

	mended := ledgerText(strings.NewReplacer("| 5 |", "| 0 |", "| 7 |", "| 0 |", "| 3 |", "| 0 |").Replace(ledgerA), ledgerB, ledgerC,
		strings.NewReplacer("built: 156", "built: 141", "leaves-v1: 15", "leaves-v1: 0", "leaves: 26", "leaves: 11").Replace(ledgerEnd))
	if l := checkLedger(t, repo, mended, built, ledgerV1()); !l.Holds() {
		t.Fatalf("mended:\n%s", l.Report())
	}
}

// Two merged cards may name one path: its row is scheduled on both, and a
// row that names only one of them leaves the other's entry without a row.
func TestTwoCardsScheduleOnePath(t *testing.T) {
	repo := ledgerRepo(t)
	mergeCards(t, repo, map[string]string{"AA-3": deletes("old/a.go")})
	l := checkLedger(t, repo, ledgerBase, ledgerBuilt(), ledgerV1())
	if len(l.Faults) != 1 || !containsFault(l.Faults, "AA-3's deletes block names old/a.go, and no (a) row of that path is scheduled on AA-3") {
		t.Fatalf("scheduled on AA-1 alone:\n%s", l.Report())
	}
	both := strings.Replace(ledgerBase, "| file | AA-1 |", "| file | AA-1, AA-3 |", 1)
	l = checkLedger(t, repo, both, ledgerBuilt(), ledgerV1())
	if !l.Holds() {
		t.Fatalf("scheduled on both:\n%s", l.Report())
	}
	if r := l.Rows[0]; r.Path != "old/a.go" || !r.Frozen || !strings.Contains(l.Report(), "(frozen: scheduled on AA-1, AA-3)") {
		t.Fatalf("row %+v:\n%s", r, l.Report())
	}
	built := ledgerBuilt()
	built["old/a.go"] = 6
	if l := checkLedger(t, repo, both, built, ledgerV1()); len(l.Faults) != 1 ||
		!containsFault(l.Faults, "row old/a.go: says 5, counts 6, and the row is frozen: the deletes block of AA-1 and of AA-3 has scheduled it") {
		t.Fatalf("a row frozen by two cards, moved:\n%s", l.Report())
	}
	// One of the two not merged is a fault, and the other still freezes
	// the row.
	mergeCards(t, repo, nil)
	if l := checkLedger(t, repo, both, ledgerBuilt(), ledgerV1()); len(l.Faults) != 1 || !l.Rows[0].Frozen ||
		!containsFault(l.Faults, "row old/a.go: it is scheduled on AA-3, which the index does not list as merged") {
		t.Fatalf("AA-3 not merged:\n%s", l.Report())
	}
}
