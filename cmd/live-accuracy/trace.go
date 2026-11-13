package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/update"
)

// A trace is what a live run cost, which versions made it and what the
// host printed, written into a folder as the run goes, each line synced:
//
//	run.json        {"v":1,"host":…,"effort":…,"model":…,"cli_version":…,"eva_md":{"commit":…,"dirty":…},"timeout_refused":…,"day":…}
//	passes.jsonl    one line a pass the runner answered: {"script":…,"pass":…,"ms":…,"tokens_in":…,"tokens_out":…,"attempts":…,
//	                "effort_used":…,"model_reported":…,"model_note":…,"degraded":…,"reason":…,"refused":…}
//	raw/SS-PP-AA.out and .err   what the CLI printed on stdout and stderr on attempt AA of pass PP of script SS
//	end.json        {"outcome":"met"|"missed"|"stopped","passes":…,"refused":…,"timeouts":…,"stopped":…}, written last
//
// run.json's effort and model are the ones the run asked for, "" for the
// host's default model; cli_version is the first N.N.N of what the CLI
// printed for --version, "" when it printed none; eva_md is the commit the
// command was built from, "" when the build does not say, and whether the
// tree had changes; day is the run's UTC date. A pass line's fields are the
// runner's Result's, ms the call's wall time, and refused why the run
// counts the answer refused, or "". end.json's passes counts the lines
// passes.jsonl holds, which is the passes the live run counts but for a
// pass the host answered with its default model: the run stops on it, and
// its line is still written, for its model_note. stopped is a code: a
// runner's reason, such as rate_limit or timeout, model_fallback for that
// stop, interrupted when the run's context ended, or error for anything
// else, which the terminal names. No JSON file holds a path, a time of day
// or anything else of the machine. The raw files hold what the host
// printed, as it printed it: model names, host, session and thread ids,
// the model's reasoning, paths under the home folder, which name the
// account, and whatever its errors say, such as a plan or a rate limit's
// reset time. The folder and every file in it are private to their owner,
// and the folder must lie outside the git work tree the command is run
// from. A trace is handed over privately and never committed as written.
//
// This file holds the format and its writer. It imports neither the suite
// nor its live run, which only main.go may import.

// traceVersion is the trace's format.
const traceVersion = 1

// traceRun is run.json.
type traceRun struct {
	V              int        `json:"v"`
	Host           string     `json:"host"`
	Effort         string     `json:"effort"`
	Model          string     `json:"model"`
	CLIVersion     string     `json:"cli_version"`
	EvaMD          traceBuild `json:"eva_md"`
	TimeoutRefused bool       `json:"timeout_refused"`
	Day            string     `json:"day"`
}

// traceBuild is the build of the command that made the trace.
type traceBuild struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
}

// tracePass is one line of passes.jsonl.
type tracePass struct {
	Script        int    `json:"script"`
	Pass          int    `json:"pass"`
	MS            int64  `json:"ms"`
	TokensIn      int    `json:"tokens_in"`
	TokensOut     int    `json:"tokens_out"`
	Attempts      int    `json:"attempts"`
	EffortUsed    string `json:"effort_used"`
	ModelReported string `json:"model_reported"`
	ModelNote     string `json:"model_note"`
	Degraded      string `json:"degraded"`
	Reason        string `json:"reason"`
	Refused       string `json:"refused"`
}

// traceEnd is end.json.
type traceEnd struct {
	Outcome  string `json:"outcome"`
	Passes   int    `json:"passes"`
	Refused  int    `json:"refused"`
	Timeouts int    `json:"timeouts"`
	Stopped  string `json:"stopped"`
}

// The outcomes end.json names.
const (
	outcomeMet     = "met"
	outcomeMissed  = "missed"
	outcomeStopped = "stopped"
)

// Why a run stopped, in end.json, when no host failure says: its context
// ended, or anything else stopped it, which the terminal names.
const (
	stoppedInterrupted = "interrupted"
	stoppedError       = "error"
)

// Why a run stopped, in end.json, when the host answered with its default
// model in place of the one asked for; the pass's model_note names it.
const stoppedFallback = "model_fallback"

