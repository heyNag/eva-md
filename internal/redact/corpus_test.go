package redact

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/heyNag/eva-md/internal/redact/baseline"
)

// The labelled corpus in testdata/corpus, whose format its README states,
// and the seeded generators that plant made-up secrets in carrier texts. The
// rates test and TestOracle read both. No file and no line of this file
// holds a whole secret: the corpus joins each from parts as it is read, and
// the generators make each from a fixed seed.

// corpusDir is the folder of the labelled corpus.
const corpusDir = "testdata/corpus"

// The labels a case can carry.
const (
	labelSecret   = "secret"
	labelNegative = "negative"
	labelFact     = "fact"
)

// The marks of a case's text (see the corpus README).
const (
	secretOpen  = "'⟦"
	secretClose = "⟧'"
	factOpen    = "⟪"
	factClose   = "⟫"
	split       = "¦"
)

// corpusCase is one labelled text: a case of the corpus, or one a generator
// made.
type corpusCase struct {
	file, label, class, name string
	text                     string
	secrets, facts           []textRange // ranges of text
}

// textRange is a range of a case's text.
type textRange struct{ start, end int }

func (c corpusCase) value(r textRange) string { return c.text[r.start:r.end] }

func (c corpusCase) String() string {
	return fmt.Sprintf("%s: %s %s %q: %q", c.file, c.label, c.class, c.name, c.text)
}

// loadCorpus reads every case of the corpus files, in the order of the
// files' names and of the cases in each.
func loadCorpus(t testing.TB) []corpusCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(corpusDir, "*.txt"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus files in %s: %v", corpusDir, err)
	}
	sort.Strings(files)
	var all []corpusCase
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		cases, err := parseCorpus(filepath.Base(f), string(data))
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, cases...)
	}
	return all
}

// parseCorpus reads the cases of one corpus file and checks each against
// the rules of the README.
func parseCorpus(file, data string) ([]corpusCase, error) {
	var cases []corpusCase
	var head string
	var body []string
	headLine := 0
	flush := func() error {
		if head == "" {
			return nil
		}
		for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
			body = body[:len(body)-1]
		}
		c, err := parseCase(file, head, strings.Join(body, "\n"))
		if err != nil {
			return fmt.Errorf("%s:%d: %v", file, headLine, err)
		}
		cases = append(cases, c)
		return nil
	}
	for i, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "=== ") {
			if err := flush(); err != nil {
				return nil, err
			}
			head, body, headLine = line, nil, i+1
			continue
		}
		if head != "" {
			body = append(body, line)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s: no cases", file)
	}
	return cases, nil
}

// parseCase reads one case from its header line and its body, with its
// marks.
func parseCase(file, head, body string) (corpusCase, error) {
	f := strings.SplitN(strings.TrimPrefix(head, "=== "), " ", 3)
	if len(f) != 3 || strings.TrimSpace(f[2]) == "" {
		return corpusCase{}, fmt.Errorf("header %q: want === <label> <class> <name>", head)
	}
	c := corpusCase{file: file, label: f[0], class: f[1], name: f[2]}
	if c.label != labelSecret && c.label != labelNegative && c.label != labelFact {
		return c, fmt.Errorf("label %q is not secret, negative or fact", c.label)
	}
	if c.class == "" || strings.Trim(c.class, "abcdefghijklmnopqrstuvwxyz_0123456789") != "" {
		return c, fmt.Errorf("class %q is not one word", c.class)
	}
	var b strings.Builder
	// splitAt is where, in the text, the open secret was first split with
	// text of it before the split; -1 when it has not been. A secret counts
	// as split only when it also goes on after that split: a split at either
	// edge leaves the raw file holding it whole.
	in, start, splitAt := "", 0, -1
	for i := 0; i < len(body); {
		rest := body[i:]
		switch {
		case strings.HasPrefix(rest, secretOpen), strings.HasPrefix(rest, factOpen):
			if in != "" {
				return c, fmt.Errorf("a mark inside a mark")
			}
			in, start, splitAt = secretOpen, b.Len(), -1
			i += len(secretOpen)
			if strings.HasPrefix(rest, factOpen) {
				in = factOpen
				i += len(factOpen) - len(secretOpen)
			}
		case strings.HasPrefix(rest, secretClose):
			if in != secretOpen {
				return c, fmt.Errorf("a secret closed that was not open")
			}
			if splitAt < 0 || splitAt == b.Len() {
				return c, fmt.Errorf("secret %q is not split with text on both sides of a split", b.String()[start:])
			}
			c.secrets = append(c.secrets, textRange{start, b.Len()})
			in = ""
			i += len(secretClose)
		case strings.HasPrefix(rest, factClose):
			if in != factOpen {
				return c, fmt.Errorf("a fact closed that was not open")
			}
			c.facts = append(c.facts, textRange{start, b.Len()})
			in = ""
			i += len(factClose)
		case strings.HasPrefix(rest, split):
			if in == secretOpen && splitAt < 0 && b.Len() > start {
				splitAt = b.Len()
			}
			i += len(split)
		case strings.HasPrefix(rest, "⟦"), strings.HasPrefix(rest, "⟧"):
			return c, fmt.Errorf("a stray secret mark")
		default:
			b.WriteByte(body[i])
			i++
		}
	}
	if in != "" {
		return c, fmt.Errorf("a mark left open")
	}
	c.text = b.String()
	switch {
	case c.label == labelSecret && (len(c.secrets) == 0 || len(c.facts) > 0):
		return c, fmt.Errorf("a secret case holds a secret and no fact")
	case c.label == labelFact && (len(c.facts) == 0 || len(c.secrets) > 0):
		return c, fmt.Errorf("a fact case holds a fact and no secret")
	case c.label == labelNegative && len(c.secrets)+len(c.facts) > 0:
		return c, fmt.Errorf("a negative case holds no secret and no fact")
	}
	return c, checkCase(c)
}

