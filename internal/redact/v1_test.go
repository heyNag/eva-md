package redact

import (
	"sort"
	"strings"

	"github.com/heyNag/eva-md/internal/redact/baseline"
)

// redactV1 is Redact as it was before Scan and Apply, kept word for word
// but for the names of its functions and where it gets the rule sets' output
// from, as the oracle the driver is checked against.
//
// Redact replaces secrets with [REDACTED:<kind>] and returns the new text
// and the number of replacements. It strips ANSI escapes, bidi controls and NUL
// first, and removes zero-width characters, so a secret split by them is
// still caught. Git SHAs and UUIDs are kept in ordinary text, but not as the
// value of a secret-looking key; op:// references are kept.
//
// The text removed is the union of what the baseline rules
// (internal/redact/baseline) remove and what the current rules remove, both
// judged on the same cleaned text. So no rule change can keep a secret that
// the baseline rules remove, and the current rules still remove what the
// baseline rules miss. Redacting the result again changes nothing.
func redactV1(text string) (string, int) { return redactV1With(text, v1Rules) }

// v1Rules runs the rule sets as redactV1 always ran them.
func v1Rules(text string, _ func() bool) (string, int, string, bool) {
	floor, nf := baseline.Redact(text)
	current, _ := redact(text, mode{entropy: true})
	return floor, nf, current, true
}

// redactV1With is redactV1 with the rule sets it runs, which tests share
// between it and the driver.
func redactV1With(text string, rules func(string, func() bool) (string, int, string, bool)) (string, int) {
	// Private key blocks span lines, so both rule sets replace them the same
	// way but leave kept text that is hard to line up between two of them.
	// Replace them first, with the pattern and marker both rule sets use.
	text, total := privateKeysV1(clean(text))
	// A union can put a marker next to text that one rule set then reads
	// differently, so repeat, at most four rounds, until nothing changes;
	// every round only removes more.
	for round := 0; round < 4; round++ {
		out, n := unionRoundV1(text, rules)
		if n == 0 {
			break
		}
		text, total = out, total+n
	}
	return text, total
}

func unionRoundV1(text string, rules func(string, func() bool) (string, int, string, bool)) (string, int) {
	floor, nf, current, _ := rules(text, nil)
	if out, n, ok := unionV1(text, floor, current); ok {
		return out, n
	}
	// The outputs could not be lined up with the text, which neither rule set
	// should cause. Redact the baseline output again with the current rules
	// instead: that keeps every removal the baseline rules made.
	out, n := redact(floor, mode{entropy: true})
	return out, nf + n
}

// privateKeys replaces private key blocks with the marker both rule sets
// write for them.
func privateKeysV1(text string) (string, int) {
	if !strings.Contains(text, "PRIVATE KEY") {
		return text, 0
	}
	n := 0
	text = privateKeyBlock.ReplaceAllStringFunc(text, func(string) string {
		n++
		return marker + "private_key]"
	})
	return text, n
}

// union renders base with every range that a or b replaced, where a and b are
// outputs of rule sets that only replace ranges of base with markers.
func unionV1(base, a, b string) (string, int, bool) {
	if a == base && b == base {
		return base, 0, true
	}
	ra, ok := removals(base, a)
	if !ok {
		return "", 0, false
	}
	rb, ok := removals(base, b)
	if !ok {
		return "", 0, false
	}
	// The current rules' kinds come first where ranges start together.
	all := append(rb, ra...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].start < all[j].start })
	var merged []removal
	for _, r := range all {
		if n := len(merged); n > 0 && r.start < merged[n-1].end {
			merged[n-1].end = max(merged[n-1].end, r.end)
			continue
		}
		merged = append(merged, r)
	}
	var out strings.Builder
	count, at := 0, 0
	for _, r := range merged {
		out.WriteString(base[at:r.start])
		if old := base[r.start:r.end]; markerRe.FindString(old) == old {
			out.WriteString(old) // a marker already in the text
		} else {
			out.WriteString(marker + r.kind + "]")
			count++
		}
		at = r.end
	}
	out.WriteString(base[at:])
	return out.String(), count, true
}
