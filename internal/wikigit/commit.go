package wikigit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// Outcomes of a run, as its record names them.
const (
	Committed = "committed"
	Nothing   = "nothing"
	Refused   = "refused"
	Failed    = "failed"
	Busy      = "busy"
)

// Push results, as a record names them.
const (
	PushOK       = "ok"
	PushUpToDate = "up_to_date"
	PushPending  = "pending"  // no network, no approval in time or another failure; retried later
	PushDiverged = "diverged" // the remote has commits this clone lacks; fixed by hand
	PushFailed   = "failed"   // the remote refused the push
	PushSkipped  = "skipped"
)

// ReasonPushOnly marks the record of a Push run, which commits nothing.
const ReasonPushOnly = "push_only"

// Time limits. A commit may wait minutes for a signing approval, such as
// a password manager's, and git must not be killed under it.
const (
	gitTimeout    = 120 * time.Second
	commitTimeout = 300 * time.Second
	pushTimeout   = 60 * time.Second
)

// wikiLockWait bounds the wait for a running page write to finish.
var wikiLockWait = 60 * time.Second

// commitPaths are the parts of the wiki a commit stages.
var commitPaths = []string{"projects", "general", "systems", "index.md", "log.md", "README.md", ".eva-md"}

// Options names the wiki to commit and where eva-md keeps its locks and
// records.
type Options struct {
	Config config.Config    // the wiki path, its expected remote and its instance
	Dirs   paths.Dirs       // the locks and the records
	Now    func() time.Time // the clock; time.Now when nil
}

// Result is what a run did. It is also the record each run appends to
// <state>/commits/<YYYY-MM>.jsonl, so it holds counts, reasons and
// wiki-relative paths only.
type Result struct {
	At             string   `json:"at"`
	Outcome        string   `json:"outcome"` // Committed, Nothing, Refused, Failed or Busy
	Reason         string   `json:"reason"`
	Pages          int      `json:"pages"`
	ValidateErrors int      `json:"validate_errors"`
	Unstaged       []string `json:"unstaged"` // paths left out of the commit
	Push           string   `json:"push"`
	LockAge        int64    `json:"lock_age_s,omitempty"` // the age of the index.lock a refusal names, in seconds
}

// Commit commits the wiki's pages with the user's own signing settings and
// pushes the commit (see Push). It refuses, with a reason and no commit, a
// wiki whose origin or instance is not the config's, an index.lock (which it
// never deletes), a rebase, merge or cherry-pick in progress and a detached
// HEAD. It leaves out paths outside the wiki's pages, symlinks, JSONL, inbox
// and temporary files and any file whose added lines hold a secret,
// recording their paths, and counts okf's validation errors without stopping
// for them. With nothing to commit, it still pushes when HEAD is ahead of
// its upstream branch, as after a pending push. Every run appends its
// record, which Commit returns; the error reports a record that could not be
// written.
func Commit(ctx context.Context, o Options) (Result, error) {
	return locked(o, "", func(r *Result, now time.Time) { commit(ctx, o, r, now) })
}

// Push pushes commits made earlier, as eva-md commit --push-only does, after
// the same origin, instance and detached HEAD checks. It runs `git push
// origin HEAD`, never forced, within 60 s. A remote with commits this clone
// lacks is diverged and left for a person to fix; a refusal by the remote is
// failed; any other failure, such as no network or no approval in time, is
// pending. Its record has the reason ReasonPushOnly, a busy run's too.
func Push(ctx context.Context, o Options) (Result, error) {
	return locked(o, ReasonPushOnly, func(r *Result, now time.Time) {
		r.Outcome = Nothing
		if guard(ctx, o, r, now, false) {
			r.Push = push(ctx, o.Config.WikiPath)
		}
	})
}

// locked runs fn under <locks>/commit.lock, which it doesn't wait for: a run
// that finds it held is busy. It appends the run's record, which starts with
// reason.
func locked(o Options, reason string, fn func(*Result, time.Time)) (Result, error) {
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	t := now()
	r := Result{At: t.Format(time.RFC3339), Reason: reason, Unstaged: []string{}, Push: PushSkipped}
	unlock, busy, err := commitLock(o.Dirs.Locks())
	switch {
	case busy:
		r.Outcome = Busy
	case err != nil:
		r.Outcome, r.Reason = Failed, "lock"
	default:
		defer unlock()
		fn(&r, t)
	}
	return r, appendRecord(o.Dirs.State, t, r)
}

