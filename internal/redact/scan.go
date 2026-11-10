package redact

import (
	"context"
	"strings"

	"github.com/heyNag/eva-md/internal/redact/baseline"
)

// Tier says how sure the rules are that a finding is a secret. It is a label
// only: every finding is replaced, whatever its tier.
type Tier int

const (
	// Certain (T1) is a format the current rules know, such as a provider
	// token, a private key, a JWT, a URL's password or an Authorization
	// value.
	Certain Tier = 1
	// Likely (T2) is judged from context by the current rules: the value of
	// a secret-looking key, a password, or a random token near a trigger
	// word.
	Likely Tier = 2
	// FloorOnly (T3) is removed by the baseline rules alone.
	FloorOnly Tier = 3
)

// The rule sets a finding names.
const (
	RuleCurrent  = "current"
	RuleBaseline = "baseline"
)

// Finding is a range of a scanned text that the rules replace with a marker.
type Finding struct {
	Start, End int    // the range of Result.Clean
	Kind       string // the marker's kind, as in [REDACTED:<kind>]
	Rule       string // the rule set that found it: RuleCurrent, or RuleBaseline when only the baseline rules did
	Tier       Tier
}

// Result is what Scan found in a text.
type Result struct {
	// Clean is the text the rules read: the text with ANSI escapes, bidi
	// controls, NUL and zero-width characters removed. Findings are ranges
	// of it.
	Clean string
	// Findings are in order and do not overlap. Two can touch, and each is
	// then its own marker.
	//
	// Rarely, the rules' output holds part of a marker, which no range of
	// Clean stands for: the baseline rules can replace a range that starts
	// inside a marker they wrote in an earlier pass, as a URL's password
	// that runs from inside an OpenAI key's marker to the '@'. Findings then
	// hold one range from the first byte the rules changed to the last, and
	// Apply returns the rules' own output.
	Findings []Finding
	// Through is how far the scan read. It is len(Clean) unless the context
	// ended first; then only Clean[:Through], which ends a line, was
	// redacted, and the rest must be dropped.
	Through int

	count    int    // replacements, as Redact counts them
	untraced bool   // the rules' output holds part of a marker
	out      string // the rules' output, when untraced
}

// firstPrefix is the size of the first prefix a scan that can be stopped
// redacts on its own, before a longer one.
const firstPrefix = 32 << 10

// Scan runs the rules on text as Redact does and returns the ranges they
// replace, each with the kind of its marker, the rule set that found it and
// a tier.
//
// It checks ctx before each round of the rules and before each pass of the
// current rules, and stops when ctx has ended; the baseline rules' passes,
// which make one call, run to their end. The rules read the whole text at
// once, so what one pass found is no redaction of any part of the text
// until the round ends. So when ctx can end, Scan first redacts prefixes of
// the text on their own, from 32 KiB, each ending a line and twice as long
// as the one before, up to half the text, and then the whole text; a scan
// stopped by ctx returns the longest prefix it finished, or nothing, with
// Through saying where it ends. Where the rules read across lines, they
// read forward, as a private key block, the words after a trigger word and
// a command continued on the next line do, so a prefix redacted on its own
// keeps nothing the whole text's redaction removes from it. The prefixes
// cost at most as much again as the whole text; a ctx that can't end, as
// Redact's, is spared them.
func Scan(ctx context.Context, text string) Result {
	return driver{ctx: ctx}.scan(text, firstPrefix)
}

// driver runs the rules on a text and stops when its context ends.
type driver struct {
	ctx context.Context
	// step, when set, is called at each point the driver checks ctx, with
	// how far the text it is redacting reaches. Only tests set it.
	step func(through int)
	// rules, when set, stands in for bothRules. Only tests set it.
	rules func(text string, stop func() bool) (floor string, nf int, current string, ok bool)
}

// bothRules runs the baseline rules and then the current rules on text,
// which stop before a pass when stop says so; ok is false when they did.
func bothRules(text string, stop func() bool) (floor string, nf int, current string, ok bool) {
	floor, nf = baseline.Redact(text)
	current, _, ok = redactUntil(text, mode{entropy: true}, stop)
	return floor, nf, current, ok
}

