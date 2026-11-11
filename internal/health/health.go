// Package health reports observable local state without model calls. Its
// two repairs write the names index of the system pages again (see
// inspectNames) and build the files made from the knowledge pages again
// (see inspectKnowledge).
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/systempage"
	"github.com/heyNag/eva-md/internal/wikigit"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}
type Report struct {
	Checks []Check `json:"checks"`
}

func (r Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == "FAIL" {
			return true
		}
	}
	return false
}
func (r *Report) add(name, status, detail, fix string) {
	r.Checks = append(r.Checks, Check{name, status, detail, fix})
}

// QuickNotice is bounded, local-only and suitable for a hook. It never reads
// host settings or emits file contents in a notice.
func QuickNotice(ctx context.Context, d paths.Dirs, c config.Config, activeHost ...string) string {
	if ctx.Err() != nil {
		return ""
	}
	// A misspelled key can be a missing exclusion.
	if len(c.Unknown) > 0 {
		return "eva.md: config has unknown keys, which are ignored; run eva-md doctor."
	}
	if c.WikiPath != "" {
		if _, err := readPrivate(filepath.Join(c.WikiPath, "index.md"), 1<<20); err != nil {
			return "eva.md: wiki index is unavailable; run eva-md doctor."
		}
		s, err := config.LoadSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"))
		if err == nil && s.SchemaVersion > 1 {
			return "eva.md: wiki schema is newer; capture continues, wiki writes are disabled."
		}
		if err == nil && c.Instance != "" && s.Instance != c.Instance {
			return "eva.md: wiki instance differs from this Mac; run eva-md doctor."
		}
	}
	now := time.Now()
	if info, err := os.Lstat(filepath.Join(d.State, "needs-trust.json")); err == nil {
		flag, e := readPrivate(filepath.Join(d.State, "needs-trust.json"), 16384)
		var pending struct {
			Host       string
			NeedsTrust bool
		}
		// Use explicit names because the persisted field has an underscore.
		var fields map[string]json.RawMessage
		if !info.Mode().IsRegular() || e != nil || json.Unmarshal(flag, &fields) != nil {
			return "eva.md: hook trust status is unavailable; run eva-md doctor."
		}
		_ = json.Unmarshal(fields["host"], &pending.Host)
		_ = json.Unmarshal(fields["needs_trust"], &pending.NeedsTrust)
		if pending.Host != "codex" || !pending.NeedsTrust {
			return "eva.md: hook trust status is unavailable; run eva-md doctor."
		}
		// A Codex hook that is running now has been trusted. Its own
		// heartbeat is written only after this notice is built.
		running := len(activeHost) > 0 && activeHost[0] == "codex"
		b, valid := readBeat(d, "codex", "SessionStart", now)
		if !running && (!valid || !b.At.After(info.ModTime())) {
			return "eva.md: review its hooks in Codex /hooks; run eva-md doctor to verify."
		}
	}
	if len(activeHost) > 0 {
		other := "codex"
		if activeHost[0] == "codex" {
			other = "claude-code"
		}
		check := captureFreshness(ctx, d, other, now)
		if check.Status == "FAIL" {
			return "eva.md: recent " + other + " activity has no matching capture heartbeat; run eva-md doctor."
		}
	}
	for _, host := range []string{"claude-code", "codex"} {
		if failed, runs := hookRuns(d, host, now); hooksFailing(failed, runs) {
			return fmt.Sprintf("eva.md: %d of the last %d %s hook runs failed; run eva-md doctor.", failed, runs, host)
		}
	}
	for _, host := range []string{"claude-code", "codex"} {
		if _, found, _ := catchup.ReadDrift(d, host); found {
			return "eva.md: transcript recovery no longer recognizes " + host + "'s transcripts, so missed replies are not recovered; run eva-md doctor."
		}
	}
	return backgroundNotice(d, c, now)
}

