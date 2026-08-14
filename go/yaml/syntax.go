package yaml

import (
	"consema.dev/consema/document"
)

// This file implements the lossless syntax tokenizer (consema-yaml
// syntax.rs): every decoded scalar belongs to exactly one lexeme, lexemes
// are classified into the closed YamlSyntaxKind set, and the anchor/alias
// named occurrences are extracted for native composition. Lexeme
// boundaries are Unicode scalar offsets; the raw-byte spans are resolved
// through the source snapshot.

// lexeme is one classified source interval in decoded scalar space.
type lexeme struct {
	start int
	end   int
	kind  YamlSyntaxKind
}

// namedOccurrence is one anchor or alias lexeme with its raw span.
type namedOccurrence struct {
	name string
	span document.Span
}

// tokenized is the tokenizer result.
type tokenized struct {
	pieces  []StructuralPiece
	kinds   []YamlSyntaxKind
	anchors []namedOccurrence
	aliases []namedOccurrence
}

// syntaxScanner replicates the Rust Scanner (syntax.rs) dispatch
// exactly; every lexeme boundary must agree so the published piece layout
// is byte-identical across languages.
type syntaxScanner struct {
	chars                    []rune
	offset                   int
	lineStart                int
	maxTokens                int
	output                   []lexeme
	pendingBlockParentIndent *int
	plainLineActive          bool
	plainParentIndent        *int
}

// tokenize scans one source into the exhaustive piece index, the parallel
// kind list, and the anchor/alias occurrences.
func tokenize(source *document.SourceSnapshot, authority document.DocumentAuthority,
	limits document.ParseLimits) (*tokenized, *FormationFailure) {
	text, ok := source.DecodedText()
	if !ok {
		// YAML sources always have decoded text; a binary source is an
		// internal invariant violation.
		return nil, newNativeFailure("yaml.native.invalid-source-span@1")
	}
	scanner := &syntaxScanner{
		chars:     []rune(text),
		maxTokens: limits.MaxTokenCount,
	}
	lexemes, failure := scanner.scan()
	if failure != nil {
		return nil, failure
	}
	result := &tokenized{
		pieces: make([]StructuralPiece, 0, len(lexemes)),
		kinds:  make([]YamlSyntaxKind, 0, len(lexemes)),
	}
	resolver := &rawByteResolver{source: source}
	for _, item := range lexemes {
		start, err := resolver.resolve(item.start)
		if err != nil {
			return nil, newNativeFailure("yaml.native.invalid-source-span@1")
		}
		end, err := resolver.resolve(item.end)
		if err != nil {
			return nil, newNativeFailure("yaml.native.invalid-source-span@1")
		}
		span, err := authority.Span(start, end)
		if err != nil {
			return nil, newNativeFailure("yaml.native.invalid-source-span@1")
		}
		kind := StructuralPieceKind(PieceToken)
		if item.kind.IsTrivia() {
			kind = PieceTrivia
		} else if item.kind == SyntaxKindErrorRegion {
			kind = PieceErrorRegion
		}
		result.pieces = append(result.pieces, document.NewStructuralPiece(span, kind))
		result.kinds = append(result.kinds, item.kind)
		if item.kind == SyntaxKindAnchor || item.kind == SyntaxKindAlias {
			name := string(scanner.chars[item.start+1 : item.end])
			if item.kind == SyntaxKindAnchor {
				result.anchors = append(result.anchors, namedOccurrence{name: name, span: span})
			} else {
				result.aliases = append(result.aliases, namedOccurrence{name: name, span: span})
			}
		}
	}
	index, err := NewLosslessStructuralIndex(authority.Identity(), source.Len(), result.pieces)
	if err != nil {
		// The scanner partitions every raw source byte exactly once; a
		// violation is an internal invariant failure.
		return nil, newNativeFailure("yaml.native.invalid-source-span@1")
	}
	result.pieces = index.Pieces()
	return result, nil
}

func (s *syntaxScanner) push(start, end int, kind YamlSyntaxKind) *FormationFailure {
	observed := len(s.output) + 1
	if observed > s.maxTokens {
		return resourceLimitFailure("syntax-pieces", observed, s.maxTokens)
	}
	s.output = append(s.output, lexeme{start: start, end: end, kind: kind})
	return nil
}

