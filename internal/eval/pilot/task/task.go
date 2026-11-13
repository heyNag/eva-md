// Package task holds the pilot's tasks: their format, the rules a task
// keeps, and the grader that runs a task's hidden tests on what a session
// left. It is throwaway: the pilot (EV-03) is deleted when the
// continuation harness (EV-17) lands, and no release builds it.
//
// A task is a folder named for its id, holding task.json and, beside it,
// repos/<name>/ (each repository as the first chat left it),
// hidden/<name>/ (the hidden tests, laid over a repository to grade it),
// solution/<name>/ (the work done right, laid over it) and, for a hidden
// decision, decoy/<name>/ (the work done without the facts the first chat
// stated, laid over it).
//
// An error names the task and the rule it breaks, never a value a task
// plants or a repository's name: both are words the pilot's rows must not
// hold (Forbid).
package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/host/adapter"
)

// Version is the format of task.json.
const Version = 1

// The families, languages and working folders a task has.
const (
	FamilyHandoff = "handoff" // the first chat did part of the work and said what was left
	FamilyHidden  = "hidden"  // the first chat stated a fact the repository does not show

	LangGo     = "go"
	LangPython = "python"

	WorkdirRepo    = "repo"    // the chats run in the one repository
	WorkdirOutside = "outside" // the chats run in a folder that is no repository and holds each one under its name
)

// Turn is one turn of the first chat.
type Turn struct {
	By     string   `json:"by"` // user or agent
	Text   string   `json:"text"`
	Plants []string `json:"plants"` // the ids of the facts the turn states
}

// Fact is something the first chat stated that the second chat needs.
type Fact struct {
	ID        string   `json:"id"`
	Statement string   `json:"statement"` // what a correction says
	Values    []string `json:"values"`    // the made-up words it turns on
	Name      bool     `json:"name"`      // a stated fact about a name
}

// Test is one hidden test.
type Test struct {
	ID    string   `json:"id"`
	Repo  string   `json:"repo"`  // the repository it runs in
	Run   []string `json:"run"`   // its argv, run with no shell
	Facts []string `json:"facts"` // the ids of the facts it encodes
}

// Task is one pilot task.
type Task struct {
	V         int      `json:"v"`
	ID        string   `json:"id"`
	Family    string   `json:"family"`
	Lang      string   `json:"lang"`
	Repos     []string `json:"repos"`
	Workdir   string   `json:"workdir"`
	History   []Turn   `json:"history"` // the first chat
	Facts     []Fact   `json:"facts"`
	Prompt    string   `json:"prompt"` // the second chat's first prompt
	Note      string   `json:"note"`   // the ideal record a person would hand over
	Tests     []Test   `json:"tests"`
	MaxTurns  int      `json:"max_turns"`
	MaxRounds int      `json:"max_rounds"`
	Minutes   int      `json:"minutes"`

	// Dir is the task's folder.
	Dir string `json:"-"`
}

// Error is a rule a task breaks.
type Error struct {
	Task   string // the task's id, or its folder's name when it has none
	Family string // its family, when known
	Rule   string // the rule's name
	Why    string // what breaks it, naming no value
}

func (e *Error) Error() string {
	return "task " + e.Task + ": " + e.Rule + ": " + e.Why
}

// The rules, by name.
const (
	ruleFormat   = "format"    // task.json is one object of this format
	ruleID       = "id"        // the family, the id and the folder agree
	ruleLang     = "lang"      // go or python
	ruleRepos    = "repos"     // one or two repositories, named as a value is, as workdir allows
	ruleLimits   = "limits"    // max_turns, max_rounds and minutes
	ruleValues   = "values"    // every fact has values, each shaped so no row can hold it by chance
	ruleHistory  = "history"   // 600 characters or more, ending with an agent turn
	rulePlanted  = "planted"   // every fact is stated by a turn that plants it
	ruleTests    = "tests"     // every test runs in a repository with its language's runner
	ruleEncoded  = "encoded"   // in a hidden decision, every fact is encoded by a test
	ruleNote     = "note"      // the note holds a value of every fact
	rulePrompt   = "prompt"    // the prompt holds no value
	ruleLayout   = "layout"    // the folders beside task.json
	ruleOverlay  = "overlay"   // every path under them is relative, clean, no link, inside its repository
	ruleSize     = "size"      // no repository over 300 lines
	ruleGoMod    = "go.mod"    // a made-up module, no requirement, no Go newer than eva-md's
	ruleLeak     = "leak"      // in a hidden decision, no file under repos/ or decoy/ holds a value
	ruleOwnTests = "own-tests" // each repository's own tests pass as it stands
	ruleStart    = "start"     // every hidden test fails on the repositories as they stand
	ruleSolution = "solution"  // every hidden test passes with the solution
	ruleDecoy    = "decoy"     // with the decoy, exactly the tests that encode a fact fail
)

