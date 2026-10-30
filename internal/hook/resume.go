package hook

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/resume"
	"github.com/heyNag/eva-md/internal/session"
)

// briefMarker is the value of the session.Briefed marker.
type briefMarker struct {
	Hash      string `json:"now_hash"`
	Variant   string `json:"variant"`
	Refreshed bool   `json:"refreshed"`
}

// markerReserve is kept before the hook's deadline for the briefed marker's
// rename and folder sync, which take a few milliseconds. A marker not renamed
// by then is not saved, and its brief is retried on the next prompt.
const markerReserve = 20 * time.Millisecond

func resumeContext(ctx context.Context, host string, p payload, env Env, project identity.Project, now time.Time) string {
	if p.Event != "SessionStart" && p.Event != "UserPromptSubmit" {
		return ""
	}
	markers := session.Markers{Root: env.Dirs.Sessions()}
	variant := p.Source
	previous, found, err := markers.GetContext(ctx, host, p.Session, session.Briefed)
	if err != nil {
		return ""
	}
	var marker briefMarker
	if found && json.Unmarshal([]byte(previous), &marker) != nil {
		found = false
	}
	if p.Event == "UserPromptSubmit" {
		if synthetic(p) || builtinCommand(p.Prompt) {
			return ""
		}
		if !found {
			variant = "startup"
		} else if marker.Refreshed || marker.Variant == "compact" {
			return ""
		} else {
			variant = "refresh"
		}
	}
	switch variant {
	case "startup", "clear", "resume", "fork", "compact", "refresh":
	default:
		variant = "startup"
	}
	result, err := resume.Build(ctx, resume.Options{Dirs: env.Dirs, Config: env.Config, Project: project, Host: host, Session: p.Session, Variant: variant, Now: now, PreviousHash: marker.Hash})
	if err != nil || ctx.Err() != nil {
		return ""
	}
	if variant == "refresh" {
		marker.Refreshed = true
		marker.Hash = result.NowHash
	} else {
		marker = briefMarker{Hash: result.NowHash, Variant: variant}
	}
	notice := ""
	if p.Event == "SessionStart" || !found {
		notice = health.QuickNotice(ctx, env.Dirs, env.Config, host)
	}
	contextText := result.Text
	// A health notice is a fixed diagnostic, not recovered chat content.
	if notice != "" && contextText == "" {
		contextText = notice
	}
	// The marker is saved last, and only with markerReserve to spare, so a
	// brief lost to the deadline is retried on the next prompt.
	saveCtx := ctx
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		saveCtx, cancel = context.WithDeadline(ctx, deadline.Add(-markerReserve))
		defer cancel()
	}
	data, _ := json.Marshal(marker)
	if err := markers.SetContext(saveCtx, host, p.Session, session.Briefed, string(data)); err != nil {
		return ""
	}
	if contextText == "" {
		return ""
	}
	output := map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": p.Event, "additionalContext": contextText}}
	if notice != "" {
		output["systemMessage"] = notice
	} else if host == "claude-code" && result.Found && variant != "refresh" && env.Config.Notice != "problems" {
		output["systemMessage"] = "eva.md: earlier work is available for this chat."
	}
	encoded, _ := json.Marshal(output)
	return string(encoded)
}

func heartbeat(ctx context.Context, dirs paths.Dirs, host, event string, transcript bool, now time.Time) {
	switch event {
	case "SessionStart", "SessionEnd", "UserPromptSubmit", "PostToolUse", "PostToolUseFailure", "Stop", "StopFailure", "PreCompact", "PostCompact", "PostModelSwitch", "Interrupt":
	default:
		return
	}
	root, err := paths.OpenRoot(filepath.Join(dirs.State, "heartbeats"), true)
	if err != nil {
		return
	}
	defer root.Close()
	if root.MkdirAll(host, 0700) != nil {
		return
	}
	data, _ := json.Marshal(struct {
		V          int    `json:"v"`
		At         string `json:"at"`
		Host       string `json:"host"`
		Event      string `json:"event"`
		Transcript bool   `json:"transcript_available"`
	}{1, now.Format(time.RFC3339Nano), host, event, transcript})
	_ = paths.WriteFileAtomicAt(ctx, root, filepath.Join(host, event+".json"), data, 0600)
}
