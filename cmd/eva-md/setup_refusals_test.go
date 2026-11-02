package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// fangWrapped is text as glab v1.127.0 prints an error through fang v2.0.1:
// "ERROR" on a line of its own, then the text word-wrapped at 116 columns,
// each line indented by two spaces and padded with spaces to that width.
// See createReason.
func fangWrapped(lines ...string) string {
	var b strings.Builder
	b.WriteString("\n  ERROR \n\n")
	for _, line := range lines {
		b.WriteString("  " + line + strings.Repeat(" ", max(0, 116-len(line))) + "\n")
	}
	return b.String()
}

// Each refusal gh, glab or a host's API prints when a repo can't be created
// gives its reason, whatever its letter case and wherever glab's word wrap
// breaks it. Every wording is checked by a line that holds it alone, as the
// CLI prints it: glab as "error creating project: POST <url>: <status>
// <message>", gh as "GraphQL: <message> (createRepository)" or "HTTP
// <status>: <message> (<url>)". Names in the lines are made up.
func TestCreateReason(t *testing.T) {
	const post = "error creating project: POST https://gitlab.example.com/api/v4/projects: "
	for _, tc := range []struct{ printed, reason string }{
		// glab's word wrap breaks a wording where a host's name puts it: here
		// as for git.example.org and code.gitlab.example.net.
		{fangWrapped("error creating project: POST https://git.example.org/api/v4/projects: 400 {message: {visibility_level: [private has",
			"been restricted by your GitLab administrator]}}."), createRestricted},
		{fangWrapped("error creating project: POST https://code.gitlab.example.net/api/v4/projects: 400 {message: {namespace: [is not",
			"valid]}}."), createNoPermission},
		{fangWrapped("error creating project: POST https://gitlab.example.com/api/v4/projects: 400 {message: {limit_reached: [You cannot",
			"create projects in your personal namespace. Contact your GitLab administrator.]}}."), createLimit},
		{post + "400 {message: {visibility_level: [private has been restricted by your GitLab administrator]}}", createRestricted},
		{post + "400 {message: {visibility_level: [internal has been restricted by your GitLab administrator]}}", createRestricted},
		{"internal repositories can only be created within an organization", createInternalNeedsOrg},
		{"GraphQL: Name already exists on this account (createRepository)", createNameTaken},
		{post + "400 {message: {name: [has already been taken]}, {path: [has already been taken]}}", createNameTaken},
		{post + "400 {message: {limit_reached: [You cannot create projects in your personal namespace. Contact your GitLab administrator.]}}", createLimit},
		{post + "400 {message: {limit_reached: [You've reached your limit of 10 projects created. Contact your GitLab administrator.]}}", createLimit},
		{post + "400 {message: {namespace: [is not valid]}}", createNoPermission},
		{"could not find group or namespace your-group: 404 Not Found", createNoPermission},
		{"HTTP 403: You need admin access to the organization before adding a repository to it. (https://api.github.com/orgs/your-org/repos)", createNoPermission},
		{"GraphQL: fake-account-7f3a does not have the correct permissions to execute `CreateRepository` (createRepository)", createNoPermission},
		{"HTTP 401: Bad credentials (https://api.github.com/graphql)", createSignIn},
		{post + "401 {message: 401 Unauthorized}", createSignIn},
		{"GET https://gitlab.example.com/api/v4/user: 401 {error: invalid_token}, {error_description: Token is expired. You can either do re-authorization or token refresh.}", createSignIn},
		{"GET https://gitlab.example.com/api/v4/user: 401 {error: invalid_token}, {error_description: Token was revoked. You have to re-authorize from the user.}", createSignIn},
		// gh 2.103.3's two lines for a host whose name doesn't resolve.
		{"error connecting to api.github.com\ncheck your internet connection or https://githubstatus.com", createUnreachable},
		{`error creating project: Post "https://gitlab.example.com/api/v4/projects": dial tcp: lookup gitlab.example.com: no such host`, createUnreachable},
		{`Post "https://api.github.com/graphql": dial tcp 192.0.2.1:443: connect: connection refused`, createUnreachable},
		{`Post "https://api.github.com/graphql": dial tcp 192.0.2.1:443: connect: network is unreachable`, createUnreachable},
		{`Post "https://api.github.com/graphql": dial tcp 192.0.2.1:443: connect: no route to host`, createUnreachable},
		{`Post "https://api.github.com/graphql": dial tcp 192.0.2.1:443: connect: operation timed out`, createUnreachable},
		{`Post "https://api.github.com/graphql": dial tcp 192.0.2.1:443: i/o timeout`, createUnreachable},
		{`Post "https://gitlab.example.com/api/v4/projects": x509: certificate signed by unknown authority`, createUnreachable},
		{`Post "https://gitlab.example.com/api/v4/projects": remote error: tls: handshake failure`, createUnreachable},
		{"error creating project: something else", ""},
		{"", ""},
	} {
		if got := createReason(tc.printed); got != tc.reason {
			t.Errorf("%q gives %q, want %q", tc.printed, got, tc.reason)
		}
	}
}

// localRepoStep is the end of every next step that names a local repo.
const localRepoStep = "local repo only your account can read\n   (mkdir -m 700 ~/eva-wiki.git && git init --bare ~/eva-wiki.git, then paste its full\n   path)"

