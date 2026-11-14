package release

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The release workflow's build job reads each binary it built before the
// upload, so a binary built with a build tag, the evaluation build's above
// all, is never published: Build's SHA256SUMS, which verify checks, ties
// what the step read to what the release holds. These tests hold where the
// step stands and that nothing lets a release go on past it, then run it as
// the runner does on real and made-up binaries.

// tagCheckName is the step's name.
const tagCheckName = "Check no binary carries a build tag"

// tagCheckHangGuard is how long a test waits for a build or the step
// before it fails naming what hung.
const tagCheckHangGuard = 5 * time.Minute

// workflowStep is one step of a job: its keys at the step's own level, with
// their values on the key's line, and its lines.
type workflowStep struct {
	keys  map[string]string
	lines []string
}

// jobSteps reads the steps of a job's text, as workflowJobs gives it: each
// starts with "- " at six spaces of indent and has its keys at eight.
func jobSteps(text string) []workflowStep {
	var steps []workflowStep
	start := regexp.MustCompile(`^      - ([A-Za-z0-9_-]+):(?: (.*))?$`)
	key := regexp.MustCompile(`^        ([A-Za-z0-9_-]+):(?: (.*))?$`)
	inSteps := false
	for _, line := range strings.Split(text, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case line == "    steps:":
			inSteps = true
			continue
		case !inSteps:
			continue
		case strings.TrimSpace(line) != "" && indent <= 4:
			inSteps = false
			continue
		}
		if m := start.FindStringSubmatch(line); m != nil {
			steps = append(steps, workflowStep{keys: map[string]string{m[1]: m[2]}})
		} else if len(steps) == 0 {
			continue
		} else if m := key.FindStringSubmatch(line); m != nil {
			steps[len(steps)-1].keys[m[1]] = m[2]
		}
		steps[len(steps)-1].lines = append(steps[len(steps)-1].lines, line)
	}
	return steps
}

// runBlock is a step's run block, its lines without their indent, as the
// runner writes it to the script it runs.
func (s workflowStep) runBlock() (string, error) {
	key := regexp.MustCompile(`^(?:      - |        )run:`)
	at := slices.IndexFunc(s.lines, key.MatchString)
	if at < 0 {
		return "", errors.New("it has no run")
	}
	if v := strings.TrimSpace(s.keys["run"]); v != "|" && v != "|-" {
		return "", fmt.Errorf("its run is %q, not a block", v)
	}
	var block []string
	indent := -1
	for _, line := range s.lines[at+1:] {
		if strings.TrimSpace(line) == "" {
			block = append(block, "")
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, " "))
		if indent < 0 {
			indent = n
		}
		if n < indent || indent <= 8 {
			break
		}
		block = append(block, line[indent:])
	}
	text := strings.TrimRight(strings.Join(block, "\n"), "\n")
	if text == "" {
		return "", errors.New("its run block is empty")
	}
	return text + "\n", nil
}

// jobNeeds gives the jobs a job needs, written on the key's line, as one
// name or a list in brackets, or as a list on the lines after it.
func jobNeeds(j *workflowJob) []string {
	v, ok := j.keys["needs"]
	if !ok {
		return nil
	}
	if v = strings.TrimSpace(v); v != "" {
		var names []string
		for _, n := range strings.Split(strings.Trim(v, "[]"), ",") {
			if n = strings.Trim(strings.TrimSpace(n), `'"`); n != "" {
				names = append(names, n)
			}
		}
		return names
	}
	var names []string
	in := false
	item := regexp.MustCompile(`^      - (\S+)$`)
	for _, line := range strings.Split(j.text, "\n") {
		if line == "    needs:" {
			in = true
			continue
		}
		if !in {
			continue
		}
		m := item.FindStringSubmatch(line)
		if m == nil {
			break
		}
		names = append(names, strings.Trim(m[1], `'"`))
	}
	return names
}

// statusFunctions are the functions that, named in a job's if, take the
// place of the success() GitHub puts before a condition, and so can run a
// job whose needed jobs failed.
var statusFunctions = []string{"success(", "failure(", "cancelled(", "always("}

// conditionGap gives what is wrong with a job's if, as its line holds it,
// or "". A condition that names a status function must have !failure() as
// one of its parts joined by && and no || outside brackets, and must not
// name always(): so no job runs once one it needs has failed.
func conditionGap(value string) string {
	v := strings.TrimSpace(value)
	if v == "" || strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
		return "is not on one line"
	}
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	if inner, ok := strings.CutPrefix(v, "${{"); ok {
		if inner, ok = strings.CutSuffix(inner, "}}"); !ok {
			return "is not one expression"
		}
		v = inner
	}
	// The expression, lowercase with no blanks outside its strings, as
	// GitHub reads a function's name in any case and blanks between
	// tokens.
	var expr strings.Builder
	quoted := false
	for _, r := range v {
		switch {
		case r == '\'':
			quoted = !quoted
		case !quoted && (r == ' ' || r == '\t'):
			continue
		case !quoted:
			r = []rune(strings.ToLower(string(r)))[0]
		}
		expr.WriteRune(r)
	}
	e := expr.String()
	if !slices.ContainsFunc(statusFunctions, func(f string) bool { return strings.Contains(e, f) }) {
		return ""
	}
	if strings.Contains(e, "always(") {
		return "names always()"
	}
	var parts []string
	depth, quoted, last := 0, false, 0
	for i := 0; i < len(e); i++ {
		switch c := e[i]; {
		case c == '\'':
			quoted = !quoted
		case quoted:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.HasPrefix(e[i:], "||"):
			return "has || outside brackets"
		case depth == 0 && strings.HasPrefix(e[i:], "&&"):
			parts = append(parts, e[last:i])
			last, i = i+2, i+1
		}
	}
	parts = append(parts, e[last:])
	if !slices.Contains(parts, "!failure()") {
		return "names a status function without !failure() as one of its parts joined by &&"
	}
	return ""
}

