package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestChildEnvironmentIsAnAllowlist(t *testing.T) {
	isolate(t)
	got := childEnv([]string{
		"HOME=/h", "PATH=/bin", "USER=u", "LOGNAME=u", "SHELL=/bin/zsh", "LANG=en_GB.UTF-8",
		"LC_ALL=C", "LC_CTYPE=UTF-8", "TMPDIR=/t", "CODEX_HOME=/c", "CLAUDE_CONFIG_DIR=/cc",
		"SSL_CERT_FILE=/s", "SSL_CERT_DIR=/sd", "NODE_EXTRA_CA_CERTS=/n", "CODEX_CA_CERTIFICATE=/x",
		"HTTP_PROXY=hp", "http_proxy=hp", "HTTPS_PROXY=sp", "https_proxy=sp", "ALL_PROXY=ap", "all_proxy=ap", "NO_PROXY=np", "no_proxy=np",
		"ANTHROPIC_API_KEY=fake-anthropic-value", "OPENAI_API_KEY=fake-openai-value",
		"CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=0",
		"TERM=xterm-256color", "EVA_MD_INNER=0", "FTP_PROXY=fp", "NODE_OPTIONS=--require=/x.js", "GIT_DIR=/g", "XLC_FAKE=1", "malformed",
	})
	want := []string{
		"HOME=/h", "PATH=/bin", "USER=u", "LOGNAME=u", "SHELL=/bin/zsh", "LANG=en_GB.UTF-8",
		"LC_ALL=C", "LC_CTYPE=UTF-8", "TMPDIR=/t", "CODEX_HOME=/c", "CLAUDE_CONFIG_DIR=/cc",
		"SSL_CERT_FILE=/s", "SSL_CERT_DIR=/sd", "NODE_EXTRA_CA_CERTS=/n", "CODEX_CA_CERTIFICATE=/x",
		"HTTP_PROXY=hp", "http_proxy=hp", "HTTPS_PROXY=sp", "https_proxy=sp", "ALL_PROXY=ap", "all_proxy=ap", "NO_PROXY=np", "no_proxy=np",
		"TERM=dumb", "EVA_MD_INNER=1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("environment\n got %q\nwant %q", got, want)
	}
}

func TestChildGetsOnlyTheClearedEnvironment(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			isolate(t)
			dump := fakeHost(t, host)
			codexHome := filepath.Join(os.Getenv("HOME"), ".codex-test")
			claudeConfig := filepath.Join(os.Getenv("HOME"), ".claude-test")
			set := map[string]string{
				"ANTHROPIC_API_KEY": "fake-anthropic-value", "OPENAI_API_KEY": "fake-openai-value",
				"CLAUDECODE": "1", "CLAUDE_CODE_ENTRYPOINT": "cli", "CLAUDE_CODE_SSE_PORT": "1234",
				"CLAUDE_CODE_DISABLE_AUTO_MEMORY": "0", "EVA_MD_INNER": "0", "TERM": "xterm-256color",
				"HTTPS_PROXY": "http://proxy.example.invalid:8080", "https_proxy": "http://proxy.example.invalid:8080",
				"ALL_PROXY": "socks5://proxy.example.invalid:1080", "all_proxy": "socks5://proxy.example.invalid:1080",
				"NO_PROXY": "localhost,.example.invalid", "CODEX_HOME": codexHome, "CLAUDE_CONFIG_DIR": claudeConfig,
				"SSL_CERT_FILE": "/etc/ssl/cert.pem", "SSL_CERT_DIR": "/etc/ssl/certs", "NODE_EXTRA_CA_CERTS": "/etc/ssl/work-ca.pem",
				"CODEX_CA_CERTIFICATE": "/etc/ssl/work-ca.pem", "LC_CTYPE": "UTF-8", "UNRELATED_SETTING": "dropped",
			}
			for k, v := range set {
				t.Setenv(k, v)
			}
			res, err := NewRunner(host).Run(context.Background(), request(t, host))
			if err != nil || res.Reason != "" {
				t.Fatalf("run failed: %+v %v", res, err)
			}
			env := envOf(t, dump, 1)
			for _, key := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "CLAUDECODE", "UNRELATED_SETTING", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM"} {
				if _, ok := env[key]; ok {
					t.Errorf("%s reached the child", key)
				}
			}
			for key := range env {
				if strings.HasPrefix(key, "CLAUDE_CODE_") && key != "CLAUDE_CODE_DISABLE_AUTO_MEMORY" {
					t.Errorf("%s reached the child", key)
				}
			}
			want := map[string]string{
				"HTTPS_PROXY": set["HTTPS_PROXY"], "https_proxy": set["https_proxy"], "NO_PROXY": set["NO_PROXY"],
				"ALL_PROXY": set["ALL_PROXY"], "all_proxy": set["all_proxy"],
				"SSL_CERT_DIR": set["SSL_CERT_DIR"], "NODE_EXTRA_CA_CERTS": set["NODE_EXTRA_CA_CERTS"], "CODEX_CA_CERTIFICATE": set["CODEX_CA_CERTIFICATE"],
				"CODEX_HOME": codexHome, "CLAUDE_CONFIG_DIR": claudeConfig, "SSL_CERT_FILE": set["SSL_CERT_FILE"], "LC_CTYPE": "UTF-8",
				"HOME": os.Getenv("HOME"), "PATH": os.Getenv("PATH"), "TMPDIR": os.Getenv("TMPDIR"),
				"TERM": "dumb", "EVA_MD_INNER": "1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1",
			}
			for key, value := range want {
				if env[key] != value {
					t.Errorf("%s = %q, want %q", key, env[key], value)
				}
			}
		})
	}
}

