package stockwell

import (
	"errors"
	"testing"
)

func TestAddRemoveTotal(t *testing.T) {
	s := New()
	s.Add("SKU-7", 4)
	s.Add("SKU-2", 1)
	if err := s.Remove("SKU-7", 3); err != nil {
		t.Fatal(err)
	}
	if got := s.Total(); got != 2 {
		t.Errorf("Total = %d, want 2", got)
	}
}

func TestRemoveTooMany(t *testing.T) {
	s := New()
	s.Add("SKU-7", 1)
	if err := s.Remove("SKU-7", 2); !errors.Is(err, ErrShort) {
		t.Fatalf("Remove = %v, want ErrShort", err)
	}
	if got := s.Total(); got != 1 {
		t.Errorf("Total = %d after a failed Remove, want 1", got)
	}
}
