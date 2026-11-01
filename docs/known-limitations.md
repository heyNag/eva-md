# Known limitations

These are open problems in eva-md as the code stands: capture, resume, search and its index, wiki updates, system pages and their code status, standing rules, pointers, the daily commit and push, setup, install, the MCP server and the health check. Parts still planned are described in [design.md](design.md), not here. What has been verified is in [verification.md](verification.md); the background update, the daily commit, system pages and their code status, standing rules, pointers and the MCP server are checked offline only.

## Redaction

- About 1% of a deliberately hard set of generated passwords still keep a piece after redaction. The shapes are ambiguous by nature: a value containing `)` inside a call, `&k=` or `;k=` inside a URL query or connection-string value, and values that start with an unclosed bracket.
- A UUID or a 40-character hex string after the word Bearer, without an Authorization header, is kept as in ordinary text.
- The entropy rule erases ordinary camelCase identifiers that follow words such as token, secret or password, as in `token = maxCompletionTokensPerRequest`. Prose is over-redacted too: `Password: expired yesterday` loses its value.
- When `redact.Identifier` replaces part of a branch name or project key, it puts an unsalted 8-hex hash of the original in front. The hash can be checked against guesses.
- Redaction takes about twice as long as the baseline rules alone, because `redact.Redact` runs both rule sets. Very long recovered replies are therefore more often cut to head and tail windows.

## Capture, identity and config

- `origin` is read from `remote.origin.url` as configured, without `url.<base>.insteadOf` rewrites, so a clone through an alias and a clone through the full URL get two project keys. Write a remote exclusion in the form origin is configured with; for a repo cloned both ways, exclude both forms.
- A config that fails to load stops capture and briefs on both hosts until it is fixed. Each chat start says so in one line, and `eva-md doctor` and `eva-md status` report it. A config read cut off by the hook's time limit says nothing. A remote exclusion written as a URL that can't be normalized, such as a `file://` URL or one with a query string, makes the config fail to load.
- Path exclusions compare bytes, so the NFC and NFD spellings of the same non-ASCII folder name do not match each other.
- A Codex memory-writing run is skipped only for the events whose prompt or folder shows it. Other events of the same run are captured as an ordinary chat.
- Under very heavy CPU load, git's error output can be cut short. A chat in a folder outside any git repo then fails identity, and that event is not captured.
- Prompts of a Codex `codex exec` run are stored as the user's words: no hook payload field or environment variable is known to mark such a run.
- A Claude chat counts as run by a program from the `CLAUDE_CODE_ENTRYPOINT` value it inherits. A `claude -p` you type yourself is taken for a program's chat too: its prompts are stored as host prompts, it gets no wiki update of its own, and it is never the brief's previous chat or lineage pick. Any process started from such a chat inherits its value.
- A program-run Claude chat still gets the startup brief, and its start still catches up the project's other idle chats, so a script that runs `claude -p` in a project can start update jobs, with model calls, for the chats left idle there. A program's chat asks for no update of its own, so until another chat's catch-up or `eva-md update --pending` passes over it, it counts as a pending update in `eva-md status` and `eva_status`, and it takes one of the 5 sessions that catch-up starts.
- The brief judges a program's chat on its whole session file only when the last MiB it reads holds the file's first line, so a program's chat whose file is longer than that can still be picked as the previous chat or by lineage.
- The update passes over a program's chat that its catch-up record doesn't mark, as when the record is missing or can't be read, only once its window, 64 MiB at most, reaches the end of the file. A longer backlog waits, as before, with no model call and its watermark where it was; another chat's start can keep taking it in its catch-up, and each such job reads that 64 MiB window again. A marked chat with no typed prompt before the watermark or in its window is passed over that window even if a person types later in the file.
- An MCP tool line keeps its string and number arguments only up to the line's 200 characters, and leaves out a value over 1 KiB. Arguments, and a fetched URL's query parameters, are left out by name: a credential under a name that says nothing of it, such as `x=`, is kept unless redaction catches its value, and an innocent one named like a credential, such as `sessionId`, is left out.

