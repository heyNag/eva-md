package redact

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Password shapes the key families of redact.go miss: keys that end in PASS or
// PWD, a key named pass or pwd alone, a password given to a MySQL or MariaDB
// client as -p<password>, to curl as user:password, to sshpass, a registry
// login, redis-cli or openssl on their command lines, and a password written
// in prose, as in "my password is Hunter2!x". What each replaces is on one
// line; only a command written over several lines, each but the last ending
// in a backslash, is read across them. Each rule runs only when its hints or
// gate find what a match needs: a pass or pwd followed by a character
// valueRe allows after a key, a password word followed by a space or tab
// and is or was, or a command's name, whose commands are then read word by
// word. So a text that only says "password" runs none of them.

// passKeyRe is a key's last part, pass or pwd in any case, and its value
// (valueRe). passValue decides from what comes before it what the key is.
const passKeyRe = `(?i:pass|pwd)` + valueRe

// passKeyQuoted and passKeySingle are the same key with its value in double
// or in single quotes, all of it, spaces too, as group 2 (see
// quotedPassValue). Group 1 is the quote that may close a quoted key, as in
// valueRe.
const (
	passKeyQuoted = `(?i:pass|pwd)(["']?)[ \t]*[:=][=>]?[ \t]*"((?:\\.|[^"\\\r\n])+)"`
	passKeySingle = `(?i:pass|pwd)(["']?)[ \t]*[:=][=>]?[ \t]*'((?:\\.|[^'\\\r\n])+)'`
)

// passGate reports whether lower holds pass or pwd followed by a character
// valueRe allows after a key.
func passGate(lower string) bool {
	separator := func(rest string) bool { return rest != "" && strings.IndexByte(":= \t\"'", rest[0]) >= 0 }
	return followed(lower, "pass", separator) || followed(lower, "pwd", separator)
}

// proseGate reports whether lower holds password, passwd, pass or pw, then
// a space or tab and is or was.
func proseGate(lower string) bool {
	verb := func(rest string) bool {
		return len(rest) > 2 && (rest[0] == ' ' || rest[0] == '\t') && (strings.HasPrefix(rest[1:], "is") || strings.HasPrefix(rest[1:], "was"))
	}
	return followed(lower, "pass", func(rest string) bool {
		return verb(rest) || verb(strings.TrimPrefix(rest, "wd")) || verb(strings.TrimPrefix(rest, "word"))
	}) || followed(lower, "pw", verb)
}

// followed reports whether word appears in s followed by text next accepts.
func followed(s, word string, next func(rest string) bool) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], word)
		if j < 0 {
			return false
		}
		i += j + len(word)
		if next(s[i:]) {
			return true
		}
	}
}

// The options that pass a password on a command line. Each pattern is
// matched at the start of a word of its command (see commandMatches) and
// is the option itself: the password is the rest of the word, or, when
// nothing follows the option, the next word, after blanks only (see
// blanks).
const (
	// mysqlPassword is a MySQL or MariaDB client's -p<password>. The
	// password is attached to -p: a -p alone asks for it, and -P is the port.
	mysqlPassword = `^-p`
	// curlPassword is curl's -u or --user, or -U or --proxy-user for a
	// proxy, perhaps after short options that take no value, as in -sSu.
	// Its value is user:password, and the password is what follows its
	// first ':' (see userPassword).
	curlPassword = `^(?:-[0-46aBfgGhiIjJklLMnNOpqRsSvVZ#]*[uU]|--(?:proxy-)?user(?:=|$))`
	// sshpassPassword is sshpass's -p, among its own options (see
	// sshpassOptions).
	sshpassPassword = `^-p`
	// loginPassword is the -p or --password of a docker or podman login (see
	// afterLogin); --password-stdin reads it from stdin.
	loginPassword = `^(?:-p=?|--password(?:=|$))`
	// redisPassword is redis-cli's -a or --pass; its -p is the port.
	redisPassword = `^(?:-a$|--pass(?:=|$))`
	// opensslPassword is openssl's -pass, -passin or -passout, whose value's
	// pass: form holds the password itself (see passArgument); its other
	// forms, as env:, file: or fd:, name where it is.
	opensslPassword = `^-pass(?:in|out)?$`
)

// prosePassword is a password written in prose, as in "my password is
// Hunter2!x" or "the pw was tr0ub4dor&3" (group 1). Only a word that looks
// like a password is taken (see proseValue).
const prosePassword = `(?i)\b(?:password|passwd|pass|pw)[ \t](?:is|was)[ \t]*:?[ \t]*(\S+)`

