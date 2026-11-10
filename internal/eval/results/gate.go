package results

import (
	"context"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
)

// Gate is one comparison of a set of rows with the accepted baseline.
type Gate struct {
	Metric string `json:"metric"`
	Stat   string `json:"stat"` // one of Stats
	// Rule is "not_worse": the rows' figure is worse than the baseline's by
	// no more than Margin; or "better": it is better by at least Margin,
	// which is then above 0.
	Rule   string  `json:"rule"`
	Margin float64 `json:"margin"`
	// Relative makes Margin a share of the baseline's figure, 0.1 for 10%,
	// in place of an amount in the metric's unit.
	Relative bool `json:"relative,omitempty"`
}

// Gates is the gates file.
type Gates struct {
	V     int    `json:"v"`
	Gates []Gate `json:"gates"`
}

// LoadGates reads the gates at file and checks them against the
// catalogue.
func LoadGates(file string, cat Catalogue) ([]Gate, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("gates: %w", err)
	}
	var g Gates
	if err := decodeStrict(data, &g); err != nil {
		return nil, fmt.Errorf("gates: %w", err)
	}
	if g.V != Version {
		return nil, fmt.Errorf("gates: its format is %d, not %d", g.V, Version)
	}
	if len(g.Gates) == 0 {
		return nil, fmt.Errorf("gates: there is no gate")
	}
	for i, gt := range g.Gates {
		if err := gt.check(cat); err != nil {
			return nil, fmt.Errorf("gates: gate %d: %w", i+1, err)
		}
		if slices.Contains(g.Gates[:i], gt) {
			return nil, fmt.Errorf("gates: gate %d repeats an earlier one", i+1)
		}
	}
	return g.Gates, nil
}

func (g Gate) check(cat Catalogue) error {
	if _, ok := cat.Metric(g.Metric); !ok {
		return fmt.Errorf("its metric is not in the catalogue")
	}
	switch {
	case !slices.Contains(Stats, g.Stat):
		return fmt.Errorf("stat must be one of %v", Stats)
	case g.Rule != "not_worse" && g.Rule != "better":
		return fmt.Errorf("rule must be not_worse or better")
	case math.IsNaN(g.Margin) || math.IsInf(g.Margin, 0) || g.Margin < 0:
		return fmt.Errorf("margin must be a number of 0 or more")
	case g.Rule == "better" && g.Margin == 0:
		return fmt.Errorf("a better rule needs a margin above 0")
	}
	return nil
}

// Verdict is what one gate came to.
type Verdict struct {
	Gate  Gate
	Holds bool
	Line  string // one line that says what was compared and why it holds or fails
}

// Evaluate evaluates every gate on rows against the baseline. Base is the
// commit of the repository at repo the baseline must still describe:
// the baseline is stale, and fails every gate of a metric, when a path the
// catalogue lists for the metric differs between the baseline's commit and
// base. Base is the commit the change the rows measure starts from, as
// StartOf finds it, and not the rows' own commit: a change the rows'
// branch makes is what the gates judge. The catalogue and the gates must
// be checked already.
func Evaluate(ctx context.Context, repo, base string, cat Catalogue, gates []Gate, rows []Row, b Baseline) []Verdict {
	stale := map[string]string{} // metric: why its baseline cannot be used, "" when it can
	verdicts := make([]Verdict, 0, len(gates))
	for _, g := range gates {
		m, _ := cat.Metric(g.Metric)
		v := Verdict{Gate: g}
		rule := g.describe(m)
		fail := func(why string) {
			v.Line = fmt.Sprintf("FAIL %s %s: %s; %s", g.Metric, g.Stat, why, rule)
		}
		cand, base0 := of(rows, g.Metric), of(b.Rows, g.Metric)
		ck, cerr := keyOf(cand)
		bk, berr := keyOf(base0)
		why, checked := stale[g.Metric]
		if !checked && len(base0) > 0 {
			switch p, err := Changed(ctx, repo, b.Commit, base, m.Paths); {
			case err != nil:
				why = "cannot tell whether the baseline is stale: " + err.Error()
			case p != "":
				why = fmt.Sprintf("the baseline is stale: %s changed between its commit %s and %s; accept a new baseline", p, short(b.Commit), short(base))
			}
			stale[g.Metric] = why
		}
		switch {
		case len(cand) == 0:
			fail("the rows hold no figure for it")
		case cerr != nil:
			fail(cerr.Error())
		case len(commitsOf(cand)) > 1:
			fail(fmt.Sprintf("the rows were measured on %d commits; a gate judges one", len(commitsOf(cand))))
		case len(base0) == 0:
			fail("the baseline holds no figure for it")
		case berr != nil:
			fail("the baseline: " + berr.Error())
		case ck != bk:
			fail(fmt.Sprintf("the rows were measured by %s, the baseline by %s", ck, bk))
		case why != "":
			fail(why)
		default:
			c, _ := Summarize(cand).Stat(g.Stat)
			a, _ := Summarize(base0).Stat(g.Stat)
			v.Holds = g.holds(m, a, c)
			word := "FAIL"
			if v.Holds {
				word = "hold"
			}
			v.Line = fmt.Sprintf("%s %s %s: %s %s against the baseline's %s; %s", word, g.Metric, g.Stat, num(c), m.Unit, num(a), rule)
		}
		verdicts = append(verdicts, v)
	}
	return verdicts
}

// allowed returns the margin as an amount, for a baseline figure of a.
func (g Gate) allowed(a float64) float64 {
	if g.Relative {
		return g.Margin * math.Abs(a)
	}
	return g.Margin
}

// holds compares the rows' figure c with the baseline's a. A figure at
// the margin holds: the comparison allows for the rounding of binary
// figures, so that 0.85 against 0.9 is worse by the 0.05 it reads as and
// not by 0.05000000000000004.
func (g Gate) holds(m Metric, a, c float64) bool {
	worse, allowed, tol := m.worse(a, c), g.allowed(a), tolerance(a, c)
	if g.Rule == "better" {
		return -worse >= allowed-tol
	}
	return worse <= allowed+tol
}

// tolerance is how far two figures computed from a and b may differ by
// rounding alone: a billionth of the larger of them, or of 1. It is far
// below any margin a gate is meant to state, and far above the rounding
// of the few operations a gate's comparison makes.
func tolerance(a, b float64) float64 {
	return 1e-9 * max(1, math.Abs(a), math.Abs(b))
}

// describe says the gate's rule in words.
func (g Gate) describe(m Metric) string {
	margin := num(g.Margin) + " " + m.Unit
	if g.Relative {
		margin = strconv.FormatFloat(g.Margin*100, 'g', 10, 64) + "% of the baseline's figure"
	}
	if g.Rule == "better" {
		return fmt.Sprintf("must be better (%s is better) by at least %s", m.Direction, margin)
	}
	return fmt.Sprintf("may be worse (%s is better) by at most %s", m.Direction, margin)
}

// num prints a figure exactly, in as few digits as tell it apart.
func num(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// Hold reports whether every verdict holds.
func Hold(verdicts []Verdict) bool {
	for _, v := range verdicts {
		if !v.Holds {
			return false
		}
	}
	return len(verdicts) > 0
}
