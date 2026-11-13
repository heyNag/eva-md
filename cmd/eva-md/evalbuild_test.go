package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"go/build/constraint"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// The evaluation build is eva-md built with the evamd_eval tag (see
// evalbuild.go). These tests hold that no release can carry it, and that
// what it changes is changed in it alone.

// evalBuilt holds the builds of these tests, by name, each built once for
// all tests beside the release stand-ins, which removeReleases removes.
var evalBuilt = map[string]string{}

// evalBinary is eva-md built from the repo's root as go build is given
// args, the package last, in a home of its own as the stand-in is (see
// standInEnv), with env added to that environment.
func evalBinary(t *testing.T, name string, env []string, args ...string) string {
	t.Helper()
	releaseMu.Lock()
	defer releaseMu.Unlock()
	if binary, ok := evalBuilt[name]; ok {
		return binary
	}
	if releaseRoot == "" {
		dir, err := os.MkdirTemp(buildTemp, "eva-md-release-")
		if err != nil {
			t.Fatal(err)
		}
		releaseRoot = dir
	}
	binary := filepath.Join(releaseRoot, "eval-"+name, "eva-md")
	build := append([]string{"build", "-o", binary}, args...)
	goCommand(t, filepath.Join(buildDir, "..", ".."), env, build...)
	evalBuilt[name] = binary
	return binary
}

// releaseRecipe is how the release workflow builds eva-md: the flags of its
// one go build line but -o, with the tag a release is cut for made up, the
// package it builds, the environment its Build step gives that line, and
// the arches the step's loop builds it for, each through GOARCH.
type releaseRecipe struct {
	flags  []string
	pkg    string
	env    []string
	arches []string
}

// releaseEnvNames are the names the release's Build step may set in its
// env, those that choose the target. Any other, GOFLAGS among them, is one
// these tests don't model, and fails them.
var releaseEnvNames = []string{"CGO_ENABLED", "GOOS", "GOAMD64", "GOARM64"}

// releaseLoop is the loop of the Build step over the arches, and the
// variable it sets.
var releaseLoop = regexp.MustCompile(`^\s*for ([a-z]+) in ([^;$]+); do\s*$`)

// readReleaseBuild reads how the release workflow builds eva-md from its
// Build step: its env, its loop over the arches, and its one go build line,
// continuations joined. It refuses what it can't fill in or doesn't model:
// a value with a $ but the release's tag, a word before go but GOARCH from
// the loop, or a name in the env but releaseEnvNames.
func readReleaseBuild(workflow string) (releaseRecipe, error) {
	var build releaseRecipe
	lines := strings.Split(workflow, "\n")
	indent := func(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "- name: Build" {
			if start >= 0 {
				return build, errors.New("the release workflow has two Build steps")
			}
			start = i
		}
	}
	if start < 0 {
		return build, errors.New("the release workflow has no Build step")
	}
	var step []string
	for _, line := range lines[start+1:] {
		if strings.TrimSpace(line) != "" && indent(line) <= indent(lines[start]) {
			break
		}
		step = append(step, line)
	}

	envAt := slices.IndexFunc(step, func(line string) bool { return strings.TrimSpace(line) == "env:" })
	if envAt >= 0 {
		for _, line := range step[envAt+1:] {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if indent(line) <= indent(step[envAt]) {
				break
			}
			name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			value = strings.Trim(strings.TrimSpace(value), `'"`)
			if !ok || !slices.Contains(releaseEnvNames, name) {
				return build, fmt.Errorf("the release's Build step sets %q, which these tests don't model", strings.TrimSpace(line))
			}
			if value == "" || strings.ContainsAny(value, "$ ") {
				return build, fmt.Errorf("the release's Build step sets %s to a value these tests can't fill in: %q", name, value)
			}
			build.env = append(build.env, name+"="+value)
		}
	}

	loopVar := ""
	for _, line := range step {
		if m := releaseLoop.FindStringSubmatch(line); m != nil {
			if loopVar != "" {
				return build, errors.New("the release's Build step has two loops over the arches")
			}
			loopVar, build.arches = m[1], strings.Fields(m[2])
		}
	}
	if loopVar == "" || len(build.arches) == 0 {
		return build, errors.New("the release's Build step has no loop over the arches")
	}

	goBuilds := func(text string) [][]string {
		var builds [][]string
		for _, line := range joinedLines(text) {
			words := shellWords(line)
			for i := 0; i+1 < len(words); i++ {
				if words[i] == "go" && words[i+1] == "build" {
					builds = append(builds, words)
					break
				}
			}
		}
		return builds
	}
	all, inStep := goBuilds(workflow), goBuilds(strings.Join(step, "\n"))
	if len(all) != 1 || len(inStep) != 1 {
		return build, fmt.Errorf("the release workflow has %d go build lines, %d of them in its Build step: %q", len(all), len(inStep), all)
	}
	words := inStep[0]
	at := slices.Index(words, "go")
	if want := []string{"GOARCH=$" + loopVar}; !slices.Equal(words[:at], want) {
		return build, fmt.Errorf("the release's go build is given %q before go, not %q", words[:at], want)
	}
	words = words[at+2:]
	if len(words) == 0 || words[len(words)-1] != "./cmd/eva-md" {
		return build, fmt.Errorf("the release workflow's go build doesn't end with ./cmd/eva-md: %q", words)
	}
	build.pkg = words[len(words)-1]
	for i := 0; i < len(words)-1; i++ {
		if words[i] == "-o" {
			i++
			continue
		}
		flag := strings.NewReplacer("${TAG}", "2026.11.99", "$TAG", "2026.11.99").Replace(words[i])
		if strings.Contains(flag, "$") {
			return build, fmt.Errorf("the release's go build has a flag these tests can't fill in: %q", words[i])
		}
		build.flags = append(build.flags, flag)
	}
	if !slices.Contains(build.flags, "-trimpath") || !slices.Contains(build.flags, "-ldflags") {
		return build, fmt.Errorf("read %q from the release's go build; the check read the wrong line", build.flags)
	}
	return build, nil
}

