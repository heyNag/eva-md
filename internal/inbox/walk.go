package inbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"unicode"

	"github.com/heyNag/eva-md/internal/paths"
)

// MaxLine is the longest inbox line a reader decodes; a longer one is passed
// over, as eva-md show and search pass it.
const MaxLine = maxLine

// Position is where a walk of a session file stands: the byte offset just
// after the last complete line it read, the physical lines before it, and
// whether the session was off the record there.
type Position struct {
	Offset int64
	Line   int
	Off    bool
}

// Line is an entry a walk yields, with where its line stands in its file:
// the line's byte offset, its length without the newline, and its 1-based
// physical line number, as eva-md show counts lines.
type Line struct {
	Entry
	Offset int64
	Length int
	Line   int
}

// Walk reads the session file f, named name in the project folder dir, from
// position from, one physical line at a time, and calls visit with each
// entry a search can match: a prompt, reply, tool or plan line that decodes,
// belongs to its file (see Trusted) and was not said off the record. The
// off-the-record state changes only at the off_record and on_record events
// it passes, in file order, as search and the brief track it. A line over
// MaxLine is passed over, and an unterminated last line is left, as a write
// may still be in progress. Walk stops at the first line that ends at or
// past limit bytes from where it started (0 for no limit), when visit
// returns false, or when ctx is done, and returns the position reached; err
// is then ctx's error, or a read error.
func Walk(ctx context.Context, f *os.File, dir, name string, from Position, limit int64, visit func(Line) bool) (Position, error) {
	pos := from
	if _, err := f.Seek(from.Offset, io.SeekStart); err != nil {
		return pos, err
	}
	r := bufio.NewReaderSize(f, 64<<10)
	var buf []byte
	for limit <= 0 || pos.Offset-from.Offset < limit {
		if err := ctx.Err(); err != nil {
			return pos, err
		}
		line, ok, size, err := readLine(r, &buf)
		if errors.Is(err, io.EOF) {
			return pos, nil
		}
		if err != nil {
			return pos, err
		}
		at := pos.Offset
		pos.Offset += int64(size)
		pos.Line++
		if !ok {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) != nil || !Trusted(e, dir, name, "") {
			continue
		}
		switch {
		case e.Kind == "event" && e.Text == "off_record":
			pos.Off = true
		case e.Kind == "event" && e.Text == "on_record":
			pos.Off = false
		case pos.Off:
		case e.Kind == "prompt" || e.Kind == "reply" || e.Kind == "tool" || e.Kind == "plan":
			if !visit(Line{Entry: e, Offset: at, Length: len(line), Line: pos.Line}) {
				return pos, nil
			}
		}
	}
	return pos, nil
}

// readLine reads one physical line from r without its newline, into buf
// when it doesn't fit r's buffer. A line over MaxLine comes back with ok
// false. size counts the bytes of the line and its newline. An unterminated
// last line is left: err is io.EOF.
func readLine(r *bufio.Reader, buf *[]byte) (line []byte, ok bool, size int, err error) {
	*buf = (*buf)[:0]
	long := false
	for {
		frag, err := r.ReadSlice('\n')
		size += len(frag)
		if err == nil && len(*buf) == 0 && !long && len(frag) <= maxLine+1 {
			return frag[:len(frag)-1], true, size, nil
		}
		if long || len(*buf)+len(frag) > maxLine+1 {
			long, *buf = true, (*buf)[:0]
		} else {
			*buf = append(*buf, frag...)
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil:
			return nil, false, size, err
		case long:
			return nil, false, size, nil
		}
		return (*buf)[:len(*buf)-1], true, size, nil
	}
}

// Trusted reports whether an entry read from the session file named name,
// in the project folder dir, belongs there, as search and the brief check
// it: a host eva-md captures, a session id free of control and format
// characters, a project whose folder is dir and that is key when key is not
// empty, and a host and session whose file is name.
func Trusted(e Entry, dir, name, key string) bool {
	if e.Session == "" || e.Host != "codex" && e.Host != "claude-code" {
		return false
	}
	for _, c := range e.Session {
		if unicode.IsControl(c) || unicode.In(c, unicode.Cf) {
			return false
		}
	}
	if key != "" && e.Project != key {
		return false
	}
	if d, err := paths.SafeName(e.Project); err != nil || d != dir {
		return false
	}
	file, ok := FileName(e.Host, e.Session)
	return ok && file == name
}

// FileName is the name of a session's file in its project folder,
// <host>-<session>.jsonl, each part made safe as Store.Path makes it.
func FileName(host, session string) (string, bool) {
	h, err := paths.SafeName(host)
	if err != nil {
		return "", false
	}
	id, err := paths.SafeName(session)
	if err != nil {
		return "", false
	}
	return h + "-" + id + ".jsonl", true
}

// OpenSession opens the session file name in the project folder root for
// reading. It refuses a symlink, a multiply linked file, a file replaced
// while opening and anything but a regular file.
func OpenSession(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	f, err := openRegular(root, name, os.O_RDONLY, false)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err == nil {
		err = regular(f)
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}
