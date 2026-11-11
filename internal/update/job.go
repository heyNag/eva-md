package update

import (
	"bufio"
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
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
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
	// SweepDay is the day, YYYY-MM-DD, a chat start found the day's sweep
	// due on, by the hook's clock, which the job then claims it on, so the
	// two judge the same day whatever time zone each runs in; "": the day of
	// the job's clock (see claimSweep).
	SweepDay string
	// Trigger names what started the job, as TriggerName gives it, for its
	// records: "" when nothing named it. The sessions the day's sweep picks
	// are recorded as TriggerSweep.
	Trigger string
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
	return j.CatchUp(ctx, refs, false)
}

// The day's sweep, which a person's first chat start of the day asks for
// with its catch-up: at most SweepSessions sessions of the whole inbox
// whose file has been unchanged for SweepIdle or more and holds a backlog,
// and apart from them, at most SweepLeft chats from before updates the
// baseline left that have been unchanged as long, for the drain to record
// with no model call, so the doctor names them.
const (
	SweepIdle     = 60 * time.Minute
	SweepSessions = 5
	SweepLeft     = 16
)

// CatchUp runs the update for the sessions refs names, as Sessions does,
// and with sweep also takes the day's sweep, unless a job took it today
// already (see claimSweep and sweep), in the same job, under its lock and
// budget: the sessions named first, then the sweep's chats from before
// updates the baseline left, which cost no call, then its sessions. With
// sweep, no session file for refs is no error.
func (j Job) CatchUp(ctx context.Context, refs []Ref, sweep bool) (Summary, error) {
	names := map[string]bool{}
	for _, ref := range refs {
		name, err := fileName(ref.Host, ref.ID)
		if err != nil {
			return Summary{}, err
		}
		names[name] = true
	}
	return j.run(ctx, func() { j.busy(refs, sweep) }, func(files []sessionFile) ([]sessionFile, error) {
		var out []sessionFile
		for _, f := range files {
			if names[f.name] {
				out = append(out, f)
			}
		}
		if sweep && j.claimSweep() {
			left, swept := j.sweep(ctx, files, names)
			j.logf("the day's sweep: %d %s idle an hour or more with a backlog", len(swept), plural(len(swept), "session", "sessions"))
			if len(left) > 0 {
				j.logf("the day's sweep: %d %s from before updates not set aside yet, idle an hour or more, recorded with no model call", len(left), plural(len(left), "chat", "chats"))
			}
			// The chats left go first: they cost no call, so a job cut
			// short still records them. Their records name the sweep.
			for _, f := range append(left, swept...) {
				f.swept = true
				out = append(out, f)
			}
			return out, nil
		}
		if len(out) == 0 && !sweep {
			return nil, ErrNoSession
		}
		return out, nil
	})
}

// sweep picks the day's sweep from files, the inbox's session files oldest
// change first, passing over those named holds, newest change first: at
// most SweepSessions whose file has been unchanged for SweepIdle or more
// and holds a backlog by its watermark, as the pending filter finds one
// (out), and apart from them, at most SweepLeft chats from before updates
// the baseline left whose file has been unchanged as long (left). The
// drain never works through a chat left, but records it as failed with no
// model call, so the doctor names it; it takes no place among the
// sessions, as it would every day while older backlogs wait, and is picked
// by the baseline's record alone, before any watermark, catch-up record or
// git is read, so one whose watermark can't be read is picked too. A chat
// its catch-up record marks as a program's needs no model call and is left
// to other jobs, and so is a watermark that can't be read or lies past the
// end of its file, which the doctor names. A session of a project the
// config excludes or that can't be told takes no place either (see
// advances).
func (j Job) sweep(ctx context.Context, files []sessionFile, named map[string]bool) (left, out []sessionFile) {
	store := inbox.Store{Root: j.Dirs.Inbox()}
	now := j.now()
	_, isLeft, _, _ := baselined(j.Dirs, now)
	projects := map[[2]string]bool{}
	// With no chat left, as is usual, the scan ends with the sessions.
	wantLeft := min(SweepLeft, len(isLeft))
	for i := len(files) - 1; i >= 0 && (len(out) < SweepSessions || len(left) < wantLeft) && ctx.Err() == nil; i-- {
		f := files[i]
		if named[f.name] || now.Sub(f.mod) < SweepIdle {
			continue
		}
		if isLeft[f.path] {
			if len(left) < SweepLeft {
				left = append(left, f)
			}
			continue
		}
		if len(out) == SweepSessions {
			continue
		}
		m, err := store.Mark(f.path)
		if err != nil || m.Offset > f.size || m.Pending(f.size) == 0 {
			continue
		}
		if _, sid, ok := sessionID(f.name); !ok || catchup.Headless(ctx, j.Dirs, f.host, sid) {
			continue
		}
		if !j.advances(ctx, store, f, m.Offset, projects) {
			continue
		}
		out = append(out, f)
	}
	return left, out
}