// theReleaseBuild is how the release workflow of this checkout builds
// eva-md.
func theReleaseBuild(t *testing.T) releaseRecipe {
	t.Helper()
	build, err := readReleaseBuild(readText(t, filepath.Join(buildDir, "..", "..", ".github", "workflows", "release.yml")))
	if err != nil {
		t.Fatal(err)
	}
	return build
}

// releaseTarget is eva-md built as the release workflow builds it for arch,
// in its Build step's environment, with the evaluation's tag added or not.
// -buildvcs=false keeps go from reading the checkout's git, which the
// tests' own user may not own; it sets no tag.
func releaseTarget(t *testing.T, arch string, withEvalTag bool) string {
	t.Helper()
	build := theReleaseBuild(t)
	args := append(append([]string{}, build.flags...), "-buildvcs=false")
	name := "release-" + arch
	if withEvalTag {
		args, name = append(args, "-tags", "evamd_eval"), name+"-tagged"
	}
	return evalBinary(t, name, append(slices.Clone(build.env), "GOARCH="+arch), append(args, build.pkg)...)
}

// releaseTagBuild is the release's build for this machine's own system, so
// it runs here, with the evaluation's tag added or not.
func releaseTagBuild(t *testing.T, withEvalTag bool) string {
	t.Helper()
	build := theReleaseBuild(t)
	args := append(append([]string{}, build.flags...), "-buildvcs=false")
	name := "release"
	if withEvalTag {
		args, name = append(args, "-tags", "evamd_eval"), "release-tagged"
	}
	return evalBinary(t, name, nil, append(args, build.pkg)...)
}

