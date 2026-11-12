package cardcheck

import (
	"context"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"
)

// importPath is this package's.
const importPath = "github.com/heyNag/eva-md/internal/cardcheck"

// root is the repository's root folder.
func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("no go.mod at the repository's root: %v", err)
	}
	return dir
}

func TestTheRepositoryPassesTheCheck(t *testing.T) {
	for _, p := range Check(os.DirFS(root(t))) {
		t.Error(p)
	}
}

// The parsers read the real files, so one that reads nothing fails here.
func TestTheParsersReadTheRepository(t *testing.T) {
	fsys := os.DirFS(root(t))
	text, err := fs.ReadFile(fsys, PlanFile)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := parsePlan(string(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) < 85 {
		t.Errorf("the plan has %d package ids, not at least 85", len(plan))
	}
	for id, stage := range map[string]int{"SEAM-0": 0, "HF-00": 0, "DV-01": 0, "LG-10": 1, "IF-01": 1, "ST-02": 1, "ST-05": 2, "CUT-1": 2, "CUT-4": 2, "DL-26": 3, "EV-23": 3, "LG-50": 4, "EV-51": 4} {
		if got, ok := plan[id]; !ok || got != stage {
			t.Errorf("plan id %s: stage %d (listed %v), want %d", id, got, ok, stage)
		}
	}
	// Ids the plan names only in prose or in other tables are not packages.
	for _, id := range []string{"LG-03", "EV-13", "HO-25", "ST-13", "DV-26", "DV-36", "DL-14"} {
		if _, ok := plan[id]; ok {
			t.Errorf("%s is read as a plan id", id)
		}
	}

	needs, err := readDesignNeeds(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(needs) < 80 {
		t.Errorf("the designs have %d need rows, not at least 80", len(needs))
	}
	designs := map[string]int{}
	for k, n := range needs {
		designs[k.design] += n
	}
	for _, d := range []string{"ledger.md", "store.md", "derive.md", "deliver.md", "hosts.md", "evaluate.md"} {
		if designs[d] == 0 {
			t.Errorf("no need read from %s", d)
		}
	}
	if len(designs) != 6 {
		t.Errorf("needs read from %v, not the six designs", designs)
	}
	for _, k := range []needKey{{"C-S4", "store.md"}, {"C-D1", "ledger.md"}, {"V-D10", "deliver.md"}, {"E-X2", "evaluate.md"}} {
		if needs[k] != 1 {
			t.Errorf("%s is read as a need %d times, not once", k, needs[k])
		}
	}
	for _, k := range []needKey{{"C-D1", "derive.md"}, {"C-S2", "ledger.md"}, {"S-D1", "store.md"}} {
		if needs[k] != 0 {
			t.Errorf("%s, a promise, is read as a need", k)
		}
	}

	cards, others, err := readCards(fsys, plan)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root(t), filepath.FromSlash(CardsDir)))
	if err != nil {
		t.Fatal(err)
	}
	var want, got []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "README.md" && !strings.HasPrefix(e.Name(), ".") {
			want = append(want, path.Join(CardsDir, e.Name()))
		}
	}
	for _, c := range cards {
		got = append(got, c.file)
	}
	got = append(got, others...)
	slices.Sort(want)
	slices.Sort(got)
	if len(cards) == 0 || !slices.Equal(got, want) {
		t.Errorf("cards read %q, want %q", got, want)
	}

	m := parseMatrix(readText(t, fsys, MatrixFile))
	if len(m.needs) < 80 || len(m.fakes) < 7 || len(m.bad) != 0 {
		t.Errorf("the matrix has %d need rows, %d fakes and %d tables under a wrong header", len(m.needs), len(m.fakes), len(m.bad))
	}
}

func TestTheMatrixKeepsItsSizeAndLineLimits(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root(t), filepath.FromSlash(MatrixFile)))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 24000 {
		t.Errorf("%s is %d bytes, over 24 KB", MatrixFile, len(data))
	}
	for i, line := range strings.Split(string(data), "\n") {
		if n := utf8.RuneCountInString(line); n > 400 {
			t.Errorf("%s line %d has %d characters, over 400", MatrixFile, i+1, n)
		}
	}
}