// command is a command whose options can pass a password, for a rule whose
// pattern is such an option (see commandMatches).
type command struct {
	names []string       // what its name starts with, looked for in the text
	name  *regexp.Regexp // its whole name
	// attached says the password is part of the option's word, as in
	// mysql's -p<password>, whose -p alone asks for it.
	attached bool
	// scope, when set, picks the words of a command that can hold the
	// option, as sshpass's own options before the command it runs. It is
	// given every word of the command, so it can also pick those of a
	// command of the same name that one of them starts (see sshpassOptions).
	scope func(text string, words []span) []span
}

// The commands the rules read.
var (
	mysqlCommand = &command{
		names:    []string{"mysql", "maria"},
		name:     regexp.MustCompile(`^m(?:ysql(?:dump|admin|import|check|show|slap|pump|binlog|sh)?|aria(?:db(?:-[a-z]+)?|backup))$`),
		attached: true,
	}
	curlCommand    = &command{names: []string{"curl"}, name: regexp.MustCompile(`^curl$`)}
	sshpassCommand = &command{names: []string{"sshpass"}, name: sshpassName, scope: sshpassOptions}
	loginCommand   = &command{names: []string{"docker", "podman"}, name: regexp.MustCompile(`^(?:docker|podman)$`), scope: afterLogin}
	redisCommand   = &command{names: []string{"redis-cli"}, name: regexp.MustCompile(`^redis-cli$`)}
	opensslCommand = &command{names: []string{"openssl"}, name: regexp.MustCompile(`^openssl$`)}
)

// sshpassName is sshpass's whole name.
var sshpassName = regexp.MustCompile(`^sshpass$`)

// maxName bounds how far a command's name is read.
const maxName = 32

// commandsRead, when set, is told how many bytes of the text each command
// commandMatches reads runs over. Only tests set it.
var commandsRead func(n int)

// commandMatches returns the matches of re, one of c's options: for each
// command whose first word is c's name, starting where a command word can
// (see commandAt), every word of the command, or of the words c's scope
// picks, that starts with the option, with the password's word, or the rest
// of the option's word, as group 1. So a second -u in a command is judged
// as the first is.
//
// It reads the text twice, and returns the matches of both readings. In
// the first, a quote opens a string whenever its closing quote is on the
// same line, so a ';' or '|' inside a quoted header or SQL statement ends
// nothing, and a backtick ends a command, as inline code in Markdown does.
// In the second, a quote opens a string only when the string is a word of
// its own, from its quote to its closing quote, as most real strings are,
// and so does a backtick, as a command substitution; any other quote is an
// ordinary character. So a quote the first reading pairs wrongly, as an
// apostrophe in o'brien, a quote inside "$(...)" or the closing quote of sh
// -c '...' taken for an opening one, can't hide a password from both. The
// second reading reads an option's value as the first reads a word, so the
// two take the same password the same way. A word that is the option again,
// as the second -u of -u -u admin:pw, is judged as one, not taken as the
// first one's value.
//
// Each reading reads a command's words once, so the cost grows with the
// text, whatever it holds. A name among them starts no command of its own,
// as the second curl in "curl -s x.example/curl -u ...": that command's
// words are the first's that follow the name, which are judged already, or
// which c's scope picks for it. A name inside quotes is the exception, as
// in sh -c 'curl -u ...', since a quoted string is one word of the command
// around it: the text inside the quotes is read again, on its own. Inside
// it, only another kind of quote can open a string, so the kinds nest only
// once each: no text is read more than three times in the first reading,
// and four in the second, which also reads a backtick substitution that is
// a word of its own again. A name right after a quote or backtick, as in
// sh -c 'curl ...', is read up to its string's closing quote, when that
// quote ends a word. match runs a pattern on a word; tests count what it is
// handed.
func commandMatches(text string, re *regexp.Regexp, c *command, match func(*regexp.Regexp, string) []int) [][]int {
	var at []int
	for _, name := range c.names {
		for i := 0; ; {
			j := strings.Index(text[i:], name)
			if j < 0 {
				break
			}
			at = append(at, i+j)
			i += j + len(name)
		}
	}
	sort.Ints(at)
	var out [][]int
	// read reads the commands of text[:hi] that start at the names at, which
	// are in order, each after the last command read; whole picks the second
	// reading.
	var read func(hi int, at []int, whole bool)
	read = func(hi int, at []int, whole bool) {
		end := 0
		for _, i := range at {
			if i < end || !commandAt(text, i) {
				continue
			}
			lim := hi
			if i > 0 && (text[i-1] == '\'' || text[i-1] == '"' || text[i-1] == '`') && (i < 2 || text[i-2] != '\\') {
				if j := closingQuote(text[:hi], i-1); j >= 0 && (j+1 == hi || !wordByte(text[j+1])) {
					lim = j
				}
			}
			n := i
			for n < lim && n-i < maxName && nameByte(text[n]) {
				n++
			}
			if n == lim || text[n] != ' ' && text[n] != '\t' || match(c.name, text[i:n]) == nil {
				continue
			}
			words, quoted, stop := commandWords(text[:lim], n, whole)
			end = stop
			if commandsRead != nil {
				commandsRead(stop - n)
			}
			if c.scope != nil {
				words = c.scope(text[:lim], words)
			}
			for k := 0; k < len(words); k++ {
				w := words[k]
				m := match(re, text[w.start:w.end])
				if m == nil {
					continue
				}
				arg := span{w.start + m[1], w.end}
				if arg.start == arg.end && text[arg.start-1] != '=' {
					// A next word that is the option again, as in -u -u
					// admin:pw, is judged as one, not taken as a value.
					if c.attached || k+1 == len(words) || !blanks(text[w.end:words[k+1].start]) || match(re, text[words[k+1].start:words[k+1].end]) != nil {
						continue
					}
					k++
					arg = words[k]
				}
				if whole {
					// The value is read as the first reading reads a word,
					// so a quoted string in it is whole, as in -p'S3 cret'.
					arg.end = max(arg.end, wordEnd(text[:lim], arg.start))
				}
				out = append(out, []int{w.start, arg.end, arg.start, arg.end})
			}
			for _, q := range quoted {
				read(q.end, at[sort.SearchInts(at, q.start):sort.SearchInts(at, q.end)], whole)
			}
		}
	}
	read(len(text), at, false)
	read(len(text), at, true)
	return out
}

