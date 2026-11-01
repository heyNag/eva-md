package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
)

// mcpHangGuard is how long TestMCPCommand waits for the child's answer or
// exit before it reports a hang.
const mcpHangGuard = time.Minute

// mcpProcess, set to 1, makes TestMCPHelperProcess run the MCP server on
// stdin and stdout instead of testing, as a host starts the installed
// binary.
const mcpProcess = "EVA_MD_TEST_MCP_PROCESS"

// mcpStderr holds a child's stderr, which a test may read while the child
// writes it.
type mcpStderr struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *mcpStderr) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *mcpStderr) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestMCPHelperProcess is no test of its own: TestMCPCommand runs the test
// binary through it as a real child process.
func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv(mcpProcess) != "1" {
		return
	}
	os.Exit(run(context.Background(), []string{"mcp", "--host", "codex"}, os.Stdin, os.Stdout, os.Stderr))
}

// noStdin fails a test that reads it.
type noStdin struct{ t *testing.T }

func (r noStdin) Read([]byte) (int, error) { r.t.Error("stdin was read"); return 0, io.EOF }

const (
	mcpInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	mcpList       = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
)

// toolNames reads a tools/list answer.
func toolNames(t *testing.T, line []byte) string {
	t.Helper()
	var r struct {
		ID     int `json:"id"`
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(line, &r); err != nil || r.ID != 2 {
		t.Fatalf("tools/list answer %s: %v", line, err)
	}
	var names []string
	for _, tool := range r.Result.Tools {
		names = append(names, tool.Name)
	}
	return strings.Join(names, " ")
}

// eva-md mcp answers on a scripted stdin and exits 0 at its end; a usage
// error exits 2 before stdin is read; and a real child process, as a host
// starts it, finds a planted chat whose ref eva-md show opens, then exits
// once its stdin closes.
func TestMCPCommand(t *testing.T) {
	d := isolated(t)
	var out, errOut bytes.Buffer
	script := mcpInitialize + "\n" + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + mcpList + "\n"
	if code := run(context.Background(), []string{"mcp", "--host", "codex"}, strings.NewReader(script), &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("code %d stderr %q", code, errOut.String())
	}
	lines := bytes.Split(bytes.TrimSuffix(out.Bytes(), []byte("\n")), []byte("\n"))
	if len(lines) != 2 || !bytes.Contains(lines[0], []byte(`"serverInfo":{"name":"eva-md","version":"`+version+`"}`)) || toolNames(t, lines[1]) != "eva_search eva_show eva_status" {
		t.Fatalf("answers:\n%s", out.String())
	}
	for _, args := range [][]string{{"mcp"}, {"mcp", "--host", "claude"}, {"mcp", "--host", "codex", "extra"}, {"mcp", "--bad"}} {
		out.Reset()
		errOut.Reset()
		if code := run(context.Background(), args, noStdin{t}, &out, &errOut); code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "mcp --host must be claude-code or codex") {
			t.Fatalf("%v: %d %q %q", args, code, out.String(), errOut.String())
		}
	}

	cwd, _ := os.Getwd()
	p, err := identity.Resolve(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	e := inbox.Entry{V: 1, TS: time.Now().Add(-time.Minute).Format(time.RFC3339), Host: "codex", Session: "mcpchild-session", Role: "user", Kind: "prompt", Text: "Remember the tangerine protocol.", Project: p.Key, Branch: p.Branch, Cwd: cwd, Origin: "hook"}
	if err := (inbox.Store{Root: d.Inbox()}).Append(e); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMCPHelperProcess$")
	cmd.Env = append(os.Environ(), mcpProcess+"=1")
	cmd.Dir = cwd
	stderr := &mcpStderr{}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	answers := make(chan []byte)
	go func() {
		defer close(answers)
		r := bufio.NewReader(stdout)
		for {
			answer, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			answers <- answer
		}
	}()
	ask := func(line string) []byte {
		t.Helper()
		if _, err := io.WriteString(stdin, line+"\n"); err != nil {
			t.Fatal(err)
		}
		select {
		case answer, ok := <-answers:
			if !ok {
				t.Fatalf("no answer to %s: the server's stdout closed; stderr %q", line, stderr.String())
			}
			return answer
		case <-time.After(mcpHangGuard):
			t.Fatalf("no answer to %s after %v: the server hung; stderr %q", line, mcpHangGuard, stderr.String())
		}
		return nil
	}
	if answer := ask(mcpInitialize); !bytes.Contains(answer, []byte(`"protocolVersion":"2025-06-18"`)) {
		t.Fatalf("initialize: %s", answer)
	}
	if _, err := io.WriteString(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	if names := toolNames(t, ask(mcpList)); names != "eva_search eva_show eva_status" {
		t.Fatal(names)
	}
	var found struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	answer := ask(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"eva_search","arguments":{"terms":["tangerine"]}}}`)
	if err := json.Unmarshal(answer, &found); err != nil || found.Result.IsError || len(found.Result.Content) != 1 {
		t.Fatalf("eva_search: %s", answer)
	}
	ref := regexp.MustCompile(`s:mcpchild#\d+`).FindString(found.Result.Content[0].Text)
	if ref == "" {
		t.Fatalf("no ref in:\n%s", found.Result.Content[0].Text)
	}
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"show", ref}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Remember the tangerine protocol.") {
		t.Fatalf("show %s: %d %q %q", ref, code, out.String(), errOut.String())
	}
	stdin.Close()
	// The server ends once its stdin closes (TestEndOfInputEndsServe in
	// internal/mcp), so a child still running after mcpHangGuard has hung.
	type exit struct {
		extra int // answers after the last one asked for
		err   error
	}
	exited := make(chan exit, 1)
	go func() {
		extra := 0
		for range answers {
			extra++
		}
		exited <- exit{extra, cmd.Wait()}
	}()
	select {
	case e := <-exited:
		if e.err != nil || e.extra != 0 || stderr.String() != "" {
			t.Fatalf("exit %v, %d more answers, stderr %q", e.err, e.extra, stderr.String())
		}
	case <-time.After(mcpHangGuard):
		t.Fatalf("the server had not exited %v after its stdin closed: it hung; stderr %q", mcpHangGuard, stderr.String())
	}
}

// eva-md status compares the plugins' .mcp.json, and only eva-md doctor
// starts the server.
func TestStatusStartsNoMCPServer(t *testing.T) {
	isolated(t)
	rows := func(command string) map[string]string {
		var out, errOut bytes.Buffer
		run(context.Background(), []string{command, "--json"}, strings.NewReader(""), &out, &errOut)
		var r struct {
			Checks []struct{ Name, Status, Detail string }
		}
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatalf("%s --json: %v", command, err)
		}
		found := map[string]string{}
		for _, c := range r.Checks {
			found[c.Name] = c.Status + " " + c.Detail
		}
		return found
	}
	status := rows("status")
	for _, name := range []string{"claude mcp manifest", "codex mcp manifest"} {
		if !strings.HasPrefix(status[name], "UNKNOWN ") {
			t.Fatalf("%s: %q", name, status[name])
		}
	}
	if row, ok := status["mcp server"]; ok {
		t.Fatalf("status started the server: %s", row)
	}
	if row := rows("doctor")["mcp server"]; row != "UNKNOWN stable hook binary is not installed" {
		t.Fatalf("doctor's mcp server row: %q", row)
	}
}
