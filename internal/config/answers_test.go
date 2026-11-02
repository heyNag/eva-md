package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The answers file is read as written: comments and blank lines skipped, an
// export allowed, one pair of quotes removed, and every value taken
// literally, so a $ or a backtick stays text.
func TestParseAnswers(t *testing.T) {
	isolate(t)
	body := strings.Join([]string{
		"# a comment",
		"",
		"   # an indented comment",
		"export EVA_MD_WIKI_DIR = \"~/projects/eva wiki\"",
		"EVA_MD_WIKI_REMOTE='git@gitlab.example.com:me/$HOME`id`.git'",
		"\texport\tEVA_MD_GIT_HOST_URL=https://GitLab.Example.com/users/your-username/projects",
		"EVA_MD_HOSTS=claude",
		"EVA_MD_HOSTS=codex",
		"EVA_MD_CODEX_IGNORE_USER_CONFIG=True",
		"EVA_MD_TOKEN=fake-secret-7f3a",
		"EVA_MD_TOKEN=again",
		"exported=1",
		"",
	}, "\r\n")
	// A key given twice keeps its last value.
	a, err := ParseAnswers([]byte(body), "config.env")
	if err != nil {
		t.Fatal(err)
	}
	on := true
	want := Answers{
		WikiDir:               "~/projects/eva wiki",
		WikiRemote:            "git@gitlab.example.com:me/$HOME`id`.git",
		GitHost:               "gitlab.example.com",
		Hosts:                 "codex",
		CodexIgnoreUserConfig: &on,
		Unknown:               []UnknownKey{{Key: "EVA_MD_TOKEN", Line: 10}, {Line: 12}},
	}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("got %+v, want %+v", a, want)
	}
	// Empty values are answers not given.
	a, err = ParseAnswers([]byte(AnswersTemplate), "config.env")
	if err != nil || !reflect.DeepEqual(a, Answers{Hosts: "all"}) {
		t.Fatalf("the template gives %+v, %v", a, err)
	}
	a, err = ParseAnswers([]byte("EVA_MD_CODEX_IGNORE_USER_CONFIG=false\nEVA_MD_WIKI_DIR=''\n"), "config.env")
	if err != nil || a.CodexIgnoreUserConfig == nil || *a.CodexIgnoreUserConfig || a.WikiDir != "" {
		t.Fatalf("got %+v, %v", a, err)
	}
}

// EVA_MD_ALLOW_INTERNAL_REPO is true or false, in any case, and empty is an
// answer not given, which asks.
func TestParseAnswersAllowInternalRepo(t *testing.T) {
	isolate(t)
	for value, want := range map[string]string{"true": "true", "FALSE": "false", "": "asks", "''": "asks"} {
		a, err := ParseAnswers([]byte("EVA_MD_ALLOW_INTERNAL_REPO="+value+"\n"), "config.env")
		got := "asks"
		if a.AllowInternalRepo != nil {
			got = fmt.Sprint(*a.AllowInternalRepo)
		}
		if err != nil || got != want || a.CodexIgnoreUserConfig != nil {
			t.Errorf("%q gives %s, %v; want %s", value, got, err, want)
		}
	}
}

