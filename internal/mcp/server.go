// Package mcp is eva-md's read-only MCP server. The hosts start it once a
// chat, from the plugins' .mcp.json, as eva-md mcp --host <host>, and it
// answers newline-delimited JSON-RPC on stdin and stdout until stdin ends.
// Its three tools wrap eva-md search, eva-md show and a status of the
// project's memory. It writes no file, takes no lock and starts no model.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/paths"
)

// Server is one run of the MCP server.
type Server struct {
	Host    string     // claude-code or codex
	Dirs    paths.Dirs // eva-md's folders
	DirsErr error      // why the home folder is unavailable; every call then fails
	Version string     // the binary's, for serverInfo
	Now     func() time.Time
	Getenv  func(string) string
	// Getwd is the folder the host started the server in, which calls
	// search unless they name another; "" when it is unknown.
	Getwd string
	// Parent is the server's parent process at start. Once its parent
	// changes, as when the host died and the server was handed to launchd,
	// the server ends. 0 or 1 turns the watch off.
	Parent  int
	Getppid func() int    // os.Getppid, unless a test replaces it
	Tick    time.Duration // how often the parent is checked; 5 s when 0

	// Tests replace the tools, their deadlines, the grace past them, the
	// wait for the chat's SessionStart hook, the time a judgement of the
	// warning has and the wait for the call in hand at the end.
	tools     map[string]handler
	deadlines map[string]time.Duration
	grace     time.Duration
	beatWait  time.Duration
	judgeTime time.Duration
	endWait   time.Duration
}

// handler answers one tool call.
type handler func(ctx context.Context, s *server, args arguments) result

// result is a tool call's answer: one text, which is an error the model
// can act on when IsError is set.
type result struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func text(s string) result    { return result{Content: []content{{Type: "text", Text: s}}} }
func failure(s string) result { r := text(s); r.IsError = true; return r }

// The JSON-RPC error codes the server answers with.
const (
	codeParse   = -32700 // not JSON, or a line over maxLine
	codeInvalid = -32600 // JSON, but no request
	codeMethod  = -32601 // a method the server doesn't have
	codeParams  = -32602 // tools/call without an object or a tool's name
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// protocolVersions are the MCP revisions the server speaks. It answers a
// client with the one it asks for, or with defaultVersion.
var protocolVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}

const defaultVersion = "2025-06-18"

// null is the id of an answer to a request whose id can't be read.
var null = json.RawMessage("null")

// errCancelled ends a call the host cancelled; it gets no answer.
var errCancelled = errors.New("mcp: the host cancelled the call")

// server is the state of one Serve.
type server struct {
	Server
	out, errOut io.Writer
	base        context.Context // ends when Serve does
	queue       chan *call
	done        chan int // the exit code, sent once
	writeMu     sync.Mutex
	mu          sync.Mutex
	calls       map[string]*call // the calls queued or running, by id
	left        int              // calls left running past their deadline
	start       time.Time
}

// call is one tools/call request.
type call struct {
	id     json.RawMessage
	name   string
	args   arguments
	ctx    context.Context
	cancel context.CancelCauseFunc
}

