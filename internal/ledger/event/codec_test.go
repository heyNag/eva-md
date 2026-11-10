package event

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func golden(t *testing.T, name string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "codec", name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("%s does not end with a newline", name)
	}
	return bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
}

// Every golden event decodes, and encodes back to the same bytes.
func TestGoldenEventsRoundTrip(t *testing.T) {
	for i, line := range golden(t, "events.jsonl") {
		e, err := Decode(line)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if e.Seq != int64(i+1) {
			t.Fatalf("line %d holds seq %d", i+1, e.Seq)
		}
		out, err := Encode(e)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if !bytes.Equal(out, line) {
			t.Fatalf("line %d does not round-trip:\n got %s\nwant %s", i+1, out, line)
		}
	}
}

// The golden file holds every kind, author, mark, exposure, cut reason and
// operation, and spans with and without a handle, so the round trip covers
// the whole model.
func TestGoldenEventsCoverTheModel(t *testing.T) {
	seen := map[string]bool{}
	for _, line := range golden(t, "events.jsonl") {
		e, err := Decode(line)
		if err != nil {
			t.Fatal(err)
		}
		seen["kind "+string(e.Kind)] = true
		seen["author "+string(e.Author)] = true
		for _, m := range e.Marks {
			seen["mark "+string(m)] = true
		}
		for _, x := range e.Exposure {
			seen["exposure "+x.Kind] = true
		}
		for _, c := range e.Cut {
			seen["cut "+c.Why] = true
		}
		for _, s := range e.Red {
			seen["handle "+map[bool]string{true: "yes", false: "no"}[s.Handle != ""]] = true
		}
		if e.Op != "" {
			seen["op "+e.Op] = true
		}
		if e.Copy != 0 {
			seen["copy"] = true
		}
		if e.Ctx != nil && e.Ctx.Head != "" {
			seen["ctx"] = true
		}
	}
	var want []string
	for _, k := range kinds {
		want = append(want, "kind "+string(k))
	}
	for _, a := range authors {
		want = append(want, "author "+string(a))
	}
	for _, m := range marks {
		want = append(want, "mark "+string(m))
	}
	for _, x := range exposures {
		want = append(want, "exposure "+x)
	}
	for _, c := range cutReasons {
		want = append(want, "cut "+c)
	}
	want = append(want, "handle yes", "handle no", "op off", "op on", "op forget", "copy", "ctx")
	for _, w := range want {
		if !seen[w] {
			t.Errorf("the golden events hold no %s", w)
		}
	}
}

func TestGoldenHeadersRoundTrip(t *testing.T) {
	for i, line := range golden(t, "header.jsonl") {
		h, err := DecodeHeader(line)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		out, err := EncodeHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, line) {
			t.Fatalf("line %d does not round-trip:\n got %s\nwant %s", i+1, out, line)
		}
	}
	for _, bad := range []string{
		`{"v":1,"k":"header","host":"codex","session":"s"}`,
		`{"v":2,"k":"event","host":"codex","session":"s"}`,
		`{"v":2,"k":"header","host":"","session":"s"}`,
		`{"v":2,"k":"header","host":"codex","session":""}`,
		`{"v":2,"k":"header","host":"codex","session":"s","seq":1}`,
	} {
		if _, err := DecodeHeader([]byte(bad)); err == nil {
			t.Errorf("DecodeHeader accepted %s", bad)
		}
	}
	if _, err := EncodeHeader(Header{Host: "codex"}); err == nil {
		t.Error("EncodeHeader accepted a header with no session")
	}
}

// Every line of invalid.jsonl is refused. Each is well-formed JSON unless
// its comment says otherwise, so it is refused for what the comment says
// and not for a typo.
func TestInvalidEventsRefused(t *testing.T) {
	why := ""
	n := 0
	for _, line := range golden(t, "invalid.jsonl") {
		if text, ok := bytes.CutPrefix(line, []byte("# ")); ok {
			why = string(text)
			continue
		}
		n++
		if !strings.Contains(why, "trailing data") && !json.Valid(line) {
			t.Errorf("%s: the line is not JSON: %s", why, line)
		}
		if e, err := Decode(line); err == nil {
			t.Errorf("%s: Decode accepted %s as %+v", why, line, e)
		}
	}
	if n < 40 {
		t.Fatalf("only %d invalid lines were read", n)
	}
}

// Encode refuses what Decode refuses: an event that breaks the invariants
// is never written.
func TestEncodeRefusesInvalidEvent(t *testing.T) {
	for _, e := range []Event{
		{},
		{Seq: 1, Kind: KindPrompt, Author: AuthorTyped},
		{Seq: 1, Kind: KindGap, Author: AuthorEva, Text: "x", Why: WhyOffRecord, Marks: []Mark{MarkWithheld}, Src: Source{Key: "k"}},
	} {
		if _, err := Encode(e); err == nil {
			t.Errorf("Encode accepted %+v", e)
		}
	}
}

// A span is written as an array, with the handle only when there is one.
func TestSpanForm(t *testing.T) {
	for _, c := range []struct {
		s    Span
		want string
	}{
		{Span{1, 5, "key", "T1", ""}, `[1,5,"key","T1"]`},
		{Span{1, 5, "key", "T1", "h_1"}, `[1,5,"key","T1","h_1"]`},
	} {
		got, err := json.Marshal(c.s)
		if err != nil || string(got) != c.want {
			t.Errorf("%+v is written as %s, %v; want %s", c.s, got, err, c.want)
		}
		var back Span
		if err := json.Unmarshal(got, &back); err != nil || back != c.s {
			t.Errorf("%s reads back as %+v, %v", got, back, err)
		}
	}
}

func TestValidKnowsOnlyItsValues(t *testing.T) {
	for _, k := range kinds {
		if !k.Valid() {
			t.Errorf("kind %q", k)
		}
	}
	for _, a := range authors {
		if !a.Valid() {
			t.Errorf("author %q", a)
		}
	}
	for _, c := range classes {
		if !c.Valid() {
			t.Errorf("class %q", c)
		}
	}
	if Kind("Prompt").Valid() || Author("Typed").Valid() || Class("").Valid() || Author("").Valid() {
		t.Error("an unknown value is valid")
	}
	if slices.Contains(authors, Author("user")) {
		t.Error("there is no plain user author: v1's is v1_user, and typed needs evidence")
	}
}
