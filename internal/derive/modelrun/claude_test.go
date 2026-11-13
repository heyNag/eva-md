package modelrun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// The Claude Code backend's tests, run through New on the rig's script.
// Every envelope here is made up in the shape claude 2.1.312's
// --output-format json takes.

// claudeSuccess is a clean run's envelope: 120 tokens in, 30 written to the
// cache and 50 read from it, 40 out, and no modelUsage.
const claudeSuccess = `{"type":"result","subtype":"success","is_error":false,"duration_ms":1200,"num_turns":1,"result":"","session_id":"00000000-0000-4000-8000-000000000000","usage":{"input_tokens":120,"cache_creation_input_tokens":30,"cache_read_input_tokens":50,"output_tokens":40},"permission_denials":[],"structured_output":{"summary":"done"}}`

// claudeSuccessUsage is claudeSuccess's usage: the cache writes in In, the
// cache reads apart.
var claudeSuccessUsage = Usage{In: 150, CachedIn: 50, Out: 40}

// claudeRejected is the envelope claude prints for a model the account
// can't use.
const claudeRejected = `{"type":"result","subtype":"success","is_error":true,"num_turns":1,"result":"There's an issue with the selected model (claude-test-model). It may not exist or you may not have access to it. Run /model to pick a different model.","usage":{"input_tokens":0,"output_tokens":0}}`

// claudeEffortRejected is the envelope claude prints for an effort the
// model can't take.
func claudeEffortRejected(effort string) string {
	return `{"type":"result","subtype":"success","is_error":true,"api_error_status":400,"result":"API Error: 400 {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"effort ` +
		effort + ` is not supported by this model\"}}","usage":{"input_tokens":0,"output_tokens":0}}`
}

// claudeStrictRefusal is claude's refusal of --strict-mcp-config under a
// managed MCP config.
const claudeStrictRefusal = "Error: You cannot use --strict-mcp-config when an enterprise MCP config is present"

// claudeEnvelopeWith is a clean envelope with the answer, its fields given
// as JSON members after type, before the answer.
func claudeEnvelopeWith(members string) string {
	if members != "" {
		members += ","
	}
	return `{"type":"result","subtype":"success","is_error":false,` + members + `"structured_output":{"summary":"done"}}`
}

