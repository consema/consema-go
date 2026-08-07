package json

import "consema.dev/consema/document"

// JsonProfile is a frozen JSON-family language profile (consema-json
// lib.rs:36-45).
type JsonProfile uint8

// The three frozen JSON-family profiles.
const (
	// JsonProfileStrictV1 is RFC-style strict JSON plus the baseline
	// duplicate/BOM diagnostics.
	JsonProfileStrictV1 JsonProfile = iota
	// JsonProfileJsoncBoundedV1 is strict JSON plus comments, trailing
	// commas, and an optional leading BOM.
	JsonProfileJsoncBoundedV1
	// JsonProfileJson5StandardV1 is Standard JSON5 1.0.0 plus bounded
	// Consema resource behavior.
	JsonProfileJson5StandardV1
)

// ID returns the immutable profile identifier.
func (p JsonProfile) ID() document.ProfileId {
	switch p {
	case JsonProfileStrictV1:
		return document.NewProfileId("json.strict", 1)
	case JsonProfileJsoncBoundedV1:
		return document.NewProfileId("jsonc.bounded", 1)
	case JsonProfileJson5StandardV1:
		return document.NewProfileId("json5.standard", 1)
	}
	return document.NewProfileId("json.strict", 1)
}

// permitsJSONCExtensions reports whether bounded comments and trailing
// commas are accepted (consema-json lib.rs:148-153).
func (p JsonProfile) permitsJSONCExtensions() bool {
	return p == JsonProfileJsoncBoundedV1 || p == JsonProfileJson5StandardV1
}

// isJSON5 reports whether the Standard JSON5 lexical surface is accepted
// (consema-json lib.rs:154-159).
func (p JsonProfile) isJSON5() bool {
	return p == JsonProfileJson5StandardV1
}

// JsonSyntaxKind is the closed JSON/JSONC v1 lossless syntax-piece
// classification (consema-json lib.rs:47-84).
type JsonSyntaxKind uint8

// The closed syntax-piece kinds in source order.
const (
	// JsonSyntaxKindBom is a leading UTF-8 byte-order mark.
	JsonSyntaxKindBom JsonSyntaxKind = iota
	// JsonSyntaxKindWhitespace is JSON whitespace.
	JsonSyntaxKindWhitespace
	// JsonSyntaxKindLineComment is a `//` comment.
	JsonSyntaxKindLineComment
	// JsonSyntaxKindBlockComment is a closed `/* ... */` comment.
	JsonSyntaxKindBlockComment
	// JsonSyntaxKindLeftBrace is `{`.
	JsonSyntaxKindLeftBrace
	// JsonSyntaxKindRightBrace is `}`.
	JsonSyntaxKindRightBrace
	// JsonSyntaxKindLeftBracket is `[`.
	JsonSyntaxKindLeftBracket
	// JsonSyntaxKindRightBracket is `]`.
	JsonSyntaxKindRightBracket
	// JsonSyntaxKindColon is `:`.
	JsonSyntaxKindColon
	// JsonSyntaxKindComma is `,`.
	JsonSyntaxKindComma
	// JsonSyntaxKindString is a complete string token.
	JsonSyntaxKindString
	// JsonSyntaxKindIdentifier is a complete JSON5 IdentifierName token.
	JsonSyntaxKindIdentifier
	// JsonSyntaxKindNumber is a valid JSON number token.
	JsonSyntaxKindNumber
	// JsonSyntaxKindTrue is `true`.
	JsonSyntaxKindTrue
	// JsonSyntaxKindFalse is `false`.
	JsonSyntaxKindFalse
	// JsonSyntaxKindNull is `null`.
	JsonSyntaxKindNull
	// JsonSyntaxKindErrorRegion is bytes retained after bounded lexical
	// recovery.
	JsonSyntaxKindErrorRegion
)

// AsStr returns the stable query and protocol name of the kind.
func (k JsonSyntaxKind) AsStr() string {
	switch k {
	case JsonSyntaxKindBom:
		return "Bom"
	case JsonSyntaxKindWhitespace:
		return "Whitespace"
	case JsonSyntaxKindLineComment:
		return "LineComment"
	case JsonSyntaxKindBlockComment:
		return "BlockComment"
	case JsonSyntaxKindLeftBrace:
		return "LeftBrace"
	case JsonSyntaxKindRightBrace:
		return "RightBrace"
	case JsonSyntaxKindLeftBracket:
		return "LeftBracket"
	case JsonSyntaxKindRightBracket:
		return "RightBracket"
	case JsonSyntaxKindColon:
		return "Colon"
	case JsonSyntaxKindComma:
		return "Comma"
	case JsonSyntaxKindString:
		return "String"
	case JsonSyntaxKindIdentifier:
		return "Identifier"
	case JsonSyntaxKindNumber:
		return "Number"
	case JsonSyntaxKindTrue:
		return "True"
	case JsonSyntaxKindFalse:
		return "False"
	case JsonSyntaxKindNull:
		return "Null"
	case JsonSyntaxKindErrorRegion:
		return "ErrorRegion"
	}
	return "ErrorRegion"
}

// String returns the stable kind name (the query vocabulary spelling).
func (k JsonSyntaxKind) String() string { return k.AsStr() }

// JsonSyntaxKindFromName resolves one exact stable kind name
// (consema-json lib.rs:111-135).
func JsonSyntaxKindFromName(name string) (JsonSyntaxKind, bool) {
	switch name {
	case "Bom":
		return JsonSyntaxKindBom, true
	case "Whitespace":
		return JsonSyntaxKindWhitespace, true
	case "LineComment":
		return JsonSyntaxKindLineComment, true
	case "BlockComment":
		return JsonSyntaxKindBlockComment, true
	case "LeftBrace":
		return JsonSyntaxKindLeftBrace, true
	case "RightBrace":
		return JsonSyntaxKindRightBrace, true
	case "LeftBracket":
		return JsonSyntaxKindLeftBracket, true
	case "RightBracket":
		return JsonSyntaxKindRightBracket, true
	case "Colon":
		return JsonSyntaxKindColon, true
	case "Comma":
		return JsonSyntaxKindComma, true
	case "String":
		return JsonSyntaxKindString, true
	case "Identifier":
		return JsonSyntaxKindIdentifier, true
	case "Number":
		return JsonSyntaxKindNumber, true
	case "True":
		return JsonSyntaxKindTrue, true
	case "False":
		return JsonSyntaxKindFalse, true
	case "Null":
		return JsonSyntaxKindNull, true
	case "ErrorRegion":
		return JsonSyntaxKindErrorRegion, true
	}
	return 0, false
}
