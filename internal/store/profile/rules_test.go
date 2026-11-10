package profile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// A lock or a verified mark a person wrote protects in every form YAML
// allows, and only an explicit false or an empty value does not.
func TestProtectionIsReadFailClosed(t *testing.T) {
	cases := []struct {
		front string
		want  Protection
	}{
		{"locked: true", Protection{Locked: true}},
		{"locked: true  # keep it", Protection{Locked: true}},
		{"locked: yes", Protection{Locked: true}},
		{"locked: 1", Protection{Locked: true}},
		{`locked: "false"`, Protection{Locked: true}},
		{"locked: 'false'", Protection{Locked: true}},
		{"locked:", Protection{Locked: true}},
		{"locked: # left empty", Protection{Locked: true}},
		{"locked:\n  - x", Protection{Locked: true}},
		{"locked: false\nlocked: false", Protection{Locked: true}},
		{"locked: false", Protection{}},
		{"locked: False  # not now", Protection{}},
		{"locked: FALSE", Protection{}},
		{"verified: [{\"by\": \"human:user\", \"at\": \"2026-11-06T09:00:00Z\"}]", Protection{Verified: true}},
		{"verified: { by: human:user, at: 2026-06-25T09:00:00Z }", Protection{Verified: true}},
		{"verified:\n  - by: human:user", Protection{Verified: true}},
		{"verified: yes", Protection{Verified: true}},
		{"verified: false", Protection{Verified: true}},
		{"verified: []", Protection{}},
		{"verified: {}", Protection{}},
		{`verified: ""`, Protection{}},
		{"verified: null", Protection{}},
		{"verified: ~", Protection{}},
		{"verified:", Protection{}},
		{"verified: [] # none yet", Protection{}},
		{"Locked: true", Protection{}},
		{"lockedness: true", Protection{}},
	}
	// Any form but the bare key protects, whatever its value.
	for _, f := range lockForms {
		cases = append(cases, struct {
			front string
			want  Protection
		}{f, Protection{Locked: true}})
	}
	for _, f := range verifyForms {
		cases = append(cases, struct {
			front string
			want  Protection
		}{f, Protection{Verified: true}})
	}
	for _, f := range unknownForms {
		cases = append(cases, struct {
			front string
			want  Protection
		}{f, Protection{Locked: true, Verified: true}})
	}
	for _, c := range cases {
		d := parseDoc("x.md", []byte("---\ntype: fact\n"+c.front+"\n---\nbody\n"))
		if got := d.protection(); got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.front, got, c.want)
		}
	}
	// A whole block may be indented; its keys are still top-level keys.
	for _, front := range []string{"  type: fact\n  locked: false", "  type: fact\n  \"locked\": true", "  # note\n  type: fact\n  locked: true"} {
		d := parseDoc("x.md", []byte("---\n"+front+"\n---\nbody\n"))
		if got := d.protection(); !got.Locked {
			t.Errorf("%q: got %+v, want locked", front, got)
		}
	}
	// A key nested under another is not a top-level key.
	if got := parseDoc("x.md", []byte("---\ntype: fact\nnote:\n  locked: true\n---\n")).protection(); got.Locked {
		t.Errorf("a nested key locks: %+v", got)
	}
}

// The forms YAML reads as the key locked, or verified, other than the
// bare key at a line's start.
var (
	lockForms = []string{
		`"locked": true`, `'locked': true`, "locked : true", "locked\t: true", "? locked\n: true", "? locked",
		`"locked": false`, "locked : false", "? locked\n: false", "!!str locked: true", "&a locked: false",
		"? !!str locked\n: true", `"lock\x65d": true`, `"\x6cocked": false`, `"locked" : true`, "&a !!str 'locked' : no",
		"locked :", `"locked":`,
	}
	verifyForms = []string{
		`"verified": [{"by": "human:user", "at": "2026-11-06T09:00:00Z"}]`, "verified : yes", "'verified': []",
		"? verified\n: []", `"verified": null`,
	}
	unknownForms = []string{"*k : true", "<<: {\"locked\": true}", "<<: *defaults", "? [locked]\n: true", "?\n  locked\n: true"}
)

