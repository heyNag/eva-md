// Package quillmark counts what a text holds and renders a short report.
package quillmark

import (
	"fmt"
	"strings"
)

// Summary is what a text holds.
type Summary struct {
	Words int
	Lines int
}

// Summarize counts the words and lines of text.
func Summarize(text string) Summary {
	s := Summary{Words: len(strings.Fields(text))}
	if text != "" {
		s.Lines = strings.Count(text, "\n")
		if !strings.HasSuffix(text, "\n") {
			s.Lines++
		}
	}
	return s
}

// Render gives the report, one line a figure, in the order NOTES.md gives.
func Render(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "wordtally: %d\n", s.Words)
	fmt.Fprintf(&b, "linetally: %d\n", s.Lines)
	return b.String()
}