func commit(ctx context.Context, o Options, r *Result, now time.Time) {
	unlock, busy, err := wikiLock(ctx, o.Dirs.Locks())
	if err != nil {
		r.Outcome, r.Reason = Failed, "lock"
		if busy {
			r.Outcome, r.Reason = Busy, "wiki_lock"
		}
		return
	}
	staged, ok := stage(ctx, o, r, now)
	// The index now holds the commit. Writes may go on while git waits for
	// a signing approval; they change files, not the index.
	unlock()
	if !ok {
		return
	}
	wiki := o.Config.WikiPath
	if len(staged) == 0 {
		r.Outcome = Nothing
		if ahead(ctx, wiki) {
			r.Push = push(ctx, wiki)
		}
		return
	}
	msg := fmt.Sprintf("eva-md daily %s: %s", now.Format("2006-01-02"), pagesText(r.Pages))
	if _, err := Run(ctx, commitTimeout, wiki, "commit", "--quiet", "-m", msg); err != nil {
		r.Outcome, r.Reason = Failed, "commit"
		if e := (*Error)(nil); errors.As(err, &e) && e.Class != "" {
			r.Reason = "commit_" + e.Class
		}
		return
	}
	r.Outcome = Committed
	r.Push = push(ctx, wiki)
}

// stage checks the wiki and stages the day's changes, leaving out what must
// not be committed. It returns the staged paths, or false when the run ends
// here, with r saying why.
func stage(ctx context.Context, o Options, r *Result, now time.Time) ([]string, bool) {
	if !guard(ctx, o, r, now, true) {
		return nil, false
	}
	wiki := o.Config.WikiPath
	fail := func(reason string) ([]string, bool) { r.Outcome, r.Reason = Failed, reason; return nil, false }
	args := []string{"add", "--"}
	for _, p := range commitPaths {
		if _, err := os.Lstat(filepath.Join(wiki, p)); err == nil {
			args = append(args, p)
		}
	}
	if len(args) > 2 {
		if _, err := Run(ctx, gitTimeout, wiki, args...); err != nil {
			return fail("add")
		}
	}
	files, err := stagedFiles(ctx, wiki)
	if err != nil {
		return fail("staged")
	}
	var out, scan []string
	for _, f := range files {
		if leftOut(f) {
			out = append(out, f.path)
		} else {
			scan = append(scan, f.path)
		}
	}
	if len(scan) > 0 {
		r.ValidateErrors = validationErrors(o)
	}
	for _, p := range scan {
		if leaks(ctx, wiki, p) {
			out = append(out, p)
		}
	}
	if err := unstage(ctx, wiki, out); err != nil {
		return fail("unstage")
	}
	// Read the index again: what it holds is what git commits.
	if files, err = stagedFiles(ctx, wiki); err != nil {
		return fail("staged")
	}
	var staged []string
	for _, f := range files {
		if leftOut(f) || slices.Contains(out, f.path) {
			return fail("unstage")
		}
		staged = append(staged, f.path)
	}
	slices.Sort(out)
	r.Unstaged = append(r.Unstaged, out...)
	r.Pages = pageCount(staged)
	return staged, true
}