## Transcript recovery

- Start-of-chat catch-up looks at the 256 most recently updated of at most 4,096 recovery records per host, and records are never pruned. Past 4,096 records it can miss this project's abandoned chats, including the newest one.
- Duplicate suppression uses the turn id or, without one, the text and its timestamp. A recovered final reply over 48 KiB that has no turn id can be stored twice.
- Going off the record moves the recovery cursor to the end of the transcript first. On-record replies that had not been recovered yet are lost.
- While a recovery pass is still behind, a hook stores only its event's name, as off-the-record mode does, because the unread rows may hold an off-the-record request. A tool line, a host compaction summary or a compact or first-prompt brief arriving then is dropped without a gap marker.
- Recovered Claude Code prompts are recognised by their `promptId` field. If a host version stopped writing it, typed prompts would no longer be recovered from transcripts; the hook would still capture them.
- The drift alarm catches only a total failure, where recovery recognizes none of a chat's rows. A format change that breaks only prompts (such as a missing `promptId`) or only replies isn't flagged, and a changed Codex `session_meta` stops recovery as an identity mismatch with no drift signal.
- A drift record stays until that host's recovery recognizes a row again. It is safe to delete.

## Resume brief

- The startup brief decodes the last MiB of each of the 64 most recently changed session files in the project. When about 64 recent chats each exceed 1 MiB, it takes about 325 ms, over its 300 ms target (the SessionStart deadline is 1.5 s).
- The briefed marker is saved only with 20 ms to spare before the hook's deadline. If its rename and folder sync take longer than that, as they can when the disk is very busy, the brief is lost and not retried.
- The tool activity line is a heuristic. It reports a test or build command only when the stored command starts with one of the listed commands, so `cd pkg && go test` or a test run through a script is missed, and it reads only the tool lines in the chat's last MiB.
- The opening request and the earlier chats' first prompts come from the first 64 KiB of each session file. A first prompt that doesn't end within it, such as a paste of over 64 KiB, is not shown, and an off_record event before a chat's first prompt hides it even when the chat was back on the record by then.
- Codex's 6,000-character cap assumes ordinary text. A brief mostly in a script that takes about one token per character, such as Chinese or Japanese, can pass the 3,000-token limit the plugin sets.
- Refresh, resume and fork briefs repeat everything instead of only what changed: a refresh shows the whole Now again, and a dirty tree or an old stash makes every resume brief non-empty. A chat gets at most 2 refreshes, its compactions included, so a third update in it waits for a /clear, a new chat or `eva-md show`.
- A refresh looks for the thread page only in the wiki folder the chat's brief found for the project, or in the project's own. A folder under another name (as after a slug collision) that appears only after the brief is found by the next brief, not by a refresh.
- The footer is cut at 450 characters. With a long wiki path, branch name or binary path it leaves out the grep fallback first, then the thread id.
- Source refs such as `s:<8 characters>#<line>` use the first 8 characters of the session id, and Codex ids are time-ordered, so two can share them. A ref is written with the full id when another session already shares its 8 characters; one written before such a session appeared can turn ambiguous later. `eva-md show` then reports it as ambiguous, and the full session id in its place resolves it.
- The brief's system pointers match a system's repos against this project only, not what the chat is about. They are the first 3 matches in the map's slug order, and a long binary path leaves room for fewer.
- The system map is as current as the last `eva-md system write`.
- The brief counts only the commits of the project's own repo that a system cites, against the worktree's HEAD, so on a feature branch far behind main, code main changed reads as unchanged. Near its deadline, or when a count takes over 60 ms, it shows the page's day instead.
- Many rules for all projects can push this project's own rules out of the rules block's 600 characters. A project rule that contradicts a rule for all projects is shown beside it; neither wins.
- A refresh or resume brief shows every rule again, not only the one that changed.

