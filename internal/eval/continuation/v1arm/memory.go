package v1arm

import (
	"context"
	"os"
	"path/filepath"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
)

// maxSession is the largest session file the memory check reads.
const maxSession = 64 << 20

// memoryOn reports whether eva-md's hooks captured the trial's new chat
// as v1 must: whether the inbox in the trial's home holds a session other
// than the replayed one, session on the trial's host, that holds an event,
// and on Claude Code, whose hook reads the chat's entrypoint, a prompt
// filed as a person's. A chat whose prompts are filed as a program's
// (host_prompt) alone is the seam failing: such a chat gets no brief a
// person gets. On Codex, which reads no entrypoint, any event counts, a
// chat's start alone included. A file over 64 MiB is not read, and an
// inbox that cannot be read shows nothing.
func memoryOn(ctx context.Context, t drive.Trial, session string) (bool, error) {
	root := filepath.Join(stateDir(t), "inbox")
	b := inboxv1.Backend{Root: root}
	keys, err := b.Sessions(ctx)
	if err != nil {
		return false, ctx.Err()
	}
	for _, k := range keys {
		if k.Host == string(t.Host.ID) && k.Session == session {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, k.Folder, k.File))
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxSession {
			continue
		}
		_, events, err := b.Read(ctx, k)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			continue
		}
		for _, e := range events {
			if t.Host.ID != adapter.ClaudeCode || e.Kind == event.KindPrompt && e.Author == event.AuthorV1User {
				return true, nil
			}
		}
	}
	return false, nil
}
