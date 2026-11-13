package modelrun

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// The Claude Code backend: v1's run (internal/update/claude.go), its single
// JSON envelope classified from the envelope's own fields first. A refused
// tool call in permission_denials is tool_used beside any answer; every model
// modelUsage names is kept, with the one that wrote the most; an envelope
// printed before a timeout gives its usage; and what nothing names is host,
// never the chat's fault. The stream reader the design wants waits on a host
// fact not yet recorded (host-facts F-113), so an allowed tool call is seen
// only through permission_denials until then.

func init() {
	register(adapter.ClaudeCode, backend{
		efforts: claudeEfforts,
		signals: []Signal{SignalClaudeStrictMCPRefused, SignalClaudeSeveralModels, SignalClaudeModelUnnamed},
		attempt: claudeAttempt,
	})
}

// The Claude Code backend's signals.
const (
	// SignalClaudeStrictMCPRefused says a managed MCP config refused
	// --strict-mcp-config, and the call's second attempt went without it.
	SignalClaudeStrictMCPRefused Signal = "claude_strict_mcp_refused"
	// SignalClaudeSeveralModels says modelUsage named two models or more.
	SignalClaudeSeveralModels Signal = "claude_several_models"
	// SignalClaudeModelUnnamed says modelUsage named a model by a name this
	// package can't keep, or named its models in a shape it can't read.
	SignalClaudeModelUnnamed Signal = "claude_model_unnamed"
)

// claudeEfforts are the values --effort takes, lowest first (v1's
// claudeEfforts).
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// Size caps (v1's maxStdout and maxDiag).
const (
	claudeMaxEnvelope = 8 << 20  // the envelope on stdout
	claudeMaxDiag     = 64 << 10 // error text kept to name a failure
)

// claudeAttempt is one run of claude -p. The first attempt passes
// --strict-mcp-config; a second, which only its refusal asks for, goes
// without it.
func claudeAttempt(ctx context.Context, a *attempt) (result, error) {
	out := &capped{limit: claudeMaxEnvelope}
	e, err := a.run(ctx, claudeArgs(a.call, a.n == 1), a.call.Input, out)
	if err != nil {
		return result{}, err
	}
	res := claudeResult(e, out, a.call.Effort)
	if res.again {
		if a.n == 1 {
			res.signals = append(res.signals, SignalClaudeStrictMCPRefused)
		} else {
			res.again = false
		}
	}
	return res, nil
}

// claudeArgs is v1's command. The input is stdin, never an argument.
// --tools "" turns off only the built-in tools, and an MCP server's tools
// stay callable, so --strict-mcp-config, with no --mcp-config, loads no MCP
// server at all. --no-session-persistence leaves no transcript.
func claudeArgs(c Call, strictMCP bool) []string {
	args := []string{"-p", "--safe-mode", "--tools", ""}
	if strictMCP {
		args = append(args, "--strict-mcp-config")
	}
	args = append(args, "--no-session-persistence", "--permission-prompts", "none")
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if c.Effort != "" {
		args = append(args, "--effort", c.Effort)
	}
	return append(args, "--system-prompt", c.System, "--output-format", "json", "--json-schema", string(c.Schema))
}

// claudeResult names the run's class from the envelope's fields first, and
// from error text only where no field decides (SPEC.md, section 7). The core
// puts a missing CLI, then a tool, then a timeout first.
func claudeResult(e exit, out *capped, effort string) result {
	res := result{class: ClassHost}
	if e.missing {
		return res
	}
	env, found := claudeEnvelope(out)
	if found {
		// A refused tool call taints the run whatever ended it, a timeout
		// too, as the core ranks a tool sign above a timeout.
		res.toolSign = env.denied
		res.models, res.signals = env.models, env.signals
		if !env.odd {
			res.answered, res.usage, res.usageSeen = env.answered, env.usage, env.usageSeen
		}
	}
	d := &claudeDiag{effort: effort}
	switch {
	case e.timedOut:
		// The answer doesn't count; the usage does.
	case !found:
		// No envelope. A CLI that failed may have printed its error as
		// plain text. Stdout with a brace may hold an envelope's model text,
		// which must not name a failure, so only stdout without one is read.
		if e.failed {
			d.note(string(e.stderr))
			if !out.overflow && bytes.IndexByte(out.Bytes(), '{') < 0 {
				d.note(out.String())
			}
			res.class, res.refused = d.class()
			res.again = d.refusesStrictMCP() && res.class == ClassHost
		}
	case env.denied:
		res.class = ClassToolUsed
	case env.odd:
		// A key given twice, a known key in another letter case, or a field
		// of a type it never has: an envelope this reader can't vouch for.
	case env.subtype == "error_max_structured_output_retries":
		res.class = ClassAnswerInvalid
	case claudeStatus(env.status) != "":
		res.class = claudeStatus(env.status)
		if env.status == 404 {
			res.refused = RefusedModel
		}
	case env.isError || env.subtype != "success" || e.failed:
		// result is error text only when is_error is set; otherwise it is
		// the model's own words, which never name a failure.
		if env.isError {
			d.note(claudeText(env.result))
		}
		d.note(claudeMessage(env.errors))
		d.note(claudeMessage(env.apiError))
		d.note(string(e.stderr))
		res.class, res.refused = d.class()
		res.again = d.refusesStrictMCP() && res.class == ClassHost
	default:
		if answer, ok := object(env.answer); ok {
			res.class, res.answer = ClassOK, answer
		} else {
			res.class = ClassAnswerInvalid
		}
	}
	return res
}

