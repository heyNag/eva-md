package catchup

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"unicode"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/session"
)

// Regression tests: one long prompt row must not wedge recovery for the rest
// of the chat. Decoding a row of about 975 KB of Greek prose that contains the
// word "record", and checking it for a record control, can take longer than
// the hooks' 100 ms budget. Recovery must still make progress: it saves an
// attempt before decoding a big row, so the next pass scans that row instead
// of decoding it again.

// greekPaste is about n bytes of Greek prose holding the ASCII word
// "record", which defeats the cheap prefilter before the control check.
func greekPaste(n int) string {
	words := strings.Fields("Αυτό είναι ένα μεγάλο κείμενο που επικολλήθηκε από τον χρήστη για ανάλυση και περιέχει πολλές λέξεις όπως αρχείο σημείωση record καταγραφή")
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteString(words[i%len(words)])
		if i%17 == 16 {
			b.WriteByte('\n')
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// sessionStarted is the event the hook stores at SessionStart, so the
// session's inbox file exists, as it does in a real chat.
func sessionStarted(t *testing.T, e inbox.Entry) inbox.Entry {
	t.Helper()
	start := e
	start.Role, start.Kind, start.Text, start.Origin = "event", "event", "session_start", "hook"
	return start
}

func TestLongPromptRowNeverWedgesRecovery(t *testing.T) {
	for _, contended := range []bool{false, true} {
		name := "idle"
		if contended {
			name = "contended"
		}
		t.Run(name, func(t *testing.T) {
			dirs, file, e := fixture(t, "claude-code")
			if err := (inbox.Store{Root: dirs.Inbox()}).Append(sessionStarted(t, e)); err != nil {
				t.Fatal(err)
			}
			appendJSON(t, file, claudeRow(e, "user", greekPaste(975<<10), "turn-1"))
			appendJSON(t, file, claudeRow(e, "assistant", "REPLY-ONE: the text is a log.", "turn-1"))
			appendJSON(t, file, claudeRow(e, "user", "PROMPT-TWO: summarize it", "turn-2"))
			if contended {
				// About half a CPU, as on a busy Mac.
				defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
				var stop atomic.Bool
				defer stop.Store(true)
				go func() {
					for !stop.Load() {
						for i := 0; i < 1e5; i++ {
						}
						runtime.Gosched()
					}
				}()
			}
			hookPasses(t, dirs, e, 6)
			assertCaptureOn(t, dirs, e)
			s, _, _ := load(context.Background(), dirs, e.Host, e.Session)
			info, _ := os.Stat(file)
			if s.Offset != info.Size() || s.Skip != nil || s.Slow != nil {
				t.Fatalf("cursor=%d size=%d skip=%v slow=%+v", s.Offset, info.Size(), s.Skip != nil, s.Slow)
			}
			rows := readInbox(t, dirs, e)
			if countText(rows, "assistant", "REPLY-ONE") != 1 || countText(rows, "user", "PROMPT-TWO") != 1 {
				t.Fatalf("later rows missing: %d rows", len(rows))
			}
			// The paste is kept, bounded, or leaves a gap; never lost silently.
			if countText(rows, "user", "Αυτό") != 1 && countText(rows, "event", "gap: transcript record") != 1 {
				t.Fatalf("paste neither kept nor marked: %d rows", len(rows))
			}
		})
	}
}

// A pass that began decoding a big row and did not finish it, out of time or
// because the hook's process ended, leaves the next pass to scan the row: it
// leaves a gap event and recovery goes on, and a control in it still holds.
func TestUnfinishedBigRowIsScanned(t *testing.T) {
	for _, control := range []bool{false, true} {
		dirs, file, e := fixture(t, "claude-code")
		text := greekPaste(400 << 10)
		if control {
			text = "off the record\nPRIVATE " + text
		}
		appendJSON(t, file, claudeRow(e, "user", text, "turn-1"))
		appendJSON(t, file, claudeRow(e, "assistant", "AFTER-REPLY", "turn-1"))
		ctx := context.Background()
		s, _, err := load(ctx, dirs, e.Host, e.Session)
		if err != nil {
			t.Fatal(err)
		}
		s.Slow = &slowRow{Offset: s.Offset, Decode: 1}
		if err := save(ctx, dirs, s); err != nil {
			t.Fatal(err)
		}
		calls := 0
		withRedactor(t, func(v string) (string, int) {
			calls++
			return redact.Redact(v)
		})
		// The scan ends the pass's window; the next pass reads on.
		for pass := 0; ; pass++ {
			_, err := Recover(ctx, dirs, e.Host, e.Session)
			if err == nil {
				break
			}
			if !errors.Is(err, ErrPending) || pass == 3 {
				t.Fatal(err)
			}
		}
		rows := readInbox(t, dirs, e)
		if control {
			if !offMarker(t, dirs, e) || strings.Contains(inboxBytes(t, dirs, e), "PRIVATE") || len(rows) != 0 {
				t.Fatalf("control in a scanned row ignored: %d rows", len(rows))
			}
			continue
		}
		if len(rows) != 2 || rows[0].Text != gapTooLong || rows[1].Text != "AFTER-REPLY" {
			t.Fatalf("rows=%#v", rows)
		}
		if calls != 1 {
			t.Fatalf("the scanned row was redacted: %d calls", calls)
		}
		if s, _, _ := load(ctx, dirs, e.Host, e.Session); s.Slow != nil || s.Skip != nil {
			t.Fatalf("slow=%+v skip=%v", s.Slow, s.Skip != nil)
		}
	}
}

// Before a big row is decoded, the pass saves that it began it.
func TestBigRowAttemptIsSavedBeforeDecoding(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	appendJSON(t, file, claudeRow(e, "user", greekPaste(300<<10), "turn-1"))
	saved := -1
	withRedactor(t, func(v string) (string, int) {
		// Runs after the row is decoded; the state on disk is what a pass
		// cut short during the decoding would have left.
		if s, _, err := load(context.Background(), dirs, e.Host, e.Session); err == nil && s.Slow != nil {
			saved = s.Slow.Decode
		}
		return redact.Redact(v)
	})
	if _, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil {
		t.Fatal(err)
	}
	if saved != 1 {
		t.Fatalf("attempt on disk before decoding: %d", saved)
	}
	if s, _, _ := load(context.Background(), dirs, e.Host, e.Session); s.Slow != nil {
		t.Fatalf("slow=%+v", s.Slow)
	}
}

// The streaming control check used for long texts gives RecordControl's
// answer, wherever in a long text a control line is.
func TestRecordControlStreamsLongText(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	filler := greekPaste(streamControl + 4<<10)
	for i := 0; i < 300; i++ {
		lines := strings.Split(filler, "\n")
		for k := r.Intn(3); k > 0; k-- {
			lines[r.Intn(len(lines))] = []string{"off the record", " Back on the record! ", "off the records", "record", "OFF THE RECORD.", "off\u00a0the record"}[r.Intn(6)]
		}
		text := strings.Join(lines, "\n")
		if r.Intn(4) == 0 {
			text = "  off the record " + text
		}
		if got, want := recordControl(text), session.RecordControl(text); got != want {
			t.Fatalf("long text %d: got %q want %q", i, got, want)
		}
	}
}

// lowerHead is the start of strings.ToLower(strings.TrimSpace(text)).
func TestLowerHeadMatchesFullLowering(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	parts := []string{" ", "\t", "\n", "\u00a0", "/loop", "/LOOP ", "<System-Reminder>", "İ", "ẞ", "Σ", "x", "<bash-input>", "[Request interrupted by user", "\u2028", "日本"}
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for k := r.Intn(40); k > 0; k-- {
			b.WriteString(parts[r.Intn(len(parts))])
		}
		if r.Intn(10) == 0 {
			b.WriteString(strings.Repeat(" ", 300))
		}
		text := b.String()
		full := strings.ToLower(strings.TrimSpace(text))
		if head := lowerHead(text); !strings.HasPrefix(full, head) || len([]rune(head)) < min(headRunes, len([]rune(strings.TrimFunc(text, unicode.IsSpace)))) {
			t.Fatalf("lowerHead(%q) = %q, not the start of %q", text, head, full)
		}
	}
}

// A recovered reply that the inbox would keep whole, up to 200 KiB, is
// redacted whole when that fits the pass; only a longer one, or one that
// outruns the pass, is cut to head and tail windows.
func TestLongReplyKeptWhole(t *testing.T) {
	secret := strings.Join([]string{"gh", "p_", strings.Repeat("Zq9x", 9)}, "")
	for _, c := range []struct {
		size  int
		whole bool
	}{{100 << 10, true}, {190 << 10, true}, {250 << 10, false}} {
		dirs, file, e := fixture(t, "claude-code")
		text := "LONG-START " + secret + "\n" + longText("prose", c.size, int64(c.size)) + "\nLONG-END\n"
		appendJSON(t, file, claudeRow(e, "user", "PROMPT-ONE", "turn-1"))
		appendJSON(t, file, claudeRow(e, "assistant", text, "turn-1"))
		if _, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil {
			t.Fatal(err)
		}
		rows := readInbox(t, dirs, e)
		if len(rows) != 2 || rows[1].Role != "assistant" || strings.Contains(inboxBytes(t, dirs, e), secret) {
			t.Fatalf("%d KiB: %d rows", c.size>>10, len(rows))
		}
		reply := rows[1]
		if c.whole {
			if reply.Trunc != 0 || !strings.HasSuffix(reply.Text, "LONG-END\n") || len(reply.Text) < len(text)-len(secret) {
				t.Fatalf("%d KiB: kept %d of %d bytes, %d left out", c.size>>10, len(reply.Text), len(text), reply.Trunc)
			}
			continue
		}
		if reply.Trunc == 0 || len(reply.Text) > 200<<10 || !strings.HasPrefix(reply.Text, "LONG-START") || !strings.HasSuffix(reply.Text, "LONG-END\n") {
			t.Fatalf("%d KiB: kept %d bytes, %d left out", c.size>>10, len(reply.Text), reply.Trunc)
		}
	}
}
