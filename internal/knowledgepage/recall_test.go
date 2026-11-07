package knowledgepage

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/identity"
)

// recallRows is the labelled fixture's index: facts of two projects, some
// for every project, as the recall index lists them.
var recallRows = func() []Row {
	type fact struct{ kind, scope, project, title, statement string }
	facts := []fact{
		{Decision, ScopeProject, project, "Inbox format", "The inbox stores entries as JSONL, one file per session"},
		{Constraint, ScopeAll, project, "Hooks stay offline", "Hooks never make a network call and exit successfully on any failure"},
		{Constraint, ScopeProject, project, "Two dependencies only", "The module depends only on okf and BurntSushi toml; add no other dependency"},
		{Fact, ScopeProject, project, "Capture latency target", "Capture p95 must stay under 50 ms and the startup brief p95 under 300 ms"},
		{Procedure, ScopeProject, project, "Release checklist", "Run gofmt, go vet, go test with the race detector and the latency benchmark before a release"},
		{Decision, ScopeProject, project, "Daily commit is signed", "The daily wiki commit is signed with the user's SSH signing key and pushed once a day"},
		{Fact, ScopeProject, other, "Invoice rounding", "Invoices round each line to the cent before summing, using banker's rounding"},
		{Decision, ScopeProject, other, "Billing retries", "Payment retries back off exponentially and stop after five attempts"},
		{Constraint, ScopeProject, other, "Card numbers never logged", "Card numbers are never written to logs or analytics events"},
		{Fact, ScopeProject, project, "Redaction baseline frozen", "The baseline redaction rules are frozen; improve redaction only in the current rules"},
		{Decision, ScopeProject, project, "Search index is a cache", "The search index is a cache built from the inbox and may be deleted at any time"},
		{Fact, ScopeProject, project, "Okf version pinned", "okf is pinned at v0.7.2 and never patched"},
		{Procedure, ScopeAll, project, "Container runtime", "Use the container CLI rather than Docker for local Linux containers"},
		{Decision, ScopeProject, project, "Thread pages by branch", "Each git branch gets its own thread page under projects"},
		{Fact, ScopeProject, other, "Tax service timeout", "The tax service call times out after two seconds and falls back to cached rates"},
		{Constraint, ScopeProject, project, "Tests never touch home", "Tests use a temporary home and never read the real home folder or git config"},
		{Decision, ScopeProject, project, "Model for updates", "The background update uses the cheapest model with low effort"},
		{Fact, ScopeProject, project, "Wiki lock timeout", "Writers wait up to sixty seconds for the wiki lock before giving up"},
		{Procedure, ScopeProject, other, "Refund approval", "Refunds over 500 dollars need a second approver in the admin console"},
		{Decision, ScopeProject, project, "Map lists retired pages", "The knowledge map lists retired pages so no page is orphaned"},
		{Fact, ScopeProject, project, "Prompt hook budget", "The prompt hook has a 300 ms budget and the session start hook 1.5 seconds"},
		{Constraint, ScopeProject, project, "No secrets in logs", "Never print or log captured text or secrets; report counts instead"},
		{Decision, ScopeProject, other, "Currency storage", "Money amounts are stored as integer minor units, never floats"},
		{Fact, ScopeProject, project, "Codex hook trust", "Codex asks the user to trust the plugin hooks again after each plugin upgrade"},
		{Fact, ScopeProject, project, "Wiki page links", "Every wiki page links to the map so okf finds no orphan page"},
		{Decision, ScopeProject, project, "Hook output fenced", "Hook output to the model is fenced as reference notes"},
		{Fact, ScopeProject, project, "Update runs per session", "The update job runs once per session after the chat ends"},
		{Constraint, ScopeProject, project, "Hooks fail closed", "A hook exits successfully on any failure and never blocks the host"},
		{Fact, ScopeProject, project, "Test helpers isolate git", "Test helpers set GIT_CONFIG_GLOBAL and GIT_CONFIG_NOSYSTEM for every test"},
		{Decision, ScopeProject, project, "Wiki commit once a day", "The wiki is committed and pushed once a day by the maintain job"},
		{Procedure, ScopeProject, project, "Page edits by hand", "To edit a wiki page by hand, lock it first so the update leaves it alone"},
		{Fact, ScopeProject, project, "Inbox is redacted", "Secrets are redacted before an entry reaches the inbox"},
		{Decision, ScopeProject, project, "Brief cap", "The startup brief is capped at 8000 characters for Claude and 6000 for Codex"},
		{Fact, ScopeProject, project, "Session file names", "Each inbox session file is named by host and session id"},
		{Decision, ScopeProject, other, "Billing page layout", "The billing page shows invoices newest first with a running total"},
		{Fact, ScopeAll, project, "Default branch main", "New repositories use main as the default branch"},
	}
	var rows []Row
	for i, f := range facts {
		rows = append(rows, Row{ID: Name(i + 1), Kind: f.kind, Tag: "stated", Scope: f.scope, Project: f.project,
			AsOf: fmt.Sprintf("2026-%02d-%02d", 9+i/28, 1+i%28), Title: f.title, Statement: f.statement})
	}
	return rows
}()

