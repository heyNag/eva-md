package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// eva-md maintain builds the files made from the knowledge pages again when
// one is missing, before the day's commit, which then holds it, and says so
// in a line of its own; with them current it prints nothing of them, and
// --push-only never builds them.
func TestMaintainRebuildsTheKnowledgeIndex(t *testing.T) {
	d, home := gitHomeFixture(t, "")
	remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
	gitRun(t, home, "init", "--bare", "--quiet", remote)
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: wiki, Dirs: d, Version: "test", OKFModule: "v0.7.2"}); err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(wiki, d.Locks())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	op := knowledgepage.Op{Action: knowledgepage.Add, Kind: knowledgepage.Constraint, Title: "Hooks stay offline", Text: "Hooks never make a network call", Tag: "stated",
		Scope: knowledgepage.ScopeAll, Refs: []string{"s:3f2a91c0#15"}, At: at}
	if _, err := knowledgepage.Save(w, []knowledgepage.Op{op}, knowledgepage.SaveMeta{Project: "github.com__acme__eva", Actor: "claude-code/claude-opus-5-5", Version: "test"}); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := evaMD(t, "maintain")
	if code != 0 || !strings.HasPrefix(out, "maintain: committed (2 pages), push ok\n") || strings.Contains(out, "knowledge") || errOut != "" {
		t.Fatalf("current: %d %q %q", code, out, errOut)
	}
	index := filepath.Join(wiki, filepath.FromSlash(knowledgepage.IndexFile))
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wiki, "commit", "--quiet", "-am", "Lose the recall index")
	if code, out, errOut := evaMD(t, "maintain", "--push-only"); code != 0 || strings.Contains(out, "knowledge") || errOut != "" {
		t.Fatalf("--push-only: %d %q %q", code, out, errOut)
	}
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("--push-only built the index: %v", err)
	}
	// A wiki whose schema names another instance, or that a newer eva-md set
	// up, is never written: the commit refuses it, and the step builds
	// nothing.
	schemaFile := filepath.Join(wiki, ".eva-md", "schema.toml")
	schema, err := config.LoadSchema(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []config.WikiSchema{{SchemaVersion: 1, OKFSpec: schema.OKFSpec, Instance: "another-instance"}, {SchemaVersion: 2, OKFSpec: schema.OKFSpec, Instance: schema.Instance}} {
		if err := config.SaveSchema(schemaFile, s); err != nil {
			t.Fatal(err)
		}
		gitRun(t, wiki, "commit", "--quiet", "-am", "Change the schema")
		if _, out, errOut := evaMD(t, "maintain"); strings.Contains(out+errOut, "knowledge") {
			t.Fatalf("schema %+v: %q %q", s, out, errOut)
		}
		if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("schema %+v: maintain built the index: %v", s, err)
		}
	}
	if err := config.SaveSchema(schemaFile, schema); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wiki, "commit", "--quiet", "-am", "Restore the schema")
	code, out, errOut = evaMD(t, "maintain")
	if code != 0 || !strings.HasPrefix(out, "maintain: knowledge: rebuilt the index (1 page)\nmaintain: committed (0 pages), push ok\n") || errOut != "" {
		t.Fatalf("missing: %d %q %q", code, out, errOut)
	}
	if files := strings.Fields(gitRun(t, wiki, "show", "--name-only", "--format=", "HEAD")); !slices.Equal(files, []string{".eva-md/knowledge.tsv"}) {
		t.Fatalf("committed %v", files)
	}
}
