//go:build race

package redact

// raceOn is set when the tests run under the race detector, which slows the
// rules many times over.
const raceOn = true
