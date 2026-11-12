# The live gates on v1: a run-book

**What it is for.** v1's update has never met a real model. Here it does, once, in the evaluation home: the doctor's live check, the accuracy suite on each host at two efforts, recorded, and the resume test with the update on. It is [the plan](plan.md)'s Stage 0 row "DV-02, EV-01", for EV-14d, DV-13, DV-11, DV-03 and section 8.

**What it costs.** 86 model calls by command, on your plans: 21 a host at each effort, and the doctor's 2. The resume test adds 8 to 10 a host. If things go wrong, at most 92 more: 21 a run done again, 2 for L-05 again, 3 a host for updates started by hand. A refused effort adds a short try in its pass. Calls past the plan's leave are yours to allow. On Codex the runs draw on the allowance the real install's updates use; in the counted week a rate limit here holds those back too, so tell the lead each run's day.

**How long.** 3 to 6 hours, mostly waiting; up to 12 if calls are slow.

**First.** The lead's commit (its first 12 characters go for `<commit>`) holds, merged: [HF-00](cards/HF-00.md) Part 1 (`hostrec scan`, `scrub`), [EV-14d](cards/EV-14d.md) (`--record`, `--replay`, `--pages`) and [DV-02a](cards/DV-02a.md) (`--trace`, `--timeout-refused`). Part 1 of [the host sittings](host-sitting-runbook.md) is handed over: `hfeval` has Go, both CLIs signed in, and `~/hf-src/eva-md`. Run this after that, before any Part 2 group and before E-05, which removes `hostrec`.

**Never.** Nothing touches your real install, its inbox or wiki, or your own `~/.claude` and `~/.codex`. The wiki here has a local folder as its remote; nothing is pushed. Every chat is made up.

**How to follow it.** Log in to `hfeval` (fast user switching), open a new Terminal window, and type each block. Each starts with `[ $USER = hfeval ] &&`, which stops it in any other account; if a block shows nothing, `echo $USER` must print `hfeval`. To tell the lead, send the step and the line shown. Keep the Mac on power, lid open. While a run goes you may switch accounts; never log out of `hfeval`.

### L-01. Build (5 min)

```
[ $USER = hfeval ] && cd ~/hf-src/eva-md && git fetch origin && git checkout --detach <commit> && git log -1 --format=%H
```
```
[ $USER = hfeval ] && cd ~/hf-src/eva-md && mkdir -p ~/gates/bin ~/gates/out && go build -o ~/gates/bin/eva-md ./cmd/eva-md && go build -o ~/gates/bin/live-accuracy ./cmd/live-accuracy && ls ~/gates/bin
```

**Expect:** an id that begins with the lead's 12 characters; then `eva-md  live-accuracy`.
**If not:** another id, or `go build` printing an error: stop and tell the lead.

### L-02. Each run's flags, with no call (1 min)

```
[ $USER = hfeval ] && for h in claude-code codex; do for e in highest low; do ~/gates/bin/live-accuracy --host $h --effort $e --timeout-refused; done; done
```

**Expect:** four lines "live-accuracy: the run makes 21 model calls on ... Nothing was called; ..." **If not:** stop and tell the lead.

`low` is DV-13's interim default, a level both hosts take. `highest` is v1's default: `max` on Claude Code, `ultra` on Codex, stepped down when a model refuses it.

### L-03. Folders for the resume test (3 min)

A made-up git identity, two scratch projects, and the wiki's local remote:

```
[ $USER = hfeval ] && git config --global user.name "Gate Eval" && git config --global user.email gate-eval@example.invalid && for p in resume-claude resume-codex; do mkdir ~/gates/$p && cd ~/gates/$p && git init -q && echo "# Export tool (made up)" > README.md && git add README.md && git commit -qm Start; done && cd ~/gates && mkdir -m 700 wiki.git && git init -q --bare wiki.git && ls
```

**Expect:** `bin  out  resume-claude  resume-codex  wiki.git`. **If not:** stop and tell the lead.

### L-04. Install eva-md here (10 min)

```
[ $USER = hfeval ] && ~/gates/bin/eva-md setup --dir ~/gates/wiki --remote ~/gates/wiki.git --yes
```

Press Enter at "Push it to ...? [Y/n]". Then `cd ~/gates && codex`, trust the folder, type `/hooks`, approve eva-md's hooks, `/quit`.

**Expect:** setup's ✓ lines (a `!` on commit signing is fine), ending "Run eva-md doctor --live to test ..."; Codex shows eva-md's hooks approved.
**If not:** a ✗ line: stop and tell the lead its first words.

### L-05. The doctor's live check (3 to 20 min, 2 calls)

First, as the cheapest: a failed sign-in or a refused flag shows here for 2 calls, not 21.

```
[ $USER = hfeval ] && ~/gates/bin/eva-md doctor --live | grep "live update:" | tee ~/gates/out/doctor-live.txt
```

