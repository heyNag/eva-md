# The host sittings: a run-book

This run-book is for the owner, at a Mac. It settles the host facts of [host-facts.md](host-facts.md) that only the hosts themselves can show: what a hook receives, when a row reaches the host's record, how a voice call reaches a hook, what a plugin change costs in reviews. It is the first half of HF-00 in [the v2 plan](plan.md) (Stage 0); the recorder it uses is the second half, described in the card [HF-00](cards/HF-00.md).

## Before anything else

**What it is for.** eva.md v2 rests on facts only the hosts can tell. The six design papers each list the ones they assume. 141 of the 216 facts in the fact sheet are settled here, in three sittings: one for the evaluation home, the machine and the git server, and one for each host. A short part after them takes everything down and hands the recording over. A small recorder keeps what the hooks receive. Afterwards the lead turns the recording into test fixtures with every value replaced by a made-up one, and marks each fact confirmed or refuted.

**What you need.**

- **An administrator's password for the Mac**, once, to add a user account in sitting 0. No later step asks for one.
- **An evaluation home.** A separate user account on the Mac, made in sitting 0, with both hosts installed and signed in with your own subscription. A separate account is safer than a separate folder: a host may keep its sign-in in the login keychain, which a folder does not separate. Sitting 0 still tests the folder way, inside the evaluation account, for the evaluation harness that needs it (F-073, F-074).
- **Both hosts' CLIs and desktop apps.** The CLIs are installed in sitting 0 inside the evaluation account only. The desktop apps in `/Applications` are shared by all accounts and need no install.
- **A scratch repository on the git server your wiki uses**, which you create in sitting 0 with your own account's browser, private and empty, and a key for it that sitting 0 makes inside the evaluation account. It receives one push. You need neither `gh` nor `glab`, and nothing signs either one in.
- **The recorder** (`hostrec`), with its scratch plugins and scratch MCP server. It is not built yet: it is the second half of HF-00, described in [the card](cards/HF-00.md). This run-book cannot be followed until that card is merged.
- **Go**, installed in sitting 0 inside the evaluation account, to build the recorder from a checkout of this public repository.
- **A microphone and a quiet room** for the two voice calls in Codex's desktop app: no one else speaking, and no television, radio or phone playing. A call transcribes every voice it hears.
- **Days between the sittings.** At least one night between sitting 0 and sitting 2, with the Mac asleep at least once in between: X-01 checks a sign-in and an approval days old, and X-49 the scheduled job's times across a sleep. Sitting 1 fits on any day after sitting 0.

**What is never touched.** Your real install and its home: your own account's eva-md, its wiki, its inbox, its hooks, `~/.claude` and `~/.codex` in your own account, and the host versions it runs. In your own account you only use System Settings (S0-01 and S0-02), your browser on the git server (S0-08, S0-10 and E-04), where you create the scratch repository, add its key and read your real project's settings without changing them, and TextEdit, for two files in `/Users/Shared`. Never use `sudo`, and never install, upgrade or sign in a host from your own account for these sittings. If a host's instructions need either, stop and tell the lead. eva-md itself is not installed in the evaluation home; only the scratch plugins are. Every prompt, name, word and value below is made up for this run-book and stands for nothing real.

**How long it takes.** Each step states its minutes. The sums below are the least each part takes; plan for about a fifth more, for first-time screens and waits no step foresaw.

| Part | What | Minutes, at least | Plan for |
| --- | --- | --- | --- |
| Sitting 0 | The evaluation home, the machine and the git server | 141 | about 170 |
| Sitting 1 | Claude Code | 164 | about 195 |
| Sitting 2 | Codex: the CLI, then the desktop app with two voice calls | 242 | about 290 |
| After the sittings | Take down, check and hand over | 35 | about 40 |
| All | | 582 | about 695 |

**Where a sitting can stop.** Between any two steps, except inside three spans that hold a chat open: C-31 to C-38, X-31 to X-32, and X-39 to X-47. Each part says where a break fits. Before you stop, type `hostrec close` in the side window; the scratch hooks record nothing while the recording is closed, and the next `hostrec mark` opens it again. If you must stop inside a span, close its open chat first, note it with `hostrec note`, and when you come back start the span again from its first step.

## How to follow a step

- You use two Terminal windows in the evaluation account. **The chat window** runs the host. **The side window** runs `hostrec` commands. Some steps open a third window for a chat left open.
- Every step starts with `hostrec mark <step>` in the side window, so the recording knows where each step begins. The first mark of a sitting opens the recording; `hostrec close` at the end of the sitting closes it.
- `hostrec tail`, in the side window, prints one line for each hook event since the step's mark (its time, host and event name, never what it held), and returns.
- Text in a box after **Type** is typed exactly, then Enter. Text after **Say** is spoken. **Type and say only the run-book's text.** If anything else was typed in a chat, or heard during a voice call (another voice, a television, a phone), type `hostrec note "off-script at <step>"`, so the lead drops that step's payloads.
- **Expect** says what should happen. Where the screen cannot show it, it says so, and the recording checks it. **If not** says what to do. When something unexpected happens, write one line with `hostrec note "what you saw"` in the side window, and go on. Do not retry a step more than once, and never change a setting a step does not name.
- **The hosts' own questions.** Answer them so the step can go on. When a host asks whether to trust a new folder, trust it: the scratch folders are made up. When a host asks to run a command or edit a file the prompt asked for, approve it, choosing the answer that stops it asking again for that command when one is offered; refuse anything the prompt did not ask for, and note it. When macOS asks for the microphone at the first voice call, allow it. During a call, approve a command the agent asks for by clicking. When closing a window or quitting an app asks you to confirm, confirm. When a desktop app offers to update, choose Later.
- **Only the run-book's chats.** Both desktop apps are signed in to your own subscription, so their lists may show chats, threads or tasks from elsewhere. Open only the chats and threads this run-book creates, and leave every other one closed.
- **Settles** names the facts a step gives evidence for. The tables at the end map each fact to its steps and each step to its facts.
- **Arms.** Before each step that needs one, `hostrec arm <name>` makes the scratch hooks return a planted word, sleep, or start a background job. Arming changes a file outside the plugins: no arm touches a plugin, on either host, so arming never costs a Codex review. Each `arm` replaces the one before. Every arm except the start-text probes of C-03 and X-03 also gives each new, resumed or cleared chat the start word `MARIGOLD-TUESDAY`. `hostrec disarm` puts the hooks back to recording only, with no word.

## Sitting 0: the evaluation home, the machine and the git server

141 minutes at least. A break fits after S0-09.

### S0-01. Make the evaluation account (10 min)

1. In your own account, open System Settings, Users & Groups, and add a user; macOS asks for an administrator's password. Choose Standard, full name `HF Eval Home`, account name `hfeval`. Give it a password you keep for these sittings.
2. Turn on fast user switching if it is off (System Settings, Control Center), so you can switch between your own account and this one without logging out.
3. Log in to `hfeval`. Skip every offer of the first-login screens (Apple Account, iCloud, Siri, Screen Time): this account needs none of them.

**Expect:** a fresh desktop for `hfeval`.
**If not:** if the Mac does not let you add a user, stop here; the lead plans the sittings again for a folder used as `HOME`.
**Settles:** nothing on its own; every later step relies on it.

### S0-02. Device management (2 min)

1. In `hfeval`, open System Settings, General, Device Management (on older macOS: Privacy & Security, Profiles). Note whether any profile is listed.
2. Switch to your own account and look at the same page, reading only. Switch back to `hfeval`.

**Expect:** no profile in either account.
**If not:** a profile is listed. Note only that one is there and whether its name mentions login items or background items; never its other details. You type that note after S0-05, when `hostrec note` exists.
**Settles:** F-202.

### S0-03. Tools for the account (20 min)

Everything here installs into the evaluation account's own home. Never use `sudo`, Homebrew or a global npm install for it: they write to folders your own account owns. In Terminal in `hfeval`:

1. Check what the Mac already has:

   ```
   git --version
   command -v ssh-keygen
   uname -m
   ```

   `uname -m` prints `arm64` on Apple silicon or `x86_64` on Intel; it decides two names below.
2. Set this account's PATH:

   ```
   mkdir -p ~/hf-tools ~/.local/bin
   echo 'export PATH="$HOME/hf-tools:$HOME/hf-tools/go/bin:$HOME/.local/bin:$PATH"' >> ~/.zshrc
   ```

   Then open a new Terminal window, so the line takes effect, and close the old one.
