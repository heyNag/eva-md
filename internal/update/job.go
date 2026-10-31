package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// Why a job, or one host's or one session's part of it, stopped before its
// backlog was drained. None of them is a failure: the next trigger tries
// again.
const (
	StopBusy        = "busy"        // another job holds the update lock
	StopPaused      = "paused"      // updates = false in the config
	StopBudget      = "budget"      // the host's daily cap of model calls is reached
	StopHeld        = "held"        // auth or mcp_failed today, until tomorrow or a successful manual run
	StopBackoff     = "backoff"     // rate_limit, host_error, wiki_error or cli_missing, for 5 to 60 minutes
	StopQuarantined = "quarantined" // 3 session failures in a row today
)

// ErrNoSession reports a session with no file in the inbox.
var ErrNoSession = errors.New("update: no inbox file for that session")

// Job is one run of the background update. One job runs at a time, under
// <locks>/update.lock. It drains each session's backlog oldest first, one
// chunk Build cuts per pass and model call, within the host's daily cap,
// and records every pass in the run log. Pending work is worked out from
// the inbox watermarks, never queued.
type Job struct {
	Dirs    paths.Dirs
	Config  config.Config
	Version string                   // eva-md's version, for the pages' writer
	Manual  bool                     // a person started it: holds, backoff and quarantine don't stop it, and a success clears them
	Runner  func(host string) Runner // nil: NewRunner
	Now     func() time.Time         // nil: time.Now
	Log     io.Writer                // a line per pass and per stop, counts and reasons only; nil: none
}

// Summary is what one job did.
type Summary struct {
	Records []Record // the passes and skips it recorded, oldest first
	Stops   []string // why parts of it stopped early, each reason once
	Waiting int      // backlogs left for more entries: under one exchange or 600 characters
	Errors  int      // session files it could not read, build or record
}

// Stopped reports whether part of the job stopped for reason.
func (s Summary) Stopped(reason string) bool { return slices.Contains(s.Stops, reason) }

// Failed reports whether a pass failed.
func (s Summary) Failed() bool {
	return slices.ContainsFunc(s.Records, func(r Record) bool { return r.Outcome == OutcomeFailed })
}

// Line describes the job in one line, such as "2 passes: 1 ok, 1 skipped;
// stopped: budget".
func (s Summary) Line() string {
	var parts []string
	if n := len(s.Records); n > 0 {
		count := map[string]int{}
		for _, r := range s.Records {
			count[r.Outcome]++
		}
		var each []string
		for _, o := range []string{OutcomeOK, OutcomeNoChange, OutcomeSkipped, OutcomeFailed} {
			if count[o] > 0 {
				each = append(each, fmt.Sprintf("%d %s", count[o], o))
			}
		}
		parts = append(parts, fmt.Sprintf("%d %s: %s", n, plural(n, "pass", "passes"), strings.Join(each, ", ")))
	}
	if len(s.Stops) > 0 {
		parts = append(parts, "stopped: "+strings.Join(s.Stops, ", "))
	}
	if s.Waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting for more entries", s.Waiting))
	}
	if s.Errors > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", s.Errors, plural(s.Errors, "error", "errors")))
	}
	if len(parts) == 0 {
		return "nothing pending"
	}
	return strings.Join(parts, "; ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Ref names a session: its host and its id, as the hooks were given it or
// as its inbox file names it.
type Ref struct{ Host, ID string }

// Session runs the update for one session: every inbox file it has, oldest
// first.
func (j Job) Session(ctx context.Context, host, id string) (Summary, error) {
	return j.Sessions(ctx, []Ref{{Host: host, ID: id}})
}

// Sessions runs the update for several sessions under one lock and budget,
// as the start-of-chat catch-up asks: every inbox file they have, oldest
// change first. ErrNoSession reports that none of them has one.
func (j Job) Sessions(ctx context.Context, refs []Ref) (Summary, error) {
	names := map[string]bool{}
	for _, ref := range refs {
		name, err := fileName(ref.Host, ref.ID)
		if err != nil {
			return Summary{}, err
		}
		names[name] = true
	}
	return j.run(ctx, func(files []sessionFile) ([]sessionFile, error) {
		var out []sessionFile
		for _, f := range files {
			if names[f.name] {
				out = append(out, f)
			}
		}
		if len(out) == 0 {
			return nil, ErrNoSession
		}
		return out, nil
	})
}

// Pending runs the update for every session with a backlog, oldest first,
// under the same lock and budget.
func (j Job) Pending(ctx context.Context) (Summary, error) {
	return j.run(ctx, func(files []sessionFile) ([]sessionFile, error) { return files, nil })
}

// fileName is a session's file name in the inbox.
func fileName(host, id string) (string, error) {
	if host != HostClaude && host != HostCodex {
		return "", fmt.Errorf("update: unknown host %q", host)
	}
	safe, err := paths.SafeName(id)
	if err != nil {
		return "", errors.New("update: the session id is empty")
	}
	return host + "-" + safe + ".jsonl", nil
}

// run takes the lock, opens the wiki and drains the files pick chooses.
func (j Job) run(ctx context.Context, pick func([]sessionFile) ([]sessionFile, error)) (Summary, error) {
	if !j.Config.Updates {
		j.logf("stopped: paused (updates = false in the config)")
		return Summary{Stops: []string{StopPaused}}, nil
	}
	unlock, busy, err := lockUpdates(j.Dirs.Locks())
	if busy {
		j.logf("stopped: another update is running")
		return Summary{Stops: []string{StopBusy}}, nil
	}
	if err != nil {
		return Summary{}, err
	}
	defer unlock()
	left, unread, cleared, err := baseline(j.Dirs, j.now())
	if err != nil {
		return Summary{}, err
	}
	if len(unread) > 0 {
		j.logf("%d project folders couldn't be read to set aside their chats from before updates; the next run tries again", len(unread))
	}
	r, files, err := j.start(pick)
	if err != nil {
		return Summary{}, err
	}
	r.left = left
	// A chat the record listed as left, set aside now: its newest record
	// says so, and the doctor stops naming it.
	for _, f := range cleared {
		_, sid, _ := sessionID(f.name)
		r.record(Record{Host: f.host, Session: sid, Project: f.dir, Outcome: OutcomeSkipped, Reason: ReasonLeft, RulesVersion: RulesVersion, Manual: j.Manual})
	}
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		r.drain(ctx, f)
	}
	return r.sum, ctx.Err()
}

