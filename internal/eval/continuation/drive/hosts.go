package drive

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// The host table: how the driver runs a headless chat on each host, by the
// adapter's id. Each entry names the host facts of docs/v2/host-facts.md it
// leans on. None of them is recorded yet: the pilot (EV-03) and the live
// gates (DV-02) settle them, and what they find changes this file only.
// Until then each shape below is the one the fake host (drive/fakehost)
// answers, which follows the hosts' published documents.

// chat is one headless call: the first prompt of a session, or a prompt
// that continues one.
type chat struct {
	Prompt string
	Turns  int    // the task's max_turns
	Model  string // "" for the host's default
	Resume string // the session continued, or "" to start one
	// Extra are the flags of the condition: hostEntry.Off's, or those
	// hostEntry.Plugin gives, then Launch.Args.
	Extra []string
	// Web are the flags that turn the host's web tools off, which
	// hostEntry.WebOff gave for the trial. They come after Extra, so that
	// no flag of a condition turns a web tool back on.
	Web []string
}

// report is what the driver reads from one call's output.
type report struct {
	Session     string
	AgentEvents int
	// PromptTokens counts every token of input, cached ones included.
	PromptTokens int64
	CachedTokens int64
	OutputTokens int64
	// Model is the model that answered, or "" when the output names none.
	Model string
	// Fail is a code of the closed list when the host said it could not
	// answer for a reason that says nothing of the task: CodeSignIn or
	// CodeRateLimited; "" otherwise. The driver sets CodeHostFailed itself
	// on a call that gave no answer for any other reason.
	Fail string
	// Answered says the output holds the end of an answer: the host's
	// final event of a call, and no failure. A call that gives no answer,
	// such as one the host's service failed or one that printed nothing, is
	// never graded, since what the workspace holds then says nothing of the
	// chat.
	Answered bool
	// Usage says the host gave its usage: each final event the reader
	// counts the tokens of carries it, and there is one. A call answered
	// without it spent what no ceiling can count.
	Usage bool
	// Spent is what --max-tokens counts of the call: its prompt and output
	// tokens, from each final event by whichever of the host's places for
	// them names more. The rows' tokens are read as the reader says, which
	// may name fewer.
	Spent int64
	// Web says the output names a call of a web tool, a search or a fetch,
	// which no chat should have had: the set-up is wrong.
	Web bool
}

// pluginLoad is how a chat loads eva-md's plugin: flags, and files written
// under the trial's home, by their path relative to it.
type pluginLoad struct {
	Args  []string
	Files map[string][]byte
}

// hostEntry is one host's row of the table.
type hostEntry struct {
	// Argv gives the arguments after the CLI's path for one call.
	Argv func(c chat) []string
	// Off are the flags of a chat with the host's own memory off and no
	// plugin loaded.
	Off []string
	// OffEnv are the variables, NAME=value, that turn the host's own memory
	// off too, beside its flags. Every chat of the host has them, whatever
	// its condition, one that loads the plugin among them.
	OffEnv []string
	// Plugin gives what loads eva-md's plugin as setup installed it in the
	// trial's home, with the host's own memory still off. It never writes a
	// trust record.
	Plugin func(home string, h adapter.Host) (pluginLoad, error)
	// WebOff gives the flags that turn the host's web tools off, its web
	// search and its web fetch, for every call of a trial, whatever its
	// condition, while the chat keeps the tools that edit files and run
	// commands. list runs the host's CLI with the arguments given, in the
	// trial's environment, and gives what it printed on stdout; an entry
	// that must ask the CLI what it can turn off uses it. An error says the
	// web tools cannot be turned off, and stops the run before any chat.
	WebOff func(list func(args ...string) ([]byte, error)) ([]string, error)
	// Read reads what one call printed on stdout.
	Read func(out []byte) report
	// ConfigEnv are the variables that point the host at a configuration
	// folder of its own. --pass-env passes none of them, whatever the host,
	// since one would give the chat the invoking home's settings, sign-in
	// and trust records.
	ConfigEnv []string
}

