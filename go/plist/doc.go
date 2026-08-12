// Package plist implements the lossless `plist.xml@1` and `plist.binary@1`
// documents under the RFC 0013 boundary (milestone 0.17.0 G3.2; docs/go-
// implementation-plan.md §2.4).
//
// The two profiles share one native value model and the immutable-snapshot,
// recovery, transaction, proof, and patch infrastructure. They do not share
// syntax: the XML profile is a text tree of tags, while the binary profile
// is an object table with offset-table and trailer facts and has no text,
// whitespace, or token fiction (RFC 0013 §1; hard gate 1).
//
// The profile is selected by the caller before formation; neither the
// `bplist00` magic number nor a `.plist` extension selects semantics. The
// two representations are format identities, not dialects of one format:
// Apple serializes the same value space to both representations, and
// Consema preserves that value identity (RFC 0013 §7). Cross-representation
// conversion is a first-class transform with a reparse closure and a
// representation-change report; a native fact the target representation
// cannot express fails the whole conversion atomically (hard gate 3).
//
// The package surface covers formation (Parse), the shared native value
// model (PlistDocument and the arena), the three query domains (native,
// lossless syntax, binary structure), projection (`plist.value-tree@1` and
// `plist.projection.require-object@1`), materialization
// (`plist.xml-canonical@1` / `plist.binary-canonical@1`), the six
// snapshot-bound structural edits (RFC 0013 §11), and the per-profile
// operation registry. Formation is side-effect free: it never fetches the
// Apple DTD or any other URI, resolves a UID or archive key path, evaluates
// an expression, reads environment or locale state, writes files, or
// invokes application code.
//
// The package depends only on the Go standard library and on the shared
// go/core, go/document, and go/protocol packages. The `plist.*` diagnostic
// codes are not part of the consema-protocol core error registry (RFC 0013
// §12) and are carried by document diagnostics directly; error text is
// human presentation only and never participates in conformance comparison.
package plist