// claudeRunner is the registered Claude Code backend's runner, its probes
// through a counting Env.
func claudeRunner(t *testing.T, o Options) Runner {
	t.Helper()
	_, env := testEnv()
	o.Env = env
	r, err := New(adapter.ClaudeCode, o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// claudeRun runs one call on the rig readied by files, a map of the dump's
// file names to their content, and gives the outcome and the dump.
func claudeRun(t *testing.T, files map[string]string, edit func(*Call)) (Outcome, string) {
	t.Helper()
	isolate(t)
	dump := fakeCLI(t, "")
	for name, content := range files {
		put(t, dump, name, content)
	}
	c := call(t, adapter.ClaudeCode)
	if edit != nil {
		edit(&c)
	}
	out, err := claudeRunner(t, Options{}).Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return out, dump
}

// claudeRead is the backend's reading of one attempt, apart from the
// process: stdout as the CLI printed it, and how the CLI ended.
func claudeRead(stdout string, e exit) result {
	out := &capped{limit: claudeMaxEnvelope}
	_, _ = out.Write([]byte(stdout))
	return claudeResult(e, out, "")
}

// claudeJSONDepth is how deep encoding/json reads one value.
const claudeJSONDepth = 10000

// claudeHasPair reports whether flag is followed by value in args.
func claudeHasPair(args []string, flag, value string) bool {
	i := slices.Index(args, flag)
	return i >= 0 && i+1 < len(args) && args[i+1] == value
}

func TestClaudeArgumentsMatchTheDesign(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "stdout", claudeSuccess)
	cli := ownCLI(t, adapter.ClaudeCode)
	r := claudeRunner(t, Options{CLI: cli})
	c := call(t, adapter.ClaudeCode)
	// The user's config is Codex's to ignore: claude's run takes no flag
	// for it.
	c.Effort, c.IgnoreUserConfig = "max", true
	out, err := r.Run(context.Background(), c)
	if err != nil || out.Class != ClassOK || string(out.Answer) != answer || out.Attempts != 1 {
		t.Fatalf("run: %+v %v", out, err)
	}
	want := []string{"-p", "--safe-mode", "--tools", "", "--strict-mcp-config", "--no-session-persistence", "--permission-prompts", "none",
		"--model", "claude-test-model", "--effort", "max", "--system-prompt", c.System,
		"--output-format", "json", "--json-schema", string(c.Schema)}
	if got := argsOf(t, dump, 1); !slices.Equal(got, want) {
		t.Fatalf("arguments\n got %q\nwant %q", got, want)
	}
	// The input is stdin, whole, and in no argument.
	if got := read(t, dump, "stdin.1"); got != string(c.Input) {
		t.Fatalf("stdin %q, want %q", got, c.Input)
	}
	for _, a := range argsOf(t, dump, 1) {
		if strings.Contains(a, batchMarker) {
			t.Fatalf("an argument holds the input: %q", a)
		}
	}
	real, err := filepath.EvalSymlinks(c.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(read(t, dump, "pwd.1")); got != real {
		t.Fatalf("working folder %s, want %s", got, real)
	}
	if entries, err := os.ReadDir(c.RunDir); err != nil || len(entries) != 0 {
		t.Fatalf("run folder holds %d entries: %v", len(entries), err)
	}
	// The CLI is the one recorded, named as the adapter names it; the
	// outcome says what was asked, and the host named no model.
	h, _ := adapter.ByID(string(adapter.ClaudeCode))
	if out.CLI != cli || filepath.Base(out.CLI) != h.Names.CLI || out.CLIVersion == "" || out.Billing != BillingPlan {
		t.Fatalf("outcome %+v", out)
	}
	if out.ModelAsked != "claude-test-model" || out.Effort != "max" || out.ModelAnswered != "" || out.ModelsSeen != nil {
		t.Fatalf("models %+v", out)
	}
	if out.Usage != claudeSuccessUsage || !out.UsageSeen || !slices.Equal(out.Signals, []Signal{SignalModelUnreported}) {
		t.Fatalf("usage %+v, signals %q", out.Usage, out.Signals)
	}
	// With no model and no effort, neither flag.
	c = call(t, adapter.ClaudeCode)
	c.Model = ""
	if out, err := r.Run(context.Background(), c); err != nil || out.Class != ClassOK || out.ModelAsked != "" {
		t.Fatalf("second run: %+v %v", out, err)
	}
	if got := argsOf(t, dump, 2); slices.Contains(got, "--model") || slices.Contains(got, "--effort") || !slices.Contains(got, "--strict-mcp-config") {
		t.Fatalf("second run arguments %q", got)
	}
}

func TestClaudeEnvelopes(t *testing.T) {
	deep := strings.Repeat("[", claudeJSONDepth+1) + strings.Repeat("]", claudeJSONDepth+1)
	cases := []struct {
		name, stdout, stderr string
		exit                 int
		class                Class
		refused              string
		usage                Usage
	}{
		{name: "structured output", stdout: claudeSuccess, class: ClassOK, usage: claudeSuccessUsage},
		{name: "no usage", stdout: claudeEnvelopeWith(""), class: ClassOK},
		// modelUsage counts every model call, so it wins over usage.
		{name: "model usage", stdout: claudeEnvelopeWith(`"usage":{"input_tokens":1,"output_tokens":1},` +
			`"modelUsage":{"claude-a":{"inputTokens":100,"outputTokens":10,"cacheReadInputTokens":5,"cacheCreationInputTokens":5},` +
			`"claude-b":{"inputTokens":20,"outputTokens":2,"cacheReadInputTokens":0,"cacheCreationInputTokens":0}}`),
			class: ClassOK, usage: Usage{In: 125, CachedIn: 5, Out: 12}},
		{name: "structured output missing", stdout: `{"type":"result","subtype":"success","is_error":false,"result":"{\"summary\":\"done\"}","usage":{"input_tokens":10,"output_tokens":5}}`,
			class: ClassAnswerInvalid, usage: Usage{In: 10, Out: 5}},
		{name: "structured output null", stdout: `{"type":"result","subtype":"success","is_error":false,"structured_output":null}`, class: ClassAnswerInvalid},
		{name: "structured output not an object", stdout: `{"type":"result","subtype":"success","is_error":false,"structured_output":"{\"summary\":\"done\"}"}`, class: ClassAnswerInvalid},
		{name: "structured output retries used up", stdout: `{"type":"result","subtype":"error_max_structured_output_retries","is_error":true,"errors":["no valid output"],"usage":{"input_tokens":300,"output_tokens":90}}`,
			exit: 1, class: ClassAnswerInvalid, usage: Usage{In: 300, Out: 90}},
		{name: "authentication", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login"}`, exit: 1, class: ClassAuth},
		{name: "authentication without an envelope", stderr: "Not logged in. Run claude auth login to authenticate.\n", exit: 1, class: ClassAuth},
		{name: "rate limit", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1767225600"}`, exit: 1, class: ClassRateLimit},
		{name: "rate limit by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":429,"result":"API Error: request rejected"}`, exit: 1, class: ClassRateLimit},
		{name: "overloaded by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":529,"result":"API Error: request rejected"}`, exit: 1, class: ClassRateLimit},
		{name: "authentication by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":401,"result":"API Error: request rejected"}`, exit: 1, class: ClassAuth},
		// A status names the class before any text does.
		{name: "status before text", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":503,"result":"Not logged in"}`, exit: 1, class: ClassHost},
		{name: "unknown model by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":404,"result":"API Error: request rejected"}`, exit: 1, class: ClassHost, refused: RefusedModel},
		// Any other status names nothing, and the text goes on to name it.
		{name: "another status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":403,"result":"Invalid API key · Please run /login"}`, exit: 1, class: ClassAuth},
		{name: "a status that is no number", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":"429","result":"request rejected"}`, exit: 1, class: ClassHost},
		// An outage or a command line a host upgrade no longer takes is the
		// host's.
		{name: "server error by status", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error_status":503,"result":"API Error: request failed"}`, exit: 1, class: ClassHost},
		{name: "server error", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"API Error: 500 Internal server error. This is a server-side issue, usually temporary — try again in a moment."}`,
			exit: 1, class: ClassHost},
		{name: "no connection", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND). A VPN, firewall, or security software can cause this. Check those or switch networks, then try again."}`,
			exit: 1, class: ClassHost},
		{name: "unknown option after an upgrade", stderr: "error: unknown option '--strict-mcp-config'\n", exit: 1, class: ClassHost},
		{name: "rate limit in errors", stdout: `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["Too many requests"]}`, exit: 1, class: ClassRateLimit},
		{name: "sign-in in an API error", stdout: `{"type":"result","subtype":"success","is_error":true,"api_error":{"type":"authentication_error"}}`, exit: 1, class: ClassAuth},
		// An MCP server's own words never name claude's failure.
		{name: "MCP server's sign-in words", stderr: "MCP server docs: authentication required, please log in\n", exit: 1, class: ClassHost},
		// A refused tool call means the run had a tool to call: tool_used,
		// even beside an answer.
		{name: "tool denied", stdout: claudeEnvelopeWith(`"usage":{"input_tokens":10,"output_tokens":5},` +
			`"permission_denials":[{"tool_name":"mcp__docs__search","tool_use_id":"toolu_01","tool_input":{"query":"release"}}]`),
			class: ClassToolUsed, usage: Usage{In: 10, Out: 5}},
		{name: "denials in another shape", stdout: claudeEnvelopeWith(`"permission_denials":{"count":1}`), class: ClassToolUsed},
		{name: "a denial beside a failure", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Not logged in","permission_denials":[1]}`, exit: 1, class: ClassToolUsed},
		{name: "no denials", stdout: claudeEnvelopeWith(`"permission_denials":null`), class: ClassOK},
		// What v1 called the chat's fault is the host's.
		{name: "error during execution", stdout: `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["stream ended"]}`, exit: 1, class: ClassHost},
		{name: "another subtype", stdout: claudeEnvelopeWith(`"subtype":"error_max_turns"`), class: ClassHost},
		{name: "no subtype", stdout: `{"type":"result","is_error":false,"structured_output":{"summary":"done"}}`, class: ClassHost},
		// The model's own words never name a failure, here a non-zero exit.
		{name: "failed exit with model text", stdout: `{"type":"result","subtype":"success","is_error":false,"result":"we hit the rate limit yesterday","structured_output":{"summary":"done"}}`,
			exit: 1, class: ClassHost},
		{name: "plain-text error", stdout: "Error: Invalid API key · Please run /login\n", exit: 1, class: ClassAuth},
		{name: "plain text after a clean exit", stdout: "we hit the rate limit yesterday\n", class: ClassHost},
		{name: "nothing printed", class: ClassHost},
		{name: "nothing printed, a failed exit", exit: 1, class: ClassHost},
		// Output this reader can't vouch for is the host's.
		{name: "not an envelope", stdout: `{"type":"assistant","structured_output":{"summary":"done"}}`, class: ClassHost},
		{name: "an array", stdout: `[` + claudeSuccess + `]`, class: ClassHost},
		{name: "two envelopes", stdout: claudeSuccess + "\n" + claudeSuccess, class: ClassHost},
		{name: "an envelope after other text", stdout: "notice " + claudeSuccess, class: ClassHost},
		{name: "a key twice", stdout: `{"type":"result","subtype":"success","is_error":false,"structured_output":{"summary":"done"},"structured_output":{"summary":"other"}}`, class: ClassHost},
		{name: "an unknown key twice", stdout: claudeEnvelopeWith(`"session_id":"a","session_id":"b"`), class: ClassHost},
		{name: "is_error twice", stdout: claudeEnvelopeWith(`"is_error":true`), class: ClassHost},
		{name: "a known key in another case", stdout: claudeEnvelopeWith(`"Is_Error":true`), class: ClassHost},
		{name: "type in another case", stdout: `{"Type":"result","subtype":"success","is_error":false,"structured_output":{"summary":"done"}}`, class: ClassHost},
		{name: "is_error that is text", stdout: `{"type":"result","subtype":"success","is_error":"false","structured_output":{"summary":"done"}}`, class: ClassHost},
		{name: "is_error that is null", stdout: `{"type":"result","subtype":"success","is_error":null,"structured_output":{"summary":"done"}}`, class: ClassOK},
		{name: "subtype that is no text", stdout: `{"type":"result","subtype":1,"is_error":false,"structured_output":{"summary":"done"}}`, class: ClassHost},
		{name: "a model twice", stdout: claudeEnvelopeWith(`"modelUsage":{"claude-a":{"outputTokens":1},"claude-a":{"outputTokens":2}}`), class: ClassHost, usage: Usage{Out: 3}},
		{name: "a subtype that is no text beside a status", stdout: `{"type":"result","subtype":1,"is_error":true,"api_error_status":401}`, exit: 1, class: ClassHost},
		{name: "nested past the depth", stdout: claudeEnvelopeWith(`"x":` + deep), class: ClassHost},
		{name: "nested past the depth, a failed exit", stdout: claudeEnvelopeWith(`"x":` + deep), exit: 1, class: ClassHost},
		// A capped envelope is no envelope.
		{name: "past the cap", stdout: claudeEnvelopeWith(`"pad":"` + strings.Repeat("a", claudeMaxEnvelope) + `"`), class: ClassHost},
		{name: "past the cap, a failed exit", stdout: claudeEnvelopeWith(`"pad":"` + strings.Repeat("a", claudeMaxEnvelope) + `"`), exit: 1, class: ClassHost},
		// What lies past the cap is unread, so an envelope followed by more
		// than the cap can't hide a second one, here with a denial.
		{name: "an envelope, then past the cap another", stdout: claudeSuccess + strings.Repeat(" ", claudeMaxEnvelope) +
			claudeEnvelopeWith(`"permission_denials":[{"tool_name":"x"}]`), class: ClassHost},
		// is_error names a failure on its own, beside a valid answer and a
		// clean exit too.
		{name: "is_error beside a valid answer", stdout: `{"type":"result","subtype":"success","is_error":true,"structured_output":{"summary":"done"}}`, class: ClassHost},
		// Beside an envelope, stderr is read too.
		{name: "is_error with its words on stderr", stdout: `{"type":"result","subtype":"success","is_error":true}`, stderr: "Not logged in\n", exit: 1, class: ClassAuth},
		// A status names the class beside is_error false and a valid answer.
		{name: "a status beside a valid answer", stdout: `{"type":"result","subtype":"success","is_error":false,"api_error_status":401,"structured_output":{"summary":"done"}}`, class: ClassAuth},
		// Retries used up come before a status.
		{name: "retries used up beside a status", stdout: `{"type":"result","subtype":"error_max_structured_output_retries","is_error":true,"api_error_status":503}`, exit: 1, class: ClassAnswerInvalid},
		// A sign-in problem before a limit.
		{name: "a limit and a sign-in problem", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"rate limit reached\nNot logged in"}`, exit: 1, class: ClassAuth},
		// A refusal counts only on a line that names the model or the effort.
		{name: "an error that names nothing", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"unknown error"}`, exit: 1, class: ClassHost},
		{name: "an invalid request that names nothing", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"invalid request"}`, exit: 1, class: ClassHost},
		{name: "a refusal and a model on lines apart", stdout: `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["model selection done","unknown error"]}`, exit: 1, class: ClassHost},
		// Each of errors is read apart: an MCP server's line is dropped
		// without claude's beside it.
		{name: "an MCP line beside claude's in errors", stdout: `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["MCP server docs failed to start","Not logged in · Please run /login"]}`,
			exit: 1, class: ClassAuth},
		// A type key in another case still makes an envelope, so a denial
		// beside it is read.
		{name: "type in another case beside a denial", stdout: `{"Type":"result","subtype":"success","is_error":false,"structured_output":{"summary":"done"},"permission_denials":[{}]}`, class: ClassToolUsed},
		// Error text is kept to 64 KiB: a sign-in line after that much
		// filler names nothing. (The core keeps 64 KiB of stderr, so the
		// filler is in the envelope.)
		{name: "a sign-in line past the text kept", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"` +
			strings.Repeat(`filler line\n`, 8000) + `Not logged in"}`, exit: 1, class: ClassHost},
		{name: "a sign-in line within the text kept", stdout: `{"type":"result","subtype":"success","is_error":true,"result":"` +
			strings.Repeat(`filler line\n`, 4000) + `Not logged in"}`, exit: 1, class: ClassAuth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, dump := claudeRun(t, map[string]string{"stdout": tc.stdout, "stderr": tc.stderr, "exit": strconv.Itoa(tc.exit)}, nil)
			if out.Class != tc.class || out.Refused != tc.refused {
				t.Fatalf("outcome %+v, want class %s refusing %q", out, tc.class, tc.refused)
			}
			if out.Usage != tc.usage || out.UsageSeen != (tc.usage != Usage{}) {
				t.Fatalf("usage %+v (seen %v), want %+v", out.Usage, out.UsageSeen, tc.usage)
			}
			if (tc.class == ClassOK) != (string(out.Answer) == answer) || (tc.class != ClassOK && out.Answer != nil) {
				t.Fatalf("answer %q on %s", out.Answer, out.Class)
			}
			if n := runs(t, dump); n != 1 || out.Attempts != 1 {
				t.Fatalf("ran %d times, %d attempts reported", n, out.Attempts)
			}
		})
	}
}

