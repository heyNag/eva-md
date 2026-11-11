package retrieval

// The generator of the labelled set. It runs only as a test: the set is
// generated from a fixed seed, TestTheSetIsGenerated generates it again
// and compares the bytes with the committed files, and
//
//	go test ./internal/eval/retrieval -run TestTheSetIsGenerated -args -write-set
//
// writes them, after a change here. A change to any held-out event is a new
// set: raise setVersion with it, and pin the new held-out half in
// heldoutHashes in set_test.go, or -write-set refuses to write it. Every
// project, name, value and prompt is made up.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var writeSet = flag.Bool("write-set", false, "write the generated set to eval/retrieval/set")

// committedSet is the set's folder, from this package's folder.
var committedSet = filepath.Join("..", "..", "..", "eval", "retrieval", "set")

const (
	setVersion        = 1
	setSeed    uint64 = 0x646c3034 // fixed: changing it is a new set
)

// commonWords are the words a paraphrase may share with its item's title
// or statement, and that never count as a word a near miss shares.
var commonWords = []string{
	"about", "and", "any", "are", "can", "did", "does", "for", "from", "has", "have", "how", "its",
	"not", "our", "that", "the", "this", "was", "what", "which", "who", "why", "with", "you",
}

// genRand is splitmix64, written here so that the set never depends on
// how a library draws numbers.
type genRand struct{ s uint64 }

func (r *genRand) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *genRand) intn(n int) int { return int(r.next() % uint64(n)) }

func pick[T any](r *genRand, xs []T) T { return xs[r.intn(len(xs))] }

func (r *genRand) shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		swap(i, r.intn(i+1))
	}
}

const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// crockfordBits writes the first chars*5 bits of sum in the store's
// alphabet.
func crockfordBits(sum [32]byte, chars int) string {
	out := make([]byte, chars)
	for i := range out {
		v := 0
		for b := range 5 {
			bit := i*5 + b
			v = v<<1 | int(sum[bit/8]>>(7-bit%8)&1)
		}
		out[i] = crockford[v]
	}
	return string(out)
}

// component is a part of a made-up project: the words its items use, other
// words for it that none of its items holds, and its file.
type component struct{ phrase, para, path string }

type genProject struct {
	name, key string
	comps     []component
}

