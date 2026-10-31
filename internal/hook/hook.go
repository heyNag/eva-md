// Package hook captures host hook events without model calls or network work.
package hook

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/session"
)

const maxPayload = 2 << 20

// Env supplies local configuration and replaceable clock/environment readers.
type Env struct {
	Dirs   paths.Dirs
	Config config.Config
	Getenv func(string) string
	Now    func() time.Time
	// recoverTranscript replaces catchup.Recover in tests. It lives here, not
	// in a package variable, because a test's abandoned capture can still be
	// running when the next test starts.
	recoverTranscript func(ctx context.Context, dirs paths.Dirs, host, id string) (int, error)
	// slack lengthens the hook's own budget in tests, so a busy machine can't
	// fail a test that checks what is stored rather than how long it took.
	slack time.Duration
	// spawn replaces detach, which starts the installed binary with args as
	// a detached job, so tests see exactly which events spawn what.
	spawn func(args ...string)
}

type payload struct {
	Session      string          `json:"session_id"`
	Transcript   string          `json:"transcript_path"`
	Cwd          string          `json:"cwd"`
	Event        string          `json:"hook_event_name"`
	Prompt       string          `json:"prompt"`
	Reply        *string         `json:"last_assistant_message"`
	Model        string          `json:"model"`
	Turn         string          `json:"turn_id"`
	PromptID     string          `json:"prompt_id"`
	Agent        string          `json:"agent_id"`
	Tool         string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
	Error        json.RawMessage `json:"error"`
	Source       string          `json:"source"`
	ToModel      string          `json:"to_model"`
	Summary      string          `json:"compact_summary"`
	// entrypoint and headless are set from the environment, never the
	// payload: Claude Code's entrypoint, cleaned for the heartbeat, and
	// whether the hook runs in a chat a program started (see entrypoint).
	entrypoint string
	headless   bool
}

var stopEvent = regexp.MustCompile(`"hook_event_name"\s*:\s*"Stop"`)

// Run handles one hook within 300 ms (1.5 seconds for SessionStart). It always
// returns zero, contains panics, and never writes a block decision. Codex Stop
// receives exactly {} even when guards or capture failures skip the record.
// The detached jobs a capture asks for start after the output is written.
func Run(ctx context.Context, host string, in io.Reader, out io.Writer, env Env) (code int) {
	started := time.Now()
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := invocationDeadline(ctx, started.Add(300*time.Millisecond+env.slack))
	var event, output string
	var spawns [][]string
	// late is the payload of a capture that ran out of time: a failed run,
	// recorded once the output is written.
	var late *payload
	defer func() {
		_ = recover()
		code = 0
		if host == "codex" && event == "Stop" {
			output = "{}"
		}
		writeOutput(out, output, deadline)
		launch(env, spawns)
		if late != nil {
			lateBeat(ctx, env.Dirs, host, *late)
		}
	}()
	if host != "claude-code" && host != "codex" {
		return 0
	}
	// Reserve a short output phase so even an expired capture can emit the
	// host's required response without exceeding the invocation budget.
	readCtx, cancelRead := context.WithDeadline(ctx, deadline.Add(-10*time.Millisecond))
	defer cancelRead()
	type result struct {
		p     payload
		err   error
		event string
	}
	read := make(chan result, 1)
	go func() {
		r := result{}
		defer func() {
			if recover() != nil {
				r.err = errors.New("hook: payload read failed")
			}
			read <- r
		}()
		data, err := io.ReadAll(io.LimitReader(in, maxPayload+1))
		if stopEvent.Match(data) {
			r.event = "Stop"
		}
		if err != nil || len(data) > maxPayload {
			r.err = errors.New("hook: invalid payload")
			return
		}
		r.err = json.Unmarshal(data, &r.p)
		if r.p.Event != "" {
			r.event = r.p.Event
		}
	}()
	var p payload
	select {
	case r := <-read:
		event = r.event
		if r.err != nil {
			return 0
		}
		p = r.p
	case <-readCtx.Done():
		return 0
	}
	p.entrypoint, p.headless = entrypoint(host, env.Getenv)
	// The brief gets longer. So do tool events, which the plugins mark async,
	// so Claude Code doesn't wait for them: a burst of parallel tool calls
	// waits its turn for the session lock, and that wait is no failure. On a
	// host that did wait, a contended lock would cost it the longer wait
	// where it used to cost the captured line.
	budget := 300 * time.Millisecond
	switch event {
	case "SessionStart", "PostToolUse", "PostToolUseFailure":
		budget = 1500 * time.Millisecond
	}
	deadline = invocationDeadline(ctx, started.Add(budget+env.slack))
	workCtx, cancel := context.WithDeadline(ctx, deadline.Add(-10*time.Millisecond))
	defer cancel()
	// A capture that misses the deadline delivers nothing, its spawns
	// included.
	type captured struct {
		output string
		spawns [][]string
	}
	done := make(chan captured, 1)
	go func() {
		var c captured
		defer func() { _ = recover(); done <- c }()
		c.output = capture(workCtx, host, p, env, &c.spawns)
	}()
	select {
	case c := <-done:
		output, spawns = c.output, c.spawns
	case <-workCtx.Done():
		late = &p
	}
	return 0
}

