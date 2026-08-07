package ini

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// StructuralPieceKind is the lossless class of one structural piece
// (consema-document lib.rs:415-422).
type StructuralPieceKind uint8

// The three frozen piece classes.
const (
	// PieceToken is a lexical token.
	PieceToken StructuralPieceKind = iota
	// PieceTrivia is whitespace, newline, comment, or profile trivia.
	PieceTrivia
	// PieceErrorRegion is bytes not accepted as token or trivia.
	PieceErrorRegion
)

// StructuralPiece is one source byte interval and its lossless class
// (consema-document lib.rs:425-447).
type StructuralPiece struct {
	span document.Span
	kind StructuralPieceKind
}

// Span returns the exact raw byte range.
func (p StructuralPiece) Span() document.Span { return p.span }

// Kind returns the lossless class.
func (p StructuralPiece) Kind() StructuralPieceKind { return p.kind }

// LosslessStructuralIndex is the exhaustive ordered token/trivia coverage
// of one source (consema-document lib.rs:449-492). The index validates
// exact byte coverage and snapshot binding at construction.
type LosslessStructuralIndex struct {
	pieces []StructuralPiece
}

// NewLosslessStructuralIndex validates exact raw-byte coverage of the
// source and snapshot binding (consema-document lib.rs:449-492).
func NewLosslessStructuralIndex(identity document.SnapshotIdentity, sourceLen int,
	pieces []StructuralPiece) (*LosslessStructuralIndex, error) {
	next := 0
	for _, piece := range pieces {
		if piece.span.Snapshot() != identity {
			return nil, &document.LocationError{Kind: document.LocationWrongSnapshot}
		}
		if piece.span.StartByte() != next || piece.span.EndByte() <= piece.span.StartByte() ||
			piece.span.EndByte() > sourceLen {
			return nil, &document.LocationError{Kind: document.LocationIncompleteStructuralCoverage}
		}
		next = piece.span.EndByte()
	}
	if next != sourceLen {
		return nil, &document.LocationError{Kind: document.LocationIncompleteStructuralCoverage}
	}
	return &LosslessStructuralIndex{pieces: append([]StructuralPiece(nil), pieces...)}, nil
}

// Pieces returns the ordered exhaustive pieces. The returned slice is a
// copy; pieces are logically immutable.
func (i *LosslessStructuralIndex) Pieces() []StructuralPiece {
	return append([]StructuralPiece(nil), i.pieces...)
}

// IniPhysicalLine is one exact physical source line (consema-ini
// lib.rs:231-263).
type IniPhysicalLine struct {
	node          document.NodeRef
	span          document.Span
	contentSpan   document.Span
	lineBreakSpan *document.Span
}

// NodeRef returns the snapshot-bound physical-line identity.
func (l IniPhysicalLine) NodeRef() document.NodeRef { return l.node }

// Span returns the complete raw line including its line break.
func (l IniPhysicalLine) Span() document.Span { return l.span }

// ContentSpan returns the raw line content excluding its line break.
func (l IniPhysicalLine) ContentSpan() document.Span { return l.contentSpan }

// LineBreakSpan returns the exact LF or CRLF range, absent at EOF.
func (l IniPhysicalLine) LineBreakSpan() *document.Span { return l.lineBreakSpan }

// IniLogicalLine is one logical record and its ordered physical
// constituents (consema-ini lib.rs:266-291).
type IniLogicalLine struct {
	node          document.NodeRef
	kind          IniLogicalLineKind
	physicalLines []document.NodeRef
}

// NodeRef returns the snapshot-bound logical-line identity.
func (l IniLogicalLine) NodeRef() document.NodeRef { return l.node }

// Kind returns the logical record kind.
func (l IniLogicalLine) Kind() IniLogicalLineKind { return l.kind }

// PhysicalLines returns the ordered physical-line identities. The returned
// slice is a copy.
func (l IniLogicalLine) PhysicalLines() []document.NodeRef {
	return append([]document.NodeRef(nil), l.physicalLines...)
}

// IniSection is one distinct section-header occurrence (consema-ini
// lib.rs:294-355).
type IniSection struct {
	node           document.NodeRef
	logicalLine    document.NodeRef
	span           document.Span
	nameSpan       document.Span
	name           string
	comparisonName string
	isDefault      bool
	duplicateGroup *uint32
}

// NodeRef returns the snapshot-bound section occurrence identity.
func (s IniSection) NodeRef() document.NodeRef { return s.node }

// LogicalLine returns the owning logical-line identity.
func (s IniSection) LogicalLine() document.NodeRef { return s.logicalLine }

// Span returns the complete header content span, excluding the line break.
func (s IniSection) Span() document.Span { return s.span }

// NameSpan returns the exact section-name span.
func (s IniSection) NameSpan() document.Span { return s.nameSpan }

// Name returns the original decoded name spelling.
func (s IniSection) Name() string { return s.name }

