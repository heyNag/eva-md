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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/testgitenv"
	"github.com/heyNag/eva-md/internal/testlimits"
)

// Tests check what the server answers and what its instructions say, not how
// fast the machine runs git or reads its files: the time limits that protect
// a host from either are lengthened for every test, before any test runs. A
// test of a limit itself sets a short one on its own server.
func TestMain(m *testing.M) {
	testgitenv.Clear()
	testlimits.Raise()
	Slack = testlimits.Long
	os.Exit(m.Run())
}

// hangGuard is how long a test waits for what must happen however slow the
// machine, such as an answer, a call starting or the end of Serve, before it
// reports a hang.
const hangGuard = time.Minute

// await waits for ch to deliver, and fails the test, naming what it waited
// for, when it hasn't after hangGuard.
func await(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(hangGuard):
		t.Fatalf("still waiting for %s after %v", what, hangGuard)
	}
}

// isolated gives a test its own home and git config.
func isolated(t *testing.T) paths.Dirs {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("EVA_MD_INNER", "")
	d, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// lockedBuffer is a buffer the server's goroutines may write while a test
// reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// session is one Serve over pipes: a test writes request lines and reads
// each answer as it comes.
type session struct {
	t      *testing.T
	in     *io.PipeWriter
	lines  chan string
	errOut *lockedBuffer
	code   chan int
}

func serve(t *testing.T, s Server) *session {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ss := &session{t: t, in: inW, lines: make(chan string, 256), errOut: &lockedBuffer{}, code: make(chan int, 1)}
	go func() {
		ss.code <- Serve(context.Background(), s, inR, outW, ss.errOut)
		outW.Close()
	}()
	go func() {
		r := bufio.NewReader(outR)
		for {
			line, err := r.ReadString('\n')
			if line != "" {
				ss.lines <- line
			}
			if err != nil {
				close(ss.lines)
				return
			}
		}
	}()
	t.Cleanup(func() { inW.Close() })
	return ss
}

func (ss *session) send(lines ...string) {
	ss.t.Helper()
	for _, l := range lines {
		if _, err := io.WriteString(ss.in, l+"\n"); err != nil {
			ss.t.Fatal(err)
		}
	}
}

// reply is one answer, as the server writes it.
type reply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
	line    string
}

// tool reads a tools/call answer.
func (r reply) tool(t *testing.T) result {
	t.Helper()
	var res result
	if err := json.Unmarshal(r.Result, &res); err != nil || len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Fatalf("not a tool result: %s", r.line)
	}
	return res
}

// next returns the next answer, which must be one valid JSON line.
func (ss *session) next() reply {
	ss.t.Helper()
	select {
	case line, ok := <-ss.lines:
		if !ok {
			ss.t.Fatal("the server ended")
		}
		var r reply
		if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 || json.Unmarshal([]byte(line), &r) != nil || r.JSONRPC != "2.0" {
			ss.t.Fatalf("not one JSON-RPC line: %q", line)
		}
		r.line = line
		return r
	case <-time.After(hangGuard):
		ss.t.Fatalf("no answer after %v: the server hung", hangGuard)
	}
	return reply{}
}

// ended waits for Serve's exit code.
func (ss *session) ended() int {
	ss.t.Helper()
	select {
	case code := <-ss.code:
		return code
	case <-time.After(hangGuard):
		ss.t.Fatalf("Serve had not ended after %v: it hung", hangGuard)
	}
	return -1
}

// rest returns the answers the test has not read, once Serve has ended:
// its output is closed then, so every answer it wrote is there.
func (ss *session) rest() []string {
	ss.t.Helper()
	var lines []string
	for {
		select {
		case line, ok := <-ss.lines:
			if !ok {
				return lines
			}
			lines = append(lines, line)
		case <-time.After(hangGuard):
			ss.t.Fatalf("Serve's output was still open after %v", hangGuard)
		}
	}
}

func request(id any, method string, params any) string {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func notification(method string, params any) string {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		m["params"] = params
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func callTool(id any, name string, args any) string {
	return request(id, "tools/call", map[string]any{"name": name, "arguments": args})
}

// testServer is a server for protocol tests, with an isolated home.
func testServer(t *testing.T) Server {
	d := isolated(t)
	return Server{Host: "codex", Dirs: d, Version: "test", beatWait: time.Millisecond}
}

// initialize echoes each protocol revision the server speaks and answers
// 2025-06-18 to any other, with the tools capability, no list changes, its
// name and the instructions.
func TestInitialize(t *testing.T) {
	ss := serve(t, testServer(t))
	for asked, want := range map[string]string{"2024-11-05": "2024-11-05", "2025-03-26": "2025-03-26", "2025-06-18": "2025-06-18", "2025-11-25": "2025-11-25", "2099-01-01": "2025-06-18", "": "2025-06-18"} {
		ss.send(request(1, "initialize", map[string]any{"protocolVersion": asked, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "test"}}))
		var r struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools map[string]bool `json:"tools"`
			} `json:"capabilities"`
			ServerInfo   struct{ Name, Version string } `json:"serverInfo"`
			Instructions string                         `json:"instructions"`
		}
		if err := json.Unmarshal(ss.next().Result, &r); err != nil {
			t.Fatal(err)
		}
		listChanged, ok := r.Capabilities.Tools["listChanged"]
		if r.ProtocolVersion != want || !ok || listChanged || r.ServerInfo.Name != "eva-md" || r.ServerInfo.Version != "test" || r.Instructions != guide {
			t.Fatalf("asked %q: %+v", asked, r)
		}
	}
}

