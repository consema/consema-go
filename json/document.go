package json

import (
	"math/big"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// LosslessStructuralIndex is the exhaustive token/trivia/error-region
// byte coverage of one JSON-family document (consema-document
// LosslessStructuralIndex; consema-json lib.rs:229-239). Every source byte
// belongs to exactly one piece, in source order.
type LosslessStructuralIndex struct {
	pieces []structuralPiece
}

// structuralPiece is one exact source piece: a token, trivia, or error
// region.
type structuralPiece struct {
	span document.Span
	kind structuralPieceKind
}

// structuralPieceKind is the closed piece classification.
type structuralPieceKind uint8

const (
	pieceToken structuralPieceKind = iota
	pieceTrivia
	pieceErrorRegion
)

// Pieces returns the ordered exhaustive pieces. The returned slice is a
// copy; pieces are logically immutable.
func (i *LosslessStructuralIndex) Pieces() []StructuralPiece {
	pieces := make([]StructuralPiece, 0, len(i.pieces))
	for _, piece := range i.pieces {
		pieces = append(pieces, StructuralPiece{span: piece.span, kind: piece.kind})
	}
	return pieces
}

// StructuralPiece is one exact source piece with its span and class
// (consema-document StructuralPiece).
type StructuralPiece struct {
	span document.Span
	kind structuralPieceKind
}

// Span returns the exact raw byte range.
func (p StructuralPiece) Span() document.Span { return p.span }

// KindName returns the stable piece class: "Token", "Trivia", or
// "ErrorRegion".
func (p StructuralPiece) KindName() string {
	switch p.kind {
	case pieceToken:
		return "Token"
	case pieceTrivia:
		return "Trivia"
	case pieceErrorRegion:
		return "ErrorRegion"
	}
	return "Token"
}

// Document is an opaque immutable JSON-family document snapshot
// (consema-json lib.rs:170-183). Completed documents are logically
// immutable and safe for concurrent reads.
type Document struct {
	authority       document.DocumentAuthority
	source          *document.SourceSnapshot
	profile         JsonProfile
	structuralIndex *LosslessStructuralIndex
	syntaxKinds     []JsonSyntaxKind
	formationStatus document.FormationStatus
	diagnostics     []*protocol.Diagnostic
	entities        []entity
	root            int
	parseLimits     document.ParseLimits
}

// SnapshotIdentity returns the snapshot identity to which every NodeRef
// and Span belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	return d.authority.Identity()
}

// Source returns the exact immutable source snapshot.
func (d *Document) Source() *document.SourceSnapshot { return d.source }

// Render returns the exact current source bytes. The returned slice is a
// copy; the raw bytes are owned by the immutable source snapshot.
func (d *Document) Render() []byte { return d.source.Bytes() }

// FormatFamily returns the JSON format family contract.
func (d *Document) FormatFamily() document.FormatFamilyId {
	return document.NewFormatFamilyId("json", 1)
}

// Profile returns the exact language profile.
func (d *Document) Profile() document.ProfileId { return d.profile.ID() }

// FormationStatus returns whether recovery structure was required.
func (d *Document) FormationStatus() document.FormationStatus { return d.formationStatus }

// Diagnostics returns the deterministically ordered document diagnostics.
// The returned slice must not be modified.
func (d *Document) Diagnostics() []*protocol.Diagnostic { return d.diagnostics }

// LosslessStructuralIndex returns the exhaustive piece coverage.
func (d *Document) LosslessStructuralIndex() *LosslessStructuralIndex {
	return d.structuralIndex
}

// LosslessSyntaxKinds returns the format-specific kind for every
// structural piece, in the same source order. The returned slice must not
// be modified.
func (d *Document) LosslessSyntaxKinds() []JsonSyntaxKind { return d.syntaxKinds }

// Root returns the root native semantic value handle. Recovered roots can
// report local semantic unavailability.
func (d *Document) Root() JsonValue {
	return JsonValue{document: d, index: d.root}
}

// entity is one structural association: a value, an object member, or an
// array element (consema-json lib.rs:623-674).
type entity struct {
	value   *valueEntity
	member  *memberEntity
	element *elementEntity
}

// valueEntity is one complete or recovered value syntax node.
type valueEntity struct {
	span        document.Span
	literalSpan *document.Span
	complete    bool
	kind        internalValueKind
}

// internalValueKind is the closed native value category of one value
// entity, including regional unavailability.
type internalValueKind struct {
	tag        internalKindTag
	boolean    bool
	integer    *big.Int
	decimal    core.Decimal
	binary64   core.BinaryFloat64
	stringText string
	array      []int
	object     []int
	unavail    SemanticUnavailable
}

// internalKindTag is the closed internal kind tag.
type internalKindTag uint8

