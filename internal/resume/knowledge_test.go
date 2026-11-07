package resume

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/okfadapter"
)

// knowledgeAt is when the fixtures' knowledge was stated, and later when a
// statement was superseded.
var (
	knowledgeAt = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	laterAt     = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
)

// indexRow is a row of the recall index as knowledgepage writes it: id,
// kind, tag, scope, project, as_of, time, access, title, statement, paths.
func indexRow(id, kind, scope, project, asOf, title, statement string) string {
	return strings.Join([]string{id, kind, "stated", scope, project, asOf, "", "", title, statement, ""}, "\t")
}

// writeIndex writes the recall index with the rows given, as knowledgepage
// writes it, and nothing else of the knowledge pages.
func writeIndex(t *testing.T, o Options, rows ...string) {
	t.Helper()
	file := filepath.Join(o.Config.WikiPath, filepath.FromSlash(knowledgepage.IndexFile))
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	text := "# eva-md knowledge index 1\n"
	for _, r := range rows {
		text += r + "\n"
	}
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// constraintLines returns the lines of a brief that list constraints.
func constraintLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "Constraint: ") {
			out = append(out, l)
		}
	}
	return out
}

// A startup or clear brief on either host, and the first prompt's brief,
// which is a startup brief, lists the constraints that hold for the
// project, those of every project and its own, newest first, right after
// the standing rules; never a fact, nor another project's constraint. A
// compact brief lists none. The lines come from the recall index alone: a
// knowledge folder that can't be read changes nothing.
func TestBriefListsTheProjectsConstraints(t *testing.T) {
	const other = "github.com__team__other"
	rows := []string{
		indexRow("k4", "constraint", "project", "github.com__team__resume", "2026-10-28", "No mocks", "Tests never mock the database"),
		indexRow("k3", "fact", "project", "github.com__team__resume", "2026-10-28", "Inbox format", "The inbox stores JSONL"),
		indexRow("k5", "constraint", "project", other, "2026-10-28", "Their rule", "Only for the other project"),
		indexRow("k2", "constraint", "all", other, "2026-10-26", "Hooks stay offline", "Hooks never make a network call"),
	}
	want := []string{
		"Constraint: No mocks: Tests never mock the database: " + usualBin + " show knowledge/k4",
		"Constraint: Hooks stay offline: Hooks never make a network call: " + usualBin + " show knowledge/k2",
	}
	for _, host := range []string{"claude-code", "codex"} {
		for _, variant := range []string{"startup", "clear", "compact", "first"} {
			t.Run(host+"/"+variant, func(t *testing.T) {
				o := briefFixture(t)
				o.Host, o.Variant = host, variant
				switch variant {
				case "compact":
					o.Session = "compact-chat"
					capture(t, o, host, o.Session, "main", o.Now.Add(-20*time.Minute), "Compact this chat.")
				case "first":
					o.Variant, o.Project.Key, o.Project.Slug, o.Project.Remote = "startup", other, "other", "github.com/team/other"
				}
				writeRules(t, o, "general/rules", true, "Always run go vet before committing")
				writeIndex(t, o, rows...)
				folder := filepath.Join(o.Config.WikiPath, "knowledge")
				if err := os.Mkdir(folder, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(folder, 0o700) })
				r, err := Build(context.Background(), o)
				if err != nil {
					t.Fatal(err)
				}
				got := constraintLines(r.Text)
				switch variant {
				case "compact":
					if len(got) != 0 {
						t.Fatalf("a compact brief listed %q", got)
					}
					return
				case "first":
					// The other project's own constraint, then the one of all projects.
					w := []string{"Constraint: Their rule: Only for the other project: " + usualBin + " show knowledge/k5", want[1]}
					if !slices.Equal(got, w) {
						t.Fatalf("got %q in:\n%s", got, r.Text)
					}
					return
				}
				rules := rulesHead + "\nFor all projects:\n- Always run go vet before committing [stated] s:0000aaaa#1"
				if !slices.Equal(got, want) || !strings.Contains(r.Text, rules+"\n\n"+strings.Join(want, "\n")+"\n\n") {
					t.Fatalf("got %q in:\n%s", got, r.Text)
				}
				cap, _, _ := budgets(o.Host, o.Variant, false)
				if runes(r.Text) > cap {
					t.Fatalf("%d runes over the cap of %d", runes(r.Text), cap)
				}
			})
		}
	}
}

