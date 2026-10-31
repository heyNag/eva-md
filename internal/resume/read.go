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
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
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

// SessionFile is one of the project's session files as a brief listed it.
type SessionFile struct {
	Name, Path string    // its name and absolute path
	Host, ID   string    // from its name: the host, and the session id as the inbox names it
	Mod        time.Time // when it last changed
	Size       int64
}

// maxFiles is how many of the project's most recently changed session files
// a brief reads.
const maxFiles = 64

// readHistories returns the chats in the project's most recently changed
// session files, and the files it read, newest first, at most maxFiles.
func readHistories(ctx context.Context, o Options) ([]history, []SessionFile, error) {
	key := safeProject(o.Project)
	if key == "" {
		return nil, nil, errors.New("resume: missing project")
	}
	folder := filepath.Join(o.Dirs.Inbox(), key)
	root, err := paths.OpenRoot(folder, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, nil, err
	}
	names, err := dir.Readdir(-1)
	dir.Close()
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(names, func(i, j int) bool { return names[i].ModTime().After(names[j].ModTime()) })
	var all []history
	var files []SessionFile
	for _, info := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if len(all) >= 64 {
			break
		}
		if !info.Mode().IsRegular() || !strings.HasSuffix(info.Name(), ".jsonl") {
			continue
		}
		if host, id, ok := splitName(info.Name()); ok && len(files) < maxFiles {
			files = append(files, SessionFile{Name: info.Name(), Path: filepath.Join(folder, info.Name()), Host: host, ID: id, Mod: info.ModTime(), Size: info.Size()})
		}
		f, err := regular(root, info.Name())
		if err != nil {
			continue
		}
		recs, prefix, err := readTail(ctx, f)
		if err != nil {
			f.Close()
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			continue
		}
		// A branch switch within a session creates separate histories. Source
		// references retain the physical JSONL line numbers in either branch.
		byBranch := map[string]*history{}
		off := false
		for _, r := range recs {
			if !belongs(r.Entry, o, info.Name()) {
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
				h = &history{host: r.Host, session: r.Session, branch: branch, file: info.Name(), size: info.Size()}
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
		// Work before the tail matters only for a history whose tail shows
		// none, so the prefix is scanned for it only then.
		skip := false
		for _, h := range byBranch {
			if useful(*h) {
				continue
			}
			worked, err := prefixWork(ctx, f, prefix)
			if err != nil {
				if ctx.Err() != nil {
					f.Close()
					return nil, nil, ctx.Err()
				}
				skip = true
			}
			for _, h := range byBranch {
				h.worked = worked
			}
			break
		}
		f.Close()
		if skip {
			continue
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
	return all, files, nil
}

// splitName returns the host and the session id a session file's name,
// <host>-<id>.jsonl, holds.
func splitName(name string) (host, id string, ok bool) {
	base, ok := strings.CutSuffix(name, ".jsonl")
	if !ok {
		return "", "", false
	}
	for _, h := range []string{"claude-code", "codex"} {
		if id, found := strings.CutPrefix(base, h+"-"); found && id != "" {
			return h, id, true
		}
	}
	return "", "", false
}

// belongs reports whether an entry read from the session file named file is
// one of o's project, with a host eva-md captures and a session id free of
// control and format characters, and whether that file is its session's.
func belongs(e inbox.Entry, o Options, file string) bool {
	for _, c := range e.Session {
		if unicode.IsControl(c) || unicode.In(c, unicode.Cf) {
			return false
		}
	}
	if e.Project != o.Project.Key || e.Session == "" || (e.Host != "codex" && e.Host != "claude-code") {
		return false
	}
	return filepath.Base((inbox.Store{Root: o.Dirs.Inbox()}).Path(e.Project, e.Host, e.Session)) == file
}

// headBytes bounds the read for a chat's first prompt.
const headBytes = 64 << 10

// firstPrompts returns, for each history, its first user prompt on its branch,
// read from the first 64 KiB of its session file only; a whole file is never
// decoded. A history whose head holds no such prompt, or an off_record event
// before it, gets a zero record.
func firstPrompts(ctx context.Context, o Options, hs []*history) []record {
	out := make([]record, len(hs))
	if len(hs) == 0 {
		return out
	}
	root, err := paths.OpenRoot(filepath.Join(o.Dirs.Inbox(), safeProject(o.Project)), false)
	if err != nil {
		return out
	}
	defer root.Close()
	for i, h := range hs {
		if ctx.Err() != nil {
			break
		}
		out[i] = headPrompt(root, o, h)
	}
	return out
}

func headPrompt(root *os.Root, o Options, h *history) record {
	f, err := regular(root, h.file)
	if err != nil {
		return record{}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, headBytes))
	if err != nil {
		return record{}
	}
	// Only whole lines count; one the limit cut is left undecoded.
	for line := 1; ; line++ {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return record{}
		}
		var e inbox.Entry
		if json.Unmarshal(data[:i], &e) == nil && belongs(e, o, h.file) {
			if e.Kind == "event" && e.Text == "off_record" {
				return record{}
			}
			branch := e.Branch
			if branch == "" {
				branch = "default"
			}
			if e.Role == "user" && e.Kind == "prompt" && e.Host == h.host && e.Session == h.session && branch == h.branch {
				return record{Entry: e, line: line}
			}
		}
		data = data[i+1:]
	}
}

// Only the final MiB is decoded. Prefix bytes are streamed only to count
// newline boundaries, preserving show references without allocating a full
// transcript. readTail returns the prefix length for prefixWork.
func readTail(ctx context.Context, f *os.File) ([]record, int64, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	start := info.Size() - tailBytes
	if start < 0 {
		start = 0
	}
	line := 1
	buf := make([]byte, 64<<10)
	for remaining := start; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		n := int64(len(buf))
		if remaining < n {
			n = remaining
		}
		got, err := io.ReadFull(f, buf[:n])
		line += bytes.Count(buf[:got], []byte{'\n'})
		remaining -= int64(got)
		if err != nil {
			return nil, 0, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, tailBytes))
	if err != nil {
		return nil, 0, err
	}
	if start > 0 { // Drop the first partial line unless the prefix ended at a newline.
		var last [1]byte
		_, _ = f.ReadAt(last[:], start-1)
		if last[0] != '\n' {
			at := bytes.IndexByte(data, '\n')
			if at < 0 {
				return nil, start, nil
			}
			data = data[at+1:]
			line++
		}
	}
	var out []record
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
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
	return out, start, nil
}

