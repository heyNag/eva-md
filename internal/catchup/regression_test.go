package catchup

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

// Regression tests for transcript recovery: a row over 1 MiB never turns the
// chat off the record, a long row never wedges recovery, rows the user did
// not type never become prompts, and an explicit request writes its own
// marker value. They drive Recover the way the hooks do.

// hookPasses runs Recover as successive hook events would, each with the
// hooks' 100 ms recovery budget, until a pass finishes with nothing pending.
// It fails if that takes more than max passes or if a pass overruns.
func hookPasses(t *testing.T, dirs paths.Dirs, e inbox.Entry, max int) int {
	t.Helper()
	for pass := 1; pass <= max; pass++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		start := time.Now()
		_, err := Recover(ctx, dirs, e.Host, e.Session)
		cancel()
		if took := time.Since(start); took > 2*time.Second {
			t.Fatalf("pass %d took %v", pass, took)
		}
		if err == nil {
			return pass
		}
		if !errors.Is(err, ErrPending) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	s, _, _ := load(context.Background(), dirs, e.Host, e.Session)
	t.Fatalf("recovery still pending after %d passes (cursor %d, reason %q)", max, s.Offset, s.Reason)
	return 0
}

func offMarker(t *testing.T, dirs paths.Dirs, e inbox.Entry) bool {
	t.Helper()
	_, off, err := (session.Markers{Root: dirs.Sessions()}).GetContext(context.Background(), e.Host, e.Session, session.OffRecord)
	if err != nil {
		t.Fatal(err)
	}
	return off
}

func inboxBytes(t *testing.T, dirs paths.Dirs, e inbox.Entry) string {
	t.Helper()
	data, err := os.ReadFile((inbox.Store{Root: dirs.Inbox()}).Path(e.Project, e.Host, e.Session))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func countText(rows []inbox.Entry, role, text string) int {
	n := 0
	for _, r := range rows {
		if r.Role == role && strings.Contains(r.Text, text) {
			n++
		}
	}
	return n
}

// imageRow is a Claude user row with the user's text and a pasted image of
// about 1.2 MiB, as Claude stores a screenshot.
func imageRow(e inbox.Entry, text, turn string) map[string]any {
	row := claudeRow(e, "user", text, turn)
	image := base64.StdEncoding.EncodeToString(make([]byte, 900<<10))
	row["message"].(map[string]any)["content"] = []any{
		map[string]any{"type": "text", "text": text},
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": image}},
	}
	return row
}

func assertCaptureOn(t *testing.T, dirs paths.Dirs, e inbox.Entry) {
	t.Helper()
	if offMarker(t, dirs, e) {
		t.Fatal("a size limit turned the chat off the record")
	}
	s, _, err := load(context.Background(), dirs, e.Host, e.Session)
	if err != nil || s.Blocked {
		t.Fatalf("recovery blocked: blocked=%v reason=%q err=%v", s.Blocked, s.Reason, err)
	}
}

// A pasted screenshot makes a row over 1 MiB. Recovery must move
// past it, leave one gap event, and keep recovering later turns.
func TestOversizedImageRowDoesNotStopCapture(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	appendJSON(t, file, imageRow(e, "Here is a screenshot of the broken layout", "turn-1"))
	appendJSON(t, file, claudeRow(e, "assistant", "REPLY-ONE: the sidebar overlaps the header.", "turn-1"))
	hookPasses(t, dirs, e, 5)
	appendJSON(t, file, claudeRow(e, "user", "PROMPT-TWO: please fix the overlap", "turn-2"))
	appendJSON(t, file, claudeRow(e, "assistant", "REPLY-TWO: fixed in layout.css", "turn-2"))
	hookPasses(t, dirs, e, 5)
	assertCaptureOn(t, dirs, e)
	rows := readInbox(t, dirs, e)
	for _, want := range []struct{ role, text string }{{"assistant", "REPLY-ONE"}, {"user", "PROMPT-TWO"}, {"assistant", "REPLY-TWO"}} {
		if countText(rows, want.role, want.text) != 1 {
			t.Fatalf("missing %s %s: %#v", want.role, want.text, rows)
		}
	}
	if n := countText(rows, "event", "gap: transcript record too large"); n != 1 {
		t.Fatalf("gap events=%d", n)
	}
	// Repeating recovery adds nothing.
	if n, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil || n != 0 || len(readInbox(t, dirs, e)) != len(rows) {
		t.Fatalf("repeat=%d %v", n, err)
	}
}

// A mid-turn tool result over 1 MiB, such as a Read of a PDF, must neither
// hold back the Stop hook's capture for good nor turn the chat off the record.
func TestOversizedToolResultMidTurnDoesNotStopCapture(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	appendJSON(t, file, claudeRow(e, "user", "Summarize the attached report", "turn-1"))
	call := claudeRow(e, "assistant", "Reading the report.", "turn-1")
	call["message"].(map[string]any)["stop_reason"] = "tool_use"
	appendJSON(t, file, call)
	pdf := base64.StdEncoding.EncodeToString(make([]byte, 600<<10))
	result := claudeRow(e, "user", "", "turn-1")
	result["message"].(map[string]any)["content"] = []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": []any{
		map[string]any{"type": "document", "source": map[string]any{"type": "base64", "media_type": "application/pdf", "data": pdf}},
		map[string]any{"type": "text", "text": "off the record\n(this line is inside the document, not the user's words)"},
	}}}
	result["toolUseResult"] = map[string]any{"type": "pdf", "file": map[string]any{"base64": pdf}}
	appendJSON(t, file, result)
	appendJSON(t, file, claudeRow(e, "assistant", "REPLY-ONE: the report lists three risks.", "turn-1"))
	appendJSON(t, file, claudeRow(e, "user", "PROMPT-TWO: which risk is largest?", "turn-2"))
	hookPasses(t, dirs, e, 6)
	assertCaptureOn(t, dirs, e)
	rows := readInbox(t, dirs, e)
	if countText(rows, "assistant", "REPLY-ONE") != 1 || countText(rows, "user", "PROMPT-TWO") != 1 {
		t.Fatalf("later rows missing: %#v", rows)
	}
	if n := countText(rows, "event", "gap: transcript record too large"); n != 1 {
		t.Fatalf("gap events=%d", n)
	}
}

