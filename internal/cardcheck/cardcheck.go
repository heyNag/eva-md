// Package cardcheck checks the v2 build's contract matrix and its cards
// against the plan and the six area designs: every need one area has of
// another has a provider, no provider comes later than its consumer, every
// card names a plan package, and a card that consumes a need with no
// provider fails. Nothing imports it; its test runs Check on the
// repository.
package cardcheck

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
)

// The files Check reads, from the repository root.
const (
	PlanFile   = "docs/v2/plan.md"
	DesignDir  = "docs/v2/working-papers/design"
	CardsDir   = "docs/v2/cards"
	MatrixFile = "docs/v2/contract-matrix.md"
)

// Problem is one breach of a rule.
type Problem struct {
	File string // the file it is in, from the repository root
	Item string // the matrix row or the card
	Rule string // the card's letter for the rule, a to g
	Text string
}

func (p Problem) String() string {
	return fmt.Sprintf("%s: %s: rule %s: %s", p.File, p.Item, p.Rule, p.Text)
}

// Check reads the plan, the designs' contract sections, the cards and the
// contract matrix from fsys, the repository's root, and returns every
// problem, sorted. The rules, as IF-01a's card names them:
//
//	a. every design need row is in the matrix exactly once, and every
//	   matrix row has its design row;
//	b. every Consumer and Provider id is a plan id, and a provider is ids
//	   or one of the words v1, later, dropped and open;
//	c. no provider is in a later stage than any of its consumers;
//	d. every card names at least one plan package;
//	e. every id in a card's "Depends on" label is a plan id or a card;
//	f. a card fails when one of its packages consumes a need whose
//	   provider is open, later or dropped;
//	g. every fake is an existing folder, or reads "waits:" and a plan id
//	   or a card.
func Check(fsys fs.FS) []Problem {
	var ps []Problem
	add := func(file, item, rule, format string, args ...any) {
		ps = append(ps, Problem{file, item, rule, fmt.Sprintf(format, args...)})
	}

	planText, err := fs.ReadFile(fsys, PlanFile)
	if err != nil {
		add(PlanFile, "the plan", "b", "can't be read: %v", err)
		return ps
	}
	plan, err := parsePlan(string(planText))
	if err != nil {
		add(PlanFile, "the plan", "b", "%v", err)
		return ps
	}
	needs, err := readDesignNeeds(fsys)
	if err != nil {
		add(DesignDir, "the designs", "a", "can't be read: %v", err)
		return ps
	}
	cards, err := readCards(fsys, plan)
	if err != nil {
		add(CardsDir, "the cards", "d", "can't be read: %v", err)
		return ps
	}
	matrixText, err := fs.ReadFile(fsys, MatrixFile)
	if err != nil {
		add(MatrixFile, "the matrix", "a", "can't be read: %v", err)
		return ps
	}
	m := parseMatrix(string(matrixText))
	isCard := map[string]bool{}
	for _, c := range cards {
		isCard[c.id] = true
	}

	// a. Each need once, and nothing else.
	if !m.needsTable {
		add(MatrixFile, "the matrix", "a", `has no table headed "Need" under "## Needs"`)
	}
	if len(needs) == 0 {
		add(DesignDir, "the designs", "a", `no design has a "## <n>. Contracts" section with a need`)
	}
	rows := map[needKey]int{}
	for _, r := range m.needs {
		rows[r.key]++
		if r.cells != 5 {
			add(MatrixFile, r.item(), "a", "has %d cells, not 5", r.cells)
			continue
		}
		if !needs[r.key] {
			add(MatrixFile, r.item(), "a", "is not a need row of %s's contracts", r.key.design)
		}
	}
	for _, k := range sortedKeys(needs) {
		switch n := rows[k]; {
		case n == 0:
			add(MatrixFile, k.String(), "a", "the design's need has no row in the matrix")
		case n > 1:
			add(MatrixFile, k.String(), "a", "the design's need has %d rows in the matrix", n)
		}
	}

	// b and c. Ids, words and stages.
	for _, r := range m.needs {
		if r.cells != 5 {
			continue
		}
		if r.consumer == "" {
			add(MatrixFile, r.item(), "b", "has no consumer: plan ids, or %q", noConsumer)
		}
		for _, id := range r.consumers {
			if _, ok := plan[id]; !ok {
				add(MatrixFile, r.item(), "b", "consumer %q is not a plan id", id)
			}
		}
		word := providerWord(r.provider)
		if word == "" {
			if len(r.providers) == 0 {
				add(MatrixFile, r.item(), "b", "has no provider")
			}
			for _, id := range r.providers {
				if _, ok := plan[id]; !ok {
					add(MatrixFile, r.item(), "b", "provider %q is neither a plan id nor one of v1, later, dropped and open alone", id)
				}
			}
		}
		if r.where == "" {
			add(MatrixFile, r.item(), "b", "says nowhere what meets the need")
		}
		if word != "" {
			continue
		}
		for _, p := range r.providers {
			for _, c := range r.consumers {
				ps1, ok1 := plan[p]
				cs, ok2 := plan[c]
				if ok1 && ok2 && ps1 > cs {
					add(MatrixFile, r.item(), "c", "provider %s is in Stage %d, after its consumer %s in Stage %d", p, ps1, c, cs)
				}
			}
		}
	}

	// d, e and f. The cards.
	for _, c := range cards {
		if len(c.packages) == 0 {
			add(c.file, c.id, "d", "names no plan package: its id, less a lowercase part, is not one, and its title line has none in brackets")
		}
		for _, id := range c.depends {
			if _, ok := plan[id]; !ok && !isCard[id] {
				add(c.file, c.id, "e", "depends on %s, which is neither a plan id nor a card", id)
			}
		}
		for _, pkg := range c.packages {
			for _, r := range m.needs {
				w := providerWord(r.provider)
				if (w == providerOpen || w == providerLater || w == providerDropped) && slices.Contains(r.consumers, pkg) {
					add(c.file, c.id, "f", "its package %s consumes %s, whose provider is %s: %s", pkg, r.key, w, r.where)
				}
			}
		}
	}

	// g. The fakes.
	if !m.fakesTable {
		add(MatrixFile, "the matrix", "g", `has no table headed "Interface" under "## Interfaces and fakes"`)
	}
	for _, f := range m.fakes {
		item := f.name
		if f.cells != 4 {
			add(MatrixFile, item, "g", "has %d cells, not 4", f.cells)
			continue
		}
		if !fakeOK(fsys, f.fake, plan, isCard) {
			add(MatrixFile, item, "g", "fake %q is neither an existing folder, %q, nor \"waits:\" and a plan id or a card", f.fake, providerOpen)
		}
	}

	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].File != ps[j].File {
			return ps[i].File < ps[j].File
		}
		if ps[i].Item != ps[j].Item {
			return ps[i].Item < ps[j].Item
		}
		return ps[i].Rule < ps[j].Rule
	})
	return ps
}

