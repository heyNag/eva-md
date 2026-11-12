package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/hostfacts"
)

// hostTimeout bounds each host command hostrec starts; tests raise it.
var hostTimeout = 2 * time.Minute

// hostCLIs are the hosts' commands, whose versions each mark keeps.
var hostCLIs = map[string]string{"claude-code": "claude", "codex": "codex"}

func cmdMark(c *cli, args []string) int {
	if len(args) != 1 {
		return c.fail("usage: hostrec mark <step>")
	}
	step := args[0]
	if !hostfacts.IsStep(step) {
		return c.fail("%q is not a step of the run-book", step)
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	// The versions are read before the lock is taken: a host CLI may take
	// a while to answer, and a hook that waits past lockWait for the lock
	// keeps nothing.
	versions := map[string]string{}
	for host, name := range hostCLIs {
		versions[host] = hostVersion(name)
	}
	unlock, err := e.lock()
	if err != nil {
		return c.fail("%v", err)
	}
	defer unlock()
	st, err := e.state()
	if err != nil {
		return c.fail("the sitting's state does not read: %v", err)
	}
	opened := !st.Open
	now := time.Now()
	_, offset := now.Zone()
	if err := e.line(hostfacts.MarksFile, hostfacts.MarkLine{
		V: 1, Kind: "mark", Mark: step, Wall: now.UTC().Format(time.RFC3339Nano), MonoNS: nanotime(), UTCOffsetS: offset, Versions: versions,
	}); err != nil {
		return c.fail("writing the mark: %v", err)
	}
	if err := e.put(hostfacts.StateFile, state{Open: true, Mark: step, MarkWall: now.UTC().Format(time.RFC3339Nano)}); err != nil {
		return c.fail("writing the sitting's state: %v", err)
	}
	if opened {
		c.say("hostrec: the recording is open; step %s is marked.", step)
	} else {
		c.say("hostrec: step %s is marked.", step)
	}
	return 0
}

// hostVersion is a host CLI's --version output, its first line, or what
// kept it from answering.
func hostVersion(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), hostTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.Error); ok {
			return "absent"
		}
		return "no answer"
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}

func cmdClose(c *cli, args []string) int {
	if len(args) != 0 {
		return c.fail("usage: hostrec close")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	unlock, err := e.lock()
	if err != nil {
		return c.fail("%v", err)
	}
	defer unlock()
	st, err := e.state()
	if err != nil {
		return c.fail("the sitting's state does not read: %v", err)
	}
	if !st.Open {
		c.say("hostrec: the recording is closed already.")
		return 0
	}
	now := time.Now()
	_, offset := now.Zone()
	if err := e.line(hostfacts.MarksFile, hostfacts.MarkLine{V: 1, Kind: "close", Mark: st.Mark, Wall: now.UTC().Format(time.RFC3339Nano), MonoNS: nanotime(), UTCOffsetS: offset}); err != nil {
		return c.fail("writing the close: %v", err)
	}
	if err := e.put(hostfacts.StateFile, state{Open: false, Mark: st.Mark}); err != nil {
		return c.fail("writing the sitting's state: %v", err)
	}
	c.say("hostrec: the recording is closed; the scratch hooks record nothing until the next mark.")
	return 0
}

func cmdNote(c *cli, args []string) int {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		return c.fail("usage: hostrec note \"what you saw\"")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	st, err := e.state()
	if err != nil {
		return c.fail("the sitting's state does not read: %v", err)
	}
	if err := e.line(hostfacts.NotesFile, hostfacts.NoteLine{V: 1, Mark: st.Mark, Wall: wallNow(), Text: text}); err != nil {
		return c.fail("writing the note: %v", err)
	}
	c.say("hostrec: noted at %s.", orNone(st.Mark))
	return 0
}

func orNone(s string) string {
	if s == "" {
		return "no step"
	}
	return s
}

// eventHead is what tail and summary read of an event: never its payload.
type eventHead struct {
	Mark   string `json:"mark"`
	Wall   string `json:"wall"`
	Host   string `json:"host"`
	Event  string `json:"event"`
	Via    string `json:"via"`
	Answer string `json:"answer"`
}

func cmdTail(c *cli, args []string) int {
	if len(args) != 0 {
		return c.fail("usage: hostrec tail")
	}
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	st, err := e.state()
	if err != nil {
		return c.fail("the sitting's state does not read: %v", err)
	}
	marks, err := readLines[hostfacts.MarkLine](filepath.Join(e.rec, hostfacts.MarksFile))
	if err != nil {
		return c.fail("%v", err)
	}
	since := time.Time{}
	for _, m := range marks {
		if m.Kind == "mark" && m.Mark == st.Mark {
			since, _ = time.Parse(time.RFC3339Nano, m.Wall)
		}
	}
	events, err := readLines[eventHead](filepath.Join(e.rec, hostfacts.EventsFile))
	if err != nil {
		return c.fail("%v", err)
	}
	n := 0
	for _, ev := range events {
		t, err := time.Parse(time.RFC3339Nano, ev.Wall)
		if err != nil || t.Before(since) || ev.Mark != st.Mark {
			continue
		}
		n++
		line := t.Local().Format("15:04:05") + "  " + ev.Host + "  " + ev.Event
		if ev.Via != "" {
			line += "  (" + ev.Via + ")"
		}
		c.say("%s", line)
	}
	c.say("hostrec: %s since the mark of %s.", plural(n, "event", "events"), orNone(st.Mark))
	return 0
}

func cmdArm(c *cli, args []string) int {
	if len(args) < 1 {
		return c.fail("usage: hostrec arm <name>")
	}
	if args[0] != armStart {
		return c.fail("the arm %q belongs to Part 2 of the recorder, which this build does not have yet; Part 1 has only start", args[0])
	}
	if len(args) != 1 {
		return c.fail("usage: hostrec arm start")
	}
	return setArm(c, armFile{V: 1, Name: armStart}, "armed: start. A new, resumed or cleared chat gets the start word.")
}

func cmdDisarm(c *cli, args []string) int {
	if len(args) != 0 {
		return c.fail("usage: hostrec disarm")
	}
	return setArm(c, armFile{V: 1}, "disarmed: the scratch hooks record only.")
}

// setArm writes the arm the hook reads. It changes nothing under a plugin
// root or in a host's copy of a plugin.
func setArm(c *cli, a armFile, said string) int {
	e, code := c.needEnv()
	if e == nil {
		return code
	}
	if err := e.put(hostfacts.ArmFile, a); err != nil {
		return c.fail("writing the arm: %v", err)
	}
	_ = e.command("arm", map[string]any{"arm": a.Name})
	c.say("hostrec: %s", said)
	return 0
}
