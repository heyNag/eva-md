package resume

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/redact"
)

// SearchOptions describes one search of the inbox and the wiki's thread
// pages. The agent starts a search when it needs an earlier chat; no hook
// ever does.
type SearchOptions struct {
	Dirs    paths.Dirs
	Config  config.Config    // its wiki's thread pages are searched; there are none without a wiki
	Project identity.Project // searched together with the general area
	All     bool             // every project folder instead
	Pages   bool             // the thread pages only
	Inbox   bool             // the inbox only; with Pages, or without either, both are searched
	Terms   []string         // each a word or, with a space, a phrase
	Days    int              // the window in days for chats and thread pages; 0 for no limit
	Limit   int              // hits to print, 1 to 50 (default 10)
	Now     time.Time        // its location is the time zone hits are shown in
	TopUp   bool             // the search may index what the index lacks for the next one (see topUp)
}

const (
	searchFacts = "Lines marked [user] are the user's words; [assistant] lines are earlier suggestions, not decisions."
	pageFacts   = "On page lines, [stated] and [accepted] rest on the user's words, [observed] on a tool result the assistant reported, [suggested] on the assistant's and [inferred] on neither; [suggested] and [inferred] lines are not decisions."
	systemFacts = "[system] lines are notes an agent wrote from documentation and code, and [unverified] lines were not found in a source; \"code moved 14 commits\" counts the commits since the page's as_of that changed the code it cites."
	codeFacts   = "\"code:\" lines name the code a page cites; a path finds the pages that cite it or a folder holding it."
)

// maxSearchLine is the longest inbox line decoded, as for eva-md show.
const maxSearchLine = 2 << 20

// recordMarker is in every off_record and on_record event line, whatever
// the spacing, because the marker is the whole text value.
var recordMarker = []byte(`_record"`)

type term struct {
	text, lower string
	needle      []byte   // lower, for the byte prefilter
	token       bool     // under 3 characters: only whole tokens match
	filter      bool     // false when encoding/json may escape part of it
	path        pathTerm // the term read as a path, when it is one
}

// pathTerm is a term read as a path: one with a slash and no space,
// cleaned, with its lower case beside it.
type pathTerm struct {
	text, lower string
	abs         bool
}

// asPath reads a term as a path, or returns the zero pathTerm for a term
// that is none, or that leaves the repo it is read in.
func asPath(text string) pathTerm {
	if !strings.Contains(text, "/") || strings.Contains(text, " ") {
		return pathTerm{}
	}
	p := path.Clean(text)
	if p == "/" || p == ".." || strings.HasPrefix(p, "../") {
		return pathTerm{}
	}
	return pathTerm{text: p, lower: strings.ToLower(p), abs: strings.HasPrefix(p, "/")}
}

// citedPath is one code source on a system or knowledge page's code line:
// where it starts in the lowered line and where its repo and commit end
// there, the project key of its repo, and the path it cites, "" for the
// whole repo, with its lower case.
type citedPath struct {
	at, repo         int
	key, path, lower string
}

// cite returns the citedPath of a code source that starts at byte at of the
// lowered code line, r being what coderef.Parse read of it.
func cite(at int, source string, r coderef.Ref) citedPath {
	head := source
	if r.Path != "" {
		// The path follows the commit's colon; Parse reads the last commit.
		if i := strings.LastIndex(source, "@"+r.Commit+":"); i >= 0 {
			head = source[:i+1+len(r.Commit)]
		}
	}
	return citedPath{at: at, repo: at + len(strings.ToLower(head)), key: r.Key, path: r.Path, lower: strings.ToLower(r.Path)}
}

// inRepo returns where, in a code line's lowered text, the first source whose
// repo and commit hold term t as text starts its hit, and whether one does
// in the case typed; or -1. A path term finds a page that cites a repo by
// the repo's name, but a cited path only as a path (see codeScope).
func inRepo(text, lower string, code []citedPath, t term) (int, bool) {
	at, exact := -1, false
	for _, cp := range code {
		i := find(lower[cp.at:cp.repo], t.lower, t.token)
		if i < 0 {
			continue
		}
		if at < 0 {
			at = cp.at + i
		}
		exact = exact || find(text[origin(text, cp.at):origin(text, cp.repo)], t.text, t.token) >= 0
	}
	return at, exact
}

// codeScope is where a path term is read: in the project's repo, by its key
// and its worktree's root, or with --all in any repo a page cites a folder
// or a file of.
type codeScope struct {
	key, root string
	all       bool
}

