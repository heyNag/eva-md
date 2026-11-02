package install

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// RecordFile, in the data folder, is the install record: everything eva-md
// made on this Mac, so uninstall can say what it removes and what it keeps.
const RecordFile = "install-record.json"

// The kinds of a record's items.
const (
	KindBinary      = "binary"      // the stable binary
	KindMarketplace = "marketplace" // a host's generated marketplace folder
	KindPlugin      = "plugin"      // a host's registration of eva-md@eva-md
	KindPathLink    = "path_link"   // the link on PATH to the stable binary
	KindConfig      = "config"      // config.toml
	KindAnswers     = "answers"     // config.env
	KindFolder      = "folder"      // one of eva-md's own folders
	KindWiki        = "wiki"        // the wiki folder, always kept
	KindWikiRemote  = "wiki_remote" // where the wiki is pushed, always kept
)

// Record lists what eva-md made on this Mac.
type Record struct {
	Format int          `json:"format"`
	Items  []RecordItem `json:"items"`
}

// RecordItem is one thing eva-md made. Kept marks what uninstall never
// removes: the wiki folder and its remote.
type RecordItem struct {
	Kind  string `json:"kind"`
	Host  string `json:"host,omitempty"`
	Path  string `json:"path,omitempty"`
	Value string `json:"value,omitempty"`
	Kept  bool   `json:"kept,omitempty"`
}

func (i RecordItem) key() string { return i.Kind + "\x00" + i.Host + "\x00" + i.Path }

// ReadRecord reads the install record. A record that is missing or can't be
// read gives ok false, and uninstall falls back to the known layout.
func ReadRecord(dirs paths.Dirs) (Record, bool) {
	root, err := paths.OpenRoot(dirs.Share, false)
	if err != nil {
		return Record{}, false
	}
	defer root.Close()
	data, err := readAt(root, RecordFile, 256<<10)
	if err != nil {
		return Record{}, false
	}
	var r Record
	if json.Unmarshal(data, &r) != nil || r.Format != 1 {
		return Record{}, false
	}
	return r, true
}

// Find returns the record's first item of kind, for host when host is set.
func (r Record) Find(kind, host string) (RecordItem, bool) {
	for _, i := range r.Items {
		if i.Kind == kind && (host == "" || i.Host == host) {
			return i, true
		}
	}
	return RecordItem{}, false
}

// writeRecord saves r in the data folder, 0600 and atomically. The caller
// holds the install lock.
func writeRecord(ctx context.Context, root *os.Root, r Record) error {
	r.Format = 1
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomicAt(ctx, root, RecordFile, append(data, '\n'), 0o600)
}

// addToRecord adds items to the record, replacing an item of the same kind,
// host and path, and keeping every other.
func addToRecord(ctx context.Context, root *os.Root, dirs paths.Dirs, items []RecordItem) error {
	r, _ := ReadRecord(dirs)
	for _, item := range items {
		replaced := false
		for n, old := range r.Items {
			if old.key() == item.key() {
				r.Items[n], replaced = item, true
				break
			}
		}
		if !replaced {
			r.Items = append(r.Items, item)
		}
	}
	return writeRecord(ctx, root, r)
}

// installedItems is what an install made or found that eva-md made: the
// stable binary, each host's marketplace and plugin, the config files that
// exist, eva-md's folders, and the wiki and its remote as items kept.
func installedItems(dirs paths.Dirs, binary string, hosts []string) []RecordItem {
	items := []RecordItem{{Kind: KindBinary, Path: binary}}
	for _, host := range hosts {
		items = append(items,
			RecordItem{Kind: KindMarketplace, Host: host, Path: marketplacePath(dirs.Share, host)},
			RecordItem{Kind: KindPlugin, Host: host, Value: "eva-md@eva-md"})
	}
	items = append(items, configItems(dirs)...)
	for _, folder := range []string{dirs.Config, dirs.Share, dirs.State, dirs.Logs(), dirs.Cache()} {
		items = append(items, RecordItem{Kind: KindFolder, Path: folder})
	}
	if c, err := config.Load(dirs.ConfigFile()); err == nil && c.WikiPath != "" {
		items = append(items, RecordItem{Kind: KindWiki, Path: c.WikiPath, Kept: true})
		if c.ExpectedRemote != "" {
			items = append(items, RecordItem{Kind: KindWikiRemote, Value: c.ExpectedRemote, Kept: true})
		}
	}
	return items
}

