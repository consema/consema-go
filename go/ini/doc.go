// Package ini implements lossless INI documents under three explicit,
// incompatible profiles (RFC 0009; consema-rs/crates/consema-ini). The caller selects
// exactly one profile before formation; the implementation never guesses a
// dialect from the extension or tries multiple profiles in sequence.
//
// The three frozen profiles are:
//
//	ini.portable@1             — conservative ASCII exchange subset
//	ini.windows@1              — deterministic Windows profile-string surface
//	ini.python-configparser@1  — Python 3.14 ConfigParser default formation
//
// The profiles share bounded source decoding, physical-line scanning,
// immutable snapshot identity, lossless coverage, transaction, proof, and
// patch infrastructure. They do not share accepted encoding, delimiter,
// comment, continuation, case-equivalence, quote, duplicate, or canonical
// generation rules.
//
// A Document retains the exact source bytes (render is byte-for-byte
// identity), ordered physical and logical lines, section/entry occurrences
// with original spelling and profile comparison names, value presence,
// quote and continuation facts, duplicate/case-collision groups without
// collapsing occurrences, ordered diagnostics, and exhaustive non-
// overlapping syntax pieces over the raw source. Recovered documents
// remain fully queryable but never project, materialize, or commit.
//
// The package mirrors the public capability face of consema-rs/crates/consema-ini:
// formation with explicit encoding selection (UTF-8, BOM-detected UTF-16LE,
// or a caller-selected Windows code page), native and lossless-syntax
// queries over validated protocol query definitions, best-exact and
// explicit-Object projections with provenance, canonical materialization
// for all three profiles, the eight versioned edit operations with
// atomic commit, dry-run plans, replayable patches, and untouched-byte
// proofs, and the frozen eight-operation registry shared by every profile.
// All failures are typed errors carrying the RFC 0016 §6 Code() contract
// with registered codes. The shared edit records (ChangeSet, EditPlan,
// UntouchedByteProof, AssociationPlacement, LosslessStructuralIndex)
// live in go/document since 0.16.0 G2.4; this package exposes them under
// its established local names as aliases and thin wrappers.
package ini
