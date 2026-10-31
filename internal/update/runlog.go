package update

import (
	"bufio"
	"bytes"
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

	"github.com/heyNag/eva-md/internal/paths"
)

// Outcomes of a pass, as the run log records them.
const (
	OutcomeOK       = "ok"        // the page was saved
	OutcomeNoChange = "no_change" // the output validated and changed nothing
	OutcomeSkipped  = "skipped"   // the watermark moved past the batch without a save
	OutcomeFailed   = "failed"    // the watermark stayed
)

// Reasons the job adds to the runners'. A skip names why the page was not
// written: locked, verified, damaged, quarantined, or small for a group a
// branch switch closed before it reached one exchange or 600 characters, or
// the rest of a group that holds only events.
const (
	ReasonOKFRefused  = "okf_refused" // okf refused the page's text; counts toward the session
	ReasonWikiError   = "wiki_error"  // the wiki lock stayed busy, or a file system error; backs off
	ReasonConflict    = "conflict"    // the page changed under three saves in a row; counts toward nothing
	ReasonWatermark   = "watermark"   // the watermark could not be saved after the call, or can't be used at all; counts toward nothing
	ReasonLeft        = "left"        // a chat from before updates that couldn't be set aside yet, never drained; counts toward nothing
	ReasonLocked      = "locked"
	ReasonVerified    = "verified"
	ReasonDamaged     = "damaged"
	ReasonQuarantined = "quarantined"
	ReasonSmall       = "small"
)

// The failure classes of docs/design.md "Failures, budget and quarantine". A
// session failure counts toward the session's quarantine. A hold stops every
// update of the host until the next day or a successful manual run. A
// backoff holds the host's updates for 5 minutes, doubling with each one in
// a row up to 60, and counts toward no session. A missing CLI backs off
// rather than holds: a job started with a PATH that lacks the CLI, as from
// an app, says nothing about the jobs a terminal starts.
var (
	sessionReasons = []string{ReasonInvalidOutput, ReasonToolUsed, ReasonOKFRefused, ReasonTimeout, ReasonModelRejected}
	holdReasons    = []string{ReasonAuth, ReasonMCPFailed}
	backoffReasons = []string{ReasonRateLimit, ReasonHostError, ReasonWikiError, ReasonCLIMissing}
)

const (
	quarantineAfter = 3 // session failures in a row
	backoffMin      = 5 * time.Minute
	backoffMax      = 60 * time.Minute
	logKeepDays     = 14
	maxRunLog       = 32 << 20 // bytes of one day's run log read
)

// Record is one line of the run log: a pass over one chunk, or a skip. It
// holds counts, names and reasons only, never entry or output text.
type Record struct {
	At                string  `json:"at"` // local time, RFC 3339
	Host              string  `json:"host"`
	Session           string  `json:"session"`
	Project           string  `json:"project"` // the project key
	Thread            string  `json:"thread"`  // the branch
	Outcome           string  `json:"outcome"`
	Reason            string  `json:"reason,omitempty"`
	ChunkBytes        int     `json:"chunk_bytes"` // the input built for the model
	Pages             int     `json:"pages"`       // thread pages saved
	DurationMS        int64   `json:"duration_ms"` // the model call, retries included
	TokensIn          int     `json:"tokens_in"`
	TokensOut         int     `json:"tokens_out"`
	Model             string  `json:"model"`       // the model the run used; empty for the host default
	Effort            string  `json:"effort"`      // the effort the run asked for
	EffortUsed        string  `json:"effort_used"` // the effort the successful attempt used; empty without the flag
	RulesVersion      int     `json:"rules_version"`
	BoundaryToSavedMS int64   `json:"boundary_to_saved_ms"` // from the chunk's newest entry to the saved page
	Calls             int     `json:"calls"`                // CLI runs of the pass, retries included; 0 for a skip
	Manual            bool    `json:"manual,omitempty"`     // a person started the run
	ModelNote         string  `json:"model_note,omitempty"`
	Degraded          string  `json:"degraded,omitempty"`
	Validation        *Report `json:"validation,omitempty"`
}

