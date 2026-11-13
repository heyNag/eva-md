package ledgerkit

// Max returns the largest amount, and false when there is none.
func Max(amounts []int) (int, bool) {
	if len(amounts) == 0 {
		return 0, false
	}
	largest := amounts[0]
	for _, a := range amounts[1:] {
		largest = max(largest, a)
	}
	return largest, true
}