## Search

- Without a search index, as before the first daily maintain or `eva-md index`, `eva-md search` scans every file, so `--all` over a long history can take seconds: a term on every line costs about 0.25 s per 50 MB. At the 5-second limit the scan stops, prints what it found and says it was cut short; newer files are scanned first. A search the index finds nothing for costs that full scan too.
- In files the index covers, a term is found as a whole token or at the start of a token or of one of its camelCase or digit parts, from 3 characters; a shorter term only as a whole token. A match inside a word, such as `a<b` in `data<base`, or in text written without spaces, such as Chinese or Japanese, is found by the scan alone: in a file's newest, unindexed lines always, in indexed files only when the index finds nothing at all. Next to any other hit, such a match goes unseen.
- A word of 3 or more characters matches at most the 256 tokens it starts that are in the most lines; a line holding only rarer ones is found only by the scan when the index finds nothing.
- The exact-case or phrase tier is judged on the line itself, among the 200 best-scored candidates of the index (8 × `--limit` past 25). For a common word typed in capitals, a rarer hit in that case past them can rank below the others or be left out.
- With a phrase, a term of 1 or 2 characters or one holding other characters than letters and digits, the count of candidates past those read again says "about N more": some may not match.
- `eva-md search` indexes at most 16 MiB for the next search, so a large backlog waits for the daily maintain or `eva-md index`. The MCP server's search never tops up the index; only `eva-md search` does, and a search with no time limit, as a library call without a deadline, indexes nothing.
- The first build of an existing inbox takes about 25 s per GB, and the index is expected to be 15 to 30% of the inbox; neither has been measured on a real inbox yet, and the doctor shows the real size.
- With an index, hits of the same tier are ordered by score, and that order shifts as the inbox grows, since the statistics are pooled over it; page lines are scored with the inbox's statistics. Without one, as before the first build, they come newest first.
- A session file rewritten in place and grown, so that where the index stopped no longer starts a line, is scanned whole and counted as one stale entry, so its new text is found, until the next `eva-md index`, daily maintain or `eva-md search` top-up indexes it whole again, under its next generation. One rewritten in place to the same size, with the same inode, or grown but with a line end still where the index stopped, is noticed only when its lines fail the check made on reading them, which counts them as stale in the trailer; until `eva-md index --rebuild`, its new text is not found through the index, and the index keeps the words of its old text.
- Removing a session file from the inbox takes its words out of the index's segments in use only at the next `eva-md index` or daily maintenance, and the segment file that held them stays on disk until the first of those runs 10 minutes or more after; deleting the index folder removes them at once.
- A query with more than 32 tokens, or a term with no letter or digit or with a run of them over 64 bytes, is searched by the scan alone.
- A config with a wiki path but no instance starts no daily maintain, so its index waits for `eva-md index`.
- Search reads a thread page's Now items and Timeline lines, not its summary or Last chat.
- A project folder in the wiki whose now page is missing or unreadable is not searched; the trailer names it.
- On `--all`, a page hit shows its wiki folder, not the project key an inbox hit shows; a system page hit shows `systems`.
- Search judges each page line on its own, as the page writers and the daily commit's scan do. A snippet can therefore show a value that `eva-md show` of the whole page redacts, because a line before it makes it look like a secret.
- `eva-md show <page>#Timeline` and `#<YYYY-MM-DD>` read the whole page once, line by line with bounded memory (a few milliseconds for 3 MB), because a day can be filed out of order.
- `eva-md show` ends a section at an unfenced `# ` line inside it, where the page model keeps that line in the section. Only a hand-written line can do this, since eva-md escapes text that looks like a heading.
- A phrase matches only words separated by one space, not words split across a line break or by several spaces.
- Terms under 3 characters match only whole tokens between characters that are not letters or digits, so they can't find a word inside text written without spaces, such as Chinese or Japanese.
- A session with two capture files, as when a chat is resumed in another project's folder, is printed with its full id, and `eva-md show` reports even that ref as ambiguous when both files have the line.
- `--all` reads every project folder in the inbox, including a project excluded in the config after its chats were captured. Excluding a project stops capture; it does not remove earlier captures.
- The default scope includes the general area for the whole window, while the brief only counts a general-area chat less than 12 hours old.