// claudeStatus names a failure from the API error's HTTP status (v1's
// claudeStatus): 401 is auth; 429, and 529, claude's overloaded status, a
// limit; 404, a model the API doesn't know, and any other 5xx are host. Any
// other status names nothing.
func claudeStatus(status int64) Class {
	switch {
	case status == 401:
		return ClassAuth
	case status == 429 || status == 529:
		return ClassRateLimit
	case status == 404 || status >= 500 && status <= 599:
		return ClassHost
	}
	return ""
}

// claudeEnv is what claudeEnvelope read from the envelope's fields.
type claudeEnv struct {
	// odd: an envelope this reader can't vouch for: a key given twice, in
	// the envelope or its modelUsage, a key it reads in another letter case,
	// or a subtype or is_error of a type that field never has. Its denials
	// and the models it names are read; its class is host, and its usage
	// and answering model are not kept.
	odd      bool
	denied   bool // permission_denials, in any letter case, lists anything
	subtype  string
	isError  bool
	status   int64 // api_error_status, 0 when it is none
	result   json.RawMessage
	errors   json.RawMessage
	apiError json.RawMessage
	answer   json.RawMessage // structured_output
	// models are the names modelUsage gives that a model flag could take,
	// and answered the one that wrote the most output, "" when that one's
	// name can't be kept.
	models    []string
	answered  string
	signals   []Signal
	usage     Usage
	usageSeen bool
}

// claudeKeys are the envelope's keys this reader reads.
var claudeKeys = []string{"type", "subtype", "is_error", "result", "errors", "structured_output",
	"usage", "modelUsage", "permission_denials", "api_error_status", "api_error"}

// claudeField is one key of a JSON object and its value, in the order given.
type claudeField struct {
	key   string
	value json.RawMessage
}

