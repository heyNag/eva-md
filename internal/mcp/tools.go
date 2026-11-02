package mcp

import (
	"bytes"
	"encoding/json"
	"sync"
	"time"
)

// The bounds of a call and of the server (docs/design.md, "MCP server").
const (
	// searchCap, showCap and statusCap bound a call's text, in bytes, under
	// the 25,000 tokens Claude Code takes from an MCP result by default;
	// showCap bounds the text under eva_show's label.
	searchCap = 24 << 10
	showCap   = 24 << 10
	statusCap = 4 << 10

	// The deadlines of the calls: search and show as the command line's,
	// status well under them.
	searchDeadline = 5 * time.Second
	showDeadline   = 5 * time.Second
	statusDeadline = 3 * time.Second
	// grace is how long past its deadline a call may run before it is
	// answered for and left running.
	grace = time.Second

	// maxAbandoned is how many calls left running past their deadline end
	// the server.
	maxAbandoned = 8
	// maxLine bounds one request line.
	maxLine = 1 << 20
	// maxQueue is how many tool calls may wait for the one before them.
	maxQueue = 64
	// maxInstructions bounds the instructions, in characters.
	maxInstructions = 600
	// parentTick is how often the server checks that its host is still its
	// parent.
	parentTick = 5 * time.Second
	// beatWait is how long the instructions wait for this chat's own
	// SessionStart hook before judging the hooks (see warning).
	beatWait = 1500 * time.Millisecond
	// judgeTime is how long one judgement of the warning may read its files.
	judgeTime = 100 * time.Millisecond
	// endWait is how long the end of Serve waits for the call in hand.
	endWait = 500 * time.Millisecond
)

// Slack lengthens the server's time limits: each call's deadline and the
// grace past it, and each judgement of the warning. Tests set it, before any
// test runs, so a busy machine can't fail a test of what a call answers or
// what the instructions say. It is zero in production.
var Slack time.Duration

// annotations are the hints every tool carries: they read and change
// nothing, and reach nothing outside this Mac.
type annotations struct {
	ReadOnly    bool `json:"readOnlyHint"`
	Destructive bool `json:"destructiveHint"`
	Idempotent  bool `json:"idempotentHint"`
	OpenWorld   bool `json:"openWorldHint"`
}

// readOnly are the annotations of every tool.
var readOnly = annotations{ReadOnly: true, Idempotent: true}

// tool is a tool as tools/list describes it.
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations annotations     `json:"annotations"`
}

// cwdProperty is the argument eva_search and eva_status share.
const cwdProperty = `"cwd":{"type":"string","description":"The project's folder."}`

// tools are the three tools, in the order tools/list gives them. Their
// schemas are written out so the list's bytes stay fixed (see
// testdata/tools.golden.json).
var tools = []tool{
	{
		Name:        "eva_search",
		Description: "Find earlier chats and wiki page lines (threads, systems, knowledge) by terms. Hits carry refs for eva_show. On a miss, retry with other words, paths or dates.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"terms":{"type":"array","items":{"type":"string","minLength":1,"maxLength":256},"minItems":1,"maxItems":16,"description":"Words; an item with a space is a phrase."},` +
			`"all":{"type":"boolean","description":"Search every project."},` +
			`"days":{"type":"integer","minimum":0,"description":"Days back, 90 by default; 0 for all."},` +
			`"limit":{"type":"integer","minimum":1,"maximum":50,"description":"Hits, 10 by default."},` +
			cwdProperty + `},"required":["terms"],"additionalProperties":false}`),
		Annotations: readOnly,
	},
	{
		Name:        "eva_show",
		Description: "Read a wiki page or section (systems/map, knowledge/map, <thread>#Now, #Timeline, #YYYY-MM-DD, #Heading) or a source ref s:<id>#<line> from eva_search.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"ref":{"type":"string","minLength":1,"maxLength":1024,"description":"A ref eva_search printed, or page#section."},` +
			`"last":{"type":"integer","minimum":1,"description":"#Timeline only: newest N date groups."},` +
			`"since":{"type":"string","description":"#Timeline only: groups since YYYY-MM-DD."}` +
			`},"required":["ref"],"additionalProperties":false}`),
		Annotations: readOnly,
	},
	{
		Name:        "eva_status",
		Description: "This project's memory: thread page and its Now date, pending update, systems, knowledge, hook and update health, what to read next.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + cwdProperty + `},"additionalProperties":false}`),
		Annotations: readOnly,
	},
}

// toolsJSON is the result of tools/list while the tools are on, built at
// the first request, so a hook, which runs the same binary, never builds it.
var toolsJSON = sync.OnceValue(func() json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string][]tool{"tools": tools}); err != nil {
		panic(err)
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
})

// noTools is the result of tools/list while the tools are off: an empty
// list, never null.
var noTools = json.RawMessage(`{"tools":[]}`)
