// Package ledgerkit adds up amounts kept in cents.
package ledgerkit

import "fmt"

// Sum returns the total of amounts, in cents.
func Sum(amounts []int) int {
	total := 0
	for _, a := range amounts {
		total += a
	}
	return total
}

// Format writes cents as units and two decimals, such as 12.05 or -3.50.
func Format(cents int) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}