// tracer writes a trace as the run goes.
type tracer struct {
	dir                      string
	made                     string      // the topmost folder createTrace made, "" when dir was there
	mode                     fs.FileMode // the mode of dir when it was there
	passes                   *os.File
	lines, refused, timeouts int
}

// checkTrace refuses a trace folder that holds anything, or is not a
// folder. A folder that does not exist yet is made when the run starts.
func checkTrace(dir string) error {
	names, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if _, lerr := os.Lstat(dir); lerr == nil {
			return errors.New("--trace names a link to nothing")
		}
		return nil
	case err != nil, len(names) != 0:
		return errors.New("--trace must name a folder that is new or empty")
	}
	return nil
}

// overlaps says whether one of two paths is the other or lies under it.
func overlaps(a, b string) bool {
	a, aerr := filepath.Abs(a)
	b, berr := filepath.Abs(b)
	if aerr != nil || berr != nil {
		return false
	}
	return under(a, b) || under(b, a)
}

// insideWorkTree says whether dir lies in the git work tree that holds the
// current folder, the checkout go run is run from: a trace left there is
// one git add from a commit, and marks every later build from it as
// changed.
func insideWorkTree(dir string) bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}
	wd = resolved(wd)
	for top := wd; ; top = filepath.Dir(top) {
		if _, err := os.Lstat(filepath.Join(top, ".git")); err == nil {
			return under(resolved(dir), top)
		}
		if filepath.Dir(top) == top {
			return false
		}
	}
}

// resolved is path made absolute, with the links of the part of it that
// exists resolved, so two names of one folder compare equal.
func resolved(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rest := ""
	for p := abs; ; p = filepath.Dir(p) {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest)
		}
		if filepath.Dir(p) == p {
			return abs
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}

// under says whether path is dir or lies under it.
func under(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// firstMissing is the topmost of dir and the folders above it that does
// not exist, which MkdirAll would make, or "" when dir exists.
func firstMissing(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	top := ""
	for p := abs; ; p = filepath.Dir(p) {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			return top
		}
		top = p
		if filepath.Dir(p) == p {
			return top
		}
	}
}

// createTrace makes the trace folder, private to its owner, with its raw
// folder, and writes run. A folder that was there, which checkTrace found
// empty, is made private too. When it fails, it removes what it made.
func createTrace(dir string, run traceRun) (_ *tracer, err error) {
	t := &tracer{dir: dir, made: firstMissing(dir)}
	defer func() {
		if err != nil {
			t.discard()
		}
	}()
	if t.made == "" {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, err
		}
		t.mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Join(dir, "raw"), 0o700); err != nil {
		return nil, err
	}
	if err := writeSynced(filepath.Join(dir, "run.json"), run); err != nil {
		return nil, err
	}
	if t.passes, err = os.OpenFile(filepath.Join(dir, "passes.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err != nil {
		return nil, err
	}
	return t, nil
}

// discard removes what createTrace made, when the run stops before it
// starts: the folders it made, or, in a folder that was there and empty,
// what it wrote there, and the folder gets its mode back.
func (t *tracer) discard() {
	if t == nil {
		return
	}
	if t.passes != nil {
		t.passes.Close()
	}
	if t.made != "" {
		os.RemoveAll(t.made)
		return
	}
	for _, name := range []string{"raw", "run.json", "passes.jsonl"} {
		os.RemoveAll(filepath.Join(t.dir, name))
	}
	if t.mode != 0 {
		os.Chmod(t.dir, t.mode)
	}
}

// raw writes what the CLI printed on one attempt of a pass, numbered from 1.
func (t *tracer) raw(script, pass, attempt int, stdout, stderr []byte) error {
	name := filepath.Join(t.dir, "raw", fmt.Sprintf("%02d-%02d-%02d", script, pass, attempt))
	if err := writeFile(name+".out", stdout); err != nil {
		return err
	}
	return writeFile(name+".err", stderr)
}

// pass appends a pass's line and syncs it, so a run that stops keeps every
// pass it traced.
func (t *tracer) pass(p tracePass) error {
	line, err := encodeLine(p)
	if err != nil {
		return err
	}
	if _, err := t.passes.Write(line); err != nil {
		return err
	}
	if err := t.passes.Sync(); err != nil {
		return err
	}
	t.lines++
	if p.Refused != "" {
		t.refused++
	}
	if p.Reason == update.ReasonTimeout {
		t.timeouts++
	}
	return nil
}

