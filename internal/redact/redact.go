// Package redact removes secrets from text before eva-md stores or commits it.
package redact

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version identifies the rule set. Raise it whenever a rule changes.
const Version = 3

// marker starts every replacement. Only a complete marker (markerRe) counts
// as earlier redaction, so text that merely starts like one shields nothing.
const marker = "[REDACTED:"

// maxPasses bounds the passes redact makes. A pass that changes anything
// replaces unredacted text, and a replacement rarely brings more text within
// reach of a rule, so the loop ends long before.
const maxPasses = 16

// Entropy rule settings.
const (
	minEntropyLen   = 20  // shortest token the entropy rule considers
	minEntropyBits  = 3.5 // Shannon entropy per character, in bits
	entropyWords    = 5   // how many words after a trigger word are checked
	entropyMaxBytes = 256 // how far after a trigger word is checked
)

// Settings for the value after a secret-looking key.
const (
	minValueLen  = 8        // shortest value replaced
	maxCodeScan  = 512      // how far a value inside a line is scanned for code
	maxFloorScan = 64 << 10 // how far the floor follows a baseline value
)

type rule struct {
	kind  string
	re    *regexp.Regexp
	group int               // submatch to replace; 0 replaces the whole match
	hints []string          // lower-case literals; the rule runs only when one appears
	check func(string) bool // optional: the replaced text must pass it
	// value, when set, picks the span to replace from a match's submatch
	// indexes, given the markers already in the text.
	value func(text string, m []int, marks []span) (span, bool)
	// resume searches again inside a match whose value turned out shorter
	// than the match, or was rejected, so a second key=value pair in the
	// same token, as in a URL query or a form body, is found too.
	resume bool
	// family groups a value rule with its floor rule; floor marks the floor.
	// The value rule applies the floor itself, at each match it rejects, so a
	// floor rule runs only when its family's rule stopped resuming its search
	// in the same pass and may have passed over a key.
	family int
	floor  bool
	// format marks a provider token format, such as sk-... or ops_.... In
	// eva-md's own identifiers, a match that reads as a name is kept (see
	// nameish): a branch called sk-learn-pipeline is not an OpenAI key.
	format bool
}

// mode says how a text is redacted.
type mode struct {
	entropy bool // apply the entropy rule
	names   bool // keep provider token formats that read as names
}

// The separator after a secret-looking key, then the value up to the next
// whitespace or quote. Submatch 1 is the quote that may close a quoted key,
// and submatch 2 the value. secretValue decides how much of it, or of the
// rest of its line, is the value.
const valueRe = `(["']?)[ \t]*[:=][=>]?[ \t]*["']?([^\s"'\x60]+)`

// floorValueRe is the value pattern of the baseline rules: 8 or more
// characters up to whitespace, a quote or one of , ; & < > ( ) { } [ ].
// The floor rules use it so that a value the baseline rules redact is still
// redacted, unless it is one of the few shapes of code the baseline rules
// redact by mistake (see floorValue). Submatches as in valueRe.
const floorValueRe = `(["']?)[ \t]*[:=][=>]?[ \t]*["']?([^\s"'\x60,;&<>(){}\[\]]{8,})`

// The secret words a key contains, in the two families the value rules use.
const (
	passwordWords = `(?i:password|passwd|passphrase|secret|private[_\-]?key)`
	tokenWords    = `(?i:token|api[_\-]?key|apikey|access[_\-]?key)`
)

// A key containing a secret word. A word may be followed by more of the key
// only after '_', '-', '.' or a capital letter, so "tokenizer" and
// "input_tokens" are not keys while "tokenSecret" and "API_KEY_PROD" are.
const keyTail = `(?:[A-Z_.\-][A-Za-z0-9_.\-]*)?`

// rules run in order. Specific formats come before the generic rules, so a
// known token is labelled with its own kind.
var rules = []rule{
	{
		kind:  "private_key",
		re:    regexp.MustCompile(`-----BEGIN[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----(?s:.*?)(?:-----END[A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|\z)`),
		hints: []string{"private key"},
	},
	{
		kind:   "anthropic_key",
		re:     regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`),
		hints:  []string{"sk-ant-"},
		format: true,
	},
	{
		kind:   "openai_key",
		re:     regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_\-]{20,}`),
		hints:  []string{"sk-"},
		format: true,
	},
	{
		kind:   "onepassword_token",
		re:     regexp.MustCompile(`\bops_[A-Za-z0-9_\-]{20,}`),
		hints:  []string{"ops_"},
		format: true,
	},
	{
		kind:   "github_token",
		re:     regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,255}\b|\bgithub_pat_[A-Za-z0-9_]{22,255}\b`),
		hints:  []string{"gh", "github_pat_"},
		format: true,
	},
	{
		kind:   "gitlab_token",
		re:     regexp.MustCompile(`\b(?:glpat|gldt|glrt|glptt|gloas)-[A-Za-z0-9_\-]{20,}`),
		hints:  []string{"gl"},
		format: true,
	},
	{
		kind:  "slack_webhook",
		re:    regexp.MustCompile(`(?i)https://hooks\.slack\.com/(?:services|workflows|triggers)/[A-Za-z0-9_/\-]+`),
		hints: []string{"hooks.slack.com"},
	},
	{
		kind:   "slack_token",
		re:     regexp.MustCompile(`\bxox[abposre]-[A-Za-z0-9\-]{10,}|\bxapp-[0-9]-[A-Za-z0-9\-]{10,}`),
		hints:  []string{"xox", "xapp-"},
		format: true,
	},
	{
		kind:   "aws_key",
		re:     regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`),
		hints:  []string{"akia", "asia", "abia", "acca"},
		format: true,
	},
	{
		kind:  "aws_key",
		re:    regexp.MustCompile(`(?i)\baws[A-Za-z0-9_\-]*(?:secret|sk)[A-Za-z0-9_\-]*["']?[ \t]*[:=][ \t]*["']?([A-Za-z0-9/+]{40})(?:[^A-Za-z0-9/+=]|\z)`),
		group: 1,
		hints: []string{"aws"},
	},
	{
		kind:   "google_key",
		re:     regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}`),
		hints:  []string{"aiza"},
		format: true,
	},
	{
		kind:   "stripe_key",
		re:     regexp.MustCompile(`\b[sr]k_(?:live|test)_[0-9A-Za-z]{16,}`),
		hints:  []string{"k_live_", "k_test_"},
		format: true,
	},
	{
		kind:  "jwt",
		re:    regexp.MustCompile(`\b[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{5,}`),
		hints: []string{"."},
		check: isJWT,
	},
	{
		kind:  "url_credentials",
		re:    regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]*://[^/\s:@'"]*:([^/\s@'"]+)@`),
		group: 1,
		hints: []string{"://"},
		check: func(s string) bool { return !opRef(s) },
	},
	{
		kind:  "auth_header",
		re:    regexp.MustCompile(`(?i)\bauthorization["']?[ \t]*[:=][ \t]*["']?(?:bearer|basic|token|digest|negotiate)[ \t]+([^\s"'\x60,;]+)`),
		group: 1,
		hints: []string{"authorization"},
		check: func(s string) bool { return !opRef(s) },
	},
	{
		kind:  "secret_value",
		re:    regexp.MustCompile(`[A-Za-z0-9_.\-]*(?i:password|passwd|passphrase|secret|private[_\-]?key|token|api[_\-]?key|apikey|access[_\-]?key)` + keyTail + `["']?[ \t]*[:=][=>]?[ \t]*"((?:\\.|[^"\\\r\n])+)"`),
		group: 1,
		hints: []string{"passw", "passphrase", "secret", "token", "key"},
		check: func(s string) bool { return len(s) >= minValueLen && !opRef(s) },
	},
	{
		kind:  "secret_value",
		re:    regexp.MustCompile(`[A-Za-z0-9_.\-]*(?i:password|passwd|passphrase|secret|private[_\-]?key|token|api[_\-]?key|apikey|access[_\-]?key)` + keyTail + `["']?[ \t]*[:=][=>]?[ \t]*'((?:\\.|[^'\\\r\n])+)'`),
		group: 1,
		hints: []string{"passw", "passphrase", "secret", "token", "key"},
		check: func(s string) bool { return len(s) >= minValueLen && !opRef(s) },
	},
	{
		kind:  "auth_header",
		re:    regexp.MustCompile(`(?i)\bbearer[ \t]+([A-Za-z0-9._~+/\-]{16,}=*)`),
		group: 1,
		hints: []string{"bearer"},
		// A bearer token is not the value of a secret-looking key, so a SHA or
		// UUID is kept here as in ordinary text. Taking it would also leave a
		// marker inside a passphrase line such as PASSWORD=... Bearer <uuid>
		// ..., and the passphrase rule would then keep the other words.
		check: func(s string) bool { return hasDigit(s) && hasLetter(s) && !keep(s) && looksSecret(s) },
	},
	{
		kind:   "secret_value",
		re:     regexp.MustCompile(`[A-Za-z0-9_.\-]*` + passwordWords + keyTail + valueRe),
		group:  2,
		hints:  []string{"passw", "passphrase", "secret", "private"},
		value:  secretValue,
		resume: true,
		family: 1,
	},
	{
		kind:   "secret_value",
		re:     regexp.MustCompile(`[A-Za-z0-9_.\-]*` + tokenWords + keyTail + valueRe),
		group:  2,
		hints:  []string{"token", "key"},
		value:  secretValue,
		resume: true,
		family: 2,
	},
	// The floor: what the baseline rules redact stays redacted.
	{
		kind:   "secret_value",
		re:     regexp.MustCompile(`[A-Za-z0-9_.\-]*` + passwordWords + keyTail + floorValueRe),
		group:  2,
		hints:  []string{"passw", "passphrase", "secret", "private"},
		value:  floorValue,
		family: 1,
		floor:  true,
	},
	{
		kind:   "secret_value",
		re:     regexp.MustCompile(`[A-Za-z0-9_.\-]*` + tokenWords + keyTail + floorValueRe),
		group:  2,
		hints:  []string{"token", "key"},
		value:  floorValue,
		family: 2,
		floor:  true,
	},
}

