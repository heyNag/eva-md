package hook

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/resume"
	"github.com/heyNag/eva-md/internal/session"
)

// briefMarker is the value of the session.Briefed marker.
type briefMarker struct {
	Hash      string `json:"now_hash"`
	Variant   string `json:"variant"`
	Refreshes int    `json:"refreshes"`
	// Refreshed is what markers recorded before the count: their one
	// refresh, sent.
	Refreshed bool `json:"refreshed,omitempty"`
	// Slug is the project's wiki folder the brief found, the one place a
	// refresh looks.
	Slug string `json:"slug,omitempty"`
}

// maxRefreshes is how many times a changed Now is injected into a chat. A
// compact brief keeps the count; any other brief starts it again.
const maxRefreshes = 2

// used is how many refreshes the marker records.
func (m briefMarker) used() int {
	if m.Refreshed {
		return max(m.Refreshes, 1)
	}
	return m.Refreshes
}

// markerReserve is kept before the hook's deadline for the briefed marker's
// rename and folder sync, which take a few milliseconds. A marker not renamed
// by then is not saved, and its brief is retried on the next prompt.
const markerReserve = 20 * time.Millisecond

// resumeContext returns the brief or refresh for a SessionStart or prompt,
// and with a prompt the lines that ride with it (see promptLines). At
// SessionStart it also asks for the catch-up update, of this chat's own
// backlog and the project's idle sessions, from what start found before the
// hook appended anything and the session files the brief read.
func resumeContext(ctx context.Context, host string, p payload, env Env, project identity.Project, now time.Time, start startState, spawns *[][]string) string {
	if p.Event != "SessionStart" && p.Event != "UserPromptSubmit" {
		return ""
	}
	if p.Event == "UserPromptSubmit" && (synthetic(p) || builtinCommand(p.Prompt)) {
		return ""
	}
	markers := session.Markers{Root: env.Dirs.Sessions()}
	b, ok := brief(ctx, host, p, env, project, now, start, spawns, markers)
	if !ok {
		return ""
	}
	s, lines, changed := nextState(ctx, markers, host, p, env, project.Key, b)
	// The briefed marker is saved first, and only with markerReserve to spare,
	// so a brief lost to the deadline is retried on the next prompt and the
	// chat's state records nothing of it. The state follows, saved in place
	// well within the reserve, and the lines it records go only once it is
	// saved.
	if b.marker != "" && saveMarker(ctx, markers, host, p.Session, session.Briefed, b.marker, markerReserve) != nil {
		return ""
	}
	if changed && saveState(ctx, markers, host, p.Session, s) != nil {
		lines = nil
	}
	r := b.reply
	if len(lines) > 0 {
		if r.context != "" {
			r.context += "\n\n"
		}
		r.context += strings.Join(lines, "\n")
	}
	return r.encode(p.Event)
}

// reply is what a SessionStart or a prompt hands the chat: context for the
// model and a message for the user.
type reply struct{ context, message string }

// encode is the hook's output for event, or "" without context.
func (r reply) encode(event string) string {
	if r.context == "" {
		return ""
	}
	output := map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": event, "additionalContext": r.context}}
	if r.message != "" {
		output["systemMessage"] = r.message
	}
	encoded, _ := json.Marshal(output)
	return string(encoded)
}

// briefed is the brief or refresh a SessionStart or prompt carries: its
// reply, the brief's own text, the briefed marker to save, "" to leave it as
// it is, whether it is a brief rather than a refresh, and whether it starts
// the chat over, as a startup or clear brief does.
type briefed struct {
	reply
	text, marker string
	brief, fresh bool
}

