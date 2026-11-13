package storetest_test

// These tests use the fake the way another package's tests would: they
// import only the store's interface, the fake and the standard library.

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/store"
	"github.com/heyNag/eva-md/internal/store/storetest"
)

// start is the fake's clock: a fixed time, never the wall clock's.
var start = time.Date(2030, 4, 5, 6, 7, 8, 0, time.UTC)

// passID is a pass id, a ULID of the clock's time and n.
func passID(at time.Time, n int) string {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	out := make([]byte, 26)
	ms := at.UnixMilli()
	for i := 9; i >= 0; i-- {
		out[i] = alphabet[ms&31]
		ms >>= 5
	}
	for i := 25; i >= 10; i-- {
		out[i] = alphabet[n&31]
		n >>= 5
	}
	return string(out)
}

func newFake(t *testing.T) *storetest.Fake {
	t.Helper()
	f, err := storetest.New(func() time.Time { return start })
	if err != nil {
		t.Fatal(err)
	}
	return f
}

const session = "claude-code:s1"

// createTxn is a pass over lines 1 to 2 of the session that creates one
// item and notes it.
func createTxn(n int, title, statement string) store.Txn {
	return store.Txn{
		Pass:    passID(start, n),
		Kind:    store.KindFast,
		Actor:   "claude-code/model-a",
		Session: session,
		Covers:  []store.Range{{From: 1, To: 2, Outcome: "done", Contract: "1"}},
		Ops: []store.Op{
			{Type: store.OpItemCreate, Authority: store.AuthorityModel, Fields: map[string]any{"kind": "fact", "title": title, "statement": statement}},
			{Type: store.OpSessionAppend, Authority: store.AuthorityModel, Fields: map[string]any{"text": "noted " + title}, Requires: []int{0}},
		},
	}
}

