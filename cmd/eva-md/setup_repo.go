package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// At the remote question, setup can create the wiki's repo with gh or glab
// when one is signed in, and it asks them whether a pasted repo is private.
// To clone a missing wiki folder again, it asks them for the repo's address.
// Setup is the only place eva-md runs either CLI: the wiki itself, init's
// git steps, the hooks and the daily commit use plain git.

// defaultRepoName is the name setup offers for the wiki's repo.
const defaultRepoName = "eva-wiki"

// repoCLITimeout bounds each gh or glab command; repo create gets three
// times as long. Tests shorten it.
var repoCLITimeout = 10 * time.Second

var (
	errCLIMissing  = errors.New("not installed")
	errCLITimedOut = errors.New("didn't answer in time")
)

// repoNameRe matches a repo name setup passes to gh or glab, after an owner
// or group if the user typed one: letters, digits, '.', '_' and '-', never
// starting with '.' or '-'.
var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}(/[A-Za-z0-9_][A-Za-z0-9_.-]{0,99})*$`)

// hostRe matches a host setup passes to gh or glab and prints.
var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,252}[a-z0-9])?(:[0-9]{1,5})?$`)

// repoCLI is gh or glab working with one host: gh with github.com, glab
// with a GitLab host. envToken is set when that host is glab's default host,
// the one a token in the environment is for.
type repoCLI struct {
	name, host string
	envToken   bool
}

// repoInfo is what a CLI says about a repo.
type repoInfo struct {
	visibility string // private, internal or public
	empty      bool
	ssh, https string // its addresses
}

// repoCLIs remembers which hosts gh and glab are signed in to and glab's
// default host, asking each once.
type repoCLIs struct {
	signedIn    map[repoCLI]bool
	glabOnce    sync.Once
	glabDefault string
	glabErr     error
}

func newRepoCLIs() *repoCLIs { return &repoCLIs{signedIn: map[repoCLI]bool{}} }

// defaultGlabHost is glab's default host, asking glab once. After an error,
// such as a glab that didn't answer in time, setup doesn't ask glab again.
func (r *repoCLIs) defaultGlabHost(ctx context.Context) (string, error) {
	r.glabOnce.Do(func() { r.glabDefault, r.glabErr = glabHost(ctx) })
	return r.glabDefault, r.glabErr
}

// offer starts the remote question when gh or glab is signed in: it offers
// to create the wiki's repo, or to use an existing private one of that name,
// as it is when empty, or after asking and check when it has content. check
// also reads a typed address. It returns the remote, with created when this
// run made the repo or may have; "" means ask for an address, after a line
// saying why when something failed. ok is false when the answers ran out.
func (r *repoCLIs) offer(ctx context.Context, ask func(string) (string, bool), out io.Writer, check func(string) error) (remote string, created, ok bool) {
	if cliInstalled() {
		fmt.Fprintln(out, "   Checking whether gh or glab is signed in.")
	}
	clis, why := r.discover(ctx)
	if why != "" {
		fmt.Fprintln(out, "   "+why)
	}
	if len(clis) == 0 {
		return "", false, true
	}
	c := clis[0]
	hint := "Setup can create it for you. Type another name if you like, or n to paste an address."
	if len(clis) > 1 {
		fmt.Fprintf(out, "   Setup can create it with gh on %s or glab on %s.\n", clis[0].host, clis[1].host)
		choice := ""
		for range 3 {
			answer, ok := ask("   Which one? [GH/glab/n] ")
			if !ok {
				return "", false, false
			}
			// Enter takes gh, the first.
			if choice = strings.ToLower(answer); choice == "" {
				choice = "gh"
			}
			if choice == "gh" || choice == "glab" || choice == "n" || choice == "no" {
				break
			}
			fmt.Fprintln(out, "   Type gh, glab or n")
		}
		switch choice {
		case "gh":
		case "glab":
			c = clis[1]
		default:
			return "", false, true
		}
		hint = "Type another name if you like, or n to paste an address."
	}
	fmt.Fprintln(out, "   "+hint)
	prompt := fmt.Sprintf("   Create a private repo named %s with %s on %s? [Y/n] ", defaultRepoName, c.name, c.host)
	for attempt := range 3 {
		answer, ok := ask(prompt)
		if !ok {
			return "", created, false
		}
		prompt = "   Another name, or an address: "
		answer = unquote(answer)
		switch lower := strings.ToLower(answer); {
		case lower == "n" || lower == "no":
			return "", created, true
		case lower == "y" || lower == "yes" || answer == "" && attempt == 0:
			answer = defaultRepoName
		case answer == "":
			fmt.Fprintln(out, "   Please type an answer, or press Ctrl-C to stop.")
			continue
		case !c.validName(answer):
			if !strings.ContainsAny(answer, ":/@") {
				// Meant as a name: an address has one of these.
				fmt.Fprintln(out, "   Repo names use letters, digits, '.', '_' and '-', with an owner or group before a slash if you like.")
				continue
			}
			// An address, checked as a typed one is.
			remote, err := r.checkAnswer(ctx, answer, out)
			if errors.Is(err, errReadable) {
				err = confirmReadable(ask, out)
			}
			if err == nil {
				err = check(remote)
			}
			if errors.Is(err, errNoAnswer) {
				return "", created, false
			}
			if err != nil {
				fmt.Fprintln(out, "   "+plain(err))
				continue
			}
			return remote, created, true
		}
		remote, made, retry := c.use(ctx, ask, out, answer, check)
		created = created || made
		if !retry {
			return remote, created, true
		}
	}
	return "", created, true
}