// claudeFields reads data as one JSON object, its fields in order, a key
// given twice kept twice. encoding/json reads each value, so no value nests
// past its own limit of 10,000 levels. ok is false for anything but one
// object.
func claudeFields(data []byte) (fields []claudeField, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}
	for dec.More() {
		tok, err := dec.Token()
		key, isKey := tok.(string)
		if err != nil || !isKey {
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		fields = append(fields, claudeField{key, value})
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return fields, true
}

// claudeEnvelope reads the envelope claude -p --output-format json prints:
// one JSON object whose type is "result", within the cap. found is false for
// anything else. A type key in another letter case, or given twice, still
// makes an envelope, which is odd, so a denial beside it is read.
func claudeEnvelope(out *capped) (env claudeEnv, found bool) {
	if out.overflow {
		return env, false
	}
	fields, ok := claudeFields(bytes.TrimSpace(out.Bytes()))
	if !ok {
		return env, false
	}
	exact := map[string]json.RawMessage{}
	for _, f := range fields {
		var kind string
		if strings.EqualFold(f.key, "type") && json.Unmarshal(f.value, &kind) == nil && kind == "result" {
			found = true
		}
		if _, twice := exact[f.key]; twice {
			env.odd = true
		}
		exact[f.key] = f.value
		known := slices.ContainsFunc(claudeKeys, func(k string) bool { return strings.EqualFold(k, f.key) })
		if known && !slices.Contains(claudeKeys, f.key) {
			env.odd = true
		}
		// Every denial counts, in whatever case or however often its key is
		// given, so a changed shape can't hide one.
		if strings.EqualFold(f.key, "permission_denials") && claudeDenied(f.value) {
			env.denied = true
		}
	}
	if !found {
		return claudeEnv{}, false
	}
	for _, f := range fields {
		if strings.EqualFold(f.key, "modelUsage") {
			env.readModels(f.value)
		}
	}
	if s, ok := exact["subtype"]; ok && json.Unmarshal(s, &env.subtype) != nil {
		env.odd = true
	}
	if raw, ok := exact["is_error"]; ok && json.Unmarshal(raw, &env.isError) != nil {
		env.odd = true
	}
	env.status = claudeCount(claudeNumber(exact["api_error_status"]))
	env.result, env.errors, env.apiError = exact["result"], exact["errors"], exact["api_error"]
	env.answer = exact["structured_output"]
	env.readUsage(exact["modelUsage"], exact["usage"])
	return env, true
}

// claudeDenied reports whether permission_denials lists anything: whatever
// it holds but null or an empty list counts.
func claudeDenied(raw json.RawMessage) bool {
	if raw == nil || string(raw) == "null" {
		return false
	}
	var denials []json.RawMessage
	return json.Unmarshal(raw, &denials) != nil || len(denials) > 0
}

// readModels reads one modelUsage: every model it names that a model flag
// could take, and the one with the most output tokens, the first by name on
// a tie, as v1's claudeModel chooses, but among every model it names: when
// that one's name can't be kept, the answering model is unknown.
func (env *claudeEnv) readModels(raw json.RawMessage) {
	if raw == nil || string(raw) == "null" {
		return
	}
	fields, ok := claudeFields(raw)
	if !ok {
		env.signals = append(env.signals, SignalClaudeModelUnnamed)
		return
	}
	names := map[string]bool{}
	best, most := "", int64(-1)
	for _, f := range fields {
		if names[f.key] {
			env.odd = true
		}
		names[f.key] = true
		if !modelName.MatchString(f.key) {
			env.signals = append(env.signals, SignalClaudeModelUnnamed)
		} else if !slices.Contains(env.models, f.key) {
			env.models = append(env.models, f.key)
		}
		n := claudeCount(claudeTokens(f.value)["outputTokens"])
		if n > most || n == most && f.key < best {
			best, most = f.key, n
		}
	}
	if len(names) > 1 || len(env.models) > 1 {
		env.signals = append(env.signals, SignalClaudeSeveralModels)
	}
	if modelName.MatchString(best) {
		env.answered = best
	}
}

// readUsage reads the token counts: from modelUsage, which covers every model
// call of the run, when it names any, else from usage, which covers the main
// loop only. In is the input and the cache writes, CachedIn the cache reads.
func (env *claudeEnv) readUsage(modelUsage, usage json.RawMessage) {
	if fields, ok := claudeFields(modelUsage); ok && len(fields) > 0 {
		for _, f := range fields {
			u := claudeTokens(f.value)
			env.usage.In += claudeCount(u["inputTokens"]) + claudeCount(u["cacheCreationInputTokens"])
			env.usage.CachedIn += claudeCount(u["cacheReadInputTokens"])
			env.usage.Out += claudeCount(u["outputTokens"])
		}
		env.usageSeen = true
		return
	}
	u := claudeTokens(usage)
	if u == nil {
		return
	}
	env.usage.In = claudeCount(u["input_tokens"]) + claudeCount(u["cache_creation_input_tokens"])
	env.usage.CachedIn = claudeCount(u["cache_read_input_tokens"])
	env.usage.Out = claudeCount(u["output_tokens"])
	env.usageSeen = true
}

// claudeTokens decodes an object of counts, and gives nil for anything else.
func claudeTokens(raw json.RawMessage) map[string]any {
	var u map[string]any
	if json.Unmarshal(raw, &u) != nil {
		return nil
	}
	return u
}

// claudeNumber decodes a JSON number, and gives nil for anything else.
func claudeNumber(raw json.RawMessage) any {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

// claudeCount reads a token count or a status, and gives 0 for anything
// that isn't a plausible one.
func claudeCount(v any) int64 {
	f, ok := v.(float64)
	if !ok || f < 0 || f > 1e12 || f != math.Trunc(f) {
		return 0
	}
	return int64(f)
}

// claudeText decodes a JSON string, and gives "" for anything else.
func claudeText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// claudeMessage reads error text given as a string, and otherwise keeps the
// raw JSON, to classify only.
func claudeMessage(raw json.RawMessage) string {
	if s := claudeText(raw); s != "" {
		return s
	}
	return string(raw)
}

// The failure wording, lowercased, as v1 reads claude's (internal/update/
// classify.go). Claude names these classes in no documented field, so where
// no field of the envelope decides, a failed run is named from its error
// text. A false auth holds the host for the day, so each term is a phrase
// seen in the binary, not a loose word. Text that no term matches is host.
// The terms v1 keeps for Codex alone are left out.
var (
	// classify.go:18-19, authTerms, but the other host's sign-in command.
	claudeAuthTerms = []string{"authenticat", "unauthorized", "not logged in", "/login", "claude auth login",
		"log in", "sign in", "api key", "token expired", "token has expired", "token revoked", "token has been revoked"}
	// classify.go:20-22, rateTerms.
	claudeRateTerms = []string{"rate limit", "rate_limit", "ratelimit", "rate-limit", "usage limit", "usage_limit",
		"hit your limit", "spend limit", "spend cap", "shared budget", "out of extra usage", "too many requests",
		"quota exceeded", "quota_exceeded", "overloaded", "at capacity", "out of credits", "credit balance"}
	// classify.go:27-43, hostTerms: claude's DNS, connection, TLS and
	// stream failures (28-32), a server's status as claude words it
	// (35-37), commander's refusal of an option (41) and claude's refusal of
	// --strict-mcp-config where an organization manages MCP servers (42).
	claudeHostTerms = []string{
		"getaddrinfo enotfound", "unable to connect to api", "can't reach the api server", "connection refused",
		"no internet route", "connection dropped", "connection lost", "connection to the api was lost",
		"connection closed before the response", "couldn't connect through your proxy", "no response from api",
		"request timed out", "stream ended without receiving any events",
		"server-side issue", "api error: 500", "api error: 502", "api error: 503", "api error: 504",
		"500 internal server error", "502 bad gateway", "503 service unavailable", "504 gateway timeout",
		"unknown option '", claudeStrictMCPRefusal,
	}
	// classify.go:44-48, refusalTerms: a refusal counts only on a line that
	// names the model or the effort; claude words an effort a model can't
	// take with thinking off "isn't available".
	claudeRefusalTerms = []string{"not exist", "not found", "not_found", "not supported", "unsupported", "invalid",
		"unknown", "not available", "isn't available", "access", "issue with"}
)

// claudeStrictMCPRefusal is claude's refusal of --strict-mcp-config where an
// organization manages MCP servers (classify.go:42, 113-117).
const claudeStrictMCPRefusal = "cannot use --strict-mcp-config"

// claudeDiag gathers a run's error text (v1's diagnostics). A line that
// names MCP is an MCP server's, whose own words, such as "authentication
// required", can't pass for claude's; a line about --strict-mcp-config is
// claude's own word on its command line (classify.go:65-78).
type claudeDiag struct {
	text   strings.Builder // lowercased lines, at most claudeMaxDiag bytes
	effort string          // the attempt's effort flag, which a refusal may quote
}

func (d *claudeDiag) note(s string) {
	for _, line := range strings.Split(strings.ToLower(s), "\n") {
		if strings.Contains(line, "mcp") && !strings.Contains(line, "--strict-mcp-config") {
			continue
		}
		if line = strings.TrimSpace(line); line != "" && d.text.Len()+len(line) < claudeMaxDiag {
			d.text.WriteString(line + "\n")
		}
	}
}

// class names a failed run, as v1's reason does (classify.go:85-101): a
// sign-in problem first, then a limit, then a network, server or command
// line failure, then a refused model or effort, which is host with Refused
// set. An outage that names the model is still an outage. Anything else is
// host.
func (d *claudeDiag) class() (Class, string) {
	text := d.text.String()
	lines := strings.Split(text, "\n")
	switch {
	case claudeContainsAny(text, claudeAuthTerms):
		return ClassAuth, ""
	case claudeContainsAny(text, claudeRateTerms):
		return ClassRateLimit, ""
	case slices.ContainsFunc(lines, func(line string) bool { return claudeContainsAny(line, claudeHostTerms) }):
		return ClassHost, ""
	case slices.ContainsFunc(lines, claudeRefusal):
		if d.refusesEffort() {
			return ClassHost, RefusedEffort
		}
		return ClassHost, RefusedModel
	}
	return ClassHost, ""
}

// refusesStrictMCP reports claude's refusal of --strict-mcp-config
// (classify.go:113-117).
func (d *claudeDiag) refusesStrictMCP() bool {
	return strings.Contains(d.text.String(), claudeStrictMCPRefusal)
}

// refusesEffort reports whether a refusal names the effort: the word, or the
// run's value in quotes (classify.go:119-127).
func (d *claudeDiag) refusesEffort() bool {
	quoted := "'" + strings.ToLower(d.effort) + "'"
	return slices.ContainsFunc(strings.Split(d.text.String(), "\n"), func(line string) bool {
		return claudeRefusal(line) && (strings.Contains(line, "effort") || d.effort != "" && strings.Contains(line, quoted))
	})
}

// claudeRefusal is a refusal on a line that names the model or the effort
// (classify.go:129-131).
func claudeRefusal(line string) bool {
	return (strings.Contains(line, "model") || strings.Contains(line, "effort")) && claudeContainsAny(line, claudeRefusalTerms)
}

func claudeContainsAny(s string, terms []string) bool {
	return slices.ContainsFunc(terms, func(t string) bool { return strings.Contains(s, t) })
}
