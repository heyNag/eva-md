// Package catchup repairs missing final exchanges from registered host transcripts.
// It never discovers old chats or imports anything preceding the first hook.
package catchup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/session"
)

const maxTail = 1 << 20

var ErrPrivacyBoundary = errors.New("catchup: privacy boundary could not be persisted")
var ErrPending = errors.New("catchup: bounded recovery has more unread records")

// errPaused stops a pass that ran out of time; Recover then saves the cursor
// and returns ErrPending.
var errPaused = errors.New("catchup: pass out of time")

// errScanRow stops the window's rows at a row to be scanned rather than
// decoded; the pass goes on with the scan.
var errScanRow = errors.New("catchup: row to scan")

// redactText redacts recovered text. Tests replace it to slow it down.
var redactText = redact.Redact

// Values of a session's off-the-record marker. The user's own request, made
// through the prompt hook, writes OffRecordAsked. Recovery writes "1" when it
// finds a control a hook missed, or when it asserts a missing marker again
// for a chat already off the record. Size limits never set it.
const (
	OffRecordAsked = "asked"
	offRecordFound = "1"
)

// Gap events stand in for a row recovery could not keep, so the hole shows.
const (
	gapTooLarge = "gap: transcript record too large" // a row longer than the window
	gapTooLong  = "gap: transcript record too long"  // text that could not be redacted in time
)

// Source contains only locally registered metadata, never transcript content.
type Source struct {
	V       int         `json:"v"`
	Home    string      `json:"home"`
	Host    string      `json:"host"`
	Session string      `json:"session"`
	Path    string      `json:"path,omitempty"`
	Cwd     string      `json:"cwd"`
	Entry   inbox.Entry `json:"entry"`
	Offset  int64       `json:"offset"`
	Device  uint64      `json:"device"`
	Inode   uint64      `json:"inode"`
	Blocked bool        `json:"blocked"`
	Missing bool        `json:"missing"`
	Updated string      `json:"updated"`
	Reason  string      `json:"reason,omitempty"`
	// Skip is the scan of a row too long for the window, kept between
	// passes. It holds a few short fields, never the row's text.
	Skip *rowScan `json:"skip,omitempty"`
	// Slow marks a row whose redaction outran a pass, and how tightly the
	// next pass bounds it.
	Slow *slowRow `json:"slow,omitempty"`
}

type slowRow struct {
	Offset int64 `json:"offset"`
	Level  int   `json:"level"`
	Tries  int   `json:"tries,omitempty"`
	// Decode counts passes that began decoding this row, a big one, and did
	// not finish it. The next pass scans the row instead (see rowScan).
	Decode int `json:"decode,omitempty"`
}

// bigRow is the length from which decoding a row and checking it for a
// record control cost a noticeable part of a pass, about 35 ms for a 1 MB
// paste. Before such a row is decoded, the pass records that it began it, so
// a pass that does not finish it, because it ran out of time or the hook's
// process ended, never makes the next pass repeat the same work: that pass
// scans the row in bounded chunks instead, which honours a record control and
// leaves a gap event.
const bigRow = 256 << 10

