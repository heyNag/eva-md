// Package okfadapter is the only eva-md code that calls okf. It adds the
// protections okf lacks: one wiki lock around every write, which a killed
// process can't leave behind, no-clobber create, compare-and-swap updates,
// skipping saves that would only bump the timestamp, actor checks and
// folder fsyncs.
package okfadapter

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/okf-memory/okf-agent-memory/pkg/okf"
)

// Errors returned by the adapter.
var (
	ErrExists   = errors.New("wiki: page already exists")
	ErrConflict = errors.New("wiki: page changed since it was read")
	ErrNoRoot   = errors.New("wiki root index.md missing")
	ErrLockBusy = errors.New("wiki lock still busy")
)

const (
	lockName = "wiki.lock"
	// Wiki pages use okf's required mode; all directories remain private.
	fileMode = 0o644
	dirMode  = 0o700
	// tempPrefix starts the temporary files of WriteFile and Init. The wiki's
	// .gitignore line .tmp-* covers them and okf's .tmp-okf-*.
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

// LockTimeout bounds the wait for the wiki lock; lockPoll is how often a
// busy lock is tried again. Tests raise LockTimeout before any test runs.
var (
	LockTimeout = 60 * time.Second
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
	held     bool             // the wiki lock is held for this Wiki (see Locked)
	lockWait time.Duration    // when set, the wait for the wiki lock in place of LockTimeout; only tests set it
	now      func() time.Time // when set, the clock log.md's dates come from; only tests set it
}

// Open opens the wiki at root. It resolves symlinks and requires a root
// index.md (without one okf would silently treat knowledge/ as the wiki). It
// reads no pages, so a hook that opens the wiki pays the same at any wiki
// size and a stray file elsewhere can't fail it; Validate loads the whole
// wiki. Locks are taken in lockDir, which must be an absolute path.
func Open(root, lockDir string) (*Wiki, error) {
	if !filepath.IsAbs(lockDir) {
		return nil, fmt.Errorf("wiki: lock folder %q is not an absolute path", lockDir)
	}
	real, err := resolveRoot(root)
	if err != nil {
		return nil, err
	}
	lockDir = filepath.Clean(lockDir)
	return &Wiki{root: real, lockDir: lockDir, lockPath: filepath.Join(lockDir, lockName)}, nil
}

// Root returns the wiki's absolute, symlink-resolved path.
func (w *Wiki) Root() string { return w.root }

// Locked runs fn under the wiki lock, so several writes land together. The
// Wiki fn gets writes without taking the lock again; it must not be used
// once fn returns.
func (w *Wiki) Locked(fn func(*Wiki) error) error {
	unlock, err := w.lock()
	if err != nil {
		return err
	}
	defer unlock()
	held := *w
	held.held = true
	return fn(&held)
}

// Read reads and parses the page with the given concept ID.
func (w *Wiki) Read(id string) (*Page, error) { return w.ReadMax(id, 0) }

// ErrTooLarge reports a page over the size a bounded read allows.
var ErrTooLarge = errors.New("wiki: page is over the read limit")

// ReadMax reads and parses a page as Read does, but refuses one over max
// bytes (ErrTooLarge) without reading it, so a hook reads bounded data. A
// max of 0 sets no limit.
func (w *Wiki) ReadMax(id string, max int64) (*Page, error) {
	id, rel, full, err := w.locate(id)
	if err != nil {
		return nil, err
	}
	raw, err := readNoFollow(full, max)
	if err != nil {
		return nil, fmt.Errorf("wiki: read %s: %w", rel, err)
	}
	c, err := okf.ParseConcept(rel, string(raw))
	if err != nil {
		return nil, fmt.Errorf("wiki: parse %s: %w", rel, err)
	}
	return &Page{ID: id, Concept: c, Raw: raw, Hash: hashOf(raw)}, nil
}

// Create writes a new page under the wiki lock. It refuses a page that
// already exists (ErrExists) and an actor okf would flag, saves through
// okf.SaveConcept with okf's own log and index updates off, and fsyncs the
// page's folder and any folder the save created. c is not modified.
func (w *Wiki) Create(id string, c *okf.Concept, actor string) error {
	if c == nil {
		return errors.New("wiki: create: nil page")
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
		return fmt.Errorf("wiki: create %s: %w", rel, err)
	}
	if !okf.IsValidActor(actor) {
		return fmt.Errorf("wiki: create %s: invalid actor %q", rel, actor)
	}
	cp := *c
	cp.ID, cp.Path = id, rel
	// Pre-create the page folders because okf itself uses 0755.
	if err := os.MkdirAll(filepath.Dir(full), dirMode); err != nil {
		return fmt.Errorf("wiki: create page folder: %w", err)
	}
	opts := okf.SaveOptions{IsNew: true, AutoLog: false, AutoIndex: false, Actor: actor}
	if err := clearOkfLock(w.root); err != nil {
		return fmt.Errorf("wiki: create %s: %w", rel, okfFailure(err))
	}
	if err := okf.SaveConcept(w.root, &cp, opts); err != nil {
		return fmt.Errorf("wiki: create %s: %w", rel, okfFailure(err))
	}
	if err := syncUp(w.root, filepath.Dir(full)); err != nil {
		return err
	}
	w.logChange("Creation", fmt.Sprintf("Documented concept `%s` (%s).", rel, cp.Title))
	return nil
}

// Update rewrites an existing page under the wiki lock. It re-reads the page
// and returns ErrConflict when its hash differs from expectHash. Otherwise it
// parses a fresh copy (keeping Extra, Verified and Sources), lets render
// change it, and saves it unless the result equals the file on disk apart
// from the generated line. changed reports whether the file was written.
func (w *Wiki) Update(id, expectHash string, render func(*okf.Concept) error, actor string) (changed bool, err error) {
	if render == nil {
		return false, errors.New("wiki: update: nil render")
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
	raw, err := readNoFollow(full, 0)
	if err != nil {
		return false, fmt.Errorf("wiki: update %s: %w", rel, err)
	}
	if hashOf(raw) != expectHash {
		return false, fmt.Errorf("%w: %s", ErrConflict, rel)
	}
	c, err := okf.ParseConcept(rel, string(raw))
	if err != nil {
		return false, fmt.Errorf("wiki: parse %s: %w", rel, err)
	}
	if err := render(c); err != nil {
		return false, fmt.Errorf("wiki: render %s: %w", rel, err)
	}
	if c.ID != id || c.Path != rel {
		return false, fmt.Errorf("wiki: render %s: changed the page id or path", rel)
	}
	if !okf.IsValidActor(actor) {
		return false, fmt.Errorf("wiki: update %s: invalid actor %q", rel, actor)
	}
	if withoutGenerated(serializeAsSaved(c)) == withoutGenerated(string(raw)) {
		return false, nil
	}
	opts := okf.SaveOptions{IsNew: false, AutoLog: false, AutoIndex: false, Actor: actor}
	if err := clearOkfLock(w.root); err != nil {
		return false, fmt.Errorf("wiki: update %s: %w", rel, okfFailure(err))
	}
	if err := okf.SaveConcept(w.root, c, opts); err != nil {
		return false, fmt.Errorf("wiki: update %s: %w", rel, okfFailure(err))
	}
	if err := syncDir(filepath.Dir(full)); err != nil {
		return true, err
	}
	w.logChange("Update", fmt.Sprintf("Updated concept `%s`.", rel))
	return true, nil
}

// maxLog bounds the log.md logChange reads and writes again.
const maxLog = 16 << 20

// logChange records a page write in log.md as okf's own log words it,
// "* **<kind>**: <text>", under a group of the local date, "## YYYY-MM-DD",
// at the end of the file: eva-md's log runs oldest first, so the union merge
// .gitattributes sets keeps both sides' lines. The same line is kept once a
// day. It runs under the wiki lock Create and Update hold. A log that is
// missing, is no regular file, is over maxLog or can't be written is left as
// it is: the page write stands, and git keeps the history anyway.
func (w *Wiki) logChange(kind, text string) {
	raw, err := readNoFollow(filepath.Join(w.root, "log.md"), maxLog)
	if err != nil {
		return
	}
	now := time.Now
	if w.now != nil {
		now = w.now
	}
	entry := strings.Join(strings.Fields("* **"+kind+"**: "+text), " ")
	if data := appendLog(string(raw), now().Format("2006-01-02"), entry); data != string(raw) {
		_ = writeInRoot(w.root, "log.md", []byte(data))
	}
}

// appendLog adds entry to log's last group when that group is day's and
// doesn't hold it yet, or ends log with a new group for day.
func appendLog(log, day, entry string) string {
	heading := "## " + day
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	last := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## ") {
			last = i
		}
	}
	if last >= 0 && strings.TrimRight(lines[last], "\r") == heading {
		for _, l := range lines[last+1:] {
			if strings.TrimRight(l, "\r") == entry {
				return log
			}
		}
		return strings.TrimRight(log, "\n") + "\n" + entry + "\n"
	}
	if strings.TrimSpace(log) == "" {
		return heading + "\n" + entry + "\n"
	}
	return strings.TrimRight(log, "\n") + "\n\n" + heading + "\n" + entry + "\n"
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

// RemoveFile removes, under the wiki lock, a wiki file that is not a concept
// page, such as an index eva-md builds that no page calls for any more, and
// fsyncs its folder. A file already gone is no error. It refuses what
// WriteFile refuses, a path through a symlink, and anything but a regular
// file.
func (w *Wiki) RemoveFile(rel string) error {
	if err := checkFileRel(rel); err != nil {
		return err
	}
	unlock, err := w.lock()
	if err != nil {
		return err
	}
	defer unlock()
	full, err := checkNoSymlinks(w.root, rel, true)
	if err != nil {
		return err
	}
	if err := os.Remove(full); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("wiki: remove %s: %w", rel, err)
	}
	return syncDir(filepath.Dir(full))
}

