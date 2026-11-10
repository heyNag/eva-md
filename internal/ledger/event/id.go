package event

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

// ID is an event's identity: its session's host and id, and its seq.
type ID struct {
	Host    string
	Session string
	Seq     int64
}

// String is the full form, <host>:<session>#<seq>.
func (id ID) String() string {
	return id.Host + ":" + id.Session + "#" + strconv.FormatInt(id.Seq, 10)
}

// Ref is the display form of the id, e:<first 8 of session>#<seq>.
func (id ID) Ref() string {
	return "e:" + Prefix(id.Session) + "#" + strconv.FormatInt(id.Seq, 10)
}

// ParseID reads the full form of an id.
func ParseID(s string) (ID, error) {
	host, rest, ok := strings.Cut(s, ":")
	i := strings.LastIndexByte(rest, '#')
	if !ok || i < 0 || !validHost(host) || !ValidSession(rest[:i]) {
		return ID{}, errors.New("event: invalid id")
	}
	seq, ok := number(rest[i+1:])
	if !ok {
		return ID{}, errors.New("event: invalid id")
	}
	return ID{Host: host, Session: rest[:i], Seq: seq}, nil
}

// Prefix is the part of a session id a short reference keeps: its first 8
// characters, or all of it when it is shorter, as v1's references keep it.
func Prefix(session string) string {
	runes := []rune(session)
	if len(runes) > 8 {
		runes = runes[:8]
	}
	return string(runes)
}

// Ref is a short reference to an event or a range of events of one
// session: e:<session>#<first>, e:<session>#<first>-<last>, or v1's form,
// s:<session>#<line>. Session is the session's first 8 characters or its
// whole id. A v1 reference names a v1 line, which the v1 reader yields as
// the event whose seq is that line.
type Ref struct {
	Session     string
	First, Last int64 // equal for a single event
	Legacy      bool  // written as s:, by v1
}

// ParseRef reads a short reference.
func ParseRef(s string) (Ref, error) {
	var r Ref
	rest, ok := strings.CutPrefix(s, "e:")
	if !ok {
		rest, ok = strings.CutPrefix(s, "s:")
		r.Legacy = true
	}
	i := strings.LastIndexByte(rest, '#')
	if !ok || i < 0 || !ValidSession(rest[:i]) || len(rest[:i]) > 128 {
		return Ref{}, errors.New("event: invalid reference")
	}
	r.Session = rest[:i]
	first, last, isRange := strings.Cut(rest[i+1:], "-")
	if r.First, ok = number(first); !ok {
		return Ref{}, errors.New("event: invalid reference")
	}
	r.Last = r.First
	if isRange {
		if r.Last, ok = number(last); r.Legacy || !ok || r.Last <= r.First {
			return Ref{}, errors.New("event: invalid reference")
		}
	}
	return r, nil
}

// String writes the reference in the form it was read.
func (r Ref) String() string {
	s := "e:"
	if r.Legacy {
		s = "s:"
	}
	s += r.Session + "#" + strconv.FormatInt(r.First, 10)
	if r.Last != r.First {
		s += "-" + strconv.FormatInt(r.Last, 10)
	}
	return s
}

// Names reports whether the reference names the session id: in full, or
// by its first 8 characters.
func (r Ref) Names(session string) bool {
	return r.Session == session || r.Session == Prefix(session)
}

// ValidSession reports a session id a reference or an id can carry: not
// empty, at most 512 bytes, and free of control and format characters,
// slashes, '#' and NUL.
func ValidSession(s string) bool {
	if s == "" || len(s) > 512 || strings.ContainsAny(s, "/\\#") {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) || unicode.In(c, unicode.Cf) {
			return false
		}
	}
	return true
}

func validHost(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// number reads a positive decimal with no sign and no leading zero.
func number(s string) (int64, bool) {
	if s == "" || s[0] == '0' || len(s) > 18 {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}
