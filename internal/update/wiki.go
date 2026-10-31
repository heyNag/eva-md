package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// schemaVersion is the wiki schema this eva-md writes.
const schemaVersion = 1

// maxSources is how many sessions a thread page lists in its sources.
const maxSources = 20

// maxSaves bounds the saves of one pass: the first and 2 retries after a
// compare-and-swap conflict, each on the page read again.
const maxSaves = 3

// openWiki opens the configured wiki for writing, once its schema is one
// this eva-md writes and names the config's instance, so one machine's
// memory never goes into another's wiki.
func openWiki(c config.Config, dirs paths.Dirs) (*okfadapter.Wiki, error) {
	if c.WikiPath == "" {
		return nil, errors.New("update: no wiki is set up; run eva-md setup first")
	}
	w, err := okfadapter.Open(c.WikiPath, dirs.Locks())
	if err != nil {
		return nil, err
	}
	s, err := config.LoadSchema(filepath.Join(w.Root(), ".eva-md", "schema.toml"))
	switch {
	case err != nil:
		return nil, errors.New("update: the wiki's .eva-md/schema.toml can't be read")
	case s.SchemaVersion != schemaVersion:
		return nil, fmt.Errorf("update: the wiki's schema version is %d; this eva-md writes %d", s.SchemaVersion, schemaVersion)
	case c.Instance == "":
		return nil, errors.New("update: the config names no wiki instance")
	case s.Instance != c.Instance:
		return nil, errors.New("update: the wiki's instance is not the one the config names")
	}
	return w, nil
}

// area is the wiki folder a project's pages go in: projects/<slug>, or
// general for the general area.
type area struct {
	dir     string // "projects/<slug>" or "general"
	slug    string // the folder's name
	project identity.Project
}

func (a area) general() bool { return a.dir == identity.GeneralKey }

// threadID is the page id of a branch's thread in the area.
func (a area) threadID(branch string) string {
	if a.general() {
		return "general/threads/default"
	}
	return a.dir + "/threads/" + threadpage.ThreadName(branch)
}

// findArea returns the project's folder in the wiki: the one whose now page
// proves the project, its own slug first, as the brief finds it; otherwise
// a free one, the slug, then owner-repo, then the slug with a hash.
func findArea(w *okfadapter.Wiki, p identity.Project) (area, error) {
	if p.Kind == identity.KindGeneral {
		return area{dir: identity.GeneralKey, slug: identity.GeneralKey, project: p}, nil
	}
	if !plainName(p.Slug) {
		return area{}, errors.New("update: the project's slug can't name a wiki folder")
	}
	names := []string{p.Slug}
	if p.Remote != "" {
		names = append(names, projectFolders(w, p.Slug)...)
	}
	for _, name := range names {
		if remote, ok := nowRemote(w, name); ok && proves(p, name, remote) {
			return area{dir: "projects/" + name, slug: name, project: p}, nil
		}
	}
	sum := sha256.Sum256([]byte(p.Key))
	for _, name := range []string{p.Slug, ownerRepo(p.Remote), p.Slug + "-" + hex.EncodeToString(sum[:])[:6]} {
		if name == "" || !plainName(name) {
			continue
		}
		info, err := os.Lstat(filepath.Join(w.Root(), "projects", name))
		if errors.Is(err, fs.ErrNotExist) {
			return area{dir: "projects/" + name, slug: name, project: p}, nil
		}
		// A folder of the project's own slug without a now page is one a
		// pass left before writing the overview. One whose now page can't
		// be read may be another project's.
		if err == nil && info.IsDir() && name == p.Slug && noNow(w, name) {
			return area{dir: "projects/" + name, slug: name, project: p}, nil
		}
	}
	return area{}, errors.New("update: no free wiki folder for the project")
}

// proves reports whether a now page with remote shows that folder name is
// p's: by its remote, or for a project without one, by its own slug.
func proves(p identity.Project, name, remote string) bool {
	if p.Remote != "" {
		return remote == p.Remote
	}
	return name == p.Slug && remote == ""
}

// nowRemote reads the remote a project folder's now page names, and reports
// whether the page could be read.
func nowRemote(w *okfadapter.Wiki, name string) (string, bool) {
	pg, err := w.Read("projects/" + name + "/now")
	if err != nil {
		return "", false
	}
	remote, _ := pg.Concept.Extra["remote"].(string)
	return remote, true
}

