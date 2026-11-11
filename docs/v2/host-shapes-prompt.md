# Host shapes prompt, a companion to the second field report

**Status:** for the counters release (RELEASE-NUMBER) or later. A read-only look, on the one real install (a Mac whose wiki is on a self-hosted GitLab), at the shapes of eva-md's records and the hosts' session files, for facts [host-facts.md](host-facts.md) marks "by the field report" that [the second report](field-report-2-prompt.md) has no room for. Message: [host-shapes-message.md](host-shapes-message.md). Checked on 2026-11-11 in two rounds, by agents in made-up homes with planted values; after the second round's fixes the script printed none but row, payload and subtype names planted in 5 or more chats, marked `*`.

Run it on the same Mac and folder as the second report, after it or another day, and after a day of normal use since the upgrade. Expect 30 to 60 minutes of the agent's work.

## How to use it

1. Open a new Codex chat in the desktop app, in the git repository you used for the second report, from your normal home. Use Codex's default mode, not full access: its sandbox blocks the network and writes outside the chat's folder and temp, so reads are not put to you.
2. Attach this file with its message, or paste the part between the lines of tildes as your first message.
3. Approve only the commands in rule 1; refuse the rest, as the message's note lists, and any request for access to a folder.
4. It writes `eva-host-shapes.md` in the chat's folder (`eva-host-shapes-run2.md` if that is there). You are the last check: strike any name, path, title, quote, model or service name, and any name marked `*` (one eva-md does not know) unless it is plainly a host's own word. Answer its last section a line each, in general terms, and bring the file back.

## What to know before you run it

- **The chat is captured like any other**, and may be summarized into the wiki and pushed by the daily job. The report leaves this chat out of its counts.
- **What the agent reads.** eva-md's records, and its config file, using only `exclude`; the hosts' session files; Codex's config file, printing one setting; the wiki remote's scheme; versions; where git, python3, gh and glab are. Scripts compare folder names with `exclude` and eva-md's run folders, and prompts only with fixed strings ("off the record", this report, eva-md's start notice), inside themselves. They print counts, sizes in buckets, days, yes or no, the hosts' field names and fixed words.
- **What may be written.** eva-md's commands named here write nothing. The agent: scripts, left-out session ids and a git check with made-up files and a throwaway key, signing without your SSH agent, in a temporary folder it deletes; the report.

~~~
I need a shapes report on eva-md, the memory layer installed on this Mac for Codex and Claude Code. A team rebuilding it must learn how the hosts' records are built: row types, the fields a row carries, how rows follow each other, how eva-md's records line up with them. They need shapes only: counts, the hosts' field names, yes or no; nothing a chat or file says, nothing that names a person, project, folder or machine.

eva-md's hooks record each chat into a local inbox, secrets removed, and give a new chat a short brief; a background job turns chats into a wiki, a git repository that a daily job, started from a hook, commits and pushes.

## Where things are

