package catchup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/session"
)

// controlWant is what the parser's check gives for a text.
func controlWant(text string) string {
	if strings.TrimSpace(text) == "" || syntheticText(text) {
		return ""
	}
	return session.RecordControl(text)
}

func randomText(r *rand.Rand) string {
	words := []string{"off", "the", "record", "back", "on", "OFF", "The", "Record", "RECORD", "records", "bacK", "x", "é", "日本", "0ff", "\x00", "\u200b"}
	seps := []string{" ", "  ", "\t", "\n", "\r\n", "!", ".", "—", "\u00a0", "\u0085", "�", "😀", "\n\n", " - "}
	var b strings.Builder
	if r.Intn(8) == 0 {
		b.WriteString([]string{"<system-reminder>", "/clear ", "  ", "\n"}[r.Intn(4)])
	}
	for i, n := 0, r.Intn(9); i < n; i++ {
		b.WriteString(words[r.Intn(len(words))])
		b.WriteString(seps[r.Intn(len(seps))])
	}
	return b.String()
}

// The streaming check, the prefilter and merging of text items all agree
// with session.RecordControl.
func TestCtlTextMatchesRecordControl(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	fixed := []string{"off the record", " Off, the record!\nrest", "back on the record", "x\n off the record \ny", "off the records", "off the\nrecord", "back on the record\noff the record", "<system-reminder>\noff the record", "bacK on the record", strings.Repeat(" ", 5000) + "off the record", "\u00a0off\u00a0the\u00a0record", ""}
	for i := 0; i < 20000; i++ {
		text := randomText(r)
		if i < len(fixed) {
			text = fixed[i]
		}
		want := controlWant(text)
		if got := recordControl(text); got != session.RecordControl(text) {
			t.Fatalf("prefilter %q: %q", text, got)
		}
		var c ctlText
		for _, ch := range text {
			c.rune(ch)
		}
		if got := c.control(); got != want {
			t.Fatalf("stream %q: got %q want %q", text, got, want)
		}
		// Items split at newlines and joined back with newlines.
		var row ctlText
		for _, part := range strings.SplitAfterN(text, "\n", 1+r.Intn(4)) {
			part = strings.TrimSuffix(part, "\n")
			var item ctlText
			for _, ch := range part {
				item.rune(ch)
			}
			item.finish()
			row.merge(&item)
		}
		if got := row.control(); got != want {
			t.Fatalf("merged %q: got %q want %q", text, got, want)
		}
	}
}

func scanRow(t *testing.T, row []byte, chunks func(int) int, roundTrip bool) *rowScan {
	t.Helper()
	z := &rowScan{}
	for i := 0; i < len(row); {
		n := min(chunks(len(row)-i), len(row)-i)
		z.feed(row[i : i+n])
		i += n
		if roundTrip {
			data, err := json.Marshal(z)
			if err != nil {
				t.Fatal(err)
			}
			var back rowScan
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatal(err)
			}
			z = &back
		}
	}
	return z
}

