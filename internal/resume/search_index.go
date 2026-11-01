package resume

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/searchindex"
)

// The search index (internal/searchindex) is a cache built from the inbox
// that proposes the inbox lines that may hold a search's terms, with their
// scores. Search reads every line it proposes again from its file and
// matches it as it matches a line it scans, so the index never changes a
// hit's text. But it decides which lines are read, so in the files it
// covers a match inside a word, in text without spaces, or a rarer
// exact-case hit past the candidates read can be left out when other hits
// are found. What it does not cover, such as the newest lines of a session
// file, is scanned. Without an index, a search runs as it always has.

// topUpMargin is the time a search keeps for itself after a top-up.
const topUpMargin = 250 * time.Millisecond

// indexHook runs at each stage of the index's part in a search, as its
// candidates holding every term and then those holding some are read; a
// variable so tests can make the index path fail.
var indexHook func(stage string)

// indexState is what a search takes from the index and says about it.
type indexState struct {
	ix    *searchindex.Index // nil when the query is left to the scan
	q     *searchindex.Query
	stats searchindex.Stats // pooled over the segments opened; N is 0 without any
	since int64             // the window in Unix seconds, math.MinInt64 for none
	limit int
	off   bool // a fault turned the index off for this search

	listed  map[string]source // the listed files the index covers, by folder/name
	gens    map[string]uint32 // their generations in the manifests
	covered []source          // the same files, newest first, each up to where the index stopped

	files     int   // files the index covered
	bytes     int64 // their bytes it covered
	unindexed map[string]int64
	newest    map[string]time.Time // per folder, its newest file in scope
	unusable  []string             // folders whose index couldn't be used
	noIndex   []string             // folders in scope with no index, while others had one
	some      []searchindex.Candidate
	someTotal int
	stale     int                     // candidates whose line was gone or changed
	fill      bool                    // too few of the candidates read held a term that isn't plain
	read      map[string]map[int]bool // the lines verify matched, by file, which a scan skips
	fallback  bool
	fallFiles int
	fallBytes int64
	topFiles  int
	topBytes  int64

	sources []source // the files listed, as the scan would read them
	before  snapshot // the search before the index's part in it
}

// snapshot is a search's state before the index's part in it, which a fault
// puts back before every file is scanned as without an index.
type snapshot struct {
	all, any          ranking
	files, unreadable int
	bytes             int64
	dirs              map[string]bool
}

func (s *scan) snapshot() snapshot {
	return snapshot{all: s.all.clone(), any: s.any.clone(), files: s.files, unreadable: s.unreadable, bytes: s.bytes, dirs: maps.Clone(s.dirs)}
}

func (s *scan) restore(b snapshot) {
	s.all, s.any, s.files, s.unreadable, s.bytes, s.dirs = b.all.clone(), b.any.clone(), b.files, b.unreadable, b.bytes, maps.Clone(b.dirs)
}

// guard runs f and reports false when it panicked: a fault in the index
// must never take a search down.
func guard(f func()) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	f()
	return true
}

// openIndex opens the index of the project folders in scope, or of every
// folder with All, and pools the statistics the scores use. It does nothing
// for a query the index can't search, which is scanned as without an index.
func (s *scan) openIndex(o SearchOptions, scope map[string]string) {
	terms := make([]string, len(s.q.terms))
	for i, t := range s.q.terms {
		terms[i] = t.text
	}
	q, ok := searchindex.NewQuery(terms)
	if !ok {
		return
	}
	x := &indexState{q: q, limit: o.Limit, since: math.MinInt64}
	if !s.q.since.IsZero() {
		x.since = s.q.since.Unix()
	}
	var folders []string
	if o.All {
		folders = searchindex.Folders(o.Dirs)
	} else {
		for dir := range scope {
			folders = append(folders, dir)
		}
		sort.Strings(folders)
	}
	if !guard(func() {
		var unusable []string
		x.ix, unusable = searchindex.Open(o.Dirs, folders)
		stats, damaged := x.ix.Stats(q)
		x.stats, x.unusable = stats, append(unusable, damaged...)
	}) {
		if x.ix != nil {
			x.ix.Close()
		}
		return
	}
	s.ix = x
}