// success reports a pass whose output validated.
func success(r Record) bool { return r.Outcome == OutcomeOK || r.Outcome == OutcomeNoChange }

// day is the local date a record was made, from its RFC 3339 time.
func day(at string) string {
	if len(at) < len(time.DateOnly) {
		return ""
	}
	return at[:len(time.DateOnly)]
}

// runLog is the run log's records of today and yesterday, oldest first,
// then the ones this job adds. Every state the job derives is a scan of it:
// the daily budget, a hold, a backoff, a quarantine and the effort a model
// starts at.
type runLog []Record

// readRunLog reads the run log of now's day and of the day before.
func readRunLog(state string, now time.Time) (runLog, error) {
	var l runLog
	for _, t := range []time.Time{now.AddDate(0, 0, -1), now} {
		records, err := readDay(state, t.Format(time.DateOnly))
		if err != nil {
			return nil, err
		}
		l = append(l, records...)
	}
	return l, nil
}

// readDay reads one day's run log. A line that doesn't decode is skipped.
func readDay(state, date string) ([]Record, error) {
	f, err := openDay(state, date)
	if f == nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRunLog))
	if err != nil {
		return nil, fmt.Errorf("update: run log: %w", err)
	}
	return parseRecords(data), nil
}

// readTail reads the newest records of one day's run log: at most limit
// bytes from the end of the file, without the line the cut falls in.
func readTail(state, date string, limit int64) ([]Record, error) {
	data, err := ReadTail(filepath.Join(state, "updates"), date+".jsonl", limit)
	if err != nil {
		return nil, fmt.Errorf("update: run log: %w", err)
	}
	return parseRecords(data), nil
}

// ReadTail reads at most limit bytes from the end of dir/name, a regular
// file, without the line the cut falls in; a missing file reads as empty.
// The run log's newest records are read with it, and the daily commit's.
func ReadTail(dir, name string, limit int64) ([]byte, error) {
	f, err := openRecords(dir, name)
	if f == nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := info.Size() - min(info.Size(), limit)
	data, err := io.ReadAll(io.NewSectionReader(f, off, info.Size()-off))
	if err != nil {
		return nil, err
	}
	if off > 0 {
		_, data, _ = bytes.Cut(data, []byte{'\n'})
	}
	return data, nil
}

// openDay opens one day's run log for reading, or returns nil and no error
// when there is none.
func openDay(state, date string) (*os.File, error) {
	f, err := openRecords(filepath.Join(state, "updates"), date+".jsonl")
	if err != nil {
		return nil, fmt.Errorf("update: run log: %w", err)
	}
	return f, nil
}

// openRecords opens dir/name, a regular file, for reading, never through a
// symlink, or returns nil and no error when there is none.
func openRecords(dir, name string) (*os.File, error) {
	root, err := paths.OpenRoot(dir, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New(name + " is not a regular file")
	}
	return f, nil
}

// parseRecords decodes run-log lines. A line that doesn't decode is skipped.
func parseRecords(data []byte) []Record {
	var out []Record
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Host != "" {
			out = append(out, r)
		}
	}
	return out
}

// Recent reads the run log's newest records, of now's day and the day
// before, at most limit bytes from the end of each file, oldest first: what
// the doctor, eva-md status and the start-of-chat notice read, so a long day
// costs them no more than limit bytes a file.
func Recent(state string, now time.Time, limit int64) ([]Record, error) {
	var out []Record
	for _, t := range []time.Time{now.AddDate(0, 0, -1), now} {
		records, err := readTail(state, t.Format(time.DateOnly), limit)
		if err != nil {
			return out, err
		}
		out = append(out, records...)
	}
	return out, nil
}

