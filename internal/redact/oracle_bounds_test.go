package redact

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// The bounds of the exceptions for code. Each gap class excuses a whole
// shape (see oracleGapShapes), so the oracle holds the current rules on the
// edges of each shape only where a near case stands. oracleBounds lists,
// from floorValue and plainCode in redact.go and the predicates they call,
// the conditions that bound an exception: each character class, each
// length, each count and each refusal it has found, one line each. Each
// bound has near cases on its far side, where the exception does not hold
// and the current rules alone remove the secret as the baseline rules do:
// one labelled in corpus/oracle_near.txt, whose name starts with the bound's
// id and a colon, and many drawn by the near class's generator, named by the
// id. A rule change that widens an exception past one of its bounds keeps
// those of its near secrets that stand where it widened, and TestOracle
// fails on them.
//
// A far side is sampled, not exhausted. Where it is a set of characters or
// operators, the generator draws each in turn from the printable marks that
// can stand there, at the start, inside or at the end of a name where that
// matters, and for the operators of a type from many operators of more than
// one character too, each with a blank on both sides, on one side and on
// neither (spaced), but each with a few continuations and values only; where
// it is a set of names, as a variable's prefix, a scope after '$' or a
// reference's scheme, it draws each word of codeWords in turn and random
// names besides; for some bounds it draws from a short list of the shapes
// code takes, as for the brackets of an index; where it is a length or a
// count, it draws the lengths and counts just past the bound and some
// beyond, and random names run to 40 characters. A widening that admits a
// member only in a context no near case stands in, as after a continuation
// the generator does not write, or a name on no list and drawn by no random
// name, keeps nothing the oracle judges, and TestOracle takes it. A name
// that shares 6 characters with a key or a marker, as secrets does, stands
// in no case (checkCase), so a widening to it is not seen.
//
// TestOracleBoundsHaveNearCases fails when a bound has no near case, labelled
// or generated, or a near case names no bound. Each bound also says how to
// widen redact.go past it alone (widen): TestOracleBoundsWidenRedactGo checks
// that each such change still applies, so a change to an exception's code
// shows which bounds to look at again, and TestOracleHoldsEachBound, run
// with -args -widen-bounds, makes each change in turn with go test -overlay
// and checks that TestOracle fails on that bound's own near cases.
//
// Conditions that are no bound a secret can cross, so have no near case:
//   - floorValue's markerStart and looksLikePlaceholder's <...>: a value the
//     baseline rules take never holds '[' or '<'.
//   - floorValue's cut of a value at '@' or '/' in a URL's authority: it
//     narrows what the floor takes, and the url_credentials rule takes the
//     password before the floor.
//   - quotedName's length of 3: brackets that open a quote close only after
//     the same quote (closeBracket), and exprLike refuses brackets with no
//     letter or digit. Its characters are a bound (index.quoted.chars): a
//     widening that reads a mark as a separator of words crosses both its
//     test of characters and wordLike's.
//   - aliasName's length of 3: a name of lower-case letters is a word, and
//     the floor takes 8 characters or more. Its characters are a bound
//     (alias.name.chars), as quotedName's are.
//   - memberPath's length of 4 for a short lower-case part: a part of 3 or
//     more lower-case letters is a word for wordLike.
//   - wordLike's characters: every caller also asks for memberSegment or the
//     quoted name's characters, which are bounds (index.name.segment,
//     index.path.segment, index.ivar.segment, index.quoted.chars and
//     alias.name.chars) whose near cases hold marks of every kind.
//   - memberSegment's first character, a letter or '_', for a name of words
//     before an index, an instance variable and an alias: wordLike, which
//     each of them also asks for, refuses a digit before the first word, and
//     the value rules read no name that starts with a digit as code
//     (codeName, refHead). For a member path it is a bound
//     (index.path.part.digits), since a widening may skip such a name
//     before either test, and so is a quoted name's (index.quoted.words).
//     For the three others, a widening that skips the digits before the
//     first word, in the value rules too, is listed as a widening of the
//     bound of their characters (index.name.segment, index.ivar.segment,
//     alias.name.chars), whose near cases have such digits.
//   - keySeparator's test of a shell assignment, a key directly followed by
//     a single '=': each exception it refuses (type.shell, index.name.shell,
//     regex.shell) has near cases of KEY= and export KEY=, so a change that
//     reads fewer keys as shell assignments keeps them.
//   - closeBracket's nesting of brackets beyond the one that opens: a
//     bracket inside that never closes leaves the first one open
//     (index.closes), and one that closes on the wrong bracket fails as
//     index.closes.match does.
//   - The rest of the line plainCode looks at, rest: it ends at the line's
//     end (index.closes.line) and maxCodeScan past the value
//     (index.closes.scan, alias.whole).
//
// Most exceptions are read twice: the value rules, which run before the
// floor, have their own reading of code (lineValue's type annotation,
// isCode with codeHead, codeEnd and closeBracket), and the floor is asked
// only for a value they leave. A bound that both readings hold is crossed
// only when both widen, so its widening changes both, and its near cases
// stand where the value rules read code and only the floor's exception
// refuses the secret: a near case the value rules take holds nothing.
// TestOracleHoldsEachBound found each bound below that the value rules hold
// too, as one whose widening of the floor alone kept nothing.

// edit is a change of redact.go: old, which occurs there exactly once,
// becomes new.
type edit struct{ old, new string }

// codeNameDash lets a name in code hold a '-', so that the value rules read
// a name such as silver-Window before an index as code (codeHead), as the
// floor's exceptions then must refuse it alone.
var codeNameDash = edit{"const codeName = `(?:[A-Za-z_$][A-Za-z0-9_$]*|", "const codeName = `(?:[A-Za-z_$][A-Za-z0-9_$-]*|"}

// oracleBound is one condition that bounds an exception for code.
type oracleBound struct {
	// id names the bound; a near case's name starts with it.
	id string
	// shape is the gap shape whose near class holds the bound's near cases.
	shape string
	// what says the bound in one line.
	what string
	// example is a near case: a made-up secret just past the bound, which
	// the baseline rules, v1 and the current rules alone remove, joined from
	// parts. value is the part the baseline rules replace. The labelled near
	// case of the bound in corpus/oracle_near.txt has the same text.
	example struct{ text, value string }
	// widen widens the exception past this bound alone.
	widen []edit
	// more are other widenings past this bound alone, each a list of edits
	// applied together, as reviewers and fixers of the near classes made them.
	more [][]edit
	// near draws the n-th generated near case of the bound: its line, with
	// %s where the value goes, and what makes the value.
	near func(g *gen, n int) (line string, value func() string)
	// weight is how many turns the bound takes in each round of its near
	// class's generator, 1 if 0: a far side of names from a vocabulary takes
	// more.
	weight int
}

// The separators and line shapes of the near classes. After a spaced "==",
// a comparison, the floor takes nothing (see floorValue): each gap class has
// that separator, so a near class does not.
var (
	nearSeps   = []string{": ", ":", " = ", " := ", " => "}
	gapKeys    = []string{"SECRET_KEY", "secret_key", "SecretKey", "db_password", "api_key", "ACCESS_TOKEN", "client_secret", "aws_secret_access_key", "private_key", "auth_token", "password", "token"}
	gapFlags   = []string{"", "g", "i", "m", "u", "gi", "im"}
	validFlags = "dgimsuvyxoen"
)

// keyLine is a line of a secret-looking key, one of seps, the value and
// tail, perhaps indented.
func keyLine(g *gen, seps []string, tail string) string {
	return g.pick([]string{"", "  "}) + g.pick(gapKeys) + g.pick(seps) + "%s" + tail
}

// shellLine is a shell assignment of the value to a secret-looking key,
// followed by tail.
func shellLine(g *gen, tail string) string {
	return g.pick([]string{"", "export "}) + g.pick(gapKeys) + "=%s" + tail
}

// nth is the n-th character of set, round and round.
func nth(set string, n int) string {
	i := n % len(set)
	return set[i : i+1]
}

// nthOf is the n-th of from, round and round.
func nthOf[T any](from []T, n int) T { return from[n%len(from)] }

