package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// knowledgeStep builds the files made from the knowledge pages again, the
// knowledge map, the folder's index, the recall index and the root index's
// line, when one is missing or out of date, before the day's commit, so the
// commit holds them. It prints a line only when it wrote something. A wiki
// with nothing of knowledge, neither the folder nor a recall index or root
// line left from one, has nothing to build. It writes only a wiki eva-md
// writes (see health.WritableWiki): one a newer eva-md set up, or whose
// schema names another instance, is the commit's to refuse. A knowledge/
// that is not a folder is left alone, as the doctor leaves it. A wiki that
// stayed busy is left for the next run; only an error fails the step.
func knowledgeStep(c config.Config, dirs paths.Dirs, out, errOut io.Writer) int {
	if info, err := os.Lstat(filepath.Join(c.WikiPath, knowledgepage.Folder)); err == nil && !info.IsDir() || !health.WritableWiki(c) {
		return 0
	}
	w, err := okfadapter.Open(c.WikiPath, dirs.Locks())
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: knowledge:", err)
		return 1
	}
	r, err := knowledgepage.Refresh(w, version, true)
	switch {
	case errors.Is(err, okfadapter.ErrLockBusy):
		fmt.Fprintln(out, "maintain: knowledge: the wiki stayed busy; the next run builds the index again")
		return 0
	case err != nil:
		fmt.Fprintln(errOut, "eva-md: knowledge:", err)
		return 1
	case r.Rebuilt:
		fmt.Fprintf(out, "maintain: knowledge: rebuilt the index (%d %s)\n", r.Pages, plural(r.Pages, "page", "pages"))
	}
	return 0
}
