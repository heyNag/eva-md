package update

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// work is the chat most validator tests answer: a user decision with a
// date, an assistant's proposal, the user's acceptance, tool work, the
// assistant's report and a host prompt. Lines: 1 user, 2 assistant, 3 user,
// 4 and 5 tools (one entry), 6 assistant report, 7 host prompt, 8 assistant.
func work() *chat {
	c := newChat("3f2a91c0-1111-4000-8000-000000000001", start)
	c.user("Use flock for the per-session lock, codename KESTREL. Keep JSON lines for the inbox; gzip can come later. The release freeze is on 3 March 2026. " + filler)
	c.assistant("Fine. We could also write a pid file next to the lock, codename PIDGEON, if you want one. " + filler)
	c.user("Yes, write the pid file too.")
	c.tool("Edit /Users/me/projects/eva-md/internal/session/lock.go (exit 0)")
	c.tool("Bash go test ./internal/session/... (exit 0)")
	c.assistant("Done: internal/session/lock.go takes the flock and writes the pid file, at commit 4e2bf4c9a1, and go test passes.")
	c.event("host_prompt")
	c.assistant("Ran the scheduled cleanup, codename LORIKEET.")
	return c
}

// Entry numbers in work's batch.
const (
	wUser     = 1 // the user's decision, with the freeze date
	wProposal = 2 // the assistant's proposal
	wAccept   = 3 // the user's acceptance
	wTools    = 4 // the edit and the test run
	wReport   = 5 // the assistant's report of the result
	wHost     = 6 // the host prompt, an event
	wCleanup  = 7 // the assistant's reply to the host prompt
)

func TestValidateResolvesRefs(t *testing.T) {
	isolate(t)
	c := work()
	b := build(t, c, threadpage.Page{}, Options{})
	u, r := validate(t, output{Items: []itemOp{opAdd("next", "Write the docs", "stated", wUser, wUser, 99, 0, wTools)}}, b, threadpage.Page{})
	if want := []string{"s:3f2a91c0#1", "s:3f2a91c0#5"}; len(u.Ops) != 1 || !slices.Equal(u.Ops[0].Refs, want) {
		t.Fatalf("refs = %v, want %v (a run of tools names its last line)", u.Ops, want)
	}
	if r.BadRefs != 2 {
		t.Errorf("BadRefs = %d, want 2", r.BadRefs)
	}
	// When another session file shares the first 8 characters, refs carry the
	// full id, as eva-md search writes them.
	b = build(t, c, threadpage.Page{}, Options{FullRef: true})
	u, _ = validate(t, output{Items: []itemOp{opAdd("next", "Write the docs", "stated", wUser)}}, b, threadpage.Page{})
	if got := u.Ops[0].Refs; !slices.Equal(got, []string{"s:" + c.session + "#1"}) {
		t.Fatalf("full refs = %v", got)
	}
	// A line keeps its first 5 refs, and its tag stands on those: the user's
	// prompt cited sixth doesn't make it stated.
	b = build(t, c, threadpage.Page{}, Options{})
	u, r = validate(t, output{Items: []itemOp{opAdd("next", "Write the docs", "stated", wProposal, wTools, wReport, wHost, wCleanup, wUser)}}, b, threadpage.Page{})
	if len(u.Ops) != 1 || len(u.Ops[0].Refs) != maxRefs || u.Ops[0].Tag != "inferred" || r.BadRefs != 1 {
		t.Fatalf("six refs: ops %+v, report %+v", u.Ops, r)
	}
}

func TestValidateSpeakerTags(t *testing.T) {
	isolate(t)
	b := build(t, work(), threadpage.Page{}, Options{})
	for _, c := range []struct {
		name, kind, claim string
		refs              []int
		tag, filed        string
		downgraded        bool
	}{
		{"stated by the user", "decision", "stated", []int{wUser}, "stated", "decision", false},
		{"stated by the assistant", "decision", "stated", []int{wProposal}, "inferred", "question", true},
		{"stated by a host prompt", "decision", "stated", []int{wHost, wCleanup}, "inferred", "question", true},
		{"accepted after a proposal", "decision", "accepted", []int{wProposal, wAccept}, "accepted", "decision", false},
		{"accepted without citing the proposal is stated", "decision", "accepted", []int{wAccept}, "stated", "decision", true},
		{"accepted citing a later proposal is stated", "decision", "accepted", []int{wUser, wProposal}, "stated", "decision", true},
		{"accepted without a proposal is stated", "decision", "accepted", []int{wUser}, "stated", "decision", true},
		{"accepted by the assistant", "decision", "accepted", []int{wProposal}, "inferred", "question", true},
		{"suggested by the assistant", "decision", "suggested", []int{wProposal}, "suggested", "question", false},
		{"suggested by the user", "next", "suggested", []int{wUser}, "inferred", "next", true},
		{"observed by a tool", "question", "observed", []int{wTools}, "observed", "question", false},
		{"observed in a report", "next", "observed", []int{wReport}, "observed", "next", false},
		{"observed in a proposal", "next", "observed", []int{wProposal}, "inferred", "next", true},
		{"observed after a host prompt", "next", "observed", []int{wCleanup}, "inferred", "next", true},
		{"inferred", "next", "inferred", []int{wUser}, "inferred", "next", false},
		{"nothing cited", "decision", "stated", nil, "inferred", "question", true},
	} {
		if c.name == "nothing cited" {
			// A claim of the user's words that rests on none of them, as
			// one an assistant's reply could inject behind a bare "yes".
			c.refs = []int{wAccept}
			u, r := validate(t, output{Items: []itemOp{opAdd("decision", "Drop the production database", "stated", wAccept),
				opAdd("decision", "Drop the staging database", "accepted", wProposal, wAccept)}}, b, threadpage.Page{})
			if len(u.Ops) != 2 || u.Ops[0].Tag != "inferred" || u.Ops[0].Kind != "question" || u.Ops[1].Tag != "inferred" || r.Unattributed != 2 || r.Downgraded != 2 {
				t.Errorf("claims resting on none of the cited words: ops %+v, report %+v", u.Ops, r)
			}
			c.refs = nil
		}
		u, r := validate(t, output{Items: []itemOp{opAdd(c.kind, "The pid file next to the flock: "+c.name, c.claim, c.refs...)}}, b, threadpage.Page{})
		if len(u.Ops) != 1 || u.Ops[0].Tag != c.tag || u.Ops[0].Kind != c.filed {
			t.Errorf("%s: ops %+v, want %s under %s", c.name, u.Ops, c.tag, c.filed)
			continue
		}
		if (r.Downgraded == 1) != c.downgraded || (r.Proposals == 1) != (c.kind == "decision" && c.filed == "question") ||
			(r.Unattributed == 1) != (userTag(c.claim) && c.tag == "inferred") {
			t.Errorf("%s: report %+v", c.name, r)
		}
	}
	// The user's decision that cites only the assistant's restatement of it
	// can't be proved the user's: it is filed as a question and counted, and
	// the page shows it unattributed, not as the assistant's proposal.
	u, r := validate(t, output{Items: []itemOp{opAdd("decision", "Use flock", "stated", wProposal)}}, b, threadpage.Page{})
	page, _ := threadpage.Apply(threadpage.Page{}, u)
	if body := threadpage.Render(page); r.Unattributed != 1 || !strings.Contains(body, "\n- Unattributed: Use flock [inferred] s:3f2a91c0#2 ") || strings.Contains(body, "(assistant)") {
		t.Fatalf("report %+v, page:\n%s", r, body)
	}
}