// Register records only hook-supplied paths. The caller holds session.Lock.
// Changing paths or branches starts a new baseline; it cannot import old data.
func Register(ctx context.Context, dirs paths.Dirs, home, path, cwd string, e inbox.Entry, blocked bool, now time.Time) error {
	home = systemAliases(home)
	path = systemAliases(path)
	cwd = systemAliases(cwd)
	old, found, err := load(ctx, dirs, e.Host, e.Session)
	if err != nil {
		return err
	}
	s := Source{V: 1, Home: home, Host: e.Host, Session: e.Session, Path: path, Cwd: cwd, Entry: e, Blocked: blocked, Updated: now.Format(time.RFC3339)}
	s.Entry.Turn = ""
	s.Entry.Text = ""
	s.Entry.Role = ""
	s.Entry.Kind = ""
	if found {
		s.Offset = old.Offset
		s.Device = old.Device
		s.Inode = old.Inode
		s.Skip = old.Skip
		s.Slow = old.Slow
		s.Blocked = old.Blocked || blocked
		if s.Blocked {
			s.Reason = old.Reason
		}
	}
	if found && old.Path == path && old.Cwd == cwd && old.Entry.Project == e.Project && old.Entry.Branch == e.Branch && old.Entry.Model == e.Model && old.Blocked == s.Blocked {
		at, _ := time.Parse(time.RFC3339, old.Updated)
		if now.Sub(at) >= 0 && now.Sub(at) < 10*time.Second {
			return nil
		}
	}
	if path == "" {
		s.Missing = true
		s.Reason = "transcript_unavailable"
		if found && old.Path != "" {
			s.Path = old.Path
			s.Missing = false
			s.Reason = ""
		}
		return save(ctx, dirs, s)
	}
	f, err := openTranscript(s)
	if err != nil {
		s.Path = ""
		s.Missing = true
		s.Reason = "transcript_unavailable"
		s.Skip, s.Slow = nil, nil
		return save(ctx, dirs, s)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	dev, ino := fileID(info)
	if !found || old.Path != path || old.Entry.Project != e.Project || old.Cwd != cwd || old.Entry.Branch != e.Branch || s.Blocked || old.Device != dev || old.Inode != ino {
		s.Offset = info.Size()
		s.Skip, s.Slow = nil, nil
	}
	s.Device = dev
	s.Inode = ino
	return save(ctx, dirs, s)
}

// SetOffRecord persists the privacy boundary before capture resumes. Even if a
// later marker write fails, recovery remains blocked until this succeeds. It
// carries out the user's own request, so going off the record saves the
// reason off_record and writes the marker OffRecordAsked.
func SetOffRecord(ctx context.Context, dirs paths.Dirs, host, id string, off bool) error {
	s, found, err := load(ctx, dirs, host, id)
	if err != nil || !found {
		return err
	}
	s.Blocked = true
	if off {
		s.Reason = "off_record"
	}
	if err := save(ctx, dirs, s); err != nil {
		return err
	}
	f, err := openTranscript(s)
	if err == nil {
		info, e := f.Stat()
		f.Close()
		if e != nil {
			return e
		}
		s.Offset = info.Size()
		s.Device, s.Inode = fileID(info)
	} else if s.Path != "" {
		return err
	}
	s.Skip, s.Slow = nil, nil
	s.Blocked = off
	if !off {
		s.Reason = ""
	}
	if err := save(ctx, dirs, s); err != nil {
		return err
	}
	markers := session.Markers{Root: dirs.Sessions()}
	if !off {
		return markers.ClearContext(ctx, host, id, session.OffRecord)
	}
	return markers.SetContext(ctx, host, id, session.OffRecord, OffRecordAsked)
}

// Recover fills missing prompts and final replies under the caller's session
// lock. Each pass reads forward from the cursor: it resumes the scan of a row
// too long for the 1 MiB window, then handles at most one window of rows,
// checking ctx between rows. The cursor moves past each row once it is
// handled and is saved on every way out, so a pass that runs out of time
// returns ErrPending and the next pass resumes after the last row it
// finished. Only a record control the user gave ever takes the chat off the
// record; a size limit records a gap event instead.
func Recover(ctx context.Context, dirs paths.Dirs, host, id string) (int, error) {
	s, found, err := load(ctx, dirs, host, id)
	if err != nil || !found {
		return 0, err
	}
	markers := session.Markers{Root: dirs.Sessions()}
	if s.Blocked && s.Reason == "off_record" {
		// Asserted again when missing, as after a failed marker write; a
		// marker already set keeps its value.
		if _, set, err := markers.GetContext(ctx, host, id, session.OffRecord); err != nil || !set {
			if err := markers.SetContext(ctx, host, id, session.OffRecord, offRecordFound); err != nil {
				return 0, errors.Join(ErrPrivacyBoundary, err)
			}
		}
	}
	if s.Blocked || s.Missing || s.Path == "" {
		return 0, nil
	}
	_, off, err := markers.GetContext(ctx, host, id, session.OffRecord)
	if err != nil || off {
		return 0, err
	}
	f, err := openTranscript(s)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if s.Skip != nil && (s.Skip.Start != s.Offset || s.Skip.Pos < s.Skip.Start) {
		s.Skip = nil
	}
	if s.Slow != nil && s.Slow.Offset < s.Offset {
		s.Slow = nil
	}
	dev, ino := fileID(info)
	if dev != s.Device || ino != s.Inode || info.Size() < s.Offset || s.Skip != nil && info.Size() < s.Skip.Pos {
		return 0, errors.New("catchup: transcript replaced or truncated")
	}
	if s.Host == "codex" {
		header := make([]byte, 64<<10)
		n, _ := f.ReadAt(header, 0)
		if !codexIdentity(header[:n], s) {
			return 0, errors.New("catchup: transcript identity mismatch")
		}
	}
	r := &recovery{ctx: ctx, dirs: dirs, markers: markers, s: s, f: f, redact: redactText}
	if deadline, ok := ctx.Deadline(); ok {
		r.deadline = deadline
	}
	return r.run()
}

// recovery is one Recover pass.
type recovery struct {
	ctx      context.Context
	dirs     paths.Dirs
	markers  session.Markers
	s        Source
	f        *os.File
	known    map[string]bool
	n        int
	dirty    bool          // s differs from the saved state
	limited  bool          // the scan stopped at scanPerPass
	deadline time.Time     // zero without one
	budget   time.Duration // the time left when the window's rows began
	fresh    bool          // the current row began with most of budget left
	redact   func(string) (string, int)
}

// Bounds on one pass's reading of an oversized row. With a deadline, ctx
// usually ends the scan first; scanPerPass bounds a pass without one.
var (
	scanChunk   = 1 << 20
	scanPerPass = 64 << 20
)

func (r *recovery) run() (int, error) {
	windowed := false
	for {
		if r.ctx.Err() != nil {
			return r.pause()
		}
		if r.s.Skip != nil {
			done, err := r.scan()
			if err != nil {
				return r.fail(err)
			}
			if !done {
				if r.ctx.Err() != nil || r.limited {
					return r.pause()
				}
				// The row is still being written. Wait for its end, as for any
				// partial last row, without holding up capture.
				return r.n, r.saveIfDirty()
			}
			if err := r.finishScan(); err != nil {
				return r.fail(err)
			}
			if r.s.Blocked {
				return r.n, r.saveIfDirty()
			}
			continue
		}
		if windowed {
			// One window per pass. Capture waits for the rest rather than
			// crossing a privacy boundary that may lie in unread rows.
			if r.s.Reason != "off_record" {
				r.s.Reason = "tail_limit"
				r.dirty = true
			}
			if err := r.saveIfDirty(); err != nil {
				return r.n, err
			}
			return r.n, ErrPending
		}
		data, start, _, more, err := delta(r.f, r.s.Offset, maxTail)
		if err != nil {
			return r.n, err
		}
		if len(data) == 0 {
			if !more {
				return r.n, r.saveIfDirty()
			}
			// No row ends within the window: scan past this one in chunks.
			// The reason is only a note; nothing here blocks capture.
			r.s.Skip = &rowScan{Start: r.s.Offset, Pos: r.s.Offset, Partial: r.midRow(r.s.Offset)}
			if r.s.Reason != "off_record" {
				r.s.Reason = "oversized_row"
			}
			r.dirty = true
			continue
		}
		windowed = true
		if err := r.rows(data, start); errors.Is(err, errScanRow) {
			continue
		} else if err != nil {
			return r.fail(err)
		}
		if r.s.Blocked || !more {
			return r.n, r.saveIfDirty()
		}
	}
}

// fail ends a pass on err, keeping its progress. A pass that ran out of time
// pauses; a privacy boundary that could not be written is reported as such.
func (r *recovery) fail(err error) (int, error) {
	if !errors.Is(err, ErrPrivacyBoundary) && (errors.Is(err, errPaused) || r.ctx.Err() != nil) {
		return r.pause()
	}
	return r.n, errors.Join(err, r.saveIfDirty())
}

// rows handles the complete rows in data, which starts at offset start,
// moving the cursor past each row once it is handled.
func (r *recovery) rows(data []byte, start int64) error {
	if start != r.s.Offset {
		r.s.Offset = start // past the rest of a row that began before registration
		r.dirty = true
	}
	if !r.deadline.IsZero() {
		r.budget = time.Until(r.deadline)
	}
	p := newParser(r.s)
	for len(data) > 0 {
		if r.ctx.Err() != nil {
			return errPaused
		}
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return errors.New("catchup: incomplete read")
		}
		line := data[:i+1]
		at := r.s.Offset
		slow := r.s.Slow != nil && r.s.Slow.Offset == at
		if slow && r.s.Slow.Decode > 0 {
			// A pass began decoding this row and did not finish it.
			r.s.Skip = &rowScan{Start: at, Pos: at, Gap: gapTooLong}
			r.dirty = true
			return errScanRow
		}
		// Judged before the row's own work begins: preparing and decoding a
		// long row are part of its cost, not a sign that it began late.
		r.fresh = r.deadline.IsZero() || time.Until(r.deadline) >= r.budget*3/4
		if slow || len(line) >= bigRow {
			if err := r.beginRow(at, len(line) >= bigRow); err != nil {
				return err
			}
		}
		e, ok := p.read(line)
		if p.offRecord {
			p.offRecord = false
			if err := r.block(); err != nil {
				return err
			}
		}
		if p.blocked && !r.s.Blocked {
			r.s.Blocked = true // a Codex turn moved to another folder
			r.dirty = true
		}
		if r.s.Slow != nil && r.s.Slow.Offset == at {
			r.s.Slow.Decode = 0 // decoded; only its redaction may still outrun a pass
		}
		if ok {
			if err := r.store(e, at); err != nil {
				return err
			}
		}
		if r.s.Slow != nil && r.s.Slow.Offset == at {
			r.clearSlow()
		}
		r.s.Offset += int64(len(line))
		r.dirty = true
		data = data[i+1:]
	}
	return nil
}

