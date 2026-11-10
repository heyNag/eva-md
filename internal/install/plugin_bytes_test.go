package install

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// pluginDigest is the SHA-256 and name of every file, sorted by name, then
// one SHA-256 over those lines, as testdata/<host>-plugin.sha256 holds them.
func pluginDigest(files map[string][]byte) string {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(files)) {
		fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(files[name]), name)
	}
	return b.String() + fmt.Sprintf("%x  (all files)\n", sha256.Sum256([]byte(b.String())))
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
// of any one file changes the digest.
func TestPluginFolderBytes(t *testing.T) {
	cost := map[string]string{
		"codex": "Codex binds its approval of eva-md's hooks to a hash of every file in the plugin, " +
			"so this change makes every install that upgrades to it review the hooks again in Codex /hooks. " +
			"If that cost is meant, say so in the change and replace testdata/codex-plugin.sha256 with:\n",
		"claude": "Claude Code binds no approval to the plugin's bytes, so this costs no review. " +
			"If the change is meant, replace testdata/claude-plugin.sha256 with:\n",
	}
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
		golden, err := os.ReadFile(filepath.Join("testdata", host+"-plugin.sha256"))
		if err != nil {
			t.Fatal(err)
		}
		got := pluginDigest(disk)
		if got != string(golden) {
			t.Errorf("the %s plugin folder's bytes changed. %s%s", host, cost[host], got)
			continue
		}
		// One changed byte in any file, on a copy in memory, fails the
		// comparison above.
		for name, data := range disk {
			changed := maps.Clone(disk)
			flipped := append([]byte{}, data...)
			if len(flipped) == 0 {
				flipped = []byte{0}
			} else {
				flipped[len(flipped)/2] ^= 1
			}
			changed[name] = flipped
			if pluginDigest(changed) == string(golden) {
				t.Errorf("%s: a changed byte in %s matches the golden", host, name)
			}
		}
	}
}