// A type name in an annotation such as "str", "Optional[str]" or
// "Promise<string>?".
const typeName = `[A-Za-z_][A-Za-z0-9_.]*(?:\[[^\]\r\n]*\]|<[^>\r\n]*>)?\??`

// A name in code, or a marker that replaced one.
const codeName = `(?:[A-Za-z_$][A-Za-z0-9_$]*|\[REDACTED:[a-z_]{1,32}\])`

var (
	markerRe = regexp.MustCompile(`\[REDACTED:[a-z_]{1,32}\]`)
	ansiRe   = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[@-Z\\\\-_]|\u009b[0-?]*[ -/]*[@-~]")
	uuidRe   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	shaRe    = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	opRefRe  = regexp.MustCompile(`op://[^\s"'\x60,;<>]+`)

	entropyTrigger = regexp.MustCompile(`(?i)\b(?:password|passwd|passphrase|secret|token|api[_\-]?key|apikey|access[_\-]?key|client[_\-]?secret|private[_\-]?key|credential|bearer)\b`)
	entropyToken   = regexp.MustCompile(`[A-Za-z0-9+/_\-.~]{20,}={0,2}`)
	variableRef    = regexp.MustCompile(`^(?:\$[A-Za-z_][A-Za-z0-9_]*|\$\{[A-Za-z_][A-Za-z0-9_]*\}|(?:os\.environ|process\.env|self|config)\.[A-Za-z_][A-Za-z0-9_]*|os\.environ)$`)
	placeholder    = regexp.MustCompile(`^<[^<>]*>[.,;:!?)\]]*$`)

	// codeHead starts a call, index or literal after a secret-looking key, up
	// to its opening bracket: getToken(user), os.environ["X"],
	// Config::token(), Config{...}, []byte("x"), map[string]int{},
	// &Block{...} or @cache[key], perhaps after await or new. A marker counts
	// as a name (codeName), so redacting part of a call in an earlier pass
	// does not turn the call into a value.
	codeHead = regexp.MustCompile(`^(?:(?:await|new)[ \t]+)?(?:\[[0-9.]*\]|map\[[^\]\s]*\]|[&*])*(?:::|@@?)?` + codeName + `(?:(?:\.|\?\.|::|->)` + codeName + `)*[?!]?(?:[(\[]|[ \t]*\{)`)
	// refHead is a reference such as &config or a YAML alias such as *defaults.
	refHead = regexp.MustCompile(`^[&*][A-Za-z_][A-Za-z0-9_.]*`)
	// regexHead is a regular expression literal, such as /^v(\d+)$/i.
	regexHead = regexp.MustCompile(`^/(?:\\.|[^/\\\r\n])+/[a-z]*`)
	// percentHead starts a Ruby literal such as %r{...} or %w[...].
	percentHead = regexp.MustCompile(`^%[qQwWiIr]?[(\[{<]`)
	// literal is a value that reads as code when it starts a value of
	// several words, as in "nil if uri.password" or "None, retries=3".
	literal = regexp.MustCompile(`^(?:nil|None|null|NULL|true|false|True|False|undefined|-?[0-9][0-9a-fA-FxX_.]*)$`)
	// yamlProperties are a YAML anchor or tag before a value, as in
	// "password: &pw value" or "password: !!str value".
	yamlProperties = regexp.MustCompile(`^(?:[&!][!A-Za-z0-9_.\-]*[ \t]+)+`)
	// typeHead matches a type annotation that starts a value, as in
	// "api_key: str = ..." or "token: string | null". Submatch 1 holds any
	// further union members; when a value is assigned, the match ends after
	// the '=' and the whitespace that follows it. Code puts spaces around the
	// '|' and '=', which keeps passwords such as hunter|Secret99 out.
	typeHead = regexp.MustCompile(`^` + typeName + `((?:[ \t]+\|[ \t]+` + typeName + `)*)(?:[ \t]+=[ \t]+|$)`)
	// nextPair starts another key=value pair, as after the '&' of a URL query
	// or the ';' of a connection string.
	nextPair = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]*=`)
	// nextSecretKey starts another secret-looking key and its separator, as
	// in "...&access_key = ...": a value ends before it in any context.
	nextSecretKey = regexp.MustCompile(`^[A-Za-z0-9_.\-]*(?:` + passwordWords + `|` + tokenWords + `)` + keyTail + `["']?[ \t]*[:=]`)
	// regexFlags are the flags a regular expression literal may end with.
	regexFlags = regexp.MustCompile(`^[dgimsuvyxoen]{0,6}$`)
	// memberSegment is one name of a member path such as stmt.Token.
	memberSegment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// RedactKnown applies the current rules without the entropy rule, and without
// the baseline rules, which include it: it replaces only secrets in a known
// format, such as provider tokens, private keys, JWTs, Authorization
// values, URL passwords and key=value secrets. It suits eva-md's own
// identifiers, such as project keys and branch names, where words like token,
// secret and password are ordinary names that the entropy rule would rewrite:
// local-token-refresh-worker-c9a8c7 would become
// local-token[REDACTED:high_entropy].
func RedactKnown(text string) (string, int) { return redact(text, mode{}) }

func redact(text string, m mode) (string, int) {
	text = clean(text)
	total := 0
	for i := 0; i < maxPasses; i++ {
		var n int
		text, n = pass(text, m)
		total += n
		if n == 0 {
			break
		}
	}
	return text, total
}

// Identifier cleans one of eva-md's own identifiers, such as a branch name or
// the thread key made from it, before it is stored or compared. It removes
// escape sequences and control characters first, so they cannot split a
// token, then applies RedactKnown, keeping a provider token format that reads
// as a name (nameish), and keeps at most limit runes (all of them when limit
// is zero or less). It returns the result and the number of replacements.
// When it replaced anything, a short hash of the identifier starts the
// result, so two identifiers that differ only in what was replaced stay
// apart, as thread keys must. Cleaning a cleaned identifier changes nothing,
// so a stored thread key still matches the branch it came from.
func Identifier(s string, limit int) (string, int) {
	out, n := identifier(s, limit)
	// The cut that makes room for the hash can complete a match, as when
	// token=getToken(user) becomes token=getToken, so clean the result again
	// until it is stable. Where one cleaning was stable, nothing changes.
	for i := 0; i < maxPasses; i++ {
		again, k := identifier(out, limit)
		if again == out {
			break
		}
		out, n = again, n+k
	}
	return out, n
}

// identifier cleans an identifier once, as Identifier describes.
func identifier(s string, limit int) (string, int) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, clean(s))
	names := mode{names: true}
	out, n := redact(s, names)
	// Cut only after redacting, so a cut cannot leave the start of a secret
	// behind. A marker can be longer than the text it replaced, and a cut can
	// complete a match, so redact the cut text again until it fits.
	for i := 0; i < maxPasses && limit > 0 && utf8.RuneCountInString(out) > limit; i++ {
		var k int
		out, k = redact(cutRunes(out, limit), names)
		n += k
	}
	out = cutRunes(out, limit)
	if n > 0 {
		// The hash goes in front: after the text it would join the last value,
		// and redacting the result again would take it in.
		sum := sha256.Sum256([]byte(s))
		prefix := hex.EncodeToString(sum[:4]) + "-"
		if limit > 0 {
			out = cutRunes(out, max(0, limit-len(prefix)))
		}
		out = prefix + out
	}
	return out, n
}