// MaxLines is the most lines a repository holds, with or without its
// solution.
const MaxLines = 300

// MinHistory is the fewest characters the first chat holds: v1's update
// waits on a shorter chat (planWait in internal/update/pass.go).
const MinHistory = 600

// EvaGo is the Go version in eva-md's go.mod. No task's go.mod names a
// newer one, so the toolchain that builds eva-md runs every task.
const EvaGo = "1.26.0"

// Arms are the pilot's conditions, which a row's scenario names.
var Arms = []string{"off", "eva", "oracle"}

// Metrics are the pilot's metrics (EV-03b): a row's metric names one.
var Metrics = []string{
	"pilot.solved", "pilot.first_try", "pilot.corrections", "pilot.agent_events", "pilot.hook_seen", "pilot.peeked",
	"pilot.prompt_tokens", "pilot.cached_tokens", "pilot.output_tokens", "pilot.update_tokens", "pilot.wall_ms", "pilot.update_ms",
	"pilot.unjudged.sign_in", "pilot.unjudged.rate_limit", "pilot.unjudged.timeout", "pilot.unjudged.no_usage",
}

var (
	idPattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)
	repoPattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	familyIDPattern = map[string]*regexp.Regexp{
		FamilyHandoff: regexp.MustCompile(`^h0[1-5]$`),
		FamilyHidden:  regexp.MustCompile(`^d0[1-5]$`),
	}
)

// Forbid returns every value of the task's facts and every repository's
// name: the words no row of the pilot may hold (results.Store's Forbid).
func Forbid(t Task) []string {
	var words []string
	for _, f := range t.Facts {
		words = append(words, f.Values...)
	}
	return append(words, t.Repos...)
}

// Load reads every task in dir, one folder each, and applies the rules
// that run nothing. It returns the tasks that keep them, by id, and an
// error joining one *Error for each task that does not; a folder it
// cannot list, or one holding no task, is an error of its own.
func Load(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot list the tasks: %s", bare(err))
	}
	var tasks []Task
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			errs = append(errs, &Error{Task: e.Name(), Rule: ruleLayout, Why: "the tasks folder holds something that is not a task's folder"})
			continue
		}
		t, err := loadOne(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		tasks = append(tasks, t)
	}
	if len(entries) == 0 {
		return nil, errors.New("the tasks folder holds no task")
	}
	slices.SortFunc(tasks, func(a, b Task) int { return strings.Compare(a.ID, b.ID) })
	return tasks, errors.Join(errs...)
}

// loadOne reads the task in dir and applies the rules that run nothing.
func loadOne(dir string) (Task, error) {
	t := Task{Dir: dir}
	name := filepath.Base(dir)
	data, err := os.ReadFile(filepath.Join(dir, "task.json"))
	if err != nil {
		return t, &Error{Task: name, Rule: ruleFormat, Why: "cannot read task.json: " + bare(err)}
	}
	if err := decodeStrict(data, &t); err != nil {
		return t, &Error{Task: name, Rule: ruleFormat, Why: "task.json is not a task: " + err.Error()}
	}
	t.Dir = dir
	if rule, why := t.rules(name); rule != "" {
		task := t.ID
		if rule == ruleFormat || rule == ruleID {
			task = name
		}
		return t, &Error{Task: task, Family: t.Family, Rule: rule, Why: why}
	}
	return t, nil
}

// decodeStrict decodes one JSON value, refusing unknown fields and
// anything after the value. Its errors give a position, never the text.
func decodeStrict(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var syntax *json.SyntaxError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntax):
			return fmt.Errorf("a syntax error at byte %d", syntax.Offset)
		case errors.As(err, &typ):
			return fmt.Errorf("the field %s has the wrong type", typ.Field)
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return errors.New(strings.TrimPrefix(err.Error(), "json: "))
		}
		return errors.New("it does not decode")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("something follows the object")
	}
	return nil
}

