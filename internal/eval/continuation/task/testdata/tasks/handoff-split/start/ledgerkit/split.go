package ledgerkit

// Split shares total cents out into parts amounts.
func Split(total, parts int) ([]int, error) {
	out := make([]int, parts)
	for i := range out {
		out[i] = total / parts
	}
	return out, nil
}