// item finds an item of the fake by its id.
func item(t *testing.T, f *storetest.Fake, id string) storetest.Item {
	t.Helper()
	for _, it := range f.Items() {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %s among %d", id, len(f.Items()))
	return storetest.Item{}
}

func TestAPassCreatesAnItem(t *testing.T) {
	f := newFake(t)
	before := len(f.History())
	res, err := f.Apply(createTxn(1, "the cache", "the cache stays local"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outcomes) != 2 || res.Outcomes[0].Result != store.ResultApplied || res.Outcomes[1].Result != store.ResultApplied {
		t.Fatalf("outcomes %+v", res.Outcomes)
	}
	if res.Commit == "" {
		t.Fatal("the pass made no commit")
	}
	it := item(t, f, res.Outcomes[0].ID)
	if it.Title != "the cache" || it.Statement != "the cache stays local" || it.Kind != "fact" || it.State != "active" {
		t.Errorf("the item reads %+v", it)
	}
	if _, ok := f.Files()[it.Path]; !ok {
		t.Errorf("no file at %s", it.Path)
	}
	h := f.History()
	if len(h) != before+1 || h[len(h)-1].ID != res.Commit || h[len(h)-1].Created != 1 {
		t.Errorf("history %+v after commit %s", h, res.Commit)
	}
	if path, ok := f.SessionFile(session); !ok || !strings.Contains(string(f.Files()[path]), "noted the cache") {
		t.Errorf("the session file %s (%v) holds no note", path, ok)
	}
	cov, err := f.Coverage(session)
	if err != nil {
		t.Fatal(err)
	}
	if len(cov) != 1 || cov[0].From != 1 || cov[0].To != 2 || cov[0].Pass != passID(start, 1) {
		t.Errorf("coverage %+v", cov)
	}
}

func TestTheSamePassAgainMakesNoCommit(t *testing.T) {
	f := newFake(t)
	txn := createTxn(1, "the cache", "the cache stays local")
	first, err := f.Apply(txn)
	if err != nil {
		t.Fatal(err)
	}
	files, history := f.Files(), f.History()
	again, err := f.Apply(txn)
	if err != nil {
		t.Fatal(err)
	}
	if again.Commit != first.Commit || !slices.Equal(again.Outcomes, first.Outcomes) {
		t.Errorf("the pass again gave %+v, first %+v", again, first)
	}
	if len(f.History()) != len(history) {
		t.Errorf("the pass again made a commit: %d commits, then %d", len(history), len(f.History()))
	}
	if len(f.Items()) != 1 || !sameFiles(files, f.Files()) {
		t.Errorf("the pass again changed the files: %d items", len(f.Items()))
	}
	// So does a pass again after a restart.
	if err := f.Reopen(nil); err != nil {
		t.Fatal(err)
	}
	if res, err := f.Apply(txn); err != nil || res.Commit != first.Commit || len(f.History()) != len(history) {
		t.Errorf("after a reopen the pass gave %+v, %v, with %d commits", res, err, len(f.History()))
	}
}

func sameFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for p, x := range a {
		if y, ok := b[p]; !ok || !bytes.Equal(x, y) {
			return false
		}
	}
	return true
}

func TestPlanReturnsApplysOutcomes(t *testing.T) {
	f := newFake(t)
	first, err := f.Apply(createTxn(1, "the cache", "the cache stays local"))
	if err != nil {
		t.Fatal(err)
	}
	it := item(t, f, first.Outcomes[0].ID)
	// A pass of every outcome: a change of nothing a noop, a revision
	// applied, a revision against a stale base refused, a create, an
	// operation that requires a refused one refused, and an operation on
	// an unknown id refused.
	txn := store.Txn{Pass: passID(start, 2), Kind: store.KindFast, Actor: "codex/model-b", Session: session,
		Covers: []store.Range{{From: 3, To: 5, Outcome: "done", Contract: "1"}},
		Ops: []store.Op{
			{Type: store.OpItemState, ID: it.ID, Base: it.Base, Authority: store.AuthorityModel, Fields: map[string]any{"state": "active"}},
			{Type: store.OpItemRevise, ID: it.ID, Base: it.Base, Authority: store.AuthorityModel, Fields: map[string]any{"statement": "the cache moved"}},
			{Type: store.OpItemRevise, ID: it.ID, Base: "stale", Authority: store.AuthorityModel, Fields: map[string]any{"statement": "lost"}},
			{Type: store.OpItemCreate, Authority: store.AuthorityUser, Fields: map[string]any{"kind": "rule", "title": "the hook", "statement": "the hook stays fast"}},
			{Type: store.OpSessionAppend, Authority: store.AuthorityModel, Fields: map[string]any{"text": "after a refusal"}, Requires: []int{2}},
			{Type: store.OpItemRevise, ID: "inothere000000", Base: "x", Authority: store.AuthorityModel, Fields: map[string]any{"statement": "none"}},
		}}
	files, history := f.Files(), f.History()
	plan, err := f.Plan(txn)
	if err != nil {
		t.Fatal(err)
	}
	if !sameFiles(files, f.Files()) || len(f.History()) != len(history) {
		t.Fatal("Plan wrote")
	}
	res, err := f.Apply(txn)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan, res.Outcomes) {
		t.Fatalf("Plan gave %+v, Apply %+v", plan, res.Outcomes)
	}
	want := []string{store.ResultNoop, store.ResultApplied, store.ResultRefused, store.ResultApplied, store.ResultRefused, store.ResultRefused}
	if len(res.Outcomes) != len(want) {
		t.Fatalf("%d outcomes for %d operations", len(res.Outcomes), len(want))
	}
	for i, o := range res.Outcomes {
		if o.Result != want[i] {
			t.Errorf("operation %d gave %+v, want %s", i, o, want[i])
		}
	}
	if r := res.Outcomes; r[2].Reason != store.ReasonConflict || r[4].Reason != store.ReasonRequires || r[5].Reason != store.ReasonUnknownID {
		t.Errorf("reasons %+v", r)
	}
	// Plan after the pass gives its recorded outcomes too.
	if again, err := f.Plan(txn); err != nil || !slices.Equal(again, res.Outcomes) {
		t.Errorf("Plan after Apply gave %+v, %v", again, err)
	}
}

