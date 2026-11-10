# Field report prompt

This is the prompt for the one machine where eva-md runs for real. It asks the agent there for a general report on how eva-md behaves in use, with nothing private in it. The report comes back to the design team and is folded into [the plan](plan.md) before the plan is agreed.

How to use it:

1. Open a Codex chat in a repository where you have used Codex or Claude Code for a while, so the chat gets a real brief.
2. Paste everything between the two lines of tildes as your first message.
3. If the sandbox asks to approve a read-only command, approve it. Nothing in the prompt needs a write outside the chat's folder.
4. Bring back the file it writes, `eva-field-report.md`. Read it once before you share it: the prompt forbids private detail, and you are the last check.

Two things to know before you run it:

- **The chat is captured like any other.** eva-md will record this chat and may summarize it into the wiki. That is deliberate: pausing capture would also switch off the recall the report needs to observe.
- **The agent reads some memory.** To judge quality it reads a few wiki pages of the current project and aggregates the inbox by field names and counts. It is told not to read chat text and not to read other projects' pages. It runs on the model provider of the chat you paste it into.

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

## Rules for this task

1. **Read only.** You may run `eva-md version`, `eva-md status`, `eva-md doctor` (never with `--live`), `eva-md brief`, `eva-md search` and `eva-md show`; read files under the three folders above and in the wiki; and run read-only git commands in the wiki (`log`, `status`, `rev-list`). Do not run any other eva-md command: no setup, init, install, uninstall, upgrade, update, commit, maintain, index, system, inbox, mcp or hook. Do not change eva-md's files, the wiki, or any Codex or Claude Code setting. Do not fix anything you find. A failure is a finding: write it down.
2. **Measure before you read.** For the inbox, the run logs and the heartbeats, write small scripts that print counts, sizes, dates, durations and field names. Do not print or read the text of captured chats. In the wiki, read the text of at most five pages, all from the project this chat is in, and only to judge their quality.
3. **Nothing private in the report.** No names of people, organizations, projects, repositories, branches, hosts or services. No URLs. No paths except eva-md's own standard ones written with `~`. No quotes from chats, prompts, wiki pages, commit messages or logs. No secrets. Use counts, sizes, durations, percentages, dates, statuses, and eva-md's own row names, reason codes and field names. Where an example would help, invent a neutral one and mark it as invented.
4. **Say what you know and how.** Mark each finding as measured (a number you computed), observed (you saw it happen in this chat) or inferred. "Unknown" and "could not check, because ..." are good answers. Do not guess.
5. If the sandbox blocks a read, ask me to approve it. Do not work around it.

## What to find out

Use these numbered headings in the report, in this order.

**0. Environment.** The macOS version. The versions of eva-md, Codex and, if installed, Claude Code. Which surface this chat runs in (terminal, desktop app, IDE extension), if you can tell. Whether the machine has managed settings for either host (yes, no or unknown). How long eva-md has been installed, from the oldest file in its state folder.

**1. What this chat was given.** You are the agent eva-md is meant to help, so start with your own experience.
- Did your context at the start of this chat hold a block from eva.md? Give its rough size, the kinds of lines in it, and whether it fitted the folder and branch you are in. Was anything in it stale, wrong, repeated, confusing, or worded as an instruction?
- Did this prompt bring recalled knowledge, a pointer to a system page, or a reminder line?
- Are the tools `eva_search`, `eva_show` and `eva_status` available to you? Is there a recall skill in your skills list?
- Did any notice from eva-md appear?
- Run `eva-md brief --cwd . --as startup --host codex`, time it, and compare its size and shape with what you were given.

**2. Health.** Run `eva-md status` and `eva-md doctor`, and time both. Give the totals of PASS, FAIL and UNKNOWN, and for each row that is not PASS its own name and the reason it states, in general terms. Say whether the fix each row suggests is something the program could have done itself.

