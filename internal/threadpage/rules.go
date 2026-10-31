package threadpage

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// A rules page holds the user's standing rules: projects/<slug>/rules.md
// for one project, general/rules.md for all of them. RenderRules writes its
// body in this shape, one "## Rules" section of items in the thread page's
// item format, oldest first:
//
//	# eva-md · rules
//
//	## Rules
//	- Never mock the database in tests [stated] s:3f2a91c0#14 <!-- i:r1 t:2026-01-14T10:00:00+01:00 -->
//
// A rule's id is r<k> on a project's page and g<k> on the general area's, so
// one pass can name the rules of both. A dropped rule moves, struck through,
// to a "## Retired rules" section after them, which keeps its id, so the id
// is never used again:
//
//	## Retired rules
//	- ~~Never mock the database in tests~~ (dropped 2026-02-03: the user wants mocks again) [stated] s:3f2a91c0#14 s:91bd0e44#5 <!-- i:r1 t:2026-01-14T10:00:00+01:00 -->
//
// A person may edit the page by hand: a rule without such an id gets one at
// the next write, and any other "## " section is kept byte for byte after
// the rules.

// RulesCap is the most rules a page holds.
const RulesCap = 40

const (
	rulesHeading   = "## Rules"
	retiredHeading = "## Retired rules"
)

// RulesPage is a rules page's body.
type RulesPage struct {
	Title string
	Rules []Item
	Other []string // any other "## " section, verbatim, in order
}

// RuleOp is one validated change to a rules page.
type RuleOp struct {
	General   bool   // the general area's page, for all of the user's projects; otherwise the project's
	ID        string // the rule an edit or drop acts on; empty for an add
	Action    string // add, edit or drop
	Text, Tag string
	Reason    string // why a rule is dropped
	Refs      []string
	At        time.Time // the newest cited user prompt's time
}

// RulesStats counts what ApplyRules changed.
type RulesStats struct {
	Added, Edited, Dropped int
	Numbered               int // rules without an id that got one
	Duplicate              int // adds already on the page, left out
	OverCap                int // adds past RulesCap, left out
}

// RenderRules writes a rules page's body. Like Render, it starts with a
// blank line and ends with one newline, and its free text is escaped and
// capped, so ParseRules(RenderRules(p)) equals p for a page whose text is
// already escaped.
func RenderRules(p RulesPage) string {
	var parts []string
	if t := cleanLine(p.Title, 0); t != "" {
		parts = append(parts, "# "+t)
	}
	lines := []string{rulesHeading}
	for _, it := range p.Rules {
		lines = append(lines, itemLine(it, false))
	}
	parts = append(parts, strings.Join(lines, "\n"))
	parts = append(parts, p.Other...)
	return "\n" + strings.TrimRightFunc(strings.Join(parts, "\n\n"), unicode.IsSpace) + "\n"
}

// ParseRules reads a body in the shape RenderRules writes. Every line of the
// rules section but a blank one is a rule, in the item format or not. A
// heading or a fence among the rules, text before the first section other
// than the title, and a repeated rules section return an error wrapping
// ErrDamaged, so a rewrite never loses text. A page without a rules section
// has no rules.
func ParseRules(body string) (RulesPage, error) {
	var p RulesPage
	section, titled, saw := "", false, false
	var other []string
	fence := ""
	flush := func() {
		if other != nil {
			p.Other = append(p.Other, strings.Join(trimBlank(other), "\n"))
		}
		other, fence = nil, ""
	}
	for _, l := range strings.Split(body, "\n") {
		if h, ok := sectionHeading(l); ok && fence == "" {
			flush()
			if h != rulesHeading {
				section, other = "other", []string{l}
				continue
			}
			if saw {
				return RulesPage{}, damaged("the ## Rules section is repeated")
			}
			section, saw = "rules", true
			continue
		}
		t := strings.TrimSpace(l)
		switch section {
		case "":
			if t == "" {
				continue
			}
			if title, ok := titleLine(l); ok && !titled {
				p.Title, titled = title, true
				continue
			}
			return RulesPage{}, damaged("text before the first section")
		case "rules":
			switch {
			case t == "":
			case headingLine.MatchString(t) || NextFence("", t) != "":
				return RulesPage{}, damaged("a heading or a fence among the rules")
			default:
				p.Rules = append(p.Rules, parseItem(t, false))
			}
		default:
			other = append(other, l)
			fence = NextFence(fence, l)
		}
	}
	flush()
	return p, nil
}

// rulePrefix starts the ids of a page's rules: g on the general area's page,
// r on a project's.
func rulePrefix(general bool) string {
	if general {
		return "g"
	}
	return "r"
}

