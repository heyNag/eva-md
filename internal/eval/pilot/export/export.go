// Package export writes the pilot's tasks in the format the continuation
// driver reads (EV-03b), so the pilot runs on that driver. It is
// throwaway: it is deleted with the pilot's tasks when the continuation's
// own tasks land (EV-17e), and no release builds it.
//
// An export checks each task it is given as eva-eval pilot check does,
// converts it and checks the result with the continuation's own Validate,
// and only then writes, into a folder that did not exist or was empty:
//
//	tasks/<id>/task.json   the task in the continuation's format, without
//	                       the pilot's v, lang and minutes, and no limit_s
//	tasks/<id>/hidden/     the overlays, copied byte for byte; a hidden
//	tasks/<id>/solution/   decision's decoy/ too, and no start/, since the
//	tasks/<id>/decoy/      repositories are as the first chat left them
//	repos/<name>/          each task's repos/<name>/, copied byte for byte
//
// The folder is left with mode 0700, and what is under it is its owner's
// alone: the run-book runs a chat as a second user, who must not read it.
//
// No message names a value a task plants, a repository or a path.
package export

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	contask "github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/pilot/task"
)

// Options says what to export, and where.
type Options struct {
	Tasks string        // the pilot's tasks folder
	Out   string        // the folder written, which must not exist or be an empty folder
	IDs   []string      // the tasks to export, by id; every task when empty
	Limit time.Duration // bounds each test run of the pilot's check
}

// Outcome is what became of one task. A task is exported when Rule,
// Unchecked and Format are all unset and Report.Written is true.
type Outcome struct {
	ID     string
	Family string
	// Rule is a rule of the pilot's that the task breaks, which Load or
	// Check found.
	Rule *task.Error
	// Unchecked is why the pilot's check could not run on the task.
	Unchecked error
	// Format is why the continuation's format refuses the converted task.
	Format string
}

// Refused reports whether the task was refused or could not be checked.
func (o Outcome) Refused() bool { return o.Rule != nil || o.Unchecked != nil || o.Format != "" }

// Report is what an export did.
type Report struct {
	Tasks   []Outcome // one a task named, by id
	Written bool      // every task passed, and the folder was written
	Minutes int       // when written, the largest minutes of the tasks exported
}

// UsageError is an export refused before any task was checked: nothing is
// written.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

func usage(format string, args ...any) error { return &UsageError{fmt.Sprintf(format, args...)} }

// Export checks the tasks o names and, when every one passes, writes them
// into o.Out. A task a rule refuses is in the report, and nothing is
// written; a *UsageError is an export refused before any check; any other
// error is one that stopped it, after which nothing it wrote is left.
func Export(ctx context.Context, o Options) (Report, error) {
	if o.Out == "" {
		return Report{}, usage("the export needs a folder to write")
	}
	// Cleaned, a link given with a slash after it is read as the link it
	// is, not as the folder it leads to.
	o.Out = filepath.Clean(o.Out)
	existed, err := outFolder(o.Out, o.Tasks)
	if err != nil {
		return Report{}, err
	}
	tasks, refused, err := load(o.Tasks)
	if err != nil {
		return Report{}, err
	}
	tasks, refused, err = named(tasks, refused, o.IDs)
	if err != nil {
		return Report{}, err
	}
	if err := oneOwnerEach(tasks); err != nil {
		return Report{}, err
	}

	scratch, err := os.MkdirTemp("", "eva-eval-pilot-")
	if err != nil {
		return Report{}, fmt.Errorf("cannot make a scratch folder: %w", pathless(err))
	}
	defer os.RemoveAll(scratch)
	if inside(scratch, o.Out) {
		return Report{}, usage("the scratch folder, in TMPDIR, would be inside the folder to write")
	}

	var r Report
	for _, e := range refused {
		r.Tasks = append(r.Tasks, Outcome{ID: e.Task, Family: e.Family, Rule: e})
	}
	ok := true
	for _, t := range tasks {
		out := Outcome{ID: t.ID, Family: t.Family}
		var te *task.Error
		switch err := task.Check(ctx, t, filepath.Join(scratch, t.ID), o.Limit); {
		case errors.As(err, &te):
			out.Rule = te
		case err != nil:
			out.Unchecked = err
		default:
			if err := Convert(t).Validate(); err != nil {
				out.Format = message(t.ID, err)
			}
		}
		ok = ok && !out.Refused()
		r.Tasks = append(r.Tasks, out)
	}
	slices.SortFunc(r.Tasks, func(a, b Outcome) int { return strings.Compare(a.ID, b.ID) })
	if !ok || len(refused) > 0 {
		return r, nil
	}
	if err := write(o.Out, existed, tasks, copyFile); err != nil {
		return Report{}, err
	}
	r.Written = true
	for _, t := range tasks {
		r.Minutes = max(r.Minutes, t.Minutes)
	}
	return r, nil
}

