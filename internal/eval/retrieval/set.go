// Package retrieval is the retrieval instrument: a labelled set of made-up
// projects, memory items and typed prompts, the System a retrieval core
// implements to be scored on it, and the scorer, which turns what a system
// added for each prompt into precision, recall, false additions and
// silence, for each half of the set, and into result rows. v1's recall is
// one of its systems, so v2's core is judged against v1's row on the same
// prompts. No release contains it.
//
// The set lives in eval/retrieval/set: set.json (its version, the seed it
// was generated with, and the short list of common words its labels allow
// a paraphrase to share with its item), projects.json, items.jsonl,
// events.jsonl and halves.sha256. An event's half, tuning or held out, is a
// function of a hash of its id, and halves.sha256 holds a hash of each
// half's events, so a held-out event cannot change unseen.
package retrieval

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The set's files, in its folder.
const (
	SetFile      = "set.json"
	ProjectsFile = "projects.json"
	ItemsFile    = "items.jsonl"
	EventsFile   = "events.jsonl"
	HalvesFile   = "halves.sha256"
)

// The kinds of item, as the store names them.
var Kinds = []string{"fact", "decision", "constraint", "procedure", "rule"}

// The scopes of an item: its project's, a workstream's in its project, or
// every project's.
const (
	ScopeHere    = "here"
	ScopeProject = "project"
	ScopeAll     = "all"
)

// Splits say what an event tests: words its items hold, other words for
// them, or a name one item is known by.
var Splits = []string{"lexical", "paraphrase", "name"}

// Classes say what an event should bring: its relevant items, or nothing
// for the three negative classes.
const Positive = "positive"

var (
	NegativeClasses = []string{"no_match", "near_miss", "sibling_only"}
	Classes         = append([]string{Positive}, NegativeClasses...)
)

// The halves of the set.
const (
	Tuning  = "tuning"
	Heldout = "heldout"
)

// Halves are the halves, in the order the scorer reports them.
var Halves = []string{Tuning, Heldout}

// Bounds of an item's fields, as the store's format sets them, but for the
// statement, which the set keeps to 150 characters so that three fit v1's
// block of 1,000.
const (
	MaxTitle     = 200
	MaxStatement = 150
	MaxCues      = 8
	MaxCue       = 60
	MaxWords     = 240
)

// Item is one piece of memory, with the store's field names.
type Item struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Project   string   `json:"project,omitempty"` // absent on an item of every project that names none
	Scope     string   `json:"scope"`
	Title     string   `json:"title"`
	Statement string   `json:"statement"`
	Cues      []string `json:"cues,omitempty"`
	Words     string   `json:"words,omitempty"` // the user's own words, of a rule or a constraint only
	Paths     []string `json:"paths,omitempty"`
}

// Project is one made-up project: its id, its key in v1's form, the name
// that key holds, and the system pages it shares with other projects.
type Project struct {
	ID    string   `json:"id"`
	Key   string   `json:"key"`
	Name  string   `json:"name"`
	Pages []string `json:"-"` // filled from the groups of projects.json
}

// Group is a system page and the projects that share it.
type Group struct {
	Page     string   `json:"page"`
	Projects []string `json:"projects"`
}

// Event is one typed prompt and its labels. A system under test is given
// its id, project, first and text only: the scorer clears its split,
// class, relevant items and decoys, and gives them to the oracle alone.
type Event struct {
	ID       string   `json:"id"`
	Project  string   `json:"project"`
	First    bool     `json:"first"` // the chat's first prompt
	Text     string   `json:"text"`
	Split    string   `json:"split"`
	Class    string   `json:"class"`
	Relevant []string `json:"relevant"` // items it should bring, best first; empty but for a positive
	Decoys   []string `json:"decoys"`   // items it was built to tempt
}

// Set is the labelled set.
type Set struct {
	Version  int
	Seed     uint64
	Common   []string // words a paraphrase may share with its item
	Projects []Project
	Groups   []Group
	Items    []Item
	Events   []Event
}