// hosts is the table, by the adapter's id.
var hosts = map[adapter.ID]hostEntry{
	// Claude Code.
	//   - F-068: --output-format stream-json emits an event for each tool
	//     call; -p takes it with --verbose.
	//   - F-069: --max-turns bounds the run, and --resume <id> continues the
	//     session.
	//   - F-062: the settings given by --settings turn its own memory off
	//     for this run, writing no settings file, and so does the variable
	//     CLAUDE_CODE_DISABLE_AUTO_MEMORY (evaluate.md 4.5 and 14, fact 3),
	//     which every chat gets too. A trial's home is new, so no plugin is
	//     installed there to turn off.
	//   - F-115: --plugin-dir loads a plugin from a folder, and its hooks
	//     fire under an API key.
	//   - F-067: the run is headless under an API key; its permission mode
	//     lets the chat edit files and run tests in the trial's workspace.
	//   - Web tools off (WebOff): no host fact records it yet, so this is a
	//     guess for the pilot's smoke run to prove. --disallowedTools takes
	//     tool names and takes those tools from the chat, whatever its
	//     permission mode (working-papers/research/claude-surface.md 6.1
	//     names the flag); WebSearch and WebFetch are the built-in web tools,
	//     as eva-md's own hook matcher names them (internal/install/
	//     templates.go). Edit, Write and Bash stay. F-113 (not recorded)
	//     says the stream's opening event names the tools on offer, where
	//     the smoke run reads that neither is there. No variable is known to
	//     turn them back on.
	//   - Web calls (Read): a tool_use block named WebSearch or WebFetch, a
	//     server_tool_use block named web_search or web_fetch, or a count of
	//     web searches or fetches above 0 in a result's usage
	//     (server_tool_use) or in a model's entry of modelUsage, as DV-11c's
	//     reader of the same host names them (internal/derive/modelrun/
	//     claude.go). A guess as well, for the smoke run to confirm.
	adapter.ClaudeCode: {
		// The prompt comes last, after "--", so that one that starts with "-"
		// is read as no flag, and no flag that takes several values takes it.
		Argv: func(c chat) []string {
			args := []string{"-p", "--output-format", "stream-json", "--verbose",
				"--max-turns", strconv.Itoa(c.Turns), "--permission-mode", "bypassPermissions"}
			if c.Model != "" {
				args = append(args, "--model", c.Model)
			}
			if c.Resume != "" {
				args = append(args, "--resume", c.Resume)
			}
			args = append(args, c.Extra...)
			args = append(args, c.Web...)
			return append(args, "--", c.Prompt)
		},
		WebOff: func(func(...string) ([]byte, error)) ([]string, error) {
			return slices.Concat([]string{"--disallowedTools"}, claudeWebTools), nil
		},
		Off:    []string{"--settings", `{"autoMemoryEnabled":false}`},
		OffEnv: []string{"CLAUDE_CODE_DISABLE_AUTO_MEMORY=1"},
		Plugin: func(home string, h adapter.Host) (pluginLoad, error) {
			dir := pluginDir(home, h)
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				return pluginLoad{}, errors.New("eva-md's plugin is not installed in the trial's home")
			}
			return pluginLoad{Args: []string{"--settings", `{"autoMemoryEnabled":false}`, "--plugin-dir", dir}}, nil
		},
		Read:      readStreamJSON,
		ConfigEnv: []string{"CLAUDE_CONFIG_DIR"},
	},
	// Codex.
	//   - F-068: exec --json emits an item for each tool call.
	//   - F-069: exec resume <id> continues the session. exec has no turn
	//     bound, so the session's time limit is its only bound.
	//   - F-169: --disable hooks --disable plugins turn hooks and plugins off
	//     for this run, writing no settings file.
	//   - F-170 (not EV-03's): its own memory is off by default and takes no
	//     exec session, so nothing turns it off, and it has no OffEnv.
	//   - F-163: managed hooks need no review, so eva-md's hooks are loaded
	//     as managed hooks, from a requirements file under the trial's home,
	//     and the plugin itself stays off. Where Codex reads that file in a
	//     container is F-163's to record; the driver writes only inside the
	//     trial.
	//   - F-067: the run is headless under an API key; it may edit files and
	//     run tests in the workspace, which need not be a repository.
	//   - Web tools off (WebOff): no host fact records it yet, so this is a
	//     guess for the pilot's smoke run to prove, from what v1's update
	//     already passes (internal/update/codex.go): -c web_search=disabled
	//     turns the web search off, and --disable turns off each feature of
	//     codexWebFeatures that the CLI's own features list names, since it
	//     refuses a name it does not know. The list is read once a trial, in
	//     its environment, and one that cannot be read stops the run. The
	//     shell and the edit tool stay. exec --json prints no list of the
	//     tools on offer. No variable is known to turn them back on.
	//   - Web calls (Read): an item of type web_search, started or
	//     completed, as DV-11b's reader of exec --json names it
	//     (internal/derive/modelrun/codex.go). A guess as well.
	adapter.Codex: {
		Argv: func(c chat) []string {
			args := []string{"exec", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"}
			if c.Model != "" {
				args = append(args, "--model", c.Model)
			}
			args = append(args, c.Extra...)
			args = append(args, c.Web...)
			if c.Resume != "" {
				args = append(args, "resume", c.Resume)
			}
			// The prompt comes last, after "--", so that one that starts with
			// "-" is read as no flag.
			return append(args, "--", c.Prompt)
		},
		Off: []string{"--disable", "hooks", "--disable", "plugins"},
		Plugin: func(home string, h adapter.Host) (pluginLoad, error) {
			data, err := os.ReadFile(filepath.Join(pluginDir(home, h), "hooks", "hooks.json"))
			if err != nil {
				return pluginLoad{}, errors.New("eva-md's plugin is not installed in the trial's home")
			}
			managed, err := managedHooks(data)
			if err != nil {
				return pluginLoad{}, err
			}
			return pluginLoad{
				Args:  []string{"--disable", "plugins"},
				Files: map[string][]byte{filepath.Join(".codex", "requirements.toml"): managed},
			}, nil
		},
		WebOff:    codexWebOff,
		Read:      readExecJSON,
		ConfigEnv: []string{"CODEX_HOME"},
	},
}

