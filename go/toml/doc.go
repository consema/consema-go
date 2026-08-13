// Package toml implements the frozen `toml.1.0@1` language profile of the
// TOML format family (RFC 0001; consema-rs/crates/consema-toml). It forms lossless
// immutable document snapshots (byte-exact render, exhaustive
// token/trivia coverage), exposes the closed TOML native item model
// (scalar categories, tables, inline tables, dotted tables, arrays, and
// arrays-of-tables with their own identities — never JSON object/member
// types), executes the `toml.native-semantic-query@1` and
// `toml.lossless-syntax-query@1` query domains, projects native semantics
// onto the `toml.best-exact-core@1` target with provenance, materializes
// PortableValues into `toml.canonical-document` style snapshots, and
// commits the frozen TOML structural edit operations atomically with
// ChangeSet, SourcePatch, and untouched-byte evidence.
//
// The shared edit records (ChangeSet, EditPlan, UntouchedByteProof,
// AssociationPlacement, LosslessStructuralIndex) live in go/document
// since 0.16.0 G2.4; this package exposes them under its established
// local names as aliases and thin wrappers.
//
// The implementation is self-written Go over the TOML 1.0 grammar (RFC
// 0001 is the semantic authority; the shared conformance vectors and the
// Rust public API follow); no third-party TOML library is used
// (docs/go-implementation-plan.md §1.3). Completed public objects are
// logically immutable; queries accept a context.Context that is used for
// cancellation and deadlines only.
package toml