// configItems lists config.toml and config.env, those that exist.
func configItems(dirs paths.Dirs) []RecordItem {
	var items []RecordItem
	for _, f := range []struct{ kind, path string }{{KindConfig, dirs.ConfigFile()}, {KindAnswers, dirs.AnswersFile()}} {
		if info, err := os.Lstat(f.path); err == nil && info.Mode().IsRegular() {
			items = append(items, RecordItem{Kind: f.kind, Path: f.path})
		}
	}
	return items
}

// RecordConfigFiles adds config.toml and config.env to the install record
// when they exist and the record lacks them. Setup's command calls it as it
// ends: on a Mac set up before there was a config.env, setup writes that
// file after the install it runs has written the record. Without a record
// it writes none, and the next install lists both.
func RecordConfigFiles(ctx context.Context, dirs paths.Dirs) error {
	missing := func() []RecordItem {
		r, ok := ReadRecord(dirs)
		if !ok {
			return nil
		}
		var items []RecordItem
		for _, item := range configItems(dirs) {
			if !r.has(item) {
				items = append(items, item)
			}
		}
		return items
	}
	if len(missing()) == 0 {
		return nil
	}
	release, err := lock(ctx, dirs.Locks())
	if err != nil {
		return err
	}
	defer release()
	// Read again under the lock: an uninstall may have changed the record.
	items := missing()
	if len(items) == 0 {
		return nil
	}
	root, err := paths.OpenRoot(dirs.Share, false)
	if err != nil {
		return err
	}
	defer root.Close()
	return addToRecord(ctx, root, dirs, items)
}

// has reports whether r lists an item of item's kind, host and path.
func (r Record) has(item RecordItem) bool {
	for _, i := range r.Items {
		if i.key() == item.key() {
			return true
		}
	}
	return false
}

// LinkPath is where install links the stable binary on PATH:
// ~/.local/bin/eva-md, beside the data folder's ~/.local/share.
func LinkPath(dirs paths.Dirs) string {
	return filepath.Join(filepath.Dir(filepath.Dir(dirs.Share)), "bin", "eva-md")
}

// linkPlan says whether install should link link to binary: when link's
// folder is a folder on PATH and holds no eva-md, or holds eva-md's own link
// already (done). Anything else leaves PATH alone.
func linkPlan(link, binary string) (want, done bool) {
	folder := filepath.Dir(link)
	real, _ := filepath.EvalSymlinks(folder)
	onPath := false
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		// A PATH entry may name the folder through a link, as /var does
		// /private/var.
		resolved, _ := filepath.EvalSymlinks(dir)
		if filepath.Clean(dir) == folder || real != "" && resolved == real {
			onPath = true
		}
	}
	if info, err := os.Stat(folder); !onPath || err != nil || !info.IsDir() {
		return false, false
	}
	info, err := os.Lstat(link)
	if errors.Is(err, fs.ErrNotExist) {
		return true, false
	}
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		if to, err := os.Readlink(link); err == nil && to == binary {
			return true, true
		}
	}
	return false, false
}

// makeLink links link to binary through a temporary link renamed into place,
// so a link that is half made is never on PATH.
func makeLink(link, binary string) error {
	tmp := filepath.Join(filepath.Dir(link), paths.TempPrefix+"link")
	_ = os.Remove(tmp)
	if err := os.Symlink(binary, tmp); err != nil {
		return err
	}
	// The folder is checked again just before the rename: eva-md never
	// replaces another program's eva-md.
	if info, err := os.Lstat(link); err == nil && (info.Mode()&os.ModeSymlink == 0 || !ownLink(link, binary)) {
		_ = os.Remove(tmp)
		return errors.New("another eva-md is there")
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ownLink reports whether link is a link to binary.
func ownLink(link, binary string) bool {
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	to, err := os.Readlink(link)
	return err == nil && to == binary
}