// When gh or glab can't create the repo for a reason setup knows, it says
// the reason and a next step in its own words, never what the CLI printed,
// and asks for another name. n then goes on to the address question, which
// asks for what that step named: the local repo after a limit or no
// permission, the existing repo after a taken name. One CLI prints its
// refusal on stdout, which is read too, and one wraps it as glab's fang
// does.
func TestSetupSaysWhyACLICouldntCreateTheRepo(t *testing.T) {
	const (
		post       = "error creating project: POST https://gitlab.example.com/api/v4/projects: "
		nameNext   = "   Type another name, or n to paste the existing repo's address.\n"
		netNext    = "   Check the network or VPN, then type the name again, or n to paste an address.\n"
		orLocal    = "   Paste the repo's SSH or HTTPS address, or a local repo's full path.\n"
		existing   = "   Paste the existing repo's SSH or HTTPS address.\n"
		createHint = "   Create an empty private repository first, on GitHub, GitLab or any git host,\n"
	)
	for _, tc := range []struct {
		name, cli, says, why, next, hint string
		stdout                           bool
	}{
		{"gh name taken", "gh", "GraphQL: Name already exists on this account (createRepository)", "a repo of that name is there already", nameNext, existing, false},
		{"glab name taken", "glab", post + "400 {message: {name: [has already been taken]}, {path: [has already been taken]}}", "a repo of that name is there already", nameNext, existing, true},
		{"gh no permission", "gh", "GraphQL: fake-account-7f3a does not have the correct permissions to execute `CreateRepository` (createRepository)",
			"the account gh is signed in as may not create repos there",
			"   Type a group or organization you can create repos in, as group/eva-wiki; or ask an owner\n   of that group or organization to let you; or type n and keep the wiki on this Mac in a\n   " + localRepoStep + ".\n", orLocal, false},
		{"glab limit", "glab", post + "400 {message: {limit_reached: [You've reached your limit of 0 projects created. Contact your GitLab administrator.]}}",
			"the account glab is signed in as may not create more repos of its own on gitlab.example.com",
			"   Type a group or organization you can create repos in, as group/eva-wiki; or ask an admin\n   of gitlab.example.com to raise the limit; or type n and keep the wiki on this Mac in a\n   " + localRepoStep + ".\n", orLocal, false},
		{"glab no permission", "glab", post + "400 {message: {namespace: [is not valid]}}",
			"the account glab is signed in as may not create repos there",
			"   Type a group or organization you can create repos in, as group/eva-wiki; or ask an owner\n   of that group or organization to let you; or type n and keep the wiki on this Mac in a\n   " + localRepoStep + ".\n", orLocal, false},
		{"glab no permission, wrapped", "glab", fangWrapped("error creating project: POST https://code.gitlab.example.net/api/v4/projects: 400 {message: {namespace: [is not", "valid]}} fake-account-7f3a"),
			"the account glab is signed in as may not create repos there",
			"   Type a group or organization you can create repos in, as group/eva-wiki; or ask an owner\n   of that group or organization to let you; or type n and keep the wiki on this Mac in a\n   " + localRepoStep + ".\n", orLocal, false},
		{"gh sign-in", "gh", "HTTP 401: Bad credentials (https://api.github.com/graphql)", "github.com refused gh's sign-in, as it does for an expired or revoked token",
			"   Run gh auth login --hostname github.com in another terminal, then type the name again, or\n   n to paste an address.\n", createHint, false},
		{"glab sign-in", "glab", post + "401 {message: 401 Unauthorized}", "gitlab.example.com refused glab's sign-in, as it does for an expired or revoked token",
			"   Run glab auth login --hostname gitlab.example.com in another terminal, then type the name\n   again, or n to paste an address.\n", createHint, false},
		{"glab expired token", "glab", "GET https://gitlab.example.com/api/v4/user: 401 {error: invalid_token}, {error_description: Token is expired. You can either do re-authorization or token refresh.}",
			"gitlab.example.com refused glab's sign-in, as it does for an expired or revoked token",
			"   Run glab auth login --hostname gitlab.example.com in another terminal, then type the name\n   again, or n to paste an address.\n", createHint, false},
		{"gh unreachable", "gh", "error connecting to api.github.com\ncheck your internet connection or https://githubstatus.com", "github.com couldn't be reached", netNext, createHint, false},
		{"glab unreachable", "glab", `error creating project: Post "https://gitlab.example.com/api/v4/projects": tls: failed to verify certificate: x509: certificate signed by unknown authority`,
			"gitlab.example.com couldn't be reached", netNext, createHint, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := "git@github.com:me/eva-wiki.git"
			cli := fakeCLI{name: tc.cli, create: 1, createSays: tc.says, createOnStdout: tc.stdout}
			if tc.cli == "glab" {
				remote, cli.host = "git@gitlab.example.com:me/eva-wiki.git", "gitlab.example.com"
			}
			f := newFakeSetup(t, true).fakes(t, cli)
			code, out, errOut := f.answers(t, "", "n", remote, "y")
			f.wantRemote(t, code, out, errOut, remote)
			wantIn(t, "stdout", out, "   "+tc.cli+" couldn't create eva-wiki: "+tc.why+".\n"+tc.next+"   Another name, or an address: "+tc.hint)
			wantNotIn(t, "stdout", out, tc.says, "--internal", "Create eva-wiki as an internal repo")
			if tc.hint != createHint {
				wantNotIn(t, "stdout", out, createHint)
			}
			wantNotIn(t, tc.cli+" log", f.log(t, tc.cli), "--internal")
		})
	}
	// A refusal setup doesn't know is said as it always was.
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "glab", host: "gitlab.example.com", create: 1, createSays: "error creating project: something unexpected for fake-account-7f3a"})
	code, out, errOut := f.answers(t, "", "git@gitlab.example.com:me/eva-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "   glab couldn't create eva-wiki.\n"+createHint)
}

