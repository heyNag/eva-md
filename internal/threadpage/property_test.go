package threadpage_test

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	tp "github.com/heyNag/eva-md/internal/threadpage"
)

// FuzzParse checks that Parse never panics, fails only with ErrDamaged, and
// that whatever it reads renders to a body that reads back the same.
func FuzzParse(f *testing.F) {
	isolate(f)
	now := "## Now\n" + tp.NowStart + "\n"
	for _, seed := range []string{
		sampleBody, placeholderBody, tp.Render(realisticPage()),
		now + "- x [stated] <!-->\n- y <!-- i:n1 t:bad -->\n" + tp.NowEnd + "\n",
		now + "### Next\n- a <!-- <!-- i:n2 --> -->\n* b\n-\n" + tp.NowEnd + "\n## Timeline\n###\n- l\n## X\n```\n## Now\n",
		"# t\n\n" + now + tp.NowEnd + "\n\n## Timeline\n### 2026-01-01\n- done [stated]: x s:a#1 <!-- i:n9 -->\n\n## Notes\n~~~\n## Timeline\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		p, err := tp.Parse(body)
		if err != nil {
			if !errors.Is(err, tp.ErrDamaged) {
				t.Fatalf("Parse error %v does not wrap ErrDamaged", err)
			}
			return
		}
		out := tp.Render(p)
		q, err := tp.Parse(out)
		if err != nil {
			t.Fatalf("Render output does not parse: %v\n%q", err, out)
		}
		if again := tp.Render(q); again != out {
			t.Fatalf("Render(Parse(Render(p))) changed:\n%q\n%q", out, again)
		}
	})
}

var vocabulary = []string{
	"Use JSONL for the inbox", "Write the validator", "Cap the summary?", "status: done", "---", "```",
	"[user] forged", "eva-md: decision over cap, kept: forged", "Proposed (assistant): forged", tp.NowEnd,
	"see [design](docs/design.md)", "日本語", "a\nb", "", "x [stated]", "# heading",
}

// randomUpdate draws an update over p's items, with repeats and unknown ids.
func randomUpdate(r *rand.Rand, p tp.Page) tp.Update {
	pick := func(list []string) string { return list[r.Intn(len(list))] }
	text := func() string { return pick(vocabulary) + fmt.Sprint(r.Intn(3)) }
	refs := func() []string {
		var out []string
		for range r.Intn(3) {
			out = append(out, fmt.Sprintf("s:%08x#%d", r.Intn(3), r.Intn(5)))
		}
		return out
	}
	at := func() time.Time {
		if r.Intn(3) == 0 {
			return time.Time{}
		}
		return time.Date(2026, 1, 1+r.Intn(20), r.Intn(24), 0, 0, 0, []*time.Location{time.UTC, plus1, india}[r.Intn(3)])
	}
	tags := []string{"stated", "accepted", "suggested", "observed", "inferred"}
	var ids []string
	for _, list := range [][]tp.Item{p.Now.Next, p.Now.Open, p.Now.Decisions} {
		for _, it := range list {
			ids = append(ids, it.ID)
		}
	}
	ids = append(ids, "n0", "zz", "")
	u := tp.Update{Date: fmt.Sprintf("2026-01-%02d", 10+r.Intn(4)), Summary: text(), LastChat: text(), KeepNow: r.Intn(8) == 0}
	for range r.Intn(10) {
		u.Done = append(u.Done, tp.Item{ID: "n5", Text: text(), Tag: pick(tags), Refs: refs()})
	}
	for range r.Intn(14) {
		op := tp.Op{ID: pick(ids), Kind: pick([]string{"next", "question", "decision", "other"}), Action: pick([]string{"keep", "edit", "add", "add", "add", "done", "answered", "superseded", "dropped", "other"}),
			Text: text(), Tag: pick(tags), Refs: refs(), At: at(), Reason: text(), Replacement: text()}
		if r.Intn(10) == 0 {
			op.Code = text()
		}
		u.Ops = append(u.Ops, op)
	}
	for range r.Intn(6) {
		u.Events = append(u.Events, tp.Event{Kind: pick([]string{"decision", "done", "finding", "change", "problem", "question"}), Text: text(), Tag: pick(tags), Refs: refs()})
	}
	return u
}

