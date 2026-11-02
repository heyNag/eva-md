package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

func setup(t *testing.T) (Env, payload) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("TMPDIR", filepath.Join(home, "tmp"))
	t.Setenv("EVA_MD_INNER", "")
	t.Setenv("CLAUDE_CODE_REMOTE", "")
	// A test run from a headless Claude Code chat inherits its entrypoint.
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	if err := os.MkdirAll(filepath.Join(home, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	// Hooks start nothing in tests unless a test asks: spawn records nothing.
	// Nor do prompts get the memory reminder. Every time limit of the hook is
	// lengthened by busySlack; a test of the hook's own limits sets Slack to 0.
	c := config.Default()
	c.ReminderEvery = 0
	env := Env{Dirs: dirs, Config: c, Getenv: os.Getenv, Now: func() time.Time { return time.Date(2026, 10, 29, 15, 4, 5, 0, time.FixedZone("ICT", 7*3600)) }, Slack: busySlack, spawn: func(...string) {}}
	return env, payload{Session: "test-session", Cwd: filepath.Join(home, "project"), Model: "model-test", Event: "UserPromptSubmit", Prompt: "please help", Turn: "turn-1"}
}

// busySlack lengthens every time limit of a test's hooks, so a busy machine
// can't fail a test that checks what a hook stores, not how long it takes.
// It is over 30 times what the slowest such test, eight hooks taking turns
// for one session's lock, takes here under the race detector.
const busySlack = 30 * time.Second

// hangGuard is how long a test waits for what must happen however slow the
// machine, such as a hook returning at its deadline, before it reports a
// hang. It is longer than a hook's budget lengthened by busySlack.
const hangGuard = time.Minute

// mustReturn runs f, which must return however slow the machine, and fails
// the test, naming what hung, if f has not returned after hangGuard.
func mustReturn(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(hangGuard):
		t.Fatalf("%s hung: it had not returned after %v", what, hangGuard)
	}
}

func invoke(t *testing.T, host string, p payload, env Env) string {
	t.Helper()
	return invokeContext(t, context.Background(), host, p, env)
}

// invokeContext is invoke under ctx: cancelling ctx ends the hook's time,
// as its deadline would.
func invokeContext(t *testing.T, ctx context.Context, host string, p payload, env Env) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Run(ctx, host, bytes.NewReader(data), &out, env); code != 0 {
		t.Fatalf("hook exit=%d", code)
	}
	return out.String()
}

func records(t *testing.T, env Env) []inbox.Entry {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(env.Dirs.Inbox(), "*", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var all []inbox.Entry
	for _, file := range files {
		rows, _, err := (inbox.Store{Root: env.Dirs.Inbox()}).ReadFrom(file, 0)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, rows...)
	}
	return all
}

func TestEveryEventOnBothHosts(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		for _, tc := range []struct{ event, role, kind, text string }{
			{"UserPromptSubmit", "user", "prompt", "please help"},
			{"Stop", "assistant", "reply", "the final reply"},
			{"PostToolUse", "tool", "tool", "Write file.txt (exit "},
			{"PostToolUseFailure", "tool", "tool", "Write file.txt (exit "},
			{"SessionStart", "event", "event", "session_start"},
			{"SessionEnd", "event", "event", "session_end"},
			{"PreCompact", "event", "event", "compact"},
			{"PostCompact", "host", "host_summary", "host summary"},
			{"PostModelSwitch", "event", "event", "model_switch"},
			{"StopFailure", "event", "event", "stop_failure: rate_limit_error"},
			{"Interrupt", "event", "event", "interrupt"},
		} {
			t.Run(host+"/"+tc.event, func(t *testing.T) {
				env, p := setup(t)
				p.Event = tc.event
				reply := "the final reply"
				p.Reply = &reply
				p.Summary = "host summary"
				p.ToModel = "next-model"
				p.Tool = "Write"
				p.ToolInput = json.RawMessage(`{"file_path":"file.txt","content":"not captured"}`)
				p.ToolResponse = json.RawMessage(`"Exit code: 7\nTOOL OUTPUT MUST NOT APPEAR"`)
				p.Error = json.RawMessage(`{"type":"rate_limit_error","message":"DO NOT SAVE THIS ERROR"}`)
				if p.Event == "PostToolUseFailure" {
					p.Error = json.RawMessage(`"Exit code 9\nPRIVATE FAILURE OUTPUT"`)
				}
				out := invoke(t, host, p, env)
				wantOut := ""
				if host == "codex" && tc.event == "Stop" {
					wantOut = "{}"
				}
				// The brief of a first chat is the footer alone.
				if tc.event == "SessionStart" || tc.event == "UserPromptSubmit" {
					if context := contextFrom(t, out); !strings.HasPrefix(context, "No earlier chats here;") || !strings.Contains(context, " search --all ") || strings.Contains(out, "systemMessage") {
						t.Fatalf("first chat stdout=%q", out)
					}
				} else if out != wantOut {
					t.Fatalf("stdout=%q want=%q", out, wantOut)
				}
				rows := records(t, env)
				if len(rows) != 1 {
					t.Fatalf("records=%#v", rows)
				}
				e := rows[0]
				if e.Role != tc.role || e.Kind != tc.kind || !strings.HasPrefix(e.Text, tc.text) {
					t.Fatalf("record=%#v", e)
				}
				if e.Cwd != "project" || e.TS != "2026-10-29T15:04:05+07:00" || e.Project != "general" || e.Origin != "hook" {
					t.Fatalf("metadata=%#v", e)
				}
				if strings.Contains(e.Text, "OUTPUT") || strings.Contains(e.Text, "ERROR") || strings.Contains(e.Text, "not captured") {
					t.Fatal("tool or error output leaked")
				}
				model, ok, err := (session.Markers{Root: env.Dirs.Sessions()}).Get(host, p.Session, session.Model)
				wantModel := "model-test"
				if tc.event == "PostModelSwitch" {
					wantModel = "next-model"
				}
				if err != nil || !ok || model != wantModel {
					t.Fatalf("model marker=%q %v %v", model, ok, err)
				}
			})
		}
	}
}

