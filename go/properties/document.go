package properties

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file defines the immutable Java Properties document and its
// snapshot-bound record handles (consema-properties lib.rs; RFC
// 0010 §9). The Document ends at the native layer: duplicate keys and
// source order are preserved and last-wins is exposed only through the
// explicit projection policy.

// PropertiesNaturalLine is one exact natural source line (lib.rs).
type PropertiesNaturalLine struct {
	node          document.NodeRef
	span          document.Span
	contentSpan   document.Span
	lineBreakSpan *document.Span
}

// NodeRef returns the snapshot-bound natural-line identity.
func (l PropertiesNaturalLine) NodeRef() document.NodeRef { return l.node }

// Span returns the complete source span including the terminator.
func (l PropertiesNaturalLine) Span() document.Span { return l.span }

// ContentSpan returns the content span excluding the terminator.
func (l PropertiesNaturalLine) ContentSpan() document.Span { return l.contentSpan }

// LineBreakSpan returns the LF, CR, or CRLF span; absent for an EOF line.
func (l PropertiesNaturalLine) LineBreakSpan() *document.Span { return l.lineBreakSpan }

// PropertiesLogicalLine is one property/error logical line and its
// natural-line constituents (lib.rs).
type PropertiesLogicalLine struct {
	node         document.NodeRef
	kind         PropertiesLogicalLineKind
	naturalLines []document.NodeRef
}

// NodeRef returns the snapshot-bound logical-line identity.
func (l PropertiesLogicalLine) NodeRef() document.NodeRef { return l.node }

// Kind returns the property or recovered-error classification.
func (l PropertiesLogicalLine) Kind() PropertiesLogicalLineKind { return l.kind }

// NaturalLines returns the ordered natural-line constituents. The
// returned slice is a copy.
func (l PropertiesLogicalLine) NaturalLines() []document.NodeRef {
	return append([]document.NodeRef(nil), l.naturalLines...)
}

// PropertiesComment is one comment natural line (lib.rs).
type PropertiesComment struct {
	node        document.NodeRef
	naturalLine document.NodeRef
	span        document.Span
	marker      rune
}

// NodeRef returns the snapshot-bound comment identity.
func (c PropertiesComment) NodeRef() document.NodeRef { return c.node }

// NaturalLine returns the owning natural line.
func (c PropertiesComment) NaturalLine() document.NodeRef { return c.naturalLine }

// Span returns the complete comment content span excluding its line
// break.
func (c PropertiesComment) Span() document.Span { return c.span }

// Marker returns the exact comment marker.
func (c PropertiesComment) Marker() rune { return c.marker }

// PropertiesEscape is one source escape and its exact Java-string output
// range (lib.rs).
type PropertiesEscape struct {
	node        document.NodeRef
	property    document.NodeRef
	inKey       bool
	kind        PropertiesEscapeKind
	span        document.Span
	outputStart int
	outputEnd   int
}

// NodeRef returns the snapshot-bound escape identity.
func (e PropertiesEscape) NodeRef() document.NodeRef { return e.node }

// Property returns the owning property occurrence.
func (e PropertiesEscape) Property() document.NodeRef { return e.property }

// InKey reports whether the output range belongs to the decoded key.
func (e PropertiesEscape) InKey() bool { return e.inKey }

// Kind returns the exact escape kind.
func (e PropertiesEscape) Kind() PropertiesEscapeKind { return e.kind }

// Span returns the complete raw escape spelling.
func (e PropertiesEscape) Span() document.Span { return e.span }

// OutputStart returns the inclusive Java UTF-16 output offset.
func (e PropertiesEscape) OutputStart() int { return e.outputStart }

// OutputEnd returns the exclusive Java UTF-16 output boundary.
func (e PropertiesEscape) OutputEnd() int { return e.outputEnd }

// Property is one distinct source-ordered property association
// (lib.rs; RFC 0010 §2).
type Property struct {
	node           document.NodeRef
	logicalLine    document.NodeRef
	span           document.Span
	keyAnchor      document.Span
	valueAnchor    document.Span
	keyFragments   []document.Span
	valueFragments []document.Span
	key            JavaString
	value          JavaString
	valueState     PropertiesValueState
	escapes        []document.NodeRef
	duplicateGroup *uint32
}

// NodeRef returns the snapshot-bound property association identity.
func (p Property) NodeRef() document.NodeRef { return p.node }

