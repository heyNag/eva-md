# The pilot's live run: a run-book

> **A draft, not yet to be followed** (the lead, 2026-11-14). It was written by one agent and checked against the code by another while three of the packages it rests on were still being built: the v1 condition (EV-17c), the report (EV-03c) and a second round on the driver that keeps each call's raw output (EV-17g). Every line marked with one of those cards is read again against the merged code before the pilot runs, and three rulings made since are written in then: v1's update signs in through a program given as `--sign-in`, which this run-book must supply and the smoke run must prove (EV-17c's card says why); the driver's own trace takes the place of the by-hand call in the smoke run (every smoke and full run gets `--trace` with a new folder outside the work folder, the results, the tasks, the repositories and every checkout; call 1 of a trial is the version probe and the rest are the chat's; the wrapper passes the real CLI's two streams through unchanged; a trace is removed once the run's readings are confirmed and is never committed or quoted); and a hang-up now ends a run as an interrupt does. What only the owner can answer is listed in [leading-the-build.md](leading-the-build.md) under "With the owner".

**What it is for.** The first real figures of an agent session: its cost, its variance, and whether the tasks can show a difference at all. It is [the plan](plan.md)'s Stage 0 row EV-03, as ruled on 2026-11-13 ([the cards' index](cards/README.md)): the ten pilot tasks, exported, on the continuation driver under memory off, the ideal record and eva-md v1, on both hosts, in a container billed to evaluation keys (decision 28). A session's price sets decision 16's ceiling (15.4).

**Who runs it.** The lead, never a builder.

**How to follow it.** Every block runs as root in the container, in one shell, in order. In a new shell, or after a restart, first set `P=/srv/pilot`, `export DISABLE_AUTOUPDATER=1` and the step's own variables; a removal written `${P:?}` removes nothing while `P` is unset. **(EV-17g)**, **(EV-17c)** or **(EV-03c)** beside a flag or a name means a card not yet merged fixes it: check it against the merged code first.

## 1. Before anything starts

If one is not true, stop.

1. **The owner's leave, in so many words**, for the pilot's live run, with a ceiling in sessions and one in tokens for the pilot itself. The build's standing leave (`AGENTS.md`) covers no live run.
2. **The price in money**, stated by the lead before the leave (decision 28): about 180 sessions of 0.3 to 0.7 million tokens, 80 to 90% cached, at the published prices for the model the owner names.
3. **Decision 28's two keys**, one per vendor, for evaluation only (step 2).
4. **Merged on `main`**: EV-03a, EV-03b, EV-17a, EV-17b, EV-17d (done); [EV-17g](cards/EV-17g.md) and [EV-17c](cards/EV-17c.md) before the smoke run; [EV-03c](cards/EV-03c.md) before step 10. `<commit>` is the first 12 characters of the commit built.
5. **The lead's rulings** listed at the end of step 12 are made.

## 2. The keys, and how one reaches a chat

- The owner puts the two keys in the container platform's secret store, never in a message, an issue, a commit or a file. Where a vendor lets a key or its project carry a spending limit, the owner sets it at the ceiling's price: the one bound no fault here can pass.
- Each is a variable of root's environment: `ANTHROPIC_API_KEY` for Claude Code; for Codex the one its documents name for `codex exec` at the installed version (`CODEX_API_KEY` when this was written). Where the container's proxy adds the key on its way, the variable holds a stand-in.
- The driver gets only the name, by `--pass-env`, and passes the value only into the trial: the chat, the version probe and the v1 condition's commands. No key is a flag, or in a file the repository holds, a row, a log or this run-book; a passed value of 8 letters and digits or more is a word no row may hold. Check one with `[ -n "$ANTHROPIC_API_KEY" ] && echo set`, never `env`.
- A proxy's variables (`HTTPS_PROXY`, the CA file's) are passed too, by adding their `--pass-env` to every command of steps 7 and 8; the CA file lives outside `/root`, since the driver refuses a value that reaches root's home.

## 3. The container

**What it holds.** Linux with util-linux (`setpriv`), procps, GNU coreutils and findutils; git 2.44 or newer; Go 1.26 or newer (`go.mod`: `go 1.26.0`; grading runs go with `GOTOOLCHAIN=local`, `GOPROXY=off`); `python3`; each host's CLI, by its own installer, in `/opt/pilot/hosts/<host>/`, readable and runnable by both users (0755), at one version for the whole pilot (the wrapper's two paths follow where the installers put them). Network: the two providers' API hosts; GitHub, Go's module proxy and the installers' hosts only for this step and step 4.