// On the name question's last try, the next step never says to type a name,
// which the address question that follows would refuse.
func TestSetupNeverAsksForANameAfterTheLastTry(t *testing.T) {
	const post = "error creating project: POST https://gitlab.example.com/api/v4/projects: "
	for _, tc := range []struct{ name, says, last, hint string }{
		{"sign-in", post + "401 {message: 401 Unauthorized}",
			"   Run glab auth login --hostname gitlab.example.com in another terminal, then run eva-md\n   setup again.\n",
			"   Create an empty private repository first, on GitHub, GitLab or any git host,\n"},
		{"unreachable", `Post "https://gitlab.example.com/api/v4/projects": dial tcp 192.0.2.1:443: connect: connection refused`,
			"   Check the network or VPN, then run eva-md setup again.\n",
			"   Create an empty private repository first, on GitHub, GitLab or any git host,\n"},
		{"limit", post + "400 {message: {limit_reached: [You cannot create projects in your personal namespace. Contact your GitLab administrator.]}}",
			"   Ask an admin of gitlab.example.com to raise the limit; or keep the wiki on this Mac in a\n   " + localRepoStep + ".\n",
			"   Paste the repo's SSH or HTTPS address, or a local repo's full path.\n"},
		{"name taken", post + "400 {message: {name: [has already been taken]}}",
			"   To try another name, run eva-md setup again.\n",
			"   Paste the existing repo's SSH or HTTPS address.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := "git@gitlab.example.com:me/eva-wiki.git"
			f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "glab", host: "gitlab.example.com", create: 1, createSays: tc.says})
			code, out, errOut := f.answers(t, "", "eva-wiki", "eva-wiki", remote, "y")
			f.wantRemote(t, code, out, errOut, remote)
			if n := strings.Count(out, "   glab couldn't create eva-wiki: "); n != 3 {
				t.Fatalf("%d refusals, want 3:\n%s", n, out)
			}
			_, third, _ := strings.Cut(out, "   Another name, or an address: ")
			_, third, _ = strings.Cut(third, "   Another name, or an address: ")
			_, third, _ = strings.Cut(third, ".\n")
			if !strings.HasPrefix(third, tc.last+tc.hint) {
				t.Fatalf("after the third refusal:\n%s\nwant it to start:\n%s", third, tc.last+tc.hint)
			}
		})
	}
}

// restrictedGlab is glab on a GitLab whose admins allow no private repo:
// repo create --private prints GitLab's refusal, and --internal works.
var restrictedGlab = fakeCLI{name: "glab", host: "gitlab.example.com", create: 1,
	createSays: "error creating project: POST https://gitlab.example.com/api/v4/projects: 400 {message: {visibility_level: [private has been restricted by your GitLab administrator]}}",
	after:      glabJSON("eva-wiki", "internal", true)}

const (
	restrictedLine     = "   glab couldn't create eva-wiki: the admins of gitlab.example.com don't allow private repos there.\n"
	internalQuestion   = "   Create eva-wiki as an internal repo instead? Everyone signed in to gitlab.example.com can read an internal repo, and the wiki holds summaries of your chats. [y/N] "
	restrictedNextStep = "   Neither glab nor you can make a private repo on gitlab.example.com. You can ask an admin\n   there to create an empty private repo for you and paste its address; keep the wiki on\n   this Mac in a " + localRepoStep + "; or use another host that allows private repos.\n   Paste the repo's SSH or HTTPS address, or a local repo's full path.\n   Remote: "
	internalCreated    = "   Created git@gitlab.example.com:me/eva-wiki.git, an internal repo on gitlab.example.com.\n"
)