// A lock or a verified mark in any of these forms protects in Check, in a
// file the hand-edit record lists and in a system page, with no fault; in
// an item the record does not list, it protects and the line is in no
// written form.
func TestProtectionFormsInCheck(t *testing.T) {
	type form struct {
		line string
		want Protection
	}
	var forms []form
	for _, f := range lockForms {
		forms = append(forms, form{f, Protection{Locked: true}})
	}
	for _, f := range verifyForms {
		forms = append(forms, form{f, Protection{Verified: true}})
	}
	for _, f := range unknownForms {
		forms = append(forms, form{f, Protection{Locked: true, Verified: true}})
	}
	for _, f := range forms {
		for _, p := range []string{hand, sys} {
			root := copyFixture(t)
			b := readFile(t, root, p)
			put(t, root, p, strings.Replace(b, "\nstatus: stable\n", "\nstatus: stable\n"+f.line+"\n", 1))
			rep, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := rep.Protected[p]; got != f.want {
				t.Errorf("%q in %s: protected %+v, want %+v", f.line, p, got, f.want)
			}
			for _, fault := range rep.Faults {
				t.Errorf("%q in %s: %v", f.line, p, fault)
			}
		}
		root := copyFixture(t)
		edit(t, root, fact, "\nstatus: stable\n", "\nstatus: stable\n"+f.line+"\n")
		rep, _ := Check(root)
		if got := rep.Protected[fact]; got != f.want {
			t.Errorf("%q in an item: protected %+v, want %+v", f.line, got, f.want)
		}
		if len(rep.Faults) == 0 {
			t.Errorf("%q in an item the record does not list: no fault", f.line)
		}
		for _, fault := range rep.Faults {
			if fault.Rule != "written-form" && fault.Rule != "frontmatter" {
				t.Errorf("%q in an item: %v", f.line, fault)
			}
		}
	}
}

func TestNamesLibrary(t *testing.T) {
	for s, want := range map[string]bool{
		"okf": true, "OKF pages": true, "okf-agent-memory": true, ".okf-write.lock": true, "x/okf_x.md": true,
		"okfVersion": true, "OKFVersion": true, "myOkf": true, "okf2": true, "notes/okf-ideas.md": true,
		"bookfair": false, "lookfor": false, "cookfest": false, "BOOKFAIR": false, "Bookfair": false, "okfadapter": false, "": false,
	} {
		if got := namesLibrary(s); got != want {
			t.Errorf("namesLibrary(%q) = %v, want %v", s, got, want)
		}
	}
	// A slug made from an ordinary title passes.
	root := copyFixture(t)
	move(t, root, fact, strings.Replace(fact, "orders-port", "bookfair-port", 1))
	appendTo(t, root, "index.md", "* [Lookfor](lookfor/) - the cookfest notes\n")
	if rep, _ := Check(root); len(rep.Faults) != 0 {
		t.Errorf("faults: %v", rep.Faults)
	}
}

func TestIndexHeadings(t *testing.T) {
	root := copyFixture(t)
	appendTo(t, root, "index.md", "\n## Projects\n\n###### Deep\n")
	if rep, _ := Check(root); len(rep.Faults) != 0 {
		t.Errorf("faults: %v", rep.Faults)
	}
	root = copyFixture(t)
	appendTo(t, root, "index.md", "####### Too deep\n")
	if rep, _ := Check(root); len(rep.Faults) != 1 || rep.Faults[0].Rule != "index" {
		t.Errorf("faults: %v", rep.Faults)
	}
}

// On a tie the unacknowledged line holds, whichever writer's record is
// read first.
func TestHandEditTie(t *testing.T) {
	for _, first := range []string{"acknowledged", "unacknowledged"} {
		second := map[string]string{"acknowledged": "unacknowledged", "unacknowledged": "acknowledged"}[first]
		root := copyFixture(t)
		put(t, root, edits, "path\tat\tstate\n"+hand+"\t2026-11-09T18:30:00+01:00\tacknowledged\n"+cons+"\t2026-11-05T10:00:00+01:00\t"+second+"\n")
		put(t, root, ".eva-md/hand-edits/w-7m2pq4zx.tsv", "path\tat\tstate\n"+cons+"\t2026-11-05T09:00:00Z\t"+first+"\n")
		rep, _ := Check(root)
		if len(rep.Faults) != 0 {
			t.Fatalf("faults: %v", rep.Faults)
		}
		if !slices.Equal(rep.Unacknowledged, []string{cons}) {
			t.Errorf("%s read first: unacknowledged %v", first, rep.Unacknowledged)
		}
	}
}