// A denial beside a valid answer is tool_used, its answer dropped, through
// the whole run.
func TestClaudeADenialBesideAValidAnswerIsToolUsed(t *testing.T) {
	stdout := strings.Replace(claudeSuccess, `"permission_denials":[]`,
		`"permission_denials":[{"tool_name":"Bash","tool_use_id":"toolu_02","tool_input":{"command":"ls"}}]`, 1)
	out, _ := claudeRun(t, map[string]string{"stdout": stdout}, nil)
	if out.Class != ClassToolUsed || out.Answer != nil || out.Usage != claudeSuccessUsage || out.Attempts != 1 {
		t.Fatalf("outcome %+v", out)
	}
}

// A denial in any shape, under its key in any letter case, given once or
// beside an empty one, anywhere in the envelope and whatever else the
// envelope says, is a tool sign; only no key, null and an empty list are
// none.
func TestClaudeADenialInAnyShapeIsToolUsed(t *testing.T) {
	none := []string{"", `null`, `[]`, `[ ]`}
	denials := []string{`""`, `"Bash"`, `0`, `1`, `-1`, `1.5`, `true`, `false`, `{}`, `{"count":0}`, `[null]`, `[{}]`, `[[]]`, `[""]`, `[0]`,
		`[{"tool_name":"x"}]`, `{"tool_name":"x"}`, `[{}, {}]`}
	keys := []string{"permission_denials", "Permission_Denials", "PERMISSION_DENIALS", "permission_Denials"}
	exits := []exit{{started: true}, {started: true, failed: true}, {started: true, failed: true, timedOut: true}}
	others := []string{
		`"subtype":"success","is_error":false,"structured_output":{"summary":"done"}`,
		`"subtype":"error_max_structured_output_retries","is_error":true`,
		`"subtype":"success","is_error":true,"api_error_status":401,"result":"x"`,
		`"subtype":"success","is_error":true,"result":"Not logged in"`,
		`"subtype":"error_during_execution","is_error":true`,
		`"subtype":"success","is_error":false,"structured_output":{"summary":"done"},"session_id":"a","session_id":"b"`,
		`"Subtype":"success","is_error":false,"structured_output":{"summary":"done"}`,
	}
	envelope := func(before, after string, denial ...string) string {
		parts := []string{`"type":"result"`}
		if before != "" {
			parts = append(parts, before)
		}
		for _, d := range denial {
			if d != "" {
				parts = append(parts, d)
			}
		}
		if after != "" {
			parts = append(parts, after)
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	n := 0
	for _, key := range keys {
		for _, d := range denials {
			member := `"` + key + `":` + d
			for _, other := range others {
				for _, stdout := range []string{
					envelope(other, "", member), envelope("", other, member),
					envelope(other, "", `"permission_denials":[]`, member), envelope(other, "", member, `"permission_denials":[]`),
					envelope(other, "", `"permission_denials":null`, member),
				} {
					for _, e := range exits {
						n++
						if res := claudeRead(stdout, e); !res.toolSign || !e.timedOut && res.class != ClassToolUsed || res.answer != nil {
							t.Fatalf("%s, exit %+v: %+v", stdout, e, res)
						}
					}
				}
			}
		}
	}
	for _, d := range none {
		member := ""
		if d != "" {
			member = `"permission_denials":` + d
		}
		res := claudeRead(envelope(others[0], "", member), exit{started: true})
		if res.toolSign || res.class != ClassOK {
			t.Fatalf("denials %q: %+v", d, res)
		}
	}
	t.Logf("%d envelopes with a denial", n)
	// Through the whole run, for a shape v1 has not seen.
	out, _ := claudeRun(t, map[string]string{"stdout": envelope(others[0], "", `"Permission_Denials":[{}]`)}, nil)
	if out.Class != ClassToolUsed || out.Answer != nil {
		t.Fatalf("outcome %+v", out)
	}
}

// Every model modelUsage names is kept, the one with the most output tokens
// as the one that answered, the first by name on a tie, and never the model
// asked for unless the host names it.
func TestClaudeModels(t *testing.T) {
	usage := func(models string) string {
		return claudeEnvelopeWith(`"usage":{"input_tokens":1,"output_tokens":1},"modelUsage":{` + models + `}`)
	}
	cases := []struct {
		name, stdout string
		seen         []string
		answered     string
		signals      []Signal
	}{
		{"two models", usage(`"claude-a":{"outputTokens":10},"claude-b":{"outputTokens":2}`),
			[]string{"claude-a", "claude-b"}, "claude-a", []Signal{SignalClaudeSeveralModels}},
		{"the other one wrote more", usage(`"claude-a":{"outputTokens":2},"claude-b":{"outputTokens":10}`),
			[]string{"claude-a", "claude-b"}, "claude-b", []Signal{SignalClaudeSeveralModels}},
		{"a tie", usage(`"claude-b":{"outputTokens":5},"claude-a":{"outputTokens":5}`),
			[]string{"claude-a", "claude-b"}, "claude-a", []Signal{SignalClaudeSeveralModels}},
		// An output count that can't be read, or none, leaves the answering
		// model unknown: the one whose count can't be read may have written
		// the most.
		{"three models, counts that are none", usage(`"claude-c":{"outputTokens":"9"},"claude-b":{"outputTokens":-4},"claude-a":7`),
			[]string{"claude-a", "claude-b", "claude-c"}, "", []Signal{SignalClaudeModelUnnamed, SignalClaudeSeveralModels, SignalModelUnreported}},
		{"a count that is text", usage(`"claude-a":{"outputTokens":"999999"},"claude-b":{"outputTokens":1}`),
			[]string{"claude-a", "claude-b"}, "", []Signal{SignalClaudeModelUnnamed, SignalClaudeSeveralModels, SignalModelUnreported}},
		{"a count past the plausible", usage(`"claude-a":{"outputTokens":2e12},"claude-b":{"outputTokens":1}`),
			[]string{"claude-a", "claude-b"}, "", []Signal{SignalClaudeModelUnnamed, SignalClaudeSeveralModels, SignalModelUnreported}},
		{"a count that is a fraction", usage(`"claude-a":{"outputTokens":1.5}`),
			[]string{"claude-a"}, "", []Signal{SignalClaudeModelUnnamed, SignalModelUnreported}},
		{"no count", usage(`"claude-a":{"inputTokens":5}`),
			[]string{"claude-a"}, "", []Signal{SignalClaudeModelUnnamed, SignalModelUnreported}},
		{"a null count", usage(`"claude-a":{"outputTokens":null},"claude-b":{"outputTokens":1}`),
			[]string{"claude-a", "claude-b"}, "", []Signal{SignalClaudeModelUnnamed, SignalClaudeSeveralModels, SignalModelUnreported}},
		{"one model", usage(`"claude-a":{"outputTokens":3}`), []string{"claude-a"}, "claude-a", nil},
		// The host may name the model asked for: that is its word, kept.
		{"the model asked for", usage(`"claude-test-model":{"outputTokens":3}`), []string{"claude-test-model"}, "claude-test-model", nil},
		{"no modelUsage", claudeEnvelopeWith(""), nil, "", []Signal{SignalModelUnreported}},
		{"an empty modelUsage", usage(""), nil, "", []Signal{SignalModelUnreported}},
		{"a null modelUsage", claudeEnvelopeWith(`"modelUsage":null`), nil, "", []Signal{SignalModelUnreported}},
		// A model whose name can't be kept is said to be there; when it
		// wrote the most, no model is said to have answered.
		{"a name that is not a model's, writing less", usage(`"claude-a":{"outputTokens":10},"not a model":{"outputTokens":2}`),
			[]string{"claude-a"}, "claude-a", []Signal{SignalClaudeModelUnnamed, SignalClaudeSeveralModels}},
		{"a name that is not a model's, writing more", usage(`"claude-a":{"outputTokens":2},"--model":{"outputTokens":10}`),
			[]string{"claude-a"}, "", []Signal{SignalClaudeModelUnnamed, SignalClaudeSeveralModels, SignalModelUnreported}},
		{"only a name that is not a model's", usage(`"":{"outputTokens":2}`),
			nil, "", []Signal{SignalClaudeModelUnnamed, SignalModelUnreported}},
		{"models in a list", claudeEnvelopeWith(`"modelUsage":[{"model":"claude-b"}]`),
			nil, "", []Signal{SignalClaudeModelUnnamed, SignalModelUnreported}},
		{"models as text", claudeEnvelopeWith(`"modelUsage":"claude-b"`),
			nil, "", []Signal{SignalClaudeModelUnnamed, SignalModelUnreported}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := claudeRun(t, map[string]string{"stdout": tc.stdout}, nil)
			if out.Class != ClassOK || out.ModelAsked != "claude-test-model" {
				t.Fatalf("outcome %+v", out)
			}
			if !slices.Equal(out.ModelsSeen, tc.seen) || out.ModelAnswered != tc.answered || !slices.Equal(out.Signals, tc.signals) {
				t.Fatalf("seen %q, answered %q, signals %q; want %q, %q, %q", out.ModelsSeen, out.ModelAnswered, out.Signals, tc.seen, tc.answered, tc.signals)
			}
		})
	}
	// An envelope this reader can't vouch for still says which models it
	// names, and the larger of what it says it spent, but not which answered:
	// models in another key's case, or given twice, are host.
	for _, stdout := range []string{
		claudeEnvelopeWith(`"modelUsage":{"claude-a":{"outputTokens":9}},"modelUsage":{"claude-b":{"outputTokens":1}}`),
		claudeEnvelopeWith(`"modelUsage":{"claude-a":{"outputTokens":9}},"ModelUsage":{"claude-b":{"outputTokens":1}}`),
		claudeEnvelopeWith(`"ModelUsage":{"claude-a":{"outputTokens":9},"claude-b":{"outputTokens":1}}`),
	} {
		out, _ := claudeRun(t, map[string]string{"stdout": stdout}, nil)
		if out.Class != ClassHost || !slices.Equal(out.ModelsSeen, []string{"claude-a", "claude-b"}) || out.ModelAnswered != "" || !out.UsageSeen || out.Usage.Out < 9 {
			t.Fatalf("%s: outcome %+v", stdout, out)
		}
	}
	// Models are kept on a failure and beside a tool.
	for _, stdout := range []string{
		`{"type":"result","subtype":"success","is_error":true,"result":"Not logged in","modelUsage":{"claude-a":{"outputTokens":1}}}`,
		claudeEnvelopeWith(`"permission_denials":[1],"modelUsage":{"claude-a":{"outputTokens":1}}`),
	} {
		out, _ := claudeRun(t, map[string]string{"stdout": stdout, "exit": "1"}, nil)
		if out.Class == ClassOK || !slices.Equal(out.ModelsSeen, []string{"claude-a"}) || out.ModelAnswered != "claude-a" {
			t.Fatalf("%s: outcome %+v", stdout, out)
		}
	}
}

// Usage comes from modelUsage where it names any model, else from usage: In
// the input and the cache writes, CachedIn the cache reads; a count that is
// no count is 0.
func TestClaudeUsage(t *testing.T) {
	for _, tc := range []struct {
		name, members string
		want          Usage
		seen          bool
	}{
		{"usage", `"usage":{"input_tokens":7,"cache_creation_input_tokens":3,"cache_read_input_tokens":11,"output_tokens":5}`, Usage{In: 10, CachedIn: 11, Out: 5}, true},
		{"model usage over two models", `"usage":{"input_tokens":1000},"modelUsage":{"claude-a":{"inputTokens":7,"cacheCreationInputTokens":3,"cacheReadInputTokens":11,"outputTokens":5},` +
			`"claude-b":{"inputTokens":1,"cacheCreationInputTokens":1,"cacheReadInputTokens":1,"outputTokens":1}}`, Usage{In: 12, CachedIn: 12, Out: 6}, true},
		{"an empty model usage", `"usage":{"input_tokens":4},"modelUsage":{}`, Usage{In: 4}, true},
		{"counts that are none", `"usage":{"input_tokens":-5,"cache_read_input_tokens":"9","output_tokens":1.5}`, Usage{}, true},
		{"usage that is no object", `"usage":[1,2]`, Usage{}, false},
		{"no usage", "", Usage{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := claudeRead(claudeEnvelopeWith(tc.members), exit{started: true})
			if res.class != ClassOK || res.usage != tc.want || res.usageSeen != tc.seen {
				t.Fatalf("result %+v, want usage %+v seen %v", res, tc.want, tc.seen)
			}
		})
	}
}

// Where an organization manages MCP servers, claude refuses
// --strict-mcp-config. The call retries once without the flag, under its
// one deadline, and says so; it never retries that refusal twice.
func TestClaudeManagedMCPConfigRetriesWithoutTheStrictFlag(t *testing.T) {
	refusals := map[string]map[string]string{
		"on stderr":       {"stderr.1": claudeStrictRefusal, "exit.1": "1"},
		"in the envelope": {"stdout.1": `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["` + claudeStrictRefusal + `"]}`, "exit.1": "1"},
		// Each of errors is read apart, so an MCP server's words beside the
		// refusal neither hide it nor name a sign-in problem.
		"in errors beside an MCP server's words": {"stdout.1": `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["` + claudeStrictRefusal +
			`","MCP server docs: authentication required"]}`, "exit.1": "1"},
	}
	for name, files := range refusals {
		t.Run("retried once "+name, func(t *testing.T) {
			files["stdout.2"] = claudeSuccess
			out, dump := claudeRun(t, files, nil)
			if n := runs(t, dump); n != 2 || out.Attempts != 2 {
				t.Fatalf("ran %d times, %d attempts reported, want 2", n, out.Attempts)
			}
			if first, second := argsOf(t, dump, 1), argsOf(t, dump, 2); !slices.Contains(first, "--strict-mcp-config") || slices.Contains(second, "--strict-mcp-config") {
				t.Fatalf("attempts %q then %q", first, second)
			}
			if !slices.Equal(argsOf(t, dump, 2), slices.DeleteFunc(argsOf(t, dump, 1), func(a string) bool { return a == "--strict-mcp-config" })) {
				t.Fatal("the second attempt differs from the first by more than the flag")
			}
			if out.Class != ClassOK || string(out.Answer) != answer || !slices.Equal(out.Signals, []Signal{SignalClaudeStrictMCPRefused, SignalModelUnreported}) {
				t.Fatalf("outcome %+v", out)
			}
		})
	}
	t.Run("not twice", func(t *testing.T) {
		out, dump := claudeRun(t, map[string]string{"stderr": claudeStrictRefusal, "exit": "1"}, nil)
		if n := runs(t, dump); n != 2 || out.Class != ClassHost || out.Attempts != 2 || !slices.Contains(out.Signals, SignalClaudeStrictMCPRefused) {
			t.Fatalf("ran %d times: %+v; want 2 runs and host", n, out)
		}
	})
	// A refusal beside a sign-in problem is the sign-in problem: no retry.
	t.Run("not beside another failure", func(t *testing.T) {
		out, dump := claudeRun(t, map[string]string{"stderr": claudeStrictRefusal + "\nNot logged in", "exit": "1"}, nil)
		if n := runs(t, dump); n != 1 || out.Class != ClassAuth || slices.Contains(out.Signals, SignalClaudeStrictMCPRefused) {
			t.Fatalf("ran %d times: %+v", n, out)
		}
	})
}

// The attempts share the call's one deadline: the retry without the refused
// flag gets what the first attempt left, and none once it has passed. The
// test ends the time limits itself, so the machine's speed decides nothing.
func TestClaudeAttemptsShareTheDeadline(t *testing.T) {
	isolate(t)
	// The first attempt is refused at once and the second runs until its
	// time ends; only a third would succeed.
	dump := fakeCLI(t, `if [ "$n" = 2 ]; then : > "$dump/second"; exec sleep 300; fi`)
	put(t, dump, "stderr", claudeStrictRefusal)
	put(t, dump, "exit", "1")
	put(t, dump, "stdout.3", claudeSuccess)
	put(t, dump, "exit.3", "0")
	r := claudeRunner(t, Options{})
	c := call(t, adapter.ClaudeCode)
	c.Deadline = time.Minute
	ctx, timeouts := holdTimeouts(context.Background())
	done := launch(t, ctx, r, c)
	if first, second := timeouts.next(t), timeouts.next(t); first != c.Deadline || second >= first {
		t.Fatalf("attempts' limits %s then %s, want %s then less", first, second, c.Deadline)
	}
	if !waitFile(dump, "second") {
		t.Fatalf("the second attempt didn't start in %s", hangGuard)
	}
	timeouts.expire()
	out, err := await(t, done, "a second attempt whose time ended")
	if err != nil {
		t.Fatal(err)
	}
	if n := runs(t, dump); n != 2 {
		t.Fatalf("ran %d times, want 2", n)
	}
	if out.Class != ClassTimeout || out.Attempts != 2 || out.Answer != nil || out.ModelAsked != "claude-test-model" || out.ModelAnswered != "" {
		t.Fatalf("outcome %+v, want a timeout on the second attempt", out)
	}
	// A deadline of a nanosecond has passed by the time the first attempt
	// is refused; the held limit let that attempt finish, and no second one
	// starts.
	for _, file := range []string{"count", "extra"} {
		if err := os.Remove(filepath.Join(dump, file)); err != nil {
			t.Fatal(err)
		}
	}
	c = call(t, adapter.ClaudeCode)
	c.Deadline = time.Nanosecond
	ctx, _ = holdTimeouts(context.Background())
	out, err = await(t, launch(t, ctx, r, c), "a call refused after its deadline passed")
	if err != nil || out.Class != ClassTimeout || out.Attempts != 1 || out.Answer != nil {
		t.Fatalf("outcome %+v %v, want a timeout after the first attempt", out, err)
	}
	if n := runs(t, dump); n != 1 {
		t.Fatalf("ran %d times with the deadline passed, want 1", n)
	}
}

// An envelope printed before the time ends doesn't count while the CLI is
// still running, but its usage and models do. The test ends the run's time
// once the fake has printed it.
func TestClaudeAnswerBeforeTheTimeoutStillTimesOut(t *testing.T) {
	printed := claudeEnvelopeWith(`"usage":{"input_tokens":120,"cache_creation_input_tokens":30,"cache_read_input_tokens":50,"output_tokens":40},"permission_denials":[]`)
	for _, tc := range []struct {
		name, stdout string
		class        Class
		usage        Usage
		models       []string
	}{
		{"an answer", printed, ClassTimeout, claudeSuccessUsage, nil},
		{"an answer with models", claudeEnvelopeWith(`"modelUsage":{"claude-a":{"inputTokens":9,"outputTokens":4}}`), ClassTimeout, Usage{In: 9, Out: 4}, []string{"claude-a"}},
		// A refused tool call taints the run whatever ended it.
		{"a denial", strings.Replace(printed, `"permission_denials":[]`, `"permission_denials":[{}]`, 1), ClassToolUsed, claudeSuccessUsage, nil},
		{"half an envelope", printed[:len(printed)/2], ClassTimeout, Usage{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			// The fake marks the file after it prints the envelope.
			dump := fakeCLI(t, `: > "$dump/printed"; exec sleep 300`)
			put(t, dump, "stdout", tc.stdout)
			ctx, timeouts := holdTimeouts(context.Background())
			done := launch(t, ctx, claudeRunner(t, Options{}), call(t, adapter.ClaudeCode))
			if d := timeouts.next(t); d != defaultDeadline {
				t.Fatalf("the run's limit is %s, want %s", d, defaultDeadline)
			}
			if !waitFile(dump, "printed") {
				t.Fatalf("the fake printed no envelope in %s", hangGuard)
			}
			timeouts.expire()
			out, err := await(t, done, "a run whose time ended")
			if err != nil || out.Class != tc.class || out.Answer != nil || out.Attempts != 1 {
				t.Fatalf("outcome %+v %v, want %s", out, err, tc.class)
			}
			if out.Usage != tc.usage || out.UsageSeen != (tc.usage != Usage{}) || !slices.Equal(out.ModelsSeen, tc.models) {
				t.Fatalf("usage %+v (seen %v), models %q", out.Usage, out.UsageSeen, out.ModelsSeen)
			}
		})
	}
}

// A refused model, and a refused effort, are one attempt: host, with Refused
// set. There is no ladder of efforts or models.
func TestClaudeModelOrEffortRefusedIsOneAttempt(t *testing.T) {
	type refusal struct{ name, stdout, effort, refused string }
	cases := []refusal{
		{"model", claudeRejected, "", RefusedModel},
		{"model with an effort", claudeRejected, "max", RefusedModel},
		{"model by status", `{"type":"result","subtype":"success","is_error":true,"api_error_status":404,"result":"x"}`, "high", RefusedModel},
		{"effort by its quoted value", `{"type":"result","subtype":"success","is_error":true,"result":"model claude-test-model: 'high' is not supported"}`, "high", RefusedEffort},
	}
	// Every level refused is refused once, never stepped down.
	for _, effort := range claudeEfforts {
		cases = append(cases, refusal{"effort " + effort, claudeEffortRejected(effort), effort, RefusedEffort})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, dump := claudeRun(t, map[string]string{"stdout": tc.stdout, "exit": "1", "stdout.2": claudeSuccess, "exit.2": "0"}, func(c *Call) { c.Effort = tc.effort })
			if out.Class != ClassHost || out.Refused != tc.refused || out.Answer != nil || out.Attempts != 1 || runs(t, dump) != 1 {
				t.Fatalf("outcome %+v after %d runs, want host refusing %s after 1", out, runs(t, dump), tc.refused)
			}
			if args := argsOf(t, dump, 1); !claudeHasPair(args, "--model", "claude-test-model") || tc.effort != "" && !claudeHasPair(args, "--effort", tc.effort) {
				t.Fatalf("arguments %q", args)
			}
		})
	}
}

