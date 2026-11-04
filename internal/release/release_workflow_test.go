// Package release tests the release flow: scripts/release.sh, run against a
// temporary origin with a fake gh, and the release workflow.
package release

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every action each workflow uses is pinned to a full commit, with its
// version in a comment, so a moved tag can't change what a release runs. A
// workflow of this repository, called by its path, runs from the same commit.
func TestReleaseWorkflowPinsEveryAction(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.y*ml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatalf("found the workflows %v; the check read the wrong folder", files)
	}
	uses := regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*(.*)$`)
	pinned := regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40} # v[0-9]+(\.[0-9]+)*$`)
	local := regexp.MustCompile(`^\./\.github/workflows/[a-z-]+\.yml$`)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for i, line := range strings.Split(string(data), "\n") {
			m := uses.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			n++
			if u := strings.TrimSpace(m[1]); !pinned.MatchString(u) && !local.MatchString(u) {
				t.Errorf("%s line %d isn't pinned to a commit with its version: %s", filepath.Base(file), i+1, strings.TrimSpace(line))
			}
		}
		if n == 0 {
			t.Errorf("%s uses no actions; the check read the wrong file", filepath.Base(file))
		}
	}
}

// The release workflow sums, attests and publishes the same files: the
// binaries it builds and the files it copies from scripts/, which
// scripts/release.sh checks main has. Once they are published, release.sh
// checks that SHA256SUMS lists exactly those files and verifies each one's
// provenance, so a file left out of one list would fail a pushed tag.
func TestReleaseWorkflowPublishesEveryAsset(t *testing.T) {
	read := func(path ...string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, path...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	workflow, script := read(".github", "workflows", "release.yml"), read("scripts", "release.sh")
	cut := read(".github", "workflows", "cut-release.yml")
	// fields is what re's group matches in text, split on spaces. Only one
	// line may match, so a second list can't go unchecked.
	fields := func(text, re string) []string {
		t.Helper()
		m := regexp.MustCompile(re).FindAllStringSubmatch(text, -1)
		if len(m) != 1 {
			t.Fatalf("%d lines match %s, not one", len(m), re)
		}
		return strings.Fields(m[0][1])
	}
	var want []string
	for _, arch := range fields(workflow, `(?m)^\s+for arch in (.+); do$`) {
		want = append(want, "eva-md-darwin-"+arch)
	}
	onMain := fields(script, `(?m)^for f in (.+); do$`)
	for _, f := range fields(workflow, `(?m)^\s+cp (.+) "\$out/"$`) {
		name, ok := strings.CutPrefix(f, "scripts/")
		if !ok {
			t.Errorf("the workflow copies %s, from outside scripts/", f)
		}
		if !slices.Contains(onMain, f) {
			t.Errorf("release.sh doesn't check that main has %s", f)
		}
		if _, err := os.Stat(filepath.Join("..", "..", f)); err != nil {
			t.Error(err)
		}
		want = append(want, name)
	}
	slices.Sort(want)
	var attested []string
	for _, f := range fields(workflow, `(?m)^\s+subject-path: \|\n((?:\s+release/\S+\n)+)`) {
		attested = append(attested, strings.TrimPrefix(f, "release/"))
	}
	for what, got := range map[string][]string{
		"release.sh checks":    fields(script, `(?m)^assets="(.+)"$`),
		"Cut a release checks": fields(cut, `(?m)^\s+assets="(.+)"$`),
		"SHA256SUMS lists":     fields(workflow, `(?m)^\s+shasum -a 256 (.+) > SHA256SUMS$`),
		"the workflow attests": attested,
		"the release has":      fields(workflow, `--notes-file notes\.md \\\n\s+(.+) SHA256SUMS\n`),
	} {
		if got = slices.Sorted(slices.Values(got)); !slices.Equal(got, want) {
			t.Errorf("%s %v, not %v", what, got, want)
		}
	}
	if n := fields(workflow, `wc -l < SHA256SUMS \| tr -d ' '\)" = (\d+)`)[0]; n != strconv.Itoa(len(want)) {
		t.Errorf("the workflow checks that SHA256SUMS has %s lines, not %d", n, len(want))
	}
}

// lines is a pattern for the lines given, one after another, each with any
// indent; "..." stands for any lines, and a line ending in @, an action's
// name, for any line that starts with it.
func lines(ls ...string) *regexp.Regexp {
	var b strings.Builder
	for _, l := range ls {
		switch {
		case l == "...":
			b.WriteString(`(?:[^\n]*\n)*?`)
		case strings.HasSuffix(l, "@"):
			b.WriteString(`[ \t]*` + regexp.QuoteMeta(l) + `[^\n]*\n`)
		default:
			b.WriteString(`[ \t]*` + regexp.QuoteMeta(l) + `\n`)
		}
	}
	return regexp.MustCompile(b.String())
}

// workflowJob is one job of a workflow, read line by line: its keys at the
// job's own level, and the permissions it asks for.
type workflowJob struct {
	text        string            // the job's lines
	keys        map[string]string // the job's own keys and their values on the key's line
	permissions map[string]string // what it asks for; empty for {}
}

// workflowJobs reads the jobs of a workflow written as this repository's are:
// two spaces of indent per level, a job's keys at four.
func workflowJobs(t *testing.T, text string) map[string]*workflowJob {
	t.Helper()
	jobs := map[string]*workflowJob{}
	start := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):$`)
	key := regexp.MustCompile(`^    ([a-z-]+):(?: (.*))?$`)
	permission := regexp.MustCompile(`^      ([a-z-]+): (read|write|none)$`)
	var job *workflowJob
	inJobs, inPermissions := false, false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == "jobs:":
			inJobs = true
			continue
		case !inJobs:
			continue
		case line != "" && !strings.HasPrefix(line, " "):
			inJobs, job = false, nil
			continue
		}
		if m := start.FindStringSubmatch(line); m != nil {
			job = &workflowJob{keys: map[string]string{}, permissions: map[string]string{}}
			jobs[m[1]] = job
			continue
		}
		if job == nil {
			continue
		}
		job.text += line + "\n"
		if m := key.FindStringSubmatch(line); m != nil {
			job.keys[m[1]] = m[2]
			inPermissions = m[1] == "permissions" && m[2] == ""
			continue
		}
		if inPermissions {
			if m := permission.FindStringSubmatch(line); m != nil {
				job.permissions[m[1]] = m[2]
			} else if strings.HasPrefix(line, "      ") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
				t.Fatalf("can't read the permission %q", line)
			}
		}
	}
	if len(jobs) == 0 {
		t.Fatal("found no jobs; the check read the wrong file")
	}
	for name, j := range jobs {
		if p, ok := j.keys["permissions"]; !ok || p != "" && p != "{}" {
			t.Errorf("job %s doesn't say what it may do: permissions %q", name, p)
		}
	}
	return jobs
}

// Cut a release releases nothing without an approval and asks for no more
// than it needs: the workflow may do nothing by default; the approve job,
// in the release environment, holds everything after the plan, and a dry
// run stops at the plan; only the tag job and the release workflow it calls
// may write to the repository; one release runs at a time; and the tag job
// pushes only the planned tag, never with force. When origin has the tag
// already, the tag job goes on without pushing only for the tag a run of the
// same plan pushed: annotated, on the planned commit, with the planned notes.
func TestReleaseCutWorkflowNeedsAnApproval(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	cut, release := read("cut-release.yml"), read("release.yml")
	for _, re := range []string{
		`(?m)^on:\n  workflow_dispatch:\n    inputs:\n      dry_run:\n        description: .+\n        type: boolean\n        default: false\n\n`,
		`(?m)^permissions: \{\}$`,
		`(?m)^concurrency:\n  group: cut-release\n  cancel-in-progress: false$`,
		`(?m)^defaults:\n  run:\n    shell: bash$`,
		// Releases are cut from main only.
		`if \[ "\$GITHUB_REF" != refs/heads/main \]; then`,
		// The release environment must make a reviewer approve.
		`gh api "repos/\$GITHUB_REPOSITORY/environments/release"`,
		`select\(\.type == "required_reviewers"\)`,
		`run: scripts/release\.sh --plan\n`,
	} {
		if !regexp.MustCompile(re).MatchString(cut) {
			t.Errorf("cut-release.yml has nothing matching %s", re)
		}
	}
	jobs := workflowJobs(t, cut)
	var names []string
	for name := range jobs {
		names = append(names, name)
	}
	if slices.Sort(names); !slices.Equal(names, []string{"approve", "check", "plan", "publish", "tag"}) {
		t.Fatalf("jobs %v", names)
	}
	for name, want := range map[string]string{"plan": "", "approve": "plan", "tag": "[plan, approve]", "publish": "[plan, tag]", "check": "[plan, publish]"} {
		if got := jobs[name].keys["needs"]; got != want {
			t.Errorf("job %s needs %q, not %q", name, got, want)
		}
	}
	// Only approve decides whether the release goes on; every later job
	// needs it, so a dry run or an unapproved run stops at the plan.
	for name, j := range jobs {
		want := ""
		if name == "approve" {
			want = "${{ !inputs.dry_run }}"
		}
		if got := j.keys["if"]; got != want {
			t.Errorf("job %s runs if %q, not %q", name, got, want)
		}
		if env := j.keys["environment"]; (env != "") != (name == "approve") || env != "" && env != "release" {
			t.Errorf("job %s has environment %q", name, env)
		}
		if strings.Contains(j.text, "inputs.dry_run") && name != "approve" && name != "plan" {
			t.Errorf("job %s reads dry_run", name)
		}
		_, called := j.keys["uses"]
		if _, ok := j.keys["timeout-minutes"]; ok == called {
			t.Errorf("job %s: timeout-minutes %v, calls a workflow %v", name, ok, called)
		}
	}
	for name, want := range map[string]map[string]string{
		"plan":    {"actions": "read", "contents": "read"},
		"approve": {},
		"tag":     {"contents": "write"},
		"publish": {"contents": "write", "id-token": "write", "attestations": "write"},
		"check":   {"contents": "read", "attestations": "read"},
	} {
		if got := jobs[name].permissions; !maps.Equal(got, want) {
			t.Errorf("job %s may %v, not %v", name, got, want)
		}
	}
	if got := jobs["publish"].keys["uses"]; got != "./.github/workflows/release.yml" {
		t.Errorf("publish calls %q", got)
	}
	// The release workflow's jobs get no more than publish grants, and need
	// all of it.
	granted := map[string]string{}
	for _, j := range workflowJobs(t, release) {
		for k, v := range j.permissions {
			if granted[k] != "write" {
				granted[k] = v
			}
		}
	}
	if !maps.Equal(granted, jobs["publish"].permissions) {
		t.Errorf("the release workflow's jobs need %v; publish grants %v", granted, jobs["publish"].permissions)
	}
	// The tag job tags the planned commit and pushes that tag alone.
	tag := jobs["tag"].text
	for _, want := range []string{
		"ref: ${{ needs.plan.outputs.commit }}\n",
		`git tag -a --no-sign --cleanup=verbatim -F "$RUNNER_TEMP/notes" "$TAG" "$COMMIT"` + "\n",
		`git push --no-follow-tags origin "refs/tags/$TAG"; then` + "\n",
		"GIT_COMMITTER_NAME: ${{ github.actor }}\n",
		"GIT_COMMITTER_EMAIL: ${{ github.actor_id }}+${{ github.actor }}@users.noreply.github.com\n",
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("the tag job has no %q", want)
		}
	}
	for what, re := range map[string]*regexp.Regexp{
		"makes the tag only when origin hasn't it, or goes on for the planned one": lines(
			"- name: Make the annotated tag",
			"id: make",
			"...",
			`if git ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null; then`,
			"...",
			`git fetch --quiet --no-tags origin "+refs/tags/$TAG:refs/tags/$TAG"`,
			`if [ "$(git cat-file -t "refs/tags/$TAG")" = tag ] &&`,
			`[ "$(git rev-parse "refs/tags/$TAG^{commit}")" = "$COMMIT" ] &&`,
			`[ "$(git tag -l --format='%(contents)' "$TAG")" = "$(printf '%s\n' "$NOTES")" ]; then`,
			"...",
			`echo "push=false" >> "$GITHUB_OUTPUT"`,
			"exit 0",
			"fi",
			`echo "::error::origin has $TAG already, on another commit or with other notes than planned; a tag is never moved, so run this workflow again, and its plan names the next tag or says $COMMIT is released"`,
			"exit 1",
			"fi",
			`printf '%s\n' "$NOTES" > "$RUNNER_TEMP/notes"`,
			`git tag -a --no-sign --cleanup=verbatim -F "$RUNNER_TEMP/notes" "$TAG" "$COMMIT"`,
			`git cat-file -p "refs/tags/$TAG"`,
			`echo "push=true" >> "$GITHUB_OUTPUT"`,
			"- name: Push only the tag",
			"if: steps.make.outputs.push == 'true'",
			"run: |",
		),
		"checks origin's tag is the local one, pushed or not": lines(
			"- name: Check origin's tag is this one",
			"run: |",
			`if [ "$(git ls-remote origin "refs/tags/$TAG" | cut -f1)" != "$(git rev-parse "refs/tags/$TAG")" ]; then`,
		),
	} {
		if !re.MatchString(tag) {
			t.Errorf("the tag job no longer %s", what)
		}
	}
	if n := strings.Count(tag, "push=false"); n != 1 {
		t.Errorf("the tag job skips its push in %d places, not one", n)
	}
	for name, j := range jobs {
		pushes := regexp.MustCompile(`git push[^\n]*`).FindAllString(j.text, -1)
		if name == "tag" && len(pushes) != 1 || name != "tag" && len(pushes) != 0 {
			t.Errorf("job %s pushes %q", name, pushes)
		}
		for _, p := range pushes {
			if strings.Contains(p, "--force") || strings.Contains(p, " -f") || strings.Contains(p, "+refs") {
				t.Errorf("job %s forces a push: %s", name, p)
			}
		}
	}
	// The release workflow runs for a pushed tag, as before, and when called
	// with one.
	for _, re := range []string{
		`(?m)^on:\n  push:\n    tags: \['\[0-9\]\*'\]\n  workflow_call:\n    inputs:\n      tag:\n        description: .+\n        type: string\n        required: true\n\n`,
		`(?m)^  TAG: \$\{\{ inputs\.tag \|\| github\.ref_name \}\}$`,
		`(?m)^  group: release-\$\{\{ inputs\.tag \|\| github\.ref_name \}\}$`,
		`(?m)^          ref: refs/tags/\$\{\{ env\.TAG \}\}$`,
	} {
		if !regexp.MustCompile(re).MatchString(release) {
			t.Errorf("release.yml has nothing matching %s", re)
		}
	}
	if strings.Contains(release, "github.ref }}") {
		t.Error("release.yml reads github.ref, which a called run doesn't set to the tag")
	}
	// A release cut from GitHub links its run, which records the approval,
	// in the release's notes, after the tag's own; a pushed tag's notes are
	// the annotation alone. The release is created from that notes file.
	for what, re := range map[string]*regexp.Regexp{
		"links a called run in the notes": lines(
			"- name: Write the notes from the tag's annotation",
			"env:",
			"CALLED_TAG: ${{ inputs.tag }}",
			"run: |",
			"...",
			`printf '%s\n' "$notes" > "$RUNNER_TEMP/release/notes.md"`,
			"...",
			`if [ -n "$CALLED_TAG" ]; then`,
			`printf '\nCut on GitHub in run %s, which records who approved it.\n' \`,
			`"$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID" >> "$RUNNER_TEMP/release/notes.md"`,
			"fi",
			"- uses: actions/setup-go@",
		),
		"uploads the notes with the build": lines(
			"- uses: actions/upload-artifact@",
			"...",
			"path: ${{ runner.temp }}/release",
		),
		"creates the release from the notes": lines(
			`--title "$TAG" --notes-file notes.md \`,
		),
	} {
		if !re.MatchString(release) {
			t.Errorf("release.yml no longer %s", what)
		}
	}
	if n := strings.Count(release, "Cut on GitHub in run"); n != 1 {
		t.Errorf("release.yml writes the run's line %d times, not once", n)
	}
}

// The provenance is checked against the workflow that signs it, release.yml,
// whichever way the release was cut: called by Cut a release, release.yml is
// still the workflow that runs the attest action, and so the signer.
//
// gh matches the signer as a prefix, so each also names the run's source
// ref: a pushed tag's run has the tag as its ref, and a called workflow's run
// its caller's, which Cut a release runs only on main.
func TestReleaseChecksProvenanceAgainstOneSigner(t *testing.T) {
	signer := regexp.MustCompile(`--signer-workflow "?([^"\s]+)"?`)
	sourceRef := regexp.MustCompile(`--source-ref "?([^"\s]+)"?`)
	for _, f := range []struct {
		path []string
		repo string // how the file names the repository
		ref  string // the source ref it checks
	}{
		{[]string{"scripts", "release.sh"}, "$repo", "refs/tags/$tag"},
		{[]string{".github", "workflows", "cut-release.yml"}, "$GITHUB_REPOSITORY", "refs/heads/main"},
	} {
		data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, f.path...)...))
		if err != nil {
			t.Fatal(err)
		}
		m := signer.FindAllStringSubmatch(string(data), -1)
		if len(m) != 1 {
			t.Fatalf("%s names the signer %d times, not once", filepath.Join(f.path...), len(m))
		}
		if got := strings.Replace(m[0][1], f.repo, "heyNag/eva-md", 1); got != "heyNag/eva-md/.github/workflows/release.yml" {
			t.Errorf("%s checks the provenance was signed by %s", filepath.Join(f.path...), got)
		}
		if m := sourceRef.FindAllStringSubmatch(string(data), -1); len(m) != 1 || m[0][1] != f.ref {
			t.Errorf("%s checks the source refs %q, not %s once", filepath.Join(f.path...), m, f.ref)
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "uses: actions/attest-build-provenance@"); n != 1 {
		t.Errorf("release.yml attests %d times, not once", n)
	}
}