// start opens the wiki, reads the run log and lists the session files with
// a backlog, for a job or a dry run.
func (j Job) start(pick func([]sessionFile) ([]sessionFile, error)) (*run, []sessionFile, error) {
	w, err := openWiki(j.Config, j.Dirs)
	if err != nil {
		return nil, nil, err
	}
	l, err := readRunLog(j.Dirs.State, j.now())
	if err != nil {
		return nil, nil, err
	}
	store := inbox.Store{Root: j.Dirs.Inbox()}
	files, prefixes, err := listSessions(store)
	if err != nil {
		return nil, nil, err
	}
	if files, err = pick(files); err != nil {
		return nil, nil, err
	}
	// A rest the job found too small waits until something is appended, as
	// the triggers and the brief leave it. A watermark that can't be read or
	// is past the end of its file is listed, so its drain records it.
	var pending []sessionFile
	for _, f := range files {
		if m, err := store.Mark(f.path); err != nil || m.Offset > f.size || m.Pending(f.size) > 0 {
			pending = append(pending, f)
		}
	}
	return &run{j: j, w: w, store: store, log: l, prefixes: prefixes, areas: map[string]area{}, stopped: map[string]bool{}, clis: cliPaths(j.Dirs.State)}, pending, nil
}

// cliPaths reads the absolute path of each host CLI that install recorded,
// <state>/cli-paths.json, by command name. A file that is missing or can't be
// read gives none, and the runs look the CLIs up on PATH.
func cliPaths(state string) map[string]string {
	f, _ := openRecords(state, "cli-paths.json")
	if f == nil {
		return nil
	}
	defer f.Close()
	var clis map[string]string
	if data, err := io.ReadAll(io.LimitReader(f, 64<<10)); err != nil || json.Unmarshal(data, &clis) != nil {
		return nil
	}
	return clis
}

// CLIRecorded reports whether install recorded a path to the host's CLI that
// a run would start, so the doctor can tell a job's cli_missing that a
// reinstall cures from one it doesn't.
func CLIRecorded(state, host string) bool {
	name := cliName(host)
	recorded := cliPaths(state)[name]
	return cliFor(name, recorded) == recorded
}

// cliName is a host's command.
func cliName(host string) string {
	if host == HostClaude {
		return "claude"
	}
	return host
}

func (j Job) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