// checkCase checks what the README asks of a case's text and secrets: the
// rules read the text as it is, and each secret is printable ASCII, which
// may run over lines, at least
// 6 characters long, and shares no 6 characters with the rest of the text or
// with a marker, so a 6-character piece of it in a redacted text is the
// secret kept.
func checkCase(c corpusCase) error {
	if clean(c.text) != c.text {
		return fmt.Errorf("%q: the rules would clean the text first", c.name)
	}
	if c.text == "" {
		return fmt.Errorf("%q: no text", c.name)
	}
	for _, s := range c.secrets {
		v := c.value(s)
		if len(v) < 6 || strings.Trim(v, printableASCII+"\n") != "" {
			return fmt.Errorf("%q: secret %q is not 6 or more printable ASCII characters and newlines", c.name, v)
		}
		rest := c.text[:s.start] + "\x00" + c.text[s.end:]
		for i := 0; i+6 <= len(v); i++ {
			if p := v[i : i+6]; strings.Contains(rest, p) || strings.Contains(markerTexts, p) {
				return fmt.Errorf("%q: secret %q shares %q with the rest of the text or a marker", c.name, v, p)
			}
		}
	}
	return nil
}

const printableASCII = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"

// markerTexts holds every marker the rules write.
var markerTexts = func() string {
	kinds := []string{"high_entropy", "private_key"}
	for _, r := range rules {
		kinds = append(kinds, r.kind)
	}
	var b strings.Builder
	for _, k := range kinds {
		b.WriteString(marker + k + "]\x00")
	}
	return b.String()
}()

// keeps reports whether out holds secret: any 6 consecutive characters of
// it.
func keeps(out, secret string) bool {
	for i := 0; i+6 <= len(secret); i++ {
		if strings.Contains(out, secret[i:i+6]) {
			return true
		}
	}
	return len(secret) < 6 && strings.Contains(out, secret)
}

// The generators. Each makes cases of one class from its own fixed seed, so
// a class's cases do not change when another's count does.

// The number of cases each generator makes, at least 20,000 in all; under
// the race detector, a twentieth.
var genCounts = map[string]int{
	"gen_provider": 5000,
	"gen_random":   6000,
	"gen_password": 6000,
	"gen_hex32":    3000,
	// The shapes of oracle_gaps.txt, one generator each (see oracleGapShapes).
	"gen_oracle_gap_regex_value":     1000,
	"gen_oracle_gap_yaml_alias":      1000,
	"gen_oracle_gap_name_index":      1000,
	"gen_oracle_gap_type_annotation": 1000,
	// Secrets just outside each of those shapes, held to the baseline (see
	// oracleNearShapes).
	"gen_oracle_near_regex_value":     4000,
	"gen_oracle_near_yaml_alias":      4000,
	"gen_oracle_near_name_index":      4000,
	"gen_oracle_near_type_annotation": 4000,
}

func genCount(class string) int {
	if raceOn {
		return genCounts[class] / 20
	}
	return genCounts[class]
}

// Character sets.
const (
	alnum     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	upperNum  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	hexLower  = "0123456789abcdef"
	base32Set = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	base64Set = alnum + "+/"
	urlSafe   = alnum + "_-"
	digits    = "0123456789"
)

// gen is a seeded source of made-up secrets.
type gen struct{ r *rand.Rand }

func newGen(seed int64) *gen { return &gen{rand.New(rand.NewSource(seed))} }

