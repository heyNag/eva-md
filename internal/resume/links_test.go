package resume

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/systempage"
)

// After a system page or a section of it, eva-md show and eva_show name the
// system pages that link to it, sorted, at most 12 and then how many more,
// as markdown reads their links: never the map and never a link in a code
// span. A page no page links to, the map itself and a ref of another kind
// get no line.
func TestLinkedFromNamesTheLinkingPages(t *testing.T) {
	o := fixture(t)
	so := systemsWiki(t, &o)
	write := func(slug, title, body string) {
		t.Helper()
		if _, err := systempage.Write(context.Background(), so, slug, false, strings.NewReader(systemPage(title, title+" in short", "2026-10-28", "github.com/team/resume", body))); err != nil {
			t.Fatal(err)
		}
	}
	write("payments", "Payments", "## Summary\nPayments charges cards.\n")
	write("orders", "Orders", "## Summary\nOrders are paid through [Payments](payments.md).\n")
	write("billing", "Billing", "## Summary\nBilling bills [orders](orders.md#Summary).\n\n## Depends on\n- [Payments](payments.md)\n")
	write("ledger", "Ledger", "## Summary\nThe ledger's notes once read `[Payments](payments.md)`.\n")
	for ref, want := range map[string]string{
		"systems/payments":             "eva.md: systems/payments: linked from systems/billing, systems/orders",
		"systems/payments#Summary":     "eva.md: systems/payments: linked from systems/billing, systems/orders",
		"systems/orders.md":            "eva.md: systems/orders: linked from systems/billing",
		"systems/billing":              "",
		"systems/map":                  "",
		"projects/resume/threads/main": "",
		"s:abc#1":                      "",
	} {
		if got := LinkedFrom(context.Background(), o.Config, ref); got != want {
			t.Errorf("%s: %q, want %q", ref, got, want)
		}
	}
	for i := range 13 {
		write(fmt.Sprintf("lnk-%02d", i), fmt.Sprintf("Link%02d", i), "## Summary\nUses [Payments](payments.md).\n")
	}
	want := "eva.md: systems/payments: linked from systems/billing"
	for i := range 11 {
		want += fmt.Sprintf(", systems/lnk-%02d", i)
	}
	if got := LinkedFrom(context.Background(), o.Config, "systems/payments"); got != want+" and 3 more" {
		t.Fatalf("past the cap: %q", got)
	}
}
