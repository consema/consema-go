package toml

import (
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// TomlProfile is the frozen TOML language profile (consema-toml lib.rs).
// The unexported field makes the set closed.
type TomlProfile struct {
	name string
}

// Toml10V1 is TOML 1.0.0 without implementation extensions.
var Toml10V1 = TomlProfile{name: "toml.1.0@1"}

// ID returns the immutable profile identifier.
func (p TomlProfile) ID() document.ProfileId {
	return document.NewProfileId("toml.1.0", 1)
}

// String returns the stable profile spelling.
func (p TomlProfile) String() string { return p.name }

// TomlSyntaxKind is the closed TOML lossless syntax-piece classification
// (consema-toml lib.rs). The stable query and protocol spellings are
// the exact `as_str` names.
type TomlSyntaxKind string

// The twelve frozen TOML syntax kinds.
const (
	// SyntaxKindWhitespace is horizontal whitespace.
	SyntaxKindWhitespace TomlSyntaxKind = "Whitespace"
	// SyntaxKindNewline is LF, CRLF, or an invalid bare CR retained for
	// formation diagnostics.
	SyntaxKindNewline TomlSyntaxKind = "Newline"
	// SyntaxKindComment is a `#` comment excluding its newline.
	SyntaxKindComment TomlSyntaxKind = "Comment"
	// SyntaxKindString is a basic or literal string token, including
	// multiline forms.
	SyntaxKindString TomlSyntaxKind = "String"
	// SyntaxKindBare is a bare key or value fragment.
	SyntaxKindBare TomlSyntaxKind = "Bare"
	// SyntaxKindEquals is `=`.
	SyntaxKindEquals TomlSyntaxKind = "Equals"
	// SyntaxKindLeftBracket is `[`.
	SyntaxKindLeftBracket TomlSyntaxKind = "LeftBracket"
	// SyntaxKindRightBracket is `]`.
	SyntaxKindRightBracket TomlSyntaxKind = "RightBracket"
	// SyntaxKindLeftBrace is `{`.
	SyntaxKindLeftBrace TomlSyntaxKind = "LeftBrace"
	// SyntaxKindRightBrace is `}`.
	SyntaxKindRightBrace TomlSyntaxKind = "RightBrace"
	// SyntaxKindComma is `,`.
	SyntaxKindComma TomlSyntaxKind = "Comma"
	// SyntaxKindDot is `.`.
	SyntaxKindDot TomlSyntaxKind = "Dot"
)

// AsStr returns the stable query and protocol name.
func (k TomlSyntaxKind) AsStr() string { return string(k) }

// TomlSyntaxKindFromName resolves one exact stable kind name.
func TomlSyntaxKindFromName(name string) (TomlSyntaxKind, bool) {
	switch name {
	case "Whitespace":
		return SyntaxKindWhitespace, true
	case "Newline":
		return SyntaxKindNewline, true
	case "Comment":
		return SyntaxKindComment, true
	case "String":
		return SyntaxKindString, true
	case "Bare":
		return SyntaxKindBare, true
	case "Equals":
		return SyntaxKindEquals, true
	case "LeftBracket":
		return SyntaxKindLeftBracket, true
	case "RightBracket":
		return SyntaxKindRightBracket, true
	case "LeftBrace":
		return SyntaxKindLeftBrace, true
	case "RightBrace":
		return SyntaxKindRightBrace, true
	case "Comma":
		return SyntaxKindComma, true
	case "Dot":
		return SyntaxKindDot, true
	}
	return "", false
}

// TomlItemKind is the closed native TOML item category (RFC 0001 §2;
// consema-toml lib.rs). Tables are native TOML items, never JSON
// object/member types.
type TomlItemKind string

// The fifteen frozen native item categories.
const (
	// ItemKindString is a decoded TOML string.
	ItemKindString TomlItemKind = "String"
	// ItemKindInteger is a signed 64-bit TOML integer.
	ItemKindInteger TomlItemKind = "Integer"
	// ItemKindFloat is an IEEE-754 binary64 TOML float.
	ItemKindFloat TomlItemKind = "Float"
	// ItemKindBoolean is a boolean.
	ItemKindBoolean TomlItemKind = "Boolean"
	// ItemKindOffsetDateTime is a date-time with a fixed offset.
	ItemKindOffsetDateTime TomlItemKind = "OffsetDateTime"
	// ItemKindLocalDateTime is a date-time without an offset.
	ItemKindLocalDateTime TomlItemKind = "LocalDateTime"
	// ItemKindLocalDate is a date without time or offset.
	ItemKindLocalDate TomlItemKind = "LocalDate"
	// ItemKindLocalTime is a time without date or offset.
	ItemKindLocalTime TomlItemKind = "LocalTime"
	// ItemKindArray is an inline value array.
	ItemKindArray TomlItemKind = "Array"
	// ItemKindInlineTable is an inline table value.
	ItemKindInlineTable TomlItemKind = "InlineTable"
	// ItemKindRootTable is the document root table.
	ItemKindRootTable TomlItemKind = "RootTable"
	// ItemKindStandardTable is an explicit standard table.
	ItemKindStandardTable TomlItemKind = "StandardTable"
	// ItemKindImplicitTable is a logical table created by a table path.
	ItemKindImplicitTable TomlItemKind = "ImplicitTable"
	// ItemKindDottedTable is a logical table created by dotted-key syntax.
	ItemKindDottedTable TomlItemKind = "DottedTable"
	// ItemKindArrayOfTables is an ordered array of explicit tables.
	ItemKindArrayOfTables TomlItemKind = "ArrayOfTables"
)

// String returns the stable category name.
func (k TomlItemKind) String() string { return string(k) }

// TomlDate is the parsed TOML date field set (consema-toml lib.rs).
type TomlDate struct {
	// Year is the four-digit year.
	Year uint16
	// Month is in 1..=12.
	Month uint8
	// Day is the day in the selected month.
	Day uint8
}

// TomlTime is the parsed TOML time field set (consema-toml lib.rs).
type TomlTime struct {
	// Hour is in 0..=23.
	Hour uint8
	// Minute is in 0..=59.
	Minute uint8
	// Second is the parsed second (0..=60; leap seconds are accepted by
	// the profile and rejected by the core projection).
	Second uint8
	// Nanosecond is the fractional second truncated to nanoseconds.
	Nanosecond uint32
}

// TomlOffset is the parsed TOML UTC offset (consema-toml lib.rs).
type TomlOffset struct {
	// Z reports the literal UTC `Z` offset.
	Z bool
	// Minutes is the signed offset in minutes of Custom offsets.
	Minutes int16
}

// TomlDateTime is the complete native TOML date/time datum
// (consema-toml lib.rs).
type TomlDateTime struct {
	// Date is the optional date component.
	Date *TomlDate
	// Time is the optional time component.
	Time *TomlTime
	// Offset is the optional UTC offset.
	Offset *TomlOffset
}

// FormationFailure is the fatal formation failure before any complete
// Document exists (consema-document lib.rs; RFC 0001 §3). It
// implements error and the RFC 0016 §6 Code() contract with the first
// diagnostic's registered code.
type FormationFailure struct {
	diagnostics []*protocol.Diagnostic
}

// Diagnostics returns the ordered diagnostics explaining the failure.
func (f *FormationFailure) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), f.diagnostics...)
}

