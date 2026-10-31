package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Both plugins carry the digest skill, which runs the installed binary by
// its absolute path and names its host, in each host's documented layout,
// while hooks.json stays byte for byte what it was before the skill.
func TestDigestSkill(t *testing.T) {
	binary := "/Users/test/it's a space/.local/share/eva-md/bin/eva-md"
	for _, host := range []string{"claude", "codex"} {
		files, err := render(host, binary)
		if err != nil {
			t.Fatal(err)
		}
		golden, err := os.ReadFile(filepath.Join("testdata", host+"-hooks.golden.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(files["plugins/eva-md/hooks/hooks.json"], golden) {
			t.Fatalf("%s hooks.json changed", host)
		}
		skill := string(files[skillPath])
		front, body, ok := strings.Cut(strings.TrimPrefix(skill, "---\n"), "\n---\n")
		if !ok || !strings.HasPrefix(skill, "---\n") {
			t.Fatalf("%s skill has no frontmatter", host)
		}
		lines := strings.Split(front, "\n")
		description, _ := strings.CutPrefix(lines[1], "description: ")
		if len(lines) != 2 || lines[0] != "name: eva-md-digest" || description == "" || len(description) > 200 || strings.ContainsAny(description, ":<>#\"'") {
			t.Fatalf("%s skill frontmatter %q", host, front)
		}
		if len(body) > 2200 {
			t.Fatalf("%s skill body is %d bytes", host, len(body))
		}
		author := map[string]string{"claude": "claude-code", "codex": "codex"}[host]
		bin := shellQuote(binary)
		for _, want := range []string{bin + " show systems/map", bin + " show systems/<slug>", bin + " system write <slug> < <file>", "`--replace`", "## Unverified",
			"`## Decisions and constraints` names its source", "`[REDACTED:...]`, restore the text from its source", "at most 24 KB", "generated: { by: " + author + "/",
			"cite each source's URL, with its version when it has one",
			"\nNever copy sensitive documents (HR, legal, health, credentials) into a system page; cite them by link and title only.\n"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s skill lacks %q", host, want)
			}
		}
		if strings.Contains(skill, "{{") {
			t.Fatalf("%s skill has a placeholder left", host)
		}
	}
	var manifest struct{ Skills string }
	codex, _ := render("codex", binary)
	if err := json.Unmarshal(codex["plugins/eva-md/.codex-plugin/plugin.json"], &manifest); err != nil || manifest.Skills != "./skills/" {
		t.Fatalf("Codex manifest skills %q %v", manifest.Skills, err)
	}
	// Claude Code scans skills/ on its own; naming it as well would add the
	// folder a second time.
	claude, _ := render("claude", binary)
	if bytes.Contains(claude["plugins/eva-md/.claude-plugin/plugin.json"], []byte("skills")) {
		t.Fatal("the Claude manifest names the skills folder")
	}
}

// A machine installed before the skill: the dry run plans to render both
// marketplaces again and writes nothing, the install adds the skill, and
// uninstall removes it with the marketplaces.
func TestDigestSkillInstallAndUninstall(t *testing.T) {
	opts, f := setup(t)
	ctx := context.Background()
	result, err := runInstall(ctx, opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	skills := map[string][]byte{}
	for _, host := range []string{"claude", "codex"} {
		files, _ := render(host, result.Binary)
		file := filepath.Join(marketplacePath(opts.Dirs.Share, host), skillPath)
		if got, err := os.ReadFile(file); err != nil || !bytes.Equal(got, files[skillPath]) {
			t.Fatalf("%s skill not installed: %v", host, err)
		}
		skills[host] = files[skillPath]
		if err := os.RemoveAll(filepath.Join(pluginPath(opts.Dirs.Share, host), "skills")); err != nil {
			t.Fatal(err)
		}
	}
	f.calls = nil
	dry := opts
	dry.DryRun = true
	planned, err := runInstall(ctx, dry, f.run)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"claude", "codex"} {
		if !slices.Contains(planned.Actions, host+": render generated marketplace") {
			t.Fatalf("dry run actions %v", planned.Actions)
		}
		if _, err := os.Stat(filepath.Join(pluginPath(opts.Dirs.Share, host), "skills")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the dry run wrote the %s skill", host)
		}
	}
	if len(mutateCalls(f.calls)) != 0 {
		t.Fatalf("dry run mutated hosts: %v", f.calls)
	}
	again, err := runInstall(ctx, opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if !again.NeedsTrust {
		t.Fatal("a changed Codex plugin folder needs the hooks trusted again")
	}
	for host, want := range skills {
		if got, err := os.ReadFile(filepath.Join(marketplacePath(opts.Dirs.Share, host), skillPath)); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s skill not restored: %v", host, err)
		}
	}
	if _, err := uninstall(ctx, opts.Dirs, true, f.run); err != nil {
		t.Fatal(err)
	}
	for host := range skills {
		if _, err := os.Stat(marketplacePath(opts.Dirs.Share, host)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstall kept the %s marketplace", host)
		}
	}
}
