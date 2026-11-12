package hostfacts

import (
	"slices"
	"strings"
	"testing"
)

// Every name here is made up.
func TestReplacerReplacesEveryNameItsWay(t *testing.T) {
	r := NewReplacer(Names{
		Eval:     Account{Short: "hfevalmadeup", Full: "HF Eval Madeup", Home: "/Users/hfevalmadeup"},
		Others:   []Account{{Short: "quinnow", Full: "Quinn O'Wynne", Home: "/Users/quinnow"}, {Short: "jo", Full: "Jo", Home: "/Users/jo"}},
		Computer: []string{"Quinns-Orchard-Mac", "orchard-mac.local"},
		Remote:   &Remote{Address: "git@git.madeup-forge.test:pears/hf-scratch-plum.git", Host: "git.madeup-forge.test", Port: "2222", Namespace: "pears", Path: "pears/hf-scratch-plum"},
	})
	for in, want := range map[string]string{
		"/Users/hfevalmadeup/scratch/alpha-orchard": "/EVAL-HOME/scratch/alpha-orchard",
		"-Users-hfevalmadeup-scratch-alpha-orchard": "-EVAL-HOME-scratch-alpha-orchard",
		"by HFEVALMADEUP":                                     "by EVAL-USER|by EVAL-FULL-NAME",
		"hf eval madeup wrote":                                "EVAL-FULL-NAME wrote",
		"quinn o’wynne and Quinn  O'Wynne":                    "OTHER-FULL-NAME and OTHER-FULL-NAME",
		"/Users/quinnow/x":                                    "/OTHER-HOME/x",
		"to jo, not joke or major":                            "to OTHER-USER, not joke or major|to OTHER-FULL-NAME, not joke or major",
		"on quinns orchard mac and ORCHARD-MAC.LOCAL":         "on COMPUTER-NAME and COMPUTER-NAME",
		"git@git.madeup-forge.test:pears/hf-scratch-plum.git": "REMOTE-ADDRESS",
		"ssh://git.madeup-forge.test:2222/x":                  "ssh://REMOTE-HOST:REMOTE-PORT/x",
		"nothing here":                                        "nothing here",
	} {
		// A name that is also another name, as a short name written like
		// the full name, may take either placeholder.
		if got := r.String(in); !slices.Contains(strings.Split(want, "|"), got) {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	v := r.Value(map[string]any{"/Users/quinnow": []any{"hfevalmadeup", true}})
	m := v.(map[string]any)
	if got, ok := m["/OTHER-HOME"].([]any); !ok || got[0] == "hfevalmadeup" || got[1] != true {
		t.Errorf("Value: %v", v)
	}
	line, err := r.Line(map[string]string{"cwd": "/Users/hfevalmadeup"})
	if err != nil || strings.Contains(string(line), "hfevalmadeup") || !strings.HasSuffix(string(line), "\n") {
		t.Errorf("Line: %q %v", line, err)
	}
}

// A name is replaced only where it stands as a word of its own, so a short
// account name or a plain group name leaves the hosts' words and the
// recorder's own keys whole; the longest name is taken first, and two
// names side by side are both taken.
func TestReplacerTakesWholeWordsOnly(t *testing.T) {
	r := NewReplacer(Names{
		Eval: Account{Short: "hfevalmadeup", Full: "HF Eval Madeup", Home: "/Users/hfevalmadeup"},
		Others: []Account{
			{Short: "tim", Full: "Tim Plumtree", Home: "/Users/tim"},
			{Short: "art", Home: "/Users/art"},
			{Short: "tom"}, {Short: "ken"}, {Short: "mark"}, {Short: "event"},
			{Short: "jo", Full: "Jo Bloggs", Home: "/Users/jo"},
		},
		Remote: &Remote{Address: "git@forge.madeup.test:agent/hf-scratch-plum.git", Host: "forge.madeup.test", Namespace: "agent", Path: "agent/hf-scratch-plum"},
	})
	for in, want := range map[string]string{
		"timestamp SessionStart custom_tool_call token_count": "timestamp SessionStart custom_tool_call token_count",
		"SubagentStart agent_message":                         "SubagentStart agent_message",
		"signed, Jo Bloggs":                                   "signed, OTHER-FULL-NAME",
		"jo bloggs wrote":                                     "OTHER-FULL-NAME wrote",
		"jo jo":                                               "OTHER-USER OTHER-USER",
		"jo/jo":                                               "OTHER-USER/OTHER-USER",
		"to tim, from art and Tom's":                          "to OTHER-USER, from OTHER-USER and OTHER-USER's",
		"/Users/tim/scratch -Users-art-x":                     "/OTHER-HOME/scratch -OTHER-HOME-x",
		"the agent group":                                     "the REMOTE-NAMESPACE group",
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	line, err := r.Line(Event{V: 1, Mark: "C-01", Event: "SessionStart", Host: "claude-code",
		Payload: map[string]any{"timestamp": "x", "hook_event_name": "SessionStart", "who": "tim", "/Users/tim/f": true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"event":"SessionStart"`, `"mark":"C-01"`, `"timestamp":"x"`, `"hook_event_name":"SessionStart"`, `"who":"OTHER-USER"`, `"/OTHER-HOME/f":true`} {
		if !strings.Contains(string(line), want) {
			t.Errorf("Line lacks %s: %s", want, line)
		}
	}
	// A recorder's key that is also a name is the recorder's word, kept.
	if strings.Contains(string(line), `"OTHER-USER":"C-01"`) || strings.Contains(string(line), `"OTHER-USER":"SessionStart"`) {
		t.Errorf("Line replaced the recorder's own keys: %s", line)
	}
}

// A name that is also a word the recording keeps is found before sitting
// 0; a name inside such a word is not a clash.
func TestClashes(t *testing.T) {
	got := Clashes(NewReplacer(Names{Others: []Account{{Short: "user"}, {Short: "art"}}, Remote: &Remote{Namespace: "agent", Host: "forge.madeup.test", Path: "agent/hf-scratch-plum"}}))
	for _, w := range []string{"user", "Agent"} {
		if !slices.Contains(got, w) {
			t.Errorf("Clashes lacks %q: %v", w, got)
		}
	}
	for _, w := range got {
		if strings.Contains(w, "Start") || w == "SessionStart" {
			t.Errorf("Clashes holds %q, which a whole-word name does not change", w)
		}
	}
	if got := Clashes(NewReplacer(Names{Eval: Account{Short: "hfevalmadeup", Full: "HF Eval Madeup", Home: "/Users/hfevalmadeup"}})); len(got) != 0 {
		t.Errorf("Clashes of plain names: %v", got)
	}
}

// The longest name is taken first whatever order the names are added in:
// here an account's short name is added before the computer's name that
// holds it, and the evaluation account's before the scratch remote's path
// that holds it, so a replacer that took names in the order given would
// leave part of each longer name in place.
func TestReplacerTakesTheLongestNameFirst(t *testing.T) {
	n := Names{
		Eval:     Account{Short: "plumtest"},
		Others:   []Account{{Short: "orchard"}},
		Computer: []string{"orchard-mac"},
		Remote:   &Remote{Address: "git@git.madeup-forge.test:pears/plumtest-tools.git", Host: "git.madeup-forge.test", Namespace: "pears", Path: "pears/plumtest-tools"},
	}
	r := NewReplacer(n)
	for in, want := range map[string]string{
		"on orchard-mac.":             "on COMPUTER-NAME.",
		"at pears/plumtest-tools now": "at REMOTE-PATH now",
		"orchard and plumtest":        "OTHER-USER and EVAL-USER",
	} {
		if got := r.String(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestHashIsKeyed(t *testing.T) {
	a, b := Hash([]byte("key one"), "plum"), Hash([]byte("key two"), "plum")
	if a == b || len(a) != 16 || a != Hash([]byte("key one"), "plum") {
		t.Errorf("%s %s", a, b)
	}
}
