package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"unicode"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// setManifestSwitch turns install_manifest on or off in the config.
func setManifestSwitch(t *testing.T, dirs paths.Dirs, on bool) {
	t.Helper()
	c, err := config.Load(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if c.Switches == nil {
		c.Switches = map[string]any{}
	}
	c.Switches["install_manifest"] = on
	if err := config.Save(dirs.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	if c, err = config.Load(dirs.ConfigFile()); err != nil || ManifestSwitch.On(c) != on {
		t.Fatalf("the switch did not read as %v: %v", on, err)
	}
}

func manifestPath(dirs paths.Dirs) string { return filepath.Join(dirs.Share, manifestFile) }

// diskManifest is the manifest on disk as plain JSON values, or nil.
type diskManifest struct {
	Format int        `json:"format"`
	Units  []diskUnit `json:"units"`
}

type diskUnit struct {
	ID         string     `json:"id"`
	Host       string     `json:"host"`
	Plugin     string     `json:"plugin"`
	Root       string     `json:"root"`
	Generation *int       `json:"generation"`
	Files      []diskFile `json:"files"`
}

type diskFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func readDiskManifest(t *testing.T, dirs paths.Dirs) (*diskManifest, []byte) {
	t.Helper()
	data, err := os.ReadFile(manifestPath(dirs))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var m diskManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("the manifest is not JSON: %v", err)
	}
	return &m, data
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// wantManifestMatchesDisk checks that the manifest's unit of each host lists
// exactly the files in its folder, with their hashes and sizes, and is
// written as card HO-11 gives it: 0600, format 1, the units in the
// adapter's order, generation 0 on the hooks unit only, files sorted.
func wantManifestMatchesDisk(t *testing.T, dirs paths.Dirs, hosts ...string) {
	t.Helper()
	m, data := readDiskManifest(t, dirs)
	if m == nil {
		t.Fatal("no install manifest")
	}
	if info, err := os.Lstat(manifestPath(dirs)); err != nil || info.Mode() != 0o600 {
		t.Errorf("the manifest's mode: %v %v", info.Mode(), err)
	}
	if m.Format != 1 {
		t.Errorf("format %d", m.Format)
	}
	wantNoRealPath(t, "the written manifest", data)
	order := map[string]int{"claude-code/plugin": 0, "codex/hooks": 1}
	last := -1
	for _, u := range m.Units {
		if i, ok := order[u.ID]; ok {
			if i < last {
				t.Errorf("units out of the adapter's order: %s", data)
			}
			last = i
		}
	}
	for _, host := range hosts {
		id, hostID := "claude-code/plugin", "claude-code"
		if host == "codex" {
			id, hostID = "codex/hooks", "codex"
		}
		i := slices.IndexFunc(m.Units, func(u diskUnit) bool { return u.ID == id })
		if i < 0 {
			t.Errorf("the manifest has no unit %s", id)
			continue
		}
		u := m.Units[i]
		if u.Host != hostID || u.Plugin != "eva-md@eva-md" || u.Root != "marketplace/"+host {
			t.Errorf("unit %s: host %q, plugin %q, root %q", id, u.Host, u.Plugin, u.Root)
		}
		if (host == "codex") != (u.Generation != nil) || u.Generation != nil && *u.Generation != 0 {
			t.Errorf("unit %s: generation %v", id, u.Generation)
		}
		disk := readFolder(t, marketplacePath(dirs.Share, host))
		listed := map[string]bool{}
		var names []string
		for _, f := range u.Files {
			names = append(names, f.Path)
			listed[f.Path] = true
			data, ok := disk[f.Path]
			if !ok {
				t.Errorf("unit %s lists %s, which is not in its folder", id, f.Path)
				continue
			}
			if f.SHA256 != sum(data) || f.Size != int64(len(data)) {
				t.Errorf("unit %s lists %s as %s, %d bytes; its folder has %s, %d", id, f.Path, f.SHA256, f.Size, sum(data), len(data))
			}
		}
		if !slices.IsSorted(names) {
			t.Errorf("unit %s lists its files unsorted: %v", id, names)
		}
		for name := range disk {
			if !listed[name] {
				t.Errorf("unit %s does not list %s, which is in its folder", id, name)
			}
		}
	}
}

// realPathRE is TestFixture2026_11_5HoldsNoRealPath's check.
var realPathRE = regexp.MustCompile(`(^|[^A-Za-z0-9@_.-])/(Users|home|root|tmp|private|var|opt)/`)

func wantNoRealPath(t *testing.T, what string, data []byte) {
	t.Helper()
	if realPathRE.Match(data) {
		t.Errorf("%s holds a real path: %s", what, data)
	}
}

// Releases made up from what the current build renders.

func dropping(names ...string) renderer {
	return func(host, binary string) (map[string][]byte, error) {
		files, err := render(host, binary)
		for name := range files {
			for _, drop := range names {
				if name == drop || strings.HasSuffix(drop, "/") && strings.HasPrefix(name, drop) {
					delete(files, name)
				}
			}
		}
		return files, err
	}
}

func renaming(from, to string) renderer {
	return func(host, binary string) (map[string][]byte, error) {
		files, err := render(host, binary)
		if data, ok := files[from]; ok {
			delete(files, from)
			files[to] = data
		}
		return files, err
	}
}

func adding(name, data string) renderer {
	return func(host, binary string) (map[string][]byte, error) {
		files, err := render(host, binary)
		if files != nil {
			files[name] = []byte(data + " for " + host + "\n")
		}
		return files, err
	}
}

// folders reads each host's marketplace folder.
func folders(t *testing.T, dirs paths.Dirs) map[string]map[string][]byte {
	t.Helper()
	all := map[string]map[string][]byte{}
	for _, host := range []string{"claude", "codex"} {
		all[host] = readFolder(t, marketplacePath(dirs.Share, host))
	}
	return all
}

func wantFolders(t *testing.T, dirs paths.Dirs, release renderer) {
	t.Helper()
	binary := filepath.Join(dirs.Share, "bin", "eva-md")
	for host, got := range folders(t, dirs) {
		want, err := release(host, binary)
		if err != nil {
			t.Fatal(err)
		}
		if !maps.EqualFunc(got, want, bytes.Equal) {
			t.Errorf("the %s folder holds %v, not what the release renders, %v", host, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
		}
	}
}

// layoutChanges are two releases each, the second installed over the first.
var layoutChanges = []struct {
	name          string
	first, second renderer
	removed       []string
}{
	{"a file removed", render, dropping("plugins/eva-md/.mcp.json"), []string{"plugins/eva-md/.mcp.json"}},
	{"a file renamed", render, renaming("plugins/eva-md/skills/eva-md-recall/SKILL.md", "plugins/eva-md/skills/eva-md-recall/RECALL.md"),
		[]string{"plugins/eva-md/skills/eva-md-recall/SKILL.md"}},
	{"a folder removed", render, dropping("plugins/eva-md/skills/"),
		[]string{"plugins/eva-md/skills/eva-md-digest/SKILL.md", "plugins/eva-md/skills/eva-md-recall/SKILL.md"}},
	{"a file added, then a rollback to the release without it", adding("plugins/eva-md/skills/eva-md-later/SKILL.md", "a later skill"), render,
		[]string{"plugins/eva-md/skills/eva-md-later/SKILL.md"}},
}

// Each layout change, with the switch off, is refused as v1 refuses it,
// changing nothing; with it on, it installs: a dry run names each file it
// would remove, the folders then hold exactly what the second release
// renders, and the manifest's paths, hashes and sizes match the disk.
func TestManifestLayoutChanges(t *testing.T) {
	for _, on := range []bool{false, true} {
		clean := 0
		for _, c := range layoutChanges {
			t.Run(c.name+map[bool]string{false: ", switch off", true: ", switch on"}[on], func(t *testing.T) {
				opts, f := setup(t)
				setManifestSwitch(t, opts.Dirs, on)
				if _, err := runInstallWith(context.Background(), opts, f.run, c.first); err != nil {
					t.Fatalf("the first release: %v", err)
				}
				if m, _ := readDiskManifest(t, opts.Dirs); (m != nil) != on {
					t.Fatalf("a manifest after the first release: %v", m != nil)
				}
				home := filepath.Dir(filepath.Dir(filepath.Dir(opts.Dirs.Share)))
				before := snapshot(t, home)

				dry := opts
				dry.DryRun = true
				result, err := runInstallWith(context.Background(), dry, f.run, c.second)
				if !on {
					if err == nil || !strings.Contains(err.Error(), "unexpected") {
						t.Fatalf("switch off, the dry run = %v", err)
					}
				} else {
					if err != nil {
						t.Fatalf("the dry run: %v", err)
					}
					var want []string
					for _, host := range []string{"claude", "codex"} {
						for _, name := range c.removed {
							want = append(want, host+": remove "+filepath.Join(marketplacePath(opts.Dirs.Share, host), filepath.FromSlash(name))+", which this release does not render")
						}
					}
					var got []string
					for _, a := range result.Actions {
						if strings.Contains(a, ": remove ") {
							got = append(got, a)
						}
					}
					if !slices.Equal(got, want) {
						t.Errorf("the dry run's removals:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
					}
				}
				if moved := changed(before, snapshot(t, home)); len(moved) != 0 {
					t.Fatalf("the dry run changed %v", moved)
				}

				f.calls = nil
				_, err = runInstallWith(context.Background(), opts, f.run, c.second)
				if !on {
					if err == nil || !strings.Contains(err.Error(), "unexpected") || !strings.Contains(err.Error(), "move it out of that folder") {
						t.Fatalf("switch off, the second release = %v, want v1's refusal", err)
					}
					if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
						t.Fatalf("a refused install changed %v and ran %v", moved, mutateCalls(f.calls))
					}
					if _, err := os.Lstat(manifestPath(opts.Dirs)); !errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("switch off, a manifest was written: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("the second release: %v", err)
				}
				wantFolders(t, opts.Dirs, c.second)
				wantManifestMatchesDisk(t, opts.Dirs, "claude", "codex")
				clean++
				// Both releases install again, either way round.
				if _, err := runInstallWith(context.Background(), opts, f.run, c.first); err != nil {
					t.Fatalf("the first release again: %v", err)
				}
				wantFolders(t, opts.Dirs, c.first)
				wantManifestMatchesDisk(t, opts.Dirs, "claude", "codex")
			})
		}
		t.Logf("switch %v: layout changes that install cleanly: %d of %d", map[bool]string{false: "off", true: "on"}[on], clean, len(layoutChanges))
		if want := map[bool]int{false: 0, true: len(layoutChanges)}[on]; clean != want {
			t.Errorf("switch on %v: %d of %d install cleanly, want %d", on, clean, len(layoutChanges), want)
		}
	}
}

// With the switch off and no manifest, install, a failed install's
// rollback, uninstall and its preview write no manifest.
func TestManifestSwitchOffWritesNone(t *testing.T) {
	opts, f := setup(t)
	none := func(when string) {
		t.Helper()
		if _, err := os.Lstat(manifestPath(opts.Dirs)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s: a manifest is there: %v", when, err)
		}
	}
	f.fail = "codex plugin add "
	if _, err := runInstall(context.Background(), opts, f.run); err == nil {
		t.Fatal("the install did not fail")
	}
	none("a failed install")
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	none("an install")
	if err := os.WriteFile(opts.Binary, []byte("binary version 2"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	none("an upgrade")
	if _, err := uninstall(context.Background(), opts.Dirs, false, f.run); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstall(context.Background(), opts.Dirs, true, f.run); err != nil {
		t.Fatal(err)
	}
	none("an uninstall")
}

// A manifest on disk is read and kept current with the switch off, so a
// release that writes none can still install over a later one's layout.
func TestManifestIsReadWithTheSwitchOff(t *testing.T) {
	opts, f := setup(t)
	setManifestSwitch(t, opts.Dirs, true)
	later := adding("plugins/eva-md/later.md", "later")
	if _, err := runInstallWith(context.Background(), opts, f.run, later); err != nil {
		t.Fatal(err)
	}
	setManifestSwitch(t, opts.Dirs, false)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatalf("the rollback with the switch off: %v", err)
	}
	wantFolders(t, opts.Dirs, render)
	wantManifestMatchesDisk(t, opts.Dirs, "claude", "codex")
	// Preflight loads no config, so with no manifest it reads the switch
	// as off: v1's paths are not adopted, even with it on.
	if err := os.Remove(manifestPath(opts.Dirs)); err != nil {
		t.Fatal(err)
	}
	setManifestSwitch(t, opts.Dirs, true)
	without := dropping("plugins/eva-md/skills/eva-md-recall/SKILL.md")
	dry := opts
	dry.DryRun = true
	if _, err := runInstallWith(context.Background(), dry, f.run, without); err != nil {
		t.Fatalf("a dry run adopts v1's paths with the switch on: %v", err)
	}
	dry.Preflight = true
	if _, err := runInstallWith(context.Background(), dry, f.run, without); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("a preflight = %v, want v1's refusal", err)
	}
}

// A run with nothing to change writes nothing, the manifest included, and
// a binary-only upgrade with the switch on starts one.
func TestManifestNothingToChangeWritesNothing(t *testing.T) {
	opts, f := setup(t)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	setManifestSwitch(t, opts.Dirs, true)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	if m, _ := readDiskManifest(t, opts.Dirs); m != nil {
		t.Fatal("a run with nothing to change wrote a manifest")
	}
	if err := os.WriteFile(opts.Binary, []byte("binary version 2"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	if len(mutateCalls(f.calls)) != 0 {
		t.Errorf("a binary-only upgrade ran %v", mutateCalls(f.calls))
	}
	wantManifestMatchesDisk(t, opts.Dirs, "claude", "codex")
	info, err := os.Lstat(manifestPath(opts.Dirs))
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Dir(filepath.Dir(filepath.Dir(opts.Dirs.Share)))
	before := snapshot(t, home)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	again, err := os.Lstat(manifestPath(opts.Dirs))
	if err != nil || !os.SameFile(info, again) {
		t.Errorf("a run with nothing to change wrote the manifest again: %v", err)
	}
	if moved := changed(before, snapshot(t, home)); len(moved) != 0 {
		t.Errorf("a run with nothing to change changed %v", moved)
	}
}

// With a manifest present, a foreign file, a foreign folder and an empty
// foreign folder in a plugin folder each stop install, a dry run and
// uninstall, changing nothing, even beside a file the manifest lists.
func TestManifestForeignFileOrFolderStops(t *testing.T) {
	for _, foreign := range []string{"plugins/eva-md/notes.txt", "plugins/eva-md/mine/notes.txt", "plugins/eva-md/mine/", "plugins/eva-md/skills/eva-md-recall/notes.txt"} {
		t.Run(foreign, func(t *testing.T) {
			opts, f := setup(t)
			setManifestSwitch(t, opts.Dirs, true)
			if _, err := runInstall(context.Background(), opts, f.run); err != nil {
				t.Fatal(err)
			}
			if m, _ := readDiskManifest(t, opts.Dirs); m == nil {
				t.Fatal("no manifest")
			}
			target := filepath.Join(pluginPath(opts.Dirs.Share, "codex"), filepath.FromSlash(strings.TrimPrefix(foreign, "plugins/eva-md/")))
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(foreign, "/") {
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(target, []byte("the person's own\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			home := filepath.Dir(filepath.Dir(filepath.Dir(opts.Dirs.Share)))
			before := snapshot(t, home)
			f.calls = nil
			// The second release no longer renders a file the manifest lists.
			later := dropping("plugins/eva-md/skills/eva-md-recall/SKILL.md")
			for _, dry := range []bool{true, false} {
				o := opts
				o.DryRun = dry
				_, err := runInstallWith(context.Background(), o, f.run, later)
				if err == nil || !strings.Contains(err.Error(), "unexpected") || !strings.Contains(err.Error(), "move it out of that folder") {
					t.Errorf("install (dry run %v) = %v", dry, err)
				}
			}
			for _, apply := range []bool{false, true} {
				if _, err := uninstallRendered(context.Background(), UninstallOptions{Dirs: opts.Dirs, Apply: apply}, f.run, later); err == nil || !strings.Contains(err.Error(), "unexpected") {
					t.Errorf("uninstall (apply %v) = %v", apply, err)
				}
			}
			if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
				t.Errorf("a refusal changed %v and ran %v", moved, mutateCalls(f.calls))
			}
		})
	}
}

// laterManifest is a manifest a later release might write over the current
// install: each unit lists what the current build renders and a file it
// no longer renders, with fields this build does not know at each level,
// a unit it does not render whose folder is beside eva-md's, and one with
// no folder.
func laterManifest(t *testing.T, dirs paths.Dirs) []byte {
	t.Helper()
	binary := filepath.Join(dirs.Share, "bin", "eva-md")
	var units []any
	for _, host := range []string{"claude", "codex"} {
		files, err := render(host, binary)
		if err != nil {
			t.Fatal(err)
		}
		files["plugins/eva-md/skills/eva-md-later/SKILL.md"] = []byte("a later skill\n")
		var list []any
		for _, name := range slices.Sorted(maps.Keys(files)) {
			entry := map[string]any{"path": name, "sha256": sum(files[name]), "size": len(files[name])}
			if name == ".eva-md-managed" {
				entry["kind"] = "marker"
			}
			list = append(list, entry)
		}
		unit := map[string]any{"id": "claude-code/plugin", "host": "claude-code", "plugin": "eva-md@eva-md", "root": "marketplace/claude", "files": list, "frozen": false}
		if host == "codex" {
			unit = map[string]any{"id": "codex/hooks", "host": "codex", "plugin": "eva-md@eva-md", "root": "marketplace/codex", "files": list, "generation": 0, "frozen": true, "rendered_by": "2026.12.3"}
		}
		units = append(units, unit)
	}
	units = append(units,
		map[string]any{"id": "codex/tools", "host": "codex", "plugin": "eva-md-tools@eva-md", "root": "marketplace/codex-tools", "registered": true,
			"files": []any{map[string]any{"path": "tools.json", "sha256": sum([]byte("{}\n")), "size": 3}}},
		map[string]any{"id": "generic/mcp", "printed": true},
	)
	data, err := json.MarshalIndent(map[string]any{"format": 1, "machine": "w-made-up", "units": units, "schema_reads": map[string]any{"store": []int{1, 2}}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	wantNoRealPath(t, "the made-up manifest", data)
	return data
}

// A later release's manifest that lists a file this build does not render,
// fields it does not know and units it does not render: install removes
// the file and keeps the fields and the units as they were; uninstall then
// removes the plugin folders and leaves the units it does not render.
func TestManifestKeepsALaterReleasesFieldsAndUnits(t *testing.T) {
	opts, f := setup(t)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"claude", "codex"} {
		path := filepath.Join(pluginPath(opts.Dirs.Share, host), "skills", "eva-md-later", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("a later skill\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tools := filepath.Join(opts.Dirs.Share, "marketplace", "codex-tools", "tools.json")
	if err := os.MkdirAll(filepath.Dir(tools), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tools, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := laterManifest(t, opts.Dirs)
	if err := os.WriteFile(manifestPath(opts.Dirs), later, 0o600); err != nil {
		t.Fatal(err)
	}
	// Switch off: the manifest on disk is read anyway.
	result, err := runInstall(context.Background(), opts, f.run)
	if err != nil {
		t.Fatalf("the install over a later release: %v", err)
	}
	var removals int
	for _, a := range result.Actions {
		if strings.Contains(a, ": remove ") {
			removals++
		}
	}
	if removals != 2 {
		t.Errorf("the install named %d removals: %v", removals, result.Actions)
	}
	wantFolders(t, opts.Dirs, render)
	wantManifestMatchesDisk(t, opts.Dirs, "claude", "codex")
	if data, err := os.ReadFile(tools); err != nil || string(data) != "{}\n" {
		t.Errorf("the folder of a unit this build does not render: %q %v", data, err)
	}

	var was, now map[string]any
	_, data := readDiskManifest(t, opts.Dirs)
	if json.Unmarshal(later, &was) != nil || json.Unmarshal(data, &now) != nil {
		t.Fatal("not JSON")
	}
	for _, key := range []string{"machine", "schema_reads"} {
		if !equalJSON(was[key], now[key]) {
			t.Errorf("the manifest's %s became %v", key, now[key])
		}
	}
	nowUnits := byID(now)
	for id, u := range byID(was) {
		switch id {
		case "claude-code/plugin", "codex/hooks":
			for key, v := range u {
				if key != "files" && !equalJSON(v, nowUnits[id][key]) {
					t.Errorf("unit %s's %s became %v", id, key, nowUnits[id][key])
				}
			}
			for _, f := range nowUnits[id]["files"].([]any) {
				entry := f.(map[string]any)
				if entry["path"] == ".eva-md-managed" && entry["kind"] != "marker" {
					t.Errorf("unit %s's marker lost its own field: %v", id, entry)
				}
			}
		default:
			if !equalJSON(u, nowUnits[id]) {
				t.Errorf("unit %s became %v", id, nowUnits[id])
			}
		}
	}
	if len(nowUnits) != len(byID(was)) {
		t.Errorf("the manifest's units: %v", slices.Sorted(maps.Keys(nowUnits)))
	}

	if _, err := uninstall(context.Background(), opts.Dirs, true, f.run); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	for _, host := range []string{"claude", "codex"} {
		if _, err := os.Lstat(marketplacePath(opts.Dirs.Share, host)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the %s folder stayed: %v", host, err)
		}
	}
	if data, err := os.ReadFile(tools); err != nil || string(data) != "{}\n" {
		t.Errorf("uninstall removed the folder of a unit this build does not render: %v", err)
	}
	_, data = readDiskManifest(t, opts.Dirs)
	var left map[string]any
	if json.Unmarshal(data, &left) != nil {
		t.Fatal("no manifest of what a later release installed")
	}
	if units := byID(left); len(units) != 2 || !equalJSON(units["codex/tools"], byID(was)["codex/tools"]) || !equalJSON(left["machine"], was["machine"]) {
		t.Errorf("uninstall left %s", data)
	}
}

func byID(m map[string]any) map[string]map[string]any {
	units := map[string]map[string]any{}
	list, _ := m["units"].([]any)
	for _, u := range list {
		unit := u.(map[string]any)
		units[unit["id"].(string)] = unit
	}
	return units
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// An install that fails after the swap leaves the manifest as it was, or
// absent; one whose rollback also fails leaves one that lists every file in
// the folders, and the next install succeeds.
func TestManifestFailedInstall(t *testing.T) {
	second := dropping("plugins/eva-md/.mcp.json")
	t.Run("as it was", func(t *testing.T) {
		opts, f := setup(t)
		setManifestSwitch(t, opts.Dirs, true)
		if _, err := runInstall(context.Background(), opts, f.run); err != nil {
			t.Fatal(err)
		}
		_, before := readDiskManifest(t, opts.Dirs)
		delete(f.installed, "codex")
		f.fail = "codex plugin add "
		_, err := runInstallWith(context.Background(), opts, f.run, second)
		if err == nil || !strings.Contains(err.Error(), "this run's changes were rolled back") {
			t.Fatalf("the failed upgrade = %v", err)
		}
		if _, after := readDiskManifest(t, opts.Dirs); !bytes.Equal(after, before) {
			t.Errorf("the manifest went from\n%s\nto\n%s", before, after)
		}
		wantFolders(t, opts.Dirs, render)
	})
	t.Run("absent", func(t *testing.T) {
		for _, upgrade := range []bool{false, true} {
			opts, f := setup(t)
			if upgrade {
				if _, err := runInstall(context.Background(), opts, f.run); err != nil {
					t.Fatal(err)
				}
				delete(f.installed, "codex")
			}
			setManifestSwitch(t, opts.Dirs, true)
			f.fail = "codex plugin add "
			if _, err := runInstallWith(context.Background(), opts, f.run, second); err == nil || !strings.Contains(err.Error(), "this run's changes were rolled back") {
				t.Fatalf("the failed install = %v", err)
			}
			if _, err := os.Lstat(manifestPath(opts.Dirs)); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("upgrade %v: a manifest is left: %v", upgrade, err)
			}
		}
	})
	t.Run("rollback incomplete", func(t *testing.T) {
		for _, upgrade := range []bool{false, true} {
			opts, f := setup(t)
			setManifestSwitch(t, opts.Dirs, true)
			if upgrade {
				if _, err := runInstall(context.Background(), opts, f.run); err != nil {
					t.Fatal(err)
				}
				delete(f.installed, "codex")
			}
			codexFailed := false
			run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
				command := host + " " + strings.Join(args, " ")
				if strings.HasPrefix(command, "codex plugin add ") {
					codexFailed = true
					return nil, errors.New("synthetic failure")
				}
				if codexFailed && (strings.HasPrefix(command, "claude plugin uninstall ") || upgrade && strings.HasPrefix(command, "claude plugin list")) {
					return nil, errors.New("synthetic failure")
				}
				return f.run(ctx, host, args...)
			}
			_, err := runInstallWith(context.Background(), opts, run, second)
			if err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
				t.Fatalf("upgrade %v: the failed install = %v", upgrade, err)
			}
			m, _ := readDiskManifest(t, opts.Dirs)
			if m == nil {
				t.Fatalf("upgrade %v: no manifest after a rollback that stopped", upgrade)
			}
			for host, files := range folders(t, opts.Dirs) {
				listed := map[string]bool{}
				for _, u := range m.Units {
					if u.Root == "marketplace/"+host {
						for _, f := range u.Files {
							listed[f.Path] = true
						}
					}
				}
				for name := range files {
					if !listed[name] {
						t.Errorf("upgrade %v: the manifest does not list %s in the %s folder", upgrade, name, host)
					}
				}
			}
			if _, err := runInstallWith(context.Background(), opts, f.run, second); err != nil {
				t.Fatalf("upgrade %v: the next install: %v", upgrade, err)
			}
			wantFolders(t, opts.Dirs, second)
			wantManifestMatchesDisk(t, opts.Dirs, "claude", "codex")
		}
	})
	// The rollback puts the first release's folders back, then stops: the
	// plugin Claude Code had does not verify once re-added. The manifest
	// written before the first change is left, and must list the files of
	// the first release, which the folders now hold: from a manifest on
	// disk and from an adopted v1 folder, and with the switch then on or
	// off, the next install of the second release removes the one it does
	// not render.
	t.Run("rollback stops after the folders are back", func(t *testing.T) {
		for _, fromV1 := range []bool{false, true} {
			for _, onAfter := range []bool{false, true} {
				var opts Options
				var f *fakeHost
				if fromV1 {
					home, dirs := newHome(t)
					layDown(t, home, fixtureAt(t, installedFixtures(t)[0], home))
					f = &fakeHost{dirs: dirs, installed: map[string]bool{"claude": true, "codex": true}, markets: map[string]bool{"claude": true, "codex": true}}
					opts = Options{Dirs: dirs, Binary: newBinary(t), Host: "all"}
					setManifestSwitch(t, dirs, true)
				} else {
					opts, f = setup(t)
					setManifestSwitch(t, opts.Dirs, true)
					if _, err := runInstall(context.Background(), opts, f.run); err != nil {
						t.Fatal(err)
					}
				}
				first := folders(t, opts.Dirs)
				delete(f.installed, "codex")
				codexFailed, claudeAdds := false, 0
				run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
					command := host + " " + strings.Join(args, " ")
					if strings.HasPrefix(command, "codex plugin add ") && !codexFailed {
						codexFailed = true
						return nil, errors.New("synthetic failure")
					}
					if strings.HasPrefix(command, "claude plugin install ") {
						claudeAdds++
					}
					// The rollback's re-add of Claude Code's plugin, after
					// the folders are restored, does not verify.
					if claudeAdds >= 2 && strings.HasPrefix(command, "claude plugin list") {
						return nil, errors.New("synthetic failure")
					}
					return f.run(ctx, host, args...)
				}
				_, err := runInstallWith(context.Background(), opts, run, second)
				if err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
					t.Fatalf("v1 %v: the failed install = %v", fromV1, err)
				}
				if claudeAdds < 2 {
					t.Fatalf("v1 %v: the rollback did not get as far as the re-add", fromV1)
				}
				now := folders(t, opts.Dirs)
				if !maps.EqualFunc(now, first, func(a, b map[string][]byte) bool { return maps.EqualFunc(a, b, bytes.Equal) }) {
					t.Fatalf("v1 %v: the rollback did not put the first release's folders back", fromV1)
				}
				m, data := readDiskManifest(t, opts.Dirs)
				if m == nil {
					t.Fatalf("v1 %v: no manifest after a rollback that stopped", fromV1)
				}
				for host, files := range now {
					listed := map[string]bool{}
					for _, u := range m.Units {
						if u.Root == "marketplace/"+host {
							var names []string
							for _, f := range u.Files {
								listed[f.Path] = true
								names = append(names, f.Path)
							}
							if !slices.IsSorted(names) {
								t.Errorf("v1 %v: the manifest lists %s's files unsorted: %v", fromV1, host, names)
							}
						}
					}
					for name := range files {
						if !listed[name] {
							t.Errorf("v1 %v: the manifest does not list %s in the %s folder: %s", fromV1, name, host, data)
						}
					}
				}
				setManifestSwitch(t, opts.Dirs, onAfter)
				f.installed["claude"], f.installed["codex"] = true, true
				if _, err := runInstallWith(context.Background(), opts, f.run, second); err != nil {
					t.Fatalf("v1 %v, switch %v: the next install: %v", fromV1, onAfter, err)
				}
				wantFolders(t, opts.Dirs, second)
				wantManifestMatchesDisk(t, opts.Dirs, "claude", "codex")
			}
		}
	})
}

// Each unreadable manifest stops install, its dry run and preflight, and
// uninstall and its preview, with one line that names the file, changing
// nothing; a manifest that breaks no rule is read.
func TestManifestUnreadable(t *testing.T) {
	unit := func(id, root string, paths ...string) string {
		var files []string
		for _, p := range paths {
			files = append(files, `{"path":`+quote(p)+`,"sha256":"`+strings.Repeat("ab", 32)+`","size":1}`)
		}
		host, _, _ := strings.Cut(id, "/")
		return `{"id":` + quote(id) + `,"host":` + quote(host) + `,"plugin":"eva-md@eva-md","root":` + quote(root) + `,"files":[` + strings.Join(files, ",") + `]}`
	}
	doc := func(units ...string) string { return `{"format":1,"units":[` + strings.Join(units, ",") + `]}` }
	forms := map[string]string{
		"not JSON":                      `{"format":1,`,
		"not an object":                 `[]`,
		"null":                          `null`,
		"a newer format":                `{"format":2,"units":[]}`,
		"no format":                     `{"units":[]}`,
		"a format that is a word":       `{"format":"1","units":[]}`,
		"an absolute path":              doc(unit("claude-code/plugin", "marketplace/claude", "/x/plugin.json")),
		"an unclean path":               doc(unit("claude-code/plugin", "marketplace/claude", "plugins/./eva-md/x")),
		"a path with a slash last":      doc(unit("claude-code/plugin", "marketplace/claude", "plugins/eva-md/")),
		"an empty path":                 doc(unit("claude-code/plugin", "marketplace/claude", "")),
		"a path that leaves":            doc(unit("claude-code/plugin", "marketplace/claude", "../codex/x")),
		"a path that leaves later":      doc(unit("codex/hooks", "marketplace/codex", "plugins/../../x")),
		"a path listed twice":           doc(unit("claude-code/plugin", "marketplace/claude", "a", "a")),
		"a root outside":                doc(unit("codex/tools", "bin", "eva-md")),
		"the marketplace as root":       doc(unit("codex/tools", "marketplace", "claude/x")),
		"an absolute root":              doc(unit("codex/tools", "/marketplace/claude", "x")),
		"a root that leaves":            doc(unit("codex/tools", "marketplace/../bin", "eva-md")),
		"a root that leaves later":      doc(unit("codex/tools", "marketplace/claude/../../bin", "eva-md")),
		"a unit with files and no root": `{"format":1,"units":[{"id":"codex/tools","files":[{"path":"x"}]}]}`,
		"a unit elsewhere":              doc(unit("claude-code/plugin", "marketplace/codex", "x")),
		"a unit of another host":        `{"format":1,"units":[{"id":"codex/hooks","host":"claude-code","plugin":"eva-md@eva-md","root":"marketplace/codex","files":[]}]}`,
		"a unit twice":                  doc(unit("codex/tools", "marketplace/tools", "x"), unit("codex/tools", "marketplace/tools", "y")),
		"a unit with no id":             `{"format":1,"units":[{"root":"marketplace/tools","files":[]}]}`,
		"a file with no hash":           `{"format":1,"units":[{"id":"claude-code/plugin","host":"claude-code","plugin":"eva-md@eva-md","root":"marketplace/claude","files":[{"path":"x","size":1}]}]}`,
		"a hash in capitals":            `{"format":1,"units":[{"id":"claude-code/plugin","host":"claude-code","plugin":"eva-md@eva-md","root":"marketplace/claude","files":[{"path":"x","sha256":"` + strings.Repeat("AB", 32) + `","size":1}]}]}`,
		"too large":                     `{"format":1,"units":[]` + strings.Repeat(" ", manifestLimit) + `}`,
		"a link":                        "",
		"a folder":                      "",
		"the path ..":                   doc(unit("claude-code/plugin", "marketplace/claude", "..")),
		"the path .":                    doc(unit("claude-code/plugin", "marketplace/claude", ".")),
		"a new line in a path":          doc(unit("claude-code/plugin", "marketplace/claude", "a\nb")),
		"an escape in a later root":     doc(unit("codex/tools", "marketplace/tools\x1b[2J", "x")),
		"a unit of another plugin":      `{"format":1,"units":[{"id":"claude-code/plugin","host":"claude-code","plugin":"eva-md-tools@eva-md","root":"marketplace/claude","files":[]}]}`,
		"a later generation":            `{"format":1,"units":[{"id":"codex/hooks","host":"codex","plugin":"eva-md@eva-md","root":"marketplace/codex","generation":1,"files":[]}]}`,
		"the hooks unit as hosts.md 4.5 lays it out": `{"format":1,"units":[{"id":"codex/hooks","host":"codex","plugin":"eva-md@eva-md","root":"marketplace/codex/plugins/eva-md","generation":1,"files":[]}]}`,
		"a file with no size":                        `{"format":1,"units":[{"id":"claude-code/plugin","host":"claude-code","plugin":"eva-md@eva-md","root":"marketplace/claude","files":[{"path":"x","sha256":"` + strings.Repeat("ab", 32) + `"}]}]}`,
		"a negative size":                            `{"format":1,"units":[{"id":"claude-code/plugin","host":"claude-code","plugin":"eva-md@eva-md","root":"marketplace/claude","files":[{"path":"x","sha256":"` + strings.Repeat("ab", 32) + `","size":-1}]}]}`,
		// A manifest's own text reaches the message only escaped and cut.
		"an id with a new line": `{"format":1,"units":[{"id":` + quote("x\nFAKE: second line") + `,"root":"bin","files":[]}]}`,
		"an id with an escape":  `{"format":1,"units":[{"id":` + quote("x\x1b[2J") + `,"root":"bin","files":[]}]}`,
		"a long id":             `{"format":1,"units":[{"id":"` + strings.Repeat("x", 200000) + `","root":"bin","files":[]}]}`,
	}
	for name, content := range forms {
		wantNoRealPath(t, name, []byte(content))
	}
	forms["readable"] = doc(unit("codex/tools", "marketplace/tools", "x"))
	// A later release's unit with no folder, such as a printed MCP entry.
	forms["readable, a unit with no folder"] = `{"format":1,"units":[{"id":"generic/mcp","files":[]},{"id":"generic/other"}]}`
	for name, content := range forms {
		t.Run(name, func(t *testing.T) {
			opts, f := setup(t)
			setManifestSwitch(t, opts.Dirs, true)
			if _, err := runInstall(context.Background(), opts, f.run); err != nil {
				t.Fatal(err)
			}
			path := manifestPath(opts.Dirs)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "a link":
				target := filepath.Join(t.TempDir(), "manifest.json")
				if err := os.WriteFile(target, []byte(doc()), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "a folder":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// The binary changes, so a readable manifest's install has work.
			if err := os.WriteFile(opts.Binary, []byte("binary version 2"), 0o700); err != nil {
				t.Fatal(err)
			}
			home := filepath.Dir(filepath.Dir(filepath.Dir(opts.Dirs.Share)))
			before := snapshot(t, home)
			f.calls = nil
			var errs []error
			for _, o := range []Options{{DryRun: true}, {DryRun: true, Preflight: true}, {}} {
				o.Dirs, o.Binary, o.Host = opts.Dirs, opts.Binary, opts.Host
				_, err := runInstall(context.Background(), o, f.run)
				errs = append(errs, err)
			}
			for _, apply := range []bool{false, true} {
				_, err := uninstall(context.Background(), opts.Dirs, apply, f.run)
				errs = append(errs, err)
			}
			if strings.HasPrefix(name, "readable") {
				for _, err := range errs {
					if err != nil {
						t.Errorf("a readable manifest: %v", err)
					}
				}
				return
			}
			for i, err := range errs {
				if err == nil || !strings.Contains(err.Error(), path+" can't be read") || strings.ContainsFunc(err.Error(), unicode.IsControl) || len(err.Error()) > 1024 {
					t.Errorf("run %d = %.2000q, want one short line naming %s", i, err, path)
				}
			}
			if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
				t.Errorf("an unreadable manifest let %v change and %v run", moved, mutateCalls(f.calls))
			}
		})
	}
}

// installLocked says whether an install holds the install lock.
func installLocked(t *testing.T, dirs paths.Dirs) bool {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dirs.Locks(), "install.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

func quote(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// A manifest that changes between install's or uninstall's preflight and
// its run under the lock stops it before any change.
func TestManifestChangedDuringPreflight(t *testing.T) {
	opts, f := setup(t)
	setManifestSwitch(t, opts.Dirs, true)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.Binary, []byte("binary version 2"), 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Dir(filepath.Dir(filepath.Dir(opts.Dirs.Share)))
	var before map[string]node
	run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		// The first host command under the install lock: the recheck.
		if before == nil && installLocked(t, opts.Dirs) {
			_, data := readDiskManifest(t, opts.Dirs)
			if err := os.WriteFile(manifestPath(opts.Dirs), append(data, ' '), 0o600); err != nil {
				t.Fatal(err)
			}
			before = snapshot(t, home)
		}
		return f.run(ctx, host, args...)
	}
	f.calls = nil
	_, err := runInstall(context.Background(), opts, run)
	if before == nil {
		t.Fatal("no host command ran under the install lock")
	}
	if err == nil || !strings.Contains(err.Error(), "the install manifest changed during preflight") {
		t.Fatalf("the install = %v", err)
	}
	if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
		t.Errorf("the install changed %v and ran %v", moved, mutateCalls(f.calls))
	}
	// Uninstall rechecks it under the lock too.
	before = nil
	f.calls = nil
	if _, err := uninstallRendered(context.Background(), UninstallOptions{Dirs: opts.Dirs, Apply: true}, run, render); err == nil || !strings.Contains(err.Error(), "the install manifest changed during uninstall preflight") {
		t.Fatalf("the uninstall = %v", err)
	}
	if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
		t.Errorf("the uninstall changed %v and ran %v", moved, mutateCalls(f.calls))
	}
}

// The removed list is rechecked under the lock: a file the manifest lists
// and the second release does not render that goes between preflight and
// the lock stops install before any change, though the folder still
// differs from what the release renders.
func TestManifestRemovedChangedDuringPreflight(t *testing.T) {
	opts, f := setup(t)
	setManifestSwitch(t, opts.Dirs, true)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	home := filepath.Dir(filepath.Dir(filepath.Dir(opts.Dirs.Share)))
	gone := filepath.Join(pluginPath(opts.Dirs.Share, "codex"), "skills", "eva-md-digest", "SKILL.md")
	var before map[string]node
	run := func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if before == nil && installLocked(t, opts.Dirs) {
			if err := os.Remove(gone); err != nil {
				t.Fatal(err)
			}
			before = snapshot(t, home)
		}
		return f.run(ctx, host, args...)
	}
	f.calls = nil
	_, err := runInstallWith(context.Background(), opts, run, dropping("plugins/eva-md/skills/"))
	if before == nil {
		t.Fatal("no host command ran under the install lock")
	}
	if err == nil || !strings.Contains(err.Error(), "generated files changed during preflight") {
		t.Fatalf("the install = %v", err)
	}
	if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
		t.Errorf("the install changed %v and ran %v", moved, mutateCalls(f.calls))
	}
}

// addUnit adds unit to the manifest on disk.
func addUnit(t *testing.T, dirs paths.Dirs, unit map[string]any) {
	t.Helper()
	_, data := readDiskManifest(t, dirs)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["units"] = append(m["units"].([]any), unit)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	wantNoRealPath(t, "the made-up manifest", data)
	if err := os.WriteFile(manifestPath(dirs), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A unit this build does not render whose folder is in, or is, a plugin
// folder this build renders stops install, its dry run, and uninstall and
// its preview, with one line naming that folder, changing nothing: the
// folder swap or uninstall would remove it, and the manifest would then
// list files that are gone. An install of the other host only goes on.
func TestManifestNestedUnitStops(t *testing.T) {
	for _, root := range []string{"marketplace/codex/plugins/eva-md-hooks", "marketplace/codex"} {
		t.Run(root, func(t *testing.T) {
			opts, f := setup(t)
			setManifestSwitch(t, opts.Dirs, true)
			if _, err := runInstall(context.Background(), opts, f.run); err != nil {
				t.Fatal(err)
			}
			hooks := filepath.Join(opts.Dirs.Share, filepath.FromSlash(root), "hooks.json")
			if err := os.MkdirAll(filepath.Dir(hooks), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(hooks, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			addUnit(t, opts.Dirs, map[string]any{"id": "codex/hooks-later", "host": "codex", "plugin": "eva-md-hooks@eva-md", "root": root, "registered": true,
				"files": []any{map[string]any{"path": "hooks.json", "sha256": sum([]byte("{}\n")), "size": 3}}})
			home := filepath.Dir(filepath.Dir(filepath.Dir(opts.Dirs.Share)))
			before := snapshot(t, home)
			f.calls = nil
			folder := filepath.Join(opts.Dirs.Share, filepath.FromSlash(root))
			want := func(what string, err error) {
				t.Helper()
				if err == nil || !strings.Contains(err.Error(), folder+" is the folder of a unit this release does not render") || strings.Contains(err.Error(), "\n") {
					t.Errorf("%s = %v", what, err)
				}
			}
			later := dropping("plugins/eva-md/.mcp.json")
			for _, dry := range []bool{true, false} {
				o := opts
				o.DryRun = dry
				_, err := runInstallWith(context.Background(), o, f.run, later)
				want("install", err)
			}
			for _, apply := range []bool{false, true} {
				_, err := uninstallRendered(context.Background(), UninstallOptions{Dirs: opts.Dirs, Apply: apply}, f.run, later)
				want("uninstall", err)
			}
			if moved := changed(before, snapshot(t, home)); len(moved) != 0 || len(mutateCalls(f.calls)) != 0 {
				t.Errorf("a refusal changed %v and ran %v", moved, mutateCalls(f.calls))
			}
			o := opts
			o.Host = "claude"
			if _, err := runInstallWith(context.Background(), o, f.run, later); err != nil {
				t.Fatalf("the install of Claude Code only: %v", err)
			}
			if data, err := os.ReadFile(hooks); err != nil || string(data) != "{}\n" {
				t.Errorf("the later unit's file: %q %v", data, err)
			}
			m, _ := readDiskManifest(t, opts.Dirs)
			if i := slices.IndexFunc(m.Units, func(u diskUnit) bool { return u.ID == "codex/hooks-later" }); i < 0 || len(m.Units[i].Files) != 1 {
				t.Errorf("the later unit is no longer listed as it was: %v", m.Units)
			}
		})
	}
}

// Uninstall reads the manifest as install does. This build's uninstall
// over a later release's layout, which lists a file this build does not
// render, with no install between them and the switch off, removes the
// plugin folders and the manifest, and its preview says so.
func TestManifestUninstallOverALaterLayout(t *testing.T) {
	opts, f := setup(t)
	setManifestSwitch(t, opts.Dirs, true)
	later := adding("plugins/eva-md/skills/eva-md-later/SKILL.md", "a later skill")
	if _, err := runInstallWith(context.Background(), opts, f.run, later); err != nil {
		t.Fatal(err)
	}
	setManifestSwitch(t, opts.Dirs, false)
	home := filepath.Dir(filepath.Dir(filepath.Dir(opts.Dirs.Share)))
	before := snapshot(t, home)
	preview, err := uninstallRendered(context.Background(), UninstallOptions{Dirs: opts.Dirs}, f.run, render)
	if err != nil {
		t.Fatalf("the preview over a later layout: %v", err)
	}
	if !slices.Contains(preview.Actions, "the install manifest, "+manifestPath(opts.Dirs)) {
		t.Errorf("the preview does not name the manifest: %v", preview.Actions)
	}
	if moved := changed(before, snapshot(t, home)); len(moved) != 0 {
		t.Errorf("the preview changed %v", moved)
	}
	if _, err := uninstallRendered(context.Background(), UninstallOptions{Dirs: opts.Dirs, Apply: true}, f.run, render); err != nil {
		t.Fatalf("the uninstall over a later layout: %v", err)
	}
	for _, path := range []string{marketplacePath(opts.Dirs.Share, "claude"), marketplacePath(opts.Dirs.Share, "codex"), manifestPath(opts.Dirs)} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("uninstall left %s: %v", path, err)
		}
	}
}

// Uninstall keeps a later release's units listed, and says so; with --all,
// which removes the data folder, its preview names the manifest and the
// folder of each such unit, and keeps their registrations.
func TestManifestUninstallSaysWhatALaterReleaseKeeps(t *testing.T) {
	opts, f := setup(t)
	setManifestSwitch(t, opts.Dirs, true)
	if _, err := runInstall(context.Background(), opts, f.run); err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(opts.Dirs.Share, "marketplace", "codex-tools")
	if err := os.MkdirAll(tools, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, "tools.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	addUnit(t, opts.Dirs, map[string]any{"id": "codex/tools", "host": "codex", "plugin": "eva-md-tools@eva-md", "root": "marketplace/codex-tools", "registered": true,
		"files": []any{map[string]any{"path": "tools.json", "sha256": sum([]byte("{}\n")), "size": 3}}})
	addUnit(t, opts.Dirs, map[string]any{"id": "generic/mcp", "files": []any{}})
	all, err := uninstallRendered(context.Background(), UninstallOptions{Dirs: opts.Dirs, All: true}, f.run, render)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"the install manifest, " + manifestPath(opts.Dirs) + ", which lists what a later release installed", "the folder of a later release's unit, " + tools} {
		if !slices.Contains(all.Actions, want) {
			t.Errorf("the --all preview does not say %q: %v", want, all.Actions)
		}
	}
	if !slices.Contains(all.Kept, "the host registrations of a later release's units, which this release does not make") {
		t.Errorf("the --all preview keeps %v", all.Kept)
	}
	r, err := uninstallRendered(context.Background(), UninstallOptions{Dirs: opts.Dirs, Apply: true}, f.run, render)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.Kept, manifestPath(opts.Dirs)+", which lists what a later release installed") {
		t.Errorf("uninstall keeps %v", r.Kept)
	}
	if slices.ContainsFunc(r.Actions, func(a string) bool { return strings.HasPrefix(a, "the install manifest") }) {
		t.Errorf("uninstall says it removes the manifest it keeps: %v", r.Actions)
	}
	m, _ := readDiskManifest(t, opts.Dirs)
	if m == nil || len(m.Units) != 2 || m.Units[0].ID != "codex/tools" || m.Units[1].ID != "generic/mcp" {
		t.Errorf("uninstall left %v", m)
	}
	if data, err := os.ReadFile(filepath.Join(tools, "tools.json")); err != nil || string(data) != "{}\n" {
		t.Errorf("the later unit's folder: %q %v", data, err)
	}
}

// A manifest that has only Codex's unit, as `install --host codex` leaves
// it, gets Claude Code's unit first, in the adapter's order.
func TestManifestUnitOrder(t *testing.T) {
	opts, f := setup(t)
	setManifestSwitch(t, opts.Dirs, true)
	codex := opts
	codex.Host = "codex"
	if _, err := runInstall(context.Background(), codex, f.run); err != nil {
		t.Fatal(err)
	}
	if m, _ := readDiskManifest(t, opts.Dirs); m == nil || len(m.Units) != 1 || m.Units[0].ID != "codex/hooks" {
		t.Fatalf("after Codex alone: %v", m)
	}
	claude := opts
	claude.Host = "claude"
	if _, err := runInstall(context.Background(), claude, f.run); err != nil {
		t.Fatal(err)
	}
	m, data := readDiskManifest(t, opts.Dirs)
	if m == nil || len(m.Units) != 2 || m.Units[0].ID != "claude-code/plugin" || m.Units[1].ID != "codex/hooks" {
		t.Errorf("after Claude Code: %s", data)
	}
	wantManifestMatchesDisk(t, opts.Dirs, "claude", "codex")
}