// chars returns n characters of set.
func (g *gen) chars(n int, set string) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = set[g.r.Intn(len(set))]
	}
	return string(b)
}

// mixed returns n characters of alnum with a capital, a small letter and a
// digit, as generated tokens have.
func (g *gen) mixed(n int) string {
	for {
		s := g.chars(n, alnum)
		if hasUpper(s) && hasLower(s) && hasDigit(s) {
			return s
		}
	}
}

func (g *gen) pick(from []string) string { return from[g.r.Intn(len(from))] }

// tokenFormats make a token of each provider format the current rules know.
// Where the rules take a URL whose part is the credential, around holds the
// rest of the URL, with %s where the credential goes.
var tokenFormats = []struct {
	name   string
	make   func(g *gen) string
	around string
}{
	{"anthropic", func(g *gen) string { return "sk-" + "ant-api03-" + g.chars(40+g.r.Intn(56), urlSafe) }, ""},
	{"openai project", func(g *gen) string { return "sk-" + "proj-" + g.mixed(48+g.r.Intn(80)) }, ""},
	{"openai", func(g *gen) string { return "sk-" + g.mixed(48) }, ""},
	{"openai service account", func(g *gen) string { return "sk-" + "svcacct-" + g.mixed(40+g.r.Intn(40)) }, ""},
	{"1password", func(g *gen) string { return "op" + "s_" + g.chars(40+g.r.Intn(400), urlSafe) }, ""},
	{"github", func(g *gen) string { return githubToken(g, g.pick([]string{"ghp", "gho", "ghu", "ghs", "ghr"})) }, ""},
	// The prefix is no part of the secret: a 6-character piece of it is in
	// the marker [REDACTED:github_token].
	{"github fine-grained", func(g *gen) string { return g.mixed(22) + "_" + g.mixed(59) }, "github" + "_pat_%s"},
	{"gitlab", func(g *gen) string {
		return g.pick([]string{"glpat", "gldt", "glrt", "glptt", "gloas"}) + "-" + g.chars(20+g.r.Intn(20), urlSafe)
	}, ""},
	{"slack token", func(g *gen) string {
		return "xo" + g.pick([]string{"xb", "xp", "xa", "xo", "xs", "xr", "xe"}) + "-" + g.chars(11, digits) + "-" + g.chars(12, digits) + "-" + g.mixed(24)
	}, ""},
	{"slack app token", func(g *gen) string {
		return "xa" + "pp-1-A0" + g.chars(9, upperNum) + "-" + g.chars(13, digits) + "-" + g.chars(64, hexLower)
	}, ""},
	{"slack webhook", func(g *gen) string {
		return "T" + g.chars(8, upperNum) + "/B" + g.chars(8, upperNum) + "/" + g.mixed(24)
	}, "https://hooks." + "slack.com/services/%s"},
	{"telegram bot", func(g *gen) string {
		return g.chars(1, "123456789") + g.chars(8+g.r.Intn(2), digits) + ":" + g.chars(35, urlSafe)
	}, "https://api." + "telegram.org/bot%s/getMe"},
	{"discord webhook", func(g *gen) string { return g.chars(68, urlSafe) }, "https://discord.com/api/" + "webhooks/1234567890123456789/%s"},
	{"aws key id", func(g *gen) string { return g.pick([]string{"AK", "AS"}) + "IA" + g.chars(16, upperNum) }, ""},
	{"google", func(g *gen) string { return "AI" + "za" + g.chars(35, urlSafe) }, ""},
	{"stripe", func(g *gen) string {
		return g.pick([]string{"sk", "rk"}) + g.pick([]string{"_live_", "_test_"}) + g.mixed(24+g.r.Intn(75))
	}, ""},
	{"huggingface", func(g *gen) string { return "hf" + "_" + g.mixed(34) }, ""},
	{"npm", func(g *gen) string { return "npm" + "_" + g.mixed(36) }, ""},
	{"pypi", func(g *gen) string { return "pypi-" + "AgE" + g.chars(60+g.r.Intn(100), urlSafe) }, ""},
	{"vault", func(g *gen) string { return "hv" + g.pick([]string{"s.", "b."}) + g.chars(90+g.r.Intn(40), urlSafe) }, ""},
	{"digitalocean", func(g *gen) string { return "do" + g.pick([]string{"o", "p", "r"}) + "_v1_" + g.chars(64, hexLower) }, ""},
	{"sendgrid", func(g *gen) string { return "S" + "G." + g.chars(22, urlSafe) + "." + g.chars(43, urlSafe) }, ""},
	{"shopify", func(g *gen) string {
		return "shp" + g.pick([]string{"at", "ca", "pa", "ss"}) + "_" + g.chars(32, hexLower)
	}, ""},
	{"docker", func(g *gen) string { return "dckr" + "_pat_" + g.chars(27+g.r.Intn(10), urlSafe) }, ""},
	{"jwt", func(g *gen) string { return genJWT(g) }, ""},
}

