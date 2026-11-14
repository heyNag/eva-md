package conformance

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/store"
)

// fixed is a clock that stands still.
func fixed() time.Time { return time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC) }

// pass mints the n-th pass id of a test.
func pass(n byte) string {
	return passAt(fixed().Add(time.Duration(n)*time.Second), bytes.Repeat([]byte{n}, 10))
}

func create(title string) store.Op {
	return store.Op{Type: store.OpItemCreate, Authority: store.AuthorityModel, Fields: map[string]any{"kind": "fact", "title": title, "statement": "s the parser lives under bin"}}
}

// opened is a model, open, with one item made by a first pass.
func opened(t *testing.T) (*Model, store.Store, Item) {
	t.Helper()
	m := NewModel()
	st, err := m.Open(fixed, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := st.Apply(store.Txn{Pass: pass(1), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 1, To: 2, Outcome: "done", Contract: "1"}}, Ops: []store.Op{create("t1")}})
	if err != nil || res.Commit == "" || res.Outcomes[0].Result != store.ResultApplied {
		t.Fatalf("the first pass gave %+v, %v", res, err)
	}
	items := m.Items()
	if len(items) != 1 || items[0].ID != res.Outcomes[0].ID || items[0].Title != "t1" || items[0].State != "active" || items[0].Kind != "fact" {
		t.Fatalf("items: %+v", items)
	}
	return m, st, items[0]
}

