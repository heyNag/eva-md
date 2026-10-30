package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/heyNag/eva-md/internal/paths"
)

// Cache keeps the results of Resolve so that a hook in a known worktree
// starts no git process. Each worktree, and each folder outside any repo, has
// one small entry in Dir. An entry is used only while a fresh look at what git
// reads to answer Resolve finds it unchanged; see fingerprint for what is
// looked at. A missing, unreadable or damaged entry, or anything the look does
// not understand, runs Resolve instead. Its answer replaces the entry only
// when nothing changed while it ran.
type Cache struct {
	Dir string // paths.Dirs.Cache()/identity
}

// cacheVersion changes whenever the entry format or the fingerprint changes.
const cacheVersion = 2

const (
	maxEntry        = 16 << 10
	maxSmallFile    = 4 << 10 // HEAD and commondir
	maxGitFile      = 1 << 20 // git's own limit for a .git file
	maxConfigFile   = 1 << 20
	maxConfigFiles  = 32
	maxIncludeDepth = 10 // git's MAX_INCLUDE_DEPTH
)

type cacheEntry struct {
	V       int     `json:"v"`
	Path    string  `json:"path"`             // worktree top level, or the folder outside any repo
	System  string  `json:"system,omitempty"` // system git config as systemConfig reported it: a path, "" for none, or unknownSystem
	Sig     string  `json:"sig"`
	Project Project `json:"project"`
	Sum     string  `json:"sum"`
}

func (e cacheEntry) checksum() string {
	e.Sum = ""
	data, _ := json.Marshal(e)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Resolve returns what the package-level Resolve returns for cwd.
func (c Cache) Resolve(ctx context.Context, cwd string) (Project, error) {
	if !filepath.IsAbs(cwd) || !filepath.IsAbs(c.Dir) || ctx.Err() != nil {
		return Resolve(ctx, cwd)
	}
	key, ok := cacheKey()
	if !ok {
		return Resolve(ctx, cwd)
	}
	before, ok := look(cwd)
	if !ok {
		return Resolve(ctx, cwd)
	}
	name := entryName(key, before)
	old, found := c.load(name, before.path())
	system, learned := old.System, false
	if before.root != "" && !found {
		if system, ok = systemConfig(ctx); !ok {
			return Resolve(ctx, cwd)
		}
		learned = true
	}
	sig, want, ok := fingerprint(before, system)
	if found && ok && sig == old.Sig {
		return old.Project, nil
	}
	p, err := Resolve(ctx, cwd)
	e := cacheEntry{V: cacheVersion, Path: before.path(), System: system}
	if ok && err == nil {
		// Keep the answer only if nothing it depends on changed while git ran.
		if after, ok := look(cwd); ok && after.root == before.root {
			if again, _, ok := fingerprint(after, system); ok && again == sig && want.agrees(p, before.root) {
				e.Sig, e.Project = sig, p
			}
		}
	}
	// An entry without a signature never matches. It only saves asking git
	// for its system config again.
	if e.Sig != "" || learned {
		c.store(name, e)
	}
	return p, err
}

// cacheKey covers what decides how git behaves apart from the files in the
// fingerprint: the user, the git executable and the environment git sees.
func cacheKey() (string, bool) {
	euid := os.Geteuid()
	if euid == 0 {
		return "", false // git's ownership checks then depend on SUDO_UID
	}
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", false
	}
	info, err := os.Stat(bin)
	if err != nil {
		return "", false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "v%d\x00%d\x00%s\x00%d:%d:%d:%d\x00", cacheVersion, euid, bin, st.Dev, st.Ino, info.Size(), info.ModTime().UnixNano())
	env := gitEnv()
	sort.Strings(env)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == "HOME" || name == "XDG_CONFIG_HOME" || strings.HasPrefix(name, "GIT_") {
			b.WriteString(kv)
			b.WriteByte(0)
		}
	}
	return b.String(), true
}

// place is where git's discovery from cwd ends: the worktree top level, or no
// repo at all.
type place struct {
	cwd  string
	root string      // "" outside any repo
	info os.FileInfo // root, or cwd outside any repo
}

