package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/heyNag/eva-md/internal/wikigit"
)

// When gh, glab or git refuses, setup never prints what they said, which can
// show account details. It sorts what they printed into a reason it knows,
// by the English wordings below, and says that reason and a next step that
// can work in its own words, naming only the host the person gave it.

// The reasons gh or glab couldn't create a repo.
const (
	createRestricted       = "restricted"         // the host's admins don't allow that visibility
	createInternalNeedsOrg = "internal_needs_org" // gh makes internal repos only in an organization
	createNameTaken        = "name_taken"         // a repo of that name is there already
	createLimit            = "limit"              // the account may create no more repos of its own
	createNoPermission     = "no_permission"      // the account may not create repos in that group
	createSignIn           = "sign_in"            // the host refused the CLI's sign-in
	createUnreachable      = "unreachable"        // the host couldn't be reached
)

// createWordings are what gh, glab and the hosts' APIs print when a repo
// can't be created, lower case, checked in this order. glab prints GitLab's
// API error as "error creating project: POST <url>: <status> <message>",
// with the message's JSON flattened as "{field: [text]}" (glab's
// project_create.go; client-go's gitlab.go, ErrorResponse.Error and
// parseError); gh prints GitHub's as "GraphQL: <message> (createRepository)"
// or "HTTP <status>: <message> (<url>)" (go-gh's pkg/api/errors.go).
var createWordings = []struct{ reason, text string }{
	// GitLab's app/services/concerns/base_service_utility.rb,
	// deny_visibility_level: "#{level_name} has been restricted by your
	// GitLab administrator", as in "private has been restricted by your
	// GitLab administrator".
	{createRestricted, "has been restricted by your gitlab administrator"},
	// gh's pkg/cmd/repo/create/http.go: "internal repositories can only be
	// created within an organization".
	{createInternalNeedsOrg, "internal repositories can only be created within an organization"},
	// GitHub's API: "name already exists on this account", as gh prints
	// "GraphQL: Name already exists on this account (createRepository)";
	// GitLab's app/models/project.rb validates the name's uniqueness, which
	// Rails words "has already been taken".
	{createNameTaken, "name already exists on this account"},
	{createNameTaken, "has already been taken"},
	// GitLab's app/models/project.rb, check_personal_projects_limit: "You
	// cannot create projects in your personal namespace. Contact your GitLab
	// administrator." and "You've reached your limit of %{limit} projects
	// created. Contact your GitLab administrator."
	{createLimit, "cannot create projects in your personal namespace"},
	{createLimit, "reached your limit of"},
	// GitLab's app/services/projects/create_service.rb,
	// validate_create_permissions: errors.add(:namespace, "is not valid"),
	// which glab prints as "{namespace: [is not valid]}"; glab's
	// project_create.go: "could not find group or namespace %s"; GitHub's
	// REST API: "You need admin access to the organization before adding a
	// repository to it."; GitHub's GraphQL API: "<login> does not have the
	// correct permissions to execute `<mutation>`".
	{createNoPermission, "namespace: [is not valid]"},
	{createNoPermission, "could not find group or namespace"},
	{createNoPermission, "you need admin access to the organization"},
	{createNoPermission, "does not have the correct permissions to execute"},
	// GitHub's API: "Bad credentials", as gh prints "HTTP 401: Bad
	// credentials (<url>)"; GitLab's lib/api/helpers.rb, unauthorized!:
	// "401 Unauthorized", for a token it has no record of; GitLab's
	// lib/api/api_guard.rb answers an expired or revoked token with
	// Rack::OAuth2's error :invalid_token ("Token is expired. You can either
	// do re-authorization or token refresh." or "Token was revoked. You have
	// to re-authorize from the user."), which glab prints as "401 {error:
	// invalid_token}, {error_description: ...}".
	{createSignIn, "bad credentials"},
	{createSignIn, "401 unauthorized"},
	{createSignIn, "invalid_token"},
	// gh's printError (cmd/gh/main.go) prints a *net.DNSError as only "error
	// connecting to <host>" and "check your internet connection or
	// https://githubstatus.com", never Go's "no such host".
	{createUnreachable, "check your internet connection"},
	// Go's net, syscall and crypto packages, which gh and glab are built
	// with: "no such host" (net.go), "connection refused", "network is
	// unreachable", "no route to host" and "operation timed out" (macOS's
	// syscall errors), "i/o timeout" (net.go), "x509: certificate signed by
	// unknown authority" (x509/verify.go), "tls: failed to verify
	// certificate: %s" (tls/common.go) and an alert such as "tls: handshake
	// failure" (tls/alert.go).
	{createUnreachable, "no such host"},
	{createUnreachable, "connection refused"},
	{createUnreachable, "network is unreachable"},
	{createUnreachable, "no route to host"},
	{createUnreachable, "operation timed out"},
	{createUnreachable, "i/o timeout"},
	{createUnreachable, "x509: "},
	{createUnreachable, "tls: "},
}