## System pages

- A system page is only as accurate as the agent's digest, and pages record names, not meaning.
- Nothing refreshes a page when its sources change. The versions and commits its sources cite let a later digest see what changed.
- The check for credentials in a source URL reads query parameter names only, so a credential under another name, or in the `#` fragment, gets through.
- A link to a system that has no page yet is refused until that page is written.
- `eva-md show` of a page's last section includes the map link line.
- A system write interrupted between its files leaves the map or the folder index stale, and a new page orphaned, until the next system write.
- A count of commits says the cited paths changed, not that the page is wrong, and a page citing a whole repo counts every commit. Counts stop at 1000.
- A path renamed or removed since the digest stops counting at that commit; `eva-md show` and `eva-md system status` report it gone at HEAD, while the pointers show only the count.
- A cited commit that is not on the clone's HEAD, as one from another branch, is counted since the commit the two have in common.
- A path cited with a typo, on a page written while no clone could check it, is reported as `cites a path its commit lacks` once a pass measures it.
- With git older than 2.44, measuring code and the brief's live git in a partial clone fail rather than fetch, so they read as not checked (`git failed`) or as `Live git: unknown`. eva-md decides a clone may be partial from that clone's own config and `config.worktree`, so with an old git a promisor set only in the global config is not covered.
- Clones are found only through the identity cache: a repo never chatted in on this Mac, or one whose clones all lack the cited commit (not fetched, rebased away or shallow), reads as not checked, and a write then checks none of its paths. Of several clones the newest entry is tried first, which is when a hook last resolved the worktree with a change, not necessarily the clone in use; the clone measured is named.
- The counts in the prompt pointers, system pages' search hits and `eva_status` are as of the last code status, taken by the day's maintenance or `eva-md system status`; `eva-md show` and system status measure live.
- The startup brief's pointer counts live, so for a path the cited commit lacks, or a commit off the branch with nothing counted since the fork, it says `code unchanged` until a code status has measured the page; for a commit off the branch also when that status was taken at another HEAD, or when the brief has no HEAD from live git, as in a new chat's footer brief, which runs no live git.
- Pages written before the map carried code have no code on the map until they are written again, so the brief and the prompt pointers show their day; the doctor counts them.
- `stale_after` is a day the agent sets, not a check of the documents: it can lapse on a page that is still right.
- `eva-md show` and `eva-md system status` print the local clone's folder, and the code status keeps it, in eva-md's state folder only.

## Per-prompt pointers

- Pointers match names, not meaning: a description of a system or a misspelling of its name gets nothing.
- Common words and one-word names under 5 characters never match. A one-word title also matches when written in lower case, because its slug is listed too, and a name of several common words matches in any case.
- Only the first and last 32 KB of a long prompt are scanned.
- After a hand edit of a system page, the names index is out of date until the next system write or `eva-md doctor`.
- Past the index's bounds, the aliases of the pages with the oldest `as_of` are left out first.
- A page is pointed at once per chat, even after a compaction.
- Program-run chats, chats off the record and prompts you didn't type get no pointers and no reminder.
- With a long install path, a pointer loses its description, then its note on the code, and the reminder is shorter or not sent at all.

## Background update

