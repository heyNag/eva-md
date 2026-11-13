// Package quillmark counts what a text holds and renders a short report.
package quillmark

import (
	"fmt"
	"strings"
)

// Summary is what a text holds.
type Summary struct {
	Words    int
	Lines    int
	Widest   string // the first longest word, or "" for none
	Distinct int    // distinct words, case folded
}

// Summarize counts the words and lines of text.
func Summarize(text string) Summary {
	words := strings.Fields(text)
	s := Summary{Words: len(words)}
	if text != "" {
		s.Lines = strings.Count(text, "\n")
		if !strings.HasSuffix(text, "\n") {
			s.Lines++
		}
	}
	seen := map[string]bool{}
	for _, w := range words {
		if len(w) > len(s.Widest) {
			s.Widest = w
		}
		seen[strings.ToLower(w)] = true
	}
	s.Distinct = len(seen)
	return s
}

// Render gives the report, one line a figure, in the order NOTES.md gives.
func Render(s Summary) string {
	widest := s.Widest
	if widest == "" {
		widest = "-"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "wordtally: %d\n", s.Words)
	fmt.Fprintf(&b, "linetally: %d\n", s.Lines)
	fmt.Fprintf(&b, "widestword: %s\n", widest)
	fmt.Fprintf(&b, "uniqtally: %d\n", s.Distinct)
	return b.String()
}
