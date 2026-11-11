// Package audit scores v1's capture against the hosts' own records: for
// each session it can pair with its host transcript, the prompts, final
// replies and tool calls found in both, missing from the inbox and extra
// in it; and for every session in scope what v1's own files show. It reads
// the inbox through the ledger's v1 reader and a host transcript only where
// the session's recovery record names one that recovery's rules admit
// (internal/catchup), so it opens nothing outside the hosts' folders of
// chats. It writes nothing, and its report holds numbers only: no text, no
// session id, no path, no project or branch name. Nothing said off the
// record is counted by kind: a range said off the record counts as one
// paused range, in the inbox and in the host's record.
package audit

import (
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

// What coverage and repeats are counted for.
const (
	Prompt = iota // a prompt that reached the agent
	Reply         // a turn's final reply
	Tool          // a call of a tool v1's hooks capture
	kinds
)

// lead is how far before a session's first stored line a host's row may
// stand and still be in what v1 captured: the inbox keeps whole seconds,
// and a hook starts a little after the host writes the row it is about.
const lead = 10 * time.Second

// Options says what to audit and where.
type Options struct {
	Dirs    paths.Dirs
	Home    string        // the home whose hosts' folders hold their records
	TempDir string        // the temporary folder chats there are left out in
	Config  config.Config // for the folders left out by design
	Session string        // one session, by its id; "" for every one
	Last    int           // the newest sessions only, when above 0
}

// Coverage counts one kind's items: in the host's record and the inbox
// (Found), in the host's record only (Missing) and in the inbox only
// (Extra).
type Coverage struct{ Found, Missing, Extra int }

// Repeats counts the stored lines of one kind that say exactly what an
// earlier line of their session and turn says (Exact), and those of them
// that pair a hook's line with one recovery stored (HookAndRecovered).
type Repeats struct{ Exact, HookAndRecovered int }

// Gaps counts the gaps the inbox holds by reason, and the lines its
// reader withholds for a reason other than a pause.
type Gaps struct {
	MissingReply, TooLarge, TooLong, Malformed, Other int
	Untrusted, UnknownKind, Unconfirmed               int
}

// NoReply counts turns with a prompt and no stored reply, by cause.
type NoReply struct{ Interrupted, Failed, NoReplyInPayload, HookNeverRan int }

// Recovery counts the sessions whose host record was not read, by why,
// and those paired with it.
type Recovery struct {
	Paired       int
	NoRecord     int // no recovery record, as for a record pruned
	NoneNamed    int // no hook named a transcript
	NamedRefused int // a hook named one and recovery refused it
	Refused      int // the record names one the rules refuse now
	Gone         int // the record names one that no longer exists
	OffNow       int // the chat is off the record now: not opened
	Unreadable   int // the record or the transcript could not be read
}

// Row is one paired session's coverage.
type Row struct {
	Host     string
	Coverage [kinds]Coverage
}

// Report is the whole audit, numbers only.
type Report struct {
	Sessions      int // sessions in scope, by host and id
	Files         int // their inbox files
	Unread        int // inbox files named that the reader could not read
	Rows          []Row
	Coverage      [kinds]Coverage
	BeforeCapture int // host items before a session's first stored line
	TooLong       int // host rows too long to read
	PausedInbox   int // ranges said off the record in the inbox
	PausedHost    int // and in the host's records read
	Repeats       [kinds]Repeats
	Gaps          Gaps
	NoReply       NoReply
	VoiceInbox    int // prompts stored inside a voice hand-over
	VoiceHost     int // and in the host's records read
	Classes       map[event.Class]int
	Recovery      Recovery
	Stores        []Store // nil unless every session was audited
}

// openTranscript opens the transcript a recovery record names. Tests
// replace it to see what is opened.
var openTranscript = catchup.OpenTranscript

// file is one inbox file of a session, as the reader returned it.
type file struct {
	session event.Session
	events  []event.Event
}

// Run audits the sessions o names.
func Run(ctx context.Context, o Options) (Report, error) {
	r := Report{Classes: map[event.Class]int{}}
	reader := inboxv1.Backend{Root: o.Dirs.Inbox()}
	keys, err := reader.Sessions(ctx)
	if err != nil {
		return Report{}, err
	}
	// A session is named by its whole id, or by the first 8 characters a
	// ref keeps when no session's whole id is that.
	whole := slices.ContainsFunc(keys, func(k ledger.Key) bool { return k.Session == o.Session })
	named := func(s string) bool {
		return o.Session == "" || s == o.Session || !whole && len(o.Session) == 8 && strings.HasPrefix(s, o.Session)
	}
	type id struct{ host, session string }
	groups := map[id][]file{}
	var order []id
	var first time.Time // the inbox's oldest stored line
	for _, k := range keys {
		if !named(k.Session) {
			continue
		}
		s, events, err := reader.Read(ctx, k)
		if ctx.Err() != nil {
			return Report{}, ctx.Err()
		}
		if err != nil {
			r.Unread++
			continue
		}
		if f, _ := span(events); !f.IsZero() && (first.IsZero() || f.Before(first)) {
			first = f
		}
		g := id{k.Host, k.Session}
		if groups[g] == nil {
			order = append(order, g)
		}
		groups[g] = append(groups[g], file{s, events})
	}
	last := func(g id) time.Time {
		var t time.Time
		for _, f := range groups[g] {
			if _, l := span(f.events); l.After(t) {
				t = l
			}
		}
		return t
	}
	slices.SortStableFunc(order, func(a, b id) int { return last(b).Compare(last(a)) })
	if o.Last > 0 && len(order) > o.Last {
		order = order[:o.Last]
	}
	for _, g := range order {
		r.Files += len(groups[g])
		if err := r.session(ctx, o, g.host, g.session, groups[g]); err != nil {
			return Report{}, err
		}
	}
	r.Sessions = len(order)
	if o.Session == "" && o.Last == 0 {
		known := map[string]map[string]bool{}
		for _, k := range keys {
			if known[k.Host] == nil {
				known[k.Host] = map[string]bool{}
			}
			known[k.Host][k.Session] = true
		}
		r.Stores = stores(o, known, first)
	}
	return r, nil
}

// span is the times of the first and the last event of events that has
// one.
func span(events []event.Event) (first, last time.Time) {
	for _, e := range events {
		t, err := time.Parse(time.RFC3339Nano, e.T)
		if err != nil {
			continue
		}
		if first.IsZero() || t.Before(first) {
			first = t
		}
		if t.After(last) {
			last = t
		}
	}
	return first, last
}

// session audits one session, its inbox files and its host's record. The
// stretches of time either says were off the record are left out of every
// count on both sides, but as paused ranges: what the host's record says
// was paused is not counted from the inbox either, where a hook stored it.
func (r *Report) session(ctx context.Context, o Options, host, id string, files []file) error {
	var paused []interval
	var first time.Time
	for _, f := range files {
		r.Classes[f.session.Class]++
		p, n := pauses(f.events)
		paused = append(paused, p...)
		r.PausedInbox += n
		if a, _ := span(f.events); !a.IsZero() && (first.IsZero() || a.Before(first)) {
			first = a
		}
	}
	rec, ok, err := r.hostRecord(ctx, o, host, id)
	if err != nil {
		return err
	}
	if ok {
		r.PausedHost += rec.paused
		r.TooLong += rec.tooLong
		paused = append(paused, rec.pauses...)
	}
	var stored [kinds]map[string]int
	for k := range stored {
		stored[k] = map[string]int{}
	}
	for i := range files {
		files[i].events = slices.DeleteFunc(slices.Clone(files[i].events), func(e event.Event) bool {
			t, err := time.Parse(time.RFC3339Nano, e.T)
			return err == nil && within(t, paused)
		})
		r.inboxFile(files[i].events, &stored)
	}
	r.repeats(files)
	if !ok {
		r.noReply(files, nil)
		return nil
	}
	var found [kinds]map[string]int
	for k := range found {
		found[k] = map[string]int{}
	}
	interrupted := map[string]bool{}
	for _, it := range rec.items {
		switch {
		case within(it.at, paused):
			continue
		case !it.at.IsZero() && !first.IsZero() && it.at.Before(first.Add(-lead)):
			r.BeforeCapture++
			continue
		case it.kind == interrupt:
			interrupted[it.turn] = true
			continue
		}
		found[it.kind][it.turn]++
		if it.voice {
			r.VoiceHost++
		}
	}
	r.noReply(files, interrupted)
	row := Row{Host: host}
	for k := range kinds {
		row.Coverage[k] = match(found[k], stored[k])
		r.Coverage[k].Found += row.Coverage[k].Found
		r.Coverage[k].Missing += row.Coverage[k].Missing
		r.Coverage[k].Extra += row.Coverage[k].Extra
	}
	r.Rows = append(r.Rows, row)
	return nil
}

// hostRecord reads the host's record of the session, where its recovery
// record names one that recovery's rules admit, and counts why it did not
// otherwise. A transcript the rules refuse by its path is never opened.
func (r *Report) hostRecord(ctx context.Context, o Options, host, id string) (hostRecord, bool, error) {
	s, found, err := catchup.Record(ctx, o.Dirs, host, id)
	switch {
	case ctx.Err() != nil:
		return hostRecord{}, false, ctx.Err()
	case err != nil:
		r.Recovery.Unreadable++
		return hostRecord{}, false, nil
	case !found:
		r.Recovery.NoRecord++
		return hostRecord{}, false, nil
	case s.Missing && s.Refused:
		r.Recovery.NamedRefused++
		return hostRecord{}, false, nil
	case s.Missing:
		r.Recovery.NoneNamed++
		return hostRecord{}, false, nil
	}
	_, off, err := session.Markers{Root: o.Dirs.Sessions()}.GetContext(ctx, host, id, session.OffRecord)
	if err != nil || off || s.Blocked && s.Reason == "off_record" {
		r.Recovery.OffNow++
		return hostRecord{}, false, nil
	}
	if !catchup.Admits(s) {
		r.Recovery.Refused++
		return hostRecord{}, false, nil
	}
	f, err := openTranscript(s)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		r.Recovery.Gone++
		return hostRecord{}, false, nil
	case err != nil:
		r.Recovery.Refused++
		return hostRecord{}, false, nil
	}
	defer f.Close()
	rec, err := readHost(ctx, host, id, f, maxRow)
	if ctx.Err() != nil {
		return hostRecord{}, false, ctx.Err()
	}
	if err != nil {
		r.Recovery.Unreadable++
		return hostRecord{}, false, nil
	}
	r.Recovery.Paired++
	return rec, true, nil
}