// within returns where, in a code line's lowered text, the first source that
// holds path p starts, and whether one holds it in the case typed; or -1.
func (c codeScope) within(code []citedPath, p pathTerm) (int, bool) {
	at, exact := -1, false
	if p.text == "" {
		return at, exact
	}
	for _, cp := range code {
		ok, ex := c.holds(cp, p)
		if !ok {
			continue
		}
		if at < 0 {
			at = cp.at
		}
		exact = exact || ex
	}
	return at, exact
}

// holds reports whether a cited source holds path p, ignoring case, and
// whether it does in the case typed. A relative path is read in the
// project's repo, where a page that cites the whole repo holds it too, or
// with --all in any repo's cited folder or file. An absolute path in the
// project's worktree is read relative to it, and any other by its end, as
// when it names a folder a page cites inside another clone. A whole-repo
// source is never a hit in the case typed, so a page citing the very folder
// ranks first.
func (c codeScope) holds(cp citedPath, p pathTerm) (bool, bool) {
	rel, lower, mine := p.text, p.lower, false
	if p.abs {
		r, ok := strings.CutPrefix(p.text, c.root+"/")
		if c.key == "" || c.root == "" || !ok {
			if cp.path == "" {
				return false, false
			}
			found := endsIn(p.lower, cp.lower)
			return found, found && endsIn(p.text, cp.path)
		}
		rel, lower, mine = r, strings.ToLower(r), true
	}
	if c.key == "" || cp.key != c.key {
		if mine || !c.all || cp.path == "" {
			return false, false
		}
	}
	if cp.path == "" {
		return true, false
	}
	found := under(lower, cp.lower)
	return found, found && under(rel, cp.path)
}

// under reports whether path p is cited path c or lies in the folder c.
func under(p, c string) bool { return p == c || strings.HasPrefix(p, c+"/") }

// endsIn reports whether absolute path p ends with cited path c, or holds it
// as a folder.
func endsIn(p, c string) bool { return strings.HasSuffix(p, "/"+c) || strings.Contains(p, "/"+c+"/") }

type query struct {
	terms  []term // as given
	filter []term // the prefilter, longest first
	phrase string // every term in order, for the phrase bonus
	since  time.Time
}

type source struct {
	dir, name string
	key       string // the project the folder must hold, when known
	mod       time.Time
	size      int64
	ino       uint64
	from      int64 // where a scan of its tail starts, after line lines, off the record or not
	line      int
	off       bool
	to        int64 // where a scan of it ends, or 0 for its end
}

// unit is one text a search matches: an inbox entry or a line of a thread
// page, a system page or a knowledge page.
type unit struct {
	text, shown     string // text is matched; shown, which starts with text, is what a snippet is cut from
	ts              time.Time
	day             bool   // ts holds only a day
	role            string // the speaker, or a page line's tag
	branch, project string
	session         string // an inbox entry's
	ref             string // a page line's page id and section
	file            string // for the last tie-break
	line            int
	page            bool
	system          bool    // a system page's line, which has no branch
	knowledge       bool    // a knowledge page's line, which has no branch either
	tool            bool    // an inbox tool line
	score           float64 // its score, when scored is set: the index's
	scored          bool
	code            []citedPath // a system or knowledge page's code line: the sources a path may lie in
}

type hit struct {
	matched                        int
	exact, page, day, system, code bool // code: a page's code line
	knowledge                      bool
	ts                             time.Time
	role, branch, project, session string
	ref                            string // a page line's page id and section
	file                           string // folder and name, the last tie-break
	line, term                     int    // term: the one matched first
	window                         string // text around the first match
	anchor                         string // the code source that holds a path term, when that was the first match
	before, after                  bool   // whether the window cut the text
	score                          float64
}

// ranking keeps the best limit hits while counting every match.
type ranking struct {
	limit, total int
	top          []hit // best first
	about        bool  // total counts candidates of the index not read again
}

type scan struct {
	q                          *query
	all, any                   ranking
	inbox, pages               bool // the sources in scope
	files, unreadable, badDirs int
	bytes                      int64
	dirs                       map[string]bool
	cut                        bool
	wiki, noWiki               bool   // the wiki was searched, or is not set up
	badWiki                    string // why the wiki could not be opened, if it couldn't
	unproved                   string // the project's folder, when no now page shows it is the project's
	cutPages                   bool   // the time limit ended the scan among the pages
	pageCount, systemCount     int    // the thread pages and the system pages scanned
	knowledgeCount             int    // ... and the knowledge pages
	pageBytes                  int64
	badPages, badFolders       []string // the pages and wiki folders that could not be read
	line, lower                []byte   // reused for every line
	ix                         *indexState
	moves                      map[string]coderef.Movement // how far each system page's code moved, for the hits shown
	here                       codeScope                   // where a path term is read
}