// Notifications get no answer: initialized, a cancel for a call that isn't
// there, and requests without an id. The ping after them is the next
// answer, and the call after it the next: calls run in order, so the
// tools/call without an id would have run and been answered first.
func TestNotificationsGetNoAnswer(t *testing.T) {
	s := testServer(t)
	var runs atomic.Int32
	s.tools = map[string]handler{"eva_status": func(context.Context, *server, arguments) result { runs.Add(1); return text("status") }}
	ss := serve(t, s)
	ss.send(notification("notifications/initialized", nil), notification("notifications/cancelled", map[string]any{"requestId": 99}),
		notification("ping", nil), notification("tools/list", nil), notification("tools/call", map[string]any{"name": "eva_status"}),
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`, notification("notifications/whatever", nil), request(7, "ping", nil))
	if r := ss.next(); string(r.ID) != "7" {
		t.Fatalf("a notification was answered: %s", r.line)
	}
	ss.send(callTool(8, "eva_status", nil))
	if r := ss.next(); string(r.ID) != "8" || runs.Load() != 1 {
		t.Fatalf("a tools/call without an id ran (%d runs): %s", runs.Load(), r.line)
	}
}

// ping answers {}, and resources/list and prompts/list empty lists, though
// the server offers neither.
func TestPingAndEmptyLists(t *testing.T) {
	ss := serve(t, testServer(t))
	for method, want := range map[string]string{"ping": `{}`, "resources/list": `{"resources":[]}`, "prompts/list": `{"prompts":[]}`} {
		ss.send(request(1, method, nil))
		if r := ss.next(); string(r.Result) != want || r.Error != nil {
			t.Fatalf("%s: %s", method, r.line)
		}
	}
}

// Malformed input gets the JSON-RPC errors, with the request's id echoed
// byte for byte when it has one and null otherwise, and the loop goes on.
func TestProtocolErrors(t *testing.T) {
	ss := serve(t, testServer(t))
	long := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", maxLine) + `"}}`
	for _, tc := range []struct {
		line, id string
		code     int
	}{
		{`{"jsonrpc":"2.0","id":"abc","method":"no/such"}`, `"abc"`, codeMethod},
		{`{"jsonrpc":"2.0","id":1.50,"method":"no/such"}`, `1.50`, codeMethod},
		{`{"jsonrpc":`, `null`, codeParse},
		{long, `null`, codeParse},
		{`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, `null`, codeInvalid},
		{`"ping"`, `null`, codeInvalid},
		{`{"jsonrpc":"1.0","id":3,"method":"ping"}`, `3`, codeInvalid},
		{`{"jsonrpc":"2.0","id":{"a":1},"method":"ping"}`, `null`, codeInvalid},
		{`{"jsonrpc":"2.0","id":4}`, `4`, codeInvalid},
		{`{"jsonrpc":"2.0","id":5,"method":7}`, `5`, codeInvalid},
		{callTool(6, "no_such_tool", map[string]any{}), `6`, codeParams},
		{request(7, "tools/call", []int{1}), `7`, codeParams},
		{request(8, "tools/call", nil), `8`, codeParams},
		{request(9, "tools/call", map[string]any{"name": "eva_search", "arguments": []int{1}}), `9`, codeParams},
	} {
		ss.send(tc.line)
		r := ss.next()
		if r.Error == nil || r.Error.Code != tc.code || string(r.ID) != tc.id || r.Result != nil {
			t.Fatalf("%.80s: %s", tc.line, r.line)
		}
		ss.send(request(100, "ping", nil))
		if r := ss.next(); string(r.ID) != "100" || string(r.Result) != "{}" {
			t.Fatalf("after %.80s, ping: %s", tc.line, r.line)
		}
	}
	// A response gets no answer: the server asks the client nothing.
	ss.send(`{"jsonrpc":"2.0","id":11,"result":{}}`, `{"jsonrpc":"2.0","id":12,"error":{"code":1,"message":"x"}}`, "", "  ", request(13, "ping", nil))
	if r := ss.next(); string(r.ID) != "13" {
		t.Fatalf("a response was answered: %s", r.line)
	}
}

// A line may end in \r\n, and the last line need not end at all.
func TestLineEnds(t *testing.T) {
	ss := serve(t, testServer(t))
	if _, err := io.WriteString(ss.in, request(1, "ping", nil)+"\r\n"); err != nil {
		t.Fatal(err)
	}
	if r := ss.next(); string(r.ID) != "1" || string(r.Result) != "{}" {
		t.Fatal(r.line)
	}
	if _, err := io.WriteString(ss.in, request(2, "ping", nil)); err != nil {
		t.Fatal(err)
	}
	ss.in.Close()
	if r := ss.next(); string(r.ID) != "2" {
		t.Fatal(r.line)
	}
	if code := ss.ended(); code != 0 {
		t.Fatal(code)
	}
}

// The end of stdin ends Serve with 0, cancelling a call that is running,
// which then answers nothing.
func TestEndOfInputEndsServe(t *testing.T) {
	s := testServer(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	s.tools = map[string]handler{"eva_status": func(ctx context.Context, _ *server, _ arguments) result {
		close(started)
		<-ctx.Done()
		close(stopped)
		return text("too late")
	}}
	ss := serve(t, s)
	ss.send(callTool(1, "eva_status", nil))
	await(t, started, "the call to start")
	ss.in.Close()
	if code := ss.ended(); code != 0 {
		t.Fatal(code)
	}
	await(t, stopped, "the running call's context to end")
	if rest := ss.rest(); len(rest) != 0 {
		t.Fatalf("answers after the end of stdin: %q", rest)
	}
}

// stuckWriter is a host that stopped reading: its first write blocks until
// the test releases it.
type stuckWriter struct {
	writing, release chan struct{}
	once             sync.Once
}

func (w *stuckWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.writing) })
	<-w.release
	return len(p), nil
}

// Once stdin ends, the call in hand gets a moment to finish and no more: an
// answer stuck in a write the host never reads holds Serve up only for that
// moment, short here, and Serve ends with 0.
func TestEndOfInputEndsServePastAStuckAnswer(t *testing.T) {
	s := testServer(t)
	s.tools = map[string]handler{"eva_status": func(context.Context, *server, arguments) result { return text("status") }}
	s.endWait = 10 * time.Millisecond
	inR, inW := io.Pipe()
	defer inW.Close()
	out := &stuckWriter{writing: make(chan struct{}), release: make(chan struct{})}
	defer close(out.release)
	code := make(chan int, 1)
	go func() { code <- Serve(context.Background(), s, inR, out, io.Discard) }()
	if _, err := io.WriteString(inW, callTool(1, "eva_status", nil)+"\n"); err != nil {
		t.Fatal(err)
	}
	await(t, out.writing, "the call's answer to be written")
	start := time.Now()
	inW.Close()
	select {
	case c := <-code:
		if c != 0 || time.Since(start) < s.endWait {
			t.Fatalf("code %d after %v", c, time.Since(start))
		}
	case <-time.After(hangGuard):
		t.Fatalf("Serve still waited for the stuck answer after %v", hangGuard)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("the host is gone") }

// An answer that can't be written means the host is gone: Serve ends with
// 0, though stdin is still open.
func TestFailedWriteEndsServe(t *testing.T) {
	s := testServer(t)
	inR, inW := io.Pipe()
	defer inW.Close()
	code := make(chan int, 1)
	go func() { code <- Serve(context.Background(), s, inR, failingWriter{}, io.Discard) }()
	if _, err := io.WriteString(inW, request(1, "ping", nil)+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-code:
		if c != 0 {
			t.Fatal(c)
		}
	case <-time.After(hangGuard):
		t.Fatalf("Serve went on writing to a host that is gone: it had not ended after %v", hangGuard)
	}
}

// Once the parent process changes, as when the host died and launchd took
// the server, the first check that sees it ends Serve with 0, though stdin
// is still open, and a check of the same parent ends nothing. Each check
// waits for the test to give it the parent it sees.
func TestParentChangeEndsServe(t *testing.T) {
	s := testServer(t)
	parents := make(chan int)
	s.Parent, s.Getppid, s.Tick = 4242, func() int { return <-parents }, time.Millisecond
	ss := serve(t, s)
	check := func(parent int) {
		t.Helper()
		select {
		case parents <- parent:
		case <-time.After(hangGuard):
			t.Fatalf("the parent had not been checked again after %v", hangGuard)
		}
	}
	// A second check comes only once the server is done with the first.
	check(4242)
	check(4242)
	ss.send(request(1, "ping", nil))
	if r := ss.next(); string(r.ID) != "1" {
		t.Fatal(r.line)
	}
	check(1)
	if code := ss.ended(); code != 0 {
		t.Fatal(code)
	}
}

// A call still running at its deadline plus the grace is answered for and
// left running, and the server goes on; only a count goes to stderr. While
// a call is stuck, a ping answers at once.
func TestStuckCallIsAnsweredAtItsDeadline(t *testing.T) {
	s := testServer(t)
	release := make(chan struct{})
	defer close(release)
	showing := make(chan struct{})
	s.tools = map[string]handler{
		// Only the search's deadline, short here, can answer it.
		"eva_search": func(context.Context, *server, arguments) result { <-release; return text("too late") },
		"eva_status": func(context.Context, *server, arguments) result { return text("status") },
		// The show's deadline, lengthened for the tests, is out of reach.
		"eva_show": func(context.Context, *server, arguments) result { close(showing); <-release; return text("too late") },
	}
	s.deadlines, s.grace = map[string]time.Duration{"eva_search": 10 * time.Millisecond}, 10*time.Millisecond
	ss := serve(t, s)
	start := time.Now()
	ss.send(callTool(1, "eva_search", map[string]any{"terms": []string{"tangerine"}}))
	r := ss.next()
	if res := r.tool(t); string(r.ID) != "1" || !res.IsError || res.Content[0].Text != "eva-md: the call passed its time limit" || time.Since(start) < 20*time.Millisecond {
		t.Fatalf("%s after %v", r.line, time.Since(start))
	}
	// Calls run in order, so the search's count is on stderr by the time the
	// next call is answered.
	ss.send(callTool(2, "eva_status", nil))
	if r := ss.next(); string(r.ID) != "2" {
		t.Fatal(r.line)
	}
	if got := ss.errOut.String(); got != "eva-md mcp: a call passed its time limit and was left running; 1 left running\n" {
		t.Fatalf("stderr %q", got)
	}
	ss.send(callTool(3, "eva_show", map[string]any{"ref": "systems/map"}))
	await(t, showing, "the show to start")
	ss.send(request(4, "ping", nil))
	if r := ss.next(); string(r.ID) != "4" {
		t.Fatalf("the ping waited behind the call: %s", r.line)
	}
}

// Once 8 calls are left running past their deadline, Serve ends with 1. Each
// is answered for, and stderr holds counts only, never an argument.
func TestAbandonedCallsEndTheServer(t *testing.T) {
	s := testServer(t)
	release := make(chan struct{})
	defer close(release)
	s.tools = map[string]handler{"eva_search": func(context.Context, *server, arguments) result { <-release; return text("too late") }}
	s.deadlines, s.grace = map[string]time.Duration{"eva_search": 10 * time.Millisecond}, 10*time.Millisecond
	ss := serve(t, s)
	for i := 1; i <= maxAbandoned; i++ {
		ss.send(callTool(i, "eva_search", map[string]any{"terms": []string{"tangerine"}}))
		if res := ss.next().tool(t); !res.IsError {
			t.Fatal(res)
		}
	}
	if code := ss.ended(); code != 1 {
		t.Fatal(code)
	}
	lines := strings.Split(strings.TrimSuffix(ss.errOut.String(), "\n"), "\n")
	count := regexp.MustCompile(`^eva-md mcp: (a call passed its time limit and was left running; \d+ left running|\d+ calls are still running past their time limit or a cancel; stopping)$`)
	if len(lines) != maxAbandoned+1 || !strings.HasSuffix(lines[maxAbandoned], "8 calls are still running past their time limit or a cancel; stopping") {
		t.Fatalf("stderr %q", lines)
	}
	for _, l := range lines {
		if !count.MatchString(l) {
			t.Fatalf("stderr line %q", l)
		}
	}
}

// A cancelled call still running the grace after its cancel is left
// running and counted as cancelled, not as one past its time limit, and 8
// of them end the server. The calls' deadline, lengthened for the tests, is
// out of reach, so only the cancels end them.
func TestCancelledCallsLeftRunningEndTheServer(t *testing.T) {
	s := testServer(t)
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{}, maxAbandoned)
	s.tools = map[string]handler{"eva_search": func(context.Context, *server, arguments) result {
		started <- struct{}{}
		<-release
		return text("too late")
	}}
	s.grace = 10 * time.Millisecond
	ss := serve(t, s)
	for i := 1; i <= maxAbandoned; i++ {
		ss.send(callTool(i, "eva_search", map[string]any{"terms": []string{"tangerine"}}))
		await(t, started, fmt.Sprintf("call %d to start", i))
		ss.send(notification("notifications/cancelled", map[string]any{"requestId": i}))
	}
	if code := ss.ended(); code != 1 {
		t.Fatal(code)
	}
	lines := strings.Split(strings.TrimSuffix(ss.errOut.String(), "\n"), "\n")
	if len(lines) != maxAbandoned+1 || lines[0] != "eva-md mcp: a call was cancelled and was left running; 1 left running" || lines[maxAbandoned] != "eva-md mcp: 8 calls are still running past their time limit or a cancel; stopping" {
		t.Fatalf("stderr %q", lines)
	}
}

// At most 64 calls wait behind the one running; the next is answered at
// once with an error. The running call's deadline, lengthened for the tests,
// is out of reach, so it holds the others up.
func TestTooManyWaitingCallsAreRefused(t *testing.T) {
	s := testServer(t)
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{}, 1)
	s.tools = map[string]handler{"eva_search": func(context.Context, *server, arguments) result {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return text("done")
	}}
	ss := serve(t, s)
	ss.send(callTool(1, "eva_search", map[string]any{"terms": []string{"tangerine"}}))
	await(t, started, "the first call to start")
	for i := 2; i <= maxQueue+2; i++ {
		ss.send(callTool(i, "eva_search", map[string]any{"terms": []string{"tangerine"}}))
	}
	r := ss.next()
	if res := r.tool(t); string(r.ID) != fmt.Sprint(maxQueue+2) || !res.IsError || res.Content[0].Text != "eva-md: too many calls are waiting; try again" {
		t.Fatal(r.line)
	}
}

// A call the host cancels while it runs sees its context end and answers
// nothing; a queued call it cancels never runs. Calls run in order, so a
// cancelled call that answered or ran would do so before the calls after it
// are answered.
func TestCancelledCallsAnswerNothing(t *testing.T) {
	s := testServer(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	var queuedRan atomic.Bool
	s.tools = map[string]handler{
		"eva_search": func(ctx context.Context, _ *server, _ arguments) result {
			close(started)
			<-ctx.Done()
			close(stopped)
			return text("late")
		},
		"eva_show": func(_ context.Context, _ *server, a arguments) result {
			<-release
			var ref string
			_ = json.Unmarshal(a["ref"], &ref)
			return text(ref)
		},
		"eva_status": func(context.Context, *server, arguments) result { queuedRan.Store(true); return text("second") },
	}
	ss := serve(t, s)
	ss.send(callTool(9, "eva_search", map[string]any{"terms": []string{"x"}}))
	await(t, started, "the call to start")
	ss.send(notification("notifications/cancelled", map[string]any{"requestId": 9, "reason": "the user moved on"}))
	await(t, stopped, "the cancelled call's context to end")
	// "second" waits behind "first", which runs until released, and is
	// cancelled while it waits; the ping's answer shows the cancel was read.
	ss.send(callTool("first", "eva_show", map[string]any{"ref": "first"}), callTool("second", "eva_status", nil),
		notification("notifications/cancelled", map[string]any{"requestId": "second"}), request(10, "ping", nil))
	if r := ss.next(); string(r.ID) != "10" {
		t.Fatalf("the cancelled call answered: %s", r.line)
	}
	close(release)
	if r := ss.next(); string(r.ID) != `"first"` || r.tool(t).Content[0].Text != "first" {
		t.Fatal(r.line)
	}
	ss.send(callTool("third", "eva_show", map[string]any{"ref": "third"}))
	if r := ss.next(); string(r.ID) != `"third"` || r.tool(t).Content[0].Text != "third" {
		t.Fatalf("an answer before the call after the cancelled one: %s", r.line)
	}
	if queuedRan.Load() {
		t.Fatal("a cancelled queued call ran")
	}
}

// A tool that panics answers an error, and the server goes on.
func TestPanickingToolIsAnError(t *testing.T) {
	s := testServer(t)
	s.tools = map[string]handler{"eva_status": func(context.Context, *server, arguments) result { panic("boom") }}
	ss := serve(t, s)
	ss.send(callTool(1, "eva_status", nil))
	if res := ss.next().tool(t); !res.IsError || res.Content[0].Text != "eva-md: internal error" {
		t.Fatal(res)
	}
	ss.send(request(2, "ping", nil))
	if r := ss.next(); string(r.ID) != "2" {
		t.Fatal(r.line)
	}
}

// Calls sent together run one at a time, in order, and each is answered
// once, each answer a whole JSON line.
func TestQueuedCallsAnsweredOnce(t *testing.T) {
	s := testServer(t)
	var running atomic.Int32
	s.tools = map[string]handler{"eva_show": func(_ context.Context, _ *server, a arguments) result {
		if running.Add(1) != 1 {
			panic("two calls at once")
		}
		defer running.Add(-1)
		// A pause, so a second call, were one to start, would overlap this
		// one; it decides nothing.
		time.Sleep(time.Millisecond)
		var ref string
		_ = json.Unmarshal(a["ref"], &ref)
		return text(strings.Repeat(ref+"\n", 50))
	}}
	ss := serve(t, s)
	for i := 0; i < 20; i++ {
		ss.send(callTool(i, "eva_show", map[string]any{"ref": fmt.Sprintf("ref-%d", i)}))
		ss.send(request(fmt.Sprintf("p%d", i), "ping", nil))
	}
	seen := map[string]int{}
	for i := 0; i < 40; i++ {
		r := ss.next()
		seen[string(r.ID)]++
		if r.Result != nil && strings.HasPrefix(string(r.ID), `"`) {
			continue
		}
		if res := r.tool(t); res.IsError || !strings.HasPrefix(res.Content[0].Text, "ref-"+string(r.ID)+"\n") {
			t.Fatalf("%s", r.line)
		}
	}
	for i := 0; i < 20; i++ {
		if seen[fmt.Sprint(i)] != 1 || seen[fmt.Sprintf(`"p%d"`, i)] != 1 {
			t.Fatalf("answers %v", seen)
		}
	}
	// A second answer to any of them would come before the next call's.
	ss.send(callTool("last", "eva_show", map[string]any{"ref": "ref-last"}))
	if r := ss.next(); string(r.ID) != `"last"` {
		t.Fatalf("an answer too many: %s", r.line)
	}
}

// The tools are off with mcp = false in the config, read for each request,
// and inside eva-md's own update run: tools/list is an empty list, never
// null, calls are refused and the instructions are the one line.
func TestOffStates(t *testing.T) {
	for _, tc := range []struct{ name, off string }{{"config", offConfig}, {"update run", offInner}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t)
			ss := serve(t, s)
			ss.send(request(1, "tools/list", nil))
			if r := ss.next(); !bytes.Equal(r.Result, toolsJSON()) {
				t.Fatalf("on: %s", r.line)
			}
			if tc.name == "config" {
				c := config.Default()
				c.MCP = false
				if err := config.Save(s.Dirs.ConfigFile(), c); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("EVA_MD_INNER", "1")
			}
			ss.send(request(2, "tools/list", nil))
			if r := ss.next(); string(r.Result) != `{"tools":[]}` {
				t.Fatalf("off: %s", r.line)
			}
			for _, name := range []string{"eva_search", "eva_show", "eva_status"} {
				ss.send(callTool(3, name, map[string]any{"terms": []string{"x"}, "ref": "systems/map"}))
				if res := ss.next().tool(t); !res.IsError || res.Content[0].Text != tc.off {
					t.Fatalf("%s: %+v", name, res)
				}
			}
			ss.send(request(4, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
			var r struct{ Instructions string }
			if err := json.Unmarshal(ss.next().Result, &r); err != nil || r.Instructions != tc.off {
				t.Fatalf("instructions %q %v", r.Instructions, err)
			}
		})
	}
}