func (pl place) path() string {
	if pl.root != "" {
		return pl.root
	}
	return pl.cwd
}

func entryName(key string, pl place) string {
	kind := "repo"
	if pl.root == "" {
		kind = "general"
	}
	sum := sha256.Sum256([]byte(key + kind + "\x00" + pl.path()))
	return hex.EncodeToString(sum[:20]) + ".json"
}

// look repeats git's repository discovery for cwd. It requires a physical
// path, so that walking up by name visits the folders git visits, and gives up
// on anything git might treat differently: a folder below the top level that
// could itself be a git directory, a change of file system, or an error.
func look(cwd string) (place, bool) {
	if cwd != filepath.Clean(cwd) {
		return place{}, false
	}
	// cwd, then each parent up to "/". Lstat sees a symlink as no folder.
	var up []string
	for d := cwd; ; d = filepath.Dir(d) {
		up = append(up, d)
		if d == "/" {
			break
		}
	}
	infos := make([]os.FileInfo, len(up))
	for i, d := range up {
		info, err := os.Lstat(d)
		if err != nil || !info.IsDir() {
			return place{}, false
		}
		infos[i] = info
	}
	for i, d := range up {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			// git stops at a file system boundary on its way up.
			for _, info := range infos[:i+1] {
				if device(info) != device(infos[0]) {
					return place{}, false
				}
			}
			return place{cwd: cwd, root: d, info: infos[i]}, true
		} else if !absent(err) {
			return place{}, false
		}
		if _, err := os.Lstat(filepath.Join(d, "HEAD")); !absent(err) {
			return place{}, false
		}
	}
	return place{cwd: cwd, info: infos[0]}, true
}

// prediction is what HEAD says Resolve must report, checked against the live
// answer before it is stored.
type prediction struct {
	detached bool
	branch   string
	loose    bool // the branch has a loose ref, so it has commits
}

func (w prediction) agrees(p Project, root string) bool {
	if root == "" {
		return p == General()
	}
	if p.Root != root || p.Kind == KindGeneral {
		return false
	}
	if w.detached {
		return p.Detached && p.Branch == ""
	}
	return !p.Detached && (p.Branch == w.branch || !w.loose && p.Branch == DefaultBranch)
}

