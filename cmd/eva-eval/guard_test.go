package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/guard"
)

// commitAll stages everything in repo and commits it with message.
func commitAll(t *testing.T, repo, message string) string {
	t.Helper()
	gitIn(t, repo, "add", "--all")
	gitIn(t, repo, "commit", "--quiet", "-m", message)
	return gitIn(t, repo, "rev-parse", "HEAD")
}

func TestPathsCommand(t *testing.T) {
	isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	write(t, filepath.Join(repo, "docs", "v2", "cards", "AA-1.md"), "# AA-1\n\n```paths\na/**\n```\n")
	write(t, filepath.Join(repo, "a", "x.go"), "package a\n")
	gitIn(t, repo, "init", "--quiet")
	base := commitAll(t, repo, "the lead's first commit")
	write(t, filepath.Join(repo, "a", "y.go"), "package a\n")
	inside := commitAll(t, repo, "inside\n\nCard: AA-1")
	write(t, filepath.Join(repo, "notes.md"), "made up\n")
	lead := commitAll(t, repo, "the lead's")

	code, out, errOut := cmd("paths", "--repo", repo, base, lead)
	want := inside[:12] + " AA-1: its 1 paths are inside its card's paths\n" +
		lead[:12] + " has no Card: line: the lead's, not judged (1 paths)\n" +
		"2 commits: 1 judged, 1 the lead's; 0 faults.\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("%d\n%s\nwant\n%s%s", code, out, want, errOut)
	}

	write(t, filepath.Join(repo, "b", "z.go"), "package b\n")
	outside := commitAll(t, repo, "outside\n\nCard: AA-1")
	code, out, _ = cmd("paths", "--repo", repo, lead, outside)
	want = outside[:12] + " AA-1: b/z.go is outside the card's paths\n1 commits: 1 judged, 0 the lead's; 1 faults.\n"
	if code != 1 || out != want {
		t.Fatalf("%d\n%s\nwant\n%s", code, out, want)
	}

	// A line that sets out to name a card in another form fails the
	// commit; it is not the lead's.
	write(t, filepath.Join(repo, "a", "w.go"), "package a\n")
	slip := commitAll(t, repo, "a slip\n\nCard: AA-1 (part 2)")
	code, out, _ = cmd("paths", "--repo", repo, outside, slip)
	want = slip[:12] + ` the line "Card: AA-1 (part 2)" does not name one card: a card is named by a line that reads "Card: <ID>", one card a line` + "\n1 commits: 1 judged, 0 the lead's; 1 faults.\n"
	if code != 1 || out != want {
		t.Fatalf("%d\n%s\nwant\n%s", code, out, want)
	}

	for name, args := range map[string][]string{
		"one commit":      {"paths", "--repo", repo, base},
		"three commits":   {"paths", "--repo", repo, base, lead, outside},
		"an unknown ref":  {"paths", "--repo", repo, "no-such-ref", lead},
		"no repository":   {"paths", "--repo", t.TempDir(), "HEAD~1", "HEAD"},
		"an unknown flag": {"paths", "--fast", base, lead},
	} {
		if code, out, errOut := cmd(args...); code != 2 || out != "" || errOut == "" {
			t.Errorf("%s: %d %q %q", name, code, out, errOut)
		}
	}
}

// sizeEnv points the go command at the test's home, offline, on the
// installed toolchain and Go's own caches, which go test itself uses.
func sizeEnv(t *testing.T) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	caches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(caches) != 2 {
		t.Fatalf("go env gave %q", out)
	}
	isolate(t)
	home := os.Getenv("HOME")
	config := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	write(t, filepath.Join(config, "go", "telemetry", "mode"), "off\n")
	for k, v := range map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "GOENV": "off", "GOCACHE": caches[0], "GOMODCACHE": caches[1],
		"GOTOOLCHAIN": "local", "GOPROXY": "off",
	} {
		t.Setenv(k, v)
	}
}

// The size command fails one line over the ceiling, end to end: a module
// whose eva-md is one file of exactly the ceiling's lines holds, and one
// line more fails.
func TestSizeCommand(t *testing.T) {
	sizeEnv(t)
	root := filepath.Join(t.TempDir(), "module")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.22\n")
	main := "package main\n\nfunc main() {}\n"
	write(t, filepath.Join(root, "cmd", "eva-md", "main.go"), main+strings.Repeat("\n", guard.Ceiling-3))
	code, out, errOut := cmd("size", "--repo", root)
	want := "Built lines: 53694\nScheduled lines: 0, in 0 files of the merged cards' deletes blocks\nNet lines: 53694\nCeiling: 53694\nHeadroom: 0\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("%d\n%s\nwant\n%s%s", code, out, want, errOut)
	}
	write(t, filepath.Join(root, "cmd", "eva-md", "main.go"), main+strings.Repeat("\n", guard.Ceiling-2))
	code, out, _ = cmd("size", "--repo", root)
	if code != 1 || !strings.Contains(out, "Headroom: -1\nThe net lines exceed the ceiling by 1.\n") {
		t.Fatalf("%d\n%s", code, out)
	}
	for name, args := range map[string][]string{
		"an argument":  {"size", "--repo", root, "extra"},
		"no module":    {"size", "--repo", t.TempDir()},
		"unknown flag": {"size", "--fast"},
	} {
		if code, out, errOut := cmd(args...); code != 2 || out != "" || errOut == "" {
			t.Errorf("%s: %d %q %q", name, code, out, errOut)
		}
	}
}

