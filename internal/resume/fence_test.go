package resume

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestFence checks the exported fence: the label on its own line, a fence of
// at least 4 backticks with the eva-memory info string, one longer than the
// longest run of backticks in the text, and the brief's own output made by
// the same function.
func TestFence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	cases := []struct{ text, fence string }{
		{"plain text", "````"},
		{"one ` and two ``", "````"},
		{"a ``` run", "````"},
		{"a ```` run", "`````"},
		{"x\n``````\ny", "```````"},
	}
	for _, c := range cases {
		got := Fence("A label.", c.text)
		want := "A label.\n" + c.fence + "eva-memory\n" + c.text + "\n" + c.fence
		if got != want {
			t.Errorf("Fence(%q) = %q, want %q", c.text, got, want)
		}
		// Nothing inside closes the fence: no line of the text is a run of
		// backticks as long as the fence.
		for _, l := range strings.Split(c.text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), c.fence) {
				t.Errorf("a line of %q closes the fence %q", c.text, c.fence)
			}
		}
	}
	// The brief fences through the same function, so its output is unchanged.
	if got, want := wrap("body", 10000), Fence(label, "body"); got != want {
		t.Errorf("wrap = %q, want Fence's %q", got, want)
	}
	if got := wrap("body", 10000); runes(got) != runes("body")+overhead("body") {
		t.Errorf("wrap's overhead changed: %d runes for %d of text and %d of overhead", runes(got), runes("body"), overhead("body"))
	}
}
