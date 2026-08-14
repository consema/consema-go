package toml

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// Parse forms one complete immutable TOML 1.0 document snapshot
// (consema-toml parser.rs; RFC 0001 §3). The formation order is:
// max_source_bytes, UTF-8 validation, tokenization
// (max_token_count), delimiter preflight (max_nesting_depth), the full
// TOML 1.0 grammar, and entity construction (max_node_count,
// max_nesting_depth). Any resource-limit hit returns
// core.parse.resource-limit@1; a TOML grammar failure returns
// toml.parse.syntax@1. There is no truncated success and no recovery in
// the 0.2 profile.
func Parse(source []byte, profile TomlProfile, limits document.ParseLimits) (*Document, *FormationFailure) {
	if len(source) > limits.MaxSourceBytes {
		return nil, resourceLimitFailure("source_bytes", len(source), limits.MaxSourceBytes)
	}
	snapshot, err := document.NewSourceSnapshotFromUTF8(source)
	if err != nil {
		return nil, sourceFailure(err)
	}
	text, _ := snapshot.DecodedText()
	authority := document.NewDocumentAuthority()
	pieces, kinds, failure := tokenize(text, &authority, limits.MaxTokenCount)
	if failure != nil {
		return nil, failure
	}
	if failure := preflightDelimiterNesting(text, pieces, limits.MaxNestingDepth); failure != nil {
		return nil, failure
	}
	index, err := NewLosslessStructuralIndex(authority.Identity(), len(source), pieces)
	if err != nil {
		// The tokenizer creates exact source coverage by construction.
		return nil, newFormationFailure("toml.parse.syntax@1", protocol.CategorySyntax, 0, 0,
			map[string]string{"parser_reason": "tokenizer coverage invariant failed"})
	}
	table, failure := parseDocument(text)
	if failure != nil {
		return nil, failure
	}
	builder := &entityBuilder{
		authority: &authority,
		sourceLen: len(source),
		limits:    limits,
	}
	root, failure := builder.buildTable(table, true, 0, spanRange{0, len(source)})
	if failure != nil {
		return nil, failure
	}
	return &Document{
		authority: authority,
		source:    snapshot,
		profile:   profile,
		index:     index,
		kinds:     kinds,
		entities:  builder.entities,
		root:      root,
		limits:    limits,
	}, nil
}

// sourceFailure maps a SourceSnapshot construction failure onto the frozen
// fatal diagnostic (consema-document lib.rs).
func sourceFailure(err error) *FormationFailure {
	sourceError, ok := err.(*document.SourceError)
	if !ok {
		return newFormationFailure("core.source.invalid-sequence@1", protocol.CategoryLexical,
			-1, -1, nil)
	}
	switch sourceError.Kind {
	case document.SourceErrorInvalidUtf8:
		return newFormationFailure("core.source.invalid-utf8@1", protocol.CategoryLexical,
			sourceError.ByteOffset, sourceError.ByteOffset, nil)
	case document.SourceErrorInvalidSequence:
		return newFormationFailure("core.source.invalid-sequence@1", protocol.CategoryLexical,
			sourceError.ByteOffset, sourceError.ByteOffset,
			map[string]string{"encoding": sourceError.Encoding.AsStr()})
	case document.SourceErrorEncodingConflict:
		return newFormationFailure("core.source.encoding-conflict@1", protocol.CategoryEncoding,
			-1, -1, nil)
	case document.SourceErrorUnsupportedBom:
		return newFormationFailure("core.source.unsupported-bom@1", protocol.CategoryEncoding,
			-1, -1, nil)
	case document.SourceErrorResourceLimit, document.SourceErrorOffsetOverflow:
		arguments := map[string]string{"name": sourceError.Name}
		if sourceError.Observed != 0 || sourceError.Limit != 0 {
			arguments["observed"] = fmt.Sprint(sourceError.Observed)
			arguments["limit"] = fmt.Sprint(sourceError.Limit)
		}
		return newFormationFailure("core.source.resource-limit@1", protocol.CategoryResource,
			-1, -1, arguments)
	}
	return newFormationFailure("core.source.invalid-sequence@1", protocol.CategoryLexical,
		-1, -1, nil)
}

// spanRange is an inclusive-start exclusive-end byte range during parsing.
type spanRange struct {
	start, end int
}

// tokenize produces the exhaustive lossless pieces and syntax kinds
// (consema-toml parser.rs). The tokenizer is byte-mechanical and
// never fails except for max_token_count; the semantic parser validates
// the grammar.
func tokenize(text string, authority *document.DocumentAuthority,
	maxCount int) ([]StructuralPiece, []TomlSyntaxKind, *FormationFailure) {
	bytes := []byte(text)
	pieces := make([]StructuralPiece, 0, len(bytes)/4)
	kinds := make([]TomlSyntaxKind, 0, len(bytes)/4)
	cursor := 0
	for cursor < len(bytes) {
		var end int
		var kind StructuralPieceKind
		var syntax TomlSyntaxKind
		switch {
		case bytes[cursor] == ' ' || bytes[cursor] == '\t':
			end = cursor + 1
			for end < len(bytes) && (bytes[end] == ' ' || bytes[end] == '\t') {
				end++
			}
			kind, syntax = PieceTrivia, SyntaxKindWhitespace
		case bytes[cursor] == '\r' || bytes[cursor] == '\n':
			end = cursor + 1
			if bytes[cursor] == '\r' && cursor+1 < len(bytes) && bytes[cursor+1] == '\n' {
				end = cursor + 2
			}
			kind, syntax = PieceTrivia, SyntaxKindNewline
		case bytes[cursor] == '#':
			end = cursor + 1
			for end < len(bytes) && bytes[end] != '\r' && bytes[end] != '\n' {
				end++
			}
			kind, syntax = PieceTrivia, SyntaxKindComment
		case bytes[cursor] == '\'' || bytes[cursor] == '"':
			end = tokenizerStringEnd(bytes, cursor)
			kind, syntax = PieceToken, SyntaxKindString
		case isPunctuation(bytes[cursor]):
			end = cursor + 1
			kind, syntax = PieceToken, punctuationKind(bytes[cursor])
		default:
			end = cursor + 1
			for end < len(bytes) && !isASCIIWhitespace(bytes[end]) && bytes[end] != '#' &&
				!isPunctuation(bytes[end]) && bytes[end] != '\'' && bytes[end] != '"' {
				end++
			}
			kind, syntax = PieceToken, SyntaxKindBare
		}
		if len(pieces)+1 > maxCount {
			return nil, nil, resourceLimitFailure("token_count", len(pieces)+1, maxCount)
		}
		span, err := authority.Span(cursor, end)
		if err != nil {
			return nil, nil, newFormationFailure("toml.parse.syntax@1", protocol.CategorySyntax,
				cursor, end, nil)
		}
		pieces = append(pieces, document.NewStructuralPiece(span, kind))
		kinds = append(kinds, syntax)
		cursor = end
	}
	return pieces, kinds, nil
}

func isPunctuation(byte byte) bool {
	switch byte {
	case '=', '[', ']', '{', '}', ',', '.':
		return true
	}
	return false
}

func punctuationKind(byte byte) TomlSyntaxKind {
	switch byte {
	case '=':
		return SyntaxKindEquals
	case '[':
		return SyntaxKindLeftBracket
	case ']':
		return SyntaxKindRightBracket
	case '{':
		return SyntaxKindLeftBrace
	case '}':
		return SyntaxKindRightBrace
	case ',':
		return SyntaxKindComma
	case '.':
		return SyntaxKindDot
	}
	panic("toml: tokenizer filtered the byte before kind dispatch")
}

