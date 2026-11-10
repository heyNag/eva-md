package health

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// updateGolden writes the golden files again from what the checks give now:
// go test ./internal/health -run TestGolden -update-golden.
var updateGolden = flag.Bool("update-golden", false, "write the golden files of eva-md status and doctor again")

// The fixture homes of the golden rows.
const (
	bareHome   = iota // nothing set up, not even a config
	setUpHome         // set up as far as a test can take it
	codexHome         // set up, with a stand-in codex on PATH
	noWikiHome        // a config that names no wiki, as after a setup cut short
)

// goldenHome makes a fixture home. Set up, it has a wiki with its schema,
// its remote and a system page, a config with an unknown key, an inbox with
// sessions that wait for an update and one done, and heartbeats from long
// ago. A noWikiHome has all of that but the wiki: its config has no
// wiki_path, as after a setup cut short. PATH holds git alone, so neither
// host's command is found and no row depends on this machine's apps; in a
// codexHome it holds a stand-in codex too, which reports its hooks feature
// on and fails every other command. It returns the home's folders and the
// home, which golden output names as <home>.
func goldenHome(t *testing.T, kind int) (paths.Dirs, string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	d := isolated(t)
	home := os.Getenv("HOME")
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	if kind == codexHome {
		put(t, filepath.Join(bin, "codex"), []byte("#!/bin/sh\nif [ \"$1\" = features ]; then printf 'hooks  stable  true\\n'; exit 0; fi\nexit 1\n"))
		if err := os.Chmod(filepath.Join(bin, "codex"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	if kind == bareHome {
		return d, home
	}

	c := config.Default()
	if kind != noWikiHome {
		wiki := filepath.Join(home, "wiki")
		for _, args := range [][]string{{"init", "-q", wiki}, {"-C", wiki, "remote", "add", "origin", "git@github.com:example/wiki.git"}} {
			if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				t.Fatalf("fixture git: %v %s", err, out)
			}
		}
		// macOS reaches its temporary folders through an alias (/var for
		// /private/var), and the wiki checks compare the configured path
		// with the one git resolves, so the fixture names the resolved one,
		// as this package's other wiki fixtures do.
		if wiki, err = filepath.EvalSymlinks(wiki); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(wiki, 0o700); err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(wiki, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Wiki\n"))
		if err := config.SaveSchema(filepath.Join(wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: "test", Created: "2026-10-29"}); err != nil {
			t.Fatal(err)
		}
		c.WikiPath, c.Instance, c.ExpectedRemote = wiki, "test", "github.com/example/wiki"
		systemFile(t, c, "billing", "Billing", "2026-10-30", "invoices")
		for _, f := range []string{"general/threads/default.md", "projects/a/threads/main.md", "projects/a/threads/feature-x.md"} {
			put(t, filepath.Join(wiki, f), []byte("x"))
		}
	}
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(d.ConfigFile(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("colour = \"blue\"\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	store := inbox.Store{Root: d.Inbox()}
	for i, id := range []string{"one", "two", "three"} {
		file := store.Path("github.com__example__a", "codex", id)
		put(t, file, []byte(strings.Repeat("{}\n", 10)))
		if i == 0 {
			if err := store.SetMark(file, inbox.Mark{Offset: 30}); err != nil {
				t.Fatal(err)
			}
		}
	}
	put(t, filepath.Join(d.State, "heartbeats", "claude-code", "SessionStart.json"),
		[]byte(`{"at":"2026-01-02T03:04:05Z","host":"claude-code","event":"SessionStart","transcript_available":false,"entrypoint":"sdk-cli","headless":true}`))
	put(t, filepath.Join(d.State, "heartbeats", "codex", "Stop.json"),
		[]byte(`{"at":"2026-01-02T03:04:06Z","host":"codex","event":"Stop","transcript_available":true}`))
	return d, home
}

// render is a report as eva-md status and doctor print it, in text and
// with --json, with the fixture's home named <home>.
func render(t *testing.T, r Report, home string) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, check := range r.Checks {
		fmt.Fprintf(&out, "%s  %s: %s", check.Status, check.Name, check.Detail)
		if check.Fix != "" {
			fmt.Fprintf(&out, "; %s", check.Fix)
		}
		fmt.Fprintln(&out)
	}
	out.WriteString("--- json\n")
	if err := json.NewEncoder(&out).Encode(r); err != nil {
		t.Fatal(err)
	}
	// The resolved form of the home goes first: on macOS it holds the
	// unresolved one (/private/var/... holds /var/...).
	text := out.Bytes()
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
		text = bytes.ReplaceAll(text, []byte(resolved), []byte("<home>"))
	}
	return bytes.ReplaceAll(text, []byte(home), []byte("<home>"))
}

// doctor is eva-md doctor's checks, and local the doctor's checks without
// asking the hosts.
func doctor(ctx context.Context, d paths.Dirs) Report { return Inspect(ctx, d, true) }
func local(ctx context.Context, d paths.Dirs) Report  { return Inspect(ctx, d, false) }

// The rows eva-md status and doctor print, with and without asking the
// hosts, on fixture homes, are pinned byte for byte, so a change to how the
// rows are put together can't change them unseen. The doctor --live rows
// are added after these, by the command.
func TestGoldenRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		home int
		run  func(context.Context, paths.Dirs) Report
	}{
		{"status-bare", bareHome, Status},
		{"doctor-bare", bareHome, doctor},
		{"status", setUpHome, Status},
		{"doctor", setUpHome, doctor},
		{"doctor-local", setUpHome, local},
		{"status-codex", codexHome, Status},
		{"doctor-codex", codexHome, doctor},
		{"status-nowiki", noWikiHome, Status},
		{"doctor-nowiki", noWikiHome, doctor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, home := goldenHome(t, tc.home)
			got := render(t, tc.run(t.Context(), d), home)
			file := filepath.Join("testdata", tc.name+".golden")
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s rows differ from %s:\n--- got\n%s--- want\n%s", tc.name, file, got, want)
			}
		})
	}
}