// A new chat in a project with no earlier chat still gets its constraints,
// with the footer that says how to search other projects.
func TestFirstChatGetsConstraints(t *testing.T) {
	o := fixture(t)
	o.Dirs.Share = usualShare
	pageWiki(t, o)
	writeIndex(t, o, indexRow("k1", "constraint", "all", identity.GeneralKey, "2026-10-26", "Hooks stay offline", "Hooks never make a network call"))
	r, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	line := "Constraint: Hooks stay offline: Hooks never make a network call: " + usualBin + " show knowledge/k1"
	if !strings.Contains(r.Text, "eva-memory\n"+line+"\n\n"+clean(firstFooter(o))+"\n") || r.Found {
		t.Fatalf("brief:\n%s", r.Text)
	}
}

// At most 3 constraints are listed, in at most 600 characters together,
// each with its command whole: a long statement is cut, or left out, then
// the title is cut. A line that cleaning as wiki text would change is left
// out, and the next constraint takes its place. A missing or damaged index
// lists none.
func TestConstraintsBlockCaps(t *testing.T) {
	o := fixture(t)
	o.Dirs.Share = usualShare
	pageWiki(t, o)
	key := o.Project.Key
	long := strings.TrimSpace(strings.Repeat("Every release keeps the changelog current and signed ", 5))
	var rows []string
	for i := 9; i >= 1; i-- {
		rows = append(rows, indexRow(fmt.Sprintf("k%d", i), "constraint", "project", key, fmt.Sprintf("2026-10-%02d", 10+i), fmt.Sprintf("Release rule %d", i), long))
	}
	writeIndex(t, o, rows...)
	text := constraintsBlock(context.Background(), o)
	lines := strings.Split(text, "\n")
	if len(lines) != 3 || runes(text) > constraintsCap {
		t.Fatalf("%d lines, %d runes:\n%s", len(lines), runes(text), text)
	}
	for i, l := range lines {
		id := 9 - i
		if !strings.HasPrefix(l, fmt.Sprintf("Constraint: Release rule %d: Every release ", id)) || !strings.HasSuffix(l, fmt.Sprintf("…: %s show knowledge/k%d", usualBin, id)) {
			t.Errorf("line %d: %q", i, l)
		}
	}
	// One constraint alone may take the whole room.
	writeIndex(t, o, rows[0])
	if text := constraintsBlock(context.Background(), o); runes(text) > constraintsCap || strings.Contains(text, "…") || !strings.HasSuffix(text, " show knowledge/k9") {
		t.Fatalf("one constraint:\n%s", text)
	}
	for _, c := range []struct {
		name  string
		limit int
		want  string
	}{
		{"cut", 80, "Constraint: Release rule 9: Every release keeps th…: /b/eva-md show knowledge/k9"},
		{"no statement", 60, "Constraint: Release rule 9: /b/eva-md show knowledge/k9"},
		{"title cut", 45, "Constraint: Rel…: /b/eva-md show knowledge/k9"},
		{"no room", 30, ""},
	} {
		r := knowledgepage.ParseIndex([]byte("# eva-md knowledge index 1\n" + rows[0] + "\n"))[0]
		if got := constraint(r, "/b/eva-md", c.limit); got != c.want || runes(got) > c.limit {
			t.Errorf("%s: %q", c.name, got)
		}
	}
	// A secret in the newest constraint leaves its line out.
	secret := "ghp_" + strings.Repeat("ab12", 10)
	writeIndex(t, o, indexRow("k12", "constraint", "project", key, "2026-10-30", "Token", "The CI token is "+secret), rows[0])
	if got := constraintsBlock(context.Background(), o); got != "Constraint: Release rule 9: "+long+": "+usualBin+" show knowledge/k9" {
		t.Fatalf("a secret:\n%s", got)
	}
	file := filepath.Join(o.Config.WikiPath, filepath.FromSlash(knowledgepage.IndexFile))
	for _, c := range []struct {
		name   string
		damage func() error
	}{
		{"header", func() error { return os.WriteFile(file, []byte(rows[0]+"\n"), 0o600) }},
		{"linked", func() error { return os.Link(file, file+".2") }},
		{"missing", func() error { os.Remove(file + ".2"); return os.Remove(file) }},
	} {
		writeIndex(t, o, rows[0])
		if err := c.damage(); err != nil {
			t.Fatal(err)
		}
		if got := constraintsBlock(context.Background(), o); got != "" {
			t.Errorf("%s: %q", c.name, got)
		}
	}
}

