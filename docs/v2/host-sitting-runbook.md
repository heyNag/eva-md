# The host sittings: a run-book

**Where this stands (the lead's note, 2026-11-11).** Part 1, the core, takes 180 minutes of steps: three short sittings and a hand-over, about 220 minutes with the margin below, in place of the ten hours the first version asked. It cannot be followed until its recorder is built and checked: Part 1 of the card [HF-00](cards/HF-00.md). Part 2 is not a plan for anyone's day. It is asked for group by group, each when the package that needs it is about to be built, and a group whose facts the second field report or the hosts' published source has settled by then is dropped.

This run-book is for the owner, at a Mac. It settles the host facts of [host-facts.md](host-facts.md) that only the hosts themselves can show. It is the first half of HF-00 in [the v2 plan](plan.md) (Stage 0); the recorder it uses is the second half, described in the card. Each fact the sheet marks for a sitting now also has a mark: **core**, settled in Part 1; **later**, settled by a Part 2 group; **by the field report**, read on the real install by the second report; or **from source**, read in the hosts' published source or documents. The tables at the end map each fact to its steps and each step to its facts.

## Part 1: the core sittings

### Before anything else

**What it is for.** Seven facts that the build cannot wait for, each of which only a recorded session can show:

- **F-016, Claude Code's half**: what marks a prompt the host wrote rather than the person typed. LG-13 (Stage 1) files Claude Code prompts as typed on it, and its fallback would file a host-written prompt with no mark as typed. Part 1 makes the kinds that reach the prompt hook; the kinds found only in the record (a compaction summary, a subagent's report) wait for groups C and E of Part 2, before LG-21.
- **F-033, Claude Code's half**: whether a resumed or forked chat names the chat it came from. Decision 19 is taken on it at the readout, and its fallback would capture the fork of a paused chat.
- **F-121**: which mark in Codex's record means a typed prompt, a program's run, a scheduled run or a hand-over. The second report counts typed Codex prompts with it, and section 8's row on proven typing is read from that count. Its fourth case, a subagent's turn, is read in Codex's published source (0.167.0): a subagent's turn carries its parent turn's mark, and is told apart by its parent turn's id and by its thread's source, which the second report counts by.
- **F-142, F-143 and F-144**: in a voice call, whose words each line is stored as, whether a spoken pause is caught, and whether a thread voice starts begins inside the wrapper (decisions 29 and 30).
- **F-175**: whether your git server takes commits from a machine identity, signed or not. Decision 3 waits on it.

Every other fact the first version settled in a sitting is read by the second report, read in published source, or waits in Part 2 for its package. Three of them are named in the plan's HF-00 row. How often a reply's row is missing when Stop fires (F-019) is sampled by every Stop of Part 1. Whether a message typed while the agent works fires a prompt hook (F-010) is counted on Codex by the second report over a week of real use; Claude Code's half waits for group B, since the real install has no person's Claude Code chat to count. Whether work the scheduler starts reaches the model and the git server (F-200, F-201) waits for group J: its fallback is the hook-started worker that DV-12b builds in Stage 1 anyway, HO-22 only adds to it, and v1's hook-started update already reaches the model on the real install, so decision 4 can be taken at the readout as "yes, confirmed before HO-22 is built". The dialog answers, plan approvals, compaction, timed hook runs and second voice call that the plan's HF-00 row lists are in Part 2 too (groups C, E, H and L).

**What you need.**

- **An administrator's password for the Mac**, once, to add a user account in sitting 0. No later step asks for one.
- **An evaluation home: a separate standard user account on the Mac.** That is the lightest home that is safe. A folder used as `HOME` inside your own account is not: the Codex desktop app, which the voice call needs, reads your own account's `~/.codex`, where your real install's hooks are, and a host may keep its sign-in in the login keychain, which a folder does not separate. A separate account needs no virtual machine and no second Mac.
- **Both hosts' CLIs and the Codex desktop app.** The CLIs are installed in sitting 0, inside the evaluation account only. The desktop apps in `/Applications` are shared by all accounts and need no install; Part 1 uses only Codex's, and S0-03 checks that it is there.
- **A scratch repository on the git server your wiki uses**, which you create in sitting 1 with your own account's browser: private, beside your wiki (under the same user or group), and holding only a README, with a key for it made inside the evaluation account. It receives two pushes of one commit each, to new branches, and you delete it right after them, in the same sitting (G-05). You need neither `gh` nor `glab`, and nothing signs either one in.
- **The recorder** (`hostrec`), built from a checkout of this public repository with Go, which sitting 0 installs inside the evaluation account. It is not built yet: it is Part 1 of the card [HF-00](cards/HF-00.md), and this part cannot be followed until that is merged.
- **A microphone and a quiet room** for the one voice call in Codex's desktop app: no one else speaking, and no television, radio or phone playing. A call transcribes every voice it hears.
- **No days between the sittings.** They go in order, on any days; sittings 1 and 2 and the hand-over can be on the same day.

**What is never touched.** Your real install and its home: your own account's eva-md, its wiki, its inbox, its hooks, `~/.claude` and `~/.codex` in your own account, and the host versions it runs. In your own account you only use System Settings (S0-01); your browser on the git server (G-02 and G-05), where you create the scratch repository, add its key, look at how it shows one commit and delete it; your browser on the private file share the lead names (E-03), where you upload the recording; TextEdit, to save one file in `/Users/Shared` and read another (G-02); and Finder, to delete the file you saved (G-05). Each account deletes only the files it made in `/Users/Shared`: macOS lets no other account delete them. Never use `sudo`, and never install, upgrade or sign in a host from your own account for these sittings. If a host's instructions need either, stop and tell the lead. eva-md itself is not installed in the evaluation home during these sittings; only the scratch plugins are. [The live gates](live-gates-runbook.md) install it there once, between Part 1's hand-over and any Part 2 group, and remove it again. Every prompt, name, word and value below is made up for this run-book and stands for nothing real.

**How long it takes.** Each step states its minutes. The sums below are the least each part takes, against the lead's target; plan for about a fifth more, for first-time screens and waits no step foresaw.

| Part | What | Minutes, at least | Target | Plan for |
| --- | --- | --- | --- | --- |
| Sitting 0 | The evaluation home | 41 | 45 or less | about 50 |
| Sitting 1 | The push for decision 3, then Claude Code | 62 | 90 or less | about 75 |
| Sitting 2 | Codex: the CLI, then the desktop app with one voice call | 62 | 90 or less | about 75 |
| The hand-over | Check the recording, and hand it over | 15 | 20 or less | about 20 |
| Part 1 | | 180 | | about 220 |

The push and the scratch repository's whole life (G-01 to G-05, 22 minutes) need no host. They open sitting 1 because sitting 0 holds them only by passing its target: with them, sitting 0 would take 63 minutes.

**Where a sitting can stop.** Between any two steps but G-04 and G-05, which go together, so that the scratch repository and its key are deleted in the sitting that made them. No step of Part 1 holds a chat open across steps. Before you stop, type `hostrec close` in the side window; the scratch hooks record nothing while the recording is closed, and the next `hostrec mark` opens it again. When you stop for the day, log out of `hfeval`.

### How to follow a step

- You use two Terminal windows in the evaluation account. **The chat window** runs the host. **The side window** runs `hostrec` commands. Some steps of Part 2 open a third window for a chat left open.
- Every step starts with `hostrec mark <step>` in the side window, so the recording knows where each step begins. The first mark of a sitting opens the recording; `hostrec close` at the end of the sitting closes it.
- `hostrec tail`, in the side window, prints one line for each hook event since the step's mark (its time, host and event name, never what it held), and returns.
- Text in a box after **Type** is typed exactly, then Enter. Text after **Say** is spoken. **Type and say only the run-book's text.** If anything else was typed in a chat, or heard during a voice call (another voice, a television, a phone), type `hostrec note "off-script at <step>"`, so the lead drops that step's payloads.
- **Expect** says what should happen. Where the screen cannot show it, it says so, and the recording checks it. **If not** says what to do. When something unexpected happens, write one line with `hostrec note "what you saw"` in the side window, and go on. Do not retry a step more than once, and never change a setting a step does not name.
- **The hosts' own questions.** Answer them so the step can go on. When a host asks whether to trust a new folder, trust it: the scratch folders are made up. When a host asks to run a command or edit a file the prompt asked for, approve it, choosing the answer that stops it asking again for that command when one is offered; refuse anything the prompt did not ask for, and note it. When macOS asks for the microphone at the voice call, allow it. During a call, approve a command the agent asks for by clicking. When closing a window or quitting an app asks you to confirm, confirm. When a desktop app offers to update, choose Later; when a CLI offers to update, or shows an update screen at its start, skip it, so that each host keeps one version through the sittings (`hostrec mark` records each CLI's version, and `summary` says if one changed).
- **Only the run-book's chats.** The desktop apps are signed in to your own subscription, so their lists may show chats, threads or tasks from elsewhere. Open only the chats and threads this run-book creates, and leave every other one closed.
- **Settles** names the facts a step is there for, marked core, and then the facts it also gives evidence for at no added minute.
- **Arms.** Before each step that needs one, `hostrec arm <name>` makes the scratch hooks return a planted word, sleep, or start a background job. Part 1 uses only `start`. Arming changes a file outside the plugins: no arm touches a plugin, on either host, so arming never costs a Codex review. Each `arm` replaces the one before. Every arm except the start-text probes of Part 2 (C-03 and X-03) gives each new, resumed or cleared chat the start word `MARIGOLD-TUESDAY`. `hostrec disarm` puts the hooks back to recording only, with no word.

### Sitting 0: the evaluation home

41 minutes at least. It settles no fact on its own: every later sitting needs it. A break fits after any step.

#### S0-01. Make the evaluation account (10 min)

1. In your own account, open System Settings, Users & Groups, and add a user; macOS asks for an administrator's password. Choose Standard, full name `HF Eval Home`, account name `hfeval`. Give it a password you keep for these sittings.
2. Turn on fast user switching if it is off (System Settings, Control Center), so you can switch between your own account and this one without logging out.
3. Log in to `hfeval`. Skip every offer of the first-login screens (Apple Account, iCloud, Siri, Screen Time): this account needs none of them.

**Expect:** a fresh desktop for `hfeval`.
**If not:** if the Mac does not let you add a user, stop here and tell the lead: a folder used as `HOME` is not safe for Part 1 (see "What you need").
**Settles:** nothing on its own; every later step relies on it.

#### S0-03. Tools for the account (20 min)

Everything here installs into the evaluation account's own home. Never use `sudo`, Homebrew or a global npm install for it: they write to folders your own account owns. In Terminal in `hfeval`:

1. Check what the Mac already has:

   ```
   git --version
   command -v ssh-keygen
   uname -m
   ls -d /Applications/Codex.app
   ```

   `uname -m` prints `arm64` on Apple silicon or `x86_64` on Intel; it decides two names below.
2. Set this account's PATH, and turn off Claude Code's background updates in this account only:

   ```
   mkdir -p ~/hf-tools ~/.local/bin
   echo 'export PATH="$HOME/hf-tools:$HOME/hf-tools/go/bin:$HOME/.local/bin:$PATH"' >> ~/.zshrc
   echo 'export DISABLE_AUTOUPDATER=1' >> ~/.zshrc
   ```

   Then open a new Terminal window, so the lines take effect, and close the old one.
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

**Expect:** `git` prints a version, `ssh-keygen` a path and `ls` the Codex app's path; each version command prints a version; nothing asks for an administrator's password.
**If not:** if `git` asks to install the command line tools, cancel and stop: they are shared by every account on the Mac, so installing them changes what your real install runs; tell the lead. If `ls` finds no `/Applications/Codex.app`, stop and tell the lead: sitting 2's voice call needs the app, and installing it from here, or moving it, would change the copy your real install uses. If anything asks for an administrator's password, cancel and stop, and tell the lead. If a download fails, tell the lead, who checks the hosts' install pages (the addresses are among the run-book's untried assumptions, in "For the lead, before sitting 0").
**Settles:** nothing on its own.

#### S0-05. Build the recorder and the scratch folders (11 min)

The lead gives you, before this sitting, the commit to build: the first 12 characters of its id, the reviewed one, never whatever `main` holds on the day. In the commands below, type them where `<commit>` stands. Type:

```
git clone https://github.com/heyNag/eva-md.git ~/hf-src/eva-md
cd ~/hf-src/eva-md && git checkout --detach <commit>
git log -1 --format=%H
go build -o ~/hf-tools/hostrec ./cmd/hostrec
hostrec setup --rec ~/hf-rec
```

**Expect:** `git log` prints a 40-character id that begins with the 12 characters the lead gave. `hostrec setup` says it found no eva-md install and no earlier session in this account, and makes the recording folder `~/hf-rec`; a private folder `~/hf-private`, which is never handed over; the scratch repositories `~/scratch/alpha-orchard` and `~/scratch/beta-lantern`, a plain folder `~/scratch/outside`, and `~/scratch/long-notes` with a long made-up `AGENTS.md`; and the scratch marketplaces under `~/hf`. All their content is made up: fruit, lanterns and orchards.
**If not:** if `git checkout` fails, or `git log` prints an id that begins otherwise, stop before `go build` and tell the lead. If `setup` refuses because it found an eva-md install or an earlier session, stop: this is not a fresh evaluation account. For any other error, copy it into `hostrec note` if that runs, or into a message to the lead, and stop. If `setup` prints a WARNING that words the recording must keep are also names on this machine, copy the words it lists into a message to the lead and stop: sitting 1 waits for the lead's answer.
**Settles:** nothing on its own.

That ends sitting 0. Log out of `hfeval` (Apple menu, Log Out); nothing in it runs while it is logged out.

### Sitting 1: the push for decision 3, then Claude Code

62 minutes at least: the push and the scratch repository (G-01 to G-05) are 22, and the windows and Claude Code (C-00, and C-0a to C-42) 40. A break fits after G-05 and after C-04, never between G-04 and G-05.

All steps run in `hfeval`, in `~/scratch/alpha-orchard` unless a step says otherwise.

#### C-00. Open the windows (2 min)

Log in to `hfeval` (fast user switching). Open Terminal with two windows, the chat window and the side window, and type `cd ~/scratch/alpha-orchard` in each. Side window: `hostrec mark C-00`, which opens the recording.

**Expect:** both windows in `~/scratch/alpha-orchard`; `mark` says the recording is open.
**If not:** if `hostrec` is not found, open a new window: the PATH line of S0-03 applies to new windows only.
**Settles:** nothing on its own.

#### G-01. A key for the scratch repository (2 min)

Side: `hostrec mark G-01`, then `hostrec key`. It makes a new key, `~/.ssh/hf_scratch_deploy`, in this account only, with no passphrase, and writes its public half to `/Users/Shared/hf-scratch-deploy-key.pub`, which G-03 deletes.

**Expect:** `key` says where it wrote the public half.
**If not:** copy the error into `hostrec note` and tell the lead. G-02 to G-05 then wait; go on with C-0a.
**Settles:** nothing on its own.

#### G-02. The scratch repository and its key, on the git server (10 min)

1. Switch to your own account. In your browser, on the git server your wiki uses, create a new private repository named `hf-scratch-plum`, in the same place as your wiki: under the same user or group, so that any rule that group gives its new repositories applies to it too. Choose to start it with a README, so that it has a default branch; the pushes go to new branches beside it.
2. Copy its SSH address, which ends in `hf-scratch-plum.git`. Open TextEdit, make a new plain-text document (Format, Make Plain Text), paste the address alone, and save it as `hf-scratch-address.txt` in the folder `/Users/Shared` (in the save dialog, Cmd+Shift+G lets you type that folder).
3. Open `/Users/Shared/hf-scratch-deploy-key.pub` with TextEdit and copy its one line. On `hf-scratch-plum`'s settings, add a deploy key: title `hf-eval-made-up`, the copied line as the key, and write access allowed. Close the file, and leave it where it is: the evaluation account made it, so only that account can delete it, and G-03 does.
4. Switch back to `hfeval`.

