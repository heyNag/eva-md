package ledgerkit

import (
	"errors"
	"strconv"
	"strings"
)

// errAmount is the error for text that is not an amount.
var errAmount = errors.New("quillspar: not an amount")

// Parse reads an amount written as Format writes it into cents.
func Parse(s string) (int, error) {
	neg := strings.HasPrefix(s, "-")
	units, cents, ok := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	if !ok || units == "" || len(cents) != 2 {
		return 0, errAmount
	}
	u, err := strconv.Atoi(units)
	if err != nil || u < 0 {
		return 0, errAmount
	}
	c, err := strconv.Atoi(cents)
	if err != nil || c < 0 {
		return 0, errAmount
	}
	total := u*100 + c
	if neg {
		total = -total
	}
	return total, nil
}