// scan runs the exact dispatch order of the Rust Scanner::scan.
func (s *syntaxScanner) scan() ([]lexeme, *FormationFailure) {
	for s.offset < len(s.chars) {
		if s.offset == s.lineStart && s.pendingBlockParentIndent != nil {
			consumed, failure := s.scanBlockContent()
			if failure != nil {
				return nil, failure
			}
			if consumed {
				continue
			}
		}
		start := s.offset
		current := s.chars[start]
		if !(current == ' ' || current == '\t' || current == '\r' || current == '\n') &&
			!s.plainLineActive && s.plainParentIndent != nil {
			if s.lineIndent() > *s.plainParentIndent && !s.startsIndentedStructure() {
				s.takeUntilBreak()
				if failure := s.push(start, s.offset, SyntaxKindPlainScalar); failure != nil {
					return nil, failure
				}
				s.plainLineActive = true
				continue
			}
			s.plainParentIndent = nil
		}
		switch {
		case current == '\uFEFF':
			s.offset++
			if failure := s.push(start, s.offset, SyntaxKindBom); failure != nil {
				return nil, failure
			}
			s.endPlainScalar()
			if start == s.lineStart {
				s.lineStart = s.offset
			}
		case current == ' ' || current == '\t':
			s.takeWhile(func(item rune) bool { return item == ' ' || item == '\t' })
			if failure := s.push(start, s.offset, SyntaxKindWhitespace); failure != nil {
				return nil, failure
			}
		case current == '\r' || current == '\n':
			if failure := s.scanNewline(start); failure != nil {
				return nil, failure
			}
		case current == '#':
			s.takeUntilBreak()
			if failure := s.push(start, s.offset, SyntaxKindComment); failure != nil {
				return nil, failure
			}
			s.endPlainScalar()
		case s.atDirective():
			s.takeUntilBreak()
			if failure := s.push(start, s.offset, SyntaxKindDirective); failure != nil {
				return nil, failure
			}
			s.endPlainScalar()
		case s.atDocumentIndicator('-', '-', '-'):
			s.offset += 3
			if failure := s.push(start, s.offset, SyntaxKindDocumentStart); failure != nil {
				return nil, failure
			}
			s.endPlainScalar()
		case s.atDocumentIndicator('.', '.', '.'):
			s.offset += 3
			if failure := s.push(start, s.offset, SyntaxKindDocumentEnd); failure != nil {
				return nil, failure
			}
			s.endPlainScalar()
		case current == '\'' || current == '"':
			s.scanQuoted(current)
			kind := SyntaxKindSingleQuotedScalar
			if current == '"' {
				kind = SyntaxKindDoubleQuotedScalar
			}
			if failure := s.push(start, s.offset, kind); failure != nil {
				return nil, failure
			}
			s.endPlainScalar()
		case (current == '|' || current == '>') && s.isBlockHeader():
			parentIndent := s.lineIndent()
			s.takeUntilBreak()
			kind := SyntaxKindLiteralBlockHeader
			if current == '>' {
				kind = SyntaxKindFoldedBlockHeader
			}
			if failure := s.push(start, s.offset, kind); failure != nil {
				return nil, failure
			}
			s.pendingBlockParentIndent = &parentIndent
			s.endPlainScalar()
		case (current == '&' || current == '*' || current == '!') && !s.plainLineActive:
			s.offset++
			s.takeWhile(func(item rune) bool {
				return !isSeparation(item) && !isFlowIndicator(item)
			})
			kind := SyntaxKindAnchor
			switch current {
			case '*':
				kind = SyntaxKindAlias
			case '!':
				kind = SyntaxKindTag
			}
			if failure := s.push(start, s.offset, kind); failure != nil {
				return nil, failure
			}
			s.endPlainScalar()
		default:
			if kind, ok := s.indicatorKind(); ok {
				s.offset++
				if failure := s.push(start, s.offset, kind); failure != nil {
					return nil, failure
				}
				s.endPlainScalar()
			} else {
				s.scanPlain()
				if failure := s.push(start, s.offset, SyntaxKindPlainScalar); failure != nil {
					return nil, failure
				}
				if !s.plainLineActive {
					indent := s.lineIndent()
					s.plainParentIndent = &indent
				}
				s.plainLineActive = true
			}
		}
	}
	return s.output, nil
}

func (s *syntaxScanner) scanNewline(start int) *FormationFailure {
	if s.chars[s.offset] == '\r' && s.offset+1 < len(s.chars) && s.chars[s.offset+1] == '\n' {
		s.offset += 2
	} else {
		s.offset++
	}
	if failure := s.push(start, s.offset, SyntaxKindNewline); failure != nil {
		return failure
	}
	s.lineStart = s.offset
	s.plainLineActive = false
	return nil
}

