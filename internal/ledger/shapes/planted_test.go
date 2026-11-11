package shapes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
)

// Words in scripts other than Latin, planted as a key, a row type and a
// value.
var nonLatin = []string{"зкмаркер", "ズクマーカー", "زقمارکر"}

// longKey is a key of 41 characters.
var longKey = marker + strings.Repeat("k", 37)

// codexIDs are the Codex chats' ids, UUIDs as Codex names its files.
func codexIDs(n int) []string {
	var ids []string
	for i := range n {
		ids = append(ids, fmt.Sprintf("1a2b3c4d-0000-4000-8000-%012d", i+1))
	}
	return ids
}

// plantedHome writes six chats of each host with a made-up marker in every
// place a value or a key can sit, and on each host one chat with five
// copies, five forks and, on Claude Code, six subagents' files; files
// with other names beside them, and one that recovery's opener refuses.
func plantedHome(f *fixture) {
	t := f.t
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("%s-claude-%d", marker, i)
		entry := any(fmt.Sprintf("%s-entry-%d", marker, i))
		if i == 3 {
			entry = nonLatin[2]
		}
		if i == 4 {
			entry = map[string]any{marker + "-entrykey": marker + "-v"}
		}
		version := "2.1.5"
		switch i {
		case 1:
			version = "2.1.5-" + marker
		case 2:
			version = "２.１.５"
		}
		user := map[string]any{"type": "user", "sessionId": id, "promptId": marker + "-turn", "timestamp": at(0, i), "cwd": f.cwd,
			branchField: marker + "-branch", "version": version, "entrypoint": entry, "turnOrigin": marker + "-origin", "userType": marker + "-usertype",
			"uuid": marker + "-uuid", "parentUuid": marker + "-parent", marker + "-rowkey": marker + "-v", longKey: 1, nonLatin[0]: nonLatin[0],
			"origin": map[string]any{"kind": marker + "-kind", marker + "-originkey": 1},
			"message": map[string]any{"role": "user", marker + "-msgkey": marker + "-v", "content": []any{
				map[string]any{"type": "text", "text": marker + " typed text", marker + "-itemkey": marker + "-v"},
				map[string]any{"type": marker + "-itemtype", "source": map[string]any{marker + "-sourcekey": marker}},
			}}}
		rows := []any{
			user,
			map[string]any{"type": marker + "-rowtype", "sessionId": id, "timestamp": at(1, i), marker + "-k": marker},
			map[string]any{"type": "system", "subtype": marker + "-subtype", "level": marker + "-level", "sessionId": id, "timestamp": at(2, i),
				"content": marker + " system text"},
			map[string]any{"type": "assistant", "sessionId": id, "promptId": marker + "-turn", "timestamp": at(3, i), "cwd": f.cwd, "requestId": marker + "-req",
				"message": map[string]any{"role": "assistant", "model": marker + "-model", "id": marker + "-msgid", "stop_reason": marker + "-stop",
					"usage": map[string]any{marker + "-usagekey": 1}, "content": []any{
						map[string]any{"type": "tool_use", "id": marker + "-call", "name": marker + "-tool",
							"input": map[string]any{marker + "-inputkey": map[string]any{"type": marker + "-deep", "file_path": "/" + marker + "/path.go"}}}}}},
			map[string]any{"type": "user", "sessionId": id, "promptId": marker + "-turn", "timestamp": at(4, i), "cwd": f.cwd,
				"toolUseResult": map[string]any{marker + "-resultkey": marker + "-resultvalue", "stdout": marker + " output"},
				"message": map[string]any{"role": "user", "content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": marker + "-call", "is_error": false,
						"content": []any{map[string]any{"type": marker + "-resulttype", marker + "-resultitemkey": 1}}}}}},
		}
		if i == 1 {
			rows = append(rows, map[string]any{"type": nonLatin[1], "sessionId": id, "timestamp": at(5, 0)})
		}
		path := f.claudeFile("", id, rows...)
		f.session(claude, id, path)
	}
	// One chat with five copies, five forks and six subagents' files.
	many := marker + "-claude-many"
	manyRows := []any{cUser(f, many, "p1", at(10, 0), marker+" prompt"), cReply(f, many, "p1", at(10, 1))}
	manyRows[0].(map[string]any)["entrypoint"] = "cli"
	path := f.claudeFile("", many, manyRows...)
	f.session(claude, many, path)
	for i := 1; i <= 5; i++ {
		f.claudeFile(fmt.Sprintf("-%s-copy-%d", marker, i), many, manyRows...)
		fork := fmt.Sprintf("%s-fork-%d", marker, i)
		fp := f.claudeFile("", fork, append(slices.Clone(manyRows), cUser(f, fork, "p2", at(11, i), marker+" fork prompt"))...)
		f.session(claude, fork, fp)
	}
	for i := 1; i <= 6; i++ {
		agent := fmt.Sprintf("agent-%s-%d", marker, i)
		row := cUser(f, many, "p1", at(12, i), marker+" subagent task")
		row["isSidechain"], row["agentId"] = true, agent
		f.subagentFile(many, agent, row)
	}
	// Files beside the chats' that are not session files, and one the
	// opener refuses: a file with two links.
	f.write(filepath.Join(f.real, ".claude", "projects", "-"+marker+"-cwd", marker+"-claude-1.txt"), `{"type":"`+marker+`-txt"}`)
	f.write(filepath.Join(f.real, ".claude", "projects", "-"+marker+"-cwd", marker+"-claude-1"), `{"type":"`+marker+`-noext"}`)
	linked := marker + "-claude-linked"
	lp := f.claudeFile("", linked, cUser(f, linked, "p1", at(0, 0), marker))
	if err := os.Link(lp, filepath.Join(f.real, marker+"-second-link.jsonl")); err != nil {
		t.Fatal(err)
	}
	f.session(claude, linked, lp)
	// An inbox file of a host eva-md does not know.
	stray := inbox.Entry{V: 1, TS: at(0, 0), Host: marker + "-host", Session: marker + "-stray", Role: "user", Kind: "prompt",
		Text: marker, Project: marker + "-project", Cwd: marker + "-cwd", Origin: "hook"}
	if err := (inbox.Store{Root: f.dirs.Inbox()}).Append(stray); err != nil {
		t.Fatal(err)
	}

	ids := codexIDs(17)
	for i := 1; i <= 6; i++ {
		id := ids[i-1]
		source := any(fmt.Sprintf("%s-source-%d", marker, i))
		if i == 2 {
			source = map[string]any{marker + "-subagent": map[string]any{marker + "-parent": marker + "-v"}}
		}
		version := "0.46.0"
		switch i {
		case 1:
			version = "0.46.0-" + marker
		case 2:
			version = "０.４６.０"
		}
		path := f.codexFile(2, id,
			xMeta(f, id, map[string]any{"source": source, "originator": marker + "-originator", "cli_version": version,
				marker + "-pkey": marker + "-v", "instructions": marker + " instructions", "git": map[string]any{"branch": marker + "-branch"}}),
			map[string]any{"timestamp": at(0, 1), "type": "turn_context", "payload": map[string]any{"turn_id": marker + "-turn", "cwd": f.cwd,
				"model": marker + "-model", "effort": marker + "-effort"}, marker + "-rowkey": 1},
			xRow(at(0, 2), "event_msg", map[string]any{"type": "user_message", "message": marker + " prompt", "images": []any{marker}}),
			xRow(at(0, 3), marker+"-xrowtype", map[string]any{"type": marker + "-ptype", marker: marker}),
			xRow(at(0, 4), "response_item", map[string]any{"type": marker + "-ptype", "role": marker + "-role"}),
			xRow(at(0, 5), "response_item", map[string]any{"type": "function_call", "name": marker + "-tool", "call_id": marker + "-call",
				"arguments": `{"` + marker + `-argkey":"` + marker + `"}`, "output": map[string]any{marker + "-outkey": 1}}),
			xRow(at(0, 6), "event_msg", map[string]any{"type": "agent_message", "message": marker + " reply", "phase": marker + "-phase"}),
		)
		f.session(codex, id, path)
	}
	// One chat with five copies and five forks.
	many = ids[6]
	meta := xMeta(f, many, map[string]any{"source": "cli"})
	manyRows = []any{meta, xRow(at(0, 2), "event_msg", map[string]any{"type": "user_message", "message": marker})}
	path = f.codexFile(2, many, manyRows...)
	f.session(codex, many, path)
	for i := 1; i <= 5; i++ {
		f.codexFile(2+i, many, manyRows...)
		fork := ids[6+i]
		fp := f.codexFile(2, fork, append([]any{xMeta(f, fork, map[string]any{"source": "cli"})}, manyRows...)...)
		f.session(codex, fork, fp)
	}
}

