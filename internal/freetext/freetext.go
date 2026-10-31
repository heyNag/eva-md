// Package freetext cleans free text the way eva-md shows it to a model, in
// the resume brief and in the update input. Speaker labels come only from
// entry fields, so no line of free text may read as one.
package freetext

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/heyNag/eva-md/internal/redact"
)

// speakerTag matches a speaker label eva-md writes, in a line already
// normalized by SpeakerShaped.
var speakerTag = regexp.MustCompile(`^\[ ?(user|assistant|host ?summary|host|tool|plan|event|unknown) ?\]`)

// SpeakerShaped reports whether line would read as a speaker header. It
// ignores what the brief's fence strips (control characters) or hides
// (format characters), treats every kind of space alike and ignores case.
func SpeakerShaped(line string) bool {
	b := make([]rune, 0, 32)
	for _, r := range line {
		switch {
		case r == '\t':
			r = ' '
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf):
			continue
		case unicode.IsSpace(r) || unicode.In(r, unicode.Z):
			r = ' '
		default:
			r = unicode.ToLower(r)
		}
		if r == ' ' && (len(b) == 0 || b[len(b)-1] == ' ') {
			continue
		}
		if b = append(b, r); len(b) == cap(b) {
			break
		}
	}
	return speakerTag.MatchString(string(b))
}

// Escape marks each line of cleaned text that would read as a speaker header.
func Escape(s string) string {
	if !strings.Contains(s, "[") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if SpeakerShaped(line) {
			lines[i] = "[quoted] " + line
		}
	}
	return strings.Join(lines, "\n")
}

// Shown is free text as the brief shows it: cleaned, with every line that
// would read as a speaker header escaped. Its length is the length the
// brief's fence keeps.
func Shown(s string) string { return Escape(Clean(s)) }

// Clean redacts s, removes control characters other than line breaks and
// tabs, turns Unicode line and paragraph separators into spaces, and quotes
// a line that starts like the brief's own notes.
func Clean(s string) string {
	s, _ = redact.Redact(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		// Unicode line and paragraph separators could act as line breaks
		// that skip the quoting indent.
		if r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "eva.md notes:") {
			line = "[quoted] " + line
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