// ComparisonName returns the profile-specific comparison name.
func (s IniSection) ComparisonName() string { return s.comparisonName }

// IsDefault reports whether this is Python's exact `DEFAULT` section.
func (s IniSection) IsDefault() bool { return s.isDefault }

// DuplicateGroup returns the deterministic duplicate/case-equivalence
// group identity, when present.
func (s IniSection) DuplicateGroup() *uint32 { return s.duplicateGroup }

// IniEntry is one distinct key/value occurrence (consema-ini lib.rs:358-445).
type IniEntry struct {
	node           document.NodeRef
	logicalLine    document.NodeRef
	section        document.NodeRef
	span           document.Span
	keySpan        document.Span
	valueSpan      document.Span
	key            string
	comparisonKey  string
	value          string
	state          IniValueState
	quoteStyle     IniQuoteStyle
	duplicateGroup *uint32
}

// NodeRef returns the snapshot-bound entry occurrence identity.
func (e IniEntry) NodeRef() document.NodeRef { return e.node }

// LogicalLine returns the owning logical-line identity.
func (e IniEntry) LogicalLine() document.NodeRef { return e.logicalLine }

// Section returns the owning section occurrence.
func (e IniEntry) Section() document.NodeRef { return e.section }

// Span returns the complete first physical-line content span.
func (e IniEntry) Span() document.Span { return e.span }

// KeySpan returns the exact original key span.
func (e IniEntry) KeySpan() document.Span { return e.keySpan }

// ValueSpan returns the exact first-line semantic value span.
func (e IniEntry) ValueSpan() document.Span { return e.valueSpan }

// Key returns the original decoded key spelling.
func (e IniEntry) Key() string { return e.key }

// ComparisonKey returns the profile-specific comparison key.
func (e IniEntry) ComparisonKey() string { return e.comparisonKey }

// Value returns the stored semantic string, including deterministic
// continuation joins.
func (e IniEntry) Value() string { return e.value }

// ValueState returns the missing, empty, or present value fact.
func (e IniEntry) ValueState() IniValueState { return e.state }

// QuoteStyle returns the profile-recognized outer quote style.
func (e IniEntry) QuoteStyle() IniQuoteStyle { return e.quoteStyle }

// DuplicateGroup returns the deterministic duplicate/case-equivalence
// group identity, when present.
func (e IniEntry) DuplicateGroup() *uint32 { return e.duplicateGroup }

// IniErrorLine is one recovered physical error record (consema-ini
// lib.rs:449-487).
type IniErrorLine struct {
	node         document.NodeRef
	logicalLine  document.NodeRef
	physicalLine document.NodeRef
	span         document.Span
	code         string
}

// NodeRef returns the snapshot-bound error identity.
func (e IniErrorLine) NodeRef() document.NodeRef { return e.node }

// LogicalLine returns the owning logical-line identity.
func (e IniErrorLine) LogicalLine() document.NodeRef { return e.logicalLine }

// PhysicalLine returns the physical line retained by recovery.
func (e IniErrorLine) PhysicalLine() document.NodeRef { return e.physicalLine }

// Span returns the exact malformed content span.
func (e IniErrorLine) Span() document.Span { return e.span }

// Code returns the stable diagnostic code.
func (e IniErrorLine) Code() string { return e.code }

// Document is the immutable lossless INI document (consema-ini lib.rs:
// 491-661). Completed documents are logically immutable; concurrent reads
// are safe.
type Document struct {
	authority       document.DocumentAuthority
	source          *document.SourceSnapshot
	profile         IniProfile
	index           *LosslessStructuralIndex
	kinds           []IniSyntaxKind
	formationStatus document.FormationStatus
	diagnostics     []*protocol.Diagnostic
	physicalLines   []IniPhysicalLine
	logicalLines    []IniLogicalLine
	sections        []IniSection
	entries         []IniEntry
	errorLines      []IniErrorLine
	limits          IniParseLimits
	rootNode        document.NodeRef
}

// SnapshotIdentity is the snapshot identity to which every INI handle and
// span belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	return d.authority.Identity()
}

// Source returns the exact immutable source snapshot.
func (d *Document) Source() *document.SourceSnapshot { return d.source }

// Render returns the default rendering, byte-for-byte identical to the
// source (including the BOM and the original newline encoding).
func (d *Document) Render() []byte { return d.source.Bytes() }

// FormatFamily returns the stable INI format family contract.
func (d *Document) FormatFamily() document.FormatFamilyId {
	return document.NewFormatFamilyId("ini", 1)
}

// Profile returns the exact selected profile identifier.
func (d *Document) Profile() document.ProfileId { return d.profile.ID() }

// NodeRef returns the root INI document identity.
func (d *Document) NodeRef() document.NodeRef { return d.rootNode }

// FormationStatus returns the complete or explicitly recovered formation
// state.
func (d *Document) FormationStatus() document.FormationStatus { return d.formationStatus }

