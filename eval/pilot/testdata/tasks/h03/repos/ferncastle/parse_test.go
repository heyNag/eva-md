package ferncastle

import (
	"errors"
	"maps"
	"testing"
)

func TestParseSettingsAndComments(t *testing.T) {
	got, err := Parse("# colours\nfg = green \n\n  bg=black\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"fg": "green", "bg": "black"}; !maps.Equal(got, want) {
		t.Errorf("Parse = %v, want %v", got, want)
	}
}

func TestParseNoEquals(t *testing.T) {
	_, err := Parse("fg = green\njust words\n")
	var le *LineError
	if !errors.Is(err, ErrNoEquals) || !errors.As(err, &le) || le.Line != 2 {
		t.Errorf("Parse = %v, want ErrNoEquals on line 2", err)
	}
}
