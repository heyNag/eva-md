// Package install owns eva-md's stable binary and generated local marketplaces.
// Host registration uses native commands; no host settings or trust records are
// read or written directly.
package install

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/paths"
)

type Options struct {
	Dirs   paths.Dirs
	Binary string
	Host   string // claude, codex, all; empty means all available hosts
	DryRun bool
	// Preflight is a dry run that needs no config yet. Setup runs it in its
	// checks of this Mac, before it makes the wiki, so that whatever install
	// would refuse stops setup before anything is made.
	Preflight bool
}
type Result struct {
	Binary     string
	Hosts      []string
	Actions    []string
	NeedsTrust bool
	// Link is the link on PATH to the stable binary, ~/.local/bin/eva-md,
	// when install made it or found it; empty when the binary is named by
	// its full path.
	Link string
}

const installMarker = ".eva-md-install"
const installMarkerValue = "eva-md installation v1\n"

// cliPathsFile, in the state folder, holds the absolute path of each host CLI
// install found, by command name, which the update job starts in preference
// to a PATH lookup: a job a hook spawns gets the host app's PATH, which may
// lack the CLI.
const cliPathsFile = "cli-paths.json"

type hostPlan struct {
	host                  string
	cli                   string // the CLI's absolute path, as PATH found it
	before                Status
	files                 map[string][]byte
	changed               bool
	registrationAttempted bool
	swap                  *folderSwap
}

// Run verifies ownership and host state before replacing any generated files.
// A failed native command is reconciled through inventories before rollback.
func Run(ctx context.Context, opts Options) (Result, error) { return runInstall(ctx, opts, native) }

// notifyInterrupt is signal.NotifyContext. Tests wrap it before any test
// runs, so a test can wait until an install has noticed an interrupt.
var notifyInterrupt = signal.NotifyContext

// RollbackTimeout bounds the rollback of a failed install. Tests raise it
// before any test runs.
var RollbackTimeout = 90 * time.Second

// ErrHooksOff marks install's refusal of a Codex whose hooks feature is off,
// as a managed Mac may set it: eva-md's hooks would never run there.
var ErrHooksOff = errors.New("install: Codex's hooks feature is off")

// hooksOff is that refusal with both ways out: turning the feature on, or
// installing for Claude Code only, which setup and upgrades read from
// EVA_MD_HOSTS in the answers file and install itself from --host. When PATH
// has no claude command, looked for as inspect does, whatever --host chose,
// it says to install Claude Code first.
func hooksOff(dirs paths.Dirs) error {
	only := "install for Claude Code only"
	if claudeCode, _ := adapter.ByID(string(adapter.ClaudeCode)); claudeCode.FindCLI(nil) == "" {
		only = "install Claude Code, then install for Claude Code only"
	}
	return fmt.Errorf("%w; turn it on with codex features enable hooks, or %s: set EVA_MD_HOSTS=claude in %s for setup and upgrades, or pass --host claude", ErrHooksOff, only, dirs.AnswersFile())
}

// Command is how a person runs the stable binary on this Mac, as setup's
// closing lines name it: eva-md when the link on PATH reaches it (see
// linkPlan), and its full path otherwise.
func Command(dirs paths.Dirs) string {
	binary := filepath.Join(dirs.Share, "bin", "eva-md")
	if _, linked := linkPlan(LinkPath(dirs), binary); linked {
		return "eva-md"
	}
	return binary
}

// FailedCommand names the host command whose failure err is, when it is
// one, so that a caller can say to run it for the host's own words, which
// install never shows.
func FailedCommand(err error) (string, bool) {
	var e *hostError
	if !errors.As(err, &e) {
		return "", false
	}
	command, ok := strings.CutSuffix(strings.TrimPrefix(e.msg, "install: "), " failed")
	return command, ok
}

