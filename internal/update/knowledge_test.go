package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// knownChat is a chat that settles some knowledge and changes some on file.
func knownChat() *chat {
	c := newChat("3f2a91c0-know", start)
	c.user("The inbox stays JSONL because grep must keep working on it, see internal/inbox/store.go. " + filler)
	c.assistant("Understood. I suggest we also cache parsed pages in memory. " + filler)
	c.tool("Bash go test ./internal/inbox/... (exit 0)")
	c.assistant("The tests pass: the inbox reader handles 64 KiB lines. " + filler)
	c.user("In all my projects, never push on Fridays.")
	c.user("Actually the inbox moves from JSONL to SQLite now; grep is no longer needed.")
	return c
}

// knownOnFile is the knowledge on file for knownChat: k3 and k4 share words
// with it, k4 is locked, and k5 shares none, so the input doesn't show it.
func knownOnFile() KnowledgeSet {
	return KnowledgeSet{Project: "github.com__you__eva-md", Head: "4e2bf4c", Rows: []knowledgepage.Row{
		{ID: "k3", Kind: "fact", Tag: "stated", Scope: "project", Project: "github.com__you__eva-md", AsOf: "2026-01-14", At: start.Add(3 * time.Minute),
			Title: "Inbox format", Statement: "The inbox format is JSONL"},
		{ID: "k4", Kind: "constraint", Tag: "stated", Scope: "project", Project: "github.com__you__eva-md", AsOf: "2026-01-13", ReadOnly: true,
			Title: "Inbox deploys", Statement: "Deploys of the inbox need a reviewer"},
		{ID: "k5", Kind: "fact", Tag: "observed", Scope: "all", Project: "general", AsOf: "2026-01-12", Title: "Gardens", Statement: "Gardens bloom quietly"},
	}}
}

func kAdd(kind, title, text, tag string, refs ...int) knowledgeOp {
	return knowledgeOp{Action: "add", Kind: kind, Title: ptr(title), Text: ptr(text), Tag: tag, Refs: refs, Scope: "project"}
}

func kSupersede(id, kind, text string, refs ...int) knowledgeOp {
	return knowledgeOp{ID: ptr(id), Action: "supersede", Kind: kind, Text: ptr(text), Tag: "stated", Refs: refs, Scope: "project"}
}

func kDrop(id, reason string, refs ...int) knowledgeOp {
	o := knowledgeOp{ID: ptr(id), Action: "drop", Kind: "fact", Tag: "stated", Refs: refs, Scope: "project"}
	if reason != "" {
		o.Reason = ptr(reason)
	}
	return o
}

