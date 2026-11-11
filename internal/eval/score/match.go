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
// condition by its marker and its words. A slot is wrong, not dropped, when
// the text holds another value of its class that no slot of the fact
// holds: another date, another quantity of the same kind, another path,
// version, number or name-like word.

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
	words []string // lower case
	raw   []string // as written
	dates []string
	quant []quantity
}

func readText(s string) text {
	return text{words: words(s), raw: split(s), dates: dates(s), quant: quantities(s)}
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

// condMarkers start a condition; stop are words a condition may lose.
var (
	condMarkers = []string{"if", "when", "whenever", "unless", "once"}
	stop        = []string{"a", "an", "the", "of", "in", "on", "at", "to", "for", "by", "with", "and", "or", "is", "are", "be"}
)

// holdsCond reports whether the text states a condition and holds every
// word of cond but its marker and its small words.
func (t text) holdsCond(cond string) bool {
	if !slices.ContainsFunc(t.words, func(w string) bool { return slices.Contains(condMarkers, w) }) {
		return false
	}
	need := 0
	for _, w := range words(cond) {
		if slices.Contains(condMarkers, w) || slices.Contains(stop, w) {
			continue
		}
		need++
		if !slices.Contains(t.words, w) {
			return false
		}
	}
	return need > 0
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

// dates returns the days s names: YYYY-MM-DD for a day written in full,
// in any of three forms, and yesterday, today or tomorrow as the word.
func dates(s string) []string {
	s = strings.ToLower(s)
	var out []string
	add := func(y, m, d string) {
		year, _ := strconv.Atoi(y)
		day, _ := strconv.Atoi(d)
		mon, ok := months[m[:min(3, len(m))]]
		if !ok {
			n, _ := strconv.Atoi(m)
			mon = time.Month(n)
		}
		t := time.Date(year, mon, day, 0, 0, 0, 0, time.UTC)
		if mon < 1 || mon > 12 || t.Day() != day || t.Month() != mon {
			return
		}
		out = append(out, t.Format(time.DateOnly))
	}
	for _, m := range isoDate.FindAllStringSubmatch(s, -1) {
		add(m[1], m[2], m[3])
	}
	for _, m := range dayMonth.FindAllStringSubmatch(s, -1) {
		add(m[3], m[2], m[1])
	}
	for _, m := range monthDay.FindAllStringSubmatch(s, -1) {
		add(m[3], m[1], m[2])
	}
	out = append(out, relDay.FindAllString(s, -1)...)
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

var numberUnit = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s?([a-z%]+)`)

// quantities returns the quantities s writes as a number and a unit. A
// number that ends a word, as in KITE-5, is not one.
func quantities(s string) []quantity {
	s = strings.ToLower(s)
	var out []quantity
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
		out = append(out, quantity{u.dim, n * u.v})
	}
	return out
}