// prefixWork reports whether the first end bytes of a session file hold real
// work: a tool or plan entry, or more than two prompts and two replies. It
// stops at the first sign of work.
func prefixWork(ctx context.Context, f *os.File, end int64) (bool, error) {
	r := io.NewSectionReader(f, 0, end)
	buf := make([]byte, 64<<10)
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
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		got, err := r.Read(buf)
		count(buf[:got])
		if tools > 0 || users > 2 && assistants > 2 {
			return true, nil
		}
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}

func readPage(ctx context.Context, o Options) page {
	if o.Config.WikiPath == "" || ctx.Err() != nil {
		return page{}
	}
	root, err := openWiki(o.Config)
	if err != nil {
		return page{}
	}
	defer root.Close()
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		return page{}
	}
	p := o.Project
	if p.Key != "general" {
		slugs := []string{p.Slug}
		if o.Variant != "refresh" {
			slugs = slugCandidates(root, p.Slug)
		} else if o.Slug != "" && plainName(o.Slug) {
			// A refresh, run on every prompt, reads only the slug its brief
			// found, or the project's own, never every project's now page.
			slugs = []string{o.Slug}
		}
		slug, ok := projectSlug(ctx, w, p, slugs)
		if !ok {
			return page{rules: readStanding(w, "")}
		}
		p.Slug = slug
	}
	pg := nowSection(w, threadID(p))
	if p.Key != "general" {
		pg.slug = p.Slug
	}
	pg.rules = readStanding(w, pg.slug)
	return pg
}

