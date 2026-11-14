package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
)

// eva-md doctor --notice prepares the line a chat start may show, prints
// nothing and exits 0 whatever happens: with no config, a config that
// can't be read, the switch health off, and on, where it writes the
// notice.
func TestDoctorNoticePrintsNothing(t *testing.T) {
	d := isolated(t)
	notice := filepath.Join(d.State, "health", "notice.json")
	check := func(when string, written bool) {
		t.Helper()
		code, out, errOut := evaMD(t, "doctor", "--notice")
		if code != 0 || out != "" || errOut != "" {
			t.Fatalf("%s: %d %q %q", when, code, out, errOut)
		}
		if _, err := os.Lstat(notice); (err == nil) != written {
			t.Fatalf("%s: notice.json: %v", when, err)
		}
	}
	check("no config", false)
	if err := os.MkdirAll(filepath.Dir(d.ConfigFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.ConfigFile(), []byte("this is = = not toml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	check("a config that can't be read", false)
	c := config.Default()
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	check("the switch off", false)
	c.Switches = map[string]any{"health": true}
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	check("the switch on", true)
	data, err := os.ReadFile(notice)
	var n struct {
		V        int            `json:"v"`
		Prepared string         `json:"prepared"`
		Lines    map[string]any `json:"lines"`
	}
	if err != nil || json.Unmarshal(data, &n) != nil || n.V != 1 || n.Prepared == "" || n.Lines == nil {
		t.Fatalf("notice.json %s: %v", data, err)
	}
}

// --notice is the doctor's alone, and takes no other flag or argument.
func TestDoctorNoticeTakesNoOtherFlag(t *testing.T) {
	isolated(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"status", "--notice"}, "eva-md: status takes no --notice; run eva-md doctor --notice\n"},
		{[]string{"doctor", "--notice", "--json"}, "eva-md: doctor --notice takes no other flag\n"},
		{[]string{"doctor", "--notice", "--report"}, "eva-md: doctor --notice takes no other flag\n"},
		{[]string{"doctor", "--notice", "now"}, "eva-md: invalid arguments for doctor\n"},
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), c.args, strings.NewReader(""), &out, &errOut); code != 2 || out.Len() != 0 || errOut.String() != c.want {
			t.Errorf("eva-md %s: %d %q %q", strings.Join(c.args, " "), code, out.String(), errOut.String())
		}
	}
}

// With the switch health on, the doctor and status print the verdict
// first, the one their --json gives; off, the first line is a row.
func TestDoctorPrintsTheVerdictFirst(t *testing.T) {
	d := isolated(t)
	c := config.Default()
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	firstLine := func(args ...string) string {
		t.Helper()
		_, out, _ := evaMD(t, args...)
		line, _, _ := strings.Cut(out, "\n")
		return line
	}
	for _, command := range []string{"doctor", "status"} {
		if line := firstLine(command); !strings.HasPrefix(line, "PASS  ") && !strings.HasPrefix(line, "FAIL  ") && !strings.HasPrefix(line, "UNKNOWN  ") {
			t.Errorf("%s, switch off: the first line is %q", command, line)
		}
	}
	c.Switches = map[string]any{"health": true}
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"doctor", "status"} {
		_, out, _ := evaMD(t, command, "--json")
		var r health.Report
		if err := json.Unmarshal([]byte(out), &r); err != nil || r.Verdict == "" {
			t.Fatalf("%s --json: %v %q", command, err, out)
		}
		if line := firstLine(command); line != r.Verdict {
			t.Errorf("%s: the first line is %q, the verdict %q", command, line, r.Verdict)
		}
	}
}
