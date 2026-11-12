package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/eval/v1page"
)

// eva-eval fidelity scores the pages v1's update wrote on its 12 accuracy
// scripts, as the live accuracy run's --pages writes them: for each of
// v1's ported scenarios it reads v1.NN.md, a missing page being an empty
// store, scores it, and appends a row for each of the scorer's metrics, of
// scenario v1.NN, then the suite's totals as scenario v1: the counts
// summed, and each share of the facts that must be stored and are still
// in force taken over all of the suite's. It prints one line for each
// scenario, of numbers and outcome codes only, and the notes that say how
// to read them.
func init() {
	register(instrument{
		name:     "fidelity",
		synopsis: "fidelity [--repo dir] --pages dir --out dir [--host h --host-version v] [--model m]",
		run:      fidelityCommand,
	})
}

// fidelityInstrument is the rows' instrument.
const fidelityInstrument = "fidelity"

// suiteScenario is the scenario of the suite's totals.
const suiteScenario = "v1"

// portBeyondV1 are the scenarios on which v1's own correct outputs do not
// hold every fact of the port (EV-14b, as merged): v1.02 drops
// "yesterday", v1.05 the benchmark's name from the condition, v1.08 keeps
// two dated lines only under the user's tag and one answer only on the
// Timeline, and v1.11 loses one codename.
var portBeyondV1 = []string{"v1.02", "v1.05", "v1.08", "v1.11"}

func fidelityCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval fidelity", flag.ContinueOnError)
	f.SetOutput(errOut)
	repo := f.String("repo", ".", "the repository measured, whose commit the rows name")
	pages := f.String("pages", "", "the folder of v1's pages, v1.01.md to v1.12.md, as the live accuracy run's --pages writes them")
	outDir := f.String("out", "", "the results folder the rows are appended to")
	host := f.String("host", "", "the host whose answers wrote the pages, with --host-version")
	hostVersion := f.String("host-version", "", "that host's version")
	model := f.String("model", "", "the model that answered, if it is known")
	if err := f.Parse(args); err != nil {
		return 2
	}
	switch {
	case f.NArg() != 0:
		return usage(errOut, "fidelity takes no argument but its flags")
	case *pages == "" || *outDir == "":
		return usage(errOut, "fidelity needs --pages and --out")
	case (*host == "") != (*hostVersion == ""):
		return usage(errOut, "fidelity takes --host and --host-version together")
	}
	if info, err := os.Stat(*pages); err != nil || !info.IsDir() {
		return fail(errOut, errors.New("--pages must name a folder"))
	}
	scenarios := scenario.V1()
	type scored struct {
		res    score.Result
		noPage bool
	}
	var all []scored
	found := 0
	for _, sc := range scenarios {
		ex, ok, err := readV1Page(filepath.Join(*pages, sc.ID+".md"))
		if err != nil {
			return fail(errOut, fmt.Errorf("%s.md: %w", sc.ID, err))
		}
		if ok {
			found++
		}
		all = append(all, scored{score.Score(sc, ex), !ok})
	}
	if found == 0 {
		return fail(errOut, errors.New("--pages holds none of v1's pages, v1.01.md to v1.12.md"))
	}
	commit, dirty, err := results.Source(ctx, *repo)
	if err != nil {
		return fail(errOut, err)
	}
	template := results.Row{Commit: commit, Dirty: dirty, Instrument: fidelityInstrument, Host: *host, HostVersion: *hostVersion,
		Model: *model, Seed: 0, Time: time.Now()}
	var rows []results.Row
	var forbid []string
	total := score.Result{Scenario: suiteScenario}
	required, held, recalled := 0, 0, 0
	for i, sc := range scenarios {
		res := all[i].res
		rows = append(rows, res.Rows(template)...)
		forbid = append(forbid, scenario.Forbid(sc)...)
		for _, f := range mustHold(sc) {
			required++
			switch res.Facts[f].Outcome {
			case score.Holds:
				held++
				recalled++
			case score.Found:
				recalled++
			}
		}
		m, t := res.Metrics, &total.Metrics
		t.WrongWrite += m.WrongWrite
		t.Noise += m.Noise
		t.Misattributed += m.Misattributed
		t.StaleInForce += m.StaleInForce
		t.Dupes += m.Dupes
		t.OTRStored += m.OTRStored
		t.Violations += m.Violations
	}
	total.Metrics.FactRecall, total.Metrics.FactHolds = shareOf(recalled, required), shareOf(held, required)
	rows = append(rows, total.Rows(template)...)
	store := results.Store{Dir: *outDir, Forbid: forbid}
	if err := store.Append(rows...); err != nil {
		return fail(errOut, err)
	}
	pasted := pastedScenarios(scenarios)
	for i, sc := range scenarios {
		line := figures(sc.ID, all[i].res.Metrics) + "; " + outcomes(all[i].res)
		if all[i].noPage {
			line += "; no_page"
		}
		if slices.Contains(portBeyondV1, sc.ID) {
			line += " [1]"
		}
		if slices.Contains(pasted, sc.ID) {
			line += " [2]"
		}
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "%s; facts that must hold: %d, holding: %d, found: %d\n", figures(suiteScenario, total.Metrics), required, held, recalled-held)
	fmt.Fprintf(out, "[1] v1's own correct outputs do not hold every fact of the port on %s, so fact_holds there is below 1 even when v1's run is right by its own measure.\n", strings.Join(portBeyondV1, ", "))
	fmt.Fprintf(out, "[2] The scorer cannot read a negation, so violations on %s, whose chats hold pasted text, is a lower bound.\n", strings.Join(pasted, ", "))
	// The folder alone: the rows file's name holds a planted word.
	fmt.Fprintf(out, "Wrote %d rows of commit %s to the results folder %s.\n", len(rows), commit[:12], *outDir)
	return 0
}

