package stockwell

import (
	"slices"
	"testing"
)

func TestHiddenReorderBelow(t *testing.T) {
	s := New()
	s.Add("SKU-7", 5)
	s.Add("SKU-3", 2)
	s.Add("SKU-1", 1)
	if err := s.Remove("SKU-1", 1); err != nil {
		t.Fatal(err)
	}
	if got := s.ReorderBelow(3); !slices.Equal(got, []string{"SKU-1", "SKU-3"}) {
		t.Errorf("ReorderBelow(3) = %v", got)
	}
}

func TestHiddenMergeShelf(t *testing.T) {
	s, other := New(), New()
	s.Add("SKU-7", 1)
	other.Add("SKU-7", 2)
	other.Add("SKU-3", 3)
	s.MergeShelf(other)
	if s.Total() != 6 || other.Total() != 5 {
		t.Errorf("after MergeShelf the totals are %d and %d, want 6 and 5", s.Total(), other.Total())
	}
	if err := s.Remove("SKU-7", 3); err != nil {
		t.Errorf("the merged shelf holds fewer than 3 of SKU-7: %v", err)
	}
}
