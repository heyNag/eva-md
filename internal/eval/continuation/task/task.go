// Package task is the continuation instrument's task format: an earlier
// chat, the repositories it worked in, the prompt that starts a new chat,
// and hidden tests that grade what the new chat leaves, each test naming
// the facts of the earlier chat it encodes. It loads and checks a task,
// builds the workspace a chat starts from, grades a workspace, and turns
// one trial's outcome into result rows. It runs no host and calls no model.
// No release contains it.
//
// A task is a folder: task.json, and overlays on the fixture repositories,
// each a folder of files laid over a copy of a repository: start/<repo>/
// (the state the earlier chat left), hidden/<repo>/ (the hidden tests),
// solution/<repo>/ (a solution) and, for a hidden-decision task,
// decoy/<repo>/ (a solution from the repository and the prompt alone,
// which misses every fact).
package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FileName is a task's own file in its folder.
const FileName = "task.json"

// Families of task.
const (
	FamilyHandoff = "handoff" // the earlier chat stopped part way
	FamilyHidden  = "hidden"  // a decision exists only in the earlier chat
	FamilyControl = "control" // memory cannot help
)

// Families lists every family, in a fixed order.
var Families = []string{FamilyHandoff, FamilyHidden, FamilyControl}

// Where a chat runs.
const (
	WorkdirRepo    = "repo"    // in the repository itself
	WorkdirOutside = "outside" // in a folder, not a repository, holding each under its name
)

// The overlays of a task, by folder.
const (
	OverlayStart    = "start"
	OverlayHidden   = "hidden"
	OverlaySolution = "solution"
	OverlayDecoy    = "decoy"
)

var overlays = []string{OverlayStart, OverlayHidden, OverlaySolution, OverlayDecoy}

// DefaultLimit is a test's time limit in seconds when it gives none, and
// MaxLimit the longest it may give.
const (
	DefaultLimit = 600
	MaxLimit     = 3600
)

// MinHistory is the fewest characters an earlier chat may hold: v1's update
// waits on a chat with less (planWait, internal/update/pass.go), so a
// shorter history would give the eva condition nothing to work from.
const MinHistory = 600

// minValue is the fewest letters and digits a value or a repository name
// may hold: a shorter word would be found by chance inside a row's fields.
const minValue = 6

// maxID is the longest task id, so that a row's scenario, the id and
// ".t<trial>", stays a results id for any trial up to 999999.
const maxID = 56

// Task is one continuation task.
type Task struct {
	ID      string   `json:"id"`
	Family  string   `json:"family"`
	Repos   []string `json:"repos"`
	Workdir string   `json:"workdir"`
	History []Turn   `json:"history"`
	Facts   []Fact   `json:"facts"`
	Prompt  string   `json:"prompt"`
	// Note is the ideal record a person would hand over.
	Note      string `json:"note"`
	Tests     []Test `json:"tests"`
	MaxTurns  int    `json:"max_turns"`
	MaxRounds int    `json:"max_rounds"`

	// Dir is the task's folder, set by Load; the overlays are read from it.
	Dir string `json:"-"`
}

// Turn is one turn of the earlier chat.
type Turn struct {
	By     string   `json:"by"` // "user" or "agent"
	Text   string   `json:"text"`
	Plants []string `json:"plants,omitempty"` // the ids of the facts it states
}

// Fact is one fact of the earlier chat a new chat may need.
type Fact struct {
	ID string `json:"id"`
	// Statement is what the driver says to supply it.
	Statement string `json:"statement"`
	// Values are the made-up words it turns on; no row may hold one.
	Values []string `json:"values"`
	// Name is true for a fact about a name.
	Name bool `json:"name,omitempty"`
}

// Test is one hidden test.
type Test struct {
	ID   string `json:"id"`
	Repo string `json:"repo"`
	// Run is the argv, run with no shell in the repository's copy, that
	// runs this one test with its language's runner.
	Run []string `json:"run"`
	// Facts are the ids of the facts it encodes.
	Facts []string `json:"facts,omitempty"`
	// LimitS is its time limit in seconds; DefaultLimit when 0.
	LimitS int `json:"limit_s,omitempty"`
}

// Limit is the test's time limit in seconds.
func (x Test) Limit() int {
	if x.LimitS == 0 {
		return DefaultLimit
	}
	return x.LimitS
}

// idPattern is a results id, as internal/eval/results checks a row's.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Load reads the task in the folder dir and validates it.
func Load(dir string) (Task, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return Task{}, fmt.Errorf("task: %w", err)
	}
	var t Task
	if err := decodeStrict(data, &t); err != nil {
		return Task{}, fmt.Errorf("task: %s: %w", filepath.Base(dir), err)
	}
	t.Dir = dir
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	return t, nil
}

// LoadAll loads every task in the folder dir, one a subfolder, in the order
// of their folders' names. Two tasks may not share an id.
func LoadAll(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("task: %w", err)
	}
	var tasks []Task
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := Load(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("task %s: two tasks have this id", t.ID)
		}
		seen[t.ID] = true
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return nil, errors.New("task: the folder holds no task")
	}
	return tasks, nil
}