// beginRow prepares to decode the row at offset at, a big one or one that
// outran an earlier pass. The entries already stored are loaded first, so
// storing the row never fails for want of time once it is decoded. Before a
// big row is decoded, the pass records that it began it and saves that, so a
// pass that does not finish it leaves the next pass to scan it instead.
func (r *recovery) beginRow(at int64, big bool) error {
	if _, err := r.knownEntries(); err != nil {
		return err
	}
	if !big {
		return nil
	}
	if r.s.Slow == nil || r.s.Slow.Offset != at {
		r.s.Slow = &slowRow{Offset: at}
	}
	r.s.Slow.Decode++
	r.dirty = true
	if err := r.save(); err != nil {
		r.s.Slow.Decode--
		if r.ctx.Err() != nil {
			return errPaused
		}
		return err
	}
	return nil
}

// clearSlow forgets the slow-row state once its row is handled.
func (r *recovery) clearSlow() {
	r.s.Slow = nil
	if r.s.Reason == "slow_row" {
		r.s.Reason = ""
	}
	r.dirty = true
}

// store keeps a recovered prompt or reply. A turn already stored is skipped
// before any redaction. Otherwise the text is redacted once, within a cost
// bound, racing the pass's deadline: a row that outruns it stays unread for
// the next pass, which bounds it more tightly, and a row that outruns every
// bound becomes a gap event. One long row can never stall recovery.
func (r *recovery) store(e inbox.Entry, at int64) error {
	known, err := r.knownEntries()
	if err != nil {
		return err
	}
	if e.Turn != "" && known["turn:"+e.Turn+":"+e.Role] {
		return nil
	}
	level := 0
	if r.s.Slow != nil && r.s.Slow.Offset == at {
		level = r.s.Slow.Level
	}
	out := redaction{gap: true}
	if level >= 0 && level < len(levels) {
		var done bool
		if out, done = redactWithin(r.ctx, e.Text, levels[level], r.redact); !done {
			r.slowed(at, level, r.fresh)
			return errPaused
		}
	}
	if r.s.Slow != nil && r.s.Slow.Offset == at {
		r.clearSlow()
	}
	if out.gap {
		return r.recordGap(gapTooLong, e.TS)
	}
	e.Text, e.Red, e.Trunc = out.text, out.red, out.trunc
	return r.add(e)
}