func TestGuardsAndOrdering(t *testing.T) {
	for _, name := range []string{"inner", "remote", "excluded", "wiki", "state", "temp", "memory-cwd", "memory-prompt", "spare", "symlink-excluded", "remote-excluded"} {
		t.Run(name, func(t *testing.T) {
			env, p := setup(t)
			p.Event = "Stop"
			reply := "DO NOT CAPTURE"
			p.Reply = &reply
			switch name {
			case "inner":
				env.Getenv = func(key string) string {
					if key == "EVA_MD_INNER" {
						return "1"
					}
					panic("later guard reached")
				}
			case "remote":
				env.Getenv = func(key string) string {
					if key == "EVA_MD_INNER" {
						return ""
					}
					if key == "CLAUDE_CODE_REMOTE" {
						return "true"
					}
					panic("later guard reached")
				}
			case "excluded":
				env.Config.Exclude = []string{p.Cwd}
			case "wiki":
				env.Config.WikiPath = p.Cwd
			case "state":
				p.Cwd = env.Dirs.State
			case "temp":
				p.Cwd = os.Getenv("TMPDIR")
			case "memory-cwd":
				p.Cwd = filepath.Join(os.Getenv("HOME"), ".codex", "memories")
			case "memory-prompt":
				p.Prompt = "## Memory Writing Agent\nprivate system prompt"
			case "spare":
				p.Cwd = filepath.Join(os.Getenv("HOME"), ".claude", "spares", "spare-1")
			case "symlink-excluded":
				env.Config.Exclude = []string{p.Cwd}
				alias := filepath.Join(os.Getenv("HOME"), "alias")
				if err := os.Symlink(p.Cwd, alias); err != nil {
					t.Fatal(err)
				}
				p.Cwd = alias
			case "remote-excluded":
				git(t, p.Cwd, "init", "--quiet")
				git(t, p.Cwd, "remote", "add", "origin", "https://github.com/private/project.git")
				env.Config.Exclude = []string{"remote:github.com/private/*"}
			}
			if err := os.MkdirAll(p.Cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			if out := invoke(t, "codex", p, env); out != "{}" {
				t.Fatalf("guard stdout=%q", out)
			}
			if rows := records(t, env); len(rows) != 0 {
				t.Fatalf("guard captured %#v", rows)
			}
			_, model, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("codex", p.Session, session.Model)
			if err != nil {
				t.Fatal(err)
			}
			if model != (name == "spare") {
				t.Fatalf("model marker present=%v", model)
			}
		})
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	all := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-C", dir}, args...)
	if out, err := exec.Command("git", all...).CombinedOutput(); err != nil {
		t.Fatalf("fixture git: %v %s", err, out)
	}
}

func TestOffRecordAndResume(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			env, p := setup(t)
			invoke(t, host, p, env)
			p.Prompt = "OFF THE RECORD: this prompt is private"
			checkConfirmation(t, host, invoke(t, host, p, env), false)
			p.Prompt = "private followup"
			if out := invoke(t, host, p, env); out != "" {
				t.Fatalf("off stdout=%q", out)
			}
			p.Event = "Stop"
			reply := "private reply"
			p.Reply = &reply
			invoke(t, host, p, env)
			p.Event = "PostCompact"
			p.Summary = "private compact summary"
			invoke(t, host, p, env)
			p.Event = "PostModelSwitch"
			p.ToModel = "new-model"
			invoke(t, host, p, env)
			p.Event = "UserPromptSubmit"
			p.Prompt = "BACK ON THE RECORD! more private context is not captured"
			checkConfirmation(t, host, invoke(t, host, p, env), true)
			p.Prompt = "public again"
			p.Model = ""
			invoke(t, host, p, env)
			rows := records(t, env)
			var texts []string
			for _, e := range rows {
				texts = append(texts, e.Text)
				if strings.Contains(e.Text, "private") {
					t.Fatal("off-record text leaked")
				}
			}
			if got := strings.Join(texts, "|"); got != "please help|off_record|compact|model_switch|on_record|public again" {
				t.Fatalf("records=%s", got)
			}
			if rows[len(rows)-1].Model != "model-test" {
				t.Fatalf("model fallback=%q", rows[len(rows)-1].Model)
			}
		})
	}
}

// A compaction summary restates the whole context, so after a chat has been
// off the record and come back, its summaries keep only the compact event.
// A resumed or compacted context still holds the off range; only a startup
// or a clear begins a context without it.
func TestCompactionSummaryAfterOffRecordIsNotStored(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			env, p := setup(t)
			invoke(t, host, p, env)
			p.Prompt = "off the record"
			invoke(t, host, p, env)
			p.Prompt = "PRIVATE acquisition plan"
			invoke(t, host, p, env)
			p.Prompt = "back on the record"
			invoke(t, host, p, env)
			compact := func(summary string) {
				q := p
				q.Event, q.Summary = "PostCompact", summary
				invoke(t, host, q, env)
			}
			start := func(source string) {
				q := p
				q.Event, q.Source = "SessionStart", source
				invoke(t, host, q, env)
			}
			compact("Summary: a PRIVATE acquisition plan, then normal work.")
			start("resume")
			start("compact")
			compact("Summary: still the PRIVATE acquisition plan.")
			start("clear")
			compact("Summary: a fresh start.")
			var summaries []string
			for _, e := range records(t, env) {
				if strings.Contains(e.Text, "PRIVATE") {
					t.Fatalf("off-record text leaked: role=%s kind=%s", e.Role, e.Kind)
				}
				if e.Kind == "host_summary" {
					summaries = append(summaries, e.Text)
				}
			}
			rows := records(t, env)
			if strings.Join(summaries, "|") != "Summary: a fresh start." || countRecords(rows, "event", "compact") != 2 {
				t.Fatalf("summaries=%q compact events=%d", summaries, countRecords(rows, "event", "compact"))
			}
		})
	}
}

