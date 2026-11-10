package event

import (
	"strconv"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
)

func TestIDForms(t *testing.T) {
	id := ID{Host: "claude-code", Session: "5f0c9a1e-77aa-4000-8000-00000000abcd", Seq: 42}
	if got := id.String(); got != "claude-code:5f0c9a1e-77aa-4000-8000-00000000abcd#42" {
		t.Fatalf("String = %q", got)
	}
	if got := id.Ref(); got != "e:5f0c9a1e#42" {
		t.Fatalf("Ref = %q", got)
	}
	back, err := ParseID(id.String())
	if err != nil || back != id {
		t.Fatalf("ParseID(%q) = %+v, %v", id.String(), back, err)
	}
	// A session id may hold a colon; the host never does.
	odd := ID{Host: "codex", Session: "a:b", Seq: 1}
	if back, err := ParseID(odd.String()); err != nil || back != odd {
		t.Fatalf("ParseID(%q) = %+v, %v", odd.String(), back, err)
	}
	for _, bad := range []string{
		"", "codex", "codex:s", "codex:s#", "codex:s#0", "codex:s#01", "codex:s#-1", "codex:s#1x",
		":s#1", "Codex:s#1", "codex:#1", "codex:a/b#1", "codex:a\x00b#1", "codex:a‮b#1",
		"codex:s#99999999999999999999",
	} {
		if id, err := ParseID(bad); err == nil {
			t.Errorf("ParseID(%q) = %+v", bad, id)
		}
	}
}

// The short form keeps the first 8 characters of a session id exactly as
// v1's references keep them, so a v1 reference and the event's display form
// name a session the same way.
func TestPrefixMatchesV1References(t *testing.T) {
	for _, session := range []string{"5f0c9a1e-77aa", "short", "12345678", "123456789", "ééééééééé-x", "日本語のセッション名です"} {
		for _, line := range []int{1, 7, 1234} {
			want := inbox.Ref(session, line)
			got := (Ref{Session: Prefix(session), First: int64(line), Last: int64(line), Legacy: true}).String()
			if got != want {
				t.Errorf("session %q line %d: %q, v1 writes %q", session, line, got, want)
			}
			r, err := ParseRef(want)
			if err != nil || !r.Legacy || r.First != int64(line) || !r.Names(session) {
				t.Errorf("ParseRef(%q) = %+v, %v", want, r, err)
			}
		}
	}
}

func TestRefForms(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Ref
	}{
		{"e:5f0c9a1e#40", Ref{Session: "5f0c9a1e", First: 40, Last: 40}},
		{"e:5f0c9a1e#40-44", Ref{Session: "5f0c9a1e", First: 40, Last: 44}},
		{"s:5f0c9a1e#3", Ref{Session: "5f0c9a1e", First: 3, Last: 3, Legacy: true}},
		{"e:5f0c9a1e-77aa-4000#9", Ref{Session: "5f0c9a1e-77aa-4000", First: 9, Last: 9}},
	} {
		got, err := ParseRef(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseRef(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
			continue
		}
		if got.String() != c.in {
			t.Errorf("%+v is written %q, read from %q", got, got.String(), c.in)
		}
	}
	for _, bad := range []string{
		"", "x:5f0c9a1e#1", "e:#1", "e:5f0c9a1e", "e:5f0c9a1e#", "e:5f0c9a1e#0", "e:5f0c9a1e#007",
		"e:5f0c9a1e#4-4", "e:5f0c9a1e#5-4", "e:5f0c9a1e#4-", "e:5f0c9a1e#-4", "s:5f0c9a1e#1-2",
		"e:a/b#1", "e:a\x01b#1", "E:5f0c9a1e#1", "e:5f0c9a1e#1#2",
	} {
		if r, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) = %+v", bad, r)
		}
	}
}

func TestRefNamesASession(t *testing.T) {
	long := "5f0c9a1e-77aa-4000"
	for _, c := range []struct {
		ref     string
		session string
		want    bool
	}{
		{"e:5f0c9a1e#1", long, true},
		{"e:" + long + "#1", long, true},
		{"e:5f0c9a1#1", long, false},    // seven characters name nothing
		{"e:5f0c9a1e-7#1", long, false}, // nor do ten that are not the whole id
		{"e:short#1", "short", true},    // a short id is its own prefix
		{"e:shor#1", "short", false},    // and nothing shorter names it
		{"e:5f0c9a1e#1", "5f0c9a1f-77aa", false},
	} {
		r, err := ParseRef(c.ref)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Names(c.session); got != c.want {
			t.Errorf("%q names %q: %v, want %v", c.ref, c.session, got, c.want)
		}
	}
}

func TestIDRefRoundTrip(t *testing.T) {
	for seq := int64(1); seq < 2000; seq += 97 {
		id := ID{Host: "codex", Session: "0193c2d1-aaaa-7000-8000-" + strconv.FormatInt(seq, 10), Seq: seq}
		r, err := ParseRef(id.Ref())
		if err != nil || r.First != seq || !r.Names(id.Session) {
			t.Fatalf("%q: %+v, %v", id.Ref(), r, err)
		}
	}
}
