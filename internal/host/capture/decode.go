// Package capture decodes one hook payload of a host into the record's
// events. Each prompt's author is set by an allowlist, on positive evidence
// only, and a voice hand-over is decoded line by line. It is pure: it reads
// no file, clock, environment or network and writes no log, so its caller
// reads Claude Code's entrypoint and gives it in Env. SPEC.md states each
// rule, its word and the fact behind it. Nothing calls it yet.
package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// MaxPayload is the largest payload Decode reads, v1's limit.
const MaxPayload = 2 << 20

// Env is what the caller reads for Decode outside the payload.
type Env struct {
	// Entrypoint is Claude Code's CLAUDE_CODE_ENTRYPOINT, as the hook
	// inherits it. Another host's is not read.
	Entrypoint string
}

// Decoded is one payload decoded.
type Decoded struct {
	Event   string // hook_event_name
	Session string // session_id
	TID     string // turn_id, else prompt_id
	Agent   string // agent_id
	// Rule is the word of the rule that decided the events: for a prompt,
	// the rule that set its author.
	Rule string
	// Events are the payload's events in order, each with TID, Agent and
	// Src.Hook set. Seq, Turn and Src.Key are the writer's, which also
	// redacts the text.
	Events []event.Event
}

// The rules' words. A prompt's rules are tried in the order SPEC.md gives.
const (
	RuleUndeclared        = "undeclared"             // an event no row of the host declares: none
	RuleEvent             = "event"                  // an event other than a prompt
	RuleEmpty             = "empty"                  // an empty prompt: none
	RuleBuiltin           = "builtin"                // one of v1's builtin commands: none
	RuleAgent             = "agent_id"               // a prompt with an agent id: a subagent's
	RuleSource            = "source"                 // a listed source: the list's author
	RuleSourceUnlisted    = "source_unlisted"        // any other source: unknown
	RuleVoice             = "voice"                  // Codex, a hand-over in form, decoded line by line
	RuleWrapper           = "wrapper"                // Codex, a hand-over out of form: one unknown prompt
	RulePrefix            = "prefix"                 // a listed prefix: the list's author
	RuleTag               = "tag"                    // any other leading tag: unknown
	RuleProgram           = "entrypoint_program"     // Claude Code, a program's entrypoint
	RuleInteractive       = "entrypoint_interactive" // Claude Code, an interactive entrypoint: typed
	RuleEntrypointUnknown = "entrypoint_unknown"     // Claude Code, any other entrypoint or none
	RuleNoEvidence        = "no_evidence"            // a host whose hook says nothing of who typed: unknown
)

// The ways Decode refuses a payload.
var (
	ErrTooLarge = errors.New("capture: payload over 2 MiB")
	ErrNotJSON  = errors.New("capture: payload is not one JSON object")
)

