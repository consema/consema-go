package hcl

import (
	"strings"
	"unicode"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the self-owned HCL tokenizer (RFC 0014 §2, §4.1):
// the token stream, the 30-kind lossless piece assembly (RFC 0014 §7.2),
// and the lexical half of the §12 divergence inventory. The lexer is
// total: every byte of the source is consumed by exactly one token or
// error region, and invalid UTF-8 is a fatal formation failure with
// `hcl.parse.invalid-utf8@1` (RFC 0014 §2, §12 D-3).

// hclTokenKind is the closed token kind set of the self-owned HCL
// tokenizer (RFC 0014 §2, §4.1).
//
// The set is richer than the 30-piece HclSyntaxKind closure: operator
// spellings, the exact trivia runs, and the zero-length Eof terminal are
// token facts. Keyword spellings are Identifier tokens — the literal
// reading is a parser fact. The `~` strip markers of interpolations and
// directives are span-internal facts of the open/close tokens.
type hclTokenKind uint8

// The closed token kinds.
const (
	tokWhitespace hclTokenKind = iota
	tokLineBreak
	tokLineComment
	tokInlineComment
	tokIdentifier
	tokNumber
	tokEquals
	tokStringOpen
	tokStringContent
	tokStringClose
	tokInterpolationOpen
	tokInterpolationContent
	tokInterpolationClose
	tokDirectiveOpen
	tokDirectiveContent
	tokDirectiveClose
	tokHeredocOpen
	tokHeredocContent
	tokHeredocClose
	tokDot
	tokComma
	tokColon
	tokQuestionMark
	tokArrow
	tokEllipsis
	tokStar
	tokBraceOpen
	tokBraceClose
	tokBracketOpen
	tokBracketClose
	tokParenOpen
	tokParenClose
	tokOpEqual
	tokOpNotEqual
	tokOpLess
	tokOpGreater
	tokOpLessEqual
	tokOpGreaterEqual
	tokOpAdd
	tokOpSubtract
	tokOpNot
	tokOpDivide
	tokOpModulo
	tokOpAnd
	tokOpOr
	tokErrorRegion
	tokEof
)

// syntaxKind maps one token kind onto its lossless syntax kind; the second
// result is false for the zero-length Eof terminal, which has no piece
// (RFC 0014 §7.2).
func (k hclTokenKind) syntaxKind() (HclSyntaxKind, bool) {
	switch k {
	case tokWhitespace:
		return HclSyntaxKindWhitespace, true
	case tokLineBreak:
		return HclSyntaxKindLineBreak, true
	case tokLineComment:
		return HclSyntaxKindLineComment, true
	case tokInlineComment:
		return HclSyntaxKindInlineComment, true
	case tokIdentifier:
		return HclSyntaxKindIdentifier, true
	case tokNumber:
		return HclSyntaxKindNumber, true
	case tokEquals:
		return HclSyntaxKindEquals, true
	case tokStringOpen:
		return HclSyntaxKindStringOpen, true
	case tokStringContent:
		return HclSyntaxKindStringContent, true
	case tokStringClose:
		return HclSyntaxKindStringClose, true
	case tokInterpolationOpen:
		return HclSyntaxKindInterpolationOpen, true
	case tokInterpolationContent:
		return HclSyntaxKindInterpolationContent, true
	case tokInterpolationClose:
		return HclSyntaxKindInterpolationClose, true
	case tokDirectiveOpen:
		return HclSyntaxKindDirectiveOpen, true
	case tokDirectiveContent:
		return HclSyntaxKindDirectiveContent, true
	case tokDirectiveClose:
		return HclSyntaxKindDirectiveClose, true
	case tokHeredocOpen:
		return HclSyntaxKindHeredocOpen, true
	case tokHeredocContent:
		return HclSyntaxKindHeredocContent, true
	case tokHeredocClose:
		return HclSyntaxKindHeredocClose, true
	case tokBraceOpen:
		return HclSyntaxKindBraceOpen, true
	case tokBraceClose:
		return HclSyntaxKindBraceClose, true
	case tokBracketOpen:
		return HclSyntaxKindBracketOpen, true
	case tokBracketClose:
		return HclSyntaxKindBracketClose, true
	case tokParenOpen:
		return HclSyntaxKindParenOpen, true
	case tokParenClose:
		return HclSyntaxKindParenClose, true
	case tokComma:
		return HclSyntaxKindComma, true
	case tokColon:
		return HclSyntaxKindColon, true
	case tokQuestionMark:
		return HclSyntaxKindQuestionMark, true
	case tokErrorRegion:
		return HclSyntaxKindErrorRegion, true
	case tokDot, tokArrow, tokEllipsis, tokStar, tokOpEqual, tokOpNotEqual,
		tokOpLess, tokOpGreater, tokOpLessEqual, tokOpGreaterEqual, tokOpAdd,
		tokOpSubtract, tokOpNot, tokOpDivide, tokOpModulo, tokOpAnd, tokOpOr:
		return HclSyntaxKindOperator, true
	case tokEof:
		return 0, false
	}
	return HclSyntaxKindErrorRegion, true
}

// structuralKind classifies one token's piece (token, trivia, or error
// region).
func (k hclTokenKind) structuralKind() document.StructuralPieceKind {
	switch k {
	case tokWhitespace, tokLineBreak, tokLineComment, tokInlineComment:
		return document.PieceTrivia
	case tokErrorRegion:
		return document.PieceErrorRegion
	default:
		return document.PieceToken
	}
}

// hclToken is one lexical token with its exact half-open raw-byte span.
//
// Every non-empty raw byte of a formed source belongs to exactly one token
// (the zero-length Eof terminal has no piece). The exact token text is
// always derived from the span against the frozen source — never copied.
type hclToken struct {
	kind hclTokenKind
	span document.Span
}

// lexOutput is the result of one lexer pass: the ordered token stream, the
// recovered error regions, the ordered diagnostics, and the lossless
// 30-kind piece index (RFC 0014 §2, §7.2).
//
// buildIndex is false for a region lex (an interpolation or directive
// interior), whose tokens still carry exact spans bound to the same
// authority but do not form a source-covering index.
type lexOutput struct {
	source       *document.SourceSnapshot
	authority    document.DocumentAuthority
	tokens       []hclToken
	errorRegions []HclErrorRegion
	diagnostics  []*protocol.Diagnostic
	recovered    bool
	index        *document.LosslessStructuralIndex
	syntaxKinds  []HclSyntaxKind
}

// templateFrame models the template nesting of RFC 0014 §4.4-§4.5: quoted
// templates and heredocs contain interpolation and directive sequences
// whose interiors are expressions, which may contain nested templates
// again. Interpolation and directive interiors are scanned but not
// emitted — the interior is one opaque content token — so the frame stack
// exists to find the matching `}` at the right brace depth and to enforce
// the template nesting limits.
type templateFrame struct {
	tag templateFrameTag
	// Quoted payload.
	open           int
	buffer         []hclToken
	interpolations int
	// Heredoc payload.
	marker       string
	contentStart int
	heredocBytes int
	lines        int
	// Interp payload.
	directive     bool
	depth         int
	interiorStart int
}

type templateFrameTag uint8

const (
	frameQuoted templateFrameTag = iota
	frameHeredoc
	frameInterp
)

// diagnosticSink is the bounded ordered diagnostic recording with the
// house truncation marker.
type diagnosticSink struct {
	diagnostics []*protocol.Diagnostic
	max         int
	occurrence  uint64
}

func newDiagnosticSink(max int) *diagnosticSink {
	return &diagnosticSink{max: max}
}

func (s *diagnosticSink) push(code string, category protocol.DiagnosticCategory,
	span *document.Span) {
	diagnostic := &protocol.Diagnostic{
		Code:       code,
		Category:   category,
		Severity:   protocol.SeverityError,
		Arguments:  map[string]string{},
		Occurrence: s.occurrence,
	}
	s.occurrence++
	if span != nil {
		diagnostic.Primary = &protocol.SourceLocation{
			SourceID:  "snapshot",
			StartByte: uint64(span.StartByte()),
			EndByte:   uint64(span.EndByte()),
		}
	}
	if len(s.diagnostics) < s.max {
		s.diagnostics = append(s.diagnostics, diagnostic)
	} else if len(s.diagnostics) == s.max {
		s.diagnostics = append(s.diagnostics, &protocol.Diagnostic{
			Code:       "core.diagnostic.truncated@1",
			Category:   protocol.CategoryResource,
			Severity:   protocol.SeverityWarning,
			Arguments:  map[string]string{},
			Occurrence: s.occurrence,
		})
	}
}

// hclLexError is the typed internal formation failure of one lexer pass.
type hclLexError struct {
	code      string
	category  protocol.DiagnosticCategory
	arguments map[string]string
}

// Error implements error.
func (e *hclLexError) Error() string { return "hcl: " + e.code }

// lexError builds a fatal lexer error.
func lexError(code string, category protocol.DiagnosticCategory) *hclLexError {
	return &hclLexError{code: code, category: category, arguments: map[string]string{}}
}

// lexErrorLimit builds a `hcl.limit.<name>@1` resource-limit failure (RFC
// 0014 §11).
func lexErrorLimit(name string, observed, limit int) *hclLexError {
	return &hclLexError{
		code:      "hcl.limit." + name + "@1",
		category:  protocol.CategoryResource,
		arguments: map[string]string{"limit": intString(limit), "observed": intString(observed)},
	}
}

// Stable `hcl.parse.*@1` diagnostic codes (RFC 0014 §2, §4.1, §4.4, §4.5,
// §11).
const (
	codeByteOrderMark         = "hcl.parse.byte-order-mark@1"
	codeLoneCR                = "hcl.parse.lone-cr@1"
	codeInvalidUTF8           = "hcl.parse.invalid-utf8@1"
	codeIdentifier            = "hcl.parse.identifier@1"
	codeInvalidNumber         = "hcl.parse.invalid-number@1"
	codeInvalidCharacter      = "hcl.parse.invalid-character@1"
	codeInvalidEscape         = "hcl.parse.invalid-escape@1"
	codeUnterminatedComment   = "hcl.parse.unterminated-comment@1"
	codeUnterminatedString    = "hcl.parse.unterminated-string@1"
	codeUnterminatedInterp    = "hcl.parse.unterminated-interpolation@1"
	codeUnterminatedDirective = "hcl.parse.unterminated-directive@1"
	codeUnterminatedHeredoc   = "hcl.parse.unterminated-heredoc@1"
	codeHeredocMarker         = "hcl.parse.heredoc-marker@1"
)

// lexer is the deterministic single-pass tokenizer over a decoded UTF-8
// source. Byte offsets are decoded-text offsets; under the UTF-8-only
// source contract they are identical to raw-byte offsets, so every span is
// issued directly against the snapshot length.
type lexer struct {
	source     *document.SourceSnapshot
	decoded    string
	authority  document.DocumentAuthority
	limits     HclParseLimits
	pos        int
	end        int
	buildIndex bool

	tokens       []hclToken
	errorRegions []HclErrorRegion
	sink         *diagnosticSink
	recovered    bool
	buffered     int
	stack        []templateFrame
}

func newLexer(source *document.SourceSnapshot, authority document.DocumentAuthority,
	limits HclParseLimits, start, end int, buildIndex bool) *lexer {
	return &lexer{
		source:     source,
		decoded:    decodedTextOf(source),
		authority:  authority,
		limits:     limits,
		pos:        start,
		end:        end,
		buildIndex: buildIndex,
		sink:       newDiagnosticSink(limits.Common.MaxDiagnostics),
	}
}

// decodedTextOf returns the decoded text view; the UTF-8 source contract
// guarantees its presence.
func decodedTextOf(source *document.SourceSnapshot) string {
	text, _ := source.DecodedText()
	return text
}

func (l *lexer) scan() error {
	for l.pos < l.end {
		top := l.topFrame()
		switch {
		case top == nil:
			if err := l.scanRoot(); err != nil {
				return err
			}
		case top.tag == frameInterp:
			if err := l.scanAbsorb(); err != nil {
				return err
			}
		case top.tag == frameQuoted:
			if err := l.scanQuoted(); err != nil {
				return err
			}
		case top.tag == frameHeredoc:
			if err := l.scanHeredoc(); err != nil {
				return err
			}
		}
	}
	return l.finishEOF()
}

func (l *lexer) topFrame() *templateFrame {
	if len(l.stack) == 0 {
		return nil
	}
	return &l.stack[len(l.stack)-1]
}

// emitting reports whether the current position is outside every
// interpolation/directive interior, i.e. whether tokens are emitted at
// all.
func (l *lexer) emitting() bool {
	for i := range l.stack {
		if l.stack[i].tag == frameInterp {
			return false
		}
	}
	return true
}

func (l *lexer) byte() (byte, bool) {
	if l.pos >= l.end {
		return 0, false
	}
	return l.decoded[l.pos], true
}

func (l *lexer) byteAt(offset int) (byte, bool) {
	if l.pos+offset >= l.end || l.pos+offset < 0 {
		return 0, false
	}
	return l.decoded[l.pos+offset], true
}

func (l *lexer) charAt(pos int) (rune, bool) {
	if pos >= l.end {
		return 0, false
	}
	text := l.decoded[pos:l.end]
	if text == "" {
		return 0, false
	}
	return []rune(text)[0], true
}

func (l *lexer) span(start, end int) (document.Span, error) {
	if start > end || end > l.source.Len() {
		return document.Span{}, &hclLexError{code: "hcl.parse.coordinates@1",
			category: protocol.CategorySyntax, arguments: map[string]string{}}
	}
	return l.authority.Span(start, end)
}

// scanRoot scans one root-level (body or expression) token.
func (l *lexer) scanRoot() error {
	byte, ok := l.byte()
	if !ok {
		return nil
	}
	switch byte {
	case ' ', '\t':
		start := l.pos
		for {
			next, ok := l.byte()
			if !ok || (next != ' ' && next != '\t') {
				break
			}
			l.pos++
		}
		return l.emitKind(tokWhitespace, start, l.pos)
	case '\n':
		if err := l.emitKind(tokLineBreak, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '\r':
		if next, ok := l.byteAt(1); ok && next == '\n' {
			if err := l.emitKind(tokLineBreak, l.pos, l.pos+2); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if err := l.emitErrorRegion(l.pos, l.pos+1, codeLoneCR, protocol.CategoryLexical); err != nil {
			return err
		}
		l.pos++
		return nil
	case '#':
		return l.scanLineComment(true)
	case '/':
		if next, ok := l.byteAt(1); ok && next == '/' {
			return l.scanLineComment(true)
		}
		if next, ok := l.byteAt(1); ok && next == '*' {
			return l.scanInlineComment(true)
		}
		if err := l.emitKind(tokOpDivide, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '"':
		return l.openQuoted(true)
	case '<':
		if next, ok := l.byteAt(1); ok && next == '<' {
			return l.openHeredoc(true)
		}
		if next, ok := l.byteAt(1); ok && next == '=' {
			if err := l.emitKind(tokOpLessEqual, l.pos, l.pos+2); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if err := l.emitKind(tokOpLess, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '>':
		if next, ok := l.byteAt(1); ok && next == '=' {
			if err := l.emitKind(tokOpGreaterEqual, l.pos, l.pos+2); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if err := l.emitKind(tokOpGreater, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '=':
		if next, ok := l.byteAt(1); ok && next == '=' {
			if err := l.emitKind(tokOpEqual, l.pos, l.pos+2); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if next, ok := l.byteAt(1); ok && next == '>' {
			if err := l.emitKind(tokArrow, l.pos, l.pos+2); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if err := l.emitKind(tokEquals, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '!':
		if next, ok := l.byteAt(1); ok && next == '=' {
			if err := l.emitKind(tokOpNotEqual, l.pos, l.pos+2); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if err := l.emitKind(tokOpNot, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '-':
		if err := l.emitKind(tokOpSubtract, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '+':
		if err := l.emitKind(tokOpAdd, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '*':
		if err := l.emitKind(tokStar, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '%':
		if err := l.emitKind(tokOpModulo, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '&':
		if next, ok := l.byteAt(1); ok && next == '&' {
			if err := l.emitKind(tokOpAnd, l.pos, l.pos+2); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if err := l.emitErrorRegion(l.pos, l.pos+1, codeInvalidCharacter, protocol.CategorySyntax); err != nil {
			return err
		}
		l.pos++
		return nil
	case '|':
		if next, ok := l.byteAt(1); ok && next == '|' {
			if err := l.emitKind(tokOpOr, l.pos, l.pos+2); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if err := l.emitErrorRegion(l.pos, l.pos+1, codeInvalidCharacter, protocol.CategorySyntax); err != nil {
			return err
		}
		l.pos++
		return nil
	case '?':
		if err := l.emitKind(tokQuestionMark, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case ':':
		if next, ok := l.byteAt(1); ok && next == ':' {
			// `::` is never an operator: the namespaced function form has
			// no spec production (RFC 0014 §12 D-6).
			if err := l.emitErrorRegion(l.pos, l.pos+2, codeInvalidCharacter, protocol.CategorySyntax); err != nil {
				return err
			}
			l.pos += 2
			return nil
		}
		if err := l.emitKind(tokColon, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case ',':
		if err := l.emitKind(tokComma, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '.':
		if next1, ok1 := l.byteAt(1); ok1 && next1 == '.' {
			if next2, ok2 := l.byteAt(2); ok2 && next2 == '.' {
				if err := l.emitKind(tokEllipsis, l.pos, l.pos+3); err != nil {
					return err
				}
				l.pos += 3
				return nil
			}
		}
		if err := l.emitKind(tokDot, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '{':
		if err := l.emitKind(tokBraceOpen, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '}':
		if err := l.emitKind(tokBraceClose, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '[':
		if err := l.emitKind(tokBracketOpen, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case ']':
		if err := l.emitKind(tokBracketClose, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '(':
		if err := l.emitKind(tokParenOpen, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case ')':
		if err := l.emitKind(tokParenClose, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '~', '\\', '$':
		if err := l.emitErrorRegion(l.pos, l.pos+1, codeInvalidCharacter, protocol.CategorySyntax); err != nil {
			return err
		}
		l.pos++
		return nil
	}
	if byte >= '0' && byte <= '9' {
		return l.scanNumber(true)
	}
	ch, _ := l.charAt(l.pos)
	switch {
	case ch == '\uFEFF':
		if err := l.emitErrorRegion(l.pos, l.pos+3, codeByteOrderMark, protocol.CategoryEncoding); err != nil {
			return err
		}
		l.pos += 3
		return nil
	case ch == '_':
		// `_` is not an ID_Start (RFC 0014 §4.1, §12 D-4).
		if err := l.emitErrorRegion(l.pos, l.pos+1, codeIdentifier, protocol.CategorySyntax); err != nil {
			return err
		}
		l.pos++
		return nil
	case isIdentifierStart(ch):
		return l.scanIdentifier(true)
	default:
		width := runeWidthOf(ch)
		if err := l.emitErrorRegion(l.pos, l.pos+width, codeInvalidCharacter, protocol.CategorySyntax); err != nil {
			return err
		}
		l.pos += width
		return nil
	}
}

// scanAbsorb scans one token inside an interpolation or directive interior:
// the interior is absorbed (no tokens), braces are balanced, and the
// sequence closes at the `}` (or `~}`) at depth zero.
func (l *lexer) scanAbsorb() error {
	byte, ok := l.byte()
	if !ok {
		return nil
	}
	switch byte {
	case '{':
		top := l.topFrame()
		top.depth++
		l.pos++
		return nil
	case '}', '~':
		top := l.topFrame()
		depth := top.depth
		directive := top.directive
		interiorStart := top.interiorStart
		closeWidth := 0
		if byte == '~' {
			if depth == 0 {
				if next, ok := l.byteAt(1); ok && next == '}' {
					closeWidth = 2
				}
			}
		} else if depth == 0 {
			closeWidth = 1
		}
		if closeWidth > 0 {
			closeStart := l.pos
			l.pos += closeWidth
			contentKind := tokInterpolationContent
			closeKind := tokInterpolationClose
			if directive {
				contentKind = tokDirectiveContent
				closeKind = tokDirectiveClose
			}
			contentSpan, err := l.span(interiorStart, closeStart)
			if err != nil {
				return err
			}
			closeSpan, err := l.span(closeStart, l.pos)
			if err != nil {
				return err
			}
			l.stack = l.stack[:len(l.stack)-1]
			if l.emitting() {
				if err := l.emit(hclToken{kind: contentKind, span: contentSpan}); err != nil {
					return err
				}
				if err := l.emit(hclToken{kind: closeKind, span: closeSpan}); err != nil {
					return err
				}
			}
			return nil
		}
		if byte == '}' {
			top := l.topFrame()
			top.depth--
			l.pos++
		} else {
			if err := l.recover(codeInvalidCharacter, protocol.CategorySyntax, l.pos, l.pos+1); err != nil {
				return err
			}
			l.pos++
		}
		return nil
	case '"':
		open := l.pos
		l.pos++
		if err := l.checkTemplateDepth(); err != nil {
			return err
		}
		l.stack = append(l.stack, templateFrame{tag: frameQuoted, open: open})
		return nil
	case '<':
		if next, ok := l.byteAt(1); ok && next == '<' {
			return l.openHeredoc(false)
		}
		if next, ok := l.byteAt(1); ok && next == '=' {
			l.pos += 2
			return nil
		}
		l.pos++
		return nil
	case '>', '!', '=', '&', '|', ':', '.', '+', '-', '*', '%', '?', ',',
		'(', ')', '[', ']', ' ', '\t', '#':
		if next, ok := l.byteAt(1); ok {
			switch {
			case (byte == '>' || byte == '!' || byte == '=') && next == '=':
				l.pos += 2
				return nil
			case byte == '=' && next == '>':
				l.pos += 2
				return nil
			case byte == '&' && next == '&':
				l.pos += 2
				return nil
			case byte == '|' && next == '|':
				l.pos += 2
				return nil
			case byte == ':' && next == ':':
				if err := l.recover(codeInvalidCharacter, protocol.CategorySyntax, l.pos, l.pos+2); err != nil {
					return err
				}
				l.pos += 2
				return nil
			case byte == '.' && next == '.':
				if third, ok := l.byteAt(2); ok && third == '.' {
					l.pos += 3
					return nil
				}
			}
		}
		if byte == '&' || byte == '|' {
			if err := l.recover(codeInvalidCharacter, protocol.CategorySyntax, l.pos, l.pos+1); err != nil {
				return err
			}
		}
		l.pos++
		return nil
	case '\\', '$':
		if err := l.recover(codeInvalidCharacter, protocol.CategorySyntax, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '\n':
		l.pos++
		return l.noteHeredocLine()
	case '\r':
		if next, ok := l.byteAt(1); ok && next == '\n' {
			l.pos += 2
			return l.noteHeredocLine()
		}
		if err := l.recover(codeLoneCR, protocol.CategoryLexical, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case '/':
		if next, ok := l.byteAt(1); ok && next == '/' {
			return l.scanLineComment(false)
		}
		if next, ok := l.byteAt(1); ok && next == '*' {
			return l.scanInlineComment(false)
		}
		l.pos++
		return nil
	}
	if byte >= '0' && byte <= '9' {
		return l.scanNumber(false)
	}
	ch, _ := l.charAt(l.pos)
	switch {
	case ch == '\uFEFF':
		if err := l.recover(codeByteOrderMark, protocol.CategoryEncoding, l.pos, l.pos+3); err != nil {
			return err
		}
		l.pos += 3
		return nil
	case ch == '_':
		if err := l.recover(codeIdentifier, protocol.CategorySyntax, l.pos, l.pos+1); err != nil {
			return err
		}
		l.pos++
		return nil
	case isIdentifierStart(ch):
		return l.scanIdentifier(false)
	default:
		width := runeWidthOf(ch)
		if err := l.recover(codeInvalidCharacter, protocol.CategorySyntax, l.pos, l.pos+width); err != nil {
			return err
		}
		l.pos += width
		return nil
	}
}

// scanQuoted scans quoted-template content up to the closing quote, an
// interpolation or directive opening, a raw newline, or end of source.
func (l *lexer) scanQuoted() error {
	emit := l.emitting()
	runStart := l.pos
	for {
		byte, ok := l.byte()
		if !ok {
			break
		}
		switch byte {
		case '"':
			if err := l.endRun(runStart, emit, tokStringContent); err != nil {
				return err
			}
			closeStart := l.pos
			l.pos++
			top := l.topFrame()
			open := top.open
			spanLen := l.pos - open
			if spanLen > l.limits.MaxStringLen {
				return lexErrorLimit("string-len", spanLen, l.limits.MaxStringLen)
			}
			if spanLen > l.limits.MaxTemplateLen {
				return lexErrorLimit("template-len", spanLen, l.limits.MaxTemplateLen)
			}
			if emit {
				if err := l.flushBuffer(); err != nil {
					return err
				}
				l.stack = l.stack[:len(l.stack)-1]
				if err := l.emitKind(tokStringClose, closeStart, l.pos); err != nil {
					return err
				}
			} else {
				l.stack = l.stack[:len(l.stack)-1]
			}
			return nil
		case '$':
			if next1, ok1 := l.byteAt(1); ok1 && next1 == '$' {
				if next2, ok2 := l.byteAt(2); ok2 && next2 == '{' {
					l.pos += 3
					continue
				}
			}
			if next, ok := l.byteAt(1); ok && next == '{' {
				if err := l.endRun(runStart, emit, tokStringContent); err != nil {
					return err
				}
				return l.openInterpolation(false, emit)
			}
			l.pos++
		case '%':
			if next1, ok1 := l.byteAt(1); ok1 && next1 == '%' {
				if next2, ok2 := l.byteAt(2); ok2 && next2 == '{' {
					l.pos += 3
					continue
				}
			}
			if next, ok := l.byteAt(1); ok && next == '{' {
				if err := l.endRun(runStart, emit, tokStringContent); err != nil {
					return err
				}
				return l.openInterpolation(true, emit)
			}
			l.pos++
		case '\\':
			if next, ok := l.byteAt(1); ok && next == '\n' {
				// A backslash-newline is not an admitted escape and a raw
				// newline is not permitted in a quoted template; the
				// sequence is one invalid escape and the template
				// continues.
				if err := l.recover(codeInvalidEscape, protocol.CategorySyntax, l.pos, l.pos+2); err != nil {
					return err
				}
				l.pos += 2
			} else if next1, ok1 := l.byteAt(1); ok1 && next1 == '\r' {
				if next2, ok2 := l.byteAt(2); ok2 && next2 == '\n' {
					if err := l.recover(codeInvalidEscape, protocol.CategorySyntax, l.pos, l.pos+3); err != nil {
						return err
					}
					l.pos += 3
				} else if err := l.scanEscape(); err != nil {
					return err
				}
			} else if err := l.scanEscape(); err != nil {
				return err
			}
		case '\n':
			return l.terminateString(l.pos)
		case '\r':
			if next, ok := l.byteAt(1); ok && next == '\n' {
				return l.terminateString(l.pos)
			}
			if err := l.endRun(runStart, emit, tokStringContent); err != nil {
				return err
			}
			if emit {
				if err := l.emitErrorRegion(l.pos, l.pos+1, codeLoneCR, protocol.CategoryLexical); err != nil {
					return err
				}
			} else if err := l.recover(codeLoneCR, protocol.CategoryLexical, l.pos, l.pos+1); err != nil {
				return err
			}
			l.pos++
			runStart = l.pos
		default:
			ch, _ := l.charAt(l.pos)
			l.pos += runeWidthOf(ch)
		}
	}
	return l.terminateString(l.end)
}

// scanEscape validates one escape sequence of a quoted template (RFC 0014
// §4.4): `\n` `\r` `\t` `\"` `\\` `\uNNNN` `\UNNNNNNNN`.
func (l *lexer) scanEscape() error {
	start := l.pos
	l.pos++
	ch, ok := l.charAt(l.pos)
	if !ok {
		return l.recover(codeInvalidEscape, protocol.CategorySyntax, start, l.pos)
	}
	l.pos += runeWidthOf(ch)
	valid := false
	switch ch {
	case 'n', 'r', 't', '"', '\\':
		valid = true
	case 'u':
		digitsStart := l.pos
		consumed := l.consumeHex(4)
		valid = consumed == 4 && validHexScalar(l.decoded[digitsStart:l.pos], 4, false)
	case 'U':
		digitsStart := l.pos
		consumed := l.consumeHex(8)
		valid = consumed == 8 && validHexScalar(l.decoded[digitsStart:l.pos], 8, true)
	}
	if !valid {
		return l.recover(codeInvalidEscape, protocol.CategorySyntax, start, l.pos)
	}
	return nil
}

// validHexScalar validates one decoded hex escape value: no surrogates,
// and at most U+10FFFF for the 8-digit form.
func validHexScalar(text string, digits int, wide bool) bool {
	if len(text) != digits {
		return false
	}
	var value uint32
	for i := 0; i < len(text); i++ {
		digit := hexDigit(text[i])
		if digit < 0 {
			return false
		}
		value = value*16 + uint32(digit)
	}
	if value >= 0xD800 && value <= 0xDFFF {
		return false
	}
	if wide && value > 0x10FFFF {
		return false
	}
	return true
}

func hexDigit(byte byte) int {
	switch {
	case byte >= '0' && byte <= '9':
		return int(byte - '0')
	case byte >= 'a' && byte <= 'f':
		return int(byte-'a') + 10
	case byte >= 'A' && byte <= 'F':
		return int(byte-'A') + 10
	}
	return -1
}

// consumeHex consumes up to count ASCII hex digits; returns how many were
// found.
func (l *lexer) consumeHex(count int) int {
	consumed := 0
	for consumed < count {
		byte, ok := l.byte()
		if !ok || hexDigit(byte) < 0 {
			break
		}
		l.pos++
		consumed++
	}
	return consumed
}

// scanHeredoc scans one heredoc content line or the closing marker line.
func (l *lexer) scanHeredoc() error {
	if l.pos >= l.end {
		return l.terminateHeredoc(l.end)
	}
	if err := l.noteHeredocContent(); err != nil {
		return err
	}
	emit := l.emitting()
	atLineStart := l.pos == 0 || l.decoded[l.pos-1] == '\n'
	lineEnd := l.findLineEnd()
	if atLineStart {
		trimmed := stringsTrimSpace(l.decoded[l.pos:lineEnd])
		top := l.topFrame()
		isClosing := trimmed == top.marker
		if isClosing {
			// The closing marker line; the whole line is HeredocClose.
			if emit {
				if err := l.flushBuffer(); err != nil {
					return err
				}
			}
			l.stack = l.stack[:len(l.stack)-1]
			if emit {
				if err := l.emitKind(tokHeredocClose, l.pos, lineEnd); err != nil {
					return err
				}
			}
			if lineEnd < l.end {
				if emit {
					if err := l.emitKind(tokLineBreak, lineEnd, lineEnd+1); err != nil {
						return err
					}
				}
				l.pos = lineEnd + 1
			} else {
				l.pos = lineEnd
			}
			return nil
		}
	}
	return l.scanHeredocLine(lineEnd)
}

// scanHeredocLine template-scans one heredoc content line: literal runs
// stay HeredocContent, and `${`/`%{` open interpolation/directive
// sequences (RFC 0014 §4.5).
func (l *lexer) scanHeredocLine(lineEnd int) error {
	emit := l.emitting()
	runStart := l.pos
	for l.pos < lineEnd {
		byte := l.decoded[l.pos]
		switch byte {
		case '$':
			if next1, ok1 := l.byteAt(1); ok1 && next1 == '$' {
				if next2, ok2 := l.byteAt(2); ok2 && next2 == '{' {
					l.pos += 3
					continue
				}
			}
			if next, ok := l.byteAt(1); ok && next == '{' {
				if err := l.endRun(runStart, emit, tokHeredocContent); err != nil {
					return err
				}
				return l.openInterpolation(false, emit)
			}
			l.pos++
		case '%':
			if next1, ok1 := l.byteAt(1); ok1 && next1 == '%' {
				if next2, ok2 := l.byteAt(2); ok2 && next2 == '{' {
					l.pos += 3
					continue
				}
			}
			if next, ok := l.byteAt(1); ok && next == '{' {
				if err := l.endRun(runStart, emit, tokHeredocContent); err != nil {
					return err
				}
				return l.openInterpolation(true, emit)
			}
			l.pos++
		case '\r':
			if l.pos+1 == lineEnd {
				if next, ok := l.byteAt(1); ok && next == '\n' {
					// The CR of a line-ending CRLF stays inside the content
					// run; the newline after it is a LineBreak.
					l.pos++
					continue
				}
			}
			if err := l.endRun(runStart, emit, tokHeredocContent); err != nil {
				return err
			}
			if emit {
				if err := l.emitErrorRegion(l.pos, l.pos+1, codeLoneCR, protocol.CategoryLexical); err != nil {
					return err
				}
			} else if err := l.recover(codeLoneCR, protocol.CategoryLexical, l.pos, l.pos+1); err != nil {
				return err
			}
			l.pos++
			runStart = l.pos
		default:
			ch, _ := l.charAt(l.pos)
			l.pos += runeWidthOf(ch)
		}
	}
	if err := l.endRun(runStart, emit, tokHeredocContent); err != nil {
		return err
	}
	if lineEnd < l.end {
		if emit {
			if err := l.emitKind(tokLineBreak, lineEnd, lineEnd+1); err != nil {
				return err
			}
		}
		l.pos = lineEnd + 1
	} else {
		l.pos = lineEnd
	}
	if err := l.noteHeredocLine(); err != nil {
		return err
	}
	return l.noteHeredocContent()
}

// openQuoted opens a quoted template at the current `"`.
func (l *lexer) openQuoted(emit bool) error {
	open := l.pos
	l.pos++
	if err := l.checkTemplateDepth(); err != nil {
		return err
	}
	if emit {
		if err := l.emitKind(tokStringOpen, open, l.pos); err != nil {
			return err
		}
	}
	l.stack = append(l.stack, templateFrame{tag: frameQuoted, open: open})
	return nil
}

// openHeredoc opens a heredoc at the current `<<` or `<<-`, or reports a
// `hcl.parse.heredoc-marker@1` error region when the introducer does not
// form one (RFC 0014 §4.5).
func (l *lexer) openHeredoc(emit bool) error {
	start := l.pos
	l.pos += 2
	if byte, ok := l.byte(); ok && byte == '-' {
		l.pos++
	}
	markerStart := l.pos
	if ch, ok := l.charAt(l.pos); ok && isIdentifierStart(ch) {
		for {
			ch, ok := l.charAt(l.pos)
			if !ok {
				break
			}
			if isIdentifierContinue(ch) || ch == '-' {
				l.pos += runeWidthOf(ch)
			} else {
				break
			}
		}
		markerLen := l.pos - markerStart
		if markerLen > l.limits.MaxIdentifierLen {
			return lexErrorLimit("identifier-len", markerLen, l.limits.MaxIdentifierLen)
		}
		marker := l.decoded[markerStart:l.pos]
		// The introducer line ends with spaces or tabs and a newline (or
		// end of file); anything else is not a heredoc introduction.
		lineCursor := l.pos
		for lineCursor < l.end {
			byte := l.decoded[lineCursor]
			if byte != ' ' && byte != '\t' {
				break
			}
			lineCursor++
		}
		newlineOK := lineCursor >= l.end ||
			l.decoded[lineCursor] == '\n' ||
			(l.decoded[lineCursor] == '\r' && lineCursor+1 < l.end && l.decoded[lineCursor+1] == '\n')
		if newlineOK {
			if emit {
				if err := l.emitKind(tokHeredocOpen, start, l.pos); err != nil {
					return err
				}
				if lineCursor > l.pos {
					if err := l.emitKind(tokWhitespace, l.pos, lineCursor); err != nil {
						return err
					}
				}
				if lineCursor < l.end {
					newlineEnd := lineCursor + 1
					if l.decoded[lineCursor] == '\r' {
						newlineEnd = lineCursor + 2
					}
					if err := l.emitKind(tokLineBreak, lineCursor, newlineEnd); err != nil {
						return err
					}
					l.pos = newlineEnd
				} else {
					l.pos = lineCursor
				}
			} else if lineCursor < l.end {
				if l.decoded[lineCursor] == '\r' {
					l.pos = lineCursor + 2
				} else {
					l.pos = lineCursor + 1
				}
			} else {
				l.pos = lineCursor
			}
			if err := l.checkTemplateDepth(); err != nil {
				return err
			}
			l.stack = append(l.stack, templateFrame{
				tag:          frameHeredoc,
				marker:       marker,
				contentStart: l.pos,
			})
			return nil
		}
	}
	if emit {
		if err := l.emitErrorRegion(start, l.pos, codeHeredocMarker, protocol.CategorySyntax); err != nil {
			return err
		}
	} else if err := l.recover(codeHeredocMarker, protocol.CategorySyntax, start, l.pos); err != nil {
		return err
	}
	return nil
}

// openInterpolation opens an interpolation (`${`) or directive (`%{`)
// sequence inside a template, with the optional `~` strip marker included
// in the open token.
func (l *lexer) openInterpolation(directive bool, emit bool) error {
	openStart := l.pos
	l.pos += 2
	if byte, ok := l.byte(); ok && byte == '~' {
		l.pos++
	}
	top := l.topFrame()
	if top != nil && top.tag != frameInterp {
		top.interpolations++
		if top.interpolations > l.limits.MaxTemplateInterpolations {
			return lexErrorLimit("template-interpolations",
				top.interpolations, l.limits.MaxTemplateInterpolations)
		}
	}
	if err := l.checkTemplateDepth(); err != nil {
		return err
	}
	if emit {
		kind := tokInterpolationOpen
		if directive {
			kind = tokDirectiveOpen
		}
		if err := l.emitKind(kind, openStart, l.pos); err != nil {
			return err
		}
	}
	l.stack = append(l.stack, templateFrame{tag: frameInterp, directive: directive, interiorStart: l.pos})
	return nil
}

// terminateString terminates an unterminated quoted template: the buffered
// content is discarded and the content becomes one error region to end
// (the newline or end of file), with `hcl.parse.unterminated-string@1`
// (RFC 0014 §3).
func (l *lexer) terminateString(end int) error {
	top := l.topFrame()
	if top == nil || top.tag != frameQuoted {
		return &hclLexError{code: "hcl.parse.internal@1", category: protocol.CategoryResource}
	}
	open := top.open
	bufferLen := len(top.buffer)
	spanLen := end - open
	if spanLen > l.limits.MaxStringLen {
		return lexErrorLimit("string-len", spanLen, l.limits.MaxStringLen)
	}
	if spanLen > l.limits.MaxTemplateLen {
		return lexErrorLimit("template-len", spanLen, l.limits.MaxTemplateLen)
	}
	if l.emitting() {
		l.buffered -= bufferLen
		l.stack = l.stack[:len(l.stack)-1]
		return l.emitErrorRegion(open+1, end, codeUnterminatedString, protocol.CategorySyntax)
	}
	l.stack = l.stack[:len(l.stack)-1]
	return l.recover(codeUnterminatedString, protocol.CategorySyntax, open+1, end)
}

// terminateHeredoc terminates an unterminated heredoc: the buffered
// content is discarded and the content becomes one error region to end of
// file (bounded by the heredoc size limits), with
// `hcl.parse.unterminated-heredoc@1` (RFC 0014 §3, §4.5).
func (l *lexer) terminateHeredoc(end int) error {
	top := l.topFrame()
	if top == nil || top.tag != frameHeredoc {
		return &hclLexError{code: "hcl.parse.internal@1", category: protocol.CategoryResource}
	}
	contentStart := top.contentStart
	bufferLen := len(top.buffer)
	if l.emitting() {
		l.buffered -= bufferLen
		l.stack = l.stack[:len(l.stack)-1]
		return l.emitErrorRegion(contentStart, end, codeUnterminatedHeredoc, protocol.CategorySyntax)
	}
	l.stack = l.stack[:len(l.stack)-1]
	return l.recover(codeUnterminatedHeredoc, protocol.CategorySyntax, contentStart, end)
}

// endRun ends the current literal run as one content token when non-empty.
func (l *lexer) endRun(runStart int, emit bool, kind hclTokenKind) error {
	if emit && l.pos > runStart {
		return l.emitKind(kind, runStart, l.pos)
	}
	return nil
}

// flushBuffer appends the top frame's buffered tokens to the stream.
func (l *lexer) flushBuffer() error {
	top := l.topFrame()
	if top == nil || (top.tag != frameQuoted && top.tag != frameHeredoc) {
		return nil
	}
	l.tokens = append(l.tokens, top.buffer...)
	l.buffered -= len(top.buffer)
	top.buffer = nil
	return nil
}

// noteHeredocLine counts one completed content line of every open heredoc.
func (l *lexer) noteHeredocLine() error {
	for i := range l.stack {
		frame := &l.stack[i]
		if frame.tag == frameHeredoc {
			frame.lines++
			if frame.lines > l.limits.MaxHeredocLines {
				return lexErrorLimit("heredoc-lines", frame.lines, l.limits.MaxHeredocLines)
			}
		}
	}
	return nil
}

// noteHeredocContent re-accounts the content bytes of the open heredoc
// against the heredoc size limits.
func (l *lexer) noteHeredocContent() error {
	top := l.topFrame()
	if top == nil || top.tag != frameHeredoc {
		return nil
	}
	top.heredocBytes = l.pos - top.contentStart
	if top.heredocBytes > l.limits.MaxHeredocBytes {
		return lexErrorLimit("heredoc-bytes", top.heredocBytes, l.limits.MaxHeredocBytes)
	}
	if top.heredocBytes > l.limits.MaxTemplateLen {
		return lexErrorLimit("template-len", top.heredocBytes, l.limits.MaxTemplateLen)
	}
	return nil
}

// checkTemplateDepth checks the template nesting depth before a frame
// push.
func (l *lexer) checkTemplateDepth() error {
	depth := len(l.stack) + 1
	if depth > l.limits.MaxTemplateDepth {
		return lexErrorLimit("template-depth", depth, l.limits.MaxTemplateDepth)
	}
	return nil
}

// emit emits one token, buffering it when an open quoted/heredoc template
// owns the current position.
func (l *lexer) emit(token hclToken) error {
	count := len(l.tokens) + l.buffered + 1
	if count > l.limits.Common.MaxTokenCount {
		return lexErrorLimit("token-count", count, l.limits.Common.MaxTokenCount)
	}
	if count > l.limits.MaxSyntaxPieces {
		return lexErrorLimit("syntax-pieces", count, l.limits.MaxSyntaxPieces)
	}
	if len(l.stack) > 0 {
		top := &l.stack[0]
		if top.tag == frameQuoted || top.tag == frameHeredoc {
			top.buffer = append(top.buffer, token)
			l.buffered++
			return nil
		}
	}
	l.tokens = append(l.tokens, token)
	return nil
}

func (l *lexer) emitKind(kind hclTokenKind, start, end int) error {
	span, err := l.span(start, end)
	if err != nil {
		return err
	}
	return l.emit(hclToken{kind: kind, span: span})
}

// emitErrorRegion emits one error-region token and records its recovery
// fact. A zero-length region publishes the diagnostic but no token — no
// empty piece can exist in the lossless index.
func (l *lexer) emitErrorRegion(start, end int, code string,
	category protocol.DiagnosticCategory) error {
	l.recovered = true
	span, err := l.span(start, end)
	if err != nil {
		return err
	}
	l.sink.push(code, category, &span)
	if end > start {
		if err := l.emit(hclToken{kind: tokErrorRegion, span: span}); err != nil {
			return err
		}
		l.errorRegions = append(l.errorRegions, HclErrorRegion{span: span, code: code})
		if len(l.errorRegions) > l.limits.MaxRecoveryRegions {
			return lexErrorLimit("recovery-regions", len(l.errorRegions), l.limits.MaxRecoveryRegions)
		}
		if len(l.errorRegions) > l.limits.MaxErrorRegions {
			return lexErrorLimit("error-regions", len(l.errorRegions), l.limits.MaxErrorRegions)
		}
	}
	return nil
}

// recover records one recovery diagnostic without a piece (absorbed
// interiors and zero-length regions).
func (l *lexer) recover(code string, category protocol.DiagnosticCategory,
	start, end int) error {
	l.recovered = true
	span, err := l.span(start, end)
	if err != nil {
		return err
	}
	l.sink.push(code, category, &span)
	return nil
}

// finishEOF pops the template stack at end of source with unterminated
// diagnostics. The outermost unterminated template owns the error region
// (when it is emitting); every unterminated construct in the chain
// publishes its diagnostic.
func (l *lexer) finishEOF() error {
	for len(l.stack) > 0 {
		top := l.topFrame()
		switch top.tag {
		case frameInterp:
			code := codeUnterminatedDirective
			if !top.directive {
				code = codeUnterminatedInterp
			}
			interiorStart := top.interiorStart
			l.stack = l.stack[:len(l.stack)-1]
			if err := l.recover(code, protocol.CategorySyntax, interiorStart, l.end); err != nil {
				return err
			}
		case frameQuoted:
			if err := l.terminateString(l.end); err != nil {
				return err
			}
		case frameHeredoc:
			if err := l.terminateHeredoc(l.end); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *lexer) findLineEnd() int {
	for offset := l.pos; offset < l.end; offset++ {
		if l.decoded[offset] == '\n' {
			return offset
		}
	}
	return l.end
}

// scanLineComment scans a `//` or `#` line comment up to (not including)
// the newline.
func (l *lexer) scanLineComment(emit bool) error {
	start := l.pos
	for l.pos < l.end {
		byte := l.decoded[l.pos]
		if byte == '\n' || byte == '\r' {
			break
		}
		l.pos++
	}
	if emit {
		return l.emitKind(tokLineComment, start, l.pos)
	}
	return nil
}

// scanInlineComment scans a `/* ... */` inline comment, which may span
// lines; an unterminated comment is one error region (RFC 0014 §4.1).
func (l *lexer) scanInlineComment(emit bool) error {
	start := l.pos
	l.pos += 2
	for l.pos+1 < l.end && !(l.decoded[l.pos] == '*' && l.decoded[l.pos+1] == '/') {
		l.pos++
	}
	if l.pos+1 < l.end {
		l.pos += 2
		if emit {
			return l.emitKind(tokInlineComment, start, l.pos)
		}
		return nil
	}
	if emit {
		if err := l.emitErrorRegion(start, l.end, codeUnterminatedComment, protocol.CategorySyntax); err != nil {
			return err
		}
	} else if err := l.recover(codeUnterminatedComment, protocol.CategorySyntax, start, l.end); err != nil {
		return err
	}
	l.pos = l.end
	return nil
}

// scanIdentifier scans one identifier run; the start position is already
// validated.
func (l *lexer) scanIdentifier(emit bool) error {
	start := l.pos
	for {
		ch, ok := l.charAt(l.pos)
		if !ok {
			break
		}
		if isIdentifierContinue(ch) || ch == '-' {
			l.pos += runeWidthOf(ch)
		} else {
			break
		}
	}
	length := l.pos - start
	if length > l.limits.MaxIdentifierLen {
		return lexErrorLimit("identifier-len", length, l.limits.MaxIdentifierLen)
	}
	if emit {
		return l.emitKind(tokIdentifier, start, l.pos)
	}
	return nil
}

// scanNumber scans one number-shaped run and validates the §4.1 decimal
// grammar (RFC 0014 §4.1).
func (l *lexer) scanNumber(emit bool) error {
	start := l.pos
	for {
		byte, ok := l.byte()
		if !ok || byte < '0' || byte > '9' {
			break
		}
		l.pos++
	}
	if byte, ok := l.byte(); ok && byte == '.' {
		if next, ok2 := l.byteAt(1); ok2 && next >= '0' && next <= '9' {
			l.pos += 2
			for {
				digit, ok := l.byte()
				if !ok || digit < '0' || digit > '9' {
					break
				}
				l.pos++
			}
		}
	}
	if byte, ok := l.byte(); ok && (byte == 'e' || byte == 'E') {
		sign := false
		if next, ok2 := l.byteAt(1); ok2 && (next == '+' || next == '-') {
			sign = true
		}
		digitsOffset := 1
		if sign {
			digitsOffset = 2
		}
		if next, ok2 := l.byteAt(digitsOffset); ok2 && next >= '0' && next <= '9' {
			l.pos++
			if sign {
				l.pos++
			}
			for {
				digit, ok := l.byte()
				if !ok || digit < '0' || digit > '9' {
					break
				}
				l.pos++
			}
		}
	}
	// A continuation that cannot start a fresh token makes the whole run
	// one invalid number: hex/octal/binary forms, underscores, a second
	// fraction, or an identifier extension.
	end := l.pos
	for {
		ch, ok := l.charAt(end)
		if !ok {
			break
		}
		if isIdentifierContinue(ch) {
			end += runeWidthOf(ch)
		} else if ch == '.' {
			if next, ok2 := l.charAt(end + 1); ok2 && next >= '0' && next <= '9' {
				end += 2
			} else {
				break
			}
		} else {
			break
		}
	}
	if end > l.pos {
		if emit {
			if err := l.emitErrorRegion(start, end, codeInvalidNumber, protocol.CategorySyntax); err != nil {
				return err
			}
		} else if err := l.recover(codeInvalidNumber, protocol.CategorySyntax, start, end); err != nil {
			return err
		}
		l.pos = end
		return nil
	}
	if emit {
		return l.emitKind(tokNumber, start, l.pos)
	}
	return nil
}

// lexSource runs one deterministic lexer pass over the decoded source.
func lexSource(source *document.SourceSnapshot, authority document.DocumentAuthority,
	limits HclParseLimits, regionStart, regionEnd int, buildIndex bool) (*lexOutput, error) {
	lexer := newLexer(source, authority, limits, regionStart, regionEnd, buildIndex)
	if err := lexer.scan(); err != nil {
		return nil, err
	}
	return lexer.finish()
}

// finish completes one pass: the Eof terminal and the lossless index.
func (l *lexer) finish() (*lexOutput, error) {
	span, err := l.span(l.end, l.end)
	if err != nil {
		return nil, err
	}
	l.tokens = append(l.tokens, hclToken{kind: tokEof, span: span})
	output := &lexOutput{
		source:       l.source,
		authority:    l.authority,
		tokens:       l.tokens,
		errorRegions: l.errorRegions,
		diagnostics:  l.sink.diagnostics,
		recovered:    l.recovered,
	}
	if l.buildIndex {
		var pieces []document.StructuralPiece
		var kinds []HclSyntaxKind
		for _, token := range l.tokens {
			kind, ok := token.kind.syntaxKind()
			if !ok {
				continue
			}
			pieces = append(pieces, document.NewStructuralPiece(token.span, token.kind.structuralKind()))
			kinds = append(kinds, kind)
		}
		index, err := document.NewLosslessStructuralIndex(l.authority.Identity(), l.source.Len(), pieces)
		if err != nil {
			return nil, &hclLexError{code: "hcl.parse.coverage@1",
				category: protocol.CategorySyntax, arguments: map[string]string{}}
		}
		output.index = index
		output.syntaxKinds = kinds
	}
	return output, nil
}

// UAX #31 identifier start: `ID_Start` with underscore excluded (RFC 0014
// §4.1, §12 D-4). The Unicode XID tables are approximated by the Go
// standard-library properties plus the frozen Other_ID_Start characters.
func isIdentifierStart(ch rune) bool {
	if ch == '_' {
		return false
	}
	return isXIDStart(ch)
}

// UAX #31 identifier continuation: `ID_Continue` (underscore included);
// the hyphen continuation is handled by the scan loops.
func isIdentifierContinue(ch rune) bool {
	return isXIDContinue(ch)
}

// isXIDStart approximates the Unicode XID_Start property with the Go
// standard-library properties plus the frozen Other_ID_Start characters
// (U+2118, U+212E, U+309B, U+309C).
func isXIDStart(ch rune) bool {
	if unicode.IsLetter(ch) {
		return true
	}
	switch ch {
	case 0x2118, 0x212E, 0x309B, 0x309C:
		return true
	}
	return false
}

// isXIDContinue approximates the Unicode XID_Continue property: XID_Start
// plus marks, digits, connector punctuation, and the frozen
// Other_ID_Continue characters (U+00B7, U+0387, U+1369-U+1371, U+19DA).
func isXIDContinue(ch rune) bool {
	if isXIDStart(ch) {
		return true
	}
	if unicode.IsMark(ch) || unicode.IsDigit(ch) || unicode.Is(unicode.Pc, ch) {
		return true
	}
	switch {
	case ch == 0x00B7 || ch == 0x0387 || ch == 0x19DA:
		return true
	case ch >= 0x1369 && ch <= 0x1371:
		return true
	}
	return false
}

// runeWidthOf returns the UTF-8 byte width of one scalar.
func runeWidthOf(ch rune) int {
	switch {
	case ch < 0x80:
		return 1
	case ch < 0x800:
		return 2
	case ch < 0x10000:
		return 3
	default:
		return 4
	}
}

// stringsTrimSpace trims Unicode whitespace from both ends (the heredoc
// closing-line rule of RFC 0014 §4.5, §12 D-8); strings.TrimSpace uses the
// same unicode.IsSpace predicate as the Rust char::is_whitespace trim.
func stringsTrimSpace(text string) string {
	return strings.TrimSpace(text)
}

// decodeRune decodes one rune from a string prefix.
func decodeRune(text string) (rune, int) {
	for _, ch := range text {
		return ch, runeWidthOf(ch)
	}
	return 0, 0
}

// parseInt64 parses a decimal string into an int64.
func parseInt64(text string) (int64, error) {
	var value int64
	for i := 0; i < len(text); i++ {
		digit := int64(text[i] - '0')
		if value > (1<<63-1-digit)/10 {
			return 0, errOverflow
		}
		value = value*10 + digit
	}
	return value, nil
}

// errOverflow is the parse overflow sentinel.
var errOverflow = &parseOverflowError{}

type parseOverflowError struct{}

func (e *parseOverflowError) Error() string { return "integer overflow" }

// intString formats one integer without the strconv import in hot paths.
func intString(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	magnitude := value
	if negative {
		magnitude = -magnitude
	}
	var digits [20]byte
	index := len(digits)
	for magnitude > 0 {
		index--
		digits[index] = byte('0' + magnitude%10)
		magnitude /= 10
	}
	if negative {
		index--
		digits[index] = '-'
	}
	return string(digits[index:])
}