// counted is the kind an inbox event is counted as, by coverage: a
// prompt v1 stored, as the user's or as a program's; a final reply; a tool
// line, a plan's included; and not a subagent's, a copy or a withheld one.
func counted(e event.Event) (int, bool) {
	if e.Agent != "" || e.Copy != 0 || e.Has(event.MarkWithheld) {
		return 0, false
	}
	switch e.Kind {
	case event.KindPrompt:
		return Prompt, true
	case event.KindReply:
		return Reply, true
	case event.KindTool, event.KindTasks:
		return Tool, true
	}
	return 0, false
}

// inboxFile counts what one inbox file shows: its items by turn id, its
// gaps and its voice hand-overs.
func (r *Report) inboxFile(events []event.Event, stored *[kinds]map[string]int) {
	for _, e := range events {
		if k, ok := counted(e); ok {
			stored[k][e.TID]++
			if e.Has(event.MarkWrapper) {
				r.VoiceInbox++
			}
		}
		switch {
		case e.Has(event.MarkWithheld):
			switch e.Why {
			case event.WhyUntrusted:
				r.Gaps.Untrusted++
			case event.WhyUnknownKind:
				r.Gaps.UnknownKind++
			case event.WhyUnconfirmed:
				r.Gaps.Unconfirmed++
			}
		case e.Has(event.MarkRepaired):
			r.Gaps.Malformed++
		case e.Kind == event.KindGap:
			switch e.Text {
			case "gap: missing final reply":
				r.Gaps.MissingReply++
			case "gap: transcript record too large":
				r.Gaps.TooLarge++
			case "gap: transcript record too long":
				r.Gaps.TooLong++
			default:
				r.Gaps.Other++
			}
		}
	}
}