func invocationDeadline(ctx context.Context, deadline time.Time) time.Time {
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		return parent
	}
	return deadline
}

// A caller-supplied io.Writer cannot be forcibly interrupted. Bound our wait,
// contain its panic, and let the CLI process exit end a permanently stalled
// write. Output to a stalled destination can consequently be lost.
func writeOutput(out io.Writer, output string, deadline time.Time) {
	if out == nil || output == "" {
		return
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return
	}
	done := make(chan struct{})
	go func() { defer close(done); defer func() { _ = recover() }(); _, _ = io.WriteString(out, output) }()
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// capture stores the event and returns the hook's output. The update
// triggers add the detached jobs they ask for to spawns, which Run starts
// once the output is written.
func capture(ctx context.Context, host string, p payload, env Env, spawns *[][]string) string {
	if env.Getenv == nil {
		env.Getenv = os.Getenv
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.recoverTranscript == nil {
		env.recoverTranscript = catchup.Recover
		if p.Event == "Stop" && p.Reply != nil && *p.Reply != "" {
			env.recoverTranscript = catchup.RecoverAfterReply
		}
	}
	if env.Getenv("EVA_MD_INNER") == "1" {
		return ""
	}
	if env.Getenv("CLAUDE_CODE_REMOTE") == "true" {
		return ""
	}
	if !filepath.IsAbs(p.Cwd) || !safeID(p.Session) || !filepath.IsAbs(env.Dirs.State) {
		return ""
	}
	if err := ctx.Err(); err != nil {
		return ""
	}
	cwd, err := filepath.EvalSymlinks(p.Cwd)
	if err != nil {
		return ""
	}
	home := env.Getenv("HOME")
	if !filepath.IsAbs(home) {
		return ""
	}
	if actual, err := filepath.EvalSymlinks(home); err == nil {
		home = actual
	}
	// Check lexical and resolved paths so an alias cannot evade exclusions.
	if env.Config.Excluded(p.Cwd, "") || env.Config.Excluded(cwd, "") || inPath(cwd, env.Config.WikiPath) || inPath(cwd, env.Dirs.State) || tempPath(cwd, env.Getenv) {
		return ""
	}
	if host == "codex" && (inPath(cwd, filepath.Join(home, ".codex", "memories")) || strings.HasSuffix(filepath.ToSlash(cwd), "/.codex/memories") || strings.HasPrefix(strings.TrimSpace(p.Prompt), "## Memory Writing Agent")) {
		return ""
	}
	// Remote exclusion must also precede marker writes, including spare sessions.
	project, err := identity.Cache{Dir: filepath.Join(env.Dirs.Cache(), "identity")}.Resolve(ctx, cwd)
	if err != nil || ctx.Err() != nil {
		// The event is lost: a failed run, which the doctor counts.
		heartbeat(ctx, env.Dirs, host, p, true, env.Now())
		return ""
	}
	if env.Config.Excluded(cwd, project.Remote) {
		return ""
	}
	markers := session.Markers{Root: env.Dirs.Sessions()}
	model := cleanField(p.Model, 256)
	if p.Event == "PostModelSwitch" {
		model = cleanField(p.ToModel, 256)
	}
	if inPath(cwd, filepath.Join(home, ".claude", "spares")) {
		if model != "" {
			_ = remember(ctx, markers, host, p.Session, session.Model, model)
		}
		return ""
	}
	var touched []catchup.Recovered
	if p.Event == "SessionStart" {
		catchCtx, cancelCatch := context.WithTimeout(ctx, 150*time.Millisecond)
		_, touched, _ = catchup.RecoverProject(catchCtx, env.Dirs, project.Key, host, p.Session, env.Now())
		cancelCatch()
	}
	// A run that panics, or that an error stops before its event is stored,
	// failed; the doctor counts them. Run records a run out of time.
	failed := false
	fail := func() string { failed = true; return "" }
	defer func() {
		if recover() != nil {
			failed = true
		}
		heartbeat(ctx, env.Dirs, host, p, failed, env.Now())
	}()
	// The day's maintain and, at a chat start, a push retry: launch decides
	// whether each is due.
	queueMaintain(spawns, env, p.Event == "SessionStart")
	release, err := sessionLock(ctx, env.Dirs.Locks(), host, p.Session)
	if err != nil {
		return fail()
	}
	defer release()
	store := inbox.Store{Root: env.Dirs.Inbox()}
	file := store.Path(project.Key, host, p.Session)
	// The prompt trigger and the SessionStart catch-up need the session's
	// file as it was before this hook appended anything, transcript recovery
	// included: one stat.
	var before os.FileInfo
	if updatesOn(env.Config) && (p.Event == "UserPromptSubmit" || p.Event == "SessionStart") {
		if info, err := os.Lstat(file); err == nil {
			before = info
		}
	}
	start := startState{before: before, recovered: touched}
	if model != "" {
		if err := remember(ctx, markers, host, p.Session, session.Model, model); err != nil {
			return fail()
		}
	} else {
		value, _, err := markers.GetContext(ctx, host, p.Session, session.Model)
		if err != nil {
			return fail()
		}
		model = cleanField(value, 256)
	}
	_, off, err := markers.GetContext(ctx, host, p.Session, session.OffRecord)
	if err != nil {
		return fail()
	}
	thread, _ := redact.Identifier(project.Branch, 1024)
	if project.Detached {
		value, _, err := markers.GetContext(ctx, host, p.Session, session.Thread)
		if err != nil {
			return fail()
		}
		thread, _ = redact.Identifier(value, 1024)
		if thread == "" {
			thread = "default"
		}
	} else if thread != "" {
		if err := remember(ctx, markers, host, p.Session, session.Thread, thread); err != nil {
			return fail()
		}
	}
	project.Branch = thread
	turn := p.Turn
	if turn == "" {
		turn = p.PromptID
	}
	rel, err := filepath.Rel(home, cwd)
	if err != nil {
		rel = ""
	}
	now := env.Now()
	if ctx.Err() != nil {
		return fail()
	}
	e := inbox.Entry{V: 1, TS: now.Format(time.RFC3339), Host: host, Session: p.Session, Turn: cleanField(turn, 256), Agent: cleanField(p.Agent, 256), Project: project.Key, Branch: thread, Cwd: cleanField(rel, 4096), Model: model, Origin: "hook"}
	// Registration never imports pre-install history; a known transcript starts at its current end.
	_ = catchup.Register(ctx, env.Dirs, home, p.Transcript, cwd, e, off, p.headless, now)
	recovered := 0
	// caughtUp runs transcript recovery and reports whether it read to the
	// end of the transcript, so the current event may be stored after what
	// the host wrote before it. A pass that runs out of time saves its cursor
	// and returns ErrPending, and the next event's pass carries on from there.
	// A pass that returns no error has read to the end, even when it finished
	// in the grace recovery gets past its deadline to save the cursor.
	caughtUp := func() bool {
		catchCtx, cancelCatch := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancelCatch()
		n, err := env.recoverTranscript(catchCtx, env.Dirs, host, p.Session)
		recovered += n
		if err == nil {
			return true
		}
		return catchCtx.Err() == nil && !errors.Is(err, catchup.ErrPrivacyBoundary) && !errors.Is(err, catchup.ErrPending)
	}
	appendEvent := func(text string) bool {
		e.Role = "event"
		e.Kind = "event"
		e.Text = text
		if store.AppendContext(ctx, e) != nil {
			failed = true
			return false
		}
		return true
	}
	// markersOnly keeps what a chat that is off the record keeps: the event's
	// fixed name, if it has one, and nothing the user or the model wrote.
	markersOnly := func() string {
		if event := eventName(p.Event); event != "" {
			appendEvent(event)
		}
		return ""
	}
	// A record control is honoured in a headless chat too: only less is kept.
	if p.Event == "UserPromptSubmit" && !synthetic(p) {
		control := recordControl(p.Prompt)
		if control == "off_record" {
			if err := markers.SetContext(ctx, host, p.Session, session.OffRecord, catchup.OffRecordAsked); err != nil {
				return fail()
			}
			_ = catchup.SetOffRecord(ctx, env.Dirs, host, p.Session, true)
			appendEvent(control)
			return confirmation(host, false)
		}
		if control == "on_record" {
			if err := catchup.SetOffRecord(ctx, env.Dirs, host, p.Session, false); err != nil {
				return fail()
			}
			if err := markers.ClearContext(ctx, host, p.Session, session.OffRecord); err != nil {
				return fail()
			}
			appendEvent(control)
			return confirmation(host, true)
		}
	}
	if off {
		// Only fixed event names and model markers survive off-record mode.
		return markersOnly()
	}
	// Until recovery has read the transcript to its end, unread rows may hold
	// an off-the-record control a hook missed, so the chat may be off the
	// record: keep only what off-record mode keeps, and no brief. The prompts
	// and replies held back are in the transcript, and a later pass recovers
	// them.
	if !caughtUp() {
		return markersOnly()
	}
	_, paused, err := markers.GetContext(ctx, host, p.Session, session.OffRecord)
	if err != nil {
		return fail()
	}
	if paused {
		return markersOnly()
	}
	switch p.Event {
	case "UserPromptSubmit":
		if p.Prompt == "" || builtinCommand(p.Prompt) {
			return ""
		}
		if synthetic(p) || p.headless {
			appendEvent("host_prompt")
			return ""
		}
		if gapBacklogged(store, file, before, now) {
			queueUpdate(spawns, env, host+":"+p.Session)
		}
		e.Role = "user"
		e.Kind = "prompt"
		e.Text = p.Prompt
	case "Stop":
		if p.Reply == nil || *p.Reply == "" {
			appendEvent("gap: missing final reply")
			return ""
		}
		e.Role = "assistant"
		e.Kind = "reply"
		e.Text = *p.Reply
	case "PostToolUse", "PostToolUseFailure":
		e.Role, e.Kind, e.Text = toolLine(host, p)
	case "PostCompact":
		e.Role = "host"
		e.Kind = "host_summary"
		e.Text = p.Summary
	case "StopFailure":
		appendEvent("stop_failure" + failureType(p.Error))
		return ""
	default:
		if event := eventName(p.Event); event != "" {
			if p.Event == "SessionStart" && p.Source != "" {
				event += ": " + cleanField(p.Source, 80)
			}
			appendEvent(event)
		}
		if p.Event == "SessionEnd" || p.Event == "PreCompact" {
			queueUpdate(spawns, env, host+":"+p.Session)
		}
		return resumeContext(ctx, host, p, env, project, now, start, spawns)
	}
	if e.Text != "" {
		duplicate := false
		if recovered > 0 && (e.Role == "user" || e.Role == "assistant") {
			duplicate, _ = catchup.Contains(ctx, env.Dirs, e)
		}
		if !duplicate && store.AppendContext(ctx, e) != nil {
			failed = true
		}
	}
	if p.Event == "Stop" && updatesOn(env.Config) && stopBacklogged(store, file) {
		queueUpdate(spawns, env, host+":"+p.Session)
	}
	return resumeContext(ctx, host, p, env, project, now, start, spawns)
}

// Ordinary captures hold the session lock here. Keeping identical markers in
// place avoids four serialized fsyncs per event without weakening durability
// when a model or branch actually changes.
func remember(ctx context.Context, markers session.Markers, host, id, name, value string) error {
	previous, exists, err := markers.GetContext(ctx, host, id, name)
	if err != nil {
		return err
	}
	if exists && previous == value {
		return ctx.Err()
	}
	return markers.SetContext(ctx, host, id, name, value)
}

func safeID(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	clean, n := redact.Redact(s)
	return n == 0 && clean == s
}

func cleanField(s string, limit int) string {
	s, _ = redact.Redact(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	runes := []rune(s)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func inPath(child, parent string) bool {
	if parent == "" || !filepath.IsAbs(parent) {
		return false
	}
	if actual, err := filepath.EvalSymlinks(parent); err == nil {
		parent = actual
	}
	child, parent = filepath.Clean(child), filepath.Clean(parent)
	if runtime.GOOS == "darwin" {
		child, parent = strings.ToLower(child), strings.ToLower(parent)
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

func tempPath(cwd string, getenv func(string) string) bool {
	tmp := getenv("TMPDIR")
	if tmp == "" {
		tmp = os.TempDir()
	}
	for _, root := range []string{tmp, "/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp"} {
		if inPath(cwd, root) {
			return true
		}
	}
	return false
}

func sessionLock(ctx context.Context, dir, host, id string) (func(), error) {
	return session.Lock(ctx, dir, host, id)
}

func eventName(event string) string {
	switch event {
	case "SessionStart":
		return "session_start"
	case "SessionEnd":
		return "session_end"
	case "PreCompact":
		return "compact"
	case "PostCompact":
		return "compact"
	case "PostModelSwitch":
		return "model_switch"
	case "StopFailure":
		return "stop_failure"
	case "Interrupt":
		return "interrupt"
	}
	return ""
}

func recordControl(prompt string) string { return session.RecordControl(prompt) }

func synthetic(p payload) bool {
	if p.Agent != "" {
		return true
	}
	switch strings.ToLower(p.Source) {
	case "scheduled", "scheduled_task", "scheduler", "cron", "loop", "subagent", "agent", "background", "other_session", "session_message", "system", "automation":
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(p.Prompt))
	for _, prefix := range []string{"<task-notification>", "<teammate-message", "<system-reminder>", "<local-command-stdout>", "<command-name>", "[message from ", "## memory writing agent", "/loop "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// entrypoint returns Claude Code's entrypoint, which its hooks inherit as
// CLAUDE_CODE_ENTRYPOINT, cleaned for the heartbeat, where the doctor shows
// it, and whether it is a program's (see headless). A Codex chat has none.
func entrypoint(host string, getenv func(string) string) (string, bool) {
	if host != "claude-code" {
		return "", false
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	value := getenv("CLAUDE_CODE_ENTRYPOINT")
	return cleanField(value, 32), headless(value)
}

// headless reports whether Claude Code's entrypoint is a program's: print
// mode (claude -p) or the Agent SDK in TypeScript or Python. Such a chat's
// prompts are the program's, so they are stored as host prompts. Any other
// value, unknown ones included, is taken for a person's chat. The desktop
// app (claude-desktop) and the VS Code extension (claude-vscode) set their
// own value before the Agent SDK they embed falls back to sdk-ts.
func headless(entrypoint string) bool {
	switch entrypoint {
	case "sdk-cli", "sdk-ts", "sdk-py":
		return true
	}
	return false
}

func builtinCommand(prompt string) bool {
	fields := strings.Fields(strings.TrimSpace(prompt))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "/clear", "/compact", "/resume", "/fork", "/model", "/help", "/exit", "/quit", "/memory", "/memories", "/hooks", "/status", "/config", "/permissions", "/cost", "/context", "/login", "/logout":
		return true
	}
	return false
}

func confirmation(host string, on bool) string {
	text := "eva.md: off the record. Capture is paused for this chat."
	if on {
		text = "eva.md: back on the record. Capture has resumed for this chat."
	}
	output := map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "UserPromptSubmit", "additionalContext": text}}
	if host == "claude-code" {
		output["systemMessage"] = text
	}
	data, _ := json.Marshal(output)
	return string(data)
}

func failureType(raw json.RawMessage) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		var kind string
		_ = json.Unmarshal(obj["type"], &kind)
		switch kind {
		case "rate_limit", "rate_limit_error", "overloaded_error", "api_error", "authentication_error", "permission_error", "invalid_request_error":
			return ": " + kind
		}
	}
	return ""
}

var claudeExit = regexp.MustCompile(`^Exit code (-?\d+)\b`)
var codexExit = regexp.MustCompile(`(?m)^\s*Exit code:\s*(-?\d+)\b`)

func toolLine(host string, p payload) (role, kind, text string) {
	role, kind = "tool", "tool"
	name := cleanField(p.Tool, 80)
	if name == "" {
		name = "unknown"
	}
	text = name
	if p.Agent != "" {
		text += " (subagent)"
	}
	var input map[string]json.RawMessage
	_ = json.Unmarshal(p.ToolInput, &input)
	get := func(key string) string { var s string; _ = json.Unmarshal(input[key], &s); return s }
	switch p.Tool {
	case "Bash":
		command := get("command")
		if command == "" {
			command = get("cmd")
		}
		if command == "" {
			_ = json.Unmarshal(p.ToolInput, &command)
		}
		// Redact before cutting: a token spanning the boundary must not leak a prefix.
		command, _ = redact.Redact(command)
		command, _, _ = strings.Cut(command, "\n")
		if head := cleanField(command, 200); head != "" {
			text += " " + head
		}
	case "Edit", "Write", "NotebookEdit", "Read":
		file := get("file_path")
		if file == "" {
			file = get("notebook_path")
		}
		if file == "" {
			file = get("path")
		}
		if file != "" {
			text += " " + cleanField(file, 1024)
		}
	case "apply_patch":
		patch := get("patch")
		if patch == "" {
			patch = get("input")
		}
		if patch == "" {
			_ = json.Unmarshal(p.ToolInput, &patch)
		}
		var files []string
		for _, line := range strings.Split(patch, "\n") {
			for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: "} {
				if file, ok := strings.CutPrefix(line, prefix); ok && len(files) < 64 {
					files = append(files, cleanField(strings.TrimSpace(file), 1024))
				}
			}
		}
		if len(files) > 0 {
			text += " " + strings.Join(files, ", ")
		}
	case "Grep", "Glob":
		if pattern := cleanField(get("pattern"), 200); pattern != "" {
			text += " " + strconv.Quote(pattern)
		}
		if path := get("path"); path != "" {
			text += " " + cleanField(path, 1024)
		}
	case "WebFetch":
		if address := fetchedURL(get("url")); address != "" {
			text += " " + cleanField(address, 1024)
		}
	case "TaskCreate", "TaskUpdate", "TodoWrite", "update_plan":
		role, kind = "plan", "plan"
		if summary := planText(p.Tool, input); summary != "" {
			text += " " + summary
		}
	default:
		if strings.HasPrefix(p.Tool, "mcp__") {
			if args := mcpArgs(input); args != "" {
				text += " " + args
			}
		}
	}
	exit := "unknown"
	if host == "claude-code" {
		if p.Event == "PostToolUse" {
			exit = "0"
		} else {
			var failure string
			_ = json.Unmarshal(p.Error, &failure)
			if m := claudeExit.FindStringSubmatch(failure); len(m) > 1 {
				exit = m[1]
			}
		}
	} else if m := codexExit.FindStringSubmatch(responseText(p.ToolResponse)); len(m) > 1 {
		exit = m[1]
	}
	if !validExit(exit) {
		exit = "unknown"
	}
	text += " (exit " + exit + ")"
	return role, kind, text
}

// fetchedURL is a fetched URL without what can carry a credential: its user
// part, its fragment, and each query parameter secretName names, as a
// signed link's sig= or an OAuth code= does. One that doesn't parse as an
// address is left out.
func fetchedURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" {
		return ""
	}
	q := u.Query()
	for name := range q {
		if secretName(name, true) {
			q.Del(name)
		}
	}
	u.User, u.Fragment, u.RawFragment, u.RawQuery = nil, "", "", q.Encode()
	return u.String()
}

// secretNames name a tool argument or a URL's query parameter that carries
// a credential, as the whole name or one part of it, or two parts joined:
// auth, accessToken, X-Amz-Signature, api_key.
var secretNames = map[string]bool{"auth": true, "authentication": true, "authorization": true, "bearer": true, "cookie": true, "cookies": true,
	"credential": true, "credentials": true, "jwt": true, "otp": true, "passphrase": true, "passwd": true, "password": true,
	"pass": true, "passcode": true, "passkey": true, "pwd": true, "secret": true, "session": true, "sessionid": true,
	"jsessionid": true, "phpsessid": true, "sig": true, "signature": true, "token": true, "apikey": true,
	"accesskey": true, "appkey": true, "privatekey": true}

// secretName reports whether name is one secretNames holds, by its parts
// (see nameParts). In a URL's query, a key or code part counts too, as an
// API key's or an OAuth code's does.
func secretName(name string, query bool) bool {
	parts := nameParts(name)
	for i, p := range parts {
		if secretNames[p] || query && (p == "key" || p == "code") || i > 0 && secretNames[parts[i-1]+p] {
			return true
		}
	}
	return false
}

// addressName reports whether an argument's name says it holds an address:
// url, uri, href, link or address, alone or as one of its parts.
func addressName(name string) bool {
	return slices.ContainsFunc(nameParts(name), func(p string) bool {
		return p == "url" || p == "uri" || p == "href" || p == "link" || p == "address"
	})
}

// nameParts splits a name at '-', '_', '.' and a lower case letter followed
// by a capital, lowercased: X-Amz-Signature gives x, amz, signature.
func nameParts(name string) []string {
	var parts []string
	part, lower := []rune{}, false
	for _, r := range name + "." {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) || unicode.IsUpper(r) && lower {
			if len(part) > 0 {
				parts = append(parts, string(part))
			}
			part = part[:0]
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			part = append(part, unicode.ToLower(r))
		}
		lower = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	return parts
}

// mcpArgs renders an MCP tool's string and number arguments, which name
// what it read (ids, titles, keys, URLs, queries), as key=value pairs,
// shortest first so a long text argument comes last. A value over 1 KiB is
// left out whole, never cut, and so is one whose name says it carries a
// credential; an absolute URL is cleaned as fetchedURL cleans one; and
// cleanField redacts the line before it cuts it to 200 characters, as a Bash
// command is.
func mcpArgs(input map[string]json.RawMessage) string {
	var pairs []string
	for key, raw := range input {
		if len(raw) == 0 || len(raw) > 1<<10 || secretName(key, false) {
			continue
		}
		var value string
		switch c := raw[0]; {
		case c == '"':
			if json.Unmarshal(raw, &value) != nil {
				continue
			}
			value = strings.Join(strings.Fields(value), " ")
			// An address, as a fetch or browser tool takes, is kept as a
			// fetched URL is, without what can carry a credential: one named
			// as an address, even with a space in its path, or one alone in
			// its argument. Text that only starts with one is kept as it is.
			if u, err := url.Parse(value); err == nil && u.Scheme != "" && u.Host != "" && (addressName(key) || !strings.Contains(value, " ")) {
				value = fetchedURL(value)
			}
		case c == '-' || c >= '0' && c <= '9':
			value = string(raw)
		}
		if value != "" {
			pairs = append(pairs, key+"="+value)
		}
	}
	slices.SortFunc(pairs, func(a, b string) int { return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b)) })
	return cleanField(strings.Join(pairs[:min(len(pairs), 16)], " "), 200)
}