func (s *scan) closeIndex() {
	if s.ix != nil {
		s.ix.ix.Close()
	}
}

// ranked reports whether hits are scored: some segment was opened.
func (s *scan) ranked() bool { return s.ix != nil && !s.ix.off && s.ix.stats.N > 0 }

// score is a unit's score: the index's for a line it proposed, otherwise
// the same formula over its text with the pooled statistics.
func (s *scan) score(u unit) float64 {
	switch {
	case u.scored:
		return u.score
	case !s.ranked():
		return 0
	}
	return s.ix.q.Score(s.ix.stats, u.text, u.tool)
}

// searchIndex reads the candidates the index proposes for the listed files
// and returns what is left to scan: the files it lacks whole, and the tails
// of those it covers. A fault turns the index off for the search, which
// then scans every file.
func (s *scan) searchIndex(ctx context.Context, root *os.Root, sources []source) []source {
	if s.ix == nil {
		return sources
	}
	s.ix.sources, s.ix.before = sources, s.snapshot()
	var rest []source
	if !guard(func() { rest = s.useIndex(ctx, root, sources) }) {
		s.restore(s.ix.before)
		s.indexOff()
		return sources
	}
	return rest
}

func (s *scan) useIndex(ctx context.Context, root *os.Root, sources []source) []source {
	x := s.ix
	if indexHook != nil {
		indexHook("all")
	}
	for {
		rest := s.classify(root, sources)
		live := func(folder, file string, gen uint32) bool {
			g, ok := x.gens[folder+"/"+file]
			return ok && g == gen
		}
		all, some, allTotal, someTotal, damaged := x.ix.Candidates(x.q, x.stats, live, x.since, searchindex.K(x.limit))
		if len(damaged) == 0 {
			damaged = x.locate(all, some)
		}
		if len(damaged) > 0 {
			// The folders found damaged are scanned instead.
			x.unusable = append(x.unusable, damaged...)
			continue
		}
		x.some, x.someTotal = some, someTotal
		before := s.all.total
		s.verify(ctx, root, all)
		if !s.cut && x.short(s.all.total-before, allTotal, len(all)) {
			// The files the index covers are scanned too (see
			// finishIndex), past the lines read here.
			x.fill = true
			return rest
		}
		s.estimate(&s.all, before, allTotal, len(all))
		return rest
	}
}

// classify sorts the listed files by what the index covers of them: a file
// it covers up to its end is not scanned; one it covers in part has its
// tail scanned, from where the index stopped, with the line count and the
// off-the-record state there; one it lacks, or that shrank or was replaced
// since, is scanned whole, and so is one where the index stopped that no
// longer starts a line, which only a rewrite in place leaves, counted as a
// stale entry.
func (s *scan) classify(root *os.Root, sources []source) []source {
	x := s.ix
	x.listed, x.gens, x.covered = map[string]source{}, map[string]uint32{}, nil
	x.files, x.bytes, x.stale = 0, 0, 0
	x.unindexed, x.newest = map[string]int64{}, map[string]time.Time{}
	none := map[string]bool{}
	indexed := false
	var rest []source
	for _, src := range sources {
		if src.mod.After(x.newest[src.dir]) {
			x.newest[src.dir] = src.mod
		}
		f := x.ix.Folder(src.dir)
		var st searchindex.FileState
		ok := false
		if f != nil {
			st, ok = f.Manifest.Files[src.name]
		}
		switch {
		case f == nil:
			none[src.dir] = true
			rest = append(rest, src)
			x.unindexed[src.dir] += src.size
		case !ok || st.To == 0 || st.Ino != src.ino || src.size < st.To:
			rest = append(rest, src)
			x.unindexed[src.dir] += src.size
		case src.size > st.To && !startsLine(root, src, st.To):
			rest = append(rest, src)
			x.unindexed[src.dir] += src.size
			x.stale++
		default:
			indexed = true
			key := src.dir + "/" + src.name
			x.listed[key], x.gens[key] = src, st.Gen
			part := src
			part.to = st.To
			x.covered = append(x.covered, part)
			x.files++
			x.bytes += st.To
			s.dirs[src.dir] = true
			if src.size > st.To {
				tail := src
				tail.from, tail.line, tail.off = st.To, st.Lines, st.Off
				rest = append(rest, tail)
				x.unindexed[src.dir] += src.size - st.To
			}
		}
	}
	x.noIndex = nil
	for dir := range none {
		if indexed && !slices.Contains(x.unusable, dir) {
			x.noIndex = append(x.noIndex, dir)
		}
	}
	sort.Strings(x.noIndex)
	return rest
}