// end writes end.json, the trace's last file, with the passes it traced,
// and closes the trace.
func (t *tracer) end(outcome, stopped string) error {
	err := writeSynced(filepath.Join(t.dir, "end.json"), traceEnd{Outcome: outcome, Passes: t.lines, Refused: t.refused, Timeouts: t.timeouts, Stopped: stopped})
	return errors.Join(err, t.passes.Close())
}

// refusedWhy is why the run counts a pass's answer refused, or "": the
// runner's reason when it refused the answer, a timeout the run counts
// refused, or the validator's error for an answer, as the live run names
// it, from the batch and page the answer was asked for.
func refusedWhy(res update.Result, b update.Batch, page threadpage.Page, timeoutRefused bool) string {
	switch res.Reason {
	case update.ReasonInvalidOutput, update.ReasonToolUsed:
		return res.Reason
	case update.ReasonTimeout:
		if timeoutRefused {
			return res.Reason
		}
	case "":
		if _, _, err := update.Validate(res.Output, b, page); err != nil {
			return update.ReasonInvalidOutput + ": " + err.Error()
		}
	}
	return ""
}

// passLine is pass n of script's line, from the runner's result and the
// call's wall time.
func passLine(script, n int, res update.Result, took time.Duration, refused string) tracePass {
	return tracePass{Script: script, Pass: n, MS: took.Milliseconds(), TokensIn: res.TokensIn, TokensOut: res.TokensOut, Attempts: res.Attempts,
		EffortUsed: res.Effort, ModelReported: res.ModelReported, ModelNote: res.ModelNote, Degraded: res.Degraded, Reason: res.Reason, Refused: refused}
}

// buildOf is the commit the command was built from, "" when the build
// does not say, and whether the tree it was built from had changes.
func buildOf() traceBuild {
	return buildFrom(debug.ReadBuildInfo())
}

// buildFrom is buildOf from the build's information, when ok.
func buildFrom(info *debug.BuildInfo, ok bool) traceBuild {
	var b traceBuild
	if !ok || info == nil {
		return b
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Commit = s.Value
		case "vcs.modified":
			b.Dirty = s.Value == "true"
		}
	}
	return b
}

// Time limits on <cli> --version, read for run.json: the read, and the
// wait for its output once it has exited or run out of time. Tests raise
// them.
var (
	versionTimeout   = 10 * time.Second
	versionWaitDelay = time.Second
)

// cliVersion is the version of host's CLI the run starts, cli when it is
// given and the one on PATH otherwise, read as the host adapter reads it:
// the first N.N.N of what <cli> --version prints, run from /. It is "" when
// none is read.
func cliVersion(ctx context.Context, host, cli string) string {
	lookPath := exec.LookPath
	if cli != "" {
		lookPath = func(string) (string, error) { return cli, nil }
	}
	env := adapter.Env{LookPath: lookPath, Run: func(_ context.Context, path string, args ...string) ([]byte, error) {
		// The adapter's own limit on the read is not one tests can raise;
		// this one is, and the run's context still ends it.
		ctx, cancel := context.WithTimeout(ctx, versionTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Dir = "/"
		cmd.WaitDelay = versionWaitDelay
		out := &capped{limit: 64 << 10}
		cmd.Stdout = out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		return out.buf.Bytes(), nil
	}}
	return adapter.Probe(ctx, adapter.ID(host), env).Version
}

// capped keeps the first limit bytes written to it and drops the rest. Its
// buffer is a named field, so os/exec writes through Write.
type capped struct {
	buf   bytes.Buffer
	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if room := c.limit - c.buf.Len(); n > room {
		p = p[:max(room, 0)]
	}
	c.buf.Write(p)
	return n, nil
}

// encodeLine is v as one JSON line, with no HTML escaping.
func encodeLine(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeSynced writes v as one JSON line to a new file at path.
func writeSynced(path string, v any) error {
	line, err := encodeLine(v)
	if err != nil {
		return err
	}
	return writeFile(path, line)
}

// writeFile writes data to a new file at path, private to its owner, and
// syncs it.
func writeFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