func TestParseCard(t *testing.T) {
	plan := map[string]int{"CT-1": 9, "LG-01": 0, "DL-02": 0, "DV-12": 1, "EV-16": 0, "SEAM-0": 0}
	for _, c := range []struct {
		file, text string
		packages   []string
		depends    []string
	}{
		{"docs/v2/cards/DV-12a.md", "# DV-12a: the worker\n\n**Depends on:** SEAM-0c (merged), for the switch.\n", []string{"DV-12"}, []string{"SEAM-0c"}},
		{"docs/v2/cards/CT-9.md", "# CT-9: counters (LG-01, DL-02) and (XX-1)\n\nText (DV-12).\n", []string{"LG-01", "DL-02"}, nil},
		{"docs/v2/cards/CT-3b.md", "# CT-3b: the capture audit\n\n**Follows:** [CT-3](CT-3.md).\n", nil, nil},
		{"docs/v2/cards/XY-1.md", "# XY-1: x\n\n**Depends on:** EV-16's baseline, CT-1 and CT-2. **Part of:** DV-12.\nlater\n\nRule: a `**Depends on:**` label names LG-99;\n", nil, []string{"EV-16", "CT-1", "CT-2"}},
		{"docs/v2/cards/SEAM-0a.md", "# SEAM-0a: tables\n\n**Depends on:** nothing.\n**Depends on:** HO-10b merged\n", []string{"SEAM-0"}, []string{"HO-10b"}},
		// A bold word inside the label does not end it; the next label does.
		{"docs/v2/cards/XY-2.md", "# XY-2: x\n\n**Depends on:** SEAM-0a (**merged**), ZZ-99, c and d. **Part of:** IF-01.\n", nil, []string{"SEAM-0a", "ZZ-99"}},
		// The label runs to the end of its paragraph, and no further.
		{"docs/v2/cards/XY-3.md", "# XY-3: x\n\n**Depends on:** SEAM-0a and\nLG-10, both merged.\n\nZZ-98 is no dependency.\n- ZZ-97 neither\n", nil, []string{"SEAM-0a", "LG-10"}},
		{"docs/v2/cards/XY-4.md", "# XY-4: x\n\n**Depends on:** SEAM-0a\n- ZZ-97 is a list\n", nil, []string{"SEAM-0a"}},
		// A label in a code block or a quote is an example, not the card's.
		{"docs/v2/cards/XY-5.md", "# XY-5: x\n\n```\n**Depends on:** ZZ-99\n```\n\n~~~~md\n**Depends on:** ZZ-96\n~~~~\n\n    **Depends on:** ZZ-95\n\n> **Depends on:** ZZ-94\n", nil, nil},
		// A title in a code block is not the card's title.
		{"docs/v2/cards/XY-6.md", "```\n# XY-6: x (DV-12)\n```\n# XY-6: y (LG-01)\n", []string{"LG-01"}, nil},
	} {
		got := parseCard(c.file, c.text, plan)
		if !slices.Equal(got.packages, c.packages) || !slices.Equal(got.depends, c.depends) {
			t.Errorf("%s: packages %q and depends %q, want %q and %q", c.file, got.packages, got.depends, c.packages, c.depends)
		}
	}
}

func TestSplitRowKeepsPipesInCode(t *testing.T) {
	got := splitRow("| S-H5 | hosts | promise | `remote set|show|remove` and a\\|b |")
	want := []string{"S-H5", "hosts", "promise", "`remote set|show|remove` and a|b"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// What renders as code is never read as a table.
func TestCodeIsNoTable(t *testing.T) {
	text := strings.Join([]string{
		"## Real",
		"| A | B |",
		"| --- | --- |",
		"| a1 | b1 |",
		"",
		"```",
		"## Fenced",
		"| C | D |",
		"| --- | --- |",
		"```",
		"",
		"````md",
		"```",
		"| E | F |",
		"```",
		"````",
		"",
		"    | G | H |",
		"    | --- | --- |",
		"",
		"    | I | J |",
		"",
		"> | K | L |",
		"",
		"- a list item",
		"",
		"    | M | N |",
		"    | --- | --- |",
		"",
		"```code``` in a line",
		"| O | P |",
	}, "\n")
	var got []string
	for _, tb := range tables(strings.Split(text, "\n"), 0, strings.Count(text, "\n")+1) {
		got = append(got, tb.heading+":"+strings.Join(tb.header, ","))
	}
	// The table inside the list item is the list's, not code; the line with
	// a code span opens no fence.
	want := []string{"Real:A,B", "Real:M,N", "Real:O,P"}
	if !slices.Equal(got, want) {
		t.Errorf("tables %q, want %q", got, want)
	}
}

// repoCopy is a copy of the files Check reads, which a test then breaks.
func repoCopy(t *testing.T) fstest.MapFS {
	t.Helper()
	fsys := os.DirFS(root(t))
	m := fstest.MapFS{}
	for _, f := range []string{PlanFile, MatrixFile} {
		m[f] = &fstest.MapFile{Data: []byte(readText(t, fsys, f)), Mode: 0o644}
	}
	for _, dir := range []string{DesignDir, CardsDir} {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				f := path.Join(dir, e.Name())
				m[f] = &fstest.MapFile{Data: []byte(readText(t, fsys, f)), Mode: 0o644}
			}
		}
	}
	return m
}

