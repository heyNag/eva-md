# TODO: the environment for the live evaluation

Written on 2026-11-14 for an agent that has never worked on this. It covers one piece of work: giving v2's two live evaluation runs, the pilot and the contract spike, a place to run. Everything else that is open in the build is in [docs/v2/leading-the-build.md](docs/v2/leading-the-build.md); section 9 below lists it in a few lines.

The owner wants this done. The first attempt to create the environment was declined only so that this plan would be written down first. Whoever takes this over proposes the environment again, once, as written here.

## 1. The issue

Two rows of the plan's Stage 0 end in a live run, which is a run that makes real model calls and costs money:

- **The pilot** (plan row EV-03): ten small coding tasks, run by each host's CLI (Claude Code and Codex) under three conditions (memory off, an ideal record, eva-md v1), three trials each: 180 agent sessions. It gives the first real figures of what a session costs and whether the tasks can show a difference. Its run-book is [docs/v2/pilot-runbook.md](docs/v2/pilot-runbook.md).
- **The contract spike** (plan row DV-03): about 1,344 single model calls through v1's update runner, which compare two forms of the update's contract over three sizes of model. Its plan is [eval/spike/run-plan.md](eval/spike/run-plan.md) and its card [docs/v2/cards/DV-03d.md](docs/v2/cards/DV-03d.md).

All the code for both is merged and checked offline. Neither has run, because they have no place to run. The place must meet all of these at once, and nothing that exists today does:

1. **The runs are billed to two API keys made for the evaluation alone**, one for each vendor (plan decision 28). The keys' values must never be in a chat message, a file of the repository, a log, a commit or an agent's context.
2. **The keys must not be on the owner's Default cloud environment.** A secret saved on an environment is sent by every session that runs on it. Default is where every other session of the owner's runs.
3. **While evaluation chats run, the container must reach the two vendors' API hosts and nothing else.** The pilot's tasks have hidden tests, and they are in this public repository. A chat that can reach GitHub, or Go's module proxy, which serves this repository's files, can read the answers.
4. **The build's own session cannot be that container.** It needs GitHub all the time (pushes, CI), which point 3 forbids.
5. **A checkout of this repository must not be readable by the user the chats run as.** A session with the repository attached has it at `/home/user/eva-md`, readable by every user of the machine. So the evaluation's session has no repository attached, and makes one clone that only root can read.
6. **The owner must see what each key spends**, and something outside eva-md's own counting must bound it.
7. **Results must outlive the container**, without entering this repository: their rows name the model, which the repository never does.

## 2. What is already decided

The owner said on 2026-11-14 to take the lead's recommendation on everything that waited on them. Each answer is in [docs/v2/plan.md](docs/v2/plan.md), section 7, "Where the decisions stand", and a word from the owner changes it:

| Question | Answer |
| --- | --- |
| Leave for the pilot | Yes, inside 240 sessions and 120,000,000 tokens by the driver's count, a tenth of it held back for reruns |
| Leave for the spike | Yes, inside its row's allowance: about 1,900 calls and 25,000,000 tokens |
| The keys | One for each vendor, for the evaluation only, each with a spending limit of 200 US dollars set at its provider |
| The model | Each host's own default for an API key, at the version of its CLI the pilot pins. It is read from the first smoke run and then named on every command line, never in the repository. The spike's three tiers are that vendor's small, middle and large models of the day |
| The keys afterwards | Kept for the spike and for EV-17, each under its limit; revoked at the Stage 0 readout or at the owner's word |
| What it costs | A rough estimate only, from third-party price lists: 60 to 160 dollars for the pilot, 30 to 100 for the spike. The smoke run prices a session on each host before any full run starts |

Not decided until this document is carried out: where the container runs. Section 3 is the proposal.

The leave takes effect on a host when the owner has put that host's key in. That is the owner's own act: no agent can make a key, and no agent asks for one in a chat message.

## 3. The proposed solution, in short