func runInstall(ctx context.Context, opts Options, run runner) (result Result, err error) {
	// Ctrl-C or SIGTERM fails the next step, so the rollback below still runs.
	ctx, stop := notifyInterrupt(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	run = finishStarted(run)
	result.Binary = filepath.Join(opts.Dirs.Share, "bin", "eva-md")
	if opts.Preflight {
		opts.DryRun = true
	}
	// The planned actions are printed with a failed run's error, so an error
	// before the first change says that none of them ran.
	mutated := false
	defer func() {
		if err != nil && !mutated && !opts.DryRun && len(result.Actions) > 0 {
			err = fmt.Errorf("%w; none of the planned actions ran", err)
		}
	}()
	// Setup's checks run before there is a config; every other run needs it.
	if !opts.Preflight {
		if _, err = config.LoadContext(ctx, opts.Dirs.ConfigFile()); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return result, errors.New("install: no wiki is set up; run eva-md setup first")
			}
			return result, errors.New("install: the eva-md config can't be read; run eva-md doctor")
		}
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
		// Rendering the folder again would bring back whatever Codex kept of
		// the plugin, disabled or not, which no rollback could tell apart from
		// this run's own, so uninstall clears the registration first.
		if status.FolderMissing {
			return result, errors.New("install: Codex has eva-md's marketplace registered, but its folder is missing; run eva-md uninstall --apply, or the uninstall script with --apply, then install eva-md again")
		}
		if !status.Available {
			if opts.Host == host {
				return result, fmt.Errorf("install: %s CLI not found", host)
			}
			result.Actions = append(result.Actions, host+": CLI not found; skipped")
			continue
		}
		if status.Installed && !status.Valid {
			return result, fmt.Errorf("install: existing %s eva-md registration needs attention: %s; run eva-md uninstall --apply, or the uninstall script with --apply, then install eva-md again", host, status.Reason)
		}
		files, err := render(host, result.Binary)
		if err != nil {
			return result, err
		}
		h := hostFor(host)
		plans = append(plans, &hostPlan{host: host, cli: h.FindCLI(nil), before: status, files: files})
		result.Hosts = append(result.Hosts, host)
		if args := h.Plugin().FeaturesArgs; args != nil {
			features, err := run(ctx, h.Names.CLI, args...)
			if err != nil {
				return result, err
			}
			if adapter.ReadHooksRows(features).Off() {
				return result, hooksOff(opts.Dirs)
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
	// A symlinked executable, such as one on PATH, is read from its real file.
	if opts.Binary, err = filepath.EvalSymlinks(opts.Binary); err != nil {
		return result, fmt.Errorf("install: read executable: %w", err)
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
			return result, fmt.Errorf("install: refusing to replace an unmanaged binary, %s; move it aside", result.Binary)
		}
		if err != nil {
			return result, err
		}
	}
	// reviewHost is the host whose changed plugin needs a review in it.
	reviewHost := ""
	for _, p := range plans {
		p.changed = true
		if share != nil {
			p.changed, err = treeChanged(share, filepath.Join("marketplace", p.host), p.files, false)
			if err != nil {
				return result, err
			}
		}
		// A missing binary is restored only under eva-md's own install marker.
		// A plugin folder that differs is rendered again, as in any upgrade.
		if p.before.Installed && previousBinary == nil && !owned {
			return result, errors.New("install: installed plugin has no owned binary; run eva-md uninstall --apply first")
		}
		if p.changed {
			result.Actions = append(result.Actions, p.host+": render generated marketplace")
		} else {
			result.Actions = append(result.Actions, p.host+": marketplace bytes unchanged")
		}
		if p.changed || !p.before.Installed {
			result.Actions = append(result.Actions, p.host+": install and verify through native CLI")
		}
		if hostFor(p.host).Plugin().NeedsReview && (p.changed || !p.before.Installed) {
			result.NeedsTrust = true
			reviewHost = p.host
		}
	}
	if !bytes.Equal(binary, previousBinary) {
		result.Actions = append(result.Actions, "replace stable binary")
	}
	link := LinkPath(opts.Dirs)
	wantLink, linked := linkPlan(link, result.Binary)
	if wantLink && !linked {
		result.Actions = append(result.Actions, "link "+link+" to the stable binary")
	}
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
	// Recheck filesystem under the lock too, and remove OS metadata files.
	for _, p := range plans {
		changed, e := treeChanged(share, filepath.Join("marketplace", p.host), p.files, true)
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
		rollbackCtx, cancel := context.WithTimeout(context.Background(), RollbackTimeout)
		defer cancel()
		if rollbackErr := rollback(rollbackCtx, opts.Dirs, share, plans, previousBinary, owned, run); rollbackErr != nil {
			err = fmt.Errorf("%w; rollback incomplete: %v; generated files were kept for recovery", err, rollbackErr)
		} else {
			err = fmt.Errorf("%w; this run's changes were rolled back", err)
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
		if validate := hostFor(p.host).Plugin().ValidateArgs; validate != nil {
			for _, path := range []string{marketplacePath(opts.Dirs.Share, p.host), pluginPath(opts.Dirs.Share, p.host)} {
				if _, err = run(ctx, command(p.host), slices.Concat(validate, []string{path})...); err != nil {
					return result, err
				}
			}
		}
		if !p.changed && p.before.Valid {
			continue
		}
		p.registrationAttempted = true
		if !p.before.Marketplace {
			if _, err = run(ctx, command(p.host), "plugin", "marketplace", "add", marketplacePath(opts.Dirs.Share, p.host), "--json"); err != nil {
				return result, err
			}
		}
		// A host may refuse to add a plugin it has. It loads the plugin's
		// folder in place, so a registration that still verifies is enough.
		_, addErr := run(ctx, command(p.host), addArgs(p.host)...)
		if addErr != nil && !p.before.Installed {
			err = addErr
			return result, err
		}
		status, e := inspect(ctx, opts.Dirs, p.host, run)
		if e != nil {
			err = e
			return result, err
		}
		if !status.Valid {
			err = fmt.Errorf("install: %s verification failed: %s", p.host, status.Reason)
			if addErr != nil {
				err = addErr
			}
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
		err = paths.WriteFileAtomicAt(ctx, state, "needs-trust.json", []byte("{\"host\":\""+reviewHost+"\",\"needs_trust\":true}\n"), 0o600)
		if err != nil {
			return result, err
		}
	}
	recordCLIs(ctx, opts.Dirs, plans)
	// The link is made last, once the stable binary works; a link that
	// can't be made leaves the binary named by its full path.
	if wantLink && !linked {
		if e := makeLink(link, result.Binary); e != nil {
			result.Actions = append(result.Actions, "left "+link+" as it was: "+e.Error())
			wantLink = false
		}
	}
	items := installedItems(opts.Dirs, result.Binary, result.Hosts)
	if wantLink {
		result.Link = link
		items = append(items, RecordItem{Kind: KindPathLink, Path: link, Value: result.Binary})
	}
	if e := addToRecord(ctx, share, opts.Dirs, items); e != nil {
		result.Actions = append(result.Actions, "couldn't write the install record: "+e.Error())
	}
	return result, nil
}

// recordCLIs saves the CLI paths of the hosts this run installed in
// <state>/cli-paths.json, keeping another host's. It fails nothing: a path
// that isn't saved leaves the update job to look the CLI up on PATH.
func recordCLIs(ctx context.Context, dirs paths.Dirs, plans []*hostPlan) {
	state, err := paths.OpenRoot(dirs.State, true)
	if err != nil {
		return
	}
	defer state.Close()
	var clis map[string]string
	if data, err := readAt(state, cliPathsFile, 64<<10); err == nil {
		_ = json.Unmarshal(data, &clis)
	}
	if clis == nil {
		clis = map[string]string{}
	}
	for _, p := range plans {
		if p.cli != "" {
			clis[p.host] = p.cli
		}
	}
	if data, err := json.Marshal(clis); err == nil {
		_ = paths.WriteFileAtomicAt(ctx, state, cliPathsFile, append(data, '\n'), 0o600)
	}
}

// selectedHosts gives the install names --host chooses: every host, in the
// adapter's order, for "" or all, and otherwise the one it names.
func selectedHosts(host string) ([]string, error) {
	if host == "" || host == "all" {
		var names []string
		for _, h := range adapter.All() {
			names = append(names, h.Names.InstallName)
		}
		return names, nil
	}
	if _, ok := adapter.ByInstallName(host); ok {
		return []string{host}, nil
	}
	return nil, errors.New("install: --host must be claude, codex or all")
}

func addArgs(host string) []string    { return hostFor(host).Plugin().AddArgs }
func removeArgs(host string) []string { return hostFor(host).Plugin().RemoveArgs }

// marketRemoveArgs removes the eva-md marketplace. Claude Code 2.1.316 keeps
// listing a marketplace removed with --scope while another scope declares it,
// so it is removed from every scope; run from / (see native), that is the
// user's settings and Claude Code's own list.
func marketRemoveArgs(host string) []string {
	return []string{"plugin", "marketplace", "remove", "eva-md", "--json"}
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
			if _, err = run(ctx, command(p.host), removeArgs(p.host)...); err != nil {
				return err
			}
		}
		if !p.before.Marketplace && live.Marketplace {
			if _, err = run(ctx, command(p.host), marketRemoveArgs(p.host)...); err != nil {
				return err
			}
		}
		reconciled, err := inspect(ctx, dirs, p.host, run)
		if err != nil {
			return err
		}
		// A plugin a failed upgrade removed is re-added below, after the old
		// files are back, so only new registrations are checked here.
		if reconciled.Installed && !p.before.Installed || reconciled.Marketplace && !p.before.Marketplace {
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
			_, addErr := run(ctx, command(p.host), addArgs(p.host)...)
			reconciled, err := inspect(ctx, dirs, p.host, run)
			if err != nil {
				return err
			}
			if !reconciled.Valid {
				if addErr != nil {
					return addErr
				}
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

// finishStarted lets a host command that has started run to the end of its
// own timeout when ctx is cancelled, since killing it could leave the host's
// plugin records half written, and starts none once ctx is done.
func finishStarted(run runner) runner {
	return func(ctx context.Context, host string, args ...string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return run(context.WithoutCancel(ctx), host, args...)
	}
}

// treeChanged ignores OS metadata files such as .DS_Store, and removes them
// when clean is set.
func treeChanged(root *os.Root, base string, expected map[string][]byte, clean bool) (bool, error) {
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
		return false, fmt.Errorf("install: refusing an unmanaged marketplace folder, %s; move it aside", filepath.Join(root.Name(), base))
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
				return fmt.Errorf("install: unexpected directory %s in managed marketplace; move it out of that folder", filepath.Join(root.Name(), name))
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("install: nonregular marketplace file")
		}
		rel, _ := filepath.Rel(base, name)
		want, ok := expected[filepath.ToSlash(rel)]
		if !ok && (d.Name() == ".DS_Store" || strings.HasPrefix(d.Name(), "._")) {
			if clean {
				return root.Remove(name)
			}
			return nil
		}
		if !ok {
			return fmt.Errorf("install: unexpected file %s in managed marketplace; move it out of that folder", filepath.Join(root.Name(), name))
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
	f, err := paths.OpenOrCreate(r, "install.lock", os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
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
