# Host shapes prompt, a companion to the second field report

**Status:** for the counters release (RELEASE-NUMBER) or later. A read-only look, on the one real install (a Mac whose wiki is on a self-hosted GitLab), at the shapes of eva-md's records and of the hosts' own session files. It settles the facts that [host-facts.md](host-facts.md) marks "by the field report" and that [the second report](field-report-2-prompt.md) has no room for, so no sitting is needed for them. Its message: [host-shapes-message.md](host-shapes-message.md). Checked on 2026-11-11: its two scripts were run on made-up Codex files, on Claude Code files of a test machine and in a temporary folder, and the eva-md commands it names on a build of the release's code in a made-up home.

Run it on the same Mac, from the same folder, after the second report or on another day. Run it after at least a day of normal use since the upgrade. Expect 30 to 60 minutes of the agent's work.

## How to use it

1. Open a new Codex chat in the desktop app, in the git repository you used for the second report, from your normal home.
2. Attach this file with its message, or paste the part between the lines of tildes as your first message.
3. Approve only commands the prompt names. Refuse any other eva-md command (as doctor, update, commit, maintain, index, inbox), `codex exec` or `claude -p`, web search or fetch, any network command (curl, ssh, git fetch, pull, push or ls-remote), and any write outside the temporary folder and the report file. Deny any request for access to a folder.
4. It writes `eva-host-shapes.md` in the chat's folder (`eva-host-shapes-run2.md` if that is there). You are the last check: before you share it, strike any name, path, title, quote, model or service name. Answer its last section a line each, in general terms, and bring the file back.

## What to know before you run it

- **The chat is captured like any other.** The report leaves this chat out of its counts.
- **What the agent reads.** By scripts that print only counts, sizes in buckets, dates, yes or no and the names of the hosts' own fields: eva-md's inbox, heartbeats, recovery, run and commit records; the hosts' session files; Claude Code's memory folder for this folder (sizes only); one setting of Codex's config; the wiki's remote (only `ssh` or `https`). It reads no text of any chat, page or file. It runs on this chat's model provider.
- **What may be written.** eva-md's commands named here write nothing. The agent: scripts, and one git check with made-up files and a throwaway key, in a folder it makes with `mktemp -d` and deletes at the end; the report in the chat's folder.

~~~
I need a shapes report on eva-md, the memory layer installed on this Mac for Codex and Claude Code. A team is rebuilding it and must learn how the hosts' own records are built: which row types exist, which fields a row carries, how rows follow each other, and how eva-md's records line up with them. They need shapes only: counts, the names of the hosts' own fields, and yes or no. They must get nothing a chat, page or file says, and nothing that names a person, project, folder or machine.

## What eva-md is

Hooks in Codex and Claude Code record each chat into a local inbox, secrets removed, and give a new chat a short brief. A background job turns chats into a wiki, a git repository that a daily job, started from a hook, commits and pushes.

## Where things are