func readText(t *testing.T, fsys fs.FS, name string) string {
	t.Helper()
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// edit replaces old, which must be in the file once, with new. Tests edit
// only the matrix, this package's own, or text they added themselves; a
// broken input in the lead's files is built from made-up rows and cards.
func edit(t *testing.T, m fstest.MapFS, file, old, new string) {
	t.Helper()
	f, ok := m[file]
	if !ok {
		t.Fatalf("no %s", file)
	}
	if n := strings.Count(string(f.Data), old); n != 1 {
		t.Fatalf("%q is in %s %d times, not once", old, file, n)
	}
	f.Data = []byte(strings.Replace(string(f.Data), old, new, 1))
}

// insertAfter inserts text after the first line of file, outside code,
// that starts with prefix.
func insertAfter(t *testing.T, m fstest.MapFS, file, prefix, text string) {
	t.Helper()
	f, ok := m[file]
	if !ok {
		t.Fatalf("no %s", file)
	}
	lines := strings.Split(string(f.Data), "\n")
	code := codeLines(lines)
	for i, line := range lines {
		if !code[i] && strings.HasPrefix(line, prefix) {
			lines = slices.Insert(lines, i+1, strings.Split(text, "\n")...)
			f.Data = []byte(strings.Join(lines, "\n"))
			return
		}
	}
	t.Fatalf("no line of %s starts %q", file, prefix)
}

// contractsHeading returns the heading line of a design's contracts section.
func contractsHeading(t *testing.T, m fstest.MapFS, design string) string {
	t.Helper()
	for _, line := range strings.Split(string(m[path.Join(DesignDir, design)].Data), "\n") {
		if contractsHead.MatchString(line) {
			return line
		}
	}
	t.Fatalf("%s has no contracts section", design)
	return ""
}

// addNeed adds a made-up need row to a design: a contracts table of its
// own at the top of the design's contracts section.
func addNeed(t *testing.T, m fstest.MapFS, design, id string) {
	t.Helper()
	insertAfter(t, m, path.Join(DesignDir, design), contractsHeading(t, m, design),
		"\n| Id | With | Direction | One line |\n| --- | --- | --- | --- |\n| "+id+" | store | need | made up |\n")
}

// addCard adds a made-up card to the copy.
func addCard(m fstest.MapFS, name, text string) {
	m[path.Join(CardsDir, name)] = &fstest.MapFile{Data: []byte(text), Mode: 0o644}
}

// freePlanID returns a plan id of at least the stage given that no card of
// the copy names as its package.
func freePlanID(t *testing.T, m fstest.MapFS, minStage int) string {
	t.Helper()
	plan, err := parsePlan(string(m[PlanFile].Data))
	if err != nil {
		t.Fatal(err)
	}
	cards, _, err := readCards(m, plan)
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, c := range cards {
		for _, p := range c.packages {
			named[p] = true
		}
	}
	var free []string
	for id, stage := range plan {
		if stage >= minStage && !named[id] {
			free = append(free, id)
		}
	}
	if len(free) == 0 {
		t.Fatalf("every plan id of stage %d or later has a card", minStage)
	}
	sort.Strings(free)
	return free[0]
}

// anyCard returns the id of a card of the copy.
func anyCard(t *testing.T, m fstest.MapFS) string {
	t.Helper()
	for f := range m {
		if path.Dir(f) == CardsDir && path.Ext(f) == ".md" && path.Base(f) != "README.md" {
			return strings.TrimSuffix(path.Base(f), ".md")
		}
	}
	t.Fatal("the copy has no card")
	return ""
}

// row returns the matrix's row of a need, which must be there once.
func row(t *testing.T, m fstest.MapFS, id, design string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(string(m[MatrixFile].Data), "\n") {
		if strings.HasPrefix(line, "| "+id+" | "+design+" |") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the matrix has %d rows for %s in %s", len(found), id, design)
	}
	return found[0]
}

// fakeLine returns the matrix's interface row whose provider is the one
// given.
func fakeLine(t *testing.T, m fstest.MapFS, provider string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(string(m[MatrixFile].Data), "\n") {
		if cells := splitRow(line); strings.HasPrefix(line, "|") && len(cells) == 4 && plain(cells[2]) == provider {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the matrix has %d interface rows provided by %s", len(found), provider)
	}
	return found[0]
}

// withCell returns a table row with its cell i replaced.
func withCell(t *testing.T, line string, i int, cell string) string {
	t.Helper()
	cells := splitRow(line)
	if i >= len(cells) {
		t.Fatalf("row %q has %d cells", line, len(cells))
	}
	cells[i] = cell
	return "| " + strings.Join(cells, " | ") + " |"
}

// withCells returns a matrix row with its consumer and provider replaced.
func withCells(t *testing.T, line, consumer, provider string) string {
	t.Helper()
	if n := len(splitRow(line)); n != 5 {
		t.Fatalf("row %q has %d cells", line, n)
	}
	return withCell(t, withCell(t, line, 2, consumer), 3, provider)
}

// broken runs Check on the copy before and after break, and returns what
// break added. The copy's own problems, if the repository has any, are
// left out, so each test sees only the breach it made.
func broken(t *testing.T, breakIt func(m fstest.MapFS)) []Problem {
	t.Helper()
	m := repoCopy(t)
	before := map[string]int{}
	for _, p := range Check(m) {
		before[p.String()]++
	}
	breakIt(m)
	var added []Problem
	for _, p := range Check(m) {
		if before[p.String()] > 0 {
			before[p.String()]--
			continue
		}
		added = append(added, p)
	}
	return added
}

// once fails the test unless ps is one problem of the rule, in file, whose
// item and text hold each of names.
func once(t *testing.T, ps []Problem, rule, file string, names ...string) {
	t.Helper()
	if len(ps) != 1 {
		t.Fatalf("want one problem of rule %s, got %d: %v", rule, len(ps), ps)
	}
	p := ps[0]
	if p.Rule != rule || p.File != file {
		t.Fatalf("want rule %s in %s, got %v", rule, file, p)
	}
	for _, n := range names {
		if !strings.Contains(p.Item+" "+p.Text, n) {
			t.Errorf("%v does not name %q", p, n)
		}
	}
}

// none fails the test unless ps is empty.
func none(t *testing.T, ps []Problem) {
	t.Helper()
	for _, p := range ps {
		t.Errorf("unexpected: %v", p)
	}
}

// has fails the test unless one problem of ps is of the rule, in file, and
// names each of names.
func has(t *testing.T, ps []Problem, rule, file string, names ...string) {
	t.Helper()
	for _, p := range ps {
		if p.Rule != rule || p.File != file {
			continue
		}
		all := true
		for _, n := range names {
			all = all && strings.Contains(p.Item+" "+p.Text, n)
		}
		if all {
			return
		}
	}
	t.Errorf("no problem of rule %s in %s names %q: %v", rule, file, names, ps)
}

func TestRuleAEveryNeedOnceAndNothingElse(t *testing.T) {
	t.Run("a need with no row", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			edit(t, m, MatrixFile, row(t, m, "C-D1", "ledger.md")+"\n", "")
		}), "a", MatrixFile, "C-D1 (ledger.md)", "no row")
	})
	t.Run("a need with two rows", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			r := row(t, m, "V-D9", "deliver.md")
			edit(t, m, MatrixFile, r+"\n", r+"\n"+r+"\n")
		}), "a", MatrixFile, "V-D9 (deliver.md)", "2 rows")
	})
	t.Run("a row for no need", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			r := row(t, m, "C-D1", "ledger.md")
			edit(t, m, MatrixFile, r+"\n", r+"\n"+withCell(t, r, 0, "Q-Z9")+"\n")
		}), "a", MatrixFile, "Q-Z9 (ledger.md)", "not a need row")
	})
	t.Run("a design gains a need", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			addNeed(t, m, "ledger.md", "Q-Z9")
		}), "a", MatrixFile, "Q-Z9 (ledger.md)", "no row")
	})
	t.Run("a design lists a need twice", func(t *testing.T) {
		design := path.Join(DesignDir, "ledger.md")
		once(t, broken(t, func(m fstest.MapFS) {
			ids := parseDesignNeeds(string(m[design].Data))
			if len(ids) == 0 {
				t.Fatal("ledger.md has no need")
			}
			addNeed(t, m, "ledger.md", ids[0])
		}), "a", design, "(ledger.md)", "2 need rows")
	})
	t.Run("a row with a cell missing", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			r := row(t, m, "S-L1", "store.md")
			cells := splitRow(r)
			edit(t, m, MatrixFile, r, "| "+strings.Join(cells[:4], " | ")+" |")
		}), "a", MatrixFile, "S-L1 (store.md)", "4 cells")
	})
	t.Run("no table of needs", func(t *testing.T) {
		ps := broken(t, func(m fstest.MapFS) {
			edit(t, m, MatrixFile, "| Need | Design | Consumer | Provider | Where |", "Need, Design, Consumer, Provider, Where")
		})
		has(t, ps, "a", MatrixFile, "the matrix", "has no table headed")
	})
	t.Run("the table of needs with its columns moved", func(t *testing.T) {
		ps := broken(t, func(m fstest.MapFS) {
			edit(t, m, MatrixFile, "| Need | Design | Consumer | Provider | Where |", "| Need | Design | Provider | Consumer | Where |")
		})
		has(t, ps, "a", MatrixFile, "Need | Design | Provider | Consumer | Where", "is under")
		has(t, ps, "a", MatrixFile, "the matrix", "has no table headed")
	})
}