// tagCheckGaps gives what is wrong with where the release workflow's text
// stands the tag check, or what could let a release go on past it, with
// marks the strings the step must look for.
func tagCheckGaps(t *testing.T, workflow string, marks []string) []string {
	t.Helper()
	var gaps []string
	if n := strings.Count(workflow, "- name: "+tagCheckName+"\n"); n != 1 {
		gaps = append(gaps, fmt.Sprintf("the workflow has %d steps named %q, not one", n, tagCheckName))
	}
	if !regexp.MustCompile(`(?m)^defaults:\n  run:\n    shell: bash\n`).MatchString(workflow) {
		gaps = append(gaps, "the workflow's defaults are no longer shell: bash")
	}
	jobs := workflowJobs(t, workflow)
	build, ok := jobs["build"]
	if !ok {
		return append(gaps, "the workflow has no build job")
	}
	if _, ok := build.keys["defaults"]; ok {
		gaps = append(gaps, "the build job has defaults of its own")
	}
	gaps = append(gaps, changeGaps(workflow, jobs)...)
	for name, j := range jobs {
		if _, ok := j.keys["continue-on-error"]; ok {
			gaps = append(gaps, "job "+name+" has continue-on-error")
		}
		if v, ok := j.keys["if"]; ok {
			if gap := conditionGap(v); gap != "" {
				gaps = append(gaps, fmt.Sprintf("job %s's if %s: %q", name, gap, v))
			}
		}
	}
	for _, name := range []string{"verify", "attest", "release"} {
		if _, ok := jobs[name]; !ok {
			gaps = append(gaps, "the workflow has no job "+name)
			continue
		}
		seen := map[string]bool{}
		next := []string{name}
		for len(next) > 0 {
			n := next[0]
			next = next[1:]
			if seen[n] || jobs[n] == nil {
				continue
			}
			seen[n] = true
			next = append(next, jobNeeds(jobs[n])...)
		}
		if !seen["build"] {
			gaps = append(gaps, "job "+name+" no longer needs build")
		}
	}

	steps := jobSteps(build.text)
	at := func(match func(workflowStep) bool) []int {
		var found []int
		for i, s := range steps {
			if match(s) {
				found = append(found, i)
			}
		}
		return found
	}
	checks := at(func(s workflowStep) bool { return s.keys["name"] == tagCheckName })
	builds := at(func(s workflowStep) bool { return s.keys["name"] == "Build" })
	uploads := at(func(s workflowStep) bool { return strings.HasPrefix(s.keys["uses"], "actions/upload-artifact@") })
	if len(checks) != 1 || len(builds) != 1 || len(uploads) != 1 {
		return append(gaps, fmt.Sprintf("the build job has %d steps named %q, %d named Build and %d uploads, not one each", len(checks), tagCheckName, len(builds), len(uploads)))
	}
	// The step is the one right before the upload, so no step can change a
	// binary or SHA256SUMS after the step has read them.
	if !(builds[0] < checks[0] && checks[0]+1 == uploads[0]) {
		gaps = append(gaps, "the step does not stand after Build and right before the upload")
	}
	if !lines("- name: Build", "...", "- name: "+tagCheckName, "...", "- uses: actions/upload-artifact@").MatchString(build.text) {
		gaps = append(gaps, "the build job's lines do not run Build, the step and the upload in that order")
	}
	step := steps[checks[0]]
	var keys []string
	for k := range step.keys {
		keys = append(keys, k)
	}
	if slices.Sort(keys); !slices.Equal(keys, []string{"name", "run"}) {
		gaps = append(gaps, fmt.Sprintf("the step has the keys %q, not name and run alone", keys))
	}
	gaps = append(gaps, stepLineGaps(step)...)
	run, err := step.runBlock()
	if err != nil {
		return append(gaps, "the step: "+err.Error())
	}
	if !strings.Contains(run, "go version -m ") {
		gaps = append(gaps, "the step does not run go version -m")
	}
	for _, mark := range marks {
		if !strings.Contains(run, mark) {
			gaps = append(gaps, "the step does not look for "+mark)
		}
	}
	if strings.Contains(run, "${{") {
		gaps = append(gaps, "the step's run holds an expression, so a test cannot run what the runner runs")
	}
	return append(gaps, runReadGaps(run)...)
}

