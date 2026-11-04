//go:build evamd_testlimits

package main

// A build with the evamd_testlimits tag is the end-to-end tests' release
// stand-in, never a release: it starts with the limits the tests raise, and
// its setup takes the machine for a Mac, as the tests' own setup does (see
// hostOS), so the scripts' tests run on Linux too.
func init() {
	raiseTimeLimits()
	hostOS = "darwin"
}