**Two users.** root runs the driver, grades and removes. `pilot-chat` runs the chats: no home, no password, no sudo.

```
useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin pilot-chat
P=/srv/pilot; install -d -m 0711 $P $P/work; install -d -m 0700 $P/out $P/logs; install -d -m 0755 $P/bin
git clone https://github.com/heyNag/eva-md $P/eva-md && chmod 700 $P/eva-md
export DISABLE_AUTOUPDATER=1
```

`/srv/pilot` is on a volume that outlives the container.

| Path | Mode (root's) | Holds | `pilot-chat` |
| --- | --- | --- | --- |
| `$P`, `$P/work` | 0711 | all below; a work folder an attempt | passes through, cannot list |
| `$P/eva-md`, `$P/out`, `$P/logs` | 0700 | the checkout; a results folder an attempt; what runs printed | no |
| `$P/export*` | 0700 (the export's) | tasks with hidden tests, repositories | no |
| `$P/bin` | 0755 | `eva-md` (0755), `eva-eval` (0700), `as-chat` (0755) | runs `eva-md`, `as-chat` |

In a trial, `pilot-chat` reaches its own `home`, `tmp` and `workspace`; `grade/` is root's alone.

**The wrapper**, `$P/bin/as-chat`, given as `--cli`. The driver links it into each trial's `bin/` under the CLI's name and starts each chat in the trial's workspace with `HOME` the trial's home. For a chat it hands the trial's home, temporary folder and workspace to `pilot-chat` and starts the real CLI as that user (EV-17g's rulings); any other call runs as its caller: the version probe (run in the home), and v1's setup and update unless EV-17c runs them in the workspace **(EV-17c)**.

```sh
#!/bin/sh
set -eu  # stop at the first failure
case $(basename "$0") in  # the driver's link names the CLI
claude) real=/opt/pilot/hosts/claude/bin/claude ;;  # by absolute path: PATH starts with this file's link
codex) real=/opt/pilot/hosts/codex/bin/codex ;;  # likewise
*) echo "as-chat: unknown CLI" >&2; exit 97 ;;  # any other name is a set-up fault
esac  # end of the host's choice
user=pilot-chat  # the chat's user
trial=${HOME%/home}  # the driver gives each chat HOME=<trial>/home
[ "$(id -u)" = 0 ] && [ "$(pwd -P)" = "$trial/workspace" ] || exec "$real" "$@"  # no chat call: run as the caller
case $trial in /srv/pilot/work/*/*) ;; *) exit 97 ;; esac  # hand over nothing outside a trial
fresh=yes  # a session's first call, until a resume is seen
for a in "$@"; do [ "$a" = -- ] && break; case $a in --resume|resume) fresh=no ;; esac; done  # flags before the prompt
[ $fresh = no ] || pkill -KILL -u $user || true  # a new session: end what an earlier one left running
[ $fresh = no ] || find /tmp /var/tmp /dev/shm -xdev -user $user -delete 2>/dev/null || true  # and its shared temporary files
chown -hRP $user:$user "$trial/home" "$trial/tmp" "$trial/workspace"  # hand them over; links never followed
exec setpriv --reuid=$user --regid=$user --init-groups --no-new-privs -- "$real" "$@"  # same environment and process group
```

`install -m 0755 as-chat $P/bin/`. Runs go one at a time, since the wrapper ends every process of `pilot-chat` at a session's first call. `cat /proc/sys/fs/protected_hardlinks` must print `1`: otherwise a chat could hard-link a root file it can name, such as `$P/bin/as-chat`, into its workspace, and the next `chown -R` would give it that file.

**What it cannot protect against.** Grading runs the chat's code as root (EV-17e moves it): it can read every hidden test and any process's environment, keys included, and write anywhere; the driver finds a hidden test copied whole or by path, not reworded. The chat holds its key and can send it wherever the network allows. A process that leaves its group lives until the next session's first call or step 8's check; a file outside the temporary folders stays until that check. It reads what is readable to all, and owns its home's files, Codex's managed hooks file among them.

## 4. Building what runs

```
cd $P/eva-md && git fetch origin && git checkout --detach <commit> && git status --short && git log -1 --format=%H
go telemetry off
go build -mod=readonly -tags evamd_eval -o $P/bin/eva-md ./cmd/eva-md && chmod 755 $P/bin/eva-md
go build -mod=readonly -o $P/bin/eva-eval ./cmd/eva-eval && chmod 700 $P/bin/eva-eval
go version -m $P/bin/eva-md | grep -e -tags
```

**Expect:** nothing from `git status` (rows record a dirty checkout), an id beginning `<commit>`, then `build	-tags=evamd_eval`.

The tag builds `cmd/eva-md/evalbuild.go` (EV-17d): setup runs on Linux, and a hook with `EVA_MD_EVAL_ACTOR=person` takes a headless Claude Code chat for a person's. No release carries it: no workflow passes the tag, `evalbuild_test.go` holds that, and EV-17f, once merged, checks each published binary. The v1 condition refuses an `eva-md` without it **(EV-17c)**.

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

180 sessions, as the plan says; 20 minutes is the export's line (the driver's default is 30). **What the driver counts.** A session is one trial: its first prompt and every follow-up round, in one host session. Tokens are every call's prompt tokens (cached ones in full) and output tokens, and under `eva` what v1's update spent. `--max-sessions` refuses a plan of more sessions. `--max-tokens` is checked before each trial, so a run can pass it by up to one session, which only its turns, rounds and `--session-timeout` bound.