// What renders as code, a quote, or a table that is no contracts table is
// never read as a need, a matrix row, a plan id or a label.
func TestExamplesAreNotRead(t *testing.T) {
	fenced := "\n```\n| Id | With | Direction | One line |\n| --- | --- | --- | --- |\n| Q-Z9 | store | need | an example |\n```\n"
	for name, text := range map[string]string{
		"a fenced table":          fenced,
		"a tilde fence":           strings.ReplaceAll(fenced, "```", "~~~~"),
		"an indented table":       "\n    | Id | With | Direction | One line |\n    | --- | --- | --- | --- |\n    | Q-Z9 | store | need | an example |\n",
		"a quoted table":          "\n> | Id | With | Direction | One line |\n> | --- | --- | --- | --- |\n> | Q-Z9 | store | need | an example |\n",
		"another table":           "\n| Area | Owner | Needs |\n| --- | --- | --- |\n| store | ST-05 | needs review |\n",
		"a fenced heading":        "\n```\n## 99. Not a heading\n```\n",
		"a heading in a code run": "\n    ## 99. Not a heading\n",
	} {
		t.Run("a design's "+name, func(t *testing.T) {
			none(t, broken(t, func(m fstest.MapFS) {
				insertAfter(t, m, path.Join(DesignDir, "ledger.md"), contractsHeading(t, m, "ledger.md"), text)
			}))
		})
	}
	t.Run("a fenced copy of a matrix row", func(t *testing.T) {
		none(t, broken(t, func(m fstest.MapFS) {
			r := row(t, m, "V-D9", "deliver.md")
			insertAfter(t, m, MatrixFile, "## Needs", "\n```\n| Need | Design | Consumer | Provider | Where |\n| --- | --- | --- | --- | --- |\n"+r+"\n```\n")
		}))
	})
	t.Run("a matrix row moved into a fence", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			r := row(t, m, "V-D9", "deliver.md")
			edit(t, m, MatrixFile, r+"\n", "")
			insertAfter(t, m, MatrixFile, "## Needs", "\n```\n| Need | Design | Consumer | Provider | Where |\n| --- | --- | --- | --- | --- |\n"+r+"\n```\n")
		}), "a", MatrixFile, "V-D9 (deliver.md)", "no row")
	})
	for name, text := range map[string]string{
		"a fenced Id table": "\n```\n| Id | Package |\n| --- | --- |\n| ZZ-77 | an example |\n```\n",
		"a quoted Id table": "\n> | Id | Package |\n> | --- | --- |\n> | ZZ-77 | an example |\n",
		"another table":     "\n| Track | Package |\n| --- | --- |\n| ZZ-77 | an example |\n",
	} {
		t.Run("the plan's "+name, func(t *testing.T) {
			ps := broken(t, func(m fstest.MapFS) {
				insertAfter(t, m, PlanFile, "### Stage 1", text)
				plan, err := parsePlan(string(m[PlanFile].Data))
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := plan["ZZ-77"]; ok {
					t.Error("ZZ-77 is read as a plan id")
				}
				addCard(m, "ZZ-77.md", "# ZZ-77: a made-up card\n")
			})
			once(t, ps, "d", path.Join(CardsDir, "ZZ-77.md"), "ZZ-77")
		})
	}
	// The same table, plain, does make a plan id, so the cases above test
	// the fence and the header, not the card.
	t.Run("the plan's Id table", func(t *testing.T) {
		none(t, broken(t, func(m fstest.MapFS) {
			insertAfter(t, m, PlanFile, "### Stage 1", "\n| Id | Package |\n| --- | --- |\n| ZZ-77 | an example |\n")
			addCard(m, "ZZ-77.md", "# ZZ-77: a made-up card\n")
		}))
	})
	for name, label := range map[string]string{
		"fenced":   "```\n**Depends on:** ZZ-99\n```",
		"indented": "    **Depends on:** ZZ-99",
		"quoted":   "> **Depends on:** ZZ-99",
		"in code":  "A `**Depends on:** ZZ-99` label.",
	} {
		t.Run("a card's "+name+" label", func(t *testing.T) {
			none(t, broken(t, func(m fstest.MapFS) {
				addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card ("+freePlanID(t, m, 0)+")\n\n"+label+"\n")
			}))
		})
	}
}

