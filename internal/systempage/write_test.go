package systempage

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// isolate gives the test a temporary home whose git config is a temporary
// file, and clears the variables that would point git elsewhere.
func isolate(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TEMPLATE_DIR"} {
		t.Setenv(name, "")
	}
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte("[user]\nname = Eva Fixture\nemail = fixture@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// fixture makes a wiki as setup does, with wikiinit, pushed to a bare remote
// in the temporary home, and returns the options a write takes.
func fixture(t *testing.T) Options {
	t.Helper()
	home := isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(home, "remote.git")
	git(t, home, "init", "--bare", "--quiet", remote)
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: filepath.Join(home, "wiki"), Dirs: dirs, Version: "test", OKFModule: "v0.7.2"}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	return Options{Config: c, Dirs: dirs, Version: "test", Now: func() time.Time { return day }}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := wikigit.Run(context.Background(), time.Minute, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", args[0], err)
	}
	return out
}

func write(o Options, slug, text string, replace bool) (Result, error) {
	return Write(context.Background(), o, slug, replace, strings.NewReader(text))
}

func mustWrite(t *testing.T, o Options, slug, text string, replace bool) Result {
	t.Helper()
	r, err := write(o, slug, text, replace)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// file reads a wiki file.
func file(t *testing.T, o Options, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(o.Config.WikiPath, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// other is a page for another system.
func other(title, description, repo string) string {
	return "---\ntype: system\ntitle: " + title + "\ndescription: " + description + "\nstatus: stable\n" +
		"generated:\n  by: codex/gpt-5.5-codex\nsources:\n- resource: https://docs.example.com/eng/overview v3\n" +
		"repos:\n  - " + repo + "\nas_of: \"2026-10-29\"\nstate: active\n---\n\n## Summary\n" + title + " in short.\n"
}

// refusal returns the problems of a refused write.
func refusal(t *testing.T, err error) string {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("not a refusal: %v", err)
	}
	return strings.Join(r.Problems, "\n")
}

// A write saves the page, ending with the map's link, and builds the map,
// sorted by slug and linking every page, and the folder's index, sorted by
// file name, and lists the folder in the root index once. okf finds no error,
// warning, orphan or broken link, and a page read back writes again
// unchanged.
func TestWriteAndMap(t *testing.T) {
	o := fixture(t)
	if r := mustWrite(t, o, "payments", other("Payments", "Charges cards and pays out", "github.com/acme/payments"), false); r.Systems != 1 || r.Page != "systems/payments.md" {
		t.Fatalf("result %+v", r)
	}
	orders := with(t, "## Unverified", "## Depends on\n- [Payments](payments.md)\n\n## Unverified")
	mustWrite(t, o, "orders", orders, false)
	mustWrite(t, o, "billing-api", other("Billing API", "Serves invoices", "git@gitlab.acme.internal:fin/billing-api.git"), false)
	r := mustWrite(t, o, "billing", other("Billing", "Issues invoices", "local-billing-a1b2c3"), false)
	if info, err := os.Stat(filepath.Join(o.Config.WikiPath, "systems", "billing.md")); r.Systems != 4 || err != nil || r.Bytes != info.Size() {
		t.Fatalf("result %+v %v", r, err)
	}
	var lines []string
	for _, l := range strings.Split(file(t, o, "systems/map.md"), "\n") {
		if strings.HasPrefix(l, "- ") {
			lines = append(lines, l)
		}
	}
	want := []string{
		"- [Billing](billing.md): Issues invoices · repos: local-billing-a1b2c3 · as of 2026-10-29",
		"- [Billing API](billing-api.md): Serves invoices · repos: gitlab.acme.internal__fin__billing-api · as of 2026-10-29",
		"- [Orders service](orders.md): Takes, prices and tracks customer orders · repos: github.com__acme__orders · as of 2026-10-30",
		"- [Payments](payments.md): Charges cards and pays out · repos: github.com__acme__payments · as of 2026-10-29",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("map lines\n%s", strings.Join(lines, "\n"))
	}
	for _, s := range []string{"billing", "billing-api", "orders", "payments"} {
		if !strings.HasSuffix(file(t, o, "systems/"+s+".md"), "\n\n"+MapLink+"\n") {
			t.Fatalf("%s does not end with the map's link", s)
		}
	}
	index := "# Systems\n\n" +
		"* [Billing API](billing-api.md) - Serves invoices\n" +
		"* [Billing](billing.md) - Issues invoices\n" +
		"* [System map](map.md) - One line per system page, built from their frontmatter\n" +
		"* [Orders service](orders.md) - Takes, prices and tracks customer orders\n" +
		"* [Payments](payments.md) - Charges cards and pays out\n"
	if got := file(t, o, "systems/index.md"); got != index {
		t.Fatalf("index\n%s", got)
	}
	if root := file(t, o, "index.md"); strings.Count(root, "](systems/index.md)") != 1 || !strings.HasPrefix(root, "---\nokf_version: \"0.2\"\n---\n") {
		t.Fatalf("root index\n%s", root)
	}
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	v, err := w.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Errors)+len(v.Warnings)+len(v.Orphans)+len(v.BrokenLinks) != 0 {
		t.Fatalf("okf findings: %+v", v)
	}
	// The daily commit stages every file the writes made, and its scan
	// flags no line of them.
	c, err := wikigit.Commit(context.Background(), wikigit.Options{Config: o.Config, Dirs: o.Dirs, Now: func() time.Time { return day }})
	if err != nil || c.Outcome != wikigit.Committed || len(c.Unstaged) != 0 {
		t.Fatalf("commit %+v %v", c, err)
	}
	files, err := os.ReadDir(filepath.Join(o.Config.WikiPath, "systems"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if n := wikigit.SecretLines(file(t, o, "systems/"+f.Name())); n != nil {
			t.Fatalf("the scan flags lines %v of %s", n, f.Name())
		}
	}
	// The page as saved, which eva-md show prints, reads as the page written
	// and writes again without a change.
	saved := file(t, o, "systems/orders.md")
	first, _ := check("orders", orders, day, func(s string) bool { return s == "payments" })
	again, problems := check("orders", saved, day, func(s string) bool { return s == "payments" })
	if len(problems) != 0 || !reflect.DeepEqual(again, first) {
		t.Fatalf("read back: %v\n%+v\n%+v", problems, again, first)
	}
	before := file(t, o, "systems/map.md")
	mustWrite(t, o, "orders", saved, true)
	if file(t, o, "systems/orders.md") != saved || file(t, o, "systems/map.md") != before {
		t.Fatal("writing the page back changed it")
	}
}

// A page that exists needs --replace, which refuses a page changed since the
// write read it, one a person verified, one that is locked and one okf can't
// read, and keeps a key a person added. The refusal lists what the page
// written has wrong as well.
func TestReplace(t *testing.T) {
	o := fixture(t)
	mustWrite(t, o, "orders", valid, false)
	path := filepath.Join(o.Config.WikiPath, "systems", "orders.md")
	_, err := write(o, "orders", valid, false)
	if text := refusal(t, err); !strings.Contains(text, "the page exists; read it with eva-md show systems/orders") {
		t.Fatalf("refusal %q", text)
	}
	_, err = write(o, "orders", with(t, "title: Orders service", "title: Orders --- service"), false)
	if text := refusal(t, err); !strings.HasPrefix(text, "the page exists;") || !strings.Contains(text, "\nline 3: title holds ---") {
		t.Fatalf("refusal %q", text)
	}
	edit := func(old, new string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), old) {
			t.Fatalf("page lacks %q: %v", old, err)
		}
		text := strings.Replace(string(b), old, new, 1)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return text
	}
	edit("state: \"active\"\n", "state: \"active\"\nowner: \"team-x\"\n")
	changed := with(t, "Takes, prices and tracks customer orders", "Takes and tracks customer orders")
	mustWrite(t, o, "orders", changed, true)
	if text := file(t, o, "systems/orders.md"); !strings.Contains(text, "description: Takes and tracks customer orders\n") || !strings.Contains(text, "owner: \"team-x\"\n") {
		t.Fatalf("replaced page\n%s", text)
	}
	if !strings.Contains(file(t, o, "systems/map.md"), "): Takes and tracks customer orders ·") {
		t.Fatal("the map kept the old description")
	}

	beforeLock = func() { edit("## Unverified", "## Unverified\n- A hand edit.") }
	t.Cleanup(func() { beforeLock = func() {} })
	_, err = write(o, "orders", valid, true)
	beforeLock = func() {}
	if text := refusal(t, err); !strings.Contains(text, "the page changed while this write ran") {
		t.Fatalf("refusal %q", text)
	}
	if !strings.Contains(file(t, o, "systems/orders.md"), "- A hand edit.") {
		t.Fatal("the hand edit was overwritten")
	}

	for _, tc := range []struct{ old, new, want string }{
		{"status: stable\n", "verified: { by: human:alex, at: \"2026-10-31T08:00:00Z\" }\nstatus: stable\n", "a person marked the page verified"},
		{"state: \"active\"\n", "locked: true\nstate: \"active\"\n", "a person locked the page"},
		{"state: \"active\"\n", "locked: yes\nstate: \"active\"\n", "a person locked the page"},
		{"---\ntype: system\n", "type: system\n", "the page on disk can't be read as a wiki page"},
	} {
		text := edit(tc.old, tc.new)
		_, err := write(o, "orders", valid, true)
		if got := refusal(t, err); !strings.Contains(got, tc.want) {
			t.Fatalf("refusal %q, want %q", got, tc.want)
		}
		if file(t, o, "systems/orders.md") != text {
			t.Fatalf("a refused write changed the page (%s)", tc.want)
		}
		edit(tc.new, tc.old)
	}
}

