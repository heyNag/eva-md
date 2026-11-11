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
	for k := range needs {
		designs[k.design]++
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
		if !needs[k] {
			t.Errorf("%s is not read as a need", k)
		}
	}
	for _, k := range []needKey{{"C-D1", "derive.md"}, {"C-S2", "ledger.md"}, {"S-D1", "store.md"}} {
		if needs[k] {
			t.Errorf("%s, a promise, is read as a need", k)
		}
	}

	cards, err := readCards(fsys, plan)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root(t), filepath.FromSlash(CardsDir)))
	if err != nil {
		t.Fatal(err)
	}
	var want, got []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "README.md" {
			want = append(want, path.Join(CardsDir, e.Name()))
		}
	}
	for _, c := range cards {
		got = append(got, c.file)
	}
	slices.Sort(want)
	if len(want) == 0 || !slices.Equal(got, want) {
		t.Errorf("cards read %q, want %q", got, want)
	}

	m := parseMatrix(readText(t, fsys, MatrixFile))
	if len(m.needs) < 80 || len(m.fakes) < 7 {
		t.Errorf("the matrix has %d need rows and %d fakes", len(m.needs), len(m.fakes))
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

// edit replaces old, which must be in the file once, with new.
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

// withCells returns a matrix row with its consumer and provider replaced.
func withCells(t *testing.T, line, consumer, provider string) string {
	t.Helper()
	cells := splitRow(line)
	if len(cells) != 5 {
		t.Fatalf("row %q has %d cells", line, len(cells))
	}
	cells[2], cells[3] = consumer, provider
	return "| " + strings.Join(cells, " | ") + " |"
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
	t.Run("a row for a promise", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			r := row(t, m, "C-D2", "derive.md")
			edit(t, m, MatrixFile, r+"\n", r+"\n"+strings.Replace(r, "| derive.md |", "| ledger.md |", 1)+"\n")
		}), "a", MatrixFile, "C-D2 (ledger.md)", "not a need row")
	})
	t.Run("a design gains a need", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			edit(t, m, path.Join(DesignDir, "ledger.md"), "| C-D2 | derive | promise |", "| C-D2 | derive | need |")
		}), "a", MatrixFile, "C-D2 (ledger.md)", "no row")
	})
	t.Run("a row with a cell missing", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			r := row(t, m, "S-L1", "store.md")
			cells := splitRow(r)
			edit(t, m, MatrixFile, r, "| "+strings.Join(cells[:4], " | ")+" |")
		}), "a", MatrixFile, "S-L1 (store.md)", "4 cells")
	})
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
}

func TestRuleCNoProviderAfterItsConsumer(t *testing.T) {
	once(t, broken(t, func(m fstest.MapFS) {
		r := row(t, m, "C-D1", "ledger.md")
		edit(t, m, MatrixFile, r, withCells(t, r, "LG-21", "DV-12, DL-16"))
	}), "c", MatrixFile, "C-D1 (ledger.md)", "DL-16 is in Stage 3", "LG-21 in Stage 2")
	// The same stage, and an earlier one, are allowed.
	if ps := broken(t, func(m fstest.MapFS) {
		r := row(t, m, "C-D1", "ledger.md")
		edit(t, m, MatrixFile, r, withCells(t, r, "LG-21, DL-16", "DV-12, ST-05, SEAM-0"))
	}); len(ps) != 0 {
		t.Errorf("got %v", ps)
	}
}

func TestRuleDEveryCardNamesAPlanPackage(t *testing.T) {
	t.Run("a title with its id dropped", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			edit(t, m, path.Join(CardsDir, "CT-3.md"), "# CT-3: capture checked against the host's own record (LG-02)", "# CT-3: capture checked against the host's own record")
		}), "d", path.Join(CardsDir, "CT-3.md"), "CT-3")
	})
	t.Run("a new card", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			m[path.Join(CardsDir, "ZZ-1.md")] = &fstest.MapFile{Data: []byte("# ZZ-1: a made-up card (ZZ-2, LG-03)\n")}
		}), "d", path.Join(CardsDir, "ZZ-1.md"), "ZZ-1")
	})
	t.Run("a card named by its plan id in brackets", func(t *testing.T) {
		if ps := broken(t, func(m fstest.MapFS) {
			m[path.Join(CardsDir, "ZZ-1.md")] = &fstest.MapFile{Data: []byte("# ZZ-1: a made-up card (EV-12)\n")}
		}); len(ps) != 0 {
			t.Errorf("got %v", ps)
		}
	})
}

