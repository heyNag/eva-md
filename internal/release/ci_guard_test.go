package release

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// CI as the run of record (EVL-K13, and the token's half of EVL-K14): ci.yml
// gives its token read access to the repository's contents and nothing
// more, no job of it asks for more, its macOS job runs the whole suite
// twice, plain and with the race detector, with nothing that lets a step
// fail, leaves it out or filters its tests, its Linux job runs the script a
// cloud session runs, whole, and every push to main and every pull request
// runs it. The workflow is read line by line, so a form of YAML that could
// hide a key from that reading fails the check.

// ciJob is one job of ci.yml, read line by line as this repository writes
// its workflows: two spaces of indent a level, a job's keys at four.
type ciJob struct {
	name string
	text string            // the job's lines, after its name
	keys map[string]string // its own keys, with their values on the key's line
}

// ciJobs reads a workflow's top-level permissions, as the lines of its
// block or the value on the key's line, and its jobs, in order.
func ciJobs(text string) (permissions []string, jobs []ciJob) {
	start := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	key := regexp.MustCompile(`^    ([a-z-]+):(?: (.*))?$`)
	section := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			name, value, _ := strings.Cut(line, ":")
			section = name
			if name == "permissions" && strings.TrimSpace(value) != "" {
				permissions = append(permissions, strings.TrimSpace(value))
			}
			continue
		}
		switch section {
		case "permissions":
			permissions = append(permissions, strings.TrimSpace(line))
		case "jobs":
			if m := start.FindStringSubmatch(line); m != nil {
				jobs = append(jobs, ciJob{name: m[1], keys: map[string]string{}})
				continue
			}
			if len(jobs) == 0 {
				continue
			}
			j := &jobs[len(jobs)-1]
			j.text += line + "\n"
			if m := key.FindStringSubmatch(line); m != nil {
				j.keys[m[1]] = strings.TrimSpace(m[2])
			}
		}
	}
	return permissions, jobs
}

// stepRun is what a step runs: its run's value on the key's line, or its
// block.
func stepRun(s workflowStep) string {
	if run, err := s.runBlock(); err == nil {
		return run
	}
	return s.keys["run"]
}

// fullSuite reads a command line as go test of the whole module, and says
// whether it runs with the race detector: ./... and no flag but -race,
// -timeout with its value, -count and -v. Anything else, a -run, a -skip,
// -short, a build tag or a narrower pattern, filters the tests.
func fullSuite(line string) (ok, race bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "go" || fields[1] != "test" {
		return false, false
	}
	whole := false
	for i := 2; i < len(fields); i++ {
		switch f := fields[i]; {
		case f == "./...":
			whole = true
		case f == "-race":
			race = true
		case f == "-timeout" && i+1 < len(fields):
			i++
		case strings.HasPrefix(f, "-timeout="), strings.HasPrefix(f, "-count="), f == "-v":
		default:
			return false, false
		}
	}
	return whole, race
}

// widens says what a permission line grants beyond reading, or "".
func widens(line string) string {
	switch line = strings.TrimSpace(line); {
	case line == "read-all", line == "{}":
		return ""
	case line == "write-all":
		return line
	}
	name, level, ok := strings.Cut(line, ":")
	if !ok {
		return line
	}
	if level = strings.TrimSpace(level); level != "read" && level != "none" {
		return name + ": " + level
	}
	return ""
}

// structural is a workflow's lines that YAML reads as keys and values, each
// with its number: comments, blank lines and the lines of a block scalar,
// such as a run's script, are left out.
func structural(text string) (lines []string, numbers []int) {
	block := -1 // the indent of the key whose block scalar the lines are in
	opens := regexp.MustCompile(`^\s*(?:- )?[^\s#][^#]*:\s*[|>][-+0-9]*\s*$`)
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block >= 0 && (trimmed == "" || indent > block) {
			continue
		}
		block = -1
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines, numbers = append(lines, line), append(numbers, i+1)
		if opens.MatchString(line) {
			block = indent
		}
	}
	return lines, numbers
}