func responseText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	for _, key := range []string{"output", "text"} {
		if json.Unmarshal(obj[key], &s) == nil {
			return s
		}
	}
	var content []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(obj["content"], &content) == nil {
		for _, item := range content {
			if match := codexExit.FindString(item.Text); match != "" {
				return match
			}
		}
	}
	return ""
}

func planText(tool string, input map[string]json.RawMessage) string {
	if tool == "TaskCreate" || tool == "TaskUpdate" {
		var subject, status string
		_ = json.Unmarshal(input["subject"], &subject)
		_ = json.Unmarshal(input["status"], &status)
		if subject == "" {
			var id string
			_ = json.Unmarshal(input["taskId"], &id)
			if id != "" {
				subject = "task " + id
			}
		}
		if subject != "" {
			return "[" + planStatus(status) + "] " + cleanField(subject, 2048)
		}
		return ""
	}
	key := "plan"
	if tool == "TodoWrite" {
		key = "todos"
	}
	var steps []struct {
		Step    string `json:"step"`
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if json.Unmarshal(input[key], &steps) != nil {
		return ""
	}
	var lines []string
	for i, step := range steps {
		if i >= 100 {
			break
		}
		label := step.Step
		if label == "" {
			label = step.Content
		}
		if label == "" {
			continue
		}
		lines = append(lines, "["+planStatus(step.Status)+"] "+cleanField(label, 2048))
	}
	return strings.Join(lines, "; ")
}

func planStatus(status string) string {
	switch status {
	case "completed":
		return "x"
	case "in_progress":
		return "~"
	case "pending":
		return " "
	}
	return "?"
}

func validExit(s string) bool { _, err := strconv.ParseInt(s, 10, 32); return err == nil }
