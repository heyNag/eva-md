package resume

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/okfadapter"
)

// The user's standing rules live on two rules pages: general/rules.md for
// all of their projects and projects/<slug>/rules.md for one. Startup, clear
// and compact briefs show them first, and a refresh or a resumed chat shows
// them again when they changed.

// rulesCap bounds the standing rules a brief shows, their heading and
// "more" line included, on both hosts.
const rulesCap = 600

// generalRules is the id of the general area's rules page.
const generalRules = "general/rules"

// The headings of the standing rules: in a brief, and once a rule changed
// since the chat last saw them. A marker keeps only their hash, so the
// second shows them all, not what changed.
const (
	rulesHead        = "Standing rules, the user's own words, newest first:"
	changedRulesHead = "Standing rules (a rule changed since this chat last saw them), newest first:"
)

// standing is what the rules pages hold: each page's rules, newest first,
// and the hash of both.
type standing struct {
	general, project     []string // the rules' lines without their hidden comments
	generalID, projectID string   // their pages
	hash                 string   // "" without rules
}

// readStanding reads the general area's rules page and, given the project's
// wiki folder, the project's, each by its path.
func readStanding(w *okfadapter.Wiki, slug string) standing {
	s := standing{generalID: generalRules, general: ruleLines(w, generalRules)}
	if slug != "" {
		s.projectID = "projects/" + slug + "/rules"
		s.project = ruleLines(w, s.projectID)
	}
	if len(s.general)+len(s.project) > 0 {
		s.hash = hash(strings.Join(s.general, "\n") + "\n\x00\n" + strings.Join(s.project, "\n"))
	}
	return s
}

// ruleLines returns the rules on the page id, newest first: the item lines
// of its "## Rules" section, which lists the newest last, each without its
// hidden comment, as the Now's are shown. A page that is missing or can't
// be read has none.
func ruleLines(w *okfadapter.Wiki, id string) []string {
	got, err := w.ReadMax(id, pageCap)
	if err != nil {
		return nil
	}
	var out []string
	in := false
	for _, line := range strings.Split(got.Concept.Body, "\n") {
		if t := strings.TrimRight(line, " \t\r"); t == "##" || strings.HasPrefix(t, "## ") {
			in = t == "## Rules"
			continue
		}
		if l := stripComments(strings.TrimSpace(line)); in && (strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ")) {
			out = append(out, l)
		}
	}
	slices.Reverse(out)
	return out
}

// rulesBlock shows the standing rules under head in at most rulesCap runes:
// all projects', then this project's, each newest first, by whole lines,
// ending with one "N more" line that names the page of each one left out.
// It is "" without rules.
func rulesBlock(o Options, s standing, head string) string {
	type rule struct {
		page int // 0 for all projects', 1 for this project's
		line string
	}
	pages := [2]string{s.generalID, s.projectID}
	heads := [2]string{"For all projects:", "For this project:"}
	var all []rule
	for i, lines := range [2][]string{s.general, s.project} {
		for _, l := range lines {
			if l = oneLine(shown(l), rulesCap); l != "" {
				all = append(all, rule{i, l})
			}
		}
	}
	bin := filepath.Join(o.Dirs.Share, "bin", "eva-md")
	for k := len(all); k >= 0 && len(all) > 0; k-- {
		lines, page := []string{head}, -1
		for _, r := range all[:k] {
			if r.page != page {
				page = r.page
				lines = append(lines, heads[page])
			}
			lines = append(lines, r.line)
		}
		if left := all[k:]; len(left) > 0 {
			var shows []string
			for i, id := range pages {
				if slices.ContainsFunc(left, func(r rule) bool { return r.page == i }) {
					shows = append(shows, bin+" show "+id)
				}
			}
			lines = append(lines, fmt.Sprintf("%d more: %s", len(left), strings.Join(shows, ", ")))
		}
		if text := strings.Join(lines, "\n"); runes(text) <= rulesCap {
			return text
		}
	}
	return ""
}

// joinHash is the hash a briefed marker records of what a chat has seen:
// the Now's and, after a "+", the standing rules' when there are any, so a
// marker written before there were rules reads as it did.
func joinHash(now, rules string) string {
	if rules == "" {
		return now
	}
	return now + "+" + rules
}

// splitHash reads a hash joinHash made.
func splitHash(h string) (now, rules string) {
	now, rules, _ = strings.Cut(h, "+")
	return now, rules
}