// providerWord returns the provider cell's word when it is one of the four
// alone, and "" otherwise.
func providerWord(cell string) string {
	switch cell {
	case providerV1, providerLater, providerDropped, providerOpen:
		return cell
	}
	return ""
}

// fakeOK reports whether a fake cell is open, "waits:" and a plan id or a
// card, or a folder that exists.
func fakeOK(fsys fs.FS, cell string, plan map[string]int, isCard map[string]bool) bool {
	if cell == providerOpen {
		return true
	}
	if id, ok := strings.CutPrefix(cell, "waits:"); ok {
		id = strings.TrimSpace(id)
		_, inPlan := plan[id]
		return inPlan || isCard[id]
	}
	dir := strings.TrimSuffix(cell, "/")
	if !fs.ValidPath(dir) || dir == "." {
		return false
	}
	info, err := fs.Stat(fsys, dir)
	return err == nil && info.IsDir()
}

// readDesignNeeds reads every design of the design folder that has a
// contracts section.
func readDesignNeeds(fsys fs.FS) (map[needKey]bool, error) {
	entries, err := fs.ReadDir(fsys, DesignDir)
	if err != nil {
		return nil, err
	}
	needs := map[needKey]bool{}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".md" {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(DesignDir, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, id := range parseDesignNeeds(string(data)) {
			needs[needKey{id, e.Name()}] = true
		}
	}
	return needs, nil
}

func sortedKeys(m map[needKey]bool) []needKey {
	keys := make([]needKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].design != keys[j].design {
			return keys[i].design < keys[j].design
		}
		return keys[i].id < keys[j].id
	})
	return keys
}
