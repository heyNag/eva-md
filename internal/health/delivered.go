package health

import (
	"fmt"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

func init() {
	registerRows(620, "hooks delivered", func(r *Report, in *inspection) {
		if check, ok := deliveredCheck(in.d, in.now()); ok {
			r.Checks = append(r.Checks, check)
		}
	})
}

// deliveredCheck is the row of what the hooks of both hosts added to chats
// in the week ending today: the runs that could add text, by kind; how
// many added some, and how many characters; how many added each route;
// the share of chat starts, those that did not run out of time, whose
// state was older than the newest captured exchange or that had an update
// pending; the first-prompt fallbacks; and the runs that passed their
// deadline and delivered nothing. It is for information and never FAIL. A
// week with no such run recorded gives no row.
func deliveredCheck(d paths.Dirs, now time.Time) (Check, bool) {
	w := hookWeek(d, now)
	could := 0
	for _, n := range w.Kinds {
		could += n
	}
	if could == 0 {
		return Check{}, false
	}
	kinds := [][2]string{{"startup", "startups"}, {"clear", "clears"}, {"resume", "resumes"}, {"fork", "forks"}, {"compaction", "compactions"}, {"prompt", "prompts"}}
	detail := fmt.Sprintf("of %d hook %s in the past 7 days that could add text to a chat (%s), %d added %d %s",
		could, plural(could, "run", "runs"), counted(w.Kinds, kinds), w.Added, w.Chars, plural(w.Chars, "character", "characters"))
	routes := [][2]string{{"rules", "rules"}, {"constraints", "constraints"}, {"state", "state"}, {"exchanges", "exchanges"}, {"recalled statements", "recalled statements"}, {"pointers", "pointers"}, {"reminder", "reminder"}}
	var by []string
	for i, n := range w.Routes {
		if n > 0 {
			by = append(by, fmt.Sprintf("%s %d", routes[i][0], n))
		}
	}
	if len(by) > 0 {
		detail += "; runs that added each: " + strings.Join(by, ", ")
	}
	if w.Starts > 0 {
		detail += fmt.Sprintf("; of %d chat %s, %d (%d%%) showed a state older than the newest captured exchange or an update pending (%d stale, %d pending)",
			w.Starts, plural(w.Starts, "start", "starts"), w.StaleOrPending, (w.StaleOrPending*100+w.Starts/2)/w.Starts, w.Stale, w.Pending)
	}
	if w.Fallback > 0 {
		detail += fmt.Sprintf("; %d %s came with the chat's first prompt", w.Fallback, plural(w.Fallback, "brief", "briefs"))
	}
	if w.Late > 0 {
		detail += fmt.Sprintf("; %d %s passed the deadline and delivered nothing", w.Late, plural(w.Late, "run", "runs"))
	}
	return Check{Name: "hooks delivered", Status: "PASS", Detail: detail}, true
}

// counted words counts by the names in words, leaving out those of none.
func counted(counts []int, words [][2]string) string {
	var parts []string
	for i, n := range counts {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, words[i][0], words[i][1])))
		}
	}
	return strings.Join(parts, ", ")
}
