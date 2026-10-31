package hook

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
	"github.com/heyNag/eva-md/internal/systempage"
)

// bin is the binary pointers name in these tests, short as an installed
// one's is, so the reminder's fullest form fits in its 100 characters.
const bin = "/opt/eva/bin/eva-md"

// systemPage is a system page as an agent writes it with eva-md system write.
func systemPage(title, description string, repos []string, aliases ...string) string {
	return "---\ntype: system\ntitle: " + title + "\ndescription: " + description + "\nstatus: stable\n" +
		"generated: { by: claude-code/claude-opus-5-5 }\nsources:\n  - resource: https://docs.example.com/eng/overview v3\n" +
		"repos: [" + strings.Join(repos, ", ") + "]\naliases: [" + strings.Join(aliases, ", ") + "]\nas_of: 2026-10-28\nstate: active\n---\n\n## Summary\n" + title + " in short.\n"
}

// systemsWiki makes the wiki as setup leaves it, its instance in its schema
// and the config, and writes each page, by slug, as eva-md system write
// does, building the map and the names index.
func systemsWiki(t *testing.T, env *Env, pages map[string]string) {
	t.Helper()
	env.Config.WikiPath, env.Config.Instance = filepath.Join(os.Getenv("HOME"), "wiki"), "test-0a1b2c3d"
	if err := okfadapter.Init(env.Config.WikiPath); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveSchema(filepath.Join(env.Config.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, OKFSpec: "0.2", Instance: env.Config.Instance}); err != nil {
		t.Fatal(err)
	}
	o := systempage.Options{Config: env.Config, Dirs: env.Dirs, Version: "test", Now: env.Now}
	for _, slug := range slices.Sorted(maps.Keys(pages)) {
		if _, err := systempage.Write(context.Background(), o, slug, false, strings.NewReader(pages[slug])); err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
	}
	env.Dirs.Share = filepath.Dir(filepath.Dir(bin))
}

// chat starts a chat and returns a function that sends a prompt and returns
// the lines of context it gets.
func chat(t *testing.T, host string, p payload, env Env) func(prompt string) []string {
	t.Helper()
	p.Event, p.Source = "SessionStart", "startup"
	invoke(t, host, p, env)
	p.Event, p.Source = "UserPromptSubmit", ""
	n := 0
	return func(prompt string) []string {
		t.Helper()
		n++
		p.Prompt, p.Turn = prompt, fmt.Sprint("turn-", n)
		out := invoke(t, host, p, env)
		if out == "" {
			return nil
		}
		return strings.Split(contextFrom(t, out), "\n")
	}
}

// fourSystems are the pages most tests point at.
var fourSystems = map[string]string{
	"ledger":   systemPage("Ledger", "Keeps the double-entry books", nil, "LedgerSync"),
	"payments": systemPage("Payments", "Charges cards and pays out", nil, "payments-api"),
	"auth":     systemPage("Auth service", "Signs users in", nil, "auth.v2"),
	"orders":   systemPage("Orders service", "Takes, prices and tracks customer orders", nil, "order service"),
}

// A prompt that names systems gets a pointer to each page, at most 3, in the
// order it names them; the next prompt naming them gets the one left; then
// none, since each page is pointed at once a chat. Common words get nothing.
func TestPromptPointsAtTheSystemsItNames(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			env, p := setup(t)
			env.slack = busySlack
			systemsWiki(t, &env, fourSystems)
			prompt := chat(t, host, p, env)
			if got := prompt("Please check the error in the build, then update the tests and the service config."); got != nil {
				t.Fatalf("common words got %q", got)
			}
			const names = "Why does ledgersync miss the payments-api calls that auth.v2 signs for the orders service?"
			want := []string{
				"eva.md: Ledger: Keeps the double-entry books: " + bin + " show systems/ledger",
				"eva.md: Payments: Charges cards and pays out: " + bin + " show systems/payments",
				"eva.md: Auth service: Signs users in: " + bin + " show systems/auth",
			}
			if got := prompt(names); !reflect.DeepEqual(got, want) {
				t.Fatalf("pointers\n%s", strings.Join(got, "\n"))
			}
			if got := prompt(names); !reflect.DeepEqual(got, []string{"eva.md: Orders service: Takes, prices and tracks customer orders: " + bin + " show systems/orders"}) {
				t.Fatalf("second prompt\n%s", strings.Join(got, "\n"))
			}
			if got := prompt(names); got != nil {
				t.Fatalf("pages pointed at again: %q", got)
			}
		})
	}
}