func (s *syntaxScanner) endPlainScalar() {
	s.plainLineActive = false
	s.plainParentIndent = nil
}

// startsIndentedStructure replicates syntax.rs.
func (s *syntaxScanner) startsIndentedStructure() bool {
	if (s.chars[s.offset] == '-' || s.chars[s.offset] == '?') &&
		(s.offset+1 >= len(s.chars) || isSeparation(s.chars[s.offset+1])) {
		return true
	}
	for cursor := s.offset; cursor < len(s.chars); cursor++ {
		character := s.chars[cursor]
		if character == '\r' || character == '\n' || character == '#' {
			return false
		}
		if character == ':' && (cursor+1 >= len(s.chars) || isSeparation(s.chars[cursor+1])) {
			return true
		}
	}
	return false
}

// scanQuoted replicates syntax.rs.
func (s *syntaxScanner) scanQuoted(quote rune) {
	s.offset++
	for s.offset < len(s.chars) {
		current := s.chars[s.offset]
		s.offset++
		if quote == '"' && current == '\\' && s.offset < len(s.chars) {
			if s.chars[s.offset] == '\r' {
				s.offset++
				if s.offset < len(s.chars) && s.chars[s.offset] == '\n' {
					s.offset++
				}
				s.lineStart = s.offset
			} else if s.chars[s.offset] == '\n' {
				s.offset++
				s.lineStart = s.offset
			} else {
				s.offset++
			}
		} else if current == quote {
			if quote == '\'' && s.offset < len(s.chars) && s.chars[s.offset] == '\'' {
				s.offset++
			} else {
				break
			}
		} else if current == '\n' {
			s.lineStart = s.offset
		} else if current == '\r' {
			if s.offset < len(s.chars) && s.chars[s.offset] == '\n' {
				s.offset++
			}
			s.lineStart = s.offset
		}
	}
}

// scanPlain replicates syntax.rs.
func (s *syntaxScanner) scanPlain() {
	s.offset++
	for s.offset < len(s.chars) {
		current := s.chars[s.offset]
		if isSeparation(current) || isFlowIndicator(current) {
			break
		}
		if current == ':' {
			if s.offset+1 >= len(s.chars) || isSeparation(s.chars[s.offset+1]) ||
				isFlowIndicator(s.chars[s.offset+1]) {
				break
			}
		}
		s.offset++
	}
}

// scanBlockContent replicates syntax.rs.
func (s *syntaxScanner) scanBlockContent() (bool, *FormationFailure) {
	parentIndent := *s.pendingBlockParentIndent
	start := s.offset
	acceptedEnd := start
	cursor := start
	for cursor < len(s.chars) {
		lineEnd := nextLineEndRune(s.chars, cursor)
		contentEnd := lineContentEndRune(s.chars, cursor, lineEnd)
		indent := 0
		for cursor+indent < contentEnd && s.chars[cursor+indent] == ' ' {
			indent++
		}
		blank := true
		for index := cursor + indent; index < contentEnd; index++ {
			character := s.chars[index]
			if character != ' ' && character != '\t' {
				blank = false
				break
			}
		}
		if !blank && indent <= parentIndent {
			break
		}
		acceptedEnd = lineEnd
		cursor = lineEnd
	}
	s.pendingBlockParentIndent = nil
	if acceptedEnd == start {
		return false, nil
	}
	s.offset = acceptedEnd
	s.lineStart = acceptedEnd
	if failure := s.push(start, acceptedEnd, SyntaxKindBlockScalarContent); failure != nil {
		return false, failure
	}
	return true, nil
}

func (s *syntaxScanner) indicatorKind() (YamlSyntaxKind, bool) {
	current := s.chars[s.offset]
	switch current {
	case '[':
		return SyntaxKindFlowSequenceStart, true
	case ']':
		return SyntaxKindFlowSequenceEnd, true
	case '{':
		return SyntaxKindFlowMappingStart, true
	case '}':
		return SyntaxKindFlowMappingEnd, true
	case ',':
		return SyntaxKindFlowEntry, true
	case '-':
		if s.followedBySeparation(1) {
			return SyntaxKindSequenceEntry, true
		}
	case '?':
		if s.followedBySeparation(1) {
			return SyntaxKindExplicitKey, true
		}
	case ':':
		if s.followedBySeparation(1) {
			return SyntaxKindMappingValue, true
		}
	}
	return "", false
}

func (s *syntaxScanner) atDirective() bool {
	return s.offset == s.lineStart && s.chars[s.offset] == '%'
}