// A line for a document that is gone stays, as a union cannot drop it, and
// holds nothing; README.md and index.md may be listed, and are then the
// person's text.
func TestHandEditRecordPaths(t *testing.T) {
	root := copyFixture(t)
	remove(t, root, hand)
	appendTo(t, root, edits, areaA+"/items/2026/11/i01qa78gk3m9pz-gone.md\t2026-11-09T19:00:00+01:00\tunacknowledged\n")
	rep, _ := Check(root)
	if len(rep.Faults) != 0 {
		t.Errorf("faults: %v", rep.Faults)
	}
	if len(rep.Unacknowledged) != 0 {
		t.Errorf("a gone path is unacknowledged: %v", rep.Unacknowledged)
	}
	root = copyFixture(t)
	appendTo(t, root, edits, "README.md\t2026-11-09T19:00:00+01:00\tunacknowledged\nindex.md\t2026-11-09T19:00:00+01:00\tacknowledged\n")
	edit(t, root, "README.md", "title: \"eva.md memory\"\n", "title: My notes on the okf store\nowner: me\n")
	appendTo(t, root, "index.md", "my own line\r\n")
	rep, _ = Check(root)
	if len(rep.Faults) != 0 {
		t.Errorf("faults: %v", rep.Faults)
	}
	if !slices.Equal(rep.Unacknowledged, []string{"README.md", hand}) {
		t.Errorf("unacknowledged: %v", rep.Unacknowledged)
	}
	root = copyFixture(t)
	appendTo(t, root, edits, "index.md\t2026-11-09T19:00:00+01:00\tacknowledged\n")
	appendTo(t, root, "index.md", "\x00\n")
	if rep, _ := Check(root); len(rep.Faults) != 1 || rep.Faults[0].Rule != "unreadable" {
		t.Errorf("an unreadable listed index: %v", rep.Faults)
	}
}

func TestUnreadableForms(t *testing.T) {
	cases := map[string]string{
		"a byte-order mark":       bom + "---\ntype: fact\n---\n",
		"invalid UTF-8":           "---\ntype: fact\xff\n---\n",
		"a NUL byte":              "---\ntype: fact\x00\n---\n",
		"a long line":             "---\ntype: fact\n---\n" + strings.Repeat("x", 64<<10+1) + "\n",
		"conflict markers":        "---\ntype: fact\n<<<<<<< ours\ntitle: a\n=======\ntitle: b\n>>>>>>> theirs\n---\n",
		"a lone separator":        "---\ntype: fact\n---\nbody\n=======\n",
		"no frontmatter":          "type: fact\n",
		"frontmatter not on top":  "\n---\ntype: fact\n---\n",
		"a frontmatter not ended": "---\ntype: fact\ntitle: a\n",
		"an empty file":           "",
	}
	for name, b := range cases {
		if d := parseDoc("x.md", []byte(b)); d.unreadable == "" || !d.protection().Unreadable {
			t.Errorf("%s: read as readable", name)
		}
	}
	d := parseDoc("x.md", []byte("---\ntype: fact\n---\n"+strings.Repeat("x", 64<<10)+"\n"))
	if d.unreadable != "" {
		t.Errorf("a line of exactly 64 KiB is unreadable: %s", d.unreadable)
	}
}

