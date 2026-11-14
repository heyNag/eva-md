// Package registry is the project registry's interface: the kinds of
// project, a project as the registry knows it, and the project id as a
// function of the key that first named it (store-format.md section 3).
// It is LG-22's first part and has no caller yet; its fake is in
// registrytest.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
)

// idAlphabet is lower-case Crockford base32, the store's id alphabet.
const idAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// ProjectID is the id of the project a key named: "p_" and the first 60
// bits of SHA-256 of the key's bytes, high first, as 12 characters of
// idAlphabet. The key is hashed as given, never trimmed, lower-cased or
// normalized, so the id is the same on every machine. An empty key has
// no id: it gives "" and false.
//
// The key of a project v1 knew is its key as v1's resolver writes it,
// which v1's inbox entries carry as project; the general area's is
// "general".
func ProjectID(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	sum := sha256.Sum256([]byte(key))
	v := binary.BigEndian.Uint64(sum[:8]) >> 4 // the first 60 bits
	out := make([]byte, 14)
	out[0], out[1] = 'p', '_'
	for i := 13; i >= 2; i-- {
		out[i] = idAlphabet[v&31]
		v >>= 5
	}
	return string(out), true
}

// Kind says how a project was identified.
type Kind string

const (
	KindRepo    Kind = "repo"    // a git repository with a remote
	KindLocal   Kind = "local"   // a git repository with no remote
	KindGeneral Kind = "general" // the general area, outside any project
	KindFolder  Kind = "folder"  // a folder outside git, as its own project
)

// Known reports whether k is one of the four kinds.
func (k Kind) Known() bool {
	switch k {
	case KindRepo, KindLocal, KindGeneral, KindFolder:
		return true
	}
	return false
}

// Project is a project as the registry knows it. ID is ProjectID(Keys[0]):
// a project keeps the id of the first key that named it, joined or not.
type Project struct {
	ID    string
	Kind  Kind
	Title string
	// Keys holds every key that has named the project, the first first.
	// For a project v1 knew they are its v1 keys, the record's
	// legacy_keys.
	Keys        []string
	Remotes     []string
	RootCommits []string
	// Roots are the project's local folders. They are never written to
	// the store.
	Roots []string
	// MergedInto is the id of the project this one was joined into, and
	// "" until a join. A joined project keeps its id.
	MergedInto string
	Related    []string
}

// Registry lists the projects eva-md knows.
type Registry interface {
	// Projects lists every project, joined ones too, sorted by id.
	Projects(ctx context.Context) ([]Project, error)
	// ByKey gives the project a key named, following no join. A key no
	// project was named by gives false and no error.
	ByKey(ctx context.Context, key string) (Project, bool, error)
}
