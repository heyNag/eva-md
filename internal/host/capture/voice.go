package capture

import (
	"strings"

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
		// A first line the host's cut left short begins with the cut mark,
		// so it has no head and is unknown, as any other line.
		for _, line := range strings.Split(transcript, "\n") {
			spoken, isSpoken := strings.CutPrefix(line, headSpoken)
			front, isFront := strings.CutPrefix(line, headFront)
			switch {
			case isSpoken && repeats(spoken, request):
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

// repeats reports a spoken line that is the request: equal to it, or, when
// the host cut the request, beginning with what is left of it.
func repeats(line, request string) bool {
	if line == request {
		return true
	}
	kept, cut := strings.CutSuffix(request, cutMark)
	return cut && kept != "" && strings.HasPrefix(line, kept)
}
