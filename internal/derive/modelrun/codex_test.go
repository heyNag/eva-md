package modelrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/host/hostfake"
)

// The Codex backend's tests, run through New on the rig's script. Every
// stream here is made up in the shape codex 0.166.0's --json events take.

// codexClean is a clean run's stream: 500 tokens in, 100 of them cached, 60
// out.
const codexClean = `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000000"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"Reading the batch"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"summary\":\"done\"}"}}
{"type":"turn.completed","usage":{"input_tokens":500,"cached_input_tokens":100,"output_tokens":60}}
`

// codexCleanUsage is codexClean's usage, its cached tokens out of In.
var codexCleanUsage = Usage{In: 400, CachedIn: 100, Out: 60}

// codexRejected is a stream for a model the account can't use.
const codexRejected = `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000001"}
{"type":"turn.started"}
{"type":"error","message":"unexpected status 400 Bad Request: {\"detail\":\"The 'gpt-test-model' model is not supported when using Codex with a ChatGPT account.\"}"}
{"type":"turn.failed","error":{"message":"unexpected status 400 Bad Request: The 'gpt-test-model' model is not supported when using Codex with a ChatGPT account."}}
`

// codexEffortRejected is a stream for an effort the model can't take. Codex
// quotes the refused value and doesn't say "effort".
func codexEffortRejected(effort string) string {
	refusal := `Unsupported value: '` + effort + `' is not supported with the 'gpt-test-model' model.`
	return `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000003"}
{"type":"turn.started"}
{"type":"error","message":"unexpected status 400 Bad Request: {\"detail\":\"` + refusal + `\"}"}
{"type":"turn.failed","error":{"message":"unexpected status 400 Bad Request: ` + refusal + `"}}
`
}

// codexItem is an item event of the given type.
func codexItem(event, kind string) string {
	return `{"type":"` + event + `","item":{"id":"item_0","type":"` + kind + `"}}` + "\n"
}

// codexRunner is the registered Codex backend's runner, its probes through
// a counting Env.
func codexRunner(t *testing.T, o Options) (Runner, *probeCalls) {
	t.Helper()
	calls, env := testEnv()
	o.Env = env
	r, err := New(adapter.Codex, o)
	if err != nil {
		t.Fatal(err)
	}
	return r, calls
}

// codexRun runs one call on the rig readied by files, a map of the dump's
// file names to their content, and gives the outcome and the dump.
func codexRun(t *testing.T, files map[string]string, edit func(*Call)) (Outcome, string) {
	t.Helper()
	isolate(t)
	dump := fakeCLI(t, "")
	for name, content := range files {
		put(t, dump, name, content)
	}
	r, _ := codexRunner(t, Options{})
	c := call(t, adapter.Codex)
	if edit != nil {
		edit(&c)
	}
	out, err := r.Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return out, dump
}

// codexDisabled is the features a run's arguments disable, in order.
func codexDisabled(args []string) []string {
	var names []string
	for i, a := range args {
		if a == "--disable" && i+1 < len(args) {
			names = append(names, args[i+1])
		}
	}
	return names
}

// withUnreported is a run's signals as the core gives them: the backend's,
// then model_unreported, since codex names no model.
func withUnreported(signals ...Signal) []Signal {
	return append(signals, SignalModelUnreported)
}

func TestCodexArgumentsMatchTheDesign(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "output", answer)
	put(t, dump, "stdout", codexClean)
	// The run disables the features codex features list names, and no others.
	put(t, dump, "features", "hooks  stable  true\nmulti_agent  stable  false\nview_image  stable  true\n")
	r, calls := codexRunner(t, Options{CLI: ownCLI(t, adapter.Codex)})
	c := call(t, adapter.Codex)
	c.Effort, c.IgnoreUserConfig = "ultra", true
	out, err := r.Run(context.Background(), c)
	if err != nil || out.Class != ClassOK || string(out.Answer) != answer {
		t.Fatalf("run: %+v %v", out, err)
	}
	got := argsOf(t, dump, 1)
	i := slices.Index(got, "--output-schema")
	if i < 0 || i+3 >= len(got) {
		t.Fatalf("no schema or output file in %q", got)
	}
	schema, output := got[i+1], got[i+3]
	want := []string{"--no-daemon", "exec", "--ephemeral", "--json", "-s", "read-only", "--skip-git-repo-check", "-C", c.RunDir,
		"--disable", "hooks", "--disable", "view_image", "--disable", "multi_agent",
		"-c", "web_search=disabled", "-c", "tools.update_plan.enabled=false", "-m", "gpt-test-model", "-c", "model_reasoning_effort=ultra",
		"--ignore-user-config", "--output-schema", schema, "-o", output, "-"}
	if !slices.Equal(got, want) {
		t.Fatalf("arguments\n got %q\nwant %q", got, want)
	}
	if read(t, dump, "schema.1") != string(c.Schema) {
		t.Fatal("the schema file does not hold the call's schema")
	}
	// The instructions lead stdin, the input follows, and neither is an
	// argument.
	if got, want := read(t, dump, "stdin.1"), c.System+"\n\n"+string(c.Input); got != want {
		t.Fatalf("stdin %q, want %q", got, want)
	}
	for _, a := range got {
		if strings.Contains(a, batchMarker) || strings.Contains(a, "Fixed rules") {
			t.Fatalf("an argument holds the input or the instructions: %q", a)
		}
	}
	// The schema and answer sit in a private folder outside the run folder,
	// removed after the run; the run folder stays empty.
	dir := filepath.Dir(schema)
	if filepath.Dir(output) != dir || strings.HasPrefix(dir, c.RunDir) {
		t.Fatalf("schema %s and output %s", schema, output)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private folder left behind: %v", err)
	}
	if entries, err := os.ReadDir(c.RunDir); err != nil || len(entries) != 0 {
		t.Fatalf("run folder holds %d entries: %v", len(entries), err)
	}
	real, err := filepath.EvalSymlinks(c.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(read(t, dump, "pwd.1")); got != real {
		t.Fatalf("working folder %s, want %s", got, real)
	}
	// A second run, with no effort and the user's config: the features
	// list is not read again, though it changed, and its own private folder
	// holds the answer.
	put(t, dump, "features", "hooks  stable  true\n")
	c = call(t, adapter.Codex)
	if out, err := r.Run(context.Background(), c); err != nil || out.Class != ClassOK {
		t.Fatalf("second run: %+v %v", out, err)
	}
	second := argsOf(t, dump, 2)
	if slices.Contains(second, "--ignore-user-config") || slices.ContainsFunc(second, func(a string) bool { return strings.HasPrefix(a, "model_reasoning_effort") }) {
		t.Fatalf("second run arguments %q", second)
	}
	if got := codexDisabled(second); !slices.Equal(got, []string{"hooks", "view_image", "multi_agent"}) {
		t.Fatalf("second run disabled %q", got)
	}
	if calls.count("features list") != 1 || calls.count("--version") != 1 {
		t.Fatalf("two runs probed %q", calls.calls)
	}
	j := slices.Index(second, "--output-schema")
	if j < 0 || j+1 >= len(second) || filepath.Dir(second[j+1]) == dir {
		t.Fatalf("the second run shares the first's private folder: %q", second)
	}
}