// slowed records that the row at offset at outran the pass. It is bounded
// more tightly next time if it began with most of the pass's budget left
// (fresh), so it is too costly for any pass at its current bound, or after a
// few tries that each began late in a pass.
func (r *recovery) slowed(at int64, level int, fresh bool) {
	if r.s.Slow == nil || r.s.Slow.Offset != at {
		r.s.Slow = &slowRow{Offset: at, Level: level}
	}
	r.s.Slow.Decode = 0 // the row was decoded; its redaction outran the pass
	r.s.Slow.Tries++
	if fresh || r.s.Slow.Tries >= 3 {
		r.s.Slow.Level = level + 1
		r.s.Slow.Tries = 0
	}
	if r.s.Reason != "off_record" {
		r.s.Reason = "slow_row"
	}
	r.dirty = true
}

// block persists an off-the-record boundary found in a row the hook missed.
// The state is blocked first, so the boundary holds even when the marker
// can't be written; Recover asserts the marker again on the next pass.
func (r *recovery) block() error {
	r.s.Blocked = true
	r.s.Reason = "off_record"
	r.dirty = true
	ctx, cancel := r.grace()
	defer cancel()
	if err := r.markers.SetContext(ctx, r.s.Host, r.s.Session, session.OffRecord, offRecordFound); err != nil {
		return errors.Join(ErrPrivacyBoundary, err)
	}
	return nil
}

// recordGap appends a fixed gap event in place of a row, at the row's time.
func (r *recovery) recordGap(text, ts string) error {
	e := r.s.Entry
	e.Origin = "catchup"
	e.Role, e.Kind, e.Text, e.Turn = "event", "event", text, ""
	e.Red, e.Trunc = 0, 0
	e.TS = ts
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return r.add(e)
}

// add appends an entry whose text is already redacted, unless the same turn
// or text is already stored. Once a row's text is redacted, the append gets a
// short grace past the deadline, so the work is not thrown away.
func (r *recovery) add(e inbox.Entry) error {
	known, err := r.knownEntries()
	if err != nil {
		return err
	}
	keys := entryKeys(e)
	duplicate := known[keys[0]]
	if e.Turn != "" {
		duplicate = known[keys[1]] || known["unknown:"+keys[0]]
	}
	if !duplicate {
		ctx, cancel := r.grace()
		err := (inbox.Store{Root: r.dirs.Inbox()}).AppendRedacted(ctx, e)
		cancel()
		if err != nil {
			return err
		}
		if e.Role != "event" {
			r.n++
		}
	}
	for _, key := range keys {
		known[key] = true
	}
	if e.Turn == "" {
		known["unknown:"+keys[0]] = true
	}
	return nil
}

func (r *recovery) knownEntries() (map[string]bool, error) {
	if r.known == nil {
		known, err := knownEntries(r.ctx, r.dirs, r.s.Entry)
		if err != nil {
			if r.ctx.Err() != nil {
				return nil, errPaused
			}
			return nil, err
		}
		r.known = known
	}
	return r.known, nil
}