**Splitting the owner's ceiling**, once step 7 is done, from what it spent: a smoke run's `--max-tokens` cannot bound its one session. The figures are made up, for the arithmetic only. Round every share down, to whole sessions and tokens. What all runs are given never sums past the owner's ceiling, and what a run leaves unspent goes to no other.

| | Sessions | Tokens |
| --- | --- | --- |
| The owner's ceiling | 240 | 120,000,000 |
| Smoke (step 7) as spent: its records' `tokens`, reruns and by-hand calls included | 8 | 9,000,000 |
| Left | 232 | 111,000,000 |
| Reserve for reruns: a tenth, rounded down | 23 | 11,100,000 |
| For the six runs | 209 | 99,900,000 |
| A run's share: a sixth, rounded down | 34 | 16,650,000 |
| Margin for one session past the threshold: twice the smoke's largest (say 900,000) | | 1,800,000 |
| `--max-sessions` and `--max-tokens` of each run | 34 | 14,850,000 |

Then check a run fits: 30 sessions at the smoke's median (say 400,000), plus under `eva` its memory tokens (say 60,000): 30 × 460,000 = 13,800,000, under 14,850,000. If one does not fit, stop and ask the owner for a higher ceiling or fewer trials.

**The plan line**, printed before anything starts:

```
plan: condition off, host claude-code, tasks 10, trials 3 each, sessions 30; ceilings: sessions 34, tokens 14850000; trial folders removed
```

`sessions` is tasks × trials, refused over its ceiling; `tokens` is the threshold; with `--keep-trials` the end reads `kept, each closed to others once it ends` **(EV-17g)**. Without `--spend-model-quota` it prints `eva-eval: nothing started: the run needs --spend-model-quota to start a chat` and exits 2.

## 7. The smoke run

One task, d01 (a hidden decision, which under memory off should need a correction, so a session is continued), one trial, per host and condition, folders kept. In order, one at a time: claude-code off, oracle, eva; then codex off, oracle, eva. Set:

- `M` is the exact id the host reports, not an alias: the driver takes only lower-case letters, digits, `.`, `_`, `-`, and a session naming another model is unjudged `other_model`.
- Claude Code: `H=claude-code M=<the model the owner names for it> PASS="--pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER"`
- Codex: `H=codex M=<the model the owner names for it> PASS="--pass-env CODEX_API_KEY"`
- `C=off E=`, `C=oracle E=`, or `C=eva E="--eva-md $P/bin/eva-md"`

```
X=$P/export-smoke R=smoke-$H-$C-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host $H --cli $P/bin/as-chat --condition $C --trials 1 --work $P/work/$R --out $P/out/$R --max-sessions 1 --max-tokens 1500000 --model $M --session-timeout 20m $PASS $E --repo $P/eva-md --keep-trials --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"; cat $P/logs/$R.txt
```

Run it first without its last flag: the plan line, then exit 2 (step 6). Then as written. **Expect:** `run <id>`; `trial d01.t1: solved ..., rounds ...`; `run <id>: planned 1, judged 1, ..., hidden_found 0; tokens ..., rows ...`; `exit 0`.

