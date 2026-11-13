package ledgerkit

import "testing"

func TestSum(t *testing.T) {
	if got := Sum([]int{1205, -350, 45}); got != 900 {
		t.Fatalf("Sum = %d, want 900", got)
	}
	if got := Sum(nil); got != 0 {
		t.Fatalf("Sum(nil) = %d, want 0", got)
	}
}

func TestFormat(t *testing.T) {
	for cents, want := range map[int]string{1205: "12.05", -350: "-3.50", 7: "0.07", 0: "0.00"} {
		if got := Format(cents); got != want {
			t.Errorf("Format(%d) = %q, want %q", cents, got, want)
		}
	}
}