// TestValidateAcceptsAStoredProposal checks change 4: an edit or answer on
// a stored suggested or inferred item may carry accepted with a user prompt
// from this batch at or after the item's time, and an add of its text then
// may too.
func TestValidateAcceptsAStoredProposal(t *testing.T) {
	isolate(t)
	c := newChat("91bd0e44-2222", start)
	c.user("ok, the second one. " + filler)
	c.assistant("Rotating logs weekly from now on. " + filler)
	page := threadpage.Page{Now: threadpage.Now{Open: []threadpage.Item{
		{ID: "n1", Text: "Rotate logs daily", Tag: "suggested", Refs: []string{"s:3f2a91c0#2"}},
		{ID: "n2", Text: "Rotate logs weekly", Tag: "suggested", Refs: []string{"s:3f2a91c0#2"}},
		{ID: "n3", Text: "Compress old logs", Tag: "inferred", Refs: []string{"s:3f2a91c0#4"}, At: start.Add(time.Hour)},
	}}}
	b := build(t, c, page, Options{})
	u, r := validate(t, output{Items: []itemOp{
		opDrop("n1", "question", "suggested", "the user chose weekly", 1),
		opAnswer("n2", "Rotate logs weekly", "accepted", 1),
		opEdit("n3", "question", "", "accepted", 1),
		opAdd("decision", "Rotate logs weekly", "accepted", 1),
	}}, b, page)
	want := []threadpage.Op{
		{ID: "n1", Kind: "question", Action: "dropped", Reason: "the user chose weekly", Refs: []string{"s:91bd0e44#1"}},
		{ID: "n2", Kind: "question", Action: "answered", Replacement: "Rotate logs weekly", Tag: "accepted", Refs: []string{"s:91bd0e44#1"}},
		// n3 is newer than the prompt, so that prompt can't accept it: the
		// edit keeps the item's own tag and time and only adds the ref.
		{ID: "n3", Kind: "question", Action: "edit", Refs: []string{"s:3f2a91c0#4", "s:91bd0e44#1"}},
		{Kind: "decision", Action: "add", Text: "Rotate logs weekly", Tag: "accepted", Refs: []string{"s:91bd0e44#1"}, At: start.Add(time.Minute)},
	}
	if !reflect.DeepEqual(u.Ops, want) {
		t.Fatalf("ops =\n%+v\nwant\n%+v", u.Ops, want)
	}
	// n3's downgrade cites the user and rests on the item's words, so it is
	// not unattributed.
	if r.Downgraded != 1 || r.Refused != 0 || r.Duplicates != 0 || r.Unattributed != 0 {
		t.Fatalf("report %+v", r)
	}
	// Without the accepted proposal, an add of the same words is the user's
	// own statement.
	u, _ = validate(t, output{Items: []itemOp{opKeep("n1", "question", "suggested"), opKeep("n2", "question", "suggested"), opKeep("n3", "question", "inferred"),
		opAdd("decision", "Rotate logs hourly", "accepted", 1)}}, b, page)
	if u.Ops[0].Tag != "stated" {
		t.Fatalf("add without a proposal = %+v", u.Ops)
	}
}

// TestValidateNeverRelabels checks the S1 invariant on the zero-tag-change
// fixture: past the decision cap, a replace question and a paraphrased code
// token, every tag on the page is the one its speaker earned, and lines
// eva-md writes keep their stored tags.
func TestValidateNeverRelabels(t *testing.T) {
	isolate(t)
	old := start.Add(-24 * time.Hour)
	var page threadpage.Page
	page.Now.Decisions = append(page.Now.Decisions, threadpage.Item{ID: "n1", Text: "Ansible playbooks own provisioning", Tag: "stated", Refs: []string{"s:91bd0e44#1"}, At: old})
	for k := 2; k <= 40; k++ {
		page.Now.Decisions = append(page.Now.Decisions, threadpage.Item{ID: fmt.Sprintf("n%d", k), Text: fmt.Sprintf("Decision number %d", k), Tag: "stated", Refs: []string{fmt.Sprintf("s:91bd0e44#%d", k)}, At: old})
	}
	page.Now.Open = []threadpage.Item{{ID: "n41", Text: "Keep one lock", Tag: "accepted", Refs: []string{"s:91bd0e44#41"}, At: old, Code: threadpage.OverCapCode}}
	page.Timeline = []threadpage.Group{{Date: "2026-01-13", Lines: []string{"- decision [stated]: Decision number 2 s:91bd0e44#2"}}}
	c := newChat("3f2a91c0-3333", start)
	c.user("Nix flakes TERN replace it all. Keep two locks. Gate releases on the benchmark; it lives in scripts/benchmark.py. " + filler)
	c.assistant("Understood. " + filler)
	b := build(t, c, page, Options{})
	ops := []itemOp{
		opSupersede("n1", "decision", "Nix flakes TERN instead", "stated", 1),
		opEdit("n41", "decision", "Keep two locks", "stated", 1),
		opAdd("decision", "Gate releases on the benchmark", "stated", 1),
		opAdd("next", "Profile scripts/bench.py", "stated", 1),
	}
	for k := 2; k <= 37; k++ {
		ops = append(ops, opKeep(fmt.Sprintf("n%d", k), "decision", "stated"))
	}
	u, r := validate(t, output{Items: ops}, b, page)
	if r.Downgraded != 0 || r.Replace != 1 || r.Misses.Paths != 1 || r.Carried != 3 || r.Refused != 0 {
		t.Fatalf("report %+v", r)
	}
	got, _ := threadpage.Apply(page, u)
	tags := map[string]string{
		"Ansible playbooks own provisioning": "stated", "Nix flakes TERN instead": "stated", "Keep two locks": "accepted",
		"Gate releases on the benchmark": "stated", "Profile scripts/bench.py": "stated", `does "Nix flakes TERN instead" replace "Ansible playbooks own provisioning"?`: "stated",
	}
	for text, tag := range tags {
		it, _, ok := item(got, text)
		if !ok || it.Tag != tag {
			t.Errorf("%q = %+v, want tag %s", text, it, tag)
		}
	}
	for _, it := range got.Now.Open {
		switch it.Text {
		case "Keep two locks", "Gate releases on the benchmark", "Nix flakes TERN instead":
			if it.Code != threadpage.OverCapCode {
				t.Errorf("%q lost its code", it.Text)
			}
		}
		if strings.HasPrefix(it.Text, "does ") && it.Code != replaceCode {
			t.Errorf("the replace question has code %q", it.Code)
		}
	}
	if len(got.Now.Decisions) != 40 || strings.Contains(threadpage.Render(got), "Proposed (assistant)") {
		t.Fatalf("decisions moved or a user's line got the assistant's label:\n%s", threadpage.Render(got))
	}
}