// LogicalLine returns the owning logical line.
func (p Property) LogicalLine() document.NodeRef { return p.logicalLine }

// Span returns the complete first-to-last property source range.
func (p Property) Span() document.Span { return p.span }

// KeyAnchor returns the zero-width source anchor at the start of the
// decoded key.
func (p Property) KeyAnchor() document.Span { return p.keyAnchor }

// ValueAnchor returns the zero-width source anchor at the start of the
// decoded value.
func (p Property) ValueAnchor() document.Span { return p.valueAnchor }

// KeyFragments returns the ordered raw source fragments contributing to
// the key. The returned slice is a copy.
func (p Property) KeyFragments() []document.Span {
	return append([]document.Span(nil), p.keyFragments...)
}

// ValueFragments returns the ordered raw source fragments contributing to
// the value. The returned slice is a copy.
func (p Property) ValueFragments() []document.Span {
	return append([]document.Span(nil), p.valueFragments...)
}

// Key returns the exact decoded Java UTF-16 key.
func (p Property) Key() JavaString { return p.key }

// Value returns the exact decoded Java UTF-16 element.
func (p Property) Value() JavaString { return p.value }

// ValueState returns the implicit, explicit-empty, or present source
// state.
func (p Property) ValueState() PropertiesValueState { return p.valueState }

// Escapes returns the ordered escape identities in key-then-value decode
// order. The returned slice is a copy.
func (p Property) Escapes() []document.NodeRef {
	return append([]document.NodeRef(nil), p.escapes...)
}

// DuplicateGroup returns the deterministic exact-code-unit duplicate
// group, when this property is a member of one.
func (p Property) DuplicateGroup() *uint32 { return p.duplicateGroup }

// PropertiesErrorLine is one recovered malformed logical line
// (lib.rs).
type PropertiesErrorLine struct {
	node         document.NodeRef
	logicalLine  document.NodeRef
	naturalLines []document.NodeRef
	span         document.Span
	code         string
}

// NodeRef returns the snapshot-bound error identity.
func (l PropertiesErrorLine) NodeRef() document.NodeRef { return l.node }

// LogicalLine returns the owning recovered logical line.
func (l PropertiesErrorLine) LogicalLine() document.NodeRef { return l.logicalLine }

// NaturalLines returns the natural lines retained by this recovery
// record. The returned slice is a copy.
func (l PropertiesErrorLine) NaturalLines() []document.NodeRef {
	return append([]document.NodeRef(nil), l.naturalLines...)
}

// Span returns the complete recovered source range.
func (l PropertiesErrorLine) Span() document.Span { return l.span }

// Code returns the stable diagnostic code.
func (l PropertiesErrorLine) Code() string { return l.code }

// Document is the immutable, duplicate-preserving Java Properties
// document (lib.rs; RFC 0010 §9). Completed documents are
// logically immutable; concurrent reads are safe.
type Document struct {
	authority       document.DocumentAuthority
	source          *document.SourceSnapshot
	profile         PropertiesProfile
	index           *LosslessStructuralIndex
	syntaxKinds     []PropertiesSyntaxKind
	formationStatus document.FormationStatus
	diagnostics     []*protocol.Diagnostic
	naturalLines    []PropertiesNaturalLine
	logicalLines    []PropertiesLogicalLine
	properties      []Property
	comments        []PropertiesComment
	escapes         []PropertiesEscape
	errorLines      []PropertiesErrorLine
	parseLimits     PropertiesParseLimits
}

// SnapshotIdentity returns the snapshot identity to which every Properties
// handle and span belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	return d.authority.Identity()
}

// Source returns the exact immutable source snapshot.
func (d *Document) Source() *document.SourceSnapshot { return d.source }

// Render returns the default rendering, byte-for-byte identical to the
// source (RFC 0010 §3).
func (d *Document) Render() []byte { return d.source.Bytes() }

// FormatFamily returns the stable Java Properties format family.
func (d *Document) FormatFamily() document.FormatFamilyId {
	return document.NewFormatFamilyId("java-properties", 1)
}

// Profile returns the exact selected profile.
func (d *Document) Profile() document.ProfileId { return d.profile.ID() }

// SelectedProfile returns the concrete selected profile.
func (d *Document) SelectedProfile() PropertiesProfile { return d.profile }