// githubToken makes a token of GitHub's format whose last six characters
// are not the checksum GitHub gives a token, so no made-up token can be one
// GitHub issued.
func githubToken(g *gen, prefix string) string {
	for {
		body := g.mixed(30)
		sum := g.mixed(6)
		if !githubChecksum(prefix, body, sum) {
			return prefix + "_" + body + sum
		}
	}
}

// githubChecksum reports whether sum is the CRC32 checksum, in base 62,
// GitHub writes after a token's 30 random characters, of those characters
// or of the prefix and them.
func githubChecksum(prefix, body, sum string) bool {
	for _, s := range []string{body, prefix + "_" + body} {
		if base62(crc32.ChecksumIEEE([]byte(s)), 6) == sum {
			return true
		}
	}
	return false
}

func base62(v uint32, width int) string {
	const set = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		b[i] = set[v%62]
		v /= 62
	}
	return string(b)
}

// genJWT makes a JWT whose header names an algorithm, with a made-up
// subject and signature.
func genJWT(g *gen) string {
	enc := func(s string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	head := enc(`{"alg":"` + g.pick([]string{"HS256", "RS256", "ES256"}) + `","typ":"JWT"}`)
	body := enc(`{"sub":"` + g.chars(8, alnum) + `","exp":` + g.chars(1, "123456789") + g.chars(9, digits) + `}`)
	return head + "." + body + "." + g.chars(43, urlSafe)
}

// providerCarriers hold a provider token where one is pasted.
var providerCarriers = []string{
	"use %s now",
	"export SERVICE_CREDENTIAL=%s",
	`{"value": "%s", "env": "prod"}`,
	"| prod | %s | rotated |",
	"%s",
	"the new one is %s.",
	"curl -H 'X-Api-Key: %s' https://api.acme.example/v1/items",
	"- %s\n- the next item",
	"config:\n  value: %s\n",
	"I pasted `%s` by mistake",
}

// randomTokenSets are the character classes of random tokens.
var randomTokenSets = []struct{ name, set string }{
	{"hex", hexLower}, {"base32", base32Set}, {"base62", alnum}, {"base64", base64Set}, {"digits", digits}, {"urlsafe", urlSafe},
}

// triggerCarriers hold a random token near a trigger word.
var triggerCarriers = []string{
	"the staging token is %s",
	"secret: %s",
	"api key %s",
	"password %s for the box",
	"my access key is %s",
	"here is the credential %s, rotate it later",
	"bearer %s",
	"client_secret %s",
	"the passphrase is %s",
}

// hexCarriers are phrases a 32-character hex key follows.
var hexCarriers = []string{
	"the staging token is %s",
	"the api key is %s",
	"here is the secret %s",
	"my access key is %s",
	"the password is %s",
	"token %s",
	"use secret %s for it",
	"the bearer value is %s",
}

// generatedCases returns the cases the generators make, in a fixed order.
func generatedCases() []corpusCase {
	var all []corpusCase
	plant := func(class, name, carrier, secret string) corpusCase {
		i := strings.Index(carrier, "%s")
		c := corpusCase{file: "generated", label: labelSecret, class: class, name: name, text: carrier[:i] + secret + carrier[i+2:]}
		c.secrets = []textRange{{i, i + len(secret)}}
		return c
	}
	// add plants secrets from make until one fits its carrier, as checkCase
	// asks.
	add := func(class, name, carrier string, make func() string) {
		var err error
		for try := 0; try < 100; try++ {
			c := plant(class, name, carrier, make())
			if err = checkCase(c); err == nil {
				all = append(all, c)
				return
			}
		}
		panic(fmt.Sprintf("the generator of %s %s makes no secret that fits %q: %v", class, name, carrier, err))
	}
	g := newGen(1201)
	for i := 0; i < genCount("gen_provider"); i++ {
		f := tokenFormats[i%len(tokenFormats)]
		carrier := g.pick(providerCarriers)
		if f.around != "" {
			carrier = strings.Replace(carrier, "%s", f.around, 1)
		}
		add("gen_provider", f.name, carrier, func() string { return f.make(g) })
	}
	g = newGen(1202)
	for i := 0; i < genCount("gen_random"); i++ {
		set := randomTokenSets[i%len(randomTokenSets)]
		n := []int{16, 20, 24, 32, 40, 48, 64}[g.r.Intn(7)]
		add("gen_random_"+set.name, fmt.Sprintf("%d %s characters", n, set.name), g.pick(triggerCarriers), func() string { return g.chars(n, set.set) })
	}
	// Passwords in the line shapes of TestNoSecretVersion1Removed.
	g = newGen(1203)
	keys := []string{"password", "DB_PASSWORD", "api_key", "API_KEY", "secret", "client_secret", "access_token", "token", "refresh_token", "db.password", "PGPASSWORD", "passphrase"}
	shapes := []string{
		"%s=%s", "export %s=%s", "%s: %s", "  %s: %s", "%s = %s", "%s=%s # rotated", "run %s=%s now", "log %s=%s {\"user\":\"bob\"}",
		"https://x.example/cb?state=abc&%s=%s&next=/home", "Server=db;%s=%s;Database=app", "curl -d 'user=bob&%s=%s'",
		"%s=%s psql -h db", "f(%s=%s)", "  \"%s\": %s,",
		"https://x.example/cb?access_token=" + "Ab3dEf6h" + "Ij9&expires_in=3599&%s=%s",
		"?token=<your-token>&%s=%s",
	}
	const sym = "!@#$%^&*()-_=+[]{};:,.<>/?~|"
	for i := 0; i < genCount("gen_password"); i++ {
		shape := strings.Replace(shapes[g.r.Intn(len(shapes))], "%s", keys[g.r.Intn(len(keys))], 1)
		add("gen_password", "password in a line", shape, func() string {
			var b strings.Builder
			for k := 10 + g.r.Intn(14); k > 0; k-- {
				if g.r.Intn(4) == 0 {
					b.WriteByte(sym[g.r.Intn(len(sym))])
				} else {
					b.WriteByte(alnum[g.r.Intn(len(alnum))])
				}
			}
			return b.String()
		})
	}
	g = newGen(1204)
	for i := 0; i < genCount("gen_hex32"); i++ {
		add("gen_hex32", "32 hex characters after a phrase", g.pick(hexCarriers), func() string { return g.chars(32, hexLower) })
	}
	// The gap classes' generators: secrets in the shapes of code that the
	// current rules keep on purpose and the baseline rules remove, after a
	// secret-looking key and each separator such a key takes. Where the
	// current rules alone keep one that the baseline rules remove, TestOracle
	// counts it as a gap of its class, not as a violation (see
	// oracleGapShapes and oracle_gaps.txt).
	gapSeps := []string{": ", ":", " = ", " := ", " => ", " == "}
	// gapLine picks a line of shapes, each with a %s for the key, the
	// separator and the value, and fills in a key and a separator of seps.
	gapLine := func(g *gen, shapes, seps []string) string {
		return fmt.Sprintf(g.pick(shapes), g.pick(gapKeys), g.pick(seps), "%s")
	}
	// Values that read as regular expression literals: base64 that starts
	// with '/', holds a '+', and ends with '/' and up to two flags.
	g = newGen(1205)
	for i := 0; i < genCount("gen_oracle_gap_regex_value"); i++ {
		shape := gapLine(g, []string{"%s%s%s", "  %s%s%s", "%s%s%s # rotated", "%s%s%s and more", "%s%s%s;", "%s%s%s,"}, gapSeps)
		add("gen_oracle_gap_regex_value", "regex-like value after a key", shape, func() string {
			body := []byte(g.chars(16+g.r.Intn(25), base64Set))
			body[1+g.r.Intn(len(body)-1)] = '+'
			return "/" + string(body) + "/" + g.pick(gapFlags)
		})
	}
	// Passwords made of words after a '*', which read as a YAML alias.
	g = newGen(1206)
	for i := 0; i < genCount("gen_oracle_gap_yaml_alias"); i++ {
		shape := gapLine(g, []string{"%s%s%s", "  %s%s%s", "db:\n  %s%s%s\n  port: 5432"}, []string{": ", ":", " := ", " == "})
		add("gen_oracle_gap_yaml_alias", "a password of words after a star", shape, func() string { return "*" + wordsValue(g) })
	}
	// Passwords made of words, as a name, a member path or an instance
	// variable, followed by an index, a literal or a block.
	g = newGen(1207)
	for i := 0; i < genCount("gen_oracle_gap_name_index"); i++ {
		tails := []string{"[0]", "[i]", "[x9]", `["ab"]`, "{a}", "[i] = 1"}
		value := func() string { return wordsValue(g) }
		switch i % 4 {
		case 2:
			value = func() string { return g.pick(gapWords) + "." + g.pick(gapWords) }
			tails = append(tails, " { x }")
		case 3:
			value = func() string {
				return "@" + strings.ToLower(g.pick(gapWords)) + "_" + strings.ToLower(g.pick(gapWords))
			}
			tails = append(tails, " { x }")
		}
		shape := gapLine(g, []string{"%s%s%s" + g.pick(tails)}, gapSeps)
		add("gen_oracle_gap_name_index", "a password of words before an index", shape, value)
	}
	// Random tokens followed by what reads as an assignment or a union of
	// types, as in "api_key: Xk9mP2vLq = 1" or "token: Xk9mP2vLq | None".
	g = newGen(1208)
	for i := 0; i < genCount("gen_oracle_gap_type_annotation"); i++ {
		tail := g.pick([]string{" = 1", " = None", " = x", " | None", " | str", " | None = None"})
		shape := gapLine(g, []string{"%s%s%s" + tail, "  %s%s%s" + tail}, gapSeps)
		add("gen_oracle_gap_type_annotation", "a token before an assignment", shape, func() string {
			return g.chars(1, alnum[:52]) + g.mixed(9+g.r.Intn(14))
		})
	}
	// The near classes' generators: for each gap shape, secrets just past
	// each bound of the exceptions that make it (see oracleBounds), where the
	// current rules remove them today, the bounds of a shape in turn. They are
	// held to the baseline like any class, so an exception that widens past a
	// bound fails the oracle.
	for k, shape := range []string{"regex_value", "yaml_alias", "name_index", "type_annotation"} {
		g = newGen(int64(1209 + k))
		class := "gen_oracle_near_" + shape
		bounds := boundsOf(shape)
		for i := 0; i < genCount(class); i++ {
			b := bounds[i%len(bounds)]
			line, value := b.near(g, i/len(bounds))
			add(class, b.id, line, value)
		}
	}
	return all
}

// nearToken makes a random token of 10 to 22 letters and digits that starts
// with a letter and reads as no name of words, as a generated password does.
func nearToken(g *gen) string {
	for {
		s := g.chars(1, alnum[:52]) + g.mixed(9+g.r.Intn(13))
		if !wordLike(s, 1) {
			return s
		}
	}
}

// nearRegex makes a value between slashes, of letters and digits and, with
// meta, a '+', followed by flags.
func nearRegex(g *gen, meta bool, flags string) string {
	body := []byte(g.mixed(16 + g.r.Intn(25)))
	if meta {
		body[1+g.r.Intn(len(body)-1)] = '+'
	}
	return "/" + string(body) + "/" + flags
}

// gapWords are the words of the made-up passwords of the gap classes, each
// of five letters or more and none part of a key or a marker.
var gapWords = []string{"Summer", "Winter", "Autumn", "Orange", "Tiger", "Falcon", "Harbor", "Maple", "Copper", "Silver", "River", "Canyon", "Meadow", "Thunder", "Garden", "Pepper", "Marble", "Velvet", "Window", "Rocket", "Basket", "Candle", "Pillow", "Lantern", "Walnut", "Cobalt"}

// wordsValue makes a password of words, as people choose them:
// Summer2Winter, mapleCopper, Falcon7Harbor3 or river_canyon_walnut.
func wordsValue(g *gen) string {
	a, b := g.pick(gapWords), g.pick(gapWords)
	switch g.r.Intn(4) {
	case 0:
		return a + g.chars(1+g.r.Intn(2), digits) + b
	case 1:
		return strings.ToLower(a) + b
	case 2:
		return a + g.chars(1, digits) + b + g.chars(1, digits)
	default:
		return strings.ToLower(a) + "_" + strings.ToLower(b) + "_" + strings.ToLower(g.pick(gapWords))
	}
}

// engineRun is what a rule set made of a case's text.
type engineRun struct {
	out   string
	spans []textRange // the ranges of the text it replaced; nil for the baseline rules
}

// measured is what each rule set made of each case: the baseline rules,
// v1's engine (Redact with the baseline at run time) and the current rules
// alone.
type measured struct {
	cases                  []corpusCase
	baseline, v1, alone    []engineRun
	corpusCount, generated int
}

var (
	measureOnce sync.Once
	measureAll  *measured
)

// measure runs the three rule sets on the corpus and the generated cases,
// once for the test binary.
func measure(t testing.TB) *measured {
	t.Helper()
	measureOnce.Do(func() {
		corpus := loadCorpus(t)
		gen := generatedCases()
		m := &measured{cases: append(corpus, gen...), corpusCount: len(corpus), generated: len(gen)}
		m.baseline = make([]engineRun, len(m.cases))
		m.v1 = make([]engineRun, len(m.cases))
		m.alone = make([]engineRun, len(m.cases))
		var wg sync.WaitGroup
		next := make(chan int)
		for w := 0; w < runtime.GOMAXPROCS(0); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range next {
					text := m.cases[i].text
					out, _ := baseline.Redact(text)
					m.baseline[i] = engineRun{out: out}
					m.v1[i] = runEngine(driver{ctx: context.Background()}, text)
					m.alone[i] = runEngine(driver{ctx: context.Background(), alone: true}, text)
				}
			}()
		}
		for i := range m.cases {
			next <- i
		}
		close(next)
		wg.Wait()
		measureAll = m
	})
	if measureAll == nil {
		t.Fatal("the corpus could not be measured")
	}
	return measureAll
}

