package protocol

import (
	"sort"

	"consema.dev/consema/core"
)

// This file implements the transferable `core.diagnostic@1` record
// (crates/consema-protocol/src/diagnostic.rs). Construction validates the
// code against the frozen error registry and the category against the
// registry record (RFC 0011; RFC 0016 §6: "unknown code or category
// contradiction is a protocol error").

// Severity is the presentation severity of one diagnostic.
type Severity string

// The three frozen presentation severities.
const (
	SeverityInfo    Severity = "Info"
	SeverityWarning Severity = "Warning"
	SeverityError   Severity = "Error"
)

// ParseSeverity parses one canonical severity spelling.
func ParseSeverity(name string) (Severity, error) {
	switch name {
	case "Info":
		return SeverityInfo, nil
	case "Warning":
		return SeverityWarning, nil
	case "Error":
		return SeverityError, nil
	}
	return "", invalid("$.severity", "unknown diagnostic severity")
}

// String returns the canonical severity spelling.
func (s Severity) String() string { return string(s) }

// FixApplicability classifies whether a fix can be applied without
// additional judgment.
type FixApplicability string

// The three frozen applicability classes.
const (
	ApplicabilityMachineApplicable FixApplicability = "MachineApplicable"
	ApplicabilityMaybeApplicable   FixApplicability = "MaybeApplicable"
	ApplicabilityManual            FixApplicability = "Manual"
)

// ParseFixApplicability parses one canonical applicability spelling.
func ParseFixApplicability(name string) (FixApplicability, error) {
	switch name {
	case "MachineApplicable":
		return ApplicabilityMachineApplicable, nil
	case "MaybeApplicable":
		return ApplicabilityMaybeApplicable, nil
	case "Manual":
		return ApplicabilityManual, nil
	}
	return "", invalid("$.fixes[].applicability", "unknown fix applicability")
}

// String returns the canonical applicability spelling.
func (a FixApplicability) String() string { return string(a) }

// SourceLocation is a transferable source location bound to a
// caller-assigned stable source ID (diagnostic.rs:13-59).
type SourceLocation struct {
	// SourceID is the caller-assigned stable source identity.
	SourceID string
	// StartByte is the inclusive start byte.
	StartByte uint64
	// EndByte is the exclusive end byte.
	EndByte uint64
}

// NewSourceLocation validates one half-open source range.
func NewSourceLocation(sourceID string, startByte, endByte uint64) (*SourceLocation, error) {
	if len(sourceID) == 0 || len(sourceID) > 1024 || startByte > endByte {
		return nil, invalid("$.location", "source ID or half-open byte range is invalid")
	}
	return &SourceLocation{SourceID: sourceID, StartByte: startByte, EndByte: endByte}, nil
}

// RelatedSourceLocation is a related transferable source location with its
// stable relationship role.
type RelatedSourceLocation struct {
	// Role is the stable relationship role.
	Role string
	// Location is the related source range.
	Location SourceLocation
}

// FixProposal is an explicit source replacement proposal; never an implicit
// write.
type FixProposal struct {
	// ID is the stable namespaced fix ID.
	ID string
	// Applicability is the applicability classification.
	Applicability FixApplicability
	// Location is the optional target source range.
	Location *SourceLocation
	// Replacement is the exact replacement bytes.
	Replacement []byte
}

// Diagnostic is the full `core.diagnostic@1` record independent from
// control-flow status (RFC 0016 §6). New validates the code against the
// frozen error registry; unknown codes and category contradictions are
// protocol errors.
type Diagnostic struct {
	// Code is the stable namespaced registered code.
	Code string
	// Category is the diagnostic category.
	Category DiagnosticCategory
	// Severity is the presentation severity.
	Severity Severity
	// Primary is the optional primary source location.
	Primary *SourceLocation
	// Related are the related locations in semantic order.
	Related []RelatedSourceLocation
	// Arguments are the deterministic arguments; the wire form sorts the
	// names (the Rust BTreeMap ordering).
	Arguments map[string]string
	// Notes are the stable note IDs or localized fallback text.
	Notes []string
	// Fixes are the explicit optional fixes.
	Fixes []FixProposal
	// Occurrence is the final deterministic occurrence ordinal.
	Occurrence uint64
}

// NewDiagnostic validates the code/category consistency against the error
// registry and constructs the diagnostic (the Rust
// DiagnosticMessage::from_core_with_registry validation,
// diagnostic.rs:336-351).
func NewDiagnostic(code string, category DiagnosticCategory, severity Severity,
	primary *SourceLocation, related []RelatedSourceLocation,
	arguments map[string]string, notes []string, fixes []FixProposal,
	occurrence uint64, registry ErrorCodeRegistry) (*Diagnostic, error) {
	if err := validateDiagnosticCode(code, category, registry); err != nil {
		return nil, err
	}
	diagnostic := &Diagnostic{
		Code:       code,
		Category:   category,
		Severity:   severity,
		Primary:    primary,
		Related:    related,
		Arguments:  arguments,
		Notes:      notes,
		Fixes:      fixes,
		Occurrence: occurrence,
	}
	if diagnostic.Arguments == nil {
		diagnostic.Arguments = map[string]string{}
	}
	return diagnostic, nil
}