func TestWrittenForms(t *testing.T) {
	cases := []struct {
		line    string
		written bool
	}{
		{`title: "Sign commits"`, true},
		{`title: "a: b, c"`, true},
		{`title: "quote \" inside: here"`, true},
		{"state: active", true},
		{"project: p_00gb3367a19r", true},
		{"writer: w-k3f9a2bq", true},
		{`cues: ["a", "b"]`, true},
		{`generated: {"by": "x/y", "at": "2026-11-10T09:00:00Z"}`, true},
		{"sources:\n  - {\"id\": \"e1\", \"n\": 2}", true},
		{"cues: []", true},
		{"title: Sign commits", false},
		{"title: Word", false},
		{"state: true", false},
		{"state: yes", false},
		{"state: n", false},
		{"state: null", false},
		{"state: 12", false},
		{"state: 2026-11-10", false},
		{`title:  "two spaces"`, false},
		{`title: "x" # note`, false},
		{"title: 'single'", false},
		{`cues: ["a","b"]`, false},
		{`cues: [ "a", "b" ]`, false},
		{`generated: {"by":"x/y"}`, false},
		{`generated: {by: x/y}`, false},
		{"sources:\n  - \"e1\"", false},
		{"sources:\n    - {\"id\": \"e1\"}", false},
		{"sources:\n- {\"id\": \"e1\"}", false},
		{"sources:", false},
		{`count: 2`, false},
	}
	for _, c := range cases {
		d := parseDoc("x.md", []byte("---\n"+c.line+"\n---\n"))
		if len(d.entries) != 1 || len(d.opaque) != 0 {
			t.Fatalf("%q: read as %d entries", c.line, len(d.entries))
		}
		if d.entries[0].written != c.written {
			t.Errorf("%q: written %v, want %v", c.line, d.entries[0].written, c.written)
		}
	}
	d := parseDoc("x.md", []byte("---\n# a comment\n\ntype: fact\n---\n"))
	if !slices.Equal(d.opaque, []int{2, 3}) {
		t.Errorf("a comment and a blank line are not kept apart: %v", d.opaque)
	}
}

