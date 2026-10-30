package session

import (
	"strings"
	"unicode"
)

// RecordControl returns "off_record" or "on_record" when the prompt starts
// with that phrase or has a line that is only the phrase, ignoring case and
// punctuation, and "" otherwise. It is shared by capture and recovery, so
// missing hooks cannot weaken an off-the-record boundary through punctuation
// or indentation.
func RecordControl(prompt string) string {
	words := func(s string) string {
		return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if unicode.IsPunct(r) || unicode.IsSymbol(r) {
				return ' '
			}
			return unicode.ToLower(r)
		}, s)), " ")
	}
	for _, control := range []struct{ phrase, event string }{{"off the record", "off_record"}, {"back on the record", "on_record"}} {
		normal := words(prompt)
		if normal == control.phrase || strings.HasPrefix(normal, control.phrase+" ") {
			return control.event
		}
		for _, line := range strings.Split(prompt, "\n") {
			if words(line) == control.phrase {
				return control.event
			}
		}
	}
	return ""
}
