package results

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
)

// Metric is one entry of the catalogue.
type Metric struct {
	Name      string `json:"name"`
	Unit      string `json:"unit"`
	Direction string `json:"direction"` // "lower" or "higher": which is better
	// Paths are the repository's files and folders, relative to its root
	// in slash form, whose change makes a stored baseline of the metric
	// stale.
	Paths []string `json:"paths"`
}

// Catalogue is every metric a row may carry.
type Catalogue struct {
	V       int      `json:"v"`
	Metrics []Metric `json:"metrics"`
}

// LoadCatalogue reads and checks the catalogue at file.
func LoadCatalogue(file string) (Catalogue, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Catalogue{}, fmt.Errorf("catalogue: %w", err)
	}
	var c Catalogue
	if err := decodeStrict(data, &c); err != nil {
		return Catalogue{}, fmt.Errorf("catalogue: %w", err)
	}
	return c, c.check()
}

func (c Catalogue) check() error {
	if c.V != Version {
		return fmt.Errorf("catalogue: its format is %d, not %d", c.V, Version)
	}
	if len(c.Metrics) == 0 {
		return fmt.Errorf("catalogue: it lists no metric")
	}
	seen := map[string]bool{}
	for i, m := range c.Metrics {
		switch {
		case !idPattern.MatchString(m.Name):
			return fmt.Errorf("catalogue: metric %d: name is not an id", i+1)
		case seen[m.Name]:
			return fmt.Errorf("catalogue: %s is listed twice", m.Name)
		case !idPattern.MatchString(m.Unit):
			return fmt.Errorf("catalogue: %s: unit is not an id", m.Name)
		case m.Direction != "lower" && m.Direction != "higher":
			return fmt.Errorf("catalogue: %s: direction must be lower or higher", m.Name)
		case len(m.Paths) == 0:
			return fmt.Errorf("catalogue: %s lists no path, so its baseline could never go stale", m.Name)
		}
		for _, p := range m.Paths {
			if !repoPath(p) {
				return fmt.Errorf("catalogue: %s: a path is not a clean relative path in the repository", m.Name)
			}
		}
		seen[m.Name] = true
	}
	return nil
}

// repoPath reports whether p is a clean, relative, slash-separated path
// inside a repository, other than its root.
func repoPath(p string) bool {
	return p != "" && p != "." && path.Clean(p) == p && !path.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../") &&
		!strings.ContainsAny(p, "\\\x00\n") && !strings.HasPrefix(p, ":") && !strings.HasPrefix(p, "-")
}

// Metric returns the metric named name, and whether the catalogue has it.
func (c Catalogue) Metric(name string) (Metric, bool) {
	i := slices.IndexFunc(c.Metrics, func(m Metric) bool { return m.Name == name })
	if i < 0 {
		return Metric{}, false
	}
	return c.Metrics[i], true
}

// Check returns an error when a row names a metric the catalogue lacks, or
// gives it another unit.
func (c Catalogue) Check(rows []Row) error {
	for i, r := range rows {
		m, ok := c.Metric(r.Metric)
		if !ok {
			return fmt.Errorf("row %d: %s is not in the catalogue", i+1, r.Metric)
		}
		if r.Unit != m.Unit {
			return fmt.Errorf("row %d: %s is in %s, not the catalogue's %s", i+1, r.Metric, r.Unit, m.Unit)
		}
	}
	return nil
}

// worse returns how much worse b is than a for the metric: positive when
// worse, negative when better.
func (m Metric) worse(a, b float64) float64 {
	if m.Direction == "lower" {
		return b - a
	}
	return a - b
}