// HostState is what the run log says about one host's updates.
type HostState struct {
	Last        Record   // the newest record; its At is empty when there is none
	Failures    []string // the reasons of the failures in a row, newest first
	Held        string   // the failure that holds every update of the host today, or ""
	Quarantined []string // the sessions quarantined today
	Watermarks  []string // the sessions whose newest record is failed (watermark), newest first
	Left        []string // the sessions whose newest record is failed (left), newest first
	Calls       int      // the model calls made today
}

// Host works out a host's state from records, oldest first, by the rules
// the job stops by. A skip neither counts as a failure nor ends a row of
// them.
func Host(records []Record, host string, now time.Time) HostState {
	l := runLog(records)
	today := now.Format(time.DateOnly)
	s := HostState{Held: l.held(host, today), Calls: l.calls(host, today)}
	row := true
	for i := len(l) - 1; i >= 0; i-- {
		r := l[i]
		if r.Host != host {
			continue
		}
		if s.Last.At == "" {
			s.Last = r
		}
		switch {
		case success(r):
			row = false
		case r.Reason == ReasonLeft:
			// A chat from before updates not set aside yet is no failure
			// of an update: it neither counts nor ends a row.
		case row && r.Outcome == OutcomeFailed:
			s.Failures = append(s.Failures, r.Reason)
		}
	}
	seen := map[string]bool{}
	for _, r := range l {
		if r.Host == host && !seen[r.Session] {
			seen[r.Session] = true
			if l.quarantined(host, r.Session) == today {
				s.Quarantined = append(s.Quarantined, r.Session)
			}
		}
	}
	clear(seen)
	for i := len(l) - 1; i >= 0; i-- {
		if r := l[i]; r.Host == host && !seen[r.Session] {
			seen[r.Session] = true
			switch {
			case r.Outcome == OutcomeFailed && r.Reason == ReasonWatermark:
				s.Watermarks = append(s.Watermarks, r.Session)
			case r.Outcome == OutcomeFailed && r.Reason == ReasonLeft:
				s.Left = append(s.Left, r.Session)
			}
		}
	}
	return s
}

// appendRecord appends r to the run log of its day,
// <state>/updates/<YYYY-MM-DD>.jsonl, creating the folder with mode 0700 and
// the file with mode 0600, and syncs it: the budget, holds and quarantines
// are read back from it.
func appendRecord(state string, r Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("update: run log: %w", err)
	}
	root, err := paths.OpenRoot(filepath.Join(state, "updates"), true)
	if err != nil {
		return fmt.Errorf("update: run log: %w", err)
	}
	defer root.Close()
	f, err := root.OpenFile(day(r.At)+".jsonl", os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return fmt.Errorf("update: run log: %w", err)
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New("update: the run log is not a regular file")
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("update: run log: %w", err)
	}
	return f.Sync()
}

// calls counts the host's model calls on date.
func (l runLog) calls(host, date string) int {
	n := 0
	for _, r := range l {
		if r.Host == host && r.Calls > 0 && day(r.At) == date {
			n++
		}
	}
	return n
}

// held names an auth or mcp_failed failure of the host on date that no
// successful manual run has followed since, or returns "".
func (l runLog) held(host, date string) string {
	reason := ""
	for _, r := range l {
		switch {
		case r.Host != host || day(r.At) != date:
		case r.Outcome == OutcomeFailed && slices.Contains(holdReasons, r.Reason):
			reason = r.Reason
		case r.Manual && success(r):
			reason = ""
		}
	}
	return reason
}