func isASCIIWhitespace(byte byte) bool {
	switch byte {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// tokenizerStringEnd scans a string token from its opening quote
// (consema-toml parser.rs): escapes skip two bytes, and triple
// quotes close on three consecutive quotes.
func tokenizerStringEnd(bytes []byte, start int) int {
	quote := bytes[start]
	triple := start+2 < len(bytes) && bytes[start+1] == quote && bytes[start+2] == quote
	cursor := start + 1
	if triple {
		cursor = start + 3
	}
	for cursor < len(bytes) {
		if quote == '"' && bytes[cursor] == '\\' {
			cursor += 2
			if cursor > len(bytes) {
				cursor = len(bytes)
			}
			continue
		}
		if triple {
			if cursor+2 < len(bytes) && bytes[cursor] == quote &&
				bytes[cursor+1] == quote && bytes[cursor+2] == quote {
				return cursor + 3
			}
		} else if bytes[cursor] == quote {
			return cursor + 1
		}
		cursor++
	}
	return len(bytes)
}

// preflightDelimiterNesting counts `[` and `{` tokens before the semantic
// parse (consema-toml parser.rs).
func preflightDelimiterNesting(text string, pieces []StructuralPiece, maxDepth int) *FormationFailure {
	depth := 0
	for _, piece := range pieces {
		if piece.Kind() != PieceToken {
			continue
		}
		span := piece.Span()
		token := text[span.StartByte():span.EndByte()]
		switch token {
		case "[", "{":
			depth++
			if depth > maxDepth {
				return resourceLimitFailure("nesting_depth", depth, maxDepth)
			}
		case "]", "}":
			depth--
		}
	}
	return nil
}

// parseError is the internal semantic parse failure; the first failure
// aborts formation.
type parseError struct {
	pos    int
	end    int
	reason string
}

func (e *parseError) Error() string { return e.reason }

func (e *parseError) failure() *FormationFailure {
	span := e.pos
	if span < 0 {
		span = 0
	}
	return newFormationFailure("toml.parse.syntax@1", protocol.CategorySyntax,
		span, e.end, map[string]string{"parser_reason": e.reason})
}

// pTable is the parse-tree table with toml_edit-equivalent semantics
// (toml_edit parser/state.rs): ordered items (keyvals and subtables
// interleaved in insertion order), dotted/implicit flavors, and the
// remove-and-reinsert behavior of implicit-table reuse.
type pTable struct {
	name     string
	keySpan  spanRange
	span     spanRange
	hasSpan  bool
	items    []*pItem
	byName   map[string]int
	isInline bool
	implicit bool
	dotted   bool
}

type pItemKind uint8

const (
	itemKeyval pItemKind = iota
	itemSubtable
	itemAOT
)

type pItem struct {
	name    string
	keySpan spanRange
	kind    pItemKind
	value   *pValue
	table   *pTable
	aot     *pAOT
}

type pAOT struct {
	name     string
	keySpan  spanRange
	span     spanRange
	hasSpan  bool
	elements []*pTable
}

// pValue is one parsed value with its exact raw span.
type pValue struct {
	span     spanRange
	kind     pValueKind
	str      string
	integer  int64
	bits     uint64
	boolean  bool
	dateTime TomlDateTime
	array    []*pValue
	table    *pTable // inline table root
}

type pValueKind uint8

const (
	valueString pValueKind = iota
	valueInteger
	valueFloat
	valueBoolean
	valueDateTime
	valueArray
	valueInlineTable
)

// parser is the byte-level TOML 1.0 scanner.
type parser struct {
	src         string
	pos         int
	root        *pTable
	current     *pTable
	currentPath []string
}

func parseDocument(text string) (*pTable, *FormationFailure) {
	root := &pTable{byName: map[string]int{}}
	p := &parser{src: text, root: root, current: root}
	// An optional BOM is skipped at the document start
	// (toml_edit parser/document.rs `opt(b"\xEF\xBB\xBF")`).
	if strings.HasPrefix(text, "\uFEFF") {
		p.pos = 3
	}
	if err := p.document(); err != nil {
		return nil, err.(*parseError).failure()
	}
	return root, nil
}

func (p *parser) document() error {
	for {
		p.skipWS()
		if p.pos >= len(p.src) {
			break
		}
		switch p.src[p.pos] {
		case '#':
			if err := p.parseComment(); err != nil {
				return err
			}
		case '\n':
			p.pos++
		case '\r':
			if p.pos+1 >= len(p.src) || p.src[p.pos+1] != '\n' {
				return p.errorAt(p.pos, p.pos+1, "expected `\\n` after `\\r`")
			}
			p.pos += 2
		case '[':
			if err := p.parseHeader(); err != nil {
				return err
			}
		default:
			if err := p.parseKeyval(); err != nil {
				return err
			}
		}
	}
	return p.finalize()
}

func (p *parser) skipWS() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) errorAt(start, end int, reason string) *parseError {
	return &parseError{pos: start, end: end, reason: reason}
}

// commentContent consumes `#` plus non-EOL content.
func (p *parser) commentContent() error {
	p.pos++ // '#'
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '\n' || c == '\r' {
			return nil
		}
		if c == '\t' || (c >= 0x20 && c <= 0x7E) || c >= 0x80 {
			p.pos++
			continue
		}
		return p.errorAt(p.pos, p.pos+1, "control character in comment")
	}
	return nil
}

// parseComment consumes a comment and requires a line ending (newline or
// EOF) (toml_edit parser/document.rs parse_comment).
func (p *parser) parseComment() error {
	if err := p.commentContent(); err != nil {
		return err
	}
	return p.lineEnding()
}

// lineEnding requires a newline (LF or CRLF) or EOF.
func (p *parser) lineEnding() error {
	if p.pos >= len(p.src) {
		return nil
	}
	switch p.src[p.pos] {
	case '\n':
		p.pos++
		return nil
	case '\r':
		if p.pos+1 < len(p.src) && p.src[p.pos+1] == '\n' {
			p.pos += 2
			return nil
		}
	}
	return p.errorAt(p.pos, p.pos+1, "expected newline")
}

// lineTrailing consumes ws and an optional comment, then requires a line
// ending (toml_edit parser/trivia.rs line-trailing).
func (p *parser) lineTrailing() error {
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == '#' {
		if err := p.commentContent(); err != nil {
			return err
		}
	}
	return p.lineEnding()
}

// wsCommentNewline consumes ws, comments, and newlines (toml_edit
// parser/trivia.rs ws-comment-newline), used inside arrays.
func (p *parser) wsCommentNewline() error {
	for {
		p.skipWS()
		if p.pos >= len(p.src) {
			return nil
		}
		switch p.src[p.pos] {
		case '#':
			if err := p.commentContent(); err != nil {
				return err
			}
			if err := p.lineEnding(); err != nil {
				return err
			}
		case '\n':
			p.pos++
		case '\r':
			if p.pos+1 >= len(p.src) || p.src[p.pos+1] != '\n' {
				return p.errorAt(p.pos, p.pos+1, "expected `\\n` after `\\r`")
			}
			p.pos += 2
		default:
			return nil
		}
	}
}

// keyPart is one decoded key segment with its raw token span.
type keyPart struct {
	name string
	span spanRange
}

// parseKey parses a dotted key (toml_edit parser/key.rs): simple keys
// joined by dots, with whitespace allowed around each dot.
func (p *parser) parseKey() ([]keyPart, error) {
	var parts []keyPart
	for {
		part, err := p.parseSimpleKey()
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != '.' {
			return parts, nil
		}
		p.pos++
		p.skipWS()
	}
}

// parseSimpleKey parses a bare, basic-string, or literal-string key
// (single-line forms only).
func (p *parser) parseSimpleKey() (keyPart, error) {
	if p.pos >= len(p.src) {
		return keyPart{}, p.errorAt(p.pos, p.pos, "expected key")
	}
	start := p.pos
	switch p.src[p.pos] {
	case '"':
		value, end, err := p.parseBasicString()
		if err != nil {
			return keyPart{}, err
		}
		return keyPart{name: value, span: spanRange{start, end}}, nil
	case '\'':
		value, end, err := p.parseLiteralString()
		if err != nil {
			return keyPart{}, err
		}
		return keyPart{name: value, span: spanRange{start, end}}, nil
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return keyPart{}, p.errorAt(p.pos, p.pos+1, "expected key")
	}
	return keyPart{name: p.src[start:p.pos], span: spanRange{start, p.pos}}, nil
}