// Transcript recovery that finds an off-the-record control the hooks missed
// marks the chat as having been off the record too, so after the user comes
// back on the record a compaction summary is still not stored.
func TestCompactionSummaryAfterRecoveredOffRecordIsNotStored(t *testing.T) {
	env, p := setup(t)
	file := registeredTranscript(t, env, &p)
	p.Event = "UserPromptSubmit"
	invoke(t, "claude-code", p, env)
	at := env.Now().Format(time.RFC3339)
	user := func(text, turn string) map[string]any {
		return map[string]any{"type": "user", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": at, "promptId": turn, "message": map[string]any{"role": "user", "content": text}}
	}
	appendRows(t, file, user(p.Prompt, p.Turn), user("off the record", "turn-2"), user("PRIVATE acquisition plan", "turn-3"))
	p.Event, p.Prompt, p.Turn = "UserPromptSubmit", "PRIVATE second thought", "turn-4"
	invoke(t, "claude-code", p, env)
	if _, set, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("claude-code", p.Session, session.WasOff); err != nil || !set {
		t.Fatalf("was_off marker set=%v err=%v", set, err)
	}
	p.Prompt, p.Turn = "back on the record", "turn-5"
	invoke(t, "claude-code", p, env)
	// The transcript's own copy of the summary is host output, which
	// recovery never stores either.
	summary := user("Summary: a PRIVATE acquisition plan.", "")
	summary["isCompactSummary"] = true
	appendRows(t, file, user(p.Prompt, p.Turn), summary)
	p.Event, p.Summary = "PostCompact", "Summary: a PRIVATE acquisition plan."
	invoke(t, "claude-code", p, env)
	p.Event, p.Prompt, p.Turn = "UserPromptSubmit", "public again", "turn-6"
	invoke(t, "claude-code", p, env)
	rows := records(t, env)
	for _, e := range rows {
		if strings.Contains(e.Text, "PRIVATE") {
			t.Fatalf("off-record text leaked: role=%s kind=%s origin=%s", e.Role, e.Kind, e.Origin)
		}
	}
	if countRecords(rows, "event", "compact") != 1 || countRecords(rows, "user", "public again") != 1 {
		t.Fatalf("%d records, compact events=%d", len(rows), countRecords(rows, "event", "compact"))
	}
}

func checkConfirmation(t *testing.T, host, out string, on bool) {
	t.Helper()
	var data struct {
		System   string `json:"systemMessage"`
		Specific struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("confirmation=%q: %v", out, err)
	}
	want := "off the record"
	if on {
		want = "back on the record"
	}
	if data.Specific.Event != "UserPromptSubmit" || !strings.Contains(data.Specific.Context, want) {
		t.Fatalf("confirmation=%q", out)
	}
	if (data.System != "") != (host == "claude-code") {
		t.Fatalf("systemMessage=%q", data.System)
	}
}

func TestControlDetectionAndSyntheticPrompts(t *testing.T) {
	for _, tc := range []struct{ text, want string }{{"off the record, secret", "off_record"}, {"some text\n--- OFF, THE RECORD!!! ---\nsecret", "off_record"}, {"Back on the record.", "on_record"}, {"please go off the record today", ""}, {"off the recorder", ""}, {"my quotation is off the record", ""}} {
		if got := recordControl(tc.text); got != tc.want {
			t.Errorf("control %q=%q want %q", tc.text, got, tc.want)
		}
	}
	for _, tc := range []struct {
		name, agent, source, prompt string
		count                       int
	}{{"agent", "agent-1", "", "off the record", 1}, {"scheduled", "", "scheduled_task", "scheduled private input", 1}, {"loop", "", "", "/loop 1m check", 1}, {"notification", "", "", "<task-notification>report</task-notification>", 1}, {"other-session", "", "other_session", "from another chat", 1}, {"command", "", "", "/compact", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			env, p := setup(t)
			p.Agent = tc.agent
			p.Source = tc.source
			p.Prompt = tc.prompt
			invoke(t, "codex", p, env)
			rows := records(t, env)
			if len(rows) != tc.count {
				t.Fatalf("rows=%#v", rows)
			}
			if len(rows) > 0 && (rows[0].Role != "event" || rows[0].Text != "host_prompt") {
				t.Fatalf("synthetic stored as user: %#v", rows)
			}
			_, off, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("codex", p.Session, session.OffRecord)
			if err != nil || off {
				t.Fatalf("synthetic changed off-record: %v %v", off, err)
			}
		})
	}
}