// Search finds earlier chat text in the inbox: prompts, replies, tool and
// plan lines, never events and never anything said off the record. It also
// searches the Now items and Timeline lines of the wiki's thread pages, and
// the names, descriptions, cited code and section lines of its system pages,
// and the statements and cited code of its knowledge pages, where a path
// also finds the pages that cite it or a folder holding it. It reads one
// file at a time, line by line, a knowledge page whole: the pages first,
// then the inbox files newest first, decoding only inbox lines that can hold
// every term.
// Only when nothing holds every term does it fall back to what holds any of
// them. Running out of time ends the scan early, which the trailer says;
// that is not an error.
func Search(ctx context.Context, o SearchOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	start := time.Now()
	if o.Now.IsZero() {
		o.Now = start
	}
	if o.Limit < 1 {
		o.Limit = 10
	}
	o.Limit = min(o.Limit, 50)
	q := newQuery(o.Terms)
	if len(q.terms) == 0 {
		return "", errors.New("resume: search needs at least one term")
	}
	if o.Days > 0 {
		q.since = o.Now.AddDate(0, 0, -o.Days)
	}
	scope := map[string]string{}
	if !o.All {
		key := safeProject(o.Project)
		if key == "" {
			return "", errors.New("resume: missing project")
		}
		scope[key] = o.Project.Key
		scope[safeProject(identity.General())] = identity.GeneralKey
	}
	s := &scan{q: q, all: ranking{limit: o.Limit}, any: ranking{limit: o.Limit}, dirs: map[string]bool{}, inbox: o.Inbox || !o.Pages, pages: o.Pages || !o.Inbox}
	s.here.all = o.All
	if o.Project.Key != "" && o.Project.Key != identity.GeneralKey {
		s.here.key = o.Project.Key
		if filepath.IsAbs(o.Project.Root) {
			s.here.root = filepath.Clean(o.Project.Root)
		}
	}
	// The search index's statistics score page lines too, so it opens first.
	s.openIndex(o, scope)
	defer s.closeIndex()
	// The pages go first: they are few, and their lines rank first.
	if s.pages {
		s.searchPages(ctx, o)
	}
	var prefixes map[string]int
	if s.inbox && !s.cut {
		root, err := paths.OpenRoot(o.Dirs.Inbox(), false)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return "", err
		default:
			defer root.Close()
			var sources []source
			sources, prefixes, err = s.list(root, o.All, scope)
			if err != nil {
				return "", err
			}
			sources = s.searchIndex(ctx, root, sources)
			for _, src := range sources {
				if s.cut = ctx.Err() != nil; s.cut {
					break
				}
				s.file(ctx, root, src)
				if s.cut {
					break
				}
			}
			s.finishIndex(ctx, root, o, start)
		}
	}
	s.readMoves(ctx, o)
	return s.report(o, prefixes, time.Since(start)), nil
}

// readMoves reads how far each system page's code moved, from the code
// status (see CodeMoves), once and only when a system page's line is among
// the hits the report shows.
func (s *scan) readMoves(ctx context.Context, o SearchOptions) {
	if r, _ := s.shown(); slices.ContainsFunc(r.top, func(h hit) bool { return h.system }) {
		s.moves = CodeMoves(ctx, Options{Dirs: o.Dirs, Config: o.Config})
	}
}

// moved says how far the code of the system page a hit is on moved, as
// "code moved 14 commits" or "code unchanged", or "" for a hit of another
// kind and a page whose code the status didn't measure.
func (s *scan) moved(h hit) string {
	if !h.system {
		return ""
	}
	slug, _, _ := strings.Cut(strings.TrimPrefix(h.ref, "systems/"), "#")
	if m, ok := s.moves[slug]; ok {
		return m.Words()
	}
	return ""
}

