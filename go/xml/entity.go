package xml

// This file implements the safe internal DTD/entity boundary (RFC 0012
// §3; consema-rs/consema-xml/src/entity.rs). The Profile permits no DOCTYPE or
// an internal-only DOCTYPE with a bounded subset. External subsets,
// external/unparsed/parameter entities, notation, and
// `ELEMENT`/`ATTLIST`/conditional declarations never trigger fallback
// behavior. Expansion is guarded before and during allocation across the
// whole document, not independently per reference.

// PredefinedEntity is one predefined XML entity (entity.rs:9-16).
type PredefinedEntity struct {
	// Name is the entity name without the `&` and `;`.
	Name string
	// Value is the replacement character data.
	Value string
}

// PredefinedEntities are the five predefined entities, always available
// with their XML meanings (entity.rs:19-40).
var PredefinedEntities = []PredefinedEntity{
	{Name: "lt", Value: "<"},
	{Name: "gt", Value: ">"},
	{Name: "amp", Value: "&"},
	{Name: "apos", Value: "'"},
	{Name: "quot", Value: "\""},
}

// PredefinedValue returns the replacement value of a predefined entity by
// exact name (entity.rs:44-49).
func PredefinedValue(name string) (string, bool) {
	for _, entity := range PredefinedEntities {
		if entity.Name == name {
			return entity.Value, true
		}
	}
	return "", false
}

// IsXMLChar reports whether c is a legal XML 1.0 character (entity.rs:52-59).
func IsXMLChar(c rune) bool {
	switch value := uint32(c); {
	case value == 0x09 || value == 0x0A || value == 0x0D:
		return true
	case value >= 0x20 && value <= 0xD7FF:
		return true
	case value >= 0xE000 && value <= 0xFFFD:
		return true
	case value >= 0x0001_0000 && value <= 0x0010_FFFF:
		return true
	}
	return false
}

// ReplacementErrorKind classifies a replacement-text validation failure
// (entity.rs:61-72).
type ReplacementErrorKind uint8

// The closed replacement failure classes.
const (
	// ReplacementErrorContainsMarkup: the replacement text contains `<`,
	// which would create entity-generated markup.
	ReplacementErrorContainsMarkup ReplacementErrorKind = iota
	// ReplacementErrorIllegalCharacter: the replacement text contains an
	// illegal XML 1.0 character.
	ReplacementErrorIllegalCharacter
)

// ReplacementError is one replacement-text validation failure.
type ReplacementError struct {
	// Kind identifies the failure.
	Kind ReplacementErrorKind
	// Scalar is the offending Unicode scalar value of IllegalCharacter.
	Scalar rune
}

// Error implements error; the text is human presentation only.
func (e *ReplacementError) Error() string {
	switch e.Kind {
	case ReplacementErrorContainsMarkup:
		return "xml: entity replacement text would create markup"
	case ReplacementErrorIllegalCharacter:
		return "xml: illegal character in entity replacement text"
	}
	return "xml: replacement text failure"
}

// replacementCode maps one replacement-text failure to its stable
// diagnostic code (parser.rs:783-794).
func replacementCode(err *ReplacementError) string {
	switch err.Kind {
	case ReplacementErrorContainsMarkup:
		return "xml.entity.markup@1"
	case ReplacementErrorIllegalCharacter:
		return "xml.entity.illegal-character@1"
	}
	return "xml.entity.markup@1"
}

// Code returns the stable family code of the failure (RFC 0016 §6;
// parser.rs:783-794).
func (e *ReplacementError) Code() string { return replacementCode(e) }

// ValidateReplacementText validates one internal general entity value
// (entity.rs:74-89). An admitted value may contain character data,
// character references, predefined entity references, or references to
// another admitted internal general entity, but never `<`.
func ValidateReplacementText(text string) *ReplacementError {
	for _, c := range text {
		if c == '<' {
			return &ReplacementError{Kind: ReplacementErrorContainsMarkup}
		}
	}
	for _, c := range text {
		if !IsXMLChar(c) {
			return &ReplacementError{Kind: ReplacementErrorIllegalCharacter, Scalar: c}
		}
	}
	return nil
}

// ExpansionBreachKind is an entity expansion breach category
// (entity.rs:91-106).
type ExpansionBreachKind uint8

// The closed breach categories.
const (
	// ExpansionBreachDeclarationLimit: too many entity declarations.
	ExpansionBreachDeclarationLimit ExpansionBreachKind = iota
	// ExpansionBreachReferenceLimit: too many entity references.
	ExpansionBreachReferenceLimit
	// ExpansionBreachDepthLimit: reference expansion depth exceeded.
	ExpansionBreachDepthLimit
	// ExpansionBreachExpandedBytes: expanded bytes exceed the document-wide
	// budget.
	ExpansionBreachExpandedBytes
	// ExpansionBreachExpandedScalars: expanded scalars exceed the
	// document-wide budget.
	ExpansionBreachExpandedScalars
	// ExpansionBreachAmplification: expanded/declared byte amplification
	// exceeds the ratio.
	ExpansionBreachAmplification
)

