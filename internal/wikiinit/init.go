// Package wikiinit initializes or adopts a separate eva-md wiki repository.
package wikiinit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// Options describes an explicitly requested wiki initialization.
type Options struct {
	Remote, Dir, Version, OKFModule string
	NoPush                          bool
	Dirs                            paths.Dirs
}

// Result identifies the initialized wiki; Adopted means its schema already existed.
type Result struct {
	WikiPath, Instance string
	Adopted            bool
}

var scaffoldPaths = []string{"README.md", "index.md", "log.md", "general", ".eva-md/schema.toml", ".gitattributes", ".gitignore"}

// Run clones an empty remote or adopts an existing eva wiki, validates it,
// makes its initial commit with normal signing settings and saves local config.
// It never retries a failed signature or push, and never deletes a failed clone.
func Run(ctx context.Context, o Options) (Result, error) {
	var result Result
	home, err := os.UserHomeDir()
	if err != nil {
		return result, err
	}
	// The folder is the user's choice; there is no default location.
	if o.Dir == "" {
		return result, errors.New("wikiinit: choose a folder for the wiki")
	}
	root, err := allowedLocation(o.Dir, home, o.Dirs)
	if err != nil {
		return result, err
	}
	remote, expected, err := remoteIdentity(o.Remote)
	if err != nil {
		return result, err
	}
	if !filepath.IsAbs(o.Dirs.Config) || !filepath.IsAbs(o.Dirs.State) {
		return result, errors.New("wikiinit: config and state paths must be absolute")
	}
	unlock, err := initLock(o.Dirs.Locks())
	if err != nil {
		return result, err
	}
	defer unlock()
	previous, err := config.Load(o.Dirs.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return result, errors.New("wikiinit: existing config cannot be read; left unchanged")
	}
	if previous.WikiPath != "" && (previous.WikiPath != root || previous.ExpectedRemote != expected) {
		return result, errors.New("wikiinit: config already points to a different wiki; left unchanged")
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && Empty(entries)) {
		if err := paths.EnsureDir(filepath.Dir(root)); err != nil {
			return result, err
		}
		// git clones only into an empty folder; Finder's file is safe to go.
		if len(entries) > 0 {
			if err := os.Remove(filepath.Join(root, ".DS_Store")); err != nil {
				return result, err
			}
		}
		if _, err := git(ctx, filepath.Dir(root), "clone", "--no-local", "--", remote, root); err != nil {
			return result, err
		}
		if err := os.Chmod(root, 0o700); err != nil {
			return result, err
		}
	} else if err != nil {
		return result, fmt.Errorf("wikiinit: inspect destination: %w", err)
	}
	// An ordinary standalone clone is required. A parent repo, bare repo, or
	// worktree must never accidentally receive the scaffold or its commit.
	info, err := os.Lstat(filepath.Join(root, ".git"))
	if err != nil || !info.IsDir() {
		return result, errors.New("wikiinit: destination must be a standalone Git clone")
	}
	top, err := git(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return result, err
	}
	realTop, err := filepath.EvalSymlinks(top)
	if err != nil || realTop != root {
		return result, errors.New("wikiinit: destination is not the repository root")
	}
	origin, err := git(ctx, root, "config", "--get", "remote.origin.url")
	if err != nil {
		return result, err
	}
	_, originID, err := remoteIdentity(origin)
	if err != nil || originID != expected {
		return result, errors.New("wikiinit: origin does not match requested remote")
	}
	// Refuse a push URL that would send memory somewhere else.
	pushURLs, err := git(ctx, root, "remote", "get-url", "--push", "--all", "origin")
	if err != nil {
		return result, err
	}
	for _, pushURL := range strings.Split(pushURLs, "\n") {
		_, pushID, err := remoteIdentity(pushURL)
		if err != nil || pushID != expected {
			return result, errors.New("wikiinit: origin push URL differs from requested remote")
		}
	}
	schemaPath := filepath.Join(root, ".eva-md", "schema.toml")
	if info, err := os.Lstat(filepath.Dir(schemaPath)); err == nil {
		if !info.IsDir() {
			return result, errors.New("wikiinit: wiki metadata directory must not be a symlink")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return result, errors.New("wikiinit: cannot inspect wiki metadata directory")
	}
	schema, schemaErr := config.LoadSchema(schemaPath)
	adopt := schemaErr == nil
	if schemaErr != nil && !errors.Is(schemaErr, fs.ErrNotExist) {
		return result, errors.New("wikiinit: existing wiki schema is unreadable")
	}
	hasHead := false
	if _, err := git(ctx, root, "rev-parse", "--verify", "HEAD"); err == nil {
		hasHead = true
	}
	if !adopt {
		// A new wiki here would take the place of the one the config names.
		if previous.Instance != "" {
			return result, errors.New("wikiinit: the repo is empty, but the config names an existing wiki; push that wiki there, or move the config aside to start over")
		}
		items, err := os.ReadDir(root)
		if err != nil {
			return result, err
		}
		if len(items) != 1 || items[0].Name() != ".git" || hasHead {
			return result, errors.New("wikiinit: non-empty repository has no eva-md schema; refusing to modify it")
		}
		label, err := MachineLabel()
		if err != nil {
			return result, err
		}
		var nonce [4]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return result, err
		}
		schema = config.WikiSchema{SchemaVersion: 1, OKFSpec: "0.2", OKFModule: o.OKFModule, Instance: label + "-" + hex.EncodeToString(nonce[:]), Created: time.Now().UTC().Format(time.RFC3339)}
		if err := scaffold(root, o, schema); err != nil {
			return result, err
		}
		previous.MachineLabel = label
	} else {
		if schema.SchemaVersion != 1 || schema.OKFSpec != "0.2" || schema.Instance == "" {
			return result, errors.New("wikiinit: unsupported or incomplete wiki schema")
		}
		if previous.Instance != "" && previous.Instance != schema.Instance {
			return result, errors.New("wikiinit: wiki instance differs from existing config")
		}
	}
	w, err := okfadapter.Open(root, o.Dirs.Locks())
	if err != nil {
		return result, err
	}
	v, err := w.Validate()
	if err != nil {
		return result, err
	}
	// A fresh scaffold must have no findings at all. An adopted wiki is held
	// to what eva-md validate refuses.
	if (!adopt && len(v.Warnings) != 0) || len(v.Errors)+len(v.Orphans)+len(v.BrokenLinks) != 0 || BlockingWarning(v.Warnings) {
		return result, errors.New("wikiinit: wiki validation has findings; run eva-md validate to inspect")
	}
	if hasHead {
		status, err := git(ctx, root, "status", "--porcelain", "--untracked-files=all")
		if err != nil {
			return result, err
		}
		if status != "" {
			return result, errors.New("wikiinit: existing wiki has uncommitted changes; left untouched")
		}
	} else {
		if err := checkInitialFiles(root); err != nil {
			return result, err
		}
		args := append([]string{"add", "--"}, scaffoldPaths...)
		if _, err := git(ctx, root, args...); err != nil {
			return result, err
		}
		staged, err := git(ctx, root, "diff", "--cached", "--name-only", "-z")
		if err != nil {
			return result, err
		}
		for _, name := range strings.Split(staged, "\x00") {
			if name != "" && !isScaffoldFile(name) {
				return result, errors.New("wikiinit: unexpected staged path; refusing commit")
			}
		}
		if _, err := git(ctx, root, "commit", "-m", "Initialize eva.md wiki"); err != nil {
			return result, err
		}
	}
	if !o.NoPush {
		branch, err := git(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
		if err != nil {
			return result, errors.New("wikiinit: cannot push a detached HEAD")
		}
		if _, err := git(ctx, root, "push", "--set-upstream", "origin", "HEAD:refs/heads/"+branch); err != nil {
			return result, err
		}
	}
	previous.WikiPath, previous.ExpectedRemote, previous.Instance = root, expected, schema.Instance
	if previous.MachineLabel == "" {
		if previous.MachineLabel, err = MachineLabel(); err != nil {
			return result, err
		}
	}
	if err := config.Save(o.Dirs.ConfigFile(), previous); err != nil {
		return result, err
	}
	return Result{WikiPath: root, Instance: schema.Instance, Adopted: adopt}, nil
}

func scaffold(root string, o Options, schema config.WikiSchema) error {
	if err := okfadapter.Init(root); err != nil {
		return err
	}
	w, err := okfadapter.Open(root, o.Dirs.Locks())
	if err != nil {
		return err
	}
	actor := okfadapter.Actor("eva-md", o.Version)
	// The default thread is a page whose Now is the placeholder, as
	// threadpage renders it.
	thread := threadpage.Page{Title: "General · default", Now: threadpage.Now{Summary: threadpage.Placeholder}}
	pages := []struct{ id, typ, title, desc, body string }{
		{"README", "guide", "Eva dot MD", "How to read this private memory wiki", "# Eva dot MD\n\nMemory maintained by eva-md. Start with [General memory](general/now.md).\n\nTo protect a page from automated updates, set `locked: true` in its frontmatter. Human-verified pages are also left alone. Source references resolve through the local inbox; raw chats are never committed here.\n"},
		{"general/now", "now", "General memory", "Overview of general conversations", "# General memory\n\n## Threads\n\n* [Default thread](threads/default.md) — " + threadpage.Placeholder + "\n"},
		{"general/threads/default", "thread", thread.Title, "General conversations outside a project", threadpage.Render(thread)},
	}
	for _, p := range pages {
		c := &okfadapter.Concept{Type: p.typ, Title: p.title, Description: p.desc, Status: "stable", Body: p.body, Extra: map[string]any{"locked": false, "writer": actor}}
		if p.typ == "thread" {
			c.Extra["branch"] = "default"
			c.Extra["state"] = "active"
			c.Extra["project"] = "general"
		}
		if err := w.Create(p.id, c, actor); err != nil {
			return err
		}
	}
	files := map[string]string{
		"index.md":                 "---\nokf_version: \"0.2\"\n---\n# Eva dot MD\n\n" + okfadapter.IndexLine("Eva dot MD", "README.md", "How to read this private memory wiki") + "\n" + okfadapter.IndexLine("General", "general/index.md", "General conversations") + "\n",
		"general/index.md":         "# General\n\n" + okfadapter.IndexLine("General memory", "now.md", "Overview of general conversations") + "\n" + okfadapter.IndexLine("Threads", "threads/index.md", "Conversation threads") + "\n",
		"general/threads/index.md": "# Threads\n\n" + okfadapter.IndexLine("General · default", "default.md", "General conversations outside a project") + "\n",
		".gitattributes":           "log.md merge=union\n*.md text eol=lf\n",
		".gitignore":               "*.jsonl\n/inbox/\n.eva-md/tmp/\n.tmp-okf-*\n.tmp-eva-*\n.DS_Store\n",
	}
	for _, name := range []string{"index.md", "general/index.md", "general/threads/index.md", ".gitattributes", ".gitignore"} {
		if err := w.WriteFile(name, []byte(files[name])); err != nil {
			return err
		}
	}
	return config.SaveSchema(filepath.Join(root, ".eva-md", "schema.toml"), schema)
}

// hostname is a variable so tests can set the machine's name.
var hostname = os.Hostname

// MachineLabel is this Mac's label: the sanitized hostname, or "mac" without
// one. The instance of a wiki made here starts with it.
func MachineLabel() (string, error) {
	label, err := hostname()
	if err != nil || label == "" {
		label = "mac"
	}
	return paths.SafeName(label)
}

// MadeOn is the label of the Mac a wiki's instance was made on: the instance
// without the random part Run adds after the label. An instance not shaped
// that way is returned whole.
func MadeOn(instance string) string {
	if i := strings.LastIndexByte(instance, '-'); i > 0 && len(instance)-i-1 == 8 {
		if _, err := hex.DecodeString(instance[i+1:]); err == nil {
			return instance[:i]
		}
	}
	return instance
}

// BlockingWarning reports a warning that eva-md validate and adopting a wiki
// refuse: an invalid actor or a parent-index problem.
func BlockingWarning(warnings []string) bool {
	for _, warning := range warnings {
		lower := strings.ToLower(warning)
		if strings.Contains(lower, "actor") || strings.Contains(lower, "parent index") || strings.Contains(lower, "parent-index") {
			return true
		}
	}
	return false
}

func isScaffoldFile(name string) bool {
	switch name {
	case "README.md", "index.md", "log.md", "general/index.md", "general/now.md", "general/threads/index.md", "general/threads/default.md", ".eva-md/schema.toml", ".gitattributes", ".gitignore":
		return true
	}
	return false
}

func checkInitialFiles(root string) error {
	return filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if file == root {
			return nil
		}
		rel, _ := filepath.Rel(root, file)
		if rel == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			switch rel {
			case "general", "general/threads", ".eva-md":
				return nil
			}
			return fmt.Errorf("wikiinit: unexpected directory in initial wiki: %s", rel)
		}
		// Finder writes .DS_Store files, and the scaffold's .gitignore keeps
		// them out of the commit.
		if d.Name() == ".DS_Store" && d.Type().IsRegular() {
			return nil
		}
		if !d.Type().IsRegular() || !isScaffoldFile(filepath.ToSlash(rel)) {
			return fmt.Errorf("wikiinit: unexpected file in initial wiki: %s", rel)
		}
		return nil
	})
}