- No live update has run yet. The triggers and the job are checked offline only, with fake host CLIs and a stand-in for the spawned binary.
- A chat left open and never prompted again is caught up only at the next chat start in that project: its own triggers are its end, a compaction, a Stop past 60 KB unprocessed, the next prompt after 30 minutes and its resume after 2 minutes idle.
- The start-of-chat catch-up takes at most 5 sessions, from the project's 64 most recently changed session files only. Further idle backlogs wait for later chat starts, and one in an older file waits for `eva-md update --pending`.
- A chat that is off the record, or whose transcript recovery is behind, spawns nothing at its end or compaction; a later chat's catch-up takes its backlog.
- A prompt whose chat ends with no reply, because the chat was closed, crashed or its turn failed, waits with its session_end and never reaches a page. So does a last prompt in the rest of a group an earlier pass stopped in: it waits for its reply, and a chat that ends with neither a reply nor a session_end leaves it waiting.
- The brief's pending line says only that the previous chat's file has a backlog a pass could take, 600 bytes or more (or any rest of a group a pass stopped in), that no update has found too small. A few short messages can pass 600 bytes and still be under one exchange or 600 characters, so the line shows until a job finds them too small. When that chat's job could not run, because another job held the lock or the budget, a hold or a backoff stopped it, the Now it announces comes after a later trigger, not with a later prompt of this chat.
- The hooks spawn `~/.local/share/eva-md/bin/eva-md` and nothing else. Without a regular, executable file there, as with a symlink in its place (which the doctor also reports as not installed), nothing is spawned.
- Failure classes are named by matching the host CLIs' error text (as claude 2.1.312 and codex 0.166.0 word it), since neither reports them in a documented field. They are checked against that wording with fake CLIs, and live only at the first live gate. A host release that rewords an error can turn, say, a rate limit into `invalid_output`, which counts toward the session's quarantine.
- A trigger that finds a job running exits, so that session's backlog waits for its next trigger or for `eva-md update --pending`.
- While the budget, a hold, a backoff or a quarantine stops a chat past 60 KB unprocessed, each of its Stops still spawns a job, which takes the lock, checks the wiki, reads the run log and lists the inbox before it stops. It reads nothing of the chat's file.
- Chats captured before the update first ran are never summarized: install, or else the first job, sets each one's watermark at its end, once. A chat open across that moment reaches its page only from then on, and there is no command to work through older chats. A chat that couldn't be set aside then waits, recorded as failed by each job that lists it, until it can be; one that never can, as a session file with a second hard link, stays so. A project folder that couldn't be read then has its chats set aside once it can, by when they last changed, so a chat from before updates that went on in that folder before it could be read again is taken for a new one.
- The out-of-order guard compares the page's `as_of` with the newest entry in the window the job reads (4 MiB, more only while a chunk needs it), not with the whole backlog. In a larger backlog, the first chunks of a session that goes on past the page can be taken for an older session's and only add Timeline lines.
- A prompt with no reply before a prompt on another branch is a group of its own, too small to update a page, so it is skipped and never reaches one; the inbox and the brief keep it. A closed chat's last exchange reaches its page, however short. What still waits is a closing group without a whole exchange, and the last short group of a chat that ends with no session_end; the watermark records it as waiting, so the triggers and the brief stop reporting it.
- A turn that switches branch goes to the page of the branch its prompt was on. When the 60 KB chunk limit cuts such a turn between its prompt and the switch, the rest of the turn goes to the new branch's page, since the watermark doesn't keep the branch the cut group was on.
- A tool feature added by a later Codex release isn't disabled until eva-md adds its name to its list, and a feature that managed requirements pin on stays on. A run that uses either is still rejected as `tool_used`. Each attempt also starts `codex features list` once more, up to 15 s, before the run. A `codex features list` that can't be read, because it fails, takes over 15 s, prints over 64 KiB or names no feature, leaves only hooks, plugins, memories, shell_tool and apps disabled on that attempt; the other tool features stay as Codex's config sets them, and a run that uses one is rejected as `tool_used`.
- Without `codex_ignore_user_config = true`, your Codex MCP servers start on update runs, and a run stops at its first tool item only after Codex has sent that call: a server can receive the call's arguments, which may carry text from the chat. On Claude, an organization's managed MCP config that refuses `--strict-mcp-config` makes the run go without it, so a managed server whose tools managed settings allow can be called the same way. Both are servers you or your organization configured; set `codex_ignore_user_config = true` on a machine whose servers should never see chat text.
- A model accepted only without an effort flag starts the day's later runs at the table's lowest effort, since a request can't ask for no flag, and pays one refused step on each run.
- The runs drop API keys and the gateway, Bedrock and Vertex variables, so a host set up to bill through those fails as `auth` or `host_error`. Which account pays is decided by the host's own settings or a stored API-key login, which eva-md can't see.
- Holds, backoff and quarantines look back no further than yesterday's run log: a quarantined session whose next run comes days later starts a fresh count.
- A save whose page was written but whose folder fsync then failed is recorded as `wiki_error`, so its chunk runs again, with one more model call, on the page as written.
- A wiki that can't be written, such as one on a full disk, costs a model call at each backoff step (5 minutes, doubling to an hour) until it can, since the save comes after the call; the doctor and the chat-start notice report it after 3 failures in a row.
- A project whose inbox key is a hash (`repo-<hash>` or `local-<hash>`, for names that read as tokens) can't be identified once the chat's folder is gone or has moved, so its backlog stays with an error.
- Each run counts the lines of a pending session file before its watermark from the start of the file, which takes a moment for a very large session file.
- The run log keeps one file per day and nothing prunes it; the job's logs are pruned after 14 days.
- A kill or a failed watermark write after a page is saved leaves the watermark behind, so the next pass can repeat Next items and done, change or problem lines. The window is wider than the save itself: the overview and index writes run between the save and the watermark, and each can wait up to 60 s for the wiki lock.
- Before it opens a file, the job judges a quarantine by the session id the file name holds. A session whose id `SafeName` changes is therefore opened before its quarantine stops it, and its `failed (watermark)` record uses the file-name id, so the doctor can name one session two ways.
- Deleting a damaged watermark makes the job read that session from its start.
- If okf refuses a project's first thread page, the project's new `now.md` links nothing and counts as an orphan, so the doctor's wiki validation fails while the wiki holds other pages, until a later pass writes a thread page.
- A recorded CLI path goes stale when the CLI, or the version a version manager installed it under, is removed; jobs then look the CLI up on PATH until install runs again. Installs made before paths were recorded have none, and the doctor says to install again. `eva-md doctor --live` always looks the CLI up on PATH.