- The program: `~/.local/share/eva-md/bin/eva-md`, maybe on PATH as `eva-md`; its modification time is the upgrade's. Config: `~/.config/eva-md/config.toml`: `wiki_path` names the wiki, `exclude` lists folders left out of capture.
- State, `~/.local/state/eva-md/`:
  - `inbox/<project>/<host>-<session>.jsonl`: entries with `ts`, `host`, `session`, `turn`, `role`, `kind`, `agent`, `origin` (`hook` or `catchup`), `cwd`, `text`. An event is kind `event` with a fixed text: `session_start: <source>`, `session_end`, `compact`, `gap: missing final reply`, `off_record`, `on_record`, `host_prompt`, `stop_failure`. Kind `host_summary` is a compaction's summary.
  - `heartbeats/<host>/<event>.json`: `runs`, `week`, `transcript_available` (the last payload named the host's file), and on Claude Code `entrypoint` and `headless`. `heartbeats/<host>/uncaptured.json`: the time of the last hook run that captured nothing.
  - `updates/<date>.jsonl`: the background job's passes: `at`, `host`, `outcome`, `calls`, `duration_ms`, `trigger`.
  - `commits/<YYYY-MM>.jsonl`: each daily commit and push: `at`, `outcome`, `reason`, `push` (`ok`, `up_to_date`, `pending`, `diverged`, `failed`, `skipped`).
- Claude Code: `~/.claude/projects/<folder>/<session>.jsonl`, a subagent's in `<session>/subagents/`, memory in `<folder>/memory/`. As eva-md's code reads a row: `type`, `sessionId`, `timestamp`, `cwd`, `gitBranch`, `promptId`, `isMeta`, `isSidechain`, `agentId`, `isCompactSummary`, `entrypoint`, `version`, and `message` with `role`, `model`, `stop_reason` and `content` (text, or items with a `type` such as `text`, `tool_use`, `tool_result`).
- Codex: `~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-...-<session>.jsonl` and `~/.codex/archived_sessions/`; `~/.codex/config.toml`. As eva-md's code reads a row: `timestamp`, `type` (`session_meta`, `turn_context`, `event_msg`, `response_item`, `compacted`) and `payload` with its own `type`: `task_started` or `turn_started` (with `turn_id`), `user_message`, `agent_message` (`phase`), `task_complete`, `turn_aborted`, `context_compacted`, `message`, `function_call`, `custom_tool_call`, `local_shell_call`. `session_meta`'s payload: `id`, `cwd`, `source`, `originator`, `cli_version`.
- These lists are what eva-md knows, not all there is. Find the rest by counting names under rule 4, never by printing a row.

## Rules

1. **Read only.** You may run: `eva-md version`; `eva-md audit capture` with no flag; `eva-md status --updates`; `codex --version` and `claude --version`; `defaults read` of a desktop app's `Info.plist`; `command -v`; `xcode-select -p`; `gh --version` and `glab --version`, nothing else of either; `git --version`; the git check of section 0 (git and `ssh-keygen`), only inside your folder; `git -C <wiki> --no-optional-locks config --get remote.origin.url`, into a script that prints only its scheme; `stat`, `find`, `ls` and `wc` into a script; `python3` and the shell; `mktemp -d` and `rm -r` of that folder. Nothing else: no other eva-md command, no `codex exec`, `claude -p` or the like, no host command that changes anything, no web search or fetch, no network command, no subagent. Change no file or setting. Deny any request for access to a folder. Fix nothing: a failure is a finding.
2. **eva-md's reports first.** Run section 1's commands before any script, and copy their numbers as printed. Script only what they do not give.
3. **Shapes only.** A script may open eva-md's files, the hosts' session files and the few other files a section names, and may print only: counts; shares; durations and gaps in time, in the buckets this file names; sizes in bytes, in buckets (under 1 KB, 1 to 8 KB, 8 to 64 KB, 64 KB to 1 MB, over 1 MB); yes or no; dates as days; version numbers of eva-md, the hosts, `gh` and `glab`, and git's only as at or above 2.36 or below; and names of the hosts' own schema, under rule 4. It never prints a value of any field, whole or in part, hashed or not: no text, path, folder, repository, branch, project, session or turn id, account, model, effort, plan, tool name beyond the hosts' built-in kinds, server, address or key.
4. **A name is printed only if it is schema.** Keys can be a person's data. A script prints a field's name only when all of these hold: it is at a row's top, or in Claude Code's `message` or an item of `message.content`, or in Codex's `payload` or in `payload.source` when that is an object, and never deeper; it is not inside a field that holds free data; it is 40 characters or fewer of letters, digits and `_`; and it is found in at least 5 session files. Free data, never opened (its own name may be counted): Claude Code's `content` and `text` anywhere, an item's `input` and `thinking`, and at a row's top `toolUseResult`, `attachment`, `snapshot`, `data`, `todos` and `summary`; Codex's `message`, `content`, `text`, `arguments`, `input`, `output`, `instructions` and every other `*_instructions`, `summary`, `encrypted_content`, `replacement_history`, `last_agent_message`, `action`, `git`, `info`, `rate_limits` and `images`; on both, any `env`, any tool's metadata or result, and any field you find holding text, a path or an object a person or a tool defined. Every other key is counted as `other`. A row type's value follows the same rule. A field's value is printed only where a section names it, and only in the shape it names.
5. **Fewer than 3.** A count under 3 of anything that could tell what a chat was about is written "fewer than 3".
6. **Sessions left out.** First write `$T/skip`, one session id a line, of: each session with an inbox entry `off_record`; each whose first prompt (an inbox entry of role `user` and kind `prompt`, or the first prompt row of the host's file) starts with `I need a field report on eva-md` or `I need a shapes report on eva-md`, or holds `field-report-prompt.md`, `field-report-2-prompt.md` or `host-shapes-prompt.md`; and each whose first folder is in the config's `exclude` list. Print only how many for each reason. Every count leaves these out.
7. **Compare inside, print outside.** A script may compare ids, folders and times inside itself and print the count. It tests a text only against a fixed string this file gives. It never compares, hashes or keeps two texts of chats.
8. **Write as you go.** Make a folder with `mktemp -d` (`$T`) for your scripts, `$T/skip` and the git check, and nothing else; delete it at the end. Write scripts for Python 3.9. In this chat's folder write only the report: create it first, and write each section when done. If this chat compacts, note it and go on from the last finished section.
9. If the sandbox blocks a read or a command, ask me. Do not work around it.
10. **Nothing private in the report.** No names of people, organizations, projects, repositories, branches, hosts, servers, services or models. No paths but eva-md's standard ones and the hosts' folders as written above, with `~`. Mark each finding measured, observed or inferred. "Unknown" and "could not check, because ..." are good answers. An example is invented and marked so.

## What to find out

Use these numbered headings, in order. Each fact number (F-...) is the sheet's.

**0. This run and this machine.** `eva-md version`, `codex --version`, `claude --version`, and each desktop app's version (`defaults read` of the app's `Info.plist`, key `CFBundleShortVersionString`). The upgrade's date. `$T/skip`'s counts by reason. Then:
- F-182: whether `gh` and `glab` are found by `command -v`, and each one's version number.
- F-185, F-189: run `xcode-select -p` first and print only `clt` or `no-clt`. With `no-clt`, never run `/usr/bin/git` or `/usr/bin/python3`, which would offer an install; if `python3` is found only there, stop and tell me. Map the git found by `command -v git` to `/usr/bin`, Homebrew (`/opt/homebrew` or `/usr/local`) or other, and print its version only as "at or above 2.36" or "below 2.36". Do the same for `/usr/bin/git` with `clt`. Then save this check as `$T/g.sh` and run it for each git, as `T=$T G=<that git> bash $T/g.sh`; copy its four lines (a missing line is a fail). Its name, address and key are made up:

```sh
G=${G:-git}; r=$(mktemp -d "$T/g.XXXXXX"); cd "$r" || exit
export HOME="$r" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0
g() { "$G" -c core.hooksPath=/dev/null -c user.name=Test -c user.email=test@example.invalid -c init.defaultBranch=main "$@" >/dev/null 2>&1; }
ok() { if "$@"; then echo pass; else echo fail; fi; }
ssh-keygen -q -t ed25519 -N '' -C test -f "$r/k" >/dev/null 2>&1
g init -q w && cd w || exit
echo a > f && ln -s f l && g add f l && g commit -q -m one
printf 'sign: '; ok g -c gpg.format=ssh -c user.signingkey="$r/k" commit -q -S --allow-empty -m two
printf 'fsync: '; ok g -c core.fsync=loose-object,reference -c core.fsyncMethod=fsync commit -q --allow-empty -m three
g switch -q -c b && echo b > f && g commit -q -am b && g switch -q main && echo c > f && g commit -q -am c
g merge --no-commit -X no-renames b
printf 'conflict stages: '; ok test "$("$G" ls-files -u -- f | wc -l | tr -d ' ')" = 3
g merge --abort
printf 'symlink as file: '; cd "$r" && g -c core.symlinks=false clone -q w s && ok test -f s/l -a ! -L s/l
```

- F-132: a script that prints only the value of `persistence` under `[history]` in `~/.codex/config.toml` if it is `save-all` or `none`, else `other`, or `absent`.

**1. eva-md's reports.** Copy `eva-md audit capture` as printed, but its per-session lines: count those. Copy from `eva-md status --updates` the lines `outcomes`, `chat boundary to memory on file, by trigger`, `busy exits` and `model calls a day` (calls only, per host). These give F-004 (the final replies found, missing and extra; the turns with no stored reply by cause), F-024 (`host sessions with no eva-md file`), F-030 (passes a hook's trigger started) and F-032 (passes with calls that succeeded, per host).