// edit makes a hand edit with the store closed, and reopens it.
func edit(t *testing.T, m *Model, st store.Store, path string, e Edit) store.Store {
	t.Helper()
	st.Close()
	if err := m.HandEdit(path, e); err != nil {
		t.Fatal(err)
	}
	st, err := m.Open(fixed, nil)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func revise(it Item, authority string) store.Op {
	return store.Op{Type: store.OpItemRevise, ID: it.ID, Base: it.Base, Authority: authority, Fields: map[string]any{"statement": "s the parser moved"}}
}

func current(m *Model, path string) Item {
	for _, it := range m.Items() {
		if it.Path == path {
			return it
		}
	}
	return Item{}
}

func TestALockWithACommentRefusesEveryOperation(t *testing.T) {
	m, st, it := opened(t)
	st = edit(t, m, st, it.Path, Edit{Kind: EditLock})
	if !strings.Contains(string(m.Files()[it.Path]), "\nlocked: true  # note\n") {
		t.Fatalf("the lock was not written as a person writes it:\n%s", m.Files()[it.Path])
	}
	before := m.Files()[it.Path]
	it = current(m, it.Path)
	ops := []store.Op{
		revise(it, store.AuthorityModel), revise(it, store.AuthorityUser),
		{Type: store.OpItemState, ID: it.ID, Base: it.Base, Authority: store.AuthorityUser, Fields: map[string]any{"state": "retired"}},
		{Type: store.OpDocAcknowledge, ID: it.ID, Base: it.Base, Authority: store.AuthorityUser},
		create("t2"),
	}
	res, err := st.Apply(store.Txn{Pass: pass(2), Kind: store.KindCorrect, Ops: ops})
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range res.Outcomes[:4] {
		if o.Result != store.ResultRefused || o.Reason != store.ReasonLocked || o.ID != it.ID {
			t.Errorf("operation %d gave %+v", i, o)
		}
	}
	if res.Outcomes[4].Result != store.ResultApplied || res.Commit == "" {
		t.Errorf("the pass did not go on: %+v", res)
	}
	if !bytes.Equal(m.Files()[it.Path], before) {
		t.Error("the locked file changed")
	}
}

func TestAHandEditIsAdoptedAndHeldUntilAcknowledged(t *testing.T) {
	m, st, it := opened(t)
	commits := len(m.History())
	st = edit(t, m, st, it.Path, Edit{Kind: EditChange, Text: "s the parser was rewritten by hand"})
	h := m.History()
	if len(h) != commits+1 || h[len(h)-1].Kind != store.KindHuman || h[len(h)-1].Pass != "" {
		t.Fatalf("the hand edit was not adopted as kind human: %+v", h[len(h)-1])
	}
	if !unacknowledged(m.Files()[recordPath], it.Path) {
		t.Fatalf("the record does not hold the hand edit:\n%s", m.Files()[recordPath])
	}
	it = current(m, it.Path)
	if it.Statement != "s the parser was rewritten by hand" {
		t.Fatalf("the hand edit was not kept: %q", it.Statement)
	}
	res, _ := st.Apply(store.Txn{Pass: pass(2), Kind: store.KindFast, Ops: []store.Op{revise(it, store.AuthorityModel), {Type: store.OpDocAcknowledge, ID: it.ID, Base: it.Base, Authority: store.AuthorityModel}}})
	for _, o := range res.Outcomes {
		if o.Reason != store.ReasonHandEdit {
			t.Fatalf("a model's operation on an unacknowledged hand edit gave %+v", res.Outcomes)
		}
	}
	res, _ = st.Apply(store.Txn{Pass: pass(3), Kind: store.KindAcknowledge, Ops: []store.Op{{Type: store.OpDocAcknowledge, ID: it.ID, Base: it.Base, Authority: store.AuthorityUser}}})
	if res.Outcomes[0].Result != store.ResultApplied || unacknowledged(m.Files()[recordPath], it.Path) {
		t.Fatalf("the acknowledgement gave %+v", res.Outcomes)
	}
	res, _ = st.Apply(store.Txn{Pass: pass(4), Kind: store.KindAcknowledge, Ops: []store.Op{{Type: store.OpDocAcknowledge, ID: it.ID, Base: it.Base, Authority: store.AuthorityUser}}})
	if res.Outcomes[0].Result != store.ResultNoop || res.Commit != "" {
		t.Fatalf("a second acknowledgement gave %+v", res)
	}
	res, _ = st.Apply(store.Txn{Pass: pass(5), Kind: store.KindFast, Ops: []store.Op{revise(it, store.AuthorityModel)}})
	if res.Outcomes[0].Result != store.ResultApplied || current(m, it.Path).Statement != "s the parser moved" {
		t.Fatalf("a model's revision after the acknowledgement gave %+v", res.Outcomes)
	}
}

func TestVerifiedAndUnreadableFiles(t *testing.T) {
	m, st, it := opened(t)
	st = edit(t, m, st, it.Path, Edit{Kind: EditVerify})
	st.Apply(store.Txn{Pass: pass(2), Kind: store.KindAcknowledge, Ops: []store.Op{{Type: store.OpDocAcknowledge, ID: it.ID, Base: current(m, it.Path).Base, Authority: store.AuthorityUser}}})
	it = current(m, it.Path)
	res, _ := st.Apply(store.Txn{Pass: pass(3), Kind: store.KindFast, Ops: []store.Op{revise(it, store.AuthorityModel)}})
	if o := res.Outcomes[0]; o.Reason != store.ReasonVerified {
		t.Fatalf("a model's revision of a verified file gave %+v", o)
	}
	res, _ = st.Apply(store.Txn{Pass: pass(4), Kind: store.KindCorrect, Ops: []store.Op{revise(it, store.AuthorityUser)}})
	if o := res.Outcomes[0]; o.Result != store.ResultApplied || !strings.Contains(string(m.Files()[it.Path]), "\nverified: ") {
		t.Fatalf("the user's revision of a verified file gave %+v, or dropped the mark", o)
	}
	st = edit(t, m, st, it.Path, Edit{Kind: EditGarble})
	it = current(m, it.Path)
	res, _ = st.Apply(store.Txn{Pass: pass(5), Kind: store.KindCorrect, Ops: []store.Op{revise(it, store.AuthorityUser)}})
	if o := res.Outcomes[0]; o.Reason != store.ReasonUnreadable {
		t.Fatalf("a revision of an unreadable file gave %+v", o)
	}
}

func TestOutcomesOfEachOperation(t *testing.T) {
	m, st, it := opened(t)
	stale := it
	stale.Base = strings.Repeat("0", 64)
	ops := []store.Op{
		{Type: store.OpItemRetract, ID: it.ID, Base: it.Base, Authority: store.AuthorityUser},                                           // 0 not done by the model
		{Type: store.OpItemState, ID: it.ID, Base: it.Base, Authority: store.AuthorityModel, Fields: map[string]any{"state": "active"}}, // 1 noop
		revise(stale, store.AuthorityModel),                                     // 2 conflict
		revise(Item{ID: "i0000000000000", Base: it.Base}, store.AuthorityModel), // 3 unknown id
		{Type: store.OpSessionAppend, Authority: store.AuthorityModel, Fields: map[string]any{"text": "did a thing"}, Requires: []int{2}},                      // 4 requires
		{Type: store.OpSessionAppend, Authority: store.AuthorityModel, Fields: map[string]any{"text": "did a thing"}, Requires: []int{5}},                      // 5 invalid: not earlier
		{Type: store.OpItemCreate, Authority: "agent", Fields: map[string]any{"kind": "fact", "title": "t3", "statement": "s"}},                                // 6 invalid authority
		{Type: store.OpItemCreate, Authority: store.AuthorityModel, Fields: map[string]any{"kind": "opinion", "title": "t3", "statement": "s"}},                // 7 invalid kind
		{Type: store.OpItemState, ID: it.ID, Base: it.Base, Authority: store.AuthorityModel, Fields: map[string]any{"state": "retracted"}, Requires: []int{1}}, // 8 applied
		{Type: store.OpSessionAppend, Authority: store.AuthorityModel, Fields: map[string]any{"text": "noted"}, Requires: []int{8}},                            // 9 applied
	}
	want := []store.Outcome{
		{Result: store.ResultRefused, Reason: store.ReasonInvalid, ID: it.ID},
		{Result: store.ResultNoop, ID: it.ID},
		{Result: store.ResultRefused, Reason: store.ReasonConflict, ID: it.ID},
		{Result: store.ResultRefused, Reason: store.ReasonUnknownID, ID: "i0000000000000"},
		{Result: store.ResultRefused, Reason: store.ReasonRequires},
		{Result: store.ResultRefused, Reason: store.ReasonInvalid},
		{Result: store.ResultRefused, Reason: store.ReasonInvalid},
		{Result: store.ResultRefused, Reason: store.ReasonInvalid},
		{Result: store.ResultApplied, ID: it.ID},
		{Result: store.ResultApplied},
	}
	txn := store.Txn{Pass: pass(2), Kind: store.KindFast, Session: "codex:b2", Ops: ops}
	plan, err := st.Plan(txn)
	if err != nil || !reflect.DeepEqual(plan, want) {
		t.Fatalf("Plan gave %+v, %v", plan, err)
	}
	res, err := st.Apply(txn)
	if err != nil || !reflect.DeepEqual(res.Outcomes, want) {
		t.Fatalf("Apply gave %+v, %v", res.Outcomes, err)
	}
	c := m.History()[len(m.History())-1]
	if c.Pass != txn.Pass || c.Created != 0 || c.Revised != 2 || c.Refused != 7 || c.ID != res.Commit {
		t.Fatalf("the commit: %+v", c)
	}
	// Retracted is final.
	it = current(m, it.Path)
	res, _ = st.Apply(store.Txn{Pass: pass(3), Kind: store.KindCorrect, Ops: []store.Op{{Type: store.OpItemState, ID: it.ID, Base: it.Base, Authority: store.AuthorityUser, Fields: map[string]any{"state": "active"}}}})
	if res.Outcomes[0].Reason != store.ReasonInvalid {
		t.Fatalf("a retracted item came back: %+v", res.Outcomes)
	}
}

func TestCoverageIsInTheTree(t *testing.T) {
	m, st, _ := opened(t)
	got, err := st.Coverage("codex:b2")
	want := []store.Range{{From: 1, To: 2, Pass: pass(1), Outcome: "done", Contract: "1"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Coverage gave %+v, %v", got, err)
	}
	if !strings.Contains(string(m.Files()["sessions/codex.b2.md"]), `"from":1,"to":2`) {
		t.Fatal("the range is not in the session file")
	}
	// A pass that only covers still commits; one that covers a range
	// again does not.
	n := len(m.History())
	res, _ := st.Apply(store.Txn{Pass: pass(2), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 3, To: 4, Outcome: "skipped", Reason: "empty", Contract: "1"}}})
	if res.Commit == "" || len(m.History()) != n+1 {
		t.Fatal("a pass that only covers made no commit")
	}
	res, _ = st.Apply(store.Txn{Pass: pass(3), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 3, To: 4, Outcome: "done", Contract: "1"}}})
	if res.Commit != "" || len(m.History()) != n+1 {
		t.Fatal("a pass that covers nothing new made a commit")
	}
	if _, err := st.Apply(store.Txn{Pass: pass(4), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 2, To: 5, Outcome: "done", Contract: "1"}}}); !errors.Is(err, ErrTxn) {
		t.Fatalf("a range across covered lines gave %v", err)
	}
}