**The sign-in check**, once per host, at no cost: the off command again, `R=smoke-$H-signin-1`, with the key's variable set to a made-up wrong value for that command only (`ANTHROPIC_API_KEY=made-up-wrong-0000 $P/bin/eva-eval ...`). **Expect:** `trial d01.t1: unjudged, sign_in`, `exit 1`. Skip it where a proxy adds the key.

**What to look at.** Kept trials are under `$P/work/smoke-*`, closed to all but root.

1. **`memory_off`** (eva) **(EV-17g, EV-17c)**: must be 0. On Claude Code a 1 means the plugin's hooks did not fire under the key (F-115) or the actor seam failed; on Codex, no managed hooks read from `<home>/.codex/requirements.toml` (F-163). The lead rules before that host's v1 run, and asks the owner if no route works in a container (decision 28's fallback).
2. **The plugin loaded twice** (Claude Code eva): `find $P/work/smoke-claude-code-eva-1 -path '*eva-md/inbox/*' -name '*.jsonl' -exec grep -c '"kind":"prompt"' {} +` counts prompts a session file; the new chat's equals its rounds. Twice that: setup's registration and `--plugin-dir` both load it; the lead rules a change to `hosts.go`'s plugin entry.
3. **Codex's managed hooks** (Codex eva): as item 1, and the kept inbox holds the new chat's session with events.
4. **No usage** **(EV-17g)**: `no_usage 0`. A 1 stopped the run: the reader missed the usage, or the CLI changed. The driver keeps no raw output: as `pilot-chat`, in a scratch folder under `$P/work`, make one short call with `hosts.go`'s flags, output to a root-only file; read event types and field names only; add its tokens to the smoke's; remove both. Then a fix (step 9).
5. **The model**: the summary's `no model`, and `grep -o '"model":"[^"]*"' $P/out/$R/results.jsonl | sort | uniq -c`. A session naming no model is judged, its rows carry none. `other_model`, a session that named another: check `--model` against the id the host names, then the reader. No full run starts while a smoke shows one.
6. **Sign-in and rate-limit readings**: the sign-in check reads `sign_in`; a rate limit met later must read `rate_limited`, and `host_failed` there means the reader is wrong.
7. **v1's update** (eva): `update_failed 0`, `setup_failed 0`. v1's update passes its CLI no key variable, so until the lead rules how it signs in (step 12), `update_failed` is likely.
8. **What `pilot-chat` could reach.** `setpriv --reuid=pilot-chat --regid=pilot-chat --init-groups -- sh -c "ls $P/export $P/out $P/eva-md /root; cat $P/export/tasks/d01/task.json"`: `Permission denied` each time. After every run, step 8's check prints nothing. `hidden_found 0` in every summary.
9. **Telemetry, record** **(EV-17g)**: each kept trial's `home/.config/go/telemetry/mode` reads `off`; `runs.jsonl` holds one line, `"ended":"done"`, one version in `host_versions`.
10. **Figures**: the rows' `prompt_tokens`, `output_tokens`, `wall_ms`, and under `eva` `memory_tokens`, for step 6's margin and fit check.

**When something is wrong**, the lead rules; a fix to `hosts.go` (step 9) is followed by that host's smoke again, ending `-2`. Ask the owner when a host cannot run on a key alone (F-067 refuted), a key may have leaked, or the runs cannot fit the ceiling. Then `rm -rf ${P:?}/work/smoke-*`.

## 8. The full runs

Memory off and the ideal record first, since they need no eva-md; v1 last, on a host only once its `eva` smoke judged d01.t1 (`memory_off 0`, `update_failed 0`) or the lead has ruled. One at a time, in `tmux`: a dropped connection kills a run as a restart does. `S` and `T` are step 6's ceilings; `M_CC` and `M_CX` the models' ids; `X=$P/export`. Run each first without its last flag and read the plan line. A run takes about 2 to 6 hours, at most about 12.