// stepLineGaps gives the lines of the tag check's step that are neither
// its name line, its run line, its run block, a blank nor a comment: a key
// the test's reading skips, written in quotes, with a blank before its
// colon or as a flow mapping, which the runner still reads.
func stepLineGaps(step workflowStep) []string {
	var gaps []string
	nameLine, runLine := "      - name: "+tagCheckName, "        run: |"
	seen := map[string]int{}
	inRun, blockIndent := false, -1
	for _, line := range step.lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if inRun {
			if trimmed == "" {
				continue
			}
			if blockIndent < 0 {
				blockIndent = indent
			}
			if indent >= blockIndent && blockIndent > 8 {
				continue
			}
			inRun = false
		}
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		case line == nameLine || line == runLine:
			seen[line]++
			inRun = line == runLine
		default:
			gaps = append(gaps, fmt.Sprintf("the step has a line that is not its name, its run or its run block: %q", line))
		}
	}
	if seen[nameLine] != 1 || seen[runLine] != 1 {
		gaps = append(gaps, fmt.Sprintf("the step has %d lines %q and %d lines %q, not one each", seen[nameLine], nameLine, seen[runLine], runLine))
	}
	return gaps
}

// tagCheckNames are the variables the step's run may read: those it sets
// itself, and RUNNER_TEMP, where Build writes the binaries. A run that read
// any other, such as TAG or the runner's GITHUB_ ones, could pass every
// binary on the runner and still fail every bad case of a test.
var tagCheckNames = []string{"RUNNER_TEMP", "bad", "bin", "bins", "found", "info", "line", "mark", "name", "nl", "ours", "path", "setting", "tab", "tags_re", "want"}

// runReadGaps gives what the step's run reads from its environment beyond
// tagCheckNames, by name or through a command that reads variables it
// does not name.
func runReadGaps(run string) []string {
	var code []string
	for _, line := range strings.Split(run, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			code = append(code, line)
		}
	}
	text := strings.Join(code, "\n")
	var gaps []string
	for _, m := range regexp.MustCompile(`\$\{?#?([A-Za-z_][A-Za-z0-9_]*)`).FindAllStringSubmatch(text, -1) {
		if !slices.Contains(tagCheckNames, m[1]) {
			gaps = append(gaps, fmt.Sprintf("the step's run reads $%s, which it does not set", m[1]))
		}
	}
	for _, m := range regexp.MustCompile(`\$\{!|\b(?:printenv|env|export|declare|typeset|compgen|eval|source)\b|(?:\[\[?|\btest)\s+(?:!\s+)?-[vR]\s`).FindAllString(text, -1) {
		gaps = append(gaps, fmt.Sprintf("the step's run reads variables it does not name, with %q", m))
	}
	return gaps
}

// changeGaps gives the earlier steps', jobs' and workflow's settings that
// can change what the step runs: a job's or the workflow's line that the
// test's reading skips, a write to the runner's PATH or environment files,
// BASH_ENV, which bash reads before the step's script, an env of the build
// job, or a workflow env beyond TAG.
func changeGaps(workflow string, jobs map[string]*workflowJob) []string {
	var gaps []string
	jobKey := regexp.MustCompile(`^    [a-z-]+:(?: |$)`)
	for name, j := range jobs {
		for _, line := range strings.Split(j.text, "\n") {
			trimmed := strings.TrimSpace(line)
			indent := len(line) - len(strings.TrimLeft(line, " "))
			if trimmed == "" || strings.HasPrefix(trimmed, "#") || indent >= 6 || jobKey.MatchString(line) {
				continue
			}
			gaps = append(gaps, fmt.Sprintf("job %s has a line that is not one of its keys: %q", name, line))
		}
	}
	if build := jobs["build"]; build != nil {
		if _, ok := build.keys["env"]; ok {
			gaps = append(gaps, "the build job has an env of its own")
		}
		for _, file := range []string{"GITHUB_PATH", "GITHUB_ENV"} {
			if strings.Contains(build.text, file) {
				gaps = append(gaps, "the build job names "+file+", so a step can change what later steps run")
			}
		}
	}
	if strings.Contains(workflow, "BASH_ENV") {
		gaps = append(gaps, "the workflow names BASH_ENV, which bash reads before the step's script")
	}
	in := false
	envKey := regexp.MustCompile(`^  ([A-Za-z_][A-Za-z0-9_]*):(?: |$)`)
	for _, line := range strings.Split(workflow, "\n") {
		if line == "env:" {
			in = true
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !in || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			in = false
			continue
		}
		if m := envKey.FindStringSubmatch(line); m == nil || m[1] != "TAG" {
			gaps = append(gaps, fmt.Sprintf("the workflow's env has %q, not TAG alone", line))
		}
	}
	if !regexp.MustCompile(`(?m)^env:\n  TAG: `).MatchString(workflow) || strings.Count(workflow, "\nenv:") != 1 {
		gaps = append(gaps, "the workflow's env is not TAG alone, once")
	}
	return gaps
}

// tagCheckRun is the step's run block in the build job of the workflow's
// text.
func tagCheckRun(t *testing.T, workflow string) string {
	t.Helper()
	for _, s := range jobSteps(workflowJobs(t, workflow)["build"].text) {
		if s.keys["name"] == tagCheckName {
			run, err := s.runBlock()
			if err != nil {
				t.Fatalf("the release workflow's step %q: %v", tagCheckName, err)
			}
			return run
		}
	}
	t.Fatalf("the release workflow's build job has no step %q", tagCheckName)
	return ""
}

