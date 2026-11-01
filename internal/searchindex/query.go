package searchindex

import (
	"container/heap"
	"errors"
	"io/fs"
	"math"
	"os"
	"sort"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// Index is the index of the project folders a search opened, held open for
// the search: each folder's manifest and segments.
type Index struct {
	folders []*Folder
	byName  map[string]*Folder
}

// Folder is one project folder's open index. A folder found damaged while
// it is read is marked unusable, and search then scans its files.
type Folder struct {
	Name     string
	Manifest Manifest
	segs     []*Segment
	unusable bool
	// exp holds, per query token, the dictionary terms kept for it in each
	// segment, as the statistics chose them.
	exp [][][]Term
}

// Folders lists the project folders that have an index folder.
func Folders(dirs paths.Dirs) []string {
	names, _ := subfolders(dirs.SearchIndex())
	return names
}

// Open opens the index of each of names, the project folders a search
// covers: its manifest and its segments, each checked as OpenSegment checks
// it. A folder with no manifest is left out of the index; one whose
// manifest or a segment can't be read or fails its checks is named in
// unusable and left out too. Open writes nothing and takes no lock: a
// writer replaces the manifest atomically and keeps the segments it
// replaced for PruneAge, so the segments the manifest names are there.
func Open(dirs paths.Dirs, names []string) (ix *Index, unusable []string) {
	ix = &Index{byName: map[string]*Folder{}}
	for _, name := range names {
		dir, err := folderPath(dirs.SearchIndex(), name)
		if err != nil {
			continue
		}
		root, err := paths.OpenRoot(dir, false)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			unusable = append(unusable, name)
			continue
		}
		f, err := openFolder(root, name)
		root.Close()
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			unusable = append(unusable, name)
		default:
			ix.folders = append(ix.folders, f)
			ix.byName[name] = f
		}
	}
	return ix, unusable
}

// openFolder opens a folder's manifest and every segment it names.
func openFolder(root *os.Root, name string) (*Folder, error) {
	m, err := ReadManifest(root)
	if err != nil {
		return nil, err
	}
	f := &Folder{Name: name, Manifest: m}
	for _, s := range m.Segments {
		seg, err := OpenSegment(root, s.File)
		if err != nil {
			closeAll(f.segs)
			if errors.Is(err, fs.ErrNotExist) {
				err = damaged("a segment the manifest names is missing")
			}
			return nil, err
		}
		f.segs = append(f.segs, seg)
	}
	return f, nil
}

// Folder returns the usable index of the project folder name, or nil.
func (ix *Index) Folder(name string) *Folder {
	if f := ix.byName[name]; f != nil && !f.unusable {
		return f
	}
	return nil
}

// Close closes every segment the index holds open.
func (ix *Index) Close() {
	for _, f := range ix.folders {
		closeAll(f.segs)
	}
}

// Built is when the newest of the usable folders was last indexed.
func (ix *Index) Built() time.Time {
	var t time.Time
	for _, f := range ix.folders {
		if !f.unusable && f.Manifest.Built.After(t) {
			t = f.Manifest.Built
		}
	}
	return t
}

