package drive

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// A trace (--trace) keeps what each call of a host's CLI the driver
// starts printed, the version's and each chat call's, so that a wrong
// reading of a real host's output, such as a call read as giving no
// usage, a rate limit read as a host's failure or a model not found, can
// be mended from the run. Each call writes two files, its stdout and its
// stderr as the driver read them and within the limit it reads stdout by:
//
//	<trace>/<run>/<task>/<condition>/t<n>/<k>-version.stdout, .stderr
//	<trace>/<run>/<task>/<condition>/t<n>/<k>-chat.stdout, .stderr
//
// where k counts the trial's calls from 1. The folder is new or empty,
// made for the driver's user alone (0700, its files 0600), and lies
// outside --work, --out, --tasks, --repos and the git checkouts of --repo
// and of the folder the driver is run from, since what a host prints
// holds a model's text and may hold paths: it is never committed. Nothing
// of a trace reaches a row, the run's record, the output or an error. A
// trace that cannot be written stops the run before its next call, as a
// row that cannot be written does.

// Kinds of the calls a trace keeps.
const (
	traceVersion = "version"
	traceChat    = "chat"
)

// checkTrace refuses --trace when it is not a folder that is new or empty,
// or lies inside, or holds, --work, --out, --tasks or --repos, or lies in
// the git checkout that holds --repo or the folder the driver is run from:
// by name with links resolved, and by the folders themselves. It sets
// p.trace to the folder with its links resolved. It writes nothing.
func (p *plan) checkTrace() error {
	o := p.opts
	if o.Trace == "" {
		return nil
	}
	dir, err := resolve(o.Trace)
	if err != nil {
		return usagef("--trace cannot be read")
	}
	for _, f := range []struct{ name, path string }{
		{"--work", p.work}, {"--out", o.Out}, {"--tasks", o.Tasks}, {"--repos", o.Repos},
	} {
		other, err := resolve(f.path)
		if err != nil {
			return usagef("%s cannot be read", f.name)
		}
		if insideFolder(o.Trace, f.path) || within(other, dir) {
			return usagef("--trace overlaps %s", f.name)
		}
	}
	repo := o.Repo
	if repo == "" {
		repo = "."
	}
	cwd, err := os.Getwd()
	if err != nil {
		return usagef("the folder the driver is run from cannot be read")
	}
	for _, from := range []string{repo, cwd} {
		if top := checkoutOf(from); top != "" && insideFolder(o.Trace, top) {
			return usagef("--trace is in a git checkout, where it could be committed")
		}
	}
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if _, lerr := os.Lstat(dir); lerr == nil {
			return usagef("--trace is a link to nothing")
		}
	case err != nil:
		return usagef("--trace is not a folder that can be read")
	case len(entries) > 0:
		return usagef("--trace is not empty")
	}
	p.trace = dir
	return nil
}

// checkoutOf is the git checkout that holds the folder from, links
// resolved: the nearest folder at or above it that holds .git, or "" when
// none does.
func checkoutOf(from string) string {
	dir, err := resolve(from)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// insideFolder reports whether path is dir or lies inside it, by name, both
// with their links resolved as far as they exist, or by the folder each
// name reaches: a folder can have a second name that no link gives, such
// as a spelling in another letter case on a file system that ignores case,
// a firmlink or a bind mount. The path is walked up both as given, made
// absolute, and with its links resolved.
func insideFolder(path, dir string) bool {
	p, perr := resolve(path)
	d, derr := resolve(dir)
	if perr == nil && derr == nil && within(p, d) {
		return true
	}
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	starts := []string{abs}
	if perr == nil {
		starts = append(starts, p)
	}
	for _, at := range starts {
		for {
			if i, err := os.Stat(at); err == nil && os.SameFile(i, info) {
				return true
			}
			parent := filepath.Dir(at)
			if parent == at {
				break
			}
			at = parent
		}
	}
	return false
}

// makeTrace makes the trace folder, for its owner alone, once the run is
// to start. One that was there, which checkTrace found empty, is closed to
// others too.
func makeTrace(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return pathless(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return pathless(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return pathless(err)
	}
	if len(entries) > 0 {
		return errors.New("it is no longer empty")
	}
	return nil
}

// callTrace is where a trial's calls are kept; one with no folder keeps
// none.
type callTrace struct {
	dir  string // the trial's folder of the trace, "" for none
	n    int    // the calls kept so far
	open func(path string) (io.WriteCloser, error)
}

// newCallTrace makes the trace's folder of the trial n of task t in the
// run, when the run keeps a trace.
func (p *plan) newCallTrace(run string, t task.Task, n int) (*callTrace, error) {
	c := &callTrace{open: p.opts.openTrace}
	if p.trace == "" {
		return c, nil
	}
	if c.open == nil {
		c.open = func(path string) (io.WriteCloser, error) {
			return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		}
	}
	c.dir = filepath.Join(p.trace, run, t.ID, string(p.opts.Condition), "t"+strconv.Itoa(n))
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return nil, traceFault(err)
	}
	return c, nil
}

// start opens the files of the trial's next call, of the kind given, or
// gives nil when the trial keeps no trace.
func (c *callTrace) start(kind string) (*callFiles, error) {
	if c == nil || c.dir == "" {
		return nil, nil
	}
	c.n++
	name := filepath.Join(c.dir, strconv.Itoa(c.n)+"-"+kind)
	stdout, err := c.open(name + ".stdout")
	if err != nil {
		return nil, traceFault(err)
	}
	stderr, err := c.open(name + ".stderr")
	if err != nil {
		_ = stdout.Close()
		return nil, traceFault(err)
	}
	return &callFiles{stdout: stdout, stderr: stderr}, nil
}

// callFiles are the two files one call's output is kept in.
type callFiles struct {
	stdout, stderr io.WriteCloser
}

// traceFault is the error of a trace that could not be written: what
// failed, with no path.
func traceFault(err error) error {
	return fmt.Errorf("the trace: %w", pathless(err))
}
