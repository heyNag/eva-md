package main

import (
	"time"

	"github.com/heyNag/eva-md/internal/mcp"
	"github.com/heyNag/eva-md/internal/testlimits"
)

// raiseTimeLimits raises the time limits tests raise: this package's own
// and, through testlimits, the other packages' exported ones, and it
// lengthens the hook's and the MCP server's. TestMain calls it before
// any test runs. A binary built with the evamd_testlimits tag, as the
// release stand-in of the end-to-end tests is, calls it as it starts, so
// the setup, install and uninstall those tests run in child processes have
// the same limits. A release build never calls it.
func raiseTimeLimits() {
	testlimits.Raise()
	hookSlack = 30 * time.Second
	mcp.Slack = time.Minute
	resumeTimeout, installTimeout, checkTimeout = testlimits.Long, testlimits.Long, testlimits.Long
	passTime, showTime, showCall = testlimits.Long, testlimits.Long, testlimits.Long
	setupGitTimeout, peekCloneTimeout, signingTimeout = testlimits.Long, testlimits.Long, testlimits.Long
	repoCLITimeout, repoCLIWaitDelay, probeTimeout = testlimits.Long, testlimits.Long, testlimits.Long
	versionTimeout = testlimits.Long
}