// What the model wrote, in result without is_error, never names a failure:
// only claude's own error text does. Nor does stdout with a brace, which may
// hold an envelope's model text.
func TestClaudeModelTextNeverClassifies(t *testing.T) {
	for _, words := range []string{"rate limit", "Not logged in", "Please sign in again", "API Error: 503", "There's an issue with the selected model",
		"cannot use --strict-mcp-config"} {
		t.Run(words, func(t *testing.T) {
			envelope := `{"type":"result","is_error":false,"result":"` + words + `"`
			success := envelope + `,"subtype":"success"}`
			for _, tc := range []struct {
				files map[string]string
				class Class
			}{
				{map[string]string{"stdout": success, "exit": "1"}, ClassHost},
				{map[string]string{"stdout": envelope + `,"subtype":"error_during_execution"}`}, ClassHost},
				{map[string]string{"stdout": success}, ClassAnswerInvalid},
				{map[string]string{"stdout": envelope + `,"subtype":"success","structured_output":{"summary":"done"}}`}, ClassOK},
				{map[string]string{"stdout": envelope + `,"subtype":"success","structured_output":{"summary":"done"}}`, "exit": "1"}, ClassHost},
				{map[string]string{"stdout": "notice " + success, "exit": "1"}, ClassHost},
				{map[string]string{"stdout": `{"type":"assistant","text":"` + words + `"}`, "exit": "1"}, ClassHost},
				{map[string]string{"stdout": success + "\n" + success, "exit": "1"}, ClassHost},
			} {
				out, dump := claudeRun(t, tc.files, nil)
				if out.Class != tc.class || out.Refused != "" || runs(t, dump) != 1 {
					t.Fatalf("files %q: outcome %+v after %d runs, want %s", tc.files, out, runs(t, dump), tc.class)
				}
			}
		})
	}
}

