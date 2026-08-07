package hcl

import "consema.dev/consema/document"

// HclProfile is a frozen HCL formation profile (RFC 0014 §1).
//
// The profile is selected by the caller before formation; neither the `.tf`
// nor the `.tfvars` extension selects a profile, representation, or
// encoding. The two profiles share one grammar and one native semantic
// model; `hcl.tfvars@1` is `hcl.native@1` under one structural restriction:
// the top-level body admits attributes only, never blocks (RFC 0014 §5).
type HclProfile uint8

// The two frozen HCL profiles.
const (
	// HclProfileNativeV1 is the full HCL Native Syntax (RFC 0014 §4).
	HclProfileNativeV1 HclProfile = iota
	// HclProfileTfvarsV1 is `hcl.native@1` under the tfvars structural
	// restriction (RFC 0014 §5).
	HclProfileTfvarsV1
)

// ID returns the immutable profile identifier.
func (p HclProfile) ID() document.ProfileId {
	switch p {
	case HclProfileNativeV1:
		return document.NewProfileId("hcl.native", 1)
	case HclProfileTfvarsV1:
		return document.NewProfileId("hcl.tfvars", 1)
	}
	return document.NewProfileId("hcl.native", 1)
}

// HclSyntaxKind is the closed HCL lossless syntax-kind set (RFC 0014 §7.2).
//
// Exactly thirty kinds. Every non-empty raw byte of a formed document
// belongs to exactly one ordered structural piece with one of these kinds;
// there is no `Bom` kind because a BOM is excluded at formation (RFC 0014
// §2). `HeredocOpen` covers the `<<`/`<<-` introducer and the marker
// identifier; `HeredocClose` covers the closing marker line.
type HclSyntaxKind uint8

// The closed syntax-piece kinds in source order.
const (
	// HclSyntaxKindWhitespace is space or tab trivia.
	HclSyntaxKindWhitespace HclSyntaxKind = iota
	// HclSyntaxKindLineBreak is an LF or CRLF newline sequence.
	HclSyntaxKindLineBreak
	// HclSyntaxKindLineComment is a `//` or `#` line comment.
	HclSyntaxKindLineComment
	// HclSyntaxKindInlineComment is a `/* ... */` inline comment.
	HclSyntaxKindInlineComment
	// HclSyntaxKindIdentifier is an identifier token.
	HclSyntaxKindIdentifier
	// HclSyntaxKindEquals is `=`.
	HclSyntaxKindEquals
	// HclSyntaxKindNumber is a number literal token.
	HclSyntaxKindNumber
	// HclSyntaxKindStringOpen is a quoted-template opening quote.
	HclSyntaxKindStringOpen
	// HclSyntaxKindStringContent is quoted-template literal content.
	HclSyntaxKindStringContent
	// HclSyntaxKindStringClose is a quoted-template closing quote.
	HclSyntaxKindStringClose
	// HclSyntaxKindInterpolationOpen is `${` with an optional `~` strip
	// marker.
	HclSyntaxKindInterpolationOpen
	// HclSyntaxKindInterpolationContent is the interpolation interior.
	HclSyntaxKindInterpolationContent
	// HclSyntaxKindInterpolationClose is `}` with an optional `~` strip
	// marker.
	HclSyntaxKindInterpolationClose
	// HclSyntaxKindDirectiveOpen is `%{` with an optional `~` strip marker.
	HclSyntaxKindDirectiveOpen
	// HclSyntaxKindDirectiveContent is the directive interior.
	HclSyntaxKindDirectiveContent
	// HclSyntaxKindDirectiveClose is `}` with an optional `~` strip marker.
	HclSyntaxKindDirectiveClose
	// HclSyntaxKindHeredocOpen is a `<<`/`<<-` introducer and marker
	// identifier.
	HclSyntaxKindHeredocOpen
	// HclSyntaxKindHeredocContent is a heredoc content line.
	HclSyntaxKindHeredocContent
	// HclSyntaxKindHeredocClose is the heredoc closing marker line.
	HclSyntaxKindHeredocClose
	// HclSyntaxKindBraceOpen is `{`.
	HclSyntaxKindBraceOpen
	// HclSyntaxKindBraceClose is `}`.
	HclSyntaxKindBraceClose
	// HclSyntaxKindBracketOpen is `[`.
	HclSyntaxKindBracketOpen
	// HclSyntaxKindBracketClose is `]`.
	HclSyntaxKindBracketClose
	// HclSyntaxKindParenOpen is `(`.
	HclSyntaxKindParenOpen
	// HclSyntaxKindParenClose is `)`.
	HclSyntaxKindParenClose
	// HclSyntaxKindComma is `,`.
	HclSyntaxKindComma
	// HclSyntaxKindColon is `:`.
	HclSyntaxKindColon
	// HclSyntaxKindQuestionMark is `?`.
	HclSyntaxKindQuestionMark
	// HclSyntaxKindOperator is an operator token (`-`, `!`, `==`, `!=`,
	// `<`, `>`, `<=`, `>=`, `+`, `*`, `/`, `%`, `&&`, `||`, `.`, `=>`,
	// `...`).
	HclSyntaxKindOperator
	// HclSyntaxKindErrorRegion is a recovered error region.
	HclSyntaxKindErrorRegion
)