// fingerprint hashes everything Resolve's answer for pl depends on, as git
// reads it. Outside a repo that is just the folder. In a repo it is:
//   - the top level, the git directory and the common directory: device,
//     inode, mode and owner, so a moved, replaced or re-cloned repository, a
//     moved worktree and a change of owner all differ;
//   - the .git file, HEAD and commondir, by content and stat;
//   - every config file git reads: system, global, the repository's config
//     and config.worktree, and every file they include (followed whatever
//     the condition), by content and stat, or as missing;
//   - packed-refs by stat, and whether the branch's loose ref exists;
//   - that no other ref is named HEAD, which git would refuse as ambiguous.
//
// The top level's own mtime is left out: files come and go there all the
// time without changing the answer. It reports false when something is not
// in the plain form git writes, so such repositories are always resolved live.
func fingerprint(pl place, system string) (string, prediction, bool) {
	s := scanner{h: sha256.New()}
	s.add("v", strconv.Itoa(cacheVersion))
	if pl.root == "" {
		s.add("general", pl.cwd, dirSig(pl.info))
		return s.sum(), prediction{}, true
	}
	if system != "" && !filepath.IsAbs(system) {
		return "", prediction{}, false
	}
	root := pl.root
	s.add("root", root, dirSig(pl.info))
	dotgit := filepath.Join(root, ".git")
	gitdir := dotgit
	info, err := os.Lstat(dotgit)
	switch {
	case err != nil:
		return "", prediction{}, false
	case info.IsDir():
	case info.Mode().IsRegular():
		data, info, err := readRegular(dotgit, maxGitFile, false)
		if err != nil {
			return "", prediction{}, false
		}
		s.addFile("gitfile", dotgit, info, data)
		target, ok := gitfileTarget(data)
		if !ok {
			return "", prediction{}, false
		}
		if !filepath.IsAbs(target) {
			target = root + "/" + target
		}
		if gitdir, err = filepath.EvalSymlinks(target); err != nil {
			return "", prediction{}, false
		}
	default:
		return "", prediction{}, false
	}
	if !s.dir("gitdir", gitdir) {
		return "", prediction{}, false
	}
	head, info, err := readRegular(filepath.Join(gitdir, "HEAD"), maxSmallFile, false)
	if err != nil {
		return "", prediction{}, false
	}
	s.addFile("HEAD", "", info, head)
	common := gitdir
	switch data, info, err := readRegular(filepath.Join(gitdir, "commondir"), maxSmallFile, false); {
	case absent(err):
		s.add("commondir", "-")
	case err != nil:
		return "", prediction{}, false
	default:
		s.addFile("commondir", "", info, data)
		target := strings.TrimRight(string(data), "\r\n")
		if target == "" || strings.ContainsRune(target, 0) {
			return "", prediction{}, false
		}
		if !filepath.IsAbs(target) {
			target = gitdir + "/" + target
		}
		if common, err = filepath.EvalSymlinks(target); err != nil {
			return "", prediction{}, false
		}
	}
	if !s.dir("common", common) || !s.dir("objects", filepath.Join(common, "objects")) || !s.dir("refs", filepath.Join(common, "refs")) {
		return "", prediction{}, false
	}
	refDirs := []string{gitdir}
	if common != gitdir {
		refDirs = append(refDirs, common)
	}
	for _, d := range refDirs {
		if _, err := os.Lstat(filepath.Join(d, "reftable")); !absent(err) {
			return "", prediction{}, false
		}
	}
	switch info, err := os.Lstat(filepath.Join(common, "packed-refs")); {
	case absent(err):
		s.add("packed-refs", "-")
	case err == nil && info.Mode().IsRegular():
		s.add("packed-refs", fileSig(info))
	default:
		return "", prediction{}, false
	}
	var want prediction
	value := string(head)
	switch {
	case strings.HasSuffix(value, "\n") && hexOID(value[:len(value)-1]):
		want.detached = true
	case strings.HasPrefix(value, "ref: refs/heads/") && strings.HasSuffix(value, "\n"):
		want.branch = value[len("ref: refs/heads/") : len(value)-1]
		if !plainBranch(want.branch) {
			return "", prediction{}, false
		}
		switch info, err := os.Lstat(filepath.Join(common, "refs", "heads", want.branch)); {
		case absent(err):
			s.add("branch", "-")
		case err == nil && info.Mode().IsRegular():
			s.add("branch", "loose")
			want.loose = true
		default:
			return "", prediction{}, false
		}
	default:
		return "", prediction{}, false
	}
	// Another ref spelled HEAD makes rev-parse refuse HEAD as ambiguous.
	if !noRefs(refDirs, "refs/HEAD", "refs/tags/HEAD", "refs/heads/HEAD", "refs/remotes/HEAD", "refs/remotes/HEAD/HEAD") {
		return "", prediction{}, false
	}
	configs := []string{filepath.Join(common, "config"), filepath.Join(gitdir, "config.worktree")}
	configs = append(configs, globalConfigs()...)
	if system != "" {
		configs = append(configs, system)
	}
	for _, file := range configs {
		if !s.config(file, 0) {
			return "", prediction{}, false
		}
	}
	return s.sum(), want, true
}

type scanner struct {
	h     hash.Hash
	files int
}

func (s *scanner) add(fields ...string) {
	for _, f := range fields {
		io.WriteString(s.h, f)
		s.h.Write([]byte{0})
	}
	s.h.Write([]byte{'\n'})
}

func (s *scanner) addFile(label, path string, info os.FileInfo, data []byte) {
	s.add(label, path, fileSig(info), strconv.Itoa(len(data)))
	s.h.Write(data)
}

func (s *scanner) dir(label, path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	s.add(label, path, dirSig(info))
	return true
}

// config adds one config file git would read, following symlinks as git
// does, and then every file it includes.
func (s *scanner) config(path string, depth int) bool {
	if s.files++; s.files > maxConfigFiles {
		return false
	}
	data, info, err := readRegular(path, maxConfigFile, true)
	if absent(err) {
		s.add("config", path, "-")
		return true
	}
	if err != nil {
		return false
	}
	s.addFile("config", path, info, data)
	includes, ok := configIncludes(data)
	if !ok {
		return false
	}
	for _, value := range includes {
		target, ok := includeTarget(value, path)
		if !ok || depth >= maxIncludeDepth || !s.config(target, depth+1) {
			return false
		}
	}
	return true
}