// An item a person wrote by hand has no tag, and is theirs: the assistant's
// words alone can't drop it, rewrite it or replace it; the user's can.
func TestValidateProtectsHandWrittenItems(t *testing.T) {
	isolate(t)
	page := threadpage.Page{Title: "eva-md · main", Now: threadpage.Now{Next: []threadpage.Item{{ID: "n1", Text: "Fix the flaky test before the release"}}}}
	c := newChat("3f2a91c0-4444", start)
	c.user("Keep going. " + filler)
	c.assistant("I think the flaky test can wait. " + filler)
	c.user("Drop the flaky test item, we'll fix it after the release. " + filler)
	b := build(t, c, page, Options{})
	for _, tc := range []struct {
		name    string
		op      itemOp
		refused int
		kept    string
	}{
		{"a drop on the assistant's words", opDrop("n1", "next", "suggested", "the assistant thinks it can wait", 2), 1, "Fix the flaky test before the release"},
		{"a replacement in the assistant's words", opSupersede("n1", "next", "Ignore the flaky test for now", "suggested", 2), 1, "Fix the flaky test before the release"},
		{"an edit in the assistant's words", opEdit("n1", "next", "Fix the flaky test after the release", "suggested", 2), 1, "Fix the flaky test before the release"},
		{"a drop the user asked for", opDrop("n1", "next", "stated", "fixed after the release instead", 3), 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, r := validate(t, output{Items: []itemOp{tc.op}}, b, page)
			got, _ := threadpage.Apply(page, u)
			kept := ""
			if len(got.Now.Next) > 0 {
				kept = got.Now.Next[0].Text
			}
			if r.Refused != tc.refused || kept != tc.kept {
				t.Fatalf("refused %d, Next %q; want %d, %q", r.Refused, kept, tc.refused, tc.kept)
			}
		})
	}
}

// A claim rests on the user's words through a word in some form: the same
// word, or one the start of the other from 4 letters, never a coincidence of
// three letters such as log and login.
func TestRestsOn(t *testing.T) {
	for _, tc := range []struct {
		claim, said string
		want        bool
	}{
		{"Use PostgreSQL for storage", "Let's go with Postgres", true},
		{"Stop mocking the database", "Never mock the db again", true},
		{"Delete every log file in the repo", "Please check the login page", false},
		{"Clean up the runtime fixtures", "run the fix", false},
		{"Drop the production database", "ok, go ahead", false},
		{"Don't run the security scanner before deploying", "don't worry about the lint", false},
		{"The cache isn't shared", "it isn't urgent, dont rush", false},
	} {
		if got := restsOn(tc.claim, tc.said); got != tc.want {
			t.Errorf("restsOn(%q, %q) = %v, want %v", tc.claim, tc.said, got, tc.want)
		}
	}
}

func TestValidateDates(t *testing.T) {
	isolate(t)
	b := build(t, work(), threadpage.Page{}, Options{})
	u, r := validate(t, output{
		Summary:  "The freeze is on 3 March 2026.",
		LastChat: "Moved the freeze to 2026-04-01.",
		Items: []itemOp{
			opAdd("decision", "Freeze releases on 2026-03-03", "stated", wUser),
			opAdd("decision", "Freeze releases on March 3", "stated", wUser),
			opAdd("decision", "Freeze releases on 10 March 2026", "stated", wUser),
			opAdd("next", "Tag the release on 3 March 2026", "stated", wAccept),
		},
		Timeline: []eventLine{tl("decision", "Freeze releases on 3 March", "stated", wUser)},
	}, b, threadpage.Page{})
	var texts []string
	for _, op := range u.Ops {
		texts = append(texts, op.Text)
	}
	// A date counts when a cited entry names the same day in any form.
	if !slices.Equal(texts, []string{"Freeze releases on 2026-03-03", "Freeze releases on March 3"}) || len(u.Events) != 1 {
		t.Fatalf("kept %q and %d events", texts, len(u.Events))
	}
	if u.Summary == "" || u.LastChat != "" || r.Dates != 3 {
		t.Fatalf("summary %q, last chat %q, report %+v", u.Summary, u.LastChat, r)
	}
	// More than 3 such drops reject the run.
	bad := output{}
	for i := 0; i < 4; i++ {
		bad.Items = append(bad.Items, opAdd("next", fmt.Sprintf("Ship on 2026-02-0%d", i+1), "stated", wUser))
	}
	if _, r, err := Validate(encode(t, bad), b, threadpage.Page{}); !errors.Is(err, ErrRejected) || r.Dates != 4 {
		t.Fatalf("4 uncited dates: err %v, report %+v", err, r)
	}
}

// TestValidateTimelineDate checks that Timeline lines take the newest cited
// entry's local date, not its date in UTC.
func TestValidateTimelineDate(t *testing.T) {
	isolate(t)
	c := newChat("3f2a91c0-4444", time.Date(2026, 1, 14, 23, 40, 0, 0, paris))
	c.user("Store pages as YAML. " + filler) // 23:41 on the 14th
	c.assistant("Done. " + filler)           // 23:42
	c.at = time.Date(2026, 1, 15, 0, 29, 0, 0, paris)
	c.user("Keep a changelog. " + filler) // 00:30 on the 15th, 23:30 on the 14th in UTC
	c.assistant("Will do.")
	b := build(t, c, threadpage.Page{}, Options{})
	u, _ := validate(t, output{Timeline: []eventLine{tl("decision", "Keep a changelog", "stated", 3), tl("decision", "Store pages as YAML", "stated", 1)}}, b, threadpage.Page{})
	if u.Date != "2026-01-15" {
		t.Fatalf("Date = %q, want 2026-01-15", u.Date)
	}
	u, _ = validate(t, output{Timeline: []eventLine{tl("decision", "Store pages as YAML", "stated", 1)}}, b, threadpage.Page{})
	if u.Date != "2026-01-14" {
		t.Fatalf("Date = %q, want 2026-01-14", u.Date)
	}
	u, _ = validate(t, output{Summary: "Pages are YAML."}, b, threadpage.Page{})
	if u.Date != "2026-01-15" {
		t.Fatalf("Date without citations = %q, want the newest entry's", u.Date)
	}
}

