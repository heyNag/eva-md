package health

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/redact"
)

// The names the report counts the record by. Each is a word eva-md's own
// code writes; whatever else a file holds is counted as other, so no text
// read from a file reaches the report.
var (
	// entryKinds are the kinds of entry the hooks and transcript recovery
	// write.
	entryKinds = []string{"prompt", "reply", "tool", "plan", "event", "host_summary"}
	// markerKinds are the kinds of marker the redaction rules write, as in
	// [REDACTED:<kind>], current and baseline rules alike.
	markerKinds = []string{
		"anthropic_key", "auth_header", "aws_key", "digitalocean_token", "discord_webhook", "docker_token",
		"github_token", "gitlab_token", "google_key", "high_entropy", "huggingface_token", "jwt", "npm_token",
		"onepassword_token", "openai_key", "password", "private_key", "pypi_token", "secret_value",
		"sendgrid_key", "shopify_token", "slack_token", "slack_webhook", "stripe_key", "telegram_token",
		"url_credentials", "vault_token",
	}
	// editTools are the tools whose line names the files they changed.
	editTools = []string{"Edit", "Write", "NotebookEdit", "apply_patch"}
)

// other is the name the report counts a name it doesn't know under.
const other = "other"

// known is name when names holds it, and otherwise other.
func known(name string, names []string) string {
	if slices.Contains(names, name) {
		return name
	}
	return other
}

// RecordFigures is what the inbox holds, over every session file: sessions
// and bytes by host; entries by kind; lines passed over, as too long or not
// a well-formed entry of their file; the project folders and session files
// that couldn't be opened or read to the end, which leave the part
// incomplete; the redaction markers stored, by kind, and the replacements
// that left one (see Replacements); what the redaction rules of this
// release would catch in the stored text (see Catch); and the first and
// the last day an entry is dated.
//
// Replacements is, for each entry, the replacements it says were made, up
// to the complete markers its text holds: a figure the report can stand
// behind, whatever number a damaged or edited entry holds, since each
// replacement it counts left its marker in the stored text.
type RecordFigures struct {
	From         string         `json:"from"`
	To           string         `json:"to"`
	Sessions     map[string]int `json:"sessions"`
	Bytes        map[string]int `json:"bytes"`
	Entries      map[string]int `json:"entries"`
	PassedOver   int            `json:"passed_over"`
	Unread       int            `json:"unread"`
	Markers      map[string]int `json:"markers"`
	Replacements int            `json:"replacements"`
	RulesVersion int            `json:"rules_version"`
	Catch        Catch          `json:"would_catch"`
}

// Catch is what the redaction rules of this release would catch in the
// stored text, which is what an older release's rules missed: the
// replacements by marker kind and by tier, and the entries and sessions
// that hold one. It leaves out the entries cut at the size limit when they
// were stored, and counts them in Cut: a cut joins the head of a text to
// its tail, which can put a word that marks a secret beside a value the
// rules, reading the whole text, never saw beside it, so this release can
// catch more in what it stored itself. Text the rules redacted whole they
// catch nothing more in.
type Catch struct {
	Kinds    map[string]int `json:"kinds"`
	Tiers    map[string]int `json:"tiers"`
	Entries  int            `json:"entries"`
	Sessions int            `json:"sessions"`
	Cut      int            `json:"cut_entries"`
}

// toolUse is what the tool lines show of the wiki: runs of eva-md system
// write, edits of a file under the wiki, and edits whose lines name only
// relative paths, which can't be placed.
type toolUse struct {
	SystemWrites int `json:"system_write_runs"`
	Edits        int `json:"edits"`
	Unplaced     int `json:"edits_unplaced"`
}

