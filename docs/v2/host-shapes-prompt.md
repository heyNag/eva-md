# Host shapes prompt, a companion to the second field report

**Status:** for the counters release (RELEASE-NUMBER) or later. A read-only look at the shapes of eva-md's and the hosts' records on the one real install (a Mac whose wiki is on a self-hosted GitLab), for facts [host-facts.md](host-facts.md) marks "by the field report" that [the second report](field-report-2-prompt.md) has no room for. Message: [host-shapes-message.md](host-shapes-message.md). Checked in four rounds, in made-up homes with planted values.

Run it on the same Mac and folder as the second report, after it or another day, after a day of normal use since the upgrade. Expect 30 to 60 minutes.

## How to use it

1. Open a new Codex chat in the desktop app, in the git repository you used for the second report, from your normal home, in default mode, not full access: its sandbox blocks the network and writes outside the chat's folder and temp.
2. Attach this file with its message, or paste the part between the lines of tildes as your first message.
3. In default mode most commands run without asking, and nothing limits what is read: you are asked only about commands that need more. Approve only those in rule 1; refuse the rest and any request for access to a folder. Afterwards, skim the chat's commands for one that opens a file not named below.
4. It writes `eva-host-shapes.md` in the chat's folder; move it out of the repository before any commit there. You are the last check: strike any name, path, title, quote, model or service name. Answer its last section a line each, in general terms, and bring the file back.

## What to know before you run it

- **The chat is captured like any other**, and may go into the wiki and be pushed by the daily job; the report leaves it out of its counts.
- **What the agent reads**: eva-md's records and config (`exclude`, `wiki_path`), the hosts' session files, one setting of Codex's config, the wiki's remote (printed only as its scheme), versions, and whether git, python3, gh and glab are found. Its scripts print counts, buckets, days, yes or no, and words from lists in this file, never one they found.
- **What is written**: the report; and scripts, left-out ids and a git check (made-up files, a throwaway key, not your SSH agent) in a temporary folder the agent deletes.

~~~
I need a shapes report on eva-md, the memory layer installed on this Mac for Codex and Claude Code. A team rebuilding it must learn how the hosts' records are built and how eva-md's line up with them: shapes only, counts, yes or no, names from fixed lists; nothing a chat or file says, nothing that names a person, project, folder or machine.

eva-md's hooks record each chat into a local inbox, secrets removed; a background job turns chats into a wiki, a git repository a daily job commits and pushes.

## Where things are

