package xml

// This file implements XML formation: source facts, tokenization, native
// tree, safe DTD subset, bounded entity expansion, recovery, and
// exhaustive piece coverage (RFC 0012 §2-4, §6-7, §12-13;
// crates/consema-xml/src/parser.rs). The tokenizer is an independent
// implementation of the XML 1.0 token surface consumed by the reference
// parser; it performs no I/O and never opens another entity.

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// xmlTokenKind is the closed token category emitted by the tokenizer.
type xmlTokenKind uint8

const (
	tokenDeclaration xmlTokenKind = iota
	tokenProcessingInstruction
	tokenComment
	tokenDtdStart
	tokenEmptyDtd
	tokenEntityDeclaration
	tokenDtdEnd
	tokenElementStart
	tokenAttribute
	tokenElementOpenEnd
	tokenElementEmptyEnd
	tokenElementCloseEnd
	tokenText
	tokenCdata
)

// xmlToken is one tokenizer-produced token with decoded byte spans.
type xmlToken struct {
	kind xmlTokenKind
	// Full construct span.
	start, end int
	// Name spans: prefix (empty when unprefixed) and local.
	prefixStart, prefixEnd int
	localStart, localEnd   int
	// Value spans (attribute value, entity value, comment text, CDATA
	// text, PI content, declaration values).
	valueStart, valueEnd int
	// Declaration facts.
	versionStart, versionEnd   int
	encodingStart, encodingEnd int
	standalone                 bool
	hasStandalone              bool
	// PI target span.
	targetStart, targetEnd int
	// DTD facts.
	dtdExternal bool
	dtdSubset   bool
	entityValue bool
}

// isPrefix reports whether the token's name carries a prefix.
func (t *xmlToken) isPrefix() bool { return t.prefixStart < t.prefixEnd }

// xmlParser is the formation state machine (parser.rs:182-208).
type xmlParser struct {
	ctx                     context.Context
	source                  *document.SourceSnapshot
	authority               document.DocumentAuthority
	decoded                 string
	limits                  XmlParseLimits
	diagnostics             []*protocol.Diagnostic
	pieces                  []document.StructuralPiece
	syntaxKinds             []XmlSyntaxKind
	nodes                   []XmlContent
	parentOf                []*int
	nextOrdinal             uint64
	entityState             EntityExpansionState
	entities                []EntityDeclarationData
	stack                   []*xmlFrame
	prolog                  []XmlPrologItem
	epilog                  []XmlPrologItem
	declaration             *XmlDeclarationData
	doctype                 *XmlDoctypeData
	doctypeName             *QNameFacts
	doctypeSpanStart        *int
	externalSubsetRecovered bool
	dtdSubsetStart          *int
	root                    *int
	recovered               bool
	errorRegions            int
	pending                 []*xmlToken
}

// xmlFrame is one open element's formation state (parser.rs:163-180).
type xmlFrame struct {
	start               int
	span                document.Span
	qname               QNameFacts
	expanded            *ExpandedName
	namespaceError      *NamespaceError
	scope               NamespaceScope
	namespaces          []XmlNamespaceBindingData
	attributes          []XmlAttributeData
	children            []int
	pendingDeclarations []pendingDeclaration
	pendingAttributes   []pendingAttribute
}

// pendingDeclaration is one namespace declaration seen before start-tag
// finalization (parser.rs:161-169).
type pendingDeclaration struct {
	qname   QNameFacts
	uri     string
	uriSpan document.Span
}

// pendingAttribute is one attribute seen before start-tag finalization
// (parser.rs:151-159).
type pendingAttribute struct {
	qname       QNameFacts
	span        document.Span
	valueSpan   document.Span
	fragments   []ReferenceFragment
	normalized  string
	singleQuote bool
}

// buildDocument forms one document from the decoded text
// (parser.rs:246-273).
func buildDocument(ctx context.Context, source *document.SourceSnapshot,
	decoded string, limits XmlParseLimits) (*Document, *FormationFailure) {
	parser := &xmlParser{
		ctx:       ctx,
		source:    source,
		authority: document.NewDocumentAuthority(),
		decoded:   decoded,
		limits:    limits,
	}
	parser.coverBOM()
	pos := 0
	for {
		if ctx != nil && ctx.Err() != nil {
			return nil, &FormationFailure{Kind: FormationFailureCancelled}
		}
		token, next, errPos := parser.scan(pos)
		if next < 0 {
			break
		}
		if errPos >= 0 {
			// A tokenizer error jumps the stream to the end of the document
			// (xmlparser Stream::jump_to_end), so the recovery region is
			// always the final byte and tokenization stops.
			parser.pending = nil
			if failure := parser.recoverErrorRegion(len(decoded)-1, len(decoded)); failure != nil {
				return nil, failure
			}
			break
		}
		if token != nil {
			if failure := parser.handle(token); failure != nil {
				return nil, failure
			}
		}
		pos = next
	}
	return parser.finish()
}

// coverBOM covers a leading BOM as trivia; the tokenizer skips it in
// decoded text (parser.rs:275-285).
func (p *xmlParser) coverBOM() {
	if bom := p.source.EncodingFacts().Bom(); bom != nil {
		length := 0
		switch *bom {
		case document.BomUtf8:
			length = 3
		case document.BomUtf16Le, document.BomUtf16Be:
			length = 2
		}
		if length > 0 {
			if span, err := p.authority.Span(0, length); err == nil {
				p.pushPiece(span, XmlSyntaxKindBom, document.PieceTrivia)
			}
		}
	}
}

// scan tokenizes one construct at pos. The return protocol is
// (token, next, errPos): a negative next means end of input; a nil token
// with a non-negative next means whitespace was skipped at the top level;
// a non-negative errPos means a tokenizer error.
func (p *xmlParser) scan(pos int) (*xmlToken, int, int) {
	if len(p.pending) > 0 {
		token := p.pending[0]
		p.pending = p.pending[1:]
		return token, pos, -1
	}
	if pos >= len(p.decoded) {
		return nil, -1, -1
	}
	if pos == 0 {
		// The decoded text retains a leading BOM as U+FEFF, which the
		// tokenizer skips inside its stream.
		r, width := utf8.DecodeRuneInString(p.decoded)
		if r == '\uFEFF' {
			pos += width
			if pos >= len(p.decoded) {
				return nil, -1, -1
			}
		}
	}
	if len(p.stack) == 0 {
		// Top-level content: whitespace is skipped without a token, and
		// non-whitespace text is a tokenizer error (xmlparser states
		// AfterDeclaration/AfterDtd/AfterElements).
		return p.scanTopLevel(pos)
	}
	return p.scanElementContent(pos)
}

// scanTopLevel scans one construct outside any element.
func (p *xmlParser) scanTopLevel(pos int) (*xmlToken, int, int) {
	rest := p.decoded[pos:]
	switch {
	case p.isSpace(rest[0]):
		cursor := pos
		p.skipSpaces(&cursor)
		return nil, cursor, -1
	case strings.HasPrefix(rest, "<!DOCTYPE"):
		if p.root != nil {
			// AfterElements rejects a second DOCTYPE.
			return nil, pos, pos
		}
		return p.scanDoctype(pos)
	case strings.HasPrefix(rest, "<!--"):
		return p.scanComment(pos)
	case strings.HasPrefix(rest, "<?"):
		if strings.HasPrefix(rest, "<?xml ") && !p.atStreamStart(pos) {
			// `<?xml ` is only legal at the very start of the stream.
			return nil, pos, pos
		}
		return p.scanQuestion(pos)
	case strings.HasPrefix(rest, "<!"):
		return nil, pos, pos + 2
	case strings.HasPrefix(rest, "</"):
		return nil, pos, pos + 2
	case strings.HasPrefix(rest, "<"):
		if p.root != nil {
			// AfterElements rejects any further root-level markup.
			return nil, pos, pos
		}
		return p.scanStartTag(pos)
	default:
		// Non-whitespace character data outside the document element is a
		// tokenizer error.
		return nil, pos, pos
	}
}

// scanElementContent scans one construct inside an element.
func (p *xmlParser) scanElementContent(pos int) (*xmlToken, int, int) {
	rest := p.decoded[pos:]
	switch {
	case strings.HasPrefix(rest, "<?"):
		if strings.HasPrefix(rest, "<?xml ") {
			return nil, pos, pos
		}
		return p.scanQuestion(pos)
	case strings.HasPrefix(rest, "<!--"):
		return p.scanComment(pos)
	case strings.HasPrefix(rest, "<![CDATA["):
		return p.scanCdata(pos)
	case strings.HasPrefix(rest, "<!"):
		return nil, pos, pos + 2
	case strings.HasPrefix(rest, "</"):
		return p.scanEndTag(pos)
	case strings.HasPrefix(rest, "<"):
		return p.scanStartTag(pos)
	default:
		return p.scanText(pos)
	}
}

// scanText scans one text run up to the next markup start.
func (p *xmlParser) scanText(pos int) (*xmlToken, int, int) {
	next := strings.IndexByte(p.decoded[pos:], '<')
	if next < 0 {
		next = len(p.decoded)
	} else {
		next += pos
	}
	// According to the spec, `]]>` must not appear inside a Text node.
	if strings.Contains(p.decoded[pos:next], "]]>") {
		return nil, pos, pos
	}
	return &xmlToken{kind: tokenText, start: pos, end: next,
		valueStart: pos, valueEnd: next}, next, -1
}

// scanQuestion scans a declaration (only as the first construct) or a
// processing instruction.
func (p *xmlParser) scanQuestion(pos int) (*xmlToken, int, int) {
	atStart := p.atStreamStart(pos)
	cursor := pos + 2
	nameStart, nameEnd, ok := p.scanName(cursor)
	if !ok {
		return nil, pos, cursor
	}
	// The declaration is recognized only by the exact `<?xml ` spelling at
	// the stream start (xmlparser Declaration state); `<?xml` without the
	// trailing space is a processing instruction with target `xml`.
	if atStart && p.decoded[nameStart:nameEnd] == "xml" &&
		nameEnd < len(p.decoded) && p.isSpace(p.decoded[nameEnd]) {
		return p.scanDeclaration(pos, nameStart, nameEnd)
	}
	// Processing instruction: optional whitespace then content to `?>`.
	targetStart, targetEnd := nameStart, nameEnd
	contentStart := nameEnd
	// The content excludes the whitespace after the target.
	for contentStart < len(p.decoded) && p.isSpace(p.decoded[contentStart]) {
		contentStart++
	}
	closeAt := strings.Index(p.decoded[contentStart:], "?>")
	if closeAt < 0 {
		return nil, pos, contentStart
	}
	closeAt += contentStart
	token := &xmlToken{
		kind:        tokenProcessingInstruction,
		start:       pos,
		end:         closeAt + 2,
		targetStart: targetStart,
		targetEnd:   targetEnd,
		valueStart:  contentStart,
		valueEnd:    closeAt,
	}
	return token, closeAt + 2, -1
}

