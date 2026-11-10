// Package results is the evaluation harness's one store of measurements:
// every instrument writes each figure it measures as a Row, a catalogue
// says what each metric is and which code it covers, gates say how a set
// of rows must compare with the accepted baseline, and Summarize gives the
// figures they compare. No release contains it.
//
// A row holds ids, numbers and a time, never text: each string in it must
// be a short id from a closed alphabet, so no captured text, sentence or
// path can be stored, and a Store refuses any row that holds one of the
// words its run forbids, such as a scenario's codenames. Errors name the
// row and the field, never what the field held.
package results

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Version is the format of the rows, the catalogue, the gates and the
// baseline this package writes and reads.
const Version = 1

// FileName is the rows file in a results folder.
const FileName = "results.jsonl"

// Row is one measured figure.
type Row struct {
	V           int       `json:"v"`
	Metric      string    `json:"metric"`
	Value       float64   `json:"value"`
	Unit        string    `json:"unit"`
	Commit      string    `json:"commit"` // the commit measured, in full
	Dirty       bool      `json:"dirty"`  // whether the tree had changes the commit does not hold
	Instrument  string    `json:"instrument"`
	Scenario    string    `json:"scenario"`
	Host        string    `json:"host,omitempty"`         // the host the figure was measured on, if any
	HostVersion string    `json:"host_version,omitempty"` // that host's version
	Model       string    `json:"model,omitempty"`        // the model that answered, if one was used
	Seed        uint64    `json:"seed"`
	Time        time.Time `json:"time"`
}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

// check returns what is wrong with r, naming the field and never its value,
// or "".
func (r Row) check() string {
	switch {
	case r.V != Version:
		return fmt.Sprintf("its format is %d, not %d", r.V, Version)
	case !idPattern.MatchString(r.Metric):
		return "metric is not an id"
	case math.IsNaN(r.Value) || math.IsInf(r.Value, 0):
		return "value is not a finite number"
	case !idPattern.MatchString(r.Unit):
		return "unit is not an id"
	case !commitPattern.MatchString(r.Commit):
		return "commit is not a full commit id"
	case !idPattern.MatchString(r.Instrument):
		return "instrument is not an id"
	case !idPattern.MatchString(r.Scenario):
		return "scenario is not an id"
	case r.Host != "" && !idPattern.MatchString(r.Host):
		return "host is not an id"
	case (r.Host == "") != (r.HostVersion == ""):
		return "host and host_version must be given together"
	case r.HostVersion != "" && !versionPattern.MatchString(r.HostVersion):
		return "host_version is not a version"
	case r.Model != "" && !idPattern.MatchString(r.Model):
		return "model is not an id"
	case r.Time.IsZero():
		return "time is missing"
	}
	return ""
}

// strings returns the row's string fields, by name.
func (r Row) strings() [][2]string {
	return [][2]string{{"metric", r.Metric}, {"unit", r.Unit}, {"commit", r.Commit}, {"instrument", r.Instrument},
		{"scenario", r.Scenario}, {"host", r.Host}, {"host_version", r.HostVersion}, {"model", r.Model}}
}

// Store appends rows to the rows file of a results folder.
type Store struct {
	Dir string
	// Forbid holds words no row may contain: the codenames and other
	// planted values of the run's scenarios. They are matched as Forbidden
	// matches them, so in any case and in any spelling of their letters
	// and digits.
	Forbid []string
}

// Forbidden returns an error that names the first row and field holding
// one of words, never the word, or nil. A word is matched by its letters
// and digits alone, in any case, against a field's letters and digits
// alone: since a field is an id, "Blue Heron" is found in "blue-heron",
// "blue.heron" and "blueheron", and "KESTREL_41" in "kestrel-41". A word
// with no letter or digit matches nothing.
func Forbidden(rows []Row, words []string) error {
	keys := forbidKeys(words)
	for i, r := range rows {
		if field := r.forbidden(keys); field != "" {
			return fmt.Errorf("row %d: %s holds a word the run forbids", i+1, field)
		}
	}
	return nil
}

// forbidKeys returns the letters and digits of each word that has some.
func forbidKeys(words []string) []string {
	var keys []string
	for _, w := range words {
		if k := letters(w); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// forbidden returns the name of r's first field whose letters and digits
// hold one of keys, or "".
func (r Row) forbidden(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	for _, f := range r.strings() {
		field := letters(f[1])
		for _, k := range keys {
			if strings.Contains(field, k) {
				return f[0]
			}
		}
	}
	return ""
}

// letters returns s's letters and digits, in lower case, and nothing else.
func letters(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// Append checks every row, then appends them all to the folder's rows
// file in one write, making the folder if needed. It sets each row's
// format and puts its time in UTC. A row that fails a check appends
// nothing, and so does a rows file that does not end with a whole line,
// as after an interrupted write: a row appended to it would join the
// part of a line and make the whole file unreadable.
func (s Store) Append(rows ...Row) error {
	var buf bytes.Buffer
	keys := forbidKeys(s.Forbid)
	for i, r := range rows {
		r.V = Version
		r.Time = r.Time.UTC()
		if msg := r.check(); msg != "" {
			return fmt.Errorf("results: row %d: %s", i+1, msg)
		}
		if field := r.forbidden(keys); field != "" {
			return fmt.Errorf("results: row %d: %s holds a word the run forbids", i+1, field)
		}
		line, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("results: row %d: %w", i+1, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if len(rows) == 0 {
		return nil
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("results: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, FileName), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("results: %w", err)
	}
	err = endsWithALine(f)
	if err == nil {
		_, err = f.Write(buf.Bytes())
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("results: %w", err)
	}
	return nil
}

// errPartialLine is the error for a rows file that does not end with a
// whole line.
var errPartialLine = errors.New("the rows file does not end with a whole line")

// endsWithALine returns errPartialLine when the file f is not empty and
// its last byte is not a newline.
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

// Load reads a set of rows: the rows file of the folder path names, or the
// file itself. It refuses the whole set when any line is not a row that
// passes the checks Append makes, so nothing but ids and numbers reaches a
// report.
func Load(path string) ([]Row, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, FileName)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("results: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("results: %w", errPartialLine)
	}
	var rows []Row
	for i, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		var r Row
		if err := decodeStrict(line, &r); err != nil {
			return nil, fmt.Errorf("results: line %d is not a row", i+1)
		}
		if msg := r.check(); msg != "" {
			return nil, fmt.Errorf("results: line %d: %s", i+1, msg)
		}
		rows = append(rows, r)
	}
	return rows, nil
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

// Source returns the commit checked out in the repository at repo, in
// full, and whether its tree has changes the commit does not hold, an
// untracked file among them: what a row's commit and dirty say.
func Source(ctx context.Context, repo string) (commit string, dirty bool, err error) {
	out, err := git(ctx, repo, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", false, fmt.Errorf("results: cannot read the commit: %w", err)
	}
	commit = strings.TrimSpace(out)
	if !commitPattern.MatchString(commit) {
		return "", false, errors.New("results: git gave no commit id")
	}
	status, err := git(ctx, repo, "status", "--porcelain", "-z", "--untracked-files=normal")
	if err != nil {
		return "", false, fmt.Errorf("results: cannot read the tree's state: %w", err)
	}
	return commit, status != "", nil
}

// git runs git in repo and returns its output. Its error says how git
// ended and never what it printed.
func git(ctx context.Context, repo string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return out.String(), nil
}
