package plist

// This file implements the cross-representation conversion internals
// (consema-plist document.rs convert_xml_to_binary / convert_binary_to_xml;
// RFC 0013 §7). Both directions serialize the reachable native value graph
// under the target profile, reparse the exact emitted bytes, and verify
// native-model equality; the binary direction additionally validates the
// XML expressibility boundary first (hard gate 3) and computes the
// reachable-graph post-order ranks the report uses.

import (
	"math"
	"strings"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// convertXMLToBinary converts one `plist.xml@1` document to
// `plist.binary@1` (RFC 0013 §7). Every arena node of an XML-sourced
// document is reachable and expressible in binary; the target object table
// writes one string object per dictionary entry key, in entry order,
// immediately before its dictionary, and every other object exactly once in
// source arena order.
func convertXMLToBinary(source *Document, limits PlistParseLimits) (*ConvertedDocument, *ConversionFailure) {
	native := source.native
	nodeCount := native.NodeCount()
	if nodeCount > limits.MaxConversionNodes {
		return nil, conversionLimit("conversion-nodes", nodeCount, limits.MaxConversionNodes)
	}
	totalKeys := arenaKeyCount(native)
	targetObjectCount := nodeCount + totalKeys
	if targetObjectCount > limits.MaxObjectCount {
		return nil, conversionLimit("object-count", targetObjectCount, limits.MaxObjectCount)
	}
	eventCount := 1 + nodeCount
	if eventCount > limits.MaxReportEvents {
		return nil, conversionLimit("report-events", eventCount, limits.MaxReportEvents)
	}
	bytes, failure := serializeBinary(native)
	if failure != nil {
		return nil, failure
	}
	formed, formationFailure := parseBinary(bytes, limits)
	if formationFailure != nil {
		return nil, &ConversionFailure{diagnostics: formationFailure.Diagnostics}
	}
	if formed.status != document.FormationStatusComplete ||
		!formed.native.Equal(native) {
		return nil, reparseFailure()
	}
	// Mapping: node `i` lands after the key objects of every earlier
	// dictionary and its own.
	keysBefore := 0
	events := make([]ConversionReportEvent, 0, eventCount)
	events = append(events, ConversionReportEvent{Kind: ConversionEventKindRepresentationChange})
	for index := 0; index < nodeCount; index++ {
		dictKeys := 0
		if value, ok := native.Get(PlistValueRef{index: index}); ok {
			if dict, isDict := value.AsDict(); isDict {
				dictKeys = dict.Len()
			}
		}
		reference := PlistValueRef{index: index}
		target := index + keysBefore + dictKeys
		events = append(events, ConversionReportEvent{
			Kind: ConversionEventKindValueMapped, Source: &reference, Target: &target})
		keysBefore += dictKeys
	}
	return &ConvertedDocument{
		document: formed,
		report:   &ConversionReport{events: events},
	}, nil
}

// convertBinaryToXML converts one `plist.binary@1` document to
// `plist.xml@1` (RFC 0013 §7). The reachable value graph is validated for
// XML expressibility first; any binary-only fact fails the whole conversion
// atomically. The XML parser assigns target arena ordinals in close-tag
// (post) order, so the report maps each source node to its post-order rank.
func convertBinaryToXML(source *Document, limits PlistParseLimits) (*ConvertedDocument, *ConversionFailure) {
	native := source.native
	graph, failure := analyze(native, limits)
	if failure != nil {
		return nil, failure
	}
	reachableCount := len(graph.reachable)
	eventCount := 1 + reachableCount
	if eventCount > limits.MaxReportEvents {
		return nil, conversionLimit("report-events", eventCount, limits.MaxReportEvents)
	}
	bytes, failure := serializeXML(native, graph)
	if failure != nil {
		return nil, failure
	}
	formed, formationFailure := parseXML(bytes, limits)
	if formationFailure != nil {
		return nil, &ConversionFailure{diagnostics: formationFailure.Diagnostics}
	}
	if formed.status != document.FormationStatusComplete ||
		!formed.native.Equal(native) {
		return nil, reparseFailure()
	}
	events := make([]ConversionReportEvent, 0, eventCount)
	events = append(events, ConversionReportEvent{Kind: ConversionEventKindRepresentationChange})
	for _, index := range graph.reachable {
		reference := PlistValueRef{index: index}
		target := graph.ranks[index]
		events = append(events, ConversionReportEvent{
			Kind: ConversionEventKindValueMapped, Source: &reference, Target: &target})
	}
	return &ConvertedDocument{
		document: formed,
		report:   &ConversionReport{events: events},
	}, nil
}

// reachableGraph is the reachable-graph facts of one native document (RFC
// 0013 §7).
type reachableGraph struct {
	// children are the ordered value children of every arena node.
	children [][]PlistValueRef
	// ranks are the post-order rank of every reachable node, indexed by
	// source arena ordinal.
	ranks []int
	// reachable are the source ordinals of the reachable nodes in source
	// arena order.
	reachable []int
}

// analyze validates one native document against the XML expressibility
// boundary (RFC 0013 §7, hard gate 3) and computes the reachable-graph
// facts. The traversal is iterative, counts every incoming reference of
// every reachable node (so shared identity is detected exactly), and
// reports one `plist.conversion.inexpressible@1` diagnostic per violating
// node, in source arena order.
func analyze(native *PlistDocument, limits PlistParseLimits) (*reachableGraph, *ConversionFailure) {
	nodeCount := native.NodeCount()
	if nodeCount > limits.MaxConversionNodes {
		return nil, conversionLimit("conversion-nodes", nodeCount, limits.MaxConversionNodes)
	}
	children := make([][]PlistValueRef, nodeCount)
	for index := 0; index < nodeCount; index++ {
		children[index] = childrenOf(native, PlistValueRef{index: index})
	}
	visited := make([]bool, nodeCount)
	indegree := make([]int, nodeCount)
	postorder := make([]int, 0, nodeCount)
	root := native.Root()
	visited[root.index] = true
	stack := []struct {
		node      int
		nextChild int
	}{{root.index, 0}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		nodeChildren := children[top.node]
		if top.nextChild < len(nodeChildren) {
			child := nodeChildren[top.nextChild]
			top.nextChild++
			indegree[child.index]++
			if !visited[child.index] {
				visited[child.index] = true
				stack = append(stack, struct {
					node      int
					nextChild int
				}{child.index, 0})
			}
		} else {
			postorder = append(postorder, top.node)
			stack = stack[:len(stack)-1]
		}
	}
	ranks := make([]int, nodeCount)
	for index := range ranks {
		ranks[index] = -1
	}
	for rank, index := range postorder {
		ranks[index] = rank
	}
	type violation struct {
		node int
		fact string
	}
	var violations []violation
	for index := 0; index < nodeCount; index++ {
		if !visited[index] {
			continue
		}
		if indegree[index] > 1 {
			violations = append(violations, violation{index, "shared-identity"})
		}
		value, _ := native.Get(PlistValueRef{index: index})
		switch value.kind {
		case PlistValueKindUid:
			violations = append(violations, violation{index, "uid"})
		case PlistValueKindReal:
			if value.real.width == RealWidthFloat32 {
				violations = append(violations, violation{index, "float32-width"})
			} else if !realExpressible(value.real) {
				violations = append(violations, violation{index, "real-nan-payload"})
			}
		case PlistValueKindString:
			if value.str.status == PlistStringUnpairedSurrogate {
				violations = append(violations, violation{index, "unpaired-surrogate"})
			} else if !isXMLText(value.str.CodeUnits()) {
				violations = append(violations, violation{index, "non-xml-character"})
			}
		case PlistValueKindDate:
			_, _, _, _, _, _, dateError := wholeSecondDate(value.date.seconds)
			switch dateError {
			case dateRangeFractionalSeconds:
				violations = append(violations, violation{index, "fractional-seconds"})
			case dateRangeYearOutOfRange:
				violations = append(violations, violation{index, "date-year-range"})
			}
		case PlistValueKindDict:
			for _, entry := range value.dict.entries {
				key := entry.key
				if key.Status() == PlistStringUnpairedSurrogate {
					violations = append(violations, violation{index, "unpaired-surrogate"})
				} else if !isXMLText(key.CodeUnits()) {
					violations = append(violations, violation{index, "non-xml-character"})
				}
			}
		}
	}
	if len(violations) > 0 {
		count := len(violations)
		if count > limits.Common.MaxDiagnostics {
			count = limits.Common.MaxDiagnostics
		}
		diagnostics := make([]*protocol.Diagnostic, 0, count)
		for _, item := range violations[:count] {
			diagnostics = append(diagnostics, newDiagnostic("plist.conversion.inexpressible@1",
				protocol.CategoryConversion, protocol.SeverityError, nil,
				map[string]string{"fact": item.fact, "node": itoa(item.node)}, 0))
		}
		return nil, &ConversionFailure{diagnostics: diagnostics}
	}
	reachable := make([]int, 0, nodeCount)
	for index := 0; index < nodeCount; index++ {
		if visited[index] {
			reachable = append(reachable, index)
		}
	}
	return &reachableGraph{children: children, ranks: ranks, reachable: reachable}, nil
}

// childrenOf returns the ordered direct value children of one node (RFC
// 0013 §6).
func childrenOf(native *PlistDocument, node PlistValueRef) []PlistValueRef {
	value, ok := native.Get(node)
	if !ok {
		return nil
	}
	switch value.kind {
	case PlistValueKindDict:
		refs := make([]PlistValueRef, 0, len(value.dict.entries))
		for _, entry := range value.dict.entries {
			refs = append(refs, entry.value)
		}
		return refs
	case PlistValueKindArray:
		return append([]PlistValueRef(nil), value.array.elements...)
	}
	return nil
}

// arenaKeyCount returns the number of dictionary entry keys of the whole
// arena (one binary string object per key).
func arenaKeyCount(native *PlistDocument) int {
	total := 0
	for index := 0; index < native.NodeCount(); index++ {
		if value, ok := native.Get(PlistValueRef{index: index}); ok {
			if dict, isDict := value.AsDict(); isDict {
				total += dict.Len()
			}
		}
	}
	return total
}

// serializeXML serializes one native value graph as a `plist.xml@1` source
// (RFC 0013 §4, §7). The caller guarantees expressibility; the emitted
// bytes reparse Complete with native-model equality. The document uses the
// Apple header spelling, four-space indentation, LF line endings, and a
// trailing newline; the root value element is written at depth 0.
func serializeXML(native *PlistDocument, graph *reachableGraph) ([]byte, *ConversionFailure) {
	var out strings.Builder
	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	out.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	out.WriteString("<plist version=\"1.0\">\n")
	root := native.Root()
	rootValue, _ := native.Get(root)
	if rootValue.kind == PlistValueKindDict || rootValue.kind == PlistValueKindArray {
		type frame struct {
			node      int
			depth     int
			nextChild int
		}
		frames := []frame{{root.index, 0, 0}}
		for len(frames) > 0 {
			top := &frames[len(frames)-1]
			value, _ := native.Get(PlistValueRef{index: top.node})
			children := graph.children[top.node]
			if top.nextChild == 0 {
				writeIndent(&out, top.depth)
				switch value.kind {
				case PlistValueKindDict:
					if len(children) == 0 {
						out.WriteString("<dict></dict>\n")
						frames = frames[:len(frames)-1]
						continue
					}
					out.WriteString("<dict>\n")
				case PlistValueKindArray:
					if len(children) == 0 {
						out.WriteString("<array></array>\n")
						frames = frames[:len(frames)-1]
						continue
					}
					out.WriteString("<array>\n")
				default:
					return nil, internalConversionFailure()
				}
			}
			if top.nextChild < len(children) {
				child := children[top.nextChild]
				top.nextChild++
				if value.kind == PlistValueKindDict {
					keyText, err := value.dict.entries[top.nextChild-1].key.ToUnicode()
					if err != nil {
						return nil, internalConversionFailure()
					}
					writeIndent(&out, top.depth+1)
					out.WriteString("<key>")
					out.WriteString(escapeXMLText(keyText))
					out.WriteString("</key>\n")
				}
				childValue, _ := native.Get(child)
				if childValue.kind == PlistValueKindDict || childValue.kind == PlistValueKindArray {
					frames = append(frames, frame{child.index, top.depth + 1, 0})
				} else {
					if failure := emitScalarXML(&out, native, child, top.depth+1); failure != nil {
						return nil, failure
					}
				}
			} else {
				writeIndent(&out, top.depth)
				switch value.kind {
				case PlistValueKindDict:
					out.WriteString("</dict>\n")
				case PlistValueKindArray:
					out.WriteString("</array>\n")
				default:
					return nil, internalConversionFailure()
				}
				frames = frames[:len(frames)-1]
			}
		}
	} else {
		if failure := emitScalarXML(&out, native, root, 0); failure != nil {
			return nil, failure
		}
	}
	out.WriteString("</plist>\n")
	return []byte(out.String()), nil
}

// emitScalarXML emits one scalar value element at the given depth.
func emitScalarXML(out *strings.Builder, native *PlistDocument, node PlistValueRef,
	depth int) *ConversionFailure {
	writeIndent(out, depth)
	value, _ := native.Get(node)
	switch value.kind {
	case PlistValueKindString:
		text, err := value.str.ToUnicode()
		if err != nil {
			return internalConversionFailure()
		}
		out.WriteString("<string>")
		out.WriteString(escapeXMLText(text))
		out.WriteString("</string>\n")
	case PlistValueKindInteger:
		out.WriteString("<integer>")
		out.WriteString(itoa64(value.integer.value))
		out.WriteString("</integer>\n")
	case PlistValueKindReal:
		out.WriteString("<real>")
		out.WriteString(renderReal(value.real))
		out.WriteString("</real>\n")
	case PlistValueKindBoolean:
		if value.boolean.value {
			out.WriteString("<true/>\n")
		} else {
			out.WriteString("<false/>\n")
		}
	case PlistValueKindDate:
		out.WriteString("<date>")
		year, month, day, hour, minute, second, dateError := wholeSecondDate(value.date.seconds)
		if dateError != 0 {
			return internalConversionFailure()
		}
		_ = hour
		_ = minute
		_ = second
		_ = year
		_ = month
		_ = day
		out.WriteString(renderDate(year, month, day, hour, minute, second))
		out.WriteString("</date>\n")
	case PlistValueKindData:
		out.WriteString("<data>")
		out.WriteString(encodeBase64(value.data.Bytes()))
		out.WriteString("</data>\n")
	default:
		return internalConversionFailure()
	}
	return nil
}

// serializeBinary serializes one native value graph as a `plist.binary@1`
// source (RFC 0013 §5, §7). Objects are written once each in source arena
// order — dictionary keys as fresh string objects immediately before their
// dictionary — with minimal integer widths (negatives always 8 bytes),
// Float32 width preserved, UID minimal widths, and offset/ref sizes chosen
// minimally to satisfy the trailer sufficiency checks.
func serializeBinary(native *PlistDocument) ([]byte, *ConversionFailure) {
	nodeCount := native.NodeCount()
	targetIndex := make([]int, nodeCount)
	keysBefore := 0
	for index := 0; index < nodeCount; index++ {
		dictKeys := 0
		if value, ok := native.Get(PlistValueRef{index: index}); ok {
			if dict, isDict := value.AsDict(); isDict {
				dictKeys = dict.Len()
			}
		}
		targetIndex[index] = index + keysBefore + dictKeys
		keysBefore += dictKeys
	}
	targetObjectCount := nodeCount + keysBefore
	refSize := refSizeFor(targetObjectCount)
	out := append([]byte(nil), binaryHeader...)
	offsets := make([]int, 0, targetObjectCount)
	for index := 0; index < nodeCount; index++ {
		value, _ := native.Get(PlistValueRef{index: index})
		if value.kind == PlistValueKindDict {
			for _, entry := range value.dict.entries {
				offsets = append(offsets, len(out))
				out = writeStringObject(out, entry.key.String())
			}
		}
		offsets = append(offsets, len(out))
		out = writeBinaryObject(out, index, value, refSize, targetIndex)
	}
	offsetTableOffset := len(out)
	offsetIntSize := refSizeFor(offsetTableOffset)
	for _, offset := range offsets {
		out = writeBE(out, uint64(offset), offsetIntSize)
	}
	out = append(out, 0, 0, 0, 0, 0)
	out = append(out, 0) // sortVersion
	out = append(out, byte(offsetIntSize))
	out = append(out, byte(refSize))
	out = writeBE(out, uint64(targetObjectCount), 8)
	out = writeBE(out, uint64(targetIndex[native.Root().index]), 8)
	out = writeBE(out, uint64(offsetTableOffset), 8)
	return out, nil
}

// writeBinaryObject writes one object: marker, size, payload, and
// references (RFC 0013 §5).
func writeBinaryObject(out []byte, sourceIndex int, value PlistValue, refSize int,
	targetIndex []int) []byte {
	switch value.kind {
	case PlistValueKindDict:
		count := value.dict.Len()
		out = writeSizedMarker(out, 0xD0, count)
		keyStart := targetIndex[sourceIndex] - count
		for position := 0; position < count; position++ {
			out = writeBE(out, uint64(keyStart+position), refSize)
		}
		for _, entry := range value.dict.entries {
			out = writeBE(out, uint64(targetIndex[entry.value.index]), refSize)
		}
	case PlistValueKindArray:
		count := value.array.Len()
		out = writeSizedMarker(out, 0xA0, count)
		for _, element := range value.array.elements {
			out = writeBE(out, uint64(targetIndex[element.index]), refSize)
		}
	case PlistValueKindString:
		out = writeStringObject(out, value.str)
	case PlistValueKindInteger:
		integer := value.integer.value
		width := integerWidth(integer)
		out = append(out, 0x10|byte(log2Width(width)))
		// The two's-complement bit pattern of the signed value, written at
		// exactly `width` bytes (RFC 0013 §5.3).
		out = writeBE(out, uint64(integer), width)
	case PlistValueKindReal:
		switch value.real.width {
		case RealWidthFloat64:
			out = append(out, 0x23)
			out = writeBE(out, value.real.bits, 8)
		case RealWidthFloat32:
			out = append(out, 0x22)
			out = writeBE(out, value.real.bits, 4)
		}
	case PlistValueKindBoolean:
		if value.boolean.value {
			out = append(out, 0x09)
		} else {
			out = append(out, 0x08)
		}
	case PlistValueKindDate:
		out = append(out, 0x33)
		out = writeBE(out, math.Float64bits(value.date.seconds), 8)
	case PlistValueKindData:
		bytes := value.data.Bytes()
		out = writeSizedMarker(out, 0x40, len(bytes))
		out = append(out, bytes...)
	case PlistValueKindUid:
		width := uidWidth(uint64(value.uid.value))
		out = append(out, 0x80|byte(width-1))
		out = writeBE(out, uint64(value.uid.value), width)
	}
	return out
}

// writeStringObject writes one string object: the ASCII marker when every
// code unit is below `0x80`, else the UTF-16BE marker (RFC 0013 §5.6).
func writeStringObject(out []byte, string PlistString) []byte {
	units := string.CodeUnits()
	allASCII := true
	for _, unit := range units {
		if unit >= 0x80 {
			allASCII = false
			break
		}
	}
	if allASCII {
		out = writeSizedMarker(out, 0x50, len(units))
		for _, unit := range units {
			out = append(out, byte(unit))
		}
	} else {
		out = writeSizedMarker(out, 0x60, len(units))
		for _, unit := range units {
			out = append(out, byte(unit>>8), byte(unit))
		}
	}
	return out
}

// log2Width maps one minimal marker width to its low-nibble exponent
// (1 -> 0, 2 -> 1, 4 -> 2, 8 -> 3).
func log2Width(width int) int {
	switch width {
	case 2:
		return 1
	case 4:
		return 2
	case 8:
		return 3
	}
	return 0
}

// itoa64 formats one signed 64-bit ordinal.
func itoa64(value int64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	negative := value < 0
	magnitude := value
	if negative {
		magnitude = -magnitude
	}
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
