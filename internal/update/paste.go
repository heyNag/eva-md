package update

import "strings"

// pasteTag starts the wrapper Claude Code puts around text the user pasted
// into a prompt, as it tells the model: an opening and a closing tag that
// carry the same random id, such as <pasted_content id="7f3a"> and
// </pasted_content id="7f3a">. Codex marks no paste, so its prompts have no
// pasted span: eva-md never guesses one from the text. Whether the prompt a
// hook receives still holds the wrapper is not yet observed (docs/design.md,
// Open host questions).
const pasteTag = "<pasted_content"

// maxPasteTag bounds an opening tag, its id included.
const maxPasteTag = 200

// pastes reports whether a host marks what the user pasted into a prompt.
func pastes(host string) bool { return host == HostClaude }

// span is a run of a prompt's text: the user's own words, or pasted.
type span struct {
	text   string
	pasted bool
}

// spans splits a prompt into the user's own text and the spans the host
// marks as pasted, in order, without the tags. A span opens at
// "<pasted_content" followed by '>', a space or '_', and what follows up to
// the '>' on the same line, and closes only at the closing tag that repeats
// all of it, so pasted text can't end its own span early without the id. A
// span whose closing tag is missing runs to the end of the prompt.
func spans(s string) []span {
	var out []span
	for {
		i, tag := openTag(s)
		if i < 0 {
			break
		}
		if i > 0 {
			out = append(out, span{text: s[:i]})
		}
		body := s[i+len(tag):]
		closing := "</" + tag[1:]
		end := strings.Index(body, closing)
		if end < 0 {
			return append(out, span{text: body, pasted: true})
		}
		out = append(out, span{text: body[:end], pasted: true})
		s = body[end+len(closing):]
	}
	if s != "" {
		out = append(out, span{text: s})
	}
	return out
}

// openTag finds the first opening tag in s and returns where it starts and
// the tag, or -1.
func openTag(s string) (int, string) {
	for from := 0; ; {
		i := strings.Index(s[from:], pasteTag)
		if i < 0 {
			return -1, ""
		}
		i += from
		rest := s[i+len(pasteTag):]
		end := strings.IndexByte(rest, '>')
		if end >= 0 && end < maxPasteTag && !strings.ContainsAny(rest[:end], "\n<") && (end == 0 || rest[0] == ' ' || rest[0] == '_') {
			return i, s[i : i+len(pasteTag)+end+1]
		}
		from = i + len(pasteTag)
	}
}

// ownAndPasted returns a prompt's own text and its pasted text, each span
// on lines of its own. A prompt without a paste is all its own.
func ownAndPasted(s string) (own, pasted string) {
	var mine, theirs []string
	for _, sp := range spans(s) {
		if sp.pasted {
			theirs = append(theirs, sp.text)
		} else {
			mine = append(mine, sp.text)
		}
	}
	return strings.Join(mine, "\n"), strings.Join(theirs, "\n")
}

// markPasted writes a prompt for the model with every line of a pasted span
// on a line of its own, marked "[pasted] ", so the model can tell it from the
// user's own words. A prompt without a paste is left as it is.
func markPasted(s string) string {
	parts := spans(s)
	if len(parts) == 0 || len(parts) == 1 && !parts[0].pasted {
		return s
	}
	var lines []string
	for _, sp := range parts {
		text := strings.Trim(sp.text, "\n")
		if !sp.pasted {
			if strings.TrimSpace(text) != "" {
				lines = append(lines, text)
			}
			continue
		}
		for _, l := range strings.Split(text, "\n") {
			lines = append(lines, strings.TrimRight("[pasted] "+l, " "))
		}
	}
	return strings.Join(lines, "\n")
}