// A chat's prompts get at most 12 pointers in all.
func TestPointersPerChat(t *testing.T) {
	env, p := setup(t)
	env.slack = busySlack
	pages := map[string]string{}
	var names []string
	for i := range 14 {
		pages[fmt.Sprintf("system-%02d", i)] = systemPage(fmt.Sprintf("System %02d", i), "One of many", nil, fmt.Sprintf("Sys-%02d", i))
		names = append(names, fmt.Sprintf("sys-%02d", i))
	}
	systemsWiki(t, &env, pages)
	prompt := chat(t, "codex", p, env)
	sent := 0
	for i := range 5 {
		got := prompt("Compare " + strings.Join(names, ", "))
		if want := min(3, 12-sent); len(got) != want {
			t.Fatalf("prompt %d: %d pointers, want %d: %q", i+1, len(got), want, got)
		}
		sent += len(got)
	}
	value, _, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("codex", p.Session, promptsMarker)
	if err != nil || !strings.Contains(value, `"prompts":5,"pointers":12,`) {
		t.Fatalf("state %s %v", value, err)
	}
}

// A page the chat's brief pointed at, as the project's system, gets no
// pointer from a prompt.
func TestPointersSkipTheBriefsPages(t *testing.T) {
	env, p := setup(t)
	env.slack = busySlack
	git(t, p.Cwd, "init", "--quiet", "-b", "main")
	git(t, p.Cwd, "remote", "add", "origin", "https://github.com/team/proj.git")
	git(t, p.Cwd, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "fixture")
	pages := maps.Clone(fourSystems)
	pages["ledger"] = systemPage("Ledger", "Keeps the double-entry books", []string{"github.com/team/proj"}, "LedgerSync")
	systemsWiki(t, &env, pages)
	prompt := chat(t, "claude-code", p, env)
	if got := prompt("Does LedgerSync call payments-api?"); len(got) != 1 || !strings.HasSuffix(got[0], " show systems/payments") {
		t.Fatalf("pointers %q", got)
	}
	value, _, _ := (session.Markers{Root: env.Dirs.Sessions()}).Get("claude-code", p.Session, promptsMarker)
	if !strings.Contains(value, `"pages":["systems/ledger","systems/payments"]`) {
		t.Fatalf("state %s", value)
	}
}

