package health

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

// Bounds of eva-md status's listings.
const (
	maxProjects    = 4096 // project folders counted in the wiki
	filesPerFolder = 64   // a project's newest session files whose watermarks are read, as a brief reads them
	maxEntries     = 4096 // entries of the inbox's project folders listed in all
)

// Status is eva-md status: the doctor's local checks, without asking the
// hosts or the doctor's repair, then what the wiki and the inbox hold: its
// projects and threads, and the sessions whose entries wait for an update.
func Status(ctx context.Context, d paths.Dirs, module string) Report {
	r := inspect(ctx, d, module, false, false)
	c, err := config.LoadContext(ctx, d.ConfigFile())
	if err != nil || c.WikiPath == "" {
		return r
	}
	inspectContents(&r, c)
	inspectBacklog(ctx, &r, d, c)
	return r
}

// inspectContents counts the wiki's project folders and thread pages from
// folder listings, never reading a page.
func inspectContents(r *Report, c config.Config) {
	threads := countPages(filepath.Join(c.WikiPath, "general", "threads"))
	entries, err := os.ReadDir(filepath.Join(c.WikiPath, "projects"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		r.add("wiki contents", "UNKNOWN", "the projects folder is unreadable", "restore the wiki's projects folder")
		return
	}
	projects, more := 0, ""
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if projects == maxProjects {
			more = "at least "
			break
		}
		projects++
		threads += countPages(filepath.Join(c.WikiPath, "projects", e.Name(), "threads"))
	}
	r.add("wiki contents", "PASS", fmt.Sprintf("%s%d %s, %d %s", more, projects, plural(projects, "project", "projects"), threads, plural(threads, "thread", "threads")), "")
}

// countPages counts the thread pages of a threads folder: its Markdown
// files other than the index.
func countPages(dir string) int {
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".md"); ok && e.Type().IsRegular() && name != "index" && !strings.HasPrefix(name, ".") {
			n++
		}
	}
	return n
}

// inspectBacklog counts the sessions whose entries wait for an update: of
// each project's 64 newest session files, those longer than their
// watermark. It names those whose watermark can't be read or is past the end
// of the file, which the hooks never start an update for. It lists at most
// 4096 entries of the project folders in all, and reads a watermark a file,
// never an entry.
func inspectBacklog(ctx context.Context, r *Report, d paths.Dirs, c config.Config) {
	store := inbox.Store{Root: d.Inbox()}
	projects, err := os.ReadDir(d.Inbox())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		r.add("update backlog", "UNKNOWN", "the inbox is unreadable", "inspect inbox permissions")
		return
	}
	waiting, left, more := 0, maxEntries, ""
	var damaged []string
	for _, p := range projects {
		if !p.IsDir() || strings.HasPrefix(p.Name(), ".") {
			continue
		}
		if left == 0 || ctx.Err() != nil {
			more = "at least "
			break
		}
		files, all := newestSessions(filepath.Join(d.Inbox(), p.Name()), &left)
		if !all {
			more = "at least "
		}
		for _, f := range files {
			m, err := store.Mark(f.path)
			switch {
			case err != nil || m.Offset > f.size:
				damaged = append(damaged, fileRef(f.path))
			case m.Pending(f.size) > 0:
				waiting++
			}
		}
	}
	detail := fmt.Sprintf("%s%d %s for an update", more, waiting, plural(waiting, "session waits", "sessions wait"))
	if waiting == 0 && more == "" {
		detail = "no session waits for an update"
	}
	if !c.Updates {
		detail += "; updates are off (updates = false in the config)"
	}
	if n := len(damaged); n > 0 {
		detail += fmt.Sprintf("; %d %s a watermark that can't be read or is past the end of its file: %s", n, plural(n, "session has", "sessions have"), list(damaged))
		r.add("update backlog", "FAIL", detail, "delete each damaged watermark, in the .state folder beside the session's inbox file, to read the session again from its start")
		return
	}
	r.add("update backlog", "PASS", detail, "")
}

// fileRef names a session file as host:<the first 8 characters of its id>.
func fileRef(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	for _, host := range updateHosts {
		if id, ok := strings.CutPrefix(name, host+"-"); ok {
			return sessionRefs(host, []string{id})
		}
	}
	return name
}

// sessionFile is a session file as a folder listing found it.
type sessionFile struct {
	path string
	size int64
}

// newestSessions lists a project folder's session files, newest change
// first, at most filesPerFolder. It lists at most *left of the folder's
// entries, in batches, and takes what it listed from *left; all reports that
// it listed the whole folder.
func newestSessions(dir string, left *int) (files []sessionFile, all bool) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, true
	}
	defer f.Close()
	type found struct {
		f   sessionFile
		mod int64
	}
	var seen []found
	for *left > 0 {
		batch, err := f.ReadDir(min(128, *left))
		*left -= len(batch)
		for _, e := range batch {
			name := e.Name()
			if !e.Type().IsRegular() || !strings.HasSuffix(name, ".jsonl") || !strings.HasPrefix(name, "claude-code-") && !strings.HasPrefix(name, "codex-") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			seen = append(seen, found{sessionFile{filepath.Join(dir, name), info.Size()}, info.ModTime().UnixNano()})
		}
		if err != nil {
			all = errors.Is(err, io.EOF)
			break
		}
	}
	sort.Slice(seen, func(i, j int) bool { return seen[i].mod > seen[j].mod })
	for _, s := range seen[:min(len(seen), filesPerFolder)] {
		files = append(files, s.f)
	}
	return files, all
}
