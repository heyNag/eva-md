// Package okfadapter is the only eva-md code that calls okf. It adds the
// protections okf lacks: one wiki lock around every write, no-clobber
// create, compare-and-swap updates, skipping saves that would only bump the
// timestamp, actor checks and folder fsyncs.
package okfadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/okf-memory/okf-agent-memory/pkg/okf"
)

// Errors returned by the adapter.
var (
	ErrExists   = errors.New("okfadapter: page already exists")
	ErrConflict = errors.New("okfadapter: page changed since it was read")
	ErrNoRoot   = errors.New("okfadapter: wiki root index.md missing")
)

const (
	lockName = "wiki.lock"
	// Wiki pages use okf's required mode; all directories remain private.
	fileMode = 0o644
	dirMode  = 0o700
	// tempPrefix starts the temporary files of WriteFile and Init. The wiki's
	// .gitignore lists it next to okf's .tmp-okf-*.
	tempPrefix = ".tmp-eva-"
)

// Concept is okf's page model, exposed here to keep okf imports in this package.
type Concept = okf.Concept

// Generated records a page's author and save time.
type Generated = okf.Generated

// Verified records a page verification event.
type Verified = okf.Verified

// Source records the source of a page's claims.
type Source = okf.Source

// ValidationResult contains the wiki's conformance and connectivity findings.
type ValidationResult = okf.ValidationResult

// lockTimeout bounds the wait for the wiki lock; lockPoll is how often a
// busy lock is tried again.
var (
	lockTimeout = 60 * time.Second
	lockPoll    = 10 * time.Millisecond
)

// Page is a concept page as read from disk.
type Page struct {
	ID      string
	Concept *okf.Concept
	Raw     []byte
	Hash    string // sha256 of Raw, for Update's compare-and-swap
}

// Wiki is an opened eva wiki.
type Wiki struct {
	root     string // symlink-resolved absolute path
	lockDir  string
	lockPath string
}

// Open opens the wiki at root. It resolves symlinks, requires a root
// index.md (without one okf would silently treat knowledge/ as the wiki),
// loads the bundle and checks that okf loaded root itself. Locks are taken in
// lockDir, which must be an absolute path.
func Open(root, lockDir string) (*Wiki, error) {
	if !filepath.IsAbs(lockDir) {
		return nil, fmt.Errorf("okfadapter: lock folder %q is not an absolute path", lockDir)
	}
	real, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	if _, err := loadBundle(real); err != nil {
		return nil, err
	}
	lockDir = filepath.Clean(lockDir)
	return &Wiki{root: real, lockDir: lockDir, lockPath: filepath.Join(lockDir, lockName)}, nil
}

// Root returns the wiki's absolute, symlink-resolved path.
func (w *Wiki) Root() string { return w.root }

// Read reads and parses the page with the given concept ID.
func (w *Wiki) Read(id string) (*Page, error) {
	id, rel, full, err := w.locate(id)
	if err != nil {
		return nil, err
	}
	raw, err := readNoFollow(full)
	if err != nil {
		return nil, fmt.Errorf("okfadapter: read %s: %w", rel, err)
	}
	c, err := okf.ParseConcept(rel, string(raw))
	if err != nil {
		return nil, fmt.Errorf("okfadapter: parse %s: %w", rel, err)
	}
	return &Page{ID: id, Concept: c, Raw: raw, Hash: hashOf(raw)}, nil
}

// Create writes a new page under the wiki lock. It refuses a page that
// already exists (ErrExists) and an actor okf would flag, saves through
// okf.SaveConcept with okf's own log and index updates off, and fsyncs the
// page's folder and any folder the save created. c is not modified.
func (w *Wiki) Create(id string, c *okf.Concept, actor string) error {
	if c == nil {
		return errors.New("okfadapter: create: nil concept")
	}
	unlock, err := w.lock()
	if err != nil {
		return err
	}
	defer unlock()

	id, rel, full, err := w.locate(id)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(full); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, rel)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("okfadapter: create %s: %w", rel, err)
	}
	if !okf.IsValidActor(actor) {
		return fmt.Errorf("okfadapter: create %s: invalid actor %q", rel, actor)
	}
	cp := *c
	cp.ID, cp.Path = id, rel
	// Pre-create the page folders because okf itself uses 0755.
	if err := os.MkdirAll(filepath.Dir(full), dirMode); err != nil {
		return fmt.Errorf("okfadapter: create page folder: %w", err)
	}
	opts := okf.SaveOptions{IsNew: true, AutoLog: false, AutoIndex: false, Actor: actor}
	if err := okf.SaveConcept(w.root, &cp, opts); err != nil {
		return fmt.Errorf("okfadapter: create %s: %w", rel, err)
	}
	return syncUp(w.root, filepath.Dir(full))
}

