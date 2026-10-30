package redact

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

// Tests for the baseline floor: a second key=value pair in one token is
// redacted, and no code-shape exemption keeps a password whole that the
// baseline rules redact. Fixtures that look like secrets are joined from
// fragments at run time.

// The second pair of a URL query, a form body or a connection string is
// redacted, and so is a key that follows a value written with spaces.
func TestSecondPairInOneToken(t *testing.T) {
	isolate(t)
	for _, c := range []struct{ name, in, want string }{
		{"url query",
			"redirected to https://app.example.com/cb?access_token=" + j("ya29Abc", "123Def456") + "&expires_in=3599&refresh_token=" + j("1x2y3z4w", "5v6u7t8s"),
			"redirected to https://app.example.com/cb?access_token=" + secretMark + "&expires_in=3599&refresh_token=" + secretMark},
		{"form body",
			"curl -X POST https://auth.example.com/oauth/token -d 'grant_type=password&username=alice&password=" + j("Hunter20", "24x") + "&client_id=app&client_secret=" + j("s3cr3tVal", "ue123") + "'",
			"curl -X POST https://auth.example.com/oauth/token -d 'grant_type=password&username=alice&password=" + secretMark + "&client_id=app&client_secret=" + secretMark + "'"},
		{"connection string",
			"Server=db;Password=abc;Passphrase=" + j("Secret12", "34567"),
			"Server=db;Password=abc;Passphrase=" + secretMark},
		{"after a placeholder",
			"?token=<your-token>&api_key=" + j("Secret1", "2345xyz"),
			"?token=<your-token>&api_key=" + secretMark},
		{"spaced key after a value",
			"API_KEY: " + j("9h7cQoYM", "swaGJvEdU47") + "&access_key = " + j("SWoUwt5", "Sp330"),
			"API_KEY: " + secretMark + "&access_key = " + secretMark},
		{"symbols in the second value",
			"https://x.example/cb?access_token=" + j("Ab3dEf6h", "Ij9") + "&refresh_token=" + j("x(y)zz", "zzzzzz") + "&next=/home",
			"https://x.example/cb?access_token=" + secretMark + "&refresh_token=" + secretMark + "&next=/home"},
		{"three pairs",
			"a=1&token=" + j("abcd", "efgh1") + "&secret=" + j("abcd", "efgh2") + "&api_key=" + j("abcd", "efgh3"),
			"a=1&token=" + secretMark + "&secret=" + secretMark + "&api_key=" + secretMark},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, _ := Redact(c.in)
			if out != c.want {
				t.Fatalf("Redact(%q)\n got %q\nwant %q", c.in, out, c.want)
			}
			if twice, n := Redact(out); twice != out || n != 0 {
				t.Fatalf("not idempotent: %q", twice)
			}
		})
	}
}

// Passwords that only start like code, a YAML property, a reference or a
// regular expression are redacted at least as fully as the baseline rules redact them.
func TestFloorKeepsVersion1Redactions(t *testing.T) {
	isolate(t)
	for _, c := range []struct{ in, want string }{
		{"PGPASSWORD=!" + j("Summer", "2024x") + " psql -h db -U app", "PGPASSWORD=" + secretMark + " psql -h db -U app"},
		{"DB_PASSWORD=&" + j("Tr0ub4", "dor99") + " npm run migrate", "DB_PASSWORD=" + secretMark + " npm run migrate"},
		{"export DB_PASSWORD=*" + j("Hunter", "2024x") + "; npm start", "export DB_PASSWORD=" + secretMark + "; npm start"},
		{"DB_PASSWORD=/" + j("Kx9mP2", "vL") + "/abc", "DB_PASSWORD=" + secretMark},
		{"api_key: " + j("Zx8cVb7N", "{m6Lk5Jh4,"), "api_key: " + secretMark + ","},
		{"PASSWORD=" + j("SR5RxewAw", "[zdLr;U>~9yMWdsU95]"), "PASSWORD=" + secretMark},
		{`2026-10-30T10:01:02Z INFO login ok password=` + j("Summer", "2024!") + ` {"user":"bob"}`, `2026-10-30T10:01:02Z INFO login ok password=` + secretMark + ` {"user":"bob"}`},
		{"DEBUG auth access_token=" + j("f81kQ0pL", "m3nB7vC9") + " {expires_in: 3600}", "DEBUG auth access_token=" + secretMark + " {expires_in: 3600}"},
		{"DB_PASSWORD=" + j("Summer", "2024[!]"), "DB_PASSWORD=" + secretMark},
		{"PASSWORD=" + j("Xk9mP2vL", "5nQ8{"), "PASSWORD=" + secretMark},
		{"API_KEY=" + j("Xk9mP2vL", "5nQ8wR4t[]"), "API_KEY=" + secretMark},
		{"PASSWORD=*" + j("Xk9mP2vL", "5nQ8wR4t"), "PASSWORD=" + secretMark},
		{"SECRET=/" + j("aB3+cD4", "/efgh"), "SECRET=" + secretMark},
		{"api_key=" + j("vura{scPY", "~6?JUjR{"), "api_key=" + secretMark},
		{"DB_PASSWORD=*" + j("Xdfqzj.", "q46vj}"), "DB_PASSWORD=" + secretMark},
		{"password: *" + j("Xk9mP2vL", "5nQ8wR4t"), "password: " + secretMark},
		{"token = /" + j("kocz51vh", "K?k/)Y"), "token = " + secretMark},
		{"secret: *" + j("Hmyhyb", "bz82"), "secret: " + secretMark},
	} {
		out, n := Redact(c.in)
		if out != c.want || n == 0 {
			t.Errorf("Redact(%q)\n got %q\nwant %q", c.in, out, c.want)
		}
		if twice, n := Redact(out); twice != out || n != 0 {
			t.Errorf("not idempotent: %q", twice)
		}
	}
}

