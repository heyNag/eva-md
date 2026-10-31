package update

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// trailShare is each text of a rule change's Timeline line, so the line fits
// threadpage.TextCap.
const trailShare = 120

// RuleSet is the standing rules a pass sees: the general area's page, which
// holds the rules for all of the user's projects, and the project's own. A
// chat outside a project has only the general area's, and its rules all go
// there.
type RuleSet struct {
	General, Project threadpage.RulesPage
	Outside          bool // the chat is outside a project
}

// page is the rules page of the general area, or of the project.
func (s RuleSet) page(general bool) threadpage.RulesPage {
	switch {
	case general:
		return s.General
	case s.Outside:
		return threadpage.RulesPage{}
	}
	return s.Project
}

// allProjects are the words, read in the cited prompt's own words, that make
// a rule the user's for all of their projects. Without them a rule stays
// with the project.
var allProjects = []string{"all my projects", "all of my projects", "all projects", "every project", "all my repos", "all repos", "every repo",
	"all my repositories", "every repository", "everywhere"}

// namesAllProjects reports whether own names all projects at a place at
// accepts, given the words before that place in its clause.
func namesAllProjects(own string, at func(before []string) bool) bool {
	for _, clause := range clauseEnd.Split(strings.ToLower(strings.ReplaceAll(own, "’", "'")), -1) {
		words := strings.FieldsFunc(clause, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' })
		for i := range words {
			words[i] = strings.Trim(words[i], "'")
		}
		for i := range words {
			for _, p := range allProjects {
				phrase := strings.Fields(p)
				if i+len(phrase) <= len(words) && slices.Equal(words[i:i+len(phrase)], phrase) && at(words[:i]) {
					return true
				}
			}
		}
	}
	return false
}

// everywhere reports whether own, one prompt's own words, makes a new rule
// one for all projects: it names all projects, and no negation of scope
// (not, cannot or a word ending in n't) comes before those words in their
// clause without a word of the rule between them. So "this isn't a rule for
// all my projects" and "don't add this to every repo" keep a rule here, while
// in "don't mock the database in all my projects" the negation is the rule's
// own, as never and avoid always are.
func everywhere(own, rule string) bool {
	words := contentTokens(rule)
	return namesAllProjects(own, func(before []string) bool { return !negatedBefore(before, words) })
}

// namesAll reports whether own names all projects in any sense, as a change
// of a rule already for all projects needs: "rule g1 shouldn't apply to all
// projects anymore" names the rule's scope to lift it.
func namesAll(own string) bool { return namesAllProjects(own, func([]string) bool { return true }) }

// negatedBefore reports whether before, the words ahead of the words for all
// projects in their clause, hold a negation of scope with no word of the
// rule after it.
func negatedBefore(before []string, rule map[string]bool) bool {
	for i := len(before) - 1; i >= 0; i-- {
		w := before[i]
		if w == "not" || w == "cannot" || strings.HasSuffix(w, "n't") {
			return true
		}
		for k := range rule {
			if sameWord(k, w) {
				return false
			}
		}
	}
	return false
}

// rules checks the output's rule changes: edits and drops first, the first
// one for a rule on file only, then the adds. A rule is the user's own: a
// change applies only when it is tagged stated and one cited user prompt's
// own words, never text pasted into it, state the rule: they hold every
// distinctive word of it in the same sense, or for a drop at least half of
// the dropped rule's. An add goes to the general area's page when that
// prompt also says all projects. An edit or drop needs a cited prompt at or
// after the rule's time, as a supersession does, and a rule for all projects
// changes only on a prompt that also says all projects. Every edit or drop
// leaves a Timeline line on the thread page. While the out-of-order guard
// keeps the Now, no rule changes, as no decision does.
func (v *validator) rules(ops []ruleOp) {
	if v.u.KeepNow {
		v.report.Rules.Ignored += len(ops)
		return
	}
	acted := map[string]bool{}
	for _, op := range ops {
		if op.Action != "add" {
			v.changeRule(op, acted)
		}
	}
	for _, op := range ops {
		if op.Action == "add" {
			v.addRule(op)
		}
	}
}

// addRule checks a new rule.
func (v *validator) addRule(op ruleOp) {
	text := scrub(deref(op.Text), threadpage.TextCap)
	c := v.cite(op.Refs)
	states := func(own string) bool { return inOwnWords(text, own) }
	general := v.b.Rules.Outside || said(c, states, func(own string) bool { return everywhere(own, text) })
	n := &v.report.Rules
	switch {
	case !v.dated(text, c.nums):
		v.report.Dates++
	case op.Tag != "stated" || !said(c, states, nil):
		n.Refused++
	case v.ruleKeys[general][normalize(text)]:
		n.Duplicates++
	case v.ruleN[general] >= threadpage.RulesCap:
		n.Refused++
	default:
		v.ruleKeys[general][normalize(text)] = true
		v.ruleN[general]++
		v.u.Rules = append(v.u.Rules, threadpage.RuleOp{General: general, Action: "add", Text: text, Tag: "stated", Refs: c.refs, At: c.newestUser})
		n.Added++
		if general {
			n.General++
		}
		v.ground(text)
	}
}

// changeRule checks an edit or a drop of a rule on file: one cited prompt
// must state an edit's new text, or name a dropped rule.
func (v *validator) changeRule(op ruleOp, acted map[string]bool) {
	n := &v.report.Rules
	id := strings.TrimSpace(deref(op.ID))
	h, ok := v.rulesOn[id]
	if !ok || acted[id] {
		n.Ignored++
		return
	}
	acted[id] = true
	c := v.cite(op.Refs)
	edit := op.Action == "edit"
	text := scrub(deref(op.Text), threadpage.TextCap)
	user := func(own string) bool { return inOwnWords(text, own) }
	if !edit {
		text = scrub(deref(op.Reason), threadpage.TextCap)
		user = func(own string) bool { return mentions(own, h.item.Text) }
	}
	switch {
	case text == "":
		n.Refused++
	case !v.dated(text, c.nums):
		v.report.Dates++
	case edit && v.ruleKeys[h.general][normalize(text)]:
		n.Duplicates++
	case op.Tag != "stated" || !said(c, user, nil):
		n.Refused++
	case h.general && !v.b.Rules.Outside && !said(c, user, namesAll):
		n.Refused++
	case !h.item.At.IsZero() && (c.newestUser.IsZero() || c.newestUser.Before(h.item.At)):
		n.Stale++
	case edit:
		v.ruleKeys[h.general][normalize(text)] = true
		v.u.Rules = append(v.u.Rules, threadpage.RuleOp{General: h.general, ID: id, Action: "edit", Text: text, Tag: "stated", Refs: c.refs, At: c.newestUser})
		n.Edited++
		v.ground(text)
		v.trail(id, h, "→ "+cutRunes(text, trailShare), c)
	default:
		v.ruleN[h.general]--
		v.u.Rules = append(v.u.Rules, threadpage.RuleOp{General: h.general, ID: id, Action: "drop", Tag: "stated", Reason: text, Refs: c.refs, At: c.newestUser})
		n.Dropped++
		v.trail(id, h, "(dropped: "+cutRunes(text, trailShare)+")", c)
	}
}

// trail adds the Timeline line a kept edit or drop leaves on the thread
// page, "rule <id>: ~~<old>~~ → <new>" or "rule <id>: ~~<old>~~ (dropped:
// <reason>)", so no rule changes off the record and eva-md search finds the
// change.
func (v *validator) trail(id string, h heldRule, change string, c cites) {
	name := "rule " + id
	if h.general {
		name += " for all projects"
	}
	text := name + ": ~~" + cutRunes(h.item.Text, trailShare) + "~~ " + change
	v.u.Events = append(v.u.Events, threadpage.Event{Kind: "change", Text: text, Tag: "stated", Refs: c.refs})
	v.at, v.linesAt = later(v.at, c.newest), later(v.linesAt, c.newest)
}

// said reports whether one cited prompt's own words, by ok, hold up a rule
// change, and, when scope is set, say by it that the rule is for all
// projects.
func said(c cites, ok, scope func(own string) bool) bool {
	return slices.ContainsFunc(c.owns, func(own string) bool { return ok(own) && (scope == nil || scope(own)) })
}

// inOwnWords reports whether own, one prompt's own words, states a rule: it
// holds every distinctive word of the rule in the same sense (see
// samePolarity). A rule without a distinctive word holds no instruction.
func inOwnWords(rule, own string) bool {
	n, all := found(rule, own)
	return all > 0 && n == all && samePolarity(rule, own)
}

// mentions reports whether own names a rule: it holds at least half of the
// rule's distinctive words.
func mentions(own, rule string) bool {
	n, all := found(rule, own)
	return all > 0 && 2*n >= all
}

// found returns how many of a rule's distinctive words, its content tokens,
// own holds, and how many it has. It finds a code-like token as the
// grounding count does: a path by its basename too, a SHA by a prefix of 7.
func found(rule, own string) (n, all int) {
	hay := strings.ToLower(own)
	codes := map[string]codeToken{}
	for _, t := range codeTokens(rule) {
		codes[strings.ToLower(t.text)] = t
	}
	words := contentTokens(rule)
	for w := range words {
		if t, ok := codes[w]; ok && grounded(t, hay) || strings.Contains(hay, w) {
			n++
		}
	}
	return n, len(words)
}

// negations are the words that turn around what follows them in their
// clause.
var negations = map[string]bool{"never": true, "not": true, "no": true, "nor": true, "don't": true, "dont": true, "doesn't": true,
	"shouldn't": true, "mustn't": true, "won't": true, "can't": true, "cannot": true, "stop": true, "avoid": true, "without": true, "instead": true}

// clauseEnd ends a clause: a line break, punctuation, or "but".
var clauseEnd = regexp.MustCompile(`[\n,;:!?()—–]|\.(?:\s|$)|\bbut\b`)

// samePolarity reports whether a rule keeps the sense of own, the user's
// words: own negates every word the rule negates, and doesn't only negate a
// word the rule asserts. So "Mock the database" doesn't keep "from now on,
// never mock the database", while "Always run the race tests" keeps "No,
// always run the race tests". Each word may be in another form, one
// starting with the other, as "mock" and "mocking".
func samePolarity(rule, own string) bool {
	ruleNeg, rulePlain := senses(rule)
	ownNeg, ownPlain := senses(own)
	for w := range ruleNeg {
		if !anyForm(ownNeg, w) {
			return false
		}
	}
	for w := range rulePlain {
		if anyForm(ownNeg, w) && !anyForm(ownPlain, w) {
			return false
		}
	}
	return true
}

// senses splits the words of s of 3 letters or more, without stopwords, into
// those a negation before them in their clause turns around and the others.
// Every negative contraction is a negation, with or without its apostrophe.
func senses(s string) (negated, plain map[string]bool) {
	negated, plain = map[string]bool{}, map[string]bool{}
	for _, clause := range clauseEnd.Split(strings.ToLower(strings.ReplaceAll(s, "’", "'")), -1) {
		neg := false
		for _, w := range strings.FieldsFunc(clause, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' }) {
			switch w = strings.Trim(w, "'"); {
			case negations[w] || strings.HasSuffix(w, "n't") || contractions[w]:
				neg = true
			case utf8.RuneCountInString(w) < 3 || stopwords[w]:
			case neg:
				negated[w] = true
			default:
				plain[w] = true
			}
		}
	}
	return negated, plain
}

// anyForm reports whether words holds w in some form: one of them starts
// with the other.
func anyForm(words map[string]bool, w string) bool {
	for k := range words {
		if strings.HasPrefix(k, w) || strings.HasPrefix(w, k) {
			return true
		}
	}
	return false
}
