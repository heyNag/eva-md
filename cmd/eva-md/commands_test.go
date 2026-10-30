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
	for _, c := range r.Checks {
		if c.Name == "background updates" && c.Status == "PASS" {
			t.Fatal("unbuilt background updates reported as ready")
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
