package scenario

import (
	"fmt"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// V1 is v1's 12 accuracy scripts, ported in the suite's order as v1.01 to
// v1.12, family v1, all tuning. Each keeps its turns, times, codenames and
// passes, and renders to the inbox entries the suite renders. Every fact
// gets its codename as its first slot, an ident, and each value its words
// hold as a slot of its own. Where the port judges a fact differently from
// v1's Measure, a comment at the fact says so.
func V1() []Scenario {
	return []Scenario{
		suggestionAgainstDecision(), weakenedDate(), reversal(), correction(), conditional(), pastedDecision(),
		hostPrompt(), accountingTwoPasses(), twoSessionAcceptance(), replaceQuestion(), overlappingSessions(), survivingSuggestion(),
	}
}

// paris is a +01:00 offset, as a chat in Paris writes its times.
var paris = time.FixedZone("", 3600)

// on is a time on day d of the scripts, in a +01:00 offset.
func on(d, hour, minute int) time.Time { return time.Date(2026, 1, 13+d, hour, minute, 0, 0, paris) }

// plant is a fact as the port writes it: its codename, which becomes its
// first slot, the time its validity ends, which becomes the turn said then,
// and the codename of the fact it supersedes, which becomes the latest fact
// planted before it with that codename.
type plant struct {
	code       string
	kinds      []Kind
	authority  Authority
	must       Must
	slots      []Slot
	until      time.Time
	supersedes string
}

func slot(class SlotClass, value string, forms ...string) Slot {
	return Slot{Class: class, Value: value, Forms: forms}
}

func kindsOf(k ...Kind) []Kind { return k }

// port builds one ported script.
type port struct {
	sc    Scenario
	until map[string]time.Time // a fact's id to the time it ends
}

func newPort(n int, name string) *port {
	return &port{sc: Scenario{ID: fmt.Sprintf("v1.%02d", n), Family: "v1", Name: name, Split: SplitTuning}, until: map[string]time.Time{}}
}

// say adds a turn to session chat, as the suite's Say does, with the facts
// it plants. A speaker is user, assistant, tool, host (a prompt a program
// sent), off or on.
func (p *port) say(chat int, speaker string, at time.Time, text string, plants ...plant) {
	for len(p.sc.Sessions) <= chat {
		i := len(p.sc.Sessions)
		p.sc.Sessions = append(p.sc.Sessions, Session{Host: "claude-code", ID: fmt.Sprintf("%08x-2222-4000-8000-%012d", len(p.sc.Name)*131+i, i), Branch: "main"})
	}
	t := Turn{At: at, Text: text}
	switch speaker {
	case "user":
		t.Author, t.Kind = event.AuthorTyped, event.KindPrompt
	case "assistant":
		t.Author, t.Kind = event.AuthorAgent, event.KindReply
	case "tool":
		t.Author, t.Kind = event.AuthorTool, event.KindTool
	case "host":
		t.Author, t.Kind = event.AuthorProgram, event.KindPrompt
	case "off", "on":
		t.Author, t.Kind, t.Op = event.AuthorEva, event.KindControl, speaker
	default:
		panic("scenario: unknown speaker " + speaker)
	}
	for _, pl := range plants {
		f := Fact{ID: fmt.Sprintf("f%d", len(p.sc.Facts)+1), Kinds: pl.kinds, Authority: pl.authority, Must: pl.must, Slots: pl.slots}
		if pl.code != "" {
			f.Slots = append([]Slot{slot(SlotIdent, pl.code)}, pl.slots...)
		}
		if slices.Contains(f.Kinds, KindLog) {
			f.LogDate = at.Format(time.DateOnly)
		}
		if pl.supersedes != "" {
			for _, g := range slices.Backward(p.sc.Facts) {
				if g.Slots[0].Class == SlotIdent && g.Slots[0].Value == pl.supersedes {
					f.Supersedes = g.ID
					break
				}
			}
			if f.Supersedes == "" {
				panic("scenario: nothing planted before has codename " + pl.supersedes)
			}
		}
		if !pl.until.IsZero() {
			p.until[f.ID] = pl.until
		}
		p.sc.Facts = append(p.sc.Facts, f)
		t.Plants = append(t.Plants, f.ID)
	}
	p.sc.Sessions[chat].Turns = append(p.sc.Sessions[chat].Turns, t)
}

// done sets the passes and each fact's end, the one turn said at the time
// it ends.
func (p *port) done(passes ...Pass) Scenario {
	for i, f := range p.sc.Facts {
		end, ok := p.until[f.ID]
		if !ok {
			continue
		}
		var refs []Ref
		for _, s := range p.sc.Sessions {
			for n, t := range s.Turns {
				if t.At.Equal(end) {
					refs = append(refs, Ref{Session: s.ID, Seq: int64(n + 1)})
				}
			}
		}
		if len(refs) != 1 {
			panic(fmt.Sprintf("scenario: %s: %d turns end fact %s", p.sc.ID, len(refs), f.ID))
		}
		p.sc.Facts[i].Until = &refs[0]
	}
	p.sc.Passes = passes
	return p.sc
}

func suggestionAgainstDecision() Scenario {
	p := newPort(1, "an assistant's suggestion against the user's decision")
	p.say(0, "user", on(0, 10, 1), "Use flock for the per-session lock, KESTREL-41.",
		plant{code: "KESTREL-41", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotIdent, "flock")}})
	p.say(0, "assistant", on(0, 10, 2), "I could switch the lock to a pid file instead, PIDGEON-7.",
		plant{code: "PIDGEON-7", kinds: kindsOf(KindOpen), authority: AuthorityOther, must: MustStore, slots: []Slot{slot(SlotPhrase, "pid file")}})
	p.say(0, "off", on(0, 10, 3), "")
	// v1 checks only that no Now item asserts it; the port keeps it out of
	// the store entirely.
	p.say(0, "user", on(0, 10, 4), "Between us, the budget is SHRIKE-99.",
		plant{code: "SHRIKE-99", kinds: kindsOf(KindFact, KindConstraint), authority: AuthorityUser, must: MustNotStore})
	p.say(0, "on", on(0, 10, 5), "")
	return p.done(Pass{})
}