// cutRunes returns the first limit runes of s, or all of s when limit is zero
// or less.
func cutRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	for i := range s {
		if limit == 0 {
			return s[:i]
		}
		limit--
	}
	return s
}

// pass runs every rule once, and the entropy rule when m.entropy is set.
func pass(text string, m mode) (string, int) {
	lower := strings.ToLower(text)
	count := 0
	var missed [3]bool // a family's value rule stopped resuming its search
	for _, r := range rules {
		if !hinted(lower, r.hints) || r.floor && !missed[r.family] {
			continue
		}
		var n int
		var miss bool
		text, n, miss = apply(text, r, m)
		if r.family > 0 && !r.floor {
			missed[r.family] = miss
		}
		if n > 0 {
			count += n
			lower = strings.ToLower(text)
		}
	}
	if !m.entropy {
		return text, count
	}
	var n int
	text, n = redactEntropy(text, lower)
	return text, count + n
}

// clean strips ANSI escape sequences, bidi controls and NUL, and removes
// zero-width characters. Invalid UTF-8 becomes U+FFFD.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == 0:
			return -1
		case unicode.Is(unicode.Cf, r) || r == 0x034F:
			return -1 // bidi controls and invisible formatting/joiner characters
		}
		return r
	}, s)
	if strings.ContainsAny(s, "\x1b\u009b") {
		s = ansiRe.ReplaceAllString(s, "")
		s = strings.NewReplacer("\x1b", "", "\u009b", "").Replace(s)
	}
	return s
}

func hinted(lower string, hints []string) bool {
	for _, h := range hints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	return false
}

type span struct{ start, end int }

// markerSpans returns where the complete [REDACTED:<kind>] markers in text
// are, in order.
func markerSpans(text string) []span {
	if !strings.Contains(text, marker) {
		return nil
	}
	var out []span
	for _, m := range markerRe.FindAllStringIndex(text, -1) {
		out = append(out, span{m[0], m[1]})
	}
	return out
}

// overlapsAny reports whether s overlaps any of spans, which are in order and
// do not overlap, so a binary search finds the one candidate. Text with many
// markers then costs no more per match.
func overlapsAny(spans []span, s span) bool {
	i := sort.Search(len(spans), func(i int) bool { return spans[i].end > s.start })
	return i < len(spans) && spans[i].start < s.end
}