// decodeStrict decodes one JSON value that holds only v's fields and
// nothing after it.
func decodeStrict(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("data after the value")
	}
	return nil
}

// Validate names the first fault of the task, never a value. When Dir is
// set it checks the overlays on disk too.
func (t Task) Validate() error {
	err := t.validate()
	switch {
	case err == nil:
		return nil
	case idPattern.MatchString(t.ID) && !holdsAny(t.ID, Forbid(t)):
		return fmt.Errorf("task %s: %w", t.ID, err)
	}
	return fmt.Errorf("task: %w", err)
}

// holdsAny reports whether s holds one of words, matched by their letters
// and digits as a results Store matches them.
func holdsAny(s string, words []string) bool {
	s = letters(s)
	return slices.ContainsFunc(words, func(w string) bool { k := letters(w); return k != "" && strings.Contains(s, k) })
}

func (t Task) validate() error {
	switch {
	case !idPattern.MatchString(t.ID) || len(t.ID) > maxID:
		return fmt.Errorf("the id is not a results id of at most %d characters", maxID)
	case !slices.Contains(Families, t.Family):
		return errors.New("unknown family")
	case t.Workdir != WorkdirRepo && t.Workdir != WorkdirOutside:
		return errors.New("unknown workdir")
	case len(t.Repos) == 0 || len(t.Repos) > 2:
		return errors.New("a task names one or two repositories")
	case t.Workdir == WorkdirRepo && len(t.Repos) != 1:
		return errors.New("a chat in a repository has one repository; two need the workdir outside")
	case strings.TrimSpace(t.Prompt) == "":
		return errors.New("no prompt")
	case strings.TrimSpace(t.Note) == "":
		return errors.New("no note")
	case t.MaxTurns < 1:
		return errors.New("max_turns is under 1")
	case t.MaxRounds < 1 || t.MaxRounds > 5:
		return errors.New("max_rounds is not 1 to 5")
	case len(t.Tests) == 0:
		return errors.New("no test")
	}
	for i, r := range t.Repos {
		if !idPattern.MatchString(r) || slices.Contains(t.Repos[:i], r) {
			return fmt.Errorf("repository %d: its name is not an id, or is given twice", i+1)
		}
	}
	facts := map[string]bool{}
	for i, f := range t.Facts {
		switch {
		case !idPattern.MatchString(f.ID) || facts[f.ID]:
			return fmt.Errorf("fact %d: its id is not an id, or is given twice", i+1)
		case strings.TrimSpace(f.Statement) == "":
			return fmt.Errorf("fact %d: no statement", i+1)
		case len(f.Values) == 0:
			return fmt.Errorf("fact %d: no value", i+1)
		}
		facts[f.ID] = true
	}
	if err := t.checkHistory(facts); err != nil {
		return err
	}
	encoded := map[string]bool{}
	tests := map[string]bool{}
	for i, x := range t.Tests {
		switch {
		case !idPattern.MatchString(x.ID) || tests[x.ID]:
			return fmt.Errorf("test %d: its id is not an id, or is given twice", i+1)
		case !slices.Contains(t.Repos, x.Repo):
			return fmt.Errorf("test %d: its repository is not the task's", i+1)
		case len(x.Run) == 0 || x.Run[0] == "":
			return fmt.Errorf("test %d: run is not an argv", i+1)
		case x.LimitS < 0 || x.LimitS > MaxLimit:
			return fmt.Errorf("test %d: limit_s is not 0 to %d", i+1, MaxLimit)
		case t.Family == FamilyControl && len(x.Facts) > 0:
			return fmt.Errorf("test %d: a control task's test encodes no fact", i+1)
		}
		tests[x.ID] = true
		for j, f := range x.Facts {
			if !facts[f] || slices.Contains(x.Facts[:j], f) {
				return fmt.Errorf("test %d: it encodes an unknown fact, or one twice", i+1)
			}
			encoded[f] = true
		}
	}
	if t.Family == FamilyHidden {
		if len(t.Facts) == 0 {
			return errors.New("a hidden-decision task hides no fact")
		}
		for i, f := range t.Facts {
			if !encoded[f.ID] {
				return fmt.Errorf("fact %d: no test encodes it, in a hidden-decision task", i+1)
			}
		}
	}
	if err := t.checkWords(); err != nil {
		return err
	}
	if t.Dir != "" {
		return t.checkOverlays()
	}
	return nil
}