var genProjects = []genProject{
	{"tidewater", "github.com__orchard-labs__tidewater", []component{
		{"booking queue", "reservation backlog", "internal/booking/queue.go"},
		{"rate engine", "pricing calculator", "internal/rates/engine.go"},
		{"carrier gateway", "shipping partner bridge", "internal/carrier/gateway.go"},
		{"invoice service", "billing module", "internal/invoice/service.go"},
		{"quote cache", "estimate memo", "internal/quote/cache.go"},
		{"manifest builder", "cargo list generator", "internal/manifest/builder.go"},
		{"customs exporter", "border paperwork feed", "internal/customs/export.go"},
		{"tracking poller", "shipment location watcher", "internal/tracking/poller.go"},
		{"webhook dispatcher", "outbound callback sender", "internal/webhook/dispatch.go"},
		{"auth broker", "login middleman", "internal/auth/broker.go"},
		{"audit ledger", "compliance trail", "internal/audit/ledger.go"},
		{"pallet planner", "load arrangement tool", "internal/pallet/planner.go"},
		{"dock scheduler", "loading bay calendar", "internal/dock/scheduler.go"},
		{"fuel surcharge", "diesel levy", "internal/fuel/surcharge.go"},
		{"address validator", "postal lookup", "internal/address/validate.go"},
		{"report exporter", "spreadsheet download", "internal/report/export.go"},
	}},
	{"saltmarsh", "github.com__orchard-labs__saltmarsh", []component{
		{"offline sync", "disconnected mirroring", "app/sync/offline.ts"},
		{"push notifier", "phone alert relay", "app/notify/push.ts"},
		{"photo uploader", "picture transfer", "app/photos/upload.ts"},
		{"signature pad", "autograph capture", "app/signature/pad.ts"},
		{"map renderer", "cartography layer", "app/map/render.ts"},
		{"driver roster", "courier lineup", "app/roster/drivers.ts"},
		{"barcode scanner", "label reader", "app/scan/barcode.ts"},
		{"settings screen", "preferences page", "app/screens/settings.tsx"},
		{"crash reporter", "fault telemetry", "app/crash/report.ts"},
		{"deep links", "inbound app addresses", "app/links/deep.ts"},
		{"theme loader", "visual style bootstrap", "app/theme/load.ts"},
		{"session keeper", "login lifetime", "app/session/keeper.ts"},
		{"route preview", "trip sketch", "app/route/preview.tsx"},
		{"proof gallery", "delivery evidence album", "app/proof/gallery.tsx"},
		{"onboarding wizard", "welcome walkthrough", "app/onboarding/wizard.tsx"},
		{"battery saver", "power conservation mode", "app/battery/saver.ts"},
	}},
	{"fernvale", "gitlab.example.com__brookline__fernvale", []component{
		{"ingest worker", "intake process", "pipeline/ingest/worker.py"},
		{"dedupe stage", "duplicate removal step", "pipeline/dedupe/stage.py"},
		{"format registry", "layout catalog", "pipeline/registry/formats.py"},
		{"parquet writer", "columnar file emitter", "pipeline/sink/parquet.py"},
		{"backfill runner", "historical replay", "pipeline/backfill/runner.py"},
		{"lineage tracker", "provenance map", "pipeline/lineage/tracker.py"},
		{"warehouse loader", "analytics depot feeder", "pipeline/load/warehouse.py"},
		{"metrics rollup", "measurement aggregation", "pipeline/rollup/metrics.py"},
		{"partition pruner", "shard trimming", "pipeline/partition/prune.py"},
		{"anomaly detector", "outlier spotter", "pipeline/anomaly/detect.py"},
		{"export bucket", "outgoing blob container", "pipeline/export/bucket.py"},
		{"join planner", "merge strategy picker", "pipeline/join/planner.py"},
		{"retention sweeper", "expiry cleanup", "pipeline/retention/sweep.py"},
		{"feature vault", "model input library", "pipeline/features/vault.py"},
		{"quality gate", "health checkpoint", "pipeline/quality/gate.py"},
		{"cost estimator", "spend forecaster", "pipeline/cost/estimate.py"},
	}},
	{"brambleworks", "local-brambleworks-3f9c2a", []component{
		{"markdown parser", "text markup reader", "src/parse/markdown.rs"},
		{"link checker", "hyperlink validator", "src/links/checker.rs"},
		{"search indexer", "lookup catalog builder", "src/search/indexer.rs"},
		{"theme compiler", "stylesheet assembler", "src/theme/compile.rs"},
		{"image resizer", "picture shrinker", "src/images/resize.rs"},
		{"feed generator", "syndication output", "src/feed/generate.rs"},
		{"word cloud", "term bubble chart", "src/cloud/words.rs"},
		{"backlink graph", "reverse reference web", "src/graph/backlinks.rs"},
		{"draft watcher", "unfinished note monitor", "src/drafts/watch.rs"},
		{"export pipeline", "publishing chain", "src/export/pipeline.rs"},
		{"spell linter", "orthography inspector", "src/lint/spell.rs"},
		{"template engine", "layout filler", "src/templates/engine.rs"},
		{"sitemap writer", "crawler index file", "src/sitemap/write.rs"},
		{"math renderer", "equation typesetter", "src/math/render.rs"},
		{"sync daemon", "background mirror service", "src/sync/daemon.rs"},
		{"citation formatter", "bibliography styler", "src/cite/format.rs"},
	}},
}

// genGroups are the projects, by index, that share a system page.
var genGroups = []struct {
	page     string
	projects []int
}{{"freight-platform", []int{0, 1}}}

// topic is a kind of thing an item of a component says. Its templates
// take the component ({c}, {C} capitalized), its other words ({p}, {P}),
// its file ({path}) and the item's value ({v}).
type topic struct {
	kind      string
	title     string
	statement string
	words     string // the user's own words, for a rule or a constraint
	values    []string
	cues      []string
	paths     []string // more files the item cites
	lexical   []string // prompts that use the item's words
	para      []string // prompts that use none of them
	miss      string   // a prompt that shares the component and one of the topic's words, in another sense
}

