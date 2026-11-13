package ledgerkit

import "testing"

func TestMaxLargest(t *testing.T) {
	if got, ok := Max([]int{-350, 1205, 45}); !ok || got != 1205 {
		t.Fatalf("Max = %d, %v, want 1205, true", got, ok)
	}
	if got, ok := Max([]int{-350, -45}); !ok || got != -45 {
		t.Fatalf("Max = %d, %v, want -45, true", got, ok)
	}
}

func TestMaxEmpty(t *testing.T) {
	if _, ok := Max(nil); ok {
		t.Fatal("Max(nil) found an amount")
	}
}