**3. Capture.**
- Sessions in the inbox per host, total size, and the range of dates.
- Entries by kind and by origin (a count for each value of those fields). How many entries are marked as recovered from a transcript, as gaps, or as cut short.
- For each host, which hook events have a heartbeat file, the newest time in each, and any failure flags or durations they record: the median and the 95th percentile per event if the data allows.
- Which hook events never appear at all. Whether session starts of every kind (startup, resume, clear, compact) are present.
- Anything that suggests chats were missed: days you can see git activity in this repository but no captured session, sessions with a start and nothing else.

**4. The background job.** Read the run logs in `~/.local/state/eva-md/updates/`. If there are none, say so and look for the reason: the config's switches, the job's logs (reason codes only), the doctor's rows.
If there are records, give:
- Records per day, per host, per outcome and per reason.
- The models and effort levels asked for and used.
- Duration: median, 95th percentile and maximum.
- Tokens in and out: the median per run and the total per day.
- The time from a chat's newest entry to its saved page (the field named `boundary_to_saved_ms`, if present): median and 95th percentile.
- The totals of every numeric field that counts what the checker did: what it refused, what it could not attribute, grounding misses by class, rule changes and knowledge changes added, refused, duplicate, stale, ignored or over the cap.
- How many runs failed outright, were skipped or put a session in quarantine, and what share of chats ended with no page written.
- Calls per day against the daily cap.
- The backlog now, from `eva-md status`.

**5. The wiki.**
- Counts: projects, thread pages, rules, knowledge pages by kind, scope and state, system pages. Sizes of the largest pages. The oldest and newest `as_of` dates.
- How many pages are locked, verified or reported as damaged. How many rows the recall index holds.
- Then read up to five pages of this project (its thread page for this branch, its rules page, and up to three knowledge pages) and judge them in general terms. Are they right, as far as you can check against the repository? Current or stale? Vague or specific? Do they repeat each other or what the repository's own instruction files already say? Would they help an agent new to this project? Do the provenance tags fit? Does anything look as if it came from a tool's output or a web page and not from the user?

**6. Finding things.** Make up six searches from what you now know of this project: two for things the memory should hold, two that ask for the same things in other words, and two for things it should not hold. Run each with `eva-md search`, and with `eva_search` if you have it. For each give hit or miss, the rank of the right result, how much noise came with it, and the time taken. Do not record the queries. Say how many calls it takes to get from a question to a usable answer.

**7. The daily commit and push.** From the records in `~/.local/state/eva-md/commit/` and read-only git in the wiki: on how many of the last 30 days was there a commit, how many commits wait to be pushed, how many files are uncommitted, what reasons failures give, and whether commits are signed (yes or no). If a push has failed, say for how long and with what class of reason.

**8. Codex.** Does the doctor report eva-md's hooks as trusted? Is `needs-trust.json` present, and how old is it? Has capture on Codex stopped at any point after an upgrade of eva-md or of Codex? Does eva-md's plugin folder under Codex's plugin cache hold any file eva-md did not put there (a count is enough)? Are Codex's own memories switched on? Is this chat's folder a worktree that Codex manages?

**9. Claude Code**, if it is installed. Is eva-md's plugin listed? Do its hook events have recent heartbeats? Does Claude Code's own auto memory folder exist for this project, and how many files does it hold? Do eva-md's brief and that memory say the same things?

**10. Cost.** Tokens and model calls per day spent by the background job. Any sign that it met a rate limit or held back. The size of what eva-md adds to a chat: the brief, and each addition to a prompt.

**11. Questions for the owner.** You cannot answer these, so list them at the end of the report for the person to answer in a line each: what they have had to do by hand since installing; which prompts, approvals or notices they have seen and how often; whether any signing or sign-in prompt appears with the daily commit; whether the agent has ever plainly remembered something useful, or plainly failed to; and whether anything has ever been remembered that should not have been.

**12. Your judgement.** As the agent this memory is for: the ten shortfalls that matter most, in order, each with what it costs you; the five things that work well and should not be lost; anything that surprised you; and what you needed in this chat that memory could have given you and did not.

## The report

Write it to `eva-field-report.md` in the current folder, as Markdown with the headings above. Keep it under about 600 lines. Do not commit it. End it with a line saying you read the whole report again against rule 3, and what you removed, if anything. Then give me a summary of five lines in the chat.
~~~
