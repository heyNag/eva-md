package capture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// label is what a made-up payload in testdata/payloads decodes to, written
// by hand.
type label struct {
	File       string       `json:"file"`
	Host       adapter.ID   `json:"host"`
	Entrypoint string       `json:"entrypoint"`
	Rule       string       `json:"rule"`
	TID        string       `json:"tid"`
	Agent      string       `json:"agent"`
	Events     []labelEvent `json:"events"`
}

type labelEvent struct {
	Kind   event.Kind   `json:"k"`
	Author event.Author `json:"pv"`
	Text   string       `json:"text"`
	Marks  []event.Mark `json:"mark"`
}

func readLabels(t *testing.T) []label {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "payloads", "labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	var labels []label
	if err := json.Unmarshal(data, &labels); err != nil {
		t.Fatal(err)
	}
	return labels
}

// Every made-up payload decodes to its labels, and every event it gives
// passes Validate once the writer gives it a seq and a key.
func TestPayloadsDecodeToTheirLabels(t *testing.T) {
	labels := readLabels(t)
	files, err := filepath.Glob(filepath.Join("testdata", "payloads", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	labelled := map[string]bool{"labels.json": true}
	for _, l := range labels {
		if labelled[l.File] {
			t.Errorf("%s is labelled twice", l.File)
		}
		labelled[l.File] = true
		raw, err := os.ReadFile(filepath.Join("testdata", "payloads", l.File))
		if err != nil {
			t.Error(err)
			continue
		}
		d, err := Decode(l.Host, raw, Env{Entrypoint: l.Entrypoint})
		if err != nil {
			t.Errorf("%s: %v", l.File, err)
			continue
		}
		if d.Rule != l.Rule || d.TID != l.TID || d.Agent != l.Agent || d.Session == "" || d.Event == "" {
			t.Errorf("%s: rule %q, turn %q, agent %q, session %q, event %q; want rule %q, turn %q, agent %q", l.File, d.Rule, d.TID, d.Agent, d.Session, d.Event, l.Rule, l.TID, l.Agent)
		}
		if len(d.Events) != len(l.Events) {
			t.Errorf("%s: %d events %+v, want %d %+v", l.File, len(d.Events), d.Events, len(l.Events), l.Events)
			continue
		}
		for i, e := range d.Events {
			w := l.Events[i]
			if e.Kind != w.Kind || e.Author != w.Author || e.Text != w.Text || !slices.Equal(e.Marks, w.Marks) {
				t.Errorf("%s: event %d is %s by %s %q %v, want %s by %s %q %v", l.File, i, e.Kind, e.Author, e.Text, e.Marks, w.Kind, w.Author, w.Text, w.Marks)
			}
			if e.TID != d.TID || e.Agent != d.Agent || !e.Src.Hook || e.Seq != 0 || e.Turn != 0 || e.Src.Key != "" || e.Src.Row != "" {
				t.Errorf("%s: event %d carries %+v", l.File, i, e)
			}
			e.Seq, e.Src.Key = int64(i+1), "made-up-key"
			if err := e.Validate(); err != nil {
				t.Errorf("%s: event %d: %v", l.File, i, err)
			}
		}
	}
	for _, f := range files {
		if !labelled[filepath.Base(f)] {
			t.Errorf("%s has no label", f)
		}
	}
}

// The labels cover every rule word each host can reach, and every author
// a prompt can be given.
func TestPayloadsCoverEveryRuleOnBothHosts(t *testing.T) {
	seen := map[adapter.ID]map[string]bool{}
	authors := map[event.Author]bool{}
	for _, l := range readLabels(t) {
		if seen[l.Host] == nil {
			seen[l.Host] = map[string]bool{}
		}
		seen[l.Host][l.Rule] = true
		for _, e := range l.Events {
			authors[e.Author] = true
		}
	}
	only := map[adapter.ID][]string{
		adapter.ClaudeCode: {RuleProgram, RuleInteractive, RuleEntrypointUnknown},
		adapter.Codex:      {RuleVoice, RuleWrapper, RuleNoEvidence},
	}
	for _, rule := range ruleWords(t) {
		for _, h := range adapter.All() {
			want := true
			for other, rules := range only {
				if other != h.ID && slices.Contains(rules, rule) {
					want = false
				}
			}
			if seen[h.ID][rule] != want {
				t.Errorf("on %s the labels cover the rule %q: %v, want %v", h.ID, rule, seen[h.ID][rule], want)
			}
		}
	}
	for _, a := range []event.Author{event.AuthorTyped, event.AuthorProgram, event.AuthorHost, event.AuthorSubagent, event.AuthorAgent, event.AuthorTool, event.AuthorSpoken, event.AuthorFront, event.AuthorUnknown} {
		if !authors[a] {
			t.Errorf("no label gives the author %s", a)
		}
	}
}

// The voice payloads hold each case the card names.
func TestPayloadsHoldEveryHandOverCase(t *testing.T) {
	files := map[string]bool{}
	for _, l := range readLabels(t) {
		files[l.File] = true
	}
	for _, f := range []string{
		"codex-voice-correction.json", "codex-voice-yes.json", "codex-voice-pause.json", "codex-voice-tail-flush.json",
		"codex-voice-empty-request.json", "codex-voice-escapes.json", "codex-voice-cut.json", "codex-voice-cut-request.json",
		"codex-voice-request-is-transcript.json", "codex-voice-unknown-line.json", "codex-voice-steered.json",
		"codex-wrapper-audit-off.json", "codex-wrapper-audit-space.json", "codex-wrapper-audit-bare.json",
		"codex-voice-multiline-request.json", "codex-voice-multiline-heads.json", "codex-voice-ellipsis-request.json",
		"codex-voice-both-cut.json", "codex-voice-transcript-cut.json", "claude-voice-on-claude-sdk.json",
	} {
		if !files[f] {
			t.Errorf("no payload %s", f)
		}
	}
}