// Error implements error; the text is human presentation only (RFC 0016 §6).
func (f *FormationFailure) Error() string {
	if len(f.diagnostics) == 0 {
		return "toml: formation failed"
	}
	return "toml: formation failed: " + f.diagnostics[0].Code
}

// Code returns the registered code of the first diagnostic.
func (f *FormationFailure) Code() string {
	if len(f.diagnostics) == 0 {
		return "toml.parse.syntax@1"
	}
	return f.diagnostics[0].Code
}

// newFormationFailure builds a single-diagnostic fatal failure against the
// frozen registry.
func newFormationFailure(code string, category protocol.DiagnosticCategory,
	spanStart, spanEnd int, arguments map[string]string) *FormationFailure {
	var primary *protocol.SourceLocation
	if spanStart >= 0 {
		primary = &protocol.SourceLocation{StartByte: uint64(spanStart), EndByte: uint64(spanEnd)}
	}
	diagnostic, err := protocol.NewDiagnostic(code, category, protocol.SeverityError, primary,
		nil, arguments, nil, nil, 0, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		// All formation codes are registered; an unregistered code is an
		// internal invariant violation and must never produce a document.
		panic("toml: unregistered formation code " + code)
	}
	return &FormationFailure{diagnostics: []*protocol.Diagnostic{diagnostic}}
}