// createReason sorts what a failed repo create printed into a reason, or ""
// for any other failure. The text is only read here. Runs of white space
// count as one space: glab prints its errors through fang's
// DefaultErrorHandler (charm.land/fang/v2 v2.0.1's help.go), which never
// takes its plain branch, since fang.Execute hands it a colorprofile.Writer
// that isn't a terminal file, so it renders them with theme.go's ErrorText,
// Width(width()-4), where width() is 120 when stdout isn't a terminal: the
// error comes word-wrapped at 116 columns, each line indented by two spaces
// and padded with spaces, which can split a wording anywhere.
func createReason(printed string) string {
	p := strings.Join(strings.Fields(strings.ToLower(printed)), " ")
	for _, w := range createWordings {
		if strings.Contains(p, w.text) {
			return w.reason
		}
	}
	return ""
}

// createError is a repo create that failed, with its reason. It carries
// nothing the CLI printed.
type createError struct {
	cli, reason string
}

func (e *createError) Error() string { return e.cli + " repo create failed" }

// sortBuffer keeps the first 64 KiB of what a CLI prints, only to sort a
// failure into a reason, and drops the rest without failing the command.
// Its two writers, stdout and stderr, may write at once.
type sortBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const sortBufferLimit = 64 << 10

func (b *sortBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := sortBufferLimit - len(b.buf); room > 0 {
		b.buf = append(b.buf, p[:min(len(p), room)]...)
	}
	return len(p), nil
}

func (b *sortBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// why says a create's reason in setup's words, for the line "<cli> couldn't
// create <name>: <why>.", for a repo of this visibility.
func (c repoCLI) why(reason, visibility string) string {
	switch reason {
	case createRestricted:
		if visibility == "internal" {
			return "the admins of " + c.host + " don't allow internal repos there either"
		}
		return "the admins of " + c.host + " don't allow " + visibility + " repos there"
	case createInternalNeedsOrg:
		return "GitHub makes internal repos only in an organization"
	case createNameTaken:
		return "a repo of that name is there already"
	case createLimit:
		return "the account " + c.name + " is signed in as may not create more repos of its own on " + c.host
	case createNoPermission:
		return "the account " + c.name + " is signed in as may not create repos there"
	case createSignIn:
		return c.host + " refused " + c.name + "'s sign-in, as it does for an expired or revoked token"
	case createUnreachable:
		return c.host + " couldn't be reached"
	}
	return ""
}

// localRepo is how to keep the wiki in a bare repo on this Mac that only the
// user's account can read: git keeps the mode of a folder that is there
// already, and every account on a Mac can read a home folder's 0755 one.
// Its spaces don't break (see say).
const localRepo = "keep the wiki on this Mac in a local repo only your account can read (" +
	"mkdir\u00a0-m\u00a0700\u00a0~/eva-wiki.git\u00a0&&\u00a0git\u00a0init\u00a0--bare\u00a0~/eva-wiki.git, then paste its full path)"

// nextStep says on out what can work after a create failed for reason, and
// reports whether the name question is asked again: no for a host that
// allows no private repo, where no other name helps. last is the name
// question's last try, after which setup asks for an address, so the step
// then never says to type a name.
func (c repoCLI) nextStep(out io.Writer, reason string, last bool) (again bool) {
	switch reason {
	case createRestricted:
		say(out, "Neither "+c.name+" nor you can make a private repo on "+c.host+". You can ask an admin there to create an empty private repo for you and paste its address; "+localRepo+"; or use another host that allows private repos.")
		return false
	case createInternalNeedsOrg, createLimit, createNoPermission:
		var steps []string
		if !last {
			steps = append(steps, "type a group or organization you can create repos in, as group/eva-wiki")
		}
		if who := c.whoCanAllow(reason); who != "" {
			steps = append(steps, "ask "+who)
		}
		if last {
			steps = append(steps, localRepo)
		} else {
			steps = append(steps, "type n and "+localRepo)
		}
		say(out, capitalize(strings.Join(steps, "; or "))+".")
	case createNameTaken:
		if last {
			say(out, "To try another name, run eva-md setup again.")
		} else {
			say(out, "Type another name, or n to paste the existing repo's address.")
		}
	case createSignIn:
		if last {
			say(out, "Run "+c.name+" auth login --hostname "+c.host+" in another terminal, then run eva-md setup again.")
		} else {
			say(out, "Run "+c.name+" auth login --hostname "+c.host+" in another terminal, then type the name again, or n to paste an address.")
		}
	case createUnreachable:
		if last {
			say(out, "Check the network or VPN, then run eva-md setup again.")
		} else {
			say(out, "Check the network or VPN, then type the name again, or n to paste an address.")
		}
	}
	return true
}

// whoCanAllow is who can let the account create a repo after a refusal for
// reason, or "" for no one the user can ask: a group's or organization's
// owner, or a self-hosted host's admin for a limit. github.com's and
// gitlab.com's own staff don't manage anyone's groups or limits.
func (c repoCLI) whoCanAllow(reason string) string {
	switch {
	case reason == createNoPermission:
		return "an owner of that group or organization to let you"
	case reason == createLimit && c.host != githubHost && c.host != gitlabHost:
		return "an admin of " + c.host + " to raise the limit"
	}
	return ""
}

// addressHint is what setup asks for when offer gives no repo, after the
// step it said for the last refusal: never a private repo to create on a
// host that allows none, and the local repo again when that step named it.
// created means offer made a repo, or may have.
func (r *repoCLIs) addressHint(created bool) string {
	switch {
	case r.restricted, r.failed == createLimit, r.failed == createNoPermission, r.failed == createInternalNeedsOrg:
		return "   Paste the repo's SSH or HTTPS address, or a local repo's full path.\n"
	case created:
		return "   Paste the SSH or HTTPS address of an empty private repository, such as\n   git@github.com:you/eva-wiki.git.\n"
	case r.failed == createNameTaken:
		return "   Paste the existing repo's SSH or HTTPS address.\n"
	}
	return "   Create an empty private repository first, on GitHub, GitLab or any git host,\n   then paste its SSH or HTTPS address, such as git@github.com:you/eva-wiki.git.\n"
}

// say prints text on out in lines of at most 92 columns, each indented by
// three spaces. A no-break space joins words, such as a command's, that
// must stay on one line; it prints as a space.
func say(out io.Writer, text string) {
	const width = 92
	line := "  "
	// strings.Fields would split at a no-break space too.
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' }) {
		word = strings.ReplaceAll(word, "\u00a0", " ")
		if len(line) > 2 && len(line)+1+len(word) > width {
			fmt.Fprintln(out, line)
			line = "  "
		}
		line += " " + word
	}
	fmt.Fprintln(out, line)
}