// readRecord reads every session file of the inbox once, for the record's
// figures and, when wiki names the wiki's folders, the tool lines' use of
// it. It reads and writes no other file. complete is false when ctx ended
// first or a folder or file couldn't be read, and the counts are then a
// floor.
func readRecord(ctx context.Context, dir string, wiki []string, loc *time.Location) (r RecordFigures, tools toolUse, complete bool, err error) {
	r = RecordFigures{Sessions: map[string]int{}, Bytes: map[string]int{}, Entries: map[string]int{}, Markers: map[string]int{},
		RulesVersion: redact.Version, Catch: Catch{Kinds: map[string]int{}, Tiers: map[string]int{}}}
	for _, host := range updateHosts {
		r.Sessions[host], r.Bytes[host] = 0, 0
	}
	complete = true
	r.Unread, err = eachSessionFile(ctx, dir, func(project, name, host string, f *os.File, size int64) error {
		r.Sessions[host]++
		r.Bytes[host] += int(size)
		caught := false
		return eachLine(ctx, f, func(line []byte) {
			var e inbox.Entry
			if json.Unmarshal(line, &e) != nil || !inbox.Trusted(e, project, name, "") {
				r.PassedOver++
				return
			}
			r.Entries[known(e.Kind, entryKinds)]++
			if t, err := time.Parse(time.RFC3339, e.TS); err == nil {
				day := t.In(loc).Format(time.DateOnly)
				if r.From == "" || day < r.From {
					r.From = day
				}
				if day > r.To {
					r.To = day
				}
			}
			r.Replacements += min(max(e.Red, 0), countMarkers(r.Markers, e.Text))
			switch {
			case e.Text == "":
			case e.Trunc > 0:
				r.Catch.Cut++
			default:
				s := redact.Scan(ctx, e.Text)
				if s.Through != len(s.Clean) {
					complete = false
				}
				for _, found := range s.Findings {
					r.Catch.Kinds[known(found.Kind, markerKinds)]++
					r.Catch.Tiers[tierName(found.Tier)]++
				}
				if len(s.Findings) > 0 {
					r.Catch.Entries++
					if !caught {
						caught = true
						r.Catch.Sessions++
					}
				}
			}
			if e.Kind == "tool" && wiki != nil {
				tools.add(e.Text, wiki)
			}
		}, func() { r.PassedOver++ })
	})
	if ctx.Err() != nil {
		complete = false
		err = nil
	}
	if r.Unread > 0 {
		complete = false
	}
	return r, tools, complete, err
}

// tierName names a tier of the redaction rules.
func tierName(t redact.Tier) string {
	switch t {
	case redact.Certain:
		return "certain"
	case redact.Likely:
		return "likely"
	case redact.FloorOnly:
		return "floor_only"
	}
	return other
}

// countMarkers counts the complete markers text holds, [REDACTED:<kind>]
// with a kind of 1 to 32 lower-case letters and underscores, as the rules
// write them, by kind, and returns how many it counted.
func countMarkers(counts map[string]int, text string) int {
	const open = "[REDACTED:"
	n := 0
	for {
		i := strings.Index(text, open)
		if i < 0 {
			return n
		}
		text = text[i+len(open):]
		end := strings.IndexByte(text, ']')
		if end < 1 || end > 32 || strings.TrimLeft(text[:end], "abcdefghijklmnopqrstuvwxyz_") != "" {
			continue
		}
		counts[known(text[:end], markerKinds)]++
		n++
		text = text[end+1:]
	}
}

// add counts a tool line's use of the wiki, whose folders are wiki. A line
// is the tool's name, " (subagent)" for a subagent's, what the hook keeps of
// its input, as a command's first line or the files an edit names, and
// " (exit <code>)".
func (u *toolUse) add(text string, wiki []string) {
	name, rest, _ := strings.Cut(text, " ")
	rest = strings.TrimPrefix(rest, "(subagent) ")
	if i := strings.LastIndex(rest, " (exit "); i >= 0 && strings.HasSuffix(rest, ")") {
		rest = rest[:i]
	} else if strings.HasPrefix(rest, "(exit ") {
		rest = ""
	}
	if runsSystemWrite(rest) {
		u.SystemWrites++
	}
	if !slices.Contains(editTools, name) || rest == "" {
		return
	}
	files := []string{rest}
	if name == "apply_patch" {
		files = strings.Split(rest, ", ")
	}
	placed := false
	for _, file := range files {
		if !filepath.IsAbs(file) {
			continue
		}
		placed = true
		file = filepath.Clean(file)
		for _, w := range wiki {
			if file == w || strings.HasPrefix(file, w+string(filepath.Separator)) {
				u.Edits++
				return
			}
		}
	}
	if !placed {
		u.Unplaced++
	}
}

// runsSystemWrite reports whether a command runs eva-md system write: a
// word that is eva-md, or a path to it, followed by system and write.
func runsSystemWrite(command string) bool {
	words := strings.Fields(command)
	for i := 0; i+2 < len(words); i++ {
		if filepath.Base(words[i]) == "eva-md" && words[i+1] == "system" && words[i+2] == "write" {
			return true
		}
	}
	return false
}