**2. eva-md's inbox and heartbeats.** One script over the inbox. By host:
- F-013: inbox files by the session part of their name: a UUID; only letters, digits, `_` and `-` but not a UUID; anything else.
- F-001: the share of entries with a non-empty `turn`, by kind and `origin`. Then, in each session's hook entries in order, a turn runs from a prompt entry to the next: count turns whose every entry carries the prompt's `turn`, and turns with an entry that carries another or none.
- F-004: `reply` entries from hooks, and `gap: missing final reply` events.
- F-027: tool entries with a non-empty `agent`, how many of them end with ` (subagent)`, and sessions with one.
- F-034, F-136: `compact` events and `host_summary` entries; a summary's size, median and largest, in buckets.
- F-127: of the distinct `cwd` values, compared inside the script and read with `stat`, and by reading only the first four bytes of a worktree's `HEAD`: `.git` a file (a worktree), a folder, absent, the folder gone; of the worktrees, how many have a detached HEAD (not starting `ref:`) and how many lie under `~/.codex/worktrees/`.
- From the heartbeats, per host and event: F-005, `transcript_available` true or false; on Claude Code, F-018 and F-079, `entrypoint` (print only `cli`, `sdk-cli`, `sdk-ts`, `sdk-py`, `claude-desktop`, `claude-vscode`, else `other`) and `headless`.