// rowScan reaches the same verdict as the parser for every row shape, however
// the row is cut into chunks, and when its state is saved between chunks.
func TestRowScanMatchesParser(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	claude := Source{Host: "claude-code", Session: "s1", Cwd: cwd, Entry: inbox.Entry{Host: "claude-code", Session: "s1"}}
	codex := Source{Host: "codex", Session: "s1", Cwd: cwd, Entry: inbox.Entry{Host: "codex", Session: "s1"}}
	user := func(content any, extra map[string]any) string {
		row := map[string]any{"type": "user", "sessionId": "s1", "cwd": cwd, "timestamp": "2026-10-29T10:01:00Z", "promptId": "p1", "message": map[string]any{"role": "user", "content": content}}
		for k, v := range extra {
			row[k] = v
		}
		data, _ := json.Marshal(row)
		return string(data)
	}
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	image := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "data": strings.Repeat("QUJD", 3000)}}
	big := strings.Repeat("a line of pasted text with \"quotes\", tabs\tand ünïcode 😀\n", 6000)
	rows := []struct {
		src Source
		row string
	}{
		{claude, user("off the record", nil)},
		{claude, user("back on the record", nil)},
		{claude, user("hello there", nil)},
		{claude, user([]any{text("off the record\nPRIVATE"), image}, nil)},
		{claude, user([]any{image, text("note"), text("back on the record")}, nil)},
		{claude, user([]any{text("off the"), text("record")}, nil)},
		{claude, user([]any{map[string]any{"type": "tool_result", "content": "off the record"}, text("off the record")}, nil)},
		{claude, user([]any{nil, text("off the record")}, nil)},
		{claude, user([]any{"off the record"}, nil)},
		{claude, user([]any{map[string]any{"text": "off the record", "type": "text"}}, nil)},
		{claude, user([]any{map[string]any{"text": "off the record", "type": "image"}}, nil)},
		{claude, user("off the record", map[string]any{"isMeta": true})},
		{claude, user("off the record", map[string]any{"isMeta": false})},
		{claude, user("off the record", map[string]any{"isSidechain": true})},
		{claude, user("off the record", map[string]any{"isCompactSummary": true})},
		{claude, user("off the record", map[string]any{"repeatsCompactSummary": "yes"})},
		{claude, user("off the record", map[string]any{"agentId": "a1"})},
		{claude, user("off the record", map[string]any{"turnOrigin": "agent"})},
		{claude, user("off the record", map[string]any{"turnOrigin": "human"})},
		{claude, user("off the record", map[string]any{"origin": map[string]any{"kind": "task"}})},
		{claude, user("off the record", map[string]any{"origin": "elsewhere"})},
		{claude, user("off the record", map[string]any{"sessionId": "other"})},
		{claude, user("off the record", map[string]any{"cwd": "/elsewhere"})},
		{claude, user("off the record", map[string]any{"timestamp": "yesterday"})},
		{claude, user("off the record", map[string]any{"type": "assistant"})},
		{claude, user("off the record", map[string]any{"message": map[string]any{"role": "assistant", "content": "off the record"}})},
		{claude, user("<system-reminder>\noff the record", nil)},
		{claude, user("<bash-stdout>\noff the record\n</bash-stdout>", nil)},
		{claude, user(big+"off the record\n", nil)},
		{claude, user([]any{text(big + "back on the record"), image}, map[string]any{"toolUseResult": map[string]any{"a": []any{map[string]any{"b": []any{1, 2, map[string]any{"c": "off the record"}}}}}})},
		{claude, `{"type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"of the\u0009record"}}`},
		{claude, `{"type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"😀 x\noff the record\ud800"}}`},
		{claude, `{"type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"\ud800A off the record \udc00"}}`},
		{claude, `{"type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"off the record` + "\xff\xfe" + `"}}`},
		{claude, `{"type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"off the record"},"message":{"role":"user","content":"fine"}}`},
		{claude, `{"type":"assistant","type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"off the record"}}`},
		{claude, `{"type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"off the record"}}`},
		{claude, `{"type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"off the record"}`},
		{claude, `{"type":"user","sessionId":"s1","cwd":"` + cwd + `","timestamp":"2026-10-29T10:01:00Z","message":{"role":"user","content":"off the record"}} trailing`},
		{claude, `["not", "an", "object"]`},
		{codex, `{"type":"event_msg","timestamp":"2026-10-29T10:01:00Z","payload":{"type":"user_message","message":"off the record"}}`},
		{codex, `{"type":"event_msg","timestamp":"2026-10-29T10:01:00Z","payload":{"message":"back on the record","type":"user_message","images":["` + strings.Repeat("QUJD", 1000) + `"]}}`},
		{codex, `{"type":"event_msg","timestamp":"2026-10-29T10:01:00Z","payload":{"type":"agent_message","message":"off the record"}}`},
		{codex, `{"type":"event_msg","timestamp":"2026-10-29T10:01:00Z","payload":"off the record"}`},
	}
	r := rand.New(rand.NewSource(3))
	for i, tc := range rows {
		p := newParser(tc.src)
		p.read([]byte(tc.row + "\n"))
		want := p.control
		// Small chunks, with the state saved and restored between them, for
		// small rows; long rows are cut at random into larger pieces.
		chunkings := map[string]func(int) int{
			"whole":  func(n int) int { return n },
			"random": func(int) int { return 1 + r.Intn(64<<10) },
		}
		if len(tc.row) < 40000 {
			chunkings["seven"] = func(int) int { return 7 }
			chunkings["random"] = func(int) int { return 1 + r.Intn(5000) }
		}
		if len(tc.row) < 4000 {
			chunkings["byte"] = func(int) int { return 1 }
		}
		for name, chunks := range chunkings {
			z := scanRow(t, []byte(tc.row), chunks, name != "whole")
			got, _ := z.verdict(tc.src)
			if got != want {
				t.Fatalf("row %d (%s): got %q want %q\n%.300s", i, name, got, want, tc.row)
			}
		}
	}
}