// Serve answers MCP requests from in on out, one JSON-RPC message a line,
// until in ends (0), out can't be written (0), the parent process changes
// (0) or maxAbandoned calls are left running past their deadline (1). The
// reader answers initialize, ping, tools/list and errors at once, and hands
// each tools/call to one worker, which runs them in order, each under its
// deadline. Only the abandoned calls write to errOut, a count each.
func Serve(ctx context.Context, s Server, in io.Reader, out, errOut io.Writer) int {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Getenv == nil {
		s.Getenv = os.Getenv
	}
	if s.Getppid == nil {
		s.Getppid = os.Getppid
	}
	if s.Tick <= 0 {
		s.Tick = parentTick
	}
	if s.grace <= 0 {
		s.grace = grace + Slack
	}
	if s.beatWait == 0 {
		s.beatWait = beatWait
	}
	if s.judgeTime <= 0 {
		s.judgeTime = judgeTime + Slack
	}
	if s.endWait <= 0 {
		s.endWait = endWait
	}
	base, stop := context.WithCancel(ctx)
	defer stop()
	srv := &server{Server: s, out: out, errOut: errOut, base: base, queue: make(chan *call, maxQueue), done: make(chan int, 1), calls: map[string]*call{}, start: s.Now()}
	worker := make(chan struct{})
	go func() {
		defer close(worker)
		srv.work()
	}()
	go srv.read(in)
	if s.Parent > 1 {
		go srv.watch()
	}
	code := 0
	select {
	case code = <-srv.done:
	case <-ctx.Done():
	}
	// Every call sees its context end. The one in hand gets a moment to
	// return, and nothing more: a call the server ends answers nothing.
	stop()
	select {
	case <-worker:
	case <-time.After(s.endWait):
	}
	return code
}

// end ends Serve with code, unless it is ending already.
func (s *server) end(code int) {
	select {
	case s.done <- code:
	default:
	}
}

// read handles in's lines until it ends. A panic here ends the server.
func (s *server) read(in io.Reader) {
	defer func() {
		if recover() != nil {
			s.end(1)
		}
	}()
	r := bufio.NewReaderSize(in, 64<<10)
	var buf []byte
	for {
		line, long, err := readLine(r, &buf)
		if s.base.Err() != nil {
			return
		}
		switch {
		case long:
			s.reply(null, nil, &rpcError{codeParse, "the request line is over 1 MiB"})
		case len(bytes.TrimSpace(line)) > 0:
			s.handle(line)
		}
		if err != nil {
			s.end(0)
			return
		}
	}
}

// readLine reads one line without its line end, into *buf. A line over
// maxLine bytes is read to its end and dropped, with long set. At the end
// of the input err is io.EOF, with the last line if it has no line end.
func readLine(r *bufio.Reader, buf *[]byte) (line []byte, long bool, err error) {
	*buf = (*buf)[:0]
	for {
		frag, rerr := r.ReadSlice('\n')
		if !long && len(*buf)+len(frag) > maxLine+2 {
			long, *buf = true, (*buf)[:0]
		}
		if !long {
			*buf = append(*buf, frag...)
		}
		if errors.Is(rerr, bufio.ErrBufferFull) {
			continue
		}
		line = bytes.TrimSuffix(bytes.TrimSuffix(*buf, []byte("\n")), []byte("\r"))
		if long || len(line) > maxLine {
			return nil, true, rerr
		}
		return line, false, rerr
	}
}