func TestRuleEDependsOnPlanIdsAndCards(t *testing.T) {
	file := path.Join(CardsDir, "IF-01a.md")
	once(t, broken(t, func(m fstest.MapFS) {
		edit(t, m, file, "**Depends on:** SEAM-0a, c and d,", "**Depends on:** SEAM-0a, LG-03, c and d,")
	}), "e", file, "IF-01a", "LG-03")
	// A card's id and a plan id are both allowed.
	if ps := broken(t, func(m fstest.MapFS) {
		edit(t, m, file, "**Depends on:** SEAM-0a, c and d,", "**Depends on:** SEAM-0a, CT-3b, DL-26, c and d,")
	}); len(ps) != 0 {
		t.Errorf("got %v", ps)
	}
}

func TestRuleFACardFailsOnANeedWithNoProvider(t *testing.T) {
	for _, word := range []string{"open", "later", "dropped"} {
		t.Run(word, func(t *testing.T) {
			once(t, broken(t, func(m fstest.MapFS) {
				r := row(t, m, "C-D1", "ledger.md")
				edit(t, m, MatrixFile, r, withCells(t, r, "LG-11", word))
			}), "f", path.Join(CardsDir, "LG-11.md"), "LG-11", "C-D1 (ledger.md)", "provider is "+word)
		})
	}
	// A card whose package is a consumer of a row with a provider passes.
	if ps := broken(t, func(m fstest.MapFS) {
		r := row(t, m, "C-D1", "ledger.md")
		edit(t, m, MatrixFile, r, withCells(t, r, "LG-21, LG-11", "DV-12"))
	}); len(ps) != 0 {
		t.Errorf("got %v", ps)
	}
}

func TestRuleGEveryFakeExistsOrWaits(t *testing.T) {
	for _, c := range []struct{ name, fake, says string }{
		{"waits on no package", "`waits: ZZ-1`", "waits: ZZ-1"},
		{"no such folder", "`internal/ledger/nowhere`", "internal/ledger/nowhere"},
		{"a path out of the root", "`../internal/ledger`", "../internal/ledger"},
	} {
		t.Run(c.name, func(t *testing.T) {
			once(t, broken(t, func(m fstest.MapFS) {
				edit(t, m, MatrixFile, "| The reader | `ledger.Reader` | LG-10 | `waits: IF-01b` |", "| The reader | `ledger.Reader` | LG-10 | "+c.fake+" |")
			}), "g", MatrixFile, "The reader", c.says)
		})
	}
	t.Run("a folder that exists", func(t *testing.T) {
		if ps := broken(t, func(m fstest.MapFS) {
			m["internal/ledger/ledgertest/fake.go"] = &fstest.MapFile{Data: []byte("package ledgertest\n")}
			edit(t, m, MatrixFile, "| The reader | `ledger.Reader` | LG-10 | `waits: IF-01b` |", "| The reader | `ledger.Reader` | LG-10 | `internal/ledger/ledgertest` |")
		}); len(ps) != 0 {
			t.Errorf("got %v", ps)
		}
	})
	t.Run("a file is no folder", func(t *testing.T) {
		once(t, broken(t, func(m fstest.MapFS) {
			m["internal/ledger/ledgertest/fake.go"] = &fstest.MapFile{Data: []byte("package ledgertest\n")}
			edit(t, m, MatrixFile, "| The reader | `ledger.Reader` | LG-10 | `waits: IF-01b` |", "| The reader | `ledger.Reader` | LG-10 | `internal/ledger/ledgertest/fake.go` |")
		}), "g", MatrixFile, "The reader")
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
