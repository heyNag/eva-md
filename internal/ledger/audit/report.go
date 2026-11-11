package audit

import (
	"fmt"
	"io"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// names are the kinds as the report says them.
var names = [kinds]string{"prompts", "final replies", "tool calls"}

// Write prints the report: numbers, the hosts' names and fixed words only.
// A paired session is named by its place, newest first.
func (r Report) Write(w io.Writer) error {
	p := &printer{w: w}
	p.f("capture audit: %d sessions in %d inbox files, %d more files unreadable; %d paired with the host's own record\n", r.Sessions, r.Files, r.Unread, r.Recovery.Paired)
	p.f("coverage of the paired sessions (found, missing from the inbox, extra in it):\n")
	for k := range kinds {
		c := r.Coverage[k]
		p.f("  %s: %d found, %d missing, %d extra\n", names[k], c.Found, c.Missing, c.Extra)
	}
	for i, row := range r.Rows {
		p.f("  session %d (%s):", i+1, row.Host)
		for k := range kinds {
			c := row.Coverage[k]
			p.f(" %s %d/%d/%d", names[k], c.Found, c.Missing, c.Extra)
		}
		p.f("\n")
	}
	p.f("host's rows not compared: before a session's first stored line %d, in folders left out by design %d, too long to read %d\n", r.BeforeCapture, r.LeftOut, r.TooLong)
	p.f("ranges said off the record, counted as paused only: %d in the inbox, %d in the host's records read\n", r.PausedInbox, r.PausedHost)
	p.f("exact repeats (all, a hook's line with a recovered one):\n")
	for k, n := range [kinds]string{"prompts", "replies", "tool lines"} {
		p.f("  %s: %d, %d\n", n, r.Repeats[k].Exact, r.Repeats[k].HookAndRecovered)
	}
	g := r.Gaps
	p.f("gaps by reason: missing final reply %d, transcript record too large %d, transcript record too long %d, malformed line %d, other %d\n",
		g.MissingReply, g.TooLarge, g.TooLong, g.Malformed, g.Other)
	p.f("lines withheld: not the file's own %d, of an unknown kind %d, not confirmed %d\n", g.Untrusted, g.UnknownKind, g.Unconfirmed)
	n := r.NoReply
	p.f("turns with a prompt and no stored reply: interrupted %d, failed %d, no reply in the payload %d, hook never ran %d\n",
		n.Interrupted, n.Failed, n.NoReplyInPayload, n.HookNeverRan)
	p.f("prompts inside a voice hand-over: %d in the inbox, %d in the host's records read\n", r.VoiceInbox, r.VoiceHost)
	c := r.Classes
	p.f("inbox files by what they show: a subagent's %d, a program's start %d, an end with no start %d, no turn at all %d, none of these %d\n",
		c[event.ClassSubagent], c[event.ClassProgram], c[event.ClassUnrun], c[event.ClassHousekeeping], c[event.ClassUnknown]+c[event.ClassChat])
	v := r.Recovery
	p.f("sessions not paired with a host record: no recovery record %d, payload named none %d, path named and refused %d, path named and not written yet %d, missing for a reason an older record does not keep %d, refused now %d, gone %d, off the record now %d, off the record once with its range not found %d, unreadable %d\n",
		v.NoRecord, v.NoneNamed, v.NamedRefused, v.NotWritten, v.WhyUnknown, v.Refused, v.Gone, v.OffNow, v.OffUnplaced, v.Unreadable)
	if r.Stores == nil {
		p.f("host sessions with no eva-md file: counted only when every session is audited\n")
	}
	for _, s := range r.Stores {
		p.f("host sessions with no eva-md file on %s: %d of %d", s.Host, s.NoEvaFile, s.Files)
		if s.Folders {
			p.f(", %d of them in folders left out by design, those excluded by their remote not told", s.ByDesign)
		} else {
			p.f(", how many in folders left out by design not known")
		}
		p.f("; %d more older than the inbox\n", s.Older)
	}
	return p.err
}

// printer writes formatted lines and keeps the first error.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}