// readFailure is why git couldn't read a repo, in setup's words: what
// happened and what to do. It holds nothing git printed.
type readFailure struct {
	reason    string // as wikigit sorted it
	what, fix string
}

func (e *readFailure) Error() string { return e.what + "; " + e.fix }

// readError is why git couldn't read remote, in setup's words, for the
// reason wikigit gave; a reason that doesn't fit the address, or none, gives
// the message setup always gave. It names only the host or path the person
// gave.
func readError(remote, reason string) error {
	fail := func(what, fix string) error { return &readFailure{reason: reason, what: what, fix: fix} }
	generic := fail("git couldn't read this repo", "check its address and that git can sign in to it")
	if filepath.IsAbs(remote) {
		if reason == wikigit.ReadNotFound {
			return fail("git found no repo at "+remote, "make one there with git init --bare, in a folder only your account can read, or give another address")
		}
		return generic
	}
	host, user, port, ssh := remoteHost(remote)
	if host == "" {
		return generic
	}
	// ssh -T signs in as git does: as the address's user, or as ssh's
	// config or the login on this Mac when it names none, on its port.
	target := host
	if user != "" {
		target = user + "@" + host
	}
	if port != "" {
		target = "-p " + port + " " + target
	}
	switch {
	case reason == wikigit.ReadNotFound:
		return fail("git found no repo at this address on "+host+", or none the account it signs in with can see", "create it first as an empty private repo, or check the address")
	case reason == wikigit.ReadUnreachable:
		return fail("git couldn't reach "+host, "check the network or VPN, then try again")
	case reason == wikigit.ReadKeyRefused && ssh:
		return fail("git's SSH key was refused by "+host, "add the key to your account there (ssh -T "+target+" shows which account the key signs in as), or use the repo's HTTPS address")
	case reason == wikigit.ReadAgentRefused && ssh:
		return fail("the SSH agent didn't sign git's sign-in to "+host, "approve the agent's request when it asks, then try again")
	case reason == wikigit.ReadHostKey && ssh:
		return fail("ssh doesn't know the host key of "+host, "run ssh -T "+target+" once and, if the fingerprint it shows is the host's, answer yes, then try again")
	case reason == wikigit.ReadHostKeyChanged && ssh:
		// ssh names the known_hosts line it holds for the host, under the
		// name and port its config gives, which the address may not.
		return fail("the host key of "+host+" isn't the one ssh knows, which can mean someone is intercepting the connection", "ask the host's admins whether its key changed, and only then delete the line of known_hosts that ssh -T "+target+" names as the offending key, and try again")
	case reason == wikigit.ReadHostKeyRevoked && ssh:
		// Removing the host's lines from known_hosts would keep the
		// revocation and lose the key ssh trusts.
		return fail("the host key of "+host+" is one ssh has marked revoked, which can mean a stolen key is being used to impersonate the host", "ask the host's admins about its key before you try again")
	case reason == wikigit.ReadNoSignIn && !ssh:
		return fail("git couldn't sign in to "+host+" over HTTPS", "set up git's sign-in for that host, or use the repo's SSH address")
	}
	return generic
}

