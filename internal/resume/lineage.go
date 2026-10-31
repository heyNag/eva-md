package resume

import (
	"context"
	"time"
)

// Lineage returns the branch a new thread on o.Project.Branch continues
// from, by the rule the brief's lineage fallback uses: the branch of the
// project's most recently active chat that did real work, leaving out
// o.Session's chat and the new thread's own branch. It returns "" when there
// is none. Like the brief, it reads only the project's 64 most recently
// changed session files. The update job uses it for a new thread page's
// parent.
func Lineage(ctx context.Context, o Options) (string, error) {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	all, _, err := readHistories(ctx, o)
	if err != nil {
		return "", err
	}
	for _, h := range all {
		if h.host == o.Host && h.session == o.Session || h.branch == o.Project.Branch || !useful(h) {
			continue
		}
		return h.branch, nil
	}
	return "", nil
}