var topics = []topic{
	{
		kind: "fact", title: "{C} retry limit",
		statement: "The {c} retries a failed job {v} times and then parks it for review.",
		values:    []string{"two", "three", "four", "five", "six", "seven", "eight"},
		cues:      []string{"how many retries", "retry count"},
		lexical: []string{
			"What is the {c} retry limit?",
			"How many retries does the {c} make before it parks a failed job?",
			"I'm editing {path}; remind me of the {c} retry limit.",
			"Does the {c} still retry a failed job {v} times?",
		},
		para: []string{
			"How often will {p} attempt something again after an error?",
			"When {p} hits a problem, how persistent is it?",
		},
		miss: "Rename the {c} variable job to task, it reads better.",
	},
	{
		kind: "fact", title: "{C} request timeout",
		statement: "Calls from the {c} give up after {v} seconds instead of the platform default.",
		values:    []string{"five", "ten", "fifteen", "twenty", "thirty", "ninety"},
		cues:      []string{"how long calls wait", "timeout"},
		lexical: []string{
			"What is the {c} request timeout?",
			"How many seconds before calls from the {c} give up?",
			"I'm looking at {path}: is the {c} request timeout still {v} seconds?",
		},
		para: []string{
			"How long may {p} wait on a reply before it stops?",
			"Is there a cap on how much waiting {p} does for an answer?",
		},
		miss: "Fix the typo in the {c} request form label.",
	},
	{
		kind: "decision", title: "{C} storage choice",
		statement: "We decided the {c} keeps its state in {v}, not in the shared database.",
		values:    []string{"Postgres", "Redis", "SQLite", "etcd", "a local file"},
		cues:      []string{"database choice"},
		lexical: []string{
			"Which storage did we choose for the {c}?",
			"Does the {c} keep its state in {v} or in the shared database?",
			"What was the {c} storage choice again?",
		},
		para: []string{
			"Where did we settle that {p} should persist its data?",
			"What backing store holds records for {p}?",
		},
		miss: "Show a loading state in the {c} admin panel.",
	},
	{
		kind: "decision", title: "{C} release cadence",
		statement: "We agreed to ship the {c} every {v}, and never on a Friday.",
		values:    []string{"Monday", "Tuesday", "Wednesday", "Thursday"},
		cues:      []string{"when it ships", "release day"},
		lexical: []string{
			"What is the {c} release cadence?",
			"Do we still ship the {c} every {v}?",
			"Can I ship the {c} on a Friday this week?",
		},
		para: []string{
			"On which weekday does {p} go out to users?",
			"How regularly do new builds of {p} reach people?",
		},
		miss: "Update the {c} changelog wording for the release notes.",
	},
	{
		kind: "constraint", title: "{C} memory ceiling",
		statement: "The {c} must stay under {v} megabytes of memory on the small instances.",
		words:     "keep the {c} under {v} MB, the small boxes fall over otherwise",
		values:    []string{"256", "384", "512", "768", "1024"},
		cues:      []string{"memory budget"},
		lexical: []string{
			"What is the {c} memory ceiling?",
			"How much memory may the {c} use on the small instances?",
			"Is {v} megabytes still the cap for the {c}?",
		},
		para: []string{
			"How much RAM can {p} use at most?",
			"What footprint is acceptable for {p} on the cheap boxes?",
		},
		miss: "Make the {c} icon small on narrow screens.",
	},
	{
		kind: "constraint", title: "{C} public interface",
		statement: "Never change the {c} public interface without {v} releases of deprecation first.",
		words:     "don't break the {c} interface, clients get {v} releases of warning",
		values:    []string{"two", "three", "four", "six"},
		cues:      []string{"breaking changes", "api compatibility"},
		lexical: []string{
			"Can I change the {c} public interface in this release?",
			"How many releases of deprecation does the {c} interface need?",
		},
		para: []string{
			"Am I allowed to alter the signatures {p} exposes to callers?",
			"What notice do consumers of {p} get before a breaking edit?",
		},
		miss: "Translate the {c} interface labels into Spanish.",
	},
	{
		kind: "procedure", title: "Rolling back the {c}",
		statement: "To roll back the {c}, pin the previous tag, run make rollback, then watch the error rate.",
		values:    []string{""},
		cues:      []string{"undo a deploy", "revert a release"},
		paths:     []string{"Makefile"},
		lexical: []string{
			"How do I roll back the {c}?",
			"The {c} deploy went bad, what are the rollback steps?",
		},
		para: []string{
			"What do I do to undo a bad deploy of {p}?",
			"{P} broke in production; how do we revert to yesterday's build?",
		},
		miss: "Add an alt tag to the {c} screenshot in the docs.",
	},
	{
		kind: "procedure", title: "{C} local setup",
		statement: "To run the {c} locally, start the compose stack and seed it with make fixtures.",
		values:    []string{""},
		cues:      []string{"dev environment"},
		paths:     []string{"compose.yaml"},
		lexical: []string{
			"How do I run the {c} locally?",
			"What is the {c} local setup?",
		},
		para: []string{
			"How do I get {p} going on my laptop?",
			"What brings up a working copy of {p} on a dev machine?",
		},
		miss: "Explain the {c} stack trace in this crash report.",
	},
	{
		kind: "rule", title: "Ask before migrating the {c}",
		statement: "Always ask the user before running a migration against the {c}.",
		words:     "ask me before you migrate the {c}, every time",
		values:    []string{""},
		cues:      []string{"schema changes need approval"},
		lexical: []string{
			"Can I run the migration against the {c} now?",
			"I'm about to migrate the {c} schema, go ahead?",
		},
		para: []string{
			"Can I alter the schema of {p} without checking with you first?",
			"Is it fine to evolve the tables of {p} on my own?",
		},
		miss: "Draw the {c} user journey as a diagram.",
	},
	{
		kind: "rule", title: "{C} test command",
		statement: "Run the {c} tests with make check, never with plain go test.",
		words:     "use make check for the {c}, not go test directly",
		values:    []string{""},
		cues:      []string{"how to run tests"},
		paths:     []string{"Makefile"},
		lexical: []string{
			"What is the {c} test command?",
			"Should I run go test for the {c} or make check?",
		},
		para: []string{
			"How should I validate edits to {p}?",
			"Which suite invocation verifies {p} is healthy?",
		},
		miss: "Spell check the {c} readme.",
	},
}

