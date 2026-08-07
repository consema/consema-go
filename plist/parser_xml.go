package plist

// This file implements `plist.xml@1` formation (consema-plist parser_xml.rs;
// RFC 0013 §2.1, §3, §4, §8.2, §12). Formation is one deterministic forward
// pass over the decoded text with a lossless tokenizer mirroring the frozen
// xmlparser token stream (RFC 0013 §13); Consema owns every plist rule: the
// strict DOCTYPE contract, the `<plist version="1.0">` root contract, the
// value grammar, dictionary association rules, recovery, and every resource
// limit.
//
// Recovery follows RFC 0013 §3: non-fatal deviations form a Recovered
// document that retains the immutable source, exhaustive piece coverage,
// ordered diagnostics, and every independently proven construct. A value
// element either proves (its whole subtree proves) or contributes no native
// value, and never is a closing tag or value invented to fabricate a
// Complete tree. The native document exists exactly when the root value is
// provable.
//
// The lossless syntax index partitions every raw byte into exactly one
// piece (RFC 0013 §8.2); the root open tag `<plist version="1.0">`
// partitions as PlistOpen, Whitespace, PlistVersionName, PlistVersionValue,
// PlistOpen.

import (
	"strconv"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Byte lengths of the fixed markup openings (RFC 0013 §4).
const (
	doctypeOpenBytes     = 9 // `<!DOCTYPE`
	declarationOpenBytes = 5 // `<?xml`
	cdataOpenBytes       = 9 // `<![CDATA[`
	commentOpenBytes     = 4 // `<!--`
)

// The exact Apple plist DOCTYPE identifiers (RFC 0013 §4.1).
const (
	plistDoctypePublic = "-//Apple//DTD PLIST 1.0//EN"
	plistDoctypeSystem = "http://www.apple.com/DTDs/PropertyList-1.0.dtd"
)

// The exact root version value (RFC 0013 §4.2).
const plistVersion = "1.0"

// elementKind is the plist element vocabulary (RFC 0013 §4.3).
type elementKind uint8

const (
	elementPlist elementKind = iota
	elementDict
	elementArray
	elementString
	elementKey
	elementInteger
	elementReal
	elementTrue
	elementFalse
	elementData
	elementDate
)

func (k elementKind) isScalar() bool {
	switch k {
	case elementString, elementKey, elementInteger, elementReal,
		elementTrue, elementFalse, elementData, elementDate:
		return true
	}
	return false
}

func (k elementKind) openKind() PlistSyntaxKind {
	switch k {
	case elementPlist:
		return PlistSyntaxKindPlistOpen
	case elementDict:
		return PlistSyntaxKindDictOpen
	case elementArray:
		return PlistSyntaxKindArrayOpen
	case elementString:
		return PlistSyntaxKindStringOpen
	case elementKey:
		return PlistSyntaxKindKeyOpen
	case elementInteger:
		return PlistSyntaxKindIntegerOpen
	case elementReal:
		return PlistSyntaxKindRealOpen
	case elementTrue:
		return PlistSyntaxKindTrue
	case elementFalse:
		return PlistSyntaxKindFalse
	case elementData:
		return PlistSyntaxKindDataOpen
	case elementDate:
		return PlistSyntaxKindDateOpen
	}
	return PlistSyntaxKindErrorRegion
}

func (k elementKind) closeKind() PlistSyntaxKind {
	switch k {
	case elementPlist:
		return PlistSyntaxKindPlistClose
	case elementDict:
		return PlistSyntaxKindDictClose
	case elementArray:
		return PlistSyntaxKindArrayClose
	case elementString:
		return PlistSyntaxKindStringClose
	case elementKey:
		return PlistSyntaxKindKeyClose
	case elementInteger:
		return PlistSyntaxKindIntegerClose
	case elementReal:
		return PlistSyntaxKindRealClose
	case elementTrue:
		return PlistSyntaxKindTrue
	case elementFalse:
		return PlistSyntaxKindFalse
	case elementData:
		return PlistSyntaxKindDataClose
	case elementDate:
		return PlistSyntaxKindDateClose
	}
	return PlistSyntaxKindErrorRegion
}

// classifyElement resolves an unqualified element name; nil is unknown or
// prefixed (RFC 0013 §4.3).
func classifyElement(prefix, local string) *elementKind {
	if prefix != "" {
		return nil
	}
	var kind elementKind
	switch local {
	case "plist":
		kind = elementPlist
	case "dict":
		kind = elementDict
	case "array":
		kind = elementArray
	case "string":
		kind = elementString
	case "key":
		kind = elementKey
	case "integer":
		kind = elementInteger
	case "real":
		kind = elementReal
	case "true":
		kind = elementTrue
	case "false":
		kind = elementFalse
	case "data":
		kind = elementData
	case "date":
		kind = elementDate
	default:
		return nil
	}
	return &kind
}

// dictState is the ordered association state of one open `<dict>`
// (RFC 0013 §4.4).
type dictState struct {
	entries     []PlistDictEntry
	groups      map[string]int
	pendingKey  *PlistKey
	expectValue bool
}

// frameValue is the native value accumulation of one open frame.
type frameValueKind uint8

const (
	frameNone frameValueKind = iota
	frameRoot
	frameDict
	frameArray
)

type frameValue struct {
	kind     frameValueKind
	dict     *dictState
	elements []PlistValueRef
}

// xmlFrame is one open element frame: XML tree facts and native value
// accumulation.
type xmlFrame struct {
	kind                *elementKind
	name                string
	openStart           int
	openEnd             int
	tagCursor           int
	unknownSubtreeStart *int
	valueAllowed        bool
	value               frameValue
	content             strings.Builder
	scalarUnproven      bool
	rootVersion         *string
	selfClosing         bool
}

// textPosition is the character-data position of one text or CDATA token.
type textPosition uint8

const (
	textOutside textPosition = iota
	textContainer
	textBoolean
	textScalar
)

// normalization selects the literal-text normalization for native content.
type normalization uint8

const (
	normalizationText normalization = iota
	normalizationAttribute
)

// xmlParser is the formation state for one XML source.
type xmlParser struct {
	authority        document.DocumentAuthority
	source           *document.SourceSnapshot
	decoded          string
	limits           PlistParseLimits
	sink             *diagnosticSink
	recovered        bool
	pieces           []document.StructuralPiece
	kinds            []PlistSyntaxKind
	stack            []*xmlFrame
	unknownDepth     int
	doctypeBodyStart *int
	anyTopLevel      bool
	plistRootSeen    bool
	rootValueCount   int
	rootValueRef     *PlistValueRef
	arena            PlistDocumentBuilder
}

// parseXMLDocument forms one `plist.xml@1` document from a validated source
// snapshot.
func parseXMLDocument(authority document.DocumentAuthority, snapshot *document.SourceSnapshot,
	limits PlistParseLimits) (*Document, *FormationFailure) {
	decoded, ok := snapshot.DecodedText()
	if !ok {
		return nil, encodingFailure("plist.xml.encoding@1")
	}
	parser := &xmlParser{
		authority: authority,
		source:    snapshot,
		decoded:   decoded,
		limits:    limits,
		sink:      newDiagnosticSink(limits.Common.MaxDiagnostics),
		arena:     NewPlistDocumentBuilderWithLimits(limits.arenaLimits()),
	}
	return parser.parse()
}

// ---------------------------------------------------------------------------
// Tokenizer (mirrors the frozen xmlparser 0.13.6 token stream)
// ---------------------------------------------------------------------------

type xmlTokenKind uint8

const (
	tokenDeclaration xmlTokenKind = iota
	tokenProcessingInstruction
	tokenComment
	tokenDtdStart
	tokenDtdEnd
	tokenEmptyDtd
	tokenEntityDeclaration
	tokenElementStart
	tokenAttribute
	tokenElementEnd
	tokenText
	tokenCdata
)

type elementEndKind uint8

const (
	endOpen elementEndKind = iota
	endEmpty
	endClose
)

// xmlToken is one token with decoded offsets (half-open byte ranges over
// the decoded UTF-8 text).
type xmlToken struct {
	kind xmlTokenKind
	// span covers the whole token.
	start, end int
	// Declaration fields.
	versionStart, versionEnd   int
	encodingStart, encodingEnd int
	hasEncoding                bool
	// PI fields.
	targetStart, targetEnd   int
	contentStart, contentEnd int
	hasContent               bool
	// Comment text.
	textStart, textEnd int
	// DOCTYPE fields.
	dtdNameStart, dtdNameEnd     int
	dtdPublicStart, dtdPublicEnd int
	dtdSystemStart, dtdSystemEnd int
	dtdHasPublic, dtdHasSystem   bool
	// Element start fields.
	elemPrefixStart, elemPrefixEnd int
	elemLocalStart, elemLocalEnd   int
	// Attribute fields.
	attrPrefixStart, attrPrefixEnd int
	attrLocalStart, attrLocalEnd   int
	attrValueStart, attrValueEnd   int
	// Element end fields.
	endKind                      elementEndKind
	closeNameStart, closeNameEnd int
	// Text and CDATA.
	cdataTextStart, cdataTextEnd int
}

// xmlTokenizer mirrors the xmlparser state machine (lib.rs tokenizer
// states). Every position is a decoded byte offset.
type xmlTokenizer struct {
	text     string
	pos      int
	state    xmlState
	depth    int
	fragment bool
}

type xmlState uint8

const (
	stateDeclaration xmlState = iota
	stateAfterDeclaration
	stateDtd
	stateAfterDtd
	stateElements
	stateAttributes
	stateAfterElements
	stateEnd
)

// newXMLTokenizer starts a full-document tokenizer. The leading EF BB BF
// bytes (the UTF-8 BOM, which is also the UTF-8 encoding of the decoded
// U+FEFF of UTF-16 BOM sources) are skipped exactly like xmlparser's
// `From<&str>` skip, so token offsets are relative to the remaining text.
func newXMLTokenizer(text string) *xmlTokenizer {
	pos := 0
	if len(text) >= 3 && text[0] == 0xEF && text[1] == 0xBB && text[2] == 0xBF {
		pos = 3
	}
	return &xmlTokenizer{text: text, pos: pos, state: stateDeclaration}
}

// newFragmentTokenizer restarts tokenization from one decoded offset in
// fragment mode (the plist recovery path).
func newFragmentTokenizer(text string, from int) *xmlTokenizer {
	return &xmlTokenizer{text: text, pos: from, state: stateElements, fragment: true}
}

// atEnd reports whether the stream is exhausted.
func (t *xmlTokenizer) atEnd() bool { return t.pos >= len(t.text) }

// next yields the next token; errorPos >= 0 marks a tokenizer error at that
// decoded offset.
func (t *xmlTokenizer) next() (*xmlToken, int) {
	for {
		if t.atEnd() {
			t.state = stateEnd
			return nil, -1
		}
		switch t.state {
		case stateDeclaration:
			if strings.HasPrefix(t.text[t.pos:], "<?xml ") {
				token, ok := t.parseDeclaration()
				if ok {
					return token, -1
				}
				return nil, t.pos
			}
			t.state = stateAfterDeclaration
		case stateAfterDeclaration:
			if strings.HasPrefix(t.text[t.pos:], "<!DOCTYPE") {
				token, ok, errPos := t.parseDoctype()
				if errPos >= 0 {
					return nil, errPos
				}
				if !ok {
					t.state = stateAfterDtd
					continue
				}
				if token.kind == tokenDtdStart {
					t.state = stateDtd
				} else {
					t.state = stateAfterDtd
				}
				return token, -1
			}
			if strings.HasPrefix(t.text[t.pos:], "<!--") {
				token, ok := t.parseComment()
				if ok {
					return token, -1
				}
				return nil, t.pos
			}
			if strings.HasPrefix(t.text[t.pos:], "<?") {
				if strings.HasPrefix(t.text[t.pos:], "<?xml ") {
					return nil, t.pos
				}
				token, ok := t.parsePI()
				if ok {
					return token, -1
				}
				return nil, t.pos
			}
			if t.text[t.pos] == ' ' || t.text[t.pos] == '\t' || t.text[t.pos] == '\n' || t.text[t.pos] == '\r' {
				t.skipSpaces()
				continue
			}
			t.state = stateAfterDtd
		case stateDtd:
			rest := t.text[t.pos:]
			switch {
			case strings.HasPrefix(rest, "<!ENTITY"):
				ok := t.consumeDecl()
				if !ok {
					return nil, t.pos
				}
			case strings.HasPrefix(rest, "<!--"):
				token, ok := t.parseComment()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case strings.HasPrefix(rest, "<?"):
				if strings.HasPrefix(rest, "<?xml ") {
					return nil, t.pos
				}
				token, ok := t.parsePI()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case rest[0] == ']':
				// DTD ends with ']' S? '>'.
				start := t.pos
				t.pos++
				t.skipSpaces()
				if t.atEnd() {
					return nil, t.pos
				}
				if t.text[t.pos] != '>' {
					return nil, t.pos
				}
				t.pos++
				t.state = stateAfterDtd
				return &xmlToken{kind: tokenDtdEnd, start: start, end: t.pos}, -1
			case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r':
				t.skipSpaces()
			case strings.HasPrefix(rest, "<!ELEMENT") || strings.HasPrefix(rest, "<!ATTLIST") ||
				strings.HasPrefix(rest, "<!NOTATION"):
				ok := t.consumeDecl()
				if !ok {
					return nil, t.pos
				}
			default:
				return nil, t.pos
			}
		case stateAfterDtd:
			rest := t.text[t.pos:]
			switch {
			case strings.HasPrefix(rest, "<!--"):
				token, ok := t.parseComment()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case strings.HasPrefix(rest, "<?"):
				if strings.HasPrefix(rest, "<?xml ") {
					return nil, t.pos
				}
				token, ok := t.parsePI()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case strings.HasPrefix(rest, "<!"):
				return nil, t.pos
			case rest[0] == '<':
				t.state = stateAttributes
				token, ok := t.parseElementStart()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r':
				t.skipSpaces()
			default:
				return nil, t.pos
			}
		case stateElements:
			if t.text[t.pos] != '<' {
				token, ok := t.parseText()
				if ok {
					return token, -1
				}
				return nil, t.pos
			}
			rest := t.text[t.pos:]
			switch {
			case strings.HasPrefix(rest, "<!--"):
				token, ok := t.parseComment()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case strings.HasPrefix(rest, "<![CDATA["):
				token, ok := t.parseCdata()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case strings.HasPrefix(rest, "<!"):
				return nil, t.pos
			case strings.HasPrefix(rest, "<?"):
				if strings.HasPrefix(rest, "<?xml ") {
					return nil, t.pos
				}
				token, ok := t.parsePI()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case rest[1] == '/':
				if t.depth > 0 {
					t.depth--
				}
				if t.depth == 0 && !t.fragment {
					t.state = stateAfterElements
				} else {
					t.state = stateElements
				}
				token, ok := t.parseCloseElement()
				if ok {
					return token, -1
				}
				return nil, t.pos
			default:
				t.state = stateAttributes
				token, ok := t.parseElementStart()
				if ok {
					return token, -1
				}
				return nil, t.pos
			}
		case stateAttributes:
			token, ok := t.parseAttribute()
			if !ok {
				return nil, t.pos
			}
			if token.kind == tokenElementEnd {
				if token.endKind == endOpen {
					t.depth++
				}
				if t.depth == 0 && !t.fragment {
					t.state = stateAfterElements
				} else {
					t.state = stateElements
				}
			}
			return token, -1
		case stateAfterElements:
			rest := t.text[t.pos:]
			switch {
			case strings.HasPrefix(rest, "<!--"):
				token, ok := t.parseComment()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case strings.HasPrefix(rest, "<?"):
				if strings.HasPrefix(rest, "<?xml ") {
					return nil, t.pos
				}
				token, ok := t.parsePI()
				if ok {
					return token, -1
				}
				return nil, t.pos
			case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r':
				t.skipSpaces()
			default:
				return nil, t.pos
			}
		case stateEnd:
			return nil, -1
		}
	}
}

func (t *xmlTokenizer) skipSpaces() {
	for t.pos < len(t.text) {
		switch t.text[t.pos] {
		case ' ', '\t', '\n', '\r':
			t.pos++
		default:
			return
		}
	}
}

// parseDeclaration parses `<?xml version="..." encoding="..." standalone="..."?>`.
func (t *xmlTokenizer) parseDeclaration() (*xmlToken, bool) {
	start := t.pos
	t.pos += 6 // `<?xml `
	if !t.skipString("version") {
		return nil, false
	}
	if !t.consumeEq() {
		return nil, false
	}
	quote, ok := t.consumeQuote()
	if !ok {
		return nil, false
	}
	// VersionNum ::= '1.' [0-9]+; the version span covers the whole number
	// including the `1.` prefix (the frozen xmlparser slice-back).
	versionStart := t.pos
	if !t.skipString("1.") {
		return nil, false
	}
	for t.pos < len(t.text) && isASCIIDigit(t.text[t.pos]) {
		t.pos++
	}
	versionEnd := t.pos
	if !t.consumeByte(quote) {
		return nil, false
	}
	if !t.consumeDeclarationSpaces() {
		return nil, false
	}
	token := &xmlToken{kind: tokenDeclaration, start: start, end: 0,
		versionStart: versionStart, versionEnd: versionEnd}
	if strings.HasPrefix(t.text[t.pos:], "encoding") {
		t.pos += 8
		if !t.consumeEq() {
			return nil, false
		}
		quote, ok = t.consumeQuote()
		if !ok {
			return nil, false
		}
		encodingStart := t.pos
		for t.pos < len(t.text) && isEncodingByte(t.text[t.pos]) {
			t.pos++
		}
		encodingEnd := t.pos
		if !t.consumeByte(quote) {
			return nil, false
		}
		token.encodingStart, token.encodingEnd, token.hasEncoding = encodingStart, encodingEnd, true
		if !t.consumeDeclarationSpaces() {
			return nil, false
		}
	}
	if strings.HasPrefix(t.text[t.pos:], "standalone") {
		t.pos += 10
		if !t.consumeEq() {
			return nil, false
		}
		quote, ok = t.consumeQuote()
		if !ok {
			return nil, false
		}
		valueStart := t.pos
		for t.pos < len(t.text) && t.text[t.pos] != quote {
			t.pos++
		}
		value := t.text[valueStart:t.pos]
		if value != "yes" && value != "no" {
			return nil, false
		}
		if !t.consumeByte(quote) {
			return nil, false
		}
	}
	t.skipSpaces()
	if !t.skipString("?>") {
		return nil, false
	}
	token.end = t.pos
	return token, true
}

// consumeDeclarationSpaces requires at least one XML space unless `?>` or
// the end follows.
func (t *xmlTokenizer) consumeDeclarationSpaces() bool {
	switch {
	case t.pos < len(t.text) && isXMLSpaceByte(t.text[t.pos]):
		t.skipSpaces()
		return true
	case strings.HasPrefix(t.text[t.pos:], "?>"):
		return true
	case t.atEnd():
		return true
	}
	return false
}

// parsePI parses `<?target content?>`.
func (t *xmlTokenizer) parsePI() (*xmlToken, bool) {
	start := t.pos
	t.pos += 2
	prefix, local, ok := t.consumeQName()
	if !ok {
		return nil, false
	}
	_ = prefix
	token := &xmlToken{kind: tokenProcessingInstruction, start: start,
		targetStart: local.start, targetEnd: local.end}
	if strings.HasPrefix(t.text[t.pos:], "?>") {
		t.pos += 2
		token.end = t.pos
		return token, true
	}
	if t.pos >= len(t.text) || !isXMLSpaceByte(t.text[t.pos]) {
		return nil, false
	}
	contentStart := t.pos
	for t.pos < len(t.text) && !strings.HasPrefix(t.text[t.pos:], "?>") {
		t.pos++
	}
	if t.pos >= len(t.text) {
		return nil, false
	}
	contentEnd := t.pos
	t.pos += 2
	token.contentStart, token.contentEnd, token.hasContent = contentStart, contentEnd, true
	token.end = t.pos
	return token, true
}

// parseComment parses `<!-- ... -->`.
func (t *xmlTokenizer) parseComment() (*xmlToken, bool) {
	start := t.pos
	t.pos += 4
	textStart := t.pos
	for t.pos < len(t.text) && !strings.HasPrefix(t.text[t.pos:], "-->") {
		t.pos++
	}
	if t.pos >= len(t.text) {
		return nil, false
	}
	textEnd := t.pos
	if strings.Contains(t.text[textStart:textEnd], "--") {
		return nil, false
	}
	if textEnd > textStart && t.text[textEnd-1] == '-' {
		return nil, false
	}
	t.pos += 3
	return &xmlToken{kind: tokenComment, start: start, end: t.pos,
		textStart: textStart, textEnd: textEnd}, true
}

// parseDoctype parses `<!DOCTYPE name (PUBLIC ... | SYSTEM ...)? ('[' | '>')`.
func (t *xmlTokenizer) parseDoctype() (*xmlToken, bool, int) {
	start := t.pos
	t.pos += 9 // `<!DOCTYPE`
	t.skipSpaces()
	_, name, ok := t.consumeQName()
	if !ok {
		return nil, false, t.pos
	}
	t.skipSpaces()
	token := &xmlToken{kind: tokenEmptyDtd, start: start,
		dtdNameStart: name.start, dtdNameEnd: name.end}
	if strings.HasPrefix(t.text[t.pos:], "SYSTEM") || strings.HasPrefix(t.text[t.pos:], "PUBLIC") {
		isPublic := strings.HasPrefix(t.text[t.pos:], "PUBLIC")
		t.pos += 6
		t.skipSpaces()
		quote, ok := t.consumeQuote()
		if !ok {
			return nil, false, t.pos
		}
		literalStart := t.pos
		for t.pos < len(t.text) && t.text[t.pos] != quote {
			t.pos++
		}
		if t.pos >= len(t.text) {
			return nil, false, t.pos
		}
		literalEnd := t.pos
		if !t.consumeByte(quote) {
			return nil, false, t.pos
		}
		if isPublic {
			token.dtdHasPublic = true
			token.dtdPublicStart, token.dtdPublicEnd = literalStart, literalEnd
			t.skipSpaces()
			quote, ok = t.consumeQuote()
			if !ok {
				return nil, false, t.pos
			}
			sysStart := t.pos
			for t.pos < len(t.text) && t.text[t.pos] != quote {
				t.pos++
			}
			if t.pos >= len(t.text) {
				return nil, false, t.pos
			}
			sysEnd := t.pos
			if !t.consumeByte(quote) {
				return nil, false, t.pos
			}
			token.dtdHasSystem = true
			token.dtdSystemStart, token.dtdSystemEnd = sysStart, sysEnd
		} else {
			token.dtdHasSystem = true
			token.dtdSystemStart, token.dtdSystemEnd = literalStart, literalEnd
		}
	}
	t.skipSpaces()
	if t.pos >= len(t.text) {
		return nil, false, t.pos
	}
	switch t.text[t.pos] {
	case '[':
		t.pos++
		token.kind = tokenDtdStart
	case '>':
		t.pos++
	default:
		return nil, false, t.pos
	}
	token.end = t.pos
	return token, true, -1
}

// consumeDecl consumes one `<!...>` declaration silently (Dtd state).
func (t *xmlTokenizer) consumeDecl() bool {
	for t.pos < len(t.text) && t.text[t.pos] != '>' {
		t.pos++
	}
	if t.pos >= len(t.text) {
		return false
	}
	t.pos++
	return true
}

// parseElementStart parses `<name` or `<prefix:name`.
func (t *xmlTokenizer) parseElementStart() (*xmlToken, bool) {
	start := t.pos
	t.pos++
	prefix, local, ok := t.consumeQName()
	if !ok {
		return nil, false
	}
	return &xmlToken{kind: tokenElementStart, start: start, end: t.pos,
		elemPrefixStart: prefix.start, elemPrefixEnd: prefix.end,
		elemLocalStart: local.start, elemLocalEnd: local.end}, true
}

// parseCloseElement parses `</name S? >`.
func (t *xmlTokenizer) parseCloseElement() (*xmlToken, bool) {
	start := t.pos
	t.pos += 2
	_, name, ok := t.consumeQName()
	if !ok {
		return nil, false
	}
	t.skipSpaces()
	if t.pos >= len(t.text) || t.text[t.pos] != '>' {
		return nil, false
	}
	t.pos++
	return &xmlToken{kind: tokenElementEnd, start: start, end: t.pos,
		endKind: endClose, closeNameStart: name.start, closeNameEnd: name.end}, true
}

// parseAttribute parses one attribute or the element-end terminator.
func (t *xmlTokenizer) parseAttribute() (*xmlToken, bool) {
	attrStart := t.pos
	hasSpace := t.pos < len(t.text) && isXMLSpaceByte(t.text[t.pos])
	t.skipSpaces()
	if t.pos >= len(t.text) {
		return nil, false
	}
	switch t.text[t.pos] {
	case '>':
		start := t.pos
		t.pos++
		return &xmlToken{kind: tokenElementEnd, start: start, end: t.pos, endKind: endOpen}, true
	case '/':
		start := t.pos
		t.pos++
		if t.pos >= len(t.text) || t.text[t.pos] != '>' {
			return nil, false
		}
		t.pos++
		return &xmlToken{kind: tokenElementEnd, start: start, end: t.pos, endKind: endEmpty}, true
	}
	if !hasSpace {
		return nil, false
	}
	start := attrStart
	prefix, local, ok := t.consumeQName()
	if !ok {
		return nil, false
	}
	if !t.consumeEq() {
		return nil, false
	}
	quote, ok := t.consumeQuote()
	if !ok {
		return nil, false
	}
	valueStart := t.pos
	for t.pos < len(t.text) && t.text[t.pos] != quote && t.text[t.pos] != '<' {
		t.pos++
	}
	if t.pos >= len(t.text) {
		return nil, false
	}
	valueEnd := t.pos
	if !t.consumeByte(quote) {
		return nil, false
	}
	return &xmlToken{kind: tokenAttribute, start: start, end: t.pos,
		attrPrefixStart: prefix.start, attrPrefixEnd: prefix.end,
		attrLocalStart: local.start, attrLocalEnd: local.end,
		attrValueStart: valueStart, attrValueEnd: valueEnd}, true
}

// parseText parses character data up to the next `<`.
func (t *xmlTokenizer) parseText() (*xmlToken, bool) {
	start := t.pos
	for t.pos < len(t.text) && t.text[t.pos] != '<' {
		t.pos++
	}
	text := t.text[start:t.pos]
	if strings.Contains(text, ">") && strings.Contains(text, "]]>") {
		return nil, false
	}
	return &xmlToken{kind: tokenText, start: start, end: t.pos}, true
}

// parseCdata parses `<![CDATA[ ... ]]>`.
func (t *xmlTokenizer) parseCdata() (*xmlToken, bool) {
	start := t.pos
	t.pos += 9
	textStart := t.pos
	for t.pos < len(t.text) && !strings.HasPrefix(t.text[t.pos:], "]]>") {
		t.pos++
	}
	if t.pos >= len(t.text) {
		return nil, false
	}
	textEnd := t.pos
	t.pos += 3
	return &xmlToken{kind: tokenCdata, start: start, end: t.pos,
		cdataTextStart: textStart, cdataTextEnd: textEnd}, true
}

// consumeQName consumes one name or prefix:local pair.
func (t *xmlTokenizer) consumeQName() (span, span, bool) {
	start := t.pos
	for t.pos < len(t.text) && isNameByte(t.text[t.pos]) {
		t.pos++
	}
	if t.pos == start {
		return span{}, span{}, false
	}
	end := t.pos
	if t.pos < len(t.text) && t.text[t.pos] == ':' {
		prefix := span{start: start, end: end}
		t.pos++
		localStart := t.pos
		for t.pos < len(t.text) && isNameByte(t.text[t.pos]) {
			t.pos++
		}
		if t.pos == localStart {
			return span{}, span{}, false
		}
		return prefix, span{start: localStart, end: t.pos}, true
	}
	return span{}, span{start: start, end: end}, true
}

type span struct{ start, end int }

func (t *xmlTokenizer) consumeEq() bool {
	t.skipSpaces()
	if t.pos >= len(t.text) || t.text[t.pos] != '=' {
		return false
	}
	t.pos++
	t.skipSpaces()
	return true
}

func (t *xmlTokenizer) consumeQuote() (byte, bool) {
	if t.pos >= len(t.text) {
		return 0, false
	}
	quote := t.text[t.pos]
	if quote != '"' && quote != '\'' {
		return 0, false
	}
	t.pos++
	return quote, true
}

func (t *xmlTokenizer) consumeByte(byte byte) bool {
	if t.pos >= len(t.text) || t.text[t.pos] != byte {
		return false
	}
	t.pos++
	return true
}

func (t *xmlTokenizer) skipString(value string) bool {
	if strings.HasPrefix(t.text[t.pos:], value) {
		t.pos += len(value)
		return true
	}
	return false
}

func isASCIIDigit(byte byte) bool { return byte >= '0' && byte <= '9' }

func isXMLSpaceByte(byte byte) bool {
	return byte == ' ' || byte == '\t' || byte == '\n' || byte == '\r'
}

func isNameByte(byte byte) bool {
	return byte == '_' || byte == '-' || byte == '.' || byte == ':' ||
		(byte >= 'a' && byte <= 'z') || (byte >= 'A' && byte <= 'Z') ||
		(byte >= '0' && byte <= '9')
}

func isEncodingByte(byte byte) bool {
	return byte == '_' || byte == '-' || byte == '.' ||
		(byte >= 'a' && byte <= 'z') || (byte >= 'A' && byte <= 'Z') ||
		(byte >= '0' && byte <= '9')
}

// ---------------------------------------------------------------------------
// Plist grammar over the token stream
// ---------------------------------------------------------------------------

func (p *xmlParser) parse() (*Document, *FormationFailure) {
	if failure := p.coverBOM(); failure != nil {
		return nil, failure
	}
	tokenizer := newXMLTokenizer(p.decoded)
	for {
		token, errPos := tokenizer.next()
		if token == nil {
			if errPos < 0 {
				break
			}
			// xmlparser jumps its stream on an error; the deterministic
			// error region is the last byte before the error position, and
			// every byte before it stays covered by handler pieces and gap
			// assembly (RFC 0013 §3).
			end := errPos
			if end > len(p.decoded) {
				end = len(p.decoded)
			}
			start := end - 1
			if start < 0 {
				start = 0
			}
			if end > start {
				if failure := p.recoverErrorRegion(start, end); failure != nil {
					return nil, failure
				}
			}
			next := strings.IndexByte(p.decoded[end:], '<')
			if next < 0 {
				break
			}
			tokenizer = newFragmentTokenizer(p.decoded, end+next)
			continue
		}
		if failure := p.token(token); failure != nil {
			return nil, failure
		}
	}
	return p.finish()
}

func (p *xmlParser) token(token *xmlToken) *FormationFailure {
	switch token.kind {
	case tokenDeclaration:
		return p.declaration(token)
	case tokenProcessingInstruction:
		return p.processingInstruction(token)
	case tokenComment:
		return p.comment(token)
	case tokenDtdStart:
		return p.doctypeStart(token)
	case tokenEmptyDtd:
		return p.doctypeEmpty(token)
	case tokenEntityDeclaration:
		// Inside the DOCTYPE body, which is one DoctypeBody piece.
		return nil
	case tokenDtdEnd:
		return p.dtdEnd(token)
	case tokenElementStart:
		return p.elementStart(token)
	case tokenAttribute:
		return p.attribute(token)
	case tokenElementEnd:
		return p.elementEnd(token)
	case tokenText:
		return p.text(token)
	case tokenCdata:
		return p.cdata(token)
	}
	return nil
}

// coverBOM covers a leading BOM as a trivia piece; the tokenizer skips the
// same bytes in decoded text.
func (p *xmlParser) coverBOM() *FormationFailure {
	if bom := p.source.EncodingFacts().Bom(); bom != nil {
		length := 0
		switch *bom {
		case document.BomUtf8:
			length = 3
		case document.BomUtf16Le, document.BomUtf16Be:
			length = 2
		}
		if length > 0 {
			// The BOM piece is a raw-offset piece: decoded offset 0 maps to
			// the raw BOM bytes already.
			return p.pushPieceRaw(document.NewStructuralPiece(
				mustSpan(p.authority, 0, length), document.PieceTrivia), PlistSyntaxKindBom)
		}
	}
	return nil
}

func (p *xmlParser) declaration(token *xmlToken) *FormationFailure {
	if p.unknownDepth > 0 {
		return nil
	}
	if failure := p.pushPiece(token.start, token.start+declarationOpenBytes,
		PlistSyntaxKindDeclarationOpen, document.PieceToken); failure != nil {
		return failure
	}
	rel := declarationOpenBytes
	text := p.decoded[token.start:token.end]
	rel = skipDeclarationSpaces(text, rel)
	if strings.HasPrefix(text[rel:], "version") {
		if failure := p.pushPiece(token.start+rel, token.start+rel+7,
			PlistSyntaxKindDeclarationName, document.PieceToken); failure != nil {
			return failure
		}
		if failure := p.pushPiece(token.versionStart, token.versionEnd,
			PlistSyntaxKindDeclarationValue, document.PieceToken); failure != nil {
			return failure
		}
		rel = token.versionEnd - token.start + 1
	}
	if version := p.decoded[token.versionStart:token.versionEnd]; version != "1.0" {
		p.recover("plist.parse.declaration-version@1", protocol.CategorySyntax,
			p.rawLocation(token.versionStart, token.versionEnd),
			map[string]string{"version": version})
	}
	if token.hasEncoding {
		rel = skipDeclarationSpaces(text, rel)
		if strings.HasPrefix(text[rel:], "encoding") {
			if failure := p.pushPiece(token.start+rel, token.start+rel+8,
				PlistSyntaxKindDeclarationName, document.PieceToken); failure != nil {
				return failure
			}
			if failure := p.pushPiece(token.encodingStart, token.encodingEnd,
				PlistSyntaxKindDeclarationValue, document.PieceToken); failure != nil {
				return failure
			}
			rel = token.encodingEnd - token.start + 1
		}
		declared := strings.ToUpper(p.decoded[token.encodingStart:token.encodingEnd])
		selected := p.source.EncodingFacts().Selected()
		agrees := false
		switch selected.Kind() {
		case document.EncodingUtf8:
			agrees = declared == "UTF-8"
		case document.EncodingUtf16Le:
			agrees = declared == "UTF-16" || declared == "UTF-16LE"
		case document.EncodingUtf16Be:
			agrees = declared == "UTF-16" || declared == "UTF-16BE"
		}
		if !agrees {
			p.recover("plist.parse.declaration-conflict@1", protocol.CategoryEncoding,
				p.rawLocation(token.encodingStart, token.encodingEnd),
				map[string]string{
					"declared": p.decoded[token.encodingStart:token.encodingEnd],
					"selected": selected.AsStr(),
				})
		}
	}
	rel = skipDeclarationSpaces(text, rel)
	if strings.HasPrefix(text[rel:], "standalone") {
		if failure := p.pushPiece(token.start+rel, token.start+rel+10,
			PlistSyntaxKindDeclarationName, document.PieceToken); failure != nil {
			return failure
		}
		valueSpan := p.standaloneValueSpan(token, rel+10)
		if valueSpan.start < valueSpan.end {
			if failure := p.pushPiece(valueSpan.start, valueSpan.end,
				PlistSyntaxKindDeclarationValue, document.PieceToken); failure != nil {
				return failure
			}
		}
	}
	if strings.HasSuffix(text, "?>") {
		if failure := p.pushPiece(token.end-2, token.end,
			PlistSyntaxKindDeclarationClose, document.PieceToken); failure != nil {
			return failure
		}
	}
	return nil
}

// standaloneValueSpan locates the `standalone` value span inside the
// declaration text.
func (p *xmlParser) standaloneValueSpan(token *xmlToken, rel int) span {
	text := p.decoded[token.start:token.end]
	rel = skipDeclarationSpaces(text, rel)
	eq := strings.IndexByte(text[rel:], '=')
	if eq < 0 {
		return span{start: token.end, end: token.end}
	}
	rel += eq + 1
	rel = skipDeclarationSpaces(text, rel)
	if rel >= len(text) || (text[rel] != '"' && text[rel] != '\'') {
		return span{start: token.end, end: token.end}
	}
	quote := text[rel]
	rel++
	closeAt := strings.IndexByte(text[rel:], quote)
	if closeAt < 0 {
		return span{start: token.end, end: token.end}
	}
	return span{start: token.start + rel, end: token.start + rel + closeAt}
}

func (p *xmlParser) processingInstruction(token *xmlToken) *FormationFailure {
	if p.doctypeBodyStart != nil || p.unknownDepth > 0 {
		return nil
	}
	target := p.decoded[token.targetStart:token.targetEnd]
	if strings.EqualFold(target, "xml") {
		p.recover("plist.parse.pi-target@1", protocol.CategorySyntax,
			p.rawLocation(token.targetStart, token.targetEnd), nil)
	}
	if failure := p.pushPiece(token.start, token.start+2,
		PlistSyntaxKindProcessingInstructionOpen, document.PieceTrivia); failure != nil {
		return failure
	}
	if failure := p.pushPiece(token.targetStart, token.targetEnd,
		PlistSyntaxKindProcessingInstructionTarget, document.PieceTrivia); failure != nil {
		return failure
	}
	if token.hasContent {
		if failure := p.pushPiece(token.contentStart, token.contentEnd,
			PlistSyntaxKindProcessingInstructionContent, document.PieceTrivia); failure != nil {
			return failure
		}
	}
	closeStart := token.end - 2
	if closeStart < token.start {
		closeStart = token.start
	}
	return p.pushPiece(closeStart, token.end,
		PlistSyntaxKindProcessingInstructionClose, document.PieceTrivia)
}

func (p *xmlParser) comment(token *xmlToken) *FormationFailure {
	if p.doctypeBodyStart != nil || p.unknownDepth > 0 {
		return nil
	}
	if failure := p.pushPiece(token.start, token.start+commentOpenBytes,
		PlistSyntaxKindCommentOpen, document.PieceTrivia); failure != nil {
		return failure
	}
	if failure := p.pushPiece(token.textStart, token.textEnd,
		PlistSyntaxKindCommentText, document.PieceTrivia); failure != nil {
		return failure
	}
	return p.pushPiece(token.textEnd, token.end,
		PlistSyntaxKindCommentClose, document.PieceTrivia)
}

func (p *xmlParser) doctypeStart(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	if pushFailure := p.pushPieceRaw(document.NewStructuralPiece(
		mustSpan(p.authority, raw.StartByte(), raw.StartByte()+doctypeOpenBytes),
		document.PieceToken), PlistSyntaxKindDoctypeOpen); pushFailure != nil {
		return pushFailure
	}
	p.validateDoctype(token, raw)
	p.recover("plist.parse.doctype-subset@1", protocol.CategorySyntax,
		p.locationOfRaw(raw.StartByte(), raw.EndByte()), nil)
	start := raw.StartByte() + doctypeOpenBytes
	p.doctypeBodyStart = &start
	return nil
}

func (p *xmlParser) doctypeEmpty(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	if pushFailure := p.pushPieceRaw(document.NewStructuralPiece(
		mustSpan(p.authority, raw.StartByte(), raw.StartByte()+doctypeOpenBytes),
		document.PieceToken), PlistSyntaxKindDoctypeOpen); pushFailure != nil {
		return pushFailure
	}
	p.validateDoctype(token, raw)
	bodyEnd := raw.EndByte() - 1
	if pushFailure := p.pushPieceRaw(document.NewStructuralPiece(
		mustSpan(p.authority, raw.StartByte()+doctypeOpenBytes, bodyEnd),
		document.PieceToken), PlistSyntaxKindDoctypeBody); pushFailure != nil {
		return pushFailure
	}
	return p.pushPieceRaw(document.NewStructuralPiece(
		mustSpan(p.authority, bodyEnd, raw.EndByte()), document.PieceToken),
		PlistSyntaxKindDoctypeClose)
}

// validateDoctype enforces the exact Apple plist DOCTYPE identity (RFC 0013
// §4.1).
func (p *xmlParser) validateDoctype(token *xmlToken, raw document.Span) {
	name := p.decoded[token.dtdNameStart:token.dtdNameEnd]
	identifiersOK := token.dtdHasPublic && token.dtdHasSystem &&
		p.decoded[token.dtdPublicStart:token.dtdPublicEnd] == plistDoctypePublic &&
		p.decoded[token.dtdSystemStart:token.dtdSystemEnd] == plistDoctypeSystem
	if name != "plist" || !identifiersOK {
		arguments := map[string]string{"name": name}
		if token.dtdHasPublic && token.dtdHasSystem {
			arguments["public"] = p.decoded[token.dtdPublicStart:token.dtdPublicEnd]
			arguments["system"] = p.decoded[token.dtdSystemStart:token.dtdSystemEnd]
		} else if token.dtdHasSystem {
			arguments["system"] = p.decoded[token.dtdSystemStart:token.dtdSystemEnd]
		}
		p.recover("plist.parse.doctype@1", protocol.CategorySyntax,
			p.locationOfRaw(raw.StartByte(), raw.EndByte()), arguments)
	}
}

func (p *xmlParser) dtdEnd(token *xmlToken) *FormationFailure {
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	bodyEnd := raw.EndByte() - 1
	if p.doctypeBodyStart != nil {
		if pushFailure := p.pushPieceRaw(document.NewStructuralPiece(
			mustSpan(p.authority, *p.doctypeBodyStart, bodyEnd),
			document.PieceToken), PlistSyntaxKindDoctypeBody); pushFailure != nil {
			return pushFailure
		}
		p.doctypeBodyStart = nil
	}
	return p.pushPieceRaw(document.NewStructuralPiece(
		mustSpan(p.authority, bodyEnd, raw.EndByte()), document.PieceToken),
		PlistSyntaxKindDoctypeClose)
}

func (p *xmlParser) elementStart(token *xmlToken) *FormationFailure {
	if len(p.stack) >= p.limits.Common.MaxNestingDepth {
		return p.fatalLimit("nesting-depth", len(p.stack)+1, p.limits.Common.MaxNestingDepth)
	}
	raw, failure := p.rawSpan(token.start, token.end)
	if failure != nil {
		return failure
	}
	prefix := p.decoded[token.elemPrefixStart:token.elemPrefixEnd]
	local := p.decoded[token.elemLocalStart:token.elemLocalEnd]
	kind := classifyElement(prefix, local)
	name := p.decoded[token.start+1 : token.end]
	topLevel := len(p.stack) == 0
	admittedRoot := topLevel && !p.plistRootSeen && kind != nil && *kind == elementPlist
	isUnknown := false
	if topLevel {
		isUnknown = !admittedRoot
	} else {
		isUnknown = kind == nil || (kind != nil && *kind == elementPlist)
	}
	if topLevel {
		p.anyTopLevel = true
	}
	frameValue := frameValue{kind: frameNone}
	if kind != nil {
		switch *kind {
		case elementPlist:
			frameValue.kind = frameRoot
		case elementDict:
			frameValue.kind = frameDict
			frameValue.dict = &dictState{groups: map[string]int{}}
		case elementArray:
			frameValue.kind = frameArray
		}
	}
	valueAllowed := !isUnknown
	scalarViolation := false
	if !isUnknown && kind != nil {
		parentKind := (*elementKind)(nil)
		var parentAllowed bool
		parentExpectValue := false
		parentScalar := false
		if len(p.stack) > 0 {
			parent := p.stack[len(p.stack)-1]
			parentKind = parent.kind
			parentAllowed = parent.valueAllowed
			parentExpectValue = parent.value.kind == frameDict && parent.value.dict.expectValue
			parentScalar = parent.kind != nil && parent.kind.isScalar()
		} else {
			parentAllowed = true
		}
		valueAllowed = parentAllowed
		switch *kind {
		case elementKey:
			switch {
			case parentKind != nil && *parentKind == elementDict:
				if parentAllowed && parentExpectValue {
					p.recover("plist.parse.dict-missing-value@1", protocol.CategorySyntax,
						p.locationOfRaw(raw.StartByte(), raw.EndByte()), nil)
				}
			case parentKind != nil && (*parentKind == elementPlist || *parentKind == elementArray):
				p.recover("plist.parse.key-outside-dict@1", protocol.CategorySyntax,
					p.locationOfRaw(raw.StartByte(), raw.EndByte()), map[string]string{"name": name})
			case parentKind != nil:
				scalarViolation = true
			}
		case elementDict, elementArray:
			if parentScalar {
				scalarViolation = true
			}
		default:
			switch {
			case parentKind != nil && *parentKind == elementDict:
				if parentAllowed && !parentExpectValue {
					p.recover("plist.parse.dict-key@1", protocol.CategorySyntax,
						p.locationOfRaw(raw.StartByte(), raw.EndByte()),
						map[string]string{"element": name})
				}
			case parentKind != nil && (*parentKind == elementPlist || *parentKind == elementArray):
				// Admitted value position.
			case parentKind != nil:
				scalarViolation = true
			}
		}
	}
	if scalarViolation {
		p.recover("plist.parse.scalar-content@1", protocol.CategorySyntax,
			p.locationOfRaw(raw.StartByte(), raw.EndByte()), map[string]string{"element": name})
		if len(p.stack) > 0 {
			p.stack[len(p.stack)-1].scalarUnproven = true
		}
		valueAllowed = false
	}
	if isUnknown && p.unknownDepth == 0 {
		p.recover("plist.parse.element-name@1", protocol.CategorySyntax,
			p.locationOfRaw(raw.StartByte(), raw.EndByte()), map[string]string{"name": name})
	}
	if p.unknownDepth == 0 && !isUnknown && kind != nil {
		if pushFailure := p.pushPieceRaw(document.NewStructuralPiece(
			mustSpan(p.authority, raw.StartByte(), raw.EndByte()),
			document.PieceToken), kind.openKind()); pushFailure != nil {
			return pushFailure
		}
	}
	var unknownMarker *int
	if isUnknown && p.unknownDepth == 0 {
		marker := raw.StartByte()
		unknownMarker = &marker
	}
	frame := &xmlFrame{
		kind: kind, name: name,
		openStart: raw.StartByte(), openEnd: raw.EndByte(),
		tagCursor:           token.end,
		unknownSubtreeStart: unknownMarker,
		valueAllowed:        valueAllowed,
		value:               frameValue,
		scalarUnproven:      false,
	}
	p.stack = append(p.stack, frame)
	if isUnknown {
		p.unknownDepth++
	}
	if admittedRoot {
		p.plistRootSeen = true
	}
	return nil
}

func (p *xmlParser) attribute(token *xmlToken) *FormationFailure {
	if p.unknownDepth > 0 {
		return nil
	}
	var tagCursor, isRoot int
	versionUnset := true
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		tagCursor = frame.tagCursor
		if frame.kind != nil && *frame.kind == elementPlist && len(p.stack) == 1 {
			isRoot = 1
		}
		if frame.rootVersion != nil {
			versionUnset = false
		}
	}
	if failure := p.pushWhitespacePieces(tagCursor, token.start); failure != nil {
		return failure
	}
	prefix := p.decoded[token.attrPrefixStart:token.attrPrefixEnd]
	local := p.decoded[token.attrLocalStart:token.attrLocalEnd]
	isVersion := isRoot == 1 && versionUnset && prefix == "" && local == "version"
	var rootVersion *string
	if isVersion {
		if pushFailure := p.pushPiece(token.attrLocalStart, token.attrLocalEnd,
			PlistSyntaxKindPlistVersionName, document.PieceToken); pushFailure != nil {
			return pushFailure
		}
		eqAt := token.attrLocalEnd
		if at := strings.IndexByte(p.decoded[token.attrLocalEnd:token.attrValueStart], '='); at >= 0 {
			eqAt = token.attrLocalEnd + at
		}
		if pushFailure := p.pushPiece(eqAt, token.attrValueEnd+1,
			PlistSyntaxKindPlistVersionValue, document.PieceToken); pushFailure != nil {
			return pushFailure
		}
		normalized, failure := p.normalizeAttributeValue(token)
		if failure != nil {
			return failure
		}
		if normalized != plistVersion {
			p.recover("plist.parse.root-version@1", protocol.CategorySyntax,
				p.rawLocation(eqAt, token.attrValueEnd+1), map[string]string{"version": normalized})
		}
		rootVersion = &normalized
	} else {
		attrEnd := token.attrValueEnd + 1
		if pushFailure := p.pushPiece(token.start, attrEnd,
			PlistSyntaxKindErrorRegion, document.PieceErrorRegion); pushFailure != nil {
			return pushFailure
		}
		code := "plist.parse.element-attribute@1"
		if isRoot == 1 {
			code = "plist.parse.root-attribute@1"
		}
		nameText := local
		if prefix != "" {
			nameText = prefix + ":" + local
		}
		p.recover(code, protocol.CategorySyntax,
			p.rawLocation(token.start, attrEnd), map[string]string{"name": nameText})
	}
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		frame.tagCursor = token.attrValueEnd + 1
		if rootVersion != nil {
			frame.rootVersion = rootVersion
		}
	}
	return nil
}

