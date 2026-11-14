package health

import (
	"fmt"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/update"
)

// budgetDetail is what a host's updates row adds, with the update's switch
// token_budget on, to say what its updates spent today, from the run-log
// records the row reads: "; about N thousand weighted tokens today, P% of
// the day's budget", the share rounded down, so it says 100% only once the
// budget is reached. Off, it adds nothing.
func budgetDetail(c config.Config, records []update.Record, host string, now time.Time) string {
	if !update.TokenBudget.On(c) {
		return ""
	}
	spent, budget := update.Spent(records, host, now).Weighted(), c.TokenBudget()
	return fmt.Sprintf("; about %d thousand weighted tokens today, %d%% of the day's budget", (spent+500)/1000, spent*100/budget)
}

// HostCost is what one host's updates spent over the 7 days eva-md status
// --updates reads: tokens, each count apart and weighted; model calls,
// retries included, and the passes that made them; the chats organized,
// those with a pass that saved a page or found nothing to change, and the
// weighted tokens per chat organized; and how many passes found nothing to
// change, and how many ran below the effort they asked for. Counts only.
type HostCost struct {
	Host        string `json:"host"`
	TokensIn    int64  `json:"tokens_in"` // uncached
	CachedIn    int64  `json:"tokens_cached_in"`
	TokensOut   int64  `json:"tokens_out"`
	Weighted    int64  `json:"weighted_tokens"`
	Calls       int64  `json:"calls"`
	Passes      int64  `json:"passes"`
	Organized   int64  `json:"organized"`
	PerChat     int64  `json:"weighted_per_organized"`
	NoChange    int64  `json:"no_change"`
	BelowEffort int64  `json:"below_effort"`
}

// costSection is the cost part of eva-md status --updates, with the
// update's switch token_budget on: each host's HostCost over records, the
// 7 days' run log. Off, it is nil, and the report is as before.
func costSection(c config.Config, records []update.Record, now time.Time) []HostCost {
	if !update.TokenBudget.On(c) {
		return nil
	}
	out := []HostCost{}
	for _, host := range updateHosts {
		w := update.WeekOf(records, host, now)
		out = append(out, HostCost{Host: host, TokensIn: w.In, CachedIn: w.CachedIn, TokensOut: w.Out, Weighted: w.Weighted(),
			Calls: w.Calls, Passes: w.Passes, Organized: w.Organized, PerChat: w.PerChat(), NoChange: w.NoChange, BelowEffort: w.BelowEffort})
	}
	return out
}

// costLines words the cost section, a line a host.
func costLines(hosts []HostCost) []string {
	var lines []string
	for _, h := range hosts {
		lines = append(lines, fmt.Sprintf("cost, %s: tokens %d in, %d cached in, %d out, %d weighted; %d model %s in %d %s; "+
			"%d %s organized, %d weighted tokens a chat; unchanged %s, below the effort asked %s",
			h.Host, h.TokensIn, h.CachedIn, h.TokensOut, h.Weighted, h.Calls, plural(int(h.Calls), "call", "calls"), h.Passes, plural(int(h.Passes), "pass", "passes"),
			h.Organized, plural(int(h.Organized), "chat", "chats"), h.PerChat, share(h.NoChange, h.Passes), share(h.BelowEffort, h.Passes)))
	}
	return lines
}

// share is n of all as a percentage rounded to the nearest, or none when
// all is 0.
func share(n, all int64) string {
	if all <= 0 {
		return "none"
	}
	return fmt.Sprintf("%d%%", (n*200+all)/(2*all))
}
