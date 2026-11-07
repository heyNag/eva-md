package resume

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The brief's three system lines go first to the pages that cite this
// project's own code, then to the project's other pages, each in the map's
// order, and eva_status lists the project's systems in the same order. The
// order costs no git: near the brief's deadline, with no time for a count,
// the pages are ranked the same, each showing its day.
func TestBriefRanksSystemsCitingTheProjectsCode(t *testing.T) {
	o, c := codeFixture(t)
	key, other := o.Project.Key, "github.com__team__other"
	writeMap(t, o,
		mapLine("alpha", "Alpha", "Lists the project", key),
		codeLine("bravo", "Bravo", key+", "+other, other+"@4e2bf4c:cmd"),
		mapLine("charlie", "Charlie", "Lists the project too", key),
		codeLine("delta", "Delta", key, key+"@"+c[0][:7]+":internal/orders"),
		codeLine("echo", "Echo", other, other+"@4e2bf4c:internal"),
		codeLine("foxtrot", "Foxtrot", key, key+"@"+c[2][:7]+":README.md"))
	line := func(slug, title, description, note string) string {
		return "System: " + title + ": " + description + " (" + note + "): " + usualBin + " show systems/" + slug
	}
	last := "System index: 6 systems: " + usualBin + " show systems/index"
	r, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		line("delta", "Delta", "Takes orders", "code moved 2 commits since 2026-10-28"),
		line("foxtrot", "Foxtrot", "Takes orders", "code unchanged since 2026-10-28"),
		line("alpha", "Alpha", "Lists the project", "as of 2026-10-28"),
		last,
	}
	if got := pointers(r.Text); !reflect.DeepEqual(got, want) {
		t.Fatalf("pointers\n%s\nin:\n%s", strings.Join(got, "\n"), r.Text)
	}
	systems, total := SystemsFor(context.Background(), o)
	var slugs []string
	for _, e := range systems {
		slugs = append(slugs, e.Slug)
	}
	if strings.Join(slugs, " ") != "delta foxtrot alpha bravo charlie" || total != 6 {
		t.Fatalf("eva_status's systems %v of %d", slugs, total)
	}
	logged := gitLog(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), gitReserve+time.Minute)
	defer cancel()
	want = []string{
		line("delta", "Delta", "Takes orders", "as of 2026-10-28"),
		line("foxtrot", "Foxtrot", "Takes orders", "as of 2026-10-28"),
		line("alpha", "Alpha", "Lists the project", "as of 2026-10-28"),
		last,
	}
	if got := pointersWithin(ctx, o, "", &codeClock{left: 2 * time.Minute, call: time.Minute, budget: time.Minute}); got != strings.Join(want, "\n") || logged() != "" {
		t.Fatalf("near the deadline:\n%s\ngit ran:\n%s", got, logged())
	}
}