// A brief that quotes an exchange naming a system page's command has not
// pointed at the page, so a prompt that names the system still gets one.
func TestQuotedCommandIsNoPointer(t *testing.T) {
	env, p := setup(t)
	env.slack = busySlack
	systemsWiki(t, &env, fourSystems)
	store := inbox.Store{Root: env.Dirs.Inbox()}
	for i, text := range []string{"Look at the payments flow.", "It retries twice.", "Run eva-md show systems/payments to read about it."} {
		for _, said := range [][2]string{{"user", "prompt"}, {"assistant", "reply"}} {
			e := inbox.Entry{V: 1, TS: env.Now().Add(time.Duration(i-5) * time.Minute).Format(time.RFC3339), Host: "codex", Session: "previous-session", Turn: fmt.Sprint("turn-", i), Role: said[0], Kind: said[1], Text: text, Project: "general", Branch: "default", Cwd: "project", Model: "fixture-model", Origin: "hook"}
			if err := store.Append(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	p.Event, p.Source = "SessionStart", "startup"
	if context := contextFrom(t, invoke(t, "claude-code", p, env)); !strings.Contains(context, "\n  Run eva-md show systems/payments to read about it.") {
		t.Fatalf("the brief quotes no command: %q", context)
	}
	p.Event, p.Source, p.Prompt = "UserPromptSubmit", "", "Does payments-api retry?"
	if context := contextFrom(t, invoke(t, "claude-code", p, env)); context != "eva.md: Payments: Charges cards and pays out: "+bin+" show systems/payments" {
		t.Fatalf("prompt: %q", context)
	}
}

// A prompt whose refresh is lost to the deadline gets no lines and records
// nothing in the chat's state, so the next prompt, which delivers the
// refresh, still points at the system it names.
func TestLostRefreshRecordsNoPointer(t *testing.T) {
	env, p := setup(t)
	systemsWiki(t, &env, fourSystems)
	plant(t, env, "codex", "previous-session")
	generalNow(t, &env, "### Next\n- Write the parser")
	markers := session.Markers{Root: env.Dirs.Sessions()}
	if out := resumeContext(context.Background(), "claude-code", p, env, identity.General(), env.Now(), startState{}, new([][]string)); out == "" {
		t.Fatal("no brief")
	}
	before, _, _ := markers.Get("claude-code", p.Session, promptsMarker)
	generalNow(t, &env, "### Next\n- Test the parser")
	p.Prompt = "Is LedgerSync behind?"
	if out := resumeContext(nearDeadline{context.Background()}, "claude-code", p, env, identity.General(), env.Now(), startState{}, new([][]string)); out != "" {
		t.Fatalf("lines given with a lost refresh: %q", out)
	}
	if after, _, _ := markers.Get("claude-code", p.Session, promptsMarker); after != before {
		t.Fatalf("a lost refresh changed the state from %s to %s", before, after)
	}
	out := resumeContext(context.Background(), "claude-code", p, env, identity.General(), env.Now(), startState{}, new([][]string))
	if context := contextFrom(t, out); !strings.Contains(context, "Test the parser") || !strings.HasSuffix(context, "\n\neva.md: Ledger: Keeps the double-entry books: "+bin+" show systems/ledger") {
		t.Fatalf("next prompt: %q", context)
	}
}

// A startup or clear brief starts the chat's state over, as a host that
// keeps a session's id across a clear needs: the next prompt gets the
// reminder again, and a page pointed at before is pointed at again. A
// compact brief continues the chat, and its state.
func TestClearStartsTheStateOver(t *testing.T) {
	env, p := setup(t)
	env.slack = busySlack
	env.Config.ReminderEvery = 5
	systemsWiki(t, &env, fourSystems)
	again := [][]string{
		{"eva.md: earlier chats: " + bin + " search <terms>; system map: show systems/map"},
		{"eva.md: Ledger: Keeps the double-entry books: " + bin + " show systems/ledger"},
	}
	for _, tc := range []struct {
		source string
		want   [][]string
	}{{"startup", again}, {"clear", again}, {"compact", [][]string{nil, nil}}} {
		p.Event, p.Source = "SessionStart", tc.source
		invoke(t, "claude-code", p, env)
		for i, prompt := range []string{"Carry on", "Now look at LedgerSync"} {
			p.Event, p.Source, p.Prompt, p.Turn = "UserPromptSubmit", "", prompt, fmt.Sprint(tc.source, "-", i)
			var got []string
			if out := invoke(t, "claude-code", p, env); out != "" {
				got = strings.Split(contextFrom(t, out), "\n")
			}
			if !reflect.DeepEqual(got, tc.want[i]) {
				t.Fatalf("after a %s brief, prompt %d: %q", tc.source, i+1, got)
			}
		}
	}
}

// No wiki, no index, a wiki that is not the config's instance or an index
// that names a page since removed gives no pointer. A damaged line of the
// index is skipped, and the rest still match.
func TestPointersNeedTheIndex(t *testing.T) {
	const names = "Is LedgerSync behind payments-api?"
	env, p := setup(t)
	env.slack = busySlack
	if got := chat(t, "codex", p, env)(names); got != nil {
		t.Fatalf("no wiki: %q", got)
	}
	systemsWiki(t, &env, fourSystems)
	index := filepath.Join(env.Config.WikiPath, filepath.FromSlash(systempage.NamesFile))
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Env) error
		want   []string
	}{
		{"another instance", func(e *Env) error { e.Config.Instance = "other-00000000"; return nil }, nil},
		{"no index", func(*Env) error { return os.Remove(index) }, nil},
		{"damaged", func(*Env) error {
			return os.WriteFile(index, append([]byte("LedgerSync\tsystems/../ledger\tLedger\n\x00\x01\n"), data[strings.Index(string(data), "payments-api"):]...), 0o600)
		}, []string{"show systems/payments"}},
		{"page removed", func(*Env) error {
			return os.Remove(filepath.Join(env.Config.WikiPath, "systems", "payments.md"))
		}, nil},
	} {
		e := env
		if err := tc.change(&e); err != nil {
			t.Fatal(err)
		}
		p.Session = "session-" + strings.ReplaceAll(tc.name, " ", "-")
		got := chat(t, "codex", p, e)(names)
		if len(got) != len(tc.want) || len(got) == 1 && !strings.HasSuffix(got[0], tc.want[0]) {
			t.Fatalf("%s: %q", tc.name, got)
		}
	}
}

// The reminder rides with a chat's first prompt and then every
// reminder_every prompts, unless the prompt got a pointer or carries a
// brief. It names the system map when the wiki has system pages, fits in
// 100 characters, and reminder_every = 0 turns it off.
func TestReminder(t *testing.T) {
	const search = "eva.md: earlier chats: " + bin + " search <terms>"
	const withMap = search + "; system map: show systems/map"
	for _, tc := range []struct {
		name  string
		every int
		wiki  bool
		want  map[int]string // the prompts that get the reminder
	}{
		{"every 5", 5, true, map[int]string{1: withMap, 11: withMap}},
		{"every 1", 1, true, map[int]string{1: withMap, 2: withMap, 3: withMap, 4: withMap, 5: withMap, 7: withMap, 8: withMap, 9: withMap, 10: withMap, 11: withMap, 12: withMap}},
		{"no wiki", 5, false, map[int]string{1: search, 6: search, 11: search}},
		{"off", 0, true, map[int]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, p := setup(t)
			env.slack = busySlack
			env.Config.ReminderEvery = tc.every
			if tc.wiki {
				systemsWiki(t, &env, fourSystems)
			} else {
				env.Dirs.Share = filepath.Dir(filepath.Dir(bin))
			}
			prompt := chat(t, "claude-code", p, env)
			for n := 1; n <= 12; n++ {
				text := "Carry on with step " + fmt.Sprint(n)
				if n == 6 {
					text = "Now look at LedgerSync"
				}
				got := prompt(text)
				if tc.wiki && n == 6 {
					if len(got) != 1 || !strings.HasSuffix(got[0], " show systems/ledger") {
						t.Fatalf("prompt 6: %q", got)
					}
					continue
				}
				want, due := tc.want[n]
				if due != (got != nil) || due && (len(got) != 1 || got[0] != want || len([]rune(want)) > reminderCap) {
					t.Fatalf("prompt %d: %q", n, got)
				}
			}
		})
	}
	// A first prompt that carries the brief, since none came at the chat's
	// start, gets no reminder, which the brief's footer would repeat; the
	// next prompt does.
	env, p := setup(t)
	env.slack = busySlack
	env.Config.ReminderEvery = 1
	env.Dirs.Share = filepath.Dir(filepath.Dir(bin))
	if context := contextFrom(t, invoke(t, "codex", p, env)); !strings.HasPrefix(context, "No earlier chats here;") || strings.Contains(context, "eva.md: earlier chats:") {
		t.Fatalf("first prompt with its brief: %q", context)
	}
	p.Turn = "turn-2"
	if context := contextFrom(t, invoke(t, "codex", p, env)); context != search {
		t.Fatalf("second prompt: %q", context)
	}
}