// joinedLines gives text's lines with each line that ends in a backslash
// joined to the next, as the shell reads them.
func joinedLines(text string) []string {
	var lines []string
	current := ""
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutSuffix(strings.TrimRight(line, " \t\r"), `\`); ok {
			current += rest + " "
			continue
		}
		lines = append(lines, current+line)
		current = ""
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// shellWords splits a line into words as the shell does for the lines these
// tests read: by blanks, outside single or double quotes, which are taken
// away.
func shellWords(line string) []string {
	var words []string
	var word strings.Builder
	inWord, quote := false, rune(0)
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

var (
	// tagsFlag finds a -tags or --tags flag, alone or with its value after
	// =, and not git's --no-tags or --no-follow-tags.
	tagsFlag = regexp.MustCompile(`(?:^|[\s'",=\[(])--?tags\b`)
	// tagsAllowed is the start of a command that may take a -tags: git, as
	// git ls-remote --tags, or go test or go vet, which build no release.
	// It reads from the last of ; & | ( ` or { before the flag, past the
	// shell's keywords, a YAML run: and assignments.
	tagsAllowed = regexp.MustCompile(`^\s*(?:-\s+)?(?:run:\s*)?(?:(?:if|then|else|elif|do|while|until|!|exec|command)\s+)*(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*(?:git|go\s+(?:test|vet))\s`)
	// goflagsSet finds where GOFLAGS is set: GOFLAGS=, GOFLAGS: in YAML,
	// or "GOFLAGS": in a script's map.
	goflagsSet = regexp.MustCompile(`\bGOFLAGS['"]?\s*([=:])`)
)

// tagGaps gives what in a file of a script or a workflow could give a
// release the evaluation build's tag: the tag's name; a -tags anywhere but
// on a git, go test or go vet command, so in whatever form a go build, go
// install or go run is written, or a value a variable holds; a GOFLAGS
// whose value holds -tags or comes from a variable or an expression; and in
// the release's own workflows any GOFLAGS at all.
func tagGaps(name, text string) []string {
	var gaps []string
	if strings.Contains(text, "evamd_eval") {
		gaps = append(gaps, name+" names evamd_eval")
	}
	if (name == ".github/workflows/release.yml" || name == ".github/workflows/cut-release.yml") && strings.Contains(text, "GOFLAGS") {
		gaps = append(gaps, name+" names GOFLAGS, which a release's build would take")
	}
	for _, line := range joinedLines(text) {
		for _, at := range tagsFlag.FindAllStringIndex(line, -1) {
			command := line[:at[0]+strings.Index(line[at[0]:at[1]], "-")]
			if cut := strings.LastIndexAny(command, ";&|(`{"); cut >= 0 {
				command = command[cut+1:]
			}
			if !tagsAllowed.MatchString(command) {
				gaps = append(gaps, name+" passes -tags to a command but git, go test or go vet: "+strings.TrimSpace(line))
				break
			}
		}
		for _, at := range goflagsSet.FindAllStringSubmatchIndex(line, -1) {
			rest := line[at[1]:]
			value := strings.TrimSpace(rest)
			if line[at[2]:at[3]] == "=" {
				value = ""
				if words := shellWords(rest); len(words) > 0 && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
					value = words[0]
				}
			}
			if strings.Contains(value, "$") || tagsFlag.MatchString(value) {
				gaps = append(gaps, name+" sets GOFLAGS to -tags or a value it can't show: "+strings.TrimSpace(line))
			}
		}
	}
	return gaps
}

// buildConstraint gives the build constraint of a Go file's source: its
// one //go:build line before the package clause. A second such line, or a
// // +build line, is an error.
func buildConstraint(source string) (constraint.Expr, error) {
	var expr constraint.Expr
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(line, "package ") {
			break
		}
		switch {
		case constraint.IsPlusBuild(line):
			return nil, fmt.Errorf("a // +build line: %q", line)
		case constraint.IsGoBuild(line):
			if expr != nil {
				return nil, fmt.Errorf("a second //go:build line: %q", line)
			}
			e, err := constraint.Parse(line)
			if err != nil {
				return nil, err
			}
			expr = e
		}
	}
	return expr, nil
}

// evalMarks are what the evaluation build's code carries into a binary:
// the path of its file, which a binary keeps for its stack traces, and the
// name of the variable its seam reads. A release binary holds neither.
// (table.go names both, but its list of files is read by tests only, so a
// build drops it; if that changed, these tests would say so.)
var evalMarks = []string{"/cmd/eva-md/evalbuild", "EVA_MD_EVAL_ACTOR"}

// No release can carry the evaluation build. evalbuild.go's only build
// constraint is the tag, which nothing but the tag can meet, and no other
// file of the command names the tag or the seam's variable. Without the tag
// go compiles evalbuild.go neither here nor for any target the release
// workflow builds, in the environment its Build step gives, and with it
// compiles it and nothing else besides. No script or workflow names the
// tag, passes -tags but to git, go test or go vet, or sets a GOFLAGS that
// could hold it. And the release's own build, for each of its targets,
// records no tags and holds none of the evaluation build's code, while the
// same build with the tag records it, which is what the evaluation reads,
// and holds that code.
func TestNoReleaseCarriesTheEvaluationBuild(t *testing.T) {
	source := readText(t, filepath.Join(buildDir, "evalbuild.go"))
	if expr, err := buildConstraint(source); err != nil {
		t.Errorf("evalbuild.go: %v", err)
	} else if tag, ok := expr.(*constraint.TagExpr); !ok || tag.Tag != "evamd_eval" {
		t.Errorf("evalbuild.go's build constraint is %v, not the tag evamd_eval alone", expr)
	}
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "evalbuild.go" {
			continue
		}
		for _, line := range strings.Split(readText(t, filepath.Join(buildDir, name)), "\n") {
			if (strings.Contains(line, "evamd_eval") || strings.Contains(line, "EVA_MD_EVAL_ACTOR")) &&
				!(name == "table.go" && strings.HasPrefix(line, "\t{\"evalbuild.go\", ")) {
				t.Errorf("%s names the evaluation build's tag or variable: %q", name, strings.TrimSpace(line))
			}
		}
	}

	build := theReleaseBuild(t)
	list := func(env []string, args ...string) []string {
		t.Helper()
		out := goCommand(t, buildDir, env, append(append([]string{"list"}, args...), "-f", "{{range .GoFiles}}{{.}}\n{{end}}", ".")...)
		files := strings.Fields(out)
		if len(files) < 10 || !slices.Contains(files, "main.go") {
			t.Fatalf("go list gave %q", out)
		}
		return files
	}
	targets := map[string][]string{"this machine": nil}
	for _, arch := range build.arches {
		targets["the release for "+arch] = append(slices.Clone(build.env), "GOARCH="+arch)
	}
	for target, env := range targets {
		plain, tagged := list(env), list(env, "-tags", "evamd_eval")
		if slices.Contains(plain, "evalbuild.go") {
			t.Errorf("for %s a build without the tag compiles evalbuild.go: %q", target, plain)
		}
		if want := append(slices.Clone(plain), "evalbuild.go"); !slices.Equal(sortedCopy(tagged), sortedCopy(want)) {
			t.Errorf("for %s go compiles %q with the tag, not %q", target, tagged, want)
		}
	}

	root := filepath.Join(buildDir, "..", "..")
	read := 0
	for _, dir := range []string{".github", "scripts"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, e fs.DirEntry, err error) error {
			if err != nil || !e.Type().IsRegular() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			for _, gap := range tagGaps(filepath.ToSlash(rel), string(data)) {
				t.Error(gap)
			}
			read++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if read < 10 {
		t.Fatalf("read %d files of the scripts and workflows", read)
	}

	inspect := func(binary string) (tags string, marks []string) {
		t.Helper()
		info, err := buildinfo.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		for _, setting := range info.Settings {
			if setting.Key == "-tags" {
				tags = setting.Value
			}
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		for _, mark := range evalMarks {
			if bytes.Contains(data, []byte(mark)) {
				marks = append(marks, mark)
			}
		}
		return tags, marks
	}
	for _, arch := range build.arches {
		if tags, marks := inspect(releaseTarget(t, arch, false)); tags != "" || marks != nil {
			t.Errorf("the release's build for %s records the tags %q and holds %q", arch, tags, marks)
		}
	}
	tags, marks := inspect(releaseTarget(t, build.arches[0], true))
	if !slices.Contains(strings.Split(tags, ","), "evamd_eval") || !slices.Equal(marks, evalMarks) {
		t.Errorf("the release's build with the evaluation's tag records the tags %q and holds %q", tags, marks)
	}
}

func sortedCopy(s []string) []string {
	c := slices.Clone(s)
	slices.Sort(c)
	return c
}

// The check of the scripts and workflows fails on each way a release could
// be given the tag, here in made-up lines, and passes the lines the
// workflows and scripts hold today, git's tag options among them.
func TestTheTagCheckCatchesAGap(t *testing.T) {
	for _, text := range []string{
		"run: go build -mod=readonly -o bin/eva-md ./cmd/eva-md\n",
		"GOARCH=$arch go build -trimpath -mod=readonly -ldflags \"-X main.version=$TAG\" \\\n  -o \"$out/eva-md\" ./cmd/eva-md\n",
		"git fetch --quiet --no-tags --force origin \"+refs/tags/$TAG:refs/tags/$TAG\"\n",
		"git push --no-follow-tags origin \"refs/tags/$tag\"\n",
		"remote=$(git ls-remote --tags origin 2>/dev/null) ||\n",
		"  if git ls-remote --exit-code --tags origin \"refs/tags/$TAG\" >/dev/null; then\n",
		"fail \"no tags\" \"check your access to origin (git ls-remote --tags origin), then run it again\"\n",
		"GOMODCACHE=\"$modcache\" GOPROXY=off GOFLAGS='-mod=readonly -count=1' \\\n  go test ./...\n",
		"# GOFLAGS that it doesn't have\n",
		"go test -tags evamd_testlimits ./...\n",
		"run: go vet -tags other ./...\n",
	} {
		if gaps := tagGaps("ok.yml", text); gaps != nil {
			t.Errorf("%q gives %q", text, gaps)
		}
	}
	for _, text := range []string{
		"run: go build -tags evamd_eval -o bin/eva-md ./cmd/eva-md\n",
		"run: go build -o bin/eva-md -tags=other ./cmd/eva-md\n",
		"go build -trimpath \\\n  --tags other \\\n  -o out ./cmd/eva-md\n",
		"go install -tags=other ./cmd/eva-md\n",
		"go run -tags other ./cmd/eva-md version\n",
		"subprocess.run([\"go\", \"build\", \"-tags\", \"other\", \"./cmd/eva-md\"])\n",
		"subprocess.run([\n  \"go\", \"build\",\n  \"-tags\", \"other\", \"./cmd/eva-md\"])\n",
		"go -C . build -tags=other ./cmd/eva-md\n",
		"run: >-\n  go build\n  -tags other ./cmd/eva-md\n",
		"FLAGS=\"-tags=other_$KIND\"\ngo build $FLAGS ./cmd/eva-md\n",
		"GO=go; $GO build -tags other ./cmd/eva-md\n",
		"git status && go build -tags other ./cmd/eva-md\n",
		"env:\n  GOFLAGS: -tags=other\n",
		"export GOFLAGS='-mod=readonly -tags other'\n",
		"env:\n  GOFLAGS: ${{ vars.EXTRA_GOFLAGS }}\n",
		"GOFLAGS=\"$EXTRA\" GOARCH=$arch go build -trimpath ./cmd/eva-md\n",
		"echo \"GOFLAGS=$FLAGS\" >> \"$GITHUB_ENV\"\n",
		"env = {\"GOFLAGS\": os.environ[\"EXTRA\"] + \"$X\"}\n",
		"# the evamd_eval build\n",
	} {
		if gaps := tagGaps("bad.yml", text); len(gaps) == 0 {
			t.Errorf("%q gives no gap", text)
		}
	}
	for _, name := range []string{".github/workflows/release.yml", ".github/workflows/cut-release.yml"} {
		if gaps := tagGaps(name, "env:\n  GOFLAGS: -mod=readonly\n"); len(gaps) == 0 {
			t.Errorf("a GOFLAGS in %s gives no gap", name)
		}
	}
	got := shellWords(joinedLines("GOARCH=$arch go build -trimpath -ldflags \"-X main.version=$TAG\" \\\n  -o \"$out/x\" ./cmd/eva-md")[0])
	want := []string{"GOARCH=$arch", "go", "build", "-trimpath", "-ldflags", "-X main.version=$TAG", "-o", "$out/x", "./cmd/eva-md"}
	if !slices.Equal(got, want) {
		t.Errorf("read %q, not %q", got, want)
	}
}

// The build constraint evalbuild.go is held to is the tag alone: any other
// form, which a release's target or a missing C compiler could meet, fails.
func TestTheConstraintCheckTakesTheTagAlone(t *testing.T) {
	for source, ok := range map[string]bool{
		"//go:build evamd_eval\n\npackage main\n":                       true,
		"//go:build evamd_eval || !cgo\n\npackage main\n":               false,
		"//go:build evamd_eval || (darwin && amd64)\n\npackage main\n":  false,
		"//go:build !evamd_eval\n\npackage main\n":                      false,
		"//go:build evamd_eval\n// +build evamd_eval\n\npackage main\n": false,
		"//go:build evamd_eval\n//go:build darwin\n\npackage main\n":    false,
		"package main\n": false,
		"// a comment\n\n//go:build evamd_testlimits\n\npackage main\n": false,
	} {
		expr, err := buildConstraint(source)
		tag, isTag := expr.(*constraint.TagExpr)
		if got := err == nil && isTag && tag.Tag == "evamd_eval"; got != ok {
			t.Errorf("%q: took it %v, err %v", source, got, err)
		}
	}
}

// The reading of the release workflow's build takes today's form and
// refuses each change it doesn't model: a word before go but GOARCH from
// the loop, a GOFLAGS or another name in the step's env, a value from a
// variable, a flag it can't fill in, or a second go build.
func TestTheReleaseBuildIsReadWhole(t *testing.T) {
	const step = `  build:
    steps:
      - name: Build
        env:
          CGO_ENABLED: '0'
          GOOS: darwin
        run: |
          out=$RUNNER_TEMP/release
          for arch in arm64 amd64; do
            GOARCH=$arch go build -trimpath -mod=readonly -ldflags "-X main.version=$TAG" \
              -o "$out/eva-md-darwin-$arch" ./cmd/eva-md
          done
      - name: Upload
        run: echo done
`
	build, err := readReleaseBuild(step)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"CGO_ENABLED=0", "GOOS=darwin"}; !slices.Equal(build.env, want) {
		t.Errorf("env %q, not %q", build.env, want)
	}
	if want := []string{"arm64", "amd64"}; !slices.Equal(build.arches, want) {
		t.Errorf("arches %q, not %q", build.arches, want)
	}
	if want := []string{"-trimpath", "-mod=readonly", "-ldflags", "-X main.version=2026.11.99"}; !slices.Equal(build.flags, want) || build.pkg != "./cmd/eva-md" {
		t.Errorf("flags %q and package %q", build.flags, build.pkg)
	}
	for name, change := range map[string][2]string{
		"GOFLAGS before go":      {"GOARCH=$arch go build", `GOFLAGS="$EXTRA" GOARCH=$arch go build`},
		"GOOS before go":         {"GOARCH=$arch go build", `GOOS=linux GOARCH=$arch go build`},
		"GOFLAGS in the env":     {"GOOS: darwin\n", "GOOS: darwin\n          GOFLAGS: -mod=readonly\n"},
		"a value from an input":  {"GOOS: darwin", "GOOS: ${{ inputs.goos }}"},
		"arches from a variable": {"for arch in arm64 amd64; do", "for arch in $ARCHES; do"},
		"a flag from a variable": {"-mod=readonly", "$EXTRA"},
		"a second go build":      {"      - name: Upload\n        run: echo done", "      - name: Upload\n        run: go build -o x ./cmd/eva-md"},
		"no Build step":          {"- name: Build", "- name: Compile"},
		"another package":        {"\" ./cmd/eva-md", "\" ./cmd/eva-eval"},
		"no loop":                {"for arch in arm64 amd64; do", "arch=arm64; {"},
	} {
		changed := strings.Replace(step, change[0], change[1], 1)
		if changed == step {
			t.Fatalf("%s: the change didn't apply", name)
		}
		if _, err := readReleaseBuild(changed); err == nil {
			t.Errorf("%s: read without an error", name)
		}
	}
}

// In the evaluation build a hook with EVA_MD_EVAL_ACTOR=person takes a
// Claude Code chat that a program started, as print mode starts one, for a
// person's: its prompt is stored as the user's. The evaluation build with
// another value or none, and the release stand-in with the variable, store
// only the event host_prompt, as for any program's chat. Both builds raise
// the time limits, so a slow machine doesn't decide the outcome.
func TestTheEvaluationBuildTakesAScriptedChatForAPersons(t *testing.T) {
	eval := evalBinary(t, "standin", nil, "-mod=readonly", "-buildvcs=false", "-tags", "evamd_testlimits,evamd_eval", "./cmd/eva-md")
	standIn := releaseBinary(t, "2026.11.3")
	for _, tc := range []struct {
		name, binary, actor string
		person              bool
	}{
		{"evaluation build, person", eval, "person", true},
		{"evaluation build, no actor", eval, "", false},
		{"evaluation build, another actor", eval, "program", false},
		{"evaluation build, a person's word in another case", eval, "Person", false},
		{"release stand-in, person", standIn, "person", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := isolated(t)
			cwd, err := filepath.EvalSymlinks(project(t))
			if err != nil {
				t.Fatal(err)
			}
			const prompt = "zqeval what does the release checklist say?"
			payload, _ := json.Marshal(map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "zqeval-session-1", "cwd": cwd, "prompt": prompt})
			env := []string{"CLAUDE_CODE_ENTRYPOINT=sdk-cli"}
			if tc.actor != "" {
				env = append(env, "EVA_MD_EVAL_ACTOR="+tc.actor)
			}
			if code, out, errOut := runBuilt(t, tc.binary, cwd, payload, env, "hook", "--host", "claude-code"); code != 0 {
				t.Fatalf("the hook exited %d: %q %q", code, out, errOut)
			}
			stored := inboxText(t, d.Inbox())
			asUser := strings.Contains(stored, `"role":"user","kind":"prompt"`) && strings.Contains(stored, prompt)
			asHost := strings.Contains(stored, "host_prompt")
			if tc.person && (!asUser || asHost) || !tc.person && (!asHost || strings.Contains(stored, prompt) || strings.Contains(stored, `"kind":"prompt"`)) {
				t.Fatalf("person=%v stored:\n%s", tc.person, stored)
			}
		})
	}
}

