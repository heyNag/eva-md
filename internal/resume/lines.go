package resume

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// pendingLine follows the previous chat in a brief while the update has yet
// to process part of that chat: the hash-triggered refresh delivers the Now
// it writes.
const pendingLine = "An update for the previous chat is pending; its Now arrives with a later prompt."

// header names the project, the branch, the worktree and the chat the brief
// continues, and says when an update for that chat is pending.
func header(o Options, chosen *history, lineage, pending bool) string {
	text := fmt.Sprintf("Project %s · branch %s", o.Project.Key, o.Project.Branch)
	if o.Project.Root != "" {
		text += "\nWorktree: " + o.Project.Root
	}
	if chosen == nil {
		return text
	}
	model := "unknown"
	for i := len(chosen.records) - 1; i >= 0; i-- {
		if chosen.records[i].Model != "" {
			model = oneLine(chosen.records[i].Model, 200)
			break
		}
	}
	text += fmt.Sprintf("\nPrevious chat: %s / %s · %s · %s", chosen.host, model, chosen.latest.Format(time.RFC3339), ending(*chosen))
	if lineage {
		text += "\nContinuing from " + oneLine(chosen.branch, 200)
	}
	if pending {
		text += "\n" + pendingLine
	}
	return text
}

// minPending is the smallest backlog, in bytes, that a pass of the update
// can take: a group needs 600 characters of chat (minChars in
// internal/update), and each character takes at least a byte. The rest of a
// group a pass stopped in needs only one entry.
const minPending = 600

// updatePending reports whether a brief says an update for the chosen chat
// is pending: a startup or clear brief whose chat is on this branch, with
// updates on and a wiki set up, and a backlog in its session file that a
// pass could take, 600 bytes or more or any rest of a group a pass stopped
// in, and that no update has yet found too small to process. A compact
// brief's chat is its own, and another branch's update never refreshes this
// branch's Now.
func updatePending(o Options, chosen *history, lineage bool) bool {
	if chosen == nil || lineage || o.Variant != "startup" && o.Variant != "clear" || !o.Config.Updates || o.Config.WikiPath == "" {
		return false
	}
	store := inbox.Store{Root: o.Dirs.Inbox()}
	m, err := store.Mark(filepath.Join(o.Dirs.Inbox(), safeProject(o.Project), chosen.file))
	if err != nil {
		return false
	}
	n := m.Pending(chosen.size)
	return n >= minPending || m.More && n > 0
}

// nowHeading says whose Now follows and how fresh it is. A borrowed Now
// names the branch it belongs to.
func nowHeading(pg page, stale, lineage bool, chosen *history) string {
	heading, notes := "Now", []string{}
	if lineage {
		heading += " from " + oneLine(chosen.branch, 200)
		notes = append(notes, "lineage")
	}
	if pg.asOf.IsZero() {
		notes = append(notes, "date unknown")
	} else {
		notes = append(notes, "as of "+pg.asOf.Format(time.RFC3339))
	}
	heading += " (" + strings.Join(notes, ", ") + ")"
	if stale && pg.asOf.IsZero() {
		heading += " — freshness unknown relative to the captured exchanges below"
	} else if stale {
		heading += " — stale: predates the captured exchanges below"
	}
	return heading
}

// openingLine shows a chat's first prompt on one line with its ref, so no
// line inside it can start a line of the brief.
func openingLine(r record) string {
	head := "Opening request (" + inbox.Ref(r.Session, r.line) + "): "
	return head + oneLine(r.Text, 200-runes(head))
}

// latestPlan is the chat's last plan entry when it is newer than the page.
func latestPlan(h history, asOf time.Time) string {
	var plan string
	for _, r := range h.records {
		ts, _ := time.Parse(time.RFC3339Nano, r.TS)
		if r.Kind == "plan" && ts.After(asOf) {
			plan = r.Text
		}
	}
	return plan
}

// earlier returns up to 3 other chats that did real work on this branch,
// newest first, leaving out the chosen chat and this session.
func earlier(all []history, o Options, chosen *history) []*history {
	var out []*history
	for i := range all {
		h := &all[i]
		if h == chosen || h.branch != o.Project.Branch || h.host == o.Host && h.session == o.Session || !useful(*h) {
			continue
		}
		if out = append(out, h); len(out) == 3 {
			break
		}
	}
	return out
}

// earlierChats lists those chats in about 80 characters each: the day each
// was last active, its host, and its first prompt with its ref. A chat whose
// first prompt was not found is left out.
func earlierChats(chats []*history, firsts []record) string {
	var lines []string
	for i, h := range chats {
		r := firsts[i]
		if r.line == 0 {
			continue
		}
		ref := inbox.Ref(r.Session, r.line)
		head := h.latest.Format("2006-01-02") + " " + h.host + " · "
		lines = append(lines, head+oneLine(r.Text, max(80-runes(head)-runes(ref)-3, 10))+" · "+ref)
	}
	if len(lines) == 0 {
		return ""
	}
	return "Earlier chats on this branch, newest first:\n" + strings.Join(lines, "\n")
}