// The chat's state is saved in place, but never through a link: a prompts
// marker that is a symlink, here to another marker, or that has a second
// hard link, is refused and what it leads to is left as it is.
func TestStateNeverSavedThroughALink(t *testing.T) {
	env, p := setup(t)
	markers := session.Markers{Root: env.Dirs.Sessions()}
	if err := markers.Set("codex", "other-session", session.Briefed, "keep"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(env.Dirs.Sessions(), "codex", "other-session", session.Briefed)
	marker := filepath.Join(env.Dirs.Sessions(), "codex", p.Session, promptsMarker)
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, link := range []func(string, string) error{os.Symlink, os.Link} {
		if err := link(other, marker); err != nil {
			t.Fatal(err)
		}
		if err := saveState(context.Background(), markers, "codex", p.Session, promptState{Prompts: 1}); err == nil {
			t.Fatal("the state was saved through a link")
		}
		if data, err := os.ReadFile(other); err != nil || string(data) != "keep" {
			t.Fatalf("the linked marker holds %q, %v", data, err)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveState(context.Background(), markers, "codex", p.Session, promptState{Prompts: 2, Pages: []string{"systems/ledger"}}); err != nil {
		t.Fatal(err)
	}
	if s, err := readState(context.Background(), markers, "codex", p.Session); err != nil || s.Prompts != 2 || !slices.Equal(s.Pages, []string{"systems/ledger"}) {
		t.Fatalf("state %+v %v", s, err)
	}
}

// The reminder takes the fullest form that fits in 100 characters, and none
// when even the shortest doesn't.
func TestReminderForms(t *testing.T) {
	for _, tc := range []struct {
		share string
		want  string
	}{
		{"/e", "eva.md: earlier chats: /e/bin/eva-md search <terms>; system map: /e/bin/eva-md show systems/map"},
		{"/Users/alex/.local/share/eva-md", "eva.md: earlier chats: /Users/alex/.local/share/eva-md/bin/eva-md search <terms>; show systems/map"},
		{"/Users/someone.with.a.long.name/.local/share/eva-md", "eva.md: earlier chats: /Users/someone.with.a.long.name/.local/share/eva-md/bin/eva-md search <terms>"},
		{"/" + strings.Repeat("x", 80), ""},
	} {
		got := reminder(paths.Dirs{Share: tc.share}, true)
		if got != tc.want || len([]rune(got)) > reminderCap {
			t.Errorf("share %s: %q", tc.share, got)
		}
	}
}

// A pointer fits in 160 characters: the description is cut to fit or left
// out, then the title cut, and the command is never cut. A line the daily
// commit's scan would take for a secret is never given: the description is
// left out, and failing that the pointer.
func TestPointerFits(t *testing.T) {
	long := strings.Repeat("Takes orders and much more ", 8)
	secret := "ghp_" + strings.Repeat("ab12", 10)
	for _, tc := range []struct{ title, description, want string }{
		{"Orders", "Takes orders", "eva.md: Orders: Takes orders: " + bin + " show systems/orders"},
		{"Orders", long, "eva.md: Orders: " + long[:160-len("eva.md: Orders: : "+bin+" show systems/orders")-1] + "…: " + bin + " show systems/orders"},
		{strings.Repeat("Orders ", 20), "Takes orders", "eva.md: " + strings.Repeat("Orders ", 20)[:160-len("eva.md: : "+bin+" show systems/orders")-1] + "…: " + bin + " show systems/orders"},
		{"Orders\tservice​", "", "eva.md: Orders service: " + bin + " show systems/orders"},
		{"Orders", "Calls the bank with " + secret, "eva.md: Orders: " + bin + " show systems/orders"},
		{"Orders " + secret, "Takes orders", ""},
	} {
		got := pointer(tc.title, tc.description, bin, "orders")
		if got != tc.want || len([]rune(got)) > pointerCap {
			t.Errorf("pointer(%q, %q)\n got %q\nwant %q", tc.title, tc.description, got, tc.want)
		}
	}
}

// Precision: what a prompt matches and what it never does.
func TestNamedPrecision(t *testing.T) {
	index := []systempage.Name{
		{Name: "Ledger", ID: "systems/ledger"}, {Name: "ledger", ID: "systems/ledger"}, {Name: "LedgerSync", ID: "systems/ledger"},
		{Name: "Payments", ID: "systems/payments"}, {Name: "payments", ID: "systems/payments"}, {Name: "payments-api", ID: "systems/payments"},
		{Name: "Auth service", ID: "systems/auth"}, {Name: "auth", ID: "systems/auth"}, {Name: "auth.v2", ID: "systems/auth"},
		{Name: "Orders service", ID: "systems/orders"}, {Name: "order_service", ID: "systems/orders"},
		{Name: "Search", ID: "systems/search"}, {Name: "search", ID: "systems/search"},
		{Name: "C++ SDK", ID: "systems/sdk"},
	}
	for _, tc := range []struct {
		prompt string
		want   []string
	}{
		// Common words, short words and parts of words never match.
		{"Please search the docs, then check the build and the order", nil},
		{"Fix auth for the SDK", nil},
		{"The LedgerSyncer class and my-payments-api-v2 and xauth.v2", nil},
		// A distinctive one-word name matches in any case, as a whole word.
		{"why does ledgersync drop rows", []string{"ledger"}},
		{"LEDGERSYNC is down", []string{"ledger"}},
		{"see auth.v2.", []string{"auth"}},
		{"the PAYMENTS-API (v1) and ORDER_SERVICE", []string{"payments", "orders"}},
		// A plain one only in the case the index lists it.
		{"Ledger and payments", []string{"ledger", "payments"}},
		{"LEDGER and PAYMENTS and pAyments", nil},
		// A name of several words matches as whole words, ignoring case,
		// with the same text between them, any run of space as one.
		{"the auth   service\nand the ORDERS SERVICE", []string{"auth", "orders"}},
		{"auth, service and orders. Service", nil},
		{"the c++ sdk", []string{"sdk"}},
		// Each page once, in the order the prompt first names it.
		{"payments-api, then LedgerSync, then payments", []string{"payments", "ledger"}},
	} {
		var got []string
		for _, n := range named(tc.prompt, index) {
			got = append(got, strings.TrimPrefix(n.ID, "systems/"))
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.prompt, got, tc.want)
		}
	}
}

// A long prompt is matched at its start and its end.
func TestLongPromptScanned(t *testing.T) {
	index := []systempage.Name{{Name: "LedgerSync", ID: "systems/ledger"}, {Name: "payments-api", ID: "systems/payments"}, {Name: "auth.v2", ID: "systems/auth"}}
	middle := strings.Repeat("filler words ", 20<<10)
	prompt := "LedgerSync " + middle[:len(middle)/2] + " auth.v2 " + middle[len(middle)/2:] + " payments-api"
	var got []string
	for _, n := range named(scanned(prompt), index) {
		got = append(got, n.ID)
	}
	if !reflect.DeepEqual(got, []string{"systems/ledger", "systems/payments"}) {
		t.Fatalf("%q", got)
	}
}