// CheckFolder reports whether dir can hold the wiki, before Run is asked to
// create it there: an absolute folder of its own, outside Documents, Desktop,
// iCloud Drive and eva-md's own folders, that is new, empty or already a git
// clone (Run then checks its remote and schema). It returns the folder's
// canonical path.
func CheckFolder(dir string, dirs paths.Dirs) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root, err := allowedLocation(dir, home, dirs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return root, nil
	}
	if err != nil {
		return "", fmt.Errorf("wikiinit: inspect folder: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("wikiinit: that path is a file, not a folder")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("wikiinit: inspect folder: %w", err)
	}
	if Empty(entries) {
		return root, nil
	}
	if git, err := os.Lstat(filepath.Join(root, ".git")); err == nil && git.IsDir() {
		return root, nil
	}
	return "", errors.New("wikiinit: the folder is not empty; choose a new or empty folder")
}

// Empty reports whether a folder's entries leave it empty for the wiki: it
// holds nothing, or only the .DS_Store file Finder leaves in a folder it
// showed.
func Empty(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.Name() != ".DS_Store" || !e.Type().IsRegular() {
			return false
		}
	}
	return true
}

// CheckRemote reports whether remote can be the wiki's remote, and returns
// the normalized identity that the config records for it.
func CheckRemote(remote string) (string, error) {
	_, id, err := remoteIdentity(remote)
	return id, err
}