func newQuery(args []string) *query {
	q := &query{}
	seen := map[string]bool{}
	var lowers []string
	for _, a := range args {
		text := strings.Join(strings.Fields(a), " ")
		lower := strings.ToLower(text)
		if text == "" || seen[lower] {
			continue
		}
		seen[lower] = true
		t := term{text: text, lower: lower, needle: []byte(lower), token: utf8.RuneCountInString(text) < 3, filter: !escapable(text), path: asPath(text)}
		q.terms = append(q.terms, t)
		if t.filter {
			q.filter = append(q.filter, t)
		}
		lowers = append(lowers, lower)
	}
	sort.SliceStable(q.filter, func(i, j int) bool { return len(q.filter[i].needle) > len(q.filter[j].needle) })
	if len(lowers) > 1 {
		q.phrase = strings.Join(lowers, " ")
	}
	return q
}

// escapable reports whether encoding/json may write part of s as an escape,
// in which case a raw line can match without holding s.
func escapable(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return strings.ContainsRune(`"\<>&`, r) || unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == utf8.RuneError
	})
}

// list returns the files in scope and inside the window, newest first. It
// also counts every session file in the inbox by the first 8 characters of
// its id, because eva-md show looks for a ref in every project folder; a
// session with two files counts twice.
func (s *scan) list(root *os.Root, all bool, scope map[string]string) ([]source, map[string]int, error) {
	d, err := root.Open(".")
	if err != nil {
		return nil, nil, err
	}
	dirs, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return nil, nil, err
	}
	var out []source
	prefixes := map[string]int{}
	for _, dir := range dirs {
		if !dir.IsDir() || strings.HasPrefix(dir.Name(), ".") {
			continue
		}
		key, in := scope[dir.Name()]
		in = in || all
		var files []fs.DirEntry
		f, err := root.Open(dir.Name())
		if err == nil {
			files, err = f.ReadDir(-1)
			f.Close()
		}
		if err != nil {
			if in {
				s.badDirs++
			}
			continue
		}
		for _, file := range files {
			sid, ok := sessionName(file.Name())
			if !ok || !file.Type().IsRegular() {
				continue
			}
			prefixes[prefix(sid)]++
			if !in {
				continue
			}
			info, err := file.Info()
			if err != nil {
				s.unreadable++
				continue
			}
			if info.ModTime().Before(s.q.since) {
				continue
			}
			out = append(out, source{dir: dir.Name(), name: file.Name(), key: key, mod: info.ModTime(), size: info.Size(), ino: statIno(info)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].mod.Equal(out[j].mod) {
			return out[i].mod.After(out[j].mod)
		}
		return out[i].dir+"/"+out[i].name < out[j].dir+"/"+out[j].name
	})
	return out, prefixes, nil
}

// sessionName returns the session part of a capture file's name.
func sessionName(file string) (string, bool) {
	_, sid, ok := splitName(file)
	return sid, ok
}

// prefix is the part of a session id a short ref keeps (see inbox.Ref).
func prefix(sid string) string {
	r := []rune(sid)
	if len(r) > 8 {
		r = r[:8]
	}
	return string(r)
}

// file scans one session file, or only its tail from src.from when the
// search index covers the rest. Line numbers are physical and 1-based, as
// the inbox and eva-md show count them.
func (s *scan) file(ctx context.Context, root *os.Root, src source) {
	f, err := regular(root, filepath.Join(src.dir, src.name))
	if err != nil {
		s.unreadable++
		return
	}
	defer f.Close()
	s.tail(f, &src)
	var rd io.Reader = f
	if src.to > 0 {
		rd = io.LimitReader(f, src.to-src.from)
	}
	r := bufio.NewReaderSize(rd, 64<<10)
	off := src.off
	for n := src.line + 1; ; n++ {
		if ctx.Err() != nil {
			s.cut = true
			break
		}
		line, ok, err := s.next(r)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.unreadable++
				return
			}
			break
		}
		if ok {
			off = s.entry(line, n, src, off)
		}
	}
	s.files++
	s.dirs[src.dir] = true
}

// next reads one physical line of an inbox file without its newline. A
// line over maxSearchLine comes back with ok false. An unterminated last line
// is left unread, as a write may still be in progress.
func (s *scan) next(r *bufio.Reader) ([]byte, bool, error) {
	line, ok, size, err := readLine(r, &s.line, maxSearchLine, false)
	s.bytes += int64(size)
	return line, ok, err
}