// without returns the printable ASCII characters but a space that are not
// in set.
func without(set string) string {
	var b strings.Builder
	for _, c := range printableASCII[1:] {
		if !strings.ContainsRune(set, c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// The far sides of the character classes the bounds hold. A character the
// baseline rules end a value at, or that starts a variable ($), is left out
// where it would put the secret outside what the baseline rules replace; so
// is '=' at the start of a value, which the baseline rules and the current
// rules read as part of the separator (":=").
var (
	// Flags other than a regular expression's: any character but the valid
	// flags and those that end the value.
	farFlags = without(validFlags + "\"'`,;&<>(){}[]$")
	// What may follow a closing bracket and goes on as no code does.
	farAfter = without(" \t,;)]}.[(\"'`\\") + "\v\f"
	// What a type name does not start with or hold.
	farTypeFirst = "0123456789+-.!@*%^~?:|#/\\"
	farTypeRest  = "+-!%^~*@:=?/#|$\\"
	// A prefix that is no '@' or '$'.
	farPrefix = "%!^~-+:?.*|#/\\"
	// A character that is no regular expression metacharacter.
	farMeta = ".-_=~!@%:#"
	// A character that is no part of a variable's name.
	farVariable = "+-.~!%^*=:?/#|@\\"
	// Prefixes of a name that make no variable.
	farVariablePrefix = []string{"cfg.", "env.", "settings.", "conf.", "this.", "os.", "process.", "ENV.", "Config.", "SELF.", "os.environ", "process.env", "os_environ.", "self_"}
	// What may stand before a member path but '@', '$' and "::". A '.' is
	// left out, since the rules before the floor take such a value, and so
	// is '=', which the rules read as part of a separator ":=".
	farPathPrefix = "%!^~-+:?*|#/\\"
	// What may end a member path but '?' and '!'.
	farPathSuffix = "#%^~*+-=:@$|/\\"
	// What may join two names but '.' and "::".
	farPathSeparator = ":/|\\+~^%*!?=@$#"
	// What may stand in a member path's name, at its start, inside it or at
	// its end, but a letter, a digit and '_'. '&', at which the baseline
	// rules end a value, is left out, and so is '.', which joins names.
	farSegment = "$-@%+~^!*:#=|/\\?"
	// What may stand between a type and the type after it but a single '|',
	// as an operator of a union of types: every mark but '|' and '=', and
	// operators of more than one character and of words. A '=' alone makes
	// an assignment, and a '|' alone a union, which are the gap shape.
	farUnionOps = append(strings.Split(without(alnum+"|="), ""),
		"or", "and", "OR", "AND", "xor", "in", "is", "as", "&&", "||", "|>", "<|", "->", "=>", "::", "..", "|:", "+|", "|&", "&|", "|=", "or=", "<>", "!|", "|!")
	// What may stand between a type and the value assigned to it but a single
	// '=': every mark but '=' and '|' (a union, of the gap shape), the
	// operators of a compound assignment, and others.
	farAssignOps = append(strings.Split(without(alnum+"=|"), ""),
		":=", "+=", "-=", "*=", "/=", "%=", "|=", "&=", "^=", "<=", ">=", "!=", "?=", "~=", ".=", "**=", "//=", "<<=", ">>=", "??=", "||=", "&&=", "@=", "#=", "$=", "::=",
		"=>", "->", "<-", "=:", "===", "=~", "=!", "=?", "=*", "=&", "=|", "=+", "=-", "is", "to", "as")
	// What may follow a type, its index or its generic, but one '?': every
	// mark but '?' and those a name holds ('_' and '.'), and runs of marks.
	// A '(' makes a call, which the baseline rules leave alone too.
	farTypeSuffix = append(strings.Split(without(alnum+"?_.("), ""), "??", "???", "?!", "!?", "!!", "?*", "*?", "?:", "?+", "!!!", "?$", "?#")
	// What may follow a regular expression literal but a blank, a quote, a
	// backslash, ';' and ','. The value the floor looks at ends where the
	// baseline rules end it, so what follows it is one of these or the end
	// of the line: every other character is part of the value, and makes no
	// whole literal (regex.whole, regex.flags.letters). A '(' makes a call,
	// which the baseline rules leave alone too.
	farRegexAfter = ")]}<>&[{\v\f"
	// What may follow that, so that the value rules read what follows the
	// literal as code (codeEnd) and leave the value to the floor: a URL's
	// next pair, a member, an index, a name, a mark.
	regexAfterTails = []string{"", "x", "a=1", "b=2&c=3", ".x", ".source", "[0]", "x=1", "x)", "1", ")", "]", "&d=4", "!", "?.x", "::y", "->z", "+1", "|x", "*", "/"}
	// What may stand in brackets with no letter or digit: every mark but the
	// brackets and quotes, which close or open what they hold.
	farBracketInner = without(alnum + "[]{}()\"'`")
	// The bases and joins of a variable's prefix: each base followed by each
	// join is no variable's prefix but where the base is a variable's (the
	// first four) and the join '.' (see variableRef), and the floor takes a
	// value after it. A "->" or a '[' is left out: the baseline rules end the
	// value at its '>' or '['.
	variableBases = []string{"self", "config", "os.environ", "process.env", "cfg", "this", "env", "ENV", "settings", "conf", "Self", "SELF", "Config", "os", "process", "opts", "ctx"}
	variableJoins = []string{".", "?.", "!.", "::", "?", ":", "/", "_", "-", "..", ".$", "@", "|", "^", "~", "%", "+", "*", "=", "#"}
	// codeWords are words that code and configuration name a scope, a store
	// or a source of values with, as a variable's prefix (props., vars.), a
	// reference's scheme (vault://) or a scope after '$' ($env:). Where a far
	// side is a set of such names, the generator draws each of these in turn
	// and random lower-case names besides: a widening to a name on neither
	// is not seen. None is a variable's base of variableRef.
	codeWords = []string{"env", "ENV", "Env", "environ", "environment", "vars", "var", "inputs", "input", "params", "param", "props", "vault", "Vault", "settings", "Settings", "conf", "opts", "options", "args", "argv", "arg", "context", "state", "store", "data", "app", "App", "Rails", "meta", "global", "globals", "local", "locals", "script", "sys", "system", "Self", "window", "document", "req", "request", "res", "response", "session", "user", "values", "Values", "Chart", "Release", "matrix", "steps", "needs", "job", "runner", "keys", "key", "creds", "auth", "pkg", "main", "ns", "module", "mod", "lib", "util", "utils", "provider", "aws", "gcp", "azure", "kube", "k8s", "db", "redis", "kv", "consul", "sops", "ssm", "file", "path", "url", "host", "site", "page", "form", "model", "item", "obj", "record", "row", "node", "server", "api", "web", "prod", "dev", "test", "stage", "tf", "terraform", "pillar", "helm", "import", "export", "use", "my", "our", "public", "static", "let", "def", "new", "get", "set", "fetch", "load", "lookup", "from", "to", "of", "in", "at", "as", "bw", "pass", "gpg", "age", "kms", "sm", "gsm", "akv", "keychain", "keyring", "doppler", "infisical", "chamber", "parameter", "dotenv", "ini", "yaml", "json", "toml", "OP", "Op"}
	// dottedBases are prefixes of more than one name that code reads values
	// from, none of them a variable's.
	dottedBases = []string{"import.meta.env", "Rails.application.config", "process.argv", "os.getenv", "sys.argv", "this.props", "this.state", "self.config", "app.config", "ctx.vault", "gh.event", "context.vars"}
)

var (
	// What may join a scope or a name to the name after it, after '$': each
	// mark that is no part of a variable's name and runs of two. One at which
	// the baseline rules end a value is left out.
	variableScopeJoins = append(strings.Split(farVariable, ""), "::", ":::", "..", ":.", ".:", "-:", "::$", ":$", ".$", "/:", "\\\\", "//", "#:", "!:", "?:", "~:", "@:", "%:")
	// The marks a name between two of them stands in: each mark that may
	// start a value, around it, and pairs of them. A '=' is left out: at the
	// start of a value it is read as part of the separator.
	variableWraps = func() [][2]string {
		var w [][2]string
		for _, c := range "%$@!^~-+:?.*|#/\\" {
			w = append(w, [2]string{string(c), string(c)})
		}
		return append(w, [][2]string{{"$", "%"}, {"%", "$"}, {"$%", "%"}, {"%%", "%%"}, {"$$", "$$"}, {"@@", "@@"}, {"$:", ":"}, {"$!", "!"}, {"$#", "#"}, {"!", "?"}}...)
	}()
)

// spaced puts op between what comes before and after it, by k in turn with
// a blank on both sides, before it only, after it only and on neither; a
// side where op has a letter or a digit always has one, which would
// otherwise join op to the name beside it, and so does the side before a
// '(', which would otherwise make a call that the baseline rules leave
// alone.
func spaced(op string, k int) string {
	before, after := k%4 < 2, k%4 == 0 || k%4 == 2
	if isAlnum(op[0]) || op[0] == '(' {
		before = true
	}
	if isAlnum(op[len(op)-1]) {
		after = true
	}
	s := op
	if before {
		s = " " + s
	}
	if after {
		s += " "
	}
	return s
}

// lowerName makes a random name of 2 to 10 lower-case letters.
func lowerName(g *gen) string {
	return g.chars(2+g.r.Intn(9), "abcdefghijklmnopqrstuvwxyz")
}

// codeWord draws the n-th name of a far side of names: each of codeWords in
// turn, and every third time a random lower-case name.
func codeWord(g *gen, n int) string {
	if n%3 == 2 {
		return lowerName(g)
	}
	return nthOf(codeWords, n-n/3)
}

// shortPart makes a random part, as randomPart does, that leaves a value
// that starts with prefix shorter than 20 characters, which the entropy
// rule leaves to the floor, and of 8 at least, which the floor takes; it is
// 3 characters at least.
func shortPart(g *gen, prefix string) string {
	return randomPart(g, max(3, min(12, 19-len(prefix)-g.r.Intn(3))))
}

// farVariablePrefixOf draws the n-th prefix that makes no variable: by turns,
// one of farVariablePrefix or a base of variableBases with '.' that is no
// variable's, and a variable's base with each join of variableJoins other
// than '.', in turn.
func farVariablePrefixOf(n int) string {
	if n%2 == 0 {
		var dotted []string
		for _, b := range variableBases {
			if !slices.Contains(variableBases[:4], b) {
				dotted = append(dotted, b+".")
			}
		}
		return nthOf(append(dotted, farVariablePrefix...), n/2)
	}
	k := n / 2
	return nthOf(variableBases[:4], k) + nthOf(variableJoins[1:], k/4)
}

// wordsOf makes a name of two words, as wordsValue does without digits:
// SummerWinter or silverWindow.
func wordsOf(g *gen) string {
	a, b := g.pick(gapWords), g.pick(gapWords)
	if g.r.Intn(2) == 0 {
		a = strings.ToLower(a)
	}
	return a + b
}

// randomPart makes a name of n characters that starts with a letter, holds
// a capital or a digit, and is no word, as a generated password's part is.
func randomPart(g *gen, n int) string {
	for {
		p := g.chars(1, alnum[:52]) + g.chars(n-1, alnum)
		if p != strings.ToLower(p) || hasDigit(p) {
			if !wordLike(p, 1) && !(len(p) <= 4 && strings.Trim(p, "abcdefghijklmnopqrstuvwxyz") == "") {
				return p
			}
		}
	}
}

// insertAt puts c into s at a random place inside it.
func insertAt(g *gen, s, c string) string {
	i := 1 + g.r.Intn(len(s)-1)
	return s[:i] + c + s[i:]
}

// separated makes a random name of n characters, as randomPart does, with
// one to three separators of words of seps inside it, as in Qm7_Rt2xP: a
// name that holds what wordLike splits words at and is still no name of
// words.
func separated(g *gen, n int, seps string) string {
	s := randomPart(g, n)
	for k := 1 + g.r.Intn(3); k > 0; k-- {
		s = insertAt(g, s, nth(seps, g.r.Intn(len(seps))))
	}
	return s
}

// placeMark puts mark c into name at its start, inside it or at its end,
// by k in turn. A '@' or '$' goes inside rather than at the start, where it
// would make an instance variable, and so does a '=', which would be read as
// part of the separator.
func placeMark(g *gen, name, c string, k int) string {
	switch {
	case k%3 == 0 || k%3 == 2 && strings.ContainsAny(c, "@$="):
		return insertAt(g, name, c)
	case k%3 == 1:
		return name + c
	}
	return c + name
}

// exprWith makes brackets around an expression of 1 to 30 characters that
// holds mark at its start, inside it or at its end, as the n-th near case of
// a bound that refuses the mark.
func exprWith(g *gen, n int, mark string) string {
	expr := nthOf([]string{"a", "x9", "i", "key", "len(s)-1", "user_id", "0:10", "a, b", "name.first", "i + 1", "get(k)", "k1 k2 k3"}, n/3)
	for len(expr) < 30 && g.r.Intn(3) == 0 {
		expr += nthOf([]string{" ", "_", "."}, g.r.Intn(3)) + strings.ToLower(g.pick(gapWords))
	}
	open, close := nthOf([]string{"[", "{"}, n/2), nthOf([]string{"]", "}"}, n/2)
	switch n % 3 {
	case 0:
		expr = mark + expr
	case 1:
		expr += mark
	default:
		expr = expr[:len(expr)/2] + mark + expr[len(expr)/2:]
	}
	return open + expr + close
}

var indexTails = []string{"[x9]", "[0]", "[i]", "{a}"}

// oracleBounds are the bounds, by shape, in the order of the code.
var oracleBounds = []oracleBound{
	// floorValue, for every shape.
	{
		id: "floor.call", shape: "yaml_alias",
		what:    "the floor takes nothing before '(', as the baseline rules do, and takes the value before any other character",
		example: ex("password: *Hmyhy"+"bbz82)", "*Hmyhy"+"bbz82"),
		widen:   []edit{{"text[s.end] == '('", `strings.IndexByte("()", text[s.end]) >= 0`}},
		near: func(g *gen, n int) (string, func() string) {
			// A character the baseline rules end a value at that is no '(', or
			// a '(' after a blank.
			tail := nthOf([]string{")", "<", ">", "]", "}", "[", "{", "&", ",", ";", `"`, "'", "`", " (", "\t(", "  (", "\f("}, n)
			return keyLine(g, nearSeps, tail+g.pick([]string{"", "x"})), func() string { return "*" + nearToken(g) }
		},
	},
	{
		id: "floor.comparison.eq", shape: "yaml_alias",
		what:    `a comparison, after which the floor takes nothing, is a separator with "==", not '=', ':=' or '=>'`,
		example: ex("token = *Tq7Wn"+"3Ks9Bv", "*Tq7Wn"+"3Ks9Bv"),
		widen:   []edit{{`strings.Contains(sep, "==") && strings.ContainsAny(sep, " \t")`, `strings.Contains(sep, "=") && strings.ContainsAny(sep, " \t")`}},
		near: func(g *gen, n int) (string, func() string) {
			// Every spaced separator the floor's key takes but "==" (see
			// floorValueRe): ':' or '=', perhaps followed by '=' or '>'.
			return keyLine(g, []string{" = ", " := ", " => ", "  =  ", " : ", " :> ", "\t=\t", " =\t", "\t:= ", " =>\t"}, ""), func() string { return "*" + nearToken(g) }
		},
	},
	{
		id: "floor.comparison.blank", shape: "yaml_alias",
		what:    `a comparison's "==" has a blank beside it: an unspaced "==" is no comparison`,
		example: ex("token==*Rv8Kd"+"2Wm5Qx", "*Rv8Kd"+"2Wm5Qx"),
		widen:   []edit{{`strings.Contains(sep, "==") && strings.ContainsAny(sep, " \t")`, `strings.Contains(sep, "==")`}},
		near: func(g *gen, n int) (string, func() string) {
			return g.pick(gapKeys) + "==%s", func() string { return "*" + nearToken(g) }
		},
	},
	{
		id: "floor.length", shape: "yaml_alias",
		what:    "the floor takes a value of 8 characters or more, as the baseline rules do",
		example: ex("password: *Xk9m"+"P2v", "*Xk9m"+"P2v"),
		widen:   []edit{{"if len(v) < minValueLen || !looksSecret(v) || markerStart(v) {", "if len(v) < minValueLen+1 || !looksSecret(v) || markerStart(v) {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, []string{": ", " = ", ":"}, ""), func() string {
				for {
					s := g.chars(1, alnum[:52]) + g.mixed(6+n%2)
					if !wordLike(s, 1) {
						return "*" + s
					}
				}
			}
		},
	},
	{
		id: "floor.variable", shape: "name_index",
		what:    "a variable, which the floor leaves alone as the baseline rules do, is $, self., config., os.environ. or process.env. and a name of letters, digits and '_' that starts with no digit",
		example: ex("password: $Xk9mP2"+"+vLq7Rt", "$Xk9mP2"+"+vLq7Rt"),
		widen:   []edit{{`^(?:\$[A-Za-z_][A-Za-z0-9_]*|`, `^(?:\$[A-Za-z_][A-Za-z0-9_+]*|`}},
		more: [][]edit{
			// A name after a prefix that starts with a digit.
			{{`(?:os\.environ|process\.env|self|config)\.[A-Za-z_][A-Za-z0-9_]*`, `(?:os\.environ|process\.env|self|config)\.[A-Za-z0-9_][A-Za-z0-9_]*`}},
			// A '!' after a name after '$'.
			{{`^(?:\$[A-Za-z_][A-Za-z0-9_]*|`, `^(?:\$[A-Za-z_][A-Za-z0-9_]*!?|`}},
			// A '?' or '!' after a name after a prefix.
			{{`(?:os\.environ|process\.env|self|config)\.[A-Za-z_][A-Za-z0-9_]*`, `(?:os\.environ|process\.env|self|config)\.[A-Za-z_][A-Za-z0-9_]*[?!]?`}},
			// A reviewer's: PowerShell's $env:NAME.
			{{`^(?:\$[A-Za-z_][A-Za-z0-9_]*|`, `^(?:\$(?:env:)?[A-Za-z_][A-Za-z0-9_]*|`}},
			// A reviewer's: Windows' %NAME%.
			{{`^(?:\$[A-Za-z_][A-Za-z0-9_]*|`, `^(?:%[A-Za-z_][A-Za-z0-9_]*%|\$[A-Za-z_][A-Za-z0-9_]*|`}},
			// A reviewer's: Perl's $Pkg::name.
			{{`^(?:\$[A-Za-z_][A-Za-z0-9_]*|`, `^(?:\$[A-Za-z_][A-Za-z0-9_]*(?:::[A-Za-z_][A-Za-z0-9_]*)*|`}},
			// Make's $(NAME) has a '(', at which the baseline rules end a
			// value; a name between '@', as in a template's @NAME@.
			{{`^(?:\$[A-Za-z_][A-Za-z0-9_]*|`, `^(?:@[A-Za-z_][A-Za-z0-9_]*@|\$[A-Za-z_][A-Za-z0-9_]*|`}},
		},
		weight: 8,
		near: func(g *gen, n int) (string, func() string) {
			k := n / 4
			switch n % 4 {
			case 1:
				// A scope after '$', joined to the name by ':' or "::" as a
				// language joins one ($env:X, $Pkg::x): each of codeWords with
				// each join in turn.
				return keyLine(g, nearSeps, ""), func() string {
					p := "$" + nthOf(codeWords, k) + nthOf([]string{":", "::"}, k/len(codeWords))
					return p + shortPart(g, p)
				}
			case 2:
				// A random name after '$' joined to a name by each mark or run
				// of marks in turn ($a::b, $a/b).
				return keyLine(g, nearSeps, ""), func() string {
					p := "$" + randomPart(g, 3+g.r.Intn(5)) + nthOf(variableScopeJoins, k)
					return p + shortPart(g, p)
				}
			case 3:
				// A name between two marks (%X%, @X@), each pair in turn.
				return keyLine(g, nearSeps, ""), func() string {
					w := nthOf(variableWraps, k)
					return w[0] + shortPart(g, w[0]+w[1]) + w[1]
				}
			}
			n = k
			// Names of fewer than 20 characters, which the entropy rule leaves
			// to the floor.
			prefixes := []string{"$", "self.", "config.", "os.environ.", "process.env."}
			// A mark of farVariable at the start of the name, inside it or at
			// its end, in turn.
			place := func(name string) string {
				c := nth(farVariable, n/3)
				switch n / 3 / len(farVariable) % 3 {
				case 0:
					return insertAt(g, name, c)
				case 1:
					return name + c
				}
				return c + name
			}
			return keyLine(g, nearSeps, ""), func() string {
				switch n % 3 {
				case 0:
					return "$" + place(randomPart(g, 9+g.r.Intn(6)))
				case 1:
					return nthOf(prefixes[1:], n/3) + place(randomPart(g, 9+g.r.Intn(6)))
				}
				// A name that starts with a digit.
				return nthOf(prefixes, n/3) + g.chars(1, digits) + randomPart(g, 9+g.r.Intn(6))
			}
		},
	},
	{
		id: "floor.variable.prefix", shape: "name_index",
		what:    "a variable's prefix is $, self., config., os.environ. or process.env., or it is os.environ alone",
		example: ex("password: cfg.Xk9m"+"P2vLq7Rt", "cfg.Xk9m"+"P2vLq7Rt"),
		widen:   []edit{{`(?:os\.environ|process\.env|self|config)\.`, `(?:os\.environ|process\.env|self|config|cfg)\.`}},
		more: [][]edit{
			// A reviewer's: optional chaining after a prefix's name.
			{{`(?:os\.environ|process\.env|self|config)\.`, `(?:os\.environ|process\.env|self|config)\??\.`}},
			// A name joined by "::".
			{{`(?:os\.environ|process\.env|self|config)\.`, `(?:os\.environ|process\.env|self|config)(?:\.|::)`}},
			// A reviewer's: React's props.
			{{`(?:os\.environ|process\.env|self|config)\.`, `(?:os\.environ|process\.env|self|config|props)\.`}},
			// A reviewer's: a template's vars., inputs. and params.
			{{`(?:os\.environ|process\.env|self|config)\.`, `(?:os\.environ|process\.env|self|config|vars|inputs|params)\.`}},
			// A reviewer's: vault.
			{{`(?:os\.environ|process\.env|self|config)\.`, `(?:os\.environ|process\.env|self|config|vault)\.`}},
			// A prefix of more than one name, as Vite's import.meta.env.
			{{`(?:os\.environ|process\.env|self|config)\.`, `(?:os\.environ|process\.env|import\.meta\.env|self|config)\.`}},
			// Any lower-case name, as a prefix.
			{{`(?:os\.environ|process\.env|self|config)\.`, `(?:os\.environ|process\.env|self|config|[a-z]+)\.`}},
		},
		weight: 6,
		near: func(g *gen, n int) (string, func() string) {
			k := n / 3
			bases := append(append([]string{}, codeWords...), dottedBases...)
			return keyLine(g, nearSeps, ""), func() string {
				var p string
				switch n % 3 {
				case 0:
					// A variable's base with a join that is no '.', or a base
					// that is no variable's.
					p = farVariablePrefixOf(k)
				case 1:
					// Each name of codeWords and dottedBases in turn, joined by
					// '.' twice and then by each other join.
					p = nthOf(bases, k) + nthOf(variableJoins, k/len(bases)/2)
				default:
					// A random lower-case name, with each join in turn.
					p = lowerName(g) + nthOf(variableJoins, k)
				}
				return p + shortPart(g, p)
			}
		},
	},
	{
		id: "floor.opref", shape: "yaml_alias",
		what:    "a 1Password reference, which the floor leaves alone as the baseline rules do, starts with op:// exactly",
		example: ex("password: op:Xk9m"+"P2vLq7Rt", "op:Xk9m"+"P2vLq7Rt"),
		widen:   []edit{{`return strings.HasPrefix(v, "op://") }`, `return strings.HasPrefix(v, "op:") }`}},
		more: [][]edit{
			// A reviewer's: another store's references, as vault://.
			{{`return strings.HasPrefix(v, "op://") }`, `return strings.HasPrefix(v, "op://") || strings.HasPrefix(v, "vault://") }`}},
			// Any scheme of lower-case letters.
			{{`return strings.HasPrefix(v, "op://") }`, `return strings.HasPrefix(v, "op://") || regexp.MustCompile("^[a-z]+://").MatchString(v) }`}},
		},
		weight: 4,
		near: func(g *gen, n int) (string, func() string) {
			k := n / 3
			return keyLine(g, nearSeps, ""), func() string {
				switch n % 3 {
				case 1:
					// Each name of codeWords in turn as a scheme.
					p := nthOf(codeWords, k) + "://"
					return p + shortPart(g, p)
				case 2:
					// A random scheme.
					name := lowerName(g)
					for name == "op" {
						name = lowerName(g)
					}
					p := name + nthOf([]string{"://", "://", ":/", ":"}, k)
					return p + shortPart(g, p)
				}
				return nthOf([]string{"op:", "op:/", "op//", "OP://", "Op://", "op.//", "op_"}, k) + nearToken(g)
			}
		},
	},
	{
		id: "floor.undefined", shape: "yaml_alias",
		what:    "the floor leaves alone the word undefined, in lower case and alone",
		example: ex("password: undefined_"+"Xk9mP2", "undefined_"+"Xk9mP2"),
		widen:   []edit{{`v == "undefined"`, `strings.HasPrefix(v, "undefined")`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, ""), func() string {
				switch n % 5 {
				case 0:
					return "undefined" + nearToken(g)
				case 1:
					return "undefined_" + nearToken(g)
				case 2:
					return nearToken(g) + "undefined"
				case 3:
					return "Undefined"
				}
				return "UNDEFINED"
			}
		},
	},

	// plainCode: a type annotation or a chained assignment.
	{
		id: "type.shell", shape: "type_annotation",
		what:    "a type annotation is never read in a shell assignment",
		example: ex("PASSWORD=Xk9mP2"+"vLq7Rt = 1", "Xk9mP2"+"vLq7Rt"),
		widen:   []edit{{"\tif !shell {\n\t\t// A name that is assigned", "\tif true {\n\t\t// A name that is assigned"}},
		near: func(g *gen, n int) (string, func() string) {
			return shellLine(g, g.pick([]string{" = 1", " | None", " = None"})), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.assign.before", shape: "type_annotation",
		what:    "an assignment after a type has a blank before its '='",
		example: ex("password: Wt6Ns1"+"Kd9Fq4= 1", "Wt6Ns1"+"Kd9Fq4"),
		widen:   []edit{{`(?:[ \t]+=[ \t]+|$)`, `(?:[ \t]*=[ \t]+|$)`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"= 1", "= None", "= x"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.assign.after", shape: "type_annotation",
		what:    "an assignment after a type has a blank after its '='",
		example: ex("password: Xk9mP2"+"vLq7Rt =1", "Xk9mP2"+"vLq7Rt"),
		widen: []edit{
			{`(?:[ \t]+=[ \t]+|$)`, `(?:[ \t]+=[ \t]*|$)`},
			{`if line[t[1]-1] == ' ' || line[t[1]-1] == '\t' || t[3] > t[2] {`, `if line[t[1]-1] == ' ' || line[t[1]-1] == '=' || t[3] > t[2] {`},
			// lineValue reads the annotation too, before the floor does.
			{`case v[t[1]-1] == ' ' || v[t[1]-1] == '\t':`, `case v[t[1]-1] == ' ' || v[t[1]-1] == '=':`},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" =1", " =None", " =x"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.assign.single", shape: "type_annotation",
		what:    `an assignment after a type is a single '=', not "=="`,
		example: ex("password: Hb4Zq8"+"Lm2Wx6 == 1", "Hb4Zq8"+"Lm2Wx6"),
		widen:   []edit{{`(?:[ \t]+=[ \t]+|$)`, `(?:[ \t]+==?[ \t]+|$)`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" == 1", " == None", " == x"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.assign.operator", shape: "type_annotation",
		what:    "an assignment after a type is a '=' alone: no other operator, such as ':=', '+=' or '=>', stands for it",
		example: ex("password: Wd3Hk8"+"Rn6Tc1 := 1", "Wd3Hk8"+"Rn6Tc1"),
		widen:   []edit{{`(?:[ \t]+=[ \t]+|$)`, `(?:[ \t]+:?=[ \t]+|$)`}},
		more: [][]edit{
			// The operators of a compound assignment.
			{{`(?:[ \t]+=[ \t]+|$)`, `(?:[ \t]+[-+*/%|&^]?=[ \t]+|$)`}},
			// An arrow.
			{{`(?:[ \t]+=[ \t]+|$)`, `(?:[ \t]+(?:=|=>|->)[ \t]+|$)`}},
		},
		weight: 3,
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, spaced(nthOf(farAssignOps, n), n/len(farAssignOps))+g.pick([]string{"1", "None", "x"})), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.bare", shape: "type_annotation",
		what:    "a type with no union and no assignment after it is no annotation",
		example: ex("password: Xk9mP2"+"vLq7Rt<ab>", "Xk9mP2"+"vLq7Rt"),
		widen: []edit{
			{`if line[t[1]-1] == ' ' || line[t[1]-1] == '\t' || t[3] > t[2] {`, `if len(line) > 0 {`},
			// lineValue reads the annotation too, before the floor does.
			{"case t[3] > t[2]:\n\t\t\treturn span{}, false, false", "case t[3] >= t[2]:\n\t\t\treturn span{}, false, false"},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"<ab>", "[ab]", "<T>", "[x9]", "?", "<T>?", "[]", "<>", "[a]?", ""}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.union.before", shape: "type_annotation",
		what:    "a union of types has a blank before its '|'",
		example: ex("secret: Yh2Mc7"+"Pz5Lr8| None", "Yh2Mc7"+"Pz5Lr8"),
		widen:   []edit{{`((?:[ \t]+\|[ \t]+`, `((?:[ \t]*\|[ \t]+`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"| None", "| str", "|  int"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.union.after", shape: "type_annotation",
		what:    "a union of types has a blank after its '|'",
		example: ex("secret: aB3dE5"+"fG7hJ2 |None", "aB3dE5"+"fG7hJ2"),
		widen:   []edit{{`((?:[ \t]+\|[ \t]+`, `((?:[ \t]+\|[ \t]*`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" |None", " |str", "  |int"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.union.single", shape: "type_annotation",
		what:    `a union of types has a single '|', not "||"`,
		example: ex("secret: Nc5Gt1"+"Vx8Kp3 || None", "Nc5Gt1"+"Vx8Kp3"),
		widen:   []edit{{`((?:[ \t]+\|[ \t]+`, `((?:[ \t]+\|\|?[ \t]+`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" || None", " || str"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.union.operator", shape: "type_annotation",
		what:    "a union of types is joined by a '|' alone: no other operator, such as '&', 'or' or '|>', stands for it",
		example: ex("secret: Lt5Bq9"+"Zm3Wx7 & None", "Lt5Bq9"+"Zm3Wx7"),
		widen:   []edit{{`((?:[ \t]+\|[ \t]+`, `((?:[ \t]+[|&][ \t]+`}},
		more: [][]edit{
			// A reviewer's: a union by a word.
			{{`((?:[ \t]+\|[ \t]+`, `((?:[ \t]+(?:\||or)[ \t]+`}},
			// A pipe.
			{{`((?:[ \t]+\|[ \t]+`, `((?:[ \t]+\|>?[ \t]+`}},
			// A '+'.
			{{`((?:[ \t]+\|[ \t]+`, `((?:[ \t]+[|+][ \t]+`}},
			// A reviewer's: an unspaced ',' between members.
			{{"`((?:[ \\t]+\\|[ \\t]+` + typeName + `)*)", "`((?:(?:[ \\t]+\\||,)[ \\t]+` + typeName + `)*)"}},
			// A '&' with or without blanks.
			{{"`((?:[ \\t]+\\|[ \\t]+` + typeName + `)*)", "`((?:(?:[ \\t]+\\|[ \\t]+|[ \\t]*&[ \\t]*)` + typeName + `)*)"}},
		},
		weight: 3,
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, spaced(nthOf(farUnionOps, n), n/len(farUnionOps))+g.pick([]string{"None", "str", "int"})), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.union.member", shape: "type_annotation",
		what:    "each member of a union of types is a type's name: no number, string or mark",
		example: ex("secret: Bz7Hq2"+"Xn4Rw9 | 1", "Bz7Hq2"+"Xn4Rw9"),
		widen:   []edit{{"\\|[ \\t]+` + typeName + `)*)", "\\|[ \\t]+(?:` + typeName + `|-?[0-9]+))*)"}},
		more: [][]edit{
			// Any member at all.
			{{"\\|[ \\t]+` + typeName + `)*)", "\\|[ \\t]+(?:` + typeName + `|[^ \\t]+))*)"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			// A member that starts with a digit or with any mark but those of a
			// name ('_'), a blank, '|' and '=', or that is a number or a string.
			m := nthOf([]string{"1", "-1", "0x1", "'x'", `"x"`, "9x", "42"}, n/2)
			if n%2 == 0 {
				m = nth(without(alnum+"_|="), n/2) + g.pick([]string{"x", "T", "1", ""})
			}
			return keyLine(g, nearSeps, " | "+m+g.pick([]string{"", " = 1"})), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.blank", shape: "type_annotation",
		what:    "the blanks around a type's '|' and '=' are spaces and tabs, not other white space",
		example: ex("secret: Kq3Vb9"+"Wd5Hn1\v| None", "Kq3Vb9"+"Wd5Hn1"),
		widen: []edit{
			{`((?:[ \t]+\|[ \t]+`, `((?:[ \t\v\f]+\|[ \t\v\f]+`},
			{`(?:[ \t]+=[ \t]+|$)`, `(?:[ \t\v\f]+=[ \t\v\f]+|$)`},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"\v| None", "\f| str", "\v= 1", "\f= None", " |\vNone", "\t\v| x", " =\f1"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.name.first", shape: "type_annotation",
		what:    "a type's name starts with a letter or '_'",
		example: ex("password: 7Xk9mP"+"2vLq7Rt = 1", "7Xk9mP"+"2vLq7Rt"),
		widen:   []edit{{"const typeName = `[A-Za-z_][A-Za-z0-9_.]*", "const typeName = `[A-Za-z0-9_][A-Za-z0-9_.]*"}},
		more: [][]edit{
			// A reviewer's: a type that starts with a digit and holds '+' or '/'.
			{{"const typeName = `[A-Za-z_][A-Za-z0-9_.]*", "const typeName = `[A-Za-z0-9_][A-Za-z0-9_.+/]*"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" = 1", " | None", " = x"}, n/len(farTypeFirst))), func() string { return nth(farTypeFirst, n) + nearToken(g) }
		},
	},
	{
		id: "type.name.rest", shape: "type_annotation",
		what:    "a type's name goes on with letters, digits, '_' and '.' only",
		example: ex("password: Xk9mP2"+"+vLq7/Rt = 1", "Xk9mP2"+"+vLq7/Rt"),
		widen:   []edit{{"const typeName = `[A-Za-z_][A-Za-z0-9_.]*", "const typeName = `[A-Za-z_][A-Za-z0-9_.+]*"}},
		more: [][]edit{
			{{"const typeName = `[A-Za-z_][A-Za-z0-9_.]*", "const typeName = `[A-Za-z_][A-Za-z0-9_./]*"}},
			{{"const typeName = `[A-Za-z_][A-Za-z0-9_.]*", "const typeName = `[A-Za-z_][A-Za-z0-9_.+/]*"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" = 1", " | None", " = x"}, n/len(farTypeRest))), func() string { return insertAt(g, nearToken(g), nth(farTypeRest, n)) }
		},
	},
	{
		id: "type.name.index", shape: "type_annotation",
		what:    "an index on a type ends at its first ']', which closes it on its line",
		example: ex("password: Gm5Tz1"+"Ry8Pv3[a]b] = 1", "Gm5Tz1"+"Ry8Pv3"),
		widen:   []edit{{`(?:\[[^\]\r\n]*\]|`, `(?:\[[^\r\n]*\]|`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[a]b] = 1", "[x]9] | None", "[i]]] = x", "[0]a]? = 1", "[ab = 1", "[x9 | None"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.name.generic", shape: "type_annotation",
		what:    "a generic on a type ends at its first '>', which closes it on its line",
		example: ex("password: Bn7Kx2"+"qW4mZ9<a>b> = 1", "Bn7Kx2"+"qW4mZ9"),
		widen:   []edit{{`|<[^>\r\n]*>)?`, `|<[^\r\n]*>)?`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"<a>b> = 1", "<T>U> | None", "<x>>> = 1", "<ab = 1", "<T | None"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.name.brackets", shape: "type_annotation",
		what:    "a type has one index or one generic at most",
		example: ex("password: Qh6Vt2"+"Lw9Ks4[a][b] = 1", "Qh6Vt2"+"Lw9Ks4"),
		widen:   []edit{{"(?:\\[[^\\]\\r\\n]*\\]|<[^>\\r\\n]*>)?\\??`", "(?:\\[[^\\]\\r\\n]*\\]|<[^>\\r\\n]*>)*\\??`"}},
		more: [][]edit{
			// Two at most.
			{{"(?:\\[[^\\]\\r\\n]*\\]|<[^>\\r\\n]*>)?\\??`", "(?:\\[[^\\]\\r\\n]*\\]|<[^>\\r\\n]*>){0,2}\\??`"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			var b strings.Builder
			for k := 2 + n%3; k > 0; k-- {
				b.WriteString(nthOf([]string{"[a]", "<T>", "[x9]", "<K, V>", "[0]", "[]", "<>", "[i]"}, n/3+k))
			}
			b.WriteString(g.pick([]string{"", "?"}))
			return keyLine(g, nearSeps, b.String()+g.pick([]string{" = 1", " | None", " = x"})), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.name.optional", shape: "type_annotation",
		what:    "a type ends in its name, index or generic and one '?' at most: no second '?' and no other mark",
		example: ex("password: Vc3Lp8"+"Hy5Mt2?? = 1", "Vc3Lp8"+"Hy5Mt2??"),
		widen:   []edit{{">)?\\??`", ">)?\\?*`"}},
		more: [][]edit{
			// A reviewer's: a '!' in place of the '?'.
			{{">)?\\??`", ">)?[?!]?`"}},
			// A '*', as a pointer.
			{{">)?\\??`", ">)?[?*]?`"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			tail := nthOf([]string{" = 1", " | None", " = x"}, n/len(farTypeSuffix))
			suffix := nthOf(farTypeSuffix, n)
			if strings.ContainsAny(suffix[:1], `"'`+"`,;&<>(){}[]") {
				// The baseline rules end the value before it.
				return keyLine(g, nearSeps, suffix+tail), func() string { return nearToken(g) }
			}
			return keyLine(g, nearSeps, tail), func() string { return nearToken(g) + suffix }
		},
	},

	// plainCode: an index or a literal after the value.
	{
		id: "index.opens", shape: "name_index",
		what:    "an index or a literal after the value opens with '[' or '{': no other bracket, such as '<', opens one",
		example: ex("password: harbor"+"Lantern<x9>", "harbor"+"Lantern"),
		widen: []edit{
			{"case rest != \"\" && (rest[0] == '[' || rest[0] == '{'):", "case rest != \"\" && (rest[0] == '[' || rest[0] == '{' || rest[0] == '<'):"},
			{"\t\tcase '{':\n\t\t\twant = append(want, '}')\n\t\tcase ')', ']', '}':\n\t\t\tif len(want) == 0 || want[len(want)-1] != c {", "\t\tcase '{':\n\t\t\twant = append(want, '}')\n\t\tcase '<':\n\t\t\twant = append(want, '>')\n\t\tcase ')', ']', '}', '>':\n\t\t\tif len(want) == 0 || want[len(want)-1] != c {"},
			// The value rules read no '<' after a name as code (codeHead).
			{"[?!]?(?:[(\\[]|", "[?!]?(?:[(\\[<]|"},
		},
		near: func(g *gen, n int) (string, func() string) {
			// Every mark the baseline rules end a value at but '[', '{' and
			// '(' (a call), with what an index holds after it.
			open := nth("<>)]}&", n)
			inner := nthOf([]string{"x9", "i", "0", "a, b"}, n/6)
			closer := map[string]string{"<": ">"}[open]
			return keyLine(g, nearSeps, open+inner+closer), func() string {
				switch n / 6 / 4 % 3 {
				case 0:
					return wordsValue(g)
				case 1:
					return g.pick(gapWords) + "." + g.pick(gapWords)
				}
				return "@" + strings.ToLower(g.pick(gapWords)) + "_" + strings.ToLower(g.pick(gapWords))
			}
		},
	},
	{
		id: "index.closes", shape: "name_index",
		what:    "brackets after the value close on its line, even where the line goes on to the next (the value rules read such a bracket as code)",
		example: ex("password: hunter"+"Marble[x9,", "hunter"+"Marble"),
		widen:   []edit{{"end, ok := closingBracket(rest)", "end, ok := closingBracket(rest)\n\t\tif !ok && len(rest) > 2 {\n\t\t\tend, ok = len(rest), true\n\t\t}"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[x9,", "{ab,", "[i[", "{a(", "[0, ", "[x9, y", "{a, b", "[x9"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.closes.match", shape: "name_index",
		what:    "a closing bracket after the value closes the bracket it matches",
		example: ex("password: copper"+"Lantern[x9)", "copper"+"Lantern"),
		widen:   []edit{{"if len(want) == 0 || want[len(want)-1] != c {", "if len(want) == 0 {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[x9)", "{ab]", "[i}", "{0)", "[ab}", "[a(b]"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.closes.quote", shape: "name_index",
		what:    "a quote inside the brackets closes before the bracket does",
		example: ex(`password: maple`+`Rocket["x9]`, "maple"+"Rocket"),
		widen:   []edit{{"j := strings.Index(v[i+len(q):], q)\n\t\t\tif j < 0 {", "j := strings.Index(v[i+len(q):], q)\n\t\t\tif j < 0 && len(v) < 0 {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{`["x9]`, `['i]`, `[a"]`, `{'ab}`, `[x9']`, `{"0}`}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.closes.line", shape: "name_index",
		what:    "the brackets close on the value's line",
		example: ex("password: velvet"+"Harbor[x9,\n]", "velvet"+"Harbor"),
		widen:   []edit{{`if i := strings.IndexAny(text[s.start:lineEnd], "\r\n"); i >= 0 {`, `if i := strings.IndexAny(text[s.start:lineEnd], "\r"); i >= 0 {`}},
		// A widening that ends a line at '\n' only keeps the generated near
		// cases with a '\r'; a corpus file holds no lone '\r', so it has no
		// labelled case and is not listed in more, which asks for one.
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[x9,\n]", "[i,\n  j]", "{a,\n}", "[0(\n)] # next", "{ab,\n}\nport: 1", "[x9\n]", "[x9,\r]", "{a,\r\n}", "[i\r]"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.closes.scan", shape: "name_index",
		what:    "the brackets close within maxCodeScan bytes of the value",
		example: ex("password: candle"+"Meadow[i"+strings.Repeat(" ", 520)+"]", "candle"+"Meadow"),
		widen:   []edit{{"lineEnd, whole := min(len(text), s.end+maxCodeScan), false", "lineEnd, whole := min(len(text), s.end+4*maxCodeScan), false"}},
		near: func(g *gen, n int) (string, func() string) {
			open, close := nthOf([]string{"[", "{"}, n), nthOf([]string{"]", "}"}, n)
			return keyLine(g, nearSeps, open+nth("ix0a", n/2)+strings.Repeat(" ", maxCodeScan+1+n%16)+close), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.alnum", shape: "name_index",
		what:    "the brackets hold a letter or a digit",
		example: ex("password: lantern"+"Pillow[--]", "lantern"+"Pillow"),
		widen:   []edit{{"return strings.IndexFunc(s, func(r rune) bool { return r < 128 && isAlnum(byte(r)) }) >= 0 && ", "return "}},
		more: [][]edit{
			// A reviewer's: '_' counted as a letter.
			{{"return r < 128 && isAlnum(byte(r))", "return r < 128 && (isAlnum(byte(r)) || r == '_')"}},
			// '$' counted as a letter, as a name in code may hold it.
			{{"return r < 128 && isAlnum(byte(r))", "return r < 128 && (isAlnum(byte(r)) || r == '$')"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			if n%4 == 3 {
				return keyLine(g, nearSeps, nthOf([]string{"[--]", "[+-]", "{..}", "[!]", `[""]`, "{+}", "[__]", "[_.]", "{_}", "[$_]"}, n/4)), func() string { return wordsValue(g) }
			}
			// One to three marks of every mark but brackets and quotes.
			open, close := nthOf([]string{"[", "{"}, n), nthOf([]string{"]", "}"}, n)
			inner := nth(farBracketInner, n) + g.chars(n%3, farBracketInner)
			return keyLine(g, nearSeps, open+inner+close), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.ascii", shape: "name_index",
		what:    "the letter or digit in the brackets is ASCII",
		example: ex("password: walnut"+"Basket[š]", "walnut"+"Basket"),
		widen:   []edit{{"r < 128 && isAlnum(byte(r))", "isAlnum(byte(r))"}},
		near: func(g *gen, n int) (string, func() string) {
			// Each of these runes has the low byte of an ASCII letter or digit.
			return keyLine(g, nearSeps, nthOf([]string{"[š]", "[Ł]", "{ű}", "[İ]", "[šš]", "{Łű}"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.semicolon", shape: "name_index",
		what:    "the brackets hold no ';'",
		example: ex("password: lantern"+"Marble[a;b]", "lantern"+"Marble"),
		widen:   []edit{{"!strings.ContainsAny(s, \";~#`\")", "!strings.ContainsAny(s, \"~#`\")"}},
		more: [][]edit{
			// A ';' in a longer expression.
			{{"!strings.ContainsAny(s, \";~#`\")", "(!strings.ContainsAny(s, \";~#`\") || strings.Count(s, \";\") == 1 && len(s) > 6)"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, exprWith(g, n, ";")), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.tilde", shape: "name_index",
		what:    "the brackets hold no '~'",
		example: ex("password: copper"+"Basket[a~1]", "copper"+"Basket"),
		widen:   []edit{{"!strings.ContainsAny(s, \";~#`\")", "!strings.ContainsAny(s, \";#`\")"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, exprWith(g, n, "~")), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.hash", shape: "name_index",
		what:    "the brackets hold no '#'",
		example: ex("password: walnut"+"Rocket[a#1]", "walnut"+"Rocket"),
		widen:   []edit{{"!strings.ContainsAny(s, \";~#`\")", "!strings.ContainsAny(s, \";~`\")"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, exprWith(g, n, "#")), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.backtick", shape: "name_index",
		what:    "the brackets hold no backtick",
		example: ex("password: silver"+"Window[`a`]", "silver"+"Window"),
		widen:   []edit{{"!strings.ContainsAny(s, \";~#`\")", "!strings.ContainsAny(s, \";~#\")"}},
		near: func(g *gen, n int) (string, func() string) {
			if n%2 == 0 {
				// A quoted string, closed.
				return keyLine(g, nearSeps, nthOf([]string{"[`a`]", "[`x9`]", "{`i`}", "[`" + strings.ToLower(g.pick(gapWords)) + "`, 1]"}, n/2)), func() string { return wordsValue(g) }
			}
			return keyLine(g, nearSeps, exprWith(g, n/2, "`")), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.after", shape: "name_index",
		what:    "after the brackets comes nothing, a blank, a quote, a backslash or one of , ; ) ] } . [ (",
		example: ex("password: harbor"+"Candle[x9]:", "harbor"+"Candle"),
		widen:   []edit{{"strings.IndexByte(\" \\t,;)]}.[(\\\"'`\\\\\", s[0])", "strings.IndexByte(\" \\t,;)]}.[(\\\"'`\\\\:\", s[0])"}},
		more: [][]edit{
			{{"strings.IndexByte(\" \\t,;)]}.[(\\\"'`\\\\\", s[0])", "strings.IndexByte(\" \\t,;)]}.[(\\\"'`\\\\!&?\", s[0])"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			if n%2 == 1 {
				// What the value rules read as code after a bracket (codeEnd),
				// so that the floor decides.
				return keyLine(g, nearSeps, nthOf(indexTails, n/2)+nthOf([]string{":", "!", "!)", ":;", "&.size", "?.size", "&page=2", "!,"}, n/2)), func() string { return wordsValue(g) }
			}
			return keyLine(g, nearSeps, nthOf(indexTails, n/2/len(farAfter))+nth(farAfter, n/2)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.quoted.words", shape: "name_index",
		what:    "an index by a quoted name, which any value may have before it, holds a name of words",
		example: ex(`api_key: Jd4Hs8`+`Wq2Nc6["Qw3zR8"]`, "Jd4Hs8"+"Wq2Nc6"),
		widen:   []edit{{`== "" && wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1)`, `== ""`}},
		more: [][]edit{
			// A name that holds a separator of words, as a name of words does.
			{{`== "" && wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1)`, `== "" && (wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1) || strings.ContainsAny(name, "_-."))`}},
			// A reviewer's: a name of digits.
			{{`&& wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1)`, `&& (allDigits(name) || wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1))`}},
			// A name that starts with a digit.
			{{`&& wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1)`, `&& (name[0] >= '0' && name[0] <= '9' || wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1))`}},
		},
		near: func(g *gen, n int) (string, func() string) {
			q := nthOf([]string{`"`, `'`}, n)
			// Random names, short names, names with separators of words,
			// names of digits and names that start with a digit.
			inside := nthOf([]string{randomPart(g, 6), "ab", "x9", randomPart(g, 4), separated(g, 6+g.r.Intn(8), "_-."), "a_b", "x-9",
				g.chars(1+g.r.Intn(8), digits), g.chars(1, digits) + randomPart(g, 3+g.r.Intn(6)), g.chars(1+g.r.Intn(3), digits) + strings.ToLower(g.pick(gapWords)), g.chars(1+g.r.Intn(4), digits) + "_" + strings.ToLower(g.pick(gapWords))}, n/2)
			return keyLine(g, nearSeps, "["+q+inside+q+"]"), func() string { return nearToken(g) }
		},
	},
	{
		id: "index.quoted.bracket", shape: "name_index",
		what:    "an index by a quoted name is in square brackets: a quoted name in braces is none",
		example: ex(`password: Xk9mP2`+`vLq7Rt{"Token"}`, "Xk9mP2"+"vLq7Rt"),
		widen:   []edit{{`if rest[0] == '[' && quotedName(rest[1:end-1]) {`, `if quotedName(rest[1:end-1]) {`}},
		near: func(g *gen, n int) (string, func() string) {
			q := nthOf([]string{`"`, `'`}, n)
			name := nthOf([]string{"Token", strings.ToLower(g.pick(gapWords)) + "_key", "access_token", g.pick(gapWords) + g.pick(gapWords)}, n/2)
			return keyLine(g, nearSeps, "{"+q+name+q+"}"), func() string { return nearToken(g) }
		},
	},
	{
		id: "index.quoted.chars", shape: "name_index",
		what:    "an index by a quoted name holds letters, digits, '_', '-' and '.' only",
		example: ex(`password: Tw9Gk3`+`Pz6Mr1["access:token"]`, "Tw9Gk3"+"Pz6Mr1"),
		widen:   []edit{{"name := s[1 : len(s)-1]\n", "name := s[1 : len(s)-1]\n\tname = strings.ReplaceAll(name, \":\", \"_\")\n"}},
		more: [][]edit{
			// Any mark read as a separator of words.
			{{"name := s[1 : len(s)-1]\n", "name := s[1 : len(s)-1]\n\tname = strings.Map(func(r rune) rune {\n\t\tif r < 128 && !isAlnum(byte(r)) {\n\t\t\treturn '_'\n\t\t}\n\t\treturn r\n\t}, name)\n"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			// Every mark but the quotes, those exprLike refuses and brackets,
			// between two words, at the start or at the end.
			q := nthOf([]string{`"`, `'`}, n)
			marks := without(alnum + "-._\"'`;~#[]{}()")
			c := nth(marks, n/2)
			a, b := strings.ToLower(g.pick(gapWords)), strings.ToLower(g.pick(gapWords))
			name := a + c + b
			switch n / 2 / len(marks) % 3 {
			case 1:
				name = c + a + "_" + b
			case 2:
				name = a + "_" + b + c
			}
			return keyLine(g, nearSeps, "["+q+name+q+"]"), func() string { return nearToken(g) }
		},
	},
	{
		id: "index.quoted.quote", shape: "name_index",
		what:    `an index by a quoted name opens and closes with the same quote, '"' or '''`,
		example: ex("password: Mv6Rk2"+"Tz8Pq4[xTokenx]", "Mv6Rk2"+"Tz8Pq4"),
		widen:   []edit{{`if len(s) < 3 || s[0] != s[len(s)-1] || s[0] != '"' && s[0] != '\'' {`, `if len(s) < 3 || s[0] != s[len(s)-1] {`}},
		near: func(g *gen, n int) (string, func() string) {
			// Any character but a quote and the marks exprLike refuses.
			q := nth(without(`"'`+"`;~#[]{}()"), n)
			name := nthOf([]string{"Token", "access_token", "api-key", "Secret"}, n/7)
			return keyLine(g, nearSeps, "["+q+name+q+"]"), func() string { return nearToken(g) }
		},
	},
	{
		id: "index.name.shell", shape: "name_index",
		what:    "a name of words before an index is no code in a shell assignment",
		example: ex("DB_PASSWORD=candle"+"Basket[x9]", "candle"+"Basket"),
		widen:   []edit{{"return path || !shell && memberSegment", "return path || memberSegment"}},
		near: func(g *gen, n int) (string, func() string) {
			return shellLine(g, nthOf(indexTails, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.name.segment", shape: "name_index",
		what:    "a name of words before an index is one name of letters, digits and '_'",
		example: ex("password: silver"+"-Window[x9]", "silver"+"-Window"),
		widen: []edit{
			{"!shell && memberSegment.MatchString(v) && wordLike(v, 2)", "!shell && wordLike(v, 2)"},
			// The value rules read a name with a '-' as no code (codeHead).
			codeNameDash,
		},
		more: [][]edit{
			// A '$' in the name, as the value rules read one in code
			// (codeName).
			{{"!shell && memberSegment.MatchString(v) && wordLike(v, 2)", "!shell && memberSegment.MatchString(strings.ReplaceAll(v, \"$\", \"\")) && wordLike(strings.ReplaceAll(v, \"$\", \"\"), 2)"}},
			// A '?' or '!' at the end of the name, as a member path may have.
			{{"!shell && memberSegment.MatchString(v) && wordLike(v, 2)", "!shell && memberSegment.MatchString(strings.TrimRight(v, \"?!\")) && wordLike(strings.TrimRight(v, \"?!\"), 2)"}},
			// Digits before the first word, in the value rules too (codeName).
			{
				{"!shell && memberSegment.MatchString(v) && wordLike(v, 2)", "!shell && memberSegment.MatchString(strings.TrimLeft(v, \"0123456789\")) && wordLike(strings.TrimLeft(v, \"0123456789\"), 2)"},
				{"const codeName = `(?:[A-Za-z_$][A-Za-z0-9_$]*|", "const codeName = `(?:[A-Za-z0-9_$][A-Za-z0-9_$]*|"},
			},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/2)), func() string {
				if n%4 == 2 {
					// Digits before a name of words.
					return g.chars(1+g.r.Intn(4), digits) + wordsOf(g)
				}
				if n%2 == 0 {
					return strings.ToLower(g.pick(gapWords)) + "-" + g.pick(gapWords)
				}
				// Any mark of farSegment, at the start, inside or at the end.
				return placeMark(g, wordsOf(g), nth(farSegment, n/2), n/2/len(farSegment))
			}
		},
	},
	{
		id: "index.name.words", shape: "name_index",
		what:    "a name before an index that is no member path or instance variable has two words or more",
		example: ex("password: Lantern"+"42[0]", "Lantern"+"42"),
		widen:   []edit{{"wordLike(v, 2), true", "wordLike(v, 1), true"}},
		more: [][]edit{
			// A name that holds a '_', as a name of words does.
			{{"wordLike(v, 2), true", "(wordLike(v, 2) || strings.Contains(v, \"_\")), true"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/2)), func() string {
				if n%2 == 1 {
					// A random name with a '_' inside.
					return separated(g, 9+g.r.Intn(32), "_")
				}
				w := g.pick(gapWords)
				return w + g.chars(max(1, 8-len(w))+g.r.Intn(2), digits)
			}
		},
	},
	{
		id: "words.digits.count", shape: "name_index",
		what:    "a name of words has 4 digits at most after a word",
		example: ex("password: Summer"+"12345Winter[0]", "Summer"+"12345Winter"),
		widen:   []edit{{"if words == 0 || j-i > 4 {", "if words == 0 || j-i > 5 {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return g.pick(gapWords) + g.chars(5+n%2, digits) + g.pick(gapWords)
			}
		},
	},
	{
		id: "words.digits.first", shape: "name_index",
		what:    "a name of words has no digit before its first word",
		example: ex("password: _42sum"+"merWinter[0]", "_42sum"+"merWinter"),
		widen:   []edit{{"if words == 0 || j-i > 4 {", "if j-i > 4 {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return "_" + g.chars(1+n%3, digits) + wordsOf(g)
			}
		},
	},
	{
		id: "words.part.length", shape: "name_index",
		what:    "a word of a name has 3 letters or more",
		example: ex("password: huMarb"+"leRiver[0]", "huMarb"+"leRiver"),
		widen:   []edit{{"if j-i < 3 {", "if j-i < 2 {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				p := g.chars(1, alnum[:26]) + g.chars(1, alnum[:26])
				if n%2 == 1 {
					p = strings.ToUpper(p[:1]) + p[1:]
				}
				return p + g.pick(gapWords) + g.pick(gapWords)
			}
		},
	},
	{
		id: "words.acronym", shape: "name_index",
		what:    "an acronym in a name of words has 2 capitals or more",
		example: ex("password: XMarbl"+"eRiver[0]", "XMarbl"+"eRiver"),
		widen:   []edit{{"if k-i >= 2 {", "if k-i >= 1 {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return g.chars(1, alnum[26:52]) + g.pick(gapWords) + g.pick(gapWords)
			}
		},
	},
	{
		id: "words.acronym.split", shape: "name_index",
		what:    "the last capital of a run before a lower-case letter starts the next word, so two capitals before one are no acronym",
		example: ex("password: QZmarb"+"leLantern[0]", "QZmarb"+"leLantern"),
		widen:   []edit{{"k-- // the last capital starts the next word, as in AWSSecrets", "// the last capital starts the next word, as in AWSSecrets"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return g.chars(2, alnum[26:52]) + strings.ToLower(g.pick(gapWords)) + g.pick(gapWords)
			}
		},
	},
	{
		id: "index.path.prefix", shape: "name_index",
		what:    "a member path has nothing before its first name but '@' and '$', or '::'",
		example: ex("password: *Summer"+".Winter[i]", "*Summer"+".Winter"),
		// The value rules read a path after a '*' or '&' as code (codeHead).
		widen: []edit{{`path := memberPath(strings.TrimLeft(v, "@$"))`, `path := memberPath(strings.TrimLeft(v, "@$*"))`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/2)), func() string {
				p := nthOf([]string{"*", "**", "*@", "*$"}, n/2)
				if n%2 == 0 {
					p = nth(farPathPrefix, n/2)
				}
				return p + g.pick(gapWords) + "." + g.pick(gapWords)
			}
		},
	},
	{
		id: "index.path.suffix", shape: "name_index",
		what:    "a member path ends in its last name, perhaps followed by '?' or '!'",
		example: ex("password: Summer.Win"+"ter$[0]", "Summer.Win"+"ter$"),
		// The value rules read a name that ends in '$' as code (codeName).
		widen: []edit{{`v = strings.TrimRight(v, "?!")`, `v = strings.TrimRight(v, "?!$")`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/2)), func() string {
				s := nthOf([]string{"$", "$$", "$!", "$?"}, n/2)
				if n%2 == 0 {
					s = nth(farPathSuffix, n/2)
				}
				return g.pick(gapWords) + "." + g.pick(gapWords) + s
			}
		},
	},
	{
		id: "index.path.separator", shape: "name_index",
		what:    "the names of a member path are joined by '.' or '::' only",
		example: ex("password: Summer?."+"Winter[0]", "Summer?."+"Winter"),
		// The value rules read names joined by "?." as code (codeHead).
		widen: []edit{{`strings.FieldsFunc(strings.ReplaceAll(v, "::", "."),`, `strings.FieldsFunc(strings.NewReplacer("::", ".", "?.", ".").Replace(v),`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/2)), func() string {
				sep := "?."
				switch {
				case n%4 == 2:
					sep = nthOf([]string{":::", ".?.", "?.?."}, n/4)
				case n%2 == 0:
					sep = nth(farPathSeparator, n/4)
				}
				return g.pick(gapWords) + sep + g.pick(gapWords)
			}
		},
	},
	{
		id: "index.path.parts", shape: "name_index",
		what:    "a member path has two names or more",
		example: ex("password: lantern"+"marble[0]", "lantern"+"marble"),
		widen:   []edit{{"if len(parts) < 2 ||", "if len(parts) < 1 ||"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				if n%2 == 0 {
					return strings.ToLower(g.pick(gapWords)) + strings.ToLower(g.pick(gapWords))
				}
				w := g.pick(gapWords)
				return w + g.chars(max(1, 8-len(w))+g.r.Intn(2), digits)
			}
		},
	},
	{
		id: "index.path.dots", shape: "name_index",
		what:    `a member path has no ".."`,
		example: ex("password: Summer"+"..Winter[2]", "Summer"+"..Winter"),
		widen: []edit{
			{`strings.Contains(v, "..") || `, ``},
			// The value rules read names joined by ".." as no code (codeHead).
			{"`(?:(?:\\.|\\?\\.|::|->)`", "`(?:(?:\\.\\.?|\\?\\.|::|->)`"},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string { return g.pick(gapWords) + ".." + g.pick(gapWords) }
		},
	},
	{
		id: "index.path.trailing", shape: "name_index",
		what:    "a member path does not end in '.'",
		example: ex("password: Summer"+".Winter.[2]", "Summer"+".Winter."),
		widen: []edit{
			{` || strings.HasSuffix(v, ".")`, ``},
			// The value rules read a name that ends in '.' as no code (codeHead).
			{"`)*[?!]?(?:[(\\[]|[ \\t]*\\{)`", "`)*\\.?[?!]?(?:[(\\[]|[ \\t]*\\{)`"},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string { return g.pick(gapWords) + "." + g.pick(gapWords) + "." }
		},
	},
	{
		id: "index.path.segment", shape: "name_index",
		what:    "each name of a member path is of letters, digits and '_'",
		example: ex("password: Summer"+".win-river[2]", "Summer"+".win-river"),
		widen: []edit{
			{"if !memberSegment.MatchString(p) || !(wordLike(p, 1)", "if !(wordLike(p, 1)"},
			codeNameDash,
		},
		more: [][]edit{
			// A reviewer's: a '$' at the start of each name.
			{{"for _, p := range parts {\n\t\tif !memberSegment.MatchString(p)", "for _, p := range parts {\n\t\tp = strings.TrimLeft(p, \"$\")\n\t\tif !memberSegment.MatchString(p)"}},
			// A '$' anywhere in a name, as the value rules read one in code
			// (codeName).
			{{"for _, p := range parts {\n\t\tif !memberSegment.MatchString(p)", "for _, p := range parts {\n\t\tp = strings.ReplaceAll(p, \"$\", \"\")\n\t\tif !memberSegment.MatchString(p)"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/2)), func() string {
				if n%2 == 0 {
					return g.pick(gapWords) + "." + strings.ToLower(g.pick(gapWords)) + "-" + strings.ToLower(g.pick(gapWords))
				}
				// A mark of farSegment at the start of the second name, inside a
				// name, or at the end of the first: neither the prefix nor the
				// suffix a whole path may have.
				c := nth(farSegment, n/2)
				a, b := wordsOf(g), wordsOf(g)
				switch n / 2 / len(farSegment) % 3 {
				case 0:
					b = c + b
				case 1:
					a = insertAt(g, a, c)
				default:
					a += c
				}
				return a + "." + b
			}
		},
	},
	{
		id: "index.path.part.words", shape: "name_index",
		what:    "each name of a member path is made of words, is one character, or is a short lower-case name",
		example: ex("secret: Bn7Kx2."+"qW4mZ9[2]", "Bn7Kx2."+"qW4mZ9"),
		widen:   []edit{{`if !memberSegment.MatchString(p) || !(wordLike(p, 1) || len(p) == 1 || len(p) <= 4 && strings.Trim(p, "abcdefghijklmnopqrstuvwxyz") == "") {`, `if !memberSegment.MatchString(p) {`}},
		more: [][]edit{
			// A name that holds a '_', as a name of words does.
			{{`if !memberSegment.MatchString(p) || !(wordLike(p, 1) ||`, `if !memberSegment.MatchString(p) || !(wordLike(p, 1) || strings.Contains(p, "_") ||`}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[2]", "[i]", "[x9]", " { x }"}, n)), func() string {
				switch n % 3 {
				case 0:
					return g.pick(gapWords) + "." + nearToken(g)
				case 1:
					return nearToken(g) + "." + nearToken(g)
				}
				// Random names with a '_' inside.
				return g.pick(gapWords) + "." + separated(g, 6+g.r.Intn(30), "_")
			}
		},
	},
	{
		id: "index.path.part.one", shape: "name_index",
		what:    "a name of a member path that is not made of words is one character, not two",
		example: ex("password: Summer"+".Wq[2]", "Summer"+".Wq"),
		widen:   []edit{{"len(p) == 1 ||", "len(p) <= 2 ||"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string { return g.pick(gapWords) + "." + randomPart(g, 2) }
		},
	},
	{
		id: "index.path.part.short", shape: "name_index",
		what:    "a short name of a member path that is not made of words is lower-case letters only",
		example: ex("password: Summer"+".ab12[2]", "Summer"+".ab12"),
		widen:   []edit{{`len(p) <= 4 && strings.Trim(p, "abcdefghijklmnopqrstuvwxyz") == ""`, `len(p) <= 4 && strings.Trim(p, "abcdefghijklmnopqrstuvwxyz0123456789") == ""`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return g.pick(gapWords) + "." + randomPart(g, 3+n%2)
			}
		},
	},
	{
		id: "index.path.part.digits", shape: "name_index",
		what: "each name of a member path starts with a letter or '_' (memberSegment, and wordLike after it): a name of digits, or one that starts with a digit, makes no path",
		// The value rules read no name that starts with a digit as code
		// (codeName), so each widening changes them too.
		example: ex("password: harbor.8473"+"1925[0]", "harbor.8473"+"1925"),
		widen: []edit{
			{"const codeName = `(?:[A-Za-z_$][A-Za-z0-9_$]*|", "const codeName = `(?:[A-Za-z_$][A-Za-z0-9_$]*|[0-9]+|"},
			{"for _, p := range parts {\n\t\tif !memberSegment.MatchString(p)", "for _, p := range parts {\n\t\tif allDigits(p) {\n\t\t\tcontinue\n\t\t}\n\t\tif !memberSegment.MatchString(p)"},
		},
		more: [][]edit{
			// A name that starts with a digit.
			{
				{"const codeName = `(?:[A-Za-z_$][A-Za-z0-9_$]*|", "const codeName = `(?:[A-Za-z0-9_$][A-Za-z0-9_$]*|"},
				{"for _, p := range parts {\n\t\tif !memberSegment.MatchString(p)", "for _, p := range parts {\n\t\tif p[0] >= '0' && p[0] <= '9' {\n\t\t\tcontinue\n\t\t}\n\t\tif !memberSegment.MatchString(p)"},
			},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/4)), func() string {
				switch n % 4 {
				case 0:
					return g.pick(gapWords) + "." + g.chars(3+g.r.Intn(6), digits)
				case 1:
					return strings.ToLower(g.pick(gapWords)) + "." + g.chars(1+g.r.Intn(4), digits) + "." + g.pick(gapWords)
				case 2:
					return g.pick(gapWords) + "." + g.chars(1, digits) + randomPart(g, 3+g.r.Intn(6))
				}
				return g.chars(1+g.r.Intn(6), digits) + "." + wordsOf(g)
			}
		},
	},
	{
		id: "index.ivar.prefix", shape: "name_index",
		what:    "an instance variable has one or two of '@' or '$' before its name",
		example: ex("password: $$$silver"+"Window[i]", "$$$silver"+"Window"),
		widen:   []edit{{"return len(v)-len(name) <= 2 && name != v", "return len(v)-len(name) <= 3 && name != v"}},
		more: [][]edit{
			// A reviewer's: no count at all.
			{{"return len(v)-len(name) <= 2 && name != v", "return name != v"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				var p strings.Builder
				for k := 3 + n%2; k > 0; k-- {
					p.WriteString(g.pick([]string{"@", "$"}))
				}
				return p.String() + wordsOf(g)
			}
		},
	},
	{
		id: "index.ivar.prefix.chars", shape: "name_index",
		what:    "an instance variable's prefix is of '@' and '$' only",
		example: ex("password: *silver"+"Window[i]", "*silver"+"Window"),
		// The value rules read a name after a '*' or '&' as code (codeHead).
		widen: []edit{{`name := strings.TrimLeft(v, "@$")`, `name := strings.TrimLeft(v, "@$*")`}},
		more: [][]edit{
			// A ':' after the '@', which the value rules then read as code too.
			{{`name := strings.TrimLeft(v, "@$")`, `name := strings.TrimLeft(v, "@$:")`}, {"(?:::|@@?)?` + codeName", "(?:::|@@?:?)?` + codeName"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/2)), func() string {
				p := nthOf([]string{"*", "*@", "*$", "@*"}, n/2)
				if n%2 == 0 {
					// A mark of farPrefix alone, after '@', "@@" or '$', or
					// before '@'.
					c := nth(farPrefix, n/2)
					p = nthOf([]string{c, "@" + c, "@@" + c, "$" + c, c + "@"}, n/2/len(farPrefix))
				}
				return p + wordsOf(g)
			}
		},
	},
	{
		id: "index.ivar.segment", shape: "name_index",
		what:    "an instance variable's name is of letters, digits and '_'",
		example: ex("password: @silver"+"-window[i]", "@silver"+"-window"),
		widen: []edit{
			{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && wordLike(name, 1)"},
			codeNameDash,
		},
		more: [][]edit{
			// A '$' in the name, as the value rules read one in code
			// (codeName).
			{{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && memberSegment.MatchString(strings.ReplaceAll(name, \"$\", \"\")) && wordLike(strings.ReplaceAll(name, \"$\", \"\"), 1)"}},
			// A '?' or '!' at the end of the name, as a member path may have.
			{{`name := strings.TrimLeft(v, "@$")`, `name := strings.TrimRight(strings.TrimLeft(v, "@$"), "?!")`}},
			// Digits before the first word, in the value rules too (codeName).
			{
				{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && memberSegment.MatchString(strings.TrimLeft(name, \"0123456789\")) && wordLike(strings.TrimLeft(name, \"0123456789\"), 1)"},
				{"const codeName = `(?:[A-Za-z_$][A-Za-z0-9_$]*|", "const codeName = `(?:[A-Za-z0-9_$][A-Za-z0-9_$]*|"},
			},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/2)), func() string {
				p := nthOf([]string{"@", "$", "@@"}, n/2)
				if n%4 == 2 {
					// Digits before a name of words.
					return p + g.chars(1+g.r.Intn(4), digits) + strings.ToLower(g.pick(gapWords)) + g.pick([]string{"", "_"}) + strings.ToLower(g.pick(gapWords))
				}
				if n%2 == 0 {
					return p + strings.ToLower(g.pick(gapWords)) + "-" + strings.ToLower(g.pick(gapWords))
				}
				name := strings.ToLower(g.pick(gapWords)) + strings.ToLower(g.pick(gapWords))
				return p + placeMark(g, name, nth(farSegment, n/2/3), n/2/3/len(farSegment))
			}
		},
	},
	{
		id: "index.ivar.words", shape: "name_index",
		what:    "an instance variable's name is made of words",
		example: ex("auth_token: @Vc3Lp"+"8Hy5Mt[i]", "@Vc3Lp"+"8Hy5Mt"),
		widen:   []edit{{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && memberSegment.MatchString(name)"}},
		more: [][]edit{
			// A reviewer's: a short name, of 6 characters or fewer.
			{{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && memberSegment.MatchString(name) && (wordLike(name, 1) || len(name) <= 6)"}},
			// A name that starts with a capital.
			{{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && memberSegment.MatchString(name) && (wordLike(name, 1) || name[0] >= 'A' && name[0] <= 'Z')"}},
			// A name that holds a '_', as a name of words does.
			{{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && memberSegment.MatchString(name) && (wordLike(name, 1) || strings.Contains(name, \"_\"))"}},
			// A long name, of 23 characters or more.
			{{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && memberSegment.MatchString(name) && (wordLike(name, 1) || len(name) >= 23)"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				// Names of every length from what the floor takes, 8 characters
				// with the prefix, to 40.
				p := nthOf([]string{"@", "@@"}, n)
				if n%3 == 2 {
					// A name with a '_' inside, which memberSegment takes.
					return p + separated(g, 6+g.r.Intn(35), "_")
				}
				return p + randomPart(g, 8-len(p)+n/2%(33+len(p)))
			}
		},
	},
	{
		id: "index.ivar.prefixed", shape: "name_index",
		what:    "an instance variable has a prefix: a name of one word alone is none",
		example: ex("password: copper"+"falcon[i]", "copper"+"falcon"),
		widen:   []edit{{"name != v && ", ""}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return strings.ToLower(g.pick(gapWords)) + strings.ToLower(g.pick(gapWords))
			}
		},
	},
	{
		id: "index.block.path", shape: "name_index",
		what:    "a block follows a member path or an instance variable only",
		example: ex("password: Xk9mP2"+"vLq7Rt { x }", "Xk9mP2"+"vLq7Rt"),
		widen:   []edit{{`case path && strings.HasPrefix(strings.TrimLeft(rest, " \t"), "{"):`, `case strings.HasPrefix(strings.TrimLeft(rest, " \t"), "{"):`}},
		more: [][]edit{
			// A block after a name of words too.
			{{`case path && strings.HasPrefix(strings.TrimLeft(rest, " \t"), "{"):`, `case (path || wordLike(v, 2)) && strings.HasPrefix(strings.TrimLeft(rest, " \t"), "{"):`}},
		},
		near: func(g *gen, n int) (string, func() string) {
			// Every value that is no member path or instance variable: a
			// random token, a path of random names, a name of words, a name of
			// lower-case words and one of a word and digits.
			return keyLine(g, nearSeps, nthOf([]string{" { x }", " {", "  { |t| t }", "\t{ x }"}, n/5)), func() string {
				switch n % 5 {
				case 0:
					return nearToken(g)
				case 1:
					return nearToken(g) + "." + nearToken(g)
				case 2:
					return wordsValue(g)
				case 3:
					return strings.ToLower(g.pick(gapWords)) + strings.ToLower(g.pick(gapWords))
				}
				w := g.pick(gapWords)
				return w + g.chars(max(1, 8-len(w))+g.r.Intn(2), digits)
			}
		},
	},
	{
		id: "index.block.blanks", shape: "name_index",
		what:    "a block's '{' has nothing but blanks before it",
		example: ex("password: Walnut"+".Cobalt x { y }", "Walnut"+".Cobalt"),
		widen: []edit{
			{`case path && strings.HasPrefix(strings.TrimLeft(rest, " \t"), "{"):`, `case path && strings.Contains(rest, "{"):`},
			// The value rules read a '{' after more than blanks as no code (codeHead).
			{"[?!]?(?:[(\\[]|[ \\t]*\\{)`", "[?!]?(?:[(\\[]|[^{\\r\\n]*\\{)`"},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" x { y }", " + { y }", " do { y }", " | { y }", " \v{ y }", "\f{ y }"}, n)), func() string {
				return g.pick(gapWords) + "." + g.pick(gapWords)
			}
		},
	},

	// plainCode: a YAML alias.
	{
		id: "alias.star", shape: "yaml_alias",
		what:    "a YAML alias starts with '*': no other mark, such as a tag's '!', makes one",
		example: ex("password: !Summer3"+"Pillow", "!Summer3"+"Pillow"),
		widen: []edit{
			{"case v[0] == '*':", "case v[0] == '*' || v[0] == '!':"},
			// The value rules read only '&' and '*' as a reference (refHead).
			{"refHead = regexp.MustCompile(`^[&*][A-Za-z_]", "refHead = regexp.MustCompile(`^[&*!][A-Za-z_]"},
		},
		near: func(g *gen, n int) (string, func() string) {
			// Every mark that may start the value but '*', '&' (the baseline
			// rules end a value at it), '$' and '@' (a variable), '/' (a
			// regular expression) and quotes and brackets.
			c := nth(without(alnum+"*&$@/\"'`,;<>(){}[]="), n)
			return keyLine(g, []string{": ", ":", " := "}, ""), func() string {
				if n%3 == 2 {
					return c + strings.ToLower(g.pick(gapWords)) + strings.ToLower(g.pick(gapWords))
				}
				return c + wordsValue(g)
			}
		},
	},
	{
		id: "alias.name.chars", shape: "yaml_alias",
		what:    "an alias's name holds letters, digits, '_', '-' and '.' only",
		example: ex("password: *maple:"+"Thunder", "*maple:"+"Thunder"),
		widen: []edit{
			{"func aliasName(name string) bool {\n", "func aliasName(name string) bool {\n\tname = strings.ReplaceAll(name, \":\", \"_\")\n"},
			// The value rules read a reference's name as letters, digits, '_'
			// and '.' (refHead).
			{"refHead = regexp.MustCompile(`^[&*][A-Za-z_][A-Za-z0-9_.]*`)", "refHead = regexp.MustCompile(`^[&*][A-Za-z_][A-Za-z0-9_.:]*`)"},
		},
		more: [][]edit{
			// Any mark read as a separator of words.
			{
				{"func aliasName(name string) bool {\n", "func aliasName(name string) bool {\n\tname = strings.Map(func(r rune) rune {\n\t\tif r < 128 && !isAlnum(byte(r)) {\n\t\t\treturn '_'\n\t\t}\n\t\treturn r\n\t}, name)\n"},
				{"refHead = regexp.MustCompile(`^[&*][A-Za-z_][A-Za-z0-9_.]*`)", "refHead = regexp.MustCompile(`^[&*][A-Za-z_][^ \\t;,)\\]}]*`)"},
			},
			// Digits before the first word, in the value rules too (refHead).
			{
				{"func aliasName(name string) bool {\n", "func aliasName(name string) bool {\n\tname = strings.TrimLeft(name, \"0123456789\")\n"},
				{"refHead = regexp.MustCompile(`^[&*][A-Za-z_][A-Za-z0-9_.]*`)", "refHead = regexp.MustCompile(`^[&*][A-Za-z0-9_][A-Za-z0-9_.]*`)"},
			},
		},
		near: func(g *gen, n int) (string, func() string) {
			if n%4 == 3 {
				// Digits before a name of words.
				return keyLine(g, []string{": ", ":", " := "}, ""), func() string {
					return "*" + g.chars(1+g.r.Intn(4), digits) + strings.ToLower(g.pick(gapWords)) + g.pick(gapWords)
				}
			}
			n = n/4*3 + n%4
			// Every mark the baseline rules take in a value but '-', '.' and
			// '_', between two words or at the end.
			c := nth(without(alnum+"-._\"'`,;&<>(){}[]"), n)
			return keyLine(g, []string{": ", ":", " := "}, ""), func() string {
				a, b := strings.ToLower(g.pick(gapWords)), g.pick(gapWords)
				if n%3 == 2 {
					return "*" + a + b + c
				}
				return "*" + a + c + b
			}
		},
	},
	{
		id: "alias.colon", shape: "yaml_alias",
		what:    "a YAML alias follows ':' only",
		example: ex("secret = *Garden7"+"Pillow", "*Garden7"+"Pillow"),
		widen:   []edit{{"return sep == ':' && aliasName", "return (sep == ':' || sep == '=') && aliasName"}},
		near: func(g *gen, n int) (string, func() string) {
			// Every separator the floor's key takes whose first mark is '='
			// but a spaced "==", a comparison.
			return keyLine(g, []string{" = ", " => ", " =", "=", "=>", "\t=\t", "==", "= ", " =>"}, ""), func() string { return "*" + wordsValue(g) }
		},
	},
	{
		id: "alias.words", shape: "yaml_alias",
		what:    "an alias's name has two words or more, or is one word of lower-case letters",
		example: ex("password: *Lantern"+"sx", "*Lantern"+"sx"),
		widen:   []edit{{"return wordLike(name, 2) ||", "return wordLike(name, 1) ||"}},
		more: [][]edit{
			// A name that holds a '_', as a name of words does.
			{{"return wordLike(name, 2) ||", "return wordLike(name, 2) || strings.Contains(name, \"_\") ||"}},
			// A long name, of 23 characters or more.
			{{"return wordLike(name, 2) ||", "return wordLike(name, 2) || len(name) >= 23 ||"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, []string{": ", ":", " := "}, ""), func() string {
				switch n % 4 {
				case 0:
					return "*" + nearToken(g)
				case 2:
					// A name with '_', '-' or '.' inside, which aliasName takes
					// as separators of words.
					return "*" + separated(g, 9+g.r.Intn(32), "_-.")
				}
				w := g.pick(gapWords)
				return "*" + w + g.chars(max(2, 9-len(w)), "abcdefghijklmnopqrstuvwxyz")
			}
		},
	},
	{
		id: "alias.lower", shape: "yaml_alias",
		what:    "an alias's name of one word is lower-case letters only",
		example: ex("password: *lantern"+"42", "*lantern"+"42"),
		widen:   []edit{{`len(name) >= 3 && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz") == ""`, `len(name) >= 3 && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789") == ""`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, []string{": ", ":", " := "}, ""), func() string {
				w := strings.ToLower(g.pick(gapWords)) + strings.ToLower(g.pick(gapWords))
				if n%2 == 0 {
					return "*" + w + g.chars(1+g.r.Intn(3), digits)
				}
				return "*" + w + g.chars(1, alnum[26:52])
			}
		},
	},
	{
		id: "alias.whole", shape: "yaml_alias",
		what: "an alias's line ends within maxCodeScan of it",
		// A comment, which the value rules cut off the line, so that they read
		// the alias as code and the floor decides.
		example: ex("password: *Velvet4"+"Rocket"+strings.Repeat(" ", 520)+"# c", "*Velvet4"+"Rocket"),
		widen:   []edit{{`aliasName(v[1:]) && whole && strings.TrimSpace(rest) == ""`, `aliasName(v[1:]) && strings.TrimSpace(rest) == ""`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, []string{": ", ":"}, strings.Repeat(" ", maxCodeScan+1+n%8)+g.pick([]string{"x", "# c"})), func() string { return "*" + wordsValue(g) }
		},
	},
	{
		id: "alias.rest", shape: "yaml_alias",
		what:    "an alias has nothing but blanks after it on its line",
		example: ex("password: *Summer2"+"Winter # x", "*Summer2"+"Winter"),
		widen:   []edit{{`aliasName(v[1:]) && whole && strings.TrimSpace(rest) == ""`, `aliasName(v[1:]) && whole && (strings.TrimSpace(rest) == "" || strings.HasPrefix(strings.TrimSpace(rest), "#"))`}},
		near: func(g *gen, n int) (string, func() string) {
			// The value ends at a blank or at a mark the baseline rules end it
			// at (a '(' makes a call, and a '[' or '{' an index); after it
			// comes anything but blanks alone.
			start := nthOf([]string{" ", "\t", "  ", ",", ";", ")", "]", "}", "&", "<", ">", "\v", "\f"}, n)
			rest := nthOf([]string{"# rotated", "and more", "x", "1", "*", "|", "=", ":", "!", "#", "", ",", "- b"}, n/13)
			if rest == "" && strings.TrimSpace(start) == "" {
				rest = "#"
			}
			return keyLine(g, []string{": ", ":", " := "}, start+rest), func() string { return "*" + wordsValue(g) }
		},
	},

	// plainCode: a regular expression literal.
	{
		id: "regex.start", shape: "regex_value",
		what: "a regular expression literal starts with '/': no letter or mark before it, as Perl's qr/ or m/ or Ruby's %r/, makes one",
		// Shorter than 20 characters, which the entropy rule would take after
		// api_key whatever the floor does.
		example: ex("api_key = qr/Fz6Qe3"+"+Nb8vL/i", "qr/Fz6Qe3"+"+Nb8vL/i"),
		// The value rules read only a '/' as the start of a literal
		// (regexHead), so each widening changes them too.
		widen: []edit{
			{"regexHead = regexp.MustCompile(`^/(?:", "regexHead = regexp.MustCompile(`^(?:qr)?/(?:"},
			{"case v[0] == '/':\n\t\treturn !shell && regexLiteral(v) &&", "case v[0] == '/' || strings.HasPrefix(v, \"qr/\"):\n\t\treturn !shell && regexLiteral(v) &&"},
		},
		more: [][]edit{
			// Perl's m/ and Ruby's %r/.
			{
				{"regexHead = regexp.MustCompile(`^/(?:", "regexHead = regexp.MustCompile(`^(?:m|%r)?/(?:"},
				{"case v[0] == '/':\n\t\treturn !shell && regexLiteral(v) &&", "case v[0] == '/' || strings.HasPrefix(v, \"m/\") || strings.HasPrefix(v, \"%r/\"):\n\t\treturn !shell && regexLiteral(v) &&"},
			},
			// Any one to three letters or one mark.
			{
				{"regexHead = regexp.MustCompile(`^/(?:", "regexHead = regexp.MustCompile(`^(?:[A-Za-z]{1,3}|[^A-Za-z0-9/\\s])?/(?:"},
				{"case v[0] == '/':\n\t\treturn !shell && regexLiteral(v) &&", "case regexHead.MatchString(v):\n\t\treturn !shell && regexLiteral(v) &&"},
			},
		},
		near: func(g *gen, n int) (string, func() string) {
			// Letters of Perl's, Ruby's and other languages' literals, random
			// letters, and each mark that may start a value but '/', '*' (an
			// alias), '$' and '@' (a variable), '=' (read as part of the
			// separator) and those the baseline rules end a value at.
			k := n / 2
			p := nthOf([]string{"qr", "m", "%r", "r", "s", "x", "re", "rx", "tr", "y", "qw", "q", "R", "#r", "RE", "Regex", "regex", "new", "pat"}, k)
			if n%2 == 1 {
				p = g.chars(1+g.r.Intn(3), alnum[:52])
				if k%2 == 1 {
					p = nth(without(alnum+"/*$@=\"'`,;&<>(){}[]"), k/2)
				}
			}
			return keyLine(g, nearSeps, ""), func() string { return p + nearRegex(g, true, g.pick(gapFlags)) }
		},
	},
	{
		id: "regex.shell", shape: "regex_value",
		what:    "a regular expression literal is never read in a shell assignment",
		example: ex("SECRET_KEY=/Pk4Nd8"+"+Rw2Hs6Vm1Cx9/n", "/Pk4Nd8"+"+Rw2Hs6Vm1Cx9/n"),
		widen: []edit{
			{"return !shell && regexLiteral(v)", "return regexLiteral(v)"},
			// The value rules read no regular expression after KEY= (isCode).
			{"} else if shell {", "} else if shell && len(v) < 0 {"},
		},
		near: func(g *gen, n int) (string, func() string) {
			return shellLine(g, g.pick([]string{"", " # rotated"})), func() string { return nearRegex(g, true, g.pick(gapFlags)) }
		},
	},
	{
		id: "regex.whole", shape: "regex_value",
		what: "a regular expression literal is the whole value: '/', a body with no '/' unescaped, '/' and flags",
		// After the literal's own flags, what the value rules read as code
		// goes on (codeEnd), then a second '/' and valid flags, so that only
		// this bound refuses it.
		example: ex("SECRET_KEY: /Qm7+Rt2"+"/g.source/i", "/Qm7+Rt2"+"/g.source/i"),
		widen:   []edit{{"if m == nil || m[1] != len(v) {", "if m == nil {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, ""), func() string {
				if n%2 == 1 {
					// Short enough for the entropy rule to leave it to the floor.
					mid := nthOf([]string{"g.source", `\d`, "?.x", "i.test", ".flags", `\w+`}, n/2)
					return "/" + g.mixed(3+g.r.Intn(3)) + "+" + g.chars(2, alnum) + "/" + mid + "/" + g.pick(gapFlags)
				}
				return "/" + g.mixed(5+g.r.Intn(6)) + "+/" + g.mixed(5+g.r.Intn(6)) + "/" + g.pick(gapFlags)
			}
		},
	},
	{
		id: "regex.meta", shape: "regex_value",
		what:    "a regular expression's body holds a metacharacter, one of \\ ^ $ | * + ?",
		example: ex("db_password: /Lw5Gt9"+"Bq3Xe7.Jn2Dk6/g", "/Lw5Gt9"+"Bq3Xe7.Jn2Dk6/g"),
		widen:   []edit{{"strings.ContainsAny(body, `\\^$|*+?`)", "strings.ContainsAny(body, `\\^$|*+?.`)"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, ""), func() string {
				return "/" + insertAt(g, g.mixed(16+g.r.Intn(25)), nth(farMeta, n)) + "/" + g.pick(gapFlags)
			}
		},
	},
	{
		id: "regex.flags.letters", shape: "regex_value",
		what:    "a regular expression's flags are of d g i m s u v y x o e n",
		example: ex("SECRET_KEY: /Qm7Rt2"+"+Wx9Lp4Zk/zq", "/Qm7Rt2"+"+Wx9Lp4Zk/zq"),
		widen:   []edit{{"regexp.MustCompile(`^[dgimsuvyxoen]{0,6}$`)", "regexp.MustCompile(`^[a-z]{0,6}$`)"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, ""), func() string {
				f := g.pick(gapFlags)
				i := g.r.Intn(len(f) + 1)
				return nearRegex(g, true, f[:i]+nth(farFlags, n)+f[i:])
			}
		},
	},
	{
		id: "regex.flags.count", shape: "regex_value",
		what:    "a regular expression has 6 flags at most",
		example: ex("SECRET_KEY: /Hb4Zq8"+"+Lm2Wx6/dgimsuv", "/Hb4Zq8"+"+Lm2Wx6/dgimsuv"),
		widen:   []edit{{"regexp.MustCompile(`^[dgimsuvyxoen]{0,6}$`)", "regexp.MustCompile(`^[dgimsuvyxoen]{0,7}$`)"}},
		more: [][]edit{
			// A reviewer's: up to 12 flags.
			{{"regexp.MustCompile(`^[dgimsuvyxoen]{0,6}$`)", "regexp.MustCompile(`^[dgimsuvyxoen]{0,12}$`)"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, ""), func() string {
				f := []byte(validFlags)
				g.r.Shuffle(len(f), func(i, j int) { f[i], f[j] = f[j], f[i] })
				return nearRegex(g, true, string(f[:7+n%6]))
			}
		},
	},
	{
		id: "regex.flags.repeat", shape: "regex_value",
		what:    "a regular expression has no flag twice",
		example: ex("access_token: /Rc8Vy2"+"+Mh5Kp1Wz4Tb7/gg", "/Rc8Vy2"+"+Mh5Kp1Wz4Tb7/gg"),
		widen:   []edit{{"if seen[f] {", "if seen[f] && false {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, ""), func() string { return nearRegex(g, true, nthOf([]string{"gg", "ii", "mm", "gig", "imsi"}, n)) }
		},
	},
	{
		id: "regex.after", shape: "regex_value",
		what: "after a regular expression comes nothing, a blank, a quote, a backslash, ';' or ','",
		// Shorter than 20 characters, which the entropy rule would take after
		// api_key whatever the floor does.
		example: ex("api_key = /Fz6Qe3"+"+Nb8/i)", "/Fz6Qe3"+"+Nb8/i"),
		widen:   []edit{{"strings.IndexByte(\" \\t;,\\\"'`\\\\\", rest[0])", "strings.IndexByte(\" \\t;,\\\"'`\\\\)\", rest[0])"}},
		more: [][]edit{
			// A reviewer's: a '&', as before a URL's next pair.
			{{"strings.IndexByte(\" \\t;,\\\"'`\\\\\", rest[0])", "strings.IndexByte(\" \\t;,\\\"'`\\\\&\", rest[0])"}},
			// A ']'.
			{{"strings.IndexByte(\" \\t;,\\\"'`\\\\\", rest[0])", "strings.IndexByte(\" \\t;,\\\"'`\\\\]\", rest[0])"}},
		},
		near: func(g *gen, n int) (string, func() string) {
			// Every mark that is no blank, quote, backslash, ';' or ',', and
			// after it each of what the value rules may read as code.
			after := nth(farRegexAfter, n) + nthOf(regexAfterTails, n/len(farRegexAfter))
			return keyLine(g, nearSeps, after), func() string { return nearRegex(g, true, g.pick(gapFlags)) }
		},
	},
}

// ex is an example: text, and the value in it that the baseline rules
// replace.
func ex(text, value string) struct{ text, value string } {
	return struct{ text, value string }{text, value}
}

// widenings returns each widening of b: widen, then those of more.
func (b oracleBound) widenings() [][]edit {
	return append([][]edit{b.widen}, b.more...)
}

// boundsOf returns the bounds whose near cases are of shape.
func boundsOf(shape string) []oracleBound {
	var bs []oracleBound
	for _, b := range oracleBounds {
		if b.shape == shape {
			bs = append(bs, b)
		}
	}
	return bs
}

// nearBoundID returns the id of the bound a near case is of: its name up to
// a colon.
func nearBoundID(c corpusCase) string {
	id, _, _ := strings.Cut(c.name, ":")
	return id
}

// oracleNearShapes are, for each gap shape, the examples of the bounds whose
// near cases are of that shape.
var oracleNearShapes = func() map[string][]struct{ text, value string } {
	m := map[string][]struct{ text, value string }{}
	for _, b := range oracleBounds {
		m[b.shape] = append(m[b.shape], b.example)
	}
	return m
}()

// keptByBound counts, for each bound, the secrets of its near cases,
// labelled and generated, that the baseline rules remove and engine keeps.
func keptByBound(m *measured, engine func(i int) string) map[string][2]int {
	kept := map[string][2]int{}
	for i, c := range m.cases {
		if !oracleNear(c.class) {
			continue
		}
		out := ""
		for k, s := range c.secrets {
			v := c.value(s)
			if keeps(m.baseline[i].out, v) {
				continue
			}
			if k == 0 {
				out = engine(i)
			}
			if keeps(out, v) {
				n := kept[nearBoundID(c)]
				if i < m.corpusCount {
					n[0]++
				} else {
					n[1]++
				}
				kept[nearBoundID(c)] = n
			}
		}
	}
	return kept
}

// formatKept prints keptByBound's counts as id=labelled/generated, sorted.
func formatKept(kept map[string][2]int) string {
	var parts []string
	for id, n := range kept {
		parts = append(parts, fmt.Sprintf("%s=%d/%d", id, n[0], n[1]))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// Every bound has a near case, labelled and generated, of its shape's near
// class, and every near case is of a bound; the labelled one has the text of
// the bound's example. (TestOracleNearShapesAreRemoved checks that the
// baseline rules, v1 and the current rules alone remove each example's
// value, and TestOracle that they remove every near case's.)
func TestOracleBoundsHaveNearCases(t *testing.T) {
	isolate(t)
	m := measure(t)
	shapes := map[string]bool{}
	for _, g := range oracleGapShapes {
		shapes[g.shape] = true
	}
	byID := map[string]oracleBound{}
	idRe := regexp.MustCompile(`^[a-z]+(?:\.[a-z]+)+$`)
	for _, b := range oracleBounds {
		if !idRe.MatchString(b.id) || byID[b.id].id != "" {
			t.Errorf("bound id %q is not a dotted name, or is not unique", b.id)
		}
		byID[b.id] = b
		if !shapes[b.shape] || b.what == "" || len(b.widen) == 0 || b.near == nil {
			t.Errorf("bound %s: shape %q is no gap shape, or it has no line, no widening or no generator", b.id, b.shape)
		}
		if !strings.Contains(b.example.text, b.example.value) || len(b.example.value) < 6 {
			t.Errorf("bound %s: example %q does not hold %q, of 6 characters or more", b.id, b.example.text, b.example.value)
		}
	}
	labelled, generated := map[string]int{}, map[string]int{}
	examples := map[string]bool{}
	for i, c := range m.cases {
		if !oracleNear(c.class) {
			continue
		}
		b, ok := byID[nearBoundID(c)]
		if !ok || "oracle_near_"+b.shape != strings.TrimPrefix(c.class, "gen_") {
			t.Errorf("near case of no bound of its shape: %s", c)
			continue
		}
		if i < m.corpusCount {
			labelled[b.id]++
			examples[b.id] = examples[b.id] || c.text == b.example.text && len(c.secrets) == 1 && c.value(c.secrets[0]) == b.example.value
		} else {
			generated[b.id]++
		}
	}
	// Each bound's generator draws from its far side in turn, so a bound
	// whose share of its near class shrinks, as more bounds of its shape are
	// listed, leaves members of its far side out: raise the class's count
	// in genCounts then.
	minGenerated := 200
	if raceOn {
		minGenerated /= 20
	}
	for _, b := range oracleBounds {
		if labelled[b.id] == 0 || generated[b.id] < minGenerated {
			t.Errorf("bound %s has %d labelled and %d generated near cases, want one labelled and %d generated at least", b.id, labelled[b.id], generated[b.id], minGenerated)
		}
		if !examples[b.id] {
			t.Errorf("bound %s: no labelled near case has the text of its example %q", b.id, b.example.text)
		}
	}
	t.Logf("%d bounds; near cases by bound, labelled: %v; generated: %v", len(oracleBounds), labelled, generated)
}

// applyWidening applies a widening's edits to src, each of whose old text
// must occur exactly once in what the edits before it left.
func applyWidening(src string, widen []edit) (string, error) {
	if len(widen) == 0 {
		return "", fmt.Errorf("a widening of no edit")
	}
	for _, e := range widen {
		if e.old == e.new {
			return "", fmt.Errorf("an edit that changes nothing: %q", e.old)
		}
		if n := strings.Count(src, e.old); n != 1 {
			return "", fmt.Errorf("%q occurs %d times in redact.go, want once", e.old, n)
		}
		src = strings.Replace(src, e.old, e.new, 1)
	}
	return src, nil
}

// Each bound's widening still applies to redact.go: when a change to the
// code of an exception moves the text one edits, this fails on the bounds
// whose near cases and widening to look at again.
func TestOracleBoundsWidenRedactGo(t *testing.T) {
	isolate(t)
	src, err := os.ReadFile("redact.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range oracleBounds {
		for k, w := range b.widenings() {
			if _, err := applyWidening(string(src), w); err != nil {
				t.Errorf("bound %s, widening %d: %v", b.id, k+1, err)
			}
		}
	}
}

// widenBounds runs TestOracleHoldsEachBound, which builds and tests the
// package once for each bound and so takes many minutes.
var widenBounds = flag.Bool("widen-bounds", false, "widen each bound of the exceptions for code in turn and check that TestOracle fails on its near cases")

// boundEnv is the environment the tests started in, before any test moves
// HOME, which the go commands of TestOracleHoldsEachBound find Go's caches
// from.
var boundEnv = os.Environ()

// boundHangGuard bounds one go test run of TestOracleHoldsEachBound from
// above; it never decides an outcome otherwise.
const boundHangGuard = 30 * time.Minute

// For each bound, redact.go widened past it alone (with go test -overlay,
// leaving the file as it is) makes TestOracle fail, on near cases of that
// bound, labelled and generated. Run it with
//
//	go test ./internal/redact -run '^TestOracleHoldsEachBound$' -args -widen-bounds
//
// after a change to an exception for code; -bounds=<regexp> picks bounds by
// id.
func TestOracleHoldsEachBound(t *testing.T) {
	if !*widenBounds {
		t.Skip("run with -args -widen-bounds")
	}
	pick := regexp.MustCompile(*boundPick)
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), boundHangGuard)
	cmd := exec.CommandContext(ctx, goTool, "env", "GOCACHE", "GOMODCACHE")
	cmd.Env = boundEnv
	out, err := cmd.Output()
	cancel()
	caches := strings.Fields(string(out))
	if err != nil || len(caches) != 2 {
		t.Fatalf("go env: %q %v", out, err)
	}
	isolate(t)
	home := os.Getenv("HOME")
	env := append(append([]string{}, boundEnv...), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "GOENV=off",
		"GOCACHE="+caches[0], "GOMODCACHE="+caches[1], "GOPROXY=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly", "GOTELEMETRY=off")
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("redact.go")
	if err != nil {
		t.Fatal(err)
	}
	keptRe := regexp.MustCompile(`near cases kept, by bound: (.*)`)
	for _, b := range oracleBounds {
		if !pick.MatchString(b.id) {
			continue
		}
		for k, w := range b.widenings() {
			name := b.id
			if k > 0 {
				name = fmt.Sprintf("%s#%d", b.id, k+1)
			}
			t.Run(name, func(t *testing.T) {
				widened, err := applyWidening(string(src), w)
				if err != nil {
					t.Fatal(err)
				}
				tmp := t.TempDir()
				file := filepath.Join(tmp, "redact.go")
				overlay := filepath.Join(tmp, "overlay.json")
				ov, _ := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(dir, "redact.go"): file}})
				if err := os.WriteFile(file, []byte(widened), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(overlay, ov, 0o600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), boundHangGuard)
				defer cancel()
				cmd := exec.CommandContext(ctx, goTool, "test", "-count=1", "-overlay", overlay, "-run", "^TestOracle$", ".")
				cmd.Dir, cmd.Env = dir, env
				out, err := cmd.CombinedOutput()
				if err == nil {
					t.Fatalf("TestOracle passed with %s widened:\n%s", name, out)
				}
				got := keptRe.FindSubmatch(out)
				if got == nil {
					t.Fatalf("TestOracle failed with %s widened, but not on a near case:\n%s", name, out)
				}
				counts := map[string]string{}
				for _, f := range strings.Fields(string(got[1])) {
					id, n, _ := strings.Cut(f, "=")
					counts[id] = n
				}
				var l, g int
				if _, err := fmt.Sscanf(counts[b.id], "%d/%d", &l, &g); err != nil || l == 0 || g == 0 {
					t.Errorf("with %s widened, TestOracle kept near cases of %s only, none of its own both labelled and generated (%q)", name, got[1], counts[b.id])
				}
				t.Logf("%s widened: near cases kept: %s", name, got[1])
			})
		}
	}
}

var boundPick = flag.String("bounds", "", "with -widen-bounds, the ids of the bounds to widen, as a regular expression")
