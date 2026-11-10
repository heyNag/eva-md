package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testSwitches registers a boolean switch, b, off by default, and a word
// switch, w, "off" by default, for one test.
func testSwitches(t *testing.T) (BoolSwitch, WordSwitch) {
	t.Helper()
	t.Cleanup(func() { delete(switches, "b"); delete(switches, "w") })
	return NewBoolSwitch("b", false), NewWordSwitch("w", "off", "off", "shadow", "on")
}

// load writes body as a config file and loads it.
func load(t *testing.T, body string) (Config, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(file)
}

// The config package registers no switch of its own; the packages that
// are switched on register theirs.
func TestNoSwitchIsRegisteredHere(t *testing.T) {
	if len(switches) != 0 {
		t.Fatalf("switches registered outside a test: %v", switches)
	}
}

// Without a [switches] table, a registered switch reads its default and
// the config is as it was before switches: nothing unknown, no table, and
// none written by a save.
func TestSwitchesReadTheirDefaultsWithoutATable(t *testing.T) {
	isolate(t)
	b, w := testSwitches(t)
	on := NewBoolSwitch("on_by_default", true)
	t.Cleanup(func() { delete(switches, "on_by_default") })
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(file, []byte("notice = \"start\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if b.On(c) || w.Value(c) != "off" || !on.On(c) || c.Unknown != nil || c.Switches != nil {
		t.Fatalf("b %v, w %q, on_by_default %v, unknown %q, switches %v", b.On(c), w.Value(c), on.On(c), c.Unknown, c.Switches)
	}
	if b.On(Default()) || w.Value(Default()) != "off" || !on.On(Default()) {
		t.Fatal("Default() does not read the defaults")
	}
	if err := Save(file, c); err != nil {
		t.Fatal(err)
	}
	if saved, _ := os.ReadFile(file); strings.Contains(string(saved), "switches") {
		t.Fatalf("a save wrote a switches table:\n%s", saved)
	}
}

// With a [switches] table, a registered switch reads the table's value,
// which a save keeps; an empty table is no unknown key and is not written.
func TestSwitchesReadTheTable(t *testing.T) {
	isolate(t)
	b, w := testSwitches(t)
	c, err := load(t, "notice = \"start\"\n[switches]\nb = true\nw = \"shadow\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !b.On(c) || w.Value(c) != "shadow" || c.Unknown != nil {
		t.Fatalf("b %v, w %q, unknown %q", b.On(c), w.Value(c), c.Unknown)
	}
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(file, c); err != nil {
		t.Fatal(err)
	}
	again, err := Load(file)
	if err != nil || !reflect.DeepEqual(again, c) {
		t.Fatalf("round trip: %+v, %v; want %+v", again, err, c)
	}
	c, err = load(t, "b = 1\n[switches]\nb = false\n")
	if err != nil || b.On(c) || !reflect.DeepEqual(c.Unknown, []string{"b"}) {
		t.Fatalf("a top-level b: switch %v, unknown %q, %v", b.On(c), c.Unknown, err)
	}
	c, err = load(t, "[switches]\n")
	if err != nil || c.Unknown != nil || b.On(c) || w.Value(c) != "off" {
		t.Fatalf("empty table: unknown %q, %v", c.Unknown, err)
	}
	if err := Save(file, c); err != nil {
		t.Fatal(err)
	}
	if saved, _ := os.ReadFile(file); strings.Contains(string(saved), "switches") {
		t.Fatalf("a save wrote an empty switches table:\n%s", saved)
	}
}

// A name under [switches] that no package registered is an unknown key,
// named as an unknown key at the top is, a table's own keys included; it
// is ignored, not saved, and the registered switches beside it still read.
func TestUnregisteredSwitchIsAnUnknownKey(t *testing.T) {
	isolate(t)
	b, _ := testSwitches(t)
	c, err := load(t, "colour = 1\n[extra]\nkey = 1\n[switches]\nb = true\nnope = \"on\"\n\"a.b\" = true\n[switches.deep]\nx = 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"colour", "extra", "extra.key", "switches.nope", `switches."a.b"`, "switches.deep", "switches.deep.x"}; !reflect.DeepEqual(c.Unknown, want) {
		t.Fatalf("unknown %q, want %q", c.Unknown, want)
	}
	if !b.On(c) || !reflect.DeepEqual(c.Switches, map[string]any{"b": true}) {
		t.Fatalf("switch b %v, switches %v", b.On(c), c.Switches)
	}
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(file, c); err != nil {
		t.Fatal(err)
	}
	if saved, _ := os.ReadFile(file); strings.Contains(string(saved), "nope") || strings.Contains(string(saved), "deep") || !strings.Contains(string(saved), "b = true") {
		t.Fatalf("saved:\n%s", saved)
	}
	// Unknown keys keep the file's order, wherever [switches] stands.
	for body, want := range map[string][]string{
		"switches.nope = true\ncolour = 1\n":                       {"switches.nope", "colour"},
		"[switches]\nnope = 1\nb = true\n[zzz]\na = 1\n":           {"switches.nope", "zzz", "zzz.a"},
		"[zzz]\na = 1\n[switches]\nnope = 1\n[yyy]\nc = 1\n":       {"zzz", "zzz.a", "switches.nope", "yyy", "yyy.c"},
		"colour = 1\nswitches = {nope = 1, b = true}\nshade = 2\n": {"colour", "switches.nope", "shade"},
	} {
		c, err := load(t, body)
		if err != nil || !reflect.DeepEqual(c.Unknown, want) {
			t.Errorf("%q: unknown %q, %v; want %q", body, c.Unknown, err, want)
		}
	}
	// A switches that is no table is unknown, every key under it with it,
	// as it was before there were switches: the config still loads, and
	// keeps none of it, so a registered switch reads its default.
	for body, want := range map[string][]string{
		"switches = true\n":                 {"switches"},
		"switches = 5\n":                    {"switches"},
		"switches = \"on\"\n":               {"switches"},
		"switches = [1, 2]\n":               {"switches"},
		"switches = [{b = true}]\n":         {"switches", "switches.b"},
		"[[switches]]\nb = true\n":          {"switches", "switches.b"},
		"colour = 1\nswitches = 1\nz = 2\n": {"colour", "switches", "z"},
	} {
		c, err := load(t, body)
		if err != nil || !reflect.DeepEqual(c.Unknown, want) || c.Switches != nil || b.On(c) {
			t.Errorf("%q: unknown %q, switches %v, b %v, %v; want unknown %q", body, c.Unknown, c.Switches, b.On(c), err, want)
		}
	}
	// An empty table is no unknown key.
	for _, body := range []string{"[switches]\n", "switches = {}\n"} {
		if c, err := load(t, body); err != nil || c.Unknown != nil {
			t.Errorf("%q: unknown %q, %v", body, c.Unknown, err)
		}
	}
	// With no switch registered at all, every name is unknown.
	delete(switches, "b")
	delete(switches, "w")
	c, err = load(t, "[switches]\nb = true\n")
	if err != nil || !reflect.DeepEqual(c.Unknown, []string{"switches.b"}) || c.Switches["b"] != nil {
		t.Fatalf("unknown %q, switches %v, %v", c.Unknown, c.Switches, err)
	}
}

// A value its switch does not accept makes the config invalid as a bad
// notice does: Load gives Default() and an error that names the switch and
// what it accepts and is no missing file, and Save refuses it.
func TestBadSwitchValueInvalidatesTheConfig(t *testing.T) {
	isolate(t)
	testSwitches(t)
	for _, tc := range []struct{ body, msg string }{
		{"notice = \"loud\"\n", `notice must be "start" or "problems"`},
		{"[switches]\nb = \"yes\"\n", "switch b must be true or false"},
		{"[switches]\nb = 1\n", "switch b must be true or false"},
		{"[switches.b]\nx = true\n", "switch b must be true or false"},
		{"[switches]\nw = \"loud\"\n", `switch w must be "off", "shadow" or "on"`},
		{"[switches]\nw = true\n", `switch w must be "off", "shadow" or "on"`},
		{"[switches]\nb = true\nw = \"ON\"\n", `switch w must be "off", "shadow" or "on"`},
	} {
		c, err := load(t, tc.body)
		if err == nil || errors.Is(err, fs.ErrNotExist) || !strings.HasSuffix(err.Error(), tc.msg) {
			t.Errorf("%q: error %v, want one ending %q", tc.body, err, tc.msg)
		}
		if !reflect.DeepEqual(c, Default()) {
			t.Errorf("%q: config %+v, want Default()", tc.body, c)
		}
	}
	c := Default()
	c.Switches = map[string]any{"w": "loud"}
	if err := Save(filepath.Join(t.TempDir(), "config.toml"), c); err == nil {
		t.Error("Save wrote a bad switch value")
	}
	c.Switches = map[string]any{"nope": true}
	if err := Save(filepath.Join(t.TempDir(), "config.toml"), c); err == nil {
		t.Error("Save wrote a switch no package registered")
	}
}

// A switch registered twice, under a name that is no bare lower-case key,
// or with a default not among its words, panics as it is registered.
func TestSwitchRegistrationRefusesMistakes(t *testing.T) {
	testSwitches(t)
	for name, register := range map[string]func(){
		"twice":           func() { NewBoolSwitch("b", true) },
		"twice as a word": func() { NewWordSwitch("b", "on", "on") },
		"empty":           func() { NewBoolSwitch("", true) },
		"upper case":      func() { NewBoolSwitch("Shadow", true) },
		"dotted":          func() { NewBoolSwitch("a.b", true) },
		"digit first":     func() { NewBoolSwitch("2fast", true) },
		"default":         func() { NewWordSwitch("x", "loud", "off", "on") },
		"no words":        func() { NewWordSwitch("x", "") },
	} {
		before := len(switches)
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: registered", name)
				}
			}()
			register()
		}()
		if len(switches) != before {
			t.Errorf("%s: the registry grew", name)
		}
	}
}