// NodeRef returns the root Properties document identity.
func (d *Document) NodeRef() document.NodeRef {
	return d.authority.NodeRef(0, document.RolePropertiesDocument)
}

// FormationStatus returns the complete or explicitly recovered formation
// state.
func (d *Document) FormationStatus() document.FormationStatus { return d.formationStatus }

// Diagnostics returns the deterministically ordered diagnostics.
func (d *Document) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), d.diagnostics...)
}

// LosslessStructuralIndex returns the exhaustive token/trivia byte
// coverage.
func (d *Document) LosslessStructuralIndex() *LosslessStructuralIndex { return d.index }

// LosslessSyntaxKinds returns the format-specific kind for every
// structural piece, in the same source order. The returned slice is a
// copy.
func (d *Document) LosslessSyntaxKinds() []PropertiesSyntaxKind {
	return append([]PropertiesSyntaxKind(nil), d.syntaxKinds...)
}

// NaturalLines returns the ordered natural source lines. The returned
// slice is a copy.
func (d *Document) NaturalLines() []PropertiesNaturalLine {
	return append([]PropertiesNaturalLine(nil), d.naturalLines...)
}

// LogicalLines returns the ordered property/error logical lines. The
// returned slice is a copy.
func (d *Document) LogicalLines() []PropertiesLogicalLine {
	return append([]PropertiesLogicalLine(nil), d.logicalLines...)
}

// Properties returns the ordered duplicate-preserving property
// associations. The returned slice is a copy.
func (d *Document) Properties() []Property {
	return append([]Property(nil), d.properties...)
}

// Comments returns the ordered comment occurrences. The returned slice is
// a copy.
func (d *Document) Comments() []PropertiesComment {
	return append([]PropertiesComment(nil), d.comments...)
}

// Escapes returns the ordered escape occurrences. The returned slice is a
// copy.
func (d *Document) Escapes() []PropertiesEscape {
	return append([]PropertiesEscape(nil), d.escapes...)
}

// ErrorLines returns the ordered recovered error lines. The returned
// slice is a copy.
func (d *Document) ErrorLines() []PropertiesErrorLine {
	return append([]PropertiesErrorLine(nil), d.errorLines...)
}

// ParseLimits returns the resource contract used to form this snapshot.
func (d *Document) ParseLimits() PropertiesParseLimits { return d.parseLimits }

// Property resolves one property handle only within this snapshot.
func (d *Document) Property(node document.NodeRef) (Property, error) {
	if err := d.authority.Verify(node); err != nil {
		return Property{}, err
	}
	if node.Role() != document.RolePropertiesProperty {
		return Property{}, &document.LocationError{Kind: document.LocationWrongRole}
	}
	for _, property := range d.properties {
		if property.node == node {
			return property, nil
		}
	}
	return Property{}, &document.LocationError{Kind: document.LocationOutOfBounds}
}

// NaturalLine resolves one natural-line handle only within this snapshot.
func (d *Document) NaturalLine(node document.NodeRef) (PropertiesNaturalLine, error) {
	if err := d.authority.Verify(node); err != nil {
		return PropertiesNaturalLine{}, err
	}
	if node.Role() != document.RolePropertiesNaturalLine {
		return PropertiesNaturalLine{}, &document.LocationError{Kind: document.LocationWrongRole}
	}
	for _, line := range d.naturalLines {
		if line.node == node {
			return line, nil
		}
	}
	return PropertiesNaturalLine{}, &document.LocationError{Kind: document.LocationOutOfBounds}
}

// LogicalLine resolves one logical-line handle only within this snapshot.
func (d *Document) LogicalLine(node document.NodeRef) (PropertiesLogicalLine, error) {
	if err := d.authority.Verify(node); err != nil {
		return PropertiesLogicalLine{}, err
	}
	if node.Role() != document.RolePropertiesLogicalLine {
		return PropertiesLogicalLine{}, &document.LocationError{Kind: document.LocationWrongRole}
	}
	for _, line := range d.logicalLines {
		if line.node == node {
			return line, nil
		}
	}
	return PropertiesLogicalLine{}, &document.LocationError{Kind: document.LocationOutOfBounds}
}