// A run disables each tool feature codex features list names, on or off,
// and no name the list lacks; a feature that gives no tool stays as it is.
// A list that can't be read, or holds no feature, leaves the fixed five, and
// is read again by the next probe.
func TestCodexDisablesTheToolFeaturesCodexLists(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "output", answer)
	put(t, dump, "stdout", codexClean)
	put(t, dump, "features", `hooks                                    stable             true
plugins                                  stable             true
memories                                 stable             false
shell_tool                               stable             true
unified_exec                             stable             true
view_image                               stable             true
skill_search                             stable             true
code_mode                                under development  false
code_mode_host                           stable             true
computer_use                             stable             true
browser_use                              stable             false
deferred_executor                        under development  false
secret_auth_storage                      stable             false
guardianv2.thread_context                removed            false
a notice that is not a feature
`)
	r, _ := codexRunner(t, Options{CLI: ownCLI(t, adapter.Codex)})
	if out, err := r.Run(context.Background(), call(t, adapter.Codex)); err != nil || out.Class != ClassOK {
		t.Fatalf("run: %+v %v", out, err)
	}
	if got := read(t, dump, "features.args"); got != "features\x00list\x00" {
		t.Fatalf("features list arguments %q", got)
	}
	// apps is not listed, so it is not passed.
	want := []string{"hooks", "plugins", "memories", "shell_tool", "view_image", "code_mode", "code_mode_host", "deferred_executor", "browser_use", "computer_use"}
	if got := codexDisabled(argsOf(t, dump, 1)); !slices.Equal(got, want) {
		t.Fatalf("disabled %q, want %q", got, want)
	}
	n := 1
	for _, list := range []string{"", "Error: features are unavailable\n", "-"} {
		if list == "-" {
			if err := os.Remove(filepath.Join(dump, "features")); err != nil {
				t.Fatal(err)
			}
		} else {
			put(t, dump, "features", list)
		}
		r, calls := codexRunner(t, Options{CLI: ownCLI(t, adapter.Codex)})
		for range 2 {
			n++
			if out, err := r.Run(context.Background(), call(t, adapter.Codex)); err != nil || out.Class != ClassOK {
				t.Fatalf("list %q: %+v %v", list, out, err)
			}
			if got := codexDisabled(argsOf(t, dump, n)); !slices.Equal(got, codexOff) {
				t.Fatalf("list %q: disabled %q, want %q", list, got, codexOff)
			}
		}
		if calls.count("features list") != 2 {
			t.Fatalf("list %q, which gives no feature, was read %d times by two runs, want 2", list, calls.count("features list"))
		}
	}
}

// codex features list ends at the probe step's limit, and a run whose list
// hangs goes on with the fixed five. The test ends that limit once the fake
// lists, so the machine's speed decides nothing.
func TestCodexFeaturesListThatHangsFallsBack(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "output", answer)
	put(t, dump, "stdout", codexClean)
	put(t, dump, "features.sh", `: > "$dump/listing"; exec sleep 300`)
	r, _ := codexRunner(t, Options{CLI: ownCLI(t, adapter.Codex)})
	ctx, step := holdProbeStep(context.Background())
	done := launch(t, ctx, r, call(t, adapter.Codex))
	if d := step.next(t); d != probeStepTimeout {
		t.Fatalf("the list's limit is %s, want %s", d, probeStepTimeout)
	}
	if !waitFile(dump, "listing") {
		t.Fatalf("the fake didn't list in %s", hangGuard)
	}
	step.expire()
	out, err := await(t, done, "a run whose features list hung")
	if err != nil || out.Class != ClassOK {
		t.Fatalf("outcome %+v %v", out, err)
	}
	if got := codexDisabled(argsOf(t, dump, 1)); !slices.Equal(got, codexOff) {
		t.Fatalf("disabled %q, want %q", got, codexOff)
	}
}

