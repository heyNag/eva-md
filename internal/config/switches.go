package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// switchesTable is the config file's table of switches.
const switchesTable = "switches"

// A switch is a setting under [switches] that a package registers, so that
// it can be off by default and turned on by a setting without a field of
// its own in Config: a boolean, or one of a short list of words. A name no
// package registered is reported as an unknown key, as an unknown key at
// the top is, and a value its switch does not accept makes the config
// invalid, as a bad notice does. A switchSpec is what a registered switch
// accepts: the words of a word switch, or nil for a boolean; its default is
// kept by its BoolSwitch or WordSwitch.
type switchSpec struct {
	words []string
}

// switches are the registered switches by name. Packages register theirs
// as they are initialized, before any config is loaded.
var switches = map[string]switchSpec{}

// A BoolSwitch is a registered switch that is true or false.
type BoolSwitch struct {
	name string
	def  bool
}

// A WordSwitch is a registered switch that is one of a short list of words.
type WordSwitch struct {
	name, def string
}

// NewBoolSwitch registers a switch that is true or false, def when the
// config doesn't set it. Call it from a package-level variable, so it is
// registered before any config is loaded. A name taken already, or one
// that is not a bare lower-case TOML key, panics.
func NewBoolSwitch(name string, def bool) BoolSwitch {
	registerSwitch(name, switchSpec{})
	return BoolSwitch{name, def}
}

// NewWordSwitch registers a switch that is one of words, def when the
// config doesn't set it, as NewBoolSwitch does. A default not among the
// words, or no words, panics.
func NewWordSwitch(name, def string, words ...string) WordSwitch {
	if !slices.Contains(words, def) {
		panic(fmt.Sprintf("config: switch %q: default %q is not one of %q", name, def, words))
	}
	registerSwitch(name, switchSpec{words: slices.Clone(words)})
	return WordSwitch{name, def}
}

func registerSwitch(name string, s switchSpec) {
	if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" || name[0] == '_' || name[0] >= '0' && name[0] <= '9' {
		panic(fmt.Sprintf("config: switch name %q must be lower-case letters, digits and _, starting with a letter", name))
	}
	if _, taken := switches[name]; taken {
		panic(fmt.Sprintf("config: switch %q is registered twice", name))
	}
	switches[name] = s
}

// On is the switch's value in c: the config's, or the default when the
// config doesn't set it.
func (s BoolSwitch) On(c Config) bool {
	if v, ok := c.Switches[s.name].(bool); ok {
		return v
	}
	return s.def
}

// Value is the switch's word in c: the config's, or the default when the
// config doesn't set it.
func (s WordSwitch) Value(c Config) string {
	if v, ok := c.Switches[s.name].(string); ok {
		return v
	}
	return s.def
}

// unknownKeys lists the keys in the file that eva-md does not know, in the
// file's order: those the decode left undecoded, and under [switches] each
// name no package registered, a table's own keys included, whose values it
// drops from c. A switches that is no table, such as switches = true, an
// array or an array of tables, is unknown with every key under it, as it
// was before there were switches, and c keeps none of it.
func unknownKeys(md toml.MetaData, c *Config) []string {
	undecoded := md.Undecoded()
	kind := md.Type(switchesTable)
	// A [switches] made only of dotted keys, as switches.b = true, is
	// defined but has no type of its own.
	if kind == "" && !md.IsDefined(switchesTable) {
		// No switches in the file: what the decode left, as before.
		var unknown []string
		for _, key := range undecoded {
			unknown = append(unknown, key.String())
		}
		return unknown
	}
	table := kind == "Hash" || kind == ""
	if !table {
		c.Switches = nil
	}
	left := make(map[string]bool, len(undecoded))
	for _, key := range undecoded {
		left[key.String()] = true
	}
	var unknown []string
	for _, key := range md.Keys() {
		name := key.String()
		switch {
		case key[0] != switchesTable:
			if left[name] {
				unknown = append(unknown, name)
			}
		case !table:
			unknown = append(unknown, name)
		case len(key) >= 2:
			if _, ok := switches[key[1]]; !ok {
				unknown = append(unknown, name)
				delete(c.Switches, key[1])
			}
		}
	}
	return unknown
}

// checkSwitches reports the first switch in c whose value its switch does
// not accept, in the order of the names.
func checkSwitches(c Config) error {
	if len(c.Switches) == 0 {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(c.Switches)) {
		s, ok := switches[name]
		if !ok {
			return fmt.Errorf("switch %q is not one eva-md knows", name)
		}
		v := c.Switches[name]
		if s.words == nil {
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("switch %s must be true or false", name)
			}
			continue
		}
		if w, ok := v.(string); !ok || !slices.Contains(s.words, w) {
			return fmt.Errorf("switch %s must be %s", name, oneOf(s.words))
		}
	}
	return nil
}

// oneOf words a switch's words as notice's are: "a" or "b", or "a", "b" or
// "c".
func oneOf(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = fmt.Sprintf("%q", w)
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}
