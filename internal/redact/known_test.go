package redact

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// hashedID is an identifier that held a secret: a short hash of the
// original, which keeps distinct identifiers apart, then what is left.
var hashedID = regexp.MustCompile(`^[0-9a-f]{8}-.*\[REDACTED:[a-z_]+\]`)

// eva-md's own identifiers contain words such as token, secret and
// password. RedactKnown checks them with the known-format rules only.
func TestRedactKnownKeepsIdentifiers(t *testing.T) {
	isolate(t)
	for _, id := range []string{
		"local-token-refresh-worker-c9a8c7",
		"local-secret-sharing-prototype-e82b2c",
		"local-jwt-token-validator-service-c9a8c7",
		"local-password-generator-extension-e82b2c",
		"github.com__acme__k8s-secret-rotation-operator-v1",
		"feature/token-refresh-for-oauth2-clients",
		"feature/token-expiry-in-cli-v2-parser",
	} {
		if out, n := RedactKnown(id); out != id || n != 0 {
			t.Errorf("RedactKnown(%q) = %q, %d; want unchanged", id, out, n)
		}
	}
	for _, c := range []struct{ in, want string }{
		{ghTok, "[REDACTED:github_token]"},
		{"token=" + ghTok, "token=[REDACTED:github_token]"},
		{"password=" + j("Tr0ub4", "dor3x"), "password=[REDACTED:secret_value]"},
		{"x " + pemKey, "x [REDACTED:private_key]"},
	} {
		if out, n := RedactKnown(c.in); out != c.want || n == 0 {
			t.Errorf("RedactKnown(%q) = %q, %d; want %q", c.in, out, n, c.want)
		}
	}
	// The entropy rule is skipped; Redact keeps it.
	in := "the deploy token is " + random
	if out, n := RedactKnown(in); out != in || n != 0 {
		t.Errorf("RedactKnown applied the entropy rule: %q", out)
	}
	if out, _ := Redact(in); out == in {
		t.Error("Redact lost the entropy rule")
	}
}

func TestIdentifier(t *testing.T) {
	isolate(t)
	a, b := "feature/token-refresh-for-oauth2-clients", "feature/token-expiry-in-cli-v2-parser"
	if got, n := Identifier(a, 1024); got != a || n != 0 {
		t.Errorf("Identifier(%q) = %q, %d", a, got, n)
	}
	if got, _ := Identifier(b, 1024); got != b {
		t.Errorf("Identifier(%q) = %q", b, got)
	}
	// Control characters and escapes go first, so they cannot split a token.
	split := "fix/" + ghTok[:10] + "\n" + ghTok[10:20] + "\x1b[31m" + ghTok[20:] + "\x07"
	if got, n := Identifier(split, 1024); !hashedID.MatchString(got) || !strings.HasSuffix(got, "-fix/[REDACTED:github_token]") || n != 1 {
		t.Errorf("split token: %q, %d", got, n)
	}
	// The limit counts runes.
	long := strings.Repeat("界", 2000)
	if got, _ := Identifier(long, 1024); utf8.RuneCountInString(got) != 1024 || !utf8.ValidString(got) {
		t.Errorf("limit: %d runes", utf8.RuneCountInString(got))
	}
	// The cut comes after redaction, so it cannot leave the start of a secret
	// behind, and the result is stable when it is cleaned again.
	for _, s := range []string{
		strings.Repeat("a", 1000) + "/" + ghTok + "/" + ghTok,
		strings.Repeat("b/", 500) + j("gh", "p_") + strings.Repeat("Ab3d", 80),
		strings.Repeat("c", 1020) + "/" + ghTok,
		long,
		"\x00\u202e" + a + "\u200b",
	} {
		once, _ := Identifier(s, 1024)
		if utf8.RuneCountInString(once) > 1024 {
			t.Errorf("over the limit: %d runes", utf8.RuneCountInString(once))
		}
		if strings.Contains(once, ghTok[4:30]) {
			t.Errorf("a cut kept part of a token: %q", once[max(0, len(once)-60):])
		}
		if twice, n := Identifier(once, 1024); twice != once || n != 0 {
			t.Errorf("not stable: %q then %q", once, twice)
		}
	}
}

// The cut that makes room for the hash can turn code after a key into a
// value, as token=getToken(user) cut to token=getToken. Cleaning the result
// again still changes nothing.
func TestIdentifierStableAfterHashCut(t *testing.T) {
	isolate(t)
	for pad := 960; pad < 1000; pad++ {
		for _, tail := range []string{"/token=getToken(user)", "/password=abcdefgh(x)"} {
			s := strings.Repeat("a", pad) + "/" + ghTok + tail
			once, _ := Identifier(s, 1024)
			if utf8.RuneCountInString(once) > 1024 || strings.Contains(once, ghTok[4:30]) {
				t.Errorf("pad %d: %q", pad, once[max(0, len(once)-60):])
			}
			if twice, n := Identifier(once, 1024); twice != once || n != 0 {
				t.Errorf("pad %d: not stable: %q then %q", pad, once[len(once)-40:], twice[max(0, len(twice)-40):])
			}
		}
	}
}

// Provider token formats also match ordinary names, such as
// sk-learn for scikit-learn or ops_ for operations. In identifiers a match
// that reads as a name is kept, so such branches stay whole and apart, while
// real tokens are still replaced. A replaced identifier starts with a short
// hash of the original, so two of them never merge.
func TestIdentifierKeepsNamesAndSeparatesSecrets(t *testing.T) {
	isolate(t)
	for _, id := range []string{
		"feature/sk-learn-pipeline-upgrade",
		"feature/sk-learn-pipeline-refactor",
		"feature/sk-learn-v2-pipeline-for-py3",
		"feature/sk-learn-Upgrade-Docs",
		"ops_runbooks_and_scripts",
		"chore/xoxo-gossip-fan-site-redesign",
		"fix/glpat-rotation-for-ci-runners",
	} {
		if got, n := Identifier(id, 1024); got != id || n != 0 {
			t.Errorf("Identifier(%q) = %q, %d; want unchanged", id, got, n)
		}
	}
	seen := map[string]string{}
	for _, id := range []string{"fix/" + ghTok, "fix/" + oai, "fix/" + ops, "fix/" + slack, "fix/" + glTok, "fix/" + ant, "fix/" + stripe, "fix/" + ghTok + "-second"} {
		got, n := Identifier(id, 1024)
		if n == 0 || !hashedID.MatchString(got) || strings.Contains(got, id[4:12]) {
			t.Errorf("Identifier(%q) = %q, %d", id, got, n)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%q and %q merge as %q", id, other, got)
		}
		seen[got] = id
		if again, k := Identifier(got, 1024); again != got || k != 0 {
			t.Errorf("not stable: %q then %q", got, again)
		}
	}
	// The hash fits within the limit, and a value the hash could join is
	// not taken in by cleaning the result again.
	for _, id := range []string{strings.Repeat("a", 1020) + "/" + ghTok, `token"=` + j("pass", "word1"), "fix/token=" + j("abcd", "efgh9")} {
		got, _ := Identifier(id, 1024)
		if again, k := Identifier(got, 1024); again != got || k != 0 || utf8.RuneCountInString(got) > 1024 || len(id) < 100 && !hashedID.MatchString(got) {
			t.Errorf("Identifier(%q) = %q, then %q", id, got, again)
		}
	}
	// RedactKnown still replaces provider tokens in text, where the rules stay
	// strict.
	if out, _ := RedactKnown("key " + oai); strings.Contains(out, oai) {
		t.Error("RedactKnown kept a token")
	}
}
