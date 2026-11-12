package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// shapeOf gives a skeleton with its hashes left out, as JSON.
func shapeOf(v *hostfacts.Voice) string {
	c := *v
	c.Input.Hash = ""
	if c.Transcript != nil {
		tr := *c.Transcript
		tr.Hash = ""
		tr.Lines = slices.Clone(tr.Lines)
		for i := range tr.Lines {
			tr.Lines[i].Hash = ""
		}
		c.Transcript = &tr
	}
	data, _ := json.Marshal(c)
	return string(data)
}

// partsOf gives a hand-over's parts, the request, the transcript and each
// line's rest, with their texts and their hashes under the test's own key.
func partsOf(t *testing.T, s string) (texts, hashes []string) {
	t.Helper()
	v, ok := hostfacts.VoiceOf([]byte("the test's own key, to compare texts by"), s)
	if !ok {
		t.Fatalf("out of form: %q", s)
	}
	_, body, _ := strings.Cut(s, "<input>")
	input, _, _ := strings.Cut(body, "</input>")
	texts, hashes = append(texts, input), append(hashes, v.Input.Hash)
	if v.Transcript != nil {
		_, body, _ := strings.Cut(s, "<transcript_delta>")
		delta, _, _ := strings.Cut(body, "</transcript_delta>")
		texts, hashes = append(texts, delta), append(hashes, v.Transcript.Hash)
		inner := strings.TrimSuffix(strings.TrimPrefix(delta, hostfacts.CutMark), hostfacts.CutMark)
		for i, l := range strings.Split(inner, "\n") {
			rest := strings.TrimPrefix(strings.TrimPrefix(l, hostfacts.HeadUser), hostfacts.HeadAssist)
			texts, hashes = append(texts, rest), append(hashes, v.Transcript.Lines[i].Hash)
		}
	}
	return texts, hashes
}

