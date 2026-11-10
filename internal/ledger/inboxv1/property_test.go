package inboxv1

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// v1Pairs are the role and kind pairs v1 writes, with the fixed texts of
// its events.
var v1Pairs = [][2]string{{"user", "prompt"}, {"assistant", "reply"}, {"tool", "tool"}, {"plan", "plan"}, {"host", "host_summary"}}

var v1Events = []string{"session_start: startup", "session_start: resume", "session_end", "compact", "model_switch", "interrupt",
	"stop_failure: rate_limit", "host_prompt", "gap: missing final reply", "gap: interrupted append", "off_record", "on_record"}

// generated is a v1 session file the property test wrote, and what it
// knows of each line.
type generated struct {
	folder, file string
	tokens       map[string]bool // tokens of lines said off the record or forged
	lines        int             // complete lines
}

// generate writes one v1 session file from r: entries of every kind v1
// writes, entries repeated as the hook and recovery repeat them, entries
// forged from another session or folder, lines that do not decode, a line
// over v1's limit, pauses and resumes placed anywhere and forged ones, and
// sometimes an unfinished last line. Every line that is off the record by
// v1's walker's rule, or forged, carries a token the reader must never
// return, in every field.
func generate(t *testing.T, root string, r *rand.Rand, seed uint64) generated {
	t.Helper()
	host := []string{"claude-code", "codex"}[r.IntN(2)]
	session := fmt.Sprintf("gen-%d", seed)
	project := []string{"general", "acme-app"}[r.IntN(2)]
	file, _ := inbox.FileName(host, session)
	g := generated{folder: project, file: file, tokens: map[string]bool{}}
	var b strings.Builder
	off := false
	var written []inbox.Entry // entries written on the record, which may be written again
	long := false
	n := 1 + r.IntN(60)
	for i := 0; i < n; i++ {
		token := fmt.Sprintf("tok%d-%d", seed, i)
		e := inbox.Entry{V: 1, TS: "2026-11-02T10:00:00Z", Host: host, Session: session, Project: project, Origin: []string{"hook", "catchup"}[r.IntN(2)]}
		e.Turn = []string{"", "t1", "t2", "t3"}[r.IntN(4)]
		if r.IntN(5) == 0 {
			e.Agent = "agent-1"
		}
		switch roll := r.IntN(20); {
		case roll < 9:
			pair := v1Pairs[r.IntN(len(v1Pairs))]
			e.Role, e.Kind = pair[0], pair[1]
			e.Text = []string{"same words", "other words", token + " words"}[r.IntN(3)]
			if r.IntN(10) == 0 {
				e.Trunc = 1 + r.IntN(5000)
			}
			if r.IntN(15) == 0 {
				e.Role = "odd" // a role v1 never writes with this kind
			}
		case roll < 12:
			e.Role, e.Kind, e.Text = "event", "event", v1Events[r.IntN(len(v1Events))]
		case roll < 14:
			// A pause or a resume, the walker's markers.
			e.Role, e.Kind, e.Text = "event", "event", []string{"off_record", "on_record"}[r.IntN(2)]
		case roll < 15 && len(written) > 0:
			// The same entry again, as the hook and recovery both store it.
			e = written[r.IntN(len(written))]
		case roll < 17:
			// An entry forged from another session, folder or host.
			e.Role, e.Kind, e.Text = "user", "prompt", token+" forged"
			if r.IntN(3) == 0 {
				e.Role, e.Kind, e.Text = "event", "event", []string{"off_record", "on_record"}[r.IntN(2)]
			}
			switch r.IntN(3) {
			case 0:
				e.Session = "elsewhere"
			case 1:
				e.Project = "another-project"
			default:
				e.Host = "other-host"
			}
			e.Turn, e.Agent, e.Branch = token, token, token
			g.tokens[token] = true
			b.WriteString(line(t, e))
			g.lines++
			continue
		case roll < 18:
			b.WriteString(`{"v":1,"host":"` + host + `","session":"` + session + `","text":"` + token + " cut short\n")
			g.tokens[token] = true
			g.lines++
			continue
		case roll < 19 && seed%16 == 0 && !long:
			// A line over v1's limit, which every v1 reader passes over.
			e.Role, e.Kind, e.Text = "assistant", "reply", token+strings.Repeat("x", inbox.MaxLine)
			g.tokens[token] = true
			long = true
			b.WriteString(line(t, e))
			g.lines++
			continue
		default:
			e.Role, e.Kind, e.Text = "assistant", "reply", token+" plain"
		}
		trusted := inbox.Trusted(e, project, file, "")
		marker := e.Kind == "event" && (e.Text == "off_record" || e.Text == "on_record")
		if trusted && marker {
			off = e.Text == "off_record"
		}
		if off && !marker {
			// Said off the record: every field carries the token.
			if !strings.Contains(e.Text, token) && e.Kind != "event" {
				e.Text = token + " " + e.Text
			}
			e.Turn, e.Branch, e.Cwd, e.Model = token, token, token, token
			if e.Agent != "" {
				e.Agent = token
			}
			g.tokens[token] = true
		} else {
			written = append(written, e)
		}
		b.WriteString(line(t, e))
		g.lines++
	}
	if r.IntN(4) == 0 {
		token := fmt.Sprintf("tok%d-tail", seed)
		b.WriteString(`{"v":1,"host":"` + host + `","session":"` + session + `","role":"user","kind":"prompt","text":"` + token)
		g.tokens[token] = true
	}
	if err := os.MkdirAll(filepath.Join(root, project), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, project, file), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return g
}

