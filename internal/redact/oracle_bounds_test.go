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
	"sort"
	"strings"
	"testing"
	"time"
)

// The bounds of the exceptions for code. Each gap class excuses a whole
// shape (see oracleGapShapes), so the oracle holds the current rules on the
// edges of each shape only where a near case stands. oracleBounds lists,
// from floorValue and plainCode in redact.go and the predicates they call,
// every condition that bounds an exception: each character class, each
// length, each count and each refusal, one line each. Each bound has near
// cases on its far side, where the exception does not hold and the current
// rules alone remove the secret as the baseline rules do: one labelled in
// corpus/oracle_near.txt, whose name starts with the bound's id and a colon,
// and many drawn by the near class's generator, named by the id. A rule
// change that widens an exception past one of its bounds keeps those near
// secrets, and TestOracle fails on them.
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
//   - quotedName's quotes, its length of 3 and its characters: brackets that
//     open a quote close only after the same quote (closeBracket), exprLike
//     refuses brackets with no letter or digit, and wordLike, which the
//     quoted name must also pass, takes exactly its characters.
//   - aliasName's memberSegment and length of 3: wordLike or a name of
//     lower-case letters implies both, and the floor takes 8 characters or
//     more.
//   - memberPath's length of 4 for a short lower-case part: a part of 3 or
//     more lower-case letters is a word for wordLike.
//   - wordLike's characters: every caller also asks for memberSegment or the
//     quoted name's characters.