// scan resumes the scan of the oversized row at the cursor and reports
// whether the row's end was reached.
func (r *recovery) scan() (bool, error) {
	z := r.s.Skip
	if z.Done {
		return true, nil // scanned, but its gap event is still to be written
	}
	buf := make([]byte, scanChunk)
	for read := 0; ; {
		if r.ctx.Err() != nil {
			return false, nil
		}
		if read >= scanPerPass {
			r.limited = true
			return false, nil
		}
		n, err := r.f.ReadAt(buf, z.Pos)
		if err != nil && err != io.EOF {
			return false, err
		}
		chunk := buf[:n]
		if i := bytes.IndexByte(chunk, '\n'); i >= 0 {
			if !z.Partial {
				z.feed(chunk[:i])
			}
			z.Pos += int64(i + 1)
			z.Done = true
			r.dirty = true
			return true, nil
		}
		if !z.Partial {
			z.feed(chunk)
		}
		z.Pos += int64(n)
		read += n
		if n > 0 {
			r.dirty = true
		}
		if n < len(buf) {
			return false, nil // the end of the transcript
		}
	}
}

// finishScan acts on a fully scanned oversized row and moves past it. A
// control the user gave is honoured as in any row; anything else leaves one
// gap event, so the brief shows the hole and capture goes on.
func (r *recovery) finishScan() error {
	z := r.s.Skip
	control, ts := z.verdict(r.s)
	switch {
	case z.Partial || control == "on_record":
		// The rest of a row from before registration, or a control recovery
		// never acts on: it never turns capture back on by itself.
	case control == "off_record":
		if r.s.Slow != nil && r.s.Slow.Offset == z.Start {
			r.clearSlow()
		}
		r.s.Skip, r.s.Offset, r.dirty = nil, z.Pos, true
		return r.block()
	default:
		// Written before the cursor moves on, so the hole is always marked.
		gap := z.Gap
		if gap == "" {
			gap = gapTooLarge
		}
		if err := r.recordGap(gap, ts); err != nil {
			return err
		}
	}
	if r.s.Slow != nil && r.s.Slow.Offset == z.Start {
		r.clearSlow()
	}
	r.s.Skip, r.s.Offset, r.dirty = nil, z.Pos, true
	if r.s.Reason == "oversized_row" {
		r.s.Reason = ""
	}
	return nil
}

// midRow reports whether offset falls inside a row, after a cursor taken from
// the file's size while the host was writing.
func (r *recovery) midRow(offset int64) bool {
	if offset == 0 {
		return false
	}
	var prev [1]byte
	_, err := r.f.ReadAt(prev[:], offset-1)
	return err == nil && prev[0] != '\n'
}

// pause saves the cursor and reports that more remains, so the caller defers
// what it would store after the unread rows.
func (r *recovery) pause() (int, error) {
	if err := r.saveIfDirty(); err != nil {
		return r.n, errors.Join(ErrPending, err)
	}
	return r.n, errors.Join(ErrPending, r.ctx.Err())
}

func (r *recovery) save() error {
	ctx, cancel := r.grace()
	defer cancel()
	if err := save(ctx, r.dirs, r.s); err != nil {
		return err
	}
	r.dirty = false
	return nil
}

func (r *recovery) saveIfDirty() error {
	if !r.dirty {
		return nil
	}
	return r.save()
}

// grace is how long finishing a row and saving the cursor may take past the
// pass's deadline, or past the moment the pass noticed it was out of time,
// whichever is later, so the progress of a pass that ran out of time is kept
// even when a single step, such as decoding a long row, overran the deadline.
const grace = 50 * time.Millisecond

