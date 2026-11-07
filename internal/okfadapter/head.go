package okfadapter

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/okf-memory/okf-agent-memory/pkg/okf"
)

// ReadHead reads and parses the start of a page, as ReadMax reads the whole
// of it: the frontmatter, then the body up to the end of the first line that
// holds marker, or the whole page when no line of its body does. It reads
// the page in pieces of 64 KiB, never more than max bytes and one, whatever
// the page's size, and refuses a start longer than max (ErrTooLarge), so a
// section near the top of a page stays readable however long the page
// grows; a max of 0 sets no limit. The Concept's Body is the body read; a
// marker in the frontmatter ends nothing.
func (w *Wiki) ReadHead(id, marker string, max int64) (*Concept, error) {
	_, rel, full, err := w.locate(id)
	if err != nil {
		return nil, err
	}
	raw, err := readHead(full, []byte(marker), max)
	if err != nil {
		return nil, fmt.Errorf("wiki: read %s: %w", rel, err)
	}
	c, err := okf.ParseConcept(rel, string(raw))
	if err != nil {
		return nil, fmt.Errorf("wiki: parse %s: %w", rel, err)
	}
	return c, nil
}

// frontLine is the line that opens and closes a page's frontmatter, and bom
// a byte order mark okf skips before it.
var (
	frontLine = []byte("---")
	bom       = []byte{0xef, 0xbb, 0xbf}
)

// readHead reads a regular file, never through a link, up to the end of the
// first line after its frontmatter that holds marker, or to its end, in at
// most max bytes. The frontmatter runs from a first line "---" to the next
// "---" line, as okf finds it; a file whose first line is not "---" has
// none, and is read no further than that line, which okf then refuses.
func readHead(full string, marker []byte, max int64) ([]byte, error) {
	f, err := os.OpenFile(full, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	var src io.Reader = f
	if max > 0 {
		src = io.LimitReader(f, max+1)
	}
	r := bufio.NewReaderSize(src, 64<<10)
	var raw []byte
	const (
		first = iota // the first line
		front        // in the frontmatter
		body         // after it
	)
	at := first
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if raw = append(raw, line...); max > 0 && int64(len(raw)) > max {
			return nil, ErrTooLarge
		}
		bare := bytes.TrimRight(line, "\r\n")
		switch {
		case at == first && !bytes.Equal(bytes.TrimPrefix(bare, bom), frontLine):
			return raw, nil
		case at == first:
			at = front
		case at == front && bytes.Equal(bare, frontLine):
			at = body
		case at == body && bytes.Contains(line, marker):
			return raw, nil
		}
		if err != nil {
			return raw, nil
		}
	}
}
