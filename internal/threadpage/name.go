package threadpage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

// ThreadName maps a branch to a stable OKF-safe filename component.
// resume.ThreadName wraps it. It doesn't bound the name's length, so a
// branch longer than the filesystem allows for a file name gives a name it
// refuses.
func ThreadName(branch string) string {
	if branch == "" {
		return "default"
	}
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-' || r == '.' {
			return r
		}
		return '-'
	}, branch)
	if s == "" {
		s = "thread"
	}
	if s[0] == '.' || s[0] == '-' {
		s = "_" + s
	}
	if strings.EqualFold(s, "index") || strings.EqualFold(s, "log") || strings.EqualFold(s, "AGENTS") {
		s += "-thread"
	}
	if s != branch {
		s += "-" + hash(branch)[:6]
	}
	return s
}

func hash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
