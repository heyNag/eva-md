package v1arm

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
)

// within is ctx limited to d, by the arm's limit.
func (a *arm) within(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if a.limit != nil {
		return a.limit(ctx, d)
	}
	return context.WithTimeout(ctx, d)
}

// cappedBuffer keeps the first limit bytes written to it.
type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := b.limit - b.buffer.Len(); len(p) > room {
		p = p[:max(room, 0)]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

// errLimit is a command's error when its limit, or the run's end, ended it.
var errLimit = errors.New("the command ran past its limit")

// run runs name with args in dir, with exactly the environment env and
// stdin on its standard input, in a process group of its own that is
// killed when d has passed (d 0: when ctx ends) and as soon as name has
// exited, and gives the first 64 KiB it printed on stdout. Its stderr is
// dropped. An exit other than 0 is an error; none of its output is in any
// error, whose words are fixed.
func (a *arm) run(ctx context.Context, d time.Duration, name string, args, env []string, dir string, stdin []byte) ([]byte, error) {
	if d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = a.within(ctx, d)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(stdin)
	out := &cappedBuffer{limit: outputLimit}
	cmd.Stdout = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = drive.WaitDelay
	err := cmd.Start()
	if err == nil {
		err = cmd.Wait()
		_ = killGroup(cmd.Process.Pid)
	}
	switch {
	case ctx.Err() != nil:
		return nil, errLimit
	case err != nil:
		return nil, errors.New("the command failed")
	}
	return out.buffer.Bytes(), nil
}

// killGroup kills the process group pid leads.
func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// The locks of eva-md's two jobs, in its state folder's locks/.
const (
	updateLock   = "update.lock"
	maintainLock = "maintain.lock"
)

// held reports whether a process holds the lock file name in dir: one
// that is there and that an exclusive lock, asked for without waiting,
// cannot take. A lock taken is let go at once; a job that tries for it
// meanwhile leaves as busy, as it would beside any other job. A lock that
// is not there, or not a regular file, is held by no job.
func held(dir, name string) (bool, error) {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ELOOP):
		return false, nil
	case err != nil:
		return false, errors.New("a lock of eva-md's cannot be read")
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return false, nil
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return true, nil
		}
		return false, errors.New("a lock of eva-md's cannot be read")
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}

// waitFree waits while a process holds one of the locks named in the
// trial's eva-md state, until ctx ends.
func (a *arm) waitFree(ctx context.Context, t drive.Trial, names ...string) error {
	dir := filepath.Join(stateDir(t), "locks")
	for {
		holder := ""
		for _, name := range names {
			on, err := held(dir, name)
			if err != nil {
				return err
			}
			if on {
				holder = name
				break
			}
		}
		if holder == "" {
			return nil
		}
		if a.held != nil {
			a.held(holder)
		}
		select {
		case <-ctx.Done():
			return errLimit
		case <-time.After(a.poll):
		}
	}
}

// waitJobs waits, within LockTimeout, while one of eva-md's jobs in the
// trial holds the update or the maintain lock; past it the trial is
// update_failed.
func (a *arm) waitJobs(ctx context.Context, t drive.Trial) error {
	wctx, cancel := a.within(ctx, LockTimeout)
	defer cancel()
	if err := a.waitFree(wctx, t, updateLock, maintainLock); err != nil {
		return a.failed(ctx, err, updateFailed)
	}
	return nil
}
