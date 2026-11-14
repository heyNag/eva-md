package health

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/searchindex"
)

func init() {
	registerRows(500, "search index", func(r *Report, in *inspection) {
		c, _ := in.config()
		searchIndexRow(r, in.d, Switch.On(c))
	})
}

// inspectSearchIndex adds the search index's row, read from its manifests
// and the folder listings alone: how much of the inbox it covers, that it
// is not built yet, or the project folders whose index is damaged, which
// search scans instead. Search works without it either way. The doctor
// repairs nothing here; eva-md index and the day's maintenance do.
func inspectSearchIndex(r *Report, d paths.Dirs) { searchIndexRow(r, d, false) }

// searchIndexRow is inspectSearchIndex; with rest, as with the switch
// health on, an index that covers less than all of the inbox says that
// search scans the rest.
func searchIndexRow(r *Report, d paths.Dirs, rest bool) {
	st := searchindex.Inspect(d)
	switch n := len(st.Damaged); {
	case n > 0:
		r.add("search index", "FAIL", fmt.Sprintf("the index for %d project %s damaged, so search scans %s files: %s", n, plural(n, "folder is", "folders are"), plural(n, "its", "their"), list(st.Damaged)), "run eva-md index")
	case st.Indexed == 0 && st.Inbox == 0:
		r.add("search index", "UNKNOWN", "nothing to index yet", "")
	case st.Indexed == 0:
		r.add("search index", "UNKNOWN", "no index yet, so search scans the inbox", "run eva-md index, or wait for the day's maintenance")
	default:
		pct := 100.0
		if st.Inbox > 0 {
			pct = 100 * float64(st.Covered) / float64(st.Inbox)
		}
		detail := fmt.Sprintf("covers %s of %s MB of the inbox (%.1f%%) in %d %s; %s MB in %d %s", megabytes(st.Covered), megabytes(st.Inbox), pct, st.Indexed, plural(st.Indexed, "project", "projects"), megabytes(st.Size), st.Segments, plural(st.Segments, "segment", "segments"))
		if !st.Built.IsZero() {
			detail += "; built " + st.Built.Local().Format("2006-01-02 15:04") + " by " + st.By
		}
		if rest && st.Covered < st.Inbox {
			detail += "; search scans the rest"
		}
		r.add("search index", "PASS", detail, "")
	}
}

// megabytes shows bytes as MB with one decimal and thousands separators,
// such as 1,073.2.
func megabytes(n int64) string {
	s := strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String() + "." + frac
}