// apply replaces every match of r in text and returns the count. missed
// reports that the search of a resuming rule stopped resuming, so its floor
// rule must look at the text itself.
func apply(text string, r rule, md mode) (out string, n int, missed bool) {
	marks := markerSpans(text)
	refs := opRefRe.FindAllStringIndex(text, -1)
	var spans []span
	pick := func(m []int, value func(string, []int, []span) (span, bool)) (span, bool) {
		s := span{m[2*r.group], m[2*r.group+1]}
		if s.start < 0 {
			return s, false
		}
		if value != nil {
			var ok bool
			if s, ok = value(text, m, marks); !ok {
				return s, false
			}
		}
		if containedBy(marks, s) || inRef(refs, s) {
			return s, false
		}
		if r.check != nil && !r.check(text[s.start:s.end]) {
			return s, false
		}
		if md.names && r.format && nameish(text[s.start:s.end]) {
			return s, false
		}
		return s, true
	}
	if !r.resume {
		for _, m := range r.re.FindAllStringSubmatchIndex(text, -1) {
			if s, ok := pick(m, r.value); ok {
				spans = append(spans, s)
			}
		}
		spans = dropOverlaps(spans)
		return replace(text, spans, r.kind), len(spans), false
	}
	// A match takes its value to the next whitespace or quote, but the value
	// picked can end sooner, as before "&refresh_token=" in a URL query, or
	// be rejected, as a placeholder before "&api_key=". The search then
	// resumes after the value picked, or at the start of the rejected one,
	// instead of after the whole match. Searching text again costs time, so
	// the bytes searched twice are bounded; past the bound the search goes on
	// after each whole match, and the floor rule looks at the text itself.
	//
	// A rejected match gets the floor's verdict on the spot: the value
	// the baseline rules would take at the same key (see floorValue). An
	// accepted value always covers that one.
	again := len(text) + 64<<10
	for pos := 0; pos < len(text); {
		m := r.re.FindStringSubmatchIndex(text[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		next := m[1]
		s, ok := pick(m, r.value)
		if !ok {
			if fm, has := floorMatch(text, m); has {
				s, ok = pick(fm, floorValue)
			}
		}
		if ok {
			spans = append(spans, s)
		}
		resume := m[2*r.group]
		if ok {
			resume = s.end
		}
		if resume > pos && resume < next {
			if next-resume > again {
				missed = true
			} else {
				again -= next - resume
				next = resume
			}
		}
		pos = max(next, pos+1)
	}
	// A value that runs to the end of its line can cover a later match.
	spans = dropOverlaps(spans)
	return replace(text, spans, r.kind), len(spans), missed
}

// floorMatch turns match m of a value rule (valueRe) into the match its
// floor rule (floorValueRe) has at the same key, if any: the value starts at
// the same place, and the baseline rules take 8 or more characters of it. The scan is
// bounded, far past any real secret.
func floorMatch(text string, m []int) ([]int, bool) {
	start := m[4]
	end, limit := start, min(len(text), start+maxFloorScan)
	for end < limit && strings.IndexByte(" \t\r\n\f\v\"'`,;&<>(){}[]", text[end]) < 0 {
		end++
	}
	if end-start < minValueLen {
		return nil, false
	}
	fm := append([]int(nil), m...)
	fm[1], fm[5] = end, end
	return fm, true
}

// containedBy reports whether s lies inside one of spans, which are in order
// and do not overlap.
func containedBy(spans []span, s span) bool {
	i := sort.Search(len(spans), func(i int) bool { return spans[i].start > s.start }) - 1
	return i >= 0 && s.end <= spans[i].end
}

// inRef reports whether s lies inside one of the op:// references refs, which
// are in order and do not overlap.
func inRef(refs [][]int, s span) bool {
	i := sort.Search(len(refs), func(i int) bool { return refs[i][0] > s.start }) - 1
	return i >= 0 && s.end <= refs[i][1]
}

// replace swaps each span, which must not overlap, for a marker of kind.
func replace(text string, spans []span, kind string) string {
	if len(spans) == 0 {
		return text
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for _, s := range spans {
		b.WriteString(text[last:s.start])
		b.WriteString(marker + kind + "]")
		last = s.end
	}
	b.WriteString(text[last:])
	return b.String()
}

// secretValue picks the text to replace after a secret-looking key matched
// with valueRe.
//
// When the key starts its line, as in a .env, YAML, INI or properties file,
// the value runs to the end of the line (see lineValue), so symbols and the
// spaces of a passphrase are part of it. A value of several words that reads
// as code or prose is judged by its first token instead, like a value inside
// a line.
//
// Inside a line, a value runs to the next whitespace or quote: symbols such
// as & ; ( or [ no longer end it, but punctuation at its very end does, and
// so does a bracket that closes one opened before the key, as in
// params(token: String). In a URL query (the key follows '?' or '&') it also
// ends before an '&' that starts another key=value pair, and in a connection
// string (the key follows ';') before such a ';', because a literal separator
// cannot be part of a value there.
//
// Code and references are kept, such as getToken(user), os.environ["X"],
// ${TOKEN}, $(cmd) and {{ .Values.token }}, and so are placeholders, op://
// references and values under 8 characters. A SHA or UUID here is taken: as
// the value of a secret-looking key it may well be the secret. A key written
// as in a shell or .env assignment, KEY=value, is not code, so a value there
// that starts like a reference (&x, *x), a regular expression (/x/) or a Ruby
// literal is judged as a value.
func secretValue(text string, m []int, marks []span) (span, bool) {
	start, end := m[4], m[5]
	var keyQuote byte
	if m[3] > m[2] {
		keyQuote = text[m[2]]
	}
	sep, shell := keySeparator(text, m)
	var s span
	whole := false
	if q := text[start-1]; q != '"' && q != '\'' && atLineStart(text, m[0], keyQuote) {
		var ok bool
		if s, whole, ok = lineValue(text, &start, sep, shell); !ok {
			return span{}, false
		}
		if !whole && start != m[4] {
			// The value moved past a YAML anchor or tag; take its first token.
			end = len(text)
			if i := strings.IndexAny(text[start:], " \t\r\n\f\v\"'`"); i >= 0 {
				end = start + i
			}
		}
	}
	if !whole {
		if isCode(text[start:inlineLimit(text, start)], true, shell) {
			return span{}, false
		}
		s = span{start, inlineEnd(text, start, end, inlineContextAt(text, m[0]), marks)}
	}
	v := text[s.start:s.end]
	if len(v) < minValueLen || !looksSecret(v) || covered(marks, s) || markerStart(v) {
		return span{}, false
	}
	return s, true
}

// inlineContextAt describes where the value of a key at text[key] sits.
func inlineContextAt(text string, key int) inlineContext {
	c := inlineContext{closers: enclosing(text, key)}
	if key > 0 {
		switch text[key-1] {
		case '?', '&':
			c.sep = '&'
		case ';':
			c.sep = ';'
		}
	}
	// In a URL's authority, as in https://x-access-token:...@host/, '@' and
	// '/' end the value.
	c.url = key >= 3 && text[key-3:key] == "://"
	return c
}

// keySeparator returns the separator after the key of match m, ':' or '=',
// and whether the key is written as in a shell or .env assignment: unquoted
// and directly followed by a single '='.
func keySeparator(text string, m []int) (sep byte, shell bool) {
	i := m[3]
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	if i >= len(text) {
		return 0, false
	}
	sep = text[i]
	single := i+1 >= len(text) || text[i+1] != '=' && text[i+1] != '>'
	return sep, sep == '=' && single && i == m[3] && m[2] == m[3]
}

// floorValue picks the value a floor rule replaces: the value the baseline
// rules replace, which the rules above left alone. The baseline rules take the
// 8 or more characters before whitespace, a quote or one of
// , ; & < > ( ) { } [ ], unless a '(' follows. Such a value is redacted with
// the rest of its token (see inlineEnd), unless it is one of the shapes of
// code that the baseline rules redact by mistake and the rules above keep on
// purpose (plainCode). So a password that only looks like code, such as
// Summer2024[!], *Hunter2024x or /Kx9mP2vL/abc after KEY=, is never kept whole
// where the baseline rules redact it.
// A comparison, a spaced "==", is code, whatever follows it.
func floorValue(text string, m []int, marks []span) (span, bool) {
	s := span{m[4], m[5]}
	if s.end < len(text) && text[s.end] == '(' {
		return span{}, false // a call, as the baseline rules have it
	}
	if sep := text[m[3]:m[4]]; strings.Contains(sep, "==") && strings.ContainsAny(sep, " \t") {
		return span{}, false // a comparison, as in "tok.ScanToken == scanner.EOF {"
	}
	// The rest of the token is looked at a bounded way past the baseline
	// value, so many values in one long token cost linear time.
	tokenEnd := min(len(text), s.end+maxCodeScan)
	if i := strings.IndexAny(text[s.start:tokenEnd], " \t\r\n\f\v\"'`"); i >= 0 {
		tokenEnd = s.start + i
	}
	c := inlineContextAt(text, m[0])
	if c.url {
		if i := strings.IndexAny(text[s.start:s.end], "@/"); i >= 0 {
			s.end = s.start + i
		}
	}
	v := text[s.start:s.end]
	if len(v) < minValueLen || !looksSecret(v) || markerStart(v) {
		return span{}, false
	}
	code, clean := plainCode(text, m, s)
	if code {
		return span{}, false
	}
	// The baseline rules leave an index such as ["Token"] or [1:] after the
	// name they take; so does the floor. Anything else in the token is taken with it.
	if !c.url && !clean {
		s.end = max(s.end, inlineEnd(text, s.start, tokenEnd, c, marks))
	}
	return s, true
}

// plainCode reports whether the value the baseline rules would replace at s, in match
// m, is code that the rules above keep on purpose (code):
//   - a type annotation or a chained assignment, as in "api_key:
//     Optional[str] = ...", "token: str | None" or "a = refresh_token = ...";
//   - an index, call or literal on a member path or an instance variable,
//     as in stmt.Token[1:], dec.tokenStack[len(...)-1] or @tokens[dst], or a
//     block after one, as in tokens.map { |t| ... };
//   - an index by a quoted name, as in credentials["Token"];
//   - after ':' or a spaced '=', a literal or index on a name of two or more
//     words, as in wrappedSigner{key};
//   - after ':', a YAML alias made of words, as in *default_password;
//   - after a spaced separator, a regular expression literal.
//
// Each holds only when its brackets close on the line around an expression,
// so a generated password that merely starts like one is not kept. clean
// reports that the value is followed by such an index, which the baseline
// rules leave in place.
func plainCode(text string, m []int, s span) (code, clean bool) {
	sep, shell := keySeparator(text, m)
	// The line is looked at a bounded way past the value, so many values on
	// one long line cost linear time.
	lineEnd, whole := min(len(text), s.end+maxCodeScan), false
	if i := strings.IndexAny(text[s.start:lineEnd], "\r\n"); i >= 0 {
		lineEnd, whole = s.start+i, true
	}
	whole = whole || lineEnd == len(text)
	v := text[s.start:s.end]
	rest := text[s.end:lineEnd]
	if !shell {
		// A name that is assigned to in turn, as in "token = refresh_token =
		// value", reads as a type annotation too.
		if t := typeHead.FindStringSubmatchIndex(text[s.start:lineEnd]); t != nil {
			line := text[s.start:lineEnd]
			if line[t[1]-1] == ' ' || line[t[1]-1] == '\t' || t[3] > t[2] {
				return true, false
			}
		}
	}
	path := memberPath(strings.TrimLeft(v, "@$")) || instanceVariable(v)
	switch {
	case rest != "" && (rest[0] == '[' || rest[0] == '{'):
		end, ok := closingBracket(rest)
		if !ok || !exprLike(rest[1:end-1]) || !codeAfter(rest[end:]) {
			return false, false
		}
		if rest[0] == '[' && quotedName(rest[1:end-1]) {
			return true, true
		}
		return path || !shell && memberSegment.MatchString(v) && wordLike(v, 2), true
	case path && strings.HasPrefix(strings.TrimLeft(rest, " \t"), "{"):
		return true, false // a block, as in tokens.map { |t| t.name }
	case v[0] == '*':
		return sep == ':' && aliasName(v[1:]) && whole && strings.TrimSpace(rest) == "", false
	case v[0] == '/':
		return !shell && regexLiteral(v) && (rest == "" || strings.IndexByte(" \t;,\"'`\\", rest[0]) >= 0), false
	}
	return false, false
}

// quotedName reports whether s is a quoted name such as "Token" or
// 'access_token', the key of a map or hash.
func quotedName(s string) bool {
	if len(s) < 3 || s[0] != s[len(s)-1] || s[0] != '"' && s[0] != '\'' {
		return false
	}
	name := s[1 : len(s)-1]
	return strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.") == "" && wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1)
}

// closingBracket returns the index just after the bracket that closes s[0],
// on the same line, skipping quoted strings.
func closingBracket(s string) (int, bool) {
	end, closed, _ := closeBracket(s, 0, false)
	return end, closed
}

// exprLike reports whether s, the inside of brackets, reads as an expression
// rather than part of a generated password: it has a letter or digit and none
// of ; ~ # or a backtick, as in 1:, len(stack)-1, key or /^password=(.+)/, 1.
func exprLike(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 128 && isAlnum(byte(r)) }) >= 0 && !strings.ContainsAny(s, ";~#`")
}

// codeAfter reports whether what follows a closing bracket goes on as code:
// nothing, whitespace, one of , ; ) ] } . [ (, or the quote, perhaps escaped,
// that closes a string around the code.
func codeAfter(s string) bool {
	return s == "" || strings.IndexByte(" \t,;)]}.[(\"'`\\", s[0]) >= 0
}

// memberPath reports whether v is a path of names such as stmt.Token,
// dec.tokenStack, Config::KEYS or ::Utils.name, perhaps ending in ? or !.
// Each name is made of words, is one letter, or is a short lower-case name
// such as os or cfg.
func memberPath(v string) bool {
	v = strings.TrimPrefix(v, "::")
	v = strings.TrimRight(v, "?!")
	parts := strings.FieldsFunc(strings.ReplaceAll(v, "::", "."), func(r rune) bool { return r == '.' })
	if len(parts) < 2 || strings.Contains(v, "..") || strings.HasSuffix(v, ".") {
		return false
	}
	for _, p := range parts {
		if !memberSegment.MatchString(p) || !(wordLike(p, 1) || len(p) == 1 || len(p) <= 4 && strings.Trim(p, "abcdefghijklmnopqrstuvwxyz") == "") {
			return false
		}
	}
	return true
}

// instanceVariable reports whether v is a Ruby instance or class variable or
// a global made of words, such as @symlink_tokens.
func instanceVariable(v string) bool {
	name := strings.TrimLeft(v, "@$")
	return len(v)-len(name) <= 2 && name != v && memberSegment.MatchString(name) && wordLike(name, 1)
}

// aliasName reports whether name, after a YAML '*', reads as an anchor's
// name: two or more words, as in default_password, or one lower-case word, as
// in defaults. A password such as *Hmyhybbz82 is neither.
func aliasName(name string) bool {
	if !memberSegment.MatchString(strings.NewReplacer("-", "_", ".", "_").Replace(name)) {
		return false
	}
	return wordLike(name, 2) || len(name) >= 3 && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz") == ""
}

// regexLiteral reports whether v is a whole regular expression literal with
// valid flags and at least one metacharacter, such as /^v\d+$/i.
func regexLiteral(v string) bool {
	m := regexHead.FindStringIndex(v)
	if m == nil || m[1] != len(v) {
		return false
	}
	body := v[1:strings.LastIndexByte(v, '/')]
	flags := v[strings.LastIndexByte(v, '/')+1:]
	seen := map[rune]bool{}
	for _, f := range flags {
		if seen[f] {
			return false
		}
		seen[f] = true
	}
	return regexFlags.MatchString(flags) && strings.ContainsAny(body, `\^$|*+?`)
}

// wordLike reports whether name is made of words, as names in code are. It is
// split at '_', '-' and '.', at digits, and where the case changes (tokenStack
// gives token and Stack, AWSSecrets gives AWS and Secrets). Every part of
// letters must have at least three of them, or be an acronym of at least two
// capitals, and there must be at least min such parts. Digits may only follow
// a part, at most four of them, as in Base64 or Bug4Secret. Generated passwords mix
// cases at random, which leaves parts of one or two letters, as in Zx8cVb7N.
func wordLike(name string, min int) bool {
	words := 0
	for i := 0; i < len(name); {
		c := name[i]
		switch {
		case c == '_' || c == '-' || c == '.':
			i++
			continue
		case c >= '0' && c <= '9':
			j := i
			for j < len(name) && name[j] >= '0' && name[j] <= '9' {
				j++
			}
			if words == 0 || j-i > 4 {
				return false
			}
			i = j
			continue
		case !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'):
			return false
		}
		j := i
		if c >= 'A' && c <= 'Z' {
			k := i
			for k < len(name) && name[k] >= 'A' && name[k] <= 'Z' {
				k++
			}
			if k < len(name) && name[k] >= 'a' && name[k] <= 'z' {
				k-- // the last capital starts the next word, as in AWSSecrets
			}
			if k-i >= 2 {
				words++
				i = k
				continue
			}
			j = i + 1
		}
		for j < len(name) && name[j] >= 'a' && name[j] <= 'z' {
			j++
		}
		if j-i < 3 {
			return false
		}
		words++
		i = j
	}
	return words >= min
}

// markerStart reports whether v is the start of a marker and nothing more,
// as left when redacted text is cut to a length. It holds nothing to redact,
// and redacting it again would lengthen text that was cut to fit.
func markerStart(v string) bool {
	if len(v) <= len(marker) {
		return strings.HasPrefix(marker, v)
	}
	kind := v[len(marker):]
	return strings.HasPrefix(v, marker) && len(kind) <= 32 && strings.Trim(kind, "abcdefghijklmnopqrstuvwxyz_") == ""
}

// atLineStart reports whether a key at text[i] starts its line, after
// optional indentation and "export ". A quote may open the key only when
// keyQuote, the quote after the key, closes it, as in "password": value; a
// quote that opens a string literal such as "password = x", does not count.
func atLineStart(text string, i int, keyQuote byte) bool {
	if i > 0 && (text[i-1] == '"' || text[i-1] == '\'') {
		if text[i-1] != keyQuote {
			return false
		}
		i--
	}
	j := i
	for j > 0 && (text[j-1] == ' ' || text[j-1] == '\t') {
		j--
	}
	if j < i && j >= 6 && text[j-6:j] == "export" {
		k := j - 6
		for k > 0 && (text[k-1] == ' ' || text[k-1] == '\t') {
			k--
		}
		if k == 0 || text[k-1] == '\n' {
			return true
		}
	}
	return j == 0 || text[j-1] == '\n'
}

// lineValue returns the value of a config line that starts at text[*start],
// moving *start past a YAML anchor or tag after a ':' separator. The value
// ends before a trailing comment, whitespace and ';' or ','. After a type
// annotation, as in "api_key: str = ...", the assigned value is taken
// instead. whole is false when the value has several words and is not a
// passphrase, as in 64 * 1024, objdir + "x" or nil if uri.password: it is
// then judged by its first token like a value inside a line. ok is false
// when the value is a comment, code or a type with no value. shell says the
// key is written as KEY=value (see keySeparator).
func lineValue(text string, startp *int, sep byte, shell bool) (s span, whole, ok bool) {
	start := *startp
	code, end := lineValueEnd(text, start)
	if sep != ':' {
		// Anchors and tags are YAML's, which separates with ':'. After '=',
		// as in PGPASSWORD=!Summer2024x, the '!' or '&' is the value's own.
	} else if p := yamlProperties.FindStringIndex(text[start:end]); p != nil {
		start += p[1]
		*startp = start
	}
	v := text[start:end]
	if v == "" {
		return span{}, false, false
	}
	if t := typeHead.FindStringSubmatchIndex(v); t != nil {
		switch {
		case v[t[1]-1] == ' ' || v[t[1]-1] == '\t':
			s, ok = assigned(text, start+t[1], end) // key: Type = value
			return s, true, ok
		case t[3] > t[2]:
			return span{}, false, false // key: A | B, a union type and no value
		}
	}
	// The trailing ',' or ';' that the value leaves out still tells code, as
	// in a call that goes on to the next line.
	if isCode(text[start:code], false, shell) {
		return span{}, false, false
	}
	if strings.ContainsAny(v, " \t") && !passphrase(v) {
		return span{}, false, true
	}
	return span{start, end}, true, true
}

// lineValueEnd returns where the value of a config line that starts at
// text[start] ends: before a trailing comment (" #" or " //" followed by a
// space or the line end), trailing whitespace and a trailing ';' or ','. code
// is the end before the comment and whitespace only. A value that is itself a
// comment, '#' after whitespace and followed by whitespace or the line end,
// ends where it starts; "#Xk9m..." is a value, as in a .properties or INI
// file, where '#' starts a comment only at the start of a line.
func lineValueEnd(text string, start int) (code, end int) {
	end = len(text)
	if i := strings.IndexAny(text[start:], "\r\n"); i >= 0 {
		end = start + i
	}
	if text[start] == '#' && (text[start-1] == ' ' || text[start-1] == '\t') && (start+1 == end || text[start+1] == ' ' || text[start+1] == '\t') {
		return start, start
	}
	for i := start + 1; i < end; i++ {
		if text[i-1] != ' ' && text[i-1] != '\t' {
			continue
		}
		n := 0
		switch {
		case text[i] == '#':
			n = 1
		case strings.HasPrefix(text[i:end], "//"):
			n = 2
		}
		if n > 0 && (i+n == end || text[i+n] == ' ' || text[i+n] == '\t') {
			end = i
			break
		}
	}
	for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
		end--
	}
	code = end
	for end > start && strings.IndexByte(" \t;,", text[end-1]) >= 0 {
		end--
	}
	return code, end
}

