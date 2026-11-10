# Field report prompt

This is the prompt for the one machine where eva-md runs for real. It asks the agent there for a general report on how eva-md behaves in use, with nothing private in it. The report comes back to the design team and is folded into [the plan](plan.md) before the plan is agreed. Section 8 of the plan lists what each answer would change.

How to use it:

1. Open a Codex chat in a repository where you have used Codex or Claude Code for a while, so the chat gets a real brief.
2. Paste everything between the two lines of tildes as your first message.
3. If the sandbox asks to approve a read-only command, approve it. Nothing in the prompt needs a write outside the chat's folder.
4. Bring back the file it writes, `eva-field-report.md`. Read it once before you share it: the prompt forbids private detail, and you are the last check. Answer the questions in its section 11 yourself, a line each.

Three things to know before you run it:

- **The chat is captured like any other.** eva-md will record this chat and may summarize it into the wiki. That is deliberate: pausing capture would also switch off the recall the report needs to observe.
- **The agent reads some memory.** To judge quality it reads a few wiki pages of the current project, and it counts things in the inbox and in the hosts' own session files by field name. It is told not to read or print chat text, and not to read other projects' pages. It runs on the model provider of the chat you paste it into.
- **It changes nothing of yours.** It runs only eva-md's read and preview commands and read-only git. One of them, `eva-md doctor`, rebuilds eva-md's own index files in the wiki when they are out of date, as it always does. The prompt has the agent run `eva-md status` first, which never writes, so the report shows whether the doctor repaired anything.

~~~
I need a field report on eva-md, the memory layer installed on this Mac for Codex and Claude Code. A team is redesigning it, and this machine is the only place it runs for real, so what you find here will decide what gets rebuilt. The team cannot see this machine. The report must be useful to them and must carry nothing private from here.

## What eva-md is

- Hooks in Codex and Claude Code record each chat into a local inbox, with secrets removed.
- At the start of a chat it adds a short brief to your context: where the last chat on this branch stopped, standing rules, constraints, live git state. It is labelled eva.md and marked as reference data.
- After a chat, a background job runs the host's own model headless to summarize the chat into a wiki: one thread page per branch, rules pages, knowledge pages (one lasting fact, decision, procedure or constraint each) and system pages. The wiki is a separate git repository, committed and pushed once a day.
- A prompt that strongly matches a knowledge page gets that page's statement added to your context. Prompts that name a system get a pointer to its page.
- It also gives you tools: the commands `eva-md search` and `eva-md show`, and, when its MCP server is connected, `eva_search`, `eva_show` and `eva_status`.

## Where things are

- The program: `~/.local/share/eva-md/bin/eva-md` (it may also be on PATH as `eva-md`).
- Its config: `~/.config/eva-md/config.toml`. It names the wiki folder.
- Its state: `~/.local/state/eva-md/`, with `inbox/` (captured chats as JSONL, one file per session, in a folder per project), `updates/` (one JSONL run log per day for the background job), `logs/`, `commit/` (records of the daily commit and push), `heartbeats/<host>/` (one file per hook event), `catchup/`, `sessions/`, `cache/`, and possibly `needs-trust.json` and `code-status.json`.
- The wiki: the folder the config names.
- The hosts' own session files: under `~/.claude/projects/` and `~/.codex/sessions/`.

## Rules for this task