func (s *syntaxScanner) atDocumentIndicator(a, b, c rune) bool {
	if s.offset != s.lineStart || s.offset+3 > len(s.chars) {
		return false
	}
	if s.chars[s.offset] != a || s.chars[s.offset+1] != b || s.chars[s.offset+2] != c {
		return false
	}
	return s.followedBySeparation(3)
}

func (s *syntaxScanner) followedBySeparation(length int) bool {
	if s.offset+length >= len(s.chars) {
		return true
	}
	return isSeparation(s.chars[s.offset+length])
}

// isBlockHeader replicates syntax.rs: the rest of the line must be
// only chomping/indent indicators, separation, or a comment.
func (s *syntaxScanner) isBlockHeader() bool {
	for index := s.offset + 1; index < len(s.chars); index++ {
		character := s.chars[index]
		if character == '\r' || character == '\n' {
			break
		}
		switch character {
		case '+', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ' ', '\t', '#':
		default:
			return false
		}
	}
	return true
}

func (s *syntaxScanner) lineIndent() int {
	count := 0
	for index := s.lineStart; index < s.offset && s.chars[index] == ' '; index++ {
		count++
	}
	return count
}

func (s *syntaxScanner) takeUntilBreak() {
	s.takeWhile(func(item rune) bool {
		return item != '\r' && item != '\n'
	})
}

func (s *syntaxScanner) takeWhile(predicate func(rune) bool) {
	for s.offset < len(s.chars) && predicate(s.chars[s.offset]) {
		s.offset++
	}
}

func nextLineEndRune(chars []rune, start int) int {
	cursor := start
	for cursor < len(chars) && chars[cursor] != '\r' && chars[cursor] != '\n' {
		cursor++
	}
	if cursor < len(chars) && chars[cursor] == '\r' {
		cursor++
		if cursor < len(chars) && chars[cursor] == '\n' {
			cursor++
		}
	} else if cursor < len(chars) && chars[cursor] == '\n' {
		cursor++
	}
	return cursor
}

func lineContentEndRune(chars []rune, start, lineEnd int) int {
	end := lineEnd
	if end > start && chars[end-1] == '\n' {
		end--
	}
	if end > start && chars[end-1] == '\r' {
		end--
	}
	return end
}

// rawByteResolver converts decoded Unicode scalar offsets to exact raw
// byte offsets with one forward walk (the Go counterpart of the Rust
// RawByteResolver; consema-yaml offsets.rs). The selected encoding is
// always UTF-8 or a BOM-detected UTF-16 variant, so the raw width of one
// decoded scalar is derivable from the scalar value.
type rawByteResolver struct {
	source  *document.SourceSnapshot
	scalar  int
	rawByte int
	utf8    int
}

func (r *rawByteResolver) resolve(scalar int) (int, error) {
	if scalar < r.scalar {
		r.scalar = 0
		r.rawByte = 0
		r.utf8 = 0
	}
	text, _ := r.source.DecodedText()
	for r.scalar < scalar {
		character, size := decodeRuneAt(text, r.utf8)
		r.rawByte += r.scalarRawWidth(character)
		r.utf8 += size
		r.scalar++
	}
	return r.rawByte, nil
}

// scalarRawWidth returns the raw byte width of one decoded scalar.
func (r *rawByteResolver) scalarRawWidth(character rune) int {
	switch r.source.EncodingFacts().Selected().Kind() {
	case document.EncodingUtf16Le, document.EncodingUtf16Be:
		if character >= 0x10000 {
			return 4
		}
		return 2
	default:
		return utf8RuneLen(character)
	}
}

// utf8RuneLen returns the UTF-8 byte width of one rune.
func utf8RuneLen(character rune) int {
	switch {
	case character < 0x80:
		return 1
	case character < 0x800:
		return 2
	case character < 0x10000:
		return 3
	default:
		return 4
	}
}

// decodeRuneAt decodes one rune and its UTF-8 width.
func decodeRuneAt(text string, offset int) (rune, int) {
	character := text[offset]
	if character < 0x80 {
		return rune(character), 1
	}
	length := 0
	switch {
	case character&0xE0 == 0xC0:
		length = 2
	case character&0xF0 == 0xE0:
		length = 3
	case character&0xF8 == 0xF0:
		length = 4
	}
	if offset+length > len(text) {
		return 0xFFFD, 1
	}
	value := rune(character & (0x7F >> length))
	for index := 1; index < length; index++ {
		value = value<<6 | rune(text[offset+index]&0x3F)
	}
	return value, length
}
