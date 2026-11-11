# The host sittings: a run-book

This run-book is for the owner, at a Mac. It settles the host facts of [host-facts.md](host-facts.md) that only the hosts themselves can show: what a hook receives, when a row reaches the host's record, how a voice call reaches a hook, what a plugin change costs in reviews. It is the first half of HF-00 in [the v2 plan](plan.md) (Stage 0); the recorder it uses is the second half, described in the card [HF-00](cards/HF-00.md).

## Before anything else

**What it is for.** eva.md v2 rests on facts only the hosts can tell. The six design papers each list the ones they assume. 141 of the 216 facts in the fact sheet are settled here, in three sittings: one for the evaluation home, the machine and the git server, and one for each host. A small recorder keeps what the hooks receive. Afterwards the lead turns the recording into test fixtures with every value replaced by a made-up one, and marks each fact confirmed or refuted.

**What you need.**

- **An evaluation home.** A separate user account on the Mac, made in sitting 0, with both hosts installed and signed in with your own subscription. A separate account is safer than a separate folder: a host may keep its sign-in in the login keychain, which a folder does not separate. Sitting 0 still tests the folder way, inside the evaluation account, for the evaluation harness that needs it (F-073, F-074).
- **Both hosts' CLIs and desktop apps**, and a microphone for the two voice calls in Codex's desktop app.
- **A scratch repository on the git server** your wiki uses, created by you in sitting 0, private and empty. It receives one push.
- **The recorder** (`hostrec`), with its scratch plugins and scratch MCP server. It is not built yet: it is the second half of HF-00, described in [the card](cards/HF-00.md). This run-book cannot be followed until that card is merged.
- **Go**, to build the recorder from a checkout of this repository.

**What is never touched.** Your real install and its home: your own account's eva-md, its wiki, its inbox, its hooks, `~/.claude` and `~/.codex` in your own account. Nothing in this run-book is typed in your own account, except reading one page of System Settings in sitting 0. eva-md itself is not installed in the evaluation home; only the scratch plugins are. Every prompt, name, word and value below is made up for this run-book and stands for nothing real.

**How long it takes.** Each step states its minutes; the sums are below, and each part repeats its own.

| Part | What | Minutes |
| --- | --- | --- |
| Sitting 0 | The evaluation home, the machine and the git server | 113 |
| Sitting 1 | Claude Code | 156 |
| Sitting 2 | Codex: the CLI, then the desktop app with two voice calls | 224 |
| All three | | 493 |

Take a break wherever a part says one fits. A sitting can stop between any two steps and go on later: the recorder cuts its recording by step.

## How to follow a step

- You use two Terminal windows in the evaluation account. **The chat window** runs the host. **The side window** runs `hostrec` commands.
- Every step starts with `hostrec mark <step>` in the side window, so the recording knows where each step begins.
- Text in a box after **Type** is typed exactly, then Enter. Text after **Say** is spoken.
- **Expect** says what should happen. **If not** says what to do. When something unexpected happens, write one line with `hostrec note "what you saw"` in the side window, and go on. Do not retry a step more than once, and never change a setting a step does not name.
- **Settles** names the facts a step gives evidence for. The table at the end maps each fact to its steps and each step to its facts.
- Before each step that needs one, `hostrec arm <name>` makes the scratch hooks return a planted word, sleep, or start a background job. Arming changes a file outside the plugins, so it never costs a Codex review. Each `arm` replaces the one before, and `hostrec disarm` puts the hooks back to recording only.

## Sitting 0: the evaluation home, the machine and the git server

113 minutes, plus waiting for the scheduled job's first run. A break fits after S0-08.

### S0-01. Make the evaluation account (10 min)

1. Open System Settings, Users & Groups, and add a Standard user. Full name `HF Eval Home`, account name `hfeval`. Give it a password you keep for these sittings.
2. Turn on fast user switching if it is off (Control Center), so you can switch between your own account and this one without logging out.
3. Log in to `hfeval`.

**Expect:** a fresh desktop for `hfeval`.
**If not:** if the Mac does not let you add a user, stop here; the lead plans the sittings again for a folder used as `HOME`.
**Settles:** nothing on its own; every later step relies on it.

### S0-02. Device management (2 min)

1. In `hfeval`, open System Settings, General, Device Management (on older macOS: Privacy & Security, Profiles). Note whether any profile is listed.
2. Switch to your own account and look at the same page, reading only. Switch back to `hfeval`.

Then, in Terminal in `hfeval`:

```
mkdir -p ~/hf-tools && echo 'export PATH="$HOME/hf-tools:$PATH"' >> ~/.zshrc
```

**Expect:** no profile in either account.
**If not:** a profile is listed. Note only that one is there and whether its name mentions login items or background items; never its other details. You note it after S0-05, when `hostrec note` exists.
**Settles:** F-202.

### S0-03. Tools for the account (15 min)

1. In Terminal in `hfeval`, type `git --version`, `command -v ssh-keygen` and `go version`.
2. Install Claude Code's CLI and Codex's CLI for this account, by each host's own instructions. The desktop apps in `/Applications` are shared by all accounts and need no install.
3. Type `claude --version` and `codex --version`.

**Expect:** every command prints a version or a path.
**If not:** if `git` asks to install the command line tools, accept and wait. If `go` is missing, install it from go.dev, or ask the lead for a built recorder and put it at `~/hf-tools/hostrec`.
**Settles:** nothing on its own.

### S0-04. Sign both hosts in (10 min)

1. Type `claude` in Terminal, sign in with your own subscription when asked, then type `/exit`.
2. Type `codex login` and sign in with your own subscription.
3. Open the Claude desktop app and the Codex desktop app, sign each in, and quit both.

**Expect:** each host says you are signed in.
**If not:** if a sign-in reuses your own account's session without asking, stop: tell the lead before going on, because the evaluation home is then not separate.
**Settles:** nothing on its own; F-073 and F-074 are checked in S0-12.

### S0-05. Build the recorder and the scratch folders (10 min)

Open a new Terminal window, so the line S0-02 added takes effect, and type:

```
git clone <this repository's address, from its page on the git host> ~/hf-src/eva-md
cd ~/hf-src/eva-md && go build -o ~/hf-tools/hostrec ./cmd/hostrec
hostrec setup --rec ~/hf-rec
```

**Expect:** `hostrec setup` makes the recording folder `~/hf-rec`; the scratch repositories `~/scratch/alpha-orchard` and `~/scratch/beta-lantern`, a plain folder `~/scratch/outside`, and `~/scratch/long-notes` with a long made-up `AGENTS.md`; the scratch marketplaces under `~/hf/marketplace`; and prints the commands of S0-06. All their content is made up: fruit, lanterns and orchards.
**If not:** copy the error into `hostrec note` if it runs, or into a message to the lead, and stop.
**Settles:** nothing on its own.

### S0-06. Register the scratch plugins with no terminal attached (5 min)

Type each line as `hostrec setup` printed it. They read from nothing, so a question would fail rather than wait:

```
claude plugin marketplace add ~/hf/marketplace/claude-code --json </dev/null
claude plugin install hf-recorder@hf-scratch --scope user --json </dev/null
codex plugin marketplace add ~/hf/marketplace/codex --json </dev/null
codex plugin add hf-hooks@hf-scratch --json </dev/null
codex plugin add hf-tools@hf-scratch --json </dev/null
codex plugin list --json </dev/null
hostrec note "S0-06 done"
```

**Expect:** each command ends with no question, and the last Codex list shows both `hf-hooks` and `hf-tools`.
**If not:** note which command asked or failed, and its exit status (`echo $?`).
**Settles:** F-056, F-158.

### S0-07. Approve the Codex hooks (5 min)

1. Type `cd ~/scratch/alpha-orchard && codex`.
2. In Codex, type `/hooks`. Approve every hook of `hf-hooks`. Leave Codex with `/quit` (or Ctrl+C twice).
3. In the side window type `hostrec trust`.

**Expect:** `/hooks` lists the scratch hooks as needing review; after approval `hostrec trust` reports them trusted. No other screen asked anything.
**If not:** note what `/hooks` showed and what `hostrec trust` reports.
**Settles:** F-161.

### S0-08. The scratch repository on the git server (10 min)

1. In your browser, on the git server your wiki uses, create a new private, empty repository named `hf-scratch-plum`, with no README.
2. Copy its SSH address. In the side window type `hostrec remote set <the address>`.
3. On the repository's settings page, look at the default branch: is it protected, and are forced pushes allowed? Type `hostrec note "scratch main protected: yes|no; force push allowed: yes|no"`.
4. Answer three questions about your real wiki's project on that server from its settings page or from what you know, reading only. Type `hostrec note "real project: push rule on committer address yes|no; signed commits required yes|no; main protected against force push yes|no; server reachable only on a private network yes|no"`.

**Expect:** the repository exists and the notes are saved.
**If not:** if you cannot create a repository there, create it on github.com instead and say so in a note.
**Settles:** F-176, F-177, F-183.

*A break fits here.*

### S0-09. The machine's git checks (5 min)

Type `hostrec machine`.

**Expect:** a list of checks run in a scratch folder with made-up files, each pass or fail: git's version; a commit signed by a fresh ed25519 key given only per call, and its verification; the signature letters for an enrolled and an unknown key; a stand-in signing program set in the account's git config that must not be called; the three versions of a conflicted file after a merge with no commit; a symlink checked out as a plain file; `core.fsync` accepted; a push into a branch that is not checked out; maintenance with `gc.auto=0`; `ssh-keygen -Y verify` on a made-up signature; the extended attributes of a fetched file. No window or prompt appears.
**If not:** note any prompt or dialog that appeared, and which check it came with.
**Settles:** F-185, F-186, F-187, F-188.

### S0-10. Reads from the git server with prompts off (5 min)

1. Type `hostrec remote read`. It runs `git ls-remote` against `hf-scratch-plum` with prompts off and a 10-second limit, then an anonymous HTTPS read with no credentials.
2. Lock the evaluation account's SSH agent, or quit the app that holds its keys if you use one, then type `hostrec remote read --again`. Unlock it afterwards.

**Expect:** the first read succeeds in a second or two; the anonymous read fails fast; with the agent locked, the read fails fast and no dialog waits.
**If not:** if a dialog appears, note what it asked and answer nothing; it should time out.
**Settles:** F-178, F-179.

### S0-11. The scheduled job (15 min, then wait)