// readLine reads one physical line from r without its newline, into buf
// when it doesn't fit r's buffer. A line over max bytes comes back with ok
// false. size counts the bytes read. An unterminated last line comes back
// only with tail set: a page is replaced whole, while an inbox file may
// still be written. err is io.EOF after the last line.
func readLine(r *bufio.Reader, buf *[]byte, max int, tail bool) (line []byte, ok bool, size int, err error) {
	*buf = (*buf)[:0]
	long := false
	for {
		frag, err := r.ReadSlice('\n')
		size += len(frag)
		if err == nil && len(*buf) == 0 && !long && len(frag) <= max+1 {
			return frag[:len(frag)-1], true, size, nil
		}
		if long || len(*buf)+len(frag) > max+1 {
			long, *buf = true, (*buf)[:0]
		} else {
			*buf = append(*buf, frag...)
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && tail && (long || len(*buf) > 0):
			return *buf, !long, size, nil
		case err != nil:
			return nil, false, size, err
		case long:
			return nil, false, size, nil
		}
		return (*buf)[:len(*buf)-1], true, size, nil
	}
}

// entry handles one line and returns the file's off-the-record state, which
// only marker events change, in file order, exactly as in readHistories.
func (s *scan) entry(line []byte, n int, src source, off bool) bool {
	keep := s.prefilter(line)
	if !keep && !bytes.Contains(line, recordMarker) {
		return off
	}
	var e inbox.Entry
	if json.Unmarshal(line, &e) != nil || !s.trusted(e, src) {
		return off
	}
	if e.Kind == "event" && e.Text == "off_record" {
		return true
	}
	if e.Kind == "event" && e.Text == "on_record" {
		return false
	}
	if off || !keep {
		return off
	}
	switch e.Kind {
	case "prompt", "reply", "tool", "plan":
	default:
		return off
	}
	ts, err := time.Parse(time.RFC3339Nano, e.TS)
	if err != nil {
		ts = time.Time{}
	}
	if !s.q.since.IsZero() && (ts.IsZero() || ts.Before(s.q.since)) {
		return off
	}
	if s.ix != nil && s.ix.read[src.dir+"/"+src.name][n] {
		return off
	}
	s.match(unit{text: e.Text, shown: e.Text, ts: ts, role: speaker(e.Role), branch: e.Branch, project: e.Project, session: e.Session, file: src.dir + "/" + src.name, line: n, tool: e.Kind == "tool"})
	return off
}

// prefilter reports whether a raw line can hold a match, before any decoding.
// The lowered line must hold every term usable as a filter, longest first.
// Until some entry holds every term, a line holding any term is kept too, for
// the fallback. It never drops a line whose decoded text matches: a term
// encoding/json might escape is not used as a filter.
func (s *scan) prefilter(line []byte) bool {
	s.lower = appendLower(s.lower[:0], line)
	every, some := true, len(s.q.filter) < len(s.q.terms)
	for _, t := range s.q.filter {
		if bytes.Contains(s.lower, t.needle) {
			some = true
			continue
		}
		every = false
		if s.all.total > 0 {
			break
		}
	}
	return every || some && s.all.total == 0
}

// appendLower appends the lower case of b, rune by rune, as strings.ToLower
// maps the decoded text.
func appendLower(dst, b []byte) []byte {
	for i := 0; i < len(b); {
		if c := b[i]; c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			dst = append(dst, c)
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		dst = utf8.AppendRune(dst, unicode.ToLower(r))
		i += size
	}
	return dst
}

// trusted applies readHistories' checks: an entry must belong to the file it
// was read from, with a host eva-md captures and a session id free of
// control and format characters. The search index applies the same checks.
func (s *scan) trusted(e inbox.Entry, src source) bool {
	return inbox.Trusted(e, src.dir, src.name, src.key)
}

