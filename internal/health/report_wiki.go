package health

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/paths"
)

// maxWikiFiles bounds the wiki's files the report lists.
const maxWikiFiles = 200000

// codeStates are what measuring a page's code in one repo can find.
var codeStates = []string{coderef.Unchanged, coderef.Moved, coderef.NotOnBranch, coderef.UnknownCommit, coderef.NoClone, coderef.TimedOut, coderef.GitFailed, coderef.NotRun}

// WikiFigures is what the wiki holds, from its folder's listing alone,
// never a page's text: its Markdown pages by kind, as their place names
// them (thread, rules, knowledge, system, index for a folder's index, the
// system map and the wiki's own, other for the rest); what the inbox's tool
// lines show of its use (see toolUse); and the code status's entries.
type WikiFigures struct {
	Pages    map[string]int `json:"pages"`
	Complete bool           `json:"complete"`
	toolUse
	Code CodeFigures `json:"code_status"`
}

// CodeFigures is the code status the day's maintenance takes, whether one
// was found: the system pages it holds, those that cite code, the entries
// of each page's code in a repo by what measuring them found, and the pages
// with an entry whose repo has no clone on this Mac.
type CodeFigures struct {
	Found        bool           `json:"found"`
	Pages        int            `json:"pages"`
	CitingCode   int            `json:"pages_citing_code"`
	Entries      map[string]int `json:"entries"`
	PagesNoClone int            `json:"pages_no_clone"`
}

// wikiPages counts the wiki's pages by kind, passing over dot folders, as
// .git, and anything but a regular file. complete is false past
// maxWikiFiles or when ctx ends.
func wikiPages(ctx context.Context, wiki string) (pages map[string]int, complete bool, err error) {
	pages = map[string]int{"thread": 0, "rules": 0, "knowledge": 0, "system": 0, "index": 0, other: 0}
	seen := 0
	complete = true
	err = filepath.WalkDir(wiki, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			if path == wiki {
				return err
			}
			return nil
		}
		if ctx.Err() != nil || seen == maxWikiFiles {
			complete = false
			return fs.SkipAll
		}
		seen++
		if strings.HasPrefix(e.Name(), ".") && path != wiki {
			if e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(wiki, path)
		pages[pageKind(filepath.ToSlash(rel))]++
		return nil
	})
	return pages, complete, err
}

// pageKind is the kind of the page at rel, a path in the wiki.
func pageKind(rel string) string {
	parts := strings.Split(rel, "/")
	name := parts[len(parts)-1]
	switch {
	case name == "index.md":
		return "index"
	case len(parts) == 2 && parts[0] == knowledgepage.Folder:
		if _, ok := knowledgepage.ParseID(name); ok {
			return "knowledge"
		}
	case len(parts) == 2 && parts[0] == "systems":
		if name == "map.md" {
			return "index"
		}
		return "system"
	case len(parts) == 3 && parts[0] == "general" && parts[1] == "threads",
		len(parts) == 4 && parts[0] == "projects" && parts[2] == "threads":
		return "thread"
	case len(parts) == 2 && parts[0] == "general" && name == "rules.md",
		len(parts) == 3 && parts[0] == "projects" && name == "rules.md":
		return "rules"
	}
	return other
}

// codeFigures reads the code status, <state>/code-status.json, as the
// doctor's system code row does.
func codeFigures(d paths.Dirs) (CodeFigures, error) {
	c := CodeFigures{Entries: map[string]int{}}
	s, found, err := coderef.Load(d.State, coderef.Limit)
	if err != nil || !found {
		return c, err
	}
	c.Found, c.Pages = true, len(s.Pages)
	for _, p := range s.Pages {
		if len(p.Repos) > 0 {
			c.CitingCode++
		}
		noClone := false
		for _, r := range p.Repos {
			c.Entries[known(r.State, codeStates)]++
			noClone = noClone || r.State == coderef.NoClone
		}
		if noClone {
			c.PagesNoClone++
		}
	}
	return c, nil
}