// unplainForms are the forms of YAML the line reader above cannot read as
// GitHub does, each of which could hide a key, such as continue-on-error,
// that the reader would then read as absent.
var unplainForms = []struct {
	what string
	re   *regexp.Regexp
}{
	{"a tab in its indent", regexp.MustCompile(`^ *\t`)},
	{"a quoted key", regexp.MustCompile(`^\s*(?:- +)?(?:"[^"]*"|'[^']*')\s*:`)},
	{"a key with a space before its colon", regexp.MustCompile(`^\s*(?:- +)?[A-Za-z0-9_-]+\s+:(?:\s|$)`)},
	{"an anchor, an alias or a merge key", regexp.MustCompile(`^\s*(?:- +)?<<\s*:|:\s+[&*][A-Za-z0-9_]|^\s*- +[&*][A-Za-z0-9_]`)},
	{"a complex key", regexp.MustCompile(`^\s*(?:- +)?\?(?:\s|$)`)},
	{"a flow mapping", regexp.MustCompile(`:\s+\{|^\s*- +\{`)},
}

// unplain lists each line of a workflow, outside its block scalars, that
// holds one of unplainForms.
func unplain(text string) []string {
	var problems []string
	lines, numbers := structural(text)
	for i, line := range lines {
		for _, form := range unplainForms {
			if form.re.MatchString(line) {
				problems = append(problems, fmt.Sprintf("line %d holds %s, which this check cannot read as GitHub does: %q", numbers[i], form.what, strings.TrimSpace(line)))
			}
		}
	}
	return problems
}

// topLevel is a workflow's text before its jobs.
func topLevel(text string) string {
	before, _, _ := strings.Cut(text, "\njobs:")
	return before
}

// topKeys are a workflow's top-level keys, with their values on the key's
// line.
func topKeys(text string) map[string]string {
	keys := map[string]string{}
	lines, _ := structural(text)
	for _, line := range lines {
		if !strings.HasPrefix(line, " ") {
			name, value, _ := strings.Cut(line, ":")
			keys[name] = strings.TrimSpace(value)
		}
	}
	return keys
}

// setsEnv says whether text, outside its comments, names GOFLAGS, which
// adds flags to every go test, or GITHUB_ENV, through which a step sets the
// environment of the steps after it.
func setsEnv(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if line, _, _ = strings.Cut(line, "#"); strings.Contains(line, "GOFLAGS") || strings.Contains(line, "GITHUB_ENV") {
			return true
		}
	}
	return false
}

// listed reads a YAML flow list, or a single value, as its items.
func listed(value string) []string {
	return strings.FieldsFunc(strings.Trim(strings.TrimSpace(value), "[]"), func(r rune) bool {
		return r == ',' || r == ' ' || r == '"' || r == '\''
	})
}

// triggers holds a workflow's on: to the run of record: it runs on every
// push to main and on every pull request, with no filter on either but the
// push's branches, which name main.
func triggers(text string) []string {
	lines, _ := structural(text)
	at := slices.IndexFunc(lines, func(l string) bool { return l == "on:" || strings.HasPrefix(l, "on: ") })
	if at < 0 {
		return []string{"the workflow has no on:, so no push or pull request runs it"}
	}
	if inline := strings.TrimSpace(strings.TrimPrefix(lines[at], "on:")); inline != "" {
		if names := listed(inline); slices.Contains(names, "push") && slices.Contains(names, "pull_request") {
			return nil
		}
		return []string{fmt.Sprintf("the workflow runs on %s, not on every push to main and every pull request", inline)}
	}
	// under holds each event's lines, its value on the event's line first.
	under := map[string][]string{}
	event := ""
	for _, line := range lines[at+1:] {
		if !strings.HasPrefix(line, " ") {
			break
		}
		if !strings.HasPrefix(line, "   ") {
			name, value, _ := strings.Cut(strings.TrimSpace(line), ":")
			event = name
			under[event] = nil
			if value = strings.TrimSpace(value); value != "" {
				under[event] = append(under[event], value)
			}
			continue
		}
		under[event] = append(under[event], strings.TrimSpace(line))
	}
	var problems []string
	push, ok := under["push"]
	if !ok {
		problems = append(problems, "the workflow does not run on a push")
	}
	main := len(push) == 0
	for i, line := range push {
		name, value, _ := strings.Cut(line, ":")
		switch {
		case name == "branches" && strings.TrimSpace(value) != "":
			main = slices.Contains(listed(value), "main")
		case name == "branches":
			for _, item := range push[i+1:] {
				if !strings.HasPrefix(item, "- ") {
					break
				}
				main = main || slices.Contains(listed(strings.TrimPrefix(item, "- ")), "main")
			}
		case strings.HasPrefix(line, "- ") && i > 0:
		default:
			problems = append(problems, fmt.Sprintf("the workflow's push is filtered by %q", line))
		}
	}
	if ok && !main {
		problems = append(problems, "the workflow does not run on a push to main")
	}
	pr, ok := under["pull_request"]
	if !ok {
		problems = append(problems, "the workflow does not run on a pull request")
	}
	for _, line := range pr {
		problems = append(problems, fmt.Sprintf("the workflow's pull_request is filtered by %q", line))
	}
	return problems
}

