package plist

// This file implements the unified `plist.xml@1` / `plist.binary@1`
// document layer (consema-plist document.rs; RFC 0013 §3, §7). The two
// profiles share one native value model but have disjoint syntax systems:
// the XML lossless index and syntax kinds exist only for XML documents, and
// the binary object/offset/ref/trailer facts and structural regions exist
// only for binary documents (hard gate 1).
//
// Cross-representation conversion is a first-class transform (RFC 0013 §7):
// it serializes the reachable native value graph under the target profile,
// reparses the exact emitted bytes, verifies native-model equality (the
// reparse closure), and reports the representation change plus one
// value-mapped event per reachable node. Conversion is atomic: a native
// fact the target representation cannot express fails the whole conversion
// with a `plist.conversion.inexpressible@1` diagnostic and returns no
// target document (hard gate 3).

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// PlistRepresentation is the two plist representations (RFC 0013 §1, §7).
type PlistRepresentation uint8

// The two frozen representations.
const (
	// PlistRepresentationXML is the `plist.xml@1` text representation.
	PlistRepresentationXML PlistRepresentation = iota
	// PlistRepresentationBinary is the `plist.binary@1` object-table
	// representation.
	PlistRepresentationBinary
)

// Document is one formed plist document under either representation (RFC
// 0013 §3, §7). The concrete representation is private; representation-
// specific facts are reachable only through representation-specific
// accessors, so an XML document can never expose binary structure facts and
// vice versa (hard gate 1). Completed documents are logically immutable and
// safe for concurrent reads.
type Document struct {
	authority      document.DocumentAuthority
	source         *document.SourceSnapshot
	representation PlistRepresentation
	status         document.FormationStatus
	diagnostics    []*protocol.Diagnostic
	native         *PlistDocument
	xmlIndex       *document.LosslessStructuralIndex
	xmlKinds       []PlistSyntaxKind
	binaryFacts    *BinaryFacts
	binaryIndex    *document.BinaryStructuralIndex
	limits         PlistParseLimits
}

// SnapshotIdentity returns the snapshot identity to which every NodeRef
// and Span belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	return d.authority.Identity()
}

// Source returns the exact immutable source snapshot.
func (d *Document) Source() *document.SourceSnapshot { return d.source }

// Render returns the exact current source bytes; unmodified rendering is
// byte-exact. The returned slice is a copy.
func (d *Document) Render() []byte { return d.source.Bytes() }

// Representation returns the representation of the formed document.
func (d *Document) Representation() PlistRepresentation { return d.representation }

// FormatFamily returns the plist format family contract.
func (d *Document) FormatFamily() document.FormatFamilyId {
	return document.NewFormatFamilyId("plist", 1)
}

// Profile returns the exact source profile of the formed document.
func (d *Document) Profile() document.ProfileId {
	if d.representation == PlistRepresentationBinary {
		return PlistProfileBinaryV1.ID()
	}
	return PlistProfileXmlV1.ID()
}

// FormationStatus returns whether recovery structure was required.
func (d *Document) FormationStatus() document.FormationStatus { return d.status }

// Status returns the formation status; it is the same fact as
// FormationStatus.
func (d *Document) Status() document.FormationStatus { return d.status }

// Diagnostics returns the deterministically ordered document diagnostics.
// The returned slice must not be modified.
func (d *Document) Diagnostics() []*protocol.Diagnostic { return d.diagnostics }

// NativeDocument returns the native value arena, when the root value is
// provable (RFC 0013 §6). Both representations share the same native value
// model; non-nil exactly when formation proved the complete root value. A
// Recovered document may or may not carry one.
func (d *Document) NativeDocument() *PlistDocument { return d.native }

// LosslessStructuralIndex returns the exhaustive piece coverage;
// `plist.xml@1` only (RFC 0013 §8.2, hard gate 1).
func (d *Document) LosslessStructuralIndex() *document.LosslessStructuralIndex {
	return d.xmlIndex
}

// LosslessSyntaxKinds returns the format-specific kind for every
// structural piece, in the same source order; `plist.xml@1` only (hard
// gate 1). The returned slice must not be modified.
func (d *Document) LosslessSyntaxKinds() []PlistSyntaxKind { return d.xmlKinds }

// BinaryFacts returns the binary object/offset/reference/trailer facts;
// `plist.binary@1` only (RFC 0013 §8.3, hard gate 1).
func (d *Document) BinaryFacts() *BinaryFacts { return d.binaryFacts }

// BinaryStructuralIndex returns the exhaustive ordered region coverage;
// `plist.binary@1` only (RFC 0013 §2.2, §8.3, hard gate 1).
func (d *Document) BinaryStructuralIndex() *document.BinaryStructuralIndex {
	return d.binaryIndex
}

// ParseLimits returns the limits applied during formation.
func (d *Document) ParseLimits() PlistParseLimits { return d.limits }

// documentAuthority returns the snapshot-bound identity authority for
// issuing query handles.
func (d *Document) documentAuthority() document.DocumentAuthority { return d.authority }

// nodeRef issues one typed node handle.
func (d *Document) nodeRef(index int, role document.NodeRole) document.NodeRef {
	return d.authority.NodeRef(uint64(index), role)
}

