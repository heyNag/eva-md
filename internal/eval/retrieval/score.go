package retrieval

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
)

// The measures, for each half and over each split and all events.
const (
	Precision      = "precision"       // relevant additions, of all additions
	Recall         = "recall"          // relevant items added, of all relevant items
	FalseAdditions = "false_additions" // additions not relevant, of all additions
	SilencePos     = "silence_pos"     // positive events with no addition, of positive events
	SilenceNeg     = "silence_neg"     // other events with no addition, of other events
	FalseEvents    = "false_events"    // events of a negative class, or of all of them, with any addition, of those events
)

// Negative is the group of every event of a half whose class is not
// positive, over which FalseEvents is also given: a figure over more events
// than any one class holds.
const Negative = "negative"

// Measures are the measures of a split, in the order they are reported.
var Measures = []string{Precision, Recall, FalseAdditions, SilencePos, SilenceNeg}

// Groups are what the measures are taken over: each split, then all.
var Groups = append(slices.Clone(Splits), "all")

// Instrument and Unit are what every row says it is.
const (
	Instrument = "retrieval"
	Unit       = "ratio"
)

// Metric returns a figure's metric: retrieval.<half>.<split, all or
// class>.<measure>.
func Metric(half, group, measure string) string {
	return "retrieval." + half + "." + group + "." + measure
}

// Metrics returns every metric the scorer can give, in the order it gives
// them; a figure with no events to count is left out of a run.
func Metrics() []string {
	var out []string
	for _, half := range Halves {
		for _, g := range Groups {
			for _, m := range Measures {
				out = append(out, Metric(half, g, m))
			}
		}
		for _, c := range NegativeClasses {
			out = append(out, Metric(half, c, FalseEvents))
		}
		out = append(out, Metric(half, Negative, FalseEvents))
	}
	return out
}

// Figure is one measure: a count of a number of things.
type Figure struct {
	Metric   string
	Num, Den int
}

// Value is the figure as a ratio.
func (f Figure) Value() float64 { return float64(f.Num) / float64(f.Den) }

// Bound is the 95% upper bound of a rate seen zero times in Den: 1 -
// 0.05^(1/Den). Zero in 160 is under 1.9%.
func Bound(den int) float64 { return 1 - math.Pow(0.05, 1/float64(den)) }

// Result is a system's run on a set: what it added for each event, and the
// figures.
type Result struct {
	System  string
	Version int // the set's
	Seed    uint64
	Events  []Event
	Added   map[string][]string // by event id
	Figures []Figure
}

// Score asks the system for each event of the set, in order, and measures
// what it added. An addition must be an item of the set, once; anything
// else, an event id given twice, and any error stop the run, naming the
// event.
func Score(ctx context.Context, s *Set, name string, sys System) (*Result, error) {
	items := map[string]bool{}
	for _, it := range s.Items {
		items[it.ID] = true
	}
	r := &Result{System: name, Version: s.Version, Seed: s.Seed, Events: s.Events, Added: map[string][]string{}}
	for _, e := range s.Events {
		if _, twice := r.Added[e.ID]; twice {
			return nil, fmt.Errorf("retrieval: event %s is in the set twice", e.ID)
		}
		added, err := sys.Add(ctx, question(e, sys))
		if err != nil {
			return nil, fmt.Errorf("retrieval: %s failed on event %s: %w", name, e.ID, err)
		}
		for i, id := range added {
			if !items[id] || slices.Contains(added[:i], id) {
				return nil, fmt.Errorf("retrieval: %s added to event %s an item that is not in the set, or one twice", name, e.ID)
			}
		}
		r.Added[e.ID] = slices.Clone(added)
	}
	r.Figures = measure(s.Events, r.Added)
	return r, nil
}

// question returns what a system is asked for an event: its id, project,
// first and text, with its labels (split, class, relevant items and
// decoys) cleared, so that no system under test can read the answers. Only
// a test arm of this package that reads labels, the oracle, is given them,
// as copies, so that it cannot change the set.
func question(e Event, sys System) Event {
	if _, ok := sys.(labelReader); ok {
		e.Relevant, e.Decoys = slices.Clone(e.Relevant), slices.Clone(e.Decoys)
		return e
	}
	e.Split, e.Class, e.Relevant, e.Decoys = "", "", nil, nil
	return e
}

// counts are what the measures of one group are made of.
type counts struct {
	added, relevantAdded, relevant int
	pos, posSilent, neg, negSilent int
}

