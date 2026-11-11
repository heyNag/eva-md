package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// homeFiles is each file and folder under root with its mode, size, time
// and the hash of its bytes.
func homeFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		state := fmt.Sprint(info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state += fmt.Sprintf(" %x", sha256.Sum256(data))
		}
		files[path] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// eva-md doctor --report prints the report as JSON, with lists of numbers
// on one line, and exits 0. It runs no command, so neither git nor a host's
// CLI nor anything that reaches the network, and with a wiki whose
// knowledge files the doctor builds again, it changes no file: it repairs
// nothing.
func TestDoctorReportRunsNothingAndWritesNothing(t *testing.T) {
	d, home := gitHomeFixture(t, "")
	remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
	gitRun(t, home, "init", "--bare", "--quiet", remote)
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: wiki, Dirs: d, Version: "test"}); err != nil {
		t.Fatal(err)
	}
	reportFile(t, filepath.Join(wiki, "systems", "payments.md"), "# a page\n")
	if err := os.Mkdir(filepath.Join(wiki, "knowledge"), 0o700); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(t.TempDir(), "ran")
	if err := os.Mkdir(ran, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"git", "claude", "codex", "ssh", "curl", "gh", "glab", "security"} {
		fake := filepath.Join(pathFolder(), name)
		_ = os.Remove(fake)
		if err := os.WriteFile(fake, []byte("#!/bin/sh\ntouch '"+filepath.Join(ran, name)+"'\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	store := inbox.Store{Root: d.Inbox()}
	if err := store.Append(inbox.Entry{TS: "2026-11-10T09:00:00Z", Host: "codex", Session: "s-1", Role: "user", Kind: "prompt", Text: "hello", Project: "general", Origin: "hook"}); err != nil {
		t.Fatal(err)
	}
	before := homeFiles(t, home)

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"doctor", "--report"}, strings.NewReader(""), &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("doctor --report: %d %q", code, errOut.String())
	}
	var r health.FieldReport
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if r.Format != health.ReportFormat || r.Version != health.Version || r.Record.Entries["prompt"] != 1 || r.Wiki == nil || r.Wiki.Pages["system"] != 1 {
		t.Fatalf("%s", out.String())
	}
	if !strings.Contains(out.String(), `"ms_bounds": [2, 5, 10, 20, 30, 50, 75, 100, 150, 200, 300, 500, 1000, 1500, 3000],`) {
		t.Errorf("a list of numbers is not on one line:\n%s", out.String())
	}
	if names, _ := os.ReadDir(ran); len(names) > 0 {
		t.Errorf("doctor --report ran %v", names)
	}
	after := homeFiles(t, home)
	for path, state := range after {
		if before[path] != state {
			t.Errorf("doctor --report changed %s", path)
		}
	}
	if len(after) != len(before) {
		t.Errorf("doctor --report left %d files and folders of %d", len(after), len(before))
	}
	if strings.Contains(out.String(), home) {
		t.Errorf("the report names the home folder:\n%s", out.String())
	}

	// The doctor itself repairs this home, so the report left a repair
	// undone.
	out.Reset()
	run(context.Background(), []string{"doctor"}, strings.NewReader(""), &out, &errOut)
	repaired := homeFiles(t, home)
	if len(repaired) == len(after) {
		t.Errorf("the doctor repaired nothing either:\n%s", out.String())
	}
}

// reportFile writes text to file, mode 0600, making its folder.
func reportFile(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// --report is the doctor's alone, and takes no other flag or argument.
func TestDoctorReportTakesNoOtherFlag(t *testing.T) {
	isolated(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"status", "--report"}, "eva-md: status takes no --report; run eva-md doctor --report\n"},
		{[]string{"doctor", "--report", "--json"}, "eva-md: doctor --report takes no other flag\n"},
		{[]string{"doctor", "--report", "--live"}, "eva-md: doctor --report takes no other flag\n"},
		{[]string{"doctor", "--report", "now"}, "eva-md: invalid arguments for doctor\n"},
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), c.args, strings.NewReader(""), &out, &errOut); code != 2 || out.Len() != 0 || errOut.String() != c.want {
			t.Errorf("eva-md %s: %d %q %q", strings.Join(c.args, " "), code, out.String(), errOut.String())
		}
	}
}
