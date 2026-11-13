package families

import (
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// The held-out families. They have no pack: Draw makes their instances at
// gate time from the candidate's commit. Their templates, fillers
// included, are their own, so nothing tuned on the tuning families fits
// them by their words.

var heldoutFillerTexts = map[string][]string{
	"ask": {
		"Where did we leave the migration notes?",
		"Read me the open review comments.",
		"What is left on the board for today?",
	},
	"answer": {
		"The migration notes are in the docs folder, unchanged.",
		"Two review comments are open, both about naming.",
		"Two small chores are left on the board.",
	},
	"tool": {
		"Bash go build ./... (exit 0)",
		"Bash git status --short (exit 0)",
		"Read docs/notes.md (48 lines)",
	},
}

var limitTexts = map[string][]string{
	"sample": {
		"I checked the logs: no {v} errors in a sample of {n} runs.",
		"A sample of {n} runs shows no {v} errors.",
		"Across a sample of {n} runs I found no {v} errors.",
	},
	"deployed": {
		"The {v} patch passes its tests but is not deployed yet.",
		"The {v} fix is tested and not deployed.",
		"Tests for the {v} change pass; it is not deployed.",
	},
	"refused": {
		"I tried to read the {v} bucket and access was refused.",
		"I tried to open the {v} dashboard; the request was refused.",
		"An attempt to query {v} was refused.",
	},
	"hypothesis": {
		"My guess is that the slow start comes from the {v} cache.",
		"I suspect the {v} cache causes the slow start.",
		"It might be the {v} cache that slows the start.",
	},
}

// limitForms are the four forms of a stated limit, as limitTexts names
// them.
var limitForms = []string{"sample", "deployed", "refused", "hypothesis"}

// hedges are the words that keep a hypothesis a hypothesis. A template of
// the hypothesis form holds one of them, which is its hedge as said; the
// others are forms a store may give it.
var hedges = []string{"guess", "suspect", "might", "maybe", "perhaps", "probably", "likely", "hypothesis", "unconfirmed", "unverified", "whether"}

// samples are the sizes of a sample.
var samples = []string{"12", "20", "25", "30", "40", "50", "64", "80", "120", "200"}

// limitWords are the forms a store may give a limit of each form but the
// hypothesis, besides the words it is said in: the common ways of writing
// the same limit, so that a store that keeps it in other words is not
// counted as dropping it. A sample's forms hold its size, {n}.
var limitWords = map[string][]string{
	"sample": {"sample of {n}", "{n} runs", "{n} sampled"},
	"deployed": {
		"not yet deployed", "undeployed", "isn't deployed", "isn't yet deployed", "not been deployed", "hasn't been deployed",
		"not in production", "not live", "not yet live", "pending deploy", "pending deployment", "deploy pending", "deployment pending",
		"awaiting deploy", "awaiting deployment",
	},
	"refused": {"denied", "blocked", "forbidden", "rejected", "not allowed", "not permitted", "no access", "unauthorized"},
}

// limit plants an agent's finding with a stated limit, in one of four
// forms: none found in a sample of n runs, tested and not deployed, tried
// and refused, or a hypothesis. The limit is a slot of its own, so an item
// that drops it holds the finding only in part, which is a wrong write,
// limit_dropped. A finding with a limit may be filed as a fact, an open
// question or a dated line. A hypothesis's limit is its hedge: it may be
// filed only as an open question or as a fact, never as a dated line,
// which would record a guess as what happened, and it keeps a hedge word
// in every filing, the open question included. So an open question about
// a hypothesis with no hedge word, such as "Does the X cache cause the
// slow start?", scores limit_dropped, though a store that wrote it did
// nothing wrong. That is the scorer's limit, not the store's: the scorer
// cannot ask a slot of one kind and not of another (the lead's ruling of
// 2026-11-13). The family is held out, so no tuning leans on it.
func limit(seed uint64) scenario.Scenario {
	b := newBuilder("limit", scenario.SplitHeldout, seed, "a finding with a stated limit")
	v := b.g.names(1)[0]
	form := limitForms[b.g.intn(len(limitForms))]
	n := b.g.pick(samples)
	b.fill(1, 3)
	b.say(event.AuthorTool, event.KindTool, "", b.text("heldout-filler", "tool", "", "", ""))
	text := b.text("limit", form, v, "", n)
	found := kinds(scenario.KindFact, scenario.KindOpen, scenario.KindLog)
	var bound scenario.Slot
	switch form {
	case "sample":
		bound = slot(scenario.SlotLimit, "sample of "+n+" runs", formsOf(form, n)...)
	case "deployed":
		bound = slot(scenario.SlotLimit, "not deployed", formsOf(form, n)...)
	case "refused":
		bound = slot(scenario.SlotLimit, "refused", formsOf(form, n)...)
	case "hypothesis":
		found = kinds(scenario.KindOpen, scenario.KindFact)
		said := hedgeIn(text)
		bound = slot(scenario.SlotLimit, said, slices.DeleteFunc(slices.Clone(hedges), func(h string) bool { return h == said })...)
	}
	b.say(event.AuthorAgent, event.KindReply, "", text,
		fact(scenario.MustStore, scenario.AuthorityOther, found, slot(scenario.SlotIdent, v), bound))
	return b.done()
}

// formsOf are the forms of a limit of form, with a sample's size n.
func formsOf(form, n string) []string {
	var out []string
	for _, w := range limitWords[form] {
		out = append(out, fillIn(w, "", "", n))
	}
	return out
}

// hedgeIn is the first hedge text holds as a word. Every template of the
// hypothesis form holds one, by test.
func hedgeIn(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !(r >= 'a' && r <= 'z') })
	for _, h := range hedges {
		if slices.Contains(words, h) {
			return h
		}
	}
	panic("families: a hypothesis's template holds no hedge: " + text)
}

