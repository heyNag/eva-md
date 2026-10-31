package threadpage_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tp "github.com/heyNag/eva-md/internal/threadpage"
)

// The rules page drawn in rules.go's comment, and its body.
const rulesBody = "\n# eva-md · rules\n\n## Rules\n" +
	"- Never mock the database in tests [stated] s:3f2a91c0#14 <!-- i:r1 t:2026-01-14T10:00:00+01:00 -->\n"

func TestRulesPageRoundTrips(t *testing.T) {
	isolate(t)
	at := time.Date(2026, 1, 14, 10, 0, 0, 0, plus1)
	p := tp.RulesPage{Title: "eva-md · rules", Rules: []tp.Item{{ID: "r1", Text: "Never mock the database in tests", Tag: "stated", Refs: []string{"s:3f2a91c0#14"}, At: at}}}
	if got := tp.RenderRules(p); got != rulesBody {
		t.Fatalf("RenderRules =\n%q\nwant\n%q", got, rulesBody)
	}
	got, err := tp.ParseRules(rulesBody)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("ParseRules = %#v, %v", got, err)
	}
	// A person's edits: a line without an id or a tag, a section of their
	// own, kept byte for byte after the rules, and no rules at all.
	hand := rulesBody + "* Always run go vet first\n\n## Notes\nKept as written.\n\n```\n## not a section\n```\n"
	got, err = tp.ParseRules(hand)
	if err != nil || len(got.Rules) != 2 || got.Rules[1].Text != "Always run go vet first" || got.Rules[1].ID != "" ||
		!reflect.DeepEqual(got.Other, []string{"## Notes\nKept as written.\n\n```\n## not a section\n```"}) {
		t.Fatalf("hand-edited page = %#v, %v", got, err)
	}
	if again, _ := tp.ParseRules(tp.RenderRules(got)); !reflect.DeepEqual(again, got) {
		t.Fatalf("a hand-edited page changed in a round trip: %#v", again)
	}
	if empty, err := tp.ParseRules("\n# t\n\n## Notes\nnone\n"); err != nil || empty.Rules != nil || len(empty.Other) != 1 {
		t.Fatalf("a page without rules = %#v, %v", empty, err)
	}
	// What a rewrite would lose is damage, never written.
	for name, body := range map[string]string{
		"a heading among the rules": rulesBody + "### More\n",
		"a fence among the rules":   rulesBody + "```\n- x\n```\n",
		"text before the section":   "\nloose text\n" + rulesBody,
		"two rules sections":        rulesBody + "\n## Rules\n- again\n",
	} {
		if _, err := tp.ParseRules(body); !errors.Is(err, tp.ErrDamaged) {
			t.Errorf("%s: err %v, want ErrDamaged", name, err)
		}
	}
}