// Decode decodes host's hook payload raw. It refuses a payload over
// MaxPayload, or one that is not one JSON object with each key once and
// each field it reads a string or null.
func Decode(host adapter.ID, raw []byte, env Env) (Decoded, error) {
	if len(raw) > MaxPayload {
		return Decoded{}, ErrTooLarge
	}
	fields, err := object(raw)
	if err != nil {
		return Decoded{}, err
	}
	var d Decoded
	var tid, pid string
	for _, f := range []struct {
		key string
		to  *string
	}{{"hook_event_name", &d.Event}, {"session_id", &d.Session}, {"turn_id", &tid}, {"prompt_id", &pid}, {"agent_id", &d.Agent}} {
		if *f.to, err = text(fields, f.key); err != nil {
			return Decoded{}, err
		}
	}
	d.TID = tid
	if d.TID == "" {
		d.TID = pid
	}
	if !declared(host, d.Event) {
		d.Rule = RuleUndeclared
		return d, nil
	}
	d.Rule = RuleEvent
	// by is an author, or a subagent's when the payload names one.
	by := func(a event.Author) event.Author {
		if d.Agent != "" {
			return event.AuthorSubagent
		}
		return a
	}
	var evs []event.Event
	switch d.Event {
	case "UserPromptSubmit":
		prompt, err := text(fields, "prompt")
		if err != nil {
			return Decoded{}, err
		}
		source, err := text(fields, "source")
		if err != nil {
			return Decoded{}, err
		}
		d.Rule, evs = promptEvents(host, prompt, source, d.Agent != "", env)
	case "Stop":
		reply, err := text(fields, "last_assistant_message")
		if err != nil {
			return Decoded{}, err
		}
		if reply != "" {
			evs = append(evs, event.Event{Kind: event.KindReply, Author: by(event.AuthorAgent), Text: reply})
		}
	case "PostToolUse", "PostToolUseFailure":
		evs = append(evs, event.Event{Kind: event.KindTool, Author: by(event.AuthorTool)})
	default:
		summary := ""
		if d.Event == "PostCompact" {
			if summary, err = text(fields, "compact_summary"); err != nil {
				return Decoded{}, err
			}
		}
		if summary != "" {
			evs = append(evs, event.Event{Kind: event.KindSummary, Author: by(event.AuthorHost), Text: summary})
			break
		}
		name := eventName(d.Event)
		if name == "" {
			break
		}
		switch d.Event {
		case "SessionStart":
			source, err := text(fields, "source")
			if err != nil {
				return Decoded{}, err
			}
			if slices.Contains(startSources, source) {
				name += ": " + source
			}
		case "StopFailure":
			name += failureType(fields["error"])
		}
		evs = append(evs, event.Event{Kind: event.KindLife, Author: event.AuthorHost, Text: name, Marks: []event.Mark{event.MarkName}})
	}
	for i := range evs {
		evs[i].TID, evs[i].Agent, evs[i].Src.Hook = d.TID, d.Agent, true
	}
	d.Events = evs
	return d, nil
}

// readKeys are the payload's fields Decode reads, matched exactly.
var readKeys = []string{"hook_event_name", "session_id", "turn_id", "prompt_id", "agent_id", "prompt", "source", "last_assistant_message", "compact_summary", "error"}

// object reads raw as one JSON object, each key once, with nothing after
// it but white space. A key that is one of readKeys in another letter case
// is refused: v1's reader takes it for the field, so whether a payload
// names an agent, say, would be in doubt.
func object(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, ErrNotJSON
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return nil, ErrNotJSON
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, ErrNotJSON
		}
		// An error names a key only when it is one Decode reads, so it
		// carries none of the payload's own text.
		if _, twice := fields[key]; twice {
			if slices.Contains(readKeys, key) {
				return nil, fmt.Errorf("%w: the key %s is there twice", ErrNotJSON, key)
			}
			return nil, fmt.Errorf("%w: a key is there twice", ErrNotJSON)
		}
		for _, k := range readKeys {
			if key != k && strings.EqualFold(key, k) {
				// v1 reads it as the field, so it is the field twice or
				// in doubt.
				return nil, fmt.Errorf("%w: a key is %s in another case", ErrNotJSON, k)
			}
		}
		fields[key] = value
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, ErrNotJSON
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, ErrNotJSON
	}
	return fields, nil
}

// text is the string at key, or "" when it is absent or null.
func text(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok || string(raw) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("%w: %s is not a string", ErrNotJSON, key)
	}
	return s, nil
}

// declared reports an event a row of the host's declares.
func declared(host adapter.ID, name string) bool {
	for _, r := range adapter.Rows(host) {
		if slices.Contains(r.Events, name) {
			return true
		}
	}
	return false
}

// startSources are the sources a SessionStart's name carries, as the hosts
// give them.
var startSources = []string{"startup", "resume", "clear", "compact", "fork"}

