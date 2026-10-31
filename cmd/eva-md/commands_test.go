package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
)

func TestBriefAndShowCLIResumeAcrossHosts(t *testing.T) {
	d := isolated(t)
	cwd, _ := os.Getwd()
	p, err := identity.Resolve(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	s := inbox.Store{Root: d.Inbox()}
	now := time.Now().Add(-time.Minute)
	for n := 0; n < 3; n++ {
		for _, role := range []string{"user", "assistant"} {
			kind := "prompt"
			text := fmt.Sprintf("Decision %d: use the amber switch; next run the integration check.", n)
			if role == "assistant" {
				kind = "reply"
				text = "Suggested checking the amber switch."
			}
			e := inbox.Entry{V: 1, TS: now.Format(time.RFC3339), Host: "claude-code", Session: "abcdefgh-session-a", Turn: fmt.Sprint(n), Role: role, Kind: kind, Text: text, Project: p.Key, Branch: p.Branch, Cwd: cwd, Origin: "hook"}
			if err := s.Append(e); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Second)
		}
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"brief", "--cwd", cwd, "--host", "codex"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("brief %d %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "amber switch") || !strings.Contains(out.String(), "eva-memory") {
		t.Fatalf("no deterministic recall: %q", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"show", "s:abcdefgh#1"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("show %d %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Decision 0") {
		t.Fatal(out.String())
	}
}

func TestSearchCLIFindsWhatShowResolves(t *testing.T) {
	d := isolated(t)
	cwd, _ := os.Getwd()
	p, err := identity.Resolve(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	s := inbox.Store{Root: d.Inbox()}
	for i, text := range []string{"Remember the tangerine protocol.", "Run go test -race before merging."} {
		e := inbox.Entry{V: 1, TS: time.Now().Add(-time.Minute).Format(time.RFC3339), Host: "codex", Session: "searchcli-session", Turn: fmt.Sprint(i), Role: "user", Kind: "prompt", Text: text, Project: p.Key, Branch: p.Branch, Cwd: cwd, Origin: "hook"}
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		// Flags may follow the terms, and "--" lets a term start with a dash.
		{[]string{"search", "tangerine", "--limit", "5", "protocol", "--cwd", cwd}, "s:searchcl#1 · Remember the tangerine protocol."},
		{[]string{"search", "--cwd", cwd, "--", "-race"}, "s:searchcl#2 · Run go test -race before merging."},
		{[]string{"search", "--inbox", "tangerine", "--cwd", cwd}, "s:searchcl#1 · Remember the tangerine protocol."},
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), tc.args, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("%v: %d %s", tc.args, code, errOut.String())
		}
		// No wiki is set up, so only the inbox is named.
		if !strings.HasPrefix(out.String(), "eva.md search: ") || !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), "\nScanned 1 file (") || strings.Contains(out.String(), "thread page") {
			t.Fatalf("%v: %q", tc.args, out.String())
		}
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"show", "s:searchcl#1"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "tangerine protocol") {
		t.Fatalf("show %d %q %s", code, out.String(), errOut.String())
	}
	// --pages without a wiki says so rather than finding nothing.
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"search", "--pages", "tangerine", "--cwd", cwd}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.HasPrefix(out.String(), "No matches.\nScanned 0 thread pages (") || !strings.HasSuffix(out.String(), "; no wiki is set up, so no wiki pages were searched.\n") {
		t.Fatalf("--pages: %d %q %s", code, out.String(), errOut.String())
	}
	// An excluded folder gets nothing, as the brief does.
	c := config.Default()
	c.Exclude = []string{cwd}
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"search", "--all", "tangerine"}, strings.NewReader(""), &out, &errOut); code != 0 || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("excluded folder searched: %d %q %q", code, out.String(), errOut.String())
	}
}

func TestDoctorJSONAndStatusNeverNeedModels(t *testing.T) {
	isolated(t)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"status", "--json"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("missing config status %d", code)
	}
	var r struct {
		Checks []struct{ Name, Status string }
	}
	if json.Unmarshal(out.Bytes(), &r) != nil || len(r.Checks) == 0 {
		t.Fatal(out.String())
	}
	// No live check runs, and without a wiki the background rows can't pass.
	rows := map[string]string{}
	for _, c := range r.Checks {
		if strings.HasSuffix(c.Name, " live update") {
			t.Fatalf("status ran a live check: %+v", c)
		}
		rows[c.Name] = c.Status
	}
	for _, name := range []string{"claude-code updates", "codex updates", "daily commit"} {
		if status, ok := rows[name]; !ok || status == "PASS" {
			t.Fatalf("%s: %q, present %v", name, status, ok)
		}
	}
}

func TestInstallOutputSaysPreviewDoneOrFailed(t *testing.T) {
	d := isolated(t)
	c := config.Default()
	c.WikiPath = filepath.Join(filepath.Dir(d.Share), "wiki")
	c.ExpectedRemote = "github.com/example/wiki"
	c.Instance = "test-12345678"
	c.MachineLabel = "test"
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	// A fake claude that lists nothing, so verification fails and install
	// rolls back. The real host CLIs are never run.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nprintf '[]'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, tc := range []struct {
		args       []string
		code       int
		out, error string
	}{
		{[]string{"install", "--host", "claude", "--dry-run"}, 0, "Preview; nothing was changed", ""},
		{[]string{"install", "--host", "claude"}, 1, "Failed.", "rolled back"},
		{[]string{"uninstall"}, 0, "Preview; nothing was changed", ""},
		{[]string{"uninstall", "--apply"}, 0, "Done:", ""},
	} {
		if tc.args[0] == "uninstall" {
			// An owned, empty installation, so uninstall has an action to report.
			if err := os.MkdirAll(d.Share, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d.Share, ".eva-md-install"), []byte("eva-md installation v1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		var out, errOut bytes.Buffer
		code := run(context.Background(), tc.args, strings.NewReader(""), &out, &errOut)
		if code != tc.code || !strings.HasPrefix(out.String(), tc.out) || !strings.Contains(errOut.String(), tc.error) {
			t.Fatalf("%v: %d %q %q", tc.args, code, out.String(), errOut.String())
		}
	}
}
