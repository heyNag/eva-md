package ledgerkit

import (
	"slices"
	"strings"
	"testing"
)

func TestSplitSpreads(t *testing.T) {
	got, err := Split(1001, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{334, 334, 333}; !slices.Equal(got, want) {
		t.Fatalf("Split(1001, 3) = %v, want %v", got, want)
	}
}

func TestSplitRefusesZero(t *testing.T) {
	if _, err := Split(100, 0); err == nil || !strings.HasPrefix(err.Error(), "splinterbox:") {
		t.Fatalf("Split(100, 0) gave error %v", err)
	}
}