// commandAt reports whether a command word can start at text[i]: at the
// start of the text, after whitespace, a shell operator, a bracket or quote
// around a command, or the '/' of a path, as in /usr/bin/curl.
func commandAt(text string, i int) bool {
	return i == 0 || strings.IndexByte(" \t\r\n\f\v;|&(`\"'/", text[i-1]) >= 0
}

// nameByte reports a byte a command's name can hold.
func nameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}

// commandWords returns the words of the shell command that goes on at
// text[i], and where the command ends: at a line end, unless a backslash
// escapes it, as in a command written over several lines; at a ';', a '|',
// "&&" or a '&' that sends it to the background, but not at the '&' of a
// redirection, as in 2>&1, >&2 or &>file, nor at one inside a word, as in
// an unquoted URL query (see joined), nor at any of them inside a bracket
// the command opened, as in $(cd x && cat y), where they end a word; at a
// ')' that closes a bracket opened before it, as at the end of $(...); or
// at a backtick, which ends a command substitution or, in Markdown, inline
// code. Blanks and the shell's other operators, < > ( and ), end a word. A
// quoted string is part of its word, whatever it holds, when its closing
// quote is on the same line (see closingQuote) and, with whole, when it is
// a word of its own: its quote starts a word, and its closing quote ends
// one. Another quote is an ordinary character, and so is a character a
// backslash escapes. So no word holds a line end. quoted holds what each
// quoted string holds, inside its quotes.
func commandWords(text string, i int, whole bool) (words, quoted []span, stop int) {
	start, depth := -1, 0
	flush := func(at int) {
		if start >= 0 {
			words = append(words, span{start, at})
			start = -1
		}
	}
	for i < len(text) {
		next, inWord := i+1, true
		switch text[i] {
		case '`':
			if j := closingQuote(text, i); whole && j >= 0 && (j+1 == len(text) || !wordByte(text[j+1])) {
				quoted = append(quoted, span{i + 1, j})
				next = j + 1
				break
			}
			flush(i)
			return words, quoted, i
		case '\n':
			flush(i)
			return words, quoted, i
		case ';':
			if depth == 0 {
				flush(i)
				return words, quoted, i
			}
			inWord = false
		case '|':
			if depth == 0 && (i == 0 || text[i-1] != '>') { // >| is a redirection
				flush(i)
				return words, quoted, i
			}
			inWord = false
		case '&':
			switch {
			case redirection(text, i):
				inWord = false
			case joined(text, i):
			case depth > 0:
				inWord = false
			default:
				flush(i)
				return words, quoted, i
			}
		case ')':
			if depth == 0 {
				flush(i)
				return words, quoted, i
			}
			depth--
			inWord = false
		case '(':
			depth++
			inWord = false
		case ' ', '\t', '\r', '\f', '\v', '<', '>':
			inWord = false
		case '\\':
			if n := lineBreak(text, next); n > 0 {
				next += n
				inWord = false
			} else if next < len(text) {
				next++
			}
		case '\'', '"':
			if j := closingQuote(text, i); j >= 0 && (!whole || start < 0 && (j+1 == len(text) || !wordByte(text[j+1]))) {
				quoted = append(quoted, span{i + 1, j})
				next = j + 1
			}
		}
		if !inWord {
			flush(i)
		} else if start < 0 {
			start = i
		}
		i = next
	}
	flush(len(text))
	return words, quoted, len(text)
}