```
R=full-claude-code-off-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host claude-code --cli $P/bin/as-chat --condition off --trials 3 --work $P/work/$R --out $P/out/$R --max-sessions $S --max-tokens $T --model $M_CC --session-timeout 20m --pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-codex-off-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host codex --cli $P/bin/as-chat --condition off --trials 3 --work $P/work/$R --out $P/out/$R --max-sessions $S --max-tokens $T --model $M_CX --session-timeout 20m --pass-env CODEX_API_KEY --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-claude-code-oracle-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host claude-code --cli $P/bin/as-chat --condition oracle --trials 3 --work $P/work/$R --out $P/out/$R --max-sessions $S --max-tokens $T --model $M_CC --session-timeout 20m --pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-codex-oracle-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host codex --cli $P/bin/as-chat --condition oracle --trials 3 --work $P/work/$R --out $P/out/$R --max-sessions $S --max-tokens $T --model $M_CX --session-timeout 20m --pass-env CODEX_API_KEY --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-claude-code-eva-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host claude-code --cli $P/bin/as-chat --condition eva --trials 3 --work $P/work/$R --out $P/out/$R --max-sessions $S --max-tokens $T --model $M_CC --session-timeout 20m --pass-env ANTHROPIC_API_KEY --pass-env DISABLE_AUTOUPDATER --eva-md $P/bin/eva-md --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
R=full-codex-eva-1; $P/bin/eva-eval continuation run --tasks $X/tasks --repos $X/repos --host codex --cli $P/bin/as-chat --condition eva --trials 3 --work $P/work/$R --out $P/out/$R --max-sessions $S --max-tokens $T --model $M_CX --session-timeout 20m --pass-env CODEX_API_KEY --eva-md $P/bin/eva-md --repo $P/eva-md --spend-model-quota > $P/logs/$R.txt 2>&1; echo "exit $?"
```

Follow one with `tail -f $P/logs/$R.txt`. **Expect:** a line a trial, then the summary; `exit 0` when every trial was judged, 1 when one was unjudged or not run, 2 when nothing started or the run stopped.

**After each run**, the smoke's too, since no next session ends the last trial's leftovers, and a job left running spends what no ceiling counts: `pkill -KILL -u pilot-chat; ps -u pilot-chat; find / /tmp /var/tmp /dev/shm /run $P -xdev -user pilot-chat -not -path "$P/work/*" 2>/dev/null`. Only `ps`'s heading prints; anything found is removed and told to the lead first.

**A fresh `--out` for each attempt**, its name ending `-2`, `-3`. The driver refuses an `--out` holding a row or record of the same host, condition and task, and a `--work` that is not empty **(EV-17g)**.

**A rerun** takes only tasks with no judged trial, so none counts twice: `$P/bin/eva-eval pilot export --tasks $T0 --out $P/export-rerun-<n> --task <id>...`, then the run's command with that folder as `X`, a `-2` name, and ceilings from the reserve: `--max-sessions` 3 a task, `--max-tokens` the part of the reserve it takes less step 6's margin. When the reserve is spent, ask the owner.

**When a run stops.** To stop one, press Ctrl-C once, or `pkill -INT -x eva-eval` from another shell, and wait: it ends the call, removes the trial's folder and writes the record, ended `error`. Never `-KILL`: that leaves no record.