// Over generated v1 inboxes the backend yields one event for each line,
// its seq the line's number and its text v1's for that line; it never
// files an event as typed, never returns what was said off the record or
// forged into the file, under every placement of pauses and resumes, and
// marks and counts copies without dropping any line.
func TestGeneratedInboxes(t *testing.T) {
	seeds := 400
	if testing.Short() {
		seeds = 60
	}
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		root := newInbox(t)
		r := rand.New(rand.NewPCG(seed, 0x1e5510))
		var gens []generated
		for range 1 + r.IntN(3) {
			gens = append(gens, generate(t, root, r, seed*10+uint64(len(gens))))
		}
		keys := sessions(t, root)
		if len(keys) != len(gens) {
			t.Fatalf("seed %d: %d sessions listed for %d files", seed, len(keys), len(gens))
		}
		for _, g := range gens {
			var k = keysByFile(keys)[g.file]
			if k.File == "" {
				t.Fatalf("seed %d: %s is not listed", seed, g.file)
			}
			s, events := checkV1(t, root, k)
			if len(events) != g.lines {
				t.Fatalf("seed %d: %d events for %d lines", seed, len(events), g.lines)
			}
			// The first event of a prompt or a reply with a turn id is the
			// one its later copies name: same kind, turn, subagent and text.
			type same struct {
				kind             event.Kind
				tid, agent, text string
			}
			firsts := map[same]int64{}
			copies := 0
			for _, e := range events {
				var want int64
				if (e.Kind == event.KindPrompt || e.Kind == event.KindReply) && e.TID != "" && !e.Has(event.MarkName) {
					key := same{e.Kind, e.TID, e.Agent, e.Text}
					if first, ok := firsts[key]; ok {
						want = first
						copies++
					} else {
						firsts[key] = e.Seq
					}
				}
				if e.Copy != want {
					t.Fatalf("seed %d line %d: a copy of %d, want %d", seed, e.Seq, e.Copy, want)
				}
				b, err := event.Encode(e)
				if err != nil {
					t.Fatalf("seed %d line %d: %v", seed, e.Seq, err)
				}
				for token := range g.tokens {
					if strings.Contains(string(b), token+" ") || strings.Contains(string(b), token+`"`) || strings.Contains(string(b), token+"x") {
						t.Fatalf("seed %d line %d returns %s, which holds %s", seed, e.Seq, b, token)
					}
				}
			}
			if s.Copies != copies {
				t.Fatalf("seed %d: %d copies counted, want %d", seed, s.Copies, copies)
			}
			if s.Class == event.ClassChat {
				t.Fatalf("seed %d: the v1 reader classed a session as a person's chat", seed)
			}
		}
	}
}

func keysByFile(keys map[string]ledger.Key) map[string]ledger.Key {
	out := map[string]ledger.Key{}
	for _, k := range keys {
		out[k.File] = k
	}
	return out
}
