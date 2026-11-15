# TODO: the environment for the live evaluation

Written on 2026-11-14 for an agent that has never worked on this, revised the same day after three independent reviews, and four times more on 2026-11-15 after seven more readers, who ran its commands in stand-ins; the last four found no fault that would leak a key. It covers one piece of work: giving v2's two live evaluation runs, the pilot and the contract spike, a place to run. Everything else that is open in the build is in the lead's guide, [docs/v2/leading-the-build.md](docs/v2/leading-the-build.md); section 10 below lists it in a few lines.

The owner wants this done. The first attempt to create the environment was declined only so that this plan would be written down first. The session that was declined does not propose it again; whoever takes this document up proposes it once, as written here.

Lines marked **(a guess)** are unproven until the step named beside them shows them true. None of them costs more than a smoke run's few sessions to settle.

## 1. The issue

Two rows of the plan's Stage 0 end in a live run, which is a run that makes real model calls and costs money:

- **The pilot** (plan row EV-03): ten small coding tasks, run by each host's CLI (Claude Code and Codex) under three conditions (memory off, an ideal record, eva-md v1), three trials each: 180 agent sessions. It gives the first real figures of what a session costs and whether the tasks can show a difference. Its procedure is [docs/v2/pilot-runbook.md](docs/v2/pilot-runbook.md), called the run-book below.
- **The contract spike** (plan row DV-03): at most 1,344 model calls through v1's update runner (672 first calls, and at most as many repair calls), which compare two forms of the update's contract over three sizes of model. Its plan is [eval/spike/run-plan.md](eval/spike/run-plan.md) and its card [docs/v2/cards/DV-03d.md](docs/v2/cards/DV-03d.md).

All the code for both is merged and checked offline. Neither has run, because they have no place to run. The place must meet all of these at once, and nothing that exists today does:

1. **The runs are billed to two API keys made for the evaluation alone**, one for each vendor (plan decision 28). A key's value must never be in a chat message, a file of the repository, a log, a commit or an agent's context.
2. **The keys must not be on the owner's Default cloud environment.** A secret saved on an environment is sent by every session that runs on it, and Default is where every other session of the owner's runs.
3. **While evaluation chats run, nothing in the container may reach a host that serves this repository's files.** The pilot's tasks have hidden tests, and they are in this public repository (`eval/pilot/testdata/`). GitHub serves them, and so does Go's module proxy.
4. **The build's own session cannot be that container.** It needs GitHub all the time, for pushes and CI.
5. **No copy of this repository may be readable by the user the chats run as.** A session with the repository attached has it at `/home/user/eva-md`, readable by every user of the machine.
6. **The owner must see what each key spends**, and something outside eva-md's own counting must bound it.
7. **Results must outlive the container**, without entering this repository: their rows name the model, which the repository never does.
8. **The leave to spend must reach the agent that spends in the owner's own words.** An agent does not take another agent's message as the owner's consent, and should not.

## 2. What is already decided

The owner said on 2026-11-14 to take the lead's recommendation on everything that waited on them. Each answer is in [docs/v2/plan.md](docs/v2/plan.md), section 7, "Where the decisions stand", and a word from the owner changes it:

| Question | Answer |
| --- | --- |
| Leave for the pilot | Yes, inside 240 sessions and 120,000,000 tokens by the driver's count, a tenth of it held back for reruns |
| Leave for the spike | Yes, inside its row's allowance: about 1,900 calls and 25,000,000 tokens |
| The keys | One for each vendor, for the evaluation only, each bounded at 200 US dollars at its provider |
| The model | Each host's own default for an API key, at the one version of its CLI that is installed for the pilot. It is read from the first smoke run and then named on every command line, never in the repository. The spike's three tiers are that vendor's small, middle and large models of the day |
| The keys afterwards | Kept for the spike and for EV-17, each under its bound, and revoked at the Stage 0 readout or at the owner's word. The environment's copies are removed when the evaluation ends (step 10) |
| What it costs | A rough estimate only: 60 to 160 dollars for the pilot, 30 to 100 for the spike. The smoke runs price a session on each host before any full run starts |

The plan words that leave as taking effect on a host once the container is settled and that host's key is in. Step 3 below adds the owner's own word in the session that spends (point 8 above).

## 3. The proposed solution, in short

One cloud environment of the owner's account, named `eva-md-eval`, used by one session that does nothing but the evaluation.

- **The keys are the environment's network secrets.** The owner types each into a secure form. The environment's proxy adds it to requests to that vendor's API host. No value reaches the container, the agent or a trial's home: inside the container each key's variable holds a made-up stand-in.
- **The network has two phases.** For set-up it reaches github.com and the package registries. Before the first chat it is narrowed to the two API hosts, the user the chats run as is bound to the proxy by the machine's own firewall, and both are proven from inside, through the proxy and around it.
- **The evaluation's session has no repository attached.** It clones this public repository once during set-up, into a folder only root can read.
- **The owner starts that session themselves**, with a short message that gives the leave in their own words.
- **The build's session is untouched.** It stays on Default with its own network.
- **Each key is bounded at its provider**, and the owner reads its usage in that provider's console.
- **At most**, the two keys can be billed 200 dollars each, 400 in all, or that much in each month where a provider's limit starts anew monthly (step 5). The rough estimate for both runs is 90 to 260 dollars. The sessions themselves run on the owner's own plan besides.
- **After each run, its results are handed to the owner as a file** and nowhere else.

**What the owner does, at the least:**

1. Approves one prompt that creates the environment (step 1).
2. Starts one new session on `eva-md-eval`, with no repository, and pastes the message of step 3.
3. Makes the two keys, each in a workspace or project of its own that is bounded at 200 dollars (step 5 says how a provider bounds one), turns the API's own web search and web fetch off there where the console allows it, and types each key into a form that session shows.
4. Approves one prompt that narrows the network (step 6).
5. Answers that session's questions: what each key's console shows an hour after it is saved; after each smoke run and each full run, that key's usage over the run's hours (tokens or cost, whether any line is a web search or fetch, and whether any file is stored there); that each bound refuses requests; and prices or model ids if it asks for them.
6. Keeps the result files that session hands over (step 8).
7. At the end, removes the two secrets from the environment, or the environment itself, and archives that session (step 10).
8. Passes between that session and the build's lead what one needs from the other: a correction's patch, the last report.

The owner should also expect: that session asks before running commands, unless the owner chooses a mode in which it does not; it and any session that helps it run on the owner's own plan for a few days, besides what the keys are billed; and every result file names the model.

**What this does not close**, said plainly:

