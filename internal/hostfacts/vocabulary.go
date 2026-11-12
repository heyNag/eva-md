package hostfacts

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed vocabulary.json
var vocabularyJSON []byte

// Vocabulary is the closed list of host words the fixtures keep: field
// names, values, the tag names of host wrappers, the bracket forms of
// interrupt notices, environment variables' names, the fields whose numbers
// under 256 are exit codes, and the prefixes of the hosts' ids.
type Vocabulary struct {
	FieldNames      []string `json:"field_names"`
	Values          []string `json:"values"`
	Tags            []string `json:"tags"`
	BracketPrefixes []string `json:"bracket_prefixes"`
	EnvNames        []string `json:"env_names"`
	ExitCodeFields  []string `json:"exit_code_fields"`
	IDPrefixes      []string `json:"id_prefixes"`

	words map[string]bool
	exit  map[string]bool
}

var (
	vocabOnce sync.Once
	vocab     *Vocabulary
)

// Vocab gives the committed vocabulary. It panics if the file does not
// decode, which a test catches first.
func Vocab() *Vocabulary {
	vocabOnce.Do(func() {
		v, err := ParseVocabulary(vocabularyJSON)
		if err != nil {
			panic(err)
		}
		vocab = v
	})
	return vocab
}

// ParseVocabulary decodes a vocabulary file.
func ParseVocabulary(data []byte) (*Vocabulary, error) {
	var v Vocabulary
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	v.words = map[string]bool{}
	for _, list := range [][]string{v.FieldNames, v.Values, v.Tags, v.BracketPrefixes, v.EnvNames} {
		for _, w := range list {
			v.words[w] = true
		}
	}
	v.exit = map[string]bool{}
	for _, f := range v.ExitCodeFields {
		v.exit[f] = true
	}
	return &v, nil
}

// Has reports whether a whole key or value is on the vocabulary.
func (v *Vocabulary) Has(s string) bool { return v.words[s] }

// ExitField reports whether a field's numbers under 256 are exit codes.
func (v *Vocabulary) ExitField(name string) bool { return v.exit[name] }

// Entries gives every word of the vocabulary, for the checks.
func (v *Vocabulary) Entries() []string {
	var out []string
	for _, list := range [][]string{v.FieldNames, v.Values, v.Tags, v.BracketPrefixes, v.EnvNames, v.ExitCodeFields, v.IDPrefixes} {
		out = append(out, list...)
	}
	return out
}

// LeadingTag gives the name of the host wrapper a string begins with, after
// white space: a tag on the vocabulary, as <name> or <name followed by a
// space, or one of the bracket forms, whole.
func (v *Vocabulary) LeadingTag(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t\r\n")
	for _, p := range v.BracketPrefixes {
		if strings.HasPrefix(strings.ToLower(s), strings.ToLower(p)) {
			return p, true
		}
	}
	if !strings.HasPrefix(s, "<") {
		return "", false
	}
	for _, t := range v.Tags {
		rest, ok := strings.CutPrefix(s[1:], t)
		if ok && (strings.HasPrefix(rest, ">") || strings.HasPrefix(rest, " ")) {
			return t, true
		}
	}
	return "", false
}

// UnlistedTag gives the name of a leading tag that is not on the
// vocabulary: < or [ followed by 1 to 24 lower-case letters, digits, _ and
// -, the run ending there.
func UnlistedTag(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t\r\n")
	if s == "" || s[0] != '<' && s[0] != '[' {
		return "", false
	}
	n := 0
	for n+1 < len(s) && isTagByte(s[n+1]) {
		n++
	}
	if n == 0 || n > 24 {
		return "", false
	}
	return s[1 : n+1], true
}

func isTagByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}