// The tool list is fixed: its bytes are the golden file's, under 2 KB, each
// description under 160 characters, every tool read-only.
func TestToolListIsFixedAndSmall(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "tools.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(toolsJSON(), bytes.TrimSuffix(golden, []byte("\n"))) {
		t.Fatalf("tools/list changed; if that is meant, write it to testdata/tools.golden.json:\n%s", toolsJSON())
	}
	if len(toolsJSON()) >= 2048 {
		t.Fatalf("tools/list is %d bytes", len(toolsJSON()))
	}
	var list struct {
		Tools []struct {
			Name, Description string
			Annotations       map[string]bool
			InputSchema       map[string]any
		}
	}
	if err := json.Unmarshal(toolsJSON(), &list); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		a := tool.Annotations
		if len(tool.Description) >= 160 || !a["readOnlyHint"] || a["destructiveHint"] || !a["idempotentHint"] || a["openWorldHint"] || tool.InputSchema["type"] != "object" {
			t.Fatalf("%+v", tool)
		}
	}
	if strings.Join(names, " ") != "eva_search eva_show eva_status" {
		t.Fatal(names)
	}
}

// instructions starts a server, sends initialize and returns the
// instructions.
func instructions(t *testing.T, s Server) string {
	t.Helper()
	ss := serve(t, s)
	ss.send(request(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	return instructionsIn(t, ss.next())
}

// instructionsIn reads the instructions of an initialize answer.
func instructionsIn(t *testing.T, r reply) string {
	t.Helper()
	var result struct{ Instructions string }
	if err := json.Unmarshal(r.Result, &result); err != nil {
		t.Fatal(err)
	}
	return result.Instructions
}

// beatAt writes a heartbeat as a hook does.
func beatAt(t *testing.T, d paths.Dirs, host, event string, at time.Time) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"at": at, "host": host, "event": event})
	writeFile(t, filepath.Join(d.State, "heartbeats", host, event+".json"), string(data))
}

