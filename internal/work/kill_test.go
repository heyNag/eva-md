package work

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// childState names, for the test binary run again as a worker, the state
// folder to work in.
const childState = "EVAMD_WORK_TEST_CHILD_STATE"

// TestWorkerChild is the worker TestAKilledWorkersRunIsJournaledAsInterrupted
// kills: run again as a child with childState set, it starts a job that
// says so on stdout and never ends. Run any other way, it does nothing.
func TestWorkerChild(t *testing.T) {
	state := os.Getenv(childState)
	if state == "" {
		return
	}
	withJobs(t, Job{Kind: "sync", Priority: 2, Run: func(context.Context, []Mark, io.Writer) Outcome {
		fmt.Println("job running")
		select {}
	}})
	marks := []Mark{
		{Kind: "sync", Key: "claude-code-1", Reason: "stop", Host: "claude-code"},
		{Kind: "sync", Key: "codex-2", Reason: "end", Host: "codex"},
	}
	err := Wake(context.Background(), paths.Dirs{State: state}, marks, Options{})
	t.Fatalf("the child's wake returned: %v", err)
}

func TestAKilledWorkersRunIsJournaledAsInterrupted(t *testing.T) {
	dirs := testDirs(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkerChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), childState+"="+dirs.State)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	running := make(chan bool, 1)
	go func() {
		s := bufio.NewScanner(out)
		for s.Scan() {
			if s.Text() == "job running" {
				running <- true
				return
			}
		}
		running <- false
	}()
	select {
	case ok := <-running:
		if !ok {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("the child ended before its job ran")
		}
	case <-time.After(hangGuard):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("the child's job did not start")
	}
	if err := cmd.Process.Kill(); err != nil { // SIGKILL
		t.Fatal(err)
	}
	_ = cmd.Wait()

	lines := journal(t, dirs)
	if len(lines) != 1 || lines[0].Ev != evStart {
		t.Fatalf("the child journaled %+v", lines)
	}
	killed := lines[0]
	r := newRecorder()
	withJobs(t, Job{Kind: "sync", Priority: 2, Run: r.run})
	if err := Wake(context.Background(), dirs, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	lines = journal(t, dirs)
	if len(lines) != 4 {
		t.Fatalf("journal %+v", lines)
	}
	cut := lines[1]
	if cut.Ev != evInterrupted || cut.Run != killed.Run || cut.Kind != "sync" || cut.Prio != 2 || cut.Marks != 2 ||
		!slices.Equal(cut.Reasons, []string{"end", "stop"}) || !slices.Equal(cut.Hosts, []string{"claude-code", "codex"}) || cut.Outcome != "" || cut.Ms != nil {
		t.Fatalf("interrupted line %+v for the killed run %+v", cut, killed)
	}
	if lines[2].Ev != evStart || lines[3].Ev != evFinish || lines[2].Run == killed.Run || lines[3].Outcome != Done {
		t.Fatalf("the run after: %+v", lines[2:])
	}
	calls := r.all()
	want := [][]Mark{{
		{Kind: "sync", Key: "claude-code-1", Reason: "stop", Host: "claude-code"},
		{Kind: "sync", Key: "codex-2", Reason: "end", Host: "codex"},
	}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("ran %+v, want %+v", calls, want)
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Fatalf("marks left: %q", got)
	}
	if got := names(t, dirs, runDir); len(got) != 0 {
		t.Fatalf("runs left: %q", got)
	}
}
