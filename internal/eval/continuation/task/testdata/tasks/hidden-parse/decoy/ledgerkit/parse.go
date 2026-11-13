package ledgerkit

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse reads an amount written as Format writes it into cents.
func Parse(s string) (int, error) {
	neg := strings.HasPrefix(s, "-")
	units, cents, ok := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	if !ok || units == "" || len(cents) != 2 {
		return 0, fmt.Errorf("ledgerkit: cannot read %q as an amount", s)
	}
	u, err := strconv.Atoi(units)
	if err != nil || u < 0 {
		return 0, fmt.Errorf("ledgerkit: cannot read %q as an amount", s)
	}
	c, err := strconv.Atoi(cents)
	if err != nil || c < 0 {
		return 0, fmt.Errorf("ledgerkit: cannot read %q as an amount", s)
	}
	total := u*100 + c
	if neg {
		total = -total
	}
	return total, nil
}