## Standing rules and pasted text

- On Codex, pasted text can become a standing rule or a decision: Codex has no known paste marker, so every word of a Codex prompt counts as the user's.
- On Claude Code, pastes are found only by the host's paste wrapper, which has not been observed in a real hook payload yet. A pasted entry cut to fit the input keeps no mark on its cut part.
- A claim resting on any pasted words becomes inferred, even when the user's own words agree with it.
- Rules are checked by words, not meaning: a paraphrase, or a rule split across two prompts, is refused, and the negation check refuses some faithful rewordings, such as "Never, ever".
- A drop needs only half the rule's words in one prompt, so a wrong drop is possible; it is visible in the Timeline and the retired list, and its reason is the model's.
- "everywhere" always means all projects. Whether a negation turns the words for all projects around is read from the words before them in their clause, up to a word of the rule, so a run-on sentence with a negation early and no comma, such as "I don't care what you did before in all my projects never use yarn", keeps the rule "Never use yarn" with the project, and "don't run go vet in all my projects" said of the rule "Don't run go vet" sends it to all projects.
- A session older than the thread page changes no rule, and an older add processed out of order can land after a newer rule.
- Rules pages are saved before the thread page. When the thread page's save then fails, the rule change stands but its Timeline line is lost: the retry finds the rule already changed.
- A stated or accepted claim needs only one content word in common with the words it rests on (the same word, or one the start of the other from 4 letters), so a claim that adds to what the user said can keep the user's tag, and a faithful one in other words, such as "the second option" written out, falls to inferred.