// remoteHost is the host an SSH or HTTPS remote names, as the person typed
// it, with the user and port an SSH address gives ("" for none), and
// whether git reaches it over SSH. host is "" for an address it can't read.
func remoteHost(remote string) (host, user, port string, ssh bool) {
	if scheme, _, ok := strings.Cut(remote, "://"); ok {
		u, err := url.Parse(remote)
		if err != nil {
			return "", "", "", false
		}
		switch strings.ToLower(scheme) {
		case "ssh", "git+ssh", "ssh+git":
			if u.User != nil {
				user = u.User.Username()
			}
			return u.Hostname(), user, u.Port(), true
		case "https", "http":
			return u.Host, "", "", false
		}
		return "", "", "", false
	}
	// git's short SSH form, [user@]host:path.
	before, _, ok := strings.Cut(remote, ":")
	if !ok || strings.Contains(before, "/") {
		return "", "", "", false
	}
	if user, host, ok = strings.Cut(before, "@"); !ok {
		user, host = "", before
	}
	return host, user, "", host != ""
}

// internalReaders names who can read an internal repo on host, as cli says
// of it: a GitHub host's enterprise, or everyone signed in to a GitLab one.
func internalReaders(cli, host string) string {
	if cli == "gh" {
		return "every member of its enterprise on " + host
	}
	return "everyone signed in to " + host
}

// internalRepoError is a repo the CLI for its host says is internal, which
// setup uses only when the person agrees (see allowInternal).
type internalRepoError struct{ cli, host string }

func (e *internalRepoError) Error() string { return "this repo " + internalProblem }

// internalProblem says why setup refuses an internal repo the person hasn't
// agreed to.
const internalProblem = "is internal, so other members of its enterprise or instance can read it; the wiki holds your chats' summaries, so use a private repo"

// allowInternal asks whether setup may create or use an internal repo on
// host, as cli says it is, with question, no being the default.
// EVA_MD_ALLOW_INTERNAL_REPO answers it in advance: true is a yes, said in a
// "!" line that starts with notice, for each repo, and false a no. Without a
// terminal and without the key, the answer is no. A yes the user typed holds
// for that host for the rest of the run. err is errNoAnswer when the answers
// ran out.
func (r *repoCLIs) allowInternal(out io.Writer, cli, host, notice, question string) (bool, error) {
	switch {
	case r.internalAnswer != nil && *r.internalAnswer:
		fmt.Fprintf(out, "   ! %s, as EVA_MD_ALLOW_INTERNAL_REPO allows: %s can read it.\n", notice, internalReaders(cli, host))
		return true, nil
	case r.internalOK[host]:
		return true, nil
	case r.internalAnswer != nil || r.ask == nil:
		return false, nil
	}
	yes, ok := yesNo(r.ask, out, question, false)
	if !ok {
		return false, errNoAnswer
	}
	if yes {
		r.internalOK[host] = true
	}
	return yes, nil
}

// confirmInternal asks about an internal repo checkPrivate found, and passes
// any other error on. A no refuses it, as setup always has; without a
// terminal and without EVA_MD_ALLOW_INTERNAL_REPO, the refusal says how to
// allow it.
func (r *repoCLIs) confirmInternal(out io.Writer, err error) error {
	var e *internalRepoError
	if !errors.As(err, &e) {
		return err
	}
	question := fmt.Sprintf("   This repo is internal. %s can read an internal repo, and the wiki holds summaries of your chats. Use it anyway? [y/N] ", capitalize(internalReaders(e.cli, e.host)))
	switch use, askErr := r.allowInternal(out, e.cli, e.host, "This repo is internal", question); {
	case askErr != nil:
		return askErr
	case use:
		return nil
	case r.internalAnswer == nil && r.ask == nil:
		return errors.New("this repo " + internalProblem + ", or set EVA_MD_ALLOW_INTERNAL_REPO=true in config.env to allow one")
	}
	return e
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
