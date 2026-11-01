package resume

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/searchindex"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// buildIndex brings the search index up to date with the fixture's inbox.
func buildIndex(t *testing.T, o Options) {
	t.Helper()
	if _, err := searchindex.Catchup(context.Background(), o.Dirs, "index"); err != nil {
		t.Fatal(err)
	}
}

// indexed builds the index and searches with it, leaving it in place.
func indexed(t *testing.T, so SearchOptions) string {
	t.Helper()
	if _, err := searchindex.Catchup(context.Background(), so.Dirs, "index"); err != nil {
		t.Fatal(err)
	}
	out, err := Search(context.Background(), so)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// bulkSay appends entries to a Codex session's file in one write, as
// appends would leave them, without an fsync each.
func bulkSay(t *testing.T, o Options, sid string, when time.Time, texts ...string) {
	t.Helper()
	store := inbox.Store{Root: o.Dirs.Inbox()}
	file := store.Path(o.Project.Key, "codex", sid)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	var b []byte
	for i, text := range texts {
		e := inbox.Entry{V: 1, Host: "codex", Session: sid, Project: o.Project.Key, Branch: "main", TS: when.Add(time.Duration(i) * time.Second).Format(time.RFC3339), Role: "user", Kind: "prompt", Text: text, Origin: "hook"}
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b = append(append(b, line...), '\n')
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		t.Fatal(err)
	}
}

var throughIndex = regexp.MustCompile(`; searched \d+ files? \([\d.]+ MB\) through the index of \d{4}-\d\d-\d\d \d\d:\d\d`)

// The index finds what the scan finds, for words, phrases, short tokens,
// terms JSON escapes, mixed case, word starts and camelCase parts, and
// every ref it prints opens with eva-md show. search runs each query both
// ways and compares them; here the index's own output is checked too.
func TestSearchIndexAndScanAgree(t *testing.T) {
	o := fixture(t)
	k := o.Project.Key
	lines := []string{
		"Codename Saffron ships on Friday",
		"the ReadMax helper reads at most a line",
		"if a<b && x&y { open(\"C:\\temp\\new\") }",
		"GUI build broke, the UI is ready, ui-kit shipped",
		"Résumé ÜBER ǅemal an der Straße",
		"switch the amber light, then the amber switch",
		"gpt5 and gpt4o are models; SecretLines too",
		"a plain line about quokkas and their migration",
		"data<base is not a<b",
	}
	for i, line := range lines {
		say(t, o, k, fmt.Sprintf("agree-%d", i), o.Now.Add(-time.Duration(len(lines)-i)*time.Hour), "user", "prompt", line)
	}
	say(t, o, k, "agree-tool", o.Now.Add(-30*time.Minute), "tool", "tool", "Read internal/resume/search.go")
	queries := [][]string{
		{"saffron"}, {"Saffron", "friday"}, {"amber switch"}, {"amber", "switch"}, {"max"}, {"readmax"}, {"ReadMax"},
		{"a<b"}, {"x&y"}, {`"C:\temp\new"`}, {"ui"}, {"gui"}, {"RÉSUMÉ"}, {"strasse"}, {"STRAßE"}, {"ǆemal"},
		{"gpt"}, {"gpt5"}, {"lines"}, {"secret"}, {"quok"}, {"migr", "quokka"}, {"search.go"}, {"nothing", "here"},
	}
	for _, terms := range queries {
		search(t, searchOptions(o, terms...))
	}
	buildIndex(t, o)
	for _, terms := range queries {
		out, err := Search(context.Background(), searchOptions(o, terms...))
		if err != nil {
			t.Fatal(err)
		}
		if !throughIndex.MatchString(out) {
			t.Fatalf("%q did not use the index:\n%s", terms, out)
		}
		for _, h := range hits(t, out) {
			ref := refShape.FindString(h)
			if text, err := Show(context.Background(), o.Dirs, o.Config, ref); err != nil || text == "" {
				t.Fatalf("%q: %s did not open: %v", terms, ref, err)
			}
		}
	}
}

// A match the index can't see, inside a word, is still found: when the
// index finds nothing, the files it covers are scanned too, and the trailer
// says so; with nothing anywhere, "No matches." after that scan.
func TestSearchFallsBackToTheScan(t *testing.T) {
	o := fixture(t)
	say(t, o, o.Project.Key, "fallback", o.Now.Add(-time.Hour), "user", "prompt", "call readmax here")
	out := indexed(t, searchOptions(o, "max"))
	if got := hits(t, out); len(got) != 1 || !strings.HasSuffix(got[0], "· call readmax here") || !strings.HasSuffix(out, "; the index found nothing, so the 1 file it covers (0.0 MB) was scanned too.") {
		t.Fatalf("an infix match:\n%s", out)
	}
	out = indexed(t, searchOptions(o, "pomegranate"))
	if !strings.HasPrefix(out, "No matches.\nScanned 0 files") || !strings.Contains(out, "the index found nothing, so the 1 file it covers") {
		t.Fatalf("no match:\n%s", out)
	}
}

// Hits the index proposes and hits scanned in the files' tails rank
// together by score within a tier: an older, denser line before a newer one
// that only mentions the word; without an index, newest first.
func TestSearchMixesIndexAndTail(t *testing.T) {
	o := fixture(t)
	k := o.Project.Key
	say(t, o, k, "mixed", o.Now.Add(-3*time.Hour), "user", "prompt", "kiwi kiwi")
	say(t, o, k, "other", o.Now.Add(-2*time.Hour), "user", "prompt", "nothing of note at all here today")
	buildIndex(t, o)
	say(t, o, k, "mixed", o.Now.Add(-time.Hour), "user", "prompt", "a kiwi among many other words in a long sentence")
	out, err := Search(context.Background(), searchOptions(o, "kiwi"))
	if err != nil {
		t.Fatal(err)
	}
	got := hits(t, out)
	if len(got) != 2 || !strings.HasSuffix(got[0], "s:mixed#1 · kiwi kiwi") || !strings.HasSuffix(got[1], "s:mixed#2 · a kiwi among many other words in a long sentence") || !strings.Contains(out, "then rarer and denser matches, then newest") || !strings.Contains(out, "Scanned 1 file (") {
		t.Fatalf("index and tail:\n%s", out)
	}
	if err := os.RemoveAll(o.Dirs.SearchIndex()); err != nil {
		t.Fatal(err)
	}
	out, _ = Search(context.Background(), searchOptions(o, "kiwi"))
	if got := hits(t, out); len(got) != 2 || !strings.HasSuffix(got[0], "s:mixed#2 · a kiwi among many other words in a long sentence") {
		t.Fatalf("without an index:\n%s", out)
	}
}

// reportedDeadline is a context whose deadline is at, as a search reads it
// to decide on a top-up, but that never ends, so a slow machine can't cut
// the search short.
type reportedDeadline struct {
	context.Context
	at time.Time
}

func (c reportedDeadline) Deadline() (time.Time, bool) { return c.at, true }

// With a time limit and TopUp, as eva-md search sets it, a search indexes
// up to 16 MiB of what the index lacks in scope for the next search, once
// that is 4 MiB or more and 1.5 s of its time is left; below that, or with
// less time, or with no limit, or without TopUp, as the MCP server's search
// runs, it writes nothing. The deadlines are reported, never reached, so how fast the
// machine scans changes neither what a search finds nor whether it tops
// up: plenty of time is 10 minutes, and too little is just under 1.5 s,
// which only gets less as the search runs.
func TestSearchTopUpThresholds(t *testing.T) {
	const plenty = 10 * time.Minute
	filler := strings.Repeat("filler words that pad the inbox out to its size; ", 20)
	plant := func(t *testing.T, o Options, mb int) {
		t.Helper()
		var texts []string
		for size := 0; size < mb<<20; size += len(filler) + 200 {
			texts = append(texts, filler)
		}
		texts = append(texts, "the kumquat line")
		bulkSay(t, o, "topup", o.Now.Add(-time.Hour), texts...)
	}
	manifest := func(o Options) bool {
		_, err := os.Stat(filepath.Join(o.Dirs.SearchIndex(), safeProject(o.Project), "manifest.json"))
		return err == nil
	}
	// search searches with left of its time to go, or with no time limit
	// when left is 0, and run does with TopUp.
	search := func(t *testing.T, o Options, left time.Duration, topUp bool) string {
		t.Helper()
		ctx := context.Background()
		if left > 0 {
			ctx = reportedDeadline{Context: ctx, at: time.Now().Add(left)}
		}
		so := searchOptions(o, "kumquat")
		so.TopUp = topUp
		out, err := Search(ctx, so)
		if err != nil {
			t.Fatal(err)
		}
		if got := hits(t, out); len(got) != 1 {
			t.Fatalf("the planted line:\n%s", out)
		}
		return out
	}
	run := func(t *testing.T, o Options, left time.Duration) string {
		t.Helper()
		return search(t, o, left, true)
	}
	t.Run("4 MiB", func(t *testing.T) {
		o := fixture(t)
		plant(t, o, 5)
		out := run(t, o, plenty)
		if !manifest(o) || !regexp.MustCompile(`; indexed 1 file \([\d.]+ MB\) for the next search\.$`).MatchString(out) {
			t.Fatalf("no top-up:\n%s", out)
		}
		// The next search reads what was indexed through the index.
		if out := run(t, o, plenty); !throughIndex.MatchString(out) {
			t.Fatalf("the next search:\n%s", out)
		}
	})
	t.Run("1 MiB", func(t *testing.T) {
		o := fixture(t)
		plant(t, o, 1)
		if out := run(t, o, plenty); manifest(o) || strings.Contains(out, "indexed") {
			t.Fatalf("a top-up of 1 MiB:\n%s", out)
		}
	})
	t.Run("little time left", func(t *testing.T) {
		o := fixture(t)
		plant(t, o, 5)
		if out := run(t, o, searchindex.TopUpReserve-time.Millisecond); manifest(o) {
			t.Fatalf("a top-up with under 1.5 s left:\n%s", out)
		}
	})
	t.Run("no time limit", func(t *testing.T) {
		o := fixture(t)
		plant(t, o, 5)
		if out := run(t, o, 0); manifest(o) {
			t.Fatalf("a top-up with no time limit:\n%s", out)
		}
	})
	t.Run("without TopUp", func(t *testing.T) {
		o := fixture(t)
		plant(t, o, 5)
		out := search(t, o, plenty, false)
		if _, err := os.Stat(o.Dirs.SearchIndex()); !os.IsNotExist(err) || strings.Contains(out, "indexed") {
			t.Fatalf("a top-up without TopUp: %v\n%s", err, out)
		}
	})
}

// A phrase held by more lines than the index reads again, 200 for 10 hits:
// the 200 best candidates, newest first among equals, hold both words but
// the phrase only 3 times, so the files the index covers are scanned too,
// and a search shows the 10 hits and the count the scan finds, of every
// term or, with a term nowhere, of some. With "kiwi", the candidates
// holding every term's words are the 5 kiwi lines, which hold only kiwi as
// search matches it, and each is shown and counted once. search compares
// the two.
func TestSearchPhraseBeyondTheCandidatesRead(t *testing.T) {
	o := fixture(t)
	var texts []string
	for i := 0; i < 30; i++ {
		texts = append(texts, fmt.Sprintf("the amber switch %d", i))
	}
	for i := 0; i < 5; i++ {
		texts = append(texts, fmt.Sprintf("switch to amber kiwi %d", 500+i))
	}
	for i := 0; i < 250; i++ {
		texts = append(texts, fmt.Sprintf("switch to amber %d", 1000+i))
	}
	for i := 0; i < 3; i++ {
		texts = append(texts, fmt.Sprintf("the amber switch %d", 2000+i))
	}
	bulkSay(t, o, "phrase", o.Now.Add(-time.Hour), texts...)
	for _, c := range []struct {
		terms []string
		more  string
	}{{[]string{"amber switch"}, "\n23 more; refine"}, {[]string{"amber switch", "nowhere"}, "\n23 more; refine"}, {[]string{"amber switch", "kiwi"}, "\n28 more; refine"}} {
		terms := c.terms
		so := searchOptions(o, terms...)
		if out := search(t, so); len(hits(t, out)) != 10 || !strings.Contains(out, c.more) {
			t.Fatalf("%q:\n%s", terms, out)
		}
		out := indexed(t, so)
		if !strings.Contains(out, "; the index's best candidates held too few matches, so the 1 file it covers (") || !strings.Contains(out, c.more) {
			t.Fatalf("%q through the index:\n%s", terms, out)
		}
		seen := map[string]bool{}
		for _, h := range hits(t, out) {
			ref := refShape.FindString(h)
			if seen[ref] {
				t.Fatalf("%q through the index shows %s twice:\n%s", terms, ref, out)
			}
			seen[ref] = true
		}
		if err := os.RemoveAll(o.Dirs.SearchIndex()); err != nil {
			t.Fatal(err)
		}
	}
	// With a tail the index lacks, which is scanned as a tail, only what
	// the index covers is scanned again.
	buildIndex(t, o)
	bulkSay(t, o, "phrase", o.Now.Add(-time.Minute), "the amber switch 3000", "the amber switch 3001")
	for _, terms := range [][]string{{"amber switch"}, {"amber switch", "nowhere"}} {
		so := searchOptions(o, terms...)
		out, err := Search(context.Background(), so)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(o.Dirs.SearchIndex(), o.Dirs.SearchIndex()+".off"); err != nil {
			t.Fatal(err)
		}
		scanned, err := Search(context.Background(), so)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(o.Dirs.SearchIndex()+".off", o.Dirs.SearchIndex()); err != nil {
			t.Fatal(err)
		}
		sameHits(t, terms, scanned, out)
		if !strings.Contains(scanned, "\n25 more; refine") {
			t.Fatalf("%q:\n%s", terms, scanned)
		}
	}
}

// A file rewritten in place and grown, its first line kept, so the index's
// end no longer starts a line, is scanned whole and each of its lines shown
// once, with the index's entries for it counted as one stale entry, until
// the next catch-up indexes it whole.
func TestSearchRewrittenFileShowsEachLineOnce(t *testing.T) {
	o := fixture(t)
	k := o.Project.Key
	say(t, o, k, "rewrite", o.Now.Add(-2*time.Hour), "user", "prompt", "lime one")
	say(t, o, k, "rewrite", o.Now.Add(-time.Hour), "user", "prompt", "lime two")
	buildIndex(t, o)
	file := (inbox.Store{Root: o.Dirs.Inbox()}).Path(k, "codex", "rewrite")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The same inode, grown, its first line as it was.
	_, err = f.WriteAt([]byte(strings.Replace(string(data), `"lime two"`, `"lime two, now a longer line"`, 1)), 0)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Search(context.Background(), searchOptions(o, "lime"))
	if err != nil {
		t.Fatal(err)
	}
	got := hits(t, out)
	slices.Sort(got)
	if len(got) != 2 || !strings.HasSuffix(got[0], "s:rewrite#1 · lime one") || !strings.HasSuffix(got[1], "s:rewrite#2 · lime two, now a longer line") || !strings.Contains(out, "; 1 stale index entry skipped;") {
		t.Fatalf("a file rewritten in place:\n%s", out)
	}
	// The next catch-up indexes it whole, under its next generation, so
	// its new text is still found and a line appended after has its own
	// line number.
	buildIndex(t, o)
	say(t, o, k, "rewrite", o.Now.Add(-time.Minute), "user", "prompt", "lime three")
	if out, err = Search(context.Background(), searchOptions(o, "lime")); err != nil {
		t.Fatal(err)
	}
	got = hits(t, out)
	slices.Sort(got)
	if len(got) != 3 || !strings.HasSuffix(got[0], "s:rewrite#1 · lime one") || !strings.HasSuffix(got[1], "s:rewrite#2 · lime two, now a longer line") || !strings.HasSuffix(got[2], "s:rewrite#3 · lime three") || strings.Contains(out, "stale") || !throughIndex.MatchString(out) {
		t.Fatalf("after a catch-up:\n%s", out)
	}
}

// A file deleted since it was indexed has no hits; one replaced is scanned
// whole; one rewritten in place to the same size, which only reading its
// lines again shows, has its lines counted as stale.
func TestSearchDeadAndStaleFiles(t *testing.T) {
	o := fixture(t)
	k := o.Project.Key
	store := inbox.Store{Root: o.Dirs.Inbox()}
	say(t, o, k, "deleted", o.Now.Add(-3*time.Hour), "user", "prompt", "lime in a deleted chat")
	say(t, o, k, "replaced", o.Now.Add(-2*time.Hour), "user", "prompt", "lime in a replaced chat")
	say(t, o, k, "rewritten", o.Now.Add(-time.Hour), "user", "prompt", "lime short")
	say(t, o, k, "rewritten", o.Now.Add(-time.Hour), "user", "prompt", "lime in a much longer line than the first")
	buildIndex(t, o)
	if err := os.Remove(store.Path(k, "codex", "deleted")); err != nil {
		t.Fatal(err)
	}
	replaced := store.Path(k, "codex", "replaced")
	data, err := os.ReadFile(replaced)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaced+".new", []byte(strings.Replace(string(data), "lime in a replaced chat", "lime, the chat replaced", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replaced+".new", replaced); err != nil {
		t.Fatal(err)
	}
	rewritten := store.Path(k, "codex", "rewritten")
	data, err = os.ReadFile(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	first, second, _ := strings.Cut(strings.TrimSuffix(string(data), "\n"), "\n")
	f, err := os.OpenFile(rewritten, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteAt([]byte(second+"\n"+first+"\n"), 0) // the same bytes, the lines swapped
	f.Close()
	out, err := Search(context.Background(), searchOptions(o, "lime"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "deleted chat") || !strings.Contains(out, "· lime, the chat replaced") || !strings.Contains(out, "; 2 stale index entries skipped; eva-md index --rebuild builds the index again") {
		t.Fatalf("dead and stale files:\n%s", out)
	}
}

// A damaged segment makes the search scan the folder's files, and the
// trailer says the folder's index is unusable.
func TestSearchDamagedFolderScans(t *testing.T) {
	o := fixture(t)
	say(t, o, o.Project.Key, "damaged", o.Now.Add(-time.Hour), "user", "prompt", "guava still found")
	buildIndex(t, o)
	dir := filepath.Join(o.Dirs.SearchIndex(), safeProject(o.Project))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".idx") {
			file := filepath.Join(dir, e.Name())
			data, _ := os.ReadFile(file)
			data[len(data)/3] ^= 0xff
			os.WriteFile(file, data, 0o600)
		}
	}
	out, err := Search(context.Background(), searchOptions(o, "guava"))
	if err != nil {
		t.Fatal(err)
	}
	if got := hits(t, out); len(got) != 1 || !strings.Contains(out, "\nScanned 1 file (") || !strings.Contains(out, "; the index for 1 project folder is unusable, so its files were scanned: github.com__team__resume; eva-md index rebuilds it") {
		t.Fatalf("a damaged index:\n%s", out)
	}
}

// When more documents hold every token of a phrase than the index reads
// again, the rest are counted, and the count says "about", since a phrase
// is matched only on the line itself.
func TestSearchPhraseCountSaysAbout(t *testing.T) {
	o := fixture(t)
	var texts []string
	for i := 0; i < 300; i++ {
		if i%2 == 0 {
			texts = append(texts, fmt.Sprintf("the amber switch %d", i))
		} else {
			texts = append(texts, fmt.Sprintf("switch to amber %d", i))
		}
	}
	bulkSay(t, o, "phrase", o.Now.Add(-time.Hour), texts...)
	out := indexed(t, searchOptions(o, "amber switch"))
	if got := hits(t, out); len(got) != 10 || !regexp.MustCompile(`\nabout \d+ more; refine with other terms\n`).MatchString(out) {
		t.Fatalf("a phrase's count:\n%s", out)
	}
	// Plain words always hold as the index finds them: an exact count.
	out = indexed(t, searchOptions(o, "amber"))
	if !strings.Contains(out, "\n290 more; refine with other terms\n") {
		t.Fatalf("a word's count:\n%s", out)
	}
}

// Within a tier the index's scores rank hits: more occurrences, a shorter
// line, a rarer word in the any-term fallback; identical lines come newest
// first, and a hit in the case typed still comes before a better score.
func TestSearchRanksByScore(t *testing.T) {
	o := fixture(t)
	k := o.Project.Key
	for n, c := range []struct {
		terms []string
		texts []string // oldest first
		want  []int
	}{
		{[]string{"plum"}, []string{"plum plum pear", "plum pear fig"}, []int{0, 1}},
		{[]string{"cherry"}, []string{"cherry pie", "cherry and a long list of other fruit words"}, []int{0, 1}},
		{[]string{"rareword", "commonword"}, []string{"rareword alone", "commonword one", "commonword two", "commonword three"}, []int{0, 3, 2, 1}},
		{[]string{"lychee"}, []string{"lychee jelly", "lychee jelly"}, []int{1, 0}},
		{[]string{"Mango"}, []string{"Mango with a long list of other fruit words", "mango mango"}, []int{0, 1}},
	} {
		for i, text := range c.texts {
			say(t, o, k, fmt.Sprintf("score-%d-%d", n, i), o.Now.Add(-time.Duration(len(c.texts)-i)*time.Hour), "user", "prompt", text)
		}
		out := indexed(t, searchOptions(o, c.terms...))
		got := hits(t, out)
		if len(got) != len(c.want) {
			t.Fatalf("%q:\n%s", c.terms, out)
		}
		for i, want := range c.want {
			if !strings.HasSuffix(got[i], fmt.Sprintf("s:score-%d-%d#1 · %s", n, want, c.texts[want])) {
				t.Fatalf("%q: rank %d is not %q:\n%s", c.terms, i+1, c.texts[want], out)
			}
		}
	}
}

// The deadline can pass while the index's candidates are read again: the
// search stops there and says it was cut short.
func TestSearchDeadlineDuringVerification(t *testing.T) {
	o := fixture(t)
	var texts []string
	for i := 0; i < 50; i++ {
		texts = append(texts, fmt.Sprintf("durian %d", i))
	}
	bulkSay(t, o, "deadline", o.Now.Add(-time.Hour), texts...)
	buildIndex(t, o)
	out, err := Search(&countdown{Context: context.Background(), left: 10}, searchOptions(o, "durian"))
	if err != nil || !strings.Contains(out, "; cut short by the time limit; older files were not searched") || len(hits(t, out)) == 0 {
		t.Fatalf("cut short: %v\n%s", err, out)
	}
}

// The index's hits are kept when the deadline passes as the files it covers
// are scanned again, after its best candidates held too few: right after
// those are read, at one Err check each, or partway through that scan.
func TestSearchKeepsTheIndexHitsWhenTheFillIsCut(t *testing.T) {
	o := fixture(t)
	var texts []string
	for i := 0; i < 250; i++ {
		texts = append(texts, fmt.Sprintf("switch to amber %d", 1000+i))
	}
	for i := 0; i < 3; i++ {
		texts = append(texts, fmt.Sprintf("the amber switch %d", 2000+i))
	}
	bulkSay(t, o, "phrase", o.Now.Add(-time.Hour), texts...)
	buildIndex(t, o)
	so := searchOptions(o, "amber switch")
	t.Cleanup(func() { indexHook = nil })
	for _, past := range []int{0, 100} {
		c := &countdown{Context: context.Background(), left: math.MaxInt}
		indexHook = func(stage string) {
			if stage == "all" {
				c.left = searchindex.K(so.Limit) + past
			}
		}
		out, err := Search(c, so)
		if err != nil {
			t.Fatal(err)
		}
		got := hits(t, out)
		slices.Sort(got)
		if len(got) != 3 || !strings.HasSuffix(got[0], "#251 · the amber switch 2000") || !strings.HasSuffix(got[2], "#253 · the amber switch 2002") || !strings.Contains(out, "; cut short by the time limit;") || !strings.Contains(out, "; the index's best candidates held too few matches, so the ") {
			t.Fatalf("cut %d Err checks past the candidates read:\n%s", past, out)
		}
	}
}

// With --pages, page lines are scored with the inbox index's statistics.
func TestSearchPagesOnlyUsesInboxStats(t *testing.T) {
	o := fixture(t)
	w := pageWiki(t, o)
	say(t, o, o.Project.Key, "pages-stats", o.Now.Add(-time.Hour), "user", "prompt", "quince words for the statistics")
	p := threadpage.Page{Title: "t"}
	p, _ = threadpage.Apply(p, threadpage.Update{Date: "2026-10-27", Events: []threadpage.Event{
		{Kind: "finding", Text: "quince and a long list of other fruit words", Tag: "observed"},
		{Kind: "finding", Text: "quince quince", Tag: "observed"},
	}})
	savePage(t, w, "projects/resume/threads/main", "main", o.Now.Add(-time.Hour), p)
	so := withPages(o, "quince")
	so.Pages = true
	plain := search(t, so)
	if strings.Contains(plain, "rarer") {
		t.Fatalf("scored without an index:\n%s", plain)
	}
	out := indexed(t, so)
	got := hits(t, out)
	if !strings.Contains(out, "then rarer and denser matches, then newest") || len(got) != 2 || !strings.HasSuffix(got[0], "finding: quince quince") {
		t.Fatalf("pages with the inbox's statistics:\n%s", out)
	}
}

// Nothing said off the record shows, whether its marker is in a file's tail
// or the index stopped past it, and nothing of it reaches the index.
func TestSearchPrivacy(t *testing.T) {
	o := fixture(t)
	k := o.Project.Key
	say(t, o, k, "tail-marker", o.Now.Add(-3*time.Hour), "user", "prompt", "public fig")
	say(t, o, k, "indexed-marker", o.Now.Add(-3*time.Hour), "event", "event", "off_record")
	buildIndex(t, o)
	say(t, o, k, "tail-marker", o.Now.Add(-2*time.Hour), "event", "event", "off_record")
	say(t, o, k, "tail-marker", o.Now.Add(-time.Hour), "user", "prompt", "zzzprivatezzz fig in the tail")
	say(t, o, k, "indexed-marker", o.Now.Add(-time.Hour), "user", "prompt", "zzzprivatezzz fig after an indexed marker")
	for _, terms := range [][]string{{"fig"}, {"zzzprivatezzz"}} {
		out, err := Search(context.Background(), searchOptions(o, terms...))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "zzzprivatezzz fig") {
			t.Fatalf("%q showed an off-the-record line:\n%s", terms, out)
		}
	}
	buildIndex(t, o)
	err := filepath.WalkDir(o.Dirs.SearchIndex(), func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "privatezzz") {
			t.Fatalf("%s holds an off-the-record word", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A fault in the index path never takes a search down: it turns the index
// off and scans every file, as without an index, whether it comes as the
// candidates holding every term are read or, later, those holding some,
// with a file's tail already scanned.
func TestSearchRecoversFromAnIndexPanic(t *testing.T) {
	o := fixture(t)
	say(t, o, o.Project.Key, "panic", o.Now.Add(-2*time.Hour), "user", "prompt", "jackfruit survives")
	buildIndex(t, o)
	say(t, o, o.Project.Key, "panic", o.Now.Add(-time.Hour), "user", "prompt", "jackfruit in the tail")
	// The time a search took, "12 ms" or, on a slow machine, "1.2 s", is no
	// part of what is compared.
	strip := func(s string) string { return regexp.MustCompile(`, (?:\d+ ms|\d+\.\d s)\.$`).ReplaceAllString(s, ".") }
	for _, tc := range []struct {
		stage string
		terms []string
	}{
		{"all", []string{"jackfruit"}},
		{"some", []string{"jackfruit", "durian"}},
	} {
		saved, err := os.MkdirTemp(t.TempDir(), "index")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(o.Dirs.SearchIndex(), filepath.Join(saved, "search")); err != nil {
			t.Fatal(err)
		}
		plain, err := Search(context.Background(), searchOptions(o, tc.terms...))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(saved, "search"), o.Dirs.SearchIndex()); err != nil {
			t.Fatal(err)
		}
		indexHook = func(stage string) {
			if stage == tc.stage {
				panic("a fault in the index")
			}
		}
		out, err := Search(context.Background(), searchOptions(o, tc.terms...))
		indexHook = nil
		if err != nil {
			t.Fatal(err)
		}
		if strip(out) != strip(plain) || len(hits(t, out)) != 2 {
			t.Fatalf("a fault while reading %s:\n%s\nwithout an index:\n%s", tc.stage, out, plain)
		}
	}
}

// A fault as the candidates holding some terms are read, after the
// candidates holding every term's words were read and matched only some of
// the terms, still finds every line: the scan of every file reads again the
// lines the index had read.
func TestSearchRecoversFromAFaultAfterPartialMatches(t *testing.T) {
	o := fixture(t)
	var texts []string
	for i := 0; i < 5; i++ {
		texts = append(texts, fmt.Sprintf("switch to amber kiwi %d", 500+i))
	}
	for i := 0; i < 3; i++ {
		texts = append(texts, fmt.Sprintf("the amber switch %d", 2000+i))
	}
	bulkSay(t, o, "fault", o.Now.Add(-time.Hour), texts...)
	so := searchOptions(o, "amber switch", "kiwi")
	plain, err := Search(context.Background(), so)
	if err != nil {
		t.Fatal(err)
	}
	buildIndex(t, o)
	indexHook = func(stage string) {
		if stage == "some" {
			panic("a fault in the index")
		}
	}
	out, err := Search(context.Background(), so)
	indexHook = nil
	if err != nil {
		t.Fatal(err)
	}
	// The time a search took is no part of what is compared.
	strip := func(s string) string { return regexp.MustCompile(`, (?:\d+ ms|\d+\.\d s)\.$`).ReplaceAllString(s, ".") }
	if strip(out) != strip(plain) || len(hits(t, out)) != 8 {
		t.Fatalf("a fault as some terms' candidates were read:\n%s\nwithout an index:\n%s", out, plain)
	}
}

// A file rewritten in place to the same size, with a line end moved by a
// byte, leaves the index's candidates stale. Nothing else matches, so the
// files the index covers are scanned again, and the line now at a stale
// candidate's line number is found there, never taken as read already.
func TestSearchFindsALineAtAStaleCandidatesNumber(t *testing.T) {
	o := fixture(t)
	k := o.Project.Key
	say(t, o, k, "same", o.Now.Add(-2*time.Hour), "user", "prompt", "lime aa")
	say(t, o, k, "same", o.Now.Add(-time.Hour), "user", "prompt", "lime bb")
	buildIndex(t, o)
	file := (inbox.Store{Root: o.Dirs.Inbox()}).Path(k, "codex", "same")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.Replace(strings.Replace(string(data), `"lime aa"`, `"lime a"`, 1), `"lime bb"`, `"lime bbb"`, 1)
	if len(rewritten) != len(data) {
		t.Fatal("the rewrite changed the file's size")
	}
	f, err := os.OpenFile(file, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte(rewritten), 0)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	out, err := Search(context.Background(), searchOptions(o, "lime bb"))
	if err != nil {
		t.Fatal(err)
	}
	if got := hits(t, out); len(got) != 1 || !strings.HasSuffix(got[0], "s:same#2 · lime bbb") {
		t.Fatalf("the rewritten line:\n%s", out)
	}
}

// With --all, every project folder's index is used, and a project folder
// not indexed yet is scanned and named.
func TestSearchAllUsesEveryFolder(t *testing.T) {
	o := fixture(t)
	capture(t, o, "codex", "all-one", "main", o.Now.Add(-2*time.Hour), "nectarine here")
	general := o
	general.Project = identity.General()
	capture(t, general, "codex", "all-two", "default", o.Now.Add(-time.Hour), "nectarine in general")
	buildIndex(t, o)
	other := o
	other.Project = identity.Project{Kind: identity.KindRepo, Key: "github.com__team__other", Slug: "other", Remote: "github.com/team/other", Branch: "main"}
	capture(t, other, "codex", "all-three", "main", o.Now.Add(-30*time.Minute), "nectarine elsewhere")
	so := searchOptions(o, "nectarine")
	so.All = true
	out, err := Search(context.Background(), so)
	if err != nil {
		t.Fatal(err)
	}
	if got := hits(t, out); len(got) != 3 || !strings.Contains(out, " in 3 projects, ") || !strings.Contains(out, "; 1 project folder is not indexed yet, so its files were scanned: github.com__team__other") || !throughIndex.MatchString(out) {
		t.Fatalf("--all:\n%s", out)
	}
}
