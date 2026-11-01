package searchindex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// A segment file, little-endian throughout:
//
//	header    32 B: the magic, then ndocs u32, nsources u32, nterms u32, the
//	          table's offset u64 and 4 reserved bytes
//	sources   count u32; each: name (uvarint length, bytes), gen u32, from
//	          u64, to u64, docs u32, then its runs of document ids (uvarint
//	          count; each first u32, count u32)
//	docloc    per document 20 B: source u32, offset u64, length u32, line u32
//	docmeta   ts u32 × ndocs, length u16 × ndocs, kind u8 × ndocs, then a
//	          crc32 for each block of 64 docloc entries
//	postings  per term, per document: uvarint (doc-id delta << 1 | has tf),
//	          then uvarint tf when it is over 1
//	dict      the terms in byte order, in blocks of 128, each term after a
//	          block's first sharing a prefix with the one before it: uvarint
//	          shared length, uvarint suffix length, the suffix, uvarint df,
//	          uvarint postings offset (from the previous term's in the block,
//	          from 0 for the first), uvarint postings length, crc32 of the
//	          postings
//	dictidx   count u32; per block: its first term (uvarint length, bytes),
//	          uvarint offset and uvarint length in dict, crc32 of the block
//	table     6 × (offset u64, length u64, crc32 u32): sources, docmeta,
//	          docloc, dictidx, dict, postings
//	trailer   crc32 of the header and the table, then the magic
//
// The sections loaded whole at open (sources, docmeta, dictidx) are checked
// against the table's crc then; a dictionary block, a term's postings and a
// block of locations are checked against their own crc each time they are
// read, so a search reads only what its terms touch.
const (
	magic       = "EVAIDX\x00\x01"
	headerSize  = 32
	tableSize   = 6 * 20
	trailerSize = 12
	locSize     = 20
	locBlock    = 64
	dictBlock   = 128
	maxName     = 1024
)

// The sections, in the table's order.
const (
	secSources = iota
	secDocmeta
	secDocloc
	secDictidx
	secDict
	secPostings
)

// Document kinds.
const (
	kindPrompt = 1 + iota
	kindReply
	kindTool
	kindPlan
)

func kindCode(kind string) uint8 {
	switch kind {
	case "prompt":
		return kindPrompt
	case "reply":
		return kindReply
	case "tool":
		return kindTool
	case "plan":
		return kindPlan
	}
	return 0
}

var segmentPattern = regexp.MustCompile(`^seg-[0-9a-f]{16}\.idx$`)

// Source is a session file a segment holds documents of: its name, the
// generation of the file they were read from, the byte range read, and the
// documents, as runs of ids, each its first and its count.
type Source struct {
	Name     string
	Gen      uint32
	From, To int64
	Docs     uint32
	Runs     [][2]uint32
}

// DocLoc is where a document's line stands in its file: its source, as an
// index into its segment's sources, the line's byte offset and its length
// without the newline, and its physical line number.
type DocLoc struct {
	Source uint32
	Offset int64
	Length int
	Line   int
}

// Term is a dictionary term of a segment: its text, the documents holding
// it, and where its postings lie.
type Term struct {
	Text string
	DF   int
	off  int64
	n    int64
	crc  uint32
}

type posting struct{ doc, tf uint32 }

type section struct {
	off, len int64
	crc      uint32
}

type blockEntry struct {
	first       string
	offset, len int64
	crc         uint32
}

// segmentInput is what writeSegment writes: the sources, each document's
// timestamp, length and kind, a function that passes each document's
// location in order, and one that passes each term in byte order with its
// postings in document order.
type segmentInput struct {
	sources []Source
	ts      []uint32
	dl      []uint16
	kind    []uint8
	locs    func(emit func(DocLoc) error) error
	terms   func(emit func(term string, ps []posting) error) error
}

// segWriter writes a segment's sections in order, counting bytes and each
// section's crc32.
type segWriter struct {
	w     *bufio.Writer
	n     int64
	start int64
	crc   hash.Hash32
	err   error
	tmp   [8]byte
}

