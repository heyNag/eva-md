package install

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// installedFixtures are the folders that hold what a published release left
// in a home, as SEAM-0d's fixture of 2026.11.5 does.
func installedFixtures(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("testdata", "installed-*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no installed-* fixture: %v", err)
	}
	return dirs
}

// fixtureAt reads a fixture as readFixture reads 2026.11.5's, with home in
// place of the token, by slash-separated path relative to home.
func fixtureAt(t *testing.T, dir, home string) map[string]node {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST"))
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
				t.Fatalf("%s/MANIFEST: %q", dir, lines.Text())
			}
			n := node{mode: fs.FileMode(perm)}
			if fields[0] == "d" {
				n.mode |= fs.ModeDir
			} else {
				data, err := os.ReadFile(filepath.Join(dir, "home", filepath.FromSlash(fields[2])))
				if err != nil {
					t.Fatal(err)
				}
				n.data = bytes.ReplaceAll(data, []byte(fixtureHomeToken), []byte(home))
			}
			entries[fields[2]] = n
		default:
			t.Fatalf("%s/MANIFEST: %q", dir, lines.Text())
		}
	}
	if len(entries) == 0 {
		t.Fatalf("%s is empty", dir)
	}
	return entries
}

// fixtureMarketplace is the files a fixture holds in each host's
// marketplace folder, by install name, relative to the folder.
func fixtureMarketplace(entries map[string]node) map[string][]string {
	files := map[string][]string{}
	for path, n := range entries {
		rest, ok := strings.CutPrefix(path, ".local/share/eva-md/marketplace/")
		if !ok || !n.mode.IsRegular() {
			continue
		}
		host, name, _ := strings.Cut(rest, "/")
		files[host] = append(files[host], name)
	}
	for host := range files {
		slices.Sort(files[host])
	}
	return files
}

// The paths v1 renders, which install adopts from a folder with no
// manifest, are the files each published release's fixture holds in each
// marketplace folder, and a list that differs from a fixture's fails.
func TestManifestV1PathsAreTheFixtures(t *testing.T) {
	v1 := map[string][]string{}
	for _, h := range adapter.All() {
		v1[h.Names.InstallName] = slices.Sorted(slices.Values(v1Paths[h.ID]))
		if len(v1Paths[h.ID]) != 8 {
			t.Errorf("%s: v1 renders %d paths, not 8", h.Names.InstallName, len(v1Paths[h.ID]))
		}
	}
	for _, dir := range installedFixtures(t) {
		got := fixtureMarketplace(fixtureAt(t, dir, "/made-up-home"))
		if !maps.EqualFunc(got, v1, slices.Equal) {
			t.Errorf("%s holds %v in its marketplace folders; v1's paths are %v", dir, got, v1)
		}
		// The comparison above fails on one path more, or one less.
		for host, list := range got {
			more := maps.Clone(got)
			more[host] = append(slices.Clone(list), "plugins/eva-md/extra.md")
			less := maps.Clone(got)
			less[host] = list[1:]
			if maps.EqualFunc(more, v1, slices.Equal) || maps.EqualFunc(less, v1, slices.Equal) {
				t.Errorf("%s: a path more or less in %s is not caught", dir, host)
			}
		}
	}
}