// repoText is a file of the repository, by its path from the root.
func repoText(t *testing.T, path ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, path...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// evalMarks are the strings of evalMarks in cmd/eva-md/evalbuild_test.go,
// read from its text: what the evaluation build's code leaves in a binary.
func evalMarks(t *testing.T) []string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^var evalMarks = \[\]string\{(.*)\}$`).FindAllStringSubmatch(repoText(t, "cmd", "eva-md", "evalbuild_test.go"), -1)
	if len(m) != 1 {
		t.Fatalf("found evalMarks %d times in cmd/eva-md/evalbuild_test.go, not once", len(m))
	}
	var marks []string
	for _, q := range regexp.MustCompile(`"(?:[^"\\]|\\.)*"`).FindAllString(m[0][1], -1) {
		s, err := strconv.Unquote(q)
		if err != nil {
			t.Fatal(err)
		}
		marks = append(marks, s)
	}
	if len(marks) < 2 {
		t.Fatalf("read the marks %q from cmd/eva-md/evalbuild_test.go", marks)
	}
	return marks
}

// The step stands in the build job, after Build and before the upload,
// with nothing that lets it fail without failing the job, it reads each
// binary for each mark, and every job that publishes needs build and runs
// only when no job it needs failed. Each change here, any one of which
// would let a tagged binary through, is caught.
func TestReleaseTagCheckStandsBeforeTheUpload(t *testing.T) {
	workflow := repoText(t, ".github", "workflows", "release.yml")
	marks := evalMarks(t)
	if gaps := tagCheckGaps(t, workflow, marks); gaps != nil {
		t.Fatalf("release.yml: %q", gaps)
	}

	step := "      - name: " + tagCheckName + "\n" + "        run: |\n"
	stepAt := strings.Index(workflow, step)
	end := strings.Index(workflow, "      - uses: actions/upload-artifact@")
	if stepAt < 0 || end < stepAt {
		t.Fatal("found no step to move")
	}
	whole := workflow[stepAt:end]
	without := workflow[:stepAt] + workflow[end:]
	insert := func(text, before, what string) string {
		t.Helper()
		i := strings.Index(text, before)
		if i < 0 {
			t.Fatalf("found no %q", before)
		}
		return text[:i] + what + text[i:]
	}
	replace := func(old, new string) string {
		t.Helper()
		if !strings.Contains(workflow, old) {
			t.Fatalf("found no %q", old)
		}
		return strings.Replace(workflow, old, new, 1)
	}
	changes := map[string]string{
		"the step gone":           without,
		"the step renamed":        replace("- name: "+tagCheckName+"\n", "- name: Check the binaries\n"),
		"the step before Build":   insert(without, "      - name: Build\n", whole),
		"the step after upload":   insert(without, "\n  verify:\n", whole),
		"the step in verify":      insert(without, "\n  attest:\n", whole),
		"the step twice":          insert(workflow, "\n  attest:\n", whole),
		"the step with if":        replace(step, "      - name: "+tagCheckName+"\n        if: always()\n        run: |\n"),
		"continue-on-error":       replace(step, "      - name: "+tagCheckName+"\n        continue-on-error: true\n        run: |\n"),
		"the step with shell":     replace(step, "      - name: "+tagCheckName+"\n        shell: sh\n        run: |\n"),
		"the step with env":       replace(step, "      - name: "+tagCheckName+"\n        env:\n          X: 'y'\n        run: |\n"),
		"no go version -m":        strings.ReplaceAll(workflow, "go version -m ", "go version "),
		"an expression in run":    replace("bad=0\n", "bad=${{ env.TAG && 0 }}\n"),
		"a job continuing":        replace("    needs: verify\n", "    needs: verify\n    continue-on-error: true\n"),
		"the build job continues": replace("    timeout-minutes: 15\n", "    timeout-minutes: 15\n    continue-on-error: true\n"),
		"verify needs nothing":    replace("    needs: build\n", ""),
		"attest needs nothing":    replace("    needs: verify\n", ""),
		"release needs nothing":   replace("    needs: attest\n", ""),
		"verify needs another":    replace("    needs: build\n", "    needs: [other]\n"),
		"defaults sh":             replace("defaults:\n  run:\n    shell: bash\n", "defaults:\n  run:\n    shell: sh\n"),
		"no defaults":             replace("defaults:\n  run:\n    shell: bash\n", ""),
		"build's own defaults":    replace("    timeout-minutes: 15\n", "    timeout-minutes: 15\n    defaults:\n      run:\n        shell: sh\n"),
		"the step's run a line":   replace(step, "      - name: "+tagCheckName+"\n        run: go version -m x /cmd/eva-md/evalbuild EVA_MD_EVAL_ACTOR\n        x: |\n"),

		// A step after the check rewrites a binary that the check read.
		"a step between the check and the upload": insert(workflow, "      - uses: actions/upload-artifact@",
			"      - name: Replace a binary\n        run: cp /dev/null \"$RUNNER_TEMP/release/eva-md-darwin-arm64\"\n"),
		"the build job's env":        replace("    timeout-minutes: 15\n", "    timeout-minutes: 15\n    env:\n      PATH: /opt/tools\n"),
		"a step writes GITHUB_PATH":  insert(workflow, "      - name: Build\n", "      - name: Tools\n        run: echo /opt/tools >> \"$GITHUB_PATH\"\n"),
		"a step writes GITHUB_ENV":   insert(workflow, "      - name: Build\n", "      - name: Tools\n        run: echo PATH=/opt/tools >> \"$GITHUB_ENV\"\n"),
		"BASH_ENV in the workflow":   replace("env:\n  TAG: ", "env:\n  BASH_ENV: /opt/env.sh\n  TAG: "),
		"PATH in the workflow's env": replace("env:\n  TAG: ", "env:\n  PATH: /opt/tools\n  TAG: "),

		// The run reads what the runner sets and a test may not.
		"the run reads TAG":             replace("shopt -s nullglob\n", "shopt -s nullglob\n          if [ -n \"${TAG:-}\" ]; then echo ok; exit 0; fi\n"),
		"the run reads GITHUB_WORKFLOW": replace("shopt -s nullglob\n", "shopt -s nullglob\n          if [ \"${GITHUB_WORKFLOW:-}\" = Release ]; then exit 0; fi\n"),
		"the run reads CI":              replace("shopt -s nullglob\n", "shopt -s nullglob\n          [ \"$CI\" = true ] && exit 0\n"),
		"the run runs printenv":         replace("shopt -s nullglob\n", "shopt -s nullglob\n          if printenv TAG >/dev/null; then exit 0; fi\n"),
		"the run reads indirectly":      replace("shopt -s nullglob\n", "shopt -s nullglob\n          x=TAG\n          if [ -n \"${!x}\" ]; then exit 0; fi\n"),
		"the run tests a variable":      replace("shopt -s nullglob\n", "shopt -s nullglob\n          if [[ -v TAG ]]; then exit 0; fi\n"),
	}
	// Keys the runner reads on the step or a job, spelled so a reading of
	// bare keys skips them.
	for _, key := range []string{
		`"continue-on-error": true`,
		`continue-on-error : true`,
		`'if': false`,
		`"shell": "true {0}"`,
		`"env": {PATH: /nonexistent}`,
		`working-directory : /`,
	} {
		changes["the step with "+key] = replace(step, "      - name: "+tagCheckName+"\n        "+key+"\n        run: |\n")
		changes["the step's run then "+key] = replace("          echo \"Checked ${#bins[@]} binaries: none carries a build tag\"\n",
			"          echo \"Checked ${#bins[@]} binaries: none carries a build tag\"\n        "+key+"\n")
	}
	for job, key := range map[string]string{
		"verify":  `"if": ${{ always() }}`,
		"attest":  `continue-on-error : true`,
		"build":   `"continue-on-error": true`,
		"release": `"defaults": {run: {shell: "true {0}"}}`,
	} {
		changes["job "+job+" with "+key] = strings.Replace(workflow, "\n  "+job+":\n", "\n  "+job+":\n    "+key+"\n", 1)
	}
	changes["the build job with a quoted defaults"] = replace("    timeout-minutes: 15\n", "    timeout-minutes: 15\n    \"defaults\": {run: {shell: \"true {0}\"}}\n")
	for _, mark := range marks {
		changes["no mark "+mark] = strings.ReplaceAll(workflow, mark, "MADE_UP_MARK")
	}
	for _, cond := range []string{
		">-\n      !failure()",
		"|\n      !failure()",
		"",
		"${{ always() }}",
		"always()",
		"${{ !failure() || failure() }}",
		"${{ success() || failure() }}",
		"${{ !cancelled() }}",
		"${{ failure() }}",
		"${{ !failure() && always() }}",
		"${{ !failure() && ALWAYS() }}",
		"${{ (!failure() || true) }}",
		"${{ !failure() && true || failure() }}",
		"${{ Failure() || !failure() }}",
		"${{ always () && needs.attest.result == 'success' }}",
		"${{ success () || cancelled() }}",
		"${{ !failure() }",
		"\"${{ failure() }}\"",
	} {
		for _, job := range []string{"verify", "release"} {
			changes[fmt.Sprintf("%s if %q", job, cond)] = strings.Replace(workflow, "\n  "+job+":\n", "\n  "+job+":\n    if: "+cond+"\n", 1)
		}
	}
	for name, changed := range changes {
		if changed == workflow {
			t.Fatalf("%s: the change didn't apply", name)
		}
		if gaps := tagCheckGaps(t, changed, marks); gaps == nil {
			t.Errorf("%s: no gap", name)
		}
	}

	for _, cond := range []string{
		"${{ !failure() && !cancelled() }}",
		"!failure() && !cancelled()",
		"${{ !failure() && (github.event_name == 'push' || inputs.tag != '') }}",
		"${{ ! failure() && needs.attest.result == 'success' }}",
		"github.ref_type == 'tag'",
		"${{ github.event_name == 'push' || inputs.tag != '' }}",
	} {
		changed := strings.Replace(workflow, "\n  release:\n", "\n  release:\n    if: "+cond+"\n", 1)
		if gaps := tagCheckGaps(t, changed, marks); gaps != nil {
			t.Errorf("release if %q: %q", cond, gaps)
		}
	}
	for _, needs := range []string{"    needs: [build, attest]\n", "    needs: [ attest ]\n", "    needs:\n      - attest\n"} {
		changed := strings.Replace(workflow, "    needs: attest\n", needs, 1)
		if gaps := tagCheckGaps(t, changed, marks); gaps != nil {
			t.Errorf("release %q: %q", needs, gaps)
		}
	}
}

