// Package baseline holds eva-md's original redaction rules, kept unchanged.
// redact.Redact removes everything these rules or the current rules remove,
// so a change to the current rules can only remove more, never keep a secret
// these rules remove. Don't change the rules here; change internal/redact.
package baseline

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Version identifies the rule set. Raise it whenever a rule changes.
const Version = 1

// marker starts every replacement. Only a complete marker (markerRe) counts
// as earlier redaction, so text that merely starts like one shields nothing.
const marker = "[REDACTED:"

// maxPasses bounds the passes Redact makes. Each pass that changes anything
// replaces at least 8 bytes of unredacted text, so the loop ends long before.
const maxPasses = 16

// Entropy rule settings.
const (
	minEntropyLen   = 20  // shortest token the entropy rule considers
	minEntropyBits  = 3.5 // Shannon entropy per character, in bits
	entropyWords    = 5   // how many words after a trigger word are checked
	entropyMaxBytes = 256 // how far after a trigger word is checked
)

type rule struct {
	kind  string
	re    *regexp.Regexp
	group int               // submatch to replace; 0 replaces the whole match
	hints []string          // lower-case literals; the rule runs only when one appears
	check func(string) bool // optional: the replaced text must pass it
	// skipBefore lists bytes that, right after a match, mark it as code
	// rather than a secret, such as the '(' of a function call.
	skipBefore string
}

