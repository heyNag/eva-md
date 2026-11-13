package ledgerkit

import "errors"

// Split shares total cents out into parts amounts that differ by at most
// one cent, the extra cents on the first parts.
func Split(total, parts int) ([]int, error) {
	if parts <= 0 {
		return nil, errors.New("splinterbox: no parts to split into")
	}
	out := make([]int, parts)
	for i := range out {
		out[i] = total / parts
		if i < total%parts {
			out[i]++
		}
	}
	return out, nil
}
