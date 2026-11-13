package quillmark

import (
	"strings"
	"testing"
)

func TestHiddenWidestWord(t *testing.T) {
	if got := Render(Summarize("aa bbb ccc\nd\n")); !strings.Contains(got, "linetally: 2\nwidestword: bbb\n") {
		t.Errorf("Render = %q, want the first longest word on a widestword line after linetally", got)
	}
	if got := Render(Summarize("")); !strings.Contains(got, "widestword: -\n") {
		t.Errorf("Render of an empty text = %q, want widestword as a dash", got)
	}
}

func TestHiddenUniqTally(t *testing.T) {
	if got := Render(Summarize("Ab ab cd\nef cd\n")); !strings.HasSuffix(got, "uniqtally: 3\n") {
		t.Errorf("Render = %q, want uniqtally: 3 last", got)
	}
}

func TestHiddenWholeReport(t *testing.T) {
	want := "wordtally: 5\nlinetally: 2\nwidestword: Ab\nuniqtally: 3\n"
	if got := Render(Summarize("Ab ab cd\nef cd\n")); got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}
