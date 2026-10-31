package update

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/resume"
	"github.com/heyNag/eva-md/internal/session"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// What the next pass of a chat does.
const (
	planNone     = ""         // nothing is pending
	planWait     = "wait"     // the group waits for more entries
	planExcluded = "excluded" // the config excludes the project
	planSkip     = "skip"     // the watermark moves past the batch without a call
	planCall     = "call"     // the model runs on the batch
)

// plan is the next pass of a chat, worked out before any model call.
type plan struct {
	kind   string
	reason string // a skip's
	end    int64  // where a skip moves the watermark
	more   bool   // the next pass continues this group
	off    bool   // the chat is off the record at end
	area   area
	thread thread
	rules  rulesPages
	batch  Batch
	model  string // the chat's model; empty for the host default
	parent string // a new thread's parent branch
	label  string // host:id8, for the log
	rec    Record
}

// drain runs the passes of one session file until its backlog is empty or
// waits for more entries, a pass fails, or a stop applies. The stops are
// checked before the file is opened too: the file of a host the budget, a
// hold or a backoff stops, or of a session quarantined today by the id its
// name holds, is not even opened, and its passes, skips included, wait for
// the next run.
func (r *run) drain(ctx context.Context, f sessionFile) {
	// Never worked through: it couldn't be set aside at the end of its
	// history, and the doctor names it.
	if why, ok := r.left[f.path]; ok {
		_, sid, _ := sessionID(f.name)
		r.record(Record{Host: f.host, Session: sid, Project: f.dir, Outcome: OutcomeFailed, Reason: ReasonLeft, RulesVersion: RulesVersion, Manual: r.j.Manual})
		r.fail(f.dir+"/"+f.name, why)
		return
	}
	if r.stopped[f.host] || r.hostStop(f.host) != "" {
		return
	}
	if _, sid, ok := sessionID(f.name); ok {
		if stop, _ := r.quarantine(f.host, sid, f.host+":"+prefix8(sid)); stop != "" {
			return
		}
	}
	b, err := openBacklog(r.store, f.path)
	if err != nil {
		if errors.Is(err, errUnusableMark) {
			r.badMark(f)
		}
		r.fail(f.dir+"/"+f.name, err)
		return
	}
	defer b.close()
	c := &chatFile{file: f, b: b, commits: map[string][]string{}, heads: map[string]string{}}
	for ctx.Err() == nil {
		pending, err := b.refill()
		if err != nil {
			r.fail(f.dir+"/"+f.name, err)
			return
		}
		if !pending || !r.pass(ctx, c) {
			return
		}
	}
}

// pass runs one pass and reports whether the drain goes on.
func (r *run) pass(ctx context.Context, c *chatFile) bool {
	p, err := r.plan(ctx, c)
	if err != nil {
		r.fail(p.label, err)
		return false
	}
	switch p.kind {
	case planNone:
		return false
	case planExcluded:
		r.j.logf("%s: left as it is: the config excludes the project", p.label)
		return false
	case planWait:
		r.sum.Waiting++
		if p.rec.Thread == "" {
			r.j.logf("%s: waiting for the reply to the last prompt", p.label)
		} else {
			r.j.logf("%s %s: waiting for more entries: under one exchange or 600 characters", p.label, p.rec.Thread)
		}
		// Best effort: without the record, the next trigger only finds the
		// rest waiting again.
		if err := c.b.wait(); err != nil {
			r.j.logf("%s: %v", p.label, err)
		}
		return false
	case planSkip:
		return r.skip(c, p.rec, p.reason, p.end, p.more, p.off)
	}
	stop, retry := r.gate(p.rec.Host, p.rec.Session, p.label)
	if stop != "" {
		return false
	}
	return r.call(ctx, c, p, retry)
}

