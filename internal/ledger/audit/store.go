package audit

import (
	"io/fs"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/config"
)

// Store counts, by file name alone, the sessions a host's own folder of
// chats holds and those of them with no eva-md file. No file is opened: a
// session's id is read from its file's name, and its last write from the
// folder.
type Store struct {
	Host  string
	Files int // the host's session files
	// NoEvaFile counts the files with no eva-md file of the same session
	// written since the inbox's oldest line, and Older those written before.
	NoEvaFile, Older int
	// ByDesign counts, of NoEvaFile, those in a folder eva-md leaves out by
	// design (a temporary, spare, excluded, wiki or eva-md folder), told by
	// the name Claude Code gives a project's folder of chats. Several
	// folders share a name, so it may count a folder captured; a folder
	// excluded by its remote is not told, so it is not counted. Codex names
	// no folder, so there it is not known (Folders false).
	ByDesign int
	Folders  bool // the file names tell the folders, so ByDesign is known
}

// stores counts each host's folder of chats under o.Home against the
// sessions the inbox knows, by host. first is the inbox's oldest line.
func stores(o Options, known map[string]map[string]bool, first time.Time) []Store {
	left := leftOut(o)
	var out []Store
	for _, host := range []string{"claude-code", "codex"} {
		s := Store{Host: host, Folders: host == "claude-code"}
		walkStore(o.Home, host, func(folder, id string, mod time.Time) {
			s.Files++
			switch {
			case known[host][id]:
			case !first.IsZero() && mod.Before(first):
				s.Older++
			default:
				s.NoEvaFile++
				if host == "claude-code" && left(folder) {
					s.ByDesign++
				}
			}
		})
		out = append(out, s)
	}
	return out
}

// walkStore calls f for each session file of the host's folder of chats
// under home, with the name of its project folder on Claude Code, its
// session id and its last write. It follows the folder itself where a
// symlink leads, as recovery does, and no symlink inside it.
func walkStore(home, host string, f func(folder, id string, mod time.Time)) {
	root := filepath.Join(home, ".claude", "projects")
	depth := 2 // projects/<folder>/<id>.jsonl
	if host == "codex" {
		root = filepath.Join(home, ".codex", "sessions")
		depth = 4 // sessions/<year>/<month>/<day>/rollout-<time>-<id>.jsonl
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	_ = filepath.WalkDir(resolved, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(resolved, p)
		n := len(strings.Split(rel, string(filepath.Separator)))
		if d.IsDir() {
			if rel != "." && n >= depth {
				return filepath.SkipDir
			}
			return nil
		}
		if n != depth || !d.Type().IsRegular() {
			return nil
		}
		id, ok := sessionID(host, d.Name())
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		f(filepath.Base(filepath.Dir(p)), id, info.ModTime())
		return nil
	})
}

// sessionID reads a session's id from the name of its file in the host's
// folder: <id>.jsonl on Claude Code, rollout-<time>-<id>.jsonl on Codex,
// whose id is a UUID.
func sessionID(host, name string) (string, bool) {
	base, ok := strings.CutSuffix(name, ".jsonl")
	if !ok || base == "" {
		return "", false
	}
	if host == "claude-code" {
		return base, !strings.HasPrefix(base, ".")
	}
	if !strings.HasPrefix(base, "rollout-") || len(base) < 36 {
		return "", false
	}
	id := base[len(base)-36:]
	for i, c := range id {
		dash := i == 8 || i == 13 || i == 18 || i == 23
		if dash != (c == '-') || !dash && !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return "", false
		}
	}
	return id, true
}

// claudeFolder is the name Claude Code gives the folder of the chats run in
// dir: every character but an ASCII letter or digit becomes a dash. glob
// keeps the wildcards of a pattern, a class standing for any one character.
func claudeFolder(dir string, glob bool) string {
	var b strings.Builder
	class := false
	for _, c := range dir {
		switch {
		case class:
			class = c != ']'
		case glob && c == '[':
			class = true
			b.WriteByte('?')
		case glob && (c == '*' || c == '?'):
			b.WriteRune(c)
		case c < 128 && ('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'):
			b.WriteRune(c)
		default:
			b.WriteByte('-')
		}
	}
	return strings.ToLower(b.String())
}

// leftOut returns whether a Claude Code project folder's name is that of a
// folder, or of one inside a folder, whose chats v1's hook leaves out by
// design: a temporary folder, Claude Code's spares, eva-md's state or wiki
// folder, or a folder the config excludes. Letter case is ignored and
// several folders share a name, so it may say yes for a folder captured.
func leftOut(o Options) func(string) bool {
	var dirs []string
	for _, d := range []string{o.TempDir, "/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp", "/var/folders", "/private/var/folders",
		filepath.Join(o.Home, ".claude", "spares"), o.Dirs.State, o.Config.WikiPath} {
		if !filepath.IsAbs(d) {
			continue
		}
		dirs = append(dirs, d)
		if r, err := filepath.EvalSymlinks(d); err == nil {
			dirs = append(dirs, r)
		}
	}
	var patterns []string
	for _, d := range dirs {
		patterns = append(patterns, claudeFolder(d, false))
	}
	for _, e := range o.Config.Exclude {
		e = strings.TrimSpace(e)
		if strings.HasPrefix(e, config.RemotePrefix) {
			continue
		}
		if e == "~" || strings.HasPrefix(e, "~/") {
			e = filepath.Join(o.Home, strings.TrimPrefix(e, "~"))
		}
		if filepath.IsAbs(e) {
			patterns = append(patterns, claudeFolder(filepath.Clean(e), true))
		}
	}
	return func(folder string) bool {
		folder = strings.ToLower(folder)
		for _, p := range patterns {
			if ok, _ := path.Match(p, folder); ok {
				return true
			}
			if ok, _ := path.Match(p+"-*", folder); ok {
				return true
			}
		}
		return false
	}
}

// outside returns whether a chat's folder is one whose events v1's hook
// leaves out by design, as the hook judges it but for an exclusion by the
// folder's remote, which needs git: an excluded folder, a temporary
// folder, Claude Code's spares, Codex's memories, eva-md's state or the
// wiki, or a folder inside one, by its path and where its symlinks lead.
func outside(o Options) func(string) bool {
	var roots []string
	for _, d := range []string{o.TempDir, "/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp",
		filepath.Join(o.Home, ".claude", "spares"), filepath.Join(o.Home, ".codex", "memories"), o.Dirs.State, o.Config.WikiPath} {
		if !filepath.IsAbs(d) {
			continue
		}
		if r, err := filepath.EvalSymlinks(d); err == nil {
			d = r
		}
		roots = append(roots, filepath.Clean(d))
	}
	fold := func(p string) string {
		if runtime.GOOS == "darwin" {
			return strings.ToLower(p)
		}
		return p
	}
	inside := func(child string) bool {
		for _, root := range roots {
			if rel, err := filepath.Rel(fold(root), fold(child)); err == nil && (rel == "." || filepath.IsLocal(rel)) {
				return true
			}
		}
		return false
	}
	known := map[string]bool{}
	return func(dir string) bool {
		if v, ok := known[dir]; ok {
			return v
		}
		v := false
		if filepath.IsAbs(dir) {
			resolved := filepath.Clean(dir)
			if r, err := filepath.EvalSymlinks(dir); err == nil {
				resolved = r
			}
			v = o.Config.Excluded(dir, "") || o.Config.Excluded(resolved, "") || inside(resolved) ||
				strings.HasSuffix(filepath.ToSlash(resolved), "/.codex/memories")
		}
		known[dir] = v
		return v
	}
}