// scan is Scan, with first the size of the first prefix.
func (d driver) scan(text string, first int) Result {
	c := clean(text)
	best := Result{Clean: c}
	if d.ctx.Done() != nil {
		for b := first; b <= len(c)/2; b *= 2 {
			end := strings.LastIndexByte(c[:b], '\n') + 1
			if end <= best.Through {
				continue
			}
			r, ok := d.run(c[:end])
			if !ok {
				return best
			}
			r.Clean = c
			best = r
		}
	}
	r, ok := d.run(c)
	if !ok {
		return best
	}
	r.Clean = c
	return r
}

// Apply returns the text a Result stands for: Clean up to Through, with
// each finding replaced by its marker. r is as Scan returned it.
func Apply(r Result) string {
	if r.untraced {
		return r.out
	}
	var b strings.Builder
	b.Grow(r.Through)
	at := 0
	for _, f := range r.Findings {
		b.WriteString(r.Clean[at:f.Start])
		b.WriteString(marker + f.Kind + "]")
		at = f.End
	}
	b.WriteString(r.Clean[at:r.Through])
	return b.String()
}

// stop reports whether ctx has ended, calling step first.
func (d driver) stop(through int) bool {
	if d.step != nil {
		d.step(through)
	}
	return d.ctx.Err() != nil
}

// run redacts the cleaned text c as Redact always has: private key blocks
// first, then rounds of the baseline rules and the current rules, each on
// the output of the round before and their removals unioned, until a round
// removes nothing, at most four. It follows each round's ranges back to c.
// ok is false when ctx ended first.
func (d driver) run(c string) (r Result, ok bool) {
	t := newTrace(c)
	if d.stop(len(c)) {
		return r, false
	}
	if strings.Contains(c, "PRIVATE KEY") {
		var rs []removal
		for _, m := range privateKeyBlock.FindAllStringIndex(c, -1) {
			rs = append(rs, removal{start: m[0], end: m[1], kind: "private_key", current: true})
		}
		t.replace(rs)
		t.count += len(rs)
	}
	rules := d.rules
	if rules == nil {
		rules = bothRules
	}
	stop := func() bool { return d.stop(len(c)) }
	for round := 0; round < 4; round++ {
		if stop() {
			return r, false
		}
		floor, nf, current, ok := rules(t.text, stop)
		if !ok {
			return r, false
		}
		n := t.union(floor, nf, current)
		if n == 0 {
			break
		}
		t.count += n
	}
	return t.result(), true
}

// trace is a text the driver redacts, as the rules see it, and as pieces of
// the cleaned text it started from, so that each range a round replaces is
// known as a range of that text.
type trace struct {
	clean  string
	text   string  // the text now, as the rules return it
	pieces []piece // text, as runs of clean, kept or replaced by a marker
	count  int
	// untraced says the rules' output holds part of a marker (see
	// Result.Findings), so pieces no longer follow text.
	untraced bool
}

// piece is a range of the cleaned text, kept as it is or replaced by a
// marker of kind.
type piece struct {
	start, end int
	marker     bool
	kind, rule string
	tier       Tier
}

// size is how many bytes p takes in the text.
func (p piece) size() int {
	if p.marker {
		return len(marker) + len(p.kind) + 1
	}
	return p.end - p.start
}

func newTrace(c string) *trace {
	return &trace{clean: c, text: c, pieces: []piece{{start: 0, end: len(c)}}}
}

// union replaces what either rule set removed, given the baseline rules'
// output floor, which made nf replacements, and the current rules' output
// current, and returns the replacements made. When the outputs can't be
// lined up with the text, as when the baseline rules cut a marker of their
// own, the current rules redact the baseline output again instead, which
// keeps every removal the baseline rules made.
func (t *trace) union(floor string, nf int, current string) int {
	rs, ok := unionRanges(t.text, floor, current)
	if ok {
		n := 0
		for _, r := range rs {
			if !r.keep {
				n++
			}
		}
		if n > 0 {
			t.replace(rs)
		}
		return n
	}
	out, n := redact(floor, mode{entropy: true})
	n += nf
	if n == 0 {
		return 0
	}
	rs, ok = removals(t.text, out)
	if !ok {
		t.untraced = true
		t.text = out
		return n
	}
	for i, r := range rs {
		rs[i].current = true
		rs[i].keep = isMarker(t.text[r.start:r.end])
	}
	t.replace(rs)
	if t.text != out {
		t.untraced = true
		t.text = out
	}
	return n
}