3. Go, unpacked inside this account (on Intel, write `amd64` in place of `arm64`):

   ```
   curl -fsSL -o ~/hf-tools/go.tgz https://go.dev/dl/go1.26.0.darwin-arm64.tar.gz
   tar -C ~/hf-tools -xzf ~/hf-tools/go.tgz && rm ~/hf-tools/go.tgz
   go version
   ```

4. Claude Code's CLI, by its own installer, which installs into this account's `~/.local` only:

   ```
   curl -fsSL https://claude.ai/install.sh | bash
   claude --version
   ```

5. Codex's CLI, as the program from its releases on GitHub, put in `~/hf-tools` (on Intel, write `x86_64` in place of `aarch64`, both times):

   ```
   curl -fsSL -o ~/hf-tools/codex.tgz https://github.com/openai/codex/releases/latest/download/codex-aarch64-apple-darwin.tar.gz
   tar -C ~/hf-tools -xzf ~/hf-tools/codex.tgz && rm ~/hf-tools/codex.tgz
   mv ~/hf-tools/codex-aarch64-apple-darwin ~/hf-tools/codex
   codex --version
   ```

**Expect:** `git` prints a version and `ssh-keygen` a path; each version command prints a version; nothing asks for an administrator's password.
**If not:** if `git` asks to install the command line tools, cancel and stop: they are shared by every account on the Mac, so installing them changes what your real install runs; tell the lead. If anything asks for an administrator's password, cancel and stop, and tell the lead. If a download fails, tell the lead, who checks the hosts' install pages (the addresses are among the run-book's untried assumptions, at the end).
**Settles:** nothing on its own.

### S0-04. Sign both hosts in (10 min)

1. Type `claude` in Terminal. Answer its first screens (a colour theme, trusting the folder), sign in with your own subscription when it asks (it opens the browser of this account), then type `/exit`.
2. Type `codex login` and sign in with your own subscription.
3. Open the Claude desktop app and the Codex desktop app, sign each in, and quit both.

**Expect:** each host says you are signed in.
**If not:** if a sign-in reuses your own account's session without asking, stop: tell the lead before going on, because the evaluation home is then not separate.
**Settles:** nothing on its own; the sign-ins are checked again in S0-14.

### S0-05. Build the recorder and the scratch folders (10 min)

Type:

```
git clone https://github.com/heyNag/eva-md.git ~/hf-src/eva-md
cd ~/hf-src/eva-md && go build -o ~/hf-tools/hostrec ./cmd/hostrec
hostrec setup --rec ~/hf-rec
```

**Expect:** `hostrec setup` says it found no eva-md install and no earlier session in this account, and makes the recording folder `~/hf-rec`; a private folder `~/hf-private`, which is never handed over; the scratch repositories `~/scratch/alpha-orchard` and `~/scratch/beta-lantern`, a plain folder `~/scratch/outside`, and `~/scratch/long-notes` with a long made-up `AGENTS.md`; and the scratch marketplaces under `~/hf`. All their content is made up: fruit, lanterns and orchards.
**If not:** if `setup` refuses because it found an eva-md install or an earlier session, stop: this is not a fresh evaluation account. For any other error, copy it into `hostrec note` if that runs, or into a message to the lead, and stop.
**Settles:** nothing on its own.

### S0-06. Register the scratch plugins with no terminal attached (8 min)

Type `hostrec register`. It runs the hosts' own commands that add the scratch marketplaces and plugins, as `hostrec setup` printed them, each with no terminal attached and nothing to read from, so a question would fail rather than wait:

```
claude plugin marketplace add ~/hf/marketplace/claude-code --json </dev/null
claude plugin install hf-recorder@hf-scratch --scope user --json </dev/null
codex plugin marketplace add ~/hf/marketplace/codex --json </dev/null
codex plugin add hf-hooks@hf-scratch --json </dev/null
codex plugin add hf-tools@hf-scratch --json </dev/null
```

A command that fails is tried once more without `--json`. At the end `register` lists each host's plugins.

**Expect:** each command ends with exit status 0 and no question, and the list shows `hf-recorder` for Claude Code, and `hf-hooks` and `hf-tools` for Codex.
**If not:** for each command `register` reports as failed:

1. Type it yourself in the side window as `register` printed it last, without `</dev/null`. If it asks a question, answer it so the plugin is added, and type `hostrec note "S0-06: <the command's first four words> asked: <the question, in a few words>"`. That is a result for F-056, not a failure.
2. If it fails again, add the plugin through the host's own plugin screen (`/plugin` in Claude Code, `/plugins` in Codex), and note that you did.
3. Type `hostrec register --check`. If a scratch plugin is still not listed, stop and tell the lead: sittings 1 and 2 need all three.

**Settles:** F-056, F-158.

### S0-07. Approve the Codex hooks (5 min)

1. Type `cd ~/scratch/alpha-orchard && codex`.
2. In Codex, type `/hooks`. Approve every hook of `hf-hooks`. Leave Codex with `/quit` (or Ctrl+C twice).
3. In the side window type `hostrec trust`.

**Expect:** `/hooks` lists the scratch hooks as needing review; after approval `hostrec trust` reports them trusted. No other screen asked anything but whether to trust the folder.
**If not:** note what `/hooks` showed and what `hostrec trust` reports.
**Settles:** F-161.

### S0-08. The scratch repository on the git server (12 min)

1. Switch to your own account. In your browser, on the git server your wiki uses, create a new private, empty repository named `hf-scratch-plum`, with no README.
2. Copy its SSH address, which ends in `hf-scratch-plum.git`. Open TextEdit, make a new plain-text document (Format, Make Plain Text), paste the address alone, and save it as `hf-scratch-address.txt` in the folder `/Users/Shared` (in the save dialog, Cmd+Shift+G lets you type that folder).
3. On the repository's settings page, look at the default branch: is it protected, and are forced pushes allowed? Keep the two answers in mind.
4. Open your real wiki's project settings on that server, reading only, for the four answers of the second note below. Change nothing.
5. Switch back to `hfeval`. In the side window type `hostrec remote set`. It reads the address file, shows the repository's name, refuses any repository not named `hf-scratch-plum`, asks you to confirm with `y`, and deletes the file. Then type the two notes:

   ```
   hostrec note "scratch main protected: yes|no; force push allowed: yes|no"
   hostrec note "real project: push rule on committer address yes|no; signed commits required yes|no; main protected against force push yes|no; server reachable only on a private network yes|no"
   ```

   keeping one word of each pair. If S0-02 found a profile, type its note now as well.

**Expect:** `remote set` shows `hf-scratch-plum` and stores its address; the notes are saved. Nothing of the real project is run or changed. Your answers stay in the recording, which never enters the repository; the fact sheet records only which way the design goes.
**If not:** if you cannot create a repository there, create it on github.com in the same way and say so in a note. If `remote set` refuses the name, check that the file holds the scratch repository's address, never your real wiki's.
**Settles:** F-176, F-177, F-183.

### S0-09. The machine's git checks (5 min)

Type `hostrec machine`.

**Expect:** a list of checks run in a scratch folder with made-up files, each pass or fail: git's version; a commit signed by a fresh ed25519 key given only per call, and its verification; the signature letters for an enrolled and an unknown key; a stand-in signing program set in the account's git config that must not be called; the three versions of a conflicted file after a merge with no commit; a symlink checked out as a plain file; `core.fsync` accepted; a push into a branch that is not checked out; maintenance with `gc.auto=0`; `ssh-keygen -Y verify` on a made-up signature; the extended attributes of a fetched file. No window or prompt appears.
**If not:** note any prompt or dialog that appeared, and which check it came with.
**Settles:** F-185, F-186, F-187, F-188.

*A break fits here.*

### S0-10. A key for the scratch repository (8 min)

1. In the side window type `hostrec key`. It makes a new key, `~/.ssh/hf_scratch_deploy`, in this account only, with no passphrase, and writes its public half to `/Users/Shared/hf-scratch-deploy-key.pub`.
2. Switch to your own account. Open that file with TextEdit and copy its one line. In your browser, on `hf-scratch-plum`'s settings, add a deploy key: title `hf-eval-made-up`, the copied line as the key, and write access allowed. Close the file and move it to the Trash.
3. Switch back to `hfeval`.

Never copy a key, an SSH agent or a password manager from your own account into the evaluation account, and never add this key to your own user's keys on the server. The key reaches `hf-scratch-plum` and nothing else.

**Expect:** the server lists the deploy key on `hf-scratch-plum`, with write access.
**If not:** if the server offers no deploy keys, or none with write access, note the class (for example `hostrec note "S0-10: deploy keys switched off"`) and tell the lead: the push of S0-13 then cannot be made.
**Settles:** F-182.

### S0-11. The scheduled job (15 min)

1. Type `hostrec agent install`. It writes a launchd user agent named `org.example.hf-tick` and loads it with `launchctl bootstrap`. Every 15 minutes it appends a time to the recording, also while no sitting is open. On its first run it also: runs `claude -p "Made-up test: reply with the single word PEBBLE."` and `codex exec "Made-up test: reply with the single word PEBBLE."`; reads the scratch repository once with `git ls-remote` in batch mode with its own SSH setup, before anyone has accepted the server's host key; fetches one public eva-md release asset by name; writes, reads and removes a file under `~/.local/share/hf-test`; notes which git it finds; and posts one made-up notification with `osascript`.
2. Watch the screen for a "background item added" notice and for any password, keychain or permission dialog. Answer no dialog: press Cancel or Deny.
3. Wait for the first run to end (up to 5 minutes), then type `hostrec agent first-run`.

**Expect:** no administrator password asked; perhaps a one-time background-item notice; one notification titled with the made-up text; `first-run` shows each part's outcome. The read most likely stops at the unknown host key: that is a result, not a failure.
**If not:** note each dialog with its wording.
**Settles:** F-180, F-181, F-189, F-190, F-200, F-201, F-203, F-204.

### S0-12. The server's host key, and reads with prompts off (10 min)

1. Type `hostrec remote read`. It first connects once by SSH with the deploy key, in this window, so that you can accept the server's host key: when it asks "Are you sure you want to continue connecting", type `yes`. (If your server's help pages publish its host-key fingerprint, you may compare the two first.) It then puts the deploy key in this account's SSH agent, runs `git ls-remote` against `hf-scratch-plum` with prompts off and a 10-second limit, and makes an anonymous HTTPS read with no credentials.
2. Lock the agent: type `ssh-add -x`, and give it the made-up lock password `made-up-lock-1` twice. Then type `hostrec remote read --again`.
3. Unlock it with `ssh-add -X` and the same made-up password, then empty it with `ssh-add -D`.