func TestCodexStreams(t *testing.T) {
	const (
		threadStarted = `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000002"}` + "\n"
		turnStarted   = `{"type":"turn.started"}` + "\n"
		message       = `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"summary\":\"done\"}"}}` + "\n"
		completed     = `{"type":"turn.completed","usage":{"input_tokens":500,"cached_input_tokens":100,"output_tokens":60}}` + "\n"
	)
	cases := []struct {
		name, stream, stderr, output string
		exit                         int
		class                        Class
		signals                      []Signal
		usage                        Usage
	}{
		{name: "clean", stream: codexClean, output: answer, class: ClassOK, usage: codexCleanUsage},
		{name: "no usage", stream: threadStarted + turnStarted + message, output: answer, class: ClassOK},
		{name: "command execution", stream: threadStarted + turnStarted +
			`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"cat notes","status":"in_progress"}}` + "\n" + message + completed,
			output: answer, class: ClassToolUsed},
		{name: "file change", stream: `{"type":"item.completed","item":{"id":"item_0","type":"file_change","changes":[]}}` + "\n", output: answer, class: ClassToolUsed},
		{name: "MCP tool call", stream: `{"type":"item.started","item":{"id":"item_0","type":"mcp_tool_call","server":"docs","tool":"search"}}` + "\n", output: answer, class: ClassToolUsed},
		{name: "web search", stream: `{"type":"item.started","item":{"id":"item_0","type":"web_search","query":"release"}}` + "\n", output: answer, class: ClassToolUsed},
		// Every run turns the plan tool off, so its item means the switch
		// didn't hold.
		{name: "plan item", stream: threadStarted + turnStarted +
			`{"type":"item.started","item":{"id":"item_0","type":"todo_list","items":[{"text":"Read the batch","completed":false}]}}` + "\n" + message + completed,
			output: answer, class: ClassToolUsed},
		// An item or an event this reader doesn't know is the host's, not
		// the chat's, and stops the run before its usage is read.
		{name: "unknown item type", stream: threadStarted + turnStarted + codexItem("item.started", "browser_session") + message + completed,
			output: answer, class: ClassHost},
		{name: "item without a type", stream: `{"type":"item.completed","item":{"id":"item_0"}}` + "\n" + completed, output: answer, class: ClassHost},
		{name: "item event without an item", stream: `{"type":"item.completed"}` + "\n" + completed, output: answer, class: ClassHost},
		{name: "unknown event type", stream: threadStarted + `{"type":"turn.plan_updated","plan":[]}` + "\n" + message + completed,
			output: answer, class: ClassHost},
		{name: "event without a type", stream: threadStarted + `{"thread_id":"x"}` + "\n" + completed, output: answer, class: ClassHost},
		{name: "null event", stream: threadStarted + "null\n" + completed, output: answer, class: ClassHost},
		{name: "tool item before an unknown event", stream: threadStarted +
			`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"cat notes"}}` + "\n" +
			`{"type":"thread.compacted"}` + "\n" + message + completed,
			output: answer, class: ClassToolUsed},
		{name: "unknown event before a tool item", stream: threadStarted + `{"type":"thread.compacted"}` + "\n" +
			codexItem("item.started", "command_execution") + completed,
			output: answer, class: ClassHost},
		// A tool where no tool belongs is a tool all the same.
		{name: "tool item on a known event", stream: threadStarted +
			`{"type":"turn.completed","usage":{"input_tokens":5},"item":{"type":"command_execution"}}` + "\n",
			output: answer, class: ClassToolUsed},
		{name: "tool nested in a message", stream: threadStarted +
			`{"type":"item.completed","item":{"type":"agent_message","text":"x","parts":[{"type":"file_change"}]}}` + "\n" + completed,
			output: answer, class: ClassToolUsed},
		{name: "a key twice", stream: threadStarted +
			`{"type":"item.completed","item":{"type":"agent_message","type":"reasoning"}}` + "\n" + completed,
			output: answer, class: ClassHost},
		{name: "a tool key twice", stream: threadStarted +
			`{"type":"item.completed","item":{"type":"command_execution","type":"agent_message"}}` + "\n" + completed,
			output: answer, class: ClassToolUsed},
		{name: "tool type in another case", stream: threadStarted + codexItem("item.started", "Command_Execution") + completed,
			output: answer, class: ClassToolUsed},
		// A tool named in a message's text is no tool.
		{name: "tool named in text only", stream: threadStarted + turnStarted +
			`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"type\":\"command_execution\"} ran nothing"}}` + "\n" + completed,
			output: answer, class: ClassOK, usage: codexCleanUsage},
		{name: "error item beside valid output", stream: threadStarted + turnStarted +
			`{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Reconnecting... 1/5"}}` + "\n" + message + completed,
			output: answer, class: ClassOK, usage: codexCleanUsage},
		{name: "error event beside valid output", stream: threadStarted + turnStarted +
			`{"type":"error","message":"Reconnecting... 2/5"}` + "\n" + message + completed,
			output: answer, class: ClassOK, usage: codexCleanUsage},
		{name: "optional MCP failure beside valid output", stream: threadStarted +
			`{"type":"item.completed","item":{"id":"item_0","type":"error","message":"MCP client for ` + "`docs`" + ` failed to start: handshaking with MCP server failed"}}` + "\n" +
			turnStarted + message + completed,
			output: answer, class: ClassOK, signals: []Signal{SignalCodexMCPFailed}, usage: codexCleanUsage},
		{name: "optional MCP timeout on stderr", stream: codexClean, stderr: "MCP client for `docs` timed out after 10 seconds.\n",
			output: answer, class: ClassOK, signals: []Signal{SignalCodexMCPFailed}, usage: codexCleanUsage},
		{name: "required MCP failure", stderr: "Error: required MCP servers failed to initialize: probe\n", exit: 1, class: ClassHost},
		// An MCP server's own words never name codex's failure.
		{name: "MCP server's sign-in words", stderr: "MCP server `docs` failed: authentication required, please log in\n", exit: 1, class: ClassHost},
		{name: "authentication", stream: threadStarted + turnStarted +
			`{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized: Your access token could not be refreshed. Please log out and sign in again."}}` + "\n",
			exit: 1, class: ClassAuth},
		{name: "rate limit", stream: threadStarted + turnStarted + `{"type":"error","message":"You've hit your usage limit. Try again later."}` + "\n" +
			`{"type":"turn.failed","error":{"message":"usage limit"}}` + "\n",
			exit: 1, class: ClassRateLimit},
		// An error event and no answer is named from its text, though codex
		// exited 0.
		{name: "error event with no answer", stream: threadStarted + `{"type":"error","message":"You've hit your usage limit."}` + "\n", class: ClassRateLimit},
		{name: "error event naming nothing", stream: threadStarted + `{"type":"error","message":"something odd"}` + "\n", class: ClassHost},
		{name: "turn failed despite an answer", stream: threadStarted + turnStarted + `{"type":"turn.failed","error":{"message":"stream disconnected before completion"}}` + "\n",
			output: answer, class: ClassHost},
		{name: "server error", stream: threadStarted + turnStarted +
			`{"type":"turn.failed","error":{"message":"exceeded retry limit, last status: 503 Service Unavailable"}}` + "\n",
			exit: 1, class: ClassHost},
		{name: "an outage naming the model", stream: threadStarted +
			`{"type":"turn.failed","error":{"message":"503 Service Unavailable: the model is not available"}}` + "\n",
			exit: 1, class: ClassHost},
		{name: "usage error after an upgrade", stderr: "error: invalid value 'browser_use' for '--disable <DISABLE>'\n\nFor more information, try '--help'.\n",
			exit: 1, class: ClassHost},
		// A filtered answer is the answer's fault.
		{name: "filtered answer", stream: threadStarted + turnStarted +
			`{"type":"turn.failed","error":{"message":"Incomplete response returned, reason: content_filter"}}` + "\n",
			exit: 1, class: ClassAnswerInvalid},
		// The model's own words never name a failure.
		{name: "model text naming failures", stream: threadStarted + turnStarted +
			`{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"Not logged in. Run codex login"}}` + "\n" +
			`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"we hit the rate limit yesterday"}}` + "\n",
			exit: 1, class: ClassHost},
		{name: "turn failed beside model text naming a limit", stream: threadStarted + turnStarted +
			`{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"usage limit reached"}}` + "\n" +
			`{"type":"turn.failed","error":{"message":"stream disconnected before completion"}}` + "\n",
			exit: 1, class: ClassHost},
		// A notice that is not JSON reports no tool: it is skipped, with a
		// signal.
		{name: "not a JSON line", stream: "Update available: run brew upgrade\n" + codexClean, output: answer,
			class: ClassOK, signals: []Signal{SignalCodexLineSkipped}, usage: codexCleanUsage},
		// A line with a brace that isn't JSON may hide an event.
		{name: "event run together with other text", stream: threadStarted + `notice {"type":"item.started","item":{"id":"item_0","type":"agent_message"}}` + "\n" + message + completed,
			output: answer, class: ClassHost},
		{name: "two events on a line", stream: threadStarted + `{"type":"turn.started"} {"type":"turn.started"}` + "\n" + message + completed,
			output: answer, class: ClassHost},
		{name: "answer missing", stream: codexClean, class: ClassAnswerInvalid, usage: codexCleanUsage},
		{name: "answer not an object", stream: codexClean, output: `["done"]`, class: ClassAnswerInvalid, usage: codexCleanUsage},
		{name: "failed exit", stream: codexClean, output: answer, exit: 1, class: ClassHost, usage: codexCleanUsage},
		{name: "last line without a newline", stream: strings.TrimSuffix(threadStarted+turnStarted+message+completed, "\n"),
			output: answer, class: ClassOK, usage: codexCleanUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"stdout": tc.stream, "stderr": tc.stderr, "exit": strconv.Itoa(tc.exit)}
			if tc.output != "" {
				files["output"] = tc.output
			}
			out, dump := codexRun(t, files, nil)
			if out.Class != tc.class || !slices.Equal(out.Signals, withUnreported(tc.signals...)) {
				t.Fatalf("outcome %+v, want class %s and signals %q", out, tc.class, withUnreported(tc.signals...))
			}
			if out.Usage != tc.usage || out.UsageSeen != (tc.usage != Usage{}) {
				t.Fatalf("usage %+v (seen %v), want %+v", out.Usage, out.UsageSeen, tc.usage)
			}
			if (tc.class == ClassOK) != (string(out.Answer) == answer) || (tc.class != ClassOK && out.Answer != nil) {
				t.Fatalf("answer %q on %s", out.Answer, out.Class)
			}
			if out.Refused != "" || out.ModelAnswered != "" || out.ModelsSeen != nil {
				t.Fatalf("outcome %+v names a refusal or a model", out)
			}
			if n := runs(t, dump); n != 1 || out.Attempts != 1 {
				t.Fatalf("ran %d times, %d attempts reported", n, out.Attempts)
			}
		})
	}
}