// plan works out the next pass: the group of entries from the watermark,
// its thread page and the batch Build cuts from it. A group under one
// exchange or 600 characters waits for more entries, unless a prompt of
// another branch already ended it; then nothing can join it and it is
// skipped. So is the rest of a group the last pass stopped in that holds
// only events, which add nothing to a page. A batch for a locked, verified
// or damaged page is skipped too. A last prompt left on its own waits for
// its reply.
func (r *run) plan(ctx context.Context, c *chatFile) (plan, error) {
	b := c.b
	p := plan{label: c.file.dir + "/" + c.file.name}
	g, err := b.next(Options{OffRecord: b.off, Continued: b.more})
	if err != nil {
		return p, err
	}
	host, sid, err := owner(g.entries)
	if errors.Is(err, ErrTooSmall) && g.held {
		if host, sid, err := owner(b.entries); err == nil {
			p.label, p.rec.Host, p.rec.Session = host+":"+prefix8(sid), host, sid
		}
		p.kind = planWait
		return p, nil
	}
	if err != nil {
		if errors.Is(err, ErrTooSmall) {
			return p, nil
		}
		return p, err
	}
	p.label = host + ":" + prefix8(sid)
	a, err := r.areaOf(ctx, c, g.entries)
	if err != nil || a == nil {
		p.kind = planExcluded
		return p, err
	}
	p.area = *a
	p.rec = Record{Host: host, Session: sid, Project: c.key, Thread: g.branch, RulesVersion: RulesVersion, Manual: r.j.Manual}
	switch {
	case errors.Is(g.err, ErrTooSmall) && (g.closed || b.more):
		_, off := onRecord(g.entries, b.off)
		p.kind, p.reason, p.end, p.off = planSkip, ReasonSmall, g.entries[len(g.entries)-1].End, off
		return p, nil
	case errors.Is(g.err, ErrTooSmall):
		p.kind = planWait
		return p, nil
	case g.err != nil:
		return p, g.err
	}
	if p.thread, err = readThread(r.w, p.area, g.branch); err != nil {
		return p, err
	}
	if p.rules, err = readRulesPages(r.w, p.area); err != nil {
		return p, err
	}
	o := r.options(ctx, c, p.area, p.thread, g, host, sid)
	o.Rules = p.rules.set(p.area)
	if p.batch, err = Build(g.entries, p.thread.page, o); err != nil {
		return p, err
	}
	p.more, p.end, p.off, p.parent = p.batch.More || g.cut, p.batch.End, p.batch.Off, o.ParentBranch
	p.rec.ChunkBytes = len(p.batch.Input)
	if p.thread.skip != "" {
		p.kind, p.reason = planSkip, p.thread.skip
		return p, nil
	}
	p.kind = planCall
	p.model = chatModel(session.Markers{Root: r.j.Dirs.Sessions()}, host, sid, g.entries)
	return p, nil
}

// next is the next pass's group, built on an empty page to learn where its
// chunk ends. The window grows only while the group runs on past it and the
// chunk still has room. A group the file ends in leaves out a last prompt
// with no reply yet (see answered).
func (b *backlog) next(o Options) (group, error) {
	for {
		var g group
		g.entries, g.branch, g.closed = b.group()
		if g.cut = b.cut(g.closed); !g.closed && !g.cut {
			n := len(g.entries)
			g.entries = answered(g.entries)
			g.held = n > 0 && len(g.entries) == 0
		}
		var batch Batch
		batch, g.err = Build(g.entries, threadpage.Page{}, o)
		full := g.err == nil && batch.More
		if !g.cut || full || g.err != nil && !errors.Is(g.err, ErrTooSmall) {
			return g, nil
		}
		grown, err := b.grow()
		if err != nil || !grown {
			return g, err
		}
	}
}

// group is the next pass's group of entries.
type group struct {
	entries []Entry
	branch  string
	closed  bool  // a prompt of another branch ends the group in the window
	cut     bool  // the group may run on past the window
	held    bool  // answered left out all of it: a prompt waits for its reply
	err     error // Build's error on it, such as ErrTooSmall
}

