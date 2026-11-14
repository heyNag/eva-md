package health

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/update"
)

// The update package's host names, which it keeps, are the adapter's ids,
// and health's hosts are those ids in v1's order.
func TestUpdateHostsAreTheAdaptersIDs(t *testing.T) {
	if update.HostClaude != string(adapter.ClaudeCode) || update.HostCodex != string(adapter.Codex) {
		t.Fatalf("update's hosts %q and %q, the adapter's %q and %q", update.HostClaude, update.HostCodex, adapter.ClaudeCode, adapter.Codex)
	}
	if want := []string{update.HostClaude, update.HostCodex}; !slices.Equal(updateHosts, want) {
		t.Fatalf("health's hosts %q, want %q", updateHosts, want)
	}
}

// Each host's event list, as the doctor and status read it from the
// adapter, is v1's, in v1's order; an install name or a name eva-md doesn't
// know has none.
func TestHostEventsAreV1s(t *testing.T) {
	both := []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop", "PreCompact", "SessionEnd"}
	for host, want := range map[string][]string{
		"claude-code": append(slices.Clone(both), "PostToolUseFailure", "StopFailure", "PostCompact", "PostModelSwitch"),
		"codex":       append(slices.Clone(both), "Interrupt"),
	} {
		if got := hostEvents(host); !slices.Equal(got, want) {
			t.Errorf("%s's events %q, want %q", host, got, want)
		}
	}
	for _, host := range []string{"claude", "Codex", "", "generic"} {
		if got := hostEvents(host); got != nil {
			t.Errorf("%q has events %q", host, got)
		}
	}
}

// The names health spells from the adapter are v1's: each host's command,
// app, chat folder and the variable that moves it, the login a hold's fix
// names, and the marketplace folder, the same for a host's id and its
// install name and none for a name eva-md doesn't know.
func TestHealthSpellsV1sNames(t *testing.T) {
	d := isolated(t)
	for _, c := range []struct {
		names               []string
		cli, store, moves   string
		login, marketplaces string
	}{
		{[]string{"claude-code", "claude"}, "claude", "~/.claude/projects", "CLAUDE_CONFIG_DIR", "log in with claude auth login", "claude"},
		{[]string{"codex"}, "codex", "~/.codex/sessions", "CODEX_HOME", "log in with codex login", "codex"},
	} {
		for _, name := range c.names {
			if got := cli(name); got != c.cli {
				t.Errorf("cli(%q) = %q, want %q", name, got, c.cli)
			}
			if got, want := marketplace(d, name), filepath.Join(d.Share, "marketplace", c.marketplaces); got != want {
				t.Errorf("marketplace(%q) = %q, want %q", name, got, want)
			}
		}
		id := c.names[0]
		if got := store(id); got != c.store {
			t.Errorf("store(%q) = %q, want %q", id, got, c.store)
		}
		if got := moves(id); got != c.moves {
			t.Errorf("moves(%q) = %q, want %q", id, got, c.moves)
		}
		if got := holdFix(update.ReasonAuth, id); got != c.login {
			t.Errorf("holdFix(%q) = %q, want %q", id, got, c.login)
		}
	}
	for _, name := range []string{"", "generic", "Claude Code", "CODEX"} {
		if got := marketplace(d, name); got != "" {
			t.Errorf("marketplace(%q) = %q", name, got)
		}
		if _, ok := hostOf(name); ok {
			t.Errorf("hostOf(%q) found a host", name)
		}
	}
}

// Installed reads a host's marketplace marker by its id or its install
// name alike, and a name eva-md doesn't know is never installed, even with
// a marker straight under the marketplace folder.
func TestInstalledByIDOrInstallName(t *testing.T) {
	d := isolated(t)
	marker := []byte("eva-md marketplace v1\n")
	put(t, filepath.Join(d.Share, "marketplace", "claude", ".eva-md-managed"), marker)
	put(t, filepath.Join(d.Share, "marketplace", ".eva-md-managed"), marker)
	put(t, filepath.Join(d.Share, "marketplace", "generic", ".eva-md-managed"), marker)
	for name, want := range map[string]bool{"claude-code": true, "claude": true, "codex": false, "": false, "generic": false} {
		if got := Installed(d, name); got != want {
			t.Errorf("Installed(%q) = %v, want %v", name, got, want)
		}
	}
	put(t, filepath.Join(d.Share, "marketplace", "codex", ".eva-md-managed"), marker)
	if !Installed(d, "codex") {
		t.Error("codex's marker is not read")
	}
}

// Absent looks for each host's own command, by its id or its install name,
// and names its app; a name eva-md doesn't know is absent, with no app.
func TestAbsentByIDOrInstallName(t *testing.T) {
	isolated(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, app string
		absent    bool
	}{
		{"claude-code", "Claude Code", true},
		{"claude", "Claude Code", true},
		{"codex", "Codex", false},
		{"generic", "", true},
		{"", "", true},
	} {
		if app, absent := Absent(c.name); app != c.app || absent != c.absent {
			t.Errorf("Absent(%q) = %q, %v; want %q, %v", c.name, app, absent, c.app, c.absent)
		}
	}
}

// An inbox session file is a host's by the prefix of its id and a hyphen;
// an install name, a name eva-md doesn't know or a prefix with no hyphen is
// not one.
func TestHostFile(t *testing.T) {
	for name, want := range map[string]bool{
		"claude-code-0a1b.jsonl": true,
		"codex-0a1b.jsonl":       true,
		"claude-0a1b.jsonl":      false,
		"codex0a1b.jsonl":        false,
		"generic-0a1b.jsonl":     false,
		"x-codex-0a1b.jsonl":     false,
	} {
		if got := hostFile(name); got != want {
			t.Errorf("hostFile(%q) = %v, want %v", name, got, want)
		}
	}
}

