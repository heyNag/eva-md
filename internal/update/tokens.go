package update

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// normalize lowercases s and keeps its letters and digits, one space between
// the words, so two texts that differ only in case, punctuation or spacing
// compare equal.
func normalize(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// sameWord reports whether two lowercased words are one in some form: the
// same, or one the start of the other when the shorter has 4 letters or
// more, as mock and mocking or postgres and postgresql are, while log and
// login are not.
func sameWord(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	return a == b || utf8.RuneCountInString(a) >= 4 && strings.HasPrefix(b, a)
}

// stopwords are left out of the content tokens: words too common to show
// that two items are about the same thing.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true, "these": true, "those": true,
	"from": true, "into": true, "onto": true, "are": true, "was": true, "were": true, "will": true, "would": true,
	"should": true, "could": true, "can": true, "not": true, "but": true, "use": true, "uses": true, "used": true,
	"using": true, "now": true, "then": true, "than": true, "also": true, "all": true, "any": true, "its": true,
	"our": true, "your": true, "their": true, "they": true, "them": true, "has": true, "have": true, "had": true,
	"been": true, "being": true, "does": true, "did": true, "each": true, "only": true, "more": true, "most": true,
	"some": true, "such": true, "very": true, "just": true, "about": true, "over": true, "under": true,
	"after": true, "before": true, "when": true, "what": true, "which": true, "who": true, "how": true, "why": true,
	"where": true, "there": true, "here": true, "one": true, "two": true, "new": true, "old": true, "instead": true,
	"via": true, "per": true, "out": true, "off": true, "yes": true, "let": true, "lets": true, "must": true,
	"may": true, "might": true, "shall": true, "want": true, "need": true, "needs": true, "make": true, "keep": true,
	"get": true, "set": true, "add": true, "still": true, "again": true, "every": true, "other": true,
}

// contentTokens returns the content tokens of s: its lowercased words of 3 or
// more letters, without stopwords or negative contractions, and its
// code-like tokens, lowercased. A contraction such as don't or isn't is no
// content word, nor is its fragment don or isn: one in a claim and one in the
// user's words say nothing about the same thing. Another apostrophe splits a
// word, as user's gives user.
func contentTokens(s string) map[string]bool {
	out := map[string]bool{}
	lower := strings.ReplaceAll(strings.ToLower(s), "’", "'")
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' }) {
		if word = strings.Trim(word, "'"); strings.HasSuffix(word, "n't") || contractions[word] {
			continue
		}
		for _, w := range strings.Split(word, "'") {
			if utf8.RuneCountInString(w) >= 3 && !stopwords[w] {
				out[w] = true
			}
		}
	}
	for _, t := range codeTokens(s) {
		out[strings.ToLower(t.text)] = true
	}
	return out
}

// contractions are negative contractions written without the apostrophe.
var contractions = map[string]bool{"dont": true, "cant": true, "wont": true, "isnt": true, "arent": true, "wasnt": true,
	"werent": true, "doesnt": true, "didnt": true, "hasnt": true, "havent": true, "hadnt": true, "shouldnt": true,
	"wouldnt": true, "couldnt": true, "mustnt": true, "neednt": true, "shant": true, "aint": true}

// shareToken reports whether a and b have a content token in common.
func shareToken(a, b string) bool {
	tb := contentTokens(b)
	for t := range contentTokens(a) {
		if tb[t] {
			return true
		}
	}
	return false
}

// overlap is the share of a's content tokens that b holds too; 0 when a has
// none.
func overlap(a, b string) float64 {
	ta, tb := contentTokens(a), contentTokens(b)
	if len(ta) == 0 {
		return 0
	}
	n := 0
	for t := range ta {
		if tb[t] {
			n++
		}
	}
	return float64(n) / float64(len(ta))
}

// Classes of code-like tokens, as the Report counts grounding misses.
const (
	classPath  = "path"
	classURL   = "url"
	classSHA   = "sha"
	classIdent = "identifier"
)

// numberUnit is a number with its unit: a time, a size, a count or a
// version, such as "300 ms", "40 calls", "64 KiB" or "v1.2".
var numberUnit = regexp.MustCompile(`(?i)\b(?:v\d+(?:\.\d+)*\b|(\d+(?:\.\d+)?) ?(%|(?:ms|s|secs?|seconds?|mins?|minutes?|h|hrs?|hours?|days?|weeks?|months?|years?|bytes?|kb|kib|mb|mib|gb|gib|tb|tib|calls?|tokens?|lines?|chars?|characters?|files?|items?|entries|entry|sessions?|requests?|retries|attempts?|passes|pages?|rules?|threads?|commits?|times|x)\b))`)

// numberTokens returns the numbers with units in s, in order, lowercased,
// each with its number and unit joined by one space, or as written for a
// version.
func numberTokens(s string) []string {
	var out []string
	for _, m := range numberUnit.FindAllStringSubmatch(s, -1) {
		if m[1] == "" {
			out = append(out, strings.ToLower(m[0]))
		} else {
			out = append(out, strings.ToLower(m[1]+" "+m[2]))
		}
	}
	return out
}

// numberGrounded reports whether hay, lowercased, holds a number with its
// unit, with or without the space between them.
func numberGrounded(t, hay string) bool {
	return strings.Contains(hay, t) || strings.Contains(hay, strings.Replace(t, " ", "", 1))
}

// codeToken is one code-like token and its class.
type codeToken struct{ text, class string }

var (
	sha = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	// prose holds slashed words that read as prose, not paths.
	prose = map[string]bool{"and/or": true, "either/or": true, "yes/no": true, "on/off": true, "true/false": true, "n/a": true, "i/o": true, "w/o": true}
)

