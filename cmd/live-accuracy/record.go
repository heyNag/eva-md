package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/heyNag/eva-md/internal/update"
)

// A recording is a live run's answers, one JSON line for each pass, in the
// order the run asked for them:
//
//	{"v":1,"host":…,"effort":…,"model":…,"script":…,"pass":…,"output":…,"reason":…}
//
// host is the host the run asked, model the model it asked for ("" for
// the host's default), script the fact script's place in the suite, from
// 1, and pass the pass's, from 1. output is the answer's JSON, or null
// when the runner gave none; reason is the runner's reason, "" for an
// answer; effort is the effort flag the answer came at, "" when it came
// with none, so a replay counts the efforts as the run did. A recording
// holds nothing else: no token count, no time and no path.
//
// This file holds the format, its writer and its checks. It imports
// neither the suite nor its live run, which only main.go may import.

// recordVersion is the recording's format.
const recordVersion = 1

// passRecord is one line of a recording.
type passRecord struct {
	V      int             `json:"v"`
	Host   string          `json:"host"`
	Effort string          `json:"effort"`
	Model  string          `json:"model"`
	Script int             `json:"script"`
	Pass   int             `json:"pass"`
	Output json.RawMessage `json:"output"`
	Reason string          `json:"reason"`
}

// recordKeys are a line's keys, every one of them required.
var recordKeys = []string{"v", "host", "effort", "model", "script", "pass", "output", "reason"}

// passKey names a pass: its script's place in the suite and its own in the
// script, both from 1.
type passKey struct{ script, pass int }

// knownReasons are the reasons the update's runner gives, "" for an
// answer.
var knownReasons = []string{"", update.ReasonCLIMissing, update.ReasonAuth, update.ReasonRateLimit, update.ReasonHostError,
	update.ReasonModelRejected, update.ReasonTimeout, update.ReasonInvalidOutput, update.ReasonToolUsed, update.ReasonMCPFailed}

// effortWord is an effort flag as the runner gives one.
var effortWord = regexp.MustCompile(`^[a-z]{0,16}$`)

// result is the runner's result the line records.
func (r passRecord) result() update.Result {
	res := update.Result{Effort: r.Effort, Reason: r.Reason}
	if !bytes.Equal(r.Output, []byte("null")) {
		res.Output = r.Output
	}
	return res
}

// recorder writes a recording as the run goes.
type recorder struct {
	f           *os.File
	host, model string
}

// createRecording makes the recording at path, which must not exist yet.
func createRecording(path, host, model string) (*recorder, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return &recorder{f: f, host: host, model: model}, nil
}

// write appends the line of pass of script, numbered from 1, with the
// runner's result, and syncs it, so a run that stops keeps every pass it
// recorded.
func (r *recorder) write(script, pass int, res update.Result) error {
	line := passRecord{V: recordVersion, Host: r.host, Effort: res.Effort, Model: r.model, Script: script, Pass: pass, Reason: res.Reason}
	if len(res.Output) > 0 && json.Valid(res.Output) {
		line.Output = res.Output
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(line); err != nil {
		return err
	}
	if _, err := r.f.Write(buf.Bytes()); err != nil {
		return err
	}
	return r.f.Sync()
}

func (r *recorder) close() error { return r.f.Close() }

// readRecording reads the recording at path and checks it against the
// suite, where counts[i] is how many passes script i+1 has: every line a
// pass of the suite recorded on host for model, in this format, with a
// reason the update knows, and every pass of the suite there once. A
// recording of a run that stopped part way lacks a pass, and is refused.
// Its errors name a line by number, never what it holds.
func readRecording(path, host, model string, counts []int) (map[passKey]passRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, errors.New("the recording does not end with a whole line")
	}
	answers := map[passKey]passRecord{}
	var lines [][]byte
	if len(data) > 0 {
		lines = bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	}
	for i, raw := range lines {
		n := i + 1
		r, err := decodeRecord(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d is not a pass of a recording", n)
		}
		k := passKey{r.Script, r.Pass}
		switch {
		case r.V != recordVersion:
			return nil, fmt.Errorf("line %d is of another version of the format than %d", n, recordVersion)
		case r.Host != host:
			return nil, fmt.Errorf("line %d was recorded on another host than %s", n, host)
		case r.Model != model:
			return nil, fmt.Errorf("line %d was answered by another model than the one --model names", n)
		case r.Script < 1 || r.Script > len(counts) || r.Pass < 1 || r.Pass > counts[r.Script-1]:
			return nil, fmt.Errorf("line %d is of a pass the suite does not have", n)
		case !slices.Contains(knownReasons, r.Reason):
			return nil, fmt.Errorf("line %d has a reason the update does not know", n)
		case !effortWord.MatchString(r.Effort):
			return nil, fmt.Errorf("line %d has an effort that is not one", n)
		}
		if _, ok := answers[k]; ok {
			return nil, fmt.Errorf("line %d repeats script %d, pass %d", n, k.script, k.pass)
		}
		answers[k] = r
	}
	lack := 0
	first := passKey{}
	for s, c := range counts {
		for p := 1; p <= c; p++ {
			if _, ok := answers[passKey{s + 1, p}]; !ok {
				if lack == 0 {
					first = passKey{s + 1, p}
				}
				lack++
			}
		}
	}
	if lack > 0 {
		return nil, fmt.Errorf("the recording lacks %d %s of the suite, script %d, pass %d first: the run that made it stopped part way", lack, plural(lack, "pass", "passes"), first.script, first.pass)
	}
	return answers, nil
}

// decodeRecord decodes one line, which must hold every key of the format
// and nothing else.
func decodeRecord(raw []byte) (passRecord, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return passRecord{}, err
	}
	for _, k := range recordKeys {
		if _, ok := keys[k]; !ok {
			return passRecord{}, errors.New("a key is missing")
		}
	}
	if len(keys) != len(recordKeys) {
		return passRecord{}, errors.New("a key is not the format's")
	}
	var r passRecord
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return passRecord{}, err
	}
	return r, nil
}

// pageName is the file of script n's page, v1.NN.md, as the fidelity
// scorer names v1's ported scenarios.
func pageName(n int) string { return fmt.Sprintf("v1.%02d.md", n) }

// checkPages refuses a pages folder that holds anything, or is not a
// folder. A folder that does not exist yet is made when the run starts.
func checkPages(dir string) error {
	names, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if _, lerr := os.Lstat(dir); lerr == nil {
			return errors.New("--pages names a link to nothing")
		}
		return nil
	case err != nil:
		return errors.New("--pages must name a folder that is new or empty")
	case len(names) != 0:
		return errors.New("--pages must name a folder that is new or empty")
	}
	return nil
}

// writePage writes script n's page into dir.
func writePage(dir string, n int, text string) error {
	return os.WriteFile(filepath.Join(dir, pageName(n)), []byte(text), 0o644)
}