// readV1Page reads a page v1 wrote, and whether there was one: a page
// that is missing is an empty store.
func readV1Page(path string) (score.Export, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return score.Export{}, false, nil
	}
	if err != nil {
		return score.Export{}, false, err
	}
	ex, err := v1page.ReadThread(string(data))
	return ex, true, err
}

// mustHold gives the facts of sc that must be stored and are still in
// force, as the scorer judges them: in an empty store, these and only
// these are missing.
func mustHold(sc scenario.Scenario) []int {
	var facts []int
	for i, fr := range score.Score(sc, score.Export{}).Facts {
		if fr.Outcome == score.Missing {
			facts = append(facts, i)
		}
	}
	return facts
}

// pastedScenarios are the scenarios with a fact that may be kept, but
// never under the user's tag: text pasted into a chat.
func pastedScenarios(scenarios []scenario.Scenario) []string {
	var ids []string
	for _, sc := range scenarios {
		if slices.ContainsFunc(sc.Facts, func(f scenario.Fact) bool { return f.Must == scenario.MustNotWithAuthority }) {
			ids = append(ids, sc.ID)
		}
	}
	return ids
}

// shareOf is n of all, 1 when there is none, as the scorer's shares are.
func shareOf(n, of int) float64 {
	if of == 0 {
		return 1
	}
	return float64(n) / float64(of)
}

// figures is a scenario's metrics, by the names their rows carry.
func figures(id string, m score.Metrics) string {
	return fmt.Sprintf("%s: fact_recall %.2f, fact_holds %.2f, wrong_write %d, noise %d, misattributed %d, stale_in_force %d, dupes %d, otr_stored %d, violations %d",
		id, m.FactRecall, m.FactHolds, m.WrongWrite, m.Noise, m.Misattributed, m.StaleInForce, m.Dupes, m.OTRStored, m.Violations)
}

// outcomes is each fact's outcome by its id, with any problem, then the
// count of each outcome of the items that is not a support.
func outcomes(res score.Result) string {
	var parts []string
	for _, fr := range res.Facts {
		p := fr.Fact + " " + string(fr.Outcome)
		for _, pr := range fr.Problems {
			p += "/" + string(pr)
		}
		parts = append(parts, p)
	}
	counts := map[score.Outcome]int{}
	for _, ir := range res.Items {
		if ir.Outcome != score.Supports && ir.Outcome != score.Unscored {
			counts[ir.Outcome]++
		}
	}
	var items []string
	for _, o := range slices.Sorted(maps.Keys(counts)) {
		items = append(items, fmt.Sprintf("%s %d", o, counts[o]))
	}
	line := strings.Join(parts, ", ")
	if len(items) > 0 {
		line += "; items " + strings.Join(items, ", ")
	}
	return line
}