// runEngine redacts text with d and returns the output and the ranges
// replaced. The texts are clean, so Clean is the text and the findings are
// ranges of it.
func runEngine(d driver, text string) engineRun {
	r := d.scan(text)
	e := engineRun{out: Apply(r)}
	for _, f := range r.Findings {
		e.spans = append(e.spans, textRange{f.Start, f.End})
	}
	return e
}

// The corpus reads, and every case keeps the README's rules.
func TestCorpusReads(t *testing.T) {
	isolate(t)
	cases := loadCorpus(t)
	labels := map[string]int{}
	classes := map[string]bool{}
	secrets := 0
	for _, c := range cases {
		labels[c.label]++
		classes[c.class] = true
		secrets += len(c.secrets)
	}
	if labels[labelSecret] < 100 || labels[labelNegative] < 30 || labels[labelFact] < 4 {
		t.Fatalf("too few cases: %v", labels)
	}
	for _, want := range []string{"provider", "private_key", "jwt", "url_password", "auth_header", "key_value_shell", "key_value_yaml", "key_value_json", "key_value_toml", "key_value_query", "connection_string", "command_password", "prose_password", "random_near_trigger", "audit_kept", "code", "prose", "docs", "chat", "sha", "uuid", "reference", "fact"} {
		if !classes[want] {
			t.Errorf("no case of class %s", want)
		}
	}
	// The T3 row of the ledger design is in the facts.
	for _, want := range []string{"optional", "https://auth.example.com/oauth/token", "~/.ssh/id_ed25519_deploy", "X-Internal-Api-Key"} {
		found := false
		for _, c := range cases {
			for _, f := range c.facts {
				found = found || c.value(f) == want
			}
		}
		if !found {
			t.Errorf("no fact %q", want)
		}
	}
	t.Logf("%d cases, %d secrets, labels %v", len(cases), secrets, labels)
}