func TestInputGoesOnStdinOnly(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			isolate(t)
			dump := fakeHost(t, host)
			r := request(t, host)
			if _, err := NewRunner(host).Run(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			want := string(r.Input)
			if host == HostCodex {
				// Codex has no flag for fixed instructions: the rules lead stdin.
				want = r.Rules + "\n\n" + want
			}
			if got := read(t, dump, "stdin.1"); got != want {
				t.Fatalf("stdin %q, want %q", got, want)
			}
			for _, arg := range argsOf(t, dump, 1) {
				if strings.Contains(arg, batchMarker) || (host == HostCodex && strings.Contains(arg, "Fixed rules")) {
					t.Fatalf("argument %q carries the input", arg)
				}
			}
		})
	}
}

func TestTimeoutKillsTheChildAndItsGroup(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			isolate(t)
			dump := fakeCLI(t, `sleep 30 & echo $! > "$dump/child"; sleep 30`)
			put(t, dump, "output", answer)
			r := request(t, host)
			r.Timeout = 1500 * time.Millisecond
			start := time.Now()
			res, err := NewRunner(host).Run(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if res.Reason != ReasonTimeout || res.Output != nil {
				t.Fatalf("result %+v, want a timeout", res)
			}
			if elapsed := time.Since(start); elapsed > 10*time.Second {
				t.Fatalf("the timeout took %v", elapsed)
			}
			if runs(t, dump) != 1 {
				t.Fatal("a timeout must not be retried")
			}
			waitGone(t, dump, "child")
		})
	}
}