// checkHistory checks the earlier chat: known speakers, plants of known
// facts, every fact planted, an agent's turn last, and enough text for
// v1's update to take it.
func (t Task) checkHistory(facts map[string]bool) error {
	planted := map[string]bool{}
	chars, user := 0, false
	for i, turn := range t.History {
		switch {
		case turn.By != "user" && turn.By != "agent":
			return fmt.Errorf("turn %d: by is not user or agent", i+1)
		case strings.TrimSpace(turn.Text) == "":
			return fmt.Errorf("turn %d: no text", i+1)
		}
		for _, p := range turn.Plants {
			if !facts[p] {
				return fmt.Errorf("turn %d: it plants an unknown fact", i+1)
			}
			planted[p] = true
		}
		user = user || turn.By == "user"
		chars += utf8.RuneCountInString(turn.Text)
	}
	for i, f := range t.Facts {
		if !planted[f.ID] {
			return fmt.Errorf("fact %d: no turn plants it", i+1)
		}
	}
	switch {
	case len(t.History) == 0 || t.History[len(t.History)-1].By != "agent":
		return errors.New("the history does not end with an agent's turn")
	case !user:
		return errors.New("the history has no user's turn")
	case chars < MinHistory:
		return fmt.Errorf("the history holds under %d characters, which v1's update would wait on", MinHistory)
	}
	return nil
}

// checkWords checks every value and repository name against the rule that
// lets a results Store refuse a row holding one: it is matched by its
// letters and digits inside any field, so it must not be inside the task's
// or a test's id or a word every row of the task holds, must not be so
// short it is found by chance, and must not be hex digits only, which a
// commit id may hold.
func (t Task) checkWords() error {
	words := Forbid(t)
	id := letters(t.ID)
	fixed := fixedWords(t.Family)
	for _, w := range words {
		k := letters(w)
		switch {
		case utf8.RuneCountInString(k) < minValue:
			return fmt.Errorf("a value or repository name holds under %d letters and digits", minValue)
		case strings.Trim(k, "0123456789abcdef") == "":
			return errors.New("a value or repository name is hex digits only, which a commit id may hold")
		case inScenario(k, id):
			return errors.New("a value or repository name is inside the task's id")
		}
		for i, x := range t.Tests {
			if strings.Contains(letters(x.ID), k) {
				return fmt.Errorf("test %d: a value or repository name is inside its id", i+1)
			}
		}
		for _, f := range fixed {
			if strings.Contains(f, k) {
				return errors.New("a value or repository name is inside a word every row of the task holds")
			}
		}
	}
	return nil
}

// inScenario reports whether the key k is inside the letters and digits of
// a row's scenario, "<task id>.t<trial>", for some trial, where id is the
// task id's letters and digits.
func inScenario(k, id string) bool {
	if strings.Contains(id, k) {
		return true
	}
	for i := 0; i < len(k); i++ {
		if k[i] == 't' && strings.HasSuffix(id, k[:i]) && strings.Trim(k[i+1:], "0123456789") == "" {
			return true
		}
	}
	return false
}

// letters returns s's letters and digits, in lower case, and nothing else,
// as internal/eval/results matches a forbidden word.
func letters(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// checkOverlays checks the overlay folders of the task in Dir: each holds
// only folders named for the task's repositories, decoy/ only in a
// hidden-decision task, and under them only folders and regular files at
// paths that stay inside the repository and out of git's own folder.
func (t Task) checkOverlays() error {
	for _, o := range overlays {
		dir := filepath.Join(t.Dir, o)
		info, err := os.Lstat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("overlay %s: %w", o, err)
		}
		switch {
		case !info.IsDir():
			return fmt.Errorf("overlay %s: it is a link or not a folder", o)
		case o == OverlayDecoy && t.Family != FamilyHidden:
			return errors.New("overlay decoy: only a hidden-decision task has one")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("overlay %s: %w", o, err)
		}
		for _, e := range entries {
			if !slices.Contains(t.Repos, e.Name()) {
				return fmt.Errorf("overlay %s: it holds something other than the task's repositories", o)
			}
			if err := walkOverlay(filepath.Join(dir, e.Name()), nil); err != nil {
				return fmt.Errorf("overlay %s/%s: %w", o, e.Name(), err)
			}
		}
	}
	return nil
}

// walkOverlay walks the overlay folder root, checking each path in it, and
// calls file, if not nil, with the slash path of each regular file.
func walkOverlay(root string, file func(rel string) error) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("it is a link or not a folder")
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := overlayPath(rel); err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			return errors.New("it holds a link")
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			return errors.New("it holds something that is neither a folder nor a file")
		}
		if file != nil {
			return file(rel)
		}
		return nil
	})
}

// overlayPath returns what is wrong with rel, an overlay's path in slash
// form relative to its repository, or nil.
func overlayPath(rel string) error {
	switch {
	case path.IsAbs(rel) || filepath.IsAbs(rel):
		return errors.New("it holds an absolute path")
	case rel == "" || path.Clean(rel) != rel || strings.ContainsAny(rel, "\\\x00\n"):
		return errors.New("it holds a path that is not clean")
	case rel == ".." || strings.HasPrefix(rel, "../"):
		return errors.New("it holds a path that leaves its repository")
	case slices.ContainsFunc(strings.Split(rel, "/"), func(e string) bool { return strings.EqualFold(e, ".git") }):
		return errors.New("it holds a path in git's own folder")
	}
	return nil
}
