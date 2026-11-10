package health

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// Version is eva-md's version, which the doctor's repair of the knowledge
// map writes as its author; the binary's main sets it.
var Version = "dev"

// refreshKnowledge is a variable so tests can make the wiki's lock busy.
var refreshKnowledge = knowledgepage.Refresh

func init() {
	registerRows(1050, "knowledge", func(r *Report, in *inspection) {
		if c, _ := in.config(); c.WikiPath != "" {
			inspectKnowledge(r, in.d, c, in.repair)
		}
	})
}

// inspectKnowledge adds the row of the knowledge pages: how many there are
// of each kind, and whether the files built from them, the knowledge map,
// the folder's index, the recall index and the root index's line, are
// current. With repair, one of the doctor's two repairs, it builds those
// that are not again from the pages, waiting for the wiki's lock as any
// write does; without, as eva-md status, it writes nothing and fails,
// naming the doctor. A wiki with neither a knowledge folder nor a recall
// index or root index line left from one (see knowledgepage.Report.Exists),
// one whose knowledge/ is not a folder, and one eva-md may not write (see
// WritableWiki), gets no row.
func inspectKnowledge(r *Report, d paths.Dirs, c config.Config, repair bool) {
	if info, err := os.Lstat(filepath.Join(c.WikiPath, knowledgepage.Folder)); err == nil && !info.IsDir() || !WritableWiki(c) {
		return
	}
	w, err := okfadapter.Open(c.WikiPath, d.Locks())
	if err != nil {
		return
	}
	k, err := refreshKnowledge(w, Version, repair)
	switch {
	case err == nil && !k.Exists:
		return
	case errors.Is(err, okfadapter.ErrLockBusy):
		r.add("knowledge", "UNKNOWN", "the files built from the knowledge pages are missing or out of date, and the wiki stayed busy, so they were not built again", "run eva-md doctor again")
		return
	case err != nil:
		r.add("knowledge", "FAIL", "the knowledge pages can't be read, or the files built from them can't be written again", "check that "+knowledgepage.Folder+"/ and "+knowledgepage.IndexFile+" in the wiki are a folder and files eva-md can write, then run eva-md doctor again")
		return
	}
	detail := fmt.Sprintf("%d knowledge %s (%d %s, %d %s, %d %s, %d %s), %d retired", k.Pages, plural(k.Pages, "page", "pages"),
		k.Facts, plural(k.Facts, "fact", "facts"), k.Decisions, plural(k.Decisions, "decision", "decisions"),
		k.Procedures, plural(k.Procedures, "procedure", "procedures"), k.Constraints, plural(k.Constraints, "constraint", "constraints"), k.Retired)
	if k.Unreadable > 0 {
		detail += fmt.Sprintf(", %d unreadable", k.Unreadable)
	}
	// A map a person locked or verified, or one that can't be read as a
	// map, is left as it is: out of date, but no fault of the indexes.
	stale := slices.DeleteFunc(slices.Clone(k.Stale), func(f string) bool { return k.MapSkip != "" && f == knowledgepage.MapRef+".md" })
	switch {
	case len(stale) > 0 && !k.Rebuilt:
		r.add("knowledge", "FAIL", detail+"; out of date: "+strings.Join(stale, ", "), "run eva-md doctor, which builds them again from the pages")
		return
	case len(stale) > 0:
		detail += "; built again from the pages: " + strings.Join(stale, ", ")
	default:
		detail += "; index current"
	}
	if k.LeftOut > 0 {
		detail += fmt.Sprintf("; past %d KB, the recall index leaves out the %d oldest %s", knowledgepage.MaxIndex>>10, k.LeftOut, plural(k.LeftOut, "page", "pages"))
	}
	if k.MapSkip != "" {
		detail += "; the map is " + k.MapSkip + ", so eva-md leaves it as it is"
	}
	r.add("knowledge", "PASS", detail, "")
}

// WritableWiki reports a wiki eva-md writes: its schema is eva-md's,
// schema_version 1, and names the config's instance. The doctor's repairs
// and maintain's knowledge step write only such a wiki, as the daily commit
// commits only one.
func WritableWiki(c config.Config) bool {
	s, err := config.LoadSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"))
	return err == nil && s.SchemaVersion == 1 && c.Instance != "" && s.Instance == c.Instance
}

// countKnowledge counts the knowledge pages, k<N>.md, from the folder's
// listing alone, as eva-md status's contents row does.
func countKnowledge(c config.Config) int {
	entries, _ := os.ReadDir(filepath.Join(c.WikiPath, knowledgepage.Folder))
	n := 0
	for _, e := range entries {
		if _, ok := knowledgepage.ParseID(e.Name()); ok && strings.HasSuffix(e.Name(), ".md") && e.Type().IsRegular() {
			n++
		}
	}
	return n
}