// labelled is a prompt, the project it is asked in, and the pages it should
// recall: none for a prompt the facts in its scope don't answer.
type labelled struct {
	prompt string
	want   []string // pages it must recall
	also   []string // pages that help too, right when recalled, never missed
	in     string   // the project it is asked in
	near   []string // in place of want, the pages it must recall when the two projects share a system page
}

// ask is a labelled prompt as the fixture lists it: the prompt, the pages it
// must recall and those that help too.
type ask struct {
	prompt     string
	want, also []string
}

// askedIn returns the prompts asked in the project in.
func askedIn(in string, asks []ask) []labelled {
	var out []labelled
	for _, a := range asks {
		out = append(out, labelled{prompt: a.prompt, want: a.want, also: a.also, in: in})
	}
	return out
}

// unnamed returns the prompts asked in the project in about the other
// project's facts, without naming it: they recall nothing, unless the two
// projects share a system page, when they must recall the pages wanted.
func unnamed(in string, asks []ask) []labelled {
	var out []labelled
	for _, a := range asks {
		out = append(out, labelled{prompt: a.prompt, in: in, near: a.want})
	}
	return out
}

// billingAsks are prompts the other project's facts answer.
var billingAsks = []ask{
	{"how do invoices round line amounts?", []string{"k7"}, nil},
	{"after how many attempts do payment retries stop?", []string{"k8"}, nil},
	{"can we log the card numbers for debugging the checkout?", []string{"k9"}, nil},
	{"what happens when the tax service times out?", []string{"k15"}, nil},
	{"do refunds over 500 dollars need approval?", []string{"k19"}, nil},
	{"should money amounts be stored as floats?", []string{"k23"}, nil},
	{"do we round invoices with banker's rounding?", []string{"k7"}, nil},
}

// filler repeats a few words that no fact holds, as a pasted log does.
func filler(words string, size int) string {
	var b strings.Builder
	for b.Len() < size {
		b.WriteString(words + "\n")
	}
	return b.String()
}