// noNow reports whether a project folder has no now page at all.
func noNow(w *okfadapter.Wiki, name string) bool {
	_, err := os.Lstat(filepath.Join(w.Root(), "projects", name, "now.md"))
	return errors.Is(err, fs.ErrNotExist)
}

// projectFolders lists the folders under projects/ other than own, at most
// 256, as the brief does.
func projectFolders(w *okfadapter.Wiki, own string) []string {
	var out []string
	for _, name := range subfolders(filepath.Join(w.Root(), "projects")) {
		if name != own && len(out) < 256 {
			out = append(out, name)
		}
	}
	return out
}

// subfolders lists the plainly named folders of dir, not links, sorted.
func subfolders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && plainName(e.Name()) && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}

// ownerRepo is "owner-repo" from a normalized remote such as
// github.com/owner/repo, the folder for a project whose slug is taken.
func ownerRepo(remote string) string {
	parts := strings.Split(remote, "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[len(parts)-2] + "-" + parts[len(parts)-1]
}

// plainName reports whether a folder name holds no control, format or space
// characters, so it can be part of a page id.
func plainName(s string) bool {
	return s != "" && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) || unicode.IsSpace(r) })
}

// thread is a thread page as a pass reads it.
type thread struct {
	id, branch  string
	title, desc string // for a page the pass creates
	exists      bool
	page        threadpage.Page
	body        string // as read back, to tell a pass that changes nothing
	hash        string
	asOf        time.Time
	sources     []string // the sessions' refs
	skip        string   // locked, verified or damaged: the page is never written
}

// moves reports whether a pass's facts move the page's as_of forward or add
// a session to its sources, which a pass that changes nothing else still
// stamps, so the brief doesn't call a Now the pass confirmed stale.
func (t thread) moves(f facts) bool {
	return f.asOf.After(t.asOf) || !slices.Contains(t.sources, f.source)
}

// readThread reads a thread page by its id, never the rest of the wiki. A
// missing page is a new thread. A page with locked: true, a verified entry
// (okf reserves verified for people, and every save would supersede it) or
// a body eva-md can't read back safely is never written.
func readThread(w *okfadapter.Wiki, a area, branch string) (thread, error) {
	t := thread{id: a.threadID(branch), branch: branch}
	t.title, t.desc = threadTitle(a, branch)
	pg, err := w.Read(t.id)
	var pathErr *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		t.page = threadpage.Page{Title: t.title}
		return t, nil
	case errors.As(err, &pathErr):
		return t, err
	case err != nil:
		t.exists, t.skip = true, ReasonDamaged
		return t, nil
	}
	c := pg.Concept
	t.exists, t.body, t.hash = true, c.Body, pg.Hash
	as, _ := c.Extra["as_of"].(string)
	t.asOf, _ = time.Parse(time.RFC3339Nano, as)
	for _, s := range c.Sources {
		t.sources = append(t.sources, s.Resource)
	}
	switch {
	case okfadapter.IsLocked(c):
		t.skip = ReasonLocked
	case len(c.Verified) > 0:
		t.skip = ReasonVerified
	case c.Type != "thread":
		t.skip = ReasonDamaged
	}
	if t.page, err = threadpage.Parse(c.Body); err != nil && t.skip == "" {
		t.skip = ReasonDamaged
	}
	return t, nil
}

// threadTitle is a new thread page's title and description.
func threadTitle(a area, branch string) (title, desc string) {
	if a.general() {
		return "General · default", "General conversations outside a project"
	}
	of := a.project.Remote
	if of == "" {
		of = a.slug
	}
	return meta(a.slug + " · " + branch), meta("Branch " + branch + " of " + of)
}

// meta is text for a frontmatter field: one line, without control or
// format characters, and without the "---" okf refuses there.
func meta(s string) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, s)), " ")
	for strings.Contains(s, "---") {
		s = strings.ReplaceAll(s, "---", "--")
	}
	return s
}

// facts are what a pass writes in a thread page's frontmatter.
type facts struct {
	asOf             time.Time // the newest entry the pass covered
	head             string    // the branch's newest commit, when git told
	model, modelNote string
	parent           string // a new thread's parent branch
	source           string // the session's ref, s:<id8>
	project, writer  string
	actor            string // generated.by
}