// RemoveLeftoverLock removes, under the wiki lock, the lock file a save
// killed midway leaves in the wiki's root, as the next save would. It is
// never a change of yours, so a check for changes not yet committed calls
// this first: git doesn't ignore the file, and a commit of everything, as
// setup advises for such changes, would take it.
func (w *Wiki) RemoveLeftoverLock() error {
	unlock, err := w.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if err := clearOkfLock(w.root); err != nil {
		return fmt.Errorf("wiki: %w", okfFailure(err))
	}
	return nil
}

// Validate loads the wiki again and runs okf's validator with drift checks
// on and strict mode off.
func (w *Wiki) Validate() (*okf.ValidationResult, error) {
	real, err := resolveRoot(w.root)
	if err != nil {
		return nil, err
	}
	if real != w.root {
		return nil, errors.New("wiki root changed since it was opened")
	}
	b, err := loadBundle(real)
	if err != nil {
		return nil, err
	}
	return okf.Validate(b, okf.ValidateOptions{Drift: true, Strict: false}), nil
}

// RootIndex is the root index.md Init writes, which a new wiki's scaffold
// then writes again in full.
const RootIndex = "# Eva dot MD\n\n"

// Init scaffolds an okf bundle at root, creating the folder if needed. It
// writes eva-md's own log.md, dated with the local date, and root index
// first, so that okf.InitBundle, whose own index names okf, creates
// neither. Existing files are kept.
func Init(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("wiki: init: %q is not an absolute path", root)
	}
	if err := os.MkdirAll(root, dirMode); err != nil {
		return fmt.Errorf("wiki: init: %w", err)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("wiki: init: %w", err)
	}
	// InitBundle follows an existing index symlink when deciding to skip it.
	// Refuse that before writing any files into an existing wiki.
	fi, err := os.Lstat(filepath.Join(real, "index.md"))
	index := errors.Is(err, fs.ErrNotExist)
	switch {
	case index:
	case err != nil:
		return fmt.Errorf("wiki: init: index.md: %w", err)
	case !fi.Mode().IsRegular():
		return fmt.Errorf("wiki: init: index.md is not a regular file")
	}
	fi, err = os.Lstat(filepath.Join(real, "log.md"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		log := fmt.Sprintf("## %s\n* **Creation**: Initialized the eva-md wiki.\n", time.Now().Format("2006-01-02"))
		if err := writeInRoot(real, "log.md", []byte(log)); err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("wiki: init: %w", err)
	case !fi.Mode().IsRegular():
		return fmt.Errorf("wiki: init: %s/log.md is not a regular file", real)
	}
	if index {
		if err := writeInRoot(real, "index.md", []byte(RootIndex)); err != nil {
			return err
		}
	}
	if err := clearOkfLock(real); err != nil {
		return fmt.Errorf("wiki: init: %w", okfFailure(err))
	}
	if err := okf.InitBundle(real); err != nil {
		return fmt.Errorf("wiki: init: %w", okfFailure(err))
	}
	return syncDir(real)
}