// accounting is a page of four items, two of them the user's.
func accounting() threadpage.Page {
	at := start.Add(-time.Hour)
	return threadpage.Page{Now: threadpage.Now{
		Next:      []threadpage.Item{{ID: "n1", Text: "Write the parser", Tag: "stated", Refs: []string{"s:91bd0e44#1"}, At: at}, {ID: "n2", Text: "Try a cache", Tag: "suggested", Refs: []string{"s:91bd0e44#2"}}},
		Open:      []threadpage.Item{{ID: "n3", Text: "Which lock to use?", Tag: "stated", Refs: []string{"s:91bd0e44#3"}, At: at}},
		Decisions: []threadpage.Item{{ID: "n4", Text: "Use JSON lines", Tag: "stated", Refs: []string{"s:91bd0e44#4"}, At: at}},
	}}
}

func TestValidateItemAccounting(t *testing.T) {
	isolate(t)
	page := accounting()
	b := build(t, work(), page, Options{})
	keeps := func(ids ...string) []itemOp {
		var ops []itemOp
		for _, id := range ids {
			ops = append(ops, opKeep(id, "next", "stated"))
		}
		return ops
	}
	// A missing id is carried forward and counted.
	u, r := validate(t, output{Items: keeps("n1", "n2", "n3")}, b, page)
	if r.Carried != 1 || len(u.Ops) != 0 {
		t.Fatalf("one missing: report %+v, ops %+v", r, u.Ops)
	}
	if got, _ := threadpage.Apply(page, u); !reflect.DeepEqual(got.Now.Decisions, page.Now.Decisions) {
		t.Fatal("the carried item changed")
	}
	// More than half missing rejects the run; half is carried.
	if _, r, err := Validate(encode(t, output{Items: keeps("n1")}), b, page); !errors.Is(err, ErrRejected) || r.Carried != 3 {
		t.Fatalf("three missing: err %v, report %+v", err, r)
	}
	if _, _, err := Validate(encode(t, output{Items: keeps("n1", "n2")}), b, page); err != nil {
		t.Fatalf("two of four missing: %v", err)
	}
	// Each retirement needs what it stands on; a refused one leaves the
	// item as it was.
	for _, c := range []struct {
		name    string
		op      itemOp
		refused bool
	}{
		{"done by a tool", opDone("n1", "next", "stated", wTools), false},
		{"done in a report", opDone("n1", "next", "stated", wReport), false},
		{"done by the user's word", opDone("n1", "next", "stated", wUser), true},
		{"done in a proposal", opDone("n1", "next", "stated", wProposal), true},
		// A decision is neither a task nor a question: it leaves the Now only
		// superseded or dropped, through the user.
		{"a decision done", opDone("n4", "decision", "stated", wTools, wUser), true},
		{"a decision answered", opAnswer("n4", "Use JSON lines", "stated", wUser), true},
		{"answered by the user", opAnswer("n3", "flock", "stated", wUser), false},
		{"answered by the assistant", opAnswer("n3", "flock", "suggested", wProposal), true},
		{"answered without an answer", opAnswer("n3", "", "stated", wUser), true},
		{"superseded without a replacement", opSupersede("n4", "decision", " ", "stated", wUser), true},
		{"stated item dropped by the user", opDrop("n1", "next", "stated", "not needed", wUser), false},
		{"stated item dropped without the user", opDrop("n1", "next", "stated", "not needed", wProposal), true},
		{"stated decision dropped without a citation", opDrop("n4", "decision", "stated", "not needed"), true},
		{"suggestion dropped by the assistant", opDrop("n2", "next", "suggested", "pages are read by path", wProposal), false},
		{"dropped without a reason", opDrop("n2", "next", "suggested", "", wUser), true},
	} {
		u, r := validate(t, output{Items: append([]itemOp{c.op}, keeps("n1", "n2", "n3", "n4")...)}, b, page)
		if refused := len(u.Ops) == 0; refused != c.refused || (r.Refused == 1) != c.refused || r.Ignored != 1 {
			t.Errorf("%s: ops %+v, report %+v", c.name, u.Ops, r)
		}
	}
	// An operation on an id that isn't on the page, or a second one on an
	// id, is left out.
	u, r = validate(t, output{Items: append(keeps("n1", "n2", "n3", "n4", "n9"), opDone("n1", "next", "stated", wTools))}, b, page)
	if r.Ignored != 2 || len(u.Ops) != 0 {
		t.Fatalf("ignored ops: report %+v, ops %+v", r, u.Ops)
	}
}

