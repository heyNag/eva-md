//go:build race

package conformance

// raceOn is set when the tests run under the race detector, which slows
// the model many times over.
const raceOn = true
