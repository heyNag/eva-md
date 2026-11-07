package knowledgepage

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// inScope returns whether a strong row of another project, by its project
// key, is in the chat's scope: a project that shares a system page with the
// chat's, as siblings tells, or one the prompt names (see names). Each key
// is judged once; the prompt is read for names, and siblings asked, only
// when a row first needs them. A chat in a repo whose key hides its name,
// repo-<hash12>, is under an owner of its own, so no name alone names
// another project there.
func inScope(prompt, project string, siblings func() map[string]bool) func(key string) bool {
	_, owner := projectName(project)
	if strings.HasPrefix(project, "repo-") {
		owner = project
	}
	judged := map[string]bool{}
	var said string
	var near map[string]bool
	read, asked := false, false
	return func(key string) bool {
		if in, ok := judged[key]; ok {
			return in
		}
		if !read {
			said, read = lowered(prompt), true
		}
		in := names(said, owner, key)
		if !in && siblings != nil {
			if !asked {
				near, asked = siblings(), true
			}
			in = near[key]
		}
		judged[key] = in
		return in
	}
}

// lowered is the text a prompt names projects in, lowercased: the whole
// prompt, or of one over maxPrompt bytes its first and last half, as its
// terms are read.
func lowered(prompt string) string {
	if len(prompt) > maxPrompt {
		prompt = strings.ToValidUTF8(prompt[:maxPrompt/2], "") + " " + strings.ToValidUTF8(prompt[len(prompt)-maxPrompt/2:], "")
	}
	return strings.ToLower(prompt)
}

// names reports whether a prompt, lowercased, names a project by its key
// (see projectName): it holds the path of the project's owner after the
// host, a slash and its name, as acme/billing for
// github.com__acme__billing and clientb/backend/api for
// gitlab.example.com__clientb__backend__api, or, for an owner that is a
// host alone, the host, a slash or a colon and the name, as
// git.example.com/ledger; a local repo's whole key, as local-notes-1a2b3c;
// or its name alone, when the chat's project, under owner, is under the
// same owner or under none, as a chat outside any repo is, and the name is
// a content word, as a prompt's terms are. Each is matched as a whole word
// (see holds). So a name that two owners' projects share, as api or web
// often is, never brings another owner's knowledge into a chat it wasn't
// asked for in, and neither does a group and name that two owners share,
// as backend/api for clienta/backend/api and clientb/backend/api.
func names(prompt, owner, key string) bool {
	name, theirs := projectName(key)
	switch {
	case name == "":
		return false
	case theirs == "":
		if holds(prompt, key, false) {
			return true
		}
	case holds(prompt, ownerPath(theirs)+"/"+name, false):
		return true
	case !strings.Contains(theirs, "__") && holds(prompt, ownerPath(theirs)+":"+name, false):
		return true
	}
	return (owner == "" || owner == theirs) && content([]byte(name)) && holds(prompt, name, true)
}

// projectName returns the name a prompt can name a project by, and its
// owner, from its key: a remote project's last path part and the rest, as
// billing under github.com__acme for github.com__acme__billing, with "_u"
// read as '_'; a local repo's folder, under no owner, as notes for
// local-notes-1a2b3c. A key that hides the name, as repo-<hash12> and
// local-<hash12> do, and the general area's give none.
func projectName(key string) (name, owner string) {
	if rest, ok := strings.CutPrefix(key, "local-"); ok {
		i := strings.LastIndex(rest, "-")
		if i <= 0 || len(rest)-i-1 != 6 {
			return "", ""
		}
		return rest[:i], ""
	}
	i := strings.LastIndex(key, "__")
	if i <= 0 || i+2 == len(key) {
		return "", ""
	}
	return unescape.Replace(key[i+2:]), key[:i]
}

// unescape reads a project key's escaped '_'.
var unescape = strings.NewReplacer("_u", "_")

// ownerPath is an owner's path as its remote writes it, from the key's
// owner: its parts after the host, joined by slashes, as acme of
// github.com__acme and clientb/backend of
// gitlab.example.com__clientb__backend, or the host itself for an owner
// that is a host alone, as git.example.com.
func ownerPath(owner string) string {
	if _, rest, ok := strings.Cut(owner, "__"); ok {
		owner = strings.ReplaceAll(rest, "__", "/")
	}
	return unescape.Replace(owner)
}

// holds reports whether text holds s as a whole word: with no letter or
// digit right before or after it, nor a '-', '_' or '.' between it and one,
// as billing-api, billing.go or my-billing have for billing. A slash after
// it never joins, so billing/src names billing, and neither does a clone
// URL's .git, so acme/billing.git names acme/billing, though billing.github
// still doesn't name billing; a slash before it joins only when alone, so
// acme/billing doesn't name billing alone, while a host before an owner, as
// in github.com/acme/billing, still names acme/billing.
func holds(text, s string, alone bool) bool {
	joiners := "-_."
	if alone {
		joiners += "/"
	}
	for i := 0; i <= len(text); {
		j := strings.Index(text[i:], s)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(s)
		before, n := utf8.DecodeLastRuneInString(text[:start])
		if strings.ContainsRune(joiners, before) {
			before, _ = utf8.DecodeLastRuneInString(text[:start-n])
		}
		rest := strings.TrimPrefix(text[end:], ".git")
		after, n := utf8.DecodeRuneInString(rest)
		if strings.ContainsRune("-_.", after) {
			after, _ = utf8.DecodeRuneInString(rest[n:])
		}
		if !word(before) && !word(after) {
			return true
		}
		_, n = utf8.DecodeRuneInString(text[start:])
		i = start + n
	}
	return false
}

// word reports a letter or a digit.
func word(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