**3. eva-md's commit and run records.**
- F-031, F-029, F-178, F-180: in 14 days of commit records, counts by `outcome` and by `push`, and the days with an `ok` push. The wiki remote's scheme, by a `case` that prints only `ssh` (`git@...` or `ssh://...`), `https` or `other`. A daily job starts from a hook: an `ok` push over `ssh` means a hook's job reached the server through the SSH agent with the host key known; a `pending` push means it did not.
- F-028: sessions of eva-md's own runs in each host's files (the first folder's last part starts `eva-md-run-`, `eva-md-codex-` or `eva-md-live-`), with an inbox file and without. Whether `uncaptured.json`'s time on each host falls between the first and last row of such a session (yes, no, no file).

**4. The hosts' records.** Save this script as `$T/shapes.py`. Run `python3 -I $T/shapes.py claude $T/skip`, then with `codex`. It applies rules 3 to 5 itself: a name or type it cannot print shows as `other`. Copy its output into tables, one set per host: files and row counts; the first row's names and size; row types with counts and sizes; for each row type its names, with the rows carrying each where that is fewer than all; names that are `true` on some rows; the five commonest turns (a turn's row types in order, repeats folded, 12 at most); compactions, with the rows before and after; rows holding eva-md's start notice; and the values it prints (`entrypoint`, or `source`, `originator` and `cli_version`).

```python
import json,os,re,sys,glob
from collections import Counter as C,defaultdict as D
from datetime import datetime as DT
H=sys.argv[1];sk=set(open(sys.argv[2]).read().split());hm=os.path.expanduser('~')
G={'claude':['.claude/projects/**/*.jsonl'],'codex':['.codex/sessions/**/*','.codex/archived_sessions/**/*']}[H]
fs=[f for g in G for f in glob.glob(os.path.join(hm,g),recursive=True) if os.path.isfile(f)]
W=re.compile(r'[A-Za-z0-9_]{1,40}$');V=re.compile(r'\d+\.\d+\.\d+[-.0-9a-z]{0,20}$');U=re.compile(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}')
OK={'cli','sdk-cli','sdk-ts','sdk-py','claude-desktop','claude-vscode'};NOTE='eva.md: earlier work is available for this chat.'
nf=D(set);n=C();big=[];SZ=D(list);RC=C();FN=C();RK=C();TR=C();SQ=C();CP=C();NT=C();VL=C()
def ts(s):
 try:return DT.strptime(s[:19],'%Y-%m-%dT%H:%M:%S').timestamp()+float('0'+s[19:-1])
 except Exception:return None
def strs(o,k=''):
 if isinstance(o,str):yield k,o
 for a,b in (o.items() if isinstance(o,dict) else enumerate(o) if isinstance(o,list) else ()):yield from strs(b,a if isinstance(a,str) else k)
for f in fs:
 sub='/subagents/' in f;u=U.findall(os.path.basename(os.path.dirname(os.path.dirname(f)) if sub else f))
 if u and u[-1] in sk:n['left out']+=1;continue
 n['files'+' sub'*sub+' '+(os.path.splitext(f)[1] or 'none')]+=1;ks=[];pt=None;where=set();got=0
 for i,x in enumerate(open(f,'rb')):
  try:r=json.loads(x);assert isinstance(r,dict)
  except Exception:n['unreadable rows']+=1;continue
  m=r.get('payload' if H=='codex' else 'message');m=m if isinstance(m,dict) else {};c=m.get('content') if H=='claude' else None
  it=[j for j in c if isinstance(j,dict)] if isinstance(c,list) else [];a=str(r.get('type'))
  b=[m['type']] if H=='codex' and isinstance(m.get('type'),str) else sorted({str(j.get('type')) for j in it})
  if r.get('isCompactSummary') is True:b=['compact_summary']
  k=(a,'+'.join(b));ks.append(k);RC[k]+=1;SZ[k].append(len(x));[nf['t:'+v].add(f) for v in [a]+b]
  for p,o in [('',r),('p.',m)]+[('c:%s'%j.get('type'),j) for j in it]+[('p.source.',m.get('source'))]*isinstance(m.get('source'),dict):
   for y,z in o.items():nf['k:'+y].add(f);RK[k,p,y]+=1;TR[k,p,y]+=z is True;FN[p,y]+=i==0
  if i==0:big.append(len(x))
  n['sidechain rows in main files']+=r.get('isSidechain') is True and not sub
  for y,z in strs(r):NT[k,y]+=z==NOTE
  w=ts(r.get('timestamp')) if isinstance(r.get('timestamp'),str) else None
  if None not in (w,pt):n['rows earlier than the row before']+=w<pt;n['rows at the time of the row before']+=w==pt
  pt=pt if w is None else w;cw=r.get('cwd') or m.get('cwd');where|={(cw,r.get('gitBranch'))} if cw else set()
  e=[]
  if H=='claude' and a=='user' and not got:got=1;e=[('entrypoint',r.get('entrypoint'))]
  if H=='codex' and a=='session_meta':e=[(y,m.get(y)) for y in ('source','originator','cli_version')]
  for y,z in e:z=z if isinstance(z,str) else 'object:'+','.join(z) if isinstance(z,dict) else 'absent';VL[y,z]+=1;nf['v:'+z].add(f)
 n['files whose folder or branch changes']+=len(where)>1
 st=[i for i,k in enumerate(ks) if k in (('event_msg','task_started'),('event_msg','turn_started')) or k[0]=='user' and 'tool_result' not in k[1] and k[1]!='compact_summary']
 for i,j in zip(st,st[1:]+[len(ks)]):
  q=[]
  for k in ks[i:j]:q+=[k]*(not q or q[-1]!=k)
  SQ[tuple(q[:12])+(('...',''),)*(len(q)>12)]+=1
 for i,k in enumerate(ks):CP[k,ks[i-1] if i else('none',''),ks[i+1] if i+1<len(ks) else('none','')]+=k[1] in('compact_summary','context_compacted') or k[0]=='compacted'
def ok(s,p='k:'):
 if s in('...','none','compact_summary') or p=='v:' and (s in OK or V.match(s) or s[:7]=='object:' and all(W.match(v) for v in s[7:].split(','))):return s
 return s if W.match(s) and len(nf[p+s])>=5 else 'other'
def N(p,y):return ('c[%s].'%ok(p[2:],'t:') if p[:2]=='c:' else p)+ok(y)
def K(k):return ':'.join([ok(k[0],'t:')]+['+'.join(ok(v,'t:') for v in k[1].split('+'))]*(k[1]!=''))
def B(v):return next(l for m,l in((1024,'<1KB'),(8192,'1-8KB'),(65536,'8-64KB'),(1<<20,'64KB-1MB'),(1<<62,'>1MB')) if v<m)
def say(t,c):
 z=C()
 for k,v in c.items():z[k]+=v
 for k,v in z.most_common():print(t,'|',k,'|',v if v>=3 else 'fewer than 3') if v else 0
b=sorted(big) or [0];print(dict(n),'| first row bytes: median',B(b[len(b)//2]),'largest',B(b[-1]),'over 64 KiB',sum(v>65536 for v in b))
say('first row has',{N(p,y):v for (p,y),v in FN.items()})
say('rows of type',{K(k):v for k,v in RC.items()})
Z=D(list)
for k,v in SZ.items():Z[K(k)]+=v
for k,v in Z.items():v.sort();print('bytes of type |',k,'| median',B(v[len(v)//2]),'largest',B(v[-1])) if len(v)>=3 else 0
say('rows of type, name',{(K(k),N(p,y)):v for (k,p,y),v in RK.items()})
say('rows of type, name true',{(K(k),N(p,y)):v for (k,p,y),v in TR.items()})
Z=C()
for q,v in SQ.items():Z[' > '.join(K(k) for k in q)]+=v
for q,v in Z.most_common(5):print('turn |',q,'|',v if v>=3 else 'fewer than 3')
say('compaction row, before, after',{tuple(K(k) for k in x):v for x,v in CP.items()})
say('notice in type, name',{(K(k),ok(y)):v for (k,y),v in NT.items()})
say('value',{(y,ok(z,'v:')):v for (y,z),v in VL.items()})
```

What its output settles: F-020 and F-133 (rows out of time order, rows at the same time, whether rows carry a time or another position); F-021 (which row types carry a time, a folder and a branch; files whose folder or branch changes); F-022 (turns: text, tool calls and tool results as separate rows, and whether a tool's row carries a status); F-084 and F-085 (Claude Code's names, flags and row types, and how large each type's rows are); F-132 and F-134 (Codex's versions, file kinds and its first row); F-034 and F-136 (compactions); F-027 (subagent files, sidechain rows, a subagent's `source`); F-046 (on which row type and field eva-md's start notice, sent only to be shown, was found: a row of a kind the model reads means it reached the model); F-018 and F-079 (the values); F-149 (whether a voice call's rows, such as `realtime_item`, are in the files, and in what kind of file).

**5. Claude Code beside eva-md.**
- F-001: of hook entries with a `turn`, the share whose `turn` is the `promptId` of a row in the session's own file.
- F-013: the hosts' session ids (file names) in section 2's three classes.
- F-087: the folder under `~/.claude/projects/` whose name is this folder's path with each character other than a letter or digit made `-`: whether it has `memory/MEMORY.md`, its size in buckets, and how many files `memory/` holds (if no folder matches, make only `/` and `.` into `-`, and say which matched). Also how many project folders have a `memory` folder.

**6. Codex beside eva-md.** For Codex sessions with both an inbox file and a session file, matched by id inside the script. Map each to a client: the desktop app, the terminal, an IDE extension, a program, a subagent or other, from `source` and `originator`, as the second report's section 3 did if its report is in this folder; say how you mapped.
- F-013: the `session_meta` ids in section 2's three classes.
- F-137: by client, inbox sessions with a session file and without.
- F-138: inbox sessions with a `session_end` and no `session_start`: how many have a session file, and of those how many hold a turn-start row.
- F-001: the share of turn-start rows (`task_started` or `turn_started`) that carry a `turn_id`, and of hook entries with a `turn`, the share that equal a turn-start row's `turn_id` in the same file.
- F-010: in a person's chats, turns whose file holds two or more `user_message` rows between one turn-start row and the next. Of them, how many have as many inbox prompt entries with that turn id as the file has user rows, how many fewer, and how many none.
- F-126: sessions with a `session_end` entry: the minutes from the file's last row to that entry (read each time with its offset), in buckets (end before the last row, under 1, 1 to 5, 5 to 30, 30 to 35, 35 to 60, over 60), with the median and 95th percentile, by client.
- F-156: sessions started since the upgrade by client, and how many of each have an inbox file with a `session_start` entry.

**7. Questions for the owner.** List these, with room for a line under each:
1. F-038: Did eva-md's MCP tools ask for permission the first time, on each host? Once per tool, once per chat, or never? Was "always allow" offered?
2. F-176, F-183, F-202: (a) Does the daily commit or push ask you to sign or sign in? (b) Open your wiki project's settings in your browser, reading only, at its push rules and branch protection. Answer yes, no or don't know, never a setting's name: does a rule require a committer address the server knows? Does one require signed commits? Is the default branch protected against your pushes? Are forced pushes allowed on it? (c) In System Settings, General, Device Management, is any profile listed?
3. F-177: Is the git server reachable only on a private network?
4. F-178, F-180: Has a git push or fetch on this Mac ever waited for a password, a key's passphrase, a touch or a dialog? When the wiki was first pushed, were you asked to accept the server's host key?

## The report

Write it to `eva-host-shapes.md` in this folder (`eva-host-shapes-run2.md` if that exists), in Markdown, headings 0 to 7 as above, tables where numbers repeat. Then a section "Facts": one line per fact, in number order: `F-...` (and the host, where the halves differ): what was seen, as one line of shapes; in how many files or sessions; measured, observed or inferred. Give F-001, F-004, F-005, F-010, F-013, F-018, F-020, F-021, F-022, F-024, F-027, F-028, F-029, F-030, F-031, F-032, F-034, F-038, F-046, F-079, F-084, F-085, F-087, F-126, F-127, F-132, F-133, F-134, F-136, F-137, F-138, F-149, F-156, F-176, F-177, F-178, F-180, F-182, F-183, F-185, F-189 and F-202; for one you could not settle, say why. No prose about what any chat was about. End with a line saying you read the report again against rules 3, 4 and 10, and how many things you removed by class, never the thing itself. Delete `$T`. Then give me a five-line summary in the chat.
~~~