// parseKeyval parses one key-value line and applies the toml_edit keyval
// semantics (toml_edit parser/state.rs on_keyval).
func (p *parser) parseKeyval() error {
	parts, err := p.parseKey()
	if err != nil {
		return err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != '=' {
		return p.errorAt(p.pos, p.pos+1, "expected `=`")
	}
	p.pos++
	p.skipWS()
	value, err := p.parseValue()
	if err != nil {
		return err
	}
	if err := p.lineTrailing(); err != nil {
		return err
	}
	leaf := parts[len(parts)-1]
	table, err := p.descendPath(p.current, parts[:len(parts)-1], true)
	if err != nil {
		return err
	}
	if table.dotted == (len(parts) == 1) {
		return p.errorAt(leaf.span.start, leaf.span.end,
			"dotted key redefines a defined table")
	}
	if _, exists := table.byName[leaf.name]; exists {
		return p.errorAt(leaf.span.start, leaf.span.end, "duplicate key `"+leaf.name+"`")
	}
	table.items = append(table.items, &pItem{
		name: leaf.name, keySpan: leaf.span, kind: itemKeyval, value: value,
	})
	table.byName[leaf.name] = len(table.items) - 1
	if table.hasSpan {
		table.span.end = value.span.end
	}
	return nil
}

// descendPath walks the dotted path, creating implicit (and, for keyval
// paths, dotted) tables as needed (toml_edit parser/state.rs
// descend_path).
func (p *parser) descendPath(table *pTable, path []keyPart, dotted bool) (*pTable, error) {
	for index, part := range path {
		position, exists := table.byName[part.name]
		if !exists {
			child := &pTable{name: part.name, keySpan: part.span, byName: map[string]int{},
				implicit: true, dotted: dotted}
			table.items = append(table.items, &pItem{
				name: part.name, keySpan: part.span, kind: itemSubtable, table: child,
			})
			table.byName[part.name] = len(table.items) - 1
			table = child
			continue
		}
		item := table.items[position]
		switch item.kind {
		case itemKeyval:
			return nil, p.errorAt(part.span.start, part.span.end,
				"dotted key `"+strings.Join(joinParts(path[:index+1]), ".")+
					"` attempted to extend non-table type ("+valueTypeName(item.value.kind)+")")
		case itemAOT:
			array := item.aot
			if len(array.elements) == 0 {
				return nil, p.errorAt(part.span.start, part.span.end, "empty array of tables")
			}
			table = array.elements[len(array.elements)-1]
		case itemSubtable:
			child := item.table
			if dotted && !child.implicit {
				return nil, p.errorAt(part.span.start, part.span.end,
					"dotted key `"+strings.Join(joinParts(path[:index+1]), ".")+
						"` redefines a defined table")
			}
			table = child
		}
	}
	return table, nil
}

func joinParts(parts []keyPart) []string {
	names := make([]string, len(parts))
	for index, part := range parts {
		names[index] = part.name
	}
	return names
}

func valueTypeName(kind pValueKind) string {
	switch kind {
	case valueString:
		return "string"
	case valueInteger:
		return "integer"
	case valueFloat:
		return "float"
	case valueBoolean:
		return "boolean"
	case valueDateTime:
		return "datetime"
	case valueArray:
		return "array"
	case valueInlineTable:
		return "inline table"
	}
	return "value"
}

// parseHeader parses a standard `[a.b]` or array-of-tables `[[a.b]]`
// header and switches the current table (toml_edit parser/state.rs
// on_std_header/on_array_header).
func (p *parser) parseHeader() error {
	start := p.pos
	arrayTable := p.pos+1 < len(p.src) && p.src[p.pos+1] == '['
	if arrayTable {
		p.pos += 2
	} else {
		p.pos++
	}
	p.skipWS()
	parts, err := p.parseKey()
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return p.errorAt(start, p.pos, "empty table header")
	}
	p.skipWS()
	if arrayTable {
		if p.pos+1 >= len(p.src) || p.src[p.pos] != ']' || p.src[p.pos+1] != ']' {
			return p.errorAt(p.pos, p.pos+1, "expected `]]`")
		}
		p.pos += 2
	} else {
		if p.pos >= len(p.src) || p.src[p.pos] != ']' {
			return p.errorAt(p.pos, p.pos+1, "expected `]`")
		}
		p.pos++
	}
	if err := p.lineTrailing(); err != nil {
		return err
	}
	headerSpan := spanRange{start, p.pos}

	// Finalize the previous current table first.
	if err := p.finalize(); err != nil {
		return err
	}
	leaf := parts[len(parts)-1]
	parent, err := p.descendPath(p.root, parts[:len(parts)-1], false)
	if err != nil {
		return err
	}
	if arrayTable {
		position, exists := parent.byName[leaf.name]
		var array *pAOT
		if exists {
			if parent.items[position].kind != itemAOT {
				return p.errorAt(leaf.span.start, leaf.span.end, "duplicate key `"+leaf.name+"`")
			}
			array = parent.items[position].aot
		} else {
			array = &pAOT{name: leaf.name, keySpan: leaf.span}
			parent.items = append(parent.items, &pItem{
				name: leaf.name, keySpan: leaf.span, kind: itemAOT, aot: array,
			})
			parent.byName[leaf.name] = len(parent.items) - 1
		}
		// The element table is appended to the array at finalize
		// (toml_edit finalize_table), keeping the array span computation
		// intact.
		table := &pTable{name: leaf.name, keySpan: leaf.span, span: headerSpan,
			hasSpan: true, byName: map[string]int{}}
		p.current = table
		p.currentPath = joinParts(parts)
		return nil
	}
	position, exists := parent.byName[leaf.name]
	if exists {
		item := parent.items[position]
		if item.kind != itemSubtable || !item.table.implicit || item.table.dotted {
			return p.errorAt(leaf.span.start, leaf.span.end, "duplicate key `"+leaf.name+"`")
		}
		// Reuse the implicit table (its children are preserved); it is
		// removed now and reinserted at finalize, moving it to the end of
		// the parent's items (toml_edit start_table remove/reinsert).
		table := item.table
		table.implicit = false
		table.dotted = false
		table.span = headerSpan
		table.hasSpan = true
		parent.items = append(parent.items[:position], parent.items[position+1:]...)
		rebuildByName(parent)
		p.current = table
		p.currentPath = joinParts(parts)
		return nil
	}
	// The new table is inserted into the parent at finalize (toml_edit
	// finalize_table); inserting at header time would make the finalize
	// see an occupied non-implicit entry.
	table := &pTable{name: leaf.name, keySpan: leaf.span, span: headerSpan,
		hasSpan: true, byName: map[string]int{}}
	p.current = table
	p.currentPath = joinParts(parts)
	return nil
}

func rebuildByName(table *pTable) {
	table.byName = make(map[string]int, len(table.items))
	for index, item := range table.items {
		table.byName[item.name] = index
	}
}