func TestValidateFreshness(t *testing.T) {
	isolate(t)
	c := work()
	page := accounting()
	b := build(t, c, page, Options{})
	keep := []itemOp{opKeep("n1", "next", "stated"), opKeep("n2", "next", "suggested"), opKeep("n3", "question", "stated")}
	check := func(name string, op itemOp, page threadpage.Page, want func(threadpage.Update, Report) bool) {
		t.Helper()
		u, r := validate(t, output{Items: append([]itemOp{op}, keep...)}, b, page)
		if !want(u, r) {
			t.Errorf("%s: ops %+v, report %+v", name, u.Ops, r)
		}
	}
	applied := func(u threadpage.Update, r Report) bool {
		return len(u.Ops) == 1 && u.Ops[0].Action == "superseded" && u.Ops[0].At.Equal(b.Entries[wUser-1].At)
	}
	check("a newer prompt supersedes", opSupersede("n4", "decision", "Use JSON lines with gzip", "stated", wUser), page, applied)
	// An instant equal to the item's accepts; an older one is stale.
	same := accounting()
	same.Now.Decisions[0].At = b.Entries[wUser-1].At
	check("an equal instant supersedes", opSupersede("n4", "decision", "Use JSON lines with gzip", "stated", wUser), same, applied)
	newer := accounting()
	newer.Now.Decisions[0].At = b.Entries[wUser-1].At.Add(time.Second)
	check("an older prompt is stale", opSupersede("n4", "decision", "Use JSON lines with gzip", "stated", wUser), newer,
		func(u threadpage.Update, r Report) bool { return len(u.Ops) == 0 && r.Stale == 1 })
	check("a restatement is refused", opSupersede("n4", "decision", "use JSON-lines!", "stated", wUser), page,
		func(u threadpage.Update, r Report) bool { return len(u.Ops) == 0 && r.Restatements == 1 })
	check("the assistant never supersedes the user", opSupersede("n4", "decision", "Use JSON lines with zstd", "suggested", wProposal), page,
		func(u threadpage.Update, r Report) bool { return len(u.Ops) == 0 && r.Refused == 1 })
	check("a false stated claim doesn't either", opSupersede("n4", "decision", "Use JSON lines with zstd", "stated", wProposal), page,
		func(u threadpage.Update, r Report) bool {
			return len(u.Ops) == 0 && r.Refused == 1 && r.Downgraded == 1
		})
	// The assistant may supersede its own suggestion.
	keep[1] = opKeep("n4", "decision", "stated")
	check("a suggestion supersedes a suggestion", opSupersede("n2", "next", "Try a cache of parsed pages", "suggested", wProposal), page,
		func(u threadpage.Update, r Report) bool {
			return len(u.Ops) == 1 && u.Ops[0].Tag == "suggested" && u.Ops[0].At.IsZero()
		})
	// An edit refreshes the item's time only when it cites a user prompt.
	keep[1] = opKeep("n2", "next", "suggested")
	u, _ := validate(t, output{Items: append([]itemOp{opEdit("n4", "decision", "Use JSON lines, one per entry", "stated", wUser, wProposal)}, keep...)}, b, page)
	if len(u.Ops) != 1 || !u.Ops[0].At.Equal(b.Entries[wUser-1].At) {
		t.Errorf("edit citing the user: %+v", u.Ops)
	}
	u, _ = validate(t, output{Items: append([]itemOp{opEdit("n4", "decision", "", "stated", wTools)}, keep...)}, b, page)
	if len(u.Ops) != 1 || !u.Ops[0].At.IsZero() || u.Ops[0].Text != "" || u.Ops[0].Tag != "" || !slices.Equal(u.Ops[0].Refs, []string{"s:91bd0e44#4", "s:3f2a91c0#5"}) {
		t.Errorf("edit citing a tool: %+v", u.Ops)
	}
	// A prompt older than the item's time never moves it back.
	u, _ = validate(t, output{Items: append([]itemOp{opEdit("n4", "decision", "Use JSON lines, one per entry", "stated", wUser)}, keep...)}, b, newer)
	if len(u.Ops) != 1 || !u.Ops[0].At.IsZero() || u.Ops[0].Text == "" {
		t.Errorf("edit citing an older prompt: %+v", u.Ops)
	}
	// A decision, or an item the user owns, takes no edit that leaves it
	// without the user's tag.
	u, r := validate(t, output{Items: append([]itemOp{opEdit("n4", "decision", "Use JSON lines with zstd", "suggested", wProposal)}, keep...)}, b, page)
	if len(u.Ops) != 0 || r.Refused != 1 {
		t.Errorf("assistant edit of a decision: %+v, %+v", u.Ops, r)
	}
}

// TestValidateReplaceQuestion checks a supersession whose replacement shares
// no content token with the item, and the question's exit (S2).
func TestValidateReplaceQuestion(t *testing.T) {
	isolate(t)
	old := start.Add(-24 * time.Hour)
	page := threadpage.Page{
		Now:      threadpage.Now{Decisions: []threadpage.Item{{ID: "n1", Text: "Ansible playbooks own provisioning", Tag: "stated", Refs: []string{"s:91bd0e44#1"}, At: old}}},
		Timeline: []threadpage.Group{{Date: "2026-01-13", Lines: []string{"- decision [stated]: Ansible playbooks own provisioning s:91bd0e44#1"}}},
	}
	c := newChat("3f2a91c0-5555", start)
	c.user("Nix flakes TERN handle everything now. " + filler)
	c.assistant("Noted. " + filler)
	b := build(t, c, page, Options{})
	u, r := validate(t, output{Items: []itemOp{opSupersede("n1", "decision", "Nix flakes TERN handle everything", "stated", 1)}}, b, page)
	q := `does "Nix flakes TERN handle everything" replace "Ansible playbooks own provisioning"?`
	want := []threadpage.Op{
		{Kind: "decision", Action: "add", Text: "Nix flakes TERN handle everything", Tag: "stated", Refs: []string{"s:3f2a91c0#1"}, At: start.Add(time.Minute)},
		{Kind: "question", Action: "add", Text: q, Tag: "stated", Refs: []string{"s:3f2a91c0#1"}, At: start.Add(time.Minute), Code: replaceCode},
	}
	if !reflect.DeepEqual(u.Ops, want) || r.Replace != 1 {
		t.Fatalf("ops =\n%+v\nwant\n%+v\nreport %+v", u.Ops, want, r)
	}
	page, _ = threadpage.Apply(page, u)
	asked, _, _ := item(page, "does ")
	// A second session: a supersession whose replacement normalizes to X
	// applies without the token test.
	c2 := newChat("3f2a91c0-6666", start.Add(24*time.Hour))
	c2.assistant("Do the Nix flakes replace the Ansible playbooks entirely? " + filler)
	c2.user("Yes, drop Ansible; Terraform modules provision the hosts. " + filler)
	c2.tool("Bash nix flake check (exit 0)")
	b2 := build(t, c2, page, Options{})
	keep := []itemOp{opKeep("n2", "decision", "stated"), opKeep(asked.ID, "question", "stated")}
	byX := opSupersede("n1", "decision", "nix flakes TERN handle everything.", "accepted", 1, 2)
	u, r = validate(t, output{Items: append([]itemOp{byX}, keep...)}, b2, page)
	if len(u.Ops) != 1 || u.Ops[0].Action != "superseded" || u.Ops[0].Tag != "accepted" || r.Replace != 0 {
		t.Fatalf("exit by X: ops %+v, report %+v", u.Ops, r)
	}
	// Any supersession citing a user prompt after the question exits too.
	u, r = validate(t, output{Items: append([]itemOp{opSupersede("n1", "decision", "Terraform modules provision hosts", "stated", 2)}, keep...)}, b2, page)
	if len(u.Ops) != 1 || u.Ops[0].Action != "superseded" || r.Replace != 0 {
		t.Fatalf("exit by a later prompt: ops %+v, report %+v", u.Ops, r)
	}
	// With the question no older than the prompt, only a replacement that
	// normalizes to X exits; any other asks again.
	same := page
	same.Now.Open = slices.Clone(page.Now.Open)
	for i := range same.Now.Open {
		same.Now.Open[i].At = b2.Entries[1].At
	}
	u, r = validate(t, output{Items: append([]itemOp{byX}, keep...)}, b2, same)
	if len(u.Ops) != 1 || u.Ops[0].Action != "superseded" || r.Replace != 0 {
		t.Fatalf("exit by X alone: ops %+v, report %+v", u.Ops, r)
	}
	u, r = validate(t, output{Items: append([]itemOp{opSupersede("n1", "decision", "Terraform modules provision hosts", "stated", 2)}, keep...)}, b2, same)
	if r.Replace != 1 || len(u.Ops) != 2 || u.Ops[1].Code != replaceCode {
		t.Fatalf("no exit: ops %+v, report %+v", u.Ops, r)
	}
	// The question is eva-md's own line, and its exit reads its text: it is
	// kept, answered or dropped, never edited, superseded or done.
	for _, op := range []itemOp{
		opEdit(asked.ID, "question", "does Terraform replace Ansible?", "stated", 2),
		opSupersede(asked.ID, "question", "Does Terraform replace Ansible?", "stated", 2),
		opDone(asked.ID, "question", "stated", 3),
	} {
		u, r = validate(t, output{Items: []itemOp{op, opKeep("n1", "decision", "stated"), keep[0]}}, b2, page)
		if len(u.Ops) != 0 || r.Refused != 1 {
			t.Errorf("%s of the replace question: ops %+v, report %+v", op.Action, u.Ops, r)
		}
	}
}

