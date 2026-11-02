//go:build evamd_testlimits

package main

// A build with the evamd_testlimits tag is the end-to-end tests' release
// stand-in, never a release: it starts with the limits the tests raise.
func init() { raiseTimeLimits() }
