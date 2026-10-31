package update

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strings"
)

// Failure wording, lowercased, as claude 2.1.312 and codex 0.166.0 word it.
// Neither CLI reports these classes in a documented field, so a failed run is
// named from its error text; eva-md doctor --live shows the class a real
// failure of each installed host gets. A false auth pauses every update
// for the day, so each term is a phrase seen in the binaries, not a loose
// word such as "login", "credential", "quota" or "limit reached".
var (
	authTerms = []string{"authenticat", "unauthorized", "not logged in", "/login", "claude auth login", "codex login",
		"log in", "sign in", "api key", "token expired", "token has expired", "token revoked", "token has been revoked"}
	rateTerms = []string{"rate limit", "rate_limit", "ratelimit", "rate-limit", "usage limit", "usage_limit",
		"hit your limit", "spend limit", "spend cap", "shared budget", "out of extra usage", "too many requests",
		"quota exceeded", "quota_exceeded", "overloaded", "at capacity", "out of credits", "credit balance"}
	// A network, server or CLI-usage failure, each a phrase the host prints
	// for that failure only, never a word ordinary error text holds, such as
	// "timed out", "server" or "invalid". Claude's network errors follow
	// "API Error: "; a 529 or a 429 is a limit, caught above.
	hostTerms = []string{
		// claude: DNS, connection, TLS and stream failures
		"getaddrinfo enotfound", "unable to connect to api", "can't reach the api server", "connection refused",
		"no internet route", "connection dropped", "connection lost", "connection to the api was lost",
		"connection closed before the response", "couldn't connect through your proxy", "no response from api",
		"request timed out", "stream ended without receiving any events",
		// codex: a dropped stream, and reqwest's failure to send at all
		"stream disconnected", "error sending request",
		// a server error, as claude and codex word the status
		"server-side issue", "api error: 500", "api error: 502", "api error: 503", "api error: 504",
		"500 internal server error", "502 bad gateway", "503 service unavailable", "504 gateway timeout",
		// a command line the CLI no longer takes: commander (claude), clap
		// (codex), codex's own feature check, and claude's refusal of
		// --strict-mcp-config where an organization manages MCP servers
		"unknown option '", "unexpected argument '", "unknown feature flag", "error parsing -c overrides",
		"cannot use --strict-mcp-config",
	}
	// A refusal counts only on a line that names the model or the effort.
	// "not_found" is the API's own error type for an unknown model; claude
	// words an effort a model can't take with thinking off "isn't available".
	refusalTerms = []string{"not exist", "not found", "not_found", "not supported", "unsupported", "invalid",
		"unknown", "not available", "isn't available", "access", "issue with"}
	// Codex words an MCP server that didn't come up as failed to start, timed
	// out, or not initialized.
	mcpFailureTerms = []string{"fail", "timed out", "not initialized", "interrupted", "incomplete", "error"}
)

// diagnostics sorts a run's error text into MCP lines and the rest. The rest
// names a failed run's reason. MCP lines only tell whether an MCP server
// failed, so an optional server's own error words, such as "authentication
// required", can't pass for the host's.
type diagnostics struct {
	text        strings.Builder // lowercased lines without "mcp" or about --strict-mcp-config, at most maxDiag bytes
	mcpFailed   bool            // an MCP line reports a failure
	mcpRequired bool            // codex's "required MCP servers failed to initialize"
	effort      string          // the attempt's effort flag, which a refusal may quote
}

// note sorts s into lines. A line about the --strict-mcp-config flag eva-md
// passes is claude's own word on the command line, not an MCP server's.
func (d *diagnostics) note(s string) {
	for _, line := range strings.Split(strings.ToLower(s), "\n") {
		if strings.Contains(line, "mcp") && !strings.Contains(line, "--strict-mcp-config") {
			d.mcpFailed = d.mcpFailed || containsAny(line, mcpFailureTerms)
			d.mcpRequired = d.mcpRequired || strings.Contains(line, "required mcp")
			continue
		}
		if line = strings.TrimSpace(line); line != "" && d.text.Len()+len(line) < maxDiag {
			d.text.WriteString(line + "\n")
		}
	}
}

// reason names a failed run: a login problem first, then a limit, then a
// network, server or CLI-usage failure, then a refused model or effort. An
// outage that names the model, such as a 503 "model not available", is
// still an outage, not a refusal. A failed required MCP server explains the
// rest; anything else is invalid_output.
func (d *diagnostics) reason() string {
	text := d.text.String()
	lines := strings.Split(text, "\n")
	switch {
	case containsAny(text, authTerms):
		return ReasonAuth
	case containsAny(text, rateTerms):
		return ReasonRateLimit
	case slices.ContainsFunc(lines, hostFailure):
		return ReasonHostError
	case slices.ContainsFunc(lines, refusesModel):
		return ReasonModelRejected
	case d.mcpFailed && d.mcpRequired:
		return ReasonMCPFailed
	}
	return ReasonInvalidOutput
}

// hostFailure reports a network, server or CLI-usage failure on one line.
// A stream codex ended because the answer was filtered or ran too long
// ("Incomplete response returned, reason: content_filter") is the batch's
// fault, not the host's, so it stays invalid_output and counts toward the
// session's quarantine.
func hostFailure(line string) bool {
	return (containsAny(line, hostTerms) || invalidFlagValue(line)) && !strings.Contains(line, "incomplete response returned")
}

// refusesStrictMCP reports claude's refusal of --strict-mcp-config, which it
// gives where an organization manages MCP servers.
func (d *diagnostics) refusesStrictMCP() bool {
	return strings.Contains(d.text.String(), "cannot use --strict-mcp-config")
}

// refusesEffort reports whether a refusal names the effort: the word, or the
// attempt's value in quotes, as codex quotes it ("Unsupported value: 'ultra'
// is not supported with the 'gpt-x' model."). A refusal that names only the
// model refuses the model.
func (d *diagnostics) refusesEffort() bool {
	quoted := "'" + d.effort + "'"
	return slices.ContainsFunc(strings.Split(d.text.String(), "\n"), func(line string) bool {
		return refusesModel(line) && (strings.Contains(line, "effort") || d.effort != "" && strings.Contains(line, quoted))
	})
}

func refusesModel(line string) bool {
	return (strings.Contains(line, "model") || strings.Contains(line, "effort")) && containsAny(line, refusalTerms)
}

// invalidFlagValue is clap's "invalid value 'x' for '--flag <VALUE>'", whose
// two parts must share a line: "invalid value" alone is ordinary text.
func invalidFlagValue(line string) bool {
	i := strings.Index(line, "invalid value '")
	return i >= 0 && strings.Contains(line[i:], "' for '--")
}

func containsAny(s string, terms []string) bool {
	return slices.ContainsFunc(terms, func(t string) bool { return strings.Contains(s, t) })
}

// text decodes a JSON string, and gives "" for anything else.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// message reads an error message given as a string or as an object with a
// message field, and otherwise keeps the raw JSON.
func message(raw json.RawMessage) string {
	if s := text(raw); s != "" {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		if s := text(obj["message"]); s != "" {
			return s
		}
	}
	return string(raw)
}

// object returns data without surrounding space when it is one JSON object.
func object(data []byte) ([]byte, bool) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' || !json.Valid(data) {
		return nil, false
	}
	return data, true
}

func isObject(data []byte) bool {
	_, ok := object(data)
	return ok
}

// count reads a token count, ignoring anything that isn't a plausible one.
func count(v any) int {
	f, ok := v.(float64)
	if !ok || f < 0 || f > 1e12 || f != math.Trunc(f) {
		return 0
	}
	return int(f)
}
