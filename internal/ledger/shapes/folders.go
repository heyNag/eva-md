package shapes

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/ledger/audit"
)

// cachedRepos is the most entries of eva-md's identity cache read to judge
// a folder by its repository's remote.
const cachedRepos = 4096

// outside judges a folder as the hooks leave one out: by its path, as the
// capture audit judges it, and, when the config excludes remotes
// (`remote:`), by the remote of the repository eva-md's identity cache
// last gave the folder, as the hook judges it by the remote it resolves.
// The cache is eva-md's own: reading it starts no git and opens no folder
// of a chat's. The newest entry of the deepest repository holding the
// folder, as written or where its links lead, gives the remote; a folder
// no entry holds is judged by its path alone. An entry may be out of date,
// which can only leave out a chat the hook would not leave out today.
func outside(o Options) func(string) bool {
	byPath := audit.Outside(audit.Options{Dirs: o.Dirs, Home: o.Home, TempDir: o.TempDir, Config: o.Config})
	if !slices.ContainsFunc(o.Config.Exclude, func(e string) bool {
		return strings.HasPrefix(strings.TrimSpace(e), config.RemotePrefix)
	}) {
		return byPath
	}
	entries := identity.Cache{Dir: filepath.Join(o.Dirs.Cache(), "identity")}.Entries(cachedRepos)
	fold := func(p string) string {
		if runtime.GOOS == "darwin" {
			return strings.ToLower(p)
		}
		return p
	}
	// remote is the remote of the deepest repository the cache holds dir
	// in, and whether one holds it.
	remote := func(dir string) (string, bool) {
		depth, found, got := -1, false, ""
		for _, e := range entries {
			root := filepath.Clean(e.Path)
			rel, err := filepath.Rel(fold(root), fold(dir))
			if err != nil || (rel != "." && !filepath.IsLocal(rel)) || len(root) <= depth {
				continue
			}
			depth, found, got = len(root), true, e.Project.Remote
		}
		return got, found
	}
	known := map[string]bool{}
	return func(dir string) bool {
		if byPath(dir) {
			return true
		}
		if !filepath.IsAbs(dir) {
			return false
		}
		if v, ok := known[dir]; ok {
			return v
		}
		ways := []string{filepath.Clean(dir)}
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			ways = append(ways, filepath.Clean(r))
		}
		v := false
		for _, d := range ways {
			if r, ok := remote(d); ok && r != "" && o.Config.Excluded("", r) {
				v = true
			}
		}
		known[dir] = v
		return v
	}
}