// Diagnostics returns the deterministically ordered non-fatal
// diagnostics.
func (d *Document) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), d.diagnostics...)
}

// LosslessStructuralIndex returns the exhaustive ordered source coverage.
func (d *Document) LosslessStructuralIndex() *LosslessStructuralIndex { return d.index }

// LosslessSyntaxKinds returns the format-specific kind for every
// structural piece, in the same source order.
func (d *Document) LosslessSyntaxKinds() []IniSyntaxKind {
	return append([]IniSyntaxKind(nil), d.kinds...)
}

// PhysicalLines returns the ordered physical source lines.
func (d *Document) PhysicalLines() []IniPhysicalLine {
	return append([]IniPhysicalLine(nil), d.physicalLines...)
}

// LogicalLines returns the ordered logical records.
func (d *Document) LogicalLines() []IniLogicalLine {
	return append([]IniLogicalLine(nil), d.logicalLines...)
}

// Sections returns the ordered distinct section occurrences.
func (d *Document) Sections() []IniSection {
	return append([]IniSection(nil), d.sections...)
}

// Entries returns the ordered distinct entry occurrences.
func (d *Document) Entries() []IniEntry {
	return append([]IniEntry(nil), d.entries...)
}

// ErrorLines returns the ordered recovered error records.
func (d *Document) ErrorLines() []IniErrorLine {
	return append([]IniErrorLine(nil), d.errorLines...)
}

// ParseLimits returns the resource contract used to form this snapshot and
// any edit successor.
func (d *Document) ParseLimits() IniParseLimits { return d.limits }

// PhysicalLine resolves one physical-line handle only within this
// snapshot.
func (d *Document) PhysicalLine(node document.NodeRef) (IniPhysicalLine, bool) {
	if node.Snapshot() != d.SnapshotIdentity() {
		return IniPhysicalLine{}, false
	}
	if node.Role() != document.RoleIniPhysicalLine {
		return IniPhysicalLine{}, false
	}
	for _, line := range d.physicalLines {
		if line.node == node {
			return line, true
		}
	}
	return IniPhysicalLine{}, false
}

// LogicalLine resolves one logical-line handle only within this snapshot.
func (d *Document) LogicalLine(node document.NodeRef) (IniLogicalLine, bool) {
	if node.Snapshot() != d.SnapshotIdentity() {
		return IniLogicalLine{}, false
	}
	if node.Role() != document.RoleIniLogicalLine {
		return IniLogicalLine{}, false
	}
	for _, line := range d.logicalLines {
		if line.node == node {
			return line, true
		}
	}
	return IniLogicalLine{}, false
}

// Section resolves one section/default-section handle only within this
// snapshot.
func (d *Document) Section(node document.NodeRef) (IniSection, bool) {
	if node.Snapshot() != d.SnapshotIdentity() {
		return IniSection{}, false
	}
	if node.Role() != document.RoleIniSection && node.Role() != document.RoleIniDefaultSection {
		return IniSection{}, false
	}
	for _, section := range d.sections {
		if section.node == node {
			return section, true
		}
	}
	return IniSection{}, false
}

// Entry resolves one entry handle only within this snapshot.
func (d *Document) Entry(node document.NodeRef) (IniEntry, bool) {
	if node.Snapshot() != d.SnapshotIdentity() {
		return IniEntry{}, false
	}
	if node.Role() != document.RoleIniEntry {
		return IniEntry{}, false
	}
	for _, entry := range d.entries {
		if entry.node == node {
			return entry, true
		}
	}
	return IniEntry{}, false
}

// span builds one validated snapshot-bound span.
func (d *Document) span(start, end int) (document.Span, bool) {
	span, err := d.authority.Span(start, end)
	if err != nil {
		return document.Span{}, false
	}
	return span, true
}

// pieceWithin returns the first piece of one kind within a raw span.
func (d *Document) pieceWithin(span document.Span, kind IniSyntaxKind) (document.Span, bool) {
	for index, piece := range d.index.pieces {
		if piece.span.StartByte() >= span.StartByte() && piece.span.EndByte() <= span.EndByte() &&
			d.kinds[index] == kind {
			return piece.span, true
		}
	}
	return document.Span{}, false
}

// raw returns the exact raw bytes of one span.
func (d *Document) raw(span document.Span) []byte {
	return d.source.Bytes()[span.StartByte():span.EndByte()]
}

// decodedTextOf returns the decoded text of one raw span, when every
// boundary is a decoded scalar boundary.
func (d *Document) decodedTextOf(span document.Span) (string, bool) {
	text, ok := d.source.DecodedText()
	if !ok {
		return "", false
	}
	start, err := d.source.DecodedPosition(span.StartByte())
	if err != nil {
		return "", false
	}
	end, err := d.source.DecodedPosition(span.EndByte())
	if err != nil {
		return "", false
	}
	return text[start.DecodedUTF8Byte:end.DecodedUTF8Byte], true
}
