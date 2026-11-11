package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/retrieval"
)

// retrievalRepo makes a repository holding the paths the retrieval
// metrics cover, committed, and returns it, its commit and the committed
// set's folder.
func retrievalRepo(t *testing.T) (string, string, string) {
	t.Helper()
	isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	for _, p := range []string{"eval/retrieval/set/set.json", "internal/eval/retrieval/doc.go", "internal/knowledgepage/doc.go"} {
		write(t, filepath.Join(repo, filepath.FromSlash(p)), "placeholder\n")
	}
	gitIn(t, filepath.Dir(repo), "init", "-q", repo)
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "fixture")
	return repo, gitIn(t, repo, "rev-parse", "HEAD"), filepath.Join(moduleRoot(t), "eval", "retrieval", "set")
}

// The command scores a system on the set: it appends a row for each
// figure, of the repository's commit, and prints the figures and ids. Two
// runs give the same rows apart from the time, the rows' commit is the
// repository's, and nothing printed holds a prompt's text or a planted
// word.
func TestRetrievalCommand(t *testing.T) {
	repo, commit, set := retrievalRepo(t)
	s, err := retrieval.Load(set)
	if err != nil {
		t.Fatal(err)
	}
	var runs [2][]results.Row
	for i := range runs {
		out := filepath.Join(t.TempDir(), "out")
		code, stdout, stderr := cmd("retrieval", "--repo", repo, "--set", set, "--system", "v1-recall", "--out", out)
		if code != 0 || stderr != "" {
			t.Fatalf("run %d: %d %s", i+1, code, stderr)
		}
		if !strings.Contains(stdout, "retrieval.heldout.all.precision") || !strings.Contains(stdout, "Wrote ") {
			t.Errorf("output:\n%s", stdout)
		}
		low := strings.ToLower(stdout)
		for _, e := range s.Events {
			if strings.Contains(stdout, e.Text) {
				t.Fatalf("the output shows event %s's text", e.ID)
			}
		}
		for _, w := range append(s.Forbid(), "okf", "toml") {
			if strings.Contains(low, strings.ToLower(w)) {
				t.Errorf("the output holds %q", w)
			}
		}
		if runs[i], err = results.Load(out); err != nil {
			t.Fatal(err)
		}
	}
	if len(runs[0]) < 40 {
		t.Fatalf("%d rows", len(runs[0]))
	}
	for i := range runs[1] {
		if runs[0][i].Time.IsZero() || runs[1][i].Time.IsZero() {
			t.Errorf("row %d's time: %v then %v", i+1, runs[0][i].Time, runs[1][i].Time)
		}
		runs[1][i].Time = runs[0][i].Time
	}
	if !slices.Equal(runs[0], runs[1]) {
		t.Error("two runs give different rows")
	}
	for _, r := range runs[0] {
		if r.Commit != commit || r.Dirty || r.Scenario != "v1-recall.set1" || r.Instrument != "retrieval" {
			t.Fatalf("a row: %+v", r)
		}
	}
}

// silentSystem adds nothing, for a system registered by a test.
type silentSystem struct{}

func (silentSystem) Add(context.Context, retrieval.Event) ([]string, error) { return nil, nil }

// The command gives the Store the set's forbidden words: a system whose
// name holds a project's name, and so would put it in every row's
// scenario, is refused, and no row is written.
func TestRetrievalCommandForbidsTheSetsNames(t *testing.T) {
	repo, _, set := retrievalRepo(t)
	const planted = "tidewater-x" // tidewater is a project of the committed set
	if !slices.Contains(retrieval.Systems(), planted) {
		retrieval.Register(planted, func([]retrieval.Item, []retrieval.Project) (retrieval.System, error) {
			return silentSystem{}, nil
		})
	}
	s, err := retrieval.Load(set)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(s.Projects, func(p retrieval.Project) bool { return p.Name == "tidewater" }) {
		t.Fatal("the committed set has no project tidewater")
	}
	out := filepath.Join(t.TempDir(), "out")
	code, stdout, stderr := cmd("retrieval", "--repo", repo, "--set", set, "--system", planted, "--out", out)
	if code != 2 || stdout != "" || !strings.Contains(stderr, "scenario holds a word the run forbids") {
		t.Errorf("a system named %s: %d %q %q", planted, code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, results.FileName)); !os.IsNotExist(err) {
		t.Errorf("a refused run wrote rows: %v", err)
	}
}