// Damaged or hostile input never makes the scanner panic, and its state stays
// small enough to save with the cursor.
func TestRowScanToleratesAnyInput(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	seeds := []string{`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"off the record"}]}}`, `{"a":[1,2,{"b":"\u12"}]}`, `{"message":{"content":[[[[[[[[`, strings.Repeat("[", 100), `{"cwd":"` + strings.Repeat("x", 9000) + `"}`, `{"` + strings.Repeat("k", 500) + `":1}`}
	for i := 0; i < 1000; i++ {
		row := []byte(seeds[r.Intn(len(seeds))])
		for j := r.Intn(6); j > 0; j-- {
			if len(row) == 0 {
				break
			}
			row[r.Intn(len(row))] = byte(r.Intn(256))
		}
		row = []byte(strings.ReplaceAll(string(row), "\n", " "))
		z := scanRow(t, row, func(int) int { return 1 + r.Intn(300) }, true)
		z.verdict(Source{Host: "claude-code", Session: "s", Cwd: "/"})
		if data, _ := json.Marshal(z); len(data) > 32<<10 {
			t.Fatalf("state of %d bytes", len(data))
		}
	}
}

func FuzzRowScan(f *testing.F) {
	f.Add([]byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"off the record"}]}}`), uint8(3))
	f.Add([]byte(`{"type":"event_msg","payload":{"type":"user_message","message":"\ud800x"}}`), uint8(1))
	f.Fuzz(func(t *testing.T, row []byte, step uint8) {
		row = []byte(strings.ReplaceAll(string(row), "\n", ""))
		n := int(step%32) + 1
		scanRow(t, row, func(int) int { return n }, step%2 == 0).verdict(Source{Host: "codex", Session: "s", Cwd: "/"})
	})
}

// A row too long to scan in one pass resumes where the last pass stopped,
// keeps capture deferred but never turns the chat off the record by itself,
// and honours a control found at its end.
func TestOversizedScanResumesAcrossPasses(t *testing.T) {
	oldChunk, oldPass := scanChunk, scanPerPass
	scanChunk, scanPerPass = 64<<10, 256<<10
	t.Cleanup(func() { scanChunk, scanPerPass = oldChunk, oldPass })
	for _, control := range []bool{false, true} {
		t.Run(fmt.Sprint("control=", control), func(t *testing.T) {
			dirs, file, e := fixture(t, "claude-code")
			ctx := context.Background()
			text := strings.Repeat("pasted log line with \"quotes\" and ünïcode\n", 70000)
			if control {
				text += "off the record\n"
			}
			appendJSON(t, file, claudeRow(e, "user", text, "turn-1"))
			appendJSON(t, file, claudeRow(e, "assistant", "REPLY-AFTER-LONG-ROW", "turn-1"))
			passes := 0
			for ; passes < 40; passes++ {
				_, err := Recover(ctx, dirs, e.Host, e.Session)
				if err == nil {
					break
				}
				if !errors.Is(err, ErrPending) {
					t.Fatal(err)
				}
				if offMarker(t, dirs, e) {
					t.Fatal("pending scan turned the chat off the record")
				}
				data, err := os.ReadFile(filepath.Join(dirs.State, "catchup", e.Host, e.Session+".json"))
				if err != nil || len(data) > 16<<10 || !strings.Contains(string(data), `"skip"`) {
					t.Fatalf("state %d bytes: %v", len(data), err)
				}
			}
			if passes < 8 || passes >= 40 {
				t.Fatalf("passes=%d", passes)
			}
			rows := readInbox(t, dirs, e)
			if control {
				if !offMarker(t, dirs, e) || len(rows) != 0 {
					t.Fatalf("control not honoured: %#v", rows)
				}
				return
			}
			assertCaptureOn(t, dirs, e)
			if len(rows) != 2 || rows[0].Text != gapTooLarge || rows[1].Text != "REPLY-AFTER-LONG-ROW" {
				t.Fatalf("rows=%#v", rows)
			}
		})
	}
}

// Codex rows over the window: a user message with large images leaves a gap
// and capture continues, unless its text is a record control.
func TestCodexOversizedRows(t *testing.T) {
	for _, message := range []string{"compare these screenshots", "off the record"} {
		t.Run(message, func(t *testing.T) {
			dirs, file, e := fixture(t, "codex")
			emit := func(payload any) {
				appendJSON(t, file, map[string]any{"type": "event_msg", "timestamp": "2026-10-29T10:01:00Z", "payload": payload})
			}
			image := "data:image/png;base64," + strings.Repeat("QUJD", 330<<10)
			emit(map[string]any{"type": "task_started", "turn_id": "turn1"})
			emit(map[string]any{"type": "user_message", "message": message, "images": []any{image}})
			emit(map[string]any{"type": "agent_message", "phase": "final", "message": "REPLY-AFTER-IMAGES"})
			hookPasses(t, dirs, e, 5)
			rows := readInbox(t, dirs, e)
			if message == "off the record" {
				if !offMarker(t, dirs, e) || len(rows) != 0 {
					t.Fatalf("control not honoured: %#v", rows)
				}
				return
			}
			assertCaptureOn(t, dirs, e)
			if len(rows) != 2 || rows[0].Text != gapTooLarge || rows[1].Text != "REPLY-AFTER-IMAGES" {
				t.Fatalf("rows=%#v", rows)
			}
		})
	}
}

// The gap event is written before the cursor moves past an oversized row: if
// the write fails, the next pass writes it, without reading into the next row.
func TestOversizedGapWrittenBeforeCursorMoves(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	appendJSON(t, file, imageRow(e, "screenshot", "turn-1"))
	appendJSON(t, file, claudeRow(e, "user", "NEXT ROW", "turn-2"))
	// Make the inbox unwritable for one pass.
	blocker := (inbox.Store{Root: dirs.Inbox()}).Path(e.Project, e.Host, e.Session)
	if err := os.MkdirAll(blocker, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Recover(ctx, dirs, e.Host, e.Session); err == nil {
		t.Fatal("expected the gap write to fail")
	}
	s, _, _ := load(ctx, dirs, e.Host, e.Session)
	if s.Offset != 0 || s.Skip == nil || !s.Skip.Done {
		t.Fatalf("cursor=%d skip=%+v", s.Offset, s.Skip)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
		t.Fatalf("recover=%d %v", n, err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 2 || rows[0].Text != gapTooLarge || rows[1].Text != "NEXT ROW" {
		t.Fatalf("rows=%#v", rows)
	}
}

// A row that began before registration and runs past the window is skipped
// quietly: it predates capture, so it is not a gap.
func TestPartialOversizedRowBeforeRegistrationIsSkipped(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"user","message":{"content":"` + strings.Repeat("x", 10))
	f.Close()
	s, _, _ := load(ctx, dirs, e.Host, e.Session)
	info, _ := os.Stat(file)
	s.Offset = info.Size() // a cursor taken while the host was writing
	if err := save(ctx, dirs, s); err != nil {
		t.Fatal(err)
	}
	f, _ = os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(strings.Repeat("y", 1200<<10) + `"}}` + "\n")
	f.Close()
	appendJSON(t, file, claudeRow(e, "user", "after registration", "turn-1"))
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
		t.Fatalf("recover=%d %v", n, err)
	}
	if rows := readInbox(t, dirs, e); len(rows) != 1 || rows[0].Text != "after registration" {
		t.Fatalf("rows=%#v", rows)
	}
}

