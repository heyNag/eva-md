package capture

import (
	"encoding/json"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// truth is what the generator knows of a prompt it made.
type truth struct {
	handOver bool     // it begins with the wrapper, in form or not
	inForm   bool     // a hand-over in form
	spoken   []string // the person's lines of a hand-over in form, unescaped
}

// gen makes prompts from a seeded source.
type gen struct{ r *rand.Rand }

func (g gen) pick(s []string) string { return s[g.r.IntN(len(s))] }

// words draws n made-up words, some of them with the characters a
// hand-over escapes.
func (g gen) words(n int) string {
	pool := []string{"made-up", "alpha", "beta", "off the record", "on the record", "yes", "no", "fourteen", "a<b", "c>d", "x&y", "&lt;", "&amp;", "…", "user:", "assistant: x", "<realtime_delegation>", "é", "line\twith tab"}
	var out []string
	for range n {
		out = append(out, g.pick(pool))
	}
	return strings.Join(out, " ")
}

// cased changes the case of some ASCII letters of s.
func (g gen) cased(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' && g.r.IntN(3) == 0 {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func (g gen) space() string {
	return g.pick([]string{"", "", "", " ", "\n", " \t\r\n", " ", " "})
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// handOver makes a hand-over in form, and what each of its lines is.
func (g gen) handOver() (string, truth) {
	tr := truth{handOver: true, inForm: true}
	request := "do " + g.words(1+g.r.IntN(3))
	source := ""
	switch g.r.IntN(8) {
	case 0:
		request = ""
	case 1:
		source = TailFlush
		request = "The user just ended their realtime session."
	case 2:
		request += cutMark // the host cut it
	}
	var lines []string
	if g.r.IntN(5) == 0 {
		// The cut left the first line short.
		lines = append(lines, cutMark+g.pick([]string{"user: ", "assistant: ", "ser: ", ""})+g.words(1))
	}
	for range g.r.IntN(6) {
		switch g.r.IntN(7) {
		case 0, 1, 2:
			said := "said " + g.words(1+g.r.IntN(3))
			lines = append(lines, headSpoken+said)
			tr.spoken = append(tr.spoken, said)
		case 3, 4:
			lines = append(lines, headFront+"reply "+g.words(1+g.r.IntN(2)))
		case 5:
			lines = append(lines, g.pick([]string{"system: ", "User: ", "user:", " user: ", "ASSISTANT: ", ""})+g.words(1))
		case 6:
			// The host adds the request as a user line, whole, where
			// the request may be cut.
			if kept, cut := strings.CutSuffix(request, cutMark); cut {
				lines = append(lines, headSpoken+kept+g.pick([]string{"", " and the rest"}))
			} else if request != "" {
				lines = append(lines, headSpoken+request)
			}
		}
	}
	transcript := strings.Join(lines, "\n")
	if transcript == "" && g.r.IntN(2) == 0 {
		transcript = headSpoken + "said alone"
		tr.spoken = append(tr.spoken, "said alone")
	}
	input := escape(request)
	if request == "" && transcript != "" && g.r.IntN(2) == 0 {
		input = escape(transcript) // the host's stand-in for an empty request
	}
	s := g.space()
	if s == " " || s == " " {
		s = " "
	}
	s += VoiceWrapper + "\n"
	if source != "" {
		s += "  <source>" + source + "</source>\n"
	}
	s += "  <input>" + input + "</input>\n"
	if transcript != "" {
		s += "  <transcript_delta>" + escape(transcript) + "</transcript_delta>\n"
	}
	return s + voiceClose + g.pick([]string{"", "\n", " \r\n"}), tr
}

// outOfForm breaks a hand-over in form in one way, each of which no form
// of Codex's has.
func (g gen) outOfForm() (string, truth) {
	s, _ := g.handOver()
	breaks := []func(string) string{
		func(s string) string { return strings.Replace(s, "  <input>", " <input>", 1) },
		func(s string) string { return s + " typed after" },
		func(s string) string { return strings.Replace(s, "  <input>", "  <source>made_up</source>\n  <input>", 1) },
		func(s string) string { return strings.Replace(s, "  <input>", "  <input>& ", 1) },
		func(s string) string { return strings.Replace(s, "  <input>", "  <input>a<b ", 1) },
		func(s string) string { return strings.Replace(s, voiceClose, "", 1) },
		func(s string) string { return strings.Replace(s, voiceClose, "  <extra>x</extra>\n"+voiceClose, 1) },
		func(s string) string { return strings.Replace(s, VoiceWrapper+"\n", VoiceWrapper, 1) },
		func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") },
		func(s string) string { return " " + s },
		func(s string) string { return strings.Replace(s, "</input>", "</inputs>", 1) },
	}
	return breaks[g.r.IntN(len(breaks))](s), truth{handOver: true}
}

func (g gen) prompt() (string, truth) {
	switch g.r.IntN(12) {
	case 0:
		return g.space(), truth{}
	case 1:
		return g.space() + g.cased(g.pick(builtinCommands)) + g.pick([]string{"", " arg", "\targ", "x"}), truth{}
	case 2:
		return g.space() + g.cased(g.pick(values(prefixes))) + g.words(1), truth{}
	case 3:
		return g.space() + "<" + g.pick([]string{"made-up", "Made", "é", "1", " ", "/x", "!--"}) + ">" + g.words(1), truth{}
	case 4, 5:
		return g.handOver()
	case 6:
		return g.outOfForm()
	case 7:
		w := g.cased(VoiceWrapper)
		return g.space() + w + g.words(1), truth{handOver: w == VoiceWrapper}
	}
	return g.space() + g.words(1+g.r.IntN(4)), truth{}
}

var leadingTagRE = regexp.MustCompile(`^<\pL`)

// wantTyped is item 3 of the card for typed, written apart from the
// decoder: every condition must hold.
func wantTyped(host adapter.ID, name, prompt, source, agent, entrypoint string) bool {
	if host != adapter.ClaudeCode || name != "UserPromptSubmit" || agent != "" || source != "" {
		return false
	}
	head := strings.TrimLeftFunc(prompt, unicode.IsSpace)
	fields := strings.Fields(head)
	if len(fields) == 0 || slices.Contains(builtinCommands, strings.ToLower(fields[0])) || strings.HasPrefix(head, "<realtime_delegation>") {
		return false
	}
	for _, p := range values(prefixes) {
		if strings.HasPrefix(strings.ToLower(head), p) {
			return false
		}
	}
	return !leadingTagRE.MatchString(head) && slices.Contains([]string{"cli", "claude-desktop", "claude-vscode"}, entrypoint)
}

// A seeded run over hosts, events, entrypoints, sources, agent ids and
// prompts: typed only where every condition holds, spoken only from a
// person's line of a hand-over in form, and nothing of Codex typed.
func TestGeneratedPayloads(t *testing.T) {
	g := gen{rand.New(rand.NewPCG(2026, 1113))}
	hosts := []adapter.ID{adapter.ClaudeCode, adapter.Codex, "made-up-host"}
	names := []string{"UserPromptSubmit", "Stop", "PostToolUse", "PostToolUseFailure", "PostCompact", "PreCompact", "SessionStart", "SessionEnd", "StopFailure", "PostModelSwitch", "Interrupt", "SubagentStop", "made-up"}
	entrypoints := append(values(entrypoints), "claude-desktop-3p", "local-agent", "", "CLI", "cli ", "made-up")
	sources := append(values(sources), "SCHEDULED", " cron", "made-up", "")
	typed, spoken, wrappers := 0, 0, 0
	for i := range 40000 {
		host, entrypoint := g.pick(hostStrings(hosts)), g.pick(entrypoints)
		name := "UserPromptSubmit"
		if g.r.IntN(3) == 0 {
			name = g.pick(names)
		}
		prompt, tr := g.prompt()
		source, agent := "", ""
		if g.r.IntN(5) == 0 {
			source = g.pick(sources)
		}
		if g.r.IntN(6) == 0 {
			agent = "agent-made-up"
		}
		p := map[string]any{"hook_event_name": name, "session_id": "s-made-up", "turn_id": "t-made-up", "prompt": prompt,
			"last_assistant_message": g.words(1), "compact_summary": g.pick([]string{"", "made-up summary"}), "source": source}
		if agent != "" {
			p["agent_id"] = agent
		}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		d, err := Decode(adapter.ID(host), raw, Env{Entrypoint: entrypoint})
		if err != nil {
			t.Fatalf("payload %d: %v", i, err)
		}
		want := wantTyped(adapter.ID(host), name, prompt, source, agent, entrypoint)
		got := 0
		for j, e := range d.Events {
			e.Seq, e.Src.Key = int64(j+1), "k"
			if err := e.Validate(); err != nil {
				t.Fatalf("payload %d %q: %v", i, prompt, err)
			}
			switch e.Author {
			case event.AuthorTyped:
				got++
				if host != string(adapter.ClaudeCode) {
					t.Fatalf("payload %d: a %s prompt is typed: %q", i, host, prompt)
				}
			case event.AuthorSpoken:
				spoken++
				if !tr.inForm || d.Rule != RuleVoice || !slices.Contains(tr.spoken, e.Text) {
					t.Fatalf("payload %d: %q spoken from %q (rule %s, in form %v)", i, e.Text, prompt, d.Rule, tr.inForm)
				}
			case event.AuthorFront:
				if d.Rule != RuleVoice {
					t.Fatalf("payload %d: a front model's line outside a hand-over: %q", i, prompt)
				}
			}
		}
		if got > 1 || (got == 1) != want {
			t.Fatalf("payload %d on %s, entrypoint %q, source %q, agent %q: %d typed, want %v, for %q (rule %s)", i, host, entrypoint, source, agent, got, want, prompt, d.Rule)
		}
		typed += got
		// A hand-over decides when no rule before it does.
		if tr.handOver && name == "UserPromptSubmit" && host != "made-up-host" && agent == "" && source == "" {
			if tr.inForm {
				var people []string
				for _, e := range d.Events {
					if e.Author == event.AuthorSpoken {
						people = append(people, e.Text)
					}
				}
				if d.Rule != RuleVoice || !slices.Equal(people, tr.spoken) {
					t.Fatalf("payload %d: %q gave %s %v, want the person's lines %q", i, prompt, d.Rule, people, tr.spoken)
				}
			} else {
				wrappers++
				if d.Rule != RuleWrapper || len(d.Events) != 1 || d.Events[0].Author != event.AuthorUnknown || d.Events[0].Text != prompt || !d.Events[0].Has(event.MarkWrapper) {
					t.Fatalf("payload %d: out of form %q gave %s %+v", i, prompt, d.Rule, d.Events)
				}
			}
		}
	}
	if typed < 500 || spoken < 2000 || wrappers < 300 {
		t.Fatalf("the run made too few cases: %d typed, %d spoken, %d out of form", typed, spoken, wrappers)
	}
}

func hostStrings(hosts []adapter.ID) []string {
	var out []string
	for _, h := range hosts {
		out = append(out, string(h))
	}
	return out
}
