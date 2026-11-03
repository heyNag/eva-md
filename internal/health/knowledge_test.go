package health

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// saveKnowledge saves knowledge pages as the update does.
func saveKnowledge(t *testing.T, d paths.Dirs, c config.Config, ops ...knowledgepage.Op) {
	t.Helper()
	w, err := okfadapter.Open(c.WikiPath, d.Locks())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	for i := range ops {
		ops[i].At = at
	}
	meta := knowledgepage.SaveMeta{Project: "github.com__acme__eva", Commit: "4e2bf4c", Actor: "claude-code/claude-opus-5-5", Version: "test", Now: func() time.Time { return at }}
	if _, err := knowledgepage.Save(w, ops, meta); err != nil {
		t.Fatal(err)
	}
}

// add is an operation that adds a page of the kind given.
func add(kind, title, text string) knowledgepage.Op {
	return knowledgepage.Op{Action: knowledgepage.Add, Kind: kind, Title: title, Text: text, Tag: "stated", Scope: knowledgepage.ScopeProject, Refs: []string{"s:3f2a91c0#14"}}
}

// knowledgeRow runs the knowledge check, with repair as the doctor or
// without as eva-md status, and returns its row, if any.
func knowledgeRow(d paths.Dirs, c config.Config, repair bool) (Check, bool) {
	var r Report
	inspectKnowledge(&r, d, c, repair)
	for _, check := range r.Checks {
		if check.Name == "knowledge" {
			return check, true
		}
	}
	return Check{}, false
}

