package main

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// stepRecord is what the recording holds for one step: how many times it
// was marked, and the events, command results and notes kept under it.
type stepRecord struct {
	marks, events, commands, notes int
}

func (r stepRecord) holds() bool { return r.events+r.commands+r.notes > 0 }

// recording is a recording read for summary and voice-check: never a
// payload's text but where a command reads one itself.
type recording struct {
	marks    []hostfacts.MarkLine
	events   []eventHead
	notes    []hostfacts.NoteLine
	outputs  []hostfacts.OutputLine
	perStep  map[string]stepRecord
	eventsBy map[string]map[string]int // host, then event, then count
}

func readRecording(rec string) (*recording, error) {
	r := &recording{perStep: map[string]stepRecord{}, eventsBy: map[string]map[string]int{}}
	var err error
	if r.marks, err = readLines[hostfacts.MarkLine](filepath.Join(rec, hostfacts.MarksFile)); err != nil {
		return nil, err
	}
	if r.events, err = readLines[eventHead](filepath.Join(rec, hostfacts.EventsFile)); err != nil {
		return nil, err
	}
	if r.notes, err = readLines[hostfacts.NoteLine](filepath.Join(rec, hostfacts.NotesFile)); err != nil {
		return nil, err
	}
	if r.outputs, err = readLines[hostfacts.OutputLine](filepath.Join(rec, hostfacts.OutputsFile)); err != nil {
		return nil, err
	}
	for _, m := range r.marks {
		if m.Kind == "mark" {
			s := r.perStep[m.Mark]
			s.marks++
			r.perStep[m.Mark] = s
		}
	}
	for _, ev := range r.events {
		s := r.perStep[ev.Mark]
		s.events++
		r.perStep[ev.Mark] = s
		if r.eventsBy[ev.Host] == nil {
			r.eventsBy[ev.Host] = map[string]int{}
		}
		r.eventsBy[ev.Host][ev.Event]++
	}
	for _, o := range r.outputs {
		s := r.perStep[o.Mark]
		s.commands++
		r.perStep[o.Mark] = s
	}
	for _, n := range r.notes {
		s := r.perStep[n.Mark]
		s.notes++
		r.perStep[n.Mark] = s
	}
	return r, nil
}

// scopeHosts are the hosts whose events a summary lists.
var scopeHosts = map[string][]string{"sitting-1": {"claude-code"}, "sitting-2": {"codex"}}

