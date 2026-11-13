package fakehost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// The made-up words of the fixture's task: its repository's name and the
// values of its two facts.
const (
	FixtureRepo  = "quarrystone"
	FixtureColor = "vermilionx"
	FixtureLimit = "tessellate"
)

// FixtureID is the fixture task's id.
const FixtureID = "hidden-banner"

// Fixture writes, under dir, a hidden-decision task in tasks/ and its
// fixture repository in repos/, and returns both folders. The task hides
// the first facts of two (one or two): what each says must be in the
// repository's answer.txt, which its hidden tests check with /bin/sh
// scripts, so grading it needs no language's runner. Its decoy writes an
// answer that misses every fact, its solution one that holds each.
func Fixture(dir string, facts int) (tasks, repos string, err error) {
	tasks, repos = filepath.Join(dir, "tasks"), filepath.Join(dir, "repos")
	all := []task.Fact{
		{ID: "f-color", Statement: "The banner's colour is " + FixtureColor + ", as the design review chose.", Values: []string{FixtureColor}},
		{ID: "f-limit", Statement: "The banner's closing word is " + FixtureLimit + ", which the printer expects.", Values: []string{FixtureLimit}},
	}
	all = all[:facts]
	t := task.Task{
		ID:      FixtureID,
		Family:  task.FamilyHidden,
		Repos:   []string{FixtureRepo},
		Workdir: task.WorkdirRepo,
		Facts:   all,
		Prompt:  "Write answer.txt for the banner, as we planned.",
		Tests: []task.Test{
			{ID: "has-answer", Repo: FixtureRepo, Run: []string{"checks/answer"}},
			{ID: "banner-colour", Repo: FixtureRepo, Run: []string{"checks/colour"}, Facts: []string{"f-color"}},
		},
		MaxTurns:  20,
		MaxRounds: 3,
	}
	var plants, solution, note []string
	for _, f := range all {
		plants = append(plants, f.ID)
		solution = append(solution, f.Values[0])
		note = append(note, f.Statement)
	}
	t.Note = strings.Join(note, " ")
	files := map[string]string{
		"repos/" + FixtureRepo + "/README.md":                         "A made-up repository for the driver's tests.\n",
		"tasks/" + FixtureID + "/hidden/" + FixtureRepo + "/checks/answer": "#!/bin/sh\ntest -f answer.txt\n",
		"tasks/" + FixtureID + "/hidden/" + FixtureRepo + "/checks/colour": "#!/bin/sh\ngrep -q " + FixtureColor + " answer.txt\n",
		"tasks/" + FixtureID + "/decoy/" + FixtureRepo + "/answer.txt":     "plain\n",
		"tasks/" + FixtureID + "/solution/" + FixtureRepo + "/answer.txt":  strings.Join(solution, " ") + "\n",
	}
	if facts > 1 {
		t.Tests = append(t.Tests, task.Test{ID: "banner-close", Repo: FixtureRepo, Run: []string{"checks/close"}, Facts: []string{"f-limit"}})
		files["tasks/"+FixtureID+"/hidden/"+FixtureRepo+"/checks/close"] = "#!/bin/sh\ngrep -q " + FixtureLimit + " answer.txt\n"
	}
	filler := " We spoke about the layout of the banner, the order of its lines and the size of its type, and agreed to leave the rest for the next chat."
	t.History = []task.Turn{
		{By: "user", Text: "Here is what we settled for the banner." + filler + filler, Plants: plants},
		{By: "agent", Text: "Noted, I will keep each of those for when answer.txt is written." + filler + filler},
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", "", err
	}
	files["tasks/"+FixtureID+"/"+task.FileName] = string(data) + "\n"
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", "", err
		}
		mode := os.FileMode(0o644)
		if strings.Contains(rel, "/checks/") {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			return "", "", err
		}
	}
	return tasks, repos, nil
}
