package searchindex

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// scored opens the general area's index and returns the statistics and the
// candidates of terms, every document live.
func scored(t *testing.T, d paths.Dirs, k int, terms ...string) (*Query, Stats, []Candidate, []Candidate, int, int, *Index) {
	t.Helper()
	q, ok := NewQuery(terms)
	if !ok {
		t.Fatalf("%q is left to the scan", terms)
	}
	ix, unusable := Open(d, []string{"general"})
	t.Cleanup(ix.Close)
	if len(unusable) > 0 {
		t.Fatal(unusable)
	}
	st, _ := ix.Stats(q)
	all, some, nAll, nSome, _ := ix.Candidates(q, st, func(string, string, uint32) bool { return true }, math.MinInt64, k)
	return q, st, all, some, nAll, nSome, ix
}

// The window applies to each document's own date: one before it, or with a
// date that can't be read, is not a candidate.
func TestQueryDaysOnTimestamps(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "days", 1, "plum early")
	prompt(t, d, "days", 100, "plum late")
	e := `{"v":1,"ts":"not a date","host":"codex","session":"days","role":"user","kind":"prompt","text":"plum undated","project":"general","origin":"hook"}` + "\n"
	f, err := openAppend(sessionPath(d, "days"))
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(e)
	f.Close()
	catchupOK(t, d)
	if all, _ := lookup(t, d, noWindow(), "plum"); len(all) != 3 {
		t.Fatalf("no window: %v", all)
	}
	if all, _ := lookup(t, d, base.Add(50*time.Minute).Unix(), "plum"); texts(all) != "plum late" {
		t.Fatalf("a window: %v", all)
	}
}

// A word of 3 or more characters matches the tokens it starts, a camelCase
// part among them; a shorter one only an equal token, never inside one.
func TestPrefixAndShortTokens(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "p1", 1, "the SecretLines helper")
	prompt(t, d, "p2", 2, "the UI is ready")
	prompt(t, d, "p3", 3, "GUI build broke")
	prompt(t, d, "p4", 4, "ui-kit shipped")
	prompt(t, d, "p5", 5, "secrets kept")
	catchupOK(t, d)
	for _, tc := range []struct {
		term, want string
	}{
		{"sec", "secrets kept|the SecretLines helper"},
		{"lines", "the SecretLines helper"},
		{"se", ""},
		{"ui", "the UI is ready|ui-kit shipped"},
		{"gui", "GUI build broke"},
		{"Helper", "the SecretLines helper"},
	} {
		if all, _ := lookup(t, d, noWindow(), tc.term); sortedTexts(all) != tc.want {
			t.Errorf("%s: %q", tc.term, sortedTexts(all))
		}
	}
}

// The scores are BM25 as written: okf's idf, saturated term frequency and
// length against the average, with a tool line weighed at the average
// length; a word's dictionary terms count as one term, their documents and
// their counts summed.
func TestBM25HandComputed(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "bm", 1, "kiwi kiwi kiwi")                  // 3 tokens, tf 3
	prompt(t, d, "bm", 2, "kiwi and many other plain words") // 6 tokens, tf 1
	say(t, d, "bm", 3, "tool", "tool", "Read kiwi.go")       // 3 tokens, tf 1, a tool line
	prompt(t, d, "bm", 4, "kiwis kiwifruit")                 // 2 tokens, two expansions
	catchupOK(t, d)
	_, st, all, _, _, _, ix := scored(t, d, 10, "kiwi")
	n, avg := 4.0, (3.0+6+3+2)/4
	if st.N != n || st.AvgLen != avg || st.DF[0] != 5 {
		t.Fatalf("statistics: %+v", st)
	}
	df := 4.0 // held to N
	idf := math.Log(1 + (n-df+0.5)/(df+0.5))
	bm := func(tf, dl float64) float64 { return idf * tf * (k1 + 1) / (tf + k1*(1-b+b*dl/avg)) }
	want := map[string]float64{"kiwi kiwi kiwi": bm(3, 3), "kiwi and many other plain words": bm(1, 6), "Read kiwi.go": bm(1, avg), "kiwis kiwifruit": bm(2, 2)}
	if len(all) != 4 {
		t.Fatalf("%d candidates", len(all))
	}
	for i := range all {
		if err := ix.Locate(&all[i]); err != nil {
			t.Fatal(err)
		}
		c := all[i]
		text := entryText(t, readAt(t, d, c))
		if math.Abs(c.Score-want[text]) > 1e-12 {
			t.Errorf("%q scored %v, want %v", text, c.Score, want[text])
		}
		if i > 0 && all[i-1].Score < c.Score {
			t.Errorf("candidates out of order at %d", i)
		}
	}
}

