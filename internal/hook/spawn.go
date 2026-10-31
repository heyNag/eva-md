package hook

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// launch starts each eva-md command the hook asked for that is due (see
// due), once its output is written, and never waits for one. A spawn that
// fails or panics is ignored: the next trigger tries again.
func launch(env Env, spawns [][]string) {
	defer func() { _ = recover() }()
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	started := false
	for _, args := range spawns {
		if !due(env, args, now(), &started) {
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
func detach(dirs paths.Dirs, now time.Time, args ...string) error {
	if !filepath.IsAbs(dirs.Share) {
		return errors.New("hook: eva-md is not installed")
	}
	binary := filepath.Join(dirs.Share, "bin", "eva-md")
	if info, err := os.Lstat(binary); err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errors.New("hook: eva-md is not installed")
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
