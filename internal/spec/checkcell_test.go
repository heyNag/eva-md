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
	"with no continue-on-error, if, needs, shell or defaults on the step, its job or the workflow"

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

// plainKey is a node with a plain key, and the value after it.
var plainKey = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.$/-]*:(?:\s+(.*)|$)`)

// blockScalar is the value of a key whose block scalar follows: | or >,
// with an indentation or a chomping indicator.
var blockScalar = regexp.MustCompile(`^[|>](?:[1-9][-+]?|[-+][1-9]?)?$`)

// node reads a line of a workflow outside its comments and block scalars:
// col is the column of its key, past the dashes of the sequence entries it
// opens, or for an entry with no key the column of its last dash (its
// indent when it has none); value is what follows the key, or the entry's
// scalar, with a comment that ends the line taken off. A key is plain
// letters, digits and _ . $ / -, then a colon and a space or the line's end.
func node(line string) (col int, value string) {
	rest := strings.TrimLeft(line, " ")
	col = len(line) - len(rest)
	dash := col
	for rest == "-" || strings.HasPrefix(rest, "- ") {
		dash = col
		after := strings.TrimLeft(rest[1:], " ")
		col += len(rest) - len(after)
		rest = after
	}
	if i := strings.Index(rest, " #"); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if m := plainKey.FindStringSubmatch(rest); m != nil {
		return col, strings.TrimSpace(m[1])
	}
	return dash, rest
}

// readWorkflow reads a workflow as this repository writes them, two spaces
// of indent a level: whether a push or a pull request runs it, and its
// jobs with their steps. A workflow's defaults are given to each job, as
// they hold for each. A run's literal block is read line by line; a folded
// one, whose lines YAML joins, is not read, so its step runs nothing here;
// any other key's block scalar is passed over as text.
func readWorkflow(text string) (pushed bool, jobs []wfJob) {
	jobStart := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	jobKey := regexp.MustCompile(`^    ([A-Za-z0-9_-]+):(?:\s+(.*))?$`)
	stepStart := regexp.MustCompile(`^      - ([A-Za-z0-9_-]+):(?:\s+(.*))?$`)
	stepKey := regexp.MustCompile(`^        ([A-Za-z0-9_-]+):(?:\s+(.*))?$`)
	section, inSteps, defaults := "", false, false
	block := -1        // the column of the key whose block scalar the lines are in
	readBlock := false // whether that block is a run's literal block, read as its lines
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block >= 0 {
			if trimmed == "" || indent > block {
				if readBlock {
					j := &jobs[len(jobs)-1]
					s := &j.steps[len(j.steps)-1]
					s.run = append(s.run, trimmed)
				}
				continue
			}
			block, readBlock = -1, false
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		col, scalar := node(line)
		if blockScalar.MatchString(scalar) {
			block = col
		}
		if indent == 0 {
			name, value, _ := strings.Cut(line, ":")
			section = name
			if name == "on" && strings.TrimSpace(value) != "" {
				names := strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune("[], ", r) })
				pushed = slices.Contains(names, "push") || slices.Contains(names, "pull_request")
			}
			defaults = defaults || name == "defaults"
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
			if block >= 0 {
				readBlock = strings.HasPrefix(scalar, "|")
			} else {
				s.run = append(s.run, strings.Trim(value, `"'`))
			}
		}
	}
	if defaults {
		for _, j := range jobs {
			j.keys["defaults"] = "(the workflow's)"
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
	regexp.MustCompile(`^ *\t`),                                                       // a tab in its indent
	regexp.MustCompile(`^\s*(?:-\s+)*(?:"[^"]*"|'[^']*')\s*:`),                        // a quoted key
	regexp.MustCompile(`^\s*(?:-\s+)*[A-Za-z0-9_-]+\s+:(?:\s|$)`),                     // a key with a space before its colon
	regexp.MustCompile(`^\s*(?:-\s+)*<<\s*:|:\s+[&*][A-Za-z0-9_]|^\s*(?:-\s+)*[&*!]`), // an anchor, an alias, a tag or a merge key
	regexp.MustCompile(`^\s*(?:-\s+)*[?:](?:\s|$)`),                                   // a complex key, or its value
	regexp.MustCompile(`:\s+\{|^\s*(?:-\s+)*[{},%@` + "`" + `]`),                      // a flow mapping, or an indicator YAML reserves
}

// closedQuote is a quoted value YAML reads as the text between its quotes:
// closed on its line, with no escape and no quote doubled.
var closedQuote = regexp.MustCompile(`^(?:"[^"\\]*"|'[^']*')$`)

// unplain says whether a workflow, outside its comments and its block
// scalars, holds one of unplainForms, or a scalar the reader cannot read
// as YAML does: a value that goes on past its line, which YAML joins to it;
// a quoted value that is not closed on its line, holds an escape or doubles
// its quote; or a plain value that holds a colon and a space, which YAML
// refuses. A block scalar's lines are those indented as its first line is,
// or more, and it ends at a line indented less; a line then indented past
// the block's key is unplain too.
func unplain(text string) bool {
	block, content := -1, -1  // the column of the block scalar's key, and its lines' indent
	prev, inline := -1, false // the column of the last node, and whether it held a value
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block >= 0 {
			if trimmed == "" {
				continue
			}
			if content < 0 && indent > block {
				content = indent
			}
			if content >= 0 && indent >= content {
				continue
			}
			block, content = -1, -1
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if slices.ContainsFunc(unplainForms, func(re *regexp.Regexp) bool { return re.MatchString(line) }) {
			return true
		}
		if inline && indent > prev {
			return true
		}
		col, value := node(line)
		if quoted := strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'"); quoted && !closedQuote.MatchString(value) ||
			!quoted && (strings.Contains(value, ": ") || strings.Contains(value, ":\t")) {
			return true
		}
		if blockScalar.MatchString(value) {
			block = col
		}
		prev, inline = col, value != ""
	}
	return false
}

// sure says whether a step of a job runs whenever its job does and fails
// it when its command fails: no continue-on-error but false, no if, no
// shell of its own and no defaults, on it or its job (the workflow's
// defaults are its job's), and a job that needs no other job.
func sure(j wfJob, s wfStep) bool {
	for _, keys := range []map[string]string{j.keys, s.keys} {
		if v, ok := keys["continue-on-error"]; ok && v != "false" {
			return false
		}
		for _, k := range []string{"if", "needs", "shell", "defaults"} {
			if _, ok := keys[k]; ok {
				return false
			}
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
			`^docs/spec/(checks|testing)\.md:[0-9]+: EVL-K08 names check:scripts/bench/test_gate\.py, which no run step of a workflow in \.github/workflows runs, on a push or a pull request, itself or through a script of the repository it runs, with no continue-on-error, if, needs, shell or defaults on the step, its job or the workflow$`,
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
		{"a key hidden by quotes after a block", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "      - run: |\n          python3 -I scripts/bench/test_gate.py\n        \"continue-on-error\": true\n")
		}, append(gateGone, benchGone...)},
		{"a key hidden behind an anchor on it", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, gateStep+"        &a continue-on-error: true\n")
		}, append(gateGone, benchGone...)},
		{"a key hidden behind a tag on it", func(t *testing.T, dir string) {
			edit(t, dir, ci, latency, latency+"    !!str if: false\n")
		}, append(gateGone, benchGone...)},
		{"a check whose failure a line that goes on passes over", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "      - run: python3 -I scripts/bench/test_gate.py\n          || true\n")
		}, append(gateGone, benchGone...)},
		{"a check whose failure an escape hides", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "      - run: \"python3 -I scripts/bench/test_gate.py \\x7c| true\"\n")
		}, append(gateGone, benchGone...)},
		{"a check whose failure a folded block passes over", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, "      - run: >\n          python3 -I scripts/bench/test_gate.py\n          || true\n")
		}, gateGone},
		{"a check run by a step with a shell of its own", func(t *testing.T, dir string) {
			edit(t, dir, ci, gateStep, gateStep+"        shell: bash {0}\n")
		}, gateGone},
		{"a check run by a job that needs another", func(t *testing.T, dir string) {
			edit(t, dir, ci, latency, latency+"    needs: test\n")
		}, gateGone},
		{"a check run under the workflow's defaults", func(t *testing.T, dir string) {
			edit(t, dir, ci, "permissions:\n  contents: read\n", "permissions:\n  contents: read\n\ndefaults:\n  run:\n    shell: bash {0}\n")
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
		// Each form after a block opened on a step's first line, whose
		// block ends at the step's next key, as YAML reads it.
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n        \"continue-on-error\": true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n        if : false\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n        ? if\n        : false\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n        \tcontinue-on-error: true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n        &a continue-on-error: true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n         continue-on-error: true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: |-\n            python3 -I scripts/x.py\n          true\n", "", false},
		// A colon and a bar in a plain value open no block.
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py a:|\n        \"continue-on-error\": true\n", "", false},
		// An anchor or a tag on a key.
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n        &a continue-on-error: true\n", "", false},
		{on + "jobs:\n  a:\n    &j if: false\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n        !!str if: false\n", "", false},
		// A folded run, whose lines YAML joins into one command.
		{on + "jobs:\n  a:\n    steps:\n      - run: >\n          python3 -I scripts/x.py\n          || true\n", "", false},
		// A value that goes on past its line, an escape, a plain value
		// YAML refuses.
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n          || true\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: \"python3 -I scripts/x.py\n          || true\"\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: \"python3 -I scripts/x.py \\x7c| true\"\n", "", false},
		{on + "jobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py a: b\n", "", false},
		// A shell of the step's own, defaults, and a job that needs another.
		{on + "jobs:\n  a:\n    steps:\n      - shell: bash {0}\n        run: |\n          python3 -I scripts/x.py\n          true\n", "", false},
		{on + "jobs:\n  a:\n    defaults:\n      run:\n        shell: bash {0}\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "defaults:\n  run:\n    shell: bash {0}\njobs:\n  a:\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		{on + "jobs:\n  b:\n    if: false\n    steps:\n      - run: true\n  a:\n    needs: b\n    steps:\n      - run: python3 -I scripts/x.py\n", "", false},
		// What YAML reads as the reader does still holds.
		{on + "jobs:\n  a:\n    steps:\n      - run: |\n          python3 -I scripts/x.py\n        env:\n          A: b\n      - name: \"a b\"\n        run: 'true'\n", "", true},
		{on + "jobs:\n  a:\n    steps:\n      - name: x # y\n        run: | # z\n          python3 -I scripts/x.py\n\n          true\n", "", true},
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
