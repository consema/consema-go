// Package document implements the immutable source-snapshot, structural
// location, formation-status, limits, materialization-request, and
// source-patch surface of the Go SDK (milestone 0.15.0 G1.1; RFC 0016 §3.2;
// docs/go-implementation-plan.md §2.2). It maps the public capability face
// of the Rust crate consema-document: SourceSnapshot, Span (byte offsets),
// NodeRef, ProfileId, FormationStatus (closed Complete/Recovered),
// ParseLimits, MaterializationRequest, SourcePatch, and — since 0.16.0
// G2.4 — the shared edit records the family packages used to define
// locally: AssociationPlacement, SourceEdit, NodeMapping, ChangeSet,
// EditPlan, EditPlanSourceId, UntouchedByteProof, StructuralPiece,
// StructuralPieceKind, and LosslessStructuralIndex (consema-document
// lib.rs, edit_plan.rs, untouched_proof.rs).
//
// The package is an independent implementation of the language-neutral
// contracts; it shares no code with the Rust crates (RFC 0016 §1.1 cgo
// ban). It depends only on the Go standard library and on go/protocol's
// registered record types (Diagnostic, EditOperationSummary,
// NodeMappingStatus) — the Go mapping of the Rust consema-core Diagnostic
// surface (RFC 0016 §6), mirroring consema-document's dependency on
// consema-core. The wire-facing records of go/protocol
// (records_source.go: core.source-snapshot@1/@2, core.source-patch@1/@2,
// core.source-encoding@1) carry the same facts in their transferable
// shapes; the two layers stay aligned by shared semantics, verified by
// the field-consistency tests in this package.
//
// Source encodings: UTF-8, UTF-16LE/BE, Latin-1, the frozen versioned
// Windows code pages (874, 932, 936, 949, 950, 1250-1258, 65001), and
// opaque binary. BOM interpretation is explicit (DetectUnicode /
// TreatAsContent). Single-byte page decoding is aligned with the Rust
// reference (encoding_rs 0.8.35, the exact version pinned by Cargo.toml),
// frozen 2026-08-07: the authority tables were captured from the
// wcp-authority scratch cargo project, which decodes every byte 0x00-0xFF
// of the nine pages through the exact Rust decode path
// (decode_to_string_without_replacement, one byte per call; provenance
// and regeneration notes in wcp_authority_test.go). C1 control positions
// decode to their U+00xx scalars, real mappings are exact, and Malformed
// positions fail the whole source with SourceErrorInvalidSequence at the
// byte offset, exactly as document source.rs reports them. CP932 decodes
// from the frozen Python-stdlib data shared with
// go/protocol/cp932_table.go; CP936/CP949/CP950 are recognized code pages
// whose full DBCS decoding tables land with a later milestone (their
// non-ASCII bytes are rejected, exactly as go/protocol rejects them
// today).
//
// Errors returned by this package are typed and implement the RFC 0016 §6
// Code() contract: *SourceError, *SourcePatchError, *LocationError,
// *SourcePatchRedactionError, *UntouchedByteProofError, and
// *EditPlanError. Error text is human presentation only and never
// participates in conformance comparison.
package document
