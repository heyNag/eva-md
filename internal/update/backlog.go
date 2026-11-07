package update

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/heyNag/eva-md/internal/inbox"
)

// The window of a session file the job decodes at a time. A pass covers at
// most 60 KB of trimmed input, which a few MiB of entries nearly always
// fill, so the window grows, doubling, only when a group runs on past it
// with room left in its chunk.
const (
	windowBytes    = 4 << 20
	maxWindowBytes = 64 << 20
)

// errUnusableMark reports a watermark that can't be read, or that is past
// the end of its session file or inside a line. The triggers never spawn for
// such a session, so the job records it as failed (watermark).
var errUnusableMark = errors.New("update: unusable watermark")

// backlog reads one session file past its watermark, a bounded window at a
// time, and never the whole of a large backlog.
type backlog struct {
	store   inbox.Store
	path    string
	f       *os.File
	start   int64 // the watermark: entries[0] starts here
	lines   int   // the physical lines before start
	off     bool  // the chat was off the record at start
	web     bool  // the turn open at start used a web fetch or web search
	more    bool  // the pass that set the watermark stopped inside a group
	typed   bool  // a prompt the user typed comes before start
	hosted  bool  // a host prompt comes before start
	window  int64 // the bytes read past start
	limit   int64 // the most window grows to: maxWindowBytes, less in a test
	eof     bool  // the window reached the end of the file
	entries []Entry
}

// openBacklog opens a session file at its watermark. It counts the lines
// before the watermark once, with scanPrefix, which also tells whether the
// chat was off the record there, whether the turn open there used the web
// and whether prompts come before it.
func openBacklog(store inbox.Store, path string) (*backlog, error) {
	m, err := store.Mark(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errUnusableMark, err)
	}
	f, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	b := &backlog{store: store, path: path, f: f, start: m.Offset, more: m.More, limit: maxWindowBytes}
	if err := b.check(); err != nil {
		f.Close()
		return nil, err
	}
	p, err := scanPrefix(io.NewSectionReader(f, 0, m.Offset))
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("update: read the session file: %w", err)
	}
	b.lines, b.off, b.web, b.typed, b.hosted = p.lines, p.off, p.web, p.typed, p.hosted
	if err := b.load(windowBytes); err != nil {
		f.Close()
		return nil, err
	}
	return b, nil
}

func (b *backlog) close() { b.f.Close() }

// check refuses a watermark past the end of the file or inside a line.
func (b *backlog) check() error {
	info, err := b.f.Stat()
	if err != nil {
		return err
	}
	if b.start > info.Size() {
		return fmt.Errorf("%w: past the end of the session file", errUnusableMark)
	}
	if b.start > 0 {
		var prev [1]byte
		if _, err := b.f.ReadAt(prev[:], b.start-1); err != nil || prev[0] != '\n' {
			return fmt.Errorf("%w: not at a line boundary", errUnusableMark)
		}
	}
	return nil
}

// load reads n bytes past start and decodes their complete lines.
func (b *backlog) load(n int64) error {
	buf := make([]byte, n)
	k, err := b.f.ReadAt(buf, b.start)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("update: read the session file: %w", err)
	}
	b.window, b.eof = int64(k), k < len(buf)
	b.entries = Decode(buf[:k], b.start, b.lines+1)
	return nil
}

// grow reads a window twice as large, or reports that it can't.
func (b *backlog) grow() (bool, error) {
	if b.eof || b.window >= b.limit {
		return false, nil
	}
	return true, b.load(min(max(2*b.window, windowBytes), b.limit))
}

// refill reads the next window once the decoded entries are used up, and
// reports whether any entries are pending.
func (b *backlog) refill() (bool, error) {
	if len(b.entries) == 0 && !b.eof {
		if err := b.load(windowBytes); err != nil {
			return false, err
		}
	}
	return len(b.entries) > 0, nil
}

