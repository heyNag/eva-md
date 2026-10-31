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
	// Nor do prompts get the memory reminder.
	c := config.Default()
	c.ReminderEvery = 0
	env := Env{Dirs: dirs, Config: c, Getenv: os.Getenv, Now: func() time.Time { return time.Date(2026, 10, 29, 15, 4, 5, 0, time.FixedZone("ICT", 7*3600)) }, spawn: func(...string) {}}
	return env, payload{Session: "test-session", Cwd: filepath.Join(home, "project"), Model: "model-test", Event: "UserPromptSubmit", Prompt: "please help", Turn: "turn-1"}
}

// busySlack is how much tests that check what a hook stores, not how long
// it takes, lengthen its budget, so a busy machine can't fail them.
const busySlack = 10 * time.Second

func invoke(t *testing.T, host string, p payload, env Env) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Run(context.Background(), host, bytes.NewReader(data), &out, env); code != 0 {
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
				env.slack = busySlack
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

func TestDeadlinesAndNoLateCapture(t *testing.T) {
	env, p := setup(t)
	p.Event = "Stop"
	reply := "must not arrive late"
	p.Reply = &reply
	store := inbox.Store{Root: env.Dirs.Inbox()}
	file := store.Path("general", "codex", p.Session)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	out := invoke(t, "codex", p, env)
	elapsed := time.Since(started)
	if out != "{}" || elapsed > 650*time.Millisecond || elapsed < 200*time.Millisecond {
		t.Fatalf("deadline output=%q elapsed=%v", out, elapsed)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	// A second invocation serializes with any finishing worker. Only its reply
	// may appear after the held inbox lock is released.
	reply = "new reply"
	p.Reply = &reply
	invoke(t, "codex", p, env)
	rows := records(t, env)
	if len(rows) != 1 || rows[0].Text != "new reply" {
		t.Fatalf("late capture=%#v", rows)
	}
	unblock := make(chan struct{})
	defer close(unblock)
	started = time.Now()
	Run(context.Background(), "codex", blockedReader{unblock}, io.Discard, env)
	if time.Since(started) > 600*time.Millisecond {
		t.Fatal("input read ignored deadline")
	}
}

func TestSessionStartBudgetAndMarkerErrors(t *testing.T) {
	env, p := setup(t)
	p.Event = "SessionStart"
	release, err := sessionLock(context.Background(), env.Dirs.Locks(), "codex", p.Session)
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(400 * time.Millisecond); release() }()
	started := time.Now()
	invoke(t, "codex", p, env)
	if elapsed := time.Since(started); elapsed < 350*time.Millisecond || elapsed > 1400*time.Millisecond {
		t.Fatalf("SessionStart budget=%v", elapsed)
	}
	if rows := records(t, env); len(rows) != 1 || rows[0].Text != "session_start" {
		t.Fatal(rows)
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

func TestCancelledCallbacksCannotWriteLater(t *testing.T) {
	for _, callback := range []string{"getenv", "clock"} {
		t.Run(callback, func(t *testing.T) {
			env, p := setup(t)
			p.Event = "Stop"
			reply := "expired capture"
			p.Reply = &reply
			unblock := make(chan struct{})
			returned := make(chan struct{})
			if callback == "getenv" {
				env.Getenv = func(key string) string {
					if key == "EVA_MD_INNER" {
						<-unblock
						close(returned)
					}
					return os.Getenv(key)
				}
			} else {
				env.Now = func() time.Time { <-unblock; close(returned); return time.Now() }
			}
			if out := invoke(t, "codex", p, env); out != "{}" {
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
	env.slack = busySlack
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

func TestBlockedOutputHonorsDeadline(t *testing.T) {
	for _, budget := range []time.Duration{300 * time.Millisecond, 100 * time.Millisecond} {
		t.Run(budget.String(), func(t *testing.T) {
			env, p := setup(t)
			// The caller's deadline must win over the hook's own budget, here
			// made far longer, so the upper bound can allow for a busy machine.
			env.slack = busySlack
			p.Event = "Stop"
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			writer := blockedWriter{started: make(chan struct{}), unblock: make(chan struct{}), written: make(chan string, 1)}
			defer close(writer.unblock)
			// The capture Run gave up on can still write under HOME after Run
			// returns. Wait for it before the test's folders are removed.
			running := runtime.NumGoroutine()
			t.Cleanup(func() {
				for end := time.Now().Add(2 * time.Second); runtime.NumGoroutine() > running && time.Now().Before(end); {
					time.Sleep(5 * time.Millisecond)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			started := time.Now()
			if code := Run(ctx, "codex", bytes.NewReader(data), writer, env); code != 0 {
				t.Fatal(code)
			}
			elapsed := time.Since(started)
			if elapsed < budget-40*time.Millisecond || elapsed > budget+500*time.Millisecond {
				t.Fatalf("blocked output elapsed=%v budget=%v", elapsed, budget)
			}
			// Run starts the write before it waits, but a busy machine may run
			// the writer only after Run has returned.
			select {
			case <-writer.started:
			case <-time.After(time.Second):
				t.Fatal("output writer was not invoked")
			}
			// Release after Run returned and wait for the writer at test cleanup;
			// a permanently stalled output destination cannot promise delivery.
			t.Cleanup(func() {
				select {
				case got := <-writer.written:
					if got != "{}" {
						t.Errorf("eventual stdout=%q", got)
					}
				case <-time.After(time.Second):
					t.Error("writer did not stop after release")
				}
			})
		})
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
