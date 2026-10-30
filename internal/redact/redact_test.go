package redact

import (
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"
)

// Test secrets are joined from fragments at run time, so this file holds no
// string a secret scanner would flag.
func j(parts ...string) string { return strings.Join(parts, "") }

var (
	sha40   = strings.Repeat("4e2bf4c1", 5)
	uuid    = "3f2a91c0-1234-4abc-9def-0123456789ab"
	random  = "Zx8Qp2Lr7Vt4Nm1Kj6Hg3Fd9Sa5Wq0E"
	ghTok   = j("gh", "p_", strings.Repeat("aB3d", 9))
	patTok  = j("github", "_pat_", strings.Repeat("11AbC", 5), "_", strings.Repeat("xY9z", 6))
	glTok   = j("glp", "at-", "Ab3dEf6hIj9lMn2pQr5t")
	slack   = j("xo", "xb-", "1234567890-0987654321-", "AbCdEfGhIjKlMnOp")
	hook    = j("https://hooks.", "slack.com/services/", "T0A1B2C3D/B0A1B2C3D/", "aBcDeFgHiJkLmNoPqRsTuVwX")
	oai     = j("sk-", "proj-", "Ab3dEf6hIj9lMn2pQr5tUv8x")
	ant     = j("sk-", "ant-", "api03-", "Ab3dEf6hIj9lMn2pQr5tUv8x")
	goog    = j("AI", "za", "Sy", strings.Repeat("Ab3dE", 6), "f6h")
	stripe  = j("sk", "_live_", "4eC39HqLyjWDarjtT1zdp7dc")
	ops     = j("op", "s_", "eyJzaWduSW5BZGRyZXNzIjoibXkuMXBhc3N3b3JkLmNvbSJ9")
	jwt     = j("ey", "JhbGciOiJIUzI1NiJ9", ".", "ey", "JzdWIiOiIxMjM0In0", ".", "dBjftJeZ4CVPmB92K27uhbUJU1p1r")
	akid    = j("AK", "IA", "Z3Q7RT5X2LMN8PQ4")
	awsSec  = j("wJalrXUtnFEMI/K7MDENG/", "bPxRfiCYzz9x8w7v6u")
	pemKey  = j("-----BEGIN RSA ", "PRIVATE KEY-----\nMIIEowIBAAKCAQEA7\nq2w3e4r5t6y7u8i9o0p\n-----END RSA ", "PRIVATE KEY-----")
	pemOpen = j("-----BEGIN OPENSSH ", "PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n")
)

func TestPositives(t *testing.T) {
	isolate(t)
	cases := []struct {
		name, in, kind, gone string
	}{
		{"pem", "key:\n" + pemKey + "\nend", "private_key", "MIIEowIBAAKCAQEA7"},
		{"pem without end", "pasted " + pemOpen, "private_key", "b3BlbnNzaC1rZXktdjEAAAAA"},
		{"aws key id", "id " + akid + " here", "aws_key", akid},
		{"aws secret", "aws_secret_access_key = " + awsSec + "\n", "aws_key", awsSec},
		{"github", "token is " + ghTok, "github_token", ghTok},
		{"github pat", "use " + patTok + " now", "github_token", patTok},
		{"gitlab", "GL " + glTok, "gitlab_token", glTok},
		{"slack token", "bot " + slack, "slack_token", slack},
		{"slack webhook", "post to " + hook + " please", "slack_webhook", hook},
		{"openai", "OPENAI " + oai, "openai_key", oai},
		{"anthropic", "the key " + ant, "anthropic_key", ant},
		{"google", "maps " + goog, "google_key", goog},
		{"stripe", "charge with " + stripe, "stripe_key", stripe},
		{"1password", "OP_SERVICE_ACCOUNT " + ops, "onepassword_token", ops},
		{"jwt", "cookie " + jwt, "jwt", jwt},
		{"bearer header", "Authorization: Bearer " + random, "auth_header", random},
		{"basic header", "authorization: Basic " + j("dXNlcjpw", "YXNzd29yZA=="), "auth_header", "dXNlcjpw"},
		{"bare bearer", "curl -H 'X: bearer " + random + "'", "auth_header", random},
		{"url credentials", "clone https://nag:" + j("hunter2", "Pass9") + "@git.acme.dev/r.git", "url_credentials", "hunter2Pass9"},
		{"password env", "DB_PASSWORD=" + j("Tr0ub4", "dor3x"), "secret_value", "Tr0ub4dor3x"},
		{"password yaml", "password: " + j("s3cret", "Value!"), "secret_value", "s3cretValue!"},
		{"api key quoted", `api_key = "` + j("abcd1234", "efgh") + `"`, "secret_value", "abcd1234efgh"},
		{"client secret", "client_secret: " + j("q8w7e6", "r5t4"), "secret_value", "q8w7e6r5t4"},
		{"camel token", "accessToken=" + j("tok", "_9f8e7d6c"), "secret_value", "tok_9f8e7d6c"},
		{"json token", `{"token": "` + j("abc", "DEF12345") + `"}`, "secret_value", "abcDEF12345"},
		{"entropy after word", "the deploy token is " + random, "high_entropy", random},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, n := Redact(c.in)
			if n < 1 {
				t.Fatalf("count = %d, want at least 1; out %q", n, out)
			}
			if !strings.Contains(out, "[REDACTED:"+c.kind+"]") {
				t.Errorf("out %q has no %s marker", out, c.kind)
			}
			if strings.Contains(out, c.gone) {
				t.Errorf("out %q still contains the secret", out)
			}
		})
	}
}