Never copy a key, an SSH agent or a password manager from your own account into the evaluation account, and never add this key to your own user's keys on the server. The key reaches `hf-scratch-plum` and nothing else. Look at nothing else on the server: your real project's rules are asked in the second report's question 4, not here.

**Expect:** the server lists the deploy key on `hf-scratch-plum`, with write access, and the repository holds the README on its default branch.
**If not:** if you cannot create a repository there, stop this part and tell the lead: a repository elsewhere says nothing about your server. If you can create one only under your own user and not beside your wiki, do that and, back in `hfeval`, type `hostrec note "G-02: not beside the wiki"`. If the server offers no deploy keys, or none with write access, type, back in `hfeval`, a note of the class (for example `hostrec note "G-02: deploy keys switched off"`) and tell the lead: G-04 then cannot be made, and you go straight to G-05 to delete the repository. The public key's file then stays in `/Users/Shared` until Part 2's take-down; it holds nothing secret. Otherwise go on with G-03.
**Settles:** nothing core. Gives evidence for F-182 (write deploy keys exist and can be added).

#### G-03. The repository's address (2 min)

Side: `hostrec mark G-03`, then `hostrec remote set`. It reads the address file, shows the repository's name, refuses any repository not named `hf-scratch-plum`, asks you to confirm with `y`, and stores the address in `~/hf-private`. It then deletes `/Users/Shared/hf-scratch-deploy-key.pub`, which this account wrote, and leaves the address file, which your own account saved and deletes in G-05.

**Expect:** `remote set` shows `hf-scratch-plum`, stores its address and says it deleted the public key's file.
**If not:** if it refuses the name, check that the file holds the scratch repository's address, never your real wiki's. If `remote set` prints a WARNING that words the recording must keep are also names on this machine, copy the words it lists into `hostrec note`, finish G-04 and G-05 as written, then stop before C-0a and tell the lead.
**Settles:** nothing on its own.

#### G-04. The pushes (4 min)

Side: `hostrec mark G-04`, then `hostrec push`. It uses the deploy key given per call and no SSH agent throughout. It fetches the repository's default branch, makes two commits on top of it, both authored and committed as `eva.md (hf-test) <hf-test@machines.eva-md.invalid>`, one unsigned and one signed with a fresh key no account knows, and pushes each by itself to a new branch, never to the default branch: the unsigned one to `hf-check-unsigned`, then the signed one to `hf-check-signed`. The first time it reaches the server it accepts the server's host key into the recorder's own known-hosts file in `~/hf-private`, never into this account's, and shows that key's fingerprint on screen only; if your server's help pages publish their fingerprints, you may compare. Then it reads both branches back and prints each push's outcome.

**Expect:** no dialog and no question; for each push, accepted, or refused because of the committer or because of the signature. Those are the answers decision 3 waits for.
**If not:** note any dialog with its wording, and answer nothing. If a push is refused for another reason (a protected branch, a rule on branch names, the key), `push` prints that class: F-175 is then not settled. Tell the lead, and go on with G-05. If the pushes fail before the server answers (the network, the host key), note the class and tell the lead; `hostrec push` may be tried once more at the start of sitting 2, after `hostrec mark G-04`, and G-05 then waits for that try (see G-05).
**Settles:** F-175 (core): whether the server takes commits from a machine identity, unsigned or signed by a key it does not know, each pushed by itself so that one refusal does not hide the other's outcome. Also gives evidence for F-182 (a write deploy key pushes), F-185 and F-186 (a commit signed per call with a fresh key, with no prompt) and F-191 (a push with no agent).

#### G-05. Delete the scratch repository (4 min)

Right after G-04, in the same sitting. Side: `hostrec mark G-05`.

1. Switch to your own account. In your browser, if the server shows a branch `hf-check-signed` on `hf-scratch-plum`, open its newest commit and see how the server labels it: verified, unverified, or with no label.
2. Delete `hf-scratch-plum` on the git server, which removes its deploy key with it. Check that no key titled `hf-eval-made-up` is left in your settings there.
3. In Finder, choose Go, Go to Folder, type `/Users/Shared`, and move `hf-scratch-address.txt` to the Trash. Your own account saved it, so it can delete it with no password.
4. Switch back to `hfeval`. Side: type `hostrec note "G-05: signed commit shown as <verified, unverified or no label>"`, or `hostrec note "G-05: no signed branch"` if the server showed none.

Only if G-04's pushes failed before the server answered: leave `hf-scratch-plum` for now and do this step right after G-04's one more try at the start of sitting 2, whatever that try gives. Its key then lives until that sitting, and no longer.

**Expect:** `hf-scratch-plum` is gone from the server, so no write key to your server outlives the sitting that made it.
**If not:** if the server will not delete the repository, delete its deploy key by hand, note it, and tell the lead.
**Settles:** F-175 (core): how the server labels a commit signed by a key it does not know, which bears on decision 3's "sign it if the server accepts".

#### C-0a. Sign Claude Code in (4 min)

Side: `hostrec mark C-0a`. Chat: type `claude`. Answer its first screens (a colour theme, trusting the folder), sign in with your own subscription when it asks (it opens the browser of this account), then type `/exit`.

**Expect:** Claude Code says you are signed in.
**If not:** if the sign-in reuses your own account's session without asking, stop: tell the lead before going on, because the evaluation home is then not separate.
**Settles:** nothing on its own.

#### C-0b. Register the Claude Code scratch plugin (4 min)

Side: `hostrec mark C-0b`, then `hostrec register claude-code`. It runs Claude Code's own commands that add the scratch marketplace and plugin, as `hostrec setup` printed them, each with no terminal attached and nothing to read from, so a question would fail rather than wait:

```
claude plugin marketplace add ~/hf/marketplace/claude-code --json </dev/null
claude plugin install hf-recorder@hf-scratch --scope user --json </dev/null
```

A command that fails is tried once more without `--json`. At the end `register` lists Claude Code's plugins.

**Expect:** each command ends with exit status 0 and no question, and the list shows `hf-recorder`.
**If not:** for each command `register` reports as failed:

1. Type it yourself in the side window as `register` printed it last, without `</dev/null`. If it asks a question, answer it so the plugin is added, and type `hostrec note "C-0b: <the command's first four words> asked: <the question, in a few words>"`. That is a result for F-056, not a failure.
2. If it fails again, add the plugin through Claude Code's own plugin screen (`/plugin`), and note that you did.
3. Type `hostrec register --check`. If `hf-recorder` is still not listed, stop and tell the lead: the rest of this sitting needs it.

**Settles:** nothing core. Gives evidence for F-056 (a plugin registers with no question).

#### C-01. Start (3 min)

Side window: `hostrec mark C-01`, then `hostrec arm start`. Chat window: `cd ~/scratch/alpha-orchard && claude`.

**Expect:** Claude Code opens, still signed in, after about one second's wait. A line beginning `hf test` is shown above the prompt.
**If not:** if it asks you to sign in, note it and sign in. Note how the `hf test` line looked, or that there was none: `hostrec note "start line: ..."`.
**Settles:** nothing core. Gives evidence for F-046, F-047 and F-073.

#### C-02. The first prompt (4 min)

Side: `hostrec mark C-02`. Type:

```
This is a test with made-up content. Answer on two lines. 1: What codeword, if any, were you given at the start of this chat? Give the word only, or NONE. 2: Did you see a line that starts with "hf test"? Yes or no.
```

Then side: `hostrec tail`.

**Expect:** 1 is `MARIGOLD-TUESDAY`; 2 is most likely no. `tail` shows a SessionStart line and a UserPromptSubmit line.
**If not:** note the two answers as given. If `tail` shows no SessionStart or no UserPromptSubmit line, stop: the scratch hooks are not recording; tell the lead.
**Settles:** nothing core; it proves the recording works. Gives evidence for F-012, F-046, F-047 and F-049.

#### C-04. A turn with two tools and words between (4 min)

Side: `hostrec mark C-04`. Chat: `/exit`, `claude`, then:

```
Made-up test. First write one sentence saying what you will do. Then read the file orchard/plums.txt and tell me in one sentence what it says. Then run the shell command `ls orchard` and tell me how many files it lists. Write a short sentence before each step.
```

When it asks to run `ls orchard`, approve it with the answer that stops it asking again for that command.

**Expect:** a read, a shell command, short sentences between them, and a final reply.
**If not:** note what the agent did instead.
**Settles:** F-016 (core): the typed turn that the host-written prompts of C-15 to C-27 are read against. Also gives evidence for F-001, F-002, F-004, F-005, F-013, F-019, F-020, F-021, F-022, F-084 and F-085.

#### C-15. An interrupt (2 min)

Side: `hostrec mark C-15`. Type:

```
Made-up test. Run the shell command `sleep 60`, then say done.
```

If it asks to run the command, approve it first. About five seconds after the command has started, press Esc.

**Expect:** the turn stops and the screen says it was interrupted.
**If not:** note it.
**Settles:** F-016 (core): the notice Claude Code writes when a turn is interrupted. Also gives evidence for F-004 and F-008.

#### C-17. A shell line and a plugin command (3 min)

Side: `hostrec mark C-17`. Type `!ls orchard` and Enter. Then type `/hf-recorder:hf-pause-check` and Enter.

**Expect:** the shell line runs without the model; the plugin's command runs and its text reaches the agent.
**If not:** note it.
**Settles:** F-016 (core): how a shell line and a plugin command's text reach the prompt hook. Also gives evidence for F-009.

#### C-19. Prompts nobody typed (5 min)

Side: `hostrec mark C-19`.

1. Chat: type `/loop` without Enter. If the list of commands that appears offers no `/loop`, clear the line, type `hostrec note "no schedule offered"` in the side window, and go on to part 2.
2. Otherwise finish the line as `/loop 1m Made-up test: say tick.` and press Enter. Wait for two ticks, at most 3 minutes. Then press Esc to stop the loop. If a tick still arrives after Esc, type `/exit`, then `claude --continue`, and note it.
3. Then type:

   ```
   Made-up test. Start a background shell command `sleep 20 && echo done-made-up`, and tell me when it finishes.
   ```

**Expect:** the ticks arrive as prompts nobody typed; the finished background command arrives as a notice.
**If not:** if no tick arrived within the 3 minutes, note "loop did not tick", stop the loop as above, and go on with part 3.
**Settles:** F-016 (core): loop prompts and a background task's notice, which nobody typed. Also gives evidence for F-080.

#### C-27. Resume and fork (4 min)

Side: `hostrec mark C-27`. Type `/exit`, then `claude --continue`, then:

```
Made-up test: say hello again.
```

Type `/exit`, then `claude --continue --fork-session`, which forks the newest chat of this folder. If that is refused, type `claude --resume`, pick the newest alpha-orchard chat in the list it shows, and take its fork choice if it has one. Then type:

```
Made-up test: say hello from the fork.
```

