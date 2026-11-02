package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
)

// Install records what it made, 0600: the stable binary, each host's
// marketplace and plugin, config.toml and config.env when they exist,
// eva-md's folders, and the wiki and its remote as kept. A later install
// adds to the record without repeating an item; a dry run writes none.
func TestInstallRecordsWhatItMade(t *testing.T) {
	opts, f := setup(t)
	ctx := context.Background()
	opts.DryRun = true
	if _, err := runInstall(ctx, opts, f.run); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadRecord(opts.Dirs); ok {
		t.Fatal("a dry run wrote the record")
	}
	opts.DryRun = false
	if _, err := runInstall(ctx, opts, f.run); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(opts.Dirs.Share, RecordFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the record: %v %v", info, err)
	}
	r, ok := ReadRecord(opts.Dirs)
	if !ok {
		t.Fatal("the record can't be read")
	}
	c, _ := config.Load(opts.Dirs.ConfigFile())
	for _, want := range []RecordItem{
		{Kind: KindBinary, Path: filepath.Join(opts.Dirs.Share, "bin", "eva-md")},
		{Kind: KindMarketplace, Host: "claude", Path: marketplacePath(opts.Dirs.Share, "claude")},
		{Kind: KindPlugin, Host: "codex", Value: "eva-md@eva-md"},
		{Kind: KindConfig, Path: opts.Dirs.ConfigFile()},
		{Kind: KindFolder, Path: opts.Dirs.Logs()},
		{Kind: KindFolder, Path: opts.Dirs.Cache()},
		{Kind: KindWiki, Path: c.WikiPath, Kept: true},
		{Kind: KindWikiRemote, Value: "github.com/example/wiki", Kept: true},
	} {
		if !hasItem(r, want) {
			t.Errorf("the record lacks %+v", want)
		}
	}
	if _, has := r.Find(KindAnswers, ""); has {
		t.Fatal("the record lists a config.env that isn't there")
	}
	if err := os.WriteFile(opts.Dirs.AnswersFile(), []byte("EVA_MD_HOSTS=all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(r.Items)
	if _, err := runInstall(ctx, opts, f.run); err != nil {
		t.Fatal(err)
	}
	r, _ = ReadRecord(opts.Dirs)
	if !hasItem(r, RecordItem{Kind: KindAnswers, Path: opts.Dirs.AnswersFile()}) || len(r.Items) != before+1 {
		t.Fatalf("the record after a second install: %+v", r)
	}
}

func hasItem(r Record, want RecordItem) bool {
	for _, i := range r.Items {
		if i == want {
			return true
		}
	}
	return false
}

// Install links ~/.local/bin/eva-md to the stable binary only when that
// folder exists and is on PATH, with no eva-md in it or eva-md's own link
// already; it never replaces another eva-md, nor makes the folder.
func TestInstallLinksOnPath(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name               string
		onPath, folder     bool
		there              string // "file", "other link" or "own link"
		wantLink, wantSays bool
	}{
		{name: "not on PATH", folder: true},
		{name: "no folder", onPath: true},
		{name: "made", onPath: true, folder: true, wantLink: true, wantSays: true},
		{name: "own link", onPath: true, folder: true, there: "own link", wantLink: true},
		{name: "a file", onPath: true, folder: true, there: "file"},
		{name: "another link", onPath: true, folder: true, there: "other link"},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts, f := setup(t)
			link := LinkPath(opts.Dirs)
			binary := filepath.Join(opts.Dirs.Share, "bin", "eva-md")
			if c.folder {
				if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if c.onPath {
				t.Setenv("PATH", os.Getenv("PATH")+string(filepath.ListSeparator)+filepath.Dir(link))
			}
			var err error
			switch c.there {
			case "file":
				err = os.WriteFile(link, []byte("another eva-md"), 0o700)
			case "other link":
				err = os.Symlink(opts.Binary, link)
			case "own link":
				err = os.Symlink(binary, link)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.Lstat(link)
			r, err := runInstall(ctx, opts, f.run)
			if err != nil {
				t.Fatal(err)
			}
			says := strings.Contains(strings.Join(r.Actions, "\n"), "link "+link+" to the stable binary")
			if says != c.wantSays || (r.Link != "") != c.wantLink {
				t.Fatalf("actions %q link %q", r.Actions, r.Link)
			}
			rec, _ := ReadRecord(opts.Dirs)
			if _, has := rec.Find(KindPathLink, ""); has != c.wantLink {
				t.Fatalf("the record: %+v", rec)
			}
			switch {
			case c.wantLink:
				if !ownLink(link, binary) {
					t.Fatal("no link to the stable binary")
				}
			case !c.folder:
				if _, err := os.Stat(filepath.Dir(link)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("install made the folder: %v", err)
				}
			default:
				after, _ := os.Lstat(link)
				if (before == nil) != (after == nil) || before != nil && !os.SameFile(before, after) {
					t.Fatal("install changed what was there")
				}
			}
		})
	}
}

// --all removes the config and the local data with the program, never the
// wiki, and refuses outright, removing nothing, when the wiki is inside one
// of the folders it would remove.
func TestUninstallAll(t *testing.T) {
	opts, f := setup(t)
	ctx := context.Background()
	if _, err := runInstall(ctx, opts, f.run); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load(opts.Dirs.ConfigFile())
	inside := c
	inside.WikiPath = filepath.Join(opts.Dirs.State, "wiki")
	if err := config.Save(opts.Dirs.ConfigFile(), inside); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstallWith(ctx, UninstallOptions{Dirs: opts.Dirs, Apply: true, All: true}, f.run); err == nil || !strings.Contains(err.Error(), "would delete it") {
		t.Fatalf("a wiki inside the state folder: %v", err)
	}
	if !f.installed["claude"] || !f.installed["codex"] {
		t.Fatal("the refusal removed a plugin")
	}
	if err := config.Save(opts.Dirs.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.WikiPath, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := uninstallWith(ctx, UninstallOptions{Dirs: opts.Dirs, Apply: true, All: true}, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if r.Wiki != c.WikiPath || r.Remote != "github.com/example/wiki" {
		t.Fatalf("result %+v", r)
	}
	for _, gone := range []string{opts.Dirs.Config, opts.Dirs.State, opts.Dirs.Share} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s kept: %v", gone, err)
		}
	}
	if _, err := os.Stat(c.WikiPath); err != nil {
		t.Fatalf("the wiki: %v", err)
	}
}

// Setup's command adds a config.env that setup wrote after its install to
// the record, once; without a record it writes none.
func TestRecordConfigFiles(t *testing.T) {
	opts, f := setup(t)
	ctx := context.Background()
	if err := RecordConfigFiles(ctx, opts.Dirs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(opts.Dirs.Share, RecordFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a record without an install: %v", err)
	}
	if _, err := runInstall(ctx, opts, f.run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.Dirs.AnswersFile(), []byte("EVA_MD_HOSTS=all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RecordConfigFiles(ctx, opts.Dirs); err != nil {
			t.Fatal(err)
		}
	}
	r, _ := ReadRecord(opts.Dirs)
	n := 0
	for _, i := range r.Items {
		if i.Kind == KindAnswers && i.Path == opts.Dirs.AnswersFile() {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the record lists config.env %d times: %+v", n, r)
	}
	if _, has := r.Find(KindBinary, ""); !has {
		t.Fatalf("the record lost the install's items: %+v", r)
	}
}
