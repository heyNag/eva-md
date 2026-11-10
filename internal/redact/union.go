package redact

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// Redact replaces secrets with [REDACTED:<kind>] and returns the new text and
// the number of replacements. It strips ANSI escapes, bidi controls and NUL
// first, and removes zero-width characters, so a secret split by them is
// still caught. Git SHAs and UUIDs are kept in ordinary text, but not as the
// value of a secret-looking key; op:// references are kept.
//
// The text removed is the union of what the baseline rules
// (internal/redact/baseline) remove and what the current rules remove, both
// judged on the same cleaned text. So no rule change can keep a secret that
// the baseline rules remove, and the current rules still remove what the
// baseline rules miss. Redacting the result again changes nothing.
//
// Redact is Scan to the end of the text, then Apply.
func Redact(text string) (string, int) {
	r := Scan(context.Background(), text)
	out := Apply(r)
	if redacted != nil {
		redacted(text, out, r.count)
	}
	return out, r.count
}

// redacted, when set, is told of every text Redact redacts, what it
// returned and its count. Only tests set it.
var redacted func(text, out string, n int)

var privateKeyBlock = regexp.MustCompile(`-----BEGIN[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----(?s:.*?)(?:-----END[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|\z)`)

// removal is a range of the cleaned text that a rule set replaced.
type removal struct {
	start, end int
	kind       string
	// current says the current rules replaced text in the range, more than
	// a marker that was there already.
	current bool
	// keep says the range is one marker that was there already, which stays
	// as it is.
	keep bool
	// tier is the surest tier of the kinds of the markers that replaced the
	// range, in either rule set's output (see kindTier), or 0 when unknown.
	// A range only the baseline rules replaced is still FloorOnly; tier is
	// what it is worth when the current rules replaced text in it too.
	tier Tier
}

// unionRanges returns the ranges of base that a or b replaced, where a and b
// are outputs of rule sets that only replace ranges of base with markers, a
// the baseline rules' and b the current rules'. Ranges that overlap are
// merged, and a merged range takes the kind of its first, the surest tier of
// them all, and is current if any of them is; where ranges start together,
// the current rules' comes first. A merged range that is one marker already
// in base is marked keep. ok is false when an output can't be lined up with
// base.
func unionRanges(base, a, b string) (merged []removal, ok bool) {
	if a == base && b == base {
		return nil, true
	}
	ra, ok := removals(base, a)
	if !ok {
		return nil, false
	}
	rb, ok := removals(base, b)
	if !ok {
		return nil, false
	}
	for i, r := range rb {
		rb[i].current = !isMarker(base[r.start:r.end])
	}
	all := append(rb, ra...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].start < all[j].start })
	for _, r := range all {
		if n := len(merged); n > 0 && r.start < merged[n-1].end {
			merged[n-1].end = max(merged[n-1].end, r.end)
			merged[n-1].current = merged[n-1].current || r.current
			merged[n-1].tier = min(merged[n-1].tier, r.tier)
			continue
		}
		merged = append(merged, r)
	}
	for i, r := range merged {
		merged[i].keep = isMarker(base[r.start:r.end])
	}
	return merged, true
}

// isMarker reports whether s is one whole marker.
func isMarker(s string) bool { return markerRe.FindString(s) == s }

// removals lines up out with base and returns the ranges of base it
// replaced. out is base with ranges replaced by markers: kept text, then a
// run of markers, then kept text again. Where kept text could line up in
// more than one place, the range runs from the earliest place the kept text
// before it can end to the latest place the kept text after it can start, so
// an ambiguous line-up counts more text as removed, never less. Each range
// takes the kind of its run's first marker and the surest tier of the
// run's markers.
func removals(base, out string) ([]removal, bool) {
	marks := markerRe.FindAllStringIndex(out, -1)
	if len(marks) == 0 {
		return nil, out == base
	}
	// kept[i] is the text before the i-th run of markers; kinds[i] is the kind
	// of that run's first marker. The last kept text follows the last run.
	var kept, kinds []string
	var tiers []Tier
	at := 0
	for i, m := range marks {
		if i > 0 && m[0] == marks[i-1][1] {
			continue // the same run
		}
		kept = append(kept, out[at:m[0]])
		kinds = append(kinds, out[m[0]+len(marker):m[1]-1])
		tier := kindTier(kinds[len(kinds)-1])
		end := m[1]
		for j := i + 1; j < len(marks) && marks[j][0] == end; j++ {
			tier = min(tier, kindTier(out[marks[j][0]+len(marker):marks[j][1]-1]))
			end = marks[j][1]
		}
		tiers = append(tiers, tier)
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
		rs[i] = removal{start: earliest[i] + len(kept[i]), end: latest[i+1], kind: kinds[i], tier: tiers[i]}
	}
	return rs, true
}
