// Package properties implements the Consema Java Properties family for Go
// (RFC 0010): the frozen Reader and Latin-1 profiles over one shared
// lossless natural/logical-line pipeline with exact Java UTF-16 semantics.
//
// The package mirrors the capability face of crates/consema-properties
// (docs/go-implementation-plan.md §2.3 G2.3): explicit profile and
// encoding selection (never locale or platform guessing), byte-exact
// unmodified rendering, the native duplicate-preserving document view with
// exact JavaString keys/values (unpaired surrogates are native content and
// are never silently replaced), native and lossless-syntax query domains,
// best-exact EntryMapping and explicit unique-Object projection with
// atomic unpaired-surrogate failure, canonical Reader/Latin-1
// materialization with exact closure, the five frozen structural edit
// operations with replayable patches and untouched-byte proofs, and the
// RFC 0010 security boundaries (no defaults chains, no application table
// collapse, no encoding auto-detection).
//
// The parser is a self-written standard-library implementation of the Java
// Properties line grammar (docs/go-implementation-plan.md §1.3 rejects
// every third-party Properties base). Public records are snapshot-bound
// handles over an immutable Document; the typed errors implement the
// RFC 0016 §6 Code() contract, and error text is human presentation only
// and never participates in conformance comparison.
package properties