// boundedRedact keeps exactly the lines a full redaction would show at the
// head and the tail of the text, even with secrets at the cuts.
func TestBoundedRedactMatchesFullRedaction(t *testing.T) {
	token := func(r *rand.Rand) string {
		const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
		b := make([]byte, 36)
		for i := range b {
			b[i] = alphabet[r.Intn(len(alphabet))]
		}
		return string(b)
	}
	pem := strings.Join([]string{"-----BEGIN RSA ", "PRIVATE KEY-----\n", strings.Repeat("MIIEowIBAAKCAQEAx9", 3), "\n", strings.Repeat("Zq0Lm8", 10), "\n-----END RSA ", "PRIVATE KEY-----\n"}, "")
	// Small windows put many secrets and private key blocks at the cuts.
	lim := limits{full: 8 << 10, head: 2 << 10, tail: 2 << 10}
	r := rand.New(rand.NewSource(5))
	tails := 0
	for i := 0; i < 24; i++ {
		var b strings.Builder
		var secrets []string
		for b.Len() < 12<<10 {
			switch r.Intn(14) {
			case 0:
				s := strings.Join([]string{"gh", "p_", token(r)}, "")
				secrets = append(secrets, s)
				b.WriteString("export GITHUB=" + s + "\n")
			case 1:
				s := token(r)[:24]
				secrets = append(secrets, s)
				b.WriteString("the api token is\n  " + s + " for staging\n") // trigger word on the line above
			case 2:
				secrets = append(secrets, "Zq0Lm8Zq0Lm8")
				b.WriteString(pem)
			case 3:
				b.WriteString("\x1b[31mcolored\x1b[0m line\r\n")
			case 4:
				b.WriteString(strings.Repeat("w", 300+r.Intn(1200)) + "\n")
			default:
				fmt.Fprintf(&b, "line %d with ordinary words and a key name\n", r.Intn(100000))
			}
		}
		text := b.String()
		out := boundedRedact(text, lim, redact.Redact)
		full, _ := redact.Redact(text)
		if out.gap || out.trunc <= 0 || len(out.text) > 4*lim.head {
			t.Fatalf("case %d: gap=%v trunc=%d len=%d", i, out.gap, out.trunc, len(out.text))
		}
		for _, s := range secrets {
			if strings.Contains(out.text, s) {
				t.Fatalf("case %d: secret kept: %q", i, s)
			}
		}
		if !headAndTailOf(full, out.text) {
			t.Fatalf("case %d: kept text is not whole lines of the full redaction", i)
		}
		// Private key blocks are replaced before the cut, so no tail has to
		// be dropped for a changed line count.
		if last := full[strings.LastIndexByte(full[:len(full)-1], '\n')+1:]; strings.HasSuffix(out.text, last) {
			tails++
		}
	}
	if tails != 24 {
		t.Fatalf("tail kept in only %d of 24 cases", tails)
	}
	// A text with no line break near either end has no safe cut: it is a gap.
	if out := boundedRedact(strings.Repeat("x", 12<<10), lim, redact.Redact); !out.gap {
		t.Fatal("single long line kept")
	}
}