type beat struct {
	At                  time.Time `json:"at"`
	Host                string    `json:"host"`
	Event               string    `json:"event"`
	TranscriptAvailable bool      `json:"transcript_available"`
	// TranscriptElsewhere says the last run's transcript lies outside the
	// folder transcript recovery reads (see hostBeats).
	TranscriptElsewhere bool      `json:"transcript_elsewhere"`
	Runs                []hookRun `json:"runs"` // the event's last runs, oldest first
	// Entrypoint is the Claude Code entrypoint the last run inherited, and
	// Headless whether the hook took it for a program's chat.
	Entrypoint string `json:"entrypoint"`
	Headless   bool   `json:"headless"`
	// Week is the event's runs of the last WeekDays days (see AddRun).
	Week []HookDay `json:"week"`
}

// hookRun is one run in a heartbeat's history, with the time it took, which
// runs from before durations were recorded lack (see hookTimes).
type hookRun struct {
	At         time.Time `json:"at"`
	Failed     bool      `json:"failed"`
	DurationMS float64   `json:"duration_ms"`
}

// Inspect checks disk state and optionally queries native hosts, as eva-md
// doctor does. It never authenticates, starts a model, changes trust or
// writes host settings; its two repairs are the names index (see
// inspectNames) and the files built from the knowledge pages (see
// inspectKnowledge).
func Inspect(ctx context.Context, d paths.Dirs, hosts bool) Report {
	return inspect(ctx, d, hosts, true)
}

// inspect is Inspect, and without repair, as eva-md status, writes nothing.
// The rows come from the registered producers, in the order of their places
// (see registerRows).
func inspect(ctx context.Context, d paths.Dirs, hosts, repair bool) Report {
	return rows(&inspection{ctx: ctx, d: d, hosts: hosts, repair: repair})
}

func init() {
	registerRows(100, "binary", func(r *Report, in *inspection) { inspectBinary(r, in.d) })
	registerRows(200, "config", inspectConfig)
	registerRows(300, "wiki", func(r *Report, in *inspection) {
		if c, err := in.config(); err == nil {
			inspectWiki(in.ctx, r, in.d, c, in.repair)
		}
	})
	registerRows(400, "inbox", func(r *Report, in *inspection) { inspectInbox(in.ctx, r, in.d) })
}

// inspectBinary adds the row of the stable binary the hooks run.
func inspectBinary(r *Report, d paths.Dirs) {
	if info, err := os.Lstat(filepath.Join(d.Share, "bin", "eva-md")); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
		r.add("binary", "PASS", "stable hook binary is installed", "")
	} else {
		r.add("binary", "UNKNOWN", "stable hook binary is not installed", "run eva-md install")
	}
}

// inspectConfig adds the rows of the config file: that it parses, its
// unknown keys and its mode.
func inspectConfig(r *Report, in *inspection) {
	c, err := in.config()
	if err != nil {
		r.add("config", "FAIL", "configuration is missing or invalid", "run eva-md setup, or repair the eva-md config")
		return
	}
	r.add("config", "PASS", "configuration parses", "")
	if len(c.Unknown) > 0 {
		r.add("config keys", "FAIL", "unknown keys are ignored: "+strings.Join(c.Unknown, ", "), "correct or remove them in the eva-md config")
	}
	if info, e := os.Lstat(in.d.ConfigFile()); e != nil || info.Mode().Perm()&0077 != 0 {
		r.add("config permissions", "FAIL", "config must be private", "set config mode to 0600")
	}
}