// A marker shaped like a name, planted where claude writes text: result,
// with is_error and without, errors, api_error, a denial's input, stderr,
// fields this reader doesn't read, a model's own fields, and plain stdout,
// reaches no field of the outcome, the models included.
func TestClaudePlantedMarkersReachNoField(t *testing.T) {
	for _, marker := range []string{
		"planted-marker-41", "planted.marker@example.invalid", "/Users/planted/client-project", "0b1e8a3c-1111-4222-8333-444455556666",
	} {
		q := `"` + marker + `"`
		for _, tc := range []struct {
			place string
			files map[string]string
		}{
			{"result", map[string]string{"stdout": claudeEnvelopeWith(`"result":` + q + `,"model":` + q + `,"session_id":` + q + `,"uuid":` + q)}},
			{"result with is_error", map[string]string{"exit": "1", "stdout": `{"type":"result","subtype":"success","is_error":true,"result":"model ` + marker + ` not found","model":` + q + `}`}},
			{"errors", map[string]string{"exit": "1", "stdout": `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":[` + q + `,{"model":` + q + `}],"api_error":{"model":` + q + `}}`}},
			{"a denial's input", map[string]string{"stdout": claudeEnvelopeWith(`"permission_denials":[{"tool_name":` + q + `,"tool_use_id":` + q + `,"tool_input":{"model":` + q + `,"path":` + q + `}}]`)}},
			{"stderr", map[string]string{"exit": "1", "stderr": marker + "\nmodel " + marker + " is unknown\n" + claudeStrictRefusal + " " + marker}},
			{"plain stdout", map[string]string{"exit": "1", "stdout": marker + "\n"}},
			{"usage", map[string]string{"stdout": claudeEnvelopeWith(`"usage":{"model":` + q + `,"input_tokens":5},"modelUsage":{"claude-a":{"model":` + q + `,"outputTokens":1,"name":` + q + `}}`)}},
			{"an envelope's type", map[string]string{"stdout": `{"type":` + q + `,"modelUsage":{"x":1},"model":` + q + `}`}},
			{"a subtype", map[string]string{"stdout": claudeEnvelopeWith(`"subtype":` + q)}},
		} {
			t.Run(tc.place+" "+marker, func(t *testing.T) {
				out, _ := claudeRun(t, tc.files, nil)
				if out.ModelAnswered != "" && out.ModelAnswered != "claude-a" || strings.Contains(fmt.Sprintf("%#v", out), marker) {
					t.Fatalf("the marker reached the outcome: %#v", out)
				}
				for _, m := range out.ModelsSeen {
					if m != "claude-a" {
						t.Fatalf("model %q", m)
					}
				}
				for _, s := range out.Signals {
					if !slices.Contains([]Signal{SignalModelUnreported, SignalClaudeStrictMCPRefused, SignalClaudeSeveralModels, SignalClaudeModelUnnamed}, s) {
						t.Fatalf("signal %q", s)
					}
				}
			})
		}
	}
}