// knownOps runs Validate's knowledge checks on ops.
func knownOps(t *testing.T, b Batch, ops ...knowledgeOp) (threadpage.Update, []knowledgepage.Op, Report) {
	t.Helper()
	u, k, r, err := checkOutput(encode(t, output{Knowledge: ops}), b, threadpage.Page{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return u, k, r
}

// The input shows the knowledge that shares words with the batch, so the
// model can name it, within 12 lines and 2,400 characters.
func TestBuildBoundsTheKnowledgeOnFile(t *testing.T) {
	isolate(t)
	b := build(t, knownChat(), threadpage.Page{}, Options{Knowledge: knownOnFile()})
	if !slices.Equal(b.Knowledge.Shown, []string{"k3", "k4"}) || b.Knowledge.Head != "4e2bf4c" || len(b.Knowledge.Rows) != 3 {
		t.Fatalf("knowledge %+v", b.Knowledge)
	}
	if !strings.Contains(string(b.Input), "Knowledge on file, by id (supersede or drop rather than repeat):\n- k3 fact for this project: The inbox format is JSONL\n"+
		"- k4 (read-only) constraint for this project: Deploys of the inbox need a reviewer\nLast Timeline lines") {
		t.Fatalf("input:\n%s", b.Input)
	}
	section := func(rows []knowledgepage.Row) (Batch, []string, int) {
		b := build(t, knownChat(), threadpage.Page{}, Options{Knowledge: KnowledgeSet{Project: "p", Rows: rows}})
		_, rest, _ := strings.Cut(string(b.Input), "Knowledge on file, by id (supersede or drop rather than repeat):\n")
		text, _, _ := strings.Cut(rest, "Last Timeline lines")
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		return b, lines, utf8.RuneCountInString(strings.ReplaceAll(text, "\n", ""))
	}
	rows := func(long func(i int) int) []knowledgepage.Row {
		var out []knowledgepage.Row
		for i := range 30 {
			out = append(out, knowledgepage.Row{ID: fmt.Sprintf("k%d", i+1), Kind: "fact", Scope: "project", Project: "p", AsOf: "2026-01-14",
				Statement: "The inbox reader " + strings.Repeat("x", long(i))})
		}
		return out
	}
	// Ties go to the newest first, 12 lines at most.
	b, lines, _ := section(rows(func(int) int { return 0 }))
	if len(lines) != knownShown || !slices.Equal(b.Knowledge.Shown[:2], []string{"k30", "k29"}) || slices.Contains(b.Knowledge.Shown, "k18") {
		t.Fatalf("%d lines, shown %v", len(lines), b.Knowledge.Shown)
	}
	// A line that doesn't fit the characters left is left out, and shorter
	// ones after it still go in.
	b, lines, chars := section(rows(func(i int) int { return i % 3 * 290 }))
	if chars > knownChars || len(lines) != len(b.Knowledge.Shown) || slices.Contains(b.Knowledge.Shown, "k24") || !slices.Contains(b.Knowledge.Shown, "k22") {
		t.Fatalf("%d characters, shown %v", chars, b.Knowledge.Shown)
	}
}

// Knowledge is promoted only from what the batch's entries settle: the
// user's words or agreement for a decision or a constraint, those or a tool
// result for a fact or a procedure, never a suggestion; never a statement
// on file, nor a near copy of one; never a token nothing grounds.
func TestValidateKnowledgeAdds(t *testing.T) {
	isolate(t)
	c := knownChat()
	b := build(t, c, threadpage.Page{}, Options{Knowledge: knownOnFile()})
	grep := kAdd("fact", "Inbox and grep", "Grep must keep working on the inbox", "stated", 1)
	for _, tc := range []struct {
		name string
		op   knowledgeOp
		want KnowledgeCounts
		tag  string
	}{
		{"a fact the user states", grep, KnowledgeCounts{Added: 1}, "stated"},
		{"a fact a tool result shows", kAdd("fact", "Inbox reader", "The inbox reader handles 64 KiB lines", "observed", 3, 4), KnowledgeCounts{Added: 1}, "observed"},
		{"a decision the assistant suggested", kAdd("decision", "Page cache", "Cache parsed pages in memory", "suggested", 2), KnowledgeCounts{Refused: 1}, ""},
		{"the assistant's suggestion as the user's", kAdd("decision", "Page cache", "Cache parsed pages in memory", "stated", 2), KnowledgeCounts{Refused: 1}, ""},
		{"the assistant's answer cited to the user's words", kAdd("decision", "Page cache", "Cache parsed pages in memory", "accepted", 1, 2), KnowledgeCounts{Refused: 1}, ""},
		{"a constraint a tool shows", kAdd("constraint", "Inbox tests", "The inbox tests must pass", "observed", 3), KnowledgeCounts{Refused: 1}, ""},
		{"an inference", kAdd("fact", "Inbox", "The inbox is fast", "inferred", 1), KnowledgeCounts{Refused: 1}, ""},
		{"no title", knowledgeOp{Action: "add", Kind: "fact", Text: ptr("Grep must keep working on the inbox"), Tag: "stated", Refs: []int{1}, Scope: "project"}, KnowledgeCounts{Refused: 1}, ""},
		{"a statement on file", kAdd("fact", "Inbox", "the inbox format is JSONL.", "stated", 1), KnowledgeCounts{Duplicates: 1}, ""},
		{"a near copy of one", kAdd("fact", "Inbox", "Inbox format: JSONL lines", "stated", 1), KnowledgeCounts{NearDuplicates: 1}, ""},
		{"a near copy of one not shown", kAdd("fact", "Gardens", "Gardens bloom quietly in the scheduler", "observed", 3), KnowledgeCounts{NearDuplicates: 1}, ""},
		{"a path nothing grounds", kAdd("fact", "Inbox store", "The inbox lives in internal/inbox/db.go", "stated", 1), KnowledgeCounts{Refused: 1}, ""},
	} {
		u, ops, r := knownOps(t, b, tc.op)
		if r.Knowledge != tc.want || len(ops) != tc.want.Added || len(u.Events) != 0 {
			t.Errorf("%s: %+v, ops %+v", tc.name, r.Knowledge, ops)
			continue
		}
		if tc.want.Added == 1 && (ops[0].Tag != tc.tag || ops[0].Action != "add" || ops[0].Scope != "project" || ops[0].At.IsZero()) {
			t.Errorf("%s: op %+v", tc.name, ops[0])
		}
	}
	// A kept add carries its refs, the newest cited entry's time and the
	// paths a cited entry writes, as it writes them; the rest are counted.
	g := grep
	g.Paths = []string{"./internal/inbox/store.go", "/etc/hosts", "internal/inbox/"}
	_, ops, r := knownOps(t, b, g)
	want := knowledgepage.Op{Action: "add", Kind: "fact", Title: "Inbox and grep", Text: "Grep must keep working on the inbox", Tag: "stated", Scope: "project",
		Refs: []string{"s:3f2a91c0#1"}, At: start.Add(time.Minute), Paths: []string{"internal/inbox/store.go", "internal/inbox"}}
	if len(ops) != 1 || !reflect.DeepEqual(ops[0], want) || r.Knowledge.PathsDropped != 1 {
		t.Fatalf("ops %+v, report %+v", ops, r.Knowledge)
	}
	g.Paths = []string{"internal/other.go", "../internal/inbox", "internal/inbox/store.go "}
	if _, ops, r := knownOps(t, b, g); len(ops) != 1 || !slices.Equal(ops[0].Paths, []string{"internal/inbox/store.go"}) || r.Knowledge.PathsDropped != 2 {
		t.Fatalf("ops %+v, report %+v", ops, r.Knowledge)
	}
	v := newValidator(b, threadpage.Page{})
	if got := v.knownPaths([]string{"internal/inbox/store.go", "internal/inbox/store.go", "internal/inbox", "internal", "internal/inbox/store"}, v.cite([]int{1})); !slices.Equal(got, []string{"internal/inbox/store.go", "internal/inbox", "internal"}) ||
		v.report.Knowledge.PathsDropped != 2 {
		t.Fatalf("paths %v, %d dropped", got, v.report.Knowledge.PathsDropped)
	}
	// A fragment of a path a cited entry writes is no path it writes.
	g.Paths = []string{"internal/in", "nal/inbox/st", "inbox/store.go"}
	if _, ops, r := knownOps(t, b, g); len(ops) != 1 || len(ops[0].Paths) != 0 || r.Knowledge.PathsDropped != 3 {
		t.Fatalf("fragments: ops %+v, report %+v", ops, r.Knowledge)
	}
	// A near copy holds most of the new statement's words, or every code
	// token of a statement on file, whole, with a word beside them; a short
	// statement the new one merely holds, or a path that shares only its
	// file name, is no near copy.
	reach := knownOnFile()
	reach.Rows = append(reach.Rows,
		knowledgepage.Row{ID: "k6", Kind: "fact", Tag: "stated", Scope: "project", Project: "github.com__you__eva-md", AsOf: "2026-01-13", Title: "Grep", Statement: "Inbox grep"},
		knowledgepage.Row{ID: "k7", Kind: "fact", Tag: "stated", Scope: "project", Project: "github.com__you__eva-md", AsOf: "2026-01-13", Title: "Reader", Statement: "The reader's entry point is cmd/reader/store.go"},
		knowledgepage.Row{ID: "k8", Kind: "fact", Tag: "stated", Scope: "project", Project: "github.com__you__eva-md", AsOf: "2026-01-13", Title: "Store", Statement: "Grep reads internal/inbox/store.go"})
	rb := build(t, c, threadpage.Page{}, Options{Knowledge: reach})
	for _, tc := range []struct {
		op   knowledgeOp
		want KnowledgeCounts
	}{
		{kAdd("fact", "Inbox and grep", "Grep must keep working on the inbox, see internal/inbox/store.go", "stated", 1), KnowledgeCounts{NearDuplicates: 1}},
		{kAdd("fact", "Inbox and grep", "The inbox stays JSONL because of internal/inbox/store.go", "stated", 1), KnowledgeCounts{Added: 1}},
	} {
		if _, ops, r := knownOps(t, rb, tc.op); r.Knowledge != tc.want || len(ops) != tc.want.Added {
			t.Errorf("%q: %+v", deref(tc.op.Text), r.Knowledge)
		}
	}
	reach.Rows = reach.Rows[:len(reach.Rows)-1]
	rb = build(t, c, threadpage.Page{}, Options{Knowledge: reach})
	if _, ops, r := knownOps(t, rb, kAdd("fact", "Inbox and grep", "Grep must keep working on the inbox, see internal/inbox/store.go", "stated", 1)); len(ops) != 1 || r.Knowledge != (KnowledgeCounts{Added: 1}) {
		t.Errorf("a statement that holds a short one, or a path's file name: %+v", r.Knowledge)
	}
	// A date no cited entry holds leaves the statement out, as any line.
	if _, ops, r := knownOps(t, b, kAdd("fact", "Inbox", "Grep stays on the inbox until 2026-03-01", "stated", 1)); len(ops) != 0 || r.Dates != 1 {
		t.Fatalf("a date: %+v %+v", ops, r)
	}
	// Two adds of one statement, or of near copies, keep the first.
	if _, ops, r := knownOps(t, b, grep, grep, kAdd("fact", "Grep", "Grep must keep working on the inbox files", "stated", 1)); len(ops) != 1 ||
		r.Knowledge != (KnowledgeCounts{Added: 1, Duplicates: 1, NearDuplicates: 1}) {
		t.Fatalf("repeated adds: %+v %+v", ops, r.Knowledge)
	}
	// At most 5 changes are kept a pass.
	var seven []knowledgeOp
	for _, w := range []string{"Scheduler logs alpha", "Retry loop bravo", "Numbers match charlie", "Monday notes delta", "Hour reading echo", "Plan settled foxtrot", "Chat long golf"} {
		seven = append(seven, kAdd("fact", w, w, "stated", 1))
	}
	if _, ops, r := knownOps(t, b, seven...); len(ops) != keepKnowledge || r.Knowledge != (KnowledgeCounts{Added: 5, OverCap: 2}) {
		t.Fatalf("over the cap: %d ops, %+v", len(ops), r.Knowledge)
	}
	// The out-of-order guard holds every change.
	old := build(t, c, threadpage.Page{}, Options{Knowledge: knownOnFile(), AsOf: c.at.Add(time.Hour)})
	if _, ops, r := knownOps(t, old, grep, kDrop("k3", "the user moved on", 6)); len(ops) != 0 || r.Knowledge != (KnowledgeCounts{Ignored: 2}) {
		t.Fatalf("older session: %+v %+v", ops, r.Knowledge)
	}
}

// A fact or a procedure is observed only on a command's, a file's or an MCP
// tool's result: a tool entry or a report of a turn that used a web fetch or
// web search, from the user's prompt to the next, supports no observed
// knowledge, cited alone or beside another turn's, while the user's own
// words in that turn still settle a fact.
func TestValidateKnowledgeOffTheWeb(t *testing.T) {
	isolate(t)
	c := newChat("3f2a91c0-webk", start)
	c.user("Check the deploy steps. " + filler)                               // 1
	c.tool("Bash make deploy-check (exit 0)")                                 // 2
	c.assistant("The check passes: deploys read the DEPLOY_TARGET variable.") // 3
	c.user("What do the docs say about the runner?")                          // 4
	c.tool("WebFetch https://docs.example.com/runner (exit 0)")               // 5: a run of a web fetch
	c.tool("Bash make runner-check (exit 0)")                                 // and a command
	c.assistant("The docs say the runner reads the RUNNER_POOL variable.")    // 6
	c.user("And the notes?")                                                  // 7
	c.tool("mcp__notes__read_page id=42 (exit 0)")                            // 8
	c.assistant("The note says builds read the BUILD_QUEUE variable.")        // 9
	known := KnowledgeSet{Project: "github.com__you__eva-md", Rows: []knowledgepage.Row{{ID: "k3", Kind: "fact", Tag: "stated", Scope: "project",
		Project: "github.com__you__eva-md", AsOf: "2026-01-13", At: start, Title: "Runner pool", Statement: "The runner pool is fixed"}}}
	b := build(t, c, threadpage.Page{}, Options{Knowledge: known})
	if !slices.Equal(b.Knowledge.Shown, []string{"k3"}) {
		t.Fatalf("shown %v", b.Knowledge.Shown)
	}
	runner := "The runner reads the RUNNER_POOL variable"
	observed := func(id, text string, refs ...int) knowledgeOp {
		op := kSupersede(id, "fact", text, refs...)
		op.Tag = "observed"
		return op
	}
	for _, tc := range []struct {
		name string
		op   knowledgeOp
		want KnowledgeCounts
	}{
		{"a command's result", kAdd("fact", "Deploy target", "Deploys read the DEPLOY_TARGET variable", "observed", 2, 3), KnowledgeCounts{Added: 1}},
		{"a web turn's run and report", kAdd("fact", "Runner pool", runner, "observed", 5, 6), KnowledgeCounts{Refused: 1}},
		{"a web turn's report", kAdd("fact", "Runner pool", runner, "observed", 6), KnowledgeCounts{Refused: 1}},
		{"a web turn's command", kAdd("fact", "Runner pool", runner, "observed", 5), KnowledgeCounts{Refused: 1}},
		{"a web turn's report beside a command's", kAdd("fact", "Deploy target", "Deploys read the DEPLOY_TARGET variable", "observed", 3, 6), KnowledgeCounts{Refused: 1}},
		{"a procedure of a web turn", kAdd("procedure", "Runner pool", "Set the RUNNER_POOL variable before a runner check", "observed", 5, 6), KnowledgeCounts{Refused: 1}},
		{"an MCP tool's result", kAdd("fact", "Build queue", "Builds read the BUILD_QUEUE variable", "observed", 8, 9), KnowledgeCounts{Added: 1}},
		{"the user's words in a web turn", kAdd("fact", "Runner docs", "The runner has docs", "stated", 4), KnowledgeCounts{Added: 1}},
		{"a supersede on a web turn", observed("k3", "The runner pool is set by the RUNNER_POOL variable", 4, 6), KnowledgeCounts{Refused: 1}},
		{"a supersede on an MCP tool's result", observed("k3", "The runner pool is set by the BUILD_QUEUE variable", 7, 9), KnowledgeCounts{Superseded: 1}},
	} {
		if _, ops, r := knownOps(t, b, tc.op); r.Knowledge != tc.want || len(ops) != tc.want.Added+tc.want.Superseded {
			t.Errorf("%s: %+v, ops %+v", tc.name, r.Knowledge, ops)
		}
	}
}

// A statement holds for all projects only when a cited prompt says so in
// the user's own words, or the chat is outside a project.
func TestKnowledgeScope(t *testing.T) {
	isolate(t)
	c := knownChat()
	b := build(t, c, threadpage.Page{}, Options{Knowledge: knownOnFile()})
	fridays := kAdd("constraint", "No Friday pushes", "Never push on Fridays", "stated", 5)
	fridays.Scope = "all"
	grep := kAdd("fact", "Inbox and grep", "Grep must keep working on the inbox", "stated", 1)
	grep.Scope = "all"
	if _, ops, _ := knownOps(t, b, fridays, grep); len(ops) != 2 || ops[0].Scope != "all" || ops[1].Scope != "project" {
		t.Fatalf("ops %+v", ops)
	}
	// The prompt that names all projects must be the one that states it.
	elsewhere := grep
	elsewhere.Refs = []int{1, 5}
	if _, ops, _ := knownOps(t, b, elsewhere); len(ops) != 1 || ops[0].Scope != "project" {
		t.Fatalf("all projects named by another prompt: %+v", ops)
	}
	outside := knownOnFile()
	b = build(t, c, threadpage.Page{}, Options{Rules: RuleSet{Outside: true}, Knowledge: outside})
	if _, ops, _ := knownOps(t, b, grep); len(ops) != 1 || ops[0].Scope != "all" {
		t.Fatalf("outside a project: %+v", ops)
	}
}

// A supersede of an entry for all projects keeps all, unless the user's own
// words narrow it to this project, which then takes the page.
func TestKnowledgeScopeOfASupersede(t *testing.T) {
	isolate(t)
	c := newChat("3f2a91c0-scop", start)
	c.user("From now on, never push on Fridays after noon; Friday mornings are fine. " + filler)
	c.assistant("Understood. " + filler)
	c.user("Never push on Fridays after noon, just in this repo.")
	c.user("Never push on Fridays after noon, but not in all my projects.")
	c.user("In all my projects, never push on Fridays after noon.")
	c.assistant("Noted. " + filler)
	known := KnowledgeSet{Project: "github.com__you__eva-md", Rows: []knowledgepage.Row{{ID: "k5", Kind: "constraint", Tag: "stated", Scope: "all",
		Project: "general", AsOf: "2026-01-13", At: start, Title: "No Friday pushes", Statement: "Never push on Fridays"}}}
	b := build(t, c, threadpage.Page{}, Options{Knowledge: known})
	for _, tc := range []struct {
		ref          int
		want         string
		scope, moved string
	}{
		{1, "project", "all", ""},
		{1, "all", "all", ""},
		{3, "project", "project", "github.com__you__eva-md"},
		{4, "all", "project", "github.com__you__eva-md"},
		{5, "project", "all", ""},
	} {
		op := kSupersede("k5", "constraint", "Never push on Fridays after noon", tc.ref)
		op.Scope = tc.want
		_, ops, r := knownOps(t, b, op)
		if len(ops) != 1 || ops[0].Scope != tc.scope || ops[0].Project != tc.moved {
			t.Errorf("prompt %d, scope %s: ops %+v, %+v", tc.ref, tc.want, ops, r.Knowledge)
		}
	}
	outside := build(t, c, threadpage.Page{}, Options{Rules: RuleSet{Outside: true}, Knowledge: known})
	if _, ops, _ := knownOps(t, outside, kSupersede("k5", "constraint", "Never push on Fridays after noon", 3)); len(ops) != 1 || ops[0].Scope != "all" || ops[0].Project != "" {
		t.Fatalf("outside a project: %+v", ops)
	}
}

// A supersede or a drop names an entry the input showed, needs the user's
// prompt at or after the entry's time, and leaves a Timeline line; a page a
// person locked or verified is never changed.
func TestValidateKnowledgeChanges(t *testing.T) {
	isolate(t)
	b := build(t, knownChat(), threadpage.Page{}, Options{Knowledge: knownOnFile()})
	sqlite := kSupersede("k3", "fact", "The inbox moves to SQLite", 6)
	for _, tc := range []struct {
		name  string
		ops   []knowledgeOp
		want  KnowledgeCounts
		trail string
	}{
		{"a supersede on the user's newer words", []knowledgeOp{sqlite}, KnowledgeCounts{Superseded: 1}, "knowledge k3: ~~The inbox format is JSONL~~ → The inbox moves to SQLite"},
		{"a drop on them", []knowledgeOp{kDrop("k3", "the inbox moves to SQLite", 6)}, KnowledgeCounts{Dropped: 1}, "knowledge k3: ~~The inbox format is JSONL~~ (dropped: the inbox moves to SQLite)"},
		{"a supersede on older words", []knowledgeOp{kSupersede("k3", "fact", "The inbox stays JSONL for grep", 1)}, KnowledgeCounts{Stale: 1}, ""},
		{"a supersede on the assistant's words", []knowledgeOp{kSupersede("k3", "fact", "The inbox reader handles 64 KiB lines", 4)}, KnowledgeCounts{Stale: 1}, ""},
		{"a restatement", []knowledgeOp{kSupersede("k3", "fact", "The inbox format is JSONL.", 6)}, KnowledgeCounts{Refused: 1}, ""},
		{"a decision without the user's tag", []knowledgeOp{{ID: ptr("k3"), Action: "supersede", Kind: "decision", Text: ptr("The inbox moves to SQLite"), Tag: "observed", Refs: []int{6}, Scope: "project"}}, KnowledgeCounts{Refused: 1}, ""},
		{"a drop without a reason", []knowledgeOp{kDrop("k3", "", 6)}, KnowledgeCounts{Refused: 1}, ""},
		{"a locked page", []knowledgeOp{kSupersede("k4", "constraint", "Deploys of the inbox need two reviewers", 6)}, KnowledgeCounts{ReadOnly: 1}, ""},
		{"an id the input didn't show", []knowledgeOp{kDrop("k5", "the user moved on", 6)}, KnowledgeCounts{Ignored: 1}, ""},
		{"an id not on file", []knowledgeOp{kDrop("k9", "the user moved on", 6)}, KnowledgeCounts{Ignored: 1}, ""},
		{"two changes to one id", []knowledgeOp{sqlite, kDrop("k3", "the user moved on", 6)}, KnowledgeCounts{Superseded: 1, Ignored: 1}, "knowledge k3: ~~The inbox format is JSONL~~ → The inbox moves to SQLite"},
		{"the assistant's words with the user's newer prompt", []knowledgeOp{kSupersede("k3", "decision", "The inbox caches parsed pages in memory", 2, 6)}, KnowledgeCounts{Refused: 1}, ""},
		{"older words with a newer prompt about something else", []knowledgeOp{kSupersede("k3", "fact", "The inbox stays JSONL for grep", 1, 5)}, KnowledgeCounts{Refused: 1}, ""},
		{"a drop on a newer prompt about something else", []knowledgeOp{kDrop("k3", "the inbox format changed", 5)}, KnowledgeCounts{Refused: 1}, ""},
		{"a drop on older words", []knowledgeOp{kDrop("k3", "the inbox stays JSONL", 1)}, KnowledgeCounts{Stale: 1}, ""},
		{"a drop whose reason has a date no entry writes", []knowledgeOp{kDrop("k3", "the inbox moved on 2026-03-01", 6)}, KnowledgeCounts{}, ""},
	} {
		u, ops, r := knownOps(t, b, tc.ops...)
		if r.Knowledge != tc.want || len(ops) != tc.want.Superseded+tc.want.Dropped || r.Knowledge == (KnowledgeCounts{}) && r.Dates != 1 {
			t.Errorf("%s: %+v, %d dates, ops %+v", tc.name, r.Knowledge, r.Dates, ops)
			continue
		}
		var trail []threadpage.Event
		if tc.trail != "" {
			trail = []threadpage.Event{{Kind: "change", Text: tc.trail, Tag: "stated", Refs: []string{"s:3f2a91c0#6"}}}
			if ops[0].ID != 3 || ops[0].At != start.Add(6*time.Minute) || !slices.Equal(ops[0].Refs, []string{"s:3f2a91c0#6"}) {
				t.Errorf("%s: op %+v", tc.name, ops[0])
			}
		}
		if !reflect.DeepEqual(u.Events, trail) {
			t.Errorf("%s: Timeline %+v", tc.name, u.Events)
		}
	}
	// A supersede frees its old statement and takes the new one: an add of
	// the new one is then a duplicate.
	add := kAdd("fact", "Inbox store", "The inbox moves to SQLite", "stated", 6)
	if _, ops, r := knownOps(t, b, add, sqlite); len(ops) != 1 || ops[0].Action != "supersede" || r.Knowledge != (KnowledgeCounts{Superseded: 1, Duplicates: 1}) {
		t.Fatalf("an add after the supersede: %+v %+v", ops, r.Knowledge)
	}
	// A supersede to a statement another entry holds is a duplicate, and one
	// for all projects without the user's saying so stays the project's.
	known := knownOnFile()
	known.Rows = append(known.Rows, knowledgepage.Row{ID: "k6", Kind: "fact", Tag: "stated", Scope: "project", Project: "github.com__you__eva-md", AsOf: "2026-01-13",
		Title: "Inbox store", Statement: "The inbox moves from JSONL to SQLite"})
	if _, ops, r := knownOps(t, build(t, knownChat(), threadpage.Page{}, Options{Knowledge: known}), kSupersede("k3", "fact", "The inbox moves from JSONL to SQLite", 6)); len(ops) != 0 ||
		r.Knowledge != (KnowledgeCounts{Duplicates: 1}) {
		t.Fatalf("a supersede to another entry's statement: %+v %+v", ops, r.Knowledge)
	}
	all := sqlite
	all.Scope = "all"
	if _, ops, _ := knownOps(t, b, all); len(ops) != 1 || ops[0].Scope != "project" || ops[0].Project != "" {
		t.Fatalf("a supersede for all projects: %+v", ops)
	}
}

// Another project's own page is shown read-only, never changed, and never
// a duplicate here: the same statement may hold in this project too.
func TestKnowledgeOfAnotherProject(t *testing.T) {
	isolate(t)
	known := knownOnFile()
	known.Rows = append(known.Rows, knowledgepage.Row{ID: "k7", Kind: "fact", Tag: "stated", Scope: "project", Project: "github.com__other__billing", AsOf: "2026-01-12",
		Title: "Inbox and grep", Statement: "Grep must keep working on the inbox"})
	b := build(t, knownChat(), threadpage.Page{}, Options{Knowledge: known})
	if !slices.Contains(b.Knowledge.Shown, "k7") || !strings.Contains(string(b.Input), "\n- k7 (read-only) fact from github.com__other__billing: Grep must keep working on the inbox\n") {
		t.Fatalf("shown %v, input:\n%s", b.Knowledge.Shown, b.Input)
	}
	for _, op := range []knowledgeOp{kSupersede("k7", "fact", "The inbox moves from JSONL to SQLite", 6), kDrop("k7", "the inbox moves from JSONL to SQLite, grep is no longer needed", 6)} {
		if u, ops, r := knownOps(t, b, op); len(ops) != 0 || len(u.Events) != 0 || r.Knowledge != (KnowledgeCounts{ReadOnly: 1}) {
			t.Errorf("a %s of another project's page: %+v %+v", op.Action, ops, r.Knowledge)
		}
	}
	if _, ops, r := knownOps(t, b, kAdd("fact", "Inbox and grep", "Grep must keep working on the inbox", "stated", 1)); len(ops) != 1 || r.Knowledge != (KnowledgeCounts{Added: 1}) {
		t.Fatalf("an add of another project's statement: %+v %+v", ops, r.Knowledge)
	}
}

// The knowledge checks are pure and deterministic, as the rest of
// Validate is, however many statements are on file.
func TestValidateKnowledgeIsDeterministic(t *testing.T) {
	isolate(t)
	known := knownOnFile()
	for i := range 200 {
		known.Rows = append(known.Rows, knowledgepage.Row{ID: fmt.Sprintf("k%d", 100+i), Kind: "fact", Tag: "stated", Scope: "project", Project: "p",
			AsOf: "2026-01-01", Title: "Filler", Statement: fmt.Sprintf("Statement number %d about nothing", i)})
	}
	b := build(t, knownChat(), threadpage.Page{}, Options{Knowledge: known})
	out := encode(t, output{Knowledge: []knowledgeOp{kAdd("fact", "Inbox and grep", "Grep must keep working on the inbox", "stated", 1),
		kSupersede("k3", "fact", "The inbox moves to SQLite", 6), kAdd("fact", "Inbox", "Inbox format: JSONL lines", "stated", 1), kDrop("k4", "gone", 6)}})
	u1, k1, r1, err1 := checkOutput(out, b, threadpage.Page{})
	u2, k2, r2, err2 := checkOutput(out, b, threadpage.Page{})
	if err1 != nil || err2 != nil || !reflect.DeepEqual(u1, u2) || !reflect.DeepEqual(k1, k2) || r1 != r2 || len(k1) != 3 || r1.Knowledge.ReadOnly != 1 {
		t.Fatalf("two runs differ, or kept %d: %v %v %+v", len(k1), err1, err2, r1.Knowledge)
	}
}

// What the knowledge on a page says must pass the daily commit's secret
// scan as it will be saved, so ordinary statements pass it.
func TestKnowledgeSecretScanPassesPlainText(t *testing.T) {
	isolate(t)
	b := build(t, knownChat(), threadpage.Page{}, Options{Knowledge: knownOnFile()})
	v := newValidator(b, threadpage.Page{})
	op := knowledgepage.Op{Action: "add", Kind: "fact", Title: "local-token-refresh-worker-c9a8c7", Text: "Grep must keep working on the inbox, see internal/inbox/store.go",
		Tag: "stated", Scope: "project", Refs: []string{"s:3f2a91c0#1"}, At: start, Paths: []string{"internal/inbox/store.go"}}
	if v.flagged(op, knowledgepage.Entry{}, "") {
		t.Fatal("plain text flagged")
	}
}

// A change is refused when the daily commit's secret scan would flag any
// line the save writes for it, though each text alone passes: the recall
// index row, the folder index line, the History line and the Timeline
// line. Every file the save writes for a kept change passes the scan.
func TestKnowledgeSecretScan(t *testing.T) {
	isolate(t)
	c := newChat("3f2a91c0-scan", start)
	c.user("The deploy token rotates monthly. Separately, the CI config lives at kv/ci/Xk9fQ2mLp8zR4vW7bN3cQ1. " + filler)
	c.assistant("Understood. " + filler)
	c.user("The staging bucket is now called Zx8Kq2Lm9Wv4Rt7Yp3Nb6Hc1.")
	c.user("The deploy job reads the CI config from kv/ci/Xk9fQ2mLp8zR4vW7bN3cQ1.")
	known := KnowledgeSet{Project: "github.com__you__eva-md", Head: "4e2bf4c", Rows: []knowledgepage.Row{{ID: "k1", Kind: "fact", Tag: "stated", Scope: "project",
		Project: "github.com__you__eva-md", AsOf: "2026-01-13", At: start, Title: "Staging bucket", Statement: "CI reads the staging bucket name from the deploy token"},
		// Cut to 120 characters on the Timeline line, its trigger word ends
		// up beside the new text there, and only there.
		{ID: "k2", Kind: "fact", Tag: "stated", Scope: "project", Project: "github.com__you__eva-md", AsOf: "2026-01-13", At: start, Title: "Cache bucket",
			Statement: "The staging bucket name the nightly release job uses for its shared build cache always comes from the deploy token as read by the nightly job every morning"}}}
	b := build(t, c, threadpage.Page{}, Options{Knowledge: known})
	config := kAdd("fact", "Deploy config", "The deploy token rotates monthly", "stated", 1)
	config.Paths = []string{"kv/ci/Xk9fQ2mLp8zR4vW7bN3cQ1"}
	for _, tc := range []struct {
		name string
		op   knowledgeOp
	}{
		{"a statement beside a path in the recall index", config},
		{"a title beside a statement in the folder's index", kAdd("fact", "Signing token", "Lives at kv/ci/Xk9fQ2mLp8zR4vW7bN3cQ1", "stated", 1)},
		{"an old statement beside the new one in the History", kSupersede("k1", "fact", "Zx8Kq2Lm9Wv4Rt7Yp3Nb6Hc1 is the staging bucket name", 3)},
		{"an old statement beside the new one on the Timeline", kSupersede("k2", "fact", "Zx8Kq2Lm9Wv4Rt7Yp3Nb6Hc1 is the staging bucket name", 3)},
	} {
		if u, ops, r := knownOps(t, b, tc.op); len(ops) != 0 || len(u.Events) != 0 || r.Knowledge != (KnowledgeCounts{Refused: 1}) {
			t.Errorf("%s: %+v, ops %+v", tc.name, r.Knowledge, ops)
		}
	}
	plain := kAdd("fact", "Deploy job", "The deploy job reads the CI config", "stated", 4)
	plain.Paths = []string{"kv/ci/Xk9fQ2mLp8zR4vW7bN3cQ1"}
	u, ops, r := knownOps(t, b, plain, kSupersede("k1", "fact", "The staging bucket has a new name", 3))
	if len(ops) != 2 || r.Knowledge != (KnowledgeCounts{Added: 1, Superseded: 1}) || !slices.Equal(ops[1].Paths, plain.Paths) {
		t.Fatalf("kept: %+v, ops %+v", r.Knowledge, ops)
	}
	w := knownWiki(t)
	if _, err := knowledgepage.Save(w, []knowledgepage.Op{{Action: "add", Kind: "fact", Title: "Staging bucket", Text: "CI reads the staging bucket name from the deploy token",
		Tag: "stated", Scope: "project", Refs: []string{"s:3f2a91c0#1"}, At: start}}, knowledgepage.SaveMeta{Project: known.Project, Commit: known.Head, Version: "test"}); err != nil {
		t.Fatal(err)
	}
	res, err := knowledgepage.Save(w, ops, knowledgepage.SaveMeta{Project: known.Project, Commit: known.Head, Actor: "claude-code/claude-opus-5-5", Version: "test"})
	if err != nil || len(res.Skipped) != 0 {
		t.Fatalf("Save: %+v, %v", res, err)
	}
	for _, f := range append(res.Files, "log.md") {
		data, err := os.ReadFile(filepath.Join(w.Root(), filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		if lines := wikigit.SecretLines(string(data)); lines != nil {
			t.Errorf("the commit's scan flags %s at %v", f, lines)
		}
	}
	page, _ := threadpage.Apply(threadpage.Page{}, u)
	if lines := wikigit.SecretLines(threadpage.Render(page)); lines != nil || !strings.Contains(threadpage.Render(page), "knowledge k1: ~~") {
		t.Errorf("the commit's scan flags the thread page at %v", lines)
	}
}

// A pass saves its knowledge changes before the rules and thread pages:
// the page, the map, the folder's index, the recall index and the root
// line; a rerun after a failed thread save writes no second page; a
// supersede leaves its Timeline line; a page a person locked is left as it
// is, with a log line, and the pass goes on.
func TestPassWritesTheKnowledgePages(t *testing.T) {
	e := newEnv(t)
	known := []knowledgeOp{kAdd("decision", "Inbox format", "The inbox stays JSONL so grep keeps working", "stated", 1)}
	known[0].Paths = []string{"internal/inbox/store.go"}
	e.fake.Respond = func(r Request) (Result, error) {
		o := keeps(r)
		o.Summary, o.LastChat, o.Knowledge = "The inbox work goes on.", "Talked about the inbox.", known
		return Result{Output: encode(t, o), Attempts: 1}, nil
	}
	c := newChat("3f2a91c0-kpas", start)
	c.user("The inbox stays JSONL so grep keeps working, see internal/inbox/store.go. " + filler)
	c.assistant("Understood. " + filler)
	file := e.write(c)
	// The thread page's folder can't be written: the knowledge page is
	// saved, the pass fails and its batch waits.
	threads := filepath.Join(e.cfg.WikiPath, "projects", "eva-md", "threads")
	if err := os.MkdirAll(threads, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(threads, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(threads, 0o700) })
	if s := e.run(c); outcomes(s) != "failed (wiki_error)" || e.mark(file).Offset != 0 {
		t.Fatalf("outcomes %q %+v\n%s", outcomes(s), s.Records[0].Validation, e.log)
	}
	pg, err := e.wiki().Read("knowledge/k1")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := knowledgepage.Read(pg.Concept)
	if err != nil || entry.Statement != "The inbox stays JSONL so grep keeps working" || entry.Kind != "decision" || entry.Tag != "stated" ||
		entry.Project != "github.com__you__eva-md" || entry.AsOf != "2026-01-14" || !slices.Equal(entry.Refs, []string{"s:3f2a91c0#1"}) || len(entry.Code) != 0 {
		t.Fatalf("entry %+v, %v", entry, err)
	}
	if err := os.Chmod(threads, 0o700); err != nil {
		t.Fatal(err)
	}
	e.clock = e.clock.Add(20 * time.Minute)
	s := e.run(c)
	if outcomes(s) != "ok" || e.mark(file).Offset != size(t, file) || s.Records[0].Pages != 1 ||
		s.Records[0].Validation.Knowledge != (KnowledgeCounts{Duplicates: 1}) {
		t.Fatalf("the rerun: outcomes %q, record %+v\n%s", outcomes(s), s.Records[0], e.log)
	}
	if _, err := e.wiki().Read("knowledge/k2"); err == nil {
		t.Fatal("the rerun wrote a second page")
	}
	rows, err := knowledgepage.ReadIndexAt(e.cfg.WikiPath)
	if err != nil || len(rows) != 1 || rows[0].ID != "k1" {
		t.Fatalf("index %+v, %v", rows, err)
	}
	root, _ := os.ReadFile(filepath.Join(e.cfg.WikiPath, "index.md"))
	if !strings.Contains(string(root), "(knowledge/index.md)") {
		t.Fatalf("root index:\n%s", root)
	}
	if v, err := e.wiki().Validate(); err != nil || len(v.Errors)+len(v.Orphans)+len(v.BrokenLinks)+len(v.Warnings) != 0 {
		t.Fatalf("okf validate: %v %+v", err, v)
	}
	// The user changes it: the page keeps the old statement in its History,
	// and the thread page's Timeline records the change. The input showed
	// the entry, and the record counts the page.
	known = []knowledgeOp{kSupersede("k1", "decision", "The inbox moves to SQLite", 1)}
	c.user("We decided the inbox moves to SQLite. " + filler)
	c.assistant("Fine. " + filler)
	e.write(c)
	s = e.run(c)
	req := e.fake.Requests()
	if outcomes(s) != "ok" || s.Records[0].Pages != 2 || s.Records[0].Validation.Knowledge != (KnowledgeCounts{Superseded: 1}) ||
		!strings.Contains(string(req[len(req)-1].Input), "\n- k1 decision for this project: The inbox stays JSONL so grep keeps working\n") {
		t.Fatalf("outcomes %q, record %+v\n%s", outcomes(s), s.Records[0], e.log)
	}
	pg, _ = e.wiki().Read("knowledge/k1")
	entry, _ = knowledgepage.Read(pg.Concept)
	thread, _ := e.page(testPage)
	if entry.Statement != "The inbox moves to SQLite" || len(entry.History) != 1 || !strings.Contains(entry.History[0], "~~The inbox stays JSONL so grep keeps working~~ → The inbox moves to SQLite (superseded 2026-01-14)") ||
		!strings.Contains(threadpage.Render(thread), "\n- change [stated]: knowledge k1: ~~The inbox stays JSONL so grep keeps working~~ → The inbox moves to SQLite s:3f2a91c0#3\n") {
		t.Fatalf("entry %+v\nthread page:\n%s", entry, threadpage.Render(thread))
	}
	// A person locks the page: the next change leaves it as it is, with a
	// log line, and the pass goes on.
	if _, err := e.wiki().Update("knowledge/k1", pg.Hash, func(c *okfadapter.Concept) error { c.Extra["locked"] = true; return nil }, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	locked, _ := e.wiki().Read("knowledge/k1")
	known = []knowledgeOp{kDrop("k1", "the user moved the inbox again", 1)}
	c.user("Forget the SQLite inbox decision. " + filler)
	c.assistant("Noted. " + filler)
	e.write(c)
	s = e.run(c)
	after, _ := e.wiki().Read("knowledge/k1")
	thread, _ = e.page(testPage)
	if outcomes(s) != "no_change" || string(after.Raw) != string(locked.Raw) || s.Records[0].Validation.Knowledge != (KnowledgeCounts{ReadOnly: 1}) ||
		strings.Contains(threadpage.Render(thread), "the user moved the inbox again") || !strings.Contains(e.log.String(), "knowledge op 0 on k1: locked; left as it is") {
		t.Fatalf("outcomes %q, page changed: %v\n%s", outcomes(s), string(after.Raw) != string(locked.Raw), e.log)
	}
}

// A pass in a project with commits cites the code its knowledge is about at
// the branch's HEAD, and the recall index lists the path.
func TestPassCitesTheCodeAtHead(t *testing.T) {
	e := newEnv(t)
	repo := filepath.Join(os.Getenv("HOME"), "projects", "eva-md")
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "commit.gpgsign", "false")
	git("remote", "add", "origin", "git@github.com:you/eva-md.git")
	git("commit", "-q", "--allow-empty", "-m", "first subject")
	head := git("rev-parse", "HEAD")
	known := []knowledgeOp{kAdd("decision", "Inbox format", "The inbox stays JSONL so grep keeps working", "stated", 1)}
	known[0].Paths = []string{"internal/inbox/store.go"}
	e.fake.Respond = func(r Request) (Result, error) {
		o := keeps(r)
		o.Summary, o.LastChat, o.Knowledge = "The inbox work goes on.", "Talked about the inbox.", known
		return Result{Output: encode(t, o), Attempts: 1}, nil
	}
	c := newChat("3f2a91c0-kode", start)
	c.user("The inbox stays JSONL so grep keeps working, see internal/inbox/store.go. " + filler)
	c.assistant("Understood. " + filler)
	e.write(c)
	if s := e.run(c); outcomes(s) != "ok" || s.Records[0].Validation.Knowledge != (KnowledgeCounts{Added: 1}) {
		t.Fatalf("outcomes %q, record %+v\n%s", outcomes(s), s.Records[0], e.log)
	}
	pg, err := e.wiki().Read("knowledge/k1")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := knowledgepage.Read(pg.Concept)
	if err != nil || !slices.Equal(entry.Code, []string{"github.com__you__eva-md@" + head + ":internal/inbox/store.go"}) {
		t.Fatalf("entry %+v, %v", entry, err)
	}
	rows, err := knowledgepage.ReadIndexAt(e.cfg.WikiPath)
	if err != nil || len(rows) != 1 || !slices.Equal(rows[0].Paths, []string{"internal/inbox/store.go"}) {
		t.Fatalf("index %+v, %v", rows, err)
	}
}
