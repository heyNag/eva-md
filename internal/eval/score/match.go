package score

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/eval/scenario"
)

// The scorer's own small normalizers. A slot is held when the text holds
// its value or one of its forms, compared by class: words as a run of
// lower-case words, dates as days, quantities by number and unit, a
// condition by the words, dates and quantities that follow a marker within
// the clause it opens. A slot is wrong, not dropped, when
// the text holds another value of its class that no slot of the fact
// holds: another date, another quantity of the same kind, another path,
// version, number or name-like word.
//
// A day written with slashes, as 3/3/2026, is not read as one: whether the
// day or the month comes first is not written. A scenario that needs it
// lists the form on its slot.

// words returns the lower-case words of s: runs of letters, digits and the
// characters _ - . / inside a word, with . - / trimmed from both ends.
func words(s string) []string { return split(strings.ToLower(s)) }

// split is words without the change of case.
func split(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == '/')
	}) {
		if w = strings.Trim(w, ".-/"); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// hasRun reports whether have holds want as a run of consecutive words.
func hasRun(have, want []string) bool {
	if len(want) == 0 {
		return false
	}
	for i := 0; i+len(want) <= len(have); i++ {
		if slices.Equal(have[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// literals are a slot's value and its forms.
func literals(s scenario.Slot) []string { return append([]string{s.Value}, s.Forms...) }

// text is an item's text, read once.
type text struct {
	lower string   // the whole text, in lower case
	words []string // lower case
	raw   []string // as written
	dates []string
	quant []quantity
}

func readText(s string) text {
	return text{lower: strings.ToLower(s), words: words(s), raw: split(s), dates: dates(s), quant: quantities(s)}
}

// holds reports whether the text holds slot s.
func (t text) holds(s scenario.Slot) bool {
	for _, lit := range literals(s) {
		switch s.Class {
		case scenario.SlotDate:
			if ds := dates(lit); len(ds) > 0 {
				if slices.ContainsFunc(ds, func(d string) bool { return slices.Contains(t.dates, d) }) {
					return true
				}
				continue
			}
		case scenario.SlotQuantity:
			if qs := quantities(lit); len(qs) > 0 {
				if slices.ContainsFunc(qs, func(q quantity) bool { return slices.ContainsFunc(t.quant, q.equal) }) {
					return true
				}
				continue
			}
		case scenario.SlotCond:
			if t.holdsCond(lit) {
				return true
			}
			continue
		}
		if hasRun(t.words, words(lit)) {
			return true
		}
	}
	return false
}

// condMarkers start a condition, unless one of the opposite sense; stop
// are words a condition may lose.
var (
	condMarkers = []string{"if", "when", "whenever", "unless", "once"}
	stop        = []string{"a", "an", "the", "of", "in", "on", "at", "to", "for", "by", "with", "and", "or", "is", "are", "be"}
	marker      = regexp.MustCompile(`\b(?:if|when|whenever|unless|once)\b`)
	// clauseBreak ends a clause: a semicolon, or a full stop, a question or
	// an exclamation mark before a space or the end. Whether a comma ends
	// one is holdsCond's to say.
	clauseBreak = regexp.MustCompile(`;|[.!?](?:\s|$)`)
)

// cond is a condition as a slot gives it: its quantities and dates, the
// rest of its words but its marker and its small words, whether its marker
// is unless, and how many clause breaks it holds past its marker.
type cond struct {
	quant  []quantity
	dates  []string
	words  []string
	unless bool
	breaks int
}

func readCond(s string) cond {
	lower := strings.ToLower(s)
	masked := maskValues(lower)
	c := cond{quant: quantities(lower), dates: dates(lower)}
	for _, w := range words(masked) {
		if !slices.Contains(condMarkers, w) && !slices.Contains(stop, w) {
			c.words = append(c.words, w)
		}
	}
	rest := masked
	if m := marker.FindStringIndex(masked); m != nil {
		rest, c.unless = masked[m[1]:], masked[m[0]:m[1]] == "unless"
	}
	c.breaks = len(clauseBreak.FindAllStringIndex(strings.TrimRight(rest, " \t.,;!?"), -1))
	return c
}

// touches reports whether span holds any quantity, date or word of c.
func (c cond) touches(span string) bool {
	qs, ds, ws := quantities(span), dates(span), words(span)
	return slices.ContainsFunc(c.quant, func(q quantity) bool { return slices.ContainsFunc(qs, q.equal) }) ||
		slices.ContainsFunc(c.dates, func(d string) bool { return slices.Contains(ds, d) }) ||
		slices.ContainsFunc(c.words, func(w string) bool { return slices.Contains(ws, w) })
}

// only reports whether masked, a span with its dates and quantities
// blanked, holds no word but the condition's own words, markers and small
// words.
func (c cond) only(masked string) bool {
	return !slices.ContainsFunc(words(masked), func(w string) bool {
		return !slices.Contains(c.words, w) && !slices.Contains(condMarkers, w) && !slices.Contains(stop, w)
	})
}

// heldBy reports whether span holds every quantity, date and word of c.
func (c cond) heldBy(span string) bool {
	qs, ds, ws := quantities(span), dates(span), words(span)
	return !slices.ContainsFunc(c.quant, func(q quantity) bool { return !slices.ContainsFunc(qs, q.equal) }) &&
		!slices.ContainsFunc(c.dates, func(d string) bool { return !slices.Contains(ds, d) }) &&
		!slices.ContainsFunc(c.words, func(w string) bool { return !slices.Contains(ws, w) })
}

// holdsCond reports whether the text states condition s: whether, after a
// marker of the same sense, unless or any other, and within the clause it
// opens, it holds each of the condition's quantities and dates, compared
// as their classes are, and each of its other words but its marker and its
// small words. A quantity or a date never ends a clause, so the comma in
// "March 3, 2026" does not; a condition with clause breaks of its own
// spans as many more clauses. A comma ends the clause unless the part it
// closes holds something of the condition and the part it opens holds no
// word but the condition's own, its markers and small words, with any
// quantity or date. So "if BenchmarkBrief, in scripts/benchmark.py, passes"
// and "if within 300 ms, BenchmarkBrief passes" are one clause each; "if the
// tests pass, deploy, and BenchmarkBrief passes" ends at its first comma;
// and "if BenchmarkBrief passes, switch within 300 ms" ends at its comma
// too, since its threshold now qualifies the switch, not the condition.
func (t text) holdsCond(s string) bool {
	c := readCond(s)
	if len(c.quant)+len(c.dates)+len(c.words) == 0 {
		return false
	}
	masked := maskValues(t.lower)
	for _, m := range marker.FindAllStringIndex(masked, -1) {
		if (masked[m[0]:m[1]] == "unless") != c.unless {
			continue
		}
		end := len(masked)
		if bs := clauseBreak.FindAllStringIndex(masked[m[1]:], c.breaks+1); len(bs) > c.breaks {
			end = m[1] + bs[c.breaks][0]
		}
		for lo := m[1]; ; {
			hi := end
			if k := strings.IndexByte(masked[lo:end], ','); k >= 0 {
				hi = lo + k
			}
			if c.heldBy(t.lower[m[1]:hi]) {
				return true
			}
			if hi == end || !c.touches(t.lower[lo:hi]) {
				break
			}
			lo = hi + 1
			next := end
			if k := strings.IndexByte(masked[lo:end], ','); k >= 0 {
				next = lo + k
			}
			if !c.only(masked[lo:next]) {
				break
			}
		}
	}
	return false
}

// maskValues blanks each date and quantity of s, which is in lower case,
// with spaces, keeping its length.
func maskValues(s string) string {
	b := []byte(s)
	for _, sp := range dateSpans(s) {
		copy(b[sp.lo:sp.hi], strings.Repeat(" ", sp.hi-sp.lo))
	}
	for _, sp := range quantitySpans(s) {
		copy(b[sp.lo:sp.hi], strings.Repeat(" ", sp.hi-sp.lo))
	}
	return string(b)
}

// wrong reports whether the text holds another value of s's class that
// no slot of f holds.
func (t text) wrong(s scenario.Slot, f scenario.Fact) bool {
	var cover []string
	var coverDates []string
	var coverQuant []quantity
	for _, fs := range f.Slots {
		for _, lit := range literals(fs) {
			cover = append(cover, words(lit)...)
			coverDates = append(coverDates, dates(lit)...)
			coverQuant = append(coverQuant, quantities(lit)...)
		}
	}
	switch s.Class {
	case scenario.SlotDate:
		return slices.ContainsFunc(t.dates, func(d string) bool { return !slices.Contains(coverDates, d) })
	case scenario.SlotQuantity:
		var dims []string
		for _, lit := range literals(s) {
			for _, q := range quantities(lit) {
				dims = append(dims, q.dim)
			}
		}
		return slices.ContainsFunc(t.quant, func(q quantity) bool {
			return slices.Contains(dims, q.dim) && !slices.ContainsFunc(coverQuant, q.equal)
		})
	case scenario.SlotIdent:
		return slices.ContainsFunc(t.raw, func(w string) bool { return identLike(w) && !slices.Contains(cover, strings.ToLower(w)) })
	case scenario.SlotPath:
		return slices.ContainsFunc(t.words, func(w string) bool { return pathLike(w) && !slices.Contains(cover, w) })
	case scenario.SlotVersion:
		return slices.ContainsFunc(t.words, func(w string) bool { return versionLike.MatchString(w) && !slices.Contains(cover, w) })
	case scenario.SlotInt:
		return slices.ContainsFunc(t.words, func(w string) bool { return intLike.MatchString(w) && !slices.Contains(cover, w) })
	}
	return false
}

var (
	identShape  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[_-][A-Za-z0-9]+)*$`)
	versionLike = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)+$`)
	intLike     = regexp.MustCompile(`^[0-9]+$`)
	fileLike    = regexp.MustCompile(`^[a-z0-9_-]*[a-z][a-z0-9_-]*\.[a-z]{1,5}$`)
)

// identLike reports a word written as a name in code is: one with a digit,
// an underscore or a capital past its first letter, as KITE-5, EVA_TOKEN,
// YAML and SQLite are.
func identLike(w string) bool {
	if !identShape.MatchString(w) {
		return false
	}
	return strings.ContainsAny(w, "0123456789_") || strings.ContainsFunc(w[1:], unicode.IsUpper)
}

// pathLike reports a word that names a file or a folder: one with a slash
// and a letter, or a name of two characters or more with an extension.
func pathLike(w string) bool {
	if strings.Contains(w, "/") {
		return strings.ContainsFunc(w, unicode.IsLetter)
	}
	name, _, _ := strings.Cut(w, ".")
	return len(name) >= 2 && fileLike.MatchString(w)
}

// Dates.

var (
	month    = `(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`
	isoDate  = regexp.MustCompile(`\b([0-9]{4})-([0-9]{2})-([0-9]{2})\b`)
	dayMonth = regexp.MustCompile(`\b([0-9]{1,2})(?:st|nd|rd|th)?\s+` + month + `\.?,?\s+([0-9]{4})\b`)
	monthDay = regexp.MustCompile(`\b` + month + `\.?\s+([0-9]{1,2})(?:st|nd|rd|th)?,?\s+([0-9]{4})\b`)
	relDay   = regexp.MustCompile(`\b(yesterday|today|tomorrow)\b`)
)

var months = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

// span is a value found in a text, and where it stands: s[lo:hi].
type span[T any] struct {
	v      T
	lo, hi int
}

// dates returns the days s names: YYYY-MM-DD for a day written in full,
// in any of three forms, and yesterday, today or tomorrow as the word.
func dates(s string) []string {
	var out []string
	for _, sp := range dateSpans(strings.ToLower(s)) {
		out = append(out, sp.v)
	}
	return out
}

// dateSpans is dates, with where each stands in s, which is in lower case.
func dateSpans(s string) []span[string] {
	var out []span[string]
	add := func(m []int, y, mo, d int) {
		part := func(k int) string { return s[m[2*k]:m[2*k+1]] }
		year, _ := strconv.Atoi(part(y))
		day, _ := strconv.Atoi(part(d))
		mt := part(mo)
		mon, ok := months[mt[:min(3, len(mt))]]
		if !ok {
			n, _ := strconv.Atoi(mt)
			mon = time.Month(n)
		}
		t := time.Date(year, mon, day, 0, 0, 0, 0, time.UTC)
		if mon < 1 || mon > 12 || t.Day() != day || t.Month() != mon {
			return
		}
		out = append(out, span[string]{t.Format(time.DateOnly), m[0], m[1]})
	}
	for _, m := range isoDate.FindAllStringSubmatchIndex(s, -1) {
		add(m, 1, 2, 3)
	}
	for _, m := range dayMonth.FindAllStringSubmatchIndex(s, -1) {
		add(m, 3, 2, 1)
	}
	for _, m := range monthDay.FindAllStringSubmatchIndex(s, -1) {
		add(m, 3, 1, 2)
	}
	for _, m := range relDay.FindAllStringIndex(s, -1) {
		out = append(out, span[string]{s[m[0]:m[1]], m[0], m[1]})
	}
	return out
}

// Quantities.

// quantity is a number in a unit's base, and what the unit measures.
type quantity struct {
	dim string
	v   float64
}

func (q quantity) equal(o quantity) bool {
	return q.dim == o.dim && math.Abs(q.v-o.v) <= 1e-9*math.Max(1, math.Abs(q.v))
}

// units maps a unit as written to what it measures and its size in the
// base unit of that measure.
var units = map[string]quantity{
	"ms": {"time", 0.001}, "msec": {"time", 0.001}, "msecs": {"time", 0.001}, "millisecond": {"time", 0.001}, "milliseconds": {"time", 0.001},
	"s": {"time", 1}, "sec": {"time", 1}, "secs": {"time", 1}, "second": {"time", 1}, "seconds": {"time", 1},
	"min": {"time", 60}, "mins": {"time", 60}, "minute": {"time", 60}, "minutes": {"time", 60},
	"h": {"time", 3600}, "hr": {"time", 3600}, "hrs": {"time", 3600}, "hour": {"time", 3600}, "hours": {"time", 3600},
	"day": {"time", 86400}, "days": {"time", 86400}, "week": {"time", 604800}, "weeks": {"time", 604800},
	"byte": {"size", 1}, "bytes": {"size", 1}, "kb": {"size", 1e3}, "kib": {"size", 1024}, "mb": {"size", 1e6}, "mib": {"size", 1 << 20},
	"gb": {"size", 1e9}, "gib": {"size", 1 << 30}, "%": {"ratio", 0.01}, "percent": {"ratio", 0.01},
}

var numberUnit = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)(?:\s|-)?([a-z%]+)`)

// quantities returns the quantities s writes as a number and a unit, apart,
// joined or joined by a hyphen, as in 5 min, 300ms and 5-minute. A number
// that ends a word, as in KITE-5, is not one.
func quantities(s string) []quantity {
	var out []quantity
	for _, sp := range quantitySpans(strings.ToLower(s)) {
		out = append(out, sp.v)
	}
	return out
}

// quantitySpans is quantities, with where each stands in s, which is in
// lower case.
func quantitySpans(s string) []span[quantity] {
	var out []span[quantity]
	for _, m := range numberUnit.FindAllStringSubmatchIndex(s, -1) {
		if m[0] > 0 {
			if c := s[m[0]-1]; c == '-' || c == '_' || c == '.' || c == '/' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
				continue
			}
		}
		u, ok := units[s[m[4]:m[5]]]
		if !ok {
			continue
		}
		n, err := strconv.ParseFloat(s[m[2]:m[3]], 64)
		if err != nil {
			continue
		}
		out = append(out, span[quantity]{quantity{u.dim, n * u.v}, m[0], m[1]})
	}
	return out
}