// normalizeAttributeValue resolves references and applies XML attribute
// normalization.
func (p *xmlParser) normalizeAttributeValue(token *xmlToken) (string, *FormationFailure) {
	return p.resolveFragments(token.attrValueStart, token.attrValueEnd,
		normalizationAttribute, false)
}

func (p *xmlParser) elementEnd(token *xmlToken) *FormationFailure {
	switch token.endKind {
	case endOpen:
		return p.openTagEnd(token)
	case endEmpty:
		return p.emptyTagEnd(token)
	case endClose:
		return p.closeTagEnd(token)
	}
	return nil
}

func (p *xmlParser) openTagEnd(token *xmlToken) *FormationFailure {
	isPlist := false
	hasVersion := true
	var tagCursor int
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		tagCursor = frame.tagCursor
		if frame.kind != nil && *frame.kind == elementPlist && len(p.stack) == 1 {
			isPlist = true
		}
		if frame.rootVersion == nil {
			hasVersion = false
		}
	}
	if p.unknownDepth == 0 {
		if failure := p.pushWhitespacePieces(tagCursor, token.start); failure != nil {
			return failure
		}
		kind := PlistSyntaxKindErrorRegion
		if len(p.stack) > 0 {
			frame := p.stack[len(p.stack)-1]
			if frame.kind != nil {
				kind = frame.kind.openKind()
			}
		}
		if failure := p.pushPiece(token.start, token.end, kind, document.PieceToken); failure != nil {
			return failure
		}
	}
	if isPlist && !hasVersion {
		p.recover("plist.parse.root-version@1", protocol.CategorySyntax,
			p.rawLocation(token.start, token.end), map[string]string{"version": "<missing>"})
	}
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		frame.tagCursor = token.end
		if p.unknownDepth == 0 {
			frame.openEnd = token.end
		}
	}
	return nil
}