- **A vendor's API can itself search or fetch the web.** A chat has a shell, and the proxy adds the key to any request the container sends to an API host. So a chat that set out to could ask the API to read this repository. The run-book turns each host's own web tools off and stops a run when a chat uses one (`web_used`). A chat that called the API by hand, or started a second CLI without those flags, would be caught only if it wrote a hidden test whole into its own folder (`hidden_found`); one it read and reworded would not be. So the owner turns the API's own web search and fetch off for each evaluation workspace or project where the console allows it, and after each run looks at that key's usage for any line of a web tool. One such line means that run's results are not used: the owner tells the evaluation agent, which leaves that run's folder out of the report, starts no further run, and asks the owner before running its tasks again from the reserve. Each API can also reach the web through a remote MCP server named in a request, or a file given by its address; the console may show neither, and only `hidden_found` would catch what came back whole. The tasks ask for nothing of the kind, so this is a limit to know, not one that is expected to bite.
- **Anything in the container can spend through the proxy**, a chat's own calls included, and the driver counts only what the host's CLI reports. The bound at the provider is the one bound no fault here can pass.
- **Grading runs a chat's code as root** (the run-book's step 3, "What it cannot protect against"; EV-17e moves it to another user). In a cloud container root reaches more than the hidden tests and the clone: this session's own tokens, which are in files only root reads and in the environment of the session's own processes. Code a chat wrote, run at grading, could copy one of them, or a hidden test, to where that chat's next round reads it, or send it to an API host through the proxy. Each run is started from an empty environment (step 4, "Starting a long run"), which keeps those tokens out of the driver's own environment but not out of root's reach, and step 6's firewall binds `pilot-chat`, not root. Under the `eva` condition root also acts on what a chat leaves, with no test involved: after each chat it runs eva-md's hooks in the chat's workspace and v1's update, and through them git and the host's CLI, with the trial's home as `HOME` (`internal/eval/continuation/v1arm`). The chat owns that home, so what it writes there can become a command root runs: a helper or a hook in Claude Code's settings (the sign-in program relies on the helper being run), Codex's configuration, git's. Moving grading to another user does not close that part. The tasks ask for nothing of the kind, and their tests run for seconds. The owner accepts this limit or does not start the pilot, and archives the evaluation's session when it is over (step 10), so that its tokens end with it.
- **A vendor's API can also keep data between calls.** With the key the proxy adds, a chat can store a file at the vendor, and a later chat, of another trial or another condition, can list it and read it: that would carry memory where the run says there is none, and nothing in the container sees it. So after each run the owner also looks, in that key's workspace or project, for any stored file, where the console shows them; one there means that run's results are not used, as with a web line. Where the console lists no stored files, nothing checks this; and a file one chat stored and a later chat deleted is not seen even where it does.
- **The firewall binds IP alone, and only what listens when it is set.** A chat can open sockets of other families that no rule sees, such as the virtual machine's own channel to its host, which any user could open in a container of this kind; what answers there is not known and was not tried. Should something there lead off the machine, a chat could fetch the hidden tests that way, and only `hidden_found` would catch one copied whole. A port root opens on the machine while a run goes (grading's code, v1's update, a tool of this session) is open to `pilot-chat` until `bind.sh` is run again, which is before every run.
- **What a chat can read of the machine.** In a container of this kind any user can read the environment manager's logs under `/tmp` (the session's id and address), the proxy's status page (the hosts it refused lately) and every process's command line (a run's `--model`). None holds a key or a token.

**Alternatives looked at and not taken:**

- *The keys on Default.* Every session would send them (point 2).
- *The build's session as the container.* It needs GitHub (points 3 and 4).
- *The owner's Mac, in a container.* It needs the Mac awake and the owner to start every run. It stays decision 28's fallback for what cannot run on a key alone.
- *A session started by another agent.* The agent that spends would hold the owner's leave only at second hand (point 8), and would have no sure way to report back.

## 4. Who does what

- **The owner**: the things listed in section 3, under "What the owner does, at the least".
- **The setting-up agent**: whoever picks up this document, in a cloud session with this repository attached, on Default. It does steps 1 and 2 and, at the end, step 10's part in the repository. If it is not the build's lead, it commits nothing to `main` on its own: it hands its changes to the lead or acts on the owner's word (`AGENTS.md`, "Don't commit or push unless asked").
- **The evaluation agent**: the session the owner starts in step 3. It does steps 4 to 9 and its part of step 10. It has no repository attached and cannot push.

**Where the run-book or a card says "the lead"**, read it so: the evaluation agent does everything the lead does inside the container. That covers running the commands, reading a trace through the shape reader, writing the web probe's two commands, and mending the sign-in program, which is a file of the container and not of the repository. Every ruling that changes a ceiling is the owner's. Every change to the repository (a correction to `hosts.go`, the host facts, the plan) is the build's lead's, from what the evaluation agent reports.

**If the agent has none of the tools named below**, which are a cloud session's own environment tools, the owner can do steps 1, 5 and 6 by hand in the app's settings for environments, with the same values.

## 5. Step by step

### Before step 1: read

1. [AGENTS.md](AGENTS.md), above all its rules on the real machine, on secrets and on live runs.
2. [docs/v2/pilot-runbook.md](docs/v2/pilot-runbook.md), whole. It is the procedure; this document gives it a place to run and says what a cloud container changes. Its last section holds the lead's rulings and the owner's answers.
3. [docs/v2/plan.md](docs/v2/plan.md), section 7, "Where the decisions stand", and decisions 16 and 28 in the table above it.
4. For the spike: [eval/spike/run-plan.md](eval/spike/run-plan.md) and [docs/v2/cards/DV-03d.md](docs/v2/cards/DV-03d.md).

The evaluation agent reads the same list before step 4, from its clone under `/srv/pilot/eva-md`: its session has no repository attached, so it is given none of these by itself, `AGENTS.md`'s rules among them.

### Step 1: create the environment (the setting-up agent)

Look at the current environment first (`get_environment`), as that tool asks. Then tell the owner in one sentence what is about to be created and why, and call `create_environment` with:

| Field | Value | Why |
| --- | --- | --- |
| `name` | `eva-md-eval` | |
| `scope` | `personal` | Only the owner's sessions use it |
| `reason` | one plain sentence, such as "A separate environment for eva-md's live evaluation, so its two keys stay off Default" | The tool requires it |
| `allowed_hosts` | `["github.com"]` | Set-up clones this repository. The two API hosts are not listed: saving a key for a host allows that host |
| `allow_package_managers` | `true` | Set-up downloads Go modules and installs each host's CLI |
| `allow_mcp_servers` | `false` | The evaluation needs no connector, and a connector's host is one more way out for a chat. If the session's own tools fail without it **(a guess that they do not)**, the owner does steps 5 and 6 in the settings instead |
| `switch_session` | `none` | The setting-up agent's own session stays on Default |
| `repositories` | left out | Sessions that build eva-md must not be steered onto this environment |
| `setup_script`, variables | none | Set-up is done by hand, by the run-book |

The call is itself the request: the owner sees a prompt with these values and approves it. If they decline, stop and ask what they want changed; do not call it again unasked.

### Step 2: record it, and hand the owner their message (the setting-up agent)

1. In [docs/v2/plan.md](docs/v2/plan.md), section 7, point (c) says the container is not settled and that no live run starts and no key is asked for until it is. Change it to say that the environment `eva-md-eval` was created on that day as this document describes, and that the container is settled only once this document's step 6 has passed, with no live run before: the plan's point (a) makes the leave take effect once the container is settled. The evaluation agent reads the plan from its own clone, and must not find it saying the opposite of what it is asked to do. The lead's guide says the same in its bullet "With the owner" and in its list "What is the owner's alone": change both, the second to say that no live evaluation runs until this document's step 6 has passed. Before the push, run the tests that read those documents: `go test ./cmd/eva-md -run 'Design|Agents|Usage|Verification|Wording|Document'`, `go test ./internal/store/profile -run TestSpec`, `go test ./internal/spec` and `go test ./internal/cardcheck`. Commit and push as section 4 says.
2. Only once that change is on `main` (`git fetch origin main && git log origin/main -1` shows it), give the owner the message of step 3, and tell them to start a new session on the environment `eva-md-eval`, with no repository, and to paste it there.

### Step 3: the owner starts the evaluation's session

A new session, on `eva-md-eval`, with **no repository** (point 5 of section 1), whose first message is the owner's own:

> I am the owner of the repository github.com/heyNag/eva-md. As root, and nowhere else on this machine, run `install -d -m 0711 /srv/pilot && git clone https://github.com/heyNag/eva-md /srv/pilot/eva-md && chmod 700 /srv/pilot/eva-md`. Then read `/srv/pilot/eva-md/TODO.md` whole, and what its "Before step 1: read" lists, from that clone. Carry out its steps 4 to 9 and its part of step 10 as the evaluation agent, and report its section 9 to me. I give leave for the live runs it describes, the pilot and the contract spike, inside the ceilings of its section 2 and no further. Report to me here at the end of each step, and whenever something is not as written.

From here the evaluation agent reports to the owner in that session. If the owner wants the build's lead to follow along, they say so there and give that session's id.

### Step 4: set up the container, at no cost (the evaluation agent)

Follow the run-book's steps 3, 4 and 5: the user `pilot-chat`, the folders under `/srv/pilot`, the clone, each host's CLI, the wrapper `as-chat`, the sign-in program, the shape reader (the three scripts' text is in the run-book's step 3), the two builds, the export and its checks. Steps 4 and 5 of the run-book each have an **Expect** line; step 3's own check is that `cat /proc/sys/fs/protected_hardlinks` prints `1`. Go on only when each matches. The exports of the run-book's step 5 run every hidden test and take minutes: start them as a long run is started (below).

One difference from the run-book's last section: GitHub stays reachable until step 6 here, and the two API hosts are reachable from step 5. No chat runs before step 6.

The run-book was written for a person at one shell in a container of their own. What a cloud session changes:

- **Every command of an agent starts a new shell.** Nothing set in one command is there in the next. Keep the settings in one file that only root can read, and start every command with `. /srv/pilot/env.sh`. Make the file first, with `install -m 600 /dev/null /srv/pilot/env.sh`, then write it:

  ```
  # /srv/pilot/env.sh, mode 600: paths and stand-ins only, never a real key
  P=/srv/pilot
  export DISABLE_AUTOUPDATER=1
  [ -r /root/.ccr/ca-bundle.crt ] || echo "env.sh: /root/.ccr/ca-bundle.crt is missing: stop and read /root/.ccr/README.md" >&2
  # the proxy's certificate, where pilot-chat can read it: copied only when it changed, and put in place in one move
  cmp -s /root/.ccr/ca-bundle.crt $P/ca/ca-bundle.crt || { install -D -m 644 /root/.ccr/ca-bundle.crt $P/ca/new.crt && mv -f $P/ca/new.crt $P/ca/ca-bundle.crt; }
  export SSL_CERT_FILE=$P/ca/ca-bundle.crt NODE_EXTRA_CA_CERTS=$P/ca/ca-bundle.crt CODEX_CA_CERTIFICATE=$P/ca/ca-bundle.crt
  export ANTHROPIC_API_KEY=stand-in-not-a-key CODEX_API_KEY=stand-in-not-a-key
  PROXYPASS="--pass-env HTTPS_PROXY --pass-env SSL_CERT_FILE --pass-env NODE_EXTRA_CA_CERTS --pass-env CODEX_CA_CERTIFICATE"
  NODEDIR=$(dirname "$(command -v node || echo /nonexistent/node)")   # node's folder; /nonexistent, never ".", when there is none
  # listeners: the TCP ports something on this machine listens on
  listeners() { awk 'FNR>1 && $4=="0A" {n=split($2,a,":"); print a[n]}' /proc/net/tcp /proc/net/tcp6 2>/dev/null | sort -u | while read -r h; do echo $((0x$h)); done; }
  # as_chat COMMAND...: runs it as pilot-chat, from an empty environment that names only what it needs
  as_chat() { env -i PATH="$NODEDIR:/usr/bin:/bin" HOME=/nonexistent HTTPS_PROXY="$HTTPS_PROXY" SSL_CERT_FILE=$P/ca/ca-bundle.crt CURL_CA_BUNDLE=$P/ca/ca-bundle.crt NODE_EXTRA_CA_CERTS=$P/ca/ca-bundle.crt CODEX_CA_CERTIFICATE=$P/ca/ca-bundle.crt setpriv --reuid=pilot-chat --regid=pilot-chat --init-groups --no-new-privs -- "$@"; }
  ```

  `HTTPS_PROXY` is not stored: it is the session's own, read anew by each command, since the proxy's port may change at a restart. `$P/ca` is mode 755. The certificate is copied only when it has changed, and moved into place whole, because every command reads this file, also while a run is going, and a chat that starts must never find the certificate half written. Add `$PROXYPASS` to every run command, after the run-book's own `--pass-env` flags (its step 2 says the same in its last line).
- **The proxy.** All HTTPS leaves through a local proxy named by `HTTPS_PROXY`, which signs TLS anew; `/root/.ccr/README.md` in the container explains it, and its status page names a host it refused. The certificate bundle is under `/root`, which `pilot-chat` cannot read and which the driver refuses as a value: hence the copy above.
- **The stand-ins.** Each key's variable holds a made-up value that no one could take for a key. The proxy puts the real key in place of what the CLI sends. If a CLI will not start on that form **(a guess that it will)**, use the vendor's key prefix followed by the same words.
- **Never hand root's environment to another user.** `setpriv` keeps the caller's whole environment, and root's holds the session's own tokens. Every command run as `pilot-chat` outside the driver goes through `as_chat`, which `env.sh` defines: it empties the environment first and only then changes user, and names what the command needs, the folder of `node` among it, since each CLI is a script of node's. As the run-book's wrapper does, it also sets `no_new_privs`, so that no program that runs as its owner (`su`, `sudo` and `mount` are on such an image) lifts a process of the web probe out of `pilot-chat`, and so out of `pkill -u` and of the firewall, which both go by the user a process runs as. The order matters: a process of `pilot-chat` can read the environment of every other process of `pilot-chat`, and a chat may have left one running, so no process of that user may hold root's environment even for an instant (tried with a stand-in user: with `setpriv` first, a watching process read root's environment at every call; with `env -i` first, never). So the run-book's own lines that start with `setpriv` are never run as written: they go through `as_chat`. After any chat has run, the web probe's first command among them, run `pkill -KILL -u pilot-chat` before the next `as_chat`. The driver itself gives a trial only `HOME`, `PATH`, `TMPDIR`, `GIT_CONFIG_NOSYSTEM` and what `--pass-env` names, so its chats are given nothing of root's; this rule is for the checks of step 6 and the web probe of step 7.
- **The clone and the commit.** The owner's message has made the clone, so skip the run-book's `git clone` line. Make sure no other copy of the repository is on the machine: `find / -xdev -name task.json -path '*eval/pilot*' -not -path '/srv/pilot/*' 2>/dev/null` prints nothing. Build `origin/main` as it is that day, and write its first 12 characters down as `<commit>` for the run-book's step 4 and for every report.
- **The CLIs.** The wrapper expects exactly `/opt/pilot/hosts/claude/bin/claude` and `/opt/pilot/hosts/codex/bin/codex`. With npm, which both hosts offer **(a guess: these are the packages each host's own install page names, and the container cannot open those pages)**: `npm view @anthropic-ai/claude-code version` and `npm view @openai/codex version` name the newest; then `npm install -g --prefix /opt/pilot/hosts/claude @anthropic-ai/claude-code@<version>` and `npm install -g --prefix /opt/pilot/hosts/codex @openai/codex@<version>`. Take the newest version of each that day, write both down, and never update them: one version for the whole pilot. Then `ls -l /opt/pilot/hosts/*/bin/` and each CLI's `--version`, run through `as_chat`.
- **Everything that needs the network happens now.** The builds may fetch Go's toolchain and modules. After step 6 the container reaches none of that. Root's environment tells Go and npm to go around the proxy for their registries; if a download fails with a connection error, run it again with `env -u NO_PROXY -u no_proxy -u npm_config_noproxy -u GLOBAL_AGENT_NO_PROXY` before it, so that it goes through the proxy (npm reads a list of its own).
- **Long runs.** A command an agent starts directly is cut off after minutes or, in the background, after two hours, and a full run takes two to six. Never start a run in the shell tool itself: not a full run, not a smoke run, not an arm of the spike, not the exports. "Starting a long run", below this list, says how.
- **Places a chat could leave a note for the next one.** The wrapper empties `/tmp`, `/var/tmp` and `/dev/shm` of `pilot-chat`'s files at each session's first call, and nothing else, and a note left elsewhere would undo "memory off" between trials. Before step 6, run `find / /dev/shm -xdev -perm -0002 \( -type d -o -type f \) ! -path /tmp ! -path /var/tmp ! -path /dev/shm -print`, take the write bit from each path it prints with `chmod o-w`, and run it again: it must print nothing. In a container of this kind it printed two folders (`/run/lock` and one of PHP's) and three lock files.
- **`/srv/pilot` is not on a volume that outlives the container.** Files have survived a restart of a cloud machine, but nothing promises it. Step 8 is the answer.

**Starting a long run.** Write the run as a script in one command and start it in the next, in `tmux`, from an empty environment, so that neither `tmux` nor the driver, nor what the driver runs as root, is handed this session's own variables. These lines stand at the first column on purpose: an `EOF` with spaces before it does not end the script.

```
. /srv/pilot/env.sh; R=<the run's name>; install -d -m 700 $P/runs; set -C; cat > $P/runs/$R.sh <<'EOF'
. /srv/pilot/env.sh
R=<the run's name> X=$P/export S=<sessions> T=<tokens>
iptables -C OUTPUT -m owner --uid-owner "$(id -u pilot-chat)" -j PILOTCHAT 2>/dev/null && [ "$(cat /proc/sys/kernel/random/boot_id)" = "$(cat $P/logs/bound-boot 2>/dev/null)" ] || { echo 9 > $P/logs/$R.exit; exit 9; }
<the run-book's line for this run, from $P/bin/eva-eval on, with $PROXYPASS after its --pass-env flags or its $PASS, cut off just before its first ; echo "exit $?">
echo $? > $P/logs/$R.exit
EOF
```

```
. /srv/pilot/env.sh; R=<the run's name>; sh -n $P/runs/$R.sh && ! tmux -L pilot has-session 2>/dev/null && ! pgrep -x eva-eval >/dev/null && [ ! -e $P/logs/$R.txt ] && rm -f $P/logs/$R.exit && env -i PATH="$PATH" HOME=/root HTTPS_PROXY="$HTTPS_PROXY" tmux -L pilot new-session -d -s "$R" "sh $P/runs/$R.sh" && echo "started $R" || echo "NOT started: $R"
```

- **The script.** The quoted `'EOF'` keeps every `$` for the run's own shell, so the run-book's line is pasted unchanged but for its two ends. Its start, up to `$P/bin/eva-eval`, is left out: a full run's line begins `R=full-...;` and a smoke run's `X=$P/export-smoke R=smoke-$H-$C-1;`, and the script's second line has set both already. Left in, the run-book's own `R=` would send the run's folders, its log and its `.exit` to the run-book's name while every check here reads the name given (tried with a stand-in: a run named `-2` wrote over the `-1` run's log and made none of its own). Its end, from its first `; echo "exit $?"` on, is left out too: what follows there (a smoke run's line goes on with `; cat $P/logs/$R.txt`) would put its own 0 into the `.exit` file in place of the run's 0, 1 or 2. For a smoke run the second line sets `X=$P/export-smoke` and the run-book's `H`, `M`, `PASS`, `C` and `E`; for the first smoke run on a host `--model $M` is left out of the line (step 7). For a rerun (the run-book's step 8, "A rerun") it sets `X=$P/export-rerun-<n>` and the reserve's `S` and `T`. For the exports of the run-book's step 5 and for the `sleep 900` try the second line is `R=<the run's name>` alone, and the exports' middle is that step's block, each command followed by `>> $P/logs/$R.txt 2>&1`. Their `.exit` file holds only the last command's figure, so the exports are judged by their log against the run-book's step 5 **Expect** line, never by the `.exit`.
- **Its third line** refuses to run, with `9` in the `.exit` file and nothing started, when `bind.sh`'s jump is not there or the machine has restarted since `bind.sh` ran (step 6): a run's own output would not show that its chats ran unbound. After a `9`, run `bind.sh` and the proof, then the second command again; it removes the `.exit` a refused start left, so that file only ever holds the latest start's figure. Leave that line out only for what starts no chat and comes before step 6: the exports, and the `sleep 900` try below.
- **Never two.** `set -C` makes the first command refuse a script that exists already: that name was used, so take a new one, ending `-2`. The second command starts the run only when `sh -n` finds no fault in the script, no session of `tmux -L pilot` is alive, no `eva-eval` runs and the run has no log yet; then it prints `started` and the run's name. Anything else ends in `NOT started` and starts nothing, so a command that is tried again, under the same name or another, never starts a second run (tried with stand-in runs).
- **The try without the last flag.** The run-book tries each smoke run and each full run first without `--spend-model-quota`, to read the plan line. Make that try in the shell tool itself, as this step's end does, since it starts nothing: `. /srv/pilot/env.sh`, the script's second line, then the same line from `$P/bin/eva-eval` on without its last flag and without its `> $P/logs/$R.txt 2>&1`. A log under the run's name would make the second command refuse the run itself.
- **One server.** `tmux` gives a new session the environment its server was started with, not that of the command that asks for it (tried: a session asked for through `env -i`, on a server another command had started, held that command's variables). So the `pilot` server is only ever started by the second command above, and no other command names `-L pilot` but `has-session`, `ls` and `kill-server`.
- **Following it.** Every later command names the run again, and the `=` makes `tmux` match that name exactly: `R=<the run's name>; tmux -L pilot has-session -t "=$R"; tail -n 5 /srv/pilot/logs/$R.txt; cat /srv/pilot/logs/$R.exit`. To wait, start `R=<the run's name>; while tmux -L pilot has-session -t "=$R" 2>/dev/null; do sleep 60; done` as a background command and end the turn: it calls back when the run ends, or at its two hours, when the same loop is started again; the loop only waits and starts nothing. A run's name holds only letters, digits and `-`: `tmux` turns a `.` or a `:` into `_` (tried: `smoke.v2` became `smoke_v2`, and the wait would have ended at once). Where the run-book says Ctrl-C, run `pkill -INT -x eva-eval` and wait for the run's own end; never `-KILL`, which leaves no record (the run-book's step 8, "When a run stops"). A run that is killed leaves no record and its whole share counts as spent.
- **(a guess** that `tmux` outlives the command that started it, and that the driver needs no variable `env -i` leaves out: try the first with a script whose middle is `sleep 900`, and read the second from the first smoke run; a variable the driver wants is added to the second command by name, never by dropping `env -i`.)

**End of the step**, at no cost and with no key: the smoke command of the run-book's step 7, for one host, **without `--model` and without its last flag**. As written there, `--model $M` with `M` unset would swallow the next flag. For Claude Code:

```
. /srv/pilot/env.sh; X=$P/export-smoke R=smoke-claude-code-off-1
$P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host claude-code --cli $P/bin/as-chat --condition off --trials 1 --work $P/work/$R --out $P/out/$R --trace $P/trace/$R --max-sessions 1 --max-tokens 1500000 --session-timeout 20m --pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER $PROXYPASS --repo $P/eva-md --keep-trials; echo "exit $?"
```

It must print the plan line and `eva-eval: nothing started: the run needs --spend-model-quota to start a chat`, exit 2, and make no folder. Report to the owner that set-up is done and nothing was spent, that every prompt of theirs from here on (steps 5 and 6) appears in this session, and that each result file reaches them through the tool that sends a file (steps 6 and 8).

### Step 5: the keys (the owner, and the evaluation agent)

**The owner makes the keys.** Neither vendor bounds a single key **(as far as is known; the owner checks in each console)**: one bounds the workspace a key belongs to, the other the project. So each key is made in a workspace or project of its own, created for this evaluation and holding nothing else, whose spending limit is set to 200 US dollars. There the owner also turns the API's own web search and web fetch off, where the console allows it (section 3). The owner checks that the limit refuses requests when it is reached and does not only send a notice. If a provider offers only a notice, no key of that vendor is saved until the owner has chosen another bound, such as 200 dollars of prepaid credit with automatic top-up off, in an account used for nothing else. A limit that starts anew each month bounds a month, not the whole evaluation: the owner says whether that will do.

**The agent asks for them.** First say in a message which secret is needed, for which host and why; then call `request_credential`, once for each host. The call shows the owner a secure form. The agent never sees the value: the result says only whether a key is now saved.

| Host | Header | Label in the form |
| --- | --- | --- |
| `api.anthropic.com` | `header_name` `x-api-key`, no prefix | eva-md evaluation key, Claude Code |
| `api.openai.com` | the default, `Authorization: Bearer` | eva-md evaluation key, Codex |

If a form cannot be shown, the owner adds the two secrets in the environment's settings, with the same hosts and headers. Never ask for a key in a message, and never accept one that arrives in one: tell the owner to revoke it.

**Prove each key is added, at no cost**, with the stand-in and a call that lists models. Both must print 200 **(a guess: that each vendor serves this path to a key)**:

```
. /srv/pilot/env.sh
as_chat curl -sS -o /dev/null -m 30 -w '%{http_code}\n' -H 'x-api-key: stand-in-not-a-key' -H 'anthropic-version: 2023-06-01' https://api.anthropic.com/v1/models
as_chat curl -sS -o /dev/null -m 30 -w '%{http_code}\n' -H 'Authorization: Bearer stand-in-not-a-key' https://api.openai.com/v1/models
```

A 401 means the proxy did not replace the stand-in: the secret's host or header is wrong. A `000`, with `CONNECT tunnel failed, response 403` when the command is run without `-sS -o /dev/null`, means the proxy does not allow that host: no secret is saved for it yet. A 404 means the vendor does not serve that path: ask the owner for another call that costs nothing.

**Then go on with step 6, and once an hour has passed since the second key was saved, ask the owner to look at each key's usage. Step 7 does not start on a host until the owner has answered for that host's key.** Listing models may show as requests that cost nothing; any tokens or cost is usage. It must be none. The evaluation agent is itself a session of a host on this environment, and its own model traffic must not be billed to the evaluation's key **(a guess that it is not: that traffic is the service's own)**. If the key shows usage before any smoke run, stop, have the owner remove that secret, and tell them.

### Step 6: narrow the network, and prove it (the evaluation agent, and the owner)

Call `get_environment`, then `update_environment` with the environment's name and `applies_to` exactly as returned, `remove_allowed_hosts` naming every host of the allowed list it returns but `api.anthropic.com` and `api.openai.com`, should it list them (so `github.com`, and any host added for set-up), and `allow_package_managers: false`. The owner approves the prompt. The change reaches the proxy within about half a minute. Then call `get_environment` again: it must show what section 9's first box says.

That narrows what the proxy lets through. It does not bind the machine: a cloud machine may keep its earlier rules for connections that go around the proxy until it is replaced, and it has been seen to let such connections out. So the user the chats run as is bound to the proxy by the machine's own firewall, as root. Keep these lines as `/srv/pilot/bind.sh`, mode 700, and run them with `bash /srv/pilot/bind.sh`, before any chat has run and with no process of `pilot-chat` alive; they can be run again at any time:

```
. /srv/pilot/env.sh
U=$(id -u pilot-chat) || exit 1
case "$HTTPS_PROXY" in http://127.0.0.1:*) PORT=${HTTPS_PROXY##*:}; PORT=${PORT%%/*};; *) echo "bind: the proxy is not on 127.0.0.1: no rule added"; exit 1;; esac
bound() { awk 'FNR>1 {n=split($2,a,":"); print a[n]}' /proc/net/udp /proc/net/udp6 2>/dev/null | sort -u | while read -r h; do echo $((0x$h)); done; }
for t in iptables ip6tables; do
  $t -N PILOTCHAT 2>/dev/null || $t -F PILOTCHAT
  $t -C OUTPUT -m owner --uid-owner "$U" -j PILOTCHAT 2>/dev/null || $t -I OUTPUT 1 -m owner --uid-owner "$U" -j PILOTCHAT
  $t -A PILOTCHAT ! -o lo -j REJECT
  for p in $(bound); do $t -A PILOTCHAT -o lo -p udp --dport "$p" -j REJECT; done
  for p in $(listeners); do [ "$p" = "$PORT" ] || $t -A PILOTCHAT -o lo -p tcp --dport "$p" -j REJECT; done
  printf '%s: jump %s, rules %s\n' $t "$($t -S OUTPUT | grep -c -- "--uid-owner $U ")" "$($t -S PILOTCHAT | grep -c -- '-A PILOTCHAT')"
done
tee $P/logs/bound-boot < /proc/sys/kernel/random/boot_id
```

Everything `pilot-chat` sends goes through a chain of its own, which refuses all that would leave the machine and, on the machine itself, every port that something already listens on but the proxy's. A cloud container runs services of its own there, its environment manager among them, and they are no chat's business. A port a chat opens itself later, as a task's tests may, stays reachable. This was tried on 2026-11-15 in a cloud container of this kind, with a stand-in user: the rules were accepted; that user's direct lines went from showing an address to `exit 7 ip  status 000`; the machine's other listeners, two of which had answered it, refused it; the proxy still answered, and so did a server that user started itself. **(a guess** still that the evaluation's own container is alike.)

Each of the two lines must read `jump 1` and `rules` of 1 or more; the counts look for the user's number, since `iptables` lists a rule by number and never by name. If `ip6tables` alone refuses, go on only if `/proc/net/tcp6` does not exist, which means the machine has no IPv6; otherwise stop and ask the owner. If it prints `bind: the proxy is not on 127.0.0.1`, no rule was added: ask the owner. The last line is the machine's boot id, kept in `$P/logs/bound-boot`: it changes when the machine restarts, and a restart drops every rule, which is what a run's own third line looks for (step 4).

Then prove it from inside, as the user the chats run as: through the proxy, as a chat's tools go, and around it, as a chat with a shell could try. Keep these lines as `/srv/pilot/proof.sh`, mode 700, so that the proof is made the same way each time, and run it with `bash /srv/pilot/proof.sh 2>&1 | tee /srv/pilot/logs/proof-$(date -u +%Y%m%dT%H%M%S).txt`:

```
. /srv/pilot/env.sh
hosts="github.com raw.githubusercontent.com codeload.github.com api.github.com objects.githubusercontent.com gist.githubusercontent.com proxy.golang.org sum.golang.org registry.npmjs.org pypi.org cdn.jsdelivr.net mcp-proxy.anthropic.com goproxy.io goproxy.cn cdn.statically.io raw.githack.com sourcegraph.com example.com"
for h in $hosts; do printf 'proxy  %s: ' "$h"; as_chat curl -sS -o /dev/null -m 90 -w 'connect %{http_connect} status %{http_code}\n' "https://$h/" 2>/dev/null | grep . || echo 'no result'; done
direct() { printf 'direct %s: ' "$1"; as_chat curl -sS -g --noproxy '*' -o /dev/null -m 20 -w 'exit %{exitcode} ip %{remote_ip} status %{http_code}\n' "$1" 2>/dev/null | grep . || echo 'no result'; }
for h in $hosts api.anthropic.com api.openai.com; do direct "https://$h/"; done
for u in https://140.82.112.3/ http://140.82.112.3/ https://1.1.1.1/ http://1.1.1.1/ 'https://[2606:4700:4700::1111]/' http://169.254.169.254/ https://198.51.100.7/; do direct "$u"; done
PORT=${HTTPS_PROXY##*:}; PORT=${PORT%%/*}
for p in $(listeners); do [ "$p" = "$PORT" ] && n='the proxy' || n="port $p"; for a in 127.0.0.1 '[::1]'; do printf 'local %s %s: ' "$a" "$n"; as_chat curl -sS -g --noproxy '*' -o /dev/null -m 5 -w 'exit %{exitcode} status %{http_code}\n' "http://$a:$p/" 2>/dev/null | grep . || echo 'no result'; done; done
printf 'dns: '; as_chat getent hosts example.com | grep -c .
printf 'rule: '; iptables -S OUTPUT | grep -c -- "--uid-owner $(id -u pilot-chat) "; printf 'boot: '; cat /proc/sys/kernel/random/boot_id
```

- **Through the proxy**, every host of the list must be refused: `connect` is 403 or 407, which is the proxy's answer, and `status` is 000. A `connect 200` means the host is reachable: wait a minute and run it again, and if it stays, stop and tell the owner. A `connect 000` is no answer either way: the proxy can hold a connection for about a minute before it refuses it, which is why the limit here is 90 seconds; if it stays 000, read the proxy's status page and ask the owner. A `connect 429` or `connect 503` is the proxy being busy, not an answer about the host: wait a minute and make the proof again.
- **Around the proxy**, no line may show an address. A line passes when `ip` is followed at once by `status`, as in `exit 7 ip  status 000`: no connection was made. With the rules in place a name reads `exit 6` (`pilot-chat` can no longer look a name up, and needs not: the proxy does that), an address reads `exit 7`, and `dns:` prints 0. The lines with addresses in place of names matter most: a name that is not found proves nothing about an address, and they try plain HTTP, IPv6 and the machine's metadata address as well. The two API hosts are no exception: a chat reaches them only through the proxy, the one place its key is added.
- **On the machine itself**, every `local` line reads `exit 7` but the proxy's own on 127.0.0.1, which answers with a status: a chat may talk to the proxy and to what it starts itself, and to nothing else that listens here.
- **`rule:`** prints 1 and **`boot:`** the same id as `cat $P/logs/bound-boot`; anything else means the rules are gone: run `bind.sh` and the proof again. Only where `iptables` refused the rules (below) does `rule:` print 0.
- **An address after `ip`**, on any line, means a connection was made around the proxy, whatever the exit or the status: stop. Do not try to read such lines without the rules. A cloud machine's gateway can answer a direct connection to any address itself, with a certificate of its own, so an address does not tell a host that was reached from one that was refused; the last line, `198.51.100.7`, belongs to no one, and an address there shows exactly that (it showed one in a container whose network was not limited). **This is the check that decides whether the design holds.**
- **`no result`** means the command did not run: run that one line again without `2>/dev/null` and read why. It is never a pass.
- **If `iptables` refuses the rules**, run the proof anyway and hand the owner its lines. It passes without them only if no line shows an address, the lines with addresses read `exit 7` or `exit 28`, and every `local` line but the proxy's reads `exit 7`. If any line shows an address, stop: tell the owner that this machine cannot be shown to hold the evaluation, and that what comes next is theirs to choose (a session started anew on the narrowed environment cannot do set-up; another kind of container can).
- If a tool refuses to run one of these probes for a reason of its own, that is no result: say so and ask the owner, and do not take it for a refused host.

**The proof is made again before every run**, smoke or full, and before each arm of the spike, after `pkill -KILL -u pilot-chat; bash /srv/pilot/bind.sh`: together they take about two minutes and cost nothing. `bind.sh` refuses only the ports something listens on when it runs (tried: a server root started after it answered the stand-in user), so it is run again each time, to take in any listener started since, and to bind anew a machine that restarted. A network that was proven once says nothing of a machine that has restarted since.

Three more checks, once:

1. **The keys still arrive**: step 5's two calls again; both must still print 200, through the proxy and under the rules.
2. **What `pilot-chat` can read** (the run-book's step 7, item 8), through `as_chat` and not as the run-book writes it: `as_chat sh -c "ls $P/export $P/out $P/trace $P/eva-md /root; cat $P/export/tasks/d01/task.json"` must print `Permission denied` each time. And `ls -ld /srv/pilot/eva-md /srv/pilot/export*` shows mode 700, `ls -l /srv/pilot/env.sh` mode 600. What a chat can read besides is in section 3.
3. **The way out for results**: write a made-up file, `printf 'test\n' > /srv/pilot/send-test.txt`, and hand it to the owner with the tool that sends a file to the user **(a guess that it works once the network is narrowed)**. If it does not arrive, stop before any smoke run and tell the owner: step 8 depends on it.

For the owner's information, not as a pass or a fail: `for u in https://github.com/ https://198.51.100.7/; do printf 'root direct %s: ' "$u"; curl -sS -g --noproxy '*' -o /dev/null -m 20 -w 'exit %{exitcode} ip %{remote_ip} status %{http_code}\n' "$u" 2>/dev/null | grep . || echo 'no result'; done` shows what root reaches around the proxy; tell the owner both lines. An `exit 60` with an address is a connection too: something around the proxy answered, with a certificate of its own. Grading runs a chat's code as root, which the rules do not bind (section 3).

**No chat starts on any host until every line here is as written.**

### Step 7: the smoke runs, the web probe and the price (the evaluation agent)

The run-book's step 7, one host at a time, with what follows. Each smoke run is started as a long run is started (step 4), after the proof of step 6 has been made again.

- **First, the owner's answer** to step 5's look at each key's usage. No smoke run starts on a host until the owner has said that host's key shows none: a smoke run's own spend would hide what the agent's own traffic put there.
- **After each smoke run**, ask the owner for that key's usage over the run's hours: its tokens or cost, for the price below, whether any line is a web search or fetch, and whether any file is stored there (section 3). A web line or a stored file means that run's results are not used: start no further run, and ask the owner.
- **The owner's leave** is their message of step 3. The run-book's step 1 lists it among what must be true, and first among what is asked of the owner.
- **The model.** The first smoke run on a host is made without `--model`. Read the id its rows name, as the run-book's step 7, item 5 shows; add it to `env.sh` as `M_CC` or `M_CX`, and give it as `--model` on every later command for that host. It is never written into the repository or into a report that goes there.
- **The sign-in check** of the run-book's step 7 is skipped, as it says for a proxy that adds the key. Under the `eva` condition the sign-in program writes the stand-in into the trial's home.
- **The web probe** (the run-book's step 7, item 11) runs as `pilot-chat` in a throwaway home, never with root's environment: `install -d -o pilot-chat -g pilot-chat -m 700 /srv/probe/home /srv/probe/tmp`, then, from `/srv/probe/home`, `as_chat env HOME=/srv/probe/home TMPDIR=/srv/probe/tmp ANTHROPIC_API_KEY=stand-in-not-a-key <the command> > $P/logs/probe-<host>-<1 or 2>.out 2>&1` (for Codex its own key's variable), with `pkill -KILL -u pilot-chat` after each of the two commands, and `rm -rf /srv/probe` afterwards. Before each of the two commands, run `pkill -KILL -u pilot-chat; bash /srv/pilot/bind.sh` and make the proof, as before a run: the probe is a chat, and no run's third line guards it. Each output is root's alone and is read only through the shape reader. The probe counts only if each output holds the host's own closing event, read through the shape reader (Claude Code: an event of type `result`; Codex: `turn.completed`). An empty output, or a CLI that did not start, is no result.
- **The rates.** A session's price, and the report's `--rates`, need each vendor's published prices for the model the smoke run named; the spike needs each vendor's three model ids. The container cannot look them up. Use the session's own web search, which is the service's and not the container's, or ask the owner for them in the session. They go on a command line and into what the owner is told, never into the repository.
- **The price and the split.** From the smoke runs' rows, work out a session's tokens on each host and split the owner's ceiling as the run-book's step 6 shows. Tell the owner the split and a session's price in money before the first full run. If the six runs do not fit the ceiling, stop: more is the owner's to give.

A full run starts on a host only when that host's smoke runs and its web probe are clean, as the run-book says.

### Step 8: the full runs, and keeping their results (the evaluation agent)

The run-book's step 8: six runs, one at a time, each started as a long run is started (step 4), memory off and the ideal record before v1. Before each, the proof of step 6 is made again.

After **each** run, once its readings are confirmed and the run-book's check after a run shows only what its step 8 allows (the heading of `ps`, and for `pgrep` root's own eva-md jobs):

1. Archive that run alone, where only root can read it: `. /srv/pilot/env.sh; R=<the run's name>; install -d -m 700 $P/keep; tar -czf $P/keep/$R.tar.gz -C $P out/$R logs/$R.txt; sha256sum $P/keep/$R.tar.gz`.
2. Hand `$P/keep/$R.tar.gz` itself to the owner with the tool that sends a file to the user, with one line naming the run and its checksum. Make no other copy. It goes nowhere else: no commit, no issue, no message's text.
3. Ask the owner for that key's usage over the run's hours: its tokens beside the run's own count (the run-book's step 8), whether any line is a web search or fetch, and whether any file is stored there. A web line or a stored file means the run's results are not used (section 3): start no further run, and ask the owner.
4. When a host's three runs are done, make that host's report (the run-book's step 10) and give the owner its printed figures, which name no model.

**If the container is lost.** A run that was going leaves no record: its whole share counts as spent, and its tasks can be run again only from the reserve, which is smaller than a run. So ask the owner before going on. A run whose archive the owner holds is not lost: the owner gives the file back to the new session. The set-up is lost too: the first command of the owner's message (step 3) is run again, then steps 4 and 6, with `M_CC` and `M_CX` put into `env.sh` again from the first smoke runs' rows. Step 4 needs the network widened for it, which is one approval; then it is narrowed, `bind.sh` is run and the proof is made again before any chat. A machine that only restarted keeps its files and loses the rules: step 6 says how that is seen.

**If `hosts.go` must be corrected after step 6** (the run-book's step 9): the build's lead merges the correction on `main` and gives the evaluation agent its patch as text (`git format-patch -1 --stdout`), through the owner. Apply it in the clone with `git am < <the patch's file>`, with the identity and signing root's git already has, so the checkout is clean (`git status --short` prints nothing) and holds a commit. If git stops for want of either (it prints `fatal: failed to write commit object` or `unable to auto-detect email address`), run `git am --abort`, so that `git status --short` prints nothing again; set nothing and turn nothing off (`AGENTS.md`), whatever git's own message suggests, and use the widening below **(a guess that root's git signs on the narrowed network)**. Then build by the run-book's step 4 without its first line, whose fetch would fail and whose checkout would undo the patch: `. /srv/pilot/env.sh; cd $P/eva-md && git status --short && git log -1 --format=%H && GOPROXY=off go build -mod=readonly -tags evamd_eval -o $P/bin/eva-md ./cmd/eva-md && chmod 755 $P/bin/eva-md && GOPROXY=off go build -mod=readonly -o $P/bin/eva-eval ./cmd/eva-eval && chmod 700 $P/bin/eva-eval && go version -m $P/bin/eva-md | grep -e -tags`. Reports then name both the clone's commit and `main`'s. If that cannot work, the network is widened only when no run is going and the run-book's check after a run shows nothing of `pilot-chat` and nothing under `$P/work` left alive; then fetch, build, narrow, and make step 6's proof again, `bind.sh` included.

### Step 9: the contract spike (the evaluation agent)

In the same container, after the pilot's six runs and their reports, never while a run is going: the spike bills the same two keys under the same bounds, and a pilot run that fails for want of credit part way counts as spent in full. Read [eval/spike/run-plan.md](eval/spike/run-plan.md) first. `eva-eval spike plan --out <file>` writes the calls and tokens of every arm before anything is called; `eva-eval spike run` calls nothing without `--spend-model-quota`.

The spike does not go through the pilot's driver, so nothing of steps 4 to 8 signs it in. It calls the host's CLI through v1's update runner, as root, and that runner drops every key's variable, keeps the caller's `HOME` and `NO_PROXY`, and with no `--cli` runs whatever CLI is first on `PATH`. Left alone it would run the session's own CLI with the session's own home, around the proxy. So, each line **a guess** until one arm has answered:

1. A home of its own, never root's: `install -d -m 700 /srv/spike/home`. Sign each CLI in there with the stand-in, as the sign-in program does in a trial's home. Claude Code: `. /srv/pilot/env.sh; cd /srv/spike/home && umask 077 && mkdir -p .claude && printf '%s' "$ANTHROPIC_API_KEY" > .claude/pilot-key && printf '{"apiKeyHelper": "cat %s/.claude/pilot-key"}\n' /srv/spike/home > .claude/settings.json`. Then `wc -c /srv/spike/home/.claude/pilot-key` prints 18, the stand-in's length; a 0 means `env.sh` was not read. Codex: `. /srv/pilot/env.sh; printf '%s' "$CODEX_API_KEY" | env -u CODEX_HOME HOME=/srv/spike/home /opt/pilot/hosts/codex/bin/codex login --with-api-key`. Never write under `/root/.claude` or `/root/.codex`: those are the session's own.
2. Every arm, with the proxy's bypass list unset and the installed CLI named:

   ```
   . /srv/pilot/env.sh
   env -u NO_PROXY -u no_proxy -u CLAUDE_CONFIG_DIR -u CODEX_HOME HOME=/srv/spike/home $P/bin/eva-eval spike run --host <host> --tier <tier> --effort <effort> --form <form> --out $P/out/$R --model <that tier's model> --cli /opt/pilot/hosts/<claude|codex>/bin/<claude|codex> --repo $P/eva-md --spend-model-quota
   ```

   Each arm is started as a long run is started (step 4): the script's second line is `R=spike-<host>-<tier>-<effort>-<form>` (an arm started again takes a new name, ending `-2`), and its middle is the `env -u` line above with `> $P/logs/$R.txt 2>&1` after it, so that the arm's folder, its log and its `.exit` all carry the name given: `spike run` adds its rows to a folder that holds some already, and a second attempt must not write into the first one's. An arm makes 28 first calls and at most as many repair calls, which can take longer than a command of the shell tool may run, and an arm that is cut off has billed its calls and may leave no rows. There `env -i` has already left the bypass list out; the `env -u` stays, and does no harm.
3. One arm first, of the small tier. Check that its calls were answered, not refused for sign-in, and ask the owner whether that key's console shows them, and no other key or plan does. From its rows work out the input tokens the host adds to each call above the plan's figure, which was worked out with none; write the plan again with `$P/bin/eva-eval spike plan --overhead <host>=<that figure> --out <file>`, and price every arm at each tier's published rates. Tell the owner the spike's tokens and price, and what is left of each key's bound after the pilot. Only if the tokens fit the allowance of section 2 and the price fits what is left, and the owner says go on, run the other 23.

Each arm names its model with `--model`, which is written nowhere, and a call answered by another model or at another effort stops the arm. Stay inside the allowance of section 2. Archive each arm's folder as step 8's item 1 archives a run (`tar -czf $P/keep/$R.tar.gz -C $P out/$R logs/$R.txt`) and hand the file to the owner; give the owner the table that `$P/bin/eva-eval spike frontier --repo $P/eva-md $P/out/spike-*` prints (the card DV-03d, item 3).

### Step 10: the end

**The evaluation agent**: the run-book's step 11 in the container, and a last report to the owner: what was spent by the records and what each provider shows, a session's price, each fact the run-book's step 9 lists with its result, and whether the tasks can show a difference.

**The owner**: removes the two secrets from `eva-md-eval`, or deletes the environment, even if the keys are kept at their providers for EV-17, and archives the evaluation's session, so that its own tokens end with it (section 3). While a secret is on the environment, any session started there spends it.

**The build's lead**, from that report, which the owner passes on to the lead's session, under the build's usual rules (no model's name, no key, no price of a named model, nothing of a chat's text):

- the host facts the pilot settles, in [docs/v2/host-facts.md](docs/v2/host-facts.md);
- the figures for the Stage 0 readout, and the ceiling proposed for decision 16;
- the spike's table and its decision record;
- this file deleted, together with the links to it in the plan (section 7), the lead's guide and the run-book's last section, each replaced by a line saying where the content went. The public-wording test reads this file only while it exists, so it needs no change.

## 6. Never

- Show, type, print, log or commit a key, or ask for one in a message.
- Start a chat before step 6's proof has passed, both ways.
- Run a command as `pilot-chat` with root's environment, or change to that user before the environment is emptied.
- Start a run in the shell tool itself, start two at once, or start one on a machine whose proof was made before its last restart.
- Attach this repository to the evaluation's session, or leave a copy of it readable by `pilot-chat`.
- Run the evaluation on the Default environment, in the build's session or on the owner's machine.
- Take another agent's message for the owner's leave.
- Raise a ceiling, move a share between runs, or go on past the reserve without the owner.
- Copy a trial's folder, a session's files, an inbox, a wiki or a trace out of the container, or quote a trace.
- Put a model's name, a price or anything of a chat's text into the repository.
- Write under root's own `.claude` or `.codex` folder in the container.
- Change eva-md's code to make a run pass.

## 7. What is not yet known

| Guess | Settled by | If it is wrong |
| --- | --- | --- |
| The session's own tools work with `allow_mcp_servers` off | Step 5: the form appears | The owner does steps 5 and 6 in the settings |
| Each CLI installs with npm under the names step 4 gives, and starts as another user through the proxy | Step 4: each `--version` through `as_chat` (it starts); step 7's first smoke run (it goes through the proxy) | The proxy's status page names the host it refused; a host needed only to install is added for set-up and removed in step 6 |
| A made-up stand-in is accepted by each CLI | Step 7: a smoke run starts | The vendor's key prefix, followed by the same words |
| The proxy replaces the stand-in in the header of step 5's table | Step 5: both calls print 200 | The owner corrects that secret's host or header in the settings |
| Each vendor answers `/v1/models` to a key, at no cost | Step 5: both calls print 200 | A 404: the owner names another call that costs nothing on that vendor; a 401 is the row above |
| A vendor bounds a workspace or a project, not one key | Step 5: the owner reads each console | The owner chooses another bound before that key is saved |
| The evaluation agent's own traffic is not billed to the key | Step 5: the console shows nothing after an hour | Stop; the owner removes the secret |
| The evaluation's container lets root add a firewall rule for one user, as a container of its kind did, and `pilot-chat`'s CLI needs no name looked up but through the proxy | Step 6: `bind.sh` prints `jump 1` twice and the proof is as written; step 7: a smoke run is judged | Without the rules the proof passes only on its own lines (step 6); else stop, and the owner chooses |
| The tool that sends a file to the user works once the network is narrowed | Step 6: a made-up file arrives | No run starts; the owner chooses another way out for the results |
| `tmux` outlives the command that started it, and the driver needs nothing `env -i` leaves out | Step 4: a run whose middle is `sleep 900` is there in a later command; step 7: the first smoke run starts | Ask the owner before any run; a variable the driver wants is added by name |
| Root's git identity and signing work on the narrowed network | Step 8: `git am` of a correction makes a commit | `git am --abort`; widen the network as step 8 says |
| Each CLI needs no host but its vendor's API host for a chat on a key | Step 7: a smoke run is judged | Tell the owner the host's name. It is added only if it cannot serve this repository's files |
| The host table's flags turn every web tool off on each host | Step 7: the web probe | No full run on that host until `hosts.go` is corrected (step 8) |
| A run of two to six hours survives in a cloud container | Step 8 | Ask the owner before a rerun; the archives handed over are safe |
| The spike's CLI signs in from a home of its own and goes through the proxy | Step 9: one arm first | Stop the spike and tell the owner; the pilot is not affected |

## 8. When to stop and ask the owner

- Anything suggests a key leaked, or a real key appears where a stand-in should be: stop every run and tell them to revoke it.
- A key shows usage that the evaluation did not make.
- A line of step 6's proof shows an address or a listener that answers, with the rules or without them, or the rules cannot be added and the proof does not pass on its own.
- A check of step 4, 5 or 6 does not print what it must, or a tool refuses to run one of step 6's probes.
- A key's usage shows a web search or fetch, or a stored file, after a smoke run or a full run (section 3).
- A run's `.exit` file holds 9 a second time, after `bind.sh` and the proof were made again.
- The made-up file of step 6 does not reach the owner.
- The owner declines a prompt (steps 1 and 6): ask what they want changed, and do not call it again unasked.
- The spike's first arm is refused for sign-in, its calls are not on the evaluation's key, or its price does not fit (step 9).
- A host cannot run on a key alone, or the proxy cannot add the key for it.
- The runs do not fit the ceiling, the reserve is spent, or the container was lost during a run.
- A provider's figure for a run's hours is far above the run's own count of tokens.

## 9. Done when

The environment part is done when all of these are true; from there the run-book alone governs. The second box is the setting-up agent's, before step 3. The evaluation agent checks the others and reports them to the owner in one message.

- [ ] `get_environment`, called in the evaluation's session, shows `eva-md-eval`: personal, `session_has_repository` false, no setup script, package managers and connectors off, no allowed host but those its two secrets bring.
- [ ] The plan's section 7 and the lead's guide say the environment exists.
- [ ] The evaluation's session runs on it with no repository attached, and its first message is the owner's leave.
- [ ] The run-book's steps 3 to 5 passed their checks, and step 4's last command here printed the plan line and exited 2; and step 4's two `find` commands, for another copy and for writable places, printed nothing.
- [ ] `request_credential` reported both keys saved, both calls of step 5 printed 200, and the owner has said that each key is bounded and showed no usage before the first smoke run.
- [ ] Step 6's proof is as written on every line, with `bind.sh`'s rules in place; both calls of step 5 still print 200; `pilot-chat`'s read check prints `Permission denied` each time; `ls -ld` shows mode 700 and `ls -l /srv/pilot/env.sh` mode 600; and the made-up file reached the owner.
- [ ] The owner has been told which session shows their prompts, and how each result file reaches them.

## 10. What else is open

Not part of this document; the lead's guide, [docs/v2/leading-the-build.md](docs/v2/leading-the-build.md), has each in full.

- **The build.** Every card that could be built without the owner is merged or in its last round (the cards' index, [docs/v2/cards/README.md](docs/v2/cards/README.md), says which). The latency gate has given no verdict yet for the three health packages merged on 2026-11-14: it wants a quiet machine.
- **The owner's own steps**, which no agent takes: releases, the upgrade of the real install, the host sittings, the live gates on v1 and the second field evaluation.
- **After the evaluation**: the Stage 0 readout, which decides the packages that wait for it (the signed upgrade, the 28 continuation tasks).