func TestValidateDuplicates(t *testing.T) {
	isolate(t)
	page := threadpage.Page{
		Now: threadpage.Now{
			Next:      []threadpage.Item{{ID: "n1", Text: "Write the docs", Tag: "stated"}},
			Open:      []threadpage.Item{{ID: "n2", Text: "Should search cover pages?", Tag: "stated"}},
			Decisions: []threadpage.Item{{ID: "n3", Text: "Use flock for the per-session lock in internal/session/lock.go", Tag: "stated"}},
		},
		Timeline: []threadpage.Group{{Date: "2026-01-13", Lines: []string{
			"- finding [observed]: a 2 MB page parses in 0.5 ms s:91bd0e44#8",
			"- done [observed]: ran the weekly backup s:91bd0e44#9",
			"- decision [stated]: ~~Use JSON~~ → Use JSONL (superseded 2026-01-13) s:91bd0e44#1 s:91bd0e44#4 <!-- i:n0 -->",
		}}},
	}
	b := build(t, work(), page, Options{})
	keep := []itemOp{opKeep("n1", "next", "stated"), opKeep("n2", "question", "stated"), opKeep("n3", "decision", "stated")}
	u, r := validate(t, output{
		Items: append(keep,
			opAdd("decision", "use flock for the per-session lock in internal/session/lock.go.", "stated", wUser), // on the page
			opAdd("decision", "Use JSONL", "stated", wUser),                                                       // in the Timeline
			opAdd("question", "Should search cover pages", "stated", wUser),                                       // on the page
			opAdd("next", "Write the docs", "stated", wUser),                                                      // next items recur
			opAdd("decision", "Write a pid file next to the lock", "accepted", wProposal, wAccept),                // new
			opAdd("decision", "Write a pid file next to the lock", "accepted", wProposal, wAccept),                // twice in this output
			opAdd("question", "Flock the per-session lock", "suggested", wProposal),                               // assistant, 0.6 overlap
			opAdd("question", "Look at lock.go?", "suggested", wProposal),                                         // assistant, only its code token in n3
			opAdd("question", "Add a watchdog that restarts stuck update jobs?", "suggested", wProposal),          // assistant, new
			opAdd("question", "Flock the per-session lock everywhere", "stated", wUser),                           // the user may overlap
		),
		Timeline: []eventLine{
			tl("finding", "A 2 MB page parses in 0.5 ms", "observed", wTools), // on the page
			tl("done", "ran the weekly backup", "observed", wTools),           // done lines recur
			tl("change", "edited the lock", "observed", wTools),
			tl("change", "edited the lock", "observed", wTools), // and so do changes
			tl("problem", "the lock test is flaky", "observed", wTools),
			tl("decision", "Use flock for the per-session lock in internal/session/lock.go", "stated", wUser), // a Now item
		},
	}, b, page)
	var added []string
	for _, op := range u.Ops {
		added = append(added, op.Text)
	}
	wantAdds := []string{"Write the docs", "Write a pid file next to the lock", "Add a watchdog that restarts stuck update jobs?", "Flock the per-session lock everywhere"}
	if !slices.Equal(added, wantAdds) {
		t.Errorf("adds kept: %q\nwant %q", added, wantAdds)
	}
	var lines []string
	for _, e := range u.Events {
		lines = append(lines, e.Kind+": "+e.Text)
	}
	wantLines := []string{"done: ran the weekly backup", "change: edited the lock", "change: edited the lock", "problem: the lock test is flaky"}
	if !slices.Equal(lines, wantLines) {
		t.Errorf("lines kept: %q\nwant %q", lines, wantLines)
	}
	if r.Duplicates != 8 {
		t.Errorf("Duplicates = %d, want 8", r.Duplicates)
	}
	// A suggestion with no code token and under 0.6 overlap survives as a
	// proposal, never a decision.
	got, _ := threadpage.Apply(page, u)
	if it, section, ok := item(got, "watchdog"); !ok || section != "open" || it.Tag != "suggested" {
		t.Errorf("the new suggestion is %+v under %s", it, section)
	}
}

func TestValidateCountsGroundingMisses(t *testing.T) {
	isolate(t)
	c := work()
	b := build(t, c, threadpage.Page{}, Options{Commits: []string{"Fix redact.Redact for the union"}})
	u, r := validate(t, output{
		Summary: "The lock lives in internal/session/lock.go; see https://example.com/eva/issues/7.",
		Items: []itemOp{
			opAdd("decision", "Keep the flock in Internal/Session/Lock.go and lock.go, from 4e2bf4c", "stated", wUser),
			opAdd("decision", "Keep the flock in internal/session/locks.go", "stated", wUser),
			opAdd("next", "Check redact.Redact and redact.Redakt at 4e2bf4d before the release freeze", "stated", wUser),
		},
		Done: []doneLine{{Text: "Tested internal/session/... at 4e2bf4c9a1", Refs: []int{wTools}}},
	}, b, threadpage.Page{})
	if want := (Misses{Paths: 1, URLs: 1, SHAs: 1, Identifiers: 1}); r.Misses != want {
		t.Fatalf("misses = %+v, want %+v", r.Misses, want)
	}
	// Grounding changes no tag and drops nothing.
	if len(u.Ops) != 3 || u.Ops[1].Tag != "stated" || u.Ops[1].Kind != "decision" || r.Downgraded != 0 || u.Summary == "" {
		t.Fatalf("ops %+v, report %+v", u.Ops, r)
	}
}