1. Type `hostrec agent install`. It writes a launchd user agent named `org.example.hf-tick` and loads it with `launchctl bootstrap`. Every 15 minutes it appends a time to the recording. On its first run it also: runs `claude -p "Made-up test: reply with the single word PEBBLE."` and `codex exec "Made-up test: reply with the single word PEBBLE."`; reads the scratch repository with `git ls-remote` in batch mode, once with its own SSH setup and once with a made-up machine key passed per call; makes the sitting's one push to `hf-scratch-plum`, two commits authored as `eva.md (hf-test) <hf-test@machines.eva-md.invalid>`, one signed with a fresh key no account knows and one unsigned; fetches one public eva-md release asset by name; writes, reads and removes a file under `~/.local/share/hf-test`; notes which git it finds; and posts one made-up notification with `osascript`.
2. Watch the screen for a "background item added" notice and for any password, keychain or permission dialog. Answer no dialog: press Cancel or Deny.
3. Wait for the first run to end (up to 5 minutes), then type `hostrec agent first-run`.

**Expect:** no administrator password asked; perhaps a one-time background-item notice; one notification titled with the made-up text; `first-run` shows each part's outcome; on the git server, `hf-scratch-plum` now holds two commits.
**If not:** note each dialog with its wording. If the push failed because the job had no SSH agent, type `hostrec push` once in the side window; that is still the sitting's one push to reach the server.
**Settles:** F-175, F-180, F-181, F-189, F-190, F-191, F-200, F-201, F-203, F-204.

### S0-12. A sign-in under another HOME (10 min)