// atStreamStart reports whether the construct at pos is the first
// construct after an optional BOM.
func (p *xmlParser) atStreamStart(pos int) bool {
	start := 0
	r, width := utf8.DecodeRuneInString(p.decoded)
	if r == '\uFEFF' {
		start = width
	}
	return pos == start
}

// scanDeclaration scans the fixed declaration grammar `<?xml` S version Eq
// value (S encoding Eq value)? (S standalone Eq value)? S? `?>`.
func (p *xmlParser) scanDeclaration(pos, nameStart, nameEnd int) (*xmlToken, int, int) {
	cursor := nameEnd
	p.skipSpaces(&cursor)
	versionName, versionStart, versionEnd, versionAfter, ok := p.scanPseudoAttribute(cursor)
	if !ok || versionName != "version" {
		return nil, pos, cursor
	}
	token := &xmlToken{
		kind:         tokenDeclaration,
		start:        pos,
		versionStart: versionStart,
		versionEnd:   versionEnd,
	}
	cursor = versionAfter
	p.skipSpaces(&cursor)
	encodingName, encodingStart, encodingEnd, encodingAfter, encodingOk := p.scanPseudoAttribute(cursor)
	if encodingOk && encodingName == "encoding" {
		token.encodingStart, token.encodingEnd = encodingStart, encodingEnd
		cursor = encodingAfter
		p.skipSpaces(&cursor)
	}
	standaloneName, standaloneStart, standaloneEnd, standaloneAfter, standaloneOk := p.scanPseudoAttribute(cursor)
	if standaloneOk {
		if standaloneName != "standalone" {
			return nil, pos, cursor
		}
		value := p.decoded[standaloneStart:standaloneEnd]
		if value != "yes" && value != "no" {
			return nil, pos, cursor
		}
		token.standalone = value == "yes"
		token.hasStandalone = true
		cursor = standaloneAfter
		p.skipSpaces(&cursor)
	}
	if !strings.HasPrefix(p.decoded[cursor:], "?>") {
		return nil, pos, cursor
	}
	token.end = cursor + 2
	return token, cursor + 2, -1
}

// scanPseudoAttribute scans `name = "value"` returning the name, the
// value span, and the position after the closing quote.
func (p *xmlParser) scanPseudoAttribute(cursor int) (name string, valueStart, valueEnd, after int, ok bool) {
	nameStart, nameEnd, ok := p.scanName(cursor)
	if !ok {
		return "", 0, 0, 0, false
	}
	name = p.decoded[nameStart:nameEnd]
	cursor = nameEnd
	p.skipSpaces(&cursor)
	if cursor >= len(p.decoded) || p.decoded[cursor] != '=' {
		return "", 0, 0, 0, false
	}
	cursor++
	p.skipSpaces(&cursor)
	if cursor >= len(p.decoded) || (p.decoded[cursor] != '"' && p.decoded[cursor] != '\'') {
		return "", 0, 0, 0, false
	}
	quote := p.decoded[cursor]
	cursor++
	valueStart = cursor
	closeAt := strings.IndexByte(p.decoded[cursor:], quote)
	if closeAt < 0 {
		return "", 0, 0, 0, false
	}
	closeAt += cursor
	return name, valueStart, closeAt, closeAt + 1, true
}

// scanComment scans `<!-- … -->`.
func (p *xmlParser) scanComment(pos int) (*xmlToken, int, int) {
	closeAt := strings.Index(p.decoded[pos+4:], "-->")
	if closeAt < 0 {
		return nil, pos, pos + 4
	}
	closeAt += pos + 4
	token := &xmlToken{
		kind:       tokenComment,
		start:      pos,
		end:        closeAt + 3,
		valueStart: pos + 4,
		valueEnd:   closeAt,
	}
	return token, closeAt + 3, -1
}

// scanCdata scans `<![CDATA[ … ]]>`.
func (p *xmlParser) scanCdata(pos int) (*xmlToken, int, int) {
	closeAt := strings.Index(p.decoded[pos+9:], "]]>")
	if closeAt < 0 {
		return nil, pos, pos + 9
	}
	closeAt += pos + 9
	token := &xmlToken{
		kind:       tokenCdata,
		start:      pos,
		end:        closeAt + 3,
		valueStart: pos + 9,
		valueEnd:   closeAt,
	}
	return token, closeAt + 3, -1
}

// scanDoctype scans `<!DOCTYPE name (external-id)? (">" | "[" … "]")`.
func (p *xmlParser) scanDoctype(pos int) (*xmlToken, int, int) {
	cursor := pos + 9
	p.skipSpaces(&cursor)
	nameStart, nameEnd, ok := p.scanName(cursor)
	if !ok {
		return nil, pos, cursor
	}
	cursor = nameEnd
	p.skipSpaces(&cursor)
	external := false
	if strings.HasPrefix(p.decoded[cursor:], "SYSTEM") || strings.HasPrefix(p.decoded[cursor:], "PUBLIC") {
		external = true
		cursor += 6
		p.skipSpaces(&cursor)
		if cursor >= len(p.decoded) || (p.decoded[cursor] != '"' && p.decoded[cursor] != '\'') {
			return nil, pos, cursor
		}
		quote := p.decoded[cursor]
		closeAt := strings.IndexByte(p.decoded[cursor+1:], quote)
		if closeAt < 0 {
			return nil, pos, cursor
		}
		cursor += 1 + closeAt + 1
		p.skipSpaces(&cursor)
	}
	token := &xmlToken{
		kind:        tokenEmptyDtd,
		start:       pos,
		localStart:  nameStart,
		localEnd:    nameEnd,
		dtdExternal: external,
	}
	if cursor < len(p.decoded) && p.decoded[cursor] == '[' {
		token.kind = tokenDtdStart
		token.dtdSubset = true
		token.end = cursor // the DtdStart span ends at the `[`
		subsetEnd, errPos := p.scanSubset(cursor + 1)
		if errPos >= 0 {
			return nil, pos, errPos
		}
		p.pending = append(p.pending, &xmlToken{
			kind:  tokenDtdEnd,
			start: subsetEnd - 2,
			end:   subsetEnd,
		})
		return token, subsetEnd, -1
	}
	if cursor < len(p.decoded) && p.decoded[cursor] == '>' {
		token.end = cursor + 1
		return token, cursor + 1, -1
	}
	return nil, pos, cursor
}

// scanSubset scans the internal DTD subset until `]>`, queueing the
// admitted subset tokens.
func (p *xmlParser) scanSubset(pos int) (int, int) {
	cursor := pos
	for {
		if cursor >= len(p.decoded) {
			return 0, cursor
		}
		switch {
		case p.decoded[cursor] == ']' && cursor+1 < len(p.decoded) && p.decoded[cursor+1] == '>':
			return cursor + 2, -1
		case p.isSpace(p.decoded[cursor]):
			cursor++
		case strings.HasPrefix(p.decoded[cursor:], "<!--"):
			closeAt := strings.Index(p.decoded[cursor+4:], "-->")
			if closeAt < 0 {
				return 0, cursor + 4
			}
			closeAt += cursor + 4
			p.pending = append(p.pending, &xmlToken{
				kind:       tokenComment,
				start:      cursor,
				end:        closeAt + 3,
				valueStart: cursor + 4,
				valueEnd:   closeAt,
			})
			cursor = closeAt + 3
		case strings.HasPrefix(p.decoded[cursor:], "<?"):
			closeAt := strings.Index(p.decoded[cursor+2:], "?>")
			if closeAt < 0 {
				return 0, cursor + 2
			}
			closeAt += cursor + 2
			contentStart := cursor + 2
			contentEnd := closeAt
			targetStart, targetEnd, ok := p.scanName(contentStart)
			if !ok {
				return 0, contentStart
			}
			contentStart = targetEnd
			for contentStart < closeAt && p.isSpace(p.decoded[contentStart]) {
				contentStart++
			}
			p.pending = append(p.pending, &xmlToken{
				kind:        tokenProcessingInstruction,
				start:       cursor,
				end:         closeAt + 2,
				targetStart: targetStart,
				targetEnd:   targetEnd,
				valueStart:  contentStart,
				valueEnd:    contentEnd,
			})
			cursor = closeAt + 2
		case strings.HasPrefix(p.decoded[cursor:], "<!ENTITY"):
			next, errPos := p.scanEntityDeclaration(cursor)
			if errPos >= 0 {
				return 0, errPos
			}
			cursor = next
		case strings.HasPrefix(p.decoded[cursor:], "<!ELEMENT"),
			strings.HasPrefix(p.decoded[cursor:], "<!ATTLIST"),
			strings.HasPrefix(p.decoded[cursor:], "<!NOTATION"):
			// Excluded validation declarations are consumed by the tokenizer
			// and flagged from the subset text at the DTD end.
			closeAt := strings.IndexByte(p.decoded[cursor:], '>')
			if closeAt < 0 {
				return 0, cursor
			}
			cursor += closeAt + 1
		default:
			return 0, cursor
		}
	}
}

// scanEntityDeclaration scans `<!ENTITY S? (% S)? name (value | SYSTEM/PUBLIC …) >`.
func (p *xmlParser) scanEntityDeclaration(pos int) (int, int) {
	cursor := pos + 8
	p.skipSpaces(&cursor)
	if cursor < len(p.decoded) && p.decoded[cursor] == '%' {
		cursor++
		p.skipSpaces(&cursor)
	}
	nameStart, nameEnd, ok := p.scanName(cursor)
	if !ok {
		return 0, cursor
	}
	cursor = nameEnd
	p.skipSpaces(&cursor)
	token := &xmlToken{
		kind:       tokenEntityDeclaration,
		start:      pos,
		localStart: nameStart,
		localEnd:   nameEnd,
	}
	if strings.HasPrefix(p.decoded[cursor:], "SYSTEM") || strings.HasPrefix(p.decoded[cursor:], "PUBLIC") {
		token.entityValue = false
		cursor += 6
		p.skipSpaces(&cursor)
		if cursor >= len(p.decoded) || (p.decoded[cursor] != '"' && p.decoded[cursor] != '\'') {
			return 0, cursor
		}
		quote := p.decoded[cursor]
		closeAt := strings.IndexByte(p.decoded[cursor+1:], quote)
		if closeAt < 0 {
			return 0, cursor
		}
		cursor += 1 + closeAt + 1
		p.skipSpaces(&cursor)
	} else {
		token.entityValue = true
		if cursor >= len(p.decoded) || (p.decoded[cursor] != '"' && p.decoded[cursor] != '\'') {
			return 0, cursor
		}
		quote := p.decoded[cursor]
		cursor++
		valueStart := cursor
		closeAt := strings.IndexByte(p.decoded[cursor:], quote)
		if closeAt < 0 {
			return 0, cursor
		}
		closeAt += cursor
		token.valueStart = valueStart
		token.valueEnd = closeAt
		cursor = closeAt + 1
		p.skipSpaces(&cursor)
	}
	if cursor >= len(p.decoded) || p.decoded[cursor] != '>' {
		return 0, cursor
	}
	token.end = cursor + 1
	p.pending = append(p.pending, token)
	return token.end, -1
}