// finalize inserts the current table into its parent (toml_edit
// parser/state.rs finalize_table); for arrays of tables it appends the
// element table to the array and extends the array span.
func (p *parser) finalize() error {
	table := p.current
	if table == p.root {
		return nil
	}
	path := p.currentPath
	parent, err := p.descendPath(p.root, pathParts(path[:len(path)-1]), false)
	if err != nil {
		return err
	}
	leaf := path[len(path)-1]
	position, exists := parent.byName[leaf]
	if !exists {
		parent.items = append(parent.items, &pItem{
			name: leaf, keySpan: table.keySpan, kind: itemSubtable, table: table,
		})
		parent.byName[leaf] = len(parent.items) - 1
	} else {
		item := parent.items[position]
		switch {
		case item.kind == itemSubtable && item.table.implicit:
			item.table = table
			item.keySpan = table.keySpan
		case item.kind == itemAOT:
			array := item.aot
			array.elements = append(array.elements, table)
			if len(array.elements) == 1 {
				array.span = table.span
				array.hasSpan = true
			} else {
				array.span.end = table.span.end
			}
			p.current = p.root
			p.currentPath = nil
			return nil
		default:
			return p.errorAt(0, 0, "duplicate key `"+leaf+"`")
		}
	}
	p.current = p.root
	p.currentPath = nil
	return nil
}

func pathParts(path []string) []keyPart {
	parts := make([]keyPart, len(path))
	for index, name := range path {
		parts[index] = keyPart{name: name}
	}
	return parts
}

// parseValue parses one complete value (toml_edit parser/value.rs):
// strings, booleans, arrays, inline tables, date-times, floats, and
// integers, in the toml_edit dispatch order.
func (p *parser) parseValue() (*pValue, error) {
	if p.pos >= len(p.src) {
		return nil, p.errorAt(p.pos, p.pos, "expected value")
	}
	start := p.pos
	switch c := p.src[p.pos]; {
	case c == '"' || c == '\'':
		return p.parseStringValue(start)
	case c == '[':
		return p.parseArray(start)
	case c == '{':
		return p.parseInlineTableValue(start)
	case c == '+' || c == '-' || (c >= '0' && c <= '9'):
		value, err := p.parseNumberValue(start)
		if err != nil {
			return nil, err
		}
		return value, nil
	case c == '_':
		return nil, p.errorAt(p.pos, p.pos+1, "expected leading digit")
	case c == '.':
		return nil, p.errorAt(p.pos, p.pos+1, "expected leading digit")
	case c == 't':
		if strings.HasPrefix(p.src[p.pos:], "true") {
			p.pos += 4
			return &pValue{span: spanRange{start, p.pos}, kind: valueBoolean, boolean: true}, nil
		}
		return nil, p.errorAt(p.pos, p.pos+1, "expected string value")
	case c == 'f':
		if strings.HasPrefix(p.src[p.pos:], "false") {
			p.pos += 5
			return &pValue{span: spanRange{start, p.pos}, kind: valueBoolean, boolean: false}, nil
		}
		return nil, p.errorAt(p.pos, p.pos+1, "expected string value")
	case c == 'i':
		if strings.HasPrefix(p.src[p.pos:], "inf") {
			p.pos += 3
			return &pValue{span: spanRange{start, p.pos}, kind: valueFloat,
				bits: math.Float64bits(math.Inf(1))}, nil
		}
		return nil, p.errorAt(p.pos, p.pos+1, "expected string value")
	case c == 'n':
		if strings.HasPrefix(p.src[p.pos:], "nan") {
			p.pos += 3
			return &pValue{span: spanRange{start, p.pos}, kind: valueFloat,
				bits: 0x7ff8000000000000}, nil
		}
		return nil, p.errorAt(p.pos, p.pos+1, "expected string value")
	default:
		return nil, p.errorAt(p.pos, p.pos+1, "expected string value")
	}
}

func (p *parser) parseStringValue(start int) (*pValue, error) {
	value, end, err := p.parseStringToken()
	if err != nil {
		return nil, err
	}
	return &pValue{span: spanRange{start, end}, kind: valueString, str: value}, nil
}

// parseStringToken dispatches on the opening quote and returns the decoded
// value plus the exclusive end offset.
func (p *parser) parseStringToken() (string, int, error) {
	if strings.HasPrefix(p.src[p.pos:], `"""`) {
		return p.parseMultilineBasicString()
	}
	if strings.HasPrefix(p.src[p.pos:], `'''`) {
		return p.parseMultilineLiteralString()
	}
	if p.src[p.pos] == '"' {
		return p.parseBasicString()
	}
	return p.parseLiteralString()
}

// parseBasicString parses a single-line basic string; the returned end is
// after the closing quote.
func (p *parser) parseBasicString() (string, int, error) {
	start := p.pos
	p.pos++ // opening quote
	var output strings.Builder
	for {
		if p.pos >= len(p.src) {
			return "", 0, p.errorAt(start, p.pos, "unterminated basic string")
		}
		c := p.src[p.pos]
		switch {
		case c == '"':
			p.pos++
			return output.String(), p.pos, nil
		case c == '\\':
			decoded, err := p.parseEscape()
			if err != nil {
				return "", 0, err
			}
			output.WriteRune(decoded)
		case c == '\t' || (c >= 0x20 && c <= 0x21) || (c >= 0x23 && c <= 0x5B) ||
			(c >= 0x5D && c <= 0x7E) || c >= 0x80:
			p.pos++
			output.WriteByte(c)
		default:
			return "", 0, p.errorAt(p.pos, p.pos+1, "invalid basic string character")
		}
	}
}

// parseEscape parses one `\` escape sequence.
func (p *parser) parseEscape() (rune, error) {
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return 0, p.errorAt(p.pos-1, p.pos, "unterminated escape sequence")
	}
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 'b':
		return '\b', nil
	case 't':
		return '\t', nil
	case 'n':
		return '\n', nil
	case 'f':
		return '\f', nil
	case 'r':
		return '\r', nil
	case '"':
		return '"', nil
	case '\\':
		return '\\', nil
	case 'u':
		return p.parseHexEscape(4)
	case 'U':
		return p.parseHexEscape(8)
	}
	return 0, p.errorAt(p.pos-2, p.pos, "invalid escape sequence")
}

func (p *parser) parseHexEscape(digits int) (rune, error) {
	if p.pos+digits > len(p.src) {
		return 0, p.errorAt(p.pos, p.pos, "invalid unicode escape")
	}
	value, err := strconv.ParseUint(p.src[p.pos:p.pos+digits], 16, 32)
	if err != nil {
		return 0, p.errorAt(p.pos, p.pos+digits, "invalid unicode escape")
	}
	p.pos += digits
	scalar := rune(value)
	if !isValidScalar(scalar) {
		return 0, p.errorAt(p.pos-digits, p.pos, "unicode escape is not a scalar value")
	}
	return scalar, nil
}

func isValidScalar(scalar rune) bool {
	return scalar <= 0x10FFFF && !(scalar >= 0xD800 && scalar <= 0xDFFF)
}

// parseMultilineBasicString parses `"""..."""` (toml_edit
// parser/strings.rs ml-basic-string): the first newline is trimmed, CRLF
// is normalized to LF, backslash-line-ending continuations are trimmed,
// and runs of one or two quotes followed by content are literal.
func (p *parser) parseMultilineBasicString() (string, int, error) {
	start := p.pos
	p.pos += 3
	if p.pos < len(p.src) && (p.src[p.pos] == '\n' ||
		(p.src[p.pos] == '\r' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '\n')) {
		if p.src[p.pos] == '\r' {
			p.pos += 2
		} else {
			p.pos++
		}
	}
	var output strings.Builder
	for {
		if p.pos >= len(p.src) {
			return "", 0, p.errorAt(start, p.pos, "unterminated multiline basic string")
		}
		c := p.src[p.pos]
		switch {
		case strings.HasPrefix(p.src[p.pos:], `"""`):
			p.pos += 3
			return output.String(), p.pos, nil
		case c == '"' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '"' &&
			(p.pos+2 >= len(p.src) || p.src[p.pos+2] != '"'):
			output.WriteString(`""`)
			p.pos += 2
		case c == '"' && (p.pos+1 >= len(p.src) || p.src[p.pos+1] != '"'):
			output.WriteByte('"')
			p.pos++
		case c == '\\' && p.escapedNewlineAhead():
			if err := p.parseEscapedNewline(); err != nil {
				return "", 0, err
			}
		case c == '\\':
			decoded, err := p.parseEscape()
			if err != nil {
				return "", 0, err
			}
			output.WriteRune(decoded)
		case c == '\r':
			if p.pos+1 >= len(p.src) || p.src[p.pos+1] != '\n' {
				return "", 0, p.errorAt(p.pos, p.pos+1, "invalid multiline basic string character")
			}
			p.pos += 2
			output.WriteByte('\n')
		case c == '\n':
			p.pos++
			output.WriteByte('\n')
		case c == '\t' || (c >= 0x20 && c <= 0x21) || (c >= 0x23 && c <= 0x5B) ||
			(c >= 0x5D && c <= 0x7E) || c >= 0x80:
			p.pos++
			output.WriteByte(c)
		default:
			return "", 0, p.errorAt(p.pos, p.pos+1, "invalid multiline basic string character")
		}
	}
}