// A bad value stops setup with a message naming the key and the file, never
// the value; a line that isn't KEY=value is named by its number.
func TestParseAnswersRefusesBadValues(t *testing.T) {
	isolate(t)
	for body, want := range map[string]string{
		"EVA_MD_HOSTS=both-7f3a\n":                                 "EVA_MD_HOSTS in /c/config.env must be claude, codex or all",
		"EVA_MD_HOSTS=\"codex'\n":                                  "EVA_MD_HOSTS in /c/config.env must be claude, codex or all",
		"EVA_MD_CODEX_IGNORE_USER_CONFIG=yes-7f3a\n":               "EVA_MD_CODEX_IGNORE_USER_CONFIG in /c/config.env must be true or false",
		"EVA_MD_ALLOW_INTERNAL_REPO=yes-7f3a\n":                    "EVA_MD_ALLOW_INTERNAL_REPO in /c/config.env must be true or false",
		"EVA_MD_GIT_HOST_URL=https://me:7f3a@gitlab.example.com\n": "EVA_MD_GIT_HOST_URL in /c/config.env must not hold a user name or password; give the host's web address, such as https://gitlab.example.com",
		"EVA_MD_GIT_HOST_URL=git@gitlab.example.com:me/7f3a.git\n": "EVA_MD_GIT_HOST_URL in /c/config.env is a repo's address; give the git host's web address, such as https://gitlab.example.com, and a repo's address in EVA_MD_WIKI_REMOTE or --remote",
		"EVA_MD_GIT_HOST_URL=ftp://gitlab.example.com/7f3a\n":      "EVA_MD_GIT_HOST_URL in /c/config.env must be the git host's web address, such as https://gitlab.example.com, or its name, such as gitlab.example.com",
		"EVA_MD_WIKI_DIR=projects/wiki-7f3a\n":                     "EVA_MD_WIKI_DIR in /c/config.env must be a full path or start with ~/",
		"# fine\nfake-secret-7f3a\n":                               "line 2 of /c/config.env isn't KEY=value",
		"=fake-secret-7f3a\n":                                      "line 1 of /c/config.env isn't KEY=value",
		"EVA MD=fake-secret-7f3a\n":                                "line 1 of /c/config.env isn't KEY=value",
	} {
		_, err := ParseAnswers([]byte(body), "/c/config.env")
		if err == nil || err.Error() != want {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "7f3a") {
			t.Errorf("%q: the error shows the value: %v", body, err)
		}
	}
}

// An unknown key is named only when it looks like one of setup's own: a
// secret pasted by mistake, as a base64 token whose = padding makes it look
// like KEY=, or any other key, is named by its line number only.
func TestParseAnswersNamesOnlyKeysThatLookLikeSetups(t *testing.T) {
	isolate(t)
	const secret = "ZmFrZVNlY3JldFRva2VuN2YzYTEyMzQ1Njc4OTBhYg"
	body := "# answers\n" + secret + "==\nEVA_MD_WIKI_DRI=~/wiki\nAKIAFAKEKEY7F3A=1\nGITLAB_HOST=gitlab.example.com\neva_md_hosts=all\n"
	a, err := ParseAnswers([]byte(body), "/c/config.env")
	if err != nil {
		t.Fatal(err)
	}
	want := []UnknownKey{{Line: 2}, {Key: "EVA_MD_WIKI_DRI", Line: 3}, {Line: 4}, {Line: 5}, {Line: 6}}
	if !reflect.DeepEqual(a.Unknown, want) {
		t.Fatalf("unknown %+v, want %+v", a.Unknown, want)
	}
	for _, u := range a.Unknown {
		if strings.Contains(u.Key, "ZmFr") || strings.Contains(u.Key, "AKIA") {
			t.Fatalf("a secret is named: %+v", u)
		}
	}
}