// replace replaces each range of the text in rs, which are in order and do
// not overlap, with a marker of its kind, but for those marked keep.
func (t *trace) replace(rs []removal) {
	var b strings.Builder
	b.Grow(len(t.text))
	at := 0
	for _, r := range rs {
		b.WriteString(t.text[at:r.start])
		if r.keep {
			b.WriteString(t.text[r.start:r.end])
		} else {
			b.WriteString(marker + r.kind + "]")
		}
		at = r.end
	}
	b.WriteString(t.text[at:])
	t.text = b.String()
	if !t.untraced {
		t.pieces, t.untraced = remap(t.pieces, rs)
	}
}

// remap returns ps, the pieces of a text, with each range of the text in rs
// replaced by a marker piece over the cleaned text it stands for. A marker
// piece a range covers is taken in; its tier stays if it is surer, and so
// does the current rule set. bad is true when a range starts or ends inside
// a marker piece: a marker cut in two stands for no range of the cleaned
// text.
func remap(ps []piece, rs []removal) (out []piece, bad bool) {
	i, off := 0, 0 // ps[i] starts at off in the text
	for _, r := range rs {
		if r.keep {
			continue
		}
		for off+ps[i].size() <= r.start {
			out = append(out, ps[i])
			off += ps[i].size()
			i++
		}
		f := piece{marker: true, kind: r.kind, rule: RuleBaseline, tier: FloorOnly}
		if r.current {
			f.rule, f.tier = RuleCurrent, kindTier(r.kind)
		}
		if p := ps[i]; r.start > off {
			if p.marker {
				return nil, true
			}
			out = append(out, piece{start: p.start, end: p.start + r.start - off})
			f.start = p.start + r.start - off
		} else {
			f.start = p.start
		}
		for i < len(ps) && off+ps[i].size() <= r.end {
			if p := ps[i]; p.marker {
				f.tier = min(f.tier, p.tier)
				if p.rule == RuleCurrent {
					f.rule = RuleCurrent
				}
			}
			f.end = ps[i].end
			off += ps[i].size()
			i++
		}
		if r.end > off {
			p := ps[i]
			if p.marker {
				return nil, true
			}
			f.end = p.start + r.end - off
			ps[i] = piece{start: f.end, end: p.end}
			off = r.end
		}
		out = append(out, f)
	}
	return append(out, ps[i:]...), false
}

// kindTier is the tier of what the current rules replace with a marker of
// kind: a value judged from its key or context is likely, a format certain.
func kindTier(kind string) Tier {
	switch kind {
	case "password", "secret_value", "high_entropy":
		return Likely
	}
	return Certain
}

// result returns what the trace found. Its pieces are checked against the
// text the rules returned; where they could not follow it, the one finding
// covers all that changed, and Apply returns the rules' own text.
func (t *trace) result() Result {
	r := Result{Clean: t.clean, Through: len(t.clean), count: t.count}
	if !t.untraced {
		for _, p := range t.pieces {
			if p.marker {
				r.Findings = append(r.Findings, Finding{Start: p.start, End: p.end, Kind: p.kind, Rule: p.rule, Tier: p.tier})
			}
		}
		if Apply(r) == t.text {
			return r
		}
	}
	// Keep the rules' text, and report one range from the first byte that
	// changed to the last.
	head := 0
	for head < len(t.clean) && head < len(t.text) && t.clean[head] == t.text[head] {
		head++
	}
	tail := 0
	for tail < len(t.clean)-head && tail < len(t.text)-head && t.clean[len(t.clean)-1-tail] == t.text[len(t.text)-1-tail] {
		tail++
	}
	r.Findings = nil
	if head < len(t.clean)-tail {
		r.Findings = []Finding{{Start: head, End: len(t.clean) - tail, Kind: "secret_value", Rule: RuleCurrent, Tier: Likely}}
	}
	r.untraced, r.out = true, t.text
	return r
}