// knowledgeWiki saves knowledge pages through knowledgepage, as the update
// does: a decision about the inbox, which cites code, superseded once; a
// constraint for every project; and a fact, then dropped.
func knowledgeWiki(t *testing.T, o Options) *okfadapter.Wiki {
	t.Helper()
	w := pageWiki(t, o)
	meta := knowledgepage.SaveMeta{Project: o.Project.Key, Commit: "4e2bf4c", Actor: "claude-code/claude-opus-5-5", Version: "test", Now: func() time.Time { return o.Now }}
	ops := []knowledgepage.Op{
		{Action: knowledgepage.Add, Kind: knowledgepage.Decision, Title: "Inbox format", Text: "The inbox stores entries as JSONL, one file per session", Tag: "stated",
			Scope: knowledgepage.ScopeProject, Refs: []string{"s:3f2a91c0#14"}, At: knowledgeAt, Paths: []string{"internal/inbox/"}},
		{Action: knowledgepage.Add, Kind: knowledgepage.Constraint, Title: "Hooks stay offline", Text: "Hooks never make a network call", Tag: "stated",
			Scope: knowledgepage.ScopeAll, Refs: []string{"s:3f2a91c0#15"}, At: knowledgeAt},
		{Action: knowledgepage.Add, Kind: knowledgepage.Fact, Title: "Quince", Text: "The quince job runs nightly", Tag: "observed",
			Scope: knowledgepage.ScopeProject, Refs: []string{"s:3f2a91c0#16"}, At: knowledgeAt},
	}
	if _, err := knowledgepage.Save(w, ops, meta); err != nil {
		t.Fatal(err)
	}
	later := []knowledgepage.Op{
		{Action: knowledgepage.Supersede, ID: 1, Kind: knowledgepage.Decision, Text: "The inbox stores entries as JSONL, one file per session and day", Tag: "stated",
			Refs: []string{"s:4b5c6d7e#3"}, At: laterAt},
		{Action: knowledgepage.Drop, ID: 3, Reason: "the quince job was removed", Tag: "stated", Refs: []string{"s:4b5c6d7e#4"}, At: laterAt},
	}
	if _, err := knowledgepage.Save(w, later, meta); err != nil {
		t.Fatal(err)
	}
	return w
}

// The labels say what [observed] rests on wherever a tag can show: the
// brief's, with the Now's other tags and what its constraint lines rest on,
// and search's, on page lines and on knowledge lines, whose observed facts
// and procedures rest on a command, a file or an MCP tool alone.
func TestLabelsSayWhatObservedRestsOn(t *testing.T) {
	t.Run("brief", func(t *testing.T) {
		o := briefFixture(t)
		knowledgeWiki(t, o)
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Now lines tagged [stated] or [accepted] rest on the user's words, [observed] ones on a tool result the assistant reported, [suggested] ones on the assistant's and [inferred] ones on neither.",
			" Constraint lines rest on the user's words. ", "\nConstraint: Hooks stay offline: "} {
			if !strings.Contains(r.Text, want) {
				t.Errorf("the brief lacks %q:\n%s", want, r.Text)
			}
		}
	})
	t.Run("search", func(t *testing.T) {
		o := fixture(t)
		knowledgeWiki(t, o)
		out := search(t, withPages(o, "quince"))
		for _, want := range []string{"On page lines, [stated] and [accepted] rest on the user's words, [observed] on a tool result the assistant reported, [suggested] on the assistant's and [inferred] on neither;",
			"decision and constraint lines rest on the user's words, and fact and procedure lines on them too or, tagged [observed], on a command, file or MCP result the assistant reported.",
			"\n[retired] 2026-10-27 · knowledge/k3 · fact [observed]: Quince: "} {
			if !strings.Contains(out, want) {
				t.Errorf("search lacks %q:\n%s", want, out)
			}
		}
	})
}

