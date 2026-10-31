package hook

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/paths"
)

// The jobs maintain runs as: the day's commit, push and lint, and the retry
// of a pending push.
var (
	dailyJob = []string{"maintain"}
	retryJob = []string{"maintain", "--push-only"}
)

// retryEvery spaces the push retries chat starts spawn.
const retryEvery = 30 * time.Minute

// maintainOn reports whether the hooks start maintain: a wiki set up by
// init, its path and instance in the config, without which maintain could
// only be refused.
func maintainOn(c config.Config) bool { return c.WikiPath != "" && c.Instance != "" }

// queueMaintain asks for the day's maintain, and at a chat start for a push
// retry while commits are on. Asking costs the hook nothing here: launch
// decides, once the output is written, whether each is due.
func queueMaintain(spawns *[][]string, env Env, start bool) {
	if !maintainOn(env.Config) {
		return
	}
	*spawns = append(*spawns, dailyJob)
	if start && env.Config.Commits {
		*spawns = append(*spawns, retryJob)
	}
}

// due reports whether launch starts args now. An update always is. The
// day's maintain is due for the day's first event, the one whose open
// creates <state>/commit/attempt-<YYYY-MM-DD>; every later event that day
// finds the file and starts nothing. A push retry is due when no maintain
// started with it and retryDue says so. started records whether this hook
// started the day's maintain.
func due(env Env, args []string, now time.Time, started *bool) bool {
	switch {
	case slices.Equal(args, dailyJob):
		*started = firstOfDay(env.Dirs.State, now)
		return *started
	case slices.Equal(args, retryJob):
		return !*started && retryDue(env.Dirs, now)
	}
	return true
}

// firstOfDay creates the day's attempt marker, and reports whether this
// call created it: one open with O_EXCL, and the folder made once, by the
// first event ever.
func firstOfDay(state string, now time.Time) bool {
	file := filepath.Join(state, "commit", "attempt-"+now.Format(time.DateOnly))
	err := create(file, os.O_EXCL)
	if errors.Is(err, fs.ErrNotExist) && paths.EnsureDir(filepath.Dir(file)) == nil {
		err = create(file, os.O_EXCL)
	}
	return err == nil
}

// retryDue reports whether a push retry is due: the newest commit record's
// push pending, as a check finds it at most every 30 minutes, by the
// modification time of <state>/commit/push-retry, which every check sets to
// now, due or not. Between checks a chat start costs one stat; a check adds
// a read of the newest commit records.
func retryDue(dirs paths.Dirs, now time.Time) bool {
	marker := filepath.Join(dirs.State, "commit", "push-retry")
	if info, err := os.Lstat(marker); err == nil {
		if age := now.Sub(info.ModTime()); age >= 0 && age < retryEvery {
			return false
		}
	}
	pending := health.RetryPush(dirs, now)
	if paths.EnsureDir(filepath.Dir(marker)) != nil || create(marker, 0) != nil || os.Chtimes(marker, now, now) != nil {
		return false
	}
	return pending
}

// create opens file for writing, creating it, never through a symlink, and
// closes it.
func create(file string, flag int) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|syscall.O_NOFOLLOW|flag, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}
