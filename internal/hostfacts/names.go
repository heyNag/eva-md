package hostfacts

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Hash is the recording's keyed hash of a value: HMAC-SHA-256 under the key
// kept in the private folder, cut to 16 hex digits. Equal values have equal
// hashes; without the key a hash says nothing of its value.
func Hash(key []byte, value string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(value))
	return hex.EncodeToString(m.Sum(nil))[:16]
}

// Account is one account on the Mac: its short name, full name and home.
type Account struct {
	Short string `json:"short"`
	Full  string `json:"full"`
	Home  string `json:"home"`
}

// Remote is the scratch repository's address, in its parts.
type Remote struct {
	Address   string `json:"address"`
	Host      string `json:"host"`
	Port      string `json:"port,omitempty"`
	Namespace string `json:"namespace"`
	Path      string `json:"path"`
}

// Names are what is replaced before anything is written under the
// recording folder: the evaluation account's names, every other account's,
// the computer's, and the scratch remote's once it is stored. They are kept
// in the private folder, never in the recording.
type Names struct {
	Eval     Account   `json:"eval"`
	Others   []Account `json:"others"`
	Computer []string  `json:"computer"`
	Remote   *Remote   `json:"remote,omitempty"`
}

// The placeholders a name is replaced by. None holds a name, and none is
// shaped like one of the hosts' values.
const (
	PlaceEvalHome  = "EVAL-HOME"
	PlaceEvalUser  = "EVAL-USER"
	PlaceEvalFull  = "EVAL-FULL-NAME"
	PlaceOtherHome = "OTHER-HOME"
	PlaceOtherUser = "OTHER-USER"
	PlaceOtherFull = "OTHER-FULL-NAME"
	PlaceComputer  = "COMPUTER-NAME"
	PlaceRemote    = "REMOTE-ADDRESS"
	PlaceHost      = "REMOTE-HOST"
	PlacePort      = "REMOTE-PORT"
	PlaceNamespace = "REMOTE-NAMESPACE"
	PlacePath      = "REMOTE-PATH"
)

// separators are what may stand between the words of a name where it
// occurs: spaces, apostrophes of any kind, hyphens, underscores, dots and
// slashes, so "Jo O'Neil" also matches "jo-o’neil" and "/Users/jo" matches
// a host's folder name "-Users-jo".
const separators = "[\\s'’‘`\\-_./\\\\]*"

// Replacer replaces the names in strings, matched without regard to letter
// case, with spaces and apostrophes normalised.
type Replacer struct {
	re    *regexp.Regexp
	place []string
}

// NewReplacer builds the replacer for names. Longer patterns are tried
// first, so a home path goes before the short name inside it.
func NewReplacer(n Names) *Replacer {
	type pattern struct{ re, place string }
	var pats []pattern
	add := func(name, place string) {
		words := splitName(name)
		if len(words) == 0 {
			return
		}
		quoted := make([]string, len(words))
		for i, w := range words {
			quoted[i] = regexp.QuoteMeta(w)
		}
		p := "(" + strings.Join(quoted, separators) + ")"
		// A name of one or two letters is matched only as a word of its
		// own, or it would replace letters in every value.
		if len(strings.Join(words, "")) < 3 {
			p = `(?:^|[^\pL\pN])` + p + `(?:$|[^\pL\pN])`
		}
		pats = append(pats, pattern{p, place})
	}
	add(n.Eval.Home, PlaceEvalHome)
	add(n.Eval.Full, PlaceEvalFull)
	add(n.Eval.Short, PlaceEvalUser)
	for _, a := range n.Others {
		add(a.Home, PlaceOtherHome)
		add(a.Full, PlaceOtherFull)
		add(a.Short, PlaceOtherUser)
	}
	for _, c := range n.Computer {
		add(c, PlaceComputer)
	}
	if r := n.Remote; r != nil {
		add(r.Address, PlaceRemote)
		add(r.Path, PlacePath)
		add(r.Namespace, PlaceNamespace)
		add(r.Host, PlaceHost)
		if r.Port != "" {
			pats = append(pats, pattern{`:(` + regexp.QuoteMeta(r.Port) + `)\b`, PlacePort})
		}
	}
	slices.SortStableFunc(pats, func(a, b pattern) int { return len(b.re) - len(a.re) })
	r := &Replacer{}
	var alts []string
	for _, p := range pats {
		alts = append(alts, "("+p.re+")")
		r.place = append(r.place, p.place)
	}
	if len(alts) > 0 {
		r.re = regexp.MustCompile("(?i)" + strings.Join(alts, "|"))
	}
	return r
}

// splitName gives a name's words: what stands between separators.
func splitName(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("'’‘`-_./\\", r)
	})
}

// String replaces every name in s. Each pattern holds two groups: the whole
// of what it matched, and the name inside it; what a short name's word
// boundary took around the name is kept.
func (r *Replacer) String(s string) string {
	if r == nil || r.re == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range r.re.FindAllStringSubmatchIndex(s, -1) {
		for i, place := range r.place {
			outer, inner := 2+4*i, 4+4*i
			if m[outer] < 0 {
				continue
			}
			b.WriteString(s[last:m[inner]])
			b.WriteString(place)
			b.WriteString(s[m[inner+1]:m[outer+1]])
			last = m[outer+1]
			break
		}
	}
	b.WriteString(s[last:])
	return b.String()
}

// Value replaces the names in every string and object key of a decoded JSON
// value, and returns the result. Numbers and booleans are left as they are.
func (r *Replacer) Value(v any) any {
	switch t := v.(type) {
	case string:
		return r.String(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[r.String(k)] = r.Value(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = r.Value(x)
		}
		return out
	}
	return v
}

// Line encodes v as one JSON line with every name replaced: the only way
// anything is written under the recording folder.
func (r *Replacer) Line(v any) ([]byte, error) {
	data, err := Marshal(v)
	if err != nil {
		return nil, err
	}
	decoded, err := Decode(data)
	if err != nil {
		return nil, err
	}
	out, err := Marshal(r.Value(decoded))
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// Decode decodes one JSON value, keeping numbers as json.Number so none
// changes on the way through.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errTrailing
	}
	return v, nil
}

// Marshal encodes v without escaping <, > and &, so a wrapper's tag stays
// readable in the recording.
func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}
