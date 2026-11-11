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
// not only the 64 newest of a folder that eva-md status reads. Complete is
// false when the context ended before every file was read, and the counts
// are then a floor.
type Waiting struct {
	Hosts    map[string]HostWaiting
	Complete bool
}

// HostWaiting is one host's part of Waiting: the session files longer than
// their watermark, as the update judges them (see inbox.Mark.Pending), the
// bytes past those watermarks, and the time of the oldest entry past one,
// zero when no such entry carries a time. Untimed counts the waiting
// sessions none of whose waiting entries carries one, and Damaged the
// sessions whose watermark can't be read or is past the end of the file,
// which the update never starts for.
type HostWaiting struct {
	Sessions int
	Bytes    int64
	Oldest   time.Time
	Untimed  int
	Damaged  int
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
	err := eachSessionFile(ctx, d.Inbox(), func(dir, name, host string, f *os.File, size int64) error {
		h := w.Hosts[host]
		defer func() { w.Hosts[host] = h }()
		m, err := store.Mark(filepath.Join(d.Inbox(), dir, name))
		if err != nil || m.Offset > size {
			h.Damaged++
			return nil
		}
		n := m.Pending(size)
		if n == 0 {
			return nil
		}
		h.Sessions++
		h.Bytes += n
		var oldest time.Time
		if _, err := f.Seek(m.Offset, io.SeekStart); err == nil {
			err = eachLine(ctx, io.LimitReader(f, n), func(line []byte) {
				var e inbox.Entry
				if json.Unmarshal(line, &e) != nil || !inbox.Trusted(e, dir, name, "") {
					return
				}
				if t, err := time.Parse(time.RFC3339, e.TS); err == nil && (oldest.IsZero() || t.Before(oldest)) {
					oldest = t
				}
			})
			if err != nil {
				return err
			}
		}
		switch {
		case oldest.IsZero():
			h.Untimed++
		case h.Oldest.IsZero() || oldest.Before(h.Oldest):
			h.Oldest = oldest
		}
		return nil
	})
	if ctx.Err() != nil {
		w.Complete = false
		return w, nil
	}
	return w, err
}

// eachSessionFile calls visit with each session file of every project
// folder of the inbox, in the order of their names: the folder's name, the
// file's name, its host, the file, open for reading through no link (see
// inbox.OpenSession), and its size. A project folder or a file that can't
// be opened is passed over. It stops at visit's first error, or when ctx
// ends, and returns that error. A missing inbox has no files.
func eachSessionFile(ctx context.Context, dir string, visit func(project, name, host string, f *os.File, size int64) error) error {
	projects, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, p := range projects {
		if !p.IsDir() || strings.HasPrefix(p.Name(), ".") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := eachFileOf(ctx, dir, p.Name(), visit); err != nil {
			return err
		}
	}
	return nil
}

// eachFileOf is eachSessionFile for one project folder.
func eachFileOf(ctx context.Context, dir, project string, visit func(project, name, host string, f *os.File, size int64) error) error {
	root, err := paths.OpenRoot(filepath.Join(dir, project), false)
	if err != nil {
		return nil
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		host := sessionHost(e.Name())
		if host == "" || !e.Type().IsRegular() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		f, info, err := inbox.OpenSession(root, e.Name())
		if err != nil {
			continue
		}
		err = visit(project, e.Name(), host, f, info.Size())
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
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
// and search pass it, and an unterminated last line is left, as a write may
// still be in progress. It stops when ctx ends, and returns ctx's error or
// a read error.
func eachLine(ctx context.Context, r io.Reader, visit func(line []byte)) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	long := false
	for {
		frag, err := br.ReadSlice('\n')
		switch {
		case long:
		case len(buf)+len(frag) > inbox.MaxLine+1:
			long, buf = true, buf[:0]
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
		if !long {
			visit(buf[:len(buf)-1])
		}
		buf, long = buf[:0], false
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}