// The filled-in file is the template with this Mac's values, quoted where
// reading them back would change them, written as the template is: mode
// 0600, never over a file. A value with a line break is refused.
func TestWriteAnswers(t *testing.T) {
	isolate(t)
	on := false
	a := Answers{WikiDir: "~/projects/eva wiki", WikiRemote: "git@gitlab.example.com:me/eva-wiki.git", GitHost: "gitlab.example.com", Hosts: "all", CodexIgnoreUserConfig: &on}
	file := filepath.Join(t.TempDir(), "eva-md", "config.env")
	if written, err := WriteAnswers(file, a); err != nil || !written {
		t.Fatalf("written %t, %v", written, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info, err)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		"# eva-md setup answers for this Mac: fill them in, then run eva-md setup.", "# eva-md setup answers for this Mac, as setup left it: edit them, then run eva-md setup.",
		"EVA_MD_WIKI_DIR=\n", "EVA_MD_WIKI_DIR=~/projects/eva wiki\n",
		"EVA_MD_WIKI_REMOTE=\n", "EVA_MD_WIKI_REMOTE=git@gitlab.example.com:me/eva-wiki.git\n",
		"EVA_MD_GIT_HOST_URL=\n", "EVA_MD_GIT_HOST_URL=https://gitlab.example.com\n",
		"EVA_MD_CODEX_IGNORE_USER_CONFIG=\n", "EVA_MD_CODEX_IGNORE_USER_CONFIG=false\n").Replace(wantTemplate)
	if string(b) != want {
		t.Fatalf("the file is\n%s\nwant\n%s", b, want)
	}
	if back, err := LoadAnswers(file); err != nil || back.WikiDir != a.WikiDir || back.WikiRemote != a.WikiRemote || back.GitHost != a.GitHost || back.Hosts != "all" || back.CodexIgnoreUserConfig == nil || *back.CodexIgnoreUserConfig {
		t.Fatalf("read back %+v, %v", back, err)
	}
	if written, err := WriteAnswers(file, Answers{Hosts: "codex"}); err != nil || written {
		t.Fatalf("over a file: written %t, %v", written, err)
	}
	if after, _ := os.ReadFile(file); string(after) != string(b) {
		t.Fatalf("the file was replaced: %q", after)
	}
	// A folder that reads back with its quotes removed is quoted.
	text, err := FilledAnswers(Answers{WikiRemote: "'/Volumes/wiki'", Hosts: "all"})
	if err != nil || !strings.Contains(text, "\nEVA_MD_WIKI_REMOTE=\"'/Volumes/wiki'\"\n") {
		t.Fatalf("%v\n%s", err, text)
	}
	if back, err := ParseAnswers([]byte(text), "x"); err != nil || back.WikiRemote != "'/Volumes/wiki'" {
		t.Fatalf("read back %+v, %v", back, err)
	}
	// The internal switch reads back as written.
	text, err = FilledAnswers(Answers{Hosts: "all", AllowInternalRepo: &on})
	if back, perr := ParseAnswers([]byte(text), "x"); err != nil || perr != nil || !strings.Contains(text, "\nEVA_MD_ALLOW_INTERNAL_REPO=false\n") || back.AllowInternalRepo == nil || *back.AllowInternalRepo {
		t.Fatalf("%v %v\n%s", err, perr, text)
	}
	if _, err := FilledAnswers(Answers{WikiDir: "/tmp/a\nEVA_MD_HOSTS=codex"}); err == nil {
		t.Fatal("a line break was written")
	}
}