// The privacy check still reads the user's text in an oversized row: a
// screenshot sent with "off the record" keeps everything after it private.
func TestOversizedRowControlIsHonoured(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	appendJSON(t, file, claudeRow(e, "user", "Public question first.", "turn-1"))
	appendJSON(t, file, imageRow(e, "off the record\nPRIVATE: what does this scan show?", "turn-2"))
	appendJSON(t, file, claudeRow(e, "assistant", "PRIVATE REPLY", "turn-2"))
	for i := 0; i < 4; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, _ = Recover(ctx, dirs, e.Host, e.Session)
		cancel()
	}
	if !offMarker(t, dirs, e) {
		t.Fatal("control in an oversized row was not honoured")
	}
	if strings.Contains(inboxBytes(t, dirs, e), "PRIVATE") {
		t.Fatal("private text recovered")
	}
	if rows := readInbox(t, dirs, e); len(rows) != 1 || rows[0].Text != "Public question first." {
		t.Fatalf("rows=%#v", rows)
	}
}

// The user's request saves its own reason and marker value.
func TestExplicitOffRecordIsMarked(t *testing.T) {
	dirs, _, e := fixture(t, "claude-code")
	ctx := context.Background()
	if err := SetOffRecord(ctx, dirs, e.Host, e.Session, true); err != nil {
		t.Fatal(err)
	}
	s, _, _ := load(ctx, dirs, e.Host, e.Session)
	value, off, _ := (session.Markers{Root: dirs.Sessions()}).GetContext(ctx, e.Host, e.Session, session.OffRecord)
	if !s.Blocked || s.Reason != "off_record" || !off || value != OffRecordAsked {
		t.Fatalf("blocked=%v reason=%q marker=%q", s.Blocked, s.Reason, value)
	}
}

