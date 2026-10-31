package wikigit

import "time"

// SetWikiLockWait sets how long a commit waits for the wiki lock, for a
// test, and returns a function that restores the wait.
func SetWikiLockWait(d time.Duration) (restore func()) {
	old := wikiLockWait
	wikiLockWait = d
	return func() { wikiLockWait = old }
}
