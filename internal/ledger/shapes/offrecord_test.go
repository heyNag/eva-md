package shapes

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/session"
)

// special is a session that adds to no figure but its own count: said off
// the record, by one side's record or the other's, or run in a folder the
// hooks leave out. build writes it into a home; inside picks one of two
// contents that differ only inside it. why is the count it adds to.
type special struct {
	name, host, why string
	build           func(f *fixture, inside int)
	files           int // the session's files the hosts list
	subagents       int // its subagents' files, read and counted under why
}

// inside are two contents of a special session's host file: in the first,
// rows of every kind with planted names, one naming the other chat's
// session, and a row too long to read; in the second, a single row.
func insideRows(f *fixture, host, id string, inside int) []any {
	if inside == 1 {
		if host == codex {
			return []any{xMeta(f, id, nil)}
		}
		return []any{cUser(f, id, "p9", at(30, 0), "one row")}
	}
	if host == codex {
		return []any{
			xMeta(f, id, map[string]any{"source": "exec", "cli_version": "9.9.9", marker + "-pkey": 1}),
			xMeta(f, "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", nil),
			xRow(at(31, 0), marker+"-rowtype", map[string]any{"type": marker + "-ptype"}),
			xRow(at(31, 1), "event_msg", map[string]any{"type": "task_started", "turn_id": "t1"}),
			xRow(at(31, 2), "event_msg", map[string]any{"type": "user_message", "message": "<realtime_delegation> " + strings.Repeat("x", 300)}),
			xRow(at(31, 3), "compacted", map[string]any{"message": strings.Repeat("y", 70000)}),
		}
	}
	return []any{
		map[string]any{"type": marker + "-rowtype", "sessionId": "c1", "timestamp": at(31, 0), "version": "9.9.9", "entrypoint": "sdk-py", marker: 1},
		cUser(f, id, "p1", at(31, 1), strings.Repeat("z", 300)),
		cReply(f, id, "p1", at(31, 2)),
		map[string]any{"type": "system", "subtype": "x", "timestamp": at(29, 0)},
		"not json " + marker,
	}
}