// passphrase reports whether a value of several words reads as a passphrase
// rather than code or prose: every word is letters and digits, with ' or -
// inside, the first is not a literal such as nil, None or a number, and at
// most one is a common function word, as in "correct horse battery staple"
// but not "still not working after the reset".
func passphrase(v string) bool {
	words := strings.Fields(v)
	if literal.MatchString(words[0]) {
		return false
	}
	common := 0
	for _, w := range words {
		if !isAlnum(w[0]) || !isAlnum(w[len(w)-1]) || strings.Trim(w, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'-") != "" {
			return false
		}
		if functionWords[strings.ToLower(w)] {
			common++
		}
	}
	return common < 2
}

// functionWords are common English words that mark a value of several words
// as prose rather than a passphrase.
var functionWords = func() map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.Fields(`a an the and or but not no nor is are was were be been am it its it's this that
		these those to of in on at by for from with after before into over under about as than then so if
		when while what why how who which there here i i'm me my we our you your he she they them their
		do does did don't doesn't didn't have has had can can't could will won't would should still just
		also only again yet now please`) {
		words[w] = true
	}
	return words
}()

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// assigned returns the value assigned after a type annotation, at
// text[start:end]: the text inside the quotes of a quoted string, or else the
// rest of the line unless it is code.
func assigned(text string, start, end int) (span, bool) {
	v := text[start:end]
	if v == "" {
		return span{}, false
	}
	if q := v[0]; q == '"' || q == '\'' {
		if i := strings.IndexByte(v[1:], q); i >= 0 {
			return span{start + 1, start + 1 + i}, true
		}
		return span{start + 1, end}, true
	}
	if isCode(v, false, false) {
		return span{}, false
	}
	return span{start, end}, true
}

