package main

import (
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/testgitenv"
)

func TestMain(m *testing.M) {
	testgitenv.Clear()
	os.Exit(m.Run())
}

// isolate gives the test a temporary home whose git config is a temporary
// file with a made-up identity.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TEMPLATE_DIR"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte("[user]\nname = Eva Fixture\nemail = fixture@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const catalogueJSON = `{"v":1,"metrics":[{"name":"hook.capture_p95_ms","unit":"ms","direction":"lower","paths":["internal/hook"]},` +
	`{"name":"fidelity.fact_recall","unit":"ratio","direction":"higher","paths":["internal/update"]}]}`

const gatesJSON = `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","stat":"p95","rule":"not_worse","margin":0.1,"relative":true},` +
	`{"metric":"fidelity.fact_recall","stat":"mean","rule":"not_worse","margin":0.05}]}`

// newRepo makes a repository with the catalogue and the gates where the
// commands look for them, and returns it and its commit.
func newRepo(t *testing.T) (string, string) {
	t.Helper()
	isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	write(t, filepath.Join(repo, "spec", "metrics.json"), catalogueJSON)
	write(t, filepath.Join(repo, "eval", "gates.json"), gatesJSON)
	write(t, filepath.Join(repo, "internal", "hook", "hook.go"), "package hook\n")
	write(t, filepath.Join(repo, "internal", "update", "update.go"), "package update\n")
	gitIn(t, repo, "init", "--quiet")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "first")
	return repo, gitIn(t, repo, "rev-parse", "HEAD")
}

// rowsOf appends rows of both metrics, measured at commit, to a new
// results folder and returns it.
func rowsOf(t *testing.T, commit string, dirty bool, latency []float64, recall float64) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "run")
	var rows []results.Row
	for i, v := range latency {
		rows = append(rows, results.Row{Metric: "hook.capture_p95_ms", Value: v, Unit: "ms", Commit: commit, Dirty: dirty,
			Instrument: "latency", Scenario: "lat." + strconv.Itoa(i), Seed: 1, Time: time0})
	}
	rows = append(rows, results.Row{Metric: "fidelity.fact_recall", Value: recall, Unit: "ratio", Commit: commit, Dirty: dirty,
		Instrument: "fidelity", Scenario: "e.reversal.0001", Host: "codex", HostVersion: "0.167.0", Model: "made-up-model", Seed: 1, Time: time0})
	if err := (results.Store{Dir: dir}).Append(rows...); err != nil {
		t.Fatal(err)
	}
	return dir
}

var time0 = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

