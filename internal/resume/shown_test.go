package resume

import (
	"context"
	"testing"
	"time"
)

// Each kind of brief says which parts it shows and how fresh its state
// was, for the hook's run record: a startup brief its rules, state and
// exchanges, stale once the chat it resumes worked after the Now was
// written; a refresh the rules or the Now that changed; a resumed chat's
// brief the rules it had not seen; a new chat with nothing to resume its
// rules alone; and an unchanged refresh nothing.
func TestBuildSaysWhatItShows(t *testing.T) {
	o := briefFixture(t)
	writeRules(t, o, "projects/resume/rules", false, "Never mock the database in tests")
	build := func(o Options) Result {
		t.Helper()
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := build(o)
	if want := (Shown{Rules: true, State: true, Exchanges: true}); r.Shown != want {
		t.Fatalf("startup %+v", r.Shown)
	}
	seen := r.NowHash
	o.Variant, o.PreviousHash, o.Slug = "refresh", seen, r.Slug
	if r := build(o); r.Shown != (Shown{}) {
		t.Fatalf("unchanged refresh %+v", r.Shown)
	}
	o.PreviousHash, _ = splitHash(seen)
	if r := build(o); r.Shown != (Shown{Rules: true}) {
		t.Fatalf("refresh of the rules %+v", r.Shown)
	}
	o.PreviousHash = "unseen+" + func() string { _, rules := splitHash(seen); return rules }()
	if r := build(o); r.Shown != (Shown{State: true}) {
		t.Fatalf("refresh of the Now %+v", r.Shown)
	}
	o.Variant, o.Host, o.Session, o.PreviousHash = "resume", "codex", "system-chat", ""
	if r := build(o); r.Shown.Rules != true || r.Shown.Exchanges || r.Shown.Stale {
		t.Fatalf("resumed chat %+v", r.Shown)
	}

	// The chat works on after the Now was written.
	o = briefFixture(t)
	capture(t, o, "claude-code", "later-chat", "main", o.Now.Add(-10*time.Minute), "Carry on with the export.")
	if r := build(o); !r.Shown.Stale || !r.Shown.State {
		t.Fatalf("stale state %+v", r.Shown)
	}

	// Another project, with nothing to resume, gets the rules for all
	// projects alone.
	o = briefFixture(t)
	writeRules(t, o, "general/rules", true, "Always run go vet before committing")
	o.Project.Key, o.Project.Slug, o.Project.Remote = "github.com__team__other", "other", "github.com/team/other"
	if r := build(o); r.Shown != (Shown{Rules: true}) || r.Found {
		t.Fatalf("new chat %+v", r.Shown)
	}
}