// escapedNewlineAhead reports whether the backslash at the current
// position begins a line-ending continuation: `\` ws newline.
func (p *parser) escapedNewlineAhead() bool {
	cursor := p.pos + 1
	for cursor < len(p.src) && (p.src[cursor] == ' ' || p.src[cursor] == '\t') {
		cursor++
	}
	if cursor >= len(p.src) {
		return false
	}
	return p.src[cursor] == '\n' ||
		(p.src[cursor] == '\r' && cursor+1 < len(p.src) && p.src[cursor+1] == '\n')
}

// parseEscapedNewline trims one or more `\` ws newline (wschar/newline)*
// continuations (toml_edit parser/strings.rs mlb-escaped-nl).
func (p *parser) parseEscapedNewline() error {
	for {
		p.pos++ // backslash
		p.skipWS()
		if p.pos >= len(p.src) {
			return p.errorAt(p.pos, p.pos, "unterminated line continuation")
		}
		switch p.src[p.pos] {
		case '\n':
			p.pos++
		case '\r':
			if p.pos+1 >= len(p.src) || p.src[p.pos+1] != '\n' {
				return p.errorAt(p.pos, p.pos+1, "expected `\\n` after `\\r`")
			}
			p.pos += 2
		default:
			return p.errorAt(p.pos, p.pos+1, "expected newline after `\\`")
		}
		for p.pos < len(p.src) {
			c := p.src[p.pos]
			if c == ' ' || c == '\t' || c == '\n' {
				p.pos++
				continue
			}
			if c == '\r' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '\n' {
				p.pos += 2
				continue
			}
			break
		}
		if p.pos >= len(p.src) || p.src[p.pos] != '\\' || !p.escapedNewlineAhead() {
			return nil
		}
	}
}

// parseLiteralString parses a single-line literal string.
func (p *parser) parseLiteralString() (string, int, error) {
	start := p.pos
	p.pos++ // opening apostrophe
	contentStart := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '\'' {
			value := p.src[contentStart:p.pos]
			p.pos++
			return value, p.pos, nil
		}
		if c == '\t' || (c >= 0x20 && c <= 0x26) || (c >= 0x28 && c <= 0x7E) || c >= 0x80 {
			p.pos++
			continue
		}
		return "", 0, p.errorAt(p.pos, p.pos+1, "invalid literal string character")
	}
	return "", 0, p.errorAt(start, p.pos, "unterminated literal string")
}

// parseMultilineLiteralString parses `”'...”'` (toml_edit
// parser/strings.rs ml-literal-string): the first newline is trimmed and
// CRLF is normalized to LF; no escapes; runs of one or two apostrophes
// followed by content are literal.
func (p *parser) parseMultilineLiteralString() (string, int, error) {
	start := p.pos
	p.pos += 3
	if p.pos < len(p.src) && (p.src[p.pos] == '\n' ||
		(p.src[p.pos] == '\r' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '\n')) {
		if p.src[p.pos] == '\r' {
			p.pos += 2
		} else {
			p.pos++
		}
	}
	contentStart := p.pos
	for {
		if p.pos >= len(p.src) {
			return "", 0, p.errorAt(start, p.pos, "unterminated multiline literal string")
		}
		c := p.src[p.pos]
		switch {
		case strings.HasPrefix(p.src[p.pos:], `'''`):
			value := strings.ReplaceAll(p.src[contentStart:p.pos], "\r\n", "\n")
			p.pos += 3
			return value, p.pos, nil
		case c == '\'' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '\'' &&
			(p.pos+2 >= len(p.src) || p.src[p.pos+2] != '\''):
			p.pos += 2
		case c == '\'' && (p.pos+1 >= len(p.src) || p.src[p.pos+1] != '\''):
			p.pos++
		case c == '\r':
			if p.pos+1 >= len(p.src) || p.src[p.pos+1] != '\n' {
				return "", 0, p.errorAt(p.pos, p.pos+1, "invalid multiline literal string character")
			}
			p.pos += 2
		case c == '\n':
			p.pos++
		case c == '\t' || (c >= 0x20 && c <= 0x26) || (c >= 0x28 && c <= 0x7E) || c >= 0x80:
			p.pos++
		default:
			return "", 0, p.errorAt(p.pos, p.pos+1, "invalid multiline literal string character")
		}
	}
}

// parseNumberValue parses a date-time, float, or integer (toml_edit
// parser/value.rs dispatch and parser/numbers.rs).
func (p *parser) parseNumberValue(start int) (*pValue, error) {
	// Date-time first; a cut failure inside it is a hard error.
	datetime, ok, cut, err := p.tryDateTime(start)
	if err != nil {
		return nil, err
	}
	if ok {
		return &pValue{span: spanRange{start, p.pos}, kind: valueDateTime, dateTime: datetime}, nil
	}
	if cut {
		return nil, p.errorAt(start, p.pos, "invalid date-time")
	}
	// Float: dec-int part with exp and/or fraction, or a special float;
	// the complete token must parse as a finite f64.
	if token, matched := p.tryFloatToken(); matched {
		text := strings.ReplaceAll(token, "_", "")
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsInf(value, 0) {
			return nil, p.errorAt(start, p.pos, "invalid floating-point number")
		}
		return &pValue{span: spanRange{start, p.pos}, kind: valueFloat,
			bits: math.Float64bits(value)}, nil
	}
	if value, matched, err := p.trySpecialFloat(start); err != nil {
		return nil, err
	} else if matched {
		return value, nil
	}
	// Integer: decimal, hex, octal, or binary.
	integer, err := p.parseIntToken(start)
	if err != nil {
		return nil, err
	}
	return &pValue{span: spanRange{start, p.pos}, kind: valueInteger, integer: integer}, nil
}

// trySpecialFloat parses `[+-]? inf|nan`.
func (p *parser) trySpecialFloat(start int) (*pValue, bool, error) {
	cursor := p.pos
	negative := false
	if cursor < len(p.src) && (p.src[cursor] == '+' || p.src[cursor] == '-') {
		negative = p.src[cursor] == '-'
		cursor++
	}
	if cursor+3 > len(p.src) {
		return nil, false, nil
	}
	switch p.src[cursor : cursor+3] {
	case "inf":
		p.pos = cursor + 3
		bits := math.Float64bits(math.Inf(1))
		if negative {
			bits = math.Float64bits(math.Inf(-1))
		}
		return &pValue{span: spanRange{start, p.pos}, kind: valueFloat, bits: bits}, true, nil
	case "nan":
		p.pos = cursor + 3
		bits := uint64(0x7ff8000000000000)
		if negative {
			bits = 0xfff8000000000000
		}
		return &pValue{span: spanRange{start, p.pos}, kind: valueFloat, bits: bits}, true, nil
	}
	return nil, false, nil
}