// componentMisses share a component's words and nothing else with its
// items.
var componentMisses = []string{
	"Add a log line to the {c} so slow requests show up.",
	"Who wrote the {c} originally?",
	"Move the {c} code into its own folder.",
}

// topicsPerComponent is how many of the topics each component has an item
// of; the rest are what a near miss can ask about.
const topicsPerComponent = 8

// named is a kind of item known by a name ({N}): a host, a vendor, a
// branch, a group. Specific is a prompt with the name and some of the
// item's words.
type named struct {
	kind, title, statement, words string
	cues                          []string
	specific                      string
}

var namedTemplates = []named{
	{"fact", "{C} staging host", "{N} is the staging host for the {c}; it is wiped every night.", "", []string{"staging box"}, "Is {N} wiped every night?"},
	{"fact", "{C} feature flag", "The new {c} flow sits behind a flag that is switched on per tenant.", "", []string{"rollout toggle"}, "Is {N} switched on for every tenant now?"},
	{"decision", "{C} vendor choice", "We picked {N} as the vendor for the {c} after the trial month.", "", nil, "Did the trial month with {N} go well?"},
	{"decision", "{C} rewrite plan", "The {c} rewrite goes ahead in two phases, and the old code is removed last.", "", []string{"rewrite codename"}, "How many phases does {N} have?"},
	{"constraint", "{C} freeze window", "No {c} deploys go to the {N} cluster during the quarter-end freeze.", "don't push the {c} to {N} during the freeze", nil, "Can I deploy to {N} during the freeze?"},
	{"constraint", "{C} data residency", "Customer records from the {c} must stay in the EU region.", "the {N} records stay in the EU, no exceptions", nil, "Can {N} records leave the EU region?"},
	{"procedure", "{C} hotfix branch", "Hotfixes for the {c} go on the {N} branch, then merge back to main.", "", nil, "Which branch do hotfixes go on, {N}?"},
	{"procedure", "{C} on-call handover", "Hand over the {c} pager with a written summary of open incidents.", "", []string{"handover runbook"}, "Where is the {N} handover?"},
	{"rule", "{C} reviewers", "Ask the {N} group to review any {c} change first.", "get the {N} folks to review {c} changes", nil, "Do I need {N} to review this?"},
	{"rule", "{C} secrets file", "Never print or commit the {c} secrets file.", "the {N} file is never printed or committed", nil, "Can I print the {N} file to debug?"},
}

// namePrompts ask about a name and nothing else.
var namePrompts = []string{
	"What should I know before touching {N}?",
	"Remind me what we settled about {N}.",
	"Anything I need to keep in mind with {N}?",
	"Is there a note about {N}?",
}