func allowedLocation(dir, home string, dirs paths.Dirs) (string, error) {
	if !filepath.IsAbs(dir) || !filepath.IsAbs(home) {
		return "", errors.New("wikiinit: wiki and home paths must be absolute")
	}
	root, err := canonicalFuture(dir)
	if err != nil {
		return "", err
	}
	realHome, err := canonicalFuture(home)
	if err != nil {
		return "", err
	}
	if equalPath(root, realHome) || root == string(filepath.Separator) {
		return "", errors.New("wikiinit: the wiki needs a folder of its own, not your home folder")
	}
	for _, banned := range []struct{ path, why string }{
		{filepath.Join(home, "Documents"), "that folder is in Documents, which macOS protects from background updates; choose a folder outside it"},
		{filepath.Join(home, "Desktop"), "that folder is on the Desktop, which macOS protects from background updates; choose a folder outside it"},
		{filepath.Join(home, "Library", "Mobile Documents"), "that folder is in iCloud Drive, which macOS protects from background updates and syncs into .git; choose a folder outside it"},
		{dirs.Config, "that folder is inside eva-md's own files; choose another folder"},
		{dirs.State, "that folder is inside eva-md's own files; choose another folder"},
		{dirs.Share, "that folder is inside eva-md's own files; choose another folder"},
	} {
		if banned.path == "" {
			continue
		}
		for _, candidate := range []string{filepath.Clean(dir), root} {
			resolved, err := canonicalFuture(banned.path)
			if err != nil {
				return "", err
			}
			if within(candidate, filepath.Clean(banned.path)) || within(candidate, resolved) {
				return "", errors.New("wikiinit: " + banned.why)
			}
		}
	}
	return root, nil
}

// canonicalFuture resolves existing ancestors of a path whose leaf may not exist.
func canonicalFuture(p string) (string, error) {
	p = filepath.Clean(p)
	if _, err := os.Lstat(p); err == nil {
		return filepath.EvalSymlinks(p)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent, err := canonicalFuture(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(p)), nil
}

func within(p, root string) bool {
	if runtime.GOOS == "darwin" {
		p, root = strings.ToLower(p), strings.ToLower(root)
	}
	r, err := filepath.Rel(root, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func equalPath(a, b string) bool {
	if runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func initLock(dir string) (func(), error) {
	root, err := paths.OpenRoot(dir, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile("init.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("wikiinit: invalid initialization lock")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("wikiinit: another initialization is running")
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
