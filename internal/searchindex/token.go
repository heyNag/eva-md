package searchindex

import (
	"unicode"
	"unicode/utf8"
)

// maxParts is the most parts a run is split into: a run of more, such as a
// hex hash, is indexed only whole.
const maxParts = 8

// Tokenize calls emit with each token of s, in order, and returns how many
// it emitted, s's length as a score counts it. A token is a run of letters
// and numbers, lowered rune by rune as strings.ToLower lowers it, so ß stays
// ß. A run with camelCase or letter and digit boundaries also emits each of
// its parts, when it has 8 or fewer: ReadMax gives readmax, read and max,
// and gpt5 gives gpt5, gpt and 5. A run over MaxToken bytes is dropped,
// parts and all.
func Tokenize(s string, emit func(string)) int {
	return tokenize(s, func(b []byte) { emit(string(b)) })
}

// tokenize is Tokenize with each token passed as bytes that are valid only
// during the call.
func tokenize(s string, emit func([]byte)) int {
	var low [MaxToken * utf8.UTFMax]byte
	var cuts [maxParts]int
	n := 0
	runs(s, func(rs []rune) {
		if rs == nil {
			return
		}
		b, nc := low[:0], 0
		for j, r := range rs {
			if j > 0 && boundary(rs, j) {
				if nc < len(cuts) {
					cuts[nc] = len(b)
				}
				nc++
			}
			b = utf8.AppendRune(b, unicode.ToLower(r))
		}
		if len(b) > MaxToken {
			return
		}
		emit(b)
		n++
		if nc == 0 || nc >= maxParts {
			return
		}
		start := 0
		for k := 0; k <= nc; k++ {
			end := len(b)
			if k < nc {
				end = cuts[k]
			}
			emit(b[start:end])
			n++
			start = end
		}
	})
	return n
}

// runs calls visit with each run of letters and numbers in s, as runes. A
// run of more than MaxToken runes, which can only be over MaxToken bytes,
// comes as nil. The runes are valid only during the call.
func runs(s string, visit func([]rune)) {
	var buf [MaxToken]rune
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !alnum(r) {
			i += size
			continue
		}
		count := 0
		for i < len(s) {
			r, size = utf8.DecodeRuneInString(s[i:])
			if !alnum(r) {
				break
			}
			if count < len(buf) {
				buf[count] = r
			}
			count++
			i += size
		}
		if count > len(buf) {
			visit(nil)
			continue
		}
		visit(buf[:count])
	}
}

// boundary reports whether a part of the run rs starts at rune j: between a
// letter and a digit, at a capital after a lower-case letter, and at the
// last capital of a run of them before a lower-case letter, as in HTTPServer.
func boundary(rs []rune, j int) bool {
	prev, cur := rs[j-1], rs[j]
	switch {
	case unicode.IsNumber(prev) != unicode.IsNumber(cur):
		return true
	case unicode.IsLower(prev) && unicode.IsUpper(cur):
		return true
	}
	return unicode.IsUpper(prev) && unicode.IsUpper(cur) && j+1 < len(rs) && unicode.IsLower(rs[j+1])
}

func alnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

// QueryTokens returns the tokens of one search term: its runs of letters
// and numbers, lowered as Tokenize lowers them, without the parts a run in
// a document adds. ok is false when the term holds no token, or one over
// MaxToken bytes, which the index can't look up.
func QueryTokens(term string) (tokens []string, ok bool) {
	ok = true
	runs(term, func(rs []rune) {
		var b []byte
		for _, r := range rs {
			b = utf8.AppendRune(b, unicode.ToLower(r))
		}
		if rs == nil || len(b) > MaxToken {
			ok = false
			return
		}
		tokens = append(tokens, string(b))
	})
	if !ok || len(tokens) == 0 {
		return nil, false
	}
	return tokens, true
}

// Prefixable reports whether a query token is 3 or more characters long, so
// that it matches any token that starts with it rather than only its equal.
func Prefixable(token string) bool { return utf8.RuneCountInString(token) >= 3 }
