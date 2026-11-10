# Hosts and operations: packaging, install, upgrades, health and reach (L5 Hosts, L6 Operations)

Design section for the eva-md v2 plan. It covers the host adapter interface, packaging for each host, the first run, upgrades, health, the command surface, the cost ledger, reach, and managed installs.

What was read: the brief and the lead's notes; the audits of operations, setup, packaging, capture and transcript recovery with their verification sections; the research notes on Claude Code, Codex and coding memory tools with their fact checks; the sibling drafts for the ledger, the store, derive and deliver (the evaluation draft had not arrived). Code read in full: `internal/install`, `internal/release`, `cmd/eva-md/main.go` and `upgrade.go`, `internal/hook/spawn.go` and `maintain.go`, `internal/paths`. Read in part: `internal/health` (`health.go`, `watchdog.go`, `background.go`, `hosts.go`), `internal/config`, `internal/mcp/instructions.go`, the release workflow, `docs/releasing.md`, `docs/decisions.md`, `docs/design.md` 1120 to 1165 and 1356 to 1362. Codex's trust rules were read again in its source at commit `1885417c`: `hooks/src/lib.rs` (`hook_key`), `hooks/src/engine/discovery.rs` (`hook_hash`), `hooks/src/engine/plugin_fingerprint.rs`, `core-plugins/src/store.rs`. Nothing was run but read-only commands.

Finding ids are the register's. For this area they map to the audits as follows: OPS-01 to OPS-15 are ops-1, 2, 3, 5, 7, 8, miss-1, 4, 6, 9, 10, miss-2, miss-3, 11, 12 in that order; SET-01 to SET-15 are setup-1, 3, 4, 2, 5, 6, 9, 10, missed-1, missed-2, 7, 8, 11, 12, missed-3; MCP-04, 05, 06, 08, 10 are the packaging audit's F4, F6, F7, F5, F9.

---

## 1. Goal and the measurable bar

**Goal.** A person installs eva-md with one action and answers nothing. From then on it keeps itself current, repairs what a program can repair, and speaks only when a person alone can act. It reaches the places agents run without becoming a thing to look after.

**The bar.** Every row is a number a test or the real install can produce.

| Measure | v1 today | v2 bar | Where it is measured |
| --- | --- | --- | --- |
| Questions asked by the first run | 4 (one a typed path, one a typed `y`) | 0 | first-run scorecard (HO-31) |
| Approvals during the first run (git sign-in, signing) | up to 4 | 0 | scorecard |
| Lines printed by the first run | about 45 to 50 | at most 12 | scorecard |
| Host CLI inventories per host per install | 5 | at most 2 | scorecard |
| Host-imposed steps after install | Codex `/hooks`, again after most upgrades | Codex `/hooks` once per frozen generation; Claude Code none | conditions history |
| Codex re-approvals caused by a release | 4 recorded folder changes in 11 days | 0 for every release that keeps the generation | `codex_reapprovals_per_release` |
| Actions needed for an upgrade | find out, run a command, answer `y` | 0 (with the owner's yes), else 1 command with no question | `update_lag_h` on the canary |
| Time to detect a silent stop | never, for a person with one host | one evaluation by an observer that still runs (table in 7.7) | fault matrix, `time_to_detect` |
| Notices on a healthy install | one at every Claude start while Codex is unapproved, forever | 0 in a replayed healthy month | `false_notices_30d` |
| Notices shown at once | the first in code order hides the rest | the most severe relevant one; at most one line per chat start | fault matrix |
| Health work inside a chat-start hook | up to 35 ms of folder scanning plus six file reads | one read of a file under 1 KB | hook benchmark |
| Commands a person runs in an ordinary month | upgrade, doctor, and whatever the doctor names | 0 | field report |
| `status` output | about 50 rows, no verdict | a verdict and at most 6 lines | golden test |
| Cost a person can read | per-pass log lines, never totalled | one line in `status`, a 30-day table on request | golden rollup |
| Host names in non-test code outside the adapter | 64 lines in 29 files (narrow count) | 0 | lint test |

---

## 2. The design in one page

1. **One host adapter interface with declared capabilities.** A host is a row in a spec table plus a small package. Each area takes the half it needs: capture (the ledger's), delivery (deliver's), headless runs (derive's), packaging, probing and policy (this area's). A third adapter, `generic`, is a host with an MCP server and nothing else.
2. **Packaging in units, recorded in an install manifest.** On Codex the hooks live in a unit of two small files whose bytes are frozen for a generation, because Codex binds hook approval to a hash of every file in the plugin. Skills, the MCP declaration and the icon live in a second unit that changes freely. On Claude Code one plugin holds everything, because nothing there is bound to a hash. The manifest lists every file eva-md rendered, so any release can replace or remove another release's files.
3. **A generation is frozen late and extended by addition.** The v1 plugin already approved on a machine keeps working behind the v2 binary, untouched. Generation 1 is fixed once, after the measurements that decide its events. A later need for one more event ships as a second small frozen unit: one review, and capture never stops while it waits.
4. **The first run is `reconcile` and one statement.** It asks nothing, creates the store locally through the store area, registers the plugins through the hosts' own commands, and says once what is captured, what a model is sent and how to stop either. A remote is attached later, by a command, and its failures are never failures of memory.
5. **One short-lived tick, with two fallbacks that need no decision.** With the owner's yes, launchd (a systemd user timer on Linux) starts `eva-md tick` every 15 minutes. It evaluates health, runs the worker's due work and checks for a release. Without it, the worker a hook starts and the per-chat MCP server carry the same steps while a chat is open.
6. **Health is a freshness ledger read by one evaluator.** Evidence is content-free lines the other areas already write. The evaluator turns it into typed conditions with severity, relevance, decay and a remediation class. Hooks no longer compute health; they read one prepared line. Three observers can each see the others stop: hooks, the MCP server, the tick.
7. **Self-repair comes first, then one plain ask.** A condition is automatic (the tick or the worker fixes it), agent-run (one safe command), or an ask in one sentence that names the single step.
8. **Upgrades arrive by themselves, if the owner agrees.** A signed release manifest, verified offline with the standard library; a staged swap that keeps the previous binary; a probation period; rollback by evidence; and migrations only after probation, so a rollback never meets a store it cannot read.
9. **The command surface is verdicts.** `status` answers "is it working, and what does it need from me". The rest of a person's surface is a dozen verbs; the plumbing is hidden.
10. **Reach is ordered by what is known.** Linux first (it already builds and passes). Then the MCP-only floor, which needs no host fact we lack. Cloud sessions after a spike confirms five facts. Cowork is verified, not built. Chat on the web and mobile is not worth doing in v2.

---

## 3. The host adapter interface and capability model

### 3.1 What is wrong today

Hosts are string literals: 64 lines in 29 non-test files by the narrow count, 116 in 38 by the wide one, with two names for one host (`claude` for install, `claude-code` for updates) (OPS-05, CAP-09, CUP-11). No code reads a host's version (OPS-10). Quirks are matched by wording in place: one Codex 0.166.0 sentence, six guessed keys for a marketplace root, a column position in `codex features list`.

### 3.2 The interface

`internal/host` holds the registry and the spec table. Each host is a package under it. The interface is a set of roles, and a role is nil where the host lacks it.

```go
package host

type ID string // "claude-code", "codex", "generic"

type Adapter interface {
    ID() ID
    Names() Names                    // app name for messages, CLI name, plugin ids
    Probe(ctx context.Context, env Env) Probe // no model call, no settings file read
    Capture() ledger.Capture         // the ledger draft's 6.5; nil without hooks
    Deliver() deliver.Surface        // deliver's V-H2: Encode, per-event caps; nil without injection
    Runner() organize.Runner         // derive's 8.1; nil without a headless run
    Packager() Packager              // this area: render units, register, inspect; nil for generic
}

type Probe struct {
    CLI       string    // absolute path, symlinked folders resolved (kept from v1)
    Version   string    // what `--version` printed, parsed; "" when unknown
    Known     bool      // the spec has a row for this version
    Hooks     Tri       // feature on, off, unknown (Codex: `features list`)
    Plugin    []UnitState // per unit: registered, enabled, valid, source path
    Trust     []HookTrust // per declared event: trusted, managed, modified, untrusted, unknown
    Policy    []Block   // what the host says is off by policy; never a settings file
    At        time.Time
}
```

`Packager` is the v1 install core behind an interface: `Render(unit, binary) map[string][]byte`, `Inspect`, `Register(unit)`, `Remove(unit)`, each through the host's own CLI, run from `/`, with diagnostics matched and never shown (ops must-keeps 1 to 4, kept).

### 3.3 The capability model

Capabilities are declared in the spec, then narrowed by what the probe and the evidence show. Other areas ask `host.Caps(session)` and never branch on a host name.

| Capability | Values | Claude Code | Codex | generic |
| --- | --- | --- | --- | --- |
| `hooks` | per event: fires, may add context, cap, sync or async meaning | 33 events; context capped at 10,000 characters a string; an async hook's context arrives a turn later | 14 events; about 2,500 tokens a message, 3,000 where declared; an async hook's context arrives before the next model request | none |
| `trust` | none, per declaration, per plugin content, managed | none for plugin hooks | declaration plus a hash of every file in the plugin; positional key | n/a |
| `plugin_load` | in place, copied | in place from a local marketplace folder | copied into the host's cache at install | n/a |
| `notice` | shown as a line, shown as a warning, model only | line | warning | model only |
| `mcp` | stdio, instruction cap, deferred tools, load-at-start | 4,096 characters, deferred by default | first 512 characters self-contained | yes |
| `skills` | yes or no | yes | yes | no |
| `headless` | schema output, tools off, plan login, key | derive's 8.2 | derive's 8.2 | no |
| `record` | transcript profile versions | the ledger's | the ledger's | no |
| `surfaces` | where the host runs and which caps hold there | CLI, desktop Code tab, IDE, SSH, cloud | CLI, the ChatGPT app's Codex mode, IDE without plugins | any |

Rules:

- **The spec is data in the binary**, `internal/host/spec.go`: a row per host and version range with the capabilities, the event list, the quirks (each with the fixture that shows it), and the tested maximum version. A test fails when a quirk has no fixture.
- **Version awareness.** `Probe` reads the CLI's version once per CLI file (cached by path, size and modification time; derive's runner uses the same cache, its contract D-H2). A version above the tested maximum uses the newest row and opens `host.version_untested`, which is information until a capability check fails. The desktop app carries its own Claude Code, so two versions on one machine are normal; a session's version is the ledger's to record from the host's own record.
- **Observed beats declared.** A capability the spec grants but the evidence denies (an event that never fires, a hook reported untrusted) is off for that session. `Caps` is the spec narrowed by the probe and by the hook health lines.
- **No separate data channel for host tables.** The operations audit suggested shipping wording tables as signed data that can update without a binary. Once a binary-only release is silent and costs no approval (section 10), a data channel is a second verify and rollback path that buys nothing. Not built.

### 3.4 How it lands

This is an evolve, not a rewrite. The interface and the registry come first (HO-10); each package moves behind it when its area rebuilds it: `hook` and `catchup` with the ledger, `update` with derive, `resume` and `mcp` with deliver, `install`, `health` and `cmd` here. A lint test counts host literals outside `internal/host` and may only go down.

---

## 4. Packaging per host

### 4.1 The facts that decide it

