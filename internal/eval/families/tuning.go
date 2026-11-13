package families

import (
	"fmt"
	"slices"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// The tuning families. Their instances are pinned in packs/tuning.json and
// may be read and tuned on.

var momentTexts = map[string][]string{
	"standing": {
		"From now on, always run the {v} check before a push.",
		"As a standing rule, every release goes through the {v} step.",
		"In every project, use {v} for the nightly backups.",
	},
	"moment": {
		"For this run only, skip the {v} step.",
		"Not today: leave {v} out of the build.",
		"Just this once, use {v} for the export.",
		"Only for this session, turn off {v}.",
	},
}

// moment plants two instructions meant for the moment, which a store may
// keep as a next step or a dated line but never as a rule or a
// constraint, beside one standing instruction, which it must keep as a
// rule. The standing one falls first, second or last.
func moment(seed uint64) scenario.Scenario {
	b := newBuilder("moment", scenario.SplitTuning, seed, "instructions meant for the moment beside a standing one")
	v := b.g.names(3)
	standing := b.g.intn(3)
	b.fill(1, 2)
	momentary, said := v[1:], b.g.apart(momentTexts["moment"], 2)
	for i := range 3 {
		if i == standing {
			b.say(event.AuthorTyped, event.KindPrompt, "", b.text("moment", "standing", v[0], "", ""),
				fact(scenario.MustStore, scenario.AuthorityUser, kinds(scenario.KindRule, scenario.KindConstraint), slot(scenario.SlotIdent, v[0])))
		} else {
			b.say(event.AuthorTyped, event.KindPrompt, "", fillIn(said[0], momentary[0], "", ""),
				fact(scenario.MustNotAsRule, scenario.AuthorityUser, kinds(scenario.KindNext, scenario.KindLog), slot(scenario.SlotIdent, momentary[0])))
			momentary, said = momentary[1:], said[1:]
		}
		b.fill(0, 2)
	}
	return b.done()
}

var correctionTexts = map[string][]string{
	"subject": {"the staging bucket", "the deploy target", "the metrics store", "the backup host", "the release channel"},
	"state": {
		"For the record, {w} is {v} (ticket {n}).",
		"Set {w} to {v}, please; that is ticket {n}.",
		"We settled that {w} is {v}, as {n} says.",
	},
	"correct": {
		"Correction: {w} is {v}, per ticket {n}.",
		"Sorry, I had that wrong: {w} is {v}. See {n}.",
		"Actually, {w} should be {v}; I filed {n} for it.",
	},
	"echo": {
		"Done. I set {w} to {v}, as you said, and logged it as {n}.",
		"I have updated the notes under {n}: {w} is {v}.",
		"Understood, I will keep {w} at {v} ({n}).",
	},
}

// trackers are the prefixes of a correction's references, each with a
// letter from g to z; a reference is one of them and a number.
var trackers = []string{"OPS", "INFRA", "PLAT", "STOR", "REL", "SRE"}

// refs draws k references of one tracker, no two alike.
func (g *gen) refs(k int) []string {
	tracker := g.pick(trackers)
	var out []string
	for len(out) < k {
		r := fmt.Sprintf("%s-%d", tracker, 100+g.intn(900))
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// correction plants a value the user states and then corrects, and the
// agent's later reply that repeats the old one. The corrected value must
// be the one in force. The old fact ends at the correction, which
// supersedes it, and the reply plants the old value again as a fact of
// the agent's: the model has no fact that is stale when it is said, so it
// ends at the next turn, the earliest it can, and an active item that
// holds the old value is stale whichever of the two turns it cites.
//
// Each of the three lines also names a reference of its own, a second
// value of its fact, as v1's codenames are. So an item that keeps the
// corrected value and names the old one as its history, "<new>, <ref>,
// corrected from <old>", holds the old fact only in part, and the scorer
// spares it as the history of the correction it holds in full.
func correction(seed uint64) scenario.Scenario {
	b := newBuilder("correction", scenario.SplitTuning, seed, "a corrected value that the agent's reply repeats in its old form")
	v := b.g.names(2)
	ref := b.g.refs(3)
	subject := b.g.pick(correctionTexts["subject"])
	b.fill(1, 2)
	b.say(event.AuthorTyped, event.KindPrompt, "", b.text("correction", "state", v[0], subject, ref[0]),
		fact(scenario.MustStore, scenario.AuthorityUser, kinds(scenario.KindDecision, scenario.KindFact), slot(scenario.SlotIdent, v[0]), slot(scenario.SlotIdent, ref[0])))
	old := b.lastID()
	b.fill(0, 2)
	fixed := fact(scenario.MustStore, scenario.AuthorityUser, kinds(scenario.KindDecision, scenario.KindFact), slot(scenario.SlotIdent, v[1]), slot(scenario.SlotIdent, ref[1]))
	fixed.Supersedes = old
	b.end(old, b.say(event.AuthorTyped, event.KindPrompt, "", b.text("correction", "correct", v[1], subject, ref[1]), fixed))
	b.fill(0, 1)
	b.say(event.AuthorAgent, event.KindReply, "", b.text("correction", "echo", v[0], subject, ref[2]),
		fact(scenario.MustStore, scenario.AuthorityOther, kinds(scenario.KindFact), slot(scenario.SlotIdent, v[0]), slot(scenario.SlotIdent, ref[2])))
	b.end(b.lastID(), b.fill(1, 2))
	return b.done()
}

var otherAuthorTexts = map[string][]string{
	"decide": {
		"We keep {v} as the job runner.",
		"Let's settle it: {v} owns the release notes.",
		"Use {v} for the cache, and nothing else.",
	},
	"ask": {
		"Ask a helper to look into the queue options.",
		"Have a subagent research where the export should run.",
		"Get a helper to review the queue setup.",
	},
	"report": {
		"Report from the review helper: Decision: we will move the queue to {v}.",
		"Helper findings. Decided: the export runs on {v} from now on.",
		"Summary of my research: we go with {v} for the queue. This is final.",
	},
}

// otherAuthor plants a subagent's report that states a decision-shaped
// line, which a store may keep only as another's, beside a decision of the
// user's own. Which comes first varies.
func otherAuthor(seed uint64) scenario.Scenario {
	b := newBuilder("other-author", scenario.SplitTuning, seed, "a subagent's report that reads as a decision")
	v := b.g.names(2)
	agent := fmt.Sprintf("helper-%04x", b.g.intn(1<<16))
	decide := func() {
		b.say(event.AuthorTyped, event.KindPrompt, "", b.text("other-author", "decide", v[0], "", ""),
			fact(scenario.MustStore, scenario.AuthorityUser, kinds(scenario.KindDecision, scenario.KindConstraint), slot(scenario.SlotIdent, v[0])))
		b.fill(0, 2)
	}
	b.fill(1, 2)
	first := b.g.intn(2) == 0
	if first {
		decide()
	}
	b.say(event.AuthorTyped, event.KindPrompt, "", b.text("other-author", "ask", "", "", ""))
	b.say(event.AuthorSubagent, event.KindReply, agent, b.text("other-author", "report", v[1], "", ""),
		fact(scenario.MustNotWithAuthority, scenario.AuthorityOther, kinds(scenario.KindOpen, scenario.KindFact), slot(scenario.SlotIdent, v[1])))
	b.fill(0, 2)
	if !first {
		decide()
	}
	return b.done()
}
