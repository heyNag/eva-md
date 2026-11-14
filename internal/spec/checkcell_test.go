package spec

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// surely is what a run step that holds a check must be, as the must-keep
// check's problem says it.
const surely = "on a push or a pull request, itself or through a script of the repository it runs, " +
	"with no continue-on-error or if on the step or its job"

// wfStep is one step of a workflow's job, as the check reads it.
type wfStep struct {
	keys map[string]string // its keys, with their values on the key's line
	run  []string          // the lines its run runs: the value, or the block
}

// wfJob is one job of a workflow and its steps.
type wfJob struct {
	keys  map[string]string
	steps []wfStep
}

// readWorkflow reads a workflow as this repository writes them, two spaces
// of indent a level: whether a push or a pull request runs it, and its
// jobs with their steps.
func readWorkflow(text string) (pushed bool, jobs []wfJob) {
	jobStart := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	jobKey := regexp.MustCompile(`^    ([A-Za-z0-9_-]+):(?:\s+(.*))?$`)
	stepStart := regexp.MustCompile(`^      - ([A-Za-z0-9_-]+):(?:\s+(.*))?$`)
	stepKey := regexp.MustCompile(`^        ([A-Za-z0-9_-]+):(?:\s+(.*))?$`)
	section, inSteps := "", false
	block := -1 // the indent of the run key whose block the lines are in
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block >= 0 {
			if trimmed == "" || indent > block {
				j := &jobs[len(jobs)-1]
				s := &j.steps[len(j.steps)-1]
				s.run = append(s.run, trimmed)
				continue
			}
			block = -1
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if indent == 0 {
			name, value, _ := strings.Cut(line, ":")
			section = name
			if name == "on" && strings.TrimSpace(value) != "" {
				names := strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune("[], ", r) })
				pushed = slices.Contains(names, "push") || slices.Contains(names, "pull_request")
			}
			continue
		}
		switch section {
		case "on":
			if indent == 2 && (strings.HasPrefix(trimmed, "push:") || strings.HasPrefix(trimmed, "pull_request:")) {
				pushed = true
			}
			continue
		case "jobs":
		default:
			continue
		}
		if m := jobStart.FindStringSubmatch(line); m != nil {
			jobs, inSteps = append(jobs, wfJob{keys: map[string]string{}}), false
			continue
		}
		if len(jobs) == 0 {
			continue
		}
		j := &jobs[len(jobs)-1]
		if m := jobKey.FindStringSubmatch(line); m != nil {
			j.keys[m[1]], inSteps = m[2], m[1] == "steps"
			continue
		}
		var key, value string
		if m := stepStart.FindStringSubmatch(line); m != nil && inSteps {
			j.steps = append(j.steps, wfStep{keys: map[string]string{}})
			key, value = m[1], m[2]
		} else if m := stepKey.FindStringSubmatch(line); m != nil && inSteps && len(j.steps) > 0 {
			key, value = m[1], m[2]
		} else {
			continue
		}
		s := &j.steps[len(j.steps)-1]
		s.keys[key] = value
		if key == "run" {
			if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
				block = 8
			} else {
				s.run = append(s.run, strings.Trim(value, `"'`))
			}
		}
	}
	return pushed, jobs
}

// unplainForms are the forms of YAML readWorkflow cannot read as GitHub
// does, each of which could hide a key, such as continue-on-error or if,
// that it would then read as absent. The same list is written in
// internal/release/ci_guard_test.go, for the line reader there: the two test
// packages share no code, so a form added to one is added to the other.
var unplainForms = []*regexp.Regexp{
	regexp.MustCompile(`^ *\t`),                                                           // a tab in its indent
	regexp.MustCompile(`^\s*(?:- +)?(?:"[^"]*"|'[^']*')\s*:`),                             // a quoted key
	regexp.MustCompile(`^\s*(?:- +)?[A-Za-z0-9_-]+\s+:(?:\s|$)`),                          // a key with a space before its colon
	regexp.MustCompile(`^\s*(?:- +)?<<\s*:|:\s+[&*][A-Za-z0-9_]|^\s*- +[&*][A-Za-z0-9_]`), // an anchor, an alias or a merge key
	regexp.MustCompile(`^\s*(?:- +)?\?(?:\s|$)`),                                          // a complex key
	regexp.MustCompile(`:\s+\{|^\s*- +\{`),                                                // a flow mapping
}

