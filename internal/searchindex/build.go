package searchindex

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// builder gathers a block's documents: each document's timestamp, length,
// kind and location, and one (token, document, tf) triple for each token a
// document holds, with the tokens interned.
type builder struct {
	ids     map[string]uint32
	tokens  []string
	triples []triple
	ts      []uint32
	dl      []uint16
	kind    []uint8
	locs    []DocLoc
	sources []Source
	counts  []uint32 // per token id, the current document's tf
	touched []uint32
	bytes   int64 // the inbox bytes the block read
}

type triple struct {
	token, doc uint32
	tf         uint16
}

func newBuilder() *builder { return &builder{ids: map[string]uint32{}} }

// source starts the documents of a range of a session file, and returns
// its index among the block's sources.
func (b *builder) source(name string, gen uint32, from int64) int {
	b.sources = append(b.sources, Source{Name: name, Gen: gen, From: from, To: from})
	return len(b.sources) - 1
}

// add adds a line Walk yielded as a document of source src.
func (b *builder) add(l inbox.Line, src int) {
	doc := uint32(len(b.ts))
	n := tokenize(l.Text, func(t []byte) {
		id, ok := b.ids[string(t)]
		if !ok {
			id = uint32(len(b.tokens))
			s := string(t)
			b.ids[s] = id
			b.tokens = append(b.tokens, s)
			b.counts = append(b.counts, 0)
		}
		if b.counts[id] == 0 {
			b.touched = append(b.touched, id)
		}
		b.counts[id]++
	})
	for _, id := range b.touched {
		b.triples = append(b.triples, triple{token: id, doc: doc, tf: uint16(min(b.counts[id], math.MaxUint16))})
		b.counts[id] = 0
	}
	b.touched = b.touched[:0]
	b.ts = append(b.ts, unixSeconds(l.TS))
	b.dl = append(b.dl, uint16(min(n, math.MaxUint16)))
	b.kind = append(b.kind, kindCode(l.Kind))
	b.locs = append(b.locs, DocLoc{Source: uint32(src), Offset: l.Offset, Length: l.Length, Line: l.Line})
	s := &b.sources[src]
	if k := len(s.Runs); k > 0 && s.Runs[k-1][0]+s.Runs[k-1][1] == doc {
		s.Runs[k-1][1]++
	} else {
		s.Runs = append(s.Runs, [2]uint32{doc, 1})
	}
	s.Docs++
}

// unixSeconds is a timestamp as a segment stores it: whole seconds since
// 1970, 0 when it can't be read or is earlier, and the largest value when
// it is past what fits.
func unixSeconds(ts string) uint32 {
	t, err := time.Parse(time.RFC3339Nano, ts)
	switch {
	case err != nil || t.Unix() <= 0:
		return 0
	case t.Unix() > math.MaxUint32:
		return math.MaxUint32
	}
	return uint32(t.Unix())
}

// estimate is a generous guess at the segment the block makes, in bytes.
func (b *builder) estimate() int64 {
	size := int64(len(b.triples))*3 + int64(len(b.ts))*31 + 4096
	for _, t := range b.tokens {
		size += int64(len(t)) + 16
	}
	return size
}

// input lays out the block as a segment: the tokens sorted, and the
// triples placed by token with a counting sort, which keeps each token's
// documents in order.
func (b *builder) input() segmentInput {
	order := make([]uint32, len(b.tokens))
	for i := range order {
		order[i] = uint32(i)
	}
	slices.SortFunc(order, func(x, y uint32) int {
		switch {
		case b.tokens[x] < b.tokens[y]:
			return -1
		case b.tokens[x] > b.tokens[y]:
			return 1
		}
		return 0
	})
	rank := make([]uint32, len(b.tokens))
	for r, id := range order {
		rank[id] = uint32(r)
	}
	start := make([]int, len(b.tokens)+1)
	for _, t := range b.triples {
		start[rank[t.token]+1]++
	}
	for i := 1; i < len(start); i++ {
		start[i] += start[i-1]
	}
	placed := make([]posting, len(b.triples))
	next := slices.Clone(start)
	for _, t := range b.triples {
		r := rank[t.token]
		placed[next[r]] = posting{doc: t.doc, tf: uint32(t.tf)}
		next[r]++
	}
	return segmentInput{
		sources: b.sources,
		ts:      b.ts,
		dl:      b.dl,
		kind:    b.kind,
		locs: func(emit func(DocLoc) error) error {
			for _, l := range b.locs {
				if err := emit(l); err != nil {
					return err
				}
			}
			return nil
		},
		terms: func(emit func(string, []posting) error) error {
			for r, id := range order {
				if err := emit(b.tokens[id], placed[start[r]:start[r+1]]); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// freeSpace returns the bytes free to an unprivileged writer on the file
// system holding dir; a variable so tests can fill the disk.
var freeSpace = func(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// errDiskFull reports too little free space to write a segment.
var errDiskFull = errors.New("searchindex: too little free disk space to write the index")

// flush writes the block as a segment into the index folder root at dir,
// when it holds a document, after checking for twice its likely size plus
// 256 MB of free space.
func (b *builder) flush(ctx context.Context, root *os.Root, dir string) (SegmentInfo, bool, error) {
	if len(b.ts) == 0 {
		return SegmentInfo{}, false, nil
	}
	free, err := freeSpace(dir)
	if err != nil {
		return SegmentInfo{}, false, fmt.Errorf("searchindex: free space: %w", err)
	}
	if free < uint64(2*b.estimate())+256<<20 {
		return SegmentInfo{}, false, errDiskFull
	}
	info, err := writeSegment(ctx, root, b.input())
	return info, err == nil, err
}
