package results

import (
	"cmp"
	"math"
	"slices"
)

// Draws is how many bootstrap resamples an interval takes, and Seed the
// seed they are drawn with. Both are fixed, so the same rows always give
// the same interval, on any machine and any Go release.
const (
	Draws        = 10000
	Seed  uint64 = 0x65766120657661 // fixed; changing it changes every interval
)

// Summary is what a set of one metric's rows comes to. Every figure but
// the interval is exact: computed from the values alone, in an order that
// does not depend on the order of the rows. The interval is the central
// 95% of the means of Draws resamples of the scenarios, drawn with Seed.
type Summary struct {
	N                 int
	Mean, Median, P95 float64
	Min, Max          float64
	CILow, CIHigh     float64
	Scenarios         int
}

// Stats names the figures of a Summary a gate may compare.
var Stats = []string{"mean", "median", "p95", "min", "max", "ci_low", "ci_high"}

// Stat returns the figure name names, and whether there is one.
func (s Summary) Stat(name string) (float64, bool) {
	switch name {
	case "mean":
		return s.Mean, true
	case "median":
		return s.Median, true
	case "p95":
		return s.P95, true
	case "min":
		return s.Min, true
	case "max":
		return s.Max, true
	case "ci_low":
		return s.CILow, true
	case "ci_high":
		return s.CIHigh, true
	}
	return 0, false
}

// Summarize summarizes rows, which hold one metric. It returns the zero
// Summary for no rows.
func Summarize(rows []Row) Summary {
	return summarize(rows, Seed, Draws)
}

func summarize(rows []Row, seed uint64, draws int) Summary {
	if len(rows) == 0 {
		return Summary{}
	}
	values := make([]float64, len(rows))
	byScenario := map[string][]float64{}
	for i, r := range rows {
		values[i] = r.Value
		byScenario[r.Scenario] = append(byScenario[r.Scenario], r.Value)
	}
	slices.Sort(values)
	n := len(values)
	s := Summary{N: n, Min: values[0], Max: values[n-1], Scenarios: len(byScenario)}
	s.Mean = sum(values) / float64(n)
	if n%2 == 1 {
		s.Median = values[n/2]
	} else {
		s.Median = (values[n/2-1] + values[n/2]) / 2
	}
	// The nearest rank: the smallest value at least 95% of the values are
	// at or below.
	s.P95 = values[int(math.Ceil(0.95*float64(n)))-1]

	// The bootstrap resamples scenarios, not rows, since the trials of one
	// scenario are not independent of each other. Each scenario is reduced
	// to its sum and count once, in the order of the scenarios' names.
	type cluster struct {
		sum   float64
		count int
	}
	names := make([]string, 0, len(byScenario))
	for name := range byScenario {
		names = append(names, name)
	}
	slices.Sort(names)
	clusters := make([]cluster, len(names))
	for i, name := range names {
		v := byScenario[name]
		slices.Sort(v)
		clusters[i] = cluster{sum(v), len(v)}
	}
	means := make([]float64, draws)
	g := splitmix{seed}
	k := uint64(len(clusters))
	for d := range means {
		var total float64
		var count int
		for range clusters {
			c := clusters[g.below(k)]
			total += c.sum
			count += c.count
		}
		means[d] = total / float64(count)
	}
	slices.SortFunc(means, cmp.Compare[float64])
	s.CILow = means[int(math.Floor(0.025*float64(draws)))]
	s.CIHigh = means[int(math.Ceil(0.975*float64(draws)))-1]
	return s
}

// sum adds values in their order.
func sum(values []float64) float64 {
	var t float64
	for _, v := range values {
		t += v
	}
	return t
}

// splitmix is SplitMix64 (Steele, Lea and Flood, 2014), written out here so
// that a seed gives the same draws whatever Go's own generators do.
type splitmix struct{ state uint64 }

func (g *splitmix) next() uint64 {
	g.state += 0x9e3779b97f4a7c15
	z := g.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// below returns a uniform draw from [0, n), n > 0, rejecting the few values
// that would favour the low numbers.
func (g *splitmix) below(n uint64) uint64 {
	threshold := -n % n // 2^64 mod n
	for {
		if x := g.next(); x >= threshold {
			return x % n
		}
	}
}