// names are the names items are known by, each with an ordinary use of
// its word, which a near miss puts beside the component's words.
var names = []struct{ name, ordinary string }{
	{"Beacon", "fires a beacon request when the page unloads"},
	{"Granite", "uses a granite gray background"},
	{"Juniper", "shows a juniper green badge"},
	{"Falcon", "has a falcon mascot in the header"},
	{"Harbor", "lists every harbor on the map"},
	{"Lantern", "puts a lantern emoji next to night shifts"},
	{"Compass", "draws a compass rose in the corner"},
	{"Meadow", "uses a meadow photo on the sign in screen"},
	{"Ember", "pulses an ember orange dot for alerts"},
	{"Cobalt", "paints the buttons cobalt blue"},
	{"Willow", "has a willow tree drawing on the empty state"},
	{"Summit", "shows a mountain summit in the banner"},
	{"Anchor", "jumps to an anchor at the bottom of the page"},
	{"Thistle", "uses thistle purple for warnings"},
	{"Marble", "renders a marble texture behind the cards"},
	{"Sparrow", "plays a sparrow chirp on success"},
	{"Copper", "shows copper colored borders"},
	{"Pebble", "shows a pebble shaped avatar"},
	{"Kestrel", "has a kestrel in the splash art"},
	{"Orchid", "uses orchid pink for highlights"},
	{"Basalt", "uses basalt gray tones in dark mode"},
	{"Heron", "prints a heron watermark on exports"},
	{"Cedar", "uses cedar brown for the footer"},
	{"Saffron", "uses saffron yellow for pending rows"},
	{"Tundra", "shows a tundra landscape on the about page"},
	{"Glacier", "animates a glacier on the loading screen"},
	{"Comet", "draws a comet trail behind the cursor"},
	{"Prism", "splits the hero image like a prism"},
	{"Quartz", "has a quartz clock widget"},
	{"Raven", "draws a raven silhouette on the missing page"},
	{"Hazel", "offers hazel as an eye color for avatars"},
	{"Mosaic", "lays photos out in a mosaic grid"},
	{"Nimbus", "shows a nimbus cloud icon for rain"},
	{"Onyx", "uses onyx black for body text"},
	{"Topaz", "uses a topaz accent on links"},
	{"Velvet", "has a velvet texture on the premium card"},
	{"Zephyr", "animates a zephyr breeze on the weather card"},
	{"Cinder", "shows a cinder block pattern in the footer"},
	{"Osprey", "uses an osprey photo on the landing page"},
	{"Magnolia", "uses magnolia white for the canvas"},
	{"Pelican", "has a pelican sticker in the help panel"},
	{"Sorrel", "uses sorrel red for alerts"},
	{"Thimble", "shows a thimble icon for the sewing category"},
	{"Walnut", "uses walnut brown for headings"},
	{"Lichen", "uses lichen green for success badges"},
}

// nameMisses put a name's word beside the component's, as an ordinary
// word: {ordinary} is the name's ordinary use, {n} the word.
var nameMisses = []string{
	"The {c} status page {ordinary}; can we tone that down?",
	"Put a little {n} drawing on the {c} welcome screen.",
	"Does the {c} help text need the {n} metaphor at all?",
}

// global is an item of every project, and the prompts that ask for it.
type global struct {
	kind, title, statement, words string
	lexical, para                 []string
}

