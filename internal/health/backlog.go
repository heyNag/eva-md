package health

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// Waiting is what waits for an update, by host, read from the entries
// themselves: in every session file of every project folder of the inbox,
// not only the 64 newest of a folder that eva-md status reads. Unread
// counts the project folders and session files that couldn't be opened or
// read to the end. Complete is false when one couldn't, or when the
// context ended before every file was read, and the counts are then a
// floor.
type Waiting struct {
	Hosts    map[string]HostWaiting
	Unread   int
	Complete bool
}

// HostWaiting is one host's part of Waiting: the session files longer than
// their watermark, as the update judges them (see inbox.Mark.Pending), the
// bytes past those watermarks, and the time of the oldest entry past one,
// zero when no such entry carries a time. Untimed counts the waiting
// sessions none of whose waiting entries carries one, and Damaged the
// sessions whose watermark can't be read or is past the end of the file,
// which the update never starts for.
//
// Held counts apart the sessions longer than their watermark that the
// update leaves until more is appended, as it found what follows the
// watermark too small to process, as a turn not yet finished: their
// entries are not organized either. HeldBytes is the bytes past their
// watermarks, and HeldOldest the time of the oldest entry past one, zero
// when none carries a time.
type HostWaiting struct {
	Sessions   int
	Bytes      int64
	Oldest     time.Time
	Untimed    int
	Damaged    int
	Held       int
	HeldBytes  int64
	HeldOldest time.Time
}

// Unorganized reads what waits for an update. It reads each session file's
// watermark and the entries past it, never the entries before, and writes
// nothing. A missing inbox has nothing waiting; one that can't be listed is
// an error.
func Unorganized(ctx context.Context, d paths.Dirs) (Waiting, error) {
	w := Waiting{Hosts: map[string]HostWaiting{}, Complete: true}
	for _, host := range updateHosts {
		w.Hosts[host] = HostWaiting{}
	}
	store := inbox.Store{Root: d.Inbox()}
	unread, err := eachSessionFile(ctx, d.Inbox(), func(dir, name, host string, f *os.File, size int64) error {
		h := w.Hosts[host]
		defer func() { w.Hosts[host] = h }()
		m, err := store.Mark(filepath.Join(d.Inbox(), dir, name))
		if err != nil || m.Offset > size {
			h.Damaged++
			return nil
		}
		if size <= m.Offset {
			return nil
		}
		held := m.Pending(size) == 0
		var oldest time.Time
		if _, err := f.Seek(m.Offset, io.SeekStart); err != nil {
			return err
		}
		err = eachLine(ctx, io.LimitReader(f, size-m.Offset), func(line []byte) {
			var e inbox.Entry
			if json.Unmarshal(line, &e) != nil || !inbox.Trusted(e, dir, name, "") {
				return
			}
			if t, err := time.Parse(time.RFC3339, e.TS); err == nil && (oldest.IsZero() || t.Before(oldest)) {
				oldest = t
			}
		}, nil)
		if err != nil {
			return err
		}
		if held {
			h.Held++
			h.HeldBytes += size - m.Offset
			if !oldest.IsZero() && (h.HeldOldest.IsZero() || oldest.Before(h.HeldOldest)) {
				h.HeldOldest = oldest
			}
			return nil
		}
		h.Sessions++
		h.Bytes += size - m.Offset
		switch {
		case oldest.IsZero():
			h.Untimed++
		case h.Oldest.IsZero() || oldest.Before(h.Oldest):
			h.Oldest = oldest
		}
		return nil
	})
	w.Unread = unread
	if ctx.Err() != nil || unread > 0 {
		w.Complete = false
	}
	if ctx.Err() != nil {
		return w, nil
	}
	return w, err
}

// eachSessionFile calls visit with each session file of every project
// folder of the inbox, in the order of their names: the folder's name, the
// file's name, its host, the file, open for reading through no link (see
// inbox.OpenSession), and its size. A project folder or a file that can't
// be opened, or a file visit fails to read, is passed over and counted in
// unread. It stops when ctx ends, and returns ctx's error then; an inbox
// that can't be listed is an error, and a missing one has no files.
func eachSessionFile(ctx context.Context, dir string, visit func(project, name, host string, f *os.File, size int64) error) (unread int, err error) {
	projects, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, p := range projects {
		if !p.IsDir() || strings.HasPrefix(p.Name(), ".") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return unread, err
		}
		n, err := eachFileOf(ctx, dir, p.Name(), visit)
		unread += n
		if err != nil {
			return unread, err
		}
	}
	return unread, nil
}

// eachFileOf is eachSessionFile for one project folder.
func eachFileOf(ctx context.Context, dir, project string, visit func(project, name, host string, f *os.File, size int64) error) (unread int, err error) {
	root, err := paths.OpenRoot(filepath.Join(dir, project), false)
	if err != nil {
		return 1, nil
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return 1, nil
	}
	for _, e := range entries {
		host := sessionHost(e.Name())
		if host == "" || !e.Type().IsRegular() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return unread, err
		}
		f, info, err := inbox.OpenSession(root, e.Name())
		if err != nil {
			unread++
			continue
		}
		err = visit(project, e.Name(), host, f, info.Size())
		f.Close()
		if ctx.Err() != nil {
			return unread, ctx.Err()
		}
		if err != nil {
			unread++
		}
	}
	return unread, nil
}

// sessionHost is the host of a session file's name, <host>-<id>.jsonl, or
// "" for a name that is no session file's.
func sessionHost(name string) string {
	if !strings.HasSuffix(name, ".jsonl") {
		return ""
	}
	for _, host := range updateHosts {
		if strings.HasPrefix(name, host+"-") && len(name) > len(host)+len("-.jsonl") {
			return host
		}
	}
	return ""
}

// eachLine calls visit with each complete line r holds, without its
// newline. A line longer than inbox.MaxLine is passed over, as eva-md show
// and search pass it, with a call of long when long is not nil, and an
// unterminated last line is left, as a write may still be in progress. It
// stops when ctx ends, and returns ctx's error or a read error.
func eachLine(ctx context.Context, r io.Reader, visit func(line []byte), long func()) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	tooLong := false
	for {
		frag, err := br.ReadSlice('\n')
		switch {
		case tooLong:
		case len(buf)+len(frag) > inbox.MaxLine+1:
			tooLong, buf = true, buf[:0]
		default:
			buf = append(buf, frag...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case !tooLong:
			visit(buf[:len(buf)-1])
		case long != nil:
			long()
		}
		buf, tooLong = buf[:0], false
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}