// wordEnd returns where the word that starts at text[i] ends, as the first
// reading reads it (see commandWords): at a blank, a line end or an
// operator, outside a quoted string whose closing quote is on the same line.
func wordEnd(text string, i int) int {
	for i < len(text) {
		switch c := text[i]; {
		case c == '\'' || c == '"':
			if j := closingQuote(text, i); j >= 0 {
				i = j + 1
				continue
			}
		case c == '\\':
			if lineBreak(text, i+1) > 0 {
				return i
			}
			if i+1 < len(text) {
				i++
			}
		case c == '&' && joined(text, i):
		case !wordByte(c):
			return i
		}
		i++
	}
	return i
}

// blanks reports whether s, the text between two words, holds only blanks
// and backslash-newlines, as between an option and its value. An operator
// between them, as the '<' of a placeholder such as <password>, makes the
// next word none of the option's.
func blanks(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\r', '\f', '\v':
		case '\\':
			n := lineBreak(s, i+1)
			if n == 0 {
				return false
			}
			i += n
		default:
			return false
		}
	}
	return true
}

// redirection reports whether the '&' at text[i] is part of a redirection,
// as in 2>&1, >&2, <&0 or &>file, rather than "&&" or a '&' that sends a
// command to the background.
func redirection(text string, i int) bool {
	if i+1 < len(text) && text[i+1] == '&' {
		return false
	}
	return i > 0 && (text[i-1] == '>' || text[i-1] == '<') || i+1 < len(text) && text[i+1] == '>'
}

// joined reports whether the '&' at text[i] is part of a word, as in an
// unquoted URL query such as ?a=1&b=2: the characters on both sides of it
// can be part of a word (see wordByte), so it is neither "&&" nor next to a
// blank, a line end or the text's edge. The shell would send what comes
// before it to the background, but a command pasted into a chat means the
// URL, and its options go on after it.
func joined(text string, i int) bool {
	return i > 0 && i+1 < len(text) && wordByte(text[i-1]) && wordByte(text[i+1])
}

// wordByte reports whether c can be part of a word: neither a blank, a line
// end nor one of the shell's operators.
func wordByte(c byte) bool {
	return strings.IndexByte(" \t\r\n\f\v;|&<>()`", c) < 0
}

// lineBreak returns the length of the line break at text[i], "\n" or
// "\r\n", or 0.
func lineBreak(text string, i int) int {
	switch {
	case i < len(text) && text[i] == '\n':
		return 1
	case i+1 < len(text) && text[i] == '\r' && text[i+1] == '\n':
		return 2
	}
	return 0
}

// closingQuote returns where the string the quote at text[i] opens closes,
// on the same line, or -1. Inside double quotes a backslash escapes the
// next character, but not a line end, and so it does inside bash's $'...'.
func closingQuote(text string, i int) int {
	if text[i] == '`' || text[i] == '\'' && !(i > 0 && text[i-1] == '$' && (i < 2 || text[i-2] != '\\')) {
		if j := strings.IndexAny(text[i+1:], text[i:i+1]+"\n"); j >= 0 && text[i+1+j] == text[i] {
			return i + 1 + j
		}
		return -1
	}
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case text[i]:
			return j
		case '\n':
			return -1
		case '\\':
			if j+1 < len(text) && text[j+1] != '\n' {
				j++
			}
		}
	}
	return -1
}

