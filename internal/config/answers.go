package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/paths"
)

// The keys of the answers file, config.env.
const (
	KeyWikiDir               = "EVA_MD_WIKI_DIR"
	KeyWikiRemote            = "EVA_MD_WIKI_REMOTE"
	KeyGitHostURL            = "EVA_MD_GIT_HOST_URL"
	KeyHosts                 = "EVA_MD_HOSTS"
	KeyCodexIgnoreUserConfig = "EVA_MD_CODEX_IGNORE_USER_CONFIG"
)

// maxAnswersBytes caps the answers file.
const maxAnswersBytes = 16 << 10

// AnswersTemplate is the answers file setup writes when there is none. Its
// placeholders are generic: the repository is public.
const AnswersTemplate = `# eva-md setup answers for this Mac: fill them in, then run eva-md setup.
# eva-md's own settings are in config.toml beside this file; setup writes that one.
# Edit this file yourself; keep it out of chat and Git. Values are literal:
# shell commands and variable substitutions are never evaluated. A command-line
# flag wins over a value here, for that run only.
#
# Where the wiki lives: a new, empty folder of its own (not Documents, Desktop,
# Downloads or synced storage).
EVA_MD_WIKI_DIR=
# The private repo the wiki is pushed to, such as git@gitlab.example.com:you/eva-wiki.git.
# Leave it empty to have setup create one on EVA_MD_GIT_HOST_URL.
EVA_MD_WIKI_REMOTE=
# Where setup creates the repo when EVA_MD_WIKI_REMOTE is empty: github.com, gitlab.com,
# or a self-hosted GitLab or GitHub Enterprise server, such as https://gitlab.example.com.
# Any page on the host works, such as your profile or projects page: setup reads only
# the host, and creates the repo under the account gh or glab is signed in as there.
EVA_MD_GIT_HOST_URL=
# Which hosts to install: claude, codex or all.
EVA_MD_HOSTS=all
# true keeps your own Codex MCP servers, such as a documentation one, out of
# eva-md's background updates; empty leaves the setting as it is.
EVA_MD_CODEX_IGNORE_USER_CONFIG=
`

// Answers are the answers to setup's questions that a person wrote in
// config.env. An empty string, or a nil switch, is an answer not given.
type Answers struct {
	WikiDir    string // an absolute path, or one starting with ~/
	WikiRemote string
	GitHost    string // the host EVA_MD_GIT_HOST_URL names, as ParseGitHostURL gives it
	Hosts      string // claude, codex or all
	// CodexIgnoreUserConfig sets codex_ignore_user_config in config.toml.
	CodexIgnoreUserConfig *bool
	// Unknown lists the lines whose key setup doesn't know.
	Unknown []UnknownKey
}

// UnknownKey is a line whose key setup doesn't know. Key names it only when
// it looks like one of setup's own, EVA_MD_ then capitals, digits and
// underscores; any other line may hold a secret pasted by mistake, such as a
// base64 token ending in =, so it is named by its number only.
type UnknownKey struct {
	Key  string // "" when the key isn't named
	Line int
}

// ownKeyRe matches a key that looks like one of setup's own.
var ownKeyRe = regexp.MustCompile(`^EVA_MD_[A-Z0-9_]+$`)