// outFolder refuses an out folder that is a link, is not a folder, is not
// empty, is in no folder that exists, or is or lies inside the tasks
// folder, links resolved. It reports whether the folder exists.
func outFolder(out, tasks string) (bool, error) {
	info, err := os.Lstat(out)
	existed := err == nil
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if parent, err := os.Stat(filepath.Dir(out)); err != nil || !parent.IsDir() {
			return false, usage("the folder to write is in no folder that exists")
		}
	case err != nil:
		return false, fmt.Errorf("cannot read the folder to write: %w", pathless(err))
	case info.Mode()&fs.ModeSymlink != 0:
		return false, usage("the folder to write is a link")
	case !info.IsDir():
		return false, usage("the folder to write is not a folder")
	default:
		entries, err := os.ReadDir(out)
		if err != nil {
			return false, fmt.Errorf("cannot list the folder to write: %w", pathless(err))
		}
		if len(entries) > 0 {
			return false, usage("the folder to write is not empty")
		}
	}
	if inside(out, tasks) {
		return false, usage("the folder to write lies inside the tasks folder")
	}
	return existed, nil
}

// load reads the tasks folder with the pilot's Load, and returns the tasks
// it takes and those whose rule it names. Any other error is the folder's.
func load(dir string) ([]task.Task, []*task.Error, error) {
	tasks, err := task.Load(dir)
	if err == nil {
		return tasks, nil, nil
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		return nil, nil, err
	}
	var refused []*task.Error
	for _, e := range joined.Unwrap() {
		var te *task.Error
		if !errors.As(e, &te) {
			return nil, nil, e
		}
		refused = append(refused, te)
	}
	return tasks, refused, nil
}

// named keeps the tasks and refusals whose id is one of ids, or every one
// when ids is empty. An id that names nothing in the folder is refused,
// and not shown, since it is the caller's own text.
func named(tasks []task.Task, refused []*task.Error, ids []string) ([]task.Task, []*task.Error, error) {
	if len(ids) == 0 {
		return tasks, refused, nil
	}
	for _, id := range ids {
		if !slices.ContainsFunc(tasks, func(t task.Task) bool { return t.ID == id }) &&
			!slices.ContainsFunc(refused, func(e *task.Error) bool { return e.Task == id }) {
			return nil, nil, usage("a task named by --task is not in the tasks folder")
		}
	}
	tasks = slices.DeleteFunc(slices.Clone(tasks), func(t task.Task) bool { return !slices.Contains(ids, t.ID) })
	refused = slices.DeleteFunc(slices.Clone(refused), func(e *task.Error) bool { return !slices.Contains(ids, e.Task) })
	return tasks, refused, nil
}

// oneOwnerEach refuses two tasks that name one repository: the export's
// repos/ folder, which the driver reads as one --repos, holds each once.
func oneOwnerEach(tasks []task.Task) error {
	owner := map[string]string{}
	for _, t := range tasks {
		for _, r := range t.Repos {
			if other, ok := owner[r]; ok {
				return usage("tasks %s and %s name one repository, and the folder of repositories holds each once", other, t.ID)
			}
			owner[r] = t.ID
		}
	}
	return nil
}

// Convert is the pilot's task t in the continuation's format: every field
// the pilot's has but v, lang and minutes, with no time limit on a test,
// and Dir empty.
func Convert(t task.Task) contask.Task {
	c := contask.Task{
		ID:        t.ID,
		Family:    t.Family,
		Repos:     slices.Clone(t.Repos),
		Workdir:   t.Workdir,
		Prompt:    t.Prompt,
		Note:      t.Note,
		MaxTurns:  t.MaxTurns,
		MaxRounds: t.MaxRounds,
	}
	for _, turn := range t.History {
		c.History = append(c.History, contask.Turn{By: turn.By, Text: turn.Text, Plants: slices.Clone(turn.Plants)})
	}
	for _, f := range t.Facts {
		c.Facts = append(c.Facts, contask.Fact{ID: f.ID, Statement: f.Statement, Values: slices.Clone(f.Values), Name: f.Name})
	}
	for _, x := range t.Tests {
		c.Tests = append(c.Tests, contask.Test{ID: x.ID, Repo: x.Repo, Run: slices.Clone(x.Run), Facts: slices.Clone(x.Facts)})
	}
	return c
}

// message is the continuation's error for the task id without the prefix
// that names the task, which the caller's line names already.
func message(id string, err error) string {
	msg := err.Error()
	for _, prefix := range []string{"task " + id + ": ", "task: "} {
		if rest, ok := strings.CutPrefix(msg, prefix); ok {
			return rest
		}
	}
	return msg
}