// An empty answer file is no answer.
func TestCodexAnEmptyAnswerFileIsNoAnswer(t *testing.T) {
	out, _ := codexRun(t, map[string]string{"stdout": codexClean, "output": ""}, nil)
	if out.Class != ClassAnswerInvalid || out.Answer != nil {
		t.Fatalf("outcome %+v", out)
	}
}

// Each tool item, beside a valid answer file, is tool_used, and an unknown
// item and an unknown event are host: each stops the run at once, its group
// killed, though the CLI would run on for 300 s.
func TestCodexToolEventStopsTheRun(t *testing.T) {
	type stop struct {
		name, event string
		class       Class
	}
	var cases []stop
	for _, kind := range codexToolItems {
		cases = append(cases, stop{kind, codexItem("item.started", kind), ClassToolUsed})
	}
	cases = append(cases,
		stop{"unknown item type", codexItem("item.started", "browser_session"), ClassHost},
		stop{"unknown event type", `{"type":"turn.plan_updated"}` + "\n", ClassHost},
		stop{"brace in a notice", `notice {"type":"turn.started"}` + "\n", ClassHost},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			// The fake starts a child, prints the event, then would run for
			// 300 s.
			dump := fakeCLI(t, startChild+`cat "$dump/event"; exec sleep 300`)
			put(t, dump, "event", `{"type":"thread.started","thread_id":"x"}`+"\n"+tc.event)
			put(t, dump, "output", answer)
			r, _ := codexRunner(t, Options{})
			c := call(t, adapter.Codex)
			// Past hangGuard, so only the stream can end the run in time.
			c.Deadline = 10 * time.Minute
			out, err := await(t, launch(t, context.Background(), r, c), "a run after "+tc.name)
			if err != nil || out.Class != tc.class || out.Answer != nil || out.Attempts != 1 {
				t.Fatalf("outcome %+v %v, want %s", out, err, tc.class)
			}
			waitGone(t, dump, "child")
		})
	}
}

