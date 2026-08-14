package protocol

import (
	"strings"
)

// This file implements the strict canonical-JSON record codec of
// core.batch-plan@1 (and its nested source-patch records) on the parse
// tree. The tree level is the only place the Go implementation can carry
// the Bytes leaves of patch replacements (doc.go); the record decoders
// enforce the same exact-field and cross constraints as the value-level
// codec, and both feed the same native constructors.

// encodeJSONTree encodes a complete transport envelope tree as canonical
// bytes.
func encodeJSONTree(node *jsonNode, limits ProtocolLimits) ([]byte, error) {
	var builder strings.Builder
	if err := encodeTransportNode(&builder, node, limits); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// planMessageNode builds the tagged value tree of one plan manifest (the
// transport envelope is added by the encoder).
func planMessageNode(message *BatchPlanMessage) (*jsonNode, error) {
	files := make([]*jsonNode, 0, len(message.files))
	for index, entry := range message.files {
		node, err := planEntryNode(entry, index)
		if err != nil {
			return nil, err
		}
		files = append(files, node)
	}
	return taggedObject([]jsonField{
		{key: "schema", value: taggedString("core.batch-plan@1")},
		{key: "product_version", value: taggedString(message.productVersion)},
		{key: "command", value: taggedString("plan")},
		{key: "files", value: taggedArray(files)},
	}), nil
}

// planEntryNode builds one plan entry as a tagged object.
func planEntryNode(entry *BatchPlanFileEntry, index int) (*jsonNode, error) {
	var profile *jsonNode = taggedNull()
	if entry.profile != nil {
		profile = taggedObject([]jsonField{
			{key: "id", value: taggedString(entry.profile.id)},
			{key: "version", value: taggedInteger(uint64(entry.profile.version))},
		})
	}
	var sourceDigest *jsonNode = taggedNull()
	if entry.sourceDigest != nil {
		sourceDigest = digestNode(*entry.sourceDigest)
	}
	var operations *jsonNode = taggedNull()
	if entry.operations != nil {
		items := make([]*jsonNode, 0, len(entry.operations))
		for _, operation := range entry.operations {
			summaryFields := make([]jsonField, 0, len(operation.Summary))
			for _, name := range sortedStringKeys(operation.Summary) {
				summaryFields = append(summaryFields, jsonField{
					key: name, value: taggedString(operation.Summary[name]),
				})
			}
			items = append(items, taggedObject([]jsonField{
				{key: "operation", value: taggedObject([]jsonField{
					{key: "id", value: taggedString(operation.Operation.id)},
					{key: "version", value: taggedInteger(uint64(operation.Operation.version))},
				})},
				{key: "summary", value: taggedObject(summaryFields)},
			}))
		}
		operations = taggedArray(items)
	}
	var sourcePatch *jsonNode = taggedNull()
	if entry.sourcePatch != nil {
		sourcePatch = sourcePatchNode(entry.sourcePatch)
	}
	var failureCode *jsonNode = taggedNull()
	if entry.failureCode != nil {
		failureCode = taggedString(*entry.failureCode)
	}
	var diagnostics *jsonNode = taggedNull()
	if entry.diagnostics != nil {
		items := make([]*jsonNode, 0, len(entry.diagnostics))
		for _, diagnostic := range entry.diagnostics {
			node, err := diagnosticNode(diagnostic)
			if err != nil {
				return nil, err
			}
			items = append(items, node)
		}
		diagnostics = taggedArray(items)
	}
	return taggedObject([]jsonField{
		{key: "path", value: taggedString(entry.path)},
		{key: "status", value: taggedString(string(entry.status))},
		{key: "profile", value: profile},
		{key: "source_digest", value: sourceDigest},
		{key: "operations", value: operations},
		{key: "source_patch", value: sourcePatch},
		{key: "failure_code", value: failureCode},
		{key: "diagnostics", value: diagnostics},
	}), nil
}

// parsePlanMessageNode decodes the plan manifest from the transport
// envelope's value node, re-validating every cross constraint. Record error
// paths are relative to the plan value (the Rust record codec restarts at
// "$"), so the shared vectors' error_path facts match.
func parsePlanMessageNode(value *jsonNode, registry ErrorCodeRegistry) (*BatchPlanMessage, error) {
	fields, err := jsonRecordFields(value,
		[]string{"schema", "product_version", "command", "files"}, "$")
	if err != nil {
		return nil, err
	}
	schema, err := jsonTaggedString(fields[0], "$.schema")
	if err != nil {
		return nil, err
	}
	if schema != "core.batch-plan@1" {
		return nil, protocolError(KindSchemaMismatch, "$.schema", "expected core.batch-plan@1")
	}
	productVersion, err := jsonTaggedString(fields[1], "$.product_version")
	if err != nil {
		return nil, err
	}
	command, err := jsonTaggedString(fields[2], "$.command")
	if err != nil {
		return nil, err
	}
	if command != "plan" {
		return nil, invalid("$.command", "expected command \"plan\"")
	}
	fileNodes, err := jsonTaggedArray(fields[3], "$.files")
	if err != nil {
		return nil, err
	}
	files := make([]*BatchPlanFileEntry, 0, len(fileNodes))
	for index, item := range fileNodes {
		entry, err := parsePlanEntryNode(item,
			"$.files["+uint32String(uint32(index))+"]", registry)
		if err != nil {
			return nil, err
		}
		files = append(files, entry)
	}
	return newBatchPlanMessageWithRegistry(productVersion, files, registry)
}

// parsePlanEntryNode decodes one plan entry from the tree.
func parsePlanEntryNode(node *jsonNode, path string, registry ErrorCodeRegistry) (*BatchPlanFileEntry, error) {
	fields, err := jsonRecordFields(node, []string{"path", "status", "profile",
		"source_digest", "operations", "source_patch", "failure_code", "diagnostics"}, path)
	if err != nil {
		return nil, err
	}
	pathText, err := jsonTaggedString(fields[0], path+".path")
	if err != nil {
		return nil, err
	}
	statusName, err := jsonTaggedString(fields[1], path+".status")
	if err != nil {
		return nil, err
	}
	var status BatchPlanFileStatus
	switch statusName {
	case "planned":
		status = PlanStatusPlanned
	case "failed":
		status = PlanStatusFailed
	default:
		return nil, invalid(path+".status", "unknown plan file status")
	}
	var profile *ProfileReference
	var sourceDigest *ContentDigest
	var operations []*EditOperationSummary
	var sourcePatch *SourcePatch
	var failureCode *string
	var diagnostics []*Diagnostic
	switch status {
	case PlanStatusPlanned:
		profile, err = parseProfileNode(fields[2], path+".profile")
		if err != nil {
			return nil, err
		}
		digest, err := parseDigestNode(fields[3], path+".source_digest")
		if err != nil {
			return nil, err
		}
		sourceDigest = &digest
		operationNodes, err := jsonTaggedArray(fields[4], path+".operations")
		if err != nil {
			return nil, err
		}
		operations = make([]*EditOperationSummary, 0, len(operationNodes))
		for operationIndex, item := range operationNodes {
			operation, err := parseOperationSummaryNode(item,
				path+".operations["+uint32String(uint32(operationIndex))+"]")
			if err != nil {
				return nil, err
			}
			operations = append(operations, operation)
		}
		sourcePatch, err = parseSourcePatchNode(fields[5], path+".source_patch")
		if err != nil {
			return nil, err
		}
		if !jsonIsTaggedNull(fields[6]) || !jsonIsTaggedNull(fields[7]) {
			return nil, invalid(path, "planned entries cannot carry failure_code or diagnostics")
		}
	case PlanStatusFailed:
		if !jsonIsTaggedNull(fields[2]) || !jsonIsTaggedNull(fields[3]) ||
			!jsonIsTaggedNull(fields[4]) || !jsonIsTaggedNull(fields[5]) {
			return nil, invalid(path, "failed entries cannot carry planning facts")
		}
		code, err := jsonTaggedString(fields[6], path+".failure_code")
		if err != nil {
			return nil, err
		}
		if code == "" {
			return nil, invalid(path+".failure_code", "failure_code cannot be empty")
		}
		failureCode = &code
		diagnosticNodes, err := jsonTaggedArray(fields[7], path+".diagnostics")
		if err != nil {
			return nil, err
		}
		diagnostics = make([]*Diagnostic, 0, len(diagnosticNodes))
		for diagnosticIndex, item := range diagnosticNodes {
			diagnostic, err := parseDiagnosticNode(item,
				path+".diagnostics["+uint32String(uint32(diagnosticIndex))+"]", registry)
			if err != nil {
				return nil, err
			}
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return NewBatchPlanFileEntry(pathText, status, profile, sourceDigest, operations,
		sourcePatch, failureCode, diagnostics, registry)
}

// sourcePatchNode builds the core.source-patch@2 tree with Bytes leaves.
func sourcePatchNode(patch *SourcePatch) *jsonNode {
	replacements := make([]*jsonNode, 0, len(patch.Replacements))
	for _, replacement := range patch.Replacements {
		replacements = append(replacements, taggedObject([]jsonField{
			{key: "old_start", value: taggedInteger(replacement.OldStart)},
			{key: "old_end", value: taggedInteger(replacement.OldEnd)},
			{key: "original", value: taggedBytes(replacement.Original)},
			{key: "replacement", value: taggedBytes(replacement.Replacement)},
			{key: "redact_original", value: taggedBoolean(replacement.RedactOriginal)},
			{key: "redact_replacement", value: taggedBoolean(replacement.RedactReplacement)},
		}))
	}
	metadataFields := make([]jsonField, 0, len(patch.Metadata))
	for _, name := range sortedStringKeys(patch.Metadata) {
		metadataFields = append(metadataFields, jsonField{
			key: name, value: taggedString(patch.Metadata[name]),
		})
	}
	return taggedObject([]jsonField{
		{key: "schema", value: taggedString("core.source-patch@2")},
		{key: "base_digest", value: digestNode(patch.BaseDigest)},
		{key: "target_digest", value: digestNode(patch.TargetDigest)},
		{key: "encoding", value: encodingFactsNode(&patch.Encoding)},
		{key: "replacements", value: taggedArray(replacements)},
		{key: "metadata", value: taggedObject(metadataFields)},
	})
}

// parseSourcePatchNode decodes the core.source-patch@2 tree with Bytes
// leaves into the native wire form.
func parseSourcePatchNode(node *jsonNode, path string) (*SourcePatch, error) {
	fields, err := jsonRecordFields(node, []string{"schema", "base_digest",
		"target_digest", "encoding", "replacements", "metadata"}, path)
	if err != nil {
		return nil, err
	}
	schema, err := jsonTaggedString(fields[0], path+".schema")
	if err != nil {
		return nil, err
	}
	if schema != "core.source-patch@2" {
		return nil, protocolError(KindSchemaMismatch, path+".schema", "expected core.source-patch@2")
	}
	baseDigest, err := parseDigestNode(fields[1], path+".base_digest")
	if err != nil {
		return nil, err
	}
	targetDigest, err := parseDigestNode(fields[2], path+".target_digest")
	if err != nil {
		return nil, err
	}
	encoding, err := parseEncodingFactsNode(fields[3], path+".encoding")
	if err != nil {
		return nil, err
	}
	replacementNodes, err := jsonTaggedArray(fields[4], path+".replacements")
	if err != nil {
		return nil, err
	}
	replacements := make([]SourceReplacement, 0, len(replacementNodes))
	for index, item := range replacementNodes {
		replacementPath := path + ".replacements[" + uint32String(uint32(index)) + "]"
		replacementFields, err := jsonRecordFields(item, []string{"old_start", "old_end",
			"original", "replacement", "redact_original", "redact_replacement"}, replacementPath)
		if err != nil {
			return nil, err
		}
		oldStart, err := jsonTaggedUint64(replacementFields[0], replacementPath+".old_start")
		if err != nil {
			return nil, err
		}
		oldEnd, err := jsonTaggedUint64(replacementFields[1], replacementPath+".old_end")
		if err != nil {
			return nil, err
		}
		original, err := jsonTaggedBytes(replacementFields[2], replacementPath+".original")
		if err != nil {
			return nil, err
		}
		replacementBytes, err := jsonTaggedBytes(replacementFields[3], replacementPath+".replacement")
		if err != nil {
			return nil, err
		}
		redactOriginal, err := jsonTaggedBoolean(replacementFields[4], replacementPath+".redact_original")
		if err != nil {
			return nil, err
		}
		redactReplacement, err := jsonTaggedBoolean(replacementFields[5], replacementPath+".redact_replacement")
		if err != nil {
			return nil, err
		}
		replacements = append(replacements, SourceReplacement{
			OldStart:          oldStart,
			OldEnd:            oldEnd,
			Original:          original,
			Replacement:       replacementBytes,
			RedactOriginal:    redactOriginal,
			RedactReplacement: redactReplacement,
		})
	}
	metadata, err := jsonStringMap(fields[5], path+".metadata")
	if err != nil {
		return nil, err
	}
	return &SourcePatch{
		BaseDigest:   baseDigest,
		TargetDigest: targetDigest,
		Encoding:     *encoding,
		Replacements: replacements,
		Metadata:     metadata,
	}, nil
}

// encodingFactsNode builds the source-patch@2 encoding facts tree.
func encodingFactsNode(facts *EncodingFacts) *jsonNode {
	profileDefault := taggedNull()
	if facts.ProfileDefault != nil {
		profileDefault = sourceEncodingNode(facts.ProfileDefault)
	}
	bom := taggedNull()
	if facts.Bom != nil {
		bom = taggedString(*facts.Bom)
	}
	declaration := taggedNull()
	if facts.Declaration != nil {
		declaration = sourceEncodingNode(facts.Declaration)
	}
	callerOverride := taggedNull()
	if facts.CallerOverride != nil {
		callerOverride = sourceEncodingNode(facts.CallerOverride)
	}
	selected := taggedNull()
	if facts.Selected != nil {
		selected = sourceEncodingNode(facts.Selected)
	}
	return taggedObject([]jsonField{
		{key: "profile_default", value: profileDefault},
		{key: "bom_policy", value: taggedString(facts.BomPolicy)},
		{key: "bom", value: bom},
		{key: "declaration", value: declaration},
		{key: "caller_override", value: callerOverride},
		{key: "selected", value: selected},
	})
}

// parseEncodingFactsNode decodes the encoding facts tree. profile_default is
// a required `core.source-encoding@1` record at the value level
// (source.rs); the tree codec mirrors that acceptance and rejects
// Null here.
func parseEncodingFactsNode(node *jsonNode, path string) (*EncodingFacts, error) {
	fields, err := jsonRecordFields(node, []string{"profile_default", "bom_policy",
		"bom", "declaration", "caller_override", "selected"}, path)
	if err != nil {
		return nil, err
	}
	profileDefault, err := parseSourceEncodingNode(fields[0], path+".profile_default")
	if err != nil {
		return nil, err
	}
	bomPolicy, err := jsonTaggedString(fields[1], path+".bom_policy")
	if err != nil {
		return nil, err
	}
	var bom *string
	if !jsonIsTaggedNull(fields[2]) {
		text, err := jsonTaggedString(fields[2], path+".bom")
		if err != nil {
			return nil, err
		}
		bom = &text
	}
	var declaration *SourceEncoding
	if !jsonIsTaggedNull(fields[3]) {
		declaration, err = parseSourceEncodingNode(fields[3], path+".declaration")
		if err != nil {
			return nil, err
		}
	}
	var callerOverride *SourceEncoding
	if !jsonIsTaggedNull(fields[4]) {
		callerOverride, err = parseSourceEncodingNode(fields[4], path+".caller_override")
		if err != nil {
			return nil, err
		}
	}
	selected, err := parseSourceEncodingNode(fields[5], path+".selected")
	if err != nil {
		return nil, err
	}
	return &EncodingFacts{
		ProfileDefault: profileDefault,
		BomPolicy:      bomPolicy,
		Bom:            bom,
		Declaration:    declaration,
		CallerOverride: callerOverride,
		Selected:       selected,
	}, nil
}

// sourceEncodingNode builds one core.source-encoding@1 tree record.
func sourceEncodingNode(encoding *SourceEncoding) *jsonNode {
	var codePage *jsonNode = taggedNull()
	if encoding.WindowsCodePage != nil {
		codePage = taggedInteger(uint64(*encoding.WindowsCodePage))
	}
	return taggedObject([]jsonField{
		{key: "schema", value: taggedString("core.source-encoding@1")},
		{key: "kind", value: taggedString(encoding.Kind)},
		{key: "windows_code_page", value: codePage},
	})
}

// parseSourceEncodingNode decodes one core.source-encoding@1 tree record.
func parseSourceEncodingNode(node *jsonNode, path string) (*SourceEncoding, error) {
	fields, err := jsonRecordFields(node, []string{"schema", "kind", "windows_code_page"}, path)
	if err != nil {
		return nil, err
	}
	schema, err := jsonTaggedString(fields[0], path+".schema")
	if err != nil {
		return nil, err
	}
	if schema != "core.source-encoding@1" {
		return nil, protocolError(KindSchemaMismatch, path+".schema", "expected core.source-encoding@1")
	}
	kind, err := jsonTaggedString(fields[1], path+".kind")
	if err != nil {
		return nil, err
	}
	var codePage *uint32
	if !jsonIsTaggedNull(fields[2]) {
		number, err := jsonTaggedUint32(fields[2], path+".windows_code_page")
		if err != nil {
			return nil, err
		}
		codePage = &number
	}
	return &SourceEncoding{Kind: kind, WindowsCodePage: codePage}, nil
}

// diagnosticNode builds one core.diagnostic@1 tree record, including fix
// replacements as Bytes leaves.
func diagnosticNode(diagnostic *Diagnostic) (*jsonNode, error) {
	related := make([]*jsonNode, 0, len(diagnostic.Related))
	for _, item := range diagnostic.Related {
		related = append(related, taggedObject([]jsonField{
			{key: "role", value: taggedString(item.Role)},
			{key: "location", value: locationNode(&item.Location)},
		}))
	}
	argumentFields := make([]jsonField, 0, len(diagnostic.Arguments))
	for _, name := range sortedStringKeys(diagnostic.Arguments) {
		argumentFields = append(argumentFields, jsonField{
			key: name, value: taggedString(diagnostic.Arguments[name]),
		})
	}
	notes := make([]*jsonNode, 0, len(diagnostic.Notes))
	for _, note := range diagnostic.Notes {
		notes = append(notes, taggedString(note))
	}
	fixes := make([]*jsonNode, 0, len(diagnostic.Fixes))
	for _, fix := range diagnostic.Fixes {
		var location *jsonNode = taggedNull()
		if fix.Location != nil {
			location = locationNode(fix.Location)
		}
		fixes = append(fixes, taggedObject([]jsonField{
			{key: "id", value: taggedString(fix.ID)},
			{key: "applicability", value: taggedString(fix.Applicability.String())},
			{key: "location", value: location},
			{key: "replacement", value: taggedBytes(fix.Replacement)},
		}))
	}
	var primary *jsonNode = taggedNull()
	if diagnostic.Primary != nil {
		primary = locationNode(diagnostic.Primary)
	}
	return taggedObject([]jsonField{
		{key: "schema", value: taggedString("core.diagnostic@1")},
		{key: "code", value: taggedString(diagnostic.Code)},
		{key: "category", value: taggedString(diagnostic.Category.String())},
		{key: "severity", value: taggedString(diagnostic.Severity.String())},
		{key: "primary", value: primary},
		{key: "related", value: taggedArray(related)},
		{key: "arguments", value: taggedObject(argumentFields)},
		{key: "notes", value: taggedArray(notes)},
		{key: "fixes", value: taggedArray(fixes)},
		{key: "occurrence", value: taggedInteger(diagnostic.Occurrence)},
	}), nil
}

// parseDiagnosticNode decodes one core.diagnostic@1 tree record, including
// fix replacements as bytes, and binds it to the registry.
func parseDiagnosticNode(node *jsonNode, path string, registry ErrorCodeRegistry) (*Diagnostic, error) {
	fields, err := jsonRecordFields(node, []string{"schema", "code", "category",
		"severity", "primary", "related", "arguments", "notes", "fixes", "occurrence"}, path)
	if err != nil {
		return nil, err
	}
	schema, err := jsonTaggedString(fields[0], path+".schema")
	if err != nil {
		return nil, err
	}
	if schema != "core.diagnostic@1" {
		return nil, protocolError(KindSchemaMismatch, path+".schema", "expected core.diagnostic@1")
	}
	code, err := jsonTaggedString(fields[1], path+".code")
	if err != nil {
		return nil, err
	}
	categoryText, err := jsonTaggedString(fields[2], path+".category")
	if err != nil {
		return nil, err
	}
	category, err := ParseDiagnosticCategory(categoryText)
	if err != nil {
		return nil, err
	}
	severityText, err := jsonTaggedString(fields[3], path+".severity")
	if err != nil {
		return nil, err
	}
	severity, err := ParseSeverity(severityText)
	if err != nil {
		return nil, err
	}
	var primary *SourceLocation
	if !jsonIsTaggedNull(fields[4]) {
		primary, err = parseLocationNode(fields[4], path+".primary")
		if err != nil {
			return nil, err
		}
	}
	relatedNodes, err := jsonTaggedArray(fields[5], path+".related")
	if err != nil {
		return nil, err
	}
	related := make([]RelatedSourceLocation, 0, len(relatedNodes))
	for index, item := range relatedNodes {
		itemPath := path + ".related[" + uint32String(uint32(index)) + "]"
		itemFields, err := jsonRecordFields(item, []string{"role", "location"}, itemPath)
		if err != nil {
			return nil, err
		}
		role, err := jsonTaggedString(itemFields[0], itemPath+".role")
		if err != nil {
			return nil, err
		}
		location, err := parseLocationNode(itemFields[1], itemPath+".location")
		if err != nil {
			return nil, err
		}
		related = append(related, RelatedSourceLocation{Role: role, Location: *location})
	}
	arguments, err := jsonStringMap(fields[6], path+".arguments")
	if err != nil {
		return nil, err
	}
	noteNodes, err := jsonTaggedArray(fields[7], path+".notes")
	if err != nil {
		return nil, err
	}
	notes := make([]string, 0, len(noteNodes))
	for index, note := range noteNodes {
		text, err := jsonTaggedString(note, path+".notes["+uint32String(uint32(index))+"]")
		if err != nil {
			return nil, err
		}
		notes = append(notes, text)
	}
	fixNodes, err := jsonTaggedArray(fields[8], path+".fixes")
	if err != nil {
		return nil, err
	}
	fixes := make([]FixProposal, 0, len(fixNodes))
	for index, item := range fixNodes {
		itemPath := path + ".fixes[" + uint32String(uint32(index)) + "]"
		itemFields, err := jsonRecordFields(item, []string{"id", "applicability",
			"location", "replacement"}, itemPath)
		if err != nil {
			return nil, err
		}
		id, err := jsonTaggedString(itemFields[0], itemPath+".id")
		if err != nil {
			return nil, err
		}
		applicabilityText, err := jsonTaggedString(itemFields[1], itemPath+".applicability")
		if err != nil {
			return nil, err
		}
		applicability, err := ParseFixApplicability(applicabilityText)
		if err != nil {
			return nil, err
		}
		var location *SourceLocation
		if !jsonIsTaggedNull(itemFields[2]) {
			location, err = parseLocationNode(itemFields[2], itemPath+".location")
			if err != nil {
				return nil, err
			}
		}
		replacement, err := jsonTaggedBytes(itemFields[3], itemPath+".replacement")
		if err != nil {
			return nil, err
		}
		fixes = append(fixes, FixProposal{
			ID: id, Applicability: applicability, Location: location, Replacement: replacement,
		})
	}
	occurrence, err := jsonTaggedUint64(fields[9], path+".occurrence")
	if err != nil {
		return nil, err
	}
	return NewDiagnostic(code, category, severity, primary, related, arguments,
		notes, fixes, occurrence, registry)
}

// locationNode builds one source-location tree record.
func locationNode(location *SourceLocation) *jsonNode {
	return taggedObject([]jsonField{
		{key: "source_id", value: taggedString(location.SourceID)},
		{key: "start_byte", value: taggedInteger(location.StartByte)},
		{key: "end_byte", value: taggedInteger(location.EndByte)},
	})
}

// parseLocationNode decodes one source-location tree record.
func parseLocationNode(node *jsonNode, path string) (*SourceLocation, error) {
	fields, err := jsonRecordFields(node, []string{"source_id", "start_byte", "end_byte"}, path)
	if err != nil {
		return nil, err
	}
	sourceID, err := jsonTaggedString(fields[0], path+".source_id")
	if err != nil {
		return nil, err
	}
	startByte, err := jsonTaggedUint64(fields[1], path+".start_byte")
	if err != nil {
		return nil, err
	}
	endByte, err := jsonTaggedUint64(fields[2], path+".end_byte")
	if err != nil {
		return nil, err
	}
	return NewSourceLocation(sourceID, startByte, endByte)
}

// digestNode builds one digest tree record.
func digestNode(digest ContentDigest) *jsonNode {
	return taggedObject([]jsonField{
		{key: "algorithm", value: taggedString(digest.Algorithm())},
		{key: "hex", value: taggedString(digest.Hex())},
	})
}

// parseDigestNode decodes one digest tree record.
func parseDigestNode(node *jsonNode, path string) (ContentDigest, error) {
	fields, err := jsonRecordFields(node, []string{"algorithm", "hex"}, path)
	if err != nil {
		return ContentDigest{}, err
	}
	algorithm, err := jsonTaggedString(fields[0], path+".algorithm")
	if err != nil {
		return ContentDigest{}, err
	}
	if algorithm != "sha256" {
		return ContentDigest{}, invalid(path, "expected sha256")
	}
	hex, err := jsonTaggedString(fields[1], path+".hex")
	if err != nil {
		return ContentDigest{}, err
	}
	if len(hex) != 64 || !isLowercaseHex(hex) {
		return ContentDigest{}, invalid(path, "invalid lowercase sha256")
	}
	var bytes [32]byte
	for index := 0; index < 32; index++ {
		high := hexDigitValue(hex[index*2])
		low := hexDigitValue(hex[index*2+1])
		bytes[index] = byte(high<<4 | low)
	}
	return ContentDigestFromBytes(bytes), nil
}

// parseProfileNode decodes one profile reference tree record.
func parseProfileNode(node *jsonNode, path string) (*ProfileReference, error) {
	fields, err := jsonRecordFields(node, []string{"id", "version"}, path)
	if err != nil {
		return nil, err
	}
	id, err := jsonTaggedString(fields[0], path+".id")
	if err != nil {
		return nil, err
	}
	version, err := jsonTaggedUint32(fields[1], path+".version")
	if err != nil {
		return nil, err
	}
	return NewProfileReference(id, version)
}

// parseOperationSummaryNode decodes one operation summary tree record.
func parseOperationSummaryNode(node *jsonNode, path string) (*EditOperationSummary, error) {
	fields, err := jsonRecordFields(node, []string{"operation", "summary"}, path)
	if err != nil {
		return nil, err
	}
	referenceFields, err := jsonRecordFields(fields[0], []string{"id", "version"}, path+".operation")
	if err != nil {
		return nil, err
	}
	id, err := jsonTaggedString(referenceFields[0], path+".operation.id")
	if err != nil {
		return nil, err
	}
	version, err := jsonTaggedUint32(referenceFields[1], path+".operation.version")
	if err != nil {
		return nil, err
	}
	summary, err := jsonStringMap(fields[1], path+".summary")
	if err != nil {
		return nil, err
	}
	return NewEditOperationSummary(NewFormatOperationId(id, version), summary)
}

// jsonStringMap decodes a tagged Object<String, String> from the tree.
func jsonStringMap(node *jsonNode, path string) (map[string]string, error) {
	if node.kind != jsonObject {
		return nil, protocolError(KindWrongType, path, "expected Object")
	}
	if err := jsonObjectFieldsExact(node, []string{"type", "entries"}, path); err != nil {
		return nil, err
	}
	entries := node.fields[1].value
	if entries.kind != jsonArray {
		return nil, protocolError(KindWrongType, path+".entries", "expected JSON array")
	}
	output := make(map[string]string, len(entries.items))
	for _, item := range entries.items {
		entryFields, err := jsonObjectExact(item, []string{"key", "value"}, path+".entries")
		if err != nil {
			return nil, err
		}
		key, err := jsonStringOf(entryFields[0], path+".entries.key")
		if err != nil {
			return nil, err
		}
		text, err := jsonTaggedString(entryFields[1], path+"."+key)
		if err != nil {
			return nil, err
		}
		output[key] = text
	}
	return output, nil
}

// taggedKindFields validates a tagged value object (its first member is
// "type") and returns the kind name plus the remaining members.
func taggedKindFields(node *jsonNode, path string) (string, []jsonField, error) {
	if node.kind != jsonObject {
		return "", nil, protocolError(KindWrongType, path, "expected Object")
	}
	if len(node.fields) == 0 || node.fields[0].key != "type" {
		return "", nil, protocolError(KindSchemaMismatch, path, "type must be the first field")
	}
	kind, err := jsonStringOf(node.fields[0].value, path+".type")
	if err != nil {
		return "", nil, err
	}
	return kind, node.fields[1:], nil
}

// memberOf finds one member by name in a member slice.
func memberOf(fields []jsonField, name string) *jsonNode {
	for _, field := range fields {
		if field.key == name {
			return field.value
		}
	}
	return nil
}

// jsonRecordFields validates a tagged Object value whose members are a fixed
// record (or, when expected is nil, an arbitrary Object<String, Value>
// mapping) and returns the member values in member order.
func jsonRecordFields(node *jsonNode, expected []string, path string) ([]*jsonNode, error) {
	kind, fields, err := taggedKindFields(node, path)
	if err != nil {
		return nil, err
	}
	if kind != "Object" {
		return nil, protocolError(KindWrongType, path, "expected Object")
	}
	entries := memberOf(fields, "entries")
	if entries == nil || entries.kind != jsonArray {
		return nil, protocolError(KindWrongType, path+".entries", "expected JSON array")
	}
	names := make([]string, 0, len(entries.items))
	values := make([]*jsonNode, 0, len(entries.items))
	for _, item := range entries.items {
		if item.kind != jsonObject {
			return nil, protocolError(KindWrongType, path+".entries", "expected JSON object entry")
		}
		entryFields, err := jsonObjectExact(item, []string{"key", "value"}, path+".entries")
		if err != nil {
			return nil, err
		}
		key, err := jsonStringOf(entryFields[0], path+".entries.key")
		if err != nil {
			return nil, err
		}
		names = append(names, key)
		values = append(values, entryFields[1])
	}
	if expected != nil {
		for _, name := range names {
			if !containsString(expected, name) {
				return nil, protocolError(KindUnknownField, path+"."+name, "field is not declared by the fixed schema")
			}
		}
		for _, name := range expected {
			if !containsString(names, name) {
				return nil, protocolError(KindMissingField, path+"."+name, "required field is absent")
			}
		}
		if !equalStrings(names, expected) {
			return nil, protocolError(KindSchemaMismatch, path, "fields are duplicated or not in canonical order")
		}
	}
	return values, nil
}

// The tagged-node builders produce the exact canonical tagged forms.
func taggedString(text string) *jsonNode {
	return &jsonNode{kind: jsonObject, fields: []jsonField{
		{key: "type", value: &jsonNode{kind: jsonString, text: "String"}},
		{key: "value", value: &jsonNode{kind: jsonString, text: text}},
	}}
}

func taggedInteger(value uint64) *jsonNode {
	return &jsonNode{kind: jsonObject, fields: []jsonField{
		{key: "type", value: &jsonNode{kind: jsonString, text: "Integer"}},
		{key: "value", value: &jsonNode{kind: jsonString, text: uint64String(value)}},
	}}
}

func taggedBoolean(value bool) *jsonNode {
	return &jsonNode{kind: jsonObject, fields: []jsonField{
		{key: "type", value: &jsonNode{kind: jsonString, text: "Boolean"}},
		{key: "value", value: &jsonNode{kind: jsonBool, truth: value}},
	}}
}

func taggedNull() *jsonNode {
	return &jsonNode{kind: jsonObject, fields: []jsonField{
		{key: "type", value: &jsonNode{kind: jsonString, text: "Null"}},
	}}
}

func taggedArray(items []*jsonNode) *jsonNode {
	return &jsonNode{kind: jsonObject, fields: []jsonField{
		{key: "type", value: &jsonNode{kind: jsonString, text: "Sequence"}},
		{key: "items", value: &jsonNode{kind: jsonArray, items: items}},
	}}
}

func taggedObject(fields []jsonField) *jsonNode {
	entries := make([]*jsonNode, 0, len(fields))
	for _, field := range fields {
		entries = append(entries, &jsonNode{kind: jsonObject, fields: []jsonField{
			{key: "key", value: &jsonNode{kind: jsonString, text: field.key}},
			{key: "value", value: field.value},
		}})
	}
	return &jsonNode{kind: jsonObject, fields: []jsonField{
		{key: "type", value: &jsonNode{kind: jsonString, text: "Object"}},
		{key: "entries", value: &jsonNode{kind: jsonArray, items: entries}},
	}}
}

func taggedBytes(bytes []byte) *jsonNode {
	const hexDigits = "0123456789abcdef"
	text := make([]byte, 0, len(bytes)*2)
	for _, byte := range bytes {
		text = append(text, hexDigits[byte>>4], hexDigits[byte&0x0f])
	}
	return &jsonNode{kind: jsonObject, fields: []jsonField{
		{key: "type", value: &jsonNode{kind: jsonString, text: "Bytes"}},
		{key: "hex", value: &jsonNode{kind: jsonString, text: string(text)}},
	}}
}

// jsonTaggedString reads a tagged String member.
func jsonTaggedString(node *jsonNode, path string) (string, error) {
	kind, fields, err := taggedKindFields(node, path)
	if err != nil {
		return "", err
	}
	if kind != "String" {
		return "", protocolError(KindWrongType, path, "expected String")
	}
	value := memberOf(fields, "value")
	if value == nil {
		return "", protocolError(KindMissingField, path+".value", "required field is absent")
	}
	return jsonStringOf(value, path+".value")
}

// jsonTaggedBoolean reads a tagged Boolean member.
func jsonTaggedBoolean(node *jsonNode, path string) (bool, error) {
	kind, fields, err := taggedKindFields(node, path)
	if err != nil {
		return false, err
	}
	if kind != "Boolean" {
		return false, protocolError(KindWrongType, path, "expected Boolean")
	}
	value := memberOf(fields, "value")
	if value == nil {
		return false, protocolError(KindMissingField, path+".value", "required field is absent")
	}
	return jsonBooleanOf(value, path+".value")
}

// jsonIsTaggedNull reports whether the member is a tagged Null.
func jsonIsTaggedNull(node *jsonNode) bool {
	if node.kind != jsonObject || len(node.fields) != 1 {
		return false
	}
	if node.fields[0].key != "type" || node.fields[0].value.kind != jsonString {
		return false
	}
	return node.fields[0].value.text == "Null"
}

// jsonTaggedUint32 reads a tagged Integer member fitting uint32.
func jsonTaggedUint32(node *jsonNode, path string) (uint32, error) {
	value, err := jsonTaggedUint64(node, path)
	if err != nil {
		return 0, err
	}
	if value > 0xffffffff {
		return 0, invalid(path, "expected an unsigned 32-bit Integer")
	}
	return uint32(value), nil
}

// jsonTaggedUint64 reads a tagged Integer member fitting uint64.
func jsonTaggedUint64(node *jsonNode, path string) (uint64, error) {
	kind, fields, err := taggedKindFields(node, path)
	if err != nil {
		return 0, err
	}
	if kind != "Integer" {
		return 0, protocolError(KindWrongType, path, "expected Integer")
	}
	value := memberOf(fields, "value")
	if value == nil {
		return 0, protocolError(KindMissingField, path+".value", "required field is absent")
	}
	text, err := jsonStringOf(value, path+".value")
	if err != nil {
		return 0, err
	}
	if text == "" {
		return 0, invalid(path, "expected an unsigned 64-bit Integer")
	}
	var number uint64
	for index := 0; index < len(text); index++ {
		digit := text[index]
		if digit < '0' || digit > '9' {
			return 0, invalid(path, "expected an unsigned 64-bit Integer")
		}
		if number > (^uint64(0)-uint64(digit-'0'))/10 {
			return 0, invalid(path, "expected an unsigned 64-bit Integer")
		}
		number = number*10 + uint64(digit-'0')
	}
	return number, nil
}

// jsonTaggedArray reads a tagged Sequence member.
func jsonTaggedArray(node *jsonNode, path string) ([]*jsonNode, error) {
	kind, fields, err := taggedKindFields(node, path)
	if err != nil {
		return nil, err
	}
	if kind != "Sequence" {
		return nil, protocolError(KindWrongType, path, "expected Sequence")
	}
	items := memberOf(fields, "items")
	if items == nil || items.kind != jsonArray {
		return nil, protocolError(KindWrongType, path+".items", "expected JSON array")
	}
	return items.items, nil
}

// jsonTaggedBytes reads a tagged Bytes member's hex content.
func jsonTaggedBytes(node *jsonNode, path string) ([]byte, error) {
	kind, fields, err := taggedKindFields(node, path)
	if err != nil {
		return nil, err
	}
	if kind != "Bytes" {
		return nil, protocolError(KindWrongType, path, "expected Bytes")
	}
	hexField := memberOf(fields, "hex")
	if hexField == nil {
		return nil, protocolError(KindMissingField, path+".hex", "required field is absent")
	}
	hex, err := jsonStringOf(hexField, path+".hex")
	if err != nil {
		return nil, err
	}
	if len(hex)%2 != 0 {
		return nil, invalid(path, "byte hex length must be even")
	}
	output := make([]byte, 0, len(hex)/2)
	for index := 0; index < len(hex); index += 2 {
		high := hexDigitValue(hex[index])
		low := hexDigitValue(hex[index+1])
		if high < 0 || low < 0 {
			return nil, invalid(path, "invalid byte hex")
		}
		output = append(output, byte(high<<4|low))
	}
	return output, nil
}

// uint64String formats a uint64 without strconv (kept dependency-visible;
// strconv is stdlib, this avoids the import for one call site).
func uint64String(value uint64) string {
	var digits [20]byte
	index := len(digits)
	if value == 0 {
		return "0"
	}
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// sortedStringKeys returns the sorted keys of a string map.
func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