// A brief lists the constraint the writer saved, through the recall index
// it built.
func TestBriefListsWrittenConstraints(t *testing.T) {
	o := briefFixture(t)
	knowledgeWiki(t, o)
	r, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := constraintLines(r.Text); len(got) != 1 || got[0] != "Constraint: Hooks stay offline: Hooks never make a network call: "+usualBin+" show knowledge/k2" {
		t.Fatalf("constraints %q in:\n%s", got, r.Text)
	}
}

// Search finds a knowledge page's statement, "<kind> [<tag>]: <title>:
// <statement>", at the page's ref with the tag [knowledge], or [retired] for
// a retired page, dated by its as_of, which the window does not test; and
// the code it cites, which a path finds as a system page's code line. The
// label says what a knowledge line is only when one is shown, and the
// trailer counts the knowledge pages. Knowledge pages are global: every
// scope and --pages search them, --inbox leaves them out, and with --all a
// hit shows the knowledge folder.
func TestSearchFindsKnowledgePages(t *testing.T) {
	o := fixture(t)
	knowledgeWiki(t, o)
	const (
		inbox   = "[knowledge] 2026-10-28 · knowledge/k1 · decision [stated]: Inbox format: The inbox stores entries as JSONL, one file per session and day"
		offline = "[knowledge] 2026-10-27 · knowledge/k2 · constraint [stated]: Hooks stay offline: Hooks never make a network call"
		quince  = "[retired] 2026-10-27 · knowledge/k3 · fact [observed]: Quince: The quince job runs nightly"
		code    = "[knowledge] 2026-10-28 · knowledge/k1 · code: github.com__team__resume@4e2bf4c:internal/inbox"
	)
	for _, c := range []struct {
		terms []string
		want  []string
	}{
		{[]string{"JSONL", "session"}, []string{inbox}},
		{[]string{"network", "call"}, []string{offline}},
		{[]string{"quince"}, []string{quince}},
		{[]string{"internal/inbox/store.go"}, []string{code}},
		{[]string{"./internal/inbox/"}, []string{code}},
		{[]string{"internal/inbox"}, []string{code}},
	} {
		so := withPages(o, c.terms...)
		so.Days = 1 // the window bounds chats and thread pages only
		out := search(t, so)
		if got := hits(t, out); !slices.Equal(got, c.want) {
			t.Fatalf("%q:\n%s", c.terms, out)
		}
		if !strings.Contains(out, " "+knowledgeFacts) || !strings.HasPrefix(out, "eva.md search: earlier chat text and thread and knowledge page lines matching ") {
			t.Fatalf("%q: label:\n%s", c.terms, out)
		}
		if !strings.Contains(out, "\nScanned 0 files (0.0 MB) in 0 projects, 0 thread pages and 3 knowledge pages (") {
			t.Fatalf("%q: trailer:\n%s", c.terms, out)
		}
	}
	// A miss in the knowledge pages leaves the label's knowledge sentence out.
	say(t, o, o.Project.Key, "topic-chat", o.Now.Add(-time.Minute), "user", "prompt", "Which walnut is first?")
	if out := search(t, withPages(o, "walnut")); strings.Contains(out, knowledgeFacts) || len(hits(t, out)) != 1 {
		t.Fatalf("no knowledge hit:\n%s", out)
	}
	general := o
	general.Project = identity.General()
	if got := hits(t, search(t, withPages(general, "network", "call"))); !slices.Equal(got, []string{offline}) {
		t.Fatalf("general:\n%s", strings.Join(got, "\n"))
	}
	every := withPages(o, "network", "call")
	every.All = true
	if got := hits(t, search(t, every)); !slices.Equal(got, []string{strings.Replace(offline, " · knowledge/", " · knowledge · knowledge/", 1)}) {
		t.Fatalf("--all:\n%s", strings.Join(got, "\n"))
	}
	pagesOnly := withPages(o, "network", "call")
	pagesOnly.Pages = true
	if out := search(t, pagesOnly); !slices.Equal(hits(t, out), []string{offline}) || !strings.Contains(out, "\nScanned 0 thread pages and 3 knowledge pages (") {
		t.Fatalf("--pages:\n%s", out)
	}
	inboxOnly := withPages(o, "network", "call")
	inboxOnly.Inbox = true
	if out := search(t, inboxOnly); !strings.HasPrefix(out, "No matches.") || strings.Contains(out, "knowledge") {
		t.Fatalf("--inbox:\n%s", out)
	}
	// A prefix of the cited folder's name is no path in it. In another
	// project a relative path names no repo the page cites, not even the
	// cited folder itself, and with --all it lies in the cited folder.
	if out := search(t, withPages(o, "internal/inb")); !strings.HasPrefix(out, "No matches.") {
		t.Fatalf("a prefix:\n%s", out)
	}
	other := o
	other.Project = identity.Project{Kind: identity.KindRepo, Key: "github.com__team__other", Slug: "other", Remote: "github.com/team/other", Branch: "main"}
	for _, path := range []string{"internal/inbox/store.go", "internal/inbox", "internal/inb"} {
		if out := search(t, withPages(other, path)); !strings.HasPrefix(out, "No matches.") {
			t.Fatalf("another project, %s:\n%s", path, out)
		}
	}
	for _, path := range []string{"internal/inbox/store.go", "internal/inbox"} {
		every := withPages(other, path)
		every.All = true
		if got := hits(t, search(t, every)); !slices.Equal(got, []string{strings.Replace(code, " · knowledge/", " · knowledge · knowledge/", 1)}) {
			t.Fatalf("another project with --all, %s:\n%s", path, strings.Join(got, "\n"))
		}
	}
	if os.Geteuid() == 0 {
		return
	}
	file := filepath.Join(o.Config.WikiPath, "knowledge", "k2.md")
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(file, 0o644) })
	if out := search(t, withPages(o, "JSONL")); !strings.HasSuffix(out, "; 1 page unreadable: knowledge/k2.") || !strings.Contains(out, " and 2 knowledge pages (") {
		t.Fatalf("unreadable:\n%s", out)
	}
}

