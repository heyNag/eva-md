package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// isolated gives a test its own home and git config, and a PATH of one new
// folder holding only a link to git, so no test can reach a real gh, glab,
// claude or codex; GH_HOST and GITLAB_HOST are cleared. A test that needs a
// fake CLI writes it into that folder, which pathFolder returns.
func isolated(t *testing.T) paths.Dirs {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("EVA_MD_INNER", "")
	t.Setenv("CLAUDE_CODE_REMOTE", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	folder := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if git, err := exec.LookPath("git"); err == nil {
		if err := os.Symlink(git, filepath.Join(folder, "git")); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", folder)
	for _, key := range []string{"GH_HOST", "GITLAB_HOST"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	d, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// pathFolder is the PATH folder isolated made.
func pathFolder() string { return os.Getenv("PATH") }

func TestCommands(t *testing.T) {
	isolated(t)
	for _, tc := range []struct {
		args     []string
		code     int
		contains string
	}{
		{nil, 2, "usage:"}, {[]string{"version"}, 0, "okf_spec 0.2"}, {[]string{"version", "extra"}, 2, "no arguments"},
		{[]string{"install", "--invalid"}, 2, "invalid arguments"}, {[]string{"update"}, 2, "needs --session host:id or --pending"},
		{[]string{"show"}, 2, "requires"}, {[]string{"show", "a", "b"}, 2, "requires"}, {[]string{"brief", "--as", "compact"}, 2, "requires --session"},
		{[]string{"show", "x#Timeline", "--last", "-1"}, 2, "--last must be"}, {[]string{"show", "x#Timeline", "--last", "0"}, 2, "--last must be"},
		{[]string{"show", "--last", "3", "s:abc#1"}, 1, "apply only to a page's #Timeline"}, {[]string{"show", "x#Timeline", "--since", "2025-1-5"}, 1, "--since must be"},
		{[]string{"search"}, 2, "requires at least one term"}, {[]string{"search", "--", " "}, 2, "requires at least one term"},
		{[]string{"search", "x", "--limit", "51"}, 2, "--limit must be"}, {[]string{"search", "--days", "-1", "x"}, 2, "--days must be"},
		{[]string{"search", "--bad", "x"}, 2, "invalid arguments for search"},
		{[]string{"status", "--live"}, 2, "status makes no model call; run eva-md doctor --live"},
		{[]string{"maintain", "extra"}, 2, "invalid arguments for maintain"}, {[]string{"maintain", "--push"}, 2, "invalid arguments for maintain"},
		{[]string{"maintain"}, 1, "no wiki is set up"},
		{[]string{"init"}, 2, "requires --remote"}, {[]string{"validate"}, 1, "pass --dir"},
		{[]string{"init", "--remote", "git@github.com:me/eva-wiki.git"}, 2, "requires --remote and --dir"},
		{[]string{"setup"}, 2, "pass --dir and --remote"}, {[]string{"setup", "--host", "x"}, 2, "--host must be"},
		{[]string{"init", "--bad"}, 2, "invalid arguments"}, {[]string{"validate", "extra"}, 2, "invalid arguments"},
	} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), tc.args, strings.NewReader(""), &out, &errOut)
		if code != tc.code || !strings.Contains(out.String()+errOut.String(), tc.contains) {
			t.Errorf("%v: code %d stdout %q stderr %q", tc.args, code, out.String(), errOut.String())
		}
	}
}

func TestHookBinaryBoundary(t *testing.T) {
	d := isolated(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"codex", "claude-code"} {
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "session_id": "cli-smoke", "cwd": cwd, "last_assistant_message": "Finished the fixture."})
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{"hook", "--host", host}, bytes.NewReader(payload), &out, &errOut); code != 0 {
			t.Fatal(code)
		}
		want := ""
		if host == "codex" {
			want = "{}"
		}
		if out.String() != want || errOut.Len() != 0 {
			t.Fatalf("%s output %q stderr %q", host, out.String(), errOut.String())
		}
		files, _ := filepath.Glob(filepath.Join(d.Inbox(), "*", host+"-*.jsonl"))
		if len(files) != 1 {
			t.Fatalf("%s captured files: %v", host, files)
		}
		entries, _, err := (inbox.Store{Root: d.Inbox()}).ReadFrom(files[0], 0)
		if err != nil || len(entries) != 1 || entries[0].Text != "Finished the fixture." {
			t.Fatalf("%s capture: %+v %v", host, entries, err)
		}
	}
}

func TestHookBadConfigFailsClosed(t *testing.T) {
	d := isolated(t)
	cwd, _ := os.Getwd()
	if err := paths.EnsureDir(d.Config); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.ConfigFile(), []byte("exclude = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	payload, _ := json.Marshal(map[string]string{"hook_event_name": "Stop", "cwd": cwd, "session_id": "bad-config", "last_assistant_message": "must not persist"})
	if code := run(context.Background(), []string{"hook", "--host", "codex"}, bytes.NewReader(payload), &out, &errOut); code != 0 {
		t.Fatal(code)
	}
	if out.String() != "{}" || errOut.Len() != 0 {
		t.Fatalf("output %q %q", out.String(), errOut.String())
	}
	if _, err := os.Stat(d.Inbox()); !os.IsNotExist(err) {
		t.Fatalf("capture with bad config: %v", err)
	}
}

func TestHookBadArgumentsNeverBlock(t *testing.T) {
	isolated(t)
	for _, args := range [][]string{{"hook"}, {"hook", "--host", "unknown"}, {"hook", "--bad"}} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, strings.NewReader("{}"), &out, &errOut); code != 0 || out.Len() != 0 || errOut.Len() != 0 {
			t.Fatalf("hook argument failure leaked or blocked")
		}
	}
}

