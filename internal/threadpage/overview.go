package threadpage

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// sentenceCap bounds the summary sentence on an overview line.
const sentenceCap = 200

// Thread is one thread on a project's overview.
type Thread struct {
	Branch, File, Summary string    // File is the thread's file name as ThreadName returns it; empty means ThreadName(Branch)
	Active                time.Time // the thread's last activity
}

// RenderOverview writes the body of a project's now.md: the project heading,
// then one line per thread, newest activity first:
//
//	## Threads
//
//	* [<branch>](threads/<file>.md) — <first sentence of the summary> · last active <date>
//
// With rules set, a "## Rules" section before the threads links the folder's
// rules page, rules.md. Every thread and the rules page are linked, because
// okf counts a page nothing links to as orphaned. Like Render, the body
// starts with a blank line.
func RenderOverview(project string, threads []Thread, rules bool) string {
	ts := slices.Clone(threads)
	slices.SortStableFunc(ts, func(x, y Thread) int {
		switch {
		case x.Active.After(y.Active):
			return -1
		case y.Active.After(x.Active):
			return 1
		}
		return strings.Compare(x.Branch+"\x00"+x.File, y.Branch+"\x00"+y.File)
	})
	var b strings.Builder
	b.WriteString("\n")
	if t := cleanLine(project, 0); t != "" {
		b.WriteString("# " + t + "\n\n")
	}
	if rules {
		b.WriteString(rulesHeading + "\n\n* [Standing rules](rules.md)\n\n")
	}
	b.WriteString("## Threads\n")
	if len(ts) > 0 {
		b.WriteString("\n")
	}
	for _, t := range ts {
		file := t.File
		if file == "" {
			file = ThreadName(t.Branch)
		}
		name := linkText(t.Branch)
		if name == "" {
			name = linkText(file)
		}
		line := "* [" + name + "](threads/" + file + ".md)"
		if s := firstSentence(t.Summary); s != "" {
			line += " — " + s
		}
		if !t.Active.IsZero() {
			line += " · last active " + t.Active.Format("2006-01-02")
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// linkText escapes s for a link's text, so it can't end the link early.
func linkText(s string) string {
	return strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(cleanLine(s, 0))
}

// firstSentence returns the summary up to the end of its first sentence. A
// full stop after a digit or a lone letter, as in "v1." or "e.g.", doesn't
// end one.
func firstSentence(s string) string {
	s = cleanLine(s, 0)
	for i, r := range s {
		switch r {
		case '.', '!', '?':
			if (i+1 == len(s) || s[i+1] == ' ') && (r != '.' || !abbreviation(s[:i])) {
				return cut(s[:i+1], sentenceCap)
			}
		case '。', '！', '？':
			return cut(s[:i+utf8.RuneLen(r)], sentenceCap)
		}
	}
	return cut(s, sentenceCap)
}

// abbreviation reports whether the text before a full stop ends in a digit
// or in a letter that follows no other letter.
func abbreviation(before string) bool {
	r, n := utf8.DecodeLastRuneInString(before)
	if unicode.IsDigit(r) {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(before[:len(before)-n])
	return unicode.IsLetter(r) && !unicode.IsLetter(prev)
}