// okfTemp starts the names of the temporary files okf's saves write.
const okfTemp = ".tmp-okf-"

// okfLock is the lock file okf holds in the wiki's root for the length of
// each save and of InitBundle.
const okfLock = ".okf-write.lock"

// clearOkfLock removes okf's lock file from root before an okf call that
// takes it. okf removes the file when the call returns, so one that is there
// was left by a process killed midway. okf would make each save wait up to
// 10 seconds for it, then refuse the save, until the file is 30 seconds
// old, and from then on try to remove it itself, again and again with no
// pause and no end when it can't. So when the file is there and can't be
// removed, as in a wiki folder eva-md can't write, the error is returned
// and the caller doesn't call okf. Only eva-md writes the wiki through okf,
// and only under its own lock (init's, <locks>/init.lock, for Init), which
// the kernel lets go of when a process dies, so no okf call can hold the
// file when this runs.
func clearOkfLock(root string) error {
	if err := os.Remove(filepath.Join(root, okfLock)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// okfFailure is err, from an okf call, said in eva-md's words when the step
// that failed was on a file whose name names okf, its temporary file or its
// lock file: the step and its error, which stays wrapped, so an errno still
// tells a file system failure from a page okf refused. A lock file okf
// waited for in vain is ErrLockBusy, as a busy wiki lock is.
func okfFailure(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		switch base := filepath.Base(pe.Path); {
		case strings.HasPrefix(base, okfTemp):
			return fmt.Errorf("%s a temporary file: %w", pe.Op, pe.Err)
		case base == okfLock:
			return fmt.Errorf("%s the save's lock file: %w", pe.Op, pe.Err)
		}
	}
	var le *os.LinkError
	if errors.As(err, &le) && strings.HasPrefix(filepath.Base(le.Old), okfTemp) {
		return fmt.Errorf("%s a temporary file: %w", le.Op, le.Err)
	}
	// okf quotes the lock file's path in its errors, so a page whose name
	// only holds the lock file's never matches.
	if strings.Contains(err.Error(), string(filepath.Separator)+okfLock+`"`) {
		return fmt.Errorf("%w: a save's lock file stayed in the wiki", ErrLockBusy)
	}
	return err
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
		return "", errors.New("wiki: empty path")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("wiki %s: %w", root, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("wiki %s: %w", root, err)
	}
	fi, err := os.Lstat(filepath.Join(real, "index.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNoRoot, real)
	}
	if err != nil {
		return "", fmt.Errorf("wiki %s: %w", real, err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s/index.md is not a regular file", ErrNoRoot, real)
	}
	return real, nil
}

func loadBundle(real string) (*okf.Bundle, error) {
	b, err := okf.LoadBundle(real)
	if err != nil {
		return nil, fmt.Errorf("wiki: load %s: %w", real, err)
	}
	if b.RootPath != real {
		return nil, fmt.Errorf("wiki: loaded %s instead of %s", b.RootPath, real)
	}
	return b, nil
}

// locate checks a concept ID and returns it without ".md", its relative path
// and its absolute path. No existing part of the path may be a symlink.
func (w *Wiki) locate(id string) (cleanID, rel, full string, err error) {
	cleanID = strings.TrimSuffix(id, ".md")
	if cleanID != strings.TrimSpace(cleanID) || strings.Contains(cleanID, `\`) || path.Clean(cleanID) != cleanID {
		return "", "", "", fmt.Errorf("wiki: invalid page id %q", id)
	}
	if err := okf.ValidateConceptID(cleanID); err != nil {
		return "", "", "", fmt.Errorf("wiki: %w", err)
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
		return "", fmt.Errorf("wiki root: %w", err)
	} else if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return "", errors.New("wiki root is no longer a regular directory")
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
			return "", fmt.Errorf("wiki: %s: %w", rel, err)
		}
		last := i == len(parts)-1
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			return "", fmt.Errorf("wiki: %s: %s is a symlink", rel, path.Join(parts[:i+1]...))
		case !last && !fi.IsDir():
			return "", fmt.Errorf("wiki: %s: %s is not a folder", rel, path.Join(parts[:i+1]...))
		case last && regular && !fi.Mode().IsRegular():
			return "", fmt.Errorf("wiki: %s is not a regular file", rel)
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
		return fmt.Errorf("wiki: invalid path %q", rel)
	}
	for _, p := range strings.Split(rel, "/") {
		if strings.EqualFold(p, ".git") {
			return fmt.Errorf("wiki: refusing to write inside .git: %q", rel)
		}
	}
	base := path.Base(rel)
	if strings.HasSuffix(strings.ToLower(base), ".md") && base != "index.md" && rel != "log.md" {
		return fmt.Errorf("wiki: %q is a page; use Create or Update", rel)
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
		return fmt.Errorf("wiki: write %s: %w", rel, err)
	}
	mode := os.FileMode(0o600)
	if strings.HasSuffix(rel, ".md") {
		mode = fileMode
	}
	if err := writeAtomic(full, data, mode); err != nil {
		return fmt.Errorf("wiki: write %s: %w", rel, err)
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

func readNoFollow(full string, max int64) ([]byte, error) {
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
	if max <= 0 {
		return io.ReadAll(f)
	}
	if fi.Size() > max {
		return nil, ErrTooLarge
	}
	// The page may grow between the size check and the read.
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(raw)) > max {
		return nil, ErrTooLarge
	}
	return raw, err
}

func hashOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Serialize returns the text Create and Update save for c, with the
// generated line c holds, so a writer can check a page as it will be saved.
func Serialize(c *Concept) string { return serializeAsSaved(c) }

// IsLocked reports a page a person locked: its frontmatter sets locked to
// true, yes or on, in any case, as YAML reads true.
func IsLocked(c *Concept) bool {
	switch v := c.Extra["locked"].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "on":
			return true
		}
	}
	return false
}

// IsVerified reports a page a person verified: okf read a verified entry,
// or its frontmatter has a top-level verified key with a value that isn't
// empty, inline or in the lines under it. okf reads entries only from a
// flow mapping or a list of mappings, and drops the key when it writes a
// page, so verified: true, a plain value or a block mapping count too.
func IsVerified(pg *Page) bool {
	return len(pg.Concept.Verified) > 0 || VerifiedIn(pg.Raw)
}

// VerifiedIn reports whether a page's raw text has a top-level verified key
// in its frontmatter with a value that isn't empty: an inline value other
// than an empty string, null, ~, [] or {}, or a line under the key that
// isn't blank.
// Its keys are read as okf reads them, unquoted.
func VerifiedIn(raw []byte) bool {
	front, _, ok := okf.ExtractFrontmatter(string(raw))
	if !ok {
		return false
	}
	in := false // the lines under a verified key
	for _, l := range strings.Split(front, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") || in && strings.HasPrefix(l, "-") {
			if in {
				return true
			}
			continue
		}
		key, value, ok := strings.Cut(l, ":")
		in = false
		if !ok || strings.Trim(strings.TrimSpace(key), `"'`) != "verified" {
			continue
		}
		switch v := strings.TrimSpace(value); v {
		case "", `""`, "''", "null", "~", "[]", "{}":
			in = v == ""
		default:
			return true
		}
	}
	return false
}

// reservedKey is a line okf refuses inside a --- block of a body: a key okf
// checks, quoted or not, before a colon, in any case.
var reservedKey = regexp.MustCompile(`(?i)^\s*["']?(verified|governance|generated|type|status|code_refs|stale_after)["']?\s*:`)

// SmugglesFrontmatter reports a body okf's save refuses: a line that trims
// to --- opens or closes a block, and a line in such a block, once trimmed,
// starts with a reserved key and a colon, code fences or not. A writer that
// keeps a person's text checks it before the write, so the save never fails
// on it.
func SmugglesFrontmatter(body string) bool {
	in := false
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "---":
			in = !in
		case in && t != "" && reservedKey.MatchString(t):
			return true
		}
	}
	return false
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

// lock takes the exclusive wiki lock, waiting at most LockTimeout, or
// lockWait when it is set. Under Locked the lock is already held, so it
// takes nothing.
func (w *Wiki) lock() (unlock func(), err error) {
	return w.lockWithin(cmp.Or(w.lockWait, LockTimeout))
}

// lockWithin is lock with the wait bounded by timeout.
func (w *Wiki) lockWithin(timeout time.Duration) (unlock func(), err error) {
	if w.held {
		return func() {}, nil
	}
	if err := os.MkdirAll(w.lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("wiki: lock folder: %w", err)
	}
	if fi, err := os.Lstat(w.lockDir); err != nil {
		return nil, fmt.Errorf("wiki: lock folder: %w", err)
	} else if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("wiki: lock folder is not a regular directory")
	}
	f, err := os.OpenFile(w.lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("wiki: open lock: %w", err)
	}
	if fi, err := f.Stat(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("wiki: inspect lock: %w", err)
	} else if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("wiki lock is not a regular file")
	}
	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("wiki lock: %w", err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%w after %s", ErrLockBusy, timeout)
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
		return fmt.Errorf("wiki: open folder %s: %w", dir, err)
	}
	serr := d.Sync()
	cerr := d.Close()
	if serr != nil {
		return fmt.Errorf("wiki: fsync folder %s: %w", dir, serr)
	}
	if cerr != nil {
		return fmt.Errorf("wiki: close folder %s: %w", dir, cerr)
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