func (r *recovery) grace() (context.Context, context.CancelFunc) {
	if r.deadline.IsZero() {
		return context.WithCancel(r.ctx)
	}
	return context.WithDeadline(context.WithoutCancel(r.ctx), later(r.deadline, time.Now()).Add(grace))
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// Bounded redaction

// limits bounds the redaction of one recovered text: text up to full bytes is
// redacted whole, and longer text keeps a head and a tail window.
type limits struct{ full, head, tail int }

// levels are the bounds a row gets: the first for every row, then tighter
// ones for a row that outran a pass. The first keeps what the inbox keeps,
// the whole text up to 200 KiB, so a reply whose redaction fits the pass is
// kept whole, as the hook would have kept it. redact.Redact runs both the
// baseline and the current rules, so its cost per KiB is about the sum of
// the two, and the longest texts at this bound can outrun a 100 ms hook
// budget. A text that outruns it keeps a head and a tail window at the
// second bound, and the third needs a fraction of that budget. A row that
// outruns all of them becomes a gap event.
var levels = []limits{
	{full: 200 << 10, head: 96 << 10, tail: 96 << 10},
	{full: 48 << 10, head: 22 << 10, tail: 22 << 10},
	{full: 16 << 10, head: 6 << 10, tail: 6 << 10},
}

// containsBound is the longest text Contains redacts for its text key, which
// it does in the hook's own time. Text keys of one turn rarely match across
// sources anyway, as each holds its own source's timestamp.
const containsBound = 48 << 10

// syncRedact is the longest text redacted without a goroutine to race the
// deadline; it takes a few milliseconds at most.
const syncRedact = 8 << 10

// windowContext is how much text before the tail window is redacted with it
// and then dropped, so a secret whose trigger word sits just before the
// window, as in "token:" on the line above, is still caught. The entropy rule
// looks at most 256 bytes past a trigger word.
const windowContext = 4 << 10

// redaction is a recovered text after bounded redaction. gap means no part of
// it could be kept, and a fixed gap event is stored instead.
type redaction struct {
	text  string
	red   int
	trunc int
	gap   bool
}

// redactWithin runs boundedRedact, giving up when ctx ends. The redactor
// can't be interrupted, so a long text runs in its own goroutine, which is
// abandoned if the pass runs out of time; it only computes a string.
func redactWithin(ctx context.Context, text string, lim limits, redactor func(string) (string, int)) (redaction, bool) {
	if len(text) <= syncRedact {
		return safeRedact(text, lim, redactor), true
	}
	if ctx.Err() != nil {
		return redaction{}, false
	}
	done := make(chan redaction, 1)
	go func() { done <- safeRedact(text, lim, redactor) }()
	select {
	case out := <-done:
		return out, true
	case <-ctx.Done():
		select {
		case out := <-done:
			return out, true
		default:
			return redaction{}, false
		}
	}
}

// safeRedact turns a redactor panic into a gap, so a row the redactor can't
// handle is passed over instead of stopping recovery at it on every pass.
func safeRedact(text string, lim limits, redactor func(string) (string, int)) (out redaction) {
	defer func() {
		if recover() != nil {
			out = redaction{gap: true}
		}
	}()
	return boundedRedact(text, lim, redactor)
}

// boundedRedact redacts text once, with a cost that does not grow with the
// text. Text over lim.full keeps a head and a tail window, cut at line
// boundaries, and drops the middle, as the inbox does for text over its own
// limit. The rules of both of the redactor's rule sets match within a line,
// with two exceptions, so each window is redacted as the whole text would be:
//   - private key blocks span lines. They are replaced first, as redact.Redact
//     replaces them first, so no cut lands inside one;
//   - the entropy rule looks past a trigger word, across lines. The tail is
//     redacted with windowContext bytes before it, and those lines are then
//     dropped by counting newlines, which no remaining rule removes. If the
//     count changes anyway, the tail is dropped rather than risk a cut.
//
// Positions are taken on the text the redactor sees, after its cleaning.
// When neither window holds a whole line, nothing is kept and the result is a
// gap.
func boundedRedact(text string, lim limits, redactor func(string) (string, int)) redaction {
	if len(text) <= lim.full {
		out, n := redactor(text)
		return redaction{text: out, red: n}
	}
	text = cleanLikeRedact(text)
	keys := 0
	if strings.Contains(text, "PRIVATE KEY") {
		text, keys = replacePrivateKeys(text)
	}
	if len(text) <= lim.full {
		out, n := redactor(text)
		return redaction{text: out, red: n + keys}
	}
	// lim.full covers both windows and the context, so they don't overlap.
	headEnd := strings.LastIndexByte(text[:lim.head], '\n') + 1
	tailStart := len(text)
	if i := strings.IndexByte(text[len(text)-lim.tail:], '\n'); i >= 0 {
		tailStart = len(text) - lim.tail + i + 1
	}
	var b strings.Builder
	red := keys
	if headEnd > 0 {
		out, n := redactor(text[:headEnd])
		b.WriteString(out)
		red += n
	}
	if tailStart < len(text) {
		from := max(headEnd, tailStart-windowContext)
		window := text[from:]
		out, n := redactor(window)
		if strings.Count(out, "\n") == strings.Count(window, "\n") {
			b.WriteString(out[afterLines(out, strings.Count(text[from:tailStart], "\n")):])
			red += n
		} else {
			tailStart = len(text)
		}
	}
	if b.Len() == 0 {
		return redaction{gap: true}
	}
	return redaction{text: b.String(), red: red, trunc: tailStart - headEnd}
}

// afterLines returns the index just past the nth newline in s.
func afterLines(s string, n int) int {
	i := 0
	for ; n > 0; n-- {
		j := strings.IndexByte(s[i:], '\n')
		if j < 0 {
			return len(s)
		}
		i += j + 1
	}
	return i
}

// cleanLikeRedact repeats the cleaning redact.Redact does before its rules
// (see clean in internal/redact): it drops NUL, format characters and ANSI
// escape sequences. Cleaning twice changes nothing, so the redactor then
// sees exactly the text whose positions the windows were cut at.
func cleanLikeRedact(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == 0 || unicode.Is(unicode.Cf, r) || r == 0x034F {
			return -1
		}
		return r
	}, s)
	if strings.ContainsAny(s, "\x1b\u009b") {
		s = ansiEscape.ReplaceAllString(s, "")
		s = strings.NewReplacer("\x1b", "", "\u009b", "").Replace(s)
	}
	return s
}