// ToValue encodes `core.diagnostic@1` (diagnostic.rs:187-250).
func (d *Diagnostic) ToValue() (core.Value, error) {
	related := make([]core.Value, 0, len(d.Related))
	for _, item := range d.Related {
		location, err := locationValue(&item.Location)
		if err != nil {
			return nil, err
		}
		entry, err := core.NewObject(
			core.Entry{Key: "role", Value: core.String(item.Role)},
			core.Entry{Key: "location", Value: location},
		)
		if err != nil {
			return nil, err
		}
		related = append(related, entry)
	}
	arguments, err := stringMapObject(d.Arguments)
	if err != nil {
		return nil, err
	}
	notes := make([]core.Value, 0, len(d.Notes))
	for _, note := range d.Notes {
		notes = append(notes, core.String(note))
	}
	fixes := make([]core.Value, 0, len(d.Fixes))
	for index, fix := range d.Fixes {
		// The wire replacement field is a Bytes leaf, which the value-level
		// diagnostic record codec keeps outside its expressible subset
		// (doc.go). Any fix carrying a replacement is rejected here.
		if fix.Replacement != nil {
			return nil, invalid("$.fixes["+uint32String(uint32(index))+".replacement",
				"fix replacements are outside the value-level diagnostic subset")
		}
		var location core.Value = core.NullValue()
		if fix.Location != nil {
			location, err = locationValue(fix.Location)
			if err != nil {
				return nil, err
			}
		}
		entry, err := core.NewObject(
			core.Entry{Key: "id", Value: core.String(fix.ID)},
			core.Entry{Key: "applicability", Value: core.String(fix.Applicability.String())},
			core.Entry{Key: "location", Value: location},
			core.Entry{Key: "replacement", Value: core.NullValue()},
		)
		if err != nil {
			return nil, err
		}
		fixes = append(fixes, entry)
	}
	primary := core.NullValue()
	if d.Primary != nil {
		primary, err = locationValue(d.Primary)
		if err != nil {
			return nil, err
		}
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.diagnostic@1")},
		core.Entry{Key: "code", Value: core.String(d.Code)},
		core.Entry{Key: "category", Value: core.String(d.Category.String())},
		core.Entry{Key: "severity", Value: core.String(d.Severity.String())},
		core.Entry{Key: "primary", Value: primary},
		core.Entry{Key: "related", Value: core.NewArray(related...)},
		core.Entry{Key: "arguments", Value: arguments},
		core.Entry{Key: "notes", Value: core.NewArray(notes...)},
		core.Entry{Key: "fixes", Value: core.NewArray(fixes...)},
		core.Entry{Key: "occurrence", Value: integerValue(d.Occurrence)},
	)
}

// FromValue strictly decodes `core.diagnostic@1` under one explicit error
// registry (diagnostic.rs:252-333).
func (d *Diagnostic) FromValue(value core.Value, registry ErrorCodeRegistry) (*Diagnostic, error) {
	fields, err := schemaFields(value, "core.diagnostic@1",
		[]string{"schema", "code", "category", "severity", "primary", "related",
			"arguments", "notes", "fixes", "occurrence"}, "$")
	if err != nil {
		return nil, err
	}
	code, err := stringOf(fields[1], "$.code")
	if err != nil {
		return nil, err
	}
	categoryText, err := stringOf(fields[2], "$.category")
	if err != nil {
		return nil, err
	}
	category, err := ParseDiagnosticCategory(categoryText)
	if err != nil {
		return nil, err
	}
	severityText, err := stringOf(fields[3], "$.severity")
	if err != nil {
		return nil, err
	}
	severity, err := ParseSeverity(severityText)
	if err != nil {
		return nil, err
	}
	var primary *SourceLocation
	if _, isNull := fields[4].(core.Null); !isNull {
		primary, err = parseLocation(fields[4], "$.primary")
		if err != nil {
			return nil, err
		}
	}
	relatedValues, err := sequenceOf(fields[5], "$.related")
	if err != nil {
		return nil, err
	}
	related := make([]RelatedSourceLocation, 0, len(relatedValues))
	for index, item := range relatedValues {
		path := "$.related[" + uint32String(uint32(index)) + "]"
		entry, err := exactFields(item, []string{"role", "location"}, path)
		if err != nil {
			return nil, err
		}
		role, err := stringOf(entry[0], path+".role")
		if err != nil {
			return nil, err
		}
		location, err := parseLocation(entry[1], path+".location")
		if err != nil {
			return nil, err
		}
		related = append(related, RelatedSourceLocation{Role: role, Location: *location})
	}
	arguments, err := stringMapFromObject(fields[6], "$.arguments")
	if err != nil {
		return nil, err
	}
	noteValues, err := sequenceOf(fields[7], "$.notes")
	if err != nil {
		return nil, err
	}
	notes := make([]string, 0, len(noteValues))
	for index, note := range noteValues {
		text, err := stringOf(note, "$.notes["+uint32String(uint32(index))+"]")
		if err != nil {
			return nil, err
		}
		notes = append(notes, text)
	}
	fixValues, err := sequenceOf(fields[8], "$.fixes")
	if err != nil {
		return nil, err
	}
	fixes := make([]FixProposal, 0, len(fixValues))
	for index, item := range fixValues {
		fix, err := decodeFix(item, "$.fixes["+uint32String(uint32(index))+"]")
		if err != nil {
			return nil, err
		}
		fixes = append(fixes, *fix)
	}
	occurrence, err := unsigned64(fields[9], "$.occurrence")
	if err != nil {
		return nil, err
	}
	return NewDiagnostic(code, category, severity, primary, related, arguments,
		notes, fixes, occurrence, registry)
}

