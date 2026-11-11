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

func TestHashIsKeyed(t *testing.T) {
	a, b := Hash([]byte("key one"), "plum"), Hash([]byte("key two"), "plum")
	if a == b || len(a) != 16 || a != Hash([]byte("key one"), "plum") {
		t.Errorf("%s %s", a, b)
	}
}