// handle answers one message. A request has a string or number id; one
// without, or with a null id, is a notification and gets no answer. A
// response gets none either: the server asks the client nothing.
func (s *server) handle(line []byte) {
	if !json.Valid(line) {
		s.reply(null, nil, &rpcError{codeParse, "the request is not valid JSON"})
		return
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(line, &msg) != nil {
		// JSON that is no object, such as a batch, which MCP dropped in
		// its 2025-06-18 revision.
		s.reply(null, nil, &rpcError{codeInvalid, "the request is not a JSON object"})
		return
	}
	id, ok := requestID(msg["id"])
	if !ok {
		s.reply(null, nil, &rpcError{codeInvalid, "the request id is not a string or a number"})
		return
	}
	answerTo := id
	if answerTo == nil {
		answerTo = null
	}
	var version, method string
	if raw, ok := msg["jsonrpc"]; ok && (json.Unmarshal(raw, &version) != nil || version != "2.0") {
		s.reply(answerTo, nil, &rpcError{codeInvalid, `jsonrpc must be "2.0"`})
		return
	}
	raw, ok := msg["method"]
	if !ok {
		if _, isResult := msg["result"]; isResult {
			return
		}
		if _, isError := msg["error"]; isError {
			return
		}
		s.reply(answerTo, nil, &rpcError{codeInvalid, "the request has no method"})
		return
	}
	if json.Unmarshal(raw, &method) != nil {
		s.reply(answerTo, nil, &rpcError{codeInvalid, "the method is not a string"})
		return
	}
	params := msg["params"]
	switch {
	case method == "notifications/cancelled":
		s.cancel(params)
		return
	case id == nil:
		// notifications/initialized and any other notification.
		return
	}
	switch method {
	case "initialize":
		s.reply(id, s.initialize(params), nil)
	case "ping":
		s.reply(id, struct{}{}, nil)
	case "tools/list":
		list := toolsJSON()
		if s.off() != "" {
			list = noTools
		}
		s.reply(id, list, nil)
	case "tools/call":
		s.enqueue(id, params)
	case "resources/list":
		s.reply(id, map[string][]any{"resources": {}}, nil)
	case "prompts/list":
		s.reply(id, map[string][]any{"prompts": {}}, nil)
	default:
		s.reply(id, nil, &rpcError{codeMethod, "method not found"})
	}
}

// requestID reads a message's id: nil for a notification, which has none
// or a null one, and the id's bytes for a string or a number, echoed as
// they came.
func requestID(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true
	}
	if raw[0] == '"' || raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9' {
		return raw, true
	}
	return nil, false
}

// reply writes one answer as one line, in one write. A failed write means
// the host is gone, which ends the server.
func (s *server) reply(id json.RawMessage, value any, rerr *rpcError) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(response{JSONRPC: "2.0", ID: id, Result: value, Error: rerr}); err != nil {
		b.Reset()
		_ = enc.Encode(response{JSONRPC: "2.0", ID: id, Result: failure("eva-md: internal error")})
	}
	s.writeMu.Lock()
	_, err := s.out.Write(b.Bytes())
	s.writeMu.Unlock()
	if err != nil {
		s.end(0)
	}
}

// initialize answers with the protocol version the client asked for, when
// the server speaks it, the tools capability and the instructions.
func (s *server) initialize(params json.RawMessage) any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name string `json:"name"`
		} `json:"clientInfo"`
	}
	_ = json.Unmarshal(params, &p)
	version := defaultVersion
	if slices.Contains(protocolVersions, p.ProtocolVersion) {
		version = p.ProtocolVersion
	}
	r := map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]bool{"listChanged": false}},
		"serverInfo":      map[string]string{"name": "eva-md", "version": s.Version},
	}
	if text := s.instructions(p.ClientInfo.Name != health.DoctorClient); text != "" {
		r["instructions"] = text
	}
	return r
}

// enqueue checks a tools/call request and hands it to the worker. A call
// without an object, or with a tool the server doesn't have, is a protocol
// error; anything wrong with an argument is the tool's to answer.
func (s *server) enqueue(id, params json.RawMessage) {
	var p map[string]json.RawMessage
	if json.Unmarshal(params, &p) != nil || p == nil {
		s.reply(id, nil, &rpcError{codeParams, "tools/call takes an object with the tool's name and arguments"})
		return
	}
	var name string
	if json.Unmarshal(p["name"], &name) != nil || s.handler(name) == nil {
		s.reply(id, nil, &rpcError{codeParams, "unknown tool; the tools are eva_search, eva_show and eva_status"})
		return
	}
	args := arguments{}
	if raw, ok := p["arguments"]; ok && string(raw) != "null" && (json.Unmarshal(raw, &args) != nil || args == nil) {
		s.reply(id, nil, &rpcError{codeParams, "arguments must be an object"})
		return
	}
	c := &call{id: id, name: name, args: args}
	c.ctx, c.cancel = context.WithCancelCause(s.base)
	s.mu.Lock()
	s.calls[string(id)] = c
	s.mu.Unlock()
	select {
	case s.queue <- c:
	default:
		s.forget(c)
		s.reply(id, failure("eva-md: too many calls are waiting; try again"), nil)
	}
}