// A made-up recording shaped as X-44's call: hand-overs for line 1, line 2
// while the agent works, line 4 with its proposal and yes, line 5 and its
// correction, and line 6 after the pause phrase, then a tail-flush row and
// an empty-input row that no payload matches. Fixtures write each
// hand-over in form with the same lines, heads, source values, cut marks
// and byte lengths, keep every equality of hashes as equality of text, and
// pass check; one line's recorded text put back fails check and the
// repository's check.
func TestFixturesOfAVoiceCall(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "X-44")
	const ack = "Sure, I will ask the agent to do that now."
	s := hostfacts.VoiceScript
	i1 := "Run the shell command sleep 30, then list the files in the orchard folder."
	i2 := "Create a file named fig.txt in the orchard folder containing the single word ripe."
	i4 := "Create a file named quince-notes.txt containing the word golden, and then tell me wh…"
	i5 := "Write the number 47213 into a file named port.txt."
	t5b := "user: " + s[6].Text + "\nassistant: " + ack
	i6d := s[10].Text
	calls := []struct{ input, delta, turn string }{
		{i1, "user: " + s[0].Text + "\nassistant: " + ack + "\nuser: " + i1, turnID},
		{i2, "user: " + s[1].Text + "\nassistant: " + ack + "\nuser: " + i2, turnID},
		{i4, "user: " + s[3].Text + "\nassistant: How about quince-notes.txt?\nuser: " + s[4].Text + "\nuser: " + i4, "turn-made-up-4"},
		{i5, "…e agent to write the number four seven two one three into a file named port dot t x t.\nuser: " + i5, "turn-made-up-5"},
		{t5b, t5b, "turn-made-up-5"},
		{"Create a file named pebble.txt containing the word pebble &amp; nothing else.", "user: " + s[7].Text + "\nassistant: Okay.\nuser: " + s[8].Text, "turn-made-up-6"},
		{i6d, "user: " + s[9].Text + "\nuser: " + i6d, "turn-made-up-7"},
	}
	rollout := newTranscript(t, filepath.Join(h.home, ".codex", "sessions", "2026", "05", "01", "rollout-2026-05-01T10-00-00-"+sessionID+".jsonl"))
	rollout.add(map[string]any{"type": "session_meta", "payload": map[string]any{"id": sessionID, "originator": "codex_vscode"}})
	var prompts []string
	for _, c := range calls {
		w := handOver("", c.input, c.delta)
		prompts = append(prompts, w)
		rollout.add(map[string]any{"type": "realtime_item", "payload": map[string]any{"type": "message", "role": "user", "text": "made-up talk"}})
		rollout.add(voiceRows(w)...)
		h.hook(t, "codex", "UserPromptSubmit", map[string]any{"session_id": sessionID, "turn_id": c.turn, "transcript_path": rollout.path,
			"hook_event_name": "UserPromptSubmit", "prompt": w})
	}
	h.hook(t, "codex", "Stop", map[string]any{"session_id": sessionID, "turn_id": "turn-made-up-7", "transcript_path": rollout.path,
		"hook_event_name": "Stop", "last_assistant_message": ack})
	tail := handOver(hostfacts.TailFlush, "The user just ended their realtime session. Here is the remaining handoff/transcript tail.", "user: "+s[11].Text)
	empty := handOver("", "", "user: "+s[2].Text+"\nassistant: A ripe plum is a deep purple.")
	rollout.add(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": tail}})
	rollout.add(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": empty}})
	h.hook(t, "codex", "SessionEnd", map[string]any{"session_id": sessionID, "transcript_path": rollout.path, "hook_event_name": "SessionEnd", "reason": "other"})
	events := h.events(t)
	if len(events) != len(calls)+2 {
		t.Fatalf("%d events", len(events))
	}
	last := rowVoices(t, events[len(events)-1])
	var flush, noInput bool
	for _, v := range last {
		flush = flush || v.Source != nil && v.Source.Value == hostfacts.TailFlush
		noInput = noInput || v.Input.Len == 0 && v.Transcript != nil && len(v.Transcript.Lines) == 2
	}
	if len(last) != 2 || !flush || !noInput {
		t.Fatalf("the rows no payload matches keep no skeleton: %v", last)
	}

	fx := filepath.Join(filepath.Dir(h.home), "fixtures", "codex", "9.9.9")
	h.must(t, "", "fixtures", "--rec", h.rec, "--out", fx)
	if out := h.must(t, "", "check", "--rec", h.rec, "--fixtures", fx); !strings.Contains(out, "every value is one the generator makes") {
		t.Errorf("check: %s", out)
	}
	data, err := os.ReadFile(filepath.Join(fx, hostfacts.EventsFile))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var v map[string]any
		if err := json.Unmarshal([]byte(l), &v); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, v)
	}
	prompt := func(i int) string {
		p, _ := fixtures[i]["payload"].(map[string]any)["prompt"].(string)
		return p
	}
	// The same skeleton, but for its hashes, and the same bytes in all;
	// equal recorded parts have equal made-up text, and only those.
	key := []byte("the test's own key, to compare texts by")
	recToFix, fixToRec := map[string]string{}, map[string]string{}
	textOf := map[string]string{}
	for i, w := range prompts {
		got := prompt(i)
		fv, ok := hostfacts.VoiceOf(key, got)
		if !ok {
			t.Fatalf("hand-over %d is out of form in the fixture: %q", i+1, got)
		}
		rv, _ := hostfacts.VoiceOf(key, w)
		if shapeOf(fv) != shapeOf(rv) || len(got) != len(w) {
			t.Errorf("hand-over %d's skeleton changed:\n%s\n%s", i+1, shapeOf(rv), shapeOf(fv))
		}
		rt, rh := partsOf(t, w)
		ft, fh := partsOf(t, got)
		for j := range rh {
			if a, ok := recToFix[rh[j]]; ok && a != fh[j] {
				t.Errorf("hand-over %d: equal recorded parts have different made-up text", i+1)
			}
			if b, ok := fixToRec[fh[j]]; ok && b != rh[j] {
				t.Errorf("hand-over %d: different recorded parts have equal made-up text", i+1)
			}
			recToFix[rh[j]], fixToRec[fh[j]] = fh[j], rh[j]
			textOf[rt[j]] = ft[j]
		}
		for _, word := range strings.Fields(strings.ReplaceAll(w, "\n", " ")) {
			head := word+" " == hostfacts.HeadUser || word+" " == hostfacts.HeadAssist
			if len(word) >= 4 && !head && !strings.ContainsAny(word, "<>") && strings.Contains(got, word) {
				t.Errorf("hand-over %d keeps the word %q", i+1, word)
			}
		}
		// The payload's skeleton and its rows' keep equal hashes in the
		// fixture too.
		pv := eventVoices(t, fixtures[i])["prompt"]
		rows := rowVoices(t, fixtures[i])
		if pv == nil || len(rows) != 1 {
			t.Fatalf("hand-over %d: the fixture's skeletons: %v %v", i+1, pv, rows)
		}
		for _, r := range rows {
			if r.Input.Hash != pv.Input.Hash || r.Transcript.Hash != pv.Transcript.Hash || r.Transcript.Lines[0].Hash != pv.Transcript.Lines[0].Hash {
				t.Errorf("hand-over %d: the fixture's row skeleton does not join its payload's", i+1)
			}
		}
	}
	// Item 4's equalities, as text: a request equal to a line, a request
	// equal to the transcript, a line repeated in another hand-over, and
	// a payload string with a line's hash.
	f1, _ := partsOf(t, prompt(0))
	f2, _ := partsOf(t, prompt(1))
	f5, _ := partsOf(t, prompt(4))
	f7, _ := partsOf(t, prompt(6))
	if f1[0] != f1[4] || f7[0] != f7[3] {
		t.Error("a request equal to a line has other text")
	}
	if f1[3] != f2[3] || f1[3] != textOf[ack] {
		t.Error("a line repeated in another hand-over has other text")
	}
	if f5[0] != f5[1] || f5[3] != textOf[ack] {
		t.Error("a request equal to its transcript is not the rebuilt transcript")
	}
	if f1[0] == f2[0] || f1[2] == f2[2] {
		t.Error("different requests or lines have equal text")
	}
	if got := fixtures[len(calls)]["payload"].(map[string]any)["last_assistant_message"]; got != textOf[ack] {
		t.Errorf("a payload string with a line's hash is %q, not the line's text %q", got, textOf[ack])
	}
	// The rows no payload matches keep their source and heads.
	var sources []string
	for _, v := range rowVoices(t, fixtures[len(fixtures)-1]) {
		if v.Source != nil {
			sources = append(sources, v.Source.Value)
		}
		for _, l := range v.Transcript.Lines {
			if l.Head != hostfacts.HeadUser && l.Head != hostfacts.HeadAssist {
				t.Errorf("a row's head is %q in the fixture", l.Head)
			}
		}
	}
	if !slices.Equal(sources, []string{hostfacts.TailFlush}) {
		t.Errorf("the rows' sources in the fixture: %v", sources)
	}
	// One line's recorded text put back.
	made := textOf[s[3].Text]
	if len(made) != len(s[3].Text) || !strings.Contains(string(data), made) {
		t.Fatalf("no made-up line to put back: %q", made)
	}
	if err := os.WriteFile(filepath.Join(fx, hostfacts.EventsFile), []byte(strings.Replace(string(data), made, s[3].Text, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := h.run(t, "", "check", "--rec", h.rec, "--fixtures", fx); code == 0 || !strings.Contains(out, "recorded string") {
		t.Errorf("check passes a recorded line put back: %d\n%s", code, out)
	}
	if problems, err := hostfacts.CheckFixtureDir(fx); err != nil || len(problems) == 0 {
		t.Errorf("the repository's check passes a recorded line put back: %v", err)
	}
}
