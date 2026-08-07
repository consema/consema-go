package ini

import (
	"sort"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the lossless INI parser (crates/consema-ini
// parser.rs). Physical lines are scanned over the decoded text first with
// the physical limits; each line is then classified per profile into
// comments, sections, entries, continuations, or recovered error records;
// duplicate and case-equivalence groups are assigned deterministically;
// and the exhaustive syntax pieces are emitted in raw source order.

// scannedLine is one physical line expressed in decoded UTF-8 byte
// offsets.
type scannedLine struct {
	decodedStart      int
	decodedContentEnd int
	decodedBreakStart int
	decodedEnd        int
	physicalIndex     int
}

// pythonEntryState tracks one active Python entry for continuation
// joining.
type pythonEntryState struct {
	entryIndex        int
	logicalIndex      int
	indent            int
	continuationLines int
	logicalBytes      int
	logicalScalars    int
	pendingBlankLines []int
}

// parser carries the formation state; a limit failure sets failed and
// stops the parse with no partial Document.
type parser struct {
	source              *document.SourceSnapshot
	profile             IniProfile
	limits              IniParseLimits
	authority           document.DocumentAuthority
	nextNode            uint64
	lines               []scannedLine
	physicalLines       []IniPhysicalLine
	logicalLines        []IniLogicalLine
	sections            []IniSection
	entries             []IniEntry
	entrySectionIndices []int
	errorLines          []IniErrorLine
	pieces              []StructuralPiece
	kinds               []IniSyntaxKind
	diagnostics         []*protocol.Diagnostic
	occurrence          uint64
	recovered           bool
	currentSection      int
	pythonEntry         *pythonEntryState
	rootNode            document.NodeRef
	failed              *FormationFailure
}

// newParser scans the physical lines and prepares the parser
// (parser.rs:149-181).
func newParser(source *document.SourceSnapshot, profile IniProfile,
	limits IniParseLimits) *parser {
	p := &parser{
		source:         source,
		profile:        profile,
		limits:         limits,
		authority:      document.NewDocumentAuthority(),
		currentSection: -1,
	}
	p.rootNode = p.authority.NodeRef(0, document.RoleIniDocument)
	p.nextNode = 1
	p.scanPhysicalLines()
	return p
}

// parse completes the formation and returns the immutable Document
// (parser.rs:183-226).
func (p *parser) parse() (*Document, *FormationFailure) {
	if p.failed != nil {
		return nil, p.failed
	}
	p.pushBom()
	if p.failed != nil {
		return nil, p.failed
	}
	for lineIndex := range p.lines {
		if p.failed != nil {
			return nil, p.failed
		}
		p.parseLine(lineIndex)
		if p.failed != nil {
			return nil, p.failed
		}
		p.pushLineBreak(lineIndex)
		if p.failed != nil {
			return nil, p.failed
		}
	}
	if p.profile.isPortable() && len(p.sections) == 0 {
		at := p.source.Len()
		p.diagnostic("ini.parse.missing-section@1", protocol.CategoryConformance, at, at, true)
		if p.failed != nil {
			return nil, p.failed
		}
	}
	p.assignDuplicateGroups()
	if p.failed != nil {
		return nil, p.failed
	}
	index, err := NewLosslessStructuralIndex(p.authority.Identity(), p.source.Len(), p.pieces)
	if err != nil {
		return nil, resourceLimitFailure("source-coordinate-coverage", 1, 0)
	}
	sortDiagnostics(p.diagnostics)
	status := document.FormationStatusComplete
	if p.recovered {
		status = document.FormationStatusRecovered
	}
	return &Document{
		authority:       p.authority,
		source:          p.source,
		profile:         p.profile,
		index:           index,
		kinds:           p.kinds,
		formationStatus: status,
		diagnostics:     p.diagnostics,
		physicalLines:   p.physicalLines,
		logicalLines:    p.logicalLines,
		sections:        p.sections,
		entries:         p.entries,
		errorLines:      p.errorLines,
		limits:          p.limits,
		rootNode:        p.rootNode,
	}, nil
}

// scanPhysicalLines scans the decoded text into physical lines and issues
// their identities (parser.rs:228-301).
func (p *parser) scanPhysicalLines() {
	text, ok := p.source.DecodedText()
	if !ok {
		p.failed = profileFailure()
		return
	}
	start := 0
	if p.source.EncodingFacts().Bom() != nil && strings.HasPrefix(text, "\uFEFF") {
		start = len("\uFEFF")
	}
	type decodedLine struct {
		start, contentEnd, breakStart, end, scalarCount int
	}
	var decoded []decodedLine
	for start < len(text) {
		newline := strings.IndexByte(text[start:], '\n')
		var contentEnd, breakStart, end int
		if newline >= 0 {
			newline += start
			breakStart = newline
			if newline > start && text[newline-1] == '\r' {
				breakStart = newline - 1
			}
			contentEnd, end = breakStart, newline+1
		} else {
			contentEnd, breakStart, end = len(text), len(text), len(text)
		}
		observed := len(decoded) + 1
		p.checkLimit("physical-lines", observed, p.limits.MaxPhysicalLines)
		if p.failed != nil {
			return
		}
		decoded = append(decoded, decodedLine{
			start: start, contentEnd: contentEnd, breakStart: breakStart, end: end,
			scalarCount: strings.Count(text[start:contentEnd], "") - 1,
		})
		start = end
	}
	for _, line := range decoded {
		fullSpan, ok := p.rawSpan(line.start, line.end)
		if !ok {
			return
		}
		contentSpan, ok := p.rawSpan(line.start, line.contentEnd)
		if !ok {
			return
		}
		p.checkLimit("physical-line-bytes", fullSpan.Len(), p.limits.MaxPhysicalLineBytes)
		if p.failed != nil {
			return
		}
		p.checkLimit("physical-line-scalars", line.scalarCount, p.limits.MaxPhysicalLineScalars)
		if p.failed != nil {
			return
		}
		node, ok := p.issueNode(document.RoleIniPhysicalLine)
		if !ok {
			return
		}
		var lineBreak *document.Span
		if line.breakStart < line.end {
			span, ok := p.rawSpan(line.breakStart, line.end)
			if !ok {
				return
			}
			lineBreak = &span
		}
		physicalIndex := len(p.physicalLines)
		p.physicalLines = append(p.physicalLines, IniPhysicalLine{
			node: node, span: fullSpan, contentSpan: contentSpan, lineBreakSpan: lineBreak,
		})
		p.lines = append(p.lines, scannedLine{
			decodedStart: line.start, decodedContentEnd: line.contentEnd,
			decodedBreakStart: line.breakStart, decodedEnd: line.end, physicalIndex: physicalIndex,
		})
	}
}

// parseLine classifies one physical line (parser.rs:303-346).
func (p *parser) parseLine(lineIndex int) {
	line := p.lines[lineIndex]
	content := p.decodedContent(&line)
	if strings.ContainsAny(content, "\x00\r") {
		p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
		return
	}
	if p.profile.isPortable() {
		for _, byte := range []byte(content) {
			if byte != '\t' && (byte < 0x20 || byte > 0x7E) {
				p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
				return
			}
		}
	}
	if allHorizontal(content) {
		if len(content) > 0 {
			p.pushPiece(line.decodedStart, line.decodedContentEnd, PieceTrivia, SyntaxKindWhitespace)
			if p.failed != nil {
				return
			}
		}
		if p.profile.isPython() && p.pythonEntry != nil {
			p.pythonEntry.pendingBlankLines = append(p.pythonEntry.pendingBlankLines, lineIndex)
		}
		return
	}
	leading := leadingHorizontal(content)
	marker := byte(0)
	if leading < len(content) {
		marker = content[leading]
	}
	isComment := false
	if p.profile.isPython() {
		isComment = marker == ';' || marker == '#'
	} else {
		isComment = marker == ';'
	}
	if isComment {
		p.pushComment(&line, leading)
		return
	}
	switch {
	case p.profile.isPortable():
		p.parsePortableLine(lineIndex)
	case p.profile.isWindows():
		p.parseWindowsLine(lineIndex)
	default:
		p.parsePythonLine(lineIndex)
	}
}

// parsePortableLine parses one portable profile line (parser.rs:348-399).
func (p *parser) parsePortableLine(lineIndex int) {
	p.pythonEntry = nil
	line := p.lines[lineIndex]
	content := p.decodedContent(&line)
	if strings.HasPrefix(content, "[") {
		if line.decodedBreakStart == line.decodedEnd || !strings.HasSuffix(content, "]") ||
			len(content) < 3 {
			p.recoverLine(lineIndex, "ini.parse.malformed-section@1")
			return
		}
		name := content[1 : len(content)-1]
		for _, byte := range []byte(name) {
			if !isPortableName(byte) {
				p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
				return
			}
		}
		p.pushSectionSyntax(&line, 0, 1, len(content)-1, len(content))
		if p.failed != nil {
			return
		}
		p.addSection(lineIndex, 1, len(content)-1, name, false)
		return
	}
	delimiter := strings.IndexByte(content, '=')
	if delimiter < 0 {
		p.recoverLine(lineIndex, "ini.parse.missing-delimiter@1")
		return
	}
	key := content[:delimiter]
	value := content[delimiter+1:]
	if len(key) == 0 {
		p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
		return
	}
	for _, byte := range []byte(key) {
		if !isPortableName(byte) {
			p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
			return
		}
	}
	for _, byte := range []byte(value) {
		if !isPortableValue(byte) {
			p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
			return
		}
	}
	if p.currentSection < 0 {
		p.recoverLine(lineIndex, "ini.parse.missing-section@1")
		return
	}
	p.pushEntrySyntax(&line, 0, delimiter, delimiter, delimiter+1, delimiter+1, len(content), nil)
	if p.failed != nil {
		return
	}
	p.addEntry(lineIndex, p.currentSection, 0, delimiter, delimiter+1, len(content),
		key, value, QuoteStyleNone)
}

// parseWindowsLine parses one Windows profile line (parser.rs:401-467).
func (p *parser) parseWindowsLine(lineIndex int) {
	p.pythonEntry = nil
	line := p.lines[lineIndex]
	content := p.decodedContent(&line)
	trimStart, trimEnd := trimHorizontalBounds(content)
	core := content[trimStart:trimEnd]
	if strings.HasPrefix(core, "[") {
		if !strings.HasSuffix(core, "]") || len(core) < 3 {
			p.recoverLine(lineIndex, "ini.parse.malformed-section@1")
			return
		}
		name := core[1 : len(core)-1]
		for _, byte := range []byte(name) {
			if !isWindowsName(byte) {
				p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
				return
			}
		}
		p.pushOptionalWhitespace(&line, 0, trimStart)
		if p.failed != nil {
			return
		}
		p.pushSectionSyntax(&line, trimStart, trimStart+1, trimEnd-1, trimEnd)
		if p.failed != nil {
			return
		}
		p.pushOptionalWhitespace(&line, trimEnd, len(content))
		if p.failed != nil {
			return
		}
		p.addSection(lineIndex, trimStart+1, trimEnd-1, name, false)
		return
	}
	relative := strings.IndexByte(content[trimStart:], '=')
	if relative < 0 {
		p.recoverLine(lineIndex, "ini.parse.missing-delimiter@1")
		return
	}
	delimiter := relative + trimStart
	keyStart, keyEnd := trimHorizontalBounds(content[trimStart:delimiter])
	keyStart += trimStart
	keyEnd += trimStart
	key := content[keyStart:keyEnd]
	if len(key) == 0 {
		p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
		return
	}
	for _, byte := range []byte(key) {
		if !isWindowsName(byte) {
			p.recoverLine(lineIndex, "ini.parse.invalid-character@1")
			return
		}
	}
	if p.currentSection < 0 {
		p.recoverLine(lineIndex, "ini.parse.missing-section@1")
		return
	}
	literalRangeStart := delimiter + 1
	literal := content[literalRangeStart:]
	valueStart, valueEnd, quoteStyle := quotedWindowsValue(literal, literalRangeStart)
	value := content[valueStart:valueEnd]
	p.pushOptionalWhitespace(&line, 0, keyStart)
	if p.failed != nil {
		return
	}
	p.pushPieceLocal(&line, keyStart, keyEnd, PieceToken, SyntaxKindEntryKey)
	if p.failed != nil {
		return
	}
	p.pushOptionalWhitespace(&line, keyEnd, delimiter)
	if p.failed != nil {
		return
	}
	p.pushPieceLocal(&line, delimiter, delimiter+1, PieceToken, SyntaxKindDelimiter)
	if p.failed != nil {
		return
	}
	p.pushWindowsValueSyntax(&line, literalRangeStart, len(content), valueStart, valueEnd, quoteStyle)
	if p.failed != nil {
		return
	}
	p.addEntry(lineIndex, p.currentSection, keyStart, keyEnd, valueStart, valueEnd,
		key, value, quoteStyle)
}

// parsePythonLine parses one Python ConfigParser line (parser.rs:469-578).
func (p *parser) parsePythonLine(lineIndex int) {
	line := p.lines[lineIndex]
	content := p.decodedContent(&line)
	indent := leadingHorizontal(content)
	if p.pythonEntry != nil && indent > p.pythonEntry.indent {
		p.addPythonContinuation(lineIndex, indent)
		return
	}
	if p.pythonEntry != nil {
		p.pythonEntry.pendingBlankLines = nil
	}
	p.pythonEntry = nil
	trimStart, trimEnd := trimHorizontalBounds(content)
	core := content[trimStart:trimEnd]
	if strings.HasPrefix(core, "[") {
		if !strings.HasSuffix(core, "]") || len(core) < 3 {
			p.recoverLine(lineIndex, "ini.parse.malformed-section@1")
			return
		}
		name := core[1 : len(core)-1]
		p.pushOptionalWhitespace(&line, 0, trimStart)
		if p.failed != nil {
			return
		}
		p.pushSectionSyntax(&line, trimStart, trimStart+1, trimEnd-1, trimEnd)
		if p.failed != nil {
			return
		}
		p.pushOptionalWhitespace(&line, trimEnd, len(content))
		if p.failed != nil {
			return
		}
		p.addSection(lineIndex, trimStart+1, trimEnd-1, name, name == "DEFAULT")
		return
	}
	relative := firstPythonDelimiter(content[trimStart:])
	if relative < 0 {
		code := "ini.parse.missing-delimiter@1"
		if indent > 0 {
			code = "ini.parse.invalid-continuation@1"
		}
		p.recoverLine(lineIndex, code)
		return
	}
	delimiter := relative + trimStart
	relativeKeyStart, relativeKeyEnd := trimHorizontalBounds(content[trimStart:delimiter])
	keyStart := trimStart + relativeKeyStart
	keyEnd := trimStart + relativeKeyEnd
	if keyStart == keyEnd {
		p.recoverLine(lineIndex, "ini.parse.malformed-line@1")
		return
	}
	if p.currentSection < 0 {
		p.recoverLine(lineIndex, "ini.parse.missing-section@1")
		return
	}
	relativeValueStart, relativeValueEnd := trimHorizontalBounds(content[delimiter+1:])
	valueStart := delimiter + 1 + relativeValueStart
	valueEnd := delimiter + 1 + relativeValueEnd
	key := content[keyStart:keyEnd]
	value := content[valueStart:valueEnd]
	p.pushOptionalWhitespace(&line, 0, keyStart)
	if p.failed != nil {
		return
	}
	p.pushPieceLocal(&line, keyStart, keyEnd, PieceToken, SyntaxKindEntryKey)
	if p.failed != nil {
		return
	}
	p.pushOptionalWhitespace(&line, keyEnd, delimiter)
	if p.failed != nil {
		return
	}
	p.pushPieceLocal(&line, delimiter, delimiter+1, PieceToken, SyntaxKindDelimiter)
	if p.failed != nil {
		return
	}
	p.pushOptionalWhitespace(&line, delimiter+1, valueStart)
	if p.failed != nil {
		return
	}
	if valueStart < valueEnd {
		p.pushPieceLocal(&line, valueStart, valueEnd, PieceToken, SyntaxKindEntryValue)
		if p.failed != nil {
			return
		}
	}
	p.pushOptionalWhitespace(&line, valueEnd, len(content))
	if p.failed != nil {
		return
	}
	entryIndex := p.addEntry(lineIndex, p.currentSection, keyStart, keyEnd, valueStart, valueEnd,
		key, value, QuoteStyleNone)
	if p.failed != nil {
		return
	}
	logicalIndex := p.logicalLines[len(p.logicalLines)-1].node
	position := -1
	for index := range p.logicalLines {
		if p.logicalLines[index].node == logicalIndex {
			position = index
			break
		}
	}
	physical := p.physicalLines[line.physicalIndex]
	p.pythonEntry = &pythonEntryState{
		entryIndex: entryIndex, logicalIndex: position, indent: indent,
		continuationLines: 0, logicalBytes: physical.span.Len(),
		logicalScalars: strings.Count(content, "") - 1,
	}
}

// addPythonContinuation joins one more-indented physical line into the
// active Python entry (parser.rs:580-747).
func (p *parser) addPythonContinuation(lineIndex int, indent int) {
	line := p.lines[lineIndex]
	content := p.decodedContent(&line)
	_, relativeValueEnd := trimHorizontalBounds(content[indent:])
	valueStart := indent
	valueEnd := indent + relativeValueEnd
	state := p.pythonEntry
	p.pythonEntry = nil
	addedLines := len(state.pendingBlankLines) + 1
	continuationLines := state.continuationLines + addedLines
	p.checkLimit("continuation-lines", continuationLines, p.limits.MaxContinuationLines)
	if p.failed != nil {
		return
	}
	pendingBytes := 0
	pendingScalars := 0
	for _, pending := range state.pendingBlankLines {
		pendingLine := p.lines[pending]
		physical := p.physicalLines[pendingLine.physicalIndex]
		pendingBytes += physical.span.Len()
		pendingScalars += strings.Count(p.decodedContent(&pendingLine), "") - 1
	}
	physical := p.physicalLines[line.physicalIndex]
	logicalBytes := state.logicalBytes + pendingBytes + physical.span.Len()
	logicalScalars := state.logicalScalars + pendingScalars + (strings.Count(content, "") - 1)
	p.checkLimit("logical-line-bytes", logicalBytes, p.limits.MaxLogicalLineBytes)
	if p.failed != nil {
		return
	}
	p.checkLimit("logical-line-scalars", logicalScalars, p.limits.MaxLogicalLineScalars)
	if p.failed != nil {
		return
	}
	fragment := content[valueStart:valueEnd]
	valueStorageBytes := len(p.entries[state.entryIndex].value) + addedLines + len(fragment)
	p.checkLimit("logical-value-storage-bytes", valueStorageBytes,
		p.limits.MaxDecodedUTF8Bytes)
	if p.failed != nil {
		return
	}
	var joined strings.Builder
	joined.WriteString(p.entries[state.entryIndex].value)
	for _, pending := range state.pendingBlankLines {
		pendingLine := p.lines[pending]
		p.logicalLines[state.logicalIndex].physicalLines =
			append(p.logicalLines[state.logicalIndex].physicalLines,
				p.physicalLines[pendingLine.physicalIndex].node)
		joined.WriteByte('\n')
	}
	p.logicalLines[state.logicalIndex].physicalLines = append(
		p.logicalLines[state.logicalIndex].physicalLines, physical.node)
	joined.WriteByte('\n')
	joined.WriteString(fragment)
	p.entries[state.entryIndex].value = joined.String()
	if len(p.entries[state.entryIndex].value) == 0 {
		p.entries[state.entryIndex].state = ValueStateEmpty
	} else {
		p.entries[state.entryIndex].state = ValueStatePresent
	}
	p.pushPieceLocal(&line, 0, indent, PieceTrivia, SyntaxKindContinuationMarker)
	if p.failed != nil {
		return
	}
	if valueStart < valueEnd {
		p.pushPieceLocal(&line, valueStart, valueEnd, PieceToken, SyntaxKindEntryValue)
		if p.failed != nil {
			return
		}
	}
	p.pushOptionalWhitespace(&line, valueEnd, len(content))
	if p.failed != nil {
		return
	}
	state.continuationLines = continuationLines
	state.logicalBytes = logicalBytes
	state.logicalScalars = logicalScalars
	state.pendingBlankLines = nil
	p.pythonEntry = state
}

// addSection records one section-header occurrence (parser.rs:749-785).
func (p *parser) addSection(lineIndex int, nameStart, nameEnd int, name string, isDefault bool) {
	p.checkLimit("sections", len(p.sections)+1, p.limits.MaxSections)
	if p.failed != nil {
		return
	}
	line := p.lines[lineIndex]
	logicalIndex := p.addLogical(lineIndex, LogicalLineSection)
	if p.failed != nil {
		return
	}
	role := document.RoleIniSection
	if isDefault {
		role = document.RoleIniDefaultSection
	}
	node, ok := p.issueNode(role)
	if !ok {
		return
	}
	nameSpan, ok := p.rawSpan(line.decodedStart+nameStart, line.decodedStart+nameEnd)
	if !ok {
		return
	}
	section := IniSection{
		node:           node,
		logicalLine:    p.logicalLines[logicalIndex].node,
		span:           p.physicalLines[line.physicalIndex].contentSpan,
		nameSpan:       nameSpan,
		name:           name,
		comparisonName: p.sectionComparison(name),
		isDefault:      isDefault,
	}
	p.sections = append(p.sections, section)
	p.currentSection = len(p.sections) - 1
	p.pythonEntry = nil
}

// addEntry records one key/value occurrence (parser.rs:788-834).
func (p *parser) addEntry(lineIndex, sectionIndex, keyStart, keyEnd, valueStart, valueEnd int,
	key, value string, quoteStyle IniQuoteStyle) int {
	p.checkLimit("entries", len(p.entries)+1, p.limits.MaxEntries)
	if p.failed != nil {
		return -1
	}
	line := p.lines[lineIndex]
	logicalIndex := p.addLogical(lineIndex, LogicalLineEntry)
	if p.failed != nil {
		return -1
	}
	node, ok := p.issueNode(document.RoleIniEntry)
	if !ok {
		return -1
	}
	keySpan, ok := p.rawSpan(line.decodedStart+keyStart, line.decodedStart+keyEnd)
	if !ok {
		return -1
	}
	valueSpan, ok := p.rawSpan(line.decodedStart+valueStart, line.decodedStart+valueEnd)
	if !ok {
		return -1
	}
	state := ValueStatePresent
	if len(value) == 0 {
		state = ValueStateEmpty
	}
	entry := IniEntry{
		node:          node,
		logicalLine:   p.logicalLines[logicalIndex].node,
		section:       p.sections[sectionIndex].node,
		span:          p.physicalLines[line.physicalIndex].contentSpan,
		keySpan:       keySpan,
		valueSpan:     valueSpan,
		key:           key,
		comparisonKey: p.keyComparison(key),
		value:         value,
		state:         state,
		quoteStyle:    quoteStyle,
	}
	entryIndex := len(p.entries)
	p.entries = append(p.entries, entry)
	p.entrySectionIndices = append(p.entrySectionIndices, sectionIndex)
	return entryIndex
}

// addLogical records one logical record (parser.rs:836-867).
func (p *parser) addLogical(lineIndex int, kind IniLogicalLineKind) int {
	p.checkLimit("logical-lines", len(p.logicalLines)+1, p.limits.MaxLogicalLines)
	if p.failed != nil {
		return -1
	}
	line := p.lines[lineIndex]
	physical := p.physicalLines[line.physicalIndex]
	p.checkLimit("logical-line-bytes", physical.span.Len(), p.limits.MaxLogicalLineBytes)
	if p.failed != nil {
		return -1
	}
	p.checkLimit("logical-line-scalars",
		strings.Count(p.decodedContent(&line), "")-1, p.limits.MaxLogicalLineScalars)
	if p.failed != nil {
		return -1
	}
	node, ok := p.issueNode(document.RoleIniLogicalLine)
	if !ok {
		return -1
	}
	index := len(p.logicalLines)
	p.logicalLines = append(p.logicalLines, IniLogicalLine{
		node: node, kind: kind, physicalLines: []document.NodeRef{physical.node},
	})
	return index
}

// recoverLine retains one physical line as an error record with a stable
// diagnostic (parser.rs:869-905).
func (p *parser) recoverLine(lineIndex int, code string) {
	p.checkLimit("recovery-regions", len(p.errorLines)+1, p.limits.MaxRecoveryRegions)
	if p.failed != nil {
		return
	}
	p.pythonEntry = nil
	line := p.lines[lineIndex]
	if line.decodedStart < line.decodedContentEnd {
		p.pushPiece(line.decodedStart, line.decodedContentEnd, PieceErrorRegion, SyntaxKindErrorRegion)
		if p.failed != nil {
			return
		}
	}
	logicalIndex := p.addLogical(lineIndex, LogicalLineError)
	if p.failed != nil {
		return
	}
	node, ok := p.issueNode(document.RoleIniErrorLine)
	if !ok {
		return
	}
	physical := p.physicalLines[line.physicalIndex]
	p.errorLines = append(p.errorLines, IniErrorLine{
		node:         node,
		logicalLine:  p.logicalLines[logicalIndex].node,
		physicalLine: physical.node,
		span:         physical.contentSpan,
		code:         code,
	})
	p.diagnostic(code, recoveryCategory(code), physical.contentSpan.StartByte(),
		physical.contentSpan.EndByte(), true)
}

// recoveryCategory maps one recovery code to its registered category. The
// frozen registry pins ini.parse.missing-section@1 to Conformance (it is
// also used by the end-of-document check), while every other recovery code
// is a Syntax failure.
func recoveryCategory(code string) protocol.DiagnosticCategory {
	if code == "ini.parse.missing-section@1" {
		return protocol.CategoryConformance
	}
	return protocol.CategorySyntax
}

// pushBom emits the BOM trivia piece when the source carries one
// (parser.rs:907-919).
func (p *parser) pushBom() {
	if p.source.EncodingFacts().Bom() != nil {
		text, ok := p.source.DecodedText()
		if ok && strings.HasPrefix(text, "\uFEFF") {
			p.pushPiece(0, len("\uFEFF"), PieceTrivia, SyntaxKindBom)
		}
	}
}

// pushComment emits the comment pieces of one comment line (parser.rs:
// 921-943).
func (p *parser) pushComment(line *scannedLine, leading int) {
	p.pushOptionalWhitespace(line, 0, leading)
	if p.failed != nil {
		return
	}
	p.pushPieceLocal(line, leading, leading+1, PieceTrivia, SyntaxKindCommentMarker)
	if p.failed != nil {
		return
	}
	length := line.decodedContentEnd - line.decodedStart
	if leading+1 < length {
		p.pushPieceLocal(line, leading+1, length, PieceTrivia, SyntaxKindCommentText)
	}
}

// pushSectionSyntax emits the three section pieces (parser.rs:945-971).
func (p *parser) pushSectionSyntax(line *scannedLine, open, nameStart, nameEnd, closeEnd int) {
	p.pushPieceLocal(line, open, nameStart, PieceToken, SyntaxKindSectionOpen)
	if p.failed != nil {
		return
	}
	p.pushPieceLocal(line, nameStart, nameEnd, PieceToken, SyntaxKindSectionName)
	if p.failed != nil {
		return
	}
	p.pushPieceLocal(line, nameEnd, closeEnd, PieceToken, SyntaxKindSectionClose)
}

// pushEntrySyntax emits the key, delimiter, value, and optional quote
// pieces (parser.rs:973-1018).
func (p *parser) pushEntrySyntax(line *scannedLine, keyStart, keyEnd, delimiterStart,
	delimiterEnd, valueStart, valueEnd int, quote *[2]int) {
	p.pushPieceLocal(line, keyStart, keyEnd, PieceToken, SyntaxKindEntryKey)
	if p.failed != nil {
		return
	}
	p.pushPieceLocal(line, delimiterStart, delimiterEnd, PieceToken, SyntaxKindDelimiter)
	if p.failed != nil {
		return
	}
	if quote != nil {
		p.pushPieceLocal(line, quote[0], quote[0]+1, PieceToken, SyntaxKindQuote)
		if p.failed != nil {
			return
		}
		if valueStart < valueEnd {
			p.pushPieceLocal(line, valueStart, valueEnd, PieceToken, SyntaxKindEntryValue)
			if p.failed != nil {
				return
			}
		}
		p.pushPieceLocal(line, quote[1], quote[1]+1, PieceToken, SyntaxKindQuote)
		return
	}
	if valueStart < valueEnd {
		p.pushPieceLocal(line, valueStart, valueEnd, PieceToken, SyntaxKindEntryValue)
	}
}

// pushWindowsValueSyntax emits the value pieces of a Windows entry
// (parser.rs:1020-1037).
func (p *parser) pushWindowsValueSyntax(line *scannedLine, literalStart, literalEnd,
	valueStart, valueEnd int, quoteStyle IniQuoteStyle) {
	if quoteStyle == QuoteStyleNone {
		p.pushEntrySyntax(line, 0, 0, 0, 0, literalStart, literalEnd, nil)
		return
	}
	p.pushEntrySyntax(line, 0, 0, 0, 0, valueStart, valueEnd,
		&[2]int{literalStart, valueEnd})
}

// pushLineBreak emits the line-break trivia piece (parser.rs:1039-1049).
func (p *parser) pushLineBreak(lineIndex int) {
	line := p.lines[lineIndex]
	if line.decodedBreakStart < line.decodedEnd {
		p.pushPiece(line.decodedBreakStart, line.decodedEnd, PieceTrivia, SyntaxKindLineBreak)
	}
}

// pushOptionalWhitespace emits one whitespace piece when non-empty
// (parser.rs:1051-1065).
func (p *parser) pushOptionalWhitespace(line *scannedLine, start, end int) {
	if start < end {
		p.pushPieceLocal(line, start, end, PieceTrivia, SyntaxKindWhitespace)
	}
}

// pushPieceLocal emits one piece with decoded offsets relative to the line
// start (parser.rs:1067-1082).
func (p *parser) pushPieceLocal(line *scannedLine, start, end int, kind StructuralPieceKind,
	syntax IniSyntaxKind) {
	if start == end {
		return
	}
	p.pushPiece(line.decodedStart+start, line.decodedStart+end, kind, syntax)
}

// pushPiece emits one exhaustive source piece (parser.rs:1084-1107).
func (p *parser) pushPiece(decodedStart, decodedEnd int, kind StructuralPieceKind,
	syntax IniSyntaxKind) {
	observed := len(p.pieces) + 1
	p.checkLimit("syntax-pieces", observed, p.limits.Common.MaxTokenCount)
	if p.failed != nil {
		return
	}
	span, ok := p.rawSpan(decodedStart, decodedEnd)
	if !ok {
		return
	}
	if span.IsEmpty() {
		p.failed = resourceLimitFailure("source-coordinate-coverage", 1, 0)
		return
	}
	p.pieces = append(p.pieces, StructuralPiece{span: span, kind: kind})
	p.kinds = append(p.kinds, syntax)
}

// rawSpan converts one decoded UTF-8 byte range into an exact raw span
// (parser.rs:1109-1125).
func (p *parser) rawSpan(decodedStart, decodedEnd int) (document.Span, bool) {
	rawStart, err := p.source.RawByteAt(document.NewUtf8ByteOffset(decodedStart))
	if err != nil {
		p.failed = resourceLimitFailure("source-coordinate-boundary", 1, 0)
		return document.Span{}, false
	}
	rawEnd, err := p.source.RawByteAt(document.NewUtf8ByteOffset(decodedEnd))
	if err != nil {
		p.failed = resourceLimitFailure("source-coordinate-boundary", 1, 0)
		return document.Span{}, false
	}
	span, err := p.authority.Span(rawStart, rawEnd)
	if err != nil {
		p.failed = resourceLimitFailure("source-coordinate-boundary", 1, 0)
		return document.Span{}, false
	}
	return span, true
}

// decodedContent returns the decoded line content.
func (p *parser) decodedContent(line *scannedLine) string {
	text, _ := p.source.DecodedText()
	return text[line.decodedStart:line.decodedContentEnd]
}

// issueNode issues one node identity with a fresh ordinal
// (parser.rs:1132-1142).
func (p *parser) issueNode(role document.NodeRole) (document.NodeRef, bool) {
	observed := int(p.nextNode) + 1
	p.checkLimit("nodes", observed, p.limits.Common.MaxNodeCount)
	if p.failed != nil {
		return document.NodeRef{}, false
	}
	node := p.authority.NodeRef(p.nextNode, role)
	p.nextNode++
	return node, true
}

// checkLimit records a fatal resource-limit failure (parser.rs:1144-1156).
func (p *parser) checkLimit(name string, observed, limit int) {
	if p.failed != nil {
		return
	}
	if observed > limit {
		p.failed = resourceLimitFailure(name, observed, limit)
	}
}

// diagnostic records one ordered non-fatal or recovery diagnostic
// (parser.rs:1158-1195).
func (p *parser) diagnostic(code string, category protocol.DiagnosticCategory,
	start, end int, recovered bool) {
	p.checkLimit("diagnostics", len(p.diagnostics)+1, p.limits.Common.MaxDiagnostics)
	if p.failed != nil {
		return
	}
	severity := protocol.SeverityWarning
	if recovered {
		severity = protocol.SeverityError
	}
	location := &protocol.SourceLocation{StartByte: uint64(start), EndByte: uint64(end)}
	diagnostic, err := protocol.NewDiagnostic(code, category, severity, location, nil, nil,
		nil, nil, p.occurrence, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		panic("ini: unregistered diagnostic code " + code)
	}
	p.diagnostics = append(p.diagnostics, diagnostic)
	p.occurrence++
	if recovered {
		p.recovered = true
	}
}

// sectionComparison applies the profile comparison rule to one section
// name (parser.rs:1197-1202).
func (p *parser) sectionComparison(name string) string {
	if p.profile.isWindows() {
		return strings.ToLower(name)
	}
	return name
}

// keyComparison applies the profile comparison rule to one entry key
// (parser.rs:1204-1210).
func (p *parser) keyComparison(key string) string {
	switch {
	case p.profile.isWindows():
		return strings.ToLower(key)
	case p.profile.isPython():
		return optionxform(key)
	}
	return key
}

// assignDuplicateGroups marks duplicate and case-equivalence groups
// deterministically (parser.rs:1212-1304).
func (p *parser) assignDuplicateGroups() {
	sectionGroups := map[string][]int{}
	sectionOrder := []string{}
	for index, section := range p.sections {
		key := section.comparisonName
		if _, seen := sectionGroups[key]; !seen {
			sectionOrder = append(sectionOrder, key)
		}
		sectionGroups[key] = append(sectionGroups[key], index)
	}
	nextGroup := uint32(1)
	for _, key := range sectionOrder {
		indices := sectionGroups[key]
		if len(indices) <= 1 {
			continue
		}
		p.checkLimit("duplicate-group-members", len(indices), p.limits.MaxDuplicateGroupMembers)
		if p.failed != nil {
			return
		}
		group := nextGroup
		nextGroup++
		firstIndex := indices[0]
		firstSection := p.sections[firstIndex]
		for _, index := range indices {
			p.sections[index].duplicateGroup = &group
		}
		for _, index := range indices[1:] {
			span := p.sections[index].span
			code := "ini.formation.duplicate-section@1"
			if p.sections[index].name != firstSection.name {
				code = "ini.formation.case-collision@1"
			}
			p.diagnostic(code, protocol.CategorySemantic, span.StartByte(), span.EndByte(),
				!p.profile.isWindows())
			if p.failed != nil {
				return
			}
		}
	}
	type entryGroupKey struct {
		section string
		key     string
	}
	entryGroups := map[entryGroupKey][]int{}
	entryOrder := []entryGroupKey{}
	for index, entry := range p.entries {
		sectionIdentity := p.sections[p.entrySectionIndices[index]].comparisonName
		if !p.profile.isWindows() {
			sectionIdentity = itoa(p.entrySectionIndices[index])
		}
		key := entryGroupKey{section: sectionIdentity, key: entry.comparisonKey}
		if _, seen := entryGroups[key]; !seen {
			entryOrder = append(entryOrder, key)
		}
		entryGroups[key] = append(entryGroups[key], index)
	}
	for _, key := range entryOrder {
		indices := entryGroups[key]
		if len(indices) <= 1 {
			continue
		}
		p.checkLimit("duplicate-group-members", len(indices), p.limits.MaxDuplicateGroupMembers)
		if p.failed != nil {
			return
		}
		group := nextGroup
		nextGroup++
		firstIndex := indices[0]
		firstEntry := p.entries[firstIndex]
		for _, index := range indices {
			p.entries[index].duplicateGroup = &group
		}
		for _, index := range indices[1:] {
			span := p.entries[index].span
			code := "ini.formation.duplicate-entry@1"
			if p.entries[index].key != firstEntry.key {
				code = "ini.formation.case-collision@1"
			}
			p.diagnostic(code, protocol.CategorySemantic, span.StartByte(), span.EndByte(),
				!p.profile.isWindows())
			if p.failed != nil {
				return
			}
		}
	}
}

// sortDiagnostics applies the deterministic diagnostic ordering
// (consema-core diagnostic.rs:107-123): primary start, category, code,
// occurrence.
func sortDiagnostics(diagnostics []*protocol.Diagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left := diagnostics[i]
		right := diagnostics[j]
		leftStart := uint64(1<<64 - 1)
		rightStart := uint64(1<<64 - 1)
		if left.Primary != nil {
			leftStart = left.Primary.StartByte
		}
		if right.Primary != nil {
			rightStart = right.Primary.StartByte
		}
		if leftStart != rightStart {
			return leftStart < rightStart
		}
		leftCategory := categoryOrder(left.Category)
		rightCategory := categoryOrder(right.Category)
		if leftCategory != rightCategory {
			return leftCategory < rightCategory
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Occurrence < right.Occurrence
	})
}

// categoryOrder is the frozen diagnostic category discriminant order
// (consema-core diagnostic.rs:7-30).
func categoryOrder(category protocol.DiagnosticCategory) int {
	switch category {
	case protocol.CategoryLexical:
		return 0
	case protocol.CategorySyntax:
		return 1
	case protocol.CategoryConformance:
		return 2
	case protocol.CategorySemantic:
		return 3
	case protocol.CategoryQuery:
		return 4
	case protocol.CategoryProjection:
		return 5
	case protocol.CategoryMaterialization:
		return 6
	case protocol.CategoryConversion:
		return 7
	case protocol.CategoryEdit:
		return 8
	case protocol.CategoryResource:
		return 9
	case protocol.CategoryEncoding:
		return 10
	}
	return 11
}

// itoa renders one non-negative integer without importing fmt.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 12)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}

// leadingHorizontal returns the count of leading horizontal characters.
func leadingHorizontal(value string) int {
	count := 0
	for count < len(value) && isHorizontalByte(value[count]) {
		count++
	}
	return count
}

// trimHorizontalBounds returns the content bounds after trimming leading
// and trailing horizontal whitespace.
func trimHorizontalBounds(value string) (int, int) {
	start := leadingHorizontal(value)
	end := len(value)
	for end > start && isHorizontalByte(value[end-1]) {
		end--
	}
	if end < start {
		end = start
	}
	return start, end
}

// allHorizontal reports whether every byte is horizontal whitespace.
func allHorizontal(value string) bool {
	for index := 0; index < len(value); index++ {
		if !isHorizontalByte(value[index]) {
			return false
		}
	}
	return true
}

func isHorizontalByte(byte byte) bool {
	return byte == ' ' || byte == '\t'
}

// isPortableName is the portable profile identifier character set.
func isPortableName(byte byte) bool {
	return byte >= 'a' && byte <= 'z' || byte >= 'A' && byte <= 'Z' ||
		byte >= '0' && byte <= '9' || byte == '_' || byte == '-' || byte == '.'
}

// isPortableValue is the portable profile value character set.
func isPortableValue(byte byte) bool {
	if byte >= 0x21 && byte <= 0x7E {
		switch byte {
		case '\'', '"', '\\', ':', '#', ';':
			return false
		}
		return true
	}
	return byte == ' '
}

// isWindowsName is the Windows profile identifier character set.
func isWindowsName(byte byte) bool {
	if byte == ' ' {
		return true
	}
	if byte < 0x21 || byte > 0x7E {
		return false
	}
	switch byte {
	case '[', ']', '=':
		return false
	}
	return true
}

// quotedWindowsValue returns the semantic value range and quote style of
// one Windows literal (parser.rs:1341-1358).
func quotedWindowsValue(value string, absoluteStart int) (int, int, IniQuoteStyle) {
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		style := QuoteStyleNone
		switch {
		case first == '\'' && last == '\'':
			style = QuoteStyleSingle
		case first == '"' && last == '"':
			style = QuoteStyleDouble
		}
		if style != QuoteStyleNone {
			return absoluteStart + 1, absoluteStart + len(value) - 1, style
		}
	}
	return absoluteStart, absoluteStart + len(value), QuoteStyleNone
}

// firstPythonDelimiter returns the first `=` or `:` offset.
func firstPythonDelimiter(value string) int {
	for index := 0; index < len(value); index++ {
		if value[index] == '=' || value[index] == ':' {
			return index
		}
	}
	return -1
}