const (
	internalNull internalKindTag = iota
	internalBoolean
	internalInteger
	internalDecimal
	internalBinaryFloat64
	internalString
	internalArray
	internalObject
	internalUnavailable
)

// memberEntity is one object member association.
type memberEntity struct {
	span    document.Span
	key     int
	value   int
	ordinal int
}

// elementEntity is one array element association.
type elementEntity struct {
	span    document.Span
	value   int
	ordinal int
}

// JsonValue is a borrowed typed native semantic value bound to one
// Document snapshot (consema-json lib.rs:342-493).
type JsonValue struct {
	document *Document
	index    int
}

// NodeRef returns the exact value node handle.
func (v JsonValue) NodeRef() document.NodeRef {
	return v.document.nodeRef(v.index, document.RoleValue)
}

// Span returns the exact syntax span, possibly zero-width for a missing
// recovered node.
func (v JsonValue) Span() document.Span { return v.document.span(v.index) }

// Kind returns the native semantic category when available.
func (v JsonValue) Kind() SemanticAvailability[JsonValueKind] {
	kind := v.document.valueEntity(v.index).kind
	switch kind.tag {
	case internalNull:
		return Available(JsonValueKindNull)
	case internalBoolean:
		return Available(JsonValueKindBoolean)
	case internalInteger:
		return Available(JsonValueKindInteger)
	case internalDecimal:
		return Available(JsonValueKindDecimal)
	case internalBinaryFloat64:
		return Available(JsonValueKindBinaryFloat64)
	case internalString:
		return Available(JsonValueKindString)
	case internalArray:
		return Available(JsonValueKindArray)
	case internalObject:
		return Available(JsonValueKindObject)
	case internalUnavailable:
		return Unavailable[JsonValueKind](kind.unavail)
	}
	return Unavailable[JsonValueKind](SemanticUnavailableMissing)
}

// AsBoolean returns the boolean value; the nil pointer means the value is
// not a Boolean.
func (v JsonValue) AsBoolean() SemanticAvailability[*bool] {
	kind := v.document.valueEntity(v.index).kind
	switch kind.tag {
	case internalBoolean:
		return Available(&kind.boolean)
	case internalUnavailable:
		return Unavailable[*bool](kind.unavail)
	}
	return Available((*bool)(nil))
}

// AsInteger returns the exact arbitrary-precision integer; the nil pointer
// means the value is not an Integer. The returned big.Int must not be
// modified.
func (v JsonValue) AsInteger() SemanticAvailability[*big.Int] {
	kind := v.document.valueEntity(v.index).kind
	switch kind.tag {
	case internalInteger:
		return Available(kind.integer)
	case internalUnavailable:
		return Unavailable[*big.Int](kind.unavail)
	}
	return Available((*big.Int)(nil))
}

// AsDecimal returns the exact normalized decimal; the nil pointer means
// the value is not a Decimal.
func (v JsonValue) AsDecimal() SemanticAvailability[*core.Decimal] {
	kind := v.document.valueEntity(v.index).kind
	switch kind.tag {
	case internalDecimal:
		decimal := kind.decimal
		return Available(&decimal)
	case internalUnavailable:
		return Unavailable[*core.Decimal](kind.unavail)
	}
	return Available((*core.Decimal)(nil))
}

// AsBinaryFloat64 returns the exact IEEE-754 binary64 datum used by JSON5
// non-finite literals; the nil pointer means the value is not a
// BinaryFloat64.
func (v JsonValue) AsBinaryFloat64() SemanticAvailability[*core.BinaryFloat64] {
	kind := v.document.valueEntity(v.index).kind
	switch kind.tag {
	case internalBinaryFloat64:
		binary := kind.binary64
		return Available(&binary)
	case internalUnavailable:
		return Unavailable[*core.BinaryFloat64](kind.unavail)
	}
	return Available((*core.BinaryFloat64)(nil))
}

// AsString returns the decoded Unicode string without normalization; the
// nil pointer means the value is not a String.
func (v JsonValue) AsString() SemanticAvailability[*string] {
	kind := v.document.valueEntity(v.index).kind
	switch kind.tag {
	case internalString:
		return Available(&kind.stringText)
	case internalUnavailable:
		return Unavailable[*string](kind.unavail)
	}
	return Available((*string)(nil))
}

// ArrayElements returns the ordered array elements; the nil slice means
// the value is not an Array.
func (v JsonValue) ArrayElements() SemanticAvailability[[]JsonArrayElement] {
	kind := v.document.valueEntity(v.index).kind
	switch kind.tag {
	case internalArray:
		elements := make([]JsonArrayElement, 0, len(kind.array))
		for _, index := range kind.array {
			elements = append(elements, JsonArrayElement{document: v.document, index: index})
		}
		return Available(elements)
	case internalUnavailable:
		return Unavailable[[]JsonArrayElement](kind.unavail)
	}
	return Available([]JsonArrayElement(nil))
}