// The doctor counts the knowledge pages by kind and state once there is a
// knowledge folder, and builds the files made from them again when one is
// missing or out of date, saying which; without repair, as eva-md status,
// it fails and names the doctor. A map a person locked is left as it is.
// A wiki of another instance gets no row.
func TestKnowledgeRow(t *testing.T) {
	d, c := namesWiki(t)
	if check, found := knowledgeRow(d, c, true); found {
		t.Fatalf("a wiki without knowledge pages got %+v", check)
	}
	saveKnowledge(t, d, c, add(knowledgepage.Decision, "Inbox format", "The inbox stores JSONL"), add(knowledgepage.Constraint, "Hooks stay offline", "Hooks never make a network call"),
		add(knowledgepage.Fact, "Quince", "The quince job runs nightly"))
	saveKnowledge(t, d, c, knowledgepage.Op{Action: knowledgepage.Drop, ID: 3, Reason: "removed", Tag: "stated", Refs: []string{"s:4b5c6d7e#4"}})
	const counts = "3 knowledge pages (0 facts, 1 decision, 0 procedures, 1 constraint), 1 retired"
	if check, found := knowledgeRow(d, c, true); !found || check != (Check{"knowledge", "PASS", counts + "; index current", ""}) {
		t.Fatalf("current: %+v", check)
	}
	index := filepath.Join(c.WikiPath, filepath.FromSlash(knowledgepage.IndexFile))
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if check, _ := knowledgeRow(d, c, false); check != (Check{"knowledge", "FAIL", counts + "; out of date: .eva-md/knowledge.tsv", "run eva-md doctor, which builds them again from the pages"}) {
		t.Fatalf("status: %+v", check)
	}
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("status wrote the index: %v", err)
	}
	if check, _ := knowledgeRow(d, c, true); check != (Check{"knowledge", "PASS", counts + "; built again from the pages: .eva-md/knowledge.tsv", ""}) {
		t.Fatalf("repair: %+v", check)
	}
	if data, err := os.ReadFile(index); err != nil || len(knowledgepage.ParseIndex(data)) != 2 {
		t.Fatalf("index %q %v", data, err)
	}
	// A person locks the map; a later page leaves it out of date, which is
	// no fault.
	mapFile := filepath.Join(c.WikiPath, "knowledge", "map.md")
	data, err := os.ReadFile(mapFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapFile, []byte(strings.Replace(string(data), "type: map\n", "type: map\nlocked: true\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	saveKnowledge(t, d, c, add(knowledgepage.Procedure, "Release", "Tag the release, then push the tag"))
	if check, _ := knowledgeRow(d, c, true); check != (Check{"knowledge", "PASS", "4 knowledge pages (0 facts, 1 decision, 1 procedure, 1 constraint), 1 retired; index current; the map is locked, so eva-md leaves it as it is", ""}) {
		t.Fatalf("a locked map: %+v", check)
	}
	c.Instance = "other"
	if check, found := knowledgeRow(d, c, true); found {
		t.Fatalf("another instance's wiki got %+v", check)
	}
}

// A doctor that finds the wiki's lock busy says the files were not built
// again and to run it again; a folder it can't read fails.
func TestKnowledgeRowWithTheWikiBusy(t *testing.T) {
	d, c := namesWiki(t)
	saveKnowledge(t, d, c, add(knowledgepage.Constraint, "Hooks stay offline", "Hooks never make a network call"))
	refreshKnowledge = func(*okfadapter.Wiki, string, bool) (knowledgepage.Report, error) {
		return knowledgepage.Report{}, fmt.Errorf("%w after 1m0s", okfadapter.ErrLockBusy)
	}
	t.Cleanup(func() { refreshKnowledge = knowledgepage.Refresh })
	if check, _ := knowledgeRow(d, c, true); check.Status != "UNKNOWN" || !strings.Contains(check.Detail, "the wiki stayed busy") || check.Fix != "run eva-md doctor again" {
		t.Fatalf("busy: %+v", check)
	}
	refreshKnowledge = func(*okfadapter.Wiki, string, bool) (knowledgepage.Report, error) {
		return knowledgepage.Report{}, errors.New("knowledgepage: knowledge is not a folder")
	}
	if check, _ := knowledgeRow(d, c, true); check.Status != "FAIL" || !strings.Contains(check.Fix, knowledgepage.IndexFile) {
		t.Fatalf("an error: %+v", check)
	}
}

// eva-md status says when the files built from the knowledge pages are out
// of date and never builds them; the doctor does, and status then passes.
// Status counts the pages among the wiki's contents from the folder's
// listing.
func TestStatusLeavesTheKnowledgeIndex(t *testing.T) {
	d, c := namesWiki(t)
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	saveKnowledge(t, d, c, add(knowledgepage.Constraint, "Hooks stay offline", "Hooks never make a network call"))
	index := filepath.Join(c.WikiPath, filepath.FromSlash(knowledgepage.IndexFile))
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	r := Status(t.Context(), d)
	if row := findRow(t, r, "knowledge"); row.Status != "FAIL" || !strings.HasSuffix(row.Detail, "; out of date: .eva-md/knowledge.tsv") {
		t.Fatalf("status: %+v", row)
	}
	if row := findRow(t, r, "wiki contents"); row.Detail != "0 projects, 0 threads, 1 knowledge page" {
		t.Fatalf("contents: %+v", row)
	}
	if _, err := os.Lstat(index); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("status wrote the index: %v", err)
	}
	if row := findRow(t, Inspect(t.Context(), d, false), "knowledge"); row.Status != "PASS" || !strings.HasSuffix(row.Detail, "; built again from the pages: .eva-md/knowledge.tsv") {
		t.Fatalf("doctor: %+v", row)
	}
	if row := findRow(t, Status(t.Context(), d), "knowledge"); row.Status != "PASS" || !strings.HasSuffix(row.Detail, "; index current") {
		t.Fatalf("status after the doctor: %+v", row)
	}
}

// A person removes the knowledge folder: the doctor still has a row, since
// the recall index and the root index's line list pages that are gone, and
// its repair leaves the index its header alone and removes the line, so
// nothing recalls a removed page. A wiki of a newer schema gets no row.
func TestKnowledgeRowWithoutTheFolder(t *testing.T) {
	d, c := namesWiki(t)
	saveKnowledge(t, d, c, add(knowledgepage.Constraint, "Hooks stay offline", "Hooks never make a network call"))
	if err := os.RemoveAll(filepath.Join(c.WikiPath, knowledgepage.Folder)); err != nil {
		t.Fatal(err)
	}
	const counts = "0 knowledge pages (0 facts, 0 decisions, 0 procedures, 0 constraints), 0 retired"
	if check, found := knowledgeRow(d, c, false); !found || check != (Check{"knowledge", "FAIL", counts + "; out of date: .eva-md/knowledge.tsv, index.md", "run eva-md doctor, which builds them again from the pages"}) {
		t.Fatalf("status: %+v", check)
	}
	if check, _ := knowledgeRow(d, c, true); check != (Check{"knowledge", "PASS", counts + "; built again from the pages: .eva-md/knowledge.tsv, index.md", ""}) {
		t.Fatalf("repair: %+v", check)
	}
	if rows, err := knowledgepage.ReadIndexAt(c.WikiPath); err != nil || len(rows) != 0 {
		t.Fatalf("the recall index: %+v, %v", rows, err)
	}
	if root, _ := os.ReadFile(filepath.Join(c.WikiPath, "index.md")); strings.Contains(string(root), "knowledge/index.md") {
		t.Fatalf("the root index:\n%s", root)
	}
	if check, _ := knowledgeRow(d, c, true); check != (Check{"knowledge", "PASS", counts + "; index current", ""}) {
		t.Fatalf("repaired: %+v", check)
	}
	if err := config.SaveSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 2, Instance: c.Instance}); err != nil {
		t.Fatal(err)
	}
	if check, found := knowledgeRow(d, c, true); found {
		t.Fatalf("a newer schema's wiki got %+v", check)
	}
}