// ruleNumber returns k of an id <prefix><k>, or 0 for any other id.
func ruleNumber(id, prefix string) int {
	digits, ok := strings.CutPrefix(id, prefix)
	if !ok || digits == "" || len(digits) > 9 || strings.Trim(digits, "0123456789") != "" {
		return 0
	}
	k, _ := strconv.Atoi(digits)
	return k
}

// HeldRules returns the rules of p an operation can name, the general area's
// page when general is set: those with an id of the page's kind, each id
// once, in page order.
func HeldRules(p RulesPage, general bool) []Item {
	var out []Item
	seen := map[string]bool{}
	for _, it := range p.Rules {
		if ruleNumber(it.ID, rulePrefix(general)) > 0 && !seen[it.ID] {
			seen[it.ID] = true
			out = append(out, it)
		}
	}
	return out
}

// ApplyRules places a pass's validated rule changes on p, the general area's
// page when general is set, and returns the new page; p itself is not
// changed. Only the operations for that page count. Edits and drops run
// first, in order, the first one for an id only: an edit replaces the rule's
// text, tag, refs and time and moves it to the end, where the newest rules
// are, and a drop moves it to the retired rules. Then each rule without an
// id of the page's kind, or with one an earlier rule holds, gets the next
// one, in page order, and the adds follow. An add whose text is on the page
// already, or past RulesCap, changes nothing, so applying the same changes
// twice changes nothing. A new id is one more than the largest on the page,
// a retired rule's included.
func ApplyRules(p RulesPage, ops []RuleOp, general bool) (RulesPage, RulesStats) {
	out := RulesPage{Title: p.Title, Rules: cloneItems(p.Rules), Other: slices.Clone(p.Other)}
	var st RulesStats
	prefix := rulePrefix(general)
	done := map[string]bool{}
	for _, op := range ops {
		if op.General != general || op.Action == "add" || done[op.ID] {
			continue
		}
		i := slices.IndexFunc(out.Rules, func(it Item) bool { return it.ID == op.ID && ruleNumber(it.ID, prefix) > 0 })
		if i < 0 {
			continue
		}
		done[op.ID] = true
		switch op.Action {
		case "edit":
			it := out.Rules[i]
			if it.Text = cleanLine(op.Text, TextCap); it.Text == "" {
				continue
			}
			it.Tag, it.Refs, it.At = op.Tag, cleanRefs(op.Refs), canonical(op.At)
			out.Rules = append(remove(out.Rules, i), it)
			st.Edited++
		case "drop":
			out.Other = retire(out.Other, out.Rules[i], op)
			out.Rules = remove(out.Rules, i)
			st.Dropped++
		}
	}
	next := 0
	for _, it := range out.Rules {
		next = max(next, ruleNumber(it.ID, prefix))
	}
	for _, s := range out.Other {
		for _, id := range commentIDs(s) {
			next = max(next, ruleNumber(id, prefix))
		}
	}
	seen := map[string]bool{}
	for i := range out.Rules {
		if id := out.Rules[i].ID; ruleNumber(id, prefix) == 0 || seen[id] {
			next++
			out.Rules[i].ID = prefix + strconv.Itoa(next)
			st.Numbered++
		}
		seen[out.Rules[i].ID] = true
	}
	for _, op := range ops {
		if op.General != general || op.Action != "add" {
			continue
		}
		it := cleanItem(Item{Text: op.Text, Tag: op.Tag, Refs: op.Refs, At: op.At})
		switch {
		case it.Text == "":
		case slices.ContainsFunc(out.Rules, func(have Item) bool { return have.Text == it.Text }):
			st.Duplicate++
		case len(out.Rules) >= RulesCap:
			st.OverCap++
		default:
			next++
			it.ID = prefix + strconv.Itoa(next)
			out.Rules = append(out.Rules, it)
			st.Added++
		}
	}
	return out, st
}

// retire adds a dropped rule's line, struck through with the drop's date and
// reason, to the retired rules in other, starting that section after the
// others when there is none.
func retire(other []string, it Item, op RuleOp) []string {
	why := "(dropped"
	if !op.At.IsZero() {
		why += " " + canonical(op.At).Format(time.DateOnly)
	}
	if r := cleanLine(op.Reason, TextCap); r != "" {
		why += ": " + r
	}
	parts := append([]string{"~~" + cleanLine(it.Text, TextCap) + "~~", why + ")", tagToken(it.Tag)}, mergeRefs(it.Refs, cleanRefs(op.Refs))...)
	line := "- " + joinNonEmpty(" ", append(parts, comment(it.ID, it.At))...)
	for i, s := range other {
		if heading, _, _ := strings.Cut(s, "\n"); strings.TrimRight(heading, " \t\r") == retiredHeading {
			other[i] = s + "\n" + line
			return other
		}
	}
	return append(other, retiredHeading+"\n"+line)
}