// tagCheckGo is the go command the tests build and run the step with: the
// one first on PATH as go test runs them, or the test's own.
func tagCheckGo() string {
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	return filepath.Join(runtime.GOROOT(), "bin", "go")
}

// tagCheckCaches are Go's build and module caches, as go env finds them in
// the environment the tests started in, before any test moves HOME.
var tagCheckCaches = sync.OnceValues(func() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tagCheckHangGuard)
	defer cancel()
	out, err := exec.CommandContext(ctx, tagCheckGo(), "env", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		return nil, fmt.Errorf("go env: %v", err)
	}
	caches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(caches) != 2 || !filepath.IsAbs(caches[0]) || !filepath.IsAbs(caches[1]) {
		return nil, fmt.Errorf("go env gave the caches as %q", out)
	}
	return caches, nil
})

// tagCheckTag is the made-up tag the release's build is given here, as
// Build gives it the tag it publishes.
const tagCheckTag = "2026.11.99"

// tagCheckRunner is what the runner sets for a step of the build job beyond
// the test's environment, with the workflow's TAG, so that a step that read
// any of it would run here as it does there.
var tagCheckRunner = []string{
	"CI=true", "GITHUB_ACTIONS=true", "GITHUB_WORKFLOW=Release", "GITHUB_JOB=build", "GITHUB_EVENT_NAME=push",
	"GITHUB_REF=refs/tags/" + tagCheckTag, "GITHUB_REF_NAME=" + tagCheckTag, "GITHUB_REF_TYPE=tag",
	"GITHUB_REPOSITORY=heyNag/eva-md", "RUNNER_OS=Linux", "RUNNER_ARCH=X64", "TAG=" + tagCheckTag,
}

