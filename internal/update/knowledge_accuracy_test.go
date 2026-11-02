package update

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// The knowledge histories: lasting facts the user settles, and the words
// that must never become one. Each runs its passes as the job does: the
// batch is built with the knowledge on file as the recall index lists it,
// every output is validated against it, and the correct one is saved to a
// wiki through knowledgepage.Save before the thread page is applied.

// The project and commit of the histories' pages.
const (
	knownProject = "github.com__you__eva-md"
	knownHead    = "4e2bf4c"
)

// knowledgeHistory is one history of knowledge: its chats, the passes over
// them, the codenames the active pages hold at the end, by id, and what
// else its end must show.
type knowledgeHistory struct {
	name   string
	s      *script
	passes []knowledgePass
	active []string
	end    func(t *testing.T, w *okfadapter.Wiki, page threadpage.Page)
}

// knowledgePass is one chat's batch. before, when set, changes the wiki
// first, as a person might.
type knowledgePass struct {
	chat    int
	before  func(t *testing.T, w *okfadapter.Wiki)
	correct func(v view) output
	bad     []knowledgeBad
	kept    bool
}

// knowledgeBad is an adversarial output and the validator's expected answer
// to it.
type knowledgeBad struct {
	name   string
	output func(v view) output
	want   func(ops []knowledgepage.Op, r KnowledgeCounts) bool
}

// known is the id of the knowledge on file that holds code.
func (v view) known(code string) string {
	for _, r := range v.b.Knowledge.Rows {
		if holdsCode(r.Statement, code) {
			return r.ID
		}
	}
	v.t.Fatalf("no knowledge holds %q", code)
	return ""
}

// refused is the validator's answer to an output whose one change it
// refuses for want.
func refused(want KnowledgeCounts) func([]knowledgepage.Op, KnowledgeCounts) bool {
	return func(ops []knowledgepage.Op, r KnowledgeCounts) bool { return len(ops) == 0 && r == want }
}

func knownWiki(t *testing.T) *okfadapter.Wiki {
	t.Helper()
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "wiki")
	if err := okfadapter.Init(root); err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(root, dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func (h knowledgeHistory) run(t *testing.T) {
	w := knownWiki(t)
	var page threadpage.Page
	var asOf time.Time
	for i, ps := range h.passes {
		if ps.before != nil {
			ps.before(t, w)
		}
		rows, err := knowledgepage.ReadIndexAt(w.Root())
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("pass %d: the recall index: %v", i+1, err)
		}
		b := build(t, h.s.chat(ps.chat), page, Options{AsOf: asOf, Knowledge: KnowledgeSet{Rows: rows, Project: knownProject, Head: knownHead}})
		for _, bd := range ps.bad {
			name := fmt.Sprintf("pass %d, %s", i+1, bd.name)
			u, ops, r, err := checkOutput(encode(t, bd.output(view{t: t, b: b, p: page})), b, page)
			if err != nil || !bd.want(ops, r.Knowledge) {
				t.Errorf("%s: report %+v, ops %+v, err %v", name, r.Knowledge, ops, err)
			}
			enforceKnowledge(t, name, b, u, ops)
		}
		u, ops, r, err := checkOutput(encode(t, ps.correct(view{t: t, b: b, p: page})), b, page)
		if err != nil {
			t.Fatalf("pass %d, correct: %v", i+1, err)
		}
		if k := r.Knowledge; k.Refused+k.Stale+k.Duplicates+k.NearDuplicates+k.Ignored+k.OverCap+k.ReadOnly+k.PathsDropped != 0 || r.KeptNow != ps.kept {
			t.Errorf("pass %d, correct: knowledge report %+v, kept the Now %v", i+1, k, r.KeptNow)
		}
		enforceKnowledge(t, fmt.Sprintf("pass %d, correct", i+1), b, u, ops)
		res, err := knowledgepage.Save(w, ops, knowledgepage.SaveMeta{Project: knownProject, Commit: knownHead, Actor: "claude-code/claude-opus-5-5", Version: "test"})
		if err != nil {
			t.Fatalf("pass %d: Save: %v", i+1, err)
		}
		u.Events = withoutTrails(u.Events, res.Skipped)
		page, _ = threadpage.Apply(page, u)
		asOf = later(asOf, b.Last)
	}
	rows, err := knowledgepage.ReadIndexAt(w.Root())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the recall index: %v", err)
	}
	var codes []string
	for _, r := range rows {
		codes = append(codes, r.ID+" "+codename.FindString(r.Statement))
	}
	slices.Sort(codes)
	if !slices.Equal(codes, h.active) {
		t.Errorf("the active knowledge holds %v, want %v", codes, h.active)
	}
	if v, err := w.Validate(); err != nil || len(v.Errors)+len(v.Orphans)+len(v.BrokenLinks)+len(v.Warnings) != 0 {
		t.Errorf("okf validate: %v %+v", err, v)
	}
	if h.end != nil {
		h.end(t, w, page)
	}
}