// eventName is v1's fixed name of a lifecycle event, or "".
func eventName(name string) string {
	switch name {
	case "SessionStart":
		return "session_start"
	case "SessionEnd":
		return "session_end"
	case "PreCompact", "PostCompact":
		return "compact"
	case "PostModelSwitch":
		return "model_switch"
	case "StopFailure":
		return "stop_failure"
	case "Interrupt":
		return "interrupt"
	}
	return ""
}

// failureType is v1's: ": " and the error's type when it is one of the
// API's own, else "".
func failureType(raw json.RawMessage) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		var kind string
		_ = json.Unmarshal(obj["type"], &kind)
		switch kind {
		case "rate_limit", "rate_limit_error", "overloaded_error", "api_error", "authentication_error", "permission_error", "invalid_request_error":
			return ": " + kind
		}
	}
	return ""
}

// promptEvents sets a prompt's author by the rules in their order, and
// gives the rule's word and the events, with Kind, Author, Text and Marks
// set.
func promptEvents(host adapter.ID, prompt, source string, agent bool, env Env) (string, []event.Event) {
	one := func(rule string, a event.Author, marks ...event.Mark) (string, []event.Event) {
		return rule, []event.Event{{Kind: event.KindPrompt, Author: a, Text: prompt, Marks: marks}}
	}
	head := strings.TrimLeftFunc(prompt, unicode.IsSpace)
	switch {
	case head == "":
		return RuleEmpty, nil
	case builtin(head):
		return RuleBuiltin, nil
	case agent:
		return one(RuleAgent, event.AuthorSubagent)
	case source != "":
		if e, ok := find(sources, strings.ToLower(source), false); ok {
			return one(RuleSource, e.Author)
		}
		return one(RuleSourceUnlisted, event.AuthorUnknown)
	case host == adapter.Codex && strings.HasPrefix(head, VoiceWrapper):
		// Only Codex writes a hand-over (F-141). On another host the form
		// is only the prompt's shape, which whoever wrote it chose, so the
		// prompt falls to the tag rule.
		if evs, ok := handOver(prompt); ok {
			return RuleVoice, evs
		}
		return one(RuleWrapper, event.AuthorUnknown, event.MarkWrapper)
	}
	if e, ok := find(prefixes, lowerHead(head), true); ok {
		return one(RulePrefix, e.Author)
	}
	if leadingTag(head) {
		return one(RuleTag, event.AuthorUnknown)
	}
	if host != adapter.ClaudeCode {
		return one(RuleNoEvidence, event.AuthorUnknown)
	}
	if e, ok := find(entrypoints, env.Entrypoint, false); ok {
		if e.Author == event.AuthorTyped {
			return one(RuleInteractive, e.Author)
		}
		return one(RuleProgram, e.Author)
	}
	return one(RuleEntrypointUnknown, event.AuthorUnknown)
}

// builtin reports a prompt whose first word is one of v1's builtin
// commands, in any letter case.
func builtin(head string) bool {
	word := head
	if i := strings.IndexFunc(head, unicode.IsSpace); i >= 0 {
		word = head[:i]
	}
	return slices.Contains(builtinCommands, strings.ToLower(word))
}

// leadingTag reports a text that begins with < and a letter.
func leadingTag(head string) bool {
	rest, ok := strings.CutPrefix(head, "<")
	if !ok {
		return false
	}
	for _, r := range rest {
		return unicode.IsLetter(r)
	}
	return false
}

// headRunes is how much of a prompt is lower-cased to match a prefix: more
// than the longest prefix on the list, which a test holds.
const headRunes = 64

// lowerHead lower-cases the first headRunes runes of head.
func lowerHead(head string) string {
	n := 0
	for i := range head {
		if n == headRunes {
			head = head[:i]
			break
		}
		n++
	}
	return strings.ToLower(head)
}

// find gives the entry of list whose value is s, or, when prefix is set,
// that s begins with.
func find(list []Entry, s string, prefix bool) (Entry, bool) {
	for _, e := range list {
		if s == e.Value || prefix && strings.HasPrefix(s, e.Value) {
			return e, true
		}
	}
	return Entry{}, false
}