// use uses the repo name on c's host when it exists and is private: as it is
// when empty, and when it has content, such as the wiki from another Mac,
// once the user says so and check accepts it. Otherwise it creates the repo.
// It returns the address, with made when the repo was created now or may
// have been. retry means the repo exists but can't hold the wiki, or the user
// chose another. With neither an address nor retry, use has said why it
// failed.
func (c repoCLI) use(ctx context.Context, ask func(string) (string, bool), out io.Writer, name string, check func(string) error) (remote string, made, retry bool) {
	info, err := c.view(ctx, name)
	if err == nil {
		if problem := visibilityProblem(info.visibility); problem != "" {
			fmt.Fprintf(out, "   %s already exists on %s and %s.\n", name, c.host, problem)
			return "", false, true
		}
		if remote, err = c.address(ctx, info); err != nil {
			fmt.Fprintf(out, "   %s couldn't read the address of %s.\n", c.name, name)
			return "", false, false
		}
		if info.empty {
			fmt.Fprintf(out, "   %s already exists on %s, private and empty: using %s.\n", name, c.host, remote)
		} else {
			// Should the answers have run out, the next question finds that.
			if yes, _ := yesNo(ask, out, "   "+name+" already has content: use it as your wiki? [Y/n] ", true); !yes {
				return "", false, true
			}
			if err := check(remote); err != nil {
				if !errors.Is(err, errNoAnswer) {
					fmt.Fprintln(out, "   "+plain(err))
				}
				return "", false, true
			}
		}
		c.sayHTTPS(out, remote)
		return remote, false, false
	}
	if errors.Is(err, errCLITimedOut) {
		fmt.Fprintf(out, "   %s didn't answer in time.\n", c.name)
		return "", false, false
	}
	// gh and glab fail the same way for a repo that doesn't exist and for
	// one they can't reach; create tells the two apart.
	if err := c.create(ctx, name); err != nil {
		if errors.Is(err, errCLITimedOut) {
			// The host may have made it before the answer was lost.
			fmt.Fprintf(out, "   %s didn't answer in time; %s may or may not have been created.\n", c.name, name)
			return "", true, false
		}
		fmt.Fprintf(out, "   %s couldn't create %s.\n", c.name, name)
		return "", false, false
	}
	if info, err = c.view(ctx, name); err == nil {
		if problem := visibilityProblem(info.visibility); problem != "" {
			fmt.Fprintf(out, "   %s created %s, but it %s.\n", c.name, name, problem)
			return "", true, false
		}
		remote, err = c.address(ctx, info)
	}
	if err != nil {
		fmt.Fprintf(out, "   %s created %s but couldn't read back its address.\n", c.name, name)
		return "", true, false
	}
	fmt.Fprintf(out, "   Created %s, a private repo on %s.\n", remote, c.host)
	c.sayHTTPS(out, remote)
	return remote, true, false
}