// rules applies the rules that run nothing, in order, and returns the
// first one broken and why, or "". name is the folder's name.
func (t *Task) rules(name string) (rule, why string) {
	for _, check := range []func(string) (string, string){
		t.checkFormat, t.checkID, t.checkRepos, t.checkLimits, t.checkValues, t.checkHistory,
		t.checkPlanted, t.checkTests, t.checkEncoded, t.checkNote, t.checkPrompt,
		t.checkLayout, t.checkOverlay, t.checkSize, t.checkGoMod, t.checkLeak,
	} {
		if rule, why := check(name); rule != "" {
			return rule, why
		}
	}
	return "", ""
}

func (t *Task) checkFormat(string) (string, string) {
	if t.V != Version {
		return ruleFormat, fmt.Sprintf("its format is %d, not %d", t.V, Version)
	}
	return "", ""
}

func (t *Task) checkID(name string) (string, string) {
	p, ok := familyIDPattern[t.Family]
	switch {
	case !ok:
		return ruleID, "the family is neither handoff nor hidden"
	case !p.MatchString(t.ID):
		return ruleID, "the id is not h01 to h05 for a handoff or d01 to d05 for a hidden decision"
	case t.ID != name:
		return ruleID, "the folder is not named for the id"
	}
	return "", ""
}

func (t *Task) checkRepos(string) (string, string) {
	switch {
	case t.Lang != LangGo && t.Lang != LangPython:
		return ruleLang, "the language is neither go nor python"
	case len(t.Repos) < 1 || len(t.Repos) > 2:
		return ruleRepos, "a task has one repository or two"
	case t.Workdir != WorkdirRepo && t.Workdir != WorkdirOutside:
		return ruleRepos, "workdir is neither repo nor outside"
	case t.Workdir == WorkdirRepo && len(t.Repos) != 1:
		return ruleRepos, "workdir repo needs exactly one repository"
	case len(t.Repos) == 2 && t.Repos[0] == t.Repos[1]:
		return ruleRepos, "the two repositories have one name"
	}
	for i, r := range t.Repos {
		if !repoPattern.MatchString(r) {
			return ruleRepos, fmt.Sprintf("repository %d's name is not lower-case letters, digits, dashes and underscores", i+1)
		}
		if why := shape(r); why != "" {
			return ruleRepos, fmt.Sprintf("repository %d's name %s", i+1, why)
		}
	}
	return "", ""
}

func (t *Task) checkLimits(string) (string, string) {
	switch {
	case t.MaxTurns < 1:
		return ruleLimits, "max_turns is below 1"
	case t.MaxRounds < 1 || t.MaxRounds > 5:
		return ruleLimits, "max_rounds is not 1 to 5"
	case t.Minutes < 1:
		return ruleLimits, "minutes is below 1"
	}
	return "", ""
}

func (t *Task) checkValues(string) (string, string) {
	if len(t.Facts) == 0 {
		return ruleValues, "the task has no fact"
	}
	seen := map[string]bool{}
	for _, f := range t.Facts {
		switch {
		case !idPattern.MatchString(f.ID):
			return ruleValues, "a fact's id is not an id"
		case seen[f.ID]:
			return ruleValues, "fact " + f.ID + " is listed twice"
		case strings.TrimSpace(f.Statement) == "":
			return ruleValues, "fact " + f.ID + " has no statement"
		case len(f.Values) == 0:
			return ruleValues, "fact " + f.ID + " has no value"
		}
		seen[f.ID] = true
		for i, v := range f.Values {
			if why := shape(v); why != "" {
				return ruleValues, fmt.Sprintf("value %d of fact %s %s", i+1, f.ID, why)
			}
		}
	}
	return "", ""
}