// v1's rows accepted as the baseline hold every gate; the oracle's do
// too; the silent system's fail, since it gives no precision.
func TestRetrievalRowsAgainstTheGates(t *testing.T) {
	repo, _, set := retrievalRepo(t)
	files := filepath.Join(moduleRoot(t), "eval", "retrieval")
	catalogue, gates := filepath.Join(files, "metrics.json"), filepath.Join(files, "gates.json")
	rows := map[string]string{}
	for _, system := range []string{"v1-recall", "oracle", "silent"} {
		rows[system] = filepath.Join(t.TempDir(), system)
		if code, _, stderr := cmd("retrieval", "--repo", repo, "--set", set, "--system", system, "--out", rows[system]); code != 0 {
			t.Fatalf("%s: %d %s", system, code, stderr)
		}
	}
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	if code, _, stderr := cmd("baseline", "accept", "--repo", repo, "--catalogue", catalogue, "--out", baseline, rows["v1-recall"]); code != 0 {
		t.Fatalf("accept: %d %s", code, stderr)
	}
	for system, want := range map[string]int{"v1-recall": 0, "oracle": 0, "silent": 1} {
		code, stdout, stderr := cmd("gate", "--repo", repo, "--base", "HEAD", "--catalogue", catalogue, "--gates", gates, "--baseline", baseline, rows[system])
		if code != want {
			t.Errorf("%s: gate exits %d, not %d: %s%s", system, code, want, stdout, stderr)
		}
		if strings.Count(stdout, "\n") != 7 {
			t.Errorf("%s: gate prints:\n%s", system, stdout)
		}
	}
}

// The command refuses a usage it does not know, an unknown system, a set
// it cannot load and a folder that is not a repository, and says so with
// eva's name only.
func TestRetrievalCommandErrors(t *testing.T) {
	repo, _, set := retrievalRepo(t)
	out := filepath.Join(t.TempDir(), "out")
	notRepo := t.TempDir()
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"no system":          {[]string{"retrieval", "--out", out}, "needs --system and --out"},
		"no out":             {[]string{"retrieval", "--system", "silent"}, "needs --system and --out"},
		"an argument":        {[]string{"retrieval", "--system", "silent", "--out", out, "extra"}, "takes no argument"},
		"an unknown system":  {[]string{"retrieval", "--repo", repo, "--set", set, "--system", "v9", "--out", out}, "there is no system v9"},
		"the repo's own set": {[]string{"retrieval", "--repo", repo, "--system", "silent", "--out", out}, "set.json is not valid"},
		"a missing set":      {[]string{"retrieval", "--repo", repo, "--set", filepath.Join(notRepo, "none"), "--system", "silent", "--out", out}, "no such file"},
		"not a repository":   {[]string{"retrieval", "--repo", notRepo, "--set", set, "--system", "silent", "--out", out}, "cannot read the commit"},
	} {
		code, stdout, stderr := cmd(c.args...)
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "eva-eval: ") || !strings.Contains(stderr, c.want) {
			t.Errorf("%s: %d %q %q", name, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused run wrote rows: %v", err)
	}
	_, _, stderr := cmd("unknown")
	if !strings.Contains(stderr, "\n  eva-eval retrieval [--repo dir] [--set dir] --system name --out dir") {
		t.Errorf("the usage does not list the command:\n%s", stderr)
	}
}