// measure counts the figures, leaving out a ratio with no denominator.
func measure(events []Event, added map[string][]string) []Figure {
	var out []Figure
	for _, half := range Halves {
		groups := map[string]*counts{}
		for _, g := range Groups {
			groups[g] = &counts{}
		}
		classes := map[string][2]int{} // class, or Negative: events with an addition, events
		for _, e := range events {
			if Half(e.ID) != half {
				continue
			}
			got := added[e.ID]
			hits := 0
			for _, id := range got {
				if slices.Contains(e.Relevant, id) {
					hits++
				}
			}
			for _, c := range []*counts{groups[e.Split], groups["all"]} {
				c.added += len(got)
				c.relevantAdded += hits
				c.relevant += len(e.Relevant)
				if e.Class == Positive {
					c.pos++
					if len(got) == 0 {
						c.posSilent++
					}
				} else {
					c.neg++
					if len(got) == 0 {
						c.negSilent++
					}
				}
			}
			if e.Class != Positive {
				for _, k := range []string{e.Class, Negative} {
					n := classes[k]
					if len(got) > 0 {
						n[0]++
					}
					n[1]++
					classes[k] = n
				}
			}
		}
		add := func(group, measure string, num, den int) {
			if den > 0 {
				out = append(out, Figure{Metric(half, group, measure), num, den})
			}
		}
		for _, g := range Groups {
			c := groups[g]
			add(g, Precision, c.relevantAdded, c.added)
			add(g, Recall, c.relevantAdded, c.relevant)
			add(g, FalseAdditions, c.added-c.relevantAdded, c.added)
			add(g, SilencePos, c.posSilent, c.pos)
			add(g, SilenceNeg, c.negSilent, c.neg)
		}
		for _, class := range append(slices.Clone(NegativeClasses), Negative) {
			add(class, FalseEvents, classes[class][0], classes[class][1])
		}
	}
	return out
}

// Scenario is the rows' scenario: the system and the set's version.
func (r *Result) Scenario() string { return r.System + ".set" + strconv.Itoa(r.Version) }

// Rows returns a row for each figure, measured at the commit, at the time.
func (r *Result) Rows(commit string, dirty bool, at time.Time) []results.Row {
	rows := make([]results.Row, 0, len(r.Figures))
	for _, f := range r.Figures {
		rows = append(rows, results.Row{
			V: results.Version, Metric: f.Metric, Value: f.Value(), Unit: Unit, Commit: commit, Dirty: dirty,
			Instrument: Instrument, Scenario: r.Scenario(), Seed: r.Seed, Time: at,
		})
	}
	return rows
}

// Table writes the figures, each as a ratio and a count, with the 95%
// upper bound of a count of zero, then the ids of each event given an item
// that is not relevant, and the ids of those items. It writes ids and
// numbers only, never an event's text.
func (r *Result) Table(w io.Writer) {
	fmt.Fprintf(w, "Retrieval by %s on the set's version %d: %d events.\n", r.System, r.Version, len(r.Events))
	width := 0
	for _, f := range r.Figures {
		width = max(width, len(f.Metric))
	}
	for _, f := range r.Figures {
		count := fmt.Sprintf("%d of %d", f.Num, f.Den)
		if f.Num == 0 {
			count += fmt.Sprintf(", under %s at 95%%", percent(Bound(f.Den)))
		}
		fmt.Fprintf(w, "%-*s  %6.4f  %s\n", width, f.Metric, f.Value(), count)
	}
	var wrong []string
	for _, e := range r.Events {
		var not []string
		for _, id := range r.Added[e.ID] {
			if !slices.Contains(e.Relevant, id) {
				not = append(not, id)
			}
		}
		if len(not) > 0 {
			wrong = append(wrong, fmt.Sprintf("  %s (%s, %s, %s): %v", e.ID, Half(e.ID), e.Split, e.Class, not))
		}
	}
	if len(wrong) == 0 {
		fmt.Fprintln(w, "No event was given an item that is not relevant.")
		return
	}
	fmt.Fprintf(w, "Events given an item that is not relevant, %d:\n", len(wrong))
	for _, l := range wrong {
		fmt.Fprintln(w, l)
	}
}

// percent prints a ratio as a percentage, rounded up to two decimals, so
// a bound is never printed below itself.
func percent(f float64) string {
	return strconv.FormatFloat(math.Ceil(f*10000)/100, 'f', 2, 64) + "%"
}
