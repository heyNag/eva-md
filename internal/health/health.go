// Package health reports observable local state without model calls. Its
// one repair writes the names index of the system pages again (see
// inspectNames).
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
	DurationMS          float64   `json:"duration_ms"`
	Runs                []hookRun `json:"runs"` // the event's last runs, oldest first
	// Entrypoint is the Claude Code entrypoint the last run inherited, and
	// Headless whether the hook took it for a program's chat.
	Entrypoint string `json:"entrypoint"`
	Headless   bool   `json:"headless"`
}

// hookRun is one run in a heartbeat's history.
type hookRun struct {
	At     time.Time `json:"at"`
	Failed bool      `json:"failed"`
}

// Inspect checks disk state and optionally queries native hosts, as eva-md
// doctor does. It never authenticates, starts a model, changes trust or
// writes host settings; its one repair is the names index (see
// inspectNames).
func Inspect(ctx context.Context, d paths.Dirs, module string, hosts bool) Report {
	return inspect(ctx, d, module, hosts, true)
}

// inspect is Inspect, and without repair, as eva-md status, writes nothing.
func inspect(ctx context.Context, d paths.Dirs, module string, hosts, repair bool) Report {
	r := Report{}
	if info, err := os.Lstat(filepath.Join(d.Share, "bin", "eva-md")); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
		r.add("binary", "PASS", "stable hook binary is installed", "")
	} else {
		r.add("binary", "UNKNOWN", "stable hook binary is not installed", "run eva-md install")
	}
	c, err := config.LoadContext(ctx, d.ConfigFile())
	if err != nil {
		r.add("config", "FAIL", "configuration is missing or invalid", "run eva-md setup, or repair the eva-md config")
	} else {
		r.add("config", "PASS", "configuration parses", "")
		if len(c.Unknown) > 0 {
			r.add("config keys", "FAIL", "unknown keys are ignored: "+strings.Join(c.Unknown, ", "), "correct or remove them in the eva-md config")
		}
		if info, e := os.Lstat(d.ConfigFile()); e != nil || info.Mode().Perm()&0077 != 0 {
			r.add("config permissions", "FAIL", "config must be private", "set config mode to 0600")
		}
		inspectWiki(ctx, &r, d, c, module, repair)
	}
	inspectInbox(ctx, &r, d)
	now := time.Now()
	for _, host := range []string{"claude-code", "codex"} {
		for _, event := range hostEvents(host) {
			b, valid := readBeat(d, host, event, now)
			if !valid {
				r.add(host+" "+event, "UNKNOWN", "no valid heartbeat observed", "exercise this event in an enabled, trusted session")
				continue
			}
			detail := "last observed " + b.At.Format(time.RFC3339)
			if !b.TranscriptAvailable {
				detail += "; transcript unavailable for catch-up"
			}
			if b.Entrypoint != "" {
				detail += "; entrypoint " + b.Entrypoint
				if b.Headless {
					detail += ", a program's chat: its prompts are stored as host prompts"
				}
			}
			status := "PASS"
			if now.Sub(b.At) > 24*time.Hour {
				status = "UNKNOWN"
				detail += "; historical evidence only"
			}
			r.add(host+" "+event, status, detail, "")
		}
		r.Checks = append(r.Checks, captureFreshness(ctx, d, host, now), hookCheck(d, host, now), driftCheck(d, host))
	}
	if hosts {
		inspectHosts(ctx, &r, d)
	}
	inspectBackground(&r, d, c, now)
	return r
}

func inspectWiki(ctx context.Context, r *Report, d paths.Dirs, c config.Config, module string, repair bool) {
	if c.WikiPath == "" {
		r.add("wiki", "FAIL", "wiki path is missing", "run eva-md setup")
		return
	}
	root, err := paths.OpenRoot(c.WikiPath, false)
	if err != nil {
		r.add("wiki location", "FAIL", "wiki path is unavailable or unsafe", "restore the configured private wiki folder")
		return
	}
	root.Close()
	r.add("wiki location", "PASS", "wiki is a real, accessible directory", "")
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
		if module == "unknown" || module == "" {
			r.add("okf module", "UNKNOWN", "binary dependency pin is unavailable", "use a binary built from the pinned module")
		} else if s.OKFModule != module {
			r.add("okf module", "FAIL", "wiki pin and binary pin differ", "review the dependency upgrade before writing")
		} else {
			r.add("okf module", "PASS", "wiki and binary dependency pins agree", "")
		}
	}
	p, e := identity.Resolve(ctx, c.WikiPath)
	gitInfo, gitErr := os.Lstat(filepath.Join(c.WikiPath, ".git"))
	expected, valid := identity.NormalizeRemote(c.ExpectedRemote)
	// init saves the normalized identity, not necessarily a transport URL.
	// Accept an exact match with the identity already validated by Resolve.
	if !valid && p.Remote != "" && c.ExpectedRemote == p.Remote {
		expected, valid = p.Remote, true
	}
	if e != nil || p.Root != c.WikiPath || !valid || p.Remote != expected || gitErr != nil || !gitInfo.IsDir() {
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
		r.add("wiki validation", "PASS", fmt.Sprintf("%d concepts; no errors, broken links or orphans", v.ConceptCount), "")
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

// inspectNames checks the names index the prompt hook reads. With repair it
// writes the index again from the system pages when it is missing,
// unreadable or out of date, the doctor's one repair, waiting for the wiki's
// lock as any write does; without, it only says so. A wiki without system
// pages has nothing to check.
func inspectNames(ctx context.Context, r *Report, d paths.Dirs, c config.Config, repair bool) {
	n, err := refreshNames(ctx, systempage.Options{Config: c, Dirs: d}, repair)
	switch {
	case errors.Is(err, okfadapter.ErrLockBusy):
		r.add("system names", "UNKNOWN", "the names index of the system pages is missing, unreadable or out of date, and the wiki stayed busy, so it was not written again", "run eva-md doctor again")
		return
	case err != nil:
		r.add("system names", "FAIL", "the names index of the system pages can't be checked or written again", "check that "+systempage.NamesFile+" in the wiki is a regular file eva-md can write, or remove it, then run eva-md doctor again")
		return
	case n.Stale:
		r.add("system names", "FAIL", fmt.Sprintf("the names index of the %d system %s is missing, unreadable or out of date, so prompts may miss pointers to them", n.Pages, plural(n.Pages, "page", "pages")), "run eva-md doctor, which writes it again")
		return
	case n.Pages == 0 && !n.Rebuilt:
		return
	}
	detail := fmt.Sprintf("%d %s for %d system %s", n.Names, plural(n.Names, "name", "names"), n.Pages, plural(n.Pages, "page", "pages"))
	if n.Rebuilt {
		detail = "written again from the system pages: " + detail
	}
	if n.NoAliases > 0 {
		detail += fmt.Sprintf("; past %d names or %d KB, the aliases of the %d oldest %s are left out", systempage.MaxNames, systempage.MaxNamesSize>>10, n.NoAliases, plural(n.NoAliases, "page", "pages"))
	}
	if n.LeftOut > 0 {
		detail += fmt.Sprintf(", and %d oldest %s altogether", n.LeftOut, plural(n.LeftOut, "page", "pages"))
	}
	r.add("system names", "PASS", detail, "")
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