func cmdSummary(c *cli, args []string) int {
	if len(args) != 1 {
		return c.fail("usage: hostrec summary <sitting-1|sitting-2|group letter>")
	}
	steps, ok := hostfacts.Scope(args[0])
	if !ok {
		return c.fail("%q is not a sitting or a group of the run-book", args[0])
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	r, err := readRecording(e.rec)
	if err != nil {
		return c.fail("%v", err)
	}
	hosts, ok := scopeHosts[args[0]]
	if !ok {
		hosts = []string{"claude-code", "codex"}
	}
	c.say("Events that fired:")
	for _, h := range hosts {
		names := make([]string, 0, len(r.eventsBy[h]))
		for n := range r.eventsBy[h] {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			c.say("  %s: none", h)
		}
		for _, n := range names {
			c.say("  %s %s: %d", h, n, r.eventsBy[h][n])
		}
	}
	c.say("Each host CLI's version at each mark:")
	last := map[string]string{}
	changed := false
	for _, m := range r.marks {
		if m.Kind != "mark" || !slices.Contains(steps, m.Mark) {
			continue
		}
		var parts []string
		for _, h := range []string{"claude-code", "codex"} {
			v := m.Versions[h]
			parts = append(parts, h+" "+v)
			if prev, ok := last[h]; ok && prev != v {
				c.say("  WARNING: %s changed version before %s", h, m.Mark)
				changed = true
			}
			last[h] = v
		}
		c.say("  %s: %s", m.Mark, strings.Join(parts, "; "))
	}
	missing := 0
	c.say("Facts:")
	for _, f := range hostfacts.FactsIn(steps) {
		var held, inScope []string
		core := false
		for _, ref := range f.Refs() {
			if !slices.Contains(steps, ref.ID) {
				continue
			}
			inScope = append(inScope, ref.ID)
			core = core || ref.Core
			if s := r.perStep[ref.ID]; s.holds() {
				held = append(held, ref.ID)
			}
		}
		label := "evidence"
		if core {
			label = "core"
		}
		switch {
		case len(held) > 0:
			var parts []string
			for _, id := range held {
				s := r.perStep[id]
				parts = append(parts, id+": "+plural(s.events, "event", "events")+", "+plural(s.commands, "result", "results")+", "+plural(s.notes, "note", "notes"))
			}
			c.say("  %s (%s) %s: recorded at %s", f.ID, label, f.Title, strings.Join(parts, "; "))
		case core:
			missing++
			c.say("  %s (core) %s: MISSING; settled by %s", f.ID, f.Title, strings.Join(inScope, ", "))
		default:
			c.say("  %s (evidence) %s: nothing recorded at %s", f.ID, f.Title, strings.Join(inScope, ", "))
		}
	}
	_ = e.command("summary", map[string]any{"scope": args[0], "core_missing": missing, "version_changed": changed})
	if missing > 0 {
		c.say("%s missing.", plural(missing, "core fact", "core facts"))
		return 1
	}
	return 0
}

// handOverTag is the voice wrapper Codex puts around a request the voice
// model hands to the agent.
const handOverTag = "realtime_delegation"

var inputPart = regexp.MustCompile(`(?s)<input>(.*?)</input>`)

// voiceEvent is what voice-check reads of an event: its payload's prompt
// and the record's rows, which it counts and never prints.
type voiceEvent struct {
	Mark          string              `json:"mark"`
	Wall          string              `json:"wall"`
	Host          string              `json:"host"`
	Event         string              `json:"event"`
	Payload       map[string]any      `json:"payload"`
	PayloadHashes map[string]string   `json:"payload_hashes"`
	Record        *hostfacts.Snapshot `json:"record"`
}

func cmdVoiceCheck(c *cli, args []string) int {
	if len(args) != 0 {
		return c.fail("usage: hostrec voice-check")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	events, err := readLines[voiceEvent](filepath.Join(e.rec, hostfacts.EventsFile))
	if err != nil {
		return c.fail("%v", err)
	}
	var call []voiceEvent
	for _, ev := range events {
		if ev.Mark == "X-44" && ev.Host == "codex" {
			call = append(call, ev)
		}
	}
	rowTypes := map[string]int{}
	var versionPaths []string
	addPath := func(p string) {
		if strings.Contains(strings.ToLower(p), "version") && !slices.Contains(versionPaths, p) {
			versionPaths = append(versionPaths, p)
		}
	}
	type handOver struct {
		label    string
		kept, of int
		talk     int
	}
	var hands []handOver
	talk := 0
	voiceValues := map[string]string{}
	for _, ev := range call {
		for p := range ev.PayloadHashes {
			addPath(p)
		}
		if ev.Record != nil {
			for _, row := range ev.Record.Rows {
				rowTypes[orUnknown(row.RowType)]++
				if row.RowType == "realtime_item" {
					talk += row.Size
				}
				for _, f := range row.Fields {
					addPath(f.Path)
				}
			}
		}
		voiceVersions(ev.Payload, "", voiceValues)
		prompt := strings.TrimLeft(str(ev.Payload, "prompt"), " \t\r\n")
		if ev.Event == "UserPromptSubmit" && strings.HasPrefix(prompt, "<"+handOverTag+">") {
			request := prompt
			if m := inputPart.FindStringSubmatch(prompt); m != nil {
				request = m[1]
			}
			label, kept, of := hostfacts.ClosestLine(request)
			hands = append(hands, handOver{label, kept, of, talk})
			talk = 0
		}
	}
	sort.Strings(versionPaths)
	c.say("The voice call (X-44): %s from Codex, %s.", plural(len(call), "event", "events"), plural(len(hands), "hand-over", "hand-overs"))
	types := make([]string, 0, len(rowTypes))
	for t := range rowTypes {
		types = append(types, t)
	}
	sort.Strings(types)
	c.say("Rows of the session file, by type:")
	for _, t := range types {
		c.say("  %s: %d", t, rowTypes[t])
	}
	var results []map[string]any
	for i, h := range hands {
		c.say("Hand-over %d: closest to the script's line %s, %d of its %d words kept; %d bytes of talk before it.", i+1, h.label, h.kept, h.of, h.talk)
		results = append(results, map[string]any{"line": h.label, "kept": h.kept, "of": h.of, "talk_bytes": h.talk})
	}
	c.say("Version fields carried: %s.", orNoneList(versionPaths))
	for _, p := range slices.Sorted(maps.Keys(voiceValues)) {
		c.say("  %s names the voice feature's version: %s", p, voiceValues[p])
	}
	data, _ := json.Marshal(results)
	var decoded any
	_ = json.Unmarshal(data, &decoded)
	_ = e.command("voice-check", map[string]any{"events": len(call), "hand_overs": decoded, "row_types": rowTypes, "version_paths": versionPaths, "voice_versions": voiceValues, "at": time.Now().UTC().Format(time.RFC3339)})
	return 0
}

// voiceVersions keeps the value of each payload field whose name says it
// is the voice feature's version: a version field under a voice or
// realtime name, holding a short value.
func voiceVersions(v any, path string, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			p := k
			if path != "" {
				p = path + "." + k
			}
			voiceVersions(x, p, out)
		}
	case string:
		lower := strings.ToLower(path)
		if strings.Contains(lower, "version") && (strings.Contains(lower, "voice") || strings.Contains(lower, "realtime")) && len(t) <= 40 {
			out[path] = t
		}
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "(no type)"
	}
	return s
}