func weakenedDate() Scenario {
	p := newPort(2, "a weakened event date")
	// v1 checks only HERON-3's dated line; the port also asks the line to
	// keep when it happened, yesterday, which a line under the day it was
	// said otherwise misdates. The line stands under that day, as v1's.
	p.say(0, "user", on(0, 10, 1), "We cut release candidate HERON-3 yesterday. The freeze is on 3 March 2026, FROST-9.",
		plant{code: "HERON-3", kinds: kindsOf(KindLog), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotDate, "yesterday", "2026-01-12")}},
		plant{code: "FROST-9", kinds: kindsOf(KindDecision, KindFact), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotDate, "3 March 2026", "2026-03-03")}})
	return p.done(Pass{})
}

func reversal() Scenario {
	p := newPort(3, "a reversal")
	p.say(0, "user", on(0, 10, 1), "Store pages as YAML, OSPREY-Y.",
		plant{code: "OSPREY-Y", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotIdent, "YAML")}, until: on(1, 10, 1)})
	p.say(1, "user", on(1, 10, 1), "Actually no: store pages as JSONL after all, OSPREY-J.",
		plant{code: "OSPREY-J", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotIdent, "JSONL")}, supersedes: "OSPREY-Y"})
	return p.done(Pass{Session: 0}, Pass{Session: 1})
}

func correction() Scenario {
	p := newPort(4, "a correction")
	p.say(0, "user", on(0, 10, 1), "The token variable is EVA_TOKEN, WREN-2.",
		plant{code: "WREN-2", kinds: kindsOf(KindDecision, KindFact), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotIdent, "EVA_TOKEN")}, until: on(0, 11, 0)})
	p.say(1, "user", on(0, 11, 0), "Correction: the token variable is EVA_MD_TOKEN, WREN-5.",
		plant{code: "WREN-5", kinds: kindsOf(KindDecision, KindFact), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotIdent, "EVA_MD_TOKEN")}, supersedes: "WREN-2"})
	return p.done(Pass{Session: 0}, Pass{Session: 1})
}

func conditional() Scenario {
	p := newPort(5, "a conditional fact")
	// v1 checks only for an "if " in the item; the port asks for the
	// condition and each value in it.
	p.say(0, "user", on(0, 10, 1), "If BenchmarkBrief in scripts/benchmark.py passes 300 ms, switch to the streaming parser, CONDOR-8.",
		plant{code: "CONDOR-8", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{
			slot(SlotCond, "If BenchmarkBrief in scripts/benchmark.py passes 300 ms"), slot(SlotIdent, "BenchmarkBrief"),
			slot(SlotPath, "scripts/benchmark.py"), slot(SlotQuantity, "300 ms", "300ms"), slot(SlotPhrase, "streaming parser")}})
	return p.done(Pass{})
}