// advances reports whether the drain takes on the project of a session file
// the sweep would take, as areaOf finds it, here from the entry at the
// watermark, offset: not when the config excludes it, which the drain
// leaves as it is, nor when it can't be told, as when its chat folder is
// gone and its key holds no remote, which the drain fails on. A file whose
// entry can't be read is left to the drain to judge. Each project key and
// chat folder is resolved once a sweep, in projects; resolving runs git in
// the chat folder, which the background job can afford.
func (j Job) advances(ctx context.Context, store inbox.Store, f sessionFile, offset int64, projects map[[2]string]bool) bool {
	e, ok := entryAt(store, f.path, offset)
	if !ok {
		return true
	}
	key := f.dir
	if e.Project != "" {
		key = e.Project
	}
	entries := []Entry{{Entry: e}}
	at := [2]string{key, chatFolder(entries)}
	if v, ok := projects[at]; ok {
		return v
	}
	p, dir, err := identify(ctx, key, entries)
	v := err == nil && !j.Config.Excluded(dir, p.Remote)
	projects[at] = v
	return v
}

// entryAt decodes the entry on the line of a session file that starts at
// offset, a line of at most maxEntryLine bytes; ok is false when it can't
// be read or decoded.
func entryAt(store inbox.Store, path string, offset int64) (e inbox.Entry, ok bool) {
	f, err := store.Open(path)
	if err != nil {
		return e, false
	}
	defer f.Close()
	line, err := bufio.NewReader(io.NewSectionReader(f, offset, maxEntryLine)).ReadBytes('\n')
	if err != nil || json.Unmarshal(line, &e) != nil {
		return inbox.Entry{}, false
	}
	return e, true
}

// sweepFile, in <state>/updates, holds the day the newest sweep was taken,
// YYYY-MM-DD.
const sweepFile = "sweep"

// SweepDue reports whether no job has taken the day's sweep yet on day, a
// YYYY-MM-DD: the hooks ask for it with a chat start's catch-up only then,
// passing day for the job to claim it on (see Job.SweepDay). It reads one
// small file.
func SweepDue(state, day string) bool {
	return !covers(sweptOn(state), day)
}

// covers reports whether a sweep taken on recorded, a YYYY-MM-DD, counts as
// the sweep of day: it was taken on day, or on the day after, as by a chat
// start whose time zone is a day ahead of day's, so chat starts on two days
// at once never take turns at it. One taken earlier doesn't count, and nor
// does one taken further ahead, as one a clock set wrong recorded, so such a
// record can't keep the sweep from running.
func covers(recorded, day string) bool {
	if recorded == day {
		return true
	}
	d, err := time.Parse(time.DateOnly, day)
	return err == nil && recorded == d.AddDate(0, 0, 1).Format(time.DateOnly)
}

// sweptOn reads the day the newest sweep was taken, or "" when none was or
// the record can't be read.
func sweptOn(state string) string {
	f, _ := openRecords(filepath.Join(state, "updates"), sweepFile)
	if f == nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// claimSweep takes the day's sweep for this job, which holds the update
// lock, by recording today as the day it was taken, unless a job took it
// today already, or on the day after (see covers). Today is the day the
// chat start that asked for the sweep found it due on, when it says, so the
// job never finds taken a sweep the hook found due, as when the hook's time
// zone, from the host's TZ, is a day ahead of the job's, whose environment
// drops TZ. A sweep that can't be recorded isn't taken, so none runs twice
// a day.
func (j Job) claimSweep() bool {
	today := j.SweepDay
	if today == "" {
		today = j.now().Format(time.DateOnly)
	}
	if covers(sweptOn(j.Dirs.State), today) {
		j.logf("the day's sweep: an earlier job took it today")
		return false
	}
	root, err := paths.OpenRoot(filepath.Join(j.Dirs.State, "updates"), true)
	if err == nil {
		defer root.Close()
		err = paths.WriteFileAtomicAt(context.Background(), root, sweepFile, []byte(today+"\n"), 0o600)
	}
	if err != nil {
		j.logf("the day's sweep: not taken, as the day it was taken can't be recorded")
		return false
	}
	return true
}

// Pending runs the update for every session with a backlog, oldest first,
// under the same lock and budget.
func (j Job) Pending(ctx context.Context) (Summary, error) {
	return j.run(ctx, func() { j.busy(nil, false) }, func(files []sessionFile) ([]sessionFile, error) { return files, nil })
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

// run takes the lock, opens the wiki and drains the files pick chooses. A
// job that finds the lock held records its exit with busy and leaves.
func (j Job) run(ctx context.Context, busy func(), pick func([]sessionFile) ([]sessionFile, error)) (Summary, error) {
	if !j.Config.Updates {
		j.logf("stopped: paused (updates = false in the config)")
		return Summary{Stops: []string{StopPaused}}, nil
	}
	unlock, held, err := lockUpdates(j.Dirs.Locks())
	if held {
		j.logf("stopped: another update is running")
		busy()
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
	r.trigger = j.Trigger
	for _, f := range cleared {
		_, sid, _ := sessionID(f.name)
		r.record(Record{Host: f.host, Session: sid, Project: f.dir, Outcome: OutcomeSkipped, Reason: ReasonLeft, RulesVersion: RulesVersion, Manual: j.Manual})
	}
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		if r.trigger = j.Trigger; f.swept {
			r.trigger = TriggerSweep
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

// TryLock takes the lock file dir/name without waiting, as paths.TryLock
// does. The update job and eva-md maintain take their locks with it.
func TryLock(dir, name string) (unlock func(), busy bool, err error) {
	return paths.TryLock(dir, name)
}

// sessionFile is one session file of the inbox.
type sessionFile struct {
	dir, name, path string // its project folder, its name and its path
	host            string // from its name
	mod             time.Time
	size            int64
	swept           bool // the day's sweep picked it
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
	trigger  string            // the trigger of the session file being drained, for its records
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
