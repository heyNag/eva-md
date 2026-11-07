package session

import (
	"context"
	"errors"
	"github.com/heyNag/eva-md/internal/paths"
	"io/fs"
	"os"
	"syscall"
	"time"
)

// Lock serializes captures and transcript recovery for one session.
func Lock(ctx context.Context, dir, host, id string) (func(), error) {
	f, err := openLock(ctx, dir, host, id)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (func(), error) { f.Close(); return nil, err }
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return unlock(f), nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return fail(err)
		}
		timer := time.NewTimer(2 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fail(ctx.Err())
		case <-timer.C:
		}
	}
}

// TryLock takes the session's lock as Lock does, without waiting: busy
// reports that another holder, such as a hook writing the session, has it.
// The daily maintain takes it so to prune a recovery record.
func TryLock(dir, host, id string) (release func(), busy bool, err error) {
	f, err := openLock(context.Background(), dir, host, id)
	if err != nil {
		return nil, false, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	switch {
	case err == nil:
		return unlock(f), false, nil
	case errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN):
		f.Close()
		return nil, true, nil
	}
	f.Close()
	return nil, false, err
}

// openLock opens the session's lock file, <dir>/<host>-<id>.lock, creating
// it, and refuses one that isn't a regular file linked once.
func openLock(ctx context.Context, dir, host, id string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := paths.OpenRoot(dir, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name, err := paths.SafeName(host + "-" + id)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file := name + ".lock"
	f, err := root.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NONBLOCK, 0o600)
	if errors.Is(err, fs.ErrExist) {
		before, statErr := root.Lstat(file)
		if statErr != nil || !before.Mode().IsRegular() {
			return nil, errors.New("session: lock is not regular")
		}
		f, err = root.OpenFile(file, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err == nil {
			opened, statErr := f.Stat()
			if statErr != nil || !os.SameFile(before, opened) {
				f.Close()
				return nil, errors.New("session: lock changed while opening")
			}
		}
	}
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("session: lock is not regular")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
		f.Close()
		return nil, errors.New("session: multiply linked lock")
	}
	return f, nil
}

// unlock returns the release of a lock taken on f.
func unlock(f *os.File) func() {
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }
}