func TestToolLines(t *testing.T) {
	secret := "sk-" + strings.Repeat("aB4cD5", 8)
	for _, tc := range []struct{ name, host, tool, input, response, failure, want, role string }{
		{"bash", "claude-code", "Bash", `{"command":"go test ./..."}`, `"PRIVATE OUTPUT"`, "", "Bash go test ./... (exit 0)", "tool"},
		{"failure", "claude-code", "Bash", `{"command":"false"}`, `null`, `"Exit code 12\nPRIVATE ERROR"`, "Bash false (exit 12)", "tool"},
		{"codex-exit", "codex", "Bash", `{"command":"false"}`, `{"output":"Wall time 2s\nExit code: 3\nPRIVATE OUTPUT"}`, "", "Bash false (exit 3)", "tool"},
		{"codex-content", "codex", "Bash", `{"cmd":"pwd"}`, `{"content":[{"text":"Exit code: 0\nPRIVATE OUTPUT"}]}`, "", "Bash pwd (exit 0)", "tool"},
		{"unknown-exit", "codex", "Bash", `{"command":"work"}`, `"still running"`, "", "Bash work (exit unknown)", "tool"},
		{"edit", "claude-code", "Edit", `{"file_path":"source.go","old_string":"PRIVATE OLD","new_string":"PRIVATE NEW"}`, `null`, "", "Edit source.go (exit 0)", "tool"},
		{"notebook", "claude-code", "NotebookEdit", `{"notebook_path":"work.ipynb","new_source":"PRIVATE CODE"}`, `null`, "", "NotebookEdit work.ipynb (exit 0)", "tool"},
		{"patch", "codex", "apply_patch", `"*** Begin Patch\n*** Add File: a.go\n+PRIVATE CONTENT\n*** Update File: b.go\n-PRIVATE OLD\n*** Delete File: c.go\n*** End Patch"`, `"Exit code: 0"`, "", "apply_patch a.go, b.go, c.go (exit 0)", "tool"},
		{"plan", "codex", "update_plan", `{"plan":[{"step":"Read code","status":"completed"},{"step":"Run tests","status":"in_progress"}]}`, `null`, "", "update_plan [x] Read code; [~] Run tests (exit unknown)", "plan"},
		{"todo", "claude-code", "TodoWrite", `{"todos":[{"content":"Test it","status":"pending"}]}`, `null`, "", "TodoWrite [ ] Test it (exit 0)", "plan"},
		{"task-create", "claude-code", "TaskCreate", `{"subject":"Add tests","description":"PRIVATE DETAILS"}`, `null`, "", "TaskCreate [?] Add tests (exit 0)", "plan"},
		{"task-update", "claude-code", "TaskUpdate", `{"taskId":"12","status":"completed"}`, `null`, "", "TaskUpdate [x] task 12 (exit 0)", "plan"},
		{"read", "claude-code", "Read", `{"file_path":"/work/notes.md","offset":10,"limit":50}`, `"PRIVATE FILE"`, "", "Read /work/notes.md (exit 0)", "tool"},
		{"grep", "claude-code", "Grep", `{"pattern":"func (main|init)","path":"internal/hook","output_mode":"content"}`, `"PRIVATE MATCHES"`, "", `Grep "func (main|init)" internal/hook (exit 0)`, "tool"},
		{"glob", "claude-code", "Glob", `{"pattern":"**/*.go"}`, `"PRIVATE LIST"`, "", `Glob "**/*.go" (exit 0)`, "tool"},
		{"web-fetch", "claude-code", "WebFetch", `{"url":"https://docs.example.com/guide?page=2","prompt":"PRIVATE QUESTION"}`, `"PRIVATE PAGE"`, "", "WebFetch https://docs.example.com/guide?page=2 (exit 0)", "tool"},
		// What can carry a credential is left out: the user part, the
		// fragment and each query parameter named so.
		{"web-fetch-signed", "claude-code", "WebFetch", `{"url":"https://me:PRIVATE@files.example.com/report.pdf?sig=PRIVATE&page=2&X-Amz-Signature=PRIVATE&code=PRIVATE#access_token=PRIVATE"}`, `"PRIVATE PAGE"`, "", "WebFetch https://files.example.com/report.pdf?page=2 (exit 0)", "tool"},
		// Scalar arguments, shortest first: no object, boolean or value over 1 KiB.
		{"mcp", "claude-code", "mcp__wiki__getPage", `{"cloudId": "c0ffee00-0000-4000-8000-000000000000", "title":"Release  plan","pageId":4242,"body":"` + strings.Repeat("PRIVATE ", 130) + `","withBody":true,"filter":{"PRIVATE":"x"}}`, `{"content":[{"type":"text","text":"PRIVATE PAGE"}]}`, "", "mcp__wiki__getPage pageId=4242 title=Release plan cloudId=c0ffee00-0000-4000-8000-000000000000 (exit 0)", "tool"},
		{"mcp-codex", "codex", "mcp__tracker__get_issue", `{"issue_key":"OPS-12"}`, `"Exit code: 0\nPRIVATE ISSUE"`, "", "mcp__tracker__get_issue issue_key=OPS-12 (exit 0)", "tool"},
		{"mcp-no-args", "claude-code", "mcp__tracker__list", `{}`, `"PRIVATE LIST"`, "", "mcp__tracker__list (exit 0)", "tool"},
		{"mcp-credential-args", "claude-code", "mcp__vault__read", `{"path":"team/app","auth":"PRIVATE","sessionId":"PRIVATE","apiKey":"PRIVATE","X-Cookie":"PRIVATE","pass":"PRIVATE","authentication":"PRIVATE","app_key":"PRIVATE","passcode":"PRIVATE","passkey":"PRIVATE","sessionid":"PRIVATE","PHPSESSID":"PRIVATE","author":"jane"}`, `"PRIVATE VALUE"`, "", "mcp__vault__read author=jane path=team/app (exit 0)", "tool"},
		// A URL argument is cleaned as WebFetch's is.
		{"mcp-url-signed", "claude-code", "mcp__fetch__fetch", `{"url":"https://me:PRIVATE@files.example.com/report.pdf?sig=PRIVATE&page=2&X-Amz-Signature=PRIVATE&code=PRIVATE&sessionid=PRIVATE#access_token=PRIVATE"}`, `"PRIVATE PAGE"`, "", "mcp__fetch__fetch url=https://files.example.com/report.pdf?page=2 (exit 0)", "tool"},
		// One named as an address is cleaned, even with a space in its path.
		{"mcp-url-with-space", "claude-code", "mcp__fetch__fetch", `{"url":"https://me:PRIVATE@files.example.com/my report.pdf?X-Amz-Signature=PRIVATE&page=2"}`, `"PRIVATE PAGE"`, "", "mcp__fetch__fetch url=https://files.example.com/my%20report.pdf?page=2 (exit 0)", "tool"},
		// Text that only starts with an address is kept as it is.
		{"mcp-text-with-url", "claude-code", "mcp__browser__ask", `{"text":"https://example.com/a?b=1 summarize the page"}`, `"PRIVATE ANSWER"`, "", "mcp__browser__ask text=https://example.com/a?b=1 summarize the page (exit 0)", "tool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, p := setup(t)
			p.Event = "PostToolUse"
			if tc.failure != "" {
				p.Event = "PostToolUseFailure"
			}
			p.Tool = tc.tool
			p.ToolInput = json.RawMessage(tc.input)
			p.ToolResponse = json.RawMessage(tc.response)
			if tc.failure != "" {
				p.Error = json.RawMessage(tc.failure)
			}
			invoke(t, tc.host, p, env)
			rows := records(t, env)
			if len(rows) != 1 || rows[0].Text != tc.want || rows[0].Role != tc.role {
				t.Fatalf("records=%#v want=%q", rows, tc.want)
			}
		})
	}
	t.Run("redact-before-truncate", func(t *testing.T) {
		env, p := setup(t)
		p.Event = "PostToolUse"
		p.Tool = "Bash"
		p.Agent = "agent-1"
		input, _ := json.Marshal(map[string]string{"command": strings.Repeat("x", 175) + " token=" + secret + "\nPRIVATE SCRIPT BODY"})
		p.ToolInput = input
		invoke(t, "claude-code", p, env)
		rows := records(t, env)
		if len(rows) != 1 {
			t.Fatal(rows)
		}
		text := rows[0].Text
		if strings.Contains(text, secret[:12]) || strings.Contains(text, "PRIVATE") || !strings.Contains(text, "(subagent)") {
			t.Fatalf("unsafe tool summary=%q", text)
		}
		command := strings.TrimSuffix(strings.TrimPrefix(text, "Bash (subagent) "), " (exit 0)")
		if len([]rune(command)) > 200 {
			t.Fatalf("command head=%d characters", len([]rune(command)))
		}
	})
	// An MCP tool's arguments are redacted before the line is cut at 200
	// characters: the token crosses the cut, and its start alone matches no
	// rule.
	t.Run("mcp-redact-before-cut", func(t *testing.T) {
		env, p := setup(t)
		p.Event = "PostToolUse"
		p.Tool = "mcp__tracker__search"
		token := "ghp_" + strings.Repeat("ab12", 9)
		input, _ := json.Marshal(map[string]any{"query": strings.Repeat("q", 165) + " " + token, "limit": 5})
		p.ToolInput = input
		invoke(t, "claude-code", p, env)
		rows := records(t, env)
		if len(rows) != 1 {
			t.Fatal(rows)
		}
		args := strings.TrimSuffix(strings.TrimPrefix(rows[0].Text, "mcp__tracker__search "), " (exit 0)")
		if strings.Contains(args, token[:12]) || !strings.HasPrefix(args, "limit=5 query=qqq") || len([]rune(args)) > 200 {
			t.Fatalf("unsafe arguments=%q", args)
		}
	})
}