// Escape resolves one escape handle only within this snapshot.
func (d *Document) Escape(node document.NodeRef) (PropertiesEscape, error) {
	if err := d.authority.Verify(node); err != nil {
		return PropertiesEscape{}, err
	}
	if node.Role() != document.RolePropertiesEscape {
		return PropertiesEscape{}, &document.LocationError{Kind: document.LocationWrongRole}
	}
	for _, escape := range d.escapes {
		if escape.node == node {
			return escape, nil
		}
	}
	return PropertiesEscape{}, &document.LocationError{Kind: document.LocationOutOfBounds}
}

// Parse forms one immutable Properties snapshot under one exact
// profile/source contract (parser.rs; RFC 0010 §3, §8). The
// profile and encoding are selected explicitly by the caller and never
// guessed from the extension, locale, or platform.
func Parse(source []byte, profile PropertiesProfile,
	selection PropertiesEncodingSelection, limits PropertiesParseLimits) (*Document, *FormationFailure) {
	return parse(source, profile, selection, limits)
}

// parse forms one immutable Properties snapshot under one exact
// profile/source contract.
func parse(source []byte, profile PropertiesProfile,
	selection PropertiesEncodingSelection, limits PropertiesParseLimits) (*Document, *FormationFailure) {
	request, failure := encodingRequest(profile, selection)
	if failure != nil {
		return nil, failure
	}
	snapshot, err := document.NewSourceSnapshotFromRaw(source, request, document.SourceLimits{
		MaxRawBytes:         limits.Common.MaxSourceBytes,
		MaxDecodedUTF8Bytes: limits.MaxDecodedUTF8Bytes,
		MaxDecodedScalars:   limits.MaxDecodedScalars,
	})
	if err != nil {
		return nil, sourceFailure(err)
	}
	if failure := validateProfileEncoding(snapshot, profile, selection); failure != nil {
		return nil, failure
	}
	p, failure := newParser(snapshot, profile, limits)
	if failure != nil {
		return nil, failure
	}
	return p.parse()
}

// ParseReader parses Reader input using one explicit published text
// encoding (lib.rs).
func ParseReader(source []byte, encoding document.SourceEncoding,
	limits PropertiesParseLimits) (*Document, *FormationFailure) {
	return parse(source, PropertiesReaderV1, ReaderEncodingSelection(encoding), limits)
}

// ParseLatin1 parses InputStream-compatible Latin-1 bytes with marker
// bytes as content (lib.rs).
func ParseLatin1(source []byte, limits PropertiesParseLimits) (*Document, *FormationFailure) {
	return parse(source, PropertiesLatin1V1, Latin1EncodingSelection(), limits)
}

// encodingRequest builds the source encoding request of one exact
// profile/selection pair (parser.rs).
func encodingRequest(profile PropertiesProfile,
	selection PropertiesEncodingSelection) (document.EncodingRequest, *FormationFailure) {
	switch {
	case profile == PropertiesReaderV1 && !selection.IsLatin1():
		encoding, _ := selection.ReaderEncoding()
		if encoding.Kind() == document.EncodingBinary {
			return document.EncodingRequest{}, profileEncodingFailure()
		}
		return document.NewEncodingRequest(encoding).WithCallerOverride(encoding), nil
	case profile == PropertiesLatin1V1 && selection.IsLatin1():
		return document.NewEncodingRequest(document.Latin1Encoding()).
			WithCallerOverride(document.Latin1Encoding()).
			WithBomPolicy(document.BomPolicyTreatAsContent), nil
	}
	return document.EncodingRequest{}, profileEncodingFailure()
}

// validateProfileEncoding rejects every profile/selection/encoding
// contradiction (parser.rs; RFC 0010 §3).
func validateProfileEncoding(snapshot *document.SourceSnapshot, profile PropertiesProfile,
	selection PropertiesEncodingSelection) *FormationFailure {
	facts := snapshot.EncodingFacts()
	valid := false
	switch {
	case profile == PropertiesReaderV1 && !selection.IsLatin1():
		encoding, _ := selection.ReaderEncoding()
		valid = encoding.Kind() != document.EncodingBinary &&
			facts.Selected().Equal(encoding) &&
			facts.BomPolicy() == document.BomPolicyDetectUnicode
	case profile == PropertiesLatin1V1 && selection.IsLatin1():
		valid = facts.Selected().Equal(document.Latin1Encoding()) &&
			facts.BomPolicy() == document.BomPolicyTreatAsContent &&
			facts.Bom() == nil
	}
	if valid {
		return nil
	}
	return profileEncodingFailure()
}
