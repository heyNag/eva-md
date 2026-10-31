// Package inbox stores private, append-only, redacted capture records.
package inbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/redact"
)

const maxText = 200 << 10
const maxLine = 2 << 20

// Entry is one JSONL record in a session's inbox.
type Entry struct {
	V       int    `json:"v"`
	TS      string `json:"ts"`
	Host    string `json:"host"`
	Session string `json:"session"`
	Turn    string `json:"turn,omitempty"`
	Role    string `json:"role"`
	Kind    string `json:"kind"`
	Agent   string `json:"agent,omitempty"`
	Text    string `json:"text,omitempty"`
	Project string `json:"project"`
	Branch  string `json:"branch,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Model   string `json:"model,omitempty"`
	Origin  string `json:"origin"`
	Red     int    `json:"red,omitempty"`
	Trunc   int    `json:"trunc,omitempty"`
}

// Store locates captures beneath Root, normally paths.Dirs.Inbox().
type Store struct{ Root string }

// Path returns the path of a session, sanitizing each outside path component.
// An empty component or a relative Root returns an empty path.
func (s Store) Path(projectKey, host, session string) string {
	if !filepath.IsAbs(s.Root) {
		return ""
	}
	p, err := paths.SafeName(projectKey)
	if err != nil {
		return ""
	}
	h, err := paths.SafeName(host)
	if err != nil {
		return ""
	}
	id, err := paths.SafeName(session)
	if err != nil {
		return ""
	}
	return filepath.Join(s.Root, p, h+"-"+id+".jsonl")
}

// Append redacts and appends one entry under an exclusive flock. User and
// assistant records are fsynced before returning.
func (s Store) Append(e Entry) error { return s.AppendContext(context.Background(), e) }

// AppendContext is Append with cancellable, nonblocking flock acquisition.
func (s Store) AppendContext(ctx context.Context, e Entry) error {
	return s.append(ctx, e, redact.Redact)
}

// Passing the redactor explicitly keeps panic injection in tests race-free.
func (s Store) append(ctx context.Context, e Entry, redactor func(string) (string, int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.V == 0 {
		e.V = 1
	}
	var ok bool
	e.Text, e.Red, ok = redactText(e.Text, e.Red, redactor)
	if !ok {
		e.Text = ""
	}
	// Metadata reaches disk too. Reject identity fields (which also become
	// filenames) that hold a secret in a known format. They skip the entropy
	// rule, which would reject eva-md's own keys for ordinary names such as
	// local-token-refresh-worker-c9a8c7 and drop every capture for them.
	for _, value := range []string{e.Host, e.Session, e.Project} {
		clean, _, safe := redactText(value, 0, redact.RedactKnown)
		if !safe || clean != value || len(value) > 512 {
			return errors.New("inbox: unsafe identity metadata")
		}
	}
	// The branch groups entries into threads, so it gets the treatment the
	// hook gives its thread key: no control characters and format-only
	// redaction, which keeps feature/token-refresh-... and
	// feature/token-expiry-... apart.
	branch, n, safe := redactText(e.Branch, 0, branchIdentifier)
	if !safe {
		branch = ""
	}
	e.Branch = branch
	e.Red += n
	// Redact all remaining free-form metadata.
	for _, value := range []*string{&e.TS, &e.Turn, &e.Role, &e.Kind, &e.Agent, &e.Cwd, &e.Model, &e.Origin} {
		clean, n, safe := redactText(*value, 0, redact.Redact)
		if !safe {
			clean = ""
		}
		*value = cutUTF8(clean, 4096)
		e.Red += n
	}
	if len(e.Text) > maxText {
		head := cutUTF8(e.Text, maxText/2)
		tail := e.Text[len(e.Text)-maxText/2:]
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
		e.Trunc += len(e.Text) - len(head) - len(tail)
		e.Text = head + tail
	}
	file := s.Path(e.Project, e.Host, e.Session)
	if file == "" {
		return errors.New("inbox: invalid location")
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("inbox: encode: %w", err)
	}
	data = append(data, '\n')
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := paths.OpenRoot(s.Root, true)
	if err != nil {
		return fmt.Errorf("inbox: root: %w", err)
	}
	defer root.Close()
	name, _ := filepath.Rel(s.Root, file)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return fmt.Errorf("inbox: directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := openRegular(root, name, os.O_APPEND|os.O_WRONLY, true)
	if err != nil {
		return fmt.Errorf("inbox: open: %w", err)
	}
	defer f.Close()
	if err := regular(f); err != nil {
		return err
	}
	if err := lock(ctx, f); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("inbox: permissions: %w", err)
	}
	// A killed writer can leave a partial final JSON object. Keep the file
	// append-only, terminate that physical line, and record the lost capture.
	partial, err := partialTail(root, name, f)
	if err != nil {
		return err
	}
	if partial {
		gap := e
		gap.Role = "event"
		gap.Kind = "event"
		gap.Text = "gap: interrupted append"
		gap.Red = 0
		gap.Trunc = 0
		gapData, err := json.Marshal(gap)
		if err != nil {
			return err
		}
		prefix := append([]byte{'\n'}, gapData...)
		prefix = append(prefix, '\n')
		data = append(prefix, data...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("inbox: append: %w", err)
	}
	if e.Role == "user" || e.Role == "assistant" {
		if err := f.Sync(); err != nil {
			return fmt.Errorf("inbox: sync: %w", err)
		}
		if err := paths.SyncDirAt(root, filepath.Dir(name)); err != nil {
			return fmt.Errorf("inbox: sync directory: %w", err)
		}
	}
	return nil
}

func partialTail(root *os.Root, name string, writer *os.File) (bool, error) {
	info, err := writer.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}
	r, err := openRegular(root, name, os.O_RDONLY, false)
	if err != nil {
		return false, err
	}
	defer r.Close()
	rInfo, err := r.Stat()
	if err != nil {
		return false, err
	}
	if !os.SameFile(info, rInfo) {
		return false, errors.New("inbox: file changed while appending")
	}
	var last [1]byte
	if _, err := r.ReadAt(last[:], info.Size()-1); err != nil {
		return false, err
	}
	return last[0] != '\n', nil
}

// maxBranch is the hook's limit for the thread key, in runes, so a stored
// branch always equals the thread key it came from.
const maxBranch = 1024

func branchIdentifier(s string) (string, int) { return redact.Identifier(s, maxBranch) }

func redactText(s string, count int, redactor func(string) (string, int)) (text string, n int, ok bool) {
	defer func() {
		if recover() != nil {
			text = ""
			n = count
			ok = false
		}
	}()
	text, n = redactor(s)
	return text, count + n, true
}

func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func lock(ctx context.Context, f *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return fmt.Errorf("inbox: lock: %w", err)
		}
		timer := time.NewTimer(2 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func regular(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("inbox: stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("inbox: not a regular file")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
		return errors.New("inbox: multiply linked file")
	}
	return nil
}

// os.Root resolves final symlinks itself, even with O_NOFOLLOW. Check both the
// path and opened inode. Exclusive creation also avoids a macOS/Go os.Root
// initial O_CREATE race that can report ENOENT to concurrent writers.
func openRegular(root *os.Root, name string, flags int, create bool) (*os.File, error) {
	if create {
		f, err := root.OpenFile(name, flags|os.O_CREATE|os.O_EXCL|syscall.O_NONBLOCK, 0o600)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("inbox: path is not a regular file")
	}
	f, err := root.OpenFile(name, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !os.SameFile(before, info) {
		f.Close()
		return nil, errors.New("inbox: file changed while opening")
	}
	if err := regular(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (s Store) relative(file string) (string, error) {
	if !filepath.IsAbs(s.Root) || !filepath.IsAbs(file) {
		return "", errors.New("inbox: expected absolute path")
	}
	rel, err := filepath.Rel(s.Root, file)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return "", errors.New("inbox: path outside root")
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || parts[0] == ".state" || !strings.HasSuffix(parts[1], ".jsonl") {
		return "", errors.New("inbox: invalid capture path")
	}
	return rel, nil
}

// ReadFrom returns complete JSON lines after offset. An incomplete final line
// stays unread. A malformed complete line becomes a fixed gap entry so later
// valid records remain readable and physical line references remain stable.
func (s Store) ReadFrom(file string, offset int64) ([]Entry, int64, error) {
	if offset < 0 {
		return nil, offset, errors.New("inbox: negative offset")
	}
	name, err := s.relative(file)
	if err != nil {
		return nil, offset, err
	}
	root, err := paths.OpenRoot(s.Root, false)
	if err != nil {
		return nil, offset, err
	}
	defer root.Close()
	f, err := openRegular(root, name, os.O_RDONLY, false)
	if err != nil {
		return nil, offset, fmt.Errorf("inbox: read: %w", err)
	}
	defer f.Close()
	if err := regular(f); err != nil {
		return nil, offset, err
	}
	info, _ := f.Stat()
	if offset > info.Size() {
		return nil, offset, errors.New("inbox: offset beyond file")
	}
	if offset > 0 {
		var prev [1]byte
		if _, err := f.ReadAt(prev[:], offset-1); err != nil || prev[0] != '\n' {
			return nil, offset, errors.New("inbox: offset is not a line boundary")
		}
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	r := bufio.NewReader(f)
	var entries []Entry
	for {
		var line []byte
		var length int64
		oversized := false
		for {
			fragment, err := r.ReadSlice('\n')
			length += int64(len(fragment))
			if length > maxLine {
				oversized = true
				line = nil
			}
			if !oversized {
				line = append(line, fragment...)
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if errors.Is(err, io.EOF) {
				return entries, offset, nil
			}
			if err != nil {
				return entries, offset, fmt.Errorf("inbox: read line: %w", err)
			}
			break
		}
		var e Entry
		if err := json.Unmarshal(line, &e); oversized || err != nil {
			e = Entry{V: 1, Role: "event", Kind: "event", Text: "gap: malformed inbox record", Origin: "hook"}
		}
		entries = append(entries, e)
		offset += length
	}
}

func (s Store) watermarkName(file string) (string, error) {
	rel, err := s.relative(file)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(rel), ".state", filepath.Base(rel)+".wm"), nil
}

// Mark is a session file's watermark: the byte offset the background update
// has processed up to, and whether the pass that set it stopped inside a
// group of entries, so the next pass continues that group rather than
// judging its rest as a group of its own. Wait is the file's size when the
// update last found the rest too small to process yet, waiting for more
// entries; zero when it hasn't.
type Mark struct {
	Offset int64
	More   bool
	Wait   int64
}

// Pending is how many bytes of a session file size bytes long the update
// has yet to process: those past the offset, unless the update found them
// all too small to process yet and nothing was appended since.
func (m Mark) Pending(size int64) int64 {
	if size <= m.Offset || size <= m.Wait {
		return 0
	}
	return size - m.Offset
}

// Watermark returns the stored byte offset, or zero when no watermark exists.
func (s Store) Watermark(file string) (int64, error) {
	m, err := s.Mark(file)
	return m.Offset, err
}

// Mark returns the stored watermark, or the zero Mark when none exists. The
// file holds the offset, followed by " more" when More is set and by
// " wait <size>" when Wait is.
func (s Store) Mark(file string) (Mark, error) {
	name, err := s.watermarkName(file)
	if err != nil {
		return Mark{}, err
	}
	root, err := paths.OpenRoot(s.Root, false)
	if errors.Is(err, fs.ErrNotExist) {
		return Mark{}, nil
	}
	if err != nil {
		return Mark{}, err
	}
	defer root.Close()
	f, err := openRegular(root, name, os.O_RDONLY, false)
	if errors.Is(err, fs.ErrNotExist) {
		return Mark{}, nil
	}
	if err != nil {
		return Mark{}, err
	}
	defer f.Close()
	if err := regular(f); err != nil {
		return Mark{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil {
		return Mark{}, err
	}
	m, ok := parseMark(strings.Fields(string(data)))
	if !ok {
		return Mark{}, errors.New("inbox: invalid watermark")
	}
	return m, nil
}

// parseMark reads a watermark's fields: the offset, then "more" when More
// is set, then "wait" and the size when Wait is.
func parseMark(fields []string) (Mark, bool) {
	var m Mark
	if len(fields) == 0 {
		return m, false
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || n < 0 {
		return m, false
	}
	m.Offset, fields = n, fields[1:]
	if len(fields) > 0 && fields[0] == "more" {
		m.More, fields = true, fields[1:]
	}
	if len(fields) == 2 && fields[0] == "wait" {
		w, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || w <= 0 {
			return m, false
		}
		m.Wait, fields = w, nil
	}
	return m, len(fields) == 0
}

// SetWatermark atomically replaces the watermark with a nonnegative offset.
func (s Store) SetWatermark(file string, offset int64) error {
	return s.SetMark(file, Mark{Offset: offset})
}

// SetMark atomically replaces the watermark.
func (s Store) SetMark(file string, m Mark) error {
	if m.Offset < 0 || m.Wait < 0 {
		return errors.New("inbox: negative watermark")
	}
	name, err := s.watermarkName(file)
	if err != nil {
		return err
	}
	root, err := paths.OpenRoot(s.Root, true)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return err
	}
	data := strconv.FormatInt(m.Offset, 10)
	if m.More {
		data += " more"
	}
	if m.Wait > 0 {
		data += " wait " + strconv.FormatInt(m.Wait, 10)
	}
	return paths.WriteFileAtomicAt(context.Background(), root, name, []byte(data+"\n"), 0o600)
}

// Open opens a session file for reading. It refuses a path outside Root or
// in a .state folder, a symlink, a multiply linked file and anything but a
// regular file.
func (s Store) Open(file string) (*os.File, error) {
	name, err := s.relative(file)
	if err != nil {
		return nil, err
	}
	root, err := paths.OpenRoot(s.Root, false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := openRegular(root, name, os.O_RDONLY, false)
	if err != nil {
		return nil, fmt.Errorf("inbox: open: %w", err)
	}
	if err := regular(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Ref formats a short session-and-line reference.
func Ref(session string, line int) string {
	runes := []rune(session)
	if len(runes) > 8 {
		runes = runes[:8]
	}
	return "s:" + string(runes) + "#" + strconv.Itoa(line)
}
