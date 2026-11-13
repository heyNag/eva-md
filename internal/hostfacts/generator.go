package hostfacts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The made-up epoch the fixtures' times start from: in a year before any
// recording, and a number of seconds past midnight that is not a whole
// number of minutes. MonoBase is where the fixtures' monotonic readings
// start.
var (
	Epoch    = time.Date(2019, 3, 14, 9, 26, 53, 0, time.UTC)
	MonoBase = int64(1_000_000_000_007)
)

// EpochEnd bounds the fixtures' times from above: a recording spans less.
var EpochEnd = Epoch.AddDate(1, 0, 0)

// greek is the alphabet a character outside ASCII is replaced from, so a
// replaced string keeps its length in characters and its classes.
const greek = "αβγδεζηθικλμνξοπρστυφχψω"

// A shape is a value's form with its content left out: each character's
// class (a lower-case letter, an upper-case letter, a digit, a hex digit, a
// character outside ASCII) and every character that is kept as it is
// (punctuation, white space, a wrapper's tag on the vocabulary, an id's
// prefix). Times are pieces of their own, mapped by value and not drawn.
//
// The shape is written as one string: a class as a letter after '%', a
// kept character as itself, '%' itself as "%%", and a time as "%T" with
// its form's letters.
var (
	pieceOnce sync.Once
	pieceRe   *regexp.Regexp
)

const timePattern = `\d{4}-\d{2}-\d{2}T\d{2}[:\-]\d{2}[:\-]\d{2}(?:\.\d{1,9})?(?:Z|[+\-]\d{2}:\d{2})?`

func pieces() *regexp.Regexp {
	pieceOnce.Do(func() {
		v := Vocab()
		var tags []string
		for _, t := range v.Tags {
			tags = append(tags, regexp.QuoteMeta(t))
		}
		var brackets []string
		for _, b := range v.BracketPrefixes {
			brackets = append(brackets, regexp.QuoteMeta(b))
		}
		var prefixes []string
		for _, p := range v.IDPrefixes {
			prefixes = append(prefixes, regexp.QuoteMeta(p))
		}
		pieceRe = regexp.MustCompile(strings.Join([]string{
			"(" + timePattern + ")",
			`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`,
			`(</?(?:` + strings.Join(tags, "|") + `)>)`,
			`(` + strings.Join(brackets, "|") + `)`,
			`(` + strings.Join(prefixes, "|") + `)[A-Za-z0-9]`,
			`\b([0-9a-f]*[a-f][0-9a-f]*)\b`,
		}, "|"))
	})
	return pieceRe
}

// shapeOf gives a string's shape, and the times it holds in order.
func shapeOf(s string) (shape string, times []string) {
	var b strings.Builder
	classes := func(text string, hexRun bool) {
		for _, r := range text {
			switch {
			case hexRun && (r >= '0' && r <= '9' || r >= 'a' && r <= 'f'):
				b.WriteString("%h")
			case hexRun && r >= 'A' && r <= 'F':
				b.WriteString("%H")
			case r >= 'a' && r <= 'z':
				b.WriteString("%a")
			case r >= 'A' && r <= 'Z':
				b.WriteString("%A")
			case r >= '0' && r <= '9':
				b.WriteString("%9")
			case r == '%':
				b.WriteString("%%")
			case r < utf8.RuneSelf:
				b.WriteRune(r)
			default:
				b.WriteString("%u")
			}
		}
	}
	kept := func(text string) { b.WriteString(strings.ReplaceAll(text, "%", "%%")) }
	last := 0
	for _, m := range pieces().FindAllStringSubmatchIndex(s, -1) {
		classes(s[last:m[0]], false)
		switch {
		case m[2] >= 0:
			t := s[m[2]:m[3]]
			times = append(times, t)
			b.WriteString("%T" + timeForm(t))
		case m[4] >= 0:
			classes(s[m[4]:m[5]], true)
		case m[6] >= 0:
			kept(s[m[6]:m[7]])
		case m[8] >= 0:
			kept(s[m[8]:m[9]])
		case m[10] >= 0:
			kept(s[m[10]:m[11]])
			classes(s[m[11]:m[1]], false)
		case m[12] >= 0:
			if m[13]-m[12] >= 16 {
				classes(s[m[12]:m[13]], true)
			} else {
				classes(s[m[12]:m[13]], false)
			}
		}
		last = m[1]
	}
	classes(s[last:], false)
	return b.String(), times
}

// timeForm writes a time's form: its separator, its digits after the point
// and whether it carried a zone, as three characters.
func timeForm(t string) string {
	sep := t[13]
	frac := 0
	if i := strings.IndexByte(t, '.'); i >= 0 {
		j := i + 1
		for j < len(t) && t[j] >= '0' && t[j] <= '9' {
			j++
		}
		frac = j - i - 1
	}
	return string(sep) + strconv.Itoa(frac) + "z"
}

// stream is the generator's randomness for one shape and one draw: an
// HMAC of the seed over the shape and the draw's number, in counter mode.
type stream struct {
	mac   []byte
	block []byte
	n     uint64
	buf   []byte
}