// sayHTTPS says how git can sign in to push when remote uses HTTPS, as c's
// git protocol has it.
func (c repoCLI) sayHTTPS(out io.Writer, remote string) {
	if !strings.HasPrefix(remote, "https://") {
		return
	}
	fix := "gh auth setup-git"
	if c.name == "glab" {
		fix = "glab auth login --hostname " + c.host + " --setup-git-credential-helper"
	}
	fmt.Fprintf(out, "   Git pushes over HTTPS, as %s is set to. If git can't sign in, run:\n     %s\n", c.name, fix)
}

// checkAnswer checks a typed or pasted remote as setup always has, then
// with checkPrivate.
func (r *repoCLIs) checkAnswer(ctx context.Context, answer string, out io.Writer) (string, error) {
	a := unquote(answer)
	if _, err := wikiinit.CheckRemote(a); err != nil {
		return "", fmt.Errorf("%s. Paste an address like git@github.com:you/eva-wiki.git or https://github.com/you/eva-wiki.git", strings.TrimSuffix(plain(err), "."))
	}
	return a, r.checkPrivate(ctx, a, out)
}

// checkPrivate refuses a remote, one wikiinit.CheckRemote accepts, when the
// CLI for its host says the repo is not private: gh for github.com, glab for
// a GitLab host it is signed in to. When no CLI can tell, git reads the repo
// over HTTPS with nothing that could sign it in (anyoneCanRead), saying so
// on out first: one it reads is errReadable, which setup asks about, and one
// it can't read passes.
func (r *repoCLIs) checkPrivate(ctx context.Context, remote string, out io.Writer) error {
	id, err := wikiinit.CheckRemote(remote)
	if err != nil {
		return nil
	}
	c, path, ok := r.cliFor(ctx, id)
	if !ok {
		// With no CLI to tell, a repo git reads with no sign-in may be
		// public: setup asks before it uses one (see confirmReadable).
		if !strings.HasPrefix(id, "file://") {
			fmt.Fprintf(out, "   Checking whether %s can be read without signing in.\n", id)
		}
		if anyoneCanRead(ctx, id) {
			return errReadable
		}
		return nil
	}
	info, err := c.view(ctx, path)
	if err != nil {
		return nil
	}
	if problem := visibilityProblem(info.visibility); problem != "" {
		return errors.New("this repo " + problem)
	}
	return nil
}

// cloneAddress is an address to clone the wiki again from, for the remote
// whose identity the config records: a local remote's path; the address gh
// or glab gives, as for a repo setup creates, when it names that repo; or
// else the SSH address.
func (r *repoCLIs) cloneAddress(ctx context.Context, id string) string {
	if p, ok := strings.CutPrefix(id, "file://"); ok {
		return filepath.FromSlash(p)
	}
	if c, path, ok := r.cliFor(ctx, id); ok {
		if info, err := c.view(ctx, path); err == nil {
			// A renamed or moved repo answers to its old name with its new
			// address, which init would refuse.
			if remote, err := c.address(ctx, info); err == nil {
				if got, _ := wikiinit.CheckRemote(remote); got == id {
					return remote
				}
			}
		}
	}
	host, path, _ := strings.Cut(id, "/")
	return "git@" + host + ":" + path + ".git"
}