**Expect:** the resumed chat continues; the fork starts as a new chat with the old one's history.
**If not:** if no fork is offered, note it.
**Settles:** F-033 (core): whether a resumed chat's and a forked chat's start and first rows name the chat they came from; F-016 (core): the note a resume adds. Also gives evidence for F-006 (a fork's source value) and F-083.

#### C-42. End of sitting 1 (5 min)

Side: `hostrec mark C-42`, `hostrec disarm`, `hostrec summary sitting-1`.

**Expect:** a list of every Claude Code event that fired; each core fact of this sitting (F-016, F-033, and F-175 from G-04 and G-05) with what was recorded for it; and each CLI's version at each mark, with a warning if one changed.
**If not:** `summary` names, for each core fact it lists as missing, the steps that settle it. If the sitting has time, redo those steps now, each starting with its own `hostrec mark`, then type `hostrec summary sitting-1` again. If not, leave them and tell the lead. G-04 is never redone here: one more try is made only as G-04 and G-05 say.

Then type `hostrec close`, and log out of `hfeval` (Apple menu, Log Out).
**Settles:** nothing core. Gives evidence for F-075 (which events fired).

### Sitting 2: Codex

62 minutes at least: the CLI part (X-00 to X-26) is 28 and the desktop app part (X-39 to X-50) 34. A break fits after X-26, before the desktop app part. If G-04's pushes are to be tried once more (G-05 says when), do G-04 and then G-05 right after X-00; they add 8 minutes.

All steps run in `hfeval`, in `~/scratch/alpha-orchard` unless a step says otherwise.

#### X-00. Open the windows (2 min)

Log in to `hfeval` (fast user switching). Open Terminal with two windows, the chat window and the side window, and type `cd ~/scratch/alpha-orchard` in each. Side window: `hostrec mark X-00`, which opens the recording.

**Expect:** both windows in `~/scratch/alpha-orchard`; `mark` says the recording is open.
**If not:** if `hostrec` is not found, open a new window: the PATH line of S0-03 applies to new windows only.
**Settles:** nothing on its own.

#### X-0a. Sign Codex in, in the CLI and the desktop app (6 min)

Side: `hostrec mark X-0a`.

1. Chat: type `codex login`, and sign in with your own subscription.
2. Open the Codex desktop app, sign it in, and quit it.

**Expect:** each says you are signed in.
**If not:** if a sign-in reuses your own account's session without asking, stop: tell the lead before going on, because the evaluation home is then not separate.
**Settles:** nothing on its own.

#### X-0b. Register the Codex scratch plugin (4 min)

Side: `hostrec mark X-0b`, then `hostrec register codex`. It runs Codex's own commands that add the scratch marketplace and its hooks plugin, as `hostrec setup` printed them, each with no terminal attached and nothing to read from:

```
codex plugin marketplace add ~/hf/marketplace/codex --json </dev/null
codex plugin add hf-hooks@hf-scratch --json </dev/null
```

A command that fails is tried once more without `--json`. At the end `register` lists Codex's plugins.

**Expect:** each command ends with exit status 0 and no question, and the list shows `hf-hooks`.
**If not:** as in C-0b, with Codex's plugin screen (`/plugins`) in place of `/plugin`, and `X-0b` in the note. If `hf-hooks` is still not listed after `hostrec register --check`, stop and tell the lead.
**Settles:** nothing core. Gives evidence for F-056.

#### X-0c. Approve the Codex hooks (5 min)

Side: `hostrec mark X-0c`.

1. Chat: type `codex`.
2. In Codex, type `/hooks`. Approve every hook of `hf-hooks`. Leave Codex with `/quit` (or Ctrl+C twice).
3. Side: `hostrec trust`.

**Expect:** `/hooks` lists the scratch hooks as needing review; after approval `hostrec trust` reports them trusted. No other screen asked anything but whether to trust the folder.
**If not:** note what `/hooks` showed and what `hostrec trust` reports. If the hooks are still not trusted, stop and tell the lead: nothing after this step would record.
**Settles:** nothing core. Gives evidence for F-161 (approving in `/hooks` is all it takes).

#### X-02. The first prompt (4 min)

Side: `hostrec mark X-02`, `hostrec arm start`. Chat: `codex`, then:

```
This is a test with made-up content. Answer on two lines. 1: What codeword, if any, were you given at the start of this chat? Give the word only, or NONE. 2: Did you see a line that starts with "hf test"? Yes or no.
```

Then side: `hostrec tail`.

**Expect:** 1 is `MARIGOLD-TUESDAY`; Codex showed the `hf test` text as a warning; 2 is most likely no. `tail` shows a SessionStart line and a UserPromptSubmit line.
**If not:** note how the line showed and the answers. If `tail` shows no SessionStart or no UserPromptSubmit line, stop: the scratch hooks are not recording; tell the lead.
**Settles:** nothing core; it proves the recording works. Gives evidence for F-012, F-046, F-047 and F-049.

#### X-04. A turn with two tools and words between (4 min)

Side: `hostrec mark X-04`. Chat: `/quit`, `codex`, then:

```
Made-up test. First write one sentence saying what you will do. Then read the file orchard/plums.txt and tell me in one sentence what it says. Then run the shell command `ls orchard` and tell me how many files it lists. Write a short sentence before each step.
```

When it asks to run a command, approve it with the answer that stops it asking again for that command.

**Expect:** tool calls, short sentences between them, and a final reply.
**If not:** note what the agent did instead.
**Settles:** F-121 (core): the mark a prompt typed in the CLI leaves in the record. Also gives evidence for F-001, F-002, F-004, F-005, F-013, F-019 to F-022, F-120, F-122, F-132 and F-133.

#### X-26. A run a program drives (3 min)

Side: `hostrec mark X-26`. In the side window, in `~/scratch/alpha-orchard`:

```
HF_PROGRAM_MARK=1 codex exec "Made-up test: reply with the single word PEBBLE."
codex exec --json "Made-up test: reply with the single word PEBBLE."
```

**Expect:** `PEBBLE` twice; the second as JSON events.
**If not:** note it.
**Settles:** F-121 (core): the mark a program's run leaves. Also gives evidence for F-018, F-028 and F-032.

#### X-39. The desktop app's version and voice setting (3 min)

Side: `hostrec mark X-39`. Open the Codex desktop app. If it lists chats, threads or tasks from elsewhere, leave them closed. Open its About window (from the app's menu) and type `hostrec note "codex app version: <the version shown>"`. Open its settings, find the voice setting, and type `hostrec note "voice setting: <what it shows>"`. Close the settings.

**Expect:** no hook review asked in the app.
**If not:** if the app asks for a hook review, approve the scratch hooks there and note that it asked. Note anything else it asked.
**Settles:** F-142 (core): the app's version and voice setting say which voice version X-44's call uses. Also gives evidence for F-156 (one approval holds for the CLI and the app).

#### X-40. A short chat in the app (6 min)

Side: `hostrec mark X-40`, `hostrec arm start`. In the app, before choosing anything, type `hostrec tail` in the side window and note how many SessionStart lines it shows. Choose the project `~/scratch/alpha-orchard`, start a new thread, and type:

```
Made-up test: say hello from the app, and tell me any codeword you were given at the start.
```

Note how the `hf test` text showed in the app.

**Expect:** `MARIGOLD-TUESDAY`, so the hooks fired in the app. Nothing more to see on screen: the recording checks that the hooks name a session file under Codex's folder.
**If not:** note it.
**Settles:** F-121 (core): the mark a prompt typed in the app leaves; the app's hooks must fire for X-43 and X-44. Also gives evidence for F-011, F-046 and F-137.

#### X-43. A scheduled run in the app (4 min)

Side: `hostrec mark X-43`. If the app offers scheduled tasks, make one for `alpha-orchard` that runs once, at the earliest time the app allows, with the prompt `Made-up test: say tick.` Never use a "run now" choice in its place: a run started by hand may carry another mark than a scheduled one.

- If it runs within about a minute: wait for it, at most 3 minutes past its time, then delete it.
- If the earliest time is later, but within about 25 minutes, so before X-50: note it with `hostrec note "X-43: earliest schedule <minutes> min"`, leave it, and go on with X-44. X-50 waits for it.
- If the earliest time is later than that: note the smallest delay the app offers in the same way, make none, and go on.

**Expect:** the run arrives as a turn nobody typed.
**If not:** if the app offers no schedule, note "no schedule offered". If a run has not arrived 3 minutes past its time, note "scheduled run did not arrive" and delete it.
**Settles:** F-121 (core): the mark a scheduled run leaves, the one prompt nobody typed that Codex's published source cannot show. Also gives evidence for Codex's half of F-016.

#### X-44. Voice call one: hand-overs, a correction, a yes, the pause phrase (13 min)

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
   **Pause** until the agent has finished. **Say:** "Back on the record." **Pause** 3 seconds. **Say:** "Ask the agent to list the files in the orchard folder."
   **Pause** until the agent has finished.
7. **Say:** "This last line is made-up and is not for the agent: the lantern is blue."
   **Pause** 5 seconds. End the call with the app's end button. Wait 10 seconds.

**Expect:** a hand-over for each request; the voice model answers line 3 itself.
**If not:** if the voice model hands over a line you meant to keep from the agent, or will not hand over a request, note which line, and go on.
**Settles:** F-143 (core): whose words each spoken line, the spoken yes, the correction and the spoken pause arrive as, and whether a request handed over after "Back on the record." carries the resume phrase inside its wrapper (line 6); F-144 (core): whether a thread voice starts begins inside the wrapper (line 1); F-142 (core): whether each request is the front model's wording; F-121 (core): the mark a hand-over leaves. Also gives evidence for F-141, F-145, F-147, F-148 and F-149.

#### X-46. After the call (3 min)

Side: `hostrec mark X-46`, `hostrec voice-check`. It counts the rows of the call's session file by type, beside the hand-overs the hooks saw, compares the hand-overs' requests with this run-book's script, and prints the names of any version or model fields the wrapper or the session file carries, never a model field's value, and the value of a short field that names the voice feature's version.

**Expect:** a short table of counts and field names; no text from the files but a voice version's value.
**If not:** note it.
**Settles:** F-142 (core): the version fields, and whether each request kept the script's words or a model's. Also gives evidence for F-147 and F-149.

#### X-50. End of sitting 2 (5 min)

If X-43 left a scheduled run waiting, wait for it first, at most 3 minutes past its time, and then delete it in the app; if it has not arrived by then, note "scheduled run did not arrive". Then side: `hostrec mark X-50`, `hostrec disarm`, `hostrec summary sitting-2`.

**Expect:** every Codex event that fired; each core fact of this sitting (F-121, F-142, F-143, F-144) with what was recorded; and each CLI's version at each mark, with a warning if one changed.
**If not:** `summary` names, for each core fact it lists as missing, the steps that settle it. If the sitting has time, redo those steps now, each starting with its own `hostrec mark`, then type `hostrec summary sitting-2` again. If not, leave them and tell the lead.

Then type `hostrec close`. If the hand-over follows now, stay in `hfeval`; otherwise log out of it.
**Settles:** nothing core. Gives evidence for F-120 (which events fired).

### The hand-over

15 minutes at least, on the day of sitting 2 or later. In `hfeval` (log in with fast user switching if you logged out), open Terminal; one window is enough. You bring back one thing: the recording folder, `~/hf-rec` in the evaluation account. Never the hosts' own folders (`~/.claude`, `~/.codex`, the desktop apps' folders), never `~/hf-private`, and never a screenshot of anything but the made-up content above.

The recorder keeps every hook payload whole, since their content is made up. Of the hosts' own files it keeps only row types, field names, sizes, short values such as `true` or `user` from a fixed list of fields and from the few row types the core facts read, ids, and for every other value its length and a hash, which says nothing of the text without the key kept in `~/hf-private`, and that key is never handed over. It never keeps a row's text.

#### E-02. Check the recording (10 min)

Type `hostrec scan ~/hf-rec`. It asks you once, never storing the answers: the email address you sign in with; your full name; your own account's short name (it shows its guess from the Mac's list of users, for you to confirm); your git server's host name, and your user or group name there; and any other word you want to be sure is absent, such as a project's name. It then searches every file of the recording.