// repeats counts the stored lines that say exactly what an earlier one of
// their session, kind, subagent and turn says. A line with no turn id is in
// the turn its file's reader gave it.
func (r *Report) repeats(files []file) {
	type origin struct{ hook, recovered bool }
	seen := map[string]origin{}
	for i, f := range files {
		for _, e := range f.events {
			if e.Has(event.MarkWithheld) || e.Has(event.MarkName) {
				continue
			}
			k := -1
			switch e.Kind {
			case event.KindPrompt:
				k = Prompt
			case event.KindReply:
				k = Reply
			case event.KindTool, event.KindTasks:
				k = Tool
			}
			if k < 0 {
				continue
			}
			turn := "t:" + e.TID
			if e.TID == "" {
				turn = "n:" + strconv.Itoa(i) + ":" + strconv.Itoa(e.Turn)
			}
			sum := sha256.Sum256([]byte(e.Text))
			key := strconv.Itoa(k) + "\x00" + e.Agent + "\x00" + turn + "\x00" + string(sum[:])
			this := origin{e.Src.Hook, e.Has(event.MarkRecovered)}
			was, ok := seen[key]
			if !ok {
				seen[key] = this
				continue
			}
			r.Repeats[k].Exact++
			if was.hook && this.recovered || was.recovered && this.hook {
				r.Repeats[k].HookAndRecovered++
			}
		}
	}
}

