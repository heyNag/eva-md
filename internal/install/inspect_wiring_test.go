package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Uninstall refuses an install folder without eva-md's marker that holds
// any of what eva-md would put there: the binary, or the marketplace folder
// of any host. It removes nothing and asks no host.
func TestUninstallRefusesAnUnmanagedFolderOfAnyHostsMarketplace(t *testing.T) {
	for _, name := range []string{"bin/eva-md", "marketplace/claude", "marketplace/codex"} {
		t.Run(name, func(t *testing.T) {
			opts, f := setup(t)
			path := filepath.Join(opts.Dirs.Share, name)
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, apply := range []bool{false, true} {
				f.calls = nil
				_, err := uninstallWith(context.Background(), UninstallOptions{Dirs: opts.Dirs, Apply: apply, All: true}, f.run)
				if err == nil || err.Error() != "install: refusing to uninstall an unmanaged installation" {
					t.Fatalf("apply %v: %v", apply, err)
				}
				if len(f.calls) != 0 {
					t.Fatalf("apply %v asked the hosts: %v", apply, f.calls)
				}
				for _, kept := range []string{path, opts.Dirs.ConfigFile()} {
					if _, err := os.Stat(kept); err != nil {
						t.Fatalf("apply %v removed %s: %v", apply, kept, err)
					}
				}
			}
		})
	}
	// A folder holding none of them is not refused.
	opts, f := setup(t)
	if err := os.MkdirAll(filepath.Join(opts.Dirs.Share, "marketplace", "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstallWith(context.Background(), UninstallOptions{Dirs: opts.Dirs}, f.run); err != nil {
		t.Fatalf("a folder with another marketplace: %v", err)
	}
}

// Only a host that fails its lists while eva-md's marketplace folder is
// missing reads such a failure as that: Claude Code's list failing in
// Codex's words is a failure of the host, and its status is not a missing
// folder.
func TestInspectReadsAMissingFolderOnlyForTheHostThatFailsSo(t *testing.T) {
	opts, f := setup(t)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(opts.Dirs.Share, "marketplace")); err != nil {
		t.Fatal(err)
	}
	want := missingFolder("claude plugin marketplace list --json", marketplacePath(opts.Dirs.Share, "claude"))
	run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if host == "claude" && strings.Join(args, " ") == "plugin marketplace list --json" {
			return nil, want
		}
		return f.run(ctx, host, args...)
	}
	s, err := inspect(context.Background(), opts.Dirs, "claude", run)
	if !errors.Is(err, want) || s.FolderMissing || s.Marketplace {
		t.Fatalf("claude with its list failing in Codex's words: %+v %v", s, err)
	}
	// Codex, whose list fails so, gives the missing folder.
	s, err = inspect(context.Background(), opts.Dirs, "codex", f.run)
	if err != nil || !s.FolderMissing || !s.Marketplace || s.Reason != "eva-md's marketplace folder is missing" {
		t.Fatalf("codex with its folder gone: %+v %v", s, err)
	}
}

// Inspect's reason for a plugin loaded from another folder, or from another
// folder version, is v1's, word for word, for each host, and install's
// error carries it.
func TestInspectGivesEachHostsSourceDiffersReason(t *testing.T) {
	cases := []struct {
		name, host string
		edit       func(entry map[string]any)
		reason     string
	}{
		{"claude folder", "claude", func(e map[string]any) { e["readFromFolder"] = "/elsewhere/eva-md" }, "Claude source folder or folder version differs"},
		{"claude folder version", "claude", func(e map[string]any) { e["folderVersion"] = "2" }, "Claude source folder or folder version differs"},
		{"codex folder", "codex", func(e map[string]any) {
			e["source"] = map[string]any{"source": "local", "path": "/elsewhere/eva-md"}
		}, "Codex source folder differs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, f := setup(t)
			opts.Host = c.host
			// The host lists eva-md's entry with the edit made.
			run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
				data, err := f.run(ctx, host, args...)
				if err != nil || len(args) < 3 || args[0] != "plugin" || args[1] != "list" {
					return data, err
				}
				var value any
				if err = json.Unmarshal(data, &value); err != nil {
					t.Fatal(err)
				}
				list, _ := value.([]any)
				if m, ok := value.(map[string]any); ok {
					list, _ = m["installed"].([]any)
				}
				for _, item := range list {
					if entry := item.(map[string]any); entry["name"] == "eva-md" {
						c.edit(entry)
					}
				}
				return json.Marshal(value)
			}
			_, err := runInstall(context.Background(), opts, run)
			if want := "install: " + c.host + " verification failed: " + c.reason + "; this run's changes were rolled back"; err == nil || err.Error() != want {
				t.Fatalf("install: %v, want %q", err, want)
			}
			// Installed as it should be, the host then lists the entry edited.
			if _, err = runInstall(context.Background(), opts, f.run); err != nil {
				t.Fatal(err)
			}
			s, err := inspect(context.Background(), opts.Dirs, c.host, f.run)
			if err != nil || !s.Valid {
				t.Fatalf("status as installed: %+v %v", s, err)
			}
			s, err = inspect(context.Background(), opts.Dirs, c.host, run)
			if err != nil || s.Valid || !s.Installed || s.Reason != c.reason {
				t.Fatalf("status: %+v %v, want reason %q", s, err, c.reason)
			}
		})
	}
}