// keyRe matches a key: a shell variable's name.
var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// gitHostRe matches a plain host name in lower case, with a port if one is
// needed.
var gitHostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,252}[a-z0-9])?(:[0-9]{1,5})?$`)

// ValidGitHost reports whether host is a plain host name in lower case, such
// as github.com or gitlab.example.com, with no scheme, user or path.
func ValidGitHost(host string) bool { return gitHostRe.MatchString(host) }

// The ways a git host's address can be refused. Each says what is wrong
// after the key or flag that holds it, and never shows the value, which may
// hold a password.
var (
	errGitHostUser = errors.New("must not hold a user name or password; give the host's web address, such as https://gitlab.example.com")
	errGitHostRepo = errors.New("is a repo's address; give the git host's web address, such as https://gitlab.example.com, and a repo's address in EVA_MD_WIKI_REMOTE or --remote")
	errGitHostBad  = errors.New("must be the git host's web address, such as https://gitlab.example.com, or its name, such as gitlab.example.com")
)

// ParseGitHostURL is the host a git host's address names, which is all
// setup uses of it: a web address (https:// or http://) with any path, query
// or fragment, such as a profile or projects page, or a bare host name with
// or without a path. The host is lower case, keeps a port and loses a
// trailing dot, and www.github.com and www.gitlab.com are github.com and
// gitlab.com. An empty value gives "". A user name or password, another
// scheme, a repo's address (ssh:// or git@host:path) and an empty or
// malformed host are refused, with an error that doesn't show the value.
func ParseGitHostURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	rest := value
	if scheme, after, ok := strings.Cut(value, "://"); ok {
		switch strings.ToLower(scheme) {
		case "https", "http":
			rest = after
		case "ssh", "git+ssh", "ssh+git":
			return "", errGitHostRepo
		default:
			return "", errGitHostBad
		}
	} else if strings.HasPrefix(strings.ToLower(value), "git@") {
		return "", errGitHostRepo
	}
	host := rest
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		host = rest[:i]
	}
	if strings.Contains(host, "@") {
		return "", errGitHostUser
	}
	name, port, hasPort := strings.Cut(host, ":")
	if hasPort && rest == value && strings.Trim(port, "0123456789") != "" {
		// As in gitlab.example.com:you/eva-wiki.git, git's short form of an
		// SSH address.
		return "", errGitHostRepo
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	switch name {
	case "www.github.com", "www.gitlab.com":
		name = strings.TrimPrefix(name, "www.")
	}
	if host = name; hasPort {
		host += ":" + port
	}
	if !ValidGitHost(host) {
		return "", errGitHostBad
	}
	return host, nil
}

// LoadAnswers reads the answers file. A missing file gives an error that
// satisfies errors.Is(err, fs.ErrNotExist). It reads only a regular file of
// at most 16 KB of UTF-8, never through a link, and every other error names
// the file, and the key when a value is wrong, but never a value.
func LoadAnswers(file string) (Answers, error) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Answers{}, err
	case errors.Is(err, syscall.ELOOP):
		return Answers{}, fmt.Errorf("%s is a link; setup reads its answers only from a regular file", file)
	case err != nil:
		return Answers{}, fmt.Errorf("%s can't be read", file)
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return Answers{}, fmt.Errorf("%s is not a regular file", file)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAnswersBytes+1))
	switch {
	case err != nil:
		return Answers{}, fmt.Errorf("%s can't be read", file)
	case len(data) > maxAnswersBytes:
		return Answers{}, fmt.Errorf("%s is larger than 16 KB", file)
	}
	return ParseAnswers(data, file)
}

// ParseAnswers reads the answers in data, from file, which only names it in
// errors. Lines are KEY=value, with an optional leading "export "; blank
// lines and lines starting with # are skipped. The value is everything after
// the first =, trimmed, without one pair of matching quotes around it. It is
// taken literally: nothing in it is expanded or run. A key given twice keeps
// its last value.
func ParseAnswers(data []byte, file string) (Answers, error) {
	if !utf8.Valid(data) {
		return Answers{}, fmt.Errorf("%s is not UTF-8 text", file)
	}
	var a Answers
	seen := map[string]bool{}
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimSpace(rest)
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !keyRe.MatchString(key) {
			// The line may hold a secret pasted by mistake, so only its
			// number is named.
			return Answers{}, fmt.Errorf("line %d of %s isn't KEY=value", n+1, file)
		}
		value = unquote(strings.TrimSpace(value))
		switch key {
		case KeyWikiDir:
			if value != "" && !filepath.IsAbs(value) && !strings.HasPrefix(value, "~/") {
				return Answers{}, fmt.Errorf("%s in %s must be a full path or start with ~/", key, file)
			}
			a.WikiDir = value
		case KeyWikiRemote:
			a.WikiRemote = value
		case KeyGitHostURL:
			host, err := ParseGitHostURL(value)
			if err != nil {
				return Answers{}, fmt.Errorf("%s in %s %v", key, file, err)
			}
			a.GitHost = host
		case KeyHosts:
			if value != "" && value != "claude" && value != "codex" && value != "all" {
				return Answers{}, fmt.Errorf("%s in %s must be claude, codex or all", key, file)
			}
			a.Hosts = value
		case KeyCodexIgnoreUserConfig:
			switch strings.ToLower(value) {
			case "":
				a.CodexIgnoreUserConfig = nil
			case "true", "false":
				on := strings.EqualFold(value, "true")
				a.CodexIgnoreUserConfig = &on
			default:
				return Answers{}, fmt.Errorf("%s in %s must be true or false", key, file)
			}
		default:
			if !seen[key] {
				u := UnknownKey{Line: n + 1}
				if ownKeyRe.MatchString(key) {
					u.Key = key
				}
				a.Unknown = append(a.Unknown, u)
			}
		}
		seen[key] = true
	}
	return a, nil
}

// unquote removes one pair of matching double or single quotes around s.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// WriteAnswersTemplate writes AnswersTemplate to file with mode 0600, making
// its folder, unless something is already there, which it never replaces.
// written reports whether it wrote the file.
func WriteAnswersTemplate(file string) (written bool, err error) {
	return writeNew(file, AnswersTemplate)
}

// filledHeader replaces the template's first line in a file setup fills in.
const filledHeader = "# eva-md setup answers for this Mac, as setup left it: edit them, then run eva-md setup."

// FilledAnswers is the answers file for a, in the template's words with its
// values filled in, the git host as https://<host>. A value is quoted when reading it back would otherwise
// change it, and one holding a line break, which no line can hold, is an
// error.
func FilledAnswers(a Answers) (string, error) {
	switchText := func(p *bool) string {
		if p == nil {
			return ""
		}
		return fmt.Sprint(*p)
	}
	hostURL := ""
	if a.GitHost != "" {
		hostURL = "https://" + a.GitHost
	}
	values := map[string]string{KeyWikiDir: a.WikiDir, KeyWikiRemote: a.WikiRemote, KeyGitHostURL: hostURL, KeyHosts: a.Hosts, KeyCodexIgnoreUserConfig: switchText(a.CodexIgnoreUserConfig)}
	lines := strings.Split(AnswersTemplate, "\n")
	lines[0] = filledHeader
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		value, known := values[key]
		if !ok || !known || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(value, "\r\n") {
			return "", fmt.Errorf("%s has a line break", key)
		}
		if value != strings.TrimSpace(value) || unquote(value) != value {
			value = `"` + value + `"`
		}
		lines[i] = key + "=" + value
	}
	text := strings.Join(lines, "\n")
	// Read back, it must give the same answers.
	back, err := ParseAnswers([]byte(text), "the filled-in answers")
	if err != nil {
		return "", err
	}
	if back.WikiDir != a.WikiDir || back.WikiRemote != a.WikiRemote || back.GitHost != a.GitHost || back.Hosts != a.Hosts || switchText(back.CodexIgnoreUserConfig) != switchText(a.CodexIgnoreUserConfig) {
		return "", errors.New("the answers don't read back as written")
	}
	return text, nil
}

// WriteAnswers writes the answers file for a, as FilledAnswers gives it, as
// WriteAnswersTemplate writes the template: mode 0600, never over anything.
func WriteAnswers(file string, a Answers) (written bool, err error) {
	text, err := FilledAnswers(a)
	if err != nil {
		return false, err
	}
	return writeNew(file, text)
}

// writeNew writes text to file with mode 0600, making its folder, unless
// something is already there, a link included, which it never replaces.
func writeNew(file, text string) (written bool, err error) {
	if err := paths.EnsureDir(filepath.Dir(file)); err != nil {
		return false, err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(file)
		return false, err
	}
	return true, nil
}