// A session file that is locked, verified or unreadable refuses a note,
// whatever its authority, and takes no coverage, so its ranges stay in the
// queue; the pass's other operations go on. A session file a hand edit
// only changed takes both.
func TestAProtectedSessionFileRefusesAppendsAndCoverage(t *testing.T) {
	for _, c := range []struct{ edit, reason string }{{EditLock, store.ReasonLocked}, {EditVerify, store.ReasonVerified}, {EditGarble, store.ReasonUnreadable}, {EditChange, ""}} {
		m, st, _ := opened(t)
		path := sessionPath("codex:b2")
		st = edit(t, m, st, path, Edit{Kind: c.edit, Text: "a line the person wrote"})
		before := m.Files()[path]
		txn := store.Txn{Pass: pass(2), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 3, To: 4, Outcome: "done", Contract: "1"}}, Ops: []store.Op{
			{Type: store.OpSessionAppend, Authority: store.AuthorityUser, Fields: map[string]any{"text": "did a thing"}},
			create("t2"),
		}}
		plan, err := st.Plan(txn)
		if err != nil {
			t.Fatal(err)
		}
		res, err := st.Apply(txn)
		if err != nil || !reflect.DeepEqual(plan, res.Outcomes) || res.Outcomes[1].Result != store.ResultApplied || res.Commit == "" {
			t.Fatalf("%s: Plan %+v, Apply %+v, %v", c.edit, plan, res, err)
		}
		cov, _ := st.Coverage("codex:b2")
		covered := slices.ContainsFunc(cov, func(r store.Range) bool { return r.From == 3 })
		last := m.History()[len(m.History())-1]
		if c.reason == "" {
			if o := res.Outcomes[0]; o.Result != store.ResultApplied || !covered || len(last.Covers) != 1 {
				t.Fatalf("a hand-edited session file gave %+v and coverage %+v", o, cov)
			}
			if b := string(m.Files()[path]); !strings.Contains(b, "a line the person wrote") || !strings.Contains(b, "- did: did a thing") {
				t.Fatalf("the session file lost the person's line or the note:\n%s", b)
			}
			st.Close()
			continue
		}
		if o := res.Outcomes[0]; o.Result != store.ResultRefused || o.Reason != c.reason {
			t.Fatalf("%s: the note gave %+v", c.edit, o)
		}
		if !bytes.Equal(m.Files()[path], before) {
			t.Fatalf("%s: the session file changed", c.edit)
		}
		if covered || len(last.Covers) != 0 {
			t.Fatalf("%s: the session took coverage: %+v, commit %+v", c.edit, cov, last.Covers)
		}
		// A note under the model's authority, alone, is refused alike and
		// makes no commit.
		n := len(m.History())
		res, err = st.Apply(store.Txn{Pass: pass(3), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 3, To: 4, Outcome: "done", Contract: "1"}}, Ops: []store.Op{
			{Type: store.OpSessionAppend, Authority: store.AuthorityModel, Fields: map[string]any{"text": "did another thing"}},
		}})
		if err != nil || res.Outcomes[0].Reason != c.reason || res.Commit != "" || len(m.History()) != n || !bytes.Equal(m.Files()[path], before) {
			t.Fatalf("%s: a model's note gave %+v, %v", c.edit, res, err)
		}
		st.Close()
	}
}