**Expect:** "PASS  claude-code live update: valid output in ..." and the same for codex.
**If not:** `FAIL ... auth`: sign that host in again (`[ $USER = hfeval ] && claude auth login`, or the same with `codex login`) and repeat once. `rate_limit`: start here another day. Anything else: stop and tell the lead.

### L-06 to L-09. Four runs of 21 calls

| Step | Type first | Usually | At most |
| --- | --- | --- | --- |
| L-06 | `H=claude-code E=highest N=` | 30 to 90 min | 3 h 30 |
| L-07 | `H=codex E=highest N=` | 30 to 90 min | 3 h 30 |
| L-08 | `H=claude-code E=low N=` | 10 to 30 min | 1 h 05 |
| L-09 | `H=codex E=low N=` | 10 to 30 min | 1 h 45 |

Type the step's words, then:

```
[ $USER = hfeval ] && cd ~/gates/out && R=$H-$E$N && caffeinate -i ../bin/live-accuracy --host $H --effort $E --timeout-refused --spend-model-quota --record $R.jsonl --pages $R-pages --trace $R-trace 2>&1 | tee -a $R.txt
```

A pass prints a line as it starts and one as it ends; between them, 10 minutes of nothing is normal. Never press Ctrl-C. L-06 and L-07 may run at once in two windows, and so may L-08 and L-09.

**Expect:** lines "..., pass 1 of 2: asking ..." and "answered in 63.2 s, effort ..." (or "timeout after ...", counted as refused), figures after each script, and last "The run meets the target." or "The run misses the target." Both are results.
**If not:** a line ending "the run stopped before it could be judged", or beginning "live-accuracy: the run stopped:": see "When a run stops".

## The resume test, with the update on

A chat plants four made-up items, the update writes them to the wiki, and a new, a cleared and a compacted chat ask for them, host memory off.

| Item | Claude Code | Codex |
| --- | --- | --- |
| Codename | COPPER-WREN-58 | SLATE-FINCH-73 |
| Decision | CSV, not JSON | TSV, not XLSX |
| Open question | archived rows | deleted rows |
| Next step | docs/export.md | docs/columns.md |

One word an answer: `pass`, all four as planted; `partial`, some, none wrong; `misread`, any one otherwise (another codename or format, the question called settled, another file); `none`, no item, or no record of earlier work.

### R-01. Claude Code (25 to 40 min, 8 to 10 calls)

1. Start the chat, trusting the folder if asked:
   ```
   [ $USER = hfeval ] && H=claude-code && cd ~/gates/resume-claude && CLAUDE_CODE_DISABLE_AUTO_MEMORY=1 claude
   ```
   Type the message below as one; if a paste shows `[Pasted text` in place of the words, clear the box and type it. Refuse any tool the host asks for; if one ran anyway, end your line in item 4 with ` (tool used)`. After the reply, `/exit`.
   `We are planning the export command of this tool. The codename for this work is COPPER-WREN-58. I have decided that exports are written as CSV, not JSON, because the finance sheet opens only CSV. One question is still open: whether the export includes archived rows; we have not decided. The next step is to write the column list in docs/export.md. Do not read, create or change any file, and run no command. Reply in two sentences that restate all four.`
2. In the same window, every 3 minutes, up to 12, until it shows `"outcome":"ok"` or `"outcome":"no_change"`:
   ```
   [ $USER = hfeval ] && grep -h "\"host\":\"$H\"" ~/.local/state/eva-md/updates/*.jsonl | grep -oE '"outcome":"[a-z_]+"' | sort | uniq -c
   ```
   **If not** (`failed`, or none by 12 minutes; an error before then is normal): `[ $USER = hfeval ] && ~/gates/bin/eva-md update --pending --foreground`, wait for its `update:` line (up to 10 min; `stopped: busy` means a job still runs), and check again. At most three times; then write `update-failed` for all three words and skip item 3.
3. Start the chat as in item 1, refusing any tool. Type `Where did we stop?`. Then `/clear` and the question again. Then `/compact`, wait for it, and the question again. Then `/exit`.
4. In the same window, your three words, in order, for each `<word>`:
   ```
   [ $USER = hfeval ] && echo "$H new <word>, clear <word>, compact <word>" >> ~/gates/out/resume.txt
   ```

### R-02. Codex (25 to 40 min, 8 to 10 calls)

R-01 again, with `/quit` for `/exit` (and `/new` for `/clear` if this version has none), this block in item 1, and this message: `We are planning the export command of this tool. The codename for this work is SLATE-FINCH-73. I have decided that exports are written as TSV, not XLSX, because the import tool reads only TSV. One question is still open: whether the export includes deleted rows; we have not decided. The next step is to write the column list in docs/columns.md. Do not read, create or change any file, and run no command. Reply in two sentences that restate all four.`

```
[ $USER = hfeval ] && H=codex && cd ~/gates/resume-codex && codex --disable memories
```

**If not:** if Codex refuses `--disable memories`, start it with `codex` alone and end the line in `resume.txt` with ` (memory on)`.

### R-03. Keep what the update wrote (12 min)