// The trailer lists the thread, system and knowledge pages it scanned, and
// the label names all three.
func TestSearchCountsEveryKindOfPage(t *testing.T) {
	o := fixture(t)
	so := systemsWiki(t, &o)
	writeSystems(t, so)
	knowledgeWiki(t, o)
	out := search(t, withPages(o, "network"))
	if !strings.HasPrefix(out, "eva.md search: earlier chat text and thread, system and knowledge page lines matching ") || !strings.Contains(out, "\nScanned 0 files (0.0 MB) in 0 projects, 0 thread pages, 2 system pages and 3 knowledge pages (") {
		t.Fatalf("out:\n%s", out)
	}
}

// eva-md show opens a knowledge page whole, as saved, its Statement and its
// History, and the knowledge map.
func TestShowKnowledgePages(t *testing.T) {
	o := fixture(t)
	knowledgeWiki(t, o)
	saved, err := os.ReadFile(filepath.Join(o.Config.WikiPath, "knowledge", "k1.md"))
	if err != nil {
		t.Fatal(err)
	}
	if text, err := Show(context.Background(), o.Dirs, o.Config, "knowledge/k1"); err != nil || text != string(saved) {
		t.Fatalf("show knowledge/k1 = %q, %v", text, err)
	}
	for ref, want := range map[string]string{
		"knowledge/k1#Statement": "## Statement\nThe inbox stores entries as JSONL, one file per session and day [stated] s:4b5c6d7e#3 <!-- t:2026-10-28T09:00:00Z -->",
		"knowledge/k1#History":   "## History\n- ~~The inbox stores entries as JSONL, one file per session~~ → The inbox stores entries as JSONL, one file per session and day (superseded 2026-10-28) [stated] s:4b5c6d7e#3\n\n" + knowledgepage.MapLink,
		"knowledge/k3#history":   "## History\n- ~~The quince job runs nightly~~ (retired 2026-10-28: the quince job was removed) [stated] s:4b5c6d7e#4\n\n" + knowledgepage.MapLink,
		"knowledge/k2#Statement": "## Statement\nHooks never make a network call [stated] s:3f2a91c0#15 <!-- t:2026-10-27T10:00:00Z -->\n\n" + knowledgepage.MapLink,
	} {
		if text, err := Show(context.Background(), o.Dirs, o.Config, ref); err != nil || text != want {
			t.Errorf("show %s = %q, %v", ref, text, err)
		}
	}
	if text, err := Show(context.Background(), o.Dirs, o.Config, "knowledge/map"); err != nil || !strings.Contains(text, "\n- [Hooks stay offline](k2.md): constraint · all projects · "+o.Project.Key+" · as of 2026-10-27\n") {
		t.Fatalf("show knowledge/map = %q, %v", text, err)
	}
}

