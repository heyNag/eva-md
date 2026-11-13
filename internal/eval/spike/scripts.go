// Package spike is the contract spike of the v2 plan (DV-03): a throwaway
// prototype of the extraction contract, run black-box on made-up scripts
// to settle its open bets by measurement. It holds the scripts, among them
// the family that plants typed refusals, hedges and assents to the agent's
// proposals; the renderer that shows a pass to the model in either form of
// evidence; and the contract's prompt, schemas, refusal codes and strict
// reading of an answer. No release contains it, and it is deleted when
// DV-21 lands. SPEC.md states its contract.
package spike

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// How a typed prompt answers a proposal.
const (
	ModeRefusal = "refusal"
	ModeHedge   = "hedge"
	ModeAssent  = "assent"
)

var modes = []string{ModeRefusal, ModeHedge, ModeAssent}

// Script is one script of the spike: a scenario, and the answers to the
// agent's proposals that it plants.
type Script struct {
	Scenario scenario.Scenario `json:"scenario"`
	Plants   []Plant           `json:"plants,omitempty"`
}

// Plant says that the typed prompt at Seq of Session answers, by Mode, the
// proposal of fact Fact that the agent's reply at Proposal plants.
type Plant struct {
	Session  string `json:"session"`
	Seq      int64  `json:"seq"`
	Mode     string `json:"mode"`
	Proposal int64  `json:"proposal"`
	Fact     string `json:"fact"`
}

// Scripts are the spike's scripts in a fixed order: v1's 12, the family
// assent, then bounds.01.
func Scripts() []Script {
	var out []Script
	for _, sc := range scenario.V1() {
		out = append(out, Script{Scenario: sc})
	}
	out = append(out, assentFamily()...)
	return append(out, bounds())
}

// Validate reports the first way s breaks the scenario model or its plants.
func (s Script) Validate() error {
	sc := s.Scenario
	if err := sc.Validate(); err != nil {
		return err
	}
	answered := map[string]bool{}
	for i, p := range s.Plants {
		if err := s.checkPlant(p, answered); err != nil {
			return fmt.Errorf("script %s: plant %d: %w", sc.ID, i+1, err)
		}
	}
	return nil
}

// checkPlant reports what is wrong with p; answered holds the facts the
// plants before it answer.
func (s Script) checkPlant(p Plant, answered map[string]bool) error {
	i := slices.IndexFunc(s.Scenario.Sessions, func(x scenario.Session) bool { return x.ID == p.Session })
	if i < 0 {
		return errors.New("no such session")
	}
	turns := s.Scenario.Sessions[i].Turns
	switch {
	case !slices.Contains(modes, p.Mode):
		return fmt.Errorf("unknown mode %q", p.Mode)
	case p.Proposal < 1 || p.Seq <= p.Proposal || p.Seq > int64(len(turns)):
		return errors.New("the answer must come after the proposal, in the session")
	}
	proposal, answer := turns[p.Proposal-1], turns[p.Seq-1]
	switch {
	case proposal.Author != event.AuthorAgent || proposal.Kind != event.KindReply:
		return errors.New("the proposal is not the agent's reply")
	case answer.Author != event.AuthorTyped || answer.Kind != event.KindPrompt:
		return errors.New("the answer is not a typed prompt")
	case slices.ContainsFunc(turns[p.Proposal:p.Seq-1], typedPrompt):
		return errors.New("the answer is not the first typed prompt after the proposal")
	case !slices.Contains(proposal.Plants, p.Fact):
		return fmt.Errorf("the proposal plants no fact %s", p.Fact)
	case answered[p.Fact]:
		return fmt.Errorf("fact %s is answered twice", p.Fact)
	}
	answered[p.Fact] = true
	f := s.Scenario.Facts[slices.IndexFunc(s.Scenario.Facts, func(f scenario.Fact) bool { return f.ID == p.Fact })]
	if p.Mode == ModeAssent {
		if f.Must != scenario.MustStore || f.Authority != scenario.AuthorityUser || !slices.ContainsFunc(f.Kinds, userKind) ||
			slices.ContainsFunc(f.Kinds, func(k scenario.Kind) bool { return !userKind(k) }) {
			return fmt.Errorf("fact %s is assented to, so it is stored under the user's authority as a decision or a procedure", f.ID)
		}
		return nil
	}
	if f.Must != scenario.MustNotWithAuthority {
		return fmt.Errorf("fact %s is answered by a %s, so it must not carry the user's authority", f.ID, p.Mode)
	}
	for _, sl := range f.Slots {
		for _, v := range append([]string{sl.Value}, sl.Forms...) {
			if k := letters(v); k != "" && strings.Contains(letters(answer.Text), k) {
				return fmt.Errorf("the %s repeats the value it answers", p.Mode)
			}
		}
	}
	return nil
}

