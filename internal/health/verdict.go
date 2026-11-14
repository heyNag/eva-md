package health

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// With the switch health on, eva-md status and doctor give each stage's lag
// beside its bar, each host's counted losses, and, once every other row is
// in, a verdict: a loss past its bar leaves none of its host's capture rows
// PASS, a host nobody uses gives one row in place of all of its own, and
// the verdict names a loss past its bar, else the stage furthest past its
// bar. With the switch off, these add and change nothing.
func init() {
	registerRows(1300, "stages", func(r *Report, in *inspection) {
		c, _ := in.config()
		if !Switch.On(c) {
			return
		}
		now := in.now()
		in.staged = true
		in.stages = Stages(in.ctx, in.d, c, now)
		in.losses = countLosses(in.ctx, in.d, now)
		for _, s := range in.stages {
			switch s.Name {
			case StageActivity:
				continue
			case StageCapture:
				r.Checks = append(r.Checks, stageRow(s), lossRow(s.Host, in.losses[s.Host]))
			default:
				r.Checks = append(r.Checks, stageRow(s))
			}
		}
	})
	registerRows(1390, "verdict", func(r *Report, in *inspection) {
		if !in.staged {
			return
		}
		now := in.now()
		var lost, unused []string
		for _, host := range updateHosts {
			if in.losses[host].past() {
				lost = append(lost, host)
			}
			if unusedHost(in.d, host, in.stages, now) {
				unused = append(unused, host)
			}
		}
		markLosses(r, lost)
		for _, host := range unused {
			collapseHost(r, host)
		}
		r.Verdict = verdict(in.stages, in.losses, unused)
	})
}

// stageRow is a stage's row, <host> <stage> lag: the lag beside the bar,
// past the bar when it is, the reason, and what waits. PASS within the
// budget, FAIL past it, unless the stage is off; UNKNOWN when the evidence
// could not be read, saying which.
func stageRow(s Stage) Check {
	name := s.Name + " lag"
	if s.Host != "" {
		name = s.Host + " " + name
	}
	if s.Unread != "" {
		return Check{Name: name, Status: "UNKNOWN", Detail: "the lag can't be told: " + s.Unread + "; bar " + span(s.Bar)}
	}
	detail := "no lag"
	if s.Lag > 0 {
		detail = "lag " + span(s.Lag)
	}
	detail += ", bar " + span(s.Bar)
	if s.Lag > s.Bar {
		detail += ", past its bar"
		if s.Lag > s.Budget {
			detail += " and its budget of " + span(s.Budget)
		}
		if s.Reason != "" {
			detail += ": " + s.Reason
		}
	}
	if words := waitingWords(s); words != "" {
		detail += "; " + words
	}
	for _, note := range s.Notes {
		detail += "; " + note
	}
	if s.Lag > s.Budget && !s.Off {
		return Check{Name: name, Status: "FAIL", Detail: detail, Fix: stageFix(s)}
	}
	return Check{Name: name, Status: "PASS", Detail: detail}
}

// waitingWords says what waits at a stage.
func waitingWords(s Stage) string {
	n := s.Waiting
	switch s.Name {
	case StageCapture:
		if s.Off {
			return ""
		}
		if s.Lag > 0 {
			return "the host's newest chat is newer than its newest hook run"
		}
		return "no chat newer than the host's newest hook run"
	case StageOrganizing:
		if n == 0 {
			return "no session waits"
		}
		return fmt.Sprintf("%d %s", n, plural(n, "session waits", "sessions wait"))
	case StageCommit:
		return fmt.Sprintf("%d %s written and not yet committed", n, plural(n, "file", "files"))
	case StagePush:
		return fmt.Sprintf("%d %s not yet pushed", n, plural(n, "commit", "commits"))
	}
	return ""
}

// stageFix is what a stage past its budget asks for.
func stageFix(s Stage) string {
	switch s.Name {
	case StageCapture:
		return "review host plugin enablement and hook trust, then start a new chat"
	case StageOrganizing:
		return "eva-md update --pending --foreground runs the update now and shows why it fails"
	case StageCommit:
		return "eva-md commit commits them now"
	}
	return "eva-md commit --push-only pushes them now"
}

// unusedHost reports a host nobody uses: no chat in its folder and no hook
// run in the last 14 days, and no install for it in the last day. A host
// whose chat folder could not be read is not one.
func unusedHost(d paths.Dirs, host string, stages []Stage, now time.Time) bool {
	i := slices.IndexFunc(stages, func(s Stage) bool { return s.Name == StageActivity && s.Host == host })
	if i < 0 || stages[i].Unread != "" || !stages[i].Since.IsZero() {
		return false
	}
	if run := lastHookRun(d, host, now); !run.IsZero() && now.Sub(run) <= usedWithin {
		return false
	}
	at := installedAt(d, host)
	return at.IsZero() || now.Sub(at) > 24*time.Hour
}