// eva_status's reader counts the pages the recall index lists, the
// constraints that hold for the project and whether the map exists, and
// reads no page. A wiki it can't read or of another instance, and an index
// it can't read, are its problems; a missing index is none in a wiki with
// no knowledge yet, but one beside the knowledge pages or their map is.
func TestKnowledgeFor(t *testing.T) {
	o := fixture(t)
	if k := KnowledgeFor(context.Background(), o); k != (KnowledgeInfo{Problem: "the wiki is unreadable"}) {
		t.Fatalf("no wiki yet: %+v", k)
	}
	pageWiki(t, o)
	if k := KnowledgeFor(context.Background(), o); k != (KnowledgeInfo{}) {
		t.Fatalf("none yet: %+v", k)
	}
	knowledgeWiki(t, o)
	if k := KnowledgeFor(context.Background(), o); k != (KnowledgeInfo{Found: true, Pages: 2, Constraints: 1, Map: true}) {
		t.Fatalf("pages: %+v", k)
	}
	other := o
	other.Project.Key = "github.com__team__other"
	writeIndex(t, o, indexRow("k4", "constraint", "project", o.Project.Key, "2026-10-28", "No mocks", "Tests never mock the database"))
	if k := KnowledgeFor(context.Background(), other); k != (KnowledgeInfo{Found: true, Pages: 1, Constraints: 0, Map: true}) {
		t.Fatalf("another project: %+v", k)
	}
	instance := o
	instance.Config.Instance = "personal-instance"
	if k := KnowledgeFor(context.Background(), instance); k != (KnowledgeInfo{Problem: "the wiki is not the instance the config names"}) {
		t.Fatalf("another instance: %+v", k)
	}
	file := filepath.Join(o.Config.WikiPath, filepath.FromSlash(knowledgepage.IndexFile))
	if err := os.Link(file, file+".2"); err != nil {
		t.Fatal(err)
	}
	if k := KnowledgeFor(context.Background(), o); k.Problem != "the recall index is unreadable; eva-md doctor builds it again" || !k.Map {
		t.Fatalf("a linked index: %+v", k)
	}
	// Without the index, the knowledge folder or the map means it is gone.
	if err := os.Remove(file + ".2"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	const missing = "the recall index is missing; eva-md doctor builds it again"
	if k := KnowledgeFor(context.Background(), o); k != (KnowledgeInfo{Problem: missing, Map: true}) {
		t.Fatalf("a missing index beside the map: %+v", k)
	}
	if err := os.Remove(filepath.Join(o.Config.WikiPath, "knowledge", "map.md")); err != nil {
		t.Fatal(err)
	}
	if k := KnowledgeFor(context.Background(), o); k != (KnowledgeInfo{Problem: missing}) {
		t.Fatalf("a missing index beside the pages: %+v", k)
	}
	o.Config.WikiPath = ""
	if k := KnowledgeFor(context.Background(), o); k != (KnowledgeInfo{Problem: "no wiki is set up"}) {
		t.Fatalf("no wiki: %+v", k)
	}
}
