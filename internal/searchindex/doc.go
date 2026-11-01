// Package searchindex is eva-md search's index: a cache built from the
// redacted, on-record inbox that proposes which inbox lines may hold a
// search's terms, and how well they match. It holds the lines' lowercase
// words and where the lines are, never the lines. Search reads every line
// it proposes again from its file and matches it as it matches a line it
// scans, so the index never changes a hit's text, and scans whatever the
// index does not cover. But the index decides which lines are read, so in
// the files it covers a match inside a word, in text without spaces, or a
// rarer exact-case hit past the candidates read can be left out when other
// hits are found. A session file deleted or replaced has its words merged
// out at the next catch-up, and the segments that held them are deleted by
// the first catch-up PruneAge after. Deleting the index loses nothing,
// since eva-md index and the daily maintain build it again from the inbox.
// The hooks never read or write it.
//
// Under paths.Dirs.SearchIndex() it keeps a folder per inbox project folder,
// of the same name, holding manifest.json, which says how far each session
// file is indexed, the immutable segment files it names, seg-<16 hex>.idx,
// and the lock its writers take. Only the inbox's prompts, replies, tool and
// plan lines are indexed, never events, host summaries or anything said off
// the record; thread and system pages are not indexed at all.
package searchindex

import (
	"errors"
	"time"
)

const (
	// FormatVersion is the version of the manifest and the segment format.
	// A folder of another version counts as unindexed until a catch-up
	// builds it again.
	FormatVersion = 1
	// MaxToken is the longest token indexed, in bytes. A longer run of
	// letters and numbers, such as a base64 blob, is dropped, and a query
	// holding one is left to the scan.
	MaxToken = 64
	// MaxTerms is how many tokens a query may hold for the index to search
	// it; a longer query is left to the scan.
	MaxTerms = 32
	// MaxExpansions is how many dictionary terms a word of 3 or more
	// characters matches at most, those in the most documents.
	MaxExpansions = 256
	// BlockBytes is how much of the inbox a refresh indexes into one
	// segment, which bounds a refresh's memory.
	BlockBytes = 16 << 20
	// TopUpBytes is the most a search indexes for the next one, and TopUpMin
	// how much of the inbox in scope the index must miss before it does,
	// with TopUpReserve of the search's time left.
	TopUpBytes   = 16 << 20
	TopUpMin     = 4 << 20
	TopUpReserve = 1500 * time.Millisecond
	// PruneAge is how long a segment no manifest names any more, or a
	// temporary file, is kept, so a search that read the previous manifest
	// still opens its segments.
	PruneAge = 10 * time.Minute
	// MaxManifest is the largest manifest read.
	MaxManifest = 16 << 20
)

// K is how many of the best-scored candidates a search reads again from the
// inbox to judge them, for a search showing limit hits.
func K(limit int) int { return max(200, 8*limit) }

// ErrDamaged reports a manifest or segment that fails its checks. The
// folder then counts as unindexed, so search scans its files, until a
// catch-up builds it again.
var ErrDamaged = errors.New("searchindex: damaged index")