// cliFor is the CLI that can tell about the repo with this identity, with
// the repo's path on its host: gh for github.com, or glab for another host,
// when it is installed and signed in there.
func (r *repoCLIs) cliFor(ctx context.Context, id string) (c repoCLI, path string, ok bool) {
	if strings.HasPrefix(id, "file://") {
		return c, "", false
	}
	host, path, _ := strings.Cut(id, "/")
	c = repoCLI{name: "glab", host: host}
	if host == "github.com" {
		c.name = "gh"
	}
	if !c.validName(path) || !hostRe.MatchString(host) {
		return c, "", false
	}
	if c.name == "glab" {
		// glab gets a token from the environment only for its default
		// host, so a glab that can't name that host isn't asked.
		def, err := r.defaultGlabHost(ctx)
		if err != nil {
			return c, "", false
		}
		c.envToken = host == def
	}
	return c, path, r.ready(ctx, c)
}

// cliInstalled reports whether gh or glab is on PATH, so that setup says it
// asks them before the wait.
func cliInstalled() bool {
	for _, name := range []string{"gh", "glab"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

// discover asks gh whether it is signed in to github.com and glab whether
// it is signed in to its default GitLab host, both at once, and returns
// those that are. When none is but one is installed, why says in a line
// why setup can't create the repo.
func (r *repoCLIs) discover(ctx context.Context) (clis []repoCLI, why string) {
	found := []struct {
		cli repoCLI
		err error
	}{{cli: repoCLI{name: "gh", host: "github.com"}}, {cli: repoCLI{name: "glab", envToken: true}}}
	var wg sync.WaitGroup
	for i := range found {
		wg.Go(func() {
			f := &found[i]
			if f.cli.name == "glab" {
				if f.cli.host, f.err = r.defaultGlabHost(ctx); f.err != nil {
					return
				}
			}
			f.err = f.cli.authStatus(ctx)
		})
	}
	wg.Wait()
	var reasons []string
	for _, f := range found {
		if errors.Is(f.err, errCLIMissing) {
			continue
		}
		if f.cli.host != "" {
			r.signedIn[f.cli] = f.err == nil
		}
		switch {
		case f.err == nil:
			clis = append(clis, f.cli)
		case errors.Is(f.err, errCLITimedOut):
			reasons = append(reasons, f.cli.name+" didn't answer in time")
		case f.cli.host == "":
			reasons = append(reasons, f.cli.name+" isn't signed in")
		default:
			reasons = append(reasons, f.cli.name+" isn't signed in to "+f.cli.host)
		}
	}
	if len(clis) == 0 && len(reasons) > 0 {
		why = strings.Join(reasons, " and ") + ", so setup can't create the repository."
	}
	return clis, why
}

// ready reports whether c is installed and signed in to its host.
func (r *repoCLIs) ready(ctx context.Context, c repoCLI) bool {
	ok, asked := r.signedIn[c]
	if !asked {
		ok = c.authStatus(ctx) == nil
		r.signedIn[c] = ok
	}
	return ok
}

// glabHost is the GitLab host glab works with by default, as `glab config
// get host` names it from GITLAB_HOST or glab's config, or gitlab.com when
// neither sets one.
func glabHost(ctx context.Context) (string, error) {
	out, err := repoCLI{name: "glab"}.run(ctx, repoCLITimeout, "config", "get", "host")
	if err != nil {
		return "", err
	}
	host := strings.TrimSpace(string(out))
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.ToLower(strings.TrimSuffix(host, "/"))
	switch {
	case host == "":
		return "gitlab.com", nil
	case !hostRe.MatchString(host):
		return "", errors.New("glab: unusable host")
	}
	return host, nil
}

func (c repoCLI) authStatus(ctx context.Context) error {
	_, err := c.run(ctx, repoCLITimeout, "auth", "status", "--hostname", c.host)
	return err
}

// validName reports whether name is a repo name c takes: gh takes OWNER/REPO
// at most, glab nested groups too.
func (c repoCLI) validName(name string) bool {
	return repoNameRe.MatchString(name) && (c.name != "gh" || strings.Count(name, "/") <= 1)
}

// view reads what c's host says about the repo name. An error means c can't
// tell, as for a repo that doesn't exist.
func (c repoCLI) view(ctx context.Context, name string) (repoInfo, error) {
	args := []string{"repo", "view", name, "--output", "json"}
	if c.name == "gh" {
		args = []string{"repo", "view", name, "--json", "visibility,isEmpty,sshUrl,url"}
	}
	out, err := c.run(ctx, repoCLITimeout, args...)
	if err != nil {
		return repoInfo{}, err
	}
	// gh names the fields as GitHub's GraphQL API does, glab prints the
	// project as GitLab's REST API does.
	var v struct {
		Visibility    string `json:"visibility"`
		IsEmpty       *bool  `json:"isEmpty"`
		SSHURL        string `json:"sshUrl"`
		URL           string `json:"url"`
		EmptyRepo     *bool  `json:"empty_repo"`
		SSHURLToRepo  string `json:"ssh_url_to_repo"`
		HTTPURLToRepo string `json:"http_url_to_repo"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return repoInfo{}, fmt.Errorf("%s repo view: unexpected output", c.name)
	}
	info := repoInfo{visibility: strings.ToLower(v.Visibility), ssh: v.SSHURLToRepo, https: v.HTTPURLToRepo}
	empty := v.EmptyRepo
	if c.name == "gh" {
		info.ssh, empty = v.SSHURL, v.IsEmpty
		if v.URL != "" {
			info.https = v.URL + ".git"
		}
	}
	if empty == nil || info.visibility != "private" && info.visibility != "internal" && info.visibility != "public" {
		return repoInfo{}, fmt.Errorf("%s repo view: unexpected output", c.name)
	}
	info.empty = *empty
	return info, nil
}

// create creates the private repo name on c's host.
func (c repoCLI) create(ctx context.Context, name string) error {
	args := []string{"repo", "create", name, "--private"}
	if c.name == "glab" {
		// No git init or clone in the current folder, and no question
		// about one.
		args = append(args, "--skipGitInit")
	}
	_, err := c.run(ctx, 3*repoCLITimeout, args...)
	return err
}

// address is the repo's address for git: SSH, or HTTPS when c's git
// protocol for its host is HTTPS. It passes setup's remote check and is
// never a local path.
func (c repoCLI) address(ctx context.Context, info repoInfo) (string, error) {
	remote := info.ssh
	if out, err := c.run(ctx, repoCLITimeout, "config", "get", "git_protocol", "--host", c.host); err == nil && strings.TrimSpace(string(out)) == "https" {
		remote = info.https
	}
	if id, err := wikiinit.CheckRemote(remote); err != nil || strings.HasPrefix(id, "file://") {
		return "", fmt.Errorf("%s gave an unusable address", c.name)
	}
	return remote, nil
}

// visibilityProblem says why a repo with this visibility can't hold the
// wiki, or "" for a private one. An internal repo is readable across its
// GitHub enterprise or by anyone signed in to its GitLab instance.
// errReadable is a repo git read over HTTPS with no sign-in, on a host no
// CLI can tell about: it may be public.
var errReadable = errors.New("git read this repo over HTTPS without signing in, so it may be public; the wiki holds your chats' summaries, so use a private repo")

// confirmReadable asks before setup uses a repo git read with no sign-in,
// no being the default: a host can grant reads to its own network, so a
// private repo there reads the same.
func confirmReadable(ask func(string) (string, bool), out io.Writer) error {
	use, ok := yesNo(ask, out, "   git read this repo over HTTPS without signing in, so it may be public, and the wiki holds your chats' summaries. Use it anyway? [y/N] ", false)
	switch {
	case !ok:
		return errNoAnswer
	case !use:
		return errors.New("choose a private repo")
	}
	return nil
}

// anyoneCanRead reports whether git reads the repo with this identity over
// HTTPS with no sign-in, as anyone can read a public repo. A repo it can't
// read, for any reason, isn't known to be public.
var anyoneCanRead = func(ctx context.Context, id string) bool {
	if strings.HasPrefix(id, "file://") {
		return false
	}
	home, err := os.MkdirTemp("", "eva-md-probe-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(home)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return probe(ctx, id, home).Run() == nil
}

// probe is the read anyoneCanRead runs, with nothing that could sign it in:
// none of the user's git config, so no credential helper, header or URL
// rewrite; an empty home folder, so no .netrc; no Kerberos ticket, and no
// empty-user authentication that would offer one; no proxy; no prompt.
func probe(ctx context.Context, id, home string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", "-c", "credential.helper=", "-c", "http.emptyAuth=false", "ls-remote", "--quiet", "--", "https://"+id+".git")
	cmd.Dir = home
	for _, kv := range wikigit.Environment() {
		key, _, _ := strings.Cut(kv, "=")
		switch upper := strings.ToUpper(key); {
		case strings.HasPrefix(key, "GIT_"), strings.HasSuffix(key, "ASKPASS"), strings.HasPrefix(upper, "KRB5"), strings.HasSuffix(upper, "_PROXY"),
			key == "HOME", key == "XDG_CONFIG_HOME", key == "NETRC", key == "CURL_HOME":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "KRB5CCNAME=FILE:/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https")
	return cmd
}

func visibilityProblem(visibility string) string {
	switch visibility {
	case "private":
		return ""
	case "internal":
		return "is internal, so other members of its enterprise or instance can read it; the wiki holds your chats' summaries, so use a private repo"
	}
	return "is public; the wiki holds your chats' summaries, so use a private repo"
}

// run runs c for its host and returns what it printed. It finds c on PATH
// and runs it in /, outside any repository, with no input, the environment
// repoCLIEnv gives and timeout. What it writes to stderr goes to the null
// device and its output is capped: either can show account details, so an
// error names only the command.
func (c repoCLI) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	path, err := exec.LookPath(c.name)
	if err != nil {
		return nil, errCLIMissing
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = "/"
	cmd.Env = repoCLIEnv(c)
	cmd.WaitDelay = time.Second
	var out cliOutput
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errCLITimedOut
		}
		return nil, fmt.Errorf("%s %s failed", c.name, strings.Join(args[:2], " "))
	}
	return out.Bytes(), nil
}

// repoCLIEnv is the environment c runs with: git's (wikigit.Environment
// drops what could point git at another repository), without what could
// point gh or glab at another repository or group, force terminal output or
// write debug logs, with prompts, colour, update checks and notices off and
// c's host as the default host. A GitLab token in the environment stays only
// with c.envToken, since glab would trust whatever host setup names with it.
func repoCLIEnv(c repoCLI) []string {
	var env []string
	for _, kv := range wikigit.Environment() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "GH_REPO", "GITLAB_REPO", "GITLAB_GROUP", "GH_FORCE_TTY", "CLICOLOR_FORCE", "GLAB_COLOR", "GLAB_FORCE_HYPERLINKS", "FORCE_HYPERLINKS", "GH_DEBUG", "GLAB_DEBUG", "GLAB_DEBUG_HTTP", "DEBUG":
			continue
		case "GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN":
			if !c.envToken {
				continue
			}
		}
		env = append(env, kv)
	}
	env = append(env, "NO_COLOR=1", "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1",
		"GLAB_NO_PROMPT=true", "GLAB_CHECK_UPDATE=false", "GLAB_SHOW_WHATS_NEW=false", "GLAB_NOTIFY_SKILL_UPDATES=false")
	// A later value wins, so these replace the user's own.
	switch {
	case c.host == "":
	case c.name == "gh":
		env = append(env, "GH_HOST="+c.host)
	default:
		env = append(env, "GITLAB_HOST="+c.host)
	}
	return env
}

// cliOutput keeps a CLI's output, failing the command past 1 MiB.
type cliOutput struct{ bytes.Buffer }

func (b *cliOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, errors.New("output exceeds limit")
	}
	return b.Buffer.Write(p)
}
