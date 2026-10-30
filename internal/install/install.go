// Package install owns eva-md's stable binary and generated local marketplaces.
// Host registration uses native commands; no host settings or trust records are
// read or written directly.
package install

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

type Options struct {
	Dirs   paths.Dirs
	Binary string
	Host   string // claude, codex, all; empty means all available hosts
	DryRun bool
}
type Result struct {
	Binary        string
	Hosts         []string
	Actions       []string
	NeedsTrust    bool
	CodexFeatures []Feature
}

// Feature records only native feature names/stages/states, never diagnostics.
type Feature struct {
	Name, Stage string
	Enabled     bool
}

const installMarker = ".eva-md-install"
const installMarkerValue = "eva-md installation v1\n"

type hostPlan struct {
	host                  string
	before                Status
	files                 map[string][]byte
	changed               bool
	registrationAttempted bool
	swap                  *folderSwap
}

// Run verifies ownership and host state before replacing any generated files.
// A failed native command is reconciled through inventories before rollback.
func Run(ctx context.Context, opts Options) (Result, error) { return runInstall(ctx, opts, native) }

func runInstall(ctx context.Context, opts Options, run runner) (result Result, err error) {
	result.Binary = filepath.Join(opts.Dirs.Share, "bin", "eva-md")
	if _, err = config.LoadContext(ctx, opts.Dirs.ConfigFile()); err != nil {
		return result, fmt.Errorf("install: run eva-md setup first: %w", err)
	}
	hosts, err := selectedHosts(opts.Host)
	if err != nil {
		return result, err
	}
	var plans []*hostPlan
	for _, host := range hosts {
		status, err := inspect(ctx, opts.Dirs, host, run)
		if err != nil {
			return result, err
		}
		if !status.Available {
			if opts.Host == host {
				return result, fmt.Errorf("install: %s CLI not found", host)
			}
			result.Actions = append(result.Actions, host+": CLI not found; skipped")
			continue
		}
		if status.Installed && !status.Valid {
			return result, fmt.Errorf("install: existing %s eva-md registration needs attention: %s", host, status.Reason)
		}
		files, err := render(host, result.Binary)
		if err != nil {
			return result, err
		}
		plans = append(plans, &hostPlan{host: host, before: status, files: files})
		result.Hosts = append(result.Hosts, host)
		if host == "codex" {
			features, err := run(ctx, host, "features", "list")
			if err != nil {
				return result, err
			}
			for _, line := range strings.Split(string(features), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 3 && (fields[2] == "false" || fields[2] == "true") && plainFeature(fields[0]) && plainFeature(fields[1]) {
					result.CodexFeatures = append(result.CodexFeatures, Feature{Name: fields[0], Stage: fields[1], Enabled: fields[2] == "true"})
				}
				if len(fields) >= 2 && fields[0] == "hooks" && fields[len(fields)-1] == "false" {
					return result, errors.New("install: Codex hooks feature is disabled; enable it in Codex before installing")
				}
			}
		}
	}
	if len(plans) == 0 {
		return result, errors.New("install: neither host CLI is available")
	}
	if opts.Binary == "" {
		opts.Binary, err = os.Executable()
		if err != nil {
			return result, err
		}
	}
	binary, err := readRegular(opts.Binary, 128<<20)
	if err != nil {
		return result, fmt.Errorf("install: read executable: %w", err)
	}
	if len(binary) == 0 {
		return result, errors.New("install: empty executable")
	}
	// Dry runs inspect the tree without creating directories or lock files.
	share, err := paths.OpenRoot(opts.Dirs.Share, false)
	if errors.Is(err, fs.ErrNotExist) {
		share = nil
		err = nil
	}
	if err != nil {
		return result, err
	}
	if share != nil {
		defer share.Close()
	}
	var previousBinary []byte
	var owned bool
	if share != nil {
		owned, err = hasMarker(share, installMarker, installMarkerValue)
		if err != nil {
			return result, err
		}
		previousBinary, err = readAt(share, "bin/eva-md", 128<<20)
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		} else if err == nil && !owned {
			return result, errors.New("install: refusing to replace an unmanaged binary")
		}
		if err != nil {
			return result, err
		}
	}
	for _, p := range plans {
		p.changed = true
		if share != nil {
			p.changed, err = treeChanged(share, filepath.Join("marketplace", p.host), p.files)
			if err != nil {
				return result, err
			}
		}
		if p.before.Installed && previousBinary == nil {
			return result, errors.New("install: installed plugin has no owned binary; repair registration first")
		}
		if p.changed {
			result.Actions = append(result.Actions, p.host+": render generated marketplace")
		} else {
			result.Actions = append(result.Actions, p.host+": marketplace bytes unchanged")
		}
		if p.changed || !p.before.Installed {
			result.Actions = append(result.Actions, p.host+": install and verify through native CLI")
		}
		if p.host == "codex" && (p.changed || !p.before.Installed) {
			result.NeedsTrust = true
		}
	}
	if !bytes.Equal(binary, previousBinary) {
		result.Actions = append(result.Actions, "replace stable binary")
	}
	result.Actions = append(result.Actions, "managed hook policy is verified by host behavior and hook heartbeats")
	if opts.DryRun {
		return result, nil
	}
	// Serialize installation with uninstall. Recheck snapshots under the lock
	// so two simultaneous installs cannot both claim the same registration.
	release, err := lock(ctx, opts.Dirs.Locks())
	if err != nil {
		return result, err
	}
	defer release()
	for _, p := range plans {
		now, e := inspect(ctx, opts.Dirs, p.host, run)
		if e != nil {
			return result, e
		}
		if now != p.before {
			return result, errors.New("install: host state changed during preflight; rerun install")
		}
	}
	if share == nil {
		share, err = paths.OpenRoot(opts.Dirs.Share, true)
		if err != nil {
			return result, err
		}
		defer share.Close()
	}
	if err = mkdirAll(share, "bin"); err != nil {
		return result, err
	}
	// Recheck filesystem under the lock too.
	for _, p := range plans {
		changed, e := treeChanged(share, filepath.Join("marketplace", p.host), p.files)
		if e != nil {
			return result, e
		}
		if changed != p.changed {
			return result, errors.New("install: generated files changed during preflight")
		}
	}
	current, e := readAt(share, "bin/eva-md", 128<<20)
	if e != nil && !errors.Is(e, fs.ErrNotExist) {
		return result, e
	}
	if !bytes.Equal(current, previousBinary) {
		return result, errors.New("install: binary changed during preflight")
	}
	if err = paths.WriteFileAtomicAt(ctx, share, installMarker, []byte(installMarkerValue), 0o600); err != nil {
		return result, err
	}
	mutated := false
	defer func() {
		if err == nil {
			for _, p := range plans {
				if p.swap != nil {
					_ = p.swap.finish()
				}
			}
			return
		}
		if !mutated {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if rollbackErr := rollback(rollbackCtx, opts.Dirs, share, plans, previousBinary, owned, run); rollbackErr != nil {
			err = fmt.Errorf("%w; rollback incomplete: %v; generated files were kept for recovery", err, rollbackErr)
		}
	}()
	mutated = true
	if !bytes.Equal(binary, previousBinary) {
		if err = paths.WriteFileAtomicAt(ctx, share, "bin/eva-md", binary, 0o700); err != nil {
			return result, err
		}
	}
	for _, p := range plans {
		if p.changed {
			p.swap, err = swapFolder(ctx, share, filepath.Join("marketplace", p.host), p.files)
			if err != nil {
				return result, err
			}
		}
		if p.host == "claude" {
			for _, path := range []string{marketplacePath(opts.Dirs.Share, p.host), pluginPath(opts.Dirs.Share, p.host)} {
				if _, err = run(ctx, p.host, "plugin", "validate", "--strict", "--json", path); err != nil {
					return result, err
				}
			}
		}
		if !p.changed && p.before.Valid {
			continue
		}
		p.registrationAttempted = true
		if !p.before.Marketplace {
			if _, err = run(ctx, p.host, "plugin", "marketplace", "add", marketplacePath(opts.Dirs.Share, p.host), "--json"); err != nil {
				return result, err
			}
		}
		if _, err = run(ctx, p.host, addArgs(p.host)...); err != nil {
			return result, err
		}
		status, e := inspect(ctx, opts.Dirs, p.host, run)
		if e != nil {
			err = e
			return result, err
		}
		if !status.Valid {
			err = fmt.Errorf("install: %s verification failed: %s", p.host, status.Reason)
			return result, err
		}
	}
	if result.NeedsTrust {
		state, e := paths.OpenRoot(opts.Dirs.State, true)
		if e != nil {
			err = e
			return result, err
		}
		defer state.Close()
		err = paths.WriteFileAtomicAt(ctx, state, "needs-trust.json", []byte("{\"host\":\"codex\",\"needs_trust\":true}\n"), 0o600)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func selectedHosts(host string) ([]string, error) {
	switch host {
	case "", "all":
		return []string{"claude", "codex"}, nil
	case "claude", "codex":
		return []string{host}, nil
	default:
		return nil, errors.New("install: --host must be claude, codex or all")
	}
}

func plainFeature(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func addArgs(host string) []string {
	if host == "claude" {
		return []string{"plugin", "install", "eva-md@eva-md", "--scope", "user", "--json"}
	}
	return []string{"plugin", "add", "eva-md@eva-md", "--json"}
}
func removeArgs(host string) []string {
	if host == "claude" {
		return []string{"plugin", "uninstall", "eva-md@eva-md", "--scope", "user", "--json"}
	}
	return []string{"plugin", "remove", "eva-md@eva-md", "--json"}
}
func marketRemoveArgs(host string) []string {
	args := []string{"plugin", "marketplace", "remove", "eva-md"}
	if host == "claude" {
		args = append(args, "--scope", "user")
	}
	return append(args, "--json")
}

func rollback(ctx context.Context, dirs paths.Dirs, root *os.Root, plans []*hostPlan, previous []byte, owned bool, run runner) error {
	for i := len(plans) - 1; i >= 0; i-- {
		p := plans[i]
		if !p.registrationAttempted {
			continue
		}
		live, err := inspect(ctx, dirs, p.host, run)
		if err != nil {
			return err
		}
		if !p.before.Installed && live.Installed {
			if _, err = run(ctx, p.host, removeArgs(p.host)...); err != nil {
				return err
			}
		}
		if !p.before.Marketplace && live.Marketplace {
			if _, err = run(ctx, p.host, marketRemoveArgs(p.host)...); err != nil {
				return err
			}
		}
		reconciled, err := inspect(ctx, dirs, p.host, run)
		if err != nil {
			return err
		}
		if reconciled.Installed != p.before.Installed || reconciled.Marketplace != p.before.Marketplace {
			return errors.New("native rollback did not restore prior registrations")
		}
	}
	for i := len(plans) - 1; i >= 0; i-- {
		if plans[i].swap != nil {
			if err := plans[i].swap.restore(); err != nil {
				return err
			}
		}
	}
	if previous != nil {
		if err := paths.WriteFileAtomicAt(ctx, root, "bin/eva-md", previous, 0o700); err != nil {
			return err
		}
	} else {
		if err := root.Remove("bin/eva-md"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, p := range plans {
		if p.before.Installed && p.registrationAttempted {
			if _, err := run(ctx, p.host, addArgs(p.host)...); err != nil {
				return err
			}
			reconciled, err := inspect(ctx, dirs, p.host, run)
			if err != nil {
				return err
			}
			if !reconciled.Valid {
				return errors.New("previous plugin could not be verified after rollback")
			}
		}
	}
	if !owned {
		if err := root.Remove(installMarker); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

type folderSwap struct {
	root           *os.Root
	target, backup string
	had            bool
}

func swapFolder(ctx context.Context, root *os.Root, target string, files map[string][]byte) (*folderSwap, error) {
	if err := mkdirAll(root, filepath.Dir(target)); err != nil {
		return nil, err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	tmp := target + ".new-" + hex.EncodeToString(nonce[:])
	backup := target + ".old-" + hex.EncodeToString(nonce[:])
	if err := root.Mkdir(tmp, 0o700); err != nil {
		return nil, err
	}
	defer root.RemoveAll(tmp)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := mkdirAll(root, filepath.Join(tmp, filepath.Dir(name))); err != nil {
			return nil, err
		}
		if err := paths.WriteFileAtomicAt(ctx, root, filepath.Join(tmp, name), files[name], 0o600); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := &folderSwap{root: root, target: target, backup: backup}
	if _, err := root.Lstat(target); err == nil {
		if err = root.Rename(target, backup); err != nil {
			return nil, err
		}
		s.had = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := root.Rename(tmp, target); err != nil {
		if s.had {
			_ = root.Rename(backup, target)
		}
		return nil, err
	}
	if err := paths.SyncDirAt(root, filepath.Dir(target)); err != nil {
		_ = s.restore()
		return nil, err
	}
	return s, nil
}
func (s *folderSwap) restore() error {
	if err := s.root.RemoveAll(s.target); err != nil {
		return err
	}
	if s.had {
		if err := s.root.Rename(s.backup, s.target); err != nil {
			return err
		}
	}
	return paths.SyncDirAt(s.root, filepath.Dir(s.target))
}
func (s *folderSwap) finish() error {
	if s.had {
		return s.root.RemoveAll(s.backup)
	}
	return nil
}

func treeChanged(root *os.Root, base string, expected map[string][]byte) (bool, error) {
	info, err := root.Lstat(base)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, errors.New("install: marketplace is not a real directory")
	}
	owned, err := hasMarker(root, filepath.Join(base, managed), managedContents)
	if err != nil {
		return false, err
	}
	if !owned {
		return false, errors.New("install: refusing an unmanaged marketplace folder")
	}
	changed := false
	allowedDirs := map[string]bool{base: true}
	for name := range expected {
		for dir := filepath.Dir(filepath.Join(base, name)); dir != base && dir != "."; dir = filepath.Dir(dir) {
			allowedDirs[dir] = true
		}
	}
	err = fs.WalkDir(root.FS(), base, func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("install: symlink in marketplace")
		}
		if d.IsDir() {
			if !allowedDirs[name] {
				return errors.New("install: unexpected directory in managed marketplace")
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("install: nonregular marketplace file")
		}
		rel, _ := filepath.Rel(base, name)
		want, ok := expected[filepath.ToSlash(rel)]
		if !ok {
			return errors.New("install: unexpected file in managed marketplace; refusing to overwrite it")
		}
		actual, e := readAt(root, name, 2<<20)
		if e != nil {
			return e
		}
		if !bytes.Equal(actual, want) {
			changed = true
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	for name := range expected {
		if _, e := root.Lstat(filepath.Join(base, name)); errors.Is(e, fs.ErrNotExist) {
			changed = true
		} else if e != nil {
			return false, e
		}
	}
	return changed, nil
}
func hasMarker(root *os.Root, name, want string) (bool, error) {
	data, err := readAt(root, name, 1024)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(data) != want {
		return false, errors.New("install: ownership marker is invalid")
	}
	return true, nil
}
func mkdirAll(root *os.Root, name string) error {
	current := ""
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		if part == ".." {
			return errors.New("install: invalid relative path")
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if err = root.Mkdir(current, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			info, err = root.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("install: parent is not a real directory")
		}
	}
	return nil
}
func readRegular(name string, limit int64) ([]byte, error) {
	root, err := paths.OpenRoot(filepath.Dir(name), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readAt(root, filepath.Base(name), limit)
}
func readAt(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("install: file is not regular")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("install: file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("install: file exceeds size limit")
	}
	return data, nil
}
func lock(ctx context.Context, dir string) (func(), error) {
	r, err := paths.OpenRoot(dir, true)
	if err != nil {
		return nil, err
	}
	f, err := r.OpenFile("install.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	r.Close()
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("install: invalid lock file")
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// Uninstall previews by default. Data/config/wiki are never removed. If a
// native step fails, keep generated files so remaining registrations still work.
func Uninstall(ctx context.Context, dirs paths.Dirs, apply bool) (Result, error) {
	return uninstall(ctx, dirs, apply, native)
}
func uninstall(ctx context.Context, dirs paths.Dirs, apply bool, run runner) (result Result, err error) {
	result.Binary = filepath.Join(dirs.Share, "bin", "eva-md")
	root, err := paths.OpenRoot(dirs.Share, false)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer root.Close()
	owned, err := hasMarker(root, installMarker, installMarkerValue)
	if err != nil {
		return result, err
	}
	if !owned {
		for _, name := range []string{"bin/eva-md", "marketplace/claude", "marketplace/codex"} {
			if _, e := root.Lstat(name); !errors.Is(e, fs.ErrNotExist) {
				return result, errors.New("install: refusing to uninstall an unmanaged installation")
			}
		}
		return result, nil
	}
	var plans []*hostPlan
	for _, host := range []string{"claude", "codex"} {
		files, e := render(host, result.Binary)
		if e != nil {
			return result, e
		}
		_, e = treeChanged(root, filepath.Join("marketplace", host), files)
		if e != nil {
			return result, e
		}
		s, e := inspect(ctx, dirs, host, run)
		if e != nil {
			return result, e
		}
		if !s.Available {
			if _, e = root.Lstat(filepath.Join("marketplace", host)); e == nil {
				return result, fmt.Errorf("install: %s CLI missing; cannot safely unregister its marketplace", host)
			}
			continue
		}
		plans = append(plans, &hostPlan{host: host, before: s})
		if s.Installed || s.Marketplace {
			result.Hosts = append(result.Hosts, host)
			result.Actions = append(result.Actions, host+": remove eva-md plugin and marketplace registration")
		}
	}
	result.Actions = append(result.Actions, "delete owned marketplace folders and binary; retain config, inbox and wiki")
	if !apply {
		return result, nil
	}
	release, err := lock(ctx, dirs.Locks())
	if err != nil {
		return result, err
	}
	defer release()
	for _, p := range plans {
		now, e := inspect(ctx, dirs, p.host, run)
		if e != nil {
			return result, e
		}
		if now != p.before {
			return result, errors.New("install: host state changed during uninstall preflight")
		}
	}
	for _, p := range plans {
		if p.before.Installed {
			if _, err = run(ctx, p.host, removeArgs(p.host)...); err != nil {
				return result, err
			}
		}
		if p.before.Marketplace {
			if _, err = run(ctx, p.host, marketRemoveArgs(p.host)...); err != nil {
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
	for _, p := range plans {
		if err = root.RemoveAll(filepath.Join("marketplace", p.host)); err != nil {
			return result, err
		}
	}
	if err = root.Remove("bin/eva-md"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return result, err
	}
	if err = root.Remove(installMarker); err != nil {
		return result, err
	}
	state, e := paths.OpenRoot(dirs.State, false)
	if e == nil {
		_ = state.Remove("needs-trust.json")
		state.Close()
	}
	return result, nil
}
