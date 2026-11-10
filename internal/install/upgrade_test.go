package install

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/paths"
)

// fixture2026115 is what release 2026.11.5 left in a home when its install
// ran there: MANIFEST lists every folder, file and link with its mode, and
// home holds the files, with the home's path written as fixtureHomeToken.
// Its README says how it was made.
const fixture2026115 = "testdata/installed-2026.11.5"

const fixtureHomeToken = "@HOME@"

// node is one entry of a home: a folder, a file with its bytes, or a link
// with its target.
type node struct {
	mode fs.FileMode
	link string
	data []byte
}

func (n node) equal(o node) bool {
	return n.mode == o.mode && n.link == o.link && bytes.Equal(n.data, o.data)
}

// readFixture reads the 2026.11.5 fixture, with home in place of the token,
// by slash-separated path relative to home.
func readFixture(t *testing.T, home string) map[string]node {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(fixture2026115, "MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]node{}
	lines := bufio.NewScanner(bytes.NewReader(manifest))
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		switch {
		case len(fields) == 3 && fields[0] == "l":
			entries[fields[1]] = node{mode: fs.ModeSymlink, link: strings.ReplaceAll(fields[2], fixtureHomeToken, home)}
		case len(fields) == 3 && (fields[0] == "d" || fields[0] == "f"):
			perm, err := strconv.ParseUint(fields[1], 8, 32)
			if err != nil {
				t.Fatalf("MANIFEST: %q", lines.Text())
			}
			n := node{mode: fs.FileMode(perm)}
			if fields[0] == "d" {
				n.mode |= fs.ModeDir
			} else {
				data, err := os.ReadFile(filepath.Join(fixture2026115, "home", filepath.FromSlash(fields[2])))
				if err != nil {
					t.Fatal(err)
				}
				n.data = bytes.ReplaceAll(data, []byte(fixtureHomeToken), []byte(home))
			}
			entries[fields[2]] = n
		default:
			t.Fatalf("MANIFEST: %q", lines.Text())
		}
	}
	if len(entries) == 0 {
		t.Fatal("the fixture is empty")
	}
	return entries
}