// meta is set.json.
type meta struct {
	V       int      `json:"v"`
	Version int      `json:"version"`
	Seed    uint64   `json:"seed"`
	Common  []string `json:"common"`
}

// projectsFile is projects.json.
type projectsFile struct {
	Projects []Project `json:"projects"`
	Groups   []Group   `json:"groups"`
}

var (
	itemID    = regexp.MustCompile(`^i[0-9abcdefghjkmnpqrstvwxyz]{13}$`)
	projectID = regexp.MustCompile(`^p_[0-9abcdefghjkmnpqrstvwxyz]{12}$`)
	eventID   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	pageSlug  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// Load reads the set in dir and checks it: every file whole and of known
// fields, every id well formed and once, every item within the store's
// bounds, every label naming items that exist, no item of another project
// relevant, and each half's events as halves.sha256 holds them. Errors
// name the file, line and field, never what a text field held.
func Load(dir string) (*Set, error) {
	var m meta
	if err := readJSON(filepath.Join(dir, SetFile), &m); err != nil {
		return nil, err
	}
	if m.V != 1 || m.Version < 1 {
		return nil, fmt.Errorf("retrieval: %s: its format must be 1 and its version 1 or more", SetFile)
	}
	var p projectsFile
	if err := readJSON(filepath.Join(dir, ProjectsFile), &p); err != nil {
		return nil, err
	}
	s := &Set{Version: m.Version, Seed: m.Seed, Common: m.Common, Projects: p.Projects, Groups: p.Groups}
	var err error
	if s.Items, err = readLines[Item](filepath.Join(dir, ItemsFile)); err != nil {
		return nil, err
	}
	if s.Events, err = readLines[Event](filepath.Join(dir, EventsFile)); err != nil {
		return nil, err
	}
	if err := s.check(); err != nil {
		return nil, err
	}
	halves, err := os.ReadFile(filepath.Join(dir, HalvesFile))
	if err != nil {
		return nil, fmt.Errorf("retrieval: %w", err)
	}
	if err := s.checkHalves(halves); err != nil {
		return nil, err
	}
	return s, nil
}

// readJSON decodes the one JSON value in file, which holds only v's fields.
func readJSON(file string, v any) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("retrieval: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("retrieval: %s is not valid: %w", filepath.Base(file), err)
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("retrieval: %s holds data after its value", filepath.Base(file))
	}
	return nil
}

// readLines decodes a file of one JSON value a line, each holding only T's
// fields.
func readLines[T any](file string) ([]T, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("retrieval: %w", err)
	}
	name := filepath.Base(file)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("retrieval: %s does not end with a whole line", name)
	}
	var out []T
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		var v T
		d := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		d.DisallowUnknownFields()
		if err := d.Decode(&v); err != nil {
			return nil, fmt.Errorf("retrieval: %s line %d is not valid", name, n)
		}
		if _, err := d.Token(); err != io.EOF {
			return nil, fmt.Errorf("retrieval: %s line %d holds more than one value", name, n)
		}
		out = append(out, v)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("retrieval: %s: %w", name, err)
	}
	return out, nil
}

