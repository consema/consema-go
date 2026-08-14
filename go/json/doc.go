// Package json implements the Consema JSON family: lossless
// `json.strict@1`, `jsonc.bounded@1`, and `json5.standard@1` documents with
// native/syntax queries, projection, materialization, and structural edits
// (consema-rs/consema-json; RFC 0016 §5; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.2
// G1.2). The language-neutral behavior mirrors the Rust crate's public
// semantics; the implementation is Go-idiomatic and standard-library only
// (no FFI, no third-party dependencies).
//
// The package builds on go/document for raw source snapshots, spans, node
// identities, parse limits, materialization requests, and — since 0.16.0
// G2.4 — the shared edit records (change sets, edit plans, untouched-byte
// proofs, lossless structural indexes, association placement). This
// package keeps the format-local names of its public surface as aliases
// and thin wrappers over the go/document records; the format operation
// registries remain format-local because the operation descriptors are
// family-specific facts.
//
// Cancellation follows the SDK convention (roadmap §21.2「Go API」
// 「`context.Context` 只用于取消/deadline，不隐藏业务参数」):
// context.Context carries cancellation and deadlines only, never business
// parameters.
package json
