package hostfake

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --version prints the version set, the defaults first, and features list's
// hooks row is experimental before 0.167.0 and stable from it.
func TestVersions(t *testing.T) {
	h := newHost(t, Codex)
	for cli, want := range map[string]string{Codex: "codex-cli 0.167.0\n", Claude: "2.1.321 (Claude Code)\n"} {
		if out, err := h.f.Run(context.Background(), cli, "--version"); err != nil || string(out) != want {
			t.Errorf("%s --version = %q, %v; want %q", cli, out, err, want)
		}
	}
	if err := h.f.SetVersion(Codex, "0.166.0"); err != nil {
		t.Fatal(err)
	}
	if out := h.run(t, "--version"); string(out) != "codex-cli 0.166.0\n" {
		t.Errorf("--version after SetVersion = %q", out)
	}
	if out := h.run(t, "features", "list"); !strings.Contains(string(out), "\nhooks experimental true\n") {
		t.Errorf("features list at 0.166.0 = %q", out)
	}
	if err := h.f.SetVersion(Codex, "0.167"); err == nil {
		t.Error("SetVersion took a version that isn't N.N.N")
	}
	// The CLI is found by its last element too, as a probe gives a path.
	if out, err := h.f.Run(context.Background(), "/made/up/bin/codex", "--version"); err != nil || string(out) != "codex-cli 0.166.0\n" {
		t.Errorf("the CLI by its path = %q, %v", out, err)
	}
}

// Codex 0.166.0 fails its lists, in its words, while a registered
// marketplace's folder has no manifest, naming the folder with its symlinks
// resolved; a list of another marketplace still answers.
func TestMissingMarketplaceFolder(t *testing.T) {
	dir := realTemp(t)
	f := New(filepath.Join(dir, "fake"))
	real := filepath.Join(dir, "real")
	write(t, filepath.Join(real, ".agents", "plugins", "marketplace.json"), `{"name": "eva-md", "plugins": []}`+"\n")
	if err := os.Symlink(real, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other")
	write(t, filepath.Join(other, ".agents", "plugins", "marketplace.json"), `{"name": "other", "plugins": []}`+"\n")
	for _, path := range []string{filepath.Join(dir, "link"), other} {
		if _, err := f.Run(context.Background(), Codex, "plugin", "marketplace", "add", path, "--json"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(real); err != nil {
		t.Fatal(err)
	}
	want := "Error: failed to load marketplace(s):\n- `eva-md` at " + real + ": marketplace root does not contain a supported manifest\n"
	for _, args := range [][]string{
		{"plugin", "marketplace", "list", "--json"},
		{"plugin", "list", "--json", "--marketplace=eva-md"},
		{"plugin", "list", "--json"},
	} {
		_, err := f.Run(context.Background(), Codex, args...)
		var e *ExitError
		if !errors.As(err, &e) || e.Code != 1 || e.Stderr != want {
			t.Errorf("%v gave %v, %q; want %q", args, err, stderrOf(err), want)
		}
	}
	if _, err := f.Run(context.Background(), Codex, "plugin", "list", "--json", "--marketplace=other"); err != nil {
		t.Errorf("another marketplace's list failed: %v", err)
	}
	// Removing the registration answers, so uninstall can clear it.
	for _, args := range [][]string{{"plugin", "remove", "eva-md@eva-md", "--json"}, {"plugin", "marketplace", "remove", "eva-md", "--json"}} {
		if _, err := f.Run(context.Background(), Codex, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	if _, err := f.Run(context.Background(), Codex, "plugin", "marketplace", "list", "--json"); err != nil {
		t.Errorf("the list after the removal: %v", err)
	}
}

func stderrOf(err error) string {
	var e *ExitError
	if errors.As(err, &e) {
		return e.Stderr
	}
	return ""
}

// Claude Code refuses to install a plugin it has and keeps it; it keeps
// listing a marketplace removed with --scope. Codex installs again, and
// removing a plugin removes its copy.
func TestRegistrationQuirks(t *testing.T) {
	claude := newHost(t, Claude)
	_, err := claude.f.Run(context.Background(), Claude, "plugin", "install", "eva-md@eva-md", "--scope", "user", "--json")
	if e, ok := err.(*ExitError); !ok || e.Code != 1 || !strings.Contains(e.Stderr, "already installed") {
		t.Errorf("a second install gave %v", err)
	}
	if out := claude.run(t, "plugin", "list", "--json"); !strings.Contains(string(out), `"readFromFolder":"`+claude.plugin+`"`) || !strings.Contains(string(out), `"folderVersion":"1"`) {
		t.Errorf("after the refusal the list is %s", out)
	}
	claude.run(t, "plugin", "marketplace", "remove", "eva-md", "--scope", "user", "--json")
	if out := claude.run(t, "plugin", "marketplace", "list", "--json"); !strings.Contains(string(out), claude.market) {
		t.Errorf("a scoped remove took the marketplace off the list: %s", out)
	}
	claude.run(t, "plugin", "marketplace", "remove", "eva-md", "--json")
	if out := claude.run(t, "plugin", "marketplace", "list", "--json"); string(out) != "[]\n" {
		t.Errorf("an unscoped remove left %s", out)
	}

	codex := newHost(t, Codex)
	write(t, filepath.Join(codex.plugin, "skills", "recall", "SKILL.md"), "a newer made-up skill\n")
	codex.install(t)
	if data, err := os.ReadFile(filepath.Join(codex.cache(), "skills", "recall", "SKILL.md")); err != nil || string(data) != "a newer made-up skill\n" {
		t.Errorf("a second add didn't copy the plugin again: %q %v", data, err)
	}
	codex.run(t, "plugin", "remove", "eva-md@eva-md", "--json")
	if _, err := os.Stat(codex.cache()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the copy is still there after the remove: %v", err)
	}
	if got := hooksList(t, codex.f); len(got) != 0 {
		t.Errorf("hooks/list after the remove: %v", got)
	}
	if fired := codex.fire(t, "SessionStart"); len(fired) != 0 {
		t.Errorf("a removed plugin's hooks fired: %v", fired)
	}
}

// A copy without .codex-plugin/plugin.json loads no hooks.
func TestCodexLoadsHooksOnlyWithItsManifest(t *testing.T) {
	h := newHost(t, Codex)
	h.approve(t)
	if err := os.Remove(filepath.Join(h.cache(), ".codex-plugin", "plugin.json")); err != nil {
		t.Fatal(err)
	}
	if got := hooksList(t, h.f); len(got) != 0 {
		t.Errorf("hooks/list without the manifest: %v", got)
	}
	if fired := h.fire(t, "SessionStart"); len(fired) != 0 {
		t.Errorf("hooks fired without the manifest: %v", fired)
	}
}
