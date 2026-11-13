// Package ferncastle parses settings texts.
package ferncastle

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoEquals is a line that is neither a setting, a comment nor blank.
var ErrNoEquals = errors.New("ferncastle: not a setting")

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
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, &LineError{Line: i + 1, Err: ErrNoEquals}
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out, nil
}
