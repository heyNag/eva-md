package ferncastle

import (
	"errors"
	"maps"
	"testing"
)

func TestHiddenStanzas(t *testing.T) {
	got, err := Parse("mode = fast\n[net]\nport = 80\n# a comment\n[disk]\nport = 9\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"mode": "fast", "net.port": "80", "disk.port": "9"}
	if !maps.Equal(got, want) {
		t.Errorf("Parse = %v, want %v", got, want)
	}
}

func TestHiddenTwiceSet(t *testing.T) {
	_, err := Parse("[net]\nport = 80\nhost = a\nport = 81\n")
	var le *LineError
	if !errors.Is(err, ErrTwiceSet) || !errors.As(err, &le) || le.Line != 4 {
		t.Errorf("Parse = %v, want ErrTwiceSet on line 4", err)
	}
	if _, err := Parse("[a]\nport = 1\n[b]\nport = 2\n"); err != nil {
		t.Errorf("one key in two stanzas: %v", err)
	}
}
