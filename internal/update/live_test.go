package update

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
)

// liveAnswer is an answer to the live fixture that the validator accepts.
const liveAnswer = `{"summary":"The build script has a verbose flag, off by default; a test for it comes next.",` +
	`"last_chat":"Added a verbose flag to the build script.",` +
	`"done":[{"text":"Added the verbose flag to the build script","refs":[2]}],` +
	`"items":[{"id":null,"kind":"next","action":"add","text":"Add a test that runs the build with the flag and checks the step names","tag":"stated","refs":[3],"reason":null,"replacement":null}],` +
	`"timeline":[{"kind":"decision","text":"Keep the verbose flag off by default","tag":"stated","refs":[3]}],"unknowns":[]}`

// claudeEnvelope is claude's success envelope around a structured answer.
func claudeEnvelope(answer string) string {
	return `{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":120,"output_tokens":40},"permission_denials":[],"structured_output":` + answer + `}`
}

// doctor --live runs the update's exact command once, against the fake
// CLIs here: the host's default model at the configured effort, the batch
// on stdin and no argument, in an empty folder. A valid answer passes; each
// failure class comes back as the runner names it, and an answer the
// validator refuses is invalid_output. Nothing is written but the fake's
// own dump: no wiki, no run log, no watermark.
func TestLiveCheck(t *testing.T) {
	for _, tc := range []struct {
		name, host     string
		stdout, stderr string
		output         string // codex's -o answer
		exit           string
		path           string // PATH, when not the fake CLIs'
		reason         string
	}{
		{name: "claude passes", host: HostClaude, stdout: claudeEnvelope(liveAnswer)},
		{name: "codex passes", host: HostCodex, stdout: codexClean, output: liveAnswer},
		{name: "auth", host: HostClaude, stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login"}`, exit: "1", reason: ReasonAuth},
		{name: "rate limit", host: HostClaude, stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1767225600"}`, exit: "1", reason: ReasonRateLimit},
		{name: "host error", host: HostClaude, stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":503,"result":"API Error: request failed"}`, exit: "1", reason: ReasonHostError},
		{name: "model rejected", host: HostClaude, stdout: claudeRejected, exit: "1", reason: ReasonModelRejected},
		{name: "tool used", host: HostClaude, stdout: `{"type":"result","subtype":"success","is_error":false,"permission_denials":[{"tool_name":"Bash"}],"structured_output":` + liveAnswer + `}`, reason: ReasonToolUsed},
		{name: "invalid output", host: HostClaude, stdout: claudeEnvelope(`{"summary":"done"}`), reason: ReasonInvalidOutput},
		{name: "no structured output", host: HostClaude, stdout: `{"type":"result","subtype":"success","is_error":false,"result":"done"}`, reason: ReasonInvalidOutput},
		{name: "mcp failed", host: HostCodex, stderr: "Error: required MCP servers failed to initialize: probe\n", exit: "1", reason: ReasonMCPFailed},
		{name: "cli missing", host: HostCodex, path: "/usr/bin:/bin", reason: ReasonCLIMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			if tc.path != "" {
				t.Setenv("PATH", filepath.Join(t.TempDir(), "empty")+":"+tc.path)
			}
			dump := fakeCLI(t, "")
			for name, content := range map[string]string{"stdout": tc.stdout, "stderr": tc.stderr, "output": tc.output, "exit": tc.exit} {
				if content != "" {
					put(t, dump, name, content)
				}
			}
			c := config.Default()
			c.Effort = map[string]string{tc.host: "high"}
			res, err := Live(context.Background(), NewRunner(tc.host), tc.host, c)
			if err != nil || res.Reason != tc.reason || res.Host != tc.host || res.Asked != "high" {
				t.Fatalf("result %+v %v", res, err)
			}
			if tc.reason == "" && (res.Effort != "high" || res.Duration <= 0) {
				t.Fatalf("a pass reports %+v", res)
			}
			if tc.reason == ReasonCLIMissing {
				if runs(t, dump) != 0 {
					t.Fatal("a missing CLI ran")
				}
				return
			}
			args := argsOf(t, dump, 1)
			stdin := read(t, dump, "stdin.1")
			if slices.Contains(args, "--model") || slices.Contains(args, "-m") || !strings.Contains(strings.Join(args, " "), "high") ||
				!strings.Contains(stdin, "[1] user 2026-01-15") || !strings.Contains(stdin, "--verbose flag") || strings.Contains(strings.Join(args, " "), "--verbose flag") {
				t.Fatalf("arguments %q, stdin %q", args, stdin)
			}
			for _, dir := range []string{".local/state", ".config"} {
				if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), dir)); !os.IsNotExist(err) {
					t.Fatalf("the live check wrote %s: %v", dir, err)
				}
			}
		})
	}
}

// The fixture is a group the update would process: two exchanges past 600
// characters, numbered 1 to 4, for an empty page.
func TestLiveFixture(t *testing.T) {
	b, page, err := liveFixture(HostCodex)
	if err != nil || len(b.Entries) != 4 || b.More || b.Host != HostCodex || len(heldItems(page)) != 0 {
		t.Fatalf("fixture %+v %v", b, err)
	}
	if _, _, err := Validate([]byte(liveAnswer), b, page); err != nil {
		t.Fatalf("the canned answer: %v", err)
	}
}
