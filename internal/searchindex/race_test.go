//go:build race

package searchindex

// raceOn is set when the tests run under the race detector, which slows
// them several times over.
const raceOn = true
