package update

import (
	"reflect"
	"testing"
)

func TestNormalizeAndContentTokens(t *testing.T) {
	isolate(t)
	if got := normalize("  Use JSONL, for the INBOX!  "); got != "use jsonl for the inbox" {
		t.Errorf("normalize = %q", got)
	}
	if normalize("Use flock.") != normalize("use   flock") || normalize("Use flock") == normalize("Use flocks") {
		t.Error("normalize compares the wrong texts")
	}
	got := contentTokens("Use the flock for a per-session lock in internal/session/lock.go")
	for _, want := range []string{"flock", "per", "session", "lock", "internal/session/lock.go"} {
		if want == "per" {
			if got[want] {
				t.Error("a stopword is a content token")
			}
			continue
		}
		if !got[want] {
			t.Errorf("content tokens lack %q: %v", want, got)
		}
	}
	for _, short := range []string{"use", "the", "for", "in", "a"} {
		if got[short] {
			t.Errorf("%q is a content token", short)
		}
	}
	if shareToken("Ansible playbooks own provisioning", "Nix flakes handle everything") {
		t.Error("two texts without a shared word share a token")
	}
	if !shareToken("Store pages as YAML", "Store pages as JSONL") {
		t.Error("two texts about pages share no token")
	}
	if o := overlap("flock lock", "Use flock for the session lock"); o != 1 {
		t.Errorf("overlap = %v, want 1", o)
	}
	if o := overlap("Switch the lock from flock to a pid file", "Use flock for the session lock"); o >= overlapShare {
		t.Errorf("overlap = %v, want under %v", o, overlapShare)
	}
}

func TestCodeTokens(t *testing.T) {
	isolate(t)
	got := codeTokens("See internal/update/input.go, https://example.com/x?y=1, commit 4e2bf4c9 and redact.Redact; max_tokens, std::vector " +
		"but not and/or, e.g., 3.5ms, deadbeef, 1234567, v1.2.3 or (s:3f2a91c0#14).")
	want := []codeToken{
		{"internal/update/input.go", classPath}, {"https://example.com/x?y=1", classURL}, {"4e2bf4c9", classSHA},
		{"redact.Redact", classIdent}, {"max_tokens", classIdent}, {"std::vector", classIdent},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codeTokens =\n%v\nwant\n%v", got, want)
	}
	hay := "edited /users/me/eva-md/internal/update/input.go after 4e2bf4c and redact.redact"
	for _, tok := range []codeToken{{"input.go", classIdent}, {"Internal/Update/Input.go", classPath}, {"src/input.go", classPath}, {"4e2bf4c9a1", classSHA}, {"REDACT.Redact", classIdent}} {
		if !grounded(tok, hay) {
			t.Errorf("%v is not grounded", tok)
		}
	}
	for _, tok := range []codeToken{{"input_test.go", classIdent}, {"internal/update/inputs.go", classPath}, {"4e2bf4d", classSHA}, {"https://example.com", classURL}} {
		if grounded(tok, hay) {
			t.Errorf("%v is grounded", tok)
		}
	}
}

func TestDates(t *testing.T) {
	isolate(t)
	for text, want := range map[string][]date{
		"freeze on 2026-03-03, not 2026-13-40":    {{2026, 3, 3}},
		"on 3 March 2026 and 14th Jan":            {{2026, 3, 3}, {0, 1, 14}},
		"by March 3, 2026 or Sept 9 or Dec. 1st":  {{2026, 3, 3}, {0, 9, 9}, {0, 12, 1}},
		"at 2026-01-14T10:00:00+01:00":            {{2026, 1, 14}},
		"build 12026-01-14 and v2026-01-140":      nil,
		"May 2026, march on, 5 mayflies, 3 marks": nil,
		"no dates here":                           nil,
		// A lowercase month name is a word, unless a year follows.
		"Option 2 may be simpler":                 nil,
		"the 3 may need review":                   nil,
		"step 1 march on":                         nil,
		"freeze on 3 march 2026 or march 9, 2026": {{2026, 3, 3}, {2026, 3, 9}},
	} {
		if got := dates(text); !reflect.DeepEqual(got, want) {
			t.Errorf("dates(%q) = %v, want %v", text, got, want)
		}
	}
	// An entry's dates are read in any case, so a date copied with a capital
	// counts.
	if got := entryDates("freeze on march 3"); !reflect.DeepEqual(got, []date{{0, 3, 3}}) {
		t.Errorf("entryDates = %v", got)
	}
	if !(date{0, 3, 3}).same(date{2026, 3, 3}) || (date{2025, 3, 3}).same(date{2026, 3, 3}) || (date{2026, 3, 4}).same(date{2026, 3, 3}) {
		t.Error("same compares the wrong dates")
	}
}