// ConvertTo converts the document to the other representation (RFC 0013
// §7). Conversion serializes the reachable native value graph under the
// target profile, reparses the exact emitted bytes under the same limits,
// and verifies that the target native model equals the source native model
// (reparse closure). Every conversion reports one `RepresentationChange`
// event followed by one `ValueMapped` event per reachable native node, in
// source arena order.
//
// Conversion is atomic (hard gate 3): a native fact the target
// representation cannot express fails the whole conversion with a
// `plist.conversion.inexpressible@1` diagnostic and returns no target
// document. A source that is not Complete with a provable native document
// cannot be converted, and a target equal to the source representation is
// not a conversion.
func (d *Document) ConvertTo(target PlistProfile, limits PlistParseLimits) (*ConvertedDocument, *ConversionFailure) {
	targetRepresentation := PlistRepresentationXML
	if target == PlistProfileBinaryV1 {
		targetRepresentation = PlistRepresentationBinary
	}
	if d.representation == targetRepresentation {
		return nil, conversionFailure("plist.conversion.same-representation@1")
	}
	if d.status != document.FormationStatusComplete {
		return nil, conversionFailureWithArgs("plist.conversion.formation@1",
			map[string]string{"status": "recovered"})
	}
	if d.native == nil {
		return nil, conversionFailureWithArgs("plist.conversion.formation@1",
			map[string]string{"status": "no-native-document"})
	}
	if d.representation == PlistRepresentationXML {
		return convertXMLToBinary(d, limits)
	}
	return convertBinaryToXML(d, limits)
}

// ConvertedDocument is one successful cross-representation conversion (RFC
// 0013 §7).
type ConvertedDocument struct {
	document *Document
	report   *ConversionReport
}

// Document returns the target document in the converted representation.
func (c *ConvertedDocument) Document() *Document { return c.document }

// Report returns the conversion report events.
func (c *ConvertedDocument) Report() *ConversionReport { return c.report }

// IntoDocument returns the target document.
func (c *ConvertedDocument) IntoDocument() *Document { return c.document }

// ConversionReport is the conversion report of one cross-representation
// conversion (RFC 0013 §7).
type ConversionReport struct {
	events []ConversionReportEvent
}

// Events returns the ordered report events: one `RepresentationChange`
// event followed by one `ValueMapped` event per reachable native node, in
// source arena order.
func (r *ConversionReport) Events() []ConversionReportEvent {
	return append([]ConversionReportEvent(nil), r.events...)
}

// RepresentationChanged reports whether the conversion changed
// representation (always true for a successful cross-representation
// conversion).
func (r *ConversionReport) RepresentationChanged() bool {
	for _, event := range r.events {
		if event.Kind == ConversionEventKindRepresentationChange {
			return true
		}
	}
	return false
}

// ConversionEventKind is the closed event kind of one conversion report.
type ConversionEventKind uint8

// The two frozen event kinds.
const (
	// ConversionEventKindRepresentationChange: the document changed
	// representation (hard gate 2).
	ConversionEventKindRepresentationChange ConversionEventKind = iota
	// ConversionEventKindValueMapped: one reachable native value node was
	// carried into the target document at the mapped target arena ordinal.
	ConversionEventKindValueMapped
)

// ConversionReportEvent is one conversion report event (RFC 0013 §7).
type ConversionReportEvent struct {
	// Kind is the event kind.
	Kind ConversionEventKind
	// Source is the source arena node this event concerns, when one exists.
	Source *PlistValueRef
	// Target is the target arena ordinal this event maps to, when one
	// exists.
	Target *int
}

// ConversionFailure is the atomic conversion failure (RFC 0013 §7, hard
// gate 3). A failed conversion returns no target document, no partial
// bytes, and no partial report.
type ConversionFailure struct {
	diagnostics []*protocol.Diagnostic
}

// Diagnostics returns the ordered diagnostics explaining why no target
// document exists.
func (f *ConversionFailure) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), f.diagnostics...)
}

// Code returns the code of the first ordered diagnostic (RFC 0013 §7).
func (f *ConversionFailure) Code() string {
	if len(f.diagnostics) == 0 {
		return ""
	}
	return f.diagnostics[0].Code
}

// conversionFailure builds one conversion failure diagnostic.
func conversionFailure(code string) *ConversionFailure {
	return conversionFailureWithArgs(code, nil)
}

// conversionFailureWithArgs builds one conversion failure diagnostic with
// stable arguments.
func conversionFailureWithArgs(code string, arguments map[string]string) *ConversionFailure {
	return &ConversionFailure{diagnostics: []*protocol.Diagnostic{newDiagnostic(code,
		protocol.CategoryConversion, protocol.SeverityError, nil, arguments, 0)}}
}

// conversionLimit builds the `plist.limit.<name>@1` conversion limit
// failure (RFC 0013 §12).
func conversionLimit(name string, observed, limit int) *ConversionFailure {
	return &ConversionFailure{diagnostics: []*protocol.Diagnostic{newDiagnostic(
		"plist.limit."+name+"@1", protocol.CategoryResource, protocol.SeverityError, nil,
		map[string]string{"limit": itoa(limit), "observed": itoa(observed)}, 0)}}
}

// reparseFailure is the serializer-produced-bytes invariant violation.
func reparseFailure() *ConversionFailure {
	return conversionFailure("plist.conversion.reparse@1")
}

// internalConversionFailure is the unreachable internal state of the
// conversion layer.
func internalConversionFailure() *ConversionFailure {
	return conversionFailure("plist.conversion.internal@1")
}