1. Type `hostrec keychain-names before`. It lists the names, never the contents, of the login keychain's items.
2. Type `mkdir -p ~/home-b && HOME=~/home-b claude`. Sign in when asked, type `Made-up test: say hello.`, then `/exit`.
3. Type `HOME=~/home-b codex login` and sign in.
4. Type `claude` (with the account's own HOME). It should still be signed in. Type `/exit`. Type `codex login status`.
5. Type `hostrec keychain-names after`.

**Expect:** both hosts sign in under `~/home-b` and make their folders there; the account's own sign-in still holds afterwards.
**If not:** if the account's own sign-in was replaced or lost, note it, and sign in again as in S0-04.
**Settles:** F-073, F-074.

### S0-13. Optional: a push with a deploy key (5 min)

Only if you agree to a second push to `hf-scratch-plum`. Type `hostrec deploy-key`. It prints the help of `gh` or `glab` for deploy keys, makes a key for this test, adds it to `hf-scratch-plum` with write access, and pushes one commit from the scheduled job's setup with it.

**Expect:** the push succeeds with no dialog.
**If not:** note the class of failure (for example, deploy keys switched off on the server).
**Settles:** F-182, F-191.

### S0-14. Optional: a forced push with a lease (3 min)

Only if you agree to one more push. Type `hostrec force-lease`. It rewrites the last commit of `hf-scratch-plum` and pushes it with `--force-with-lease`.

**Expect:** accepted where forced pushes are allowed, refused where `main` is protected.
**If not:** note the outcome.
**Settles:** F-183.

### S0-15. End of sitting 0 (3 min)

Type `hostrec summary s0`. It lists, by fact number, what was recorded. Leave the evaluation account logged in (switch back to your own with fast user switching), so the scheduled job keeps running. If the Mac sleeps before the next sitting, that is wanted: the job's times show the wake.

**Settles:** nothing new.

## Sitting 1: Claude Code

156 minutes. Breaks fit after C-13 and after C-27. Step C-31 leaves a chat open for 25 minutes while later steps go on.

All steps run in `hfeval`, in `~/scratch/alpha-orchard` unless a step says otherwise.

### C-01. Start (3 min)

Side window: `hostrec mark C-01`, then `hostrec arm start`. Chat window: `cd ~/scratch/alpha-orchard && claude`.

**Expect:** Claude Code opens, still signed in, after about one second's wait. A line beginning `hf test` is shown above the prompt.
**If not:** if it asks you to sign in, note it and sign in. Note how the `hf test` line looked, or that there was none: `hostrec note "start line: ..."`.
**Settles:** F-046, F-047, F-073.

### C-02. The first prompt (4 min)

Side: `hostrec mark C-02`. Type:

```
This is a test with made-up content. Answer three things on three lines. 1: What codeword, if any, were you given at the start of this chat? Give the word only, or NONE. 2: Did you see a line that starts with "hf test"? Yes or no. 3: List the names of the tools whose names start with hf_ that you can call right now, without searching for tools.
```

**Expect:** 1 is `MARIGOLD-TUESDAY`; 2 is most likely no; 3 lists `hf_lookup` (the tool marked to load at start) and perhaps not the others.
**If not:** note the three answers as given.
**Settles:** F-012, F-046, F-047, F-049, F-093.

### C-03. Long start texts (5 min)

Side: `hostrec mark C-03`, `hostrec arm start-9900`. In the chat window type `/exit`, then `claude`, then:

```
Made-up test. At the very end of the text you were given at the start of this chat there is a word in capitals. What is it? Word only, or NONE.
```

Then side: `hostrec arm start-10100`, chat: `/exit`, `claude`, and the same prompt again.

**Expect:** after 9,900 characters the agent gives `LAST-WORD-NINE`; after 10,100 characters it may say NONE or mention a file.
**If not:** note both answers.
**Settles:** F-088.

### C-04. A turn with two tools and words between (4 min)

Side: `hostrec mark C-04`, `hostrec arm stop-child`. Chat: `/exit`, `claude`, then:

```
Made-up test. First write one sentence saying what you will do. Then read the file orchard/plums.txt and tell me in one sentence what it says. Then run the shell command `ls orchard` and tell me how many files it lists. Write a short sentence before each step.
```

**Expect:** a read, a shell command, short sentences between them, and a final reply. Approve the tools if asked.
**If not:** note what the agent did instead.
**Settles:** F-001, F-002, F-004, F-005, F-013, F-019, F-020, F-021, F-022, F-030, F-084, F-085.

### C-05. A command that fails (2 min)

Side: `hostrec mark C-05`. Type:

```
Made-up test. Run the shell command `cat orchard/no-such-fruit.txt` and tell me the exact error it printed and its exit status.
```

**Expect:** the command fails with "No such file or directory" and exit status 1.
**If not:** note what happened.
**Settles:** F-002, F-007.

### C-06. An edit on another branch, with a word from the pre-tool hook (4 min)

Side: `hostrec mark C-06`, `git -C ~/scratch/alpha-orchard switch -c hf-branch-two`, `hostrec arm pretool-word`. Type:

```
Made-up test. In orchard/plums.txt, change the line `count: 12` to `count: 13`. Then write the file orchard/new-plum.txt containing the single word `damson`.
```

When it is done, type:

```
What checkword, if any, were you given while you edited? Word only, or NONE.
```

**Expect:** both edits; the answer `HARBOR-NINE`.
**If not:** note the answer.
**Settles:** F-021, F-081, F-089.

### C-07. Parallel edits (3 min)

Side: `hostrec mark C-07`, `hostrec arm pretool-slow`. Type:

```
Made-up test. Create three new files at once, using parallel tool calls if you can: orchard/a.txt containing `alpha`, orchard/b.txt containing `beta`, and orchard/c.txt containing `gamma`. Then run `cat orchard/a.txt` in the shell.
```

**Expect:** three writes, each held about two seconds by the hook, then a shell command.
**If not:** note whether the writes came one after another.
**Settles:** F-048, F-081.

### C-08. Two reads, and a word from an async hook (5 min)

Side: `hostrec mark C-08`, `hostrec arm async-word`. Type:

```
Made-up test. Read orchard/plums.txt and orchard/pears.txt, in parallel if you can, and say which is longer. Then tell me any marker word you were given since you started reading, or NONE.
```

Then type:

```
Were you given any marker word since my last message? Word only, or NONE.
```

**Expect:** two reads; the first answer most likely NONE, the second `COPPER-FINCH`.
**If not:** note both answers.
**Settles:** F-082, F-090.

### C-09. A multiple-choice question (5 min)

Side: `hostrec mark C-09`, `hostrec disarm`. Type:

```
Made-up test. Before you do anything, ask me one multiple-choice question with your question tool: which fruit file should we create next, with the options pear, quince and fig. Then create orchard/<my choice>.txt containing the single word `ripe`.
```

Pick **quince** from the options. When done, type:

```
Ask me the same kind of question again, about a vegetable file, with the options leek, kale and okra.
```

This time do not pick an option: choose the free answer and type `parsnip`.

**Expect:** a question with the options, then orchard/quince.txt; then a second question answered in your own words.
**If not:** if the agent asks in plain text with no question tool, note it and answer in text.
**Settles:** F-003.

### C-10. A plan approved and a plan rejected (5 min)

Side: `hostrec mark C-10`. Press Shift+Tab until the footer shows plan mode. Type:

```
Made-up test. Plan how you would rename orchard/a.txt to orchard/apple.txt. Do not do it yet.
```

Approve the plan when it is shown. After the rename, enter plan mode again and type:

```
Made-up test. Plan how you would delete orchard/b.txt.
```

Reject this plan, keep planning, and type `Not now, this is a test.`

**Expect:** the first plan approved and carried out; the second rejected, with nothing deleted.
**If not:** note what the plan screen offered.
**Settles:** F-003.

### C-11. A task list (3 min)

Side: `hostrec mark C-11`. Press Shift+Tab until plan mode is off. Type:

```
Made-up test. Use your task list tool: add three tasks, `water the plums`, `count the pears` and `label the quinces`, then mark `water the plums` as done. Do nothing else.
```

**Expect:** the task list shows three tasks, one done.
**If not:** note it.
**Settles:** F-078.

### C-12. A subagent (6 min)

Side: `hostrec mark C-12`, `hostrec arm subagent-word`. Type:

```
Made-up test. Delegate this to a general-purpose subagent: it should first tell you any word in capitals it was given when it started, then tell you the codeword given at the start of this chat if it knows it, then read orchard/pears.txt, add a line `checked: yes` to it, and report back in one sentence. Include the subagent's two words in your answer.
```

**Expect:** a subagent runs; it reports `SILVER-OTTER` and most likely does not know `MARIGOLD-TUESDAY`; the file gains the line.
**If not:** note the words it gave.
**Settles:** F-003, F-025, F-026, F-027, F-091.

### C-13. A custom agent (3 min)

Side: `hostrec mark C-13`. Type:

```
Made-up test. Delegate to the hf-gardener agent: read orchard/pears.txt and report its first line.
```

**Expect:** the plugin's own agent runs and reports the line.
**If not:** note it.
**Settles:** F-026.

*A break fits here.*

### C-14. A message typed while the agent works (4 min)

Side: `hostrec mark C-14`. Type:

```
Made-up test. Count from 1 to 30. For each number, run the shell command `sleep 1` once, then print the number. One tool call per number.
```

After it has printed about five numbers, type while it is still working:

```
Made-up interjection: when you finish, also write the word TEAL-KITE.
```

**Expect:** the interjection is accepted while it works, and `TEAL-KITE` appears at the end.
**If not:** note whether the message waited until the end.
**Settles:** F-010.

### C-15. An interrupt (2 min)

Side: `hostrec mark C-15`. Type:

```
Made-up test. Run the shell command `sleep 60`, then say done.
```

After about five seconds, press Esc.

**Expect:** the turn stops and the screen says it was interrupted.
**If not:** note it.
**Settles:** F-004, F-008, F-016.

### C-16. Pasted text (3 min)

Side: `hostrec mark C-16`. Copy these four lines into the clipboard:

```
moon line: the orchard moon is made up
pear line: forty pears in a made-up crate
gate line: the gate code here is not real
end line: nothing here is real
```

Type `Made-up test. Here is a pasted block:` then paste, then type ` Which line mentions the moon?` and press Enter. Then paste the four lines again alone and press Enter.

**Expect:** two answers. On screen, the paste may show as a collapsed block.
**If not:** note how the paste showed.
**Settles:** F-017.

### C-17. A shell line and a plugin command (3 min)

Side: `hostrec mark C-17`. Type `!ls orchard` and Enter. Then type `/hf-recorder:hf-pause-check` and Enter.

**Expect:** the shell line runs without the model; the plugin's command runs and its text reaches the agent.
**If not:** note it.
**Settles:** F-009, F-016.

### C-18. The pause phrase, typed (2 min)

Side: `hostrec mark C-18`. Type `off the record`, then `Made-up test: what is two plus two?`, then `back on the record`.

**Expect:** three ordinary turns. eva-md is not installed here, so nothing pauses; the recording shows how the phrase reaches the hook.
**If not:** note it.
**Settles:** F-009.

### C-19. Prompts nobody typed (3 min)

Side: `hostrec mark C-19`. If your Claude Code offers a way to schedule a prompt (such as `/loop`), type `/loop 1m Made-up test: say tick.`, wait for two ticks, and stop it. Then type:

```
Made-up test. Start a background shell command `sleep 20 && echo done-made-up`, and tell me when it finishes.
```

**Expect:** the ticks arrive as prompts nobody typed; the finished background command arrives as a notice.
**If not:** if there is no way to schedule, note "no schedule offered" and do only the second part.
**Settles:** F-016, F-080.

### C-20. The scratch MCP server (8 min)

Side: `hostrec mark C-20`. Type, one prompt at a time:

```
Made-up test. Use the hf_lookup tool from the hf-scratch server to look up `quince`, and quote its whole answer.
```

When the host asks permission, note whether "allow always" is offered (`hostrec note`), and allow once.

```
Use hf_lookup again, for `fig`.
```

```
Made-up test. Your hf-scratch server gave you instructions. Quote any words in capitals in them, in order.
```

```
Made-up test. Does the hf-scratch server offer a resource? If you can read it, quote the word in capitals in it.
```

```
Made-up test. Call hf_big four times, with sizes 10, 25, 50 and 100. For each, quote the last word of the result, or say that you could not see the end.
```

**Expect:** one permission question per tool; the instructions' words `QUARTZ-HERON` and `REED-LAMP`, perhaps not `FINAL-BELL`; a resource word `MOSS-DIAL` if resources are read; the last words of the smaller results.
**If not:** note each answer.
**Settles:** F-035, F-036, F-037, F-038, F-039, F-040, F-041.

### C-21. The same lookup by MCP and by command (5 min)

Side: `hostrec mark C-21`. Type:

```
Made-up test. Do this three times: call hf_lookup for `pear`, then run the shell command `hostrec lookup pear`. Report the six answers in one list.
```

**Expect:** six answers, the same text by both routes.
**If not:** note it.
**Settles:** F-042.

### C-22. A web fetch and a network command (3 min)

Side: `hostrec mark C-22`. Type:

```
Made-up test. Fetch the page https://example.com and tell me its title. Then run the shell command `curl -sI https://example.com` and tell me the status line.
```

**Expect:** a web fetch and a shell command; both say Example Domain or 200.
**If not:** note it.
**Settles:** F-035.

### C-23. Another repository (3 min)

Side: `hostrec mark C-23`. Chat: `/exit`, `cd ~/scratch/outside && claude`, then:

```
Made-up test. Read the file ~/scratch/beta-lantern/lantern/wicks.txt, then search the folder ~/scratch/beta-lantern for the word `amber`, and tell me what you found.
```

**Expect:** a read and a search in the other repository.
**If not:** note it.
**Settles:** F-015.

### C-24. A model change (2 min)

Side: `hostrec mark C-24`. Type `/model`, pick a different model from the list, then type `Made-up test: say hello.`

**Expect:** a reply from the newly picked model.
**If not:** note it.
**Settles:** F-077.

### C-25. A compaction (5 min)

Side: `hostrec mark C-25`, `hostrec arm compact`. Chat: `/exit`, `cd ~/scratch/alpha-orchard && claude --continue`, then type `/compact`. When it is done, type:

```
Made-up test. Two questions. 1: What word in capitals, if any, were you given just after the compaction? 2: Does your summary of this chat mention the path orchard/quince.txt? Yes or no.
```

**Expect:** 1 is `AMBER-GATE`; 2 is yes, because the PreCompact hook asked for it.
**If not:** note both answers.
**Settles:** F-016, F-034, F-098, F-099, F-100, F-101.

### C-26. A clear (2 min)

Side: `hostrec mark C-26`. Type `/clear`, then `Made-up test: say hello.`

**Expect:** a fresh context and a reply.
**If not:** note it.
**Settles:** F-006, F-014.

*A break fits after the next step.*

### C-27. Resume and fork (6 min)

Side: `hostrec mark C-27`. Type `/exit`. Wait two minutes. Type `claude --continue`, then:

```
Made-up test: say hello again.
```

Type `/exit`, then `claude --resume`, pick the newest chat, and if your version offers it, fork it (`claude --resume <id> --fork-session`, or the fork choice in the list). Then type:

```
Made-up test: say hello from the fork.
```

**Expect:** the resumed chat continues; the fork starts as a new chat with the old one's history.
**If not:** if no fork is offered, note it.
**Settles:** F-006, F-016, F-033, F-083.

### C-28. A hook past its time limit (2 min)

Side: `hostrec mark C-28`, `hostrec arm timeout`. Type `Made-up test: say hello.`

**Expect:** the prompt hook runs past its two-second limit; the host shows a notice and goes on.
**If not:** note exactly what was shown.
**Settles:** F-092.

### C-29. A missing hook binary (3 min)

Side: `hostrec mark C-29`, `hostrec arm missing`. It renames `~/hf-tools/hf-missing-check`, which one declared hook names, and the exec-form hook's script under the plugin root. Type `Made-up test: say hello.`

**Expect:** the reply comes; at most a quiet notice.
**If not:** note exactly what was shown, and whether you had to do anything to go on. Then `hostrec disarm`.
**Settles:** F-050, F-104.

### C-30. A chat with hooks switched off (4 min)

Side: `hostrec mark C-30`, `hostrec disarm`. Chat: `/exit`, then `claude --settings '{"disableAllHooks": true}'`. Wait two minutes without typing. Then type `Made-up test: say hello with hooks off.` and `/exit`.

**Expect:** no hook line in the recording for this chat; the scratch MCP server's start line is there; the host's own session file still changes.
**If not:** if the setting is refused, note it.
**Settles:** F-024, F-043.

### C-31. A chat left open (3 min, then 25 minutes in the background)

Side: `hostrec mark C-31`. Open a third Terminal window, type `cd ~/scratch/beta-lantern && claude`, and leave it open, typing nothing, for 25 minutes while the next steps go on. After ten minutes, in the side window type `hostrec swap-binary` (it replaces `~/hf-tools/hostrec` with a second build of itself).

**Expect:** the scratch server's minute stamps keep coming from one process; SessionStart fires within 90 seconds of the server's start; no new server starts after the swap.
**If not:** note it. Close the window after 25 minutes, with `/exit`.
**Settles:** F-044, F-045.

### C-32. A changed hooks file (3 min)

Side: `hostrec mark C-32`, `hostrec plugin-edit claude-code timeout`, then the update command it prints. Chat: `claude`, then `Made-up test: say hello.`

**Expect:** no question or review; the recording shows the hook under its new time limit.
**If not:** note any question.
**Settles:** F-103.

### C-33. A rules file with a path pattern (5 min)

Side: `hostrec mark C-33`, `hostrec arm rules`. It writes a rules file in this account's Claude Code folder, for paths `orchard/**`, holding the made-up word `CEDAR-WREN`. Chat: `/exit`, `cd ~/scratch/alpha-orchard && claude`, then:

```
Made-up test. Read orchard/plums.txt. Then: do you have any rule that mentions a word in capitals? Quote the word or say NONE. Then delegate the same question to a subagent and give its answer too.
```

Then `/compact`, and ask again: `Do you still have a rule with a word in capitals? Word or NONE.` Then `/exit`, `cd ~/scratch/beta-lantern && claude`, and type the first prompt again with `lantern/wicks.txt` in place of `orchard/plums.txt`.

**Expect:** the word is known in alpha-orchard after the read; the answers in the other places are what this step measures.
**If not:** note each answer. `hostrec disarm` afterwards removes the rules file.
**Settles:** F-094.

### C-34. A note for the host's own memory (3 min)

Side: `hostrec mark C-34`. Type:

```
Made-up test. Please remember for later that the made-up gate code is 4417.
```

Then side: `hostrec memory-index`.

**Expect:** the host saves a memory note; `memory-index` reports the index file's path shape and format.
**If not:** note it.
**Settles:** F-087.

### C-35. The environment a hook gets (4 min)

Side: `hostrec mark C-35`. Chat: `/exit`, then:

```
HF_FAKE_API_TOKEN=made-up-not-real-000 HF_PLAIN_MARK=1 CLAUDE_CONFIG_DIR=~/hf-alt-claude claude
```

Sign in again if asked (same subscription). Type `Made-up test: say hello. Then run the shell command command -v hf-linked-name.` Then `/exit`.

**Expect:** the recording notes that both variable names were present in the hooks (never their values), and that the record lies under `~/hf-alt-claude`.
**If not:** note it.
**Settles:** F-023, F-028, F-029.

### C-36. A turn that fails (3 min)

Side: `hostrec mark C-36`. Chat: `claude`. Turn off Wi-Fi. Type `Made-up test: say hello.` Wait for the error. Turn Wi-Fi on.

**Expect:** an error, and the turn ends.
**If not:** note the message.
**Settles:** F-008, F-076.

### C-37. A run a program drives (3 min)

Side: `hostrec mark C-37`. In the side window, in `~/scratch/alpha-orchard`, type:

```
HF_PROGRAM_MARK=1 claude -p "Made-up test: reply with the single word PEBBLE."
claude -p --output-format stream-json --verbose "Made-up test: reply with the single word PEBBLE."
```

**Expect:** `PEBBLE` twice; the second as a stream of JSON lines.
**If not:** note it.
**Settles:** F-018, F-028, F-032, F-079.

### C-38. The desktop app's Code tab (6 min)

Side: `hostrec mark C-38`, `hostrec arm start`. Open the Claude desktop app and go to its Code tab. Note how many SessionStart lines the side window's `hostrec tail` shows before you do anything. Choose the folder `~/scratch/alpha-orchard`, and type:

```
Made-up test: say hello from the desktop app, and tell me any codeword you were given at the start.
```

Note how the `hf test` start line showed there, if at all. Quit the app.

**Expect:** the hooks fire in the app; the codeword is `MARIGOLD-TUESDAY`.
**If not:** note it.
**Settles:** F-011, F-046, F-079, F-105.

### C-39. Ending a chat (4 min)

Side: `hostrec mark C-39`, `hostrec arm end-slow`. Chat: `claude`, type `Made-up test: say hello.`, then `/exit`. Then `claude` again, type `Made-up test: say hello.`, and close the whole Terminal window with Cmd+W. Wait 15 seconds, open a new window and type `hostrec children`.

**Expect:** the end hook's lines stop after about 1.5 seconds; each detached child's file appears about 10 seconds after its hook.
**If not:** note it.
**Settles:** F-006, F-030, F-102.

### C-40. A job a hook starts, against the git server (2 min)

Side: `hostrec mark C-40`, `hostrec arm net-job`. Chat: `cd ~/scratch/alpha-orchard && claude`, type `Made-up test: say hello.`, wait 20 seconds, `/exit`. Side: `hostrec children`.

**Expect:** the Stop hook's job read the scratch repository and wrote under `~/.local/share/hf-test`.
**If not:** note it.
**Settles:** F-031, F-189, F-190.

### C-41. Read-only reports (3 min)

Side: `hostrec mark C-41`, `hostrec cli-report claude-code`. It runs, with the network off once and on once, `claude --version`, `claude --help` and `claude plugin list --json`, and times each.

**Expect:** each prints without signing in or calling a model.
**If not:** note it.
**Settles:** F-057.

### C-42. End of sitting 1 (5 min)

Side: `hostrec mark C-42`, `hostrec disarm`, `hostrec summary claude-code`.

**Expect:** a list of every Claude Code event that fired, and of every fact of this sitting with what was recorded for it.
**If not:** note any fact listed as missing.
**Settles:** F-075.

## Sitting 2: Codex

224 minutes: the CLI part (X-01 to X-38) is 158 minutes and the desktop app part (X-39 to X-50) 66; a break fits after X-20 and before X-39, where the desktop app starts. Step X-31 leaves a chat open for 35 minutes.

All steps run in `hfeval`, in `~/scratch/alpha-orchard` unless a step says otherwise.

### X-01. Start (3 min)

Side: `hostrec mark X-01`, `hostrec trust`.

**Expect:** Codex is still signed in (`codex login status`), and the scratch hooks are still trusted, days after their approval, loaded from the plugin's `.codex-plugin/plugin.json`.
**If not:** if they are not trusted, note it, approve them in `/hooks`, and go on.
**Settles:** F-073, F-150, F-157, F-160.

### X-02. The first prompt (4 min)

Side: `hostrec mark X-02`, `hostrec arm start`. Chat: `codex`, then:

```
This is a test with made-up content. Answer on two lines. 1: What codeword, if any, were you given at the start of this chat? Give the word only, or NONE. 2: Did you see a line that starts with "hf test"? Yes or no.
```

**Expect:** 1 is `MARIGOLD-TUESDAY`; Codex showed the `hf test` text as a warning; 2 is most likely no.
**If not:** note how the line showed and the answers.
**Settles:** F-012, F-046, F-047, F-049.

### X-03. Long hook messages (6 min)

Side: `hostrec mark X-03`. For each of the four arms below, type the arm in the side window, then in the chat window `/quit`, `codex`, and the prompt:

```
Made-up test. At the very end of the text you were given at the start of this chat there is a word in capitals. What is it? Word only, or NONE.
```

Arms, in order: `hostrec arm start-4900-declared`, `hostrec arm start-9000-declared`, `hostrec arm start-4900-plain`, `hostrec arm start-9000-plain`.

**Expect:** the word comes back for the shorter texts; the longer ones may be cut.
**If not:** note the four answers.
**Settles:** F-125.

### X-04. A turn with two tools and words between (4 min)

Side: `hostrec mark X-04`, `hostrec arm stop-child`. Chat: `/quit`, `codex`, then:

```
Made-up test. First write one sentence saying what you will do. Then read the file orchard/plums.txt and tell me in one sentence what it says. Then run the shell command `ls orchard` and tell me how many files it lists. Write a short sentence before each step.
```

**Expect:** tool calls, short sentences between them, and a final reply. Approve commands if asked.
**If not:** note what the agent did instead.
**Settles:** F-001, F-002, F-004, F-005, F-013, F-019, F-020, F-021, F-022, F-120, F-121, F-122, F-132, F-133.

### X-05. Long instructions (3 min)

Side: `hostrec mark X-05`. Chat: `/quit`, `cd ~/scratch/long-notes && codex`, then `Made-up test: say hello.`, then `/quit`.

**Expect:** a reply; the rollout's opening row is large because the folder's made-up instructions are long.
**If not:** note it.
**Settles:** F-134.

### X-06. A command that fails (2 min)

Side: `hostrec mark X-06`. Chat: `cd ~/scratch/alpha-orchard && codex`, then:

```
Made-up test. Run the shell command `cat orchard/no-such-fruit.txt` and tell me the exact error it printed and its exit status.
```

**Expect:** the command fails with exit status 1.
**If not:** note it.
**Settles:** F-007.

### X-07. An edit on another branch, with a word from the pre-tool hook (4 min)

Side: `hostrec mark X-07`, `git -C ~/scratch/alpha-orchard switch -c hf-branch-three`, `hostrec arm pretool-word`. Type:

```
Made-up test. In orchard/plums.txt, change the line `count: 13` to `count: 14`. Then run the shell command `wc -l orchard/plums.txt`.
```

When done:

```
What checkword, if any, were you given while you edited or ran commands? Word only, or NONE.
```

**Expect:** an edit through `apply_patch` and a command; the answer `HARBOR-NINE`.
**If not:** note it.
**Settles:** F-021, F-123.

### X-08. A word from an async hook, in the same turn (3 min)

Side: `hostrec mark X-08`, `hostrec arm async-word`. Type:

```
Made-up test. Print the file orchard/pears.txt with the shell command `cat`. Then, in the same reply, tell me any marker word you were given after printing it, or NONE.
```

**Expect:** `COPPER-FINCH` in the same reply.
**If not:** note the answer.
**Settles:** F-124.

### X-09. Parallel edits (3 min)

Side: `hostrec mark X-09`, `hostrec arm pretool-slow`. Type:

```
Made-up test. Create three new files at once, in parallel if you can: orchard/d.txt containing `delta`, orchard/e.txt containing `epsilon`, and orchard/f.txt containing `zeta`.
```

**Expect:** three edits, each held about two seconds by the hook.
**If not:** note whether they came one after another.
**Settles:** F-048.

### X-10. A multiple-choice question (4 min)

Side: `hostrec mark X-10`, `hostrec disarm`. Type:

```
Made-up test. If you have a tool for asking me a multiple-choice question, use it: which fruit file should we create next, with the options pear, quince and fig. Then create orchard/<my choice>-two.txt containing the word `ripe`.
```

If options are shown, pick **fig**. Then ask for a second question as in C-09 and answer it in your own words, `parsnip`.

**Expect:** a question with options, if this version has such a tool.
**If not:** note "no question tool" and answer in text.
**Settles:** F-003.

### X-11. A plan approved and a plan rejected (5 min)

Side: `hostrec mark X-11`. Switch Codex to plan mode in the way your version offers (Shift+Tab, or `/plan`). Type:

```
Made-up test. Plan how you would rename orchard/d.txt to orchard/date.txt. Do not do it yet.
```

Approve the plan. Then plan mode again:

```
Made-up test. Plan how you would delete orchard/e.txt.
```

Reject it and type `Not now, this is a test.`

**Expect:** one plan carried out, one rejected.
**If not:** if there is no plan mode, note it.
**Settles:** F-003.

### X-12. A subagent (6 min)

Side: `hostrec mark X-12`, `hostrec arm subagent-word`. Type:

```
Made-up test. If you can start a subagent, delegate this to one: it should first tell you any word in capitals it was given when it started, then read orchard/pears.txt, add a line `checked: again` to it, and report back in one sentence. Include the subagent's word in your answer.
```

**Expect:** a subagent runs and reports `SILVER-OTTER`.
**If not:** if this version has no subagents, note it.
**Settles:** F-003, F-025, F-026, F-027, F-121.

### X-13. A message typed while the agent works (4 min)

Side: `hostrec mark X-13`. Type:

```
Made-up test. Count from 1 to 30. For each number, run the shell command `sleep 1` once, then print the number. One tool call per number.
```

After about five numbers, type:

```
Made-up interjection: when you finish, also write the word TEAL-KITE.
```

**Expect:** the interjection is taken while it works.
**If not:** note whether it waited until the end.
**Settles:** F-010.

### X-14. An interrupt (2 min)

Side: `hostrec mark X-14`. Type:

```
Made-up test. Run the shell command `sleep 60`, then say done.
```

After about five seconds, press Esc.

**Expect:** the turn stops.
**If not:** note it.
**Settles:** F-004, F-008, F-016, F-120.

### X-15. Pasted text (3 min)

Side: `hostrec mark X-15`. Paste the same four made-up lines as in C-16, once inside a typed prompt (`Made-up test. Here is a pasted block:` … ` Which line mentions the moon?`) and once alone.

**Expect:** two answers.
**If not:** note how the paste showed.
**Settles:** F-017.

### X-16. A shell line, a skill and the pause phrase, typed (3 min)

Side: `hostrec mark X-16`. Type `!ls orchard`. Then type `$hf-pause-check` and Enter. Then `off the record`, `Made-up test: what is two plus two?`, and `back on the record`.

**Expect:** the shell line runs; the skill runs; three ordinary turns.
**If not:** note it.
**Settles:** F-009, F-016.

### X-17. The scratch MCP server (8 min)

Side: `hostrec mark X-17`. Type the five prompts of C-20, one at a time. Note whether a permission question appears and whether "allow always" is offered.

**Expect:** as in C-20; Codex may show more or less of the instructions.
**If not:** note each answer.
**Settles:** F-035, F-036, F-037, F-038, F-039, F-040, F-041.

### X-18. The same lookup by MCP and by command (5 min)

Side: `hostrec mark X-18`. Type the prompt of C-21.

**Expect:** six answers.
**If not:** note it.
**Settles:** F-042.

### X-19. A web search and a network command (3 min)

Side: `hostrec mark X-19`. Type:

```
Made-up test. Search the web for the page example.com and tell me its title. Then run the shell command `curl -sI https://example.com` and tell me the status line.
```

**Expect:** a search if this version offers one, and a shell command.
**If not:** note it.
**Settles:** F-035.

### X-20. Another repository (3 min)

Side: `hostrec mark X-20`. Chat: `/quit`, `cd ~/scratch/outside && codex`, then the prompt of C-23.

**Expect:** a read and a search in `beta-lantern`.
**If not:** note it.
**Settles:** F-015.

*A break fits here.*

### X-21. A compaction by command (5 min)

Side: `hostrec mark X-21`, `hostrec arm compact`. Chat: `/quit`, `cd ~/scratch/alpha-orchard && codex resume --last`, then `/compact`. Then:

```
Made-up test. What word in capitals, if any, were you given just after the compaction? Word only, or NONE.
```

**Expect:** `AMBER-GATE`.
**If not:** note it.
**Settles:** F-016, F-034, F-135, F-136.

### X-22. A compaction in the middle of a turn (8 min)

Side: `hostrec mark X-22`, `hostrec arm compact`. Chat: `/quit`, then `codex -c model_auto_compact_token_limit=20000` in `~/scratch/long-notes`, then:

```
Made-up test. Read AGENTS.md in four parts with the shell, one part per command, and after each part write one sentence about it. At the end, tell me two words: the word in capitals you were given at the start of this chat, and any word in capitals given to you after a compaction. NONE for any you do not have.
```

**Expect:** an automatic compaction during the turn; the agent knows `AMBER-GATE` and most likely not `MARIGOLD-TUESDAY`.
**If not:** if no compaction happens, note it.
**Settles:** F-034, F-174.

### X-23. A clear (2 min)

Side: `hostrec mark X-23`. Type `/clear` (or `/new` if this version has no `/clear`), then `Made-up test: say hello.`

**Expect:** a fresh context and a reply.
**If not:** note it.
**Settles:** F-006, F-014.

### X-24. Resume and fork (6 min)

Side: `hostrec mark X-24`. `/quit`, wait two minutes, `cd ~/scratch/alpha-orchard && codex resume --last`, then `Made-up test: say hello again.` Then fork the chat the way your version offers (`/fork`, or editing an earlier message), and type `Made-up test: say hello from the fork.`

**Expect:** the resumed chat continues; the fork starts as a new chat.
**If not:** note it.
**Settles:** F-006, F-033.

### X-25. The environment a hook gets (4 min)

Side: `hostrec mark X-25`. Chat: `/quit`, then:

```
HF_FAKE_API_TOKEN=made-up-not-real-000 HF_PLAIN_MARK=1 codex
```

Type `Made-up test: say hello. Then run the shell command command -v hf-linked-name.`, then `/quit`.

**Expect:** the recording notes both variable names present in the hooks, never their values, and whether `CODEX_HOME` is set for them.
**If not:** note it.
**Settles:** F-023, F-028, F-029.

### X-26. A run a program drives (3 min)

Side: `hostrec mark X-26`. In the side window, in `~/scratch/alpha-orchard`:

```
HF_PROGRAM_MARK=1 codex exec "Made-up test: reply with the single word PEBBLE."
codex exec --json "Made-up test: reply with the single word PEBBLE."
```

**Expect:** `PEBBLE` twice; the second as JSON events.
**If not:** note it.
**Settles:** F-018, F-028, F-032, F-121.

### X-27. A missing hook binary (3 min)

Side: `hostrec mark X-27`, `hostrec arm missing`. Chat: `codex`, then `Made-up test: say hello.`, then `/quit`. Side: `hostrec disarm`.

**Expect:** the reply comes; at most a quiet warning.
**If not:** note what was shown.
**Settles:** F-050.

### X-28. Timed hook runs (10 min)

Side: `hostrec mark X-28`, `hostrec arm timed`. Chat: `codex`, then:

```
Made-up test. Run the shell command `true` 100 times, as 100 separate tool calls one after another, not in a loop or a script. Say done at the end.
```

Approve commands for the session if asked. Side, at the end: `hostrec timing codex`.

**Expect:** about 200 hook starts (one before and one after each call); `timing` prints the median and the 95th percentile, and the shell the hooks ran under.
**If not:** if the agent batches the commands, let it finish, note it, and type the prompt once more.
**Settles:** F-162, F-173.

### X-29. Closing right after a turn (4 min)

Side: `hostrec mark X-29`, `hostrec arm stop-child`. Type `Made-up test: say hello.` As soon as the reply appears, press Ctrl+C twice to quit Codex. Wait 15 seconds, then side: `hostrec children`.

**Expect:** the detached child's file appears; the end hook fired on the close.
**If not:** note it.
**Settles:** F-006, F-030, F-126.

### X-30. A job a hook starts, against the git server (2 min)

Side: `hostrec mark X-30`, `hostrec arm net-job`. Chat: `codex`, `Made-up test: say hello.`, wait 20 seconds, `/quit`. Side: `hostrec children`.

**Expect:** the job read the scratch repository and wrote under `~/.local/share/hf-test`.
**If not:** note it.
**Settles:** F-031, F-189, F-190.

### X-31. A chat left open (2 min, then 35 minutes in the background)

Side: `hostrec mark X-31`, `hostrec disarm`. Open a third Terminal window, `cd ~/scratch/beta-lantern && codex`, and leave it open, typing nothing. It stays open until X-47.

**Expect:** the scratch server stamps every minute; SessionStart follows its start within 90 seconds.
**If not:** note it.
**Settles:** F-044, F-045.

### X-32. A new binary behind approved hooks (4 min)

Side: `hostrec mark X-32`, `hostrec trust`, `hostrec swap-binary`, `hostrec trust`. Chat: `codex`, `Made-up test: say hello.`, `/quit`.

**Expect:** still trusted before and after; the hooks fire with the new build (its log line names the second build); the open chat of X-31 keeps its old server.
**If not:** note it.
**Settles:** F-044, F-153.

### X-33. A change to the plugin with no hooks (4 min)

Side: `hostrec mark X-33`, `hostrec plugin-edit codex tools-skill`, then the reinstall command it prints, then `hostrec trust`. Chat: `codex`, `Made-up test: say hello.`, `/quit`.

**Expect:** the hooks stay trusted and fire; no review asked.
**If not:** note it.
**Settles:** F-151.

### X-34. A stray file in Codex's copy (4 min)

Side: `hostrec mark X-34`, `hostrec plugin-edit codex stray-file`, `hostrec trust`, then the reinstall command it prints, then `hostrec trust`.

**Expect:** after the stray file, `modified`; after installing the same bytes again, `trusted`, with no review. `plugin-edit` also reports the shape of the cached copy's path.
**If not:** note each state.
**Settles:** F-154, F-155, F-160.

### X-35. A second plugin with hooks (4 min)

Side: `hostrec mark X-35`, then `codex plugin add hf-hooks-two@hf-scratch --json </dev/null`. Chat: `codex`, `Made-up test: say hello.`, `/quit`. Side: `hostrec trust`.

**Expect:** the first plugin's hooks fire; the second's wait for review.
**If not:** note it. Leave the second plugin unapproved.
**Settles:** F-159.

### X-36. A changed hook declaration (4 min)

Side: `hostrec mark X-36`, `hostrec plugin-edit codex hooks-timeout`, the reinstall command it prints, `hostrec trust`. Chat: `codex`, then `/hooks`: note whether a review is asked, and approve. `/quit`.

**Expect:** a review asked; trusted again after it.
**If not:** note it.
**Settles:** F-152, F-160.

### X-37. A skill changed inside the hooks plugin (8 min)

Side: `hostrec mark X-37`, `hostrec plugin-edit codex hooks-skill`, the reinstall command it prints, `hostrec trust`. Chat: `codex`. Wait two minutes without typing. Then type `Made-up test: say hello before approving.` Then `/hooks`: note whether a review is asked, approve, `/quit`. Side: `hostrec trust`.

**Expect:** the hooks are not trusted after the skill change; no hook line while you wait, though the scratch server starts and the session file changes; after approval, trusted again.
**If not:** note it.
**Settles:** F-024, F-043, F-150.

### X-38. Read-only reports (3 min)

Side: `hostrec mark X-38`, `hostrec cli-report codex`. It runs, with the network off once and on once, `codex --version`, `codex features list`, `codex plugin list --json` and the read-only `hooks/list`, and times each.

**Expect:** each prints without signing in or calling a model.
**If not:** note it.
**Settles:** F-057.

*A break fits here. The desktop app part starts with X-39.*

### X-39. The desktop app shares the approval (3 min)

Side: `hostrec mark X-39`. Open the Codex desktop app.

**Expect:** no hook review asked in the app.
**If not:** note what it asked.
**Settles:** F-156.

### X-40. A short chat in the app (6 min)

Side: `hostrec mark X-40`, `hostrec arm start`. In the app, before choosing anything, note how many SessionStart lines `hostrec tail` shows. Choose the project `~/scratch/alpha-orchard`, start a new thread, and type:

```
Made-up test: say hello from the app, and tell me any codeword you were given at the start.
```

Note how the `hf test` text showed in the app.

**Expect:** `MARIGOLD-TUESDAY`; the hooks name a session file under Codex's folder.
**If not:** note it.
**Settles:** F-011, F-046, F-137.

### X-41. A task in a worktree (4 min)

Side: `hostrec mark X-41`, `hostrec disarm`. In the app, start a new thread for `alpha-orchard` and choose to run it in a worktree, if offered. Type:

```
Made-up test: run `git status` and `pwd`, and tell me both results.
```

**Expect:** the task runs in a separate worktree folder.
**If not:** if no worktree is offered, note it.
**Settles:** F-127.

### X-42. Threads opened and never run (3 min)

Side: `hostrec mark X-42`. In the app's thread list, open the thread of X-40, look at it, and open another one, without typing. Then start a new thread and leave it without sending anything.

**Expect:** perhaps end hooks with no start; this step records which.
**If not:** nothing to do.
**Settles:** F-138.

### X-43. A scheduled run in the app (3 min)

Side: `hostrec mark X-43`. If the app offers scheduled tasks, make one for `alpha-orchard` that runs once, a minute from now, with the prompt `Made-up test: say tick.` Wait for it to run, then delete it.

**Expect:** the run arrives as a turn nobody typed.
**If not:** if the app offers no schedule, note "no schedule offered".
**Settles:** F-016, F-121.

### X-44. Voice call one: hand-overs, a correction, a yes, the pause phrase (12 min)

Side: `hostrec mark X-44`. In the app, choose `alpha-orchard`, start a new thread, and type nothing. Start a voice call with the app's voice button. Speak at your normal pace. Wait for each reply before the next line unless a line says otherwise.

1. **Say:** "Hello. This is a test with made-up content. Please ask the agent to run the shell command sleep thirty, and then list the files in the orchard folder."
   **Pause** until the screen shows the agent working (about 5 seconds).
2. While it is still working, **say:** "Also ask the agent to create a file named fig dot t x t in the orchard folder, containing the single word ripe."
   **Pause** 30 seconds, until the agent has finished both.
3. **Say:** "Don't send this one to the agent. Just tell me, in one sentence: what colour is a ripe plum?"
   **Pause** for the answer.
4. **Say:** "Suggest one short name for a new file about quinces. Don't create it yet."
   **Pause** for its suggestion. Then **say:** "Yes, use that name. Ask the agent to create that file with the word golden inside."
   **Pause** until the agent has finished.
5. **Say:** "Ask the agent to write the number four seven two one three into a file named port dot t x t."
   **Pause** until the agent is working. Then **say:** "Sorry, I said that wrong. The right number is four seven two three one. Ask the agent to fix port dot t x t."
   **Pause** until the agent has finished.
6. **Say:** "Off the record." **Pause** 3 seconds. **Say:** "Ask the agent to create a file named pebble dot t x t containing the word pebble."
   **Pause** until the agent has finished. **Say:** "Back on the record."
7. **Say:** "This last line is made-up and is not for the agent: the lantern is blue."
   **Pause** 5 seconds. End the call with the app's end button. Wait 10 seconds.

**Expect:** a hand-over for each request; the voice model answers line 3 itself.
**If not:** if the voice model hands over a line you meant to keep from the agent, or will not hand over a request, note which line, and go on.
**Settles:** F-121, F-141, F-142, F-143, F-144, F-145, F-147, F-148.

### X-45. Voice call two: a long stretch, ended by quitting the app (15 min)

Side: `hostrec mark X-45`. In the app, start a new thread for `alpha-orchard` and start a voice call.

1. **Say:** "This is a made-up test. For the next few minutes, please do not send anything to the agent. I will read you a made-up story. After each paragraph, reply with one short sentence."
2. **Read** the seven paragraphs below, one at a time, at your normal pace. After each, **pause** for its one-sentence reply. This takes about six minutes and passes the hand-over's 4 KiB limit.
3. **Say:** "Now please ask the agent to create a file named story-end dot t x t containing the word heron."
   **Pause** until the agent has finished.
4. **Say:** "Two more made-up lines, not for the agent. The tide was low at seven. The ferry left without its bell."
5. Without ending the call, quit the app with Cmd+Q. Wait 30 seconds.

The story, all made up:

> One. The first line of the story is this: the heron counted nine lanterns on the pier. The pier belonged to no one, and the lanterns had been left there by a ferry that no longer ran. Every evening the heron walked from one end to the other and counted them again, as if one might have walked away during the day. None ever had. The town behind the pier was small and quiet, with a bakery, a school, a post office painted green, and a clock tower that was always four minutes slow.
>
> Two. In the bakery worked a woman who made bread shaped like fish. Nobody remembered why she had started. Children bought them on the way to school and ate the tails first. On Fridays she made a larger loaf shaped like a whale, which was cut into slices and shared at the harbour. The harbour master said it was the only reason anyone came down to the water on Fridays, and he was probably right, because there was very little else to see there.
>
> Three. The school had eleven pupils and one teacher, who also kept bees. In spring the bees escaped into the classroom and the lessons stopped while everyone stood very still. The teacher said this was the most important lesson of the year: how to wait without moving while something small and loud decides what to do. The pupils agreed that it was more interesting than spelling, and some of them hoped the bees would escape more often.
>
> Four. The post office sold stamps with pictures of the lanterns on them. Each year the picture changed: one year the lanterns were drawn at night, another at dawn, another in the rain. Collectors from three towns away wrote to ask for the full set. The postmaster kept a folder of every design in a drawer, and on quiet afternoons she took it out and arranged the stamps in order, from the oldest to the newest, and then back again.
>
> Five. The clock tower was four minutes slow because the man who wound it believed the town deserved four extra minutes each day. He had never told anyone this. People simply learned to add four minutes in their heads, and visitors were always early for everything. The man who wound the clock thought this was a kindness to visitors too, since being early in a quiet town is pleasant, and being late anywhere is not.
>
> Six. One winter the lanterns stopped shining, all nine at once. The heron counted them in the dark and found the number unchanged, which seemed to satisfy it. The town held a meeting in the school. Someone suggested new lanterns, someone suggested new candles, and the baker suggested bread shaped like lanterns, which was not a solution but was very popular. In the end the teacher's bees were blamed, although nobody could explain how.
>
> Seven. In spring the lanterns began to shine again, one each night, until all nine were lit. Nobody knew who had done it. The postmaster drew that year's stamp: nine lanterns, one heron, and a loaf of bread shaped like a whale. The clock tower struck the hour four minutes late, as always, and the heron walked the pier and counted to nine. That is the end of the made-up story.

**Expect:** no hand-over during the story; one hand-over in step 3; nothing after it reaches a hook.
**If not:** if the voice model hands over during the story, note after which paragraph, and go on.
**Settles:** F-141, F-146, F-147, F-148, F-149.

### X-46. After the calls (3 min)

Side: `hostrec mark X-46`, `hostrec voice-check`. It counts the rows of the two calls' session files by type, beside the hand-overs the hooks saw, and compares the hand-overs' requests with this run-book's script.

**Expect:** a short table of counts; no text from the files.
**If not:** note it.
**Settles:** F-147, F-149.

### X-47. The chat left open (2 min)

Side: `hostrec mark X-47`. At least 35 minutes after X-31, look at the third Terminal window, then type `/quit` there. Side: `hostrec summary idle`.

**Expect:** an end hook fired after about 30 idle minutes, or at the quit; the server stamped every minute from one process.
**If not:** note it.
**Settles:** F-044, F-045, F-126.

### X-48. Reports with the plugin switched off (3 min)

Side: `hostrec mark X-48`. Type `codex plugin disable hf-hooks@hf-scratch --json </dev/null` (or the form your version takes), then `hostrec cli-report codex`, then `codex plugin enable hf-hooks@hf-scratch --json </dev/null`, then `hostrec trust`.

**Expect:** the reports name the plugin's state in each case.
**If not:** if enabling again asks for a review, approve it in `/hooks` and note it.
**Settles:** F-057.

### X-49. The scheduled job's times (2 min)

Side: `hostrec mark X-49`, `hostrec agent times`, then `hostrec agent remove`.

**Expect:** a time about every 15 minutes since sitting 0, with one soon after each wake from sleep.
**If not:** note the gaps.
**Settles:** F-200.

### X-50. End of sitting 2 (10 min)

Side: `hostrec mark X-50`, `hostrec summary codex`, then the hand-over check below.

**Expect:** every Codex event that fired, and every fact of this sitting with what was recorded.
**If not:** note any fact listed as missing.
**Settles:** F-120.

## What you bring back, and how you check it first

You bring back one thing: the recording folder, `~/hf-rec` in the evaluation account. Never the hosts' own folders (`~/.claude`, `~/.codex`, the desktop apps' folders), never `~/home-b` or `~/hf-alt-claude`, and never a screenshot of anything but the made-up content above.

The recorder keeps every hook payload whole, since their content is made up; of the hosts' own files it keeps only row types, field names, sizes and short values such as `true` or `user`, never text. Before you hand the folder over:

1. Type `hostrec scan ~/hf-rec`. It asks you once, without storing it, for the email address you sign in with and for any other word you want to be sure is absent, such as your own name or a project's name. It then searches the whole folder for those, for your own account's user name and home path, the Mac's computer name, every email address and anything shaped like a key or token, and prints each hit with its file and line.
2. Read the hits. Expect none, or only the made-up values of this run-book (`made-up-not-real-000` is one, and is never recorded as a value). For any other hit, type `hostrec scrub ~/hf-rec "<the text>"`, which replaces it with a made-up value of the same shape in every file, and run `hostrec scan` again until it is clean.
3. Type `hostrec pack ~/hf-rec`. It makes `~/hf-rec.tar.gz` and prints its size and a checksum. Hand that file to the lead the way you hand over other files, and say the checksum in the same message.
4. Afterwards you may delete the evaluation account, or keep it for the next sittings (DV-02's live gates use the same home).

The lead turns the recording into fixtures with every value replaced (the card's check fails if any value from the recording survives), writes each fact's result into [host-facts.md](host-facts.md) with the date and each host's version, and does not commit the recording itself.

## Facts to steps

Each fact the sheet marks for a sitting, and the steps that settle it. A fact on both hosts has steps in both host sittings.

| Fact | Sitting | Steps |
| --- | --- | --- |
| F-001 A turn id on every event of a turn | Both | C-04, X-04 |
| F-002 A tool-use id on tool events, and on the record's row | Both | C-04, C-05, X-04 |
| F-003 The question, plan-approval and delegation tools reach PostToolUse whole | Both | C-09, C-10, C-12, X-10, X-11, X-12 |
| F-004 The Stop payload carries the final reply | Both | C-04, C-15, X-04, X-14 |
| F-005 Payloads name the session's record file | Both | C-04, X-04 |
| F-006 SessionStart names its source, SessionEnd its reason | Both | C-26, C-27, C-39, X-23, X-24, X-29 |
| F-007 A failed tool call's payload holds the error and the exit status | Both | C-05, X-06 |
| F-008 An interrupted or failed turn is marked as ended | Both | C-15, C-36, X-14 |
| F-009 A typed slash command or skill reaches the prompt hook before the model | Both | C-17, C-18, X-16 |
| F-010 A message typed while the agent works fires a prompt hook | Both | C-14, X-13 |
| F-011 The desktop app's spare session starts again in the real folder | Both | C-38, X-40 |
| F-012 Each host reports its own version | Both | C-02, X-02 |
| F-013 A session id is safe in a file name | Both | C-04, X-04 |
| F-014 The session id survives a clear | Both | C-26, X-23 |
| F-015 A read or search in another repository names that repository | Both | C-23, X-20 |
| F-016 A prompt the host writes is marked as the host's | Both | C-15, C-17, C-19, C-25, C-27, X-14, X-16, X-21, X-43 |
| F-017 Pasted text is marked on Claude Code and not on Codex | Both | C-16, X-15 |
| F-018 A program's session can be told from a person's chat | Both | C-37, X-26 |
| F-019 The reply row is on disk when Stop fires | Both | C-04, X-04 |
| F-020 The record is appended in order | Both | C-04, X-04 |
| F-021 Each record row carries its time, folder and branch | Both | C-04, C-06, X-04, X-07 |
| F-022 Text between tool calls and each tool's status are separate rows | Both | C-04, X-04 |
| F-023 The store root comes from the host's environment | Both | C-35, X-25 |
| F-024 Each chat writes a session file even with no hook | Both | C-30, X-37 |
| F-025 SubagentStop carries the report and the agent id | Both | C-12, X-12 |
| F-026 SubagentStart can add context, and tells the host's own agents apart | Both | C-12, C-13, X-12 |
| F-027 A subagent's hook events carry its agent id | Both | C-12, X-12 |
| F-028 A hook inherits the host's environment | Both | C-35, C-37, X-25, X-26 |
| F-029 A hook's environment: home, shell, agent socket, path | Both | C-35, X-25 |
| F-030 A detached child outlives its hook | Both | C-04, C-39, X-29 |
| F-031 A job a hook starts can reach the git server | Both | C-40, X-30 |
| F-032 Each host runs headless on a plan login | Both | C-37, X-26 |
| F-033 A fork or resume names its parent | Both | C-27, X-24 |
| F-034 A compaction leaves a hook or a record row | Both | C-25, X-21, X-22 |
| F-035 An MCP tool's name names its server, and other tools are told apart | Both | C-20, C-22, X-17, X-19 |
| F-036 PostToolUse for an MCP tool holds its result text | Both | C-20, X-17 |
| F-037 An MCP server's instructions reach the model, within a size | Both | C-20, X-17 |
| F-038 The first use of an MCP tool asks once | Both | C-20, X-17 |
| F-039 A tool result has a size limit | Both | C-20, X-17 |
| F-040 An MCP server is not told which chat it serves | Both | C-20, X-17 |
| F-041 Whether a host reads an MCP server's resources | Both | C-20, X-17 |
| F-042 Where the time of an MCP call goes | Both | C-21, X-18 |
| F-043 A plugin's MCP server starts even when its hooks do not run | Both | C-30, X-37 |
| F-044 One MCP server process per chat, for the chat's life | Both | C-31, X-31, X-32, X-47 |
| F-045 SessionStart follows the MCP server's start within 90 seconds | Both | C-31, X-31, X-47 |
| F-046 A `systemMessage` is shown to the person and not to the model | Both | C-01, C-02, C-38, X-02, X-40 |
| F-047 SessionStart is synchronous | Both | C-01, C-02, X-02 |
| F-048 Several pre-tool hooks can run at once | Both | C-07, X-09 |
| F-049 The host adds its own context at a chat start | Both | C-02, X-02 |
| F-050 A missing hook binary leaves the chat unharmed | Both | C-29, X-27 |
| F-056 Plugins register and are removed with no question | Sitting 0 | S0-06 |
| F-057 Each CLI reports its state read-only | Both | C-41, X-38, X-48 |
| F-073 Each host signs in under another HOME and stays signed in | Sitting 0 | S0-04, S0-12, C-01, X-01 |
| F-074 A sign-in under another HOME leaves the account's own sign-in alone | Sitting 0 | S0-04, S0-12 |
| F-075 A plugin can declare every event v2 uses | Claude Code | C-42 |
| F-076 StopFailure fires on a failed turn | Claude Code | C-36 |
| F-077 PostModelSwitch fires on a model change | Claude Code | C-24 |
| F-078 The task tool's payload holds the whole list | Claude Code | C-11 |
| F-079 The entrypoint tells a person's chat from a program's | Claude Code | C-37, C-38 |
| F-080 UserPromptSubmit also fires for prompts no one typed | Claude Code | C-19 |
| F-081 PreToolUse holds the path or the command | Claude Code | C-06, C-07 |
| F-082 PostToolBatch names each call of the batch | Claude Code | C-08 |
| F-083 A resume's SessionStart says how long it has been | Claude Code | C-27 |
| F-084 Transcript rows carry ids and flags | Claude Code | C-04 |
| F-085 Transcripts hold system prompts and request records | Claude Code | C-04 |
| F-087 The memory index is a local file | Claude Code | C-34 |
| F-088 Injected text: whole up to 10,000 characters | Claude Code | C-03 |
| F-089 PreToolUse context arrives beside the result | Claude Code | C-06 |
| F-090 An async hook's context arrives a turn later | Claude Code | C-08 |
| F-091 A subagent gets instruction files and not the start context | Claude Code | C-12 |
| F-092 A hook past its timeout shows a notice | Claude Code | C-28 |
| F-093 An MCP tool can load at start | Claude Code | C-02 |
| F-094 A user-level rules file with a path pattern | Claude Code | C-33 |
| F-098 PostCompact carries the summary | Claude Code | C-25 |
| F-099 PreCompact steers the summary | Claude Code | C-25 |
| F-100 PostCompact fires before SessionStart `compact` | Claude Code | C-25 |
| F-101 SessionStart `compact` output joins the new context | Claude Code | C-25 |
| F-102 Session end gives hooks 1.5 seconds | Claude Code | C-39 |
| F-103 A changed hooks file is used with no review | Claude Code | C-32 |
| F-104 Hooks run in exec form with the plugin root expanded | Claude Code | C-29 |
| F-105 The desktop app's Code tab runs its own Claude Code | Claude Code | C-38 |
| F-120 A plugin can declare every event v2 uses | Codex | X-04, X-14, X-50 |
| F-121 The typed-prompt mark in the record | Codex | X-04, X-12, X-26, X-43, X-44 |
| F-122 That row is on disk when the prompt hook fires | Codex | X-04 |
| F-123 PreToolUse adds context, and an edit matcher matches `apply_patch` | Codex | X-07 |
| F-124 An async PostToolUse's context arrives before the next request | Codex | X-08 |
| F-125 A hook message: about 2,500 tokens, 3,000 declared | Codex | X-03 |
| F-126 Session end on close and after 30 idle minutes | Codex | X-29, X-47 |
| F-127 A managed worktree as the working folder | Codex | X-41 |
| F-132 The rollout is written in a known history mode | Codex | X-04 |
| F-133 Rollout rows have a stable position | Codex | X-04 |
| F-134 The opening row names the session and folder, and can be large | Codex | X-05 |
| F-135 PostCompact carries only the trigger | Codex | X-21 |
| F-136 The rollout holds the compaction summary | Codex | X-21 |
| F-137 Desktop threads name a session file eva-md can read | Codex | X-40 |
| F-138 A thread opened and never run ends with no start | Codex | X-42 |
| F-141 A hand-over reaches the prompt hook as a wrapper | Codex | X-44, X-45 |
| F-142 The request is the front model's wording | Codex | X-44 |
| F-143 Each spoken line keeps its speaker | Codex | X-44 |
| F-144 A task voice starts by itself begins inside the wrapper | Codex | X-44 |
| F-145 A hand-over while the agent works shares its turn's id | Codex | X-44 |
| F-146 Each field of a hand-over holds at most 4 KiB | Codex | X-45 |
| F-147 Talk past the limit and the call's end are rows no hook saw | Codex | X-44, X-45, X-46 |
| F-148 Nothing after the last hand-over reaches a hook | Codex | X-44, X-45 |
| F-149 The session file holds a line for each utterance | Codex | X-45, X-46 |
| F-150 Approval is bound to every byte of the hooks plugin | Codex | X-01, X-37 |
| F-151 A change to a plugin with no hooks costs no review | Codex | X-33 |
| F-152 A changed hook declaration asks for a new review | Codex | X-36 |
| F-153 A new binary behind an approved hook keeps the approval | Codex | X-32 |
| F-154 A stray file unapproves; the same bytes again restore it | Codex | X-34 |
| F-155 Codex runs a cached copy of the plugin | Codex | X-34 |
| F-156 One approval holds for the CLI and the desktop app | Codex | X-39 |
| F-157 Hooks load only from `.codex-plugin/plugin.json` | Codex | X-01 |
| F-158 Two plugins from one marketplace | Sitting 0 | S0-06 |
| F-159 A second hooks plugin leaves the first approved | Codex | X-35 |
| F-160 `hooks/list` reports each hook's trust | Codex | X-01, X-34, X-36 |
| F-161 Approving in `/hooks` is all it takes | Sitting 0 | S0-07 |
| F-162 Hook commands run through a non-login shell | Codex | X-28 |
| F-173 One hook start costs 100 ms or less at the 95th percentile | Codex | X-28 |
| F-174 SessionStart `compact` runs before the next request, also mid-turn | Codex | X-22 |
| F-175 The server takes a push of machine commits | Sitting 0 | S0-11 |
| F-176 The real wiki's project has no push rule against them | Sitting 0 | S0-08 |
| F-177 The real wiki's git server is reachable without a private network | Sitting 0 | S0-08 |
| F-178 A read with prompts off never waits | Sitting 0 | S0-10 |
| F-179 An anonymous read of a private repository fails fast | Sitting 0 | S0-10 |
| F-180 A background job knows the server's host key | Sitting 0 | S0-11 |
| F-181 Release downloads reach a background job | Sitting 0 | S0-11 |
| F-182 Write deploy keys exist and can be added | Sitting 0 | S0-13 |
| F-183 The default branch takes a push with a lease | Sitting 0 | S0-08, S0-14 |
| F-185 The Mac's git does what the store needs | Sitting 0 | S0-09 |
| F-186 `ssh-keygen` makes and signs with a key, with no prompt | Sitting 0 | S0-09 |
| F-187 Signature letters for known and unknown keys | Sitting 0 | S0-09 |
| F-188 Per-call settings never start the person's signing program | Sitting 0 | S0-09 |
| F-189 The git a background job finds is new enough | Sitting 0 | S0-11, C-40, X-30 |
| F-190 No privacy prompt for the store's folder | Sitting 0 | S0-11, C-40, X-30 |
| F-191 The machine key pushes with no agent | Sitting 0 | S0-11, S0-13 |
| F-200 A launchd user agent installs without an admin and keeps time | Sitting 0 | S0-11, X-49 |
| F-201 Work the scheduler starts reaches the model and the git server | Sitting 0 | S0-11 |
| F-202 No device management forbids user launch agents | Sitting 0 | S0-02 |
| F-203 Installing an agent shows a background-item notice | Sitting 0 | S0-11 |
| F-204 A notification from a scheduled job needs no permission | Sitting 0 | S0-11 |

## Steps to facts

| Step | Minutes | Facts |
| --- | --- | --- |
| S0-01 Make the evaluation account | 10 | none |
| S0-02 Device management | 2 | F-202 |
| S0-03 Tools for the account | 15 | none |
| S0-04 Sign both hosts in | 10 | F-073, F-074 |
| S0-05 Build the recorder and the scratch folders | 10 | none |
| S0-06 Register the scratch plugins with no terminal attached | 5 | F-056, F-158 |
| S0-07 Approve the Codex hooks | 5 | F-161 |
| S0-08 The scratch repository on the git server | 10 | F-176, F-177, F-183 |
| S0-09 The machine's git checks | 5 | F-185, F-186, F-187, F-188 |
| S0-10 Reads from the git server with prompts off | 5 | F-178, F-179 |
| S0-11 The scheduled job | 15 | F-175, F-180, F-181, F-189, F-190, F-191, F-200, F-201, F-203, F-204 |
| S0-12 A sign-in under another HOME | 10 | F-073, F-074 |
| S0-13 Optional: a push with a deploy key | 5 | F-182, F-191 |
| S0-14 Optional: a forced push with a lease | 3 | F-183 |
| S0-15 End of sitting 0 | 3 | none |
| C-01 Start | 3 | F-046, F-047, F-073 |
| C-02 The first prompt | 4 | F-012, F-046, F-047, F-049, F-093 |
| C-03 Long start texts | 5 | F-088 |
| C-04 A turn with two tools and words between | 4 | F-001, F-002, F-004, F-005, F-013, F-019, F-020, F-021, F-022, F-030, F-084, F-085 |
| C-05 A command that fails | 2 | F-002, F-007 |
| C-06 An edit on another branch, with a word from the pre-tool hook | 4 | F-021, F-081, F-089 |
| C-07 Parallel edits | 3 | F-048, F-081 |
| C-08 Two reads, and a word from an async hook | 5 | F-082, F-090 |
| C-09 A multiple-choice question | 5 | F-003 |
| C-10 A plan approved and a plan rejected | 5 | F-003 |
| C-11 A task list | 3 | F-078 |
| C-12 A subagent | 6 | F-003, F-025, F-026, F-027, F-091 |
| C-13 A custom agent | 3 | F-026 |
| C-14 A message typed while the agent works | 4 | F-010 |
| C-15 An interrupt | 2 | F-004, F-008, F-016 |
| C-16 Pasted text | 3 | F-017 |
| C-17 A shell line and a plugin command | 3 | F-009, F-016 |
| C-18 The pause phrase, typed | 2 | F-009 |
| C-19 Prompts nobody typed | 3 | F-016, F-080 |
| C-20 The scratch MCP server | 8 | F-035, F-036, F-037, F-038, F-039, F-040, F-041 |
| C-21 The same lookup by MCP and by command | 5 | F-042 |
| C-22 A web fetch and a network command | 3 | F-035 |
| C-23 Another repository | 3 | F-015 |
| C-24 A model change | 2 | F-077 |
| C-25 A compaction | 5 | F-016, F-034, F-098, F-099, F-100, F-101 |
| C-26 A clear | 2 | F-006, F-014 |
| C-27 Resume and fork | 6 | F-006, F-016, F-033, F-083 |
| C-28 A hook past its time limit | 2 | F-092 |
| C-29 A missing hook binary | 3 | F-050, F-104 |
| C-30 A chat with hooks switched off | 4 | F-024, F-043 |
| C-31 A chat left open | 3 | F-044, F-045 |
| C-32 A changed hooks file | 3 | F-103 |
| C-33 A rules file with a path pattern | 5 | F-094 |
| C-34 A note for the host's own memory | 3 | F-087 |
| C-35 The environment a hook gets | 4 | F-023, F-028, F-029 |
| C-36 A turn that fails | 3 | F-008, F-076 |
| C-37 A run a program drives | 3 | F-018, F-028, F-032, F-079 |
| C-38 The desktop app's Code tab | 6 | F-011, F-046, F-079, F-105 |
| C-39 Ending a chat | 4 | F-006, F-030, F-102 |
| C-40 A job a hook starts, against the git server | 2 | F-031, F-189, F-190 |
| C-41 Read-only reports | 3 | F-057 |
| C-42 End of sitting 1 | 5 | F-075 |
| X-01 Start | 3 | F-073, F-150, F-157, F-160 |
| X-02 The first prompt | 4 | F-012, F-046, F-047, F-049 |
| X-03 Long hook messages | 6 | F-125 |
| X-04 A turn with two tools and words between | 4 | F-001, F-002, F-004, F-005, F-013, F-019, F-020, F-021, F-022, F-120, F-121, F-122, F-132, F-133 |
| X-05 Long instructions | 3 | F-134 |
| X-06 A command that fails | 2 | F-007 |
| X-07 An edit on another branch, with a word from the pre-tool hook | 4 | F-021, F-123 |
| X-08 A word from an async hook, in the same turn | 3 | F-124 |
| X-09 Parallel edits | 3 | F-048 |
| X-10 A multiple-choice question | 4 | F-003 |
| X-11 A plan approved and a plan rejected | 5 | F-003 |
| X-12 A subagent | 6 | F-003, F-025, F-026, F-027, F-121 |
| X-13 A message typed while the agent works | 4 | F-010 |
| X-14 An interrupt | 2 | F-004, F-008, F-016, F-120 |
| X-15 Pasted text | 3 | F-017 |
| X-16 A shell line, a skill and the pause phrase, typed | 3 | F-009, F-016 |
| X-17 The scratch MCP server | 8 | F-035, F-036, F-037, F-038, F-039, F-040, F-041 |
| X-18 The same lookup by MCP and by command | 5 | F-042 |
| X-19 A web search and a network command | 3 | F-035 |
| X-20 Another repository | 3 | F-015 |
| X-21 A compaction by command | 5 | F-016, F-034, F-135, F-136 |
| X-22 A compaction in the middle of a turn | 8 | F-034, F-174 |
| X-23 A clear | 2 | F-006, F-014 |
| X-24 Resume and fork | 6 | F-006, F-033 |
| X-25 The environment a hook gets | 4 | F-023, F-028, F-029 |
| X-26 A run a program drives | 3 | F-018, F-028, F-032, F-121 |
| X-27 A missing hook binary | 3 | F-050 |
| X-28 Timed hook runs | 10 | F-162, F-173 |
| X-29 Closing right after a turn | 4 | F-006, F-030, F-126 |
| X-30 A job a hook starts, against the git server | 2 | F-031, F-189, F-190 |
| X-31 A chat left open | 2 | F-044, F-045 |
| X-32 A new binary behind approved hooks | 4 | F-044, F-153 |
| X-33 A change to the plugin with no hooks | 4 | F-151 |
| X-34 A stray file in Codex's copy | 4 | F-154, F-155, F-160 |
| X-35 A second plugin with hooks | 4 | F-159 |
| X-36 A changed hook declaration | 4 | F-152, F-160 |
| X-37 A skill changed inside the hooks plugin | 8 | F-024, F-043, F-150 |
| X-38 Read-only reports | 3 | F-057 |
| X-39 The desktop app shares the approval | 3 | F-156 |
| X-40 A short chat in the app | 6 | F-011, F-046, F-137 |
| X-41 A task in a worktree | 4 | F-127 |
| X-42 Threads opened and never run | 3 | F-138 |
| X-43 A scheduled run in the app | 3 | F-016, F-121 |
| X-44 Voice call one: hand-overs, a correction, a yes, the pause phrase | 12 | F-121, F-141, F-142, F-143, F-144, F-145, F-147, F-148 |
| X-45 Voice call two: a long stretch, ended by quitting the app | 15 | F-141, F-146, F-147, F-148, F-149 |
| X-46 After the calls | 3 | F-147, F-149 |
| X-47 The chat left open | 2 | F-044, F-045, F-126 |
| X-48 Reports with the plugin switched off | 3 | F-057 |
| X-49 The scheduled job's times | 2 | F-200 |
| X-50 End of sitting 2 | 10 | F-120 |