// errInstance reports a wiki whose schema doesn't name the config's instance.
var errInstance = errors.New("resume: wiki instance mismatch")

// openWiki opens the configured wiki for reading. Its root must hold a
// regular index.md, checked before okf is used so that okf cannot fall back
// to knowledge/, and when the config names an instance, the wiki's schema
// must name the same one.
func openWiki(cfg config.Config) (*os.Root, error) {
	if cfg.WikiPath == "" {
		return nil, errors.New("resume: no wiki is set up")
	}
	root, err := paths.OpenRoot(cfg.WikiPath, false)
	if err != nil {
		return nil, err
	}
	index, err := regular(root, "index.md")
	if err != nil {
		root.Close()
		return nil, errors.New("resume: wiki root index missing")
	}
	index.Close()
	if cfg.Instance != "" {
		schema, err := config.LoadSchema(filepath.Join(cfg.WikiPath, ".eva-md", "schema.toml"))
		if err != nil || schema.Instance != cfg.Instance {
			root.Close()
			return nil, errInstance
		}
	}
	return root, nil
}

// slugCandidates returns the project's own slug, then the other folders
// under projects/ that may hold it under another name, at most 256. A folder
// name is part of the page ids a brief or search shows, so it may not hold
// a line break.
func slugCandidates(root *os.Root, own string) []string {
	slugs := []string{own}
	d, err := root.Open("projects")
	if err != nil {
		return slugs
	}
	entries, _ := d.ReadDir(256)
	d.Close()
	for _, e := range entries {
		if e.IsDir() && e.Name() != own && plainName(e.Name()) {
			slugs = append(slugs, e.Name())
		}
	}
	return slugs
}

// projectSlug returns the first of slugs that is p's wiki folder. A slug
// alias is accepted only when its now page proves p's normalized remote; a
// project without a remote has only its own slug.
func projectSlug(ctx context.Context, w *okfadapter.Wiki, p identity.Project, slugs []string) (string, bool) {
	for _, slug := range slugs {
		if ctx.Err() != nil {
			return "", false
		}
		candidate, err := w.ReadMax("projects/"+slug+"/now", pageCap)
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
		return slug, true
	}
	return "", false
}

// plainName reports whether a folder name holds no control, format or space
// characters.
func plainName(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) || unicode.IsSpace(r) })
}

// nowSection reads the Now section of the thread page id.
func nowSection(w *okfadapter.Wiki, id string) page {
	got, err := w.ReadMax(id, pageCap)
	if err != nil {
		return page{}
	}
	body := got.Concept.Body
	if strings.Count(body, threadpage.NowStart) != 1 || strings.Count(body, threadpage.NowEnd) != 1 {
		return page{}
	}
	a, b := strings.Index(body, threadpage.NowStart), strings.Index(body, threadpage.NowEnd)
	if b <= a {
		return page{}
	}
	now := stripComments(strings.TrimSpace(body[a+len(threadpage.NowStart) : b]))
	if now == "" {
		return page{}
	}
	as, _ := got.Concept.Extra["as_of"].(string)
	ts, _ := time.Parse(time.RFC3339Nano, as)
	// Initialization creates this undated placeholder before any chat exists.
	// It is structure, not saved memory, and must not make an empty project
	// look as though a previous conversation was found.
	if now == threadpage.Placeholder && ts.IsZero() && len(got.Concept.Sources) == 0 {
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
	if info.Size() > pageCap {
		return "", errors.New("resume: page exceeds 2 MiB")
	}
	b, err := io.ReadAll(io.LimitReader(f, pageCap+1))
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