// size --ledger exits 2 for a ledger it cannot read, a repository without
// v1's history and a usage error, before it counts anything.
func TestSizeLedgerCommandUsage(t *testing.T) {
	isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	write(t, filepath.Join(repo, "docs", "v2", "size-ledger.md"), "# A made-up ledger\n")
	write(t, filepath.Join(repo, "go.mod"), "module example.com/app\n\ngo 1.22\n")
	gitIn(t, repo, "init", "--quiet")
	commitAll(t, repo, "a history without v1's commit")
	// A ledger in a folder that is no repository is read, and then v1's
	// files cannot be listed.
	notRepo := t.TempDir()
	write(t, filepath.Join(notRepo, "docs", "v2", "size-ledger.md"), "# A made-up ledger\n")
	for name, c := range map[string]struct {
		args []string
		says string
	}{
		"a missing ledger":            {[]string{"size", "--repo", repo, "--ledger", "docs/v2/none.md"}, "none.md"},
		"a ledger that is a folder":   {[]string{"size", "--repo", repo, "--ledger", "docs/v2"}, "docs/v2"},
		"no v1 history":               {[]string{"size", "--repo", repo, "--ledger", "docs/v2/size-ledger.md"}, guard.V1},
		"no ledger at the repository": {[]string{"size", "--repo", t.TempDir(), "--ledger", "docs/v2/size-ledger.md"}, "size-ledger.md"},
		"no repository":               {[]string{"size", "--repo", notRepo, "--ledger", "docs/v2/size-ledger.md"}, guard.V1},
		"an unknown flag":             {[]string{"size", "--repo", repo, "--ledger", "docs/v2/size-ledger.md", "--fast"}, "-fast"},
		"an argument":                 {[]string{"size", "--repo", repo, "--ledger", "docs/v2/size-ledger.md", "extra"}, "size [--repo dir] [--ledger file]"},
		"no file to the flag":         {[]string{"size", "--repo", repo, "--ledger"}, "-ledger"},
	} {
		if code, out, errOut := cmd(c.args...); code != 2 || out != "" || !strings.Contains(errOut, c.says) {
			t.Errorf("%s: %d %q %q", name, code, out, errOut)
		}
	}
}

// size --ledger prints size's report, then the ledger's, and exits 1 on a
// difference or a fault of the ledger, on a fault of the deletes blocks,
// and on a size over the ceiling only once the cut-over has made it plain.
func TestSizeLedgerVerdict(t *testing.T) {
	under, over := guard.Size{Built: 10, Ceiling: 20}, guard.Size{Built: 30, Ceiling: 20}
	plain := func(s guard.Size) guard.Size { s.Plain = true; return s }
	holds := guard.Ledger{CountedAt: "0123456789ab"}
	for name, c := range map[string]struct {
		size   guard.Size
		ledger guard.Ledger
		code   int
	}{
		"both hold":                            {under, holds, 0},
		"over the ceiling before the cut-over": {over, holds, 0},
		"under the ceiling after it":           {plain(under), holds, 0},
		"over the ceiling after it":            {plain(over), holds, 1},
		"a difference": {under, guard.Ledger{CountedAt: "0123456789ab", Differences: []guard.Difference{
			{Text: "row x.go: says 1, counts 2", Write: "| `x.go` | 2 |"}}}, 1},
		"a fault of the ledger":        {under, guard.Ledger{CountedAt: "0123456789ab", Faults: []string{"row x.go: the path does not exist"}}, 1},
		"a fault of the deletes block": {guard.Size{Built: 10, Ceiling: 20, Faults: []string{"AA-1: a made-up fault"}}, holds, 1},
	} {
		var out strings.Builder
		code := ledgerVerdict(&out, "docs/v2/size-ledger.md", "fedcba987654", c.size, c.ledger)
		want := c.size.Report() + "Ledger docs/v2/size-ledger.md, counted at 0123456789ab; HEAD is fedcba987654.\n" + c.ledger.Report()
		if code != c.code || out.String() != want {
			t.Errorf("%s: %d, want %d\n%s\nwant\n%s", name, code, c.code, out.String(), want)
		}
	}
}
