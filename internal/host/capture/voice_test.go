package capture

import (
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
)

// The wrapper is the one the capture audit counts as a hand-over.
func TestVoiceWrapperIsTheAuditsWrapper(t *testing.T) {
	if VoiceWrapper != inboxv1.Wrapper {
		t.Fatalf("VoiceWrapper is %q, inboxv1.Wrapper %q", VoiceWrapper, inboxv1.Wrapper)
	}
}

// handOverText writes a hand-over as Codex's source does.
func handOverText(source, input, transcript string) string {
	s := VoiceWrapper + "\n"
	if source != "" {
		s += "  <source>" + source + "</source>\n"
	}
	s += "  <input>" + input + "</input>\n"
	if transcript != "" {
		s += "  <transcript_delta>" + transcript + "</transcript_delta>\n"
	}
	return s + voiceClose
}

// What is in form and what is not, one change at a time from a hand-over
// in form.
func TestHandOverForm(t *testing.T) {
	good := handOverText("", "made-up request", "user: made-up\nassistant: made-up reply")
	for _, tc := range []struct {
		name, s string
		ok      bool
	}{
		{"in form", good, true},
		{"no transcript", handOverText("", "made-up", ""), true},
		{"tail flush", handOverText(TailFlush, "made-up", "user: bye"), true},
		{"empty request", handOverText("", "", "user: made-up"), true},
		{"white space around", " \t\r\n" + good + "\n\r\t ", true},
		{"escapes", handOverText("", "a &lt; b &amp; c &gt; d &amp;lt;", "user: &amp;"), true},
		{"unlisted source", handOverText("made_up", "made-up", "user: hi"), false},
		{"source in another case", handOverText("Transcript_Tail_Flush", "made-up", "user: hi"), false},
		{"empty source", strings.Replace(good, "  <input>", "  <source></source>\n  <input>", 1), false},
		{"empty transcript", strings.Replace(good, "user: made-up\nassistant: made-up reply", "", 1), false},
		{"bare ampersand", handOverText("", "this & that", ""), false},
		{"unknown escape", handOverText("", "a &quot; b", ""), false},
		{"a cut that splits an escape", handOverText("", "a &l…", ""), false},
		{"a cut after an escape", handOverText("", "a &lt;…", "…t; b\nuser: c"), true},
		{"ampersand in the transcript", handOverText("", "made-up", "user: &nbsp;"), false},
		{"bare <", handOverText("", "a < b", ""), false},
		{"bare >", handOverText("", "a > b", ""), false},
		{"tag in the transcript", handOverText("", "made-up", "user: <b>hi</b>"), false},
		{"text after", good + " typed", false},
		{"text before", "typed " + good, false},
		{"no closing tag", strings.TrimSuffix(good, voiceClose), false},
		{"closing tag on the last line", strings.Replace(good, "</transcript_delta>\n</realtime", "</transcript_delta></realtime", 1), false},
		{"no line break after the opening tag", strings.Replace(good, VoiceWrapper+"\n", VoiceWrapper, 1), false},
		{"CRLF", strings.ReplaceAll(good, "\n", "\r\n"), false},
		{"other indentation", strings.Replace(good, "  <input>", "\t<input>", 1), false},
		{"three spaces", strings.Replace(good, "  <input>", "   <input>", 1), false},
		{"no request", strings.Replace(good, "  <input>made-up request</input>\n", "", 1), false},
		{"two requests", strings.Replace(good, "  <input>", "  <input>a</input>\n  <input>", 1), false},
		{"transcript first", VoiceWrapper + "\n  <transcript_delta>user: hi</transcript_delta>\n  <input>made-up</input>\n" + voiceClose, false},
		{"source after the request", VoiceWrapper + "\n  <input>made-up</input>\n  <source>" + TailFlush + "</source>\n" + voiceClose, false},
		{"extra tag", strings.Replace(good, voiceClose, "  <extra>x</extra>\n"+voiceClose, 1), false},
		{"two transcripts", strings.Replace(good, voiceClose, "  <transcript_delta>user: x</transcript_delta>\n"+voiceClose, 1), false},
		{"a tag closed by another name", strings.Replace(good, "</input>", "</inputs>", 1), false},
		{"no-break space before", " " + good, false},
		{"upper case", strings.ToUpper(good), false},
		{"wrapper alone", VoiceWrapper, false},
		{"one line", VoiceWrapper + "made-up" + voiceClose, false},
	} {
		if _, ok := parseHandOver(tc.s); ok != tc.ok {
			t.Errorf("%s: in form %v, want %v", tc.name, ok, tc.ok)
		}
		evs, ok := handOver(tc.s)
		if ok != tc.ok {
			t.Errorf("%s: decoded %v, want %v", tc.name, ok, tc.ok)
		}
		for _, e := range evs {
			if e.Author == event.AuthorTyped {
				t.Errorf("%s: a hand-over gave typed text %q", tc.name, e.Text)
			}
		}
	}
}

