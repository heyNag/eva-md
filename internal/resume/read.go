package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

const tailBytes = 1 << 20

// regular opens name through a pinned root, refusing symlinks, hard links,
// devices and a substituted inode.
func regular(root *os.Root, name string) (*os.File, error) {
	if !filepath.IsLocal(name) {
		return nil, errors.New("resume: invalid file path")
	}
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i := 1; i < len(parts); i++ {
		info, err := root.Lstat(filepath.Join(parts[:i]...))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("resume: linked parent")
		}
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("resume: not a regular file")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, errors.New("resume: file changed while opening")
	}
	if st, ok := after.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
		f.Close()
		return nil, errors.New("resume: multiply linked file")
	}
	return f, nil
}

func readHistories(ctx context.Context, o Options) ([]history, error) {
	key := safeProject(o.Project)
	if key == "" {
		return nil, errors.New("resume: missing project")
	}
	root, err := paths.OpenRoot(filepath.Join(o.Dirs.Inbox(), key), false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	names, err := dir.Readdir(-1)
	dir.Close()
	if err != nil {
		return nil, err
	}
	sort.Slice(names, func(i, j int) bool { return names[i].ModTime().After(names[j].ModTime()) })
	var all []history
	for _, info := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(all) >= 64 {
			break
		}
		if !info.Mode().IsRegular() || !strings.HasSuffix(info.Name(), ".jsonl") {
			continue
		}
		f, err := regular(root, info.Name())
		if err != nil {
			continue
		}
		recs, worked, err := readTail(ctx, f)
		f.Close()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		// A branch switch within a session creates separate histories. Source
		// references retain the physical JSONL line numbers in either branch.
		byBranch := map[string]*history{}
		off := false
		for _, r := range recs {
			badID := false
			for _, c := range r.Session {
				if unicode.IsControl(c) || unicode.In(c, unicode.Cf) {
					badID = true
					break
				}
			}
			if badID {
				continue
			}
			if r.Project != o.Project.Key || r.Session == "" || (r.Host != "codex" && r.Host != "claude-code") {
				continue
			}
			if filepath.Base((inbox.Store{Root: o.Dirs.Inbox()}).Path(r.Project, r.Host, r.Session)) != info.Name() {
				continue
			}
			if r.Kind == "event" && r.Text == "off_record" {
				off = true
				continue
			}
			if r.Kind == "event" && r.Text == "on_record" {
				off = false
				continue
			}
			if off {
				continue
			}
			branch := r.Branch
			if branch == "" {
				branch = "default"
			}
			h := byBranch[branch]
			if h == nil {
				h = &history{host: r.Host, session: r.Session, branch: branch, worked: worked}
				byBranch[branch] = h
			}
			if h.host != r.Host || h.session != r.Session {
				continue
			}
			h.records = append(h.records, r)
			ts, e := time.Parse(time.RFC3339Nano, r.TS)
			if e == nil && ts.After(h.latest) {
				h.latest = ts
			}
		}
		for _, h := range byBranch {
			if h.latest.IsZero() {
				continue
			}
			if o.Project.Key == "general" && (o.Now.Sub(h.latest) > 12*time.Hour || h.latest.After(o.Now.Add(time.Minute))) {
				continue
			}
			// Repaired entries are appended later but retain their original time.
			// Keep physical source refs while presenting the real chronology.
			sort.SliceStable(h.records, func(i, j int) bool { return parseTime(h.records[i].TS).Before(parseTime(h.records[j].TS)) })
			all = append(all, *h)
		}
	}
	sortHistories(all)
	return all, nil
}