// Text generators for long rows. Both use the words that make the redactor
// work hardest (token, key, secret, password), as real code and logs do.
func longText(kind string, n int, seed int64) string {
	r := rand.New(rand.NewSource(seed))
	var b strings.Builder
	switch kind {
	case "code":
		code := []string{
			"func (c *Client) refreshToken(ctx context.Context, key string) (string, error) {",
			"\ttoken, err := c.store.Get(ctx, \"session-token\")",
			"\tif err != nil { return \"\", fmt.Errorf(\"secret lookup: %w\", err) }",
			"\tpassword := os.Getenv(\"DB_PASSWORD\")",
			"\treturn c.http.Do(req.WithContext(ctx)) // retry with backoff",
		}
		for b.Len() < n {
			switch r.Intn(3) {
			case 0:
				fmt.Fprintf(&b, "2026-10-29T10:01:%02d INFO auth: token refresh ok user_id=%d request_id=%x\n", r.Intn(60), r.Intn(100000), r.Int63())
			case 1:
				fmt.Fprintf(&b, "  \"access_token_expiry\": 3600, \"client_id\": \"svc-%d\", \"key\": \"k%d\",\n", r.Intn(1000), r.Intn(1000))
			default:
				b.WriteString(code[r.Intn(len(code))])
				b.WriteByte('\n')
			}
		}
	default:
		words := strings.Fields("the hook stores each prompt and reply in a private inbox so that a new chat can resume where the last one stopped; the session token and the signing key never leave the machine, and a secret that reaches a transcript is redacted before it is kept. Recovery reads the transcript only for a session a hook registered, and a password typed by mistake is replaced with a marker.")
		for b.Len() < n {
			for i := 0; i < 12+r.Intn(20) && b.Len() < n; i++ {
				b.WriteString(words[r.Intn(len(words))])
				b.WriteByte(' ')
			}
			b.WriteString("\n")
		}
	}
	return b.String()[:n]
}

// Long replies and pasted prompts recover within the hooks' budget,
// one bounded redaction each, and the turns after them are still stored.
func TestLongRowsDoNotWedgeRecovery(t *testing.T) {
	secret := strings.Join([]string{"gh", "p_", strings.Repeat("Zq9x", 9)}, "")
	for _, size := range []int{100, 150, 250} {
		for _, kind := range []string{"prose", "code"} {
			for _, where := range []string{"reply", "paste", "paste-captured"} {
				t.Run(fmt.Sprintf("%s-%s-%dKiB", where, kind, size), func(t *testing.T) {
					dirs, file, e := fixture(t, "claude-code")
					big := "LONG-START " + secret + "\n" + longText(kind, size<<10, int64(size)) + "\nLONG-END " + secret + "\n"
					switch where {
					case "reply":
						appendJSON(t, file, claudeRow(e, "user", "PROMPT-ONE: review this module", "turn-1"))
						appendJSON(t, file, claudeRow(e, "assistant", big, "turn-1"))
					default:
						if where == "paste-captured" {
							// The prompt hook stored the paste before the
							// transcript row was read, as it usually does.
							// Duplicates are found by turn, so a short copy
							// stands in for it.
							prompt := e
							prompt.Role, prompt.Kind, prompt.Text, prompt.Turn, prompt.Origin = "user", "prompt", "HOOK-COPY of the paste", "turn-1", "hook"
							if err := (inbox.Store{Root: dirs.Inbox()}).Append(prompt); err != nil {
								t.Fatal(err)
							}
						}
						appendJSON(t, file, claudeRow(e, "user", big, "turn-1"))
						appendJSON(t, file, claudeRow(e, "assistant", "REPLY-ONE: noted.", "turn-1"))
					}
					appendJSON(t, file, claudeRow(e, "user", "PROMPT-TWO: what about retention?", "turn-2"))
					appendJSON(t, file, claudeRow(e, "assistant", "REPLY-TWO: keep it.", "turn-2"))
					appendJSON(t, file, claudeRow(e, "user", "PROMPT-THREE: thanks", "turn-3"))
					// The claim is that recovery finishes, not how fast, so
					// the cap leaves room for a slow or busy machine.
					passes := hookPasses(t, dirs, e, 30)
					s, _, _ := load(context.Background(), dirs, e.Host, e.Session)
					info, _ := os.Stat(file)
					if s.Offset != info.Size() || s.Blocked || offMarker(t, dirs, e) {
						t.Fatalf("cursor=%d size=%d blocked=%v", s.Offset, info.Size(), s.Blocked)
					}
					rows := readInbox(t, dirs, e)
					for _, want := range []struct{ role, text string }{{"user", "PROMPT-TWO"}, {"assistant", "REPLY-TWO"}, {"user", "PROMPT-THREE"}} {
						if countText(rows, want.role, want.text) != 1 {
							t.Fatalf("missing %s %q", want.role, want.text)
						}
					}
					role := "assistant"
					if where != "reply" {
						role = "user"
					}
					switch {
					case where == "paste-captured":
						// Recovery found the stored turn and added nothing.
						if countText(rows, "user", "HOOK-COPY") != 1 || countText(rows, "user", "LONG-START") != 0 || countText(rows, "event", "gap:") != 0 {
							t.Fatalf("stored paste duplicated: %d rows", len(rows))
						}
					case countText(rows, role, "LONG-START") != 1 && countText(rows, "event", "gap: transcript record too long") != 1:
						// The long row is kept, bounded, or leaves a gap event;
						// it is never silently lost.
						t.Fatalf("long row neither kept nor marked: %d rows", len(rows))
					}
					for _, r := range rows {
						if strings.Contains(r.Text, "LONG-START") || strings.HasPrefix(r.Text, "gap:") {
							t.Logf("passes=%d stored %s %s: %d bytes, %d left out", passes, r.Origin, r.Role, len(r.Text), r.Trunc)
						}
					}
					if strings.Contains(inboxBytes(t, dirs, e), secret) {
						t.Fatal("secret stored")
					}
				})
			}
		}
	}
}

