// Package hostfake is a fake Claude Code CLI and a fake Codex CLI for tests,
// which fail the way the real ones do: Codex binds a hook's approval to a
// hash of its plugin's files, a host's version changes that rule, and an
// administrator can turn hooks or plugins off. Only tests import it, and it
// imports only the standard library.
//
// One Fake answers both CLIs, in two forms that give the same bytes and exit
// codes: Run, which has the signature of install's runner, and the claude
// and codex executables WriteCLIs writes, which run the test binary again;
// a package that uses them calls Main first in its TestMain, or in an init
// function of one of its test files. All state is kept under the root New
// is given. Approve stands for the person's review in the host, Fire for the
// host firing an event, SetVersion and SetPolicy for another host version
// and an administrator's policy, and FailNext makes a command fail before or
// after it takes effect, or stall until its context ends.
//
// Run fails as the written CLI does, with an *ExitError that holds the exit
// code and stderr, and gives what the command printed on stdout even then.
// A caller's own runner fails otherwise: install's native runner gives no
// output and an error of install's own type, which is the only one it reads
// a host's wording from, such as Codex 0.166.0's missing-folder failure. So
// a caller that uses Run in place of its runner maps an *ExitError to its
// own error type, as internal/install's tests on this fake do; one that
// passes Run as it is loses whatever it reads from a host's stderr.
//
// What the fake does, and where each behaviour and wording comes from.
// Research sections are those of docs/v2/working-papers/research/; hosts.md
// is docs/v2/working-papers/design/hosts.md; F-numbers are the facts of
// docs/v2/host-facts.md, whose host sittings (HF-00) settle them. A line
// marked "the fake's own choice" stands until HF-00 records what the real
// host does.
//
// The commands v1 runs, with v1's fakes as the source of each shape
// (internal/install/install_test.go's fakeHost, the fixtures of
// internal/host/adapter/testdata/quirks and internal/health/hosts.go):
//
//   - --version prints "codex-cli <version>" and "<version> (Claude Code)".
//     The wording is the fake's own choice; DV-11b reads only its N.N.N.
//   - codex features list prints the apps, hooks and plugins rows, read by
//     their last column; the hooks row's middle column is experimental
//     before 0.167.0 and stable from it (adapter quirk
//     codex.features-list-hooks-row).
//   - plugin marketplace add, list and remove, plugin add and remove
//     (Codex), plugin install, uninstall and validate --strict (Claude
//     Code), and plugin list, Codex's one marketplace at a time with
//     --marketplace=, in the shapes v1's fakes give. A marketplace's folder
//     is listed as root and installLocation (adapter quirks
//     claude-code.marketplace-root-keys and codex.marketplace-root-keys).
//   - codex --no-daemon app-server --stdio answers initialize and
//     hooks/list, one JSON-RPC request a line, in the shape
//     internal/health/hosts.go reads: for each folder asked, the hooks that
//     load, each with its key, eventName, pluginId, enabled and trustStatus.
//   - While a registered marketplace's folder has no manifest, Codex's
//     marketplace list and the plugin lists that take it in fail with Codex
//     0.166.0's words: "- `<name>` at <folder as added, symlinks resolved>:
//     marketplace root does not contain a supported manifest" (adapter quirk
//     codex.marketplace-folder-missing, v1's missingFolder).
//   - Claude Code refuses to install a plugin it has, which it loads in
//     place anyway (v1's TestUpgradeWhenTheHostRefusesToAddAnInstalledPlugin).
//     Making it Claude Code's rule, and its wording, is the fake's own
//     choice. Codex installs again, which copies the plugin again.
//   - Claude Code 2.1.316 keeps listing a marketplace removed with --scope
//     (v1's fakeHost; install.go's marketRemoveArgs).
//   - A headless run (claude -p, codex exec) is refused with exit 2: the
//     fake runs no model. Any other command is refused with exit 2 too.
//
// Codex's trust, with the hosts' design facts as the source:
//
//   - plugin add copies the plugin's folder to
//     <root>/codex/plugins/cache/<marketplace>/<plugin>/local/, and Codex
//     loads that copy (codex-surface 2.3; hosts.md 4.1; F-155). The last
//     segment, local, is the fake's own choice: hosts.md writes <version>.
//     plugin remove deletes the copy. Hooks load only from a copy with a
//     .codex-plugin/plugin.json (hosts.md 4.1; F-157).
//   - Each hook has a positional key,
//     <plugin>@<marketplace>:hooks/hooks.json:<event>:<group>:<handler>
//     (codex-surface "Facts the notes lack" 1; F-164), and an approval
//     stores two hashes under it: the declaration's, of event, matcher,
//     command, timeout, async, statusMessage and a context limit that isn't
//     the default (codex-surface 1.7 and fact 1; F-152, F-164), and the
//     content's, CodexContentHash of the copy: each file's path, size and
//     bytes, passing over __pycache__, .venv, *.pyc and *.egg-info, with a
//     symlink failing the hash (codex-surface 1.7 and fact 2; hosts.md 4.1
//     and 4.4; F-150, F-154). So a stray .DS_Store unapproves, a changed
//     timeout or a group inserted above eva-md's needs review, and a new
//     binary behind an unchanged command keeps trust (hosts.md 14 fact 2;
//     F-153).
//   - Before 0.164.0 a plugin hook's approval is bound to its declaration
//     only (codex-surface 1.7, changelog 0.164.0): Approve stores no content
//     hash and nothing hashes the copy, so a symlink in it stops no hook.
//     From 0.164.0 an approval made before it, which holds no content hash,
//     reads as modified, and its hook fails as for a changed copy, until
//     Approve runs again. The fake's own choice.
//   - Only Approve grants trust. No command does, and
//     --dangerously-bypass-hook-trust, on any command, is refused (exit 2)
//     and recorded (codex-surface 1.7; AGENTS.md: never grant Codex hook
//     trust on the user's behalf).
//   - hooks/list says trusted or managed (F-160), modified for a hook whose
//     approval no longer matches its declaration or its copy, and untrusted
//     for one with no approval under its key. Which of modified and
//     untrusted Codex says, and when, is the fake's own choice.
//   - Fire skips a hook that isn't trusted. A hook whose copy has changed,
//     or can't be hashed, fails with "plugin contents changed; hook
//     approval required" or "plugin contents could not be hashed: ..."
//     (codex-surface fact 2). The declared timeout is recorded, not
//     enforced.
//   - Installing the same bytes again restores trust, with no review: the
//     copy's hash matches the stored one again (hosts.md 14 fact 3; F-154).
//     The fake's own choice.
//   - Managed hooks a test declares run without review and are listed as
//     managed (codex-surface 1.7; hosts.md 4.1 and 12; F-163).
//
// Claude Code asks for no approval and loads its plugin in place, so Fire
// reads the plugin's folder each time and runs an edited hook at once
// (hosts.md 4.1 and 14 fact 9; claude-surface 2.3). Approve does nothing
// there.
//
// The defaults are Codex 0.167.0 and Claude Code 2.1.321 (plan section
// 4.2).
//
// Policy, each flag with its one effect on the CLI's output and on Fire
// (codex-surface 1.7 and 2.4; claude-surface 1.1 and 2.4; hosts.md 12).
// How each block shows is the fake's own choice (F-057 leaves policy states
// unsettled):
//
//   - CodexHooksOff: features list ends the hooks row in false, hooks/list
//     lists no hook, and Fire runs no Codex hook, managed ones included.
//   - CodexPluginsOff: features list ends the plugins row in false; plugin
//     add and marketplace add fail with "plugins are turned off by your
//     administrator"; the plugin lists list no plugin; no plugin hook is
//     listed or fired.
//   - CodexManagedHooksOnly: hooks/list lists the managed hooks alone, and
//     Fire runs them alone.
//   - ClaudeManagedHooksOnly: each entry of claude plugin list carries
//     "hooksBlockedBy": "allowManagedHooksOnly", and Fire runs the managed
//     hooks alone.
//   - BlockedMarketplace: adding the marketplace that has that name, or a
//     plugin from it, fails with "marketplace <name> is blocked by your
//     administrator"; a plugin already installed from it is listed with
//     enabled false; none of its hooks is listed or fired.
package hostfake