// specials are the sessions that add only to their own count: off the
// record by eva-md's inbox, by its marker alone, or by the host's record
// alone, in a row read whole or one too long to read; and in a folder the
// hooks leave out, by eva-md's records or by the host's alone.
func specials() []special {
	control := inbox.Entry{TS: at(30, 1), Role: "event", Kind: "event", Text: "off_record"}
	prompt := func(inside int) inbox.Entry {
		return inbox.Entry{TS: at(30, 0), Turn: "p1", Role: "user", Kind: "prompt", Text: fmt.Sprintf("%s %d", marker, inside)}
	}
	tmp := func(f *fixture) string {
		d := filepath.Join(f.home, "tmp", marker+"-scratch")
		if err := os.MkdirAll(filepath.Join(f.real, "tmp", marker+"-scratch"), 0o700); err != nil {
			f.t.Fatal(err)
		}
		return d
	}
	return []special{
		{"the inbox says it", claude, statusOff, func(f *fixture, inside int) {
			id := marker + "-off-inbox"
			path := f.claudeFile("", id, insideRows(f, claude, id, inside)...)
			f.session(claude, id, path, prompt(inside), control, prompt(inside+2))
		}, 1, 0},
		{"a marker says it", claude, statusOff, func(f *fixture, inside int) {
			id := marker + "-off-marker"
			path := f.claudeFile("", id, insideRows(f, claude, id, inside)...)
			f.session(claude, id, path, prompt(inside))
			f.mark(claude, id, session.WasOff)
		}, 1, 0},
		{"the host's record says it", claude, statusOff, func(f *fixture, inside int) {
			id := marker + "-off-host"
			rows := append(insideRows(f, claude, id, inside), cUser(f, id, "p8", at(32, 0), "Off the record."))
			path := f.claudeFile("", id, rows...)
			f.session(claude, id, path, prompt(inside))
		}, 1, 0},
		{"a row's own content says it", claude, statusOff, func(f *fixture, inside int) {
			id := marker + "-off-queued"
			rows := append(insideRows(f, claude, id, inside), map[string]any{"type": marker + "-queued", "sessionId": id, "timestamp": at(32, 0), "content": "off the record"})
			path := f.claudeFile("", id, rows...)
			f.session(claude, id, path, prompt(inside))
		}, 1, 0},
		{"a row too long says it", claude, statusOff, func(f *fixture, inside int) {
			f.rowLimit = 4096
			id := marker + "-off-long"
			rows := append(insideRows(f, claude, id, inside), cUser(f, id, "p8", at(32, 0), "off the record\n"+strings.Repeat("w", 5000)))
			path := f.claudeFile("", id, rows...)
			f.session(claude, id, path, prompt(inside))
		}, 1, 0},
		{"the inbox says it", codex, statusOff, func(f *fixture, inside int) {
			id := codexIDs(20)[19]
			path := f.codexFile(3, id, insideRows(f, codex, id, inside)...)
			f.session(codex, id, path, prompt(inside), control)
		}, 1, 0},
		{"the host's record says it", codex, statusOff, func(f *fixture, inside int) {
			id := codexIDs(20)[18]
			rows := append(insideRows(f, codex, id, inside), xRow(at(33, 0), "event_msg", map[string]any{"type": "user_message", "message": "off the record"}))
			path := f.codexFile(3, id, rows...)
			f.session(codex, id, path, prompt(inside))
		}, 1, 0},
		{"eva-md's records say it", claude, statusLeftOut, func(f *fixture, inside int) {
			id := marker + "-away-inbox"
			dir := tmp(f)
			rows := insideRows(f, claude, id, inside)
			path := f.claudeFile("", id, rows...)
			e := prompt(inside)
			rel, err := filepath.Rel(f.home, dir)
			if err != nil {
				f.t.Fatal(err)
			}
			e.Cwd = rel
			f.session(claude, id, path, e)
		}, 1, 0},
		{"the host's record says it", codex, statusLeftOut, func(f *fixture, inside int) {
			id := codexIDs(20)[17]
			rows := append(insideRows(f, codex, id, inside), xRow(at(33, 0), "turn_context", map[string]any{"turn_id": "t2", "cwd": tmp(f)}))
			path := f.codexFile(3, id, rows...)
			f.session(codex, id, path, prompt(inside))
		}, 1, 0},
		{"a line the inbox withholds says it", claude, statusOff, func(f *fixture, inside int) {
			// A pause of another project's, which v1's reader does not
			// trust and never returns as a pause, then a prompt it
			// withholds as said off the record.
			id := marker + "-off-withheld"
			path := f.claudeFile("", id, insideRows(f, claude, id, inside)...)
			f.session(claude, id, path, prompt(inside))
			pause := control
			pause.Project = marker + "-elsewhere"
			f.appendInbox(claude, id, pause, prompt(inside+2))
		}, 1, 0},
		{"its recovery record says it", claude, statusOff, func(f *fixture, inside int) {
			id := marker + "-off-record"
			path := f.claudeFile("", id, insideRows(f, claude, id, inside)...)
			f.session(claude, id, path, prompt(inside))
			f.block(claude, id)
		}, 1, 0},
		{"the off_record marker says it", claude, statusOff, func(f *fixture, inside int) {
			id := marker + "-off-marker-now"
			path := f.claudeFile("", id, insideRows(f, claude, id, inside)...)
			f.session(claude, id, path, prompt(inside))
			f.mark(claude, id, session.OffRecord)
		}, 1, 0},
		{"a row too long says it of a session an earlier row named", claude, statusOff, func(f *fixture, inside int) {
			f.rowLimit = 4096
			id, named := marker+"-off-long-named", marker+"-named"
			rows := append(insideRows(f, claude, id, inside), cUser(f, named, "p7", at(31, 5), "one row"),
				cUser(f, named, "p8", at(32, 0), "off the record\n"+strings.Repeat("w", 5000)))
			path := f.claudeFile("", id, rows...)
			f.session(claude, id, path, prompt(inside))
		}, 1, 0},
		{"the host's record says it, with subagents' files", claude, statusOff, func(f *fixture, inside int) {
			// Its subagents' files differ in number, and one names the
			// other chat.
			id := marker + "-off-subagents"
			rows := append(insideRows(f, claude, id, inside), cUser(f, id, "p8", at(32, 0), "Off the record."))
			path := f.claudeFile("", id, rows...)
			f.session(claude, id, path, prompt(inside))
			for i := range 4 - 3*inside {
				named := id
				if inside == 0 && i == 0 {
					named = "c1"
				}
				row := cUser(f, named, "p1", at(34, i), "a subagent's prompt")
				row["isSidechain"], row["agentId"] = true, marker
				f.subagentFile(id, fmt.Sprintf("agent-%s-%d", marker, i), row)
			}
		}, 1, 0},
		{"a subagent's file says it", claude, statusOff, func(f *fixture, inside int) {
			// The session's own file is a single clean row, so nothing
			// puts it out before its subagents' files are read; one of
			// them is said off the record, and names the other chat or
			// not.
			id := marker + "-off-subagent"
			path := f.claudeFile("", id, cUser(f, id, "p9", at(30, 0), "one row"))
			f.session(claude, id, path, prompt(inside))
			control := cUser(f, id, "p8", at(32, 0), "Off the record.")
			control["isSidechain"], control["agentId"] = true, marker
			rows := []any{control}
			if inside == 0 {
				rows = append(insideRows(f, claude, id, inside), control)
			}
			f.subagentFile(id, "agent-"+marker+"-off", rows...)
		}, 1, 1},
		{"the host's record says it, in one of two copies", claude, statusOff, func(f *fixture, inside int) {
			// The file in the chats' folder is said off the record; its
			// copy in another folder names the other chat, or itself.
			id := marker + "-off-copy"
			rows := append(insideRows(f, claude, id, inside), cUser(f, id, "p8", at(32, 0), "Off the record."))
			path := f.claudeFile("", id, rows...)
			named := id
			if inside == 0 {
				named = "c1"
			}
			f.claudeFile("-"+marker+"-elsewhere", id, cUser(f, id, "p1", at(31, 0), "one row"), cUser(f, named, "p1", at(31, 1), "two rows"))
			f.session(claude, id, path, prompt(inside))
		}, 2, 0},
		{"the host's record says it, in the copy listed last", claude, statusOff, func(f *fixture, inside int) {
			// The file in the chats' folder names the other chat, or not;
			// its copy in a folder listed after it is said off the record.
			id := marker + "-off-copy-last"
			path := f.claudeFile("", id, insideRows(f, claude, id, inside)...)
			f.claudeFile("-"+marker+"-elsewhere", id, cUser(f, id, "p1", at(31, 0), "Off the record."))
			f.session(claude, id, path, prompt(inside))
		}, 2, 0},
		{"a row of the host's says it", claude, statusLeftOut, func(f *fixture, inside int) {
			// eva-md's records give the chats' folder; one of the host's
			// rows gives a folder the hooks leave out.
			id := marker + "-away-host"
			row := cUser(f, id, "p8", at(32, 0), "one row")
			row["cwd"] = tmp(f)
			path := f.claudeFile("", id, append(insideRows(f, claude, id, inside), row)...)
			f.session(claude, id, path, prompt(inside))
		}, 1, 0},
		{"a row is not JSON", claude, statusNotWhole, func(f *fixture, inside int) {
			id := marker + "-not-json"
			path := f.claudeFile("", id, append(insideRows(f, claude, id, inside), "{"+marker)...)
			f.session(claude, id, path, prompt(inside))
		}, 1, 0},
		{"a row too long to follow", claude, statusNotWhole, func(f *fixture, inside int) {
			f.rowLimit = 4096
			id := marker + "-long-not-json"
			path := f.claudeFile("", id, append(insideRows(f, claude, id, inside), strings.Repeat(marker, 2000))...)
			f.session(claude, id, path, prompt(inside))
		}, 1, 0},
	}
}