// A pass keeps the frontmatter lines of a session file that it does not
// own, as a person wrote them.
func TestAPassKeepsTheLinesOfASessionFileItDoesNotOwn(t *testing.T) {
	m, st, _ := opened(t)
	path := sessionPath("codex:b2")
	st.Close()
	b := m.folder.files[path]
	m.folder.files[path] = append([]byte("---\ntags: [kept]\n"), b[len("---\n"):]...)
	st, err := m.Open(fixed, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := st.Apply(store.Txn{Pass: pass(2), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 3, To: 4, Outcome: "done", Contract: "1"}}, Ops: []store.Op{
		{Type: store.OpSessionAppend, Authority: store.AuthorityModel, Fields: map[string]any{"text": "did a thing"}},
	}})
	if err != nil || res.Outcomes[0].Result != store.ResultApplied {
		t.Fatalf("the note gave %+v, %v", res, err)
	}
	got := string(m.Files()[path])
	if !strings.Contains(got, "\ntags: [kept]\n") || !strings.Contains(got, "- did: did a thing") {
		t.Fatalf("the pass dropped the person's line or the note:\n%s", got)
	}
	if cov, _ := st.Coverage("codex:b2"); len(cov) != 2 {
		t.Fatalf("coverage %+v", cov)
	}
}

