package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

// OpenOrCreate creates a missing file with its mode, opens one that exists
// as it is, and gives each of many callers that start the same new file at
// once that file, never ENOENT, which a file created through os.Root without
// O_EXCL can give the caller that loses the race on macOS.
func TestOpenOrCreate(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	appendTo := func(name, text string) {
		t.Helper()
		f, err := OpenOrCreate(root, name, os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(text); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	appendTo("log.jsonl", "first\n")
	appendTo("log.jsonl", "second\n")
	if data, err := os.ReadFile(filepath.Join(dir, "log.jsonl")); err != nil || string(data) != "first\nsecond\n" {
		t.Fatalf("log.jsonl holds %q, %v", data, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "log.jsonl")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("log.jsonl: %v %v", info, err)
	}
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("new-%d.lock", i)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Go(func() {
				<-start
				f, err := OpenOrCreate(root, name, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					return
				}
				f.Close()
			})
		}
		close(start)
		wg.Wait()
		if t.Failed() {
			return
		}
	}
}
