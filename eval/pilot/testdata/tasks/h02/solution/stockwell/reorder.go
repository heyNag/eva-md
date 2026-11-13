package stockwell

import "sort"

// ReorderBelow lists, sorted, the SKUs whose count is below limit.
func (s *Shelf) ReorderBelow(limit int) []string {
	var skus []string
	for sku, n := range s.counts {
		if n < limit {
			skus = append(skus, sku)
		}
	}
	sort.Strings(skus)
	return skus
}

// MergeShelf adds every count of other to the shelf.
func (s *Shelf) MergeShelf(other *Shelf) {
	for sku, n := range other.counts {
		s.counts[sku] += n
	}
}
