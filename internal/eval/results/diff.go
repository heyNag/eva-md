package results

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"
)

// Diff writes two sets of rows side by side: one line for each metric and
// what measured it, in either set, with each set's count, mean, median and
// 95th percentile, and how the second set's mean compares with the
// first's. The catalogue must describe every row.
func Diff(w io.Writer, cat Catalogue, first, second []Row) error {
	for _, rows := range [][]Row{first, second} {
		if err := cat.Check(rows); err != nil {
			return err
		}
	}
	type group struct {
		metric string
		k      key
	}
	sets := [2]map[group][]Row{{}, {}}
	var groups []group
	for i, rows := range [][]Row{first, second} {
		for _, r := range rows {
			g := group{r.Metric, key{r.Instrument, r.Host, r.Model}}
			if _, ok := sets[0][g]; !ok {
				if _, ok := sets[1][g]; !ok {
					groups = append(groups, g)
				}
			}
			sets[i][g] = append(sets[i][g], r)
		}
	}
	slices.SortFunc(groups, func(a, b group) int {
		return cmp.Or(cmp.Compare(a.metric, b.metric), cmp.Compare(a.k.instrument, b.k.instrument),
			cmp.Compare(a.k.host, b.k.host), cmp.Compare(a.k.model, b.k.model))
	})
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "metric\tmeasured by\tunit\tfirst n\tmean\tmedian\tp95\tsecond n\tmean\tmedian\tp95\tsecond against first")
	for _, g := range groups {
		m, _ := cat.Metric(g.metric)
		line := fmt.Sprintf("%s\t%s\t%s", g.metric, g.k, m.Unit)
		var means [2]float64
		for i := range sets {
			rows := sets[i][g]
			if len(rows) == 0 {
				line += "\t0\t-\t-\t-"
				continue
			}
			s := Summarize(rows)
			means[i] = s.Mean
			line += fmt.Sprintf("\t%d\t%s\t%s\t%s", s.N, num(s.Mean), num(s.Median), num(s.P95))
		}
		change := "-"
		if len(sets[0][g]) > 0 && len(sets[1][g]) > 0 {
			switch worse := m.worse(means[0], means[1]); {
			case worse > 0:
				change = "worse by " + num(worse)
			case worse < 0:
				change = "better by " + num(-worse)
			default:
				change = "the same"
			}
		}
		fmt.Fprintln(tw, line+"\t"+change)
	}
	return tw.Flush()
}