// The attempts share one timeout: a rejected model or a refused effort
// doesn't give the next attempt a fresh one.
func TestAttemptsShareTheTimeout(t *testing.T) {
	for name, refusal := range map[string]string{"model": claudeRejected, "effort": claudeEffortRejected("max")} {
		t.Run(name, func(t *testing.T) {
			isolate(t)
			// Every attempt takes 1.2 s; only the third would succeed.
			dump := fakeCLI(t, "sleep 1.2")
			put(t, dump, "stdout", refusal)
			put(t, dump, "exit", "1")
			put(t, dump, "stdout.3", claudeSuccess)
			put(t, dump, "exit.3", "0")
			r := request(t, HostClaude)
			r.Timeout = 2 * time.Second
			res, err := NewRunner(HostClaude).Run(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			// The first attempt used 1.2 s of the 2 s, so the second runs out
			// of time before it can be rejected, and there is no third.
			if n := runs(t, dump); n != 2 {
				t.Fatalf("ran %d times, want 2", n)
			}
			if res.Reason != ReasonTimeout || res.Attempts != 2 || res.Output != nil || res.Model != "claude-test-model" {
				t.Fatalf("result %+v, want a timeout on the second attempt", res)
			}
		})
	}
}

// A refused effort steps down the host's table one entry at a time, so a
// model whose top level is below the table's top runs at its own top, not at
// the default effort.
func TestRefusedEffortStepsDownToTheModelsTop(t *testing.T) {
	cases := []struct {
		host    string
		refused []string // the efforts the model refuses, from the table's top
		top     string   // the model's own top
	}{
		{HostClaude, []string{"max"}, "xhigh"},
		{HostClaude, []string{"max", "xhigh"}, "high"},
		{HostCodex, []string{"ultra"}, "max"},
		{HostCodex, []string{"ultra", "max"}, "xhigh"},
	}
	for _, tc := range cases {
		t.Run(tc.host+" "+tc.top, func(t *testing.T) {
			isolate(t)
			dump := fakeHost(t, tc.host)
			for i, effort := range tc.refused {
				n := strconv.Itoa(i + 1)
				put(t, dump, "stdout."+n, effortRejected(tc.host, effort))
				put(t, dump, "exit."+n, "1")
			}
			r := request(t, tc.host)
			res, err := NewRunner(tc.host).Run(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			tried := append(slices.Clone(tc.refused), tc.top)
			if n := runs(t, dump); n != len(tried) || res.Attempts != len(tried) {
				t.Fatalf("ran %d times, %d attempts reported, want %d", n, res.Attempts, len(tried))
			}
			for i, effort := range tried {
				if args := argsOf(t, dump, i+1); !usesEffort(tc.host, args, effort) || !slices.Contains(args, r.Model) {
					t.Fatalf("attempt %d must keep the model at effort %s: %q", i+1, effort, args)
				}
			}
			if res.Reason != "" || string(res.Output) != answer || res.Model != r.Model || res.ModelNote != "" {
				t.Fatalf("result %+v", res)
			}
			if res.Effort != tc.top {
				t.Fatalf("effort used %q, want %q", res.Effort, tc.top)
			}
			if want := "effort " + tc.refused[0] + " was rejected; the run used " + tc.top; res.Degraded != want {
				t.Fatalf("degraded %q, want %q", res.Degraded, want)
			}
		})
	}
}

// effortRejected is host's refusal of an effort the model can't take.
func effortRejected(host, effort string) string {
	if host == HostClaude {
		return claudeEffortRejected(effort)
	}
	return codexEffortRejected(effort)
}

// usesEffort reports whether args pass effort to host.
func usesEffort(host string, args []string, effort string) bool {
	if host == HostClaude {
		return hasPair(args, "--effort", effort)
	}
	return slices.Contains(args, "model_reasoning_effort="+effort)
}

func TestCanceledRunIsAnErrorAndEndsTheChild(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, `sleep 30 & echo $! > "$dump/child"; sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		waitFile(dump, "child")
		cancel()
	}()
	start := time.Now()
	_, err := NewRunner(HostCodex).Run(ctx, request(t, HostCodex))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("cancel took %v", elapsed)
	}
	waitGone(t, dump, "child")
}

func TestMissingCLIIsCLIMissing(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			isolate(t)
			t.Setenv("PATH", t.TempDir())
			res, err := NewRunner(host).Run(context.Background(), request(t, host))
			if err != nil || res.Reason != ReasonCLIMissing || res.Output != nil || res.Attempts != 0 {
				t.Fatalf("result %+v %v, want cli_missing and no attempt", res, err)
			}
		})
	}
}

// A run starts the CLI by the path install recorded, though PATH lacks it,
// as an app's may, and adds the CLI's folder to the end of the child's PATH,
// where an npm install puts the node its #! line names. A recorded path that
// is gone, or names another program, gives way to PATH.
func TestRecordedCLIPathIsPreferred(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			isolate(t)
			dump := fakeHost(t, host)
			name := cliName(host)
			t.Setenv("PATH", "/usr/bin:/bin")
			r := request(t, host)
			r.CLI = filepath.Join(fakeBin, name)
			if res, err := NewRunner(host).Run(context.Background(), r); err != nil || res.Reason != "" || runs(t, dump) != 1 {
				t.Fatalf("recorded path: %+v %v", res, err)
			}
			if path := envOf(t, dump, 1)["PATH"]; path != "/usr/bin:/bin:"+fakeBin {
				t.Fatalf("child PATH %q", path)
			}
			t.Setenv("PATH", t.TempDir())
			r = request(t, host)
			r.CLI = filepath.Join(t.TempDir(), name)
			if res, err := NewRunner(host).Run(context.Background(), r); err != nil || res.Reason != ReasonCLIMissing || runs(t, dump) != 1 {
				t.Fatalf("a recorded path that is gone, with no CLI on PATH: %+v %v", res, err)
			}
			t.Setenv("PATH", fakeBin+":/usr/bin:/bin")
			for i, recorded := range []string{filepath.Join(t.TempDir(), name), "/bin/sh", name} {
				r = request(t, host)
				r.CLI = recorded
				if res, err := NewRunner(host).Run(context.Background(), r); err != nil || res.Reason != "" || runs(t, dump) != i+2 {
					t.Fatalf("recorded %q: %+v %v", recorded, res, err)
				}
				if path := envOf(t, dump, i+2)["PATH"]; path != fakeBin+":/usr/bin:/bin" {
					t.Fatalf("recorded %q: child PATH %q", recorded, path)
				}
			}
		})
	}
}

// The job passes each run the CLI path install recorded for its host, from
// <state>/cli-paths.json, and none when the file can't be read.
func TestJobPassesTheRecordedCLIPath(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(e.dirs.State, 0o700); err != nil {
		t.Fatal(err)
	}
	clis := filepath.Join(e.dirs.State, "cli-paths.json")
	for i, tc := range []struct{ file, want string }{
		{`{"claude":"/opt/tools/bin/claude","codex":"/opt/tools/bin/codex"}`, "/opt/tools/bin/claude"},
		{`not json`, ""},
	} {
		if err := os.WriteFile(clis, []byte(tc.file), 0o600); err != nil {
			t.Fatal(err)
		}
		c := newChat("recorded-"+strconv.Itoa(i), start.Add(time.Duration(i)*time.Hour))
		talk(c, 1, "amber")
		e.write(c)
		if s := e.run(c); outcomes(s) != "ok" || e.fake.Requests()[i].CLI != tc.want {
			t.Fatalf("%s: %q, CLI %q", tc.file, outcomes(s), e.fake.Requests()[i].CLI)
		}
	}
}

func TestRequestsThatCannotRunAreRefused(t *testing.T) {
	isolate(t)
	dump := fakeHost(t, HostClaude)
	notEmpty := t.TempDir()
	put(t, notEmpty, "AGENTS.md", "planted instructions")
	file := filepath.Join(notEmpty, "AGENTS.md")
	cases := map[string]func(*Request){
		"host mismatch":           func(r *Request) { r.Host = HostCodex },
		"no host":                 func(r *Request) { r.Host = "" },
		"model read as a flag":    func(r *Request) { r.Model = "--dangerously-skip-permissions" },
		"model with a space":      func(r *Request) { r.Model = "claude test" },
		"unknown effort":          func(r *Request) { r.Effort = "turbo" },
		"another host's effort":   func(r *Request) { r.Effort = "ultra" },
		"relative run folder":     func(r *Request) { r.RunDir = "runs/1" },
		"missing run folder":      func(r *Request) { r.RunDir = filepath.Join(notEmpty, "absent") },
		"run folder not empty":    func(r *Request) { r.RunDir = notEmpty },
		"run folder is a file":    func(r *Request) { r.RunDir = file },
		"rules start with a dash": func(r *Request) { r.Rules = "--verbose" },
		"rules with a NUL byte":   func(r *Request) { r.Rules = "a\x00b" },
		"empty rules":             func(r *Request) { r.Rules = "" },
		"blank rules":             func(r *Request) { r.Rules = " \n" },
		"empty batch":             func(r *Request) { r.Input = nil },
		"blank batch":             func(r *Request) { r.Input = []byte("\n\t ") },
		"schema not an object":    func(r *Request) { r.Schema = []byte(`[1]`) },
		"schema not JSON":         func(r *Request) { r.Schema = []byte(`{"type":`) },
		"negative timeout":        func(r *Request) { r.Timeout = -time.Second },
		"feature read as a flag":  func(r *Request) { r.CodexDisable = []string{"--yolo"} },
	}
	for name, change := range cases {
		r := request(t, HostClaude)
		change(&r)
		if res, err := NewRunner(HostClaude).Run(context.Background(), r); err == nil {
			t.Errorf("%s: ran with result %+v", name, res)
		}
	}
	if _, err := NewRunner("gemini").Run(context.Background(), Request{Host: "gemini"}); err == nil {
		t.Error("an unknown host ran")
	}
	if n := runs(t, dump); n != 0 {
		t.Fatalf("the CLI started %d times for refused requests", n)
	}
}

func TestEffortTablesAndTimeouts(t *testing.T) {
	isolate(t)
	if got, _ := resolveEffort(claudeEfforts, "highest"); got != "max" {
		t.Fatalf("Claude highest is %q", got)
	}
	if got, _ := resolveEffort(codexEfforts, "highest"); got != "ultra" {
		t.Fatalf("Codex highest is %q", got)
	}
	for _, levels := range [][]string{claudeEfforts, codexEfforts} {
		last := levels[len(levels)-1]
		for _, effort := range []string{"", "highest", last} {
			if got, err := resolveEffort(levels, effort); err != nil || got != last {
				t.Fatalf("%q resolved to %q %v", effort, got, err)
			}
		}
		if got, err := resolveEffort(levels, levels[0]); err != nil || got != levels[0] {
			t.Fatalf("%q resolved to %q %v", levels[0], got, err)
		}
		if _, err := resolveEffort(levels, "Highest"); err == nil {
			t.Fatal("effort values are exact")
		}
		if got := effortTimeout(levels, levels[0]); got != 180*time.Second {
			t.Fatalf("lowest effort timeout %v", got)
		}
		if got := effortTimeout(levels, last); got != 600*time.Second {
			t.Fatalf("highest effort timeout %v", got)
		}
		for i := 1; i < len(levels); i++ {
			if effortTimeout(levels, levels[i]) <= effortTimeout(levels, levels[i-1]) {
				t.Fatalf("timeout does not grow from %s to %s", levels[i-1], levels[i])
			}
			if got := lowerEffort(levels, levels[i]); got != levels[i-1] {
				t.Fatalf("below %s is %q, want %s", levels[i], got, levels[i-1])
			}
		}
		// Below the lowest entry, or off the table, there is no effort flag.
		for _, effort := range []string{levels[0], "", "turbo"} {
			if got := lowerEffort(levels, effort); got != "" {
				t.Fatalf("below %q is %q", effort, got)
			}
		}
	}
}

func TestClassifyFailureText(t *testing.T) {
	isolate(t)
	cases := []struct{ text, want string }{
		{"Invalid API key · Please run /login", ReasonAuth},
		{"Not logged in · Please run /login", ReasonAuth},
		{"unexpected status 401 Unauthorized: Your authentication token has expired.", ReasonAuth},
		{"Your access token could not be refreshed. Please log out and sign in again.", ReasonAuth},
		{"Claude AI usage limit reached|1767225600", ReasonRateLimit},
		{`API Error: 429 {"type":"error","error":{"type":"rate_limit_error"}}`, ReasonRateLimit},
		// A limit that names a model is still a limit.
		{"You've hit your usage limit for gpt-x. Switch to another model now.", ReasonRateLimit},
		{"There's an issue with the selected model (claude-x). It may not exist or you may not have access to it.", ReasonModelRejected},
		{"The model claude-x is not available on your bedrock deployment.", ReasonModelRejected},
		{"Unsupported value: 'ultra' is not supported with the 'gpt-x' model.", ReasonModelRejected},
		{"error: option '--effort <level>' argument 'turbo' is invalid.", ReasonModelRejected},
		{"API Error: Effort 'max' isn't available with thinking turned off on this model · use --effort high (or the effortLevel setting)", ReasonModelRejected},
		{"Error: required MCP servers failed to initialize: probe", ReasonMCPFailed},
		// A stream codex ended because the answer was filtered or too long is
		// the batch's fault, so it counts toward the session.
		{"stream disconnected before completion: Incomplete response returned, reason: content_filter", ReasonInvalidOutput},
		{"stream disconnected before completion: Incomplete response returned, reason: max_output_tokens", ReasonInvalidOutput},
		{"API Error: Stream ended without receiving any events", ReasonHostError},
		{"Error parsing -c overrides: invalid type: map, expected a boolean for key `tools.update_plan`", ReasonHostError},
		// An optional server's own error words never name the host's failure:
		// the disconnect does, and it isn't auth.
		{"MCP client for `docs` failed to start: authentication required\nstream disconnected before completion", ReasonHostError},
		{"MCP authentication required. Reconnect to continue using this server.", ReasonInvalidOutput},
		{"", ReasonInvalidOutput},
		// Network, server and CLI-usage failures: an outage or a host upgrade,
		// not the batch.
		{"stream disconnected before completion", ReasonHostError},
		{"API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND). A VPN, firewall, or security software can cause this.", ReasonHostError},
		{"request to https://api.anthropic.com/v1/messages failed, reason: getaddrinfo ENOTFOUND api.anthropic.com", ReasonHostError},
		{"API Error: Unable to connect to API: SSL certificate verification failed (UNABLE_TO_GET_ISSUER_CERT_LOCALLY).", ReasonHostError},
		{"API Error: Connection refused — a firewall or proxy may be blocking it (ECONNREFUSED)", ReasonHostError},
		{"API Error: No internet route — check your connection or VPN (ENETUNREACH)", ReasonHostError},
		{"API Error: Connection dropped (ECONNRESET). Check those or switch networks, then try again.", ReasonHostError},
		{"API Error: Connection lost while your computer was asleep", ReasonHostError},
		{"API Error: Connection to the API was lost (EPIPE). This is usually temporary — try again.", ReasonHostError},
		{"API Error: Connection closed before the response finished", ReasonHostError},
		{"API Error: Couldn't connect through your proxy (ERR_PROXY_TUNNEL) — the proxy refused the tunnel", ReasonHostError},
		{"API Error: No response from API (waited 5m)", ReasonHostError},
		{"API Error: Request timed out. Check your internet connection and proxy settings", ReasonHostError},
		{"API Error: 500 Internal server error. This is a server-side issue, usually temporary — try again in a moment.", ReasonHostError},
		{`API Error: 502 {"type":"error","error":{"type":"api_error","message":"upstream failed"}}`, ReasonHostError},
		{`API Error: 503 {"type":"error","error":{"type":"api_error","message":"upstream unavailable"}}`, ReasonHostError},
		{"API Error: 504 upstream request timeout", ReasonHostError},
		{"unexpected status 500 Internal Server Error: upstream failed", ReasonHostError},
		{"exceeded retry limit, last status: 502 Bad Gateway", ReasonHostError},
		{"unexpected status 503 Service Unavailable: upstream connect error", ReasonHostError},
		{"unexpected status 504 Gateway Timeout", ReasonHostError},
		{"error sending request for url (https://chatgpt.com/backend-api/codex/responses): operation timed out", ReasonHostError},
		{"error: unknown option '--strict-mcp-config'", ReasonHostError},
		{"You cannot use --strict-mcp-config when an enterprise MCP config is present", ReasonHostError},
		{"error: unexpected argument '--no-daemon' found", ReasonHostError},
		{"error: invalid value 'browser_use' for '--disable <DISABLE>'", ReasonHostError},
		{"Error: Unknown feature flag: browser_use", ReasonHostError},
		// An outage that names the model is an outage, not a refusal, and a
		// limit stays a limit.
		{"unexpected status 503 Service Unavailable: The model gpt-x is not available right now", ReasonHostError},
		{"exceeded retry limit, last status: 429 Too Many Requests", ReasonRateLimit},
		{"API Error: Repeated 529 Overloaded errors. The API is at capacity — this is usually temporary.", ReasonRateLimit},
		// Near misses: ordinary error text holding a word of those phrases.
		{"the tool call timed out", ReasonInvalidOutput},
		{"the server returned an answer that is not JSON", ReasonInvalidOutput},
		{"the summary must be 500 characters or fewer", ReasonInvalidOutput},
		{"internal error while rendering the answer", ReasonInvalidOutput},
		{"invalid value for field summary", ReasonInvalidOutput},
		{"invalid value 'x' in field summary, for 'refs'", ReasonInvalidOutput},
		{"the option is unknown", ReasonInvalidOutput},
		{"unexpected token in the response", ReasonInvalidOutput},
		{"the stream ended before the answer was complete", ReasonInvalidOutput},
		{"error sending the answer to the validator", ReasonInvalidOutput},
		{"the docs service is unavailable", ReasonInvalidOutput},
		{"connection established, then the answer was empty", ReasonInvalidOutput},
		{"API Error: 400 the request was malformed", ReasonInvalidOutput},
		// The API's own error for an unknown model, without a status field.
		{`API Error: 404 {"type":"error","error":{"type":"not_found_error","message":"model: claude-x"}}`, ReasonModelRejected},
		{"Not signed in. Please run 'codex login' to sign in with ChatGPT.", ReasonAuth},
		{"OAuth token revoked · Please run /login", ReasonAuth},
		{"Selected model is at capacity. Please try a different model.", ReasonRateLimit},
		{"You hit your spend cap set in your workspace. Increase your spend cap to continue.", ReasonRateLimit},
		{"spend limit reached (daily; resets 2026-08-08 00:00 UTC)", ReasonRateLimit},
		{"Quota exceeded. Check your plan and billing details.", ReasonRateLimit},
		// Loose words never name a failure: a false auth pauses every update
		// for the day, and a false limit retries an oversize batch for ever.
		{"Token limit reached for this conversation", ReasonInvalidOutput},
		{`The plugin "foo" failed to load because its credentials are missing`, ReasonInvalidOutput},
		{"failed to refresh OAuth tokens for server docs", ReasonInvalidOutput},
		{"error assigning the response to a field", ReasonInvalidOutput},
		{"unexpected quotation mark in the response", ReasonInvalidOutput},
	}
	for _, tc := range cases {
		var d diagnostics
		d.note(tc.text)
		if got := d.reason(); got != tc.want {
			t.Errorf("%q: %s, want %s", tc.text, got, tc.want)
		}
	}
}

// A refusal names the effort by the word or by the attempt's quoted value;
// one that names only the model refuses the model.
func TestRefusalNamesTheEffortOrTheModel(t *testing.T) {
	isolate(t)
	cases := []struct {
		text, effort string
		want         bool
	}{
		{"API Error: 400 effort max is not supported by this model", "max", true},
		{"error: option '--effort <level>' argument 'max' is invalid.", "max", true},
		{"API Error: Effort 'max' isn't available with thinking turned off on this model", "max", true},
		{"Unsupported value: 'ultra' is not supported with the 'gpt-x' model.", "ultra", true},
		// Another value quoted: this attempt's effort isn't the one refused.
		{"Unsupported value: 'ultra' is not supported with the 'gpt-x' model.", "max", false},
		{"There's an issue with the selected model (claude-x). It may not exist or you may not have access to it.", "max", false},
		{"The 'gpt-x' model is not supported when using Codex with a ChatGPT account.", "ultra", false},
		{`API Error: 404 {"type":"error","error":{"type":"not_found_error","message":"model: claude-x"}}`, "max", false},
		// The effort's value outside a refusal names nothing.
		{"the 'max' plan is not active", "max", false},
	}
	for _, tc := range cases {
		d := diagnostics{effort: tc.effort}
		d.note(tc.text)
		if got := d.refusesEffort(); got != tc.want {
			t.Errorf("%q at effort %s: %v, want %v", tc.text, tc.effort, got, tc.want)
		}
	}
}

func TestOutputIsCapped(t *testing.T) {
	isolate(t)
	c := &capped{limit: 4}
	for _, part := range []string{"abc", "def"} {
		if n, err := c.Write([]byte(part)); n != len(part) || err != nil {
			t.Fatalf("write %q: %d %v", part, n, err)
		}
	}
	if c.String() != "abcd" || !c.overflow {
		t.Fatalf("kept %q, overflow %v", c.String(), c.overflow)
	}
	// A capped envelope is never read, even when what was kept parses.
	out := &capped{limit: len(claudeSuccess)}
	_, _ = out.Write([]byte(claudeSuccess + "\n trailing"))
	if a := claudeResult(child{}, out, ""); a.reason != ReasonInvalidOutput || a.output != nil {
		t.Fatalf("capped envelope gave %+v", a)
	}
	// A Codex event longer than the line cap can't be classified: the run is
	// invalid and stopped.
	stopped := false
	s := &codexStream{stop: func() { stopped = true }}
	if n, err := s.Write(make([]byte, maxLine+1)); n != maxLine+1 || err != nil {
		t.Fatalf("write: %d %v", n, err)
	}
	if !s.invalid || !stopped {
		t.Fatalf("over-long line: invalid %v, stopped %v", s.invalid, stopped)
	}
}
