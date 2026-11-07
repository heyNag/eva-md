package resume

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// BenchmarkProgramRunFiles measures what the files of chats a program ran
// add to a startup brief beyond the 64 files of a person's chats it reads,
// with as many of them as the brief reads beyond the 64: up to
// programFiles files and programBytes in all. Their entries are as an agent
// loop leaves them, many short tool lines and short replies holding quotes,
// tabs and line breaks, which cost far more to decode a MiB than a few long
// plain strings. Compare programs=0 with programs=64:
//
//	go test -run '^$' -bench BenchmarkProgramRunFiles -count 6 ./internal/resume
func BenchmarkProgramRunFiles(b *testing.B) {
	for _, programs := range []int{0, programFiles} {
		b.Run(fmt.Sprintf("programs=%d", programs), func(b *testing.B) {
			home := b.TempDir()
			b.Setenv("HOME", home)
			b.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
			b.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			o := Options{Dirs: paths.Dirs{State: filepath.Join(home, "state"), Share: filepath.Join(home, "share"), Config: filepath.Join(home, "config")},
				Config: config.Config{WikiPath: filepath.Join(home, "wiki")}, Project: identity.Project{Kind: identity.KindRepo, Key: "github.com__team__resume", Slug: "resume", Remote: "github.com/team/resume", Branch: "main"},
				Host: "claude-code", Session: "new-chat", Variant: "startup", Now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
			for i := range maxFiles {
				benchChat(b, o, fmt.Sprintf("person-%02d", i), o.Now.Add(-time.Hour-time.Duration(i)*time.Minute), false, 16<<10)
			}
			// Each as large as the bound lets all of them be.
			size := programBytes / programFiles
			for i := range programs {
				benchChat(b, o, fmt.Sprintf("worker-%02d", i), o.Now.Add(-time.Duration(i+1)*time.Second), true, size)
			}
			r, err := Build(context.Background(), o)
			if err != nil || len(r.Files) != maxFiles+programs {
				b.Fatalf("the brief listed %d files: %v", len(r.Files), err)
			}
			for b.Loop() {
				if _, err := Build(context.Background(), o); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// benchChat writes a session file of at most size bytes for the project, as
// the hooks store a chat: a person's starts with a prompt typed, a
// program's with the host_prompt event and no typed prompt, then as many
// tool lines and replies as fit. Its file last changed at when.
func benchChat(b *testing.B, o Options, sid string, when time.Time, program bool, size int) {
	b.Helper()
	file := inbox.Store{Root: o.Dirs.Inbox()}.Path(o.Project.Key, "claude-code", sid)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		b.Fatal(err)
	}
	var text strings.Builder
	line := func(role, kind, value string) string {
		data, err := json.Marshal(inbox.Entry{V: 1, Host: "claude-code", Session: sid, Project: o.Project.Key, Branch: "main", TS: when.Format(time.RFC3339), Role: role, Kind: kind, Text: value, Origin: "hook"})
		if err != nil {
			b.Fatal(err)
		}
		return string(data) + "\n"
	}
	text.WriteString(line("event", "event", "session_start: startup"))
	if program {
		text.WriteString(line("event", "event", "host_prompt"))
	} else {
		text.WriteString(line("user", "prompt", "Check the \"replay\" step again.\nThen run the tests."))
	}
	pair := line("tool", "tool", `Bash go test ./internal/... -run "TestReplay" (exit 0)`) +
		line("assistant", "reply", strings.Repeat("The worker's \"report\": the step passed.\n\tDetail: /src/replay/step.go:12 returned {\"ok\": true}.\n", 3))
	for text.Len()+len(pair) <= size {
		text.WriteString(pair)
	}
	if err := os.WriteFile(file, []byte(text.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	if err := os.Chtimes(file, when, when); err != nil {
		b.Fatal(err)
	}
}