func inspectWiki(ctx context.Context, r *Report, d paths.Dirs, c config.Config, repair bool) {
	if c.WikiPath == "" {
		r.add("wiki", "FAIL", "wiki path is missing", "run eva-md setup")
		return
	}
	root, err := paths.OpenRoot(c.WikiPath, false)
	if err != nil {
		r.add("wiki location", "FAIL", "wiki path is unavailable or unsafe", "restore the configured private wiki folder")
		return
	}
	info, err := root.Stat(".")
	root.Close()
	r.add("wiki location", "PASS", "wiki is a real, accessible directory", "")
	// A new wiki is 0700; one adopted from a clone made by hand keeps git's
	// 0755, which lets every account on this Mac read the pages.
	switch {
	case err != nil:
		r.add("wiki permissions", "UNKNOWN", "the wiki folder's mode can't be read", "check the wiki folder")
	case info.Mode().Perm()&0o077 != 0:
		r.add("wiki permissions", "FAIL", fmt.Sprintf("the wiki folder is %04o, so other accounts on this Mac can read it", info.Mode().Perm()), "run chmod 700 "+shellArg(c.WikiPath))
	default:
		r.add("wiki permissions", "PASS", "only your account can open the wiki folder", "")
	}
	s, err := config.LoadSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"))
	// eva-md writes the names index only to a wiki of its schema and the
	// config's instance.
	writable := err == nil && s.SchemaVersion == 1 && c.Instance != "" && s.Instance == c.Instance
	if err != nil {
		r.add("wiki schema", "FAIL", "schema is missing or invalid", "restore .eva-md/schema.toml")
	} else {
		status, fix := "PASS", ""
		if s.SchemaVersion > 1 {
			status, fix = "FAIL", "upgrade eva-md before writing this wiki"
		}
		r.add("wiki schema", status, fmt.Sprintf("schema %d", s.SchemaVersion), fix)
		if c.Instance == "" || s.Instance != c.Instance {
			r.add("wiki instance", "FAIL", "instance does not match this Mac", "select the intended wiki and config")
		} else {
			r.add("wiki instance", "PASS", "instance matches", "")
		}
	}
	p, e := identity.Resolve(ctx, c.WikiPath)
	gitInfo, gitErr := os.Lstat(filepath.Join(c.WikiPath, ".git"))
	// init saves the remote's identity, such as github.com/me/wiki or the
	// file:// URL of a local remote; a transport URL names the same repo.
	// The origin and its push URLs are checked as the daily commit checks
	// them.
	expected := c.ExpectedRemote
	if id, ok := identity.NormalizeRemote(expected); ok {
		expected = id
	}
	if e != nil || p.Root != c.WikiPath || gitErr != nil || !gitInfo.IsDir() || !wikigit.SameRemote(ctx, c.WikiPath, expected) {
		r.add("wiki remote", "FAIL", "standalone repository or remote does not match", "restore the configured wiki origin")
	} else {
		r.add("wiki remote", "PASS", "standalone repository and expected origin match", "")
	}
	w, e := okfadapter.Open(c.WikiPath, d.Locks())
	if e != nil {
		r.add("wiki index", "FAIL", "root index is unavailable", "restore index.md at the wiki root")
		return
	}
	v, e := w.Validate()
	if e != nil {
		r.add("wiki validation", "FAIL", "validation could not complete", "run eva-md validate")
	} else if len(v.Errors)+len(v.BrokenLinks)+len(v.Orphans) > 0 {
		r.add("wiki validation", "FAIL", fmt.Sprintf("%d errors, %d broken links, %d orphans", len(v.Errors), len(v.BrokenLinks), len(v.Orphans)), "run eva-md validate")
	} else {
		r.add("wiki validation", "PASS", fmt.Sprintf("%d pages; no errors, broken links or orphans", v.ConceptCount), "")
	}
	temps := 0
	seen := 0
	complete := true
	walkErr := filepath.WalkDir(c.WikiPath, func(path string, entry fs.DirEntry, e error) error {
		if ctx.Err() != nil || seen > 100000 {
			complete = false
			return fs.SkipAll
		}
		seen++
		if e != nil {
			return e
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return fs.SkipDir
		}
		if strings.HasPrefix(entry.Name(), ".tmp-okf-") {
			temps++
		}
		return nil
	})
	if walkErr != nil || !complete {
		r.add("wiki temporary files", "UNKNOWN", "temporary-file scan incomplete", "inspect the wiki before staging files")
	} else if temps > 0 {
		r.add("wiki temporary files", "FAIL", fmt.Sprintf("%d interrupted-save temporary files", temps), "inspect leftovers; never stage them")
	} else {
		r.add("wiki temporary files", "PASS", "none found", "")
	}
	if writable {
		inspectNames(ctx, r, d, c, repair)
	}
}

// refreshNames is a variable so tests can make the wiki's lock busy.
var refreshNames = systempage.RefreshNames