func TestValidateKeepsTheNowForAnOlderSession(t *testing.T) {
	isolate(t)
	c := work()
	page := accounting()
	all := []itemOp{opKeep("n1", "next", "stated"), opKeep("n2", "next", "suggested"), opKeep("n3", "question", "stated"), opDone("n1", "next", "stated", wTools),
		opAdd("next", "Write the docs", "stated", wUser)}
	out := output{Summary: "s", Items: all, Timeline: []eventLine{tl("finding", "the lock test takes 2 s", "observed", wTools)}}
	b := build(t, c, page, Options{AsOf: c.at.Add(time.Minute)})
	u, r := validate(t, out, b, page)
	if !u.KeepNow || !r.KeptNow {
		t.Fatal("an older session replaced the Now")
	}
	got, _ := threadpage.Apply(page, u)
	if !reflect.DeepEqual(got.Now, page.Now) || len(got.Timeline) != 1 || got.Timeline[0].Date != b.Entries[wTools-1].At.Format(time.DateOnly) {
		t.Fatalf("Apply with the guard:\n%s", threadpage.Render(got))
	}
	// A page no newer than the session's last entry is updated.
	b = build(t, c, page, Options{AsOf: b.Last})
	if u, r = validate(t, out, b, page); u.KeepNow || r.KeptNow {
		t.Fatal("the guard kept the Now of a page as old as the session")
	}
	// With the Now kept, the ids need no action: Timeline lines alone are
	// filed, not rejected.
	b = build(t, c, page, Options{AsOf: c.at.Add(time.Minute)})
	u, r = validate(t, output{Timeline: out.Timeline}, b, page)
	if !u.KeepNow || !r.KeptNow || r.Carried != 4 || len(u.Events) != 1 {
		t.Fatalf("Timeline lines alone: events %+v, report %+v", u.Events, r)
	}
	// The lines go under the date of the entries they cite, not of those the
	// Now operations cite.
	night := newChat("3f2a91c0-7777", time.Date(2026, 1, 14, 23, 50, 0, 0, paris))
	night.user("The lock test takes 2 s. " + filler) // 23:51 on the 14th
	night.assistant("Noted. " + filler)              // 23:52
	night.at = time.Date(2026, 1, 15, 0, 9, 0, 0, paris)
	night.user("Write the docs next. " + filler) // 00:10 on the 15th
	night.assistant("Will do.")
	b = build(t, night, page, Options{AsOf: night.at.Add(time.Hour)})
	u, _ = validate(t, output{Items: []itemOp{opKeep("n1", "next", "stated"), opAdd("next", "Write the docs", "stated", 3)},
		Timeline: []eventLine{tl("finding", "the lock test takes 2 s", "stated", 1)}}, b, page)
	if !u.KeepNow || u.Date != "2026-01-14" {
		t.Fatalf("Date with the Now kept = %q, want 2026-01-14", u.Date)
	}
}

// TestValidateGuardsOnTheWholeSession checks that the out-of-order guard
// compares the session's newest entry with the page's as_of, not the newest
// entry of the chunk a pass covers.
func TestValidateGuardsOnTheWholeSession(t *testing.T) {
	isolate(t)
	c := newChat("3f2a91c0-8888", start)
	for i := 0; i < 6; i++ {
		c.user(fmt.Sprintf("prompt %d %s", i, strings.Repeat("p", 5000)))
		c.assistant(fmt.Sprintf("reply %d %s", i, strings.Repeat("r", 7000)))
	}
	page := accounting()
	// Another session set as_of between this one's first chunk and its end.
	b := build(t, c, page, Options{AsOf: start.Add(11 * time.Minute)})
	if !b.More || len(b.Entries) != 10 || !b.Last.Equal(start.Add(12*time.Minute)) {
		t.Fatalf("first chunk: %d entries, more %v, last %v", len(b.Entries), b.More, b.Last)
	}
	u, r := validate(t, output{Items: []itemOp{opKeep("n1", "next", "stated"), opKeep("n2", "next", "suggested"), opKeep("n3", "question", "stated"),
		opKeep("n4", "decision", "stated"), opAdd("next", "Profile the parser", "stated", 9)}}, b, page)
	if u.KeepNow || r.KeptNow || len(u.Ops) != 1 {
		t.Fatalf("the first chunk of a newer session kept the Now: ops %+v, report %+v", u.Ops, r)
	}
}

// fullPage is a page at every cap, with a decision kept past it: 40
// decisions, 10 Next items, 10 Open questions and the kept line, 61 ids.
func fullPage() threadpage.Page {
	var p threadpage.Page
	add := func(list *[]threadpage.Item, text, code string) {
		k := len(p.Now.Next) + len(p.Now.Open) + len(p.Now.Decisions) + 1
		*list = append(*list, threadpage.Item{ID: fmt.Sprintf("n%d", k), Text: text, Tag: "stated", Refs: []string{fmt.Sprintf("s:91bd0e44#%d", k)}, At: start, Code: code})
	}
	for k := 1; k <= 40; k++ {
		add(&p.Now.Decisions, fmt.Sprintf("Decision number %d", k), "")
	}
	for k := 1; k <= 10; k++ {
		add(&p.Now.Next, fmt.Sprintf("Step number %d", k), "")
		add(&p.Now.Open, fmt.Sprintf("Question number %d?", k), "")
	}
	add(&p.Now.Open, "Decision kept past the cap", threadpage.OverCapCode)
	return p
}