func TestRuleBIdsAndWords(t *testing.T) {
	for _, c := range []struct{ name, consumer, provider, says string }{
		{"a consumer that is no plan id", "LG-03", "DV-12", `consumer "LG-03"`},
		{"a provider that is no plan id", "LG-21", "LG-21, ST-13", `provider "ST-13"`},
		{"a provider that is not one of the words", "LG-21", "maybe", `provider "maybe"`},
		{"a word with an id", "LG-21", "v1, DV-12", `provider "v1"`},
		{"no consumer", "", "DV-12", "no consumer"},
		{"no provider", "LG-21", "", "no provider"},
	} {
		t.Run(c.name, func(t *testing.T) {
			once(t, broken(t, func(m fstest.MapFS) {
				r := row(t, m, "C-D1", "ledger.md")
				edit(t, m, MatrixFile, r, withCells(t, r, c.consumer, c.provider))
			}), "b", MatrixFile, "C-D1 (ledger.md)", c.says)
		})
	}
	t.Run("nowhere", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			r := row(t, m, "C-D1", "ledger.md")
			edit(t, m, MatrixFile, r, withCell(t, r, 4, ""))
		}), "b", MatrixFile, "C-D1 (ledger.md)", "nowhere")
	})
}

func TestRuleCNoProviderAfterItsConsumer(t *testing.T) {
	once(t, broken(t, func(m fstest.MapFS) {
		r := row(t, m, "C-D1", "ledger.md")
		edit(t, m, MatrixFile, r, withCells(t, r, "LG-21", "DV-12, DL-16"))
	}), "c", MatrixFile, "C-D1 (ledger.md)", "DL-16 is in Stage 3", "LG-21 in Stage 2")
	// The same stage, and an earlier one, are allowed.
	none(t, broken(t, func(m fstest.MapFS) {
		r := row(t, m, "C-D1", "ledger.md")
		edit(t, m, MatrixFile, r, withCells(t, r, "LG-21, DL-16", "DV-12, ST-05, SEAM-0"))
	}))
}