// Claude writes user rows that the user did not type as prompts. None
// of them may become a [user] prompt, and command output is never stored.
func TestHostRowsAreNeverUserPrompts(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	value := strings.Join([]string{"postgres://app:", "Hx7", "vQ2", "@db.internal/prod"}, "")
	for i, text := range []string{
		"<bash-input>cat .env</bash-input>",
		"<bash-stdout>DATABASE_URL=" + value + "\nBASH-STDOUT-LINE</bash-stdout><bash-stderr>BASH-STDERR-LINE</bash-stderr>",
		"<bash-stderr>cat: missing: No such file or directory</bash-stderr>",
		"[Request interrupted by user]",
		"[Request interrupted by user for tool use]",
		"<command-message>review is running…</command-message>\n<command-name>/review</command-name>\n<command-args>COMMAND-ARGS</command-args>",
		"<command-name>/model</command-name>",
		"<local-command-stdout>LOCAL-COMMAND-OUTPUT</local-command-stdout>",
		"<local-command-stderr>LOCAL-COMMAND-ERROR</local-command-stderr>",
	} {
		appendJSON(t, file, claudeRow(e, "user", text, fmt.Sprintf("host-%d", i)))
	}
	// A user row without a prompt id is not known to be typed either.
	noID := claudeRow(e, "user", "NO-PROMPT-ID ROW", "")
	delete(noID, "promptId")
	appendJSON(t, file, noID)
	appendJSON(t, file, claudeRow(e, "user", "TYPED-PROMPT: run the tests", "typed-1"))
	appendJSON(t, file, claudeRow(e, "assistant", "TYPED-REPLY: all green.", "typed-1"))
	if _, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil {
		t.Fatal(err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 2 || rows[0].Role != "user" || rows[0].Text != "TYPED-PROMPT: run the tests" || rows[1].Text != "TYPED-REPLY: all green." {
		t.Fatalf("rows=%#v", rows)
	}
	stored := inboxBytes(t, dirs, e)
	for _, banned := range []string{"bash", "BASH-", "postgres", "Hx7", "interrupted", "review is running", "COMMAND-ARGS", "LOCAL-COMMAND", "NO-PROMPT-ID"} {
		if strings.Contains(stored, banned) {
			t.Fatalf("host row text stored: %q", banned)
		}
	}
}

// A privacy control typed beside a pasted image is still a control:
// recovery reads the text items of a mixed row.
func TestControlBesideAttachmentIsHonoured(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	row := claudeRow(e, "user", "off the record\nPRIVATE: your results", "turn-1")
	row["message"].(map[string]any)["content"] = append(row["message"].(map[string]any)["content"].([]any),
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="}})
	appendJSON(t, file, row)
	appendJSON(t, file, claudeRow(e, "assistant", "PRIVATE: elevated values.", "turn-1"))
	if _, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil {
		t.Fatal(err)
	}
	if !offMarker(t, dirs, e) || strings.Contains(inboxBytes(t, dirs, e), "PRIVATE") {
		t.Fatal("control beside an attachment ignored")
	}
}