// A session said off the record at any time, by either side's record, or
// run in a folder the hooks leave out, adds to no figure but its own
// count: two homes that differ only inside it print the same report, whose
// facts are those of the same home without it, and whose hosts' part
// differs from that home's only by the session's file and chat, counted
// under why it was left out. Nothing it names joins it to another chat.
// It is so with a link where macOS or a person may put one.
func TestASessionLeftOutAddsOnlyItsCount(t *testing.T) {
	for _, link := range links {
		t.Run(link, func(t *testing.T) { aSessionLeftOutAddsOnlyItsCount(t, link) })
	}
}

func aSessionLeftOutAddsOnlyItsCount(t *testing.T, link string) {
	base := newFixture(t, link)
	oneChatEach(base)
	rb, _ := base.run()
	for _, sp := range specials() {
		t.Run(sp.host+" "+sp.why+" "+sp.name, func(t *testing.T) {
			var outs [2]string
			for inside := range 2 {
				f := newFixture(t, link)
				oneChatEach(f)
				sp.build(f, inside)
				r, out := f.run()
				outs[inside] = out
				if got, want := leaves(r.Facts), leaves(rb.Facts); !maps.Equal(got, want) {
					t.Errorf("inside %d: the facts differ from the home's without it:\n%v\n%v", inside, got, want)
				}
				want := leaves(rb.Hosts)
				want[sp.host+"/files/listed"] += sp.files
				want[sp.host+"/files/"+sp.why] += sp.files + sp.subagents
				if sp.subagents > 0 {
					want[sp.host+"/files/subagent_files"] += sp.subagents
				}
				want[sp.host+"/chats/"+sp.why]++
				if got := leaves(r.Hosts); !maps.Equal(got, want) {
					t.Errorf("inside %d: hosts\n%v\nwant\n%v", inside, got, want)
				}
			}
			if outs[0] != outs[1] {
				t.Errorf("what was inside the session changed the report:\n%s\n%s", outs[0], outs[1])
			}
		})
	}
}