// check checks everything but the halves.
func (s *Set) check() error {
	projects := map[string]*Project{}
	for i := range s.Projects {
		p := &s.Projects[i]
		switch {
		case !projectID.MatchString(p.ID):
			return fmt.Errorf("retrieval: project %d: id is not a project id", i+1)
		case projects[p.ID] != nil:
			return fmt.Errorf("retrieval: project %s is listed twice", p.ID)
		case p.Key == "" || p.Name == "" || !strings.Contains(p.Key, p.Name):
			return fmt.Errorf("retrieval: project %s: its key must be given and hold its name", p.ID)
		}
		p.Pages = nil
		projects[p.ID] = p
	}
	if len(projects) == 0 {
		return errors.New("retrieval: the set has no project")
	}
	pages := map[string]bool{}
	for i, g := range s.Groups {
		if !pageSlug.MatchString(g.Page) || pages[g.Page] || len(g.Projects) < 2 {
			return fmt.Errorf("retrieval: group %d: a page is named once, and shared by two projects or more", i+1)
		}
		pages[g.Page] = true
		for _, id := range g.Projects {
			p := projects[id]
			if p == nil || slices.Contains(p.Pages, g.Page) {
				return fmt.Errorf("retrieval: group %s names a project that is not in the set, or one twice", g.Page)
			}
			p.Pages = append(p.Pages, g.Page)
		}
	}
	items := map[string]*Item{}
	for i := range s.Items {
		it := &s.Items[i]
		if msg := it.check(projects); msg != "" {
			return fmt.Errorf("retrieval: %s line %d: %s", ItemsFile, i+1, msg)
		}
		if items[it.ID] != nil {
			return fmt.Errorf("retrieval: %s line %d: id is listed twice", ItemsFile, i+1)
		}
		items[it.ID] = it
	}
	seen := map[string]bool{}
	for i, e := range s.Events {
		if msg := e.check(projects, items); msg != "" {
			return fmt.Errorf("retrieval: %s line %d: %s", EventsFile, i+1, msg)
		}
		if seen[e.ID] {
			return fmt.Errorf("retrieval: %s line %d: id is listed twice", EventsFile, i+1)
		}
		seen[e.ID] = true
	}
	return nil
}

// check returns what is wrong with an item, naming the field, or "".
func (it *Item) check(projects map[string]*Project) string {
	switch {
	case !itemID.MatchString(it.ID):
		return "id is not an item id"
	case !slices.Contains(Kinds, it.Type):
		return "type is not one of the five kinds"
	case it.Scope != ScopeHere && it.Scope != ScopeProject && it.Scope != ScopeAll:
		return "scope is not here, project or all"
	case it.Project == "" && it.Scope != ScopeAll, it.Project != "" && projects[it.Project] == nil:
		return "project is not a project of the set"
	case !within(it.Title, 1, MaxTitle):
		return fmt.Sprintf("title must be 1 to %d characters on one line", MaxTitle)
	case !within(it.Statement, 1, MaxStatement):
		return fmt.Sprintf("statement must be 1 to %d characters on one line", MaxStatement)
	case len(it.Cues) > MaxCues:
		return fmt.Sprintf("it has more than %d cues", MaxCues)
	case (it.Type == "rule" || it.Type == "constraint") != (it.Words != ""):
		return "words are given for a rule or a constraint, and for nothing else"
	case it.Words != "" && !within(it.Words, 1, MaxWords):
		return fmt.Sprintf("words must be 1 to %d characters on one line", MaxWords)
	}
	for _, c := range it.Cues {
		if !within(c, 1, MaxCue) {
			return fmt.Sprintf("a cue must be 1 to %d characters on one line", MaxCue)
		}
	}
	for _, p := range it.Paths {
		if !within(p, 1, 200) || strings.ContainsAny(p, " \t") {
			return "a path is empty, long or holds a space"
		}
	}
	return ""
}

// within reports whether s is one line of min to max characters.
func within(s string, min, max int) bool {
	n := utf8.RuneCountInString(s)
	return utf8.ValidString(s) && n >= min && n <= max && !strings.ContainsAny(s, "\n\r") && strings.TrimSpace(s) == s
}