**Expect:** the host-key question once; the first read succeeds in a second or two; the anonymous read fails fast; with the agent locked, the read fails fast and no dialog waits.
**If not:** if a dialog appears, note what it asked and answer nothing; it should time out. If the first read fails, note its class and tell the lead before S0-13.
**Settles:** F-178, F-179, F-180.

### S0-13. The scheduled job's push (8 min)

Type `hostrec agent kick`. It starts the scheduled job now and waits for that run to end (up to 5 minutes). This run reads the scratch repository in batch mode with its own SSH setup again, now that the host key is accepted. For this run only, it adds a line to this account's SSH config naming a made-up agent for the server. It then reads and pushes with the deploy key passed per call and no agent socket: the sitting's one push to `hf-scratch-plum`, two commits authored as `eva.md (hf-test) <hf-test@machines.eva-md.invalid>`, one signed with a fresh key no account knows and one unsigned. Last it removes the config line and prints each part's outcome.

**Expect:** the read with the job's own setup gets past the host-key check (it may then fail for want of a key, which is expected); the read with the deploy key succeeds; the push is accepted, or refused with a class of refusal; no dialog; on the server, `hf-scratch-plum` holds two commits if it was accepted.
**If not:** note each dialog with its wording. If the job did not run at all, type `hostrec push` once in the side window: it makes the same push from this window with the same key, and it is still the sitting's one push.
**Settles:** F-175, F-180, F-182, F-191, F-201.

### S0-14. A sign-in under another HOME (10 min)