// areaOf works out the session file's project and its wiki folder once, or
// returns nil for a project the config excludes.
func (r *run) areaOf(ctx context.Context, c *chatFile, entries []Entry) (*area, error) {
	if c.area != nil {
		return c.area, nil
	}
	c.key = c.file.dir
	for _, e := range entries {
		if e.Project != "" {
			c.key = e.Project
			break
		}
	}
	p, dir, err := identify(ctx, c.key, entries)
	if err != nil {
		return nil, err
	}
	if r.j.Config.Excluded(dir, p.Remote) {
		return nil, nil
	}
	a, ok := r.areas[c.key]
	if !ok {
		if a, err = findArea(r.w, p); err != nil {
			return nil, err
		}
		r.areas[c.key] = a
	}
	c.area = &a
	return c.area, nil
}

// options is what Build needs for the pass: the branch and its commits, the
// page's as_of, a new thread's parent Now, whether the chat was off the
// record at the watermark, whether refs need the full session id, and
// whether the pass continues a group the last one stopped in.
func (r *run) options(ctx context.Context, c *chatFile, a area, t thread, g group, host, sid string) Options {
	o := Options{Branch: g.branch, AsOf: t.asOf, OffRecord: c.b.off, Continued: c.b.more, FullRef: r.prefixes[prefix8(sid)] > 1}
	if _, ok := c.commits[g.branch]; !ok {
		c.commits[g.branch], c.heads[g.branch] = commitsOf(ctx, a.project.Root, g.branch)
	}
	o.Commits = c.commits[g.branch]
	if !t.exists && !a.general() {
		o.Parent, o.ParentBranch = r.parent(ctx, a, host, sid, g.branch)
	}
	return o
}

// parent is a new thread's parent Now: the page of the branch the brief's
// lineage rule picks, when that branch has one.
func (r *run) parent(ctx context.Context, a area, host, sid, branch string) (*threadpage.Now, string) {
	p := a.project
	p.Branch = branch
	pb, err := resume.Lineage(ctx, resume.Options{Dirs: r.j.Dirs, Config: r.j.Config, Project: p, Host: host, Session: sid, Now: r.j.now()})
	if err != nil || pb == "" {
		return nil, ""
	}
	pt, err := readThread(r.w, a, pb)
	if err != nil || !pt.exists || pt.skip != "" {
		return nil, ""
	}
	return &pt.page.Now, pb
}

// gate checks what may stop a pass that needs a model call: the host's
// stops, then the session's quarantine. It returns the reason it stopped,
// and whether the pass is a quarantined session's one retry on a day after
// the quarantine began.
func (r *run) gate(host, sid, label string) (stop string, retry bool) {
	if stop := r.hostStop(host); stop != "" {
		return stop, false
	}
	return r.quarantine(host, sid, label)
}

// hostStop checks what stops every model call of a host: its daily cap,
// then, unless a person started the job, a hold or a backoff. A host it
// stops stays stopped for the rest of the job.
func (r *run) hostStop(host string) string {
	now := r.j.now()
	today := now.Format(time.DateOnly)
	if n := r.log.calls(host, today); n >= r.j.Config.DailyCap {
		return r.stopHost(host, StopBudget, fmt.Sprintf("%d of %d model calls today", n, r.j.Config.DailyCap))
	}
	if r.j.Manual {
		return ""
	}
	if reason := r.log.held(host, today); reason != "" {
		return r.stopHost(host, StopHeld, "after "+reason+", until tomorrow or a successful manual eva-md update")
	}
	if until := r.log.backoffUntil(host); now.Before(until) {
		return r.stopHost(host, StopBackoff, "until "+until.Format("15:04:05"))
	}
	return ""
}

// quarantine checks the session's quarantine, unless a person started the
// job: it stops a session quarantined today, and reports a pass of one
// quarantined on an earlier day as its one retry.
func (r *run) quarantine(host, sid, label string) (stop string, retry bool) {
	if r.j.Manual {
		return "", false
	}
	switch reached := r.log.quarantined(host, sid); reached {
	case "":
		return "", false
	case r.j.now().Format(time.DateOnly):
		r.stop(StopQuarantined)
		r.j.logf("%s: stopped: quarantined after %d failures in a row; it gets one retry tomorrow", label, quarantineAfter)
		return StopQuarantined, false
	}
	return "", true
}