// scanEndTag scans `</ name S? >`.
func (p *xmlParser) scanEndTag(pos int) (*xmlToken, int, int) {
	cursor := pos + 2
	localStart, localEnd, prefixStart, prefixEnd, ok := p.scanQName(cursor)
	if !ok {
		return nil, pos, cursor
	}
	cursor = localEnd
	p.skipSpaces(&cursor)
	if cursor >= len(p.decoded) || p.decoded[cursor] != '>' {
		return nil, pos, cursor
	}
	return &xmlToken{
		kind:        tokenElementCloseEnd,
		start:       pos,
		end:         cursor + 1,
		prefixStart: prefixStart,
		prefixEnd:   prefixEnd,
		localStart:  localStart,
		localEnd:    localEnd,
	}, cursor + 1, -1
}

// scanStartTag scans `< name (S attribute)* S? ("/>" | ">")`.
func (p *xmlParser) scanStartTag(pos int) (*xmlToken, int, int) {
	cursor := pos + 1
	localStart, localEnd, prefixStart, prefixEnd, ok := p.scanQName(cursor)
	if !ok {
		return nil, pos, cursor
	}
	token := &xmlToken{
		kind:        tokenElementStart,
		start:       pos,
		end:         localEnd,
		prefixStart: prefixStart,
		prefixEnd:   prefixEnd,
		localStart:  localStart,
		localEnd:    localEnd,
	}
	next, errPos := p.scanTagTail(localEnd)
	if errPos >= 0 {
		return nil, pos, errPos
	}
	if p.decoded[next-1] == '>' {
		if next >= 2 && p.decoded[next-2] == '/' {
			p.pending = append(p.pending, &xmlToken{
				kind: tokenElementEmptyEnd, start: next - 2, end: next,
			})
		} else {
			p.pending = append(p.pending, &xmlToken{
				kind: tokenElementOpenEnd, start: next - 1, end: next,
			})
		}
	}
	return token, next, -1
}

// scanTagTail scans the rest of one start tag including its attributes
// and returns the position after the closing `>` or `/>`.
func (p *xmlParser) scanTagTail(cursor int) (int, int) {
	for {
		if cursor >= len(p.decoded) {
			return 0, cursor
		}
		switch p.decoded[cursor] {
		case '>':
			return cursor + 1, -1
		case '/':
			if cursor+1 < len(p.decoded) && p.decoded[cursor+1] == '>' {
				return cursor + 2, -1
			}
			return 0, cursor
		case ' ', '\t', '\r', '\n':
			cursor++
		default:
			next, errPos := p.scanAttribute(cursor)
			if errPos >= 0 {
				return 0, errPos
			}
			cursor = next
		}
	}
}

// scanAttribute scans one `name S? = S? "value"` attribute and queues its
// token.
func (p *xmlParser) scanAttribute(cursor int) (int, int) {
	localStart, localEnd, prefixStart, prefixEnd, ok := p.scanQName(cursor)
	if !ok {
		return 0, cursor
	}
	attrCursor := localEnd
	p.skipSpaces(&attrCursor)
	if attrCursor >= len(p.decoded) || p.decoded[attrCursor] != '=' {
		return 0, attrCursor
	}
	attrCursor++
	p.skipSpaces(&attrCursor)
	if attrCursor >= len(p.decoded) || (p.decoded[attrCursor] != '"' && p.decoded[attrCursor] != '\'') {
		return 0, attrCursor
	}
	quote := p.decoded[attrCursor]
	attrCursor++
	valueStart := attrCursor
	for attrCursor < len(p.decoded) {
		if p.decoded[attrCursor] == quote {
			break
		}
		if p.decoded[attrCursor] == '<' {
			return 0, attrCursor
		}
		attrCursor++
	}
	if attrCursor >= len(p.decoded) {
		return 0, attrCursor
	}
	p.pending = append(p.pending, &xmlToken{
		kind:        tokenAttribute,
		start:       localStart,
		end:         attrCursor + 1,
		prefixStart: prefixStart,
		prefixEnd:   prefixEnd,
		localStart:  localStart,
		localEnd:    localEnd,
		valueStart:  valueStart,
		valueEnd:    attrCursor,
	})
	return attrCursor + 1, -1
}

// scanQName scans one name possibly split at the first colon; the local
// span starts after the colon.
func (p *xmlParser) scanQName(cursor int) (localStart, localEnd, prefixStart, prefixEnd int, ok bool) {
	nameStart, nameEnd, ok := p.scanName(cursor)
	if !ok {
		return 0, 0, 0, 0, false
	}
	colon := strings.IndexByte(p.decoded[nameStart:nameEnd], ':')
	if colon < 0 {
		return nameStart, nameEnd, 0, 0, true
	}
	colon += nameStart
	if strings.IndexByte(p.decoded[colon+1:nameEnd], ':') >= 0 {
		return 0, 0, 0, 0, false
	}
	return colon + 1, nameEnd, nameStart, colon, true
}

// scanName scans one XML name at cursor.
func (p *xmlParser) scanName(cursor int) (int, int, bool) {
	if cursor >= len(p.decoded) {
		return 0, 0, false
	}
	r, width := utf8.DecodeRuneInString(p.decoded[cursor:])
	if !isNameStart(r) {
		return 0, 0, false
	}
	start := cursor
	cursor += width
	for cursor < len(p.decoded) {
		r, width = utf8.DecodeRuneInString(p.decoded[cursor:])
		if !isNameChar(r) {
			break
		}
		cursor += width
	}
	return start, cursor, true
}

// skipSpaces skips XML whitespace (space, tab, LF, CR).
func (p *xmlParser) skipSpaces(cursor *int) {
	for *cursor < len(p.decoded) && p.isSpace(p.decoded[*cursor]) {
		*cursor++
	}
}

func (p *xmlParser) isSpace(byte byte) bool {
	return byte == ' ' || byte == '\t' || byte == '\n' || byte == '\r'
}

// isNameStart reports whether r can start an XML name.
func isNameStart(r rune) bool {
	switch {
	case r == ':' || r == '_':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= 0xC0 && r <= 0xD6:
		return true
	case r >= 0xD8 && r <= 0xF6:
		return true
	case r >= 0xF8 && r <= 0x2FF:
		return true
	case r >= 0x370 && r <= 0x37D:
		return true
	case r >= 0x37F && r <= 0x1FFF:
		return true
	case r >= 0x200C && r <= 0x200D:
		return true
	case r >= 0x2070 && r <= 0x218F:
		return true
	case r >= 0x2C00 && r <= 0x2FEF:
		return true
	case r >= 0x3001 && r <= 0xD7FF:
		return true
	case r >= 0xF900 && r <= 0xFDCF:
		return true
	case r >= 0xFDF0 && r <= 0xFFFD:
		return true
	case r >= 0x10000 && r <= 0xEFFFF:
		return true
	}
	return false
}

// isNameChar reports whether r can continue an XML name.
func isNameChar(r rune) bool {
	if isNameStart(r) {
		return true
	}
	switch {
	case r == '-' || r == '.':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == 0xB7:
		return true
	case r >= 0x300 && r <= 0x36F:
		return true
	case r >= 0x203F && r <= 0x2040:
		return true
	}
	return false
}

// handle dispatches one token to its formation handler (parser.rs:287-332).
func (p *xmlParser) handle(token *xmlToken) *FormationFailure {
	switch token.kind {
	case tokenDeclaration:
		return p.handleDeclaration(token)
	case tokenProcessingInstruction:
		return p.processingInstruction(token)
	case tokenComment:
		return p.comment(token)
	case tokenDtdStart:
		return p.doctypeStart(token)
	case tokenEmptyDtd:
		return p.doctypeEmpty(token)
	case tokenEntityDeclaration:
		return p.entityDeclaration(token)
	case tokenDtdEnd:
		return p.dtdEnd(token)
	case tokenElementStart:
		return p.elementStart(token)
	case tokenAttribute:
		return p.attribute(token)
	case tokenElementOpenEnd:
		return p.elementOpenEnd(token)
	case tokenElementEmptyEnd:
		return p.elementEmptyEnd(token)
	case tokenElementCloseEnd:
		return p.elementCloseEnd(token)
	case tokenText:
		return p.text(token)
	case tokenCdata:
		return p.cdata(token)
	}
	return nil
}

// declaration handles the XML declaration token (parser.rs:334-503).
func (p *xmlParser) handleDeclaration(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	declarationOpen, failure := p.rawSpanOffset(token.start, token.start+5)
	if failure != nil {
		return failure
	}
	p.pushPiece(declarationOpen, XmlSyntaxKindDeclarationOpen, document.PieceToken)
	standaloneFacts, failure := p.declarationParts(token)
	if failure != nil {
		return failure
	}
	versionRaw, failure := p.rawSpanOffset(token.versionStart, token.versionEnd)
	if failure != nil {
		return failure
	}
	if p.decoded[token.versionStart:token.versionEnd] != "1.0" {
		p.recover("xml.declaration.version@1", versionRaw, protocol.CategorySyntax)
	}
	var encodingFacts *XmlEncodingFact
	if token.encodingStart < token.encodingEnd {
		encodingRaw, failure := p.rawSpanOffset(token.encodingStart, token.encodingEnd)
		if failure != nil {
			return failure
		}
		upper := strings.ToUpper(p.decoded[token.encodingStart:token.encodingEnd])
		selected := p.source.EncodingFacts().Selected()
		agrees := false
		switch {
		case selected.Equal(document.Utf8Encoding()):
			agrees = upper == "UTF-8"
		case selected.Equal(document.Utf16LeEncoding()):
			agrees = upper == "UTF-16" || upper == "UTF-16LE"
		case selected.Equal(document.Utf16BeEncoding()):
			agrees = upper == "UTF-16" || upper == "UTF-16BE"
		}
		if !agrees {
			p.recover("xml.declaration.conflict@1", encodingRaw, protocol.CategoryEncoding)
		}
		encodingFacts = &XmlEncodingFact{
			Span:  encodingRaw,
			Value: p.decoded[token.encodingStart:token.encodingEnd],
		}
	}
	declared := &XmlDeclarationData{
		Span:        raw,
		VersionSpan: versionRaw,
		Version:     p.decoded[token.versionStart:token.versionEnd],
		Encoding:    encodingFacts,
		Standalone:  standaloneFacts,
	}
	if p.declaration != nil {
		p.recover("xml.declaration.duplicate@1", raw, protocol.CategorySyntax)
	}
	p.declaration = declared
	return nil
}