// layDown makes entries in home, parents before children.
func layDown(t *testing.T, home string, entries map[string]node) {
	t.Helper()
	for _, rel := range slices.Sorted(maps.Keys(entries)) {
		n, path := entries[rel], filepath.Join(home, filepath.FromSlash(rel))
		var err error
		switch {
		case n.mode&fs.ModeSymlink != 0:
			err = os.Symlink(n.link, path)
		case n.mode.IsDir():
			if err = os.Mkdir(path, 0o700); err == nil {
				err = os.Chmod(path, n.mode.Perm())
			}
		default:
			if err = os.WriteFile(path, n.data, 0o600); err == nil {
				err = os.Chmod(path, n.mode.Perm())
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// snapshot is every folder, file and link under home, by slash-separated
// path relative to it.
func snapshot(t *testing.T, home string) map[string]node {
	t.Helper()
	entries := map[string]node{}
	err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == home {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(home, path)
		n := node{mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			n.mode = fs.ModeSymlink
			n.link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			n.data, err = os.ReadFile(path)
		}
		entries[filepath.ToSlash(rel)] = n
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// changed lists the paths whose entry before and after differ, or that only
// one of them has.
func changed(before, after map[string]node) []string {
	var paths []string
	for path, n := range after {
		if o, ok := before[path]; !ok || !o.equal(n) {
			paths = append(paths, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

// notOwned lists the paths owned doesn't have.
func notOwned(paths []string, owned map[string]bool) []string {
	var outside []string
	for _, path := range paths {
		if !owned[path] {
			outside = append(outside, path)
		}
	}
	return outside
}

// leftBehind lists the plugin files 2026.11.5 wrote that are still there
// and are not what the current install renders: each must be replaced by
// the current file or removed.
func leftBehind(old, after map[string]node, rendered map[string][]byte) []string {
	var left []string
	for path, n := range old {
		if !strings.Contains(path, "/marketplace/") || n.mode&fs.ModeType != 0 {
			continue
		}
		now, ok := after[path]
		if !ok {
			continue
		}
		want, current := rendered[path]
		if !current || now.mode&fs.ModeType != 0 || !bytes.Equal(now.data, want) {
			left = append(left, path)
		}
	}
	slices.Sort(left)
	return left
}

// newHome makes a home for a test install: HOME, the git config, the host
// CLIs that PATH finds (the fake runner answers in their place), and
// ~/.local/bin on PATH, as the install script and the 2026.11.5 fixture had
// it. The home is named by its real path, as a link would resolve it.
func newHome(t *testing.T) (string, paths.Dirs) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	bin := filepath.Join(home, "fake-bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(filepath.ListSeparator)+filepath.Join(home, ".local", "bin"))
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	return home, dirs
}

// renderedFiles is what the current install renders for both hosts, by path
// relative to home.
func renderedFiles(t *testing.T, home string, dirs paths.Dirs) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	binary := filepath.Join(dirs.Share, "bin", "eva-md")
	for _, host := range []string{"claude", "codex"} {
		r, err := render(host, binary)
		if err != nil {
			t.Fatal(err)
		}
		base, _ := filepath.Rel(home, marketplacePath(dirs.Share, host))
		for name, data := range r {
			files[filepath.ToSlash(filepath.Join(base, name))] = data
		}
	}
	return files
}

// newBinary is the binary the current install installs, outside the home.
func newBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "eva-md")
	if err := os.WriteFile(binary, []byte("binary of the current build"), 0o700); err != nil {
		t.Fatal(err)
	}
	return binary
}

// currentInstallWrites is every path a first install of the current build
// makes in a home that holds only setup's config and ~/.local/bin, relative
// to that home.
func currentInstallWrites(t *testing.T) map[string]bool {
	t.Helper()
	home, dirs := newHome(t)
	setupOnly := map[string]node{}
	for path, n := range readFixture(t, home) {
		if path == ".local" || path == ".local/bin" || strings.HasPrefix(path, ".config") {
			setupOnly[path] = n
		}
	}
	layDown(t, home, setupOnly)
	before := snapshot(t, home)
	f := &fakeHost{dirs: dirs, installed: map[string]bool{}, markets: map[string]bool{}}
	if _, err := runInstall(context.Background(), Options{Dirs: dirs, Binary: newBinary(t), Host: "all"}, f.run); err != nil {
		t.Fatal(err)
	}
	writes := map[string]bool{}
	for _, path := range changed(before, snapshot(t, home)) {
		writes[path] = true
	}
	return writes
}

// The one real install runs release 2026.11.5. The current install over
// the files it left, in a home that also holds files neither install owns,
// succeeds; every plugin file 2026.11.5 wrote is now the current one or
// gone, and the folders hold exactly what the current build renders; the
// install record names the current install's items and keeps the old ones;
// nothing either install doesn't own is created, changed or removed; and a
// second run changes nothing. A plugin file left behind, or a change outside
// what the installs own, is caught.
func TestUpgradeFrom2026_11_5(t *testing.T) {
	ownedByCurrent := currentInstallWrites(t)
	home, dirs := newHome(t)
	old := readFixture(t, home)
	layDown(t, home, old)
	// Made-up files that no install owns: the host's own settings, the
	// wiki, a captured chat and another tool on PATH.
	others := map[string]node{
		".claude":               {mode: fs.ModeDir | 0o700},
		".claude/settings.json": {mode: 0o600, data: []byte("{\"theme\": \"made-up\"}\n")},
		"eva-wiki":              {mode: fs.ModeDir | 0o700},
		"eva-wiki/index.md":     {mode: 0o600, data: []byte("# Made-up wiki\n")},
		".local/state/eva-md/inbox/made-up-project":            {mode: fs.ModeDir | 0o700},
		".local/state/eva-md/inbox/made-up-project/chat.jsonl": {mode: 0o600, data: []byte("{\"text\":\"made-up\"}\n")},
		".local/bin/other-tool":                                {mode: 0o700, data: []byte("#!/bin/sh\n")},
	}
	layDown(t, home, others)
	// What the two installs own: what a first install of the current build
	// writes, and the plugin folders 2026.11.5 wrote. The rest of what
	// 2026.11.5 left, such as the inbox's baseline, stays as it was.
	owned := maps.Clone(ownedByCurrent)
	for path := range old {
		if strings.Contains(path, "/marketplace/") {
			owned[path] = true
		}
	}
	rendered := renderedFiles(t, home, dirs)
	codexChanged := false
	for path, n := range old {
		if want, ok := rendered[path]; n.mode.IsRegular() && ok && !bytes.Equal(n.data, want) {
			t.Logf("2026.11.5 wrote another %s", path)
			codexChanged = codexChanged || strings.Contains(path, "/marketplace/codex/")
		}
	}
	if !codexChanged {
		t.Fatal("2026.11.5's Codex plugin is the current one, so this test replaces no Codex file")
	}

	before := snapshot(t, home)
	f := &fakeHost{dirs: dirs, installed: map[string]bool{"claude": true, "codex": true}, markets: map[string]bool{"claude": true, "codex": true}}
	opts := Options{Dirs: dirs, Binary: newBinary(t), Host: "all"}
	result, err := runInstall(context.Background(), opts, f.run)
	if err != nil {
		t.Fatalf("the install over 2026.11.5 failed: %v", err)
	}
	if !result.NeedsTrust {
		t.Error("a changed Codex plugin needs its hooks reviewed again")
	}
	after := snapshot(t, home)

	if left := leftBehind(old, after, rendered); len(left) != 0 {
		t.Errorf("plugin files 2026.11.5 wrote were left behind: %v", left)
	}
	for _, host := range []string{"claude", "codex"} {
		dir := marketplacePath(dirs.Share, host)
		base, _ := filepath.Rel(home, dir)
		want := map[string][]byte{}
		for path, data := range rendered {
			if name, ok := strings.CutPrefix(path, filepath.ToSlash(base)+"/"); ok {
				want[name] = data
			}
		}
		if got := readFolder(t, dir); !maps.EqualFunc(got, want, bytes.Equal) {
			t.Errorf("the %s marketplace is not what the current build renders", host)
		}
	}
	if outside := notOwned(changed(before, after), owned); len(outside) != 0 {
		t.Errorf("the install changed what neither install owns: %v", outside)
	}

	record, ok := ReadRecord(dirs)
	if !ok {
		t.Fatal("no install record after the upgrade")
	}
	binary := filepath.Join(dirs.Share, "bin", "eva-md")
	wants := append(installedItems(dirs, binary, []string{"claude", "codex"}), RecordItem{Kind: KindPathLink, Path: LinkPath(dirs), Value: binary})
	for _, want := range wants {
		if !slices.Contains(record.Items, want) {
			t.Errorf("the install record lacks %+v", want)
		}
	}
	var oldRecord Record
	if err := json.Unmarshal(old[".local/share/eva-md/"+RecordFile].data, &oldRecord); err != nil || len(oldRecord.Items) == 0 {
		t.Fatalf("the fixture's install record: %v", err)
	}
	for _, item := range oldRecord.Items {
		if !record.has(item) {
			t.Errorf("the install record lost %+v", item)
		}
	}
	keys := map[string]bool{}
	for _, item := range record.Items {
		if keys[item.key()] {
			t.Errorf("the install record names %+v twice", item)
		}
		keys[item.key()] = true
		// Folders are named whether they exist or not, as for logs and the
		// cache before their first use; the files it names must exist.
		if !slices.Contains([]string{KindBinary, KindMarketplace, KindPathLink, KindConfig}, item.Kind) {
			continue
		}
		if _, err := os.Lstat(item.Path); err != nil {
			t.Errorf("the install record names %s, which is missing", item.Path)
		}
	}

	f.calls = nil
	again, err := runInstall(context.Background(), opts, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if moved := changed(after, snapshot(t, home)); len(moved) != 0 || again.NeedsTrust || len(mutateCalls(f.calls)) != 0 {
		t.Errorf("a second install changed %v, needs trust %v, ran %v", moved, again.NeedsTrust, mutateCalls(f.calls))
	}

	// The checks above catch a plugin file 2026.11.5 wrote that stays as it
	// was, one the current build no longer renders, and a change outside
	// what the installs own.
	var differs string
	for _, path := range slices.Sorted(maps.Keys(old)) {
		if want, ok := rendered[path]; ok && old[path].mode.IsRegular() && !bytes.Equal(old[path].data, want) {
			differs = path
			break
		}
	}
	stale := maps.Clone(after)
	stale[differs] = old[differs]
	gone := pathpkg.Join(pathpkg.Dir(differs), "left-by-2026.11.5.md")
	oldWithGone := maps.Clone(old)
	oldWithGone[gone] = node{mode: 0o600, data: []byte("a file 2026.11.5 might have written\n")}
	stale[gone] = oldWithGone[gone]
	if left := leftBehind(oldWithGone, stale, rendered); !slices.Equal(left, slices.Sorted(slices.Values([]string{differs, gone}))) {
		t.Errorf("seeded leftovers %s and %s were reported as %v", differs, gone, left)
	}
	stray := maps.Clone(after)
	stray[".claude/settings.json"] = node{mode: 0o600, data: []byte("{}\n")}
	delete(stray, ".local/state/eva-md/inbox/.baseline")
	want := []string{".claude/settings.json", ".local/state/eva-md/inbox/.baseline"}
	if outside := notOwned(changed(after, stray), owned); !slices.Equal(outside, want) {
		t.Errorf("seeded changes outside the installs, %v, were reported as %v", want, outside)
	}
}

// The fixture holds no path of the machine that made it: every absolute
// path in it starts at the home token.
func TestFixture2026_11_5HoldsNoRealPath(t *testing.T) {
	real := regexp.MustCompile(`(^|[^A-Za-z0-9@_.-])/(Users|home|root|tmp|private|var|opt)/`)
	err := filepath.WalkDir(fixture2026115, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil && real.Match(data) {
			t.Errorf("%s holds a real path", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