// codeTokens returns the code-like tokens of s, in order: URLs, paths (a
// slash), SHAs (7 to 40 hex digits holding a digit and a letter) and
// identifiers with a dot, underscore or "::" (two letters or more, not an
// abbreviation such as e.g).
func codeTokens(s string) []codeToken {
	var out []codeToken
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("\"'`()[]{}<>,;|", r) }) {
		f = strings.TrimRight(strings.TrimLeft(f, "*"), ".:!?*")
		if f == "" {
			continue
		}
		lower := strings.ToLower(f)
		switch {
		case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
			out = append(out, codeToken{f, classURL})
		case strings.Contains(f, "/") && !prose[lower] && strings.ContainsFunc(f, isAlnum):
			out = append(out, codeToken{f, classPath})
		case sha.MatchString(f) && strings.ContainsAny(f, "0123456789") && strings.ContainsAny(f, "abcdef"):
			out = append(out, codeToken{f, classSHA})
		case identifier(f):
			out = append(out, codeToken{f, classIdent})
		}
	}
	return out
}

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// identifier reports an identifier with a dot, underscore or "::": it starts
// with a letter or underscore and holds two letters or more, unless every
// part between the dots is one letter, as in e.g or a.m. A number with a
// unit, such as 3.5ms, is not one.
func identifier(f string) bool {
	if !strings.ContainsAny(f, "._") && !strings.Contains(f, "::") {
		return false
	}
	if first, _ := utf8.DecodeRuneInString(f); !unicode.IsLetter(first) && first != '_' {
		return false
	}
	letters, short := 0, true
	for _, part := range strings.FieldsFunc(f, func(r rune) bool { return r == '.' || r == '_' || r == ':' }) {
		if utf8.RuneCountInString(part) > 1 {
			short = false
		}
	}
	for _, r := range f {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r) || strings.ContainsRune("._:-", r):
		default:
			return false
		}
	}
	return letters >= 2 && !short
}

// grounded reports whether hay, lowercased, holds the token: a path by its
// basename too, a SHA by a prefix of 7, ignoring case.
func grounded(t codeToken, hay string) bool {
	s := strings.ToLower(t.text)
	switch {
	case strings.Contains(hay, s):
		return true
	case t.class == classPath:
		base := path.Base(strings.TrimRight(s, "/"))
		return base != "." && base != "/" && strings.Contains(hay, base)
	case t.class == classSHA:
		return strings.Contains(hay, s[:7])
	}
	return false
}

// date is a calendar date written in text; year is 0 when the text gives
// none.
type date struct{ year, month, day int }

// same reports whether two written dates name the same day: the month and
// day agree, and the years too when both are written.
func (d date) same(e date) bool {
	return d.month == e.month && d.day == e.day && (d.year == 0 || e.year == 0 || d.year == e.year)
}

const monthNames = `(January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sept|Sep|Oct|Nov|Dec)`

var (
	isoDate = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	// dayMonth is "D Month YYYY", and "D Month" too, so a date written that
	// way in an entry still counts for the same date written another way. It
	// ignores case; findDates decides what a lowercase month name counts for.
	dayMonth = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)? ` + monthNames + `\b\.?(?:,? (\d{4})\b)?`)
	// monthDay is "Month D" and "Month D YYYY".
	monthDay = regexp.MustCompile(`(?i)\b` + monthNames + `\.? (\d{1,2})(?:st|nd|rd|th)?\b(?:,? (\d{4})\b)?`)
)

// dates returns the dates written in s in the forms the date rule knows:
// YYYY-MM-DD, D Month YYYY, D Month, Month D and Month D YYYY. A month name
// in lowercase counts only with a year, so "option 2 may be simpler" holds
// no date.
func dates(s string) []date { return findDates(s, false) }

// entryDates returns the dates an entry's text holds, reading a month name
// in any case, so a date the model copies with a capital still counts.
func entryDates(s string) []date { return findDates(s, true) }

func findDates(s string, anyCase bool) []date {
	var out []date
	for _, loc := range isoDate.FindAllStringIndex(s, -1) {
		if loc[0] > 0 && isDigit(s[loc[0]-1]) || loc[1] < len(s) && isDigit(s[loc[1]]) {
			continue
		}
		y, _ := strconv.Atoi(s[loc[0] : loc[0]+4])
		m, _ := strconv.Atoi(s[loc[0]+5 : loc[0]+7])
		d, _ := strconv.Atoi(s[loc[0]+8 : loc[0]+10])
		out = appendDate(out, y, m, d)
	}
	for _, m := range dayMonth.FindAllStringSubmatch(s, -1) {
		if anyCase || m[3] != "" || !lower(m[2]) {
			d, _ := strconv.Atoi(m[1])
			y, _ := strconv.Atoi(m[3])
			out = appendDate(out, y, month(m[2]), d)
		}
	}
	for _, m := range monthDay.FindAllStringSubmatch(s, -1) {
		if anyCase || m[3] != "" || !lower(m[1]) {
			d, _ := strconv.Atoi(m[2])
			y, _ := strconv.Atoi(m[3])
			out = appendDate(out, y, month(m[1]), d)
		}
	}
	return out
}

// lower reports a word that starts with a lowercase letter.
func lower(w string) bool {
	r, _ := utf8.DecodeRuneInString(w)
	return unicode.IsLower(r)
}

func isDigit(b byte) bool { return '0' <= b && b <= '9' }

// appendDate adds a date that could be a real one.
func appendDate(out []date, y, m, d int) []date {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return out
	}
	return append(out, date{y, m, d})
}

// month returns the number of a month name or its abbreviation.
func month(name string) int {
	prefix := strings.ToLower(name)[:3]
	for i, m := range []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"} {
		if prefix == m {
			return i + 1
		}
	}
	return 0
}