// stamp sets a thread page's frontmatter for a pass. as_of only moves
// forward, and the session joins the end of the last 20 sources.
func stamp(c *okfadapter.Concept, f facts, branch string) {
	if c.Extra == nil {
		c.Extra = map[string]any{}
	}
	old, _ := c.Extra["as_of"].(string)
	if t, err := time.Parse(time.RFC3339Nano, old); err != nil || f.asOf.After(t) {
		if !f.asOf.IsZero() {
			c.Extra["as_of"] = f.asOf.Format(time.RFC3339Nano)
		}
	}
	if f.head != "" {
		c.Extra["head"] = f.head
	}
	c.Extra["branch"] = branch
	c.Extra["model"] = f.model
	c.Extra["model_note"] = f.modelNote
	c.Extra["project"] = f.project
	c.Extra["state"] = "active"
	c.Extra["writer"] = f.writer
	if _, ok := c.Extra["parent"]; !ok {
		c.Extra["parent"] = f.parent
	}
	if _, ok := c.Extra["locked"]; !ok {
		c.Extra["locked"] = false
	}
	var sources []okfadapter.Source
	for _, s := range c.Sources {
		if s.Resource != f.source {
			sources = append(sources, s)
		}
	}
	sources = append(sources, okfadapter.Source{Resource: f.source})
	c.Sources = sources[max(len(sources)-maxSources, 0):]
}

// saveThread applies u to the thread page and saves it through the
// adapter's compare-and-swap. On a conflict it reads the page again and
// applies u to it again, at most twice. A page whose rendered body equals
// the body read back keeps its body, and only its frontmatter is stamped,
// when the pass moves its as_of or adds its session to the sources; a new
// page the update leaves empty is not created. It returns whether the body
// changed and whether the page was written, or why it was skipped when it
// became locked, verified or damaged meanwhile.
func saveThread(w *okfadapter.Wiki, a area, t thread, u threadpage.Update, f facts) (changed, wrote bool, skip string, err error) {
	for n := 1; ; n++ {
		next, _ := threadpage.Apply(t.page, u)
		body := threadpage.Render(next)
		same := t.exists && body == t.body
		if same && !t.moves(f) || !t.exists && body == threadpage.Render(threadpage.Page{Title: t.title}) {
			return false, false, "", nil
		}
		if t.exists {
			wrote, err = w.Update(t.id, t.hash, func(c *okfadapter.Concept) error {
				c.Body = body
				stamp(c, f, t.branch)
				return nil
			}, f.actor)
		} else {
			c := &okfadapter.Concept{Type: "thread", Title: t.title, Description: t.desc, Status: "stable", Body: body}
			stamp(c, f, t.branch)
			err = w.Locked(func(w *okfadapter.Wiki) error {
				if err := claimArea(w, a, f.writer); err != nil {
					return err
				}
				return w.Create(t.id, c, f.actor)
			})
			wrote = true
		}
		if !errors.Is(err, okfadapter.ErrConflict) && !errors.Is(err, okfadapter.ErrExists) || n == maxSaves {
			wrote = wrote && err == nil
			return wrote && !same, wrote, "", err
		}
		if t, err = readThread(w, a, t.branch); err != nil {
			return false, false, "", err
		}
		if t.skip != "" {
			return false, false, t.skip, nil
		}
	}
}

// rulesID is the id of the area's rules page, or of the general area's.
func (a area) rulesID(general bool) string {
	if general || a.general() {
		return identity.GeneralKey + "/rules"
	}
	return a.dir + "/rules"
}

// rulesMeta is a new rules page's title and description.
func rulesMeta(a area, general bool) (title, desc string) {
	if general || a.general() {
		return "General · rules", "Standing rules for all of the user's projects"
	}
	of := a.project.Remote
	if of == "" {
		of = a.slug
	}
	return meta(a.slug + " · rules"), meta("Standing rules for " + of)
}

// rulesPage is a standing rules page as a pass reads it.
type rulesPage struct {
	id, title, desc string // the title and description for a page the pass creates
	general         bool
	exists          bool
	page            threadpage.RulesPage
	body, hash      string
	skip            string // locked, verified or damaged: the page is never written
}

// rulesPages are the rules pages a pass sees: the general area's and, in a
// project, the project's.
type rulesPages struct{ general, project rulesPage }

// set is what Build and Validate see of the pages.
func (r rulesPages) set(a area) RuleSet {
	return RuleSet{General: r.general.page, Project: r.project.page, Outside: a.general()}
}

