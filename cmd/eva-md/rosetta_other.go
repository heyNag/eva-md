//go:build !darwin

package main

// rosettaTranslated is false off macOS, which has no Rosetta. eva-md runs
// only on macOS; this lets the package build and its tests run on Linux, as
// in a cloud session.
func rosettaTranslated() bool { return false }