// inspectNames checks the names index the prompt hook reads for pointers,
// and the repos index prompt recall reads for the projects that share a
// system page. With repair it writes either again from the system pages
// when it is missing, unreadable or out of date, or removes it when it
// would list nothing, one of the doctor's two repairs, waiting for the
// wiki's lock as any write does; without, it only says so. Each row names
// the index at fault, what it costs and what the doctor does about it. A
// wiki with no system page and neither index has nothing to check.
func inspectNames(ctx context.Context, r *Report, d paths.Dirs, c config.Config, repair bool) {
	n, err := refreshNames(ctx, systempage.Options{Config: c, Dirs: d}, repair)
	switch {
	case errors.Is(err, okfadapter.ErrLockBusy):
		r.add("system names", "UNKNOWN", staleWords(n).busy, "run eva-md doctor again")
		return
	case err != nil:
		which := staleIndexes(n)
		r.add("system names", "FAIL", fmt.Sprintf("the %s of the system pages can't be checked or written again", which.name), "check that "+which.files+" in the wiki "+which.are+", then run eva-md doctor again")
		return
	case n.NamesStale || n.ReposStale:
		words := staleWords(n)
		r.add("system names", "FAIL", words.status, "run eva-md doctor, which "+words.does)
		return
	case n.Pages == 0 && !n.NamesRebuilt && !n.ReposRebuilt && !n.NamesRemoved && !n.ReposRemoved:
		return
	}
	detail := fmt.Sprintf("%d %s for %d system %s", n.Names, plural(n.Names, "name", "names"), n.Pages, plural(n.Pages, "page", "pages"))
	if n.NamesRebuilt {
		detail = "written again from the system pages: " + detail
	}
	if n.NoAliases > 0 {
		detail += fmt.Sprintf("; past %d names or %d KB, the aliases of the %d oldest %s are left out", systempage.MaxNames, systempage.MaxNamesSize>>10, n.NoAliases, plural(n.NoAliases, "page", "pages"))
	}
	if n.LeftOut > 0 {
		detail += fmt.Sprintf(", and %d oldest %s altogether", n.LeftOut, plural(n.LeftOut, "page", "pages"))
	}
	if n.NamesRemoved {
		detail += "; the names index was removed, since it would list no name"
	}
	switch {
	case n.ReposRebuilt && n.NamesRebuilt:
		detail += "; so was the repos index"
	case n.ReposRebuilt:
		detail += "; the repos index was written again from them"
	case n.ReposRemoved:
		detail += "; the repos index was removed, since it would list no page"
	}
	r.add("system names", "PASS", detail, "")
}

// indexes names the names or repos index, or both, in the system names row
// of a check or write that failed.
type indexes struct {
	name       string // "repos index"
	files, are string // the files, and what to check of them
}

// staleIndexes names the indexes a report finds stale, or both when it
// finds neither, as when the wiki couldn't be read.
func staleIndexes(n systempage.NamesReport) indexes {
	one := func(name, file string) indexes {
		return indexes{name: name, files: file, are: "is a regular file eva-md can write, or remove it"}
	}
	switch {
	case n.NamesStale && !n.ReposStale:
		return one("names index", systempage.NamesFile)
	case n.ReposStale && !n.NamesStale:
		return one("repos index", systempage.ReposFile)
	}
	return indexes{name: "names and repos indexes", files: systempage.NamesFile + " and " + systempage.ReposFile, are: "are regular files eva-md can write, or remove them"}
}

// staleRow is what the system names row says of the indexes a report finds
// stale: in eva-md status, what is wrong with them and what that costs
// (status), and what the doctor does about it (does); in a doctor that
// found the wiki busy, what it left undone (busy).
type staleRow struct{ status, does, busy string }