// tagCheckEnv is the environment the builds and the step run in, built
// from nothing so that no variable of the machine the test runs on reaches
// them: a home of their own with Go's telemetry off in it, the test's
// TMPDIR, no Go settings but these, Go's own caches, no network, no other
// toolchain, and go first on the test's PATH. A later value of a name wins,
// as os/exec reads it.
func tagCheckEnv(t *testing.T) []string {
	t.Helper()
	caches, err := tagCheckCaches()
	if err != nil {
		t.Fatal(err)
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
	env := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "GOENV=off",
		"GOCACHE=" + caches[0], "GOMODCACHE=" + caches[1], "GOPROXY=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly",
		"PATH=" + filepath.Dir(tagCheckGo()) + string(os.PathListSeparator) + os.Getenv("PATH")}
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		env = append(env, "TMPDIR="+tmp)
	}
	return env
}

// tagCheckModules fails the test, saying what to run, when Go's module
// cache lacks a module ./cmd/eva-md needs: the builds run with no network,
// and this package's own tests need neither of eva-md's two modules, so go
// test of it alone never fetches them.
func tagCheckModules(t *testing.T, env []string, root string) {
	t.Helper()
	full := append(slices.Clone(env), "CGO_ENABLED=0", "GOOS=darwin")
	code, out := tagCheckCommand(t, root, full, tagCheckGo(), "list", "-e", "-deps", "-f", "{{with .Error}}{{.}}{{end}}", "./cmd/eva-md")
	if out = strings.TrimSpace(out); code != 0 || out != "" {
		t.Fatalf("Go's module cache lacks a module ./cmd/eva-md needs, and these tests build it with no network; run go mod download in the repository first. go list exited %d:\n%s", code, out)
	}
}

// tagCheckCommand runs name with args in dir and env, and gives its exit
// code and its output, stdout and stderr together.
func tagCheckCommand(t *testing.T, dir string, env []string, name string, args ...string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), tagCheckHangGuard)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s %q hung", name, args)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), string(out)
	} else if err != nil {
		t.Fatal(err)
	}
	return 0, string(out)
}