// tryDateTime attempts the RFC 3339 date-time grammar (toml_edit
// parser/datetime.rs). It returns the parsed datum, whether it matched,
// whether a cut failure occurred inside the date/time/offset part, and the
// error.
func (p *parser) tryDateTime(start int) (TomlDateTime, bool, bool, error) {
	if p.pos+10 > len(p.src) {
		return p.tryLocalTime(start)
	}
	year, err := strconv.Atoi(p.src[p.pos : p.pos+4])
	if err != nil {
		return p.tryLocalTime(start)
	}
	if p.src[p.pos+4] != '-' {
		return p.tryLocalTime(start)
	}
	month, err := strconv.Atoi(p.src[p.pos+5 : p.pos+7])
	if err != nil || month < 1 || month > 12 {
		return TomlDateTime{}, false, true, nil
	}
	if p.src[p.pos+7] != '-' {
		return TomlDateTime{}, false, true, nil
	}
	day, err := strconv.Atoi(p.src[p.pos+8 : p.pos+10])
	if err != nil || day < 1 || day > 31 || day > daysInMonth(year, month) {
		return TomlDateTime{}, false, true, nil
	}
	date := &TomlDate{Year: uint16(year), Month: uint8(month), Day: uint8(day)}
	p.pos += 10
	// Optional time after T/t/space.
	if p.pos < len(p.src) && (p.src[p.pos] == 'T' || p.src[p.pos] == 't' || p.src[p.pos] == ' ') {
		saved := p.pos
		p.pos++
		time, ok, cut := p.tryPartialTime()
		if !ok {
			if cut {
				return TomlDateTime{}, false, true, nil
			}
			// Clean backtrack: restore before the delimiter so the value
			// is the date alone (toml_edit opt semantics).
			p.pos = saved
			return TomlDateTime{Date: date}, true, false, nil
		}
		offset, cut, err := p.tryTimeOffset()
		if err != nil {
			return TomlDateTime{}, false, true, err
		}
		if cut {
			return TomlDateTime{}, false, true, nil
		}
		return TomlDateTime{Date: date, Time: &time, Offset: offset}, true, false, nil
	}
	return TomlDateTime{Date: date}, true, false, nil
}

func daysInMonth(year, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// tryLocalTime attempts a bare local time value.
func (p *parser) tryLocalTime(start int) (TomlDateTime, bool, bool, error) {
	time, ok, cut := p.tryPartialTime()
	if !ok {
		return TomlDateTime{}, false, cut, nil
	}
	return TomlDateTime{Time: &time}, true, false, nil
}

// tryPartialTime parses HH:MM:SS with an optional fraction; cut reports a
// hard failure after the first two digits and colon.
func (p *parser) tryPartialTime() (TomlTime, bool, bool) {
	if p.pos+2 > len(p.src) {
		return TomlTime{}, false, false
	}
	hourText := p.src[p.pos : p.pos+2]
	hour, err := strconv.Atoi(hourText)
	if err != nil || hour > 23 {
		return TomlTime{}, false, false
	}
	if p.pos+2 >= len(p.src) || p.src[p.pos+2] != ':' {
		return TomlTime{}, false, false
	}
	p.pos += 3
	if p.pos+2 > len(p.src) {
		return TomlTime{}, false, true
	}
	minuteText := p.src[p.pos : p.pos+2]
	minute, err := strconv.Atoi(minuteText)
	if err != nil || minute > 59 {
		return TomlTime{}, false, true
	}
	if p.pos+2 >= len(p.src) || p.src[p.pos+2] != ':' {
		return TomlTime{}, false, true
	}
	p.pos += 3
	if p.pos+2 > len(p.src) {
		return TomlTime{}, false, true
	}
	secondText := p.src[p.pos : p.pos+2]
	second, err := strconv.Atoi(secondText)
	if err != nil || second > 60 {
		return TomlTime{}, false, true
	}
	p.pos += 2
	nanosecond := uint32(0)
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		cursor := p.pos + 1
		digitsStart := cursor
		for cursor < len(p.src) && p.src[cursor] >= '0' && p.src[cursor] <= '9' {
			cursor++
		}
		if cursor == digitsStart {
			return TomlTime{}, false, true
		}
		fraction := p.src[digitsStart:cursor]
		if len(fraction) > 9 {
			fraction = fraction[:9]
		}
		value, err := strconv.ParseUint(fraction, 10, 32)
		if err != nil {
			return TomlTime{}, false, true
		}
		for index := len(fraction); index < 9; index++ {
			value *= 10
		}
		nanosecond = uint32(value)
		p.pos = cursor
	}
	return TomlTime{Hour: uint8(hour), Minute: uint8(minute), Second: uint8(second),
		Nanosecond: nanosecond}, true, false
}

// tryTimeOffset parses an optional `Z` or `±HH:MM` offset; cut reports a
// hard failure inside a numeric offset.
func (p *parser) tryTimeOffset() (*TomlOffset, bool, error) {
	if p.pos >= len(p.src) {
		return nil, false, nil
	}
	switch p.src[p.pos] {
	case 'Z', 'z':
		p.pos++
		return &TomlOffset{Z: true}, false, nil
	case '+', '-':
		sign := p.src[p.pos]
		if p.pos+6 > len(p.src) {
			return nil, true, nil
		}
		hour, err := strconv.Atoi(p.src[p.pos+1 : p.pos+3])
		if err != nil || hour > 23 {
			return nil, true, nil
		}
		if p.src[p.pos+3] != ':' {
			return nil, true, nil
		}
		minute, err := strconv.Atoi(p.src[p.pos+4 : p.pos+6])
		if err != nil || minute > 59 {
			return nil, true, nil
		}
		minutes := int16(hour*60 + minute)
		if sign == '-' {
			minutes = -minutes
		}
		p.pos += 6
		return &TomlOffset{Minutes: minutes}, false, nil
	}
	return nil, false, nil
}

// tryFloatToken attempts the float grammar and reports whether a complete
// token was consumed (toml_edit parser/numbers.rs float).
func (p *parser) tryFloatToken() (string, bool) {
	saved := p.pos
	if !p.tryDecIntPart() {
		p.pos = saved
		return "", false
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		if !p.tryZeroPrefixableInt() {
			p.pos = saved
			return "", false
		}
		return p.src[saved:p.pos], true
	}
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		if !p.tryZeroPrefixableInt() {
			p.pos = saved
			return "", false
		}
		if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
			p.pos++
			if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
				p.pos++
			}
			if !p.tryZeroPrefixableInt() {
				p.pos = saved
				return "", false
			}
		}
		return p.src[saved:p.pos], true
	}
	p.pos = saved
	return "", false
}

// tryDecIntPart matches `[+-]? (0 | [1-9][0-9_]* with underscore rules)`
// (toml_edit parser/numbers.rs dec-int: the single `0` token never
// extends).
func (p *parser) tryDecIntPart() bool {
	saved := p.pos
	if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
		p.pos++
	}
	if p.pos >= len(p.src) {
		p.pos = saved
		return false
	}
	c := p.src[p.pos]
	if c == '0' {
		p.pos++
		return true
	}
	if c < '1' || c > '9' {
		p.pos = saved
		return false
	}
	p.pos++
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c >= '0' && c <= '9' {
			p.pos++
			continue
		}
		if c == '_' {
			if p.pos+1 >= len(p.src) || p.src[p.pos+1] < '0' || p.src[p.pos+1] > '9' {
				p.pos = saved
				return false
			}
			p.pos += 2
			continue
		}
		break
	}
	return true
}

// tryZeroPrefixableInt matches `[0-9]([0-9]|_[0-9])*`.
func (p *parser) tryZeroPrefixableInt() bool {
	saved := p.pos
	if p.pos >= len(p.src) || p.src[p.pos] < '0' || p.src[p.pos] > '9' {
		return false
	}
	p.pos++
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c >= '0' && c <= '9' {
			p.pos++
			continue
		}
		if c == '_' {
			if p.pos+1 >= len(p.src) || p.src[p.pos+1] < '0' || p.src[p.pos+1] > '9' {
				p.pos = saved
				return false
			}
			p.pos += 2
			continue
		}
		break
	}
	return true
}

