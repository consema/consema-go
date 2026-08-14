// Package consema is the Go SDK facade root: the common opaque `Document`
// union, the additive `Registry` surface (families, profiles, query
// domains, operation registries), and the `Convert*` two-stage
// composition (milestone 0.15.0 G1.4; RFC 0016 §3.2; consema-rs/consema
// facade; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.2).
//
// The package maps the public capability face of the Rust facade
// (`consema-rs/consema/src/lib.rs` and `conversion.rs`):
//
//   - `Document` is the common snapshot handle over the format documents.
//     Format access is only possible through the typed adapters (`AsJSON`,
//     `AsTOML`, `AsYAML`, `AsINI`, `AsProperties`, `AsXML`, `AsPlist`,
//     `AsHCL`); all returned facts are immutable snapshot facts. The
//     union is additive: JSON and TOML landed with G1.4, the remaining
//     families (yaml/ini/properties/xml/plist/hcl) landed with
//     0.16.0-0.18.0 without changing this type.
//   - `Families`/`Profiles`/`QueryDomains`/`OperationRegistry` expose the
//     registry surface of RFC 0015 §6.2 (8 families / 16 profiles / 21
//     query domains / 16 operation registries), mirroring the Rust
//     facade's `registry` module. The inventory is the declared
//     capability set of the Feature-Complete Manifest (the Go starting
//     point, roadmap §15.7「Feature-Complete Manifest」「Go 以该 manifest
//     为起点」); everything the Go packages can
//     derive is derived from them — family ids, profile ids, and
//     operation registries for json/toml come from go/json and go/toml
//     themselves, and the drift-guard tests assert that derivation
//     (Rust facade registry tests precedent). All eight families are
//     implemented (0.15.0-0.18.0), so the registry content is derived
//     from the backend facts and drift-guarded (G056, adversarial audit
//     2026-08-13 — the "not-yet-implemented families ... lands with
//     their format milestones" note was stale).
//   - The eight `Convert*` entries (`ConvertJSON`/`ConvertTOML`/
//     `ConvertYAML`/`ConvertINI`/`ConvertProperties`/`ConvertXML`/
//     `ConvertPlist`/`ConvertHCL`) compose one format-owned projection
//     and the requested target materializer, retaining the intermediate
//     portable value, both provenance directions, and the two-stage
//     report (`consema-rs/consema/src/conversion.rs`). The composition never
//     invents a cross-format convention: the projection target, the
//     materialization request, the mapping policy, and the
//     representability policy are all explicit caller choices; loss
//     without an authorizing policy fails atomically
//     (`core.conversion.unauthorized-loss@1`); a failure never returns a
//     partial target document.
//   - The cross-family edit surface (0.18.0 G4.3; RFC 0016 §5.3, RFC 0004):
//     `PlanEdit`/`CommitEdit` dispatch one `EditTransaction` to the owning
//     family's edit API and close the shared artifacts
//     (`document.ChangeSet`, `document.EditPlan`,
//     `document.UntouchedByteProof`, `document.SourcePatch`);
//     `BatchPlanner`/`ApplyPlanFile` close the `core.batch-plan@1` /
//     `core.batch-result@1` protocol records with the base-digest and
//     original-bytes dual preconditions (RFC 0015 §8-§9);
//     `ChangeSetMessageFromDocument` externalizes a committed change set
//     with caller-stable locators; `OrderedCursor`/`PortableCursor`
//     provide the language-neutral query cursor terminal semantics
//     (Completed/Cancelled/Failed).
//
// # Cross-family discipline
//
// Cross-family composition (convert) lives in this package only (RFC 0016
// §3.2 — the convert-dispatch surface is root-package-only, per the
// section's constraints bullet; wave-4 R40, 2026-08-15: the old "line
// 108" reference pointed at the package-topology table's graph row and
// was dropped — the section is the anchor, line numbers may drift);
// go/json and go/toml never import this package, and no
// package imports a sibling format package's private internals. The root
// package itself depends only on the public APIs of go/document,
// go/json, go/toml, go/core, and go/protocol, all of which are
// independent implementations of the language-neutral contracts
// (RFC 0016 §1.1; stdlib-only).
//
// # Known structural debt (cleared 2026-08-08, G2.4)
//
// The same-family edit surface types (ChangeSet, EditPlan,
// UntouchedByteProof, LosslessStructuralIndex, AssociationPlacement, and
// the format operation registries) were promoted into go/document and the
// family packages migrated to the shared shapes in G2.4, including the
// seven drift items unified against the Rust reference (git records:
// "Deliver Go 0.16.0 G2.1-G2.3"). The Document union and the Registry
// surface still use composition, never type merging.
//
// # context.Context
//
// context.Context carries cancellation and deadlines only, never
// business parameters (roadmap §21.2; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.6 G5.5). The JSON, XML, and HCL family parse entries are
// cancellation-capable; the facade parse entries pass cancellation
// through where the family supports it (TOML, YAML, INI, Properties,
// and plist mirror the Rust facade, which has no cancellation).
package consema