// sshpassOptions picks sshpass's own options, which end before the command
// it runs, whose -p, as ssh's, is a port: at the first word that is neither
// an option nor the value of -f, -d, -P or -p, which may be the next word.
// A word that starts with -p is the password option even where the option
// before it would take it as a value, as in -d -p S3cret.
// A later word that ends in sshpass's name, where a command can start, as
// in "sshpass -e ssh jump sshpass -p ...", starts another sshpass, whose
// options it picks too: they end as the first's do, or at a ')' that closes
// a bracket opened before its name, as at the end of $(sshpass ...).
func sshpassOptions(text string, words []span) []span {
	var out []span
	in, value, depth := true, false, 0 // in an sshpass's options, the next word is an option's value, the brackets open since its name
	for i, w := range words {
		if i > 0 {
			for _, b := range []byte(text[words[i-1].end:w.start]) {
				switch b {
				case '(':
					depth++
				case ')':
					if depth--; depth < 0 {
						in = false
					}
				}
			}
		}
		switch word := text[w.start:w.end]; {
		case in && value && !strings.HasPrefix(word, "-p"):
			out, value = append(out, w), false
		case in && (word == "-f" || word == "-d" || word == "-P" || word == "-p"):
			out, value = append(out, w), true
		case in && value:
			out, value = append(out, w), false
		case in && strings.HasPrefix(word, "-"):
			out = append(out, w)
		default:
			in, value, depth = startsCommand(text, w, sshpassName), false, 0
		}
	}
	return out
}

// startsCommand reports whether word w ends in a command's name that re
// matches, where a command word can start (see commandAt), with a blank
// after it, so a command of that name starts there.
func startsCommand(text string, w span, re *regexp.Regexp) bool {
	if w.end == len(text) || text[w.end] != ' ' && text[w.end] != '\t' {
		return false
	}
	p := w.end
	for p > w.start && w.end-p < maxName && nameByte(text[p-1]) {
		p--
	}
	return p < w.end && commandAt(text, p) && re.MatchString(text[p:w.end])
}

// afterLogin picks the words of a docker or podman command after its login
// command; another command's -p, as docker run's, is a port. Those words
// hold those of any docker or podman login that a later word starts.
func afterLogin(text string, words []span) []span {
	for i, w := range words {
		if text[w.start:w.end] == "login" {
			return words[i+1:]
		}
	}
	return nil
}

// generated reports whether a provider token, after its prefix, mixes at
// least two of capitals, small letters and digits, as generated tokens do,
// rather than reading as one long word.
func generated(s string) bool {
	if i := strings.IndexAny(s, "_-."); i >= 0 {
		s = s[i+1:]
	}
	n := 0
	for _, has := range []bool{hasUpper(s), hasLower(s), hasDigit(s)} {
		if has {
			n++
		}
	}
	return n >= 2
}

// passValue picks the value of a key whose last part, where match m starts,
// is pass or pwd. Its value is taken as any secret-looking key's is (see
// secretValue), unless what the key is (see passKey) calls for a value
// that looks like a password (see passwordLike). A path after a pwd key is
// kept: PWD and its kin, as in PWD=/Users/me or SAVED_PWD=/tmp/x, hold a
// folder.
func passValue(text string, m []int, marks []span) (span, bool) {
	m, judged, pwd, ok := passKey(text, m)
	if !ok {
		return span{}, false
	}
	s, ok := secretValue(text, m, marks)
	if !ok {
		return span{}, false
	}
	v := text[s.start:s.end]
	if pwd && pathLike(v) || judged && !passwordLike(v) {
		return span{}, false
	}
	return s, true
}

// quotedPassValue picks the value in quotes of a key whose last part, where
// match m starts, is pass or pwd (group 2): all of it, spaces too, when it
// has 8 characters or more and is no variable, command substitution,
// placeholder or op:// reference, and the key (see passKey) calls for no
// more, as passValue has it. A value that starts or ends with a blank is
// code between two strings, whose first ends with the key, as in "DB_PASS="
// + pw + "...", and one that holds another key (see laterKey) ran on past
// its own string.
func quotedPassValue(text string, m []int, marks []span) (span, bool) {
	_, judged, pwd, ok := passKey(text, m)
	if !ok {
		return span{}, false
	}
	s := span{m[4], m[5]}
	v := text[s.start:s.end]
	if len(v) < minValueLen || strings.Trim(v, " \t") != v || !looksSecret(v) || strings.HasPrefix(v, "$(") || laterKey.MatchString(v) || covered(marks, s) || markerStart(v) {
		return span{}, false
	}
	if pwd && pathLike(v) || judged && !passwordLike(v) {
		return span{}, false
	}
	return s, true
}

