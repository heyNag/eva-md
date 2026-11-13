package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
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
// standInEnv).
func evalBinary(t *testing.T, name string, args ...string) string {
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
	goCommand(t, filepath.Join(buildDir, "..", ".."), nil, build...)
	evalBuilt[name] = binary
	return binary
}

// releaseTagBuild is the release's build, as the release workflow runs it,
// with the build tag of the evaluation added or not. -buildvcs=false keeps
// go from reading the checkout's git, which the tests' own user may not
// own; it sets no tag.
func releaseTagBuild(t *testing.T, withEvalTag bool) string {
	t.Helper()
	flags, pkg := releaseBuildFlags(t)
	args := append(append([]string{}, flags...), "-buildvcs=false")
	name := "release"
	if withEvalTag {
		args, name = append(args, "-tags", "evamd_eval"), "release-tagged"
	}
	return evalBinary(t, name, append(args, pkg)...)
}

// releaseBuildFlags reads the one go build line of the release workflow,
// its continuations joined, and gives its flags but -o, with the tag a
// release is cut for made up, and the package it builds.
func releaseBuildFlags(t *testing.T) ([]string, string) {
	t.Helper()
	workflow := readText(t, filepath.Join(buildDir, "..", "..", ".github", "workflows", "release.yml"))
	var builds [][]string
	for _, line := range joinedLines(workflow) {
		words := shellWords(line)
		for i := 0; i+1 < len(words); i++ {
			if words[i] == "go" && words[i+1] == "build" {
				builds = append(builds, words[i+2:])
				break
			}
		}
	}
	if len(builds) != 1 {
		t.Fatalf("the release workflow has %d go build lines: %q", len(builds), builds)
	}
	words := builds[0]
	if len(words) == 0 || words[len(words)-1] != "./cmd/eva-md" {
		t.Fatalf("the release workflow's go build doesn't end with ./cmd/eva-md: %q", words)
	}
	var flags []string
	for i := 0; i < len(words)-1; i++ {
		if words[i] == "-o" {
			i++
			continue
		}
		flag := strings.NewReplacer("${TAG}", "2026.11.99", "$TAG", "2026.11.99").Replace(words[i])
		if strings.Contains(flag, "$") {
			t.Fatalf("the release's go build has a flag this test can't fill in: %q", words[i])
		}
		flags = append(flags, flag)
	}
	if !slices.Contains(flags, "-trimpath") || !slices.Contains(flags, "-ldflags") {
		t.Fatalf("read %q from the release's go build; the check read the wrong line", flags)
	}
	return flags, words[len(words)-1]
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

// tagsFlag finds a -tags or --tags flag, alone or with its value after =,
// and not git's --no-tags.
var (
	tagsFlag = regexp.MustCompile(`(?:^|[\s'",=])--?tags\b`)
	goBuilds = regexp.MustCompile(`\bgo['",\s]+(?:build|install|run)\b`)
)

// tagGaps gives what in a file of a script or a workflow could give a
// release the evaluation build's tag: the tag's name, a go build, go
// install or go run line that passes -tags, or a GOFLAGS that holds it.
func tagGaps(name, text string) []string {
	var gaps []string
	if strings.Contains(text, "evamd_eval") {
		gaps = append(gaps, name+" names evamd_eval")
	}
	for _, line := range joinedLines(text) {
		if at := goBuilds.FindStringIndex(line); at != nil && tagsFlag.MatchString(line[at[1]:]) {
			gaps = append(gaps, name+" passes -tags to go: "+strings.TrimSpace(line))
		}
		if at := strings.Index(line, "GOFLAGS"); at >= 0 && tagsFlag.MatchString(line[at:]) {
			gaps = append(gaps, name+" sets GOFLAGS with -tags: "+strings.TrimSpace(line))
		}
	}
	return gaps
}

// No release can carry the evaluation build: without the tag go doesn't
// compile evalbuild.go, and with it compiles it and nothing else besides;
// no script or workflow names the tag, passes -tags to go build, go install
// or go run, or sets a GOFLAGS that holds it; and the release's own build
// records no tags, while the same build with the tag records it, which is
// what the evaluation reads.
func TestNoReleaseCarriesTheEvaluationBuild(t *testing.T) {
	list := func(args ...string) []string {
		t.Helper()
		var files []string
		out := goCommand(t, buildDir, nil, append(append([]string{"list"}, args...), "-f", "{{json .GoFiles}}", ".")...)
		if err := json.Unmarshal([]byte(out), &files); err != nil || len(files) < 10 {
			t.Fatalf("go list gave %q (%v)", out, err)
		}
		return files
	}
	plain, tagged := list(), list("-tags", "evamd_eval")
	if slices.Contains(plain, "evalbuild.go") {
		t.Errorf("a build without the tag compiles evalbuild.go: %q", plain)
	}
	if want := append(slices.Clone(plain), "evalbuild.go"); !slices.Equal(sortedCopy(tagged), sortedCopy(want)) {
		t.Errorf("with the tag go compiles %q, not %q", tagged, want)
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

	tags := func(binary string) (string, bool) {
		t.Helper()
		info, err := buildinfo.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		for _, setting := range info.Settings {
			if setting.Key == "-tags" {
				return setting.Value, true
			}
		}
		return "", false
	}
	if value, ok := tags(releaseTagBuild(t, false)); ok {
		t.Errorf("the release's build records the tags %q", value)
	}
	if value, _ := tags(releaseTagBuild(t, true)); !slices.Contains(strings.Split(value, ","), "evamd_eval") {
		t.Errorf("the release's build with the evaluation's tag records the tags %q", value)
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
		"remote=$(git ls-remote --tags origin)\n",
		"GOFLAGS='-mod=readonly -count=1' go test ./...\n",
		"go test -tags evamd_testlimits ./...\n",
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
		"env:\n  GOFLAGS: -tags=other\n",
		"export GOFLAGS='-mod=readonly -tags other'\n",
		"# the evamd_eval build\n",
	} {
		if gaps := tagGaps("bad.yml", text); len(gaps) == 0 {
			t.Errorf("%q gives no gap", text)
		}
	}
	got := shellWords(joinedLines("GOARCH=$arch go build -trimpath -ldflags \"-X main.version=$TAG\" \\\n  -o \"$out/x\" ./cmd/eva-md")[0])
	want := []string{"GOARCH=$arch", "go", "build", "-trimpath", "-ldflags", "-X main.version=$TAG", "-o", "$out/x", "./cmd/eva-md"}
	if !slices.Equal(got, want) {
		t.Errorf("read %q, not %q", got, want)
	}
}

// In the evaluation build a hook with EVA_MD_EVAL_ACTOR=person takes a
// Claude Code chat that a program started, as print mode starts one, for a
// person's: its prompt is stored as the user's. The evaluation build with
// another value or none, and the release stand-in with the variable, store
// only the event host_prompt, as for any program's chat. Both builds raise
// the time limits, so a slow machine doesn't decide the outcome.
func TestTheEvaluationBuildTakesAScriptedChatForAPersons(t *testing.T) {
	eval := evalBinary(t, "standin", "-mod=readonly", "-buildvcs=false", "-tags", "evamd_testlimits,evamd_eval", "./cmd/eva-md")
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