// cancel ends the call notifications/cancelled names, queued or running.
func (s *server) cancel(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	s.mu.Lock()
	c := s.calls[string(p.RequestID)]
	s.mu.Unlock()
	if c != nil {
		c.cancel(errCancelled)
	}
}

// forget drops a call that is done.
func (s *server) forget(c *call) {
	c.cancel(nil)
	s.mu.Lock()
	if s.calls[string(c.id)] == c {
		delete(s.calls, string(c.id))
	}
	s.mu.Unlock()
}

// handler returns the tool name names, or nil.
func (s *server) handler(name string) handler {
	if s.tools != nil {
		return s.tools[name]
	}
	switch name {
	case "eva_search":
		return search
	case "eva_show":
		return show
	case "eva_status":
		return status
	}
	return nil
}

// deadline is how long a call of the tool name may take.
func (s *server) deadline(name string) time.Duration {
	if d, ok := s.deadlines[name]; ok {
		return d
	}
	switch name {
	case "eva_search":
		return searchDeadline + Slack
	case "eva_show":
		return showDeadline + Slack
	}
	return statusDeadline + Slack
}

// work runs the queued calls one at a time, in order.
func (s *server) work() {
	for {
		select {
		case <-s.base.Done():
			return
		case c := <-s.queue:
			s.run(c)
		}
	}
}

// run runs one call in a goroutine of its own, under its deadline, and
// answers with its result. A call still running at its deadline plus the
// grace is answered for and left running with its context ended, so a
// handler stuck in a system call never holds the calls after it. A call the
// host cancelled, or that the server's end cut short, answers nothing, and
// one the host cancelled that is still running the grace after is left
// running too.
func (s *server) run(c *call) {
	defer s.forget(c)
	if c.ctx.Err() != nil {
		return
	}
	limit := s.deadline(c.name)
	ctx, stop := context.WithTimeout(c.ctx, limit)
	defer stop()
	results := make(chan result, 1)
	tool := s.handler(c.name)
	go func() {
		defer func() {
			if recover() != nil {
				results <- failure("eva-md: internal error")
			}
		}()
		results <- tool(ctx, s, c.args)
	}()
	timer := time.NewTimer(limit + s.grace)
	defer timer.Stop()
	select {
	case r := <-results:
		if c.ctx.Err() == nil {
			s.reply(c.id, r, nil)
		}
	case <-c.ctx.Done():
		if s.base.Err() != nil {
			return
		}
		select {
		case <-results:
		case <-time.After(s.grace):
			s.abandon(results, "was cancelled")
		}
	case <-timer.C:
		s.reply(c.id, failure("eva-md: the call passed its time limit"), nil)
		s.abandon(results, "passed its time limit")
	}
}

// abandon counts a call left running until its handler returns; why says
// what ended it. Once maxAbandoned are left, the server ends: the hosts
// don't restart it, and the hooks, the brief and the command line go on
// without it.
func (s *server) abandon(results <-chan result, why string) {
	s.mu.Lock()
	s.left++
	left := s.left
	s.mu.Unlock()
	go func() {
		<-results
		s.mu.Lock()
		s.left--
		s.mu.Unlock()
	}()
	fmt.Fprintf(s.errOut, "eva-md mcp: a call %s and was left running; %d left running\n", why, left)
	if left >= maxAbandoned {
		fmt.Fprintf(s.errOut, "eva-md mcp: %d calls are still running past their time limit or a cancel; stopping\n", left)
		s.end(1)
	}
}

// watch ends the server once its parent process changes.
func (s *server) watch() {
	t := time.NewTicker(s.Tick)
	defer t.Stop()
	for {
		select {
		case <-s.base.Done():
			return
		case <-t.C:
			if s.Getppid() != s.Parent {
				s.end(0)
				return
			}
		}
	}
}
