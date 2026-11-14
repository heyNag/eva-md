package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/work"
)

// The worker's two jobs, v1's update and maintain, and the marks that ask
// for them. A session's mark has the key sessionKey gives, the day's sweep
// sweep-<YYYY-MM-DD>, the day's maintain daily and a push retry push.
const (
	updateKind   = "update"
	maintainKind = "maintain"
	sweepPrefix  = "sweep-"
	dailyKey     = "daily"
	pushKey      = "push"
)

// wakeReasons are the words a wake may give as its reason: the hooks'
// reasons for a job, and tick.
var wakeReasons = []string{"daily", "push_retry", update.TriggerGap, update.TriggerStop, update.TriggerEnd, update.TriggerCompact, update.TriggerStart, "tick"}

func init() {
	register(command{name: "work", file: "work.go", place: 145,
		synopsis: []string{
			"eva-md work --wake --reason WORD [--session host:id ...] [--sweep=YYYY-MM-DD] [--maintain [--push-only]]",
			"eva-md work --tick",
			"eva-md work --status",
		},
		run: func(ctx context.Context, args []string, _ io.Reader, out, errOut io.Writer) int {
			return runWork(ctx, args, out, errOut)
		}})
	work.Register(work.Job{Kind: updateKind, Priority: 3, Run: runUpdateJob, Waiting: updateWaiting})
	work.Register(work.Job{Kind: maintainKind, Priority: 4, Run: runMaintainJob})
}