- The program: `~/.local/share/eva-md/bin/eva-md`, maybe on PATH as `eva-md`; if not, run it by that path. Config: `~/.config/eva-md/config.toml`: `wiki_path` names the wiki, `exclude` lists folders left out of capture.
- State, `~/.local/state/eva-md/`:
  - `inbox/<project>/<host>-<session>.jsonl`: entries with `ts`, `host`, `session`, `turn`, `role`, `kind`, `agent` (a subagent's id), `origin` (`hook` or `catchup`), `cwd` (relative to the home folder), `text`. An event is kind `event` with a fixed text, as `session_start: <source>`, `session_end`, `compact`, `gap: missing final reply`, `off_record`. Kind `host_summary` is a compaction's summary.
  - `heartbeats/<host>/<event>.json`: `transcript_available` (the last payload named the host's file), and on Claude Code `entrypoint` and `headless`. `heartbeats/<host>/uncaptured.json`: `at`, the time of the last hook run that captured nothing.
  - `commits/<YYYY-MM>.jsonl`: each daily commit and push: `at`, `outcome`, `push` (`ok`, `up_to_date`, `pending`, `diverged`, `failed`, `skipped`).
- Claude Code: `~/.claude/projects/<folder>/<session>.jsonl`; a subagent's file is in `<session>/subagents/`, or, from older versions, `agent-<id>.jsonl` beside the sessions. A row has `type`, `sessionId`, `uuid`, `promptId` and `message`, whose `content` is text or items with a `type`.
- Codex: `~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-...-<session>.jsonl` (or `.jsonl.zst`) and `~/.codex/archived_sessions/`. A row has `type` and `payload` with its own `type`, as `task_started` or `turn_started` (with `turn_id`) and `user_message`. `session_meta`'s payload: `id`, `cwd`, `source`, `originator`, `cli_version`.

## Rules

1. **Read only.** You may run: `mktemp -d`; `cat > "$T/<name>" <<'EOF'` (or your file tool) to save a script in `$T`; section 0's `c.sh` (`xcode-select -p`, and git and `ssh-keygen` only inside `$T`) and `v.sh` (versions and `command -v`); `eva-md audit capture` with no flag, `eva-md status --updates`; `python3 -I` on your scripts; `rm -r "$T"`. Between them, `echo`, `if`, `case`, `for`, pipes, `wc -l` at a pipe's end (never with a file name) and `>` into `$T` or the report are fine. `git -C <wiki> --no-optional-locks config --get remote.origin.url` runs only inside section 3's script. Run nothing bare that prints a path, a version or a config line. Nothing else: no other eva-md or host command, no web search or fetch, no network command, no subagent. Change no file or setting. Fix nothing: a failure is a finding.
2. **Order.** Make `$T` (rule 8), then section 0, section 1 (copy eva-md's numbers as printed; they include this chat and the sessions rule 6 leaves out: say so), rule 6's step, and the rest. Script only what eva-md's reports do not give.
3. **Shapes only.** A script may open eva-md's files, the hosts' session files and the other files a section names, never a chat's folder. It prints only counts, shares, durations and sizes in this file's buckets, yes or no, days or counts of days, versions as ASCII digits and dots (git's only as at or above 2.36 or below), and words from this file's lists (rule 4). Never a value of a field, whole or in part, hashed or not: no text, path, folder, repository, branch, project, session or turn id, account, model, effort, plan, tool name, server, address or key. **Errors.** Every script starts with s.py's first five lines and prints only with its `P`, which withholds a line with a character outside printable ASCII, a word not in that script, or a number not standing alone (a time, a date, an address). It cannot tell a count from an id of digits: only the lists make a script safe. An error prints only its class and line. Read files as s.py's `rows` does. When a script fails or a count looks wrong, never print a row, line, config, id, listing or value to see why: fix it from the class and line.
4. **Names and values only from lists.** Keys can be a person's data, so nothing prints because a script found it. A name (a row's or payload's `type`, a field's name, a system `subtype`, a content item's `type` or key) prints only if it is on section 4's list `L` for its level; a value only if a section gives its field's list. Anything else is `other`, counted per level: how many other names, in how many chats. Never add to a list what you saw. A chat is a session, not a file: files sharing a session id or a row's `uuid` (copies, resumes, forks), with their subagents' files, are one chat. Open only a row's top, Claude Code's `message` and its items, and Codex's `payload`; every other field is free data, never opened, its text tested only against a fixed string this file gives.
5. **Fewer than 3.** A count under 3 of anything that could tell what a chat was about is written "fewer than 3".
6. **Sessions left out.** Save section 4's script as `$T/s.py` and run `python3 -I $T/s.py skip $T/skip`. It lists in `$T/skip`, one id a line, each session with an `off_record` entry or a prompt that starts with, or has a line that is, "off the record"; each whose first prompt is a field or shapes report's; each whose first folder, or a parent, matches `exclude` (all, if `exclude` cannot be read); with a `remote:` entry, each host session with no inbox file but eva-md's own runs; and every file of the same chat (rule 4). Copy its counts. If the remote rule left any out, say at F-013 and F-085 that the hosts' counts cover only sessions with an inbox file, with that count. Every count of a script leaves these out: your own scripts skip a session file whose name's id, any `sessionId` in it, or its parent's (the `<session>/subagents/` folder, Codex's `parent_thread_id` in `source`) is in `$T/skip`.
7. **Compare inside.** A script compares ids, folders and times inside itself, and a text only with a fixed string this file gives: never two texts of chats, never a hash, never a kept copy.
8. **Write as you go.** `$T`, from `mktemp -d`, holds only your scripts, `$T/skip` and the git check; delete it at the end. Scripts are for Python 3.9. In this chat's folder write only the report: create it first, add each section when done. If this chat compacts, note it and go on from the last finished section.
9. If the sandbox blocks a read or a command, ask me. Do not work around it.
10. **Nothing private in the report.** No names of people, organizations, projects, repositories, branches, hosts, servers, services or models. No paths but eva-md's standard ones and the hosts' folders as written above, with `~`. "Unknown" and "could not check, because ..." are good answers. An example is invented and marked so.

## What to find out

Use these numbered headings, in order.

**0. This run and this machine.** Save the scripts below as `$T/v.sh`, `$T/c.sh` and `$T/g.sh`. Run `bash $T/v.sh` and copy what it prints: versions, `missing` for a program not found (F-182 for `gh` and `glab`). F-185, F-189: run `T=$T bash $T/c.sh`; copy what it prints (a missing check line is a fail). If it prints `no-clt` and `v.sh` printed `python3: system`, stop and tell me: without the tools, `/usr/bin/python3` would offer an install.

```sh
export GH_NO_UPDATE_NOTIFIER=1 GLAB_CHECK_UPDATE=false; E=$(command -v eva-md || echo ~/.local/share/eva-md/bin/eva-md)
v() { command -v "$2" >/dev/null || { echo "$1: missing"; return; }; echo "$1: $("${@:2}" 2>/dev/null | head -1 | LC_ALL=C grep -oE '[0-9]+(\.[0-9]+){1,2}' | head -1)"; }
v eva-md "$E" version; v codex codex --version; v claude claude --version; v gh gh --version; v glab glab --version
for a in Codex Claude; do v "$a app" defaults read /Applications/$a.app/Contents/Info.plist CFBundleShortVersionString; done
case "$(command -v python3)" in /usr/bin/python3) echo python3: system;; '') echo python3: missing;; *) echo python3: other;; esac
```

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

Then F-132: a script that prints `persistence` under `[history]` in `~/.codex/config.toml` only as `save-all`, `none`, `other` or `absent`.

**1. eva-md's reports.** Copy `eva-md audit capture` as printed, but count its per-session lines. From `eva-md status --updates` copy `outcomes`, `chat boundary to memory on file, by trigger` and `busy exits`, and of `model calls a day` only each host's sum and its number of days with a call. They give F-004 (final replies; turns with no stored reply, by cause), F-024 (`host sessions with no eva-md file`), F-030 (passes a hook's trigger started), F-032 (passes whose calls succeeded, per host).

**2. eva-md's inbox and heartbeats.** One script over the inbox, printing values only from these lists, else `other`: host `claude-code codex` (the file name's prefix, the heartbeats' folder); `kind` `prompt reply tool event host_summary`; `origin` `hook catchup`; event `SessionStart SessionEnd UserPromptSubmit PostToolUse PostToolUseFailure Stop StopFailure PreCompact PostCompact PostModelSwitch Interrupt`. By host:
- F-013: inbox files by the session part of their name: a UUID; only letters, digits, `_` and `-` but not a UUID; anything else.
- F-001: the share of entries with a non-empty `turn`, by kind and `origin`. In hook entries from one prompt to the next, events aside: by kind, entries with the prompt's `turn`, with none, with another.
- F-004: `reply` entries from hooks, and `gap: missing final reply` events.
- F-027: tool entries with a non-empty `agent`; how many of them have a `text` holding ` (subagent)` (eva-md's mark after a tool's name); sessions with such entries.
- F-034, F-136: `compact` events and `host_summary` entries; a summary's size, median and largest, in buckets.
- From the heartbeats, per host and event: F-005, `transcript_available` true or false; on Claude Code, F-018 and F-079, `entrypoint` (section 4's list) and `headless`.

**3. eva-md's commit and run records.**
- F-031, F-029, F-178, F-180: in the last 14 days of commit records, counts by `outcome` (`committed`, `nothing`, `refused`, `failed`, `busy`, else `other`) and by `push` (its list above, else `other`), the number of days with an `ok` push, and the longest run of days without one; never the dates. The wiki remote's scheme: a script reads `wiki_path` from the config itself, runs rule 1's git command, and prints only `ssh` (`git@...` or `ssh://...`), `https` or `other`, never the config, the path or the address.
- F-028: sessions of eva-md's own runs in each host's files (the first folder's last part starts `eva-md-run-`, `eva-md-codex-` or `eva-md-live-`), with an inbox file and without. Whether `uncaptured.json`'s `at` on each host falls between the first and last row of such a session (yes, no, unreadable, no file).

**4. The hosts' records.** This is `$T/s.py` (rule 6). Run `python3 -I $T/s.py claude $T/skip`, then with `codex`. It applies rules 3 to 5 itself. Its lists `L` (row types `rc` `rx`; top names `kc` `kx`; `message` and `payload` names `km` `kp`; payload types `px`; item types and keys `it` `ik`; system subtypes `st`) hold names eva-md reads and words the facts name. A field's value prints only from the list named after it, `cli_version` as ASCII digits and dots. Copy its output into tables, one set per host, in its order; a turn is its row types, repeats folded, 12 at most.

```python
import json,os,re,sys,glob,fnmatch,traceback;from collections import Counter as C,defaultdict as D;from datetime import datetime as DT
def E(t,b):print('error',t.__name__,'line',b and traceback.extract_tb(b)[-1].lineno)
sys.excepthook=lambda t,v,b:E(t,b);sys.unraisablehook=lambda u:print('error unraisable');WR=re.compile(r'[A-Za-z0-9_-]+').findall;SW={w for w in WR(open(sys.argv[0]).read()) if not w.isdigit()}
def OK(w):return re.fullmatch(r'([0-9]+|[0-9]{1,3}(\.[0-9]{1,3}){1,2})%?',w) or set(WR(w))<=SW
def P(*a):t=' | '.join(map(str,a));print(t if t.isascii() and t.isprintable() and all(map(OK,re.split(r'[\s|,()]+',t))) else 'line withheld')
def main():
 H,SK=sys.argv[1:3];I=isinstance;hm=os.path.expanduser('~');J=os.path.join;bn=os.path.basename;dn=os.path.dirname;cl=H=='claude'
 NOTE='eva.md: earlier work is available for this chat.'
 L={}
 for x in '''rc user assistant system summary progress attachment file-history-snapshot queue-operation
kc type uuid parentUuid sessionId promptId prompt_id timestamp cwd gitBranch version userType entrypoint isMeta isSidechain isCompactSummary repeatsCompactSummary isApiErrorMessage isVisibleInTranscriptOnly agentId agent_id turnOrigin origin subtype level message toolUseResult requestId content summary leafUuid
km role model content stop_reason stop_sequence id type usage
it text tool_use tool_result image document thinking redacted_thinking
ik type text id name input tool_use_id is_error content thinking signature
st compact_boundary
rx session_meta turn_context event_msg response_item compacted
kx timestamp type payload ordinal
px task_started turn_started user_message agent_message task_complete turn_aborted context_compacted message function_call function_call_output custom_tool_call custom_tool_call_output local_shell_call reasoning token_count exec_command_end
kp type id cwd source originator cli_version forked_from_id turn_id turn_attribution message phase last_agent_message role content name call_id arguments output status exit_code outcome
entrypoint cli sdk-cli sdk-ts sdk-py claude-desktop claude-vscode
source cli vscode exec mcp unknown atlas chatgpt
originator codex_cli_rs codex_vscode codex_exec codex_sdk_ts Codex~Desktop
turnOrigin human'''.split('\n'):k,*w=x.split();L[k]=set(w)
 AK=set().union(*map(L.get,'kc km ik kx kp'.split()))
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
  except Exception as e:UN[I(e,PermissionError) and 'PermissionError' or 'other']+=1;return
  for x in L:
   if x.strip():
    try:r=json.loads(x)
    except Exception:r=None
    yield x,r if I(r,dict) else None
 def strs(o,k='',d=0):
  if I(o,str):yield k,o
  if d>40:return
  for a,b in o.items() if I(o,dict) else enumerate(o) if I(o,list) else ():yield from strs(b,a if I(a,str) else k,d+1)
 def REP(m):return bool(re.search(r'I need a (field|shapes) report on eva-md|field-report(-2)?-prompt\.md|host-shapes-prompt\.md',m))
 def OFF(m):
  w=lambda l:' '.join(re.findall('[a-z]+',l.lower()))
  return w(m).startswith('off the record') or any(w(l)=='off the record' for l in m.splitlines())
 def who(f,L):
  st=bn(f).split('.')[0];ids={st,re.sub(r'^rollout-[0-9T:-]{19}-','',st)};par=bn(dn(dn(f))) if '/subagents/' in f else None;cw=None;mk=off=n=0;fp=1
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
  m=re.search(r'^\s*["\']?exclude["\']?\s*=\s*(\[)?',cf,re.M);ALL|='exclude' in cf
  if m:
   for a,b,c in re.findall(r'"((?:[^"\\]|\\.)*)"|\'([^\']*)\'|(#[^\n]*|\])',cf[m.end():])*bool(m.group(1)):
    if c==']':ALL='\\' in ''.join(ex);break
    if not c:ex.append(a or b)
  rem=any(e.strip().startswith('remote:') for e in ex);Q=set()
  def cp(p):i=re.search(r'[*?[]',p);a=dn(p[:i.start()]) if i else p;return J(os.path.realpath(a),os.path.relpath(p,a))
  for e in ex:
   e=e.strip();e=J(hm,e[2:]) if e[:2]=='~/' else hm if e=='~' else e
   if os.path.isabs(e):Q|={os.path.normpath(x).lower() for x in(e,cp(e))}
  def exc(d):
   for t in{os.path.normpath(x).lower() for x in(d,cp(d))}if Q and d[:1]=='/' else():
    while 1:
     if any(fnmatch.fnmatchcase(t,p) for p in Q):return 1
     if dn(t)==t:break
     t=dn(t)
   return 0
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
  Z={R(a) for a in S}
  for k in A:
   if any(R(a) in Z for a in k) and not k<=S:S|=k;R0['all','joined to one left out']+=1
  open(SK,'w').write(''.join(i+'\n' for i in S))
  for k,v in sorted(R0.items()):P('left out',*k,F(v))
  for k,v in UN.items():P('files not read',k,F(v))
  return
 sk=set(open(SK).read().split())
 n=C();big=[];SZ=D(list);RK=C();SQ=C();CP=C();NT=C();VL=C();OT=D(lambda:D(set))
 def M(v,y,t):
  if y in L[v]:return y
  OT[v][y].add(t);return 'other'
 def ts(s):
  try:return DT.strptime(s[:19],'%Y-%m-%dT%H:%M:%S').timestamp()+float('0'+s[19:-1])
  except Exception:return None
 def val(y,z):
  v=re.match(r'[0-9]{1,3}(\.[0-9]{1,3}){1,2}',z) if y=='cli_version' and I(z,str) else None
  if v:n['cli_version with a suffix']+=v.end()<len(z);return v.group()
  return 'absent' if z is None else('object:subagent' if 'subagent' in z else 'object:other') if I(z,dict) else z if I(z,str) and z.replace(' ','~') in L.get(y,()) else 'other'
 for fi,f in enumerate(files(H)):
  L0=list(rows(f));ids,par,_,_,_=who(f,L0);sub='/subagents/' in f or bn(f)[:6]=='agent-';t='f%d'%fi
  if (ids|{par})&sk:n['left out']+=1;continue
  join([t]+sorted(ids|{par}-{None}))
  x=os.path.splitext(f)[1];n['files'+' sub'*sub+' '+(x if x in('.jsonl','.zst') else 'other' if x else 'none')]+=1;ks=[];pt=None;where=set();got=0
  for i,(x,r) in enumerate(L0):
   if r is None:n['unreadable rows']+=1;continue
   m=r.get('message' if cl else 'payload');m=m if I(m,dict) else {};c=m.get('content') if cl else None
   it=[j for j in c if I(j,dict)] if I(c,list) else [];a=M('rc' if cl else 'rx',str(r.get('type')),t)
   b=sorted({M('it',str(j.get('type')),t) for j in it}) if cl else [M('px',m['type'],t)] if I(m.get('type'),str) else []
   if cl and r.get('type')=='system' and I(r.get('subtype'),str):b=[M('st',r['subtype'],t)]
   if r.get('isCompactSummary') is True:b=['compact_summary']
   k=a+(':'+'+'.join(b))*bool(b);ks.append(k);SZ[k].append(len(x))
   for p,v,o in [('','kc' if cl else 'kx',r),('p.','km' if cl else 'kp',m)]+[('c[%s].'%M('it',str(j.get('type')),t),'ik',j) for j in it]:
    for y,z in o.items():y=p+M(v,y,t);RK[k,y+'=true'*(z is True)]+=1
   if i==0:big.append(len(x))
   n['sidechain rows in main files']+=r.get('isSidechain') is True and not sub
   for y,z in strs(r):NT[k,y if y in AK else 'other']+=NOTE in z
   w=ts(r.get('timestamp')) if I(r.get('timestamp'),str) else None
   if None not in (w,pt):n['rows earlier than the last']+=w<pt;n['rows at the same time']+=w==pt
   pt=pt if w is None else w;cw=r.get('cwd') or m.get('cwd');where|={(str(cw),str(r.get('gitBranch')))} if cw else set()
   e=[('turnOrigin',r.get('turnOrigin'))]+[('entrypoint',r.get('entrypoint'))]*(not got) if cl and r.get('type')=='user' else []
   got|=bool(e)
   if not cl and r.get('type')=='session_meta':e=[(y,m.get(y)) for y in ('source','originator','cli_version')]
   for y,z in e:VL[y,val(y,z)]+=1
  n['files whose folder or branch changes']+=len(where)>1
  st=[i for i,k in enumerate(ks) if k in('event_msg:task_started','event_msg:turn_started') or k[:4]=='user' and not re.search('tool_result|compact_summary',k)]
  for i,j in zip(st,st[1:]+[len(ks)]):
   q=[]
   for k in ks[i:j]:q+=[k]*(not q or q[-1]!=k)
   SQ[' > '.join(q[:12]+['...']*(len(q)>12))]+=1
  for i,k in enumerate(ks):CP[k,ks[i-1] if i else 'none',ks[i+1] if i+1<len(ks) else 'none']+=k=='compacted' or bool(re.search(':(compact_summary|context_compacted|compact_boundary)',k))
 def B(v):return next(l for m,l in((1024,'<1KB'),(8192,'1-8KB'),(65536,'8-64KB'),(1<<20,'64KB-1MB'),(1<<62,'>1MB')) if v<m)
 def fold(t,c):
  z=D(C)
  for (k,y),v in c:z[k][y]+=v
  for k,d in sorted(z.items()):any(d.values()) and P(t,k,', '.join('%s %s'%(y,F(v)) for y,v in d.most_common() if v))
 for k,v in sorted(n.items()):P(k,F(v))
 b=sorted(big) or [0];P('first row bytes: median',B(b[len(b)//2]),'largest',B(b[-1]),'over 64 KiB',F(sum(v>65536 for v in b)))
 for k,v in UN.items():P('files not read',k,F(v))
 for k,v in sorted(SZ.items(),key=lambda z:-len(z[1])):v.sort();P('rows of type',k,F(len(v)),*['bytes: median',B(v[len(v)//2]),'largest',B(v[-1])]*(len(v)>2))
 fold('names of type',RK.items())
 for q,v in SQ.most_common(5):P('turn',q,F(v))
 for x,v in CP.most_common():v and P('compaction row, before, after',*x,F(v))
 fold('notice in type, name',NT.items())
 fold('value',VL.items())
 for v,d in sorted(OT.items()):P('other names at level',v,F(len(d)),'distinct, in chats',F(len({R(t) for s in d.values() for t in s})))
try:main()
except BaseException as e:E(type(e),e.__traceback__)
```

It settles F-020, F-133 (time order); F-021 (time, folder, branch); F-022 (tool rows and status); F-084, F-085 in part (listed ids, flags, row types; how much is `other`); F-132, F-134 (versions, file kinds, first row); F-034, F-136 (compactions); F-027 (subagent files, sidechain rows, `source`); F-046 (where eva-md's start notice sits); F-018, F-079 (values).

**5. Claude Code beside eva-md.**
- F-001: of hook entries with a `turn`, the share whose `turn` is the `promptId` of a row in the session's own file.
- F-013: the hosts' session ids (file names, leaving out `agent-` files) in section 2's three classes.

**6. Codex beside eva-md.** Match inbox and session files by id inside the script. Class each session by the first that applies, from `session_meta`: subagent (`source` an object with `subagent`); program (`source` `exec` or `mcp`, or `originator` `codex_exec` or `codex_sdk_ts`); desktop app (`originator` `Codex Desktop`); IDE (`source` `vscode` or `originator` `codex_vscode`); terminal (`source` `cli`, `originator` `codex_cli_rs`); else other.
- F-013: the `session_meta` ids in section 2's three classes.
- F-137: by class, inbox sessions with a session file; and, as one count, those without.
- F-138: inbox sessions with a `session_end` and no `session_start`: how many have a session file, and of those how many hold a turn-start row.
- F-001: the share of turn-start rows (`task_started` or `turn_started`) that carry a `turn_id`, and of hook entries with a `turn`, the share that equal a turn-start row's `turn_id` in the same file.

**7. Questions for the owner.** List these, with room for a line under each:
1. F-176, F-183, F-202: (a) Does the daily commit or push ask you to sign or sign in? (b) In your wiki project's settings in your browser, reading only, answer yes, no or don't know, never a setting's name: does a rule require a committer address the server knows? Does one require signed commits? Is the default branch protected against your pushes? Are forced pushes allowed on it? (c) In System Settings, General, Device Management, is any profile listed?
2. F-177: Is the git server reachable only on a private network?
3. F-178, F-180: Has a git push or fetch on this Mac ever waited for a password, a key's passphrase, a touch or a dialog? When the wiki was first pushed, were you asked to accept the server's host key?

## The report

Write `eva-host-shapes.md` here (`eva-host-shapes-run2.md` if that exists), in Markdown, headings 0 to 7, tables where numbers repeat. Then "Facts": a line per fact, in number order: `F-...` (and the host, where the halves differ): what was seen, as one line of shapes; in how many files or sessions; measured, observed or inferred. Give every fact this file names; for one you could not settle, say why. No prose about what any chat was about. End with a line: you read it again against rules 3, 4 and 10, and how many things you removed, by class, never the thing. Delete `$T`, and tell me in one line that the report is written.
~~~