// A git host's address gives only its host: a web address with any path,
// query or fragment, or a bare host name with or without a path, in lower
// case with its port and without a trailing dot, www.github.com and
// www.gitlab.com read as github.com and gitlab.com; an HTTPS repo address is
// a page like any other. A user name or password, another scheme, an SSH
// repo address and an empty or malformed host are refused without showing
// the value.
func TestParseGitHostURL(t *testing.T) {
	isolate(t)
	for value, want := range map[string]string{
		"":                            "",
		"https://gitlab.example.com":  "gitlab.example.com",
		"https://gitlab.example.com/": "gitlab.example.com",
		"https://gitlab.example.com/your-username":                "gitlab.example.com",
		"https://gitlab.example.com/users/your-username/projects": "gitlab.example.com",
		"HTTPS://GitLab.Example.com./x?tab=1#top":                 "gitlab.example.com",
		"http://gitlab.example.com:8080/x":                        "gitlab.example.com:8080",
		"gitlab.example.com":                                      "gitlab.example.com",
		"gitlab.example.com/your-username":                        "gitlab.example.com",
		"gitlab.example.com:8443/your-username":                   "gitlab.example.com:8443",
		" https://github.com/your-username ":                      "github.com",
		"https://www.github.com/your-username":                    "github.com",
		"www.gitlab.com":                                          "gitlab.com",
		"https://www.example.com":                                 "www.example.com",
		"https://acme.ghe.com/orgs/your-org":                      "acme.ghe.com",
		"https://gitlab.example.com/you/eva-wiki.git":             "gitlab.example.com",
	} {
		if got, err := ParseGitHostURL(value); err != nil || got != want {
			t.Errorf("%q: %q, %v, want %q", value, got, err, want)
		}
	}
	for value, want := range map[string]error{
		"https://me@gitlab.example.com/7f3a":        errGitHostUser,
		"https://me:7f3a@gitlab.example.com":        errGitHostUser,
		"me:7f3a@gitlab.example.com/x":              errGitHostUser,
		"git@gitlab.example.com:me/7f3a.git":        errGitHostRepo,
		"ssh://git@gitlab.example.com/me/7f3a.git":  errGitHostRepo,
		"SSH://gitlab.example.com:2222/me/7f3a.git": errGitHostRepo,
		"gitlab.example.com:me/7f3a.git":            errGitHostRepo,
		"file:///Volumes/7f3a/eva-wiki.git":         errGitHostBad,
		"ftp://gitlab.example.com/7f3a":             errGitHostBad,
		"https://":                                  errGitHostBad,
		"https:///7f3a":                             errGitHostBad,
		"https://gitlab_7f3a.example.com":           errGitHostBad,
		"https://[::1]:7f3a/":                       errGitHostBad,
		"/Volumes/7f3a/eva-wiki.git":                errGitHostBad,
		"gitlab.example.com:/7f3a":                  errGitHostBad,
		"https://gitlab.example.com:7f3a/x":         errGitHostBad,
	} {
		got, err := ParseGitHostURL(value)
		if err != want || got != "" {
			t.Errorf("%q: %q, %v, want %v", value, got, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "7f3a") {
			t.Errorf("%q: the error shows the value: %v", value, err)
		}
	}
}

// The file is read only when it is a regular file of at most 16 KB of
// UTF-8, never through a link.
func TestLoadAnswersIsStrict(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.env")
	if _, err := LoadAnswers(file); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file: %v", err)
	}
	real := filepath.Join(dir, "real.env")
	if err := os.WriteFile(real, []byte("EVA_MD_HOSTS=codex\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if a, err := LoadAnswers(real); err != nil || a.Hosts != "codex" {
		t.Fatalf("a regular file: %+v, %v", a, err)
	}
	if err := os.Symlink(real, file); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnswers(file); err == nil || err.Error() != file+" is a link; setup reads its answers only from a regular file" {
		t.Fatalf("a link: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnswers(file); err == nil || err.Error() != file+" is not a regular file" {
		t.Fatalf("a folder: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	// Exactly 16 KB is read; one byte more is refused.
	full := "EVA_MD_HOSTS=all\n" + strings.Repeat("#", 16<<10-len("EVA_MD_HOSTS=all\n")-1) + "\n"
	if err := os.WriteFile(file, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	if a, err := LoadAnswers(file); err != nil || a.Hosts != "all" {
		t.Fatalf("16 KB: %+v, %v", a, err)
	}
	if err := os.WriteFile(file, []byte(full+"#"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnswers(file); err == nil || err.Error() != file+" is larger than 16 KB" {
		t.Fatalf("an oversize file: %v", err)
	}
	if err := os.WriteFile(file, []byte("EVA_MD_HOSTS=all\n# \xff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnswers(file); err == nil || err.Error() != file+" is not UTF-8 text" {
		t.Fatalf("a file that isn't UTF-8: %v", err)
	}
}

// wantTemplate is the template's exact text.
const wantTemplate = `# eva-md setup answers for this Mac: fill them in, then run eva-md setup.
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
# Whether setup may use an internal repo, which everyone signed in to its GitLab, or every
# member of its GitHub enterprise, can read; the wiki holds summaries of your chats. true
# lets setup, without asking, create one when the host's admins don't allow private
# repos, and use any internal repo you give or it finds, on any host. false refuses one;
# empty asks in a terminal, and refuses one without a terminal.
EVA_MD_ALLOW_INTERNAL_REPO=
# Which hosts to install: claude, codex or all.
EVA_MD_HOSTS=all
# true keeps your own Codex MCP servers, such as a documentation one, out of
# eva-md's background updates; empty leaves the setting as it is.
EVA_MD_CODEX_IGNORE_USER_CONFIG=
`

// The template is written with mode 0600, its folder made, and never
// replaces what is already there, a link included.
func TestWriteAnswersTemplate(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "eva-md", "config.env")
	if written, err := WriteAnswersTemplate(file); err != nil || !written {
		t.Fatalf("written %t, %v", written, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info, err)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != wantTemplate {
		t.Fatalf("the template is %q, %v", b, err)
	}
	if err := os.WriteFile(file, []byte("EVA_MD_HOSTS=codex\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if written, err := WriteAnswersTemplate(file); err != nil || written {
		t.Fatalf("over a file: written %t, %v", written, err)
	}
	if b, _ := os.ReadFile(file); string(b) != "EVA_MD_HOSTS=codex\n" {
		t.Fatalf("the file was replaced: %q", b)
	}
	link := filepath.Join(filepath.Dir(file), "link.env")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if written, err := WriteAnswersTemplate(link); err != nil || written {
		t.Fatalf("over a link: written %t, %v", written, err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("written through the link: %v", err)
	}
}
