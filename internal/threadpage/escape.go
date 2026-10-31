package threadpage

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// fenceRun is a run of backticks or tildes that could open a fence.
	fenceRun = regexp.MustCompile("`{3,}|~{3,}")
	// labelTag is a bracketed speaker label or tag, as the brief and the page
	// write them, with optional spaces inside the brackets.
	labelTag = regexp.MustCompile(`(?i)\[(\s*(?:user|assistant|host\s*summary|host|tool|plan|event|unknown|quoted|stated|accepted|suggested|observed|inferred)\s*)\]`)
	// headingLine is a line markdown reads as a heading.
	headingLine = regexp.MustCompile(`^#{1,6}(?:\s|$)`)
	// keyLine is a line that starts with a frontmatter key okf refuses
	// between --- lines (docs/design.md Validation 8), or with one of
	// eva-md's own labels.
	keyLine = regexp.MustCompile(`(?i)^(["']?(?:verified|governance|generated|type|status|code_refs|stale_after)["']?|eva-md|proposed \(assistant\)|unattributed)(\s*:)`)
	// refPattern is a source ref, read from the end of an item without a tag.
	refPattern = regexp.MustCompile(`^s:\S+#[0-9]+$`)
)

// cleanLine returns s as one escaped line of at most n characters, or of any
// length when n is 0.
func cleanLine(s string, n int) string {
	return cut(escape(strings.Join(strings.Fields(strip(s)), " ")), n)
}

// cleanText is cleanLine for text that keeps its line breaks, such as the
// summary. Each line is collapsed and escaped on its own, and paragraphs are
// separated by at most one blank line.
func cleanText(s string, n int) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n").Replace(s)
	var lines []string
	gap := false
	for _, l := range strings.Split(strip(s), "\n") {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			gap = len(lines) > 0
			continue
		}
		if gap {
			lines = append(lines, "")
			gap = false
		}
		lines = append(lines, escape(l))
	}
	return cut(strings.Join(lines, "\n"), n)
}

// strip removes control and format characters, keeping line breaks and
// turning every other space into a plain one.
func strip(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf):
			return -1
		}
		return r
	}, s)
}

// escape makes one collapsed line safe to write. Each change keeps the text
// readable and none can create another, so escaping twice changes nothing.
func escape(line string) string {
	line = strings.ReplaceAll(line, "<!--", "‹!--")
	line = strings.ReplaceAll(line, "-->", "--›")
	line = fenceRun.ReplaceAllStringFunc(line, func(run string) string {
		return strings.NewReplacer("`", "ˋ", "~", "˜").Replace(run)
	})
	line = labelTag.ReplaceAllString(line, "($1)")
	line = strings.ReplaceAll(line, "](", "] (")
	switch {
	case line == "---":
		return `\---`
	case headingLine.MatchString(line):
		return `\` + line
	}
	return keyLine.ReplaceAllString(line, "`${1}`${2}")
}

// cleanCode returns a code as one escaped line without ": ", which ends the
// code in an item's label.
func cleanCode(s string) string {
	s = cleanLine(s, TextCap)
	for strings.Contains(s, ": ") {
		s = strings.ReplaceAll(s, ": ", " - ")
	}
	return cleanLine(s, TextCap)
}

// cleanRefs returns refs as tokens, dropping empty ones.
func cleanRefs(refs []string) []string {
	var out []string
	for _, r := range refs {
		if r = token(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// token returns s as one token: no spaces, control or format characters,
// and no character that could start markup.
func token(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r) || unicode.IsControl(r) || unicode.In(r, unicode.Cf):
			return -1
		case strings.ContainsRune("<>[]()`", r):
			return '-'
		}
		return r
	}, s)
}

// word keeps only the letters, digits, '-' and '_' of s.
func word(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return -1
	}, s)
}

// cut keeps at most n characters of s, ending in "…" when it cuts; n <= 0
// keeps everything.
func cut(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