// blockOpens is a line whose key's value is a block scalar, such as a
// run's script, whose lines YAML reads as text.
var blockOpens = regexp.MustCompile(`^\s*(?:- )?[^\s#][^#]*:\s*[|>][-+0-9]*\s*$`)

// unplain says whether a line of a workflow, outside its comments and its
// block scalars, holds one of unplainForms.
func unplain(text string) bool {
	block := -1 // the indent of the key whose block scalar the lines are in
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block >= 0 && (trimmed == "" || indent > block) {
			continue
		}
		block = -1
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if slices.ContainsFunc(unplainForms, func(re *regexp.Regexp) bool { return re.MatchString(line) }) {
			return true
		}
		if blockOpens.MatchString(line) {
			block = indent
		}
	}
	return false
}

// sure says whether a step of a job runs whenever its job does and fails
// it when its command fails: no continue-on-error but false, and no if, on
// it or its job.
func sure(j wfJob, s wfStep) bool {
	for _, keys := range []map[string]string{j.keys, s.keys} {
		if v, ok := keys["continue-on-error"]; ok && v != "false" {
			return false
		}
		if _, ok := keys["if"]; ok {
			return false
		}
	}
	return true
}

// shellFunc is a line that defines a shell function.
var shellFunc = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\(\)\s*\{`)

// runsPath says whether a line of a shell command runs path: its first
// word, or the first after an interpreter and its flags, is path, once
// words that run what follows them (exec, time, command, and the functions
// the script defines) are passed. A line that may go on past a failure
// (||) or runs the command in the background does not count, and neither
// does a word in a comment, an argument such as echo's, or a longer path.
func runsPath(line, path string, funcs []string) bool {
	if line, _, _ = strings.Cut(line, " #"); strings.HasPrefix(strings.TrimSpace(line), "#") ||
		strings.Contains(line, "||") || strings.HasSuffix(strings.TrimSpace(line), "&") {
		return false
	}
	words := strings.Fields(line)
	i := 0
	for i < len(words) && (slices.Contains([]string{"exec", "time", "command"}, words[i]) || slices.Contains(funcs, words[i])) {
		i++
	}
	if i < len(words) && slices.Contains([]string{"python3", "python", "bash", "sh"}, words[i]) {
		for i++; i < len(words) && strings.HasPrefix(words[i], "-"); i++ {
		}
	}
	return i < len(words) && strings.TrimPrefix(strings.Trim(words[i], `"'`), "./") == path
}

