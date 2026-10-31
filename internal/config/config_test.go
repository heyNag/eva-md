package config

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func TestDefault(t *testing.T) {
	isolate(t)
	c := Default()
	if c.Notice != NoticeStart || !c.Updates || !c.Commits || c.DailyCap != DefaultDailyCap || c.CodexIgnoreUserConfig || c.ReminderEvery != DefaultReminderEvery {
		t.Fatalf("Default() = %+v", c)
	}
}

// commits = false stops the daily commit and push on its own: absent it is
// true, updates = false leaves it on, and a misspelled key is reported as
// unknown rather than read as the switch.
func TestCommitsSwitch(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	for _, tc := range []struct {
		body             string
		updates, commits bool
		unknown          []string
	}{
		{"notice = \"start\"\n", true, true, nil},
		{"commits = false\n", true, false, nil},
		{"updates = false\n", false, true, nil},
		{"updates = false\ncommits = true\n", false, true, nil},
		{"commit = false\n", true, true, []string{"commit"}},
	} {
		if err := os.WriteFile(file, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(file)
		if err != nil {
			t.Fatalf("%q: %v", tc.body, err)
		}
		if c.Updates != tc.updates || c.Commits != tc.commits || !reflect.DeepEqual(c.Unknown, tc.unknown) {
			t.Errorf("%q: updates %v, commits %v, unknown %q", tc.body, c.Updates, c.Commits, c.Unknown)
		}
	}
	if err := os.WriteFile(file, []byte("commits = \"no\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Error("a string for commits was accepted")
	}
}

// reminder_every spaces the prompt's memory reminder: absent it is 5, an
// explicit 0 turns it off and stays 0 through a save, and a negative number
// or a string is refused.
func TestReminderEvery(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	for _, tc := range []struct {
		body string
		want int
	}{
		{"notice = \"start\"\n", DefaultReminderEvery},
		{"reminder_every = 0\n", 0},
		{"reminder_every = 3\n", 3},
	} {
		if err := os.WriteFile(file, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(file)
		if err != nil || c.ReminderEvery != tc.want || c.Unknown != nil {
			t.Fatalf("%q: reminder_every %d, unknown %q, %v; want %d", tc.body, c.ReminderEvery, c.Unknown, err, tc.want)
		}
		if err := Save(file, c); err != nil {
			t.Fatal(err)
		}
		if again, err := Load(file); err != nil || again.ReminderEvery != tc.want {
			t.Fatalf("%q saved and read back: %d, %v", tc.body, again.ReminderEvery, err)
		}
	}
	for _, body := range []string{"reminder_every = -1\n", "reminder_every = \"5\"\n"} {
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(file); err == nil {
			t.Errorf("%q was accepted", body)
		}
	}
}

// updates = false is the kill switch: absent it is true, and a misspelled
// key is reported as unknown rather than read as the switch.
func TestUpdatesKillSwitch(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	for _, tc := range []struct {
		body    string
		updates bool
		unknown []string
	}{
		{"notice = \"start\"\n", true, nil},
		{"updates = false\n", false, nil},
		{"updates = true\n", true, nil},
		{"update = false\n", true, []string{"update"}},
	} {
		if err := os.WriteFile(file, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(file)
		if err != nil {
			t.Fatalf("%q: %v", tc.body, err)
		}
		if c.Updates != tc.updates || !reflect.DeepEqual(c.Unknown, tc.unknown) {
			t.Errorf("%q: updates %v, unknown %q", tc.body, c.Updates, c.Unknown)
		}
	}
	if err := os.WriteFile(file, []byte("updates = \"no\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Error("a string for updates was accepted")
	}
}

func TestExcludedSymlinkAliases(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	real := filepath.Join(base, "sensitive")
	alias := filepath.Join(base, "alias")
	if err := os.MkdirAll(filepath.Join(real, "repo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	for _, exclude := range []string{real, alias, filepath.Join(alias, "*")} {
		c := Config{Exclude: []string{exclude}}
		for _, target := range []string{filepath.Join(real, "repo"), filepath.Join(alias, "repo")} {
			if !c.Excluded(target, "") {
				t.Errorf("%q did not exclude %q", exclude, target)
			}
		}
	}
}

func TestBoundedPrivateConfigReads(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := syscall.Mkfifo(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Fatal("read FIFO")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", (1<<20)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Fatal("read oversized file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadContext(ctx, file); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled LoadContext: %v", err)
	}
	private := "sensitive-fixture-value"
	for _, body := range []string{"daily_cap = \"" + private + "\"", "notice = \"" + private + "\""} {
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(file); err == nil || strings.Contains(err.Error(), private) {
			t.Fatalf("error leaked config value: %v", err)
		}
	}
}

func TestSchemaRoundTrip(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "wiki", ".eva-md.toml")
	want := WikiSchema{SchemaVersion: 1, OKFSpec: "0.1", OKFModule: "v0.7.2", Instance: "fixture", Created: "2026-10-29"}
	if err := SaveSchema(file, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSchema(file)
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("schema permissions: %v, %v", info, err)
	}
}

func TestLoadMissing(t *testing.T) {
	isolate(t)
	c, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Fatalf("config = %+v, want Default()", c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "eva-md", "config.toml")
	want := Config{
		WikiPath:              "/Users/x/eva-wiki",
		ExpectedRemote:        "github.com/you/eva-wiki",
		Instance:              "mac-1a2b3c4d",
		MachineLabel:          "mac",
		Exclude:               []string{"~/secret", "remote:github.com/acme/*"},
		Notice:                NoticeProblems,
		DailyCap:              12,
		Effort:                map[string]string{"codex": "high", "claude-code/claude-opus-5-5": "max"},
		CodexIgnoreUserConfig: true,
	}
	if err := Save(file, want); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("folder mode = %v, want 0700", di.Mode().Perm())
	}
	got, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoadFillsDefaults(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(file, []byte("wiki_path = \"/w\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if c.WikiPath != "/w" || c.Notice != NoticeStart || c.DailyCap != DefaultDailyCap {
		t.Fatalf("config = %+v", c)
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	isolate(t)
	for name, body := range map[string]string{
		"syntax":        "wiki_path = ",
		"notice":        "notice = \"loud\"\n",
		"negative cap":  "daily_cap = -1\n",
		"relative path": "exclude = [\"projects/secret\"]\n",
		"empty remote":  "exclude = [\"remote:\"]\n",
		"bad glob":      "exclude = [\"/a/[\"]\n",
		"empty effort":  "[effort]\ncodex = \"\"\n",
		"remote query":  "exclude = [\"remote:https://github.com/acme/repo?\"]\n",
	} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(file)
			if err == nil {
				t.Fatalf("want an error for %q", body)
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a bad file must not look missing: %v", err)
			}
			if !reflect.DeepEqual(c, Default()) {
				t.Fatalf("config = %+v, want Default()", c)
			}
		})
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	isolate(t)
	c := Default()
	c.Notice = "never"
	if err := Save(filepath.Join(t.TempDir(), "config.toml"), c); err == nil {
		t.Fatal("want an error")
	}
}

func TestEffortFor(t *testing.T) {
	isolate(t)
	c := Config{Effort: map[string]string{
		"codex":                       "high",
		"claude-code/claude-opus-5-5": "max",
	}}
	for _, tc := range []struct{ host, model, want string }{
		{"claude-code", "claude-opus-5-5", "max"},
		{"claude-code", "claude-sonnet-5", EffortHighest},
		{"claude-code", "", EffortHighest},
		{"codex", "gpt-5.5-codex", "high"},
		{"codex", "", "high"},
	} {
		if got := c.EffortFor(tc.host, tc.model); got != tc.want {
			t.Errorf("EffortFor(%q, %q) = %q, want %q", tc.host, tc.model, got, tc.want)
		}
	}
	if got := Default().EffortFor("codex", "m"); got != EffortHighest {
		t.Errorf("default EffortFor = %q, want %q", got, EffortHighest)
	}
}

func TestExcluded(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := Config{Exclude: []string{
		"~/clients/acme",
		"/opt/work/*/secret",
		"remote:github.com/acme/*",
		"remote:gitlab.corp.example/hr",
	}}
	for _, tc := range []struct {
		dir, remote string
		want        bool
	}{
		{filepath.Join(home, "clients", "acme"), "", true},
		{filepath.Join(home, "clients", "acme", "sub", "deep"), "", true},
		{filepath.Join(home, "clients", "acme2"), "", false},
		{filepath.Join(home, "clients"), "", false},
		{"/opt/work/a/secret", "", true},
		{"/opt/work/a/secret/x", "", true},
		{"/opt/work/a/b/secret", "", false},
		{filepath.Join(home, "code"), "github.com/acme/widgets", true},
		{filepath.Join(home, "code"), "GitHub.com/Acme/Widgets", true},
		{filepath.Join(home, "code"), "github.com/acmeco/widgets", false},
		{"", "gitlab.corp.example/hr/payroll", true},
		{"", "gitlab.corp.example/hrx", false},
		{filepath.Join(home, "code"), "", false},
		{"", "", false},
	} {
		if got := c.Excluded(tc.dir, tc.remote); got != tc.want {
			t.Errorf("Excluded(%q, %q) = %v, want %v", tc.dir, tc.remote, got, tc.want)
		}
	}
	if Default().Excluded(home, "github.com/acme/x") {
		t.Error("an empty exclude list must exclude nothing")
	}
}

func TestLoadRecordsUnknownKeysAndKeepsZeroCap(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	body := "daily_cap = 0\nexcludes = [\"~/secret\"]\n[effort]\ncodex = \"high\"\n[extra]\nkey = 1\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if c.DailyCap != 0 {
		t.Errorf("daily_cap = %d, want an explicit 0 kept", c.DailyCap)
	}
	if want := []string{"excludes", "extra", "extra.key"}; !reflect.DeepEqual(c.Unknown, want) {
		t.Errorf("Unknown = %q, want %q", c.Unknown, want)
	}
	if err := Save(file, c); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(saved)), "unknown") || strings.Contains(string(saved), "excludes") {
		t.Fatalf("unknown keys were saved:\n%s", saved)
	}
}

func TestReadErrorNamesFileOnce(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "config.toml")
	_, err := Load(file)
	if err == nil || strings.Count(err.Error(), file) != 1 || strings.Count(err.Error(), "config:") != 1 {
		t.Fatalf("error = %v", err)
	}
	if err := syscall.Mkfifo(file, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(file)
	if err == nil || strings.Count(err.Error(), file) != 1 || strings.Count(err.Error(), "config:") != 1 {
		t.Fatalf("error = %v", err)
	}
}

func TestRemoteExclusionForms(t *testing.T) {
	isolate(t)
	for _, entry := range []string{
		"remote: github.com/acme/*",
		"remote:github.com/acme/widgets.git",
		"remote:https://github.com/acme/*",
		"remote:git@github.com:acme/widgets.git",
		"remote:ssh://git@github.com/Acme/*.git",
	} {
		c := Config{Notice: NoticeStart, Exclude: []string{entry}}
		if err := c.Validate(); err != nil {
			t.Errorf("%q: %v", entry, err)
		}
		if !c.Excluded("", "github.com/acme/widgets") {
			t.Errorf("%q did not exclude github.com/acme/widgets", entry)
		}
		if c.Excluded("", "github.com/other/widgets") {
			t.Errorf("%q excluded github.com/other/widgets", entry)
		}
	}
}
