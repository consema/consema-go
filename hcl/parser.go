package hcl

import (
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the deterministic parser pass over the frozen lexer
// token stream: the body/expression grammar with the §3 recovery
// semantics, the native tree assembly, and the merged error regions (RFC
// 0014 §3-§6). The whole formation is side-effect free: nothing is ever
// evaluated (hard gate 1).

// Stable `hcl.parse.*@1` parser diagnostic codes (RFC 0014 §3, §11).
const (
	codeItem               = "hcl.parse.item@1"
	codeAttribute          = "hcl.parse.attribute@1"
	codeBlock              = "hcl.parse.block@1"
	codeLabel              = "hcl.parse.label@1"
	codeExpression         = "hcl.parse.expression@1"
	codeDirective          = "hcl.parse.directive@1"
	codeNewline            = "hcl.parse.newline@1"
	codeSeparator          = "hcl.parse.separator@1"
	codeDuplicateAttribute = "hcl.parse.duplicate-attribute@1"
)

// exprMode is the expression context: whether newline sequences are
// whitespace.
type exprMode uint8

const (
	// exprModeTop is the body-level expression: newlines and line comments
	// end the expression.
	exprModeTop exprMode = iota
	// exprModeNested is an expression inside brackets, parens, calls, or
	// template interiors: newlines are ignored as whitespace (RFC 0014 §2,
	// §4.3).
	exprModeNested
)

// bodyEnd is the terminator of one body parse.
type bodyEnd uint8

const (
	// bodyEndEof is the root body: end of file only.
	bodyEndEof bodyEnd = iota
	// bodyEndBraceClose is a nested body: a closing brace or end of file.
	bodyEndBraceClose
)

// delim is one open bracket of the expression parser.
type delim uint8

const (
	delimBrace delim = iota
	delimBracket
	delimParen
)

func (d delim) matches(kind hclTokenKind) bool {
	switch d {
	case delimBrace:
		return kind == tokBraceClose
	case delimBracket:
		return kind == tokBracketClose
	case delimParen:
		return kind == tokParenClose
	}
	return false
}

// attributeFailure is why one attribute occurrence failed to form.
type attributeFailure uint8

const (
	// attributeFailureMissingEquals: the `=` sign is missing or not on the
	// same line as the name.
	attributeFailureMissingEquals attributeFailure = iota
	// attributeFailureMissingExpression: the expression after `=` is
	// missing or invalid.
	attributeFailureMissingExpression
)

func (f attributeFailure) code() string {
	switch f {
	case attributeFailureMissingEquals:
		return codeAttribute
	case attributeFailureMissingExpression:
		return codeExpression
	}
	return codeExpression
}

// attributeOutcome is the outcome of one attribute parse.
type attributeOutcome struct {
	attribute *HclAttribute
	failed    bool
	failure   attributeFailure
}

// parser is one deterministic pass over the frozen lexer token stream.
type parser struct {
	lexed        *lexOutput
	source       *document.SourceSnapshot
	decoded      string
	authority    document.DocumentAuthority
	limits       HclParseLimits
	tokens       []hclToken
	pos          int
	sink         *diagnosticSink
	recovered    bool
	errorRegions []HclErrorRegion
	// Brackets opened by the expression parser but never closed; taken by
	// the recovery scan when the enclosing item fails (RFC 0014 §3).
	brackets []delim
}

func newParser(lexed *lexOutput, source *document.SourceSnapshot,
	limits HclParseLimits, sinkCap int) *parser {
	return &parser{
		lexed:     lexed,
		source:    source,
		decoded:   decodedTextOf(source),
		authority: lexed.authority,
		limits:    limits,
		tokens:    lexed.tokens,
		sink:      newDiagnosticSink(sinkCap),
	}
}

func (p *parser) parse() (*formedDocument, error) {
	for _, diagnostic := range p.lexed.diagnostics {
		p.sink.push(diagnostic.Code, diagnostic.Category, nil)
	}
	for _, region := range p.lexed.errorRegions {
		p.errorRegions = append(p.errorRegions, region)
	}
	p.recovered = p.recovered || p.lexed.recovered
	if err := p.checkErrorRegionLimits(); err != nil {
		return nil, err
	}
	body, err := p.parseBody(1, bodyEndEof)
	if err != nil {
		return nil, err
	}
	sortErrorRegions(p.errorRegions)
	return &formedDocument{
		source:       p.source,
		authority:    p.authority,
		recovered:    p.recovered,
		diagnostics:  p.sink.diagnostics,
		document:     &HclDocument{source: p.source, body: body},
		errorRegions: p.errorRegions,
		index:        p.lexed.index,
		syntaxKinds:  p.lexed.syntaxKinds,
		limits:       p.limits,
	}, nil
}

func sortErrorRegions(regions []HclErrorRegion) {
	for i := 1; i < len(regions); i++ {
		for j := i; j > 0 && regions[j-1].span.StartByte() > regions[j].span.StartByte(); j-- {
			regions[j-1], regions[j] = regions[j], regions[j-1]
		}
	}
}

// -- token cursor ----------------------------------------------------------

func (p *parser) peek() hclToken {
	index := p.pos
	if index >= len(p.tokens) {
		index = len(p.tokens) - 1
	}
	return p.tokens[index]
}

func (p *parser) peekKind() hclTokenKind { return p.peek().kind }

func (p *parser) advance() hclToken {
	token := p.peek()
	if token.kind != tokEof {
		p.pos++
	}
	return token
}

func (p *parser) at(kind hclTokenKind) bool { return p.peekKind() == kind }

func (p *parser) eat(kind hclTokenKind) (hclToken, bool) {
	if p.at(kind) {
		return p.advance(), true
	}
	return hclToken{}, false
}

// skipTrivia skips whitespace and inline comments (inline comments may
// span lines but count as whitespace; RFC 0014 §4.1).
func (p *parser) skipTrivia() {
	for {
		switch p.peekKind() {
		case tokWhitespace, tokInlineComment:
			p.pos++
		default:
			return
		}
	}
}

// skipStructural skips all trivia, including newlines and line comments.
func (p *parser) skipStructural() {
	for {
		switch p.peekKind() {
		case tokWhitespace, tokInlineComment, tokLineBreak, tokLineComment:
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) skipExpressionTrivia(mode exprMode) {
	if mode == exprModeTop {
		p.skipTrivia()
	} else {
		p.skipStructural()
	}
}

// text derives the exact token text from the frozen decoded text.
func (p *parser) text(token hclToken) string {
	return p.decoded[token.span.StartByte():token.span.EndByte()]
}

func (p *parser) span(start, end int) (document.Span, error) {
	if start > end || end > p.source.Len() {
		return document.Span{}, &hclLexError{code: "hcl.parse.coordinates@1",
			category: protocol.CategorySyntax, arguments: map[string]string{}}
	}
	return p.authority.Span(start, end)
}

// -- diagnostics and recovery ----------------------------------------------

// diagnose records one recovery diagnostic and marks the parse Recovered.
func (p *parser) diagnose(code string, span document.Span, category protocol.DiagnosticCategory) {
	p.recovered = true
	p.sink.push(code, category, &span)
}

// emitErrorRegion emits one error region with its diagnostic; a
// zero-length region publishes the diagnostic only, never an empty piece.
func (p *parser) emitErrorRegion(start, end int, code string,
	category protocol.DiagnosticCategory) error {
	p.recovered = true
	span, err := p.span(start, end)
	if err != nil {
		return err
	}
	p.sink.push(code, category, &span)
	if end > start {
		p.errorRegions = append(p.errorRegions, HclErrorRegion{span: span, code: code})
		return p.checkErrorRegionLimits()
	}
	return nil
}

func (p *parser) checkErrorRegionLimits() error {
	if len(p.errorRegions) > p.limits.MaxRecoveryRegions {
		return lexErrorLimit("recovery-regions", len(p.errorRegions), p.limits.MaxRecoveryRegions)
	}
	if len(p.errorRegions) > p.limits.MaxErrorRegions {
		return lexErrorLimit("error-regions", len(p.errorRegions), p.limits.MaxErrorRegions)
	}
	return nil
}

// failItem fails one body item: emits the error region from the item start
// to the deterministic recovery boundary and advances past the region.
//
// The boundary follows RFC 0014 §3: the end of the line when no bracket
// opened by the failed expression is still open; the matching close of the
// innermost open bracket when one exists; end of file otherwise. A closing
// delimiter that would close an enclosing body construct stops the region
// before it and is never consumed.
func (p *parser) failItem(start int, code string) error {
	brackets := p.brackets
	p.brackets = nil
	boundary, err := p.scanRecovery(brackets)
	if err != nil {
		return err
	}
	return p.emitErrorRegion(start, boundary, code, protocol.CategorySyntax)
}

// scanRecovery scans forward from the current token to the recovery
// boundary and advances pos to the boundary token (the region end is
// returned).
//
// Whitespace and comments are consumed; a newline or line comment stops
// the scan when no bracket is open; end of file always stops it. An open
// bracket pushes; a close bracket that matches the innermost open bracket
// pops it and ends the region after the close when the stack empties; a
// close bracket with an empty stack ends the region before it; a
// mismatched close discards the innermost open bracket and the scan
// continues.
func (p *parser) scanRecovery(stack []delim) (int, error) {
	for {
		token := p.peek()
		switch token.kind {
		case tokEof:
			return p.source.Len(), nil
		case tokLineBreak, tokLineComment:
			if len(stack) == 0 {
				return token.span.StartByte(), nil
			}
			p.pos++
		case tokBraceOpen, tokBracketOpen, tokParenOpen:
			stack = append(stack, delimOf(token.kind))
			p.pos++
		case tokBraceClose, tokBracketClose, tokParenClose:
			if len(stack) == 0 {
				return token.span.StartByte(), nil
			}
			top := stack[len(stack)-1]
			if top.matches(token.kind) {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					p.pos++
					return token.span.EndByte(), nil
				}
			} else {
				stack = stack[:len(stack)-1]
			}
			p.pos++
		default:
			p.pos++
		}
	}
}

// scanToCloseBrace consumes tokens through the next `}` at brace depth
// zero and returns its end byte; -1 at end of file. Used to close a
// one-line block whose content is invalid.
func (p *parser) scanToCloseBrace() (int, bool) {
	braces := 0
	for {
		token := p.peek()
		switch token.kind {
		case tokEof:
			return 0, false
		case tokBraceOpen:
			braces++
			p.pos++
		case tokBraceClose:
			if braces == 0 {
				p.pos++
				return token.span.EndByte(), true
			}
			braces--
			p.pos++
		default:
			p.pos++
		}
	}
}

// -- body grammar ----------------------------------------------------------

func (p *parser) parseBody(depth int, end bodyEnd) (*HclBody, error) {
	if depth > p.limits.MaxBodyDepth {
		return nil, lexErrorLimit("body-depth", depth, p.limits.MaxBodyDepth)
	}
	var items []HclBodyItem
	attributeCount := 0
	blockCount := 0
	itemCount := 0
	names := make(map[string]bool)
	for {
		p.skipStructural()
		token := p.peek()
		switch token.kind {
		case tokEof:
			return &HclBody{items: items}, nil
		case tokBraceClose:
			if end == bodyEndBraceClose {
				// The caller consumes the closing brace.
				return &HclBody{items: items}, nil
			}
			// An orphan closing delimiter at this body level: it closes no
			// open construct, so it is consumed with a diagnostic instead
			// of starting an item.
			p.diagnose(codeItem, token.span, protocol.CategorySyntax)
			p.advance()
		case tokIdentifier:
			token := p.advance()
			name := p.text(token)
			p.skipTrivia()
			switch p.peekKind() {
			case tokEquals:
				itemCount++
				attributeCount++
				if itemCount > p.limits.MaxBodyItemCount {
					return nil, lexErrorLimit("body-item-count", itemCount, p.limits.MaxBodyItemCount)
				}
				if attributeCount > p.limits.MaxAttributeCount {
					return nil, lexErrorLimit("attribute-count", attributeCount, p.limits.MaxAttributeCount)
				}
				outcome, err := p.parseAttribute(token, name, false)
				if err != nil {
					return nil, err
				}
				if outcome.failed {
					if err := p.failItem(token.span.StartByte(), outcome.failure.code()); err != nil {
						return nil, err
					}
					continue
				}
				if !names[name] {
					names[name] = true
					items = append(items, HclBodyItem{attribute: outcome.attribute})
				} else {
					// The duplicate stays a proven syntax piece but never a
					// native attribute (RFC 0014 §3).
					p.diagnose(codeDuplicateAttribute, token.span, protocol.CategorySyntax)
				}
			case tokStringOpen, tokBraceOpen, tokIdentifier:
				itemCount++
				blockCount++
				if itemCount > p.limits.MaxBodyItemCount {
					return nil, lexErrorLimit("body-item-count", itemCount, p.limits.MaxBodyItemCount)
				}
				if blockCount > p.limits.MaxBlockCount {
					return nil, lexErrorLimit("block-count", blockCount, p.limits.MaxBlockCount)
				}
				block, err := p.parseBlock(token, depth)
				if err != nil {
					return nil, err
				}
				if block != nil {
					items = append(items, HclBodyItem{block: block})
				}
			default:
				if err := p.failItem(token.span.StartByte(), codeItem); err != nil {
					return nil, err
				}
			}
		default:
			if token.kind == tokBraceClose || token.kind == tokBracketClose || token.kind == tokParenClose {
				// An orphan closing delimiter at this body level: it closes
				// no open construct, so it is consumed with a diagnostic
				// instead of starting an item.
				p.diagnose(codeItem, token.span, protocol.CategorySyntax)
				p.advance()
			} else {
				if err := p.failItem(token.span.StartByte(), codeItem); err != nil {
					return nil, err
				}
			}
		}
	}
}

func (p *parser) parseAttribute(nameToken hclToken, name string,
	singleLine bool) (attributeOutcome, error) {
	p.skipTrivia()
	equals, ok := p.eat(tokEquals)
	if !ok {
		return attributeOutcome{failed: true, failure: attributeFailureMissingEquals}, nil
	}
	p.skipTrivia()
	expression, err := p.parseExpression(exprModeTop, 0)
	if err != nil {
		return attributeOutcome{}, err
	}
	if expression == nil {
		return attributeOutcome{failed: true, failure: attributeFailureMissingExpression}, nil
	}
	if !singleLine {
		p.skipTrivia()
		switch p.peekKind() {
		case tokLineBreak, tokLineComment, tokEof:
		default:
			// The attribute is proven; only its terminator is missing (RFC
			// 0014 §2, §12 D-9).
			p.diagnose(codeNewline, p.peek().span, protocol.CategorySyntax)
			if _, err := p.scanRecovery(nil); err != nil {
				return attributeOutcome{}, err
			}
		}
	}
	return attributeOutcome{attribute: &HclAttribute{
		name:       name,
		nameSpan:   nameToken.span,
		equalsSpan: equals.span,
		expression: expression,
	}}, nil
}

func (p *parser) parseBlock(typeToken hclToken, depth int) (*HclBlock, error) {
	blockStart := typeToken.span.StartByte()
	blockType := p.text(typeToken)
	var labels []HclBlockLabel
	for {
		p.skipTrivia()
		switch p.peekKind() {
		case tokIdentifier:
			token := p.advance()
			labels = append(labels, HclBlockLabel{text: p.text(token), span: token.span})
			if len(labels) > p.limits.MaxLabelCount {
				return nil, lexErrorLimit("label-count", len(labels), p.limits.MaxLabelCount)
			}
		case tokStringOpen:
			label, err := p.parseQuotedLabel()
			if err != nil {
				return nil, err
			}
			if label == nil {
				if err := p.failItem(blockStart, codeLabel); err != nil {
					return nil, err
				}
				return nil, nil
			}
			labels = append(labels, *label)
			if len(labels) > p.limits.MaxLabelCount {
				return nil, lexErrorLimit("label-count", len(labels), p.limits.MaxLabelCount)
			}
		case tokBraceOpen:
			goto opened
		default:
			if err := p.failItem(blockStart, codeBlock); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}
opened:
	p.advance() // open brace
	p.skipTrivia()
	var body *HclBody
	var closeEnd int
	switch p.peekKind() {
	case tokLineBreak, tokLineComment:
		p.skipStructural()
		nested, err := p.parseBody(depth+1, bodyEndBraceClose)
		if err != nil {
			return nil, err
		}
		if p.at(tokBraceClose) {
			close := p.advance()
			body, closeEnd = nested, close.span.EndByte()
		} else {
			if err := p.failItem(blockStart, codeBlock); err != nil {
				return nil, err
			}
			return nil, nil
		}
	case tokBraceClose:
		close := p.advance()
		body, closeEnd = &HclBody{}, close.span.EndByte()
	case tokEof:
		if err := p.failItem(blockStart, codeBlock); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		formed, ok, err := p.parseOneLineBody(blockStart)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		body, closeEnd = formed.body, formed.closeEnd
	}
	p.skipTrivia()
	switch p.peekKind() {
	case tokLineBreak, tokLineComment, tokEof:
	default:
		p.diagnose(codeNewline, p.peek().span, protocol.CategorySyntax)
		if _, err := p.scanRecovery(nil); err != nil {
			return nil, err
		}
	}
	span, err := p.span(blockStart, closeEnd)
	if err != nil {
		return nil, err
	}
	body.startByte = blockStart
	return &HclBlock{blockType: blockType, labels: labels, body: body, span: span}, nil
}

// parseQuotedLabel parses one quoted block label: a quoted literal string
// without any interpolation or directive sequence (RFC 0014 §4.2). nil
// when the template is unterminated (already recovered at the lexer) or
// contains a template sequence.
func (p *parser) parseQuotedLabel() (*HclBlockLabel, error) {
	open := p.advance()
	var text strings.Builder
	for {
		token := p.peek()
		switch token.kind {
		case tokStringContent:
			p.advance()
			text.WriteString(decodeQuotedLiteral(p.text(token)))
		case tokStringClose:
			close := p.advance()
			span, err := p.span(open.span.StartByte(), close.span.EndByte())
			if err != nil {
				return nil, err
			}
			return &HclBlockLabel{text: text.String(), span: span, quoted: true}, nil
		case tokErrorRegion, tokEof:
			// Unterminated at the lexer; the lexer already published its
			// diagnostic.
			return nil, nil
		default:
			p.diagnose(codeLabel, token.span, protocol.CategorySyntax)
			return nil, nil
		}
	}
}

// oneLineBody is the outcome of one one-line block body parse.
type oneLineBody struct {
	body     *HclBody
	closeEnd int
}

// parseOneLineBody parses a one-line block body: at most one attribute,
// closed by `}` on the same line (RFC 0014 §4.2).
//
// The second result is false when the block is never closed; the failure
// region is emitted by the caller through the block-start path only for
// the unclosed forms. A failing single attribute dies with its own region
// and the block still closes.
func (p *parser) parseOneLineBody(blockStart int) (oneLineBody, bool, error) {
	switch p.peekKind() {
	case tokBraceClose:
		close := p.advance()
		return oneLineBody{body: &HclBody{}, closeEnd: close.span.EndByte()}, true, nil
	case tokEof:
		if err := p.failItem(blockStart, codeBlock); err != nil {
			return oneLineBody{}, false, err
		}
		return oneLineBody{}, false, nil
	case tokIdentifier:
		nameToken := p.advance()
		name := p.text(nameToken)
		outcome, err := p.parseAttribute(nameToken, name, true)
		if err != nil {
			return oneLineBody{}, false, err
		}
		if outcome.failed {
			if err := p.failItem(nameToken.span.StartByte(), outcome.failure.code()); err != nil {
				return oneLineBody{}, false, err
			}
			closeEnd, ok := p.scanToCloseBrace()
			if !ok {
				if err := p.failItem(blockStart, codeBlock); err != nil {
					return oneLineBody{}, false, err
				}
				return oneLineBody{}, false, nil
			}
			return oneLineBody{body: &HclBody{}, closeEnd: closeEnd}, true, nil
		}
		p.skipTrivia()
		switch p.peekKind() {
		case tokBraceClose:
			close := p.advance()
			return oneLineBody{body: &HclBody{items: []HclBodyItem{{attribute: outcome.attribute}}},
				closeEnd: close.span.EndByte()}, true, nil
		case tokEof:
			if err := p.failItem(blockStart, codeBlock); err != nil {
				return oneLineBody{}, false, err
			}
			return oneLineBody{}, false, nil
		default:
			p.diagnose(codeBlock, p.peek().span, protocol.CategorySyntax)
			closeEnd, ok := p.scanToCloseBrace()
			if !ok {
				if err := p.failItem(blockStart, codeBlock); err != nil {
					return oneLineBody{}, false, err
				}
				return oneLineBody{}, false, nil
			}
			return oneLineBody{body: &HclBody{items: []HclBodyItem{{attribute: outcome.attribute}}},
				closeEnd: closeEnd}, true, nil
		}
	default:
		p.diagnose(codeBlock, p.peek().span, protocol.CategorySyntax)
		closeEnd, ok := p.scanToCloseBrace()
		if !ok {
			if err := p.failItem(blockStart, codeBlock); err != nil {
				return oneLineBody{}, false, err
			}
			return oneLineBody{}, false, nil
		}
		return oneLineBody{body: &HclBody{}, closeEnd: closeEnd}, true, nil
	}
}

// -- expression grammar ----------------------------------------------------

func (p *parser) parseExpression(mode exprMode, depth int) (*HclExpression, error) {
	if depth >= p.limits.MaxExpressionDepth {
		return nil, lexErrorLimit("expression-depth", depth+1, p.limits.MaxExpressionDepth)
	}
	return p.parseConditional(mode, depth)
}

func (p *parser) parseConditional(mode exprMode, depth int) (*HclExpression, error) {
	condition, err := p.parseOr(mode, depth)
	if err != nil {
		return nil, err
	}
	if condition == nil {
		return nil, nil
	}
	p.skipTrivia()
	if !p.at(tokQuestionMark) {
		return condition, nil
	}
	p.advance()
	thenExpr, err := p.parseConditional(mode, depth+1)
	if err != nil {
		return nil, err
	}
	if thenExpr == nil {
		return nil, nil
	}
	p.skipTrivia()
	if _, ok := p.eat(tokColon); !ok {
		p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
		return nil, nil
	}
	elseExpr, err := p.parseConditional(mode, depth+1)
	if err != nil {
		return nil, err
	}
	if elseExpr == nil {
		return nil, nil
	}
	span, err := p.span(condition.span.StartByte(), elseExpr.span.EndByte())
	if err != nil {
		return nil, err
	}
	return &HclExpression{kind: NewConditionalKind(condition, thenExpr, elseExpr), span: span}, nil
}

// parseBinaryLevel parses one left-associative binary level; chain is the
// number of operators already folded at this level, bounded by the
// expression depth so a left-deep chain can never overflow the
// structural-equality recursion.
func (p *parser) parseBinaryLevel(mode exprMode, depth int, level int,
	operands func(int, int) (*HclExpression, error),
	operator func(hclTokenKind) (BinaryOp, bool)) (*HclExpression, error) {
	lhs, err := operands(depth, level)
	if err != nil {
		return nil, err
	}
	if lhs == nil {
		return nil, nil
	}
	chain := 0
	for {
		p.skipTrivia()
		op, ok := operator(p.peekKind())
		if !ok {
			break
		}
		chain++
		if chain > p.limits.MaxExpressionDepth {
			return nil, lexErrorLimit("expression-depth", chain, p.limits.MaxExpressionDepth)
		}
		p.advance()
		p.skipExpressionTrivia(mode)
		rhs, err := operands(depth, level+1)
		if err != nil {
			return nil, err
		}
		if rhs == nil {
			return nil, nil
		}
		span, err := p.span(lhs.span.StartByte(), rhs.span.EndByte())
		if err != nil {
			return nil, err
		}
		lhs = &HclExpression{kind: NewBinaryKind(op, lhs, rhs), span: span}
	}
	return lhs, nil
}

func (p *parser) parseOr(mode exprMode, depth int) (*HclExpression, error) {
	return p.parseBinaryLevel(mode, depth, 0,
		func(d, _ int) (*HclExpression, error) { return p.parseAnd(mode, d) },
		func(kind hclTokenKind) (BinaryOp, bool) {
			if kind == tokOpOr {
				return BinaryOpOr, true
			}
			return 0, false
		})
}

func (p *parser) parseAnd(mode exprMode, depth int) (*HclExpression, error) {
	return p.parseBinaryLevel(mode, depth, 0,
		func(d, _ int) (*HclExpression, error) { return p.parseEquality(mode, d) },
		func(kind hclTokenKind) (BinaryOp, bool) {
			if kind == tokOpAnd {
				return BinaryOpAnd, true
			}
			return 0, false
		})
}

func (p *parser) parseEquality(mode exprMode, depth int) (*HclExpression, error) {
	return p.parseBinaryLevel(mode, depth, 0,
		func(d, _ int) (*HclExpression, error) { return p.parseRelational(mode, d) },
		func(kind hclTokenKind) (BinaryOp, bool) {
			switch kind {
			case tokOpEqual:
				return BinaryOpEqual, true
			case tokOpNotEqual:
				return BinaryOpNotEqual, true
			}
			return 0, false
		})
}

func (p *parser) parseRelational(mode exprMode, depth int) (*HclExpression, error) {
	return p.parseBinaryLevel(mode, depth, 0,
		func(d, _ int) (*HclExpression, error) { return p.parseAdditive(mode, d) },
		func(kind hclTokenKind) (BinaryOp, bool) {
			switch kind {
			case tokOpLess:
				return BinaryOpLess, true
			case tokOpGreater:
				return BinaryOpGreater, true
			case tokOpLessEqual:
				return BinaryOpLessEqual, true
			case tokOpGreaterEqual:
				return BinaryOpGreaterEqual, true
			}
			return 0, false
		})
}

func (p *parser) parseAdditive(mode exprMode, depth int) (*HclExpression, error) {
	return p.parseBinaryLevel(mode, depth, 0,
		func(d, _ int) (*HclExpression, error) { return p.parseMultiplicative(mode, d) },
		func(kind hclTokenKind) (BinaryOp, bool) {
			switch kind {
			case tokOpAdd:
				return BinaryOpAdd, true
			case tokOpSubtract:
				return BinaryOpSubtract, true
			}
			return 0, false
		})
}

func (p *parser) parseMultiplicative(mode exprMode, depth int) (*HclExpression, error) {
	return p.parseBinaryLevel(mode, depth, 0,
		func(d, _ int) (*HclExpression, error) { return p.parseTerm(mode, d) },
		func(kind hclTokenKind) (BinaryOp, bool) {
			switch kind {
			case tokStar:
				return BinaryOpMultiply, true
			case tokOpDivide:
				return BinaryOpDivide, true
			case tokOpModulo:
				return BinaryOpModulo, true
			}
			return 0, false
		})
}

// parseTerm parses the term layer: unary chains over the base term and its
// postfix traversal steps (RFC 0014 §4.3).
func (p *parser) parseTerm(mode exprMode, depth int) (*HclExpression, error) {
	if depth >= p.limits.MaxExpressionDepth {
		return nil, lexErrorLimit("expression-depth", depth+1, p.limits.MaxExpressionDepth)
	}
	p.skipExpressionTrivia(mode)
	token := p.peek()
	switch token.kind {
	case tokOpSubtract, tokOpNot:
		opToken := p.advance()
		op := UnaryOpMinus
		if opToken.kind == tokOpNot {
			op = UnaryOpNot
		}
		operand, err := p.parseTerm(mode, depth+1)
		if err != nil {
			return nil, err
		}
		if operand == nil {
			return nil, nil
		}
		span, err := p.span(opToken.span.StartByte(), operand.span.EndByte())
		if err != nil {
			return nil, err
		}
		return &HclExpression{kind: NewUnaryKind(op, operand), span: span}, nil
	case tokNumber:
		token := p.advance()
		number, err := p.number(token)
		if err != nil {
			return nil, err
		}
		return &HclExpression{kind: NewNumberKind(number), span: token.span}, nil
	case tokStringOpen:
		parts, span, err := p.parseQuotedTemplate(depth)
		if err != nil {
			return nil, err
		}
		if parts == nil {
			return nil, nil
		}
		return &HclExpression{kind: NewTemplateKind(parts, nil), span: span}, nil
	case tokHeredocOpen:
		return p.parseHeredoc(depth)
	case tokParenOpen:
		return p.parseParen(depth)
	case tokBracketOpen:
		return p.parseBracket(depth)
	case tokBraceOpen:
		return p.parseBrace(depth)
	case tokIdentifier:
		return p.parseIdentifierTerm(mode, depth)
	default:
		p.diagnose(codeExpression, token.span, protocol.CategorySyntax)
		return nil, nil
	}
}

// number builds one HclNumber from a lexer-valid spelling; a spelling
// whose exponent does not fit the bounded canonical-decimal representation
// is a fatal `hcl.limit.number-digits@1` failure (RFC 0014 §11).
func (p *parser) number(token hclToken) (HclNumber, error) {
	spelling := p.text(token)
	canonical, ok := canonicalDecimalBounded(spelling, p.limits.MaxNumberDigits)
	if !ok {
		return HclNumber{}, lexErrorLimit("number-digits", int(^uint(0)>>1), p.limits.MaxNumberDigits)
	}
	digits := 0
	for i := 0; i < len(canonical); i++ {
		if canonical[i] >= '0' && canonical[i] <= '9' {
			digits++
		}
	}
	if digits > p.limits.MaxNumberDigits {
		return HclNumber{}, lexErrorLimit("number-digits", digits, p.limits.MaxNumberDigits)
	}
	return HclNumber{span: token.span, canonical: canonical}, nil
}

// parseIdentifierTerm parses one identifier term: a keyword or variable
// base with postfix traversal steps, or a function call (RFC 0014 §4.3).
func (p *parser) parseIdentifierTerm(mode exprMode, depth int) (*HclExpression, error) {
	nameToken := p.peek()
	name := p.text(nameToken)
	p.advance()
	p.skipExpressionTrivia(mode)
	if p.at(tokParenOpen) {
		return p.parseCall(nameToken, depth)
	}
	var base HclExpressionKind
	switch name {
	case "true":
		base = NewBooleanKind(true)
	case "false":
		base = NewBooleanKind(false)
	case "null":
		base = NewNullKind()
	default:
		base = NewVariableRefKind(name)
	}
	var steps []HclTraversalStep
	end := nameToken.span.EndByte()
	for {
		p.skipExpressionTrivia(mode)
		switch p.peekKind() {
		case tokDot:
			dot := p.advance()
			p.skipExpressionTrivia(mode)
			switch p.peekKind() {
			case tokIdentifier:
				ident := p.advance()
				span, err := p.span(dot.span.StartByte(), ident.span.EndByte())
				if err != nil {
					return nil, err
				}
				steps = append(steps, NewGetAttrStep(p.text(ident), span))
				end = ident.span.EndByte()
			case tokStar:
				// Attribute splat `. * GetAttr*`.
				star := p.advance()
				end = star.span.EndByte()
				var nested []HclTraversalStep
				for {
					p.skipExpressionTrivia(mode)
					if !p.at(tokDot) {
						break
					}
					ndot := p.advance()
					p.skipExpressionTrivia(mode)
					if !p.at(tokIdentifier) {
						p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
						return nil, nil
					}
					nident := p.advance()
					span, err := p.span(ndot.span.StartByte(), nident.span.EndByte())
					if err != nil {
						return nil, err
					}
					nested = append(nested, NewGetAttrStep(p.text(nident), span))
					end = nident.span.EndByte()
				}
				steps = append(steps, NewAttrSplatStep(nested))
			default:
				// D-5: `foo.0` is rejected — `GetAttr = "." Identifier`
				// admits identifiers only (RFC 0014 §12).
				p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
				return nil, nil
			}
		case tokBracketOpen:
			p.brackets = append(p.brackets, delimBracket)
			open := p.advance()
			p.skipStructural()
			if p.at(tokStar) {
				// Full splat `[ * ] (GetAttr | Index)*`.
				p.advance()
				p.skipStructural()
				if !p.at(tokBracketClose) {
					p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
					return nil, nil
				}
				close := p.advance()
				end = close.span.EndByte()
				var nested []HclTraversalStep
				for {
					p.skipExpressionTrivia(mode)
					if p.at(tokDot) {
						dot := p.advance()
						p.skipExpressionTrivia(mode)
						if !p.at(tokIdentifier) {
							p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
							return nil, nil
						}
						ident := p.advance()
						span, err := p.span(dot.span.StartByte(), ident.span.EndByte())
						if err != nil {
							return nil, err
						}
						nested = append(nested, NewGetAttrStep(p.text(ident), span))
						end = ident.span.EndByte()
					} else if p.at(tokBracketOpen) {
						indexOpen := p.advance()
						p.brackets = append(p.brackets, delimBracket)
						p.skipStructural()
						key, err := p.parseExpression(exprModeNested, depth+1)
						if err != nil {
							return nil, err
						}
						if key == nil {
							return nil, nil
						}
						p.skipStructural()
						if !p.at(tokBracketClose) {
							p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
							return nil, nil
						}
						indexClose := p.advance()
						p.brackets = p.brackets[:len(p.brackets)-1]
						span, err := p.span(indexOpen.span.StartByte(), indexClose.span.EndByte())
						if err != nil {
							return nil, err
						}
						nested = append(nested, NewIndexStep(key, span))
						end = indexClose.span.EndByte()
					} else {
						break
					}
				}
				steps = append(steps, NewFullSplatStep(nested))
				p.brackets = p.brackets[:len(p.brackets)-1]
			} else {
				// Index step `[ Expression ]`.
				key, err := p.parseExpression(exprModeNested, depth+1)
				if err != nil {
					return nil, err
				}
				if key == nil {
					return nil, nil
				}
				p.skipStructural()
				if !p.at(tokBracketClose) {
					p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
					return nil, nil
				}
				close := p.advance()
				p.brackets = p.brackets[:len(p.brackets)-1]
				span, err := p.span(open.span.StartByte(), close.span.EndByte())
				if err != nil {
					return nil, err
				}
				steps = append(steps, NewIndexStep(key, span))
				end = close.span.EndByte()
			}
		default:
			goto done
		}
	}
done:
	if len(steps) == 0 {
		return &HclExpression{kind: base, span: nameToken.span}, nil
	}
	var root HclTraversalRoot
	switch name {
	case "true":
		root = NewTraversalRootBoolean(true)
	case "false":
		root = NewTraversalRootBoolean(false)
	case "null":
		root = NewTraversalRootNull()
	default:
		root = NewTraversalRootVariable(name)
	}
	span, err := p.span(nameToken.span.StartByte(), end)
	if err != nil {
		return nil, err
	}
	return &HclExpression{kind: NewTraversalKind(root, steps), span: span}, nil
}

// parseCall parses one function call (RFC 0014 §4.3).
func (p *parser) parseCall(nameToken hclToken, depth int) (*HclExpression, error) {
	p.brackets = append(p.brackets, delimParen)
	p.advance() // open paren
	var args []HclCallArg
	var close hclToken
	for {
		p.skipStructural()
		if p.at(tokParenClose) {
			close = p.advance()
			break
		}
		expression, err := p.parseExpression(exprModeNested, depth+1)
		if err != nil {
			return nil, err
		}
		if expression == nil {
			return nil, nil
		}
		expand := false
		p.skipStructural()
		if p.at(tokEllipsis) {
			// The expansion marker may only appear on the final argument (a
			// parser contract).
			p.advance()
			expand = true
			p.skipStructural()
			if p.at(tokComma) {
				p.advance()
				p.skipStructural()
			}
			if !p.at(tokParenClose) {
				p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
				return nil, nil
			}
		}
		args = append(args, NewHclCallArg(expression, expand))
		if p.at(tokParenClose) {
			close = p.advance()
			break
		}
		switch p.peekKind() {
		case tokComma, tokLineBreak, tokLineComment:
			p.advance()
			continue
		default:
			p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
			return nil, nil
		}
	}
	p.brackets = p.brackets[:len(p.brackets)-1]
	span, err := p.span(nameToken.span.StartByte(), close.span.EndByte())
	if err != nil {
		return nil, err
	}
	return &HclExpression{kind: NewFunctionCallKind(p.text(nameToken), nameToken.span, args), span: span}, nil
}

func (p *parser) parseParen(depth int) (*HclExpression, error) {
	p.brackets = append(p.brackets, delimParen)
	open := p.advance()
	p.skipStructural()
	inner, err := p.parseExpression(exprModeNested, depth+1)
	if err != nil {
		return nil, err
	}
	if inner == nil {
		return nil, nil
	}
	p.skipStructural()
	if !p.at(tokParenClose) {
		p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
		return nil, nil
	}
	close := p.advance()
	p.brackets = p.brackets[:len(p.brackets)-1]
	span, err := p.span(open.span.StartByte(), close.span.EndByte())
	if err != nil {
		return nil, err
	}
	return &HclExpression{kind: NewParenKind(inner), span: span}, nil
}

func (p *parser) parseBracket(depth int) (*HclExpression, error) {
	p.brackets = append(p.brackets, delimBracket)
	open := p.advance()
	p.skipStructural()
	if p.at(tokIdentifier) && p.text(p.peek()) == "for" {
		// The for-expression interpretation has priority over a first
		// element literally spelled `for` (RFC 0014 §4.6).
		return p.parseForTuple(open, depth)
	}
	var elements []*HclExpression
	var close hclToken
	for {
		p.skipStructural()
		if p.at(tokBracketClose) {
			close = p.advance()
			break
		}
		element, err := p.parseExpression(exprModeNested, depth+1)
		if err != nil {
			return nil, err
		}
		if element == nil {
			return nil, nil
		}
		if len(elements) >= p.limits.MaxTupleElements {
			return nil, lexErrorLimit("tuple-elements", len(elements)+1, p.limits.MaxTupleElements)
		}
		elements = append(elements, element)
		p.skipTrivia()
		switch p.peekKind() {
		case tokComma, tokLineBreak, tokLineComment:
			p.advance()
		case tokBracketClose:
		default:
			p.diagnose(codeSeparator, p.peek().span, protocol.CategorySyntax)
		}
	}
	p.brackets = p.brackets[:len(p.brackets)-1]
	span, err := p.span(open.span.StartByte(), close.span.EndByte())
	if err != nil {
		return nil, err
	}
	return &HclExpression{kind: NewTupleKind(elements), span: span}, nil
}

func (p *parser) parseBrace(depth int) (*HclExpression, error) {
	p.brackets = append(p.brackets, delimBrace)
	open := p.advance()
	p.skipStructural()
	if p.at(tokIdentifier) && p.text(p.peek()) == "for" {
		// The for-expression interpretation has priority over a first key
		// literally spelled `for` (RFC 0014 §4.6).
		return p.parseForObject(open, depth)
	}
	var entries []HclObjectEntry
	var close hclToken
	for {
		p.skipStructural()
		if p.at(tokBraceClose) {
			close = p.advance()
			break
		}
		var key HclObjectKey
		switch p.peekKind() {
		case tokIdentifier:
			token := p.advance()
			key = NewIdentifierObjectKey(p.text(token))
		case tokNumber:
			token := p.advance()
			number, err := p.number(token)
			if err != nil {
				return nil, err
			}
			key = NewNumberObjectKey(number)
		case tokStringOpen:
			parts, span, err := p.parseQuotedTemplate(depth)
			if err != nil {
				return nil, err
			}
			if parts == nil {
				return nil, nil
			}
			key = NewTemplateObjectKey(NewHclTemplateKey(parts, span))
		case tokParenOpen:
			inner, err := p.parseParen(depth)
			if err != nil {
				return nil, err
			}
			if inner == nil {
				return nil, nil
			}
			key = NewParenObjectKey(inner)
		default:
			p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
			return nil, nil
		}
		p.skipStructural()
		var separator ObjectSeparator
		switch p.peekKind() {
		case tokEquals:
			p.advance()
			separator = ObjectSeparatorEquals
		case tokColon:
			p.advance()
			separator = ObjectSeparatorColon
		default:
			p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
			return nil, nil
		}
		p.skipStructural()
		value, err := p.parseExpression(exprModeNested, depth+1)
		if err != nil {
			return nil, err
		}
		if value == nil {
			return nil, nil
		}
		if len(entries) >= p.limits.MaxObjectEntries {
			return nil, lexErrorLimit("object-entries", len(entries)+1, p.limits.MaxObjectEntries)
		}
		entries = append(entries, NewHclObjectEntry(key, separator, value))
		p.skipTrivia()
		switch p.peekKind() {
		case tokComma, tokLineBreak, tokLineComment:
			p.advance()
		case tokBraceClose:
		default:
			p.diagnose(codeSeparator, p.peek().span, protocol.CategorySyntax)
		}
	}
	p.brackets = p.brackets[:len(p.brackets)-1]
	span, err := p.span(open.span.StartByte(), close.span.EndByte())
	if err != nil {
		return nil, err
	}
	return &HclExpression{kind: NewObjectKind(entries), span: span}, nil
}

// parseForIntro parses the shared `for` introduction (RFC 0014 §4.6).
//
// The key identifier is read only when a comma follows (RFC 0014 §12 D-7),
// so `for v in x` and `for k, v in x` are both admitted. With
// expectColon, the introduction ends at the required `:` of a
// for-expression; without it, the introduction ends at the collection
// expression (a template directive).
func (p *parser) parseForIntro(forStart int, depth int, expectColon bool) (*HclForIntro, error) {
	p.skipStructural()
	var firstToken hclToken
	if p.at(tokIdentifier) {
		firstToken = p.advance()
	} else {
		p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
		return nil, nil
	}
	var key *string
	var value string
	p.skipStructural()
	if p.at(tokComma) {
		p.advance()
		p.skipStructural()
		var valueToken hclToken
		if p.at(tokIdentifier) {
			valueToken = p.advance()
		} else {
			p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
			return nil, nil
		}
		// `for k, v in ...`: the first identifier is the key.
		keyText := p.text(firstToken)
		key = &keyText
		value = p.text(valueToken)
		p.skipStructural()
	} else {
		value = p.text(firstToken)
	}
	if !(p.at(tokIdentifier) && p.text(p.peek()) == "in") {
		p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
		return nil, nil
	}
	p.advance()
	p.skipStructural()
	collection, err := p.parseExpression(exprModeNested, depth+1)
	if err != nil {
		return nil, err
	}
	if collection == nil {
		return nil, nil
	}
	end := collection.span.EndByte()
	if expectColon {
		p.skipStructural()
		colon, ok := p.eat(tokColon)
		if !ok {
			p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
			return nil, nil
		}
		end = colon.span.EndByte()
	}
	span, err := p.span(forStart, end)
	if err != nil {
		return nil, err
	}
	return &HclForIntro{key: key, value: value, collection: collection, span: span}, nil
}

// parseForCondition parses the optional `if Expression` guard.
func (p *parser) parseForCondition(depth int) (*HclExpression, error) {
	if p.at(tokIdentifier) && p.text(p.peek()) == "if" {
		p.advance()
		p.skipStructural()
		condition, err := p.parseExpression(exprModeNested, depth+1)
		if err != nil {
			return nil, err
		}
		return condition, nil
	}
	return nil, nil
}

func (p *parser) checkForExtent(span document.Span) error {
	extent := span.Len()
	if extent > p.limits.MaxForExtent {
		return lexErrorLimit("for-extent", extent, p.limits.MaxForExtent)
	}
	return nil
}

func (p *parser) parseForTuple(open hclToken, depth int) (*HclExpression, error) {
	forToken := p.advance()
	intro, err := p.parseForIntro(forToken.span.StartByte(), depth, true)
	if err != nil {
		return nil, err
	}
	if intro == nil {
		return nil, nil
	}
	p.skipStructural()
	value, err := p.parseExpression(exprModeNested, depth+1)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	p.skipStructural()
	condition, err := p.parseForCondition(depth)
	if err != nil {
		return nil, err
	}
	p.skipStructural()
	if !p.at(tokBracketClose) {
		p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
		return nil, nil
	}
	close := p.advance()
	p.brackets = p.brackets[:len(p.brackets)-1]
	span, err := p.span(open.span.StartByte(), close.span.EndByte())
	if err != nil {
		return nil, err
	}
	if err := p.checkForExtent(span); err != nil {
		return nil, err
	}
	return &HclExpression{kind: NewForTupleKind(*intro, value, condition), span: span}, nil
}

func (p *parser) parseForObject(open hclToken, depth int) (*HclExpression, error) {
	forToken := p.advance()
	intro, err := p.parseForIntro(forToken.span.StartByte(), depth, true)
	if err != nil {
		return nil, err
	}
	if intro == nil {
		return nil, nil
	}
	p.skipStructural()
	key, err := p.parseExpression(exprModeNested, depth+1)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, nil
	}
	p.skipStructural()
	if !p.at(tokArrow) {
		p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
		return nil, nil
	}
	p.advance()
	p.skipStructural()
	value, err := p.parseExpression(exprModeNested, depth+1)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	grouping := false
	p.skipStructural()
	if p.at(tokEllipsis) {
		p.advance()
		grouping = true
	}
	p.skipStructural()
	condition, err := p.parseForCondition(depth)
	if err != nil {
		return nil, err
	}
	p.skipStructural()
	if !p.at(tokBraceClose) {
		p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
		return nil, nil
	}
	close := p.advance()
	p.brackets = p.brackets[:len(p.brackets)-1]
	span, err := p.span(open.span.StartByte(), close.span.EndByte())
	if err != nil {
		return nil, err
	}
	if err := p.checkForExtent(span); err != nil {
		return nil, err
	}
	return &HclExpression{kind: NewForObjectKind(*intro, key, value, grouping, condition), span: span}, nil
}

// -- templates and heredocs ------------------------------------------------

// parseQuotedTemplate parses one quoted template: literal runs with escape
// decoding, interpolation and directive parts, closed by the closing
// quote. The parts are nil when the template is unterminated (the lexer
// already recovered it) or any part fails; the caller recovers the item.
func (p *parser) parseQuotedTemplate(depth int) ([]HclTemplatePart, document.Span, error) {
	open := p.advance()
	var parts []HclTemplatePart
	for {
		token := p.peek()
		switch token.kind {
		case tokStringClose:
			close := p.advance()
			span, err := p.span(open.span.StartByte(), close.span.EndByte())
			if err != nil {
				return nil, document.Span{}, err
			}
			return parts, span, nil
		case tokStringContent:
			p.advance()
			parts = append(parts, NewLiteralTemplatePart(token.span, decodeQuotedLiteral(p.text(token))))
		case tokInterpolationOpen, tokDirectiveOpen:
			directive := token.kind == tokDirectiveOpen
			partOpen := p.advance()
			contentKind := tokInterpolationContent
			closeKind := tokInterpolationClose
			if directive {
				contentKind = tokDirectiveContent
				closeKind = tokDirectiveClose
			}
			content, ok := p.eat(contentKind)
			if !ok {
				return nil, document.Span{}, nil
			}
			partClose, ok := p.eat(closeKind)
			if !ok {
				return nil, document.Span{}, nil
			}
			partSpan, err := p.span(partOpen.span.StartByte(), partClose.span.EndByte())
			if err != nil {
				return nil, document.Span{}, err
			}
			if directive {
				kind, err := p.parseDirectiveRegion(content.span, depth+1)
				if err != nil {
					return nil, document.Span{}, err
				}
				if kind == nil {
					return nil, document.Span{}, nil
				}
				parts = append(parts, NewDirectiveTemplatePart(partSpan, *kind))
			} else {
				expression, err := p.parseRegionExpression(content.span, depth+1)
				if err != nil {
					return nil, document.Span{}, err
				}
				if expression == nil {
					return nil, document.Span{}, nil
				}
				parts = append(parts, NewInterpolationTemplatePart(partSpan, expression))
			}
		case tokErrorRegion, tokEof:
			// Unterminated at the lexer; no extra diagnostic.
			return nil, document.Span{}, nil
		default:
			p.diagnose(codeExpression, token.span, protocol.CategorySyntax)
			return nil, document.Span{}, nil
		}
	}
}

// parseHeredoc parses one heredoc template: literal content lines with
// `$${`/`%%{` decoding, interpolation and directive parts, and the closing
// marker line as a representation fact (RFC 0014 §4.5).
func (p *parser) parseHeredoc(depth int) (*HclExpression, error) {
	open := p.advance()
	p.skipTrivia()
	if !p.at(tokLineBreak) {
		// Unterminated introducer or content; the lexer recovered it.
		return nil, nil
	}
	p.advance()
	var parts []HclTemplatePart
	for {
		token := p.peek()
		switch token.kind {
		case tokHeredocClose:
			close := p.advance()
			heredocSpan, err := p.span(open.span.StartByte(), close.span.EndByte())
			if err != nil {
				return nil, err
			}
			mode := HeredocModePlain
			if strings.HasPrefix(p.text(open), "<<-") {
				mode = HeredocModeStripIndent
			}
			markerStart := open.span.StartByte() + 2
			if mode == HeredocModeStripIndent {
				markerStart = open.span.StartByte() + 3
			}
			marker := p.decoded[markerStart:open.span.EndByte()]
			markerSpan, err := p.span(markerStart, open.span.EndByte())
			if err != nil {
				return nil, err
			}
			closing := close.span
			facts := NewHeredocFacts(mode, marker, markerSpan, &closing)
			return &HclExpression{kind: NewTemplateKind(parts, &facts), span: heredocSpan}, nil
		case tokHeredocContent:
			p.advance()
			parts = append(parts, NewLiteralTemplatePart(token.span, decodeHeredocLiteral(p.text(token))))
		case tokLineBreak:
			token := p.advance()
			parts = append(parts, NewLiteralTemplatePart(token.span, "\n"))
		case tokInterpolationOpen, tokDirectiveOpen:
			directive := token.kind == tokDirectiveOpen
			partOpen := p.advance()
			contentKind := tokInterpolationContent
			closeKind := tokInterpolationClose
			if directive {
				contentKind = tokDirectiveContent
				closeKind = tokDirectiveClose
			}
			content, ok := p.eat(contentKind)
			if !ok {
				return nil, nil
			}
			partClose, ok := p.eat(closeKind)
			if !ok {
				return nil, nil
			}
			partSpan, err := p.span(partOpen.span.StartByte(), partClose.span.EndByte())
			if err != nil {
				return nil, err
			}
			if directive {
				kind, err := p.parseDirectiveRegion(content.span, depth+1)
				if err != nil {
					return nil, err
				}
				if kind == nil {
					return nil, nil
				}
				parts = append(parts, NewDirectiveTemplatePart(partSpan, *kind))
			} else {
				expression, err := p.parseRegionExpression(content.span, depth+1)
				if err != nil {
					return nil, err
				}
				if expression == nil {
					return nil, nil
				}
				parts = append(parts, NewInterpolationTemplatePart(partSpan, expression))
			}
		default:
			// Unterminated at the lexer (error region or end of file); no
			// extra diagnostic.
			return nil, nil
		}
	}
}

// parseRegionExpression re-lexes one interpolation interior and parses it
// as an expression on a sub-parser over the region tokens.
func (p *parser) parseRegionExpression(span document.Span, depth int) (*HclExpression, error) {
	return withRegion(p, span, func(sub *parser) (*HclExpression, error) {
		return sub.parseExpressionRegion(depth)
	})
}

// parseDirectiveRegion re-lexes one directive interior and parses it as a
// template directive on a sub-parser over the region tokens.
func (p *parser) parseDirectiveRegion(span document.Span, depth int) (*HclDirectiveKind, error) {
	return withRegion(p, span, func(sub *parser) (*HclDirectiveKind, error) {
		return sub.parseDirective(depth)
	})
}

// withRegion runs one parse on a fresh sub-parser over a re-lexed
// interior, then merges the sub-parser's recovery facts into this pass.
func withRegion[T any](p *parser, span document.Span,
	parse func(*parser) (T, error)) (T, error) {
	var zero T
	output, err := lexSource(p.source, p.authority, p.limits, span.StartByte(), span.EndByte(), false)
	if err != nil {
		return zero, err
	}
	p.recovered = p.recovered || output.recovered
	for _, diagnostic := range output.diagnostics {
		p.sink.push(diagnostic.Code, diagnostic.Category, nil)
	}
	for _, region := range output.errorRegions {
		p.errorRegions = append(p.errorRegions, region)
	}
	if err := p.checkErrorRegionLimits(); err != nil {
		return zero, err
	}
	sub := newParser(output, p.source, p.limits, int(^uint(0)>>1))
	result, err := parse(sub)
	p.recovered = p.recovered || sub.recovered
	for _, diagnostic := range sub.sink.diagnostics {
		p.sink.push(diagnostic.Code, diagnostic.Category, nil)
	}
	for _, region := range sub.errorRegions {
		p.errorRegions = append(p.errorRegions, region)
	}
	if err := p.checkErrorRegionLimits(); err != nil {
		return zero, err
	}
	return result, err
}

// parseExpressionRegion parses one expression over the region token
// stream: a full expression followed by the region end, with newlines
// ignored as whitespace.
func (p *parser) parseExpressionRegion(depth int) (*HclExpression, error) {
	expression, err := p.parseExpression(exprModeNested, depth)
	if err != nil {
		return nil, err
	}
	if expression == nil {
		return nil, nil
	}
	p.skipStructural()
	if p.at(tokEof) {
		return expression, nil
	}
	p.diagnose(codeExpression, p.peek().span, protocol.CategorySyntax)
	return nil, nil
}

// parseDirective parses one template directive over the region token
// stream (RFC 0014 §4.4): `%{ if Expression }`, `%{ else }`, `%{ endif }`,
// `%{ for k, v in Expression }` (single-identifier form admitted, §12
// D-7), and `%{ endfor }`.
func (p *parser) parseDirective(depth int) (*HclDirectiveKind, error) {
	p.skipStructural()
	token := p.peek()
	if token.kind != tokIdentifier {
		p.diagnose(codeDirective, token.span, protocol.CategorySyntax)
		return nil, nil
	}
	switch p.text(token) {
	case "if":
		p.advance()
		p.skipStructural()
		condition, err := p.parseExpression(exprModeNested, depth+1)
		if err != nil {
			return nil, err
		}
		if condition == nil {
			return nil, nil
		}
		p.skipStructural()
		if !p.at(tokEof) {
			p.diagnose(codeDirective, p.peek().span, protocol.CategorySyntax)
			return nil, nil
		}
		kind := NewDirectiveIf(condition)
		return &kind, nil
	case "else", "endif", "endfor":
		p.advance()
		p.skipStructural()
		if !p.at(tokEof) {
			p.diagnose(codeDirective, p.peek().span, protocol.CategorySyntax)
			return nil, nil
		}
		var kind HclDirectiveKind
		switch p.text(token) {
		case "else":
			kind = NewDirectiveElse()
		case "endif":
			kind = NewDirectiveEndIf()
		default:
			kind = NewDirectiveEndFor()
		}
		return &kind, nil
	case "for":
		forToken := p.advance()
		intro, err := p.parseForIntro(forToken.span.StartByte(), depth, false)
		if err != nil {
			return nil, err
		}
		if intro == nil {
			return nil, nil
		}
		p.skipStructural()
		if !p.at(tokEof) {
			p.diagnose(codeDirective, p.peek().span, protocol.CategorySyntax)
			return nil, nil
		}
		kind := NewDirectiveFor(*intro)
		return &kind, nil
	default:
		p.diagnose(codeDirective, token.span, protocol.CategorySyntax)
		return nil, nil
	}
}

func delimOf(kind hclTokenKind) delim {
	switch kind {
	case tokBraceOpen:
		return delimBrace
	case tokBracketOpen:
		return delimBracket
	case tokParenOpen:
		return delimParen
	}
	return delimBrace
}

// decodeQuotedLiteral decodes one quoted-template literal run: the frozen
// escape sequences `\n` `\r` `\t` `\"` `\\` `\uNNNN` `\UNNNNNNNN` and the
// escaped openers `$${`/`%%{` (RFC 0014 §4.4). An invalid escape (already
// recovered by the lexer) passes through unchanged.
func decodeQuotedLiteral(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	index := 0
	for index < len(text) {
		switch text[index] {
		case '\\':
			if index+1 >= len(text) {
				out.WriteByte('\\')
				index++
				continue
			}
			next := text[index+1]
			switch next {
			case 'n':
				out.WriteByte('\n')
				index += 2
			case 'r':
				out.WriteByte('\r')
				index += 2
			case 't':
				out.WriteByte('\t')
				index += 2
			case '"':
				out.WriteByte('"')
				index += 2
			case '\\':
				out.WriteByte('\\')
				index += 2
			case 'u':
				if index+6 <= len(text) {
					if value, ok := hexValue(text[index+2 : index+6]); ok {
						if ch, ok := runeFromScalar(value); ok {
							out.WriteRune(ch)
							index += 6
							continue
						}
					}
				}
				out.WriteByte('\\')
				index++
			case 'U':
				if index+10 <= len(text) {
					if value, ok := hexValue(text[index+2 : index+10]); ok {
						if ch, ok := runeFromScalar(value); ok {
							out.WriteRune(ch)
							index += 10
							continue
						}
					}
				}
				out.WriteByte('\\')
				index++
			default:
				out.WriteByte('\\')
				index++
			}
		case '$':
			if index+2 < len(text) && text[index+1] == '$' && text[index+2] == '{' {
				out.WriteString("${")
				index += 3
			} else {
				out.WriteByte('$')
				index++
			}
		case '%':
			if index+2 < len(text) && text[index+1] == '%' && text[index+2] == '{' {
				out.WriteString("%{")
				index += 3
			} else {
				out.WriteByte('%')
				index++
			}
		default:
			ch, width := decodeRune(text[index:])
			out.WriteRune(ch)
			index += width
		}
	}
	return out.String()
}

// decodeHeredocLiteral decodes one heredoc content run: only the escaped
// openers `$${`/`%%{` decode (RFC 0014 §4.5); backslash escapes are not
// admitted in heredocs.
func decodeHeredocLiteral(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	index := 0
	for index < len(text) {
		switch text[index] {
		case '$':
			if index+2 < len(text) && text[index+1] == '$' && text[index+2] == '{' {
				out.WriteString("${")
				index += 3
			} else {
				out.WriteByte('$')
				index++
			}
		case '%':
			if index+2 < len(text) && text[index+1] == '%' && text[index+2] == '{' {
				out.WriteString("%{")
				index += 3
			} else {
				out.WriteByte('%')
				index++
			}
		default:
			ch, width := decodeRune(text[index:])
			out.WriteRune(ch)
			index += width
		}
	}
	return out.String()
}

// hexValue decodes one hexadecimal digit run.
func hexValue(text string) (uint32, bool) {
	var value uint32
	for i := 0; i < len(text); i++ {
		digit := hexDigit(text[i])
		if digit < 0 {
			return 0, false
		}
		value = value*16 + uint32(digit)
	}
	return value, true
}

// runeFromScalar converts one scalar value into a rune; surrogates and
// out-of-range values are rejected.
func runeFromScalar(value uint32) (rune, bool) {
	if value > 0x10FFFF || (value >= 0xD800 && value <= 0xDFFF) {
		return 0, false
	}
	return rune(value), true
}