// resourceLimitFailure builds the frozen core.parse.resource-limit@1
// diagnostic (consema-document lib.rs).
func resourceLimitFailure(name string, observed, limit int) *FormationFailure {
	return newFormationFailure("core.parse.resource-limit@1", protocol.CategoryResource,
		-1, -1, map[string]string{
			"limit":    fmt.Sprint(limit),
			"name":     name,
			"observed": fmt.Sprint(observed),
		})
}

// StructuralPieceKind is the lossless class of one structural piece
// (document.StructuralPieceKind; consema-document lib.rs).
type StructuralPieceKind = document.StructuralPieceKind

// The three frozen piece classes.
const (
	// PieceToken is a lexical token.
	PieceToken = document.PieceToken
	// PieceTrivia is whitespace, newline, comment, or profile trivia.
	PieceTrivia = document.PieceTrivia
	// PieceErrorRegion is bytes not accepted as token or trivia.
	PieceErrorRegion = document.PieceErrorRegion
)

// StructuralPiece is one source byte interval and its lossless class
// (document.StructuralPiece; consema-document lib.rs).
type StructuralPiece = document.StructuralPiece

// LosslessStructuralIndex is the exhaustive ordered token/trivia coverage
// of one source (document.LosslessStructuralIndex; consema-document
// lib.rs). The index validates exact byte coverage and snapshot
// binding at construction.
type LosslessStructuralIndex = document.LosslessStructuralIndex

// NewLosslessStructuralIndex validates exact raw-byte coverage of the
// source and snapshot binding (consema-document lib.rs; the syntax
// piece identities are the source ordinals, so no duplicate-identity check
// applies).
func NewLosslessStructuralIndex(identity document.SnapshotIdentity, sourceLen int,
	pieces []StructuralPiece) (*LosslessStructuralIndex, error) {
	return document.NewLosslessStructuralIndex(identity, sourceLen, pieces)
}

// Document is the opaque immutable TOML document snapshot (consema-toml
// lib.rs). Completed documents are logically immutable; concurrent
// reads are safe.
type Document struct {
	authority document.DocumentAuthority
	source    *document.SourceSnapshot
	profile   TomlProfile
	index     *LosslessStructuralIndex
	kinds     []TomlSyntaxKind
	entities  []entity
	root      int
	limits    document.ParseLimits
}

// SnapshotIdentity is the snapshot identity to which every native handle
// and span belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	return d.authority.Identity()
}

// Source returns the exact immutable UTF-8 source.
func (d *Document) Source() *document.SourceSnapshot { return d.source }

// Render returns the default rendering, byte-for-byte identical to the
// source.
func (d *Document) Render() []byte { return d.source.Bytes() }

// FormatFamily returns the TOML format family contract.
func (d *Document) FormatFamily() document.FormatFamilyId {
	return document.NewFormatFamilyId("toml", 1)
}

// Profile returns the exact language profile.
func (d *Document) Profile() document.ProfileId { return d.profile.ID() }

// FormationStatus returns the formation state. TOML 0.2 forms only
// complete valid documents.
func (d *Document) FormationStatus() document.FormationStatus {
	return document.FormationStatusComplete
}

// Diagnostics returns the deterministically ordered non-fatal diagnostics;
// complete TOML documents always carry none.
func (d *Document) Diagnostics() []*protocol.Diagnostic { return nil }