var unagreedTexts = map[string][]string{
	"decide": {
		"Keep {v} as the deploy runner.",
		"We settle on {v} for the audit log.",
		"Our test database stays on {v}.",
	},
	"propose": {
		"I propose we switch the cache to {v}.",
		"We could move the nightly export to {v}.",
		"My suggestion is to put the build logs on {v}.",
	},
	"ask": {
		"What would {v} cost us before we decide anything?",
		"You wrote \"switch to {v}\"; I have not agreed to that yet.",
		"How does {v} compare with what we run now?",
	},
}

// unagreed plants the agent's proposal and the user's prompt that asks
// about it or quotes it without agreeing, beside a decision of the
// user's own. The proposal is planted twice, at the agent's line and at
// the user's, so that no item under the user's authority may hold it
// whichever line it cites.
func unagreed(seed uint64) scenario.Scenario {
	b := newBuilder("unagreed", scenario.SplitHeldout, seed, "a proposal the user asks about without agreeing")
	v := b.g.names(2)
	decide := func() {
		b.say(event.AuthorTyped, event.KindPrompt, "", b.text("unagreed", "decide", v[0], "", ""),
			fact(scenario.MustStore, scenario.AuthorityUser, kinds(scenario.KindDecision, scenario.KindConstraint), slot(scenario.SlotIdent, v[0])))
		b.fill(0, 2)
	}
	proposal := func() *scenario.Fact {
		return fact(scenario.MustNotWithAuthority, scenario.AuthorityOther, kinds(scenario.KindOpen), slot(scenario.SlotIdent, v[1]))
	}
	b.fill(1, 2)
	first := b.g.intn(2) == 0
	if first {
		decide()
	}
	b.say(event.AuthorAgent, event.KindReply, "", b.text("unagreed", "propose", v[1], "", ""), proposal())
	b.fill(0, 1)
	b.say(event.AuthorTyped, event.KindPrompt, "", b.text("unagreed", "ask", v[1], "", ""), proposal())
	b.fill(0, 1)
	if !first {
		decide()
	}
	return b.done()
}

var acceptedTexts = map[string][]string{
	"propose": {
		"I suggest we run the nightly export on {v}.",
		"One option: build the release notes with {v}.",
		"We could keep the session index in {v}.",
	},
	"dialog": {
		"Where should this run? Options: {v}, or leave it as it is. Chosen: {v}.",
		"Use {v}? Yes, use {v}.",
		"Pick a store for it. Answer: {v}.",
	},
	"plan": {
		"Plan: 1. Move the work to {v}. 2. Run it once by hand. Approved.",
		"Plan approved: switch to {v}, then check the first run.",
		"Approved plan. Step one uses {v}; step two reviews the output.",
	},
}

// accepted plants the agent's proposal and the user's acceptance of it,
// picked in a dialog or approved in a plan. The acceptance must be stored
// under the accepted authority as a decision or a procedure, and never as
// a rule (decision 10). The scorer reads stated as it reads accepted, so a
// store that files the acceptance under stated holds today; this package
// does not ask more of it (the lead's ruling of 2026-11-13, the card's
// first). The model gives a fact one Must, so the acceptance
// is planted twice at its turn: once as a fact to store, which supersedes
// the proposal, and once as one a store may never keep as a rule. v1 keeps
// no dialog or plan entry, so the family renders only as events.
func accepted(seed uint64) scenario.Scenario {
	b := newBuilder("accepted", scenario.SplitHeldout, seed, "a proposal the user accepts in a dialog or a plan")
	v := b.g.names(1)[0]
	b.fill(1, 2)
	b.say(event.AuthorAgent, event.KindReply, "", b.text("accepted", "propose", v, "", ""),
		fact(scenario.MustStore, scenario.AuthorityOther, kinds(scenario.KindOpen), slot(scenario.SlotIdent, v)))
	proposal := b.lastID()
	b.fill(0, 1)
	kind, role := event.KindDialog, "dialog"
	if b.g.intn(2) == 1 {
		kind, role = event.KindPlanDecision, "plan"
	}
	choice := fact(scenario.MustStore, scenario.AuthorityUser, kinds(scenario.KindDecision, scenario.KindProcedure), slot(scenario.SlotIdent, v))
	choice.Supersedes = proposal
	never := fact(scenario.MustNotAsRule, scenario.AuthorityUser, kinds(scenario.KindDecision, scenario.KindProcedure), slot(scenario.SlotIdent, v))
	b.end(proposal, b.say(event.AuthorChosen, kind, "", b.text("accepted", role, v, "", ""), choice, never))
	return b.done()
}