var globals = []global{
	{"rule", "Plain commit messages", "Write commit messages in plain English with no emoji.", "commit messages in plain English, no emoji please",
		[]string{"Can I put an emoji in the commit messages?", "Should commit messages be in plain English?"},
		[]string{"Any style guidance for the text I attach to each changeset?"}},
	{"rule", "No force push", "Never force push to a shared branch; open a new branch instead.", "never force push anything that others pull from",
		[]string{"Is it okay to force push the shared branch?"},
		[]string{"May I overwrite the remote history of a collaborative line of work?"}},
	{"rule", "Ask before adding dependencies", "Ask before adding a new third-party dependency to any project.", "check with me before you pull in another library",
		[]string{"Can I add a third-party dependency for this?", "Do I need to ask before adding a dependency?"},
		[]string{"Should I bring in an outside package to solve this?"}},
	{"constraint", "Secrets stay out of logs", "Secrets and tokens must never appear in log output.", "never log a token or a secret, ever",
		[]string{"Can secrets or tokens appear in the log output?"},
		[]string{"May credentials be printed while I debug?"}},
	{"rule", "A test with every bug fix", "Every bug fix comes with a test that fails before the fix.", "add a failing test with every bug fix",
		[]string{"Does this bug fix need a test that fails first?"},
		[]string{"When I repair a defect, what proof should go along with it?"}},
	{"constraint", "Support the oldest runtime", "Code must still run on the oldest supported runtime, two versions back.", "keep it working on the runtime from two versions ago",
		[]string{"Does the code still have to run on the oldest supported runtime?"},
		[]string{"Which interpreter generation do we keep compatible with?"}},
	{"rule", "British spelling in documentation", "Write documentation in British spelling.", "docs use British spelling, colour not color",
		[]string{"Should the documentation use British spelling?"},
		[]string{"Colour or color, which form do the manuals follow?"}},
	{"rule", "Small pull requests", "Keep pull requests under four hundred changed lines.", "keep PRs small, a few hundred lines at most",
		[]string{"How many changed lines may a pull request have?"},
		[]string{"What size limit applies to a proposed merge?"}},
	{"constraint", "No network in unit tests", "Unit tests must not call the network; use a fake server.", "tests never touch the network",
		[]string{"Can this unit test call the real network?"},
		[]string{"Is it acceptable for a quick automated check to reach the internet?"}},
	{"rule", "Dates in ISO format", "Write dates in ISO format, year first, in every document.", "dates are ISO, year first",
		[]string{"Should dates use ISO format with the year first?"},
		[]string{"How do we put a calendar day into prose?"}},
	{"rule", "Plan before a large refactor", "Describe the plan and wait for approval before any large refactor.", "show me a plan before any big refactor",
		[]string{"Can I start a large refactor without a plan?"},
		[]string{"Should I restructure big chunks of code straight away?"}},
	{"constraint", "Licence headers", "Every source file must carry the licence header at the top.", "every source file carries the licence header",
		[]string{"Does this new source file need the licence header?"},
		[]string{"What boilerplate opens each code module?"}},
}

// generic prompts, made of words no item holds, for no_match events.
var (
	noMatchVerbs   = []string{"Rename", "Simplify", "Document", "Reformat", "Inline", "Reorder", "Explain", "Tidy up"}
	noMatchObjects = []string{
		"the string padding helper", "this regular expression", "the struct field order", "the import block",
		"the comment above this function", "these variable names", "the switch statement on line 40",
		"the nested ternary", "this recursive function", "the README intro paragraph", "the copyright line",
		"the gitignore entries", "the unused constants", "the editor settings", "the enum of shapes",
	}
	noMatchTails = []string{"", " please", " so it reads better", " without altering behaviour", " here"}
)

// genItem is an item and what its prompts are made of.
type genItem struct {
	Item
	project  int // index in genProjects, or -1
	comp     component
	topic    *topic
	value    string
	named    *named
	name     string
	ordinary string
	global   *global
}