func TestMalformedMissingAndPanicOutput(t *testing.T) {
	env, p := setup(t)
	for _, input := range []string{"garbage", "{}", `{"hook_event_name":"Stop",`} {
		var out bytes.Buffer
		code := Run(context.Background(), "codex", strings.NewReader(input), &out, env)
		if code != 0 {
			t.Fatal(code)
		}
		want := ""
		if strings.Contains(input, "Stop") {
			want = "{}"
		}
		if out.String() != want {
			t.Fatalf("input=%q output=%q", input, out.String())
		}
	}
	if rows := records(t, env); len(rows) != 0 {
		t.Fatal(rows)
	}
	p.Event = "Stop"
	p.Model = ""
	p.Turn = ""
	p.Reply = nil
	if out := invoke(t, "codex", p, env); out != "{}" {
		t.Fatal(out)
	}
	rows := records(t, env)
	if len(rows) != 1 || rows[0].Text != "gap: missing final reply" || rows[0].Model != "" {
		t.Fatalf("missing reply=%#v", rows)
	}
	env.Now = func() time.Time { panic("clock failed") }
	if out := invoke(t, "codex", p, env); out != "{}" {
		t.Fatal(out)
	}
	data, _ := json.Marshal(p)
	if code := Run(context.Background(), "codex", bytes.NewReader(data), panicWriter{}, env); code != 0 {
		t.Fatal(code)
	}
}

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("write failed") }

type blockedReader struct{ unblock <-chan struct{} }

func (b blockedReader) Read([]byte) (int, error) { <-b.unblock; return 0, io.EOF }

// The hook's budget is 300 ms, before the payload names its event too, and
// 1.5 seconds for SessionStart and the tool events. The tests below show Run
// keeps to it, returning at its deadline and not before.
func TestBudget(t *testing.T) {
	for event, want := range map[string]time.Duration{
		"":                   300 * time.Millisecond,
		"UserPromptSubmit":   300 * time.Millisecond,
		"Stop":               300 * time.Millisecond,
		"StopFailure":        300 * time.Millisecond,
		"SessionEnd":         300 * time.Millisecond,
		"PreCompact":         300 * time.Millisecond,
		"PostCompact":        300 * time.Millisecond,
		"PostModelSwitch":    300 * time.Millisecond,
		"Interrupt":          300 * time.Millisecond,
		"SessionStart":       1500 * time.Millisecond,
		"PostToolUse":        1500 * time.Millisecond,
		"PostToolUseFailure": 1500 * time.Millisecond,
	} {
		if got := budget(event); got != want {
			t.Errorf("budget(%q) = %v, want %v", event, got, want)
		}
	}
}

// holdInbox locks the session's inbox file, so no capture can store an event
// until the returned release.
func holdInbox(t *testing.T, env Env, host, id string) func() {
	t.Helper()
	file := (inbox.Store{Root: env.Dirs.Inbox()}).Path("general", host, id)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }
}

// A capture the inbox's lock holds up delivers nothing, and Codex still
// gets its {}. Run waits for it until its deadline, 300 ms from its start,
// and then returns without waiting for the lock; the capture it gave up on
// never stores its event, even once the lock is free. The test ends the
// capture's time itself, at an exact point, once the capture is seen
// waiting for the lock, so no clock decides the outcome, or what the test
// can catch, however slow the machine. A payload that never arrives holds
// Run no longer than its deadline either.
func TestDeadlinesAndNoLateCapture(t *testing.T) {
	env, p := setup(t)
	p.Event = "Stop"
	reply := "must not arrive late"
	p.Reply = &reply
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	release := holdInbox(t, env, "codex", p.Session)
	defer release()
	// Recovery has caught up at once, so only the inbox's lock holds the
	// capture up, and only the end of its time, which the test passes, ends
	// it.
	held := env
	held.Slack = 0
	held.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) { return 0, nil }
	waits := make(chan heldWait, 3)
	held.withDeadline = holdWaits(waits)
	var out bytes.Buffer
	before := time.Now()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		Run(context.Background(), "codex", bytes.NewReader(data), &out, held)
	}()
	read := nextWait(t, waits, "its wait for the payload")
	work := nextWait(t, waits, "its wait for the capture")
	asked := time.Now()
	// Run takes its start between before and asked. A Stop keeps the 300 ms
	// every payload gets, and Run ends the capture's time 10 ms before it,
	// keeping those for its output.
	if !work.deadline.Equal(read.deadline) {
		t.Fatalf("a Stop moved the deadline by %v", work.deadline.Sub(read.deadline))
	}
	if deadline := work.deadline.Add(10 * time.Millisecond); deadline.Before(before.Add(budget("Stop"))) || deadline.After(asked.Add(budget("Stop"))) {
		t.Fatalf("a Stop's deadline is %v after the test started it, want %v after Run's start", deadline.Sub(before), budget("Stop"))
	}
	early := "a Stop held up by the inbox's lock returned before its deadline"
	awaitParked(t, returned, inboxWait, "/internal/hook.capture(", "the capture was not seen waiting for the inbox's lock", early)
	awaitParked(t, returned, runWait, "/hook."+t.Name()+".", "Run was not seen waiting for the capture", early)
	work.end()
	mustReturn(t, "a Stop held up by the inbox's lock, past its deadline,", func() { <-returned })
	if out.String() != "{}" {
		t.Fatalf("a Stop past its deadline wrote %q, want {}", out.String())
	}
	release()
	// A second invocation waits for the session lock, which the capture Run
	// gave up on holds until it fails. Only its reply may appear once the
	// inbox's lock is free.
	reply = "new reply"
	invoke(t, "codex", p, env)
	rows := records(t, env)
	if len(rows) != 1 || rows[0].Text != "new reply" {
		t.Fatalf("late capture=%#v", rows)
	}
	// The start is taken before Run computes its deadline, on a real clock,
	// and Run ends a read's time 10 ms before it, keeping those for its
	// output: it can't return sooner.
	never := env
	never.Slack = 0
	unblock := make(chan struct{})
	defer close(unblock)
	started := time.Now()
	mustReturn(t, "a hook whose payload never arrives", func() {
		Run(context.Background(), "codex", blockedReader{unblock}, io.Discard, never)
	})
	if elapsed := time.Since(started); elapsed < budget("")-10*time.Millisecond {
		t.Fatalf("a hook whose payload never arrives returned after %v, before its deadline", elapsed)
	}
}