// edit is a change of redact.go: old, which occurs there exactly once,
// becomes new.
type edit struct{ old, new string }

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
	// near draws the n-th generated near case of the bound: its line, with
	// %s where the value goes, and what makes the value.
	near func(g *gen, n int) (line string, value func() string)
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
func nthOf(from []string, n int) string { return from[n%len(from)] }

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
	farAfter = without(" \t,;)]}.[(\"'`\\")
	// What a type name does not start with or hold.
	farTypeFirst = "0123456789+-.!@*%^~?:|"
	farTypeRest  = "+-!%^~*@:=?/#|$"
	// A prefix that is no '@' or '$'.
	farPrefix = "%!^~-+:?.*|#"
	// A character that is no regular expression metacharacter.
	farMeta = ".-_=~!@%:#"
	// A character that is no part of a variable's name.
	farVariable = "+-.~!%^*=:?/#|@"
)

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
			return keyLine(g, nearSeps, nth(")<>]}[{&,;", n)+g.pick([]string{"", "x"})), func() string { return "*" + nearToken(g) }
		},
	},
	{
		id: "floor.comparison.eq", shape: "yaml_alias",
		what:    "a comparison, after which the floor takes nothing, is a separator with "==", not '=', ':=' or '=>'",
		example: ex("token = *Tq7Wn"+"3Ks9Bv", "*Tq7Wn"+"3Ks9Bv"),
		widen:   []edit{{`strings.Contains(sep, "==") && strings.ContainsAny(sep, " \t")`, `strings.Contains(sep, "=") && strings.ContainsAny(sep, " \t")`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, []string{" = ", " := ", " => ", "  =  "}, ""), func() string { return "*" + nearToken(g) }
		},
	},
	{
		id: "floor.comparison.blank", shape: "yaml_alias",
		what:    "a comparison's "==" has a blank beside it: an unspaced "==" is no comparison",
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
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, ""), func() string {
				if n%4 == 3 {
					return "$" + g.chars(1, digits) + nearToken(g)
				}
				return g.pick([]string{"$", "$", "self.", "config."}) + insertAt(g, nearToken(g), nth(farVariable, n))
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
		},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" =1", " =None", " =x"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.assign.single", shape: "type_annotation",
		what:    "an assignment after a type is a single '=', not "=="",
		example: ex("password: Hb4Zq8"+"Lm2Wx6 == 1", "Hb4Zq8"+"Lm2Wx6"),
		widen:   []edit{{`(?:[ \t]+=[ \t]+|$)`, `(?:[ \t]+==?[ \t]+|$)`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" == 1", " == None", " == x"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.bare", shape: "type_annotation",
		what:    "a type with no union and no assignment after it is no annotation",
		example: ex("password: Xk9mP2"+"vLq7Rt<ab>", "Xk9mP2"+"vLq7Rt"),
		widen:   []edit{{`if line[t[1]-1] == ' ' || line[t[1]-1] == '\t' || t[3] > t[2] {`, `if true {`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"<ab>", "[ab]", "<T>", "[x9]"}, n)), func() string { return nearToken(g) }
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
		what:    "a union of types has a single '|', not "||"",
		example: ex("secret: Nc5Gt1"+"Vx8Kp3 || None", "Nc5Gt1"+"Vx8Kp3"),
		widen:   []edit{{`((?:[ \t]+\|[ \t]+`, `((?:[ \t]+\|\|?[ \t]+`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" || None", " || str"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.name.first", shape: "type_annotation",
		what:    "a type's name starts with a letter or '_'",
		example: ex("password: 7Xk9mP"+"2vLq7Rt = 1", "7Xk9mP"+"2vLq7Rt"),
		widen:   []edit{{"const typeName = `[A-Za-z_][A-Za-z0-9_.]*", "const typeName = `[A-Za-z0-9_][A-Za-z0-9_.]*"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" = 1", " | None", " = x"}, n/len(farTypeFirst))), func() string { return nth(farTypeFirst, n) + nearToken(g) }
		},
	},
	{
		id: "type.name.rest", shape: "type_annotation",
		what:    "a type's name goes on with letters, digits, '_' and '.' only",
		example: ex("password: Xk9mP2"+"+vLq7/Rt = 1", "Xk9mP2"+"+vLq7/Rt"),
		widen:   []edit{{"const typeName = `[A-Za-z_][A-Za-z0-9_.]*", "const typeName = `[A-Za-z_][A-Za-z0-9_.+]*"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" = 1", " | None", " = x"}, n/len(farTypeRest))), func() string { return insertAt(g, nearToken(g), nth(farTypeRest, n)) }
		},
	},
	{
		id: "type.name.index", shape: "type_annotation",
		what:    "an index on a type closes on its line",
		example: ex("password: Gm5Tz1"+"Ry8Pv3[ab = 1", "Gm5Tz1"+"Ry8Pv3"),
		widen:   []edit{{`(?:\[[^\]\r\n]*\]|`, `(?:\[[^\]\r\n]*\]?|`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[ab = 1", "[x9 | None", "[i = x"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.name.generic", shape: "type_annotation",
		what:    "a generic's '<' on a type closes on its line",
		example: ex("password: Bn7Kx2"+"qW4mZ9<ab = 1", "Bn7Kx2"+"qW4mZ9"),
		widen:   []edit{{`|<[^>\r\n]*>)?`, `|<[^>\r\n]*>?)?`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"<ab = 1", "<T | None", "<x = 1"}, n)), func() string { return nearToken(g) }
		},
	},
	{
		id: "type.name.optional", shape: "type_annotation",
		what:    "a type ends in one '?' at most",
		example: ex("password: Vc3Lp8"+"Hy5Mt2?? = 1", "Vc3Lp8"+"Hy5Mt2??"),
		widen:   []edit{{">)?\\??`", ">)?\\?*`"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" = 1", " | None"}, n)), func() string { return nearToken(g) + strings.Repeat("?", 2+n%2) }
		},
	},

	// plainCode: an index or a literal after the value.
	{
		id: "index.closes", shape: "name_index",
		what:    "brackets after the value close on its line",
		example: ex("password: hunter"+"Marble[x9", "hunter"+"Marble"),
		widen:   []edit{{"end, ok := closingBracket(rest)", "end, ok := closingBracket(rest)\n\t\tif !ok && len(rest) > 2 {\n\t\t\tend, ok = len(rest), true\n\t\t}"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[x9", "{ab", "[i", "{a", "[0"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.alnum", shape: "name_index",
		what:    "the brackets hold a letter or a digit",
		example: ex("password: lantern"+"Pillow[--]", "lantern"+"Pillow"),
		widen:   []edit{{"return strings.IndexFunc(s, func(r rune) bool { return r < 128 && isAlnum(byte(r)) }) >= 0 && ", "return "}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[--]", "[+-]", "{..}", "[!]", `[""]`, "{+}"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.semicolon", shape: "name_index",
		what:    "the brackets hold no ';'",
		example: ex("password: lantern"+"Marble[a;b]", "lantern"+"Marble"),
		widen:   []edit{{"!strings.ContainsAny(s, \";~#`\")", "!strings.ContainsAny(s, \"~#`\")"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[a;b]", "[x9;]", "{i;j}"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.tilde", shape: "name_index",
		what:    "the brackets hold no '~'",
		example: ex("password: copper"+"Basket[a~1]", "copper"+"Basket"),
		widen:   []edit{{"!strings.ContainsAny(s, \";~#`\")", "!strings.ContainsAny(s, \";#`\")"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[~1]", "[a~b]", "{x~9}"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.hash", shape: "name_index",
		what:    "the brackets hold no '#'",
		example: ex("password: walnut"+"Rocket[a#1]", "walnut"+"Rocket"),
		widen:   []edit{{"!strings.ContainsAny(s, \";~#`\")", "!strings.ContainsAny(s, \";~`\")"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[a#1]", "{x#2}", "[i#]"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.expr.backtick", shape: "name_index",
		what:    "the brackets hold no backtick",
		example: ex("password: silver"+"Window[`a`]", "silver"+"Window"),
		widen:   []edit{{"!strings.ContainsAny(s, \";~#`\")", "!strings.ContainsAny(s, \";~#\")"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[`a`]", "[`x9`]", "{`i`}"}, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.after", shape: "name_index",
		what:    "after the brackets comes nothing, a blank, a quote, a backslash or one of , ; ) ] } . [ (",
		example: ex("password: harbor"+"Candle[x9]=", "harbor"+"Candle"),
		widen:   []edit{{"strings.IndexByte(\" \\t,;)]}.[(\\\"'`\\\\\", s[0])", "strings.IndexByte(\" \\t,;)]}.[(\\\"'`\\\\=\", s[0])"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/len(farAfter))+nth(farAfter, n)), func() string { return wordsValue(g) }
		},
	},
	{
		id: "index.quoted.words", shape: "name_index",
		what:    "an index by a quoted name, which any value may have before it, holds a name of words",
		example: ex(`api_key: Jd4Hs8`+`Wq2Nc6["Qw3zR8"]`, "Jd4Hs8"+"Wq2Nc6"),
		widen:   []edit{{`== "" && wordLike(strings.NewReplacer("-", "_", ".", "_").Replace(name), 1)`, `== ""`}},
		near: func(g *gen, n int) (string, func() string) {
			q := nthOf([]string{`"`, `'`}, n)
			inside := nthOf([]string{randomPart(g, 6), "ab", "x9", randomPart(g, 4)}, n/2)
			return keyLine(g, nearSeps, "["+q+inside+q+"]"), func() string { return nearToken(g) }
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
		widen:   []edit{{"!shell && memberSegment.MatchString(v) && wordLike(v, 2)", "!shell && wordLike(v, 2)"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return strings.ToLower(g.pick(gapWords)) + "-" + g.pick(gapWords)
			}
		},
	},
	{
		id: "index.name.words", shape: "name_index",
		what:    "a name before an index that is no member path or instance variable has two words or more",
		example: ex("password: Lantern"+"42[0]", "Lantern"+"42"),
		widen:   []edit{{"wordLike(v, 2), true", "wordLike(v, 1), true"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
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
		what:    "a member path has no ".."",
		example: ex("password: Summer"+"..Winter[2]", "Summer"+"..Winter"),
		widen:   []edit{{`strings.Contains(v, "..") || `, ``}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string { return g.pick(gapWords) + ".." + g.pick(gapWords) }
		},
	},
	{
		id: "index.path.trailing", shape: "name_index",
		what:    "a member path does not end in '.'",
		example: ex("password: Summer"+".Winter.[2]", "Summer"+".Winter."),
		widen:   []edit{{` || strings.HasSuffix(v, ".")`, ``}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string { return g.pick(gapWords) + "." + g.pick(gapWords) + "." }
		},
	},
	{
		id: "index.path.segment", shape: "name_index",
		what:    "each name of a member path is of letters, digits and '_'",
		example: ex("password: Summer"+".win-river[2]", "Summer"+".win-river"),
		widen:   []edit{{"if !memberSegment.MatchString(p) || !(wordLike(p, 1)", "if !(wordLike(p, 1)"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return g.pick(gapWords) + "." + strings.ToLower(g.pick(gapWords)) + "-" + strings.ToLower(g.pick(gapWords))
			}
		},
	},
	{
		id: "index.path.part.words", shape: "name_index",
		what:    "each name of a member path is made of words, is one character, or is a short lower-case name",
		example: ex("secret: Bn7Kx2."+"qW4mZ9[2]", "Bn7Kx2."+"qW4mZ9"),
		widen:   []edit{{`if !memberSegment.MatchString(p) || !(wordLike(p, 1) || len(p) == 1 || len(p) <= 4 && strings.Trim(p, "abcdefghijklmnopqrstuvwxyz") == "") {`, `if !memberSegment.MatchString(p) {`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{"[2]", "[i]", "[x9]", " { x }"}, n)), func() string {
				if n%2 == 0 {
					return g.pick(gapWords) + "." + nearToken(g)
				}
				return nearToken(g) + "." + nearToken(g)
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
		id: "index.ivar.prefix", shape: "name_index",
		what:    "an instance variable has one or two of '@' or '$' before its name",
		example: ex("password: $$$silver"+"Window[i]", "$$$silver"+"Window"),
		widen:   []edit{{"return len(v)-len(name) <= 2 && name != v", "return len(v)-len(name) <= 3 && name != v"}},
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
		example: ex("password: %silver"+"Window[i]", "%silver"+"Window"),
		widen:   []edit{{`name := strings.TrimLeft(v, "@$")`, `name := strings.TrimLeft(v, "@$%")`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n/len(farPrefix))), func() string { return nth(farPrefix, n) + wordsOf(g) }
		},
	},
	{
		id: "index.ivar.segment", shape: "name_index",
		what:    "an instance variable's name is of letters, digits and '_'",
		example: ex("password: @silver"+"-window[i]", "@silver"+"-window"),
		widen:   []edit{{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && wordLike(name, 1)"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string {
				return g.pick([]string{"@", "$", "@@"}) + strings.ToLower(g.pick(gapWords)) + "-" + strings.ToLower(g.pick(gapWords))
			}
		},
	},
	{
		id: "index.ivar.words", shape: "name_index",
		what:    "an instance variable's name is made of words",
		example: ex("auth_token: @Vc3Lp"+"8Hy5Mt[i]", "@Vc3Lp"+"8Hy5Mt"),
		widen:   []edit{{"name != v && memberSegment.MatchString(name) && wordLike(name, 1)", "name != v && memberSegment.MatchString(name)"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf(indexTails, n)), func() string { return g.pick([]string{"@", "@@"}) + nearToken(g) }
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
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" { x }", " {", "  { |t| t }"}, n)), func() string {
				if n%2 == 0 {
					return nearToken(g)
				}
				return nearToken(g) + "." + nearToken(g)
			}
		},
	},
	{
		id: "index.block.blanks", shape: "name_index",
		what:    "a block's '{' has nothing but blanks before it",
		example: ex("password: Walnut"+".Cobalt x { y }", "Walnut"+".Cobalt"),
		widen:   []edit{{`case path && strings.HasPrefix(strings.TrimLeft(rest, " \t"), "{"):`, `case path && strings.Contains(rest, "{"):`}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nthOf([]string{" x { y }", " + { y }", " do { y }", " | { y }"}, n)), func() string {
				return g.pick(gapWords) + "." + g.pick(gapWords)
			}
		},
	},

	// plainCode: a YAML alias.
	{
		id: "alias.colon", shape: "yaml_alias",
		what:    "a YAML alias follows ':' only",
		example: ex("secret = *Garden7"+"Pillow", "*Garden7"+"Pillow"),
		widen:   []edit{{"return sep == ':' && aliasName", "return (sep == ':' || sep == '=') && aliasName"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, []string{" = ", " => ", " ="}, ""), func() string { return "*" + wordsValue(g) }
		},
	},
	{
		id: "alias.words", shape: "yaml_alias",
		what:    "an alias's name has two words or more, or is one word of lower-case letters",
		example: ex("password: *Lantern"+"sx", "*Lantern"+"sx"),
		widen:   []edit{{"return wordLike(name, 2) ||", "return wordLike(name, 1) ||"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, []string{": ", ":", " := "}, ""), func() string {
				if n%2 == 0 {
					return "*" + nearToken(g)
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
		what:    "an alias's line ends within maxCodeScan of it",
		example: ex("password: *Velvet4"+"Rocket"+strings.Repeat(" ", 520)+"x", "*Velvet4"+"Rocket"),
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
			return keyLine(g, []string{": ", ":", " := "}, nthOf([]string{" # rotated", " and more", ",", ";", " x", ")", "  #"}, n)), func() string { return "*" + wordsValue(g) }
		},
	},

	// plainCode: a regular expression literal.
	{
		id: "regex.shell", shape: "regex_value",
		what:    "a regular expression literal is never read in a shell assignment",
		example: ex("SECRET_KEY=/Pk4Nd8"+"+Rw2Hs6Vm1Cx9/n", "/Pk4Nd8"+"+Rw2Hs6Vm1Cx9/n"),
		widen:   []edit{{"return !shell && regexLiteral(v)", "return regexLiteral(v)"}},
		near: func(g *gen, n int) (string, func() string) {
			return shellLine(g, g.pick([]string{"", " # rotated"})), func() string { return nearRegex(g, true, g.pick(gapFlags)) }
		},
	},
	{
		id: "regex.whole", shape: "regex_value",
		what:    "a regular expression literal is the whole value: '/', a body with no '/', '/' and flags",
		example: ex("SECRET_KEY: /Qm7+Rt2"+"/Wx9Lp4Zk/g", "/Qm7+Rt2"+"/Wx9Lp4Zk/g"),
		widen:   []edit{{"if m == nil || m[1] != len(v) {", "if m == nil {"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, ""), func() string {
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
		what:    "after a regular expression comes nothing, a blank, a quote, a backslash, ';' or ','",
		example: ex("api_key = /Fz6Qe3"+"+Nb8Ls2Gx5Hj9/i)", "/Fz6Qe3"+"+Nb8Ls2Gx5Hj9/i"),
		widen:   []edit{{"strings.IndexByte(\" \\t;,\\\"'`\\\\\", rest[0])", "strings.IndexByte(\" \\t;,\\\"'`\\\\)\", rest[0])"}},
		near: func(g *gen, n int) (string, func() string) {
			return keyLine(g, nearSeps, nth(")]}<>&[{", n)+g.pick([]string{"", "x"})), func() string { return nearRegex(g, true, g.pick(gapFlags)) }
		},
	},
}

// ex is an example: text, and the value in it that the baseline rules
// replace.
func ex(text, value string) struct{ text, value string } {
	return struct{ text, value string }{text, value}
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
	for _, b := range oracleBounds {
		if labelled[b.id] == 0 || generated[b.id] == 0 {
			t.Errorf("bound %s has %d labelled and %d generated near cases, want one of each at least", b.id, labelled[b.id], generated[b.id])
		}
		if !examples[b.id] {
			t.Errorf("bound %s: no labelled near case has the text of its example %q", b.id, b.example.text)
		}
	}
	t.Logf("%d bounds; near cases by bound, labelled: %v; generated: %v", len(oracleBounds), labelled, generated)
}

// applyWidening applies a bound's edits to src, each of whose old text must
// occur exactly once in what the edits before it left.
func applyWidening(src string, b oracleBound) (string, error) {
	for _, e := range b.widen {
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
		if _, err := applyWidening(string(src), b); err != nil {
			t.Errorf("bound %s: %v", b.id, err)
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
		t.Run(b.id, func(t *testing.T) {
			widened, err := applyWidening(string(src), b)
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
				t.Fatalf("TestOracle passed with %s widened:\n%s", b.id, out)
			}
			got := keptRe.FindSubmatch(out)
			if got == nil {
				t.Fatalf("TestOracle failed with %s widened, but not on a near case:\n%s", b.id, out)
			}
			counts := map[string]string{}
			for _, f := range strings.Fields(string(got[1])) {
				id, n, _ := strings.Cut(f, "=")
				counts[id] = n
			}
			var l, g int
			if _, err := fmt.Sscanf(counts[b.id], "%d/%d", &l, &g); err != nil || l == 0 || g == 0 {
				t.Errorf("with %s widened, TestOracle kept near cases of %s only, none of its own both labelled and generated (%q)", b.id, got[1], counts[b.id])
			}
			t.Logf("%s widened: near cases kept: %s", b.id, got[1])
		})
	}
}

var boundPick = flag.String("bounds", "", "with -widen-bounds, the ids of the bounds to widen, as a regular expression")
