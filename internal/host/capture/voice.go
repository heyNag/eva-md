package capture

import (
	"strings"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// A voice hand-over, exactly as Codex's source writes it (SPEC.md, "The
// voice hand-over"):
//
//	<realtime_delegation>
//	  <source>transcript_tail_flush</source>
//	  <input>REQUEST</input>
//	  <transcript_delta>user: …
//	assistant: …</transcript_delta>
//	</realtime_delegation>
//
// The source line and the transcript line are optional, and &, < and > are
// escaped in each field.
const (
	// VoiceWrapper opens a hand-over. A test holds it equal to the
	// wrapper the capture audit counts.
	VoiceWrapper = "<realtime_delegation>"
	voiceClose   = "</realtime_delegation>"
	// TailFlush is the one source value of the form: what the host saves
	// of a call's end, with a fixed request of its own.
	TailFlush = "transcript_tail_flush"
	// cutMark marks where the host cut a field to 4 KiB: a request keeps
	// its start, a transcript its end.
	cutMark    = "…"
	headSpoken = "user: "
	headFront  = "assistant: "
	// voiceSpace is the white space the form allows around the wrapper.
	voiceSpace = " \t\r\n"
)

// unescape undoes the host's escapes once, so &amp;lt; is &lt;.
var unescape = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")

// handOverForm is a hand-over read into its fields, as written.
type handOverForm struct {
	source     *string
	input      string
	transcript *string
}

// parseHandOver reads s as a hand-over in form: after white space, exactly
// what Codex's source writes, then white space only. No field holds < or >,
// each & begins one of the three escapes, the source is the tail flush's
// and a transcript is not empty.
func parseHandOver(s string) (handOverForm, bool) {
	var h handOverForm
	rest, ok := strings.CutPrefix(strings.Trim(s, voiceSpace), VoiceWrapper+"\n")
	if !ok {
		return h, false
	}
	field := func(name string) (string, bool) {
		r, ok := strings.CutPrefix(rest, "  <"+name+">")
		if !ok {
			return "", false
		}
		i := strings.IndexAny(r, "<>")
		if i < 0 {
			return "", false
		}
		value := r[:i]
		if r, ok = strings.CutPrefix(r[i:], "</"+name+">\n"); !ok || !escaped(value) {
			return "", false
		}
		rest = r
		return value, true
	}
	if strings.HasPrefix(rest, "  <source>") {
		src, ok := field("source")
		if !ok || src != TailFlush {
			return h, false
		}
		h.source = &src
	}
	if h.input, ok = field("input"); !ok {
		return h, false
	}
	if strings.HasPrefix(rest, "  <transcript_delta>") {
		t, ok := field("transcript_delta")
		if !ok || t == "" {
			return h, false
		}
		h.transcript = &t
	}
	return h, rest == voiceClose
}

// escaped reports a field whose every & begins &amp;, &lt; or &gt;.
func escaped(s string) bool {
	for i := strings.IndexByte(s, '&'); i >= 0; i = strings.IndexByte(s, '&') {
		s = s[i:]
		if !strings.HasPrefix(s, "&amp;") && !strings.HasPrefix(s, "&lt;") && !strings.HasPrefix(s, "&gt;") {
			return false
		}
		s = s[1:]
	}
	return true
}

// handOver decodes a prompt that is a hand-over in form into its events:
// each line of the transcript in order, then the request. It reports false
// for a prompt out of form.
func handOver(prompt string) ([]event.Event, bool) {
	h, ok := parseHandOver(prompt)
	if !ok {
		return nil, false
	}
	request := unescape.Replace(h.input)
	var evs []event.Event
	add := func(a event.Author, text string) {
		if text != "" {
			evs = append(evs, event.Event{Kind: event.KindPrompt, Author: a, Text: text})
		}
	}
	transcript := ""
	if h.transcript != nil {
		transcript = unescape.Replace(*h.transcript)
		// An escape holds no line break, so the lines as written and as
		// unescaped are the same lines.
		lines := strings.Split(transcript, "\n")
		held := appended(*h.transcript, h.input, len(lines))
		// A first line the host's cut left short begins with the cut mark,
		// so it has no head and is unknown, as any other line.
		for i, line := range lines {
			spoken, isSpoken := strings.CutPrefix(line, headSpoken)
			front, isFront := strings.CutPrefix(line, headFront)
			switch {
			case held[i]:
				// The host adds the request as a user line.
				add(event.AuthorUnknown, line)
			case isSpoken:
				add(event.AuthorSpoken, spoken)
			case isFront:
				add(event.AuthorFront, front)
			default:
				add(event.AuthorUnknown, line)
			}
		}
	}
	switch {
	case h.transcript != nil && request == transcript:
		// The host's stand-in for an empty request.
		add(event.AuthorUnknown, request)
	case h.source != nil:
		add(event.AuthorHost, request)
	default:
		add(event.AuthorFront, request)
	}
	return evs, true
}

// fieldCap is the most a field of a hand-over holds once escaped, the cut
// mark within it (F-146): the host keeps 4 KiB less the mark and steps
// back, or on, to a character's start, so a field it cut holds fieldCap
// bytes or up to three fewer.
const fieldCap = 4 << 10

// cutLength reports a field, as written, of the length the host's cut
// leaves.
func cutLength(field string) bool {
	return len(field) <= fieldCap && len(field) >= fieldCap-(utf8.UTFMax-1)
}

// cutRequest gives what the host kept of a request it cut: the field ends
// with the cut mark and has the length the cut leaves. A shorter request
// whose own words end in the mark was not cut.
func cutRequest(input string) (string, bool) {
	kept, ok := strings.CutSuffix(input, cutMark)
	return kept, ok && cutLength(input)
}

// cutTranscript gives what the host kept of a transcript it cut: its end.
func cutTranscript(transcript string) (string, bool) {
	kept, ok := strings.CutPrefix(transcript, cutMark)
	return kept, ok && cutLength(transcript)
}

// appended marks which of the transcript's n lines hold the request as the
// host adds it: one user line, `user: ` and the request, which runs over
// several lines when the request holds a line break. Both fields are as
// written, so a cut is measured as the host made it.
//
//   - A whole request: each place the block begins a line and ends one.
//   - A cut request: from the last place the block's kept part begins a
//     line to the transcript's end, since how far the rest ran is unknown.
//   - A cut transcript whose every line may lie in the block: all of them.
//     When the request was cut too, the block is longer than what the
//     transcript keeps, so what is left of the transcript is taken to lie
//     in it.
func appended(transcript, input string, n int) []bool {
	held := make([]bool, n)
	if input == "" {
		return held
	}
	line := func(i int) int { return strings.Count(transcript[:i], "\n") }
	mark := func(from, to int) {
		for l := line(from); l <= line(to); l++ {
			held[l] = true
		}
	}
	// starts gives each place block begins a line of the transcript.
	starts := func(block string) []int {
		var out []int
		for i := 0; i+len(block) <= len(transcript); i++ {
			j := strings.Index(transcript[i:], block)
			if j < 0 {
				break
			}
			i += j
			if i == 0 || transcript[i-1] == '\n' {
				out = append(out, i)
			}
		}
		return out
	}
	rest, transcriptCut := cutTranscript(transcript)
	if kept, cut := cutRequest(input); cut {
		if at := starts(headSpoken + kept); len(at) > 0 {
			mark(at[len(at)-1], len(transcript)-1)
		} else if transcriptCut {
			mark(0, len(transcript)-1)
		}
		return held
	}
	block := headSpoken + input
	for _, i := range starts(block) {
		if end := i + len(block); end == len(transcript) || transcript[end] == '\n' {
			mark(i, end-1)
		}
	}
	if transcriptCut && strings.HasSuffix("\n"+block, rest) {
		mark(0, len(transcript)-1)
	}
	return held
}
