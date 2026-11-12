package hostfacts

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
)

// The tag a hand-over begins with is the one the ledger reads.
func TestVoiceTagIsTheLedgers(t *testing.T) {
	if VoiceTag != inboxv1.Wrapper {
		t.Errorf("the recorder's tag is %q, the ledger's %q", VoiceTag, inboxv1.Wrapper)
	}
}

// wrap writes a hand-over as Codex's source does, every part given.
func wrap(source, input, delta *string) string {
	s := VoiceTag + "\n"
	if source != nil {
		s += "  <source>" + *source + "</source>\n"
	}
	s += "  <input>" + input2(input) + "</input>\n"
	if delta != nil {
		s += "  <transcript_delta>" + *delta + "</transcript_delta>\n"
	}
	return s + "</realtime_delegation>"
}

func input2(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func sp(s string) *string { return &s }

// Every request, line and value here is made up.
func TestVoiceSkeletons(t *testing.T) {
	key := []byte("a made-up key for the voice test")
	h := func(s string) string { return Hash(key, s) }
	field := func(s string, start, end bool) VoiceField {
		return VoiceField{Len: len(s), Hash: h(s), CutStart: start, CutEnd: end}
	}
	line := func(head, rest string) TranscriptLine {
		return TranscriptLine{Head: head, Len: len(rest), Hash: h(rest)}
	}
	delta := func(s string, start, end bool, lines ...TranscriptLine) *VoiceTranscript {
		return &VoiceTranscript{Len: len(s), Hash: h(s), CutStart: start, CutEnd: end, Lines: lines}
	}
	const (
		req   = "made-up request about plums"
		hello = "made-up hello"
		reply = "made-up reply, twice"
	)
	two := "user: " + hello + "\nassistant: " + reply
	cutTwo := "…llo there\nassistant: " + reply
	esc := "fish &amp; chips &lt;3 &gt; made up"
	for _, c := range []struct {
		name string
		in   string
		want *Voice
	}{
		{"a request and a transcript", wrap(nil, sp(req), sp(two)),
			&Voice{Input: field(req, false, false), Transcript: delta(two, false, false, line(HeadUser, hello), line(HeadAssist, reply))}},
		{"no transcript", wrap(nil, sp(req), nil), &Voice{Input: field(req, false, false)}},
		{"the tail-flush source", wrap(sp(TailFlush), sp(req), sp("user: "+hello)),
			&Voice{Source: &VoiceSource{Value: TailFlush}, Input: field(req, false, false), Transcript: delta("user: "+hello, false, false, line(HeadUser, hello))}},
		{"an unlisted source", wrap(sp("made_up_source_word"), sp(req), nil),
			&Voice{Source: &VoiceSource{Value: "other", Len: len("made_up_source_word")}, Input: field(req, false, false)}},
		{"a cut request", wrap(nil, sp(req+"…"), nil), &Voice{Input: field(req+"…", false, true)}},
		{"a cut transcript whose first line lost its head", wrap(nil, sp(req), sp(cutTwo)),
			&Voice{Input: field(req, false, false), Transcript: delta(cutTwo, true, false, line(HeadOther, "llo there"), line(HeadAssist, reply))}},
		{"an empty request", wrap(nil, sp(""), sp("user: "+hello)),
			&Voice{Input: field("", false, false), Transcript: delta("user: "+hello, false, false, line(HeadUser, hello))}},
		{"a request equal to a line", wrap(nil, sp(req), sp("assistant: "+reply+"\nuser: "+req)),
			&Voice{Input: field(req, false, false), Transcript: delta("assistant: "+reply+"\nuser: "+req, false, false, line(HeadAssist, reply), line(HeadUser, req))}},
		{"a request equal to the transcript", wrap(nil, sp(two), sp(two)),
			&Voice{Input: field(two, false, false), Transcript: delta(two, false, false, line(HeadUser, hello), line(HeadAssist, reply))}},
		{"an unlisted head", wrap(nil, sp(req), sp("narrator: "+hello+"\nUser: "+reply+"\nuser:"+hello)),
			&Voice{Input: field(req, false, false), Transcript: delta("narrator: "+hello+"\nUser: "+reply+"\nuser:"+hello, false, false,
				line(HeadOther, "narrator: "+hello), line(HeadOther, "User: "+reply), line(HeadOther, "user:"+hello))}},
		{"escapes", wrap(nil, sp(esc), sp("user: "+esc)),
			&Voice{Input: field(esc, false, false), Transcript: delta("user: "+esc, false, false, line(HeadUser, esc))}},
		{"white space around it", " \t\r\n" + wrap(nil, sp(req), nil) + "\n\n ",
			&Voice{Lead: " \t\r\n", Trail: "\n\n ", Input: field(req, false, false)}},
		// Only a leading cut mark is set aside before the transcript is
		// split: a trailing one stays in the last line's rest, so a request
		// that ends as the last line does is equal to it.
		{"a cut mark at both ends and in the middle", wrap(nil, sp("…a … b…"), sp("…user: c\nassistant: d…")),
			&Voice{Input: field("…a … b…", true, true), Transcript: delta("…user: c\nassistant: d…", true, true, line(HeadUser, "c"), line(HeadAssist, "d…"))}},
		{"a request equal to a last line that ends with the cut mark", wrap(nil, sp(req+"…"), sp("assistant: "+reply+"\nuser: "+req+"…")),
			&Voice{Input: field(req+"…", false, true), Transcript: delta("assistant: "+reply+"\nuser: "+req+"…", false, true, line(HeadAssist, reply), line(HeadUser, req+"…"))}},
		{"a cut mark alone", wrap(nil, sp("…"), nil), &Voice{Input: field("…", true, false)}},
		{"a transcript of a cut mark alone", wrap(nil, sp(req), sp("…")),
			&Voice{Input: field(req, false, false), Transcript: delta("…", true, false, line(HeadOther, ""))}},
		{"a transcript of two cut marks", wrap(nil, sp(req), sp("……")),
			&Voice{Input: field(req, false, false), Transcript: delta("……", true, true, line(HeadOther, "…"))}},
	} {
		got, ok := VoiceOf(key, c.in)
		if !ok || !reflect.DeepEqual(got, c.want) {
			gj, _ := json.Marshal(got)
			wj, _ := json.Marshal(c.want)
			t.Errorf("%s: %v\n got %s\nwant %s", c.name, ok, gj, wj)
		}
	}
	// What equal text shows: a request equal to a line, or to the
	// transcript, has that line's or the transcript's hash.
	if v, _ := VoiceOf(key, wrap(nil, sp(req), sp("assistant: "+reply+"\nuser: "+req))); v.Input.Hash != v.Transcript.Lines[1].Hash {
		t.Error("a request equal to a line does not show it")
	}
	if v, _ := VoiceOf(key, wrap(nil, sp(req+"…"), sp("user: "+req+"…"))); v.Input.Hash != v.Transcript.Lines[0].Hash {
		t.Error("a request equal to a last line that ends with the cut mark does not show it")
	}
	if v, _ := VoiceOf(key, wrap(nil, sp(two), sp(two))); v.Input.Hash != v.Transcript.Hash {
		t.Error("a request equal to the transcript does not show it")
	}
	good := wrap(nil, sp(req), sp(two))
	for _, c := range []struct{ name, in string }{
		{"a swapped tag order", VoiceTag + "\n  <transcript_delta>" + two + "</transcript_delta>\n  <input>" + req + "</input>\n</realtime_delegation>"},
		{"the source after the request", VoiceTag + "\n  <input>" + req + "</input>\n  <source>" + TailFlush + "</source>\n</realtime_delegation>"},
		{"an extra tag", strings.Replace(good, "  <input>", "  <extra>made up</extra>\n  <input>", 1)},
		{"an extra tag after the transcript", strings.Replace(good, "</realtime_delegation>", "  <extra>made up</extra>\n</realtime_delegation>", 1)},
		{"text after the closing tag", good + " made up"},
		{"text before the opening tag", "made up " + good},
		{"a missing line break after the opening tag", strings.Replace(good, VoiceTag+"\n", VoiceTag, 1)},
		{"a missing line break before the closing tag", strings.Replace(good, "</transcript_delta>\n", "</transcript_delta>", 1)},
		{"other indentation", strings.Replace(good, "  <input>", "   <input>", 1)},
		{"a tab for indentation", strings.Replace(good, "  <input>", "\t<input>", 1)},
		{"a bare <", wrap(nil, sp("made up < request"), nil)},
		{"a bare >", wrap(nil, sp(req), sp("user: a > b"))},
		{"a bare < in the source", wrap(sp("made<up"), sp(req), nil)},
		{"no closing tag", strings.TrimSuffix(good, "</realtime_delegation>")},
		{"no request", VoiceTag + "\n  <transcript_delta>" + two + "</transcript_delta>\n</realtime_delegation>"},
		{"a second request", strings.Replace(good, "  <transcript_delta>", "  <input>"+req+"</input>\n  <transcript_delta>", 1)},
		{"a closing tag of another name", strings.Replace(good, "</input>", "</inputs>", 1)},
		{"the tag in another case", strings.Replace(good, VoiceTag, "<Realtime_Delegation>", 1)},
		{"nothing", ""},
	} {
		if v, ok := VoiceOf(key, c.in); ok || v != nil {
			t.Errorf("%s has a skeleton", c.name)
		}
	}
}

// A payload's in-form strings get a skeleton at any path, an object key
// never does, and a string out of form gets none.
func TestPayloadVoices(t *testing.T) {
	key := []byte("a made-up key")
	w := wrap(nil, sp("made-up request"), nil)
	got := PayloadVoices(key, map[string]any{
		"prompt": w, "nested": map[string]any{"list": []any{"x", w}}, w: "made up",
		"out": w + " and more", "plain": "made up",
	})
	if len(got) != 2 || got["prompt"] == nil || got["nested.list.1"] == nil {
		t.Errorf("skeletons at %v", got)
	}
	if PayloadVoices(key, map[string]any{"prompt": "made up"}) != nil {
		t.Error("a payload with no hand-over has skeletons")
	}
}

// Every word a skeleton keeps, and each of its field names, is one the
// fixtures keep: a host word on the vocabulary or the recorder's own.
func TestEveryWordASkeletonKeepsIsOnTheVocabulary(t *testing.T) {
	for _, w := range []string{"realtime_delegation", "source", "input", "transcript_delta"} {
		if !strings.Contains(string(vocabularyJSON), `"`+w+`"`) || !Vocab().Has(w) {
			t.Errorf("the vocabulary lacks %q", w)
		}
	}
	for _, w := range voiceWords {
		if !Vocab().Has(w) {
			t.Errorf("the vocabulary lacks %q", w)
		}
	}
	key := []byte("a made-up key")
	for _, s := range []string{
		wrap(sp(TailFlush), sp("made up…"), sp("…user: a\nassistant: b\nnarrator: c")),
		wrap(sp("made_up_source"), sp(""), nil),
	} {
		v, ok := VoiceOf(key, s)
		if !ok {
			t.Fatal("no skeleton")
		}
		data, _ := json.Marshal(v)
		decoded, err := Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		walk(decoded, nil, func(path []string, x any) {
			last := path[len(path)-1]
			if !isIndex(last) && !kept(last) {
				t.Errorf("a skeleton's field name %q is not kept", last)
			}
			str, ok := x.(string)
			switch {
			case !ok:
			case last == "hash":
			case last == "lead" || last == "trail":
				if strings.Trim(str, voiceSpace) != "" {
					t.Errorf("%s holds %q", last, str)
				}
			case !Vocab().Has(str):
				t.Errorf("a skeleton keeps %q at %s, which is not on the vocabulary", str, strings.Join(path, "."))
			}
		})
	}
}

// A hand-over rebuilt from its skeleton alone has the same white space,
// lines, heads, source value, cut marks and byte lengths, and made-up text
// of lower-case letters and single spaces; equal hashes get equal text.
func TestRebuildKeepsTheSkeleton(t *testing.T) {
	key, other := []byte("a made-up key"), []byte("another made-up key")
	const req = "made-up request: plums &amp; pears"
	cases := []string{
		"\n " + wrap(sp(TailFlush), sp(req+"…"), sp("…lo there\nassistant: made-up reply\nuser: "+req)) + "\n",
		wrap(sp("made_up_source"), sp(""), sp("narrator: made up")),
		wrap(nil, sp("user: a\nassistant: b"), sp("user: a\nassistant: b")),
		wrap(nil, sp(req), nil),
		// A line that ends with the cut mark in the middle of one
		// transcript, drawn before it is seen as the last line of a cut
		// one; a request that begins with the cut mark, equal to a line.
		wrap(nil, sp("made up"), sp("user: then plums…\nassistant: made up")),
		wrap(nil, sp("then plums…"), sp("…lo\nuser: then plums…")),
		wrap(nil, sp("…mid part"), sp("user: …mid part\nassistant: a tail…")),
		wrap(nil, sp("a"), sp("……")),
		wrap(nil, sp("……"), sp("…")),
		// A cut mark alone, which begins the request and ends the last
		// line of a cut transcript: one text of three bytes.
		wrap(nil, sp("…"), sp("user: a\nuser: …")),
	}
	var skels []*Voice
	for _, s := range cases {
		v, _ := VoiceOf(key, s)
		skels = append(skels, v)
	}
	texts := map[string]string{}
	n := 0
	assignVoice(skels, knownCuts(skels), texts, func(size int) string { n++; return madeVoice([]byte("a seed"), size, n) })
	// Equal parts across the hand-overs have equal made-up text, and only
	// those.
	recToFix, fixToRec := map[string]string{}, map[string]string{}
	for i, v := range skels {
		out := v.rebuild(texts, "")
		got, ok := VoiceOf(key, out)
		if !ok {
			continue
		}
		pairs := [][2]string{{v.Input.Hash, got.Input.Hash}}
		if v.Transcript != nil && got.Transcript != nil && len(got.Transcript.Lines) == len(v.Transcript.Lines) {
			pairs = append(pairs, [2]string{v.Transcript.Hash, got.Transcript.Hash})
			for j, l := range v.Transcript.Lines {
				pairs = append(pairs, [2]string{l.Hash, got.Transcript.Lines[j].Hash})
			}
		}
		for _, p := range pairs {
			if f, ok := recToFix[p[0]]; ok && f != p[1] {
				t.Errorf("case %d: equal parts have different made-up text", i)
			}
			if r, ok := fixToRec[p[1]]; ok && r != p[0] {
				t.Errorf("case %d: different parts have equal made-up text", i)
			}
			recToFix[p[0]], fixToRec[p[1]] = p[1], p[0]
		}
	}
	for i, v := range skels {
		src := ""
		if v.Source != nil && v.Source.Value != TailFlush {
			src = madeVoice([]byte("a seed"), v.Source.Len, 0)
		}
		out := v.rebuild(texts, src)
		got, ok := VoiceOf(other, out)
		if !ok {
			t.Fatalf("case %d: the rebuilt hand-over is out of form: %q", i, out)
		}
		if len(out) != len(cases[i]) {
			t.Errorf("case %d: %d bytes, want %d", i, len(out), len(cases[i]))
		}
		// The same skeleton, but for the hashes, which another key makes.
		strip := func(v *Voice) string {
			c := *v
			c.Input.Hash = ""
			if c.Transcript != nil {
				tr := *c.Transcript
				tr.Hash = ""
				tr.Lines = append([]TranscriptLine(nil), tr.Lines...)
				for j := range tr.Lines {
					tr.Lines[j].Hash = ""
				}
				c.Transcript = &tr
			}
			data, _ := json.Marshal(c)
			return string(data)
		}
		if strip(got) != strip(v) {
			t.Errorf("case %d: the skeleton changed:\n%s\n%s", i, strip(v), strip(got))
		}
		// Equal hashes before are equal hashes after, and only those.
		if (v.Transcript != nil && v.Input.Hash == v.Transcript.Hash) != (got.Transcript != nil && got.Input.Hash == got.Transcript.Hash) {
			t.Errorf("case %d: the request's equality to the transcript changed", i)
		}
		if v.Transcript != nil {
			for j, l := range v.Transcript.Lines {
				if (l.Hash == v.Input.Hash) != (got.Transcript.Lines[j].Hash == got.Input.Hash) {
					t.Errorf("case %d: line %d's equality to the request changed", i, j)
				}
			}
		}
		for _, bad := range []string{"plums", "pears", "&amp;", "narrator", "made_up_source", "made-up", "lo there"} {
			if strings.Contains(out, bad) {
				t.Errorf("case %d: the rebuilt hand-over holds %q", i, bad)
			}
		}
	}
	for size := range 40 {
		for k := range 20 {
			s := madeVoice([]byte("a seed"), size, k)
			if len(s) != size || size > 0 && !voiceShaped.MatchString(s) {
				t.Errorf("a made-up text of %d bytes: %q", size, s)
			}
		}
	}
}

// writeVoiceFixture writes a folder of fixtures that holds the given
// payload strings, each in an event line, as WriteFixtures would lay them
// out, for the repository's check to read.
func writeVoiceFixture(t *testing.T, seed []byte, prompts ...string) string {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	for _, p := range prompts {
		line, err := Marshal(map[string]any{"v": json.Number("1"), "payload": map[string]any{"prompt": p}})
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(line, '\n'))
	}
	if err := os.WriteFile(filepath.Join(dir, EventsFile), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(FixtureMeta{V: 1, Seed: hex.EncodeToString(seed), Epoch: Epoch.Format(time.RFC3339), MonoBase: MonoBase, Files: []string{EventsFile}})
	if err := os.WriteFile(filepath.Join(dir, "fixture.json"), meta, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The repository's check reads each fixture hand-over back to its parts
// and rebuilds them from the seed: a repeat of an earlier part, a request
// equal to a line or a transcript, and the tail-flush source pass; a
// wrapper out of form, a head off the list, a part the generator would
// not make, and a request or source of recorded text fail.
func TestTheRepositoryCheckRebuildsHandOvers(t *testing.T) {
	seed := []byte("a made-up seed")
	m := func(n, k int) string { return madeVoice(seed, n, k) }
	a, b, c, d := m(12, 0), m(30, 0), m(12, 1), m(7, 0)
	two := "user: " + a + "\nassistant: " + b
	good := []string{
		wrap(sp(TailFlush), sp(a), sp(two)),
		"\n" + wrap(sp(d), sp(two), sp(two)) + " ",
		wrap(nil, sp(c+"…"), sp("…"+c+"\nuser: "+a)),
		wrap(nil, sp(""), nil),
		wrap(nil, sp(m(9, 0)+"…"), sp("…"+a+"\nuser: "+m(9, 0)+"…")),
	}
	if problems, err := CheckFixtureDir(writeVoiceFixture(t, seed, good...)); err != nil || len(problems) != 0 {
		t.Fatalf("hand-overs the generator makes fail: %v %v", problems, err)
	}
	// A string with a part's text, standing alone, passes too, as do a
	// cut request and a transcript that only a row held.
	for _, alone := range []string{a, c + "…", "user: " + a + "\nassistant: " + m(12, 2) + "…", "…" + m(30, 1) + "\nuser: " + a} {
		if problems, err := CheckFixtureDir(writeVoiceFixture(t, seed, append(good, alone)...)); err != nil || len(problems) != 0 {
			t.Errorf("a part's text standing alone fails: %q %v %v", alone, problems, err)
		}
	}
	for _, bad := range []struct{ name, prompt string }{
		{"a wrapper out of form", strings.Replace(good[0], "  <input>", "   <input>", 1)},
		{"a head off the list", wrap(nil, sp(a), sp("narrator: "+a))},
		{"a line of recorded text", wrap(nil, sp(a), sp("user: made up plum talk"))},
		{"a line of a length it was not drawn for", wrap(nil, sp(a), sp("user: "+a+"x"))},
		{"a request of recorded text", wrap(nil, sp("made up plum talk"), nil)},
		{"a cut request of recorded text", wrap(nil, sp("made up plum talk…"), nil)},
		{"a source of recorded text", wrap(sp("made_up_source"), sp(a), nil)},
		{"a request with a stray capital", wrap(nil, sp(strings.ToUpper(a[:1])+a[1:]), nil)},
		{"a transcript standing alone with a line of recorded text", "user: " + a + "\nassistant: made up plum talk"},
		{"a cut text standing alone of another seed", madeVoice([]byte("another seed"), 12, 0) + "…"},
		{"another seed's line", wrap(nil, sp(a), sp("user: "+madeVoice([]byte("another seed"), 12, 0)))},
	} {
		problems, err := CheckFixtureDir(writeVoiceFixture(t, seed, append(good, bad.prompt)...))
		if err != nil || len(problems) == 0 {
			t.Errorf("the repository's check passes %s: %v", bad.name, err)
		}
	}
}