// The envelope's reader on its own: a field's value nested past
// encoding/json's limit, or stdout past the cap, is no envelope and costs no
// stack; at the limit a value is read.
func TestClaudeDeepAndHugeEnvelopes(t *testing.T) {
	for _, tc := range []struct {
		name, stdout string
	}{
		{"brackets to the cap", strings.Repeat("[", claudeMaxEnvelope)},
		{"objects to the cap", strings.Repeat(`{"a":`, claudeMaxEnvelope/5)},
		{"a field past the depth", claudeEnvelopeWith(`"x":` + strings.Repeat("[", claudeJSONDepth+1) + strings.Repeat("]", claudeJSONDepth+1))},
		{"the answer past the depth", `{"type":"result","subtype":"success","is_error":false,"structured_output":{"a":` +
			strings.Repeat("[", claudeJSONDepth) + strings.Repeat("]", claudeJSONDepth) + `}}`},
		{"a denial past the depth", claudeEnvelopeWith(`"permission_denials":` + strings.Repeat("[", claudeJSONDepth+1) + strings.Repeat("]", claudeJSONDepth+1))},
		{"past the cap", claudeEnvelopeWith(`"pad":"` + strings.Repeat("a", claudeMaxEnvelope) + `"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, e := range []exit{{started: true}, {started: true, failed: true}} {
				res := claudeRead(tc.stdout, e)
				if res.class != ClassHost || res.answer != nil || res.toolSign || res.usageSeen || res.models != nil {
					t.Fatalf("exit %+v: %+v", e, res)
				}
			}
		})
	}
	// Just inside both limits, an envelope is read.
	inside := claudeEnvelopeWith(`"x":` + strings.Repeat("[", claudeJSONDepth) + strings.Repeat("]", claudeJSONDepth))
	if res := claudeRead(inside, exit{started: true}); res.class != ClassOK {
		t.Fatalf("a field at the depth: %+v", res)
	}
	big := claudeEnvelopeWith(`"pad":""`)
	big = claudeEnvelopeWith(`"pad":"` + strings.Repeat("a", claudeMaxEnvelope-len(big)) + `"`)
	if res := claudeRead(big, exit{started: true}); res.class != ClassOK || len(big) != claudeMaxEnvelope {
		t.Fatalf("an envelope of %d bytes at the cap: %+v", len(big), res)
	}
	// Through the whole run, a deep line ends the run with a class.
	out, _ := claudeRun(t, map[string]string{"stdout": strings.Repeat("[", 3<<20)}, nil)
	if out.Class != ClassHost || out.Answer != nil {
		t.Fatalf("outcome %+v", out)
	}
}

// The backend's own result, apart from the core: a missing CLI reads
// nothing, a timeout keeps no answer, and an attempt after the first never
// asks for another.
func TestClaudeResultApartFromTheCore(t *testing.T) {
	if res := claudeRead(claudeSuccess, exit{missing: true}); res.class != ClassHost || res.answer != nil || res.usageSeen {
		t.Fatalf("a missing CLI: %+v", res)
	}
	if res := claudeRead(claudeSuccess, exit{started: true, failed: true, timedOut: true}); res.answer != nil || res.class == ClassOK || res.usage != claudeSuccessUsage {
		t.Fatalf("a timeout: %+v", res)
	}
	if res := claudeRead(claudeSuccess, exit{started: true}); res.class != ClassOK || string(res.answer) != answer || res.toolSign {
		t.Fatalf("a clean run: %+v", res)
	}
}

// A count of tools the API ran itself, such as a web search, in usage or in
// a model's entry in modelUsage, under its key in any letter case, is a tool
// sign beside any answer, a timeout too; only no key, null, 0 and an object
// of those are none.
func TestClaudeServerToolCountsAreToolUsed(t *testing.T) {
	used := []string{`1`, `2`, `-1`, `0.5`, `"0"`, `"x"`, `true`, `false`, `[]`, `[0]`, `{"web_search_requests":1}`,
		`{"web_search_requests":0,"web_fetch_requests":1}`, `{"a":{"b":1}}`, `{"web_search_requests":"0"}`, `{"x":[]}`}
	none := []string{`0`, `null`, `0.0`, `{}`, `{"web_search_requests":0,"web_fetch_requests":0}`, `{"a":{"b":null}}`}
	usageKeys := []string{"server_tool_use", "Server_Tool_Use", "serverToolUse", "web_search_requests", "web_fetch_requests"}
	modelKeys := []string{"webSearchRequests", "webFetchRequests", "WEBSEARCHREQUESTS", "server_tool_use", "serverToolUse"}
	envelopes := func(v string) []string {
		var out []string
		for _, k := range usageKeys {
			for _, usage := range []string{"usage", "Usage"} {
				out = append(out, claudeEnvelopeWith(`"`+usage+`":{"input_tokens":1,"output_tokens":1,"`+k+`":`+v+`}`))
			}
		}
		for _, k := range modelKeys {
			out = append(out,
				claudeEnvelopeWith(`"modelUsage":{"claude-a":{"outputTokens":3,"`+k+`":`+v+`}}`),
				claudeEnvelopeWith(`"modelUsage":{"claude-a":{"outputTokens":3},"claude-b":{"outputTokens":1,"`+k+`":`+v+`}}`),
				claudeEnvelopeWith(`"modelUsage":{"claude-a":{"outputTokens":3}},"ModelUsage":{"claude-b":{"`+k+`":`+v+`}}`))
		}
		return out
	}
	for _, v := range used {
		for _, stdout := range envelopes(v) {
			for _, e := range []exit{{started: true}, {started: true, failed: true}, {started: true, failed: true, timedOut: true}} {
				if res := claudeRead(stdout, e); !res.toolSign || !e.timedOut && res.class != ClassToolUsed || res.answer != nil {
					t.Fatalf("%s, exit %+v: %+v", stdout, e, res)
				}
			}
		}
	}
	for _, v := range none {
		for _, stdout := range envelopes(v) {
			if res := claudeRead(stdout, exit{started: true}); res.toolSign || res.class == ClassToolUsed {
				t.Fatalf("%s: %+v", stdout, res)
			}
		}
	}
	// An envelope in the shape claude prints, its counts at none, is ok;
	// one with a web search, through the whole run, is tool_used.
	zero := claudeEnvelopeWith(`"usage":{"input_tokens":5,"output_tokens":2,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0}},` +
		`"modelUsage":{"claude-a":{"inputTokens":5,"outputTokens":2,"webSearchRequests":0}}`)
	if out, _ := claudeRun(t, map[string]string{"stdout": zero}, nil); out.Class != ClassOK || string(out.Answer) != answer {
		t.Fatalf("no server tool: %+v", out)
	}
	searched := strings.Replace(zero, `"webSearchRequests":0`, `"webSearchRequests":2`, 1)
	if out, _ := claudeRun(t, map[string]string{"stdout": searched}, nil); out.Class != ClassToolUsed || out.Answer != nil || out.Usage != (Usage{In: 5, Out: 2}) {
		t.Fatalf("a web search: %+v", out)
	}
}

// modelUsage gives at most claudeMaxModels names: past that the envelope is
// host, with no model kept and none said to answer, though every entry's
// usage and tool counts are still read. The reader's time grows with the
// envelope, not with its square: an envelope near the cap holding hundreds
// of thousands of names returns, and the bound holds.
func TestClaudeManyModels(t *testing.T) {
	models := func(n int, last string) string {
		var b strings.Builder
		for i := range n {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `"m%07d":{"outputTokens":1}`, i)
		}
		if last != "" {
			b.WriteString("," + last)
		}
		return claudeEnvelopeWith(`"modelUsage":{` + b.String() + `}`)
	}
	at := claudeRead(models(claudeMaxModels, ""), exit{started: true})
	if at.class != ClassOK || len(at.models) != claudeMaxModels || at.answered != "m0000000" || at.usage.Out != claudeMaxModels {
		t.Fatalf("%d models: class %s, %d models, answered %q, usage %+v", claudeMaxModels, at.class, len(at.models), at.answered, at.usage)
	}
	huge := models(250000, "")
	if len(huge) > claudeMaxEnvelope {
		t.Fatalf("an envelope of %d bytes is past the cap", len(huge))
	}
	for _, tc := range []struct {
		name, stdout string
		n            int64
		class        Class
	}{
		{"one past the bound", models(claudeMaxModels+1, ""), claudeMaxModels + 1, ClassHost},
		{"near the cap", huge, 250000, ClassHost},
		{"near the cap, a web search last", models(250000, `"m9999999":{"outputTokens":1,"webSearchRequests":1}`), 250001, ClassToolUsed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := claudeRead(tc.stdout, exit{started: true})
			if res.class != tc.class || res.models != nil || res.answered != "" || res.answer != nil ||
				!res.usageSeen || res.usage.Out != tc.n || !slices.Equal(res.signals, []Signal{SignalClaudeModelUnnamed, SignalClaudeSeveralModels}) {
				t.Fatalf("class %s, %d models, answered %q, usage %+v, signals %q", res.class, len(res.models), res.answered, res.usage, res.signals)
			}
		})
	}
	// Through the whole run, the core's own reading included.
	out, _ := claudeRun(t, map[string]string{"stdout": huge}, nil)
	if out.Class != ClassHost || out.ModelsSeen != nil || out.ModelAnswered != "" || out.Usage.Out != 250000 ||
		!slices.Equal(out.Signals, []Signal{SignalClaudeModelUnnamed, SignalClaudeSeveralModels, SignalModelUnreported}) {
		t.Fatalf("class %s, %d models, answered %q, usage %+v, signals %q", out.Class, len(out.ModelsSeen), out.ModelAnswered, out.Usage, out.Signals)
	}
}

// What an envelope this reader can't vouch for says it spent is kept, the
// larger reading where a count is given twice, so a cost is never lost to
// an odd key.
func TestClaudeOddEnvelopesKeepTheirUsage(t *testing.T) {
	for _, tc := range []struct {
		name, members string
		want          Usage
	}{
		{"an unknown key twice", `"usage":{"input_tokens":100,"output_tokens":50},"session_id":"a","session_id":"a"`, Usage{In: 100, Out: 50}},
		{"usage twice", `"usage":{"input_tokens":100},"usage":{"input_tokens":7,"output_tokens":9}`, Usage{In: 100, Out: 9}},
		{"a count twice", `"usage":{"output_tokens":40,"output_tokens":1}`, Usage{Out: 40}},
		{"usage in another case", `"Usage":{"input_tokens":3,"cache_read_input_tokens":4}`, Usage{In: 3, CachedIn: 4}},
		{"is_error of another type", `"is_error":"no","usage":{"output_tokens":6}`, Usage{Out: 6}},
		{"a model's count twice", `"modelUsage":{"claude-a":{"outputTokens":2,"outputTokens":8}}`, Usage{Out: 8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout := `{"type":"result","subtype":"success",` + tc.members + `,"structured_output":{"summary":"done"}}`
			out, _ := claudeRun(t, map[string]string{"stdout": stdout}, nil)
			if out.Class != ClassHost || out.Answer != nil || out.Usage != tc.want || !out.UsageSeen || out.ModelAnswered != "" {
				t.Fatalf("outcome %+v, want host with usage %+v", out, tc.want)
			}
		})
	}
}

// With no envelope and a failed exit, stdout with a brace is never read as
// error text: an envelope that could not be read, printed over several
// lines, holds the model's own words on lines of their own.
func TestClaudeUnreadEnvelopeLinesNeverClassify(t *testing.T) {
	pretty := "{\n  \"type\": \"Result\",\n  \"subtype\": \"success\",\n  \"is_error\": false,\n  \"result\": \"Claude AI usage limit reached\",\n" +
		"  \"note\": \"Not logged in\"\n}\n"
	for _, stdout := range []string{pretty, pretty + pretty, "notice\n" + pretty,
		"notice " + `{"type":"result","subtype":"success","is_error":false,"result":"rate limit"}`} {
		out, _ := claudeRun(t, map[string]string{"stdout": stdout, "exit": "1"}, nil)
		if out.Class != ClassHost || out.Refused != "" {
			t.Fatalf("%q: outcome %+v", stdout, out)
		}
	}
}