// LosslessStructuralIndex returns the exhaustive token/trivia byte
// coverage.
func (d *Document) LosslessStructuralIndex() *LosslessStructuralIndex {
	return d.index
}

// LosslessSyntaxKinds returns the format-specific kind for every
// structural piece, in the same source order.
func (d *Document) LosslessSyntaxKinds() []TomlSyntaxKind {
	return append([]TomlSyntaxKind(nil), d.kinds...)
}

// ParseLimits returns the resource contract used to form this snapshot and
// any edit successor.
func (d *Document) ParseLimits() document.ParseLimits { return d.limits }

// Root returns the root native item, which is always the RootTable.
func (d *Document) Root() TomlItem {
	return TomlItem{document: d, index: d.root}
}

// Item resolves one snapshot-bound TOML item handle.
func (d *Document) Item(node document.NodeRef) (TomlItem, error) {
	index, err := d.validateRef(node, document.RoleTomlItem)
	if err != nil {
		return TomlItem{}, err
	}
	if d.entities[index].kind != entityItem {
		return TomlItem{}, &TomlAccessError{Kind: TomlAccessWrongRole}
	}
	return TomlItem{document: d, index: index}, nil
}

// TomlAccessErrorKind classifies a stable native handle failure
// (consema-toml lib.rs).
type TomlAccessErrorKind uint8

// The stable handle failure classes.
const (
	// TomlAccessWrongSnapshot: the handle belongs to another immutable
	// snapshot.
	TomlAccessWrongSnapshot TomlAccessErrorKind = iota
	// TomlAccessWrongRole: the handle role does not match the requested
	// native entity.
	TomlAccessWrongRole
	// TomlAccessUnknownNode: the handle index is not present in this
	// document.
	TomlAccessUnknownNode
)

// TomlAccessError is a stable TOML native handle failure.
type TomlAccessError struct {
	// Kind identifies the failure.
	Kind TomlAccessErrorKind
}

// Error implements error.
func (e *TomlAccessError) Error() string {
	switch e.Kind {
	case TomlAccessWrongSnapshot:
		return "toml: handle belongs to another snapshot"
	case TomlAccessWrongRole:
		return "toml: handle role mismatch"
	case TomlAccessUnknownNode:
		return "toml: unknown node"
	}
	return "toml: access error"
}

// Code returns the registered invalid-input code (RFC 0016 §6).
func (e *TomlAccessError) Code() string { return "core.protocol.invalid-value@1" }

// Name returns the stable failure name mirrored from the Rust
// TomlAccessError variant.
func (e *TomlAccessError) Name() string {
	switch e.Kind {
	case TomlAccessWrongSnapshot:
		return "WrongSnapshot"
	case TomlAccessWrongRole:
		return "WrongRole"
	case TomlAccessUnknownNode:
		return "UnknownNode"
	}
	return fmt.Sprintf("TomlAccessError(%d)", e.Kind)
}

// TomlItem is the borrowed native TOML item bound to one document snapshot
// (consema-toml lib.rs).
type TomlItem struct {
	document *Document
	index    int
}

// NodeRef returns the exact item identity.
func (i TomlItem) NodeRef() document.NodeRef {
	return i.document.nodeRef(i.index, document.RoleTomlItem)
}

// Span returns the exact or contract-authorized logical source span.
func (i TomlItem) Span() document.Span { return i.document.entities[i.index].span }

// Kind returns the native item category.
func (i TomlItem) Kind() TomlItemKind { return i.document.itemEntity(i.index).publicKind() }

// AsString returns the decoded string when this item is a string.
func (i TomlItem) AsString() (string, bool) {
	item := i.document.itemEntity(i.index)
	if item.kind != itemString {
		return "", false
	}
	return item.str, true
}

// AsInteger returns the signed integer when this item is an integer.
func (i TomlItem) AsInteger() (int64, bool) {
	item := i.document.itemEntity(i.index)
	if item.kind != itemInteger {
		return 0, false
	}
	return item.integer, true
}

