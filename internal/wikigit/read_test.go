package wikigit

import "testing"

// changedKey is what OpenSSH 10's sshconnect.c prints for a host key that
// isn't the one known_hosts holds, before sshconnect2.c's "Host key
// verification failed.". The fingerprint is made up.
const changedKey = `@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
Someone could be eavesdropping on you right now (man-in-the-middle attack)!
It is also possible that a host key has just been changed.
The fingerprint for the ED25519 key sent by the remote host is
SHA256:fakefingerprint7f3a.
Please contact your system administrator.
Add correct host key in /Users/you/.ssh/known_hosts to get rid of this message.
Offending ED25519 key in /Users/you/.ssh/known_hosts:3
Host key for gitlab.example.com has changed and you have requested strict checking.
Host key verification failed.
fatal: Could not read from remote repository.
`

// revokedKey is what OpenSSH 10's sshconnect.c prints for a host key
// marked revoked, before "Host key verification failed.".
const revokedKey = `@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@       WARNING: REVOKED HOST KEY DETECTED!               @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
The ED25519 host key for gitlab.example.com is marked as revoked.
This could mean that a stolen key is being used to
impersonate this host.
ED25519 host key for gitlab.example.com was revoked and you have requested strict checking.
Host key verification failed.
`

// agentRefused is what OpenSSH prints when the SSH agent declines to sign,
// as when its approval is denied: sshconnect2.c's sign_and_send_pubkey logs
// the failure with ssherr.c's "agent refused operation", and with no key
// signed ssh ends with "Permission denied".
const agentRefused = `sign_and_send_pubkey: signing failed for ED25519 "/Users/you/.ssh/id_ed25519" from agent: agent refused operation
git@gitlab.example.com: Permission denied (publickey).
fatal: Could not read from remote repository.
`

// staleSocket is what OpenSSH's mux.c prints for a control socket whose
// master has exited, with ControlMaster no, its default, and a ControlPath
// set, before it connects without it: a refusal on this Mac, never the
// host's.
const staleSocket = "Control socket connect(/Users/you/.ssh/cm-git@gitlab.example.com:22): Connection refused\r\n"