func TestRuleDEveryCardNamesAPlanPackage(t *testing.T) {
	t.Run("a card with no plan id", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card\n")
		}), "d", path.Join(CardsDir, "ZZ-1.md"), "ZZ-1")
	})
	t.Run("a card whose brackets hold no plan id", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card (ZZ-2, LG-03)\n")
		}), "d", path.Join(CardsDir, "ZZ-1.md"), "ZZ-1")
	})
	t.Run("a card whose plan id is only in code", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			addCard(m, "ZZ-1.md", "```\n# ZZ-1: an example ("+freePlanID(t, m, 0)+")\n```\n# ZZ-1: a made-up card\n")
		}), "d", path.Join(CardsDir, "ZZ-1.md"), "ZZ-1")
	})
	t.Run("a card named by its plan id in brackets", func(t *testing.T) {
		none(t, broken(t, func(m fstest.MapFS) {
			addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card ("+freePlanID(t, m, 0)+")\n")
		}))
	})
	t.Run("a card named by its file", func(t *testing.T) {
		none(t, broken(t, func(m fstest.MapFS) {
			addCard(m, freePlanID(t, m, 0)+"b.md", "# a made-up card\n")
		}))
	})
	t.Run("a file that is no Markdown", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			addCard(m, "ZZ-1.txt", "# ZZ-1: a made-up card\n")
		}), "d", path.Join(CardsDir, "ZZ-1.txt"), "no Markdown")
	})
	t.Run("hidden files", func(t *testing.T) {
		none(t, broken(t, func(m fstest.MapFS) {
			addCard(m, ".DS_Store", "\x00\x00\x00\x01Bud1")
			addCard(m, ".ZZ-1.md.swp", "b0VIM")
		}))
	})
}