// eva-md built with the evaluation's tag alone, not the stand-in's, gets
// past setup's macOS check on Linux too; the release's build stops there
// off macOS. The test reads only the line after "Checking this Mac".
func TestTheEvaluationBuildLiftsTheMacOSCheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tagged bool
		want   string
	}{
		{"evaluation build", true, "  ✓ macOS"},
		{"release build", false, "  ✗ This isn't macOS: eva-md runs on macOS only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.tagged && runtime.GOOS == "darwin" {
				tc.want = "  ✓ macOS"
			}
			binary := releaseTagBuild(t, tc.tagged)
			isolated(t)
			home := os.Getenv("HOME")
			_, out, _ := runBuilt(t, binary, home, nil, []string{"NO_COLOR=1"}, "setup")
			_, after, found := strings.Cut(out, "Checking this Mac\n")
			line, _, _ := strings.Cut(after, "\n")
			if !found || line != tc.want {
				t.Fatalf("setup's first check reads %q, not %q:\n%s", line, tc.want, out)
			}
		})
	}
}

// runBuilt runs a built eva-md in dir with the test's environment, as
// isolated left it, and env after it, EVA_MD_EVAL_ACTOR and
// CLAUDE_CODE_ENTRYPOINT taken from the first, and stdin as its input.
func runBuilt(t *testing.T, binary, dir string, stdin []byte, env []string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), scriptHangGuard)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "EVA_MD_EVAL_ACTOR=") || strings.HasPrefix(kv, "CLAUDE_CODE_ENTRYPOINT=")
	})
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %v hung", binary, args)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out.String(), errOut.String()
	} else if err != nil {
		t.Fatal(err)
	}
	return 0, out.String(), errOut.String()
}

// inboxText is every file of the inbox, one after another, each headed by
// its path within the inbox.
func inboxText(t *testing.T, inbox string) string {
	t.Helper()
	var text strings.Builder
	err := filepath.WalkDir(inbox, func(path string, e fs.DirEntry, err error) error {
		if err != nil || !e.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(inbox, path)
		fmt.Fprintf(&text, "== %s\n%s", rel, data)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the inbox: %v", err)
	}
	return text.String()
}