// AsFloatBits returns the exact IEEE-754 binary64 bit pattern when this
// item is a float.
func (i TomlItem) AsFloatBits() (uint64, bool) {
	item := i.document.itemEntity(i.index)
	if item.kind != itemFloat {
		return 0, false
	}
	return item.bits, true
}

// AsBoolean returns the boolean when this item is a boolean.
func (i TomlItem) AsBoolean() (bool, bool) {
	item := i.document.itemEntity(i.index)
	if item.kind != itemBoolean {
		return false, false
	}
	return item.boolean, true
}

// AsDateTime returns the native temporal datum when this item is any TOML
// date/time category.
func (i TomlItem) AsDateTime() (*TomlDateTime, bool) {
	item := i.document.itemEntity(i.index)
	if item.kind != itemDateTime {
		return nil, false
	}
	value := item.dateTime
	return &value, true
}

// TableEntries returns the direct ordered entries for any table category
// or inline table.
func (i TomlItem) TableEntries() ([]TomlEntry, bool) {
	item := i.document.itemEntity(i.index)
	var entries []int
	switch item.kind {
	case itemTable:
		entries = item.entries
	case itemInlineTable:
		entries = item.entries
	default:
		return nil, false
	}
	result := make([]TomlEntry, 0, len(entries))
	for _, index := range entries {
		result = append(result, TomlEntry{document: i.document, index: index})
	}
	return result, true
}

// ArrayElements returns the direct ordered elements for arrays and
// arrays-of-tables.
func (i TomlItem) ArrayElements() ([]TomlArrayElement, bool) {
	item := i.document.itemEntity(i.index)
	var elements []int
	switch item.kind {
	case itemArray:
		elements = item.elements
	case itemArrayOfTables:
		elements = item.elements
	default:
		return nil, false
	}
	result := make([]TomlArrayElement, 0, len(elements))
	for _, index := range elements {
		result = append(result, TomlArrayElement{document: i.document, index: index})
	}
	return result, true
}

// TomlEntry is the borrowed direct table entry association (consema-toml
// lib.rs).
type TomlEntry struct {
	document *Document
	index    int
}

// Ordinal returns the zero-based direct entry ordinal.
func (e TomlEntry) Ordinal() int { return e.document.entities[e.index].entry.ordinal }

// NodeRef returns the association identity.
func (e TomlEntry) NodeRef() document.NodeRef {
	return e.document.nodeRef(e.index, document.RoleTomlEntry)
}

// KeyNodeRef returns the direct key segment identity.
func (e TomlEntry) KeyNodeRef() document.NodeRef {
	return e.document.nodeRef(e.document.entities[e.index].entry.key, document.RoleTomlKey)
}

// ItemNodeRef returns the associated item identity.
func (e TomlEntry) ItemNodeRef() document.NodeRef {
	return e.document.nodeRef(e.document.entities[e.index].entry.item, document.RoleTomlItem)
}

// Span returns the association source span.
func (e TomlEntry) Span() document.Span { return e.document.entities[e.index].span }

// Name returns the decoded direct key segment without normalization.
func (e TomlEntry) Name() string {
	key := e.document.entities[e.index].entry.key
	return e.document.entities[key].key.name
}

// Item returns the associated native item.
func (e TomlEntry) Item() TomlItem {
	return TomlItem{document: e.document, index: e.document.entities[e.index].entry.item}
}

// TomlArrayElement is the borrowed array or array-of-tables element
// association (consema-toml lib.rs).
type TomlArrayElement struct {
	document *Document
	index    int
}

// Ordinal returns the zero-based direct element ordinal.
func (e TomlArrayElement) Ordinal() int { return e.document.entities[e.index].element.ordinal }

// NodeRef returns the association identity.
func (e TomlArrayElement) NodeRef() document.NodeRef {
	return e.document.nodeRef(e.index, document.RoleTomlArrayElement)
}