// Over each published release's fixture, with the switch on: install
// succeeds and the manifest lists what it rendered; a second run changes
// nothing; uninstall removes the plugin folders and the manifest. A
// release that no longer renders a path v1 rendered installs over it too,
// as v1's folder is adopted, and is refused with the switch off.
func TestManifestUpgradeFromEachFixture(t *testing.T) {
	for _, dir := range installedFixtures(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			home, dirs := newHome(t)
			layDown(t, home, fixtureAt(t, dir, home))
			setManifestSwitch(t, dirs, true)
			f := &fakeHost{dirs: dirs, installed: map[string]bool{"claude": true, "codex": true}, markets: map[string]bool{"claude": true, "codex": true}}
			opts := Options{Dirs: dirs, Binary: newBinary(t), Host: "all"}
			if _, err := upgrade(t, opts, f.run); err != nil {
				t.Fatalf("the install over %s: %v", dir, err)
			}
			wantFolders(t, dirs, render)
			wantManifestMatchesDisk(t, dirs, "claude", "codex")
			after := snapshot(t, home)
			f.calls = nil
			if _, err := upgrade(t, opts, f.run); err != nil {
				t.Fatal(err)
			}
			if moved := changed(after, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
				t.Errorf("a second install changed %v and ran %v", moved, mutateCalls(f.calls))
			}
			if _, err := uninstall(context.Background(), dirs, true, f.run); err != nil {
				t.Fatalf("uninstall: %v", err)
			}
			for _, path := range []string{marketplacePath(dirs.Share, "claude"), marketplacePath(dirs.Share, "codex"), manifestPath(dirs)} {
				if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("uninstall left %s: %v", path, err)
				}
			}
		})
		for _, on := range []bool{false, true} {
			t.Run(filepath.Base(dir)+" adopted, switch "+strconv.FormatBool(on), func(t *testing.T) {
				home, dirs := newHome(t)
				layDown(t, home, fixtureAt(t, dir, home))
				setManifestSwitch(t, dirs, on)
				f := &fakeHost{dirs: dirs, installed: map[string]bool{"claude": true, "codex": true}, markets: map[string]bool{"claude": true, "codex": true}}
				opts := Options{Dirs: dirs, Binary: newBinary(t), Host: "all"}
				// A release that renders none of v1's paths but the marker
				// and the Codex marketplace's manifest, without which Codex
				// fails its lists: each of them is adopted, and removed.
				later := dropping("plugins/", ".claude-plugin/")
				before := snapshot(t, home)
				_, err := runInstallWith(context.Background(), opts, f.run, later)
				if !on {
					if err == nil || !strings.Contains(err.Error(), "unexpected") {
						t.Fatalf("switch off: %v, want v1's refusal", err)
					}
					if moved := changed(before, snapshot(t, home)); len(moved) != 0 {
						t.Errorf("a refused install changed %v", moved)
					}
					return
				}
				if err != nil {
					t.Fatalf("a release without a v1 path, over %s: %v", dir, err)
				}
				wantFolders(t, dirs, later)
				wantManifestMatchesDisk(t, dirs, "claude", "codex")
				if _, err := uninstall(context.Background(), dirs, true, f.run); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// Uninstall adopts a v1 folder as install does: with the switch on and no
// manifest, a release that no longer renders a path v1 rendered uninstalls
// over each fixture, preview and apply, removing the plugin folders and
// writing no manifest; with the switch off it is refused as in v1.
func TestManifestUninstallAdoptsEachFixture(t *testing.T) {
	later := dropping("plugins/eva-md/.mcp.json")
	for _, dir := range installedFixtures(t) {
		for _, on := range []bool{false, true} {
			t.Run(filepath.Base(dir)+", switch "+strconv.FormatBool(on), func(t *testing.T) {
				home, dirs := newHome(t)
				layDown(t, home, fixtureAt(t, dir, home))
				setManifestSwitch(t, dirs, on)
				f := &fakeHost{dirs: dirs, installed: map[string]bool{"claude": true, "codex": true}, markets: map[string]bool{"claude": true, "codex": true}}
				before := snapshot(t, home)
				for _, apply := range []bool{false, true} {
					r, err := uninstallRendered(context.Background(), UninstallOptions{Dirs: dirs, Apply: apply}, f.run, later)
					if !on {
						if err == nil || !strings.Contains(err.Error(), "unexpected") {
							t.Fatalf("switch off, uninstall (apply %v) = %v, want v1's refusal", apply, err)
						}
						continue
					}
					if err != nil {
						t.Fatalf("uninstall (apply %v) over %s: %v", apply, dir, err)
					}
					if slices.ContainsFunc(r.Actions, func(a string) bool { return strings.Contains(a, "install manifest") }) {
						t.Errorf("uninstall names a manifest there is none of: %v", r.Actions)
					}
					if !apply {
						if moved := changed(before, snapshot(t, home)); len(moved) != 0 {
							t.Errorf("the preview changed %v", moved)
						}
					}
				}
				if !on {
					if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
						t.Errorf("a refusal changed %v and ran %v", moved, mutateCalls(f.calls))
					}
					return
				}
				for _, path := range []string{marketplacePath(dirs.Share, "claude"), marketplacePath(dirs.Share, "codex"), manifestPath(dirs)} {
					if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
						t.Errorf("uninstall left %s: %v", path, err)
					}
				}
			})
		}
	}
}

// A v1 folder holding a file v1 never rendered is not adopted: with the
// switch on and no manifest, install and uninstall still refuse it.
func TestManifestAdoptsOnlyV1Paths(t *testing.T) {
	home, dirs := newHome(t)
	layDown(t, home, fixtureAt(t, installedFixtures(t)[0], home))
	setManifestSwitch(t, dirs, true)
	stray := filepath.Join(pluginPath(dirs.Share, "codex"), "skills", "eva-md-other", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(stray), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte("not v1's\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeHost{dirs: dirs, installed: map[string]bool{"claude": true, "codex": true}, markets: map[string]bool{"claude": true, "codex": true}}
	before := snapshot(t, home)
	if _, err := runInstall(context.Background(), Options{Dirs: dirs, Binary: newBinary(t), Host: "all"}, f.run); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("install = %v", err)
	}
	if _, err := uninstall(context.Background(), dirs, true, f.run); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("uninstall = %v", err)
	}
	if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
		t.Errorf("a refusal changed %v and ran %v", moved, mutateCalls(f.calls))
	}
}
