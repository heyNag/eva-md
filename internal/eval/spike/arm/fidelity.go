package arm

import (
	"fmt"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/eval/spike"
)

// userAuthors are the authors of a user's own entries, typed or chosen, as
// the record names them.
var userAuthors = []string{"typed", "chosen"}

// adds reports an operation that adds a record to the store.
func adds(op string) bool {
	return slices.Contains([]string{"item.add", "item.supersede", "next.add", "ask.add", "note"}, op)
}

// apply applies v to the side's store, and gives, for each operation of v
// that added a record, the record's place in the store's items. It keeps
// the places of those of the user's authority.
func (s *store) apply(sess scenario.Session, v spike.Verdict) (map[int]int, error) {
	k := len(s.st.Items())
	if err := s.st.Apply(sess, v); err != nil {
		return nil, err
	}
	made := map[int]int{}
	for i, j := range v.Ops {
		if !j.Accepted() || !adds(j.Op.Op) {
			continue
		}
		made[i] = k
		if j.Tag == spike.TagStated || j.Tag == spike.TagAccepted {
			s.kept = append(s.kept, k)
		}
		k++
	}
	if k != len(s.st.Items()) {
		return nil, fmt.Errorf("the store holds %d records, not the %d its operations added", len(s.st.Items()), k)
	}
	return made, nil
}

// refuse keeps, as an item alone, each operation v refused that adds a
// record and cites a user's entry the pass shows.
func refuse(s *store, v spike.Verdict, in spike.Input, at map[int64]time.Time) {
	for _, j := range v.Ops {
		if j.Accepted() {
			continue
		}
		if it, ok := solo(j.Op, in, at); ok {
			s.refused = append(s.refused, it)
		}
	}
}

// solo is op exported alone as an item of the user's own words, stated,
// citing the entries it names that in shows; ok is false for an operation
// that adds no record or cites no user's entry that in shows. Its kind is
// as export gives a record's: a next step next, a question open, and a
// note a dated line under the day of its newest entry.
func solo(op spike.Op, in spike.Input, at map[int64]time.Time) (score.Item, bool) {
	if !adds(op.Op) {
		return score.Item{}, false
	}
	var sources []score.Source
	user := false
	var newest time.Time
	for _, sp := range op.Ev {
		i := slices.IndexFunc(in.Entries, func(e spike.Entry) bool { return e.N == sp.E })
		if i < 0 {
			continue
		}
		e := in.Entries[i]
		user = user || slices.Contains(userAuthors, string(e.Author))
		if src := (score.Source{Session: e.Session, Seq: e.N}); !slices.Contains(sources, src) {
			sources = append(sources, src)
		}
		if t := at[e.N]; t.After(newest) {
			newest = t
		}
	}
	if !user {
		return score.Item{}, false
	}
	it := score.Item{State: score.StateActive, Authority: score.TagStated, Sources: sources}
	if op.Text != nil {
		it.Text = *op.Text
	}
	switch op.Op {
	case "next.add":
		it.Kind = scenario.KindNext
	case "ask.add":
		it.Kind = scenario.KindOpen
	case "note":
		it.Kind, it.State, it.Date = scenario.KindLog, score.StateLog, newest.Format(time.DateOnly)
	default:
		if op.Kind != nil {
			it.Kind = scenario.Kind(*op.Kind)
		}
	}
	return it, true
}

// export is a store's records as the scorer reads them: an item of kind k
// is a k, a next step next, a question open, a note a dated line under its
// day; a retired record keeps its kind with state retired; its tag is its
// authority and its cited entries its sources.
func export(items []spike.Item) score.Export {
	ex := score.Export{Items: make([]score.Item, 0, len(items))}
	for _, it := range items {
		x := score.Item{Kind: scenario.Kind(it.Kind), State: score.StateActive, Authority: it.Tag, Text: it.Text}
		switch {
		case it.State == spike.StateLog:
			x.Kind, x.State, x.Date = scenario.KindLog, score.StateLog, it.Day
		case it.Kind == spike.KindNext:
			x.Kind = scenario.KindNext
		case it.Kind == spike.KindQuestion:
			x.Kind = scenario.KindOpen
		}
		if it.State == spike.StateRetired {
			x.State = score.StateRetired
		}
		for _, src := range it.Sources {
			x.Sources = append(x.Sources, score.Source{Session: src.Session, Seq: src.N})
		}
		ex.Items = append(ex.Items, x)
	}
	return ex
}

// score scores the side's store against sc: its fidelity, the facts that
// must hold among required, and honest_refused's two counts.
func (sd *Side) score(sc scenario.Scenario, s *store, required []int) {
	sd.Items = s.st.Items()
	sd.Fidelity = score.Score(sc, export(sd.Items))
	sd.Required = len(required)
	for _, f := range required {
		switch sd.Fidelity.Facts[f].Outcome {
		case score.Holds:
			sd.Held++
			sd.Recalled++
		case score.Found:
			sd.Recalled++
		}
	}
	for _, it := range s.refused {
		if score.Score(sc, score.Export{Items: []score.Item{it}}).Items[0].Outcome == score.Supports {
			sd.Honest++
		}
	}
	sd.HonestOf = sd.Honest
	for _, k := range s.kept {
		if sd.Fidelity.Items[k].Outcome == score.Supports {
			sd.HonestOf++
		}
	}
}

// times are the times of the events of sess, by seq.
func times(sess scenario.Session) (map[int64]time.Time, error) {
	events, err := scenario.Events(sess)
	if err != nil {
		return nil, fmt.Errorf("arm: %w", err)
	}
	at := map[int64]time.Time{}
	for _, e := range events {
		t, err := time.Parse(time.RFC3339Nano, e.T)
		if err != nil {
			return nil, fmt.Errorf("arm: event %d has no time: %w", e.Seq, err)
		}
		at[e.Seq] = t
	}
	return at, nil
}