// heldWait is one of Run's waits, for the payload, the capture or the
// output, which ends only when the test ends it.
type heldWait struct {
	deadline time.Time          // when a real timer would have ended it
	ctx      context.Context    // the wait's context
	end      context.CancelFunc // ends the wait
}

// holdWaits replaces Run's real timers: it hands the test each wait Run
// asks for, in the order asked, on waits, which has room for them all.
func holdWaits(waits chan<- heldWait) func(context.Context, time.Time) (context.Context, context.CancelFunc) {
	return func(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
		ctx, end := context.WithCancel(parent)
		waits <- heldWait{deadline, ctx, end}
		return ctx, end
	}
}

// nextWait returns the next wait Run asks for, and fails the test, naming
// the wait by what, if Run has not asked for it after hangGuard.
func nextWait(t *testing.T, waits <-chan heldWait, what string) heldWait {
	t.Helper()
	select {
	case w := <-waits:
		return w
	case <-time.After(hangGuard):
	}
	t.Fatalf("Run had not asked for %s after %v", what, hangGuard)
	return heldWait{}
}

// The functions whose waits a test sees in the goroutines' stacks: Run's
// for the payload and the capture, writeOutput's for the output, and the
// inbox's and the session's between tries of a busy lock.
const (
	runWait     = "github.com/heyNag/eva-md/internal/hook.Run"
	outputWait  = "github.com/heyNag/eva-md/internal/hook.writeOutput"
	inboxWait   = "github.com/heyNag/eva-md/internal/inbox.lock"
	sessionWait = "github.com/heyNag/eva-md/internal/session.Lock"
)

// awaitParked returns once a goroutine whose stack holds within is parked
// in a select in fn itself, as the goroutines' stacks show. A goroutine
// parks in a select only when none of its cases is ready, so whatever would
// end that wait has not happened. It fails the test with early if returned
// closes first, and with what, naming the hang, if hangGuard passes first.
// The pause between looks only paces them.
func awaitParked(t *testing.T, returned <-chan struct{}, fn, within, what, early string) {
	t.Helper()
	guard := time.After(hangGuard)
	for !parkedIn(fn, within) {
		select {
		case <-returned:
			t.Fatal(early)
		case <-guard:
			t.Fatalf("%s in %v: it hangs, or waits somewhere else", what, hangGuard)
		case <-time.After(time.Millisecond):
		}
	}
}

// parkedIn reports whether a goroutine whose stack holds within is parked
// in a select in fn itself: fn is its innermost frame outside the runtime,
// whose own frames a dump may or may not show.
func parkedIn(fn, within string) bool {
	for _, g := range strings.Split(stacks(), "\n\n") {
		header, frames, _ := strings.Cut(g, "\n")
		if !strings.Contains(header, " [select") || !strings.Contains(frames, within) {
			continue
		}
		for _, line := range strings.Split(frames, "\n") {
			if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "runtime.") {
				continue
			}
			if strings.HasPrefix(line, fn+"(") {
				return true
			}
			break
		}
	}
	return false
}

// captureFrame is how capture's own frame begins in a goroutine's stack.
const captureFrame = "github.com/heyNag/eva-md/internal/hook.capture("

// awaitNoCapture returns once no goroutine's stack holds capture, so every
// write a capture Run gave up on could make has been made, the heartbeat it
// writes after letting go of the session's lock included. It fails the test
// with what, naming the hang, if hangGuard passes first. The pause between
// looks only paces them.
func awaitNoCapture(t *testing.T, what string) {
	t.Helper()
	guard := time.After(hangGuard)
	for strings.Contains(stacks(), captureFrame) {
		select {
		case <-guard:
			t.Fatalf("%s had not returned after %v", what, hangGuard)
		case <-time.After(time.Millisecond):
		}
	}
}

// stacks returns every goroutine's stack, as runtime.Stack dumps them.
func stacks() string {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, 2*len(buf))
	}
}