func TestALockedItemIsRefusedAndKeepsItsBytes(t *testing.T) {
	f := newFake(t)
	first, err := f.Apply(createTxn(1, "the cache", "the cache stays local"))
	if err != nil {
		t.Fatal(err)
	}
	id := first.Outcomes[0].ID
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.HandEdit(item(t, f, id).Path, storetest.Edit{Kind: storetest.EditLock}); err != nil {
		t.Fatal(err)
	}
	if err := f.Reopen(nil); err != nil {
		t.Fatal(err)
	}
	locked := item(t, f, id)
	bytesBefore := f.Files()[locked.Path]
	if !strings.Contains(string(bytesBefore), "locked: true") {
		t.Fatalf("the hand edit did not lock the item")
	}
	// The record says when the store adopted the edit: the fake's clock.
	if !strings.Contains(string(bytes.Join(values(f.Files()), nil)), start.Format(time.RFC3339)) {
		t.Error("the adopted hand edit does not carry the fake's time")
	}
	commits := len(f.History())
	for n, authority := range []string{store.AuthorityModel, store.AuthorityUser} {
		txn := store.Txn{Pass: passID(start.Add(time.Minute), n), Kind: store.KindCorrect, Actor: "human:person",
			Ops: []store.Op{
				{Type: store.OpItemRevise, ID: id, Base: locked.Base, Authority: authority, Fields: map[string]any{"statement": "rewritten"}},
				{Type: store.OpItemState, ID: id, Base: locked.Base, Authority: authority, Fields: map[string]any{"state": "retired"}},
			}}
		res, err := f.Apply(txn)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range res.Outcomes {
			if o.Result != store.ResultRefused || o.Reason != store.ReasonLocked || o.ID != id {
				t.Errorf("with %s authority an operation on a locked item gave %+v", authority, o)
			}
		}
		if !bytes.Equal(f.Files()[locked.Path], bytesBefore) {
			t.Errorf("with %s authority the locked item's bytes changed", authority)
		}
	}
	if len(f.History()) != commits {
		t.Errorf("refused passes made %d commits", len(f.History())-commits)
	}
}

// values lists a map's values.
func values(m map[string][]byte) [][]byte {
	var out [][]byte
	for _, b := range m {
		out = append(out, b)
	}
	return out
}

