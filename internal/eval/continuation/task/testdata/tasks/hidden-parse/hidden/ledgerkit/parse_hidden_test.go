package ledgerkit

import (
	"strings"
	"testing"
)

func TestParseReads(t *testing.T) {
	for text, want := range map[string]int{"12.05": 1205, "-3.50": -350, "0.07": 7} {
		got, err := Parse(text)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v, want %d", text, got, err, want)
		}
	}
	if _, err := Parse("twelve"); err == nil {
		t.Error("Parse read text that is not an amount")
	}
}

func TestParseErrors(t *testing.T) {
	for _, text := range []string{"twelve", "12,05", ""} {
		if _, err := Parse(text); err == nil || !strings.HasPrefix(err.Error(), "quillspar:") {
			t.Errorf("Parse(%q) gave error %v", text, err)
		}
	}
}