// startsLine reports whether offset at of src starts a line: the byte
// before it is a newline. A file it can't open is left to the scan, which
// counts it unreadable.
func startsLine(root *os.Root, src source, at int64) bool {
	f, err := regular(root, filepath.Join(src.dir, src.name))
	if err != nil {
		return true
	}
	defer f.Close()
	var prev [1]byte
	_, err = f.ReadAt(prev[:], at-1)
	return err == nil && prev[0] == '\n'
}

// locate reads where each candidate's line stands, and returns the folders
// whose locations failed their checks.
func (x *indexState) locate(lists ...[]searchindex.Candidate) []string {
	var damaged []string
	for _, cs := range lists {
		for i := range cs {
			if err := x.ix.Locate(&cs[i]); err != nil && !slices.Contains(damaged, cs[i].Folder) {
				damaged = append(damaged, cs[i].Folder)
			}
		}
	}
	return damaged
}

// verify reads each candidate's line again and matches it as a scanned
// line is matched, with the index's score. A line that is gone, no longer
// ends where it did, doesn't decode or no longer belongs to its file is
// stale and skipped.
func (s *scan) verify(ctx context.Context, root *os.Root, cs []searchindex.Candidate) {
	files := map[string]*os.File{}
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	var buf []byte
	for _, c := range cs {
		if ctx.Err() != nil {
			s.cut = true
			break
		}
		key := c.Folder + "/" + c.File
		src, ok := s.ix.listed[key]
		if !ok || s.ix.gens[key] != c.Gen {
			continue
		}
		f := files[key]
		if f == nil {
			if len(files) == 64 {
				for k, open := range files {
					open.Close()
					delete(files, k)
				}
			}
			var err error
			if f, err = regular(root, filepath.Join(src.dir, src.name)); err != nil || inode(f) != src.ino {
				if f != nil {
					f.Close()
				}
				s.ix.stale++
				continue
			}
			files[key] = f
		}
		var e inbox.Entry
		if buf, ok = lineAt(f, c.Loc, buf); !ok || json.Unmarshal(buf, &e) != nil || !inbox.Trusted(e, src.dir, src.name, "") || !searchable(e.Kind) {
			s.ix.stale++
			continue
		}
		if src.key != "" && e.Project != src.key {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, e.TS)
		if err != nil {
			ts = time.Time{}
		}
		if !s.q.since.IsZero() && (ts.IsZero() || ts.Before(s.q.since)) {
			continue
		}
		if s.ix.read == nil {
			s.ix.read = map[string]map[int]bool{}
		}
		if s.ix.read[key] == nil {
			s.ix.read[key] = map[int]bool{}
		}
		s.ix.read[key][c.Loc.Line] = true
		s.match(unit{text: e.Text, shown: e.Text, ts: ts, role: speaker(e.Role), branch: e.Branch, project: e.Project, session: e.Session, file: key, line: c.Loc.Line, tool: e.Kind == "tool", score: c.Score, scored: true})
	}
}

// lineAt reads the line at l into buf: the byte before it must end a line
// and the byte after it must be its newline, as they were when it was
// indexed in a file that is only ever appended to.
func lineAt(f *os.File, l searchindex.DocLoc, buf []byte) ([]byte, bool) {
	if l.Offset > 0 {
		var prev [1]byte
		if _, err := f.ReadAt(prev[:], l.Offset-1); err != nil || prev[0] != '\n' {
			return buf, false
		}
	}
	buf = slices.Grow(buf[:0], l.Length+1)[:l.Length+1]
	if _, err := f.ReadAt(buf, l.Offset); err != nil || buf[l.Length] != '\n' {
		return buf, false
	}
	return buf[:l.Length], true
}

