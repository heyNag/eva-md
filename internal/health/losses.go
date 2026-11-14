package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
	"github.com/heyNag/eva-md/internal/paths"
)

// lossDays is the week the counted losses are read over.
const lossDays = 7 * 24 * time.Hour

// A loss is what a host's capture lost in the past 7 days, counted: the
// events its hooks held back or kept as a name only while transcript
// recovery was behind (CT-1's counts of prompts, replies and tool lines;
// a brief held back is a delivery, not a capture, and a chat off the
// record is no loss), of its hook runs; and its closed turns with a prompt
// and no stored reply, of its closed turns with a prompt, in the session
// files changed in those days. Unread counts the session files that could
// not be read, and Listed is false when the inbox could not be listed, so
// no turn was counted. Counts only.
type loss struct {
	Held, Runs      int
	NoReply, Closed int
	Unread          int
	Listed          bool
}

// pastBar reports n of of past the bar of 0.5%; n at the bar is not.
func pastBar(n, of int) bool { return of > 0 && n*200 > of }

// past reports a loss past its bar, by either count.
func (l loss) past() bool { return pastBar(l.Held, l.Runs) || pastBar(l.NoReply, l.Closed) }

// countLosses counts each host's losses at now. It reads the hooks'
// heartbeats and, through the ledger's reader of v1's inbox, the session
// files changed in the past 7 days; it writes nothing.
func countLosses(ctx context.Context, d paths.Dirs, now time.Time) map[string]loss {
	out := map[string]loss{}
	for _, host := range updateHosts {
		var l loss
		for _, ev := range hostEvents(host) {
			for _, day := range eventWeek(d, host, ev, now) {
				l.Runs += day.Runs
				for i, kind := range HeldKinds {
					if kind != "brief" {
						l.Held += day.Behind[i]
					}
				}
			}
		}
		out[host] = l
	}
	reader := inboxv1.Backend{Root: d.Inbox()}
	keys, err := reader.Sessions(ctx)
	if err != nil {
		return out
	}
	for host, l := range out {
		l.Listed = true
		out[host] = l
	}
	for _, k := range keys {
		l, ok := out[k.Host]
		if !ok {
			continue
		}
		info, err := os.Lstat(filepath.Join(d.Inbox(), k.Folder, k.File))
		switch {
		case err != nil:
			l.Unread++
		case now.Sub(info.ModTime()) > lossDays:
			// Not changed in the days the row counts: never read, so not
			// one the row failed to read.
		case ctx.Err() != nil:
			// The check's time ran out: what is left of the days the row
			// counts is counted as not read, so the row says its count is
			// short.
			l.Unread++
		default:
			if _, events, err := reader.Read(ctx, k); err != nil {
				l.Unread++
			} else {
				closed, noReply := turnLosses(events)
				l.Closed += closed
				l.NoReply += noReply
			}
		}
		out[k.Host] = l
	}
	return out
}

// turnLosses counts a session's closed turns with a prompt, and those with
// no stored reply. A turn is closed when a later turn opened, or it holds a
// reply, the stop hook's mark of a missing reply, a failed stop, an
// interrupt or the chat's end. A turn the host failed or that was
// interrupted had no reply to store, so it is closed and no loss. Only the
// chat's own turns count: a subagent's lines, and what the reader
// withholds, are passed over.
func turnLosses(events []event.Event) (closed, noReply int) {
	type turn struct{ prompt, reply, ended, excused bool }
	var order []*turn
	turns := map[int]*turn{}
	for _, e := range events {
		if e.Turn == 0 || e.Agent != "" || e.Has(event.MarkWithheld) {
			continue
		}
		t := turns[e.Turn]
		if t == nil {
			t = &turn{}
			turns[e.Turn] = t
			order = append(order, t)
		}
		name, _, _ := strings.Cut(e.Text, ":")
		switch {
		case e.Kind == event.KindPrompt && e.Copy == 0:
			t.prompt = true
		case e.Kind == event.KindReply:
			t.reply = true
		case e.Kind == event.KindLife && (name == "stop_failure" || name == "interrupt"):
			t.ended, t.excused = true, true
		case e.Kind == event.KindLife && name == "session_end":
			t.ended = true
		case e.Kind == event.KindGap && e.Text == "gap: missing final reply":
			t.ended = true
		}
	}
	for i, t := range order {
		if !t.prompt || !t.reply && !t.ended && i == len(order)-1 {
			continue
		}
		closed++
		if !t.reply && !t.excused {
			noReply++
		}
	}
	return closed, noReply
}

// share words n of of as a percentage to three significant digits, so a
// share just past the bar of 0.5% reads past it, as 0.503%.
func share(n, of int) string {
	return fmt.Sprintf("%.3g%%", 100*float64(n)/float64(of))
}

// lossRow is a host's row of its counted losses: FAIL past the bar of
// 0.5%; UNKNOWN when its hook runs say nothing past the bar and its turns
// could not be read; PASS otherwise.
func lossRow(host string, l loss) Check {
	check := Check{Name: host + " capture losses", Status: "PASS"}
	var parts []string
	if l.Runs == 0 {
		parts = append(parts, "no hook run recorded in the past 7 days")
	} else {
		parts = append(parts, fmt.Sprintf("in the past 7 days %d %s held back or kept as a name only in %d hook %s (%s)", l.Held, plural(l.Held, "event was", "events were"), l.Runs, plural(l.Runs, "run", "runs"), share(l.Held, l.Runs)))
	}
	switch {
	case !l.Listed:
		parts = append(parts, "the inbox could not be listed, so no turn was counted")
	case l.Closed == 0:
		parts = append(parts, "no closed turn with a prompt in the sessions changed in the past 7 days")
	default:
		parts = append(parts, fmt.Sprintf("%d of %d closed turns with a prompt have no stored reply (%s)", l.NoReply, l.Closed, share(l.NoReply, l.Closed)))
	}
	if l.Unread > 0 {
		parts = append(parts, fmt.Sprintf("%d session %s could not be read, so the turns counted are a floor", l.Unread, plural(l.Unread, "file", "files")))
	}
	check.Detail = strings.Join(parts, "; ") + "; the bar of each is 0.5%"
	switch {
	case l.past():
		check.Status = "FAIL"
		check.Fix = "check that eva-md's hooks are enabled and trusted in the host, and that its state folder is private and writable, then start a new chat"
	case !l.Listed:
		check.Status = "UNKNOWN"
	}
	return check
}