// '#' starts a value unless whitespace or the line end follows it.
func TestHashValueIsNotAComment(t *testing.T) {
	isolate(t)
	for _, c := range []struct{ in, want string }{
		{"db.password = #" + j("Xk9mP2vL", "5nQ8wR4t"), "db.password = " + secretMark},
		{"password = #" + j("Xk9mP2vL", "5nQ8wR4t"), "password = " + secretMark},
		{"Password: #" + j("3tg3vZ7", "#cw~f*kq81F"), "Password: " + secretMark},
	} {
		if out, _ := Redact(c.in); out != c.want {
			t.Errorf("Redact(%q) = %q, want %q", c.in, out, c.want)
		}
	}
	for _, in := range []string{"API_KEY= # set in CI", "password: # TODO", "token: #", "secret = #\tlater"} {
		if out, n := Redact(in); out != in || n != 0 {
			t.Errorf("Redact(%q) = %q, %d; want unchanged", in, out, n)
		}
	}
}

// The current rules keep this code on purpose (found in the Go, Python and
// Ruby sources the rules were measured against); Redact removes from it only
// what the baseline rules remove.
func TestFloorKeepsCode(t *testing.T) {
	isolate(t)
	for _, in := range []string{
		"\t\t} else if !test.atEOF && tok.ScanToken == scanner.EOF {",
		"    TemplateTokenValue = T.type_alias { T.any(String, Pathname) }",
		"      t.token_to_s_table = @grammar.symbols.map {|sym| sym.to_s }",
		"        token_args = Regexp::TOKEN_KEYS.map { |k| attrs.delete(k) }",
		"        first_non_comment_token = processed_source.tokens.find { |token| !token.comment? }",
		`      secret = credentials["SecretAccessKey"]`,
		`      security_token = credentials["Token"]`,
		"    def password=(password)",
		"  def rubygems_api_key=(api_key)",
		"      github_password = git_credential_out[/^password=(.+)/, 1]",
		"\t\t\t\tPrivateKey:  bitFlippingSigner{tt.cert.PrivateKey.(crypto.Signer)},",
		"\tcliSecret := cli2.writeSecret[QUICEncryptionLevelEarly]",
		"\t\tSecret: Bug4Secret{1},",
		"TOKEN_ENDS = TSPECIALS | WSP",
		"                smtp.user, smtp.password = sim_auth[0], sim_auth[1]",
		"\t\tprivKey.PrivateKey = privKey.PrivateKey[1:]",
		"\t\t\"\\tstmt.Token = stmt.Token[1:]\",",
		"    token = /^v\\d+$/i",
		"\t\t\"    token = /^v\\d+$/i\",",
		"    token=get_token(",
		"TOKENS=(alpha beta)",
	} {
		keepsAsBaseline(t, in)
	}
}