// When the host's admins allow no private repo, setup says so and asks
// whether to create an internal one instead, no being the default. A yes
// creates it with --internal; a no, Enter, or no answer creates nothing
// internal and never sends the user off to create a private repo, which
// that host doesn't allow: a local repo's path is one way on.
func TestSetupOffersAnInternalRepoWhenPrivateIsRestricted(t *testing.T) {
	t.Run("yes", func(t *testing.T) {
		f := newFakeSetup(t, true).fakes(t, restrictedGlab)
		code, out, errOut := f.answers(t, "", "y", "y")
		f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
		wantIn(t, "stdout", out, restrictedLine+internalQuestion+internalCreated)
		wantNotIn(t, "stdout", out, "has been restricted by your GitLab administrator", "Create an empty private repository first")
		wantIn(t, "glab log", f.log(t, "glab"), "repo create eva-wiki --private --skipGitInit ", "repo create eva-wiki --internal --skipGitInit ")
	})
	for _, answer := range []string{"n", ""} {
		t.Run("answered "+answer, func(t *testing.T) {
			f := newFakeSetup(t, true).fakes(t, restrictedGlab)
			// A bare repo on this Mac holds the wiki instead.
			local := filepath.Join(t.TempDir(), "eva-wiki.git")
			if err := os.Mkdir(local, 0o700); err != nil {
				t.Fatal(err)
			}
			code, out, errOut := f.answers(t, "", answer, local, "y")
			f.wantRemote(t, code, out, errOut, local)
			wantIn(t, "stdout", out, restrictedLine+internalQuestion+restrictedNextStep)
			wantNotIn(t, "stdout", out, "Create an empty private repository first", "Created ")
			wantNotIn(t, "glab log", f.log(t, "glab"), "--internal")
		})
	}
	t.Run("no answer", func(t *testing.T) {
		f := newFakeSetup(t, true).fakes(t, restrictedGlab)
		code, out, _ := f.answers(t, "")
		if code != 1 || len(f.inits) != 0 {
			t.Fatalf("code %d inits %d\n%s", code, len(f.inits), out)
		}
		wantIn(t, "stdout", out, restrictedLine+internalQuestion)
		wantNotIn(t, "glab log", f.log(t, "glab"), "--internal")
	})
	t.Run("wrapped as glab wraps it", func(t *testing.T) {
		glab := restrictedGlab
		glab.createSays = fangWrapped("error creating project: POST https://git.example.org/api/v4/projects: 400 {message: {visibility_level: [private has",
			"been restricted by your GitLab administrator]}}.")
		f := newFakeSetup(t, true).fakes(t, glab)
		code, out, errOut := f.answers(t, "", "y", "y")
		f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
		wantIn(t, "stdout", out, restrictedLine+internalQuestion+internalCreated)
	})
	// After a yes, setup never sends the user off to create a private repo,
	// whether the internal create failed for a reason another name may fix
	// and the user typed n, or made the repo but lost its address.
	t.Run("yes, then the name is taken", func(t *testing.T) {
		glab := restrictedGlab
		glab.internal, glab.internalSays = 1, "error creating project: POST https://gitlab.example.com/api/v4/projects: 400 {message: {name: [has already been taken]}}"
		f := newFakeSetup(t, true).fakes(t, glab)
		local := t.TempDir()
		code, out, errOut := f.answers(t, "", "y", "n", local, "y")
		f.wantRemote(t, code, out, errOut, local)
		wantIn(t, "stdout", out, internalQuestion+"   glab couldn't create eva-wiki as an internal repo: a repo of that name is there already.\n"+
			"   Type another name, or n to paste the existing repo's address.\n   Another name, or an address: "+
			"   Paste the repo's SSH or HTTPS address, or a local repo's full path.\n   Remote: ")
		wantNotIn(t, "stdout", out, "Create an empty private repository first", "of an empty private repository")
	})
	t.Run("yes, then the address is lost", func(t *testing.T) {
		glab := restrictedGlab
		glab.after = "not json"
		f := newFakeSetup(t, true).fakes(t, glab)
		local := t.TempDir()
		code, out, errOut := f.answers(t, "", "y", local, "y")
		f.wantRemote(t, code, out, errOut, local)
		wantIn(t, "stdout", out, internalQuestion+"   glab created eva-wiki but couldn't read back its address.\n"+
			"   Paste the repo's SSH or HTTPS address, or a local repo's full path.\n   Remote: ")
		wantNotIn(t, "stdout", out, "Create an empty private repository first", "of an empty private repository")
	})
	t.Run("internal restricted too", func(t *testing.T) {
		glab := restrictedGlab
		glab.internal, glab.internalSays = 1, strings.Replace(glab.createSays, "[private", "[internal", 1)
		f := newFakeSetup(t, true).fakes(t, glab)
		local := t.TempDir()
		code, out, errOut := f.answers(t, "", "y", local, "y")
		f.wantRemote(t, code, out, errOut, local)
		wantIn(t, "stdout", out, restrictedLine+internalQuestion+"   glab couldn't create eva-wiki as an internal repo: the admins of gitlab.example.com don't allow internal repos there either.\n"+restrictedNextStep)
		wantNotIn(t, "stdout", out, "has been restricted", "Create an empty private repository first")
	})
}

// EVA_MD_ALLOW_INTERNAL_REPO answers the question in advance: true creates
// the internal repo without asking, with a "!" line naming who can read it;
// false creates none, also without asking.
func TestSetupTakesTheInternalAnswerFromTheFile(t *testing.T) {
	const creating = "   ! Creating eva-wiki as an internal repo, as EVA_MD_ALLOW_INTERNAL_REPO allows: everyone signed in to gitlab.example.com can read it.\n"
	f := newFakeSetup(t, true).fakes(t, restrictedGlab)
	f.answersFile(t, "EVA_MD_ALLOW_INTERNAL_REPO=true\n")
	code, out, errOut := f.answers(t, "", "y")
	f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, restrictedLine+creating+internalCreated)
	wantNotIn(t, "stdout", out, "as an internal repo instead?")

	f = newFakeSetup(t, true).fakes(t, restrictedGlab)
	f.answersFile(t, "EVA_MD_ALLOW_INTERNAL_REPO=false\n")
	local := t.TempDir()
	code, out, errOut = f.answers(t, "", local, "y")
	f.wantRemote(t, code, out, errOut, local)
	wantIn(t, "stdout", out, restrictedLine+restrictedNextStep)
	wantNotIn(t, "stdout", out, "as an internal repo instead?", "EVA_MD_ALLOW_INTERNAL_REPO allows")
	wantNotIn(t, "glab log", f.log(t, "glab"), "--internal")

	// true says the "!" line for each internal repo setup meets: the one it
	// creates, and another given after that create failed, which the line
	// for the first doesn't cover.
	glab := restrictedGlab
	glab.internal, glab.internalSays = 1, "error creating project: POST https://gitlab.example.com/api/v4/projects: 400 {message: {name: [has already been taken]}}"
	glab.exists, glab.before = "me/other", glabJSON("other", "internal", true)
	f = newFakeSetup(t, true).fakes(t, glab)
	f.answersFile(t, "EVA_MD_ALLOW_INTERNAL_REPO=true\n")
	other := "git@gitlab.example.com:me/other.git"
	code, out, errOut = f.answers(t, "", "n", other, "y")
	f.wantRemote(t, code, out, errOut, other)
	wantIn(t, "stdout", out, restrictedLine+creating+"   glab couldn't create eva-wiki as an internal repo: a repo of that name is there already.\n",
		"   Remote:    ! This repo is internal, as EVA_MD_ALLOW_INTERNAL_REPO allows: everyone signed in to gitlab.example.com can read it.\n")
}