// tagCheckBuild builds the package pkg of the module in dir to a new file
// with the release's flags, its tag made up, then args, for darwin on arch
// without cgo, in base with env after it.
func tagCheckBuild(t *testing.T, base []string, dir, arch, pkg string, env []string, args ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "eva-md")
	build := append([]string{"build", "-trimpath", "-mod=readonly", "-buildvcs=false", "-ldflags", "-X main.version=" + tagCheckTag, "-o", out}, args...)
	full := append(append(slices.Clone(base), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH="+arch), env...)
	if code, output := tagCheckCommand(t, dir, full, tagCheckGo(), append(build, pkg)...); code != 0 {
		t.Fatalf("go %q in %s exited %d:\n%s", build, dir, code, output)
	}
	return out
}

// builtBinary is a binary a test built, and what it must be shown to hold
// before the step reads it: the tag its build information records, or ""
// for none, and the marks its bytes hold.
type builtBinary struct {
	path string
	tag  string
	held []string
}

// shown gives the binary's path once it holds what it is meant to.
func (b builtBinary) shown(t *testing.T, marks []string) string {
	t.Helper()
	info, err := buildinfo.ReadFile(b.path)
	if err != nil {
		t.Fatal(err)
	}
	tags := ""
	for _, s := range info.Settings {
		if s.Key == "-tags" {
			tags = s.Value
		}
	}
	data, err := os.ReadFile(b.path)
	if err != nil {
		t.Fatal(err)
	}
	var held []string
	for _, mark := range marks {
		if bytes.Contains(data, []byte(mark)) {
			held = append(held, mark)
		}
	}
	if tags != b.tag || !slices.Equal(held, b.held) {
		t.Fatalf("%s records the tags %q and holds %q, not %q and %q", b.path, tags, held, b.tag, b.held)
	}
	return b.path
}

// runTagCheck runs the step's run block as the runner's shell: bash runs
// one, in env with what the runner sets after it and RUNNER_TEMP holding a
// release folder of the files given, by name, beside three that are no
// binary, and gives its exit code and output. A file given as "link:" and a path is a link to that path, and
// one given as "folder:" and a path a folder that holds a copy of it.
func runTagCheck(t *testing.T, run string, env []string, files map[string]string) (int, string) {
	t.Helper()
	temp := t.TempDir()
	release := filepath.Join(temp, "release")
	if err := os.Mkdir(release, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{"SHA256SUMS", "notes.md", "install.sh"} {
		if err := os.WriteFile(filepath.Join(release, other), []byte("made-up\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, from := range files {
		to := filepath.Join(release, name)
		if target, ok := strings.CutPrefix(from, "link:"); ok {
			if err := os.Symlink(target, to); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if inner, ok := strings.CutPrefix(from, "folder:"); ok {
			if err := os.Mkdir(to, 0o755); err != nil {
				t.Fatal(err)
			}
			from, to = inner, filepath.Join(to, "eva-md")
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(t.TempDir(), "step.sh")
	if err := os.WriteFile(script, []byte(run), 0o644); err != nil {
		t.Fatal(err)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	return tagCheckCommand(t, temp, append(append(slices.Clone(env), tagCheckRunner...), "RUNNER_TEMP="+temp), bash, "--noprofile", "--norc", "-eo", "pipefail", script)
}

// errorLines are the lines of the step's output that GitHub shows as
// errors.
func errorLines(out string) []string {
	var errs []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "::error::") {
			errs = append(errs, line)
		}
	}
	return errs
}

// The step, run as the runner runs it, passes eva-md built as a release is
// for each arch the Build step builds, and fails, naming the file, when
// either is the same build with the evaluation's tag, or is a made-up
// binary of eva-md's module that records a tag, from the flag or from
// GOFLAGS, holds one of the evaluation build's marks with no tag, is
// another package, even one whose path starts with eva-md's command's, or
// is no Go binary at all; and it fails when there is no binary. Each binary is first shown to hold what it is meant to.
func TestReleaseTagCheckFailsATaggedBinary(t *testing.T) {
	workflow := repoText(t, ".github", "workflows", "release.yml")
	run := tagCheckRun(t, workflow)
	marks := evalMarks(t)
	m := regexp.MustCompile(`(?m)^\s+for arch in (.+); do$`).FindAllStringSubmatch(workflow, -1)
	if len(m) != 1 {
		t.Fatalf("%d lines of the workflow loop over the arches, not one", len(m))
	}
	arches := strings.Fields(m[0][1])
	if len(arches) < 2 {
		t.Fatalf("the Build step builds for %q", arches)
	}
	env := tagCheckEnv(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tagCheckModules(t, env, root)
	fileOf := func(arch string) string { return "eva-md-darwin-" + arch }
	release, tagged := map[string]string{}, map[string]string{}
	for _, arch := range arches {
		release[arch] = builtBinary{tagCheckBuild(t, env, root, arch, "./cmd/eva-md", nil), "", nil}.shown(t, marks)
		tagged[arch] = builtBinary{tagCheckBuild(t, env, root, arch, "./cmd/eva-md", nil, "-tags", "evamd_eval"), "evamd_eval", marks}.shown(t, marks)
	}

	// Made-up modules with eva-md's path and a go line no newer than
	// go.mod's.
	goLine := regexp.MustCompile(`(?m)^go \S+$`).FindString(repoText(t, "go.mod"))
	if goLine == "" {
		t.Fatal("go.mod has no go line")
	}
	madeUp := func(files map[string]string) string {
		t.Helper()
		dir := t.TempDir()
		files["go.mod"] = "module github.com/heyNag/eva-md\n\n" + goLine + "\n"
		for name, text := range files {
			path := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	const plainMain = "package main\n\nimport \"fmt\"\n\nvar version = \"dev\"\n\nfunc main() { fmt.Println(\"made-up\", version) }\n"
	plain := madeUp(map[string]string{"cmd/eva-md/main.go": plainMain})
	withFile := madeUp(map[string]string{
		"cmd/eva-md/main.go":      "package main\n\nimport \"fmt\"\n\nvar version = \"dev\"\n\nfunc main() { fmt.Println(\"made-up\", version, seam()) }\n",
		"cmd/eva-md/evalbuild.go": "package main\n\nimport \"os\"\n\n//go:noinline\nfunc seam() int { return len(os.Args) }\n",
	})
	withVariable := madeUp(map[string]string{
		"cmd/eva-md/main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nvar version = \"dev\"\n\nfunc main() { fmt.Println(\"made-up\", version, os.Getenv(\"EVA_MD_EVAL_ACTOR\")) }\n",
	})
	other := madeUp(map[string]string{"cmd/other/main.go": plainMain})
	// A package whose path starts with eva-md's command's.
	longer := madeUp(map[string]string{"cmd/eva-md2/main.go": plainMain})
	last := arches[len(arches)-1]
	notGo := filepath.Join(t.TempDir(), "notgo")
	if err := os.WriteFile(notGo, []byte("#!/bin/sh\necho made-up\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := buildinfo.ReadFile(notGo); err == nil {
		t.Fatal("the file that is no Go binary has build information")
	}
	madeUpBuild := func(dir, pkg string, env2 []string, args ...string) string {
		t.Helper()
		return tagCheckBuild(t, env, dir, last, pkg, env2, args...)
	}

	// A grep that cannot read what it is given, first on PATH.
	fakeGrep := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeGrep, "grep"), []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}

	// Each case puts its binaries in place of the release's own build, by
	// arch, and runs the step with env after the builds' environment. bad
	// are the files the step must fail on, each named first in at least one
	// of its errors and no other file in any; found is what its errors
	// must say it found. No bad is a pass.
	type tagCase struct {
		name   string
		binary map[string]string
		env    []string
		bad    []string
		found  []string
	}
	all := []string{}
	for _, arch := range arches {
		all = append(all, fileOf(arch))
	}
	cases := []tagCase{
		{"the release's build", nil, nil, nil, nil},
		{"made-up, no tag and no mark", map[string]string{last: builtBinary{madeUpBuild(plain, "./cmd/eva-md", nil), "", nil}.shown(t, marks)}, nil, nil, nil},
		{"made-up, a tag from the flag", map[string]string{last: builtBinary{madeUpBuild(plain, "./cmd/eva-md", nil, "-tags", "evamd_testlimits"), "evamd_testlimits", nil}.shown(t, marks)}, nil, []string{fileOf(last)}, []string{"build -tags=evamd_testlimits"}},
		{"made-up, a tag from GOFLAGS", map[string]string{last: builtBinary{madeUpBuild(plain, "./cmd/eva-md", []string{"GOFLAGS=-mod=readonly -tags=evamd_testlimits"}), "evamd_testlimits", nil}.shown(t, marks)}, nil, []string{fileOf(last)}, []string{"build -tags=evamd_testlimits"}},
		{"made-up, evalbuild.go with no constraint", map[string]string{last: builtBinary{madeUpBuild(withFile, "./cmd/eva-md", nil), "", marks[:1]}.shown(t, marks)}, nil, []string{fileOf(last)}, marks[:1]},
		{"made-up, the variable in main.go alone", map[string]string{last: builtBinary{madeUpBuild(withVariable, "./cmd/eva-md", nil), "", marks[1:]}.shown(t, marks)}, nil, []string{fileOf(last)}, marks[1:]},
		{"made-up, another package", map[string]string{last: builtBinary{madeUpBuild(other, "./cmd/other", nil), "", nil}.shown(t, marks)}, nil, []string{fileOf(last)}, []string{"github.com/heyNag/eva-md/cmd/other,"}},
		{"made-up, a package whose path starts with eva-md's", map[string]string{last: builtBinary{madeUpBuild(longer, "./cmd/eva-md2", nil), "", nil}.shown(t, marks)}, nil, []string{fileOf(last)}, []string{"github.com/heyNag/eva-md/cmd/eva-md2,"}},
		{"no Go binary", map[string]string{last: notGo}, nil, []string{fileOf(last)}, []string{"has no build information"}},
		{"a link to the release's build", map[string]string{last: "link:" + release[last]}, nil, []string{fileOf(last)}, []string{"is not a regular file"}},
		{"a folder that holds a tagged build", map[string]string{last: "folder:" + tagged[last]}, nil, []string{fileOf(last)}, []string{"is not a regular file"}},
		{"a grep that cannot read", nil, []string{"PATH=" + fakeGrep + string(os.PathListSeparator) + path}, all, []string{"grep exited 2"}},
	}
	for _, arch := range arches {
		cases = append(cases, tagCase{"the release's build with the evaluation's tag for " + arch, map[string]string{arch: tagged[arch]}, nil, []string{fileOf(arch)}, append([]string{"build -tags=evamd_eval"}, marks...)})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{}
			for _, arch := range arches {
				files[fileOf(arch)] = release[arch]
				if b, ok := c.binary[arch]; ok {
					files[fileOf(arch)] = b
				}
			}
			code, out := runTagCheck(t, run, append(slices.Clone(env), c.env...), files)
			errs := errorLines(out)
			if c.bad == nil {
				if code != 0 || errs != nil {
					t.Fatalf("the step exited %d:\n%s", code, out)
				}
				return
			}
			if code == 0 || errs == nil {
				t.Fatalf("the step exited %d with no error:\n%s", code, out)
			}
			named := map[string]bool{}
			for _, e := range errs {
				file, _, _ := strings.Cut(strings.TrimPrefix(e, "::error::"), " ")
				if !slices.Contains(c.bad, file) {
					t.Errorf("the step's error doesn't name %q first: %s", c.bad, e)
				}
				named[file] = true
			}
			for _, file := range c.bad {
				if !named[file] {
					t.Errorf("no error of the step names %s:\n%s", file, out)
				}
			}
			for _, want := range c.found {
				if !strings.Contains(strings.Join(errs, "\n"), want) {
					t.Errorf("no error of the step says it found %q:\n%s", want, out)
				}
			}
		})
	}
	t.Run("no binary", func(t *testing.T) {
		code, out := runTagCheck(t, run, env, nil)
		if errs := errorLines(out); code == 0 || len(errs) != 1 || !strings.Contains(errs[0], "eva-md-darwin-*") {
			t.Fatalf("the step exited %d with no binary:\n%s", code, out)
		}
	})
}