// Two sessions never share a file: a session of a safe host and key is
// <host>.<key>.md, and any other a hash with no dot.
func TestTwoSessionsNeverShareAFile(t *testing.T) {
	if got := sessionPath("claude-code:a_1-B"); got != "sessions/claude-code.a_1-B.md" {
		t.Fatalf("a safe session is %s", got)
	}
	all := []string{"claude-code:x.y", "claude-code.x:y", "claude-code:x:y", "claude-code:x", "claude-code.x", "x", "", ":x", "claude-code:", "claude-code:" + strings.Repeat("a", 81)}
	seen := map[string]string{}
	for _, s := range all {
		p := sessionPath(s)
		if other, ok := seen[p]; ok {
			t.Errorf("%q and %q share %s", s, other, p)
		}
		seen[p] = s
		if name := strings.TrimSuffix(strings.TrimPrefix(p, "sessions/"), ".md"); strings.Count(name, ".") > 1 || strings.Contains(s, ".") && strings.Contains(name, ".") {
			t.Errorf("%q is %s", s, p)
		}
	}
	m, st, _ := opened(t)
	defer st.Close()
	if _, err := st.Apply(store.Txn{Pass: pass(2), Kind: store.KindFast, Session: "claude-code:x.y", Covers: []store.Range{{From: 1, To: 9, Outcome: "done", Contract: "1"}}}); err != nil {
		t.Fatal(err)
	}
	if cov, _ := st.Coverage("claude-code.x:y"); len(cov) != 0 {
		t.Fatalf("one session's coverage shows as another's: %+v", cov)
	}
	if _, ok := m.SessionFile("claude-code.x:y"); ok {
		t.Fatal("the other session has a file")
	}
}