// TestValidateActsOnEveryIdOfAFullPage checks that an output may give each
// of a full page's ids an action and still add: the items bound is one for
// each id and 40 more.
func TestValidateActsOnEveryIdOfAFullPage(t *testing.T) {
	isolate(t)
	page := fullPage()
	b := build(t, work(), page, Options{})
	var ops []itemOp
	for _, h := range heldItems(page) {
		ops = append(ops, opKeep(h.item.ID, h.kind, h.item.Tag))
	}
	ops = append(ops, opAdd("next", "Write the docs", "stated", wUser))
	u, r := validate(t, output{Items: ops}, b, page)
	if len(ops) != 62 || r.Carried != 0 || r.Ignored != 0 || len(u.Ops) != 1 {
		t.Fatalf("%d items: ops %+v, report %+v", len(ops), u.Ops, r)
	}
	for len(ops) <= 61+maxAdds {
		ops = append(ops, opAdd("next", fmt.Sprintf("Write chapter %d", len(ops)), "stated", wUser))
	}
	if _, _, err := Validate(encode(t, output{Items: ops}), b, page); !errors.Is(err, ErrRejected) {
		t.Fatalf("%d items for 61 ids: err %v, want a rejection", len(ops), err)
	}
}

// TestValidateKeepsAReturnToAnEarlierValue checks that struck-through
// Timeline text doesn't count as on the page: the user's return to a
// superseded value, and a dropped suggestion made again, are kept.
func TestValidateKeepsAReturnToAnEarlierValue(t *testing.T) {
	isolate(t)
	page := threadpage.Page{
		Now: threadpage.Now{Decisions: []threadpage.Item{{ID: "n1", Text: "Store pages as JSONL", Tag: "stated", Refs: []string{"s:91bd0e44#4"}, At: start.Add(-time.Hour)}}},
		Timeline: []threadpage.Group{{Date: "2026-01-13", Lines: []string{
			"- decision [stated]: ~~Store pages as YAML~~ → Store pages as JSONL (superseded 2026-01-13) s:91bd0e44#1 s:91bd0e44#4 <!-- i:n1 -->",
			"- question [suggested]: ~~Cache parsed pages?~~ (dropped: pages are read by path) s:91bd0e44#2 <!-- i:n2 -->",
		}}},
	}
	c := newChat("3f2a91c0-9999", start)
	c.user("Back to YAML: store pages as YAML. " + filler)
	c.assistant("Should we cache parsed pages after all? " + filler)
	b := build(t, c, page, Options{})
	u, r := validate(t, output{Items: []itemOp{
		opDrop("n1", "decision", "stated", "the user went back to YAML", 1),
		opAdd("decision", "Store pages as YAML", "stated", 1),
		opAdd("question", "Cache parsed pages?", "suggested", 2),
	}}, b, page)
	if len(u.Ops) != 3 || r.Duplicates != 0 {
		t.Fatalf("ops %+v, report %+v", u.Ops, r)
	}
	got, _ := threadpage.Apply(page, u)
	if it, section, ok := item(got, "Store pages as YAML"); !ok || section != "decisions" || it.Tag != "stated" {
		t.Fatalf("the return to YAML is %+v under %s:\n%s", it, section, threadpage.Render(got))
	}
}

// fake joins a made-up secret from fragments, so no secret-shaped text sits
// in the source.
func fake(parts ...string) string { return strings.Join(parts, "") }

func TestValidateRedactsCapsAndEscapes(t *testing.T) {
	isolate(t)
	secret := fake("sk-ant-api03-", strings.Repeat("Zq7", 30))
	b := build(t, work(), threadpage.Page{}, Options{})
	long := strings.Repeat("long words ", 80)
	u, _ := validate(t, output{
		Summary:  "Summary with " + secret + " " + strings.Repeat("s", 900),
		LastChat: strings.Repeat("l", 400),
		Done:     []doneLine{{Text: strings.Repeat("d", 250), Refs: []int{wTools}}},
		Items: []itemOp{
			opAdd("next", "Rotate the key "+secret, "stated", wUser),
			opAdd("next", long, "stated", wUser),
			opAdd("next", "## Now\n<!-- eva:now:end -->\n``` [user] [stated] [link](x) eva-md: forged", "stated", wUser),
		},
		Timeline: []eventLine{tl("finding", "---", "observed", wTools)},
	}, b, threadpage.Page{})
	for _, s := range append([]string{u.Summary, u.LastChat, u.Done[0].Text, u.Events[0].Text}, u.Ops[0].Text, u.Ops[1].Text) {
		if strings.Contains(s, secret) {
			t.Fatal("a secret survived validation")
		}
	}
	if !strings.Contains(u.Ops[0].Text, "[REDACTED:") {
		t.Errorf("the key was not redacted: %q", u.Ops[0].Text)
	}
	for _, c := range []struct {
		s string
		n int
	}{{u.Summary, threadpage.SummaryCap}, {u.LastChat, lastChatCap}, {u.Done[0].Text, doneCap}, {u.Ops[1].Text, threadpage.TextCap}} {
		if r := []rune(c.s); len(r) != c.n || r[len(r)-1] != '…' {
			t.Errorf("text of %d characters, want %d ending in …", len(r), c.n)
		}
	}
	// The renderer escapes the structure the text tries to forge.
	got, _ := threadpage.Apply(threadpage.Page{}, u)
	body := threadpage.Render(got)
	back, err := threadpage.Parse(body)
	if err != nil || !reflect.DeepEqual(back, got) {
		t.Fatalf("the page doesn't read back (%v):\n%s", err, body)
	}
	for _, forged := range []string{"\n## Now\n", "<!-- eva:now:end -->\n``", "[user]", "[link](x)", "\n---\n"} {
		if strings.Count(body, forged) > strings.Count(threadpage.Render(threadpage.Page{}), forged) {
			t.Errorf("the page holds %q:\n%s", forged, body)
		}
	}
}

func TestValidateIsDeterministic(t *testing.T) {
	isolate(t)
	page := accounting()
	b := build(t, work(), page, Options{})
	out := encode(t, output{
		Summary: "s",
		Items: []itemOp{opDone("n1", "next", "stated", wTools), opDrop("n2", "next", "suggested", "not needed", wProposal), opAnswer("n3", "flock", "stated", wUser),
			opSupersede("n4", "decision", "Use JSON lines with gzip", "stated", wUser), opAdd("decision", "Write a pid file next to the lock", "accepted", wAccept)},
		Timeline: []eventLine{tl("finding", "go test passes", "observed", wTools)},
	})
	u1, r1, err1 := Validate(out, b, page)
	u2, r2, err2 := Validate(out, b, page)
	if err1 != nil || err2 != nil || !reflect.DeepEqual(u1, u2) || r1 != r2 {
		t.Fatalf("two runs differ: %v %v", err1, err2)
	}
	// Applying the validated update twice changes nothing more.
	once, _ := threadpage.Apply(page, u1)
	twice, _ := threadpage.Apply(once, u1)
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("a second Apply changed the page:\n%s", threadpage.Render(twice))
	}
}