// backoffUntil is when the host's backoff ends: 5 minutes after its newest
// rate_limit, host_error, wiki_error or cli_missing, doubling for each one
// before it in a row, up to 60 minutes. A call that ended in any other way
// ends the row; a skip made no call and is passed over, while a missing CLI,
// which made none either, counts.
func (l runLog) backoffUntil(host string) time.Time {
	n, last := 0, time.Time{}
	for i := len(l) - 1; i >= 0; i-- {
		r := l[i]
		if r.Host != host || r.Calls == 0 && r.Reason != ReasonCLIMissing {
			continue
		}
		if r.Outcome != OutcomeFailed || !slices.Contains(backoffReasons, r.Reason) {
			break
		}
		if n == 0 {
			last, _ = time.Parse(time.RFC3339Nano, r.At)
		}
		n++
	}
	if n == 0 || last.IsZero() {
		return time.Time{}
	}
	wait := backoffMin
	for i := 1; i < n && wait < backoffMax; i++ {
		wait *= 2
	}
	return last.Add(min(wait, backoffMax))
}

// quarantined returns the day the session's failures in a row reached 3,
// or "" while they haven't. Only invalid_output, tool_used, okf_refused,
// timeout and model_rejected count. A pass that validated or a skip ends a
// row; another failure, such as a rate limit, neither counts nor ends one.
func (l runLog) quarantined(host, session string) string {
	n, reached := 0, ""
	for _, r := range l {
		switch {
		case r.Host != host || r.Session != session:
		case r.Outcome == OutcomeFailed && slices.Contains(sessionReasons, r.Reason):
			if n++; n == quarantineAfter {
				reached = day(r.At)
			}
		case r.Outcome != OutcomeFailed:
			n, reached = 0, ""
		}
	}
	return reached
}

// startEffort is the effort a run for host and model starts at on date:
// want, unless that day's successful runs showed it refused. A run that
// asked for want or more sets the start to the effort it was accepted at,
// so a model whose top level is below the table's top doesn't pay the
// refused steps on every run, and a later run at a higher one replaces it;
// a run that asked for that start and was refused again lowers it further.
// A run accepted only without the effort flag starts the next at the
// table's lowest entry, the closest a request can come to no flag.
func (l runLog) startEffort(levels []string, want, host, model, date string) string {
	w := slices.Index(levels, want)
	if w < 0 {
		return want
	}
	s := w
	for _, r := range l {
		if r.Host != host || r.Model != model || !success(r) || day(r.At) != date {
			continue
		}
		asked, used := slices.Index(levels, r.Effort), slices.Index(levels, r.EffortUsed)
		switch {
		case asked < 0:
		case asked >= w:
			s = min(w, used)
		case asked >= s:
			s = min(s, used)
		}
	}
	return levels[max(s, 0)]
}

// OpenLog opens today's log of the detached job,
// <state>/logs/update-<YYYY-MM-DD>.log, for appending, creating the folder
// with mode 0700 and the file with mode 0600. The first open of a day
// deletes the logs dated more than 14 days before it. The log holds counts
// and reasons only.
func OpenLog(dirs paths.Dirs, now time.Time) (*os.File, error) {
	root, err := paths.OpenRoot(dirs.Logs(), true)
	if err != nil {
		return nil, fmt.Errorf("update: log folder: %w", err)
	}
	defer root.Close()
	name := "update-" + now.Format(time.DateOnly) + ".log"
	if _, err := root.Lstat(name); errors.Is(err, fs.ErrNotExist) {
		pruneLogs(root, now)
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("update: log: %w", err)
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("update: the log is not a regular file")
	}
	return f, nil
}

// pruneLogs deletes the job's logs dated more than logKeepDays before now.
func pruneLogs(root *os.Root, now time.Time) {
	d, err := root.Open(".")
	if err != nil {
		return
	}
	names, _ := d.Readdirnames(-1)
	d.Close()
	cutoff := now.AddDate(0, 0, -logKeepDays).Format(time.DateOnly)
	for _, name := range names {
		date, ok := strings.CutPrefix(strings.TrimSuffix(name, ".log"), "update-")
		if !ok || !strings.HasSuffix(name, ".log") || len(date) != len(time.DateOnly) {
			continue
		}
		if _, err := time.Parse(time.DateOnly, date); err == nil && date < cutoff {
			_ = root.Remove(name)
		}
	}
}
