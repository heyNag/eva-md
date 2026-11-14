package spec

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A check that is no Go test stands in a Tests cell as check:<path> only
// while its file is there and a workflow's run step runs it: a check whose
// file is gone, one no workflow runs, one a workflow names only in a
// comment, and a path that is no plain path from the root each fail the
// must-keep check, with a problem that names it.
func TestACheckCellIsHeldToAWorkflow(t *testing.T) {
	tempHome(t)
	const gateStep = "      - run: python3 -I scripts/bench/test_gate.py\n"
	const ci = workflows + "/ci.yml"
	cases := []struct {
		name string
		seed func(t *testing.T, dir string)
		want []string
	}{
		{"a check whose file is gone", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "EVL-K10")
			edit(t, dir, e.file, row, strings.Replace(row, "check:scripts/bench/test_gate.py", "check:scripts/bench/gone.py", 1))
		}, []string{`^docs/spec/performance\.md:[0-9]+: EVL-K10 names check:scripts/bench/gone\.py, which is no file of the repository$`}},
		{"a check no workflow runs", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "")
		}, []string{
			`^docs/spec/(checks|testing)\.md:[0-9]+: EVL-K08 names check:scripts/bench/test_gate\.py, which no run step of a workflow in \.github/workflows runs$`,
			`^docs/spec/performance\.md:[0-9]+: EVL-K10 names check:scripts/bench/test_gate\.py, which no run step`,
		}},
		{"a check a workflow names in a comment", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "      # python3 -I scripts/bench/test_gate.py\n      - run: true\n")
		}, []string{
			`^docs/spec/(checks|testing)\.md:[0-9]+: EVL-K08 names check:scripts/bench/test_gate\.py, which no run step`,
			`^docs/spec/performance\.md:[0-9]+: EVL-K10 names check:scripts/bench/test_gate\.py, which no run step`,
		}},
		{"a check a workflow names as part of another path", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "      - run: python3 -I scripts/bench/test_gate.py.bak\n")
		}, []string{
			`^docs/spec/(checks|testing)\.md:[0-9]+: EVL-K08 names check:scripts/bench/test_gate\.py, which no run step`,
			`^docs/spec/performance\.md:[0-9]+: EVL-K10 names check:scripts/bench/test_gate\.py, which no run step`,
		}},
		{"a path out of the repository", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "EVL-K09")
			edit(t, dir, e.file, row, strings.Replace(row, "check:scripts/benchmark.py", "check:scripts/../go.mod", 1))
		}, []string{`^docs/spec/performance\.md:[0-9]+: EVL-K09 names "check:scripts/\.\./go\.mod", which is not <package path>:<TestName> or check:<path>`}},
		{"a check named twice", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "EVL-K09")
			edit(t, dir, e.file, row, strings.Replace(row, "check:scripts/benchmark.py", "check:scripts/benchmark.py, check:scripts/benchmark.py", 1))
		}, []string{`^docs/spec/performance\.md:[0-9]+: EVL-K09 names check:scripts/benchmark\.py twice$`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyMustKeep(t)
			c.seed(t, dir)
			problems := checkMustKeep(dir)
			for _, p := range problems {
				if !slices.ContainsFunc(c.want, func(w string) bool { return regexp.MustCompile(w).MatchString(p) }) {
					t.Errorf("a problem the seed does not explain: %q", p)
				}
			}
			for _, w := range c.want {
				if !slices.ContainsFunc(problems, regexp.MustCompile(w).MatchString) {
					t.Errorf("no problem says %q: %q", w, problems)
				}
			}
		})
	}
}

// A run step names a check as a word of its command, inline or in its
// block, and not in a comment, another key or a longer path.
func TestRunByWorkflowReadsRunSteps(t *testing.T) {
	tempHome(t)
	for _, c := range []struct {
		yml  string
		want bool
	}{
		{"jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n", true},
		{"jobs:\n  a:\n    steps:\n      - name: x\n        run: |\n          cd y\n          python3 -I scripts/x.py --flag\n", true},
		{"jobs:\n  a:\n    steps:\n      - run: \"scripts/x.py\"\n", true},
		{"jobs:\n  a:\n    steps:\n      - run: |\n          # scripts/x.py\n          true\n", false},
		{"jobs:\n  a:\n    steps:\n      - name: scripts/x.py\n        run: true\n", false},
		{"jobs:\n  a:\n    steps:\n      - run: |\n          true\n      - name: scripts/x.py\n", false},
		{"jobs:\n  a:\n    steps:\n      - run: python3 scripts/x.pyc\n", false},
		{"jobs:\n  a:\n    steps:\n      - run: python3 other/scripts/x.py\n", false},
	} {
		dir := t.TempDir()
		write(t, dir, workflows+"/a.yml", c.yml)
		if got := runByWorkflow(dir, "scripts/x.py"); got != c.want {
			t.Errorf("%q read as running scripts/x.py: %v, want %v", c.yml, got, c.want)
		}
	}
}