// Only the final MiB is decoded. Prefix bytes are streamed to count newline
// boundaries, preserving show references without allocating a full transcript.
func readTail(ctx context.Context, f *os.File) ([]record, bool, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	start := info.Size() - tailBytes
	if start < 0 {
		start = 0
	}
	line := 1
	buf := make([]byte, 64<<10)
	remaining := start
	var carry []byte
	users, assistants, tools := 0, 0, 0
	count := func(data []byte) {
		joined := append(append([]byte{}, carry...), data...)
		for i, pat := range []string{`"role":"user","kind":"prompt"`, `"role":"assistant","kind":"reply"`, `"kind":"tool"`, `"kind":"plan"`} {
			for pos := 0; pos < len(joined); {
				n := bytes.Index(joined[pos:], []byte(pat))
				if n < 0 {
					break
				}
				end := pos + n + len(pat)
				if end > len(carry) {
					switch i {
					case 0:
						users++
					case 1:
						assistants++
					default:
						tools++
					}
				}
				pos = end
			}
		}
		if len(joined) > 80 {
			carry = append(carry[:0], joined[len(joined)-80:]...)
		} else {
			carry = append(carry[:0], joined...)
		}
	}
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		n := int64(len(buf))
		if remaining < n {
			n = remaining
		}
		got, err := io.ReadFull(f, buf[:n])
		count(buf[:got])
		line += bytes.Count(buf[:got], []byte{'\n'})
		remaining -= int64(got)
		if err != nil {
			return nil, false, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, tailBytes))
	if err != nil {
		return nil, false, err
	}
	if start > 0 { // Drop the first partial line unless the prefix ended at a newline.
		var last [1]byte
		_, _ = f.ReadAt(last[:], start-1)
		if last[0] != '\n' {
			at := bytes.IndexByte(data, '\n')
			if at < 0 {
				return nil, false, nil
			}
			data = data[at+1:]
			line++
		}
	}
	var out []record
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		var e inbox.Entry
		if json.Unmarshal(data[:i], &e) == nil {
			out = append(out, record{Entry: e, line: line})
		}
		data = data[i+1:]
		line++
	}
	return out, tools > 0 || users > 2 && assistants > 2, nil
}

func readPage(ctx context.Context, o Options) page {
	if o.Config.WikiPath == "" || ctx.Err() != nil {
		return page{}
	}
	root, err := paths.OpenRoot(o.Config.WikiPath, false)
	if err != nil {
		return page{}
	}
	defer root.Close()
	if o.Config.Instance != "" {
		schema, err := config.LoadSchema(filepath.Join(o.Config.WikiPath, ".eva-md", "schema.toml"))
		if err != nil || schema.Instance != o.Config.Instance {
			return page{}
		}
	}
	// Root presence is checked before the adapter so OKF cannot fall back to knowledge/.
	f, err := regular(root, "index.md")
	if err != nil {
		return page{}
	}
	f.Close()
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		return page{}
	}
	p := o.Project
	if p.Key != "general" {
		// Slug aliases are accepted only when now.md proves the normalized remote.
		ids := []string{p.Slug}
		d, err := root.Open("projects")
		if err == nil {
			entries, _ := d.ReadDir(256)
			d.Close()
			for _, e := range entries {
				if e.IsDir() && e.Name() != p.Slug {
					ids = append(ids, e.Name())
				}
			}
		}
		found := false
		for _, slug := range ids {
			if ctx.Err() != nil {
				return page{}
			}
			candidate, err := w.Read("projects/" + slug + "/now")
			if err != nil {
				continue
			}
			remote, _ := candidate.Concept.Extra["remote"].(string)
			if p.Remote != "" && remote != p.Remote {
				continue
			}
			if p.Remote == "" && slug != p.Slug {
				continue
			}
			p.Slug = slug
			found = true
			break
		}
		if !found {
			return page{}
		}
	}
	id := threadID(p)
	got, err := w.Read(id)
	if err != nil {
		return page{}
	}
	body := got.Concept.Body
	const begin = "<!-- eva:now:start -->"
	const end = "<!-- eva:now:end -->"
	if strings.Count(body, begin) != 1 || strings.Count(body, end) != 1 {
		return page{}
	}
	a, b := strings.Index(body, begin), strings.Index(body, end)
	if b <= a {
		return page{}
	}
	now := strings.TrimSpace(body[a+len(begin) : b])
	if now == "" {
		return page{}
	}
	as, _ := got.Concept.Extra["as_of"].(string)
	ts, _ := time.Parse(time.RFC3339Nano, as)
	// Initialization creates this undated placeholder before any chat exists.
	// It is structure, not saved memory, and must not make an empty project
	// look as though a previous conversation was found.
	if now == "No chats captured yet." && ts.IsZero() && len(got.Concept.Sources) == 0 {
		return page{}
	}
	head, _ := got.Concept.Extra["head"].(string)
	return page{now: now, hash: hash(now), id: id, head: head, asOf: ts}
}

func pageText(root *os.Root, name string) (string, error) {
	f, err := regular(root, name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > 2<<20 {
		return "", errors.New("resume: page exceeds 2 MiB")
	}
	b, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func parseTime(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }
func displayTime(s string) string {
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts.Format(time.RFC3339)
	}
	return "unknown"
}
func sourceDescription(r record) string {
	role := "unknown"
	switch r.Role {
	case "user", "assistant", "tool", "plan", "host", "event":
		role = r.Role
	}
	stamp := displayTime(r.TS)
	return fmt.Sprintf("[%s] %s · %s\n%s", role, stamp, inbox.Ref(r.Session, r.line), clean(quoted(r.Text)))
}