## Daily commit and push

- A hand edit pushed to the wiki from elsewhere stops every push as `diverged` until it is merged by hand; eva-md never pulls, rebases or forces.
- A declined signature waits until the next day or a manual `eva-md commit`. A failed commit's reason is read from git's wording (as git 2.56 words it), so another git can sort a missing signer or a declined signature as a plain `commit`.
- A day whose maintain spawn fails, or whose first event missed its deadline, gets no commit until the next day or a manual run.
- A push left pending just after a check waits up to 30 minutes for its first retry.
- Files staged by hand outside the commit paths are unstaged and listed once in that run's record, then left as they are. Symlinks, JSONL and temp files inside the commit folders are listed in every run until handled by hand.
- Binary files and files with an added line over 1 MiB are never committed.
- The secret scan takes words shaped like identifiers for identifiers, so a token whose random part is all lower-case words and digits can pass it.
- A branch without an upstream, such as a wiki set up with `--no-push`, is pushed on a day with nothing to commit only by the chat-start retry of a pending push.
- A first commit that wasn't pushed (`--no-push`, or a no at setup's push question) is pushed by the next daily commit that has changes, or by `eva-md commit --push-only`, which pushes without setting an upstream.
- `eva-md init` with the remote out of reach fails, rather than leaving its push pending; running it again once the host is reachable works.
- A refused `--push-only` run is read by the doctor as a commit run.

## Setup

- A repo's visibility is known only when gh (github.com) or glab (a host it is signed in to) can tell, and glab is asked only when `glab config get host` answers with a usable host. Setup offers to create a repo with glab only on that default host, so for another GitLab host run setup with `GITLAB_HOST` set to it, or set glab's host first. On any other host, such as a GitHub Enterprise server or a GitLab host glab isn't signed in to, setup only tries to read the repo over HTTPS with nothing that could sign it in, and asks before using one it reads (refusing it without a terminal). A host that lets its own network read repos makes a private repo read the same, so answer yes there. An internal repo, one served on another HTTPS port or path, and one reachable only through a proxy or a custom certificate pass as private, and such a host is never offered.
- An HTTPS remote needs a git credential helper, or the first push fails.
- Setup's Commit signing line is read from the home folder; the first commit's own author and signing are shown before the push, read from the wiki, but not with `--no-push`, which pushes nothing. The check before the push runs only for a branch the remote has never had, and shows only the commit at HEAD: an adopted wiki whose branch the remote has, with local commits ahead of it, is pushed without being shown, and on a never-pushed branch with several commits only the newest is described.
- After a create times out, the repo may exist without setup knowing. The next run finds it private and empty and uses it.
- The config records only the remote's identity (lower case, no port). Without gh or glab, cloning a missing wiki folder again uses the SSH address, so an HTTPS remote or a custom SSH port needs `--remote` or a restore by hand.
- Reading a repo before using it is one more fetch, which may mean one more approval in an SSH agent that asks, and it briefly writes a shallow copy under the temporary folder. A dumb-HTTP server can't serve a shallow clone, so its repo reads as unreadable.
- Without a terminal, another Mac's wiki is adopted without a warning.
- Renaming the Mac makes its own wiki look like another Mac's, and so does a host name with characters the label replaces, which gets a hash suffix.
- If init refuses after cloning, the clone stays in the folder: init never deletes a failed clone.
- A folder holding a wiki of a newer schema is still offered at the folder question, and init then refuses it.
- Cloud storage is recognized only under `~/Library/CloudStorage` and iCloud Drive's `~/Library/Mobile Documents`, so a sync folder anywhere else, such as one an older sync client keeps in the home folder, is accepted as the wiki folder though it syncs into `.git`.