// checkCI holds a ci.yml's text to the run of record, and returns what it
// lacks.
func checkCI(text string) []string {
	problems := unplain(text)
	problems = append(problems, triggers(text)...)
	if setsEnv(topLevel(text)) {
		problems = append(problems, "the workflow sets GOFLAGS or the environment of its steps for every job")
	}
	if _, ok := topKeys(text)["defaults"]; ok {
		problems = append(problems, "the workflow gives every job's steps a folder or a shell of their own (defaults)")
	}
	permissions, jobs := ciJobs(text)
	if !slices.Equal(permissions, []string{"contents: read"}) {
		problems = append(problems, fmt.Sprintf("the workflow's token is %q, not contents: read alone", permissions))
	}
	var mac, linux []ciJob
	for _, j := range jobs {
		_, has := j.keys["permissions"]
		if has {
			var lines []string
			if v := j.keys["permissions"]; v != "" {
				lines = append(lines, v)
			}
			in := false
			for _, line := range strings.Split(j.text, "\n") {
				switch {
				case strings.HasPrefix(line, "    permissions:"):
					in = true
				case in && strings.HasPrefix(line, "      ") && !strings.HasPrefix(strings.TrimSpace(line), "-"):
					lines = append(lines, line)
				default:
					in = false
				}
			}
			for _, line := range lines {
				if w := widens(line); w != "" {
					problems = append(problems, fmt.Sprintf("job %s asks for %s", j.name, w))
				}
			}
		}
		switch runsOn := j.keys["runs-on"]; {
		case strings.HasPrefix(runsOn, "macos-"):
			mac = append(mac, j)
		case strings.HasPrefix(runsOn, "ubuntu-"):
			linux = append(linux, j)
		}
	}
	// A job runs the suite, or the script, when one of its steps does, with
	// no way for the step or the job to fail and pass, to be left out, or
	// to run less than it names: no continue-on-error and no if, on the
	// step or the job, no job it waits on, no folder or shell of its own,
	// and nothing in the job that sets GOFLAGS or the environment of the
	// steps after it.
	sure := func(j ciJob, s workflowStep) bool {
		for _, keys := range []map[string]string{j.keys, s.keys} {
			if v, ok := keys["continue-on-error"]; ok && v != "false" {
				return false
			}
			if _, ok := keys["if"]; ok {
				return false
			}
		}
		for _, key := range []string{"needs", "defaults"} {
			if _, ok := j.keys[key]; ok {
				return false
			}
		}
		for _, key := range []string{"working-directory", "shell"} {
			if _, ok := s.keys[key]; ok {
				return false
			}
		}
		return !setsEnv(j.text)
	}
	var plain, race bool
	for _, j := range mac {
		for _, s := range jobSteps(j.text) {
			// The suite counts only as the whole of its step's run: in a
			// block of several lines a set +e before it, or an exit 0
			// after it, would let the step pass whatever the tests do.
			run := strings.TrimSpace(stepRun(s))
			if strings.Contains(run, "\n") {
				continue
			}
			if ok, r := fullSuite(run); ok && sure(j, s) {
				plain, race = plain || !r, race || r
			}
		}
	}
	if !plain {
		problems = append(problems, "no macOS job runs go test ./... in full with nothing that lets it fail and pass")
	}
	if !race {
		problems = append(problems, "no macOS job runs go test -race ./... in full with nothing that lets it fail and pass")
	}
	script := false
	for _, j := range linux {
		for _, s := range jobSteps(j.text) {
			if strings.TrimSpace(stepRun(s)) == "scripts/linux-check.sh" && sure(j, s) {
				script = true
			}
		}
	}
	if !script {
		problems = append(problems, "no Linux job runs scripts/linux-check.sh whole with nothing that lets it fail and pass")
	}
	return problems
}