// Stats pools the statistics of q over the usable folders' segments: the
// documents, their average length, and for each token the documents
// holding the dictionary terms it matches, at most MaxExpansions of them,
// those in the most documents, kept the same in every segment. It returns
// the folders found damaged while reading their dictionaries, which it
// marks unusable.
func (ix *Index) Stats(q *Query) (Stats, []string) {
	var damagedFolders []string
	st := Stats{DF: make([]float64, len(q.tokens)), kept: make([]map[string]bool, len(q.tokens))}
	for _, f := range ix.folders {
		if f.unusable {
			continue
		}
		f.exp = make([][][]Term, len(q.tokens))
		for i := range f.exp {
			f.exp[i] = make([][]Term, len(f.segs))
		}
	}
	for i, qt := range q.tokens {
		df := map[string]int{}
		for _, f := range ix.folders {
			if f.unusable {
				continue
			}
			for si, seg := range f.segs {
				terms, err := expansions(seg, qt)
				if err != nil {
					f.unusable = true
					damagedFolders = append(damagedFolders, f.Name)
					break
				}
				f.exp[i][si] = terms
				for _, t := range terms {
					df[t.Text] += t.DF
				}
			}
		}
		if len(df) > MaxExpansions {
			texts := make([]string, 0, len(df))
			for t := range df {
				texts = append(texts, t)
			}
			sort.Slice(texts, func(a, b int) bool {
				if df[texts[a]] != df[texts[b]] {
					return df[texts[a]] > df[texts[b]]
				}
				return texts[a] < texts[b]
			})
			st.kept[i] = map[string]bool{}
			for _, t := range texts[:MaxExpansions] {
				st.kept[i][t] = true
			}
			for _, f := range ix.folders {
				for si := range f.exp[i] {
					var kept []Term
					for _, t := range f.exp[i][si] {
						if st.kept[i][t.Text] {
							kept = append(kept, t)
						}
					}
					f.exp[i][si] = kept
				}
			}
		}
		for t, n := range df {
			if st.kept[i] == nil || st.kept[i][t] {
				st.DF[i] += float64(n)
			}
		}
	}
	var tokens float64
	for _, f := range ix.folders {
		if f.unusable {
			continue
		}
		for _, seg := range f.segs {
			st.N += float64(seg.Docs())
			tokens += float64(seg.tokens)
		}
	}
	if st.N > 0 {
		st.AvgLen = tokens / st.N
	}
	return st, damagedFolders
}

// perSegment bounds the dictionary terms one segment gives a token, those
// in the most documents, before the terms of all segments are cut to
// MaxExpansions.
const perSegment = 4 * MaxExpansions

// expansions returns the dictionary terms of seg a query token matches: the
// token itself, and for a prefix token every term that starts with it, at
// most perSegment, those in the most documents.
func expansions(seg *Segment, qt qtoken) ([]Term, error) {
	var out []Term
	err := seg.Lookup(qt.text, qt.prefix, func(t Term) bool {
		out = append(out, t)
		return true
	})
	if len(out) > perSegment {
		sort.SliceStable(out, func(a, b int) bool { return out[a].DF > out[b].DF })
		out = out[:perSegment]
	}
	return out, err
}

// Candidate is a document the index proposes: its folder, its score and
// timestamp, and the terms the index finds in it. Locate fills in where its
// line stands.
type Candidate struct {
	Folder  string
	Score   float64
	TS      uint32
	Matched int
	File    string // the session file's name
	Gen     uint32
	Loc     DocLoc
	seg     *Segment
	segIdx  int
	doc     uint32
}

// Live reports whether the documents of a session file of a folder, of a
// generation, can still be shown: search says so from its listing.
type Live func(folder, file string, gen uint32) bool

// Candidates scores q's documents in the usable folders and returns the k
// best holding every term, by score, then newest, and the k best holding
// some, by the terms they hold, then score, then newest, with how many of
// each there were. It skips documents dated before since (Unix seconds;
// math.MinInt64 for no window), with an unknown date when since is
// positive, and documents of files live does not accept. A folder found
// damaged is marked unusable, its candidates dropped, and named.
func (ix *Index) Candidates(q *Query, st Stats, live Live, since int64, k int) (all, some []Candidate, allTotal, someTotal int, damagedFolders []string) {
	allHeap := &candHeap{better: byScore}
	someHeap := &candHeap{better: byTerms}
	full := q.matched(math.MaxUint32)
	for fi, f := range ix.folders {
		if f.unusable || f.exp == nil {
			continue
		}
		// The folder's best, merged into the search's once the folder reads
		// without damage.
		fAll, fSome := &candHeap{better: byScore}, &candHeap{better: byTerms}
		nAll, nSome := 0, 0
		bad := false
		for si, seg := range f.segs {
			dead := deadIn(seg, f.Name, live)
			n := seg.Docs()
			score := make([]float64, n)
			mask := make([]uint32, n)
			acc := make([]uint32, n)
			var touched, hit []uint32
			for i := range q.tokens {
				for _, t := range f.exp[i][si] {
					err := seg.Postings(t, func(doc, tf uint32) {
						if dead != nil && dead[doc] || since != math.MinInt64 && int64(seg.ts[doc]) < since {
							return
						}
						if acc[doc] == 0 {
							touched = append(touched, doc)
						}
						acc[doc] += tf
					})
					if err != nil {
						bad = true
						break
					}
				}
				if bad {
					break
				}
				for _, d := range touched {
					dl := float64(seg.dl[d])
					if seg.kind[d] == kindTool {
						dl = st.AvgLen
					}
					score[d] += BM25(float64(acc[d]), st.DF[i], st.N, dl, st.AvgLen)
					if mask[d] == 0 {
						hit = append(hit, d)
					}
					mask[d] |= 1 << i
					acc[d] = 0
				}
				touched = touched[:0]
			}
			if bad {
				break
			}
			for _, d := range hit {
				c := Candidate{Folder: f.Name, Score: score[d], TS: seg.ts[d], Matched: q.matched(mask[d]), seg: seg, segIdx: fi<<16 | si, doc: d}
				switch {
				case c.Matched == full:
					nAll++
					fAll.offer(c, k)
				case c.Matched > 0:
					nSome++
					fSome.offer(c, k)
				}
			}
		}
		if bad {
			f.unusable = true
			damagedFolders = append(damagedFolders, f.Name)
			continue
		}
		allTotal += nAll
		someTotal += nSome
		for _, c := range fAll.items {
			allHeap.offer(c, k)
		}
		for _, c := range fSome.items {
			someHeap.offer(c, k)
		}
	}
	return allHeap.sorted(), someHeap.sorted(), allTotal, someTotal, damagedFolders
}

