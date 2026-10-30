package redact

import (
	"regexp"
	"sort"
	"strings"

	"github.com/heyNag/eva-md/internal/redact/baseline"
)

// Redact replaces secrets with [REDACTED:<kind>] and returns the new text and
// the number of replacements. It strips ANSI escapes, bidi controls and NUL
// first, and removes zero-width characters, so a secret split by them is
// still caught. Git SHAs, UUIDs and op:// references are kept.
//
// The text removed is the union of what the baseline rules
// (internal/redact/baseline) remove and what the current rules remove, both
// judged on the same cleaned text. So no rule change can keep a secret that
// the baseline rules remove, and the current rules still remove what the
// baseline rules miss. Redacting the result again changes nothing.
func Redact(text string) (string, int) {
	// Private key blocks span lines, so both rule sets replace them the same
	// way but leave kept text that is hard to line up between two of them.
	// Replace them first, with the pattern and marker both rule sets use.
	text, total := privateKeys(clean(text))
	// A union can put a marker next to text that one rule set then reads
	// differently, so repeat, at most four rounds, until nothing changes;
	// every round only removes more.
	for round := 0; round < 4; round++ {
		out, n := unionRound(text)
		if n == 0 {
			break
		}
		text, total = out, total+n
	}
	return text, total
}

func unionRound(text string) (string, int) {
	floor, nf := baseline.Redact(text)
	current, _ := redact(text, mode{entropy: true})
	if out, n, ok := union(text, floor, current); ok {
		return out, n
	}
	// The outputs could not be lined up with the text, which neither rule set
	// should cause. Redact the baseline output again with the current rules
	// instead: that keeps every removal the baseline rules made.
	out, n := redact(floor, mode{entropy: true})
	return out, nf + n
}

var privateKeyBlock = regexp.MustCompile(`-----BEGIN[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----(?s:.*?)(?:-----END[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|\z)`)

// privateKeys replaces private key blocks with the marker both rule sets
// write for them.
func privateKeys(text string) (string, int) {
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

// removal is a range of the cleaned text that a rule set replaced.
type removal struct {
	start, end int
	kind       string
}

// union renders base with every range that a or b replaced, where a and b are
// outputs of rule sets that only replace ranges of base with markers.
func union(base, a, b string) (string, int, bool) {
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

// removals lines up out with base and returns the ranges of base it
// replaced. out is base with ranges replaced by markers: kept text, then a
// run of markers, then kept text again. Where kept text could line up in
// more than one place, the range runs from the earliest place the kept text
// before it can end to the latest place the kept text after it can start, so
// an ambiguous line-up counts more text as removed, never less.
func removals(base, out string) ([]removal, bool) {
	marks := markerRe.FindAllStringIndex(out, -1)
	if len(marks) == 0 {
		return nil, out == base
	}
	// kept[i] is the text before the i-th run of markers; kinds[i] is the kind
	// of that run's first marker. The last kept text follows the last run.
	var kept, kinds []string
	at := 0
	for i, m := range marks {
		if i > 0 && m[0] == marks[i-1][1] {
			continue // the same run
		}
		kept = append(kept, out[at:m[0]])
		kinds = append(kinds, out[m[0]+len(marker):m[1]-1])
		end := m[1]
		for j := i + 1; j < len(marks) && marks[j][0] == end; j++ {
			end = marks[j][1]
		}
		at = end
	}
	kept = append(kept, out[at:])
	runs := len(kinds)
	first, last := kept[0], kept[runs]
	if !strings.HasPrefix(base, first) || len(base)-len(last) < len(first)+1 || !strings.HasSuffix(base, last) {
		return nil, false
	}
	// earliest[i] and latest[i] bound where kept[i] starts.
	earliest := make([]int, runs+1)
	latest := make([]int, runs+1)
	earliest[runs], latest[runs] = len(base)-len(last), len(base)-len(last)
	pos := len(first)
	for i := 1; i < runs; i++ {
		// Each run replaced at least one byte.
		j := strings.Index(base[min(pos+1, len(base)):], kept[i])
		if j < 0 {
			return nil, false
		}
		earliest[i] = pos + 1 + j
		pos = earliest[i] + len(kept[i])
	}
	if pos+1 > earliest[runs] {
		return nil, false
	}
	end := earliest[runs]
	for i := runs - 1; i >= 1; i-- {
		j := strings.LastIndex(base[:end-1], kept[i])
		if j < earliest[i] {
			return nil, false
		}
		latest[i] = j
		end = j
	}
	if end-1 < len(first) {
		return nil, false
	}
	rs := make([]removal, runs)
	for i := 0; i < runs; i++ {
		rs[i] = removal{start: earliest[i] + len(kept[i]), end: latest[i+1], kind: kinds[i]}
	}
	return rs, true
}
