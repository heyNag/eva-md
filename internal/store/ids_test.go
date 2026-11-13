package store

import (
	"crypto/sha256"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The format's section 3: a letter and 13 lower-case Crockford characters.
var reID = regexp.MustCompile(`^[a-z][0-9a-hjkmnp-tv-z]{13}$`)

// ulid writes a pass id for a time, with a fixed random part.
func ulid(t time.Time, random string) string {
	ms := t.UnixMilli()
	var b [10]byte
	for i := 9; i >= 0; i-- {
		b[i] = passAlphabet[ms&31]
		ms >>= 5
	}
	return string(b[:]) + random
}

// sinceEpoch is the time secs seconds after an id's epoch.
func sinceEpoch(secs int64) time.Time {
	return time.Unix(idEpoch.Unix()+secs, 0).UTC()
}

// bitsOf encodes the first n*5 bits of b, high first, as the profile
// checker reads them.
func bitsOf(b []byte, n int) string {
	out := make([]byte, n)
	for i := range n {
		v := 0
		for j := range 5 {
			bit := i*5 + j
			v = v<<1 | int(b[bit/8]>>(7-bit%8)&1)
		}
		out[i] = idAlphabet[v]
	}
	return string(out)
}

func TestNewIDIsTheFormatsGrammar(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 890_000_000, time.UTC)
	pass := ulid(at, "0123456789ABCDEF")
	for _, letter := range []byte("idnqsw") {
		for op := range 40 {
			id, err := NewID(letter, pass, op)
			if err != nil {
				t.Fatal(err)
			}
			if !reID.MatchString(id) || id[0] != letter {
				t.Fatalf("NewID(%q, %s, %d) = %q, not in the grammar", letter, pass, op, id)
			}
			// The first 7 characters after the letter are 35 bits of
			// seconds from 2025-01-01; the last 6 are the first 30 bits of
			// SHA-256 of the pass, a line feed and the index.
			var secs int64
			for _, c := range id[1:8] {
				secs = secs<<5 | int64(strings.IndexRune(idAlphabet, c))
			}
			if got := idEpoch.Add(time.Duration(secs) * time.Second); !got.Equal(at.Truncate(time.Second)) {
				t.Fatalf("id %s says %v, the pass says %v", id, got, at)
			}
			sum := sha256.Sum256([]byte(pass + "\n" + strconv.Itoa(op)))
			if want := bitsOf(sum[:], 6); id[8:] != want {
				t.Fatalf("id %s ends %s, want %s", id, id[8:], want)
			}
		}
	}
}

func TestNewIDIsTheSameForOnePassAndIndex(t *testing.T) {
	pass := ulid(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "ZZZZZZZZZZZZZZZZZZZZZZZZZZ"[:16])
	seen := map[string]int{}
	for op := range 2000 {
		a, err := NewID('i', pass, op)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := NewID('i', pass, op)
		if a != b {
			t.Fatalf("index %d gave %s, then %s", op, a, b)
		}
		if prev, ok := seen[a]; ok {
			t.Fatalf("indexes %d and %d both gave %s", prev, op, a)
		}
		seen[a] = op
	}
	// Another pass at the same second differs.
	other := ulid(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "0000000000000001")
	a, _ := NewID('i', pass, 0)
	b, _ := NewID('i', other, 0)
	if a == b || a[:8] != b[:8] {
		t.Fatalf("two passes in one second gave %s and %s", a, b)
	}
}

func TestNewIDSortsByThePassesTime(t *testing.T) {
	var ids []string
	// From the epoch to the last second 35 bits hold, in uneven steps,
	// so every character of the time part changes.
	for i, step := 0, int64(1); step < 1<<35; i, step = i+1, step*3+int64(i) {
		pass := ulid(sinceEpoch(step), "ZZZZZZZZZZZZZZZZ")
		id, err := NewID('i', pass, 7)
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		ids = append(ids, id)
	}
	last := ulid(sinceEpoch(1<<35-1), "0000000000000000")
	id, err := NewID('i', last, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "izzzzzzz") {
		t.Fatalf("the last second gave %s", id)
	}
	ids = append(ids, id)
	if len(ids) < 15 {
		t.Fatalf("only %d ids", len(ids))
	}
	if !slices.IsSorted(ids) {
		t.Fatalf("ids do not sort by time: %q", ids)
	}
}

func TestNewIDRefuses(t *testing.T) {
	good := ulid(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "ABCDEFGHJKMNPQRS")
	cases := []struct {
		name   string
		letter byte
		pass   string
		op     int
	}{
		{"an upper-case letter", 'I', good, 0},
		{"a digit", '1', good, 0},
		{"a negative index", 'i', good, -1},
		{"a short pass", 'i', good[:25], 0},
		{"a lower-case pass", 'i', strings.ToLower(good), 0},
		{"a pass with I", 'i', good[:25] + "I", 0},
		{"a pass past 48 bits", 'i', "8" + good[1:], 0},
		{"a pass before 2025", 'i', ulid(time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC), "ABCDEFGHJKMNPQRS"), 0},
		{"a pass past 35 bits of seconds", 'i', ulid(sinceEpoch(1<<35), "ABCDEFGHJKMNPQRS"), 0},
	}
	for _, c := range cases {
		if id, err := NewID(c.letter, c.pass, c.op); !errors.Is(err, ErrID) || id != "" {
			t.Errorf("%s: NewID gave %q, %v", c.name, id, err)
		}
	}
}

func TestPassTime(t *testing.T) {
	at := time.Date(2026, 11, 11, 6, 7, 8, 123_000_000, time.UTC)
	got, ok := PassTime(ulid(at, "ABCDEFGHJKMNPQRS"))
	if !ok || !got.Equal(at) {
		t.Fatalf("PassTime gave %v, %v", got, ok)
	}
	if _, ok := PassTime("01JD3K8Q2M7V5T9R4N6W0X1Y2"); ok {
		t.Fatal("a 25-character pass has a time")
	}
}