// headAndTailOf reports whether kept is some leading lines of full followed by
// some trailing lines of full.
func headAndTailOf(full, kept string) bool {
	for head := 0; ; {
		// kept[:head] is whole leading lines of full; is the rest whole
		// trailing lines, after them?
		tail := kept[head:]
		at := len(full) - len(tail)
		if tail == "" || strings.HasSuffix(full, tail) && at >= head && (at == 0 || full[at-1] == '\n') {
			return true
		}
		i := strings.IndexByte(tail, '\n')
		if i < 0 || !strings.HasPrefix(full, kept[:head+i+1]) {
			return false
		}
		head += i + 1
	}
}

// The windows must see the text the redactor sees: cleanLikeRedact is the
// redactor's own cleaning, and replacePrivateKeys writes its marker.
func TestRedactorCleaningAndKeyMarkerReplicas(t *testing.T) {
	for _, s := range []string{
		"plain text\nwith lines",
		"nul\x00byte and zero\u200bwidth and bidi\u202eoverride\u034f",
		"\x1b[1;31mbold red\x1b[0m and \x1b]0;window title\x07 and \x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\",
		"osc across\x1b]2;multi\nline\x07lines and a lone \x1b escape \u009b0m csi",
		"invalid \xff\xfe utf-8",
	} {
		want, n := redact.Redact(s)
		if n != 0 || cleanLikeRedact(s) != want {
			t.Fatalf("cleaning differs for %q: %q vs %q", s, cleanLikeRedact(s), want)
		}
	}
	pem := strings.Join([]string{"before\n-----BEGIN OPENSSH ", "PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH ", "PRIVATE KEY-----\nafter"}, "")
	want, _ := redact.Redact(pem)
	if got, n := replacePrivateKeys(pem); got != want || n != 1 {
		t.Fatalf("key marker differs: %q vs %q", got, want)
	}
}

func withRedactor(t *testing.T, fn func(string) (string, int)) {
	t.Helper()
	old := redactText
	redactText = fn
	t.Cleanup(func() { redactText = old })
}