// check returns what is wrong with an event, naming the field, or "".
func (e *Event) check(projects map[string]*Project, items map[string]*Item) string {
	switch {
	case !eventID.MatchString(e.ID):
		return "id is not an event id"
	case projects[e.Project] == nil:
		return "project is not a project of the set"
	case strings.TrimSpace(e.Text) == "" || !utf8.ValidString(e.Text):
		return "text is empty"
	case !slices.Contains(Splits, e.Split):
		return "split is not lexical, paraphrase or name"
	case !slices.Contains(Classes, e.Class):
		return "class is not positive, no_match, near_miss or sibling_only"
	case e.Relevant == nil || e.Decoys == nil:
		return "relevant and decoys must be lists"
	case (e.Class == Positive) != (len(e.Relevant) > 0):
		return "a positive event has relevant items, and no other does"
	}
	for _, id := range e.Relevant {
		it := items[id]
		switch {
		case it == nil:
			return "a relevant item is not in the set"
		case it.Scope != ScopeAll && it.Project != e.Project:
			return "a relevant item is another project's"
		case count(e.Relevant, id) > 1:
			return "a relevant item is listed twice"
		}
	}
	for _, id := range e.Decoys {
		switch {
		case items[id] == nil:
			return "a decoy is not in the set"
		case slices.Contains(e.Relevant, id) || count(e.Decoys, id) > 1:
			return "a decoy is listed twice or is relevant"
		}
	}
	return ""
}

// count returns how many times id is in ids.
func count(ids []string, id string) int {
	n := 0
	for _, x := range ids {
		if x == id {
			n++
		}
	}
	return n
}

// Half returns the half an event of the id is in: a function of the
// SHA-256 of its id alone.
func Half(id string) string {
	if sum := sha256.Sum256([]byte(id)); sum[0]&1 == 0 {
		return Tuning
	}
	return Heldout
}

// HalvesText returns what halves.sha256 holds for events: for each half, a
// line of the SHA-256 of its events, one JSON line each in their order,
// two spaces and the half's name.
func HalvesText(events []Event) []byte {
	var out bytes.Buffer
	for _, half := range Halves {
		h := sha256.New()
		for _, e := range events {
			if Half(e.ID) == half {
				line, _ := json.Marshal(e)
				h.Write(append(line, '\n'))
			}
		}
		fmt.Fprintf(&out, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), half)
	}
	return out.Bytes()
}

// checkHalves checks the events against halves.sha256, naming the half
// that changed.
func (s *Set) checkHalves(data []byte) error {
	want := strings.Split(string(HalvesText(s.Events)), "\n")
	got := strings.Split(string(data), "\n")
	if len(got) != len(want) {
		return fmt.Errorf("retrieval: %s does not hold a line for each half", HalvesFile)
	}
	for i, half := range Halves {
		if got[i] != want[i] {
			return fmt.Errorf("retrieval: the %s half's events are not those %s holds: a changed event needs a new hash, and a held-out one a new version of the set", half, HalvesFile)
		}
	}
	return nil
}

// Item returns the item with the id, or nil.
func (s *Set) Item(id string) *Item {
	for i := range s.Items {
		if s.Items[i].ID == id {
			return &s.Items[i]
		}
	}
	return nil
}

// Forbid returns the words no result row may hold: every made-up name in
// the set. That is every project's name and key, the owner a key names
// between its host and its name, every system page the projects share, and
// every name an item is known by, a cue of one capitalized word. A key's
// host is not made up, and is not forbidden.
func (s *Set) Forbid() []string {
	var out []string
	add := func(w string) {
		if w != "" && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	for _, p := range s.Projects {
		add(p.Name)
		add(p.Key)
		if parts := strings.Split(p.Key, "__"); len(parts) > 2 {
			for _, owner := range parts[1 : len(parts)-1] {
				add(owner)
			}
		}
	}
	for _, g := range s.Groups {
		add(g.Page)
	}
	for _, it := range s.Items {
		for _, c := range it.Cues {
			if IsName(c) {
				add(c)
			}
		}
	}
	return out
}

var namePattern = regexp.MustCompile(`^[A-Z][a-z]{2,}$`)

// IsName reports whether a cue is a name: one word, capitalized.
func IsName(cue string) bool { return namePattern.MatchString(cue) }