// declarationParts pushes the declaration part pieces and locates the
// standalone value span (parser.rs:411-503).
func (p *xmlParser) declarationParts(token *xmlToken) (*XmlStandaloneFact, *FormationFailure) {
	text := p.decoded[token.start:token.end]
	cursor := 5 // past `<?xml`, relative to the declaration span
	pushNameValue := func(name string, nameStart int, valueStart, valueEnd int) *FormationFailure {
		if nameRaw, err := p.rawSpanOffset(token.start+nameStart, token.start+nameStart+len(name)); err != nil {
			return err
		} else {
			p.pushPiece(nameRaw, XmlSyntaxKindDeclarationName, document.PieceToken)
		}
		valueRaw, err := p.rawSpanOffset(token.start+valueStart, token.start+valueEnd)
		if err != nil {
			return err
		}
		p.pushPiece(valueRaw, XmlSyntaxKindDeclarationValue, document.PieceToken)
		return nil
	}
	cursor = skipDeclarationSpaces(text, cursor)
	if strings.HasPrefix(text[cursor:], "version") {
		if failure := pushNameValue("version", cursor,
			token.versionStart-token.start, token.versionEnd-token.start); failure != nil {
			return nil, failure
		}
		cursor = token.versionEnd - token.start + 1 // past the closing quote
	}
	if token.encodingStart < token.encodingEnd {
		cursor = skipDeclarationSpaces(text, cursor)
		if strings.HasPrefix(text[cursor:], "encoding") {
			if failure := pushNameValue("encoding", cursor,
				token.encodingStart-token.start, token.encodingEnd-token.start); failure != nil {
				return nil, failure
			}
			cursor = token.encodingEnd - token.start + 1
		}
	}
	cursor = skipDeclarationSpaces(text, cursor)
	var standaloneFacts *XmlStandaloneFact
	if strings.HasPrefix(text[cursor:], "standalone") {
		if nameRaw, err := p.rawSpanOffset(token.start+cursor, token.start+cursor+len("standalone")); err != nil {
			return nil, err
		} else {
			p.pushPiece(nameRaw, XmlSyntaxKindDeclarationName, document.PieceToken)
		}
		rest := text[cursor+len("standalone"):]
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			return nil, nil
		}
		valueStart := skipDeclarationSpaces(text, cursor+len("standalone")+eq+1)
		if valueStart >= len(text) || (text[valueStart] != '"' && text[valueStart] != '\'') {
			return nil, nil
		}
		quote := text[valueStart]
		valueEnd := strings.IndexByte(text[valueStart+1:], quote)
		if valueEnd < 0 {
			return nil, nil
		}
		valueEnd += valueStart + 1
		valueSpan, err := p.rawSpanOffset(token.start+valueStart+1, token.start+valueEnd)
		if err != nil {
			return nil, err
		}
		p.pushPiece(valueSpan, XmlSyntaxKindDeclarationValue, document.PieceToken)
		standaloneFacts = &XmlStandaloneFact{Span: valueSpan, Value: token.standalone}
	}
	if strings.HasSuffix(text, "?>") {
		if close, err := p.rawSpanOffset(token.end-2, token.end); err != nil {
			return nil, err
		} else {
			p.pushPiece(close, XmlSyntaxKindDeclarationClose, document.PieceToken)
		}
	}
	return standaloneFacts, nil
}

// skipDeclarationSpaces skips XML declaration spaces forward
// (parser.rs:140-149).
func skipDeclarationSpaces(text string, cursor int) int {
	for cursor < len(text) {
		switch text[cursor] {
		case ' ', '\t', '\n', '\r':
			cursor++
		default:
			return cursor
		}
	}
	return cursor
}

// processingInstruction handles one PI token (parser.rs:505-579).
func (p *xmlParser) processingInstruction(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	targetRaw, failure := p.rawSpanOffset(token.targetStart, token.targetEnd)
	if failure != nil {
		return failure
	}
	if strings.EqualFold(p.decoded[token.targetStart:token.targetEnd], "xml") {
		p.recover("xml.pi.target@1", targetRaw, protocol.CategorySyntax)
	}
	var contentFacts *XmlPiContent
	if token.valueStart < token.valueEnd {
		valueRaw, failure := p.rawSpanOffset(token.valueStart, token.valueEnd)
		if failure != nil {
			return failure
		}
		value := p.decoded[token.valueStart:token.valueEnd]
		if len(value) > p.limits.MaxPiLength {
			return profileFailure("xml.limit.pi@1")
		}
		contentFacts = &XmlPiContent{Span: valueRaw, Text: value}
	}
	if p.dtdSubsetStart != nil {
		// A PI inside the internal subset is admitted DTD markup, never a
		// prolog/epilog or element-content occurrence.
		p.pushPiece(raw, XmlSyntaxKindDtdMarkup, document.PieceToken)
		return nil
	}
	open, failure := p.rawSpanOffset(token.start, token.start+2)
	if failure != nil {
		return failure
	}
	p.pushPiece(open, XmlSyntaxKindProcessingInstructionOpen, document.PieceToken)
	p.pushPiece(targetRaw, XmlSyntaxKindProcessingInstructionTarget, document.PieceToken)
	if contentFacts != nil {
		p.pushPiece(contentFacts.Span, XmlSyntaxKindProcessingInstructionContent, document.PieceToken)
	}
	close, failure := p.rawSpanOffset(token.end-2, token.end)
	if failure != nil {
		return failure
	}
	p.pushPiece(close, XmlSyntaxKindProcessingInstructionClose, document.PieceToken)
	item := XmlPiData{
		Ordinal:    p.ordinal(),
		Span:       raw,
		TargetSpan: targetRaw,
		Target:     p.decoded[token.targetStart:token.targetEnd],
		Content:    contentFacts,
	}
	if len(p.stack) == 0 {
		if p.root == nil {
			p.prolog = append(p.prolog, XmlPrologItem{
				Kind: XmlPrologItemProcessingInstruction, ProcessingInstruction: &item,
			})
		} else {
			p.epilog = append(p.epilog, XmlPrologItem{
				Kind: XmlPrologItemProcessingInstruction, ProcessingInstruction: &item,
			})
		}
	} else {
		p.pushContent(XmlContent{Kind: XmlContentProcessingInstruction, ProcessingInstruction: &item})
	}
	return nil
}

// comment handles one comment token (parser.rs:581-644).
func (p *xmlParser) comment(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	value := p.decoded[token.valueStart:token.valueEnd]
	if strings.Contains(value, "--") || strings.HasSuffix(value, "-") {
		textRaw, failure := p.rawSpanOffset(token.valueStart, token.valueEnd)
		if failure != nil {
			return failure
		}
		p.recover("xml.comment.content@1", textRaw, protocol.CategorySyntax)
	}
	if len(value) > p.limits.MaxCommentLength {
		return profileFailure("xml.limit.comment@1")
	}
	if p.dtdSubsetStart != nil {
		// A comment inside the internal subset is admitted DTD markup,
		// never a prolog/epilog or element-content occurrence.
		p.pushPiece(raw, XmlSyntaxKindDtdMarkup, document.PieceTrivia)
		return nil
	}
	open, failure := p.rawSpanOffset(token.start, token.start+4)
	if failure != nil {
		return failure
	}
	p.pushPiece(open, XmlSyntaxKindCommentOpen, document.PieceTrivia)
	textRaw, failure := p.rawSpanOffset(token.valueStart, token.valueEnd)
	if failure != nil {
		return failure
	}
	p.pushPiece(textRaw, XmlSyntaxKindCommentText, document.PieceTrivia)
	close, failure := p.rawSpanOffset(token.valueEnd, token.end)
	if failure != nil {
		return failure
	}
	p.pushPiece(close, XmlSyntaxKindCommentClose, document.PieceTrivia)
	item := XmlCommentData{
		Ordinal:  p.ordinal(),
		Span:     raw,
		TextSpan: textRaw,
		Text:     value,
	}
	if len(p.stack) == 0 {
		if p.root == nil {
			p.prolog = append(p.prolog, XmlPrologItem{Kind: XmlPrologItemComment, Comment: &item})
		} else {
			p.epilog = append(p.epilog, XmlPrologItem{Kind: XmlPrologItemComment, Comment: &item})
		}
	} else {
		p.pushContent(XmlContent{Kind: XmlContentComment, Comment: &item})
	}
	return nil
}

// doctypeStart handles `<!DOCTYPE name [ …` (parser.rs:646-667).
func (p *xmlParser) doctypeStart(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	if failure := p.pushDoctypeOpen(raw); failure != nil {
		return failure
	}
	if failure := p.doctypeCommon(token, raw); failure != nil {
		return failure
	}
	if token.dtdExternal {
		p.externalSubsetRecovered = true
		p.recover("xml.dtd.external-subset@1", raw, protocol.CategoryConformance)
	}
	start := raw.StartByte()
	p.doctypeSpanStart = &start
	subset := token.end
	p.dtdSubsetStart = &subset
	return nil
}

// doctypeEmpty handles `<!DOCTYPE name … >` without an internal subset
// (parser.rs:669-689).
func (p *xmlParser) doctypeEmpty(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	if failure := p.pushDoctypeOpen(raw); failure != nil {
		return failure
	}
	if failure := p.doctypeCommon(token, raw); failure != nil {
		return failure
	}
	if token.dtdExternal {
		p.externalSubsetRecovered = true
		p.recover("xml.dtd.external-subset@1", raw, protocol.CategoryConformance)
	}
	start := raw.StartByte()
	p.doctypeSpanStart = &start
	return p.buildDoctype(raw)
}

// buildDoctype assembles the immutable DOCTYPE facts once its end is known
// (parser.rs:692-711).
func (p *xmlParser) buildDoctype(end document.Span) *FormationFailure {
	if p.doctypeSpanStart == nil || p.doctypeName == nil {
		return profileFailure("xml.source.span@1")
	}
	span, err := p.authority.Span(*p.doctypeSpanStart, end.EndByte())
	if err != nil {
		return profileFailure("xml.source.span@1")
	}
	name := *p.doctypeName
	entities := append([]EntityDeclarationData(nil), p.entities...)
	p.doctype = &XmlDoctypeData{
		Span:      span,
		Name:      name,
		Entities:  entities,
		Recovered: p.externalSubsetRecovered,
	}
	return nil
}

// pushDoctypeOpen pushes the `<!DOCTYPE` opening piece for a DTD start
// span (parser.rs:714-718).
func (p *xmlParser) pushDoctypeOpen(raw document.Span) *FormationFailure {
	open, err := p.authority.Span(raw.StartByte(), raw.StartByte()+9)
	if err != nil {
		return profileFailure("xml.source.span@1")
	}
	p.pushPiece(open, XmlSyntaxKindDoctypeOpen, document.PieceToken)
	return nil
}

// doctypeCommon records the DOCTYPE name facts (parser.rs:720-745).
func (p *xmlParser) doctypeCommon(token *xmlToken, raw document.Span) *FormationFailure {
	if p.doctype != nil {
		p.recover("xml.dtd.multiple-doctype@1", raw, protocol.CategorySyntax)
	}
	qname, failure := p.qnameFactsFromSpans(token.localStart, token.localEnd)
	if failure != nil {
		return failure
	}
	if qname.Span.Len() > p.limits.MaxQNameLength {
		return profileFailure("xml.limit.qname@1")
	}
	p.pushPiece(qname.Span, XmlSyntaxKindDoctypeName, document.PieceToken)
	p.doctypeName = &qname
	return nil
}

