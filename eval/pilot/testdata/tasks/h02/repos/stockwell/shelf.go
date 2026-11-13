// Package stockwell keeps a shelf of stock counts by SKU.
package stockwell

import "errors"

// ErrShort is returned when the shelf holds fewer units than asked for.
var ErrShort = errors.New("stockwell: not enough units on the shelf")

// Shelf holds stock counts by SKU. Make one with New.
type Shelf struct {
	counts map[string]int
}

// New returns an empty shelf.
func New() *Shelf {
	return &Shelf{counts: map[string]int{}}
}

// Add adds n units of sku.
func (s *Shelf) Add(sku string, n int) {
	s.counts[sku] += n
}

// Remove takes n units of sku away, or fails with ErrShort and changes
// nothing. A SKU that falls to 0 stays on the shelf.
func (s *Shelf) Remove(sku string, n int) error {
	if s.counts[sku] < n {
		return ErrShort
	}
	s.counts[sku] -= n
	return nil
}

// Total is the number of units on the shelf.
func (s *Shelf) Total() int {
	total := 0
	for _, n := range s.counts {
		total += n
	}
	return total
}
