// Package protocol implements the language-neutral Consema protocol layer
// for Go (RFC 0016 §3.2; docs/go-implementation-plan.md §2.1 G0.3). It maps
// consema-rs/crates/consema-protocol: the frozen contract registry (semantic-model v7,
// 41 contracts), the public error-code registry (v7, 187 codes), Diagnostic
// construction validation, Capability/Profile descriptors, QueryDefinition
// validation and binding, the three CLI machine-protocol records of RFC 0015
// (CliOutputMessage, BatchPlanMessage, BatchResultMessage), and the
// ClassifyErrorCode exit-class table.
//
// The package is standard-library only (plan §1.3); it imports go/core for
// the value model and PVCE/1, and nothing else.
//
// # Value model boundary
//
// All records are fixed-field trees over the closed fifteen-kind go/core
// value model (RFC 0016 §4.1; 配置内容统一处理标准与 Rust 参考实现.md §10:
// Null, Boolean, Integer, Decimal, BinaryFloat32, BinaryFloat64, String,
// Bytes, Date, Time, LocalDateTime, OffsetDateTime, Sequence, Object,
// EntryMapping), matching the Rust core kinds exactly. Only the Rust
// ExtendedValue (tag 0x7f) has no Go representation. Reachable-code
// differences are documented at each use:
//
//   - DecodePVCE rejects extended records through go/core's
//     ErrUnknownCoreTag, reported as ProtocolErrorKindInvalidPvce (the Rust
//     decoder has ExtendedValue and an ExpectedCoreValue/NestedExtendedValue
//     classification).
//   - The core.source-patch@2 record nested in core.batch-plan@1 carries
//     Bytes leaves (replacement original/replacement content). Both
//     BatchPlanMessage codec paths — FromJSON/ToJSON (canonical tagged JSON,
//     the primary machine transport of RFC 0015 §3.2) and
//     FromValue/ToValue/PVCE — carry the replacement bytes with full
//     fidelity, byte-identical to the Rust value-level codec. See cli.go
//     for the full note.
//
// # Error classification
//
// Every typed error in this package implements the RFC 0016 §6 Code()
// contract: Code returns the frozen registered code, and error text is human
// presentation only (roadmap §16.1: Go error text never participates in
// conformance comparison). ClassifyErrorCode is implemented once here and is
// the only exit-class mapping; SDK packages never classify.
package protocol