// inlineLimit returns where the scan for code in a value inside a line stops:
// at the line's end, at most maxCodeScan bytes on.
func inlineLimit(text string, start int) int {
	end := min(len(text), start+maxCodeScan)
	if i := strings.IndexAny(text[start:end], "\r\n"); i >= 0 {
		end = start + i
	}
	return end
}

// valueStops end the first part of a value inside a line.
const valueStops = ",;&<>(){}[]"

// inlineContext describes where a value inside a line sits.
type inlineContext struct {
	sep     byte   // '&' in a URL query, ';' in a connection string
	closers string // brackets that close what the line opened before the key, innermost last
	url     bool   // the key is in a URL's authority
}

// inlineEnd returns where a value inside a line ends, given its token
// text[start:end]. The first part of the value ends at a symbol in
// valueStops; markers are stepped over whole. When more than symbols follow,
// the value runs on to the end of the token. It stops early at a bracket that
// closes one opened before the key, as the ')' in params(token: String), at
// a c.sep that starts another key=value pair, at an '&', ';' or ',' that
// starts another secret-looking key, and in a URL at '@' or '/'.
func inlineEnd(text string, start, end int, c inlineContext, marks []span) int {
	if c.url {
		if i := strings.IndexAny(text[start:end], "@/"); i >= 0 {
			end = start + i
		}
	}
	closer := byte(0)
	if c.closers != "" {
		closer = c.closers[len(c.closers)-1]
	}
	head := start
	for head < end {
		if m, ok := markerAt(marks, head); ok {
			head = m.end
			continue
		}
		if strings.IndexByte(valueStops, text[head]) >= 0 {
			break
		}
		head++
	}
	if head >= end || strings.TrimLeft(text[head:end], valueStops) == "" || text[head] == closer {
		return min(head, end)
	}
	depth := 0
	for i := head; i < end; i++ {
		switch ch := text[i]; {
		case ch == '(' || ch == '[' || ch == '{':
			depth++
		case ch == ')' || ch == ']' || ch == '}':
			if depth > 0 {
				depth--
			} else if ch == closer {
				return i
			}
		case ch == c.sep && c.sep != 0 && nextPair.MatchString(text[i+1:end]):
			return i
		case (ch == '&' || ch == ';' || ch == ',') && secretKeyAt(text, i+1):
			return i
		}
	}
	return end
}