// deadIn marks the documents of seg whose files live does not accept, or
// returns nil when it accepts them all.
func deadIn(seg *Segment, folder string, live Live) []bool {
	var dead []bool
	for _, src := range seg.sources {
		if live(folder, src.Name, src.Gen) {
			continue
		}
		if dead == nil {
			dead = make([]bool, seg.Docs())
		}
		for _, run := range src.Runs {
			for d := run[0]; d < run[0]+run[1]; d++ {
				dead[d] = true
			}
		}
	}
	return dead
}

// Locate reads where c's line stands, and its file's name and generation.
// A location that fails its check marks c's folder unusable.
func (ix *Index) Locate(c *Candidate) error {
	l, err := c.seg.Loc(c.doc)
	if err != nil {
		if f := ix.byName[c.Folder]; f != nil {
			f.unusable = true
		}
		return err
	}
	src := c.seg.sources[l.Source]
	c.Loc, c.File, c.Gen = l, src.Name, src.Gen
	return nil
}

// byScore orders candidates by score, then newest, then where they are.
func byScore(a, b *Candidate) bool {
	switch {
	case a.Score != b.Score:
		return a.Score > b.Score
	case a.TS != b.TS:
		return a.TS > b.TS
	}
	return where(a, b)
}

// byTerms orders candidates by the terms they hold, then as byScore.
func byTerms(a, b *Candidate) bool {
	if a.Matched != b.Matched {
		return a.Matched > b.Matched
	}
	return byScore(a, b)
}

func where(a, b *Candidate) bool {
	if a.segIdx != b.segIdx {
		return a.segIdx < b.segIdx
	}
	return a.doc < b.doc
}

// candHeap keeps the k best candidates, the worst on top.
type candHeap struct {
	items  []Candidate
	better func(a, b *Candidate) bool
}

func (h *candHeap) Len() int           { return len(h.items) }
func (h *candHeap) Less(i, j int) bool { return h.better(&h.items[j], &h.items[i]) }
func (h *candHeap) Swap(i, j int)      { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *candHeap) Push(x any)         { h.items = append(h.items, x.(Candidate)) }
func (h *candHeap) Pop() any {
	x := h.items[len(h.items)-1]
	h.items = h.items[:len(h.items)-1]
	return x
}

func (h *candHeap) offer(c Candidate, k int) {
	if len(h.items) < k {
		heap.Push(h, c)
		return
	}
	if h.better(&c, &h.items[0]) {
		h.items[0] = c
		heap.Fix(h, 0)
	}
}

// sorted returns the candidates best first.
func (h *candHeap) sorted() []Candidate {
	out := append([]Candidate(nil), h.items...)
	sort.Slice(out, func(i, j int) bool { return h.better(&out[i], &out[j]) })
	return out
}
