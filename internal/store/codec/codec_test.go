package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"testing/iotest"
)

const item = "---\ntype: fact\ntitle: \"A title\"\nstatus: stable\n---\nThe statement.\n\n## Evidence\n[^e1]: x\n"

func mustWrite(t *testing.T, d *Doc) string {
	t.Helper()
	b, err := Write(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Every edit of a locked or unreadable document, and any Set or Delete of
// locked, is refused with ErrProtected and changes nothing.
func TestRefusals(t *testing.T) {
	edits := map[string]func(d *Doc) error{
		"Set":            func(d *Doc) error { return d.Set("title", "x") },
		"SetBlock":       func(d *Doc) error { return d.SetBlock("sources", []map[string]string{{"id": "e1"}}) },
		"Delete":         func(d *Doc) error { return d.Delete("title") },
		"SetPreamble":    func(d *Doc) error { return d.SetPreamble("x\n") },
		"SetSection":     func(d *Doc) error { return d.SetSection("Evidence", "x\n") },
		"SetSection new": func(d *Doc) error { return d.SetSection("Detail", "x\n") },
		"RemoveSection":  func(d *Doc) error { return d.RemoveSection("Evidence") },
	}
	docs := map[string]string{
		"locked":            strings.Replace(item, "status: stable\n", "status: stable\nlocked: true  # x\n", 1),
		"locked by alias":   strings.Replace(item, "status: stable\n", "status: stable\n*k : true\n", 1),
		"locked twice":      strings.Replace(item, "status: stable\n", "locked: false\nstatus: stable\nlocked: false\n", 1),
		"unreadable":        strings.Replace(item, "status: stable\n", "status: stable\n=======\n", 1),
		"unclosed":          strings.Replace(item, "---\nThe", "The", 1),
		"a byte-order mark": "\xef\xbb\xbf" + item,
		"empty":             "",
	}
	for dn, in := range docs {
		for en, edit := range edits {
			d := Read([]byte(in))
			if err := edit(d); !errors.Is(err, ErrProtected) {
				t.Errorf("%s, %s: %v, want ErrProtected", dn, en, err)
			}
			if got := mustWrite(t, d); got != in {
				t.Errorf("%s, %s: the refused document does not write its input", dn, en)
			}
		}
	}
	// The zero Doc reads as the empty document.
	var zero Doc
	if err := zero.Set("title", "x"); !errors.Is(err, ErrProtected) || mustWrite(t, &zero) != "" || zero.Unreadable() == "" {
		t.Errorf("the zero Doc: %v", err)
	}
	if _, err := Write(nil); err == nil {
		t.Error("Write(nil) did not fail")
	}
	// locked is never written, whatever the document holds.
	for _, in := range []string{item, strings.Replace(item, "status: stable\n", "locked: false\nstatus: stable\n", 1)} {
		for name, edit := range map[string]func(d *Doc) error{
			"Set":       func(d *Doc) error { return d.Set("locked", true) },
			"Set false": func(d *Doc) error { return d.Set("locked", "false") },
			"SetBlock":  func(d *Doc) error { return d.SetBlock("locked", []map[string]bool{{"x": true}}) },
			"Delete":    func(d *Doc) error { return d.Delete("locked") },
		} {
			d := Read([]byte(in))
			if err := edit(d); !errors.Is(err, ErrProtected) {
				t.Errorf("%s of locked: %v, want ErrProtected", name, err)
			}
			if mustWrite(t, d) != in {
				t.Errorf("%s of locked: the refused document does not write its input", name)
			}
		}
	}
	// A verified mark is reported, not enforced: the store core decides
	// who may change a verified document.
	in := strings.Replace(item, "status: stable\n", "status: stable\nverified: [{\"by\": \"human:user\", \"at\": \"2026-11-06T09:00:00Z\"}]\n", 1)
	d := Read([]byte(in))
	if !d.Protection().Verified {
		t.Fatal("not verified")
	}
	if err := d.Set("title", "x"); err != nil {
		t.Errorf("an edit of a verified document: %v", err)
	}
}

// Other refusals: a repeated key or section, a key or block the codec does
// not write, a value that is not a string, list or object, and an edit
// that would not read back.
func TestOtherRefusals(t *testing.T) {
	cases := []struct {
		name string
		in   string
		edit func(d *Doc) error
		want error
	}{
		{"Set repeated", "---\nstate: active\nstate: retired\n---\n", func(d *Doc) error { return d.Set("state", "active") }, ErrRepeated},
		{"Delete repeated", "---\nstate: active\nstate: retired\n---\n", func(d *Doc) error { return d.Delete("state") }, ErrRepeated},
		{"Delete absent", item, func(d *Doc) error { return d.Delete("cues") }, ErrNoKey},
		{"a number", item, func(d *Doc) error { return d.Set("cues", 3) }, ErrNotWritten},
		{"a bool", item, func(d *Doc) error { return d.Set("cues", true) }, ErrNotWritten},
		{"null", item, func(d *Doc) error { return d.Set("cues", nil) }, ErrNotWritten},
		{"a nil slice", item, func(d *Doc) error { return d.Set("cues", []string(nil)) }, ErrNotWritten},
		{"a value JSON refuses", item, func(d *Doc) error { return d.Set("cues", math.Inf(1)) }, ErrNotWritten},
		{"a channel", item, func(d *Doc) error { return d.Set("cues", make(chan int)) }, ErrNotWritten},
		{"a key with a space", item, func(d *Doc) error { return d.Set("my key", "x") }, ErrNotWritten},
		{"an upper-case key", item, func(d *Doc) error { return d.Set("Title", "x") }, ErrNotWritten},
		{"a YAML word as a key", item, func(d *Doc) error { return d.Set("yes", "x") }, ErrNotWritten},
		{"an empty block", item, func(d *Doc) error { return d.SetBlock("sources", []any{}) }, ErrNotWritten},
		{"a block of strings", item, func(d *Doc) error { return d.SetBlock("sources", []string{"a"}) }, ErrNotWritten},
		{"a block that is an object", item, func(d *Doc) error { return d.SetBlock("sources", map[string]int{"a": 1}) }, ErrNotWritten},
		{"an indented block", "---\n  type: fact\n  title: x\n---\n", func(d *Doc) error { return d.Set("title", "y") }, ErrNotWritten},
		{"a line over 64 KiB", item, func(d *Doc) error { return d.Set("title", strings.Repeat("x ", 33<<10)) }, ErrNotWritten},
		{"an explicit key would take a value", "---\n? note\nk: v\n: x\n---\n", func(d *Doc) error { return d.Delete("k") }, ErrNotWritten},
		{"a flow mapping would become the block", "---\nk: v\n{b: 2}\n---\n", func(d *Doc) error { return d.Delete("k") }, ErrNotWritten},
		{"a repeated section", item + "## Evidence\nmore\n", func(d *Doc) error { return d.SetSection("Evidence", "x\n") }, ErrRepeated},
		{"remove a repeated section", item + "## Evidence\nmore\n", func(d *Doc) error { return d.RemoveSection("Evidence") }, ErrRepeated},
		{"remove an absent section", item, func(d *Doc) error { return d.RemoveSection("Detail") }, ErrNoKey},
		{"a text that starts a section", item, func(d *Doc) error { return d.SetPreamble("x\n## Evidence\n") }, ErrNotWritten},
		{"a text that opens a fence", item, func(d *Doc) error { return d.SetPreamble("```\n") }, ErrNotWritten},
		{"a text with a conflict marker", item, func(d *Doc) error { return d.SetSection("Evidence", "=======\n") }, ErrNotWritten},
		{"a text without a final newline before more", item, func(d *Doc) error { return d.SetPreamble("x") }, ErrNotWritten},
		{"a heading with a line end", item, func(d *Doc) error { return d.SetSection("A\nB", "x\n") }, ErrNotWritten},
		{"a section after an open fence", item + "```\n", func(d *Doc) error { return d.SetSection("Detail", "x\n") }, ErrNotWritten},
	}
	for _, c := range cases {
		d := Read([]byte(c.in))
		err := c.edit(d)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if mustWrite(t, d) != c.in {
			t.Errorf("%s: the refused document does not write its input", c.name)
		}
	}
}

func TestValue(t *testing.T) {
	in := "---\n" +
		"state: active\n" +
		"title: \"Sign: commits, \\\"now\\\"\"\n" +
		"cues: [\"a\", \"b\"]\n" +
		"generated: {\"by\": \"x/y\", \"n\": 12, \"f\": 1.5}\n" +
		"sources:\n  - {\"id\": \"e1\"}\n  - {\"id\": \"e2\", \"n\": 2}\n" +
		"twice: a\ntwice: b\n" +
		"plain: Two words\n" +
		"word: yes\n" +
		"spaced: [ \"a\" ]\n" +
		"commented: \"x\" # note\n" +
		"block: |\n  text\n" +
		"strings:\n  - \"a\"\n" +
		"\"quoted\": x\n" +
		"empty:\n" +
		"---\nbody\n"
	d := Read([]byte(in))
	want := map[string]any{
		"state":     "active",
		"title":     `Sign: commits, "now"`,
		"cues":      []any{"a", "b"},
		"generated": map[string]any{"by": "x/y", "n": json.Number("12"), "f": json.Number("1.5")},
		"sources":   []any{map[string]any{"id": "e1"}, map[string]any{"id": "e2", "n": json.Number("2")}},
	}
	for k, w := range want {
		if got, err := d.Value(k); err != nil || !reflect.DeepEqual(got, w) {
			t.Errorf("%s: %#v (%v), want %#v", k, got, err, w)
		}
	}
	errs := map[string]error{
		"twice": ErrRepeated, "absent": ErrNoKey, "plain": ErrNotWritten, "word": ErrNotWritten, "spaced": ErrNotWritten,
		"commented": ErrNotWritten, "block": ErrNotWritten, "strings": ErrNotWritten, "quoted": ErrNotWritten, "empty": ErrNotWritten,
	}
	for k, w := range errs {
		if _, err := d.Value(k); err != w {
			t.Errorf("%s: %v, want %v", k, err, w)
		}
	}
	if _, err := Read([]byte("\xef\xbb\xbf" + in)).Value("state"); err != ErrNotWritten {
		t.Errorf("an unreadable document: %v", err)
	}
	// A value Value gives is the caller's: changing it changes no other.
	v, _ := d.Value("cues")
	v.([]any)[0] = "changed"
	if v2, _ := d.Value("cues"); v2.([]any)[0] != "a" {
		t.Error("Value shares its result")
	}
}

// The written form: plain when it can be, otherwise JSON with HTML
// escaping off, struct order kept, one space after each colon and comma
// outside strings.
func TestWrittenForm(t *testing.T) {
	type src struct {
		ID       string `json:"id"`
		Resource string `json:"resource"`
		PV       string `json:"pv"`
	}
	cases := []struct {
		v    any
		line string
	}{
		{"active", "k: active"},
		{"p_00gb3367a19r", "k: p_00gb3367a19r"},
		{"w-k3f9a2bq", "k: w-k3f9a2bq"},
		{"a.b-c_d", "k: a.b-c_d"},
		{"true", `k: "true"`},
		{"yes", `k: "yes"`},
		{"n", `k: "n"`},
		{"null", `k: "null"`},
		{"Active", `k: "Active"`},
		{"12", `k: "12"`},
		{"2026-11-10", `k: "2026-11-10"`},
		{"", `k: ""`},
		{"two words", `k: "two words"`},
		{"a: b, c", `k: "a: b, c"`},
		{"<b>&</b>", `k: "<b>&</b>"`},
		{"line\nbreak # x", `k: "line\nbreak # x"`},
		{`"quoted", \ here: yes`, `k: "\"quoted\", \\ here: yes"`},
		{[]string{"a", "b, c"}, `k: ["a", "b, c"]`},
		{[]any{}, `k: []`},
		{map[string]any{}, `k: {}`},
		{map[string]any{"b": 1, "a": []int{1, 2}}, `k: {"a": [1, 2], "b": 1}`},
		{src{"e1", "claude-code:x#1", "tool"}, `k: {"id": "e1", "resource": "claude-code:x#1", "pv": "tool"}`},
		{json.RawMessage(`{ "z" :1 ,"a":[ ] }`), `k: {"z": 1, "a": []}`},
	}
	for _, c := range cases {
		d := Read([]byte("---\ntype: fact\n---\n"))
		if err := d.Set("k", c.v); err != nil {
			t.Errorf("%#v: %v", c.v, err)
			continue
		}
		if got, want := mustWrite(t, d), "---\ntype: fact\n"+c.line+"\n---\n"; got != want {
			t.Errorf("%#v: wrote %q, want %q", c.v, got, want)
		}
	}
	d := Read([]byte("---\ntype: fact\n---\n"))
	if err := d.SetBlock("sources", []src{{"e1", "r:1", "tool"}, {"e2", "a, b: c", "typed"}}); err != nil {
		t.Fatal(err)
	}
	want := "---\ntype: fact\nsources:\n  - {\"id\": \"e1\", \"resource\": \"r:1\", \"pv\": \"tool\"}\n  - {\"id\": \"e2\", \"resource\": \"a, b: c\", \"pv\": \"typed\"}\n---\n"
	if got := mustWrite(t, d); got != want {
		t.Errorf("SetBlock wrote %q, want %q", got, want)
	}
}

// An edit changes only the lines of what it names, in whatever form the
// entry was written; a new key goes after the last entry.
func TestSetKeepsOtherLines(t *testing.T) {
	cases := []struct {
		name, in, key string
		v             any
		want          string
	}{
		{"a quoted key", "---\ntype: fact\n\"title\": x # c\nstatus: stable\n---\n", "title", "z", "---\ntype: fact\ntitle: z\nstatus: stable\n---\n"},
		{"an explicit key", "---\ntype: fact\n? title\n: x\nstatus: stable\n---\n", "title", "z", "---\ntype: fact\ntitle: z\nstatus: stable\n---\n"},
		{"a block scalar", "---\ntype: fact\ndescription: |\n  one\n\n  two\nstatus: stable\n---\n", "description", "d", "---\ntype: fact\ndescription: d\n\nstatus: stable\n---\n"},
		{"a comment inside a block", "---\nsources:\n# keep me\n  - {\"id\": \"e1\"}\n---\n", "sources", []string{"x"}, "---\nsources: [\"x\"]\n# keep me\n---\n"},
		{"a new key after trailing comments", "---\ntype: fact\nsources:\n  - {\"id\": \"e1\"}\n# end\n\n---\nbody\n", "title", "t", "---\ntype: fact\nsources:\n  - {\"id\": \"e1\"}\ntitle: t\n# end\n\n---\nbody\n"},
		{"a new key in an empty block", "---\n# only a comment\n---\n", "type", "fact", "---\n# only a comment\ntype: fact\n---\n"},
		{"CRLF lines kept", "---\r\ntype: fact\r\ntitle: x\r\n---\r\nbody\r\n", "title", "z", "---\r\ntype: fact\r\ntitle: z\n---\r\nbody\r\n"},
	}
	for _, c := range cases {
		d := Read([]byte(c.in))
		if err := d.Set(c.key, c.v); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got := mustWrite(t, d); got != c.want {
			t.Errorf("%s: wrote %q, want %q", c.name, got, c.want)
		}
	}
	d := Read([]byte("---\ntype: fact\ntitle: x\n# about status\nstatus: stable\n---\n"))
	if err := d.Delete("title"); err != nil {
		t.Fatal(err)
	}
	if got := mustWrite(t, d); got != "---\ntype: fact\n# about status\nstatus: stable\n---\n" {
		t.Errorf("Delete wrote %q", got)
	}
}

func TestSections(t *testing.T) {
	in := "---\ntype: fact\n---\nThe statement.\n\n```\n## not a section\n```\n\n## Detail\nwhy\n\n## Evidence\n[^e1]: x\n"
	d := Read([]byte(in))
	if got := d.Preamble(); got != "The statement.\n\n```\n## not a section\n```\n\n" {
		t.Errorf("preamble %q", got)
	}
	if got, err := d.Section("Detail"); err != nil || got != "why\n\n" {
		t.Errorf("Detail: %q %v", got, err)
	}
	if _, err := d.Section("not a section"); err != ErrNoKey {
		t.Errorf("a heading in a fence is a section: %v", err)
	}
	if err := d.SetSection("Detail", "because\n"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetSection("Log", "- did: x\n"); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveSection("Evidence"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPreamble("New.\n\n"); err != nil {
		t.Fatal(err)
	}
	if got, want := mustWrite(t, d), "---\ntype: fact\n---\nNew.\n\n## Detail\nbecause\n## Log\n- did: x\n"; got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
	// A document with no final newline gets one only where more follows.
	d = Read([]byte("---\ntype: fact\n---"))
	if err := d.SetSection("Detail", "x"); err != nil {
		t.Fatal(err)
	}
	if got := mustWrite(t, d); got != "---\ntype: fact\n---\n## Detail\nx" {
		t.Errorf("wrote %q", got)
	}
	d = Read([]byte("---\ntype: fact\n---\nend"))
	if err := d.SetSection("Detail", "x\n"); err != nil {
		t.Fatal(err)
	}
	if got := mustWrite(t, d); got != "---\ntype: fact\n---\nend\n## Detail\nx\n" {
		t.Errorf("wrote %q", got)
	}
	// Fences: a tilde fence is not closed by backticks, nor a long fence
	// by a short one; a backtick line with a backtick after is no fence.
	for body, n := range map[string]int{
		"~~~\n## a\n```\n## b\n~~~\n## c\n":     1,
		"````\n## a\n```\n## b\n````\n## c\n":   1,
		"``` x`y\n## a\n":                       1,
		"   ```\n## a\n   ```\n## b\n":          1,
		"    ```\n## a\n":                       1,
		"```\n## a\n``` \t\n## b\n":             1,
		"##  two spaces\n###  three\n##\n## \n": 2,
	} {
		if got := len(Read([]byte("---\n---\n" + body)).sections); got != n {
			t.Errorf("%q: %d sections, want %d", body, got, n)
		}
	}
}

func TestReadFrom(t *testing.T) {
	b := []byte(item)
	for _, max := range []int64{int64(len(b)), int64(len(b)) + 1, math.MaxInt64} {
		d, err := ReadFrom(bytes.NewReader(b), max)
		if err != nil || mustWrite(t, d) != item {
			t.Errorf("max %d: %v", max, err)
		}
	}
	// It reads at most max bytes and one.
	r := &counting{r: strings.NewReader(item + strings.Repeat("x", 1<<20))}
	if _, err := ReadFrom(r, 100); err != ErrTooLarge {
		t.Errorf("over the bound: %v", err)
	}
	if r.n > 101 {
		t.Errorf("read %d bytes for a bound of 100", r.n)
	}
	if _, err := ReadFrom(strings.NewReader("x"), -1); err != ErrTooLarge {
		t.Errorf("a negative bound: %v", err)
	}
	if d, err := ReadFrom(strings.NewReader(""), -1); err != nil || d.Unreadable() == "" {
		t.Errorf("an empty reader: %v", err)
	}
	failing := errors.New("disk on fire")
	if _, err := ReadFrom(iotest.ErrReader(failing), 100); !errors.Is(err, failing) {
		t.Errorf("a failing reader: %v", err)
	}
}

type counting struct {
	r io.Reader
	n int
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestUnreadableReasons(t *testing.T) {
	cases := map[string]string{
		"a byte-order mark":                 "\xef\xbb\xbf" + item,
		"invalid UTF-8":                     "---\ntype: \xff\n---\n",
		"a NUL byte":                        "---\ntype: \x00\n---\n",
		"line 4 is over 64 KiB":             "---\ntype: fact\n---\n" + strings.Repeat("x", 64<<10+1) + "\n",
		"a conflict marker on line 2":       "---\n>>>>>>> theirs\n---\n",
		"a conflict marker on line 4":       "---\n---\nbody\n|||||||\r\n",
		"no frontmatter on line 1":          "\n---\n---\n",
		"a frontmatter that does not close": "---\ntype: fact\n--- \n",
	}
	for want, in := range cases {
		if got := Read([]byte(in)).Unreadable(); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	readable := []string{"---\n---\n", "---\r\n---", "---\n---\n" + strings.Repeat("x", 64<<10) + "\n", "---\n---\n========\n", "---\n---\n<<<<<<<x\n"}
	for _, in := range readable {
		if got := Read([]byte(in)).Unreadable(); got != "" {
			t.Errorf("%q: %q", in, got)
		}
	}
}

// The spec is at most 24 KB, and each test it names exists.
func TestSpec(t *testing.T) {
	spec, err := os.ReadFile("SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(spec) > 24<<10 {
		t.Errorf("SPEC.md is %d bytes, over 24 KB", len(spec))
	}
	var code []byte
	files, _ := os.ReadDir(".")
	for _, f := range files {
		if strings.HasSuffix(f.Name(), "_test.go") {
			b, _ := os.ReadFile(f.Name())
			code = append(code, b...)
		}
	}
	names := regexp.MustCompile("`((Test|Fuzz)[A-Za-z]+)`").FindAllStringSubmatch(string(spec), -1)
	if len(names) < 15 {
		t.Fatalf("SPEC.md names %d tests", len(names))
	}
	for _, m := range names {
		if !bytes.Contains(code, []byte("func "+m[1]+"(")) {
			t.Errorf("SPEC.md names %s, which does not exist", m[1])
		}
	}
}

// No error text, and no line of the package's code, names the library v1
// was built on.
func TestNoLibraryName(t *testing.T) {
	re := regexp.MustCompile(`(?i)okf`)
	for _, err := range []error{ErrProtected, ErrNoKey, ErrRepeated, ErrNotWritten, ErrTooLarge} {
		if re.MatchString(err.Error()) {
			t.Errorf("%q names the library", err)
		}
	}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") && f.Name() != "SPEC.md" {
			continue
		}
		b, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(f.Name(), "_test.go") {
			// Tests may name it only to look for it.
			b = bytes.ReplaceAll(b, []byte("(?i)okf"), nil)
		}
		if re.Match(b) {
			t.Errorf("%s names the library", f.Name())
		}
	}
}
