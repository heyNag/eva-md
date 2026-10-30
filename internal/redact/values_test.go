package redact

import (
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/redact/baseline"
)

const secretMark = "[REDACTED:secret_value]"

// keepsAsBaseline checks that Redact removes no more from in than the
// baseline rules do: the current rules keep these lines on purpose, and the
// baseline rules still remove what they remove from them.
func keepsAsBaseline(t *testing.T, in string) {
	t.Helper()
	want, _ := baseline.Redact(in)
	if out, _ := Redact(in); out != want {
		t.Errorf("Redact(%q) = %q; want the baseline rules' %q", in, out, want)
	}
}

// A key=value or key: value secret is replaced whole, even when it holds
// & ; , ( ) < > { } [ ].
func TestValuesWithSymbols(t *testing.T) {
	isolate(t)
	for _, c := range []struct{ name, in, want string }{
		// The examples from the review.
		{"yaml ampersand", "password: " + j("a&bcdefg", "hijklmnop"), "password: " + secretMark},
		{"export paren", "export PGPASSWORD=" + j("Xy7(kLm9", "qR2zT5"), "export PGPASSWORD=" + secretMark},
		{"paren after 8", "password=" + j("Abcdefgh", "(1234zz"), "password=" + secretMark},
		{"semicolon", "secret: " + j("q8;w7e6r", "5t4y3u2"), "secret: " + secretMark},
		{"short first part", "password: " + j("hunter2;", "rest-of-the-secret"), "password: " + secretMark},
		{"partial before", "DB_PASSWORD=" + j("xK9#mP2$", "vL5&nQ8!wR4t"), "DB_PASSWORD=" + secretMark},
		// Every symbol that could end a value early.
		{"all symbols", "password=" + j("ab,cd;ef&gh<ij>", "kl(mn)op{qr}st[uv]wx"), "password=" + secretMark},
		{"symbol first", "api_key: " + j("&k3y(v4l", "ue)xyz"), "api_key: " + secretMark},
		// Config lines: indentation, a quoted key, a passphrase with spaces, a
		// trailing comment, a trailing semicolon and CRLF line ends.
		{"passphrase and comment", "  db_password: " + j("correct horse ", "battery staple") + "  # rotated", "  db_password: " + secretMark + "  # rotated"},
		{"quoted key", `"api_key": ` + j("k3y&v4", "lue)x"), `"api_key": ` + secretMark},
		{"ini crlf", "[db]\r\npassword = " + j("p4ss;", "w0rd!") + "\r\nhost = db", "[db]\r\npassword = " + secretMark + "\r\nhost = db"},
		{"properties", "spring.datasource.password=" + j("Pa(55", "word)99"), "spring.datasource.password=" + secretMark},
		{"hash inside value", "token=" + j("abc#def", "ghi123"), "token=" + secretMark},
		{"two lines", "user: bob\nsecret: " + j("q8;w7e6r", "5t4y3u2") + "\nport: 5432", "user: bob\nsecret: " + secretMark + "\nport: 5432"},
		// Inside a line, a value runs to the next whitespace or quote.
		{"inline ampersand", "then set password=" + j("a&bcdefg", "hijklmnop") + " and retry", "then set password=" + secretMark + " and retry"},
		{"inline partial", "use DB_PASSWORD=" + j("xK9#mP2$", "vL5&nQ8!wR4t") + " for now", "use DB_PASSWORD=" + secretMark + " for now"},
		{"inline paren", "run with password=" + j("Abcdefgh", "(1234zz") + " (staging)", "run with password=" + secretMark + " (staging)"},
		{"inline quoted", `curl -d 'token=` + j("ab;cd", "efgh12") + `'`, `curl -d 'token=` + secretMark + `'`},
		// It stops before the next key=value pair, as in a URL query or a
		// connection string, and leaves punctuation that ends code.
		{"url query", "open https://app.example/cb?state=xyz&token=" + j("abcd", "efgh1234") + "&next=/home", "open https://app.example/cb?state=xyz&token=" + secretMark + "&next=/home"},
		{"connection string", "Server=db;Password=" + j("Xy7(k", "Lm9q") + ";Database=app", "Server=db;Password=" + secretMark + ";Database=app"},
		{"call argument", "connect(password=" + j("abcd", "efgh1234") + ")", "connect(password=" + secretMark + ")"},
		{"string literal", `cmd := "export PGPASSWORD=` + j("Xy7(kLm9", "qR2zT5") + `"`, `cmd := "export PGPASSWORD=` + secretMark + `"`},
		{"literal line", "\t\t\"DB_PASSWORD=" + j("xK9#mP2$", "vL5&nQ8!wR4t") + "\",", "\t\t\"DB_PASSWORD=" + secretMark + "\","},
		{"object literal", "{token: " + j("abcd", "efgh1234") + ", user: bob}", "{token: " + secretMark + ", user: bob}"},
		// A type annotation is skipped and the assigned literal judged instead.
		{"annotated literal", "    jwt_secret: str = " + `"` + j("dev-secret-", "change-me") + `"`, "    jwt_secret: str = " + `"` + secretMark + `"`},
		// The baseline rules take refresh_token for the value; the union keeps that.
		{"chained assignment", "token = refresh_token = " + j("abcd", "efgh1234"), "token = " + secretMark + " = " + secretMark},
		// Generated passwords that only resemble code, a type or a URL query.
		{"pipe", "password: " + j("hunter|", "Secret99"), "password: " + secretMark},
		{"pipe no spaces", "password: " + j("xK9|mP2", "vL5"), "password: " + secretMark},
		{"call then more", "password: " + j("Ab3(x9)", "k&Lm"), "password: " + secretMark},
		{"call then symbol", "password=" + j("Ab3(x9)", "#qR"), "password=" + secretMark},
		{"inline pair inside", "use DB_PASSWORD=" + j("xK9&mP2", "=vL5qR") + " for now", "use DB_PASSWORD=" + secretMark + " for now"},
		{"inline semicolon pair", "run PGPASSWORD=" + j("a;b=cdef", "ghij") + " psql", "run PGPASSWORD=" + secretMark + " psql"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, n := Redact(c.in)
			if out != c.want || n == 0 {
				t.Fatalf("Redact(%q)\n got %q (%d)\nwant %q", c.in, out, n, c.want)
			}
			if twice, n := Redact(out); twice != out || n != 0 {
				t.Fatalf("not idempotent: %q", twice)
			}
		})
	}
}