func TestRuleEDependsOnPlanIdsAndCards(t *testing.T) {
	file := path.Join(CardsDir, "ZZ-1.md")
	once(t, broken(t, func(m fstest.MapFS) {
		addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card ("+freePlanID(t, m, 0)+")\n\n**Plan:** none. **Depends on:** SEAM-0a, LG-03, c and d. **Part of:** nothing.\n")
	}), "e", file, "ZZ-1", "LG-03")
	t.Run("after a bold word", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card ("+freePlanID(t, m, 0)+")\n\n**Depends on:** SEAM-0 (**merged**), ZZ-99, c and d. **Part of:** nothing.\n")
		}), "e", file, "ZZ-1", "ZZ-99")
	})
	t.Run("on the paragraph's next line", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card ("+freePlanID(t, m, 0)+")\n\n**Depends on:** SEAM-0 and\nZZ-99.\n")
		}), "e", file, "ZZ-1", "ZZ-99")
	})
	t.Run("a card's id and a plan id", func(t *testing.T) {
		none(t, broken(t, func(m fstest.MapFS) {
			addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card ("+freePlanID(t, m, 0)+")\n\n**Depends on:** "+anyCard(t, m)+", DL-26, c and d.\n")
		}))
	})
}

func TestRuleFACardFailsOnANeedWithNoProvider(t *testing.T) {
	file := path.Join(CardsDir, "ZZ-1.md")
	for _, word := range []string{"open", "later", "dropped"} {
		t.Run(word, func(t *testing.T) {
			once(t, broken(t, func(m fstest.MapFS) {
				pkg := freePlanID(t, m, 2)
				addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card ("+pkg+")\n")
				r := row(t, m, "C-D1", "ledger.md")
				edit(t, m, MatrixFile, r, withCells(t, r, pkg, word))
			}), "f", file, "ZZ-1", "C-D1 (ledger.md)", "provider is "+word)
		})
	}
	// A card whose package is a consumer of a row with a provider passes.
	none(t, broken(t, func(m fstest.MapFS) {
		pkg := freePlanID(t, m, 2)
		addCard(m, "ZZ-1.md", "# ZZ-1: a made-up card ("+pkg+")\n")
		r := row(t, m, "C-D1", "ledger.md")
		edit(t, m, MatrixFile, r, withCells(t, r, "LG-21, "+pkg, "DV-12"))
	}))
}

