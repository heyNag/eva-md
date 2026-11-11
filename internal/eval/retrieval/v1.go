package retrieval

import (
	"context"
	"fmt"

	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/systempage"
)

// v1AsOf is the day every item is dated in v1's rows, so that v1's order
// among equal scores is the set's order, not a date the set made up.
const v1AsOf = "2026-01-01"

func init() { Register("v1-recall", newV1) }

// v1 is v1's per-prompt recall as the hook runs it: knowledgepage.Recall
// over the items as v1's rows, in the chat's project by its v1 key, with
// the projects that share a system page with it as its siblings, and the
// first MaxRecall rows it returns. It skips nothing, since the set's
// events are not chats that were already given an item.
type v1 struct {
	rows  []knowledgepage.Row
	keys  map[string]string // project id: its v1 key
	pages [][]string        // each system page's project keys, as v1's repos index lists them
}

func newV1(items []Item, projects []Project) (System, error) {
	v := &v1{keys: map[string]string{}}
	byPage := map[string][]string{}
	var order []string
	for _, p := range projects {
		v.keys[p.ID] = p.Key
		for _, page := range p.Pages {
			if byPage[page] == nil {
				order = append(order, page)
			}
			byPage[page] = append(byPage[page], p.Key)
		}
	}
	for _, page := range order {
		v.pages = append(v.pages, byPage[page])
	}
	for _, it := range items {
		r := knowledgepage.Row{
			ID: it.ID, Kind: it.Type, Scope: knowledgepage.ScopeProject, AsOf: v1AsOf,
			Title: it.Title, Statement: it.Statement, Paths: it.Paths,
		}
		if it.Type == "rule" {
			// v1 has no rule: a standing instruction is a constraint there.
			r.Kind = knowledgepage.Constraint
		}
		if it.Scope == ScopeAll {
			r.Scope = knowledgepage.ScopeAll
		}
		if it.Project != "" {
			key, ok := v.keys[it.Project]
			if !ok {
				return nil, fmt.Errorf("an item's project is not a project of the set")
			}
			r.Project = key
		}
		v.rows = append(v.rows, r)
	}
	return v, nil
}

func (v *v1) Add(ctx context.Context, e Event) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, ok := v.keys[e.Project]
	if !ok {
		return nil, fmt.Errorf("the event's project is not a project of the set")
	}
	rows := knowledgepage.Recall(v.rows, e.Text, key, func() map[string]bool {
		return systempage.Siblings(v.pages, key)
	}, nil)
	var out []string
	for _, r := range rows[:min(len(rows), knowledgepage.MaxRecall)] {
		out = append(out, r.ID)
	}
	return out, nil
}