func newStream(seed []byte, kind, shape string, k int) *stream {
	m := hmac.New(sha256.New, seed)
	m.Write([]byte(kind + "\x00" + shape + "\x00" + strconv.Itoa(k)))
	return &stream{mac: m.Sum(nil)}
}

func (s *stream) next(n int) int {
	if len(s.buf) < 2 {
		m := hmac.New(sha256.New, s.mac)
		var c [8]byte
		binary.BigEndian.PutUint64(c[:], s.n)
		s.n++
		m.Write(c[:])
		s.buf = m.Sum(nil)
	}
	v := int(binary.BigEndian.Uint16(s.buf))
	s.buf = s.buf[2:]
	return v % n
}

// draw makes the k-th made-up string of a shape, with times, in order, in
// place of the shape's times.
func draw(seed []byte, shape string, k int, times []string) string {
	st := newStream(seed, "s", shape, k)
	var b strings.Builder
	for i := 0; i < len(shape); i++ {
		if shape[i] != '%' {
			b.WriteByte(shape[i])
			continue
		}
		i++
		switch shape[i] {
		case 'a':
			b.WriteByte(byte('a' + st.next(26)))
		case 'A':
			b.WriteByte(byte('A' + st.next(26)))
		case '9':
			b.WriteByte(byte('0' + st.next(10)))
		case 'h':
			b.WriteByte("0123456789abcdef"[st.next(16)])
		case 'H':
			b.WriteByte("0123456789ABCDEF"[st.next(16)])
		case 'u':
			b.WriteRune([]rune(greek)[st.next(utf8.RuneCountInString(greek))])
		case '%':
			b.WriteByte('%')
		case 'T':
			if len(times) > 0 {
				b.WriteString(times[0])
				times = times[1:]
			}
			i += 3
		}
	}
	return b.String()
}

// numberForm is a number's shape: its sign, its digits before the point
// and its digits after.
func numberForm(s string) (form string, ok bool) {
	sign := "+"
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	if strings.ContainsAny(s, "eE") {
		return "", false
	}
	whole, frac, _ := strings.Cut(s, ".")
	return sign + strconv.Itoa(len(whole)) + "." + strconv.Itoa(len(frac)), true
}

// drawNumber makes the k-th made-up number of a form: the same sign and the
// same count of digits, with no leading zero.
func drawNumber(seed []byte, form string, k int) string {
	st := newStream(seed, "n", form, k)
	sign := form[:1]
	whole, frac, _ := strings.Cut(form[1:], ".")
	w, _ := strconv.Atoi(whole)
	f, _ := strconv.Atoi(frac)
	var b strings.Builder
	if sign == "-" {
		b.WriteByte('-')
	}
	for i := 0; i < w; i++ {
		lo := 0
		if i == 0 && w > 1 {
			lo = 1
		}
		b.WriteByte(byte('0' + lo + st.next(10-lo)))
	}
	if f > 0 {
		b.WriteByte('.')
		for i := 0; i < f; i++ {
			d := st.next(10)
			if i == f-1 && d == 0 {
				d = 1 + st.next(9)
			}
			b.WriteByte(byte('0' + d))
		}
	}
	return b.String()
}

// parseTime reads a time a recording holds; one written with no zone is in
// the recording's own offset from UTC.
func parseTime(t string, offset int) (time.Time, bool) {
	m := timeParts.FindStringSubmatch(t)
	if m == nil {
		return time.Time{}, false
	}
	n := make([]int, 7)
	for i := 1; i <= 6; i++ {
		n[i], _ = strconv.Atoi(m[i])
	}
	nanos := 0
	if m[7] != "" {
		nanos, _ = strconv.Atoi((m[7] + "000000000")[:9])
	}
	loc := time.FixedZone("", offset)
	switch {
	case m[8] == "Z":
		loc = time.UTC
	case m[8] != "":
		h, _ := strconv.Atoi(m[8][1:3])
		mm, _ := strconv.Atoi(m[8][4:6])
		off := h*3600 + mm*60
		if m[8][0] == '-' {
			off = -off
		}
		loc = time.FixedZone("", off)
	}
	p := time.Date(n[1], time.Month(n[2]), n[3], n[4], n[5], n[6], nanos, loc)
	if p.Month() != time.Month(n[2]) || p.Day() != n[3] || n[4] > 23 || n[5] > 59 || n[6] > 59 {
		return time.Time{}, false
	}
	return p, true
}

var timeParts = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})T(\d{2})[:\-](\d{2})[:\-](\d{2})(?:\.(\d{1,9}))?(Z|[+\-]\d{2}:\d{2})?$`)

// formatTime writes a made-up time in UTC with Z, in the form of the time
// it stands for.
func formatTime(t time.Time, form string) string {
	sep := form[0]
	frac, _ := strconv.Atoi(form[1:2])
	layout := "2006-01-02T15" + string(sep) + "04" + string(sep) + "05"
	if frac > 0 {
		layout += "." + strings.Repeat("0", frac)
	}
	return t.UTC().Format(layout) + "Z"
}
