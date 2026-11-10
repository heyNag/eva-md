package health

import (
	"context"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
)

// A name under [switches] that no package registered is reported as an
// unknown key at the top is: in the doctor's config keys row and in the
// start-of-chat notice, while the config still parses.
func TestUnknownSwitchIsReportedAsAnUnknownKey(t *testing.T) {
	d := isolated(t)
	put(t, d.ConfigFile(), []byte("colour = \"blue\"\n[switches]\nnot_registered = true\n"))
	r := Inspect(context.Background(), d, false)
	if row := findRow(t, r, "config"); row.Status != "PASS" {
		t.Fatalf("config row %+v", row)
	}
	if row := findRow(t, r, "config keys"); row.Status != "FAIL" || row.Detail != "unknown keys are ignored: colour, switches.not_registered" {
		t.Fatalf("config keys row %+v", row)
	}
	c, err := config.Load(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if got := QuickNotice(context.Background(), d, c); !strings.Contains(got, "unknown keys") {
		t.Fatalf("notice %q", got)
	}
}

// A switches that is no table, as switches = true, is reported as an
// unknown key, as it was before there were switches, in the file's order.
func TestSwitchesThatIsNoTableIsAnUnknownKey(t *testing.T) {
	d := isolated(t)
	for body, want := range map[string]string{
		"switches = true\n":                     "switches",
		"switches = [1]\n":                      "switches",
		"[[switches]]\nx = 1\n":                 "switches, switches.x",
		"switches.nope = true\ncolour = 1\n":    "switches.nope, colour",
		"colour = 1\nswitches = 5\nshade = 2\n": "colour, switches, shade",
	} {
		put(t, d.ConfigFile(), []byte(body))
		r := Inspect(context.Background(), d, false)
		if row := findRow(t, r, "config"); row.Status != "PASS" {
			t.Errorf("%q: config row %+v", body, row)
		}
		if row := findRow(t, r, "config keys"); row.Status != "FAIL" || row.Detail != "unknown keys are ignored: "+want {
			t.Errorf("%q: config keys row %+v, want %q", body, row, want)
		}
	}
}
