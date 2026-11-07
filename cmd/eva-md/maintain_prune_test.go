package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// eva-md maintain removes the transcript-recovery records not saved for 30
// days, on a capture-only install and beside a wiki, and says how many; a
// fresh record and the drift record beside the hosts' folders stay.
func TestMaintainPrunesRecoveryRecords(t *testing.T) {
	month := 31 * 24 * time.Hour
	plant := func(t *testing.T, d paths.Dirs) {
		t.Helper()
		now := time.Now()
		for rel, mod := range map[string]time.Time{"codex/old-chat.json": now.Add(-month), "codex/new-chat.json": now.Add(-time.Hour), "drift-codex.json": now.Add(-month)} {
			file := filepath.Join(d.State, "catchup", rel)
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(file, mod, mod); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(t *testing.T, d paths.Dirs, code int, out, errOut string) {
		t.Helper()
		if code != 0 || !strings.Contains(out, "maintain: transcript recovery: removed 1 record\n") || errOut != "" {
			t.Fatalf("maintain: %d %q %q", code, out, errOut)
		}
		if _, err := os.Lstat(filepath.Join(d.State, "catchup", "codex", "old-chat.json")); !os.IsNotExist(err) {
			t.Fatalf("a record not saved for 31 days was kept: %v", err)
		}
		for _, rel := range []string{"codex/new-chat.json", "drift-codex.json"} {
			if _, err := os.Lstat(filepath.Join(d.State, "catchup", rel)); err != nil {
				t.Fatalf("%s was removed: %v", rel, err)
			}
		}
		if code, out, _ := evaMD(t, "maintain"); code != 0 || strings.Contains(out, "transcript recovery") {
			t.Fatalf("maintain again: %d %q", code, out)
		}
	}
	t.Run("capture only", func(t *testing.T) {
		d := isolated(t)
		plant(t, d)
		code, out, errOut := evaMD(t, "maintain")
		check(t, d, code, out, errOut)
	})
	t.Run("with a wiki", func(t *testing.T) {
		d, home := gitHomeFixture(t, "")
		remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
		gitRun(t, home, "init", "--bare", "--quiet", remote)
		if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: wiki, Dirs: d, Version: "test"}); err != nil {
			t.Fatal(err)
		}
		plant(t, d)
		code, out, errOut := evaMD(t, "maintain")
		check(t, d, code, out, errOut)
	})
}