func TestNegatives(t *testing.T) {
	isolate(t)
	cases := []string{
		"the token was rotated in commit " + sha40,
		"secret rotation landed at " + sha40 + " yesterday",
		"session token " + uuid,
		"ANTHROPIC_API_KEY=op://Vault/Example/ANTHROPIC_API_KEY",
		"password: op://Work/db/password",
		"Please rotate the password policy and update the secret manager docs.",
		"tokenizer = NewTokenizer(cfg)",
		"input_tokens = 1500 and max_tokens: 4096",
		"api_key = os.environ['OPENAI_API_KEY']",
		"password: ${DB_PASSWORD}",
		"token = getAccessToken(user)",
		"export GITHUB_TOKEN=$(gh auth token)",
		"API_KEY=<your-api-key-here>",
		"the secret model is claude-opus-5-5-20261028",
		"run go test ./internal/redact/... with the token flag",
		"see https://github.com/okf-memory/okf-agent-memory/blob/develop/pkg/okf/mutate.go",
		"clone https://github.com/heyNag/eva-md.git",
		"ssh://git@gitlab.acme.dev:2222/team/repo.git",
		"Use op run --env-file anthropic.env -- claude -p",
		"skip-tests and sk-learn are not keys",
		"func (c Config) EffortFor(host, model string) string",
		"Authorization header must be set by the client",
	}
	for _, in := range cases {
		out, n := Redact(in)
		if n != 0 || out != in {
			t.Errorf("Redact(%q) = %q, %d; want unchanged", in, out, n)
		}
	}
}