func (j Job) logf(format string, args ...any) {
	if j.Log != nil {
		fmt.Fprintf(j.Log, "%s %s\n", j.now().Format("15:04:05"), fmt.Sprintf(format, args...))
	}
}

// lockUpdates takes <locks>/update.lock without waiting; busy reports that
// another job holds it. The job never takes the hooks' session lock, which
// a hook holds for its whole run, dropping the capture when it can't.
func lockUpdates(dir string) (unlock func(), busy bool, err error) {
	unlock, busy, err = TryLock(dir, "update.lock")
	if err != nil {
		err = fmt.Errorf("update: %w", err)
	}
	return unlock, busy, err
}

// TryLock takes the lock file dir/name, creating both, without waiting;
// busy reports that another process holds it. It refuses a symlink or
// anything else that is not a regular file. The update job and eva-md
// maintain take their locks with it.
func TryLock(dir, name string) (unlock func(), busy bool, err error) {
	root, err := paths.OpenRoot(dir, true)
	if err != nil {
		return nil, false, fmt.Errorf("lock folder: %w", err)
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("lock: %w", err)
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, false, errors.New("the lock is not a regular file")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("lock: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, false, nil
}

// sessionFile is one session file of the inbox.
type sessionFile struct {
	dir, name, path string // its project folder, its name and its path
	host            string // from its name
	mod             time.Time
	size            int64
}

// listSessions lists the session files of every project folder, oldest
// change first, and counts the session ids by the first 8 characters a short
// ref keeps, so a ref another session would share names the full id.
func listSessions(store inbox.Store) ([]sessionFile, map[string]int, error) {
	files, prefixes, _, err := listInbox(store)
	return files, prefixes, err
}

// listInbox is listSessions, and also names the project folders it couldn't
// read.
func listInbox(store inbox.Store) ([]sessionFile, map[string]int, []string, error) {
	prefixes := map[string]int{}
	root, err := paths.OpenRoot(store.Root, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, prefixes, nil, nil
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("update: inbox: %w", err)
	}
	defer root.Close()
	dirs, err := readDir(root, ".")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("update: inbox: %w", err)
	}
	var out []sessionFile
	var unread []string
	for _, d := range dirs {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		files, err := folderSessions(root, store, d.Name())
		if err != nil {
			unread = append(unread, d.Name())
		}
		for _, f := range files {
			_, sid, _ := sessionID(f.name)
			prefixes[prefix8(sid)]++
		}
		out = append(out, files...)
	}
	sort.Slice(out, func(i, k int) bool {
		if !out[i].mod.Equal(out[k].mod) {
			return out[i].mod.Before(out[k].mod)
		}
		return out[i].dir+"/"+out[i].name < out[k].dir+"/"+out[k].name
	})
	return out, prefixes, unread, nil
}

// folderSessions lists the session files of one project folder.
func folderSessions(root *os.Root, store inbox.Store, dir string) ([]sessionFile, error) {
	entries, err := readDir(root, dir)
	var out []sessionFile
	for _, f := range entries {
		host, _, ok := sessionID(f.Name())
		info, err := f.Info()
		if !ok || err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, sessionFile{dir: dir, name: f.Name(), host: host, path: filepath.Join(store.Root, dir, f.Name()), mod: info.ModTime(), size: info.Size()})
	}
	return out, err
}

func readDir(root *os.Root, name string) ([]fs.DirEntry, error) {
	d, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	return d.ReadDir(-1)
}

// sessionID returns the host and session parts of a session file's name.
func sessionID(file string) (host, sid string, ok bool) {
	name, ok := strings.CutSuffix(file, ".jsonl")
	if !ok {
		return "", "", false
	}
	for _, h := range []string{HostClaude, HostCodex} {
		if s, found := strings.CutPrefix(name, h+"-"); found && s != "" {
			return h, s, true
		}
	}
	return "", "", false
}

// run is one job's state.
type run struct {
	j        Job
	w        *okfadapter.Wiki
	store    inbox.Store
	log      runLog
	prefixes map[string]int
	areas    map[string]area   // by project key
	stopped  map[string]bool   // hosts a budget, hold or backoff stopped; their files wait whole
	clis     map[string]string // the CLIs' paths install recorded, by command name
	left     map[string]error  // session files from before updates not set aside yet, and why, by path
	sum      Summary
}

// chatFile is one session file being drained.
type chatFile struct {
	file    sessionFile
	b       *backlog
	key     string // the project key
	area    *area
	commits map[string][]string // by branch, with the branch's head
	heads   map[string]string
}