func (t *Task) checkHistory(string) (string, string) {
	if len(t.History) == 0 {
		return ruleHistory, "the history has no turn"
	}
	n := 0
	for i, turn := range t.History {
		if turn.By != "user" && turn.By != "agent" {
			return ruleHistory, fmt.Sprintf("turn %d is by neither user nor agent", i+1)
		}
		if strings.TrimSpace(turn.Text) == "" {
			return ruleHistory, fmt.Sprintf("turn %d has no text", i+1)
		}
		n += utf8.RuneCountInString(turn.Text)
	}
	switch {
	case n < MinHistory:
		return ruleHistory, fmt.Sprintf("the history holds fewer than %d characters", MinHistory)
	case t.History[len(t.History)-1].By != "agent":
		return ruleHistory, "the history does not end with an agent turn"
	}
	return "", ""
}

func (t *Task) checkPlanted(string) (string, string) {
	planted := map[string]bool{}
	for i, turn := range t.History {
		for _, id := range turn.Plants {
			f, ok := t.fact(id)
			if !ok {
				return rulePlanted, fmt.Sprintf("turn %d plants a fact the task does not have", i+1)
			}
			if !holds(turn.Text, f.Values) {
				return rulePlanted, fmt.Sprintf("turn %d plants fact %s and holds none of its values", i+1, id)
			}
			planted[id] = true
		}
	}
	for _, f := range t.Facts {
		if !planted[f.ID] {
			return rulePlanted, "no turn plants fact " + f.ID
		}
	}
	return "", ""
}

func (t *Task) checkTests(string) (string, string) {
	if len(t.Tests) == 0 {
		return ruleTests, "the task has no test"
	}
	seen := map[string]bool{}
	for _, test := range t.Tests {
		switch {
		case !idPattern.MatchString(test.ID):
			return ruleTests, "a test's id is not an id"
		case seen[test.ID]:
			return ruleTests, "test " + test.ID + " is listed twice"
		case !slices.Contains(t.Repos, test.Repo):
			return ruleTests, "test " + test.ID + " runs in no repository of the task"
		case !runsWith(test.Run, t.Lang):
			return ruleTests, "test " + test.ID + " is not run by its language's runner: go test, or python3 -m unittest"
		}
		seen[test.ID] = true
		for _, id := range test.Facts {
			if _, ok := t.fact(id); !ok {
				return ruleTests, "test " + test.ID + " encodes a fact the task does not have"
			}
		}
	}
	return "", ""
}

// runsWith reports whether run is an argv of the language's test runner,
// with no empty argument.
func runsWith(run []string, lang string) bool {
	if slices.Contains(run, "") || slices.ContainsFunc(run, func(a string) bool { return strings.ContainsRune(a, 0) }) {
		return false
	}
	switch lang {
	case LangGo:
		return len(run) >= 2 && run[0] == "go" && run[1] == "test"
	case LangPython:
		return len(run) >= 3 && run[0] == "python3" && run[1] == "-m" && run[2] == "unittest"
	}
	return false
}

func (t *Task) checkEncoded(string) (string, string) {
	if t.Family != FamilyHidden {
		return "", ""
	}
	for _, f := range t.Facts {
		if !slices.ContainsFunc(t.Tests, func(test Test) bool { return slices.Contains(test.Facts, f.ID) }) {
			return ruleEncoded, "no test encodes fact " + f.ID
		}
	}
	return "", ""
}

func (t *Task) checkNote(string) (string, string) {
	for _, f := range t.Facts {
		if !holds(t.Note, f.Values) {
			return ruleNote, "the note holds no value of fact " + f.ID
		}
	}
	return "", ""
}

func (t *Task) checkPrompt(string) (string, string) {
	if strings.TrimSpace(t.Prompt) == "" {
		return rulePrompt, "the prompt is empty"
	}
	for _, f := range t.Facts {
		if holds(t.Prompt, f.Values) {
			return rulePrompt, "the prompt holds a value of fact " + f.ID
		}
	}
	return "", ""
}

// The trees beside task.json.
const (
	treeRepos    = "repos"
	treeHidden   = "hidden"
	treeSolution = "solution"
	treeDecoy    = "decoy"
)