// codexWebFeatures are the Codex features that give a chat a tool that
// reaches the web, from v1's list of tool features (codexTools in
// internal/update/codex.go): the standalone web search, and browser and
// computer use, whose browser fetches pages. None edits a file or runs a
// command.
var codexWebFeatures = []string{"standalone_web_search", "browser_use", "browser_use_external",
	"browser_use_full_cdp_access", "computer_use"}

// codexWebOff turns Codex's web search off by its setting, and each
// feature of codexWebFeatures that codex features list names, whether on
// or off. A list that cannot be read, or names no feature, is an error:
// whether a web feature is there to turn off is then unknown.
func codexWebOff(list func(args ...string) ([]byte, error)) ([]string, error) {
	out, err := list("features", "list")
	if err != nil {
		return nil, errors.New("the host's CLI did not list its features, so its web tools cannot be turned off")
	}
	// A feature's line is its name, its stage and whether it is on, as v1's
	// update reads it (codexDisable in internal/update/codex.go).
	listed := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) >= 3 && (f[len(f)-1] == "true" || f[len(f)-1] == "false") {
			listed[f[0]] = true
		}
	}
	if len(listed) == 0 {
		return nil, errors.New("the host's CLI listed no features, so its web tools cannot be turned off")
	}
	args := []string{"-c", "web_search=disabled"}
	for _, name := range codexWebFeatures {
		if listed[name] {
			args = append(args, "--disable", name)
		}
	}
	return args, nil
}

// claudeWebTools are Claude Code's web tools, by the names a tool_use block
// gives them; claudeServerWeb are the API's own, by the names a
// server_tool_use block gives them.
var (
	claudeWebTools  = []string{"WebSearch", "WebFetch"}
	claudeServerWeb = []string{"web_search", "web_fetch"}
)

// claudeWebCounts are the counts of web searches and fetches the API ran,
// in a result's usage.server_tool_use (snake case) or in a model's entry of
// modelUsage (camel case), as DV-11c's reader names them.
var claudeWebCounts = []string{"web_search_requests", "web_fetch_requests", "webSearchRequests", "webFetchRequests"}

// webCounted reports whether one of claudeWebCounts in u is above 0.
func webCounted(u map[string]json.RawMessage) bool {
	for _, k := range claudeWebCounts {
		if num(u[k]) > 0 {
			return true
		}
	}
	return false
}

// pluginDir is where install renders eva-md's plugin for the host in a
// home (pluginPath in internal/install/templates.go).
func pluginDir(home string, h adapter.Host) string {
	return filepath.Join(home, ".local", "share", "eva-md", "marketplace", h.Names.InstallName, "plugins", "eva-md")
}