func (p *xmlParser) emptyTagEnd(token *xmlToken) *FormationFailure {
	isPlist := false
	hasVersion := true
	var tagCursor int
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		tagCursor = frame.tagCursor
		if frame.kind != nil && *frame.kind == elementPlist && len(p.stack) == 1 {
			isPlist = true
		}
		if frame.rootVersion == nil {
			hasVersion = false
		}
	}
	if p.unknownDepth == 0 {
		if failure := p.pushWhitespacePieces(tagCursor, token.start); failure != nil {
			return failure
		}
		kind := PlistSyntaxKindErrorRegion
		if len(p.stack) > 0 {
			frame := p.stack[len(p.stack)-1]
			if frame.kind != nil {
				kind = frame.kind.closeKind()
			}
		}
		if failure := p.pushPiece(token.start, token.end, kind, document.PieceToken); failure != nil {
			return failure
		}
	}
	if isPlist && !hasVersion {
		p.recover("plist.parse.root-version@1", protocol.CategorySyntax,
			p.rawLocation(token.start, token.end), map[string]string{"version": "<missing>"})
	}
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		frame.selfClosing = true
		if p.unknownDepth == 0 {
			frame.openEnd = token.end
		}
	}
	return p.closeFrame(token.start, token.end)
}

func (p *xmlParser) closeTagEnd(token *xmlToken) *FormationFailure {
	closeName := p.decoded[token.closeNameStart:token.closeNameEnd]
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		if frame.name != closeName {
			p.recover("plist.parse.mismatched-end-tag@1", protocol.CategorySyntax,
				p.rawLocation(token.start, token.end),
				map[string]string{"expected": frame.name, "found": closeName})
		}
	}
	if p.unknownDepth == 0 {
		kind := PlistSyntaxKindErrorRegion
		if len(p.stack) > 0 {
			frame := p.stack[len(p.stack)-1]
			if frame.kind != nil {
				kind = frame.kind.closeKind()
			}
		}
		if failure := p.pushPiece(token.start, token.end, kind, document.PieceToken); failure != nil {
			return failure
		}
	}
	return p.closeFrame(token.start, token.end)
}