// runWork runs eva-md work. With --wake, which the hooks start in place of
// update and maintain while the switch worker is on, it leaves a mark for
// each session, the day's sweep and maintain it names, with its reason,
// and wakes the worker, which runs every job with marks, one at a time, or
// leaves them to the worker already running; it exits 0 then too. With
// --tick it wakes the worker with no mark, so marks a busy lock left run.
// Both do nothing while the switch is off. With --status it prints what
// waits longer than the worker's bar, or is held, by reason: counts, hosts
// and times only.
func runWork(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("work", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	wake := f.Bool("wake", false, "")
	tick := f.Bool("tick", false, "")
	status := f.Bool("status", false, "")
	reason := f.String("reason", "", "")
	var sessionArgs []string
	f.Func("session", "", func(s string) error { sessionArgs = append(sessionArgs, s); return nil })
	sweep := f.String("sweep", "", "")
	maintainFlag := f.Bool("maintain", false, "")
	pushOnly := f.Bool("push-only", false, "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for work")
	}
	set := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	modes := 0
	for _, on := range []bool{*wake, *tick, *status} {
		if on {
			modes++
		}
	}
	var marks []work.Mark
	switch {
	case modes != 1:
		return usage(errOut, "work takes one of --wake, --tick or --status")
	case !*wake && len(set) > 1:
		return usage(errOut, "work --tick and work --status take no other flag")
	case *wake && !slices.Contains(wakeReasons, *reason):
		return usage(errOut, "work --wake needs --reason, one of "+strings.Join(wakeReasons, ", "))
	case *wake && len(sessionArgs) == 0 && !set["sweep"] && !*maintainFlag:
		return usage(errOut, "work --wake needs --session host:id, --sweep=YYYY-MM-DD or --maintain")
	case *pushOnly && !*maintainFlag:
		return usage(errOut, "work --push-only goes with --maintain")
	}
	if *wake {
		for _, s := range sessionArgs {
			host, id, _ := strings.Cut(s, ":")
			key := sessionKey(host, id)
			if key == "" {
				return usage(errOut, "work --session takes host:id, with host claude-code or codex")
			}
			marks = append(marks, work.Mark{Kind: updateKind, Key: key, Reason: *reason, Host: host})
		}
		if set["sweep"] {
			if !validDay(*sweep) {
				return usage(errOut, "work --sweep takes a day, YYYY-MM-DD")
			}
			marks = append(marks, work.Mark{Kind: updateKind, Key: sweepPrefix + *sweep, Reason: *reason})
		}
		if *maintainFlag {
			key := dailyKey
			if *pushOnly {
				key = pushKey
			}
			marks = append(marks, work.Mark{Kind: maintainKind, Key: key, Reason: *reason})
		}
	}
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	if *status {
		now := time.Now()
		for _, line := range health.WorkLines(work.Status(ctx, dirs, now), now) {
			fmt.Fprintln(out, "work: "+line)
		}
		return 0
	}
	c, err := config.Load(dirs.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(errOut, "eva-md: configuration is invalid")
		return 1
	}
	if !work.Switch.On(c) {
		fmt.Fprintln(out, "work: the worker is off; [switches] worker = true in the config turns it on")
		return 0
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	o := work.Options{}
	if log, err := update.OpenLog(dirs, time.Now()); err == nil {
		defer log.Close()
		o.Log = log
	}
	if err := work.Wake(ctx, dirs, marks, o); err != nil {
		fmt.Fprintln(errOut, "eva-md: work:", err)
		return 1
	}
	return 0
}

// sessionKey is the key of a session's mark: <host>-<id> when that is a key
// the worker takes, a file name paths.SafeName leaves as it is with no
// upper-case letter. Any other id, which the host chose, gives <host>-, the
// id as its inbox file names it, lower-cased and cut to 40 bytes, and 16
// hex digits of a hash of that name, so it is the same every time and two
// ids that differ only in letter case get two keys. The update job makes
// the key of each session file in the inbox the same way, from its name,
// to find a mark's session. A host other than claude-code or codex, or an
// empty id, gives "".
func sessionKey(host, id string) string {
	if host != update.HostClaude && host != update.HostCodex {
		return ""
	}
	sid, err := paths.SafeName(id)
	if err != nil {
		return ""
	}
	return fileKey(host, sid)
}

// fileKey is sessionKey of sid, the session part of an inbox file's name,
// which paths.SafeName leaves as it is.
func fileKey(host, sid string) string {
	key := host + "-" + sid
	if safe, err := paths.SafeName(key); err == nil && safe == key && strings.ToLower(key) == key {
		return key
	}
	sum := sha256.Sum256([]byte(sid))
	name := strings.ToLower(sid)
	return host + "-" + name[:min(len(name), 40)] + "-" + hex.EncodeToString(sum[:8])
}

// validDay reports a day written YYYY-MM-DD.
func validDay(day string) bool {
	at, err := time.Parse(time.DateOnly, day)
	return err == nil && at.Format(time.DateOnly) == day
}

// workSetup is what a job of the worker reads first: eva-md's folders and
// its config. It writes why it can't to log, with no path.
func workSetup(log io.Writer) (paths.Dirs, config.Config, bool) {
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(log, "eva-md: home folder is unavailable")
		return paths.Dirs{}, config.Config{}, false
	}
	c, err := config.Load(dirs.ConfigFile())
	if err != nil {
		fmt.Fprintln(log, "eva-md: configuration is invalid or missing")
		return paths.Dirs{}, config.Config{}, false
	}
	return dirs, c, true
}

// sessionsByKey lists the inbox's session files by the key of their marks
// (see fileKey), each as the Ref that names it to the update, its id as
// its file names it. It reads folder listings only.
func sessionsByKey(dirs paths.Dirs) map[string][]update.Ref {
	out := map[string][]update.Ref{}
	projects, _ := os.ReadDir(dirs.Inbox())
	for _, p := range projects {
		if !p.IsDir() || strings.HasPrefix(p.Name(), ".") {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(dirs.Inbox(), p.Name()))
		for _, f := range files {
			name, ok := strings.CutSuffix(f.Name(), ".jsonl")
			if !ok || !f.Type().IsRegular() {
				continue
			}
			for _, host := range []string{update.HostClaude, update.HostCodex} {
				if sid, found := strings.CutPrefix(name, host+"-"); found && sid != "" {
					ref := update.Ref{Host: host, ID: sid}
					if key := fileKey(host, sid); !slices.Contains(out[key], ref) {
						out[key] = append(out[key], ref)
					}
				}
			}
		}
	}
	return out
}

