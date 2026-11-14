package report

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// measureWords are the fixed words each measure prints as.
var measureWords = map[string]string{
	"corrections":   "corrections",
	"rounds":        "rounds",
	"agent_events":  "agent events",
	"prompt_tokens": "prompt tokens",
	"cached_tokens": "cached tokens",
	"output_tokens": "output tokens",
	"wall_ms":       "minutes",
	"memory_tokens": "memory tokens",
	"memory_ms":     "memory minutes",
}

// ended are the ways a run ends, in the order they print.
var ended = []string{drive.EndedDone, drive.EndedError, drive.CodeNoUsage, drive.CodeMemoryOff, drive.CodeHiddenFound}

// Text is the report in fixed words, ids and figures, by host, then task,
// then condition. It refuses to give a text any line of which holds a word
// the tasks forbid, matched as a results Store matches it.
func (r Report) Text() (string, error) {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	for _, h := range r.Hosts {
		p("host %s", h.ID)
		for _, x := range h.Runs {
			var ends, unjudged []string
			for _, e := range ended {
				ends = append(ends, fmt.Sprintf("%s %d", e, x.Ended[e]))
			}
			for _, c := range drive.Codes {
				unjudged = append(unjudged, fmt.Sprintf("%s %d", c, x.Unjudged[c]))
			}
			p("  runs, condition %s: records %d, ended %s; planned %d, judged %d, not run %d, unjudged %s; no model %d, tokens %d",
				x.Condition, x.Records, strings.Join(ends, ", "), x.Planned, x.Judged, x.NotRun, strings.Join(unjudged, ", "), x.NoModel, x.Tokens)
			var commits []string
			for _, c := range x.Commits {
				state := "clean"
				if c.Dirty {
					state = "dirty"
				}
				commits = append(commits, c.ID+" "+state)
			}
			p("  runs, condition %s: commits %s; host versions %s; models asked %s; models answered %s",
				x.Condition, list(commits), list(x.HostVersions), list(x.ModelsAsked), list(x.ModelsAnswered))
		}
		for _, f := range h.Tasks {
			parts := []string{fmt.Sprintf("judged %d, solved %d, solved first try %d", f.Judged, f.Solved, f.FirstTry)}
			for _, m := range measuresOf(f.Condition) {
				parts = append(parts, measureWords[m]+" "+stat(m, f.Stats[m]))
			}
			p("  task %s %s, condition %s: %s", f.Task, f.Family, f.Condition, strings.Join(parts, "; "))
		}
		for _, k := range h.Costs {
			var parts []string
			for _, m := range figures {
				parts = append(parts, measureWords[m]+" "+median(m, k.Judged, k.Medians[m]))
			}
			line := fmt.Sprintf("  cost, condition %s: judged %d; medians %s", k.Condition, k.Judged, strings.Join(parts, ", "))
			if r.Rates != nil {
				line += fmt.Sprintf("; price median %s, at %s uncached prompt, %s cached and %s output a million tokens",
					money(k.Judged, k.Price), plain(r.Rates.In), plain(r.Rates.Cached), plain(r.Rates.Out))
			}
			if k.Condition == task.Eva {
				line += fmt.Sprintf("; memory apart: memory tokens %s, memory minutes %s",
					median("memory_tokens", k.Judged, k.Medians["memory_tokens"]), median("memory_ms", k.Judged, k.Medians["memory_ms"]))
				if r.Rates != nil {
					line += fmt.Sprintf(", memory price median %s, a floor, every memory token at the uncached prompt's rate", money(k.Judged, k.MemoryPrice))
				}
			}
			p("%s", line)
		}
		for _, v := range h.Variance {
			p("  variance, %s against off: agent events %s; prompt tokens %s", v.Arm, pairs(v.AgentEvents), pairs(v.Prompt))
		}
		counts := map[string]int{}
		for _, s := range h.Screens {
			counts[s.Verdict]++
			p("  screen, task %s %s: %s", s.Task, s.Family, s.Verdict)
		}
		p("  screens: passed %d, failed %d, too few %d, none %d", counts[ScreenPass], counts[ScreenFail], counts[ScreenTooFew], counts[ScreenNone])
	}
	text := b.String()
	// A line is matched by its letters and digits, so that a word spelled
	// across two of the text's words is found too.
	for _, line := range strings.Split(text, "\n") {
		if holds(line, r.forbid) {
			return "", errors.New("the report would hold a word a task forbids")
		}
	}
	return text, nil
}

// list joins ids, "none" for an empty one or an empty list.
func list(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id
		if id == "" {
			out[i] = "none"
		}
	}
	return strings.Join(out, ", ")
}

// figure prints a value of measure m: minutes from milliseconds, to two
// places; any other as it is, which is a whole number or a half.
func figure(m string, v float64) string {
	if m == "wall_ms" || m == "memory_ms" {
		return strconv.FormatFloat(v/60000, 'f', 2, 64)
	}
	return plain(v)
}

func plain(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func stat(m string, s Stat) string {
	if s.N == 0 {
		return "none"
	}
	return fmt.Sprintf("median %s, least %s, most %s", figure(m, s.Median), figure(m, s.Least), figure(m, s.Most))
}

func median(m string, n int, v float64) string {
	if n == 0 {
		return "none"
	}
	return figure(m, v)
}

func money(n int, v float64) string {
	if n == 0 {
		return "none"
	}
	return strconv.FormatFloat(v, 'f', 6, 64)
}

func pairs(p Pairs) string {
	switch p.N {
	case 0:
		return "pairs 0, no mean and no s"
	case 1:
		return fmt.Sprintf("pairs 1, mean %.4f, no s: under two pairs", p.Mean)
	}
	return fmt.Sprintf("pairs %d, mean %.4f, s %.4f, n %d", p.N, p.Mean, p.S, p.Need)
}