func (p *xmlParser) closeFrame(closeStart, closeEnd int) *FormationFailure {
	if len(p.stack) == 0 {
		p.recover("plist.parse.extra-end-tag@1", protocol.CategorySyntax,
			p.rawLocation(closeStart, closeEnd), nil)
		return nil
	}
	frame := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	if frame.unknownSubtreeStart != nil {
		if failure := p.pushPieceRaw(document.NewStructuralPiece(
			mustSpan(p.authority, *frame.unknownSubtreeStart, closeEnd),
			document.PieceErrorRegion), PlistSyntaxKindErrorRegion); failure != nil {
			return failure
		}
	}
	if frame.kind == nil {
		p.unknownDepth--
		return nil
	}
	kind := *frame.kind
	if kind == elementKey {
		units := utf16Encode(frame.content.String())
		if len(units) > p.limits.MaxStringCodeUnits {
			return p.fatalLimit("string-code-units", len(units), p.limits.MaxStringCodeUnits)
		}
		if frame.valueAllowed {
			var pending *PlistKey
			if !frame.scalarUnproven {
				key := NewPlistKeyFromString(NewPlistStringFromCodeUnits(units))
				pending = &key
			}
			if len(p.stack) > 0 {
				parent := p.stack[len(p.stack)-1]
				if parent.valueAllowed && parent.value.kind == frameDict {
					parent.value.dict.pendingKey = pending
					parent.value.dict.expectValue = true
				}
			}
		}
		return nil
	}
	valueRef, failure := p.buildValue(frame, closeStart, closeEnd)
	if failure != nil {
		return failure
	}
	if len(p.stack) == 0 {
		return nil
	}
	parent := p.stack[len(p.stack)-1]
	missingValue := false
	switch parent.value.kind {
	case frameRoot:
		p.rootValueCount++
		if valueRef != nil && p.rootValueRef == nil {
			p.rootValueRef = valueRef
		}
	case frameDict:
		state := parent.value.dict
		if state.expectValue {
			state.expectValue = false
			if state.pendingKey != nil {
				key := *state.pendingKey
				state.pendingKey = nil
				if valueRef != nil {
					group := state.groups[codeUnitKey(key)] + 1
					state.groups[codeUnitKey(key)] = group
					if group > p.limits.MaxDuplicateKeyGroupMembers {
						return p.fatalLimit("duplicate-key-group", group,
							p.limits.MaxDuplicateKeyGroupMembers)
					}
					if len(state.entries) >= p.limits.MaxDictEntries {
						return p.fatalLimit("dict-entries", len(state.entries)+1,
							p.limits.MaxDictEntries)
					}
					state.entries = append(state.entries, NewPlistDictEntry(key, *valueRef))
				} else {
					missingValue = true
				}
			} else {
				missingValue = true
			}
		}
	case frameArray:
		if valueRef != nil {
			if len(parent.value.elements) >= p.limits.MaxArrayElements {
				return p.fatalLimit("array-elements", len(parent.value.elements)+1,
					p.limits.MaxArrayElements)
			}
			parent.value.elements = append(parent.value.elements, *valueRef)
		}
	}
	if missingValue {
		p.recover("plist.parse.dict-missing-value@1", protocol.CategorySyntax,
			p.rawLocation(closeStart, closeEnd), nil)
	}
	return nil
}