// A prompt that begins with the wrapper but is out of form is one unknown
// prompt marked wrapper, whatever its parts say.
func TestOutOfFormIsOneUnknownPrompt(t *testing.T) {
	prompt := handOverText("", "this & that", "user: made-up spoken words")
	for _, ep := range []string{"cli", "sdk-cli", ""} {
		rule, evs := promptEvents("claude-code", prompt, "", false, Env{Entrypoint: ep})
		if rule != RuleWrapper || len(evs) != 1 || evs[0].Author != event.AuthorUnknown || evs[0].Text != prompt || !evs[0].Has(event.MarkWrapper) {
			t.Fatalf("entrypoint %q: %s %+v", ep, rule, evs)
		}
	}
}

// The host's stand-ins and repeats are never a person's spoken words.
func TestHandOverRepeatsAndCuts(t *testing.T) {
	for _, tc := range []struct {
		name, input, transcript string
		want                    []event.Author
	}{
		{"the request repeated as the last line", "made-up request", "user: made-up\nuser: made-up request", []event.Author{"spoken", "unknown", "front_model"}},
		{"the request repeated as the first line", "made-up request", "user: made-up request\nassistant: ok", []event.Author{"unknown", "front_model", "front_model"}},
		{"the request repeated by the front model", "made-up request", "assistant: made-up request", []event.Author{"front_model", "front_model"}},
		{"the request repeated with its escapes", "a &lt; b", "user: a &lt; b", []event.Author{"unknown", "front_model"}},
		{"a cut request", "made-up long request…", "user: made-up long request, all of it", []event.Author{"unknown", "front_model"}},
		{"a cut request that is only the mark", "…", "user: made-up", []event.Author{"spoken", "front_model"}},
		{"a line that only begins like the request", "made-up", "user: made-up and more", []event.Author{"spoken", "front_model"}},
		{"the transcript as the request", "user: a\nassistant: b", "user: a\nassistant: b", []event.Author{"spoken", "front_model", "unknown"}},
		{"a cut first line", "made-up", "…user: cut\nuser: whole", []event.Author{"unknown", "spoken", "front_model"}},
		{"a cut mark later on", "made-up", "user: whole\nuser: …not cut by the host", []event.Author{"spoken", "spoken", "front_model"}},
		{"heads without a space", "made-up", "user:x\nassistant:y", []event.Author{"unknown", "unknown", "front_model"}},
		{"heads in another case", "made-up", "User: x\nASSISTANT: y", []event.Author{"unknown", "unknown", "front_model"}},
		{"a head after white space", "made-up", " user: x", []event.Author{"unknown", "front_model"}},
	} {
		evs, ok := handOver(handOverText("", tc.input, tc.transcript))
		if !ok {
			t.Errorf("%s: out of form", tc.name)
			continue
		}
		var got []event.Author
		for _, e := range evs {
			got = append(got, e.Author)
		}
		if strings.Join(authorStrings(got), " ") != strings.Join(authorStrings(tc.want), " ") {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func authorStrings(as []event.Author) []string {
	var out []string
	for _, a := range as {
		out = append(out, string(a))
	}
	return out
}