// otherBranches lists up to 3 other branches with real work in the project's
// recent chats, newest first, with the day each was last active.
func otherBranches(all []history, branch string) string {
	seen := map[string]bool{branch: true}
	var lines []string
	for _, h := range all {
		if seen[h.branch] || !useful(h) {
			continue
		}
		seen[h.branch] = true
		if lines = append(lines, oneLine(h.branch, 45)+" · "+h.latest.Format("2006-01-02")); len(lines) == 3 {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Other branches, last active:\n" + strings.Join(lines, "\n")
}

// testCommands start the commands the tool activity line reports.
var testCommands = []string{"go test", "go build", "go vet", "npm test", "pnpm test", "yarn test", "pytest", "make", "cargo test", "swift test"}

// activity reports the chat's last test or build command with its exit code,
// and up to 5 distinct files its edit and write tools touched, newest first.
// It reads the stored tool lines (see hook.toolLine), so it is a heuristic: a
// test run after cd or through a script is not recognised.
func activity(h history, root string) string {
	var test string
	var files []string
	seen := map[string]bool{}
	for i := len(h.records) - 1; i >= 0 && (test == "" || len(files) < 5); i-- {
		if h.records[i].Kind != "tool" {
			continue
		}
		name, detail, exit, ok := toolCall(h.records[i].Text)
		switch {
		case !ok:
		case name == "Bash":
			if command := strings.Join(strings.Fields(detail), " "); test == "" && testCommand(command) {
				exit = " (exit " + oneLine(exit, 12) + ")"
				head := "Last test or build command: "
				test = head + oneLine(command, 150-runes(head)-runes(exit)) + exit
			}
		case name == "Edit" || name == "Write" || name == "NotebookEdit" || name == "apply_patch":
			list := []string{detail}
			if name == "apply_patch" {
				list = strings.Split(detail, ", ")
			}
			for _, file := range list {
				if file = shortPath(file, root); file != "" && !seen[file] && len(files) < 5 {
					seen[file] = true
					files = append(files, file)
				}
			}
		}
	}
	var lines []string
	if test != "" {
		lines = append(lines, test)
	}
	if len(files) > 0 {
		lines = append(lines, "Files edited, newest first: "+strings.Join(files, ", "))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Tool activity (heuristic):\n" + strings.Join(lines, "\n")
}

// toolCall splits a stored tool line, "<tool>[ (subagent)][ <detail>] (exit
// <code>)", into the tool's name, what it acted on (a command or paths) and
// its exit code.
func toolCall(text string) (name, detail, exit string, ok bool) {
	i := strings.LastIndex(text, " (exit ")
	if i < 0 || !strings.HasSuffix(text, ")") {
		return "", "", "", false
	}
	exit = text[i+len(" (exit ") : len(text)-1]
	name, detail, _ = strings.Cut(text[:i], " ")
	detail = strings.TrimPrefix(detail, "(subagent)")
	return strings.TrimSuffix(name, ":"), strings.TrimSpace(detail), exit, true
}

func testCommand(command string) bool {
	for _, c := range testCommands {
		if command == c || strings.HasPrefix(command, c+" ") {
			return true
		}
	}
	return false
}

// shortPath shows a file relative to the worktree when it is inside it, and
// keeps the end of a long one, where its name is.
func shortPath(file, root string) string {
	file = strings.Join(strings.Fields(file), " ")
	if root != "" {
		file = strings.TrimPrefix(file, strings.TrimSuffix(root, "/")+"/")
	}
	if r := []rune(file); len(r) > 60 {
		file = "…" + string(r[len(r)-59:])
	}
	return file
}

// footerCap bounds the footer, which a brief reserves before anything else.
const footerCap = 450

// footer tells the agent how to fetch more. eva-md is not on PATH, so it names
// the installed binary by its absolute path, once. With a thread page it
// names the page's #Now and newest Timeline groups for show. What passes
// footerCap goes first: the grep fallback, then the thread's id; the rest is
// cut.
func footer(o Options, pg page) string {
	bin := filepath.Join(o.Dirs.Share, "bin", "eva-md")
	protocol := func(show string) string {
		return "Earlier chats: run " + bin + " with: search <terms> when the answer depends on them (on a miss, try other words, file names or dates; stop after about 5); " + show + ". Check what may have changed against the code."
	}
	text := protocol("show <ref>")
	if withID := protocol("show <ref>, " + pg.id + "#Now or #Timeline --last 10"); pg.id != "" && runes(withID) <= footerCap {
		text = withID
	}
	if o.Config.WikiPath != "" {
		id := pg.id
		if id == "" {
			id = threadID(o.Project)
		}
		if fallback := "\nFallback: grep -ri <terms> " + filepath.Join(o.Config.WikiPath, filepath.Dir(filepath.FromSlash(id))); runes(text+fallback) <= footerCap {
			text += fallback
		}
	}
	return cut(text, footerCap)
}

// firstFooter is the whole brief of a chat with no earlier chat and no Now
// to resume: how to search the other projects.
func firstFooter(o Options) string {
	bin := filepath.Join(o.Dirs.Share, "bin", "eva-md")
	return "No earlier chats here; other projects: " + bin + " search --all <terms>, then " + bin + " show <ref>."
}