- The program: `~/.local/share/eva-md/bin/eva-md`, maybe on PATH as `eva-md`; if not, run it by that path. Config: `~/.config/eva-md/config.toml`: `wiki_path` names the wiki, `exclude` lists folders left out of capture.
- State, `~/.local/state/eva-md/`:
  - `inbox/<project>/<host>-<session>.jsonl`: entries with `ts`, `host`, `session`, `turn`, `role`, `kind`, `agent` (a subagent's id), `origin` (`hook` or `catchup`), `cwd` (relative to the home folder), `text`. An event is kind `event` with a fixed text, as `session_start: <source>`, `session_end`, `compact`, `gap: missing final reply`, `off_record`. Kind `host_summary` is a compaction's summary.
  - `heartbeats/<host>/<event>.json`: `transcript_available` (the last payload named the host's file), and on Claude Code `entrypoint` and `headless`. `heartbeats/<host>/uncaptured.json`: `at`, the time of the last hook run that captured nothing.
  - `commits/<YYYY-MM>.jsonl`: each daily commit and push: `at`, `outcome`, `push` (`ok`, `up_to_date`, `pending`, `diverged`, `failed`, `skipped`).
- Claude Code: `~/.claude/projects/<folder>/<session>.jsonl`; a subagent's file is in `<session>/subagents/`, or, from older versions, `agent-<id>.jsonl` beside the sessions. A row has `type`, `sessionId`, `uuid`, `promptId` and `message`, whose `content` is text or items with a `type` (`text`, `tool_use`, `tool_result`).
- Codex: `~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-...-<session>.jsonl` and `~/.codex/archived_sessions/`. A row has `type` (`session_meta`, `event_msg`, `response_item` and others) and `payload` with its own `type`, as `task_started` or `turn_started` (with `turn_id`) and `user_message`. `session_meta`'s payload: `id`, `cwd`, `source`, `originator`, `cli_version`.

## Rules

1. **Read only.** You may run, in this order: section 0's scripts (`xcode-select -p`, and git and `ssh-keygen` only inside your folder); `eva-md version`, `eva-md audit capture` with no flag, `eva-md status --updates`; `codex --version`, `claude --version`; `defaults read /Applications/Codex.app/Contents/Info.plist CFBundleShortVersionString`, and the same for `Claude.app`; `command -v`; `GH_NO_UPDATE_NOTIFIER=1 gh --version 2>/dev/null | head -1` and `GLAB_CHECK_UPDATE=false glab --version 2>/dev/null | head -1`, nothing else of either; `git -C <wiki> --no-optional-locks config --get remote.origin.url`, only in a script that prints only its scheme; `python3 -I` on your scripts; `mktemp -d` and `rm -r` of that folder. Between them, `echo`, `if`, `case`, `for`, pipes, `wc -l` and `>` into `$T` or the report are fine. Nothing else: no other eva-md or host command, no web search or fetch, no network command, no subagent. Change no file or setting. Fix nothing: a failure is a finding.
2. **Order.** Make `$T` (rule 8), then section 0, section 1 (copy eva-md's numbers as printed; they include this chat and the sessions rule 6 leaves out: say so), rule 6's step, and the rest. Script only what eva-md's reports do not give.
3. **Shapes only.** A script may open eva-md's files, the hosts' session files and the few other files a section names, never a chat's folder, and may print only: counts; shares; durations and gaps in time, in the buckets this file names; sizes in bytes, in buckets (under 1 KB, 1 to 8 KB, 8 to 64 KB, 64 KB to 1 MB, over 1 MB); yes or no; days, or counts of days; version numbers of eva-md, the hosts, `gh` and `glab`, and git's only as at or above 2.36 or below; names under rule 4; and the fixed words a section lists. It never prints a value of any field, whole or in part, hashed or not: no text, path, folder, repository, branch, project, session or turn id, account, model, effort, plan, tool name beyond the hosts' built-in kinds, server, address or key. **Errors.** Every script starts with s.py's first two lines, so an error prints only its class and line, and reads files as s.py's `rows` does, counting one it cannot read by the error's class. When a script fails or a count looks wrong, never print a row, line, config, id, folder listing or value to see why: fix it from the class and line only.
4. **A name is printed only if it is schema.** Keys can be a person's data. Open only the levels named here and the fields a section names; every other field is free data: never opened, its name only counted, its text tested only against a fixed string this file gives. A script prints a name (a field's name, a row's or payload's `type`, a system row's `subtype`) only when it is at a row's top, in Claude Code's `message` or Codex's `payload` or `payload.source`, never deeper; and it is in section 4's known set `KN`, or is 40 characters or fewer of letters, digits and `_` (a type may hold `-`) and found in 5 or more different chats. A chat is a session, not a file: files sharing a session id or a row's `uuid` (copies, resumes, forks) and its subagents' files are one chat. Items of Claude Code's `message.content` are free data but for their `type` and keys, printed only if in `KN`. A printed name outside `KN` is marked `*`; one not printed shows as `other`. A field's value is printed only where a section names it, and only from the fixed list it gives.
5. **Fewer than 3.** A count under 3 of anything that could tell what a chat was about is written "fewer than 3".
6. **Sessions left out.** Save section 4's script as `$T/s.py` and run `python3 -I $T/s.py skip $T/skip`. It writes `$T/skip`, one id a line, of each session with an `off_record` entry or a prompt that starts with, or has a line that is, "off the record" (case and punctuation aside); each whose first turn is a field or shapes report's prompt; each whose first folder, or a parent, matches `exclude` (if `exclude` cannot be read, every session); if `exclude` has a `remote:` entry, each host session with no inbox file but eva-md's own runs; and every file of the same chat (rule 4) as one left out. Copy its counts. Every count of a script leaves these out: your own scripts skip a session file whose name's id, any `sessionId` in it, or its parent's (the `<session>/subagents/` folder, Codex's `parent_thread_id` in `source`) is in `$T/skip`.
7. **Compare inside, print outside.** A script compares ids, folders and times inside itself and prints counts. It tests a text only against a fixed string this file gives, and never compares, hashes or keeps two texts of chats.
8. **Write as you go.** Make a folder with `mktemp -d` (`$T`) for your scripts, `$T/skip` and the git check only; delete it at the end. Scripts are for Python 3.9. In this chat's folder write only the report: create it first, add each section when done. If this chat compacts, note it and go on from the last finished section.
9. If the sandbox blocks a read or a command, ask me. Do not work around it.
10. **Nothing private in the report.** No names of people, organizations, projects, repositories, branches, hosts, servers, services or models. No paths but eva-md's standard ones and the hosts' folders as written above, with `~`. Mark each finding measured, observed or inferred. "Unknown" and "could not check, because ..." are good answers. An example is invented and marked so.

## What to find out

Use these numbered headings, in order; fact numbers (F-...) are the sheet's.

**0. This run and this machine.** F-185, F-189: save the scripts below as `$T/c.sh` and `$T/g.sh`; run `T=$T bash $T/c.sh`. Copy what it prints (a missing check line is a fail). With `no-clt`, never run `/usr/bin/git` or `/usr/bin/python3`, which would offer an install; if `command -v python3` gives that one, stop and tell me. The check's name, address and key are made up.

```sh
xcode-select -p >/dev/null 2>&1 && c=clt || c=no-clt; echo $c
for G in $(printf '%s\n' "$(command -v git)" /usr/bin/git | awk 'NF&&!s[$0]++'); do
 case $G in /usr/bin/*) [ $c = clt ] || continue; w=/usr/bin;; /opt/homebrew/*|/usr/local/*) w=Homebrew;; *) w=other;; esac
 echo "$w: $("$G" --version | awk '{split($3,a,".");print (a[1]*1000+a[2]>=2036?"at or above 2.36":"below 2.36")}')"; T=$T G=$G bash $T/g.sh
done
```

```sh
G=${G:-git}; r=$(mktemp -d "$T/g.XXXXXX"); cd "$r" || exit
export HOME="$r" TMPDIR="$r" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0; unset SSH_AUTH_SOCK GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT
g() { "$G" -c core.hooksPath=/dev/null -c user.name=Test -c user.email=test@example.invalid "$@" >/dev/null 2>&1; }
ok() { if "$@"; then echo pass; else echo fail; fi; }
ssh-keygen -q -t ed25519 -N '' -C test -f "$r/k" >/dev/null 2>&1
g init -q -b main w && cd w || exit
echo a > f && ln -s f l && g add f l && g commit -q -m one
printf 'sign: '; ok g -c gpg.format=ssh -c user.signingkey="$r/k" commit -q -S --allow-empty -m two
printf 'fsync: '; ok g -c core.fsync=loose-object,reference -c core.fsyncMethod=fsync commit -q --allow-empty -m three
g switch -q -c b && echo b > f && g commit -q -am b && g switch -q main && echo c > f && g commit -q -am c
g merge --no-commit -X no-renames b
printf 'conflict stages: '; ok test "$("$G" ls-files -u -- f | wc -l | tr -d ' ')" = 3
g merge --abort
printf 'symlink as file: '; cd "$r" && g -c core.symlinks=false clone -q w s && ok test -f s/l -a ! -L s/l
```

Then the versions of eva-md, the hosts and each desktop app by rule 1's commands, and:
- F-182: whether `gh` and `glab` are found by `command -v`, and each one's version number.
- F-132: a script that prints only the value of `persistence` under `[history]` in `~/.codex/config.toml` if it is `save-all` or `none`, else `other`, or `absent`.

**1. eva-md's reports.** Copy `eva-md audit capture` as printed, but its per-session lines: count those. Copy from `eva-md status --updates` the lines `outcomes`, `chat boundary to memory on file, by trigger` and `busy exits`, and from `model calls a day` only each host's sum over the days and the number of days with a call, no dates. They give F-004 (final replies found, missing, extra; turns with no stored reply, by cause), F-024 (`host sessions with no eva-md file`), F-030 (passes a hook's trigger started), F-032 (passes whose calls succeeded, per host). Then rule 6's step.

**2. eva-md's inbox and heartbeats.** One script over the inbox. By host:
- F-013: inbox files by the session part of their name: a UUID; only letters, digits, `_` and `-` but not a UUID; anything else.
- F-001: the share of entries with a non-empty `turn`, by kind and `origin`. In each session's hook entries, from one prompt entry to the next, leaving out events: by kind, entries with the prompt's `turn`, with none, and with another.
- F-004: `reply` entries from hooks, and `gap: missing final reply` events.
- F-027: tool entries with a non-empty `agent`; of them, how many have a `text` holding ` (subagent)` (eva-md's mark after a tool's name); and sessions with such entries.
- F-034, F-136: `compact` events and `host_summary` entries; a summary's size, median and largest, in buckets.
- From the heartbeats, per host and event: F-005, `transcript_available` true or false; on Claude Code, F-018 and F-079, `entrypoint` (only a value in section 4's `VA`, else `other`) and `headless`.

**3. eva-md's commit and run records.**
- F-031, F-029, F-178, F-180: in the last 14 days of commit records, counts by `outcome` (`committed`, `nothing`, `refused`, `failed`, `busy`, else `other`) and by `push` (its list above, else `other`), the number of days with an `ok` push, and the longest run of days without one; never the dates. The wiki remote's scheme, by a `case` that prints only `ssh` (`git@...` or `ssh://...`), `https` or `other`. An `ok` push over `ssh`: the daily job reached the server through the SSH agent; `pending`: it did not.
- F-028: sessions of eva-md's own runs in each host's files (the first folder's last part starts `eva-md-run-`, `eva-md-codex-` or `eva-md-live-`), with an inbox file and without. Whether `uncaptured.json`'s `at` on each host falls between the first and last row of such a session (yes, no, no file).

**4. The hosts' records.** This is `$T/s.py` (rule 6). Run `python3 -I $T/s.py claude $T/skip`, then with `codex`. It applies rules 3 to 5 itself. Values print only from its lists `VA`, and `cli_version` as its number, with `+suffix` if more follows. Copy its output into tables, one set per host, in its order: names are folded, a line per row type; a turn is its row types in order, repeats folded, 12 at most.

```python
import json,os,re,sys,glob,fnmatch,traceback;from collections import Counter as C,defaultdict as D;from datetime import datetime as DT
sys.excepthook=lambda t,v,b:print('error',t.__name__,'line',b and traceback.extract_tb(b)[-1].lineno)
H,SK=sys.argv[1:3];I=isinstance;hm=os.path.expanduser('~');J=os.path.join;bn=os.path.basename;dn=os.path.dirname
W=re.compile(r'[A-Za-z0-9_]{1,40}\Z');TW=re.compile(r'[A-Za-z0-9_-]{1,40}\Z')
NOTE='eva.md: earlier work is available for this chat.'
VA={'entrypoint':{'cli','sdk-cli','sdk-ts','sdk-py','claude-desktop','claude-vscode'},'source':{'cli','vscode','exec','mcp','unknown','object:subagent'},'originator':{'codex_cli_rs','codex_vscode','codex_exec','codex_sdk_ts','Codex Desktop'}}
KN=set('''type sessionId timestamp cwd gitBranch promptId isMeta isSidechain agentId agent_id isCompactSummary entrypoint message role model stop_reason content uuid turnOrigin origin isApiErrorMessage isVisibleInTranscriptOnly toolUseResult attachment progress data summary subtype user assistant system compact_boundary compact_summary text tool_use tool_result thinking redacted_thinking image document server_tool_use web_search_tool_result signature id name input tool_use_id is_error payload session_meta turn_context event_msg response_item compacted realtime_item task_started turn_started turn_id user_message agent_message phase task_complete last_agent_message turn_aborted context_compacted function_call function_call_output custom_tool_call custom_tool_call_output local_shell_call call_id arguments output source originator cli_version subagent thread_spawn parent_thread_id'''.split())
def F(v):return v if v>=3 or not v else 'fewer than 3'
UN=C();U={}
def R(a):
 while U.get(a,a)!=a:a=U[a]
 return a
def join(s):
 s=[R(a) for a in s]
 for a in s[1:]:U[a]=s[0]
def files(h):
 g={'claude':['.claude/projects/**/*.jsonl'],'codex':['.codex/sessions/**/*','.codex/archived_sessions/**/*']}[h]
 return sorted(f for x in g for f in glob.glob(J(hm,x),recursive=True) if os.path.isfile(f))
def rows(f):
 try:
  with open(f,'rb') as h:L=h.read().split(b'\n')
 except Exception as e:UN[type(e).__name__]+=1;return
 for x in L:
  if x.strip():
   try:r=json.loads(x)
   except Exception:r=None
   yield x,r if I(r,dict) else None
def strs(o,k=''):
 if I(o,str):yield k,o
 for a,b in (o.items() if I(o,dict) else enumerate(o) if I(o,list) else ()):yield from strs(b,a if I(a,str) else k)
def REP(m):return bool(re.search(r'I need a (field|shapes) report on eva-md|field-report(-2)?-prompt\.md|host-shapes-prompt\.md',m))
def OFF(m):
 w=lambda l:' '.join(re.findall('[a-z]+',l.lower()))
 return w(m).startswith('off the record') or any(w(l)=='off the record' for l in m.splitlines())
def who(f,L):
 st=bn(f).rsplit('.',1)[0];ids={st,re.sub(r'^rollout-[0-9T:-]{19}-','',st)};par=bn(dn(dn(f))) if '/subagents/' in f else None;cw=None;mk=off=n=0;fp=1
 for x,r in L:
  if r is None:continue
  p=r.get('payload');p=p if I(p,dict) else {};t=r.get('type');m=None
  if t=='session_meta':
   ids|={str(p[y]) for y in ('id','forked_from_id') if p.get(y)};s=p.get('source')
   if I(s,dict):par=par or next((z for y,z in strs(s) if y=='parent_thread_id'),None)
  ids|={r[y] for y in ('sessionId','uuid') if I(r.get(y),str)}
  cw=cw or r.get('cwd') or p.get('cwd');n+=p.get('type') in('task_started','turn_started')
  if p.get('type')=='user_message':m=p.get('message')
  g=r.get('message');c=g.get('content') if t=='user' and I(g,dict) and not r.get('isMeta') else None
  if I(c,list) and not any(I(i,dict) and i.get('type')=='tool_result' for i in c):m='\n'.join(str(i.get('text')) for i in c if I(i,dict) and i.get('type')=='text')
  if I(c,str):m=c
  if I(m,str):
   off|=OFF(m)
   if fp and n<2:mk|=REP(m);fp=t!='user'
 return ids,par,cw,mk,off
if H=='skip':
 ALL=0;ex=[]
 try:cf=open(J(hm,'.config/eva-md/config.toml')).read()
 except FileNotFoundError:cf=''
 except Exception:cf='';ALL=1
 m=re.search(r'^\s*exclude\s*=\s*(\[)?',cf,re.M)
 if m:
  ALL=1
  for a,b,c in re.findall(r'"((?:[^"\\]|\\.)*)"|\'([^\']*)\'|(#[^\n]*|\])',cf[m.end():])*bool(m.group(1)):
   if c==']':ALL=0;break
   if not c:ex.append(a or b)
 rem=any(e.strip().startswith('remote:') for e in ex);P=set()
 for e in ex:
  e=e.strip();e=J(hm,e[2:]) if e[:2]=='~/' else hm if e=='~' else e
  if os.path.isabs(e):P.add(os.path.normpath(e).lower())
 def exc(d):
  t=os.path.normpath(d).lower()
  while 1:
   if any(fnmatch.fnmatchcase(t,p) for p in P):return 1
   if dn(t)==t:return 0
   t=dn(t)
 R0=C();S=set();IB=D(set);A=[]
 for f in glob.glob(J(hm,'.local/state/eva-md/inbox/*/*.jsonl')):
  if not os.path.isfile(f):continue
  b=bn(f)[:-6];h='claude' if b[:12]=='claude-code-' else 'codex';ids={b[12:] if h=='claude' else b[6:]};cw=fp=None;why=set()
  for x,e in rows(f):
   if e is None:continue
   ids|={e['session']} if I(e.get('session'),str) else set();cw=cw or e.get('cwd')
   if e.get('kind')=='event' and e.get('text')=='off_record':why.add('off the record')
   if fp is None and e.get('role')=='user' and e.get('kind')=='prompt':fp=str(e.get('text'));why|={'this report'}if REP(fp) else set()
  if I(cw,str) and exc(J(hm,cw)):why.add('excluded folder')
  if ALL:why.add('exclude list unread')
  IB[h]|=ids;S|=ids if why else set()
  for w in why:R0['inbox',w]+=1
 for h in('claude','codex'):
  for f in files(h):
   ids,par,cw,mk,off=who(f,list(rows(f)));why=set();k=ids|{par}-{None};join(list(k));A.append(k)
   if off:why.add('off the record')
   if mk:why.add('this report')
   if I(cw,str) and exc(cw):why.add('excluded folder')
   if rem and not k&IB[h] and not bn(str(cw)).startswith(('eva-md-run-','eva-md-codex-','eva-md-live-')):why.add('remote rule, no inbox file')
   if ALL:why.add('exclude list unread')
   S|=k if why else set()
   for w in why:R0[h,w]+=1
 Q={R(a) for a in S}
 for k in A:
  if any(R(a) in Q for a in k) and not k<=S:S|=k;R0['all','joined to one left out']+=1
 open(SK,'w').write(''.join(i+'\n' for i in S))
 for k,v in sorted(R0.items()):print('left out |',k,'|',F(v))
 for k,v in UN.items():print('files not read |',k,'|',F(v))
 sys.exit()
sk=set(open(SK).read().split())
nf=D(set);n=C();big=[];SZ=D(list);FN=C();RK=C();TR=C();SQ=C();CP=C();NT=C();VL=C()
def ts(s):
 try:return DT.strptime(s[:19],'%Y-%m-%dT%H:%M:%S').timestamp()+float('0'+s[19:-1])
 except Exception:return None
def val(y,z):
 z=z if I(z,str) else('object:subagent' if 'subagent' in z else 'object:other') if I(z,dict) else 'absent';v=re.match(r'\d+\.\d+\.\d+',z)
 return z if z in VA.get(y,set())|{'absent'} else v.group()+' +suffix'*(v.end()<len(z)) if y=='cli_version' and v else 'other'
for fi,f in enumerate(files(H)):
 L=list(rows(f));ids,par,_,_,_=who(f,L);sub='/subagents/' in f or bn(f)[:6]=='agent-';tok='f%d'%fi
 if (ids|{par})&sk:n['left out']+=1;continue
 join([tok]+sorted(ids|{par}-{None}))
 x=os.path.splitext(f)[1];n['files'+' sub'*sub+' '+(x if x=='.jsonl' else 'other' if x else 'none')]+=1;ks=[];pt=None;where=set();got=0
 for i,(x,r) in enumerate(L):
  if r is None:n['unreadable rows']+=1;continue
  m=r.get('payload' if H=='codex' else 'message');m=m if I(m,dict) else {};c=m.get('content') if H=='claude' else None
  it=[j for j in c if I(j,dict)] if I(c,list) else [];a=str(r.get('type'))
  b=[m['type']] if H=='codex' and I(m.get('type'),str) else sorted({str(j.get('type')) for j in it});bt=b*(H=='codex')
  if H=='claude' and a=='system' and I(r.get('subtype'),str):b=bt=[r['subtype']]
  if r.get('isCompactSummary') is True:b=['compact_summary']
  k=(a,'+'.join(b));ks.append(k);SZ[k].append(len(x));[nf['t:'+v].add(tok) for v in [a]+bt]
  for p,o in [('',r),('p.',m)]+[('c:%s'%j.get('type'),j) for j in it]+[('p.source.',m.get('source'))]*I(m.get('source'),dict):
   for y,z in o.items():
    if p[:2]!='c:':nf['k:'+y].add(tok)
    RK[k,p,y]+=1;TR[k,p,y]+=z is True;FN[p,y]+=i==0
  if i==0:big.append(len(x))
  n['sidechain rows in main files']+=r.get('isSidechain') is True and not sub
  for y,z in strs(r):NT[k,y]+=NOTE in z
  w=ts(r.get('timestamp')) if I(r.get('timestamp'),str) else None
  if None not in (w,pt):n['rows earlier than the last']+=w<pt;n['rows at the same time']+=w==pt
  pt=pt if w is None else w;cw=r.get('cwd') or m.get('cwd');where|={(str(cw),str(r.get('gitBranch')))} if cw else set()
  e=[]
  if H=='claude' and a=='user' and not got:got=1;e=[('entrypoint',r.get('entrypoint'))]
  if H=='codex' and a=='session_meta':e=[(y,m.get(y)) for y in ('source','originator','cli_version')]
  for y,z in e:VL[y,val(y,z)]+=1
 n['files whose folder or branch changes']+=len(where)>1
 st=[i for i,k in enumerate(ks) if k in (('event_msg','task_started'),('event_msg','turn_started')) or k[0]=='user' and 'tool_result' not in k[1] and k[1]!='compact_summary']
 for i,j in zip(st,st[1:]+[len(ks)]):
  q=[]
  for k in ks[i:j]:q+=[k]*(not q or q[-1]!=k)
  SQ[tuple(q[:12])+(('...',''),)*(len(q)>12)]+=1
 for i,k in enumerate(ks):CP[k,ks[i-1] if i else('none',''),ks[i+1] if i+1<len(ks) else('none','')]+=k[1] in('compact_summary','context_compacted','compact_boundary') or k[0]=='compacted'
def ok(s,p='k:'):
 if s in KN or s in('...','none'):return s
 return s+'*' if (TW if p=='t:' else W).match(s) and len({R(t) for t in nf[p+s]})>=5 else 'other'
def N(p,y):return 'c[%s].%s'%(ok(p[2:],'t:'),y if y in KN else 'other') if p[:2]=='c:' else p+ok(y)
def K(k):return ':'.join([ok(k[0],'t:')]+['+'.join(ok(v,'t:') for v in k[1].split('+'))]*(k[1]!=''))
def B(v):return next(l for m,l in((1024,'<1KB'),(8192,'1-8KB'),(65536,'8-64KB'),(1<<20,'64KB-1MB'),(1<<62,'>1MB')) if v<m)
def say(t,c):
 z=C()
 for k,v in c:z[k]+=v
 for k,v in z.most_common():print(t,'|',k,'|',F(v)) if v else 0
def fold(t,c):
 z=D(C)
 for (k,y),v in c:z[k][y]+=v
 for k,d in sorted(z.items()):print(t,'|',k,'|',', '.join('%s %s'%(y,F(v)) for y,v in d.most_common() if v)) if any(d.values()) else 0
b=sorted(big) or [0];print({k:F(v) for k,v in n.items()},'| first row bytes: median',B(b[len(b)//2]),'largest',B(b[-1]),'over 64 KiB',F(sum(v>65536 for v in b)))
for k,v in UN.items():print('files not read |',k,'|',F(v))
fold('first row has',((('names',N(p,y)),v) for (p,y),v in FN.items()))
Z=D(list)
for k,v in SZ.items():Z[K(k)]+=v
for k,v in sorted(Z.items(),key=lambda z:-len(z[1])):v.sort();print('rows of type |',k,'|',F(len(v)),*['| bytes: median',B(v[len(v)//2]),'largest',B(v[-1])]*(len(v)>2))
fold('names of type',(((K(k),N(p,y)),v) for (k,p,y),v in RK.items()))
fold('names true, of type',(((K(k),N(p,y)),v) for (k,p,y),v in TR.items()))
Z=C()
for q,v in SQ.items():Z[' > '.join(K(k) for k in q)]+=v
for q,v in Z.most_common(5):print('turn |',q,'|',F(v))
say('compaction row, before, after',((tuple(K(k) for k in x),v) for x,v in CP.items()))
say('notice in type, name',(((K(k),ok(y)),v) for (k,y),v in NT.items()))
say('value',(((y,z),v) for (y,z),v in VL.items()))
```

It settles F-020, F-133 (rows out of time order or at one time); F-021 (rows with a time, folder, branch; files whose folder or branch changes); F-022 (text, tool calls and results as rows; a tool row's status); F-084, F-085 (Claude Code's names, flags, row types, sizes); F-132, F-134 (Codex's versions, file kinds, first row); F-034, F-136 (compactions); F-027 (subagent files, sidechain rows, `source`); F-046 (the row and field holding eva-md's start notice, meant only to be shown); F-018, F-079 (values); F-149 (`realtime_item` rows).

**5. Claude Code beside eva-md.**
- F-001: of hook entries with a `turn`, the share whose `turn` is the `promptId` of a row in the session's own file.
- F-013: the hosts' session ids (file names, leaving out `agent-` files) in section 2's three classes.

**6. Codex beside eva-md.** Match inbox files and session files by id inside the script. Class each session by the first that applies, from `session_meta`: subagent (`source` an object with `subagent`); program (`source` `exec` or `mcp`, or `originator` `codex_exec` or `codex_sdk_ts`); desktop app (`originator` `Codex Desktop`); IDE (`source` `vscode` or `originator` `codex_vscode`); terminal (`source` `cli`, `originator` `codex_cli_rs`); else other. Print only the classes, never a raw value.
- F-013: the `session_meta` ids in section 2's three classes.
- F-137: by class, inbox sessions with a session file and without.
- F-138: inbox sessions with a `session_end` and no `session_start`: how many have a session file, and of those how many hold a turn-start row.
- F-001: the share of turn-start rows (`task_started` or `turn_started`) that carry a `turn_id`, and of hook entries with a `turn`, the share that equal a turn-start row's `turn_id` in the same file.

**7. Questions for the owner.** List these, with room for a line under each:
1. F-038: Did eva-md's MCP tools ask for permission the first time, on each host? Once per tool, once per chat, or never? Was "always allow" offered?
2. F-176, F-183, F-202: (a) Does the daily commit or push ask you to sign or sign in? (b) Open your wiki project's settings in your browser, reading only, at its push rules and branch protection. Answer yes, no or don't know, never a setting's name: does a rule require a committer address the server knows? Does one require signed commits? Is the default branch protected against your pushes? Are forced pushes allowed on it? (c) In System Settings, General, Device Management, is any profile listed?
3. F-177: Is the git server reachable only on a private network?
4. F-178, F-180: Has a git push or fetch on this Mac ever waited for a password, a key's passphrase, a touch or a dialog? When the wiki was first pushed, were you asked to accept the server's host key?

## The report

Write `eva-host-shapes.md` here (`eva-host-shapes-run2.md` if that exists), in Markdown, headings 0 to 7, tables where numbers repeat. Then "Facts": a line per fact, in number order: `F-...` (and the host, where the halves differ): what was seen, as one line of shapes; in how many files or sessions; measured, observed or inferred. Give every fact this file names; for one you could not settle, say why. No prose about what any chat was about. End with a line: you read it again against rules 3, 4 and 10, and how many things you removed, by class, never the thing. Delete `$T`. Then give me a five-line summary in the chat.
~~~