// passKey decides what the key of match m is, whose last part, pass or pwd,
// starts at m[0], and returns m with the key's start, whether its value
// must look like a password (judged), and whether the part is pwd. ok is
// false when it is no key:
//
//   - after a run of separators or a change of case, as in DB_PASS,
//     MYSQL_PWD, smtp.pass, db-pass, Smtp__Pass, dbPass or DBPass, the part
//     ends a longer key. PASS in capitals, as in an environment variable,
//     and PWD in any case name a password. pass and Pass also name a stage
//     or a test, as in render_pass or onPass, so their value is judged;
//   - inside a longer word, as in BYPASS or OLDPWD, it is no key. The part
//     must end the key, so DB_PASS_FILE is not one either;
//   - alone, pass or pwd names a password in a URL query, a form body or a
//     connection string (after '?', '&' or ';'), and as a long option (after
//     "--"). Elsewhere, as in YAML or an object literal, the word reads as
//     many other things, so the value is judged: pass: true is kept, pass:
//     'S3cr3tPw9!' is not. PASS: in capitals is a test result, as in "---
//     PASS: TestParseV2 (0.00s)", and is no key.
func passKey(text string, m []int) (key []int, judged, pwd, ok bool) {
	at := m[0]
	part := text[at : at+4]
	pwd = text[at+1] == 'w' || text[at+1] == 'W'
	if pwd {
		part = part[:3]
	}
	run := at // the separators before the part, as the "__" of Smtp__Pass
	for run > 0 && strings.IndexByte("_.-", text[run-1]) >= 0 {
		run--
	}
	switch {
	case run < at && run > 0 && isAlnum(text[run-1]),
		at >= 1 && caseChange(text[at-1], part):
		judged = !pwd && part != "PASS"
		start := at
		for start > 0 && (isAlnum(text[start-1]) || strings.IndexByte("_.-", text[start-1]) >= 0) {
			start--
		}
		m = append([]int(nil), m...)
		m[0] = start
	case at > 0 && (isAlnum(text[at-1]) || text[at-1] == '_'):
		return nil, false, false, false
	default:
		strict := at > 0 && strings.IndexByte("?&;", text[at-1]) >= 0 || at >= 2 && text[at-2:at] == "--"
		if sep, _ := keySeparator(text, m); !strict && sep == ':' && part == "PASS" {
			return nil, false, false, false
		}
		judged = !strict
	}
	return m, judged, pwd, true
}

// caseChange reports whether part, a key's last part, starts a new word of
// the key after the character before it, as in dbPass, db2PWD or DBPass.
// Capitals alone after a capital, as in BYPASS or OLDPWD, are one word.
func caseChange(before byte, part string) bool {
	switch {
	case before >= 'a' && before <= 'z' || before >= '0' && before <= '9':
		return part == "Pass" || part == "PASS" || part == "Pwd" || part == "PWD"
	case before >= 'A' && before <= 'Z':
		return part == "Pass" || part == "Pwd"
	}
	return false
}

// flagValue picks the password of an option such as -p<password> or -p
// <password> (group 1 of match m): all of its shell word (see passwordAt).
func flagValue(text string, m []int, marks []span) (span, bool) {
	parts := wordParts(text, m[2], m[3])
	if len(parts) == 0 {
		return span{}, false
	}
	return passwordAt(text, parts, 0, parts[0].inside().start, marks)
}

// userPassword picks the password of a user:password word (group 1 of match
// m): what follows its first ':', inside quotes or not, as in admin:'S3cret'
// or 'admin:S3cret' (see passwordAt). A ':' inside a marker, as when a token
// named the user, is not that one. A word without one names only the user,
// and curl asks for the password.
func userPassword(text string, m []int, marks []span) (span, bool) {
	parts := wordParts(text, m[2], m[3])
	k, at, ok := afterColon(text, parts, marks)
	if !ok {
		return span{}, false
	}
	return passwordAt(text, parts, k, at, marks)
}

// passArgument picks the password of an openssl password argument (group 1
// of match m) in its pass:<password> form: what follows "pass:", inside
// quotes or not (see passwordAt). Its other forms name where the password
// is, and are kept.
func passArgument(text string, m []int, marks []span) (span, bool) {
	parts := wordParts(text, m[2], m[3])
	k, at, ok := afterColon(text, parts, marks)
	if !ok || k != 0 || text[parts[0].inside().start:at] != "pass:" {
		return span{}, false
	}
	return passwordAt(text, parts, k, at, marks)
}

