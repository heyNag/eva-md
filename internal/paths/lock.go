package paths

import (
	"errors"
	"fmt"
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
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
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