// ExactlyOnce counts a pass in history only when it applied an operation
// or covered a line no pass had covered: a pass whose only range is
// covered already, or whose session file is locked, makes no commit, and
// the property holds for it.
func TestExactlyOnceCountsWhatAPassDid(t *testing.T) {
	note := fact{do: doNote, text: "s1 the hook stays local"}
	sch := &schedule{
		record: []chunk{
			{session: "codex:b2", from: 1, to: 2, facts: []fact{{do: doCreate, kind: "fact", title: "t1", text: "s2 the cache lives under bin"}}},
			{session: "codex:b2", from: 1, to: 2, facts: []fact{{do: doRevise, title: "no such title", text: "s3"}}},
			{session: "codex:b2", from: 3, to: 4, facts: []fact{note}},
		},
		events: []event{
			{what: evPass, chunk: 0},
			{what: evPass, chunk: 1},
			{what: evEdit, session: true, edit: Edit{Kind: EditLock}},
			{what: evPass, chunk: 2},
		},
	}
	if err := checkExactlyOnce(newModel, 1, sch, crashed{}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRefusesAPassThatIsNotWellFormed(t *testing.T) {
	_, st, _ := opened(t)
	for _, txn := range []store.Txn{
		{Pass: "01JD3K8Q2M7V5T9R4N6W0X1Y2", Kind: store.KindFast},
		{Pass: pass(2), Kind: "lunch"},
		{Pass: pass(2), Kind: store.KindFast, Covers: []store.Range{{From: 1, To: 1, Outcome: "done"}}},
		{Pass: pass(2), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 5, To: 6, Outcome: "done"}, {From: 6, To: 7, Outcome: "done"}}},
		{Pass: pass(2), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 5, To: 6, Outcome: "refused"}}},
		{Pass: pass(2), Kind: store.KindFast, Session: "codex:b2", Covers: []store.Range{{From: 0, To: 6, Outcome: "done"}}},
	} {
		if _, err := st.Apply(txn); !errors.Is(err, ErrTxn) {
			t.Errorf("%+v gave %v", txn, err)
		}
		if _, err := st.Plan(txn); !errors.Is(err, ErrTxn) {
			t.Errorf("Plan of %+v gave %v", txn, err)
		}
	}
}

func TestTheLockAndTheFolder(t *testing.T) {
	m, st, _ := opened(t)
	if _, err := m.Open(fixed, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second open gave %v", err)
	}
	if err := m.Lose(); !errors.Is(err, ErrBusy) {
		t.Fatalf("losing an open store gave %v", err)
	}
	st.Close()
	if _, err := st.Apply(store.Txn{Pass: pass(2), Kind: store.KindFast}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Apply after Close gave %v", err)
	}
	if err := m.Restore(); !errors.Is(err, ErrRestore) {
		t.Fatalf("restoring a store that is there gave %v", err)
	}
	m.Lose()
	if _, err := m.Open(fixed, nil); !errors.Is(err, ErrLost) {
		t.Fatalf("opening a lost store gave %v", err)
	}
	if err := m.HandEdit(itemPath("i0000000000000"), Edit{Kind: EditLock}); !errors.Is(err, ErrLost) {
		t.Fatalf("a hand edit of a lost store gave %v", err)
	}
	if err := m.Restore(); err != nil {
		t.Fatal(err)
	}
	if h := m.History(); len(h) != 1 || h[0].Kind != store.KindInit || len(m.Files()) != 1 {
		t.Fatalf("a store restored with no push is not a new one: %+v", h)
	}
}

func TestAFaultStopsTheProcess(t *testing.T) {
	m, st, it := opened(t)
	st.Close()
	st, _ = m.Open(fixed, func(s store.Step, i int) error {
		if s == store.StepStage {
			return errCrash
		}
		return nil
	})
	if _, err := st.Apply(store.Txn{Pass: pass(2), Kind: store.KindFast, Ops: []store.Op{revise(it, store.AuthorityModel)}}); !errors.Is(err, errCrash) {
		t.Fatalf("Apply gave %v", err)
	}
	if current(m, it.Path).Statement != "s the parser moved" {
		t.Fatal("the fault came before the file was written")
	}
	if _, err := st.Apply(store.Txn{Pass: pass(3), Kind: store.KindFast}); !errors.Is(err, ErrStopped) {
		t.Fatalf("a stopped store gave %v", err)
	}
	st.Close()
	if _, err := m.Open(fixed, nil); err != nil {
		t.Fatal(err)
	}
	if current(m, it.Path).Statement != it.Statement {
		t.Fatal("recovery did not put the journalled file back")
	}
}