// The corpus is in a public repository: no case names a model, as a chat
// line could say which one the owner uses. A case that needs a model's id
// makes one up, as model-x-20261028.
func TestCorpusNamesNoModel(t *testing.T) {
	isolate(t)
	files, err := filepath.Glob(filepath.Join(corpusDir, "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus files: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(data))
		for _, word := range []string{"claude", "opus", "sonnet", "haiku", "gpt-", "gemini", "llama", "mistral"} {
			if strings.Contains(text, word) {
				t.Errorf("%s names a model (%q): make one up", f, word)
			}
		}
	}
}

// The reader refuses a case that breaks the README's rules.
func TestCorpusReaderRefusesBadCases(t *testing.T) {
	isolate(t)
	for _, good := range []string{
		"comment\n=== secret provider a token\nuse '⟦abc¦defghi⟧' now\n",
		"=== secret provider a token\nuse '⟦¦abc¦defghi¦⟧' now\n",
		"=== secret provider a token\nuse '⟦abc¦def¦ghi⟧' now\n",
	} {
		if cases, err := parseCorpus("x.txt", good); err != nil || len(cases) != 1 || cases[0].value(cases[0].secrets[0]) != "abcdefghi" || cases[0].text != "use abcdefghi now" {
			t.Fatalf("parseCorpus(%q) = %+v, %v", good, cases, err)
		}
	}
	for name, data := range map[string]string{
		"no cases":                "just a comment\n",
		"bad label":               "=== secrets provider x\nuse '⟦abc¦defghi⟧'\n",
		"no name":                 "=== secret provider\nuse '⟦abc¦defghi⟧'\n",
		"two-word class":          "=== secret Provider-X x\nuse '⟦abc¦defghi⟧'\n",
		"secret not split":        "=== secret provider x\nuse '⟦abcdefghi⟧'\n",
		"split only at the start": "=== secret provider x\nuse '⟦¦abcdefghi⟧'\n",
		"split only at the end":   "=== secret provider x\nuse '⟦abcdefghi¦⟧'\n",
		"split only at the edges": "=== secret provider x\nuse '⟦¦abcdefghi¦⟧'\n",
		"split before the secret": "=== secret provider x\nuse ¦'⟦abcdefghi⟧'\n",
		"split after the secret":  "=== secret provider x\nuse '⟦abcdefghi⟧'¦\n",
		"secret too short":        "=== secret provider x\nuse '⟦ab¦cde⟧'\n",
		"secret shares a piece":   "=== secret provider x\nabcdefghi '⟦abc¦defghi⟧'\n",
		"secret shares a marker":  "=== secret provider x\nuse '⟦secret¦_value⟧'\n",
		"secret case no secret":   "=== secret provider x\nnothing here\n",
		"negative with a secret":  "=== negative code x\nuse '⟦abc¦defghi⟧'\n",
		"negative with a fact":    "=== negative code x\ntoken: ⟪optional⟫\n",
		"fact case no fact":       "=== fact fact x\ntoken: optional\n",
		"fact case with a secret": "=== fact fact x\ntoken: ⟪optional⟫ '⟦abc¦defghi⟧'\n",
		"open mark":               "=== secret provider x\nuse '⟦abc¦defghi\n",
		"nested marks":            "=== secret provider x\nuse '⟦abc¦⟪defghi⟫⟧'\n",
		"stray close":             "=== secret provider x\nuse '⟦abc¦defghi⟧' and ⟧ here\n",
		"stray open bracket":      "=== secret provider x\nuse '⟦abc¦defghi⟧' and ⟦ here\n",
		"text the rules clean":    "=== secret provider x\nuse '⟦abc¦defghi⟧' \x1b[31mred\n",
		"secret not ascii":        "=== secret provider x\nuse '⟦abcé¦defghi⟧'\n",
	} {
		if _, err := parseCorpus("x.txt", data); err == nil {
			t.Errorf("%s: parseCorpus took %q", name, data)
		}
	}
}

// keeps finds any 6 characters of a secret in a text, and nothing less.
func TestKeeps(t *testing.T) {
	isolate(t)
	secret := "abcdefghij"
	for out, want := range map[string]bool{
		"x abcdefghij y":             true,
		"x [REDACTED:secret] efghij": true,
		"x abcde [REDACTED:x] fghij": false,
		"":                           false,
		"bcdefg":                     true,
	} {
		if keeps(out, secret) != want {
			t.Errorf("keeps(%q, %q) = %v, want %v", out, secret, !want, want)
		}
	}
}

// The generators make the number of cases asked for, each keeping the
// README's rules, the same on every run; their provider tokens are of the
// formats the current rules know; and no GitHub token, generated or in the
// corpus, carries the checksum of a token GitHub issued.
func TestGenerators(t *testing.T) {
	isolate(t)
	gen := generatedCases()
	total := 0
	for _, n := range genCounts {
		total += n
	}
	if !raceOn && (len(gen) != total || total < 20000) {
		t.Fatalf("%d generated cases, want %d, at least 20,000", len(gen), total)
	}
	again := generatedCases()
	if !slices.EqualFunc(gen, again, func(a, b corpusCase) bool { return a.text == b.text && a.class == b.class }) {
		t.Fatal("the generators made other cases on a second run")
	}
	byClass := map[string]int{}
	for _, c := range gen {
		byClass[c.class]++
		if err := checkCase(c); err != nil {
			t.Fatal(err)
		}
	}
	if byClass["gen_hex32"] < 2000 && !raceOn {
		t.Fatalf("%d hex keys, want at least 2,000", byClass["gen_hex32"])
	}
	// Every provider format is one the current rules take whole, alone.
	g := newGen(99)
	for _, f := range tokenFormats {
		tok := f.make(g)
		around := f.around
		if around == "" {
			around = "%s"
		}
		out := Apply(driver{ctx: context.Background(), alone: true}.scan("use " + strings.Replace(around, "%s", tok, 1) + " now"))
		if keeps(out, tok) {
			t.Errorf("the current rules keep a %s token: %q", f.name, out)
		}
	}
	checked := 0
	for _, c := range append(loadCorpus(t), gen...) {
		for _, s := range c.secrets {
			v := c.value(s)
			if len(v) == 40 && (strings.HasPrefix(v, "gh") && v[3] == '_') {
				checked++
				if githubChecksum(v[:3], v[4:34], v[34:]) {
					t.Errorf("%s holds a GitHub token with a valid checksum", c)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no GitHub token checked")
	}
	if !githubChecksum("ghp", "abcdefghijklmnopqrstuvwxyz0123", base62(crc32.ChecksumIEEE([]byte("abcdefghijklmnopqrstuvwxyz0123")), 6)) {
		t.Fatal("githubChecksum does not know a checksum")
	}
}