// enforceKnowledge checks the obligations on every knowledge change the
// validator lets through, whatever the model wrote: at most 5 a pass, and
// none while the out-of-order guard holds; refs that name batch entries;
// every date written in a cited entry; a decision or constraint only on
// the user's own words, outside any paste, that hold its codename, and a
// statement under the user's tag on those words, never a proposal's; a
// fact or procedure on those or a tool result; a scope of all projects
// only on one prompt that holds the statement and says so, and a page for
// all projects narrowed only on the user's words, moved to the project;
// paths a cited entry writes, whole; and a supersede or drop only of an
// entry the input showed, of this project and nobody locked, on the user's
// own words at or after its time, which name a dropped entry, leaving its
// Timeline line.
func enforceKnowledge(t *testing.T, name string, b Batch, u threadpage.Update, ops []knowledgepage.Op) {
	t.Helper()
	if len(ops) > keepKnowledge || u.KeepNow && len(ops) > 0 {
		t.Errorf("%s: %d knowledge changes, the Now kept: %v", name, len(ops), u.KeepNow)
	}
	byRef := map[string]Numbered{}
	for _, e := range b.Entries {
		byRef[(&validator{b: b}).ref(e)] = e
	}
	for _, op := range ops {
		var row knowledgepage.Row
		changes := op.Action != knowledgepage.Add
		if changes {
			id := knowledgepage.Name(op.ID)
			i := slices.IndexFunc(b.Knowledge.Rows, func(r knowledgepage.Row) bool { return r.ID == id })
			if i < 0 || !slices.Contains(b.Knowledge.Shown, id) || b.Knowledge.Rows[i].ReadOnly || !b.Knowledge.applies(b.Knowledge.Rows[i]) {
				t.Errorf("%s: %s of %s, which the input didn't show, a person locked or another project owns", name, op.Action, id)
				continue
			}
			row = b.Knowledge.Rows[i]
		}
		// The user's own words: of every cited prompt, and, for a change,
		// of those at or after the entry's time only.
		var owns []string
		var text string
		var nums []int
		result := false
		for _, ref := range op.Refs {
			e, ok := byRef[ref]
			if !ok {
				t.Errorf("%s: %s cites %s, not in the batch", name, op.Action, ref)
				continue
			}
			nums = append(nums, slices.Index(b.Entries, e)+1)
			text += e.Text + "\n"
			switch {
			case e.Role == "user" && e.Kind == "prompt":
				mine, _ := ownAndPasted(e.Text)
				if strings.TrimSpace(mine) != "" && (!changes || !e.At.Before(row.At)) {
					owns = append(owns, mine)
				}
			case e.Role == "tool" || e.Role == "assistant" && e.report:
				result = true
			}
		}
		own := strings.Join(owns, "\n")
		saidBy := func(ok func(own string) bool) bool { return slices.ContainsFunc(owns, ok) }
		if !(&validator{b: b}).dated(op.Title+"\n"+op.Text+"\n"+op.Reason, nums) {
			t.Errorf("%s: %s writes a date no cited entry holds", name, op.Action)
		}
		code := codename.FindString(op.Text)
		switch {
		case op.Action == knowledgepage.Drop:
		case (op.Kind == knowledgepage.Decision || op.Kind == knowledgepage.Constraint) && (!userTag(op.Tag) || own == "" || code != "" && !strings.Contains(own, code)):
			t.Errorf("%s: %s %q is not on the user's own words", name, op.Kind, op.Text)
		case userTag(op.Tag) && !mentions(own, op.Text):
			t.Errorf("%s: %q carries the user's tag without the user's words", name, op.Text)
		case op.Tag == "observed" && !result:
			t.Errorf("%s: %q is observed without a result", name, op.Text)
		case op.Tag == "suggested" || op.Tag == "inferred":
			t.Errorf("%s: %q is promoted as %s", name, op.Text, op.Tag)
		case changes && row.Scope == knowledgepage.ScopeAll && !b.Rules.Outside:
			narrowed := saidBy(func(own string) bool { return narrows(own, op.Text) })
			if narrowed != (op.Scope == knowledgepage.ScopeProject) || narrowed != (op.Project == b.Knowledge.project()) {
				t.Errorf("%s: %q for all projects became %s for %q, narrowed by the user: %v", name, op.Text, op.Scope, op.Project, narrowed)
			}
		case op.Scope == knowledgepage.ScopeAll && !b.Rules.Outside && !saidBy(func(own string) bool { return mentions(own, op.Text) && everywhere(own, op.Text) }):
			t.Errorf("%s: %q is for all projects without the user saying so", name, op.Text)
		}
		for _, p := range op.Paths {
			if !slices.ContainsFunc(codeTokens(text), func(t codeToken) bool { w := trimPath(t.text); return w == p || strings.HasPrefix(w, p+"/") }) {
				t.Errorf("%s: path %q is in no cited entry", name, p)
			}
		}
		if !changes {
			continue
		}
		id := knowledgepage.Name(op.ID)
		switch {
		case own == "":
			t.Errorf("%s: %s of %s without the user's words as new as it", name, op.Action, id)
		case op.Action == knowledgepage.Drop && !saidBy(func(own string) bool { return mentions(own, row.Statement) }):
			t.Errorf("%s: drop of %s on words that don't name it", name, id)
		case !slices.ContainsFunc(u.Events, func(e threadpage.Event) bool {
			return e.Kind == "change" && e.Tag == "stated" && strings.HasPrefix(e.Text, "knowledge "+id+": ~~")
		}):
			t.Errorf("%s: %s of %s left no Timeline line", name, op.Action, id)
		}
	}
}

