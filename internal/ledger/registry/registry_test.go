package registry_test

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/ledger/registry"
)

// reProjectID is store-format.md section 3's project id: p_ and 12
// lower-case Crockford characters.
var reProjectID = regexp.MustCompile(`^p_[0-9a-hjkmnp-tv-z]{12}$`)

func TestProjectIDVectors(t *testing.T) {
	for key, want := range map[string]string{
		"github.com__acme__orders": "p_c0476xchw35b",
		"local-scratch-9f2c1a":     "p_6pbe1q7fxb5h",
		"general":                  "p_1zne2van6sdc",
		"github.com/acme/orders":   "p_00gb3367a19r",
		"GitHub.com__acme__orders": "p_btxwm93qrxpc",
	} {
		got, ok := registry.ProjectID(key)
		if !ok || got != want {
			t.Errorf("ProjectID(%q) = %q, %v; want %q, true", key, got, ok, want)
		}
	}
}

func TestProjectIDOfNoKey(t *testing.T) {
	if got, ok := registry.ProjectID(""); ok || got != "" {
		t.Fatalf(`ProjectID("") = %q, %v; want "", false`, got, ok)
	}
}

// bits60 is the first 60 bits of SHA-256 of key, read one bit at a time,
// high first, as 12 characters of the alphabet: the rule written another
// way than ProjectID writes it.
func bits60(key string) string {
	const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	sum := sha256.Sum256([]byte(key))
	out := make([]byte, 12)
	for i := range out {
		v := 0
		for j := range 5 {
			bit := i*5 + j
			v = v<<1 | int(sum[bit/8]>>(7-bit%8)&1)
		}
		out[i] = alphabet[v]
	}
	return "p_" + string(out)
}

func TestProjectIDOverGeneratedKeys(t *testing.T) {
	seen := map[string]string{}
	for i := range 400 {
		key := fmt.Sprintf("github.com__made-up-%d__repo-%d", i, i*7919)
		if i%3 == 1 {
			key = fmt.Sprintf("local-folder%d-%06x", i, i*104729)
		}
		if i%3 == 2 {
			key = strings.Repeat("é ", i%17) + fmt.Sprint(i) // keys of any bytes are hashed as given
		}
		id, ok := registry.ProjectID(key)
		if !ok || !reProjectID.MatchString(id) {
			t.Fatalf("ProjectID(%q) = %q, %v: not a project id", key, id, ok)
		}
		if again, _ := registry.ProjectID(key); again != id {
			t.Fatalf("ProjectID(%q) gave %q, then %q", key, id, again)
		}
		if want := bits60(key); id != want {
			t.Fatalf("ProjectID(%q) = %q; its first 60 bits are %q", key, id, want)
		}
		if other, ok := seen[id]; ok {
			t.Fatalf("%q and %q give one id %s", key, other, id)
		}
		seen[id] = key
	}
}

// A key is hashed as given: a key that differs in case, in white space at
// either end, or in its separators, is another project.
func TestProjectIDDoesNotNormalize(t *testing.T) {
	base, _ := registry.ProjectID("github.com__acme__orders")
	for _, key := range []string{
		"GitHub.com__acme__orders",
		"GITHUB.COM__ACME__ORDERS",
		" github.com__acme__orders",
		"github.com__acme__orders\n",
		"github.com/acme/orders",
		"github.com__acme__orders.git",
	} {
		if id, _ := registry.ProjectID(key); id == base {
			t.Errorf("ProjectID(%q) is the id of github.com__acme__orders", key)
		}
	}
	general, _ := registry.ProjectID("general")
	for _, key := range []string{"General", " general", "general "} {
		if id, _ := registry.ProjectID(key); id == general {
			t.Errorf("ProjectID(%q) is the general area's id", key)
		}
	}
}

// The three kinds v1 knows are identity's own strings, so a v1 project's
// kind converts directly, and the general area's key is v1's.
func TestKindsAreIdentitys(t *testing.T) {
	for _, c := range []struct {
		ours registry.Kind
		v1   identity.Kind
	}{
		{registry.KindRepo, identity.KindRepo},
		{registry.KindLocal, identity.KindLocal},
		{registry.KindGeneral, identity.KindGeneral},
	} {
		if string(c.ours) != string(c.v1) || registry.Kind(c.v1) != c.ours {
			t.Errorf("the kind %q is %q in identity", c.ours, c.v1)
		}
	}
	if registry.KindFolder != "folder" {
		t.Errorf("KindFolder is %q", registry.KindFolder)
	}
	for _, k := range []registry.Kind{registry.KindRepo, registry.KindLocal, registry.KindGeneral, registry.KindFolder} {
		if !k.Known() {
			t.Errorf("%q is not known", k)
		}
	}
	for _, k := range []registry.Kind{"", "Repo", "remote", "folder "} {
		if k.Known() {
			t.Errorf("%q is known", k)
		}
	}
	if id, ok := registry.ProjectID(identity.GeneralKey); !ok || id != "p_1zne2van6sdc" {
		t.Errorf("ProjectID(identity.GeneralKey) = %q, %v", id, ok)
	}
}