// parseIntToken parses a decimal, hex, octal, or binary integer
// (toml_edit parser/numbers.rs integer); the token's digits must fit i64.
func (p *parser) parseIntToken(start int) (int64, error) {
	saved := p.pos
	if p.pos+2 <= len(p.src) && p.src[p.pos] == '0' {
		var base int
		switch p.src[p.pos+1] {
		case 'x':
			base = 16
		case 'o':
			base = 8
		case 'b':
			base = 2
		default:
			base = 0
		}
		if base != 0 {
			p.pos += 2
			digitsStart := p.pos
			// The first digit after the prefix is mandatory; underscores
			// may only separate digits (toml_edit parser/numbers.rs
			// hex-int: `hexdig *( hexdig / underscore hexdig )`).
			if p.pos >= len(p.src) || !isDigitBase(p.src[p.pos], base) {
				p.pos = saved
				return 0, p.errorAt(p.pos, p.pos+1, "invalid integer")
			}
			p.pos++
			for p.pos < len(p.src) {
				c := p.src[p.pos]
				if isDigitBase(c, base) {
					p.pos++
					continue
				}
				if c == '_' {
					if p.pos+1 >= len(p.src) || !isDigitBase(p.src[p.pos+1], base) {
						p.pos = saved
						return 0, p.errorAt(p.pos, p.pos+1, "invalid integer")
					}
					p.pos += 2
					continue
				}
				break
			}
			text := strings.ReplaceAll(p.src[digitsStart:p.pos], "_", "")
			value, err := strconv.ParseInt(text, base, 64)
			if err != nil {
				p.pos = saved
				return 0, p.errorAt(p.pos, p.pos+1, "number too large to fit in target type")
			}
			return value, nil
		}
	}
	if !p.tryDecIntPart() {
		p.pos = saved
		return 0, p.errorAt(p.pos, p.pos+1, "invalid integer")
	}
	text := p.src[saved:p.pos]
	text = strings.ReplaceAll(text, "_", "")
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		p.pos = saved
		return 0, p.errorAt(p.pos, p.pos+1, "number too large to fit in target type")
	}
	return value, nil
}

func isDigitBase(c byte, base int) bool {
	switch base {
	case 16:
		return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
	case 8:
		return c >= '0' && c <= '7'
	case 2:
		return c == '0' || c == '1'
	}
	return c >= '0' && c <= '9'
}

// parseArray parses `[` values `]` with comments, newlines, and an
// optional trailing comma (toml_edit parser/array.rs).
func (p *parser) parseArray(start int) (*pValue, error) {
	p.pos++ // '['
	if err := p.wsCommentNewline(); err != nil {
		return nil, err
	}
	array := &pValue{span: spanRange{start, 0}, kind: valueArray}
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		array.span.end = p.pos
		return array, nil
	}
	for {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		array.array = append(array.array, value)
		if err := p.wsCommentNewline(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.src) || p.src[p.pos] != ',' {
			if p.pos >= len(p.src) || p.src[p.pos] != ']' {
				return nil, p.errorAt(p.pos, p.pos+1, "expected `,` or `]`")
			}
			p.pos++
			array.span.end = p.pos
			return array, nil
		}
		p.pos++
		if err := p.wsCommentNewline(); err != nil {
			return nil, err
		}
		if p.pos < len(p.src) && p.src[p.pos] == ']' {
			p.pos++
			array.span.end = p.pos
			return array, nil
		}
	}
}

// parseInlineTableValue parses `{` keyvals `}` (toml_edit
// parser/inline_table.rs): no trailing comma, no newlines outside values,
// dotted keys with the same duplicate semantics.
func (p *parser) parseInlineTableValue(start int) (*pValue, error) {
	p.pos++ // '{'
	p.skipWS()
	table := &pTable{byName: map[string]int{}, isInline: true}
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return &pValue{span: spanRange{start, p.pos}, kind: valueInlineTable, table: table}, nil
	}
	for {
		parts, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		if p.pos >= len(p.src) || p.src[p.pos] != '=' {
			return nil, p.errorAt(p.pos, p.pos+1, "expected `=`")
		}
		p.pos++
		p.skipWS()
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		leaf := parts[len(parts)-1]
		child, err := p.descendInlinePath(table, parts[:len(parts)-1])
		if err != nil {
			return nil, err
		}
		if child.dotted == (len(parts) == 1) {
			return nil, p.errorAt(leaf.span.start, leaf.span.end,
				"dotted key redefines a defined table")
		}
		if _, exists := child.byName[leaf.name]; exists {
			return nil, p.errorAt(leaf.span.start, leaf.span.end,
				"duplicate key `"+leaf.name+"`")
		}
		child.items = append(child.items, &pItem{
			name: leaf.name, keySpan: leaf.span, kind: itemKeyval, value: value,
		})
		child.byName[leaf.name] = len(child.items) - 1
		p.skipWS()
		if p.pos >= len(p.src) {
			return nil, p.errorAt(start, p.pos, "unterminated inline table")
		}
		if p.src[p.pos] == ',' {
			p.pos++
			p.skipWS()
			if p.pos >= len(p.src) || p.src[p.pos] == '}' {
				return nil, p.errorAt(p.pos, p.pos+1, "expected key after `,`")
			}
			continue
		}
		if p.src[p.pos] == '}' {
			p.pos++
			return &pValue{span: spanRange{start, p.pos}, kind: valueInlineTable, table: table}, nil
		}
		return nil, p.errorAt(p.pos, p.pos+1, "expected `,` or `}`")
	}
}

// descendInlinePath applies the inline-table dotted-key semantics
// (toml_edit parser/inline_table.rs descend_path).
func (p *parser) descendInlinePath(table *pTable, path []keyPart) (*pTable, error) {
	for _, part := range path {
		position, exists := table.byName[part.name]
		if !exists {
			child := &pTable{name: part.name, keySpan: part.span, byName: map[string]int{},
				implicit: true, dotted: true, isInline: true}
			table.items = append(table.items, &pItem{
				name: part.name, keySpan: part.span, kind: itemKeyval,
				value: &pValue{kind: valueInlineTable, table: child},
			})
			table.byName[part.name] = len(table.items) - 1
			table = child
			continue
		}
		item := table.items[position]
		if item.kind != itemKeyval || item.value.kind != valueInlineTable {
			return nil, p.errorAt(part.span.start, part.span.end,
				"dotted key `"+part.name+"` attempted to extend non-table type")
		}
		child := item.value.table
		if !child.implicit {
			return nil, p.errorAt(part.span.start, part.span.end,
				"dotted key `"+part.name+"` redefines a defined table")
		}
		table = child
	}
	return table, nil
}

// entityBuilder constructs the immutable entity list in the exact Rust
// order (consema-toml parser.rs) and enforces max_node_count and
// max_nesting_depth.
type entityBuilder struct {
	authority *document.DocumentAuthority
	sourceLen int
	limits    document.ParseLimits
	entities  []entity
}

func (b *entityBuilder) add(item entity) (int, *FormationFailure) {
	observed := len(b.entities) + 1
	if observed > b.limits.MaxNodeCount {
		return 0, resourceLimitFailure("node_count", observed, b.limits.MaxNodeCount)
	}
	index := len(b.entities)
	b.entities = append(b.entities, item)
	return index, nil
}

func (b *entityBuilder) checkDepth(depth int) *FormationFailure {
	if depth > b.limits.MaxNestingDepth {
		return resourceLimitFailure("nesting_depth", depth, b.limits.MaxNestingDepth)
	}
	return nil
}