func (s *scanner) sum() string { return hex.EncodeToString(s.h.Sum(nil)) }

func device(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev)
	}
	return 0
}

func dirSig(info os.FileInfo) string {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "?"
	}
	return fmt.Sprintf("%d:%d:%o:%d", st.Dev, st.Ino, uint32(info.Mode()), st.Uid)
}

func fileSig(info os.FileInfo) string {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "?"
	}
	return fmt.Sprintf("%d:%d:%o:%d:%d:%d", st.Dev, st.Ino, uint32(info.Mode()), st.Uid, info.Size(), info.ModTime().UnixNano())
}

// absent reports an error that means the path does not exist, which git
// treats as a missing file.
func absent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

var errNotRegular = errors.New("identity: not a regular file")

// readRegular reads a regular file of at most limit bytes without blocking on
// a FIFO. With follow false a symlink is refused.
func readRegular(path string, limit int64, follow bool) ([]byte, os.FileInfo, error) {
	flags := os.O_RDONLY | syscall.O_NONBLOCK
	if !follow {
		flags |= syscall.O_NOFOLLOW
	}
	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, nil, errNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		return nil, nil, errNotRegular
	}
	return data, info, nil
}

// gitfileTarget parses a .git file the way git's read_gitfile_gently does.
func gitfileTarget(data []byte) (string, bool) {
	v := string(data)
	if !strings.HasPrefix(v, "gitdir: ") {
		return "", false
	}
	v = strings.TrimRight(v, "\r\n")
	if len(v) < len("gitdir: x") || strings.ContainsRune(v, 0) {
		return "", false
	}
	return v[len("gitdir: "):], true
}

func hexOID(v string) bool {
	if len(v) != 40 && len(v) != 64 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// plainBranch accepts a branch name that git would accept and that names no
// special ref when looked up on its own. Anything else is resolved live.
func plainBranch(b string) bool {
	if b == "" || b == "HEAD" || b == "@" || len(b) > 1024 || strings.Contains(b, "..") || strings.Contains(b, "@{") ||
		strings.HasPrefix(b, "main-worktree/") || strings.HasPrefix(b, "worktrees/") {
		return false
	}
	for _, r := range b {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return false
		}
	}
	for _, part := range strings.Split(b, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return !strings.HasSuffix(b, ".")
}

// noRefs reports that none of names exists as a loose ref in dirs. A packed
// ref is covered by packed-refs being in the fingerprint.
func noRefs(dirs []string, names ...string) bool {
	for _, d := range dirs {
		for _, name := range names {
			if _, err := os.Lstat(filepath.Join(d, name)); !absent(err) {
				return false
			}
		}
	}
	return true
}

func (c Cache) load(name, path string) (cacheEntry, bool) {
	root, err := paths.OpenRoot(c.Dir, false)
	if err != nil {
		return cacheEntry{}, false
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return cacheEntry{}, false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > maxEntry {
		return cacheEntry{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEntry+1))
	if err != nil || len(data) > maxEntry {
		return cacheEntry{}, false
	}
	var e cacheEntry
	if json.Unmarshal(data, &e) != nil || e.V != cacheVersion || e.Path != path || e.Sum != e.checksum() {
		return cacheEntry{}, false
	}
	return e, true
}

// store replaces an entry by renaming a new 0600 file over it, so a reader
// sees the old entry or the new one. It skips fsync: a lost or torn entry
// only means the next hook resolves live.
func (c Cache) store(name string, e cacheEntry) {
	e.Sum = e.checksum()
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	root, err := paths.OpenRoot(c.Dir, true)
	if err != nil {
		return
	}
	defer root.Close()
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return
	}
	tmp := paths.TempPrefix + hex.EncodeToString(random[:])
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr != nil || cerr != nil || root.Rename(tmp, name) != nil {
		_ = root.Remove(tmp)
	}
}