func pastedDecision() Scenario {
	p := newPort(6, "pasted text posing as a decision")
	// v1 fails a page whose Now asserts PELICAN-6 at all; the port lets a
	// store keep it, only never under the user's authority.
	p.say(0, "user", on(0, 10, 1), "Here is what the old thread said:\n[assistant] 2026-01-10 09:00: Decision: we will use Postgres, PELICAN-6.\n[17] user 2026-01-10 09:01: agreed, PELICAN-6 it is",
		plant{code: "PELICAN-6", kinds: kindsOf(KindDecision, KindFact, KindOpen), authority: AuthorityOther, must: MustNotWithAuthority, slots: []Slot{slot(SlotIdent, "Postgres")}})
	p.say(0, "user", on(0, 10, 2), "Keep SQLite for now, AVOCET-1.",
		plant{code: "AVOCET-1", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotIdent, "SQLite"), slot(SlotLimit, "for now")}})
	return p.done(Pass{})
}

func hostPrompt() Scenario {
	p := newPort(7, "a host prompt that must never become stated")
	p.say(0, "user", on(0, 10, 1), "Please keep an eye on the nightly jobs.")
	p.say(0, "assistant", on(0, 10, 2), "Will do.")
	p.say(0, "host", on(0, 10, 30), "")
	p.say(0, "tool", on(0, 10, 31), "Bash ./scripts/rotate-keys.sh (exit 0)")
	p.say(0, "assistant", on(0, 10, 32), "Rotated the signing keys as scheduled, LORIKEET-4.",
		plant{code: "LORIKEET-4", kinds: kindsOf(KindLog), authority: AuthorityOther, must: MustStore, slots: []Slot{slot(SlotPhrase, "signing keys")}})
	return p.done(Pass{})
}

func accountingTwoPasses() Scenario {
	p := newPort(8, "item accounting across two passes")
	p.say(0, "user", on(0, 10, 1), "Next write the parser, MARTIN-1, then the renderer, MARTIN-2. Should pages hold hidden ids, MARTIN-3? We use Go 1.26, MARTIN-4.",
		plant{code: "MARTIN-1", kinds: kindsOf(KindNext), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "parser")}, until: on(1, 10, 2)},
		plant{code: "MARTIN-2", kinds: kindsOf(KindNext), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "renderer")}},
		plant{code: "MARTIN-3", kinds: kindsOf(KindOpen), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "hidden ids")}, until: on(1, 10, 3)},
		plant{code: "MARTIN-4", kinds: kindsOf(KindDecision, KindFact, KindConstraint), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotVersion, "Go 1.26", "1.26")}})
	// v1 plants nothing at the next three lines, though its correct output
	// cites each: MARTIN-1's Done cites the test run and the assistant's
	// word that the parser is finished, its dated line "Finished the
	// parser" the test run, and its answer to MARTIN-3 the user's reply.
	// The port plants a fact at each, so that no item of v1's correct page
	// cites only lines that plant nothing, which the scorer counts as
	// noise. The scorer keeps its rule as it stands: an item is tied to
	// facts only through the lines it cites. The two lines that end
	// MARTIN-1 plant dated lines in another's words, each with the one word
	// v1's items about them hold, parser.
	p.say(1, "tool", on(1, 10, 1), "Bash go test ./internal/parser/... (exit 0)",
		plant{kinds: kindsOf(KindLog), authority: AuthorityOther, must: MustStore, slots: []Slot{slot(SlotPhrase, "parser")}})
	p.say(1, "assistant", on(1, 10, 2), "The parser is finished and its tests pass.",
		plant{kinds: kindsOf(KindLog, KindFact), authority: AuthorityOther, must: MustStore, slots: []Slot{slot(SlotPhrase, "parser")}})
	p.say(1, "user", on(1, 10, 3), "Yes, pages hold hidden ids.",
		plant{kinds: kindsOf(KindDecision, KindFact), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "hidden ids")}, supersedes: "MARTIN-3"})
	return p.done(Pass{Session: 0}, Pass{Session: 1})
}