func (b *entityBuilder) span(r spanRange) document.Span {
	if r.start < 0 {
		r.start = 0
	}
	if r.end > b.sourceLen {
		r.end = b.sourceLen
	}
	if r.start > r.end {
		r.start = r.end
	}
	span, err := b.authority.Span(r.start, r.end)
	if err != nil {
		// The parser produces ordered in-bounds ranges.
		panic("toml: entity span out of bounds")
	}
	return span
}

func (b *entityBuilder) addItem(r spanRange, item itemEntity) (int, *FormationFailure) {
	return b.add(entity{span: b.span(r), kind: entityItem, item: item})
}

func (b *entityBuilder) reserveItem(r spanRange) (int, *FormationFailure) {
	return b.addItem(r, itemEntity{kind: itemArray})
}

func (b *entityBuilder) replaceItem(index int, item itemEntity) {
	b.entities[index].item = item
}

// buildTable mirrors the Rust build_table: the root item first, then per
// item in insertion order a key entity, the child item, and the entry
// entity.
func (b *entityBuilder) buildTable(table *pTable, root bool, depth int,
	fallback spanRange) (int, *FormationFailure) {
	if failure := b.checkDepth(depth); failure != nil {
		return 0, failure
	}
	tableRange := fallback
	if root {
		tableRange = spanRange{0, b.sourceLen}
	} else if table.hasSpan {
		tableRange = table.span
	}
	itemIndex, failure := b.reserveItem(tableRange)
	if failure != nil {
		return 0, failure
	}
	entries := make([]int, 0, len(table.items))
	for ordinal, item := range table.items {
		keyRange := item.keySpan
		keyIndex, failure := b.add(entity{
			span: b.span(keyRange),
			kind: entityKey,
			key:  keyEntity{name: item.name},
		})
		if failure != nil {
			return 0, failure
		}
		var childIndex int
		switch item.kind {
		case itemKeyval:
			childIndex, failure = b.buildValue(item.value, depth+1, keyRange)
		case itemSubtable:
			childIndex, failure = b.buildTable(item.table, false, depth+1, keyRange)
		case itemAOT:
			childIndex, failure = b.buildAOT(item.aot, depth+1, keyRange)
		}
		if failure != nil {
			return 0, failure
		}
		childSpan := b.entities[childIndex].span
		entryRange := spanRange{
			start: minInt(keyRange.start, childSpan.StartByte()),
			end:   maxInt(keyRange.end, childSpan.EndByte()),
		}
		entryIndex, failure := b.add(entity{
			span: b.span(entryRange),
			kind: entityEntry,
			entry: entryEntity{
				ordinal: ordinal,
				key:     keyIndex,
				item:    childIndex,
			},
		})
		if failure != nil {
			return 0, failure
		}
		entries = append(entries, entryIndex)
	}
	flavor := flavorStandard
	switch {
	case root:
		flavor = flavorRoot
	case table.dotted:
		flavor = flavorDotted
	case table.implicit:
		flavor = flavorImplicit
	}
	b.replaceItem(itemIndex, itemEntity{kind: itemTable, entries: entries, flavor: flavor})
	return itemIndex, nil
}

// buildInlineTable mirrors the Rust build_inline_table for inline-table
// values.
func (b *entityBuilder) buildInlineTable(table *pTable, depth int,
	r spanRange) (int, *FormationFailure) {
	if failure := b.checkDepth(depth); failure != nil {
		return 0, failure
	}
	itemIndex, failure := b.reserveItem(r)
	if failure != nil {
		return 0, failure
	}
	entries := make([]int, 0, len(table.items))
	for ordinal, item := range table.items {
		if item.kind != itemKeyval {
			return 0, newFormationFailure("toml.parse.syntax@1", protocol.CategorySyntax,
				item.keySpan.start, item.keySpan.end,
				map[string]string{"parser_reason": "invalid inline table structure"})
		}
		keyRange := item.keySpan
		keyIndex, failure := b.add(entity{
			span: b.span(keyRange),
			kind: entityKey,
			key:  keyEntity{name: item.name},
		})
		if failure != nil {
			return 0, failure
		}
		childIndex, failure := b.buildValue(item.value, depth+1, keyRange)
		if failure != nil {
			return 0, failure
		}
		childSpan := b.entities[childIndex].span
		entryRange := spanRange{
			start: minInt(keyRange.start, childSpan.StartByte()),
			end:   maxInt(keyRange.end, childSpan.EndByte()),
		}
		entryIndex, failure := b.add(entity{
			span: b.span(entryRange),
			kind: entityEntry,
			entry: entryEntity{
				ordinal: ordinal,
				key:     keyIndex,
				item:    childIndex,
			},
		})
		if failure != nil {
			return 0, failure
		}
		entries = append(entries, entryIndex)
	}
	b.replaceItem(itemIndex, itemEntity{kind: itemInlineTable, entries: entries})
	return itemIndex, nil
}

func (b *entityBuilder) buildValue(value *pValue, depth int,
	fallback spanRange) (int, *FormationFailure) {
	if failure := b.checkDepth(depth); failure != nil {
		return 0, failure
	}
	r := value.span
	if r.end == 0 {
		r = fallback
	}
	switch value.kind {
	case valueArray:
		return b.buildArray(value, depth, r)
	case valueInlineTable:
		return b.buildInlineTable(value.table, depth, r)
	case valueString:
		return b.addItem(r, itemEntity{kind: itemString, str: value.str})
	case valueInteger:
		return b.addItem(r, itemEntity{kind: itemInteger, integer: value.integer})
	case valueFloat:
		return b.addItem(r, itemEntity{kind: itemFloat, bits: value.bits})
	case valueBoolean:
		return b.addItem(r, itemEntity{kind: itemBoolean, boolean: value.boolean})
	case valueDateTime:
		return b.addItem(r, itemEntity{kind: itemDateTime, dateTime: value.dateTime})
	}
	return 0, newFormationFailure("toml.parse.syntax@1", protocol.CategorySyntax,
		r.start, r.end, map[string]string{"parser_reason": "unknown value kind"})
}

func (b *entityBuilder) buildArray(value *pValue, depth int,
	r spanRange) (int, *FormationFailure) {
	if failure := b.checkDepth(depth); failure != nil {
		return 0, failure
	}
	itemIndex, failure := b.reserveItem(r)
	if failure != nil {
		return 0, failure
	}
	elements := make([]int, 0, len(value.array))
	for ordinal, element := range value.array {
		valueRange := element.span
		childIndex, failure := b.buildValue(element, depth+1, valueRange)
		if failure != nil {
			return 0, failure
		}
		elementIndex, failure := b.add(entity{
			span: b.span(valueRange),
			kind: entityElement,
			element: elementEntity{
				ordinal: ordinal,
				item:    childIndex,
			},
		})
		if failure != nil {
			return 0, failure
		}
		elements = append(elements, elementIndex)
	}
	b.replaceItem(itemIndex, itemEntity{kind: itemArray, elements: elements})
	return itemIndex, nil
}

func (b *entityBuilder) buildAOT(array *pAOT, depth int,
	fallback spanRange) (int, *FormationFailure) {
	if failure := b.checkDepth(depth); failure != nil {
		return 0, failure
	}
	r := fallback
	if array.hasSpan {
		r = array.span
	}
	itemIndex, failure := b.reserveItem(r)
	if failure != nil {
		return 0, failure
	}
	elements := make([]int, 0, len(array.elements))
	for ordinal, table := range array.elements {
		childIndex, failure := b.buildTable(table, false, depth+1, table.span)
		if failure != nil {
			return 0, failure
		}
		childSpan := b.entities[childIndex].span
		elementIndex, failure := b.add(entity{
			span: childSpan,
			kind: entityElement,
			element: elementEntity{
				ordinal: ordinal,
				item:    childIndex,
			},
		})
		if failure != nil {
			return 0, failure
		}
		elements = append(elements, elementIndex)
	}
	b.replaceItem(itemIndex, itemEntity{kind: itemArrayOfTables, elements: elements})
	return itemIndex, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