// A refused model, and a refused effort, are one attempt: host, with Refused
// set. There is no ladder of efforts or models.
func TestCodexModelOrEffortRefusedIsOneAttempt(t *testing.T) {
	for _, tc := range []struct {
		name, stream, effort, refused string
	}{
		{"model", codexRejected, "", RefusedModel},
		{"model with an effort", codexRejected, "high", RefusedModel},
		{"effort", codexEffortRejected("ultra"), "ultra", RefusedEffort},
		{"effort by name", strings.ReplaceAll(codexRejected, "The 'gpt-test-model' model is not supported", "The reasoning effort is not supported"), "high", RefusedEffort},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, dump := codexRun(t, map[string]string{"stdout": tc.stream, "output": answer, "exit": "1"}, func(c *Call) { c.Effort = tc.effort })
			if out.Class != ClassHost || out.Refused != tc.refused || out.Answer != nil || out.Attempts != 1 || runs(t, dump) != 1 {
				t.Fatalf("outcome %+v after %d runs, want host refusing %s after 1", out, runs(t, dump), tc.refused)
			}
		})
	}
}

// An answer one call left is never another's: two calls in a row, the first
// leaving an answer and the second writing none, are ok and answer_invalid.
func TestCodexStaleAnswerIsNotReused(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "stdout", codexClean)
	put(t, dump, "output.1", answer)
	r, _ := codexRunner(t, Options{})
	first, err := r.Run(context.Background(), call(t, adapter.Codex))
	if err != nil || first.Class != ClassOK || string(first.Answer) != answer {
		t.Fatalf("first call %+v %v", first, err)
	}
	second, err := r.Run(context.Background(), call(t, adapter.Codex))
	if err != nil || second.Class != ClassAnswerInvalid || second.Answer != nil {
		t.Fatalf("second call %+v %v, want answer_invalid", second, err)
	}
	if n := runs(t, dump); n != 2 {
		t.Fatalf("ran %d times, want 2", n)
	}
}

// The answer file is read as v1 reads it: no link followed, a regular file,
// at most 4 MiB, one JSON object.
func TestCodexAnswerFileIsReadSafely(t *testing.T) {
	big := `{"summary":"` + strings.Repeat("a", codexMaxOutput-len(`{"summary":""}`)) + `"}`
	for _, tc := range []struct {
		name, extra string
		files       map[string]string
		class       Class
	}{
		{"a link to an answer", `rm -f "$out"; ln -s "$dump/planted" "$out"`, map[string]string{"planted": answer}, ClassAnswerInvalid},
		{"a folder", `rm -f "$out"; mkdir "$out"`, nil, ClassAnswerInvalid},
		{"a pipe", `rm -f "$out"; mkfifo "$out"`, nil, ClassAnswerInvalid},
		{"4 MiB", "", map[string]string{"output": big}, ClassOK},
		{"past 4 MiB", "", map[string]string{"output": big + " "}, ClassAnswerInvalid},
		{"invalid UTF-8", "", map[string]string{"output": "{\"summary\":\"\xff\"}"}, ClassAnswerInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			dump := fakeCLI(t, tc.extra)
			put(t, dump, "stdout", codexClean)
			for name, content := range tc.files {
				put(t, dump, name, content)
			}
			r, _ := codexRunner(t, Options{})
			out, err := await(t, launch(t, context.Background(), r, call(t, adapter.Codex)), tc.name)
			if err != nil || out.Class != tc.class || (tc.class == ClassOK) != (len(out.Answer) == len(big)) {
				t.Fatalf("outcome %s with an answer of %d bytes, %v; want %s", out.Class, len(out.Answer), err, tc.class)
			}
		})
	}
}

