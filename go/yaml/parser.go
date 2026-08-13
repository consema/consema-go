package yaml

import (
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// The standard YAML tag prefix for `!!` shorthand tags.
const tagYamlPrefix = "tag:yaml.org,2002:"

// The native content discriminator (consema-yaml native.rs NativeContent).
const (
	contentScalar = iota
	contentSequence
	contentMapping
)

// nativeDocument is one composed independent document. Parsing records
// rune-index spans; conversion fills the raw-byte spans before the
// Document publishes.
type nativeDocument struct {
	root      int
	span      document.Span
	startRune int
	endRune   int
}

// nativeNode is one composed representation node.
type nativeNode struct {
	tag         string
	anchor      string
	hasAnchor   bool
	anchorSpan  document.Span
	span        document.Span
	anchorStart int
	anchorEnd   int
	start       int
	end         int
	content     nativeContent
}

// nativeContent is the closed node content union.
type nativeContent struct {
	kind    int
	scalar  nativeScalar
	items   []nativeSequenceItem
	entries []nativeMappingEntry
}

// nativeSequenceItem is one ordered sequence association.
type nativeSequenceItem struct {
	identity uint64
	node     int
	span     document.Span
	start    int
	end      int
	alias    *int
}

// nativeMappingEntry is one ordered mapping association.
type nativeMappingEntry struct {
	identity   uint64
	key        int
	value      int
	span       document.Span
	start      int
	end        int
	keyAlias   *int
	valueAlias *int
}

// nativeAlias is one alias serialization occurrence.
type nativeAlias struct {
	identity uint64
	name     string
	target   int
	span     document.Span
	start    int
	end      int
}

// nativeScalar is one resolved native scalar.
type nativeScalar struct {
	decoded   string
	canonical string
	kind      YamlScalarKind
	style     YamlScalarStyle
}

// nativeStream is the composed native stream.
type nativeStream struct {
	nodes     []nativeNode
	documents []nativeDocument
	aliases   []nativeAlias
}

// occurrence is one composed node occurrence in a collection edge.
type occurrence struct {
	node  int
	start int
	end   int
	alias *int
}

// properties carries one parsed node property set.
type properties struct {
	anchor      string
	anchorStart int
	anchorEnd   int
	tag         string
}

// parser is the self-written YAML 1.2.2 presentation parser producing the
// native composition directly (the Go counterpart of the private Rust
// backend plus Composer; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §1.2 leaves the
// internal tree free). All positions are decoded Unicode scalar offsets.
type parser struct {
	chars        []rune
	pos          int
	lineStart    int
	profile      YamlProfile
	limits       document.ParseLimits
	depth        int
	events       int
	nodes        []nativeNode
	anchors      map[string]int
	aliases      []nativeAlias
	association  uint64
	documents    []nativeDocument
	tagDirective map[string]string
	versionSeen  bool
	failed       *FormationFailure
}

// newParser prepares one parse over the decoded text (the BOM is retained;
// the scanner skips it as the tokenizer does).
func newParser(text string, profile YamlProfile, limits document.ParseLimits) *parser {
	chars := []rune(text)
	pos := 0
	if len(chars) > 0 && chars[0] == '\uFEFF' {
		pos = 1
	}
	return &parser{
		chars:        chars,
		pos:          pos,
		lineStart:    pos,
		profile:      profile,
		limits:       limits,
		anchors:      make(map[string]int),
		tagDirective: make(map[string]string),
	}
}

// failSyntax records the frozen yaml.parse.syntax@1 failure at one scalar
// offset.
func (p *parser) failSyntax() {
	if p.failed == nil {
		p.failed = newSyntaxFailure(p.pos)
	}
}

// failNative records one frozen native composition failure.
func (p *parser) failNative(code string) {
	if p.failed == nil {
		p.failed = newNativeFailure(code)
	}
}

// failResource records the frozen core.parse.resource-limit@1 failure.
func (p *parser) failResource(name string, observed, limit int) {
	if p.failed == nil {
		p.failed = resourceLimitFailure(name, observed, limit)
	}
}

// countEvent charges one backend event against max_token_count.
func (p *parser) countEvent() {
	p.events++
	if p.events > p.limits.MaxTokenCount {
		p.failResource("syntax-events", p.events, p.limits.MaxTokenCount)
	}
}

// countDepth charges one collection start against max_nesting_depth.
func (p *parser) countDepth() {
	p.depth++
	if p.depth > p.limits.MaxNestingDepth {
		p.failResource("nesting-depth", p.depth, p.limits.MaxNestingDepth)
	}
}

// reserveNode reserves one native node index (consema-yaml native.rs
// reserve_node).
func (p *parser) reserveNode() int {
	observed := len(p.nodes) + 1
	if observed > p.limits.MaxNodeCount {
		p.failResource("native-nodes", observed, p.limits.MaxNodeCount)
	}
	index := len(p.nodes)
	p.nodes = append(p.nodes, nativeNode{})
	return index
}

// associationIdentity allocates the next shared association identity.
func (p *parser) associationIdentity() uint64 {
	identity := p.association
	p.association++
	return identity
}

func (p *parser) atEOF() bool { return p.pos >= len(p.chars) }

// current returns the character at pos or -1 at EOF.
func (p *parser) current() rune {
	if p.atEOF() {
		return -1
	}
	return p.chars[p.pos]
}

// peek returns the character at pos+1 or -1.
func (p *parser) peek() rune {
	if p.pos+1 >= len(p.chars) {
		return -1
	}
	return p.chars[p.pos+1]
}

func isSeparation(character rune) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

func isFlowIndicator(character rune) bool {
	switch character {
	case '[', ']', '{', '}', ',':
		return true
	}
	return false
}

// atLineStart reports whether pos is at the start of a line.
func (p *parser) atLineStart() bool { return p.pos == p.lineStart }

// lineEndAt returns the rune offset of the line break ending the line
// containing offset (or the text length).
func (p *parser) lineEndAt(offset int) int {
	for offset < len(p.chars) {
		character := p.chars[offset]
		if character == '\r' || character == '\n' {
			return offset
		}
		offset++
	}
	return len(p.chars)
}

// lineIndentAt returns the leading space count of the line containing
// offset.
func (p *parser) lineIndentAt(offset int) int {
	start := offset
	for start > 0 {
		character := p.chars[start-1]
		if character == '\r' || character == '\n' {
			break
		}
		start--
	}
	count := 0
	for start+count < len(p.chars) && p.chars[start+count] == ' ' {
		count++
	}
	return count
}

// nextLineStart returns the offset after the line break at offset (CRLF
// counts as one break).
func nextLineStart(chars []rune, offset int) int {
	if offset < len(chars) && chars[offset] == '\r' && offset+1 < len(chars) && chars[offset+1] == '\n' {
		return offset + 2
	}
	return offset + 1
}

// followedBySeparation reports whether the character n ahead is a
// separation or end of text.
func (p *parser) followedBySeparation(n int) bool {
	offset := p.pos + n
	if offset >= len(p.chars) {
		return true
	}
	return isSeparation(p.chars[offset])
}

// atDocumentMarker reports whether pos is at `---` or `...` at the start
// of a line, followed by separation.
func (p *parser) atDocumentMarker(marker string) bool {
	if !p.atLineStart() {
		return false
	}
	runes := []rune(marker)
	for index, expected := range runes {
		if p.pos+index >= len(p.chars) || p.chars[p.pos+index] != expected {
			return false
		}
	}
	return p.followedBySeparation(len(runes))
}

// skipSeparationInline consumes spaces and tabs.
func (p *parser) skipSeparationInline() {
	for !p.atEOF() {
		character := p.current()
		if character != ' ' && character != '\t' {
			return
		}
		p.pos++
	}
}

// skipBlankLines advances over blank and comment-only lines, including
// their line breaks. A mid-line position stops the scan when the remainder
// is not blank.
func (p *parser) skipBlankLines() {
	for {
		if p.atEOF() {
			return
		}
		lineEnd := p.lineEndAt(p.pos)
		blank := true
		for offset := p.pos; offset < lineEnd; offset++ {
			character := p.chars[offset]
			if character == '#' {
				break
			}
			if character != ' ' && character != '\t' {
				blank = false
				break
			}
		}
		if !blank {
			return
		}
		if lineEnd >= len(p.chars) {
			p.pos = len(p.chars)
			return
		}
		p.pos = nextLineStart(p.chars, lineEnd)
		p.lineStart = p.pos
	}
}

// skipSeparationConsumingLines consumes spaces/tabs and any following
// blank or comment lines (used between block entries).
func (p *parser) skipSeparationConsumingLines() {
	p.skipSeparationInline()
	if p.atEOF() {
		return
	}
	character := p.current()
	if character == '\r' || character == '\n' {
		p.pos = nextLineStart(p.chars, p.pos)
		p.lineStart = p.pos
		p.skipBlankLines()
	}
}

// lineEndsCleanly reports whether the rest of the current line is only
// trailing whitespace, a comment, or the line end.
func (p *parser) lineEndsCleanly() bool {
	if p.atEOF() {
		return true
	}
	lineEnd := p.lineEndAt(p.pos)
	for offset := p.pos; offset < lineEnd; offset++ {
		character := p.chars[offset]
		if character == '#' {
			return true
		}
		if character != ' ' && character != '\t' {
			return false
		}
	}
	return true
}

// parseStream parses the complete stream.
func (p *parser) parseStream() {
	p.countEvent() // StreamStart
	if p.failed != nil {
		return
	}
	p.parseDirectives()
	for p.failed == nil {
		p.skipBlankLines()
		if p.atEOF() {
			break
		}
		if p.atDocumentMarker("---") {
			p.parseDocument(true)
		} else if p.atDocumentMarker("...") {
			p.failSyntax()
			return
		} else {
			p.parseDocument(false)
		}
		if p.failed != nil {
			return
		}
		// After one document: end markers and directives, then the next
		// document or the end of the stream.
		for p.failed == nil {
			p.skipBlankLines()
			if p.atEOF() {
				p.countEvent() // StreamEnd
				return
			}
			if p.atDocumentMarker("---") {
				break
			}
			if p.atDocumentMarker("...") {
				p.pos += 3
				p.parseDirectives()
				continue
			}
			p.failSyntax()
			return
		}
	}
	p.countEvent() // StreamEnd
}

// parseDirectives consumes directive lines at a directive position.
func (p *parser) parseDirectives() {
	for p.failed == nil && !p.atEOF() && p.atLineStart() && p.current() == '%' {
		lineEnd := p.lineEndAt(p.pos)
		line := string(p.chars[p.pos:lineEnd])
		p.pos = lineEnd
		p.countEvent()
		p.parseDirective(line)
		if p.failed != nil || lineEnd >= len(p.chars) {
			return
		}
		p.pos = nextLineStart(p.chars, lineEnd)
		p.lineStart = p.pos
	}
}

// parseDirective validates one directive line (yaml directives, tag
// directives, and reserved directives).
func (p *parser) parseDirective(line string) {
	text := strings.TrimRight(line, "\r")
	rest, ok := stripPrefixString(text, "%YAML")
	if ok {
		if len(rest) == 0 || (rest[0] != ' ' && rest[0] != '\t') {
			p.failSyntax()
			return
		}
		if p.versionSeen {
			p.failSyntax()
			return
		}
		p.versionSeen = true
		version := firstToken(rest)
		if version != "1.1" && version != "1.2" {
			p.failSyntax()
		}
		return
	}
	rest, ok = stripPrefixString(text, "%TAG")
	if !ok {
		return // reserved directive
	}
	fields := tokenFields(rest)
	if len(fields) < 2 {
		p.failSyntax()
		return
	}
	handle := fields[0]
	prefix := fields[1]
	if len(handle) < 2 || handle[0] != '!' || handle[len(handle)-1] != '!' {
		p.failSyntax()
		return
	}
	if handle == "!!" {
		p.failSyntax()
		return
	}
	if _, exists := p.tagDirective[handle]; exists {
		p.failSyntax()
		return
	}
	p.tagDirective[handle] = prefix
}

// parseDocument parses one explicit or implicit document.
func (p *parser) parseDocument(explicit bool) {
	if p.failed != nil {
		return
	}
	start := p.pos
	p.countEvent() // DocumentStart
	if explicit {
		p.pos += 3
		p.skipSeparationInline()
	}
	p.anchors = make(map[string]int)
	document := nativeDocument{}
	if p.failed == nil {
		occurrence, empty := p.parseDocumentContent(explicit)
		if p.failed != nil {
			return
		}
		if empty {
			index := p.reserveNode()
			p.countEvent() // the empty document composes one null node
			document.root = index
			p.nodes[index] = emptyNullNode(p.pos)
		} else {
			document.root = occurrence.node
		}
	}
	// Consume an optional `...` end marker at the start of a line.
	if p.failed == nil && !p.atEOF() && p.atLineStart() && p.atDocumentMarker("...") {
		p.pos += 3
	}
	p.countEvent() // DocumentEnd
	if p.failed != nil {
		return
	}
	if explicit {
		document.startRune = start
		document.endRune = p.pos
	} else {
		node := &p.nodes[document.root]
		document.startRune = node.start
		document.endRune = node.end
	}
	p.documents = append(p.documents, document)
}

// parseDocumentContent parses the content node of a document. The returned
// empty flag reports an empty document (no node content).
func (p *parser) parseDocumentContent(explicit bool) (occurrence, bool) {
	if !explicit && p.lineIndentAt(p.pos) > 0 {
		p.failSyntax()
		return occurrence{}, false
	}
	p.skipSeparationConsumingLines()
	if p.failed != nil {
		return occurrence{}, false
	}
	if p.atEOF() || p.atDocumentMarker("---") || p.atDocumentMarker("...") {
		return occurrence{}, true
	}
	node, err := p.parseBlockNode()
	if err != nil {
		p.failed = err
		return occurrence{}, false
	}
	return node, false
}

// emptyNullNode builds the resolved empty null scalar node at one offset.
func emptyNullNode(offset int) nativeNode {
	return nativeNode{
		tag: tagNull, start: offset, end: offset,
		content: nativeContent{kind: contentScalar,
			scalar: nativeScalar{decoded: "", canonical: "", kind: ScalarKindNull,
				style: ScalarStylePlain}},
	}
}

// parseBlockNode parses one block-context node starting at the current
// line, including its properties.
func (p *parser) parseBlockNode() (occurrence, *FormationFailure) {
	props, err := p.parseProperties()
	if err != nil {
		return occurrence{}, err
	}
	return p.parseBlockNodeWithProps(props)
}

// parseBlockNodeWithProps parses one block-context node whose properties
// were already consumed.
func (p *parser) parseBlockNodeWithProps(props properties) (occurrence, *FormationFailure) {
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.skipSeparationInline()
	if p.atEOF() || p.current() == '\r' || p.current() == '\n' || p.current() == '#' {
		// Properties followed by the line end: a nested block node on the
		// following lines, or the resolved empty node carrying the
		// properties.
		_, found := p.peekContentLine()
		if found {
			p.skipBlankLines()
			if p.failed != nil {
				return occurrence{}, p.failed
			}
			return p.parseBlockNodeWithProps(props)
		}
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		return p.parseEmptyPropertyNode(props)
	}
	character := p.current()
	if character == '-' && p.followedBySeparation(1) {
		return p.parseBlockSequence(p.lineIndentAt(p.pos), props)
	}
	if character == '?' && p.followedBySeparation(1) {
		return p.parseBlockMapping(p.lineIndentAt(p.pos), props)
	}
	if p.looksLikeImplicitKey() {
		// The mapping identity is reserved before the key node, matching
		// the backend event order (MappingStart precedes its children).
		index := p.reserveNode()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		if props.anchor != "" {
			p.anchors[props.anchor] = index
		}
		key, err := p.parseInlineNode()
		if err != nil {
			return occurrence{}, err
		}
		indent := p.lineIndentAt(p.pos)
		return p.parseBlockMappingWithFirstEntry(index, indent, key, props)
	}
	// A standalone node (not a mapping): flow, quoted, alias, block
	// scalar, or a plain scalar with continuation.
	switch character {
	case '[', '{':
		return p.parseFlowNode(props)
	case '\'', '"':
		return p.parseQuoted(character, props)
	case '*':
		if props.anchor != "" || props.tag != "" {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		return p.parseAlias(0)
	case '|', '>':
		return p.parseBlockScalar(character == '>', p.lineIndentAt(p.pos), props)
	default:
		return p.parsePlainBlock(props)
	}
}

// parseEmptyPropertyNode builds the resolved empty null scalar carrying
// one property set.
func (p *parser) parseEmptyPropertyNode(props properties) (occurrence, *FormationFailure) {
	marker := p.pos
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	node := emptyNullNode(marker)
	if props.tag != "" {
		resolved, scalar, err := resolveScalar("", ScalarStylePlain, props.tag, true, p.profile)
		if err != nil {
			return occurrence{}, err
		}
		node.tag = resolved
		node.content.scalar = scalar
	}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
		node.anchor = props.anchor
		node.hasAnchor = true
		node.anchorStart = props.anchorStart
		node.anchorEnd = props.anchorEnd
	}
	p.nodes[index] = node
	return occurrence{node: index, start: marker, end: marker}, nil
}

// parseBlockSequence parses a block sequence whose entries are at the
// given indentation.
func (p *parser) parseBlockSequence(indent int, props properties) (occurrence, *FormationFailure) {
	start := p.pos
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	if props.anchor != "" {
		// The anchor registers before the children so self-aliases resolve.
		p.anchors[props.anchor] = index
	}
	p.countDepth()
	p.countEvent() // SequenceStart
	var items []nativeSequenceItem
	for {
		if p.current() != '-' || !p.followedBySeparation(1) {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		p.pos++
		p.skipSeparationInline()
		item, err := p.parseNodeAfterIndicator(indent, true)
		if err != nil {
			return occurrence{}, err
		}
		items = append(items, nativeSequenceItem{
			identity: p.associationIdentity(),
			node:     item.node,
			start:    item.start,
			end:      item.end,
			alias:    item.alias,
		})
		// Next item?
		p.skipBlankLines()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		if p.atEOF() {
			break
		}
		if p.atDocumentMarker("---") || p.atDocumentMarker("...") {
			break
		}
		lineIndent := p.lineIndentAt(p.pos)
		if lineIndent < indent {
			break
		}
		if lineIndent > indent || !p.atLineStart() {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		p.skipSeparationInline()
		if !(p.current() == '-' && p.followedBySeparation(1)) {
			break
		}
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.countEvent() // SequenceEnd
	p.depth--
	end := start
	if len(items) > 0 {
		end = items[len(items)-1].end
	}
	node := nativeNode{tag: tagSeq, start: start, end: end,
		content: nativeContent{kind: contentSequence, items: items}}
	return p.finishNode(index, node, props), nil
}

// finishNode attaches properties to one node and publishes it.
func (p *parser) finishNode(index int, node nativeNode, props properties) occurrence {
	if props.anchor != "" {
		node.anchor = props.anchor
		node.hasAnchor = true
		node.anchorStart = props.anchorStart
		node.anchorEnd = props.anchorEnd
	}
	p.nodes[index] = node
	return occurrence{node: index, start: node.start, end: node.end}
}

// parseBlockMapping parses a block mapping whose entries are at the given
// indentation.
func (p *parser) parseBlockMapping(indent int, props properties) (occurrence, *FormationFailure) {
	start := p.pos
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
	}
	p.countDepth()
	p.countEvent() // MappingStart
	entries := make([]nativeMappingEntry, 0, 4)
	for {
		entry, more, err := p.parseMappingEntry(indent)
		if err != nil {
			return occurrence{}, err
		}
		if !more {
			break
		}
		entries = append(entries, entry)
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.countEvent() // MappingEnd
	p.depth--
	end := start
	if len(entries) > 0 {
		end = entries[len(entries)-1].end
	}
	node := nativeNode{tag: tagMap, start: start, end: end,
		content: nativeContent{kind: contentMapping, entries: entries}}
	return p.finishNode(index, node, props), nil
}

// parseBlockMappingWithFirstEntry continues a block mapping whose first
// implicit key entry was already parsed into the pre-reserved node index.
func (p *parser) parseBlockMappingWithFirstEntry(index, indent int,
	key occurrence, props properties) (occurrence, *FormationFailure) {
	start := key.start
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.countDepth()
	p.countEvent() // MappingStart
	entries := make([]nativeMappingEntry, 0, 4)
	entry, err := p.parseImplicitEntry(indent, key)
	if err != nil {
		return occurrence{}, err
	}
	entries = append(entries, entry)
	for {
		next, more, err := p.parseMappingEntry(indent)
		if err != nil {
			return occurrence{}, err
		}
		if !more {
			break
		}
		entries = append(entries, next)
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.countEvent() // MappingEnd
	p.depth--
	end := start
	if len(entries) > 0 {
		end = entries[len(entries)-1].end
	}
	node := nativeNode{tag: tagMap, start: start, end: end,
		content: nativeContent{kind: contentMapping, entries: entries}}
	return p.finishNode(index, node, props), nil
}

// parseMappingEntry parses the next block-mapping entry at the mapping's
// indentation; more is false when the mapping ends.
func (p *parser) parseMappingEntry(indent int) (nativeMappingEntry, bool, *FormationFailure) {
	p.skipBlankLines()
	if p.failed != nil {
		return nativeMappingEntry{}, false, p.failed
	}
	if p.atEOF() {
		return nativeMappingEntry{}, false, nil
	}
	if p.atDocumentMarker("---") || p.atDocumentMarker("...") {
		return nativeMappingEntry{}, false, nil
	}
	if !p.atLineStart() {
		p.failSyntax()
		return nativeMappingEntry{}, false, p.failed
	}
	lineIndent := p.lineIndentAt(p.pos)
	if lineIndent < indent {
		return nativeMappingEntry{}, false, nil
	}
	if lineIndent > indent {
		p.failSyntax()
		return nativeMappingEntry{}, false, p.failed
	}
	p.skipSeparationInline()
	character := p.current()
	if character == '?' && p.followedBySeparation(1) {
		entry, err := p.parseExplicitEntry(indent)
		return entry, true, err
	}
	key, ok, err := p.parseMappingKeyCandidate()
	if err != nil {
		return nativeMappingEntry{}, false, err
	}
	if !ok {
		return nativeMappingEntry{}, false, nil
	}
	entry, err := p.parseImplicitEntry(indent, key)
	return entry, true, err
}

// parseExplicitEntry parses one `? key` / `: value` mapping entry.
func (p *parser) parseExplicitEntry(indent int) (nativeMappingEntry, *FormationFailure) {
	marker := p.pos
	p.pos++ // consume '?'
	p.skipSeparationInline()
	var key occurrence
	if p.atEOF() || p.current() == '\r' || p.current() == '\n' || p.current() == '#' {
		keyIndex := p.reserveNode()
		p.nodes[keyIndex] = emptyNullNode(marker)
		key = occurrence{node: keyIndex, start: marker, end: marker}
	} else {
		var err *FormationFailure
		key, err = p.parseInlineNode()
		if err != nil {
			return nativeMappingEntry{}, err
		}
	}
	// Expect the `:` value indicator on this line or the next line at the
	// mapping's indentation.
	p.skipSeparationInline()
	colonFound := false
	if !p.atEOF() && p.current() == ':' && p.followedBySeparation(1) {
		colonFound = true
		p.pos++
		p.skipSeparationInline()
	} else if p.atEOF() || p.current() == '\r' || p.current() == '\n' || p.current() == '#' {
		saved := p.pos
		p.skipBlankLines()
		if !p.atEOF() && p.atLineStart() && p.lineIndentAt(p.pos) == indent &&
			p.current() == ':' && p.followedBySeparation(1) {
			colonFound = true
			p.pos++
			p.skipSeparationInline()
		} else {
			p.pos = saved
		}
	}
	if p.failed != nil {
		return nativeMappingEntry{}, p.failed
	}
	if !colonFound {
		p.failSyntax()
		return nativeMappingEntry{}, p.failed
	}
	value, err := p.parseNodeAfterIndicator(indent, false)
	if err != nil {
		return nativeMappingEntry{}, err
	}
	return nativeMappingEntry{
		identity:   p.associationIdentity(),
		key:        key.node,
		value:      value.node,
		start:      key.start,
		end:        value.end,
		keyAlias:   key.alias,
		valueAlias: value.alias,
	}, nil
}

// parseImplicitEntry parses the `: value` continuation of one implicit
// key.
func (p *parser) parseImplicitEntry(indent int, key occurrence) (nativeMappingEntry, *FormationFailure) {
	p.skipSeparationInline()
	if p.atEOF() || p.current() != ':' {
		p.failSyntax()
		return nativeMappingEntry{}, p.failed
	}
	p.pos++ // consume ':'
	p.skipSeparationInline()
	value, err := p.parseNodeAfterIndicator(indent, false)
	if err != nil {
		return nativeMappingEntry{}, err
	}
	return nativeMappingEntry{
		identity:   p.associationIdentity(),
		key:        key.node,
		value:      value.node,
		start:      key.start,
		end:        value.end,
		keyAlias:   key.alias,
		valueAlias: value.alias,
	}, nil
}

// parseMappingKeyCandidate detects and parses one implicit mapping key.
func (p *parser) parseMappingKeyCandidate() (occurrence, bool, *FormationFailure) {
	if !p.looksLikeImplicitKey() {
		return occurrence{}, false, nil
	}
	key, err := p.parseInlineNode()
	if err != nil {
		return occurrence{}, false, err
	}
	return key, true, nil
}

// looksLikeImplicitKey scans the current line for a mapping `:` separator
// without building any node (the single-line implicit-key rule).
func (p *parser) looksLikeImplicitKey() bool {
	if p.atEOF() {
		return false
	}
	lineEnd := p.lineEndAt(p.pos)
	offset := p.pos
	character := p.chars[offset]
	switch {
	case character == '\'' || character == '"':
		after := p.scanQuotedLookahead(offset)
		if after < 0 || after > lineEnd {
			return false
		}
		offset = after
		for offset < lineEnd && (p.chars[offset] == ' ' || p.chars[offset] == '\t') {
			offset++
		}
	case character == '[' || character == '{':
		after := p.scanFlowLookahead(offset)
		if after < 0 || after > lineEnd {
			return false
		}
		offset = after
		for offset < lineEnd && (p.chars[offset] == ' ' || p.chars[offset] == '\t') {
			offset++
		}
	default:
		for offset < lineEnd {
			character := p.chars[offset]
			if isSeparation(character) || isFlowIndicator(character) {
				break
			}
			if character == ':' && p.colonFollowsSep(offset) {
				break
			}
			offset++
		}
		if offset >= lineEnd {
			return false
		}
		if p.chars[offset] != ':' {
			return false
		}
	}
	if offset >= lineEnd {
		return false
	}
	if p.chars[offset] != ':' {
		return false
	}
	return p.colonFollowsSep(offset)
}

// colonFollowsSep reports whether the `:` at offset is followed by a
// separation, a flow indicator, or the end of text.
func (p *parser) colonFollowsSep(offset int) bool {
	if offset+1 >= len(p.chars) {
		return true
	}
	next := p.chars[offset+1]
	return isSeparation(next) || isFlowIndicator(next)
}

// scanQuotedLookahead scans one quoted scalar and returns the offset after
// its closing quote, or -1 when unterminated.
func (p *parser) scanQuotedLookahead(cursor int) int {
	quote := p.chars[cursor]
	cursor++
	for cursor < len(p.chars) {
		character := p.chars[cursor]
		if character == quote {
			if quote == '\'' && cursor+1 < len(p.chars) && p.chars[cursor+1] == '\'' {
				cursor += 2
				continue
			}
			return cursor + 1
		}
		if character == '\\' && quote == '"' {
			cursor += 2
			continue
		}
		cursor++
	}
	return -1
}

// scanFlowLookahead scans one flow collection and returns the offset after
// its matching closing bracket, or -1 when unterminated or spanning lines.
func (p *parser) scanFlowLookahead(cursor int) int {
	depth := 0
	for cursor < len(p.chars) {
		character := p.chars[cursor]
		switch character {
		case '\'', '"':
			after := p.scanQuotedLookahead(cursor)
			if after < 0 {
				return -1
			}
			cursor = after
			continue
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return cursor + 1
			}
		case '\r', '\n':
			return -1
		}
		cursor++
	}
	return -1
}

// parseNodeAfterIndicator parses the value node following a `- `, `? `, or
// `: ` indicator whose parent block context has the given indentation.
func (p *parser) parseNodeAfterIndicator(parentIndent int, allowCompact bool) (occurrence, *FormationFailure) {
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	marker := p.pos
	p.skipSeparationInline()
	if p.atEOF() || p.current() == '\r' || p.current() == '\n' || p.current() == '#' {
		return p.parseEmptyValue(parentIndent, marker)
	}
	node, err := p.parseInlineValue(parentIndent, allowCompact)
	if err != nil {
		return occurrence{}, err
	}
	// A same-line value must end at a line boundary, a comment, or the end
	// of the stream; anything else (e.g. a second `: ` on the line) is a
	// syntax error.
	if p.failed == nil && !p.atEOF() && !p.atLineStart() && !p.lineEndsCleanly() {
		p.failSyntax()
		return occurrence{}, p.failed
	}
	return node, nil
}

// parseEmptyValue parses the value of an indicator whose line carries no
// content.
func (p *parser) parseEmptyValue(parentIndent, marker int) (occurrence, *FormationFailure) {
	next, found := p.peekContentLine()
	if found && next > parentIndent {
		p.skipBlankLines()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		return p.parseBlockNode()
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	index := p.reserveNode()
	p.countEvent() // the empty node composes one null node
	p.nodes[index] = emptyNullNode(marker)
	return occurrence{node: index, start: marker, end: marker}, nil
}

// peekContentLine returns the indentation of the next content line, or
// false when the stream ends before one.
func (p *parser) peekContentLine() (int, bool) {
	offset := p.pos
	for {
		if offset >= len(p.chars) {
			return 0, false
		}
		lineEnd := p.lineEndAt(offset)
		blank := true
		for cursor := offset; cursor < lineEnd; cursor++ {
			character := p.chars[cursor]
			if character == '#' {
				break
			}
			if character != ' ' && character != '\t' {
				blank = false
				break
			}
		}
		if !blank {
			return p.lineIndentAt(offset), true
		}
		if lineEnd >= len(p.chars) {
			return 0, false
		}
		offset = nextLineStart(p.chars, lineEnd)
	}
}

// parseInlineValue parses the value node whose content starts at the
// current position on the indicator line.
func (p *parser) parseInlineValue(parentIndent int, allowCompact bool) (occurrence, *FormationFailure) {
	props, err := p.parseProperties()
	if err != nil {
		return occurrence{}, err
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.skipSeparationInline()
	if p.atEOF() || p.current() == '\r' || p.current() == '\n' || p.current() == '#' {
		next, found := p.peekContentLine()
		if found && next > parentIndent {
			p.skipBlankLines()
			if p.failed != nil {
				return occurrence{}, p.failed
			}
			return p.parseBlockNodeWithProps(props)
		}
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		return p.parseEmptyPropertyNode(props)
	}
	character := p.current()
	switch {
	case character == '[' || character == '{':
		return p.parseFlowNode(props)
	case character == '\'' || character == '"':
		return p.parseQuoted(character, props)
	case character == '*':
		if props.anchor != "" || props.tag != "" {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		return p.parseAlias(0)
	case character == '|' || character == '>':
		return p.parseBlockScalar(character == '>', parentIndent, props)
	case character == '-' && p.followedBySeparation(1):
		if props.anchor != "" || props.tag != "" {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		return p.parseBlockSequence(p.pos-p.lineStart, properties{})
	case character == '?' && p.followedBySeparation(1):
		if !allowCompact {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		if props.anchor != "" || props.tag != "" {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		return p.parseBlockMapping(p.pos-p.lineStart, properties{})
	default:
		if p.looksLikeImplicitKey() && allowCompact {
			// Compact block mapping value (`- key: value`), indented at the
			// key's column; the mapping identity is reserved first.
			index := p.reserveNode()
			if p.failed != nil {
				return occurrence{}, p.failed
			}
			key, err := p.parseInlineNode()
			if err != nil {
				return occurrence{}, err
			}
			return p.parseBlockMappingWithFirstEntry(index, key.start-p.lineStart,
				key, properties{})
		}
		return p.parsePlainBlock(props)
	}
}

// parseInlineNode parses one single-line-capable node: properties, flow
// collections, quoted scalars, aliases, and plain scalars (without
// continuation). Used for implicit keys.
func (p *parser) parseInlineNode() (occurrence, *FormationFailure) {
	props, err := p.parseProperties()
	if err != nil {
		return occurrence{}, err
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.skipSeparationInline()
	if p.atEOF() || p.current() == '\r' || p.current() == '\n' || p.current() == '#' {
		p.failSyntax()
		return occurrence{}, p.failed
	}
	character := p.current()
	switch {
	case character == '[' || character == '{':
		return p.parseFlowNode(props)
	case character == '\'' || character == '"':
		return p.parseQuoted(character, props)
	case character == '*':
		if props.anchor != "" || props.tag != "" {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		return p.parseAlias(0)
	default:
		return p.parsePlainSingleLine(props)
	}
}

// parseProperties consumes `&anchor` and `!tag` properties in any order.
func (p *parser) parseProperties() (properties, *FormationFailure) {
	var props properties
	for {
		character := p.current()
		if character == '&' {
			if props.anchor != "" {
				p.failSyntax()
				return props, p.failed
			}
			start := p.pos
			p.pos++
			name := p.scanPropertyName()
			if name == "" {
				p.failSyntax()
				return props, p.failed
			}
			props.anchor = name
			props.anchorStart = start
			props.anchorEnd = p.pos
		} else if character == '!' {
			if props.tag != "" {
				p.failSyntax()
				return props, p.failed
			}
			value, err := p.parseTag()
			if err != nil {
				return props, err
			}
			props.tag = value
		} else {
			break
		}
		// Properties must be followed by separation or the line end; the
		// separation is consumed so the next property (or the node) is
		// reached.
		if p.atEOF() || isSeparation(p.current()) {
			p.skipSeparationInline()
			continue
		}
		p.failSyntax()
		return props, p.failed
	}
	return props, nil
}

// scanPropertyName scans the characters of one anchor or alias name (the
// lexeme between the marker and the next separation or flow indicator).
func (p *parser) scanPropertyName() string {
	start := p.pos
	for !p.atEOF() {
		character := p.current()
		if isSeparation(character) || isFlowIndicator(character) {
			break
		}
		p.pos++
	}
	return string(p.chars[start:p.pos])
}

// parseTag parses one `!` tag spelling and resolves it to the full tag
// identifier.
func (p *parser) parseTag() (string, *FormationFailure) {
	p.pos++ // consume '!'
	if p.current() == '<' {
		p.pos++
		start := p.pos
		for !p.atEOF() && p.current() != '>' {
			p.pos++
		}
		if p.atEOF() {
			p.failSyntax()
			return "", p.failed
		}
		verbatim := string(p.chars[start:p.pos])
		p.pos++ // consume '>'
		if verbatim == "" {
			p.failSyntax()
			return "", p.failed
		}
		return verbatim, nil
	}
	text := p.scanPropertyName()
	if text == "" {
		return "!", nil // the non-specific tag
	}
	if index := strings.IndexByte(text, '!'); index >= 0 {
		handle := "!" + text[:index+1]
		suffix := text[index+1:]
		if handle == "!!" {
			return tagYamlPrefix + suffix, nil
		}
		prefix, ok := p.tagDirective[handle]
		if !ok {
			p.failSyntax()
			return "", p.failed
		}
		return prefix + suffix, nil
	}
	return "!" + text, nil
}

// parseAlias parses one `*name` alias occurrence.
func (p *parser) parseAlias(marker int) (occurrence, *FormationFailure) {
	start := p.pos
	p.pos++
	name := p.scanPropertyName()
	if name == "" {
		p.failSyntax()
		return occurrence{}, p.failed
	}
	target, ok := p.anchors[name]
	if !ok {
		// Undefined aliases fail at parse time (yaml.parse.syntax@1), the
		// vector-pinned surface; the composer's defensive anchor.unknown
		// path is unreachable here.
		p.failSyntax()
		return occurrence{}, p.failed
	}
	ordinal := len(p.aliases)
	p.aliases = append(p.aliases, nativeAlias{
		identity: p.associationIdentity(),
		name:     name,
		target:   target,
		start:    start,
		end:      p.pos,
	})
	p.countEvent() // one node occurrence
	_ = marker
	return occurrence{node: target, start: start, end: p.pos, alias: intPtr(ordinal)}, nil
}

func intPtr(value int) *int { return &value }

// parsePlainSingleLine parses one plain scalar without continuation
// (implicit keys).
func (p *parser) parsePlainSingleLine(props properties) (occurrence, *FormationFailure) {
	start := p.pos
	decoded := p.scanPlainLine()
	end := p.pos
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	style := ScalarStylePlain
	var resolvedTag string
	var scalar nativeScalar
	if props.tag != "" {
		var err *FormationFailure
		resolvedTag, scalar, err = resolveScalar(decoded, style, props.tag, true, p.profile)
		if err != nil {
			return occurrence{}, err
		}
	} else {
		var err *FormationFailure
		resolvedTag, scalar, err = resolveImplicit(decoded, p.profile)
		if err != nil {
			return occurrence{}, err
		}
	}
	node := nativeNode{tag: resolvedTag, start: start, end: end,
		content: nativeContent{kind: contentScalar, scalar: scalar}}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
		node.anchor = props.anchor
		node.hasAnchor = true
		node.anchorStart = props.anchorStart
		node.anchorEnd = props.anchorEnd
	}
	p.nodes[index] = node
	p.countEvent()
	return occurrence{node: index, start: start, end: end}, nil
}

// parsePlainBlock parses one plain scalar with block continuation rules.
func (p *parser) parsePlainBlock(props properties) (occurrence, *FormationFailure) {
	start := p.pos
	firstLineIndent := p.lineIndentAt(p.pos)
	var parts []string
	for {
		before := p.pos
		parts = append(parts, p.scanBlockPlainLine())
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		if p.atEOF() || !p.peekContinuationLine(firstLineIndent) {
			break
		}
		p.skipBlankLines()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		p.skipSeparationInline()
		if p.pos == before {
			// The scanner ended at a stop character (a mapping `:` or a
			// comment) without consuming input, and the continuation
			// lookahead misread the same position as more scalar content.
			// The scalar is complete; the caller resumes at the stop
			// character. Without this guard the loop spins forever.
			break
		}
	}
	decoded := strings.Join(parts, " ")
	end := p.pos
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	style := ScalarStylePlain
	var resolvedTag string
	var scalar nativeScalar
	if props.tag != "" {
		var err *FormationFailure
		resolvedTag, scalar, err = resolveScalar(decoded, style, props.tag, true, p.profile)
		if err != nil {
			return occurrence{}, err
		}
	} else {
		var err *FormationFailure
		resolvedTag, scalar, err = resolveImplicit(decoded, p.profile)
		if err != nil {
			return occurrence{}, err
		}
	}
	node := nativeNode{tag: resolvedTag, start: start, end: end,
		content: nativeContent{kind: contentScalar, scalar: scalar}}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
		node.anchor = props.anchor
		node.hasAnchor = true
		node.anchorStart = props.anchorStart
		node.anchorEnd = props.anchorEnd
	}
	p.nodes[index] = node
	p.countEvent()
	return occurrence{node: index, start: start, end: end}, nil
}

// scanPlainLine scans one plain-scalar line fragment.
func (p *parser) scanPlainLine() string {
	start := p.pos
	for !p.atEOF() {
		character := p.current()
		if isSeparation(character) || isFlowIndicator(character) {
			break
		}
		if character == ':' && p.colonFollowsSep(p.pos) {
			break
		}
		p.pos++
	}
	return string(p.chars[start:p.pos])
}

// scanBlockPlainLine scans one block-context plain-scalar fragment. In
// block context interior spaces are scalar content (saphyr-compatible:
// `name: Rust CI` is one scalar), stopping before a comment, a mapping
// `:` separator, a flow indicator, or the line end.
func (p *parser) scanBlockPlainLine() string {
	start := p.pos
	for !p.atEOF() {
		character := p.current()
		if character == ' ' || character == '	' {
			// Look ahead: trailing separation before a stop is not content.
			offset := p.pos
			for offset < len(p.chars) && (p.chars[offset] == ' ' || p.chars[offset] == '	') {
				offset++
			}
			if offset >= len(p.chars) || p.chars[offset] == '\r' || p.chars[offset] == '\n' ||
				p.chars[offset] == '#' ||
				(p.chars[offset] == ':' && p.colonFollowsSep(offset)) {
				break
			}
			p.pos = offset
			continue
		}
		if character == '\r' || character == '\n' {
			break
		}
		// A `#` ends the scalar only at a token boundary (preceded by
		// separation); mid-word hashes are content (`k:#foo`).
		if character == '#' && p.pos > start &&
			(p.chars[p.pos-1] == ' ' || p.chars[p.pos-1] == '\t') {
			break
		}
		if character == ':' && p.colonFollowsSep(p.pos) {
			break
		}
		p.pos++
	}
	return string(p.chars[start:p.pos])
}

// peekContinuationLine reports whether the next content line continues a
// plain scalar whose first line has the given indentation.
func (p *parser) peekContinuationLine(firstLineIndent int) bool {
	offset := p.pos
	if offset >= len(p.chars) {
		return false
	}
	// Skip the line break and any blank/comment lines.
	for offset < len(p.chars) {
		if p.chars[offset] != '\r' && p.chars[offset] != '\n' {
			break
		}
		offset = nextLineStart(p.chars, offset)
	}
	for offset < len(p.chars) {
		lineEnd := p.lineEndAt(offset)
		blank := true
		for cursor := offset; cursor < lineEnd; cursor++ {
			character := p.chars[cursor]
			if character == '#' {
				break
			}
			if character != ' ' && character != '\t' {
				blank = false
				break
			}
		}
		if !blank {
			break
		}
		if lineEnd >= len(p.chars) {
			return false
		}
		offset = nextLineStart(p.chars, lineEnd)
	}
	if offset >= len(p.chars) {
		return false
	}
	indent := p.lineIndentAt(offset)
	if indent <= firstLineIndent {
		return false
	}
	// The continuation must not start an indented structure.
	lineEnd := p.lineEndAt(offset)
	for cursor := offset + indent; cursor < lineEnd; cursor++ {
		character := p.chars[cursor]
		if character == '#' {
			break
		}
		if (character == '-' || character == '?') && (cursor+1 >= lineEnd ||
			isSeparation(p.chars[cursor+1])) {
			return false
		}
		if character == ':' && (cursor+1 >= lineEnd || isSeparation(p.chars[cursor+1])) {
			return false
		}
	}
	return true
}

// parseQuoted parses one single- or double-quoted scalar.
func (p *parser) parseQuoted(quote rune, props properties) (occurrence, *FormationFailure) {
	start := p.pos
	p.pos++ // opening quote
	var decoded strings.Builder
	for {
		if p.atEOF() {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		character := p.current()
		if character == quote {
			if quote == '\'' && p.peek() == '\'' {
				decoded.WriteRune('\'')
				p.pos += 2
				continue
			}
			p.pos++ // closing quote
			break
		}
		if character == '\\' && quote == '"' {
			value, err := p.parseEscape()
			if err != nil {
				return occurrence{}, err
			}
			if value >= 0 {
				decoded.WriteRune(value)
			}
			continue
		}
		if character == '\r' || character == '\n' {
			p.foldQuotedBreak(&decoded)
			continue
		}
		decoded.WriteRune(character)
		p.pos++
	}
	end := p.pos
	text := decoded.String()
	style := ScalarStyleDoubleQuoted
	if quote == '\'' {
		style = ScalarStyleSingleQuoted
	}
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	var resolvedTag string
	var scalar nativeScalar
	if props.tag != "" {
		var err *FormationFailure
		resolvedTag, scalar, err = resolveScalar(text, style, props.tag, true, p.profile)
		if err != nil {
			return occurrence{}, err
		}
	} else {
		resolvedTag = tagStr
		scalar = nativeScalar{decoded: text, canonical: text, kind: ScalarKindString, style: style}
	}
	node := nativeNode{tag: resolvedTag, start: start, end: end,
		content: nativeContent{kind: contentScalar, scalar: scalar}}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
		node.anchor = props.anchor
		node.hasAnchor = true
		node.anchorStart = props.anchorStart
		node.anchorEnd = props.anchorEnd
	}
	p.nodes[index] = node
	p.countEvent()
	return occurrence{node: index, start: start, end: end}, nil
}

// foldQuotedBreak folds one quoted-scalar line break: a single break
// becomes a space, breaks adjacent to blank lines become line breaks, and
// leading whitespace of the continuation is stripped.
func (p *parser) foldQuotedBreak(decoded *strings.Builder) {
	breaks := 0
	for !p.atEOF() {
		character := p.current()
		if character != '\r' && character != '\n' {
			break
		}
		breaks++
		p.pos = nextLineStart(p.chars, p.pos)
		p.lineStart = p.pos
	}
	// Leading whitespace of the continuation line is stripped.
	for !p.atEOF() {
		character := p.current()
		if character == ' ' || character == '\t' {
			p.pos++
			continue
		}
		break
	}
	if breaks == 1 {
		decoded.WriteRune(' ')
		return
	}
	for index := 1; index < breaks; index++ {
		decoded.WriteRune('\n')
	}
}

// parseEscape parses one double-quoted escape sequence. A line-continuation
// escape returns -1 (nothing emitted).
func (p *parser) parseEscape() (rune, *FormationFailure) {
	p.pos++ // consume '\\'
	if p.atEOF() {
		p.failSyntax()
		return 0, p.failed
	}
	character := p.current()
	p.pos++
	switch character {
	case '0':
		return 0, nil
	case 'a':
		return '\a', nil
	case 'b':
		return '\b', nil
	case 't':
		return '\t', nil
	case 'n':
		return '\n', nil
	case 'v':
		return '\v', nil
	case 'f':
		return '\f', nil
	case 'r':
		return '\r', nil
	case 'e':
		return 0x1B, nil
	case ' ':
		return ' ', nil
	case '"':
		return '"', nil
	case '/':
		return '/', nil
	case '\\':
		return '\\', nil
	case 'N':
		return 0x85, nil
	case '_':
		return 0xA0, nil
	case 'L':
		return 0x2028, nil
	case 'P':
		return 0x2029, nil
	case 'x':
		return p.parseHexEscape(2)
	case 'u':
		return p.parseHexEscape(4)
	case 'U':
		return p.parseHexEscape(8)
	case '\r', '\n':
		// Line continuation: consume the break and the leading whitespace
		// of the next line; nothing is emitted.
		p.pos = nextLineStart(p.chars, p.pos-1)
		p.lineStart = p.pos
		for !p.atEOF() {
			next := p.current()
			if next == ' ' || next == '\t' {
				p.pos++
				continue
			}
			break
		}
		return -1, nil
	}
	p.failSyntax()
	return 0, p.failed
}

// parseHexEscape parses one \x / \u / \U escape with the given digit
// count.
func (p *parser) parseHexEscape(digits int) (rune, *FormationFailure) {
	value := rune(0)
	for index := 0; index < digits; index++ {
		if p.atEOF() {
			p.failSyntax()
			return 0, p.failed
		}
		character := p.current()
		var digit rune
		switch {
		case character >= '0' && character <= '9':
			digit = character - '0'
		case character >= 'a' && character <= 'f':
			digit = character - 'a' + 10
		case character >= 'A' && character <= 'F':
			digit = character - 'A' + 10
		default:
			p.failSyntax()
			return 0, p.failed
		}
		value = value<<4 | digit
		p.pos++
	}
	if value > 0x10FFFF || (value >= 0xD800 && value <= 0xDFFF) {
		p.failSyntax()
		return 0, p.failed
	}
	return value, nil
}

// parseFlowNode parses one flow-context collection node.
func (p *parser) parseFlowNode(props properties) (occurrence, *FormationFailure) {
	switch p.current() {
	case '[':
		return p.parseFlowSequence(props)
	case '{':
		return p.parseFlowMapping(props)
	}
	p.failSyntax()
	return occurrence{}, p.failed
}

// skipFlowSeparation skips spaces, tabs, line breaks, and comments inside
// flow collections.
func (p *parser) skipFlowSeparation() {
	for !p.atEOF() {
		character := p.current()
		switch character {
		case ' ', '\t':
			p.pos++
		case '\r', '\n':
			p.pos = nextLineStart(p.chars, p.pos)
			p.lineStart = p.pos
		case '#':
			lineEnd := p.lineEndAt(p.pos)
			p.pos = lineEnd
		default:
			return
		}
	}
}

// parseFlowSequence parses one `[...]` flow sequence.
func (p *parser) parseFlowSequence(props properties) (occurrence, *FormationFailure) {
	start := p.pos
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
	}
	p.countDepth()
	p.countEvent() // SequenceStart
	p.pos++        // consume '['
	var items []nativeSequenceItem
	done := false
	for !done {
		p.skipFlowSeparation()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		if p.atEOF() {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		if p.current() == ']' {
			p.pos++
			break
		}
		if p.current() == ',' {
			// An empty entry.
			marker := p.pos
			p.pos++
			empty := p.reserveNode()
			p.countEvent()
			p.nodes[empty] = emptyNullNode(marker)
			items = append(items, nativeSequenceItem{
				identity: p.associationIdentity(),
				node:     empty, start: marker, end: marker,
			})
			continue
		}
		entry, err := p.parseFlowEntry()
		if err != nil {
			return occurrence{}, err
		}
		items = append(items, nativeSequenceItem{
			identity: p.associationIdentity(),
			node:     entry.node, start: entry.start, end: entry.end, alias: entry.alias,
		})
		p.skipFlowSeparation()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		if p.atEOF() {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		switch p.current() {
		case ',':
			p.pos++
			continue
		case ']':
			p.pos++
			done = true
		default:
			p.failSyntax()
			return occurrence{}, p.failed
		}
		if done {
			break
		}
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.countEvent() // SequenceEnd
	p.depth--
	end := start
	if len(items) > 0 {
		end = items[len(items)-1].end
	}
	node := nativeNode{tag: tagSeq, start: start, end: end,
		content: nativeContent{kind: contentSequence, items: items}}
	return p.finishFlowNode(index, node, props), nil
}

// finishFlowNode resolves a flow collection tag, attaches properties, and
// publishes the node.
func (p *parser) finishFlowNode(index int, node nativeNode, props properties) occurrence {
	if props.tag != "" {
		expected := tagSeq
		if node.content.kind == contentMapping {
			expected = tagMap
		}
		if resolved, err := resolveCollectionTag(props.tag, true, expected); err == nil {
			node.tag = resolved
		} else {
			p.failed = err
			return occurrence{}
		}
	}
	return p.finishNode(index, node, props)
}

// parseFlowMapping parses one `{...}` flow mapping.
func (p *parser) parseFlowMapping(props properties) (occurrence, *FormationFailure) {
	start := p.pos
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
	}
	p.countDepth()
	p.countEvent() // MappingStart
	p.pos++        // consume '{'
	var entries []nativeMappingEntry
	for {
		p.skipFlowSeparation()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		if p.atEOF() {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		if p.current() == '}' {
			p.pos++
			break
		}
		if p.current() == ',' {
			p.pos++
			continue
		}
		var key occurrence
		var err *FormationFailure
		if p.current() == '?' && p.followedBySeparation(1) {
			p.pos++
			p.skipSeparationInline()
			key, err = p.parseFlowValueNode()
			if err != nil {
				return occurrence{}, err
			}
			p.skipSeparationInline()
			if !p.atEOF() && p.current() == ':' && (p.followedBySeparation(1) ||
				p.colonFollowsFlow(p.pos)) {
				p.pos++
				p.skipSeparationInline()
			} else if p.atEOF() || (p.current() != ',' && p.current() != '}') {
				p.failSyntax()
				return occurrence{}, p.failed
			}
		} else {
			key, err = p.parseFlowValueNode()
			if err != nil {
				return occurrence{}, err
			}
			p.skipSeparationInline()
			if !p.atEOF() && p.current() == ':' && (p.followedBySeparation(1) ||
				p.colonFollowsFlow(p.pos)) {
				p.pos++
				p.skipSeparationInline()
			} else if p.atEOF() || (p.current() != ',' && p.current() != '}') {
				p.failSyntax()
				return occurrence{}, p.failed
			}
		}
		// The value: empty when the entry ends, else a flow node.
		p.skipFlowSeparation()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		var value occurrence
		if !p.atEOF() && p.current() != ',' && p.current() != '}' {
			value, err = p.parseFlowValueNode()
			if err != nil {
				return occurrence{}, err
			}
		} else {
			marker := p.pos
			empty := p.reserveNode()
			p.countEvent()
			p.nodes[empty] = emptyNullNode(marker)
			value = occurrence{node: empty, start: marker, end: marker}
		}
		entries = append(entries, nativeMappingEntry{
			identity:   p.associationIdentity(),
			key:        key.node,
			value:      value.node,
			start:      key.start,
			end:        value.end,
			keyAlias:   key.alias,
			valueAlias: value.alias,
		})
		p.skipFlowSeparation()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		if p.atEOF() {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		if p.current() == ',' {
			p.pos++
			continue
		}
		if p.current() == '}' {
			p.pos++
			break
		}
		p.failSyntax()
		return occurrence{}, p.failed
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.countEvent() // MappingEnd
	p.depth--
	end := start
	if len(entries) > 0 {
		end = entries[len(entries)-1].end
	}
	node := nativeNode{tag: tagMap, start: start, end: end,
		content: nativeContent{kind: contentMapping, entries: entries}}
	return p.finishFlowNode(index, node, props), nil
}

// colonFollowsFlow reports whether the `:` at pos is followed by a
// separation, a flow indicator, or the end of text.
func (p *parser) colonFollowsFlow(pos int) bool {
	return p.colonFollowsSep(pos)
}

// parseFlowEntry parses one flow sequence entry, including the single-pair
// mapping shorthand.
func (p *parser) parseFlowEntry() (occurrence, *FormationFailure) {
	first, err := p.parseFlowValueNode()
	if err != nil {
		return occurrence{}, err
	}
	p.skipSeparationInline()
	if !p.atEOF() && p.current() == ':' && (p.followedBySeparation(1) ||
		p.colonFollowsFlow(p.pos)) {
		// Single-pair mapping entry `[k: v]`.
		p.pos++
		p.skipSeparationInline()
		var value occurrence
		if !p.atEOF() && p.current() != ',' && p.current() != ']' {
			value, err = p.parseFlowValueNode()
			if err != nil {
				return occurrence{}, err
			}
		} else {
			marker := p.pos
			empty := p.reserveNode()
			p.countEvent()
			p.nodes[empty] = emptyNullNode(marker)
			value = occurrence{node: empty, start: marker, end: marker}
		}
		index := p.reserveNode()
		if p.failed != nil {
			return occurrence{}, p.failed
		}
		p.nodes[index] = nativeNode{
			tag: tagMap, start: first.start, end: value.end,
			content: nativeContent{kind: contentMapping, entries: []nativeMappingEntry{{
				identity: p.associationIdentity(),
				key:      first.node, value: value.node,
				start: first.start, end: value.end,
			}}},
		}
		p.countEvent()
		return occurrence{node: index, start: first.start, end: value.end}, nil
	}
	return first, nil
}

// parseFlowValueNode parses one flow-context value node.
func (p *parser) parseFlowValueNode() (occurrence, *FormationFailure) {
	props, err := p.parseProperties()
	if err != nil {
		return occurrence{}, err
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	p.skipSeparationInline()
	if p.atEOF() {
		p.failSyntax()
		return occurrence{}, p.failed
	}
	character := p.current()
	switch character {
	case '[', '{':
		return p.parseFlowNode(props)
	case '\'', '"':
		return p.parseQuoted(character, props)
	case '*':
		if props.anchor != "" || props.tag != "" {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		return p.parseAlias(0)
	default:
		if isFlowIndicator(character) {
			p.failSyntax()
			return occurrence{}, p.failed
		}
		return p.parsePlainFlow(props)
	}
}

// parsePlainFlow parses one flow-context plain scalar with line folding.
func (p *parser) parsePlainFlow(props properties) (occurrence, *FormationFailure) {
	start := p.pos
	var decoded strings.Builder
	for {
		if p.atEOF() {
			break
		}
		character := p.current()
		if character == ' ' || character == '\t' {
			// Interior separation is scalar content when the entry does
			// not end there (saphyr-compatible flow plain scalars).
			offset := p.pos
			for offset < len(p.chars) && (p.chars[offset] == ' ' || p.chars[offset] == '\t') {
				offset++
			}
			if offset >= len(p.chars) || p.chars[offset] == '\r' || p.chars[offset] == '\n' ||
				p.chars[offset] == '#' || isFlowIndicator(p.chars[offset]) ||
				(p.chars[offset] == ':' && p.colonFollowsSep(offset)) {
				break
			}
			for p.pos < offset {
				decoded.WriteRune(p.chars[p.pos])
				p.pos++
			}
			continue
		}
		if character == '\r' || character == '\n' {
			// Fold the break when the next line continues the scalar.
			offset := p.pos
			for offset < len(p.chars) && (p.chars[offset] == '\r' || p.chars[offset] == '\n') {
				offset = nextLineStart(p.chars, offset)
			}
			for offset < len(p.chars) && (p.chars[offset] == ' ' || p.chars[offset] == '\t') {
				offset++
			}
			if offset >= len(p.chars) || isFlowIndicator(p.chars[offset]) ||
				p.chars[offset] == '#' {
				break
			}
			decoded.WriteRune(' ')
			p.pos = offset
			continue
		}
		if isFlowIndicator(character) {
			break
		}
		if character == ':' && p.colonFollowsSep(p.pos) {
			break
		}
		decoded.WriteRune(character)
		p.pos++
	}
	end := p.pos
	text := decoded.String()
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	var resolvedTag string
	var scalar nativeScalar
	if props.tag != "" {
		var err *FormationFailure
		resolvedTag, scalar, err = resolveScalar(text, ScalarStylePlain, props.tag, true, p.profile)
		if err != nil {
			return occurrence{}, err
		}
	} else {
		var err *FormationFailure
		resolvedTag, scalar, err = resolveImplicit(text, p.profile)
		if err != nil {
			return occurrence{}, err
		}
	}
	node := nativeNode{tag: resolvedTag, start: start, end: end,
		content: nativeContent{kind: contentScalar, scalar: scalar}}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
		node.anchor = props.anchor
		node.hasAnchor = true
		node.anchorStart = props.anchorStart
		node.anchorEnd = props.anchorEnd
	}
	p.nodes[index] = node
	p.countEvent()
	return occurrence{node: index, start: start, end: end}, nil
}

// parseBlockScalar parses one `|` or `>` block scalar.
func (p *parser) parseBlockScalar(folded bool, parentIndent int,
	props properties) (occurrence, *FormationFailure) {
	start := p.pos
	p.pos++ // the header indicator
	chomping := byte(0)
	indentDigit := 0
	for {
		if p.atEOF() {
			break
		}
		character := p.current()
		if character == '+' || character == '-' {
			if chomping != 0 {
				p.failSyntax()
				return occurrence{}, p.failed
			}
			chomping = byte(character)
			p.pos++
			continue
		}
		if character >= '1' && character <= '9' {
			if indentDigit != 0 {
				p.failSyntax()
				return occurrence{}, p.failed
			}
			indentDigit = int(character - '0')
			p.pos++
			continue
		}
		break
	}
	// The rest of the header line must be spaces, tabs, and an optional
	// comment.
	lineEnd := p.lineEndAt(p.pos)
	for p.pos < lineEnd {
		character := p.current()
		if character == ' ' || character == '\t' {
			p.pos++
			continue
		}
		if character == '#' {
			p.pos = lineEnd
			break
		}
		p.failSyntax()
		return occurrence{}, p.failed
	}
	contentIndent := 0
	if indentDigit != 0 {
		contentIndent = parentIndent + indentDigit
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	// Scan the content lines. The content region ends with a line break
	// when the last accepted line carries one; that trailing break
	// participates in folding and chomping.
	var lines []string
	endedWithBreak := false
	if p.atEOF() {
		// Header at EOF: no content.
	} else {
		p.pos = nextLineStart(p.chars, p.pos)
		p.lineStart = p.pos
	}
	for {
		if p.atEOF() {
			break
		}
		lineEnd := p.lineEndAt(p.pos)
		blank := true
		for cursor := p.pos; cursor < lineEnd; cursor++ {
			character := p.chars[cursor]
			if character != ' ' && character != '\t' {
				blank = false
				break
			}
		}
		if !blank {
			indent := p.lineIndentAt(p.pos)
			if indent <= parentIndent {
				break
			}
			if contentIndent == 0 {
				contentIndent = indent
			}
			if indent < contentIndent {
				p.failSyntax()
				return occurrence{}, p.failed
			}
		}
		// Accept the line: strip the content indentation.
		text := ""
		if !blank {
			text = string(p.chars[p.pos+contentIndent : lineEnd])
		}
		lines = append(lines, text)
		if lineEnd >= len(p.chars) {
			if lineEnd > 0 && (p.chars[lineEnd-1] == '\n' || p.chars[lineEnd-1] == '\r') {
				endedWithBreak = true
			}
			p.pos = len(p.chars)
			break
		}
		endedWithBreak = true
		p.pos = nextLineStart(p.chars, lineEnd)
		p.lineStart = p.pos
	}
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	end := p.pos
	trailing := ""
	if endedWithBreak {
		trailing = "\n"
	}
	var decoded string
	if folded {
		decoded = foldBlockLines(lines) + trailing
	} else {
		decoded = strings.Join(lines, "\n") + trailing
	}
	switch chomping {
	case '-':
		decoded = strings.TrimRight(decoded, "\n")
	case '+':
	default:
		decoded = clipChomp(decoded)
	}
	style := ScalarStyleLiteral
	if folded {
		style = ScalarStyleFolded
	}
	index := p.reserveNode()
	if p.failed != nil {
		return occurrence{}, p.failed
	}
	var resolvedTag string
	var scalar nativeScalar
	if props.tag != "" {
		var err *FormationFailure
		resolvedTag, scalar, err = resolveScalar(decoded, style, props.tag, true, p.profile)
		if err != nil {
			return occurrence{}, err
		}
	} else {
		resolvedTag = tagStr
		scalar = nativeScalar{decoded: decoded, canonical: decoded,
			kind: ScalarKindString, style: style}
	}
	node := nativeNode{tag: resolvedTag, start: start, end: end,
		content: nativeContent{kind: contentScalar, scalar: scalar}}
	if props.anchor != "" {
		p.anchors[props.anchor] = index
		node.anchor = props.anchor
		node.hasAnchor = true
		node.anchorStart = props.anchorStart
		node.anchorEnd = props.anchorEnd
	}
	p.nodes[index] = node
	p.countEvent()
	return occurrence{node: index, start: start, end: end}, nil
}

// foldBlockLines applies the YAML folded-scalar folding rules to one
// block-scalar line list.
func foldBlockLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(lines[0])
	for index := 1; index < len(lines); index++ {
		if lines[index-1] == "" || lines[index] == "" {
			builder.WriteRune('\n')
		} else {
			builder.WriteRune(' ')
		}
		builder.WriteString(lines[index])
	}
	return builder.String()
}

// clipChomp applies the clip chomping: trailing line breaks are removed
// and a single break is retained when the content has a final break.
func clipChomp(decoded string) string {
	if decoded == "" {
		return ""
	}
	trimmed := strings.TrimRight(decoded, "\n")
	if trimmed == "" {
		return ""
	}
	if len(trimmed) != len(decoded) {
		return trimmed + "\n"
	}
	return trimmed
}

// validateVersionDirectives rejects `%YAML` version directives that do not
// match the selected profile (consema-yaml lib.rs validate_version_directives).
func validateVersionDirectives(text string, profile YamlProfile) *FormationFailure {
	lineIndex := 0
	for _, line := range strings.Split(text, "\n") {
		lineIndex++
		line = strings.TrimSuffix(line, "\r")
		line = strings.TrimPrefix(line, "\uFEFF")
		rest, ok := strings.CutPrefix(line, "%YAML")
		if !ok {
			continue
		}
		if len(rest) == 0 || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		version := firstToken(rest)
		if version == profile.acceptedVersion() {
			continue
		}
		return newFormationFailure("yaml.profile.version-directive@1",
			protocol.CategoryConformance, -1, -1, map[string]string{
				"selected_profile": profile.ID().ID(),
				"declared_version": version,
				"line":             itoa(lineIndex),
			})
	}
	return nil
}

// firstToken returns the first space/tab-separated token of one directive
// rest (a `#` comment ends the token).
func firstToken(rest string) string {
	rest = strings.TrimLeft(rest, " \t")
	for index := 0; index < len(rest); index++ {
		character := rest[index]
		if character == ' ' || character == '\t' || character == '#' {
			return rest[:index]
		}
	}
	return rest
}

// tokenFields splits one directive rest into space/tab-separated fields.
func tokenFields(rest string) []string {
	var fields []string
	for {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return fields
		}
		token := firstToken(rest)
		fields = append(fields, token)
		rest = rest[len(token):]
	}
}

func stripPrefixString(text, prefix string) (string, bool) {
	if strings.HasPrefix(text, prefix) {
		return text[len(prefix):], true
	}
	return text, false
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