// hostRow reports whether a row is one of host's: its name starts with the
// host's id or its install name, and a space.
func hostRow(name string, h adapter.Host) bool {
	return strings.HasPrefix(name, string(h.ID)+" ") || strings.HasPrefix(name, h.Names.InstallName+" ")
}

// collapseHost replaces every row of a host nobody uses with one PASS row,
// <App>: not used in the last 14 days, where its first row was.
func collapseHost(r *Report, host string) {
	h, ok := adapter.ByID(host)
	if !ok {
		return
	}
	one := Check{Name: h.Names.App, Status: "PASS", Detail: "not used in the last 14 days"}
	var kept []Check
	placed := false
	for _, c := range r.Checks {
		if !hostRow(c.Name, h) {
			kept = append(kept, c)
			continue
		}
		if !placed {
			kept, placed = append(kept, one), true
		}
	}
	if !placed {
		kept = append(kept, one)
	}
	r.Checks = kept
}

// markLosses leaves none of the capture rows of a host whose counted
// losses are past their bar PASS: its capture lag, capture freshness and
// hook failures rows, and the row of what the hooks held back, which both
// hosts share, fail, saying why.
func markLosses(r *Report, hosts []string) {
	for _, host := range hosts {
		h, _ := adapter.ByID(host)
		why := "; but " + h.Names.App + "'s counted capture losses are past their bar of 0.5%"
		for i, c := range r.Checks {
			switch c.Name {
			case host + " capture lag", host + " capture freshness", host + " hook failures", "hooks held back":
			default:
				continue
			}
			if c.Status == "PASS" {
				r.Checks[i].Status = "FAIL"
				r.Checks[i].Fix = "see the " + host + " capture losses row"
			}
			r.Checks[i].Detail += why
		}
	}
}

// verdict is the one line: a counted loss past its bar first, the largest
// share first; else the stage furthest past its bar, by lag over bar, with
// its lag, its bar and its reason; else that every stage is within its bar,
// naming any whose lag could not be told, and any that is off and past its
// bar, with its lag. A host nobody uses is left out, and an off stage is
// never the one furthest past its bar.
func verdict(stages []Stage, losses map[string]loss, unused []string) string {
	worst, worstShare := "", 0.0
	for _, host := range updateHosts {
		l := losses[host]
		for _, n := range [][2]int{{l.Held, l.Runs}, {l.NoReply, l.Closed}} {
			if pastBar(n[0], n[1]) {
				if s := float64(n[0]) / float64(n[1]); s > worstShare {
					worst, worstShare = host, s
				}
			}
		}
	}
	if worst != "" {
		l := losses[worst]
		var parts []string
		if pastBar(l.Held, l.Runs) {
			parts = append(parts, fmt.Sprintf("%d %s held back or kept as a name only in %d hook %s (%s)", l.Held, plural(l.Held, "event", "events"), l.Runs, plural(l.Runs, "run", "runs"), share(l.Held, l.Runs)))
		}
		if pastBar(l.NoReply, l.Closed) {
			parts = append(parts, fmt.Sprintf("%d of %d closed turns with no stored reply (%s)", l.NoReply, l.Closed, share(l.NoReply, l.Closed)))
		}
		return label(worst, StageCapture) + " is losing chats: " + strings.Join(parts, " and ") + " in the past 7 days, past the bar of 0.5%"
	}
	var furthest *Stage
	var unread, off []string
	for i, s := range stages {
		if s.Name == StageActivity || slices.Contains(unused, s.Host) {
			continue
		}
		if s.Unread != "" {
			unread = append(unread, label(s.Host, s.Name))
			continue
		}
		if s.Off {
			if s.Lag > s.Bar {
				off = append(off, label(s.Host, s.Name)+" lags "+span(s.Lag))
			}
			continue
		}
		if s.Lag > s.Bar && (furthest == nil || float64(s.Lag)/float64(s.Bar) > float64(furthest.Lag)/float64(furthest.Bar)) {
			furthest = &stages[i]
		}
	}
	if furthest != nil {
		line := label(furthest.Host, furthest.Name) + " lags " + span(furthest.Lag) + ", past its bar of " + span(furthest.Bar)
		if furthest.Reason != "" {
			line += ": " + furthest.Reason
		}
		return line
	}
	subject := "every stage"
	switch {
	case len(off) > 0 && len(unread) > 0:
		subject += " that is switched on and could be read"
	case len(off) > 0:
		subject += " that is switched on"
	case len(unread) > 0:
		subject += " that could be read"
	}
	line := subject + " is within its bar"
	if len(unread) > 0 {
		line += "; the lag of " + list(unread) + " can't be told"
	}
	if len(off) > 0 {
		line += "; switched off, so held to no bar: " + list(off)
	}
	return line
}

// label names a stage as a person reads it: the host's app and the stage,
// or the wiki's stage.
func label(host, stage string) string {
	if h, ok := adapter.ByID(host); ok {
		return h.Names.App + " " + stage
	}
	return "the wiki's " + stage
}