// readRulesPages reads the rules pages a pass in the area sees.
func readRulesPages(w *okfadapter.Wiki, a area) (rulesPages, error) {
	var r rulesPages
	var err error
	if r.general, err = readRules(w, a, true); err != nil || a.general() {
		return r, err
	}
	r.project, err = readRules(w, a, false)
	return r, err
}

// readRules reads a rules page by its id, as readThread reads a thread page:
// a missing page is an empty one, and one that is locked, has a verified
// entry or can't be read back safely is never written.
func readRules(w *okfadapter.Wiki, a area, general bool) (rulesPage, error) {
	rp := rulesPage{id: a.rulesID(general), general: general || a.general()}
	rp.title, rp.desc = rulesMeta(a, general)
	pg, err := w.Read(rp.id)
	var pathErr *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		rp.page = threadpage.RulesPage{Title: rp.title}
		return rp, nil
	case errors.As(err, &pathErr):
		return rp, err
	case err != nil:
		rp.exists, rp.skip = true, ReasonDamaged
		return rp, nil
	}
	c := pg.Concept
	rp.exists, rp.body, rp.hash = true, c.Body, pg.Hash
	switch {
	case okfadapter.IsLocked(c):
		rp.skip = ReasonLocked
	case len(c.Verified) > 0:
		rp.skip = ReasonVerified
	case c.Type != "rules":
		rp.skip = ReasonDamaged
	}
	if rp.page, err = threadpage.ParseRules(c.Body); err != nil && rp.skip == "" {
		rp.skip = ReasonDamaged
	}
	return rp, nil
}

// saveRules applies a pass's rule changes to the rules pages they touch,
// each through the adapter's compare-and-swap and, on a conflict, read again
// and applied again, at most twice. A page that is locked, verified or
// damaged is left as it is, and skipped names it with its reason. It returns
// the pages it wrote.
func saveRules(w *okfadapter.Wiki, a area, pages rulesPages, ops []threadpage.RuleOp, f facts) (wrote []rulesPage, skipped []string, err error) {
	for _, rp := range []rulesPage{pages.general, pages.project} {
		if rp.id == "" || !slices.ContainsFunc(ops, func(op threadpage.RuleOp) bool { return op.General == rp.general }) {
			continue
		}
		saved, skip, err := saveRulesPage(w, a, rp, ops, f)
		switch {
		case err != nil:
			return wrote, skipped, err
		case skip != "":
			skipped = append(skipped, rp.id+" ("+skip+")")
		case saved:
			wrote = append(wrote, rp)
		}
	}
	return wrote, skipped, nil
}

// saveRulesPage applies the changes to one rules page and saves it when its
// body changes. A project's first rules page claims the project's folder
// first, under the same wiki lock, as a new thread page does.
func saveRulesPage(w *okfadapter.Wiki, a area, rp rulesPage, ops []threadpage.RuleOp, f facts) (wrote bool, skip string, err error) {
	for n := 1; ; n++ {
		if rp.skip != "" {
			return false, rp.skip, nil
		}
		next, _ := threadpage.ApplyRules(rp.page, ops, rp.general)
		body := threadpage.RenderRules(next)
		if rp.exists && body == rp.body || !rp.exists && len(next.Rules) == 0 {
			return false, "", nil
		}
		if rp.exists {
			wrote, err = w.Update(rp.id, rp.hash, func(c *okfadapter.Concept) error {
				c.Body = body
				stampRules(c, f)
				return nil
			}, f.actor)
		} else {
			c := &okfadapter.Concept{Type: "rules", Title: rp.title, Description: rp.desc, Status: "stable", Body: body}
			stampRules(c, f)
			err = w.Locked(func(w *okfadapter.Wiki) error {
				if !rp.general {
					if err := claimArea(w, a, f.writer); err != nil {
						return err
					}
				}
				return w.Create(rp.id, c, f.actor)
			})
			wrote = err == nil
		}
		if !errors.Is(err, okfadapter.ErrConflict) && !errors.Is(err, okfadapter.ErrExists) || n == maxSaves {
			return wrote && err == nil, "", err
		}
		if rp, err = readRules(w, a, rp.general); err != nil {
			return false, "", err
		}
	}
}

// stampRules sets a rules page's frontmatter for a pass.
func stampRules(c *okfadapter.Concept, f facts) {
	if c.Extra == nil {
		c.Extra = map[string]any{}
	}
	c.Extra["writer"] = f.writer
	if _, ok := c.Extra["locked"]; !ok {
		c.Extra["locked"] = false
	}
}

