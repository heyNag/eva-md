package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// ErrBusy is uninstall's error while a background update or the daily job
// holds its lock: it removed nothing.
var ErrBusy = errors.New("a background update or the daily job is running, so nothing was removed; try again shortly")

// UninstallOptions say what uninstall does. Without Apply it only says what
// it would remove and keep. All removes the config and the local data too.
type UninstallOptions struct {
	Dirs  paths.Dirs
	Apply bool
	All   bool
}

// UninstallResult says what uninstall removes, or removed, and what it keeps.
// Actions lists what it removes, in plain words, and Kept what it keeps.
type UninstallResult struct {
	Result
	Kept []string
	// Wiki and Remote name the wiki folder and where it is pushed, which
	// uninstall never touches; they are empty when eva-md knows neither.
	Wiki, Remote string
	// WikiFromConfig says the config named Wiki when uninstall read it,
	// before it removed anything. Otherwise Wiki is the record's, as when the
	// config is missing, can't be read or names no wiki.
	WikiFromConfig bool
}

// Uninstall previews by default. If a native step fails, the generated files
// are kept so the registrations that remain still work. The wiki folder and
// its remote are never touched.
func Uninstall(ctx context.Context, opts UninstallOptions) (UninstallResult, error) {
	return uninstallWith(ctx, opts, native)
}

func uninstall(ctx context.Context, dirs paths.Dirs, apply bool, run runner) (Result, error) {
	r, err := uninstallWith(ctx, UninstallOptions{Dirs: dirs, Apply: apply}, run)
	return r.Result, err
}

func uninstallWith(ctx context.Context, opts UninstallOptions, run runner) (UninstallResult, error) {
	return uninstallRendered(ctx, opts, run, render)
}