func TestTheItemOutlastsReopen(t *testing.T) {
	f := newFake(t)
	first, err := f.Apply(createTxn(1, "the cache", "the cache stays local"))
	if err != nil {
		t.Fatal(err)
	}
	id := first.Outcomes[0].ID
	before := item(t, f, id)
	history := f.History()
	if err := f.Reopen(nil); err != nil {
		t.Fatal(err)
	}
	if after := item(t, f, id); after != before {
		t.Errorf("after a reopen the item reads %+v, before %+v", after, before)
	}
	if len(f.History()) != len(history) {
		t.Errorf("a reopen with nothing to recover made %d commits", len(f.History())-len(history))
	}
	cov, err := f.Coverage(session)
	if err != nil || len(cov) != 1 {
		t.Errorf("after a reopen the coverage is %+v, %v", cov, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Reopen(nil); err != nil {
		t.Fatal(err)
	}
	if item(t, f, id) != before {
		t.Error("the item changed over a close and a reopen")
	}
}

// A fault stops a pass part way, as a process's death would; Reopen
// undoes what it wrote, and the same pass then applies once.
func TestAPassStoppedByAFaultIsUndoneAtReopen(t *testing.T) {
	f := newFake(t)
	stop := errors.New("made-up death")
	if err := f.Reopen(func(s store.Step, i int) error {
		if s == store.StepWrite && i == 1 {
			return stop
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	files, history := f.Files(), f.History()
	txn := createTxn(1, "the cache", "the cache stays local")
	if _, err := f.Apply(txn); !errors.Is(err, stop) {
		t.Fatalf("Apply gave %v, want the fault's error", err)
	}
	if sameFiles(files, f.Files()) {
		t.Fatal("the pass wrote nothing before its fault")
	}
	if _, err := f.Apply(txn); !errors.Is(err, storetest.ErrStopped) {
		t.Errorf("after its fault the store gave %v", err)
	}
	if err := f.Reopen(nil); err != nil {
		t.Fatal(err)
	}
	if !sameFiles(files, f.Files()) || len(f.History()) != len(history) {
		t.Fatal("the reopen did not undo the stopped pass")
	}
	res, err := f.Apply(txn)
	if err != nil || res.Commit == "" || len(f.Items()) != 1 {
		t.Fatalf("the pass after the reopen gave %+v, %v, with %d items", res, err, len(f.Items()))
	}
}

func TestAClosedFakeRefuses(t *testing.T) {
	f := newFake(t)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Apply(createTxn(1, "the cache", "the cache stays local")); !errors.Is(err, storetest.ErrClosed) {
		t.Errorf("Apply on a closed fake gave %v", err)
	}
	if _, err := f.Plan(createTxn(1, "the cache", "the cache stays local")); !errors.Is(err, storetest.ErrClosed) {
		t.Errorf("Plan on a closed fake gave %v", err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("a second Close gave %v", err)
	}
	if _, err := storetest.New(nil); !errors.Is(err, storetest.ErrNoClock) {
		t.Errorf("New with no clock gave %v", err)
	}
}

func TestFilesAreCopies(t *testing.T) {
	f := newFake(t)
	first, err := f.Apply(createTxn(1, "the cache", "the cache stays local"))
	if err != nil {
		t.Fatal(err)
	}
	path := item(t, f, first.Outcomes[0].ID).Path
	files := f.Files()
	files[path][0] = 'X'
	delete(files, path)
	if b := f.Files()[path]; len(b) == 0 || b[0] == 'X' {
		t.Error("changing what Files returned changed the fake")
	}
}

// A lost store folder comes back from the last push, and a Reopen before
// it is restored fails and leaves the fake closed.
func TestALostStoreComesBackFromItsPush(t *testing.T) {
	f := newFake(t)
	first, err := f.Apply(createTxn(1, "the cache", "the cache stays local"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Push(); err != nil {
		t.Fatal(err)
	}
	if err := f.Lose(); !errors.Is(err, storetest.ErrBusy) {
		t.Fatalf("Lose on an open store gave %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Lose(); err != nil {
		t.Fatal(err)
	}
	if len(f.Files()) != 0 || len(f.Items()) != 0 {
		t.Fatal("a lost store still has files")
	}
	if err := f.Reopen(nil); !errors.Is(err, storetest.ErrLost) {
		t.Fatalf("Reopen of a lost store gave %v", err)
	}
	if _, err := f.Apply(createTxn(3, "the parser", "the parser reads one line")); !errors.Is(err, storetest.ErrClosed) {
		t.Errorf("Apply after a failed Reopen gave %v", err)
	}
	if err := f.Restore(); err != nil {
		t.Fatal(err)
	}
	if err := f.Reopen(nil); err != nil {
		t.Fatal(err)
	}
	if items := f.Items(); len(items) != 1 || items[0].ID != first.Outcomes[0].ID {
		t.Errorf("the restored store has %d items", len(items))
	}
	if res, err := f.Apply(createTxn(1, "the cache", "the cache stays local")); err != nil || res.Commit != first.Commit {
		t.Errorf("the pushed pass again gave %+v, %v", res, err)
	}
}
