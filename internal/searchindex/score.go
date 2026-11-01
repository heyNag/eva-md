package searchindex

import (
	"math"
	"strings"
)

// BM25's parameters.
const (
	k1 = 1.2
	b  = 0.75
)

// Query is a search's terms as the index reads them: each term's tokens, a
// phrase holding several, MaxTerms at most in all.
type Query struct {
	terms  [][]int // each term's tokens, as indexes into tokens
	tokens []qtoken
	plain  bool
}

type qtoken struct {
	text   string
	prefix bool // 3 or more characters: it matches a token that starts with it
}

// NewQuery returns the query for terms, each a word or a phrase as search
// keeps it. ok is false when a term holds no token or one over MaxToken
// bytes, or the terms hold more than MaxTerms tokens in all; search then
// scans as it would without an index.
func NewQuery(terms []string) (q *Query, ok bool) {
	q = &Query{plain: true}
	for _, term := range terms {
		tokens, ok := QueryTokens(term)
		if !ok {
			return nil, false
		}
		var idx []int
		for _, t := range tokens {
			idx = append(idx, len(q.tokens))
			q.tokens = append(q.tokens, qtoken{text: t, prefix: Prefixable(t)})
		}
		q.terms = append(q.terms, idx)
		if len(tokens) != 1 || !Prefixable(tokens[0]) || tokens[0] != strings.ToLower(term) {
			q.plain = false
		}
	}
	if len(q.tokens) > MaxTerms {
		return nil, false
	}
	return q, true
}

// Plain reports whether every term is a single word of 3 or more letters and
// digits. A document the index finds holding such a term always holds it as
// search matches it, so the candidates past those read again count as hits.
func (q *Query) Plain() bool { return q.plain }

// matched counts the terms all of whose tokens are in mask, a bit a token.
func (q *Query) matched(mask uint32) int {
	n := 0
	for _, term := range q.terms {
		all := true
		for _, i := range term {
			all = all && mask&(1<<i) != 0
		}
		if all {
			n++
		}
	}
	return n
}

// Stats are what a search's scores pool over the segments it opened: their
// documents, the documents' average length, and for each query token the
// documents holding the dictionary terms it matches, summed over those
// terms, an upper bound.
type Stats struct {
	N, AvgLen float64
	DF        []float64
	// kept holds, for a token whose dictionary terms were more than
	// MaxExpansions, the ones kept; nil for every other token.
	kept []map[string]bool
}

// BM25 is a token's part of a document's score: the inverse document
// frequency okf's search uses, ln(1 + (N − df + 0.5)/(df + 0.5)), times the
// token's frequency in the document, saturated by k1 and weighed against
// the document's length over the average by b. A word's df sums its
// dictionary terms' and so can pass N; it is held to N.
func BM25(tf, df, n, dl, avg float64) float64 {
	df = min(df, n)
	idf := math.Log(1 + (n-df+0.5)/(df+0.5))
	norm := 1.0
	if avg > 0 {
		norm = 1 - b + b*dl/avg
	}
	return idf * tf * (k1 + 1) / (tf + k1*norm)
}

// Score scores a text the index doesn't cover, such as a page line or the
// inbox's newest lines, as the index scores a document: its tokens and its
// length as Tokenize counts them, a tool line weighed at the average
// length, and the pooled statistics. Without statistics it is 0.
func (q *Query) Score(st Stats, text string, tool bool) float64 {
	if st.N == 0 || len(st.DF) != len(q.tokens) {
		return 0
	}
	var tf [MaxTerms]float64
	dl := tokenize(text, func(t []byte) {
		for i, qt := range q.tokens {
			if qt.matches(t, st.keptFor(i)) {
				tf[i]++
			}
		}
	})
	l := float64(min(dl, math.MaxUint16))
	if tool {
		l = st.AvgLen
	}
	score := 0.0
	for i := range q.tokens {
		if tf[i] > 0 {
			score += BM25(tf[i], st.DF[i], st.N, l, st.AvgLen)
		}
	}
	return score
}

// keptFor returns the dictionary terms kept for query token i, or nil when
// all of them were.
func (st Stats) keptFor(i int) map[string]bool {
	if i < len(st.kept) {
		return st.kept[i]
	}
	return nil
}

// matches reports whether a document's token t is one the query token
// counts: itself, or for a prefix token one that starts with it, and only
// one of those kept when they were more than MaxExpansions.
func (qt qtoken) matches(t []byte, kept map[string]bool) bool {
	if len(t) < len(qt.text) || string(t[:len(qt.text)]) != qt.text || len(t) > len(qt.text) && !qt.prefix {
		return false
	}
	return kept == nil || kept[string(t)]
}