func fill(s string, g *genItem) string {
	c := g.comp.phrase
	return strings.NewReplacer(
		"{c}", c, "{C}", capital(c), "{p}", g.comp.para, "{P}", capital(g.comp.para), "{path}", g.comp.path,
		"{v}", g.value, "{N}", g.name, "{n}", strings.ToLower(g.name), "{ordinary}", g.ordinary,
	).Replace(s)
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// generate builds the set's files, or fails the test with what is wrong
// with a label.
func generate(t *testing.T) map[string][]byte {
	t.Helper()
	r := &genRand{s: setSeed}
	set := &Set{Version: setVersion, Seed: setSeed, Common: commonWords}
	for _, p := range genProjects {
		set.Projects = append(set.Projects, Project{ID: "p_" + crockfordBits(sha256.Sum256([]byte(p.key)), 12), Key: p.key, Name: p.name})
	}
	for _, g := range genGroups {
		grp := Group{Page: g.page}
		for _, i := range g.projects {
			grp.Projects = append(grp.Projects, set.Projects[i].ID)
		}
		set.Groups = append(set.Groups, grp)
	}

	var items []*genItem
	add := func(g *genItem) {
		g.ID = "i" + crockfordBits(sha256.Sum256(fmt.Appendf(nil, "dl-04 item\n%d", len(items))), 13)
		g.Scope = ScopeProject
		if g.project >= 0 {
			g.Project = set.Projects[g.project].ID
		} else {
			g.Scope = ScopeAll
		}
		items = append(items, g)
	}
	for pi, p := range genProjects {
		for _, c := range p.comps {
			order := make([]int, len(topics))
			for i := range order {
				order[i] = i
			}
			r.shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			kept := slices.Clone(order[:topicsPerComponent])
			slices.Sort(kept)
			for _, ti := range kept {
				tp := &topics[ti]
				g := &genItem{project: pi, comp: c, topic: tp, value: pick(r, tp.values)}
				g.Type, g.Title, g.Statement = tp.kind, fill(tp.title, g), fill(tp.statement, g)
				if tp.words != "" {
					g.Words = fill(tp.words, g)
				}
				g.Cues = slices.Clone(tp.cues)
				if r.intn(2) == 0 {
					g.Cues = append(g.Cues, c.para)
				}
				g.Paths = append([]string{c.path}, tp.paths...)
				add(g)
			}
		}
	}
	var namedItems []*genItem
	for ni, n := range names {
		pi := ni % len(genProjects)
		nt := &namedTemplates[(ni/len(genProjects)+pi)%len(namedTemplates)]
		// a component no other name of the project names, since 3 is prime to
		// the 16 components
		comps := genProjects[pi].comps
		c := comps[(ni/len(genProjects)*3+pi)%len(comps)]
		g := &genItem{project: pi, comp: c, named: nt, name: n.name, ordinary: n.ordinary}
		g.Type, g.Title, g.Statement = nt.kind, fill(nt.title, g), fill(nt.statement, g)
		if nt.words != "" {
			g.Words = fill(nt.words, g)
		}
		g.Cues = append([]string{n.name}, nt.cues...)
		g.Paths = []string{c.path}
		add(g)
		namedItems = append(namedItems, g)
	}
	for i := range globals {
		gl := &globals[i]
		g := &genItem{project: -1, global: gl}
		g.Type, g.Title, g.Statement, g.Words = gl.kind, gl.title, gl.statement, gl.words
		add(g)
	}
	for _, g := range items {
		set.Items = append(set.Items, g.Item)
	}

	itemsByID := map[string]*Item{}
	for i := range set.Items {
		itemsByID[set.Items[i].ID] = &set.Items[i]
	}
	var events []Event
	texts := map[string]bool{}
	refused := map[string]int{} // split and class: candidates whose labels would not hold
	// ev keeps a candidate event whose text is new and whose labels hold.
	ev := func(project int, text, split, class string, relevant, decoys []string) bool {
		e := Event{
			Project: set.Projects[project].ID, Text: text, Split: split, Class: class,
			Relevant: append([]string{}, relevant...), Decoys: append([]string{}, decoys...),
		}
		if texts[text] {
			return false
		}
		if err := checkEvent(set, &e, itemsByID); err != nil {
			refused[split+"."+class]++
			return false
		}
		texts[text] = true
		e.First = r.intn(10) < 3
		events = append(events, e)
		return true
	}
	// must keeps a candidate the set cannot do without.
	must := func(project int, text, split, class string, relevant, decoys []string) {
		if !ev(project, text, split, class, relevant, decoys) {
			t.Fatalf("a %s %s event about %v %v is refused or repeats another", split, class, relevant, decoys)
		}
	}
	// tries bounds the draws for want events, so that a template that
	// always fails stops the generator.
	tries := func(want int) func() {
		left := want * 20
		return func() {
			if left--; left < 0 {
				t.Fatalf("the generator drew %d candidates and could not keep %d", want*20, want)
			}
		}
	}
	// grid items and global items, the ones lexical and paraphrase prompts ask for
	var askable []*genItem
	for _, g := range items {
		if g.named == nil {
			askable = append(askable, g)
		}
	}
	prompts := func(g *genItem, para bool) []string {
		switch {
		case g.global != nil && para:
			return g.global.para
		case g.global != nil:
			return g.global.lexical
		case para:
			return g.topic.para
		}
		return g.topic.lexical
	}
	for _, para := range []bool{false, true} {
		want, split := 260, "lexical"
		if para {
			want, split = 180, "paraphrase"
		}
		try := tries(want)
		for n := 0; n < want; try() {
			g := pick(r, askable)
			project := g.project
			if project < 0 {
				project = r.intn(len(genProjects))
			}
			if ev(project, fill(pick(r, prompts(g, para)), g), split, Positive, []string{g.ID}, nil) {
				n++
			}
		}
	}
	for _, g := range namedItems {
		must(g.project, fill(pick(r, namePrompts), g), "name", Positive, []string{g.ID}, nil)
		must(g.project, fill(g.named.specific, g), "name", Positive, []string{g.ID}, nil)
		first := r.intn(len(nameMisses))
		for k := range 2 {
			must(g.project, fill(nameMisses[(first+k)%len(nameMisses)], g), "name", "near_miss", nil, []string{g.ID})
		}
	}
	grid := slices.DeleteFunc(slices.Clone(askable), func(g *genItem) bool { return g.global != nil })
	try := tries(110)
	for n := 0; n < 110; try() {
		g := pick(r, grid)
		if ev(g.project, fill(g.topic.miss, g), "lexical", "near_miss", nil, []string{g.ID}) {
			n++
		}
	}
	// near misses that share only the component's words, with every item
	// of the component
	try = tries(45)
	for n := 0; n < 45; try() {
		g := pick(r, grid)
		var decoys []string
		for _, other := range items {
			if other.project == g.project && other.comp.phrase == g.comp.phrase {
				decoys = append(decoys, other.ID)
			}
		}
		if ev(g.project, fill(pick(r, componentMisses), g), "lexical", "near_miss", nil, decoys) {
			n++
		}
	}
	try = tries(80)
	for n := 0; n < 80; try() {
		g := pick(r, grid)
		if g.project > 1 {
			continue
		}
		if ev(1-g.project, fill(pick(r, g.topic.lexical), g), "lexical", "sibling_only", nil, []string{g.ID}) {
			n++
		}
	}
	try = tries(30)
	for n := 0; n < 30; try() {
		g := pick(r, grid)
		other := r.intn(len(genProjects))
		if other == g.project || other <= 1 && g.project <= 1 {
			continue
		}
		if ev(other, fill(pick(r, g.topic.lexical), g), "lexical", "no_match", nil, []string{g.ID}) {
			n++
		}
	}
	try = tries(120)
	for n := 0; n < 120; try() {
		text := pick(r, noMatchVerbs) + " " + pick(r, noMatchObjects) + pick(r, noMatchTails) + "."
		if ev(r.intn(len(genProjects)), text, "lexical", "no_match", nil, nil) {
			n++
		}
	}
	t.Logf("candidates refused by their labels: %v", refused)
	r.shuffle(len(events), func(i, j int) { events[i], events[j] = events[j], events[i] })
	for i := range events {
		events[i].ID = fmt.Sprintf("ev-%04d", i+1)
	}
	set.Events = events
	if err := checkLabels(set); err != nil {
		t.Fatalf("the generated set: %v", err)
	}

	files := map[string][]byte{}
	files[SetFile] = indented(t, meta{V: 1, Version: set.Version, Seed: set.Seed, Common: set.Common})
	files[ProjectsFile] = indented(t, projectsFile{Projects: set.Projects, Groups: set.Groups})
	files[ItemsFile] = lines(t, set.Items)
	files[EventsFile] = lines(t, set.Events)
	files[HalvesFile] = HalvesText(set.Events)
	return files
}

func indented(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func lines[T any](t *testing.T, vs []T) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, v := range vs {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// The committed set is the generator's output, byte for byte; with
// -write-set the test writes it first.
func TestTheSetIsGenerated(t *testing.T) {
	files := generate(t)
	if *writeSet {
		if err := checkPin(setVersion, files[HalvesFile]); err != nil {
			t.Fatalf("the set is not written: %v", err)
		}
		if err := os.MkdirAll(committedSet, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(committedSet, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	entries, err := os.ReadDir(committedSet)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for _, e := range entries {
		onDisk = append(onDisk, e.Name())
	}
	var want []string
	for name := range files {
		want = append(want, name)
	}
	slices.Sort(want)
	if !slices.Equal(onDisk, want) {
		t.Fatalf("the set's folder holds %v, the generator writes %v", onDisk, want)
	}
	for _, name := range want {
		data, err := os.ReadFile(filepath.Join(committedSet, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, files[name]) {
			t.Errorf("%s is not what the generator writes; run this test with -args -write-set after a change to the generator", name)
		}
	}
}