// AsStr returns the stable query and protocol name of the kind (RFC 0014
// §7.2).
func (k HclSyntaxKind) AsStr() string {
	switch k {
	case HclSyntaxKindWhitespace:
		return "Whitespace"
	case HclSyntaxKindLineBreak:
		return "LineBreak"
	case HclSyntaxKindLineComment:
		return "LineComment"
	case HclSyntaxKindInlineComment:
		return "InlineComment"
	case HclSyntaxKindIdentifier:
		return "Identifier"
	case HclSyntaxKindEquals:
		return "Equals"
	case HclSyntaxKindNumber:
		return "Number"
	case HclSyntaxKindStringOpen:
		return "StringOpen"
	case HclSyntaxKindStringContent:
		return "StringContent"
	case HclSyntaxKindStringClose:
		return "StringClose"
	case HclSyntaxKindInterpolationOpen:
		return "InterpolationOpen"
	case HclSyntaxKindInterpolationContent:
		return "InterpolationContent"
	case HclSyntaxKindInterpolationClose:
		return "InterpolationClose"
	case HclSyntaxKindDirectiveOpen:
		return "DirectiveOpen"
	case HclSyntaxKindDirectiveContent:
		return "DirectiveContent"
	case HclSyntaxKindDirectiveClose:
		return "DirectiveClose"
	case HclSyntaxKindHeredocOpen:
		return "HeredocOpen"
	case HclSyntaxKindHeredocContent:
		return "HeredocContent"
	case HclSyntaxKindHeredocClose:
		return "HeredocClose"
	case HclSyntaxKindBraceOpen:
		return "BraceOpen"
	case HclSyntaxKindBraceClose:
		return "BraceClose"
	case HclSyntaxKindBracketOpen:
		return "BracketOpen"
	case HclSyntaxKindBracketClose:
		return "BracketClose"
	case HclSyntaxKindParenOpen:
		return "ParenOpen"
	case HclSyntaxKindParenClose:
		return "ParenClose"
	case HclSyntaxKindComma:
		return "Comma"
	case HclSyntaxKindColon:
		return "Colon"
	case HclSyntaxKindQuestionMark:
		return "QuestionMark"
	case HclSyntaxKindOperator:
		return "Operator"
	case HclSyntaxKindErrorRegion:
		return "ErrorRegion"
	}
	return "ErrorRegion"
}

// String returns the stable kind name (the query vocabulary spelling).
func (k HclSyntaxKind) String() string { return k.AsStr() }