// internalGlab is glab signed in to gitlab.example.com, where me/eva-wiki
// is an empty internal repo.
var internalGlab = fakeCLI{name: "glab", signedIn: "gitlab.example.com", host: "gitlab.example.com", exists: "me/eva-wiki", before: glabJSON("eva-wiki", "internal", true)}

const (
	internalRemote  = "git@gitlab.example.com:me/eva-wiki.git"
	givenInternal   = "   This repo is internal. Everyone signed in to gitlab.example.com can read an internal repo, and the wiki holds summaries of your chats. Use it anyway? [y/N] "
	internalRefusal = "This repo is internal, so other members of its enterprise or instance can read it; the wiki holds your chats' summaries, so use a private repo"
)

// An internal repo the user gives, at the question, with --remote or in
// the answers file, is asked about, no being the default, where setup
// always refused it; a no refuses it as before. Without a terminal it is
// refused, saying how to allow it, unless the answers file allows it.
func TestSetupAsksAboutAGivenInternalRepo(t *testing.T) {
	t.Run("at the question", func(t *testing.T) {
		f := newFakeSetup(t, true).fakes(t, internalGlab)
		code, out, errOut := f.answers(t, "n", internalRemote, "", internalRemote, "y", "y")
		f.wantRemote(t, code, out, errOut, internalRemote)
		wantIn(t, "stdout", out, "   Remote: "+givenInternal+"   "+internalRefusal+"\n   Remote: "+givenInternal+checking)
	})
	t.Run("the repo of the default name", func(t *testing.T) {
		glab := internalGlab
		glab.exists = "eva-wiki"
		f := newFakeSetup(t, true).fakes(t, glab)
		code, out, errOut := f.answers(t, "", "y", "y")
		f.wantRemote(t, code, out, errOut, internalRemote)
		wantIn(t, "stdout", out, "   eva-wiki already exists on gitlab.example.com and is internal. Everyone signed in to gitlab.example.com can read an internal repo, and the wiki holds summaries of your chats. Use it anyway? [y/N] "+
			"   eva-wiki already exists on gitlab.example.com, internal and empty: using "+internalRemote+".\n")
		wantNotIn(t, "glab log", f.log(t, "glab"), "repo create")
	})
	t.Run("by flag", func(t *testing.T) {
		dir := "--dir"
		for _, tc := range []struct{ input, errOut string }{{"y\ny\n", ""}, {"\n", "eva-md: " + internalRefusal + "\n"}, {"n\n", "eva-md: " + internalRefusal + "\n"}} {
			f := newFakeSetup(t, true).fakes(t, internalGlab)
			code, out, errOut := f.run(tc.input, dir, filepath.Join(f.env.home, "wiki"), "--remote", internalRemote)
			noCLIText(t, out+errOut)
			if tc.errOut == "" {
				f.wantRemote(t, code, out, errOut, internalRemote)
			} else if code != 1 || len(f.inits) != 0 || errOut != tc.errOut {
				t.Fatalf("answered %q: code %d inits %d\n%s\n%s", tc.input, code, len(f.inits), out, errOut)
			}
			wantIn(t, "stdout", out, givenInternal)
		}
		// Without a terminal, and with nothing in the answers file, it is
		// refused, saying how to allow it.
		f := newFakeSetup(t, false).fakes(t, internalGlab)
		code, out, errOut := f.run("", dir, filepath.Join(f.env.home, "wiki"), "--remote", internalRemote)
		if code != 1 || len(f.inits) != 0 || errOut != "eva-md: "+internalRefusal+", or set EVA_MD_ALLOW_INTERNAL_REPO=true in config.env to allow one\n" {
			t.Fatalf("no terminal: code %d\n%s\n%s", code, out, errOut)
		}
	})
	t.Run("in the file", func(t *testing.T) {
		// true uses it without a terminal, with the "!" line.
		f := newFakeSetup(t, false).fakes(t, internalGlab)
		f.answersFile(t, "EVA_MD_WIKI_DIR=~/wiki\nEVA_MD_WIKI_REMOTE="+internalRemote+"\nEVA_MD_ALLOW_INTERNAL_REPO=true\n")
		code, out, errOut := f.run("")
		f.wantRemote(t, code, out, errOut, internalRemote)
		wantIn(t, "stdout", out, "   ! This repo is internal, as EVA_MD_ALLOW_INTERNAL_REPO allows: everyone signed in to gitlab.example.com can read it.\n")
		// false refuses it in a terminal without asking.
		f = newFakeSetup(t, true).fakes(t, internalGlab)
		f.answersFile(t, "EVA_MD_WIKI_DIR=~/wiki\nEVA_MD_WIKI_REMOTE="+internalRemote+"\nEVA_MD_ALLOW_INTERNAL_REPO=false\n")
		code, out, errOut = f.run("y\ny\n")
		if code != 1 || len(f.inits) != 0 || errOut != "eva-md: "+internalRefusal+"\n" {
			t.Fatalf("false: code %d\n%s\n%s", code, out, errOut)
		}
		wantNotIn(t, "stdout", out, "Use it anyway?")
		// Empty asks in a terminal.
		f = newFakeSetup(t, true).fakes(t, internalGlab)
		f.answersFile(t, "EVA_MD_WIKI_DIR=~/wiki\nEVA_MD_WIKI_REMOTE="+internalRemote+"\nEVA_MD_ALLOW_INTERNAL_REPO=\n")
		code, out, errOut = f.run("y\ny\n")
		f.wantRemote(t, code, out, errOut, internalRemote)
		wantIn(t, "stdout", out, givenInternal)
	})
	t.Run("on GitHub", func(t *testing.T) {
		// An internal repo on a GitHub host is read by its enterprise.
		f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", signedIn: "github.com", exists: "me/eva-wiki", before: ghJSON("eva-wiki", "INTERNAL", true)})
		code, out, errOut := f.run("y\ny\n", "--dir", filepath.Join(f.env.home, "wiki"), "--remote", "git@github.com:me/eva-wiki.git")
		noCLIText(t, out+errOut)
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
		wantIn(t, "stdout", out, "   This repo is internal. Every member of its enterprise on github.com can read an internal repo, and the wiki holds summaries of your chats. Use it anyway? [y/N] ")
	})
}