// part is a part of a shell word: a quoted string, from its opening quote
// to just past its closing one, or a run of characters outside quotes.
type part struct {
	span
	quote byte // the quote, or 0
}

// inside returns where p's characters are, inside its quotes.
func (p part) inside() span {
	if p.quote != 0 {
		return span{p.start + 1, p.end - 1}
	}
	return p.span
}

// wordParts splits the shell word text[start:end] into its parts, as
// commandWords reads it: a quote whose string doesn't close within the word
// is an ordinary character, and so is a character a backslash escapes.
func wordParts(text string, start, end int) []part {
	var parts []part
	run := -1
	for i := start; i < end; {
		if q := text[i]; q == '\'' || q == '"' {
			if j := closingQuote(text[:end], i); j >= 0 {
				if run >= 0 {
					parts = append(parts, part{span{run, i}, 0})
					run = -1
				}
				parts = append(parts, part{span{i, j + 1}, q})
				i = j + 1
				continue
			}
		}
		if run < 0 {
			run = i
		}
		if text[i] == '\\' {
			i++
		}
		i++
	}
	if run >= 0 {
		parts = append(parts, part{span{run, end}, 0})
	}
	return parts
}

// afterColon returns where the value of a shell word, split into parts,
// goes on after its first ':' outside markers: the part and the place.
func afterColon(text string, parts []part, marks []span) (int, int, bool) {
	for n, p := range parts {
		in := p.inside()
		for i := in.start; i < in.end; {
			j := strings.IndexByte(text[i:in.end], ':')
			if j < 0 {
				break
			}
			i += j
			if mk, over := markerOver(marks, i); over {
				i = mk.end
				continue
			}
			return n, i + 1, true
		}
	}
	return 0, 0, false
}

// passwordAt picks a password written in a shell word, split into parts,
// from text[at], in parts[k], to the word's end: its span, and whether it
// is a literal to redact (see shellLiteral), judged by its first character
// and the quotes it is inside. A quoted string with nothing in it before
// the password is passed over. When the password is inside a quoted string
// that ends the word, as in -p'S3cret' or admin:"S3cret", the quotes are
// kept around the marker; when it starts a quoted string followed by more,
// as in -p'S3'cret, the opening quote goes with it.
func passwordAt(text string, parts []part, k, at int, marks []span) (span, bool) {
	for k < len(parts) && at >= parts[k].inside().end {
		k++
		if k < len(parts) {
			at = parts[k].inside().start
		}
	}
	if k == len(parts) {
		return span{}, false
	}
	p := parts[k]
	value := span{at, parts[len(parts)-1].end}
	s := value
	switch {
	case p.quote != 0 && k == len(parts)-1:
		value.end = p.end - 1
		s = value
	case p.quote != 0 && at == p.start+1:
		s.start = p.start
	}
	return s, shellLiteral(text, value, p.quote, marks)
}

// shellLiteral reports whether s, a value written in a shell command, which
// starts inside quote or outside quotes (0), is a literal to redact: not
// empty, not already redacted, not a placeholder or an op:// reference, and
// not a variable or command substitution, which the shell expands outside
// single quotes.
func shellLiteral(text string, s span, quote byte, marks []span) bool {
	v := text[s.start:s.end]
	if v == "" || covered(marks, s) || markerStart(v) || opRef(v) || placeholder.MatchString(v) {
		return false
	}
	return quote == '\'' || v[0] != '$' && v[0] != '`'
}

// proseValue picks the password in "password is <word>" (group 1 of match
// m) when it looks like a password (see passwordLike) and is not code: the
// word without the punctuation after it, or the inside of the quotes that
// open it, which may hold spaces up to the line's end.
func proseValue(text string, m []int, marks []span) (span, bool) {
	start, end := m[2], m[3]
	switch q := text[start]; q {
	case '"', '\'', '`':
		start++
		limit := inlineLimit(text, start)
		if i := strings.IndexByte(text[start:limit], q); i >= 0 {
			end = start + i
		}
	case '(':
		start++
	}
	for end > start {
		c := text[end-1]
		if strings.IndexByte(".,;:!?\"'`", c) < 0 && !unopened(text[start:end-1], c) {
			break
		}
		end--
	}
	s := span{start, end}
	v := text[start:end]
	if v == "" || isCode(v, true, false) || strings.Contains(v, marker) || markerStart(v) || overlapsAny(marks, s) || !passwordLike(v) {
		return span{}, false
	}
	return s, true
}