// buildValue parses one closing element's native value and adds it to the
// arena (RFC 0013 §4.3-4.9).
func (p *xmlParser) buildValue(frame *xmlFrame, closeStart, closeEnd int) (*PlistValueRef, *FormationFailure) {
	var value *PlistValue
	switch *frame.kind {
	case elementDict:
		if frame.value.kind == frameDict {
			if frame.value.dict.expectValue {
				p.recover("plist.parse.dict-missing-value@1", protocol.CategorySyntax,
					p.rawLocation(closeStart, closeEnd), nil)
			}
			dictValue := NewPlistValueDict(NewPlistDictFromEntries(frame.value.dict.entries))
			value = &dictValue
		}
	case elementArray:
		if frame.value.kind == frameArray {
			array := NewPlistArrayFromElements(frame.value.elements)
			arrayValue := NewPlistValueArray(array)
			value = &arrayValue
		}
	case elementString, elementKey:
		if frame.scalarUnproven {
			return nil, nil
		}
		units := utf16Encode(frame.content.String())
		if len(units) > p.limits.MaxStringCodeUnits {
			return nil, p.fatalLimit("string-code-units", len(units), p.limits.MaxStringCodeUnits)
		}
		stringValue := NewPlistValueString(NewPlistStringFromCodeUnits(units))
		value = &stringValue
	case elementInteger:
		if frame.scalarUnproven {
			return nil, nil
		}
		if frame.content.Len() == 0 {
			p.recover("plist.parse.empty-value@1", protocol.CategorySyntax,
				p.rawLocation(closeStart, closeEnd), map[string]string{"element": "integer"})
			return nil, nil
		}
		parsed, ok := parseIntegerText(frame.content.String())
		if ok {
			integerValue := NewPlistValueInteger(NewPlistInteger(parsed))
			value = &integerValue
		} else {
			p.recover("plist.parse.integer@1", protocol.CategorySyntax,
				p.rawLocation(closeStart, closeEnd), nil)
			return nil, nil
		}
	case elementReal:
		if frame.scalarUnproven {
			return nil, nil
		}
		if frame.content.Len() == 0 {
			p.recover("plist.parse.empty-value@1", protocol.CategorySyntax,
				p.rawLocation(closeStart, closeEnd), map[string]string{"element": "real"})
			return nil, nil
		}
		parsed, ok := parseRealText(frame.content.String())
		if ok {
			realValue := NewPlistValueReal(NewPlistRealDouble(parsed))
			value = &realValue
		} else {
			p.recover("plist.parse.real@1", protocol.CategorySyntax,
				p.rawLocation(closeStart, closeEnd), nil)
			return nil, nil
		}
	case elementDate:
		if frame.scalarUnproven {
			return nil, nil
		}
		if frame.content.Len() == 0 {
			p.recover("plist.parse.empty-value@1", protocol.CategorySyntax,
				p.rawLocation(closeStart, closeEnd), map[string]string{"element": "date"})
			return nil, nil
		}
		seconds, ok := parseDateText(frame.content.String())
		if ok {
			date, valid := NewPlistDateFromSeconds(seconds)
			if !valid {
				return nil, p.internalFailure()
			}
			dateValue := NewPlistValueDate(date)
			value = &dateValue
		} else {
			p.recover("plist.parse.date@1", protocol.CategorySyntax,
				p.rawLocation(closeStart, closeEnd), nil)
			return nil, nil
		}
	case elementData:
		if frame.content.Len() == 0 {
			if frame.selfClosing {
				p.recover("plist.parse.empty-value@1", protocol.CategorySyntax,
					p.rawLocation(closeStart, closeEnd), map[string]string{"element": "data"})
				return nil, nil
			}
			dataValue := NewPlistValueData(NewPlistDataFromBytes(nil))
			reference, failure := p.arenaAdd(dataValue)
			if failure != nil {
				return nil, failure
			}
			return &reference, nil
		}
		if frame.scalarUnproven {
			return nil, nil
		}
		bytes, ok := decodeBase64Text(frame.content.String())
		if ok {
			if len(bytes) > p.limits.MaxDataBytes {
				return nil, p.fatalLimit("data-bytes", len(bytes), p.limits.MaxDataBytes)
			}
			dataValue := NewPlistValueData(NewPlistDataFromBytes(bytes))
			value = &dataValue
		} else {
			p.recover("plist.parse.data@1", protocol.CategorySyntax,
				p.rawLocation(closeStart, closeEnd), nil)
			return nil, nil
		}
	case elementTrue, elementFalse:
		if frame.scalarUnproven {
			return nil, nil
		}
		booleanValue := NewPlistValueBoolean(NewPlistBoolean(*frame.kind == elementTrue))
		value = &booleanValue
	case elementPlist:
		return nil, nil
	}
	if value == nil {
		return nil, nil
	}
	reference, failure := p.arenaAdd(*value)
	if failure != nil {
		return nil, failure
	}
	return &reference, nil
}