| Fact | Source | Confidence |
| --- | --- | --- |
| Codex approves a plugin's hooks against the declaration hash and a SHA-256 over every file in the plugin root (paths, sizes, bytes), and checks it again on each hook run. Any byte change sends every hook of that plugin back to review, and capture stops until then | Codex source at 0.164.0, 0.167.0 and main; changelog 0.164.0 | read in source, not run |
| The declaration hash covers event, matcher, command, timeout, async, statusMessage and a non-default context limit. The stored key is positional: plugin, file path, event, group index, handler index | Codex source | read in source |
| Codex installs a plugin by copying it into `$CODEX_HOME/plugins/cache/<marketplace>/<plugin>/<version>/` and loads the copy. The hashed root is that copy | Codex source (`store.rs`), Codex docs | read in source, not run |
| Nothing may be written inside the hashed root: a `.DS_Store` unapproves the hooks, a symlink makes hashing fail | Codex source | read in source |
| Hooks load only from a `.codex-plugin/plugin.json` manifest; the portable root manifest loads none | Codex source; v1's live check on 0.164.0 | observed once |
| A plugin with no hooks has no hook approval to lose | follows from the two facts above | inferred |
| A user-level `~/.codex/hooks.json` is approved on its declaration only | Codex source | read in source |
| Managed hooks need no review and cannot be turned off by the user | Codex docs | read |
| Claude Code loads a local marketplace plugin in place at each session start and asks for no approval when its hooks change | v1's live record (`docs/design.md` 1132) for loading in place; Claude docs name no per-plugin hook review, and v1's changed hook files met none | loading observed; the absence of a review is to be confirmed (section 14, fact 9) |
| A top-level `bin/` stops a Claude plugin from installing in Chat and Cowork; executables may sit in `scripts/` | Claude docs | read |
| Mods are six weeks old, changed in 17 of the last 30 releases, can be turned off remotely, and are largely blocked for user plugins on Team and Enterprise logins | Claude docs and changelog | read |
| A third-party Claude marketplace does not auto-update by default | Claude docs | read |

### 4.2 Units

A **unit** is a folder eva-md renders and a registration with a host. The binary sits outside every unit at the stable path, as today (ops must-keep 11).

| Unit | Host | Plugin id | Holds | Changes |
| --- | --- | --- | --- | --- |
| `codex/hooks` | Codex | `eva-md@eva-md` | `.codex-plugin/plugin.json`, `hooks/hooks.json`. Nothing else: no icon, no skill, no MCP file | never within a generation |
| `codex/tools` | Codex | `eva-md-tools@eva-md` | the MCP declaration, the icon, any skill deliver ships | with any release; no approval, because it declares no hook |
| `claude-code/plugin` | Claude Code | `eva-md@eva-md` | plugin manifest, `hooks/hooks.json`, `.mcp.json`, icon, any skill | with any release; no approval |
| `generic/mcp` | others | none | a printed MCP entry; a desktop extension bundle where the host takes one (section 11.3) | never (it names only the stable binary) |

Why two units on Codex and one on Claude Code. The split exists to keep approved bytes still. Claude Code approves no bytes, so a split there would add a registration and a line in the plugin list for nothing. On Codex the alternative is one frozen plugin that also holds the MCP declaration and no skill ever. That is smaller by one registration, but the MCP declaration and the skill text are exactly what changed four times in v1's first eleven days, so the freedom is worth the line.

The hooks unit loses its 61 KB icon. Codex hashes the plugin root on every hook run, inside the hook's timeout; two files of about 2 KB make that cost negligible.

### 4.3 The frozen hooks unit

**The command.** One handler per event, no matcher on events where the binary filters:

```
'<home>/.local/share/eva-md/bin/eva-md' hook --host codex --event <Event> || echo '{}'
```

`--event` is new. It is free now and impossible to add later without a review. It lets the binary route before it reads up to 2 MiB of payload, and answer a Codex Stop with `{}` without v1's search of the raw bytes. The binary accepts the v1 form without it (the ledger's contract C-H2). Everything else is policy in the binary: which tools are recorded, what is delivered, every internal deadline. A changed policy is a binary change and costs nothing.

**Generation 0** is the v1 plugin as a machine has it today: seven events, tool matchers, the skills and the MCP file in the same folder. The v2 binary runs behind it unchanged. An upgrade to v2 does not touch that folder, so the upgrade costs no review and Codex capture never stops (section 15).

**Generation 1** is fixed once, at the end of Stage 3 (HO-30), with exactly the events whose handlers are on by default at that point. Fixing it late costs nothing, because generation 0 carries Codex until then, and it lets the Stage 0 measurement of what one hook start costs on Codex decide the one hard choice.

| Event | Sync | Timeout s | Context limit | Asked for by |
| --- | --- | --- | --- | --- |
| SessionStart | sync | 10 | 3000 | capture, brief (as v1) |
| UserPromptSubmit | sync | 5 | 3000 | capture, prompt delivery (as v1) |
| PostToolUse | async | 5 | default | capture of every tool (C-H1); read-time delivery on Codex |
| SubagentStart | sync | 5 | default | deliver V-H1 |
| SubagentStop | async | 5 | none | capture (C-H1) |
| Stop | sync | 5 | none | capture (as v1) |
| PreCompact | sync | 10 | none | capture (as v1) |
| PostCompact | sync | 5 | none | capture (C-H1) |
| Interrupt | sync | 1 | none | capture (as v1) |
| SessionEnd | sync | 2 | none | capture (as v1) |
| PreToolUse on the edit and shell tools | sync | 2 | default | deliver V-H1, **only if** its gate is open at the freeze (below) |

No `statusMessage`: it is part of the hash and it shows in the interface. PermissionRequest and the two model-request events are left out: eva-md decides no permission, and a process per model request is too dear.

**PreToolUse on Codex is the one disputed row.** Deliver asks for it in the single declaration. A synchronous hook on every edit and shell call costs a process start each time, whether or not delivery there is on. Deliver's own plan keeps tool-time delivery in shadow until its suite shows a gain (DL-14, Stage 3). The rule this design proposes: **a frozen unit declares only what is on by default when it is frozen.** If DL-14's gate is open at the freeze, PreToolUse is in generation 1. If it opens later, it ships as an additive unit.

**Additive units.** A later event ships as its own small frozen unit (`eva-md-hooks-2@eva-md`, one event, two files). Its hooks wait for one review; the base unit's key and hash do not change, so capture goes on meanwhile. This is what "the bytes never change" needs to be true in practice: without it the first new event after release would stop Codex capture for every user at once.

**A binary renders every generation still in the field.** `render(unit, generation)` is a pure function with one golden per generation. Reconcile keeps the generation a machine has; it moves to a newer one only when the person runs `eva-md upgrade --codex-hooks`, which says what will be asked of them and why. So no release ever forces a review, and a binary update is never held back for one.

**The test that keeps it frozen.** The repository holds one line per generation: the bundle hash, computed by a test helper that follows Codex's algorithm (`codex-plugin-bundle-v1`: sorted walk, path, kind, size, bytes) with the home path normalized. A test renders each generation at HEAD and fails when a hash differs from its line. Changing a line needs a new generation number.

### 4.4 Nothing is written inside a plugin root

- eva-md writes a unit's folder only when it renders it, by v1's atomic folder swap. The ownership marker sits at the marketplace root, outside every plugin root, as today.
- eva-md never writes under a host's own folders: not Codex's plugin cache or data folder, not `~/.claude`. Host registration goes only through the host's CLI (ops must-keep 1).
- The hashed root on Codex is Codex's copy, which other software can still disturb. The trust probe (v1's read-only `hooks/list` call, generalized to the installed generation's event set) reports `modified`. The repair is automatic: ask Codex to install the unit again from eva-md's unchanged source. The approved hash is the user's own record of those exact bytes, so restoring the bytes restores the approval, and eva-md writes no trust record. Whether Codex behaves so is fact 3 of section 14; if it does not, the repair becomes an ask.
- A property test runs every eva-md command against a fake home and asserts that each unit folder's bundle hash is the same before and after, except in the one function that renders it.

### 4.5 The install manifest

`<share>/install-manifest.json` replaces `install-record.json`, `cli-paths.json` and the meaning of "what did I render" that v1 keeps only in code. It is program-owned, written atomically under the install lock.

```json
{
  "format": 2,
  "machine": "w-k3f9x2ab",
  "binary": {
    "current": "2026.12.3", "previous": "2026.11.30", "swapped_at": "2026-12-04T09:12:00Z",
    "probation_until": "2026-12-05T09:12:00Z", "pinned_away": ["2026.12.1"],
    "slots": {"2026.12.3": {"sha256": "…"}, "2026.11.30": {"sha256": "…"}}
  },
  "units": [
    {"id": "codex/hooks", "host": "codex", "plugin": "eva-md@eva-md", "generation": 1, "frozen": true,
     "root": "marketplace/codex/plugins/eva-md", "bundle_sha256": "…", "rendered_by": "2026.12.3",
     "files": [{"path": ".codex-plugin/plugin.json", "sha256": "…", "size": 312},
               {"path": "hooks/hooks.json", "sha256": "…", "size": 1740}],
     "registered": true},
    {"id": "codex/tools", "host": "codex", "plugin": "eva-md-tools@eva-md", "frozen": false, "files": ["…"]},
    {"id": "claude-code/plugin", "host": "claude-code", "plugin": "eva-md@eva-md", "frozen": false, "files": ["…"]}
  ],
  "hosts": {"codex": {"chosen": true, "cli": "/opt/homebrew/bin/codex", "version": "0.167.0", "probed": "…"},
            "claude-code": {"chosen": true, "cli": "…", "version": "2.1.321", "probed": "…"}},
  "extras": [{"kind": "path_link", "path": "~/.local/bin/eva-md"},
             {"kind": "scheduler", "path": "~/Library/LaunchAgents/md.eva.tick.plist", "sha256": "…"}],
  "kept": [{"kind": "store"}, {"kind": "store_remote"}, {"kind": "ledger"}, {"kind": "config"}],
  "schema_reads": {"store": [1, 2], "ledger": [1, 5]}
}
```

The rule that resolves OPS-07: **a file in a managed folder is eva-md's own when some manifest lists its path there**, whichever release wrote that manifest. Such a file may be replaced or removed by any release. A file no manifest lists is foreign and still stops install and uninstall (ops must-keep 6, kept). For a v1 install, which has no manifest, the binary carries v1's closed set of rendered paths (eight per host) and adopts them.

The manifest also records which hosts were chosen, which v1's setup never reads back (SET-06), and the schema ranges this binary reads, which the upgrade and rollback rules need (the store's S-H4, the ledger's C-H3).

### 4.6 Claude Code: plugin, marketplace and mods

- **Kept:** the generated local marketplace, registered through `claude plugin`, loaded in place, swapped atomically, validated with `claude plugin validate --strict` (now only when the folder changed).
- **Hooks** are declared without a tool-name filter (C-H1) and may change with any release: capture's eleven events, plus SubagentStart, plus PreToolUse when deliver turns it on. Commands stay in exec form with no shell. One fact to confirm first: what the user sees when the binary is missing (section 14, fact 9). It matters for desktop SSH sessions, where the app copies the plugin to a remote machine that has no binary at that path.
- **Not used: a `command` marketplace source or a host marketplace's auto-update.** Both refresh a plugin folder. The folder already loads in place, and eva-md already rewrites it. Neither updates the binary.
- **Not used in v2.0: mods.** A mod could show "recalled 2 notes" as the host shows its own, or capture rows before they are stored. The interface is too young to stand on: it can be switched off remotely, and on Team and Enterprise logins a user's mod loses most of the events that matter. Revisit when the host publishes a stability statement. Nothing in this design may depend on one.
- **No top-level `bin/`.** The local plugin has none and gains none. The portable plugin for cloud sessions (section 11.4) keeps its binaries in `scripts/`.
- **A directory listing as a front door** (install inside the host, then let the agent finish) is not in v2.0. Its plugin could not name a machine's binary path and a hook may not download one. The portable plugin of 11.4 is the first step toward it, and the decision belongs after that spike.

---

## 5. The first run with no questions

### 5.1 What happens

```
curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/install.sh | bash
```

The script is as today (checks, download, checksum, reported version; ops must-keep 15) and then runs `eva-md setup`, which is now `reconcile` followed by the statement. It works the same with no terminal, so an agent can run it in its own shell.