// The overlays an export copies, beside task.json.
var overlays = []string{contask.OverlayHidden, contask.OverlaySolution, contask.OverlayDecoy}

// write writes the export of tasks into out, made with mode 0700 when it
// did not exist and set to it when it did, copying each file with cp. When
// a step fails, what it wrote is removed, and a folder that existed gets
// its mode back.
func write(out string, existed bool, tasks []task.Task, cp func(src, dst string) error) (err error) {
	var mode fs.FileMode
	if existed {
		info, err := os.Lstat(out)
		if err != nil || !info.IsDir() {
			return errors.New("the folder to write changed while the tasks were checked")
		}
		if entries, err := os.ReadDir(out); err != nil || len(entries) > 0 {
			return errors.New("the folder to write changed while the tasks were checked")
		}
		mode = info.Mode().Perm()
	} else if err := os.Mkdir(out, 0o700); err != nil {
		return fmt.Errorf("cannot make the folder to write: %w", pathless(err))
	}
	defer func() {
		if err == nil {
			return
		}
		if !existed {
			os.RemoveAll(out)
			return
		}
		os.RemoveAll(filepath.Join(out, "tasks"))
		os.RemoveAll(filepath.Join(out, "repos"))
		os.Chmod(out, mode)
	}()
	if err := os.Chmod(out, 0o700); err != nil {
		return fmt.Errorf("cannot set the mode of the folder to write: %w", pathless(err))
	}
	for _, dir := range []string{"tasks", "repos"} {
		if err := os.Mkdir(filepath.Join(out, dir), 0o700); err != nil {
			return fmt.Errorf("cannot write the export: %w", pathless(err))
		}
	}
	for _, t := range tasks {
		if err := writeTask(out, t, cp); err != nil {
			return fmt.Errorf("cannot write task %s: %w", t.ID, err)
		}
	}
	return nil
}

// writeTask writes the task t's folder and repositories into out.
func writeTask(out string, t task.Task, cp func(src, dst string) error) error {
	for _, name := range append([]string{t.ID}, t.Repos...) {
		if name != filepath.Base(name) || !filepath.IsLocal(name) {
			return errors.New("an id or a repository's name is not one name")
		}
	}
	dir := filepath.Join(out, "tasks", t.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return pathless(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Convert(t)); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(dir, contask.FileName), &buf, 0o600); err != nil {
		return pathless(err)
	}
	for _, o := range overlays {
		if o == contask.OverlayDecoy && t.Family != task.FamilyHidden {
			continue
		}
		if err := copyTree(filepath.Join(t.Dir, o), filepath.Join(dir, o), cp); err != nil {
			return fmt.Errorf("its %s overlay: %w", o, err)
		}
	}
	for i, r := range t.Repos {
		if err := copyTree(filepath.Join(t.Dir, "repos", r), filepath.Join(out, "repos", r), cp); err != nil {
			return fmt.Errorf("its repository %d: %w", i+1, err)
		}
	}
	return nil
}

// copyTree copies the folder src, which may hold only folders and plain
// files, to dst, which must not exist, with cp for each file. Its errors
// carry no path.
func copyTree(src, dst string, cp func(src, dst string) error) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return pathless(err)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || !filepath.IsLocal(rel) && rel != "." {
			return errors.New("a path leaves the folder copied")
		}
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return pathless(os.Mkdir(to, 0o700))
		case d.Type().IsRegular():
			return pathless(cp(path, to))
		}
		return errors.New("the folder copied holds a link or a file that is not plain")
	})
}

// copyFile streams the plain file src, never through a link, to the new
// file dst, which is executable, for its owner alone, exactly when src is
// executable by anyone.
func copyFile(src, dst string) error {
	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("a file copied is not plain")
	}
	perm := fs.FileMode(0o600)
	if info.Mode().Perm()&0o111 != 0 {
		perm = 0o700
	}
	return writeNew(dst, in, perm)
}

// writeNew writes r to the new file path, with exactly the mode perm.
func writeNew(path string, r io.Reader, perm fs.FileMode) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(perm); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// inside reports whether path is dir or lies inside it, both made absolute
// with their links resolved as far as they exist.
func inside(path, dir string) bool {
	p, errP := resolved(path)
	d, errD := resolved(dir)
	if errP != nil || errD != nil {
		return false
	}
	rel, err := filepath.Rel(d, p)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// resolved returns path made absolute with its links resolved; of a path
// that does not exist yet, the part that does is resolved.
func resolved(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, fs.ErrNotExist) && filepath.Dir(abs) != abs {
		parent, err := resolved(filepath.Dir(abs))
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, filepath.Base(abs)), nil
	}
	return real, err
}

// pathless returns err without the path an fs or link error carries, which
// can hold a repository's name, or nil for nil.
func pathless(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return fmt.Errorf("%s: %w", le.Op, le.Err)
	}
	return err
}
