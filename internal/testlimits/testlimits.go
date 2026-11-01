// Package testlimits raises, for tests, the time limits that protect a host
// from slow git, slow host commands or a slow disk, so a test checks what
// eva-md does, never how fast the machine is. Only tests import it, from
// TestMain, before any test runs. A package raises its own unexported
// limits in its own TestMain. So do the packages this one imports, directly
// or not: their tests can't import it without an import cycle.
package testlimits

import (
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/resume"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// Long replaces every limit. It is longer than any production limit it
// replaces, so raising never shortens one.
const Long = 10 * time.Minute

// Raise sets every exported time limit to Long.
func Raise() {
	identity.Timeout, identity.WaitDelay = Long, Long
	resume.GitTimeout, resume.GitWaitDelay = Long, Long
	wikigit.GitTimeout, wikigit.CommitTimeout, wikigit.PushTimeout, wikigit.WaitDelay = Long, Long, Long, Long
	wikiinit.GitTimeout = Long
	install.HostTimeout, install.HostWaitDelay, install.RollbackTimeout = Long, Long, Long
	okfadapter.LockTimeout = Long
	update.GitTimeout, update.GitWaitDelay, update.WaitDelay = Long, Long, Long
}
