package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// handOver writes a hand-over as Codex's source does; source and delta are
// left out when empty.
func handOver(source, input, delta string) string {
	s := hostfacts.VoiceTag + "\n"
	if source != "" {
		s += "  <source>" + source + "</source>\n"
	}
	s += "  <input>" + input + "</input>\n"
	if delta != "" {
		s += "  <transcript_delta>" + delta + "</transcript_delta>\n"
	}
	return s + "</realtime_delegation>"
}

// voiceRows are the two rows Codex writes for a prompt: the response item
// and the user message event.
func voiceRows(text string) []map[string]any {
	return []map[string]any{
		{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}},
		{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": text}},
	}
}

// rowVoices gives the skeletons of an event's snapshot, by the hash of the
// string each was made from.
func rowVoices(t *testing.T, ev map[string]any) map[string]*hostfacts.Voice {
	t.Helper()
	out := map[string]*hostfacts.Voice{}
	for _, r := range snapshotRows(ev) {
		for _, f := range r["fields"].([]any) {
			m := f.(map[string]any)
			if m["voice"] != nil {
				out[m["hash"].(string)] = decodeSkeleton(t, m["voice"])
			}
		}
	}
	return out
}

func decodeSkeleton(t *testing.T, x any) *hostfacts.Voice {
	t.Helper()
	data, err := json.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	var v hostfacts.Voice
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return &v
}

// eventVoices gives an event's payload skeletons by path.
func eventVoices(t *testing.T, ev map[string]any) map[string]*hostfacts.Voice {
	t.Helper()
	out := map[string]*hostfacts.Voice{}
	m, _ := ev["voice"].(map[string]any)
	for p, x := range m {
		out[p] = decodeSkeleton(t, x)
	}
	return out
}

// withoutPayload gives an event line with its payload left out.
func withoutPayload(ev map[string]any) string {
	rest := map[string]any{}
	for k, v := range ev {
		if k != "payload" {
			rest[k] = v
		}
	}
	data, _ := json.Marshal(rest)
	return string(data)
}