1. Type `hostrec keychain-names before`. It keeps, never showing them, a hash of the name of each of the login keychain's items.
2. Type `mkdir -p ~/home-b && HOME=~/home-b claude`. Sign in when asked, type `Made-up test: say hello.`, then `/exit`.
3. Type `HOME=~/home-b codex login` and sign in.
4. Type `claude` (with the account's own HOME). It should still be signed in. Type `/exit`. Type `codex login status`.
5. Type `hostrec keychain-names after`. It shows the names of the items added or changed since step 1, never their contents or account fields.

**Expect:** both hosts sign in under `~/home-b` and make their folders there; the account's own sign-in still holds afterwards.
**If not:** if the account's own sign-in was replaced or lost, note it, and sign in again as in S0-04.
**Settles:** F-073, F-074.

### S0-15. The deploy-key commands (2 min)

Type `hostrec deploy-key`. It looks for `gh` and `glab` on this Mac, signs in to nothing, and reports only whether each is installed and whether its help shows a deploy-key command with write access. Do not install either for this.

**Expect:** a short report; "neither installed" is a result too.
**If not:** note it.
**Settles:** F-182.

### S0-16. Optional: a forced push with a lease (3 min)

Only if you agree to one more push to `hf-scratch-plum`. Type `hostrec force-lease`. It rewrites the last commit of `hf-scratch-plum` and pushes it with `--force-with-lease`, with the deploy key.

**Expect:** accepted where forced pushes are allowed, refused where `main` is protected.
**If not:** note the outcome.
**Settles:** F-183.

### S0-17. End of sitting 0 (3 min)

Type `hostrec summary s0`. It lists, by fact number, what was recorded. Then type `hostrec close`. Leave the evaluation account logged in (switch back to your own with fast user switching), so the scheduled job keeps running. If the Mac sleeps before the next sitting, that is wanted: the job's times show the wake.

**Settles:** nothing new.

## Sitting 1: Claude Code

164 minutes at least. Breaks fit after C-13 and after C-27. Step C-31 leaves a chat open while later steps go on, until C-38 closes it: do not stop between them.

All steps run in `hfeval`, in `~/scratch/alpha-orchard` unless a step says otherwise.

### C-00. Open the windows (2 min)

Log in to `hfeval` (fast user switching). Open Terminal with two windows, the chat window and the side window, and type `cd ~/scratch/alpha-orchard` in each. Side window: `hostrec mark C-00`, which opens the recording.

**Expect:** both windows in `~/scratch/alpha-orchard`; `mark` says the recording is open.
**If not:** if `hostrec` is not found, open a new window: the PATH line of S0-03 applies to new windows only.
**Settles:** nothing on its own.

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

Then side: `hostrec tail`.

**Expect:** 1 is `MARIGOLD-TUESDAY`; 2 is most likely no; 3 lists `hf_lookup` (the tool marked to load at start) and perhaps not the others. `tail` shows a SessionStart line and a UserPromptSubmit line.
**If not:** note the three answers as given. If `tail` shows no SessionStart or no UserPromptSubmit line, stop: the scratch hooks are not recording; tell the lead.
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

When it asks to run `ls orchard`, approve it with the answer that stops it asking again for that command.

**Expect:** a read, a shell command, short sentences between them, and a final reply.
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

When the plan is shown with its choices, pick the one that approves the plan and still asks before each edit (worded like "Yes, and manually approve edits"), then approve the rename when it asks. After the rename, enter plan mode again and type:

```
Made-up test. Plan how you would delete orchard/b.txt.
```

This time pick the choice that keeps planning (worded like "No, keep planning"), and type `Not now, this is a test.`

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
Made-up test. First tell me the codeword you were given at the start of this chat, and the project word from your instruction file, or NONE for each. Then delegate this to a general-purpose subagent: it should first tell you any word in capitals it was given when it started, then the codeword given at the start of this chat if it knows it, then the project word from its instruction file if it has one, then read orchard/pears.txt, add a line `checked: yes` to it, and report back in one sentence. Include the subagent's three answers in your answer.
```

**Expect:** you get `MARIGOLD-TUESDAY` and `WILLOW-CANDLE` first; a subagent runs; it reports `SILVER-OTTER`, most likely does not know `MARIGOLD-TUESDAY`, and most likely knows `WILLOW-CANDLE`; the file gains the line.
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

If it asks before each `sleep 1`, approve the first with the answer that stops it asking again for that command. After it has printed about five numbers, and while no question is open, type while it is still working:

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

If it asks to run the command, approve it first. About five seconds after the command has started, press Esc.

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

Type `/exit`, then `claude --continue --fork-session`, which forks the newest chat of this folder. If that is refused, type `claude --resume`, pick the newest alpha-orchard chat in the list it shows, and take its fork choice if it has one. Then type:

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

Side: `hostrec mark C-29`, `hostrec arm missing`. It renames `~/hf-tools/hf-missing-check`, which one declared hook names, outside every plugin. The exec-form hook, whose script lies under the plugin root, keeps running. Type `Made-up test: say hello.` Then side: `hostrec disarm`, which puts the file back.

**Expect:** the reply comes; at most a quiet notice. Nothing more to see on screen: the recording shows the exec-form hook's line, with the plugin root expanded.
**If not:** note exactly what was shown, and whether you had to do anything to go on.
**Settles:** F-050, F-104.

### C-30. A chat with hooks switched off (4 min)

Side: `hostrec mark C-30`, `hostrec disarm`. Chat: `/exit`, then `claude --settings '{"disableAllHooks": true}'`. Wait two minutes without typing. Then type `Made-up test: say hello with hooks off.` and `/exit`.

**Expect:** the reply. Nothing more to see on screen: the recording checks that no hook line came from this chat, that the scratch MCP server started, and that the host's own session file changed.
**If not:** if the setting is refused, note it.
**Settles:** F-024, F-043.

### C-31. A chat left open (3 min, then open until C-38)

Side: `hostrec mark C-31`. Open a third Terminal window, type `cd ~/scratch/beta-lantern && claude`, and leave it open, typing nothing, while the next steps go on. C-35 swaps the recorder's program under it, and C-38 closes it.

**Expect:** Claude Code opens in the third window. Nothing more to see on screen: the recording checks that the scratch server's minute stamps keep coming from one process, that SessionStart fired within 90 seconds of the server's start, and that no new server starts after the swap.
**If not:** note it.
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

Then side: `hostrec disarm`, which removes the rules file.

**Expect:** the word is known in alpha-orchard after the read; the answers in the other places are what this step measures.
**If not:** note each answer.
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

### C-35. The environment a hook gets, and another config folder (10 min)

Side: `hostrec mark C-35`, then `hostrec swap-binary` (it replaces `~/hf-tools/hostrec` with a second build of itself, under the chat C-31 left open).

1. The environment. Chat: `/exit`, then:

   ```
   HF_FAKE_API_TOKEN=made-up-not-real-000 HF_PLAIN_MARK=1 claude
   ```

   Type `Made-up test: say hello. Then run the shell command command -v hf-linked-name.` Then `/exit`.
2. Another config folder. Side: `hostrec register claude-code --config-dir ~/hf-alt-claude`, which registers the scratch plugin there as S0-06 did, with no terminal attached. Chat:

   ```
   CLAUDE_CONFIG_DIR=~/hf-alt-claude claude
   ```

   Claude Code treats this folder as new: answer its first screens (a colour theme, trusting the folder), and sign in again with the same subscription if it asks. Type `Made-up test: say hello.` Then `/exit`.

**Expect:** two replies; `command -v` prints a path in `~/hf-tools`. Nothing more to see on screen: the recording checks that both made-up variable names reached the hooks (never their values), and that in part 2 the hooks fired and the record lies under `~/hf-alt-claude`.
**If not:** note it. If `register` fails for the other folder, note it and do part 2 anyway: `hostrec summary` still finds, by name only, the session file made under `~/hf-alt-claude`.
**Settles:** F-023, F-028, F-029.

### C-36. A turn that fails (3 min)

Side: `hostrec mark C-36`. Chat: `claude`. Disconnect the network (Wi-Fi off in Control Center, or the cable out). Type `Made-up test: say hello.` Wait for the error. Connect the network again.

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

Side: `hostrec mark C-38`. First close the chat C-31 left open: in the third window type `/exit`, and close that window. Then side: `hostrec arm start`.

Open the Claude desktop app. If it lists chats from elsewhere, leave them closed. Open its About window (from the app's menu) and type `hostrec note "claude app version: <the version shown>"`. Go to its Code tab. Before you choose anything, type `hostrec tail` in the side window and note how many SessionStart lines it shows. Choose the folder `~/scratch/alpha-orchard`, and type:

```
Made-up test: say hello from the desktop app, and tell me any codeword you were given at the start.
```

Note how the `hf test` start line showed there, if at all. Quit the app.

**Expect:** the codeword is `MARIGOLD-TUESDAY`, so the hooks fired in the app.
**If not:** note it.
**Settles:** F-011, F-046, F-079, F-105.

### C-39. Ending a chat (4 min)

Side: `hostrec mark C-39`, `hostrec arm end-slow`. Chat: `claude`, type `Made-up test: say hello.`, then `/exit`. Then `claude` again, type `Made-up test: say hello.`, and close the whole Terminal window with Cmd+W. Wait 15 seconds, open a new window and type `hostrec children`.

**Expect:** `children` lists a file for each detached child. Nothing more to see on screen: the recording checks that the end hook's lines stop after about 1.5 seconds, and that each child's file appeared about 10 seconds after its hook.
**If not:** note it.
**Settles:** F-006, F-030, F-102.

### C-40. A job a hook starts, against the git server (2 min)

Side: `hostrec mark C-40`, `hostrec arm net-job`. Chat: `cd ~/scratch/alpha-orchard && claude`, type `Made-up test: say hello.`, wait 20 seconds, `/exit`. Side: `hostrec children`.

**Expect:** the Stop hook's job read the scratch repository and wrote under `~/.local/share/hf-test`.
**If not:** note it.
**Settles:** F-031, F-189, F-190.

### C-41. Read-only reports (3 min)

Side: `hostrec mark C-41`, `hostrec cli-report claude-code`. It runs `claude --version`, `claude --help` and `claude plugin list --json`, and times each, once with the network off and once with it on. When it says so, disconnect the network (Wi-Fi off, or the cable out) and press Enter; when it says so again, connect it and press Enter.

**Expect:** each prints without signing in or calling a model.
**If not:** note it.
**Settles:** F-057.

### C-42. End of sitting 1 (5 min)

Side: `hostrec mark C-42`, `hostrec disarm`, `hostrec summary claude-code`.

**Expect:** a list of every Claude Code event that fired, and of every fact of this sitting with what was recorded for it.
**If not:** `summary` names, for each fact it lists as missing, the steps that settle it. If the sitting has time, redo those steps now, each starting with its own `hostrec mark`, then type `hostrec summary claude-code` again. If not, leave them and tell the lead.

Then type `hostrec close`.
**Settles:** F-075.

## Sitting 2: Codex

242 minutes at least: the CLI part (X-00 to X-38) is 172 minutes and the desktop app part (X-39 to X-50) 70. A break fits after X-20, and before X-39, where the desktop app starts; the desktop app part can be on another day, starting with X-00 again. Step X-31 leaves a chat open until the end of X-32, and step X-39 one until X-47: do not stop between them.

All steps run in `hfeval`, in `~/scratch/alpha-orchard` unless a step says otherwise.

### X-00. Open the windows (2 min)

Log in to `hfeval` (fast user switching). Open Terminal with two windows, the chat window and the side window, and type `cd ~/scratch/alpha-orchard` in each. Side window: `hostrec mark X-00`, which opens the recording.

**Expect:** both windows in `~/scratch/alpha-orchard`; `mark` says the recording is open.
**If not:** if `hostrec` is not found, open a new window: the PATH line of S0-03 applies to new windows only.
**Settles:** nothing on its own.

### X-01. Start (3 min)

Side: `hostrec mark X-01`, `codex login status`, `hostrec trust`.

**Expect:** `codex login status` says Codex is still signed in, and `trust` reports the scratch hooks still trusted, days after their approval, loaded from the plugin's `.codex-plugin/plugin.json`.
**If not:** if they are not trusted, note it, approve them in `/hooks`, and go on.
**Settles:** F-073, F-150, F-157, F-160.

### X-02. The first prompt (4 min)

Side: `hostrec mark X-02`, `hostrec arm start`. Chat: `codex`, then:

```
This is a test with made-up content. Answer on two lines. 1: What codeword, if any, were you given at the start of this chat? Give the word only, or NONE. 2: Did you see a line that starts with "hf test"? Yes or no.
```

Then side: `hostrec tail`.

**Expect:** 1 is `MARIGOLD-TUESDAY`; Codex showed the `hf test` text as a warning; 2 is most likely no. `tail` shows a SessionStart line and a UserPromptSubmit line.
**If not:** note how the line showed and the answers. If `tail` shows no SessionStart or no UserPromptSubmit line, stop: the scratch hooks are not recording; tell the lead.
**Settles:** F-012, F-046, F-047, F-049.

### X-03. Long hook messages (10 min)

Side: `hostrec mark X-03`. For each of the eight arms below, type the arm in the side window, then in the chat window `/quit`, `codex`, and the prompt:

```
Made-up test. At the very end of the text you were given at the start of this chat there is a word in capitals. What is it? Word only, or NONE.
```

Arms, in order, each a made-up text of that many characters given through the handler that declares a limit, then through the one that does not:

```
hostrec arm start-probe 7000 declared
hostrec arm start-probe 7000 plain
hostrec arm start-probe 8300 declared
hostrec arm start-probe 8300 plain
hostrec arm start-probe 11000 declared
hostrec arm start-probe 11000 plain
hostrec arm start-probe 14000 declared
hostrec arm start-probe 14000 plain
```

**Expect:** the word comes back for the shortest texts; somewhere up the ladder it stops coming back, perhaps at a larger size with the declared limit. Nothing more to see on screen: the recording counts the files Codex writes the rest to, at each size.
**If not:** note the eight answers.
**Settles:** F-125.

### X-04. A turn with two tools and words between (4 min)

Side: `hostrec mark X-04`, `hostrec arm stop-child`. Chat: `/quit`, `codex`, then:

```
Made-up test. First write one sentence saying what you will do. Then read the file orchard/plums.txt and tell me in one sentence what it says. Then run the shell command `ls orchard` and tell me how many files it lists. Write a short sentence before each step.
```

When it asks to run a command, approve it with the answer that stops it asking again for that command.

**Expect:** tool calls, short sentences between them, and a final reply.
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

When Codex shows the plan and asks whether to carry it out, pick the choice that carries it out; if no choice shows, type `Yes, carry out the plan.` Then plan mode again:

```
Made-up test. Plan how you would delete orchard/e.txt.
```

This time pick the choice that does not carry it out, or, if no choice shows, type `No, do not carry it out.` Then type `Not now, this is a test.`

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

If it asks before each `sleep 1`, approve the first with the answer that stops it asking again for that command. After about five numbers, and while no question is open, type:

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

If it asks to run the command, approve it first. About five seconds after the command has started, press Esc.

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

Side: `hostrec mark X-21`, `hostrec arm compact`. Chat: `/quit`, `cd ~/scratch/alpha-orchard && codex resume`, pick the newest alpha-orchard chat in the list it shows, then `/compact`. Then:

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

Side: `hostrec mark X-24`. `/quit`, wait two minutes, `cd ~/scratch/alpha-orchard && codex resume`, pick the newest alpha-orchard chat in the list, then `Made-up test: say hello again.` Then fork the chat the way your version offers (`/fork`, or editing an earlier message), and type `Made-up test: say hello from the fork.`

**Expect:** the resumed chat continues; the fork starts as a new chat.
**If not:** note it.
**Settles:** F-006, F-033.

### X-25. The environment a hook gets, and another Codex home (10 min)

Side: `hostrec mark X-25`.

1. The environment. Chat: `/quit`, then:

   ```
   HF_FAKE_API_TOKEN=made-up-not-real-000 HF_PLAIN_MARK=1 codex
   ```

   Type `Made-up test: say hello. Then run the shell command command -v hf-linked-name.`, then `/quit`.
2. Another Codex home. Side: `hostrec register codex --codex-home ~/hf-alt-codex`, which registers the scratch plugins there as S0-06 did. Chat: `CODEX_HOME=~/hf-alt-codex codex login`, and sign in with the same subscription. Then `CODEX_HOME=~/hf-alt-codex codex`, type `/hooks`, approve every hook of `hf-hooks`, and type `Made-up test: say hello.` Then `/quit`.

**Expect:** two replies; `command -v` prints a path in `~/hf-tools`. Nothing more to see on screen: the recording checks that both made-up variable names reached the hooks (never their values), whether `CODEX_HOME` is set for them, and that in part 2 the hooks fired and the record lies under `~/hf-alt-codex`.
**If not:** note it. If `register` fails for the other home, note it and do part 2 anyway: `hostrec summary` still finds, by name only, the session file made under `~/hf-alt-codex`.
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

**Expect:** about 200 hook starts (one before and one after each call); `timing` prints the median and the 95th percentile, apart for the plain starts and for those that also hashed a made-up folder of 2 KB, and the shell the hooks ran under.
**If not:** if the agent batches the commands, let it finish, note it, and type the prompt once more.
**Settles:** F-162, F-173.

### X-29. Closing right after a turn (4 min)

Side: `hostrec mark X-29`, `hostrec arm stop-child`. Type `Made-up test: say hello.` As soon as the reply appears, press Ctrl+C twice to quit Codex. Wait 15 seconds, then side: `hostrec children`.

**Expect:** `children` lists the detached child's file. Nothing more to see on screen: the recording checks that the end hook fired on the close.
**If not:** note it.
**Settles:** F-006, F-030, F-126.

### X-30. A job a hook starts, against the git server (2 min)

Side: `hostrec mark X-30`, `hostrec arm net-job`. Chat: `codex`, `Made-up test: say hello.`, wait 20 seconds, `/quit`. Side: `hostrec children`.

**Expect:** the job read the scratch repository and wrote under `~/.local/share/hf-test`.
**If not:** note it.
**Settles:** F-031, F-189, F-190.

### X-31. A chat left open (2 min, then open until the end of X-32)

Side: `hostrec mark X-31`, `hostrec disarm`. Open a third Terminal window, `cd ~/scratch/beta-lantern && codex`, and leave it open, typing nothing. X-32 closes it.

**Expect:** Codex opens in the third window. Nothing more to see on screen: the recording checks that the scratch server stamps every minute, and that SessionStart followed its start within 90 seconds.
**If not:** note it.
**Settles:** F-044, F-045.

### X-32. A new binary behind approved hooks (4 min)

Side: `hostrec mark X-32`, `hostrec trust`, `hostrec swap-binary`, `hostrec trust`. Chat: `codex`, `Made-up test: say hello.`, `/quit`. Then, in the third window, type `/quit` and close that window.

**Expect:** `trust` reports the hooks trusted before and after the swap. Nothing more to see on screen: the recording checks that the hooks fired with the new build and that the open chat of X-31 kept its old server.
**If not:** note it.
**Settles:** F-044, F-153.

### X-33. A change to the plugin with no hooks (4 min)

Side: `hostrec mark X-33`, `hostrec plugin-edit codex tools-skill`, then the reinstall command it prints, then `hostrec trust`. Chat: `codex`, `Made-up test: say hello.`, `/quit`.

**Expect:** the hooks stay trusted and fire; no review asked.
**If not:** note it.
**Settles:** F-151.

### X-34. A stray file and a link in Codex's copy (5 min)

Side: `hostrec mark X-34`, `hostrec plugin-edit codex stray-file`, `hostrec trust`, `hostrec plugin-edit codex stray-link`, `hostrec trust`, then the reinstall command it prints, then `hostrec trust`.

**Expect:** after the stray file, `modified`; after the made-up link, an error or `modified`; after installing the same bytes again, `trusted`, with no review. `plugin-edit` also reports the shape of the cached copy's path.
**If not:** note each state.
**Settles:** F-154, F-155, F-160.

### X-35. A second plugin with hooks (5 min)

Side: `hostrec mark X-35`, then `hostrec register codex hf-hooks-two`. Chat: `codex`, `Made-up test: say hello.`, `/quit`. Side: `hostrec trust`, then `hostrec unregister codex hf-hooks-two`, which removes it again with no terminal attached, leaving it unapproved.

**Expect:** `trust` reports the first plugin's hooks trusted and the second's waiting for review; the removal ends with no question.
**If not:** note it. If the removal fails, type it as `unregister` printed it, without `</dev/null`, and note any question it asked.
**Settles:** F-056, F-159.

### X-36. A changed hook declaration (4 min)

Side: `hostrec mark X-36`, `hostrec plugin-edit codex hooks-timeout`, the reinstall command it prints, `hostrec trust`. Chat: `codex`, then `/hooks`: note whether a review is asked, and approve. `/quit`.

**Expect:** a review asked; trusted again after it.
**If not:** note it.
**Settles:** F-152, F-160.

### X-37. A skill changed inside the hooks plugin (8 min)

Side: `hostrec mark X-37`, `hostrec plugin-edit codex hooks-skill`, the reinstall command it prints, `hostrec trust`. Chat: `codex`. Wait two minutes without typing. Then type `Made-up test: say hello before approving.` Then `/hooks`: note whether a review is asked, approve, `/quit`. Side: `hostrec trust`.

**Expect:** the first `trust` reports the hooks not trusted after the skill change; `/hooks` asks for a review; the last `trust` reports them trusted again. Nothing more to see on screen: the recording checks that no hook line came while you waited, though the scratch server started and the session file changed.
**If not:** note it.
**Settles:** F-024, F-043, F-150.

### X-38. Read-only reports (3 min)

Side: `hostrec mark X-38`, `hostrec cli-report codex`. It runs `codex --version`, `codex features list`, `codex plugin list --json` and the read-only `hooks/list`, and times each, once with the network off and once with it on. When it says so, disconnect the network (Wi-Fi off, or the cable out) and press Enter; when it says so again, connect it and press Enter.

**Expect:** each prints without signing in or calling a model.
**If not:** note it.
**Settles:** F-057.

*A break fits here. The desktop app part starts with X-39.*

### X-39. The desktop app shares the approval, and a chat left idle (5 min)

Side: `hostrec mark X-39`.

1. Open a third Terminal window, type `cd ~/scratch/beta-lantern && codex`, and leave it open, typing nothing, until X-47.
2. Open the Codex desktop app. If it lists chats, threads or tasks from elsewhere, leave them closed. Open its About window (from the app's menu) and type `hostrec note "codex app version: <the version shown>"`. Open its settings, find the voice setting, and type `hostrec note "voice setting: <what it shows>"`. Close the settings.

**Expect:** no hook review asked in the app.
**If not:** note what it asked.
**Settles:** F-126, F-142, F-156.

### X-40. A short chat in the app (6 min)

Side: `hostrec mark X-40`, `hostrec arm start`. In the app, before choosing anything, type `hostrec tail` in the side window and note how many SessionStart lines it shows. Choose the project `~/scratch/alpha-orchard`, start a new thread, and type:

```
Made-up test: say hello from the app, and tell me any codeword you were given at the start.
```

Note how the `hf test` text showed in the app.

**Expect:** `MARIGOLD-TUESDAY`, so the hooks fired in the app. Nothing more to see on screen: the recording checks that the hooks name a session file under Codex's folder.
**If not:** note it.
**Settles:** F-011, F-046, F-137.

### X-41. A task in a worktree (4 min)

Side: `hostrec mark X-41`, `hostrec disarm`. In the app, start a new thread for `alpha-orchard` and choose to run it in a worktree, if offered. Type:

```
Made-up test: run `git status` and `pwd`, and tell me both results.
```

Then leave this thread alone: do not open it again until the app is quit in X-45. It is the app's idle thread for F-126.

**Expect:** the task runs in a separate worktree folder.
**If not:** if no worktree is offered, note it, and run it in the folder itself.
**Settles:** F-126, F-127.

### X-42. Threads opened and never run (3 min)

Side: `hostrec mark X-42`. In the app's thread list, open the thread of X-40 and look at it, without typing. Then start a new thread for `alpha-orchard` and leave it without sending anything. Then open the thread of X-40 again. Do not open the thread of X-41, or any thread this run-book did not create.

**Expect:** perhaps end hooks with no start; this step records which.
**If not:** nothing to do.
**Settles:** F-138.

### X-43. A scheduled run in the app (3 min)

Side: `hostrec mark X-43`. If the app offers scheduled tasks, make one for `alpha-orchard` that runs once, a minute from now, with the prompt `Made-up test: say tick.` Wait for it to run, then delete it.

**Expect:** the run arrives as a turn nobody typed.
**If not:** if the app offers no schedule, note "no schedule offered".
**Settles:** F-016, F-121.

### X-44. Voice call one: hand-overs, a correction, a yes, the pause phrase (12 min)

Side: `hostrec mark X-44`. Make sure the room is quiet, with no other voice, television or phone. In the app, choose `alpha-orchard`, start a new thread, and type nothing. Start a voice call with the app's voice button; if macOS asks for the microphone, allow it. Speak at your normal pace. Wait for each reply before the next line unless a line says otherwise. If the agent asks to run a command, approve it by clicking.

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

### X-45. Voice call two: a long stretch, ended by quitting the app (22 min)

Side: `hostrec mark X-45`. In the app, start a new thread for `alpha-orchard` and start a voice call, in the same quiet room.

1. **Say:** "This is a made-up test. For the next quarter of an hour, please do not send anything to the agent. I will read you a made-up story. After each paragraph, reply with one short sentence."
2. **Read** the seventeen paragraphs below, one at a time, at your normal pace. After each, **pause** for its one-sentence reply. This takes about fifteen minutes. The story alone is 8,626 bytes of text, more than twice the hand-over's 4 KiB limit, so the talk passes the limit even if the voice model's replies are kept apart from your words.
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
> Seven. Every spring a travelling knife grinder came to the town on a bicycle with a stone wheel fixed to the back. He set up beside the post office and sharpened anything people brought him: kitchen knives, garden shears, the teacher's scissors and once, by mistake, a spoon. He charged nothing for the spoon. He told the children that he had cycled through forty towns and that this one was the only town whose clock was wrong on purpose. Nobody had told him about the four minutes, and nobody ever found out how he knew. When he left, he rang his bell twice at the end of the pier, and the heron did not look up.
>
> Eight. The harbour master kept a logbook of every boat that came in, even though very few boats came in. Most pages said only the date and the words "no boats". On one page he had written "one boat, painted yellow, did not stop", and the whole town had read that page, because the logbook lay open on a table in the harbour office for anyone to see. People argued for years about where the yellow boat had been going. The baker said it was carrying flour. The teacher said it was carrying bees. The postmaster said it was carrying letters that had been posted in the wrong town.
>
> Nine. In the summer the school went on a trip to the island in the bay, which could be reached on foot at low tide. The teacher walked in front with a long stick, testing the sand, and the eleven pupils followed in a line. On the island they found a broken rowing boat, a family of crabs and a wooden sign that said "island", which made everyone laugh. They ate fish-shaped bread for lunch and walked back before the tide turned. One pupil kept a pebble from the island on her windowsill, and said it was the only pebble in the town that had been to sea and come back.
>
> Ten. The town had one road in and one road out, and they were the same road. A sign at the edge of town said "welcome" on one side and "come back soon" on the other. Someone had once painted a small heron on the sign, under the word welcome, and nobody had painted over it. Lorries rarely came, because there was nothing to deliver that the town could not make or grow. When a lorry did come, the children ran beside it to the bakery, where the driver was always given a loaf, and the driver always said he would be back, and usually he was.
>
> Eleven. The post office had a parrot named Ledger, who could say "next please" and "mind the step" and nothing else. He said "next please" whenever the door opened, even when nobody was waiting, and "mind the step" whenever somebody left, even though there was no step. The postmaster had tried to teach him to say "first class", but he never learned it. Visitors thought the parrot was very clever. The town knew he was not clever at all, only polite, and agreed that a polite parrot was better than a clever one, since a clever parrot might say things nobody wanted to hear.
>
> Twelve. On the hill above the town there was an orchard of plum trees that belonged to everyone. In autumn people went up with baskets and took what they needed, and left the rest for the birds. There was a rule, written on a board at the gate, that nobody could take more plums than they could carry in one basket without stopping to rest. The rule had never been broken, partly because the hill was steep and partly because nobody had a very large basket. The baker made plum bread in the shape of a plum, which some people thought was not trying hard enough.
>
> Thirteen. In winter the man who wound the clock fell ill and could not climb the tower. For a week nobody wound it, and the clock stopped at twenty past three. The town found that it did not mind very much. People met each other when they were ready rather than when the clock said so, and the school started when the teacher arrived. When the man was well again, he climbed the tower, wound the clock, and set it four minutes slow, as before. Some people said they missed the week without a clock. The heron, as far as anyone could tell, had not noticed either way.
>
> Fourteen. The baker had an apprentice, a tall boy who was very good at making bread and very bad at making it fish-shaped. His fish looked like boots, or clouds, or once like a small sheep. The children bought them anyway and argued about what each one was. After a while the baker gave up correcting him and put a sign in the window that said "shapes vary". The boy was so pleased with the sign that he made a loaf in the shape of the sign itself, which was the first loaf he had ever made that looked exactly like what it was meant to be.
>
> Fifteen. Every year, on the longest night, the town held a quiet evening on the pier. Nobody made a speech and nobody played music. People brought a cup of something warm, stood near the lanterns, and talked in low voices about the year. The harbour master read out the number of boats that had come in, which was usually small, and everyone clapped as if it were large. The children were allowed to stay up late, and most of them fell asleep on their parents' coats before midnight, which was the way the evening was meant to end.
>
> Sixteen. One autumn a letter arrived at the post office addressed only to "the heron, the pier". The postmaster did not know what to do with it. She could not deliver it, because herons have no letterbox, and she could not send it back, because it had no return address. In the end she read it aloud on the pier one evening, so that the heron could hear it. The letter said only "thank you for counting". The heron walked to the end of the pier and back, which the town agreed was as good a reply as any.
>
> Seventeen. In spring the lanterns began to shine again, one each night, until all nine were lit. Nobody knew who had done it. The postmaster drew that year's stamp: nine lanterns, one heron, and a loaf of bread shaped like a whale. The clock tower struck the hour four minutes late, as always, and the heron walked the pier and counted to nine. That is the end of the made-up story.

**Expect:** no hand-over during the story; one hand-over in step 3, after which the agent creates the file. Nothing more to see on screen: the recording checks that nothing after that hand-over reached a hook.
**If not:** if the voice model hands over during the story, note after which paragraph, and go on.
**Settles:** F-141, F-146, F-147, F-148, F-149.

### X-46. After the calls (3 min)

Side: `hostrec mark X-46`, `hostrec voice-check`. It counts the rows of the two calls' session files by type, beside the hand-overs the hooks saw, and compares the hand-overs' requests with this run-book's script. It also prints the byte count of the talk before X-45's hand-over, and the names of any version fields the wrapper or the session file carries.

**Expect:** a short table of counts; the talk's byte count well over 4,096; no text from the files.
**If not:** note it.
**Settles:** F-147, F-149.

### X-47. The chat left open (2 min)

Side: `hostrec mark X-47`. At least 35 minutes after X-39, look at the third Terminal window, then type `/quit` there and close the window. Side: `hostrec summary idle`.

**Expect:** `summary idle` says, for the CLI chat of X-39 and for the app's thread of X-41, whether an end hook fired after about 30 idle minutes, at the quit, or not at all, and that the server stamped every minute from one process. If X-41's thread had less than 35 minutes before the app was quit, it says so, and the app's half stays unsettled.
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

### X-50. End of sitting 2 (5 min)

Side: `hostrec mark X-50`, `hostrec summary codex`.

**Expect:** every Codex event that fired, and every fact of this sitting with what was recorded.
**If not:** `summary` names, for each fact it lists as missing, the steps that settle it. If the sitting has time, redo those steps now, each starting with its own `hostrec mark`, then type `hostrec summary codex` again. If not, leave them and tell the lead.

Then type `hostrec close`.
**Settles:** F-120.

## After the sittings: take down, check and hand over

35 minutes at least, on the day of sitting 2 or later. You bring back one thing: the recording folder, `~/hf-rec` in the evaluation account. Never the hosts' own folders (`~/.claude`, `~/.codex`, the desktop apps' folders), never `~/hf-private`, `~/home-b`, `~/hf-alt-claude` or `~/hf-alt-codex`, and never a screenshot of anything but the made-up content above.

The recorder keeps every hook payload whole, since their content is made up. Of the hosts' own files it keeps only row types, field names, sizes, a few short values such as `true` or `user` from a fixed list of fields, ids, and for every other value its length and a hash, which says nothing of the text without the key kept in `~/hf-private`, and that key is never handed over. It never keeps a row's text.

### E-01. Remove the scratch plugins (5 min)

Side: `hostrec mark E-01`, `hostrec disarm`, then `hostrec unregister all`. It removes the scratch plugins and marketplaces from both hosts with the hosts' own commands, each with no terminal attached, and records whether any asked a question.

**Expect:** each removal ends with exit status 0 and no question, and the lists it prints at the end hold no scratch plugin.
**If not:** for a removal that failed, type it as `unregister` printed it, without `</dev/null`; if it asks a question, answer it and note the question. If the lead later finds a fact missing, `hostrec register` puts the plugins back for the steps to redo.

Then type `hostrec close`.
**Settles:** F-056.

### E-02. Check the recording (10 min)

Type `hostrec scan ~/hf-rec`. It asks you once, never storing the answers: the email address you sign in with; your full name; your own account's short name (it shows its guess from the Mac's list of users, for you to confirm); and any other word you want to be sure is absent, such as a project's name. It then searches every file of the recording.

It sorts what it finds. Values the hosts and the recorder make (session ids, turn and tool-use ids, commit hashes, `hf_lookup`'s `r:` tokens, the keys `hostrec` made) are only counted, by kind, and need nothing from you. It shows you only the hits to judge, each with its file and line: anything that matches what you typed, an account's name on this Mac, the computer's name, an email address, or a string shaped like a key or a token that it cannot explain.

**Expect:** "hits to judge: none", or only the made-up values of this run-book (`made-up-not-real-000` is one).
**If not:** for any other hit, type `hostrec scrub ~/hf-rec "<the text>"`, which replaces it with a made-up value of the same shape in every file, and run `hostrec scan ~/hf-rec` again until it shows no hit to judge. If you cannot tell what a hit is, scrub it: a scrubbed value costs the lead only that value.
**Settles:** nothing on its own.

### E-03. Pack and hand over (5 min)

Type `hostrec pack ~/hf-rec`. It makes `~/hf-rec.tar.gz`, without `~/hf-private`, and prints its size and a checksum. Hand that file to the lead through the private file share the lead named before sitting 0, and give the checksum in the same message. Never hand it over by a commit, an issue, a pull request, or a message anyone else can read.

**Expect:** the lead confirms it has the file and that the checksum matches.
**If not:** keep the file and the folder, and do not go on to E-04, until the lead confirms.
**Settles:** nothing on its own.

### E-04. Take everything down (15 min)

Only after the lead has confirmed the hand-over.

1. In `hfeval`'s side window, type `hostrec agent remove` if X-49 did not run, then `hostrec teardown`. It removes `~/home-b`, `~/hf-alt-claude`, `~/hf-alt-codex`, `~/.local/share/hf-test`, the deploy key and its public half, `~/hf-private`, `~/hf-rec`, `~/hf-rec.tar.gz`, the scratch folders and marketplaces, and the setup marker, and lists what it removed.
2. Switch to your own account. In your browser, delete `hf-scratch-plum` on the git server, which removes its deploy key with it. Check that no key titled `hf-eval-made-up` is left in your settings there. Delete `/Users/Shared/hf-scratch-address.txt` and `/Users/Shared/hf-scratch-deploy-key.pub` if either is still there.
3. Either delete the evaluation account (System Settings, Users & Groups, delete the user and its home folder), or keep it for DV-02's live gates, which use the same home: the recorder, its plugins and its recording are gone either way.

**Expect:** `teardown` lists each folder it removed; the scratch repository is gone from the server.
**If not:** note what is left in a message to the lead, and remove it by hand.
**Settles:** nothing on its own.

The lead turns the recording into fixtures with every value replaced (the card's check fails if any value from the recording survives), writes each fact's result into [host-facts.md](host-facts.md) with the date and each host's version, keeps the recording outside any checkout and deletes it once the fixtures and results are in, and never commits it.

## For the lead, before sitting 0

- **Name the hand-over channel.** Before sitting 0, tell the owner which private file share E-03 uses. It is never the repository, an issue or a pull request.
- **Check the run-book's untried assumptions** against the host versions current when the sittings run, and correct the run-book first. None of these has been tried by the writer: S0-03's download addresses and archive names (Go's archive, Claude Code's installer, Codex's release asset and the folder it unpacks to); `--json` on `claude plugin marketplace add` and `claude plugin install`; the forms of `codex plugin marketplace add`, `codex plugin add`, `codex plugin list --json`, `codex plugin disable` and `enable`; `codex features list` and `codex login status`; `/hooks` and `/plugins` in Codex and `/plugin` in Claude Code; `claude --settings '{"disableAllHooks": true}'`; `claude --continue --fork-session`; `/loop`; `codex -c model_auto_compact_token_limit=20000`; `codex resume` and `/fork`; each host's plan mode and question tool; how each host takes `CLAUDE_CONFIG_DIR` and `CODEX_HOME` for plugins and sign-in; and the desktop apps' About windows, the Code tab, the worktree choice, scheduled tasks and the voice button. `hostrec register` tries the main fallbacks for the plugin commands itself.
- **Build and check the recorder** (the card's acceptance) before sitting 0, and give the owner the commit to build.

## Facts to steps

Each fact the sheet marks for a sitting, and the steps that settle it. A fact on both hosts has steps in both host sittings. A sitting-0 fact can also take evidence from later steps, which are listed with it.

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
| F-056 Plugins register and are removed with no question | Sitting 0 | S0-06, X-35, E-01 |
| F-057 Each CLI reports its state read-only | Both | C-41, X-38, X-48 |
| F-073 Each host signs in under another HOME and stays signed in | Sitting 0 | S0-14, C-01, X-01 |
| F-074 A sign-in under another HOME leaves the account's own sign-in alone | Sitting 0 | S0-14 |
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
| F-126 Session end on close and after 30 idle minutes | Codex | X-29, X-39, X-41, X-47 |
| F-127 A managed worktree as the working folder | Codex | X-41 |
| F-132 The rollout is written in a known history mode | Codex | X-04 |
| F-133 Rollout rows have a stable position | Codex | X-04 |
| F-134 The opening row names the session and folder, and can be large | Codex | X-05 |
| F-135 PostCompact carries only the trigger | Codex | X-21 |
| F-136 The rollout holds the compaction summary | Codex | X-21 |
| F-137 Desktop threads name a session file eva-md can read | Codex | X-40 |
| F-138 A thread opened and never run ends with no start | Codex | X-42 |
| F-141 A hand-over reaches the prompt hook as a wrapper | Codex | X-44, X-45 |
| F-142 The request is the front model's wording | Codex | X-39, X-44 |
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
| F-175 The server takes a push of machine commits | Sitting 0 | S0-13 |
| F-176 The real wiki's project has no push rule against them | Sitting 0 | S0-08 |
| F-177 The real wiki's git server is reachable without a private network | Sitting 0 | S0-08 |
| F-178 A read with prompts off never waits | Sitting 0 | S0-12 |
| F-179 An anonymous read of a private repository fails fast | Sitting 0 | S0-12 |
| F-180 A background job knows the server's host key | Sitting 0 | S0-11, S0-12, S0-13 |
| F-181 Release downloads reach a background job | Sitting 0 | S0-11 |
| F-182 Write deploy keys exist and can be added | Sitting 0 | S0-10, S0-13, S0-15 |
| F-183 The default branch takes a push with a lease | Sitting 0 | S0-08, S0-16 |
| F-185 The Mac's git does what the store needs | Sitting 0 | S0-09 |
| F-186 `ssh-keygen` makes and signs with a key, with no prompt | Sitting 0 | S0-09 |
| F-187 Signature letters for known and unknown keys | Sitting 0 | S0-09 |
| F-188 Per-call settings never start the person's signing program | Sitting 0 | S0-09 |
| F-189 The git a background job finds is new enough | Sitting 0 | S0-11, C-40, X-30 |
| F-190 No privacy prompt for the store's folder | Sitting 0 | S0-11, C-40, X-30 |
| F-191 The machine key pushes with no agent | Sitting 0 | S0-13 |
| F-200 A launchd user agent installs without an admin and keeps time | Sitting 0 | S0-11, X-49 |
| F-201 Work the scheduler starts reaches the model and the git server | Sitting 0 | S0-11, S0-13 |
| F-202 No device management forbids user launch agents | Sitting 0 | S0-02 |
| F-203 Installing an agent shows a background-item notice | Sitting 0 | S0-11 |
| F-204 A notification from a scheduled job needs no permission | Sitting 0 | S0-11 |

## Steps to facts

| Step | Minutes | Facts |
| --- | --- | --- |
| S0-01 Make the evaluation account | 10 | none |
| S0-02 Device management | 2 | F-202 |
| S0-03 Tools for the account | 20 | none |
| S0-04 Sign both hosts in | 10 | none |
| S0-05 Build the recorder and the scratch folders | 10 | none |
| S0-06 Register the scratch plugins with no terminal attached | 8 | F-056, F-158 |
| S0-07 Approve the Codex hooks | 5 | F-161 |
| S0-08 The scratch repository on the git server | 12 | F-176, F-177, F-183 |
| S0-09 The machine's git checks | 5 | F-185, F-186, F-187, F-188 |
| S0-10 A key for the scratch repository | 8 | F-182 |
| S0-11 The scheduled job | 15 | F-180, F-181, F-189, F-190, F-200, F-201, F-203, F-204 |
| S0-12 The server's host key, and reads with prompts off | 10 | F-178, F-179, F-180 |
| S0-13 The scheduled job's push | 8 | F-175, F-180, F-182, F-191, F-201 |
| S0-14 A sign-in under another HOME | 10 | F-073, F-074 |
| S0-15 The deploy-key commands | 2 | F-182 |
| S0-16 Optional: a forced push with a lease | 3 | F-183 |
| S0-17 End of sitting 0 | 3 | none |
| C-00 Open the windows | 2 | none |
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
| C-35 The environment a hook gets, and another config folder | 10 | F-023, F-028, F-029 |
| C-36 A turn that fails | 3 | F-008, F-076 |
| C-37 A run a program drives | 3 | F-018, F-028, F-032, F-079 |
| C-38 The desktop app's Code tab | 6 | F-011, F-046, F-079, F-105 |
| C-39 Ending a chat | 4 | F-006, F-030, F-102 |
| C-40 A job a hook starts, against the git server | 2 | F-031, F-189, F-190 |
| C-41 Read-only reports | 3 | F-057 |
| C-42 End of sitting 1 | 5 | F-075 |
| X-00 Open the windows | 2 | none |
| X-01 Start | 3 | F-073, F-150, F-157, F-160 |
| X-02 The first prompt | 4 | F-012, F-046, F-047, F-049 |
| X-03 Long hook messages | 10 | F-125 |
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
| X-25 The environment a hook gets, and another Codex home | 10 | F-023, F-028, F-029 |
| X-26 A run a program drives | 3 | F-018, F-028, F-032, F-121 |
| X-27 A missing hook binary | 3 | F-050 |
| X-28 Timed hook runs | 10 | F-162, F-173 |
| X-29 Closing right after a turn | 4 | F-006, F-030, F-126 |
| X-30 A job a hook starts, against the git server | 2 | F-031, F-189, F-190 |
| X-31 A chat left open | 2 | F-044, F-045 |
| X-32 A new binary behind approved hooks | 4 | F-044, F-153 |
| X-33 A change to the plugin with no hooks | 4 | F-151 |
| X-34 A stray file and a link in Codex's copy | 5 | F-154, F-155, F-160 |
| X-35 A second plugin with hooks | 5 | F-056, F-159 |
| X-36 A changed hook declaration | 4 | F-152, F-160 |
| X-37 A skill changed inside the hooks plugin | 8 | F-024, F-043, F-150 |
| X-38 Read-only reports | 3 | F-057 |
| X-39 The desktop app shares the approval, and a chat left idle | 5 | F-126, F-142, F-156 |
| X-40 A short chat in the app | 6 | F-011, F-046, F-137 |
| X-41 A task in a worktree | 4 | F-126, F-127 |
| X-42 Threads opened and never run | 3 | F-138 |
| X-43 A scheduled run in the app | 3 | F-016, F-121 |
| X-44 Voice call one: hand-overs, a correction, a yes, the pause phrase | 12 | F-121, F-141, F-142, F-143, F-144, F-145, F-147, F-148 |
| X-45 Voice call two: a long stretch, ended by quitting the app | 22 | F-141, F-146, F-147, F-148, F-149 |
| X-46 After the calls | 3 | F-147, F-149 |
| X-47 The chat left open | 2 | F-044, F-045, F-126 |
| X-48 Reports with the plugin switched off | 3 | F-057 |
| X-49 The scheduled job's times | 2 | F-200 |
| X-50 End of sitting 2 | 5 | F-120 |
| E-01 Remove the scratch plugins | 5 | F-056 |
| E-02 Check the recording | 10 | none |
| E-03 Pack and hand over | 5 | none |
| E-04 Take everything down | 15 | none |