// No subagents' file of a session put out by its host's own rows is
// opened, while that of a session left in is: the folder of a session put
// out is not listed.
func TestNoSubagentFileOfASessionLeftOutIsOpened(t *testing.T) {
	f := newFixture(t, noLink)
	oneChatEach(f)
	for _, sp := range specials() {
		if strings.HasSuffix(sp.name, "subagents' files") {
			sp.build(f, 0)
		}
	}
	in := marker + "-in"
	path := f.claudeFile("", in, cUser(f, in, "p1", at(40, 0), "one row"))
	f.session(claude, in, path, inbox.Entry{TS: at(40, 0), Turn: "p1", Role: "user", Kind: "prompt", Text: marker})
	row := cUser(f, in, "p1", at(40, 1), "a subagent's prompt")
	row["isSidechain"] = true
	kept := f.subagentFile(in, "agent-"+marker+"-in", row)

	opened := spyOpens(t, nil)
	r, _ := f.run()
	var subagents []string
	for _, p := range *opened {
		if strings.Contains(p, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
			subagents = append(subagents, p)
		}
	}
	if !slices.Equal(subagents, []string{kept}) {
		t.Errorf("opened subagents' files %q, want only %q", subagents, kept)
	}
	if get(r.Hosts, "claude-code/files/subagent_files") != 1 || get(r.Hosts, "claude-code/files/off_the_record") != 1 {
		t.Errorf("hosts %v", leaves(r.Hosts))
	}
}

// spyOpens records the path of each file the report asks recovery's
// opener to open.
func spyOpens(t *testing.T, also func()) *[]string {
	t.Helper()
	var opened []string
	saved := open
	t.Cleanup(func() { open = saved })
	open = func(s catchup.Source) (*os.File, error) {
		opened = append(opened, s.Path)
		if also != nil {
			also()
		}
		return saved(s)
	}
	return &opened
}