// The trust flag counts only when it names, by its install name, the host
// whose plugin needs a review: Codex. Naming Claude Code, by either
// spelling, or another host, the notice says the trust status is
// unavailable and the flag isn't awaited.
func TestTrustFlagNamesTheReviewHost(t *testing.T) {
	for _, c := range []struct {
		host    string
		notice  string
		awaited bool
	}{
		{"codex", "eva.md: review its hooks in Codex /hooks; run eva-md doctor to verify.", true},
		{"claude", "eva.md: hook trust status is unavailable; run eva-md doctor.", false},
		{"claude-code", "eva.md: hook trust status is unavailable; run eva-md doctor.", false},
		{"Codex", "eva.md: hook trust status is unavailable; run eva-md doctor.", false},
	} {
		d := isolated(t)
		put(t, filepath.Join(d.State, "needs-trust.json"), []byte(`{"host":"`+c.host+`","needs_trust":true}`))
		if got := QuickNotice(context.Background(), d, config.Default()); got != c.notice {
			t.Errorf("%q: notice %q, want %q", c.host, got, c.notice)
		}
		if got := TrustAwaited(d, time.Now()); got != c.awaited {
			t.Errorf("%q: awaited %v, want %v", c.host, got, c.awaited)
		}
	}
}

// The doctor's rule for the hooks feature, PASS only on a last column of
// true, FAIL on any other, UNKNOWN with no hooks row, holds on each input
// on which install's rule, which goes on unless a last column is false,
// gives the other answer, as HO-10b found them, and on those where the two
// agree.
func TestDoctorsHooksFeatureRuleWhereInstallsDiffers(t *testing.T) {
	d := isolated(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	list := filepath.Join(t.TempDir(), "features")
	// PATH holds the stand-in alone, so it prints the list with the shell's
	// own commands.
	script := "#!/bin/sh\n[ \"$*\" = 'features list' ] || exit 2\n" +
		"while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < '" + list + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	pass := Check{"codex hooks feature", "PASS", "enabled", ""}
	fail := Check{"codex hooks feature", "FAIL", "disabled", "run codex features enable hooks"}
	unknown := Check{"codex hooks feature", "UNKNOWN", "not reported by this version", ""}
	for _, c := range []struct {
		out     string
		want    Check
		differs bool // install's rule gives the other answer
	}{
		{"hooks experimental false\nhooks stable true\n", pass, true},
		{"hooks stable TRUE\n", fail, true},
		{"hooks stable False\n", fail, true},
		{"hooks stable on\n", fail, true},
		{"hooks true\nhooks maybe\n", fail, true},
		{"", unknown, true},
		{"apps experimental false\n", unknown, true},
		{"hooks\n", unknown, true},
		{"hooks_v2 stable false\n", unknown, true},
		{"hooks experimental true\n", pass, false},
		{"hooks stable false\n", fail, false},
		{"hooks true\nhooks false\n", fail, false},
		{"  hooks \t experimental   false  \r\n", fail, false},
	} {
		if err := os.WriteFile(list, []byte(c.out), 0o600); err != nil {
			t.Fatal(err)
		}
		installGoesOn := !adapter.ReadHooksRows([]byte(c.out)).Off()
		doctorPasses := c.want.Status == "PASS"
		if differ := installGoesOn != doctorPasses; differ != c.differs {
			t.Errorf("%q: the rules differ %v, want %v", c.out, differ, c.differs)
		}
		var r Report
		inspectHosts(context.Background(), &r, d, inventoryTimeout)
		if row := findRow(t, r, "codex hooks feature"); row != c.want {
			t.Errorf("%q: %+v, want %+v", c.out, row, c.want)
		}
		for _, row := range r.Checks {
			if strings.HasPrefix(row.Name, "claude ") && strings.HasSuffix(row.Name, " feature") {
				t.Errorf("%q: Claude Code, which lists no features, has a row %+v", c.out, row)
			}
		}
	}
}

// QuickNotice reads the transcript drift of every host, Codex as well as
// Claude Code: each host's drift alone gives its own notice, named by its
// id, and so does each of them with the other host's drift recorded too,
// in the hosts' order.
func TestQuickNoticeReadsEachHostsDrift(t *testing.T) {
	notice := func(host string) string {
		return "eva.md: transcript recovery no longer recognizes " + host + "'s transcripts, so missed replies are not recovered; run eva-md doctor."
	}
	for _, c := range []struct {
		drifted []string
		want    string
	}{
		{[]string{"codex"}, notice("codex")},
		{[]string{"claude-code"}, notice("claude-code")},
		{[]string{"codex", "claude-code"}, notice("claude-code")},
	} {
		d := isolated(t)
		if got := QuickNotice(context.Background(), d, config.Default()); got != "" {
			t.Fatalf("%q: an isolated home has the notice %q", c.drifted, got)
		}
		for _, host := range c.drifted {
			put(t, filepath.Join(d.State, "catchup", "drift-"+host+".json"), []byte(`{"v":1,"host":"`+host+`"}`))
		}
		if got := QuickNotice(context.Background(), d, config.Default()); got != c.want {
			t.Errorf("%q drifted: notice %q, want %q", c.drifted, got, c.want)
		}
	}
}