1. **Read only.** You may run `eva-md version`, `eva-md status`, `eva-md doctor` (once, never with `--live`, and only after `eva-md status`, because the doctor rebuilds eva-md's own index files in the wiki when they are out of date and status never writes), `eva-md validate`, `eva-md brief`, `eva-md search` and `eva-md show`; read files under the folders above; and run read-only git commands in the wiki (`log`, `status`, `rev-list`, `config --get`). Do not run any other eva-md command: no setup, init, install, uninstall, upgrade, update, commit, maintain, index, system, inbox, mcp or hook. Do not change eva-md's files, the wiki, or any Codex or Claude Code setting. Do not contact the wiki's git server. Do not fix anything you find. A failure is a finding: write it down.
2. **Measure before you read.** For the inbox, the run logs, the heartbeats and the hosts' session files, write small scripts that print counts, sizes, dates, durations and field names. A script may look at a text field to count or measure it. It must never print one, and you must not read chat text yourself. Look at the hosts' session files for at most five recent sessions of this project. In the wiki, read the text of at most five pages, all from the project this chat is in, and only to judge their quality.
3. **Nothing private in the report.** No names of people, organizations, projects, repositories, branches, hosts or services. No URLs. No paths except eva-md's own standard ones written with `~`. No quotes from chats, prompts, wiki pages, commit messages or logs. No secrets. Use counts, sizes, durations, percentages, dates, statuses, and eva-md's own row names, reason codes and field names. Where an example would help, invent a neutral one and mark it as invented.
4. **Say what you know and how.** Mark each finding as measured (a number you computed), observed (you saw it happen in this chat) or inferred. "Unknown" and "could not check, because ..." are good answers. Do not guess.
5. If the sandbox blocks a read, ask me to approve it. Do not work around it.

## What to find out

Use these numbered headings in the report, in this order.

**0. Environment.** The macOS version. The versions of eva-md, git, Codex and, if installed, Claude Code and the desktop apps. Whether `ssh-keygen` exists. Which surface this chat runs in (terminal, desktop app, IDE extension), if you can tell, and which shell runs your commands. Whether `eva-md` is found by name in this chat's shell. Whether the machine has managed settings for either host, or a device profile (yes, no or unknown). Whether the user's prompts are written in a language other than English (yes or no). How long eva-md has been installed, from the oldest file in its state folder. The wall time of 50 runs of `eva-md version`, as a floor for what one hook start costs.

**1. What this chat was given.** You are the agent eva-md is meant to help, so start with your own experience.
- Did your context at the start of this chat hold a block from eva.md? Give its rough size, the kinds of lines in it, and whether it fitted the folder and branch you are in. Was anything in it stale, wrong, repeated, confusing, or worded as an instruction?
- Did this prompt bring recalled knowledge, a pointer to a system page, or a reminder line?
- Are the tools `eva_search`, `eva_show` and `eva_status` available to you, loaded or deferred? Is there a recall skill in your skills list? Did using any of them raise a permission question?
- Did any notice from eva-md appear?
- Run `eva-md brief --cwd . --as startup --host codex`, time it, and compare its size and shape with what you were given.

**2. Health.** Run `eva-md status`, then `eva-md doctor`, and time both. Say whether the doctor reported repairing an index that status had shown as out of date. Give the totals of PASS, FAIL and UNKNOWN, and for each row that is not PASS its own name and the reason it states, in general terms. Say whether the fix each row suggests is something the program could have done itself.

**3. Capture.**
- Sessions in the inbox per host, total size, the largest session, and the range of dates. Disk used by the inbox, the wiki and the logs.
- Entries by `role`, by `kind` and by `origin` (a count for each value). How many carry a redaction count, how many were cut short, how many are gap entries and for what reasons. How many carry an agent id.
- For each host, which hook events have a heartbeat file, the newest time in each, and any failure flags or durations they record: the median and the 95th percentile per event if the data allows.
- Which hook events never appear at all. Counts of session starts by kind (startup, resume, clear, compact, fork), if recorded.
- How chats end: the share of sessions with an end event, with a compaction, and left open. The share on the default branch, on other branches, and outside any repository.
- How many chats had capture paused at some point, and how many of those were paused by transcript recovery and not by a typed request.
- For up to five recent sessions of this project, compare the host's own session file with eva-md's inbox file, by counts only: user turns, final replies and tool calls in each, and any duplicates. Say what fields the host's rows carry (names only), whether rows have ids, and whether any file was missing, compressed or unreadable. Count how often the host's question, plan-approval and delegation tools appear in those files.
- How many typed prompts are five words or fewer (a count and a share).
- How many project folders the inbox holds, how many are for folders with no remote, and how many distinct working folders sit in the general area.
- Redaction markers by kind in the inbox and in the wiki (counts), and the share that sit as the value of a `key: value` line.
- Anything that suggests chats were missed: days with git activity in this repository and no captured session, sessions with a start and nothing else.

**4. The background job.** Read the run logs in `~/.local/state/eva-md/updates/`. If there are none, say so and look for the reason: the config's switches, the job's logs (reason codes only), the doctor's rows.
If there are records, give:
- Records per day, per host, per outcome and per reason.
- The models and effort levels asked for and used, and any fallback notes.
- Duration: median, 95th percentile and maximum. Whether any run hit its time limit.
- Tokens in and out: the median per run and the total per day. Calls per run and chunk size.
- The time from a chat's newest entry to its saved page (the field named `boundary_to_saved_ms`, if present): median and 95th percentile.
- The totals of every numeric field that counts what the checker did: what it refused, what it could not attribute, grounding misses by class, rule changes and knowledge changes added, refused, duplicate, stale, ignored or over the cap.
- How many runs failed outright, were skipped or put a session in quarantine, and what share of chats ended with no page written.
- Calls per day against the daily cap, and any sign that the plan's limits were hit.
- The backlog now, from `eva-md status`: sessions waiting, the oldest one's age, and how many short chats were never processed.
- Whether either host's CLI changed version in the period, and whether outcomes changed around that date.

**5. The wiki.**
- Counts: projects, thread pages, rules, knowledge pages by kind, scope, state and provenance tag, system pages. How many knowledge pages cite code paths. The distribution of statement lengths. Sizes of the largest pages. The oldest and newest `as_of` dates. The number of commits and the repository's size.
- How many thread pages belong to branches that are merged or gone. Whether any thread page is at its cap of decisions. How many statements look like duplicates of another by a simple normalized match.
- How many pages are locked, verified or reported as damaged, and how many carry sections a person wrote. How many rows the recall index holds. Whether lock or temporary files are lying in the wiki folder. What `eva-md validate` counts.
- Then read up to five pages of this project (its thread page for this branch, its rules page, and up to three knowledge pages) and judge them in general terms. Are they right, as far as you can check against the repository? Current or stale? Vague or specific? Do they repeat each other or what the repository's own instruction files already say? Would they help an agent new to this project? Do the provenance tags fit? Does anything look as if it came from a tool's output or a web page and not from the user?

**6. Finding things.** Make up six searches from what you now know of this project: two for things the memory should hold, two that ask for the same things in other words, and two for things it should not hold. Run each with `eva-md search`, and with `eva_search` if you have it. For each give hit or miss, the rank of the right result, how much noise came with it, and the time taken. Do not record the queries. Say how many calls it takes to get from a question to a usable answer.

**7. The daily commit and push.** From the records in `~/.local/state/eva-md/commit/` and read-only git in the wiki: on how many of the last 30 days was there a commit, how many commits wait to be pushed and how old the oldest unpushed change is, how many files are uncommitted, what classes of reason failures give, whether commits are signed and by which method (none, GPG or SSH), whether the remote is reached over SSH or HTTPS, and whether any commit in the wiki was made by hand. If a push has failed, say for how long and with what class of reason.

**8. Codex.** Does the doctor report eva-md's hooks as trusted? Is `needs-trust.json` present, and how old is it? Has capture on Codex stopped at any point after an upgrade of eva-md or of Codex? Does eva-md's plugin folder under Codex's plugin cache hold any file eva-md did not put there (a count is enough)? Are Codex's own memories switched on, and how many memory files are there? Is this chat's folder a worktree that Codex manages? How many hook-output spill files are in Codex's temporary folder?

**9. Claude Code**, if it is installed. Is eva-md's plugin listed? Do its hook events have recent heartbeats? Does Claude Code's own auto memory folder exist for this project, and how many files and index lines does it hold? Do eva-md's brief and that memory say the same things, in general terms?

**10. Cost.** Tokens and model calls per day spent by the background job. Any sign that it met a rate limit or held back. The size of what eva-md adds to a chat: the brief by kind of start, and each addition to a prompt. On what share of typed prompts anything was added.

**11. Questions for the owner.** You cannot answer these, so list them at the end of the report for the person to answer in a line each:
- What have you had to do by hand since installing: commands typed, and approvals given for signing, SSH, hook review or tool permission, and roughly how often?
- Which notices have you seen, in which host, and did any never go away or seem wrong?
- Does a signing or sign-in prompt appear with the daily commit or push? Is the git server reachable only on a VPN, and does it restrict who may push or require signed commits?
- Has the agent ever plainly remembered something useful? Plainly failed to? Acted on something stale? Re-decided something already decided? Been given something irrelevant?
- Has anything been remembered that should not have been?
- Has a second machine ever been pointed at this wiki? Do you read the wiki in a browser on the git host or edit its pages by hand?
- Has `eva-md doctor --live` or the live accuracy run ever been run, and what did it say?
- How was each upgrade so far done, and what did it ask or break?

**12. Your judgement.** As the agent this memory is for: the ten shortfalls that matter most, in order, each with what it costs you; the five things that work well and should not be lost; anything that surprised you; what you needed in this chat that memory could have given you and did not; and which counts you wanted and could not get from eva-md's files.

## The report

Write it to `eva-field-report.md` in the current folder, as Markdown with the headings above. Keep it under about 700 lines. Do not commit it. End it with a line saying you read the whole report again against rule 3, and what you removed, if anything. Then give me a summary of five lines in the chat.
~~~