func cmd(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// The three commands end to end, on a repository of their own: accept a
// baseline, gate a set of rows against it, then flip the gate by doctoring
// one row of the rows file, in both directions.
func TestAcceptGateAndADoctoredRow(t *testing.T) {
	repo, commit := newRepo(t)
	base := rowsOf(t, commit, false, []float64{30, 32, 34, 36, 40}, 0.9)
	code, out, errOut := cmd("baseline", "accept", "--repo", repo, base)
	if code != 0 || !strings.HasPrefix(out, "Accepted 6 rows of commit "+commit+" as the baseline in ") || errOut != "" {
		t.Fatalf("accept: %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(repo, "eval", "baselines", "baseline.json")); err != nil {
		t.Fatal(err)
	}

	runDir := rowsOf(t, commit, true, []float64{30, 32, 34, 36, 44}, 0.875)
	code, out, errOut = cmd("gate", "--repo", repo, runDir)
	want := []string{
		"rows of commit " + commit[:12] + " on a dirty tree, against the baseline of commit " + commit[:12] + ", which must still describe " + commit[:12],
		"hold hook.capture_p95_ms p95: 44 ms against the baseline's 40; may be worse (lower is better) by at most 10% of the baseline's figure",
		"hold fidelity.fact_recall mean: 0.875 ratio against the baseline's 0.9; may be worse (higher is better) by at most 0.05 ratio",
	}
	if code != 0 || out != strings.Join(want, "\n")+"\n" || errOut != "" {
		t.Fatalf("gate: %d\n%s%s", code, out, errOut)
	}

	file := filepath.Join(runDir, results.FileName)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	doctor := func(from, to string) {
		t.Helper()
		data = bytes.Replace(data, []byte(from), []byte(to), 1)
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	doctor(`"value":44,`, `"value":45,`)
	code, out, _ = cmd("gate", "--repo", repo, runDir)
	if lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n"); code != 1 || len(lines) != 3 || lines[0] != want[0] ||
		lines[1] != "FAIL hook.capture_p95_ms p95: 45 ms against the baseline's 40; may be worse (lower is better) by at most 10% of the baseline's figure" ||
		!strings.HasPrefix(lines[2], "hold ") {
		t.Fatalf("one row raised: %d\n%s", code, out)
	}
	doctor(`"value":45,`, `"value":44,`)
	doctor(`"value":0.875,`, `"value":0.84,`)
	if code, out, _ = cmd("gate", "--repo", repo, runDir); code != 1 || !strings.Contains(out, "FAIL fidelity.fact_recall mean: 0.84 ratio") {
		t.Fatalf("recall lowered: %d\n%s", code, out)
	}
	doctor(`"value":0.84,`, `"value":0.86,`)
	if code, out, _ = cmd("gate", "--repo", repo, runDir); code != 0 {
		t.Fatalf("recall raised back inside the margin: %d\n%s", code, out)
	}

	// A branch that changes a covered path is judged against the baseline,
	// which its own change never makes stale: by default the base is where
	// the branch leaves main.
	gitIn(t, repo, "switch", "--quiet", "-c", "faster-hook")
	write(t, filepath.Join(repo, "internal", "hook", "capture.go"), "package hook\n\nvar x = 1\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "change the hook")
	branch := gitIn(t, repo, "rev-parse", "HEAD")
	faster := rowsOf(t, branch, false, []float64{30, 32, 34, 36, 42}, 0.9)
	code, out, _ = cmd("gate", "--repo", repo, faster)
	if lines := strings.Split(out, "\n"); code != 0 || lines[0] != "rows of commit "+branch[:12]+" on a clean tree, against the baseline of commit "+commit[:12]+", which must still describe "+commit[:12] {
		t.Fatalf("the branch: %d\n%s", code, out)
	}
	slower := rowsOf(t, branch, false, []float64{30, 32, 34, 36, 45}, 0.9)
	if code, out, _ = cmd("gate", "--repo", repo, slower); code != 1 || !strings.Contains(out, "FAIL hook.capture_p95_ms p95: 45 ms") {
		t.Fatalf("the branch, slower: %d\n%s", code, out)
	}

	// Once main changes a covered path after the baseline, the baseline is
	// stale for anything that starts there: every gate of that metric
	// fails and says so, until a new baseline is accepted.
	gitIn(t, repo, "switch", "--quiet", "main")
	gitIn(t, repo, "merge", "--quiet", "--ff-only", "faster-hook")
	code, out, _ = cmd("gate", "--repo", repo, faster)
	if code != 1 || !strings.Contains(out, "FAIL hook.capture_p95_ms p95: the baseline is stale: internal/hook changed between its commit "+commit[:12]+" and "+branch[:12]+"; accept a new baseline") ||
		!strings.Contains(out, "hold fidelity.fact_recall") {
		t.Fatalf("stale: %d\n%s", code, out)
	}
	if code, out, _ = cmd("gate", "--repo", repo, "--base", commit, faster); code != 0 {
		t.Fatalf("with --base at the baseline's commit: %d\n%s", code, out)
	}
	if code, out, errOut = cmd("baseline", "accept", "--repo", repo, faster); code != 0 {
		t.Fatalf("accept on main: %d %s %s", code, out, errOut)
	}
	if code, out, _ = cmd("gate", "--repo", repo, faster); code != 0 {
		t.Fatalf("against the new baseline: %d\n%s", code, out)
	}

	// Rows whose commit the repository lacks have no start to find: the
	// gate cannot run without --base.
	missing := rowsOf(t, "0123456789abcdef0123456789abcdef01234567", false, []float64{30}, 0.9)
	if code, out, errOut = cmd("gate", "--repo", repo, missing); code != 2 || out != "" || !strings.Contains(errOut, "cannot find the commit the change starts from") || !strings.Contains(errOut, "--base") {
		t.Fatalf("a missing commit: %d %q %q", code, out, errOut)
	}
}

func TestAcceptRefusesADirtyTree(t *testing.T) {
	repo, commit := newRepo(t)
	dirty := rowsOf(t, commit, true, []float64{30}, 0.9)
	code, out, errOut := cmd("baseline", "accept", "--repo", repo, dirty)
	if code != 2 || out != "" || !strings.Contains(errOut, "dirty tree") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(repo, "eval", "baselines")); !os.IsNotExist(err) {
		t.Fatalf("a baseline was written: %v", err)
	}
}

func TestDiffCommand(t *testing.T) {
	repo, commit := newRepo(t)
	first := rowsOf(t, commit, false, []float64{10, 20}, 0.5)
	second := rowsOf(t, commit, false, []float64{12, 22}, 0.75)
	code, out, errOut := cmd("diff", "--catalogue", filepath.Join(repo, "spec", "metrics.json"), first, second)
	if code != 0 || errOut != "" {
		t.Fatalf("%d %q", code, errOut)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "metric ") ||
		!slices.Equal(strings.Fields(lines[1]), strings.Fields("fidelity.fact_recall fidelity on codex with made-up-model ratio 1 0.5 0.5 0.5 1 0.75 0.75 0.75 better by 0.25")) ||
		!slices.Equal(strings.Fields(lines[2]), strings.Fields("hook.capture_p95_ms latency ms 2 15 15 20 2 17 17 22 worse by 2")) {
		t.Fatalf("%s", out)
	}
}

// Every way a command cannot run exits 2 and says why on stderr.
func TestUsageAndDataErrors(t *testing.T) {
	repo, commit := newRepo(t)
	rows := rowsOf(t, commit, false, []float64{30}, 0.9)
	empty := t.TempDir()
	cases := map[string][]string{
		"no command":         nil,
		"an unknown command": {"score"},
		"baseline alone":     {"baseline"},
		"no rows to gate":    {"gate", "--repo", repo},
		"two sets to gate":   {"gate", "--repo", repo, rows, rows},
		"one set to diff":    {"diff", "--catalogue", filepath.Join(repo, "spec", "metrics.json"), rows},
		"an unknown flag":    {"gate", "--fast", rows},
		"no baseline":        {"gate", "--repo", repo, rows},
		"no catalogue":       {"gate", "--repo", empty, rows},
		"missing rows":       {"diff", "--catalogue", filepath.Join(repo, "spec", "metrics.json"), rows, filepath.Join(empty, "none")},
		"no rows to accept":  {"baseline", "accept", "--repo", repo},
	}
	for name, args := range cases {
		code, out, errOut := cmd(args...)
		if code != 2 || out != "" || errOut == "" {
			t.Errorf("%s: %d %q %q", name, code, out, errOut)
		}
	}
}

// moduleRoot is the repository's root.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	return root
}

// deps returns the module's packages the package at dir imports, directly
// or not, reading every non-test Go file whatever its build constraints,
// so it lists at least what any build of the package holds.
func deps(t *testing.T, root, dir string) []string {
	t.Helper()
	const module = "github.com/heyNag/eva-md/"
	seen := map[string]bool{}
	var walk func(rel string)
	walk = func(rel string) {
		files, err := filepath.Glob(filepath.Join(root, rel, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no Go files: %v", rel, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly|parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range parsed.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if dep, ok := strings.CutPrefix(p, module); ok && !seen[dep] {
					seen[dep] = true
					walk(dep)
				}
			}
		}
	}
	walk(dir)
	var out []string
	for p := range seen {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// No release contains the harness: eva-md imports no package of it,
// directly or not, and no workflow or script names the command, but for
// the two lines of CI's guards job that run its paths and size.
func TestNoReleaseContainsIt(t *testing.T) {
	root := moduleRoot(t)
	product := deps(t, root, "cmd/eva-md")
	if !slices.Contains(product, "internal/hook") || !slices.Contains(product, "internal/update") {
		t.Fatalf("eva-md's packages are %v; the walk read the wrong files", product)
	}
	for _, p := range product {
		if p == "internal/eval" || strings.HasPrefix(p, "internal/eval/") || strings.HasPrefix(p, "cmd/") {
			t.Errorf("eva-md imports %s", p)
		}
	}
	harness := deps(t, root, "cmd/eva-eval")
	if !slices.Contains(harness, "internal/eval/results") {
		t.Fatalf("the harness's packages are %v", harness)
	}
	files, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	scripts, _ := filepath.Glob(filepath.Join(root, "scripts", "*"))
	if len(files) < 3 || len(scripts) < 3 {
		t.Fatalf("found %v and %v; the check read the wrong folders", files, scripts)
	}
	ci := filepath.Join(root, ".github", "workflows", "ci.yml")
	for _, path := range append(files, scripts...) {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if m := namesHarness(string(data), path == ci); m != "" {
			t.Errorf("%s names %s", path, m)
		}
	}
}

var (
	harnessName = regexp.MustCompile(`eva-eval|cmd/eva-eval|internal/eval\b`)
	guardsRun   = regexp.MustCompile(`go run \./cmd/eva-eval (paths|size)\b`)
)

// namesHarness returns the first name of the harness a workflow's or
// script's text holds, or "". ci is true for CI's own workflow, which no
// release runs, whose guards job alone may go run the harness's paths and
// size.
func namesHarness(text string, ci bool) string {
	if ci {
		text = guardsRun.ReplaceAllString(text, "")
	}
	return harnessName.FindString(text)
}

// The guards' exception holds in CI's own workflow alone, and only for
// the guards' two commands.
func TestOnlyCINamesTheGuards(t *testing.T) {
	cases := []struct {
		text  string
		ci    bool
		names bool
	}{
		{"      - run: go run ./cmd/eva-eval paths \"$BASE\" \"$SHA\"\n      - run: go run ./cmd/eva-eval size\n", true, false},
		{"      - run: go run ./cmd/eva-eval paths \"$BASE\" \"$SHA\"\n", false, true},
		{"      - run: go run ./cmd/eva-eval size\n", false, true},
		{"      - run: go run ./cmd/eva-eval fidelity\n", true, true},
		{"      - run: go build -o bin/x ./cmd/eva-eval\n", true, true},
		{"      - run: go test ./internal/eval/...\n", true, true},
		{"      - run: go build -o bin/eva-md ./cmd/eva-md\n", false, false},
	}
	for _, c := range cases {
		if got := namesHarness(c.text, c.ci) != ""; got != c.names {
			t.Errorf("%q in ci.yml %v: names the harness %v, want %v", c.text, c.ci, got, c.names)
		}
	}
}

// Only eva's name shows: the harness imports no library outside the
// standard one, and so no library can name itself in what it prints. A
// package of eva-md itself that an instrument measures, such as v1's
// recall, is held to eva-md's own rule on the two libraries it may use.
func TestOnlyEvasNameShows(t *testing.T) {
	root := moduleRoot(t)
	product := deps(t, root, "cmd/eva-md")
	for _, dir := range append([]string{"cmd/eva-eval"}, deps(t, root, "cmd/eva-eval")...) {
		if slices.Contains(product, dir) {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(root, dir, "*.go"))
		for _, f := range files {
			parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range parsed.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if first, _, _ := strings.Cut(p, "/"); strings.Contains(first, ".") && !strings.HasPrefix(p, "github.com/heyNag/eva-md/") {
					t.Errorf("%s imports %s", f, p)
				}
			}
		}
	}
	_, _, errOut := cmd()
	if !strings.HasPrefix(errOut, "eva-eval: ") {
		t.Errorf("usage: %q", errOut)
	}
}

// An instrument adds its command from its own file: run reaches it, the
// usage lists it after the three commands, by name, and a name that is
// taken or malformed, or a command with no usage line or nothing to run,
// stops the program.
func TestAnInstrumentAddsItsCommand(t *testing.T) {
	saved := instruments
	instruments = map[string]instrument{}
	t.Cleanup(func() { instruments = saved })
	ran := func(word string) func(context.Context, []string, io.Writer, io.Writer) int {
		return func(_ context.Context, args []string, out, _ io.Writer) int {
			fmt.Fprintln(out, word, strings.Join(args, " "))
			return 7
		}
	}
	register(instrument{name: "zeta", synopsis: "zeta --out dir", run: ran("zeta ran")})
	register(instrument{name: "alpha-two", synopsis: "alpha-two", run: ran("alpha ran")})

	if code, out, errOut := cmd("zeta", "--out", "x"); code != 7 || out != "zeta ran --out x\n" || errOut != "" {
		t.Errorf("zeta: %d %q %q", code, out, errOut)
	}
	if code, out, _ := cmd("alpha-two"); code != 7 || out != "alpha ran \n" {
		t.Errorf("alpha-two: %d %q", code, out)
	}
	if code, _, _ := cmd("gate"); code != 2 {
		t.Errorf("gate with no rows: %d", code)
	}
	_, _, errOut := cmd("unknown")
	want := "eva-eval: there is no command unknown\n" + usageText + "\n  eva-eval alpha-two\n  eva-eval zeta --out dir\n"
	if errOut != want {
		t.Errorf("usage:\n%s\nwant:\n%s", errOut, want)
	}

	for name, i := range map[string]instrument{
		"a name taken by an instrument": {name: "zeta", synopsis: "zeta", run: ran("")},
		"a name taken by gate":          {name: "gate", synopsis: "gate", run: ran("")},
		"a name taken by diff":          {name: "diff", synopsis: "diff", run: ran("")},
		"a name taken by baseline":      {name: "baseline", synopsis: "baseline", run: ran("")},
		"no name":                       {synopsis: "x", run: ran("")},
		"a name with a capital":         {name: "Score", synopsis: "Score", run: ran("")},
		"a name with a space":           {name: "two words", synopsis: "two words", run: ran("")},
		"no usage line":                 {name: "score", run: ran("")},
		"a usage line of another name":  {name: "score", synopsis: "other --out dir", run: ran("")},
		"a usage line of two lines":     {name: "score", synopsis: "score\n  more", run: ran("")},
		"nothing to run":                {name: "score", synopsis: "score"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: register did not stop the program", name)
				}
			}()
			register(i)
		}()
	}
	if len(instruments) != 2 {
		t.Errorf("a refused command was kept: %d commands", len(instruments))
	}
}

// With no instrument added the usage is the three commands, as before.
func TestUsageWithNoInstrument(t *testing.T) {
	saved := instruments
	instruments = map[string]instrument{}
	t.Cleanup(func() { instruments = saved })
	if _, _, errOut := cmd(); errOut != "eva-eval: it needs a command\n"+usageText+"\n" {
		t.Errorf("usage: %q", errOut)
	}
}