// The usage line lists every built command, and every command it lists is
// one run dispatches.
func TestUsageListsEveryCommand(t *testing.T) {
	isolated(t)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), nil, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatal(code)
	}
	listed := strings.Fields(strings.NewReplacer("usage: eva-md", "", "|", " ").Replace(errOut.String()))
	built := []string{"setup", "version", "init", "install", "uninstall", "doctor", "status", "brief", "show", "search", "update", "commit", "maintain", "system", "validate", "hook"}
	if strings.Join(listed, " ") != strings.Join(built, " ") {
		t.Fatalf("usage lists %q", listed)
	}
	for _, command := range listed {
		var out, errOut bytes.Buffer
		run(context.Background(), []string{command, "--no-such-flag"}, strings.NewReader(""), &out, &errOut)
		if strings.Contains(errOut.String(), "unknown command") {
			t.Errorf("%s is listed but not dispatched", command)
		}
	}
}

// eva-md doctor --live runs the update's exact command once for each host
// eva-md is installed for, against the built-in fixture, here with fake
// CLIs: a valid answer passes with the effort it was accepted at, a failure
// fails with its class and fix, and a host eva-md isn't installed for makes
// no call. Nothing is written to the wiki or the run log.
func TestDoctorLive(t *testing.T) {
	d := isolated(t)
	home := os.Getenv("HOME")
	wiki := filepath.Join(home, "wiki")
	if err := os.MkdirAll(filepath.Join(wiki, "general"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiki, "index.md"), []byte("# Wiki\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDir(d.Config); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.ConfigFile(), []byte("wiki_path = "+jsonQuote(wiki)+"\ninstance = \"test-0a1b2c3d\"\n[effort]\nclaude-code = \"high\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installedFor := func(host string) {
		marker := filepath.Join(d.Share, "marketplace", host, ".eva-md-managed")
		if err := paths.EnsureDir(filepath.Dir(marker)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, []byte("eva-md marketplace v1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	installedFor("claude")
	// The fakes answer the update's command only, and fail at once for the
	// doctor's own host queries. Shell built-ins only: PATH holds nothing
	// else.
	answer := `{"summary":"The build script has a verbose flag, off by default.","last_chat":"Added a verbose flag to the build script.",` +
		`"done":[{"text":"Added the verbose flag to the build script","refs":[2]}],"items":[],"timeline":[],"unknowns":[]}`
	fakes := map[string]string{
		"claude": "#!/bin/sh\n[ \"$1\" = -p ] || exit 1\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done > \"$HOME/claude-stdin\"\nprintf '%s\\n' '" +
			`{"type":"result","subtype":"success","is_error":false,"permission_denials":[],"structured_output":` + answer + "}'\n",
		"codex": "#!/bin/sh\n[ \"$2\" = exec ] || exit 1\nwhile IFS= read -r line; do :; done\nprintf '%s\\n' 'Error: Not logged in. Run codex login.' >&2\nexit 1\n",
	}
	for name, script := range fakes {
		if err := os.WriteFile(filepath.Join(pathFolder(), name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	before := fileTree(t, wiki)
	live := func() (map[string]struct{ Status, Detail, Fix string }, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		run(context.Background(), []string{"doctor", "--live", "--json"}, strings.NewReader(""), &out, &errOut)
		var r struct {
			Checks []struct{ Name, Status, Detail, Fix string }
		}
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatalf("doctor --live --json: %v\n%s", err, out.String())
		}
		rows := map[string]struct{ Status, Detail, Fix string }{}
		for _, c := range r.Checks {
			rows[c.Name] = struct{ Status, Detail, Fix string }{c.Status, c.Detail, c.Fix}
		}
		return rows, errOut.String()
	}
	rows, progress := live()
	claude, codex := rows["claude-code live update"], rows["codex live update"]
	if claude.Status != "PASS" || !strings.Contains(claude.Detail, "valid output in ") || !strings.Contains(claude.Detail, "effort high accepted") {
		t.Fatalf("claude: %+v", claude)
	}
	if codex.Status != "UNKNOWN" || !strings.Contains(codex.Detail, "not installed for codex; no call was made") {
		t.Fatalf("codex: %+v", codex)
	}
	if !strings.Contains(progress, "running claude-code's update command") || strings.Contains(progress, "running codex's") {
		t.Fatalf("progress %q", progress)
	}
	stdin, err := os.ReadFile(filepath.Join(home, "claude-stdin"))
	if err != nil || !strings.Contains(string(stdin), "[1] user 2026-01-15") {
		t.Fatalf("the fake claude read %q (%v)", stdin, err)
	}
	installedFor("codex")
	rows, _ = live()
	if codex = rows["codex live update"]; codex.Status != "FAIL" || !strings.HasPrefix(codex.Detail, "auth after ") || codex.Fix != "log in with codex login, then run eva-md doctor --live again" {
		t.Fatalf("codex: %+v", codex)
	}
	if after := fileTree(t, wiki); after != before {
		t.Fatalf("the live check changed the wiki:\n%s\n---\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(d.State, "updates")); !os.IsNotExist(err) {
		t.Fatalf("the live check wrote the run log: %v", err)
	}
}

// fileTree lists a folder's files with their sizes.
func fileTree(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		b.WriteString(rel + " " + info.Mode().String() + " " + jsonQuote(info.ModTime().String()) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func jsonQuote(s string) string { data, _ := json.Marshal(s); return string(data) }
