package drive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// RunsFile is the file in a results folder that holds one Record a run,
// beside the rows file.
const RunsFile = "runs.jsonl"

// recordVersion is the format of a Record.
const recordVersion = 1

// How a run ended, besides a code of stopCodes: every trial planned was
// run or counted not run, or the run stopped on an error of its own.
const (
	EndedDone  = "done"
	EndedError = "error"
)

// Record is what one run did: a JSON line appended to RunsFile in the
// results folder when the run ends, however it ends, once its folder under
// --work is made. A process that is killed writes none. It holds ids and
// figures only, checked as a results row's are.
type Record struct {
	V          int    `json:"v"`
	Run        string `json:"run"`
	Instrument string `json:"instrument"`
	Host       string `json:"host"`
	// HostVersions are the versions of the host's CLI the trials read,
	// sorted, each once.
	HostVersions []string `json:"host_versions"`
	Condition    string   `json:"condition"`
	Model        string   `json:"model"` // the model asked for, or ""
	Commit       string   `json:"commit"`
	Dirty        bool     `json:"dirty"`
	Tasks        []string `json:"tasks"` // the tasks' ids, in the order they ran
	Trials       int      `json:"trials"`
	Planned      int      `json:"planned"`
	Judged       int      `json:"judged"`
	NotRun       int      `json:"not_run"`
	// Unjudged counts the trials left unjudged by each code of Codes, zeros
	// too.
	Unjudged map[string]int `json:"unjudged"`
	NoModel  int            `json:"no_model"`
	Tokens   int64          `json:"tokens"`
	Rows     int            `json:"rows"`
	Kept     bool           `json:"kept"` // whether the trials' folders were kept
	// Ended is EndedDone, EndedError, or the code of stopCodes that stopped
	// the run.
	Ended string `json:"ended"`
}

var (
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

// check returns what is wrong with r, naming the field and never its
// value, or "".
func (r Record) check() string {
	switch {
	case r.V != recordVersion:
		return fmt.Sprintf("its format is %d, not %d", r.V, recordVersion)
	case !idPattern.MatchString(r.Run):
		return "run is not an id"
	case r.Instrument != task.Instrument:
		return "instrument is not " + task.Instrument
	case !idPattern.MatchString(r.Host):
		return "host is not an id"
	case r.HostVersions == nil || !slices.IsSorted(r.HostVersions) || len(slices.Compact(slices.Clone(r.HostVersions))) != len(r.HostVersions):
		return "host_versions is not a sorted list, each once"
	case slices.ContainsFunc(r.HostVersions, func(v string) bool { return !versionPattern.MatchString(v) }):
		return "host_versions holds what is not a version"
	case !idPattern.MatchString(r.Condition):
		return "condition is not an id"
	case r.Model != "" && !idPattern.MatchString(r.Model):
		return "model is not an id"
	case !commitPattern.MatchString(r.Commit):
		return "commit is not a full commit id"
	case r.Tasks == nil || slices.ContainsFunc(r.Tasks, func(id string) bool { return !idPattern.MatchString(id) }):
		return "tasks is not a list of ids"
	case r.Trials < 1 || r.Planned < 0 || r.Judged < 0 || r.NotRun < 0 || r.NoModel < 0 || r.Tokens < 0 || r.Rows < 0:
		return "a count is out of range"
	case len(r.Unjudged) != len(Codes):
		return "unjudged does not count each code once"
	case r.Ended != EndedDone && r.Ended != EndedError && !slices.Contains(stopCodes, r.Ended):
		return "ended is not a way a run ends"
	}
	counted := r.Judged + r.NotRun
	for code, n := range r.Unjudged {
		if !slices.Contains(Codes, code) {
			return "unjudged holds a code not on the list"
		}
		if n < 0 {
			return "a count is out of range"
		}
		counted += n
	}
	// A trial the run stopped on an error of its own is in none of them.
	if counted > r.Planned {
		return "it counts more trials than were planned"
	}
	return ""
}

// strings are the record's string fields.
func (r Record) strings() []string {
	s := []string{r.Run, r.Instrument, r.Host, r.Condition, r.Model, r.Commit, r.Ended}
	s = append(s, r.HostVersions...)
	return append(s, r.Tasks...)
}

// appendRecord checks r and appends it to the RunsFile of the folder dir
// in one write, making the folder if needed, as a results.Store appends a
// row: one that fails a check, or holds a word of forbid as a Store
// matches it, appends nothing, and so does a file that does not end with a
// whole line.
func appendRecord(dir string, r Record, forbid []string) error {
	if msg := r.check(); msg != "" {
		return errors.New(msg)
	}
	var keys []string
	for _, w := range forbid {
		if k := lower(w); k != "" {
			keys = append(keys, k)
		}
	}
	for _, s := range r.strings() {
		field := lower(s)
		if slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(field, k) }) {
			return errors.New("it holds a word the run forbids")
		}
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return pathless(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, RunsFile), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return pathless(err)
	}
	err = endsWithALine(f)
	if err == nil {
		_, err = f.Write(append(line, '\n'))
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return pathless(err)
}

// lower returns s's letters and digits, in lower case, and nothing else.
func lower(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// errPartialLine is the error for a runs file that does not end with a
// whole line.
var errPartialLine = errors.New("the runs file does not end with a whole line")

// endsWithALine returns errPartialLine when the file f is not empty and its
// last byte is not a newline.
func endsWithALine(f *os.File) error {
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return err
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return err
	}
	if last[0] != '\n' {
		return errPartialLine
	}
	return nil
}

// LoadRuns reads the records of the RunsFile in the results folder dir, in
// the order they were written. It refuses the whole file when a line is not
// a record that passes the checks a run makes before appending one: a
// field it does not know, a code not on the list or a way of ending that
// is none among them, and a last line that is not whole. A folder with no
// RunsFile gives an error that wraps fs.ErrNotExist.
func LoadRuns(dir string) ([]Record, error) {
	data, err := os.ReadFile(filepath.Join(dir, RunsFile))
	if err != nil {
		return nil, fmt.Errorf("the runs file: %w", pathless(err))
	}
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("the runs file: %w", errPartialLine)
	}
	var records []Record
	for i, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		var r Record
		if err := decodeStrict(line, &r); err != nil {
			return nil, fmt.Errorf("the runs file: line %d is not a record", i+1)
		}
		if msg := r.check(); msg != "" {
			return nil, fmt.Errorf("the runs file: line %d: %s", i+1, msg)
		}
		records = append(records, r)
	}
	return records, nil
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