// SessionStart, whose brief gets longer, has 1.5 seconds: once the payload
// names it, Run moves its deadline from the 300 ms it gives every payload
// to 1.5 seconds from its start. Held up by the session's lock, it returns
// at that deadline, not at 300 ms, and without waiting for the lock, and it
// stores nothing. Held up past the 300 ms instead, then let through, it
// still delivers its brief and stores its event. The test passes each
// deadline itself, at an exact point, so no clock decides the outcome, or
// what the test can catch, however slow the machine. Malformed marker state
// fails closed instead of treating the chat as on-record.
func TestSessionStartBudgetAndMarkerErrors(t *testing.T) {
	env, p := setup(t)
	p.Event = "SessionStart"
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	release, err := sessionLock(context.Background(), env.Dirs.Locks(), "codex", p.Session)
	if err != nil {
		t.Fatal(err)
	}
	held := env
	held.Slack = 0
	waits := make(chan heldWait, 3)
	held.withDeadline = holdWaits(waits)
	before := time.Now()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		Run(context.Background(), "codex", bytes.NewReader(data), io.Discard, held)
	}()
	read := nextWait(t, waits, "its wait for the payload")
	work := nextWait(t, waits, "its wait for the capture")
	asked := time.Now()
	// Run takes its start between before and asked, and ends each wait 10 ms
	// before its deadline, keeping those for its output.
	if got, want := work.deadline.Sub(read.deadline), budget("SessionStart")-budget(""); got != want {
		t.Fatalf("SessionStart moved the deadline %v past the 300 ms one, want %v", got, want)
	}
	if deadline := work.deadline.Add(10 * time.Millisecond); deadline.Before(before.Add(budget("SessionStart"))) || deadline.After(asked.Add(budget("SessionStart"))) {
		t.Fatalf("a SessionStart's deadline is %v after the test started it, want %v after Run's start", deadline.Sub(before), budget("SessionStart"))
	}
	// The 300 ms deadline passes while the capture waits for the lock, as it
	// would on a real clock: the capture's time goes on, and Run waits. The
	// capture's own call names it among session.Lock's waits, as the
	// start-of-chat catch-up takes other chats' locks.
	awaitParked(t, returned, sessionWait, "/internal/hook.sessionLock(", "the capture was not seen waiting for the session's lock",
		"a SessionStart held up by the session's lock returned before its deadline")
	read.end()
	if work.ctx.Err() != nil {
		t.Fatal("the 300 ms deadline ended a SessionStart's capture")
	}
	awaitParked(t, returned, runWait, "/hook."+t.Name()+".", "Run was not seen waiting for a SessionStart's capture",
		"a SessionStart held up by the session's lock returned once the 300 ms deadline passed, before its own")
	work.end()
	mustReturn(t, "a SessionStart held up by the session's lock, past its deadline,", func() { <-returned })
	release()
	if rows := records(t, env); len(rows) != 0 {
		t.Fatalf("a SessionStart past its deadline stored %#v", rows)
	}
	// The 300 ms deadline passes as Run starts a capture the session's lock
	// holds up, before Run waits for it, as it can on a busy machine. Let
	// through once Run waits for it, the capture still delivers its brief
	// and stores its event. Its own limits keep busySlack.
	release, err = sessionLock(context.Background(), env.Dirs.Locks(), "codex", p.Session)
	if err != nil {
		t.Fatal(err)
	}
	slow := env
	waits = make(chan heldWait, 3)
	hold := holdWaits(waits)
	asks := 0
	var endPayload context.CancelFunc
	slow.withDeadline = func(parent context.Context, d time.Time) (context.Context, context.CancelFunc) {
		ctx, end := hold(parent, d)
		asks++
		switch asks {
		case 1:
			endPayload = end
		case 2:
			// Run asks for the capture's wait: the payload's ends first.
			endPayload()
		}
		return ctx, end
	}
	var out bytes.Buffer
	returned = make(chan struct{})
	go func() {
		defer close(returned)
		Run(context.Background(), "codex", bytes.NewReader(data), &out, slow)
	}()
	nextWait(t, waits, "its wait for the payload")
	nextWait(t, waits, "its wait for the capture")
	awaitParked(t, returned, runWait, "/hook."+t.Name()+".", "Run was not seen waiting for a SessionStart's capture",
		"a SessionStart returned while the session's lock held its capture up: the 300 ms deadline, already past, cut it short")
	release()
	mustReturn(t, "a SessionStart whose capture outlasted the 300 ms deadline", func() { <-returned })
	if out.Len() == 0 {
		t.Fatal("a SessionStart whose capture outlasted the 300 ms deadline printed nothing")
	}
	if context := contextFrom(t, out.String()); !strings.HasPrefix(context, "No earlier chats here;") {
		t.Fatalf("a SessionStart whose capture outlasted the 300 ms deadline printed %q", out.String())
	}
	if rows := records(t, env); len(rows) != 1 || rows[0].Text != "session_start" {
		t.Fatalf("a SessionStart whose capture outlasted the 300 ms deadline stored %#v", rows)
	}
	// Malformed marker state fails closed instead of treating the chat as on-record.
	marker := filepath.Join(env.Dirs.Sessions(), "codex", p.Session, session.OffRecord)
	if err := os.Symlink(filepath.Join(os.Getenv("HOME"), "missing"), marker); err != nil {
		t.Fatal(err)
	}
	p.Event = "UserPromptSubmit"
	p.Prompt = "must not save"
	invoke(t, "codex", p, env)
	if rows := records(t, env); len(rows) != 1 {
		t.Fatalf("marker error captured=%#v", rows)
	}
}

