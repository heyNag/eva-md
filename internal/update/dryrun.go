package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/inbox"
)

// Placeholders for what a real run passes and a dry run shows on its own.
const (
	rulesArg   = "<the rules below>"
	schemaArg  = "<the schema below>"
	runDirArg  = "<an empty run folder>"
	schemaFile = "<a file holding the schema below>"
	outputFile = "<a file for the answer>"
)

// DryRun prints what the next pass for a session would run, and runs
// nothing: the exact command, the names (never the values) of the
// variables the child would get, the rules, the input and the schema. It
// reads the inbox, the thread page, git and today's run log, and writes
// nothing, so two dry runs in a row print the same bytes. It runs while
// updates = false too, since it makes no call.
func (j Job) DryRun(ctx context.Context, host, id string, out io.Writer) error {
	name, err := fileName(host, id)
	if err != nil {
		return err
	}
	r, files, err := j.start(func(files []sessionFile) ([]sessionFile, error) {
		var keep []sessionFile
		for _, f := range files {
			if f.name == name {
				keep = append(keep, f)
			}
		}
		if len(keep) == 0 {
			return nil, ErrNoSession
		}
		return keep, nil
	})
	if err != nil {
		return err
	}
	// The job sets aside the chats captured before updates before anything
	// else (see Baseline), so a session it would set aside, or one it has
	// left, has no pass.
	set, left, aside, err := baselined(j.Dirs, j.now())
	if err != nil {
		return err
	}
	for _, f := range files {
		if !left[f.path] && set && !aside(f) {
			continue
		}
		if m, err := r.store.Mark(f.path); err == nil && m != (inbox.Mark{}) && !left[f.path] {
			continue
		}
		var line string
		switch err := whyLeft(r.store, f.path); {
		case err != nil:
			line = "%s:%s, a chat captured before updates, waits to be set aside: " + strings.TrimPrefix(err.Error(), "update: a chat from before updates that couldn't be set aside yet: ") + ". Nothing would be sent.\n"
		case left[f.path]:
			line = "%s:%s, a chat captured before updates, waits to be set aside: the next update tries to set it aside again. Nothing would be sent.\n"
		default:
			line = "The next update would set aside %s:%s, a chat captured before updates, so nothing would be sent.\n"
		}
		_, err := fmt.Fprintf(out, "eva-md update --dry-run: no call was made.\n"+line, host, id)
		return err
	}
	for _, f := range files {
		p, found, err := r.peek(ctx, f)
		if err != nil {
			return err
		}
		if found {
			return r.printPlan(out, f, p)
		}
	}
	_, err = fmt.Fprintf(out, "eva-md update --dry-run: no call was made.\nNothing is pending for %s:%s.\n", host, id)
	return err
}

// peek plans the next pass of a session file, if it has one.
func (r *run) peek(ctx context.Context, f sessionFile) (plan, bool, error) {
	b, err := openBacklog(r.store, f.path)
	if err != nil {
		return plan{}, false, err
	}
	defer b.close()
	if pending, err := b.refill(); err != nil || !pending {
		return plan{}, false, err
	}
	p, err := r.plan(ctx, &chatFile{file: f, b: b, commits: map[string][]string{}, heads: map[string]string{}})
	return p, p.kind != planNone, err
}

// printPlan writes a dry run's report of a planned pass.
func (r *run) printPlan(out io.Writer, f sessionFile, p plan) error {
	var b strings.Builder
	b.WriteString("eva-md update --dry-run: no call was made.\n")
	fmt.Fprintf(&b, "Session: %s:%s, inbox file %s/%s\n", p.rec.Host, p.rec.Session, f.dir, f.name)
	switch p.kind {
	case planExcluded:
		b.WriteString("The config excludes the project, so its entries are left as they are.\n")
	case planWait:
		if p.rec.Thread == "" {
			b.WriteString("The last prompt waits for its reply, so nothing would be sent yet.\n")
			break
		}
		fmt.Fprintf(&b, "Branch %s: the next group is under one exchange or 600 characters and waits for more entries.\n", p.rec.Thread)
	case planSkip:
		fmt.Fprintf(&b, "Branch %s: the next pass would move the watermark to byte %d without a call: skipped (%s).\n", p.rec.Thread, p.end, p.reason)
	default:
		if err := r.writeCall(&b, p); err != nil {
			return err
		}
	}
	_, err := io.WriteString(out, b.String())
	return err
}

// writeCall writes the command, environment, rules, input and schema of a
// planned call.
func (r *run) writeCall(b *strings.Builder, p plan) error {
	effort, err := r.effort(p.batch.Host, p.model)
	if err != nil {
		return err
	}
	page := "existing"
	if !p.thread.exists {
		page = "new"
		if p.parent != "" {
			page += ", parent thread " + p.parent
		}
	}
	first, last := p.batch.Entries[0].Line, p.batch.Entries[len(p.batch.Entries)-1].Line
	fmt.Fprintf(b, "Thread page: %s (%s)\n", p.thread.id, page)
	fmt.Fprintf(b, "Entries: %d, lines %d to %d; the watermark would move to byte %d", len(p.batch.Entries), first, last, p.end)
	if p.more {
		b.WriteString(", and the next pass continues this group")
	}
	b.WriteString("\n")
	model := p.model
	if model == "" {
		model = "the host default, as the chat's model is unknown"
	}
	fmt.Fprintf(b, "Model: %s; effort: %s\n", model, effort)
	r.writeCommand(b, p, effort)
	b.WriteString("\n== Rules ==\n" + Rules + "\n")
	b.WriteString("\n== Input ==\n" + string(p.batch.Input))
	if !strings.HasSuffix(string(p.batch.Input), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n== Schema ==\n" + string(Schema(p.thread.page)) + "\n")
	return nil
}

// writeCommand writes the command line the runner would start, by the CLI
// path install recorded when a run would use it, with placeholders for what
// it passes from files, and the environment keys.
func (r *run) writeCommand(b *strings.Builder, p plan, effort string) {
	req := Request{Host: p.batch.Host, Rules: rulesArg, Schema: []byte(schemaArg), RunDir: runDirArg, IgnoreUserConfig: r.j.Config.CodexIgnoreUserConfig}
	t := try{model: p.model, effort: effort}
	cli, args, stdin := cliFor("claude", r.clis["claude"]), claudeArgs(req, t), "the input below"
	if p.batch.Host == HostCodex {
		cli, args, stdin = cliFor("codex", r.clis["codex"]), codexArgs(req, t, schemaFile, outputFile), "the rules below, a blank line, then the input below"
	}
	quoted := []string{shellQuote(cli)}
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	fmt.Fprintf(b, "Command, run in an empty folder with %s on stdin:\n  %s\n", stdin, strings.Join(quoted, " "))
	b.WriteString("Environment, names only:\n  " + strings.Join(envKeys(os.Environ()), " ") + "\n")
}

// envKeys are the names of the variables the child would get, sorted.
func envKeys(environ []string) []string {
	var keys []string
	for _, kv := range childEnv(environ) {
		key, _, _ := strings.Cut(kv, "=")
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// shellQuote quotes an argument for a shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:=@%+,-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