// runByWorkflow says whether a workflow at root that a push or a pull
// request runs has a step that runs path, with nothing that lets the step
// fail and pass or leaves it out, either as a command of the step or as a
// command of a script of the repository that such a step runs. A workflow
// that holds a form of unplainForms is passed over, as it may hide a key
// that lets the step fail and pass or leaves it out.
func runByWorkflow(root, path string) bool {
	files, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(workflows), "*.yml"))
	scripts, _ := filepath.Glob(filepath.Join(root, "scripts", "*.sh"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil || unplain(string(data)) {
			continue
		}
		pushed, jobs := readWorkflow(string(data))
		if !pushed {
			continue
		}
		for _, j := range jobs {
			for _, s := range j.steps {
				if !sure(j, s) {
					continue
				}
				for _, line := range s.run {
					if runsPath(line, path, nil) {
						return true
					}
					for _, script := range scripts {
						if rel := "scripts/" + filepath.Base(script); runsPath(line, rel, nil) && scriptRuns(script, path) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// scriptRuns says whether a line of the shell script at file runs path.
func scriptRuns(file, path string) bool {
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	var funcs []string
	for _, l := range lines {
		if m := shellFunc.FindStringSubmatch(l); m != nil {
			funcs = append(funcs, m[1])
		}
	}
	return slices.ContainsFunc(lines, func(l string) bool { return runsPath(l, path, funcs) })
}

// A check that is no Go test stands in a Tests cell as check:<path> only
// while its file is there and a workflow that a push or a pull request
// runs runs it in a step that cannot fail and pass, itself or through a
// script of the repository: a check whose file is gone, one no workflow
// runs, one a workflow names only in a comment or an argument, one run by
// a step or a job that may fail or be left out, one whose failure a step
// passes over, one run only when asked, one whose script no longer runs it
// or is run by a step that may fail, one whose workflow holds a form of YAML
// the reader cannot read as GitHub does (a quoted key, a key with a space
// before its colon, an anchor, an alias, a merge key, a complex key, a flow
// mapping or a tab in an indent), and a path that is no plain path from the
// root each fail the must-keep check, with a problem that names it; the
// workflows as they are hold every check cell.
func TestACheckCellIsHeldToAWorkflow(t *testing.T) {
	tempHome(t)
	const gateStep = "      - run: python3 -I scripts/bench/test_gate.py\n"
	const ci = workflows + "/ci.yml"
	const latency = "  latency:\n    runs-on: macos-latest\n"
	gateGone := []string{
		`^docs/spec/(checks|testing)\.md:[0-9]+: EVL-K08 names check:scripts/bench/test_gate\.py, which no run step`,
		`^docs/spec/performance\.md:[0-9]+: EVL-K10 names check:scripts/bench/test_gate\.py, which no run step`,
	}
	var benchGone []string
	mk, _ := readMustKeep(repoRoot(t))
	for _, e := range mk.rows {
		if slices.Contains(e.checks, "scripts/benchmark.py") {
			benchGone = append(benchGone, `^`+regexp.QuoteMeta(e.file)+`:[0-9]+: `+e.id+` names check:scripts/benchmark\.py, which no run step`)
		}
	}
	if len(benchGone) < 4 {
		t.Fatalf("read %d rows that name the benchmark", len(benchGone))
	}
	cases := []struct {
		name string
		seed func(t *testing.T, dir string)
		want []string
	}{
		{"none", func(t *testing.T, dir string) {}, nil},
		{"a check whose file is gone", func(t *testing.T, dir string) {
			e, row := rowOf(t, dir, "EVL-K10")
			edit(t, dir, e.file, row, strings.Replace(row, "check:scripts/bench/test_gate.py", "check:scripts/bench/gone.py", 1))
		}, []string{`^docs/spec/performance\.md:[0-9]+: EVL-K10 names check:scripts/bench/gone\.py, which is no file of the repository$`}},
		{"a check no workflow runs", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "")
		}, []string{
			`^docs/spec/(checks|testing)\.md:[0-9]+: EVL-K08 names check:scripts/bench/test_gate\.py, which no run step of a workflow in \.github/workflows runs, on a push or a pull request, itself or through a script of the repository it runs, with no continue-on-error or if on the step or its job$`,
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
		{"a check run by a step that may fail", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, gateStep+"        continue-on-error: true\n")
		}, gateGone},
		{"a check run by a step only now and then", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, gateStep+"        if: github.event_name == 'pull_request'\n")
		}, gateGone},
		{"a check run by a job that never runs", func(t *testing.T, dir string) {
			edit(t, dir, ci, latency, latency+"    if: false\n")
		}, gateGone},
		{"a check run by a job that may fail", func(t *testing.T, dir string) {
			edit(t, dir, ci, latency, latency+"    continue-on-error: true\n")
		}, gateGone},
		{"a check a step only names", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "      - run: echo python3 -I scripts/bench/test_gate.py\n")
		}, gateGone},
		{"a check whose failure a step passes over", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "      - run: python3 -I scripts/bench/test_gate.py || true\n")
		}, gateGone},
		{"a check run only when asked", func(t *testing.T, dir string) {
			edit(t, dir, ci, "on:\n  push:\n    branches: [main]\n  pull_request:\n", "on:\n  workflow_dispatch:\n")
		}, append(gateGone, benchGone...)},
		{"a check no script a workflow runs runs any more", func(t *testing.T, dir string) {
			edit(t, dir, "scripts/linux-check.sh", "    in_snapshot python3 -I scripts/benchmark.py", "    in_snapshot echo python3 -I scripts/benchmark.py")
			edit(t, dir, "scripts/linux-check.sh", "    python3 -I scripts/benchmark.py --binary bin/eva-md", "    : python3 -I scripts/benchmark.py --binary bin/eva-md")
		}, benchGone},
		{"a check whose script no workflow runs any more", func(t *testing.T, dir string) {
			edit(t, dir, ci, "      - run: scripts/linux-check.sh\n", "      - run: scripts/linux-check.sh\n        continue-on-error: true\n")
		}, benchGone},
		{"a key hidden by quotes", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, gateStep+"        \"continue-on-error\": true\n")
		}, append(gateGone, benchGone...)},
		{"a key hidden by a space before its colon", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, gateStep+"        continue-on-error : true\n")
		}, append(gateGone, benchGone...)},
		{"a workflow with an anchor", func(t *testing.T, dir string) {
			edit(t, dir, ci, latency, "  latency:\n    runs-on: &os macos-latest\n")
		}, append(gateGone, benchGone...)},
		{"a workflow with an alias", func(t *testing.T, dir string) {
			edit(t, dir, ci, latency, "  latency:\n    runs-on: *os\n")
		}, append(gateGone, benchGone...)},
		{"a key hidden by a merge key", func(t *testing.T, dir string) {
			edit(t, dir, ci, latency, "  latency:\n    <<: *flaky\n    runs-on: macos-latest\n")
		}, append(gateGone, benchGone...)},
		{"a key hidden as a complex key", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, gateStep+"        ? continue-on-error\n        : true\n")
		}, append(gateGone, benchGone...)},
		{"a workflow with a flow mapping", func(t *testing.T, dir string) {
			edit(t, dir, ci, latency, latency+"    env: {LANG: C}\n")
		}, append(gateGone, benchGone...)},
		{"a key hidden behind a tab", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, gateStep+"        \tcontinue-on-error: true\n")
		}, append(gateGone, benchGone...)},
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