// recallPrompts are the labelled prompts, each asked in one project: the
// first project's facts asked there, the other's asked there by name, in
// the other and, unnamed, in the first, and a few of the first's in the other.
var recallPrompts = slices.Concat(askedIn(project, []ask{
	// Prompts the facts answer.
	{"what format does the inbox use for entries? is it JSONL?", []string{"k1"}, nil},
	{"can the hooks make a network call to fetch the latest version?", []string{"k2"}, nil},
	{"I want to add a new dependency for YAML parsing, is that ok?", []string{"k3"}, nil},
	{"what is the p95 latency target for capture?", []string{"k4"}, nil},
	{"walk me through the release checklist before I tag v1", []string{"k5"}, nil},
	{"how is the daily wiki commit signed?", []string{"k6"}, []string{"k30"}},
	{"should I change the baseline redaction rules to catch this token?", []string{"k10"}, nil},
	{"can I delete the search index to save disk space?", []string{"k11"}, nil},
	{"which okf version are we pinned to? can we patch it?", []string{"k12"}, nil},
	{"should I use docker or the container cli for the linux build?", []string{"k13"}, nil},
	{"does each branch get its own thread page?", []string{"k14"}, nil},
	{"my test reads the real home folder, is that allowed?", []string{"k16"}, nil},
	{"which model does the background update use and at what effort?", []string{"k17"}, nil},
	{"how long do writers wait for the wiki lock?", []string{"k18"}, nil},
	{"why does the knowledge map list retired pages?", []string{"k20"}, nil},
	{"what's the budget of the prompt hook in ms?", []string{"k21"}, nil},
	{"is it fine to log the captured text while debugging?", []string{"k22"}, nil},
	{"why does codex ask to trust the hooks again after upgrading the plugin?", []string{"k24"}, nil},
	{"is the startup brief p95 under 300 ms?", []string{"k4"}, nil},
	{"is the session start hook budget 1.5 seconds?", []string{"k21"}, nil},
	// Prompts that name the other project, of the same owner, and ask
	// about its facts.
	{"in the billing service, are card numbers ever written to the logs?", []string{"k9"}, nil},
	{"billing: how many attempts before payment retries give up?", []string{"k8"}, nil},
	// Prompts the facts don't answer.
	{"rename the variable in main.go to something clearer", nil, nil},
	{"write a haiku about autumn leaves", nil, nil},
	{"what is the capital of France?", nil, nil},
	{"please make the button blue in the settings page", nil, nil},
	{"explain how goroutines and channels work", nil, nil},
	{"update the readme with the new install steps", nil, nil},
	// Near misses that share one word with a fact.
	{"the format of this table looks off, can you align the columns?", nil, nil},
	{"add a retry to the http client", nil, nil},
	{"the release notes need a typo fix", nil, nil},
	{"what does the term hook mean in fishing?", nil, nil},
	{"the tax on my salary seems wrong this month", nil, nil},
	{"the home page hero needs a new image", nil, nil},
	{"which branch should I merge first?", nil, nil},
	{"the session expired while I was away", nil, nil},
	// Near misses that share two words with a fact.
	{"my lock screen timeout is too short on the laptop", nil, nil},
	{"the card on the home page needs a border", nil, nil},
	// Prompts made of words many facts hold.
	{"open the wiki page in the browser", nil, nil},
	{"the main branch build is red again", nil, nil},
	{"the test for the inbox page is flaky", nil, nil},
	{"hello, can you look at the hook and the session settings?", nil, nil},
	{"the landing page layout looks broken on phones", nil, nil},
	{"the session file is too large to open", nil, nil},
	// More prompts the facts answer, in other words.
	{"when does the maintain job commit and push the wiki?", []string{"k30"}, nil},
	{"how is the inbox session file named?", []string{"k34"}, []string{"k1"}},
	{"what's the startup brief cap for codex?", []string{"k33"}, nil},
	{"what should a hook do on failure, can it block the host?", []string{"k28"}, nil},
	{"the update to the billing page broke its layout, what should it show?", []string{"k35"}, nil},
	{"which branch is the default for new repositories?", []string{"k36"}, nil},
	{"are secrets redacted before entries reach the inbox?", []string{"k32"}, nil},
	{"what do the test helpers set for git config?", []string{"k29"}, []string{"k16"}},
	// Very long prompts: a pasted log ending with a question, a pasted
	// stack trace, and pasted prose full of common words.
	{filler("lorem ipsum dolor sit amet consectetur adipiscing elit sed eiusmod", 80<<10) + "\nwhy does the inbox use JSONL files per session?", []string{"k1"}, nil},
	{filler("goroutine 17 [running]: main.process(0xc000010000) /src/main.go:42 +0x1d panic: runtime error: slice bounds out of range", 90<<10), nil, nil},
	{strings.Repeat("Our pipeline builds every branch, runs the unit suite and uploads an artifact to the registry. Reviewers approve the change, and a bot merges it once the checks pass. ", 40) + "Can you shorten this paragraph?", nil, nil},
}),
	// The other project's facts, asked in it, and in the first project
	// without naming it.
	askedIn(other, billingAsks),
	unnamed(project, billingAsks),
	// The first project's facts asked in the other: by name, and without.
	askedIn(other, []ask{
		{"in eva, what is the p95 latency target for capture?", []string{"k4"}, nil},
		{"in eva, the release notes need a typo fix", nil, nil},
	}),
	unnamed(other, []ask{
		{"can I delete the search index to save disk space?", []string{"k11"}, nil},
		{"how long do writers wait for the wiki lock?", []string{"k18"}, nil},
	}),
)

// recallSizes are the index sizes TestRecallPrecision measures, and the
// indexes of each: every row alone, runs of 3 and of 10 rows of the
// fixture, the whole fixture, and the fixture with neutral rows that hold
// none of the prompts' words.
func recallSizes(t *testing.T) map[int][][]Row {
	t.Helper()
	sizes := map[int][][]Row{36: {recallRows}, 500: {append(slices.Clone(recallRows), neutralRows(t, 500-len(recallRows))...)}}
	for i := range recallRows {
		sizes[1] = append(sizes[1], recallRows[i:i+1])
	}
	for i := 0; i+3 <= len(recallRows); i += 3 {
		sizes[3] = append(sizes[3], recallRows[i:i+3])
	}
	for _, i := range []int{0, 10, 20, 26} {
		sizes[10] = append(sizes[10], recallRows[i:i+10])
	}
	return sizes
}