// match ranks one unit against the terms. On a system or knowledge page's
// code line a path term matches a cited path only where a source holds it
// as a path (see codeScope), never as text, so the scope and a folder's
// bounds hold for the cited folder itself and for a prefix of its name; it
// still matches a source's repo and commit as text (see inRepo). A hit in
// the case typed counts as an exact hit.
func (s *scan) match(u unit) {
	lower := strings.ToLower(u.text)
	matched, first, which, exactPath, viaPath := 0, -1, 0, false, false
	for i, t := range s.q.terms {
		at, via := -1, false
		if u.code != nil && t.path.text != "" {
			var exact bool
			if at, exact = inRepo(u.text, lower, u.code, t); at < 0 {
				at, exact = s.here.within(u.code, t.path)
				via = at >= 0
			}
			exactPath = exactPath || exact
		} else {
			at = find(lower, t.lower, t.token)
		}
		if at < 0 {
			continue
		}
		matched++
		if first < 0 || at < first {
			first, which, viaPath = at, i, via
		}
	}
	r := &s.all
	switch {
	case matched == 0:
		return
	case matched < len(s.q.terms):
		if s.all.total > 0 {
			return
		}
		r = &s.any
	}
	r.total++
	h := hit{matched: matched, exact: exactPath || s.q.exact(u.text, lower, u.code != nil), page: u.page, ts: u.ts, file: u.file, line: u.line, score: s.score(u)}
	i := r.slot(&h)
	if i < 0 {
		return
	}
	h.day, h.system, h.knowledge, h.code, h.role, h.branch, h.project, h.session, h.ref, h.term = u.day, u.system, u.knowledge, u.code != nil, u.role, u.branch, u.project, u.session, u.ref, which
	if h.branch == "" {
		h.branch = "default"
	}
	// text starts shown, so an offset in one is the same in the other.
	h.window, h.before, h.after = window(u.shown, origin(u.text, first))
	if viaPath {
		// The source that holds the path, which the snippet shows.
		h.anchor, _, _ = strings.Cut(u.text[origin(u.text, first):], ", ")
	}
	r.insert(i, h)
}