func typedPrompt(t scenario.Turn) bool {
	return t.Author == event.AuthorTyped && t.Kind == event.KindPrompt
}

// userKind reports a kind an assent may store under the user's authority.
func userKind(k scenario.Kind) bool { return k == scenario.KindDecision || k == scenario.KindProcedure }

// letters is s's letters and digits in lower case, as results.Forbidden
// matches a planted word.
func letters(s string) string {
	return strings.Map(func(r rune) rune {
		if r = unicode.ToLower(r); unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// The family assent's wordings. A refusal or a hedge never repeats the
// value it answers; each is said by the user, typed.
var (
	refusals = []string{
		"no",
		"Nope.",
		"no thanks, leave it as it is",
		"Yes, I see why, but no: keep what we have.",
		"yeah, I get the idea, but let's not do that",
		"Please don't. That would break the nightly job.",
		"I'd rather we didn't change that.",
		"Not this one, we tried something like it before and reverted it.",
	}
	hedges = []string{
		"maybe",
		"Let me think about it.",
		"not sure yet, ask me again tomorrow",
		"Hmm, possibly. Park it for now.",
		"Could be worth it later, not now.",
		"I'll check with the others first.",
		"Perhaps. What would it cost us?",
		"Good question; let's decide after the release.",
	}
	assents = []string{"yes", "Yes, do that.", "ok, go ahead", "sounds good, please do", "sure"}
	// splits answer a reply that proposes two things: the modes are of
	// the first and the second.
	splits = []struct {
		text  string
		modes [2]string
	}{
		{"Yes to the first one. Leave the second alone.", [2]string{ModeAssent, ModeRefusal}},
		{"Do the second, not the first.", [2]string{ModeRefusal, ModeAssent}},
		{"ok for the first part, but no to the other.", [2]string{ModeAssent, ModeRefusal}},
		{"The first, no. The second, yes.", [2]string{ModeRefusal, ModeAssent}},
	}
)

// proposal is one reply of the agent's that proposes one thing, or two:
// what it says, and the made-up value and kind of each thing, which the
// text holds.
type proposal struct {
	say    string
	values []string
	kinds  []scenario.Kind
}

func decision(say, value string) proposal {
	return proposal{say: say, values: []string{value}, kinds: []scenario.Kind{scenario.KindDecision}}
}

func procedure(say, value string) proposal {
	return proposal{say: say, values: []string{value}, kinds: []scenario.Kind{scenario.KindProcedure}}
}

func both(say, first, second string) proposal {
	return proposal{say: say, values: []string{first, second}, kinds: []scenario.Kind{scenario.KindDecision, scenario.KindDecision}}
}

// The order of answers in a script of ten single proposals, and in one of
// nine whose third proposes two things: r a refusal, h a hedge, a an
// assent, s a split.
const (
	tenSingles = "rhahrhrahr"
	withSplit  = "hrshrahrh"
)

// assentTopic is one script of the family: what the chat is about, and its
// proposals in the order of its pattern.
type assentTopic struct {
	topic     string
	pattern   string
	proposals []proposal
}

var assentTopics = []assentTopic{
	{"the importer", tenSingles, []proposal{
		decision("I could move the importer to the quillstream parser. Shall I?", "quillstream"),
		decision("Should the importer write its checkpoints to the marrow_ckpt table instead of files?", "marrow_ckpt"),
		procedure("Shall I add a dry run, importer verify --strict-rows, before every import?", "--strict-rows"),
		decision("I suggest we rename the importer's queue to harvest-intake. OK?", "harvest-intake"),
		decision("We could throttle the importer with the slowtide profile. Want that?", "slowtide"),
		decision("Should the importer skip the rows the orphan-sweep check flags?", "orphan-sweep"),
		procedure("Shall I run make vendor-snap before each importer release?", "vendor-snap"),
		decision("How about logging every rejected row to rejects.jsonl in the run folder?", "rejects.jsonl"),
		decision("I could switch the importer's date parsing to lenient-iso mode. Shall I?", "lenient-iso"),
		decision("Should we drop the old csvmux reader from the importer?", "csvmux"),
	}},
	{"the CI pipeline", withSplit, []proposal{
		decision("Shall I move the CI cache to the gravelstore bucket?", "gravelstore"),
		decision("I could split the test job into shards with the --shard-by=pkgweight flag. Shall I?", "--shard-by=pkgweight"),
		both("Two things: I can pin the runner image to tidewater-22, and I can also turn on the flakehunter retry plugin. Shall I do both?", "tidewater-22", "flakehunter"),
		decision("Should CI post its results to the build-lounge channel?", "build-lounge"),
		decision("Shall I make the lint job blocking with the strictlint preset?", "strictlint"),
		procedure("Before each merge, shall I run scripts/smoke-ladder.sh as a required step?", "scripts/smoke-ladder.sh"),
		decision("I could cap CI jobs at the quietlane concurrency group. Want that?", "quietlane"),
		decision("Should we replace the nightly build with the moonwatch schedule?", "moonwatch"),
		decision("How about caching Go modules behind the vault-mirror proxy?", "vault-mirror"),
	}},
	{"the search index", withSplit, []proposal{
		decision("Shall I rebuild the search index with the burrowtrie layout?", "burrowtrie"),
		decision("I could store index segments under the lichen-segs folder. Shall I?", "lichen-segs"),
		both("I can raise the merge fan-in with the fanmerge-lite policy, and I can also drop stop words with the hushlist filter. Which should I do?", "fanmerge-lite", "hushlist"),
		decision("Should queries fall back to slowpath-scan when the index is stale?", "slowpath-scan"),
		decision("Shall I compress posting lists with the zigpack codec?", "zigpack"),
		procedure("Shall I run index fsck --deep-walk after every crash?", "--deep-walk"),
		decision("How about warming the index from the hotkeys-ring at startup?", "hotkeys-ring"),
		decision("Should we rank results with the mossrank scorer?", "mossrank"),
		decision("I could shard the index by project with the parcel-split scheme. Shall I?", "parcel-split"),
	}},
	{"the deploy scripts", tenSingles, []proposal{
		decision("Shall I move the deploy scripts to the rookdeploy runner?", "rookdeploy"),
		decision("I could add a canary stage named lantern-canary before production. Shall I?", "lantern-canary"),
		procedure("Shall I run deploy preflight --with-rollback-plan before every release?", "--with-rollback-plan"),
		decision("Should failed deploys page the owl-rota schedule?", "owl-rota"),
		decision("I suggest we keep release bundles in the stonecellar store. OK?", "stonecellar"),
		decision("How about a freeze window called quiet-harbor on Fridays?", "quiet-harbor"),
		decision("Should deploys verify signatures with the sigwarden tool?", "sigwarden"),
		decision("Shall I tag every deploy with the build's mintstamp?", "mintstamp"),
		decision("I could roll back on its own with the undertow policy. Want that?", "undertow"),
		decision("Should we retire the old ship-it.sh script?", "ship-it.sh"),
	}},
	{"the log pipeline", withSplit, []proposal{
		decision("Shall I route the app logs through the brooklet shipper?", "brooklet"),
		decision("I could keep older logs in the tundra-cold tier. Shall I?", "tundra-cold"),
		both("I can add request ids with the threadmark middleware, and I can also sample debug logs with the sievelog filter. Shall I do both?", "threadmark", "sievelog"),
		decision("Should errors go to the redflag-stream as well?", "redflag-stream"),
		decision("Shall I switch the log format to the plume-json layout?", "plume-json"),
		procedure("Shall I run logcheck --rotate-dry before each rotation?", "--rotate-dry"),
		decision("How about an alert when the logs-lag gauge climbs?", "logs-lag"),
		decision("Should we drop the syslog bridge for the wrenrelay?", "wrenrelay"),
		decision("I could index logs with the fernsearch backend. Shall I?", "fernsearch"),
	}},
	{"the billing export", withSplit, []proposal{
		decision("Shall I move the billing export to the ledgerline job?", "ledgerline"),
		decision("I could write the export as parquet through the quarrywriter. Shall I?", "quarrywriter"),
		both("I can round amounts with the half-even-plus rule, and I can also send the export to the tallyhouse mailbox. Shall I?", "half-even-plus", "tallyhouse"),
		decision("Should the export run in the owlshift window at night?", "owlshift"),
		decision("Shall I stamp each export with the inkseal mark?", "inkseal"),
		procedure("Before each month's close, shall I run export check --totals-only?", "--totals-only"),
		decision("How about keeping a copy in the vellum-archive?", "vellum-archive"),
		decision("Should we drop the old xlsx-legacy format?", "xlsx-legacy"),
		decision("I could split the export by region with the mapcut tool. Shall I?", "mapcut"),
	}},
}

// assentFamily builds the family assent, assent.01 to assent.06: in each,
// one typed chat where the agent proposes changes to a topic one reply at
// a time and the user answers each, as the topic's pattern says, in the
// next typed prompt. The wordings are taken in turn across the family.
func assentFamily() []Script {
	var r, h, a, sp int
	var out []Script
	for n, tp := range assentTopics {
		if len(tp.pattern) != len(tp.proposals) {
			panic("spike: a topic's pattern does not fit its proposals")
		}
		host := adapter.All()[n%len(adapter.All())].ID
		sid := fmt.Sprintf("%08x-3333-4000-8000-%012d", 0x5a110000+n+1, n+1)
		start := time.Date(2026, 3, 2+n, 14, 0, 0, 0, time.FixedZone("", 3600))
		s := scenario.Session{Host: string(host), ID: sid, Branch: "main"}
		say := func(author event.Author, kind event.Kind, text string, plants ...string) int64 {
			s.Turns = append(s.Turns, scenario.Turn{At: start.Add(time.Duration(len(s.Turns)) * time.Minute), Author: author, Kind: kind, Text: text, Plants: plants})
			return int64(len(s.Turns))
		}
		sc := scenario.Scenario{ID: fmt.Sprintf("assent.%02d", n+1), Family: "assent", Name: "typed answers to proposals about " + tp.topic, Split: scenario.SplitTuning}
		var plants []Plant
		say(event.AuthorTyped, event.KindPrompt, "Let's go over "+tp.topic+" today. Propose changes one at a time and I'll answer each.")
		for i, p := range tp.proposals {
			var ids []string
			for j, v := range p.values {
				ids = append(ids, fmt.Sprintf("f%d", len(sc.Facts)+1))
				sc.Facts = append(sc.Facts, scenario.Fact{ID: ids[j], Slots: []scenario.Slot{{Class: scenario.SlotIdent, Value: v}}})
			}
			at := say(event.AuthorAgent, event.KindReply, p.say, ids...)
			var text string
			var ms []string
			switch tp.pattern[i] {
			case 'r':
				text, ms = refusals[r%len(refusals)], []string{ModeRefusal}
				r++
			case 'h':
				text, ms = hedges[h%len(hedges)], []string{ModeHedge}
				h++
			case 'a':
				text, ms = assents[a%len(assents)], []string{ModeAssent}
				a++
			case 's':
				text, ms = splits[sp].text, splits[sp].modes[:]
				sp++
			}
			if len(ms) != len(ids) {
				panic("spike: an answer does not fit its proposal")
			}
			seq := say(event.AuthorTyped, event.KindPrompt, text)
			for j, id := range ids {
				f := &sc.Facts[len(sc.Facts)-len(ids)+j]
				if ms[j] == ModeAssent {
					f.Kinds, f.Authority, f.Must = []scenario.Kind{p.kinds[j]}, scenario.AuthorityUser, scenario.MustStore
				} else {
					f.Kinds, f.Authority, f.Must = []scenario.Kind{scenario.KindOpen, p.kinds[j]}, scenario.AuthorityOther, scenario.MustNotWithAuthority
				}
				plants = append(plants, Plant{Session: sid, Seq: seq, Mode: ms[j], Proposal: at, Fact: id})
			}
		}
		sc.Sessions = []scenario.Session{s}
		sc.Passes = []scenario.Pass{{Session: 0}}
		out = append(out, Script{Scenario: sc, Plants: plants})
	}
	return out
}

// setting is one value of bounds.01: its key, and its value as said.
type setting struct {
	key, value string
	class      scenario.SlotClass
}

var relaySettings = []setting{
	{"relay_dial_timeout", "450 ms", scenario.SlotQuantity}, {"relay_idle_timeout", "90 s", scenario.SlotQuantity},
	{"relay_max_frames", "4096", scenario.SlotInt}, {"relay_backlog", "128", scenario.SlotInt},
	{"relay_retry_limit", "7", scenario.SlotInt}, {"relay_buffer_kib", "640", scenario.SlotInt},
	{"relay_heartbeat", "15 s", scenario.SlotQuantity}, {"relay_shutdown_grace", "30 s", scenario.SlotQuantity},
	{"relay_peer_cap", "48", scenario.SlotInt}, {"relay_route_cache", "2000", scenario.SlotInt},
	{"relay_drain_batch", "256", scenario.SlotInt}, {"relay_window", "64", scenario.SlotInt},
	{"relay_log_sample", "25", scenario.SlotInt},
}

var schedulerSettings = []setting{
	{"sched_tick", "250 ms", scenario.SlotQuantity}, {"sched_queue_depth", "512", scenario.SlotInt},
	{"sched_lease", "45 s", scenario.SlotQuantity}, {"sched_workers", "12", scenario.SlotInt},
	{"sched_retry_backoff", "800 ms", scenario.SlotQuantity}, {"sched_stale_after", "6 h", scenario.SlotQuantity},
	{"sched_batch", "96", scenario.SlotInt}, {"sched_jitter", "120 ms", scenario.SlotQuantity},
	{"sched_history", "14 days", scenario.SlotQuantity}, {"sched_priority_levels", "5", scenario.SlotInt},
	{"sched_spill_mib", "300", scenario.SlotInt}, {"sched_lock_ttl", "20 s", scenario.SlotQuantity},
	{"sched_max_inflight", "40", scenario.SlotInt},
}

// How bounds.01 states a setting, and how the agent acknowledges one; each
// is taken in turn. An acknowledgement holds no value.
var (
	settingSays = []string{"Set %s to %s.", "%s should be %s.", "Keep %s at %s.", "We agreed that %s is %s."}
	settingAcks = []string{"Noted.", "Got it.", "Understood, noted.", "Recorded."}
)

// bounds builds bounds.01: one chat in which the user states 26 settings,
// relay and scheduler settings in turn, each in a typed prompt of its own
// that the agent acknowledges, and each a fact to store. So an answer that
// keeps each in an operation of its own needs more operations than the
// schema allows (F-064), and no entry, quote or segment holds two
// settings for an operation to keep at once.
func bounds() Script {
	sid := fmt.Sprintf("%08x-4444-4000-8000-%012d", 0xb0d50001, 1)
	start := time.Date(2026, 3, 10, 9, 0, 0, 0, time.FixedZone("", 3600))
	sc := scenario.Scenario{ID: "bounds.01", Family: "bounds", Name: "more settings stated in one chat than an answer may hold", Split: scenario.SplitTuning}
	s := scenario.Session{Host: string(adapter.All()[0].ID), ID: sid, Branch: "main"}
	say := func(author event.Author, kind event.Kind, text string, plants ...string) {
		s.Turns = append(s.Turns, scenario.Turn{At: start.Add(time.Duration(len(s.Turns)) * time.Minute), Author: author, Kind: kind, Text: text, Plants: plants})
	}
	say(event.AuthorTyped, event.KindPrompt, "I'll give you the relay and scheduler settings we agreed, one at a time. Keep every one of them.")
	say(event.AuthorAgent, event.KindReply, "Ready.")
	if len(relaySettings) != len(schedulerSettings) {
		panic("spike: bounds.01's two lists of settings differ in length")
	}
	n := 0
	for i := range relaySettings {
		for _, st := range []setting{relaySettings[i], schedulerSettings[i]} {
			id := fmt.Sprintf("f%d", len(sc.Facts)+1)
			sc.Facts = append(sc.Facts, scenario.Fact{ID: id,
				Kinds:     []scenario.Kind{scenario.KindDecision, scenario.KindConstraint, scenario.KindFact},
				Authority: scenario.AuthorityUser, Must: scenario.MustStore,
				Slots: []scenario.Slot{{Class: scenario.SlotIdent, Value: st.key}, {Class: st.class, Value: st.value}}})
			say(event.AuthorTyped, event.KindPrompt, fmt.Sprintf(settingSays[n%len(settingSays)], st.key, st.value), id)
			say(event.AuthorAgent, event.KindReply, settingAcks[n%len(settingAcks)])
			n++
		}
	}
	sc.Sessions = []scenario.Session{s}
	sc.Passes = []scenario.Pass{{Session: 0}}
	return Script{Scenario: sc}
}
