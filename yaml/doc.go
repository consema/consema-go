// Package yaml implements the Consema YAML family for Go (RFC 0007): the
// frozen YAML 1.2 Core and YAML 1.1 compatibility profiles over one shared
// lossless presentation pipeline.
//
// The package mirrors the capability face of crates/consema-yaml
// (docs/go-implementation-plan.md §2.3 G2.1): explicit profile selection
// (never dialect guessing), byte-exact unmodified rendering, the native
// representation view with anchors/aliases/tags preserved as facts (never
// implicitly expanded and never fabricated into PortableValue), native and
// lossless-syntax query domains, best-exact graph and value projection,
// canonical block/flow materialization, the eight frozen structural edit
// operations with anchor-safe rules, and the RFC 0007 security boundaries
// (no custom tag constructors, no alias expansion during parse, no
// network/filesystem/application access).
//
// The parser is a self-written standard-library implementation of the YAML
// 1.2.2 presentation grammar (docs/go-implementation-plan.md §1.3 rejects
// gopkg.in/yaml.v3 and every third-party YAML base). Backend event types
// are private; the public contract is the immutable Document snapshot, its
// snapshot-bound native handles, the query/projection/materialization/edit
// results, and the typed errors implementing the RFC 0016 §6 Code()
// contract.
package yaml