// neutralRows returns n rows made of words no labelled prompt holds.
func neutralRows(t *testing.T, n int) []Row {
	t.Helper()
	asked := map[string]bool{}
	for _, p := range recallPrompts {
		for _, w := range promptTerms(p.prompt) {
			asked[w] = true
		}
	}
	word := func(i int) string { return fmt.Sprintf("zq%c%c%c", 'a'+i%26, 'a'+i/26%26, 'a'+i/676%26) }
	var rows []Row
	for i := range n {
		r := Row{ID: Name(1000 + i), Kind: Fact, Tag: "stated", Scope: ScopeProject, Project: project, AsOf: "2026-08-01",
			Title: word(3*i) + " " + word(3*i+1), Statement: word(3*i+1) + " " + word(3*i+2) + " " + word(3*i)}
		for _, w := range words(r.Title+" "+r.Statement, 1) {
			if asked[w] {
				t.Fatalf("neutral row %s holds %q, which a prompt holds", r.ID, w)
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// TestRecallPrecision measures recall on the labelled prompts at indexes of
// 1, 3, 10, 36 and 500 rows, in both scopes: with the two projects apart,
// and sharing a system page. Each prompt, asked in its project, gets at most
// MaxRecall rows, best first, as the hook shows them, and must recall the
// pages it wants that the index holds. It fails below 90% precision or 60%
// recall at any size in either scope, and reports the precision and recall
// Floor gives at each, and what nearby floors would give.
func TestRecallPrecision(t *testing.T) {
	isolate(t)
	if len(recallPrompts) < 30 || len(recallRows) < 20 {
		t.Fatalf("the fixture has %d prompts and %d rows, want at least 30 and 20", len(recallPrompts), len(recallRows))
	}
	sizes := recallSizes(t)
	order := []int{1, 3, 10, 36, 500}
	measure := func(size int, floor float64, near, report bool) (precision, coverage float64) {
		tp, fp, fn := 0, 0, 0
		for _, index := range sizes[size] {
			holds := func(id string) bool { return slices.ContainsFunc(index, func(r Row) bool { return r.ID == id }) }
			for _, p := range recallPrompts {
				want, siblings := p.want, func() map[string]bool { return nil }
				if near {
					siblings = func() map[string]bool { return map[string]bool{project: true, other: true} }
					if p.near != nil {
						want = p.near
					}
				}
				var rows []Row
				for _, s := range recall(index, p.prompt, p.in, siblings, nil, floor) {
					rows = append(rows, s.Row)
				}
				got := recallIDs(rows)
				for _, id := range got {
					if slices.Contains(want, id) || slices.Contains(p.also, id) {
						tp++
					} else {
						fp++
						if report {
							t.Logf("%d rows: false positive %s on prompt %.60q in %s", size, id, p.prompt, p.in)
						}
					}
				}
				for _, id := range want {
					if holds(id) && !slices.Contains(got, id) {
						fn++
						if report && size == len(recallRows) {
							t.Logf("%d rows: missed %s on prompt %.60q in %s", size, id, p.prompt, p.in)
						}
					}
				}
			}
		}
		if tp+fp == 0 {
			return 1, 0
		}
		return float64(tp) / float64(tp+fp), float64(tp) / float64(tp+fn)
	}
	for _, near := range []bool{false, true} {
		scope := "apart"
		if near {
			scope = "sharing a system page"
		}
		for _, floor := range []float64{2, 2.25, 2.5, 2.75, 3, 3.5, 4, 4.5, 5} {
			line := fmt.Sprintf("%s, floor %4.2f:", scope, floor)
			for _, size := range order {
				p, r := measure(size, floor, near, false)
				line += fmt.Sprintf(" %d rows %5.1f%%/%5.1f%%", size, 100*p, 100*r)
			}
			t.Log(line + " (precision/recall)")
		}
		for _, size := range order {
			p, r := measure(size, Floor, near, true)
			t.Logf("%s, Floor %.2f at %d rows: precision %.1f%%, recall %.1f%% over %d prompts and %d indexes", scope, Floor, size, 100*p, 100*r, len(recallPrompts), len(sizes[size]))
			if p < 0.9 || r < 0.6 {
				t.Errorf("%s, %d rows: precision %.1f%% and recall %.1f%% at Floor %.2f, want at least 90%% and 60%%", scope, size, 100*p, 100*r, Floor)
			}
		}
	}
}

// A score does not grow with the number of rows: a row whose terms no other
// row holds scores 2 for each title term and 1 for each other, alone or
// among 1,000 rows, and a prompt that restates it is recalled from both. It
// holds inbox in its title, and entries, jsonl, file and session.
func TestRecallScoreHoldsAtEverySize(t *testing.T) {
	isolate(t)
	fact := recallRows[0]
	const prompt = "Does the inbox store entries as JSONL, one file per session?"
	for _, rows := range [][]Row{{fact}, append([]Row{fact}, neutralRows(t, 999)...)} {
		got := recall(rows, prompt, project, nil, nil, 0)
		if len(got) != 1 || math.Abs(got[0].score-6) > 1e-9 {
			t.Fatalf("%d rows: %+v, want k1 at 6", len(rows), got)
		}
		if got := Recall(rows, prompt, project, nil, nil); len(got) != 1 || got[0].ID != "k1" {
			t.Fatalf("%d rows: recalled %+v", len(rows), got)
		}
	}
}

func recallIDs(rows []Row) []string {
	var out []string
	for _, r := range rows[:min(len(rows), MaxRecall)] {
		out = append(out, r.ID)
	}
	return out
}

// TestRecallRules checks the match rules one at a time: whole words only,
// two distinct terms at least, half the title or three terms, the skip, the
// order among equal scores, and the bounds of a long prompt.
func TestRecallRules(t *testing.T) {
	isolate(t)
	row := func(id, scope, proj, title, statement string) Row {
		return Row{ID: id, Kind: Fact, Tag: "stated", Scope: scope, Project: proj, AsOf: "2026-10-01", Title: title, Statement: statement}
	}
	// Padding rows make the terms rare, so the scores clear the floor.
	var pad []Row
	for i := 0; i < 30; i++ {
		pad = append(pad, row(fmt.Sprintf("k%d", 100+i), ScopeProject, project, fmt.Sprintf("Padding %d", i), "Unrelated filler statement"))
	}
	rows := append([]Row{
		row("k1", ScopeProject, project, "Ledger export format", "The ledger export writes Parquet files nightly"),
		row("k2", ScopeProject, other, "Ledger export format", "The ledger export writes Parquet files nightly"),
		row("k3", ScopeAll, other, "Ledger export format", "The ledger export writes Parquet files nightly"),
	}, pad...)
	// The rules are checked with no floor; the floor is checked after them.
	// The other project shares a system page with the first, so its row
	// counts, after the project's and every project's.
	sibling := func() map[string]bool { return map[string]bool{other: true} }
	ids := func(prompt string, skip func(string) bool) []string {
		var got []Row
		for _, s := range recall(rows, prompt, project, sibling, skip, 0) {
			got = append(got, s.Row)
		}
		return recallIDs(got)
	}
	if got := ids("ledger export parquet", nil); !slices.Equal(got, []string{"k1", "k3", "k2"}) {
		t.Errorf("equal scores = %q, want the project's and every project's first", got)
	}
	if got := Recall(rows, "ledger export parquet", project, nil, nil); !slices.Equal(recallIDs(got), []string{"k1", "k3"}) {
		t.Errorf("apart from the other project = %+v", got)
	}
	if got := ids("ledger export parquet", func(id string) bool { return id == "k1" }); !slices.Equal(got, []string{"k3", "k2"}) {
		t.Errorf("with k1 skipped = %q", got)
	}
	if got := ids("ledger", nil); got != nil {
		t.Errorf("one term = %q", got)
	}
	if got := ids("ledgers exporting parquets", nil); got != nil {
		t.Errorf("no whole words = %q", got)
	}
	if got := ids("the ledger and the export", nil); got != nil {
		t.Errorf("two terms, two thirds of the title = %q", got)
	}
	// A match of two terms names the whole title.
	two := append(slices.Clone(rows), row("k4", ScopeProject, project, "Release checklist", "Run every step before a tag"))
	var whole []Row
	for _, s := range recall(two, "walk me through the release checklist", project, nil, nil, 0) {
		whole = append(whole, s.Row)
	}
	if got := recallIDs(whole); !slices.Equal(got, []string{"k4"}) {
		t.Errorf("two terms, the whole title = %q", got)
	}
	if got := ids("ledger nightly", nil); got != nil {
		t.Errorf("two terms, a third of the title = %q", got)
	}
	if got := ids("ledger parquet nightly", nil); len(got) == 0 {
		t.Error("three terms didn't match")
	}
	if got := Recall(nil, "ledger export", project, nil, nil); got != nil {
		t.Errorf("no rows = %+v", got)
	}
	// The floor keeps out a match made of words most rows hold: the
	// padding rows' words weigh about a thirtieth of a rare word's, so four
	// of them score far under it, while three of the ledger rows' words, each
	// held by 3 rows of 33, clear it.
	if got := ids("padding unrelated filler statement", nil); len(got) == 0 {
		t.Fatal("the padding prompt matches no row, so the floor's check proves nothing")
	}
	if got := Recall(rows, "padding unrelated filler statement", project, sibling, nil); got != nil {
		t.Errorf("a match under the floor = %+v", got)
	}
	if got := Recall(rows, "ledger export parquet", project, sibling, nil); len(got) != 3 {
		t.Errorf("a match over the floor = %+v", got)
	}
	// A long prompt counts its first and last halves.
	long := "ledger " + filler("alpha beta gamma delta epsilon zeta", 100<<10) + " export format"
	if got := ids(long, nil); len(got) == 0 {
		t.Error("a long prompt's first and last words didn't match")
	}
	middle := filler("alpha beta", 40<<10) + " ledger export format " + filler("gamma delta", 40<<10)
	if got := ids(middle, nil); got != nil {
		t.Errorf("a long prompt's middle matched: %q", got)
	}
	var many []string
	for i := 0; i < 200; i++ {
		many = append(many, fmt.Sprintf("word%dx", i))
	}
	terms := promptTerms("ledger " + strings.Join(many, " ") + " export")
	if len(terms) != MaxTerms || terms[0] != "ledger" || terms[len(terms)-1] != "export" {
		t.Errorf("%d terms, from %q to %q", len(terms), terms[0], terms[len(terms)-1])
	}
	if got := promptTerms("The THE the of to Ledger ledger éclair ÉCLAIR x1 ab"); !slices.Equal(got, []string{"ledger", "éclair"}) {
		t.Errorf("terms = %q", got)
	}
}

// A project's name and owner come from its key, and a key that hides the
// name gives none.
func TestProjectName(t *testing.T) {
	isolate(t)
	for key, want := range map[string][2]string{
		"github.com__acme__billing":                 {"billing", "github.com__acme"},
		"gitlab.example.com__team__sub__pay_uments": {"pay_ments", "gitlab.example.com__team__sub"},
		"git.example.com__ledger":                   {"ledger", "git.example.com"},
		"local-notes-1a2b3c":                        {"notes", ""},
		"local-my-notes-1a2b3c":                     {"my-notes", ""},
		"local-0123456789ab":                        {"", ""},
		"repo-0123456789ab":                         {"", ""},
		"general":                                   {"", ""},
		"github.com__acme__":                        {"", ""},
	} {
		if name, owner := projectName(key); name != want[0] || owner != want[1] {
			t.Errorf("%s: %q under %q, want %q under %q", key, name, owner, want[0], want[1])
		}
	}
}

// A prompt names a project by its owner's whole path and its name, as
// acme/billing or clientb/backend/api, also in a clone URL, or a local repo
// by its whole key, from any chat; by its name alone, a content word, as a
// whole word in any case, from a chat under the same owner or under none;
// and never by a name another word or path joins, nor by the end of an
// owner's path, which another owner's may share.
func TestPromptNamesAProject(t *testing.T) {
	isolate(t)
	const acme, billing = "github.com__acme", "github.com__acme__billing"
	const clienta, clientb = "gitlab.example.com__clienta__backend", "gitlab.example.com__clientb__backend__api"
	for _, tc := range []struct {
		owner, key, prompt string
		want               bool
	}{
		{clienta, clientb, "why do backend/api retries stop?", false},
		{clienta, clientb, "in services/backend/api/retry.go, why do retries stop?", false},
		{"github.com__you", clientb, "in services/backend/api/retry.go, why do retries stop?", false},
		{clienta, clientb, "in the api, why do retries stop?", false},
		{clienta, clientb, "in clientb/backend/api, why do retries stop?", true},
		{clienta, clientb, "see gitlab.example.com/clientb/backend/api/src", true},
		{clienta, clientb, "clone git@gitlab.example.com:clientb/backend/api.git", true},
		{clienta, clientb, "in clienta/backend/api, why do retries stop?", false},
		{"github.com__other", billing, "clone git@github.com:acme/billing.git and check how invoices round", true},
		{"github.com__other", billing, "clone https://github.com/acme/billing.git.", true},
		{"github.com__other", billing, "see acme/billing.github.io", false},
		{"github.com__other", billing, "see acme/billing.gitignore", false},
		{acme, billing, "clone billing.git first", true},
		{acme, "git.example.com__ledger", "clone git@git.example.com:ledger.git", true},
		{acme, "local-notes-1a2b3c", "in local-notes-1a2b3c, how do invoices round?", true},
		{"repo-0123456789ab", "local-notes-1a2b3c", "see inbox/local-notes-1a2b3c/codex", true},
		{"", "local-notes-1a2b3c", "in Local-Notes-1A2B3C", true},
		{acme, "local-notes-1a2b3c", "in my-local-notes-1a2b3c", false},
		{acme, "local-notes-1a2b3c", "in local-notes-1a2b3cd", false},
		{acme, "local-0123456789ab", "in local-0123456789ab", false},
		{acme, billing, "in the billing service, are cards logged?", true},
		{acme, billing, "Billing: how many retries?", true},
		{acme, billing, "billing's retries", true},
		{acme, billing, "see billing/src/retry.go", true},
		{acme, billing, "see /billing", true},
		{acme, billing, "the billing-api client", false},
		{acme, billing, "my-billing tool", false},
		{acme, billing, "billing.go fails", false},
		{acme, billing, "rebilling the order", false},
		{acme, billing, "see other/billing", false},
		{acme, billing, "see acme/billing", true},
		{acme, billing, "clone github.com/acme/billing/src", true},
		{"github.com__other", billing, "in the billing service", false},
		{"github.com__other", billing, "in the Acme/Billing service", true},
		{"github.com__other", billing, "in my-acme/billing", false},
		{"", billing, "in the billing service", true},
		{acme, "local-notes-1a2b3c", "in notes", false},
		{"", "local-notes-1a2b3c", "in notes", true},
		{acme, "github.com__acme__the", "the retries", false},
		{acme, "github.com__acme__the", "acme/the retries", true},
		{acme, "repo-0123456789ab", "repo-0123456789ab retries", false},
		{acme, "github.com__acme__pay_uments", "pay_ments retries", true},
		{acme, "github.com__my_uteam__api", "my_team/api retries", true},
		{acme, "git.example.com__ledger", "see git.example.com/ledger", true},
		{acme, "git.example.com__ledger", "see ledger", false},
	} {
		if got := names(lowered(tc.prompt), tc.owner, tc.key); got != tc.want {
			t.Errorf("%q in %q, %s: %v, want %v", tc.prompt, tc.owner, tc.key, got, tc.want)
		}
	}
}

// Recall shows rows of the chat's project, of every project, of the
// projects that share a system page with it and of those the prompt names,
// and no other: it asks for the siblings once, and only when a strong row
// of another project needs them. A chat in a repo whose key hides its name
// names another project only with its owner.
func TestRecallScope(t *testing.T) {
	isolate(t)
	const ledger, shop, notes = "github.com__acme__ledger", "github.com__other__shop", "local-notes-1a2b3c"
	row := func(id, scope, proj string) Row {
		return Row{ID: id, Kind: Fact, Tag: "stated", Scope: scope, Project: proj, AsOf: "2026-10-01", Title: "Export format", Statement: "The export writes Parquet files nightly"}
	}
	rows := []Row{row("k1", ScopeProject, project), row("k2", ScopeAll, shop), row("k3", ScopeProject, ledger), row("k4", ScopeProject, other),
		row("k5", ScopeProject, shop), row("k6", ScopeProject, notes)}
	for i := range 30 {
		rows = append(rows, Row{ID: Name(100 + i), Kind: Fact, Tag: "stated", Scope: ScopeProject, Project: project, AsOf: "2026-09-01", Title: fmt.Sprintf("Padding %d", i), Statement: "Unrelated filler statement"})
	}
	// The first project shares a system page with the ledger.
	asked := 0
	ids := func(prompt, in string) []string {
		siblings := func() map[string]bool {
			asked++
			if in == project {
				return map[string]bool{ledger: true}
			}
			return nil
		}
		var got []string
		for _, r := range Recall(rows, prompt, in, siblings, nil) {
			got = append(got, r.ID)
		}
		slices.Sort(got)
		return got
	}
	for _, tc := range []struct {
		prompt, in string
		want       []string
		asked      int
	}{
		{"what format does the export write, parquet?", project, []string{"k1", "k2", "k3"}, 1},
		{"in billing, what format does the export write, parquet?", project, []string{"k1", "k2", "k3", "k4"}, 1},
		{"in shop, what format does the export write, parquet?", project, []string{"k1", "k2", "k3"}, 1},
		{"in other/shop, what format does the export write, parquet?", project, []string{"k1", "k2", "k3", "k5"}, 1},
		{"what format does the export write, parquet?", identity.GeneralKey, []string{"k2"}, 1},
		{"in billing and notes, what format does the export write, parquet?", identity.GeneralKey, []string{"k2", "k4", "k6"}, 1},
		{"in billing, what format does the export write, parquet?", "repo-0123456789ab", []string{"k2"}, 1},
		{"in acme/billing, what format does the export write, parquet?", "repo-0123456789ab", []string{"k2", "k4"}, 1},
		{"what is the unrelated filler statement of padding?", project, nil, 0},
	} {
		asked = 0
		if got := ids(tc.prompt, tc.in); !slices.Equal(got, tc.want) || asked != tc.asked {
			t.Errorf("%q in %s: %q, siblings asked %d times, want %q and %d", tc.prompt, tc.in, got, asked, tc.want, tc.asked)
		}
	}
	// Without siblings, only the project's, every project's and the named.
	if got := Recall(rows, "in billing, what format does the export write, parquet?", project, nil, nil); len(got) != 3 {
		t.Errorf("no siblings: %+v", got)
	}
	// Two clients' projects in nested groups share the end of their path:
	// only the whole path names the other's, and a local repo is named by
	// its key from a chat in a repo with a remote.
	const clienta, clientb = "gitlab.example.com__clienta__backend__api", "gitlab.example.com__clientb__backend__api"
	retries := []Row{{ID: "k1", Kind: Fact, Tag: "observed", Scope: ScopeProject, Project: clientb, AsOf: "2026-10-28", Title: "Retry limit", Statement: "Payment retries back off exponentially and stop after five attempts"},
		{ID: "k2", Kind: Fact, Tag: "observed", Scope: ScopeProject, Project: notes, AsOf: "2026-10-28", Title: "Invoice rounding", Statement: "Invoices round each line amount half up to the cent"}}
	none := func() map[string]bool { return nil }
	for _, tc := range []struct {
		prompt, in string
		want       int
	}{
		{"Why do backend/api payment retries stop after five attempts?", clienta, 0},
		{"In services/backend/api/retry.go, why do payment retries stop after five attempts?", clienta, 0},
		{"In services/backend/api/retry.go, why do payment retries stop after five attempts?", "github.com__you__shop", 0},
		{"In clientb/backend/api, why do payment retries stop after five attempts?", clienta, 1},
		{"In notes, how do invoices round each line amount?", "github.com__you__shop", 0},
		{"In local-notes-1a2b3c, how do invoices round each line amount?", "github.com__you__shop", 1},
	} {
		if got := Recall(retries, tc.prompt, tc.in, none, nil); len(got) != tc.want {
			t.Errorf("%q in %s: %+v, want %d rows", tc.prompt, tc.in, got, tc.want)
		}
	}
}

// TestConstraints checks the brief's rows: constraints of every project and
// of the project, newest first.
func TestConstraints(t *testing.T) {
	isolate(t)
	got := Constraints(recallRows, project)
	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if want := []string{"k28", "k22", "k16", "k3", "k2"}; !slices.Equal(ids, want) {
		t.Errorf("constraints = %q, want %q", ids, want)
	}
	got = Constraints(recallRows, other)
	ids = nil
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if want := []string{"k9", "k2"}; !slices.Equal(ids, want) {
		t.Errorf("another project's constraints = %q, want %q", ids, want)
	}
}

// BenchmarkRecall measures one recall over a full index, as the prompt hook
// runs it.
func BenchmarkRecall(b *testing.B) {
	var rows []Row
	size := 0
	for i := 0; size < MaxIndex; i++ {
		r := recallRows[i%len(recallRows)]
		r.ID = Name(i + 1)
		r.Statement = fmt.Sprintf("%s, revision %d of the statement with more words to fill the row", r.Statement, i)
		rows = append(rows, r)
		size += len(r.line()) + 1
	}
	prompt := "what is the p95 latency target for capture, and how do invoices round line amounts?"
	b.ResetTimer()
	for b.Loop() {
		Recall(rows, prompt, project, nil, nil)
	}
}