// ExpansionBreach is one entity expansion breach.
type ExpansionBreach struct {
	// Kind identifies the breach.
	Kind ExpansionBreachKind
}

// EntityExpansionLimits are the entity expansion limits derived from
// XmlParseLimits (entity.rs:108-123).
type EntityExpansionLimits struct {
	// MaxDeclarations is the maximum entity declarations.
	MaxDeclarations int
	// MaxReferences is the maximum entity references.
	MaxReferences int
	// MaxExpansionDepth is the maximum reference expansion depth.
	MaxExpansionDepth int
	// MaxExpandedBytes is the maximum expanded bytes across the whole
	// document.
	MaxExpandedBytes int
	// MaxExpandedScalars is the maximum expanded scalars across the whole
	// document.
	MaxExpandedScalars int
	// MaxAmplificationRatio is the maximum expanded/declared byte
	// amplification ratio.
	MaxAmplificationRatio uint64
}

// EntityExpansionState is the document-wide entity expansion accounting
// (entity.rs:125-145). Counters apply across the whole document, not
// independently per reference, so an attack cannot split its budget across
// references.
type EntityExpansionState struct {
	// Declarations is the collected internal general entity declarations.
	Declarations int
	// References is the total references resolved.
	References int
	// DeclaredBytes is the sum of declared replacement bytes.
	DeclaredBytes int
	// DeclaredScalars is the sum of replacement scalars over all
	// declarations.
	DeclaredScalars int
	// ExpandedBytes is the total expanded bytes emitted.
	ExpandedBytes int
	// ExpandedScalars is the total expanded scalars emitted.
	ExpandedScalars int
	// ExpansionDepth is the current reference nesting depth.
	ExpansionDepth int
}

// NewEntityExpansionState creates an empty accounting state.
func NewEntityExpansionState() EntityExpansionState {
	return EntityExpansionState{}
}

// RecordDeclaration records one collected declaration with its replacement
// text size (entity.rs:147-168).
func (s *EntityExpansionState) RecordDeclaration(replacementBytes, replacementScalars int,
	limits EntityExpansionLimits) *ExpansionBreach {
	if s.Declarations >= limits.MaxDeclarations {
		return &ExpansionBreach{Kind: ExpansionBreachDeclarationLimit}
	}
	s.Declarations++
	s.DeclaredBytes = saturatingAdd(s.DeclaredBytes, replacementBytes)
	s.DeclaredScalars = saturatingAdd(s.DeclaredScalars, replacementScalars)
	return nil
}

// EnterReference enters one reference expansion and accounts its resolved
// size (entity.rs:170-197).
func (s *EntityExpansionState) EnterReference(expandedBytes, expandedScalars int,
	limits EntityExpansionLimits) *ExpansionBreach {
	if s.References >= limits.MaxReferences {
		return &ExpansionBreach{Kind: ExpansionBreachReferenceLimit}
	}
	if s.ExpansionDepth >= limits.MaxExpansionDepth {
		return &ExpansionBreach{Kind: ExpansionBreachDepthLimit}
	}
	s.References++
	s.ExpansionDepth++
	s.ExpandedBytes = saturatingAdd(s.ExpandedBytes, expandedBytes)
	s.ExpandedScalars = saturatingAdd(s.ExpandedScalars, expandedScalars)
	if s.ExpandedBytes > limits.MaxExpandedBytes {
		return &ExpansionBreach{Kind: ExpansionBreachExpandedBytes}
	}
	if s.ExpandedScalars > limits.MaxExpandedScalars {
		return &ExpansionBreach{Kind: ExpansionBreachExpandedScalars}
	}
	if s.ExpandedBytes > s.amplificationBound(limits) {
		return &ExpansionBreach{Kind: ExpansionBreachAmplification}
	}
	return nil
}

// LeaveReference leaves one completed reference expansion
// (entity.rs:199-202).
func (s *EntityExpansionState) LeaveReference() {
	if s.ExpansionDepth > 0 {
		s.ExpansionDepth--
	}
}

func (s *EntityExpansionState) amplificationBound(limits EntityExpansionLimits) int {
	ratio := limits.MaxAmplificationRatio
	if ratio > uint64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return saturatingMul(s.DeclaredBytes, int(ratio))
}

func saturatingAdd(left, right int) int {
	if right > 0 && left > int(^uint(0)>>1)-right {
		return int(^uint(0) >> 1)
	}
	return left + right
}

func saturatingMul(left int, right int) int {
	if left == 0 || right == 0 {
		return 0
	}
	if left > int(^uint(0)>>1)/right {
		return int(^uint(0) >> 1)
	}
	return left * right
}