// rulesLine is the area index's line for its rules page, or "" when the
// area has none.
func rulesLine(w *okfadapter.Wiki, a area) string {
	pg, err := w.Read(a.rulesID(false))
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	title, desc := rulesMeta(a, false)
	if err == nil {
		title, desc = pg.Concept.Title, pg.Concept.Description
	}
	return okfadapter.IndexLine(title, "rules.md", desc)
}

// claimArea creates a project folder's now.md, which names the project's
// remote, when the folder has none. A new thread page is created after it,
// under the same wiki lock, so a pass that dies before refreshArea leaves a
// folder findArea proves this project's, never one another project of the
// same name takes for its own. refreshArea lists the threads in it.
func claimArea(w *okfadapter.Wiki, a area, actor string) error {
	if a.general() {
		return nil
	}
	if _, err := w.Read(a.dir + "/now"); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	title, desc := overviewMeta(a)
	return w.Create(a.dir+"/now", newOverview(a, title, desc, threadpage.RenderOverview(title, nil, false), actor), actor)
}

// overviewMeta is the title and description of the area's now.md, for a page
// the job creates.
func overviewMeta(a area) (title, desc string) {
	switch {
	case a.general():
		return "General memory", "Overview of general conversations"
	case a.project.Remote != "":
		return meta(a.slug), meta("Overview of the threads of " + a.project.Remote)
	}
	return meta(a.slug), meta("Overview of the threads of " + a.slug)
}

// newOverview is the area's now.md as the job creates it.
func newOverview(a area, title, desc, body, actor string) *okfadapter.Concept {
	return &okfadapter.Concept{Type: "now", Title: title, Description: desc, Status: "stable", Body: body,
		Extra: map[string]any{"locked": false, "remote": a.project.Remote, "writer": actor}}
}

// refreshArea rebuilds the area's now.md overview and the index files of
// the folders a write touched: the area's threads folder, the area itself
// and, for a project, projects/ and the root. They read every page of the
// area's threads folder, so only the update job runs them, never a hook. The
// overview links the area's rules page, and its index lists it, once it
// exists. An index file is written only when its text changes, and a locked
// or unreadable now.md is left as it is.
func refreshArea(w *okfadapter.Wiki, a area, actor string) error {
	pages := threadPages(w, a)
	rules := rulesLine(w, a)
	nowTitle, nowDesc, err := writeOverview(w, a, pages, rules != "", actor)
	if err != nil {
		return err
	}
	var lines []string
	for _, p := range pages {
		lines = append(lines, okfadapter.IndexLine(p.title, p.file+".md", p.desc))
	}
	if err := writeIndex(w, a.dir+"/threads/index.md", "Threads", lines); err != nil {
		return err
	}
	heading := a.slug
	if a.general() {
		heading = "General"
	}
	lines = []string{okfadapter.IndexLine(nowTitle, "now.md", nowDesc)}
	if rules != "" {
		lines = append(lines, rules)
	}
	lines = append(lines, okfadapter.IndexLine("Threads", "threads/index.md", "Conversation threads"))
	if err := writeIndex(w, a.dir+"/index.md", heading, lines); err != nil || a.general() {
		return err
	}
	lines = nil
	for _, name := range subfolders(filepath.Join(w.Root(), "projects")) {
		lines = append(lines, okfadapter.IndexLine(name, name+"/index.md", "Project memory"))
	}
	if err := writeIndex(w, "projects/index.md", "Projects", lines); err != nil {
		return err
	}
	// Under the wiki lock, so a system write that adds its line to the root
	// index between this read and this write can't lose it.
	return w.Locked(linkProjects)
}

// threadPage is one page of an area's threads folder, for the overview and
// the index.
type threadPage struct {
	file, title, desc string
	thread            threadpage.Thread
}

