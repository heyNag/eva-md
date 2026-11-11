package hostfacts

import (
	"reflect"
	"strings"
	"sync"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// The files of a recording folder. Every line is written through a
// Replacer, so no name it holds reaches one.
const (
	EventsFile  = "events.jsonl"
	MarksFile   = "marks.jsonl"
	NotesFile   = "notes.jsonl"
	OutputsFile = "commands.jsonl"
	ErrorsFile  = "errors.jsonl"
	ArmFile     = "arm.json"
	StateFile   = "state.json"
)

// Event is one hook's line in events.jsonl.
type Event struct {
	V      int    `json:"v"`
	Mark   string `json:"mark"`
	Wall   string `json:"wall"`
	MonoNS int64  `json:"mono_ns"`
	Host   string `json:"host"`
	Event  string `json:"event"`
	// Via names a hook declared beside the recorder's own, for one step:
	// the exec-form script, the missing binary, the one with a two-second
	// limit, or the start handler that declares no context limit.
	Via string `json:"via,omitempty"`
	// Payload is the payload whole; PayloadHashes the keyed hash of each
	// of its strings, by field path.
	Payload       any               `json:"payload"`
	PayloadHashes map[string]string `json:"payload_hashes,omitempty"`
	Proc          *Proc             `json:"proc,omitempty"`
	Git           *GitShape         `json:"git,omitempty"`
	EnvNames      []string          `json:"env_names,omitempty"`
	EnvMarks      []string          `json:"env_marks,omitempty"`
	FilesAdded    map[string]int    `json:"files_added,omitempty"`
	Record        *Snapshot         `json:"record,omitempty"`
	Arm           string            `json:"arm,omitempty"`
	Answer        string            `json:"answer,omitempty"`
	RootExpanded  *bool             `json:"root_expanded,omitempty"`
	Cut           []string          `json:"cut,omitempty"`
}

// Proc is the hook's process: its parent's name, whether that is a login
// shell, and the time from the parent's creation to the hook's exit.
type Proc struct {
	Parent      string   `json:"parent,omitempty"`
	Login       *bool    `json:"login,omitempty"`
	StartCostMS *float64 `json:"start_cost_ms,omitempty"`
}

// GitShape is the payload's working folder as git sees it: whether it is in
// a repository, whether HEAD is detached, and whether its git folder is its
// own or a worktree's of a shared one.
type GitShape struct {
	Repo     bool   `json:"repo"`
	Detached bool   `json:"detached,omitempty"`
	Common   string `json:"common,omitempty"`
}

// MarkLine is a line of marks.jsonl: a step's mark, or the close of a
// sitting.
type MarkLine struct {
	V          int               `json:"v"`
	Kind       string            `json:"kind"`
	Mark       string            `json:"mark,omitempty"`
	Wall       string            `json:"wall"`
	MonoNS     int64             `json:"mono_ns"`
	UTCOffsetS int               `json:"utc_offset_s"`
	Versions   map[string]string `json:"versions,omitempty"`
}

// NoteLine is a line of notes.jsonl.
type NoteLine struct {
	V    int    `json:"v"`
	Mark string `json:"mark"`
	Wall string `json:"wall"`
	Text string `json:"text"`
}

// OutputLine is a line of commands.jsonl: what a command found, in counts,
// states and field names.
type OutputLine struct {
	V       int            `json:"v"`
	Mark    string         `json:"mark"`
	Wall    string         `json:"wall"`
	Command string         `json:"command"`
	Result  map[string]any `json:"result"`
}

// ErrorLine is a line of errors.jsonl.
type ErrorLine struct {
	V     int    `json:"v"`
	Wall  string `json:"wall"`
	Where string `json:"where"`
	Error string `json:"error"`
}

// FixtureMeta is fixture.json, beside a folder of fixtures: the
// generator's seed, the made-up epoch and the base of the monotonic
// readings, from which a test rebuilds every value.
type FixtureMeta struct {
	V        int      `json:"v"`
	Seed     string   `json:"seed"`
	Epoch    string   `json:"epoch"`
	MonoBase int64    `json:"mono_base"`
	Files    []string `json:"files"`
}

// The recorder's own values, which the fixtures keep.
var formatValues = []string{
	"string", "number", "bool", "null", "object", "array", "own", "shared", "mark", "close",
	"exec-check", "missing-check", "timeout-check", "plain-start", "start", "start-word",
	"parent_turn_id_present", "root_turn_id_is_turn_id",
}

var (
	formatOnce  sync.Once
	formatWords map[string]bool
)

// FormatWord reports a key or value of the recording's own format: a
// field of its lines, a value it writes itself, a host's id, or a step of
// the run-book.
func FormatWord(s string) bool {
	formatOnce.Do(func() {
		formatWords = map[string]bool{}
		for _, v := range []any{Event{}, Proc{}, GitShape{}, MarkLine{}, NoteLine{}, OutputLine{}, ErrorLine{}, FixtureMeta{}, Snapshot{}, Row{}, Field{}, Presence{}} {
			t := reflect.TypeOf(v)
			for i := 0; i < t.NumField(); i++ {
				if name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); name != "" && name != "-" {
					formatWords[name] = true
				}
			}
		}
		for _, w := range formatValues {
			formatWords[w] = true
		}
		for _, h := range adapter.All() {
			formatWords[string(h.ID)] = true
		}
		for _, s := range steps {
			formatWords[s.ID] = true
		}
	})
	return formatWords[s]
}