// entityDeclaration handles one `<!ENTITY …>` token (parser.rs:747-849).
func (p *xmlParser) entityDeclaration(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	p.pushPiece(raw, XmlSyntaxKindDtdMarkup, document.PieceToken)
	text := p.decoded[token.start:token.end]
	// A parameter entity declaration is spelled `<!ENTITY % name ...`.
	isParameter := false
	if len(text) > 8 {
		for index := 8; index < len(text); index++ {
			if !isSpaceByte(text[index]) {
				isParameter = text[index] == '%'
				break
			}
		}
	}
	if isParameter {
		p.recover("xml.dtd.parameter-entity@1", raw, protocol.CategoryConformance)
		return nil
	}
	declaredName := p.decoded[token.localStart:token.localEnd]
	if !token.entityValue {
		p.recover("xml.dtd.external-entity@1", raw, protocol.CategoryConformance)
		return nil
	}
	valueText := p.decoded[token.valueStart:token.valueEnd]
	if len(valueText) > p.limits.MaxAttributeValueLength {
		return profileFailure("xml.limit.entity-replacement@1")
	}
	switch err := ValidateReplacementText(valueText); {
	case err != nil && err.Kind == ReplacementErrorContainsMarkup:
		p.recover(replacementCode(err), raw, protocol.CategoryConformance)
		return nil
	case err != nil:
		p.recover(replacementCode(err), raw, protocol.CategorySyntax)
		return nil
	}
	if strings.Contains(valueText, "%") {
		// A `%` inside an entity value is a parameter-entity reference,
		// which the Profile excludes.
		p.recover("xml.dtd.parameter-entity@1", raw, protocol.CategoryConformance)
		return nil
	}
	if _, predefined := PredefinedValue(declaredName); predefined || declaredName == "xml" || declaredName == "xmlns" {
		p.recover("xml.entity.reserved-name@1", raw, protocol.CategoryConformance)
		return nil
	}
	for _, entity := range p.entities {
		if entity.Name == declaredName {
			p.recover("xml.entity.duplicate@1", raw, protocol.CategorySyntax)
			return nil
		}
	}
	replacementRaw, failure := p.rawSpanOffset(token.valueStart, token.valueEnd)
	if failure != nil {
		return failure
	}
	declared := EntityDeclarationData{
		Span:            raw,
		Name:            declaredName,
		ReplacementSpan: replacementRaw,
		Replacement:     valueText,
	}
	if breach := p.entityState.RecordDeclaration(len(valueText), utf8.RuneCountInString(valueText),
		p.limits.EntityLimits()); breach != nil {
		p.entityLimit(breach, raw)
		return nil
	}
	p.entities = append(p.entities, declared)
	return nil
}

func isSpaceByte(byte byte) bool {
	return byte == ' ' || byte == '\t' || byte == '\n' || byte == '\r'
}

// dtdEnd handles the `]>` closing of the internal subset (parser.rs:851-867).
func (p *xmlParser) dtdEnd(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	p.pushPiece(raw, XmlSyntaxKindDoctypeClose, document.PieceToken)
	if p.dtdSubsetStart != nil {
		subset := p.decoded[*p.dtdSubsetStart:token.start]
		if failure := p.scanExcludedDtdMarkup(subset); failure != nil {
			return failure
		}
		if len(subset) > p.limits.MaxDtdBytes {
			return profileFailure("xml.limit.dtd@1")
		}
		p.dtdSubsetStart = nil
	}
	return p.buildDoctype(raw)
}

// scanExcludedDtdMarkup scans the internal subset raw text for excluded
// declarations; comments are skipped as a whole (parser.rs:869-911).
func (p *xmlParser) scanExcludedDtdMarkup(subset string) *FormationFailure {
	markers := []string{"<!ELEMENT", "<!ATTLIST", "<!NOTATION", "<!["}
	search := subset
	base := 0
	for {
		commentAt := strings.Index(search, "<!--")
		markerAt := -1
		marker := ""
		for _, candidate := range markers {
			if at := strings.Index(search, candidate); at >= 0 && (markerAt < 0 || at < markerAt) {
				markerAt = at
				marker = candidate
			}
		}
		if commentAt >= 0 && (markerAt < 0 || commentAt < markerAt) {
			relativeEnd := strings.Index(search[commentAt+4:], "-->")
			if relativeEnd < 0 {
				// An unterminated comment is already a tokenizer recovery
				// case; nothing further to scan.
				return nil
			}
			skip := commentAt + 4 + relativeEnd + 3
			base += skip
			search = search[skip:]
			continue
		}
		if markerAt < 0 {
			return nil
		}
		absolute := base + markerAt
		span, err := p.rawSpanOffset(absolute, absolute+len(marker))
		if err != nil {
			return err
		}
		code := "xml.dtd.validation-declaration@1"
		if marker == "<![" {
			code = "xml.dtd.conditional-section@1"
		}
		p.recover(code, span, protocol.CategoryConformance)
		next := markerAt + len(marker)
		base += next
		search = search[next:]
	}
}

// elementStart handles one `< name` token (parser.rs:913-961).
func (p *xmlParser) elementStart(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	tagOpen, failure := p.rawSpanOffset(token.start, token.start+1)
	if failure != nil {
		return failure
	}
	p.pushPiece(tagOpen, XmlSyntaxKindTagOpen, document.PieceToken)
	if failure := p.pushQNameParts(token); failure != nil {
		return failure
	}
	qname, failure := p.qnameFactsFromToken(token)
	if failure != nil {
		return failure
	}
	if qname.Span.Len() > p.limits.MaxQNameLength {
		return profileFailure("xml.limit.qname@1")
	}
	if len(p.nodes) >= p.limits.Common.MaxNodeCount {
		return profileFailure("xml.limit.node@1")
	}
	if len(p.nodes) >= p.limits.MaxElementCount {
		return profileFailure("xml.limit.element@1")
	}
	if len(p.stack) >= p.limits.Common.MaxNestingDepth {
		return profileFailure("xml.limit.depth@1")
	}
	// Element-name resolution is deferred to start-tag finalization so
	// that declarations on this very element are in scope.
	var scope NamespaceScope
	if len(p.stack) > 0 {
		scope = p.stack[len(p.stack)-1].scope
	}
	p.stack = append(p.stack, &xmlFrame{
		start: raw.StartByte(),
		span:  raw,
		qname: qname,
		scope: scope,
	})
	return nil
}

// attribute handles one attribute token (parser.rs:963-1063).
func (p *xmlParser) attribute(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	if len(p.stack) == 0 {
		return profileFailure("xml.syntax.attribute-outside-element@1")
	}
	frame := p.stack[len(p.stack)-1]
	declarationCount := len(frame.pendingDeclarations) + len(frame.namespaces)
	attributeCount := len(frame.pendingAttributes) + len(frame.attributes)
	if attributeCount >= p.limits.MaxAttributeCount ||
		declarationCount >= p.limits.MaxNamespaceDeclarationCount {
		return profileFailure("xml.limit.attribute@1")
	}
	qname, failure := p.qnameFactsFromToken(token)
	if failure != nil {
		return failure
	}
	isDeclaration := (qname.Prefix != nil && *qname.Prefix == "xmlns") ||
		(qname.Prefix == nil && qname.Local == "xmlns")
	if isDeclaration {
		p.pushPiece(qname.Span, XmlSyntaxKindNamespaceDeclaration, document.PieceToken)
	} else {
		p.pushPiece(qname.Span, XmlSyntaxKindAttributeName, document.PieceToken)
	}
	// `=` and the two quote characters are decoded-space offsets; the raw
	// span conversion keeps UTF-16 sources exact.
	between := p.decoded[token.localEnd:token.valueStart]
	if eq := strings.IndexByte(between, '='); eq >= 0 {
		rawEq, failure := p.rawSpanOffset(token.localEnd+eq, token.localEnd+eq+1)
		if failure != nil {
			return failure
		}
		p.pushPiece(rawEq, XmlSyntaxKindEquals, document.PieceToken)
	}
	quoteStart := token.valueStart - 1
	openQuote, failure := p.rawSpanOffset(quoteStart, quoteStart+1)
	if failure != nil {
		return failure
	}
	p.pushPiece(openQuote, XmlSyntaxKindQuote, document.PieceToken)
	singleQuote := p.decoded[quoteStart] == '\''
	closeQuote, failure := p.rawSpanOffset(token.valueEnd, token.valueEnd+1)
	if failure != nil {
		return failure
	}
	valueRaw, failure := p.rawSpanOffset(token.valueStart, token.valueEnd)
	if failure != nil {
		return failure
	}
	fragments, normalized := p.valueFragments(token)
	p.pushPiece(closeQuote, XmlSyntaxKindQuote, document.PieceToken)
	if isDeclaration {
		if len(normalized) > p.limits.MaxNamespaceURILength {
			return profileFailure("xml.limit.namespace-uri@1")
		}
		frame.pendingDeclarations = append(frame.pendingDeclarations, pendingDeclaration{
			qname:   qname,
			uri:     normalized,
			uriSpan: valueRaw,
		})
		return nil
	}
	if len(normalized) > p.limits.MaxAttributeValueLength {
		return profileFailure("xml.limit.attribute-value@1")
	}
	frame.pendingAttributes = append(frame.pendingAttributes, pendingAttribute{
		qname:       qname,
		span:        raw,
		valueSpan:   valueRaw,
		fragments:   fragments,
		normalized:  normalized,
		singleQuote: singleQuote,
	})
	return nil
}

