package hook

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/systempage"
)

// codeSystemPage is a system page that cites code as source, its repo the
// one source names.
func codeSystemPage(title, description, source string, aliases ...string) string {
	return strings.Replace(systemPage(title, description, nil, aliases...), "  - resource: https://docs.example.com/eng/overview v3\n", "  - resource: \""+source+"\"\n", 1)
}

// plantStatus saves a code status in which each page, by slug, moved by the
// count given at the commit its page cites.
func plantStatus(t *testing.T, env Env, moved map[string]int, commit string) {
	t.Helper()
	r := coderef.Report{V: 1, At: "2026-10-29T03:10:00+07:00"}
	for slug, n := range moved {
		state := coderef.Moved
		if n == 0 {
			state = coderef.Unchanged
		}
		r.Pages = append(r.Pages, coderef.PageStatus{Slug: slug, AsOf: "2026-10-28", Repos: []coderef.RepoStatus{{Key: "github.com__acme__ledger", Commit: commit, State: state, Moved: n}}})
	}
	if err := coderef.Save(env.Dirs.State, r); err != nil {
		t.Fatal(err)
	}
}

// A prompt naming a system whose code the status measured gets a pointer
// saying how far it moved, the description cut first to fit, and no git
// runs for it. A status taken at other commits than the map's line cites,
// and one missing, damaged or over its bound, gives the pointer without a
// note.
func TestPointerCarriesTheCodeNote(t *testing.T) {
	env, p := setup(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(os.Getenv("HOME"), "git.log")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\necho \"$*\" >> '"+log+"'\nexec '"+real+"' \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	long := "Keeps the double-entry books for every region, reconciles them nightly and reports"
	systemsWiki(t, &env, map[string]string{
		"ledger":   codeSystemPage("Ledger", long, "github.com/acme/ledger@4e2bf4c:internal/books/", "LedgerSync"),
		"payments": codeSystemPage("Payments", "Charges cards and pays out", "github.com/acme/ledger@4e2bf4c", "payments-api"),
		"auth":     systemPage("Auth service", "Signs users in", nil, "auth.v2"),
	})
	plantStatus(t, env, map[string]int{"ledger": 14, "payments": 0, "auth": 3}, "4e2bf4c")
	prompt := chat(t, "codex", p, env)
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := prompt("Why does LedgerSync miss the payments-api calls auth.v2 signs?")
	note := " (code moved 14 commits since 2026-10-28): " + bin + " show systems/ledger"
	want := []string{
		"eva.md: Ledger: " + long[:pointerCap-len("eva.md: Ledger: "+note)-1] + "…" + note,
		"eva.md: Payments: Charges cards and pays out (code unchanged since 2026-10-28): " + bin + " show systems/payments",
		"eva.md: Auth service: Signs users in: " + bin + " show systems/auth",
	}
	if !reflect.DeepEqual(got, want) || len([]rune(got[0])) != pointerCap {
		t.Fatalf("pointers\n%s", strings.Join(got, "\n"))
	}
	if data, err := os.ReadFile(log); err != nil || len(data) != 0 {
		t.Fatalf("the prompt ran git: %q %v", data, err)
	}
	file := filepath.Join(env.Dirs.State, coderef.File)
	for name, spoil := range map[string]func() error{
		"other commits": func() error { plantStatus(t, env, map[string]int{"ledger": 14}, "9c1d2e3"); return nil },
		"missing":       func() error { return os.Remove(file) },
		"damaged":       func() error { return os.WriteFile(file, []byte("{not json"), 0o600) },
		"over its bound": func() error {
			plantStatus(t, env, map[string]int{"ledger": 14}, "4e2bf4c")
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			padded := strings.Replace(string(data), `"pages":[`, `"pages":[{"slug":"`+strings.Repeat("x", coderef.HookLimit)+`"},`, 1)
			return os.WriteFile(file, []byte(padded), 0o600)
		},
	} {
		if err := spoil(); err != nil {
			t.Fatal(err)
		}
		p.Session = "session-" + strings.ReplaceAll(name, " ", "-")
		if got := chat(t, "codex", p, env)("Is LedgerSync behind?"); len(got) != 1 || got[0] != "eva.md: Ledger: "+long+": "+bin+" show systems/ledger" {
			t.Fatalf("%s: %q", name, got)
		}
	}
}

// A pointer keeps the command, then the title, then the note, then the
// description: the description is cut or left out first, then the note
// gives way to its form without the day, then goes, and the title is cut
// last. A line the daily commit's scan flags falls back as before.
func TestPointerFitsTheNote(t *testing.T) {
	notes := []string{"code moved 14 commits since 2026-10-28", "code moved 14 commits"}
	tail := ": " + bin + " show systems/orders"
	full, short := " ("+notes[0]+")", " ("+notes[1]+")"
	secret := "ghp_" + strings.Repeat("ab12", 10)
	for _, tc := range []struct{ title, description, want string }{
		{"Orders", "Takes orders", "eva.md: Orders: Takes orders" + full + tail},
		{"Orders " + strings.Repeat("x", 160-len("eva.md: Orders "+full+tail)), "Takes orders", "eva.md: Orders " + strings.Repeat("x", 160-len("eva.md: Orders "+full+tail)) + full + tail},
		{"Orders " + strings.Repeat("x", 160-len("eva.md: Orders "+full+tail)+1), "Takes orders", "eva.md: Orders " + strings.Repeat("x", 160-len("eva.md: Orders "+full+tail)+1) + short + tail},
		{"Orders " + strings.Repeat("x", 160-len("eva.md: Orders "+short+tail)+1), "", "eva.md: Orders " + strings.Repeat("x", 160-len("eva.md: Orders "+short+tail)+1) + tail},
		{"Orders", "Calls the bank with " + secret, "eva.md: Orders" + full + tail},
	} {
		got := pointer(tc.title, tc.description, notes, bin, "orders")
		if got != tc.want || len([]rune(got)) > pointerCap {
			t.Errorf("pointer(%q, %q)\n got %q\nwant %q", tc.title, tc.description, got, tc.want)
		}
	}
}

// The chain the daily job runs: a page cites a clone's folder, the clone
// moves two commits, a pass measures it and saves the status, and a prompt
// naming the system says so.
func TestPromptNoteFromAPass(t *testing.T) {
	env, p := setup(t)
	clone := filepath.Join(os.Getenv("HOME"), "ledger")
	if err := os.MkdirAll(filepath.Join(clone, "internal", "books"), 0o700); err != nil {
		t.Fatal(err)
	}
	commit := func(text string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(clone, "internal", "books", "a.go"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		git(t, clone, "add", "-A")
		git(t, clone, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", text)
		out, err := exec.Command("git", "-C", clone, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	git(t, clone, "init", "--quiet", "-b", "main")
	git(t, clone, "remote", "add", "origin", "git@github.com:acme/ledger.git")
	first := commit("one")
	systemsWiki(t, &env, map[string]string{"ledger": codeSystemPage("Ledger", "Keeps the books", "github.com/acme/ledger@"+first[:7]+":internal/books/", "LedgerSync")})
	commit("two")
	commit("three")
	real, err := filepath.EvalSymlinks(clone)
	if err != nil {
		t.Fatal(err)
	}
	// A chat in the clone leaves its entry in the identity cache.
	cache := identity.Cache{Dir: filepath.Join(env.Dirs.Cache(), "identity")}
	if _, err := cache.Resolve(context.Background(), real); err != nil {
		t.Fatal(err)
	}
	code, err := systempage.ReadCode(env.Config, env.Dirs)
	if err != nil {
		t.Fatal(err)
	}
	if err := coderef.Save(env.Dirs.State, coderef.Status(context.Background(), coderef.Options{Cache: cache}, code.Pages)); err != nil {
		t.Fatal(err)
	}
	if got := chat(t, "claude-code", p, env)("Is LedgerSync behind?"); len(got) != 1 || got[0] != "eva.md: Ledger: Keeps the books (code moved 2 commits since 2026-10-28): "+bin+" show systems/ledger" {
		t.Fatalf("pointer %q", got)
	}
}