// HclSyntaxKindFromName resolves one exact stable kind name (RFC 0014 §7.2).
func HclSyntaxKindFromName(name string) (HclSyntaxKind, bool) {
	switch name {
	case "Whitespace":
		return HclSyntaxKindWhitespace, true
	case "LineBreak":
		return HclSyntaxKindLineBreak, true
	case "LineComment":
		return HclSyntaxKindLineComment, true
	case "InlineComment":
		return HclSyntaxKindInlineComment, true
	case "Identifier":
		return HclSyntaxKindIdentifier, true
	case "Equals":
		return HclSyntaxKindEquals, true
	case "Number":
		return HclSyntaxKindNumber, true
	case "StringOpen":
		return HclSyntaxKindStringOpen, true
	case "StringContent":
		return HclSyntaxKindStringContent, true
	case "StringClose":
		return HclSyntaxKindStringClose, true
	case "InterpolationOpen":
		return HclSyntaxKindInterpolationOpen, true
	case "InterpolationContent":
		return HclSyntaxKindInterpolationContent, true
	case "InterpolationClose":
		return HclSyntaxKindInterpolationClose, true
	case "DirectiveOpen":
		return HclSyntaxKindDirectiveOpen, true
	case "DirectiveContent":
		return HclSyntaxKindDirectiveContent, true
	case "DirectiveClose":
		return HclSyntaxKindDirectiveClose, true
	case "HeredocOpen":
		return HclSyntaxKindHeredocOpen, true
	case "HeredocContent":
		return HclSyntaxKindHeredocContent, true
	case "HeredocClose":
		return HclSyntaxKindHeredocClose, true
	case "BraceOpen":
		return HclSyntaxKindBraceOpen, true
	case "BraceClose":
		return HclSyntaxKindBraceClose, true
	case "BracketOpen":
		return HclSyntaxKindBracketOpen, true
	case "BracketClose":
		return HclSyntaxKindBracketClose, true
	case "ParenOpen":
		return HclSyntaxKindParenOpen, true
	case "ParenClose":
		return HclSyntaxKindParenClose, true
	case "Comma":
		return HclSyntaxKindComma, true
	case "Colon":
		return HclSyntaxKindColon, true
	case "QuestionMark":
		return HclSyntaxKindQuestionMark, true
	case "Operator":
		return HclSyntaxKindOperator, true
	case "ErrorRegion":
		return HclSyntaxKindErrorRegion, true
	}
	return 0, false
}

// HclParseLimits are the HCL-specific formation, structure, recovery, and
// report limits (RFC 0014 §11).
//
// The common limits bound source bytes, generic nesting, token and node
// counts, and diagnostics; the flat fields bound the HCL-specific facts:
// decoded text, body/expression/template depth, per-body item counts,
// identifier/string/number/template/heredoc lengths, constructor extents,
// and recovery/error/piece/report counts. Every limit failure is a fatal
// formation failure or an atomic operation failure; a limit failure never
// masquerades as an empty body, truncated expression, shortened query,
// partial target, or successful edit (hard gate 4). All size arithmetic is
// checked before allocation.
type HclParseLimits struct {
	// Common bounds source bytes, generic nesting, token and node counts,
	// and diagnostics; includes MaxSourceBytes and MaxDiagnostics.
	Common document.ParseLimits
	// MaxDecodedUTF8Bytes is the maximum decoded UTF-8 bytes.
	MaxDecodedUTF8Bytes int
	// MaxDecodedScalars is the maximum decoded Unicode scalars.
	MaxDecodedScalars int
	// MaxBodyDepth is the maximum body nesting depth (block nesting; the
	// root body is depth 1).
	MaxBodyDepth int
	// MaxExpressionDepth is the maximum expression depth (the parse
	// recursion budget, shared by structural equality and the literal
	// predicate).
	MaxExpressionDepth int
	// MaxTemplateDepth is the maximum template nesting depth.
	MaxTemplateDepth int
	// MaxAttributeCount is the maximum attributes in one body.
	MaxAttributeCount int
	// MaxBlockCount is the maximum blocks in one body.
	MaxBlockCount int
	// MaxLabelCount is the maximum labels on one block.
	MaxLabelCount int
	// MaxBodyItemCount is the maximum body items (attributes plus blocks)
	// in one body.
	MaxBodyItemCount int
	// MaxIdentifierLen is the maximum identifier byte length.
	MaxIdentifierLen int
	// MaxStringLen is the maximum quoted-template byte length.
	MaxStringLen int
	// MaxNumberDigits is the maximum canonical-decimal digit count of one
	// number.
	MaxNumberDigits int
	// MaxTemplateLen is the maximum template (quoted or heredoc content)
	// byte length.
	MaxTemplateLen int
	// MaxTemplateInterpolations is the maximum interpolation or directive
	// sequences in one template.
	MaxTemplateInterpolations int
	// MaxHeredocLines is the maximum lines in one heredoc.
	MaxHeredocLines int
	// MaxHeredocBytes is the maximum heredoc bytes; bounds the error region
	// of an unterminated heredoc (RFC 0014 §3, §11).
	MaxHeredocBytes int
	// MaxTupleElements is the maximum elements in one tuple constructor.
	MaxTupleElements int
	// MaxObjectEntries is the maximum entries in one object constructor.
	MaxObjectEntries int
	// MaxForExtent is the maximum extent of one for-expression.
	MaxForExtent int
	// MaxRecoveryRegions is the maximum recovery regions in one document.
	MaxRecoveryRegions int
	// MaxErrorRegions is the maximum error regions in one document.
	MaxErrorRegions int
	// MaxSyntaxPieces is the maximum lossless syntax pieces in one
	// document (RFC 0014 §7.2).
	MaxSyntaxPieces int
	// MaxReportEvents is the maximum projection, materialization, or edit
	// report events.
	MaxReportEvents int
}

