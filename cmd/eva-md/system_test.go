package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// systemPage is a system page that writes, linking the pages named.
func systemPage(title string, links ...string) string {
	page := "---\ntype: system\ntitle: " + title + "\ndescription: " + title + " for the fixture\nstatus: stable\n" +
		"generated: { by: claude-code/claude-opus-5-5 }\nsources:\n  - resource: \"https://docs.example.com/eng/fixture?version=7\"\n" +
		"repos: [git@github.com:acme/fixture.git]\nas_of: 2026-10-30\nstate: active\n---\n\n## Summary\n" + title + " in short.\n\n" +
		"## Interfaces\n- `POST /v1/orders` with key ENG-1234\n"
	if len(links) > 0 {
		page += "\n## Depends on\n"
		for _, l := range links {
			page += "- [" + l + "](" + l + ".md)\n"
		}
	}
	return page
}

// eva-md system write writes a page from stdin with the map, exits 1 on a
// refusal that names lines and never their text, and 2 on bad arguments;
// eva-md show reads the map and a section of a page.
func TestSystemCommand(t *testing.T) {
	dirs, home := gitHomeFixture(t, "")
	cmd := func(page string, args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(page), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	orders := systemPage("Orders")
	for _, args := range [][]string{{"system"}, {"system", "read", "orders"}, {"system", "write"}, {"system", "write", "orders", "payments"}, {"system", "write", "Orders"}, {"system", "write", "map"}, {"system", "write", "orders", "--force"}} {
		if code, out, errOut := cmd(orders, args...); code != 2 || out != "" || !strings.HasPrefix(errOut, "eva-md: ") {
			t.Errorf("%v: %d %q %q", args, code, out, errOut)
		}
	}
	if code, _, errOut := cmd(orders, "system", "write", "orders"); code != 1 || !strings.Contains(errOut, "no wiki is set up") {
		t.Fatalf("without a wiki: %d %q", code, errOut)
	}
	remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
	if _, err := wikigit.Run(context.Background(), time.Minute, home, "init", "--bare", "--quiet", remote); err != nil {
		t.Fatal(err)
	}
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: wiki, Dirs: dirs, Version: "test", OKFModule: "v0.7.2"}); err != nil {
		t.Fatal(err)
	}
	size := func(slug string) int64 {
		info, err := os.Stat(filepath.Join(wiki, "systems", slug+".md"))
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	if code, out, errOut := cmd(orders, "system", "write", "orders"); code != 0 || errOut != "" || out != fmt.Sprintf("wrote systems/orders.md (%d bytes); the map has 1 system\n", size("orders")) {
		t.Fatalf("write: %d %q %q", code, out, errOut)
	}
	if code, out, errOut := cmd(systemPage("Payments", "orders"), "system", "write", "payments"); code != 0 || out != fmt.Sprintf("wrote systems/payments.md (%d bytes); the map has 2 systems\n", size("payments")) {
		t.Fatalf("second write: %d %q %q", code, out, errOut)
	}
	if code, out, errOut := cmd(orders, "system", "write", "orders"); code != 1 || out != "" || !strings.Contains(errOut, "read it with eva-md show systems/orders, merge, and write it again with --replace") {
		t.Fatalf("existing page: %d %q %q", code, out, errOut)
	}
	if code, out, errOut := cmd(systemPage("Orders", "payments"), "system", "write", "orders", "--replace"); code != 0 || !strings.HasPrefix(out, "wrote systems/orders.md (") {
		t.Fatalf("replace: %d %q %q", code, out, errOut)
	}
	secret := "ghp_" + strings.Repeat("ab12", 10)
	leak := strings.Replace(orders, "with key ENG-1234", "with token "+secret, 1)
	code, out, errOut := cmd(leak, "system", "write", "ledger")
	if code != 1 || out != "" || errOut != "eva-md: systems/ledger.md was not written:\n  line 18: what looks like a secret, which the daily commit would leave out; take it out\n" {
		t.Fatalf("refusal: %d %q %q", code, out, errOut)
	}
	if strings.Contains(errOut, secret) {
		t.Fatal("the refusal printed the secret")
	}

	code, out, errOut = cmd("", "show", "systems/map")
	if code != 0 || !strings.Contains(out, "\n- [Orders](orders.md): Orders for the fixture · repos: github.com__acme__fixture · as of 2026-10-30\n- [Payments](payments.md): ") {
		t.Fatalf("show the map: %d %q %q", code, out, errOut)
	}
	code, out, errOut = cmd("", "show", "systems/orders#Interfaces")
	if code != 0 || out != "## Interfaces\n- `POST /v1/orders` with key ENG-1234\n" {
		t.Fatalf("show a section: %d %q %q", code, out, errOut)
	}
	// The last section runs to the end of the page, the map's link included.
	const dependsOn = "## Depends on\n- [orders](orders.md)\n\nPart of the [system map](map.md).\n"
	code, out, _ = cmd("", "show", "systems/payments#Depends on")
	if code != 0 || out != dependsOn {
		t.Fatalf("show a section named in two words: %d %q", code, out)
	}
	// The ref search prints for it opens it typed as printed, split as a
	// shell splits it.
	code, out, errOut = cmd("", "search", "orders.md", "--cwd", home)
	var ref string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "[system] ") && strings.HasSuffix(l, " · - [orders](orders.md)") {
			ref = strings.Split(l, " · ")[1]
		}
	}
	if code != 0 || ref != "systems/payments#Depends-on" {
		t.Fatalf("search: %d %q %q", code, out, errOut)
	}
	if code, out, errOut := cmd("", append([]string{"show"}, strings.Fields(ref)...)...); code != 0 || out != dependsOn {
		t.Fatalf("show %s: %d %q %q", ref, code, out, errOut)
	}
}
