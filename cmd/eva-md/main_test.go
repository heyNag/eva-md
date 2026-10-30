package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

func isolated(t *testing.T) paths.Dirs {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("EVA_MD_INNER", "")
	t.Setenv("CLAUDE_CODE_REMOTE", "")
	d, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCommands(t *testing.T) {
	isolated(t)
	for _, tc := range []struct {
		args     []string
		code     int
		contains string
	}{
		{nil, 2, "usage:"}, {[]string{"version"}, 0, "okf_spec 0.2"}, {[]string{"version", "extra"}, 2, "no arguments"},
		{[]string{"install", "--invalid"}, 2, "invalid arguments"}, {[]string{"update"}, 2, "unknown command"},
		{[]string{"show"}, 2, "requires"}, {[]string{"brief", "--as", "compact"}, 2, "requires --session"},
		{[]string{"doctor", "--live"}, 2, "not built yet"},
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
