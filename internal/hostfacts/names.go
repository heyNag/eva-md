package hostfacts

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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
// case, with spaces and apostrophes normalised, and only where a name
// stands as a word of its own: what stands just before and just after it
// is the start or the end of the string, or a character that is not a
// letter, a digit or an underscore. A path, a host's folder name such as
// "-Users-jo", an address or a possessive still match; a name inside
// another word, such as "tim" in "timestamp", "art" in "SessionStart" or
// "event" in "hook_event_name", is left alone.
type Replacer struct {
	any  *regexp.Regexp
	pats []namePattern
}

// namePattern is one name to replace: a pattern whose first group is the
// name, its placeholder, and the name's length in letters and digits, by
// which the longest is replaced first.
type namePattern struct {
	re    *regexp.Regexp
	place string
	size  int
}

// NewReplacer builds the replacer for names. The longest names are taken
// first, so a home path or a full name goes before a short name inside it,
// and what one name took no other takes.
func NewReplacer(n Names) *Replacer {
	r := &Replacer{}
	add := func(name, place string) {
		p := namePatternOf(name)
		if p == "" {
			return
		}
		r.pats = append(r.pats, namePattern{regexp.MustCompile("(?i)(" + p + ")"), place, len(strings.Join(splitName(name), ""))})
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
	if rm := n.Remote; rm != nil {
		add(rm.Address, PlaceRemote)
		add(rm.Path, PlacePath)
		add(rm.Namespace, PlaceNamespace)
		add(rm.Host, PlaceHost)
		if rm.Port != "" {
			// A port is replaced only after a colon, as in host:2222.
			r.pats = append(r.pats, namePattern{regexp.MustCompile(`:(` + regexp.QuoteMeta(rm.Port) + `)`), PlacePort, 0})
		}
	}
	slices.SortStableFunc(r.pats, func(a, b namePattern) int { return b.size - a.size })
	var alts []string
	for _, p := range r.pats {
		alts = append(alts, p.re.String())
	}
	if len(alts) > 0 {
		r.any = regexp.MustCompile(strings.Join(alts, "|"))
	}
	return r
}

// splitName gives a name's words: what stands between separators.
func splitName(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("'’‘`-_./\\", r)
	})
}

// namePatternOf is the pattern a name is matched by: its words, in any
// case, with any separators between them.
func namePatternOf(name string) string {
	words := splitName(name)
	if len(words) == 0 {
		return ""
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	return strings.Join(quoted, separators)
}

// wordEdge reports whether s[a:b] stands as a word of its own: the start
// or the end of s, or a character that is not a letter, a digit or an
// underscore, on each side. An underscore joins the words of the hosts'
// field names, as hook_event_name, so a name is not taken from inside one.
func wordEdge(s string, a, b int) bool {
	if a > 0 {
		if r, _ := utf8.DecodeLastRuneInString(s[:a]); wordRune(r) {
			return false
		}
	}
	if b < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[b:]); wordRune(r) {
			return false
		}
	}
	return true
}

func wordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// joinedEdge reports whether s[a:b] stands as a word of its own, or is
// joined to another word only by an underscore or a digit, as a name is in
// "<name>_backup" or "<name>2": no letter stands just before or just after
// it. A name inside a run of hex digits, such as the recording's hashes, a
// UUID or a commit's id, is inside another word all the same.
func joinedEdge(s string, a, b int) bool {
	if wordEdge(s, a, b) {
		return true
	}
	if a > 0 {
		if r, _ := utf8.DecodeLastRuneInString(s[:a]); unicode.IsLetter(r) {
			return false
		}
	}
	if b < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[b:]); unicode.IsLetter(r) {
			return false
		}
	}
	lo, hi := a, b
	for lo > 0 && runByte(s[lo-1]) {
		lo--
	}
	for hi < len(s) && runByte(s[hi]) {
		hi++
	}
	hex := 0
	for i := lo; i < hi; i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F':
			hex++
		case c != '-':
			return true
		}
	}
	return hex < 8
}

func runByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-'
}

// eachName calls take for every place re's first group matches in s, with
// whether it stands as a word of its own. When take takes the match, the
// search goes on after it; when it does not, the search goes on one
// character later, so a match inside a word hides no word that starts in
// it, and two names side by side, as "jo jo", are both found.
func eachName(re *regexp.Regexp, s string, take func(a, b int, word bool) bool) {
	eachNameAt(re, s, wordEdge, take)
}

// eachNameAt is eachName with edge in place of wordEdge.
func eachNameAt(re *regexp.Regexp, s string, edge func(s string, a, b int) bool, take func(a, b int, word bool) bool) {
	for pos := 0; pos < len(s); {
		m := re.FindStringSubmatchIndex(s[pos:])
		if m == nil || m[2] < 0 {
			return
		}
		a, b := pos+m[2], pos+m[3]
		if take(a, b, edge(s, a, b)) {
			pos += max(m[1], m[0]+1)
			continue
		}
		_, size := utf8.DecodeRuneInString(s[pos+m[0]:])
		pos += m[0] + max(size, 1)
	}
}

// String replaces every name in s.
func (r *Replacer) String(s string) string {
	if r == nil || r.any == nil || !r.any.MatchString(s) {
		return s
	}
	type span struct {
		a, b  int
		place string
	}
	var spans []span
	free := func(a, b int) bool {
		for _, sp := range spans {
			if a < sp.b && sp.a < b {
				return false
			}
		}
		return true
	}
	for _, p := range r.pats {
		eachName(p.re, s, func(a, b int, word bool) bool {
			if !word || !free(a, b) {
				return false
			}
			spans = append(spans, span{a, b, p.place})
			return true
		})
	}
	if len(spans) == 0 {
		return s
	}
	slices.SortFunc(spans, func(x, y span) int { return x.a - y.a })
	var out strings.Builder
	last := 0
	for _, sp := range spans {
		out.WriteString(s[last:sp.a])
		out.WriteString(sp.place)
		last = sp.b
	}
	out.WriteString(s[last:])
	return out.String()
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
// anything is written under the recording folder. The field names of the
// recorder's own types are its own words and are kept; every key and
// string a host gave, and every string the recorder wrote, is replaced.
func (r *Replacer) Line(v any) ([]byte, error) {
	data, err := Marshal(v)
	if err != nil {
		return nil, err
	}
	decoded, err := Decode(data)
	if err != nil {
		return nil, err
	}
	out, err := Marshal(r.typed(decoded, reflect.TypeOf(v)))
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// typed replaces the names in a decoded value whose Go type was t: a
// struct's field names are kept, and everything else is replaced as Value
// replaces it.
func (r *Replacer) typed(v any, t reflect.Type) any {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return r.Value(v)
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return r.Value(v)
		}
		fields := jsonFields(t)
		out := make(map[string]any, len(m))
		for k, x := range m {
			if ft, ok := fields[k]; ok {
				out[k] = r.typed(x, ft)
			} else {
				out[r.String(k)] = r.Value(x)
			}
		}
		return out
	case reflect.Slice, reflect.Array:
		a, ok := v.([]any)
		if !ok {
			return r.Value(v)
		}
		out := make([]any, len(a))
		for i, x := range a {
			out[i] = r.typed(x, t.Elem())
		}
		return out
	case reflect.Map:
		m, ok := v.(map[string]any)
		if !ok {
			return r.Value(v)
		}
		out := make(map[string]any, len(m))
		for k, x := range m {
			out[r.String(k)] = r.typed(x, t.Elem())
		}
		return out
	}
	return r.Value(v)
}

// jsonFields gives a struct's fields by the names JSON writes them under.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch {
		case name == "-" || !f.IsExported() && !f.Anonymous:
			continue
		case f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct:
			for k, ft := range jsonFields(f.Type) {
				out[k] = ft
			}
			continue
		case name == "":
			name = f.Name
		}
		out[name] = f.Type
	}
	return out
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