// Update rewrites an existing page under the wiki lock. It re-reads the page
// and returns ErrConflict when its hash differs from expectHash. Otherwise it
// parses a fresh copy (keeping Extra, Verified and Sources), lets render
// change it, and saves it unless the result equals the file on disk apart
// from the generated line. changed reports whether the file was written.
func (w *Wiki) Update(id, expectHash string, render func(*okf.Concept) error, actor string) (changed bool, err error) {
	if render == nil {
		return false, errors.New("okfadapter: update: nil render")
	}
	unlock, err := w.lock()
	if err != nil {
		return false, err
	}
	defer unlock()

	id, rel, full, err := w.locate(id)
	if err != nil {
		return false, err
	}
	raw, err := readNoFollow(full)
	if err != nil {
		return false, fmt.Errorf("okfadapter: update %s: %w", rel, err)
	}
	if hashOf(raw) != expectHash {
		return false, fmt.Errorf("%w: %s", ErrConflict, rel)
	}
	c, err := okf.ParseConcept(rel, string(raw))
	if err != nil {
		return false, fmt.Errorf("okfadapter: parse %s: %w", rel, err)
	}
	if err := render(c); err != nil {
		return false, fmt.Errorf("okfadapter: render %s: %w", rel, err)
	}
	if c.ID != id || c.Path != rel {
		return false, fmt.Errorf("okfadapter: render %s: changed the page id or path", rel)
	}
	if !okf.IsValidActor(actor) {
		return false, fmt.Errorf("okfadapter: update %s: invalid actor %q", rel, actor)
	}
	if withoutGenerated(serializeAsSaved(c)) == withoutGenerated(string(raw)) {
		return false, nil
	}
	opts := okf.SaveOptions{IsNew: false, AutoLog: false, AutoIndex: false, Actor: actor}
	if err := okf.SaveConcept(w.root, c, opts); err != nil {
		return false, fmt.Errorf("okfadapter: update %s: %w", rel, err)
	}
	if err := syncDir(filepath.Dir(full)); err != nil {
		return true, err
	}
	return true, nil
}

// WriteFile writes a wiki file that is not a concept page, such as an
// index.md, the root log.md or .eva-md/schema.toml. It runs under the wiki
// lock and fsyncs the folders it touched. Markdown is 0644; other files are 0600.
// Concept pages must go through Create and Update, so other .md files are
// refused, as is anything inside .git.
func (w *Wiki) WriteFile(rel string, data []byte) error {
	if err := checkFileRel(rel); err != nil {
		return err
	}
	unlock, err := w.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return writeInRoot(w.root, rel, data)
}

// Validate loads the wiki again and runs okf's validator with drift checks
// on and strict mode off.
func (w *Wiki) Validate() (*okf.ValidationResult, error) {
	real, err := resolveRoot(w.root)
	if err != nil {
		return nil, err
	}
	if real != w.root {
		return nil, errors.New("okfadapter: wiki root changed since it was opened")
	}
	b, err := loadBundle(real)
	if err != nil {
		return nil, err
	}
	return okf.Validate(b, okf.ValidateOptions{Drift: true, Strict: false}), nil
}

// Init scaffolds an okf bundle at root, creating the folder if needed. It
// writes eva-md's own log.md first, dated with the local date, so that
// okf.InitBundle creates only the root index.md. Existing files are kept.
func Init(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("okfadapter: init: %q is not an absolute path", root)
	}
	if err := os.MkdirAll(root, dirMode); err != nil {
		return fmt.Errorf("okfadapter: init: %w", err)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("okfadapter: init: %w", err)
	}
	// InitBundle follows an existing index symlink when deciding to skip it.
	// Refuse that before writing any files into an existing wiki.
	if fi, err := os.Lstat(filepath.Join(real, "index.md")); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("okfadapter: init: index.md is not a regular file")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("okfadapter: init: index.md: %w", err)
	}
	fi, err := os.Lstat(filepath.Join(real, "log.md"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		log := fmt.Sprintf("## %s\n* **Creation**: Initialized the eva-md wiki.\n", time.Now().Format("2006-01-02"))
		if err := writeInRoot(real, "log.md", []byte(log)); err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("okfadapter: init: %w", err)
	case !fi.Mode().IsRegular():
		return fmt.Errorf("okfadapter: init: %s/log.md is not a regular file", real)
	}
	if err := okf.InitBundle(real); err != nil {
		return fmt.Errorf("okfadapter: init: %w", err)
	}
	return syncDir(real)
}

