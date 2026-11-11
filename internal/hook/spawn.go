package hook

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// launch starts each eva-md command the hook asked for that is due (see
// due), a catch-up's sweep only while it is due (see withSweep), once its
// output is written, and never waits for one. A spawn that fails or panics
// is ignored: the next trigger tries again.
func launch(env Env, spawns [][]string) {
	defer func() { _ = recover() }()
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	started := false
	for _, args := range spawns {
		if args = withSweep(env, args, now()); args == nil || !due(env, args, now(), &started) {
			continue
		}
		if env.spawn != nil {
			env.spawn(args...)
			continue
		}
		_ = detach(env.Dirs, now(), args...)
	}
}

// detach starts the installed binary, <share>/bin/eva-md, with args as a
// job of its own: in a new session (setsid), out of reach of the signals a
// host sends the hook's process group; with stdin from /dev/null and stdout
// and stderr appended to the day's update log, so it holds none of the
// hook's pipes and the host sees the hook's output end when the hook exits;
// and with the update package's allowlisted environment and EVA_MD_INNER=1,
// which a maintain job extends with gitEnv. It starts the child and
// releases it, never waiting. A share folder that is not an absolute path
// starts nothing, so the binary is never looked up from the hook's folder.
// An update's trigger goes only to the build this hook runs (see
// sameBuild); any other gets the job without it, as every release takes it.
func detach(dirs paths.Dirs, now time.Time, args ...string) error {
	if !filepath.IsAbs(dirs.Share) {
		return errors.New("hook: eva-md is not installed")
	}
	binary := filepath.Join(dirs.Share, "bin", "eva-md")
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errors.New("hook: eva-md is not installed")
	}
	if !sameBuild(info, selfAtStart, processStart) {
		args = withoutTrigger(args)
	}
	log, err := update.OpenLog(dirs, now)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(binary, args...)
	cmd.Dir = string(filepath.Separator)
	cmd.Env = jobEnv(args)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// processStart is about when this process started: when the package was
// initialized, a moment after.
var processStart = time.Now()

// selfAtStart is the file this process was started from as it was then,
// when the package was initialized (see executable).
var selfAtStart = executable()

// settled is how long before this process started the binary it spawns
// must have been written, for sameBuild to take it for the one it runs.
// Install writes the binary, syncs it and renames it in place in far less.
const settled = time.Minute

// executable describes the file this process was started from, as its path
// finds it now, or is nil when it can't be told. On macOS the path is the
// one the process was started by, which a file put there since may answer.
func executable() os.FileInfo {
	self, err := os.Executable()
	if err != nil {
		return nil
	}
	info, err := os.Stat(self)
	if err != nil {
		return nil
	}
	return info
}

// sameBuild reports whether binary, the installed binary as info describes
// it now, is the build this process runs, which takes update.TriggerFlag:
// the file self described when this process started, unchanged since, in
// its size and modification time, and written at least settled before
// start, when this process started. Releases up to 2026.11.7 refuse the
// flag, so a binary that an install, upgrade or a downgrade replaced or
// rewrote since this hook started, whatever time it was given, one written
// just before the hook started, or a hook run from another file, gets jobs
// without it: the update runs, and records no trigger.
func sameBuild(info, self os.FileInfo, start time.Time) bool {
	return self != nil && os.SameFile(info, self) && info.Size() == self.Size() && info.ModTime().Equal(self.ModTime()) &&
		info.ModTime().Before(start.Add(-settled))
}

// withoutTrigger is args without update.TriggerFlag and its value.
func withoutTrigger(args []string) []string {
	i := slices.Index(args, update.TriggerFlag)
	if i < 0 || i+1 >= len(args) {
		return args
	}
	return slices.Delete(slices.Clone(args), i, i+2)
}

// gitEnv lists the variables a maintain job keeps beyond the update's
// allowlist, so its git commit and push find the SSH agent, the user's git
// config and GnuPG home, and an SSH command set for git: the user's own
// signing and authentication, untouched. None holds a secret.
var gitEnv = []string{"SSH_AUTH_SOCK", "GNUPGHOME", "GIT_SSH", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM", "XDG_CONFIG_HOME"}

// jobEnv is the environment of a detached job: the update's allowlist, and
// for maintain the variables of gitEnv this process has.
func jobEnv(args []string) []string {
	env := update.Environ(os.Environ())
	if len(args) == 0 || args[0] != "maintain" {
		return env
	}
	for _, key := range gitEnv {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}
