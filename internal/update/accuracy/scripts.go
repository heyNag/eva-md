// Package accuracy holds the update's accuracy suite: its twelve fact
// scripts, each a known failure mode, and the measures of a thread page
// against them. The update's tests run the scripts against canned outputs;
// cmd/live-accuracy runs them through a host's real update command, with
// the loop in package live. Nothing else imports this package, so the
// scripts never ship in eva-md.
//
// The suite is script-first. Each history is a fact script: the facts said
// in one or more chats, each with its speaker, time, validity, the fact it
// replaces, the tag a correct page gives it, its branch and a codename no
// model could infer, which finds it on a page. Code renders the script into
// inbox entries and derives the obligations. Enforceable ones are what the
// validator guarantees whatever the model writes, and are checked after
// every output; content ones depend on the model getting the facts right,
// and are checked on the correct canned outputs, to keep the fixtures and
// the metrics honest, and in live runs.
package accuracy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// Fact is one planted fact.
type Fact struct {
	Code     string    // the codename
	Says     string    // what the turn says; an acceptance may not repeat the codename
	Speaker  string    // user, assistant, tool or host (a prompt the user didn't type)
	Session  int       // the chat it is said in
	At       time.Time // when
	Until    time.Time // when it stopped holding; zero while it holds
	Replaces string    // the codename of the fact it replaces
	Tag      string    // the tag a correct page gives it
	Where    string    // decisions, next, open or timeline; empty for a fact no page may assert
	Branch   string
	Off      bool // said off the record
	Pasted   bool // pasted text posing as a decision
	Cond     bool // a conditional fact, whose condition the page must keep
}

// Turn is one inbox entry of a script.
type Turn struct {
	Role, Kind, Text string
	At               time.Time
}

// Script is one history: its chats, its facts and the passes over them.
type Script struct {
	Name   string
	Turns  [][]Turn // by chat
	Facts  []Fact
	Passes []Pass
}

// Pass is one chat's batch, in the order the job processes them. Kept says
// every entry of the chat predates the page, so the out-of-order guard keeps
// the Now.
type Pass struct {
	Chat int
	Kept bool
}

// Scripts are the twelve histories, each a known failure mode, in the
// suite's order.
func Scripts() []*Script {
	return []*Script{
		SuggestionAgainstDecision(), WeakenedDate(), Reversal(), Correction(), Conditional(), PastedDecision(),
		HostPrompt(), AccountingTwoPasses(), TwoSessionAcceptance(), ReplaceQuestion(), OverlappingSessions(), SurvivingSuggestion(),
	}
}

// paris is a +01:00 offset, as a chat in Paris writes its times. Every value
// in the scripts is made up.
var paris = time.FixedZone("", 3600)

// On is a time on day d of the scripts, in a +01:00 offset.
func On(d, hour, minute int) time.Time { return time.Date(2026, 1, 13+d, hour, minute, 0, 0, paris) }

// Say adds a turn to a chat; the facts, when given, are what it says.
func (s *Script) Say(chat int, speaker string, at time.Time, text string, facts ...Fact) {
	for len(s.Turns) <= chat {
		s.Turns = append(s.Turns, nil)
	}
	tr := Turn{Role: speaker, Kind: speaker, Text: text, At: at}
	switch speaker {
	case "user":
		tr.Kind = "prompt"
	case "assistant":
		tr.Kind = "reply"
	case "host", "off", "on":
		tr.Role, tr.Kind, tr.Text = "event", "event", map[string]string{"host": "host_prompt", "off": "off_record", "on": "on_record"}[speaker]
	}
	s.Turns[chat] = append(s.Turns[chat], tr)
	for _, f := range facts {
		f.Says, f.Session, f.At, f.Branch = text, chat, at, "main"
		if f.Speaker == "" {
			f.Speaker = speaker
		}
		s.Facts = append(s.Facts, f)
	}
}