// A person's line is read as YAML reads it, so a check of its value does
// not depend on the form it was written in.
func TestLenientScalars(t *testing.T) {
	cases := map[string]string{
		" Use the sandbox  # note":  "Use the sandbox",
		` "quoted # not a comment"`: "quoted # not a comment",
		" 'it''s'":                  "it's",
		" plain":                    "plain",
		" # only a comment":         "",
	}
	for in, want := range cases {
		if got := lenient(in); got != want {
			t.Errorf("lenient(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTagRules(t *testing.T) {
	src := func(pv event.Author, supports string) source { return source{pv: pv, supports: supports} }
	typed, spoken := src(event.AuthorTyped, "stated"), src(event.AuthorSpoken, "stated")
	proposal, choice := src(event.AuthorAgent, "suggested"), src(event.AuthorChosen, "accepted")
	tool := src(event.AuthorTool, "observed")
	cases := []struct {
		kind, tag string
		srcs      []source
		migrated  bool
		ok        bool
	}{
		{"rule", "stated", []source{typed}, false, true},
		{"rule", "stated", []source{spoken}, false, false},
		{"constraint", "stated", []source{spoken, tool}, false, false},
		{"constraint", "accepted", []source{proposal, choice}, false, false},
		{"decision", "accepted", []source{proposal, choice}, false, true},
		{"decision", "accepted", []source{choice}, false, false},
		{"decision", "stated", []source{spoken}, false, true},
		{"decision", "observed", []source{tool}, false, false},
		{"procedure", "observed", []source{tool}, false, true},
		{"procedure", "accepted", []source{proposal, choice}, false, true},
		{"fact", "observed", []source{tool, proposal}, false, true},
		{"fact", "accepted", []source{proposal, choice}, false, false},
		{"fact", "stated", []source{tool}, false, false},
		{"fact", "observed", []source{typed, tool}, false, false},
		{"fact", "suggested", []source{proposal}, false, false},
		{"fact", "external", []source{src(event.AuthorFront, "external")}, false, false},
		{"fact", "echo", []source{src(event.AuthorEva, "echo")}, false, false},
		{"fact", "reading", nil, true, true},
		{"rule", "inferred", nil, true, true},
		{"fact", "reading", []source{tool}, false, false},
		{"fact", "suggested", nil, true, false},
	}
	for _, c := range cases {
		why := tagFault(c.kind, c.tag, c.srcs, c.migrated)
		if (why == "") != c.ok {
			t.Errorf("%s tagged %s from %v (migrated %v): fault %q", c.kind, c.tag, c.srcs, c.migrated, why)
		}
	}
}

// The table of what a span may support has a row for each of the record's
// authors, and only for them.
func TestSupportsCoversEveryAuthor(t *testing.T) {
	authors := []event.Author{event.AuthorTyped, event.AuthorPasted, event.AuthorChosen, event.AuthorAgent, event.AuthorSubagent,
		event.AuthorHost, event.AuthorProgram, event.AuthorSpoken, event.AuthorFront, event.AuthorTool, event.AuthorEva,
		event.AuthorV1User, event.AuthorUnknown}
	for _, a := range authors {
		if len(pvSupports[a]) == 0 {
			t.Errorf("no row for author %s", a)
		}
	}
	for a, tags := range pvSupports {
		if !a.Valid() {
			t.Errorf("a row for %s, which is no author", a)
		}
		for _, tag := range tags {
			if _, ok := rank[tag]; !ok {
				t.Errorf("%s may support %s, which is no tag", a, tag)
			}
		}
	}
	if slices.Contains(pvSupports[event.AuthorFront], "stated") || slices.Contains(pvSupports[event.AuthorFront], "accepted") {
		t.Error("the front model's words carry the person's authority")
	}
}

func TestIDs(t *testing.T) {
	if got := idTime("i01qtk73d4e5f6"); !got.Equal(time.Date(2026, 11, 9, 14, 2, 11, 0, time.UTC)) {
		t.Errorf("idTime = %v", got)
	}
	// Ids sort as the times they encode.
	var ids []string
	for i, s := range []int64{0, 31, 32, 1 << 20, 1<<35 - 1} {
		var b strings.Builder
		for j := 6; j >= 0; j-- {
			b.WriteByte(alphabet[s>>(5*j)&31])
		}
		id := "i" + b.String() + "000000"
		if !validID(id, 'i') || idTime(id).Sub(epoch) != time.Duration(s)*time.Second {
			t.Errorf("case %d: %s does not encode %d", i, id, s)
		}
		ids = append(ids, id)
	}
	if !slices.IsSorted(ids) {
		t.Errorf("ids do not sort by time: %v", ids)
	}
	for _, bad := range []string{"i01qtk73d4e5f", "i01qtk73d4e5f67", "I01qtk73d4e5f6", "i01qtk73d4e5fi", "i01qtk73d4e5fl", "i01qtk73d4e5fo", "i01qtk73d4e5fu"} {
		if validID(bad, 'i') {
			t.Errorf("%s is valid", bad)
		}
	}
	if validID("i01qtk73d4e5f6", 'd') {
		t.Error("an item id is a disagreement's")
	}
	a, b := workstreamID("p_00gb3367a19r", "main"), workstreamID("p_00gb3367a19r", "main")
	if a != b || !validID(a, 'w') || a == workstreamID("p_00gb3367a19r", "dev") || a == workstreamID("p_6pbe1q7fxb5h", "main") {
		t.Errorf("workstream ids: %s %s", a, b)
	}
	if bits([]byte{0xff, 0xff}, 3) != "zzz" || bits([]byte{0, 0}, 3) != "000" || bits([]byte{0x08, 0x86}, 3) != "123" {
		t.Error("bits does not read five bits at a time, high first")
	}
	if sessionKey("5f0c9a1e-2b7d") != "5f0c9a1e-2b7d" {
		t.Error("a safe session id is not kept")
	}
	if k := sessionKey("thread.with dots"); len(k) != 14 || k[0] != 'x' || k != sessionKey("thread.with dots") {
		t.Errorf("an unsafe session id gives %q", k)
	}
	if k := sessionKey(strings.Repeat("a", 81)); k[0] != 'x' {
		t.Errorf("a session id over 80 characters is kept: %q", k)
	}
}

func TestDuplicateIDs(t *testing.T) {
	root := copyFixture(t)
	b := readFile(t, root, fact)
	put(t, root, strings.Replace(fact, "orders-port", "orders-port-again", 1), b)
	rep, _ := Check(root)
	if len(rep.Faults) != 1 || rep.Faults[0].Rule != "id" || !strings.Contains(rep.Faults[0].String(), "orders-port-again") {
		t.Errorf("a repeated id: %v", rep.Faults)
	}
}

// A reference to a file that cannot be read still resolves: what the file
// holds is unknown, not wrong.
func TestReferencesToAnUnreadableItem(t *testing.T) {
	root := copyFixture(t)
	edit(t, root, dis, "item: i01qcsw8g7h8j9", "item: i01qmw90s6t7v8")
	edit(t, root, ws1, "/items/2026/11/i01qtk73d4e5f6-sign-with-machine-key.md", "/items/2026/11/i01qmw90s6t7v8-broken-merge.md")
	if rep, _ := Check(root); len(rep.Faults) != 0 {
		t.Errorf("faults: %v", rep.Faults)
	}
}

// The newest line for a path holds, across writers.
func TestHandEditNewestLineHolds(t *testing.T) {
	root := copyFixture(t)
	put(t, root, ".eva-md/hand-edits/w-7m2pq4zx.tsv", "path\tat\tstate\n"+hand+"\t2026-11-09T18:31:00+01:00\tacknowledged\n"+cons+"\t2026-11-05T09:30:00+01:00\tunacknowledged\n")
	rep, _ := Check(root)
	if len(rep.Faults) != 0 {
		t.Fatalf("faults: %v", rep.Faults)
	}
	// The sandbox item's later acknowledgement holds; the constraint's
	// acknowledgement at 10:00 is newer than the other writer's 09:30.
	if len(rep.Unacknowledged) != 0 {
		t.Errorf("unacknowledged: %v", rep.Unacknowledged)
	}
	root = copyFixture(t)
	put(t, root, ".eva-md/hand-edits/w-7m2pq4zx.tsv", "path\tat\tstate\n"+cons+"\t2026-11-05T11:00:00+01:00\tunacknowledged\n")
	rep, _ = Check(root)
	if !slices.Equal(rep.Unacknowledged, []string{cons, hand}) {
		t.Errorf("unacknowledged: %v", rep.Unacknowledged)
	}
}

// A person's own lines are faults in a file the hand-edit record does not
// list: an agent with a file tool cannot slip text past the format.
func TestUnlistedHandEditIsAFault(t *testing.T) {
	root := copyFixture(t)
	put(t, root, edits, "path\tat\tstate\n"+cons+"\t2026-11-05T10:00:00+01:00\tacknowledged\n")
	rep, _ := Check(root)
	var rules []string
	for _, f := range rep.Faults {
		if f.Path != hand {
			t.Errorf("a fault outside the hand-edited file: %v", f)
		}
		rules = append(rules, f.Rule)
	}
	if !slices.Contains(rules, "written-form") {
		t.Errorf("the person's lines pass unlisted: %v", rep.Faults)
	}
}

func TestSessionFileNameForAnUnsafeID(t *testing.T) {
	root := copyFixture(t)
	sid := "thread.with dots"
	b := strings.Replace(readFile(t, root, ses), "claude-code:5f0c9a1e-2b7d-4c1a-9e3f-0a1b2c3d4e5f\"", "claude-code:"+sid+"\"", 1)
	remove(t, root, ses)
	put(t, root, areaA+"/sessions/2026/11/claude-code."+sessionKey(sid)+".w-k3f9a2bq.md", b)
	if rep, _ := Check(root); len(rep.Faults) != 0 {
		t.Errorf("faults: %v", rep.Faults)
	}
}

func TestFaultNamesFileFieldAndRule(t *testing.T) {
	s := Fault{Path: "a/b.md", Field: "title", Rule: "title", Detail: "is required"}.String()
	if s != "a/b.md: title: is required (rule title)" {
		t.Errorf("%q", s)
	}
	if s := (Fault{Path: "a", Rule: "path", Detail: "x"}).String(); !strings.Contains(s, "(file)") {
		t.Errorf("%q", s)
	}
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