// secretKeyAt reports whether a secret-looking key and its separator start
// at text[i]. The key and separator may run past the value's token, as in
// "...&access_key = value", so the check reads on, a bounded way.
func secretKeyAt(text string, i int) bool {
	return nextSecretKey.MatchString(text[i:min(len(text), i+maxCodeScan)])
}

// enclosing returns the brackets that would close those the key's line opens
// before the key, innermost last. It looks back at most maxCodeScan bytes.
func enclosing(text string, key int) string {
	from := max(0, key-maxCodeScan)
	if i := strings.LastIndexAny(text[from:key], "\r\n"); i >= 0 {
		from += i + 1
	}
	var want []byte
	for i := from; i < key; i++ {
		switch c := text[i]; c {
		case '(':
			want = append(want, ')')
		case '[':
			want = append(want, ']')
		case '{':
			want = append(want, '}')
		case ')', ']', '}':
			if n := len(want); n > 0 && want[n-1] == c {
				want = want[:n-1]
			}
		}
	}
	return string(want)
}

// markerAt returns the marker that starts at text[i], if any.
func markerAt(marks []span, i int) (span, bool) {
	k := sort.Search(len(marks), func(k int) bool { return marks[k].start >= i })
	if k < len(marks) && marks[k].start == i {
		return marks[k], true
	}
	return span{}, false
}

// covered reports whether markers cover all of s, leaving nothing to redact.
// marks are in order and do not overlap.
func covered(marks []span, s span) bool {
	at := s.start
	for _, m := range marks[sort.Search(len(marks), func(i int) bool { return marks[i].end > at }):] {
		if m.start > at {
			break
		}
		at = m.end
	}
	return at >= s.end
}

// isCode reports whether v, the value after a secret-looking key, is code or
// a reference rather than a literal: a shell command substitution, a ${...}
// variable or expression, a reference such as &config, or a call, index or
// literal such as getToken(user), os.environ["X"], []byte{...},
// {{ .Values.x }} or [1, 2] whose closing bracket is followed by nothing or
// by more code. A bracket still open where v ends is code when it holds an
// argument list (", ") or v ends with an opening bracket or a comma, as in a
// call that goes on to the next line. For a value inside a line, v runs on to
// the end of the line, and whitespace inside the brackets must be where an
// argument list puts it. After a shell-style KEY= (shell), a value is never a
// reference, a regular expression or a Ruby literal: there the '&', '*', '/'
// or '%' it starts with is part of a password, and a call, index or literal
// needs a name made of words (see wordyName), as in get_token(). So does a
// bracket left open for the next line anywhere: getToken(user, goes on,
// while q6fA{ZU|si, is a password.
func isCode(v string, inline, shell bool) bool {
	open := -1
	named := true // the bracket at open follows a name that reads as code
	switch {
	case strings.HasPrefix(v, "$("):
		return true
	case strings.HasPrefix(v, "${"), strings.HasPrefix(v, "#{"):
		open = 1 // shell, template or Ruby interpolation
	default:
		if m := codeHead.FindStringIndex(v); m != nil && !(shell && strings.IndexByte("&*", v[0]) >= 0) {
			open = m[1] - 1
			named = wordyName(v[:open])
			if shell && !named {
				return false
			}
		} else if v != "" && strings.IndexByte("([{", v[0]) >= 0 {
			open = 0 // a bracketed value, or a shell array such as KEYS=(a b)
		} else if shell {
			// Only interpolation, calls, indexes and literals are code here.
		} else if m := percentHead.FindStringIndex(v); m != nil {
			end, ok := percentEnd(v, m[1]-1)
			return ok && codeEnd(v, end)
		} else if m := regexHead.FindStringIndex(v); m != nil {
			return codeEnd(v, m[1])
		} else if m := refHead.FindStringIndex(v); m != nil {
			// Only a whole value: &defaults hunter22 is a YAML anchor and a password.
			return m[1] == len(v) || strings.IndexByte(";,)]}", v[m[1]]) >= 0
		}
	}
	if open < 0 {
		return false
	}
	end, closed, args := closeBracket(v, open, inline)
	if closed {
		return codeEnd(v, end)
	}
	if !named {
		return false
	}
	continued := "([{,"
	if shell {
		// Only a call goes on to the next line after KEY=, as in Python's
		// token=get_token(; a value ending in '{', '[' or ',' is a password.
		continued = "("
	}
	return args || strings.IndexByte(continued, v[len(v)-1]) >= 0 && end == len(v)
}

// wordyName reports whether head, the code before an opening bracket that
// codeHead matched, ends in names that read as code: each name of its chain
// is made of words (see wordLike) or is short, as in getToken,
// wrappedSigner, contents.adjust, []struct, x509.Parse or f. Generated
// passwords such as Djye0{...} or q6fA{...} have neither.
func wordyName(head string) bool {
	head = markerRe.ReplaceAllString(head, "x") // a marker that replaced a name is a name
	head = strings.TrimRight(head, " \t?!")
	if i := strings.LastIndexAny(head, " \t]&*@"); i >= 0 {
		head = head[i+1:] // after await, new, [256], map[K], & or @
	}
	for _, name := range strings.FieldsFunc(head, func(r rune) bool { return r == '.' || r == ':' || r == '-' || r == '>' || r == '?' }) {
		name = strings.TrimLeft(name, "$")
		if name != "" && !shortName(name) && !wordLike(name, 1) {
			return false
		}
	}
	return true
}

// shortName reports a name of at most four lower-case letters and digits
// that starts with a letter, such as s, os, cfg or x509.
func shortName(name string) bool {
	return len(name) <= 4 && name != "" && name[0] >= 'a' && name[0] <= 'z' && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789") == ""
}

// closeBracket returns the index just after the bracket that closes v[open],
// and true. Quoted strings are skipped. When inline is set, whitespace may
// appear only where an argument list puts it. Without a match it returns how
// far it got, and args reports that it reached the end of v with the bracket
// still open after an argument list's ", ", or in a string that starts an
// argument and goes on to the next line.
func closeBracket(v string, open int, inline bool) (end int, closed, args bool) {
	var want []byte
	comma := false
	for i := open; i < len(v); i++ {
		switch c := v[i]; c {
		case '(':
			want = append(want, ')')
		case '[':
			want = append(want, ']')
		case '{':
			want = append(want, '}')
		case ')', ']', '}':
			if len(want) == 0 || want[len(want)-1] != c {
				return i, false, false
			}
			want = want[:len(want)-1]
			if len(want) == 0 {
				return i + 1, true, false
			}
		case '"', '\'', '`':
			q := v[i : i+1]
			if strings.HasPrefix(v[i:], q+q+q) {
				q += q + q // a Python triple-quoted string
			}
			j := strings.Index(v[i+len(q):], q)
			if j < 0 {
				// The string goes on past the line; it is an argument when it
				// opens right after a bracket or a comma (allowing r"..." and
				// the like).
				before := v[:i]
				for k := 0; k < 2 && strings.IndexByte("rRbBuUfF", before[len(before)-1]) >= 0; k++ {
					before = before[:len(before)-1]
				}
				before = strings.TrimRight(before, " \t")
				return len(v), false, before != "" && strings.IndexByte("([{,", before[len(before)-1]) >= 0
			}
			i += len(q) + j + len(q) - 1
		case ' ', '\t':
			if inline && !argSpace(v, i) {
				return i, false, false
			}
			comma = comma || v[i-1] == ','
		}
	}
	return len(v), false, comma
}