// Scoring a text with Query.Score gives the score the index gives the same
// document, so lines scanned and lines the index proposes rank together.
func TestScoreMatchesTheIndex(t *testing.T) {
	d := fixture(t)
	lines := []string{"alpha beta gamma", "Alpha alpha BetaMax", "gamma, beta and others", "alphabet soup", "the beta release of gpt5"}
	for i, line := range lines {
		prompt(t, d, "agree", i, line)
	}
	say(t, d, "agree", 9, "tool", "tool", "Bash: grep alpha beta")
	catchupOK(t, d)
	for _, terms := range [][]string{{"alpha"}, {"beta", "gamma"}, {"alp", "gpt"}, {"beta release"}} {
		q, st, all, some, _, _, ix := scored(t, d, 50, terms...)
		for _, list := range [][]Candidate{all, some} {
			for i := range list {
				if err := ix.Locate(&list[i]); err != nil {
					t.Fatal(err)
				}
				text := entryText(t, readAt(t, d, list[i]))
				_, _, kind := list[i].seg.Meta(list[i].doc)
				if got := q.Score(st, text, kind == kindTool); got != list[i].Score {
					t.Errorf("%q, %q: Score %v, the index %v", terms, text, got, list[i].Score)
				}
			}
		}
	}
}

// The candidates holding every term are kept apart from those holding
// some, each list bounded, best first, with every one counted.
func TestCandidatesKeepTheBestK(t *testing.T) {
	d := fixture(t)
	for i := 0; i < 300; i++ {
		text := "mango " + strings.Repeat("filler ", i%20)
		if i%3 == 0 {
			text += " papaya"
		}
		bulk(t, d, fmt.Sprintf("k%d", i%7), i, text)
	}
	catchupOK(t, d)
	_, _, all, some, nAll, nSome, _ := scored(t, d, 40, "mango", "papaya")
	if nAll != 100 || nSome != 200 || len(all) != 40 || len(some) != 40 {
		t.Fatalf("all %d of %d, some %d of %d", len(all), nAll, len(some), nSome)
	}
	for i := 1; i < len(all); i++ {
		if byScore(&all[i], &all[i-1]) {
			t.Fatalf("all out of order at %d", i)
		}
	}
	for _, c := range some {
		if c.Matched != 1 {
			t.Fatalf("a candidate holding some held %d terms", c.Matched)
		}
	}
	// The shortest documents score best: each length from 2 tokens holds 5
	// of the 100, so the best 40 have at most 9.
	if dl := all[39].seg.dl[all[39].doc]; dl > 9 {
		t.Fatalf("the 40th best has %d tokens", dl)
	}
}

// A word with more dictionary terms than MaxExpansions keeps those in the
// most documents, the same in the statistics and the scores.
func TestExpansionsAreCapped(t *testing.T) {
	d := fixture(t)
	// Letters only, so each word is one token with no parts.
	word := func(i int) string { return "capword" + string(rune('a'+i/26)) + string(rune('a'+i%26)) }
	for i := 0; i < MaxExpansions+20; i++ {
		bulk(t, d, "cap", i, word(i))
		if i >= MaxExpansions+10 {
			// The last 10 words are in a second document each.
			bulk(t, d, "cap", 1000+i, word(i)+" spare")
		}
	}
	catchupOK(t, d)
	q, st, all, _, nAll, _, _ := scored(t, d, 1000, "capword")
	if len(st.kept[0]) != MaxExpansions || nAll != MaxExpansions+10 || len(all) != nAll {
		t.Fatalf("kept %d terms, %d candidates", len(st.kept[0]), nAll)
	}
	for i := MaxExpansions + 10; i < MaxExpansions+20; i++ {
		if !st.kept[0][word(i)] {
			t.Fatalf("%s, in two documents, was dropped", word(i))
		}
	}
	// In one document each, the rest are kept in byte order: the last 20 of
	// them are dropped.
	if dropped := word(MaxExpansions + 9); st.kept[0][dropped] || q.Score(st, dropped, false) != 0 || q.Score(st, word(0), false) == 0 {
		t.Fatal("Score counts a term the cap dropped, or misses one it kept")
	}
}
