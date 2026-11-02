package systempage

import (
	"slices"
	"testing"
)

// Links reads a body's links to system pages as markdown reads them: inline
// links and reference definitions, with a #section or ./ before the name,
// each page once in the order first linked, but no link in a code span, no
// URL, no section of the page itself, no reserved name such as the map and
// no file that is no system page.
func TestLinksReadAsMarkdown(t *testing.T) {
	body := []string{
		"## Depends on",
		"- [Payments](payments.md) and [its API](payments.md#Interfaces)",
		"- [Billing](./billing.md), `[not a link](ledger.md)`, [docs](https://docs.example.com/x.md)",
		"[ref]: orders.md",
		"[Section](#Summary) and the [system map](map.md)",
		"- [Gone](../systems/gone.md) [Bad](Bad_Name.md) [mail](mailto:a@example.com)",
	}
	if got := Links(body); !slices.Equal(got, []string{"payments", "billing", "orders"}) {
		t.Fatalf("links %q", got)
	}
}