// runUpdateJob is the update job: v1's update, as runUpdate runs it with
// no --foreground, for the sessions of its marks and the day's sweep a
// mark names, one call for each reason, so the trigger each records is
// the one that asked. A session mark finds its session by its key among
// the inbox's session files; one that matches none is done. A call that
// stopped because another holds update.lock, as a person's --foreground
// run does, makes the job busy, and the worker keeps every mark for the
// next wake or tick.
func runUpdateJob(ctx context.Context, marks []work.Mark, log io.Writer) work.Outcome {
	dirs, c, ok := workSetup(log)
	if !ok {
		return work.Failed
	}
	keyed := sessionsByKey(dirs)
	type ask struct {
		refs  []update.Ref
		sweep string
	}
	asks := map[string]*ask{}
	var reasons []string
	for _, m := range marks {
		a := asks[m.Reason]
		if a == nil {
			a = &ask{}
			asks[m.Reason] = a
			reasons = append(reasons, m.Reason)
		}
		if day, ok := strings.CutPrefix(m.Key, sweepPrefix); ok && m.Host == "" && validDay(day) {
			a.sweep = max(a.sweep, day)
			continue
		}
		for _, ref := range keyed[m.Key] {
			if ref.Host == m.Host && !slices.Contains(a.refs, ref) {
				a.refs = append(a.refs, ref)
			}
		}
	}
	slices.Sort(reasons)
	outcome := work.Done
	for _, reason := range reasons {
		a := asks[reason]
		if len(a.refs) == 0 && a.sweep == "" {
			continue
		}
		job := update.Job{Dirs: dirs, Config: c, Version: version, Runner: newRunner, Now: updateClock, SweepDay: a.sweep, Log: log}
		// As the hooks pass it: a job for the sweep alone names no trigger.
		if len(a.refs) > 0 {
			job.Trigger = update.TriggerName(reason)
		}
		s, err := job.CatchUp(ctx, a.refs, a.sweep != "")
		if err == nil || errors.Is(err, context.Canceled) {
			fmt.Fprintln(log, "update: "+s.Line())
		}
		switch {
		case ctx.Err() != nil:
			return work.Cut
		case s.Stopped(update.StopBusy):
			return work.Busy
		case errors.Is(err, update.ErrNoSession):
			// The session's file is gone: there is nothing to update.
		case err != nil:
			fmt.Fprintln(log, "eva-md:", err)
			outcome = work.Failed
		case s.Failed() || s.Errors > 0:
			outcome = work.Failed
		}
	}
	return outcome
}

// updateWaiting is the update job's Waiting (see health.UpdateWaits).
func updateWaiting(ctx context.Context, now time.Time) []work.Waiting {
	dirs, c, ok := workSetup(io.Discard)
	if !ok || c.WikiPath == "" {
		return nil
	}
	return health.UpdateWaits(ctx, dirs, c, updateKind, now)
}

// runMaintainJob is the maintain job: v1's maintain, with --push-only when
// its marks ask for a push retry alone, its lines to log. A run that finds
// maintain.lock held is busy, and the worker keeps its marks.
func runMaintainJob(ctx context.Context, marks []work.Mark, log io.Writer) work.Outcome {
	keys := map[string]bool{}
	for _, m := range marks {
		keys[m.Key] = true
	}
	var args []string
	switch {
	case keys[dailyKey]:
		args = []string{"maintain"}
	case keys[pushKey]:
		args = []string{"maintain", "--push-only"}
	default:
		return work.Done
	}
	code, busy := maintain(ctx, args, log, log)
	switch {
	case ctx.Err() != nil:
		return work.Cut
	case busy:
		return work.Busy
	case code != 0:
		return work.Failed
	}
	return work.Done
}