func (p *xmlParser) arenaAdd(value PlistValue) (PlistValueRef, *FormationFailure) {
	reference, err := p.arena.Add(value)
	if err != nil {
		if arenaError, ok := err.(*PlistArenaError); ok &&
			arenaError.Kind == PlistArenaErrorObjectLimitExceeded {
			return PlistValueRef{}, p.fatalLimit("object-count", p.arena.NodeCount(),
				arenaError.Limit)
		}
		return PlistValueRef{}, p.internalFailure()
	}
	return reference, nil
}

func (p *xmlParser) text(token *xmlToken) *FormationFailure {
	if p.unknownDepth > 0 {
		return nil
	}
	text := p.decoded[token.start:token.end]
	switch p.textPosition() {
	case textOutside, textContainer:
		if allWhitespace(text) {
			return p.pushWhitespacePieces(token.start, token.end)
		}
		if failure := p.pushPiece(token.start, token.end,
			PlistSyntaxKindErrorRegion, document.PieceErrorRegion); failure != nil {
			return failure
		}
		p.recover("plist.parse.text-outside-value@1", protocol.CategorySyntax,
			p.rawLocation(token.start, token.end), nil)
	case textBoolean:
		if allWhitespace(text) {
			return p.pushWhitespacePieces(token.start, token.end)
		}
		if failure := p.pushPiece(token.start, token.end,
			PlistSyntaxKindErrorRegion, document.PieceErrorRegion); failure != nil {
			return failure
		}
		p.recover("plist.parse.boolean-content@1", protocol.CategorySyntax,
			p.rawLocation(token.start, token.end), nil)
		if len(p.stack) > 0 {
			p.stack[len(p.stack)-1].scalarUnproven = true
		}
	case textScalar:
		resolved, failure := p.resolveFragments(token.start, token.end, normalizationText, true)
		if failure != nil {
			return failure
		}
		if len(p.stack) > 0 {
			p.stack[len(p.stack)-1].content.WriteString(resolved)
		}
	}
	return nil
}

