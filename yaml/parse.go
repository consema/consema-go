package yaml

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// Parse forms one exact YAML stream snapshot (consema-yaml lib.rs:259-320).
// The source contract is explicit: UTF-8 with or without BOM, or
// BOM-detected UTF-16LE/UTF-16BE; no BOM means UTF-8. The profile is
// selected explicitly and never guessed. Syntax, directive, tag, scalar,
// alias, and limit failures return a FormationFailure with a registered
// diagnostic code; no partial Document is ever returned.
func Parse(source []byte, profile YamlProfile,
	limits document.ParseLimits) (*Document, *FormationFailure) {
	if len(source) > limits.MaxSourceBytes {
		return nil, resourceLimitFailure("source-bytes", len(source), limits.MaxSourceBytes)
	}
	snapshot, err := document.NewSourceSnapshotFromRaw(source,
		document.NewEncodingRequest(document.Utf8Encoding()),
		document.SourceLimits{
			MaxRawBytes:         limits.MaxSourceBytes,
			MaxDecodedUTF8Bytes: document.DefaultSourceLimits().MaxDecodedUTF8Bytes,
			MaxDecodedScalars:   document.DefaultSourceLimits().MaxDecodedScalars,
		})
	if err != nil {
		return nil, sourceFailure(err)
	}
	text, ok := snapshot.DecodedText()
	if !ok {
		return nil, newNativeFailure("yaml.native.invalid-source-span@1")
	}
	if failure := validateVersionDirectives(text, profile); failure != nil {
		return nil, failure
	}
	parser := newParser(text, profile, limits)
	parser.parseStream()
	if parser.failed != nil {
		return nil, parser.failed
	}
	// The lossless syntax tokenizer runs after parsing, so parse failures
	// win; its own piece limit is enforced against max_token_count.
	authority := document.NewDocumentAuthority()
	tokenized, failure := tokenize(snapshot, authority, limits)
	if failure != nil {
		return nil, failure
	}
	stream := nativeStream{
		nodes:     parser.nodes,
		documents: parser.documents,
		aliases:   parser.aliases,
	}
	converter := &spanConverter{
		resolver:  &rawByteResolver{source: snapshot},
		authority: authority,
	}
	if failure := converter.convert(&stream); failure != nil {
		return nil, failure
	}
	// The tokenizer partitions every raw source byte exactly once; a
	// coverage violation is an internal invariant failure.
	index, err := NewLosslessStructuralIndex(authority.Identity(), snapshot.Len(), tokenized.pieces)
	if err != nil {
		return nil, newNativeFailure("yaml.native.invalid-source-span@1")
	}
	return &Document{
		authority: authority,
		source:    snapshot,
		profile:   profile,
		index:     index,
		kinds:     tokenized.kinds,
		native:    stream,
		documents: len(stream.documents),
		limits:    limits,
	}, nil
}

// sourceFailure maps one source snapshot construction failure onto the
// frozen YAML formation codes.
func sourceFailure(err error) *FormationFailure {
	if sourceError, ok := err.(*document.SourceError); ok {
		switch sourceError.Kind {
		case document.SourceErrorInvalidSequence:
			return newFormationFailure("core.source.invalid-sequence@1",
				protocol.CategoryLexical, sourceError.ByteOffset, sourceError.ByteOffset, nil)
		case document.SourceErrorEncodingConflict:
			return newFormationFailure("core.source.encoding-conflict@1",
				protocol.CategoryEncoding, -1, -1, nil)
		case document.SourceErrorUnsupportedBom:
			return newFormationFailure("core.source.unsupported-bom@1",
				protocol.CategoryEncoding, -1, -1, nil)
		case document.SourceErrorResourceLimit:
			return resourceLimitFailure(sourceError.Name, sourceError.Observed, sourceError.Limit)
		}
	}
	return newNativeFailure("yaml.native.invalid-source-span@1")
}

// spanConverter converts every recorded rune-index span into an exact
// raw-byte span.
type spanConverter struct {
	resolver  *rawByteResolver
	authority document.DocumentAuthority
}

func (c *spanConverter) convert(stream *nativeStream) *FormationFailure {
	for index := range stream.nodes {
		node := &stream.nodes[index]
		span, err := c.span(node.start, node.end)
		if err != nil {
			return newNativeFailure("yaml.native.invalid-source-span@1")
		}
		node.span = span
		if node.hasAnchor {
			anchorSpan, err := c.span(node.anchorStart, node.anchorEnd)
			if err != nil {
				return newNativeFailure("yaml.native.invalid-source-span@1")
			}
			node.anchorSpan = anchorSpan
		}
		switch node.content.kind {
		case contentSequence:
			for item := range node.content.items {
				item := &node.content.items[item]
				span, err := c.span(item.start, item.end)
				if err != nil {
					return newNativeFailure("yaml.native.invalid-source-span@1")
				}
				item.span = span
			}
		case contentMapping:
			for entry := range node.content.entries {
				entry := &node.content.entries[entry]
				span, err := c.span(entry.start, entry.end)
				if err != nil {
					return newNativeFailure("yaml.native.invalid-source-span@1")
				}
				entry.span = span
			}
		}
	}
	for index := range stream.aliases {
		alias := &stream.aliases[index]
		span, err := c.span(alias.start, alias.end)
		if err != nil {
			return newNativeFailure("yaml.native.invalid-source-span@1")
		}
		alias.span = span
	}
	for index := range stream.documents {
		doc := &stream.documents[index]
		span, err := c.span(doc.startRune, doc.endRune)
		if err != nil {
			return newNativeFailure("yaml.native.invalid-source-span@1")
		}
		doc.span = span
	}
	return nil
}

func (c *spanConverter) span(start, end int) (document.Span, error) {
	rawStart, err := c.resolver.resolve(start)
	if err != nil {
		return document.Span{}, err
	}
	rawEnd, err := c.resolver.resolve(end)
	if err != nil {
		return document.Span{}, err
	}
	return c.authority.Span(rawStart, rawEnd)
}