// ansiEscape and privateKeyBlock repeat patterns from internal/redact.
var (
	ansiEscape      = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[@-Z\\\\-_]|\u009b[0-?]*[ -/]*[@-~]")
	privateKeyBlock = regexp.MustCompile(`-----BEGIN[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----(?s:.*?)(?:-----END[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|\z)`)
)

// replacePrivateKeys replaces private key blocks with the marker redact.Redact
// writes for them.
func replacePrivateKeys(s string) (string, int) {
	n := 0
	s = privateKeyBlock.ReplaceAllStringFunc(s, func(string) string {
		n++
		return "[REDACTED:private_key]"
	})
	return s, n
}

// RecoverProject visits only previously registered idle sessions in this
// project, at most five per call. It never scans the host's transcript stores.
func RecoverProject(ctx context.Context, dirs paths.Dirs, project, excludeHost, excludeID string, now time.Time) (int, error) {
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "catchup"), false)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer root.Close()
	var sources []Source
	for _, host := range []string{"claude-code", "codex"} {
		d, err := root.Open(host)
		if err != nil {
			continue
		}
		items, _ := d.ReadDir(256)
		d.Close()
		for _, item := range items {
			if !strings.HasSuffix(item.Name(), ".json") || !item.Type().IsRegular() {
				continue
			}
			s, ok, err := loadFile(ctx, root, filepath.Join(host, item.Name()))
			if err != nil || !ok {
				continue
			}
			at, err := time.Parse(time.RFC3339, s.Updated)
			if err != nil || now.Sub(at) < 2*time.Minute || s.Entry.Project != project || s.Host == excludeHost && s.Session == excludeID {
				continue
			}
			sources = append(sources, s)
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Updated > sources[j].Updated })
	total := 0
	for i, s := range sources {
		if i >= 5 {
			break
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
		release, err := session.Lock(ctx, dirs.Locks(), s.Host, s.Session)
		if err != nil {
			return total, err
		}
		n, err := Recover(ctx, dirs, s.Host, s.Session)
		release()
		total += n
		if err != nil && ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
	return total, nil
}

func stateFile(host, id string) (string, error) {
	if host != "claude-code" && host != "codex" {
		return "", errors.New("catchup: invalid host")
	}
	name, err := paths.SafeName(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(host, name+".json"), nil
}
func load(ctx context.Context, dirs paths.Dirs, host, id string) (Source, bool, error) {
	name, err := stateFile(host, id)
	if err != nil {
		return Source{}, false, err
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "catchup"), false)
	if errors.Is(err, fs.ErrNotExist) {
		return Source{}, false, nil
	}
	if err != nil {
		return Source{}, false, err
	}
	defer root.Close()
	return loadFile(ctx, root, name)
}
func loadFile(ctx context.Context, root *os.Root, name string) (Source, bool, error) {
	var s Source
	if err := ctx.Err(); err != nil {
		return s, false, err
	}
	f, err := openRegular(root, name)
	if errors.Is(err, fs.ErrNotExist) {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return s, false, err
	}
	if json.Unmarshal(data, &s) != nil || s.V != 1 || s.Offset < 0 {
		return s, false, errors.New("catchup: invalid state")
	}
	expected, err := stateFile(s.Host, s.Session)
	if err != nil || expected != name {
		return s, false, errors.New("catchup: state identity mismatch")
	}
	return s, true, nil
}
func save(ctx context.Context, dirs paths.Dirs, s Source) error {
	name, err := stateFile(s.Host, s.Session)
	if err != nil {
		return err
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "catchup"), true)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(s.Host, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return paths.WriteFileAtomicAt(ctx, root, name, data, 0600)
}
func openRegular(root *os.Root, name string) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("catchup: not a regular file")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("catchup: file changed")
	}
	if st, ok := after.Sys().(*syscall.Stat_t); ok && st.Nlink != 1 {
		f.Close()
		return nil, errors.New("catchup: multiply linked file")
	}
	return f, nil
}
func openTranscript(s Source) (*os.File, error) {
	if !filepath.IsAbs(s.Home) || !filepath.IsAbs(s.Path) || !filepath.IsAbs(s.Cwd) {
		return nil, errors.New("catchup: invalid path")
	}
	if clean, n := redact.Redact(s.Path); n != 0 || clean != s.Path {
		return nil, errors.New("catchup: invalid path")
	}
	var base string
	switch s.Host {
	case "claude-code":
		base = filepath.Join(s.Home, ".claude", "projects")
		if filepath.Base(s.Path) != s.Session+".jsonl" {
			return nil, errors.New("catchup: wrong transcript")
		}
	case "codex":
		base = filepath.Join(s.Home, ".codex", "sessions")
		if !strings.HasSuffix(filepath.Base(s.Path), "-"+s.Session+".jsonl") {
			return nil, errors.New("catchup: wrong transcript")
		}
	default:
		return nil, errors.New("catchup: invalid host")
	}
	rel, err := filepath.Rel(base, s.Path)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return nil, errors.New("catchup: outside host store")
	}
	root, err := paths.OpenRoot(filepath.Dir(s.Path), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return openRegular(root, filepath.Base(s.Path))
}
func fileID(info os.FileInfo) (uint64, uint64) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev), st.Ino
	}
	return 0, 0
}
func tail(f *os.File, offset int64, limit int) (data []byte, start, end int64, skipped bool, err error) {
	info, err := f.Stat()
	if err != nil {
		return nil, 0, 0, false, err
	}
	start = offset
	if info.Size()-start > int64(limit) {
		start = info.Size() - int64(limit)
		skipped = true
	}
	data = make([]byte, info.Size()-start)
	n, e := f.ReadAt(data, start)
	if e != nil && e != io.EOF {
		return nil, 0, 0, false, e
	}
	data = data[:n]
	if start > 0 {
		var prev [1]byte
		_, _ = f.ReadAt(prev[:], start-1)
		if prev[0] != '\n' {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				return nil, start, start, skipped, nil
			}
			start += int64(i + 1)
			data = data[i+1:]
		}
	}
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		return nil, start, start, skipped, nil
	}
	data = data[:last+1]
	return data, start, start + int64(len(data)), skipped, nil
}
func entryKeys(e inbox.Entry) []string {
	at, _ := time.Parse(time.RFC3339Nano, e.TS)
	sum := sha256.Sum256([]byte(e.Role + "\x00" + e.Text + "\x00" + at.UTC().Format(time.RFC3339)))
	keys := []string{"text:" + hex.EncodeToString(sum[:])}
	if e.Turn != "" {
		keys = append(keys, "turn:"+e.Turn+":"+e.Role)
	}
	return keys
}
func knownEntries(ctx context.Context, dirs paths.Dirs, e inbox.Entry) (map[string]bool, error) {
	known := map[string]bool{}
	s := inbox.Store{Root: dirs.Inbox()}
	file := s.Path(e.Project, e.Host, e.Session)
	root, err := paths.OpenRoot(filepath.Dir(file), false)
	if errors.Is(err, fs.ErrNotExist) {
		return known, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := openRegular(root, filepath.Base(file))
	if errors.Is(err, fs.ErrNotExist) {
		return known, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, _, _, _, err := tail(f, 0, maxTail)
	if err != nil {
		return nil, err
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var row inbox.Entry
		if json.Unmarshal(line, &row) == nil {
			keys := entryKeys(row)
			for _, key := range keys {
				known[key] = true
			}
			if row.Turn == "" {
				known["unknown:"+keys[0]] = true
			}
		}
	}
	return known, nil
}

// delta reads the oldest unprocessed prefix, never skipping a privacy boundary.
// When no row ends within limit bytes, it returns no data and more set, and
// Recover scans past that row in chunks instead of jumping to its tail.
func delta(f *os.File, offset int64, limit int) (data []byte, start, end int64, more bool, err error) {
	info, err := f.Stat()
	if err != nil {
		return nil, 0, 0, false, err
	}
	if offset < 0 || offset > info.Size() {
		return nil, 0, 0, false, errors.New("catchup: bad cursor")
	}
	start = offset
	size := info.Size() - offset
	if size > int64(limit) {
		size = int64(limit)
		more = true
	}
	data = make([]byte, size)
	n, e := f.ReadAt(data, start)
	if e != nil && e != io.EOF {
		return nil, 0, 0, false, e
	}
	data = data[:n]
	if start > 0 {
		var prev [1]byte
		_, _ = f.ReadAt(prev[:], start-1)
		if prev[0] != '\n' {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				return nil, start, start, more, nil
			}
			start += int64(i + 1)
			data = data[i+1:]
		}
	}
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		return nil, start, start, more, nil
	}
	data = data[:last+1]
	return data, start, start + int64(len(data)), more, nil
}

// Contains checks bounded recent durable captures before a hook appends a row
// that recovery may already have filled. Turn IDs take precedence over text,
// and are checked first because they need no redaction.
func Contains(ctx context.Context, dirs paths.Dirs, e inbox.Entry) (bool, error) {
	known, err := knownEntries(ctx, dirs, e)
	if err != nil {
		return false, err
	}
	if e.Turn != "" && known["turn:"+e.Turn+":"+e.Role] {
		return true, nil
	}
	// A text key compares redacted text, and redacting a long text costs the
	// hook's own time, so a text over containsBound isn't redacted here.
	if len(e.Text) > containsBound {
		return false, nil
	}
	e.Text, _ = redactText(e.Text)
	keys := entryKeys(e)
	if e.Turn != "" {
		return known["unknown:"+keys[0]], nil
	}
	return known[keys[0]], nil
}

func systemAliases(path string) string {
	for _, alias := range []string{"/var", "/tmp", "/etc"} {
		if path == alias || strings.HasPrefix(path, alias+"/") {
			if target, err := os.Readlink(alias); err == nil && filepath.Join("/", target) == "/private"+alias {
				return "/private" + path
			}
		}
	}
	return path
}