// find returns the byte offset of the first match of needle in s, or -1. A
// token match must not touch a letter or digit on either side.
func find(s, needle string, token bool) int {
	for from := 0; from <= len(s); {
		i := strings.Index(s[from:], needle)
		if i < 0 {
			return -1
		}
		i += from
		if !token || edge(s, i, i+len(needle)) {
			return i
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		from = i + size
	}
	return -1
}

func edge(s string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(s[:start])
	after, _ := utf8.DecodeRuneInString(s[end:])
	return !alnum(before) && !alnum(after)
}

func alnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

// exact reports a hit on a term in the case it was typed, or on all the terms
// together as one phrase. On a code line, match judges a path term.
func (q *query) exact(text, lower string, code bool) bool {
	for _, t := range q.terms {
		if code && t.path.text != "" {
			continue
		}
		if find(text, t.text, t.token) >= 0 {
			return true
		}
	}
	return q.phrase != "" && strings.Contains(lower, q.phrase)
}

// origin maps a byte offset in strings.ToLower(s) to the same rune in s;
// lowering can change a rune's length.
func origin(s string, lowered int) int {
	n := 0
	for i, r := range s {
		if n >= lowered {
			return i
		}
		n += utf8.RuneLen(unicode.ToLower(r))
	}
	return len(s)
}

// locate returns the byte offset in s of the first match of t, ignoring case,
// or -1.
func locate(s string, t term) int {
	at := find(strings.ToLower(s), t.lower, t.token)
	if at < 0 {
		return -1
	}
	return origin(s, at)
}

// window keeps the text around byte offset at, enough for a snippet once
// whitespace is collapsed, and reports whether it cut the text before or
// after.
func window(text string, at int) (string, bool, bool) {
	start, end := max(at-300, 0), min(at+900, len(text))
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	return text[start:end], start > 0, end < len(text)
}

// better orders hits: more distinct terms, then an exact-case or phrase hit,
// which together make the best matches, then page lines before inbox
// entries, then the higher score, which is 0 without the search index,
// then newest. File and line only make the order total.
func better(a, b *hit) bool {
	switch {
	case a.matched != b.matched:
		return a.matched > b.matched
	case a.exact != b.exact:
		return a.exact
	case a.page != b.page:
		return a.page
	case a.score != b.score:
		return a.score > b.score
	case !a.ts.Equal(b.ts):
		return a.ts.After(b.ts)
	case a.file != b.file:
		return a.file < b.file
	}
	return a.line > b.line
}

// slot returns where h goes among the best hits, or -1 when it misses them.
func (r *ranking) slot(h *hit) int {
	i := sort.Search(len(r.top), func(i int) bool { return better(h, &r.top[i]) })
	if i >= r.limit {
		return -1
	}
	return i
}

func (r *ranking) insert(i int, h hit) {
	r.top = append(r.top, hit{})
	copy(r.top[i+1:], r.top[i:])
	r.top[i] = h
	if len(r.top) > r.limit {
		r.top = r.top[:r.limit]
	}
}

func speaker(role string) string {
	switch role {
	case "user", "assistant", "tool", "plan", "host", "event":
		return role
	}
	return "unknown"
}

// shown is the ranking the report prints, and whether it is the fallback:
// what holds every term, or, when nothing does, what holds any of them.
func (s *scan) shown() (ranking, bool) {
	if s.all.total == 0 {
		return s.any, true
	}
	return s.all, false
}

func (s *scan) report(o SearchOptions, prefixes map[string]int, took time.Duration) string {
	r, some := s.shown()
	var lines []string
	if r.total == 0 {
		lines = append(lines, "No matches.")
	} else {
		lines = append(lines, s.label(some))
		for _, h := range r.top {
			lines = append(lines, hitLine(h, s.q.terms[h.term], o, prefixes, s.moved(h)))
		}
		if more := r.total - len(r.top); more > 0 {
			about := ""
			if r.about {
				about = "about "
			}
			lines = append(lines, fmt.Sprintf("%s%d more; refine with other terms", about, more))
		}
	}
	return strings.Join(append(lines, s.trailer(o, took)), "\n")
}

// label says, as facts, what was searched, for which terms, in what order,
// and what the speakers and tags mean.
func (s *scan) label(some bool) string {
	var names []string
	for _, t := range s.q.terms {
		names = append(names, `"`+plain(t.text, 80)+`"`)
	}
	terms := cut(strings.Join(names, " "), 300)
	what, order, facts := "earlier chat text", "best matches first, then newest", searchFacts
	pages := "thread page lines"
	switch {
	case s.systemCount > 0 && s.knowledgeCount > 0:
		pages = "thread, system and knowledge page lines"
	case s.systemCount > 0:
		pages = "thread and system page lines"
	case s.knowledgeCount > 0:
		pages = "thread and knowledge page lines"
	}
	switch {
	case s.wiki && s.inbox:
		what, order, facts = "earlier chat text and "+pages, "best matches first, page lines before chat text, then newest", searchFacts+" "+pageFacts
	case s.wiki:
		what, facts = pages, pageFacts
	}
	if s.systemCount > 0 {
		facts += " " + systemFacts
	}
	r, _ := s.shown()
	if slices.ContainsFunc(r.top, func(h hit) bool { return h.knowledge }) {
		facts += " " + knowledgeFacts
	}
	if slices.ContainsFunc(r.top, func(h hit) bool { return h.code }) {
		facts += " " + codeFacts
	}
	order = s.rankedOrder(order)
	if some {
		return "eva.md search: " + what + " matching some of " + terms + " (none matched all), " + order + ". " + facts
	}
	return "eva.md search: " + what + " matching " + terms + ", " + order + ". " + facts
}

// hitLine starts with the speaker from the entry's role field, or the tag a
// page line carries. Everything after it is on the same line, so no text can
// start a line of its own. A page line's ref names its page and section,
// which eva-md show opens. A system page's line has no branch, and after its
// ref says how far the code the page cites moved, when moved says it.
func hitLine(h hit, t term, o SearchOptions, prefixes map[string]int, moved string) string {
	when := "unknown"
	if !h.ts.IsZero() {
		layout := "2006-01-02 15:04"
		if h.day {
			layout = time.DateOnly
		}
		when = h.ts.In(o.Now.Location()).Format(layout)
	}
	ref := h.ref
	if !h.page {
		// eva-md show accepts the full id where a short ref would be ambiguous.
		ref = inbox.Ref(h.session, h.line)
		if prefixes[prefix(h.session)] > 1 {
			ref = "s:" + h.session + "#" + strconv.Itoa(h.line)
		}
	}
	parts := []string{"[" + h.role + "] " + when}
	if o.All {
		parts = append(parts, ident(h.project, 120))
	}
	if !h.system && !h.knowledge {
		parts = append(parts, ident(h.branch, 80))
	}
	parts = append(parts, ident(ref, 600))
	if moved != "" {
		parts = append(parts, moved)
	}
	parts = append(parts, snippet(h, t))
	return strings.Join(parts, " · ")
}

// plain keeps a value on one cleaned line.
func plain(s string, n int) string { return oneLine(clean(s), n) }

// ident keeps one of eva-md's identifiers on one line. The inbox stores them
// in a form redact.Identifier leaves as it is; clean would let the entropy
// rule rewrite a name such as local-token-refresh-worker.
func ident(s string, n int) string { s, _ = redact.Identifier(s, 0); return oneLine(s, n) }

// snippet shows at most 200 characters around the first match, on one line,
// cleaned and unforged as the brief's text is. A page line keeps the
// identifiers eva-md show keeps (see cleanPage).
func snippet(h hit, t term) string {
	text := clean(h.window)
	if h.page {
		text = cleanPage(h.window)
	}
	s := oneLine(escape(text), 1<<20)
	at := locate(s, t)
	if at < 0 && h.anchor != "" {
		at = strings.Index(s, h.anchor)
	}
	at = max(at, 0)
	r := []rune(s)
	const width = 198 // leaves room for both cut marks
	i := utf8.RuneCountInString(s[:at])
	start := max(min(i-60, len(r)-width), 0)
	end := min(start+width, len(r))
	out := string(r[start:end])
	if h.before || start > 0 {
		out = "…" + out
	}
	if h.after || end < len(r) {
		out += "…"
	}
	return out
}

// trailer says what was scanned, and names what could not be: an
// unreadable page or wiki folder is never left to look like a page with no
// hits. The system pages and the knowledge pages are counted beside the
// thread pages, when there are any.
func (s *scan) trailer(o SearchOptions, took time.Duration) string {
	window := "all dates"
	if o.Days > 0 {
		window = fmt.Sprintf("last %d %s", o.Days, plural(o.Days, "day"))
	}
	var scanned []string
	if s.inbox {
		scanned = append(scanned, fmt.Sprintf("%d %s (%.1f MB) in %d %s", s.files, plural(s.files, "file"), float64(s.bytes)/1e6, len(s.dirs), plural(len(s.dirs), "project")))
	}
	// "in 1 project and 2 thread pages", or a list: "in 1 project, 2 thread
	// pages, 1 system page and 3 knowledge pages".
	and := " and "
	if s.wiki || !s.inbox {
		counts := []string{fmt.Sprintf("%d thread %s", s.pageCount, plural(s.pageCount, "page"))}
		if s.systemCount > 0 {
			counts = append(counts, fmt.Sprintf("%d system %s", s.systemCount, plural(s.systemCount, "page")))
		}
		if s.knowledgeCount > 0 {
			counts = append(counts, fmt.Sprintf("%d knowledge %s", s.knowledgeCount, plural(s.knowledgeCount, "page")))
		}
		pages := counts[len(counts)-1]
		if n := len(counts); n > 1 {
			pages = strings.Join(counts[:n-1], ", ") + " and " + pages
			and = ", "
		}
		scanned = append(scanned, fmt.Sprintf("%s (%.1f MB)", pages, float64(s.pageBytes)/1e6))
	}
	notes := []string{"Scanned " + strings.Join(scanned, and) + ", " + window + ", " + duration(took)}
	switch {
	case s.cutPages && s.inbox:
		notes = append(notes, "cut short by the time limit; some wiki pages and the inbox were not searched")
	case s.cutPages:
		notes = append(notes, "cut short by the time limit; some wiki pages were not searched")
	case s.cut:
		notes = append(notes, "cut short by the time limit; older files were not searched")
	}
	notes = append(notes, s.indexNotes(o)...)
	if s.unreadable > 0 {
		notes = append(notes, fmt.Sprintf("%d %s unreadable", s.unreadable, plural(s.unreadable, "file")))
	}
	if s.badDirs > 0 {
		notes = append(notes, fmt.Sprintf("%d project %s unreadable", s.badDirs, plural(s.badDirs, "folder")))
	}
	switch {
	case s.noWiki && !s.inbox:
		notes = append(notes, "no wiki is set up, so no wiki pages were searched")
	case s.badWiki != "":
		notes = append(notes, s.badWiki+", so no wiki pages were searched")
	}
	if s.unproved != "" {
		notes = append(notes, ident(s.unproved, 200)+" has no readable now page for this project, so its thread pages were not searched")
	}
	if n := len(s.badFolders); n > 0 {
		notes = append(notes, fmt.Sprintf("%d wiki %s unreadable: %s", n, plural(n, "folder"), named(s.badFolders)))
	}
	if n := len(s.badPages); n > 0 {
		notes = append(notes, fmt.Sprintf("%d %s unreadable: %s", n, plural(n, "page"), named(s.badPages)))
	}
	return strings.Join(notes, "; ") + "."
}

// named lists up to 3 names, cleaned as identifiers, and how many more there
// are.
func named(names []string) string {
	var shown []string
	for _, n := range names[:min(len(names), 3)] {
		shown = append(shown, ident(n, 200))
	}
	out := strings.Join(shown, ", ")
	if len(names) > 3 {
		out += fmt.Sprintf(" and %d more", len(names)-3)
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func duration(d time.Duration) string {
	if d < time.Second {
		return strconv.FormatInt(d.Milliseconds(), 10) + " ms"
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}
