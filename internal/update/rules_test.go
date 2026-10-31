package update

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// ruleChat states a rule for this project, then one for all projects.
func ruleChat(session string) *chat {
	c := newChat(session, start)
	c.user("From now on, never mock the database in tests. " + filler)
	c.assistant("Understood. " + filler)
	c.user("In all my projects, always run go vet before committing.")
	c.assistant("Will do. " + filler)
	return c
}

// A pass writes the rules it validates: the project's to its rules page,
// the one for all projects to the general area's, each linked from its
// overview and listed in its index, so okf finds no orphan. A dropped rule
// moves to the retired rules and leaves a Timeline line. A locked rules
// page is left as it is.
func TestPassWritesTheRulesPages(t *testing.T) {
	e := newEnv(t)
	rules := []ruleOp{ruleAdd("Never mock the database in tests", 1), ruleAdd("Always run go vet before committing", 3)}
	e.fake.Respond = func(r Request) (Result, error) {
		o := keeps(r)
		o.Rules = rules
		return Result{Output: encode(t, o), Attempts: 1}, nil
	}
	c := ruleChat("3f2a91c0-rule")
	e.write(c)
	s := e.run(c)
	if outcomes(s) != "ok" || s.Records[0].Pages != 2 || s.Records[0].Validation.Rules != (RuleCounts{Added: 2, General: 1}) {
		t.Fatalf("outcomes %q, record %+v\n%s", outcomes(s), s.Records[0], e.log)
	}
	if !strings.Contains(string(e.fake.Requests()[0].Input), "Standing rules on file, by id: none.\n") {
		t.Fatal("the input doesn't list the rules on file")
	}
	for id, want := range map[string]string{
		"projects/eva-md/rules": "\n# eva-md · rules\n\n## Rules\n- Never mock the database in tests [stated] s:3f2a91c0#1 <!-- i:r1 t:2026-01-14T10:01:00+01:00 -->\n",
		"general/rules":         "\n# General · rules\n\n## Rules\n- Always run go vet before committing [stated] s:3f2a91c0#3 <!-- i:g1 t:2026-01-14T10:03:00+01:00 -->\n",
	} {
		pg, err := e.wiki().Read(id)
		if err != nil || pg.Concept.Body != want || pg.Concept.Type != "rules" || pg.Concept.Extra["locked"] != false {
			t.Fatalf("%s: %v\n%q", id, err, pg.Concept.Body)
		}
	}
	for rel, want := range map[string]string{
		"projects/eva-md/now.md":   "## Rules\n\n* [Standing rules](rules.md)\n",
		"general/now.md":           "## Rules\n\n* [Standing rules](rules.md)\n",
		"projects/eva-md/index.md": "* [eva-md · rules](rules.md) - Standing rules for github.com/you/eva-md\n",
		"general/index.md":         "* [General · rules](rules.md) - Standing rules for all of the user's projects\n",
	} {
		data, err := os.ReadFile(filepath.Join(e.cfg.WikiPath, rel))
		if err != nil || !strings.Contains(string(data), want) {
			t.Errorf("%s lacks %q:\n%s", rel, want, data)
		}
	}
	v, err := e.wiki().Validate()
	if err != nil || len(v.Errors)+len(v.Orphans)+len(v.BrokenLinks)+len(v.Warnings) != 0 {
		t.Fatalf("okf validate: %v %+v", err, v)
	}
	// The user lifts a rule: it moves to the retired rules, and the thread
	// page's Timeline records the drop.
	rules = []ruleOp{{ID: ptr("r1"), Action: "drop", Tag: "stated", Refs: []int{1}, Reason: ptr("the user allows mocks again")}}
	c.user("You can mock the database in tests again. " + filler)
	c.assistant("Fine. " + filler)
	e.write(c)
	if s = e.run(c); outcomes(s) != "ok" || s.Records[0].Validation.Rules != (RuleCounts{Dropped: 1}) {
		t.Fatalf("outcomes %q, record %+v\n%s", outcomes(s), s.Records[0], e.log)
	}
	retired, _ := e.wiki().Read("projects/eva-md/rules")
	thread, _ := e.page(testPage)
	if want := "\n## Rules\n\n## Retired rules\n- ~~Never mock the database in tests~~ (dropped 2026-01-14: the user allows mocks again) [stated] s:3f2a91c0#1 s:3f2a91c0#5 <!-- i:r1 t:2026-01-14T10:01:00+01:00 -->\n"; !strings.HasSuffix(retired.Concept.Body, want) ||
		!strings.Contains(threadpage.Render(thread), "\n- change [stated]: rule r1: ~~Never mock the database in tests~~ (dropped: the user allows mocks again) s:3f2a91c0#5\n") {
		t.Fatalf("rules page:\n%s\nthread page:\n%s", retired.Concept.Body, threadpage.Render(thread))
	}
	if v, err := e.wiki().Validate(); err != nil || len(v.Errors)+len(v.Orphans)+len(v.BrokenLinks)+len(v.Warnings) != 0 {
		t.Fatalf("okf validate after the drop: %v %+v", err, v)
	}
	// A rules page a person locked is never written; the pass goes on.
	pg, _ := e.wiki().Read("projects/eva-md/rules")
	if _, err := e.wiki().Update("projects/eva-md/rules", pg.Hash, func(c *okfadapter.Concept) error { c.Extra["locked"] = true; return nil }, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	locked, _ := e.wiki().Read("projects/eva-md/rules")
	rules = []ruleOp{ruleAdd("Never push on Fridays", 1)}
	c.user("And never push on Fridays. " + filler)
	c.assistant("Noted. " + filler)
	e.write(c)
	s = e.run(c)
	after, _ := e.wiki().Read("projects/eva-md/rules")
	if outcomes(s) != "no_change" || string(after.Raw) != string(locked.Raw) || !strings.Contains(e.log.String(), "rules page projects/eva-md/rules (locked) left as it is") {
		t.Fatalf("outcomes %q, page changed: %v\n%s", outcomes(s), string(after.Raw) != string(locked.Raw), e.log)
	}
}

// A rule is dropped only on the user's own words that name it, at or after
// its time, and one for all projects only on words that say so; every edit
// or drop leaves a Timeline line; a chat outside a project keeps its rules
// in the general area; a session older than the page changes no rule.
func TestValidateRuleChanges(t *testing.T) {
	isolate(t)
	at := time.Date(2026, 1, 14, 9, 0, 0, 0, paris)
	onFile := RuleSet{
		General: threadpage.RulesPage{Rules: []threadpage.Item{{ID: "g1", Text: "Always run go vet before committing", Tag: "stated", At: at}}},
		Project: threadpage.RulesPage{Rules: []threadpage.Item{{ID: "r1", Text: "Never mock the database in tests", Tag: "stated", At: at}}},
	}
	c := newChat("3f2a91c0-drop", start)
	c.user("You can mock the database again. " + filler)
	c.assistant("Fine. " + filler)
	c.user("Stop running go vet before committing.")
	c.user("Everywhere, stop running go vet before committing.")
	c.user("What database do we use for the staging environment?")
	c.user("Rule g1 shouldn't apply to all projects anymore, drop it: running go vet before committing is fine.")
	b := build(t, c, threadpage.Page{}, Options{Rules: onFile})
	drop := func(id string, refs ...int) ruleOp {
		return ruleOp{ID: ptr(id), Action: "drop", Tag: "stated", Refs: refs, Reason: ptr("the user changed it")}
	}
	for _, tc := range []struct {
		name  string
		op    ruleOp
		want  RuleCounts
		trail string
	}{
		{"on the user's words", drop("r1", 1), RuleCounts{Dropped: 1}, "rule r1: ~~Never mock the database in tests~~ (dropped: the user changed it)"},
		{"on words that don't name it", drop("r1", 3), RuleCounts{Refused: 1}, ""},
		{"on a question that names one of its words", drop("r1", 5), RuleCounts{Refused: 1}, ""},
		{"on the assistant's words", drop("r1", 2), RuleCounts{Refused: 1}, ""},
		{"without a reason", ruleOp{ID: ptr("r1"), Action: "drop", Tag: "stated", Refs: []int{1}}, RuleCounts{Refused: 1}, ""},
		{"an id not on file", drop("r9", 1), RuleCounts{Ignored: 1}, ""},
		{"all projects' without the words", drop("g1", 3), RuleCounts{Refused: 1}, ""},
		{"all projects' with the words", drop("g1", 4), RuleCounts{Dropped: 1}, "rule g1 for all projects: ~~Always run go vet before committing~~ (dropped: the user changed it)"},
		{"all projects' with the words negated, lifting it", drop("g1", 6), RuleCounts{Dropped: 1}, "rule g1 for all projects: ~~Always run go vet before committing~~ (dropped: the user changed it)"},
		{"an edit on the user's words", ruleEdit("r1", "You can mock the database again", 1), RuleCounts{Edited: 1}, "rule r1: ~~Never mock the database in tests~~ → You can mock the database again"},
	} {
		u, r := validate(t, output{Rules: []ruleOp{tc.op}}, b, threadpage.Page{})
		if r.Rules != tc.want || len(u.Rules) != tc.want.Dropped+tc.want.Edited {
			t.Errorf("%s: %+v, ops %+v", tc.name, r.Rules, u.Rules)
		}
		var trail []threadpage.Event
		if tc.trail != "" {
			trail = []threadpage.Event{{Kind: "change", Text: tc.trail, Tag: "stated", Refs: u.Rules[0].Refs}}
		}
		if !reflect.DeepEqual(u.Events, trail) || tc.want.Dropped == 1 && u.Rules[0].Reason != "the user changed it" {
			t.Errorf("%s: Timeline %+v, ops %+v", tc.name, u.Events, u.Rules)
		}
	}
	// Outside a project every rule is for all projects.
	outside := onFile
	outside.Outside = true
	b = build(t, c, threadpage.Page{}, Options{Rules: outside})
	u, r := validate(t, output{Rules: []ruleOp{ruleAdd("Mock the database again", 1)}}, b, threadpage.Page{})
	if len(u.Rules) != 1 || !u.Rules[0].General || r.Rules.General != 1 {
		t.Fatalf("outside a project: %+v %+v", u.Rules, r.Rules)
	}
	// The out-of-order guard holds every rule as it is.
	b = build(t, c, threadpage.Page{}, Options{Rules: onFile, AsOf: c.at.Add(time.Hour)})
	if u, r := validate(t, output{Rules: []ruleOp{drop("r1", 1), ruleAdd("Mock the database again", 1)}}, b, threadpage.Page{}); len(u.Rules) != 0 || r.Rules.Ignored != 2 {
		t.Fatalf("older session: %+v %+v", u.Rules, r.Rules)
	}
}

// A new rule is for all projects only when the prompt says so: the words
// for it, with no negation of scope before them in their clause unless a
// word of the rule stands between. A change of a rule already for all
// projects only needs the words named, in any sense.
func TestRuleScope(t *testing.T) {
	isolate(t)
	const vet, mocks = "Always run go vet before committing", "Never mock the database"
	for _, tc := range []struct {
		own, rule string
		want      bool
	}{
		{"Never mock the database in all my projects", mocks, true},
		{"Never in all my projects mock the database", mocks, true},
		{"Don't mock the database in all my projects", "Don't mock the database", true},
		{"From now on, in every repo: never commit without tests", "Never commit without tests", true},
		{"Always run go vet, everywhere", vet, true},
		{"This is not just for this repo but for all my repos: " + vet, vet, true},
		{"Don't apply this to all projects, only here: never mock the database", mocks, false},
		{"Never mock the database here, not in all projects", mocks, false},
		{"This rule isn't for every repo: always run go vet before committing", vet, false},
		{"Not everywhere, just here: always run go vet before committing", vet, false},
		{"This isn't a rule for all my projects, only here: always run go vet before committing.", vet, false},
		{"I don't want this in every repo, just here: always run go vet before committing.", vet, false},
		{"Don't add this to all my projects, only here: always run go vet before committing.", vet, false},
		{"Never mock the database in tests", mocks, false},
		{"I don't think this should go in all my projects, just here: you shouldn't mock the database", "You shouldn't mock the database", false},
		{"Don't make this a rule I must follow in all projects: you mustn't push on Fridays", "You mustn't push on Fridays", false},
	} {
		if got := everywhere(tc.own, tc.rule); got != tc.want {
			t.Errorf("everywhere(%q, %q) = %v, want %v", tc.own, tc.rule, got, tc.want)
		}
	}
	if !namesAll("Rule g1 shouldn't apply to all projects anymore, drop it") || namesAll("Drop rule g1") {
		t.Error("namesAll")
	}
}

// End to end, a rule the user keeps to this project is filed with it, and
// a general prohibition goes to all projects.
func TestRuleScopeOfAnAdd(t *testing.T) {
	isolate(t)
	for _, tc := range []struct {
		prompt, rule string
		general      bool
	}{
		{"This isn't a rule for all my projects, only here: always run go vet before committing.", "Always run go vet before committing", false},
		{"Don't mock the database in all my projects.", "Don't mock the database", true},
	} {
		c := newChat("3f2a91c0-scope", start)
		c.user(tc.prompt + " " + filler)
		c.assistant("Noted. " + filler)
		b := build(t, c, threadpage.Page{}, Options{})
		u, r := validate(t, output{Rules: []ruleOp{ruleAdd(tc.rule, 1)}}, b, threadpage.Page{})
		if len(u.Rules) != 1 || u.Rules[0].General != tc.general || (r.Rules.General == 1) != tc.general {
			t.Errorf("%q: %+v %+v", tc.prompt, u.Rules, r.Rules)
		}
	}
}

// A rule keeps the sense of the user's words: a negation stays on the words
// it turns around, and one in another clause, such as a leading "No,",
// doesn't count.
func TestRulePolarity(t *testing.T) {
	isolate(t)
	for _, tc := range []struct {
		own, rule string
		want      bool
	}{
		{"From now on, never mock the database in tests", "Never mock the database in tests", true},
		{"From now on, never mock the database in tests", "Mock the database in tests", false},
		{"Mock the database in tests", "Do not mock the database in tests", false},
		{"No, from now on always run the race tests", "Always run the race tests", true},
		{"Don't worry about the lint. Always run the race tests", "Always run the race tests", true},
		{"Stop mocking the database in tests", "Stop mocking the database in tests", true},
		{"Keep mocking the database in tests for now, never the queue", "Never mock the queue in tests", false},
		{"Never mock the queue; mock the database", "Mock the database", true},
		{"Use pnpm instead of npm", "Use npm instead of pnpm", false},
		{"force pushes are allowed here", "Force pushes aren't allowed", false},
		{"flaky tests would be retried", "Flaky tests wouldn't be retried", false},
		{"the cache isn't shared", "The cache is shared", false},
		{"from now on the main branch isn't for experiments", "The main branch isn't for experiments", true},
	} {
		if got := inOwnWords(tc.rule, tc.own); got != tc.want {
			t.Errorf("%q from %q: %v, want %v", tc.rule, tc.own, got, tc.want)
		}
	}
}