// Each line git, ssh, curl or a host prints when a repo can't be read gives
// its reason, whatever its letter case and however a line break splits it,
// and leaves the class a commit's record names as it was. Every wording is
// checked by a line that holds it alone. Names and tokens in the lines are
// made up.
func TestReadFailure(t *testing.T) {
	for _, tc := range []struct{ line, reason string }{
		{changedKey, ReadHostKeyChanged},
		{"Host key for gitlab.example.com has changed and you have requested strict checking.\nHost key verification failed.", ReadHostKeyChanged},
		// Without strict checking, ssh prints the banner and goes on, and
		// may end with the key refused.
		{"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\ngit@gitlab.example.com: Permission denied (publickey).", ReadHostKeyChanged},
		{revokedKey, ReadHostKeyRevoked},
		{"ED25519 host key for gitlab.example.com was revoked and you have requested strict checking.\nHost key verification failed.", ReadHostKeyRevoked},
		{"@       WARNING: REVOKED HOST KEY DETECTED!               @\ngit@gitlab.example.com: Permission denied (publickey).", ReadHostKeyRevoked},
		// A key in ssh's RevokedHostKeys file, and a file ssh couldn't check.
		{"Host key ED25519 SHA256:fakefingerprint7f3a revoked by file /Users/you/.ssh/revoked_host_keys\r\nHost key verification failed.", ReadHostKeyRevoked},
		{"Error checking host key ED25519 SHA256:fakefingerprint7f3a in revoked keys file /Users/you/.ssh/revoked_host_keys: No such file or directory\r\nHost key verification failed.", ""},
		// A stale control socket's refusal decides nothing: what the host
		// answered after it does, or nothing known gives no reason.
		{staleSocket + "ERROR: Repository not found.\nfatal: Could not read from remote repository.", ReadNotFound},
		{staleSocket + "remote: The project you were looking for could not be found or you don't have permission to view it.", ReadNotFound},
		{staleSocket + "git@gitlab.example.com: Permission denied (publickey).", ReadKeyRefused},
		{staleSocket + "ssh: connect to host gitlab.example.com port 22: Operation timed out", ReadUnreachable},
		{staleSocket + "Received disconnect from 192.0.2.1 port 22:2: Too many authentication failures", ""},
		{staleSocket, ""},
		// OpenSSH.
		{"No ED25519 host key is known for gitlab.example.com and you have requested strict checking.\nHost key verification failed.\nfatal: Could not read from remote repository.", ReadHostKey},
		{"git@gitlab.example.com: Permission denied (publickey).\nfatal: Could not read from remote repository.", ReadKeyRefused},
		{"git@gitlab.example.com: Permission denied (publickey,keyboard-interactive).", ReadKeyRefused},
		{"ssh: Could not resolve hostname gitlab.example.com: nodename nor servname provided, or not known", ReadUnreachable},
		{"ssh: connect to host gitlab.example.com port 22: Connection refused", ReadUnreachable},
		{"ssh: connect to host gitlab.example.com port 22: Operation timed out", ReadUnreachable},
		{"ssh: connect to host gitlab.example.com port 22: Connection timed out", ReadUnreachable},
		{"ssh: connect to host gitlab.example.com port 22: Network is unreachable", ReadUnreachable},
		{"ssh: connect to host gitlab.example.com port 22: No route to host", ReadUnreachable},
		// A wording a line break splits.
		{"ssh: connect to host gitlab.example.com port 22: No route\n  to host", ReadUnreachable},
		// curl, through git.
		{"fatal: unable to access 'https://gitlab.example.com/you/eva-wiki.git/': Could not resolve host: gitlab.example.com", ReadUnreachable},
		{"fatal: unable to access 'https://gitlab.example.com/you/eva-wiki.git/': Failed to connect to gitlab.example.com port 443 after 3 ms: Couldn't connect to server", ReadUnreachable},
		// git, with prompts off and with them on but no terminal.
		{"fatal: could not read Username for 'https://gitlab.example.com': terminal prompts disabled", ReadNoSignIn},
		{"fatal: could not read Username for 'https://gitlab.example.com': Device not configured", ReadNoSignIn},
		{"fatal: could not read Password for 'https://you@gitlab.example.com': terminal prompts disabled", ReadNoSignIn},
		{"remote: HTTP Basic: Access denied.\nfatal: Authentication failed for 'https://gitlab.example.com/you/eva-wiki.git/'", ReadNoSignIn},
		// The hosts and git.
		{"remote: \nremote: The project you were looking for could not be found or you don't have permission to view it.\nremote: \nfatal: Could not read from remote repository.", ReadNotFound},
		{"ERROR: Repository not found.\nfatal: Could not read from remote repository.", ReadNotFound},
		{"fatal: '/Users/you/eva-wiki.git' does not appear to be a git repository", ReadNotFound},
		{"remote: Not Found\nfatal: repository 'https://gitlab.example.com/you/eva-wiki.git/' not found", ReadNotFound},
		{"fatal: repository '/Users/you/eva-wiki.git' does not exist", ReadNotFound},
		// Anything else has no reason.
		{"fatal: the remote end hung up unexpectedly", ""},
		{"", ""},
	} {
		if got := readFailure(tc.line); got != tc.reason {
			t.Errorf("%q gives %q, want %q", tc.line, got, tc.reason)
		}
		if class := classify(tc.line); class != "" {
			t.Errorf("%q gives the class %q, which a commit's record would name", tc.line, class)
		}
	}
	// The agent's refusal mentions signing, so a commit's record names it a
	// signature failure, as it always has; reading a repo gives its own
	// reason, ahead of the key refusal ssh prints after it.
	if got := readFailure(agentRefused); got != ReadAgentRefused {
		t.Errorf("the agent's refusal gives %q, want %q", got, ReadAgentRefused)
	}
	if class := classify(agentRefused); class != ClassSignature {
		t.Errorf("the agent's refusal gives the class %q, want %q as before", class, ClassSignature)
	}
}
