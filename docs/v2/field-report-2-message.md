# The message that goes with the second field report prompt

## (a) To the agent, when you attach the file to a new chat

> The attached file, field-report-2-prompt.md, holds a task for you. Read all of it. The part before the first line of tildes is notes for me; the part between the two lines of tildes is your instructions. Do exactly what that part says, as if I had pasted it here. Write the report where it says (eva-field-report-2.md, or eva-field-report-2-run2.md if that file is already there). Change nothing else on this machine. Run only the commands it names. If you think you need anything else, stop and tell me what and why. Do not ask me to approve it.

## (b) A note to yourself

1. Upgrade first to the counters release (RELEASE-NUMBER) with `eva-md upgrade` (or the install command in the release notes). If Codex asks you to review eva-md's hooks (`/hooks`), approve them and remember that it asked.
2. Open a new Codex chat in the desktop app, in a git repository you used with Codex this week, from your normal home. Attach the file with message (a), or paste the part between the tildes.
3. Approve only commands the file names; `eva-md audit capture`, `eva-md status --updates` and `eva-md doctor --report` are among them, and write nothing. Refuse any other eva-md command (setup, init, install, uninstall, upgrade, update, commit, maintain, index, system, inbox, mcp, hook, doctor --live), any codex exec or claude -p, any web search or fetch, any network command (curl, ssh, git fetch, pull, push or ls-remote), and anything that writes outside the temporary folder and the report file in the chat's folder (eva-md's own index and cache files excepted, as the file lists). Deny any request for access to a folder.
4. Run it after at least a day of normal use since the upgrade: it reads all the past use on the machine, but a few counters only start at the upgrade, and run sooner they count mostly the report's own chat. If you run it again in the same folder a week later, that run reads them in full. Expect an hour or more of the agent's work.
5. Read each report once. Before you bring it back, look for any name, path, page title, quote, subject, model name or service name (also in the appendix), and strike it. Answer its last section a line each in general terms (no names), and bring the file back.
