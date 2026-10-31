package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// A chat whose work all happened before the last MiB of its file still counts
// as the previous chat: the brief scans the prefix for work when the tail
// shows none, including a pattern split across a 64 KiB read.
func TestWorkBeforeTheTailStillCounts(t *testing.T) {
	for _, name := range []string{"tool line across a read boundary", "three prompts and three replies"} {
		t.Run(name, func(t *testing.T) {
			o := fixture(t)
			o.Variant = "startup"
			var buf bytes.Buffer
			ts := o.Now.Add(-time.Hour)
			line := func(role, kind, text string) []byte {
				e := inbox.Entry{V: 1, Host: "codex", Session: "prefix-work", Project: o.Project.Key, Branch: "main", TS: ts.Format(time.RFC3339), Role: role, Kind: kind, Text: text, Origin: "hook"}
				ts = ts.Add(time.Second)
				data, err := json.Marshal(e)
				if err != nil {
					t.Fatal(err)
				}
				return append(data, '\n')
			}
			// Events are not work, so they pad the file without changing the answer.
			pad := func(n int) {
				for n > 0 {
					size := min(n, 100<<10)
					buf.Write(line("event", "event", strings.Repeat("p", size)))
					n -= size
				}
			}
			buf.Write(line("user", "prompt", "Start the migration."))
			if name == "tool line across a read boundary" {
				tool := line("tool", "tool", "Bash: go test ./... (exit 0)")
				at := bytes.Index(tool, []byte(`"kind":"tool"`))
				// Place the tool line so its pattern starts 5 bytes before 64 KiB.
				start := 64<<10 - 5 - at
				// An empty text is left out of the JSON, so size the padding with one byte.
				event := line("event", "event", "p")
				ts = ts.Add(-time.Second)
				buf.Write(line("event", "event", strings.Repeat("p", start-buf.Len()-(len(event)-1))))
				buf.Write(tool)
				if at := bytes.Index(buf.Bytes(), []byte(`"kind":"tool"`)); at >= 64<<10 || at+len(`"kind":"tool"`) <= 64<<10 {
					t.Fatalf("the tool pattern at byte %d does not straddle 64 KiB, so this case proves nothing", at)
				}
			} else {
				for i := 0; i < 3; i++ {
					buf.Write(line("assistant", "reply", "Step done."))
					buf.Write(line("user", "prompt", "Next step."))
				}
			}
			pad(1<<20 + 64<<10)
			buf.Write(line("user", "prompt", "Where are we?"))
			buf.Write(line("assistant", "reply", "TAIL-ANSWER"))
			file := (inbox.Store{Root: o.Dirs.Inbox()}).Path(o.Project.Key, "codex", "prefix-work")
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			r, err := Build(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(r.Text, "TAIL-ANSWER") {
				t.Fatalf("the chat with work before its tail was not chosen:\n%s", cut(r.Text, 800))
			}
		})
	}
}