func TestRuleGEveryFakeExistsOrWaits(t *testing.T) {
	for _, c := range []struct{ name, fake, says string }{
		{"waits on no package", "`waits: ZZ-1`", "waits: ZZ-1"},
		{"no such folder", "`internal/ledger/nowhere`", "internal/ledger/nowhere"},
		{"a path out of the root", "`../internal/ledger`", "../internal/ledger"},
		{"the root", "`.`", `"."`},
		{"open where the card names a fake", "`open`", "ST-05, LG-22, LG-23"},
	} {
		t.Run(c.name, func(t *testing.T) {
			once(t, broken(t, func(m fstest.MapFS) {
				r := fakeLine(t, m, "LG-10")
				edit(t, m, MatrixFile, r, withCell(t, r, 3, c.fake))
			}), "g", MatrixFile, "reader", c.says)
		})
	}
	t.Run("open for a package the card lets be", func(t *testing.T) {
		none(t, broken(t, func(m fstest.MapFS) {
			r := fakeLine(t, m, "ST-05")
			edit(t, m, MatrixFile, r, withCell(t, r, 3, "`open`"))
		}))
	})
	t.Run("a folder that exists", func(t *testing.T) {
		none(t, broken(t, func(m fstest.MapFS) {
			m["internal/ledger/ledgertest/fake.go"] = &fstest.MapFile{Data: []byte("package ledgertest\n")}
			r := fakeLine(t, m, "LG-10")
			edit(t, m, MatrixFile, r, withCell(t, r, 3, "`internal/ledger/ledgertest`"))
		}))
	})
	t.Run("a file is no folder", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			m["internal/ledger/ledgertest/fake.go"] = &fstest.MapFile{Data: []byte("package ledgertest\n")}
			r := fakeLine(t, m, "LG-10")
			edit(t, m, MatrixFile, r, withCell(t, r, 3, "`internal/ledger/ledgertest/fake.go`"))
		}), "g", MatrixFile, "reader")
	})
	t.Run("an interface the card lists with no row", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			edit(t, m, MatrixFile, fakeLine(t, m, "LG-10")+"\n", "")
		}), "g", MatrixFile, "LG-10")
	})
	t.Run("no table of interfaces", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			text := string(m[MatrixFile].Data)
			i := strings.Index(text, "| Interface | Go name | Provider | Fake |")
			if i < 0 {
				t.Fatal("the matrix has no table of interfaces")
			}
			end := strings.Index(text[i:], "\n\n")
			if end < 0 {
				end = len(text) - i
			}
			m[MatrixFile].Data = []byte(text[:i] + text[i+end:])
		}), "g", MatrixFile, "the matrix", "has no table headed")
	})
	t.Run("the table of interfaces with its columns moved", func(t *testing.T) {
		ps := broken(t, func(m fstest.MapFS) {
			edit(t, m, MatrixFile, "| Interface | Go name | Provider | Fake |", "| Interface | Go name | Fake | Provider |")
		})
		if len(ps) != 2 {
			t.Fatalf("want two problems, got %v", ps)
		}
		has(t, ps, "g", MatrixFile, "Interface | Go name | Fake | Provider", "is under")
		has(t, ps, "g", MatrixFile, "the matrix", "has no table headed")
	})
}

// A missing source is a problem, not a pass.
func TestCheckFailsWithNothingToRead(t *testing.T) {
	for _, file := range []string{PlanFile, MatrixFile} {
		m := repoCopy(t)
		delete(m, file)
		if ps := Check(m); len(ps) == 0 {
			t.Errorf("Check passes with no %s", file)
		}
	}
	if ps := Check(fstest.MapFS{}); len(ps) == 0 {
		t.Error("Check passes on an empty folder")
	}
}

// Nothing built into eva-md imports this package: no Go file outside it,
// tests aside, names it, and go list does not list it for the binary.
func TestNothingImportsThePackage(t *testing.T) {
	dir := root(t)
	checked := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != dir && (strings.HasPrefix(name, ".") || name == "testdata" || name == "bin") {
				return filepath.SkipDir
			}
			if rel, _ := filepath.Rel(dir, p); filepath.ToSlash(rel) == "internal/cardcheck" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		for _, spec := range f.Imports {
			if path, _ := strconv.Unquote(spec.Path.Value); path == importPath {
				t.Errorf("%s imports %s", p, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 100 {
		t.Fatalf("only %d Go files were checked", checked)
	}

	deps := goList(t, dir, "list", "-deps", "./cmd/eva-md")
	if !slices.Contains(deps, "github.com/heyNag/eva-md/cmd/eva-md") || !slices.Contains(deps, "github.com/heyNag/eva-md/internal/hook") {
		t.Fatalf("go list -deps ./cmd/eva-md did not list the binary's packages: %q", deps)
	}
	if slices.Contains(deps, importPath) {
		t.Errorf("go list -deps ./cmd/eva-md lists %s", importPath)
	}
}

// goList runs the go command in dir with a home of its own, with Go's
// telemetry off there, and Go's own caches, and returns its output's
// lines.
func goList(t *testing.T, dir string, args ...string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	goTool, err := exec.LookPath("go")
	if err != nil {
		goTool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	cmd := exec.CommandContext(ctx, goTool, "env", "GOCACHE", "GOMODCACHE")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env: %v", err)
	}
	caches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(caches) != 2 {
		t.Fatalf("go env gave the caches as %q", out)
	}
	home := t.TempDir()
	config := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	telemetry := filepath.Join(config, "go", "telemetry")
	if err := os.MkdirAll(telemetry, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(telemetry, "mode"), []byte("off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.CommandContext(ctx, goTool, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "GOENV=off",
		"GOCACHE="+caches[0], "GOMODCACHE="+caches[1], "GOPROXY=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}
