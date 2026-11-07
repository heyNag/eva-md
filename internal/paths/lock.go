package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// TryLock takes the lock file dir/name, creating both, without waiting;
// busy reports that another process holds it. It refuses a symlink or
// anything else that is not a regular file. The update job, eva-md maintain
// and the search index take their locks with it; the hooks never do.
func TryLock(dir, name string) (unlock func(), busy bool, err error) {
	root, err := OpenRoot(dir, true)
	if err != nil {
		return nil, false, fmt.Errorf("lock folder: %w", err)
	}
	defer root.Close()
	// Callers that take a new lock at once each get it or busy, never
	// ENOENT (see OpenOrCreate).
	f, err := OpenOrCreate(root, name, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("lock: %w", err)
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, false, errors.New("the lock is not a regular file")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("lock: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, false, nil
}

// OpenOrCreate opens name in root with flag, creating it with perm when it
// doesn't exist: it creates the file with O_EXCL, and opens the one there
// when that fails because it exists. On macOS, callers that create the same
// file through os.Root without O_EXCL at once can get ENOENT, so of two
// hooks or jobs that start a lock, a log or a marker at the same moment, one
// would fail. flag holds neither os.O_CREATE, os.O_EXCL nor os.O_TRUNC.
func OpenOrCreate(root *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
	f, err := root.OpenFile(name, flag|os.O_CREATE|os.O_EXCL, perm)
	if errors.Is(err, fs.ErrExist) {
		f, err = root.OpenFile(name, flag, 0)
	}
	return f, err
}