func TestApplyRules(t *testing.T) {
	isolate(t)
	at := time.Date(2026, 1, 14, 10, 0, 0, 0, plus1)
	rule := func(general bool, action, id, text string, k int) tp.RuleOp {
		return tp.RuleOp{General: general, ID: id, Action: action, Text: text, Tag: "stated", Refs: []string{fmt.Sprintf("s:3f2a91c0#%d", k)}, At: at.Add(time.Duration(k) * time.Minute)}
	}
	page := tp.RulesPage{Title: "eva-md · rules", Rules: []tp.Item{{Text: "Written by hand"}, {ID: "g4", Text: "Copied from the general page"}}}
	ops := []tp.RuleOp{
		rule(false, "add", "", "Never mock the database in tests", 1),
		rule(false, "add", "", "Run the race tests before calling it done", 2),
		rule(true, "add", "", "A rule for all projects", 3),
		rule(false, "add", "", "Never mock the database in tests", 4),
	}
	got, st := tp.ApplyRules(page, ops, false)
	var ids, texts []string
	for _, it := range got.Rules {
		ids, texts = append(ids, it.ID), append(texts, it.Text)
	}
	if strings.Join(ids, " ") != "r1 r2 r3 r4" || texts[2] != "Never mock the database in tests" || st != (tp.RulesStats{Added: 2, Numbered: 2, Duplicate: 1}) {
		t.Fatalf("ids %v, texts %q, stats %+v", ids, texts, st)
	}
	if len(page.Rules) != 2 || page.Rules[0].ID != "" {
		t.Fatal("ApplyRules changed its input")
	}
	if again, st := tp.ApplyRules(got, ops, false); !reflect.DeepEqual(again, got) || st.Added+st.Numbered != 0 {
		t.Fatalf("applying the same changes twice changed the page: %+v", st)
	}
	// An edit replaces the rule and moves it to the end, the newest; a drop
	// takes one out; a second operation on an id, or one on an id of the
	// other page, changes nothing; ids are never shared with a living rule.
	got, st = tp.ApplyRules(got, []tp.RuleOp{
		rule(false, "edit", "r3", "Use a real database in every test", 5),
		rule(false, "drop", "r3", "", 6),
		rule(false, "drop", "r1", "", 6),
		rule(false, "drop", "g4", "", 6),
		rule(false, "add", "", "Write the changelog first", 7),
	}, false)
	ids, texts = nil, nil
	for _, it := range got.Rules {
		ids, texts = append(ids, it.ID), append(texts, it.Text)
	}
	if strings.Join(ids, " ") != "r2 r4 r3 r5" || texts[2] != "Use a real database in every test" || !got.Rules[2].At.Equal(at.Add(5*time.Minute)) ||
		got.Rules[2].Refs[0] != "s:3f2a91c0#5" || st != (tp.RulesStats{Added: 1, Edited: 1, Dropped: 1}) {
		t.Fatalf("ids %v, texts %q, stats %+v", ids, texts, st)
	}
	body := tp.RenderRules(got)
	if !strings.Contains(body, "\n- Use a real database in every test [stated] s:3f2a91c0#5 <!-- i:r3 t:2026-01-14T10:05:00+01:00 -->\n") {
		t.Fatalf("body:\n%s", body)
	}
	// A dropped rule moves, struck through, to the retired rules, and its id,
	// even the largest, is never used again.
	retire := []tp.RuleOp{
		{ID: "r5", Action: "drop", Reason: "the user writes it last now", Refs: []string{"s:91bd0e44#2"}, At: time.Date(2026, 2, 3, 9, 0, 0, 0, plus1)},
		rule(false, "add", "", "Another rule", 8),
	}
	got, st = tp.ApplyRules(got, retire, false)
	ids = nil
	for _, it := range got.Rules {
		ids = append(ids, it.ID)
	}
	if strings.Join(ids, " ") != "r2 r4 r3 r6" || st != (tp.RulesStats{Added: 1, Dropped: 1}) {
		t.Fatalf("ids %v, stats %+v", ids, st)
	}
	body = tp.RenderRules(got)
	if want := "\n\n## Retired rules\n- ~~Written by hand~~ (dropped 2026-01-14) s:3f2a91c0#6 <!-- i:r1 -->\n" +
		"- ~~Write the changelog first~~ (dropped 2026-02-03: the user writes it last now) [stated] s:3f2a91c0#7 s:91bd0e44#2 <!-- i:r5 t:2026-01-14T10:07:00+01:00 -->\n"; !strings.HasSuffix(body, want) {
		t.Fatalf("body:\n%s", body)
	}
	if again, err := tp.ParseRules(body); err != nil || !reflect.DeepEqual(again, got) {
		t.Fatalf("the retired rules changed in a round trip: %#v, %v", again, err)
	}
	if again, _ := tp.ApplyRules(got, retire, false); !reflect.DeepEqual(again, got) {
		t.Fatal("applying the same drop twice changed the page")
	}
	// The general area's page numbers g<k>, and a page holds at most 40
	// rules.
	var many []tp.RuleOp
	for i := 0; i < tp.RulesCap+2; i++ {
		many = append(many, rule(true, "add", "", fmt.Sprintf("Rule number %d", i), i))
	}
	general, st := tp.ApplyRules(tp.RulesPage{}, many, true)
	if len(general.Rules) != tp.RulesCap || general.Rules[0].ID != "g1" || st.OverCap != 2 || len(tp.HeldRules(general, true)) != tp.RulesCap || len(tp.HeldRules(general, false)) != 0 {
		t.Fatalf("%d rules, first %q, stats %+v", len(general.Rules), general.Rules[0].ID, st)
	}
}