// finalizeStartTag resolves element and attribute names once the whole
// start tag has been read (parser.rs:1065-1174).
func (p *xmlParser) finalizeStartTag() {
	if len(p.stack) == 0 {
		return
	}
	frame := p.stack[len(p.stack)-1]
	declarations := frame.pendingDeclarations
	attributes := frame.pendingAttributes
	frame.pendingDeclarations = nil
	frame.pendingAttributes = nil
	scope := frame.scope
	var namespaces []XmlNamespaceBindingData
	for _, declaration := range declarations {
		var prefix *string
		if declaration.qname.Prefix != nil && *declaration.qname.Prefix == "xmlns" {
			local := declaration.qname.Local
			prefix = &local
		}
		if childScope, err := scope.Declare(prefix, declaration.uri); err != nil {
			p.recover(namespaceCode(err), declaration.qname.Span, protocol.CategorySemantic)
		} else {
			scope = childScope
			namespaces = append(namespaces, XmlNamespaceBindingData{
				Ordinal: p.ordinal(),
				Span:    declaration.qname.Span,
				Prefix:  prefix,
				URISpan: declaration.uriSpan,
				URI:     declaration.uri,
			})
		}
	}
	var expanded *ExpandedName
	var namespaceError *NamespaceError
	if resolved, err := scope.ResolveElement(frame.qname.QName()); err != nil {
		namespaceError = err
		p.recover(namespaceCode(err), frame.qname.Span, protocol.CategorySemantic)
	} else {
		expanded = &resolved
	}
	var resolvedAttributes []XmlAttributeData
	for _, pending := range attributes {
		var attrExpanded *ExpandedName
		var attrError *NamespaceError
		if resolved, err := scope.ResolveAttribute(pending.qname.QName()); err != nil {
			attrError = err
			p.recover(namespaceCode(err), pending.qname.Span, protocol.CategorySemantic)
		} else {
			attrExpanded = &resolved
		}
		duplicate := false
		if attrExpanded != nil {
			for _, existing := range resolvedAttributes {
				if existing.Expanded != nil && existing.Expanded.Equal(*attrExpanded) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				for _, binding := range namespaces {
					declared := DeclarationExpandedName(binding.Prefix)
					if declared.Equal(*attrExpanded) {
						duplicate = true
						break
					}
				}
			}
		}
		if duplicate {
			p.recover("xml.namespace.duplicate-attribute@1", pending.qname.Span, protocol.CategorySemantic)
		}
		resolvedAttributes = append(resolvedAttributes, XmlAttributeData{
			Ordinal:         p.ordinal(),
			Span:            pending.span,
			QName:           pending.qname,
			Expanded:        attrExpanded,
			NamespaceError:  attrError,
			SingleQuote:     pending.singleQuote,
			ValueSpan:       pending.valueSpan,
			Fragments:       pending.fragments,
			NormalizedValue: pending.normalized,
		})
	}
	frame.scope = scope
	frame.namespaces = append(frame.namespaces, namespaces...)
	frame.expanded = expanded
	frame.namespaceError = namespaceError
	frame.attributes = append(frame.attributes, resolvedAttributes...)
}

// elementOpenEnd handles the `>` closing of a start tag
// (parser.rs:1176-1195).
func (p *xmlParser) elementOpenEnd(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	p.pushPiece(raw, XmlSyntaxKindTagClose, document.PieceToken)
	p.finalizeStartTag()
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		extended, err := p.authority.Span(frame.start, raw.EndByte())
		if err == nil {
			frame.span = extended
		}
	}
	return nil
}

// elementEmptyEnd handles the `/>` closing of an empty element
// (parser.rs:1196-1214).
func (p *xmlParser) elementEmptyEnd(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	p.pushPiece(raw, XmlSyntaxKindEmptyElementClose, document.PieceToken)
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		extended, err := p.authority.Span(frame.start, raw.EndByte())
		if err == nil {
			frame.span = extended
		}
	}
	p.finalizeStartTag()
	p.closeFrame(raw)
	return nil
}

// elementCloseEnd handles one `</ name >` end tag (parser.rs:1215-1248).
func (p *xmlParser) elementCloseEnd(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	endOpen, failure := p.rawSpanOffset(token.start, token.start+2)
	if failure != nil {
		return failure
	}
	p.pushPiece(endOpen, XmlSyntaxKindEndTagOpen, document.PieceToken)
	if failure := p.pushQNameParts(token); failure != nil {
		return failure
	}
	tagClose, failure := p.rawSpanOffset(token.end-1, token.end)
	if failure != nil {
		return failure
	}
	p.pushPiece(tagClose, XmlSyntaxKindTagClose, document.PieceToken)
	endQname, failure := p.qnameFactsFromToken(token)
	if failure != nil {
		return failure
	}
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		if !frame.qname.QName().Equal(endQname.QName()) {
			p.recover("xml.tree.mismatched-end-tag@1", endQname.Span, protocol.CategorySyntax)
		}
	}
	p.closeFrame(raw)
	return nil
}

// closeFrame closes one element frame and publishes its arena node
// (parser.rs:1250-1305).
func (p *xmlParser) closeFrame(endTagSpan document.Span) {
	if len(p.stack) == 0 {
		// An extra end tag cannot close any proven element; it is a
		// recovery case at a deterministic markup boundary.
		p.recover("xml.tree.extra-end-tag@1", endTagSpan, protocol.CategorySyntax)
		return
	}
	frame := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	index := len(p.nodes)
	element := XmlElementData{
		Index:          index,
		Span:           frame.span,
		QName:          frame.qname,
		Expanded:       frame.expanded,
		NamespaceError: frame.namespaceError,
		Scope:          frame.scope,
		Namespaces:     frame.namespaces,
		Attributes:     frame.attributes,
		Children:       frame.children,
	}
	// Every child content item attached to this element now knows its
	// parent arena index.
	for _, child := range element.Children {
		p.parentOf[child] = intPtr(index)
	}
	p.parentOf = append(p.parentOf, nil)
	p.nodes = append(p.nodes, XmlContent{Kind: XmlContentElement, Element: &element})
	if len(p.stack) > 0 {
		parent := p.stack[len(p.stack)-1]
		if len(parent.children) >= p.limits.MaxMixedContentItems {
			// Child elements respect the same hard mixed-content budget as
			// text/CDATA/comment/PI; dropping publishes a diagnostic and
			// never passes silently.
			p.recover("xml.limit.mixed-content@1", p.nodes[index].Span(), protocol.CategoryConformance)
		} else {
			parent.children = append(parent.children, index)
		}
	} else if p.root == nil {
		root := index
		p.root = &root
	} else {
		p.recover("xml.tree.multiple-roots@1", p.nodes[index].Span(), protocol.CategorySyntax)
	}
}

// text handles one text token (parser.rs:1307-1369).
func (p *xmlParser) text(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	value := p.decoded[token.start:token.end]
	whitespaceOnly := true
	for _, c := range value {
		if !isSpaceByte(byte(c)) {
			whitespaceOnly = false
			break
		}
	}
	if len(p.stack) == 0 {
		if whitespaceOnly {
			if failure := p.pushWhitespacePieces(token); failure != nil {
				return failure
			}
			item := XmlPrologItem{Kind: XmlPrologItemWhitespace, Span: raw}
			if p.root == nil {
				p.prolog = append(p.prolog, item)
			} else {
				p.epilog = append(p.epilog, item)
			}
			return nil
		}
		// Non-whitespace character data outside the document element is
		// recovered; the piece is an error region and the literal text is
		// still preserved as an orphan text occurrence.
		p.recover("xml.syntax.text-outside-root@1", raw, protocol.CategorySyntax)
		p.pushPiece(raw, XmlSyntaxKindErrorRegion, document.PieceErrorRegion)
		item := XmlTextData{
			Ordinal: p.ordinal(),
			Span:    raw,
			Fragments: []ReferenceFragment{{
				Kind: ReferenceFragmentLiteral, Span: raw, Text: value,
			}},
		}
		p.pushContent(XmlContent{Kind: XmlContentText, Text: &item})
		return nil
	}
	if whitespaceOnly {
		if failure := p.pushWhitespacePieces(token); failure != nil {
			return failure
		}
	} else {
		fragments, failure := p.textFragments(token, XmlSyntaxKindText)
		if failure != nil {
			return failure
		}
		if len(value) > p.limits.MaxTextLength {
			return profileFailure("xml.limit.text@1")
		}
		item := XmlTextData{Ordinal: p.ordinal(), Span: raw, Fragments: fragments}
		p.pushContent(XmlContent{Kind: XmlContentText, Text: &item})
		return nil
	}
	item := XmlTextData{
		Ordinal: p.ordinal(),
		Span:    raw,
		Fragments: []ReferenceFragment{{
			Kind: ReferenceFragmentLiteral, Span: raw, Text: value,
		}},
	}
	p.pushContent(XmlContent{Kind: XmlContentText, Text: &item})
	return nil
}

// cdata handles one CDATA token (parser.rs:1371-1401).
func (p *xmlParser) cdata(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	open, failure := p.rawSpanOffset(token.start, token.start+9)
	if failure != nil {
		return failure
	}
	p.pushPiece(open, XmlSyntaxKindCdataOpen, document.PieceToken)
	textRaw, failure := p.rawSpanOffset(token.valueStart, token.valueEnd)
	if failure != nil {
		return failure
	}
	p.pushPiece(textRaw, XmlSyntaxKindCdataText, document.PieceToken)
	close, failure := p.rawSpanOffset(token.valueEnd, token.end)
	if failure != nil {
		return failure
	}
	p.pushPiece(close, XmlSyntaxKindCdataClose, document.PieceToken)
	value := p.decoded[token.valueStart:token.valueEnd]
	if len(value) > p.limits.MaxCdataLength {
		return profileFailure("xml.limit.cdata@1")
	}
	item := XmlCdataData{
		Ordinal:  p.ordinal(),
		Span:     raw,
		TextSpan: textRaw,
		Text:     value,
	}
	p.pushContent(XmlContent{Kind: XmlContentCdata, Cdata: &item})
	return nil
}

// pushContent attaches one content occurrence to the current element
// (parser.rs:1403-1422).
func (p *xmlParser) pushContent(item XmlContent) {
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		if len(frame.children) >= p.limits.MaxMixedContentItems {
			// The item is dropped under the hard budget, never silently:
			// recovery always publishes a diagnostic and the source bytes
			// stay covered by their structural piece.
			p.recover("xml.limit.mixed-content@1", item.Span(), protocol.CategoryConformance)
			return
		}
		frame.children = append(frame.children, len(p.nodes))
	}
	// The parent table stays index-parallel with the node arena; the
	// owning element fills the entry when it closes.
	p.parentOf = append(p.parentOf, nil)
	p.nodes = append(p.nodes, item)
}

// pushWhitespacePieces splits one whitespace-only text run into
// Whitespace and LineBreak pieces; CRLF counts as one line break
// (parser.rs:1424-1458).
func (p *xmlParser) pushWhitespacePieces(token *xmlToken) *FormationFailure {
	bytes := []byte(p.decoded[token.start:token.end])
	index := 0
	for index < len(bytes) {
		lineBreak := bytes[index] == '\n' || bytes[index] == '\r'
		runStart := index
		if lineBreak {
			if bytes[index] == '\r' && index+1 < len(bytes) && bytes[index+1] == '\n' {
				index += 2
			} else {
				index++
			}
		} else {
			index++
		}
		for index < len(bytes) && (bytes[index] == '\n' || bytes[index] == '\r') == lineBreak {
			index++
		}
		span, err := p.rawSpanOffset(token.start+runStart, token.start+index)
		if err != nil {
			return err
		}
		kind := XmlSyntaxKindWhitespace
		if lineBreak {
			kind = XmlSyntaxKindLineBreak
		}
		p.pushPiece(span, kind, document.PieceTrivia)
	}
	return nil
}