// Usage is summed over every turn.completed, In without the cached tokens
// and never below 0, a count that is no count read as 0; and it is kept on
// a timeout. The test holds the run's time and ends it once the fake has
// printed, so the machine's speed decides nothing.
func TestCodexUsage(t *testing.T) {
	turn := func(usage string) string { return `{"type":"turn.completed","usage":` + usage + `}` + "\n" }
	for _, tc := range []struct {
		name, stream string
		want         Usage
		seen         bool
	}{
		{"two turns", codexClean + turn(`{"input_tokens":50,"cached_input_tokens":20,"output_tokens":7}`), Usage{In: 430, CachedIn: 120, Out: 67}, true},
		{"more cached than in", turn(`{"input_tokens":10,"cached_input_tokens":30,"output_tokens":1}`), Usage{CachedIn: 30, Out: 1}, true},
		{"counts that are none", turn(`{"input_tokens":-5,"cached_input_tokens":"9","output_tokens":1.5}`), Usage{}, true},
		{"no usage", `{"type":"turn.completed"}` + "\n", Usage{}, false},
		{"usage that is no object", turn(`[1,2]`), Usage{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := codexRun(t, map[string]string{"stdout": tc.stream, "output": answer}, nil)
			if out.Class != ClassOK || out.Usage != tc.want || out.UsageSeen != tc.seen {
				t.Fatalf("outcome %+v, want usage %+v seen %v", out, tc.want, tc.seen)
			}
		})
	}
	t.Run("kept on a timeout", func(t *testing.T) {
		isolate(t)
		dump := fakeCLI(t, `: > "$dump/printed"; exec sleep 300`)
		put(t, dump, "stdout", codexClean)
		put(t, dump, "output", answer)
		r, _ := codexRunner(t, Options{})
		c := call(t, adapter.Codex)
		ctx, timeouts := holdTimeouts(context.Background())
		done := launch(t, ctx, r, c)
		if d := timeouts.next(t); d != defaultDeadline {
			t.Fatalf("the run's limit is %s, want %s", d, defaultDeadline)
		}
		if !waitFile(dump, "printed") {
			t.Fatalf("the fake printed nothing in %s", hangGuard)
		}
		timeouts.expire()
		out, err := await(t, done, "a run whose time ended")
		if err != nil || out.Class != ClassTimeout || out.Answer != nil || out.Usage != codexCleanUsage || !out.UsageSeen || out.Attempts != 1 {
			t.Fatalf("outcome %+v %v, want a timeout with usage %+v", out, err, codexCleanUsage)
		}
	})
}

// HO-14's fake answers --version and features list, through Options.Env.Run;
// the runs go to the rig's script by the test's own link. The version and
// the list are read once while the CLI is unchanged, and again once it has
// a new version and modification time.
func TestCodexProbesThroughTheFakeHost(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, "")
	put(t, dump, "stdout", codexClean)
	put(t, dump, "output", answer)
	host := hostfake.New(t.TempDir())
	calls := &probeCalls{}
	env := adapter.Env{Run: func(ctx context.Context, cli string, args ...string) ([]byte, error) {
		calls.mu.Lock()
		calls.calls = append(calls.calls, strings.Join(args, " "))
		calls.mu.Unlock()
		return host.Run(ctx, filepath.Base(cli), args...)
	}}
	cli := ownCLI(t, adapter.Codex)
	r, err := New(adapter.Codex, Options{CLI: cli, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	rows := adapter.Rows(adapter.Codex)
	if len(rows) == 0 {
		t.Fatal("the adapter has no row for the host")
	}
	newest := rows[len(rows)-1].Highest
	for i := 1; i <= 2; i++ {
		out, err := r.Run(context.Background(), call(t, adapter.Codex))
		if err != nil || out.Class != ClassOK || out.CLIVersion != hostfake.DefaultCodexVersion || out.CLI != cli {
			t.Fatalf("run %d at the fake's default: %+v %v", i, out, err)
		}
		if got := codexDisabled(argsOf(t, dump, i)); !slices.Equal(got, []string{"hooks", "plugins", "apps"}) {
			t.Fatalf("run %d disabled %q", i, got)
		}
	}
	if calls.count("features list") != 1 || calls.count("--version") != 1 || calls.total() != 2 {
		t.Fatalf("two runs at one version asked the fake %q", calls.calls)
	}
	caps, err := r.Probe(context.Background())
	if err != nil || caps.Version != hostfake.DefaultCodexVersion || !caps.Untested || caps.Known || calls.total() != 2 {
		t.Fatalf("caps at the fake's default, above the newest row %s: %+v %v", newest, caps, err)
	}
	if err := host.SetVersion(hostfake.Codex, newest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cli)
	if err != nil {
		t.Fatal(err)
	}
	later := info.ModTime().Add(time.Hour)
	if err := os.Chtimes(cli, later, later); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), call(t, adapter.Codex))
	if err != nil || out.Class != ClassOK || out.CLIVersion != newest {
		t.Fatalf("a run at %s: %+v %v", newest, out, err)
	}
	if calls.count("features list") != 2 || calls.count("--version") != 2 {
		t.Fatalf("after a new version the fake was asked %q", calls.calls)
	}
	caps, err = r.Probe(context.Background())
	if err != nil || caps.Version != newest || !caps.Known || caps.Untested || calls.total() != 4 {
		t.Fatalf("caps at %s: %+v %v", newest, caps, err)
	}
}