// Planted markers: a made-up Codex session whose payloads and rows hold
// hand-overs with a marker in each part, and out-of-form ones, one of
// them an object key. The hook keeps each in-form skeleton on both sides,
// with equal hashes, and no marker is anywhere in the recording outside
// the payload that carried it, nor in any fixture.
func TestTheHookKeepsAHandOversSkeletonAndNoWordOfIt(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "X-44")
	markers := []string{"QX-INPUT-MARK", "QX-USER-MARK", "QX-ASSISTANT-MARK", "QX-HEAD-MARK", "qx_source_mark", "QX-CUT-MARK", "QX-OUTFORM-MARK", "QX-OUTKEY-MARK", "QX-INKEY-MARK"}
	full := handOver("", "made up "+markers[0], "user: made up "+markers[1]+"\nassistant: made up "+markers[2]+"\nnarrator "+markers[3]+": made up\nuser: made up "+markers[0])
	sourced := handOver(markers[4], "…"+markers[5]+" made up", "…"+markers[5]+" made up")
	outOfForm := strings.Replace(handOver("", "made up "+markers[6], ""), "  <input>", "   <input>", 1)
	outKey := strings.Replace(handOver("", "made up "+markers[7], ""), "\n</realtime", "</realtime", 1)
	inKey := handOver("", "made up "+markers[8], "")
	rollout := newTranscript(t, filepath.Join(h.home, ".codex", "sessions", "2026", "05", "01", "rollout-2026-05-01T10-00-00-"+sessionID+".jsonl"))
	rollout.add(map[string]any{"type": "session_meta", "payload": map[string]any{"id": sessionID, "originator": "codex_vscode"}})
	for _, w := range []string{full, sourced} {
		rollout.add(voiceRows(w)...)
		rollout.add(voiceRows(outOfForm)...)
		h.hook(t, "codex", "UserPromptSubmit", map[string]any{"session_id": sessionID, "turn_id": turnID, "transcript_path": rollout.path, "hook_event_name": "UserPromptSubmit",
			"prompt": w, "extra": map[string]any{"list": []any{outOfForm}, outKey: "made up", inKey: true}})
	}
	events := h.events(t)
	if len(events) != 2 {
		t.Fatalf("%d events", len(events))
	}
	for i, ev := range events {
		pv := eventVoices(t, ev)
		if len(pv) != 1 || pv["prompt"] == nil {
			t.Fatalf("event %d: payload skeletons at %v; only the prompt is in form", i, slices.Collect(mapKeys(pv)))
		}
		rv := rowVoices(t, ev)
		hash := ev["payload_hashes"].(map[string]any)["prompt"].(string)
		if len(rv) != 1 || !reflect.DeepEqual(rv[hash], pv["prompt"]) {
			t.Errorf("event %d: the rows' skeletons %v are not the payload's", i, rv)
		}
		rows := 0
		for _, r := range snapshotRows(ev) {
			for _, f := range r["fields"].([]any) {
				if m := f.(map[string]any); m["hash"] == hash {
					rows++
					if m["voice"] == nil {
						t.Errorf("event %d: a row's hand-over has no skeleton", i)
					}
				}
			}
		}
		if rows != 2 {
			t.Errorf("event %d: %d rows hold the hand-over, want 2", i, rows)
		}
	}
	sk := eventVoices(t, events[0])["prompt"]
	if tr := sk.Transcript; tr == nil || len(tr.Lines) != 4 || tr.Lines[0].Head != "user: " || tr.Lines[1].Head != "assistant: " || tr.Lines[2].Head != "other" ||
		tr.Lines[3].Hash != sk.Input.Hash || sk.Source != nil {
		t.Errorf("the full hand-over's skeleton: %+v", sk)
	}
	sk = eventVoices(t, events[1])["prompt"]
	if sk.Source == nil || sk.Source.Value != "other" || sk.Source.Len != len(markers[4]) || !sk.Input.CutStart || !sk.Transcript.CutStart || sk.Input.Hash != sk.Transcript.Hash {
		t.Errorf("the sourced hand-over's skeleton: %+v %+v", sk, sk.Source)
	}
	// No marker outside the payload that carried it.
	for name, text := range h.recFiles(t) {
		if name == hostfacts.EventsFile {
			continue
		}
		for _, m := range markers {
			if strings.Contains(strings.ToLower(text), strings.ToLower(m)) {
				t.Errorf("%s holds %s", name, m)
			}
		}
	}
	for i, ev := range events {
		rest := strings.ToLower(withoutPayload(ev))
		for _, m := range markers {
			if strings.Contains(rest, strings.ToLower(m)) {
				t.Errorf("event %d holds %s outside its payload", i, m)
			}
		}
	}
	// Nor in any fixture.
	fx := filepath.Join(filepath.Dir(h.home), "fixtures", "codex", "9.9.9")
	h.must(t, "", "fixtures", "--rec", h.rec, "--out", fx)
	err := filepath.WalkDir(fx, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range markers {
			if strings.Contains(strings.ToLower(string(data)), strings.ToLower(m)) {
				t.Errorf("the fixture %s holds %s", filepath.Base(p), m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A line that ends with the cut mark keeps it in the fixture wherever it
// stands, as the last line of a cut transcript or in the middle of one,
// whichever of a payload and a row shows its end: here a payload's
// hand-over has the line in the middle, a row no payload matches has it
// last in a cut transcript, and another payload string is that row's
// whole transcript.
func TestACutLastLineKeepsItsMarkInTheFixture(t *testing.T) {
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "X-44")
	const line = "then the plums…"
	rollout := newTranscript(t, filepath.Join(h.home, ".codex", "sessions", "2026", "05", "01", "rollout-2026-05-01T10-00-00-"+sessionID+".jsonl"))
	rollout.add(map[string]any{"type": "session_meta", "payload": map[string]any{"id": sessionID, "originator": "codex_vscode"}})
	rollout.add(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": handOver("", "made up request", "user: "+line)}})
	prompt := handOver("", "made up", "user: "+line+"\nassistant: made up reply")
	h.hook(t, "codex", "UserPromptSubmit", map[string]any{"session_id": sessionID, "turn_id": turnID, "transcript_path": rollout.path,
		"hook_event_name": "UserPromptSubmit", "prompt": prompt, "note": "user: " + line})
	fx := filepath.Join(filepath.Dir(h.home), "fixtures", "codex", "9.9.9")
	h.must(t, "", "fixtures", "--rec", h.rec, "--out", fx)
	if out := h.must(t, "", "check", "--rec", h.rec, "--fixtures", fx); !strings.Contains(out, "every value is one the generator makes") {
		t.Errorf("check: %s", out)
	}
	data, err := os.ReadFile(filepath.Join(fx, hostfacts.EventsFile))
	if err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatal(err)
	}
	var made map[string]any
	for k, v := range ev["payload"].(map[string]any) {
		if s, ok := v.(string); ok && strings.HasPrefix(s, "user: ") {
			made = map[string]any{"note": s, "key": k}
		}
	}
	got, _ := ev["payload"].(map[string]any)["prompt"].(string)
	_, rest, _ := strings.Cut(got, "<transcript_delta>user: ")
	first, _, _ := strings.Cut(rest, "\n")
	if made == nil || made["note"] != "user: "+first || !strings.HasSuffix(first, hostfacts.CutMark) || len(first) != len(line) {
		t.Errorf("the line is %q in the hand-over and %v beside it", first, made)
	}
	if problems, err := hostfacts.CheckFixtureDir(fx); err != nil || len(problems) != 0 {
		t.Errorf("the repository's check: %v %v", problems, err)
	}
}

// voiceFixture hooks a Codex prompt with the payload given, writes its
// fixtures with a fixed seed, and gives the fixture's event line once
// check and the repository's check pass them.
func voiceFixture(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	h := newHome(t)
	h.setup(t)
	h.must(t, "", "mark", "X-44")
	rollout := newTranscript(t, filepath.Join(h.home, ".codex", "sessions", "2026", "05", "01", "rollout-2026-05-01T10-00-00-"+sessionID+".jsonl"))
	rollout.add(map[string]any{"type": "session_meta", "payload": map[string]any{"id": sessionID, "originator": "codex_vscode"}})
	full := map[string]any{"session_id": sessionID, "turn_id": turnID, "transcript_path": rollout.path, "hook_event_name": "UserPromptSubmit"}
	maps.Copy(full, payload)
	h.hook(t, "codex", "UserPromptSubmit", full)
	fx := filepath.Join(filepath.Dir(h.home), "fixtures", "codex", "9.9.9")
	if _, err := hostfacts.WriteFixtures(hostfacts.FixtureOptions{Rec: h.rec, Out: fx, Seed: []byte("a made-up seed")}); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := h.run(t, "", "check", "--rec", h.rec, "--fixtures", fx); code != 0 || !strings.Contains(out, "every value is one the generator makes") {
		t.Errorf("check: %d %s", code, out)
	}
	if problems, err := hostfacts.CheckFixtureDir(fx); err != nil || len(problems) != 0 {
		t.Errorf("the repository's check: %v %v", problems, err)
	}
	data, err := os.ReadFile(filepath.Join(fx, hostfacts.EventsFile))
	if err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

// A hand-over out of form, as another indent, an extra tag or a later
// form of Codex's would be, is made up by HF-00's rule, keeping its
// leading tag, and its fixtures pass check and the repository's check: a
// sitting that meets one can still give fixtures.
func TestAHandOverOutOfFormGivesFixturesThatPass(t *testing.T) {
	for name, prompt := range map[string]string{
		"another indent": "<realtime_delegation>\n   <input>made up request</input>\n</realtime_delegation>",
		"an extra tag":   "<realtime_delegation>\n  <input>made up request</input>\n  <mood>made up</mood>\n</realtime_delegation>",
		"text after it":  handOver("", "made up request", "") + " made up",
	} {
		t.Run(name, func(t *testing.T) {
			ev := voiceFixture(t, map[string]any{"prompt": prompt})
			got, _ := ev["payload"].(map[string]any)["prompt"].(string)
			if !strings.HasPrefix(got, hostfacts.VoiceTag) || strings.Contains(got, "made up") || len(got) != len(prompt) {
				t.Errorf("the fixture's prompt: %q", got)
			}
			if _, ok := hostfacts.VoiceOf(nil, got); ok || ev["voice"] != nil {
				t.Errorf("an out-of-form hand-over was given a skeleton: %q", got)
			}
		})
	}
}

// Keys that are not key-shaped are hashed in a recorded path, so several
// of one object share a path once the hash is set aside: each skeleton
// there is rebuilt into the string with its own shape, a plain string
// beside them is made up as any string, and two hand-overs of one shape
// each get a hand-over.
func TestHandOversUnderOddKeysKeepTheirOwnSkeletons(t *testing.T) {
	one := handOver("", "zzother request", "user: zzother line one")
	two := handOver("", "zzsecond request of more bytes", "")
	three := handOver("", "zzthird request of more bytes!", "")
	ev := voiceFixture(t, map[string]any{"prompt": "made up", "odd": map[string]any{
		"odd key one": one, "odd key two": "zzplain secret text", "odd key three": two, "odd key four": three,
	}})
	odd, _ := ev["payload"].(map[string]any)
	var shapes, plain []string
	for k, v := range odd {
		if m, ok := v.(map[string]any); ok && k != "prompt" {
			for _, x := range m {
				s, _ := x.(string)
				if v, ok := hostfacts.VoiceOf(nil, s); ok {
					shapes = append(shapes, shapeOf(v))
				} else {
					plain = append(plain, s)
				}
			}
		}
	}
	var want []string
	for _, w := range []string{one, two, three} {
		v, _ := hostfacts.VoiceOf(nil, w)
		want = append(want, shapeOf(v))
	}
	slices.Sort(shapes)
	slices.Sort(want)
	if !slices.Equal(shapes, want) || len(plain) != 1 || len(plain[0]) != len("zzplain secret text") || strings.HasPrefix(plain[0], "<") {
		t.Errorf("the fixture's hand-overs %v, want %v; plain strings %q", shapes, want, plain)
	}
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
