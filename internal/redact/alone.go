package redact

import "sync/atomic"

// baselineOff says the baseline rules do not run at run time. Its zero
// value is v1's: both rule sets run.
var baselineOff atomic.Bool

// SetBaselineAtRunTime says whether Redact and Scan run the baseline rules
// (internal/redact/baseline) beside the current rules and remove the union
// of what both remove, as v1 does, or run the current rules alone (decision
// 7 of the plan). TestOracle holds the current rules alone to remove every
// secret of its corpus and generators that the baseline rules remove, but
// for the gap classes it records: the shapes of code the current rules keep
// on purpose and the baseline rules remove. With the baseline rules off,
// Redact keeps every secret in such a shape, which v1 removes, so they stay
// on until each shape is closed and the oracle holds the rules to it (see
// Redact). It is on until a caller turns it off; config.LoadContext sets it
// from the switch redaction_current_rules_only of every config it returns.
// It is safe for concurrent use. RedactKnown and Identifier never run the
// baseline rules and do not change with it.
func SetBaselineAtRunTime(on bool) { baselineOff.Store(!on) }

// BaselineAtRunTime reports whether Redact and Scan run the baseline rules
// beside the current rules (see SetBaselineAtRunTime).
func BaselineAtRunTime() bool { return !baselineOff.Load() }

// maxAloneRounds bounds the rounds runAlone makes, as run's rounds are
// bounded: four, each of at most maxPasses passes.
const maxAloneRounds = 4

// runAlone redacts the cleaned text c with the current rules alone: private
// key blocks first, as run does, then passes of the current rules until one
// replaces nothing, with no call of the baseline rules. It follows the
// ranges the passes replaced back to c. A round is at most maxPasses passes,
// and runAlone makes another only when the ranges could not be followed
// exactly or the passes did not settle, at most maxAloneRounds. ok is false
// when ctx ended first.
func (d driver) runAlone(c string) (r Result, ok bool) {
	t := newTrace(c)
	if d.stop(len(c)) {
		return r, false
	}
	t.privateKeys()
	stop := func() bool { return d.stop(len(c)) }
	for round := 0; round < maxAloneRounds; round++ {
		out, n, settled, ok := currentRules(t.text, stop)
		if !ok {
			return r, false
		}
		if n == 0 {
			break
		}
		t.count += t.alone(out, n)
		if settled && t.text == out {
			break
		}
	}
	return t.result(), true
}

// currentRules runs passes of the current rules on text, asking stop before
// each, until one replaces nothing, at most maxPasses. It returns the text,
// the replacements made and whether the last pass replaced nothing; ok is
// false when stop said to stop.
func currentRules(text string, stop func() bool) (out string, n int, settled, ok bool) {
	for i := 0; i < maxPasses; i++ {
		if stop() {
			return "", 0, false, false
		}
		var k int
		text, k = pass(text, mode{entropy: true})
		if k == 0 {
			return text, n, true, true
		}
		n += k
	}
	return text, n, false, true
}

// alone replaces what the current rules replaced in the trace's text, given
// their output out, which made n replacements, and returns the replacements
// it counts: the ranges that are not one marker kept as it was, or n when
// out can't be lined up with the text and is taken as it is.
func (t *trace) alone(out string, n int) int {
	rs, ok := removals(t.text, out)
	if !ok {
		t.untraced = true
		t.text = out
		return n
	}
	// Where kept text could line up in more than one place, a range runs on
	// over the next one's start; merge them, as unionRanges does.
	var merged []removal
	for _, r := range rs {
		if n := len(merged); n > 0 && r.start < merged[n-1].end {
			merged[n-1].end = max(merged[n-1].end, r.end)
			merged[n-1].tier = min(merged[n-1].tier, r.tier)
			continue
		}
		merged = append(merged, r)
	}
	k := 0
	for i, r := range merged {
		merged[i].current = true
		merged[i].keep = isMarker(t.text[r.start:r.end])
		if !merged[i].keep {
			k++
		}
	}
	if k > 0 {
		t.replace(merged)
	}
	return k
}