// The release tag's pattern is written out in scripts/release.sh, in the
// release workflow and in Cut a release's tag job. All three are the same.
// The pattern checks only the format, while ValidTag, which eva-md upgrade
// reads releases with, also refuses a month or day out of range, which a tag
// named by a commit's date never has; they agree on every other tag here.
func TestReleaseTagPatternIsTheSameEverywhere(t *testing.T) {
	var pattern string
	for _, f := range []struct {
		path []string
		re   string
	}{
		{[]string{"scripts", "release.sh"}, `(?m)^tag_re='([^']*)'$`},
		{[]string{".github", "workflows", "release.yml"}, `(?m)^\s+re='([^']*)'$`},
		{[]string{".github", "workflows", "cut-release.yml"}, `(?m)^\s+re='([^']*)'$`},
	} {
		data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, f.path...)...))
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(f.re).FindAllStringSubmatch(string(data), -1)
		if len(m) != 1 {
			t.Fatalf("%s gives the tag pattern %d times, not once", filepath.Join(f.path...), len(m))
		}
		if pattern == "" {
			pattern = m[0][1]
		} else if m[0][1] != pattern {
			t.Errorf("%s's tag pattern is %s, not %s", filepath.Join(f.path...), m[0][1], pattern)
		}
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		tag            string
		matches, valid bool // the pattern matches it; ValidTag accepts it
	}{
		{"2026.11.3", true, true},
		{"2026.11.3.2", true, true},
		{"2026.11.3.10", true, true},
		{"2026.1.31", true, true},
		{"2026.11.3.1", false, false},
		{"2026.01.3", false, false},
		{"2026.11.03", false, false},
		{"v2026.11.3", false, false},
		{"2026.11", false, false},
		{"2026.11.3.0", false, false},
		// The format of a tag, with no thirteenth month.
		{"2026.13.1", true, false},
	} {
		if got := re.MatchString(c.tag); got != c.matches {
			t.Errorf("the pattern matches %s: %v, want %v", c.tag, got, c.matches)
		}
		if got := ValidTag(c.tag); got != c.valid {
			t.Errorf("ValidTag(%s) = %v, want %v", c.tag, got, c.valid)
		}
	}
}