## Install and uninstall

- Codex keeps its hook approvals after uninstall, so a reinstall with identical hooks may not ask for approval again.
- An upgrade that changes the plugin folders re-runs each host's add command over the installed plugin. When a host refuses that, install accepts a registration that still verifies, since both hosts load a local plugin's folder in place; this has been checked with fake hosts only.

## MCP server

- Each open chat keeps one idle `eva-md mcp` process, of about 13 MB resident, which exits with the chat.
- Tool calls run one at a time, so calls sent together wait for the ones before them: about 5 seconds for a search or a show and 3 for a status, and at most 6 and 4 when the call before is stuck, its deadline plus the one-second grace. At most 64 calls wait behind the one running; a further call is answered at once with the error "eva-md: too many calls are waiting; try again".
- A call's default scope is the folder the host started the server in. On Codex that may not be the project; the first line then says the general area was searched, and the agent must pass `cwd`.
- A whole page over 24 KB is cut, or a whole system page over 25 KB, where `eva-md show` prints up to 2 MiB; the note names the sections to ask for instead.
- The instructions reflect the state at the chat's start, and Codex may not show them at all, so on Codex a lapse of hook trust reaches the agent only through `eva_status`.
- A warning about the hooks waits up to 1.5 s for the chat's own SessionStart hook. In a folder whose chats aren't captured, such as one the config excludes, that hook writes no heartbeat, so when the host's hooks never ran, or last ran over 7 days ago, the server waits that long and then warns. The same wait comes at every Codex chat start while its hooks await approval, as after an install or an upgrade that changes the plugin, until they are approved in Codex's `/hooks`; since Codex may not show the instructions at all, there the wait may buy nothing. The doctor's probe never waits: it names itself to the server, so a fresh install with no chat yet doesn't slow the doctor's `mcp server` row.
- Claude Code may ask permission the first time each tool is used. eva-md never writes host settings: to allow the tools, add `mcp__plugin_eva-md_eva-md__*` to `permissions.allow` in your own Claude settings.
- On Claude Code the tools may be deferred behind ToolSearch; the instructions name them for it.
- `mcp = false` refuses calls at once, but Claude Code keeps the tool list it read until the next chat.
- The doctor's probe proves that the installed binary answers as an MCP server, not that a host connected it; whether a host loaded the server can't be seen from the command line.
- eva-md's own MCP calls are captured as tool lines, with their refs, folders and bounds but never their terms or results.
- `eva_status` counts the bytes past each session's watermark as a pending update, the current chat's own included, as `eva-md status` counts them: only a rest the update already marked as waiting, with nothing added since, is left out.
- On a host that doesn't defer them, the tool list costs about 500 tokens a chat (1,956 bytes), and the instructions up to about 150.

## Health check

- Chats in excluded, temporary or wiki folders write no heartbeat but do create host transcripts. The capture-freshness check can then report a false FAIL and a false "no matching capture heartbeat" notice.
- The capture-freshness scan gives up after 4096 entries or 512 folders in a host's chat store. Once a store is that large, the check stays UNKNOWN and no longer flags a trust lapse.
- The start-of-chat notice reads at most 64 KB of each day's run log and 16 KB of each month's commit records, so on a very busy day it can miss an older failure.
- The status backlog counts only each project's 64 newest session files and stops listing after 4,096 entries, saying "at least".
- The doctor's names check waits up to a minute for the wiki lock, whatever the doctor's own time limit.
- The search index row checks that the segments each manifest names are there, at their recorded size. Damage inside a segment is found by the next search, which scans that folder and says so, or by the next catch-up, which builds the folder again.
- The hook failure count misses runs whose payload can't be read, and counts only the past 7 days. Two runs of one event that end together may each keep only their own run in the heartbeat, which loses successes and failures alike. A capture that runs out of time records its failure after its output, up to 100 ms past its deadline.