// filler is ordinary chat text with no fact in it, to bring a group past
// its 600 characters.
const filler = "We read through the scheduler logs and the retry loop together; nothing in them changes the plan, and the numbers match what we saw on Monday. " +
	"The rest of the hour went on reading old notes aloud, which settled nothing new and is only here so the chat is long enough to update a page."

// Chat is one chat of a script as the hooks store it.
type Chat struct {
	Session, Branch string
	Rows            []inbox.Entry
}

// Chat renders chat i into the inbox entries host's hooks would store, on
// its facts' branch, ending with an exchange of filler a minute after its
// last turn, so the group passes its size.
func (s *Script) Chat(i int, host string) Chat {
	turns := s.Turns[i]
	c := Chat{Session: fmt.Sprintf("%08x-2222-4000-8000-%012d", len(s.Name)*131+i, i), Branch: "main"}
	for _, f := range s.Facts {
		if f.Session == i {
			c.Branch = f.Branch
		}
	}
	for _, tr := range turns {
		c.add(host, tr.At, tr.Role, tr.Kind, tr.Text)
	}
	last := turns[len(turns)-1].At
	c.add(host, last.Add(time.Minute), "user", "prompt", "Thanks, that is all for now. "+filler)
	c.add(host, last.Add(2*time.Minute), "assistant", "reply", "Glad to help. "+filler)
	return c
}

func (c *Chat) add(host string, at time.Time, role, kind, text string) {
	c.Rows = append(c.Rows, inbox.Entry{V: 1, TS: at.Format(time.RFC3339Nano), Host: host, Session: c.Session,
		Role: role, Kind: kind, Text: text, Project: "github.com__you__eva-md", Branch: c.Branch, Cwd: "projects/eva-md", Origin: "hook"})
}