// managedHooks renders the hooks of a plugin's hooks.json as a managed
// requirements file's hooks tables.
func managedHooks(data []byte) ([]byte, error) {
	var file struct {
		Hooks map[string][]struct {
			Matcher string           `json:"matcher"`
			Hooks   []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil || len(file.Hooks) == 0 {
		return nil, errors.New("eva-md's plugin declares no hooks the driver can read")
	}
	var b bytes.Buffer
	for _, event := range sortedKeys(file.Hooks) {
		for _, group := range file.Hooks[event] {
			fmt.Fprintf(&b, "[[hooks.%s]]\n", event)
			if group.Matcher != "" {
				fmt.Fprintf(&b, "matcher = %s\n", tomlString(group.Matcher))
			}
			for _, hook := range group.Hooks {
				fmt.Fprintf(&b, "[[hooks.%s.hooks]]\n", event)
				for _, key := range sortedKeys(hook) {
					switch v := hook[key].(type) {
					case string:
						fmt.Fprintf(&b, "%s = %s\n", key, tomlString(v))
					case float64:
						fmt.Fprintf(&b, "%s = %s\n", key, strconv.FormatFloat(v, 'f', -1, 64))
					case bool:
						fmt.Fprintf(&b, "%s = %t\n", key, v)
					default:
						return nil, errors.New("eva-md's plugin declares a hook the driver cannot render")
					}
				}
			}
		}
	}
	return b.Bytes(), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// tomlString writes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// lines calls f with each line of out that is a JSON object. A line may be
// as long as out, which runGroup keeps within the output's limit.
func lines(out []byte, f func(map[string]json.RawMessage)) {
	s := bufio.NewScanner(bytes.NewReader(out))
	s.Buffer(make([]byte, 0, 64<<10), len(out)+1)
	for s.Scan() {
		var e map[string]json.RawMessage
		if json.Unmarshal(bytes.TrimSpace(s.Bytes()), &e) == nil {
			f(e)
		}
	}
}

func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func num(raw json.RawMessage) int64 {
	var n float64
	if json.Unmarshal(raw, &n) != nil || n < 0 || n > 1<<53 {
		return 0
	}
	return int64(n)
}

// readStreamJSON reads Claude Code's stream-json: the session id from any
// event; a tool call for each tool_use block of an assistant message; and
// from the result event the tokens, by modelUsage when it names models
// (every model call of the run) else by usage, and for the ceiling by
// whichever of the two names more (Spent), and whether it failed for
// sign-in (401 or 403) or a rate limit (429). The model that answered is
// the one modelUsage credits with the most output tokens, else the last
// assistant message's. The call answered when a result event is no error,
// or is the end of a run at its turn bound (subtype error_max_turns),
// which leaves the chat's work as it stands; any other error, such as one
// of the host's service (a 5xx), is no answer. The usage was given when
// each result event has a usage object or a modelUsage naming a model. A
// web tool was called when a tool_use block names one of claudeWebTools, a
// server_tool_use block one of claudeServerWeb, or a result's counts of
// claudeWebCounts one above 0.
func readStreamJSON(out []byte) report {
	var r report
	broken := false
	finals, counted := 0, 0
	lines(out, func(e map[string]json.RawMessage) {
		if id := str(e["session_id"]); id != "" {
			r.Session = id
		}
		switch str(e["type"]) {
		case "assistant":
			var m struct {
				Model   string `json:"model"`
				Content []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
			}
			_ = json.Unmarshal(e["message"], &m)
			for _, c := range m.Content {
				switch {
				case c.Type == "tool_use":
					r.AgentEvents++
					r.Web = r.Web || slices.Contains(claudeWebTools, c.Name)
				case c.Type == "server_tool_use" && slices.Contains(claudeServerWeb, c.Name):
					r.Web = true
				}
			}
			if m.Model != "" {
				r.Model = m.Model
			}
		case "result":
			finals++
			var models map[string]map[string]json.RawMessage
			named := json.Unmarshal(e["modelUsage"], &models) == nil && len(models) > 0
			var by, mu tokens
			best, most := "", int64(-1)
			for name, u := range models {
				r.Web = r.Web || webCounted(u)
				cached := num(u["cacheReadInputTokens"])
				n := num(u["outputTokens"])
				mu.add(tokens{num(u["inputTokens"]) + num(u["cacheCreationInputTokens"]) + cached, cached, n})
				if n > most || n == most && name < best {
					best, most = name, n
				}
			}
			var u map[string]json.RawMessage
			given := json.Unmarshal(e["usage"], &u) == nil && u != nil
			if given {
				var server map[string]json.RawMessage
				_ = json.Unmarshal(u["server_tool_use"], &server)
				r.Web = r.Web || webCounted(server)
				cached := num(u["cache_read_input_tokens"])
				by = tokens{num(u["input_tokens"]) + num(u["cache_creation_input_tokens"]) + cached, cached, num(u["output_tokens"])}
			}
			if named || given {
				counted++
			}
			// The ceiling counts the place that names more tokens, so that
			// what one of them leaves out is still counted; the rows keep
			// EV-17b's measure: modelUsage when it names models, else usage.
			r.Spent += max(mu.prompt+mu.output, by.prompt+by.output)
			if named {
				by = mu
				r.Model = best
			}
			r.PromptTokens += by.prompt
			r.CachedTokens += by.cached
			r.OutputTokens += by.output
			var failed bool
			_ = json.Unmarshal(e["is_error"], &failed)
			subtype, status := str(e["subtype"]), num(e["api_error_status"])
			switch {
			case failed && (status == 401 || status == 403):
				r.Fail = CodeSignIn
			case failed && status == 429:
				r.Fail = CodeRateLimited
			case subtype == "error_max_turns":
				r.Answered = true
			case failed || strings.HasPrefix(subtype, "error"):
				broken = true
			default:
				r.Answered = true
			}
		}
	})
	r.Answered = r.Answered && !broken
	r.Usage = finals > 0 && counted == finals
	return r
}

// tokens are a final event's prompt (cached included), cached and output
// tokens.
type tokens struct{ prompt, cached, output int64 }

func (t *tokens) add(u tokens) {
	t.prompt += u.prompt
	t.cached += u.cached
	t.output += u.output
}

// readExecJSON reads Codex's exec --json: the session id from
// thread.started; a tool call for each completed item that is not the
// agent's message, its reasoning or an error; the tokens of each
// turn.completed, whose input already holds the cached ones; the model a
// turn names; and from turn.failed or error whether it failed for sign-in
// (401 or 403) or a rate limit (429). The call answered when a turn
// completed and none failed: exec has no turn bound, so a turn.failed or
// an error event is no answer. The usage was given when each
// turn.completed has a usage object. A web tool was called when an item
// started or completed is a web_search.
func readExecJSON(out []byte) report {
	var r report
	broken := false
	finals, counted := 0, 0
	lines(out, func(e map[string]json.RawMessage) {
		switch str(e["type"]) {
		case "thread.started":
			r.Session = str(e["thread_id"])
		case "item.started", "item.completed":
			var item struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(e["item"], &item)
			if item.Type == "web_search" {
				r.Web = true
			}
			if str(e["type"]) == "item.started" {
				break
			}
			switch item.Type {
			case "agent_message", "reasoning", "error":
			default:
				r.AgentEvents++
			}
		case "turn.completed":
			finals++
			var u map[string]json.RawMessage
			if json.Unmarshal(e["usage"], &u) == nil && u != nil {
				counted++
			}
			r.PromptTokens += num(u["input_tokens"])
			r.CachedTokens += num(u["cached_input_tokens"])
			r.OutputTokens += num(u["output_tokens"])
			r.Spent += num(u["input_tokens"]) + num(u["output_tokens"])
			if m := str(e["model"]); m != "" {
				r.Model = m
			}
			r.Answered = true
		case "turn.failed", "error":
			broken = true
			var msg string
			if raw, ok := e["error"]; ok {
				var x struct {
					Message string `json:"message"`
				}
				_ = json.Unmarshal(raw, &x)
				msg = x.Message
			} else {
				msg = str(e["message"])
			}
			switch {
			case strings.Contains(msg, "401"), strings.Contains(msg, "403"):
				r.Fail = CodeSignIn
			case strings.Contains(msg, "429"):
				r.Fail = CodeRateLimited
			}
		}
	})
	r.Answered = r.Answered && !broken
	r.Usage = finals > 0 && counted == finals
	return r
}
