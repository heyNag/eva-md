package task

import (
	"errors"
	"slices"
	"strconv"

	"github.com/heyNag/eva-md/internal/eval/results"
)

// Condition is what a new chat starts with.
type Condition string

const (
	// Off: memory off, the repository alone.
	Off Condition = "off"
	// Eva: eva-md loaded, its memory built from the earlier chat.
	Eva Condition = "eva"
	// Oracle: the task's note placed at the head of the first prompt.
	Oracle Condition = "oracle"
)

// Conditions lists every condition, in a fixed order.
var Conditions = []Condition{Off, Eva, Oracle}

// Separator stands between the note and the prompt under Oracle.
const Separator = "\n\n---\n\n"

// FirstPrompt is the new chat's first prompt under the condition c: the
// prompt alone under Off and Eva, and under Oracle the note, Separator,
// then the prompt.
func FirstPrompt(t Task, c Condition) (string, error) {
	switch c {
	case Off, Eva:
		return t.Prompt, nil
	case Oracle:
		return t.Note + Separator + t.Prompt, nil
	}
	return "", errors.New("task: unknown condition")
}

// Instrument is the rows' instrument.
const Instrument = "continuation"

// Units of the rows.
const (
	UnitRatio  = "ratio" // 0 or 1
	UnitCount  = "count"
	UnitTokens = "tokens"
	UnitMs     = "ms"
)

// Measure is one figure a trial gives.
type Measure struct {
	Name      string
	Unit      string
	Direction string // "lower" or "higher": which is better
}

// Measures lists every figure of a trial, in the order of its rows.
var Measures = []Measure{
	{"solved", UnitRatio, "higher"},
	{"first_try", UnitRatio, "higher"},
	{"corrections", UnitCount, "lower"},
	{"rounds", UnitCount, "lower"},
	{"agent_events", UnitCount, "lower"},
	{"prompt_tokens", UnitTokens, "lower"},
	{"cached_tokens", UnitTokens, "lower"},
	{"output_tokens", UnitTokens, "lower"},
	{"wall_ms", UnitMs, "lower"},
	{"asked_again", UnitCount, "lower"},
	{"memory_tokens", UnitTokens, "lower"},
	{"memory_ms", UnitMs, "lower"},
}

// Metric is the name of the metric of a family, a condition and a measure.
func Metric(family string, c Condition, measure string) string {
	return Instrument + "." + family + "." + string(c) + "." + measure
}

// Outcome is one trial's figures.
type Outcome struct {
	Solved bool
	// FirstTry: it passed before any follow-up.
	FirstTry     bool
	Corrections  int
	Rounds       int
	AgentEvents  int
	PromptTokens int64 // cached included
	CachedTokens int64
	OutputTokens int64
	WallMs       int64
	// AskedAgain, when measured, is how many facts the chat was given before
	// the round that supplied them; nil when not measured.
	AskedAgain *int
	// Memory, when measured, is what the condition itself spent outside the
	// chat, such as v1's update, whose model calls the chat's stream never
	// shows; nil when not measured.
	Memory *Spend
}

// Spend is what was spent: tokens and milliseconds.
type Spend struct {
	Tokens int64
	Ms     int64
}

// Rows gives one row a figure of the trial (from 1) of task t under the
// condition c, with commit, dirty, host, host version, model and time from
// base. Asked-again and memory rows come only when measured.
func Rows(t Task, c Condition, trial int, o Outcome, base results.Row) ([]results.Row, error) {
	if !slices.Contains(Conditions, c) {
		return nil, errors.New("task: unknown condition")
	}
	if trial < 1 {
		return nil, errors.New("task: a trial is numbered from 1")
	}
	values := map[string]float64{
		"solved":        ratio(o.Solved),
		"first_try":     ratio(o.FirstTry),
		"corrections":   float64(o.Corrections),
		"rounds":        float64(o.Rounds),
		"agent_events":  float64(o.AgentEvents),
		"prompt_tokens": float64(o.PromptTokens),
		"cached_tokens": float64(o.CachedTokens),
		"output_tokens": float64(o.OutputTokens),
		"wall_ms":       float64(o.WallMs),
	}
	if o.AskedAgain != nil {
		values["asked_again"] = float64(*o.AskedAgain)
	}
	if o.Memory != nil {
		values["memory_tokens"] = float64(o.Memory.Tokens)
		values["memory_ms"] = float64(o.Memory.Ms)
	}
	scenario := t.ID + ".t" + strconv.Itoa(trial)
	var rows []results.Row
	for _, m := range Measures {
		v, ok := values[m.Name]
		if !ok {
			continue
		}
		rows = append(rows, results.Row{
			V:           results.Version,
			Metric:      Metric(t.Family, c, m.Name),
			Value:       v,
			Unit:        m.Unit,
			Commit:      base.Commit,
			Dirty:       base.Dirty,
			Instrument:  Instrument,
			Scenario:    scenario,
			Host:        base.Host,
			HostVersion: base.HostVersion,
			Model:       base.Model,
			Seed:        uint64(trial),
			Time:        base.Time,
		})
	}
	return rows, nil
}

func ratio(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// Forbid lists every value of the task's facts and every repository name,
// sorted and each once: the words a results.Store must refuse in a row.
func Forbid(t Task) []string {
	var words []string
	for _, f := range t.Facts {
		words = append(words, f.Values...)
	}
	words = append(words, t.Repos...)
	slices.Sort(words)
	return slices.Compact(words)
}

// fixedWords are the letters and digits of the fields every row of a task
// of the family holds, whatever its values: its metrics, units and
// instrument.
func fixedWords(family string) []string {
	words := []string{letters(Instrument)}
	for _, c := range Conditions {
		for _, m := range Measures {
			words = append(words, letters(Metric(family, c, m.Name)))
		}
	}
	for _, m := range Measures {
		words = append(words, letters(m.Unit))
	}
	return words
}

// CataloguePaths are the paths every continuation metric covers.
var CataloguePaths = []string{"eval/continuation/testdata", "internal/eval/continuation"}

// EvaPaths are the paths an eva metric also covers: what a chat start, a
// prompt and an update run through in v1.
var EvaPaths = []string{
	"cmd/eva-md",
	"internal/hook",
	"internal/resume",
	"internal/update",
	"internal/threadpage",
	"internal/freetext",
	"internal/knowledgepage",
	"internal/install",
}

// Catalogue is every metric Rows can give, as eval/continuation/metrics.json
// lists them.
func Catalogue() results.Catalogue {
	c := results.Catalogue{V: results.Version}
	for _, family := range Families {
		for _, cond := range Conditions {
			paths := slices.Clone(CataloguePaths)
			if cond == Eva {
				paths = append(paths, EvaPaths...)
			}
			for _, m := range Measures {
				c.Metrics = append(c.Metrics, results.Metric{Name: Metric(family, cond, m.Name), Unit: m.Unit, Direction: m.Direction, Paths: paths})
			}
		}
	}
	return c
}

// ParseCondition returns the condition named s.
func ParseCondition(s string) (Condition, error) {
	c := Condition(s)
	if !slices.Contains(Conditions, c) {
		return "", errors.New("task: unknown condition")
	}
	return c, nil
}