// A map okf can't read stops the write before the page is written; the map
// is rebuilt, never edited, so removing it is the fix.
func TestDamagedMap(t *testing.T) {
	o := fixture(t)
	mustWrite(t, o, "orders", valid, false)
	mapFile := filepath.Join(o.Config.WikiPath, "systems", "map.md")
	if err := os.WriteFile(mapFile, []byte("A map without frontmatter.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := write(o, "payments", other("Payments", "Charges cards", "github.com/acme/payments"), false)
	if text := refusal(t, err); !strings.Contains(text, "systems/map.md can't be read as a wiki page") {
		t.Fatalf("refusal %q", text)
	}
	if _, err := os.Stat(filepath.Join(o.Config.WikiPath, "systems", "payments.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the page was written")
	}
	if err := os.Remove(mapFile); err != nil {
		t.Fatal(err)
	}
	if r := mustWrite(t, o, "payments", other("Payments", "Charges cards", "github.com/acme/payments"), false); r.Systems != 2 {
		t.Fatalf("result %+v", r)
	}
}

// A refused page writes nothing, names the secret's line and never holds
// the secret.
func TestSecretRefusedWritesNothing(t *testing.T) {
	o := fixture(t)
	secret := "ghp_" + strings.Repeat("ab12", 10)
	_, err := write(o, "orders", with(t, "- Orders may retry a payment twice.", "- Deploys with "+secret+"."), false)
	text := refusal(t, err)
	if !strings.Contains(text, "line 23: what looks like a secret") || strings.Contains(err.Error(), secret) {
		t.Fatalf("refusal %q", text)
	}
	if _, err := os.Stat(filepath.Join(o.Config.WikiPath, "systems")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a refused write created the folder")
	}
}

// A write checks the wiki's schema and instance against the config first.
func TestWriteChecksTheWiki(t *testing.T) {
	o := fixture(t)
	for _, tc := range []struct {
		name string
		edit func(*Options)
		want string
	}{
		{"no wiki", func(o *Options) { o.Config.WikiPath = "" }, "no wiki is set up"},
		{"no instance", func(o *Options) { o.Config.Instance = "" }, "the config names no wiki instance"},
		{"other instance", func(o *Options) { o.Config.Instance = "other-00000000" }, "the wiki's instance is not the one the config names"},
	} {
		c := o
		tc.edit(&c)
		if _, err := write(c, "orders", valid, false); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	schema := filepath.Join(o.Config.WikiPath, ".eva-md", "schema.toml")
	s, err := config.LoadSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	s.SchemaVersion = 2
	if err := config.SaveSchema(schema, s); err != nil {
		t.Fatal(err)
	}
	if _, err := write(o, "orders", valid, false); err == nil || !strings.Contains(err.Error(), "schema version is 2") {
		t.Fatalf("newer schema: %v", err)
	}
	if _, err := os.Stat(filepath.Join(o.Config.WikiPath, "systems")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a refused write created the folder")
	}
}

// The write waits for the wiki lock another process holds, and writes once
// it is released.
func TestWriteWaitsForTheWikiLock(t *testing.T) {
	o := fixture(t)
	release := holdLock(t, filepath.Join(o.Dirs.Locks(), "wiki.lock"))
	done := make(chan error, 1)
	go func() { _, err := write(o, "orders", valid, false); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("the write did not wait for the lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	page := filepath.Join(o.Config.WikiPath, "systems", "orders.md")
	if _, err := os.Stat(page); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the page was written while another process held the lock")
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(page); err != nil {
		t.Fatal(err)
	}
}

// holdLock starts this test binary as another process that holds the lock
// file until release is called.
func holdLock(t *testing.T, file string) (release func()) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldsWikiLock$")
	cmd.Env = append(os.Environ(), "EVA_MD_TEST_HOLD_LOCK="+file)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("helper: %q %v", line, err)
	}
	done := false
	release = func() {
		if !done {
			done = true
			stdin.Close()
			_ = cmd.Wait()
		}
	}
	t.Cleanup(release)
	return release
}

// TestHelperHoldsWikiLock is the other process of holdLock: it holds the
// lock until its stdin closes. Run directly, it does nothing.
func TestHelperHoldsWikiLock(t *testing.T) {
	file := os.Getenv("EVA_MD_TEST_HOLD_LOCK")
	if file == "" {
		t.Skip("run by holdLock")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("locked\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// A repo written as a remote, a normalized remote or a key gets the key the
// inbox and identity give the project, the hash key included.
func TestProjectKeyMatchesIdentity(t *testing.T) {
	home := isolate(t)
	for i, remote := range []string{
		"git@github.com:acme/orders.git",
		"https://GitHub.com/Acme/Orders_Service.git",
		"ssh://git@gitlab.acme.internal:2222/platform/orders-api.git",
		"git@gitlab:group/sub/repo.git",
		"https://github.com/acme/ghp_" + strings.Repeat("ab12", 9) + ".git",
	} {
		dir := filepath.Join(home, "repo", string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		git(t, dir, "init", "--quiet")
		git(t, dir, "remote", "add", "origin", remote)
		p, err := identity.Resolve(context.Background(), dir)
		if err != nil || p.Kind != identity.KindRepo {
			t.Fatalf("%s: %+v %v", remote, p, err)
		}
		if key, ok := projectKey(remote); !ok || key != p.Key {
			t.Errorf("%s: key %q, identity %q", remote, key, p.Key)
		}
		if key, ok := projectKey(p.Key); !ok || key != p.Key {
			t.Errorf("%s: the key %q reads back as %q", remote, p.Key, key)
		}
		host, _, _ := strings.Cut(p.Remote, "/")
		if key, ok := projectKey(p.Remote); strings.Contains(host, ".") && (!ok || key != p.Key) {
			t.Errorf("%s: the remote %q gives %q", remote, p.Remote, key)
		}
	}
	for _, repo := range []string{"", "orders", "acme/orders", "github.com", "file:///srv/git/orders.git", "/srv/git/orders", "local-Orders-a1b2c3", "github.com__acme__Orders"} {
		if key, ok := projectKey(repo); ok {
			t.Errorf("%q accepted as %q", repo, key)
		}
	}
}