// staleWords words the indexes a report finds stale, or both when it finds
// neither, as when the wiki stayed busy before it was read. The doctor
// writes a stale index again, or removes one that would list nothing,
// which is worded on its own: until then, prompts may miss pointers to the
// system pages, for the names index, or, once no system page is left, the
// memory reminder may still name the system index; for the repos index,
// recall may show, or leave out, the knowledge of other projects, or, once
// no page lists two repos, show that of projects that no longer share one.
func staleWords(n systempage.NamesReport) staleRow {
	pages := fmt.Sprintf("the %d system %s", n.Pages, plural(n.Pages, "page", "pages"))
	stale := func(name, of string) string {
		return "the " + name + " of " + of + " is missing, unreadable or out of date"
	}
	// Each index's words in status and in a busy doctor's row, what the
	// doctor does about it, as "writes" the index " again", and what a busy
	// doctor left undone.
	type index struct{ status, busy, verb, again, undone string }
	names := index{stale("names index", pages) + ", so prompts may miss pointers to them", stale("names index", "the system pages"), "writes", " again", "written again"}
	if n.NamesGone {
		names.verb, names.again, names.undone = "removes", "", "removed"
	}
	// A names index that would list nothing is worded on its own once no
	// system page is left. While one is, its names alone were too long for
	// the index's bounds, and the index is no more than out of date.
	noPage := n.NamesGone && n.Pages == 0
	if noPage {
		left := "no system page is left, but the names index is still there"
		names.status, names.busy = left+", so the memory reminder may still name the system index", left
	}
	repos := index{stale("repos index", pages) + ", so recall may show, or leave out, the knowledge of other projects", stale("repos index", "the system pages"), "writes", " again", "written again"}
	if n.ReposGone {
		left := "no system page lists two repos any more, but the repos index is still there"
		repos = index{left + ", so recall may show the knowledge of projects that no longer share a system page", left, "removes", "", "removed"}
	}
	busy := ", and the wiki stayed busy, so "
	switch {
	case n.NamesStale && !n.ReposStale:
		return staleRow{names.status, names.verb + " it" + names.again, names.busy + busy + "it was not " + names.undone}
	case n.ReposStale && !n.NamesStale:
		return staleRow{repos.status, repos.verb + " it" + repos.again, repos.busy + busy + "it was not " + repos.undone}
	case noPage && n.ReposGone:
		left := "no system page is left, but the names and repos indexes are still there"
		return staleRow{left + ", so the memory reminder may still name the system index, and recall may show the knowledge of projects that no longer share a system page", "removes them", left + busy + "neither was removed"}
	case n.NamesGone || n.ReposGone:
		return staleRow{names.status + "; " + repos.status, names.verb + " the names index" + names.again + " and " + repos.verb + " the repos index" + repos.again,
			names.busy + "; " + repos.busy + "; the wiki stayed busy, so the names index was not " + names.undone + ", nor the repos index " + repos.undone}
	}
	both := "the names and repos indexes of %s are missing, unreadable or out of date"
	return staleRow{fmt.Sprintf(both, pages) + ", so prompts may miss pointers to them, and recall may show, or leave out, the knowledge of other projects", "writes them again", fmt.Sprintf(both, "the system pages") + busy + "neither was written again"}
}

func inspectInbox(ctx context.Context, r *Report, d paths.Dirs) {
	if _, e := os.Lstat(d.Inbox()); os.IsNotExist(e) {
		r.add("inbox", "UNKNOWN", "no exchanges captured yet", "start a chat with eva-md enabled")
		return
	}
	root, e := paths.OpenRoot(d.Inbox(), false)
	if e != nil {
		r.add("inbox", "FAIL", "inbox path is unsafe", "restore the private state directory")
		return
	}
	defer root.Close()
	var bytes int64
	count, bad := 0, 0
	complete := true
	err := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, e error) error {
		if ctx.Err() != nil || count > 100000 {
			complete = false
			return fs.SkipAll
		}
		count++
		if e != nil {
			return e
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		if (!info.IsDir() && !info.Mode().IsRegular()) || info.Mode().Perm()&0077 != 0 {
			bad++
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		return nil
	})
	if err != nil || !complete {
		r.add("inbox", "UNKNOWN", "scan incomplete", "inspect inbox permissions and size")
	} else if bad > 0 {
		r.add("inbox", "FAIL", fmt.Sprintf("%d unsafe or non-private entries", bad), "use directories 0700 and files 0600; remove symlink aliases")
	} else {
		r.add("inbox", "PASS", fmt.Sprintf("%d bytes; private permissions", bytes), "")
	}
}

// shellArg quotes a path for a shell when it needs it, so a fix can be
// pasted as it is.
func shellArg(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:=@%+,-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func readPrivate(file string, max int64) ([]byte, error) {
	root, err := paths.OpenRoot(filepath.Dir(file), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(filepath.Base(file), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, fmt.Errorf("invalid health input")
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(data)) > max {
		return nil, fmt.Errorf("oversized health input")
	}
	return data, err
}