// installedFor writes the marketplace marker install leaves for host.
func installedFor(t *testing.T, d paths.Dirs, host string) {
	t.Helper()
	market := map[string]string{"claude-code": "claude", "codex": "codex"}[host]
	writeFile(t, filepath.Join(d.Share, "marketplace", market, ".eva-md-managed"), "eva-md marketplace v1\n")
}

func writeFile(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The instructions name the tools, add the ToolSearch line on Claude Code
// only, and at most one warning, in under 600 characters: hooks that never
// ran though eva-md is installed, none for 7 days, or the chat-start notice;
// none when all is well.
func TestInstructions(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, host, want string
		plant            func(t *testing.T, d paths.Dirs)
	}{
		{"healthy codex", "codex", "", func(t *testing.T, d paths.Dirs) {
			installedFor(t, d, "codex")
			beatAt(t, d, "codex", "SessionStart", now.Add(-time.Hour))
		}},
		{"healthy claude", "claude-code", "", func(t *testing.T, d paths.Dirs) {
			installedFor(t, d, "claude-code")
			beatAt(t, d, "claude-code", "SessionStart", now.Add(-time.Hour))
		}},
		{"not installed", "codex", "", func(*testing.T, paths.Dirs) {}},
		{"never ran on codex", "codex", "eva.md's hooks have not run in Codex yet: approve them in Codex /hooks, or run eva-md doctor.", func(t *testing.T, d paths.Dirs) {
			installedFor(t, d, "codex")
			beatAt(t, d, "claude-code", "SessionStart", now)
		}},
		{"never ran on claude", "claude-code", "eva.md's hooks have not run in Claude Code yet, so its chats are not captured; run eva-md doctor.", func(t *testing.T, d paths.Dirs) {
			installedFor(t, d, "claude-code")
			beatAt(t, d, "claude-code", "Stop", now)
		}},
		{"a week without hooks", "codex", "No Codex hook has run for 9 days, so capture may be off; run eva-md doctor.", func(t *testing.T, d paths.Dirs) {
			installedFor(t, d, "codex")
			beatAt(t, d, "codex", "SessionStart", now.Add(-9*24*time.Hour-time.Hour))
		}},
		{"hooks awaiting approval", "codex", "eva.md: review its hooks in Codex /hooks; run eva-md doctor to verify.", func(t *testing.T, d paths.Dirs) {
			installedFor(t, d, "codex")
			beatAt(t, d, "codex", "SessionStart", now.Add(-2*time.Hour))
			writeFile(t, filepath.Join(d.State, "needs-trust.json"), `{"host":"codex","needs_trust":true}`)
		}},
		{"unreadable config", "codex", "eva-md's configuration can't be read, so no chat is captured; run eva-md doctor.", func(t *testing.T, d paths.Dirs) {
			writeFile(t, d.ConfigFile(), "exclude = [")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t)
			s.Host = tc.host
			tc.plant(t, s.Dirs)
			got := instructions(t, s)
			want := guide
			if tc.host == "claude-code" {
				want += "\n" + toolSearch
			}
			if tc.want != "" {
				want += "\n" + tc.want
			}
			if got != want || len(got) >= maxInstructions {
				t.Fatalf("instructions:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// The server starts as the chat's own SessionStart hook runs, so a warning
// about the hooks waits for that hook's heartbeat: once it comes, the
// warning goes, without waiting out the rest. The wait here is longer than
// hangGuard, so instructions that waited it out would come too late.
func TestInstructionsWaitForTheChatsOwnHook(t *testing.T) {
	s := testServer(t)
	installedFor(t, s.Dirs, "codex")
	s.beatWait = 10 * hangGuard
	// Serve reads the clock to start, then the judgement and each look for
	// the heartbeat read it: the fourth read is the first look inside the
	// wait, after one that found no heartbeat.
	waiting := make(chan struct{})
	var reads atomic.Int32
	s.Now = func() time.Time {
		if reads.Add(1) == 4 {
			close(waiting)
		}
		return time.Now()
	}
	ss := serve(t, s)
	ss.send(request(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	await(t, waiting, "the instructions to wait for the chat's hook")
	beatAt(t, s.Dirs, "codex", "SessionStart", time.Now())
	if got := instructionsIn(t, ss.next()); got != guide {
		t.Fatalf("after the heartbeat: %s", got)
	}
	// A notice about anything else waits for nothing.
	s = testServer(t)
	s.beatWait = 10 * hangGuard
	installedFor(t, s.Dirs, "codex")
	beatAt(t, s.Dirs, "codex", "SessionStart", time.Now().Add(-time.Hour))
	writeFile(t, s.Dirs.ConfigFile(), "notices = \"start\"\n")
	if got := instructions(t, s); got != guide+"\neva.md: config has unknown keys, which are ignored; run eva-md doctor." {
		t.Fatalf("a notice: %s", got)
	}
}

// A judgement that stalls past its time, short here, gives no warning: the
// instructions come without it, with the guide alone. The judgement doesn't
// return until the test ends, so only its time can let the answer go.
func TestStalledJudgementGivesNoWarning(t *testing.T) {
	s := testServer(t)
	installedFor(t, s.Dirs, "codex")
	s.judgeTime = 10 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	// Serve reads the clock once to start; the judgement's read, its first
	// step, then stalls, before anything of its own can end at its time.
	s.Now = func() time.Time {
		if calls.Add(1) > 1 {
			<-release
		}
		return time.Now()
	}
	if got := instructions(t, s); got != guide {
		t.Fatalf("instructions: %s", got)
	}
}

// A judgement that panics, as on a bug in what it calls, gives no warning
// and ends nothing: the instructions come with the guide alone, nothing is
// written to stderr, and the server answers on.
func TestPanickingJudgementGivesNoWarning(t *testing.T) {
	s := testServer(t)
	installedFor(t, s.Dirs, "codex")
	var reads atomic.Int32
	// Serve reads the clock once to start; the judgement's read panics.
	s.Now = func() time.Time {
		if reads.Add(1) == 2 {
			panic("a bug in the judgement")
		}
		return time.Now()
	}
	ss := serve(t, s)
	ss.send(request(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	if got := instructionsIn(t, ss.next()); got != guide {
		t.Fatalf("instructions: %s", got)
	}
	ss.send(request(2, "ping", nil))
	if r := ss.next(); string(r.ID) != "2" || r.Error != nil {
		t.Fatalf("ping: %s", r.line)
	}
	if e := ss.errOut.String(); e != "" {
		t.Fatalf("stderr: %q", e)
	}
}

// The doctor's probe starts no chat, so the server it starts waits for no
// SessionStart hook: the first judgement is the warning. The clock holds at
// the first look for the heartbeat, so a server that waited would look, and
// answer nothing.
func TestDoctorsProbeWaitsForNoHook(t *testing.T) {
	s := testServer(t)
	s.Host = "claude-code"
	installedFor(t, s.Dirs, "claude-code")
	s.beatWait = 10 * hangGuard
	looked, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var reads atomic.Int32
	// Serve reads the clock to start, then the judgement; the third read is
	// the first look for the heartbeat.
	s.Now = func() time.Time {
		if reads.Add(1) == 3 {
			close(looked)
			<-release
		}
		return time.Now()
	}
	ss := serve(t, s)
	ss.send(request(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]string{"name": health.DoctorClient, "version": "1"}}))
	select {
	case line := <-ss.lines:
		var r reply
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		want := guide + "\n" + toolSearch + "\neva.md's hooks have not run in Claude Code yet, so its chats are not captured; run eva-md doctor."
		if got := instructionsIn(t, r); got != want {
			t.Fatalf("instructions: %s", got)
		}
	case <-looked:
		t.Fatal("the doctor's server looked for the chat's SessionStart hook")
	case <-time.After(hangGuard):
		t.Fatalf("no answer after %v: the server hung", hangGuard)
	}
}

// On Claude Code a warning longer than the room the guide and the ToolSearch
// line leave is cut in its middle: it keeps its final clause, and the fix it
// names, whole. The drift notice, the longest fixed chat-start notice, goes
// end to end; the notices that carry reasons are cut as the instructions cut
// them.
func TestLongWarningKeepsItsFix(t *testing.T) {
	s := testServer(t)
	s.Host = "claude-code"
	installedFor(t, s.Dirs, "claude-code")
	beatAt(t, s.Dirs, "claude-code", "SessionStart", time.Now().Add(-time.Hour))
	writeFile(t, filepath.Join(s.Dirs.State, "catchup", "drift-claude-code.json"), `{"v":1,"host":"claude-code"}`)
	base := guide + "\n" + toolSearch + "\n"
	if got, want := instructions(t, s), base+"eva.md: transcript recovery no longer recognizes claude-code's transcripts, so missed…; run eva-md doctor."; got != want {
		t.Fatalf("instructions:\n%s\nwant:\n%s", got, want)
	}
	room := maxInstructions - utf8.RuneCountInString(base)
	for _, w := range []string{
		"eva.md: the last 3 claude-code updates failed (the host's answer did not validate, not logged in, the host timed out); run eva-md doctor.",
		"eva.md: the wiki has not been committed for 3 days (the commit's scan found a secret in a page); run eva-md doctor.",
	} {
		got := cutWarning(w, room)
		if n := utf8.RuneCountInString(got); n != room || !strings.HasSuffix(got, "…"+w[strings.LastIndex(w, "; "):]) {
			t.Errorf("%d runes in %d: %s", n, room, got)
		}
	}
	if w := "eva.md: 12 claude-code sessions are quarantined after 3 failed updates; run eva-md doctor."; cutWarning(w, room) != w {
		t.Errorf("a warning that fits was cut: %s", cutWarning(w, room))
	}
}

// A judgement whose time ran out during the config read gives no warning:
// the cut read says nothing of the config. The test ends that time at the
// exact point, before the read.
func TestJudgementOutOfTimeIsNoConfigWarning(t *testing.T) {
	srv := &server{Server: testServer(t), base: context.Background(), start: time.Now()}
	srv.Now, srv.judgeTime = time.Now, -time.Second
	if j := srv.judge(); j.warning != "" || j.hooks {
		t.Fatalf("judgement %+v", j)
	}
}

// A call whose time ran out during the config read answers that it passed
// its time limit, never that the config can't be read.
func TestCallOutOfTimeIsNoConfigError(t *testing.T) {
	srv := &server{Server: testServer(t), base: context.Background(), start: time.Now()}
	srv.Getenv = func(string) string { return "" }
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	if _, r := srv.load(ctx); r == nil || !r.IsError || len(r.Content) != 1 || r.Content[0].Text != "eva-md: the call passed its time limit" {
		t.Fatalf("result %+v", r)
	}
}

// A heartbeat of the chat's own SessionStart hook that comes once the first
// judgement has read the files, and is there at the first look, has the
// warning judged again: no false alarm that the hooks never ran. The hook
// writes it as the first look reads the clock.
func TestBeatAtTheFirstLookIsJudgedAgain(t *testing.T) {
	s := testServer(t)
	installedFor(t, s.Dirs, "codex")
	s.judgeTime = hangGuard
	file := filepath.Join(s.Dirs.State, "heartbeats", "codex", "SessionStart.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	var wrote error
	s.Now = func() time.Time {
		// The judgement reads the clock once, then the first look does.
		if reads.Add(1) == 2 {
			data, _ := json.Marshal(map[string]any{"at": time.Now(), "host": "codex", "event": "SessionStart"})
			wrote = os.WriteFile(file, data, 0o600)
		}
		return time.Now()
	}
	srv := &server{Server: s, base: context.Background(), start: time.Now()}
	if w := srv.warning(true); w != "" || wrote != nil {
		t.Fatalf("warning %q, heartbeat %v", w, wrote)
	}
	if n := reads.Load(); n < 3 {
		t.Fatalf("the clock was read %d times: the warning was not judged again", n)
	}
}

// The home folder comes from the installed binary's path, and from no other.
func TestHomeFromBinary(t *testing.T) {
	for exe, want := range map[string]string{
		"/Users/someone/.local/share/eva-md/bin/eva-md":         "/Users/someone",
		"/Users/it's a space/.local/share/eva-md/bin/eva-md":    "/Users/it's a space",
		"/Users/someone/.local/share/eva-md/bin/../bin/eva-md":  "/Users/someone",
		"/.local/share/eva-md/bin/eva-md":                       "",
		"relative/.local/share/eva-md/bin/eva-md":               "",
		"/Users/someone/go/bin/eva-md":                          "",
		"/Users/someone/.local/share/eva-md/bin/eva-md-new":     "",
		"/private/var/folders/x/T/go-build/b001/exe/eva-md":     "",
		"/Users/someone/.local/share/eva-md/marketplace/eva-md": "",
	} {
		home, ok := HomeFromBinary(exe)
		if home != want || ok != (want != "") {
			t.Errorf("%s: %q %v", exe, home, ok)
		}
	}
}
