// Package xml implements the lossless `xml.1.0-safe@1` documents under the
// RFC 0012 boundary (milestone 0.17.0 G3.1; docs/go-implementation-plan.md
// §2.4; RFC 0012). It maps the public capability face of the Rust crate
// consema-xml: XmlProfile, XmlEncodingSelection, XmlParseLimits, the
// namespace-aware native document tree, lossless syntax coverage, native
// and lossless query execution, element-tree projection, canonical
// materialization, and the eight frozen structural edit operations.
//
// The Profile is selected before formation. A `.xml` extension does not
// authorize external I/O, schema lookup, DTD validation, or application
// mapping. Formation consumes one complete document entity supplied as
// bytes and never opens another entity, file, URI, network connection,
// registry, classpath, or catalog (RFC 0012 §1).
//
// The package is an independent implementation of the language-neutral
// contracts; it shares no code with the Rust crates and depends only on
// the Go standard library plus the sibling packages (RFC 0016 §1.1 cgo
// ban; docs/go-implementation-plan.md §1.3: stdlib encoding/xml is not a
// parsing base, only non-contractual stdlib helpers are used). Values use
// go/core; snapshots, patches, and the shared edit-record family use
// go/document.
//
// Errors returned by this package are typed and implement the RFC 0016 §6
// Code() contract: *FormationFailure, *EditFailure,
// *MaterializationFailure, and *ProjectionFailure. The `xml.*` diagnostic
// codes are registered by RFC 0012 and are part of the `xml.1.0-safe@1`
// contract; they do not enter the consema-protocol core error registry
// (RFC 0012 §12). Error text is human presentation only and never
// participates in conformance comparison.
package xml
