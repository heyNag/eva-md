// Package spec holds the checks on eva.md's specs and decision records
// (plan decision 15). On 2026-11-14 v1's docs/design.md, docs/verification.md
// and docs/decisions.md were split, word for word, into per-component specs
// in docs/spec/ and dated decision records in docs/decisions/, and the three
// files became short indexes. Its tests, which nothing else imports, check
// the repository:
//
//   - No word changed. A block of a source opens with a comment naming the
//     source and its order there, from 001, and ends at the next such
//     comment or at the comment that closes blocks. Each source's blocks,
//     in order, with comments, link targets and tokens made only of '#'
//     removed and white space collapsed, hash to the value the test holds,
//     computed the same way from the source at the split's base commit.
//   - Every file of docs/spec/ and docs/decisions/, and the three indexes,
//     is at most 24,576 bytes, with no line over 400 characters but a table
//     row, and none over 2,000.
//   - docs/design.md and docs/decisions.md name every heading their old
//     text had, and docs/verification.md every heading and bold bullet
//     title, against the file that holds it. docs/spec/INDEX.md lists
//     exactly the specs present, each with the component, paths and status
//     its header gives, and docs/decisions.md exactly the records present.
//     Every path they name exists.
//   - A record's name, title, fields and date agree, and every relative
//     link outside code, in the specs, the records and the indexes,
//     resolves, its anchor included.
//
// The package builds nothing into eva-md: its code is in its tests.
package spec
