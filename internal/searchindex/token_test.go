package searchindex

import (
	"strings"
	"testing"
)

// Tokens are runs of letters and numbers, lowered rune by rune; a run with
// camelCase or letter and digit boundaries also gives its parts, unless it
// has more than 8, and a run over 64 bytes gives nothing. The count
// returned is the tokens emitted.
func TestTokenize(t *testing.T) {
	sha := "3f2a9c1e5b7d0a4c8e6f2b1d9a7c5e3f1b0d8a6c"
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"ui-kit", []string{"ui", "kit"}},
		{"internal/resume/search.go", []string{"internal", "resume", "search", "go"}},
		{"SecretLines", []string{"secretlines", "secret", "lines"}},
		{"gpt5", []string{"gpt5", "gpt", "5"}},
		{"HTTPServer", []string{"httpserver", "http", "server"}},
		{"readmax", []string{"readmax"}},
		{"Straße ÜBER", []string{"straße", "über"}},
		{"ǄEMAL ǅemal", []string{"ǆemal", "ǆemal"}},
		{"日本語のテキスト", []string{"日本語のテキスト"}},
		{"→ … !! && --", nil},
		{"", nil},
		{strings.Repeat("a", 64), []string{strings.Repeat("a", 64)}},
		{strings.Repeat("a", 65) + " kept", []string{"kept"}},
		{strings.Repeat("aB", 33), nil},
		{sha, []string{sha}},
		{"a1b2c3d4", []string{"a1b2c3d4", "a", "1", "b", "2", "c", "3", "d", "4"}},
		{"a1b2c3d4e", []string{"a1b2c3d4e"}},
	} {
		var got []string
		n := Tokenize(tc.in, func(s string) { got = append(got, s) })
		if strings.Join(got, " ") != strings.Join(tc.want, " ") || n != len(got) {
			t.Errorf("Tokenize(%q) = %q, %d", tc.in, got, n)
		}
		for _, tok := range got {
			if strings.ToLower(tok) != tok {
				t.Errorf("Tokenize(%q) emitted %q, not lowered as strings.ToLower lowers", tc.in, tok)
			}
		}
	}
}

// A query term gives its runs whole, never the parts a document's runs
// give, and none at all when it holds no letter or digit or a run over 64
// bytes.
func TestQueryTokens(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"Amber Switch", "amber switch", true},
		{"ReadMax", "readmax", true},
		{"C#", "c", true},
		{`"C:\temp\new"`, "c temp new", true},
		{"a<b", "a b", true},
		{"&&", "", false},
		{strings.Repeat("x", 65), "", false},
		{"ok " + strings.Repeat("x", 65), "", false},
	} {
		got, ok := QueryTokens(tc.in)
		if strings.Join(got, " ") != tc.want || ok != tc.ok {
			t.Errorf("QueryTokens(%q) = %q, %v", tc.in, got, ok)
		}
	}
	if !Prefixable("abc") || Prefixable("ab") || !Prefixable("ßüé") {
		t.Fatal("Prefixable counts characters, from 3")
	}
}

// A query is left to the scan when a term has no token or the terms have
// more than 32 tokens; it is plain when every term is one word of 3 or
// more letters and digits.
func TestNewQuery(t *testing.T) {
	if _, ok := NewQuery([]string{"fine", "&&"}); ok {
		t.Fatal("a term with no token was accepted")
	}
	if _, ok := NewQuery([]string{strings.Repeat("w ", 33)}); ok {
		t.Fatal("33 tokens were accepted")
	}
	for _, tc := range []struct {
		terms []string
		plain bool
	}{
		{[]string{"amber", "Switch"}, true},
		{[]string{"amber switch"}, false},
		{[]string{"ui"}, false},
		{[]string{"a<b"}, false},
		{[]string{"-race"}, false},
	} {
		q, ok := NewQuery(tc.terms)
		if !ok || q.Plain() != tc.plain {
			t.Errorf("NewQuery(%q): ok %v, plain %v", tc.terms, ok, q != nil && q.Plain())
		}
	}
}