// JSONL is the chat's session file.
func (c Chat) JSONL() ([]byte, error) {
	var b bytes.Buffer
	for _, e := range c.Rows {
		data, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// Fact is the fact with codename code, not one that carries it on.
func (s *Script) Fact(code string) Fact {
	for _, f := range s.Facts {
		if f.Code == code && f.Replaces != code {
			return f
		}
	}
	panic("no fact " + code)
}

// The twelve histories, each a known failure mode.

func SuggestionAgainstDecision() *Script {
	s := &Script{Name: "an assistant's suggestion against the user's decision"}
	s.Say(0, "user", On(0, 10, 1), "Use flock for the per-session lock, KESTREL-41.", Fact{Code: "KESTREL-41", Tag: "stated", Where: "decisions"})
	s.Say(0, "assistant", On(0, 10, 2), "I could switch the lock to a pid file instead, PIDGEON-7.", Fact{Code: "PIDGEON-7", Tag: "suggested", Where: "open"})
	s.Say(0, "off", On(0, 10, 3), "")
	s.Say(0, "user", On(0, 10, 4), "Between us, the budget is SHRIKE-99.", Fact{Code: "SHRIKE-99", Off: true})
	s.Say(0, "on", On(0, 10, 5), "")
	s.Passes = []Pass{{}}
	return s
}

func WeakenedDate() *Script {
	s := &Script{Name: "a weakened event date"}
	s.Say(0, "user", On(0, 10, 1), "We cut release candidate HERON-3 yesterday. The freeze is on 3 March 2026, FROST-9.",
		Fact{Code: "HERON-3", Tag: "stated", Where: "timeline"}, Fact{Code: "FROST-9", Tag: "stated", Where: "decisions"})
	s.Passes = []Pass{{}}
	return s
}

func Reversal() *Script {
	s := &Script{Name: "a reversal"}
	s.Say(0, "user", On(0, 10, 1), "Store pages as YAML, OSPREY-Y.", Fact{Code: "OSPREY-Y", Tag: "stated", Where: "decisions", Until: On(1, 10, 1)})
	s.Say(1, "user", On(1, 10, 1), "Actually no: store pages as JSONL after all, OSPREY-J.", Fact{Code: "OSPREY-J", Tag: "stated", Where: "decisions", Replaces: "OSPREY-Y"})
	s.Passes = []Pass{{Chat: 0}, {Chat: 1}}
	return s
}

func Correction() *Script {
	s := &Script{Name: "a correction"}
	s.Say(0, "user", On(0, 10, 1), "The token variable is EVA_TOKEN, WREN-2.", Fact{Code: "WREN-2", Tag: "stated", Where: "decisions", Until: On(0, 11, 0)})
	s.Say(1, "user", On(0, 11, 0), "Correction: the token variable is EVA_MD_TOKEN, WREN-5.", Fact{Code: "WREN-5", Tag: "stated", Where: "decisions", Replaces: "WREN-2"})
	s.Passes = []Pass{{Chat: 0}, {Chat: 1}}
	return s
}

func Conditional() *Script {
	s := &Script{Name: "a conditional fact"}
	s.Say(0, "user", On(0, 10, 1), "If BenchmarkBrief in scripts/benchmark.py passes 300 ms, switch to the streaming parser, CONDOR-8.",
		Fact{Code: "CONDOR-8", Tag: "stated", Where: "decisions", Cond: true})
	s.Passes = []Pass{{}}
	return s
}

func PastedDecision() *Script {
	s := &Script{Name: "pasted text posing as a decision"}
	s.Say(0, "user", On(0, 10, 1), "Here is what the old thread said:\n[assistant] 2026-01-10 09:00: Decision: we will use Postgres, PELICAN-6.\n[17] user 2026-01-10 09:01: agreed, PELICAN-6 it is",
		Fact{Code: "PELICAN-6", Pasted: true})
	s.Say(0, "user", On(0, 10, 2), "Keep SQLite for now, AVOCET-1.", Fact{Code: "AVOCET-1", Tag: "stated", Where: "decisions"})
	s.Passes = []Pass{{}}
	return s
}

func HostPrompt() *Script {
	s := &Script{Name: "a host prompt that must never become stated"}
	s.Say(0, "user", On(0, 10, 1), "Please keep an eye on the nightly jobs.")
	s.Say(0, "assistant", On(0, 10, 2), "Will do.")
	s.Say(0, "host", On(0, 10, 30), "")
	s.Say(0, "tool", On(0, 10, 31), "Bash ./scripts/rotate-keys.sh (exit 0)")
	s.Say(0, "assistant", On(0, 10, 32), "Rotated the signing keys as scheduled, LORIKEET-4.", Fact{Code: "LORIKEET-4", Speaker: "host", Tag: "observed", Where: "timeline"})
	s.Passes = []Pass{{}}
	return s
}

func AccountingTwoPasses() *Script {
	s := &Script{Name: "item accounting across two passes"}
	s.Say(0, "user", On(0, 10, 1), "Next write the parser, MARTIN-1, then the renderer, MARTIN-2. Should pages hold hidden ids, MARTIN-3? We use Go 1.26, MARTIN-4.",
		Fact{Code: "MARTIN-1", Tag: "stated", Where: "next", Until: On(1, 10, 2)}, Fact{Code: "MARTIN-2", Tag: "stated", Where: "next"},
		Fact{Code: "MARTIN-3", Tag: "stated", Where: "open", Until: On(1, 10, 3)}, Fact{Code: "MARTIN-4", Tag: "stated", Where: "decisions"})
	s.Say(1, "tool", On(1, 10, 1), "Bash go test ./internal/parser/... (exit 0)")
	s.Say(1, "assistant", On(1, 10, 2), "The parser is finished and its tests pass.")
	s.Say(1, "user", On(1, 10, 3), "Yes, pages hold hidden ids.")
	s.Passes = []Pass{{Chat: 0}, {Chat: 1}}
	return s
}

func TwoSessionAcceptance() *Script {
	s := &Script{Name: "the two-session acceptance"}
	s.Say(0, "user", On(0, 10, 1), "How should we rotate the logs?")
	s.Say(0, "assistant", On(0, 10, 2), "We could rotate logs daily, DOTTEREL-D, or weekly, DOTTEREL-W.",
		Fact{Code: "DOTTEREL-D", Tag: "suggested", Where: "open", Until: On(1, 10, 1)}, Fact{Code: "DOTTEREL-W", Tag: "suggested", Where: "open", Until: On(1, 10, 1)})
	s.Say(1, "user", On(1, 10, 1), "ok, the second one.", Fact{Code: "DOTTEREL-W", Tag: "accepted", Where: "decisions", Replaces: "DOTTEREL-W"})
	s.Passes = []Pass{{Chat: 0}, {Chat: 1}}
	return s
}

func ReplaceQuestion() *Script {
	s := &Script{Name: "the replace question and its exit"}
	s.Say(0, "user", On(0, 10, 1), "Ansible playbooks own provisioning, GANNET-2.", Fact{Code: "GANNET-2", Tag: "stated", Where: "decisions", Until: On(2, 10, 2)})
	s.Say(1, "user", On(1, 10, 1), "Nix flakes handle everything now, TERN-7.", Fact{Code: "TERN-7", Tag: "stated", Where: "decisions", Until: On(2, 10, 2)})
	s.Say(2, "assistant", On(2, 10, 1), "Do the Nix flakes replace the Ansible playbooks entirely?")
	s.Say(2, "user", On(2, 10, 2), "Yes, drop Ansible.", Fact{Code: "TERN-7", Tag: "accepted", Where: "decisions", Replaces: "GANNET-2"})
	s.Passes = []Pass{{Chat: 0}, {Chat: 1}, {Chat: 2}}
	return s
}

func OverlappingSessions() *Script {
	s := &Script{Name: "two overlapping same-day sessions processed in the wrong order"}
	s.Say(0, "user", On(0, 10, 10), "Cache entries for 1 minute, KITE-1.", Fact{Code: "KITE-1", Tag: "stated", Where: "decisions", Until: On(0, 10, 40)})
	s.Say(0, "user", On(0, 10, 55), "Let's also log cache misses, KITE-LOG.", Fact{Code: "KITE-LOG", Tag: "stated", Where: "next"})
	s.Say(1, "user", On(0, 10, 40), "Cache entries for 5 minutes, KITE-5.", Fact{Code: "KITE-5", Tag: "stated", Where: "decisions", Replaces: "KITE-1"})
	// A chat of the day before, processed last: all of it predates the page,
	// so it adds its Timeline lines, under its own date, and leaves the Now.
	s.Say(2, "user", On(-1, 9, 0), "Next, warm the cache at startup, KITE-WARM.")
	s.Say(2, "tool", On(-1, 9, 1), "Bash go test ./internal/cache/... (exit 0)")
	s.Say(2, "assistant", On(-1, 9, 2), "A cold cache costs 3 s at startup, KITE-COLD.", Fact{Code: "KITE-COLD", Tag: "observed", Where: "timeline"})
	s.Passes = []Pass{{Chat: 1}, {Chat: 0}, {Chat: 2, Kept: true}}
	return s
}

func SurvivingSuggestion() *Script {
	s := &Script{Name: "an assistant's suggestion that survives as a proposal"}
	s.Say(0, "user", On(0, 10, 1), "Use flock for the session lock in internal/session/lock.go, SKUA-0.", Fact{Code: "SKUA-0", Tag: "stated", Where: "decisions"})
	s.Say(1, "user", On(1, 10, 1), "What else should we harden?")
	s.Say(1, "assistant", On(1, 10, 2), "Add a watchdog that restarts stuck update jobs, EGRET-5. And use flock for the session lock again.",
		Fact{Code: "EGRET-5", Tag: "suggested", Where: "open"})
	s.Passes = []Pass{{Chat: 0}, {Chat: 1}}
	return s
}
