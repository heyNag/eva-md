package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/lint"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/searchindex"
	"github.com/heyNag/eva-md/internal/session"
)

// The sessions the inbox tests capture: two in a project, one in the
// general area.
const (
	inboxKey   = "github.com__example__acme"
	inboxOld   = "3f2a91c0-1111-2222-3333-444444444444" // claude-code, in the project
	inboxNew   = "019a2b3c-5555-6666-7777-888888888888" // codex, in the project
	inboxOther = "77aa88bb-9999-aaaa-bbbb-cccccccccccc" // codex, in the general area
)

// inboxSay captures one prompt of a session, as a hook would.
func inboxSay(t *testing.T, d paths.Dirs, project, host, sid, text string) {
	t.Helper()
	e := inbox.Entry{V: 1, TS: "2026-01-14T10:00:00+01:00", Host: host, Session: sid, Project: project, Branch: "main", Role: "user", Kind: "prompt", Text: text, Origin: "hook"}
	if err := (inbox.Store{Root: d.Inbox()}).Append(e); err != nil {
		t.Fatal(err)
	}
}

// inboxFixture captures the three sessions, the old one last written on
// 2026-01-14, the new one on 2026-03-01 and the general area's on
// 2026-01-10; gives the old one a watermark, a transcript recovery record
// and an off-the-record marker; builds the search index; and sets up a wiki
// whose thread page cites the old session three times. It returns the old
// session's file.
func inboxFixture(t *testing.T, d paths.Dirs) string {
	t.Helper()
	ctx := context.Background()
	home := os.Getenv("HOME")
	inboxSay(t, d, inboxKey, "claude-code", inboxOld, "the zanzibarquokka plan")
	inboxSay(t, d, inboxKey, "claude-code", inboxOld, "and its second step")
	inboxSay(t, d, inboxKey, "codex", inboxNew, "the newer export plan")
	inboxSay(t, d, "general", "codex", inboxOther, "a general question")
	store := inbox.Store{Root: d.Inbox()}
	old := store.Path(inboxKey, "claude-code", inboxOld)
	if err := store.SetMark(old, inbox.Mark{Offset: 10}); err != nil {
		t.Fatal(err)
	}
	for file, day := range map[string]time.Time{
		old:                                     time.Date(2026, 1, 14, 10, 0, 0, 0, time.Local),
		store.Path(inboxKey, "codex", inboxNew): time.Date(2026, 3, 1, 10, 0, 0, 0, time.Local),
		store.Path("general", "codex", inboxOther): time.Date(2026, 1, 10, 10, 0, 0, 0, time.Local),
	} {
		if err := os.Chtimes(file, day, day); err != nil {
			t.Fatal(err)
		}
	}
	transcript := filepath.Join(home, ".claude", "projects", "acme", inboxOld+".jsonl")
	cwd := filepath.Join(home, "code", "acme")
	for _, dir := range []string{filepath.Dir(transcript), cwd} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(transcript, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e := inbox.Entry{V: 1, Host: "claude-code", Session: inboxOld, Project: inboxKey, Branch: "main", Cwd: "code/acme"}
	if err := catchup.Register(ctx, d, home, transcript, cwd, e, false, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := (session.Markers{Root: d.Sessions()}).Set("claude-code", inboxOld, session.OffRecord, "asked"); err != nil {
		t.Fatal(err)
	}
	if _, err := searchindex.Catchup(ctx, d, "index"); err != nil {
		t.Fatal(err)
	}
	wiki := filepath.Join(home, "wiki")
	page := "---\ntype: thread\ntitle: \"acme · main\"\n---\n\n# acme · main\n\n## Now\n<!-- eva:now:start -->\nPlanning.\n\n### Next\n" +
		"- Write the plan [stated] s:3f2a91c0#1 <!-- i:n1 -->\n<!-- eva:now:end -->\n\n## Timeline\n### 2026-01-14\n" +
		"- decision [stated]: Keep the plan s:3f2a91c0#2\n- decision [stated]: Export first s:019a2b3c#1\n- done [stated]: Planned it s:" + inboxOld + "#2\n"
	for file, text := range map[string]string{
		filepath.Join(wiki, "index.md"):                               "# Wiki\n",
		filepath.Join(wiki, "projects", "acme", "threads", "main.md"): page,
		filepath.Join(wiki, "general", "threads", "default.md"):       "# General\n",
		filepath.Join(d.Config, "config.toml"):                        "wiki_path = " + jsonQuote(wiki) + "\n",
	} {
		if err := paths.EnsureDir(filepath.Dir(file)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return old
}

// inboxRun runs eva-md with args and returns its exit code and output.
func inboxRun(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// inboxExists reports whether a file or folder is there.
func inboxExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// eva-md inbox clear previews first: it lists the session a ref names, how
// many refs in the wiki's thread pages name it, and what goes with it, and
// changes nothing. With --apply it removes the session's file with its
// watermark, its transcript recovery record and its project's search index,
// records the removal, and keeps its off-the-record marker, its lock file,
// the project's other session and every other project. The lint then counts
// the refs to it as removed, not lost, and the index built again finds the
// other session's words and none of the removed one's.
func TestInboxClearPreviewsThenRemoves(t *testing.T) {
	d := isolated(t)
	old := inboxFixture(t, d)
	store := inbox.Store{Root: d.Inbox()}
	mark := filepath.Join(filepath.Dir(old), ".state", filepath.Base(old)+".wm")
	record := filepath.Join(d.State, "catchup", "claude-code", inboxOld+".json")
	index := filepath.Join(d.SearchIndex(), inboxKey)
	marker := filepath.Join(d.Sessions(), "claude-code", inboxOld, session.OffRecord)
	for _, path := range []string{old, mark, record, index, marker} {
		if !inboxExists(t, path) {
			t.Fatalf("the fixture lacks %s", path)
		}
	}
	code, out, errOut := inboxRun("inbox", "clear", "--session", "s:3f2a91c0#2")
	for _, want := range []string{
		"Preview; nothing was changed.\nWould remove 1 captured session, ",
		"  claude-code " + inboxOld + " in " + inboxKey + ", last written 2026-01-14 10:00, ",
		"\nIts watermark goes too, and its transcript recovery record unless the chat is off the record, and so does the search index of its project, until eva-md index or the daily maintenance builds it again.\n",
		"\n3 refs on 1 thread page name this chat; the doctor counts them as removed on purpose, not lost.\n",
		"\nThe wiki keeps what updates wrote from this chat, and so does its history; eva-md never rewrites either.\n",
		"\nA chat still open is captured again from its next turn.\nAdd --apply to remove it.\n",
	} {
		if code != 0 || errOut != "" || !strings.Contains(out, want) {
			t.Fatalf("preview %d: %q lacks %q; stderr %q", code, out, want, errOut)
		}
	}
	for _, path := range []string{old, mark, record, index} {
		if !inboxExists(t, path) {
			t.Fatalf("the preview removed %s", path)
		}
	}
	if inboxExists(t, filepath.Join(d.Inbox(), ".removed")) {
		t.Fatal("the preview recorded a removal")
	}
	code, out, errOut = inboxRun("inbox", "clear", "--session", "3f2a91c0", "--apply")
	if code != 0 || errOut != "" || !strings.Contains(out, "Removed 1 captured session, ") || !strings.Contains(out, "\nIts watermark went too, and its transcript recovery record unless the chat is off the record, and so did the search index of its project, ") || !strings.Contains(out, "\n3 refs on 1 thread page name this chat; ") || strings.Contains(out, "Add --apply") {
		t.Fatalf("apply %d: %q %q", code, out, errOut)
	}
	for _, path := range []string{old, mark, record, index} {
		if inboxExists(t, path) {
			t.Fatalf("%s stayed", path)
		}
	}
	for _, path := range []string{marker, store.Path(inboxKey, "codex", inboxNew), store.Path("general", "codex", inboxOther), filepath.Join(d.Locks(), "claude-code-"+inboxOld+".lock"), filepath.Join(d.SearchIndex(), "general")} {
		if !inboxExists(t, path) {
			t.Fatalf("%s went", path)
		}
	}
	removed, err := store.Removed()
	if err != nil || len(removed) != 1 || removed[0].Session != inboxOld || removed[0].Host != "claude-code" {
		t.Fatalf("record %+v %v", removed, err)
	}
	r, err := lint.Run(context.Background(), filepath.Join(os.Getenv("HOME"), "wiki"), d)
	if err != nil || r.BadRefs != 0 || r.RemovedRefs != 3 || r.ElsewhereRefs != 0 {
		t.Fatalf("lint %+v %v", r, err)
	}
	if code, out, _ := inboxRun("index"); code != 0 || !strings.Contains(out, "index: ") {
		t.Fatalf("index %d %q", code, out)
	}
	code, out, _ = inboxRun("search", "--all", "--inbox", "--days", "0", "zanzibarquokka")
	if code != 0 || !strings.Contains(out, "No matches.") {
		t.Fatalf("the removed session's words are found: %d %q", code, out)
	}
	code, out, _ = inboxRun("search", "--all", "--inbox", "--days", "0", "export")
	if code != 0 || !strings.Contains(out, "the newer export plan") {
		t.Fatalf("the kept session's words are lost: %d %q", code, out)
	}
	code, out, _ = inboxRun("inbox", "clear", "--session", inboxOld, "--apply")
	if code != 0 || out != "Nothing to remove: no captured session matches.\n" {
		t.Fatalf("a second removal: %d %q", code, out)
	}
}

// A chat off the record keeps its transcript recovery record when it is
// removed, as it keeps its markers: the record may be all that keeps the
// chat off the record, should the markers be lost (see catchup.Forget). Its
// file and watermark go.
func TestInboxClearKeepsTheRecordOfAChatOffTheRecord(t *testing.T) {
	d := isolated(t)
	old := inboxFixture(t, d)
	mark := filepath.Join(filepath.Dir(old), ".state", filepath.Base(old)+".wm")
	record := filepath.Join(d.State, "catchup", "claude-code", inboxOld+".json")
	if err := catchup.SetOffRecord(context.Background(), d, "claude-code", inboxOld, true); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := inboxRun("inbox", "clear", "--session", inboxOld, "--apply")
	if code != 0 || errOut != "" || !strings.Contains(out, "Removed 1 captured session, ") {
		t.Fatalf("apply %d: %q %q", code, out, errOut)
	}
	if inboxExists(t, old) || inboxExists(t, mark) {
		t.Fatal("the file or its watermark stayed")
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the record of a chat off the record went: %v", err)
	}
	var kept struct {
		Blocked bool   `json:"blocked"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(data, &kept); err != nil || !kept.Blocked || kept.Reason != "off_record" {
		t.Fatalf("the record no longer keeps the chat off the record: %+v %v", kept, err)
	}
}

// --project takes a project's captures, --before those last written before
// a day, and both together those that are both; a project folder left
// empty goes. A session whose file was deleted by hand leaves its
// watermark, which a removal takes and records, so the lint's refs to it
// count as removed and no longer as lost.
func TestInboxClearChoosesByProjectAndDay(t *testing.T) {
	d := isolated(t)
	old := inboxFixture(t, d)
	store := inbox.Store{Root: d.Inbox()}
	code, out, _ := inboxRun("inbox", "clear", "--before", "2026-01-12")
	if code != 0 || !strings.Contains(out, "Would remove 1 captured session") || !strings.Contains(out, inboxOther+" in general") || strings.Contains(out, inboxOld) {
		t.Fatalf("--before %d %q", code, out)
	}
	code, out, _ = inboxRun("inbox", "clear", "--project", inboxKey, "--before", "2026-01-15")
	if code != 0 || !strings.Contains(out, "Would remove 1 captured session") || !strings.Contains(out, inboxOld) || strings.Contains(out, inboxNew) || strings.Contains(out, inboxOther) {
		t.Fatalf("--project --before %d %q", code, out)
	}
	code, out, _ = inboxRun("inbox", "clear", "--project", inboxKey)
	if code != 0 || !strings.Contains(out, "Would remove 2 captured sessions") || !strings.Contains(out, inboxOld) || !strings.Contains(out, inboxNew) {
		t.Fatalf("--project %d %q", code, out)
	}
	// The old session's file deleted by hand: the lint counts its refs lost.
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	wiki := filepath.Join(os.Getenv("HOME"), "wiki")
	if r, err := lint.Run(context.Background(), wiki, d); err != nil || r.BadRefs != 3 {
		t.Fatalf("lint %+v %v", r, err)
	}
	code, out, _ = inboxRun("inbox", "clear", "--session", "3f2a91c0")
	if code != 0 || !strings.Contains(out, "  claude-code "+inboxOld+" in "+inboxKey+": its file is already gone, and only its watermark is left") {
		t.Fatalf("the watermark left %d %q", code, out)
	}
	if code, out, errOut := inboxRun("inbox", "clear", "--project", inboxKey, "--apply"); code != 0 || !strings.Contains(out, "Removed 2 captured sessions") {
		t.Fatalf("apply %d %q %q", code, out, errOut)
	}
	if inboxExists(t, filepath.Join(d.Inbox(), inboxKey)) {
		t.Fatal("the empty project folder stayed")
	}
	if !inboxExists(t, store.Path("general", "codex", inboxOther)) {
		t.Fatal("the general area's session went")
	}
	if r, err := lint.Run(context.Background(), wiki, d); err != nil || r.BadRefs != 0 || r.RemovedRefs != 4 {
		t.Fatalf("lint %+v %v", r, err)
	}
}

// Arguments it can't use are a usage error, a filter given an empty value
// beside another too, as a script's unset variable gives it, so the
// removal never widens to every project or day; nor does --before
// 0001-01-01 where that day is the zero time, as in UTC: it chooses
// nothing, alone or beside another filter. 8 characters two sessions share
// choose nothing and name both; nothing to remove is said and succeeds.
func TestInboxClearArguments(t *testing.T) {
	d := isolated(t)
	old := inboxFixture(t, d)
	for _, tc := range []struct {
		args []string
		has  string
	}{
		{[]string{"inbox"}, "inbox takes clear"},
		{[]string{"inbox", "list"}, "inbox takes clear"},
		{[]string{"inbox", "clear"}, "needs --session, --project or --before"},
		{[]string{"inbox", "clear", "--apply"}, "needs --session, --project or --before"},
		{[]string{"inbox", "clear", "--before", "2026-1-5"}, "--before must be a date"},
		{[]string{"inbox", "clear", "--project", "../acme"}, "--project takes a project key"},
		{[]string{"inbox", "clear", "--project", "", "--before", "2026-02-01", "--apply"}, "--project takes a project key"},
		{[]string{"inbox", "clear", "--session", "3f2a91c0", "--project=", "--apply"}, "--project takes a project key"},
		{[]string{"inbox", "clear", "--project", inboxKey, "--before", "", "--apply"}, "--before must be a date"},
		{[]string{"inbox", "clear", "--before", ""}, "--before must be a date"},
		{[]string{"inbox", "clear", "--session", "s:"}, "invalid arguments for inbox clear"},
		{[]string{"inbox", "clear", "--session", "a b"}, "invalid arguments for inbox clear"},
		{[]string{"inbox", "clear", "--session", "x", "extra"}, "invalid arguments for inbox clear"},
	} {
		if code, out, errOut := inboxRun(tc.args...); code != 2 || out != "" || !strings.Contains(errOut, tc.has) {
			t.Errorf("%v: %d %q %q", tc.args, code, out, errOut)
		}
	}
	twin := "3f2a91c0-dddd-eeee-ffff-000000000000"
	inboxSay(t, d, "general", "codex", twin, "a twin")
	code, out, errOut := inboxRun("inbox", "clear", "--session", "3f2a91c0", "--apply")
	if code != 1 || out != "" || !strings.Contains(errOut, "s:3f2a91c0 names 2 sessions, so nothing was chosen; give the full id of the one to remove: "+inboxOld+", "+twin) {
		t.Fatalf("a shared short id: %d %q %q", code, out, errOut)
	}
	if code, out, _ := inboxRun("inbox", "clear", "--session", "3f2a91c0", "--project", "general"); code != 0 || !strings.Contains(out, twin) || strings.Contains(out, inboxOld) {
		t.Fatalf("a short id one project tells apart: %d %q", code, out)
	}
	if code, out, _ := inboxRun("inbox", "clear", "--session", twin); code != 0 || !strings.Contains(out, "Would remove 1 captured session") {
		t.Fatalf("a full id: %d %q", code, out)
	}
	if code, out, _ := inboxRun("inbox", "clear", "--project", "nothing-here", "--apply"); code != 0 || out != "Nothing to remove: no captured session matches.\n" {
		t.Fatalf("nothing: %d %q", code, out)
	}
	// The day read in UTC, given to this call alone.
	if day, err := time.ParseInLocation(time.DateOnly, "0001-01-01", time.UTC); err != nil || !day.IsZero() {
		t.Fatalf("0001-01-01 in UTC is %v, %v; the test needs the zero time", day, err)
	}
	for _, args := range [][]string{
		{"inbox", "clear", "--before", "0001-01-01"},
		{"inbox", "clear", "--project", inboxKey, "--before", "0001-01-01", "--apply"},
		{"inbox", "clear", "--session", inboxOld, "--before", "0001-01-01", "--apply"},
	} {
		var out, errOut bytes.Buffer
		if code := inboxClear(context.Background(), args, &out, &errOut, time.UTC); code != 0 || out.String() != "Nothing to remove: no captured session matches.\n" || errOut.Len() != 0 {
			t.Errorf("%v in UTC: %d %q %q", args, code, out.String(), errOut.String())
		}
	}
	store := inbox.Store{Root: d.Inbox()}
	if !inboxExists(t, old) || !inboxExists(t, store.Path(inboxKey, "codex", inboxNew)) {
		t.Fatal("--before 0001-01-01 removed a capture written in 2026")
	}
	if inboxExists(t, filepath.Join(d.Inbox(), ".removed")) {
		t.Fatal("a refusal recorded a removal")
	}
	// A project folder that can't be read is named, and the rest listed.
	locked := filepath.Join(d.Inbox(), "github.com__example__locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	code, out, _ = inboxRun("inbox", "clear", "--before", "2026-01-12")
	if code != 0 || !strings.HasPrefix(out, "1 project folder of the inbox couldn't be read in full, so some of its chats may be left out: github.com__example__locked\nPreview; nothing was changed.\nWould remove 1 captured session") {
		t.Fatalf("an unreadable folder: %d %q", code, out)
	}
}

// The list names 20 captures, a file by its last write and size and a
// watermark alone as such, and counts the rest; the total counts them all.
func TestInboxListNamesTwentyAndCountsTheRest(t *testing.T) {
	var captures []inbox.Capture
	for i := 0; i < 22; i++ {
		captures = append(captures, inbox.Capture{Project: "acme", Host: "codex", ID: fmt.Sprintf("s%02d", i), Size: 1024, Mod: time.Date(2026, 1, 14, 10, 0, 0, 0, time.Local), File: true})
	}
	captures[0].File, captures[0].Size = false, 0
	var out bytes.Buffer
	inboxList(&out, "Would remove", captures)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 22 || lines[0] != "Would remove 22 captured sessions, 21.0 KB:" ||
		lines[1] != "  codex s00 in acme: its file is already gone, and only its watermark is left" ||
		lines[2] != "  codex s01 in acme, last written 2026-01-14 10:00, 1.0 KB" ||
		lines[20] != "  codex s19 in acme, last written 2026-01-14 10:00, 1.0 KB" || lines[21] != "  and 2 more" {
		t.Fatalf("%q", lines)
	}
}

// inboxRemoved is what inboxRemove returned.
type inboxRemoved struct {
	done []inbox.Capture
	err  error
}

// inboxAwaitLockWait returns once a goroutine running inboxRemove is parked
// in session.Lock's wait between tries of the lock, as the goroutines'
// stacks show: it took the lock the hooks take and found it held, since it
// parks there only after a try finds the lock busy. It fails the test if
// returned has the removal's result first, and if hangGuard passes first.
// The pause between looks only paces them.
func inboxAwaitLockWait(t *testing.T, returned <-chan inboxRemoved) {
	t.Helper()
	guard := time.After(hangGuard)
	for !inboxParkedInLock() {
		select {
		case r := <-returned:
			t.Fatalf("the removal returned while a hook held the session: %v %v", r.done, r.err)
		case <-guard:
			t.Fatalf("the removal was not seen waiting for the session's lock in %v", hangGuard)
		case <-time.After(time.Millisecond):
		}
	}
}

// inboxParkedInLock reports whether a goroutine running inboxRemove is
// parked in a select in session.Lock itself: session.Lock is its innermost
// frame outside the runtime.
func inboxParkedInLock() bool {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	for _, g := range strings.Split(string(buf), "\n\n") {
		header, frames, _ := strings.Cut(g, "\n")
		if !strings.Contains(header, " [select") || !strings.Contains(frames, "/cmd/eva-md.inboxRemove(") {
			continue
		}
		for _, line := range strings.Split(frames, "\n") {
			if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "runtime.") {
				continue
			}
			if strings.HasPrefix(line, "github.com/heyNag/eva-md/internal/session.Lock(") {
				return true
			}
			break
		}
	}
	return false
}

// While the background update runs, or another writer holds the search
// index of a project past the wait, nothing is removed or recorded. A
// session a hook still holds stops the removal there, keeping its file and
// those after it, unrecorded: the removal waits for the hook's own lock,
// trying it and finding it held, as the goroutines' stacks show, keeps the
// file while it waits and takes and records it once the hook lets go, and
// with its wait run out says to run it again. That holds also when the hook
// holds the session by an id the file's name had to change.
func TestInboxClearRemovesNothingWhileBusy(t *testing.T) {
	d := isolated(t)
	old := inboxFixture(t, d)
	store := inbox.Store{Root: d.Inbox()}
	unlock, busy, err := paths.TryLock(d.Locks(), "update.lock")
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	code, out, errOut := inboxRun("inbox", "clear", "--session", inboxOld, "--apply")
	unlock()
	if code != 1 || out != "" || !strings.Contains(errOut, "a background update is running, or the search index of these chats' projects is being built, so nothing was removed; try again shortly") {
		t.Fatalf("update busy: %d %q %q", code, out, errOut)
	}
	all, _, err := store.Captures()
	if err != nil {
		t.Fatal(err)
	}
	chosen, err := inboxSelection{sessions: []string{inboxOld}}.pick(all)
	if err != nil || len(chosen) != 1 {
		t.Fatal(chosen, err)
	}
	unlock, busy, err = paths.TryLock(filepath.Join(d.SearchIndex(), inboxKey), "lock")
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	done, err := inboxRemove(context.Background(), d, chosen, time.Now(), 0, time.Minute)
	unlock()
	if !errors.Is(err, errInboxBusy) || len(done) != 0 {
		t.Fatalf("index busy: %v %v", done, err)
	}
	if removed, err := store.Removed(); err != nil || len(removed) != 0 {
		t.Fatalf("a busy removal recorded %v %v", removed, err)
	}
	// A hook holds the session throughout: a removal whose wait runs out,
	// short here, stops at it, and one that waits is seen waiting for it,
	// keeps the file, and takes it once the hook lets go.
	held, err := session.Lock(context.Background(), d.Locks(), "claude-code", inboxOld)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(held) }
	t.Cleanup(release)
	done, err = inboxRemove(context.Background(), d, chosen, time.Now(), 0, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "a hook was still writing s:3f2a91c0, so it and the chats after it were left; run this again") || len(done) != 0 {
		t.Fatalf("session busy: %v %v", done, err)
	}
	if removed, err := store.Removed(); err != nil || len(removed) != 0 {
		t.Fatalf("a session a hook held was recorded as removed: %v %v", removed, err)
	}
	returned := make(chan inboxRemoved, 1)
	go func() {
		done, err := inboxRemove(context.Background(), d, chosen, time.Now(), 0, time.Minute)
		returned <- inboxRemoved{done, err}
	}()
	inboxAwaitLockWait(t, returned)
	if !inboxExists(t, old) {
		t.Fatal("a session a hook held went")
	}
	release()
	select {
	case r := <-returned:
		if r.err != nil || len(r.done) != 1 || inboxExists(t, old) {
			t.Fatalf("the session the hook let go: %v %v", r.done, r.err)
		}
	case <-time.After(hangGuard):
		t.Fatalf("the removal still waited %v after the hook let the session go", hangGuard)
	}
	if removed, err := store.Removed(); err != nil || len(removed) != 1 || removed[0].Session != inboxOld {
		t.Fatalf("the session removed once the hook let go was recorded as %v %v", removed, err)
	}
	// The hooks lock a session by the id they were given, which a file's
	// name holds changed when it has a character a name can't.
	inboxSay(t, d, "general", "codex", "odd:id", "a chat whose id has a colon")
	all, _, err = store.Captures()
	if err != nil {
		t.Fatal(err)
	}
	chosen, err = inboxSelection{project: "general", sessions: []string{"odd:id"}}.pick(all)
	if err != nil || len(chosen) != 1 || chosen[0].ID == "odd:id" {
		t.Fatal(chosen, err)
	}
	hooks, _ := paths.SafeName("codex-odd:id")
	named, _ := paths.SafeName("codex-" + chosen[0].ID)
	done, err = inboxRemove(context.Background(), d, chosen, time.Now(), 0, time.Minute)
	if err != nil || len(done) != 1 || !inboxExists(t, filepath.Join(d.Locks(), hooks+".lock")) || inboxExists(t, filepath.Join(d.Locks(), named+".lock")) {
		t.Fatalf("an odd id's session was not locked as the hooks lock it: %v %v", done, err)
	}
}

// A session is recorded as removed under its lock, once its transcript
// recovery record is dealt with, just before its files go, so a removal
// cut short leaves no session it kept recorded: one that fails at a
// session's recovery record, here one it can't remove, records and removes
// nothing of it, and one that stops at a session a hook holds records and
// removes the session before it, and neither that one nor its file.
func TestInboxClearRecordsOnlyTheChatsItRemoves(t *testing.T) {
	d := isolated(t)
	old := inboxFixture(t, d)
	store := inbox.Store{Root: d.Inbox()}
	all, _, err := store.Captures()
	if err != nil {
		t.Fatal(err)
	}
	chosen, err := inboxSelection{sessions: []string{inboxOld}}.pick(all)
	if err != nil || len(chosen) != 1 {
		t.Fatal(chosen, err)
	}
	records := filepath.Join(d.State, "catchup", "claude-code")
	if err := os.Chmod(records, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(records, 0o700) })
	done, err := inboxRemove(context.Background(), d, chosen, time.Now(), 0, time.Minute)
	if err == nil || len(done) != 0 || !inboxExists(t, old) {
		t.Fatalf("a recovery record it can't remove: %v %v", done, err)
	}
	if removed, err := store.Removed(); err != nil || len(removed) != 0 {
		t.Fatalf("a session the failure kept was recorded as removed: %v %v", removed, err)
	}
	if err := os.Chmod(records, 0o700); err != nil {
		t.Fatal(err)
	}
	// Two sessions, the second held by a hook throughout.
	chosen, err = inboxSelection{sessions: []string{inboxNew, inboxOther}}.pick(all)
	if err != nil || len(chosen) != 2 || chosen[0].ID != inboxOther || chosen[1].ID != inboxNew {
		t.Fatal(chosen, err)
	}
	held, err := session.Lock(context.Background(), d.Locks(), "codex", inboxNew)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(held)
	done, err = inboxRemove(context.Background(), d, chosen, time.Now(), 0, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "a hook was still writing s:019a2b3c, so it and the chats after it were left; run this again") || len(done) != 1 || done[0].ID != inboxOther {
		t.Fatalf("the removal stopped at the second session: %v %v", done, err)
	}
	if inboxExists(t, store.Path("general", "codex", inboxOther)) || !inboxExists(t, store.Path(inboxKey, "codex", inboxNew)) {
		t.Fatal("the first session's file stayed, or the held one's went")
	}
	if removed, err := store.Removed(); err != nil || len(removed) != 1 || removed[0].Session != inboxOther || removed[0].Host != "codex" {
		t.Fatalf("the record lists %+v %v; want the first session alone", removed, err)
	}
}
