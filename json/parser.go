package json

import (
	"context"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the lossless JSON/JSONC/JSON5 lexer and parser
// (crates/consema-json/src/parser.rs). The language-neutral surface —
// token/trivia/error-region coverage, recovery diagnostics with their
// codes and categories, native value categories, and the JSON5 lexical
// extensions — mirrors the Rust semantics; the Go structure is
// independent.

// tokenKind is the closed lexical token vocabulary.
type tokenKind uint8

const (
	tokenLeftBrace tokenKind = iota
	tokenRightBrace
	tokenLeftBracket
	tokenRightBracket
	tokenColon
	tokenComma
	tokenString
	tokenIdentifier
	tokenNumber
	tokenTrue
	tokenFalse
	tokenNull
)

// token is one lexical token with its exact source range.
type token struct {
	kind  tokenKind
	start int
	end   int
}

// lexemeClass classifies one source piece.
type lexemeClass uint8

const (
	classToken lexemeClass = iota
	classTrivia
	classError
)

// lexeme is one source piece with its class.
type lexeme struct {
	start  int
	end    int
	class  lexemeClass
	tok    tokenKind      // classToken
	trivia JsonSyntaxKind // classTrivia
}

// buildDocument lexes and parses one snapshot into a Document. Decoded
// text offsets equal raw byte offsets because the JSON family source is
// UTF-8; all lexing happens over the decoded text.
func buildDocument(ctx context.Context, snapshot *document.SourceSnapshot,
	profile JsonProfile, limits document.ParseLimits) (*Document, *FormationFailure) {
	authority := document.NewDocumentAuthority()
	sink := newDiagnosticSink(limits.MaxDiagnostics)
	text, _ := snapshot.DecodedText()
	var (
		lexemes   []lexeme
		tokens    []token
		recovered bool
		failure   *FormationFailure
	)
	if profile.isJSON5() {
		lexemes, tokens, recovered, failure = lexJSON5(ctx, text, authority, limits, sink)
	} else {
		lexemes, tokens, recovered, failure = lexJSON(ctx, text, profile, authority, limits, sink)
	}
	if failure != nil {
		return nil, failure
	}
	syntaxKinds := make([]JsonSyntaxKind, 0, len(lexemes))
	pieces := make([]document.StructuralPiece, 0, len(lexemes))
	for _, item := range lexemes {
		syntaxKinds = append(syntaxKinds, item.syntaxKind())
		kind := document.PieceToken
		if item.class == classTrivia {
			kind = document.PieceTrivia
		} else if item.class == classError {
			kind = document.PieceErrorRegion
		}
		span, err := authority.Span(item.start, item.end)
		if err != nil {
			return nil, invariantFailure()
		}
		pieces = append(pieces, document.NewStructuralPiece(span, kind))
	}
	// The lexer partitions every source byte exactly once; a violation is
	// an internal invariant failure. The shared index constructor enforces
	// the exact coverage contract.
	index, err := document.NewLosslessStructuralIndex(authority.Identity(), snapshot.Len(), pieces)
	if err != nil {
		return nil, invariantFailure()
	}

	parser := &parserState{
		text:        text,
		profile:     profile,
		authority:   authority,
		tokens:      tokens,
		limits:      limits,
		diagnostics: sink,
		recovered:   recovered,
	}
	root, fatal := parser.parseValue(0)
	if fatal != nil {
		return nil, fatal
	}
	if parser.position < len(parser.tokens) {
		item := parser.tokens[parser.position]
		end := item.end
		if last := parser.tokens[len(parser.tokens)-1]; last.end > end {
			end = last.end
		}
		parser.syntaxDiagnostic("json.syntax.trailing-content@1", item.start, end)
		parser.recovered = true
	}
	formationStatus := document.FormationStatusComplete
	if parser.recovered {
		formationStatus = document.FormationStatusRecovered
	}
	diagnostics := parser.diagnostics.finish()
	sortDiagnostics(diagnostics)
	return &Document{
		authority:       authority,
		source:          snapshot,
		profile:         profile,
		structuralIndex: index,
		syntaxKinds:     syntaxKinds,
		formationStatus: formationStatus,
		diagnostics:     diagnostics,
		entities:        parser.entities,
		root:            root,
		parseLimits:     limits,
	}, nil
}

// invariantFailure reports an internal invariant violation as a source
// failure; it must never be reachable from valid inputs.
func invariantFailure() *FormationFailure {
	return &FormationFailure{Kind: FormationFailureSource,
		Source: &document.SourceError{Kind: document.SourceErrorInvalidSequence}}
}

// syntaxKind maps one lexeme to its lossless syntax kind.
func (l lexeme) syntaxKind() JsonSyntaxKind {
	switch l.class {
	case classToken:
		switch l.tok {
		case tokenLeftBrace:
			return JsonSyntaxKindLeftBrace
		case tokenRightBrace:
			return JsonSyntaxKindRightBrace
		case tokenLeftBracket:
			return JsonSyntaxKindLeftBracket
		case tokenRightBracket:
			return JsonSyntaxKindRightBracket
		case tokenColon:
			return JsonSyntaxKindColon
		case tokenComma:
			return JsonSyntaxKindComma
		case tokenString:
			return JsonSyntaxKindString
		case tokenIdentifier:
			return JsonSyntaxKindIdentifier
		case tokenNumber:
			return JsonSyntaxKindNumber
		case tokenTrue:
			return JsonSyntaxKindTrue
		case tokenFalse:
			return JsonSyntaxKindFalse
		case tokenNull:
			return JsonSyntaxKindNull
		}
	case classTrivia:
		return l.trivia
	case classError:
		return JsonSyntaxKindErrorRegion
	}
	return JsonSyntaxKindErrorRegion
}

// lexJSON lexes one strict or JSONC source over its decoded text
// (parser.rs:174-402).
func lexJSON(ctx context.Context, text string, profile JsonProfile,
	authority document.DocumentAuthority, limits document.ParseLimits,
	sink *diagnosticSink) ([]lexeme, []token, bool, *FormationFailure) {
	lexemes := make([]lexeme, 0, 64)
	tokens := make([]token, 0, 16)
	offset := 0
	recovered := false
	if strings.HasPrefix(text, "\xef\xbb\xbf") {
		lexemes = append(lexemes, lexeme{start: 0, end: 3, class: classTrivia,
			trivia: JsonSyntaxKindBom})
		if profile == JsonProfileStrictV1 {
			sink.push(sourceDiagnostic(authority, "json.strict.leading-bom@1",
				protocol.CategoryConformance, protocol.SeverityWarning, 0, 3))
		}
		offset = 3
	}
	for offset < len(text) {
		if ctx != nil && ctx.Err() != nil {
			return nil, nil, false, &FormationFailure{Kind: FormationFailureCancelled}
		}
		start := offset
		var class lexemeClass
		var kind tokenKind
		var trivia JsonSyntaxKind
		octet := text[offset]
		switch {
		case octet == ' ' || octet == '\t' || octet == '\r' || octet == '\n':
			offset++
		whitespaceScan:
			for offset < len(text) {
				switch text[offset] {
				case ' ', '\t', '\r', '\n':
					offset++
				default:
					break whitespaceScan
				}
			}
			class, trivia = classTrivia, JsonSyntaxKindWhitespace
		case octet == '/' && offset+1 < len(text) && text[offset+1] == '/':
			offset += 2
			for offset < len(text) && text[offset] != '\r' && text[offset] != '\n' {
				offset++
			}
			if !profile.permitsJSONCExtensions() {
				recovered = true
				sink.push(sourceDiagnostic(authority, "json.strict.comment-not-allowed@1",
					protocol.CategoryConformance, protocol.SeverityError, start, offset))
			}
			class, trivia = classTrivia, JsonSyntaxKindLineComment
		case octet == '/' && offset+1 < len(text) && text[offset+1] == '*':
			offset += 2
			closed := false
			for offset+1 < len(text) {
				if text[offset] == '*' && text[offset+1] == '/' {
					offset += 2
					closed = true
					break
				}
				offset++
			}
			if closed {
				if !profile.permitsJSONCExtensions() {
					recovered = true
					sink.push(sourceDiagnostic(authority, "json.strict.comment-not-allowed@1",
						protocol.CategoryConformance, protocol.SeverityError, start, offset))
				}
				class, trivia = classTrivia, JsonSyntaxKindBlockComment
			} else {
				offset = len(text)
				recovered = true
				sink.push(sourceDiagnostic(authority, "json.syntax.unterminated-block-comment@1",
					protocol.CategorySyntax, protocol.SeverityError, start, offset))
				class = classError
			}
		case octet == '{':
			offset++
			class, kind = classToken, tokenLeftBrace
		case octet == '}':
			offset++
			class, kind = classToken, tokenRightBrace
		case octet == '[':
			offset++
			class, kind = classToken, tokenLeftBracket
		case octet == ']':
			offset++
			class, kind = classToken, tokenRightBracket
		case octet == ':':
			offset++
			class, kind = classToken, tokenColon
		case octet == ',':
			offset++
			class, kind = classToken, tokenComma
		case octet == '"':
			offset++
			escaped := false
			closed := false
			for offset < len(text) {
				current := text[offset]
				offset++
				if escaped {
					escaped = false
				} else if current == '\\' {
					escaped = true
				} else if current == '"' {
					closed = true
					break
				}
			}
			if closed {
				class, kind = classToken, tokenString
			} else {
				recovered = true
				sink.push(sourceDiagnostic(authority, "json.syntax.unterminated-string@1",
					protocol.CategorySyntax, protocol.SeverityError, start, offset))
				class = classError
			}
		case octet == '-' || (octet >= '0' && octet <= '9'):
			offset++
		numberScan:
			for offset < len(text) {
				switch text[offset] {
				case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '+', '-', '.', 'e', 'E':
					offset++
				default:
					break numberScan
				}
			}
			if validJSONNumber(text[start:offset]) {
				class, kind = classToken, tokenNumber
			} else {
				recovered = true
				sink.push(sourceDiagnostic(authority, "json.syntax.invalid-number@1",
					protocol.CategorySyntax, protocol.SeverityError, start, offset))
				class = classError
			}
		case isWordStart(octet):
			offset++
		wordScan:
			for offset < len(text) {
				if isWordContinue(text[offset]) {
					offset++
				} else {
					break wordScan
				}
			}
			switch text[start:offset] {
			case "true":
				class, kind = classToken, tokenTrue
			case "false":
				class, kind = classToken, tokenFalse
			case "null":
				class, kind = classToken, tokenNull
			default:
				recovered = true
				sink.push(sourceDiagnostic(authority, "json.syntax.unexpected-word@1",
					protocol.CategorySyntax, protocol.SeverityError, start, offset))
				class = classError
			}
		default:
			width := utf8Width(octet)
			offset += width
			if offset > len(text) {
				offset = len(text)
			}
			recovered = true
			sink.push(sourceDiagnostic(authority, "json.syntax.unexpected-character@1",
				protocol.CategorySyntax, protocol.SeverityError, start, offset))
			class = classError
		}
		lexemes = append(lexemes, lexeme{start: start, end: offset, class: class,
			tok: kind, trivia: trivia})
		if class == classToken {
			tokens = append(tokens, token{kind: kind, start: start, end: offset})
		}
		if len(lexemes) > limits.MaxTokenCount {
			return nil, nil, false, &FormationFailure{Kind: FormationFailureResourceLimit,
				Name: "token-count", Observed: len(lexemes), Limit: limits.MaxTokenCount}
		}
	}
	return lexemes, tokens, recovered, nil
}

// isWordStart reports one strict word-leading octet.
func isWordStart(octet byte) bool {
	return (octet >= 'a' && octet <= 'z') || (octet >= 'A' && octet <= 'Z') || octet == '_'
}

// isWordContinue reports one strict word-continuing octet.
func isWordContinue(octet byte) bool {
	return isWordStart(octet) || (octet >= '0' && octet <= '9')
}

// utf8Width returns the raw byte width of one UTF-8 scalar from its
// leading octet (parser.rs:767-774).
func utf8Width(leading byte) int {
	switch {
	case leading <= 0x7f:
		return 1
	case leading >= 0xc0 && leading <= 0xdf:
		return 2
	case leading >= 0xe0 && leading <= 0xef:
		return 3
	}
	return 4
}

// validJSONNumber validates one strict/JSONC number lexeme
// (parser.rs:776-815).
func validJSONNumber(text string) bool {
	index := 0
	if index < len(text) && text[index] == '-' {
		index++
	}
	switch {
	case index < len(text) && text[index] == '0':
		index++
	case index < len(text) && text[index] >= '1' && text[index] <= '9':
		index++
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
	default:
		return false
	}
	if index < len(text) && text[index] == '.' {
		index++
		fractionStart := index
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
		if index == fractionStart {
			return false
		}
	}
	if index < len(text) && (text[index] == 'e' || text[index] == 'E') {
		index++
		if index < len(text) && (text[index] == '+' || text[index] == '-') {
			index++
		}
		exponentStart := index
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
		if index == exponentStart {
			return false
		}
	}
	return index == len(text)
}

// lexJSON5 lexes one Standard JSON5 source over its decoded text
// (parser.rs:404-581).
func lexJSON5(ctx context.Context, source string, authority document.DocumentAuthority,
	limits document.ParseLimits, sink *diagnosticSink) ([]lexeme, []token, bool, *FormationFailure) {
	lexemes := make([]lexeme, 0, 64)
	tokens := make([]token, 0, 16)
	offset := 0
	recovered := false
	if strings.HasPrefix(source, "\ufeff") {
		lexemes = append(lexemes, lexeme{start: 0, end: len("\ufeff"),
			class: classTrivia, trivia: JsonSyntaxKindBom})
		offset = len("\ufeff")
	}
	for offset < len(source) {
		if ctx != nil && ctx.Err() != nil {
			return nil, nil, false, &FormationFailure{Kind: FormationFailureCancelled}
		}
		start := offset
		character := charAt(source, offset)
		var class lexemeClass
		var kind tokenKind
		var trivia JsonSyntaxKind
		switch {
		case isJSON5Whitespace(character):
			offset += utf8.RuneLen(character)
			for offset < len(source) && isJSON5Whitespace(charAt(source, offset)) {
				offset += utf8.RuneLen(charAt(source, offset))
			}
			class, trivia = classTrivia, JsonSyntaxKindWhitespace
		case strings.HasPrefix(source[start:], "//"):
			offset += 2
			for offset < len(source) && !isJSON5LineTerminator(charAt(source, offset)) {
				offset += utf8.RuneLen(charAt(source, offset))
			}
			class, trivia = classTrivia, JsonSyntaxKindLineComment
		case strings.HasPrefix(source[start:], "/*"):
			offset += 2
			closed := false
			for offset < len(source) {
				if strings.HasPrefix(source[offset:], "*/") {
					offset += 2
					closed = true
					break
				}
				offset += utf8.RuneLen(charAt(source, offset))
			}
			if closed {
				class, trivia = classTrivia, JsonSyntaxKindBlockComment
			} else {
				recovered = true
				sink.push(sourceDiagnostic(authority, "json.syntax.unterminated-block-comment@1",
					protocol.CategorySyntax, protocol.SeverityError, start, offset))
				class = classError
			}
		default:
			switch {
			case character == '{':
				offset++
				class, kind = classToken, tokenLeftBrace
			case character == '}':
				offset++
				class, kind = classToken, tokenRightBrace
			case character == '[':
				offset++
				class, kind = classToken, tokenLeftBracket
			case character == ']':
				offset++
				class, kind = classToken, tokenRightBracket
			case character == ':':
				offset++
				class, kind = classToken, tokenColon
			case character == ',':
				offset++
				class, kind = classToken, tokenComma
			case character == '\'' || character == '"':
				quote := character
				offset += utf8.RuneLen(quote)
				closed := false
				for offset < len(source) {
					current := charAt(source, offset)
					offset += utf8.RuneLen(current)
					if current == '\\' {
						if offset < len(source) {
							escaped := charAt(source, offset)
							offset += utf8.RuneLen(escaped)
							if escaped == '\r' && strings.HasPrefix(source[offset:], "\n") {
								offset++
							}
						}
					} else if current == quote {
						closed = true
						break
					}
				}
				if closed {
					class, kind = classToken, tokenString
				} else {
					recovered = true
					sink.push(sourceDiagnostic(authority, "json.syntax.unterminated-string@1",
						protocol.CategorySyntax, protocol.SeverityError, start, offset))
					class = classError
				}
			case (character == '+' || character == '-' || character == '.' ||
				(character >= '0' && character <= '9')) &&
				(character != '.' || isDigitAt(source, offset+1)):
				offset = scanJSON5NumberCandidate(source, offset)
				if validJSON5Number(source[start:offset]) {
					class, kind = classToken, tokenNumber
				} else {
					recovered = true
					sink.push(sourceDiagnostic(authority, "json.syntax.invalid-number@1",
						protocol.CategorySyntax, protocol.SeverityError, start, offset))
					class = classError
				}
			case character == '\\' || isJSON5IdentifierStart(character):
				end, valid := scanJSON5Identifier(source, offset)
				offset = end
				if valid {
					class, kind = classToken, tokenIdentifier
				} else {
					recovered = true
					sink.push(sourceDiagnostic(authority, "json5.syntax.invalid-identifier@1",
						protocol.CategorySyntax, protocol.SeverityError, start, offset))
					class = classError
				}
			default:
				offset += utf8.RuneLen(character)
				recovered = true
				sink.push(sourceDiagnostic(authority, "json.syntax.unexpected-character@1",
					protocol.CategorySyntax, protocol.SeverityError, start, offset))
				class = classError
			}
		}
		lexemes = append(lexemes, lexeme{start: start, end: offset, class: class,
			tok: kind, trivia: trivia})
		if class == classToken {
			tokens = append(tokens, token{kind: kind, start: start, end: offset})
		}
		if len(lexemes) > limits.MaxTokenCount {
			return nil, nil, false, &FormationFailure{Kind: FormationFailureResourceLimit,
				Name: "token-count", Observed: len(lexemes), Limit: limits.MaxTokenCount}
		}
	}
	return lexemes, tokens, recovered, nil
}

// charAt returns the scalar at one decoded-text byte offset.
func charAt(source string, offset int) rune {
	character, _ := utf8.DecodeRuneInString(source[offset:])
	return character
}

// isDigitAt reports whether a scalar starts an ASCII digit at the offset.
func isDigitAt(source string, offset int) bool {
	if offset >= len(source) {
		return false
	}
	character := charAt(source, offset)
	return character >= '0' && character <= '9'
}

// isJSON5LineTerminator reports one JSON5 line terminator
// (parser.rs:590-593).
func isJSON5LineTerminator(character rune) bool {
	return character == '\n' || character == '\r' || character == '\u2028' || character == '\u2029'
}

// isJSON5Whitespace reports one JSON5 whitespace scalar
// (parser.rs:594-614).
func isJSON5Whitespace(character rune) bool {
	switch character {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680',
		'\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return character >= '\u2000' && character <= '\u200a'
}

// isJSON5IdentifierStart reports one ID_Start scalar including the JSON5
// dollar/underscore additions (parser.rs:616-623).
func isJSON5IdentifierStart(character rune) bool {
	if character == '$' || character == '_' {
		return true
	}
	return unicode.IsLetter(character) || unicode.Is(unicode.Nl, character)
}

// isJSON5IdentifierContinue reports one ID_Continue scalar including the
// JSON5 additions (parser.rs:619-623).
func isJSON5IdentifierContinue(character rune) bool {
	switch character {
	case '$', '_', '\u200c', '\u200d':
		return true
	}
	return unicode.IsLetter(character) || unicode.Is(unicode.Nl, character) ||
		unicode.Is(unicode.Mn, character) || unicode.Is(unicode.Mc, character) ||
		unicode.Is(unicode.Nd, character) || unicode.Is(unicode.Pc, character)
}

// scanJSON5Identifier scans one identifier candidate with escape decoding
// (parser.rs:625-658).
func scanJSON5Identifier(source string, start int) (int, bool) {
	offset := start
	first := true
	valid := true
	for offset < len(source) {
		character := charAt(source, offset)
		var decoded rune
		width := utf8.RuneLen(character)
		if character == '\\' {
			var ok bool
			decoded, ok = decodeIdentifierEscape(source[offset:])
			if !ok {
				valid = false
				offset = scanJSON5InvalidWord(source, offset)
				break
			}
			width = 6
		} else {
			decoded = character
		}
		permitted := isJSON5IdentifierContinue(decoded)
		if first {
			permitted = isJSON5IdentifierStart(decoded)
		}
		if !permitted {
			if first || character == '\\' {
				valid = false
				offset = scanJSON5InvalidWord(source, offset)
			}
			break
		}
		offset += width
		first = false
	}
	return offset, valid && !first
}

// scanJSON5InvalidWord consumes one recovered invalid word
// (parser.rs:660-675).
func scanJSON5InvalidWord(source string, start int) int {
	offset := start
	for offset < len(source) {
		character := charAt(source, offset)
		if isJSON5Whitespace(character) {
			break
		}
		switch character {
		case '{', '}', '[', ']', ':', ',', '/', '\'', '"':
			return offset
		}
		offset += utf8.RuneLen(character)
	}
	if offset <= start {
		offset = start + 1
	}
	return offset
}

// decodeIdentifierEscape decodes one `\uXXXX` identifier escape
// (parser.rs:677-687).
func decodeIdentifierEscape(source string) (rune, bool) {
	if !strings.HasPrefix(source, "\\u") || len(source) < 6 {
		return 0, false
	}
	var value uint32
	for index := 2; index < 6; index++ {
		digit := hexDigit(source[index])
		if digit < 0 {
			return 0, false
		}
		value = value*16 + uint32(digit)
	}
	return rune(value), true
}

// hexDigit decodes one hexadecimal digit.
func hexDigit(octet byte) int {
	switch {
	case octet >= '0' && octet <= '9':
		return int(octet - '0')
	case octet >= 'a' && octet <= 'f':
		return int(octet-'a') + 10
	case octet >= 'A' && octet <= 'F':
		return int(octet-'A') + 10
	}
	return -1
}

// scanJSON5NumberCandidate consumes one JSON5 number candidate
// (parser.rs:689-699).
func scanJSON5NumberCandidate(source string, start int) int {
	offset := start
	for offset < len(source) {
		character := charAt(source, offset)
		if !(character == '+' || character == '-' || character == '.' ||
			character == '_' || (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')) {
			break
		}
		offset += utf8.RuneLen(character)
	}
	return offset
}

// validJSON5Number validates one JSON5 number lexeme
// (parser.rs:701-760).
func validJSON5Number(text string) bool {
	unsigned := text
	if len(unsigned) > 0 && (unsigned[0] == '+' || unsigned[0] == '-') {
		unsigned = unsigned[1:]
	}
	if unsigned == "Infinity" || unsigned == "NaN" {
		return true
	}
	if hex, ok := strings.CutPrefix(unsigned, "0x"); ok {
		return hex != "" && allHexDigits(hex)
	}
	if hex, ok := strings.CutPrefix(unsigned, "0X"); ok {
		return hex != "" && allHexDigits(hex)
	}
	index := 0
	if index < len(unsigned) && unsigned[index] == '.' {
		index++
		start := index
		for index < len(unsigned) && unsigned[index] >= '0' && unsigned[index] <= '9' {
			index++
		}
		if index == start {
			return false
		}
	} else {
		switch {
		case index < len(unsigned) && unsigned[index] == '0':
			index++
			if index < len(unsigned) && unsigned[index] >= '0' && unsigned[index] <= '9' {
				return false
			}
		case index < len(unsigned) && unsigned[index] >= '1' && unsigned[index] <= '9':
			index++
			for index < len(unsigned) && unsigned[index] >= '0' && unsigned[index] <= '9' {
				index++
			}
		default:
			return false
		}
		if index < len(unsigned) && unsigned[index] == '.' {
			index++
			for index < len(unsigned) && unsigned[index] >= '0' && unsigned[index] <= '9' {
				index++
			}
		}
	}
	if index < len(unsigned) && (unsigned[index] == 'e' || unsigned[index] == 'E') {
		index++
		if index < len(unsigned) && (unsigned[index] == '+' || unsigned[index] == '-') {
			index++
		}
		exponentStart := index
		for index < len(unsigned) && unsigned[index] >= '0' && unsigned[index] <= '9' {
			index++
		}
		if index == exponentStart {
			return false
		}
	}
	return index == len(unsigned)
}

// allHexDigits reports whether every octet is a hexadecimal digit.
func allHexDigits(text string) bool {
	for index := 0; index < len(text); index++ {
		if hexDigit(text[index]) < 0 {
			return false
		}
	}
	return true
}

// parserState is the token-level parser over one lexed source.
type parserState struct {
	text        string
	profile     JsonProfile
	authority   document.DocumentAuthority
	tokens      []token
	position    int
	entities    []entity
	diagnostics *diagnosticSink
	recovered   bool
	limits      document.ParseLimits
}

// parseValue parses one value at the current token position
// (parser.rs:830-951).
func (p *parserState) parseValue(depth int) (int, *FormationFailure) {
	if depth > p.limits.MaxNestingDepth {
		return 0, &FormationFailure{Kind: FormationFailureResourceLimit,
			Name: "nesting-depth", Observed: depth, Limit: p.limits.MaxNestingDepth}
	}
	item, ok := p.peek()
	if !ok {
		offset := len(p.text)
		p.syntaxDiagnostic("json.syntax.missing-value@1", offset, offset)
		p.recovered = true
		return p.allocValue(offset, offset, nil, false,
			internalValueKind{tag: internalUnavailable, unavail: SemanticUnavailableMissing})
	}
	switch item.kind {
	case tokenNull:
		p.position++
		return p.allocScalar(item, internalValueKind{tag: internalNull})
	case tokenTrue:
		p.position++
		return p.allocScalar(item, internalValueKind{tag: internalBoolean, boolean: true})
	case tokenFalse:
		p.position++
		return p.allocScalar(item, internalValueKind{tag: internalBoolean, boolean: false})
	case tokenNumber:
		p.position++
		return p.allocScalar(item, parseNumberKind(p.text[item.start:item.end], p.profile))
	case tokenString:
		p.position++
		decoded, ok := decodeJSONString(p.text[item.start:item.end], p.profile)
		if ok {
			if decoded.hasUnescapedLineSeparator {
				p.diagnostics.push(sourceDiagnostic(p.authority, "json5.string.unescaped-line-separator@1",
					protocol.CategoryConformance, protocol.SeverityWarning, item.start, item.end))
			}
			return p.allocScalar(item, internalValueKind{tag: internalString, stringText: decoded.value})
		}
		p.syntaxDiagnostic("json.syntax.invalid-string-escape@1", item.start, item.end)
		p.recovered = true
		return p.allocValue(item.start, item.end, &[2]int{item.start, item.end}, true,
			internalValueKind{tag: internalUnavailable, unavail: SemanticUnavailableInvalidLiteral})
	case tokenIdentifier:
		if !p.profile.isJSON5() {
			break
		}
		p.position++
		text, ok := decodeJSON5Identifier(p.text[item.start:item.end])
		if !ok {
			break
		}
		switch text {
		case "null":
			return p.allocScalar(item, internalValueKind{tag: internalNull})
		case "true":
			return p.allocScalar(item, internalValueKind{tag: internalBoolean, boolean: true})
		case "false":
			return p.allocScalar(item, internalValueKind{tag: internalBoolean, boolean: false})
		case "Infinity":
			return p.allocScalar(item, internalValueKind{tag: internalBinaryFloat64,
				binary64: core.NewBinaryFloat64(0x7ff0_0000_0000_0000)})
		case "NaN":
			return p.allocScalar(item, internalValueKind{tag: internalBinaryFloat64,
				binary64: core.NewBinaryFloat64(0x7ff8_0000_0000_0000)})
		}
		p.syntaxDiagnostic("json.syntax.expected-value@1", item.start, item.end)
		p.recovered = true
		return p.allocScalar(item, internalValueKind{tag: internalUnavailable,
			unavail: SemanticUnavailableErrorRegion})
	case tokenLeftBrace:
		return p.parseObject(depth)
	case tokenLeftBracket:
		return p.parseArray(depth)
	}
	p.position++
	p.syntaxDiagnostic("json.syntax.expected-value@1", item.start, item.end)
	p.recovered = true
	return p.allocValue(item.start, item.end, nil, false,
		internalValueKind{tag: internalUnavailable, unavail: SemanticUnavailableErrorRegion})
}

// parseObject parses one object at the current token position
// (parser.rs:953-1069).
func (p *parserState) parseObject(depth int) (int, *FormationFailure) {
	open, _ := p.consume(tokenLeftBrace)
	members := make([]int, 0, 4)
	names := make(map[string]int)
	for {
		if close, ok := p.consume(tokenRightBrace); ok {
			return p.allocValue(open.start, close.end, nil, true,
				internalValueKind{tag: internalObject, object: members})
		}
		if _, ok := p.peek(); !ok {
			break
		}
		ordinal := len(members)
		var key int
		if candidate, ok := p.peek(); ok &&
			(candidate.kind == tokenString || (p.profile.isJSON5() && candidate.kind == tokenIdentifier)) {
			var fatal *FormationFailure
			key, fatal = p.parseObjectKey(depth + 1)
			if fatal != nil {
				return 0, fatal
			}
		} else {
			offset := p.currentOffset()
			p.syntaxDiagnostic("json.syntax.expected-object-key@1", offset, offset)
			p.recovered = true
			var fatal *FormationFailure
			key, fatal = p.allocValue(offset, offset, nil, false,
				internalValueKind{tag: internalUnavailable, unavail: SemanticUnavailableMissing})
			if fatal != nil {
				return 0, fatal
			}
		}
		if _, ok := p.consume(tokenColon); !ok {
			offset := p.currentOffset()
			p.syntaxDiagnostic("json.syntax.missing-colon@1", offset, offset)
			p.recovered = true
		}
		value, fatal := p.parseValue(depth + 1)
		if fatal != nil {
			return 0, fatal
		}
		memberStart := p.spanOf(key).StartByte()
		memberEnd := p.spanOf(value).EndByte()
		member, fatal := p.allocMember(memberStart, memberEnd, key, value, ordinal)
		if fatal != nil {
			return 0, fatal
		}
		members = append(members, member)
		keyKind := &p.entities[key].value.kind
		if keyKind.tag == internalString {
			name := keyKind.stringText
			if first, exists := names[name]; exists {
				diagnostic := sourceDiagnostic(p.authority, "json.object.duplicate-member@1",
					protocol.CategorySemantic, protocol.SeverityError,
					p.spanOf(member).StartByte(), p.spanOf(member).EndByte())
				diagnostic.Arguments = map[string]string{"name": name}
				firstLocation := diagnosticLocation(p.authority, p.spanOf(first))
				if firstLocation != nil {
					diagnostic.Related = []protocol.RelatedSourceLocation{{
						Role:     "first-member",
						Location: *firstLocation,
					}}
				}
				p.diagnostics.push(diagnostic)
			} else {
				names[name] = member
			}
		}
		if _, ok := p.consume(tokenComma); ok {
			if candidate, ok := p.peek(); ok && candidate.kind == tokenRightBrace &&
				!p.profile.permitsJSONCExtensions() {
				p.diagnostics.push(sourceDiagnostic(p.authority, "json.strict.trailing-comma@1",
					protocol.CategoryConformance, protocol.SeverityError,
					candidate.start-1, candidate.start))
				p.recovered = true
			}
			continue
		}
		if candidate, ok := p.peek(); ok && candidate.kind == tokenRightBrace {
			continue
		}
		offset := p.currentOffset()
		p.syntaxDiagnostic("json.syntax.missing-comma@1", offset, offset)
		p.recovered = true
		if candidate, ok := p.peek(); ok {
			switch candidate.kind {
			case tokenString, tokenIdentifier, tokenRightBrace:
			default:
				p.position++
			}
		}
	}
	end := len(p.text)
	p.syntaxDiagnostic("json.syntax.missing-object-close@1", end, end)
	p.recovered = true
	return p.allocValue(open.start, end, nil, false,
		internalValueKind{tag: internalObject, object: members})
}

// parseArray parses one array at the current token position
// (parser.rs:1071-1133).
func (p *parserState) parseArray(depth int) (int, *FormationFailure) {
	open, _ := p.consume(tokenLeftBracket)
	elements := make([]int, 0, 4)
	for {
		if close, ok := p.consume(tokenRightBracket); ok {
			return p.allocValue(open.start, close.end, nil, true,
				internalValueKind{tag: internalArray, array: elements})
		}
		if _, ok := p.peek(); !ok {
			end := len(p.text)
			p.syntaxDiagnostic("json.syntax.missing-array-close@1", end, end)
			p.recovered = true
			return p.allocValue(open.start, end, nil, false,
				internalValueKind{tag: internalArray, array: elements})
		}
		ordinal := len(elements)
		value, fatal := p.parseValue(depth + 1)
		if fatal != nil {
			return 0, fatal
		}
		span := p.spanOf(value)
		element, fatal := p.allocElement(span.StartByte(), span.EndByte(), value, ordinal)
		if fatal != nil {
			return 0, fatal
		}
		elements = append(elements, element)
		if _, ok := p.consume(tokenComma); ok {
			if candidate, ok := p.peek(); ok && candidate.kind == tokenRightBracket &&
				!p.profile.permitsJSONCExtensions() {
				p.syntaxDiagnostic("json.strict.trailing-comma@1",
					candidate.start-1, candidate.start)
				p.recovered = true
			}
			continue
		}
		if candidate, ok := p.peek(); ok && candidate.kind == tokenRightBracket {
			continue
		}
		offset := p.currentOffset()
		p.syntaxDiagnostic("json.syntax.missing-comma@1", offset, offset)
		p.recovered = true
	}
}

// parseObjectKey parses one object key token (parser.rs:1135-1145).
func (p *parserState) parseObjectKey(depth int) (int, *FormationFailure) {
	item, _ := p.peek()
	if item.kind == tokenString {
		return p.parseValue(depth)
	}
	p.position++
	text, ok := decodeJSON5Identifier(p.text[item.start:item.end])
	if !ok {
		text = p.text[item.start:item.end]
	}
	return p.allocScalar(item, internalValueKind{tag: internalString, stringText: text})
}

// allocScalar allocates one complete literal value entity
// (parser.rs:1147-1159).
func (p *parserState) allocScalar(item token, kind internalValueKind) (int, *FormationFailure) {
	return p.allocValue(item.start, item.end, &[2]int{item.start, item.end}, true, kind)
}

// allocValue allocates one value entity (parser.rs:1161-1176).
func (p *parserState) allocValue(start, end int, literal *[2]int, complete bool,
	kind internalValueKind) (int, *FormationFailure) {
	var literalSpan *document.Span
	if literal != nil {
		span, err := p.authority.Span(literal[0], literal[1])
		if err != nil {
			return 0, invariantFailure()
		}
		literalSpan = &span
	}
	span, err := p.authority.Span(start, end)
	if err != nil {
		return 0, invariantFailure()
	}
	return p.allocEntity(&entity{value: &valueEntity{
		span: span, literalSpan: literalSpan, complete: complete, kind: kind}})
}

// allocMember allocates one member association.
func (p *parserState) allocMember(start, end, key, value, ordinal int) (int, *FormationFailure) {
	span, err := p.authority.Span(start, end)
	if err != nil {
		return 0, invariantFailure()
	}
	return p.allocEntity(&entity{member: &memberEntity{
		span: span, key: key, value: value, ordinal: ordinal}})
}

// allocElement allocates one array element association.
func (p *parserState) allocElement(start, end, value, ordinal int) (int, *FormationFailure) {
	span, err := p.authority.Span(start, end)
	if err != nil {
		return 0, invariantFailure()
	}
	return p.allocEntity(&entity{element: &elementEntity{
		span: span, value: value, ordinal: ordinal}})
}

// allocEntity allocates one structural entity (parser.rs:1178-1189).
func (p *parserState) allocEntity(item *entity) (int, *FormationFailure) {
	if len(p.entities) >= p.limits.MaxNodeCount {
		return 0, &FormationFailure{Kind: FormationFailureResourceLimit,
			Name: "node-count", Observed: len(p.entities) + 1, Limit: p.limits.MaxNodeCount}
	}
	index := len(p.entities)
	p.entities = append(p.entities, *item)
	return index, nil
}

// spanOf returns the entity span of one entity index.
func (p *parserState) spanOf(index int) document.Span {
	item := &p.entities[index]
	switch {
	case item.value != nil:
		return item.value.span
	case item.member != nil:
		return item.member.span
	default:
		return item.element.span
	}
}

// peek returns the current token without consuming it.
func (p *parserState) peek() (token, bool) {
	if p.position >= len(p.tokens) {
		return token{}, false
	}
	return p.tokens[p.position], true
}

// consume consumes one token of the given kind.
func (p *parserState) consume(kind tokenKind) (token, bool) {
	item, ok := p.peek()
	if !ok || item.kind != kind {
		return token{}, false
	}
	p.position++
	return item, true
}

// currentOffset returns the byte offset of the next token or the source
// end (parser.rs:1212-1214).
func (p *parserState) currentOffset() int {
	if item, ok := p.peek(); ok {
		return item.start
	}
	return len(p.text)
}

// syntaxDiagnostic pushes one Syntax/Error diagnostic at a source range.
func (p *parserState) syntaxDiagnostic(code string, start, end int) {
	p.diagnostics.push(sourceDiagnostic(p.authority, code,
		protocol.CategorySyntax, protocol.SeverityError, start, end))
}

// parseNumberKind resolves one validated number lexeme into its native
// category (parser.rs:863-877, 1375-1443).
func parseNumberKind(text string, profile JsonProfile) internalValueKind {
	if profile.isJSON5() {
		return parseJSON5Number(text)
	}
	if strings.ContainsAny(text, ".eE") {
		return internalValueKind{tag: internalDecimal, decimal: parseJSONNumberDecimal(text)}
	}
	integer, _ := new(big.Int).SetString(text, 10)
	return internalValueKind{tag: internalInteger, integer: integer}
}

// parseJSONNumberDecimal converts one validated strict JSON number into
// its canonical coefficient × 10^exponent decimal.
func parseJSONNumberDecimal(text string) core.Decimal {
	unsigned := text
	if len(unsigned) > 0 && (unsigned[0] == '+' || unsigned[0] == '-') {
		unsigned = unsigned[1:]
	}
	coefficientText := unsigned
	explicitExponent := new(big.Int)
	if index := strings.IndexAny(unsigned, "eE"); index >= 0 {
		explicitExponent, _ = new(big.Int).SetString(unsigned[index+1:], 10)
		coefficientText = unsigned[:index]
	}
	fractionDigits := 0
	if index := strings.IndexByte(coefficientText, '.'); index >= 0 {
		fractionDigits = len(coefficientText) - index - 1
		coefficientText = coefficientText[:index] + coefficientText[index+1:]
	}
	sign := big.NewInt(1)
	if strings.HasPrefix(text, "-") {
		sign = big.NewInt(-1)
	}
	coefficient, _ := new(big.Int).SetString(coefficientText, 10)
	coefficient.Mul(coefficient, sign)
	exponent := new(big.Int).Sub(explicitExponent, big.NewInt(int64(fractionDigits)))
	return core.NewDecimal(coefficient, exponent)
}

// parseJSON5Number resolves one validated JSON5 number lexeme
// (parser.rs:1375-1443).
func parseJSON5Number(text string) internalValueKind {
	negative := false
	unsigned := text
	if rest, ok := strings.CutPrefix(text, "-"); ok {
		negative = true
		unsigned = rest
	} else if rest, ok := strings.CutPrefix(text, "+"); ok {
		unsigned = rest
	}
	switch unsigned {
	case "Infinity":
		bits := uint64(0x7ff0_0000_0000_0000)
		if negative {
			bits = 0xfff0_0000_0000_0000
		}
		return internalValueKind{tag: internalBinaryFloat64, binary64: core.NewBinaryFloat64(bits)}
	case "NaN":
		bits := uint64(0x7ff8_0000_0000_0000)
		if negative {
			bits = 0xfff8_0000_0000_0000
		}
		return internalValueKind{tag: internalBinaryFloat64, binary64: core.NewBinaryFloat64(bits)}
	}
	if hex, ok := strings.CutPrefix(unsigned, "0x"); ok {
		return json5HexInteger(hex, negative)
	}
	if hex, ok := strings.CutPrefix(unsigned, "0X"); ok {
		return json5HexInteger(hex, negative)
	}
	normalized := unsigned
	if negative {
		normalized = "-" + unsigned
	}
	signWidth := 0
	if negative {
		signWidth = 1
	}
	if strings.HasPrefix(normalized[signWidth:], ".") {
		normalized = normalized[:signWidth] + "0" + normalized[signWidth:]
	}
	exponentIndex := strings.IndexAny(normalized, "eE")
	if exponentIndex < 0 {
		exponentIndex = len(normalized)
	}
	if strings.HasSuffix(normalized[:exponentIndex], ".") {
		normalized = normalized[:exponentIndex] + "0" + normalized[exponentIndex:]
	}
	if strings.ContainsAny(normalized, ".eE") {
		return internalValueKind{tag: internalDecimal, decimal: parseJSONNumberDecimal(normalized)}
	}
	integer, _ := new(big.Int).SetString(normalized, 10)
	return internalValueKind{tag: internalInteger, integer: integer}
}

// json5HexInteger converts one validated JSON5 hex magnitude into an
// Integer.
func json5HexInteger(hex string, negative bool) internalValueKind {
	integer := new(big.Int)
	integer.SetString(hex, 16)
	if negative {
		integer.Neg(integer)
	}
	return internalValueKind{tag: internalInteger, integer: integer}
}

// decodedString is one decoded JSON string literal plus the raw line
// separator fact.
type decodedString struct {
	value                     string
	hasUnescapedLineSeparator bool
}

// decodeJSONString decodes one string literal under the profile
// (parser.rs:1232-1315).
func decodeJSONString(literal string, profile JsonProfile) (decodedString, bool) {
	quote := charAt(literal, 0)
	if quote != '"' && !(profile.isJSON5() && quote == '\'') {
		return decodedString{}, false
	}
	inner, ok := strings.CutPrefix(literal, string(quote))
	if !ok {
		return decodedString{}, false
	}
	inner, ok = strings.CutSuffix(inner, string(quote))
	if !ok {
		return decodedString{}, false
	}
	var output strings.Builder
	hasUnescapedLineSeparator := false
	chars := []rune(inner)
	position := 0
	for position < len(chars) {
		character := chars[position]
		position++
		if character != '\\' {
			if character <= '\u001f' {
				return decodedString{}, false
			}
			if character == '\u2028' || character == '\u2029' {
				hasUnescapedLineSeparator = true
			}
			output.WriteRune(character)
			continue
		}
		if position >= len(chars) {
			return decodedString{}, false
		}
		escaped := chars[position]
		position++
		switch escaped {
		case '"':
			output.WriteByte('"')
		case '\'':
			if !profile.isJSON5() {
				return decodedString{}, false
			}
			output.WriteByte('\'')
		case '\\':
			output.WriteByte('\\')
		case '/':
			output.WriteByte('/')
		case 'b':
			output.WriteByte('\b')
		case 'f':
			output.WriteByte('\f')
		case 'n':
			output.WriteByte('\n')
		case 'r':
			output.WriteByte('\r')
		case 't':
			output.WriteByte('\t')
		case 'v':
			if !profile.isJSON5() {
				return decodedString{}, false
			}
			output.WriteByte('\v')
		case '0':
			if !profile.isJSON5() {
				return decodedString{}, false
			}
			if position < len(chars) && chars[position] >= '0' && chars[position] <= '9' {
				return decodedString{}, false
			}
			output.WriteByte(0)
		case 'x':
			if !profile.isJSON5() {
				return decodedString{}, false
			}
			value, ok := readHexPair(chars, &position)
			if !ok {
				return decodedString{}, false
			}
			output.WriteByte(value)
		case 'u':
			first, ok := readHexQuad(chars, &position)
			if !ok {
				return decodedString{}, false
			}
			scalar := rune(first)
			if first >= 0xd800 && first <= 0xdbff {
				if position+1 >= len(chars) || chars[position] != '\\' || chars[position+1] != 'u' {
					return decodedString{}, false
				}
				position += 2
				second, ok := readHexQuad(chars, &position)
				if !ok || second < 0xdc00 || second > 0xdfff {
					return decodedString{}, false
				}
				scalar = rune(0x1_0000) + rune(first-0xd800)<<10 + rune(second-0xdc00)
			} else if first >= 0xdc00 && first <= 0xdfff {
				return decodedString{}, false
			}
			output.WriteRune(scalar)
		case '\n', '\u2028', '\u2029':
			if !profile.isJSON5() {
				return decodedString{}, false
			}
		case '\r':
			if !profile.isJSON5() {
				return decodedString{}, false
			}
			if position < len(chars) && chars[position] == '\n' {
				position++
			}
		default:
			if !profile.isJSON5() || (escaped >= '0' && escaped <= '9') ||
				isJSON5LineTerminator(escaped) {
				return decodedString{}, false
			}
			output.WriteRune(escaped)
		}
	}
	return decodedString{value: output.String(), hasUnescapedLineSeparator: hasUnescapedLineSeparator}, true
}

// readHexPair consumes two hexadecimal scalars.
func readHexPair(chars []rune, position *int) (byte, bool) {
	var value byte
	for count := 0; count < 2; count++ {
		if *position >= len(chars) {
			return 0, false
		}
		digit := hexDigitFromRune(chars[*position])
		if digit < 0 {
			return 0, false
		}
		value = value*16 + byte(digit)
		*position++
	}
	return value, true
}

// readHexQuad consumes four hexadecimal scalars.
func readHexQuad(chars []rune, position *int) (uint16, bool) {
	var value uint16
	for count := 0; count < 4; count++ {
		if *position >= len(chars) {
			return 0, false
		}
		digit := hexDigitFromRune(chars[*position])
		if digit < 0 {
			return 0, false
		}
		value = value*16 + uint16(digit)
		*position++
	}
	return value, true
}

// hexDigitFromRune decodes one hexadecimal rune.
func hexDigitFromRune(character rune) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'a' && character <= 'f':
		return int(character-'a') + 10
	case character >= 'A' && character <= 'F':
		return int(character-'A') + 10
	}
	return -1
}

// decodeJSON5Identifier decodes one JSON5 IdentifierName literal
// (parser.rs:1349-1373).
func decodeJSON5Identifier(literal string) (string, bool) {
	var output strings.Builder
	offset := 0
	first := true
	for offset < len(literal) {
		character := charAt(literal, offset)
		var decoded rune
		width := utf8.RuneLen(character)
		if character == '\\' {
			var ok bool
			decoded, ok = decodeIdentifierEscape(literal[offset:])
			if !ok {
				return "", false
			}
			width = 6
		} else {
			decoded = character
		}
		permitted := isJSON5IdentifierContinue(decoded)
		if first {
			permitted = isJSON5IdentifierStart(decoded)
		}
		if !permitted {
			return "", false
		}
		output.WriteRune(decoded)
		offset += width
		first = false
	}
	if first {
		return "", false
	}
	return output.String(), true
}

// diagnosticSink applies the diagnostic budget with the explicit
// truncation marker (parser.rs:1500-1537).
type diagnosticSink struct {
	diagnostics []*protocol.Diagnostic
	max         int
	occurrence  uint64
	truncated   bool
}

func newDiagnosticSink(max int) *diagnosticSink {
	return &diagnosticSink{max: max}
}

// push appends one diagnostic, assigning the deterministic occurrence
// ordinal and enforcing the budget.
func (s *diagnosticSink) push(diagnostic *protocol.Diagnostic) {
	diagnostic.Occurrence = s.occurrence
	s.occurrence++
	if len(s.diagnostics) < s.max {
		s.diagnostics = append(s.diagnostics, diagnostic)
		return
	}
	if !s.truncated {
		s.truncated = true
		truncated, err := protocol.NewDiagnostic("core.diagnostic.truncated@1",
			protocol.CategoryResource, protocol.SeverityWarning, nil, nil, nil, nil, nil,
			s.occurrence, errorRegistry())
		if err == nil {
			s.diagnostics = append(s.diagnostics, truncated)
		}
	}
}

// finish returns the ordered diagnostics.
func (s *diagnosticSink) finish() []*protocol.Diagnostic { return s.diagnostics }

// sourceDiagnostic constructs one located diagnostic; construction
// failures are protocol errors and must not happen for the frozen codes.
func sourceDiagnostic(authority document.DocumentAuthority, code string,
	category protocol.DiagnosticCategory, severity protocol.Severity, start, end int) *protocol.Diagnostic {
	location, err := authority.Span(start, end)
	if err != nil {
		return nil
	}
	diagnostic, err := protocol.NewDiagnostic(code, category, severity,
		diagnosticLocation(authority, location), nil, nil, nil, nil, 0, errorRegistry())
	if err != nil {
		// The registry is authoritative; an unregistered code is an
		// internal invariant violation.
		panic("json: unregistered diagnostic code " + code + ": " + err.Error())
	}
	return diagnostic
}

// diagnosticLocation maps one span to the transferable location record.
// The source identity is the process-local snapshot identity: stable for
// the lifetime of the process, exactly like the Rust
// DiagnosticLocation.snapshot fact. The caller-assigned stable source
// identity is attached when the diagnostics cross the protocol layer.
func diagnosticLocation(authority document.DocumentAuthority, span document.Span) *protocol.SourceLocation {
	start := span.StartByte()
	end := span.EndByte()
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	location, err := protocol.NewSourceLocation(
		strconv.FormatUint(authority.Identity().AsU64(), 10), uint64(start), uint64(end))
	if err != nil {
		return nil
	}
	return location
}

// sortDiagnostics orders diagnostics deterministically: primary start
// byte (absent last), category, code, occurrence
// (consema-core diagnostic.rs:107-123).
func sortDiagnostics(diagnostics []*protocol.Diagnostic) {
	for i := 1; i < len(diagnostics); i++ {
		for j := i; j > 0 && diagnosticLess(diagnostics[j], diagnostics[j-1]); j-- {
			diagnostics[j], diagnostics[j-1] = diagnostics[j-1], diagnostics[j]
		}
	}
}

// diagnosticLess reports whether left precedes right in the deterministic
// diagnostic order.
func diagnosticLess(left, right *protocol.Diagnostic) bool {
	leftStart := uint64(^uint64(0))
	rightStart := uint64(^uint64(0))
	if left.Primary != nil {
		leftStart = left.Primary.StartByte
	}
	if right.Primary != nil {
		rightStart = right.Primary.StartByte
	}
	if leftStart != rightStart {
		return leftStart < rightStart
	}
	if left.Category != right.Category {
		return left.Category < right.Category
	}
	if left.Code != right.Code {
		return left.Code < right.Code
	}
	return left.Occurrence < right.Occurrence
}