// noReply counts, in each inbox file, the turns with a prompt and no
// stored reply, by cause: a failed stop, an interrupt the inbox or the
// host's record shows, a stop whose payload held no reply, and otherwise
// no stop hook's line at all.
func (r *Report) noReply(files []file, interrupted map[string]bool) {
	for _, f := range files {
		type turn struct {
			tid                                         string
			prompt, reply, failed, interrupted, missing bool
		}
		var turns []*turn
		byOrdinal := map[int]*turn{}
		for _, e := range f.events {
			if e.Turn == 0 || e.Has(event.MarkWithheld) || e.Agent != "" {
				continue
			}
			t := byOrdinal[e.Turn]
			if t == nil {
				t = &turn{}
				byOrdinal[e.Turn] = t
				turns = append(turns, t)
			}
			if t.tid == "" {
				t.tid = e.TID
			}
			switch {
			case e.Kind == event.KindPrompt && e.Copy == 0:
				t.prompt = true
			case e.Kind == event.KindReply:
				t.reply = true
			case e.Kind == event.KindLife && strings.HasPrefix(e.Text, "stop_failure"):
				t.failed = true
			case e.Kind == event.KindLife && e.Text == "interrupt":
				t.interrupted = true
			case e.Kind == event.KindGap && e.Text == "gap: missing final reply":
				t.missing = true
			}
		}
		for _, t := range turns {
			switch {
			case !t.prompt || t.reply:
			case t.failed:
				r.NoReply.Failed++
			case t.interrupted || t.tid != "" && interrupted[t.tid]:
				r.NoReply.Interrupted++
			case t.missing:
				r.NoReply.NoReplyInPayload++
			default:
				r.NoReply.HookNeverRan++
			}
		}
	}
}

// interval is a stretch of time said off the record, from start to before
// end; a zero start or end leaves that side open.
type interval struct{ start, end time.Time }

// within reports a time in any of the intervals. A row with no time is
// within one whenever there is one.
func within(t time.Time, in []interval) bool {
	for _, i := range in {
		if t.IsZero() || (i.start.IsZero() || !t.Before(i.start)) && (i.end.IsZero() || t.Before(i.end)) {
			return true
		}
	}
	return false
}

// pauses returns the stretches of time an inbox file says were off the
// record, and how many ranges it holds. A range opens at a pause control,
// or at a line withheld as off the record when no control opened it, and
// closes at the next line returned; its stretch runs from the time of the
// control, or of the last line before it, to a second after the line that
// closed it, and is open where no time is known.
func pauses(events []event.Event) ([]interval, int) {
	var out []interval
	n := 0
	open := false
	var last time.Time
	var cur interval
	for _, e := range events {
		t, _ := time.Parse(time.RFC3339Nano, e.T)
		off := e.Kind == event.KindControl && e.Op == event.OpOff || e.Has(event.MarkWithheld) && e.Why == event.WhyOffRecord
		neutral := e.Has(event.MarkWithheld) && e.Why != event.WhyOffRecord
		switch {
		case off && !open:
			open, n = true, n+1
			cur = interval{start: last}
			if e.Kind == event.KindControl {
				cur.start = t
			}
		case !off && !neutral && open:
			open = false
			if !t.IsZero() {
				cur.end = t.Add(time.Second)
			}
			out = append(out, cur)
		}
		if !t.IsZero() {
			last = t
		}
	}
	if open {
		out = append(out, cur)
	}
	return out, n
}

// match pairs a kind's items of the host's record with those stored, turn
// by turn. Items with no turn id on one side pair with the other side's
// unpaired items, those with no turn id first.
func match(host, stored map[string]int) Coverage {
	var c Coverage
	missing, extra := 0, 0
	for t, h := range host {
		if t != "" {
			f := min(h, stored[t])
			c.Found += f
			missing += h - f
		}
	}
	for t, s := range stored {
		if t != "" {
			extra += s - min(host[t], s)
		}
	}
	h0, s0 := host[""], stored[""]
	f0 := min(h0, s0)
	h0, s0 = h0-f0, s0-f0
	x, y := min(h0, extra), min(s0, missing)
	c.Found += f0 + x + y
	c.Missing = missing - y + h0 - x
	c.Extra = extra - x + s0 - y
	return c
}
