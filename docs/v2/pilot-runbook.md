# The pilot's live run: a run-book

The commands were checked against `main` at `c2ca020d0c03` (2026-11-14), with EV-03b, EV-17g, EV-17c and EV-03c merged; step 1 says what the driver still owes. Every line about how a real host behaves is unproven until the smoke run (step 7) shows it on that host, the sign-in program (step 3) above all; those lines say **(a guess)**.

**What it is for.** The first real figures of an agent session: its cost, its variance, and whether the tasks can show a difference. It is [the plan](plan.md)'s Stage 0 row EV-03: the ten pilot tasks on the continuation driver under memory off, the ideal record and eva-md v1, on both hosts, in a container billed to evaluation keys (decision 28). A session's price sets decision 16's ceiling (15.4).

**Who runs it.** The lead, never a builder. **How to follow it.** Every block runs as root in the container, in one shell, in order. In a new shell, or after a restart, first set `P=/srv/pilot`, `export DISABLE_AUTOUPDATER=1` and the step's own variables. A removal written `${P:?}` removes nothing while `P` is unset.

## 1. Before anything starts

If one is not true, stop.

1. **The price in money**, stated by the lead to the owner (decision 28): about 180 sessions of 0.3 to 0.7 million tokens, 80 to 90% cached, at the published prices for the model the owner names.
2. **The owner's answers** to the list below: leave in so many words, since the build's standing leave (`AGENTS.md`) covers no live run, and decision 28's two keys, for evaluation only (step 2), among them.
3. **Merged on `main`**: EV-03a to EV-03c, EV-17a to EV-17d, EV-17g. `<commit>` is the first 12 characters of the commit built. (EV-17g's third round found no cause for one CI failure, run 176; it changes nothing here.)
4. **The lead's rulings** listed at the end of step 12 are made.

**Asked of the owner**, with the price:

- leave, with a ceiling in sessions and in tokens for the pilot alone;
- the two keys, in the platform's secret store only, each with a spending limit at its provider where it allows one;
- the exact model each host is to use;
- whether a tenth of the ceiling may be held back for reruns;
- where the container runs, and who can see each key's usage;
- whether the sign-in program may write each key into a trial's home for that trial's life (step 3);
- whether the keys are revoked after the pilot or kept for EV-17.

**Owed on the driver**: one short round before the first full run (EV-17c's card). Until it is merged, step 8 starts only on the lead's ruling, and:

- **A run the stop rule ended does not add up.** When an `eva` run's first two trials both end `setup_failed` or `update_failed`, the third ends the run (`eva-eval: trial <id>.t3: the first two trials both ended ...`, exit 2, `"ended":"error"`) uncounted: the record's counts sum to one less than `planned`.
- **`--sign-in` shows nowhere.** Neither the plan line nor the record names it, and it is checked, as `--eva-md`'s tag is, only once `--spend-model-quota` is given. So `ls -l $P/bin/sign-in $P/bin/eva-md` before each `eva` run, and read the command line before Enter.
- **Spend not counted** by `--max-tokens` or the record: the sign-in program's, a failed update's, and the update of a trial whose chat ended unjudged (the condition sums the update only after a chat that ended without a code). The key's limit at its provider bounds it; the lead compares each record's `tokens` with the provider's figure (step 8).

## 2. The keys, and how one reaches a chat

- The owner puts the two keys in the container platform's secret store, never in a message, issue, commit or file, each with a spending limit at the ceiling's price where its vendor allows one: the one bound no fault here can pass.
- Each is a variable of root's environment: `ANTHROPIC_API_KEY` for Claude Code; for Codex the one its documents name for `codex exec` at the installed version (`CODEX_API_KEY` when this was written). Where the container's proxy adds the key, the variable holds a stand-in.
- The driver gets only the name, by `--pass-env`, and passes the value only into the trial. No key is a flag, or in a file the repository holds, a row, a log or this run-book; a passed value of 8 letters and digits or more is a word no row may hold. Check one with `[ -n "$ANTHROPIC_API_KEY" ] && echo set`, never `env`.
- **v1's update drops every key**: it gives the CLI a cleared environment, by design (`childEnv`, `internal/update/process.go`). So under `eva` the sign-in program (step 3) writes the key into the trial's home, which the driver removes with the trial.
- A proxy's variables are passed by adding their `--pass-env` to every command of steps 7 and 8. v1's update keeps `HTTPS_PROXY`, `SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS` and `CODEX_CA_CERTIFICATE`: name the CA file through those, outside `/root`, since the driver refuses a value that reaches root's home.

## 3. The container

**What it holds.** Linux with util-linux (`setpriv`), procps, GNU coreutils and findutils; git 2.44 or newer; Go 1.26 or newer; `python3`; each host's CLI, by its own installer, in `/opt/pilot/hosts/<host>/`, 0755, at one version for the whole pilot. Network: the providers' API hosts; GitHub, Go's module proxy and the installers' hosts only for this step and step 4.

**Two users.** root runs the driver, the `eva` condition's commands, grading and removal. `pilot-chat` runs the chats: no home, no password, no sudo.

```
useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin pilot-chat
P=/srv/pilot; install -d -m 0711 $P $P/work; install -d -m 0700 $P/out $P/logs $P/trace; install -d -m 0755 $P/bin
git clone https://github.com/heyNag/eva-md $P/eva-md && chmod 700 $P/eva-md
export DISABLE_AUTOUPDATER=1
```

`/srv/pilot` is on a volume that outlives the container, mounted there, never reached through a link: the driver resolves `--work`'s links, and both scripts below refuse a trial outside `/srv/pilot/work`.

| Path | Mode (root's) | Holds | `pilot-chat` |
| --- | --- | --- | --- |
| `$P`, `$P/work` | 0711 | all below; a work folder a run | passes through, cannot list |
| `$P/eva-md`, `$P/out`, `$P/logs`, `$P/trace` | 0700 | the checkout; a results folder a run; what runs printed; a trace a run | no |
| `$P/export*` | 0700 (the export's) | tasks with hidden tests, repositories | no |
| `$P/bin` | 0755 | `eva-md`, `as-chat` (0755); `eva-eval`, `sign-in`, `shape` (0700) | runs `eva-md`, `as-chat` |

In a trial, `pilot-chat` reaches its own `home`, `tmp` and `workspace`; `grade/` is root's alone.

**The wrapper**, `$P/bin/as-chat`, given as `--cli`, which the driver links into each trial's `bin/` under the CLI's name. A call made as root in the trial's workspace is a chat: the wrapper hands the trial's home, `tmp` and workspace to `pilot-chat` and starts the real CLI as that user. Any other call runs as its caller.

```sh
#!/bin/sh
set -eu  # stop at the first failure
case $(basename "$0") in  # the driver's link names the CLI
claude) real=/opt/pilot/hosts/claude/bin/claude ;;  # by absolute path: PATH starts with this file's link
codex) real=/opt/pilot/hosts/codex/bin/codex ;;  # likewise
*) echo "as-chat: unknown CLI" >&2; exit 97 ;;  # any other name is a set-up fault
esac  # end of the host's choice
user=pilot-chat  # the chat's user
trial=${HOME%/home}  # every caller gives HOME=<trial>/home
[ "$(id -u)" = 0 ] && [ "$(pwd -P)" = "$trial/workspace" ] || exec "$real" "$@"  # no chat call: run as the caller
case $trial in /srv/pilot/work/*/*) ;; *) exit 97 ;; esac  # hand over nothing outside a trial
fresh=yes  # a session's first call, until a resume is seen
for a in "$@"; do [ "$a" = -- ] && break; case $a in --resume|resume) fresh=no ;; esac; done  # flags before the prompt
[ $fresh = no ] || pkill -KILL -u $user || true  # a new session: end what an earlier one left running
[ $fresh = no ] || find /tmp /var/tmp /dev/shm -xdev -user $user -delete 2>/dev/null || true  # and its shared temporary files
chown -hRP $user:$user "$trial/home" "$trial/tmp" "$trial/workspace"  # hand them over; links never followed
exec setpriv --reuid=$user --regid=$user --init-groups --no-new-privs -- "$real" "$@"  # same environment, streams and process group
```

**Where each call runs**, as the code has it: the version probe in the trial's home, the chat in its workspace; under `eva`, as root, the sign-in program in the home, setup's plugin commands and the replayed hooks' jobs in `/`, v1's update in an empty folder of the trial's `tmp`. The replayed hooks run in the workspace but start only eva-md and git. Every caller keeps `HOME`. The wrapper prints nothing but its one refusal line, and `exec` hands the real CLI the driver's stdout and stderr unchanged.

`install -m 0755 as-chat $P/bin/`. Runs go one at a time: the wrapper ends every process of `pilot-chat` at a session's first call. `cat /proc/sys/fs/protected_hardlinks` must print `1`, or a chat could hard-link a root file into its workspace for the next `chown -R` to give it.

**The sign-in program**, `$P/bin/sign-in`, given as `--sign-in` under `eva` alone. The condition runs it once in each trial before setup, as root, in the trial's home, with the trial's environment and so the variables `--pass-env` names. Its output is not read; a failure or stall is `setup_failed`.

```sh
#!/bin/sh
# sign-in: signs the host's CLI in within the trial's home, from the key the trial's environment holds
set -eu  # stop at the first failure: setup_failed
umask 077  # root's alone until the wrapper hands the home over
case $HOME in /srv/pilot/work/*/home) ;; *) exit 97 ;; esac  # write nothing outside a trial's home
[ "$(pwd -P)" = "$HOME" ] || exit 97  # the condition runs it in the trial's home
if [ -n "${ANTHROPIC_API_KEY:-}" ] && [ -z "${CODEX_API_KEY:-}" ]; then  # a Claude Code run: its key alone is passed
  mkdir -p .claude  # Claude Code's own folder in the trial's home
  printf '%s' "$ANTHROPIC_API_KEY" > .claude/pilot-key  # the key, in the trial's home alone; printf is built in: no argument list shows it
  printf '{"apiKeyHelper": "cat %s/.claude/pilot-key"}\n' "$HOME" > .claude/settings.json  # a guess: Claude Code asks this helper when no variable holds a key
elif [ -n "${CODEX_API_KEY:-}" ] && [ -z "${ANTHROPIC_API_KEY:-}" ]; then  # a Codex run
  printf '%s' "$CODEX_API_KEY" | codex login --with-api-key > /dev/null 2>&1  # a guess: Codex takes the key on stdin and keeps its sign-in in <home>/.codex
else exit 97; fi  # neither key, or both: a set-up fault
```

`install -m 0700 sign-in $P/bin/`. Its `codex` is the trial's link, which the wrapper runs as root. **No one has run this against a real host**: the smoke run proves it host by host (step 7, item 7). Three more guesses go with its two: setup's plugin commands keep Claude Code's setting; v1's update, run with `--safe-mode`, still asks the helper; a chat given one key by variable and helper runs as before. Jobs the new chat's last hooks start may run signed in, uncounted, until the trial is removed. Memory off and the ideal record do not use it.

**The shape reader**, `$P/bin/shape`, for a trace (step 7): event types and field names, never a value; `modelUsage`, whose fields are models' names, as a count.

```python
#!/usr/bin/env -S python3 -I
# shape FILE: each line of a trace's stdout as its event types and field names, never a value
import json, sys
for n, line in enumerate(open(sys.argv[1], errors="replace"), 1):
    try:
        e = json.loads(line)
    except ValueError:
        e = None
    if not isinstance(e, dict):
        print(n, "not a JSON object"); continue
    kinds = [e[k] for k in ("type", "subtype") if isinstance(e.get(k), str)]
    if isinstance(e.get("item"), dict) and isinstance(e["item"].get("type"), str):
        kinds.append("item." + e["item"]["type"])
    nested = {k: (len(v) if k == "modelUsage" else sorted(v)) for k, v in e.items() if isinstance(v, dict)}
    print(n, " ".join(kinds), sorted(e), nested)
```

`install -m 0700 shape $P/bin/`.

**What it cannot protect against.** Grading runs the chat's code as root (EV-17e moves it): it can read every hidden test and any key, and write anywhere. The driver finds a hidden test copied whole or by path, not reworded. The chat holds its key and can send it wherever the network allows. A process that leaves its group lives until the next session's first call or step 8's check. The chat owns its home's files, the sign-in's among them. A host's web search may run at its provider, past the container's network, and the hidden tests are in this public repository: the driver would not see one found that way.

## 4. Building what runs

```
cd $P/eva-md && git fetch origin && git checkout --detach <commit> && git status --short && git log -1 --format=%H
go telemetry off
go build -mod=readonly -tags evamd_eval -o $P/bin/eva-md ./cmd/eva-md && chmod 755 $P/bin/eva-md
go build -mod=readonly -o $P/bin/eva-eval ./cmd/eva-eval && chmod 700 $P/bin/eva-eval
go version -m $P/bin/eva-md | grep -e -tags
```

**Expect:** nothing from `git status` (rows record a dirty checkout), an id beginning `<commit>`, then `build	-tags=evamd_eval`.

The tag builds `cmd/eva-md/evalbuild.go` (EV-17d): setup runs on Linux, and a hook with `EVA_MD_EVAL_ACTOR=person` takes a headless Claude Code chat for a person's. No release carries it. The `eva` condition checks the tag only once `--spend-model-quota` is given; the last line checks it before.

## 5. The export, and the continuation's check

```
cd $P/eva-md && T0=$P/eva-md/eval/pilot/testdata/tasks
$P/bin/eva-eval pilot check --tasks $T0
$P/bin/eva-eval pilot export --tasks $T0 --out $P/export
$P/bin/eva-eval continuation check --tasks $P/export/tasks --repos $P/export/repos
$P/bin/eva-eval pilot export --tasks $T0 --out $P/export-smoke --task d01
stat -c '%a %U' $P/export $P/export-smoke
```

**Expect:** ten `<id> <family> ok`; ten `<id> <family> exported` and `session-timeout 20m`; ten `<id> ok`; `d01 hidden exported`, `session-timeout 20m`; twice `700 root`. Each runs every hidden test: minutes. A task refused is reported to the lead, never edited.

## 6. The plan of the runs

| Run | Host | Condition | Tasks | Trials | Sessions | `--session-timeout` |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | claude-code | off | 10 | 3 | 30 | 20m |
| 2 | codex | off | 10 | 3 | 30 | 20m |
| 3 | claude-code | oracle | 10 | 3 | 30 | 20m |
| 4 | codex | oracle | 10 | 3 | 30 | 20m |
| 5 | claude-code | eva | 10 | 3 | 30 | 20m |
| 6 | codex | eva | 10 | 3 | 30 | 20m |

180 sessions, as the plan says; 20 minutes is the export's line (the driver's default is 30). **What the driver counts.** A session is one trial, every round in one host session. Tokens are each call's prompt tokens (cached in full) and output tokens, and under `eva` v1's update, only after a chat that ended without a code (step 1). `--max-sessions` refuses a plan of more sessions. `--max-tokens` is checked before each trial, so a run can pass it by one session. A call cut off before its host printed usage is unjudged and uncounted, and the run goes on (EV-17g's ruling 1).

**Splitting the owner's ceiling**, once step 7 is done, from what it spent. The figures are made up. Round every share down. What all runs are given never sums past the owner's ceiling, and what a run leaves unspent goes to no other. A smoke run made after the split, to prove a fix, comes out of the reserve.

| | Sessions | Tokens |
| --- | --- | --- |
| The owner's ceiling | 240 | 120,000,000 |
| Smoke (step 7) as spent: the larger of its records' `tokens`, reruns included, and the providers' figures for those hours | 8 | 9,000,000 |
| Left | 232 | 111,000,000 |
| Reserve for reruns: a tenth, rounded down | 23 | 11,100,000 |
| For the six runs | 209 | 99,900,000 |
| A run's share: a sixth, rounded down | 34 | 16,650,000 |
| Margin for one session past the threshold: twice the smoke's largest (say 900,000) | | 1,800,000 |
| `--max-sessions` and `--max-tokens` of each run | 34 | 14,850,000 |

Then check a run fits: 30 sessions at the smoke's median (say 400,000), plus under `eva` its memory tokens (say 60,000): 30 × 460,000 = 13,800,000, under 14,850,000. If one does not fit, stop and ask the owner for a higher ceiling or fewer trials.

**The plan line**, printed before anything starts:

```
plan: condition off, host claude-code, tasks 10, trials 3 each, sessions 30; ceilings: sessions 34, tokens 14850000; trial folders removed; each call's output kept in the trace
```

With `--keep-trials` the folders read `kept, each closed to others once it ends`. Without `--spend-model-quota` it prints `eva-eval: nothing started: the run needs --spend-model-quota to start a chat`, exits 2 and makes no folder.

## 7. The smoke run

One task, d01 (a hidden decision, so memory off should need a correction and a continued session), one trial, per host and condition, folders kept. One at a time: claude-code off, oracle, eva; then codex off, oracle, eva. Set:

- `M` is the exact id the host reports, not an alias (lower-case letters, digits, `.`, `_`, `-`); a session naming another model is unjudged `other_model`.
- Claude Code: `H=claude-code M=<the model the owner names for it> PASS="--pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER"`
- Codex: `H=codex M=<the model the owner names for it> PASS="--pass-env CODEX_API_KEY"`
- `C=off E=`, `C=oracle E=`, or `C=eva E="--eva-md $P/bin/eva-md --sign-in $P/bin/sign-in"`

```
X=$P/export-smoke R=smoke-$H-$C-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host $H --cli $P/bin/as-chat --condition $C --trials 1 --work $P/work/$R --out $P/out/$R --trace $P/trace/$R --max-sessions 1 --max-tokens 1500000 --model $M --session-timeout 20m $PASS $E --repo $P/eva-md --keep-trials --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"; cat $P/logs/$R.txt
```

Run it first without its last flag (the plan line, exit 2), then as written. **Expect:** `run <id>`; `trial d01.t1: solved ..., first try ..., corrections ..., rounds ...`; `run <id>: planned 1, judged 1, ..., hidden_found 0; tokens ..., rows ...`; `exit 0`.

**The sign-in check**, once per host, at no cost: the off command again, `R=smoke-$H-signin-1`, with the key's variable set to a made-up wrong value for that command only (`ANTHROPIC_API_KEY=made-up-wrong-0000 $P/bin/eva-eval ...`). **Expect:** `trial d01.t1: unjudged, sign_in`, `exit 1`. Skip it where a proxy adds the key.

**The trace.** `$P/trace/$R/<run>/d01/<condition>/t1/` holds each call's two streams, root's alone: call 1 is the version probe (`1-version.stdout`, `.stderr`), then one a round for the chat (`2-chat.*`, `3-chat.*`, ...). Read it only through `$P/bin/shape <file>`, or `wc -c` for a stderr: **only event types and field names are read from it**, never a value or a line of text. It holds the chat's text and may hold a key a chat printed: nothing of it is copied, quoted or committed.

| A wrong reading | Read | What the reader needs there |
| --- | --- | --- |
| `no_usage` | the last `<k>-chat.stdout` | Claude Code: `result` with `usage`, or `modelUsage` of 1 or more; Codex: each `turn.completed` with `usage` |
| `no model` throughout, or `other_model` | each `<k>-chat.stdout` | Claude Code: `modelUsage`, or `model` in an `assistant` event's `message`; Codex: `turn.completed` with `model` |
| `host_failed` for a sign-in or rate limit | the failing `<k>-chat.stdout` | Claude Code: `result` with `is_error`, `subtype`, `api_error_status` (401, 403 or 429); Codex: `turn.failed` (`error.message`) or `error` (`message`), whose text must hold 401, 403 or 429. The reader shows whether the fields are there, not those numbers: the lead rules on more |
| `the host's CLI gave no version` | `1-version.*` | their lengths |

**Known (EV-17g):** with a trace, a process the CLI started that left its group and still holds the error stream makes the call fail after the wait: `eva-eval: trial d01.t1: the host's CLI: a process the host's CLI started still holds its output`, exit 2. Without a trace that call succeeds. A smoke run that meets it is made again without `--trace`, ending `-2`, and the lead reads why.

**What to look at.** Kept trials are under `$P/work/smoke-*`, closed to all but root; under `eva` they hold the key the sign-in wrote, until this step ends.

1. **`memory_off`** (eva): must be 0; a 1 stopped the run. On Claude Code the plugin's hooks did not fire under the key (F-115) or the actor seam failed; on Codex, no managed hooks were read from `<home>/.codex/requirements.toml` (F-163). The lead rules before that host's v1 run, and asks the owner if no route works in a container.
2. **The plugin loaded twice** (Claude Code eva): `find $P/work/smoke-claude-code-eva-1 -path '*eva-md/inbox/*' -name '*.jsonl' -exec grep -c '"kind":"prompt"' {} +`; the new chat's file counts its rounds. Twice that: setup's registration and `--plugin-dir` both load it; the lead rules on `hosts.go`'s plugin entry.
3. **Codex's managed hooks** (Codex eva): as item 1, and the kept inbox holds the new chat's session with events.
4. **No usage**: `no_usage 0`. A 1 stopped the run: the reader missed the usage, or the CLI changed. Read the trace, then a fix (step 9).
5. **The model**: the summary's `no model`, and `grep -o '"model":"[^"]*"' $P/out/$R/results.jsonl | sort | uniq -c`. A session naming no model is judged, its rows carry none. On `other_model`, check `--model` against the id the host names, then the trace; no full run starts while a smoke shows one.
6. **Sign-in and rate-limit readings**: the sign-in check reads `sign_in`; a rate limit met later must read `rate_limited`, and `host_failed` there means the reader is wrong: read the trace.
7. **v1's update and the sign-in program** (eva): `setup_failed 0`, `update_failed 0`, which proves the program on that host. If not: `find $P/work/$R -path '*/eva-md/updates/*.jsonl' -exec grep -ho '"reason":"[a-z_]*"' {} + | sort | uniq -c`. `auth`: the update found no sign-in in the trial's home. No line, with `setup_failed`: the program or setup failed first. Then no `eva` full run starts on that host (there the stop rule would end it after two trials): the lead mends the program, never eva-md, and that host's eva smoke runs again, ending `-2`; if no route works by the host's own means, the lead asks the owner (decision 28's fallback). Off and oracle go ahead. v1's update drops `DISABLE_AUTOUPDATER` too (`keepEnv`), so **(a guess)** a `timeout` or `host_error` there may be the CLI updating itself.
8. **What `pilot-chat` could reach.** `setpriv --reuid=pilot-chat --regid=pilot-chat --init-groups -- sh -c "ls $P/export $P/out $P/trace $P/eva-md /root; cat $P/export/tasks/d01/task.json"`: `Permission denied` each time. After every run, step 8's check prints nothing. `hidden_found 0` in every summary.
9. **Telemetry, record**: each kept trial's `home/.config/go/telemetry/mode` reads `off`; `runs.jsonl` holds one line, `"ended":"done"`, one version in `host_versions`, `"kept":true`.
10. **Figures**: the rows' `prompt_tokens`, `output_tokens`, `wall_ms`, and under `eva` `memory_tokens`, for step 6; beside them each provider's figure.

**When something is wrong**, the lead rules; a fix to `hosts.go` (step 9) or the sign-in program is followed by that host's smoke again, ending `-2`. Ask the owner when a host cannot run on a key alone (F-067 refuted), a key may have leaked, or the runs cannot fit the ceiling. Before step 8, `rm -rf ${P:?}/work/smoke-*`; `rm -rf ${P:?}/trace/smoke-*` once no fix still needs a trace.

## 8. The full runs

Memory off and the ideal record first, since they need no eva-md; v1 last, on a host only once its `eva` smoke judged d01.t1 (`memory_off 0`, `setup_failed 0`, `update_failed 0`) and step 1's owed round is merged, or the lead has ruled. One at a time, in `tmux`, so a dropped connection does not end it (outside `tmux` a hang-up ends a run as Ctrl-C does). `S` and `T` are step 6's ceilings; `M_CC` and `M_CX` the models' ids; `X=$P/export`. Run each first without its last flag and read the plan line with `cat $P/logs/$R.txt`. A run takes about 2 to 6 hours, at most about 12.

```
R=full-claude-code-off-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host claude-code --cli $P/bin/as-chat --condition off --trials 3 --work $P/work/$R --out $P/out/$R --trace $P/trace/$R --max-sessions $S --max-tokens $T --model $M_CC --session-timeout 20m --pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-codex-off-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host codex --cli $P/bin/as-chat --condition off --trials 3 --work $P/work/$R --out $P/out/$R --trace $P/trace/$R --max-sessions $S --max-tokens $T --model $M_CX --session-timeout 20m --pass-env CODEX_API_KEY --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-claude-code-oracle-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host claude-code --cli $P/bin/as-chat --condition oracle --trials 3 --work $P/work/$R --out $P/out/$R --trace $P/trace/$R --max-sessions $S --max-tokens $T --model $M_CC --session-timeout 20m --pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-codex-oracle-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host codex --cli $P/bin/as-chat --condition oracle --trials 3 --work $P/work/$R --out $P/out/$R --trace $P/trace/$R --max-sessions $S --max-tokens $T --model $M_CX --session-timeout 20m --pass-env CODEX_API_KEY --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-claude-code-eva-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host claude-code --cli $P/bin/as-chat --condition eva --trials 3 --work $P/work/$R --out $P/out/$R --trace $P/trace/$R --max-sessions $S --max-tokens $T --model $M_CC --session-timeout 20m --pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER --eva-md $P/bin/eva-md --sign-in $P/bin/sign-in --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-codex-eva-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host codex --cli $P/bin/as-chat --condition eva --trials 3 --work $P/work/$R --out $P/out/$R --trace $P/trace/$R --max-sessions $S --max-tokens $T --model $M_CX --session-timeout 20m --pass-env CODEX_API_KEY --eva-md $P/bin/eva-md --sign-in $P/bin/sign-in --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
```

Follow one with `tail -f $P/logs/$R.txt`. **Expect:** a line a trial, then the summary; `exit 0` when all were judged, 1 when one was unjudged or not run, 2 when nothing started or the run stopped.

**After each run**, the smoke's too, since nothing else ends the last trial's leftovers: `pkill -KILL -u pilot-chat; ps -u pilot-chat; pgrep -af "$P/work/"; find / /tmp /var/tmp /dev/shm /run $P -xdev -user pilot-chat -not -path "$P/work/*" 2>/dev/null`. Only `ps`'s heading prints (`pgrep` finds root's eva-md jobs a trial's replayed hooks left); anything found is removed and told to the lead first. Then the lead reads `grep -o '"tokens":[0-9]*' $P/out/$R/runs.jsonl` beside the provider's figure for the run's hours, larger by what no ceiling counts (step 1). Once the run's readings are confirmed, `rm -rf ${P:?}/trace/${R:?}`.

**A fresh `--out`, `--work` and `--trace` for each attempt**, ending `-2`, `-3`: the driver refuses an `--out` holding a row or record of the same host, condition and task, a `--work` not empty, and a `--trace` not new or empty.

**A rerun** takes only tasks with no judged trial, so none counts twice: `$P/bin/eva-eval pilot export --tasks $T0 --out $P/export-rerun-<n> --task <id> [--task <id>]...` (one flag a task), then the run's command with that folder as `X`, a `-2` name, and ceilings from the reserve: `--max-sessions` 3 a task, `--max-tokens` the part of the reserve it takes less step 6's margin. When the reserve is spent, ask the owner.

**When a run stops.** To stop one, press Ctrl-C once, or `pkill -INT -x eva-eval` from another shell, and wait: it ends the call, removes the trial's folder and writes the record, ended `error`, as a hang-up does. Never `-KILL`: that leaves no record.

| It shows | Do |
| --- | --- |
| `eva-eval: the run stopped: a trial is unjudged, no_usage` | Step 7 item 4, from this run's trace; then rerun the tasks with no judged trial |
| `... unjudged, memory_off` | Step 7 item 1; the lead rules |
| `... unjudged, hidden_found` | A fault of the set-up: no further run until the lead finds it (the wrapper, a folder's mode, a task) |
| `eva-eval: trial <id>.t3: the first two trials both ended ...` | The stop rule (step 1). The trials are gone, so that host's `eva` smoke runs again (`-2`, from the reserve) and step 7 item 7 reads it; the lead rules; rerun after the fix |
| `...: a process the host's CLI started still holds its output` | The trace's known fault (step 7); the lead rules on a rerun without `--trace` |
| Ended `error`, or another `eva-eval:` line | Tell the lead; the line names no path |
| `unjudged, sign_in` | Ctrl-C; check the key (step 2); a revoked one: ask the owner |
| `unjudged, other_model` | Ctrl-C at the first: every further trial would be the same; check `--model` (step 7 item 5) |
| `unjudged, rate_limited` or `host_failed` | Rerun those tasks another day. Half the trials or more: Ctrl-C, wait a day |
| `unjudged, timeout` | Many: tell the lead, who alone changes `--session-timeout` |
| `unjudged, update_failed` or `setup_failed`, after the first two | Three in a row: Ctrl-C (each further one would be the same, its spend uncounted); the lead rules |
| `not run, --max-tokens reached` | The share is spent: the reserve, or the owner |
| A restart, or a killed process | No record: its `--out` is never used again nor read by the report; its whole share counts as spent; `pkill -KILL -u pilot-chat`, `rm -rf ${P:?}/work/${R:?} ${P:?}/trace/${R:?}`; rerun all its tasks |

**Never:** reuse an `--out`, `--work` or `--trace`; raise a ceiling, or move a share, without the owner; run a host outside the container; run two runs at once; change `hosts.go` or the sign-in program while a run goes; edit a task; copy a trial's folder, session files, inbox, wiki or trace out of the container; quote a trace; show or type a key.

## 9. Recording what was learned

**Host facts.** In [host-facts.md](host-facts.md) the pilot settles, by host: F-067 (judged trials on a key alone), F-068 (`agent_events`), F-069 (corrections, resumed sessions), F-115 and F-163 (`memory_off`), F-207 (tokens, cached share and minutes a session). Each **Result.** line becomes **Result, Claude Code.** and **Result, Codex.**, as F-018's are: confirmed, refuted or not settled; the date; the CLI's version from `host_versions`; if refuted, the fallback; the sign-in method's name. Never a model's name, a key, a price, an account, or the container's provider.

**The host table.** A correction of how a host is driven (flags, the reader, a sign-in or rate-limit reading, the plugin entry), from what a trace showed, changes `internal/eval/continuation/drive/hosts.go` alone, with its tests and the fake's shape, on a builder's branch under the lead's order, merged before the next run; then `eva-eval` is rebuilt (step 4). The report shows both commits.

**To the owner**, at the readout: what was spent, by the records and by each provider; a session's price; the ceiling proposed for decision 16; each fact refuted and its fallback; whether the tasks can show a difference; any key to revoke.

## 10. The report

Once a host, over that host's runs alone, with its provider's published rates for its model, in money per million tokens (uncached prompt, cached, output), typed on the command line, never committed: one set of rates prices every trial a report reads.

```
cd $P/eva-md && $P/bin/eva-eval continuation report --tasks $P/export/tasks --catalogue $P/eva-md/eval/continuation/metrics.json --rates <in>,<cached>,<out> $P/out/full-claude-code-off-1 $P/out/full-claude-code-oracle-1 $P/out/full-claude-code-eva-1
```

Then codex, with its own folders and rates. Add each rerun's folder of that host. Never the smoke's: the report refuses its `d01.t1` only where the full run judged that trial, and would otherwise count it twice. Never a killed run's, which has no record. A refused folder is named by its place among the arguments: exit 2, no figure.

**It prints** ids and figures by host, task and condition (EV-03c's card), counting the models the rows name without naming them.

**To the Stage 0 readout** (plan, EV-03 and 15.4) the lead brings `s` and `n` against the design's 0.6 and 60, tokens and minutes per session, a session's price with v1's memory beside it, and whether the tasks can show a difference; if no task passes its screen, the plan says so before Stage 2 leans on the suite (design 6.3). Then decision 16's ceiling, from the price.

## 11. Cleaning up

1. `cd /`; `pkill -KILL -u pilot-chat`; `find /tmp /var/tmp /dev/shm -xdev -user pilot-chat -delete`; `rm -rf ${P:?}/work ${P:?}/trace ${P:?}/export* ${P:?}/eva-md ${P:?}/bin`; `userdel pilot-chat`.
2. Keep `$P/out` and `$P/logs`: `tar -czf $P/pilot-out.tar.gz -C $P out logs` and its `sha256sum`, in the private file share the lead names, never in a checkout, commit, issue or message (rows name the model). Deleted when EV-17e deletes the pilot.
3. The owner revokes the keys or keeps them for EV-17. The container and its secrets are destroyed.

Nothing of a session's text or a trace enters the repository: only the facts' Results, `hosts.go`'s corrections and the readout's figures, with no model's name.

## 12. What this run-book does not cover

The live gates: [live-gates-runbook.md](live-gates-runbook.md). What only a Mac or a pty shows (F-070, F-071, F-116): [host-sitting-runbook.md](host-sitting-runbook.md). The pilot's commands and the report's rules: [eval/pilot/README.md](../../eval/pilot/README.md). Grading as another user, and the 28 tasks: [EV-17e](cards/EV-17e.md). The contract spike, a live run of its own: [eval/spike/run-plan.md](../../eval/spike/run-plan.md) and [DV-03d](cards/DV-03d.md). Decision 16 and the readout: [the plan](plan.md), 15.3 and 15.4. Releases: [releasing.md](../releasing.md).

**The lead's rulings (2026-11-14).** The sign-in program stands as given, and each guess it rests on is the smoke run's to prove. The wrapper and its clean-up at a session's first call stand. `d01` is the smoke's task. The reserve is a tenth and the margin twice the smoke's largest session, both the owner's to change. **Step 8 waits for two things.** No full run starts before the driver's owed round has merged (step 1), and before every chat's web tools are turned off in the host table: the pilot's hidden tests are in this public repository, and a host's web search runs at its provider, past the container's network, so a chat could find them. A round on the driver carries that, with what an eva trial whose chat ended unjudged spent, which is not counted today. The smoke run may go ahead of both: it judges nothing that is kept. The shape reader may also print a status's number, and whether a message holds 401, 403 or 429: a number is no one's text. A full run's trace is removed once its readings are confirmed, not kept for the report.