One cloud environment of the owner's account, named `eva-md-eval`, used by one session that does nothing but the evaluation.

- **The keys are the environment's network secrets.** The owner types each into a secure form. The environment's proxy adds it to requests to that vendor's API host. No value reaches the container, the agent or a trial's home: inside the container each key's variable holds a made-up stand-in.
- **The network has two phases.** For set-up it reaches github.com and the package registries. Before the first chat it is narrowed to the two API hosts alone, and that is checked from inside, as the user the chats run as.
- **The evaluation's session has no repository attached.** It clones this public repository once during set-up, into a folder only root can read.
- **The build's session is untouched.** It stays on Default with its own network.
- **Each key has a spending limit at its provider**, which the owner sets when making it. The owner reads each key's usage in that provider's console.
- **After each run, its results are handed to the owner as a file** and nowhere else.

**What the owner does, in all:**

1. Approves one prompt that creates the environment (section 4, step 1).
2. Makes the two keys, each with its 200-dollar limit, and types each into a form the evaluation's session shows (step 4).
3. Approves one prompt that narrows the environment's network (step 5).
4. Keeps the result files the session hands over (step 7).

Later changes of the network, should a fix have to be fetched during the runs, are one more approval each.

**Alternatives looked at and not taken:**

- *The keys on Default.* Every session would send them (point 2).
- *The build's session as the container.* It needs GitHub (points 3 and 4).
- *The owner's Mac, in a container.* It needs the Mac awake and the owner to start every run. It stays decision 28's fallback for what cannot run on a key alone.

## 4. Step by step

Two agents take part. **The setting-up agent** is whoever picks up this document: a cloud session with this repository attached, on Default. It does steps 1 and 2, watches the rest, and writes what was learned into the repository at the end. **The evaluation agent** is the session that step 2 starts: it does steps 3 to 8 and has no repository and no way to push.

The tools named below are a cloud session's own environment and session tools. If the agent that picks this up has none of them, the owner can do steps 1, 4 and 5 by hand in the app's settings for environments, with the same values, and start the session of step 2 themselves on `eva-md-eval`, with no repository, giving it the brief of step 2.

### Before step 1: read

1. [AGENTS.md](AGENTS.md), above all its rules on the real machine, on secrets and on live runs.
2. [docs/v2/pilot-runbook.md](docs/v2/pilot-runbook.md), whole. It is the procedure; this document only gives it a place to run. Its last section holds the lead's rulings and the owner's answers.
3. [docs/v2/plan.md](docs/v2/plan.md), section 7, "Where the decisions stand", and decisions 16 and 28 in the table above it.
4. For the spike: [eval/spike/run-plan.md](eval/spike/run-plan.md) and [docs/v2/cards/DV-03d.md](docs/v2/cards/DV-03d.md).

### Step 1: create the environment (the setting-up agent)

Look at the current environment first (`get_environment`), as the tool asks. Then tell the owner in one sentence what is about to be created and why, and call `create_environment` with:

| Field | Value | Why |
| --- | --- | --- |
| `name` | `eva-md-eval` | |
| `scope` | `personal` | Only the owner's sessions use it |
| `allowed_hosts` | `["github.com"]` | Set-up clones this repository. The two API hosts are not listed: saving a key for a host allows that host |
| `allow_package_managers` | `true` | Set-up downloads Go modules and installs each host's CLI |
| `allow_mcp_servers` | `true` | So the session's own tools keep working; see the open point in section 6 |
| `switch_session` | `none` | The setting-up agent's own session stays on Default |
| `repositories` | left out | Sessions that build eva-md must not be steered onto this environment |
| `setup_script`, variables | none | Set-up is done by hand, by the run-book |

The call is itself the request: the owner sees a prompt with these values and approves it. If they decline, stop and ask what they want changed; do not call it again unasked.

### Step 2: start the evaluation's session (the setting-up agent)

Call `create_session` with `environment_id` set to the new environment's id, **no `source_url`** (point 5 of section 1), a title such as `eva-md: the live evaluation`, and this brief as `prompt`:

> You run the live evaluation of eva-md v2: the pilot, then the contract spike. The owner of the repository has given leave for both inside fixed ceilings, and the procedure is written down. Clone `https://github.com/heyNag/eva-md` as the pilot's run-book says (`/srv/pilot/eva-md`, mode 700) and read, in this order: `TODO.md`, `AGENTS.md`, `docs/v2/pilot-runbook.md`, and section 7 of `docs/v2/plan.md` ("Where the decisions stand"). Then do steps 3 to 8 of `TODO.md`, in order, stopping wherever it says to stop. Never show, type, print or log a key. Never read a trace but through the run-book's shape reader. Never start a chat before step 5's check has passed. Never raise a ceiling. Report to the session that started you, by a message, at the end of each step and whenever something is not as the run-book expects.

Tell the owner the session's title, since two of their approvals happen in it. From here the setting-up agent only reads that session's reports and answers its questions.

### Step 3: set up the container, at no cost (the evaluation agent)

Follow the run-book's steps 3, 4 and 5 as written: the user `pilot-chat`, the folders under `/srv/pilot`, each host's CLI under `/opt/pilot/hosts/<host>/` at one version, the wrapper `as-chat`, the sign-in program, the shape reader, the two builds, the export and its checks. Each step has an **Expect** line: go on only when it matches.

What a cloud container adds to those steps:

- **The proxy and its certificate.** All HTTPS leaves through a local proxy named by `HTTPS_PROXY`, which re-signs TLS. `/root/.ccr/README.md` in the container explains it. The certificate bundle is under `/root`, which `pilot-chat` cannot read and which the driver refuses as a value. Copy the bundle to a folder outside `/root` that `pilot-chat` can read, for example `/srv/pilot/ca/ca-bundle.crt`, mode 644 in a folder of mode 755.
- **What every run command passes on.** Besides what the run-book lists: `--pass-env HTTPS_PROXY --pass-env SSL_CERT_FILE --pass-env NODE_EXTRA_CA_CERTS --pass-env CODEX_CA_CERTIFICATE`, with the last three set, in root's shell, to the copy of the bundle. The run-book's step 2 says the same in its last line.
- **The keys' variables hold stand-ins.** Set `ANTHROPIC_API_KEY` and `CODEX_API_KEY` in root's shell to made-up values in the form each CLI expects of a key. They are never real. The proxy puts the real key in place of what the CLI sends.
- **The sign-in check of the run-book's step 7 is skipped**, as it says for a proxy that adds the key, and under the `eva` condition the sign-in program writes the stand-in into the trial's home.
- **`/srv/pilot` is not on a volume that outlives the container.** Files have survived a restart of a cloud machine, but nothing promises it. Step 7 below is the answer.

End of the step: for one host, run the smoke command of the run-book's step 7 **without its last flag**. It must print the plan line and `nothing started`, and exit 2. Nothing has been spent, and no key exists yet.

### Step 4: the keys (the evaluation agent, and the owner)

First say in a message which secret is needed, for which host and why; then call `request_credential`, once for each host. The call shows the owner a secure form. The agent never sees the value: the result only says whether a key is now saved.

| Host | Header | Label in the form |
| --- | --- | --- |
| `api.anthropic.com` | `header_name` `x-api-key`, no prefix | eva-md evaluation key, Claude Code |
| `api.openai.com` | the default, `Authorization: Bearer` | eva-md evaluation key, Codex |

Both headers are what each vendor's API documents for a key; whether each CLI sends its key that way is proven by the smoke run (section 6).

Ask the owner, in the same message, to give each key a spending limit of 200 US dollars at its provider before typing it in, and to make it for this evaluation only.

If a form cannot be shown, or the owner would sooner not use it, the owner adds the two secrets in the environment's settings, with the same hosts and headers. Never ask for a key in a message, and never accept one that arrives in one: tell the owner to revoke it.