func TestPrivacyRegressions(t *testing.T) {
	isolate(t)
	for name, value := range map[string]string{
		"weak password":                 "password=" + j("change", "me"),
		"numeric token":                 "token=" + j("1234", "5678"),
		"dotted password":               "password=" + j("Abcd.", "Efgh1234"),
		"punctuation password":          "password=" + j("@abc", "defghi"),
		"example substring":             "secret=" + j("real-example-", "secret-value"),
		"quoted spaces":                 `password="` + j("one two ", "three four") + `"`,
		"quoted literal":                `password="` + j("pass", "word") + `"`,
		"escaped quote":                 `password="` + j(`abc\"`, "defghijk") + `"`,
		"short bearer":                  "Authorization: Bearer " + j("ab", "cd"),
		"letter-only key":               j("sk-", strings.Repeat("aBcD", 8)),
		"letter-only service":           j("op", "s_", strings.Repeat("aBcD", 8)),
		"entropy without digits":        "the secret is " + j("aZbYcXdWeVfUgT", "hSiRjQkPlOmN"),
		"private key containing marker": strings.Replace(pemKey, "MIIE", "[REDACTED:secret_value]MIIE", 1),
		"JWT JSON whitespace":           base64.RawURLEncoding.EncodeToString([]byte(`{ "alg": "HS256" }`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(`{ "sub": "fixture" }`)) + "." + j("aBcD", "eFgH"),
	} {
		t.Run(name, func(t *testing.T) {
			out, count := Redact(value)
			if count == 0 || out == value {
				t.Fatalf("did not redact %s", name)
			}
			if strings.Contains(out, "three four") || strings.Contains(out, "MIIE") {
				t.Fatalf("partial secret remains for %s", name)
			}
			if twice, n := Redact(out); twice != out || n != 0 {
				t.Fatalf("not idempotent for %s: %q", name, twice)
			}
		})
	}
	for _, input := range []string{"token=op://Vault/" + ghTok + "/credential"} {
		if output, n := Redact(input); output != input || n != 0 {
			t.Fatalf("protected value changed: %q", output)
		}
	}
	for _, control := range []string{"\u200b", "\u200c", "\u200d", "\u2060", "\ufeff", "\u2063", "\u00ad", "\u034f", "\x1b[38:5:1m", "\x1b[3\u200b1m"} {
		input := ghTok[:12] + control + ghTok[12:]
		if output, n := Redact(input); output != "[REDACTED:github_token]" || n != 1 {
			t.Fatalf("split token leaked for control %q", control)
		}
	}
}

// A SHA or UUID is kept in ordinary text but redacted as the value of a
// secret-looking key, where it may well be the secret.
func TestSecretKeyKeepsNoSHAOrUUID(t *testing.T) {
	isolate(t)
	for _, v := range []string{uuid, strings.ToUpper(uuid), sha40, strings.ToUpper(sha40)} {
		for _, in := range []string{
			"password=" + v,
			"GITHUB_TOKEN=" + v,
			"api_key: " + v,
			`{"client_secret": "` + v + `"}`,
			"export ACCESS_KEY='" + v + "'",
			"connect with token=" + v + " now",
			"https://user:" + v + "@example.dev/x",
			"Authorization: Bearer " + v,
		} {
			out, n := Redact(in)
			if n == 0 || strings.Contains(out, v) {
				t.Errorf("Redact(%q) = %q, %d", in, out, n)
			}
			if again, k := Redact(out); again != out || k != 0 {
				t.Errorf("not idempotent: %q then %q", out, again)
			}
		}
	}
	for _, in := range []string{
		"the token refresh landed in " + sha40,
		"password reset for request " + uuid,
		"run " + uuid + " used the secret store",
	} {
		if out, n := Redact(in); out != in || n != 0 {
			t.Errorf("Redact(%q) = %q, %d; want unchanged", in, out, n)
		}
	}
	// A bearer token is not a key's value, so it leaves a passphrase line
	// whole for the rule that takes all of it.
	for _, in := range []string{
		"PASSWORD=correct horse battery Bearer " + uuid + " staple",
		"passphrase: correct horse bearer " + sha40 + " battery staple",
	} {
		if out, _ := Redact(in); strings.Contains(out, "horse") || strings.Contains(out, "staple") {
			t.Errorf("Redact(%q) = %q; want the whole value taken", in, out)
		}
	}
}

func TestCount(t *testing.T) {
	isolate(t)
	in := "a " + ghTok + " b " + glTok + " c " + akid
	out, n := Redact(in)
	if n != 3 {
		t.Fatalf("count = %d, want 3; out %q", n, out)
	}
}

func TestIdempotent(t *testing.T) {
	isolate(t)
	inputs := []string{
		pemKey, "x " + ghTok, "Authorization: Bearer " + random, "password: " + j("s3cret", "Value!"),
		"the token is " + random, "url https://u:" + j("p4ss", "word99") + "@h.dev/x", jwt + " " + ops,
		"token: [REDACTED:secret_value] and " + random,
		"secret [REDACTED:not a marker " + random + "]",
	}
	for _, in := range inputs {
		once, _ := Redact(in)
		twice, n := Redact(once)
		if twice != once || n != 0 {
			t.Errorf("second Redact changed %q to %q (count %d)", once, twice, n)
		}
	}
}

func TestCleanAndSplitSecrets(t *testing.T) {
	isolate(t)
	zw := ghTok[:10] + "\u200b" + ghTok[10:20] + "\u2060" + ghTok[20:]
	out, n := Redact("tok " + zw)
	if n != 1 || !strings.Contains(out, "[REDACTED:github_token]") {
		t.Errorf("zero-width split: %q, %d", out, n)
	}
	ansi := akid[:8] + "\x1b[31m" + akid[8:] + "\x1b[0m"
	out, n = Redact("id " + ansi)
	if n != 1 || !strings.Contains(out, "[REDACTED:aws_key]") || strings.Contains(out, "\x1b") {
		t.Errorf("ANSI split: %q, %d", out, n)
	}
	out, _ = Redact("a\x00b\u202ec\u2066d\ufeffe")
	if out != "abcde" {
		t.Errorf("controls not stripped: %q", out)
	}
}

func TestNoPanicOnRandomInput(t *testing.T) {
	isolate(t)
	r := rand.New(rand.NewSource(1))
	alphabet := []string{"token", "=", ":", " ", "password", "[REDACTED:", "]", "\x1b[", "\xff", "sk-", "gh", "p_", "Ab3", "\u200b", "://", "@", "-----BEGIN ", "PRIVATE KEY-----", "\n"}
	for i := 0; i < 2000; i++ {
		var b strings.Builder
		for k := r.Intn(40); k > 0; k-- {
			b.WriteString(alphabet[r.Intn(len(alphabet))])
			if r.Intn(3) == 0 {
				b.WriteString(random[:r.Intn(len(random))])
			}
		}
		once, _ := Redact(b.String())
		if twice, n := Redact(once); twice != once || n != 0 {
			t.Fatalf("not idempotent on %q: %q then %q", b.String(), once, twice)
		}
	}
}