// validateDiagnosticCode requires the code to be registered and its category
// to match the registry record (diagnostic.rs:336-351).
func validateDiagnosticCode(code string, category DiagnosticCategory, registry ErrorCodeRegistry) *ProtocolError {
	descriptor := registry.Descriptor(code)
	if descriptor == nil {
		return invalid("$.code", "unregistered public code: "+code)
	}
	if descriptor.Category != category {
		return invalid("$.category", "diagnostic category contradicts the error-code registry")
	}
	return nil
}

// locationValue encodes one source location.
func locationValue(location *SourceLocation) (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "source_id", Value: core.String(location.SourceID)},
		core.Entry{Key: "start_byte", Value: integerValue(location.StartByte)},
		core.Entry{Key: "end_byte", Value: integerValue(location.EndByte)},
	)
}

// parseLocation strictly decodes one source location (diagnostic.rs:386-393).
func parseLocation(value core.Value, path string) (*SourceLocation, error) {
	fields, err := exactFields(value, []string{"source_id", "start_byte", "end_byte"}, path)
	if err != nil {
		return nil, err
	}
	sourceID, err := stringOf(fields[0], path+".source_id")
	if err != nil {
		return nil, err
	}
	startByte, err := unsigned64(fields[1], path+".start_byte")
	if err != nil {
		return nil, err
	}
	endByte, err := unsigned64(fields[2], path+".end_byte")
	if err != nil {
		return nil, err
	}
	return NewSourceLocation(sourceID, startByte, endByte)
}

// decodeFix strictly decodes one fix proposal (diagnostic.rs:395-420). The
// wire replacement field is a Bytes leaf, which the value-level diagnostic
// record codec keeps outside its expressible subset (doc.go): any fix
// present on the wire is rejected here, matching the encoder's symmetric
// refusal. The other fields are still validated so that malformed records
// fail with their own errors.
func decodeFix(value core.Value, path string) (*FixProposal, error) {
	fields, err := exactFields(value, []string{"id", "applicability", "location", "replacement"}, path)
	if err != nil {
		return nil, err
	}
	if _, err := stringOf(fields[0], path+".id"); err != nil {
		return nil, err
	}
	applicabilityText, err := stringOf(fields[1], path+".applicability")
	if err != nil {
		return nil, err
	}
	if _, err := ParseFixApplicability(applicabilityText); err != nil {
		return nil, err
	}
	if _, isNull := fields[2].(core.Null); !isNull {
		if _, err := parseLocation(fields[2], path+".location"); err != nil {
			return nil, err
		}
	}
	return nil, invalid(path+".replacement",
		"fix replacements are outside the value-level diagnostic subset")
}

// stringMapObject encodes a deterministic sorted Object<String, String>.
func stringMapObject(values map[string]string) (core.Value, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]core.Entry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, core.Entry{Key: key, Value: core.String(values[key])})
	}
	return core.NewObject(entries...)
}

// stringMapFromObject decodes an Object<String, String>.
func stringMapFromObject(value core.Value, path string) (map[string]string, error) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, protocolError(KindWrongType, path, "expected Object<String, String>")
	}
	output := make(map[string]string, object.Len())
	for _, entry := range object.Entries() {
		text, err := stringOf(entry.Value, path+"."+entry.Key)
		if err != nil {
			return nil, err
		}
		output[entry.Key] = text
	}
	return output, nil
}

// hexDigitValue decodes one hexadecimal digit.
func hexDigitValue(digit byte) int {
	switch {
	case digit >= '0' && digit <= '9':
		return int(digit - '0')
	case digit >= 'a' && digit <= 'f':
		return int(digit-'a') + 10
	case digit >= 'A' && digit <= 'F':
		return int(digit-'A') + 10
	}
	return -1
}