// leaked finds in out any marker, word in another script, Codex id or
// long key the planted home holds.
func leaked(out string) []string {
	var found []string
	if strings.Contains(strings.ToLower(out), marker) {
		found = append(found, marker)
	}
	for _, w := range append(append(slices.Clone(nonLatin), codexIDs(17)...), longKey, "２", "０") {
		if strings.Contains(out, w) {
			found = append(found, w)
		}
	}
	return found
}

// strings is every string, key or value, of a JSON document.
func jsonStrings(t *testing.T, out string) []string {
	t.Helper()
	var all []string
	d := json.NewDecoder(strings.NewReader(out))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return all
		}
		if err != nil {
			t.Fatal(err)
		}
		if s, ok := tok.(string); ok {
			all = append(all, s)
		}
	}
}

// A home with a made-up marker in every place a value or a key can sit,
// on both hosts, gives a report that holds none of them: not a one-word
// value, an object in a value's place, a version with a suffix or in
// full-width digits, a key at any level, a long key, a row type, payload
// type or subtype in six chats, words in other scripts, an id, nor any
// file's name. Every byte of it is printable ASCII or a line end, and
// every string one of the package's own words, a day, a version of three
// numbers or eva-md's version.
func TestThePlantedMarkersReachNoOutput(t *testing.T) {
	words := Words() // before the report is made, so it cannot add to them
	for _, linked := range links {
		f := newFixture(t, linked)
		plantedHome(f)
		r, out := f.run()
		if found := leaked(out); len(found) > 0 {
			t.Errorf("%s: the report holds %q:\n%s", linked, found, out)
		}
		for i, b := range []byte(out) {
			if b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Fatalf("byte %d of the report is %#x", i, b)
			}
		}
		version := regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`)
		day := regexp.MustCompile(`^2026-11-[0-9]{2}$`)
		for _, s := range jsonStrings(t, out) {
			if _, ok := slices.BinarySearch(words, s); !ok && !version.MatchString(s) && !day.MatchString(s) && s != "2026.11.9" {
				t.Errorf("the report prints %q, which no list holds", s)
			}
		}
		// What was planted is counted under other, by name, value or
		// version, with how many distinct ones and in how many chats.
		for path, want := range map[string]int{
			"F-085/claude-code/row_types/other_names": 2, "F-085/claude-code/row_types/chats/other": 6,
			"F-085/claude-code/subtypes/chats/other": 6, "F-085/claude-code/subtypes/other_names": 1,
			"F-084/claude-code/fields/user/chats/other": 6, "F-084/claude-code/fields/user/other_names": 3,
			"F-084/claude-code/fields/user/rows/uuid": 6, "F-084/claude-code/fields/user/rows/userType": 6,
			"F-084/claude-code/fields/other/other_names":   1,
			"F-084/claude-code/message_fields/other_names": 2, "F-084/claude-code/item_types/other_names": 1,
			"F-084/claude-code/item_fields/text/other_names": 1, "F-084/claude-code/item_fields/other/rows/source": 0,
			"F-084/claude-code/item_fields/other/other_names": 1,
			"F-084/claude-code/versions/chats/2.1.5":          4, "F-084/claude-code/versions/chats/other": 2,
			"F-079/claude-code/entrypoint/rows/other": 6, "F-079/claude-code/entrypoint/other_names": 6,
			"F-018/claude-code/turn_origin/rows/other": 6, "F-018/claude-code/turn_origin/other_names": 1,
			"F-132/codex/row_types/chats/other": 6, "F-132/codex/row_types/other_names": 1,
			"F-132/codex/payload_types/chats/other": 6, "F-132/codex/payload_types/other_names": 1,
			"F-132/codex/fields/chats/other": 6, "F-132/codex/fields/other_names": 1,
			"F-132/codex/versions/chats/0.46.0": 4, "F-132/codex/versions/chats/other": 2,
			"F-018/codex/source/rows/other": 6, "F-018/codex/source/other_names": 6, "F-018/codex/source/chats/cli": 1,
			"F-018/codex/originator/chats/other": 6, "F-018/codex/originator/other_names": 1,
		} {
			if got := get(r.Facts, path); got != want {
				t.Errorf("%s: %s is %d, want %d", linked, path, got, want)
			}
		}
		if got := get(r.Hosts, "claude-code/files/refused"); got != 1 {
			t.Errorf("%s: the file with two links: %d refused", linked, got)
		}
	}
}

// A chat is counted once wherever a figure is in how many chats: its
// copies, its forks and its subagents' files count with it, on both
// hosts, though each file is read and its rows counted.
func TestAChatIsCountedOnce(t *testing.T) {
	f := newFixture(t, noLink)
	plantedHome(f)
	r, _ := f.run()
	for path, want := range map[string]int{
		"claude-code/chats/read": 7, "claude-code/chats/files_per_chat/over 10": 1, "claude-code/chats/files_per_chat/1": 6,
		"claude-code/files/read": 6 + 1 + 5 + 5 + 6, "claude-code/files/subagent_files": 6, "claude-code/files/refused": 1,
		"claude-code/files/not_read_whole": 0,
		"codex/chats/read":                 7, "codex/chats/files_per_chat/over 10": 1, "codex/chats/files_per_chat/1": 6,
		"codex/files/read": 6 + 1 + 5 + 5,
	} {
		if got := get(r.Hosts, path); got != want {
			t.Errorf("%s is %d, want %d", path, got, want)
		}
	}
	for path, want := range map[string]int{
		"F-079/claude-code/entrypoint/chats/cli": 1, "F-079/claude-code/entrypoint/rows/cli": 1 + 5 + 5,
		"F-085/claude-code/row_types/chats/user": 7, "F-085/claude-code/row_types/chats/assistant": 7,
		"F-027/claude-code/subagent_files/read": 6, "F-027/claude-code/subagent_files/of_another_chat": 6,
		"F-027/claude-code/sidechain": 6, "F-027/claude-code/agent_id": 6,
		"F-018/codex/source/chats/cli": 1, "F-018/codex/source/rows/cli": 1 + 5 + 5 + 5,
		"F-132/codex/row_types/chats/session_meta": 7,
	} {
		if got := get(r.Facts, path); got != want {
			t.Errorf("%s is %d, want %d", path, got, want)
		}
	}
}

// Write refuses a report that would print a string no list holds, or a
// byte that is not printable ASCII, and prints nothing then.
func TestWriteRefusesWhatNoListHolds(t *testing.T) {
	for _, bad := range []map[string]any{
		{marker: Count(4)},
		{"F-001": map[string]any{"codex": map[string]any{marker: Count(4)}}},
		{"F-001": map[string]any{nonLatin[0]: Count(4)}},
		{"F-001": map[string]any{"２.１.５": Count(4)}},
	} {
		r := Report{Format: Format, Version: "2026.11.9", Day: "2026-11-11", Facts: bad}
		var out bytes.Buffer
		if err := r.Write(&out); !errors.Is(err, ErrUnlisted) || out.Len() != 0 {
			t.Errorf("%v: %v, printed %q", bad, err, out.String())
		}
	}
	// eva-md's own version is printed as it is, so only the byte check
	// stands between a version that is not ASCII and the output.
	for _, version := range []string{"2026.11.9" + nonLatin[0], "2026.11.9é", "２０２６.11.9"} {
		r := Report{Format: Format, Version: version, Day: "2026-11-11"}
		var out bytes.Buffer
		if err := r.Write(&out); !errors.Is(err, ErrUnlisted) || out.Len() != 0 {
			t.Errorf("version %q: %v, printed %q", version, err, out.String())
		}
	}
	r := Report{Format: Format, Version: "2026.11.9", Day: "2026-11-11", Facts: map[string]any{"F-001": map[string]any{"codex": map[string]any{"rows": Count(4)}}},
		Days: map[string][2]string{codex: {"2026-11-01", "2026-11-02"}}}
	var out bytes.Buffer
	if err := r.Write(&out); err != nil {
		t.Fatal(err)
	}
}