func searchable(kind string) bool {
	return kind == "prompt" || kind == "reply" || kind == "tool" || kind == "plan"
}

func inode(f *os.File) uint64 {
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	return statIno(info)
}

func statIno(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return 0
}

// short reports whether the candidates read again held fewer than limit
// hits while more remain, for a query with a term that isn't plain: a
// phrase, a term with punctuation or a short one, which the candidates past
// those read may hold, or not, in any number.
func (x *indexState) short(hits, total, read int) bool {
	return !x.q.Plain() && hits < x.limit && total > read
}

// estimate counts the candidates past those read again as hits, when some
// of those read held every term, and words the count as approximate unless
// every one read did and every term is a plain word, which a candidate
// always holds as search matches it.
func (s *scan) estimate(r *ranking, before, total, located int) {
	rest := total - located
	if rest <= 0 || r.total == before {
		return
	}
	r.total += rest
	exact := s.ix.q.Plain() && s.ix.stale == 0 && r.total-rest-before == located
	r.about = r.about || !exact
}

// finishIndex ends what the index does for a search, once the files left to
// it are scanned: when nothing held every term, the candidates holding some
// are read again; when nothing at all matched, the files the index covers
// are scanned after all, as without an index, so "No matches." keeps its
// meaning and a match inside a word, which the index can't see, is still
// found. They are scanned too when the candidates read held too few hits
// (see short), whose hits are kept. Either way they are scanned only up to
// where the index stopped, since their tails were scanned already, and
// past the lines verify matched. Then the index is topped up for the next
// search.
func (s *scan) finishIndex(ctx context.Context, root *os.Root, o SearchOptions, start time.Time) {
	x := s.ix
	if x == nil || x.off {
		return
	}
	if !guard(func() {
		if s.all.total == 0 && len(x.some) > 0 && !s.cut && !x.fill {
			if indexHook != nil {
				indexHook("some")
			}
			before := s.any.total
			s.verify(ctx, root, x.some)
			if !s.cut && x.short(s.any.total-before, x.someTotal, len(x.some)) {
				x.fill = true
			} else {
				s.estimate(&s.any, before, x.someTotal, len(x.some))
			}
		}
	}) {
		s.restore(x.before)
		s.indexOff()
		s.scanWhole(ctx, root, x.sources)
		return
	}
	if x.files > 0 && (x.fill || s.all.total == 0 && s.any.total == 0) {
		files, bytes := s.files, s.bytes
		s.scanWhole(ctx, root, x.covered)
		x.fallback, x.fallFiles, x.fallBytes = true, s.files-files, s.bytes-bytes
		s.files, s.bytes = files, bytes
	}
	s.topUp(ctx, o, start)
}

// scanWhole scans each of sources from its first line, in order, as without
// an index, up to src.to when that is set.
func (s *scan) scanWhole(ctx context.Context, root *os.Root, sources []source) {
	for _, src := range sources {
		if s.cut = ctx.Err() != nil; s.cut {
			break
		}
		src.from, src.line, src.off = 0, 0, false
		s.file(ctx, root, src)
		if s.cut {
			break
		}
	}
}

// indexOff turns the index off for the rest of the search: every file is
// scanned, and the trailer says nothing of the index.
func (s *scan) indexOff() {
	x := s.ix
	x.off = true
	x.files, x.bytes, x.some, x.stale, x.read = 0, 0, nil, 0, nil
}

// tail starts the scan of src at its tail, src.from, which must still start
// a line, as classify checked; otherwise, after a rewrite since, the file is
// scanned whole, from its first line.
func (s *scan) tail(f *os.File, src *source) {
	if src.from == 0 {
		return
	}
	var prev [1]byte
	if _, err := f.ReadAt(prev[:], src.from-1); err == nil && prev[0] == '\n' {
		if _, err := f.Seek(src.from, io.SeekStart); err == nil {
			return
		}
	}
	s.ix.stale++
	src.from, src.line, src.off = 0, 0, false
	_, _ = f.Seek(0, io.SeekStart)
}