func (t *Task) checkLayout(string) (string, string) {
	trees := []string{treeRepos, treeHidden, treeSolution}
	if t.Family == FamilyHidden {
		trees = append(trees, treeDecoy)
	}
	want := append([]string{"task.json"}, trees...)
	entries, err := os.ReadDir(t.Dir)
	if err != nil {
		return ruleLayout, "cannot list the task's folder: " + bare(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		return ruleLayout, "the task's folder holds other than task.json, repos, hidden, solution and, for a hidden decision alone, decoy"
	}
	for _, tree := range trees {
		info, err := os.Lstat(filepath.Join(t.Dir, tree))
		if err != nil || !info.IsDir() {
			return ruleLayout, tree + " is not a folder"
		}
		names, err := os.ReadDir(filepath.Join(t.Dir, tree))
		if err != nil {
			return ruleLayout, "cannot list " + tree + ": " + bare(err)
		}
		for _, n := range names {
			if !n.IsDir() {
				return ruleLayout, tree + " holds something that is not a repository's folder"
			}
			if !slices.Contains(t.Repos, n.Name()) {
				return ruleOverlay, tree + " holds a folder that is no repository of the task"
			}
		}
		if len(names) == 0 {
			return ruleLayout, tree + " holds nothing"
		}
	}
	for i, r := range t.Repos {
		if info, err := os.Lstat(filepath.Join(t.Dir, treeRepos, r)); err != nil || !info.IsDir() {
			return ruleLayout, fmt.Sprintf("repos holds no folder for repository %d", i+1)
		}
	}
	for _, test := range t.Tests {
		if info, err := os.Lstat(filepath.Join(t.Dir, treeHidden, test.Repo)); err != nil || !info.IsDir() {
			return ruleLayout, "hidden holds no folder for the repository test " + test.ID + " runs in"
		}
	}
	return "", ""
}

func (t *Task) checkOverlay(string) (string, string) {
	err := filepath.WalkDir(t.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("cannot read the task's folder: " + bare(err))
		}
		rel, err := filepath.Rel(t.Dir, path)
		if err != nil || !filepath.IsLocal(rel) && rel != "." {
			return errors.New("a path is not inside the task")
		}
		if rel != filepath.Clean(rel) || slices.Contains(strings.Split(filepath.ToSlash(rel), "/"), ".git") {
			return errors.New("a path is not clean, or is in a .git folder")
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return errors.New("a path is a link or not a plain file")
		}
		return nil
	})
	if err != nil {
		return ruleOverlay, err.Error()
	}
	return "", ""
}

func (t *Task) checkSize(string) (string, string) {
	for i, r := range t.Repos {
		lines := map[string]int{}
		for _, tree := range []string{treeRepos, treeSolution} {
			if err := countLines(filepath.Join(t.Dir, tree, r), lines); err != nil {
				return ruleSize, fmt.Sprintf("cannot read repository %d: %s", i+1, bare(err))
			}
			total := 0
			for _, n := range lines {
				total += n
			}
			if total > MaxLines {
				return ruleSize, fmt.Sprintf("repository %d holds more than %d lines in %s", i+1, MaxLines, tree)
			}
		}
	}
	return "", ""
}

// countLines sets lines[rel] to the lines of each file under dir, by its
// path relative to dir; a missing dir adds nothing.
func countLines(dir string, lines map[string]int) error {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n := bytes.Count(data, []byte("\n"))
		if len(data) > 0 && data[len(data)-1] != '\n' {
			n++
		}
		rel, _ := filepath.Rel(dir, path)
		lines[rel] = n
		return nil
	})
}

func (t *Task) checkGoMod(string) (string, string) {
	if t.Lang != LangGo {
		return "", ""
	}
	for i, r := range t.Repos {
		for _, tree := range []string{treeRepos, treeHidden, treeSolution, treeDecoy} {
			data, err := os.ReadFile(filepath.Join(t.Dir, tree, r, "go.mod"))
			if errors.Is(err, fs.ErrNotExist) && tree != treeRepos {
				continue
			}
			if err != nil {
				return ruleGoMod, fmt.Sprintf("repository %d has no go.mod", i+1)
			}
			if why := goModWrong(data); why != "" {
				return ruleGoMod, fmt.Sprintf("the go.mod of repository %d in %s %s", i+1, tree, why)
			}
		}
	}
	return "", ""
}

var goVersion = regexp.MustCompile(`^1\.(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*))?$`)