// The value after a secret-looking key: 8 or more characters, stopping at
// quotes, spaces and the punctuation that ends a value in code or config.
const valueRe = `["']?[ \t]*[:=][=>]?[ \t]*["']?([^\s"'\x60,;&<>(){}\[\]]{8,})`

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
		kind:  "anthropic_key",
		re:    regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`),
		hints: []string{"sk-ant-"},
	},
	{
		kind:  "openai_key",
		re:    regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_\-]{20,}`),
		hints: []string{"sk-"},
	},
	{
		kind:  "onepassword_token",
		re:    regexp.MustCompile(`\bops_[A-Za-z0-9_\-]{20,}`),
		hints: []string{"ops_"},
	},
	{
		kind:  "github_token",
		re:    regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,255}\b|\bgithub_pat_[A-Za-z0-9_]{22,255}\b`),
		hints: []string{"gh", "github_pat_"},
	},
	{
		kind:  "gitlab_token",
		re:    regexp.MustCompile(`\b(?:glpat|gldt|glrt|glptt|gloas)-[A-Za-z0-9_\-]{20,}`),
		hints: []string{"gl"},
	},
	{
		kind:  "slack_webhook",
		re:    regexp.MustCompile(`(?i)https://hooks\.slack\.com/(?:services|workflows|triggers)/[A-Za-z0-9_/\-]+`),
		hints: []string{"hooks.slack.com"},
	},
	{
		kind:  "slack_token",
		re:    regexp.MustCompile(`\bxox[abposre]-[A-Za-z0-9\-]{10,}|\bxapp-[0-9]-[A-Za-z0-9\-]{10,}`),
		hints: []string{"xox", "xapp-"},
	},
	{
		kind:  "aws_key",
		re:    regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`),
		hints: []string{"akia", "asia", "abia", "acca"},
	},
	{
		kind:  "aws_key",
		re:    regexp.MustCompile(`(?i)\baws[A-Za-z0-9_\-]*(?:secret|sk)[A-Za-z0-9_\-]*["']?[ \t]*[:=][ \t]*["']?([A-Za-z0-9/+]{40})(?:[^A-Za-z0-9/+=]|\z)`),
		group: 1,
		hints: []string{"aws"},
	},
	{
		kind:  "google_key",
		re:    regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}`),
		hints: []string{"aiza"},
	},
	{
		kind:  "stripe_key",
		re:    regexp.MustCompile(`\b[sr]k_(?:live|test)_[0-9A-Za-z]{16,}`),
		hints: []string{"k_live_", "k_test_"},
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
		check: func(s string) bool { return !keep(s) },
	},
	{
		kind:  "auth_header",
		re:    regexp.MustCompile(`(?i)\bauthorization["']?[ \t]*[:=][ \t]*["']?(?:bearer|basic|token|digest|negotiate)[ \t]+([^\s"'\x60,;]+)`),
		group: 1,
		hints: []string{"authorization"},
		check: func(s string) bool { return !keep(s) },
	},
	{
		kind:  "secret_value",
		re:    regexp.MustCompile(`[A-Za-z0-9_.\-]*(?i:password|passwd|passphrase|secret|private[_\-]?key|token|api[_\-]?key|apikey|access[_\-]?key)` + keyTail + `["']?[ \t]*[:=][=>]?[ \t]*"((?:\\.|[^"\\\r\n])+)"`),
		group: 1,
		hints: []string{"passw", "passphrase", "secret", "token", "key"},
		check: func(s string) bool { return len(s) >= 8 && !keep(s) },
	},
	{
		kind:  "secret_value",
		re:    regexp.MustCompile(`[A-Za-z0-9_.\-]*(?i:password|passwd|passphrase|secret|private[_\-]?key|token|api[_\-]?key|apikey|access[_\-]?key)` + keyTail + `["']?[ \t]*[:=][=>]?[ \t]*'((?:\\.|[^'\\\r\n])+)'`),
		group: 1,
		hints: []string{"passw", "passphrase", "secret", "token", "key"},
		check: func(s string) bool { return len(s) >= 8 && !keep(s) },
	},
	{
		kind:  "auth_header",
		re:    regexp.MustCompile(`(?i)\bbearer[ \t]+([A-Za-z0-9._~+/\-]{16,}=*)`),
		group: 1,
		hints: []string{"bearer"},
		check: func(s string) bool { return hasDigit(s) && hasLetter(s) && looksSecret(s) },
	},
	{
		kind:       "secret_value",
		re:         regexp.MustCompile(`[A-Za-z0-9_.\-]*(?i:password|passwd|passphrase|secret|private[_\-]?key)` + keyTail + valueRe),
		group:      1,
		hints:      []string{"passw", "passphrase", "secret", "private"},
		check:      looksSecret,
		skipBefore: "(",
	},
	{
		kind:       "secret_value",
		re:         regexp.MustCompile(`[A-Za-z0-9_.\-]*(?i:token|api[_\-]?key|apikey|access[_\-]?key)` + keyTail + valueRe),
		group:      1,
		hints:      []string{"token", "key"},
		check:      looksSecret,
		skipBefore: "(",
	},
}

var (
	markerRe = regexp.MustCompile(`\[REDACTED:[a-z_]{1,32}\]`)
	ansiRe   = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[@-Z\\\\-_]|\u009b[0-?]*[ -/]*[@-~]")
	uuidRe   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	shaRe    = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	opRefRe  = regexp.MustCompile(`op://[^\s"'\x60,;<>]+`)

	entropyTrigger = regexp.MustCompile(`(?i)\b(?:password|passwd|passphrase|secret|token|api[_\-]?key|apikey|access[_\-]?key|client[_\-]?secret|private[_\-]?key|credential|bearer)\b`)
	entropyToken   = regexp.MustCompile(`[A-Za-z0-9+/_\-.~]{20,}={0,2}`)
	variableRef    = regexp.MustCompile(`^(?:\$[A-Za-z_][A-Za-z0-9_]*|\$\{[A-Za-z_][A-Za-z0-9_]*\}|(?:os\.environ|process\.env|self|config)\.[A-Za-z_][A-Za-z0-9_]*|os\.environ)$`)
)

// Redact replaces secrets with [REDACTED:<kind>] and returns the new text and
// the number of replacements. It strips ANSI escapes, bidi controls and NUL
// first, and removes zero-width characters, so a secret split by them is
// still caught. Git SHAs, UUIDs and op:// references are kept. It repeats
// until a pass finds nothing, because a replacement shortens the text and can
// bring a later token within reach of a trigger word, so redacting the result
// again changes nothing.
func Redact(text string) (string, int) {
	text = clean(text)
	total := 0
	for i := 0; i < maxPasses; i++ {
		var n int
		text, n = pass(text)
		total += n
		if n == 0 {
			break
		}
	}
	return text, total
}

// pass runs every rule once.
func pass(text string) (string, int) {
	lower := strings.ToLower(text)
	count := 0
	for _, r := range rules {
		if !hinted(lower, r.hints) {
			continue
		}
		var n int
		text, n = apply(text, r)
		if n > 0 {
			count += n
			lower = strings.ToLower(text)
		}
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

// markerSpans returns where the complete [REDACTED:<kind>] markers in text are.
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

// overlapsAny reports whether s overlaps any of spans.
func overlapsAny(spans []span, s span) bool {
	for _, m := range spans {
		if s.start < m.end && m.start < s.end {
			return true
		}
	}
	return false
}

// apply replaces every match of r in text and returns the count.
func apply(text string, r rule) (string, int) {
	marks := markerSpans(text)
	refs := opRefRe.FindAllStringIndex(text, -1)
	var spans []span
	for _, m := range r.re.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[2*r.group], m[2*r.group+1]
		if start < 0 || containedBy(marks, span{start, end}) {
			continue
		}
		inRef := false
		for _, ref := range refs {
			if start >= ref[0] && end <= ref[1] {
				inRef = true
				break
			}
		}
		if inRef {
			continue
		}
		v := text[start:end]
		if r.skipBefore != "" && end < len(text) && strings.IndexByte(r.skipBefore, text[end]) >= 0 {
			continue
		}
		if r.check != nil && !r.check(v) {
			continue
		}
		spans = append(spans, span{start, end})
	}
	return replace(text, spans, r.kind), len(spans)
}

func containedBy(spans []span, s span) bool {
	for _, m := range spans {
		if s.start >= m.start && s.end <= m.end {
			return true
		}
	}
	return false
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

// keep reports values that must survive redaction: git SHAs, UUIDs and
// 1Password op:// references.
func keep(v string) bool {
	return strings.HasPrefix(v, "op://") || uuidRe.MatchString(v) || shaRe.MatchString(v)
}

// looksSecret reports whether the value after a secret-looking key is a
// plausible secret, rather than a placeholder, a variable or a code reference.
func looksSecret(v string) bool {
	return !keep(v) && !looksLikePlaceholder(v)
}

func looksLikePlaceholder(v string) bool {
	if v == "" {
		return true
	}
	return variableRef.MatchString(v)
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