// ItemNodeRef returns the associated item identity.
func (e TomlArrayElement) ItemNodeRef() document.NodeRef {
	return e.document.nodeRef(e.document.entities[e.index].element.item, document.RoleTomlItem)
}

// Span returns the association source span.
func (e TomlArrayElement) Span() document.Span { return e.document.entities[e.index].span }

// Item returns the associated native item.
func (e TomlArrayElement) Item() TomlItem {
	return TomlItem{document: e.document, index: e.document.entities[e.index].element.item}
}

// entity and its internal variants mirror the Rust entity model
// (consema-toml lib.rs).
type entity struct {
	span    document.Span
	kind    entityKind
	item    itemEntity
	entry   entryEntity
	key     keyEntity
	element elementEntity
}

type entityKind uint8

const (
	entityItem entityKind = iota
	entityEntry
	entityKey
	entityElement
)

// entryEntity is one table/inline-table key-to-item association.
type entryEntity struct {
	ordinal int
	key     int
	item    int
}

// keyEntity is one decoded direct key segment.
type keyEntity struct {
	name string
}

// elementEntity is one array or array-of-tables element association.
type elementEntity struct {
	ordinal int
	item    int
}

type itemEntity struct {
	kind     itemInternalKind
	str      string
	integer  int64
	bits     uint64
	boolean  bool
	dateTime TomlDateTime
	entries  []int
	elements []int
	flavor   tableFlavor
}

type itemInternalKind uint8

const (
	itemString itemInternalKind = iota
	itemInteger
	itemFloat
	itemBoolean
	itemDateTime
	itemArray
	itemInlineTable
	itemTable
	itemArrayOfTables
)

type tableFlavor uint8

const (
	flavorRoot tableFlavor = iota
	flavorStandard
	flavorImplicit
	flavorDotted
)

func (e *itemEntity) publicKind() TomlItemKind {
	switch e.kind {
	case itemString:
		return ItemKindString
	case itemInteger:
		return ItemKindInteger
	case itemFloat:
		return ItemKindFloat
	case itemBoolean:
		return ItemKindBoolean
	case itemDateTime:
		switch {
		case e.dateTime.Date != nil && e.dateTime.Time != nil && e.dateTime.Offset != nil:
			return ItemKindOffsetDateTime
		case e.dateTime.Date != nil && e.dateTime.Time != nil:
			return ItemKindLocalDateTime
		case e.dateTime.Date != nil:
			return ItemKindLocalDate
		case e.dateTime.Time != nil:
			return ItemKindLocalTime
		}
		return ItemKindLocalTime
	case itemArray:
		return ItemKindArray
	case itemInlineTable:
		return ItemKindInlineTable
	case itemTable:
		switch e.flavor {
		case flavorRoot:
			return ItemKindRootTable
		case flavorStandard:
			return ItemKindStandardTable
		case flavorImplicit:
			return ItemKindImplicitTable
		case flavorDotted:
			return ItemKindDottedTable
		}
	case itemArrayOfTables:
		return ItemKindArrayOfTables
	}
	return ItemKindString
}

func (d *Document) nodeRef(index int, role document.NodeRole) document.NodeRef {
	return d.authority.NodeRef(uint64(index), role)
}

func (d *Document) validateRef(node document.NodeRef, role document.NodeRole) (int, error) {
	if err := d.authority.Verify(node); err != nil {
		return 0, &TomlAccessError{Kind: TomlAccessWrongSnapshot}
	}
	if node.Role() != role {
		return 0, &TomlAccessError{Kind: TomlAccessWrongRole}
	}
	index := int(node.Index())
	if index >= len(d.entities) {
		return 0, &TomlAccessError{Kind: TomlAccessUnknownNode}
	}
	return index, nil
}

func (d *Document) itemEntity(index int) *itemEntity {
	entity := &d.entities[index]
	if entity.kind != entityItem {
		panic("toml: typed TOML item handle")
	}
	return &entity.item
}

func (d *Document) entitySpan(index int) document.Span {
	return d.entities[index].span
}