// goModWrong says what is wrong with a go.mod, or "": it holds a module
// line naming a made-up module (whose first element has no dot), a go
// line no newer than eva-md's, and nothing else but comments and blank
// lines.
func goModWrong(data []byte) string {
	var module, version string
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case len(fields) == 2 && fields[0] == "module" && module == "":
			module = fields[1]
		case len(fields) == 2 && fields[0] == "go" && version == "":
			version = fields[1]
		default:
			return "holds a line other than module and go, such as a requirement"
		}
	}
	first, _, _ := strings.Cut(module, "/")
	switch {
	case module == "":
		return "names no module"
	case strings.Contains(first, "."):
		return "names a module on a host, not a made-up one"
	case !goVersion.MatchString(version):
		return "names no Go version"
	case newerGo(version, EvaGo):
		return "names a Go newer than eva-md's"
	}
	return ""
}

// newerGo reports whether Go version a is newer than b; both match
// goVersion.
func newerGo(a, b string) bool {
	num := func(v string) [3]int {
		var n [3]int
		for i, part := range strings.Split(v, ".") {
			n[i], _ = strconv.Atoi(part)
		}
		return n
	}
	x, y := num(a), num(b)
	return slices.Compare(x[:], y[:]) > 0
}

func (t *Task) checkLeak(string) (string, string) {
	if t.Family != FamilyHidden {
		return "", ""
	}
	var values []string
	for _, f := range t.Facts {
		values = append(values, f.Values...)
	}
	for _, tree := range []string{treeRepos, treeDecoy} {
		err := filepath.WalkDir(filepath.Join(t.Dir, tree), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return errors.New("cannot read " + tree + ": " + bare(err))
			}
			rel, _ := filepath.Rel(filepath.Join(t.Dir, tree), path)
			if holds(rel, values) {
				return errors.New("a path under " + tree + " holds a fact's value")
			}
			if d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return errors.New("cannot read a file under " + tree + ": " + bare(err))
			}
			if holds(string(data), values) {
				return errors.New("a file under " + tree + " holds a fact's value")
			}
			return nil
		})
		if err != nil {
			return ruleLeak, err.Error()
		}
	}
	return "", ""
}

// fact returns the task's fact with the id.
func (t *Task) fact(id string) (Fact, bool) {
	for _, f := range t.Facts {
		if f.ID == id {
			return f, true
		}
	}
	return Fact{}, false
}

// holds reports whether text holds one of words, matched as
// results.Forbidden matches a run's words against a row.
func holds(text string, words []string) bool {
	return results.Forbidden([]results.Row{{Scenario: text}}, words) != nil
}

// letters returns s's letters and digits, in lower case, as
// results.Forbidden reads a word.
func letters(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// shape says what is wrong with a value or a repository's name, or "":
// it has six letters and digits or more, is not hex digits alone, which
// a commit id could hold, and is inside no field a pilot row holds of
// its own (an id, a host, an arm, a metric, a unit or a scenario).
func shape(v string) string {
	k := letters(v)
	switch {
	case utf8.RuneCountInString(k) < 6:
		return "has fewer than six letters and digits"
	case strings.Trim(k, "0123456789abcdef") == "":
		return "is hex digits only"
	case results.Forbidden(rowFields, []string{v}) != nil:
		return "is inside a task id, host id, arm or metric, which a row holds"
	}
	return ""
}

// rowFields are the fields a pilot row holds of its own, each a row of
// its own, so a value can be matched against each as a run's words are.
var rowFields = func() []results.Row {
	words := []string{"pilot", "ratio", "count", "tokens", "ms"}
	words = append(words, Arms...)
	words = append(words, Metrics...)
	for _, h := range adapter.All() {
		words = append(words, string(h.ID), h.Names.CLI, h.Names.InstallName, h.Names.App)
	}
	for _, f := range []string{"h", "d"} {
		for i := 1; i <= 5; i++ {
			id := fmt.Sprintf("%s%02d", f, i)
			words = append(words, id)
			for _, arm := range Arms {
				for n := 1; n <= 99; n++ {
					words = append(words, fmt.Sprintf("%s.%s.t%d", id, arm, n))
				}
			}
		}
	}
	rows := make([]results.Row, len(words))
	for i, w := range words {
		rows[i] = results.Row{Scenario: w}
	}
	return rows
}()

// bare returns err without the path an fs or exec error carries, which
// can hold a repository's name.
func bare(err error) string {
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		return pe.Op + ": " + pe.Err.Error()
	case errors.As(err, &le):
		return le.Op + ": " + le.Err.Error()
	}
	return err.Error()
}
