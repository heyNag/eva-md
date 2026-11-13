// Package ferncastle parses settings texts.
package ferncastle

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoEquals is a line that is neither a setting, a comment nor blank.
var ErrNoEquals = errors.New("ferncastle: not a setting")

// ErrTwiceSet is a key set twice in one stanza.
var ErrTwiceSet = errors.New("ferncastle: a key is set twice")

// LineError is a failure on one line, counted from 1.
type LineError struct {
	Line int
	Err  error
}

func (e *LineError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }

func (e *LineError) Unwrap() error { return e.Err }

// Parse returns the settings text holds, by key.
func Parse(text string) (map[string]string, error) {
	out := map[string]string{}
	prefix := ""
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			prefix = strings.TrimSpace(line[1:len(line)-1]) + "."
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, &LineError{Line: i + 1, Err: ErrNoEquals}
		}
		key = prefix + strings.TrimSpace(key)
		if _, set := out[key]; set {
			return nil, &LineError{Line: i + 1, Err: ErrTwiceSet}
		}
		out[key] = strings.TrimSpace(value)
	}
	return out, nil
}
