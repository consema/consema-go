// Package consema is the Go SDK facade root: the common opaque `Document`
// union, the additive `Registry` surface (families, profiles, query
// domains, operation registries), and the `Convert*` two-stage
// composition (milestone 0.15.0 G1.4; RFC 0016 §3.2; crates/consema
// facade; docs/go-implementation-plan.md §2.2).
//
// The package maps the public capability face of the Rust facade
// (`crates/consema/src/lib.rs` and `conversion.rs`):
//
//   - `Document` is the common snapshot handle over the format documents.
//     Format access is only possible through the typed adapters
//     (`AsJSON`, `AsTOML`); all returned facts are immutable snapshot
//     facts. The union is additive: the JSON and TOML families land with
//     G1.4, the remaining families (yaml/ini/properties/xml/plist/hcl)
//     land with 0.16.0-0.18.0 without changing this type.
//   - `Families`/`Profiles`/`QueryDomains`/`OperationRegistry` expose the
//     registry surface of RFC 0015 §6.2 (8 families / 16 profiles / 21
//     query domains / 16 operation registries), mirroring the Rust
//     facade's `registry` module. The inventory is the declared
//     capability set of the Feature-Complete Manifest (the Go starting
//     point, roadmap §15.7 line 1445); everything the Go packages can
//     derive is derived from them — family ids, profile ids, and
//     operation registries for json/toml come from go/json and go/toml
//     themselves, and the drift-guard tests assert that derivation
//     (Rust facade registry tests precedent). The registry content of
//     the not-yet-implemented families is declared as capability facts
//     only and lands with their format milestones.
//   - `ConvertJSON`/`ConvertTOML` compose one format-owned projection and
//     the requested target materializer, retaining the intermediate
//     portable value, both provenance directions, and the two-stage
//     report (`crates/consema/src/conversion.rs`). The composition never
//     invents a cross-format convention: the projection target, the
//     materialization request, the mapping policy, and the
//     representability policy are all explicit caller choices; loss
//     without an authorizing policy fails atomically
//     (`core.conversion.unauthorized-loss@1`); a failure never returns a
//     partial target document.
//
// # Cross-family discipline
//
// Cross-family composition (convert) lives in this package only (RFC 0016
// §3.2 line 108); go/json and go/toml never import this package, and no
// package imports a sibling format package's private internals. The root
// package itself depends only on the public APIs of go/document,
// go/json, go/toml, go/core, and go/protocol, all of which are
// independent implementations of the language-neutral contracts
// (RFC 0016 §1.1; stdlib-only).
//
// # Known structural debt (recorded, not fixed in G1.4)
//
// go/json and go/toml each define the same-family edit surface types
// (ChangeSet, EditPlan, UntouchedByteProof, LosslessStructuralIndex,
// AssociationPlacement, and the format operation registries) with
// identical roles but package-local shapes. G1.4 deliberately does not
// lift them into a shared package — the lift would break the two newly
// landed packages — and the root package exposes no merged edit types:
// the Document union and the Registry surface use composition, never
// type merging. The completion path is milestone 0.16.0 G2.4 ("全操作
// 补齐"), where the shared types land and the family packages migrate.
//
// # context.Context
//
// context.Context carries cancellation and deadlines only, never
// business parameters (docs/go-implementation-plan.md §21.2 line 1827).
// Only the JSON family parse entry is cancellation-capable; the facade
// parse entries pass cancellation through where the family supports it.
package consema