Wait 10 minutes after R-02's last `/quit`, for the asking chats' updates. The wiki's settings name this Mac, so they are left out:

```
[ $USER = hfeval ] && mkdir ~/gates/out/resume && rsync -a --exclude .git --exclude .eva-md ~/gates/wiki/ ~/gates/out/resume/wiki/ && cp -R ~/.local/state/eva-md/updates ~/.local/state/eva-md/inbox ~/gates/out/resume/ && ls ~/gates/out/resume
```

**Expect:** `inbox  updates  wiki`.

### H-01. Check the folder (15 min)

```
[ $USER = hfeval ] && cd ~/gates/out && for r in claude-code-highest codex-highest claude-code-low codex-low; do for f in $r.jsonl $r-2.jsonl; do [ -f $f ] && ../bin/live-accuracy --host ${r%-*} --effort ${r##*-} --timeout-refused --replay $f 2>&1 | tail -1; done; done
```

**Expect:** for each recording, with no call, the last line its run printed (a stopped run's is refused). Then `setopt HIST_IGNORE_SPACE`, so lines starting with a space stay out of the history, and:

```
[ $USER = hfeval ] && hostrec scan ~/gates/out
```

It asks for your email, name and other words, and stores none. **Expect:** hits on `hfeval`, `/Users/hfeval` and this Mac's name. Scrub each, the line starting with a space: ` hostrec scrub ~/gates/out "/Users/hfeval"`, then `hfeval`, the Mac's name as scan shows it, and any other hit. Scan again until "hits to judge: none".

Model names, a stopped run's raw output (it can name your plan, and your limit's reset in your time zone) and the run log's times stay in the archive, for the lead to drop.

### H-02. Take eva-md down (10 min)

```
[ $USER = hfeval ] && ~/gates/bin/eva-md uninstall
```

**Expect:** its last lines name `/Users/hfeval/gates/wiki`; any other folder: stop. Then:

```
[ $USER = hfeval ] && ~/gates/bin/eva-md uninstall --apply --all
```

Type `delete` when it asks. Then:

```
[ $USER = hfeval ] && { rm -rf ~/gates/wiki ~/gates/wiki.git ~/gates/resume-claude ~/gates/resume-codex; git config --global --unset user.name; git config --global --unset user.email; }
```

**Expect:** uninstall lists what it removed. **If not:** "try again shortly" means an update still runs: wait 10 minutes and repeat.

Before any Part 2 group, tell the lead: Codex keeps its approval of eva-md's hooks, the gate chats stay, and whether `resume.txt` says `(memory on)`.

### H-03. Pack and hand over (5 min)

```
[ $USER = hfeval ] && cd ~/gates && tar -czf /Users/Shared/gates-out.tar.gz out && shasum -a 256 /Users/Shared/gates-out.tar.gz > /Users/Shared/gates-out.tar.gz.sha256
```

In your own account, upload both files to the host sittings' private file share and tell the lead; never by a commit, an issue or an open message. Once the lead confirms the checksum, in `hfeval`: `[ $USER = hfeval ] && rm /Users/Shared/gates-out.tar.gz* && rm -rf ~/gates`.

## When a run stops

Calls spent: `grep -c ": asking " ~/gates/out/<name>.txt`. Its files stay, with the failed pass DV-11 needs. A run cannot resume: each pass builds on the last. Run it again at most once, its step's words ending `N=-2` (21 calls):

| The line names | Do |
| --- | --- |
| `auth` | Sign that host in again, as in L-05; run it again |
| `rate_limit` | Skip that host's later steps today, its resume test too; do them another day, from L-05, the stopped run's words ending `N=-2` |
| `host_error`, or "the run stopped:" (a Ctrl-C or a sleep too) | Check the network; run it again |
| anything else, or a second stop | Tell the lead; go on with the next run |

## What the lead does with it

The archive stays out of every checkout. Committed: whole runs' recordings and pages as fixtures, the model's name made a tier word (a stopped run's pages are never scored); `eva-eval fidelity`'s rows; tokens and seconds a pass; raw host output only with every value off HF-00's vocabulary replaced, as `hostrec fixtures` does (ids, names, times, zones, usage, cost and limit figures), under `testdata/<host>/<cli version>/headless/`; the doctor's lines and resume words. Never: the archive, run log or inbox, a path, a session id, a model's or plan's name, a time of day, a rate-limit message.

| Reading | What it moves |
| --- | --- |
| Refused whole, timeouts apart: 30% or more | The new contract first in Stage 2, at full size |
| Under 10%, and 0.9 of planted facts held on EV-14 | The contract shrinks to exact spans; recall moves ahead |
| Highest effort: median over 120 s a pass, or any timeout | Decision 5 confirmed on fixed scripts (DV-13) |
| Low runs' score against the highest's | DV-13's low default after the counted week; DV-03 |
| A resume word `misread` or `none` | The typed brief first in Stage 2 |
| A stop or a doctor FAIL, by reason | DV-11's fixtures; a refused flag is fixed in v1 first |