// A run step runs a check when the check is the command of a line of it,
// inline or in its block, or of a line of a script of the repository it
// runs, and a push or a pull request runs the workflow with nothing that
// lets the step fail and pass or leaves it out; not when the check is in a
// comment, another key, an argument or a longer path, or a failure of it
// is passed over, or the workflow holds, outside its block scalars, a form
// of unplainForms; such a form in a block scalar is text, as YAML reads it.
func TestRunByWorkflowReadsRunSteps(t *testing.T) {
	tempHome(t)
	const on = "on:\n  push:\n    branches: [main]\n  pull_request:\n\n"
	const script = "#!/bin/bash\nwrap() { \"$@\"; }\ncheck() {\n  wrap python3 -I scripts/x.py --flag\n}\ncheck\n"
	for _, c := range []struct {
		yml, script string
		want        bool
	}{
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n", "", true},
		{on + "jobs:\n  a:\n    steps:\n      - name: x\n        run: |\n          cd y\n          python3 -I scripts/x.py --flag\n", "", true},
		{on + "jobs:\n  a:\n    steps:\n      - run: \"scripts/x.py\"\n", "", true},
		{"on: [push]\njobs:\n  a:\n    steps:\n      - run: ./scripts/x.py\n", "", true},
		{on + "jobs:\n  a:\n    steps:\n      - run: scripts/s.sh\n", script, true},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          # scripts/x.py\n          true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - name: scripts/x.py\n        run: true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          true\n      - name: scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 scripts/x.pyc\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 other/scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: echo skipping scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 scripts/x.py || true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n        continue-on-error: true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - if: always()\n        run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    if: false\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    continue-on-error: true\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{"on:\n  workflow_dispatch:\njobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{"jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: scripts/s.sh\n        continue-on-error: true\n", script, false},
		{on + "jobs:\n  a:\n    steps:\n      - run: scripts/s.sh\n", strings.Replace(script, "wrap python3", "echo python3", 1), false},
		{on + "jobs:\n  a:\n    steps:\n      - run: echo scripts/s.sh\n", script, false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n        \"continue-on-error\": true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n        'if': false\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n        continue-on-error : true\n", "", false},
		{on + "jobs:\n  a:\n    runs-on: &os ubuntu-latest\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    runs-on: *os\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n      - &later\n        name: y\n", "", false},
		{on + "jobs:\n  a:\n    <<: *flaky\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n        ? if\n        : false\n", "", false},
		{on + "jobs:\n  a:\n    env: {A: b}\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n      - {run: true, if: false}\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n        \tcontinue-on-error: true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n          echo \"a\" : {b: *c} &d\n          ? e\n      - name: y\n        env:\n          T: >-\n            <<: 'f': g\n        run: true\n", "", true},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n\t  continue-on-error: true\n", "", false},
	} {
		dir := t.TempDir()
		write(t, dir, workflows+"/a.yml", c.yml)
		if c.script != "" {
			write(t, dir, "scripts/s.sh", c.script)
		}
		if got := runByWorkflow(dir, "scripts/x.py"); got != c.want {
			t.Errorf("%q with the script %q read as running scripts/x.py: %v, want %v", c.yml, c.script, got, c.want)
		}
	}
}