func (w *segWriter) write(p []byte) {
	if w.err != nil {
		return
	}
	n, err := w.w.Write(p)
	w.n += int64(n)
	w.crc.Write(p[:n])
	w.err = err
}

func (w *segWriter) u16(v uint16) { binary.LittleEndian.PutUint16(w.tmp[:], v); w.write(w.tmp[:2]) }
func (w *segWriter) u32(v uint32) { binary.LittleEndian.PutUint32(w.tmp[:], v); w.write(w.tmp[:4]) }
func (w *segWriter) u64(v uint64) { binary.LittleEndian.PutUint64(w.tmp[:], v); w.write(w.tmp[:8]) }

func (w *segWriter) begin() { w.start, w.crc = w.n, crc32.NewIEEE() }

func (w *segWriter) end() section {
	return section{off: w.start, len: w.n - w.start, crc: w.crc.Sum32()}
}

// writeSegment writes a segment into the index folder root under a new
// name: to a temporary file, which it fsyncs and renames into place before
// fsyncing the folder, so no reader ever finds part of one. It returns the
// segment's entry for the manifest.
func writeSegment(ctx context.Context, root *os.Root, in segmentInput) (info SegmentInfo, err error) {
	if err := ctx.Err(); err != nil {
		return info, err
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return info, fmt.Errorf("searchindex: segment name: %w", err)
	}
	name := "seg-" + hex.EncodeToString(random[:]) + ".idx"
	tmp := paths.TempPrefix + hex.EncodeToString(random[:])
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return info, fmt.Errorf("searchindex: temporary file: %w", err)
	}
	renamed := false
	defer func() {
		f.Close()
		if !renamed {
			_ = root.Remove(tmp)
		}
	}()
	ndocs := len(in.ts)
	w := &segWriter{w: bufio.NewWriterSize(f, 256<<10), crc: crc32.NewIEEE()}
	w.write(make([]byte, headerSize))
	var secs [6]section

	w.begin()
	w.u32(uint32(len(in.sources)))
	for _, s := range in.sources {
		w.write(binary.AppendUvarint(w.tmp[:0], uint64(len(s.Name))))
		w.write([]byte(s.Name))
		w.u32(s.Gen)
		w.u64(uint64(s.From))
		w.u64(uint64(s.To))
		w.u32(s.Docs)
		w.write(binary.AppendUvarint(w.tmp[:0], uint64(len(s.Runs))))
		for _, r := range s.Runs {
			w.u32(r[0])
			w.u32(r[1])
		}
	}
	secs[secSources] = w.end()

	w.begin()
	var locCRC []uint32
	block, inBlock, count := crc32.NewIEEE(), 0, 0
	err = in.locs(func(l DocLoc) error {
		var e [locSize]byte
		binary.LittleEndian.PutUint32(e[0:], l.Source)
		binary.LittleEndian.PutUint64(e[4:], uint64(l.Offset))
		binary.LittleEndian.PutUint32(e[12:], uint32(l.Length))
		binary.LittleEndian.PutUint32(e[16:], uint32(l.Line))
		w.write(e[:])
		block.Write(e[:])
		count++
		if inBlock++; inBlock == locBlock {
			locCRC = append(locCRC, block.Sum32())
			block.Reset()
			inBlock = 0
		}
		return w.err
	})
	if inBlock > 0 {
		locCRC = append(locCRC, block.Sum32())
	}
	if err == nil && count != ndocs {
		err = errors.New("searchindex: a segment's locations and documents differ in number")
	}
	if err != nil {
		return info, err
	}
	secs[secDocloc] = w.end()

	w.begin()
	var tokens int64
	for _, t := range in.ts {
		w.u32(t)
	}
	for _, l := range in.dl {
		w.u16(l)
		tokens += int64(l)
	}
	w.write(in.kind)
	for _, c := range locCRC {
		w.u32(c)
	}
	secs[secDocmeta] = w.end()

	w.begin()
	var dict bytes.Buffer
	var blocks []blockEntry
	var prev, lastTerm string
	var last, start int64
	var buf, entry []byte
	nterms := 0
	closeBlock := func() {
		if len(blocks) > 0 {
			b := &blocks[len(blocks)-1]
			b.len = int64(dict.Len()) - b.offset
			b.crc = crc32.ChecksumIEEE(dict.Bytes()[b.offset:])
		}
	}
	start = w.n
	err = in.terms(func(term string, ps []posting) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if term == "" || nterms > 0 && term <= lastTerm || len(ps) == 0 {
			return errors.New("searchindex: terms out of order or without postings")
		}
		lastTerm = term
		buf = encodePostings(buf[:0], ps)
		off := w.n - start
		w.write(buf)
		if nterms%dictBlock == 0 {
			closeBlock()
			blocks = append(blocks, blockEntry{first: term, offset: int64(dict.Len())})
			prev, last = "", 0
		}
		shared := commonPrefix(prev, term)
		e := binary.AppendUvarint(entry[:0], uint64(shared))
		e = binary.AppendUvarint(e, uint64(len(term)-shared))
		e = append(e, term[shared:]...)
		e = binary.AppendUvarint(e, uint64(len(ps)))
		e = binary.AppendUvarint(e, uint64(off-last))
		e = binary.AppendUvarint(e, uint64(len(buf)))
		e = binary.LittleEndian.AppendUint32(e, crc32.ChecksumIEEE(buf))
		dict.Write(e)
		entry = e
		prev, last = term, off
		nterms++
		return w.err
	})
	if err != nil {
		return info, err
	}
	closeBlock()
	secs[secPostings] = w.end()

	w.begin()
	w.write(dict.Bytes())
	secs[secDict] = w.end()

	w.begin()
	w.u32(uint32(len(blocks)))
	for _, b := range blocks {
		w.write(binary.AppendUvarint(w.tmp[:0], uint64(len(b.first))))
		w.write([]byte(b.first))
		w.write(binary.AppendUvarint(w.tmp[:0], uint64(b.offset)))
		w.write(binary.AppendUvarint(w.tmp[:0], uint64(b.len)))
		w.u32(b.crc)
	}
	secs[secDictidx] = w.end()

	tableOff := w.n
	head := make([]byte, headerSize)
	copy(head, magic)
	binary.LittleEndian.PutUint32(head[8:], uint32(ndocs))
	binary.LittleEndian.PutUint32(head[12:], uint32(len(in.sources)))
	binary.LittleEndian.PutUint32(head[16:], uint32(nterms))
	binary.LittleEndian.PutUint64(head[20:], uint64(tableOff))
	var table []byte
	for _, s := range secs {
		table = binary.LittleEndian.AppendUint64(table, uint64(s.off))
		table = binary.LittleEndian.AppendUint64(table, uint64(s.len))
		table = binary.LittleEndian.AppendUint32(table, s.crc)
	}
	sum := crc32.NewIEEE()
	sum.Write(head)
	sum.Write(table)
	w.write(table)
	w.write(binary.LittleEndian.AppendUint32(nil, sum.Sum32()))
	w.write([]byte(magic))
	if w.err == nil {
		w.err = w.w.Flush()
	}
	if w.err != nil {
		return info, fmt.Errorf("searchindex: write: %w", w.err)
	}
	if _, err := f.WriteAt(head, 0); err != nil {
		return info, fmt.Errorf("searchindex: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return info, fmt.Errorf("searchindex: fsync: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return info, err
	}
	if err := root.Rename(tmp, name); err != nil {
		return info, fmt.Errorf("searchindex: rename: %w", err)
	}
	renamed = true
	if err := paths.SyncDirAt(root, "."); err != nil {
		return info, fmt.Errorf("searchindex: fsync folder: %w", err)
	}
	return SegmentInfo{File: name, Docs: ndocs, Tokens: tokens, Bytes: w.n}, nil
}

