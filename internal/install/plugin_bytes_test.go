package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pluginTestBinary is the made-up path of the stable binary the plugin
// folders are rendered with, so their bytes don't depend on the machine.
const pluginTestBinary = "/Users/eva/.local/share/eva-md/bin/eva-md"

// updatePluginGoldens writes testdata/<host>-plugin.sha256 from the folders
// the current build renders, for a change to the plugin's bytes made on
// purpose: go test ./internal/install -run TestPluginFolderBytes -update.
var updatePluginGoldens = flag.Bool("update", false, "write testdata/<host>-plugin.sha256 from what the current build renders")

// codexPluginFolder is the folder Codex installs the plugin from, inside the
// marketplace, as the marketplace's manifest names it. Codex binds its
// approval of the plugin's hooks to a hash of every file in it; the
// marketplace's manifest and eva-md's marker sit outside it.
const codexPluginFolder = "plugins/eva-md/"

// pluginDigest is the SHA-256 and name of every file, sorted by name, then
// one SHA-256 over those lines, as testdata/<host>-plugin.sha256 holds them.
func pluginDigest(files map[string][]byte) string {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(files)) {
		fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(files[name]), name)
	}
	return b.String() + fmt.Sprintf("%x  (all files)\n", sha256.Sum256([]byte(b.String())))
}

// digestFiles reads a digest's per-file lines back: each file's SHA-256 by
// name, without the line that hashes them all.
func digestFiles(digest string) map[string]string {
	files := map[string]string{}
	for line := range strings.Lines(digest) {
		sum, name, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "  ")
		if ok && name != "(all files)" {
			files[name] = sum
		}
	}
	return files
}

// digestChanges lists the files that one digest has and the other lacks, or
// that both have with different bytes, sorted.
func digestChanges(golden, got string) []string {
	was, now := digestFiles(golden), digestFiles(got)
	var names []string
	for name, sum := range now {
		if was[name] != sum {
			names = append(names, name)
		}
	}
	for name := range was {
		if _, ok := now[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// pluginChangeCost says, in one sentence, what the changed files cost the
// installs that upgrade to them, then how to update the golden on purpose.
func pluginChangeCost(host string, changed []string) string {
	var cost string
	switch {
	case host == "claude":
		cost = "Claude Code binds no approval to the plugin's bytes, so this change costs no review."
	case slices.ContainsFunc(changed, func(name string) bool { return strings.HasPrefix(name, codexPluginFolder) }):
		cost = "Codex binds its approval of eva-md's hooks to a hash of every file in the plugin folder, " + codexPluginFolder +
			", so this change makes every install that upgrades to it review the hooks again in Codex /hooks; if that cost is meant, say so in the change."
	default:
		cost = "Only files outside the plugin folder Codex hashes, " + codexPluginFolder +
			", changed, so this change should cost no new hook review, but Codex reads the marketplace's manifest: check the change is meant."
	}
	return fmt.Sprintf("The %s plugin folder's bytes changed in %s. %s To accept the change, run "+
		"go test ./internal/install -run TestPluginFolderBytes -update, which writes testdata/%s-plugin.sha256, and commit it.",
		host, strings.Join(changed, ", "), cost, host)
}

// readFolder reads every file under dir by its slash-separated name, and
// fails on anything that is neither a file nor a folder.
func readFolder(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Every byte of each generated marketplace folder, skills and icon
// included, is pinned to what main renders: rendered with a fixed binary
// path and written by install's own folder swap, the folder's files and one
// hash over all of them match the host's golden, and changing any one byte
// of any one file changes the digest and is reported in that file alone.
func TestPluginFolderBytes(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		files, err := render(host, pluginTestBinary)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		swap, err := swapFolder(context.Background(), root, "marketplace", files)
		if err == nil {
			err = swap.finish()
		}
		root.Close()
		if err != nil {
			t.Fatal(err)
		}
		disk := readFolder(t, filepath.Join(dir, "marketplace"))
		if !maps.EqualFunc(disk, files, bytes.Equal) {
			t.Fatalf("%s: the folder written differs from the files rendered", host)
		}
		goldenPath := filepath.Join("testdata", host+"-plugin.sha256")
		got := pluginDigest(disk)
		if *updatePluginGoldens {
			if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		golden, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(golden) {
			t.Error(pluginChangeCost(host, digestChanges(string(golden), got)))
			continue
		}
		// One changed byte in any file, on a copy in memory, fails the
		// comparison above, and the failure names that file alone.
		for name, data := range disk {
			changed := maps.Clone(disk)
			flipped := append([]byte{}, data...)
			if len(flipped) == 0 {
				flipped = []byte{0}
			} else {
				flipped[len(flipped)/2] ^= 1
			}
			changed[name] = flipped
			digest := pluginDigest(changed)
			if digest == string(golden) {
				t.Errorf("%s: a changed byte in %s matches the golden", host, name)
			}
			if names := digestChanges(string(golden), digest); !slices.Equal(names, []string{name}) {
				t.Errorf("%s: a changed byte in %s was reported in %v", host, name, names)
			}
		}
	}
}

// The failure message puts the cost of a review on a change to the Codex
// plugin folder only: a change to the marketplace's manifest or marker
// alone says it should cost none, and Claude Code's never costs one.
// Either way it names the changed files and says how to write the golden
// on purpose, with a command rather than text to paste.
func TestPluginChangeCost(t *testing.T) {
	review := "review the hooks again in Codex /hooks"
	for _, c := range []struct {
		host    string
		changed []string
		review  bool
	}{
		{"codex", []string{"plugins/eva-md/hooks/hooks.json"}, true},
		{"codex", []string{".agents/plugins/marketplace.json", "plugins/eva-md/skills/new/SKILL.md"}, true},
		{"codex", []string{".agents/plugins/marketplace.json"}, false},
		{"codex", []string{".eva-md-managed"}, false},
		{"claude", []string{"plugins/eva-md/hooks/hooks.json"}, false},
	} {
		msg := pluginChangeCost(c.host, c.changed)
		if strings.Contains(msg, review) != c.review {
			t.Errorf("%s %v: %q", c.host, c.changed, msg)
		}
		if !strings.Contains(msg, "-run TestPluginFolderBytes -update") || !strings.Contains(msg, strings.Join(c.changed, ", ")) {
			t.Errorf("%s %v: %q doesn't name the files or say how to update the golden", c.host, c.changed, msg)
		}
	}
	// A digest read back names what was added, removed and changed.
	was := pluginDigest(map[string][]byte{"a": []byte("1"), "b": []byte("2"), "c": []byte("3")})
	now := pluginDigest(map[string][]byte{"a": []byte("1"), "b": []byte("x"), "d": []byte("4")})
	if names := digestChanges(was, now); !slices.Equal(names, []string{"b", "c", "d"}) {
		t.Errorf("changes read back as %v", names)
	}
}
