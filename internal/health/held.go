package health

import (
	"fmt"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

func init() {
	registerRows(610, "hooks held back", func(r *Report, in *inspection) {
		if check, ok := heldCheck(in.d, in.now()); ok {
			r.Checks = append(r.Checks, check)
		}
	})
}

// heldCheck is the row of what the hooks of both hosts held back in the
// week ending today, by kind: what a hook kept only the event's name of, or
// nothing, while transcript recovery had not read to the end of the chat's
// transcript, and apart, while a chat was off the record. A chat's brief is
// counted at each run that held it back while it was still owed, its start
// and its prompts, so briefs count runs and not chats. It is for
// information and never FAIL. A week with no run recorded by a hook that
// keeps weeks gives no row.
func heldCheck(d paths.Dirs, now time.Time) (Check, bool) {
	w := hookWeek(d, now)
	if w.Runs == 0 {
		return Check{}, false
	}
	detail := fmt.Sprintf("of %d hook %s in the past 7 days: ", w.Runs, plural(w.Runs, "run", "runs"))
	kinds := [][2]string{{"prompt", "prompts"}, {"reply", "replies"}, {"tool line", "tool lines"}, {"brief", "briefs"}}
	behind, off := counted(w.Behind, kinds), counted(w.OffRecord, kinds)
	switch {
	case behind == "" && off == "":
		detail += "none held anything back"
	case off == "":
		detail += "held back while transcript recovery was behind: " + behind
	case behind == "":
		detail += "nothing held back while transcript recovery was behind; held back while a chat was off the record: " + off
	default:
		detail += "held back while transcript recovery was behind: " + behind + "; while a chat was off the record: " + off
	}
	if w.Behind[3]+w.OffRecord[3] > 0 {
		detail += " (a brief still owed is counted at each run that held it back)"
	}
	return Check{Name: "hooks held back", Status: "PASS", Detail: detail}, true
}