// Actor returns the generated.by value for a page written by model on host:
// "<host>/<model>", with every character other than letters, digits and
// ". _ - : @ +" in either part replaced by '-' and runs of '-' collapsed, or
// "<host>/unknown" without a model. The result always passes okf.IsValidActor.
func Actor(host, model string) string {
	h := actorPart(host)
	if h == "" {
		h = "unknown"
	}
	m := actorPart(model)
	if m == "" {
		m = "unknown"
	}
	if a := h + "/" + m; okf.IsValidActor(a) {
		return a
	}
	return "eva-md/unknown"
}

const maxActorPart = 100

func actorPart(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.TrimSpace(s) {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == ':' || r == '@' || r == '+'
		if !ok {
			if !dash {
				b.WriteByte('-')
			}
			dash = true
			continue
		}
		dash = false
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) > maxActorPart {
		out = out[:maxActorPart]
	}
	return strings.Trim(out, "-")
}

// IndexLine formats one index entry in okf's line format,
// "* [Title](file) - Description". '[', ']' and ')' in the title are
// escaped, and line breaks in the title and description become spaces.
func IndexLine(title, file, description string) string {
	flat := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")
	title = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`, ")", `\)`).Replace(flat.Replace(strings.TrimSpace(title)))
	description = flat.Replace(strings.TrimSpace(description))
	if description == "" {
		return fmt.Sprintf("* [%s](%s)", title, file)
	}
	return fmt.Sprintf("* [%s](%s) - %s", title, file, description)
}

// resolveRoot returns the absolute, symlink-resolved root and checks that it
// holds a regular index.md.
func resolveRoot(root string) (string, error) {
	if root == "" {
		return "", errors.New("okfadapter: empty wiki path")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("okfadapter: wiki %s: %w", root, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("okfadapter: wiki %s: %w", root, err)
	}
	fi, err := os.Lstat(filepath.Join(real, "index.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNoRoot, real)
	}
	if err != nil {
		return "", fmt.Errorf("okfadapter: wiki %s: %w", real, err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s/index.md is not a regular file", ErrNoRoot, real)
	}
	return real, nil
}

func loadBundle(real string) (*okf.Bundle, error) {
	b, err := okf.LoadBundle(real)
	if err != nil {
		return nil, fmt.Errorf("okfadapter: load %s: %w", real, err)
	}
	if b.RootPath != real {
		return nil, fmt.Errorf("okfadapter: okf loaded %s instead of %s", b.RootPath, real)
	}
	return b, nil
}

// locate checks a concept ID and returns it without ".md", its relative path
// and its absolute path. No existing part of the path may be a symlink.
func (w *Wiki) locate(id string) (cleanID, rel, full string, err error) {
	cleanID = strings.TrimSuffix(id, ".md")
	if cleanID != strings.TrimSpace(cleanID) || strings.Contains(cleanID, `\`) || path.Clean(cleanID) != cleanID {
		return "", "", "", fmt.Errorf("okfadapter: invalid page id %q", id)
	}
	if err := okf.ValidateConceptID(cleanID); err != nil {
		return "", "", "", fmt.Errorf("okfadapter: %w", err)
	}
	rel = cleanID + ".md"
	full, err = checkNoSymlinks(w.root, rel, true)
	if err != nil {
		return "", "", "", err
	}
	return cleanID, rel, full, nil
}

// checkNoSymlinks walks rel below root and refuses any existing part that is
// a symlink, a folder that isn't one, or (when regular is set) a final part
// that isn't a regular file. It returns the absolute path.
func checkNoSymlinks(root, rel string, regular bool) (string, error) {
	if fi, err := os.Lstat(root); err != nil {
		return "", fmt.Errorf("okfadapter: wiki root: %w", err)
	} else if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return "", errors.New("okfadapter: wiki root is no longer a regular directory")
	}
	parts := strings.Split(rel, "/")
	cur := root
	for i, p := range parts {
		cur = filepath.Join(cur, p)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return filepath.Join(root, filepath.FromSlash(rel)), nil
		}
		if err != nil {
			return "", fmt.Errorf("okfadapter: %s: %w", rel, err)
		}
		last := i == len(parts)-1
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			return "", fmt.Errorf("okfadapter: %s: %s is a symlink", rel, path.Join(parts[:i+1]...))
		case !last && !fi.IsDir():
			return "", fmt.Errorf("okfadapter: %s: %s is not a folder", rel, path.Join(parts[:i+1]...))
		case last && regular && !fi.Mode().IsRegular():
			return "", fmt.Errorf("okfadapter: %s is not a regular file", rel)
		}
	}
	return cur, nil
}

// checkFileRel accepts the relative paths WriteFile may write.
func checkFileRel(rel string) error {
	bad := rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) ||
		path.Clean(rel) != rel || rel == "." || strings.HasPrefix(rel, "../") || rel == ".."
	if !bad {
		for _, r := range rel {
			if r < 0x20 || r == 0x7f {
				bad = true
			}
		}
	}
	if bad {
		return fmt.Errorf("okfadapter: invalid wiki path %q", rel)
	}
	for _, p := range strings.Split(rel, "/") {
		if strings.EqualFold(p, ".git") {
			return fmt.Errorf("okfadapter: refusing to write inside .git: %q", rel)
		}
	}
	base := path.Base(rel)
	if strings.HasSuffix(strings.ToLower(base), ".md") && base != "index.md" && rel != "log.md" {
		return fmt.Errorf("okfadapter: %q is a concept page; use Create or Update", rel)
	}
	return nil
}

// writeInRoot atomically writes rel below root and fsyncs the folders up to
// root.
func writeInRoot(root, rel string, data []byte) error {
	full, err := checkNoSymlinks(root, rel, true)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("okfadapter: write %s: %w", rel, err)
	}
	mode := os.FileMode(0o600)
	if strings.HasSuffix(rel, ".md") {
		mode = fileMode
	}
	if err := writeAtomic(full, data, mode); err != nil {
		return fmt.Errorf("okfadapter: write %s: %w", rel, err)
	}
	return syncUp(root, dir)
}

func writeAtomic(target string, data []byte, mode os.FileMode) (err error) {
	f, err := os.CreateTemp(filepath.Dir(target), tempPrefix+"*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func readNoFollow(full string) ([]byte, error) {
	f, err := os.OpenFile(full, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return io.ReadAll(f)
}

func hashOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// serializeAsSaved serializes c the way SaveConcept would, apart from the
// generated line.
func serializeAsSaved(c *okf.Concept) string {
	cp := *c
	cp.Type = strings.TrimSpace(cp.Type)
	cp.Title = strings.TrimSpace(cp.Title)
	if cp.Description != "" {
		cp.Description = strings.TrimSpace(cp.Description)
	}
	return okf.SerializeConcept(&cp)
}

// withoutGenerated drops the generated entry, in flow or block form, from a
// page's frontmatter.
func withoutGenerated(s string) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) == 0 || lines[0] != "---\n" {
		return s
	}
	var b strings.Builder
	b.WriteString(lines[0])
	inFront, skipping := true, false
	for _, l := range lines[1:] {
		if inFront {
			if strings.TrimRight(l, "\r\n") == "---" {
				inFront = false
			} else if skipping && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) {
				continue
			} else if skipping = strings.HasPrefix(l, "generated:"); skipping {
				continue
			}
		}
		b.WriteString(l)
	}
	return b.String()
}

// lock takes the exclusive wiki lock, waiting at most lockTimeout.
func (w *Wiki) lock() (unlock func(), err error) {
	if err := os.MkdirAll(w.lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("okfadapter: lock folder: %w", err)
	}
	if fi, err := os.Lstat(w.lockDir); err != nil {
		return nil, fmt.Errorf("okfadapter: lock folder: %w", err)
	} else if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("okfadapter: lock folder is not a regular directory")
	}
	f, err := os.OpenFile(w.lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("okfadapter: open wiki lock: %w", err)
	}
	if fi, err := f.Stat(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("okfadapter: inspect wiki lock: %w", err)
	} else if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("okfadapter: wiki lock is not a regular file")
	}
	deadline := time.Now().Add(lockTimeout)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("okfadapter: wiki lock: %w", err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("okfadapter: wiki lock still busy after %s", lockTimeout)
		}
		time.Sleep(lockPoll)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("okfadapter: open folder %s: %w", dir, err)
	}
	serr := d.Sync()
	cerr := d.Close()
	if serr != nil {
		return fmt.Errorf("okfadapter: fsync folder %s: %w", dir, serr)
	}
	if cerr != nil {
		return fmt.Errorf("okfadapter: close folder %s: %w", dir, cerr)
	}
	return nil
}

// syncUp fsyncs dir and each parent up to and including root, so folders a
// save created are durable too.
func syncUp(root, dir string) error {
	for {
		if err := syncDir(dir); err != nil {
			return err
		}
		if dir == root || len(dir) <= len(root) {
			return nil
		}
		dir = filepath.Dir(dir)
	}
}