// uninstallRendered is uninstall, with the plugin files of this release as
// render gives them.
func uninstallRendered(ctx context.Context, opts UninstallOptions, run runner, render renderer) (result UninstallResult, err error) {
	dirs := opts.Dirs
	result.Binary = filepath.Join(dirs.Share, "bin", "eva-md")
	// The record says where the link and the wiki are; without it, uninstall
	// uses the known layout and the config.
	rec, _ := ReadRecord(dirs)
	link := LinkPath(dirs)
	if item, ok := rec.Find(KindPathLink, ""); ok && item.Path != "" {
		link = item.Path
	}
	if item, ok := rec.Find(KindWiki, ""); ok {
		result.Wiki = item.Path
	}
	if item, ok := rec.Find(KindWikiRemote, ""); ok {
		result.Remote = item.Value
	}
	c, cerr := config.Load(dirs.ConfigFile())
	if cerr == nil && c.WikiPath != "" {
		result.Wiki, result.Remote, result.WikiFromConfig = c.WikiPath, c.ExpectedRemote, true
	}
	if opts.All && result.Wiki != "" {
		for _, folder := range []string{dirs.Config, dirs.State, dirs.Share} {
			if within(result.Wiki, folder) {
				return result, fmt.Errorf("install: the wiki is inside %s, so --all would delete it; move the wiki first", folder)
			}
		}
	}
	// Without the install folder or its marker, the hosts may still list
	// eva-md, so their registrations are checked and removed anyway.
	owned := false
	root, err := paths.OpenRoot(dirs.Share, false)
	if errors.Is(err, fs.ErrNotExist) {
		root, err = nil, nil
	}
	if err != nil {
		return result, err
	}
	// Every host, in the adapter's order.
	hosts, _ := selectedHosts("all")
	if root != nil {
		defer root.Close()
		if owned, err = hasMarker(root, installMarker, installMarkerValue); err != nil {
			return result, err
		}
		if !owned {
			names := []string{"bin/eva-md"}
			for _, host := range hosts {
				names = append(names, filepath.Join("marketplace", host))
			}
			for _, name := range names {
				if _, e := root.Lstat(name); !errors.Is(e, fs.ErrNotExist) {
					return result, errors.New("install: refusing to uninstall an unmanaged installation")
				}
			}
		}
	}
	// The install manifest says which other files of the plugin folders
	// are eva-md's, as for install.
	var man *manifest
	var manBefore []byte
	if owned {
		if man, manBefore, err = readManifest(root); err != nil {
			return result, err
		}
		if man == nil && cerr == nil && ManifestSwitch.On(c) {
			man = &manifest{}
		}
	}
	var plans []*hostPlan
	for _, host := range hosts {
		if owned {
			files, e := render(host, result.Binary)
			if e != nil {
				return result, e
			}
			listed, e := man.listedIn(unitOf(host), dirs.Share)
			if e != nil {
				return result, e
			}
			if _, _, e = treeChanged(root, filepath.Join("marketplace", host), files, listed, false); e != nil {
				return result, e
			}
		}
		s, e := inspect(ctx, dirs, host, run)
		if e != nil {
			return result, e
		}
		if !s.Available {
			if !owned {
				continue
			}
			if _, e = root.Lstat(filepath.Join("marketplace", host)); e == nil {
				return result, fmt.Errorf("install: %s CLI missing; cannot safely unregister its marketplace", host)
			}
			continue
		}
		plans = append(plans, &hostPlan{host: host, before: s})
		if s.Installed || s.Marketplace {
			result.Hosts = append(result.Hosts, host)
			if s.FolderMissing {
				result.Actions = append(result.Actions, host+": the eva-md plugin and its marketplace registration, whose folder is missing")
			} else {
				result.Actions = append(result.Actions, host+": the eva-md plugin and its marketplace registration")
			}
		}
	}
	if owned {
		result.Actions = append(result.Actions, "the plugin folders and the stable binary, "+result.Binary)
	}
	// Units of a later release, which this one does not render, stay listed.
	var manAfter *manifest
	if manBefore != nil {
		manifestPath := filepath.Join(dirs.Share, manifestFile)
		if manAfter = man.withoutKnown(); len(manAfter.Units) == 0 {
			manAfter = nil
			result.Actions = append(result.Actions, "the install manifest, "+manifestPath)
		} else if !opts.All {
			result.Kept = append(result.Kept, manifestPath+", which lists what a later release installed")
		} else {
			// --all removes the data folder, and with it the manifest and the
			// folders of a later release's units; their registrations with
			// the hosts are that release's to remove.
			result.Actions = append(result.Actions, "the install manifest, "+manifestPath+", which lists what a later release installed")
			for _, u := range manAfter.Units {
				if u.Root == "" {
					continue
				}
				if _, e := root.Lstat(filepath.FromSlash(u.Root)); e == nil {
					result.Actions = append(result.Actions, "the folder of a later release's unit, "+shownIn(dirs.Share, u.Root))
				}
			}
			result.Kept = append(result.Kept, "the host registrations of a later release's units, which this release does not make")
		}
	}
	ourLink := ownLink(link, result.Binary)
	if ourLink {
		result.Actions = append(result.Actions, "the link "+link)
	} else if _, e := os.Lstat(link); e == nil {
		result.Kept = append(result.Kept, link+", which isn't eva-md's own link")
	}
	// The config and the local data are listed when they exist, with their
	// sizes, as removed with All and kept without.
	var local []string
	if _, e := os.Lstat(dirs.Config); e == nil {
		local = append(local, fmt.Sprintf("the config, %s (config.toml and config.env), %s", dirs.Config, size(dirSize(dirs.Config))))
	}
	record := filepath.Join(dirs.Share, RecordFile)
	if _, e := os.Lstat(dirs.State); e == nil {
		local = append(local, fmt.Sprintf("the local data, %s (inbox, search index, logs and caches) and the install record, %s", dirs.State, size(dirSize(dirs.State)+fileSize(record))))
	} else if _, e := os.Lstat(record); e == nil {
		local = append(local, "the install record, "+record)
	}
	if opts.All {
		result.Actions = append(result.Actions, local...)
	} else {
		result.Kept = append(result.Kept, local...)
	}
	if !opts.Apply {
		return result, nil
	}
	release, err := lock(ctx, dirs.Locks())
	if err != nil {
		return result, err
	}
	defer release()
	// The background update and the daily job read the binary, the config
	// and the inbox, so nothing is removed while either runs, and neither
	// starts until uninstall is done.
	for _, name := range []string{"update.lock", "maintain.lock"} {
		unlock, busy, e := paths.TryLock(dirs.Locks(), name)
		if e != nil {
			return result, e
		}
		if busy {
			return result, ErrBusy
		}
		defer unlock()
	}
	for _, p := range plans {
		now, e := inspect(ctx, dirs, p.host, run)
		if e != nil {
			return result, e
		}
		if now != p.before {
			return result, errors.New("install: host state changed during uninstall preflight")
		}
	}
	if owned {
		if _, now, e := readManifest(root); e != nil {
			return result, e
		} else if !bytes.Equal(now, manBefore) || (now == nil) != (manBefore == nil) {
			return result, errors.New("install: the install manifest changed during uninstall preflight")
		}
	}
	for _, p := range plans {
		// With its marketplace's folder missing, Codex can't say whether it
		// has the plugin, and removes it either way.
		if p.before.Installed || p.before.FolderMissing {
			if _, err = run(ctx, command(p.host), removeArgs(p.host)...); err != nil {
				return result, err
			}
		}
		if p.before.Marketplace {
			if _, err = run(ctx, command(p.host), marketRemoveArgs(p.host)...); err != nil {
				return result, err
			}
		}
		status, e := inspect(ctx, dirs, p.host, run)
		if e != nil {
			return result, e
		}
		if status.Installed || status.Marketplace {
			return result, errors.New("install: host still reports eva-md; generated files retained")
		}
	}
	if owned {
		for _, p := range plans {
			if err = root.RemoveAll(filepath.Join("marketplace", p.host)); err != nil {
				return result, err
			}
		}
		if manAfter != nil {
			onDisk := manBefore
			if _, err = putManifest(ctx, root, manAfter, &onDisk); err != nil {
				return result, err
			}
		} else if manBefore != nil {
			if err = root.Remove(manifestFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return result, err
			}
		}
		if err = root.Remove("bin/eva-md"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return result, err
		}
		if err = root.Remove(installMarker); err != nil {
			return result, err
		}
		// Remove the folders install created; Remove fails on a folder that
		// is not empty, so anything else in them stays.
		_ = root.Remove("bin")
		_ = root.Remove("marketplace")
	}
	// The link goes only while it still points at the stable binary.
	if ownLink(link, result.Binary) {
		if err = os.Remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return result, err
		}
	}
	if state, e := paths.OpenRoot(dirs.State, false); e == nil {
		_ = state.Remove("needs-trust.json")
		_ = state.Remove(cliPathsFile)
		state.Close()
	}
	if opts.All {
		for _, folder := range []string{dirs.Config, dirs.State, dirs.Share} {
			if err = os.RemoveAll(folder); err != nil {
				return result, fmt.Errorf("install: remove %s: %w", folder, err)
			}
		}
		return result, nil
	}
	// The record keeps what is left: the config, the data and the wiki.
	if root != nil {
		if r, ok := ReadRecord(dirs); ok {
			kept := r.Items[:0]
			for _, item := range r.Items {
				switch item.Kind {
				case KindBinary, KindMarketplace, KindPlugin, KindPathLink:
				default:
					kept = append(kept, item)
				}
			}
			r.Items = kept
			if err = writeRecord(ctx, root, r); err != nil {
				return result, err
			}
		}
	}
	_ = os.Remove(dirs.Share)
	return result, nil
}

// within reports whether path is folder or inside it.
func within(path, folder string) bool {
	path, folder = filepath.Clean(path), filepath.Clean(folder)
	return path == folder || strings.HasPrefix(path, folder+string(filepath.Separator))
}

// dirSize adds up the sizes of the regular files under folder, following no
// link.
func dirSize(folder string) int64 {
	var total int64
	_ = filepath.WalkDir(folder, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func fileSize(file string) int64 {
	if info, err := os.Lstat(file); err == nil && info.Mode().IsRegular() {
		return info.Size()
	}
	return 0
}

// size words a number of bytes.
func size(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}