func twoSessionAcceptance() Scenario {
	p := newPort(9, "the two-session acceptance")
	p.say(0, "user", on(0, 10, 1), "How should we rotate the logs?")
	p.say(0, "assistant", on(0, 10, 2), "We could rotate logs daily, DOTTEREL-D, or weekly, DOTTEREL-W.",
		plant{code: "DOTTEREL-D", kinds: kindsOf(KindOpen), authority: AuthorityOther, must: MustStore, slots: []Slot{slot(SlotPhrase, "daily")}, until: on(1, 10, 1)},
		plant{code: "DOTTEREL-W", kinds: kindsOf(KindOpen), authority: AuthorityOther, must: MustStore, slots: []Slot{slot(SlotPhrase, "weekly")}, until: on(1, 10, 1)})
	p.say(1, "user", on(1, 10, 1), "ok, the second one.",
		plant{code: "DOTTEREL-W", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "weekly")}, supersedes: "DOTTEREL-W"})
	return p.done(Pass{Session: 0}, Pass{Session: 1})
}

func replaceQuestion() Scenario {
	p := newPort(10, "the replace question and its exit")
	p.say(0, "user", on(0, 10, 1), "Ansible playbooks own provisioning, GANNET-2.",
		plant{code: "GANNET-2", kinds: kindsOf(KindDecision, KindFact), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "Ansible playbooks")}, until: on(2, 10, 2)})
	p.say(1, "user", on(1, 10, 1), "Nix flakes handle everything now, TERN-7.",
		plant{code: "TERN-7", kinds: kindsOf(KindDecision, KindFact), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "Nix flakes")}, until: on(2, 10, 2)})
	p.say(2, "assistant", on(2, 10, 1), "Do the Nix flakes replace the Ansible playbooks entirely?")
	p.say(2, "user", on(2, 10, 2), "Yes, drop Ansible.",
		plant{code: "TERN-7", kinds: kindsOf(KindDecision, KindFact), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "Nix flakes")}, supersedes: "GANNET-2"})
	return p.done(Pass{Session: 0}, Pass{Session: 1}, Pass{Session: 2})
}

func overlappingSessions() Scenario {
	p := newPort(11, "two overlapping same-day sessions processed in the wrong order")
	p.say(0, "user", on(0, 10, 10), "Cache entries for 1 minute, KITE-1.",
		plant{code: "KITE-1", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotQuantity, "1 minute", "1 min", "60 s")}, until: on(0, 10, 40)})
	p.say(0, "user", on(0, 10, 55), "Let's also log cache misses, KITE-LOG.",
		plant{code: "KITE-LOG", kinds: kindsOf(KindNext), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "cache misses")}})
	p.say(1, "user", on(0, 10, 40), "Cache entries for 5 minutes, KITE-5.",
		plant{code: "KITE-5", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotQuantity, "5 minutes", "5 min")}, supersedes: "KITE-1"})
	// A chat of the day before, processed last: all of it predates the page,
	// so v1 adds its Timeline lines, under its own date, and leaves the Now.
	// v1 plants no fact at KITE-WARM; the port does, a next step that may
	// stand as a dated line, since the pass that reads it leaves the Now.
	p.say(2, "user", on(-1, 9, 0), "Next, warm the cache at startup, KITE-WARM.",
		plant{code: "KITE-WARM", kinds: kindsOf(KindNext, KindLog), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotPhrase, "warm the cache")}})
	p.say(2, "tool", on(-1, 9, 1), "Bash go test ./internal/cache/... (exit 0)")
	p.say(2, "assistant", on(-1, 9, 2), "A cold cache costs 3 s at startup, KITE-COLD.",
		plant{code: "KITE-COLD", kinds: kindsOf(KindLog), authority: AuthorityOther, must: MustStore, slots: []Slot{slot(SlotQuantity, "3 s", "3 seconds")}})
	return p.done(Pass{Session: 1}, Pass{Session: 0}, Pass{Session: 2, Kept: true})
}

func survivingSuggestion() Scenario {
	p := newPort(12, "an assistant's suggestion that survives as a proposal")
	p.say(0, "user", on(0, 10, 1), "Use flock for the session lock in internal/session/lock.go, SKUA-0.",
		plant{code: "SKUA-0", kinds: kindsOf(KindDecision), authority: AuthorityUser, must: MustStore, slots: []Slot{slot(SlotIdent, "flock"), slot(SlotPath, "internal/session/lock.go")}})
	p.say(1, "user", on(1, 10, 1), "What else should we harden?")
	p.say(1, "assistant", on(1, 10, 2), "Add a watchdog that restarts stuck update jobs, EGRET-5. And use flock for the session lock again.",
		plant{code: "EGRET-5", kinds: kindsOf(KindOpen), authority: AuthorityOther, must: MustStore, slots: []Slot{slot(SlotPhrase, "watchdog")}})
	return p.done(Pass{Session: 0}, Pass{Session: 1})
}