// DefaultHclParseLimits returns the frozen defaults (RFC 0014 §11): the
// recursive depth budgets are stack-safe by measurement with at least a
// 2.5x margin (24 expression and 128 body levels); the flat count limits
// never recurse, so they stay generous.
func DefaultHclParseLimits() HclParseLimits {
	return HclParseLimits{
		Common:                    document.DefaultParseLimits(),
		MaxDecodedUTF8Bytes:       128 * 1024 * 1024,
		MaxDecodedScalars:         64 * 1024 * 1024,
		MaxBodyDepth:              128,
		MaxExpressionDepth:        24,
		MaxTemplateDepth:          256,
		MaxAttributeCount:         1_000_000,
		MaxBlockCount:             1_000_000,
		MaxLabelCount:             1_000_000,
		MaxBodyItemCount:          1_000_000,
		MaxIdentifierLen:          1024,
		MaxStringLen:              16 * 1024 * 1024,
		MaxNumberDigits:           100_000,
		MaxTemplateLen:            16 * 1024 * 1024,
		MaxTemplateInterpolations: 1_000_000,
		MaxHeredocLines:           1_000_000,
		MaxHeredocBytes:           16 * 1024 * 1024,
		MaxTupleElements:          1_000_000,
		MaxObjectEntries:          1_000_000,
		MaxForExtent:              1_000_000,
		MaxRecoveryRegions:        100_000,
		MaxErrorRegions:           100_000,
		MaxSyntaxPieces:           2_000_000,
		MaxReportEvents:           100_000,
	}
}

// HclEncodingSelection is the explicit source-encoding selection for the
// UTF-8-only HCL source contract (RFC 0014 §2).
//
// HCL has no declaration, prolog, or encoding negotiation: the encoding is
// always UTF-8 and always selected before formation. UTF-16, UTF-32,
// Latin-1, Windows code pages, and any other encoding are explicit v1
// exclusions. HclEncodingSelectionProfileDefault and
// HclEncodingSelectionExplicit(document.Utf8Encoding()) are consistent
// with the profile; any other explicit encoding is a source-contract
// conflict at formation.
type HclEncodingSelection struct {
	explicit *document.SourceEncoding
}

// HclEncodingSelectionProfileDefault applies the frozen profile default:
// UTF-8.
func HclEncodingSelectionProfileDefault() HclEncodingSelection {
	return HclEncodingSelection{}
}

// HclEncodingSelectionExplicit uses one caller-selected encoding; only
// document.Utf8Encoding() is consistent with the HCL source contract.
func HclEncodingSelectionExplicit(encoding document.SourceEncoding) HclEncodingSelection {
	return HclEncodingSelection{explicit: &encoding}
}

// Validate checks the selection against the UTF-8-only source contract and
// returns the effective encoding. A non-UTF-8 explicit selection is a
// source-contract conflict and a v1 exclusion (RFC 0014 §2).
func (s HclEncodingSelection) Validate() (document.SourceEncoding, bool) {
	if s.explicit == nil || *s.explicit == document.Utf8Encoding() {
		return document.Utf8Encoding(), true
	}
	return document.Utf8Encoding(), false
}