func knownAdd(kind, text, tag string, refs ...int) knowledgeOp {
	title := strings.TrimSpace(codename.ReplaceAllString(text, ""))
	return knowledgeOp{Action: "add", Kind: kind, Title: ptr(strings.TrimSuffix(title, ",")), Text: ptr(text), Tag: tag, Refs: refs, Scope: "project"}
}

// A fact the user states, then changes in a later chat: the page keeps the
// old statement in its History, dated, the new one is in force, and the
// thread page's Timeline records the change.
func knownSuperseded() knowledgeHistory {
	s := &script{name: "superseded"}
	s.say(0, "user", on(0, 10, 1), "For the record, the staging database is Postgres, OSPREY-4.")
	s.say(1, "user", on(2, 10, 1), "We moved the staging database to MySQL, OSPREY-9.")
	s.say(1, "assistant", on(2, 10, 2), "I suggest a read replica for the staging database, WREN-7.")
	return knowledgeHistory{name: "a fact stated, then superseded", s: s, active: []string{"k1 OSPREY-9"}, passes: []knowledgePass{{
		correct: func(v view) output {
			return output{Knowledge: []knowledgeOp{knownAdd("fact", "The staging database is Postgres, OSPREY-4", "stated", v.n("OSPREY-4"))}}
		},
		bad: []knowledgeBad{{
			name: "the fact tagged suggested",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("fact", "The staging database is Postgres, OSPREY-4", "suggested", v.n("OSPREY-4"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}, {
			name: "the fact observed with no result",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("decision", "The staging database is Postgres, OSPREY-4", "observed", v.n("OSPREY-4"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}},
	}, {
		chat: 1,
		correct: func(v view) output {
			k := knowledgeOp{ID: ptr(v.known("OSPREY-4")), Action: "supersede", Kind: "fact", Text: ptr("The staging database is MySQL, OSPREY-9"), Tag: "stated", Refs: []int{v.n("OSPREY-9")}, Scope: "project"}
			return output{Knowledge: []knowledgeOp{k}}
		},
		bad: []knowledgeBad{{
			name: "a near copy added beside the old one",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("fact", "The staging database is MySQL, OSPREY-9", "stated", v.n("OSPREY-9"))}}
			},
			want: refused(KnowledgeCounts{NearDuplicates: 1}),
		}, {
			name: "the assistant's words as the user's supersede",
			output: func(v view) output {
				k := knowledgeOp{ID: ptr(v.known("OSPREY-4")), Action: "supersede", Kind: "decision", Text: ptr("Add a read replica for the staging database, WREN-7"), Tag: "stated",
					Refs: []int{v.n("WREN-7"), v.n("OSPREY-9")}, Scope: "project"}
				return output{Knowledge: []knowledgeOp{k}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}},
	}}, end: func(t *testing.T, w *okfadapter.Wiki, page threadpage.Page) {
		pg, err := w.Read("knowledge/k1")
		if err != nil {
			t.Fatal(err)
		}
		e, err := knowledgepage.Read(pg.Concept)
		if err != nil || e.AsOf != "2026-01-15" || len(e.History) != 1 ||
			!strings.HasPrefix(e.History[0], "- ~~The staging database is Postgres, OSPREY-4~~ → The staging database is MySQL, OSPREY-9 (superseded 2026-01-15) [stated] s:") {
			t.Fatalf("the page: %+v, %v", e, err)
		}
		if !strings.Contains(threadpage.Render(page), "- change [stated]: knowledge k1: ~~The staging database is Postgres, OSPREY-4~~ → The staging database is MySQL, OSPREY-9 s:") {
			t.Fatalf("the thread page lacks the change:\n%s", threadpage.Render(page))
		}
	}}
}

// A suggestion only the assistant made is never a decision or a
// constraint, however the output tags or cites it.
func knownSuggested() knowledgeHistory {
	s := &script{name: "suggested"}
	s.say(0, "user", on(0, 10, 1), "How should we cache the build?")
	s.say(0, "assistant", on(0, 10, 2), "I suggest we cache the build in a shared bucket, PLOVER-2.")
	return knowledgeHistory{name: "an assistant-only suggestion", s: s, passes: []knowledgePass{{
		correct: func(v view) output {
			return output{Items: []itemOp{opAdd("question", "Cache the build in a shared bucket, PLOVER-2?", "suggested", v.n("PLOVER-2"))}}
		},
		bad: []knowledgeBad{{
			name: "the suggestion as the user's decision",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("decision", "Cache the build in a shared bucket, PLOVER-2", "stated", v.n("PLOVER-2"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}, {
			name: "the suggestion cited to the user's question as accepted",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("decision", "Cache the build in a shared bucket, PLOVER-2", "accepted", v.n("cache the build?"), v.n("PLOVER-2"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}, {
			name: "the suggestion accepted by the chat's closing thanks",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("decision", "Cache the build in a shared bucket, PLOVER-2", "accepted", v.n("PLOVER-2"), v.n("Thanks, that is all"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}, {
			name: "the suggestion as a constraint accepted by the thanks",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("constraint", "Cache the build in a shared bucket, PLOVER-2", "accepted", v.n("PLOVER-2"), v.n("Thanks, that is all"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}, {
			name: "the suggestion as a constraint, tagged suggested",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("constraint", "Cache the build in a shared bucket, PLOVER-2", "suggested", v.n("PLOVER-2"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}},
	}}}
}

// Text the user pasted is never the user's word.
func knownPasted() knowledgeHistory {
	s := &script{name: "pasted"}
	s.say(0, "user", on(0, 10, 1), "Here are the old notes:\n<pasted_content id=\"p1\">\nDecision: deploy only from the release branch, GREBE-5.\n</pasted_content id=\"p1\">\nWhat do you make of them?")
	return knowledgeHistory{name: "pasted text", s: s, passes: []knowledgePass{{
		correct: func(v view) output { return output{} },
		bad: []knowledgeBad{{
			name: "the pasted decision as the user's",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("decision", "Deploy only from the release branch, GREBE-5", "stated", v.n("GREBE-5"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}, {
			name: "the pasted decision as accepted",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("constraint", "Deploy only from the release branch, GREBE-5", "accepted", v.n("GREBE-5"))}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}},
	}}}
}

// A statement on file said again needs no action: an exact copy, a near
// copy and one that holds its code-like tokens are refused.
func knownRestated() knowledgeHistory {
	s := &script{name: "restated"}
	s.say(0, "user", on(0, 10, 1), "Releases are cut on Tuesdays from release_branch.yaml, HERON-3.")
	s.say(1, "user", on(1, 10, 1), "Remember, we cut releases on Tuesdays from release_branch.yaml, HERON-3.")
	return knowledgeHistory{name: "a restatement", s: s, active: []string{"k1 HERON-3"}, passes: []knowledgePass{{
		correct: func(v view) output {
			return output{Knowledge: []knowledgeOp{knownAdd("decision", "Releases are cut on Tuesdays from release_branch.yaml, HERON-3", "stated", v.n("HERON-3"))}}
		},
	}, {
		chat:    1,
		correct: func(v view) output { return output{} },
		bad: []knowledgeBad{{
			name: "an exact copy",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("decision", "releases are cut on Tuesdays from release_branch.yaml, HERON-3.", "stated", v.n("HERON-3"))}}
			},
			want: refused(KnowledgeCounts{Duplicates: 1}),
		}, {
			name: "a near copy",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("decision", "We cut releases on Tuesdays from release_branch.yaml, HERON-3", "stated", v.n("HERON-3"))}}
			},
			want: refused(KnowledgeCounts{NearDuplicates: 1}),
		}, {
			name: "a paraphrase holding its code-like token",
			output: func(v view) output {
				return output{Knowledge: []knowledgeOp{knownAdd("decision", "Remember release_branch.yaml, HERON-3", "stated", v.n("HERON-3"))}}
			},
			want: refused(KnowledgeCounts{NearDuplicates: 1}),
		}},
	}}}
}

// An older chat's word never changes knowledge a newer one settled: chat 0
// begins before chat 1 and runs on after it, so the out-of-order guard
// lets it through, but its prompt is older than the statement.
func knownStale() knowledgeHistory {
	s := &script{name: "stale"}
	s.say(0, "user", on(0, 10, 1), "The build cache lives in Redis, EGRET-1.")
	s.say(0, "user", on(0, 15, 0), "Let us tidy the scheduler logs next.")
	s.say(1, "user", on(0, 12, 0), "The build cache moved to Memcached, EGRET-2.")
	return knowledgeHistory{name: "an older chat's supersede", s: s, active: []string{"k1 EGRET-2"}, passes: []knowledgePass{{
		chat: 1,
		correct: func(v view) output {
			return output{Knowledge: []knowledgeOp{knownAdd("fact", "The build cache lives in Memcached, EGRET-2", "stated", v.n("EGRET-2"))}}
		},
	}, {
		correct: func(v view) output { return output{} },
		bad: []knowledgeBad{{
			name: "the older word supersedes the newer",
			output: func(v view) output {
				k := knowledgeOp{ID: ptr(v.known("EGRET-2")), Action: "supersede", Kind: "fact", Text: ptr("The build cache lives in Redis, EGRET-1"), Tag: "stated", Refs: []int{v.n("EGRET-1")}, Scope: "project"}
				return output{Knowledge: []knowledgeOp{k}}
			},
			want: refused(KnowledgeCounts{Stale: 1}),
		}, {
			name: "the older word drops it",
			output: func(v view) output {
				k := knowledgeOp{ID: ptr(v.known("EGRET-2")), Action: "drop", Kind: "fact", Tag: "stated", Refs: []int{v.n("EGRET-1")}, Scope: "project", Reason: ptr("the cache is in Redis")}
				return output{Knowledge: []knowledgeOp{k}}
			},
			want: refused(KnowledgeCounts{Stale: 1}),
		}, {
			name: "the older word supersedes it beside a newer thanks",
			output: func(v view) output {
				k := knowledgeOp{ID: ptr(v.known("EGRET-2")), Action: "supersede", Kind: "fact", Text: ptr("The build cache lives in Redis, EGRET-1"), Tag: "stated",
					Refs: []int{v.n("EGRET-1"), v.n("Thanks, that is all")}, Scope: "project"}
				return output{Knowledge: []knowledgeOp{k}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}, {
			name: "a newer prompt about something else drops it",
			output: func(v view) output {
				k := knowledgeOp{ID: ptr(v.known("EGRET-2")), Action: "drop", Kind: "fact", Tag: "stated", Refs: []int{v.n("scheduler logs next")}, Scope: "project", Reason: ptr("the cache is in Redis")}
				return output{Knowledge: []knowledgeOp{k}}
			},
			want: refused(KnowledgeCounts{Refused: 1}),
		}},
	}}}
}

// A page a person locks is never written: while the recall index still
// lists it as it was, the save passes over the change and the thread page
// records none; once the index says so, the validator refuses it.
func knownLocked() knowledgeHistory {
	s := &script{name: "locked"}
	s.say(0, "user", on(0, 10, 1), "The deploy script lives in scripts/deploy.sh, FINCH-6.")
	s.say(1, "user", on(1, 10, 1), "The deploy script moved to tools/deploy.sh, FINCH-8.")
	s.say(2, "user", on(2, 10, 1), "The deploy script moved again, to bin/deploy.sh, FINCH-9.")
	var locked []byte
	lock := func(t *testing.T, w *okfadapter.Wiki) {
		pg, err := w.Read("knowledge/k1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Update("knowledge/k1", pg.Hash, func(c *okfadapter.Concept) error { c.Extra["locked"] = true; return nil }, "eva-md/test"); err != nil {
			t.Fatal(err)
		}
		pg, _ = w.Read("knowledge/k1")
		locked = pg.Raw
	}
	supersede := func(code, text string) func(v view) output {
		return func(v view) output {
			k := knowledgeOp{ID: ptr(v.known("FINCH-6")), Action: "supersede", Kind: "fact", Text: ptr(text), Tag: "stated", Refs: []int{v.n(code)}, Scope: "project"}
			return output{Knowledge: []knowledgeOp{k}}
		}
	}
	return knowledgeHistory{name: "a locked page", s: s, active: []string{"k1 FINCH-6"}, passes: []knowledgePass{{
		correct: func(v view) output {
			return output{Knowledge: []knowledgeOp{knownAdd("fact", "The deploy script lives in scripts/deploy.sh, FINCH-6", "stated", v.n("FINCH-6"))}}
		},
	}, {
		chat:    1,
		before:  lock,
		correct: supersede("FINCH-8", "The deploy script lives in tools/deploy.sh, FINCH-8"),
	}, {
		chat: 2,
		before: func(t *testing.T, w *okfadapter.Wiki) {
			if _, err := knowledgepage.Refresh(w, "test", true); err != nil {
				t.Fatal(err)
			}
		},
		correct: func(v view) output { return output{} },
		bad: []knowledgeBad{{
			name:   "a supersede of the read-only page",
			output: supersede("FINCH-9", "The deploy script lives in bin/deploy.sh, FINCH-9"),
			want:   refused(KnowledgeCounts{ReadOnly: 1}),
		}},
	}}, end: func(t *testing.T, w *okfadapter.Wiki, page threadpage.Page) {
		pg, err := w.Read("knowledge/k1")
		if err != nil || string(pg.Raw) != string(locked) {
			t.Fatalf("the locked page changed: %v\n%s", err, pg.Raw)
		}
		if strings.Contains(threadpage.Render(page), "knowledge k1") {
			t.Fatalf("the thread page records a change that wasn't made:\n%s", threadpage.Render(page))
		}
	}}
}

// A constraint the user states, with the code path it is about: a path no
// cited entry writes is left out, and the brief lists the constraint.
func knownConstraint() knowledgeHistory {
	s := &script{name: "constraint"}
	s.say(0, "user", on(0, 10, 1), "Hooks must never make a network call, see internal/hook/hook.go, KITE-7.")
	add := func(paths ...string) func(v view) output {
		return func(v view) output {
			k := knownAdd("constraint", "Hooks never make a network call, KITE-7", "stated", v.n("KITE-7"))
			k.Paths = paths
			return output{Knowledge: []knowledgeOp{k}}
		}
	}
	return knowledgeHistory{name: "a constraint with its path", s: s, active: []string{"k1 KITE-7"}, passes: []knowledgePass{{
		correct: add("internal/hook/hook.go"),
		bad: []knowledgeBad{{
			name:   "paths no cited entry writes",
			output: add("internal/hook/net.go", "/etc/hook.go", "../internal/hook/hook.go"),
			want: func(ops []knowledgepage.Op, r KnowledgeCounts) bool {
				return len(ops) == 1 && len(ops[0].Paths) == 0 && r == KnowledgeCounts{Added: 1, PathsDropped: 3}
			},
		}},
	}}, end: func(t *testing.T, w *okfadapter.Wiki, page threadpage.Page) {
		rows, _ := knowledgepage.ReadIndexAt(w.Root())
		if c := knowledgepage.Constraints(rows, knownProject); len(c) != 1 || c[0].ID != "k1" || !slices.Equal(c[0].Paths, []string{"internal/hook/hook.go"}) {
			t.Fatalf("the brief's constraints: %+v", c)
		}
	}}
}

func TestAccuracyKnowledgeHistories(t *testing.T) {
	isolate(t)
	for _, h := range []knowledgeHistory{knownSuperseded(), knownSuggested(), knownPasted(), knownRestated(), knownStale(), knownLocked(), knownConstraint()} {
		t.Run(h.name, h.run)
	}
}