func TestModelAndMetadataSafety(t *testing.T) {
	env, p := setup(t)
	secret := "ghp_" + strings.Repeat("ab12", 10)
	p.Model = "token=" + secret
	p.Agent = "token=" + secret
	invoke(t, "codex", p, env)
	if err := filepath.WalkDir(env.Dirs.State, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), secret) {
				t.Errorf("secret in %s", path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p.Session = secret
	invoke(t, "codex", p, env)
	if rows := records(t, env); len(rows) != 1 {
		t.Fatalf("unsafe session captured=%#v", rows)
	}
}

func TestBranchThreadSurvivesDetachedHEAD(t *testing.T) {
	env, p := setup(t)
	git(t, p.Cwd, "init", "--quiet")
	git(t, p.Cwd, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "fixture")
	git(t, p.Cwd, "checkout", "--quiet", "-b", "feature/capture")
	invoke(t, "codex", p, env)
	git(t, p.Cwd, "checkout", "--quiet", "--detach", "HEAD")
	p.Turn = "turn-2"
	p.Prompt = "detached work"
	invoke(t, "codex", p, env)
	rows := records(t, env)
	if len(rows) != 2 || rows[0].Branch != "feature/capture" || rows[1].Branch != "feature/capture" {
		t.Fatalf("branch history=%#v", rows)
	}
	thread, ok, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("codex", p.Session, session.Thread)
	if err != nil || !ok || thread != "feature/capture" {
		t.Fatalf("thread=%q %v %v", thread, ok, err)
	}
}

// A capture whose callback is still running when the hook's time is up
// writes nothing later. The callback ends that time at an exact point, by
// cancelling the hook's context, and holds the capture up until the test
// lets it go; Run still gives Codex its {}.
func TestCancelledCallbacksCannotWriteLater(t *testing.T) {
	for _, callback := range []string{"getenv", "clock"} {
		t.Run(callback, func(t *testing.T) {
			env, p := setup(t)
			p.Event = "Stop"
			reply := "expired capture"
			p.Reply = &reply
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			unblock := make(chan struct{})
			returned := make(chan struct{})
			// Only the first call holds the capture up; a later one, as the
			// failed run's heartbeat makes, returns at once.
			var once sync.Once
			hold := func() { once.Do(func() { cancel(); <-unblock; close(returned) }) }
			if callback == "getenv" {
				env.Getenv = func(key string) string {
					if key == "EVA_MD_INNER" {
						hold()
					}
					return os.Getenv(key)
				}
			} else {
				env.Now = func() time.Time { hold(); return time.Now() }
			}
			if out := invokeContext(t, ctx, "codex", p, env); out != "{}" {
				t.Fatal(out)
			}
			close(unblock)
			<-returned
			// Run another event to wait for the same session lock and flush any
			// still-unwinding worker; the canceled payload must never appear.
			env.Getenv = os.Getenv
			env.Now = time.Now
			p.Reply = nil
			invoke(t, "codex", p, env)
			rows := records(t, env)
			if len(rows) != 1 || rows[0].Text != "gap: missing final reply" {
				t.Fatalf("late callback capture=%#v", rows)
			}
		})
	}
}

func TestParallelFreshHooks(t *testing.T) {
	env, base := setup(t)
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := base
			p.Event = "Stop"
			reply := fmt.Sprintf("parallel reply %d", i)
			p.Reply = &reply
			p.Turn = fmt.Sprintf("turn-%d", i)
			data, _ := json.Marshal(p)
			var out bytes.Buffer
			code := Run(context.Background(), "codex", bytes.NewReader(data), &out, env)
			if code != 0 || out.String() != "{}" {
				errs <- fmt.Sprintf("code=%d stdout=%q", code, out.String())
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	rows := records(t, env)
	if len(rows) != 8 {
		t.Fatalf("parallel hook records=%d", len(rows))
	}
}

type blockedWriter struct {
	started chan struct{}
	unblock chan struct{}
	written chan string
}

func (w blockedWriter) Write(data []byte) (int, error) {
	close(w.started)
	<-w.unblock
	w.written <- string(data)
	return len(data), nil
}

// callerDeadline is a caller's context whose deadline never passes by
// itself, for a test that passes Run's deadlines itself.
type callerDeadline struct {
	context.Context
	at time.Time
}

func (c callerDeadline) Deadline() (time.Time, bool) { return c.at, true }

// A caller's deadline earlier than the hook's own wins; a later one, or
// none, leaves the hook's own.
func TestEarlierCallerDeadlineWins(t *testing.T) {
	own := time.Now().Add(budget("Stop") + busySlack)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want time.Time
	}{
		{"none", context.Background(), own},
		{"earlier", callerDeadline{context.Background(), own.Add(-busySlack)}, own.Add(-busySlack)},
		{"later", callerDeadline{context.Background(), own.Add(time.Millisecond)}, own},
	} {
		if got := invocationDeadline(tc.ctx, own); !got.Equal(tc.want) {
			t.Errorf("%s caller's deadline: the hook's is %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A blocked output destination holds Run no longer than its caller's
// deadline, which wins over the hook's own budget, here lengthened by
// busySlack. The capture returns at once, as in a headless child's hook,
// and leaves nothing behind under HOME. Run starts the write and waits for
// it until that deadline, which the test passes itself, at an exact point,
// once Run is seen waiting for it, so no clock decides the outcome, or what
// the test can catch, however slow the machine.
func TestBlockedOutputHonorsDeadline(t *testing.T) {
	env, p := setup(t)
	t.Setenv("EVA_MD_INNER", "1")
	p.Event = "Stop"
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	waits := make(chan heldWait, 3)
	env.withDeadline = holdWaits(waits)
	// The caller's deadline is as close as the hook's own budget without
	// busySlack.
	ctx := callerDeadline{context.Background(), time.Now().Add(budget("Stop"))}
	writer := blockedWriter{started: make(chan struct{}), unblock: make(chan struct{}), written: make(chan string, 1)}
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		if code := Run(ctx, "codex", bytes.NewReader(data), writer, env); code != 0 {
			t.Errorf("hook exit=%d", code)
		}
	}()
	nextWait(t, waits, "its wait for the payload")
	nextWait(t, waits, "its wait for the capture")
	output := nextWait(t, waits, "its wait for its output")
	if !output.deadline.Equal(ctx.at) {
		t.Fatalf("Run waits for its output until %v, not until its caller's deadline, %v", output.deadline, ctx.at)
	}
	select {
	case <-writer.started:
	case <-time.After(hangGuard):
		t.Fatalf("the output writer was not invoked in %v", hangGuard)
	}
	// Run waits for the blocked write until its deadline, which has not
	// passed: it waits in writeOutput for the write or the deadline.
	awaitParked(t, returned, outputWait, "/hook."+t.Name()+".", "Run was not seen waiting for its blocked output or its deadline",
		"a hook whose output is blocked returned before its deadline")
	output.end()
	mustReturn(t, "a hook whose output is blocked, past its deadline,", func() { <-returned })
	// A permanently stalled output destination cannot promise delivery; this
	// one takes the output once released, after Run has returned.
	close(writer.unblock)
	select {
	case got := <-writer.written:
		if got != "{}" {
			t.Errorf("eventual stdout=%q", got)
		}
	case <-time.After(hangGuard):
		t.Errorf("the writer had not returned %v after its release", hangGuard)
	}
}

func TestUnchangedMarkersAreNotRewritten(t *testing.T) {
	env, p := setup(t)
	invoke(t, "codex", p, env)
	oldTime := time.Unix(1234567890, 0)
	markerDir := filepath.Join(env.Dirs.Sessions(), "codex", p.Session)
	for _, name := range []string{session.Model, session.Thread} {
		if err := os.Chtimes(filepath.Join(markerDir, name), oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}
	p.Prompt = "next prompt"
	p.Turn = "turn-2"
	invoke(t, "codex", p, env)
	for _, name := range []string{session.Model, session.Thread} {
		info, err := os.Stat(filepath.Join(markerDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(oldTime) {
			t.Errorf("unchanged %s marker was rewritten", name)
		}
	}
	p.Event = "PostModelSwitch"
	p.ToModel = "changed-model"
	invoke(t, "codex", p, env)
	model, exists, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("codex", p.Session, session.Model)
	if err != nil || !exists || model != "changed-model" {
		t.Fatalf("changed model=%q exists=%v err=%v", model, exists, err)
	}
}
