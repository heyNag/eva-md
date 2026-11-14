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
// fail or filters its tests, and its Linux job runs the script a cloud
// session runs, whole.

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

// checkCI holds a ci.yml's text to the run of record, and returns what it
// lacks.
func checkCI(text string) []string {
	var problems []string
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
	// no way for the step or the job to fail and pass.
	sure := func(j ciJob, s workflowStep) bool {
		for _, keys := range []map[string]string{j.keys, s.keys} {
			if v, ok := keys["continue-on-error"]; ok && v != "false" {
				return false
			}
		}
		_, cond := s.keys["if"]
		return !cond
	}
	var plain, race bool
	for _, j := range mac {
		for _, s := range jobSteps(j.text) {
			for _, line := range strings.Split(stepRun(s), "\n") {
				if ok, r := fullSuite(line); ok && sure(j, s) {
					plain, race = plain || !r, race || r
				}
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
// or runs the suite in a way that may pass on a failure, or only in part,
// and one whose Linux job runs the script in part or not at all.
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
