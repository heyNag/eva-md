package drive

import (
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// homeFiles lists every path under home, folders too, slash paths.
func homeFiles(t *testing.T, home string) []string {
	t.Helper()
	var got []string
	err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// PluginReady passes, on each host, once the plugin is where setup puts
// it in the home and in a shape the host table loads, fails before, and
// writes nothing either way: no requirements file for Codex, whose entry
// gives one for the chat to write.
func TestPluginReady(t *testing.T) {
	codexHooks := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"eva-md hook --host codex","timeout":5}]}]}}`
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			home := t.TempDir()
			if err := PluginReady(home, h); err == nil {
				t.Fatal("an empty home is ready")
			}
			dir := pluginDir(home, h)
			mkdir(t, dir)
			if h.ID == adapter.Codex {
				// A folder with no hooks file, then one that declares none.
				if err := PluginReady(home, h); err == nil {
					t.Fatal("a plugin with no hooks file is ready")
				}
				write(t, filepath.Join(dir, "hooks", "hooks.json"), `{"hooks":{}}`)
				if err := PluginReady(home, h); err == nil {
					t.Fatal("a plugin that declares no hooks is ready")
				}
				write(t, filepath.Join(dir, "hooks", "hooks.json"), codexHooks)
			}
			before := homeFiles(t, home)
			if err := PluginReady(home, h); err != nil {
				t.Fatalf("the installed plugin: %v", err)
			}
			if after := homeFiles(t, home); !slices.Equal(before, after) {
				t.Fatalf("PluginReady wrote into the home: %v, then %v", before, after)
			}
			// The plugin under another host's install name is not this one's.
			for _, other := range adapter.All() {
				if other.ID == h.ID {
					continue
				}
				elsewhere := t.TempDir()
				mkdir(t, pluginDir(elsewhere, other))
				write(t, filepath.Join(pluginDir(elsewhere, other), "hooks", "hooks.json"), codexHooks)
				if err := PluginReady(elsewhere, h); err == nil {
					t.Fatalf("%s's plugin is ready for %s", other.ID, h.ID)
				}
			}
		})
	}
	if err := PluginReady(t.TempDir(), adapter.Host{ID: "made-up"}); err == nil {
		t.Fatal("a host with no entry is ready")
	}
}