// brief builds the brief or refresh for a SessionStart or prompt, and the
// briefed marker that records it. A prompt with nothing to brief gets none.
// It reports false when the marker or the brief can't be read.
func brief(ctx context.Context, host string, p payload, env Env, project identity.Project, now time.Time, start startState, spawns *[][]string, markers session.Markers) (briefed, bool) {
	variant := p.Source
	previous, found, err := markers.GetContext(ctx, host, p.Session, session.Briefed)
	if err != nil {
		return briefed{}, false
	}
	var marker briefMarker
	if found && json.Unmarshal([]byte(previous), &marker) != nil {
		found = false
	}
	if p.Event == "UserPromptSubmit" {
		// After a brief, every prompt compares the thread's Now with the one
		// the chat last saw, until the refreshes are used up.
		if !found {
			variant = "startup"
		} else if marker.used() >= maxRefreshes {
			return briefed{}, true
		} else {
			variant = "refresh"
		}
	}
	switch variant {
	case "startup", "clear", "resume", "fork", "compact", "refresh":
	default:
		variant = "startup"
	}
	result, err := resume.Build(ctx, resume.Options{Dirs: env.Dirs, Config: env.Config, Project: project, Host: host, Session: p.Session, Variant: variant, Now: now, PreviousHash: marker.Hash, Slug: marker.Slug})
	if err != nil || ctx.Err() != nil {
		return briefed{}, false
	}
	if p.Event == "SessionStart" && updatesOn(env.Config) {
		store := inbox.Store{Root: env.Dirs.Inbox()}
		own, recovered := start.files(store, project.Key, host, p.Session)
		// A person's chat start asks for the day's sweep too; a program's
		// starts no model calls for other projects' chats.
		queueCatchUp(spawns, env, !p.headless, idleBacklogs(ctx, store, own, result.Files, recovered, now)...)
	}
	if variant == "refresh" {
		// An unchanged Now injects nothing, uses no refresh and leaves the
		// marker as it is.
		if result.Text == "" {
			return briefed{}, true
		}
		marker = briefMarker{Hash: result.NowHash, Variant: marker.Variant, Refreshes: marker.used() + 1, Slug: result.Slug}
	} else {
		// A compact brief continues the same chat, so the refreshes it
		// already had still count.
		used := 0
		if variant == "compact" && found {
			used = marker.used()
		}
		marker = briefMarker{Hash: result.NowHash, Variant: variant, Refreshes: used, Slug: result.Slug}
	}
	notice := ""
	if p.Event == "SessionStart" || !found {
		notice = health.QuickNotice(ctx, env.Dirs, env.Config, host)
	}
	data, _ := json.Marshal(marker)
	b := briefed{reply: reply{context: result.Text}, text: result.Text, marker: string(data), brief: variant != "refresh", fresh: variant == "startup" || variant == "clear"}
	// A health notice is a fixed diagnostic, not recovered chat content.
	if notice != "" && b.context == "" {
		b.context = notice
	}
	if notice != "" {
		b.message = notice
	} else if host == "claude-code" && result.Found && variant != "refresh" && env.Config.Notice != "problems" {
		b.message = "eva.md: earlier work is available for this chat."
	}
	return b, true
}

// saveMarker saves a session marker, only with reserve to spare before the
// hook's deadline.
func saveMarker(ctx context.Context, markers session.Markers, host, id, name, value string, reserve time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-reserve))
		defer cancel()
	}
	return markers.SetContext(ctx, host, id, name, value)
}

// maxRuns is how many runs each event's heartbeat keeps, so the doctor can
// find a host's last 20 runs, whichever events they were.
const maxRuns = 20

// beatRun is one hook run in a heartbeat's history: when it ran, whether it
// failed, and how long it took, in milliseconds, from the start of Run to
// just before its heartbeat is saved: the process's start and the save
// itself, an atomic write with fsync, are left out.
type beatRun struct {
	At         string  `json:"at"`
	Failed     bool    `json:"failed,omitempty"`
	DurationMS float64 `json:"duration_ms,omitempty"`
}

// beatEvent reports whether event is one the hooks keep a heartbeat for.
func beatEvent(event string) bool {
	switch event {
	case "SessionStart", "SessionEnd", "UserPromptSubmit", "PostToolUse", "PostToolUseFailure", "Stop", "StopFailure", "PreCompact", "PostCompact", "PostModelSwitch", "Interrupt":
		return true
	}
	return false
}