// guard reports whether the run may go on. Otherwise it refuses, with a
// reason: a wiki that is not a git clone of its own, an origin or a push URL
// that is not the configured remote (compared as init compares them), an
// instance in .eva-md/schema.toml that is not the config's, before a commit
// an index.lock or a rebase, merge or cherry-pick in progress, and a
// detached HEAD, where a commit would be on no branch and git can't push
// HEAD.
func guard(ctx context.Context, o Options, r *Result, now time.Time, committing bool) bool {
	refuse := func(reason string) bool { r.Outcome, r.Reason = Refused, reason; return false }
	wiki := o.Config.WikiPath
	gitDir := filepath.Join(wiki, ".git")
	if info, err := os.Lstat(gitDir); !filepath.IsAbs(wiki) || err != nil || !info.IsDir() {
		return refuse("wiki")
	}
	if !sameRemote(ctx, wiki, o.Config.ExpectedRemote) {
		return refuse("origin")
	}
	schema, err := config.LoadSchema(filepath.Join(wiki, ".eva-md", "schema.toml"))
	if err != nil || schema.Instance == "" || schema.Instance != o.Config.Instance {
		return refuse("instance")
	}
	// A wiki a newer eva-md set up is read-only to this one.
	if schema.SchemaVersion != 1 {
		return refuse("schema")
	}
	if committing {
		// A lock git left behind, or one a running git holds. Only a person
		// can tell which, so it is never deleted.
		if info, err := os.Lstat(filepath.Join(gitDir, "index.lock")); err == nil {
			r.LockAge = int64(now.Sub(info.ModTime()) / time.Second)
			return refuse("index_lock")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return refuse("index_lock")
		}
		for _, s := range []struct{ name, reason string }{{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"}, {"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry_pick"}} {
			if _, err := os.Lstat(filepath.Join(gitDir, s.name)); !errors.Is(err, fs.ErrNotExist) {
				return refuse(s.reason)
			}
		}
	}
	if _, err := Run(ctx, gitTimeout, wiki, "symbolic-ref", "--quiet", "HEAD"); err != nil {
		return refuse("detached")
	}
	return true
}

// sameRemote reports whether the wiki's origin, and every URL it pushes to,
// is the configured remote.
func sameRemote(ctx context.Context, wiki, expected string) bool {
	if expected == "" {
		return false
	}
	origin, err := Run(ctx, gitTimeout, wiki, "config", "--get", "remote.origin.url")
	if err != nil {
		return false
	}
	pushURLs, err := Run(ctx, gitTimeout, wiki, "remote", "get-url", "--push", "--all", "origin")
	if err != nil {
		return false
	}
	for _, u := range append([]string{origin}, strings.Split(pushURLs, "\n")...) {
		if id, ok := remoteID(u); !ok || id != expected {
			return false
		}
	}
	return true
}

// remoteID is a remote's identity as RemoteIdentity gives it, except that a
// local remote that doesn't exist now, such as one on a volume that isn't
// mounted, is named by its path: git can't push to it, so the push is
// pending, not refused.
func remoteID(u string) (string, bool) {
	if _, id, err := RemoteIdentity(u); err == nil {
		return id, true
	}
	if !filepath.IsAbs(u) {
		return "", false
	}
	_, err := filepath.EvalSymlinks(u)
	return "file://" + filepath.ToSlash(filepath.Clean(u)), errors.Is(err, fs.ErrNotExist)
}

// stagedFile is a path whose staged content differs from HEAD, with the
// mode it is staged with: 120000 for a symlink, 160000 for a submodule.
type stagedFile struct{ path, mode string }

// stagedFiles lists the paths whose staged content differs from HEAD.
func stagedFiles(ctx context.Context, wiki string) ([]stagedFile, error) {
	var out limitedBuffer
	if err := run(ctx, gitTimeout, wiki, &out, "diff", "--cached", "--raw", "--no-renames", "-z"); err != nil {
		return nil, err
	}
	// Each entry is ":<old mode> <new mode> <old id> <new id> <status>",
	// then its path.
	var files []stagedFile
	fields := strings.Split(out.String(), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		header := strings.Fields(fields[i])
		if len(header) != 5 || !strings.HasPrefix(header[0], ":") {
			return nil, errors.New("wikigit: unexpected git diff output")
		}
		files = append(files, stagedFile{path: fields[i+1], mode: header[1]})
	}
	return files, nil
}

// leftOut reports a staged file that is never committed: a path outside the
// commit paths, a symlink or a submodule, which eva-md never writes (git
// would commit a symlink's target, a local path, as its content), a JSONL
// file, a path in the inbox folder, or a temporary file of okf's or
// eva-md's.
func leftOut(f stagedFile) bool {
	p, base := f.path, path.Base(f.path)
	inside := slices.ContainsFunc(commitPaths, func(c string) bool { return p == c || strings.HasPrefix(p, c+"/") })
	return !inside || f.mode == "120000" || f.mode == "160000" ||
		strings.HasSuffix(strings.ToLower(p), ".jsonl") || strings.HasPrefix(p, "inbox/") ||
		strings.HasPrefix(base, ".tmp-okf-") || strings.HasPrefix(base, ".tmp-eva-")
}

// unstage resets paths in the index to HEAD, leaving their files as they
// are.
func unstage(ctx context.Context, wiki string, files []string) error {
	for len(files) > 0 {
		n := min(len(files), 100)
		args := []string{"reset", "--quiet", "--"}
		for _, f := range files[:n] {
			args = append(args, literal(f))
		}
		if _, err := Run(ctx, gitTimeout, wiki, args...); err != nil {
			return err
		}
		files = files[n:]
	}
	return nil
}

// literal makes a path a pathspec git reads as it is, never as a pattern.
func literal(p string) string { return ":(literal)" + p }

// validationErrors runs okf Validate as eva-md validate runs it and counts
// its errors. A wiki okf can't load counts as one.
func validationErrors(o Options) int {
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		return 1
	}
	v, err := w.Validate()
	if err != nil {
		return 1
	}
	return len(v.Errors)
}