// ObjectMembers returns the ordered object members without duplicate
// collapse; the nil slice means the value is not an Object.
func (v JsonValue) ObjectMembers() SemanticAvailability[[]JsonObjectMember] {
	kind := v.document.valueEntity(v.index).kind
	switch kind.tag {
	case internalObject:
		members := make([]JsonObjectMember, 0, len(kind.object))
		for _, index := range kind.object {
			members = append(members, JsonObjectMember{document: v.document, index: index})
		}
		return Available(members)
	case internalUnavailable:
		return Unavailable[[]JsonObjectMember](kind.unavail)
	}
	return Available([]JsonObjectMember(nil))
}

// JsonObjectMember is a borrowed JSON object member association
// (consema-json lib.rs:495-561).
type JsonObjectMember struct {
	document *Document
	index    int
}

// Ordinal returns the zero-based structural member ordinal.
func (m JsonObjectMember) Ordinal() int { return m.document.entity(m.index).member.ordinal }

// NodeRef returns the member association identity.
func (m JsonObjectMember) NodeRef() document.NodeRef {
	return m.document.nodeRef(m.index, document.RoleObjectMember)
}

// KeyNodeRef returns the key node identity.
func (m JsonObjectMember) KeyNodeRef() document.NodeRef {
	return m.document.nodeRef(m.document.entity(m.index).member.key, document.RoleObjectKey)
}

// ValueNodeRef returns the value node identity.
func (m JsonObjectMember) ValueNodeRef() document.NodeRef {
	return m.document.nodeRef(m.document.entity(m.index).member.value, document.RoleValue)
}

// Span returns the whole member source span.
func (m JsonObjectMember) Span() document.Span { return m.document.span(m.index) }

// Name returns the decoded member name.
func (m JsonObjectMember) Name() SemanticAvailability[*string] {
	key := m.document.valueEntity(m.document.entity(m.index).member.key)
	switch key.kind.tag {
	case internalString:
		return Available(&key.kind.stringText)
	case internalUnavailable:
		return Unavailable[*string](key.kind.unavail)
	}
	return Unavailable[*string](SemanticUnavailableInvalidLiteral)
}

// Value returns the associated value handle.
func (m JsonObjectMember) Value() JsonValue {
	return JsonValue{document: m.document, index: m.document.entity(m.index).member.value}
}

// JsonArrayElement is a borrowed JSON array element association
// (consema-json lib.rs:563-610).
type JsonArrayElement struct {
	document *Document
	index    int
}

// Ordinal returns the zero-based structural index.
func (e JsonArrayElement) Ordinal() int { return e.document.entity(e.index).element.ordinal }

// NodeRef returns the element association identity.
func (e JsonArrayElement) NodeRef() document.NodeRef {
	return e.document.nodeRef(e.index, document.RoleArrayElement)
}

// ValueNodeRef returns the associated value identity.
func (e JsonArrayElement) ValueNodeRef() document.NodeRef {
	return e.document.nodeRef(e.document.entity(e.index).element.value, document.RoleValue)
}

// Span returns the whole element span.
func (e JsonArrayElement) Span() document.Span { return e.document.span(e.index) }

// Value returns the element value handle.
func (e JsonArrayElement) Value() JsonValue {
	return JsonValue{document: e.document, index: e.document.entity(e.index).element.value}
}

// entity returns one structural entity.
func (d *Document) entity(index int) *entity { return &d.entities[index] }

// valueEntity returns one value entity; the handle must be a value entity.
func (d *Document) valueEntity(index int) *valueEntity {
	return d.entities[index].value
}

// nodeRef issues the typed node handle for one entity.
func (d *Document) nodeRef(index int, role document.NodeRole) document.NodeRef {
	return d.authority.NodeRef(uint64(index), role)
}

// span returns the exact entity span.
func (d *Document) span(index int) document.Span {
	item := &d.entities[index]
	switch {
	case item.value != nil:
		return item.value.span
	case item.member != nil:
		return item.member.span
	default:
		return item.element.span
	}
}

// validateRef verifies one handle against this snapshot and the permitted
// roles and resolves its entity index.
func (d *Document) validateRef(node document.NodeRef, roles []document.NodeRole) (int, error) {
	if err := d.authority.Verify(node); err != nil {
		return 0, &JsonAccessError{Kind: JsonAccessErrorWrongSnapshot}
	}
	permitted := false
	for _, role := range roles {
		if node.Role() == role {
			permitted = true
			break
		}
	}
	if !permitted {
		return 0, &JsonAccessError{Kind: JsonAccessErrorWrongRole}
	}
	index := int(node.Index())
	if index >= len(d.entities) {
		return 0, &JsonAccessError{Kind: JsonAccessErrorUnknownNode}
	}
	return index, nil
}