// heartbeat records the run of p's event, which took took, with the
// event's last maxRuns runs, the Claude Code entrypoint the run inherited
// and whether its transcript lies where recovery can't read it.
func heartbeat(ctx context.Context, dirs paths.Dirs, host string, p payload, failed bool, now time.Time, took time.Duration) {
	event := p.Event
	if !beatEvent(event) {
		return
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "heartbeats"), true)
	if err != nil {
		return
	}
	defer root.Close()
	if root.MkdirAll(host, 0700) != nil {
		return
	}
	name := filepath.Join(host, event+".json")
	at := now.Format(time.RFC3339Nano)
	runs := append(previousRuns(root, name), beatRun{at, failed, float64(took.Microseconds()) / 1000})
	runs = runs[max(0, len(runs)-maxRuns):]
	data, _ := json.Marshal(struct {
		V          int       `json:"v"`
		At         string    `json:"at"`
		Host       string    `json:"host"`
		Event      string    `json:"event"`
		Transcript bool      `json:"transcript_available"`
		Elsewhere  bool      `json:"transcript_elsewhere,omitempty"`
		Runs       []beatRun `json:"runs"`
		Entrypoint string    `json:"entrypoint,omitempty"`
		Headless   bool      `json:"headless,omitempty"`
	}{1, at, host, event, p.Transcript != "", p.elsewhere, runs, p.entrypoint, p.headless})
	_ = paths.WriteFileAtomicAt(ctx, root, name, data, 0600)
}

// uncapturedFile is the stamp a run that captures nothing leaves in the
// host's heartbeats folder.
const uncapturedFile = "uncaptured.json"

// stampLayout writes the stamp's time, in UTC, at one width whatever the
// time, so every stamp is as long as any other.
const stampLayout = "2006-01-02T15:04:05.000000000Z07:00"

// uncaptured stamps the time of a run that stops before capture because
// eva-md doesn't capture its folder: an excluded path or remote, a
// temporary folder, the wiki, eva-md's state folder, a Codex memory-writing
// run or a desktop spare. The host still keeps the chat, so the
// capture-freshness check reads the stamp as proof that the host's hooks
// ran, and raises no false alarm. It holds the time alone, never the
// folder, the session or the event, and the hook-failure count never reads
// it. Losing it costs at most a false alarm, so it is written in place
// without fsync, as the prompt state is, which an atomic save would cost
// about 11 ms. Runs that stamp at once, as the async hooks of parallel tool
// calls do, take no lock: each writes a stamp of the same width over the
// last, never truncating it, so once a stamp is written the file holds one
// whole stamp, the last one written.
func uncaptured(env Env, host, event string, now time.Time) {
	dirs := env.Dirs
	if !beatEvent(event) || !filepath.IsAbs(dirs.State) {
		return
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "heartbeats"), true)
	if err != nil {
		return
	}
	defer root.Close()
	if root.MkdirAll(host, 0o700) != nil {
		return
	}
	f, err := root.OpenFile(filepath.Join(host, uncapturedFile), os.O_WRONLY|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	// The stamp is written only once it is known to be a regular file of
	// its own.
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
		return
	}
	data, _ := json.Marshal(struct {
		At string `json:"at"`
	}{now.UTC().Format(stampLayout)})
	if env.stamping != nil {
		env.stamping()
	}
	_, _ = f.WriteAt(data, 0)
}

// previousRuns reads the history of the heartbeat about to be replaced. Two
// runs of one event that end together may each keep only its own run.
func previousRuns(root *os.Root, name string) []beatRun {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	var old struct {
		Runs []beatRun `json:"runs"`
	}
	if json.NewDecoder(io.LimitReader(f, 16<<10)).Decode(&old) != nil {
		return nil
	}
	return old.Runs
}

// lateBeat records a run whose capture ran out of time as failed, with the
// time it took and whether its transcript lies elsewhere, once its output
// was written. It has a moment of its own, as the run's time is up,
// lengthened by the hook's slack, and reads the system clock, since
// env.Now may be what held the capture up.
func lateBeat(ctx context.Context, env Env, host string, p payload, took time.Duration) {
	defer func() { _ = recover() }()
	if !filepath.IsAbs(env.Dirs.State) {
		return
	}
	getenv := env.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	p.elsewhere = transcriptElsewhere(host, p.Transcript, getenv("HOME"))
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 100*time.Millisecond+env.Slack)
	defer cancel()
	heartbeat(ctx, env.Dirs, host, p, true, time.Now(), took)
}
