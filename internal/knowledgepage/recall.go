package knowledgepage

import (
	"cmp"
	"math"
	"math/bits"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Recall's bounds.
const (
	// MaxRecall is how many rows a prompt gets at most.
	MaxRecall = 3
	// MaxTerms is how many of a prompt's distinct content tokens count: of
	// a prompt with more, the first and the last half of them.
	MaxTerms = 64
	// maxPrompt bounds the prompt text read: of a longer prompt, the first
	// and the last half.
	maxPrompt = 64 << 10
	// minToken is the shortest content token, in characters.
	minToken = 3
)

// Floor is the score a strong match must reach, in units of the weight of a
// term only one row holds, so a score does not grow with the number of
// rows. It was chosen on the labelled prompts in recall_test.go, where
// TestRecallPrecision reports the precision and recall it gives at indexes
// of 1, 3, 10, 36 and 500 rows. A match of two terms must name the whole
// title, which keeps out the near misses that share two words of a longer
// title; the floor then keeps out matches made of common words. At 2.5 the
// prompts, each asked in its project, give 100% precision at every size,
// with the fixture's two projects apart and sharing a system page, and 88 to
// 93% recall. It was tuned on fixtures, not on real use.
const Floor = 2.5

// Recall returns the rows that strongly match a prompt, best first: rows
// that hold at least 2 of the prompt's distinct content tokens as whole
// words in their title or statement, and either every content token of their
// title or at least 3 of the prompt's, and whose score reaches Floor. The score sums, over the tokens a row holds, the token's
// inverse document frequency over the rows, ln(1 + (N - df + 0.5) / (df +
// 0.5)), divided by that of a token only one row holds, ln(1 + (N - 0.5) /
// 1.5), and twice over for a token in the title. Only rows in the chat's
// scope count: those of the project, of every project, of the projects that
// share a system page with it, which siblings returns and is asked only when
// a strong row of another project needs it, and of the projects the prompt
// names (see names). Among equal scores, rows of the project or of every
// project come first, then the newest. A row skip reports true for (by its
// id, k<N>) is left out, as is one without a statement. The caller shows at
// most MaxRecall of them, the best that fit.
func Recall(rows []Row, prompt, project string, siblings func() map[string]bool, skip func(id string) bool) []Row {
	var out []Row
	for _, s := range recall(rows, prompt, project, siblings, skip, Floor) {
		out = append(out, s.Row)
	}
	return out
}

// scored is a row Recall keeps, with its score and whether it is of the
// project or of every project.
type scored struct {
	Row
	score float64
	local bool
}

// recall is Recall with the floor given, so a test can measure others, and
// the scores kept.
func recall(rows []Row, prompt, project string, siblings func() map[string]bool, skip func(id string) bool, floor float64) []scored {
	terms := promptTerms(prompt)
	if len(terms) < 2 || len(rows) == 0 {
		return nil
	}
	bit := make(map[string]int, len(terms))
	for i, t := range terms {
		bit[t] = i
	}
	type match struct {
		row                  int
		title, all           uint64 // the terms the title holds, and those the row holds
		titleTokens, matched int
	}
	matches := make([]match, 0, 16)
	var df [MaxTerms]int
	var seen []string
	for i, r := range rows {
		var m match
		seen = seen[:0]
		eachWord(r.Title, func(w []byte) {
			if b, ok := bit[string(w)]; ok {
				m.title |= 1 << b
			}
			if content(w) && !slices.Contains(seen, string(w)) {
				seen = append(seen, string(w))
			}
		})
		m.all = m.title
		eachWord(r.Statement, func(w []byte) {
			if b, ok := bit[string(w)]; ok {
				m.all |= 1 << b
			}
		})
		if m.all == 0 {
			continue
		}
		for b := m.all; b != 0; b &= b - 1 {
			df[bits.TrailingZeros64(b)]++
		}
		if bits.OnesCount64(m.all) >= 2 {
			m.row, m.titleTokens, m.matched = i, len(seen), bits.OnesCount64(m.title)
			matches = append(matches, m)
		}
	}
	// A term only one row holds weighs 1, whatever the number of rows.
	n := float64(len(rows))
	rare := math.Log(1 + (n-0.5)/1.5)
	related := inScope(prompt, project, siblings)
	var out []scored
	for _, m := range matches {
		r := rows[m.row]
		count := bits.OnesCount64(m.all)
		if !(count >= 3 || m.titleTokens > 0 && m.matched == m.titleTokens) || r.Statement == "" || skip != nil && skip(r.ID) {
			continue
		}
		score := 0.0
		for b := m.all; b != 0; b &= b - 1 {
			t := bits.TrailingZeros64(b)
			d := float64(df[t])
			idf := math.Log(1+(n-d+0.5)/(d+0.5)) / rare
			if m.title&(1<<t) != 0 {
				idf *= 2
			}
			score += idf
		}
		if local := r.Scope == ScopeAll || r.Project == project; score >= floor && (local || related(r.Project)) {
			out = append(out, scored{Row: r, score: score, local: local})
		}
	}
	slices.SortStableFunc(out, func(a, b scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		if a.local != b.local {
			if a.local {
				return -1
			}
			return 1
		}
		return cmp.Or(strings.Compare(b.AsOf, a.AsOf), b.At.Compare(a.At))
	})
	return out
}

// Constraints returns the constraint rows that hold for a project: those
// of every project and the project's own, the newest as_of first. The brief
// lists the first few.
func Constraints(rows []Row, project string) []Row {
	var out []Row
	for _, r := range rows {
		if r.Kind == Constraint && (r.Scope == ScopeAll || r.Project == project) {
			out = append(out, r)
		}
	}
	newestFirst(out)
	return out
}

// promptTerms returns a prompt's distinct content tokens in the order it
// first holds them: its runs of letters and digits, lowercased, of at least
// minToken characters and not on the stoplist. Of a prompt over maxPrompt
// bytes it reads the first and the last half; of one with more than
// MaxTerms tokens it keeps the first and the last MaxTerms/2.
func promptTerms(prompt string) []string {
	if len(prompt) > maxPrompt {
		head, tail := prompt[:maxPrompt/2], prompt[len(prompt)-maxPrompt/2:]
		prompt = strings.ToValidUTF8(head, "") + " " + strings.ToValidUTF8(tail, "")
	}
	var terms []string
	seen := map[string]bool{}
	eachWord(prompt, func(w []byte) {
		if content(w) && !seen[string(w)] {
			seen[string(w)] = true
			terms = append(terms, string(w))
		}
	})
	if len(terms) > MaxTerms {
		terms = append(terms[:MaxTerms/2], terms[len(terms)-MaxTerms/2:]...)
	}
	return terms
}

// content reports whether a lowercased word is a content token.
func content(w []byte) bool {
	return utf8.RuneCount(w) >= minToken && !stoplist[string(w)]
}

// words returns s's runs of letters and digits, lowercased, of at least min
// characters.
func words(s string, min int) []string {
	var out []string
	eachWord(s, func(w []byte) {
		if utf8.RuneCount(w) >= min {
			out = append(out, string(w))
		}
	})
	return out
}

// eachWord calls fn with each run of letters and digits in s, lowercased.
// The slice is reused: fn must copy what it keeps.
func eachWord(s string, fn func(w []byte)) {
	buf := make([]byte, 0, 32)
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			i++
			switch {
			case c >= 'a' && c <= 'z' || c >= '0' && c <= '9':
				buf = append(buf, c)
				continue
			case c >= 'A' && c <= 'Z':
				buf = append(buf, c+'a'-'A')
				continue
			}
		} else {
			r, size := utf8.DecodeRuneInString(s[i:])
			i += size
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				buf = utf8.AppendRune(buf, unicode.ToLower(r))
				continue
			}
		}
		if len(buf) > 0 {
			fn(buf)
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		fn(buf)
	}
}