// textFragments splits one text or attribute-value occurrence into
// reference fragments (parser.rs:1460-1555).
func (p *xmlParser) textFragments(token *xmlToken, literalKind XmlSyntaxKind) ([]ReferenceFragment, *FormationFailure) {
	bytes := p.decoded[token.valueStart:token.valueEnd]
	if !strings.Contains(bytes, "&") {
		// Fast path: a single literal covers the whole run.
		span, err := p.rawSpanOffset(token.valueStart, token.valueEnd)
		if err != nil {
			return nil, err
		}
		p.pushPiece(span, literalKind, document.PieceToken)
		return []ReferenceFragment{{Kind: ReferenceFragmentLiteral, Span: span, Text: bytes}}, nil
	}
	var fragments []ReferenceFragment
	cursor := 0
	index := 0
	for index < len(bytes) {
		relative := strings.IndexByte(bytes[index:], '&')
		if relative < 0 {
			break
		}
		at := index + relative
		if at > cursor {
			literal := bytes[cursor:at]
			span, err := p.rawSpanOffset(token.valueStart+cursor, token.valueStart+at)
			if err != nil {
				return nil, err
			}
			p.pushPiece(span, literalKind, document.PieceToken)
			fragments = append(fragments, ReferenceFragment{
				Kind: ReferenceFragmentLiteral, Span: span, Text: literal,
			})
		}
		semi := strings.IndexByte(bytes[at+1:], ';')
		if semi < 0 {
			// Unterminated reference: recover and keep the rest literal.
			span, err := p.rawSpanOffset(token.valueStart+at, token.valueEnd)
			if err != nil {
				return nil, err
			}
			p.recover("xml.reference.malformed@1", span, protocol.CategorySyntax)
			p.pushPiece(span, literalKind, document.PieceToken)
			fragments = append(fragments, ReferenceFragment{
				Kind: ReferenceFragmentLiteral, Span: span, Text: bytes[at:],
			})
			cursor = len(bytes)
			index = len(bytes)
			continue
		}
		semi += at + 1
		body := bytes[at+1 : semi]
		refSpan, err := p.rawSpanOffset(token.valueStart+at, token.valueStart+semi+1)
		if err != nil {
			return nil, err
		}
		if fragment := p.resolveReference(body, refSpan, 0); fragment != nil {
			kind := literalKind
			switch fragment.Kind {
			case ReferenceFragmentCharacterReference:
				kind = XmlSyntaxKindCharacterReference
			case ReferenceFragmentPredefinedEntity, ReferenceFragmentGeneralEntity:
				kind = XmlSyntaxKindEntityReference
			}
			p.pushPiece(refSpan, kind, document.PieceToken)
			fragments = append(fragments, *fragment)
		}
		cursor = semi + 1
		index = semi + 1
	}
	if cursor < len(bytes) {
		literal := bytes[cursor:]
		span, err := p.rawSpanOffset(token.valueStart+cursor, token.valueEnd)
		if err != nil {
			return nil, err
		}
		p.pushPiece(span, literalKind, document.PieceToken)
		fragments = append(fragments, ReferenceFragment{
			Kind: ReferenceFragmentLiteral, Span: span, Text: literal,
		})
	}
	return fragments, nil
}

// resolveReference resolves one `&…;` reference body into a fragment
// (parser.rs:1557-1645).
func (p *xmlParser) resolveReference(body string, refSpan document.Span, depth int) *ReferenceFragment {
	if strings.HasPrefix(body, "#") {
		digits := body[1:]
		var value uint64
		parsed := false
		if strings.HasPrefix(digits, "x") || strings.HasPrefix(digits, "X") {
			parsed = parseRadix(digits[1:], 16, &value)
		} else {
			parsed = parseRadix(digits, 10, &value)
		}
		if parsed && IsXMLChar(rune(value)) {
			return &ReferenceFragment{
				Kind:         ReferenceFragmentCharacterReference,
				Span:         refSpan,
				ResolvedChar: rune(value),
			}
		}
		p.recover("xml.reference.invalid-character@1", refSpan, protocol.CategorySyntax)
		return nil
	}
	if value, ok := PredefinedValue(body); ok {
		return &ReferenceFragment{
			Kind:     ReferenceFragmentPredefinedEntity,
			Span:     refSpan,
			Name:     body,
			Resolved: value,
		}
	}
	declared := p.findEntity(body)
	if declared == nil {
		p.recover("xml.entity.unknown@1", refSpan, protocol.CategoryConformance)
		return nil
	}
	limits := p.limits.EntityLimits()
	if breach := p.entityState.EnterReference(len(declared.Replacement),
		utf8.RuneCountInString(declared.Replacement), limits); breach != nil {
		p.entityLimit(breach, refSpan)
		return nil
	}
	nested := p.resolveNested(declared.Replacement, refSpan, depth+1)
	p.entityState.LeaveReference()
	if nested == nil {
		p.recover("xml.entity.cyclic@1", refSpan, protocol.CategoryConformance)
		return nil
	}
	return &ReferenceFragment{
		Kind:            ReferenceFragmentGeneralEntity,
		Span:            refSpan,
		Name:            body,
		Resolved:        *nested,
		DeclarationSpan: declared.Span,
	}
}

// resolveNested resolves nested references inside one replacement text;
// unknown references, cycles, or limit breaches inside replacement text
// produce no partial native text (parser.rs:1647-1692).
func (p *xmlParser) resolveNested(replacement string, sourceSpan document.Span, depth int) *string {
	if depth > p.limits.MaxEntityExpansionDepth {
		return nil
	}
	var out strings.Builder
	cursor := 0
	index := 0
	for index < len(replacement) {
		relative := strings.IndexByte(replacement[index:], '&')
		if relative < 0 {
			break
		}
		at := index + relative
		out.WriteString(replacement[cursor:at])
		semi := strings.IndexByte(replacement[at+1:], ';')
		if semi < 0 {
			return nil
		}
		semi += at + 1
		body := replacement[at+1 : semi]
		fragment := p.resolveReference(body, sourceSpan, depth)
		if fragment == nil {
			return nil
		}
		switch fragment.Kind {
		case ReferenceFragmentCharacterReference:
			out.WriteRune(fragment.ResolvedChar)
		case ReferenceFragmentPredefinedEntity, ReferenceFragmentGeneralEntity:
			out.WriteString(fragment.Resolved)
		case ReferenceFragmentLiteral:
			out.WriteString(fragment.Text)
		}
		cursor = semi + 1
		index = semi + 1
	}
	out.WriteString(replacement[cursor:])
	text := out.String()
	return &text
}

// valueFragments splits an attribute value into fragments and applies XML
// 1.0 CDATA normalization to the semantic value (parser.rs:1694-1729).
func (p *xmlParser) valueFragments(token *xmlToken) ([]ReferenceFragment, string) {
	fragments, _ := p.textFragments(token, XmlSyntaxKindAttributeValue)
	var normalized strings.Builder
	for _, fragment := range fragments {
		switch fragment.Kind {
		case ReferenceFragmentLiteral:
			for _, c := range fragment.Text {
				if c == '\t' || c == '\n' || c == '\r' || c == ' ' {
					normalized.WriteRune(' ')
				} else {
					normalized.WriteRune(c)
				}
			}
		case ReferenceFragmentCharacterReference:
			normalized.WriteRune(fragment.ResolvedChar)
		case ReferenceFragmentPredefinedEntity, ReferenceFragmentGeneralEntity:
			for _, c := range fragment.Resolved {
				if c == '\t' || c == '\n' || c == '\r' || c == ' ' {
					normalized.WriteRune(' ')
				} else {
					normalized.WriteRune(c)
				}
			}
		}
	}
	return fragments, normalized.String()
}

// recover records a recovery diagnostic with its exact failing span
// (parser.rs:1731-1749).
func (p *xmlParser) recover(code string, span document.Span, category protocol.DiagnosticCategory) {
	p.recovered = true
	if p.errorRegions >= p.limits.MaxRecoveryRegions {
		return
	}
	p.errorRegions++
	p.diagnostics = append(p.diagnostics, &protocol.Diagnostic{
		Code:       code,
		Category:   category,
		Severity:   protocol.SeverityError,
		Primary:    p.diagnosticLocation(span),
		Arguments:  map[string]string{},
		Occurrence: uint64(len(p.diagnostics)),
	})
}

// entityLimit maps one expansion breach to its recovery diagnostic
// (parser.rs:1751-1757).
func (p *xmlParser) entityLimit(breach *ExpansionBreach, span document.Span) {
	code := "xml.entity.limit@1"
	if breach.Kind == ExpansionBreachAmplification {
		code = "xml.entity.amplification@1"
	}
	p.recover(code, span, protocol.CategoryConformance)
}

// recoverErrorRegion covers one tokenizer error region and publishes the
// well-formedness diagnostic (parser.rs:1759-1786). The error position is
// the end of the document: a tokenizer error jumps the stream to the end
// (xmlparser Stream::jump_to_end), so the region is the final byte.
func (p *xmlParser) recoverErrorRegion(start, end int) *FormationFailure {
	p.recovered = true
	if p.errorRegions >= p.limits.MaxRecoveryRegions {
		return nil
	}
	if end <= start || start < 0 || end > len(p.decoded) {
		return nil
	}
	p.errorRegions++
	span, failure := p.rawSpanOffset(start, end)
	if failure != nil {
		return failure
	}
	p.pushPiece(span, XmlSyntaxKindErrorRegion, document.PieceErrorRegion)
	p.diagnostics = append(p.diagnostics, &protocol.Diagnostic{
		Code:       "xml.syntax.well-formedness@1",
		Category:   protocol.CategorySyntax,
		Severity:   protocol.SeverityError,
		Primary:    p.diagnosticLocation(span),
		Arguments:  map[string]string{},
		Occurrence: uint64(len(p.diagnostics)),
	})
	return nil
}