// group returns the next (session, branch) group: the run of entries from
// the watermark that share a branch. Only a user prompt of another branch
// ends it: the hooks record each entry's branch as HEAD was then, so a turn
// that checks out a branch has its tool lines and reply on the new one, and
// they stay with their prompt. An entry without a branch, such as a gap,
// joins the run it is in, and a run of none is the default thread. closed
// reports that a prompt of another branch ends the run inside the window.
func (b *backlog) group() (entries []Entry, branch string, closed bool) {
	for i, e := range b.entries {
		switch {
		case e.Branch == "":
		case branch == "":
			branch = e.Branch
		case e.Branch != branch && e.Role == "user" && e.Kind == "prompt":
			return b.entries[:i], branch, true
		}
	}
	if branch == "" {
		branch = "default"
	}
	return b.entries, branch, false
}

// cut reports whether the group may run on past the window: no prompt of
// another branch ends it there, and the file goes on.
func (b *backlog) cut(closed bool) bool { return !closed && !b.eof }

// programRun reports a chat a program ran, such as claude -p: no prompt the
// user typed comes before the watermark or in the window, and the catch-up
// record, which headless reads, marks it a program's chat, or the window
// reaches the end of the file and a host prompt comes before it. A person's
// chat can begin with host prompts, from scheduled tasks, /loop runs or
// subagent reports, and type later, so a host prompt counts only once the
// whole file is read.
func (b *backlog) programRun(headless func() bool) bool {
	if b.typed {
		return false
	}
	hosted := b.hosted
	for _, e := range b.entries {
		if typed(e.Entry) {
			return false
		}
		hosted = hosted || hostEvent(e.Entry)
	}
	return b.eof && hosted || headless()
}

// answered leaves out the last prompt of a group the file ends in, and what
// follows it, while no reply follows it, so the prompt waits to be taken
// with its reply: a pass that took it alone would leave a rest with no
// prompt, which never passes the size check, and a chat closed after the
// reply would lose it. This holds whatever comes before the prompt, so the
// result may be empty. A group the chat's session_end ends keeps the
// prompt, since no reply can join it.
func answered(entries []Entry) []Entry {
	if len(entries) == 0 || ended(entries) {
		return entries
	}
	for i := len(entries) - 1; i >= 0; i-- {
		switch e := entries[i]; {
		case e.Role == "assistant" && e.Kind == "reply":
			return entries
		case e.Role == "user" && e.Kind == "prompt":
			return entries[:i]
		}
	}
	return entries
}

// touch saves the watermark as it is, so a pass finds out that it can't be
// saved before it spends a model call.
func (b *backlog) touch() error {
	if err := b.store.SetMark(b.path, inbox.Mark{Offset: b.start, More: b.more}); err != nil {
		return fmt.Errorf("update: save the watermark: %w", err)
	}
	return nil
}

// wait records in the watermark that the rest of the file, as far as the
// window read it, waits for more entries, so the triggers and the brief
// leave the session alone until something is appended. A window that
// stopped short of the end records nothing.
func (b *backlog) wait() error {
	if !b.eof {
		return nil
	}
	if err := b.store.SetMark(b.path, inbox.Mark{Offset: b.start, More: b.more, Wait: b.start + b.window}); err != nil {
		return fmt.Errorf("update: save the watermark: %w", err)
	}
	return nil
}

// advance moves the watermark to end, just past an entry, and saves it with
// more, so the next pass continues a group this one stopped in; off is
// whether the chat was off the record there. Whether the turn open there
// used the web follows from the entries it passes, those on the record, as
// Build reads them.
func (b *backlog) advance(end int64, more, off bool) error {
	if err := b.store.SetMark(b.path, inbox.Mark{Offset: end, More: more}); err != nil {
		return fmt.Errorf("update: save the watermark: %w", err)
	}
	n, was := 0, b.off
	for n < len(b.entries) && b.entries[n].End <= end {
		e := b.entries[n].Entry
		b.typed = b.typed || typed(e)
		b.hosted = b.hosted || hostEvent(e)
		if marker(e) {
			was = e.Text == "off_record"
		} else if !was {
			b.web = turnWeb(b.web, e)
		}
		n++
	}
	b.entries = b.entries[n:]
	b.lines += n
	b.window -= end - b.start
	b.start, b.more, b.off = end, more, off
	return nil
}
