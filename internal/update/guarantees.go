package update

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// Guarantees checks what the validator guarantees on one validated pass,
// whatever the model wrote: b is the pass's batch, u the update Validate
// returned for it against the page before, and after the page Apply made of
// them. A session older than the page leaves the Now as it was; every
// operation under the user's tag cites a user prompt; an item the user owns
// is dropped only on the user's word; done cites a tool entry or a report of
// a result; a decision is never done or answered; every date written is in
// an entry; and no id is lost, each staying in the Now or on its retired
// Timeline line. It returns a line for each one the pass broke. The
// accuracy suite's tests and its live run, cmd/live-accuracy, check every
// pass with it; the update job never calls it.
func Guarantees(before, after threadpage.Page, b Batch, u threadpage.Update) []string {
	var broken []string
	// A session older than the page leaves the Now as it was.
	if !b.AsOf.IsZero() && b.Last.Before(b.AsOf) && !reflect.DeepEqual(after.Now, before.Now) {
		broken = append(broken, "a session older than the page changed the Now")
	}
	// Every operation under the user's tag cites a user prompt; an item the
	// user owns is dropped only on the user's word; done cites a tool entry
	// or a report of a result; a decision is never done or answered; and
	// every date written appears in an entry.
	users, results := map[string]bool{}, map[string]bool{}
	var texts []string
	for i, e := range b.Entries {
		ref := (&validator{b: b}).ref(b.Entries[i])
		switch {
		case e.Role == "user" && e.Kind == "prompt":
			users[ref] = true
		case e.Role == "tool" || e.report:
			results[ref] = true
		}
		texts = append(texts, e.Text)
	}
	cites := func(op threadpage.Op, set map[string]bool) bool {
		return slices.ContainsFunc(op.Refs, func(r string) bool { return set[r] })
	}
	items := map[string]held{}
	for _, h := range heldItems(before) {
		items[h.item.ID] = h
	}
	have := entryDates(strings.Join(texts, "\n"))
	written := []string{u.Summary, u.LastChat}
	for _, op := range u.Ops {
		h := items[op.ID]
		switch {
		case userTag(op.Tag) && !cites(op, users):
			broken = append(broken, fmt.Sprintf("%s %q is tagged %s without a user prompt", op.Action, op.Text, op.Tag))
		case op.Action == "dropped" && userTag(h.item.Tag) && !cites(op, users):
			broken = append(broken, fmt.Sprintf("%s, the user's, is dropped without the user", op.ID))
		case op.Action == "done" && !cites(op, results):
			broken = append(broken, fmt.Sprintf("%s is done without a result", op.ID))
		case (op.Action == "done" || op.Action == "answered") && h.kind == "decision":
			broken = append(broken, fmt.Sprintf("decision %s is %s", op.ID, op.Action))
		}
		written = append(written, op.Text, op.Replacement, op.Reason)
	}
	for _, e := range u.Events {
		written = append(written, e.Text)
	}
	for _, d := range u.Done {
		written = append(written, d.Text)
	}
	for _, w := range written {
		for _, d := range dates(w) {
			if !slices.ContainsFunc(have, d.same) {
				broken = append(broken, fmt.Sprintf("the date in %q is in no entry", w))
			}
		}
	}
	// No id is lost: each stays in the Now or on its retired Timeline line.
	commentID := regexp.MustCompile(`<!-- i:(n[0-9]+)`)
	kept := map[string]bool{}
	for _, it := range slices.Concat(after.Now.Next, after.Now.Open, after.Now.Decisions) {
		kept[it.ID] = true
	}
	for _, g := range after.Timeline {
		for _, l := range g.Lines {
			for _, m := range commentID.FindAllStringSubmatch(l, -1) {
				kept[m[1]] = true
			}
		}
	}
	for _, it := range slices.Concat(before.Now.Next, before.Now.Open, before.Now.Decisions) {
		if it.ID != "" && !kept[it.ID] {
			broken = append(broken, fmt.Sprintf("id %s left the page", it.ID))
		}
	}
	return broken
}