func encodePostings(dst []byte, ps []posting) []byte {
	prev := uint32(0)
	for i, p := range ps {
		delta := p.doc - prev
		if i == 0 {
			delta = p.doc
		}
		v := uint64(delta) << 1
		if p.tf > 1 {
			v |= 1
		}
		dst = binary.AppendUvarint(dst, v)
		if p.tf > 1 {
			dst = binary.AppendUvarint(dst, uint64(p.tf))
		}
		prev = p.doc
	}
	return dst
}

func commonPrefix(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// Segment is an open segment. Its sources, its documents' timestamps,
// lengths and kinds and its dictionary's block index are in memory; its
// postings, dictionary blocks and locations are read from the file as
// needed, every read checked. A failed check is ErrDamaged.
type Segment struct {
	Name    string
	f       *os.File
	sources []Source
	ts      []uint32
	dl      []uint16
	kind    []uint8
	locCRC  []uint32
	tokens  int64
	nterms  int
	blocks  []blockEntry
	secs    [6]section
}

func damaged(what string) error { return fmt.Errorf("%w: %s", ErrDamaged, what) }

// OpenSegment opens the segment name in the index folder root: its header,
// table and trailer, then its sources, docmeta and dictionary block index,
// each checked against its crc. A segment that is missing comes back as
// that error, one that fails a check as ErrDamaged.
func OpenSegment(root *os.Root, name string) (seg *Segment, err error) {
	if !segmentPattern.MatchString(name) {
		return nil, damaged("a segment's name")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if !info.Mode().IsRegular() || size < headerSize+tableSize+trailerSize {
		return nil, damaged("a segment's size")
	}
	head, tail, table := make([]byte, headerSize), make([]byte, trailerSize), make([]byte, tableSize)
	if _, err := f.ReadAt(head, 0); err != nil {
		return nil, damaged("a segment's header")
	}
	if _, err := f.ReadAt(tail, size-trailerSize); err != nil {
		return nil, damaged("a segment's trailer")
	}
	tableOff := size - trailerSize - tableSize
	if string(head[:8]) != magic || string(tail[4:]) != magic || int64(binary.LittleEndian.Uint64(head[20:])) != tableOff {
		return nil, damaged("a segment's header")
	}
	if _, err := f.ReadAt(table, tableOff); err != nil {
		return nil, damaged("a segment's table")
	}
	sum := crc32.NewIEEE()
	sum.Write(head)
	sum.Write(table)
	if sum.Sum32() != binary.LittleEndian.Uint32(tail) {
		return nil, damaged("a segment's table")
	}
	s := &Segment{Name: name, f: f}
	for i := range s.secs {
		e := table[i*20:]
		off, n := binary.LittleEndian.Uint64(e), binary.LittleEndian.Uint64(e[8:])
		if off < headerSize || n > uint64(tableOff) || off > uint64(tableOff)-n {
			return nil, damaged("a segment's table")
		}
		s.secs[i] = section{off: int64(off), len: int64(n), crc: binary.LittleEndian.Uint32(e[16:])}
	}
	ndocs := int64(binary.LittleEndian.Uint32(head[8:]))
	nblocks := (ndocs + locBlock - 1) / locBlock
	s.nterms = int(binary.LittleEndian.Uint32(head[16:]))
	if s.secs[secDocloc].len != ndocs*locSize || s.secs[secDocmeta].len != ndocs*7+nblocks*4 {
		return nil, damaged("a segment's document count")
	}
	if err := s.loadSources(int(binary.LittleEndian.Uint32(head[12:])), int(ndocs)); err != nil {
		return nil, err
	}
	if err := s.loadDocmeta(int(ndocs), int(nblocks)); err != nil {
		return nil, err
	}
	if err := s.loadDictidx(); err != nil {
		return nil, err
	}
	return s, nil
}

// whole reads a section loaded at open and checks it against its crc.
func (s *Segment) whole(sec int) ([]byte, error) {
	data, err := s.read(sec, 0, s.secs[sec].len)
	if err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(data) != s.secs[sec].crc {
		return nil, damaged("a segment's checksum")
	}
	return data, nil
}

// read reads n bytes at off within a section.
func (s *Segment) read(sec int, off, n int64) ([]byte, error) {
	if off < 0 || n < 0 || n > s.secs[sec].len || off > s.secs[sec].len-n {
		return nil, damaged("an offset past its section")
	}
	data := make([]byte, n)
	if _, err := s.f.ReadAt(data, s.secs[sec].off+off); err != nil && !(errors.Is(err, io.EOF) && n == 0) {
		return nil, damaged("a short read")
	}
	return data, nil
}

func (s *Segment) loadSources(count, ndocs int) error {
	data, err := s.whole(secSources)
	if err != nil {
		return err
	}
	d := decoder{b: data}
	if int(d.u32()) != count {
		return damaged("a segment's sources")
	}
	for i := 0; i < count && !d.bad; i++ {
		var src Source
		src.Name = string(d.take(d.uvarint(), maxName))
		src.Gen, src.From, src.To, src.Docs = d.u32(), int64(d.u64()), int64(d.u64()), d.u32()
		runs := d.uvarint()
		if runs > uint64(ndocs) {
			return damaged("a segment's sources")
		}
		total := uint64(0)
		for j := uint64(0); j < runs && !d.bad; j++ {
			first, n := d.u32(), d.u32()
			if n == 0 || uint64(first)+uint64(n) > uint64(ndocs) {
				return damaged("a segment's sources")
			}
			src.Runs = append(src.Runs, [2]uint32{first, n})
			total += uint64(n)
		}
		if !plainName(src.Name) || src.From < 0 || src.To < src.From || total != uint64(src.Docs) {
			return damaged("a segment's sources")
		}
		s.sources = append(s.sources, src)
	}
	if d.bad || len(d.b) != 0 {
		return damaged("a segment's sources")
	}
	return nil
}

func (s *Segment) loadDocmeta(ndocs, nblocks int) error {
	data, err := s.whole(secDocmeta)
	if err != nil {
		return err
	}
	s.ts = make([]uint32, ndocs)
	s.dl = make([]uint16, ndocs)
	s.kind = make([]uint8, ndocs)
	s.locCRC = make([]uint32, nblocks)
	for i := range s.ts {
		s.ts[i] = binary.LittleEndian.Uint32(data[4*i:])
	}
	data = data[4*ndocs:]
	for i := range s.dl {
		s.dl[i] = binary.LittleEndian.Uint16(data[2*i:])
		s.tokens += int64(s.dl[i])
	}
	data = data[2*ndocs:]
	copy(s.kind, data)
	for _, k := range s.kind {
		if k < kindPrompt || k > kindPlan {
			return damaged("a document's kind")
		}
	}
	data = data[ndocs:]
	for i := range s.locCRC {
		s.locCRC[i] = binary.LittleEndian.Uint32(data[4*i:])
	}
	return nil
}

func (s *Segment) loadDictidx() error {
	data, err := s.whole(secDictidx)
	if err != nil {
		return err
	}
	d := decoder{b: data}
	n := int(d.u32())
	if n != (s.nterms+dictBlock-1)/dictBlock {
		return damaged("a segment's dictionary")
	}
	next := int64(0)
	for i := 0; i < n && !d.bad; i++ {
		b := blockEntry{first: string(d.take(d.uvarint(), MaxToken))}
		b.offset, b.len, b.crc = int64(d.uvarint()), int64(d.uvarint()), d.u32()
		if b.first == "" || b.offset != next || b.len <= 0 || b.len > s.secs[secDict].len || i > 0 && b.first <= s.blocks[i-1].first {
			return damaged("a segment's dictionary")
		}
		next += b.len
		s.blocks = append(s.blocks, b)
	}
	if d.bad || len(d.b) != 0 || next != s.secs[secDict].len {
		return damaged("a segment's dictionary")
	}
	return nil
}

// plainName reports whether name can be a session file's name in a project
// folder: no separator, no NUL, not empty, not a dot name.
func plainName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00") && len(name) <= maxName
}

// Close closes the segment's file.
func (s *Segment) Close() error { return s.f.Close() }

// Docs is how many documents the segment holds.
func (s *Segment) Docs() int { return len(s.ts) }

// Sources returns the session files the segment holds documents of.
func (s *Segment) Sources() []Source { return s.sources }

// Meta returns a document's timestamp, in Unix seconds (0 when unknown),
// its length in tokens and its kind.
func (s *Segment) Meta(doc uint32) (ts uint32, dl uint16, kind uint8) {
	return s.ts[doc], s.dl[doc], s.kind[doc]
}

// Loc reads where a document's line stands in its file.
func (s *Segment) Loc(doc uint32) (DocLoc, error) {
	if int(doc) >= len(s.ts) {
		return DocLoc{}, damaged("a document past the segment's end")
	}
	locs, err := s.locs(int(doc) / locBlock)
	if err != nil {
		return DocLoc{}, err
	}
	return locs[int(doc)%locBlock], nil
}

// locs reads the block bi of the document locations and checks it.
func (s *Segment) locs(bi int) ([]DocLoc, error) {
	n := min(locBlock, len(s.ts)-bi*locBlock)
	data, err := s.read(secDocloc, int64(bi)*locBlock*locSize, int64(n)*locSize)
	if err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(data) != s.locCRC[bi] {
		return nil, damaged("a block of locations")
	}
	out := make([]DocLoc, n)
	for i := range out {
		e := data[i*locSize:]
		l := DocLoc{Source: binary.LittleEndian.Uint32(e), Offset: int64(binary.LittleEndian.Uint64(e[4:])), Length: int(binary.LittleEndian.Uint32(e[12:])), Line: int(binary.LittleEndian.Uint32(e[16:]))}
		if int(l.Source) >= len(s.sources) || l.Line < 1 || l.Length > inbox.MaxLine {
			return nil, damaged("a document's location")
		}
		if src := s.sources[l.Source]; l.Offset < src.From || l.Offset+int64(l.Length) >= src.To {
			return nil, damaged("a document's location")
		}
		out[i] = l
	}
	return out, nil
}

// Lookup calls visit with the term tok, or with prefix with every term
// that starts with tok, in byte order, until visit returns false.
func (s *Segment) Lookup(tok string, prefix bool, visit func(Term) bool) error {
	bi := sort.Search(len(s.blocks), func(i int) bool { return s.blocks[i].first > tok }) - 1
	for bi = max(bi, 0); bi < len(s.blocks); bi++ {
		terms, err := s.block(bi)
		if err != nil {
			return err
		}
		for _, t := range terms {
			switch {
			case t.Text < tok:
				continue
			case t.Text == tok || prefix && strings.HasPrefix(t.Text, tok):
				if !visit(t) {
					return nil
				}
			default:
				return nil
			}
		}
	}
	return nil
}

// block reads and decodes the dictionary block bi.
func (s *Segment) block(bi int) ([]Term, error) {
	e := s.blocks[bi]
	data, err := s.read(secDict, e.offset, e.len)
	if err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(data) != e.crc {
		return nil, damaged("a dictionary block")
	}
	n := dictBlock
	if bi == len(s.blocks)-1 {
		n = s.nterms - bi*dictBlock
	}
	d := decoder{b: data}
	limit := s.secs[secPostings].len
	terms := make([]Term, 0, n)
	var prev []byte
	off := int64(0)
	for i := 0; i < n; i++ {
		shared, suffix := d.uvarint(), d.uvarint()
		if shared > uint64(len(prev)) || i == 0 && shared != 0 || suffix > MaxToken || shared+suffix > MaxToken || shared+suffix == 0 {
			return nil, damaged("a dictionary term")
		}
		text := append(prev[:shared:shared], d.take(suffix, MaxToken)...)
		df, delta, length, crc := d.uvarint(), d.uvarint(), d.uvarint(), d.u32()
		if i == 0 {
			off = 0
		}
		if d.bad || delta > uint64(limit) || length > uint64(limit) || off > limit-int64(delta) {
			return nil, damaged("a dictionary term")
		}
		off += int64(delta)
		if df == 0 || df > uint64(len(s.ts)) || off > limit-int64(length) || i == 0 && string(text) != e.first || i > 0 && string(text) <= string(prev) {
			return nil, damaged("a dictionary term")
		}
		terms = append(terms, Term{Text: string(text), DF: int(df), off: off, n: int64(length), crc: crc})
		prev = text
	}
	if d.bad || len(d.b) != 0 {
		return nil, damaged("a dictionary block")
	}
	return terms, nil
}

// Postings calls visit with each document of t, in order, and how often t
// occurs in it.
func (s *Segment) Postings(t Term, visit func(doc, tf uint32)) error {
	data, err := s.read(secPostings, t.off, t.n)
	if err != nil {
		return err
	}
	if crc32.ChecksumIEEE(data) != t.crc {
		return damaged("a term's postings")
	}
	d := decoder{b: data}
	ndocs := uint64(len(s.ts))
	doc := uint64(0)
	for i := 0; i < t.DF; i++ {
		v := d.uvarint()
		delta := v >> 1
		if delta > ndocs || i > 0 && delta == 0 {
			return damaged("a term's postings")
		}
		if i == 0 {
			doc = delta
		} else {
			doc += delta
		}
		tf := uint64(1)
		if v&1 != 0 {
			tf = d.uvarint()
		}
		if d.bad || doc >= ndocs || tf == 0 || tf > math.MaxUint16 {
			return damaged("a term's postings")
		}
		visit(uint32(doc), uint32(tf))
	}
	if len(d.b) != 0 {
		return damaged("a term's postings")
	}
	return nil
}

// Verify reads the whole segment and checks every section against its crc
// and every dictionary block's terms, as a catch-up does before it builds on
// a folder.
func (s *Segment) Verify() error {
	for _, sec := range s.secs {
		sum := crc32.NewIEEE()
		if _, err := io.Copy(sum, io.NewSectionReader(s.f, sec.off, sec.len)); err != nil || sum.Sum32() != sec.crc {
			return damaged("a segment's checksum")
		}
	}
	for bi := range s.blocks {
		if _, err := s.block(bi); err != nil {
			return err
		}
	}
	return nil
}

// cursor steps through a segment's dictionary in order, a block at a time.
type cursor struct {
	s     *Segment
	block int
	terms []Term
	i     int
}

// next returns the next term, or ok false after the last.
func (c *cursor) next() (t Term, ok bool, err error) {
	for c.i >= len(c.terms) {
		if c.block >= len(c.s.blocks) {
			return Term{}, false, nil
		}
		if c.terms, err = c.s.block(c.block); err != nil {
			return Term{}, false, err
		}
		c.block, c.i = c.block+1, 0
	}
	c.i++
	return c.terms[c.i-1], true, nil
}

// decoder reads a section's fields, noting a read past its end.
type decoder struct {
	b   []byte
	bad bool
}

func (d *decoder) u32() uint32 {
	if len(d.b) < 4 {
		d.bad = true
		return 0
	}
	v := binary.LittleEndian.Uint32(d.b)
	d.b = d.b[4:]
	return v
}

func (d *decoder) u64() uint64 {
	if len(d.b) < 8 {
		d.bad = true
		return 0
	}
	v := binary.LittleEndian.Uint64(d.b)
	d.b = d.b[8:]
	return v
}

func (d *decoder) uvarint() uint64 {
	v, n := binary.Uvarint(d.b)
	if n <= 0 {
		d.bad = true
		return 0
	}
	d.b = d.b[n:]
	return v
}

// take returns the next n bytes, at most limit.
func (d *decoder) take(n uint64, limit int) []byte {
	if n > uint64(limit) || n > uint64(len(d.b)) {
		d.bad = true
		return nil
	}
	v := d.b[:n]
	d.b = d.b[n:]
	return v
}