// A public repo is refused whatever the answers file says about internal
// ones, given or found by name.
func TestSetupAlwaysRefusesAPublicRepo(t *testing.T) {
	public := fakeCLI{name: "glab", signedIn: "gitlab.example.com", host: "gitlab.example.com", exists: "*eva-wiki", before: glabJSON("eva-wiki", "public", true)}
	f := newFakeSetup(t, true).fakes(t, public)
	f.answersFile(t, "EVA_MD_ALLOW_INTERNAL_REPO=true\n")
	code, out, errOut := f.run("y\ny\n", "--dir", filepath.Join(f.env.home, "wiki"), "--remote", internalRemote)
	if code != 1 || len(f.inits) != 0 || errOut != "eva-md: This repo is public; the wiki holds your chats' summaries, so use a private repo\n" {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	f = newFakeSetup(t, true).fakes(t, public)
	f.answersFile(t, "EVA_MD_ALLOW_INTERNAL_REPO=true\n")
	code, out, _ = f.answers(t, "")
	if code != 1 || len(f.inits) != 0 {
		t.Fatalf("code %d\n%s", code, out)
	}
	wantIn(t, "stdout", out, "   eva-wiki already exists on gitlab.example.com and is public; the wiki holds your chats' summaries, so use a private repo.\n")
	wantNotIn(t, "stdout", out, "is internal")
}

// A Mac already set up, as an upgrade runs setup, never reads its repo's
// visibility, so it is never asked about an internal repo: neither when
// its wiki is healthy nor when it clones the wiki again, where glab gives
// the address.
func TestSetupNeverAsksAboutInternalOnAMacSetUp(t *testing.T) {
	glab := internalGlab
	glab.signedIn, glab.before = "*", glabJSON("eva-wiki", "internal", false)
	f := newFakeSetup(t, true).fakes(t, glab)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	c := f.setUp(t, internalRemote)
	code, out, errOut := f.run("y\n")
	if code != 0 || len(f.installs) != 1 {
		t.Fatalf("healthy: code %d\n%s\n%s", code, out, errOut)
	}
	// The answers file setup wrote for this Mac leaves the internal switch
	// empty. It goes, so that glab gives the address to clone from.
	file := f.env.dirs.AnswersFile()
	if a, err := config.LoadAnswers(file); err != nil || a.AllowInternalRepo != nil || a.WikiRemote != internalRemote {
		t.Fatalf("answers %+v, %v", a, err)
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "\nEVA_MD_ALLOW_INTERNAL_REPO=\n") {
		t.Fatalf("the answers file:\n%s", b)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(c.WikiPath); err != nil {
		t.Fatal(err)
	}
	code, out2, errOut := f.run("\n")
	noCLIText(t, out+out2+errOut)
	if code != 0 || len(f.inits) != 1 {
		t.Fatalf("cloned again: code %d inits %d\n%s\n%s", code, len(f.inits), out2, errOut)
	}
	wantIn(t, "glab log", f.log(t, "glab"), "repo view me/eva-wiki --output json")
	wantNotIn(t, "stdout", out+out2, "internal", "Use it anyway?")
}

// refusingSSH makes git's ssh print says on stderr and exit with status, as ssh
// does when it or the server refuses, in place of realGit's fake ssh.
func refusingSSH(t *testing.T, says string, status string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "says")
	if err := os.WriteFile(file, []byte(says), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", `/bin/sh -c '/bin/cat "$0" >&2; exit `+status+`' '`+file+`'`)
}

// When git can't read a repo over SSH, peekRemote says why in setup's words,
// from what ssh or the server printed, naming only the host the address
// gives: the repo isn't there, the key was refused or the agent didn't sign
// with it, the host key is unknown, or changed or revoked, which never gets
// the advice to accept it, or the host can't be reached. None of their text
// is in the message.
func TestPeekRemoteSaysWhySSHCantRead(t *testing.T) {
	const (
		notFound    = "git found no repo at this address on %s, or none the account it signs in with can see; create it first as an empty private repo, or check the address"
		unreachable = "git couldn't reach gitlab.example.com; check the network or VPN, then try again"
		changed     = "the host key of gitlab.example.com isn't the one ssh knows, which can mean someone is intercepting the connection; ask the host's admins whether its key changed, and only then delete the line of known_hosts that ssh -T git@gitlab.example.com names as the offending key, and try again"
		revoked     = "the host key of gitlab.example.com is one ssh has marked revoked, which can mean a stolen key is being used to impersonate the host; ask the host's admins about its key before you try again"
		gitlab      = "git@gitlab.example.com:you/eva-wiki.git"
		denied      = "git@gitlab.example.com: Permission denied (publickey).\n"
	)
	for _, tc := range []struct{ name, remote, says, status, want string }{
		{"GitLab not found", gitlab, "remote: \nremote: ========================================================================\nremote: \nremote: The project you were looking for could not be found or you don't have permission to view it.\nremote: \nremote: ========================================================================\nremote: \n", "1",
			strings.ReplaceAll(notFound, "%s", "gitlab.example.com")},
		{"GitHub not found", "git@github.com:you/eva-wiki.git", "ERROR: Repository not found.\n", "1", strings.ReplaceAll(notFound, "%s", "github.com")},
		{"key refused", gitlab, "git@gitlab.example.com: Permission denied (publickey).\n", "255",
			"git's SSH key was refused by gitlab.example.com; add the key to your account there (ssh -T git@gitlab.example.com shows which account the key signs in as), or use the repo's HTTPS address"},
		{"unknown host key", gitlab, "Host key verification failed.\n", "255",
			"ssh doesn't know the host key of gitlab.example.com; run ssh -T git@gitlab.example.com once and, if the fingerprint it shows is the host's, answer yes, then try again"},
		// OpenSSH 10's sshconnect.c, for a key that isn't the one known_hosts
		// holds, and for one marked revoked; the fingerprint is made up.
		{"changed host key", gitlab, "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\nIT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!\nThe fingerprint for the ED25519 key sent by the remote host is\nSHA256:fakefingerprint7f3a.\nHost key for gitlab.example.com has changed and you have requested strict checking.\nHost key verification failed.\n", "255", changed},
		{"revoked host key", gitlab, "@       WARNING: REVOKED HOST KEY DETECTED!               @\nThe ED25519 host key for gitlab.example.com is marked as revoked.\nED25519 host key for gitlab.example.com was revoked and you have requested strict checking.\nHost key verification failed.\n", "255", revoked},
		// Without strict checking, ssh warns and goes on, here to a refused key.
		{"changed host key, not strict", gitlab, "@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n" + denied, "255", changed},
		{"revoked host key, not strict", gitlab, "@       WARNING: REVOKED HOST KEY DETECTED!               @\n" + denied, "255", revoked},
		// A key in ssh's RevokedHostKeys file.
		{"host key revoked by file", gitlab, "Host key ED25519 SHA256:fakefingerprint7f3a revoked by file /Users/you/.ssh/revoked_host_keys\r\nHost key verification failed.\r\n", "255", revoked},
		// A stale control socket's refusal, then the host's answer.
		{"stale control socket", gitlab, "Control socket connect(/Users/you/.ssh/cm-git@gitlab.example.com:22): Connection refused\r\nremote: The project you were looking for could not be found or you don't have permission to view it.\n", "1",
			strings.ReplaceAll(notFound, "%s", "gitlab.example.com")},
		// OpenSSH's sshconnect2.c, when the agent declines to sign, as when
		// its approval is denied, then ends with the key refused.
		{"agent declined", gitlab, "sign_and_send_pubkey: signing failed for ED25519 \"/Users/you/.ssh/id_ed25519\" from agent: agent refused operation\ngit@gitlab.example.com: Permission denied (publickey).\n", "255",
			"the SSH agent didn't sign git's sign-in to gitlab.example.com; approve the agent's request when it asks, then try again"},
		{"no such host", gitlab, "ssh: Could not resolve hostname gitlab.example.com: nodename nor servname provided, or not known\n", "255", unreachable},
		{"refused", gitlab, "ssh: connect to host gitlab.example.com port 22: Connection refused\n", "255", unreachable},
		{"timed out on macOS", gitlab, "ssh: connect to host gitlab.example.com port 22: Operation timed out\n", "255", unreachable},
		{"timed out on Linux", gitlab, "ssh: connect to host gitlab.example.com port 22: Connection timed out\n", "255", unreachable},
		{"no network", gitlab, "ssh: connect to host gitlab.example.com port 22: Network is unreachable\n", "255", unreachable},
		{"no route", gitlab, "ssh: connect to host gitlab.example.com port 22: No route to host\n", "255", unreachable},
		{"other", gitlab, "kex_exchange_identification: read: Connection reset by peer\n", "255", "git couldn't read this repo; check its address and that git can sign in to it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSetup(t, true)
			f.realGit(t)
			refusingSSH(t, tc.says, tc.status)
			_, err := peekRemote(context.Background(), tc.remote)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v\nwant %s", err, tc.want)
			}
			f.noTempLeft(t)
		})
	}
}