func (r *run) stopHost(host, reason, why string) string {
	r.stopped[host] = true
	r.stop(reason)
	r.j.logf("%s: stopped: %s (%s)", host, reason, why)
	return reason
}

func (r *run) stop(reason string) {
	if !r.sum.Stopped(reason) {
		r.sum.Stops = append(r.sum.Stops, reason)
	}
}

// call runs the model on the batch, validates its answer and saves the
// pages, and reports whether the drain goes on. The rules pages are saved
// before the thread page, so a save that fails leaves the thread page for
// the pass that runs the batch again, whose rule changes then find
// themselves on file. The watermark moves to exactly the end of the batch
// once the output validates, whether or not a page changed, and only after
// the pages' folders are synced when they did. It is saved as it stands
// before the call, so a watermark that can't be saved costs no call.
func (r *run) call(ctx context.Context, c *chatFile, p plan, retry bool) bool {
	if err := c.b.touch(); err != nil {
		r.fail(p.label, err)
		return false
	}
	res, effort, took, err := r.runModel(ctx, p)
	if err != nil {
		if ctx.Err() == nil {
			r.fail(p.label, err)
		}
		return false
	}
	rec := p.rec
	rec.Model, rec.ModelNote, rec.Effort, rec.EffortUsed = res.Model, res.ModelNote, effort, res.Effort
	rec.TokensIn, rec.TokensOut, rec.Degraded, rec.DurationMS = res.TokensIn, res.TokensOut, res.Degraded, took.Milliseconds()
	if rec.Calls = res.Attempts; rec.Calls == 0 && res.Reason != ReasonCLIMissing {
		rec.Calls = 1
	}
	if res.Reason != "" {
		return r.failed(c, p, rec, res.Reason, retry)
	}
	u, report, err := Validate(res.Output, p.batch, p.thread.page)
	if err != nil {
		return r.failed(c, p, rec, ReasonInvalidOutput, retry)
	}
	rec.Validation = &report
	actor := okfadapter.Actor(p.batch.Host, res.Model)
	newest, worked := newestEntries(p.batch)
	u.LastChat = lastChat(u, actor, newest)
	f := facts{asOf: worked, head: c.heads[p.thread.branch], model: res.Model, modelNote: res.ModelNote, parent: p.parent,
		source: sourceRef(p.batch.Session, p.batch.FullRef), project: p.area.slug, writer: okfadapter.Actor("eva-md", r.j.Version), actor: actor}
	rules, kept, err := saveRules(r.w, p.area, p.rules, u.Rules, f)
	for _, page := range kept {
		r.j.logf("%s: rules page %s left as it is", p.label, page)
	}
	if err != nil {
		return r.saveFailed(c, p, rec, err, retry)
	}
	changed, wrote, skip, err := saveThread(r.w, p.area, p.thread, u, f)
	switch {
	case skip != "":
		return r.skip(c, rec, skip, p.end, p.more, p.off)
	case err != nil:
		return r.saveFailed(c, p, rec, err, retry)
	}
	rec.Outcome = OutcomeNoChange
	if changed || len(rules) > 0 {
		rec.Outcome = OutcomeOK
	}
	if rec.Pages = len(rules); wrote {
		rec.Pages++
	}
	if rec.Pages > 0 {
		r.refresh(p.area, rules, wrote, f.writer)
		if !newest.IsZero() {
			rec.BoundaryToSavedMS = max(r.j.now().Sub(newest).Milliseconds(), 0)
		}
	}
	if err := c.b.advance(p.end, p.more, p.off); err != nil {
		return r.unmarked(rec, err)
	}
	r.record(rec)
	return true
}

