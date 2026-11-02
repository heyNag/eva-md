package wikiinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Init's lock is taken through paths.TryLock: a second init while one holds
// it is told another initialization is running, the lock is free again once
// released, and a lock path that is no regular file is an error naming the
// lock.
func TestInitLock(t *testing.T) {
	dir := t.TempDir()
	unlock, err := initLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initLock(dir); err == nil || err.Error() != "wikiinit: another initialization is running" {
		t.Fatalf("a second lock while the first is held: %v", err)
	}
	unlock()
	again, err := initLock(dir)
	if err != nil {
		t.Fatalf("the lock once released: %v", err)
	}
	again()
	bad := t.TempDir()
	if err := os.Mkdir(filepath.Join(bad, "init.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := initLock(bad); err == nil || !strings.Contains(err.Error(), "wikiinit: initialization lock") {
		t.Fatalf("a folder where the lock goes: %v", err)
	}
}
