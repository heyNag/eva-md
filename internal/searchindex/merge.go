package searchindex

import (
	"container/heap"
	"context"
	"math"
	"os"
	"sort"
	"time"
)

// The merge policy: a folder with more than maxSegments segments, more than
// maxSmall under smallBytes, or a segment holding any dead document, has
// some merged.
const (
	maxSegments = 8
	maxSmall    = 4
	smallBytes  = 8 << 20
)

// mergeSet picks the segments of m to merge into one: every small one when
// there are more than maxSmall, the smallest when there are more than
// maxSegments, and each holding a dead document, whose merge drops it, so
// the words of a session file deleted or replaced leave the index at the
// next catch-up. dead counts each segment's dead documents. It returns none
// when merging would change nothing.
func mergeSet(m Manifest, dead map[string]int) []string {
	pick := map[string]bool{}
	var small []string
	for _, s := range m.Segments {
		if s.Bytes < smallBytes {
			small = append(small, s.File)
		}
		if dead[s.File] > 0 {
			pick[s.File] = true
		}
	}
	if len(small) > maxSmall {
		for _, f := range small {
			pick[f] = true
		}
	}
	if len(m.Segments) > maxSegments {
		bySize := append([]SegmentInfo(nil), m.Segments...)
		sort.SliceStable(bySize, func(i, j int) bool { return bySize[i].Bytes < bySize[j].Bytes })
		for _, s := range bySize[:len(bySize)-maxSegments+1] {
			pick[s.File] = true
		}
	}
	var out []string
	for _, s := range m.Segments {
		if pick[s.File] {
			out = append(out, s.File)
		}
	}
	if len(out) == 1 && dead[out[0]] == 0 {
		return nil
	}
	return out
}

// live reports whether a source's documents are live in m: its file's
// generation is still theirs.
func live(m Manifest, src Source) bool {
	st, ok := m.Files[src.Name]
	return ok && st.Gen == src.Gen
}

// deadDocs counts a segment's dead documents.
func deadDocs(m Manifest, seg *Segment) int {
	n := 0
	for _, src := range seg.sources {
		if !live(m, src) {
			n += int(src.Docs)
		}
	}
	return n
}

