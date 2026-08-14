package properties

import (
	"sort"
	"unicode/utf8"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the lossless Java Properties parser (consema-properties
// parser.rs; RFC 0010 §5-§9): natural and logical lines, continuation,
// key/separator/element grammar, exact escape decoding into UTF-16 code
// units, recovery with stable diagnostics, duplicate groups, and exhaustive
// syntax coverage. The parser is an independent standard-library
// implementation of the language-neutral contract; it shares no code with
// the Rust crate.

// atom is one decoded Unicode scalar with its exact raw source range and
// its eventual lossless syntax classification.
type atom struct {
	ch       rune
	rawStart int
	rawEnd   int
	syntax   *PropertiesSyntaxKind
}

// scannedLine is one natural source line expressed in atom coordinates.
type scannedLine struct {
	atomStart      int
	atomContentEnd int
	atomEnd        int
	naturalIndex   int
}

// escapeSpec is one decoded escape occurrence within a Java string.
type escapeSpec struct {
	atomIndices []int
	kind        PropertiesEscapeKind
	outputStart int
	outputEnd   int
}

// decodedJavaString is the exact code-unit output of one key or value.
type decodedJavaString struct {
	units          []uint16
	escapes        []escapeSpec
	unicodeEscapes int
}

// decodeError locates one malformed Unicode escape in atom coordinates.
type decodeError struct {
	atomStart int
	atomEnd   int
}

// parser assembles one immutable Document from one source snapshot.
type parser struct {
	source              *document.SourceSnapshot
	profile             PropertiesProfile
	limits              PropertiesParseLimits
	authority           document.DocumentAuthority
	nextNode            uint64
	atoms               []atom
	lines               []scannedLine
	naturalLines        []PropertiesNaturalLine
	logicalLines        []PropertiesLogicalLine
	properties          []Property
	comments            []PropertiesComment
	escapes             []PropertiesEscape
	errorLines          []PropertiesErrorLine
	diagnostics         []*protocol.Diagnostic
	occurrence          uint64
	recovered           bool
	totalJavaUnits      int
	totalUnicodeEscapes int
}

func newParser(source *document.SourceSnapshot, profile PropertiesProfile,
	limits PropertiesParseLimits) (*parser, *FormationFailure) {
	authority := document.NewDocumentAuthority()
	atoms, failure := buildAtoms(source, authority, limits)
	if failure != nil {
		return nil, failure
	}
	p := &parser{
		source:    source,
		profile:   profile,
		limits:    limits,
		authority: authority,
		nextNode:  1,
		atoms:     atoms,
	}
	if failure := p.scanNaturalLines(); failure != nil {
		return nil, failure
	}
	return p, nil
}

func (p *parser) parse() (*Document, *FormationFailure) {
	lineIndex := 0
	for lineIndex < len(p.lines) {
		if p.isBlank(lineIndex) {
			p.markLineContent(lineIndex, SyntaxKindWhitespace)
			lineIndex++
		} else if p.isComment(lineIndex) {
			if failure := p.addComment(lineIndex); failure != nil {
				return nil, failure
			}
			lineIndex++
		} else {
			next, failure := p.addLogicalLine(lineIndex)
			if failure != nil {
				return nil, failure
			}
			lineIndex = next
		}
	}
	if failure := p.assignDuplicateGroups(); failure != nil {
		return nil, failure
	}
	pieces, syntaxKinds, failure := p.buildStructuralPieces()
	if failure != nil {
		return nil, failure
	}
	index, err := NewLosslessStructuralIndex(p.authority.Identity(), p.source.Len(), pieces)
	if err != nil {
		return nil, resourceLimitFailure("source-coordinate-coverage", 1, 0)
	}
	sortDiagnostics(p.diagnostics)
	formationStatus := document.FormationStatusComplete
	if p.recovered {
		formationStatus = document.FormationStatusRecovered
	}
	return &Document{
		authority:       p.authority,
		source:          p.source,
		profile:         p.profile,
		index:           index,
		syntaxKinds:     syntaxKinds,
		formationStatus: formationStatus,
		diagnostics:     p.diagnostics,
		naturalLines:    p.naturalLines,
		logicalLines:    p.logicalLines,
		properties:      p.properties,
		comments:        p.comments,
		escapes:         p.escapes,
		errorLines:      p.errorLines,
		parseLimits:     p.limits,
	}, nil
}

func (p *parser) scanNaturalLines() *FormationFailure {
	start := 0
	if p.source.EncodingFacts().Bom() != nil && len(p.atoms) > 0 && p.atoms[0].ch == '\uFEFF' {
		syntax := SyntaxKindBom
		p.atoms[0].syntax = &syntax
		start = 1
	}
	cursor := start
	for cursor < len(p.atoms) {
		lineStart := cursor
		for cursor < len(p.atoms) && p.atoms[cursor].ch != '\r' && p.atoms[cursor].ch != '\n' {
			cursor++
		}
		contentEnd := cursor
		if cursor < len(p.atoms) {
			if p.atoms[cursor].ch == '\r' && cursor+1 < len(p.atoms) && p.atoms[cursor+1].ch == '\n' {
				cursor += 2
			} else {
				cursor++
			}
		}
		end := cursor
		if failure := p.checkLimit("natural-lines", len(p.lines)+1, p.limits.MaxNaturalLines); failure != nil {
			return failure
		}
		scalarCount := contentEnd - lineStart
		if failure := p.checkLimit("natural-line-scalars", scalarCount,
			p.limits.MaxNaturalLineScalars); failure != nil {
			return failure
		}
		span, failure := p.atomSpan(lineStart, end)
		if failure != nil {
			return failure
		}
		if failure := p.checkLimit("natural-line-bytes", span.Len(),
			p.limits.MaxNaturalLineBytes); failure != nil {
			return failure
		}
		contentSpan, failure := p.atomSpan(lineStart, contentEnd)
		if failure != nil {
			return failure
		}
		var lineBreakSpan *document.Span
		if contentEnd < end {
			p.markAtoms(contentEnd, end, SyntaxKindLineBreak)
			lineBreak, failure := p.atomSpan(contentEnd, end)
			if failure != nil {
				return failure
			}
			lineBreakSpan = &lineBreak
		}
		node, failure := p.issueNode(document.RolePropertiesNaturalLine)
		if failure != nil {
			return failure
		}
		naturalIndex := len(p.naturalLines)
		p.naturalLines = append(p.naturalLines, PropertiesNaturalLine{
			node:          node,
			span:          span,
			contentSpan:   contentSpan,
			lineBreakSpan: lineBreakSpan,
		})
		p.lines = append(p.lines, scannedLine{
			atomStart:      lineStart,
			atomContentEnd: contentEnd,
			atomEnd:        end,
			naturalIndex:   naturalIndex,
		})
	}
	return nil
}

func (p *parser) isBlank(lineIndex int) bool {
	line := &p.lines[lineIndex]
	for index := line.atomStart; index < line.atomContentEnd; index++ {
		if !isPropertiesWhitespace(p.atoms[index].ch) {
			return false
		}
	}
	return true
}

func (p *parser) isComment(lineIndex int) bool {
	line := &p.lines[lineIndex]
	for index := line.atomStart; index < line.atomContentEnd; index++ {
		ch := p.atoms[index].ch
		if !isPropertiesWhitespace(ch) {
			return ch == '#' || ch == '!'
		}
	}
	return false
}

func (p *parser) markLineContent(lineIndex int, syntax PropertiesSyntaxKind) {
	line := p.lines[lineIndex]
	p.markAtoms(line.atomStart, line.atomContentEnd, syntax)
}

func (p *parser) addComment(lineIndex int) *FormationFailure {
	if failure := p.checkLimit("comments", len(p.comments)+1, p.limits.MaxComments); failure != nil {
		return failure
	}
	line := p.lines[lineIndex]
	markerIndex := -1
	for index := line.atomStart; index < line.atomContentEnd; index++ {
		if !isPropertiesWhitespace(p.atoms[index].ch) {
			markerIndex = index
			break
		}
	}
	if markerIndex < 0 {
		// A comment line always has a marker.
		return resourceLimitFailure("comments", 1, 0)
	}
	p.markAtoms(line.atomStart, markerIndex, SyntaxKindWhitespace)
	p.markAtoms(markerIndex, markerIndex+1, SyntaxKindCommentMarker)
	p.markAtoms(markerIndex+1, line.atomContentEnd, SyntaxKindCommentText)
	node, failure := p.issueNode(document.RolePropertiesComment)
	if failure != nil {
		return failure
	}
	span, failure := p.atomSpan(line.atomStart, line.atomContentEnd)
	if failure != nil {
		return failure
	}
	p.comments = append(p.comments, PropertiesComment{
		node:        node,
		naturalLine: p.naturalLines[line.naturalIndex].node,
		span:        span,
		marker:      p.atoms[markerIndex].ch,
	})
	return nil
}

// addLogicalLine assembles one property or recovered-error logical line
// from its constituent natural lines and returns the next line index.
func (p *parser) addLogicalLine(firstLine int) (int, *FormationFailure) {
	if failure := p.checkLimit("logical-lines", len(p.logicalLines)+1,
		p.limits.MaxLogicalLines); failure != nil {
		return 0, failure
	}
	lineIndex := firstLine
	var naturalIndices []int
	var logicalAtoms []int
	for {
		line := p.lines[lineIndex]
		naturalIndices = append(naturalIndices, line.naturalIndex)
		if failure := p.checkLimit("logical-line-natural-lines", len(naturalIndices),
			p.limits.MaxLogicalLineNaturalLines); failure != nil {
			return 0, failure
		}
		leading := 0
		if lineIndex != firstLine {
			for index := line.atomStart; index < line.atomContentEnd; index++ {
				if !isPropertiesWhitespace(p.atoms[index].ch) {
					break
				}
				leading++
			}
		}
		if leading > 0 {
			p.markAtoms(line.atomStart, line.atomStart+leading, SyntaxKindWhitespace)
		}
		slashRun := 0
		for index := line.atomContentEnd - 1; index >= line.atomStart+leading; index-- {
			if p.atoms[index].ch != '\\' {
				break
			}
			slashRun++
		}
		hasBreak := line.atomContentEnd < line.atomEnd
		removeTerminalSlash := slashRun%2 == 1
		logicalEnd := line.atomContentEnd
		if removeTerminalSlash {
			logicalEnd = line.atomContentEnd - 1
		}
		for index := line.atomStart + leading; index < logicalEnd; index++ {
			logicalAtoms = append(logicalAtoms, index)
		}
		if failure := p.checkLimit("logical-line-scalars", len(logicalAtoms),
			p.limits.MaxLogicalLineScalars); failure != nil {
			return 0, failure
		}
		if removeTerminalSlash {
			p.markAtoms(logicalEnd, line.atomContentEnd, SyntaxKindContinuationMarker)
		}
		if removeTerminalSlash && hasBreak && lineIndex+1 < len(p.lines) {
			lineIndex++
			continue
		}
		break
	}
	nextLine := lineIndex + 1

	naturalNodes := make([]document.NodeRef, 0, len(naturalIndices))
	for _, index := range naturalIndices {
		naturalNodes = append(naturalNodes, p.naturalLines[index].node)
	}
	logicalNode, failure := p.issueNode(document.RolePropertiesLogicalLine)
	if failure != nil {
		return 0, failure
	}
	leading := 0
	for _, position := range logicalAtoms {
		if !isPropertiesWhitespace(p.atoms[position].ch) {
			break
		}
		leading++
	}
	p.markLogicalPositions(logicalAtoms, 0, leading, SyntaxKindWhitespace)
	keyStart, keyEnd, valueStart, hadSeparator := p.splitProperty(logicalAtoms, leading)
	p.markLogicalPositions(logicalAtoms, keyStart, keyEnd, SyntaxKindKey)
	p.markLogicalPositions(logicalAtoms, keyEnd, valueStart, SyntaxKindSeparator)
	p.markLogicalPositions(logicalAtoms, valueStart, len(logicalAtoms), SyntaxKindValue)

	key, keyError := decodeJavaString(p.atoms, logicalAtoms[keyStart:keyEnd])
	value, valueError := decodeJavaString(p.atoms, logicalAtoms[valueStart:])
	switch {
	case keyError != nil:
		if failure := p.recoverLogicalLine(logicalNode, naturalNodes, logicalAtoms,
			firstLine, lineIndex, keyError); failure != nil {
			return 0, failure
		}
		return nextLine, nil
	case valueError != nil:
		if failure := p.recoverLogicalLine(logicalNode, naturalNodes, logicalAtoms,
			firstLine, lineIndex, valueError); failure != nil {
			return 0, failure
		}
		return nextLine, nil
	}
	return nextLine, p.finishProperty(logicalNode, naturalNodes, logicalAtoms,
		keyStart, keyEnd, valueStart, hadSeparator, key, value, firstLine, lineIndex)
}

// splitProperty locates the raw key end, the value start, and whether any
// separator followed the key (parser.rs; RFC 0010 §6).
func (p *parser) splitProperty(logicalAtoms []int, keyStart int) (int, int, int, bool) {
	cursor := keyStart
	escaped := false
	for cursor < len(logicalAtoms) {
		ch := p.atoms[logicalAtoms[cursor]].ch
		if !escaped && (ch == '=' || ch == ':' || isPropertiesWhitespace(ch)) {
			break
		}
		if ch == '\\' {
			escaped = !escaped
		} else {
			escaped = false
		}
		cursor++
	}
	keyEnd := cursor
	hadSeparator := cursor < len(logicalAtoms)
	for cursor < len(logicalAtoms) && isPropertiesWhitespace(p.atoms[logicalAtoms[cursor]].ch) {
		cursor++
	}
	if cursor < len(logicalAtoms) {
		ch := p.atoms[logicalAtoms[cursor]].ch
		if ch == '=' || ch == ':' {
			cursor++
		}
	}
	for cursor < len(logicalAtoms) && isPropertiesWhitespace(p.atoms[logicalAtoms[cursor]].ch) {
		cursor++
	}
	return keyStart, keyEnd, cursor, hadSeparator
}

func (p *parser) finishProperty(logicalNode document.NodeRef, naturalNodes []document.NodeRef,
	logicalAtoms []int, keyStart, keyEnd, valueStart int, hadSeparator bool,
	key, value decodedJavaString, firstLine, lastLine int) *FormationFailure {
	if failure := p.checkLimit("properties", len(p.properties)+1, p.limits.MaxProperties); failure != nil {
		return failure
	}
	if failure := p.checkLimit("java-code-units-per-string", len(key.units),
		p.limits.MaxJavaCodeUnitsPerString); failure != nil {
		return failure
	}
	if failure := p.checkLimit("java-code-units-per-string", len(value.units),
		p.limits.MaxJavaCodeUnitsPerString); failure != nil {
		return failure
	}
	addedUnits := len(key.units) + len(value.units)
	if failure := p.checkLimit("total-java-code-units", p.totalJavaUnits+addedUnits,
		p.limits.MaxTotalJavaCodeUnits); failure != nil {
		return failure
	}
	addedEscapes := len(key.escapes) + len(value.escapes)
	addedUnicodeEscapes := key.unicodeEscapes + value.unicodeEscapes
	if failure := p.checkLimit("escapes", len(p.escapes)+addedEscapes,
		p.limits.MaxEscapes); failure != nil {
		return failure
	}
	if failure := p.checkLimit("unicode-escapes", p.totalUnicodeEscapes+addedUnicodeEscapes,
		p.limits.MaxUnicodeEscapes); failure != nil {
		return failure
	}

	propertyNode, failure := p.issueNode(document.RolePropertiesProperty)
	if failure != nil {
		return failure
	}
	escapeNodes := make([]document.NodeRef, 0, addedEscapes)
	for _, spec := range key.escapes {
		node, failure := p.registerEscape(propertyNode, true, spec)
		if failure != nil {
			return failure
		}
		escapeNodes = append(escapeNodes, node)
	}
	for _, spec := range value.escapes {
		node, failure := p.registerEscape(propertyNode, false, spec)
		if failure != nil {
			return failure
		}
		escapeNodes = append(escapeNodes, node)
	}
	valueState := ValueStatePresent
	if len(value.units) == 0 {
		if hadSeparator {
			valueState = ValueStateExplicitEmpty
		} else {
			valueState = ValueStateImplicitEmpty
		}
	}
	span, failure := p.logicalSourceSpan(firstLine, lastLine)
	if failure != nil {
		return failure
	}
	keyAnchor, failure := p.logicalAnchorSpan(logicalAtoms, keyStart, span.StartByte())
	if failure != nil {
		return failure
	}
	valueAnchor, failure := p.logicalAnchorSpan(logicalAtoms, valueStart, span.EndByte())
	if failure != nil {
		return failure
	}
	keyFragments, failure := p.fragmentSpans(logicalAtoms, keyStart, keyEnd)
	if failure != nil {
		return failure
	}
	valueFragments, failure := p.fragmentSpans(logicalAtoms, valueStart, len(logicalAtoms))
	if failure != nil {
		return failure
	}
	p.logicalLines = append(p.logicalLines, PropertiesLogicalLine{
		node:         logicalNode,
		kind:         LogicalLineProperty,
		naturalLines: append([]document.NodeRef(nil), naturalNodes...),
	})
	p.properties = append(p.properties, Property{
		node:           propertyNode,
		logicalLine:    logicalNode,
		span:           span,
		keyAnchor:      keyAnchor,
		valueAnchor:    valueAnchor,
		keyFragments:   keyFragments,
		valueFragments: valueFragments,
		key:            NewJavaStringFromCodeUnits(key.units),
		value:          NewJavaStringFromCodeUnits(value.units),
		valueState:     valueState,
		escapes:        escapeNodes,
	})
	p.totalJavaUnits += addedUnits
	return nil
}

func (p *parser) registerEscape(propertyNode document.NodeRef, inKey bool,
	spec escapeSpec) (document.NodeRef, *FormationFailure) {
	node, failure := p.issueNode(document.RolePropertiesEscape)
	if failure != nil {
		return document.NodeRef{}, failure
	}
	syntaxMarker := SyntaxKindEscapeMarker
	syntaxBody := SyntaxKindEscapeBody
	p.atoms[spec.atomIndices[0]].syntax = &syntaxMarker
	for _, atomIndex := range spec.atomIndices[1:] {
		p.atoms[atomIndex].syntax = &syntaxBody
	}
	escapeStart := spec.atomIndices[0]
	escapeEnd := spec.atomIndices[len(spec.atomIndices)-1] + 1
	span, failure := p.atomSpan(escapeStart, escapeEnd)
	if failure != nil {
		return document.NodeRef{}, failure
	}
	p.escapes = append(p.escapes, PropertiesEscape{
		node:        node,
		property:    propertyNode,
		inKey:       inKey,
		kind:        spec.kind,
		span:        span,
		outputStart: spec.outputStart,
		outputEnd:   spec.outputEnd,
	})
	return node, nil
}

func (p *parser) recoverLogicalLine(logicalNode document.NodeRef, naturalNodes []document.NodeRef,
	logicalAtoms []int, firstLine, lastLine int, decodeFailure *decodeError) *FormationFailure {
	if failure := p.checkLimit("recovery-regions", len(p.errorLines)+1,
		p.limits.MaxRecoveryRegions); failure != nil {
		return failure
	}
	syntax := SyntaxKindErrorRegion
	for _, atomIndex := range logicalAtoms {
		p.atoms[atomIndex].syntax = &syntax
	}
	span, failure := p.logicalSourceSpan(firstLine, lastLine)
	if failure != nil {
		return failure
	}
	errorSpan, failure := p.atomSpan(decodeFailure.atomStart, decodeFailure.atomEnd)
	if failure != nil {
		return failure
	}
	code := "java-properties.parse.malformed-unicode-escape@1"
	errorNode, failure := p.issueNode(document.RolePropertiesErrorLine)
	if failure != nil {
		return failure
	}
	p.logicalLines = append(p.logicalLines, PropertiesLogicalLine{
		node:         logicalNode,
		kind:         LogicalLineError,
		naturalLines: append([]document.NodeRef(nil), naturalNodes...),
	})
	p.errorLines = append(p.errorLines, PropertiesErrorLine{
		node:         errorNode,
		logicalLine:  logicalNode,
		naturalLines: append([]document.NodeRef(nil), naturalNodes...),
		span:         span,
		code:         code,
	})
	return p.diagnostic(code, protocol.CategorySyntax,
		errorSpan.StartByte(), errorSpan.EndByte())
}

// assignDuplicateGroups numbers every exact-key group with more than one
// member in deterministic code-unit key order (parser.rs).
func (p *parser) assignDuplicateGroups() *FormationFailure {
	type group struct {
		indices []int
	}
	groups := map[string]*group{}
	var keys []string
	for index, property := range p.properties {
		key := string(property.key.Utf16beBytes())
		if _, ok := groups[key]; !ok {
			groups[key] = &group{}
			keys = append(keys, key)
		}
		groups[key].indices = append(groups[key].indices, index)
	}
	sort.Strings(keys)
	nextGroup := uint32(1)
	for _, key := range keys {
		indices := groups[key].indices
		if len(indices) <= 1 {
			continue
		}
		if failure := p.checkLimit("duplicate-group-members", len(indices),
			p.limits.MaxDuplicateGroupMembers); failure != nil {
			return failure
		}
		groupNumber := nextGroup
		nextGroup++
		for _, index := range indices {
			number := groupNumber
			p.properties[index].duplicateGroup = &number
		}
	}
	return nil
}

func (p *parser) buildStructuralPieces() ([]StructuralPiece, []PropertiesSyntaxKind, *FormationFailure) {
	var pieces []StructuralPiece
	var syntaxKinds []PropertiesSyntaxKind
	cursor := 0
	for cursor < len(p.atoms) {
		syntax := SyntaxKindErrorRegion
		if p.atoms[cursor].syntax != nil {
			syntax = *p.atoms[cursor].syntax
		}
		kind := structuralKind(syntax)
		start := cursor
		cursor++
		for cursor < len(p.atoms) {
			next := SyntaxKindErrorRegion
			if p.atoms[cursor].syntax != nil {
				next = *p.atoms[cursor].syntax
			}
			if next != syntax || p.atoms[cursor].rawStart != p.atoms[cursor-1].rawEnd {
				break
			}
			cursor++
		}
		if failure := p.checkLimit("syntax-pieces", len(pieces)+1,
			p.limits.Common.MaxTokenCount); failure != nil {
			return nil, nil, failure
		}
		span, failure := p.atomSpan(start, cursor)
		if failure != nil {
			return nil, nil, failure
		}
		pieces = append(pieces, document.NewStructuralPiece(span, kind))
		syntaxKinds = append(syntaxKinds, syntax)
	}
	return pieces, syntaxKinds, nil
}

func (p *parser) markAtoms(start, end int, syntax PropertiesSyntaxKind) {
	for index := start; index < end; index++ {
		p.atoms[index].syntax = &syntax
	}
}

func (p *parser) markLogicalPositions(logicalAtoms []int, start, end int, syntax PropertiesSyntaxKind) {
	for position := start; position < end; position++ {
		p.atoms[logicalAtoms[position]].syntax = &syntax
	}
}

// fragmentSpans splits one key/value range into maximal contiguous raw
// source fragments (continuation gaps split the range).
func (p *parser) fragmentSpans(logicalAtoms []int, start, end int) ([]document.Span, *FormationFailure) {
	var spans []document.Span
	if start >= end {
		return spans, nil
	}
	fragmentStart := logicalAtoms[start]
	previous := fragmentStart
	for position := start + 1; position < end; position++ {
		current := logicalAtoms[position]
		if p.atoms[current].rawStart != p.atoms[previous].rawEnd {
			span, failure := p.atomSpan(fragmentStart, previous+1)
			if failure != nil {
				return nil, failure
			}
			spans = append(spans, span)
			fragmentStart = current
		}
		previous = current
	}
	span, failure := p.atomSpan(fragmentStart, previous+1)
	if failure != nil {
		return nil, failure
	}
	spans = append(spans, span)
	return spans, nil
}

func (p *parser) logicalSourceSpan(firstLine, lastLine int) (document.Span, *FormationFailure) {
	first := &p.lines[firstLine]
	last := &p.lines[lastLine]
	return p.atomSpan(first.atomStart, last.atomContentEnd)
}

func (p *parser) logicalAnchorSpan(logicalAtoms []int, position, emptyFallback int) (document.Span, *FormationFailure) {
	raw := emptyFallback
	if len(logicalAtoms) > 0 {
		raw = p.atoms[logicalAtoms[len(logicalAtoms)-1]].rawEnd
	}
	if position < len(logicalAtoms) {
		raw = p.atoms[logicalAtoms[position]].rawStart
	}
	span, err := p.authority.Span(raw, raw)
	if err != nil {
		return document.Span{}, resourceLimitFailure("source-coordinate-boundary", 1, 0)
	}
	return span, nil
}

func (p *parser) atomSpan(start, end int) (document.Span, *FormationFailure) {
	rawStart := p.source.Len()
	if start < len(p.atoms) {
		rawStart = p.atoms[start].rawStart
	}
	rawEnd := rawStart
	if start != end {
		rawEnd = p.source.Len()
		if end-1 < len(p.atoms) {
			rawEnd = p.atoms[end-1].rawEnd
		}
	}
	span, err := p.authority.Span(rawStart, rawEnd)
	if err != nil {
		return document.Span{}, resourceLimitFailure("source-coordinate-boundary", 1, 0)
	}
	return span, nil
}

func (p *parser) issueNode(role document.NodeRole) (document.NodeRef, *FormationFailure) {
	observed := int(p.nextNode) + 1
	if failure := p.checkLimit("nodes", observed, p.limits.Common.MaxNodeCount); failure != nil {
		return document.NodeRef{}, failure
	}
	node := p.authority.NodeRef(p.nextNode, role)
	p.nextNode++
	return node, nil
}

func (p *parser) checkLimit(name string, observed, limit int) *FormationFailure {
	if observed > limit {
		return resourceLimitFailure(name, observed, limit)
	}
	return nil
}

func (p *parser) diagnostic(code string, category protocol.DiagnosticCategory,
	start, end int) *FormationFailure {
	if failure := p.checkLimit("diagnostics", len(p.diagnostics)+1,
		p.limits.Common.MaxDiagnostics); failure != nil {
		return failure
	}
	diagnostic, err := protocol.NewDiagnostic(code, category, protocol.SeverityError,
		&protocol.SourceLocation{
			StartByte: uint64(start),
			EndByte:   uint64(end),
		}, nil, nil, nil, nil, p.occurrence,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return resourceLimitFailure("diagnostics", len(p.diagnostics)+1, 0)
	}
	p.diagnostics = append(p.diagnostics, diagnostic)
	p.occurrence++
	p.recovered = true
	return nil
}

func buildAtoms(source *document.SourceSnapshot, authority document.DocumentAuthority,
	limits PropertiesParseLimits) ([]atom, *FormationFailure) {
	text, ok := source.DecodedText()
	if !ok {
		return nil, resourceLimitFailure("source-coordinate-boundary", 1, 0)
	}
	atoms := make([]atom, 0, len(text))
	for decodedStart, ch := range text {
		decodedEnd := decodedStart + utf8.RuneLen(ch)
		rawStart, err := source.RawByteAt(document.NewUtf8ByteOffset(decodedStart))
		if err != nil {
			return nil, resourceLimitFailure("source-coordinate-boundary", 1, 0)
		}
		rawEnd, err := source.RawByteAt(document.NewUtf8ByteOffset(decodedEnd))
		if err != nil {
			return nil, resourceLimitFailure("source-coordinate-boundary", 1, 0)
		}
		atoms = append(atoms, atom{ch: ch, rawStart: rawStart, rawEnd: rawEnd})
	}
	return atoms, nil
}

// decodeJavaString decodes one raw key or value exactly left-to-right
// (parser.rs; RFC 0010 §7).
func decodeJavaString(atoms []atom, atomIndices []int) (decodedJavaString, *decodeError) {
	var units []uint16
	var escapes []escapeSpec
	unicodeEscapes := 0
	cursor := 0
	for cursor < len(atomIndices) {
		atomIndex := atomIndices[cursor]
		ch := atoms[atomIndex].ch
		if ch != '\\' {
			units = appendUTF16Units(units, ch)
			cursor++
			continue
		}
		if cursor+1 >= len(atomIndices) {
			return decodedJavaString{}, &decodeError{atomStart: atomIndex, atomEnd: atomIndex + 1}
		}
		next := atoms[atomIndices[cursor+1]].ch
		outputStart := len(units)
		var kind PropertiesEscapeKind
		consumed := 0
		switch {
		case next == 'u':
			if cursor+6 > len(atomIndices) {
				last := atomIndex
				if len(atomIndices) > 0 {
					last = atomIndices[len(atomIndices)-1]
				}
				return decodedJavaString{}, &decodeError{atomStart: atomIndex, atomEnd: last + 1}
			}
			var value uint16
			for _, digitIndex := range atomIndices[cursor+2 : cursor+6] {
				digit, ok := hexDigitValue(atoms[digitIndex].ch)
				if !ok {
					return decodedJavaString{}, &decodeError{atomStart: atomIndex, atomEnd: digitIndex + 1}
				}
				value = value<<4 | digit
			}
			units = append(units, value)
			unicodeEscapes++
			kind = EscapeKindUnicode
			consumed = 6
		case next == 't':
			units = append(units, '\t')
			kind = EscapeKindNamed
			consumed = 2
		case next == 'n':
			units = append(units, '\n')
			kind = EscapeKindNamed
			consumed = 2
		case next == 'r':
			units = append(units, '\r')
			kind = EscapeKindNamed
			consumed = 2
		case next == 'f':
			units = append(units, 0x0C)
			kind = EscapeKindNamed
			consumed = 2
		case next == '\\':
			units = append(units, '\\')
			kind = EscapeKindBackslash
			consumed = 2
		default:
			units = appendUTF16Units(units, next)
			kind = EscapeKindDroppedBackslash
			consumed = 2
		}
		specIndices := make([]int, consumed)
		for index := 0; index < consumed; index++ {
			specIndices[index] = atomIndices[cursor+index]
		}
		escapes = append(escapes, escapeSpec{
			atomIndices: specIndices,
			kind:        kind,
			outputStart: outputStart,
			outputEnd:   len(units),
		})
		cursor += consumed
	}
	return decodedJavaString{units: units, escapes: escapes, unicodeEscapes: unicodeEscapes}, nil
}

// appendUTF16Units appends the exact UTF-16 code units of one scalar.
func appendUTF16Units(units []uint16, ch rune) []uint16 {
	if ch > 0xFFFF {
		value := ch - 0x10000
		return append(units, uint16(0xD800+(value>>10)), uint16(0xDC00+(value&0x3FF)))
	}
	return append(units, uint16(ch))
}

func hexDigitValue(ch rune) (uint16, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return uint16(ch - '0'), true
	case ch >= 'a' && ch <= 'f':
		return uint16(ch-'a') + 10, true
	case ch >= 'A' && ch <= 'F':
		return uint16(ch-'A') + 10, true
	}
	return 0, false
}

func isPropertiesWhitespace(ch rune) bool {
	return ch == ' ' || ch == '\t' || ch == ''
}

func structuralKind(syntax PropertiesSyntaxKind) StructuralPieceKind {
	switch syntax {
	case SyntaxKindWhitespace, SyntaxKindLineBreak, SyntaxKindCommentMarker,
		SyntaxKindCommentText:
		return PieceTrivia
	case SyntaxKindErrorRegion:
		return PieceErrorRegion
	default: // Bom, Key, Separator, Value, EscapeMarker, EscapeBody, ContinuationMarker
		return PieceToken
	}
}

// sortDiagnostics applies the deterministic diagnostic order (the Rust
// Diagnostic::sort_deterministically: start byte, category, code,
// occurrence).
func sortDiagnostics(diagnostics []*protocol.Diagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left, right := diagnostics[i], diagnostics[j]
		leftStart := uint64(1<<63 - 1)
		rightStart := uint64(1<<63 - 1)
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
	})
}