// stoplist holds words too common in prompts to tie one to a fact: function
// words, the words people ask and instruct with, and fillers. Words that
// name a subject, even a common one such as test or file, are not on it:
// how many facts hold a word weighs it already.
var stoplist = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		about above after again against all also although always among and another any anyone anything are aren
		around because been before being below between both but can cannot could couldn did didn does doesn
		doing don done down during each either else even ever every everything few for from further get gets
		getting give given goes going gone got had hadn has hasn have haven having her here hers herself him
		himself his how however into isn its itself just keep know last least less let lets like made make makes
		making many may maybe might mine more most much must mustn need needs neither never next nor not nothing
		now off often once one only onto other others our ours ourselves out over own per please quite rather
		really same see seem seems she should shouldn since some someone something sometimes soon still such
		sure take tell than thank thanks that the their theirs them themselves then there these they thing things
		think this those though through thus too try trying two under unless until upon use used uses using
		very via want wanted wants was wasn way well were weren what whatever when where whether which while who
		whom whose why will with within without won would wouldn yes yet you your yours yourself yourselves
		able actually ago already anyway back best better bit etc okay right stuff today tomorrow yesterday
		new old first second instead look looks looking work works working help hello hey lot lots kind sort
		put said say says tried add change fix show explain
	`) {
		m[w] = true
	}
	return m
}()
