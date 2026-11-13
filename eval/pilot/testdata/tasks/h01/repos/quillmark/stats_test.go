package quillmark

import (
	"strings"
	"testing"
)

func TestSummarize(t *testing.T) {
	cases := []struct {
		text         string
		words, lines int
	}{
		{"", 0, 0},
		{"one two\nthree\n", 3, 2},
		{"no newline at the end", 5, 1},
	}
	for _, c := range cases {
		s := Summarize(c.text)
		if s.Words != c.words || s.Lines != c.lines {
			t.Errorf("Summarize(%q) = %+v, want %d words and %d lines", c.text, s, c.words, c.lines)
		}
	}
}

func TestRenderStartsWithTheTallies(t *testing.T) {
	got := Render(Summarize("one two\nthree\n"))
	if !strings.HasPrefix(got, "wordtally: 3\nlinetally: 2\n") {
		t.Errorf("Render = %q", got)
	}
}