// topUp indexes, for the next search, up to TopUpBytes of what the index
// missed in scope, the newest files first, once that is TopUpMin or more
// and TopUpReserve of the search's time is left. Only a search with TopUp
// set, eva-md search's, does: one without it, such as the MCP server's,
// or with no time limit leaves it to eva-md index and the daily maintain.
// A folder another writer holds is skipped.
func (s *scan) topUp(ctx context.Context, o SearchOptions, start time.Time) {
	x := s.ix
	deadline, ok := ctx.Deadline()
	if !o.TopUp || !ok || !s.inbox || s.cut || time.Until(deadline) < searchindex.TopUpReserve {
		return
	}
	total := int64(0)
	var folders []string
	for dir, n := range x.unindexed {
		if n > 0 {
			total += n
			folders = append(folders, dir)
		}
	}
	if total < searchindex.TopUpMin {
		return
	}
	sort.Slice(folders, func(i, j int) bool {
		if !x.newest[folders[i]].Equal(x.newest[folders[j]]) {
			return x.newest[folders[i]].After(x.newest[folders[j]])
		}
		return folders[i] < folders[j]
	})
	until := deadline.Add(-topUpMargin)
	left := int64(searchindex.TopUpBytes)
	guard(func() {
		for _, dir := range folders {
			if left <= 0 || time.Now().After(until) {
				return
			}
			r, err := searchindex.Refresh(ctx, o.Dirs, dir, searchindex.Options{Budget: searchindex.Budget{Bytes: left, Until: until}, NewestFirst: true, Reset: slices.Contains(x.unusable, dir), By: "search"})
			if err != nil || r.Busy {
				continue
			}
			x.topFiles += r.Files
			x.topBytes += r.Bytes
			left -= r.Bytes
		}
	})
}

// indexNotes are the trailer's notes on the index: what it covered and how
// current it was, the folders it could not be used for or lacks, stale
// entries, the fallback scan and the top-up.
func (s *scan) indexNotes(o SearchOptions) []string {
	x := s.ix
	if x == nil || x.off || !s.inbox {
		return nil
	}
	var notes []string
	if x.files > 0 {
		note := fmt.Sprintf("searched %d %s (%.1f MB) through the index", x.files, plural(x.files, "file"), float64(x.bytes)/1e6)
		if built := x.ix.Built(); !built.IsZero() {
			note += " of " + built.In(o.Now.Location()).Format("2006-01-02 15:04")
		}
		notes = append(notes, note)
	}
	if n := len(x.unusable); n > 0 {
		notes = append(notes, fmt.Sprintf("the index for %d project %s is unusable, so its files were scanned: %s; eva-md index rebuilds it", n, plural(n, "folder"), named(x.unusable)))
	}
	if n := len(x.noIndex); n > 0 {
		notes = append(notes, fmt.Sprintf("%d project %s not indexed yet, so %s files were scanned: %s", n, forms(n, "folder is", "folders are"), forms(n, "its", "their"), named(x.noIndex)))
	}
	if x.stale > 0 {
		notes = append(notes, fmt.Sprintf("%d stale index %s skipped; eva-md index --rebuild builds the index again", x.stale, forms(x.stale, "entry", "entries")))
	}
	if x.fallback {
		why := "the index found nothing"
		if x.fill {
			why = "the index's best candidates held too few matches"
		}
		notes = append(notes, fmt.Sprintf("%s, so the %d %s it covers (%.1f MB) %s scanned too", why, x.fallFiles, plural(x.fallFiles, "file"), float64(x.fallBytes)/1e6, forms(x.fallFiles, "was", "were")))
	}
	if x.topFiles > 0 {
		notes = append(notes, fmt.Sprintf("indexed %d %s (%.1f MB) for the next search", x.topFiles, plural(x.topFiles, "file"), float64(x.topBytes)/1e6))
	}
	return notes
}

// rankedOrder adds the scores' place to a label's order when hits were
// scored.
func (s *scan) rankedOrder(order string) string {
	if !s.ranked() {
		return order
	}
	return order[:len(order)-len("then newest")] + "then rarer and denser matches, then newest"
}

// forms picks one or many by n.
func forms(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// clone copies a ranking, so a fault can put it back.
func (r ranking) clone() ranking {
	r.top = slices.Clone(r.top)
	return r
}