// merge merges the segments named, open in segs, into one segment in the
// index folder root, dropping their dead documents, and returns m with the
// new segment in place of theirs, which it retires. A merge whose
// documents are all dead writes no segment. The dictionaries are merged a
// term at a time, through a heap of cursors, and each segment's documents
// keep their order behind the documents of the segments before it.
func merge(ctx context.Context, root *os.Root, m Manifest, segs []*Segment) (Manifest, error) {
	const dead = math.MaxUint32
	type remap struct {
		ids    []uint32 // each document's new id, or dead
		source []uint32 // each live source's index among the merged sources
	}
	remaps := make([]remap, len(segs))
	var in segmentInput
	type sourceKey struct {
		name string
		gen  uint32
	}
	key := map[sourceKey]int{}
	next := uint32(0)
	for i, seg := range segs {
		r := remap{ids: make([]uint32, seg.Docs()), source: make([]uint32, len(seg.sources))}
		for d := range r.ids {
			r.ids[d] = dead
		}
		for _, src := range seg.sources {
			if live(m, src) {
				for _, run := range src.Runs {
					for d := run[0]; d < run[0]+run[1]; d++ {
						r.ids[d] = 0
					}
				}
			}
		}
		for d := range r.ids {
			if r.ids[d] == dead {
				continue
			}
			r.ids[d] = next
			next++
			ts, dl, kind := seg.Meta(uint32(d))
			in.ts, in.dl, in.kind = append(in.ts, ts), append(in.dl, dl), append(in.kind, kind)
		}
		// A run's documents are all live, so they keep together.
		for j, src := range seg.sources {
			if !live(m, src) {
				continue
			}
			at, ok := key[sourceKey{src.Name, src.Gen}]
			if !ok {
				at = len(in.sources)
				key[sourceKey{src.Name, src.Gen}] = at
				in.sources = append(in.sources, Source{Name: src.Name, Gen: src.Gen, From: src.From, To: src.To})
			}
			r.source[j] = uint32(at)
			out := &in.sources[at]
			out.From, out.To = min(out.From, src.From), max(out.To, src.To)
			for _, run := range src.Runs {
				first := r.ids[run[0]]
				if k := len(out.Runs); k > 0 && out.Runs[k-1][0]+out.Runs[k-1][1] == first {
					out.Runs[k-1][1] += run[1]
				} else {
					out.Runs = append(out.Runs, [2]uint32{first, run[1]})
				}
				out.Docs += run[1]
			}
		}
		remaps[i] = r
	}
	in.locs = func(emit func(DocLoc) error) error {
		for i, seg := range segs {
			for bi := 0; bi*locBlock < seg.Docs(); bi++ {
				block, err := seg.locs(bi)
				if err != nil {
					return err
				}
				for j, l := range block {
					if remaps[i].ids[bi*locBlock+j] == dead {
						continue
					}
					if !live(m, seg.sources[l.Source]) {
						return damaged("a document outside its source's runs")
					}
					l.Source = remaps[i].source[l.Source]
					if err := emit(l); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	in.terms = func(emit func(string, []posting) error) error {
		h := &cursors{}
		for i, seg := range segs {
			c := &cursor{s: seg}
			t, ok, err := c.next()
			if err != nil {
				return err
			}
			if ok {
				heap.Push(h, &head{c: c, seg: i, term: t})
			}
		}
		var ps []posting
		for h.Len() > 0 {
			text := (*h)[0].term.Text
			ps = ps[:0]
			// The cursors at this term, in segment order, so the documents
			// stay in order.
			var at []*head
			for h.Len() > 0 && (*h)[0].term.Text == text {
				at = append(at, heap.Pop(h).(*head))
			}
			sort.Slice(at, func(i, j int) bool { return at[i].seg < at[j].seg })
			for _, x := range at {
				ids := remaps[x.seg].ids
				if err := x.c.s.Postings(x.term, func(doc, tf uint32) {
					if id := ids[doc]; id != dead {
						ps = append(ps, posting{doc: id, tf: tf})
					}
				}); err != nil {
					return err
				}
				t, ok, err := x.c.next()
				if err != nil {
					return err
				}
				if ok {
					x.term = t
					heap.Push(h, x)
				}
			}
			if len(ps) > 0 {
				if err := emit(text, ps); err != nil {
					return err
				}
			}
		}
		return nil
	}
	var names []string
	for _, seg := range segs {
		names = append(names, seg.Name)
	}
	var info SegmentInfo
	if len(in.ts) > 0 {
		var err error
		if info, err = writeSegment(ctx, root, in); err != nil {
			return m, err
		}
	}
	// The merged segment takes the place of the first it replaces.
	out := m
	out.Segments = nil
	placed := false
	merged := map[string]bool{}
	for _, n := range names {
		merged[n] = true
	}
	for _, s := range m.Segments {
		switch {
		case !merged[s.File]:
			out.Segments = append(out.Segments, s)
		case !placed && info.File != "":
			out.Segments = append(out.Segments, info)
			placed = true
		}
	}
	out.Retired = append([]Retired(nil), m.Retired...)
	now := time.Now()
	for _, n := range names {
		out.Retired = append(out.Retired, Retired{File: n, At: now})
	}
	return out, nil
}

// head is a cursor's current term in the merge's heap.
type head struct {
	c    *cursor
	seg  int
	term Term
}

type cursors []*head

func (h cursors) Len() int { return len(h) }
func (h cursors) Less(i, j int) bool {
	if h[i].term.Text != h[j].term.Text {
		return h[i].term.Text < h[j].term.Text
	}
	return h[i].seg < h[j].seg
}
func (h cursors) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *cursors) Push(x any)   { *h = append(*h, x.(*head)) }
func (h *cursors) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}
