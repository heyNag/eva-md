package inboxv1

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/ledgertest"
)

// The v1 backend keeps the reader's contract, as the fake does: on the
// fixture inbox, on a session kept in two folders beside two that share
// their first 8 characters, and on 200 generated inboxes.
func TestBackendKeepsTheReadersContract(t *testing.T) {
	ctx := context.Background()
	if err := ledgertest.Check(ctx, Backend{Root: fixtureInbox(t)}); err != nil {
		t.Fatalf("the fixture inbox: %v", err)
	}

	root := newInbox(t)
	for _, project := range []string{"one", "two"} {
		add(t, root, inbox.Entry{Host: "codex", Session: "11112222-aaaa", Project: project, Role: "user", Kind: "prompt", Text: "in " + project, Origin: "hook"})
	}
	add(t, root, inbox.Entry{Host: "codex", Session: "11112222-aaaa", Project: "one", Role: "assistant", Kind: "reply", Text: "one: second", Origin: "hook"})
	appendRaw(t, root, "two", "codex-11112222-aaaa.jsonl", "not a json line\n")
	add(t, root, inbox.Entry{Host: "codex", Session: "33334444", Project: "one", Role: "user", Kind: "prompt", Text: "first", Origin: "hook"})
	add(t, root, inbox.Entry{Host: "codex", Session: "33334444-bbbb", Project: "one", Role: "user", Kind: "prompt", Text: "second", Origin: "hook"})
	b := Backend{Root: root}
	if _, _, err := ledger.Resolve(ctx, b, "e:11112222-aaaa#1"); !errors.Is(err, ledger.ErrAmbiguous) {
		t.Fatalf("the inbox does not hold a reference Check must allow to be ambiguous: %v", err)
	}
	if err := ledgertest.Check(ctx, b); err != nil {
		t.Fatalf("a session in two folders: %v", err)
	}

	const inboxes = 200
	for seed := uint64(1); seed <= inboxes; seed++ {
		root := newInbox(t)
		r := rand.New(rand.NewPCG(seed, 0xc0f0))
		for i := range 1 + r.IntN(3) {
			generate(t, root, r, seed*10+uint64(i))
		}
		if err := ledgertest.Check(ctx, Backend{Root: root}); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
	}
}