// The ssh command a message names signs in as git does: as the address's
// user, or with no user when it names none, on the address's port. A reason
// that doesn't fit the address gives the message setup always gave.
func TestReadErrorFitsTheAddress(t *testing.T) {
	const generic = "git couldn't read this repo; check its address and that git can sign in to it"
	for _, tc := range []struct{ remote, reason, want string }{
		{"ssh://git@gitlab.example.com:2222/you/eva-wiki.git", wikigit.ReadHostKey, "ssh doesn't know the host key of gitlab.example.com; run ssh -T -p 2222 git@gitlab.example.com once and, if the fingerprint it shows is the host's, answer yes, then try again"},
		{"ssh://gitlab.example.com/you/eva-wiki.git", wikigit.ReadHostKey, "ssh doesn't know the host key of gitlab.example.com; run ssh -T gitlab.example.com once and, if the fingerprint it shows is the host's, answer yes, then try again"},
		{"ssh://git@gitlab.example.com:2222/you/eva-wiki.git", wikigit.ReadHostKeyChanged, "the host key of gitlab.example.com isn't the one ssh knows, which can mean someone is intercepting the connection; ask the host's admins whether its key changed, and only then delete the line of known_hosts that ssh -T -p 2222 git@gitlab.example.com names as the offending key, and try again"},
		{"https://gitlab.example.com/you/eva-wiki.git", wikigit.ReadHostKeyChanged, generic},
		{"https://gitlab.example.com/you/eva-wiki.git", wikigit.ReadHostKeyRevoked, generic},
		{"https://gitlab.example.com/you/eva-wiki.git", wikigit.ReadAgentRefused, generic},
		{"you@gitlab.example.com:you/eva-wiki.git", wikigit.ReadKeyRefused, "git's SSH key was refused by gitlab.example.com; add the key to your account there (ssh -T you@gitlab.example.com shows which account the key signs in as), or use the repo's HTTPS address"},
		// git signs in with no user here, as ssh -T then does.
		{"gitlab.example.com:you/eva-wiki.git", wikigit.ReadKeyRefused, "git's SSH key was refused by gitlab.example.com; add the key to your account there (ssh -T gitlab.example.com shows which account the key signs in as), or use the repo's HTTPS address"},
		{"gitlab.example.com:you/eva-wiki.git", wikigit.ReadNoSignIn, generic},
		{"https://gitlab.example.com/you/eva-wiki.git", wikigit.ReadKeyRefused, generic},
		{"https://gitlab.example.com/you/eva-wiki.git", wikigit.ReadHostKey, generic},
		{"https://gitlab.example.com/you/eva-wiki.git", "", generic},
		{"/Users/you/eva-wiki.git", wikigit.ReadUnreachable, generic},
	} {
		if err := readError(tc.remote, tc.reason); err.Error() != tc.want {
			t.Errorf("%s, %q: got %v\nwant %s", tc.remote, tc.reason, err, tc.want)
		}
	}
}