It sorts what it finds. Values the hosts and the recorder make (session ids, turn and tool-use ids, commit hashes, `hf_lookup`'s `r:` tokens, the keys `hostrec` made) are only counted, by kind, and need nothing from you. It shows you only the hits to judge, each with its file and line: anything that matches what you typed, an account's name on this Mac, the computer's name, an email address, or a string shaped like a key or a token that it cannot explain.

**Expect:** "hits to judge: none", or only the made-up values of this run-book (`made-up-not-real-000` is one).
**If not:** for any other hit, type `hostrec scrub ~/hf-rec "<the text>"`, which replaces it with a made-up value of the same shape in every file, and run `hostrec scan ~/hf-rec` again until it shows no hit to judge. If you cannot tell what a hit is, scrub it: a scrubbed value costs the lead only that value.
**Settles:** nothing on its own.

#### E-03. Pack and hand over (5 min)

1. In `hfeval`, type `hostrec pack ~/hf-rec --to /Users/Shared`. It writes `/Users/Shared/hf-rec.tar.gz`, without `~/hf-private`, and beside it `/Users/Shared/hf-rec.tar.gz.sha256`, its checksum, both readable by your own account so that it can upload them, and prints the archive's size.
2. Switch to your own account. In your browser, open the private file share the lead named before sitting 0 and upload both files (in the file chooser, Cmd+Shift+G lets you type `/Users/Shared`). Tell the lead they are there, by the way the lead named. Never hand them over by a commit, an issue, a pull request, or a message anyone else can read.
3. Switch back to `hfeval` and type `hostrec pack --clean`, which deletes both files from `/Users/Shared`; this account made them, so only it can. Then log out of `hfeval` (Apple menu, Log Out).

**Expect:** both files uploaded, and `--clean` says it deleted them. The hand-over ends here. The lead answers within a day that it has the archive and that its checksum matches. Until then, leave `~/hf-rec` as it is; nothing in Part 1 deletes it. The evaluation account stays, with the recorder, its plugins and the recording folder, for Part 2; nothing in it runs while it is logged out.
**If not:** if an upload fails, or the lead finds that the checksum does not match, do E-03 again.
**Settles:** nothing on its own.

The lead turns the recording into fixtures with every value replaced (the card's check fails if any value from the recording survives), writes each core fact's result into [host-facts.md](host-facts.md) with the date and each host's version, keeps the recording outside any checkout and deletes it once the fixtures and results are in, and never commits it.

### For the lead, before sitting 0

- **Name the hand-over channel.** Before sitting 0, tell the owner which private file share E-03 uploads to, from the owner's own account's browser, and how the owner tells you the files are there. It is never the repository, an issue or a pull request. Answer within a day of the upload.
- **Check Part 1's untried assumptions** against the host versions current when the sittings run, and correct the run-book first. None has been tried by the writer: S0-03's download addresses and archive names (Go's archive, Claude Code's installer, Codex's release asset and the folder it unpacks to); `--json` on `claude plugin marketplace add` and `claude plugin install`; the forms of `codex plugin marketplace add` and `codex plugin add`; `/hooks` and `/plugins` in Codex and `/plugin` in Claude Code; `DISABLE_AUTOUPDATER` and the Codex CLI's update screen; `/loop` and Esc to stop it; `claude --continue --fork-session`; the Codex desktop app's name in `/Applications`, its About window, voice setting and voice button; its scheduled tasks, the earliest run they allow, and X-43's fallback when that is later than a minute; and, on the git server, a new repository with a README, and a deploy key that may create a new branch. `hostrec register` tries the main fallbacks for the plugin commands itself.
- **Build and check the recorder** (Part 1 of the card's acceptance) before sitting 0, approve the enum-field allowlist of the card's item 2, and give the owner the first 12 characters of the reviewed commit, which S0-05 checks out. Give a new commit for each Part 2 group, for U-01.
- **The second report reads what Part 1 no longer does.** The facts marked "by the field report" stay open until the second report's prompt reads them. Add those readings to the prompt, as shapes and counts only, before the owner runs it.

## Part 2: later, when a package needs it

Nothing here is asked for now. Each group belongs to the package or decision that will need it; the lead asks for a group when that package is about to be built, and says which of its steps to run. A step whose facts the sheet marks "by the field report" or "from source" is run only if the second report or the source leaves the fact open. The groups can come in any order, weeks apart.

### How a group runs

- **What it needs from Part 1:** the evaluation account with Go, both CLIs, the recorder's checkout and both hosts signed in (S0-01 to S0-05, C-0a, C-0b, X-0a to X-0c). Each group names anything more it needs.
- **Its frame:** C-00 or X-00 (whichever host the group starts with), U-01, the group's steps in the order listed, U-02, then E-02 and E-03 of Part 1: 25 minutes beside the group's own steps. A Part 1 step run again starts with its own mark as before; the recording tells the runs apart by time.
- **Open chats.** When a step's first word in the chat window is `claude`, `codex` or `cd`, leave any chat still open there first (`/exit` in Claude Code, `/quit` in Codex). When a step begins by typing into a chat and none is open, start one first: `cd ~/scratch/alpha-orchard && claude`, or `&& codex`. When a step begins with `/exit` or `/quit` and no chat is open, leave that word out.
- **Where a group can stop.** Between any two steps, except inside the spans that hold a chat open: C-31 to C-31b, X-31 to X-32, and X-39b to X-47. Close as in Part 1.
- **Untried assumptions.** Before asking for a group, the lead checks its commands against the host versions of the day: among them `codex login status`, `codex features list`, `codex plugin list --json`, `codex plugin disable` and `enable`, `claude --settings '{"disableAllHooks": true}'`, `codex -c model_auto_compact_token_limit=20000`, `codex resume` and `/fork`, each host's plan mode and question tool, how each host takes `CLAUDE_CONFIG_DIR` and `CODEX_HOME` for plugins and sign-in, and the desktop apps' Code tab and worktree choice.

#### U-01. Bring the recorder up to date (5 min)

After C-00 or X-00, in the side window, with the first 12 characters of the commit the lead gives for this group where `<commit>` stands:

```
cd ~/hf-src/eva-md && git fetch origin && git checkout --detach <commit>
git log -1 --format=%H
go build -o ~/hf-tools/hostrec ./cmd/hostrec
hostrec setup --rec ~/hf-rec --more
hostrec register --check
```

`setup --more` adds to the evaluation home what Part 2's recorder needs (the scratch MCP server's entry in `hf-recorder`, the Codex plugin `hf-tools`, and the files Part 2's arms use), and prints the hosts' update commands for what it changed; type each, then `hostrec register`, which adds only what a host does not list yet.

**Expect:** `git log` prints a 40-character id that begins with the lead's 12 characters. The first time, `setup --more` lists what it added; later, nothing. `--check` lists `hf-recorder` for Claude Code, and `hf-hooks` and `hf-tools` for Codex.
**If not:** if `git log` prints an id that does not begin with the lead's 12 characters, or `go build` fails, stop and tell the lead. For a plugin `register` cannot add, follow C-0b's "If not". If Codex asks for a hook review at its next start, approve the scratch hooks in `/hooks` and note it.
**Settles:** nothing core. Gives evidence for F-158 (a second plugin from the same marketplace).

#### U-02. End of a group (3 min)

Side: `hostrec mark U-02`, `hostrec disarm`, `hostrec summary <the group's letter>`.

**Expect:** every event that fired, and each of the group's facts with what was recorded for it.
**If not:** `summary` names, for each fact it lists as missing, the steps that settle it. If there is time, redo those steps now, each starting with its own `hostrec mark`, then type the `summary` again. If not, leave them and tell the lead.

Then type `hostrec close`, and go on to E-02 and E-03 of Part 1.
**Settles:** nothing on its own.

### Group A: Claude Code's programs and the Code tab

**For:** LG-13's Claude Code values, if the second report leaves them open, and HO-10's version ranges. **Asked:** When HO-10b's version ranges are fixed, or sooner if the second report finds no Claude Code `entrypoint` values to read (F-079). **Needs, beyond Part 1:** The Claude desktop app, signed in by C-0c. **Minutes:** 12 of its own steps, 37 with its frame.

**Facts it gives evidence for:** F-011, F-018, F-028, F-032, F-046, F-079, F-105.

#### C-0c. Sign the Claude desktop app in (3 min)

Side: `hostrec mark C-0c`. Open the Claude desktop app, sign it in with your own subscription, and quit it. Skip it if the app is already signed in in `hfeval`.

**Expect:** the app says you are signed in.
**If not:** if the sign-in reuses your own account's session without asking, stop: tell the lead before going on, because the evaluation home is then not separate.
**Settles:** nothing on its own.

#### C-37. A run a program drives (3 min)

Side: `hostrec mark C-37`. In the side window, in `~/scratch/alpha-orchard`, type:

```
HF_PROGRAM_MARK=1 claude -p "Made-up test: reply with the single word PEBBLE."
claude -p --output-format stream-json --verbose "Made-up test: reply with the single word PEBBLE."
```

**Expect:** `PEBBLE` twice; the second as a stream of JSON lines.
**If not:** note it.
**Settles:** F-018, F-028, F-032, F-079.

#### C-38. The desktop app's Code tab (6 min)

Side: `hostrec mark C-38`, `hostrec arm start`.

Open the Claude desktop app. If it lists chats from elsewhere, leave them closed. Open its About window (from the app's menu) and type `hostrec note "claude app version: <the version shown>"`. Go to its Code tab. Before you choose anything, type `hostrec tail` in the side window and note how many SessionStart lines it shows. Choose the folder `~/scratch/alpha-orchard`, and type:

```
Made-up test: say hello from the desktop app, and tell me any codeword you were given at the start.
```

Note how the `hf test` start line showed there, if at all. Quit the app.

**Expect:** the codeword is `MARIGOLD-TUESDAY`, so the hooks fired in the app.
**If not:** note it.
**Settles:** F-011, F-046, F-079, F-105.

### Group B: The record reader

**For:** LG-21. **Asked:** When LG-21, the record reader, is about to be built. **Needs, beyond Part 1:** A network that can be switched off for a minute (C-36), and the Codex desktop app (X-42). **Minutes:** 30 of its own steps, 55 with its frame.

**Facts it gives evidence for:** F-004, F-008, F-010, F-016, F-034, F-035, F-076, F-120, F-134, F-135, F-136, F-138.

#### C-14. A message typed while the agent works (4 min)

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

#### C-36. A turn that fails (3 min)

Side: `hostrec mark C-36`. Chat: `claude`. Disconnect the network (Wi-Fi off in Control Center, or the cable out). Type `Made-up test: say hello.` Wait for the error. Connect the network again.

**Expect:** an error, and the turn ends.
**If not:** note the message.
**Settles:** F-008, F-076.

#### C-22. A web fetch and a network command (3 min)

Side: `hostrec mark C-22`. Type:

```
Made-up test. Fetch the page https://example.com and tell me its title. Then run the shell command `curl -sI https://example.com` and tell me the status line.
```

**Expect:** a web fetch and a shell command; both say Example Domain or 200.
**If not:** note it.
**Settles:** F-035.

#### X-13. A message typed while the agent works (4 min)

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

#### X-14. An interrupt (2 min)

Side: `hostrec mark X-14`. Type:

```
Made-up test. Run the shell command `sleep 60`, then say done.
```

If it asks to run the command, approve it first. About five seconds after the command has started, press Esc.

**Expect:** the turn stops.
**If not:** note it.
**Settles:** F-004, F-008, F-016, F-120.

#### X-19. A web search and a network command (3 min)

Side: `hostrec mark X-19`. Type:

```
Made-up test. Search the web for the page example.com and tell me its title. Then run the shell command `curl -sI https://example.com` and tell me the status line.
```

**Expect:** a search if this version offers one, and a shell command.
**If not:** note it.
**Settles:** F-035.

#### X-05. Long instructions (3 min)

Side: `hostrec mark X-05`. Chat: `/quit`, `cd ~/scratch/long-notes && codex`, then `Made-up test: say hello.`, then `/quit`.

**Expect:** a reply; the rollout's opening row is large because the folder's made-up instructions are long.
**If not:** note it.
**Settles:** F-134.

#### X-21. A compaction by command (5 min)

Side: `hostrec mark X-21`, `hostrec arm compact`. Chat: `/quit`, `cd ~/scratch/alpha-orchard && codex resume`, pick the newest alpha-orchard chat in the list it shows, then `/compact`. Then:

```
Made-up test. What word in capitals, if any, were you given just after the compaction? Word only, or NONE.
```

**Expect:** `AMBER-GATE`.
**If not:** note it.
**Settles:** F-016, F-034, F-135, F-136.

#### X-42. Threads opened and never run (3 min)

Side: `hostrec mark X-42`. Open the Codex desktop app. In its thread list, open the thread of X-40 and look at it, without typing. Then start a new thread for `alpha-orchard` and leave it without sending anything. Then open the thread of X-40 again. Do not open the thread of X-41, or any thread this run-book did not create.

**Expect:** perhaps end hooks with no start; this step records which.
**If not:** nothing to do.
**Settles:** F-138.

### Group C: Decisions and delegated work

**For:** LG-23. **Asked:** When LG-23 is about to be built. **Needs, beyond Part 1:** Nothing more. **Minutes:** 43 of its own steps, 68 with its frame.

**Facts it gives evidence for:** F-002, F-003, F-007, F-016, F-025, F-026, F-027, F-077, F-078, F-091, F-121.

#### C-05. A command that fails (2 min)

Side: `hostrec mark C-05`. Type:

```
Made-up test. Run the shell command `cat orchard/no-such-fruit.txt` and tell me the exact error it printed and its exit status.
```

**Expect:** the command fails with "No such file or directory" and exit status 1.
**If not:** note what happened.
**Settles:** F-002, F-007.

#### C-09. A multiple-choice question (5 min)

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

#### C-10. A plan approved and a plan rejected (5 min)

Side: `hostrec mark C-10`. Press Shift+Tab until the footer shows plan mode. Type:

```
Made-up test. Plan how you would create the file orchard/apple.txt containing the single word `crisp`. Do not do it yet.
```

When the plan is shown with its choices, pick the one that approves the plan and still asks before each edit (worded like "Yes, and manually approve edits"), then approve the write when it asks. After the write, enter plan mode again and type:

```
Made-up test. Plan how you would delete orchard/apple.txt.
```

This time pick the choice that keeps planning (worded like "No, keep planning"), and type `Not now, this is a test.`

**Expect:** the first plan approved and carried out; the second rejected, with nothing deleted.
**If not:** note what the plan screen offered.
**Settles:** F-003.

#### C-11. A task list (3 min)

Side: `hostrec mark C-11`. Press Shift+Tab until plan mode is off. Type:

```
Made-up test. Use your task list tool: add three tasks, `water the plums`, `count the pears` and `label the quinces`, then mark `water the plums` as done. Do nothing else.
```

**Expect:** the task list shows three tasks, one done.
**If not:** note it.
**Settles:** F-078.

#### C-12. A subagent (6 min)

Side: `hostrec mark C-12`, `hostrec arm subagent-word`. Type:

```
Made-up test. First tell me the codeword you were given at the start of this chat, and the project word from your instruction file, or NONE for each. Then delegate this to a general-purpose subagent: it should first tell you any word in capitals it was given when it started, then the codeword given at the start of this chat if it knows it, then the project word from its instruction file if it has one, then read orchard/pears.txt, add a line `checked: yes` to it, and report back in one sentence. Include the subagent's three answers in your answer.
```

**Expect:** you get `MARIGOLD-TUESDAY` and `WILLOW-CANDLE` first; a subagent runs; it reports `SILVER-OTTER`, most likely does not know `MARIGOLD-TUESDAY`, and most likely knows `WILLOW-CANDLE`; the file gains the line.
**If not:** note the words it gave.
**Settles:** F-003, F-016 (the subagent's report, which only the record holds), F-025, F-026, F-027, F-091.

#### C-13. A custom agent (3 min)

Side: `hostrec mark C-13`. Type:

```
Made-up test. Delegate to the hf-gardener agent: read orchard/pears.txt and report its first line.
```

**Expect:** the plugin's own agent runs and reports the line.
**If not:** note it.
**Settles:** F-026.

#### C-24. A model change (2 min)

Side: `hostrec mark C-24`. Type `/model`, pick a different model from the list, then type `Made-up test: say hello.`

**Expect:** a reply from the newly picked model.
**If not:** note it.
**Settles:** F-077.

#### X-06. A command that fails (2 min)

Side: `hostrec mark X-06`. Chat: `cd ~/scratch/alpha-orchard && codex`, then:

```
Made-up test. Run the shell command `cat orchard/no-such-fruit.txt` and tell me the exact error it printed and its exit status.
```

**Expect:** the command fails with exit status 1.
**If not:** note it.
**Settles:** F-007.

#### X-10. A multiple-choice question (4 min)

Side: `hostrec mark X-10`, `hostrec disarm`. Type:

```
Made-up test. If you have a tool for asking me a multiple-choice question, use it: which fruit file should we create next, with the options pear, quince and fig. Then create orchard/<my choice>-two.txt containing the word `ripe`.
```

If options are shown, pick **fig**. Then ask for a second question as in C-09 and answer it in your own words, `parsnip`.

**Expect:** a question with options, if this version has such a tool.
**If not:** note "no question tool" and answer in text.
**Settles:** F-003.

#### X-11. A plan approved and a plan rejected (5 min)

Side: `hostrec mark X-11`. Switch Codex to plan mode in the way your version offers (Shift+Tab, or `/plan`). Type:

```
Made-up test. Plan how you would create the file orchard/date.txt containing the single word `sweet`. Do not do it yet.
```

When Codex shows the plan and asks whether to carry it out, pick the choice that carries it out; if no choice shows, type `Yes, carry out the plan.` Then plan mode again:

```
Made-up test. Plan how you would delete orchard/date.txt.
```

This time pick the choice that does not carry it out, or, if no choice shows, type `No, do not carry it out.` Then type `Not now, this is a test.`

**Expect:** one plan carried out, one rejected.
**If not:** if there is no plan mode, note it.
**Settles:** F-003.

#### X-12. A subagent (6 min)

Side: `hostrec mark X-12`, `hostrec arm subagent-word`. Type:

```
Made-up test. If you can start a subagent, delegate this to one: it should first tell you any word in capitals it was given when it started, then read orchard/pears.txt, add a line `checked: again` to it, and report back in one sentence. Include the subagent's word in your answer.
```

**Expect:** a subagent runs and reports `SILVER-OTTER`.
**If not:** if this version has no subagents, note it.
**Settles:** F-003, F-025, F-026, F-027, F-121.

### Group D: The pause, pastes and commands

**For:** LG-24, and the explicit command of decision 19. **Asked:** When LG-24 is about to be built, and before decision 19's explicit command ships. **Needs, beyond Part 1:** Nothing more. **Minutes:** 17 of its own steps, 42 with its frame.

**Facts it gives evidence for:** F-006, F-009, F-016, F-017, F-033.

#### C-16. Pasted text (3 min)

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

#### C-18. The pause phrase, typed (2 min)

Side: `hostrec mark C-18`. Type `off the record`, then `Made-up test: what is two plus two?`, then `back on the record`.

**Expect:** three ordinary turns. eva-md is not installed here, so nothing pauses; the recording shows how the phrase reaches the hook.
**If not:** note it.
**Settles:** F-009.

#### X-15. Pasted text (3 min)

Side: `hostrec mark X-15`. Paste the same four made-up lines as in C-16, once inside a typed prompt (`Made-up test. Here is a pasted block:` … ` Which line mentions the moon?`) and once alone.

**Expect:** two answers.
**If not:** note how the paste showed.
**Settles:** F-017.

#### X-16. A shell line, a skill and the pause phrase, typed (3 min)

Side: `hostrec mark X-16`. Type `!ls orchard`. Then type `$hf-pause-check` and Enter. Then `off the record`, `Made-up test: what is two plus two?`, and `back on the record`.

**Expect:** the shell line runs; the skill runs; three ordinary turns.
**If not:** note it.
**Settles:** F-009, F-016.

#### X-24. Resume and fork (6 min)

Side: `hostrec mark X-24`. `/quit`, wait two minutes, `cd ~/scratch/alpha-orchard && codex resume`, pick the newest alpha-orchard chat in the list, then `Made-up test: say hello again.` Then fork the chat the way your version offers (`/fork`, or editing an earlier message), and type `Made-up test: say hello from the fork.`

**Expect:** the resumed chat continues; the fork starts as a new chat.
**If not:** note it.
**Settles:** F-006, F-033.

### Group E: Delivery at a start and after a compaction

**For:** DL-09, DL-10, DL-11 and DL-16. **Asked:** When DL-09 and DL-10 are about to be built; X-22 can wait for DL-16 (Stage 3). **Needs, beyond Part 1:** Nothing more. **Minutes:** 34 of its own steps, 59 with its frame.

**Facts it gives evidence for:** F-006, F-014, F-016, F-034, F-088, F-092, F-098, F-099, F-100, F-101, F-125, F-174.

#### C-03. Long start texts (5 min)

Side: `hostrec mark C-03`, `hostrec arm start-9900`. In the chat window type `/exit`, then `claude`, then:

```
Made-up test. At the very end of the text you were given at the start of this chat there is a word in capitals. What is it? Word only, or NONE.
```

Then side: `hostrec arm start-10100`, chat: `/exit`, `claude`, and the same prompt again.

**Expect:** after 9,900 characters the agent gives `LAST-WORD-NINE`; after 10,100 characters it may say NONE or mention a file.
**If not:** note both answers.
**Settles:** F-088.

#### C-25. A compaction (5 min)

Side: `hostrec mark C-25`, `hostrec arm compact`. Chat: `/exit`, `cd ~/scratch/alpha-orchard && claude --continue`, then type `/compact`. When it is done, type:

```
Made-up test. Two questions. 1: What word in capitals, if any, were you given just after the compaction? 2: Does your summary of this chat mention the path orchard/quince.txt? Yes or no.
```

**Expect:** 1 is `AMBER-GATE`; 2 is yes, because the PreCompact hook asked for it.
**If not:** note both answers.
**Settles:** F-016, F-034, F-098, F-099, F-100, F-101.

#### C-26. A clear (2 min)

Side: `hostrec mark C-26`. Type `/clear`, then `Made-up test: say hello.`

**Expect:** a fresh context and a reply.
**If not:** note it.
**Settles:** F-006, F-014.

#### C-28. A hook past its time limit (2 min)

Side: `hostrec mark C-28`, `hostrec arm timeout`. Type `Made-up test: say hello.`

**Expect:** the prompt hook runs past its two-second limit; the host shows a notice and goes on.
**If not:** note exactly what was shown.
**Settles:** F-092.

#### X-03. Long hook messages (10 min)

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

#### X-22. A compaction in the middle of a turn (8 min)

Side: `hostrec mark X-22`, `hostrec arm compact`. Chat: `/quit`, then `codex -c model_auto_compact_token_limit=20000` in `~/scratch/long-notes`, then:

```
Made-up test. Read AGENTS.md in four parts with the shell, one part per command, and after each part write one sentence about it. At the end, tell me two words: the word in capitals you were given at the start of this chat, and any word in capitals given to you after a compaction. NONE for any you do not have.
```

**Expect:** an automatic compaction during the turn; the agent knows `AMBER-GATE` and most likely not `MARIGOLD-TUESDAY`.
**If not:** if no compaction happens, note it.
**Settles:** F-034, F-174.

#### X-23. A clear (2 min)

Side: `hostrec mark X-23`. Type `/clear` (or `/new` if this version has no `/clear`), then `Made-up test: say hello.`

**Expect:** a fresh context and a reply.
**If not:** note it.
**Settles:** F-006, F-014.

### Group F: eva-md's tools in a chat

**For:** DL-12. **Asked:** When DL-12 is about to be built. **Needs, beyond Part 1:** Part 2's recorder, for the scratch MCP server (U-01 adds it). **Minutes:** 28 of its own steps, 53 with its frame.

**Facts it gives evidence for:** F-035, F-036, F-037, F-038, F-039, F-040, F-041, F-042, F-093.

#### C-02b. Tools that load at start (2 min)

Side: `hostrec mark C-02b`, `hostrec arm start`. Chat: `/exit`, `claude`, then:

```
Made-up test. List the names of the tools whose names start with hf_ that you can call right now, without searching for tools.
```

**Expect:** `hf_lookup` (the tool marked to load at start), and perhaps not the others.
**If not:** note the answer as given.
**Settles:** F-093.

#### C-20. The scratch MCP server (8 min)

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

#### C-21. The same lookup by MCP and by command (5 min)

Side: `hostrec mark C-21`. Type:

```
Made-up test. Do this three times: call hf_lookup for `pear`, then run the shell command `hostrec lookup pear`. Report the six answers in one list.
```

**Expect:** six answers, the same text by both routes.
**If not:** note it.
**Settles:** F-042.

#### X-17. The scratch MCP server (8 min)

Side: `hostrec mark X-17`. Type the five prompts of C-20, one at a time. Note whether a permission question appears and whether "allow always" is offered.

**Expect:** as in C-20; Codex may show more or less of the instructions.
**If not:** note each answer.
**Settles:** F-035, F-036, F-037, F-038, F-039, F-040, F-041.

#### X-18. The same lookup by MCP and by command (5 min)

Side: `hostrec mark X-18`. Type the prompt of C-21.

**Expect:** six answers.
**If not:** note it.
**Settles:** F-042.

### Group G: Episodes, ends and anchors

**For:** DV-20, DV-23 and DV-24, with LG-22's worktree. **Asked:** When DV-24's end and idle rules, or LG-22, are about to be built. **Needs, beyond Part 1:** At least 35 minutes of wall clock between X-39b and X-47, which the group's other steps fill, and the Codex desktop app. **Minutes:** 22 of its own steps, 47 with its frame.

**Facts it gives evidence for:** F-006, F-015, F-030, F-044, F-045, F-102, F-126, F-127.

#### X-39b. A Codex chat left idle (2 min, then open until X-47)

Side: `hostrec mark X-39b`. Open a third Terminal window, type `cd ~/scratch/beta-lantern && codex`, and leave it open, typing nothing, until X-47, at least 35 minutes later. The group's other steps go on meanwhile.

**Expect:** Codex opens in the third window. Nothing more to see on screen: X-47 reads whether an end hook fired after about 30 idle minutes.
**If not:** note it.
**Settles:** F-126.

#### X-41. A task in a worktree (4 min)

Side: `hostrec mark X-41`, `hostrec disarm`. Open the Codex desktop app, and in it start a new thread for `alpha-orchard` and choose to run it in a worktree, if offered. Type:

```
Made-up test: run `git status` and `pwd`, and tell me both results.
```

Then leave this thread alone: do not open it again until the app is quit in X-47. It is the app's idle thread for F-126.

**Expect:** the task runs in a separate worktree folder.
**If not:** if no worktree is offered, note it, and run it in the folder itself.
**Settles:** F-126, F-127.

#### C-23. Another repository (3 min)

Side: `hostrec mark C-23`. Chat: `/exit`, `cd ~/scratch/outside && claude`, then:

```
Made-up test. Read the file ~/scratch/beta-lantern/lantern/wicks.txt, then search the folder ~/scratch/beta-lantern for the word `amber`, and tell me what you found.
```

**Expect:** a read and a search in the other repository.
**If not:** note it.
**Settles:** F-015.

#### C-39. Ending a chat (4 min)

Side: `hostrec mark C-39`, `hostrec arm end-slow`. Chat: `claude`, type `Made-up test: say hello.`, then `/exit`. Then `claude` again, type `Made-up test: say hello.`, and close the whole Terminal window with Cmd+W. Wait 15 seconds, open a new window and type `hostrec children`.

**Expect:** `children` lists a file for each detached child. Nothing more to see on screen: the recording checks that the end hook's lines stop after about 1.5 seconds, and that each child's file appeared about 10 seconds after its hook.
**If not:** note it.
**Settles:** F-006, F-030, F-102.

#### X-20. Another repository (3 min)

Side: `hostrec mark X-20`. Chat: `/quit`, `cd ~/scratch/outside && codex`, then the prompt of C-23.

**Expect:** a read and a search in `beta-lantern`.
**If not:** note it.
**Settles:** F-015.

#### X-29. Closing right after a turn (4 min)

Side: `hostrec mark X-29`, `hostrec arm stop-child`. Type `Made-up test: say hello.` As soon as the reply appears, press Ctrl+C twice to quit Codex. Wait 15 seconds, then side: `hostrec children`.

**Expect:** `children` lists the detached child's file. Nothing more to see on screen: the recording checks that the end hook fired on the close.
**If not:** note it.
**Settles:** F-006, F-030, F-126.

#### X-47. The chat left idle (2 min)

Side: `hostrec mark X-47`. At least 35 minutes after X-39b, look at the third Terminal window, then type `/quit` there and close the window. Quit the Codex app with Cmd+Q. Side: `hostrec summary idle`.

**Expect:** `summary idle` says, for the CLI chat of X-39b and for the app's thread of X-41, whether an end hook fired after about 30 idle minutes, at the quit, or not at all, and that the server stamped every minute from one process. If X-41's thread had less than 35 minutes before the app was quit, it says so, and the app's half stays unsettled.
**If not:** note it.
**Settles:** F-044, F-045, F-126.

### Group H: The frozen Codex plugin

**For:** HO-20. **Asked:** Before HO-20 freezes Codex's generation-1 plugin. X-28 only if the second report's row (f) puts one hook start on Codex within about 20 ms of the 100 ms bar (F-173). **Needs, beyond Part 1:** At least one night since X-0c, for X-01's approval days old; Part 2's recorder, for `hf-hooks-two`, `plugin-edit`, `swap-binary` and `timing`. X-36 and X-37 ask for a hook review, which you approve. **Minutes:** 52 of its own steps, 77 with its frame.

**Facts it gives evidence for:** F-021, F-024, F-043, F-044, F-045, F-056, F-073, F-123, F-124, F-150, F-151, F-152, F-153, F-154, F-155, F-157, F-159, F-160, F-162, F-173.

#### X-01. Hooks approved days ago (3 min)

Side: `hostrec mark X-01`, `codex login status`, `hostrec trust`.

**Expect:** `codex login status` says Codex is still signed in, and `trust` reports the scratch hooks still trusted, days after their approval, loaded from the plugin's `.codex-plugin/plugin.json`.
**If not:** if they are not trusted, note it, approve them in `/hooks`, and go on.
**Settles:** F-073, F-150, F-157, F-160.

#### X-07. An edit on another branch, with a word from the pre-tool hook (4 min)

Side: `hostrec mark X-07`, `git -C ~/scratch/alpha-orchard switch -c hf-branch-three`, `hostrec arm pretool-word`. Type:

```
Made-up test. In orchard/plums.txt, change the line that starts with `count:` so that it reads `count: 14`. Then run the shell command `wc -l orchard/plums.txt`.
```

When done:

```
What checkword, if any, were you given while you edited or ran commands? Word only, or NONE.
```

**Expect:** an edit through `apply_patch` and a command; the answer `HARBOR-NINE`.
**If not:** note it.
**Settles:** F-021, F-123.

#### X-08. A word from an async hook, in the same turn (3 min)

Side: `hostrec mark X-08`, `hostrec arm async-word`. Type:

```
Made-up test. Print the file orchard/pears.txt with the shell command `cat`. Then, in the same reply, tell me any marker word you were given after printing it, or NONE.
```

**Expect:** `COPPER-FINCH` in the same reply.
**If not:** note the answer.
**Settles:** F-124.

#### X-28. Timed hook runs (10 min)

Side: `hostrec mark X-28`, `hostrec arm timed`. Chat: `codex`, then:

```
Made-up test. Run the shell command `true` 100 times, as 100 separate tool calls one after another, not in a loop or a script. Say done at the end.
```

Approve commands for the session if asked. Side, at the end: `hostrec timing codex`.

**Expect:** about 200 hook starts (one before and one after each call); `timing` prints the median and the 95th percentile, apart for the plain starts and for those that also hashed a made-up folder of 2 KB, and the shell the hooks ran under.
**If not:** if the agent batches the commands, let it finish, note it, and type the prompt once more.
**Settles:** F-162, F-173.

#### X-31. A chat left open (2 min, then open until the end of X-32)

Side: `hostrec mark X-31`, `hostrec disarm`. Open a third Terminal window, `cd ~/scratch/beta-lantern && codex`, and leave it open, typing nothing. X-32 closes it.

**Expect:** Codex opens in the third window. Nothing more to see on screen: the recording checks that the scratch server stamps every minute, and that SessionStart followed its start within 90 seconds.
**If not:** note it.
**Settles:** F-044, F-045.

#### X-32. A new binary behind approved hooks (4 min)

Side: `hostrec mark X-32`, `hostrec trust`, `hostrec swap-binary`, `hostrec trust`. Chat: `codex`, `Made-up test: say hello.`, `/quit`. Then, in the third window, type `/quit` and close that window.

**Expect:** `trust` reports the hooks trusted before and after the swap. Nothing more to see on screen: the recording checks that the hooks fired with the new build and that the open chat of X-31 kept its old server.
**If not:** note it.
**Settles:** F-044, F-153.

#### X-33. A change to the plugin with no hooks (4 min)

Side: `hostrec mark X-33`, `hostrec plugin-edit codex tools-skill`, then the reinstall command it prints, then `hostrec trust`. Chat: `codex`, `Made-up test: say hello.`, `/quit`.

**Expect:** the hooks stay trusted and fire; no review asked.
**If not:** note it.
**Settles:** F-151.

#### X-34. A stray file and a link in Codex's copy (5 min)

Side: `hostrec mark X-34`, `hostrec plugin-edit codex stray-file`, `hostrec trust`, `hostrec plugin-edit codex stray-link`, `hostrec trust`, then the reinstall command it prints, then `hostrec trust`.

**Expect:** after the stray file, `modified`; after the made-up link, an error or `modified`; after installing the same bytes again, `trusted`, with no review. `plugin-edit` also reports the shape of the cached copy's path.
**If not:** note each state.
**Settles:** F-154, F-155, F-160.

#### X-35. A second plugin with hooks (5 min)

Side: `hostrec mark X-35`, then `hostrec register codex hf-hooks-two`. Chat: `codex`, `Made-up test: say hello.`, `/quit`. Side: `hostrec trust`, then `hostrec unregister codex hf-hooks-two`, which removes it again with no terminal attached, leaving it unapproved.

**Expect:** `trust` reports the first plugin's hooks trusted and the second's waiting for review; the removal ends with no question.
**If not:** note it. If the removal fails, type it as `unregister` printed it, without `</dev/null`, and note any question it asked.
**Settles:** F-056, F-159.

#### X-36. A changed hook declaration (4 min)

Side: `hostrec mark X-36`, `hostrec plugin-edit codex hooks-timeout`, the reinstall command it prints, `hostrec trust`. Chat: `codex`, then `/hooks`: note whether a review is asked, and approve. `/quit`.

**Expect:** a review asked; trusted again after it.
**If not:** note it.
**Settles:** F-152, F-160.

#### X-37. A skill changed inside the hooks plugin (8 min)

Side: `hostrec mark X-37`, `hostrec plugin-edit codex hooks-skill`, the reinstall command it prints, `hostrec trust`. Chat: `codex`. Wait two minutes without typing. Then type `Made-up test: say hello before approving.` Then `/hooks`: note whether a review is asked, approve, `/quit`. Side: `hostrec trust`.

**Expect:** the first `trust` reports the hooks not trusted after the skill change; `/hooks` asks for a review; the last `trust` reports them trusted again. Nothing more to see on screen: the recording checks that no hook line came while you waited, though the scratch server started and the session file changed.
**If not:** note it.
**Settles:** F-024, F-043, F-150.

### Group I: Health, install and removal

**For:** HO-23, HO-11 and HO-13. **Asked:** When HO-23 is about to be built, or HO-11 and HO-13 if the second report leaves F-050 or F-057 open. **Needs, beyond Part 1:** A network that can be switched off for a minute (C-41, X-38). **Minutes:** 27 of its own steps, 52 with its frame.

**Facts it gives evidence for:** F-024, F-043, F-044, F-045, F-050, F-057, F-103, F-104.

#### C-29. A missing hook binary (3 min)

Side: `hostrec mark C-29`, `hostrec arm missing`. It renames `~/hf-tools/hf-missing-check`, which one declared hook names, outside every plugin. The exec-form hook, whose script lies under the plugin root, keeps running. Type `Made-up test: say hello.` Then side: `hostrec disarm`, which puts the file back.

**Expect:** the reply comes; at most a quiet notice. Nothing more to see on screen: the recording shows the exec-form hook's line, with the plugin root expanded.
**If not:** note exactly what was shown, and whether you had to do anything to go on.
**Settles:** F-050, F-104.

#### C-30. A chat with hooks switched off (4 min)

Side: `hostrec mark C-30`, `hostrec disarm`. Chat: `/exit`, then `claude --settings '{"disableAllHooks": true}'`. Wait two minutes without typing. Then type `Made-up test: say hello with hooks off.` and `/exit`.

**Expect:** the reply. Nothing more to see on screen: the recording checks that no hook line came from this chat, that the scratch MCP server started, and that the host's own session file changed.
**If not:** if the setting is refused, note it.
**Settles:** F-024, F-043.

#### C-31. A chat left open (3 min, then open until C-31b)

Side: `hostrec mark C-31`. Open a third Terminal window, type `cd ~/scratch/beta-lantern && claude`, and leave it open, typing nothing, while the group's next steps go on. C-31b swaps the recorder's program under it and closes it.

**Expect:** Claude Code opens in the third window. Nothing more to see on screen: the recording checks that the scratch server's minute stamps keep coming from one process, that SessionStart fired within 90 seconds of the server's start, and that no new server starts after the swap.
**If not:** note it.
**Settles:** F-044, F-045.

#### C-32. A changed hooks file (3 min)

Side: `hostrec mark C-32`, `hostrec plugin-edit claude-code timeout`, then the update command it prints. Chat: `claude`, then `Made-up test: say hello.`

**Expect:** no question or review; the recording shows the hook under its new time limit.
**If not:** note any question.
**Settles:** F-103.

#### C-41. Read-only reports (3 min)

Side: `hostrec mark C-41`, `hostrec cli-report claude-code`. It runs `claude --version`, `claude --help` and `claude plugin list --json`, and times each, once with the network off and once with it on. When it says so, disconnect the network (Wi-Fi off, or the cable out) and press Enter; when it says so again, connect it and press Enter.

**Expect:** each prints without signing in or calling a model.
**If not:** note it.
**Settles:** F-057.

#### C-31b. The open chat, after a new binary (2 min)

Side: `hostrec mark C-31b`, then `hostrec swap-binary`. It replaces `~/hf-tools/hostrec` with a second build of itself, under the chat C-31 left open. Wait one minute. Then, in the third window, type `/exit`, and close that window.

**Expect:** nothing to see on screen: the recording checks that the open chat kept one server, whose minute stamps came from one process until the close, and that no new server started after the swap.
**If not:** note it.
**Settles:** F-044.

#### X-27. A missing hook binary (3 min)

Side: `hostrec mark X-27`, `hostrec arm missing`. Chat: `codex`, then `Made-up test: say hello.`, then `/quit`. Side: `hostrec disarm`.

**Expect:** the reply comes; at most a quiet warning.
**If not:** note what was shown.
**Settles:** F-050.

#### X-38. Read-only reports (3 min)

Side: `hostrec mark X-38`, `hostrec cli-report codex`. It runs `codex --version`, `codex features list`, `codex plugin list --json` and the read-only `hooks/list`, and times each, once with the network off and once with it on. When it says so, disconnect the network (Wi-Fi off, or the cable out) and press Enter; when it says so again, connect it and press Enter.

**Expect:** each prints without signing in or calling a model.
**If not:** note it.
**Settles:** F-057.

#### X-48. Reports with the plugin switched off (3 min)

Side: `hostrec mark X-48`. Type `codex plugin disable hf-hooks@hf-scratch --json </dev/null` (or the form your version takes), then `hostrec cli-report codex`, then `codex plugin enable hf-hooks@hf-scratch --json </dev/null`, then `hostrec trust`.

**Expect:** the reports name the plugin's state in each case.
**If not:** if enabling again asks for a review, approve it in `/hooks` and note it.
**Settles:** F-057.

### Group J: The store, the git server and the clock

**For:** ST-05, ST-06 and ST-09, and HO-22 with decision 4 confirmed before it. **Asked:** Before ST-05 and ST-06 build the store's push, and before HO-22. Decision 4 is taken at the readout as "yes, confirmed before HO-22 is built"; this group is that confirmation. **Needs, beyond Part 1:** A new scratch repository (J-01), and a second, short visit on a later day for X-49, after at least one night with the Mac asleep: log in, C-00, X-49, U-02, then E-02 and E-03. The scheduled job runs in between, also while you are logged out. **Minutes:** 65 of its own steps, 95 with its frame.

**Facts it gives evidence for:** F-031, F-175, F-178, F-179, F-180, F-181, F-182, F-185, F-186, F-187, F-188, F-189, F-190, F-191, F-200, F-201, F-202, F-203, F-204.

#### J-01. A scratch repository again (14 min)

Part 1's scratch repository was deleted right after its pushes (G-05). Make a new one in the same way, with G-01, G-02 and G-03 of Part 1, each starting with its own mark. It has the same name, `hf-scratch-plum`; `hostrec key` makes a new key in place of the old one.

**Expect:** as in those steps.
**If not:** as in those steps. Without the repository, S0-12, S0-13, C-40 and X-30 cannot run; the group's other steps can.
**Settles:** gives evidence for F-182.

#### S0-02. Device management (2 min)

Side: `hostrec mark S0-02`.

1. In `hfeval`, open System Settings, General, Device Management (on older macOS: Privacy & Security, Profiles). Note whether any profile is listed.
2. Switch to your own account and look at the same page, reading only. Switch back to `hfeval`.

**Expect:** no profile in either account.
**If not:** a profile is listed. Note only that one is there and whether its name mentions login items or background items; never its other details. Type that note with `hostrec note` when you are back in `hfeval`.
**Settles:** F-202.

#### S0-09. The machine's git checks (5 min)

Side: `hostrec mark S0-09`, then `hostrec machine`.

**Expect:** a list of checks run in a scratch folder with made-up files, each pass or fail: git's version; a commit signed by a fresh ed25519 key given only per call, and its verification; the signature letters for an enrolled and an unknown key; a stand-in signing program set in the account's git config that must not be called; the three versions of a conflicted file after a merge with no commit; a symlink checked out as a plain file; `core.fsync` accepted; a push into a branch that is not checked out; maintenance with `gc.auto=0`; `ssh-keygen -Y verify` on a made-up signature; the extended attributes of a fetched file. No window or prompt appears.
**If not:** note any prompt or dialog that appeared, and which check it came with.
**Settles:** F-185, F-186, F-187, F-188.

#### S0-11. The scheduled job (15 min)

Side: `hostrec mark S0-11`.

1. Type `hostrec agent install`. It writes a launchd user agent named `org.example.hf-tick` and loads it with `launchctl bootstrap`. Every 15 minutes it appends a time to the recording, also while no sitting is open. On its first run it also: runs `claude -p "Made-up test: reply with the single word PEBBLE."` and `codex exec "Made-up test: reply with the single word PEBBLE."`; reads the scratch repository once with `git ls-remote` in batch mode with its own SSH setup, before anyone has accepted the server's host key; fetches one public eva-md release asset by name; writes, reads and removes a file under `~/.local/share/hf-test`; notes which git it finds; and posts one made-up notification with `osascript`.
2. Watch the screen for a "background item added" notice and for any password, keychain or permission dialog. Answer no dialog: press Cancel or Deny.
3. Wait for the first run to end (up to 5 minutes), then type `hostrec agent first-run`.

**Expect:** no administrator password asked; perhaps a one-time background-item notice; one notification titled with the made-up text; `first-run` shows each part's outcome. The read most likely stops at the unknown host key: that is a result, not a failure.
**If not:** note each dialog with its wording.
**Settles:** F-180, F-181, F-189, F-190, F-200, F-201, F-203, F-204.

#### S0-12. The server's host key, and reads with prompts off (10 min)

Side: `hostrec mark S0-12`.

1. Type `hostrec remote read`. It first connects once by SSH with the deploy key, in this window, so that you can accept the server's host key: when it asks "Are you sure you want to continue connecting", type `yes`. (If your server's help pages publish its host-key fingerprint, you may compare the two first.) It then puts the deploy key in this account's SSH agent, runs `git ls-remote` against `hf-scratch-plum` with prompts off and a 10-second limit, and makes an anonymous HTTPS read with no credentials.
2. Lock the agent: type `ssh-add -x`, and give it the made-up lock password `made-up-lock-1` twice. Then type `hostrec remote read --again`.
3. Unlock it with `ssh-add -X` and the same made-up password, then empty it with `ssh-add -D`.

**Expect:** the host-key question once; the first read succeeds in a second or two; the anonymous read fails fast; with the agent locked, the read fails fast and no dialog waits.
**If not:** if a dialog appears, note what it asked and answer nothing; it should time out. If the first read fails, note its class and tell the lead before S0-13.
**Settles:** F-178, F-179, F-180.

#### S0-13. The scheduled job's push (8 min)

Side: `hostrec mark S0-13`, then `hostrec agent kick`. It starts the scheduled job now and waits for that run to end (up to 5 minutes). This run reads the scratch repository in batch mode with its own SSH setup again, now that the host key is accepted. For this run only, it adds a line to this account's SSH config naming a made-up agent for the server. It then reads and pushes with the deploy key passed per call and no agent socket: the group's pushes to `hf-scratch-plum`, two commits authored as `eva.md (hf-test) <hf-test@machines.eva-md.invalid>`, one unsigned and one signed with a fresh key no account knows, each pushed by itself to a new branch, as in G-04. Last it removes the config line and prints each part's outcome.

**Expect:** the read with the job's own setup gets past the host-key check (it may then fail for want of a key, which is expected); the read with the deploy key succeeds; each push is accepted, or refused because of the committer or the signature (any other class of refusal leaves F-175 open, as in G-04); no dialog; on the server, `hf-scratch-plum` holds a new branch for each push accepted.
**If not:** note each dialog with its wording. If the job did not run at all, type `hostrec push` once in the side window: it makes the same pushes from this window with the same key, and they are still the group's pushes.
**Settles:** F-175, F-180, F-182, F-191, F-201.

#### S0-15. The deploy-key commands (2 min)

Side: `hostrec mark S0-15`, then `hostrec deploy-key`. It looks for `gh` and `glab` on this Mac, signs in to nothing, and reports only whether each is installed and whether its help shows a deploy-key command with write access. Do not install either for this.

**Expect:** a short report; "neither installed" is a result too.
**If not:** note it.
**Settles:** F-182.

#### C-40. A job a hook starts, against the git server (2 min)

Side: `hostrec mark C-40`, `hostrec arm net-job`. Chat: `cd ~/scratch/alpha-orchard && claude`, type `Made-up test: say hello.`, wait 20 seconds, `/exit`. Side: `hostrec children`.

**Expect:** the Stop hook's job read the scratch repository and wrote under `~/.local/share/hf-test`.
**If not:** note it.
**Settles:** F-031, F-189, F-190.

#### X-30. A job a hook starts, against the git server (2 min)

Side: `hostrec mark X-30`, `hostrec arm net-job`. Chat: `codex`, `Made-up test: say hello.`, wait 20 seconds, `/quit`. Side: `hostrec children`.

**Expect:** the job read the scratch repository and wrote under `~/.local/share/hf-test`.
**If not:** note it.
**Settles:** F-031, F-189, F-190.

#### J-02. Delete the scratch repository again (3 min)

As steps 2 to 4 of G-05 in Part 1: in your own account's browser, delete `hf-scratch-plum` (and its deploy key with it), and check that no key titled `hf-eval-made-up` is left; in Finder, delete `/Users/Shared/hf-scratch-address.txt`, which your own account saved in J-01. If group M's S0-16 is asked for in the same sitting, do it before this step.

**Expect:** the scratch repository is gone from the server.
**If not:** note what is left in a message to the lead, and remove it by hand.
**Settles:** nothing on its own.

#### X-49. The scheduled job's times (2 min)

Side: `hostrec mark X-49`, `hostrec agent times`, then `hostrec agent remove`.

**Expect:** a time about every 15 minutes while the Mac was awake. Do not expect a run at the moment of waking: the job may run again only at its next interval, and the gap after each wake is what this step measures.
**If not:** note any gap while the Mac was awake.
**Settles:** F-200.

### Group K: The evaluation harness

**For:** EV-22 and `eva-eval home init`. **Asked:** Before `eva-eval home init` accepts a folder as a home, and before EV-22's canary. **Needs, beyond Part 1:** A second sign-in of each host, in a folder made for it. **Minutes:** 30 of its own steps, 55 with its frame.

**Facts it gives evidence for:** F-023, F-028, F-029, F-073, F-074.

#### S0-14. A sign-in under another HOME (10 min)

Side: `hostrec mark S0-14`.

1. Type `hostrec keychain-names before`. It keeps, never showing them, a hash of the name of each of the login keychain's items.
2. Type `mkdir -p ~/home-b && HOME=~/home-b claude`. Sign in when asked, type `Made-up test: say hello.`, then `/exit`.
3. Type `HOME=~/home-b codex login` and sign in.
4. Type `claude` (with the account's own HOME). It should still be signed in. Type `/exit`. Type `codex login status`.
5. Type `hostrec keychain-names after`. It shows the names of the items added or changed since step 1, never their contents or account fields.

**Expect:** both hosts sign in under `~/home-b` and make their folders there; the account's own sign-in still holds afterwards.
**If not:** if the account's own sign-in was replaced or lost, note it, and sign in again as in C-0a and X-0a.
**Settles:** F-073, F-074.

#### C-35. The environment a hook gets, and another config folder (10 min)

Side: `hostrec mark C-35`.

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

#### X-25. The environment a hook gets, and another Codex home (10 min)

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

### Group L: Voice, the rest

**For:** section 12's voice item, and decision 30's second option. **Asked:** If the lead takes section 12's voice item or decision 30's second option, or the second report's counts leave F-146 to F-149 open. **Needs, beyond Part 1:** A microphone and a quiet room, as for X-44. **Minutes:** 25 of its own steps, 50 with its frame.

**Facts it gives evidence for:** F-141, F-146, F-147, F-148, F-149.

#### X-45. Voice call two: a long stretch, ended by quitting the app (22 min)

Side: `hostrec mark X-45`. Open the Codex desktop app, and in it start a new thread for `alpha-orchard` and start a voice call, in the same quiet room.

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

#### X-46b. After the second call (3 min)

Side: `hostrec mark X-46b`, `hostrec voice-check`. As X-46, now for both calls; it also prints the byte count of the talk before X-45's hand-over.

**Expect:** a short table of counts; the talk's byte count well over 4,096; no text from the files.
**If not:** note it.
**Settles:** F-146, F-147, F-149.

### Group M: Items of section 12 with no package yet

**For:** tool-time delivery, rules files, the host's memory and forced pushes. **Asked:** Only when an item of section 12 is opened: tool-time delivery (C-06 to C-08, X-09), generated rules files (C-33), the host's memory (C-34) or removal from pushed history (S0-16). **Needs, beyond Part 1:** For S0-16, a scratch repository (see the step). **Minutes:** 28 of its own steps, 53 with its frame.

**Facts it gives evidence for:** F-021, F-048, F-081, F-082, F-087, F-089, F-090, F-094, F-183.

#### C-06. An edit on another branch, with a word from the pre-tool hook (4 min)

Side: `hostrec mark C-06`, `git -C ~/scratch/alpha-orchard switch -c hf-branch-two`, `hostrec arm pretool-word`. Type:

```
Made-up test. In orchard/plums.txt, change the line that starts with `count:` so that it reads `count: 13`. Then write the file orchard/new-plum.txt containing the single word `damson`.
```

When it is done, type:

```
What checkword, if any, were you given while you edited? Word only, or NONE.
```

**Expect:** both edits; the answer `HARBOR-NINE`.
**If not:** note the answer.
**Settles:** F-021, F-081, F-089.

#### C-07. Parallel edits (3 min)

Side: `hostrec mark C-07`, `hostrec arm pretool-slow`. Type:

```
Made-up test. Create three new files at once, using parallel tool calls if you can: orchard/a.txt containing `alpha`, orchard/b.txt containing `beta`, and orchard/c.txt containing `gamma`. Then run `cat orchard/a.txt` in the shell.
```

**Expect:** three writes, each held about two seconds by the hook, then a shell command.
**If not:** note whether the writes came one after another.
**Settles:** F-048, F-081.

#### C-08. Two reads, and a word from an async hook (5 min)

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

#### C-33. A rules file with a path pattern (5 min)

Side: `hostrec mark C-33`, `hostrec arm rules`. It writes a rules file in this account's Claude Code folder, for paths `orchard/**`, holding the made-up word `CEDAR-WREN`. Chat: `/exit`, `cd ~/scratch/alpha-orchard && claude`, then:

```
Made-up test. Read orchard/plums.txt. Then: do you have any rule that mentions a word in capitals? Quote the word or say NONE. Then delegate the same question to a subagent and give its answer too.
```

Then `/compact`, and ask again: `Do you still have a rule with a word in capitals? Word or NONE.` Then `/exit`, `cd ~/scratch/beta-lantern && claude`, and type the first prompt again with `lantern/wicks.txt` in place of `orchard/plums.txt`.

Then side: `hostrec disarm`, which removes the rules file.

**Expect:** the word is known in alpha-orchard after the read; the answers in the other places are what this step measures.
**If not:** note each answer.
**Settles:** F-094.

#### C-34. A note for the host's own memory (3 min)

Side: `hostrec mark C-34`. Type:

```
Made-up test. Please remember for later that the made-up gate code is 4417.
```

Then side: `hostrec memory-index`.

**Expect:** the host saves a memory note; `memory-index` reports the index file's path shape and format.
**If not:** note it.
**Settles:** F-087.

#### X-09. Parallel edits (3 min)

Side: `hostrec mark X-09`, `hostrec arm pretool-slow`. Type:

```
Made-up test. Create three new files at once, in parallel if you can: orchard/d.txt containing `delta`, orchard/e.txt containing `epsilon`, and orchard/f.txt containing `zeta`.
```

**Expect:** three edits, each held about two seconds by the hook.
**If not:** note whether they came one after another.
**Settles:** F-048.

#### S0-16. A forced push with a lease (5 min)

Needs a scratch repository: run it in group J, before J-02, or run J-01 first and J-02 after.

1. Switch to your own account. On `hf-scratch-plum`'s settings page, look at the default branch: is it protected, and are forced pushes allowed? Switch back to `hfeval` and type `hostrec mark S0-16`, then `hostrec note "scratch main protected: yes|no; force push allowed: yes|no"`, keeping one word of each pair.
2. Only if you agree to one more push to `hf-scratch-plum`: type `hostrec force-lease`. It rewrites the last commit of `hf-scratch-plum` and pushes it with `--force-with-lease`, with the deploy key.

**Expect:** accepted where forced pushes are allowed, refused where `main` is protected.
**If not:** note the outcome.
**Settles:** F-183.

### Group N: Taking it all down

**For:** the end of the sittings. **Asked:** When the last group the lead wants is done, or when the lead says no more will be asked. **Needs, beyond Part 1:** Part 1's recorder is enough. Its frame is C-00, then E-01, E-02, E-03 and E-05, with no U-01 or U-02. **Minutes:** 20 of its own steps, 37 with its frame.

**Facts it gives evidence for:** F-056.

#### E-01. Remove the scratch plugins (5 min)

Side: `hostrec mark E-01`, `hostrec disarm`, then `hostrec unregister all`. It removes the scratch plugins and marketplaces from both hosts with the hosts' own commands, each with no terminal attached, and records whether any asked a question.

**Expect:** each removal ends with exit status 0 and no question, and the lists it prints at the end hold no scratch plugin.
**If not:** for a removal that failed, type it as `unregister` printed it, without `</dev/null`; if it asks a question, answer it and note the question. If the lead later finds a fact missing, `hostrec register` puts the plugins back for the steps to redo.

Then type `hostrec close`.
**Settles:** F-056.

#### E-05. Take everything down (15 min)

Only after the lead has confirmed the last hand-over.

1. In `hfeval`'s side window, type `hostrec agent remove` if group J installed the scheduled job and its X-49 did not remove it. Then type `hostrec teardown`. It removes `~/home-b`, `~/hf-alt-claude`, `~/hf-alt-codex`, `~/.local/share/hf-test`, the deploy key and its public half, `~/hf-private`, `~/hf-rec`, the files this account left in `/Users/Shared` (the public key's file and the archive with its checksum), the scratch folders and marketplaces, and the setup marker, whichever of them exist, and lists what it removed.
2. Switch to your own account. In your browser, check that the git server holds no repository named `hf-scratch-plum` (delete one if it is there, which removes its deploy key) and that no key titled `hf-eval-made-up` is left in your settings. In Finder, delete `/Users/Shared/hf-scratch-address.txt` if it is still there; your own account saved it, so only your own account can delete it.
3. Either delete the evaluation account (System Settings, Users & Groups, delete the user and its home folder), or keep it. [The live gates](live-gates-runbook.md) (DV-02) use the same home and its recorder's `scan` and `scrub`, so they are run before this step, after Part 1's hand-over; if they have not run yet, stop here and tell the lead. The recorder, its plugins and its recording are gone either way.

**Expect:** `teardown` lists each folder it removed; nothing of the sittings is left on the server.
**If not:** note what is left in a message to the lead, and remove it by hand.
**Settles:** nothing on its own.

## Facts to steps

Each fact the sheet marks for a sitting, with its mark and the steps that give evidence for it: in Part 1, at no added minute where the fact is not core, and in Part 2, by group. A fact with no step is settled by the second report or by published source alone.

| Fact | Sitting, first version | Mark | Part 1 steps | Part 2 steps |
| --- | --- | --- | --- | --- |
| F-001 A turn id on every event of a turn | Both | by the field report | C-04, X-04 | none |
| F-002 A tool-use id on tool events, and on the record's row | Both | from source | C-04, X-04 | C-05 (C) |
| F-003 The question, plan-approval and delegation tools reach PostToolUse whole | Both | later, group C | none | C-09 (C), C-10 (C), C-12 (C), X-10 (C), X-11 (C), X-12 (C) |
| F-004 The Stop payload carries the final reply | Both | by the field report | C-04, C-15, X-04 | X-14 (B) |
| F-005 Payloads name the session's record file | Both | by the field report | C-04, X-04 | none |
| F-006 SessionStart names its source, SessionEnd its reason | Both | from source | C-27 | X-24 (D), C-26 (E), X-23 (E), C-39 (G), X-29 (G) |
| F-007 A failed tool call's payload holds the error and the exit status | Both | from source | none | C-05 (C), X-06 (C) |
| F-008 An interrupted or failed turn is marked as ended | Both | from source | C-15 | C-36 (B), X-14 (B) |
| F-009 A typed slash command or skill reaches the prompt hook before the model | Both | later, group D | C-17 | C-18 (D), X-16 (D) |
| F-010 A message typed while the agent works fires a prompt hook | Both | by the field report for Codex's half; later, group B, for Claude Code's | none | C-14 (B), X-13 (B) |
| F-011 The desktop app's spare session starts again in the real folder | Both | later, group A | X-40 | C-38 (A) |
| F-012 Each host reports its own version | Both | from source | C-02, X-02 | none |
| F-013 A session id is safe in a file name | Both | by the field report | C-04, X-04 | none |
| F-014 The session id survives a clear | Both | later, group E | none | C-26 (E), X-23 (E) |
| F-015 A read or search in another repository names that repository | Both | later, group G | none | C-23 (G), X-20 (G) |
| F-016 A prompt the host writes is marked as the host's | Both | core for Claude Code's half, the kinds that reach the hook; the kinds only in the record later, groups C and E; Codex's half from source and the field report | C-04 (core), C-15 (core), C-17 (core), C-19 (core), C-27 (core), X-43 | X-14 (B), X-21 (B), C-12 (C), X-16 (D), C-25 (E) |
| F-017 Pasted text is marked on Claude Code and not on Codex | Both | later, group D | none | C-16 (D), X-15 (D) |
| F-018 A program's session can be told from a person's chat | Both | by the field report | X-26 | C-37 (A) |
| F-019 The reply row is on disk when Stop fires | Both | later | C-04, X-04 | none |
| F-020 The record is appended in order | Both | by the field report | C-04, X-04 | none |
| F-021 Each record row carries its time, folder and branch | Both | by the field report | C-04, X-04 | X-07 (H), C-06 (M) |
| F-022 Text between tool calls and each tool's status are separate rows | Both | by the field report | C-04, X-04 | none |
| F-023 The store root comes from the host's environment | Both | from source | none | C-35 (K), X-25 (K) |
| F-024 Each chat writes a session file even with no hook | Both | by the field report | none | X-37 (H), C-30 (I) |
| F-025 SubagentStop carries the report and the agent id | Both | by the field report | none | C-12 (C), X-12 (C) |
| F-026 SubagentStart can add context, and tells the host's own agents apart | Both | later, group C | none | C-12 (C), C-13 (C), X-12 (C) |
| F-027 A subagent's hook events carry its agent id | Both | by the field report | none | C-12 (C), X-12 (C) |
| F-028 A hook inherits the host's environment | Both | by the field report | X-26 | C-37 (A), C-35 (K), X-25 (K) |
| F-029 A hook's environment: home, shell, agent socket, path | Both | by the field report | none | C-35 (K), X-25 (K) |
| F-030 A detached child outlives its hook | Both | by the field report | none | C-39 (G), X-29 (G) |
| F-031 A job a hook starts can reach the git server | Both | by the field report | none | C-40 (J), X-30 (J) |
| F-032 Each host runs headless on a plan login | Both | by the field report | X-26 | C-37 (A) |
| F-033 A fork or resume names its parent | Both | core for Claude Code's half; Codex's half from source | C-27 (core) | X-24 (D) |
| F-034 A compaction leaves a hook or a record row | Both | by the field report | none | X-21 (B), C-25 (E), X-22 (E) |
| F-035 An MCP tool's name names its server, and other tools are told apart | Both | from source | none | C-22 (B), X-19 (B), C-20 (F), X-17 (F) |
| F-036 PostToolUse for an MCP tool holds its result text | Both | from source | none | C-20 (F), X-17 (F) |
| F-037 An MCP server's instructions reach the model, within a size | Both | from source | none | C-20 (F), X-17 (F) |
| F-038 The first use of an MCP tool asks once | Both | by the field report | none | C-20 (F), X-17 (F) |
| F-039 A tool result has a size limit | Both | from source | none | C-20 (F), X-17 (F) |
| F-040 An MCP server is not told which chat it serves | Both | from source | none | C-20 (F), X-17 (F) |
| F-041 Whether a host reads an MCP server's resources | Both | from source | none | C-20 (F), X-17 (F) |
| F-042 Where the time of an MCP call goes | Both | later, group F | none | C-21 (F), X-18 (F) |
| F-043 A plugin's MCP server starts even when its hooks do not run | Both | later, group I | none | X-37 (H), C-30 (I) |
| F-044 One MCP server process per chat, for the chat's life | Both | later, group I | none | X-47 (G), X-31 (H), X-32 (H), C-31 (I), C-31b (I) |
| F-045 SessionStart follows the MCP server's start within 90 seconds | Both | later, group I | none | X-47 (G), X-31 (H), C-31 (I) |
| F-046 A `systemMessage` is shown to the person and not to the model | Both | by the field report | C-01, C-02, X-02, X-40 | C-38 (A) |
| F-047 SessionStart is synchronous | Both | by the field report | C-01, C-02, X-02 | none |
| F-048 Several pre-tool hooks can run at once | Both | later, group M | none | C-07 (M), X-09 (M) |
| F-049 The host adds its own context at a chat start | Both | from source | C-02, X-02 | none |
| F-050 A missing hook binary leaves the chat unharmed | Both | from source | none | C-29 (I), X-27 (I) |
| F-056 Plugins register and are removed with no question | Sitting 0 | later, group N | C-0b, X-0b | X-35 (H), E-01 (N) |
| F-057 Each CLI reports its state read-only | Both | by the field report | none | C-41 (I), X-38 (I), X-48 (I) |
| F-073 Each host signs in under another HOME and stays signed in | Sitting 0 | later, group K | C-01 | X-01 (H), S0-14 (K) |
| F-074 A sign-in under another HOME leaves the account's own sign-in alone | Sitting 0 | later, group K | none | S0-14 (K) |
| F-075 A plugin can declare every event v2 uses | Claude Code | later | C-42 | none |
| F-076 StopFailure fires on a failed turn | Claude Code | later, group B | none | C-36 (B) |
| F-077 PostModelSwitch fires on a model change | Claude Code | later, group C | none | C-24 (C) |
| F-078 The task tool's payload holds the whole list | Claude Code | later, group C | none | C-11 (C) |
| F-079 The entrypoint tells a person's chat from a program's | Claude Code | by the field report | none | C-37 (A), C-38 (A) |
| F-080 UserPromptSubmit also fires for prompts no one typed | Claude Code | later | C-19 | none |
| F-081 PreToolUse holds the path or the command | Claude Code | from source | none | C-06 (M), C-07 (M) |
| F-082 PostToolBatch names each call of the batch | Claude Code | later, group M | none | C-08 (M) |
| F-083 A resume's SessionStart says how long it has been | Claude Code | later | C-27 | none |
| F-084 Transcript rows carry ids and flags | Claude Code | by the field report | C-04 | none |
| F-085 Transcripts hold system prompts and request records | Claude Code | by the field report | C-04 | none |
| F-087 The memory index is a local file | Claude Code | by the field report | none | C-34 (M) |
| F-088 Injected text: whole up to 10,000 characters | Claude Code | later, group E | none | C-03 (E) |
| F-089 PreToolUse context arrives beside the result | Claude Code | later, group M | none | C-06 (M) |
| F-090 An async hook's context arrives a turn later | Claude Code | later, group M | none | C-08 (M) |
| F-091 A subagent gets instruction files and not the start context | Claude Code | later, group C | none | C-12 (C) |
| F-092 A hook past its timeout shows a notice | Claude Code | later, group E | none | C-28 (E) |
| F-093 An MCP tool can load at start | Claude Code | later, group F | none | C-02b (F) |
| F-094 A user-level rules file with a path pattern | Claude Code | later, group M | none | C-33 (M) |
| F-098 PostCompact carries the summary | Claude Code | later, group E | none | C-25 (E) |
| F-099 PreCompact steers the summary | Claude Code | later, group E | none | C-25 (E) |
| F-100 PostCompact fires before SessionStart `compact` | Claude Code | later, group E | none | C-25 (E) |
| F-101 SessionStart `compact` output joins the new context | Claude Code | later, group E | none | C-25 (E) |
| F-102 Session end gives hooks 1.5 seconds | Claude Code | later, group G | none | C-39 (G) |
| F-103 A changed hooks file is used with no review | Claude Code | later, group I | none | C-32 (I) |
| F-104 Hooks run in exec form with the plugin root expanded | Claude Code | later, group I | none | C-29 (I) |
| F-105 The desktop app's Code tab runs its own Claude Code | Claude Code | later, group A | none | C-38 (A) |
| F-120 A plugin can declare every event v2 uses | Codex | from source | X-04, X-50 | X-14 (B) |
| F-121 The typed-prompt mark in the record | Codex | core; its subagent case from source | X-04 (core), X-26 (core), X-40 (core), X-43 (core), X-44 (core) | X-12 (C) |
| F-122 That row is on disk when the prompt hook fires | Codex | from source | X-04 | none |
| F-123 PreToolUse adds context, and an edit matcher matches `apply_patch` | Codex | later, group H | none | X-07 (H) |
| F-124 An async PostToolUse's context arrives before the next request | Codex | later, group H | none | X-08 (H) |
| F-125 A hook message: about 2,500 tokens, 3,000 declared | Codex | from source | none | X-03 (E) |
| F-126 Session end on close and after 30 idle minutes | Codex | by the field report | none | X-39b (G), X-41 (G), X-29 (G), X-47 (G) |
| F-127 A managed worktree as the working folder | Codex | by the field report | none | X-41 (G) |
| F-132 The rollout is written in a known history mode | Codex | by the field report | X-04 | none |
| F-133 Rollout rows have a stable position | Codex | by the field report | X-04 | none |
| F-134 The opening row names the session and folder, and can be large | Codex | by the field report | none | X-05 (B) |
| F-135 PostCompact carries only the trigger | Codex | from source | none | X-21 (B) |
| F-136 The rollout holds the compaction summary | Codex | by the field report | none | X-21 (B) |
| F-137 Desktop threads name a session file eva-md can read | Codex | by the field report | X-40 | none |
| F-138 A thread opened and never run ends with no start | Codex | by the field report | none | X-42 (B) |
| F-141 A hand-over reaches the prompt hook as a wrapper | Codex | by the field report | X-44 | X-45 (L) |
| F-142 The request is the front model's wording | Codex | core | X-39 (core), X-44 (core), X-46 (core) | none |
| F-143 Each spoken line keeps its speaker | Codex | core | X-44 (core) | none |
| F-144 A task voice starts by itself begins inside the wrapper | Codex | core | X-44 (core) | none |
| F-145 A hand-over while the agent works shares its turn's id | Codex | from source | X-44 | none |
| F-146 Each field of a hand-over holds at most 4 KiB | Codex | from source | none | X-45 (L), X-46b (L) |
| F-147 Talk past the limit and the call's end are rows no hook saw | Codex | by the field report | X-44, X-46 | X-45 (L), X-46b (L) |
| F-148 Nothing after the last hand-over reaches a hook | Codex | by the field report | X-44 | X-45 (L) |
| F-149 The session file holds a line for each utterance | Codex | by the field report | X-44, X-46 | X-45 (L), X-46b (L) |
| F-150 Approval is bound to every byte of the hooks plugin | Codex | by the field report | none | X-01 (H), X-37 (H) |
| F-151 A change to a plugin with no hooks costs no review | Codex | from source | none | X-33 (H) |
| F-152 A changed hook declaration asks for a new review | Codex | from source | none | X-36 (H) |
| F-153 A new binary behind an approved hook keeps the approval | Codex | from source | none | X-32 (H) |
| F-154 A stray file unapproves; the same bytes again restore it | Codex | from source | none | X-34 (H) |
| F-155 Codex runs a cached copy of the plugin | Codex | from source | none | X-34 (H) |
| F-156 One approval holds for the CLI and the desktop app | Codex | by the field report | X-39 | none |
| F-157 Hooks load only from `.codex-plugin/plugin.json` | Codex | from source | none | X-01 (H) |
| F-158 Two plugins from one marketplace | Sitting 0 | from source | none | U-01 |
| F-159 A second hooks plugin leaves the first approved | Codex | from source | none | X-35 (H) |
| F-160 `hooks/list` reports each hook's trust | Codex | by the field report | none | X-01 (H), X-34 (H), X-36 (H) |
| F-161 Approving in `/hooks` is all it takes | Sitting 0 | by the field report | X-0c | none |
| F-162 Hook commands run through a non-login shell | Codex | from source | none | X-28 (H) |
| F-173 One hook start costs 100 ms or less at the 95th percentile | Codex | by the field report | none | X-28 (H) |
| F-174 SessionStart `compact` runs before the next request, also mid-turn | Codex | later, group E | none | X-22 (E) |
| F-175 The server takes a push of machine commits | Sitting 0 | core | G-04 (core), G-05 (core) | S0-13 (J) |
| F-176 The real wiki's project has no push rule against them | Sitting 0 | by the field report | none | none |
| F-177 The real wiki's git server is reachable without a private network | Sitting 0 | by the field report | none | none |
| F-178 A read with prompts off never waits | Sitting 0 | by the field report | none | S0-12 (J) |
| F-179 An anonymous read of a private repository fails fast | Sitting 0 | from source | none | S0-12 (J) |
| F-180 A background job knows the server's host key | Sitting 0 | by the field report | none | S0-12 (J), S0-11 (J), S0-13 (J) |
| F-181 Release downloads reach a background job | Sitting 0 | later, group J | none | S0-11 (J) |
| F-182 Write deploy keys exist and can be added | Sitting 0 | by the field report | G-02, G-04 | J-01 (J), S0-15 (J), S0-13 (J) |
| F-183 The default branch takes a push with a lease | Sitting 0 | later, group M | none | S0-16 (M) |
| F-185 The Mac's git does what the store needs | Sitting 0 | by the field report | G-04 | S0-09 (J) |
| F-186 `ssh-keygen` makes and signs with a key, with no prompt | Sitting 0 | from source | G-04 | S0-09 (J) |
| F-187 Signature letters for known and unknown keys | Sitting 0 | from source | none | S0-09 (J) |
| F-188 Per-call settings never start the person's signing program | Sitting 0 | from source | none | S0-09 (J) |
| F-189 The git a background job finds is new enough | Sitting 0 | by the field report | none | C-40 (J), X-30 (J), S0-11 (J) |
| F-190 No privacy prompt for the store's folder | Sitting 0 | from source | none | C-40 (J), X-30 (J), S0-11 (J) |
| F-191 The machine key pushes with no agent | Sitting 0 | from source | G-04 | S0-13 (J) |
| F-200 A launchd user agent installs without an admin and keeps time | Sitting 0 | later, group J | none | S0-11 (J), X-49 (J) |
| F-201 Work the scheduler starts reaches the model and the git server | Sitting 0 | later, group J | none | S0-11 (J), S0-13 (J) |
| F-202 No device management forbids user launch agents | Sitting 0 | by the field report | none | S0-02 (J) |
| F-203 Installing an agent shows a background-item notice | Sitting 0 | from source | none | S0-11 (J) |
| F-204 A notification from a scheduled job needs no permission | Sitting 0 | later, group J | none | S0-11 (J) |

## Steps to facts

Every step, where it is, its minutes, and the facts it gives evidence for; core facts are marked.

| Step | Where | Minutes | Facts |
| --- | --- | --- | --- |
| S0-01 Make the evaluation account | Part 1, sitting 0 | 10 | none |
| S0-03 Tools for the account | Part 1, sitting 0 | 20 | none |
| S0-05 Build the recorder and the scratch folders | Part 1, sitting 0 | 11 | none |
| C-00 Open the windows | Part 1, sitting 1 | 2 | none |
| G-01 A key for the scratch repository | Part 1, sitting 1 | 2 | none |
| G-02 The scratch repository and its key, on the git server | Part 1, sitting 1 | 10 | F-182 |
| G-03 The repository's address | Part 1, sitting 1 | 2 | none |
| G-04 The pushes | Part 1, sitting 1 | 4 | F-175 (core), F-182, F-185, F-186, F-191 |
| G-05 Delete the scratch repository | Part 1, sitting 1 | 4 | F-175 (core) |
| C-0a Sign Claude Code in | Part 1, sitting 1 | 4 | none |
| C-0b Register the Claude Code scratch plugin | Part 1, sitting 1 | 4 | F-056 |
| C-01 Start | Part 1, sitting 1 | 3 | F-046, F-047, F-073 |
| C-02 The first prompt | Part 1, sitting 1 | 4 | F-012, F-046, F-047, F-049 |
| C-04 A turn with two tools and words between | Part 1, sitting 1 | 4 | F-001, F-002, F-004, F-005, F-013, F-016 (core), F-019, F-020, F-021, F-022, F-084, F-085 |
| C-15 An interrupt | Part 1, sitting 1 | 2 | F-004, F-008, F-016 (core) |
| C-17 A shell line and a plugin command | Part 1, sitting 1 | 3 | F-009, F-016 (core) |
| C-19 Prompts nobody typed | Part 1, sitting 1 | 5 | F-016 (core), F-080 |
| C-27 Resume and fork | Part 1, sitting 1 | 4 | F-006, F-016 (core), F-033 (core), F-083 |
| C-42 End of sitting 1 | Part 1, sitting 1 | 5 | F-075 |
| X-00 Open the windows | Part 1, sitting 2 | 2 | none |
| X-0a Sign Codex in, in the CLI and the desktop app | Part 1, sitting 2 | 6 | none |
| X-0b Register the Codex scratch plugin | Part 1, sitting 2 | 4 | F-056 |
| X-0c Approve the Codex hooks | Part 1, sitting 2 | 5 | F-161 |
| X-02 The first prompt | Part 1, sitting 2 | 4 | F-012, F-046, F-047, F-049 |
| X-04 A turn with two tools and words between | Part 1, sitting 2 | 4 | F-001, F-002, F-004, F-005, F-013, F-019, F-020, F-021, F-022, F-120, F-121 (core), F-122, F-132, F-133 |
| X-26 A run a program drives | Part 1, sitting 2 | 3 | F-018, F-028, F-032, F-121 (core) |
| X-39 The desktop app's version and voice setting | Part 1, sitting 2 | 3 | F-142 (core), F-156 |
| X-40 A short chat in the app | Part 1, sitting 2 | 6 | F-011, F-046, F-121 (core), F-137 |
| X-43 A scheduled run in the app | Part 1, sitting 2 | 4 | F-016, F-121 (core) |
| X-44 Voice call one: hand-overs, a correction, a yes, the pause phrase | Part 1, sitting 2 | 13 | F-121 (core), F-141, F-142 (core), F-143 (core), F-144 (core), F-145, F-147, F-148, F-149 |
| X-46 After the call | Part 1, sitting 2 | 3 | F-142 (core), F-147, F-149 |
| X-50 End of sitting 2 | Part 1, sitting 2 | 5 | F-120 |
| E-02 Check the recording | Part 1, hand-over | 10 | none |
| E-03 Pack and hand over | Part 1, hand-over | 5 | none |
| U-01 Bring the recorder up to date | Part 2, every group | 5 | F-158 |
| U-02 End of a group | Part 2, every group | 3 | none |
| C-0c Sign the Claude desktop app in | Part 2, group A | 3 | none |
| C-37 A run a program drives | Part 2, group A | 3 | F-018, F-028, F-032, F-079 |
| C-38 The desktop app's Code tab | Part 2, group A | 6 | F-011, F-046, F-079, F-105 |
| C-14 A message typed while the agent works | Part 2, group B | 4 | F-010 |
| C-36 A turn that fails | Part 2, group B | 3 | F-008, F-076 |
| C-22 A web fetch and a network command | Part 2, group B | 3 | F-035 |
| X-13 A message typed while the agent works | Part 2, group B | 4 | F-010 |
| X-14 An interrupt | Part 2, group B | 2 | F-004, F-008, F-016, F-120 |
| X-19 A web search and a network command | Part 2, group B | 3 | F-035 |
| X-05 Long instructions | Part 2, group B | 3 | F-134 |
| X-21 A compaction by command | Part 2, group B | 5 | F-016, F-034, F-135, F-136 |
| X-42 Threads opened and never run | Part 2, group B | 3 | F-138 |
| C-05 A command that fails | Part 2, group C | 2 | F-002, F-007 |
| C-09 A multiple-choice question | Part 2, group C | 5 | F-003 |
| C-10 A plan approved and a plan rejected | Part 2, group C | 5 | F-003 |
| C-11 A task list | Part 2, group C | 3 | F-078 |
| C-12 A subagent | Part 2, group C | 6 | F-003, F-016, F-025, F-026, F-027, F-091 |
| C-13 A custom agent | Part 2, group C | 3 | F-026 |
| C-24 A model change | Part 2, group C | 2 | F-077 |
| X-06 A command that fails | Part 2, group C | 2 | F-007 |
| X-10 A multiple-choice question | Part 2, group C | 4 | F-003 |
| X-11 A plan approved and a plan rejected | Part 2, group C | 5 | F-003 |
| X-12 A subagent | Part 2, group C | 6 | F-003, F-025, F-026, F-027, F-121 |
| C-16 Pasted text | Part 2, group D | 3 | F-017 |
| C-18 The pause phrase, typed | Part 2, group D | 2 | F-009 |
| X-15 Pasted text | Part 2, group D | 3 | F-017 |
| X-16 A shell line, a skill and the pause phrase, typed | Part 2, group D | 3 | F-009, F-016 |
| X-24 Resume and fork | Part 2, group D | 6 | F-006, F-033 |
| C-03 Long start texts | Part 2, group E | 5 | F-088 |
| C-25 A compaction | Part 2, group E | 5 | F-016, F-034, F-098, F-099, F-100, F-101 |
| C-26 A clear | Part 2, group E | 2 | F-006, F-014 |
| C-28 A hook past its time limit | Part 2, group E | 2 | F-092 |
| X-03 Long hook messages | Part 2, group E | 10 | F-125 |
| X-22 A compaction in the middle of a turn | Part 2, group E | 8 | F-034, F-174 |
| X-23 A clear | Part 2, group E | 2 | F-006, F-014 |
| C-02b Tools that load at start | Part 2, group F | 2 | F-093 |
| C-20 The scratch MCP server | Part 2, group F | 8 | F-035, F-036, F-037, F-038, F-039, F-040, F-041 |
| C-21 The same lookup by MCP and by command | Part 2, group F | 5 | F-042 |
| X-17 The scratch MCP server | Part 2, group F | 8 | F-035, F-036, F-037, F-038, F-039, F-040, F-041 |
| X-18 The same lookup by MCP and by command | Part 2, group F | 5 | F-042 |
| X-39b A Codex chat left idle | Part 2, group G | 2 | F-126 |
| X-41 A task in a worktree | Part 2, group G | 4 | F-126, F-127 |
| C-23 Another repository | Part 2, group G | 3 | F-015 |
| C-39 Ending a chat | Part 2, group G | 4 | F-006, F-030, F-102 |
| X-20 Another repository | Part 2, group G | 3 | F-015 |
| X-29 Closing right after a turn | Part 2, group G | 4 | F-006, F-030, F-126 |
| X-47 The chat left idle | Part 2, group G | 2 | F-044, F-045, F-126 |
| X-01 Hooks approved days ago | Part 2, group H | 3 | F-073, F-150, F-157, F-160 |
| X-07 An edit on another branch, with a word from the pre-tool hook | Part 2, group H | 4 | F-021, F-123 |
| X-08 A word from an async hook, in the same turn | Part 2, group H | 3 | F-124 |
| X-28 Timed hook runs | Part 2, group H | 10 | F-162, F-173 |
| X-31 A chat left open | Part 2, group H | 2 | F-044, F-045 |
| X-32 A new binary behind approved hooks | Part 2, group H | 4 | F-044, F-153 |
| X-33 A change to the plugin with no hooks | Part 2, group H | 4 | F-151 |
| X-34 A stray file and a link in Codex's copy | Part 2, group H | 5 | F-154, F-155, F-160 |
| X-35 A second plugin with hooks | Part 2, group H | 5 | F-056, F-159 |
| X-36 A changed hook declaration | Part 2, group H | 4 | F-152, F-160 |
| X-37 A skill changed inside the hooks plugin | Part 2, group H | 8 | F-024, F-043, F-150 |
| C-29 A missing hook binary | Part 2, group I | 3 | F-050, F-104 |
| C-30 A chat with hooks switched off | Part 2, group I | 4 | F-024, F-043 |
| C-31 A chat left open | Part 2, group I | 3 | F-044, F-045 |
| C-32 A changed hooks file | Part 2, group I | 3 | F-103 |
| C-41 Read-only reports | Part 2, group I | 3 | F-057 |
| C-31b The open chat, after a new binary | Part 2, group I | 2 | F-044 |
| X-27 A missing hook binary | Part 2, group I | 3 | F-050 |
| X-38 Read-only reports | Part 2, group I | 3 | F-057 |
| X-48 Reports with the plugin switched off | Part 2, group I | 3 | F-057 |
| J-01 A scratch repository again | Part 2, group J | 14 | F-182 |
| S0-02 Device management | Part 2, group J | 2 | F-202 |
| S0-09 The machine's git checks | Part 2, group J | 5 | F-185, F-186, F-187, F-188 |
| S0-11 The scheduled job | Part 2, group J | 15 | F-180, F-181, F-189, F-190, F-200, F-201, F-203, F-204 |
| S0-12 The server's host key, and reads with prompts off | Part 2, group J | 10 | F-178, F-179, F-180 |
| S0-13 The scheduled job's push | Part 2, group J | 8 | F-175, F-180, F-182, F-191, F-201 |
| S0-15 The deploy-key commands | Part 2, group J | 2 | F-182 |
| C-40 A job a hook starts, against the git server | Part 2, group J | 2 | F-031, F-189, F-190 |
| X-30 A job a hook starts, against the git server | Part 2, group J | 2 | F-031, F-189, F-190 |
| J-02 Delete the scratch repository again | Part 2, group J | 3 | none |
| X-49 The scheduled job's times | Part 2, group J | 2 | F-200 |
| S0-14 A sign-in under another HOME | Part 2, group K | 10 | F-073, F-074 |
| C-35 The environment a hook gets, and another config folder | Part 2, group K | 10 | F-023, F-028, F-029 |
| X-25 The environment a hook gets, and another Codex home | Part 2, group K | 10 | F-023, F-028, F-029 |
| X-45 Voice call two: a long stretch, ended by quitting the app | Part 2, group L | 22 | F-141, F-146, F-147, F-148, F-149 |
| X-46b After the second call | Part 2, group L | 3 | F-146, F-147, F-149 |
| C-06 An edit on another branch, with a word from the pre-tool hook | Part 2, group M | 4 | F-021, F-081, F-089 |
| C-07 Parallel edits | Part 2, group M | 3 | F-048, F-081 |
| C-08 Two reads, and a word from an async hook | Part 2, group M | 5 | F-082, F-090 |
| C-33 A rules file with a path pattern | Part 2, group M | 5 | F-094 |
| C-34 A note for the host's own memory | Part 2, group M | 3 | F-087 |
| X-09 Parallel edits | Part 2, group M | 3 | F-048 |
| S0-16 A forced push with a lease | Part 2, group M | 5 | F-183 |
| E-01 Remove the scratch plugins | Part 2, group N | 5 | F-056 |
| E-05 Take everything down | Part 2, group N | 15 | none |