// What an agent message or the model's reasoning says never names a
// failure: only codex's own error text does.
func TestCodexMessageTextNeverClassifies(t *testing.T) {
	for _, words := range []string{"rate limit", "not logged in", "Please sign in again", "503 Service Unavailable", "Incomplete response returned"} {
		for _, kind := range []string{"agent_message", "reasoning"} {
			stream := `{"type":"thread.started","thread_id":"x"}` + "\n" +
				`{"type":"item.completed","item":{"id":"item_1","type":"` + kind + `","text":"` + words + `"}}` + "\n"
			t.Run(kind+" "+words, func(t *testing.T) {
				for _, tc := range []struct {
					files map[string]string
					class Class
				}{
					{map[string]string{"stdout": stream, "exit": "1"}, ClassHost},
					{map[string]string{"stdout": stream + `{"type":"turn.failed","error":{"message":"something odd"}}` + "\n"}, ClassHost},
					{map[string]string{"stdout": stream}, ClassAnswerInvalid},
					{map[string]string{"stdout": stream, "output": answer}, ClassOK},
				} {
					if out, _ := codexRun(t, tc.files, nil); out.Class != tc.class {
						t.Fatalf("files %q: outcome %+v, want %s", tc.files, out, tc.class)
					}
				}
			})
		}
	}
}

// A marker shaped like a name, planted where codex writes text, an item's
// type, an event's type, an error's text, a model field and stderr, reaches
// no field of the outcome, the models included: codex names no model, and
// this reader reads none.
func TestCodexPlantedMarkersReachNoField(t *testing.T) {
	for _, marker := range []string{
		"planted-marker-41", "planted.marker@example.invalid", "/Users/planted/client-project", "0b1e8a3c-1111-4222-8333-444455556666",
	} {
		thread := `{"type":"thread.started","thread_id":"` + marker + `","model":"` + marker + `"}` + "\n"
		for _, tc := range []struct {
			place string
			files map[string]string
		}{
			{"a message", map[string]string{"output": answer, "stdout": thread +
				`{"type":"item.completed","item":{"id":"` + marker + `","type":"agent_message","text":"` + marker + `","model":"` + marker + `"}}` + "\n" +
				`{"type":"turn.completed","model":"` + marker + `","usage":{"input_tokens":5,"model":"` + marker + `"}}` + "\n"}},
			{"an item type", map[string]string{"output": answer, "stdout": thread + codexItem("item.completed", marker)}},
			{"an event type", map[string]string{"output": answer, "stdout": thread + `{"type":"` + marker + `","model":"` + marker + `"}` + "\n"}},
			{"an error text", map[string]string{"output": answer, "exit": "1", "stderr": marker + "\nmodel " + marker + " not found\n", "stdout": thread +
				`{"type":"error","message":"` + marker + `"}` + "\n" +
				`{"type":"item.completed","item":{"type":"error","message":"model ` + marker + ` is unknown"}}` + "\n" +
				`{"type":"turn.failed","error":{"message":"` + marker + `","model":"` + marker + `"}}` + "\n"}},
			{"a skipped line", map[string]string{"output": answer, "stdout": marker + "\n" + thread}},
		} {
			t.Run(tc.place+" "+marker, func(t *testing.T) {
				out, _ := codexRun(t, tc.files, nil)
				if out.Attempts != 1 || out.ModelAnswered != "" || out.ModelsSeen != nil || strings.Contains(fmt.Sprintf("%#v", out), marker) {
					t.Fatalf("the marker reached the outcome: %#v", out)
				}
				for _, s := range out.Signals {
					if s != SignalModelUnreported && s != SignalCodexLineSkipped {
						t.Fatalf("signal %q", s)
					}
				}
			})
		}
	}
}

// The stream reader on its own: what it decides doesn't hang on how the
// CLI's output is cut into writes.

// codexRead feeds stream to a new reader in pieces of size n, n 0 for the
// whole at once, and gives the reader and whether it stopped the run.
func codexRead(stream string, n int) (*codexStream, bool) {
	stopped := false
	s := &codexStream{stop: func() { stopped = true }}
	data := []byte(stream)
	for len(data) > 0 {
		k := len(data)
		if n > 0 {
			k = min(n, len(data))
		}
		if got, err := s.Write(data[:k]); got != k || err != nil {
			panic("a write was refused")
		}
		data = data[k:]
	}
	s.flush()
	return s, stopped
}