// Over HTTPS, as git's own HTTP code reads it from a server on this Mac: a
// repo that asks for a sign-in git doesn't have, one that refuses the one it
// has, one that isn't there, and a host nothing answers on.
func TestPeekRemoteSaysWhyHTTPCantRead(t *testing.T) {
	f := newFakeSetup(t, true)
	f.realGit(t)
	t.Setenv("GIT_ALLOW_PROTOCOL", "http")
	for _, key := range []string{"GIT_ASKPASS", "SSH_ASKPASS", "http_proxy", "HTTP_PROXY", "all_proxy", "ALL_PROXY"} {
		t.Setenv(key, "")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/you/missing.git/") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="GitLab"`)
		http.Error(w, "HTTP Basic: Access denied. fake-account-7f3a", http.StatusUnauthorized)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	away := closed.Addr().String()
	closed.Close()
	noSignIn := "git couldn't sign in to " + host + " over HTTPS; set up git's sign-in for that host, or use the repo's SSH address"
	for _, tc := range []struct{ remote, want string }{
		{server.URL + "/you/eva-wiki.git", noSignIn},
		{"http://you:fake-token-7f3a@" + host + "/you/eva-wiki.git", noSignIn},
		{server.URL + "/you/missing.git", "git found no repo at this address on " + host + ", or none the account it signs in with can see; create it first as an empty private repo, or check the address"},
		{"http://" + away + "/you/eva-wiki.git", "git couldn't reach " + away + "; check the network or VPN, then try again"},
	} {
		if _, err := peekRemote(context.Background(), tc.remote); err == nil || err.Error() != tc.want {
			t.Errorf("%s: got %v\nwant %s", tc.remote, err, tc.want)
		}
	}
	f.noTempLeft(t)
}

// A local path that isn't a git repo gets its own plain message, whether
// the folder is there or not.
func TestPeekRemoteSaysALocalPathIsNoRepo(t *testing.T) {
	f := newFakeSetup(t, true)
	f.realGit(t)
	folder := t.TempDir()
	for _, path := range []string{folder, filepath.Join(folder, "missing.git")} {
		_, err := peekRemote(context.Background(), path)
		if want := "git found no repo at " + path + "; make one there with git init --bare, in a folder only your account can read, or give another address"; err == nil || err.Error() != want {
			t.Errorf("got %v\nwant %s", err, want)
		}
	}
}

// What the server printed, here with a made-up account and token, never
// reaches setup's output: only the reason does, at the question and when a
// missing wiki can't be cloned again, where the remote leads the message.
func TestSetupNeverPrintsWhatGitPrinted(t *testing.T) {
	const says = "Welcome to GitLab, @fake-account-7f3a!\nremote: The project you were looking for could not be found or you don't have permission to view it.\nfake-token-7f3a\n"
	f := newFakeSetup(t, true)
	f.realGit(t)
	refusingSSH(t, says, "1")
	code, out, errOut := f.answers(t, "git@gitlab.example.com:you/eva-wiki.git")
	if code != 1 || len(f.inits) != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	wantIn(t, "stdout", out, checking+"   Git found no repo at this address on gitlab.example.com, or none the account it signs in with can see; create it first as an empty private repo, or check the address\n   Remote: ")
	wantNotIn(t, "stdout", out, "project you were looking for", "Welcome")

	// A Mac set up already, whose missing wiki can't be cloned again.
	f = newFakeSetup(t, true)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	c := f.setUp(t, "git@gitlab.example.com:me/eva-wiki.git")
	if err := os.RemoveAll(c.WikiPath); err != nil {
		t.Fatal(err)
	}
	refusingSSH(t, "Welcome fake-account-7f3a\ngit@gitlab.example.com: Permission denied (publickey).\n", "255")
	code, out, errOut = f.run("\n")
	noCLIText(t, out+errOut)
	if code != 1 || errOut != "eva-md: git@gitlab.example.com:me/eva-wiki.git: git's SSH key was refused by gitlab.example.com; add the key to your account there (ssh -T git@gitlab.example.com shows which account the key signs in as), or use the repo's HTTPS address; or run eva-md setup --remote with the right address\n" {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
}
