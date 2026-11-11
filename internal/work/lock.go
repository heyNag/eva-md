package work

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/heyNag/eva-md/internal/paths"
)

// lockWait takes the lock file dir/name as paths.TryLock does, but waits
// for it until ctx ends. A wait that ctx ends leaves a goroutine that takes
// the lock when it frees and lets it go at once.
func lockWait(ctx context.Context, dir, name string) (unlock func(), err error) {
	root, err := paths.OpenRoot(dir, true)
	if err != nil {
		return nil, fmt.Errorf("work: lock folder: %w", err)
	}
	defer root.Close()
	f, err := paths.OpenOrCreate(root, name, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("work: lock: %w", err)
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("work: the lock is not a regular file")
	}
	fd := int(f.Fd())
	got := make(chan error, 1)
	go func() {
		for {
			err := syscall.Flock(fd, syscall.LOCK_EX)
			if !errors.Is(err, syscall.EINTR) {
				got <- err
				return
			}
		}
	}()
	select {
	case err := <-got:
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("work: lock: %w", err)
		}
		return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = f.Close() }, nil
	case <-ctx.Done():
		go func() {
			if <-got == nil {
				_ = syscall.Flock(fd, syscall.LOCK_UN)
			}
			_ = f.Close()
		}()
		return nil, ctx.Err()
	}
}