// threadPages reads every page of the area's threads folder, sorted by file
// name. A page that can't be read is still listed, by its file name, since
// okf counts a page nothing links to as orphaned.
func threadPages(w *okfadapter.Wiki, a area) []threadPage {
	entries, err := os.ReadDir(filepath.Join(w.Root(), filepath.FromSlash(a.dir), "threads"))
	if err != nil {
		return nil
	}
	var out []threadPage
	for _, e := range entries {
		file, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || !e.Type().IsRegular() || file == "index" || strings.HasPrefix(file, ".") {
			continue
		}
		p := threadPage{file: file, title: file, thread: threadpage.Thread{Branch: file, File: file}}
		if pg, err := w.Read(a.dir + "/threads/" + file); err == nil {
			c := pg.Concept
			p.title, p.desc = c.Title, c.Description
			if branch, _ := c.Extra["branch"].(string); branch != "" {
				p.thread.Branch = branch
			}
			as, _ := c.Extra["as_of"].(string)
			p.thread.Active, _ = time.Parse(time.RFC3339Nano, as)
			if body, err := threadpage.Parse(c.Body); err == nil {
				p.thread.Summary = body.Now.Summary
			}
		}
		out = append(out, p)
	}
	return out
}

// writeOverview writes the area's now.md, one line per thread, newest
// activity first, and a link to the area's rules page when it has one, as
// eva-md's own page, and returns its title and description for the index.
func writeOverview(w *okfadapter.Wiki, a area, pages []threadPage, rules bool, actor string) (title, desc string, err error) {
	id := a.dir + "/now"
	title, desc = overviewMeta(a)
	pg, err := w.Read(id)
	var pathErr *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case errors.As(err, &pathErr):
		return "", "", err
	case err != nil:
		return title, desc, nil
	default:
		title, desc = pg.Concept.Title, pg.Concept.Description
		if okfadapter.IsLocked(pg.Concept) {
			return title, desc, nil
		}
	}
	var threads []threadpage.Thread
	for _, p := range pages {
		threads = append(threads, p.thread)
	}
	body := threadpage.RenderOverview(title, threads, rules)
	if pg == nil {
		return title, desc, w.Create(id, newOverview(a, title, desc, body, actor), actor)
	}
	_, err = w.Update(id, pg.Hash, func(c *okfadapter.Concept) error {
		c.Body = body
		if c.Extra == nil {
			c.Extra = map[string]any{}
		}
		if !a.general() {
			c.Extra["remote"] = a.project.Remote
		}
		c.Extra["writer"] = actor
		return nil
	}, actor)
	if errors.Is(err, okfadapter.ErrConflict) {
		// A hand edit landed between the read and the save; the next pass
		// writes the overview again.
		err = nil
	}
	return title, desc, err
}

// writeIndex writes a sub-index in okf's line format, its lines sorted by
// file name under a "# " heading, when its text changes.
func writeIndex(w *okfadapter.Wiki, rel, heading string, lines []string) error {
	text := "# " + heading + "\n\n" + strings.Join(lines, "\n") + "\n"
	if len(lines) == 0 {
		text = "# " + heading + "\n"
	}
	return writeChanged(w, rel, text)
}

// beforeLinkWrite runs between linkProjects's read of the root index and its
// write; a test writes the index there.
var beforeLinkWrite = func() {}

// linkProjects adds projects/index.md to the root index once, after the
// lines setup wrote, which sort before it.
func linkProjects(w *okfadapter.Wiki) error {
	data, err := readWikiFile(w, "index.md")
	if err != nil || strings.Contains(data, "](projects/index.md)") {
		return err
	}
	beforeLinkWrite()
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	return writeChanged(w, "index.md", data+okfadapter.IndexLine("Projects", "projects/index.md", "Project conversations")+"\n")
}

// writeChanged writes a wiki file that isn't a page when its text differs
// from what is there.
func writeChanged(w *okfadapter.Wiki, rel, text string) error {
	if old, err := readWikiFile(w, rel); err == nil && old == text {
		return nil
	}
	return w.WriteFile(rel, []byte(text))
}

// readWikiFile reads a regular file of the wiki that isn't a page, or
// returns "" when it is missing.
func readWikiFile(w *okfadapter.Wiki, rel string) (string, error) {
	full := filepath.Join(w.Root(), filepath.FromSlash(path.Clean(rel)))
	info, err := os.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("update: %s is not a regular file", rel)
	}
	data, err := os.ReadFile(full)
	return string(data), err
}

// sourceRef is a session's entry in a page's sources: s:<id8>, or the full
// id when another session file shares the first 8 characters.
func sourceRef(session string, full bool) string {
	if full {
		return "s:" + session
	}
	return "s:" + prefix8(session)
}

// prefix8 is the part of a session id a short ref keeps (see inbox.Ref).
func prefix8(session string) string {
	r := []rune(session)
	return string(r[:min(len(r), 8)])
}