| It shows | Do |
| --- | --- |
| `the run stopped: a trial is unjudged, no_usage` **(EV-17g)** | Step 7 item 4; then rerun the tasks with no judged trial |
| `... memory_off` **(EV-17g)** | Step 7 item 1; the lead rules |
| `... hidden_found` **(EV-17g)** | A fault of the set-up: no further run until the lead finds it (the wrapper, a folder's mode, a task) |
| Ended `error`, or an `eva-eval:` line | Tell the lead; the line names no path |
| `unjudged, sign_in` | Ctrl-C; check the key (step 2); a revoked one: ask the owner |
| `unjudged, other_model` | Ctrl-C at the first: every further trial would be the same; check `--model` (step 7 item 5) |
| `unjudged, rate_limited` or `host_failed` | Rerun those tasks another day. Half the trials or more: Ctrl-C, wait a day |
| `unjudged, timeout` | Many: tell the lead, who alone changes `--session-timeout` |
| `unjudged, update_failed` or `setup_failed` | Three in a row: Ctrl-C, every further trial would be the same; the lead rules |
| `not run, --max-tokens reached` | The share is spent: the reserve, or the owner |
| A restart, or a killed process | No record. Its `--out` is never used again nor read by the report; count its whole share as spent; `pkill -KILL -u pilot-chat`, then `rm -rf ${P:?}/work/${R:?}`; rerun all its tasks |

**Never:** reuse an `--out`; raise a ceiling, or move a share, without the owner; run a host outside the container; run two runs at once; change `hosts.go` while a run goes; edit a task; copy a trial's folder, session files, inbox or wiki out of the container; show or type a key.

## 9. Recording what was learned

**Host facts.** In [host-facts.md](host-facts.md) the pilot settles, by host: F-067 (judged trials on a key alone, the spend on the key's page), F-068 (`agent_events`), F-069 (corrections, resumed sessions), F-115 and F-163 (`memory_off`), F-207 (tokens, cached share and minutes a session). Not F-070 to F-072, F-116, F-117, F-169 (EV-03c). Split each **Result.** line into **Result, Claude Code.** and **Result, Codex.**, as F-018's are: confirmed, refuted or not settled; the date; the CLI's version from `host_versions`; if refuted, the fallback. Never a model's name, a key, a price, an account, or the container's provider.

**The host table.** A correction of how a host is driven (flags, the reader, a sign-in or rate-limit reading, the plugin entry) changes `internal/eval/continuation/drive/hosts.go` alone, with its tests and the fake's shape, on a builder's branch under the lead's order, merged before the next run; then rebuild `eva-eval` (step 4). The report shows the rows' two commits.

**To the owner**, at the readout: what was spent against the ceiling; a session's price in money; the ceiling proposed for decision 16; each fact refuted and its fallback; whether the tasks can show a difference; any key to revoke.

## 10. The report

Per host, with its model's published rates in money per million tokens (uncached prompt, cached, output), typed on the command line only, never committed **(EV-03c: the command, flags and output)**:

```
cd $P/eva-md && $P/bin/eva-eval continuation report --tasks $P/export/tasks --catalogue $P/eva-md/eval/continuation/metrics.json --rates <in>,<cached>,<out> $P/out/full-claude-code-off-1 $P/out/full-claude-code-oracle-1 $P/out/full-claude-code-eva-1
```

Then codex, with its rates. Add each rerun's folder. Never the smoke's, whose `d01.t1` would count twice, nor a killed run's, which has no record.

**It prints** ids and figures (EV-03c items 4 to 8): the records summed per host and condition; per task and condition, solved, first try, and medians of corrections, rounds, agent events, tokens and minutes; a session's median cost and price; `s` and `n` of `eva` and `oracle` against `off`; each task's screen.

**To the Stage 0 readout** (plan, EV-03 and 15.4) the lead brings: the variance, `s` and `n`, against the design's 0.6 and 60; tokens and minutes per session; a session's price, v1's memory beside it; whether the tasks can show a difference at all. If no task passes its screen, the plan says so before Stage 2 leans on the suite (design 6.3). Then decision 16's ceiling, from the price.

## 11. Cleaning up

1. `pkill -KILL -u pilot-chat`; `find /tmp /var/tmp /dev/shm -xdev -user pilot-chat -delete`; `rm -rf ${P:?}/work ${P:?}/export* ${P:?}/eva-md ${P:?}/bin`; `userdel pilot-chat`.
2. Keep `$P/out` and `$P/logs`: `tar -czf pilot-out.tar.gz -C $P out logs` and its `sha256sum`, in the private file share the lead names, never in a checkout, commit, issue or message (rows name the model). Deleted when EV-17e deletes the pilot.
3. The owner revokes the keys or keeps them for EV-17. The container and its secrets are destroyed.

Nothing of a session's text enters the repository. What does: the facts' Results, `hosts.go`'s corrections, and the readout's figures, with no model's name.

## 12. What this run-book does not cover

The live gates: [live-gates-runbook.md](live-gates-runbook.md). What only a Mac or a pty shows (F-070, F-071, F-116): [host-sitting-runbook.md](host-sitting-runbook.md). The pilot's commands alone: `eval/pilot/README.md` **(EV-03c)**. Grading as another user, and the 28 tasks: [EV-17e](cards/EV-17e.md). The contract spike: [DV-03d](cards/DV-03d.md). Decision 16 and the readout: [the plan](plan.md), 15.3 and 15.4. Releases: [releasing.md](../releasing.md).

**The lead's rulings before the smoke run:** how v1's update and its jobs sign in, since they pass their CLI no key variable; this wrapper and its clean-up at a session's first call; d01 as the smoke's task; the ceiling's reserve and margin; the by-hand shape call of step 7 item 4.