// Code, references and placeholders after a secret-looking key are kept.
func TestValueCodeShapesKept(t *testing.T) {
	isolate(t)
	for _, in := range []string{
		"password = getPassword()",
		"token: ${TOKEN}",
		`secret = os.environ["X"]`,
		"I reset my password yesterday; the token expired (again), and the secret stays in 1Password.",
		"token = process.env.GITHUB_TOKEN;",
		"password: {{ .Values.db.password }}",
		"token: ${{ secrets.GITHUB_TOKEN }}",
		"TOKEN=$(cat ~/.config/token)",
		"API_KEY= # set in CI",
		"password = await getPassword()",
		"secret_key = secrets.token_hex(32)",
		"if token == getToken(user, ctx):",
		`api_key = config["api_key"]`,
		"client = Client(token=get_token(), timeout=5)",
		"token = response.json()['access_token']",
		"    token: str | None = None",
		"  accessToken: string | null;",
		`    api_key: Optional[str] = os.getenv("API_KEY")`,
		`    db_password: SecretStr = SecretStr("")`,
		"password_hash = bcrypt.hashpw(pw, bcrypt.gensalt())",
		"secret_key_base: <%= ENV['SECRET_KEY_BASE'] %>",
		"API_KEY=<your api key here>",
		// The same shapes inside string literals in code.
		"\t\t\"api_key = os.environ['OPENAI_API_KEY']\",",
		"\t\t\"password = getPassword()\",",
		"\t\t\"if token == getToken(user, ctx):\",",
		"\t\t\"token: ${{ secrets.GITHUB_TOKEN }}\",",
		"\t\t\"password_hash = bcrypt.hashpw(pw, bcrypt.gensalt())\",",
		"log.Print(\"token=\" + getToken())",
	} {
		keepsAsBaseline(t, in)
	}
}

