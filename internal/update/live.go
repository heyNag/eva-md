package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// LiveResult is what eva-md doctor --live found for one host.
type LiveResult struct {
	Host     string
	Reason   string        // "" when the answer validated, else the failure class
	Asked    string        // the effort the run asked for
	Effort   string        // the effort the accepted attempt used; empty without the flag
	Degraded string        // a note kept beside a valid answer
	Duration time.Duration // the run, retries included
}

// liveTexts are the fixture's two exchanges, a made-up chat about a build
// script: a prompt, a reply, a prompt and a reply, over the 600 characters a
// group needs.
var liveTexts = []string{
	"Please add a --verbose flag to the build script. When it is set, each step should print its name before it runs, so a slow build shows where the time goes.",
	"Added the --verbose flag to the build script. With the flag set, every step prints its name before it starts; without it the output is unchanged, and the build still passes.",
	"Good. Keep the flag off by default. Next, add a test that runs the build with the flag and checks that each step name is printed, in order.",
	"The flag stays off by default. I suggest a test that runs the build with --verbose, captures the output and compares the step names with the list in the script.",
}

// Live runs the exact headless update command once for host, against a tiny
// built-in fixture: the two exchanges above as a batch for an empty thread
// page. It asks for the host's default model at the effort the config gives
// the host, with the update's rules and schema, in an empty run folder, and
// checks the answer with the validator a real pass uses. It writes nothing:
// no page, no watermark and no run log, so its call counts toward no budget
// and its failure holds nothing. An error means the run could not start.
func Live(ctx context.Context, runner Runner, host string, c config.Config) (LiveResult, error) {
	h, ok := lookupHost(host)
	if !ok {
		return LiveResult{}, fmt.Errorf("update: unknown host %q", host)
	}
	effort, err := resolveEffort(h.efforts, c.EffortFor(host, ""))
	if err != nil {
		return LiveResult{}, err
	}
	batch, page, err := liveFixture(host)
	if err != nil {
		return LiveResult{}, err
	}
	dir, err := os.MkdirTemp("", "eva-md-live-")
	if err != nil {
		return LiveResult{}, errors.New("update: could not create a run folder")
	}
	defer os.RemoveAll(dir)
	start := time.Now()
	res, err := runner.Run(ctx, Request{Host: host, Effort: effort, Rules: Rules, Input: batch.Input, Schema: Schema(page),
		RunDir: dir, IgnoreUserConfig: c.CodexIgnoreUserConfig})
	if err != nil {
		return LiveResult{}, err
	}
	l := LiveResult{Host: host, Reason: res.Reason, Asked: effort, Effort: res.Effort, Degraded: res.Degraded, Duration: time.Since(start)}
	if l.Reason == "" {
		if _, _, err := Validate(res.Output, batch, page); err != nil {
			l.Reason = ReasonInvalidOutput
		}
	}
	return l, nil
}

// liveFixture is the batch and the empty page doctor --live sends host.
func liveFixture(host string) (Batch, threadpage.Page, error) {
	at := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	var entries []Entry
	var end int64
	for i, text := range liveTexts {
		role, kind := "user", "prompt"
		if i%2 == 1 {
			role, kind = "assistant", "reply"
		}
		end += int64(len(text)) + 1
		e := inbox.Entry{V: 1, TS: at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339), Host: host, Session: "live-check",
			Role: role, Kind: kind, Text: text, Project: "general", Branch: "main", Origin: "hook"}
		entries = append(entries, Entry{Entry: e, Line: i + 1, End: end})
	}
	page := threadpage.Page{Title: "Live check · main"}
	b, err := Build(entries, page, Options{Branch: "main"})
	return b, page, err
}
