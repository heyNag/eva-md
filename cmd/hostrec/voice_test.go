package main

import (
	"encoding/json"
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

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