// finish completes formation and assembles the immutable document
// (parser.rs:1792-1914).
func (p *xmlParser) finish() (*Document, *FormationFailure) {
	if len(p.stack) > 0 {
		p.recovered = true
		p.diagnostics = append(p.diagnostics, &protocol.Diagnostic{
			Code:       "xml.tree.unclosed-element@1",
			Category:   protocol.CategorySyntax,
			Severity:   protocol.SeverityError,
			Arguments:  map[string]string{},
			Occurrence: uint64(len(p.diagnostics)),
		})
	}
	if p.root == nil {
		p.recovered = true
		p.diagnostics = append(p.diagnostics, &protocol.Diagnostic{
			Code:       "xml.tree.missing-root@1",
			Category:   protocol.CategorySyntax,
			Severity:   protocol.SeverityError,
			Arguments:  map[string]string{},
			Occurrence: uint64(len(p.diagnostics)),
		})
	}
	if p.root != nil && p.doctypeName != nil {
		content := &p.nodes[*p.root]
		if content.Kind == XmlContentElement && !content.Element.QName.QName().Equal(p.doctypeName.QName()) {
			p.recover("xml.doctype.root-mismatch@1", content.Element.QName.Span, protocol.CategorySyntax)
		}
	}
	status := document.FormationStatusComplete
	if p.recovered {
		status = document.FormationStatusRecovered
	}
	// Pair every piece with its kind before any ordering, so sorting can
	// never desynchronize the two parallel arrays.
	type pairedPiece struct {
		piece document.StructuralPiece
		kind  XmlSyntaxKind
	}
	paired := make([]pairedPiece, 0, len(p.pieces))
	for index := range p.pieces {
		paired = append(paired, pairedPiece{piece: p.pieces[index], kind: p.syntaxKinds[index]})
	}
	// The piece arrays are consumed; gap pieces pushed below land in the
	// freshly emptied arrays and are appended once.
	p.pieces = nil
	p.syntaxKinds = nil
	sort.Slice(paired, func(i, j int) bool {
		return paired[i].piece.Span().StartByte() < paired[j].piece.Span().StartByte()
	})
	var finalPieces []document.StructuralPiece
	var finalKinds []XmlSyntaxKind
	next := 0
	for _, item := range paired {
		start := item.piece.Span().StartByte()
		if start > next {
			gap, err := p.authority.Span(next, start)
			if err != nil {
				return nil, profileFailure("xml.source.span@1")
			}
			// In a Complete document the tokenizer only skips whitespace;
			// in a Recovered document the gap is unproven content.
			if p.recovered {
				p.pushPiece(gap, XmlSyntaxKindErrorRegion, document.PieceErrorRegion)
			} else {
				p.pushPiece(gap, XmlSyntaxKindWhitespace, document.PieceTrivia)
			}
		}
		next = item.piece.Span().EndByte()
		finalPieces = append(finalPieces, item.piece)
		finalKinds = append(finalKinds, item.kind)
	}
	sourceLen := p.source.Len()
	if next < sourceLen {
		gap, err := p.authority.Span(next, sourceLen)
		if err != nil {
			return nil, profileFailure("xml.source.span@1")
		}
		if p.recovered {
			p.pushPiece(gap, XmlSyntaxKindErrorRegion, document.PieceErrorRegion)
		} else {
			p.pushPiece(gap, XmlSyntaxKindWhitespace, document.PieceTrivia)
		}
	}
	// Gap pieces were pushed in increasing offset order; append them to
	// the final arrays, then pair and sort the complete set once.
	for index := range p.pieces {
		finalPieces = append(finalPieces, p.pieces[index])
		finalKinds = append(finalKinds, p.syntaxKinds[index])
	}
	paired = paired[:0]
	for index := range finalPieces {
		paired = append(paired, pairedPiece{piece: finalPieces[index], kind: finalKinds[index]})
	}
	sort.Slice(paired, func(i, j int) bool {
		return paired[i].piece.Span().StartByte() < paired[j].piece.Span().StartByte()
	})
	structural := make([]document.StructuralPiece, 0, len(paired))
	kinds := make([]XmlSyntaxKind, 0, len(paired))
	for _, item := range paired {
		structural = append(structural, item.piece)
		kinds = append(kinds, item.kind)
	}
	index, err := document.NewLosslessStructuralIndex(p.authority.Identity(), sourceLen, structural)
	if err != nil {
		return nil, profileFailure("xml.source.coverage@1")
	}
	sortDiagnostics(p.diagnostics)
	return &Document{
		authority:   p.authority,
		source:      p.source,
		status:      status,
		declaration: p.declaration,
		doctype:     p.doctype,
		prolog:      p.prolog,
		root:        p.root,
		epilog:      p.epilog,
		syntax:      index,
		syntaxKinds: kinds,
		diagnostics: p.diagnostics,
		nodes:       p.nodes,
		parentOf:    p.parentOf,
		parseLimits: p.limits,
	}, nil
}

// qnameFactsFromSpans builds QName facts from one name span.
func (p *xmlParser) qnameFactsFromSpans(nameStart, nameEnd int) (QNameFacts, *FormationFailure) {
	text := p.decoded[nameStart:nameEnd]
	raw, failure := p.rawSpanOffset(nameStart, nameEnd)
	if failure != nil {
		return QNameFacts{}, failure
	}
	colon := strings.IndexByte(text, ':')
	if colon < 0 {
		return QNameFacts{
			Local:     text,
			Span:      raw,
			LocalSpan: raw,
		}, nil
	}
	prefix := text[:colon]
	local := text[colon+1:]
	prefixSpan, failure := p.rawSpanOffset(nameStart, nameStart+colon)
	if failure != nil {
		return QNameFacts{}, failure
	}
	localSpan, failure := p.rawSpanOffset(nameStart+colon+1, nameEnd)
	if failure != nil {
		return QNameFacts{}, failure
	}
	return QNameFacts{
		Prefix:     &prefix,
		Local:      local,
		Span:       raw,
		PrefixSpan: &prefixSpan,
		LocalSpan:  localSpan,
	}, nil
}

// qnameFactsFromToken builds QName facts from one name-bearing token.
func (p *xmlParser) qnameFactsFromToken(token *xmlToken) (QNameFacts, *FormationFailure) {
	hasPrefix := token.isPrefix()
	start := token.localStart
	if hasPrefix {
		start = token.prefixStart
	}
	span, failure := p.rawSpanOffset(start, token.localEnd)
	if failure != nil {
		return QNameFacts{}, failure
	}
	local := p.decoded[token.localStart:token.localEnd]
	facts := QNameFacts{
		Local:     local,
		Span:      span,
		LocalSpan: span,
	}
	if hasPrefix {
		prefix := p.decoded[token.prefixStart:token.prefixEnd]
		prefixSpan, failure := p.rawSpanOffset(token.prefixStart, token.prefixEnd)
		if failure != nil {
			return QNameFacts{}, failure
		}
		localSpan, failure := p.rawSpanOffset(token.localStart, token.localEnd)
		if failure != nil {
			return QNameFacts{}, failure
		}
		facts.Prefix = &prefix
		facts.PrefixSpan = &prefixSpan
		facts.LocalSpan = localSpan
	}
	return facts, nil
}

// pushQNameParts pushes the QName part pieces for one element or end-tag
// name (parser.rs:1944-1976).
func (p *xmlParser) pushQNameParts(token *xmlToken) *FormationFailure {
	if !token.isPrefix() {
		localRaw, failure := p.rawSpanOffset(token.localStart, token.localEnd)
		if failure != nil {
			return failure
		}
		p.pushPiece(localRaw, XmlSyntaxKindLocalName, document.PieceToken)
		return nil
	}
	prefixRaw, failure := p.rawSpanOffset(token.prefixStart, token.prefixEnd)
	if failure != nil {
		return failure
	}
	colon, failure := p.rawSpanOffset(token.prefixEnd, token.localStart)
	if failure != nil {
		return failure
	}
	localRaw, failure := p.rawSpanOffset(token.localStart, token.localEnd)
	if failure != nil {
		return failure
	}
	p.pushPiece(prefixRaw, XmlSyntaxKindPrefix, document.PieceToken)
	p.pushPiece(colon, XmlSyntaxKindColon, document.PieceToken)
	p.pushPiece(localRaw, XmlSyntaxKindLocalName, document.PieceToken)
	return nil
}

// ordinal issues one document-wide occurrence ordinal (parser.rs:2009-2013).
func (p *xmlParser) ordinal() uint64 {
	ordinal := p.nextOrdinal
	p.nextOrdinal++
	return ordinal
}

// rawSpan converts one decoded span to a raw span.
func (p *xmlParser) rawSpan(start, end int) (document.Span, *FormationFailure) {
	span, failure := p.rawSpanOffset(start, end)
	if failure != nil {
		return document.Span{}, failure
	}
	return span, nil
}

// rawOffset converts one decoded UTF-8 byte offset to a raw source byte
// offset (parser.rs:2022-2052).
func (p *xmlParser) rawOffset(decoded int) (int, *FormationFailure) {
	if p.source.EncodingFacts().Selected().Equal(document.Utf8Encoding()) {
		return decoded, nil
	}
	raw, err := p.source.RawByteAt(document.NewUtf8ByteOffset(decoded))
	if err != nil {
		return 0, profileFailure("xml.source.span@1")
	}
	return raw, nil
}

// rawSpanOffset converts one decoded byte range to an exact raw span
// (parser.rs:2054-2062).
func (p *xmlParser) rawSpanOffset(start, end int) (document.Span, *FormationFailure) {
	startRaw, failure := p.rawOffset(start)
	if failure != nil {
		return document.Span{}, failure
	}
	endRaw, failure := p.rawOffset(end)
	if failure != nil {
		return document.Span{}, failure
	}
	span, err := p.authority.Span(startRaw, endRaw)
	if err != nil {
		return document.Span{}, profileFailure("xml.source.span@1")
	}
	return span, nil
}

// pushPiece records one structural piece with its format kind
// (parser.rs:2070-2073).
func (p *xmlParser) pushPiece(span document.Span, kind XmlSyntaxKind, structural document.StructuralPieceKind) {
	p.pieces = append(p.pieces, document.NewStructuralPiece(span, structural))
	p.syntaxKinds = append(p.syntaxKinds, kind)
}

// diagnosticLocation maps one span to the transferable location record;
// the source identity is the process-local snapshot identity.
func (p *xmlParser) diagnosticLocation(span document.Span) *protocol.SourceLocation {
	start := span.StartByte()
	end := span.EndByte()
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	location, err := protocol.NewSourceLocation(
		strconv.FormatUint(p.authority.Identity().AsU64(), 10), uint64(start), uint64(end))
	if err != nil {
		return nil
	}
	return location
}

// namespaceCode maps one namespace error to its stable diagnostic code
// (parser.rs:130-137).
func namespaceCode(err *NamespaceError) string {
	switch err.Kind {
	case NamespaceErrorUnboundPrefix:
		return "xml.namespace.unbound-prefix@1"
	case NamespaceErrorReservedPrefix:
		return "xml.namespace.reserved-prefix@1"
	case NamespaceErrorIllegalXmlRebinding:
		return "xml.namespace.xml-rebinding@1"
	case NamespaceErrorIllegalDefaultXmlns:
		return "xml.namespace.default-xmlns@1"
	}
	return "xml.namespace.unbound-prefix@1"
}

// findEntity locates one admitted entity declaration by name.
func (p *xmlParser) findEntity(name string) *EntityDeclarationData {
	for index := range p.entities {
		if p.entities[index].Name == name {
			return &p.entities[index]
		}
	}
	return nil
}

// sortDiagnostics orders diagnostics deterministically: primary start
// byte (absent last), category, code, occurrence (consema-core
// diagnostic.rs:107-123).
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

// parseRadix parses one reference body in the given radix, bounded by the
// XML character range.
func parseRadix(text string, radix int, out *uint64) bool {
	if text == "" {
		return false
	}
	value := uint64(0)
	for _, c := range text {
		digit := -1
		switch {
		case c >= '0' && c <= '9':
			digit = int(c - '0')
		case c >= 'a' && c <= 'f':
			digit = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			digit = int(c-'A') + 10
		}
		if digit < 0 || digit >= radix {
			return false
		}
		value = value*uint64(radix) + uint64(digit)
		if value > 0x10FFFF {
			return false
		}
	}
	*out = value
	return true
}

func intPtr(value int) *int { return &value }