// A row whose redaction outruns every pass is bounded more tightly,
// then recorded as a gap, and recovery moves on to the rows after it.
func TestSlowRowEscalatesToGap(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	withRedactor(t, func(s string) (string, int) {
		if len(s) > syncRedact {
			time.Sleep(400 * time.Millisecond)
		}
		return redact.Redact(s)
	})
	appendJSON(t, file, claudeRow(e, "user", "PROMPT-ONE", "turn-1"))
	appendJSON(t, file, claudeRow(e, "assistant", strings.Repeat("a slow reply line\n", 2500), "turn-1"))
	appendJSON(t, file, claudeRow(e, "user", "PROMPT-TWO", "turn-2"))
	var levels []int
	for pass := 0; pass < 10; pass++ {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err := Recover(ctx, dirs, e.Host, e.Session)
		cancel()
		s, _, _ := load(context.Background(), dirs, e.Host, e.Session)
		if s.Slow != nil {
			levels = append(levels, s.Slow.Level)
		}
		if err == nil {
			break
		}
		if !errors.Is(err, ErrPending) {
			t.Fatal(err)
		}
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 3 || rows[0].Text != "PROMPT-ONE" || rows[1].Text != gapTooLong || rows[2].Text != "PROMPT-TWO" {
		t.Fatalf("rows=%#v", rows)
	}
	// Normally [1 2 3]; a try that began late in its pass keeps its level.
	if got := fmt.Sprint(levels); !strings.HasSuffix(got, "1 2 3]") {
		t.Fatalf("levels=%v", levels)
	}
	if offMarker(t, dirs, e) {
		t.Fatal("slow row turned the chat off the record")
	}
}

// A redactor that fails on a row leaves a gap instead of stopping recovery at
// that row on every pass.
func TestRedactorPanicLeavesGap(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	withRedactor(t, func(s string) (string, int) {
		if strings.Contains(s, "POISON") {
			panic("redactor failed")
		}
		return redact.Redact(s)
	})
	appendJSON(t, file, claudeRow(e, "user", "POISON row", "turn-1"))
	appendJSON(t, file, claudeRow(e, "user", "next prompt", "turn-2"))
	if _, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil {
		t.Fatal(err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 2 || rows[0].Text != gapTooLong || rows[1].Text != "next prompt" {
		t.Fatalf("rows=%#v", rows)
	}
}

// A row whose turn is already stored is skipped before any redaction, in
// recovery and in the hook's duplicate check.
func TestStoredTurnSkipsRedaction(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	big := strings.Repeat("pasted code with a token and a key\n", 4000)
	prompt := e
	prompt.Role, prompt.Kind, prompt.Text, prompt.Turn, prompt.Origin = "user", "prompt", big, "turn-1", "hook"
	if err := (inbox.Store{Root: dirs.Inbox()}).Append(prompt); err != nil {
		t.Fatal(err)
	}
	calls := 0
	withRedactor(t, func(s string) (string, int) {
		calls++
		return redact.Redact(s)
	})
	appendJSON(t, file, claudeRow(e, "user", big, "turn-1"))
	if n, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil || n != 0 {
		t.Fatalf("recover=%d %v", n, err)
	}
	if ok, err := Contains(context.Background(), dirs, prompt); err != nil || !ok {
		t.Fatalf("contains=%v %v", ok, err)
	}
	if calls != 0 {
		t.Fatalf("redacted %d times", calls)
	}
}

// Recovery saves its cursor after each finished row when it runs out of
// time, so the next pass resumes after the rows it stored.
func TestPauseKeepsFinishedRows(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	release := make(chan struct{})
	withRedactor(t, func(s string) (string, int) {
		if strings.Contains(s, "SLOW") {
			<-release
		}
		return redact.Redact(s)
	})
	appendJSON(t, file, claudeRow(e, "user", "FIRST", "turn-1"))
	appendJSON(t, file, claudeRow(e, "assistant", "SLOW "+strings.Repeat("x", syncRedact), "turn-1"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, err := Recover(ctx, dirs, e.Host, e.Session)
	cancel()
	close(release)
	if !errors.Is(err, ErrPending) {
		t.Fatalf("err=%v", err)
	}
	s, _, _ := load(context.Background(), dirs, e.Host, e.Session)
	first, _ := json.Marshal(claudeRow(e, "user", "FIRST", "turn-1"))
	if s.Offset != int64(len(first)+1) || s.Slow == nil || s.Slow.Offset != s.Offset {
		t.Fatalf("cursor=%d slow=%#v", s.Offset, s.Slow)
	}
	if rows := readInbox(t, dirs, e); len(rows) != 1 || rows[0].Text != "FIRST" {
		t.Fatalf("rows=%#v", rows)
	}
	if _, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil {
		t.Fatal(err)
	}
	if rows := readInbox(t, dirs, e); len(rows) != 2 || !strings.HasPrefix(rows[1].Text, "SLOW") {
		t.Fatalf("rows=%#v", rows)
	}
}