func (p *xmlParser) cdata(token *xmlToken) *FormationFailure {
	if p.unknownDepth > 0 {
		return nil
	}
	switch p.textPosition() {
	case textOutside, textContainer:
		if failure := p.pushPiece(token.start, token.end,
			PlistSyntaxKindErrorRegion, document.PieceErrorRegion); failure != nil {
			return failure
		}
		p.recover("plist.parse.text-outside-value@1", protocol.CategorySyntax,
			p.rawLocation(token.start, token.end), nil)
	case textBoolean:
		if failure := p.pushPiece(token.start, token.end,
			PlistSyntaxKindErrorRegion, document.PieceErrorRegion); failure != nil {
			return failure
		}
		p.recover("plist.parse.boolean-content@1", protocol.CategorySyntax,
			p.rawLocation(token.start, token.end), nil)
		if len(p.stack) > 0 {
			p.stack[len(p.stack)-1].scalarUnproven = true
		}
	case textScalar:
		if failure := p.pushPiece(token.start, token.start+cdataOpenBytes,
			PlistSyntaxKindCdataOpen, document.PieceToken); failure != nil {
			return failure
		}
		if failure := p.pushPiece(token.cdataTextStart, token.cdataTextEnd,
			PlistSyntaxKindCdataText, document.PieceToken); failure != nil {
			return failure
		}
		if failure := p.pushPiece(token.cdataTextEnd, token.end,
			PlistSyntaxKindCdataClose, document.PieceToken); failure != nil {
			return failure
		}
		if len(p.stack) > 0 {
			p.stack[len(p.stack)-1].content.WriteString(
				appendNormalized(p.decoded[token.cdataTextStart:token.cdataTextEnd], normalizationText))
		}
	}
	return nil
}

func (p *xmlParser) textPosition() textPosition {
	if len(p.stack) == 0 {
		return textOutside
	}
	frame := p.stack[len(p.stack)-1]
	if frame.kind == nil {
		return textOutside
	}
	switch *frame.kind {
	case elementPlist, elementDict, elementArray:
		return textContainer
	case elementTrue, elementFalse:
		return textBoolean
	default:
		return textScalar
	}
}

// resolveFragments splits one decoded span into Text/EntityReference/
// CharacterReference pieces and returns the resolved normalized content
// (RFC 0013 §4.9). Failing references resolve to nothing and publish a
// diagnostic; the remaining proven fragments still form the native text.
func (p *xmlParser) resolveFragments(start, end int, mode normalization,
	emitPieces bool) (string, *FormationFailure) {
	bytes := p.decoded[start:end]
	content := ""
	if !strings.Contains(bytes, "&") {
		if emitPieces {
			if failure := p.pushPiece(start, end, PlistSyntaxKindText, document.PieceToken); failure != nil {
				return "", failure
			}
		}
		return appendNormalized(bytes, mode), nil
	}
	cursor := 0
	index := 0
	for index < len(bytes) {
		relative := strings.IndexByte(bytes[index:], '&')
		if relative < 0 {
			break
		}
		at := index + relative
		if at > cursor {
			if emitPieces {
				if failure := p.pushPiece(start+cursor, start+at,
					PlistSyntaxKindText, document.PieceToken); failure != nil {
					return "", failure
				}
			}
			content += appendNormalized(bytes[cursor:at], mode)
		}
		semi := at + 1 + strings.IndexByte(bytes[at+1:], ';')
		if semi < at+1 {
			// Unterminated reference: recover and keep the rest literal.
			p.recover("plist.parse.reference@1", protocol.CategorySyntax,
				p.rawLocation(start+at, end), nil)
			if emitPieces {
				if failure := p.pushPiece(start+at, end,
					PlistSyntaxKindText, document.PieceToken); failure != nil {
					return "", failure
				}
			}
			content += appendNormalized(bytes[at:], mode)
			return content, nil
		}
		body := bytes[at+1 : semi]
		resolved, failure := p.resolveReference(body, start+at, start+semi+1)
		if failure != nil {
			return "", failure
		}
		if resolved != 0 {
			if emitPieces {
				kind := PlistSyntaxKindEntityReference
				if strings.HasPrefix(body, "#") {
					kind = PlistSyntaxKindCharacterReference
				}
				if pushFailure := p.pushPiece(start+at, start+semi+1,
					kind, document.PieceToken); pushFailure != nil {
					return "", pushFailure
				}
			}
			content += string(resolved)
		}
		cursor = semi + 1
		index = semi + 1
	}
	if cursor < len(bytes) {
		if emitPieces {
			if failure := p.pushPiece(start+cursor, end,
				PlistSyntaxKindText, document.PieceToken); failure != nil {
				return "", failure
			}
		}
		content += appendNormalized(bytes[cursor:], mode)
	}
	return content, nil
}

// resolveReference resolves one `&…;` reference body; a zero rune is a
// recovered failure that contributes nothing to the native text.
func (p *xmlParser) resolveReference(body string, rawStart, rawEnd int) (rune, *FormationFailure) {
	if strings.HasPrefix(body, "#") {
		digits := body[1:]
		isHex := false
		if strings.HasPrefix(digits, "x") || strings.HasPrefix(digits, "X") {
			isHex = true
			digits = digits[1:]
		}
		valid := len(digits) > 0
		for index := 0; index < len(digits); index++ {
			if isHex {
				if !isHexDigit(digits[index]) {
					valid = false
				}
			} else if !isASCIIDigit(digits[index]) {
				valid = false
			}
		}
		value := uint64(0)
		if valid {
			parsed, err := strconv.ParseUint(digits, map[bool]int{true: 16, false: 10}[isHex], 32)
			if err != nil {
				valid = false
			} else {
				value = parsed
			}
		}
		if valid {
			if scalar := rune(value); isXMLChar(scalar) {
				return scalar, nil
			}
		}
		p.recover("plist.parse.reference@1", protocol.CategorySyntax,
			p.rawLocation(rawStart, rawEnd), nil)
		return 0, nil
	}
	if body == "" {
		p.recover("plist.parse.reference@1", protocol.CategorySyntax,
			p.rawLocation(rawStart, rawEnd), nil)
		return 0, nil
	}
	switch body {
	case "lt":
		return '<', nil
	case "gt":
		return '>', nil
	case "amp":
		return '&', nil
	case "apos":
		return '\'', nil
	case "quot":
		return '"', nil
	}
	p.recover("plist.parse.entity@1", protocol.CategoryConformance,
		p.rawLocation(rawStart, rawEnd), map[string]string{"name": body})
	return 0, nil
}

// pushWhitespacePieces splits one decoded whitespace-only run into
// Whitespace and LineBreak trivia pieces; defensive non-whitespace bytes
// become error regions.
func (p *xmlParser) pushWhitespacePieces(start, end int) *FormationFailure {
	bytes := p.decoded
	type run struct{ start, end int }
	var runs []run
	cursor := start
	for cursor < end {
		byte := bytes[cursor]
		if !isXMLSpaceByte(byte) {
			runStart := cursor
			for cursor < end && !isXMLSpaceByte(bytes[cursor]) {
				cursor++
			}
			runs = append(runs, run{runStart, cursor})
			continue
		}
		lineBreak := byte == '\n' || byte == '\r'
		runStart := cursor
		if byte == '\r' && cursor+1 < end && bytes[cursor+1] == '\n' {
			cursor += 2
		} else {
			cursor++
		}
		for cursor < end && (bytes[cursor] == '\n' || bytes[cursor] == '\r') == lineBreak {
			cursor++
		}
		runs = append(runs, run{runStart, cursor})
	}
	for _, item := range runs {
		kind := PlistSyntaxKindWhitespace
		structural := document.PieceTrivia
		if item.start < end && !isXMLSpaceByte(bytes[item.start]) {
			kind = PlistSyntaxKindErrorRegion
			structural = document.PieceErrorRegion
		} else if item.start < end && (bytes[item.start] == '\n' || bytes[item.start] == '\r') {
			kind = PlistSyntaxKindLineBreak
		}
		if failure := p.pushPiece(item.start, item.end, kind, structural); failure != nil {
			return failure
		}
	}
	return nil
}

func (p *xmlParser) recoverErrorRegion(start, end int) *FormationFailure {
	p.recovered = true
	raw, failure := p.rawSpan(start, end)
	if failure != nil {
		return failure
	}
	if p.unknownDepth == 0 {
		if pushFailure := p.pushPiece(raw.StartByte(), raw.EndByte(),
			PlistSyntaxKindErrorRegion, document.PieceErrorRegion); pushFailure != nil {
			return pushFailure
		}
	}
	p.recover("plist.parse.well-formedness@1", protocol.CategorySyntax,
		p.locationOfRaw(raw.StartByte(), raw.EndByte()), nil)
	return nil
}