// argSpace reports whether the whitespace at v[i] sits where an argument
// list or a literal puts it: after an opening bracket, a comma or a colon, or
// before a closing bracket.
func argSpace(v string, i int) bool {
	before := strings.TrimRight(v[:i], " \t")
	after := strings.TrimLeft(v[i:], " \t")
	return before != "" && strings.IndexByte(",:([{", before[len(before)-1]) >= 0 ||
		after != "" && strings.IndexByte(")]}", after[0]) >= 0
}

// codeEnd reports whether what follows a closing bracket at v[end] ends the
// value or continues code, rather than more of a literal: whitespace, a
// bracket, the quote that closes a string around the code, a method or
// property (.name or ?.name), a ';', ',', ':' or '!' that ends a statement
// or an argument, or the '&' before another pair of a URL query.
func codeEnd(v string, end int) bool {
	if end == len(v) {
		return true
	}
	next := byte(0)
	if end+1 < len(v) {
		next = v[end+1]
	}
	switch c := v[end]; {
	case strings.IndexByte(closers, c) >= 0 || c == '(' || c == '[':
		return true
	case c == '.':
		return next == '_' || next == '$' || next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z'
	case c == '?':
		return next == '.'
	case strings.IndexByte(";,:!", c) >= 0:
		return next == 0 || strings.IndexByte(closers+";,", next) >= 0
	case c == '&':
		return next == '.' || nextPair.MatchString(v[end+1:]) // Ruby's &.name, or a URL query
	}
	return false
}

// percentEnd returns the index just after the delimiter that closes the Ruby
// literal starting v (such as %r{...}n), counting only nested delimiters of
// the same kind and skipping escaped characters, as Ruby does. Flags after a
// regular expression are included.
func percentEnd(v string, open int) (int, bool) {
	o, c := v[open], byte(0)
	switch o {
	case '(':
		c = ')'
	case '[':
		c = ']'
	case '{':
		c = '}'
	case '<':
		c = '>'
	}
	depth := 0
	for i := open; i < len(v); i++ {
		switch v[i] {
		case '\\':
			i++
		case o:
			depth++
		case c:
			if depth--; depth == 0 {
				i++
				for i < len(v) && v[i] >= 'a' && v[i] <= 'z' {
					i++
				}
				return i, true
			}
		case '\r', '\n':
			return 0, false
		}
	}
	return 0, false
}

// closers end code: whitespace, a closing bracket, or the quote, perhaps
// escaped, that closes a string around the code.
const closers = " \t)]}\"'`\\"

// redactEntropy replaces high-entropy tokens of 20 or more characters that
// appear within a few words after a word such as password, secret or token.
func redactEntropy(text, lower string) (string, int) {
	if !hinted(lower, []string{"passw", "passphrase", "secret", "token", "key", "credential", "bearer"}) {
		return text, 0
	}
	marks := markerSpans(text)
	seen := map[span]bool{}
	var spans []span
	for _, t := range entropyTrigger.FindAllStringIndex(text, -1) {
		if overlapsAny(marks, span{t[0], t[1]}) {
			continue
		}
		win := window(text, t[1])
		for _, m := range entropyToken.FindAllStringIndex(text[t[1]:win], -1) {
			s := span{t[1] + m[0], t[1] + m[1]}
			if seen[s] || overlapsAny(marks, s) {
				continue
			}
			seen[s] = true
			if v := text[s.start:s.end]; highEntropy(v) {
				spans = append(spans, s)
			}
		}
	}
	spans = dropOverlaps(spans)
	return replace(text, spans, "high_entropy"), len(spans)
}

// window returns the end of the range checked after a trigger word: at most
// entropyWords words and entropyMaxBytes bytes.
func window(text string, from int) int {
	end := min(len(text), from+entropyMaxBytes)
	words, inWord := 0, false
	for i := from; i < end; i++ {
		space := text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r'
		if !space && !inWord {
			words++
			if words > entropyWords {
				return i
			}
		}
		inWord = !space
	}
	return end
}

func dropOverlaps(spans []span) []span {
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	out := spans[:0]
	for _, s := range spans {
		if len(out) > 0 && s.start < out[len(out)-1].end {
			continue
		}
		out = append(out, s)
	}
	return out
}

// highEntropy reports whether v looks like a random secret rather than a
// word, a path or an identifier.
func highEntropy(v string) bool {
	if len(v) < minEntropyLen || keep(v) {
		return false
	}
	if strings.HasPrefix(v, "//") || (strings.Contains(v, "/") && strings.Contains(v, ".")) {
		return false // a path or URL
	}
	if wordy(v) {
		return false // a name such as claude-opus-5-5-20261028
	}
	return entropy(v) >= minEntropyBits
}

// wordy reports whether v is made of parts separated by '-', '_' or '.',
// each of them only letters of one case or only digits, the shape of model
// names, versions and identifiers rather than random secrets.
func wordy(v string) bool {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		lower := strings.Trim(p, "abcdefghijklmnopqrstuvwxyz") == ""
		upper := strings.Trim(p, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
		if !lower && !upper && !allDigits(p) {
			return false
		}
	}
	return true
}

// nameish reports whether v, text a provider token format matched, reads as
// a name rather than a token: every part between '-', '_' and '.' is a word
// in lower case or with a capital first, perhaps ending in digits as in v2
// or oauth2, or capitals, or digits, as in sk-learn-pipeline-v2,
// sk-learn-Upgrade or ops_runbooks_2026. Generated tokens mix cases and
// digits within their parts.
func nameish(v string) bool {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		word := strings.TrimRight(p, "0123456789")
		if word != "" && word[0] >= 'A' && word[0] <= 'Z' {
			word = word[1:] // a capital first
		}
		if strings.Trim(word, "abcdefghijklmnopqrstuvwxyz") != "" && strings.Trim(p, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			return false
		}
	}
	return true
}

// entropy returns the Shannon entropy of s in bits per byte.
func entropy(s string) float64 {
	var freq [256]int
	for i := 0; i < len(s); i++ {
		freq[s[i]]++
	}
	n := float64(len(s))
	h := 0.0
	for _, f := range freq {
		if f > 0 {
			p := float64(f) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}

// keep reports values that must survive redaction in ordinary text: git
// SHAs, UUIDs and 1Password op:// references.
func keep(v string) bool {
	return opRef(v) || uuidRe.MatchString(v) || shaRe.MatchString(v)
}

// opRef reports a 1Password op:// reference. It is the only value kept after
// a secret-looking key: a SHA or UUID there, as in password=<uuid>, may well
// be the secret.
func opRef(v string) bool { return strings.HasPrefix(v, "op://") }

// looksSecret reports whether the value after a secret-looking key is a
// plausible secret, rather than a placeholder, a variable or a code reference.
func looksSecret(v string) bool {
	return !opRef(v) && !looksLikePlaceholder(v)
}

// looksLikePlaceholder reports an empty value, a variable such as $TOKEN or
// os.environ.TOKEN, a placeholder such as <your-api-key>, or the literal
// undefined.
func looksLikePlaceholder(v string) bool {
	return v == "" || v == "undefined" || variableRef.MatchString(v) || placeholder.MatchString(v)
}

func allDigits(s string) bool { return strings.Trim(s, "0123456789") == "" }

func hasDigit(s string) bool { return strings.ContainsAny(s, "0123456789") }

func hasUpper(s string) bool { return strings.ContainsAny(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") }

func hasLower(s string) bool { return strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyz") }

func hasLetter(s string) bool { return hasUpper(s) || hasLower(s) }

func isJWT(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for i, part := range parts[:2] {
		data, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			return false
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(data, &object) != nil || object == nil {
			return false
		}
		if i == 0 && object["alg"] == nil {
			return false
		}
	}
	return true
}