// Code lines that start with a secret-looking name are not config lines:
// expressions, literals and calls that go on to the next line are kept as
// before, rather than erased to the end of the line.
func TestCodeLinesKept(t *testing.T) {
	isolate(t)
	for _, in := range []string{
		"\tmaxScanTokenSize = 64 * 1024",
		"\tTOKEN_FLAG_A = 1 << iota",
		"\t\ttokenFile := dir + \"suffix\"",
		"\t\tcfg.Token = nil // reset below",
		"\tsecretLength   = 48 // bytes in a master secret",
		"\t\"Decoder.ReadToken\":  true, // thin wrapper",
		"\ttoken = token or ''",
		"\t\ts.masterSecret = deriveMaster(c.vers, s.suite, preMasterSecret,",
		"\ttrafficSecret := parseVector(",
		"var testPrivateKey = loadKey(`",
		"\tPrivateKey:  &testKey,",
		"var isTokenTable = [256]bool{",
		"var tokenTests = []struct {",
		"\t\tq.readSecret = map[Level]suiteSecret{}",
		"var privateKey = &Block{Type: \"TEST KEY\",",
		"\t\twantTokens: []string{\"a\", \"b\"},",
		"\tPrivateKey: wrappedSigner{key},",
		"\tstmt.Token = stmt.Token[1:]",
		"\tdec.tokenState = dec.tokenStack[len(dec.tokenStack)-1]",
		"  password: *default_password",
		"API_TOKEN=[1, 2, 3]",
		// Python and Ruby.
		"\t_token_re = re.compile(r\"\"\"",
		"                 password=None, timeout=None):",
		"    uri.password = nil if uri.password",
		"    token = get :text",
		"  TOKEN_RE = %r{[^\\s()<>@,;:\"/\\[\\]?={}]+}n",
		"    token = /(?:\"[^\"]*\"|'[^']*')/",
		"    sig { params(token: String).returns(T::Boolean) }",
		"    puts \"Proposed token: #{token}\"",
		"      next_token = lexed[index]&.first",
		"    cask_token = @symlink_tokens[dst]",
		"    GITHUB_TOKEN_SET = ENV.key?(\"GITHUB_TOKEN\")",
		"    token = contents.adjust(begin_pos: seq.begin_pos,",
		`  url = "https://api.example/q?symbol=#{symbol}&apikey=#{api_key}&format=csv"`,
		"    url.sub!(%r{^https://}, \"https://x-access-token:@github.com/\")",
		// Prose after a key at the start of a line.
		"Password: still not working after the reset",
	} {
		keepsAsBaseline(t, in)
	}
	// When an earlier pass redacts a name inside a call, the call is still
	// code: the rest of the line is not swallowed into the value.
	in := "    token = ::Utils.name_from_full_name(token)"
	if out, _ := Redact(in); !strings.HasSuffix(out, "(token)") || strings.Contains(out, secretMark) {
		t.Errorf("Redact(%q) = %q", in, out)
	}
	// A password written as a Python byte string is a value, not code.
	in = "    tls_password = b'" + j("some", "pass1") + "'"
	if out, _ := Redact(in); out != "    tls_password = "+secretMark {
		t.Errorf("Redact(%q) = %q", in, out)
	}
	// Prose keeps all but its first word, which is judged as before.
	in = "Token: refreshed by the SDK when it expires"
	if out, _ := Redact(in); out != "Token: "+secretMark+" by the SDK when it expires" {
		t.Errorf("Redact(%q) = %q", in, out)
	}
	// A YAML anchor before a value does not hide it, and a passphrase is not
	// an expression.
	for _, c := range []struct{ in, want string }{
		{"  password: &pw " + j("hunter22", "secret"), "  password: &pw " + secretMark},
		{"  password: &pw " + j("hunter2;", "rest more"), "  password: &pw " + secretMark + " more"},
		{"  token: !!str " + j("abcd", "efgh1234"), "  token: !!str " + secretMark},
		{"PASSWORD=" + j("correct horse ", "battery staple"), "PASSWORD=" + secretMark},
		{"password = " + j("Abcd", "efgh") + " (rotated)", "password = " + secretMark + " (rotated)"},
	} {
		if out, _ := Redact(c.in); out != c.want {
			t.Errorf("Redact(%q) = %q, want %q", c.in, out, c.want)
		}
	}
}

// A redacted value stays one marker: punctuation after a marker is not taken
// for more of the value, and markers are not merged or relabelled.
func TestValueMarkersAreOpaque(t *testing.T) {
	isolate(t)
	for _, in := range []string{
		"token=[REDACTED:secret_value];",
		"password: [REDACTED:secret_value] # rotated",
		"f(token=[REDACTED:github_token]), g(x)",
		"secret: [REDACTED:secret_value][REDACTED:aws_key]",
		// The start of a marker, as left when redacted text is cut to fit.
		"Bash x token=[REDACTED:openai_k (exit 0)",
		"password: [REDACTED:secr",
		"token=[REDACTE",
	} {
		keepsAsBaseline(t, in)
	}
	// A known token inside a longer value still takes the whole value, and a
	// marker's start shields nothing that follows it.
	for _, in := range []string{
		"DB_PASSWORD=" + ghTok + j("&nQ8", "!wR4t"),
		"password: [REDACTED:" + j("hunter2", "Pass9"),
		"password: [REDACTED:secr" + j("&nQ8", "!wR4t"),
	} {
		out, _ := Redact(in)
		if want := in[:strings.IndexAny(in, ":=")+1]; !strings.HasPrefix(out, want) || !strings.HasSuffix(out, secretMark) || strings.Contains(out, "wR4t") || strings.Contains(out, "Pass9") {
			t.Errorf("Redact(%q) = %q", in, out)
		}
	}
}

// Idempotence holds for random mixes of keys, separators and the symbols
// that now continue a value.
func TestValuesIdempotentOnRandomInput(t *testing.T) {
	isolate(t)
	alphabet := []string{"password", "token", "api_key", "export ", "=", ": ", " ", "\n", "\r\n", "  ", " # ", "#", " // ", "&", ";", ",", "(", ")", "[", "]", "{", "}", "<", ">", "$", "${", "#{", "{{", "}}", "$(", `"`, "'", "`", `"""`, "[REDACTED:secret_value]", "[REDACTED:", "getToken", "os.environ", "str = ", " | ", "Ab3", "x9", "?", "state=", ";Database=", "://", "@", "::", "&.", "%r{", "/", "nil ", "!!str ", "&pw ", "do |x|", "the ", "correct horse ", "1 << ", "[]byte{", "await ", "\\"}
	seed := uint32(7)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	for i := 0; i < 3000; i++ {
		var b strings.Builder
		for k := next(24); k > 0; k-- {
			b.WriteString(alphabet[next(len(alphabet))])
		}
		once, _ := Redact(b.String())
		if twice, n := Redact(once); twice != once || n != 0 {
			t.Fatalf("not idempotent on %q: %q then %q", b.String(), once, twice)
		}
	}
}