// pageCount counts the concept pages among paths: Markdown files other than
// the index files and the root log.
func pageCount(files []string) int {
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, ".md") && path.Base(f) != "index.md" && f != "log.md" {
			n++
		}
	}
	return n
}

func pagesText(n int) string {
	if n == 1 {
		return "1 page"
	}
	return fmt.Sprintf("%d pages", n)
}

// ahead reports whether HEAD has commits its upstream branch lacks, as far
// as this clone knows: a commit whose push is pending, or one a run made
// before it was stopped. It reads local refs only.
func ahead(ctx context.Context, wiki string) bool {
	n, err := Run(ctx, gitTimeout, wiki, "rev-list", "--count", "@{upstream}..HEAD")
	return err == nil && n != "0"
}

// push pushes HEAD to its branch on origin and reads git's porcelain status
// of the ref.
func push(ctx context.Context, wiki string) string {
	var out limitedBuffer
	err := run(ctx, pushTimeout, wiki, &out, "push", "--porcelain", "origin", "HEAD")
	return pushStatus(out.String(), err)
}

// pushStatus reads `git push --porcelain` output, whose ref lines are
// "<flag>\t<from>:<to>\t<summary>": '!' rejected, '=' up to date, ' ' or '*'
// pushed.
func pushStatus(out string, err error) string {
	pushed, current := false, false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || len(fields[0]) != 1 {
			continue
		}
		switch fields[0] {
		case "!":
			if strings.HasPrefix(fields[2], "[rejected]") {
				return PushDiverged
			}
			return PushFailed
		case "=":
			current = true
		case " ", "*":
			pushed = true
		}
	}
	switch {
	case err != nil:
		return PushPending
	case pushed:
		return PushOK
	case current:
		return PushUpToDate
	}
	return PushPending
}

// commitLock takes <locks>/commit.lock without waiting; busy reports that
// another run holds it.
func commitLock(dir string) (unlock func(), busy bool, err error) {
	f, err := lockFile(dir, "commit.lock")
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.Is(err, syscall.EWOULDBLOCK), errors.New("wikigit: commit lock is busy")
	}
	return release(f), false, nil
}

// wikiLock takes the lock okfadapter takes around every page write,
// <locks>/wiki.lock, waiting up to wikiLockWait for a running write; busy
// reports that it was still held.
func wikiLock(ctx context.Context, dir string) (unlock func(), busy bool, err error) {
	f, err := lockFile(dir, "wiki.lock")
	if err != nil {
		return nil, false, err
	}
	deadline := time.Now().Add(wikiLockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return release(f), false, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			return nil, false, errors.New("wikigit: wiki lock failed")
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			f.Close()
			return nil, true, errors.New("wikigit: wiki lock is busy")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// lockFile opens a lock file in dir, creating both, and refuses a symlink
// or anything else that is not a regular file.
func lockFile(dir, name string) (*os.File, error) {
	root, err := paths.OpenRoot(dir, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("wikigit: invalid lock file")
	}
	return f, nil
}

func release(f *os.File) func() {
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }
}

// appendRecord appends r to the month's record file,
// <state>/commits/<YYYY-MM>.jsonl, creating the folder with mode 0700 and
// the file with mode 0600.
func appendRecord(state string, t time.Time, r Result) error {
	root, err := paths.OpenRoot(filepath.Join(state, "commits"), true)
	if err != nil {
		return fmt.Errorf("wikigit: record: %w", err)
	}
	defer root.Close()
	f, err := root.OpenFile(t.Format("2006-01")+".jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("wikigit: record: %w", err)
	}
	line, err := json.Marshal(r)
	if err == nil {
		_, err = f.Write(append(line, '\n'))
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("wikigit: record: %w", err)
	}
	return nil
}