// baselineValues returns the values the baseline rules replace in text: 8 or more
// characters after a secret-looking key, up to whitespace, a quote or one of
// , ; & < > ( ) { } [ ], unless a '(' follows or the value is a SHA, a UUID,
// an op:// reference or a variable.
var version1Re = regexp.MustCompile(`[A-Za-z0-9_.\-]*(?i:password|passwd|passphrase|secret|private[_\-]?key|token|api[_\-]?key|apikey|access[_\-]?key)` + keyTail + `["']?[ \t]*[:=][=>]?[ \t]*["']?([^\s"'\x60,;&<>(){}\[\]]{8,})`)

func baselineValues(text string) []string {
	var out []string
	for _, m := range version1Re.FindAllStringSubmatchIndex(text, -1) {
		v := text[m[2]:m[3]]
		if m[3] < len(text) && text[m[3]] == '(' || keep(v) || variableRef.MatchString(v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// No generated password the baseline rules remove survives Redact, in any of
// the line shapes passwords are pasted in.
func TestNoSecretVersion1Removed(t *testing.T) {
	isolate(t)
	r := rand.New(rand.NewSource(42))
	alpha := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	sym := "!@#$%^&*()-_=+[]{};:,.<>/?~|"
	keys := []string{"password", "DB_PASSWORD", "api_key", "API_KEY", "secret", "client_secret", "access_token", "token", "refresh_token", "db.password", "PGPASSWORD", "passphrase"}
	shapes := []string{
		"%s=%s", "export %s=%s", "%s: %s", "  %s: %s", "%s = %s", "%s=%s # rotated", "run %s=%s now", "log %s=%s {\"user\":\"bob\"}",
		"https://x.example/cb?state=abc&%s=%s&next=/home", "Server=db;%s=%s;Database=app", "curl -d 'user=bob&%s=%s'",
		"%s=%s psql -h db", "f(%s=%s)", "  \"%s\": %s,",
		"https://x.example/cb?access_token=" + j("Ab3dEf6h", "Ij9") + "&expires_in=3599&%s=%s",
		"?token=<your-token>&%s=%s",
	}
	for i := 0; i < 5000; i++ {
		var b strings.Builder
		for k := 10 + r.Intn(14); k > 0; k-- {
			if r.Intn(4) == 0 {
				b.WriteByte(sym[r.Intn(len(sym))])
			} else {
				b.WriteByte(alpha[r.Intn(len(alpha))])
			}
		}
		secret := b.String()
		shape := shapes[r.Intn(len(shapes))]
		line := strings.Replace(strings.Replace(shape, "%s", keys[r.Intn(len(keys))], 1), "%s", secret, 1)
		out, _ := Redact(line)
		for _, v := range baselineValues(line) {
			if !strings.Contains(secret, v) && !strings.Contains(v, secret) {
				continue // the value the baseline rules take is not the password
			}
			for k := 0; k+6 <= len(v); k++ {
				if piece := v[k : k+6]; strings.Contains(secret, piece) && strings.Contains(out, piece) {
					t.Fatalf("Redact(%q) = %q keeps %q, which the baseline rules remove", line, out, piece)
				}
			}
		}
	}
}

// The marker helpers find the one candidate span with a binary search; they
// answer as a scan of every span would.
func TestSpanSearchMatchesScan(t *testing.T) {
	isolate(t)
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 2000; i++ {
		var spans []span
		var refs [][]int
		for at := r.Intn(4); at < 200; at += 1 + r.Intn(12) {
			end := at + 1 + r.Intn(8)
			spans = append(spans, span{at, end})
			refs = append(refs, []int{at, end})
			at = end
		}
		s := span{r.Intn(210), 0}
		s.end = s.start + r.Intn(20)
		var contained, overlaps, inside bool
		for _, m := range spans {
			contained = contained || s.start >= m.start && s.end <= m.end
			overlaps = overlaps || s.start < m.end && m.start < s.end
		}
		at := s.start
		for _, m := range spans {
			if m.end <= at {
				continue
			}
			if m.start > at {
				break
			}
			at = m.end
		}
		inside = at >= s.end
		if containedBy(spans, s) != contained || inRef(refs, s) != contained || overlapsAny(spans, s) != overlaps || covered(spans, s) != inside {
			t.Fatalf("spans %v, s %v: contained %v/%v overlaps %v/%v covered %v/%v", spans, s, containedBy(spans, s), contained, overlapsAny(spans, s), overlaps, covered(spans, s), inside)
		}
	}
}