// ci.yml's token reads the repository and no more, in every job; its macOS
// job runs the whole suite plainly and with the race detector, with no
// step that may fail and pass and no filter; and its Linux job runs the
// script a cloud session runs.
func TestCIIsTheRunOfRecord(t *testing.T) {
	text := repoText(t, ".github", "workflows", "ci.yml")
	for _, p := range checkCI(text) {
		t.Error(p)
	}
	if _, jobs := ciJobs(text); len(jobs) < 3 {
		t.Errorf("read %d jobs of ci.yml", len(jobs))
	}
}

// The check fails a ci.yml that widens the token, at the top or in a job,
// or runs the suite in a way that may pass on a failure, be left out or
// run only in part: a step or a job that may fail, runs only on a
// condition or after another job, a folder, a shell or GOFLAGS of its own,
// or a step that sets the environment of those after it; one whose Linux
// job runs the script in part, not at all or only on a condition; one that
// no push to main or pull request runs, or only some; and one written in a
// form of YAML its reader cannot read as GitHub does.
func TestACIThatIsNoRunOfRecordIsFound(t *testing.T) {
	text := repoText(t, ".github", "workflows", "ci.yml")
	if p := checkCI(text); len(p) > 0 {
		t.Fatalf("ci.yml fails before any fault is seeded: %q", p)
	}
	const suite, raceSuite = "      - run: go test -timeout 40m ./...\n", "      - run: go test -race -timeout 40m ./...\n"
	const script = "      - run: scripts/linux-check.sh\n"
	for _, c := range []struct {
		name     string
		old, new string
		want     []string
	}{
		{"the token writes contents", "permissions:\n  contents: read\n", "permissions:\n  contents: write\n", []string{`^the workflow's token is \["contents: write"\]`}},
		{"the token gets another scope", "permissions:\n  contents: read\n", "permissions:\n  contents: read\n  id-token: write\n", []string{`^the workflow's token is \["contents: read" "id-token: write"\]`}},
		{"the token writes all", "permissions:\n  contents: read\n", "permissions: write-all\n", []string{`^the workflow's token is \["write-all"\]`}},
		{"a job asks for more", "  guards:\n    runs-on: ubuntu-latest\n", "  guards:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: write\n", []string{`^job guards asks for contents: write$`}},
		{"a job asks for all", "  latency:\n    runs-on: macos-latest\n", "  latency:\n    runs-on: macos-latest\n    permissions: write-all\n", []string{`^job latency asks for write-all$`}},
		{"the suite filtered", suite, "      - run: go test -timeout 40m -run TestA ./...\n", []string{`^no macOS job runs go test \./\.\.\. in full`}},
		{"the suite cut to one package", suite, "      - run: go test -timeout 40m ./cmd/...\n", []string{`^no macOS job runs go test \./\.\.\. in full`}},
		{"the suite short", raceSuite, "      - run: go test -race -short -timeout 40m ./...\n", []string{`^no macOS job runs go test -race \./\.\.\. in full`}},
		{"the suite may fail", suite, "      - run: go test -timeout 40m ./...\n        continue-on-error: true\n", []string{`^no macOS job runs go test \./\.\.\. in full`}},
		{"the suite only now and then", raceSuite, "      - run: go test -race -timeout 40m ./...\n        if: github.event_name == 'pull_request'\n", []string{`^no macOS job runs go test -race \./\.\.\. in full`}},
		{"the job may fail", "  test:\n    runs-on: macos-latest\n", "  test:\n    runs-on: macos-latest\n    continue-on-error: true\n", []string{`^no macOS job runs go test \./\.\.\. in full`, `^no macOS job runs go test -race \./\.\.\. in full`}},
		{"the suite on Linux only", "  test:\n    runs-on: macos-latest\n", "  test:\n    runs-on: ubuntu-latest\n", []string{`^no macOS job runs go test \./\.\.\. in full`, `^no macOS job runs go test -race \./\.\.\. in full`}},
		{"the script in part", script, "      - run: scripts/linux-check.sh ./internal/hook\n", []string{`^no Linux job runs scripts/linux-check\.sh whole`}},
		{"the script not at all", script, "      - run: go test ./...\n", []string{`^no Linux job runs scripts/linux-check\.sh whole`}},
		{"the macOS job never runs", "  test:\n    runs-on: macos-latest\n", "  test:\n    runs-on: macos-latest\n    if: false\n", []string{`^no macOS job runs go test \./\.\.\. in full`, `^no macOS job runs go test -race \./\.\.\. in full`}},
		{"the macOS job only when asked", "  test:\n    runs-on: macos-latest\n", "  test:\n    runs-on: macos-latest\n    if: github.event_name == 'workflow_dispatch'\n", []string{`^no macOS job runs go test \./\.\.\. in full`, `^no macOS job runs go test -race \./\.\.\. in full`}},
		{"the macOS job waits on another", "  test:\n    runs-on: macos-latest\n", "  test:\n    runs-on: macos-latest\n    needs: guards\n", []string{`^no macOS job runs go test \./\.\.\. in full`, `^no macOS job runs go test -race \./\.\.\. in full`}},
		{"the Linux job never runs", "  test-linux:\n    runs-on: ubuntu-latest\n", "  test-linux:\n    runs-on: ubuntu-latest\n    if: false\n", []string{`^no Linux job runs scripts/linux-check\.sh whole`}},
		{"GOFLAGS for every job", "permissions:\n  contents: read\n", "permissions:\n  contents: read\n\nenv:\n  GOFLAGS: -short -run=TestNothing\n", []string{`^the workflow sets GOFLAGS or the environment of its steps for every job$`}},
		{"GOFLAGS for the macOS job", "    timeout-minutes: 60\n", "    timeout-minutes: 60\n    env:\n      GOFLAGS: -run=TestNothing\n", []string{`^no macOS job runs go test \./\.\.\. in full`, `^no macOS job runs go test -race \./\.\.\. in full`}},
		{"GOFLAGS for the Linux job's step", script, script + "        env:\n          GOFLAGS: -short\n", []string{`^no Linux job runs scripts/linux-check\.sh whole`}},
		{"a step that sets the steps after it", suite, "      - run: echo GOFLAGS=-short >> \"$GITHUB_ENV\"\n" + suite, []string{`^no macOS job runs go test \./\.\.\. in full`, `^no macOS job runs go test -race \./\.\.\. in full`}},
		{"the suite in a folder", suite, suite + "        working-directory: internal/spec\n", []string{`^no macOS job runs go test \./\.\.\. in full`}},
		{"the suite with a shell of its own", raceSuite, raceSuite + "        shell: bash -c 'exit 0' {0}\n", []string{`^no macOS job runs go test -race \./\.\.\. in full`}},
		{"every job's steps in a folder", "permissions:\n  contents: read\n", "permissions:\n  contents: read\n\ndefaults:\n  run:\n    working-directory: internal/spec\n", []string{`^the workflow gives every job's steps a folder or a shell of their own \(defaults\)$`}},
		{"a quoted continue-on-error", suite, suite + "        \"continue-on-error\": true\n", []string{`^line [0-9]+ holds a quoted key, which this check cannot read as GitHub does: "\\"continue-on-error\\": true"$`}},
		{"a quoted permissions", "  guards:\n    runs-on: ubuntu-latest\n", "  guards:\n    runs-on: ubuntu-latest\n    'permissions': write-all\n", []string{`^line [0-9]+ holds a quoted key`}},
		{"an if with a space before its colon", "  test:\n    runs-on: macos-latest\n", "  test:\n    runs-on: macos-latest\n    if : false\n", []string{`^line [0-9]+ holds a key with a space before its colon, which this check cannot read as GitHub does: "if : false"$`}},
		{"a continue-on-error with a space before its colon", suite, suite + "        continue-on-error : true\n", []string{`^line [0-9]+ holds a key with a space before its colon`}},
		{"the suite in a block that passes whatever it does", suite, "      - run: |\n          set +e\n          go test -timeout 40m ./...\n          exit 0\n", []string{`^no macOS job runs go test \./\.\.\. in full`}},
		{"the suite in a block with a line after it", raceSuite, "      - run: |\n          go test -race -timeout 40m ./...\n          true\n", []string{`^no macOS job runs go test -race \./\.\.\. in full`}},
		{"a merge key", "  test:\n    runs-on: macos-latest\n", "  test:\n    runs-on: macos-latest\n    <<: *lenient\n", []string{`^line [0-9]+ holds an anchor, an alias or a merge key`}},
		{"a step as a flow mapping", suite, "      - {run: go test -timeout 40m ./..., continue-on-error: true}\n", []string{`^line [0-9]+ holds a flow mapping`, `^no macOS job runs go test \./\.\.\. in full`}},
		{"a tab in an indent", "  test:\n    runs-on: macos-latest\n", "  test:\n\truns-on: macos-latest\n", []string{`^line [0-9]+ holds a tab in its indent`, `^no macOS job runs go test \./\.\.\. in full`, `^no macOS job runs go test -race \./\.\.\. in full`, `^no Linux job runs scripts/linux-check\.sh whole`}},
		{"no push or pull request runs it", "on:\n  push:\n    branches: [main]\n  pull_request:\n", "on:\n  workflow_dispatch:\n", []string{`^the workflow does not run on a push$`, `^the workflow does not run on a pull request$`}},
		{"no push to main runs it", "    branches: [main]\n", "    branches: [release]\n", []string{`^the workflow does not run on a push to main$`}},
		{"a push only to some paths", "    branches: [main]\n", "    branches: [main]\n    paths: ['docs/**']\n", []string{`^the workflow's push is filtered by "paths: \['docs/\*\*'\]"$`}},
		{"a pull request only when closed", "  pull_request:\n", "  pull_request:\n    types: [closed]\n", []string{`^the workflow's pull_request is filtered by "types: \[closed\]"$`}},
		{"an inline on of one event", "on:\n  push:\n    branches: [main]\n  pull_request:\n", "on: [workflow_dispatch]\n", []string{`^the workflow runs on \[workflow_dispatch\], not on every push`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(text, c.old) {
				t.Fatalf("ci.yml doesn't hold %q", c.old)
			}
			problems := checkCI(strings.Replace(text, c.old, c.new, 1))
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

// A go test line is read as the whole suite only with ./... and flags that
// filter nothing.
func TestFullSuiteIsTold(t *testing.T) {
	for line, want := range map[string][2]bool{
		"go test ./...":                         {true, false},
		"go test -race -timeout 40m ./...":      {true, true},
		"go test -count=1 -v -timeout=5m ./...": {true, false},
		"go test -run TestX ./...":              {false, false},
		"go test -skip TestX ./...":             {false, false},
		"go test -tags evamd_eval ./...":        {false, false},
		"go test ./internal/...":                {false, false},
		"go test":                               {false, false},
		"go vet ./...":                          {false, false},
	} {
		if ok, race := fullSuite(line); ok != want[0] || race != want[1] {
			t.Errorf("%q read as whole %v, race %v; want %v", line, ok, race, want)
		}
	}
}