func TestCodexStreamReadsAlikeInAnyPieces(t *testing.T) {
	streams := []string{
		codexClean,
		strings.TrimSuffix(codexClean, "\n"),
		"a notice\n" + codexClean,
		codexClean + codexItem("item.started", "command_execution"),
		codexClean + `{"type":"turn.plan_updated"}` + "\n" + codexItem("item.started", "command_execution"),
		codexRejected,
		"\n\n  \n" + codexClean + "\n",
	}
	for i, stream := range streams {
		whole, wholeStopped := codexRead(stream, 0)
		for _, n := range []int{1, 2, 3, 7, 64} {
			s, stopped := codexRead(stream, n)
			if s.toolUsed != whole.toolUsed || s.invalid != whole.invalid || s.turnFailed != whole.turnFailed || s.skipped != whole.skipped ||
				s.usage != whole.usage || s.usageSeen != whole.usageSeen || s.diag.text.String() != whole.diag.text.String() || stopped != wholeStopped {
				t.Fatalf("stream %d in pieces of %d read otherwise than whole", i, n)
			}
		}
	}
}

// Every tool item type, in every item event, wherever it stands in a
// stream, and in any letter case, is a tool and stops the run; no stream
// that holds one reads as anything else.
func TestCodexEveryToolItemIsATool(t *testing.T) {
	prefixes := []string{"", `{"type":"thread.started","thread_id":"x"}` + "\n", codexClean, "a notice\n", `{"type":"turn.failed","error":{"message":"m"}}` + "\n"}
	suffixes := []string{"", codexClean, `{"type":"turn.plan_updated"}` + "\n", "{broken\n"}
	for _, kind := range codexToolItems {
		for _, name := range []string{kind, strings.ToUpper(kind)} {
			for _, event := range []string{"item.started", "item.updated", "item.completed", "item.anything"} {
				for _, pre := range prefixes {
					for _, post := range suffixes {
						s, stopped := codexRead(pre+codexItem(event, name)+post, 0)
						if !s.toolUsed || s.invalid || !stopped {
							t.Fatalf("%s in %s after %q: tool %v, invalid %v, stopped %v", name, event, pre, s.toolUsed, s.invalid, stopped)
						}
					}
				}
			}
		}
	}
	// A line longer than codex's limit can't be read: host, and stopped.
	s, stopped := codexRead(`{"type":"item.completed","item":{"type":"agent_message","text":"`+strings.Repeat("a", codexMaxLine)+`"}}`+"\n", 1<<16)
	if s.toolUsed || !s.invalid || !stopped {
		t.Fatalf("a line past the limit: tool %v, invalid %v, stopped %v", s.toolUsed, s.invalid, stopped)
	}
	// One just inside it is read.
	line := `{"type":"item.completed","item":{"type":"agent_message","text":""}}`
	s, stopped = codexRead(strings.Replace(line, `""`, `"`+strings.Repeat("a", codexMaxLine-len(line))+`"`, 1)+"\n", 1<<16)
	if s.toolUsed || s.invalid || stopped {
		t.Fatalf("a line at the limit: tool %v, invalid %v, stopped %v", s.toolUsed, s.invalid, stopped)
	}
}

// The reader's own result, apart from the core: a tool is tool_used, with
// the tool sign, whatever ended the run; a line it can't vouch for is host,
// with no answer read; neither is ever ok.
func TestCodexResultPutsTheToolFirst(t *testing.T) {
	output := filepath.Join(t.TempDir(), "output.json")
	if err := os.WriteFile(output, []byte(answer), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, e := range []exit{{started: true}, {started: true, failed: true}, {started: true, failed: true, timedOut: true}} {
		tool, _ := codexRead(codexItem("item.started", "command_execution")+codexClean, 0)
		if res := tool.result(e, output, ""); res.class != ClassToolUsed || !res.toolSign || res.answer != nil || res.usage != (Usage{}) {
			t.Errorf("a tool, exit %+v: %+v", e, res)
		}
		odd, _ := codexRead(codexClean+`{"type":"turn.plan_updated"}`+"\n", 0)
		if res := odd.result(e, output, ""); res.class != ClassHost || res.toolSign || res.answer != nil || res.usage != codexCleanUsage {
			t.Errorf("an unknown event, exit %+v: %+v", e, res)
		}
	}
}

// The probe step keeps only the features of codexOff and codexTools the
// list names, and a list that can't be read gives codexOff, not to be kept.
func TestCodexProbeStep(t *testing.T) {
	list := func(out string, err error) probeRun {
		return func(ctx context.Context, args ...string) ([]byte, error) {
			if !slices.Equal(args, []string{"features", "list"}) {
				return nil, errors.New("not features list")
			}
			return []byte(out), err
		}
	}
	for _, tc := range []struct {
		name, out string
		err       error
		want      []string
		ok        bool
	}{
		{"a list", "apps experimental false\nhooks stable true\nunified_exec stable true\ncomputer_use stable false\n", nil, []string{"hooks", "apps", "computer_use"}, true},
		{"none of ours", "unified_exec stable true\n", nil, nil, true},
		{"no list", "", nil, codexOff, false},
		{"two columns", "hooks true\n", nil, codexOff, false},
		{"a failure", "hooks stable true\n", errors.New("exit 1"), codexOff, false},
	} {
		got, ok := codexProbe(context.Background(), list(tc.out, tc.err))
		if !slices.Equal(got, tc.want) || ok != tc.ok {
			t.Errorf("%s: %q %v, want %q %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	got, _ := codexProbe(context.Background(), list("", nil))
	got[0] = "changed"
	if codexOff[0] != "hooks" {
		t.Fatal("the probe step gave codexOff itself, not a copy")
	}
}