// A file named as a session's outside the hosts' folders of chats, one in
// a folder linked out of them, one that is itself a link, and a subagents'
// folder linked out of them, are never opened; nor is the file of a
// session the inbox does not hold. A Codex file that says it is another
// session's is opened by recovery's opener and refused.
func TestOnlyAdmittedFilesAreOpened(t *testing.T) {
	f := newFixture(t, noLink)
	oneChatEach(f)
	outside := filepath.Join(f.real, marker+"-outside")
	planted := []any{cUser(f, marker+"-x", "p1", at(0, 0), marker)}
	var never []string
	put := func(path string, rows ...any) {
		never = append(never, f.write(path, rows...))
	}
	// The right name outside the folders of chats.
	id := marker + "-elsewhere"
	put(filepath.Join(f.real, ".claude", id+".jsonl"), planted...)
	put(filepath.Join(f.real, ".claude", "projects", id+".jsonl"), planted...)
	put(filepath.Join(f.real, id+".jsonl"), planted...)
	f.session(claude, id, "")
	// A project folder linked out of the folder of chats.
	linkdir := marker + "-linkdir"
	put(filepath.Join(outside, "dir", linkdir+".jsonl"), planted...)
	if err := os.Symlink(filepath.Join(outside, "dir"), filepath.Join(f.real, ".claude", "projects", "-"+marker+"-linked")); err != nil {
		t.Fatal(err)
	}
	f.session(claude, linkdir, "")
	// A session file that is a link out.
	linkfile := marker + "-linkfile"
	put(filepath.Join(outside, linkfile+".jsonl"), planted...)
	if err := os.Symlink(filepath.Join(outside, linkfile+".jsonl"), filepath.Join(f.real, ".claude", "projects", "-"+marker+"-cwd", linkfile+".jsonl")); err != nil {
		t.Fatal(err)
	}
	f.session(claude, linkfile, "")
	// A subagents' folder linked out.
	put(filepath.Join(outside, "subagents", "agent-1.jsonl"), planted...)
	if err := os.MkdirAll(filepath.Join(f.real, ".claude", "projects", "-"+marker+"-cwd", "c1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "subagents"), filepath.Join(f.real, ".claude", "projects", "-"+marker+"-cwd", "c1", "subagents")); err != nil {
		t.Fatal(err)
	}
	// A file of a session the inbox does not hold.
	never = append(never, f.claudeFile("", marker+"-unknown", planted...))
	// A Codex file that names another session.
	ids := codexIDs(3)
	mismatch := f.codexFile(4, ids[0], xMeta(f, ids[1], nil))
	f.session(codex, ids[0], "")

	opened := spyOpens(t, nil)
	r, _ := f.run()
	want := []string{
		filepath.Join(f.real, ".claude", "projects", "-"+marker+"-cwd", "c1.jsonl"),
		filepath.Join(f.real, ".codex", "sessions", "2026", "11", "02", "rollout-2026-11-02T10-00-00-0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d.jsonl"),
		mismatch,
	}
	slices.Sort(want)
	got := slices.Sorted(slices.Values(*opened))
	if !slices.Equal(got, want) {
		t.Errorf("opened\n%q\nwant\n%q", got, want)
	}
	for _, p := range never {
		if slices.Contains(*opened, p) {
			t.Errorf("opened %s", p)
		}
	}
	for path, n := range map[string]int{
		"claude-code/files/read": 1, "claude-code/files/refused": 1, "claude-code/files/not_known": 1, "claude-code/files/listed": 2,
		"codex/files/read": 1, "codex/files/not_identified": 1, "codex/files/refused": 0, "codex/files/listed": 2,
	} {
		if got := get(r.Hosts, path); got != n {
			t.Errorf("%s is %d, want %d", path, got, n)
		}
	}
}

// When time runs out the report is still made: the files not read by then
// are counted as not reached, nothing of them is counted, and the hosts'
// parts say they are incomplete.
func TestAReportCutShortSaysSo(t *testing.T) {
	f := newFixture(t, noLink)
	oneChatEach(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := spyOpens(t, cancel)
	r, _ := f.runWith(ctx)
	if len(*opened) != 1 {
		t.Errorf("opened %q", *opened)
	}
	if r.Parts[claude] != PartIncomplete || r.Parts[codex] != PartIncomplete {
		t.Errorf("parts %v", r.Parts)
	}
	if get(r.Hosts, "claude-code/files/not_reached") != 1 || get(r.Hosts, "codex/files/not_reached") != 1 || get(r.Hosts, "claude-code/chats/read") != 0 {
		t.Errorf("hosts %v", leaves(r.Hosts))
	}
	for p := range leaves(r.Facts) {
		t.Errorf("counted %s", p)
	}
}