// finish assembles the final document: recovery diagnostics, the native
// arena, the exhaustive piece coverage, and the formation status.
func (p *xmlParser) finish() (*Document, *FormationFailure) {
	if len(p.stack) > 0 {
		frame := p.stack[len(p.stack)-1]
		p.recover("plist.parse.unclosed-element@1", protocol.CategorySyntax,
			p.locationOfRaw(frame.openStart, frame.openEnd),
			map[string]string{"element": frame.name})
	}
	for _, frame := range p.stack {
		if frame.unknownSubtreeStart != nil {
			if failure := p.pushPieceRaw(document.NewStructuralPiece(
				mustSpan(p.authority, *frame.unknownSubtreeStart, p.source.Len()),
				document.PieceErrorRegion), PlistSyntaxKindErrorRegion); failure != nil {
				return nil, failure
			}
		}
	}
	if !p.anyTopLevel {
		p.recover("plist.parse.missing-root@1", protocol.CategorySyntax, nil, nil)
	}
	var native *PlistDocument
	if p.plistRootSeen {
		switch p.rootValueCount {
		case 0:
			p.recover("plist.parse.root-value-count@1", protocol.CategorySyntax, nil,
				map[string]string{"count": "0"})
		case 1:
			if p.rootValueRef != nil {
				document, err := p.arena.Build(*p.rootValueRef)
				if err != nil {
					if arenaError, ok := err.(*PlistArenaError); ok {
						switch arenaError.Kind {
						case PlistArenaErrorContainerDepthLimitExceeded:
							return nil, p.fatalLimit("container-depth", arenaError.Node.Index(),
								arenaError.Limit)
						case PlistArenaErrorObjectLimitExceeded:
							return nil, p.fatalLimit("object-count", p.arena.NodeCount(),
								arenaError.Limit)
						}
					}
					return nil, p.internalFailure()
				}
				native = document
			}
		default:
			p.recover("plist.parse.root-value-count@1", protocol.CategorySyntax, nil,
				map[string]string{"count": itoa(p.rootValueCount)})
		}
	}
	status := document.FormationStatusComplete
	if p.recovered {
		status = document.FormationStatusRecovered
	}
	sourceLen := p.source.Len()
	// Pair every piece with its kind before any ordering, so sorting can
	// never desynchronize the two parallel arrays (parser_xml.rs finish).
	paired := make([]pieceKindPair, 0, len(p.pieces))
	for index := range p.pieces {
		paired = append(paired, pieceKindPair{piece: p.pieces[index], kind: p.kinds[index]})
	}
	sortPieces(paired)
	final := make([]pieceKindPair, 0, len(paired)+8)
	next := 0
	for _, item := range paired {
		start := item.piece.Span().StartByte()
		if start > next {
			gap := document.NewStructuralPiece(mustSpan(p.authority, next, start),
				pieceClassForGap(p.recovered))
			if failure := p.pushPieceRaw(gap,
				kindForGap(p.recovered)); failure != nil {
				return nil, failure
			}
		}
		next = item.piece.Span().EndByte()
		final = append(final, item)
	}
	if next < sourceLen {
		if failure := p.pushPieceRaw(
			document.NewStructuralPiece(mustSpan(p.authority, next, sourceLen),
				pieceClassForGap(p.recovered)),
			kindForGap(p.recovered)); failure != nil {
			return nil, failure
		}
	}
	// Gap pieces were pushed in increasing offset order; append them to the
	// final arrays, then pair and sort the complete set once.
	for index := len(paired); index < len(p.pieces); index++ {
		final = append(final, pieceKindPair{piece: p.pieces[index], kind: p.kinds[index]})
	}
	sortPieces(final)
	structuralPieces := make([]document.StructuralPiece, 0, len(final))
	pairedKinds := make([]PlistSyntaxKind, 0, len(final))
	errorRegions := 0
	for _, item := range final {
		structuralPieces = append(structuralPieces, item.piece)
		pairedKinds = append(pairedKinds, item.kind)
		if item.piece.Kind() == document.PieceErrorRegion {
			errorRegions++
		}
	}
	if errorRegions > p.limits.MaxRecoveryRegions {
		return nil, p.fatalLimit("recovery-regions", errorRegions, p.limits.MaxRecoveryRegions)
	}
	index, err := document.NewLosslessStructuralIndex(p.authority.Identity(), sourceLen, structuralPieces)
	if err != nil {
		return nil, p.coverageFailure()
	}
	return newXMLDocument(p, status, native, index, pairedKinds), nil
}

type pieceKindPair struct {
	piece document.StructuralPiece
	kind  PlistSyntaxKind
}

func sortPieces(paired []pieceKindPair) {
	for i := 1; i < len(paired); i++ {
		for j := i; j > 0 &&
			paired[j].piece.Span().StartByte() < paired[j-1].piece.Span().StartByte(); j-- {
			paired[j], paired[j-1] = paired[j-1], paired[j]
		}
	}
}

func pieceClassForGap(recovered bool) document.StructuralPieceKind {
	if recovered {
		return document.PieceErrorRegion
	}
	return document.PieceTrivia
}

func kindForGap(recovered bool) PlistSyntaxKind {
	if recovered {
		return PlistSyntaxKindErrorRegion
	}
	return PlistSyntaxKindWhitespace
}

// mustSpan builds one raw span; finish already validated every covered
// range, so a failure here is an internal invariant violation.
func mustSpan(authority document.DocumentAuthority, start, end int) document.Span {
	span, err := authority.Span(start, end)
	if err != nil {
		return document.Span{}
	}
	return span
}

// recover records one recovery diagnostic and marks the parse Recovered.
func (p *xmlParser) recover(code string, category protocol.DiagnosticCategory,
	location *protocol.SourceLocation, arguments map[string]string) {
	p.recovered = true
	p.sink.push(newDiagnostic(code, category, protocol.SeverityError, location, arguments, 0))
}

// pushPiece maps one decoded span to its raw span and records the piece.
func (p *xmlParser) pushPiece(start, end int, kind PlistSyntaxKind,
	structural document.StructuralPieceKind) *FormationFailure {
	raw, failure := p.rawSpan(start, end)
	if failure != nil {
		return failure
	}
	return p.pushPieceRaw(document.NewStructuralPiece(
		mustSpan(p.authority, raw.StartByte(), raw.EndByte()), structural), kind)
}

// pushPieceRaw records one already raw-mapped piece.
func (p *xmlParser) pushPieceRaw(piece document.StructuralPiece,
	kind PlistSyntaxKind) *FormationFailure {
	if len(p.pieces) >= p.limits.MaxSyntaxPieces {
		return p.fatalLimit("syntax-pieces", len(p.pieces), p.limits.MaxSyntaxPieces)
	}
	p.pieces = append(p.pieces, piece)
	p.kinds = append(p.kinds, kind)
	return nil
}

// rawSpan maps one decoded half-open span to its raw byte span.
func (p *xmlParser) rawSpan(start, end int) (document.Span, *FormationFailure) {
	rawStart, err := p.source.RawByteAt(document.NewUtf8ByteOffset(start))
	if err != nil {
		return document.Span{}, p.coordinatesFailure()
	}
	rawEnd, err := p.source.RawByteAt(document.NewUtf8ByteOffset(end))
	if err != nil {
		return document.Span{}, p.coordinatesFailure()
	}
	span, err := p.authority.Span(rawStart, rawEnd)
	if err != nil {
		return document.Span{}, p.coordinatesFailure()
	}
	return span, nil
}

// rawLocation maps one decoded span to its transferable location.
func (p *xmlParser) rawLocation(start, end int) *protocol.SourceLocation {
	raw, failure := p.rawSpan(start, end)
	if failure != nil {
		return nil
	}
	return p.locationOfRaw(raw.StartByte(), raw.EndByte())
}

func (p *xmlParser) locationOfRaw(start, end int) *protocol.SourceLocation {
	return locationOf(p.authority, start, end)
}

// fatalLimit builds the `plist.limit.<name>@1` resource-limit failure.
func (p *xmlParser) fatalLimit(name string, observed, limit int) *FormationFailure {
	return &FormationFailure{
		Kind: FormationFailureResourceLimit, Name: name, Observed: observed, Limit: limit,
		Diagnostics: []*protocol.Diagnostic{newDiagnostic("plist.limit."+name+"@1",
			protocol.CategoryResource, protocol.SeverityError, nil,
			map[string]string{"limit": itoa(limit), "observed": itoa(observed)}, 0)},
	}
}

func (p *xmlParser) coordinatesFailure() *FormationFailure {
	return &FormationFailure{
		Diagnostics: []*protocol.Diagnostic{newDiagnostic("plist.xml.coordinates@1",
			protocol.CategorySyntax, protocol.SeverityError, nil, nil, 0)},
	}
}

func (p *xmlParser) coverageFailure() *FormationFailure {
	return &FormationFailure{
		Diagnostics: []*protocol.Diagnostic{newDiagnostic("plist.xml.coverage@1",
			protocol.CategorySyntax, protocol.SeverityError, nil, nil, 0)},
	}
}

func (p *xmlParser) internalFailure() *FormationFailure {
	return &FormationFailure{
		Diagnostics: []*protocol.Diagnostic{newDiagnostic("plist.xml.internal@1",
			protocol.CategoryResource, protocol.SeverityError, nil, nil, 0)},
	}
}

// newXMLDocument wraps one formed XML parse into the shared Document.
func newXMLDocument(parser *xmlParser, status document.FormationStatus,
	native *PlistDocument, index *document.LosslessStructuralIndex,
	kinds []PlistSyntaxKind) *Document {
	return &Document{
		authority:      parser.authority,
		source:         parser.source,
		representation: PlistRepresentationXML,
		status:         status,
		diagnostics:    parser.sink.finish(),
		native:         native,
		xmlIndex:       index,
		xmlKinds:       kinds,
		limits:         parser.limits,
	}
}

// skipDeclarationSpaces skips XML declaration spaces forward.
func skipDeclarationSpaces(text string, rel int) int {
	for rel < len(text) && isXMLSpaceByte(text[rel]) {
		rel++
	}
	return rel
}

// appendNormalized appends literal text with the requested normalization
// (RFC 0013 §4.9 and XML 1.0 attribute normalization).
func appendNormalized(text string, mode normalization) string {
	var builder strings.Builder
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character == '\r' {
			if index+1 < len(text) && text[index+1] == '\n' {
				index++
			}
			character = '\n'
		}
		if mode == normalizationAttribute &&
			(character == ' ' || character == '\t' || character == '\n') {
			character = ' '
		}
		builder.WriteByte(character)
	}
	return builder.String()
}

// utf16Encode encodes one decoded text into UTF-16 code units.
func utf16Encode(text string) []uint16 {
	units := make([]uint16, 0, len(text))
	for _, rune := range text {
		if rune < 0x10000 {
			units = append(units, uint16(rune))
		} else {
			rune -= 0x10000
			units = append(units, uint16(0xD800+(rune>>10)), uint16(0xDC00+(rune&0x3FF)))
		}
	}
	return units
}

// codeUnitKey renders one key's exact code units as a string map key.
func codeUnitKey(key PlistKey) string {
	units := key.CodeUnits()
	builder := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		builder = append(builder, byte(unit>>8), byte(unit))
	}
	return string(builder)
}

func allWhitespace(text string) bool {
	for index := 0; index < len(text); index++ {
		if !isXMLSpaceByte(text[index]) {
			return false
		}
	}
	return true
}

func isHexDigit(byte byte) bool {
	return (byte >= '0' && byte <= '9') || (byte >= 'a' && byte <= 'f') || (byte >= 'A' && byte <= 'F')
}

// isXMLChar is the XML 1.0 `Char` production (RFC 0013 §4.9).
func isXMLChar(scalar rune) bool {
	return scalar == '\t' || scalar == '\n' || scalar == '\r' ||
		(scalar >= 0x20 && scalar <= 0xD7FF) ||
		(scalar >= 0xE000 && scalar <= 0xFFFD) ||
		(scalar >= 0x10000 && scalar <= 0x10FFFF)
}

// isXMLText reports whether every scalar of one well-formed UTF-16
// sequence is an XML 1.0 character; an unpaired surrogate is not.
func isXMLText(units []uint16) bool {
	index := 0
	for index < len(units) {
		unit := units[index]
		var scalar rune
		if unit >= 0xD800 && unit <= 0xDBFF {
			if index+1 >= len(units) {
				return false
			}
			low := units[index+1]
			if low < 0xDC00 || low > 0xDFFF {
				return false
			}
			index += 2
			scalar = rune(0x10000 + (uint32(unit)-0xD800)<<10 + (uint32(low) - 0xDC00))
		} else {
			index++
			scalar = rune(unit)
		}
		if !isXMLChar(scalar) {
			return false
		}
	}
	return true
}