### Step 5: narrow the network, and prove it (the evaluation agent, and the owner)

Call `get_environment`, then `update_environment` with the environment's name and `applies_to` exactly as returned, `remove_allowed_hosts: ["github.com"]` and `allow_package_managers: false`. The owner approves the prompt. The change reaches the container within about half a minute.

Then prove it from inside, **as the user the chats run as**, with the proxy's variables set. Each of these must be refused by the proxy:

```
for h in github.com raw.githubusercontent.com codeload.github.com api.github.com proxy.golang.org sum.golang.org registry.npmjs.org pypi.org example.com; do
  printf '%s: ' "$h"
  setpriv --reuid=pilot-chat --regid=pilot-chat --init-groups -- env HTTPS_PROXY="$HTTPS_PROXY" SSL_CERT_FILE=/srv/pilot/ca/ca-bundle.crt curl -sS -o /dev/null -m 20 -w '%{http_code}\n' "https://$h/" 2>&1 | tail -1
done
```

A refused host shows a proxy error (403 or 407 on CONNECT), never a page's status. The two API hosts must answer: the same command for `api.anthropic.com` and `api.openai.com` prints a status the API itself gave. The run-book's own check of what `pilot-chat` can read (its step 7, item 8) is made as well.

**No chat starts on any host until every line of this check is as written.** If a host that should be refused answers, stop and tell the setting-up agent.

### Step 6: the smoke runs, the web probe and the price (the evaluation agent)

The run-book's step 7, one host at a time, with these two additions:

- **The model.** The first smoke run on a host is made without `--model`. Read the id its rows name, as step 7's item 5 shows, and give that id as `--model` on every later command for that host. Never write it into the repository or a report that goes there.
- **The price.** From the smoke runs' rows, work out a session's tokens on each host and split the owner's ceiling as the run-book's step 6 shows. Report the split, and a session's price in money at the vendor's own published rates, to the setting-up agent before the first full run. If the six runs do not fit the ceiling, stop: more is the owner's to give.

The smoke runs also settle what this document only guesses (section 6). A full run starts on a host only when that host's smoke runs and its web probe are clean, as the run-book says.

### Step 7: the full runs, and keeping their results (the evaluation agent)

The run-book's step 8: six runs, one at a time, in `tmux`, memory off and the ideal record before v1.

After **each** run, once its readings are confirmed:

1. Make the archive the run-book's step 11 describes, of `out` and `logs` alone, with its `sha256sum`.
2. Hand it to the owner as a file from the session (put it in the session's working folder and send it), with one line saying which run it holds. It goes nowhere else: no commit, no issue, no message's text.
3. When a host's three runs are done, make that host's report (the run-book's step 10) and send its printed figures, which name no model, to the setting-up agent.

If the container is lost, a run that had not ended is run again from the reserve, as the run-book's table "When a run stops" says for a restart. A run whose archive the owner holds is not lost: the owner can give the file back to a new session.

### Step 8: the contract spike (the evaluation agent)

In the same container, with the same keys, after the pilot or between its runs, never at the same time as a run. `eva-eval spike plan` prints the calls and tokens of every arm before anything is called; `eva-eval spike run` calls nothing without `--spend-model-quota`. Each arm names its model with `--model`, which is written nowhere. Stay inside the row's allowance of section 2. The spike's rows and its frontier go to the setting-up agent as the pilot's reports do.

### At the end (the setting-up agent)

From the evaluation agent's reports, under the build's usual rules (commits on `main`, no model's name, no key, no price of a named model, nothing of a chat's text):

- the host facts the pilot settles, in [docs/v2/host-facts.md](docs/v2/host-facts.md) (the run-book's step 9 lists them);
- the figures for the Stage 0 readout, and the ceiling proposed for decision 16;
- the spike's table and its decision record;
- this file, deleted, with a line in the guide saying where its content went.

Then the run-book's step 11 in the container, and a word to the owner on whether to revoke the keys or keep them for EV-17.

## 5. Never

- Show, type, print, log or commit a key, or ask for one in a message.
- Start a chat while the network reaches more than the two API hosts.
- Attach this repository to the evaluation's session, or leave a clone of it readable by `pilot-chat`.
- Run the evaluation on the Default environment, in the build's session or on the owner's machine.
- Raise a ceiling, move a share between runs, or go on past the reserve without the owner.
- Copy a trial's folder, a session's files, an inbox, a wiki or a trace out of the container, or quote a trace.
- Put a model's name, a price or anything of a chat's text into the repository.
- Change eva-md's code to make a run pass. A correction of how a host is driven changes `internal/eval/continuation/drive/hosts.go` alone, through the build's lead, as the run-book's step 9 says.

## 6. What is not yet known

Each is settled by the step named, and none costs more than the smoke runs' few sessions.

| Guess | Settled by | If it is wrong |
| --- | --- | --- |
| The npm packages of the two CLIs install and start under the proxy, as another user | Step 3: each CLI's `--version` as `pilot-chat` | Read the proxy's status page, which `/root/.ccr/README.md` names, for the host it refused. A host needed only for installing is added for set-up and removed in step 5 |
| Each CLI sends its key in the header of step 4's table, so the proxy can replace the stand-in | Step 6: a smoke run is judged, not `sign_in` | The owner changes that secret's header in the environment's settings; the stand-in's form may need to change |
| Each CLI needs no host but its vendor's API host to run a chat on a key | Step 6: a smoke run is judged; the proxy's status shows no refusal that mattered | Tell the setting-up agent the host's name. Adding it is the owner's approval, and only if it cannot serve this repository's files |
| With `allow_mcp_servers` on, no connector's host can serve this repository's files without signing in | Step 5: list the session's connectors; a chat has none of their sign-ins | Turn it off in step 5's call, if the session's own tools still work without it; else tell the owner |
| A run of two to six hours survives in a cloud container | Step 7 | Rerun from the reserve; the archives already handed over are safe |
| The host table's flags turn every web tool off on each host | The run-book's web probe (its step 7, item 11) | No full run on that host until `hosts.go` is corrected |

When a correction must be fetched after step 5: stop every run, widen the network for the fetch and the build, narrow it again, and make step 5's check again. Each change is one approval of the owner's.

## 7. When to stop and ask the owner

- Anything suggests a key leaked: stop every run and tell them to revoke it.
- A host cannot run on a key alone, or the proxy cannot add the key for it.
- The runs do not fit the ceiling, or the reserve is spent.
- A provider's figure for a run's hours is far above the run's own count of tokens.
- A host that step 5 says must be refused answers.

## 8. Done when

The environment part is done when all of these are true; from there the run-book alone governs:

- [ ] `eva-md-eval` exists, personal, with no repository and no setup script.
- [ ] The evaluation's session runs on it, with no repository attached.
- [ ] The run-book's steps 3 to 5 gave their **Expect** lines, and a smoke command without its last flag printed the plan line and exited 2.
- [ ] Both keys are saved as the environment's secrets, each with its limit at its provider, and no value was ever in a message or a file.
- [ ] The network reaches the two API hosts alone, proven as `pilot-chat` by step 5's check.
- [ ] The owner knows which session shows their prompts, and has the first result file's place.

## 9. What else is open

Not part of this document; [docs/v2/leading-the-build.md](docs/v2/leading-the-build.md) has each in full.

- **The build.** Every card that could be built without the owner is merged or in its last round (the cards' index, [docs/v2/cards/README.md](docs/v2/cards/README.md), says which). The latency gate has given no verdict yet for the three health packages merged on 2026-11-14: it wants a quiet machine.
- **The owner's own steps**, which no agent takes: releases, the upgrade of the real install, the host sittings, the live gates on v1 and the second field evaluation.
- **After the evaluation**: the Stage 0 readout, which decides the packages that wait for it (the signed upgrade, the 28 continuation tasks).