`reconcile` is one idempotent, question-free function from (this binary, the manifest, the hosts' inventories, the preferences) to the desired state. Install, upgrade, repair and the step after a swap all call it.

1. Mint the machine id in program-owned state if missing (the store's writer id; S-H4, C-H3).
2. `store.Ensure()`, then `store.Migrate()` when no probation is running (S-H1, section 10.5). No network, no remote, no question.
3. Find the hosts: each CLI on PATH, as today. Every host found is chosen and recorded. That is safe now because a host nobody uses raises nothing (section 7.4).
4. Probe each host once. Refuse, before any change, what v1 refuses: a foreign file, a symlink, an unmanaged binary, Codex's hooks feature off (ops must-keeps 6, 9, 10).
5. Under the install lock, with the snapshot rechecked (ops must-keep 8): write the binary slot and the stable binary; render changed units; register what changed through the host CLI; install the tick if allowed; link on PATH under v1's rule (never forced); write the manifest.
6. On failure, roll back only this run's steps (ops must-keep 7).
7. Ask deliver to rebuild its pack (V-H4).

`git --version` replaces the PATH lookup as the git check (SET-15). Without git the store is deferred, by the store's own design; capture starts anyway.

### 5.2 What is said once

Setup v1 asks four questions about a repository and says nothing about what the product then does (SET-09). A question-free install that also said nothing would be worse. The statement is printed once at install and shown once in the first chat's brief, as a line for the person and not as context for the model (contract H-V1). It states; it does not ask.

```
eva.md is on. From your next chat:
  It saves your Claude Code and Codex chats on this Mac, with secrets removed. Nothing is uploaded.
  After a chat it uses your own Claude or Codex plan, on a small model, to turn the chat into notes.
  Leave a project out:  eva-md exclude <folder>     Stop the notes:  eva-md pause notes
  Stop everything:      eva-md off                  See what it is doing:  eva-md status
  Codex only: open Codex once, type /hooks and approve eva-md. Until then Codex chats are not saved.
  Your memory is a private git repository on this Mac. Back it up or share it: eva-md remote set <address>.
```

Seven lines. With the header and the result line the first run prints at most twelve. The wording of the model sentence follows derive's decision DV-D2; if the owner keeps the chat's own model, the sentence says so.

### 5.3 Preferences and program state

| What | Where | Who writes it |
| --- | --- | --- |
| Machine id, install manifest, store binding, health files | state and share folders | the program only |
| Preferences a person edits | `~/.config/eva-md/config.toml` | the person; the program never rewrites it |
| Preferences a command sets | `~/.config/eva-md/prefs.json` | `eva-md exclude`, `off`, `on`, `pause`, `resume` |

- `config.env` and its template are removed. They existed because setup asked questions and was run again as the upgrade (SET-06).
- The binding keys leave `config.toml` (the store's migration moves `wiki_path` aside).
- **Two sources stay safe because the privacy keys are unions.** A folder excluded in either file is excluded. Off in either file is off. Nobody has to learn a precedence for the keys that matter.
- **A bad line no longer turns memory off unless it has to** (SET-07). A syntax error, or an error in `exclude`, means the exclusions are unknown, so capture stops, as today, and `config.invalid` is raised at once with the line number and never the line. An error in any other key uses that key's default and raises the same condition at a lower severity. The setup audit's "last good config" fallback is not adopted: its verifier showed it would capture the very folder a mistyped exclusion was meant to leave out.
- The commands exist so that "leave this project out of memory", said in a chat, is one shell command the agent can run.

### 5.4 Consent for read-only tool grants

Pulling memory through an MCP tool raises the host's permission question on first use. A plugin cannot declare its own allow rule on either host; the only way to pre-approve is to write the host's settings, which the project rules exclude. This design recommends the same answer as deliver's DL-D9: write nothing. One read-only tool in place of three means at most one question per project; push delivery needs no permission at all; `status --all` prints the single rule a person may add themselves. The field report counts how often the question is met. The fallback, if the owner reopens the rule, is in decision HO-D5.

### 5.5 The remote helper

`eva-md remote set|show|remove` is the store's (S-H5). This area keeps the part of v1's setup that is worth keeping: the privacy rules and probes (setup must-keeps 1 to 6), the creation of a private repository through `gh` or `glab` when a person asks for it, and the one wording that gates behaviour (an internal repository needs an explicit yes). About 400 of the 1,417 lines in `setup_repo.go` and `setup_refusals.go` move to `internal/remotehelp`. The guided dialogue, the GitHub-or-GitLab guessing and the 22 hint wordings go (SET-11). Because attaching is optional and later, its commonest failures (sign-in, signing, network) are reported where a person is acting, and never block memory (SET-01, SET-14).

---

## 6. The tick, and what carries its work without it

**With the owner's yes (HO-D2, the same decision as derive's DV-D1).** A launchd user agent, `~/Library/LaunchAgents/md.eva.tick.plist`: the stable binary with `tick`, `StartInterval` 900, `RunAtLoad`, background process type, low-priority I/O, no environment of its own. It is installed with `launchctl bootstrap gui/<uid>`, needs no administrator, and is listed in the manifest so uninstall removes it. On Linux it is a systemd user timer with `Persistent=true`. macOS is expected to show a one-time "background item added" notice naming `eva-md` (the system's behaviour as documented by its vendor, not seen here; HO-01 looks once on a real Mac); the statement of 5.2 gains a line saying so when the tick is installed.

One tick, in order, each step bounded and none able to stop the next:

1. Write the tick's own heartbeat.
2. Evaluate health (section 7): local reads only, under 100 ms.
3. Run automatic repairs that are due.
4. `work.Tick()`: derive's worker with its ingest, its gated model jobs, `store.Sync` and `store.Maintain` (D-H1, S-H2).
5. If a day has passed since the last check: read the release channel, and act on it by policy (section 10).
6. Send an OS notification if one is due (7.6).

A tick with nothing due reads three small files and exits. The tick makes no model call of its own; derive's jobs carry their own gates.

**Without it, and in any case as a fallback.** The same steps run from two processes that exist already:

- the worker a hook starts after writing its output (derive's wake), which evaluates health and runs what is due, the update check included;
- the per-chat MCP server, which starts a detached `eva-md tick --from mcp` every 10 minutes while it lives, outside any tool call and never inside eva-md's own runs (V-D10).

What the fallback cannot do: anything while no chat is open, and the one alert that needs an observer outside the hosts (7.6, case 4). A Linux container without systemd runs on the fallback by design.

A managed machine may forbid user agents. Then the install records `tick: unavailable`, the fallback carries the work, and a low-severity condition says so in `status` only.

---

## 7. Health as a freshness ledger

### 7.1 What is wrong today

The model answers "did a job record a failure" and never "has the expected work happened" (OPS-01). A person with one host gets no capture alarm at all. Updates that never start leave no record and no notice. The trust notice never expires and, because the first problem in code order wins, hides every other notice (OPS-04). Failures are pooled across events, so one event that fails every time passes (OPS-12). Every notice ends "run eva-md doctor", a bare command that is on PATH only sometimes (OPS-13), and the doctor prints about 50 rows with no verdict (OPS-09). The same state is worded by four functions. And all of it is computed inside the chat-start hook.

### 7.2 Evidence

All content-free. This area writes two of the files and reads the rest.

| Evidence | Written by | Says |
| --- | --- | --- |
| `ledger/health/hooks-<day>.jsonl` | every hook run (ledger 5.3, C-H4) | time, host, event, duration, outcome, the "ran but captures nothing here" stamp |
| `health/mcp-<day>.jsonl` | the MCP server at `initialize` | time, host: a chat opened and loaded eva-md's server |
| `health/tick.json` | the tick | last run, last result of each step |
| Host activity | a metadata-only scan of the host's chat folders, bounded as v1's (ops must-keep 18) | a chat happened, whether or not a hook saw it |
| Ledger summary | `ledger.Health()` | oldest dirty spool, sessions on the hook floor for an unrecognized record, last ingest |
| `work.Status()` and derive's conditions (D-H3, D-H4) | derive | pending ranges and their oldest age, holds, the breaker, refusals |
| Store conditions (S-H3) | store | unsynced age, sign-in needed, reader too old, and the rest |
| Deliver's conditions (V-H5) | deliver | pack stale or missing, brief owed |
| Install manifest and probe | this area | units registered, generation, trust per event, versions |
| Update state | this area | last check, current, previous, probation |

The MCP start line needs the owner's leave to narrow "the server writes nothing" to "the server writes no memory" (HO-D7). Deliver's draft already narrows it for its pull log.

### 7.3 The freshness ledger

`<state>/health/freshness.json`, rewritten atomically by the evaluator. It is the answer to "what should have happened by now, and did it".

```json
{"v": 1, "evaluated": "2026-12-03T10:15:00Z", "by": "tick",
 "hosts": {
   "codex": {"relevant": true, "last_activity": "2026-12-03T10:02:11Z",
     "captured":  {"last": "2026-12-03T10:02:12Z", "lag_s": 1,   "budget_s": 300,   "state": "ok"},
     "ingested":  {"oldest_dirty": null,           "lag_s": 0,   "budget_s": 900,   "state": "ok"},
     "organized": {"oldest_pending": "2026-12-03T09:40:00Z", "lag_s": 2100, "budget_s": 86400, "state": "ok"}},
   "claude-code": {"relevant": false, "last_activity": "2026-11-12T08:00:00Z"}},
 "store":  {"synced": {"oldest_unsynced": null, "budget_s": 259200, "state": "no_remote"}},
 "starts": {"last_20": {"fresh": 18, "pending": 2, "none": 0}},
 "system": {"tick": {"last": "2026-12-03T10:15:00Z", "budget_s": 2700}, "version": "2026.12.3"}}
```

Stages per host: activity, captured, ingested, organized. Per store: committed, synced. Per chat start: whether the brief was built from memory that was current (deliver's count; EVL-07). Health is the lag between adjacent stages against a budget, so missing work shows as a growing lag even when nothing recorded a failure.

### 7.4 Conditions

`<state>/health/conditions.json`. One evaluator computes them; one table words them.

```json
{"id": "capture.silent/codex", "type": "capture.silent", "host": "codex",
 "severity": "high", "class": "ask", "ask": "approve_hooks",
 "first_seen": "2026-12-01T09:00:00Z", "last_seen": "2026-12-03T10:15:00Z",
 "evidence": {"activity": "2026-12-03T10:02:11Z", "last_hook": "2026-11-30T17:20:00Z", "trust": "modified"},
 "relevant": true, "state": "open",
 "shown": [{"via": "claude-code/line", "at": "2026-12-01T09:05:00Z"}, {"via": "os", "at": "2026-12-02T09:05:00Z"}],
 "repairs": [{"action": "reinstall_unit", "at": "2026-12-01T09:00:30Z", "result": "no_change"}],
 "quiet_until": null}
```

| Type | Opens when | Severity | Class |
| --- | --- | --- | --- |
| `capture.silent{host}` | host activity is newer than the newest hook line by 5 minutes on two evaluations in a row; or an MCP start line has no SessionStart hook line within 90 seconds | high | by cause: unapproved hooks (ask), plugin disabled or removed (ask, once), policy (ask an admin), unknown (automatic first, then ask) |
| `trust.review{host}` | the probe says a declared event is untrusted or modified | high when relevant | automatic first (reinstall the unit), then ask |
| `hook.failing{host,event}` | half or more of the last 4 or more runs of **that event** failed or ran late | high for SessionStart and UserPromptSubmit, else medium | automatic checks (disk, permissions), then ask after 3 days |
| `record.unrecognized{host}` | the ledger reports sessions on the hook floor for an unknown record format | medium | automatic: check for a release now |
| `config.invalid` | 5.3 | high for a syntax or exclusion error, else low | ask, with the line number |
| `organize.*` | derive's D-H3 | derive's | derive's |
| `store.*` | the store's S-H3 | the store's | the store's |
| `pack.*`, `brief.owed` | deliver's V-H5 | deliver's | automatic |
| `tick.stale` | no tick for three intervals while a hook ran | low | automatic: install the agent again, else use the fallback |
| `binary.missing` | the stable binary is gone while units are registered | high | automatic: restore from the slot |
| `state.unwritable`, `disk.low` | a write probe fails | high | ask |
| `host.version_untested{host}` | the CLI is newer than the spec's tested maximum | info | automatic: check for a release |
| `policy.blocked{host,what}` | the probe shows plugins or hooks off by policy | high | ask an admin; never repaired around |
| `update.*` | section 10 | info, except a failed swap | automatic |

**Relevance.** A condition about a host is relevant only while that host shows activity inside its window (14 days), or within a day of an install that registered it. A condition that is not relevant is kept and is never shown. This is what ends the Codex notice that Claude-only users see at every chat start: the host is installed for, never used, and so never mentioned.

**Decay.** Three kinds, all from evidence and a clock:

- a shown condition is shown again after 1 day, then 3, then 7, then every 14;
- a condition whose host goes quiet for 14 days becomes dormant;
- information-level conditions expire after 30 days.

A condition closes only on evidence that it is over, and a closed one is not shown again unless it reopens.

**Acknowledgement.** `eva-md status --quiet <id>` holds one condition for 30 days. `eva-md hosts remove codex` is the durable answer for a host a person does not use: it unregisters that host's units.

**Selection.** Per host, the most severe relevant condition that is due. One line per chat start at most. An unknown config key can no longer hide a diverged store.

### 7.5 Remediation classes

| Class | Who acts | Examples | Rule |
| --- | --- | --- | --- |
| Automatic | the tick or the worker | record a moved CLI path again; rebuild a derived file; reinstall a unit whose copy was disturbed; restore the stable binary; fix the mode of eva-md's own folders; retry a sync; check for a release; replay dead letters (derive) | idempotent; no model call beyond derive's own; never writes host settings; **never re-enables or re-adds what the host says the person disabled or removed** |
| Agent-run | the agent, with one command | `eva-md repair`, which runs every automatic repair now plus those that need a chat's own environment (the SSH agent, the chat's PATH); `eva-md upgrade` when automatic installs are off | one command, named by absolute path unless the link exists, safe unattended, prints one verdict line |
| One plain ask | the person | approve hooks in Codex; sign in to a host; fix line N of the config; free disk space; ask an admin | one sentence that names the single step; no "run the doctor" |

The second class is deliberately thin. Most of what v1's doctor tells a person to type is mechanical, and moves to the first class (OPS-09, OPS-15).

The asks, as shown:

- "eva.md is not saving your Codex chats yet. In Codex, type /hooks and approve eva-md. That is the only step."
- "eva.md cannot turn chats into notes on Claude Code: Claude Code is signed out. Run claude auth login."
- "eva.md is off: line 14 of its config cannot be read. Fix that line; nothing else is needed."
- "Your organization's settings stop eva.md's hooks in Codex. Only an admin can allow them. eva.md stays off there."

They are written as statements of fact, which is also what the host's own guidance asks of text a hook returns.

### 7.6 Channels: an alert that does not depend on the failing part

The evaluator writes `<state>/health/notice.json`: per host, the one selected line, who it is for, and when it may next be shown. Hooks and the MCP server read that file and compute nothing.

```json
{"v": 1, "evaluated": "2026-12-03T10:15:00Z", "ttl_s": 2700,
 "lines": {"claude-code": {"id": "c7f1", "to": "user",
   "text": "eva.md is not saving your Codex chats yet. In Codex, type /hooks and approve eva-md. That is the only step."},
   "codex": null, "generic": null}}
```

| Channel | Carries | Works when |
| --- | --- | --- |
| The host's own line at a chat start (`systemMessage`: a line on Claude Code, a warning on Codex) | the selected line for any host | that host's hooks run |
| The other host's chat start | the same file | either host's hooks run |
| The first result of a pull in the same chat | one line owned by this area, in a fixed position, once per server process (contract H-V2) | hooks are dead on that host but its MCP server loaded |
| An OS notification from the tick (`osascript` on macOS, `notify-send` on Linux) | high-severity conditions with no chat-borne delivery for 24 hours; at most one a week per condition, three in all, then dormant | the tick runs, whatever the hosts do |
| `eva-md status` | everything | a person or an agent asks |

Three observers, each able to see the others stop:

- **Hooks dead on a host**: seen by the MCP server of the same chat (a start line with no hook line), by the tick (activity with no hook line), and shown through the other host, the pull result, or the OS.
- **The tick dead**: seen by the worker a hook starts and by the MCP server (a stale heartbeat); repaired automatically.
- **The worker stuck**: seen by the tick as a growing lag.
- **Everything of eva-md gone from the hosts** (plugin removed or disabled, one host only): only the tick still runs, and only the OS can say so. Without the tick this case stays silent. That is the residual gap, and the main reason to ask for the tick.

The MCP server runs the evaluator once, 90 seconds after it starts, so the same-host case needs no tick. That replaces v1's 1.5-second wait for a heartbeat at `initialize` and its 130 lines, which deliver's draft removes; the instructions stay fixed and byte-stable.

### 7.7 The fault matrix (the acceptance suite)

Each row is a scripted test over a fake home with an injected clock. "Evaluations" are evaluator runs by an observer that is still alive.

| # | Fault | Detected by | Shown through | Budget |
| --- | --- | --- | --- | --- |
| 1 | Codex hooks unapproved; Codex is the only host used | MCP server at 90 s; tick | pull result in that chat; OS after 24 h | 1 evaluation |
| 2 | The same; Claude Code also used | as 1 | Claude Code's next chat start | 1 evaluation, next start |
| 3 | The same; Codex never used | nobody | nothing | 0 notices in 30 days |
| 4 | Plugin removed or disabled; one host; no MCP | tick only | OS at once | 2 evaluations |
| 5 | Stable binary missing | tick, worker | nothing: restored from the slot | 1 evaluation |
| 6 | SessionStart fails every time; other events fine | per-event lines | the next UserPromptSubmit of that host, or the other host | 4 runs |
| 7 | The worker never starts | tick | nothing unless it lasts: the tick runs the work | 1 evaluation |
| 8 | Organizing held for sign-in | derive's condition | next chat start on any host | next start |
| 9 | Sync failing for 72 hours with a remote | store's condition | next chat start | next start |
| 10 | The tick stops | worker, MCP server | nothing: reinstalled, else fallback | 1 chat event |
| 11 | Config has a syntax error | the hook's own failed read | next chat start on any host; OS after 24 h | next event |
| 12 | A host update changes its record format | the ledger's counter | nothing for 7 days: a release check runs | 1 evaluation |
| 13 | State folder not writable | tick and MCP write probes | OS, or the other host | 1 evaluation |
| 14 | A healthy month, replayed from fixture evidence | | | 0 notices |
| 15 | Two conditions at once (unknown config key and a diverged store) | | the more severe one; the other after it closes | never hidden |

---

## 8. A command surface reduced to verdicts

v1 lists 19 commands in its usage line. A person needs to know whether it works and how to stop it.

| For a person | What it answers or does |
| --- | --- |
| `eva-md status` | the verdict (below). `--all` adds every row; `--cost` the cost table; `--json` the condition and freshness files; `--probe` asks the hosts, read-only |
| `eva-md off`, `on` | everything stops, or starts: no capture, no delivery, no jobs |
| `eva-md pause notes`, `resume notes` | organizing only |
| `eva-md exclude <folder or remote>`, `include` | leave a project out |
| `eva-md hosts`, `hosts add|remove <host>` | which hosts eva-md is registered with |
| `eva-md remote …`, `sync`, `undo`, `log`, `why` | the store's |
| `eva-md show`, `search`, `forget`, `note` | deliver's and the ledger's |
| `eva-md upgrade` | check, verify and install now; no question |
| `eva-md repair` | run every repair now; one verdict line |
| `eva-md uninstall` | preview, then `--apply` |

Hidden from the usage line and still present: `hook`, `mcp`, `tick`, `work`, `reconcile`, `selfcheck`, `connect`, `admin`. Aliases for one release: `setup` and `install` (reconcile), `doctor` (`status --all --probe`; `doctor --live` stays as it is), `update` (derive's `work`), `maintain` (`tick`). Removed: `init`, `commit`, `index`, `validate`, `system write` (frozen with system pages by derive's DV-D6), `inbox clear` (the ledger's `forget`).

Every command prints its verdict as the first line and takes `--json`. Every message that names a command uses `install.Command()`: the short name when the link exists, the full path otherwise (OPS-13).

```
eva.md is working.
  Claude Code   saving chats · last chat 12 min ago, its notes ready 3 min after it ended
  Codex         not used in the last 14 days
  Memory        214 items · on this Mac only (no backup remote)
  Today         about 41k tokens of your Claude plan for notes (21% of eva.md's daily budget); about 3k tokens added to 6 chats
  Version       2026.12.3 · up to date, checked 5 h ago
```

```
eva.md needs one thing from you.
  Codex is not saving chats: its hooks wait for your approval. In Codex, type /hooks and approve eva-md.
  Everything else is working.
```

Exit status: 0 working, 1 needs you, 3 cannot tell. `status` writes nothing (ops must-keep 21, kept): it evaluates in memory.

---

## 9. A cost ledger the user can read

Cost is logged per pass today and never totalled or shown (EVL-08), and the default was the dearest setting (PRD-12). The ledger is a rollup of what other areas already record; this area adds no measuring of its own.

| Cost | Source | Unit |
| --- | --- | --- |
| Organizing | `work.Cost(day, host)` from derive's journal | tokens in, cached in, out; calls; model; billing; share of the budget |
| Injection | delivery records (ids and sizes, never text) | characters per chat by surface: start, prompt, tool, subagent, compact, pull |
| Hook time | hook health lines | runs, p50, p95 per event |
| Disk | sizes of the ledger, the store, the logs, the binary slots | MB |
| Network | the tick's own record | release checks and syncs, counts |

`<state>/health/cost-<month>.jsonl`, one line per day and host:

```json
{"day": "2026-12-03", "host": "codex",
 "organize": {"calls": 7, "in": 41200, "cached_in": 30100, "out": 3900, "weighted": 63710, "budget": 200000, "billing": "plan", "models": {"small": 7}},
 "inject": {"chats": 9, "start_chars": 16200, "prompt_chars": 2100, "tool_chars": 0, "pull_chars": 900},
 "hooks": {"runs": 412, "p50_ms": 11, "p95_ms": 34},
 "disk": {"ledger_mb": 48, "store_mb": 3}}
```

What a person reads is one line in `status` and a 7 and 30 day table under `status --cost`. Tokens and a share of eva-md's own budget, never money: a plan allowance is not dollars, and a price table would be wrong within a month. Cached and uncached input stay apart, as the evaluation research asks. The same file gives the evaluation its "background tokens per task" figure.

---

## 10. Upgrades that reach users

### 10.1 What is wrong today

No code path looks for a release except a person's `eva-md upgrade` (OPS-03). The upgrade runs the whole guided setup and, in a terminal, asks a question whose default is no (SET-06). A download is checked only against a file from the same release (OPS-11). Meanwhile the hosts update themselves: Codex shipped 22 minor releases in 16 weeks, and the fix for a changed record format is "install a newer eva-md", which waits for a person to notice.

### 10.2 The channel and the manifest

The tick (or the fallback) reads two small files from the newest release at most once a day, with a jitter: `release.json` and `release.json.sig`, from `github.com` and its asset host only, over HTTPS, under v1's redirect and size rules. The request carries nothing about the machine: no version, no id. Hooks make no network call, as now.

```json
{"format": 1, "tag": "2026.12.3", "commit": "<40 hex>", "published": "2026-12-03T10:12:00Z",
 "assets": {"darwin-arm64": {"name": "eva-md-darwin-arm64", "sha256": "…", "size": 15234567},
            "darwin-amd64": {"…": "…"}, "linux-amd64": {"…": "…"}, "linux-arm64": {"…": "…"},
            "plugin-portable": {"name": "eva-md-plugin.zip", "sha256": "…"}},
 "from_min": "2026.11.6",
 "schema": {"store": {"reads": [1, 2], "writes": 2}, "ledger": {"reads": [1, 5], "writes": 5}},
 "generations": {"codex/hooks": [0, 1]},
 "soak_hours": [24, 96],
 "yanked": ["2026.12.1"],
 "note": "one plain line"}
```

**The signature** is an SSH signature (`ssh-keygen -Y sign`, namespace `eva-md-release`, ed25519) over the manifest's bytes. One format serves both readers: the install script verifies it with `ssh-keygen -Y verify`, which every Mac has, and the binary verifies it with `crypto/ed25519` and about 100 lines that parse the signature envelope by hand. No dependency is added. The binary embeds two public keys, so a key can be rotated by a release signed with the old one. Where the private key lives is decision HO-D3.

Checks, in order, before anything downloaded runs (ops must-keep 15, extended):

1. The signature verifies against an embedded key.
2. The tag is newer than the installed one, is not yanked, and the installed version is at least `from_min`.
3. The manifest's schema ranges read this machine's store and ledger.
4. The asset's checksum matches the manifest.
5. The new binary reports the tag.
6. The new binary passes `selfcheck` (10.3).

**Soak.** A machine installs a release at a moment inside `soak_hours` after `published`, fixed by a hash of its machine id. With no telemetry this protects only if the maintainer sees a bad release and publishes a newer one that yanks it inside the window; it is cheap, so it is kept, and the limit is stated. A machine on a yanked version takes the newer release at once.

### 10.3 The staged swap

Under the install lock, when the worker holds no model job:

1. Download into a new folder under the share folder (v1's code). Verify as above.
2. `selfcheck`, run from the new binary against a scratch home: it renders every unit and compares the frozen ones byte for byte with what is installed; it opens the store and the ledger read-only and reports the schema; it decodes one canned payload per host and event. It needs no network and under 5 seconds.
3. Classify. **Binary only**: every unit's bytes are unchanged, so nothing touches a host. **Content**: an unfrozen unit differs, so that unit is written and registered again; no approval is involved. A release never changes a frozen unit of the generation a machine has (4.3), so there is no third class.
4. Write the slot `<share>/versions/<tag>/eva-md`. Replace the stable binary atomically, as a regular file, as today: a running hook keeps its old file, and the path the hosts call never changes (ops must-keep 11).
5. From the **old** process, run the new stable binary's `selfcheck --installed`. If it fails, put the previous binary back at once. This covers a binary that cannot start, which nothing later could detect.
6. Run the new binary's `reconcile`. Record the swap, the baseline of the health figures, and the end of probation in the manifest. Tell the worker that the binary and the contract version changed (D-H2), so dead letters are replayed.

Two slots are kept: current and previous.

### 10.4 Probation and rollback

For 24 hours and at least 20 hook runs after a swap, the evaluator compares against the baseline taken at the swap. A regression is one of:

- an event with 4 or more runs since the swap fails half the time, where it failed under a tenth before, and the host's version has not changed in between;
- three or more panic lines from the new binary;
- `selfcheck --installed` fails on a later tick;
- derive's breaker opens for every host inside probation, having been closed before, on the known-good fixture.

A regression puts the previous binary back, runs its `reconcile`, pins the bad tag away, and records `update.rolled_back`, which shows in `status` only. One rollback per tag, and no second rollback inside seven days: a host outage must not walk a machine backwards twice.

### 10.5 Swap, probation, then migrate

A rollback must never meet a store or a ledger it cannot read (the store's S-H4). So a release that carries a migration does not run it at the swap. `reconcile` calls `store.Migrate()` and the ledger's import only after probation ends clean. During probation the new binary works on the old layout, which its declared read range must include. Once a migration has run, the previous slot is dropped if it cannot read the result.

This is also why adjacent releases must share on-disk formats. A long chat keeps its MCP server, an old binary in memory, while hooks run the new one. The rule: **releases N and N-1 read each other's state files**, and a server that finds it cannot answers a pull with "eva.md was updated; start a new chat for memory tools" and nothing else.

### 10.6 Policy, and the release pipeline

`updates = "install" | "check" | "off"` in the preferences; a restrict-only policy file can pin it (section 12). `install` is the recommended default (HO-D1). With `check`, the tick only records `update.available`; after 7 days it becomes one line at a chat start, and `eva-md upgrade` installs with no question, so an agent can run it. `off` is v1.

`eva-md upgrade` uses the same path with no soak. It no longer runs the guided setup.

The release rules stand (ops must-keep 16): main only, green CI, a recorded approval or a signed tag, provenance attested, tags never moved. Added: the release workflow and `scripts/release.sh` build Linux assets, write `release.json` from the built files, sign it in a job bound to the approval-gated environment, and publish both files with the rest. Nothing cuts a release by itself. An unsigned release is simply never installed automatically.

Not done: Developer ID signing and notarization. No path here passes through Gatekeeper (a file fetched by curl or by the binary carries no quarantine mark). It becomes necessary only for a package installer or a managed rollout (HO-D8).

---

## 11. Reach

### 11.1 The order, and why it differs from the draft

| Order | Surface | What it gets | Needs | Stage |
| --- | --- | --- | --- | --- |
| 1 | Linux machines: workstations, SSH hosts, dev containers, the desktop app's SSH sessions | everything | release assets, the OS gate lifted, a systemd timer or the fallback | 4 (HO-40) |
| 2 | Hosts with MCP and no hooks: the Claude desktop app's Chat tab on the person's own computer, the Codex IDE extension, other MCP hosts | read-only memory through one tool | the `generic` adapter and a printed entry; deliver's hook-free floor | 4 (HO-41) |
| 3 | Claude Code cloud sessions | a brief and recall from a clone of the store; capture only if the spike allows | sync, a portable plugin, five confirmed facts | 4 (HO-42) |
| 4 | Cowork | whatever the portable plugin does there | the same facts, checked there | 4, verify only (HO-44) |
| 5 | Chat on the web and mobile | nothing | a hosted server | not in v2 |

The draft puts the MCP-only floor fourth. It is the smallest piece, it depends on no host fact we lack, and it is the only way into the Claude desktop app's Chat tab, which the owner named. It should come second.

Surfaces v1 already claims and has never observed stay in scope and get a line each in the field report: the desktop app's Code tab (same files as the CLI, its own bundled Claude Code), and the ChatGPT app's Codex mode (shares Codex's config; whether one hook approval covers both is fact 5 of section 14).

### 11.2 Linux

Linux already builds and passes the tests. What keeps it out is policy and assets: the OS check in setup and the script, darwin-only release assets, five `runtime.GOOS` sites. The work: `linux-amd64` and `linux-arm64` assets, the gates behind `platform.Supported()`, a systemd user timer with the fallback where there is no systemd, `notify-send` where it exists, and the first-run scorecard on Linux. Each Linux machine is its own writer and meets the others through the store's sync. Windows is not in v2: 36 non-test files use unix-only calls; WSL is Linux.

### 11.3 The MCP-only floor

`eva-md mcp --host generic` serves deliver's read tool and fixed instructions (10.4 of its draft). This area adds:

- `eva-md connect <host>`, which prints the entry for that host's MCP config and the place it goes, and writes nothing;
- for the Claude desktop app, a desktop extension bundle whose server is a launcher for the stable binary, if the bundle format accepts a native command (fact 12); otherwise the printed entry.

What such a chat gets: memory made in the coding hosts, readable on request. What it does not get: capture, a brief at start, or anything unasked. The value is real and small, and that should be said in the README as plainly as here.

### 11.4 Cloud sessions

A cloud session is a Linux machine that starts empty and is thrown away. It reads nothing from the person's home; it gets what the repository holds and what is enabled on the person's account, and account-synced plugins appear to run their hooks there (changelog evidence only). v1 skips these sessions by a guard.

The smallest useful thing is a **read path on an ephemeral machine**:

- A portable plugin, one release asset: the Linux binaries under `scripts/`, hooks that name `${CLAUDE_PLUGIN_ROOT}/scripts/eva-md-launch`, no machine path anywhere. The person adds it to their account once.
- At SessionStart the hook finds no store and asks the worker to start, as any hook does. The worker, a detached process and not a hook, clones the store's remote if the session can reach it. The brief arrives with the first prompt, which deliver's "brief owed" already handles.
- Nothing is captured. The first-run statement says so for this surface.

A **write path** (capture, organize inside the session, push as a short-lived writer) is built only if the spike shows a headless run works there on the session's own login, and after sync has proven itself on real machines.

The spike (HO-42, first half) answers: do a synced plugin's hooks fire with a plugin root and a writable data folder; may a hook's detached child run and reach the network; can the session reach the store's remote with a credential; does a file under `scripts/` keep its executable bit through the account; does `claude -p` run inside; how long the machine lives after the last turn.

Limits, said now: a store whose remote the cloud cannot reach (a self-hosted git server behind a private network, as on the one real install) gets nothing there. Not worth doing: a relay service of eva-md's own, or moving the raw ledger off the machine.

### 11.5 Cowork, and Chat

Cowork loads a plugin's hooks, and its local MCP only when it runs on the person's computer, and it never reads the CLI's folders. On consumer plans it now runs in the cloud with no local option. So it is the cloud case with another host around it: verify the portable plugin there after 11.4 works, and build nothing for it. If hooks cannot run a binary there, it falls to the floor of 11.3 where local MCP loads, and to nothing where it does not.

Chat on the web and mobile takes only skills and remote connectors. A remote connector means a server that holds the person's memory and an account to reach it, which is the opposite of local-first, no account and no telemetry. Not worth doing in v2. It should be looked at again only if the owner accepts a server a person hosts themselves, or the hosts let a local server reach Chat on every plan.

---

## 12. Managed and enterprise installs

Decision 16 says eva-md assumes plugins, hooks and headless runs are allowed, and that health reports what it can see. v2 keeps the assumption and makes the second half real.

- **Detect, say, and stop.** The probe reads what each host's own CLI reports: plugins off, the hooks feature forced off, managed-only hooks. That opens `policy.blocked`, whose only remedy is an admin. eva-md never works around a policy and never reads a managed settings file.
- **An admin kit, printed and never applied.** `eva-md admin print --host codex|claude-code` prints the snippet an admin deploys: for Codex, a managed hooks block that calls the stable binary through the shell with `$HOME`, plus the marketplace allow source; for Claude Code, the managed marketplace and enabled-plugin entries. Managed Codex hooks need no review, so an organization gets a Codex install with no approval step at all. With `eva-md setup --managed-hooks codex` the hooks unit is not registered, so nothing is captured twice; the probe already accepts `managed` as trusted.
- **A restrict-only policy file.** `/Library/Application Support/eva-md/policy.toml` (and `/etc/eva-md/policy.toml`), root-owned. It can only narrow: `updates`, `organize = false`, `remote.allowed_hosts`, `reach.cloud = false`. It cannot turn anything on. This lets an organization pin the version, forbid model use by background jobs, or keep memory on its own git server.
- **A network receipt.** `status --all` lists the hosts eva-md itself contacted in the last 30 days, by name and count: the release host and the store's remote. Model calls go through the host CLIs to the provider the chat already uses.

This is small, and it is Stage 4, after the field report says whether the one real install is managed.

---

## 13. Kept, rebuilt, evolved, removed

### 13.1 Kept as code

The install core (`install.go`, `host.go`, `uninstall.go`, about 1,740 lines): preflight, the lock, the recheck under it, the atomic folder swap, the rollback of this run's steps, the ownership marker, the CLI path resolution through symlinked folders, the PATH link rule. The release package's download, redirect and size rules and its checksum check. The release workflows and `scripts/release.sh`. The bounded, metadata-only activity scan. The read-only trust probe. The detached-spawn recipe. The privacy probes of setup.

### 13.2 Invariants

All 22 of the operations audit's must-keeps, the setup audit's 1 to 9 and 25 to 27, and the packaging audit's 7 to 16 are kept, with these changes:

| Must-keep | Change | Why |
| --- | --- | --- |
| ops 5: plugin bytes carry no version or date | kept and made testable for frozen units (a golden bundle hash per generation); the tools unit still carries none, by choice | OPS-02 |
| ops 19: the trust notice is satisfied only by a Codex SessionStart heartbeat newer than the flag | satisfied by a hook line **or** by the read-only probe reporting every declared event trusted or managed; and shown only while Codex is in use | OPS-04; the audit's verifier found a person whose Codex chats are all in excluded folders keeps the notice for good |
| ops 20: the probe wants exactly 7 events | it wants exactly the installed generation's events | generations |
| ops 14: uninstall never touches the wiki | extended to the store and the ledger, which now sit under eva-md's own folders: `uninstall --apply` removes program files only; `--all` adds state and config; memory goes only with `--delete-memory` and the typed words, after the unsynced counts are shown | S-H1 |
| packaging 8: a plugin folder is regenerated only when its bytes change | kept; `claude plugin validate` also runs only then | fewer host calls |
| setup 19: an unreadable config stops capture | kept for a syntax or exclusion error; any other key falls back to its default | SET-07 |
| "no hook computes health" | new: the chat-start hook reads one prepared line | latency, OPS-01 |

The AGENTS.md list of commands not to run against the real machine gains `reconcile`, `repair`, `tick`, `hosts add|remove`, `connect` with a bundle, and `upgrade` in its new form. `status` and `upgrade --check` stay safe previews.

### 13.3 Rebuilt, and what a smaller change would leave

| Rebuild | Findings | What a smaller change would leave |
| --- | --- | --- |
| Health signalling: evidence, freshness ledger, conditions, channels | OPS-01, OPS-04, OPS-09, OPS-12, OPS-13, EVL-07, CUP-07, CAP-11 (the heartbeat's cost) | Adding age checks to v1's rows and sorting by severity would fix OPS-04 and part of OPS-01. It would leave a person with one host unwarned, health computed inside the hook, four formatters, and no channel when hooks are dead |
| Codex packaging: frozen hooks unit, free tools unit, generations | OPS-02, MCP-06, SET-12, PRD-07 | "Stop changing the one plugin" freezes skills and the MCP declaration with the hooks, or keeps paying a review; and it has no answer to the first new event |
| The first run: `reconcile` and a statement | SET-01, SET-06, SET-09, SET-11, SET-14, SET-15, PRD-07 | Defaults and `--yes` on today's setup would leave the remote as a precondition of capture, and the upgrade as "run setup again" |
| Upgrades: signed manifest, staged swap, probation, rollback | OPS-03, OPS-11, SET-06 | A daily "a newer release exists" line (an evolve) gets the signature and the question-free upgrade but leaves every fix waiting for a person |

### 13.4 Evolved

- The install core into units and a manifest (OPS-07, OPS-14, OPS-15).
- Hosts into adapters and a spec with versions (OPS-05, OPS-10, CAP-09, CUP-11, MCP-04).
- Background triggers into the tick and its fallbacks, with derive (OPS-08, CAP-07).
- The config into preferences, program state and commands (SET-06, SET-07).
- The doctor into `status` (OPS-09), where setup's own "N passed" verdict form already pointed.

### 13.5 Removed

`config.env` and its template. The guided setup dialogue, the host guessing and the hint wordings (about 1,000 of the 1,417 repository-hosting lines, and most of setup.go's question flow). `needs-trust.json` (a condition now). `install-record.json` and `cli-paths.json` (the manifest). The heartbeat files (the ledger's health lines). The day's attempt marker and the push-retry marker (the tick). The doctor's flat rows as the default output, the always-unknown managed-policy row and the 17 per-event rows. The 28 "run eva-md doctor" strings. `QuickNotice`'s work inside the hook.

---

## 14. Host facts this design leans on, and how each is confirmed first

All from docs and source read on 2026-11-10. Both hosts change weekly. HO-01 is an owner-run script that previews by default and, with the owner's go-ahead, uses a scratch plugin in a scratch marketplace so eva-md's own registration is never touched. No Stage 2 package starts on a row that has no answer or no chosen fallback.

| # | Fact | Used for | How to confirm | If false |
| --- | --- | --- | --- | --- |
| 1 | Codex's hook approval for a plugin covers that plugin's files only; a second plugin with no hooks can change freely | the unit split | read `hooks/list` before and after changing a byte of the scratch tools plugin | one frozen plugin that holds everything, no skills on Codex |
| 2 | Replacing the binary behind the command leaves approval intact | every upgrade | the same probe after swapping a scratch binary | none exists short of managed hooks; v1 already relies on it |
| 3 | Installing a unit again with identical bytes turns `modified` back to `trusted` with no review | automatic repair | add a stray file to the cached copy, probe, reinstall, probe | the repair becomes an ask |
| 4 | Codex loads its cached copy, and where that copy is | "nothing in the root" and the repair | list the cache folder; have a scratch hook print the names of its environment variables | eva-md's own folder is the root: keep v1's cleaning of OS metadata there |
| 5 | Approval is stored in the user's config layer and holds for the CLI and the ChatGPT app's Codex mode alike | one review covers both | approve in one, probe from the other; the field report | the ask names both places |
| 6 | SubagentStart, SubagentStop and PostCompact fire for a plugin's hooks on the current release; an async hook's context lands before the next model request | generation 1's rows | the ledger's recorder (LG-03), deliver's DL-01 | the row leaves generation 1 before the freeze |
| 7 | What one hook start costs on Codex through its shell, and what hashing a 2 KB root adds | the PreToolUse row | 200 timed runs on the real install (deliver's fact 16) | no pre-tool hook on Codex |
| 8 | A detached child of a hook that exits in time survives on both hosts | the wake, the fallback tick | v1's run log already shows detached updates finishing; a scratch hook on Codex | the tick alone carries the work |
| 9 | On Claude Code: a changed hooks file in a local plugin is picked up at the next session with no approval step; and what a person sees when an exec-form hook's binary is missing | one plugin on Claude Code that changes freely; SSH copies of the plugin, uninstall races | change a timeout in the scratch plugin and start a chat; move the scratch binary aside once and start another | if a review appears, Claude Code gets the Codex split; if a missing binary shows an error, a guarded shell form for Claude Code's hooks |
| 10 | The MCP server starts with a chat even when that host's hooks are not approved or fail | the same-host observer | start Codex with hooks unapproved and read the start line | that case falls to the tick and the other host |
| 11 | A launchd user agent installs without an administrator, runs after a wake, and is allowed on the real install; `osascript` can post a notification from it without a prompt | the tick, the OS channel | `launchctl print gui/<uid>` and one manual notification, by the owner | the fallback; no OS channel on macOS |
| 12 | A desktop extension bundle may name a native command | the Chat tab floor | read the bundle format's manifest reference; install one by hand | the printed entry only |
| 13 | The release download addresses work for new asset names from a background job on the real install's network | the channel | v1's `upgrade --check` there | `update.unreachable` in `status`; manual upgrades |
| 14 | Account-synced plugins run hooks in cloud sessions | 11.4 | the spike | no cloud reach in v2 |

A canary keeps the first ten current (HO-50): each night, on a throwaway runner, the newest host CLIs, an install, the probes, a binary swap, an uninstall.

---

## 15. Migration: nothing a user has is lost, and nothing asks for a review

An existing v1 install reaches v2 through v1's own `eva-md upgrade` or the install command, both of which download the new binary and run its `setup`. So v2's `setup` must do the whole move with no question.

1. **Preflight, changing nothing.** Read the v1 config. Take v1's update and maintain locks without waiting, as uninstall does, and stop if a v1 job runs. Probe the hosts. Refuse only what v1 refuses.
2. **Adopt.** Write the manifest from what is there: v1's files by the embedded path list and the ownership marker, the chosen hosts from the install record.
3. **Binary.** Write the slot, replace the stable binary. v1's hook declarations call `eva-md hook --host <name>`, which the v2 binary answers.
4. **Claude Code.** Render and register the v2 plugin. No approval is involved.
5. **Codex: do not touch the plugin folder.** It stays generation 0, approved as it is. Capture goes on without a gap. Its skills and its MCP file stay v1's bytes, so the commands those skills name (`search`, `show`) stay as working aliases while generation 0 is in the field. The move to generation 1 is the person's choice, once, with `eva-md upgrade --codex-hooks`, which says: one visit to `/hooks`, and what it adds.
6. **Data, after probation.** The ledger's shadow and import, then the store's migration, in the order those areas give (the ledger first: S-L2). This area only orders them: never at the swap, never while a previous binary that cannot read the result might come back.
7. **Config.** The binding keys leave `config.toml` with the store's migration. `config.env` is left where it is, unread, and the statement says it is no longer used; it is the person's file and an upgrade does not delete it.
8. **Old state.** `needs-trust.json` becomes a condition and is removed. `cli-paths.json` and `install-record.json` are folded into the manifest and removed. Heartbeat files and the commit markers are removed by the tick after 30 days.
9. **The way back.** For 14 days the store's `migrate --abandon` restores v1's wiki path. The previous binary slot is v1's last release until a migration runs.

The wiki and the inbox are never written or removed by install, upgrade or uninstall. That is the whole of this area's duty to them, and the uninstall change in 13.2 is what keeps it true now that the store lives under eva-md's own folders.

Test: fixtures of the on-disk state of each published v1 release (2026.11.2, 2026.11.2.2, 2026.11.5, 2026.11.6), rendered by those tags and checked in, are upgraded to HEAD against fake hosts that implement content-hash trust. The assertions: no trust condition opens, the Codex folder's bundle hash is unchanged, both hosts capture before and after, and a rollback inside probation works. v1 has no such test; its upgrade test moves between two stand-ins built from one source.

---

## 16. Alternatives weighed

| Alternative | For | Against | Verdict |
| --- | --- | --- | --- |
| Codex hooks at user level (`~/.codex/hooks.json`) | approval covers the declaration only; no hashing on every run | writes a host settings file; the key is positional, so another tool editing the file can unapprove ours | no, unless the owner reopens the rule; it is the fallback if plugin hooks become unworkable |
| One frozen Codex plugin with the MCP declaration and no skills | one registration fewer | freezes what changed four times in eleven days | no |
| PreToolUse in the one frozen declaration from the start | one unit | a process on every edit and shell call for a feature still in shadow | only if its gate is open at the freeze |
| `mcp_tool` hook handlers to a warm server | no process start per event | since 0.164.0 such a hook waits for its server inside the hook's timeout; it fails open; it can mark a thread as tool-assisted for Codex's own memory; it makes the read-only server a write path | no; look again only if measured hook starts break the latency target |
| A Claude mod for capture or delivery | closest to built in | volatile, switchable remotely, blocked on team logins | not in v2.0 |
| A bootstrap hook that fetches the binary | install from inside the host | a hook would make a network call | no |
| Host marketplace auto-update, or a `command` source | uses the host's own machinery | updates a folder that already loads in place; does not update the binary | no |
| Evolve v1's health rows | less new code | see 13.3 | rebuild the signalling, keep the bounded readers |
| A resident daemon | simplest scheduling | ruled out; nothing here runs longer than a job | no |
| Homebrew as the update channel | familiar | needs brew; nobody upgrades daily; no rollback tied to health | maybe later as a second way to install |
| Verifying build attestations on the user's machine | reuses the release's provenance | needs a transparency-log client and a trust root far beyond the standard library | no: one ed25519 signature |
| Signed host-quirk data updated apart from the binary | fixes without a release | a second verify and rollback path, when binary-only releases are already silent | no |
| A notification helper app | proper attribution | needs a Developer ID and a bundle to maintain | no: best-effort `osascript` |
| Developer ID signing and notarization now | needed for a package installer | no current path meets Gatekeeper; a cost and a secret | when a managed rollout is wanted (HO-D8) |
| Windows | reach | 36 files of unix-only calls | not in v2 |

---

## 17. Contracts

Accepted as written: the ledger's C-H1 (events with no tool-name filter, declared once with delivery's), C-H2 (the hook entry and its fail-closed behaviour), C-H3 (machine id, the statement, the reader version in the manifest), C-H4 (hook health lines and counters). The store's S-H1 (reconcile calls `Ensure` then `Migrate`; nothing removes the store unasked), S-H2 (the tick calls `Sync` and `Maintain`), S-H3 (its conditions), S-H4 (the writer id; never a binary below `min_reader`), S-H5 (its commands; this area's wording and the thin `gh` and `glab` helper). Derive's D-H1 (the tick, and the MCP server without it), D-H2 (wake events declared with capture's; CLI path and version; the upgrade mark), D-H3 and D-H4 (its conditions, cost and status), D-H5 (the statement names model use). Deliver's V-H2 to V-H6, and V-D10. V-H1 is accepted with the change in H-V5.

| Id | With | Direction | One line |
| --- | --- | --- | --- |
| H-L1 | ledger | need | Hook health lines carry time, host, event, duration and outcome, one per run, and the per-event view is never pooled; a notice shown is one more content-free line |
| H-L2 | ledger | need | `ledger.Health()`: oldest dirty spool, sessions on the hook floor by host, last ingest, and the store roots each host's own environment named, readable without a lock |
| H-L3 | ledger | promise | The hook accepts `--event` and the v1 form; generation 0 (v1's seven events and matchers) stays a supported declaration until no fixture machine has it |
| H-L4 | ledger | promise | `health.Notice(host)` is one bounded read of a file under 1 KB; a hook computes no health and scans no host folder |
| H-D1 | derive | promise | `host.Probe(id)` gives the CLI path, version and capabilities from one shared cache; the tick calls `work.Tick()` at most every 15 minutes; a swap writes the upgrade mark with both versions |
| H-D2 | derive | need | The breaker's state and `work.Status()` are readable without the worker lock; the known-good fixture's result after a binary change is recorded where probation can read it |
| H-S1 | store | promise | `store.Migrate()` is called only after a clean probation; an upgrade or rollback checks the store's schema range first; the machine id is minted before `Ensure` |
| H-S2 | store | need | `store.Health()` is readable without the store lock; `Sync` tells "sign-in needed" from "no agent in this environment", so a sync that fails from the tick is retried from a chat's worker before anyone is asked |
| H-V1 | deliver | need | One line per chat start for the person, at most 160 characters, taken from `health.Notice`; the first-run statement once per machine in the first brief, at most 500 characters, for the person and not the model |
| H-V2 | deliver | need | The MCP server writes a start line and runs the evaluator once after 90 seconds; a pull's result may carry one line owned by this area, in a fixed place, once per server process. Deliver's draft says health text never enters its context: this is the one exception asked for, and the lead should rule on it |
| H-V3 | deliver | promise | `host.Caps(session)`: per event whether it fires, may add context, its cap and whether async is timely, narrowed by evidence; the installed generation, so deliver knows which points exist on this machine |
| H-V4 | deliver | promise | Skill text, tool text and server instructions live only in an unfrozen unit and are deliver's to write, inside byte budgets this area tests (a skill body under 2,000 bytes; instructions under 512 characters) |
| H-V5 | deliver, ledger | need | The Codex generation 1 declaration is the table of 4.3, fixed at the end of Stage 3 by HO-30 with both areas' sign-off; a row is in it only if its handler is on by default then; anything later is an additive unit |
| H-V6 | deliver | promise | `host.CommandName(env)`: `eva-md` when the link is on the chat's PATH, else the full path; text for agents names the tool first |
| H-X1 | all | need | Any file an area asks eva-md to place outside its own folders (a host skill, a rules file, if the owner ever allows one) is rendered through a unit and listed in the manifest, so uninstall removes it and any release can replace it |
| H-E1 | evaluate | need | The results store takes this area's measures by name (section 1); the fake hosts gain content-hash trust, a version switch and policy flags; a runner for the nightly canary |
| H-E2 | evaluate | promise | `status --json` has a pinned schema; the fault matrix and the first-run scorecard are libraries the harness can call; `selfcheck` is the canary's probe |

---

## 18. Where the lead's draft is wrong or over-built

1. **"A frozen hooks-only part whose bytes never change and a content part" is right for Codex only.** Claude Code binds no approval to a plugin's bytes. Splitting there adds a registration and nothing else. One plugin on Claude Code.
2. **"Never change" needs generations, or the first new event stops Codex capture for everyone.** The draft itself wants delivery at tool time and at subagent start, which are new events. Freeze late, declare only what is on, and add later events as their own small unit, so a review never interrupts capture and a binary update is never held for one.
3. **The upgrade to v2 should not touch the Codex plugin at all.** The draft implies a re-packaging at cut-over. The v1 plugin is already approved; the v2 binary can run behind it. A forced review on day one of v2 would be the old fault again.
4. **"Signed self-update with rollback" needs an order: swap, probation, then migrate.** Otherwise a rollback meets a store it cannot read. And the draft does not say who rolls back a binary that cannot start: the old process must test the new one before it exits.
5. **The observer that does not depend on the failing part is not only the tick.** The per-chat MCP server starts whether or not hooks fire, and the other host's hooks read the same file. Those two fix most of OPS-01 with no decision to make. The tick adds the cases with no chat open and with the plugin gone.
6. **Health should leave the hook.** The draft keeps a freshness ledger "per host and stage" but does not say where it is computed. Today up to 35 ms of every chat start is a folder scan for it.
7. **Reach: the MCP-only floor belongs before cloud sessions and Cowork**, and Cowork is not a separate design (11.1, 11.5).
8. **Nothing from mods in v2.0**, in this area or as a model backend. Derive's draft says the same.
9. **Local-first and question-free must still say three things once** (what is captured, what a model is sent, how to stop). An install that asks nothing and says nothing is worse on consent than v1.
10. **A cost ledger "the user can read" should not show money.** Tokens and a share of eva-md's own budget.
11. **Host tables as separately signed data** (the audit's direction, not the draft's) is over-building once binary-only releases are silent.
12. **Host-native delivery, if the owner ever allows it, is a packaging matter too.** A generated rules file or an exported skill outside eva-md's folders must be in the manifest, or uninstall leaves it behind and no later release can replace it (H-X1).

---

## 19. Owner decisions

| Id | Question | Options | Recommendation | Why | If no |
| --- | --- | --- | --- | --- | --- |
| HO-D1 (lead's D-B) | May eva-md install its own releases in the background? Reverses "no auto-update" | install; check and tell; off as v1 | Install, with the signed manifest, soak, probation, rollback, a switch, and the statement saying so | The hosts update themselves weekly and eva-md's fixes for their changes wait for a person today. The check sends nothing about the machine | `updates = "check"`: a daily check, one line after 7 days, and an upgrade command that asks nothing so an agent can run it. Cost: the installed base drifts and every fix for a host change waits for a person |
| HO-D2 (lead's D-A, derive's DV-D1) | May one OS-scheduled, short-lived tick run every 15 minutes? Reverses "no sleeping jobs" | tick; host-driven only | Tick | It organizes a chat that never ends, and it is the only observer left when a host's plugin is gone | The worker a hook starts and the MCP server carry the same steps while a chat is open. Cost: nothing runs with every chat closed, and the one-host, plugin-gone case stays silent |
| HO-D3 | Where does the release signing key live? | a secret in the approval-gated release environment; an offline key on the owner's Mac; both accepted | Both: the environment key for releases cut on GitHub, an offline key as the second embedded key for releases cut from a Mac and for rotation | A signature is what makes a background install defensible; two keys give rotation and a way back | No signature: no background install (HO-D1 falls to "check"), and upgrades stay checked against a file from the same release |
| HO-D4 | May the tick post an OS notification as the last-resort alert? | yes, rate-limited; no | Yes: high severity only, after 24 hours with no chat-borne delivery, at most one a week per condition and three in all | It is the only channel when a host's plugin is gone. On macOS the notice is attributed to the system's script runner, which the owner should see once before agreeing | No OS channel. Cost: the plugin-gone case is detected and shown only in `status` and through another host |
| HO-D5 (lead's D-N, deliver's DL-D9) | May eva-md do anything about the host's permission question for its read tool? | nothing; print the rule; write it with consent | Nothing in the host's files; one read-only tool; `status --all` prints the rule a person may add | Writing a permission rule is writing host settings, and push delivery needs none | If the owner wants it written: `eva-md grant`, which shows the one rule, writes it with a backup, records it in the manifest and removes it on uninstall. Cost: the rule "never write host settings" gains an exception |
| HO-D6 (lead's D-J) | Which surfaces does v2 reach? Reopens decision 3 and "macOS only" | Linux, the MCP-only floor, cloud after a spike; Linux only; as v1 | Linux and the floor in Stage 4; cloud sessions' read path only after the spike; Cowork verified, not built; no Chat on the web | Linux is a gate, not a design. The floor is small and reaches the desktop Chat tab. The rest rests on facts nobody has | Linux only. Cost: the desktop app's Chat tab and cloud sessions get nothing |
| HO-D7 | May the MCP server write content-free health lines and run the health evaluator? Narrows "the server writes nothing" | yes; no | Yes | It is the one process that starts with a chat whether or not hooks fire, so it is the cheapest way to notice dead hooks on a person's only host | The tick alone notices (within two ticks, and only with HO-D2). Cost: with no tick, a one-host person with unapproved hooks is never told |
| HO-D8 | Ship the kit for managed machines (printed admin snippets, a restrict-only policy file), and sign with a Developer ID? | kit only; kit and signing; neither | Kit only, in Stage 4, after the field report says whether the real install is managed | Managed hooks remove Codex's review step entirely for an organization; signing is needed only for a package installer | Document the snippets and build nothing. Cost: on a machine with managed-only hooks eva-md is off and can only say so |

---

## 20. Risks

1. **Codex changes its trust rules again.** It did on 2026-10-23. Frozen bytes may stop being enough, or the cache layout may move. The canary probes trust after a swap every night; the generation mechanism and the user-level fallback exist for this.
2. **Three facts the packaging rests on were read in source and not run** (the per-plugin scope of approval, the cached copy, repair by reinstalling). HO-01 comes before anything is built on them.
3. **A background install is a supply-chain channel.** Mitigations: an approval-gated signing job, a second offline key, monotonic versions, soak, yank, offline verification, a switch. Soak without telemetry is weak protection and is described as such.
4. **Rollback on a false regression.** A host outage in probation looks like a bad release. The rules need a signal tied to the new binary and an unchanged host version; one rollback per tag; none twice in a week.
5. **Notices that cry wolf.** The activity scan has known false alarms in v1. Two evaluations before opening, relevance, and the healthy-month replay as a release gate.
6. **The tick on a managed machine**, and the one-time system notice it causes. The fallback is not optional code, so this degrades and does not break.
7. **Version skew inside one chat** after a swap: a new hook binary and an old MCP server. The N and N-1 rule and its test.
8. **Generation 0 lingers.** Codex users who never accept generation 1 keep v1's event set and v1's skill text. The ledger's record reader covers part of the capture gap; `status` says what generation 1 would add, once.
9. **Two Claude Code versions on one machine.** The desktop app's bundled copy may reject a plugin field the CLI accepts. The spec carries ranges; validation runs against the CLI found.
10. **Policy can switch the product off.** Managed-only hooks leave nothing but the admin kit.
11. **Reach consumes effort before the core is proven.** Stage 4 work is gated on Stage 3's measures; the cloud work starts with a spike that can end it.
12. **The OS notification's attribution and behaviour differ across macOS versions.** It is best effort and decided by the owner after seeing one.
13. **More Codex events mean more processes per turn.** Ten declarations, two of them async. The hook benchmark gains a Codex-shaped run through a shell.
14. **The release pipeline gains a secret.** A leaked key signs a manifest; it still cannot move a tag or publish a release without the repository.

---

## 21. Work packages

Sizes: S up to two days, M up to a week, L more. Every package ends with `scripts/linux-check.sh` green and the docs changed in the same change. Tests use fake hosts, a fake scheduler and an injected clock; none depends on machine speed.

### Stage 0: ground truth

| Id | Title and outcome | Scope | Depends on | Acceptance | Size |
| --- | --- | --- | --- | --- | --- |
| HO-01 | Host fact sheet for packaging, trust and scheduling: every row of section 14 answered on real hosts | `scripts/host-facts.sh` (preview by default; `--apply` uses a scratch plugin and marketplace, never eva-md's own); a fact sheet under `docs/v2/`. Out: any change to eva-md's registration | the owner's go-ahead to run it | Each of the 14 rows has yes, no or "could not tell", the host version and the date; each refuted row names the fallback it switches on. Measure: facts answered, 14 of 14; `hook_start_ms` p50 and p95 on Codex | S |
| HO-02 | Operations baseline from the one real install, read-only: how often v1's health, jobs and approvals behaved as designed | a content-free report mode on v1's doctor: days the daily job ran in the last 14 (from the attempt markers), commit and push outcomes by class, the trust flag's age, failures per event, notices by kind. Out: any repair | none | On a fixture state folder the report prints exact counts and a test finds no fixture text in it; on the real install it answers field questions 2 to 7. Measure: baselines for `notices_per_day`, `days_without_maintenance`, `codex_reapprovals` | S |

### Stage 1: foundations

| Id | Title and outcome | Scope | Depends on | Acceptance | Size |
| --- | --- | --- | --- | --- | --- |
| HO-10 | The host adapter, spec and probe: one place that knows a host | `internal/host` (registry, spec table, probe and cache, `generic`); move `install`, `health` and `cmd` behind it; one host id per host. Out: `hook`, `catchup`, `update`, `resume`, `mcp`, which move with their own rebuilds | none | `go test ./internal/host`; a lint test counts host literals outside `internal/host` in the three packages and finds 0, and prints the count for the rest; fake hosts at three versions give the right spec row; a version above the tested maximum opens `host.version_untested` and uses the newest row; a quirk without a fixture fails the test. Measure: `host_literal_lines` in this area's packages, from the baseline to 0 | M |
| HO-11 | The install manifest and the unit model: any release can replace another's files | `internal/install` (manifest read and write, units, v1's path list, `reconcile --dry-run`); remove `install-record.json` and `cli-paths.json` readers. Out: new units' contents | HO-10 | Tests: a release that removes or renames a rendered file installs over the one before; a rollback to a release that lacks a file works; a foreign file still stops both install and uninstall; a v1 folder with no manifest is adopted; `go test ./internal/install -run TestManifest`. Measure: layout changes that install cleanly, 0 of 2 today (no test exists) to all | M |
| HO-12 | Health core on today's evidence: conditions, freshness, one selected line, a verdict | `internal/health` (evidence interface with a v1 backend over heartbeats, the run log and commit records; evaluator; `conditions.json`, `freshness.json`, `notice.json`; relevance, decay, quiet); `status` verdict and `--json`; the hook reads `health.Notice`. Out: OS notifications, MCP lines, v2 evidence | HO-10 | `go test ./internal/health -run TestFaultMatrix` passes rows 2, 3, 6, 8, 9, 11, 14 and 15 of 7.7 on v1 evidence; the healthy-month replay gives 0 notices; no string "run eva-md doctor" remains (a wording test); `status` prints at most 6 lines (golden); the chat-start benchmark's health part is under 1 ms. Measures: `time_to_detect` per row, `false_notices_30d` = 0, `status_lines` | M |
| HO-13 | Signed release manifests: every release carries a manifest a binary can verify offline | `internal/release` (manifest type, signature envelope parser, verify with `crypto/ed25519`, embedded keys); the release workflow, `scripts/release.sh` and `scripts/install.sh`; `docs/releasing.md`. Out: background checks and installs | HO-D3 | The release stand-in test builds, signs with a test key and verifies; a flipped byte in the manifest, the signature or an asset is refused before the binary runs; an older, yanked or too-far tag is refused; test vectors made by `ssh-keygen` verify in Go and the reverse; the workflow tests accept the new job and asset count. Measure: releases verifiable offline, 0 to all | M |
| HO-14 | Fake hosts that can fail the way real ones do | the test fakes: content-hash trust and a cache copy for Codex, a version switch, policy flags, a fake scheduler and notifier | none | Each new behaviour has a self-test; v1's install tests pass unchanged on the new fakes | S |

### Stage 2: core rebuilds

| Id | Title and outcome | Scope | Depends on | Acceptance | Size |
| --- | --- | --- | --- | --- | --- |
| HO-20 | Codex packaging in units and generations: a release never costs a review | `internal/install` and `internal/host/codex` (hooks unit, tools unit, `render(unit, generation)`, the bundle-hash helper and golden lines, the probe for a generation's events, automatic reinstall); generation 0 kept as rendered by v1. Out: generation 1's contents (HO-30) | HO-01, HO-11, HO-14 | Tests: the bundle hash of each generation at HEAD equals its line; changing any skill or MCP byte changes only the tools unit; on the fake Codex a binary swap and a tools change leave every hook trusted, and a stray file in the cache is repaired with no review; a property test finds no command that changes a unit folder outside render. Measure: `codex_reapprovals_per_release` = 0 | M |
| HO-21 | The first run: `reconcile`, the statement, preferences, and the end of the guided setup | `cmd/eva-md` (setup as reconcile, the statement, `exclude`, `off`, `on`, `pause`, `hosts`), `internal/config` (per-key validation, `prefs.json`, unions), `internal/remotehelp` carved from the setup files; remove `config.env` and the dialogue. Out: the store's own remote commands | HO-11, the store's local-first creation (ST-05) | The scorecard on fake hosts: questions 0, approvals 0, lines at most 12, inventories at most 2 a host, no network call; with no terminal it exits 0; the statement is a golden; a wrong value in a non-privacy key no longer stops capture and a syntax or exclusion error does, both raising `config.invalid`; after `eva-md exclude .` a hook in that folder stores nothing. Measures: `first_run_questions`, `first_run_approvals`, `first_run_lines`, `first_run_host_calls`, `install_wall_s` | M |
| HO-22 | The tick and its fallbacks | `cmd/eva-md/tick.go`, `internal/platform` (launchd agent install and removal, the fake's interface), the MCP server's 10-minute detached start, the tick heartbeat | HO-D2 for the agent; derive's worker (DV-12) | A tick with nothing due makes no network call and no model call (test seams) and exits; uninstall removes the agent; with the agent refused, the fallback keeps `freshness.json` no older than 10 minutes while a fake chat is open; each step's failure leaves the later steps running; time limits are tested with blocking fakes. Measure: `tick_gap_p95_min` | M |
| HO-23 | Health on v2 evidence, repairs and channels: the whole fault matrix | `internal/health` (the ledger's lines, MCP start lines, the other areas' conditions, repair classes, the pull-result line, OS notifications), `eva-md repair` | HO-12, HO-22, the ledger's health lines (LG-20), HO-D4, HO-D7 | All 15 rows of 7.7 pass; a repair never re-adds a plugin the fake host reports as removed by the user; each ask is one sentence under 160 characters (golden); the evaluator runs under 100 ms on a year of fixture evidence. Measures: `time_to_detect` for every row; `false_notices_30d` = 0 | M |
| HO-24 | The cost ledger | `internal/health/cost.go`, `status --cost` | derive's journal (DV-13), delivery records | A fixture journal and delivery log give a golden rollup; totals equal the sums of their sources; cached input is apart; a schema test finds no free-text field. Measure: `cost_rollup_error` = 0 | S |
| HO-25 | Background upgrades: check, staged swap, probation, rollback | `internal/release`, `cmd/eva-md/upgrade.go`, the manifest's binary section, `selfcheck`, the `updates` preference | HO-D1, HO-13, HO-11, HO-12, HO-22 | Against the fake release host: a binary-only release makes no host CLI call and leaves every bundle hash; a content release registers only the changed unit; a crash injected at each step leaves a verified, runnable binary at the stable path; a binary that fails `selfcheck --installed` is put back by the old process; an injected SessionStart failure in probation rolls back once and pins the tag; a rollback across a raised `min_reader` is refused; an old MCP server keeps answering after a swap; `upgrade` asks nothing. Measures: `rollback_correct` 100% in the suite; `update_lag_h` | L |
| HO-26 | Uninstall and the command surface | `cmd/eva-md` (usage line, aliases, verdict-first output, `--json`), `internal/install/uninstall.go` (manifest-driven; the tick; the slots; memory kept) | HO-11, HO-22 | `uninstall --apply` leaves the store and the ledger byte for byte; `--all` without `--delete-memory` does too; every command's first line is a verdict (a table-driven test); the usage line lists the person's verbs only | S |
| HO-27 | The move from v1, in order, with no review | `reconcile` on a v1 install (adopt, generation 0 untouched, config split, old state cleanup, migrations after probation) | HO-20, HO-21, HO-25, the ledger's import (LG-25), the store's migration (ST-07) | Fixtures of each published v1 release's on-disk state upgrade to HEAD: no trust condition opens, the Codex bundle hash is unchanged, both fake hosts capture before and after, a rollback inside probation works, the wiki and inbox folders are byte for byte the same until the data migrations run | M |

### Stage 3: memory quality

| Id | Title and outcome | Scope | Depends on | Acceptance | Size |
| --- | --- | --- | --- | --- | --- |
| HO-30 | Freeze Codex generation 1 from measured data | the declaration of 4.3 with the ledger's and deliver's sign-off; its golden line; `upgrade --codex-hooks`; the text of what it asks | HO-01, HO-20, DL-14's gate result, LG-03's fixtures | A decision record names each row and the measurement behind it; the golden line is committed; on the real install, run by the owner: one review after the command, and none after the next two releases. Measure: `codex_reapprovals_per_release` = 0 on the real install | S |
| HO-31 | The first-run scorecard and this area's measures in the results store | a scorecard library and its CI job on macOS and Linux with fake hosts; the measures of section 1 written in the store's format | HO-21, the results store | CI fails when the happy path asks a question or prints more than 12 lines; each measure of section 1 has a baseline and a current value | S |

### Stage 4: reach and sync

| Id | Title and outcome | Scope | Depends on | Acceptance | Size |
| --- | --- | --- | --- | --- | --- |
| HO-40 | Linux | release assets, `platform.Supported()`, the systemd timer, `notify-send`, the install and uninstall scripts | HO-22, HO-25, HO-D6 | The release stand-in test passes on Linux; the scorecard matches macOS; a container without systemd runs on the fallback with `tick: unavailable` in `status` and no notice | M |
| HO-41 | The MCP-only floor | `internal/host/generic`, `eva-md connect`, the desktop extension bundle where fact 12 allows | deliver's hook-free floor (DL-22), HO-D6 | A generic client lists one tool and gets the brief with one call; `connect` writes nothing (a test hashes the home); the bundle names only the stable binary. Measure: none claimed; uses are counted in the health lines | S |
| HO-42 | Cloud sessions: the spike, then the read path if it passes | the spike's fact sheet; then the portable plugin asset, the cloud mode of the hook guard, the worker's clone | the store's sync (ST-09), HO-40, deliver's owed brief | The six questions of 11.4 are answered. If they pass: in a container made like a cloud session (the variable set, an empty home, the plugin folder, a local bare remote) the first prompt gets a brief from the clone, nothing is captured, and no hook opens a socket (run with networking off) | S, then M |
| HO-43 | Managed machines | `policy.blocked`, `eva-md admin print`, the restrict-only policy file, `setup --managed-hooks` | HO-20, HO-23, HO-D8, the field report | Fake hosts with each policy flag open the condition and no repair runs; the policy file's `updates = "off"` wins over a preference and cannot turn anything on; with managed hooks the hooks unit is absent and nothing is captured twice | S |
| HO-44 | Cowork, verified | a fact sheet | HO-42 | The same six questions answered for Cowork, and a written recommendation; nothing built | S |

### Stage 5: proof

| Id | Title and outcome | Scope | Depends on | Acceptance | Size |
| --- | --- | --- | --- | --- | --- |
| HO-50 | A nightly canary on real host CLIs | a workflow on throwaway macOS and Linux runners: newest `claude` and `codex`, install, `status --json`, the probes, an upgrade from the previous published release, a swap, an uninstall. No sign-in and no model call in this tier | HO-25, HO-27, the evaluation area's runner | The job goes red on a seeded host change (a fake CLI that renames a field) and on a seeded trust change; results carry both host versions. Measure: host releases checked within a day | M |
| HO-51 | The release gate for operations | a checklist the release plan reads | all above | The fault matrix and the replay pass; the scorecard is inside its budgets; the real install shows no forced review across three releases and a week with zero commands typed for eva-md, or the plan records which did not hold and why | S |

---

## 22. What a field report from a real install should answer for this area

In general terms, with counts and names of kinds only, never content:

1. Versions: eva-md, each host CLI, the desktop apps and the CLI versions they carry, the OS.
2. Codex approval: whether the hooks are approved now; how many times a review was needed since install and after which upgrades; whether an approval given in the CLI also holds in the app.
3. Where Codex keeps its installed copy of the plugin, and whether that folder holds any file eva-md did not render (a count).
4. Notices: which kinds were seen, how often, in which host; any that never went away; any that were wrong, such as a Codex review notice in Claude Code chats on a machine where Codex is not used.
5. Hook timing per event from the doctor; and, for Codex, the wall time of one hook run through its shell, over 50 runs.
6. The doctor: counts of passed, failed and unknown rows, and the generic names of those not passed.
7. Background jobs: on how many of the last 14 days the daily job ran; commit and push outcomes by class; days with no record at all; the longest wait between a chat ending and its update.
8. Steps the owner took by hand since install: commands typed for eva-md, approvals given (signing, SSH, hook review, tool permission), and roughly how often each.
9. Whether `eva-md` is found by name in a chat's shell and in a terminal.
10. Managed settings: whether a device profile is present; whether a managed settings or requirements file exists for either host (yes or no); whether user launch agents are present and allowed.
11. Which surfaces are used (CLI, the desktop Code tab, the ChatGPT app's Codex mode, an IDE) and the entrypoint values seen.
12. The MCP server: connected in each host or not; permission questions met per week; whether Codex shows server instructions.
13. Model cost: calls per day and tokens per call by host from the run log, the model and effort asked for and used, any hold for sign-in or rate limit.
14. Disk: sizes of the inbox, the wiki and the logs, and growth per week.
15. Upgrades so far: how each was done, how long it took, what was asked, what broke.
16. Network: whether release downloads work from a background job; whether a push works from a job with no SSH agent as well as from one a chat started.
17. The environment of a hook process, by variable name only: whether HOME is set, which shell runs it, whether the SSH agent's socket is present in a job a hook starts.
18. Which prerequisites were missing at first install, and how long the first install took.