// saveFailed records a pass whose save failed, by the reason saveFailure
// names; a conflict, which counts toward nothing, is not logged.
func (r *run) saveFailed(c *chatFile, p plan, rec Record, err error, retry bool) bool {
	reason := saveFailure(err)
	if reason != ReasonConflict {
		r.j.logf("%s: the save failed: %v", p.label, err)
	}
	return r.failed(c, p, rec, reason, retry)
}

// refresh rebuilds the overview and index files of the areas a pass wrote
// to: its own, when it wrote the thread page or the area's rules page, and
// the general area, when it wrote the rules for all projects from a
// project.
func (r *run) refresh(a area, rules []rulesPage, thread bool, actor string) {
	areas := map[string]area{}
	if thread {
		areas[a.dir] = a
	}
	for _, rp := range rules {
		if rp.general && !a.general() {
			areas[identity.GeneralKey] = area{dir: identity.GeneralKey, slug: identity.GeneralKey}
		} else {
			areas[a.dir] = a
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(areas)) {
		if err := refreshArea(r.w, areas[dir], actor); err != nil {
			r.j.logf("%s: the overview or an index was not written: %v", dir, err)
		}
	}
}

// saveFailure names why a save failed: conflict when the page kept changing
// under it; wiki_error when the wiki, not the page's text, was at fault:
// its lock still busy after a minute, or a file system error such as a
// folder that can't be written, a full disk or a failed fsync; and
// otherwise okf_refused, okf's refusal of the page's text.
func saveFailure(err error) string {
	var errno syscall.Errno
	switch {
	case errors.Is(err, okfadapter.ErrConflict) || errors.Is(err, okfadapter.ErrExists):
		return ReasonConflict
	case errors.Is(err, okfadapter.ErrLockBusy) || errors.As(err, &errno):
		return ReasonWikiError
	}
	return ReasonOKFRefused
}

// effort is the effort a run for host and model starts at: the config's,
// resolved against the host's table, or lower where today's run log shows
// it refused (see startEffort).
func (r *run) effort(host, model string) (string, error) {
	h, ok := lookupHost(host)
	if !ok {
		return "", fmt.Errorf("update: unknown host %q", host)
	}
	want, err := resolveEffort(h.efforts, r.j.Config.EffortFor(host, model))
	if err != nil {
		return "", err
	}
	return r.log.startEffort(h.efforts, want, host, model, r.j.now().Format(time.DateOnly)), nil
}

// runModel runs the host CLI on the batch in an empty folder of its own, and
// returns the effort it asked for and the call's duration.
func (r *run) runModel(ctx context.Context, p plan) (Result, string, time.Duration, error) {
	effort, err := r.effort(p.batch.Host, p.model)
	if err != nil {
		return Result{}, "", 0, err
	}
	dir, err := os.MkdirTemp("", "eva-md-run-")
	if err != nil {
		return Result{}, "", 0, errors.New("update: could not create a run folder")
	}
	defer os.RemoveAll(dir)
	runner := NewRunner
	if r.j.Runner != nil {
		runner = r.j.Runner
	}
	who := p.model
	if who == "" {
		who = "the default model"
	}
	r.j.logf("%s %s: calling %s at %s on %d bytes", p.label, p.rec.Thread, who, effort, len(p.batch.Input))
	start := time.Now()
	res, err := runner(p.batch.Host).Run(ctx, Request{Host: p.batch.Host, Model: p.model, Effort: effort, Rules: Rules,
		Input: p.batch.Input, Schema: Schema(p.thread.page), RunDir: dir, IgnoreUserConfig: r.j.Config.CodexIgnoreUserConfig,
		CLI: r.clis[cliName(p.batch.Host)]})
	return res, effort, time.Since(start), err
}

// newestEntries returns the time of the batch's newest entry, events
// included, which is where the chat stood, and of its newest work, which a
// page's as_of says it covers.
func newestEntries(b Batch) (newest, worked time.Time) {
	for _, e := range b.Entries {
		newest = later(newest, e.At)
		if e.Role != "event" {
			worked = later(worked, e.At)
		}
	}
	return newest, worked
}

// lastChat puts the date and host/model in front of the model's line, as the
// page shows it: "2026-01-15 · claude-code/claude-opus-5-5 · Built it." An
// empty line stays empty, so the page keeps its last chat.
func lastChat(u threadpage.Update, actor string, at time.Time) string {
	if u.LastChat == "" {
		return ""
	}
	date := u.Date
	if date == "" && !at.IsZero() {
		date = at.Format(time.DateOnly)
	}
	if date == "" {
		return actor + " · " + u.LastChat
	}
	return date + " · " + actor + " · " + u.LastChat
}

// failed records a pass whose watermark stays, unless it was a quarantined
// session's retry that failed in a way that counts toward the session: then
// the watermark moves past the batch with skipped (quarantined), so the
// triggers and the brief stop reporting it.
func (r *run) failed(c *chatFile, p plan, rec Record, reason string, retry bool) bool {
	rec.Outcome, rec.Reason, rec.Validation = OutcomeFailed, reason, nil
	r.record(rec)
	if retry && slices.Contains(sessionReasons, reason) {
		r.skip(c, p.rec, ReasonQuarantined, p.end, p.more, p.off)
	}
	return false
}

// skip moves the watermark past a batch without saving the page and
// records why.
func (r *run) skip(c *chatFile, rec Record, reason string, end int64, more, off bool) bool {
	if err := c.b.advance(end, more, off); err != nil {
		if rec.Calls > 0 {
			return r.unmarked(rec, err)
		}
		r.fail(rec.Host+":"+prefix8(rec.Session), err)
		return false
	}
	rec.Outcome, rec.Reason = OutcomeSkipped, reason
	r.record(rec)
	return true
}

// unmarked records a pass whose watermark could not be saved after its
// model call, as failed (watermark), which counts toward nothing but keeps
// the call in the budget, and stops the drain. The batch runs again next
// time.
func (r *run) unmarked(rec Record, err error) bool {
	rec.Outcome, rec.Reason, rec.Validation = OutcomeFailed, ReasonWatermark, nil
	r.record(rec)
	r.fail(rec.Host+":"+prefix8(rec.Session), err)
	return false
}

// badMark records a session file whose watermark can't be used as failed
// (watermark), under the id its name holds, so the doctor names the session:
// the triggers never spawn for it, and nothing else would report it.
func (r *run) badMark(f sessionFile) {
	_, sid, _ := sessionID(f.name)
	r.record(Record{Host: f.host, Session: sid, Project: f.dir, Outcome: OutcomeFailed, Reason: ReasonWatermark, RulesVersion: RulesVersion, Manual: r.j.Manual})
}

// record appends rec to the run log and logs it.
func (r *run) record(rec Record) {
	rec.At = r.j.now().Format(time.RFC3339)
	if err := appendRecord(r.j.Dirs.State, rec); err != nil {
		r.fail(rec.Host+":"+prefix8(rec.Session), err)
	}
	r.log = append(r.log, rec)
	r.sum.Records = append(r.sum.Records, rec)
	label := rec.Host + ":" + prefix8(rec.Session) + " " + rec.Thread
	switch rec.Outcome {
	case OutcomeOK, OutcomeNoChange:
		r.j.logf("%s: %s, %d bytes in, %d %s, %.1f s, %d/%d tokens", label, rec.Outcome, rec.ChunkBytes, rec.Pages,
			plural(rec.Pages, "page", "pages"), float64(rec.DurationMS)/1000, rec.TokensIn, rec.TokensOut)
	default:
		r.j.logf("%s: %s (%s)", label, rec.Outcome, rec.Reason)
	}
}

// fail logs an error that stopped a session file's drain. Errors name files,
// pages and causes, never entry text.
func (r *run) fail(label string, err error) {
	r.sum.Errors++
	r.j.logf("%s: error: %v", label, err)
}
