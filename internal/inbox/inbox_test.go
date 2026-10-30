package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

func fixture(t *testing.T) Store {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return Store{Root: filepath.Join(home, "inbox")}
}

func entry() Entry {
	return Entry{Host: "codex", Session: "session-123", Project: "general", Role: "event", Kind: "event", Text: "hello", Origin: "hook"}
}

func TestConcurrentAppend(t *testing.T) {
	s := fixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				e := entry()
				e.Text = fmt.Sprintf("worker %d entry %d", worker, i)
				if err := s.Append(e); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	file := s.Path("general", "codex", "session-123")
	entries, offset, err := s.ReadFrom(file, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 8000 {
		t.Fatalf("got %d records", len(entries))
	}
	seen := make(map[string]bool)
	for _, e := range entries {
		if e.V != 1 || seen[e.Text] {
			t.Fatalf("corrupt or repeated record: %#v", e)
		}
		seen[e.Text] = true
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if offset != info.Size() {
		t.Fatalf("offset=%d size=%d", offset, info.Size())
	}
}

func TestRedactionTruncationAndFailClosed(t *testing.T) {
	s := fixture(t)
	e := entry()
	e.Role = "user"
	secret := "sk-" + strings.Repeat("aB4cD5", 8)
	e.Text = "token=" + secret
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	e.Text = strings.Repeat("界", maxText/3) + "MIDDLE" + strings.Repeat("終", maxText/3)
	wantRemoved := len(e.Text) - maxText
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	e.Text = "private text"
	if err := s.append(context.Background(), e, func(string) (string, int) { panic("redactor failed") }); err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.ReadFrom(s.Path(e.Project, e.Host, e.Session), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows", len(rows))
	}
	if strings.Contains(rows[0].Text, secret) || rows[0].Red == 0 {
		t.Fatal("secret not redacted")
	}
	if len(rows[1].Text) > maxText || !utf8.ValidString(rows[1].Text) || strings.Contains(rows[1].Text, "MIDDLE") || rows[1].Trunc < wantRemoved || rows[1].Trunc > int(wantRemoved+4) {
		t.Fatalf("bad truncation: len=%d trunc=%d", len(rows[1].Text), rows[1].Trunc)
	}
	if rows[2].Text != "" {
		t.Fatal("panic leaked text")
	}
}

func TestPartialLinesAndWatermark(t *testing.T) {
	s := fixture(t)
	e := entry()
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	file := s.Path(e.Project, e.Host, e.Session)
	rows, first, err := s.ReadFrom(file, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("read: rows=%d err=%v", len(rows), err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(e)
	split := len(data) / 2
	if _, err = f.Write(data[:split]); err != nil {
		t.Fatal(err)
	}
	rows, offset, err := s.ReadFrom(file, first)
	if err != nil || len(rows) != 0 || offset != first {
		t.Fatalf("partial read consumed bytes: %d %v", offset, err)
	}
	if _, err = f.Write(append(data[split:], '\n')); err != nil {
		t.Fatal(err)
	}
	f.Close()
	rows, offset, err = s.ReadFrom(file, first)
	if err != nil || len(rows) != 1 || offset != first+int64(len(data)+1) {
		t.Fatalf("completed line: %d %v", offset, err)
	}
	if wm, err := s.Watermark(file); err != nil || wm != 0 {
		t.Fatalf("missing watermark: %d %v", wm, err)
	}
	if err := s.SetWatermark(file, offset); err != nil {
		t.Fatal(err)
	}
	if wm, err := s.Watermark(file); err != nil || wm != offset {
		t.Fatalf("watermark: %d %v", wm, err)
	}
	for _, bad := range []int64{-1, 1, offset + 1} {
		if _, _, err := s.ReadFrom(file, bad); err == nil {
			t.Fatalf("accepted bad offset %d", bad)
		}
	}
	if err := s.SetWatermark(file, -1); err == nil {
		t.Fatal("accepted negative watermark")
	}
}

func TestPermissionsAndRefs(t *testing.T) {
	s := fixture(t)
	e := entry()
	e.Role = "assistant"
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	file := s.Path(e.Project, e.Host, e.Session)
	if err := s.SetWatermark(file, 0); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{s.Root: 0o700, filepath.Dir(file): 0o700, file: 0o600, filepath.Join(filepath.Dir(file), ".state"): 0o700, filepath.Join(filepath.Dir(file), ".state", filepath.Base(file)+".wm"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode=%o want=%o", path, info.Mode().Perm(), mode)
		}
	}
	for _, tc := range []struct {
		s    string
		line int
		want string
	}{{"abcdefghijk", 42, "s:abcdefgh#42"}, {"abc", 1, "s:abc#1"}, {"一二三四五六七八九", 2, "s:一二三四五六七八#2"}} {
		if got := Ref(tc.s, tc.line); got != tc.want {
			t.Errorf("Ref=%q want=%q", got, tc.want)
		}
	}
}

func TestLockDeadline(t *testing.T) {
	s := fixture(t)
	e := entry()
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(s.Path(e.Project, e.Host, e.Session), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = s.AppendContext(ctx, e)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 250*time.Millisecond {
		t.Fatalf("lock did not honor deadline: %v %v", err, time.Since(started))
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(e); err != nil {
		t.Fatalf("lock retained: %v", err)
	}
}

func TestTraversalAndSymlinks(t *testing.T) {
	s := fixture(t)
	e := entry()
	e.Project = "../../escape"
	e.Session = "../../../session"
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	file := s.Path(e.Project, e.Host, e.Session)
	if rel, err := filepath.Rel(s.Root, file); err != nil || !filepath.IsLocal(rel) {
		t.Fatal("path escaped")
	}
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	e = entry()
	file = s.Path(e.Project, e.Host, e.Session)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, file); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(e); err == nil {
		t.Fatal("append followed symlink")
	}
	if _, _, err := s.ReadFrom(file, 0); err == nil {
		t.Fatal("read followed symlink")
	}
	if err := s.SetWatermark(outside, 1); err == nil {
		t.Fatal("watermark escaped")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "unchanged" {
		t.Fatal("outside changed")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(e); err == nil {
		t.Fatal("append accepted FIFO")
	}
	if _, _, err := s.ReadFrom(file, 0); err == nil {
		t.Fatal("read accepted FIFO")
	}
}

func TestMetadataRedactionAndInvalidLines(t *testing.T) {
	s := fixture(t)
	e := entry()
	secret := "ghp_" + strings.Repeat("ab12", 10)
	e.Model = "token=" + secret
	e.Branch = "token=" + secret
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	file := s.Path(e.Project, e.Host, e.Session)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatal("metadata leaked")
	}
	e.Session = secret
	if err := s.Append(e); err == nil {
		t.Fatal("secret-bearing filename accepted")
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("not json\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	rows, offset, err := s.ReadFrom(file, 0)
	if err != nil || len(rows) != 2 || rows[1].Text != "gap: malformed inbox record" || offset != int64(len(data)+len("not json\n")) {
		t.Fatalf("invalid line handling: %d %d %v", len(rows), offset, err)
	}
}

func TestInterruptedAppendRecovery(t *testing.T) {
	s := fixture(t)
	e := entry()
	e.Role = "user"
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	file := s.Path(e.Project, e.Host, e.Session)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"v":1,"text":"interrupted`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	e.Text = "after interruption"
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatal("recovery rewrote the inbox")
	}
	rows, offset, err := s.ReadFrom(file, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[1].Text != "gap: malformed inbox record" || rows[2].Text != "gap: interrupted append" || rows[3].Text != e.Text || offset != int64(len(after)) {
		t.Fatalf("recovery rows=%#v offset=%d", rows, offset)
	}
}

func TestSeparateProcessAppend(t *testing.T) {
	if root := os.Getenv("EVA_MD_TEST_INBOX_ROOT"); root != "" {
		s := Store{Root: root}
		e := entry()
		for i := 0; i < 100; i++ {
			e.Text = fmt.Sprintf("process %s record %d", os.Getenv("EVA_MD_TEST_WRITER"), i)
			if err := s.Append(e); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	s := fixture(t)
	t.Setenv("EVA_MD_TEST_INBOX_ROOT", s.Root)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.CommandContext(ctx, bin, "-test.run=^TestSeparateProcessAppend$")
			cmd.Env = append(os.Environ(), fmt.Sprintf("EVA_MD_TEST_WRITER=%d", i))
			if output, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("child writer: %w: %s", err, output)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	rows, _, err := s.ReadFrom(s.Path("general", "codex", "session-123"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 400 {
		t.Fatalf("cross-process records=%d", len(rows))
	}
	seen := make(map[string]bool)
	for _, row := range rows {
		if seen[row.Text] || row.Role != "event" {
			t.Fatalf("invalid cross-process record=%#v", row)
		}
		seen[row.Text] = true
	}
}