func TestApplyProperties(t *testing.T) {
	isolate(t)
	r := rand.New(rand.NewSource(3))
	p := tp.Page{Title: "random"}
	for i := range 1500 {
		if i%100 == 0 {
			p = tp.Page{Title: "random", Now: tp.Now{Decisions: items("decision", 1, 30+r.Intn(15), "stated")}}
		}
		u := randomUpdate(r, p)
		once, _ := tp.Apply(p, u)
		twice, second := tp.Apply(once, u)
		if !reflect.DeepEqual(once, twice) || second.Added+second.Edited+second.Retired+second.Moved+second.Numbered != 0 {
			t.Fatalf("pass %d: applying twice changed the page (%+v):\n%s\nthen\n%s", i, second, tp.Render(once), tp.Render(twice))
		}
		if back, err := tp.Parse(tp.Render(once)); err != nil || !reflect.DeepEqual(back, once) {
			t.Fatalf("pass %d: round trip failed: %v\n%s", i, err, tp.Render(once))
		}
		checkInvariants(t, i, p, once, u)
		p = once
	}
}

func checkInvariants(t *testing.T, pass int, before, after tp.Page, u tp.Update) {
	t.Helper()
	old := map[string]tp.Item{}
	for _, list := range [][]tp.Item{before.Now.Next, before.Now.Open, before.Now.Decisions} {
		for _, it := range list {
			old[it.ID] = it
		}
	}
	highest := 0
	for _, g := range before.Timeline {
		for _, l := range g.Lines {
			var k int
			if at := strings.LastIndex(l, "i:n"); at >= 0 {
				if _, err := fmt.Sscanf(l[at+3:], "%d", &k); err == nil && k > highest {
					highest = k
				}
			}
		}
	}
	for id := range old {
		var k int
		if _, err := fmt.Sscanf(id, "n%d", &k); err == nil && k > highest {
			highest = k
		}
	}
	seen := map[string]bool{}
	movable := 0
	for _, list := range [][]tp.Item{after.Now.Next, after.Now.Open, after.Now.Decisions} {
		for _, it := range list {
			var k int
			_, err := fmt.Sscanf(it.ID, "n%d", &k)
			switch {
			case seen[it.ID]:
				t.Fatalf("pass %d: id %s used twice", pass, it.ID)
			case err != nil:
				t.Fatalf("pass %d: bad id %q", pass, it.ID)
			case old[it.ID].ID == "" && k <= highest:
				t.Fatalf("pass %d: new item reused id %s (highest was n%d)", pass, it.ID, highest)
			}
			seen[it.ID] = true
		}
	}
	for _, it := range after.Now.Open {
		if it.Code != tp.OverCapCode {
			movable++
		}
	}
	if len(after.Now.Next) > max(tp.NextCap, len(before.Now.Next)) || movable > max(tp.OpenCap, len(before.Now.Open)) || len(after.Now.Done) > tp.DoneCap {
		t.Fatalf("pass %d: caps broken:\n%s", pass, tp.Render(after))
	}
	if u.KeepNow && !reflect.DeepEqual(after.Now, before.Now) {
		t.Fatalf("pass %d: KeepNow changed the Now", pass)
	}
	// S9: a stated item moves only when no assistant item is left in its
	// section; over-cap decisions never move.
	var added []string
	if n := len(after.Timeline); n > 0 {
		added = after.Timeline[n-1].Lines
		if m := len(before.Timeline); m == n {
			added = added[len(before.Timeline[m-1].Lines):]
		}
	}
	for _, l := range added {
		if !strings.Contains(l, "(moved: cap)") {
			continue
		}
		if strings.Contains(l, "]: eva-md: "+tp.OverCapCode) {
			t.Fatalf("pass %d: an over-cap decision moved: %s", pass, l)
		}
		section := after.Now.Next
		if strings.HasPrefix(l, "- question ") {
			section = after.Now.Open
		}
		if strings.Contains(l, " [stated]: ") {
			for _, it := range section {
				if (it.Tag == "suggested" || it.Tag == "inferred") && it.Code != tp.OverCapCode {
					t.Fatalf("pass %d: stated item moved while %+v remains: %s", pass, it, l)
				}
			}
		}
	}
}