// unopened reports whether c closes a bracket that s, the text before it,
// never opens, as the ')' that ends a remark in brackets.
func unopened(s string, c byte) bool {
	var open byte
	switch c {
	case ')':
		open = '('
	case ']':
		open = '['
	case '}':
		open = '{'
	default:
		return false
	}
	return strings.IndexByte(s, open) < 0
}

// passwordLike reports whether v looks like a password rather than an
// ordinary word: 6 or more characters, a letter, and digits or symbols with
// it, or 12 or more letters that mix cases at random. A hyphen, apostrophe,
// underscore, dot or slash joins words and is no symbol, so case-sensitive,
// user's or n/a are words. So are technical terms such as base64, sha256 or
// utf-8, alone or joined to words, as in base64-encoded, a measurement
// alone, as 16.6ms, and paths, URLs and placeholders. Code is no password
// either: a variable or another shell expansion, which starts with '$', a
// string joined to more, as the +cmd.Dir after "PWD=", or a format verb, as
// in PWD=%s. A value of several words looks like a password when one of
// them does.
func passwordLike(v string) bool {
	if strings.ContainsAny(v, " \t") {
		for _, w := range strings.Fields(v) {
			if passwordLike(w) {
				return true
			}
		}
		return false
	}
	if utf8.RuneCountInString(v) < 6 || !hasLetter(v) || strings.IndexByte("$+%", v[0]) >= 0 || !looksSecret(v) || pathLike(v) || ordinaryWords(v) {
		return false
	}
	symbol := strings.IndexFunc(v, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(joiners, r)
	}) >= 0
	if symbol || hasDigit(v) {
		return true
	}
	words := strings.Map(func(r rune) rune {
		if strings.ContainsRune(joiners, r) {
			return '_'
		}
		return r
	}, v)
	return len(v) >= 12 && hasUpper(v) && hasLower(v) && !wordLike(words, 1)
}

// joiners join the words of an ordinary word, as in case-sensitive.
const joiners = "-'’_./"

// pathLike reports a path or a URL.
func pathLike(v string) bool {
	return strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~/") || strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../") || strings.Contains(v, "://")
}

// technicalTerm is a word with digits that names an encoding, a hash, a
// cipher, a protocol or a length, which prose says a password is or uses.
var technicalTerm = regexp.MustCompile(`^(?i:(?:base|crc)(?:16|32|36|58|62|64|85|91)|sha-?(?:1|2|3|224|256|384|512)|md[245]|hmac(?:-?sha-?\d+)?|utf-?(?:7|8|16|32)(?:mb[34])?|ipv[46]|x\.?509(?:v3)?|pbkdf2|argon2(?:id|i|d)?|aes-?\d+|rsa-?\d+|ed25519|x25519|oauth-?\d|https?-?\d|tls-?1\.?[0-3]|[1-9]password|2fa|\d+-?(?:chars?|characters?|digits?|bytes?|bits?|letters?|words?))$`)

// measurement is a number and its unit, such as a time, a size, a rate or a
// factor, which prose says a render, compiler or test pass took, as in "the
// shadow pass was 16.6ms". A whole number takes a unit of two letters or
// more, as in 120ms, so digits and a letter, as in 1234567s, are none.
var measurement = regexp.MustCompile(`^(?i:\d+(?:[.,]\d+)+(?:[shbx]|` + units + `)|\d+(?:` + units + `))$`)

// units are the units of two letters or more a measurement ends with.
const units = `[nuµμm]s|secs?|mins?|hrs?|[kmgtp]i?b|(?:[kmgtp]i?)?b(?:/s|ps)|fps|[kmg]?hz|px`

// ordinaryWords reports whether v is a technical term or a measurement, or
// words and technical terms joined, as in sha256-hashed or base64_encoded.
// A measurement counts only whole: joined to a word, as the 42ms of
// Hunter_42ms, it is part of a password.
func ordinaryWords(v string) bool {
	if technicalTerm.MatchString(v) || measurement.MatchString(v) {
		return true
	}
	parts := strings.FieldsFunc(v, func(r rune) bool { return strings.ContainsRune(joiners, r) })
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if !technicalTerm.MatchString(p) && strings.IndexFunc(p, func(r rune) bool { return !unicode.IsLetter(r) }) >= 0 {
			return false
		}
	}
	return true
}
