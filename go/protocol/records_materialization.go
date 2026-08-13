package protocol

// Materialization request and result records
// (consema-rs/consema-protocol/src/materialization.rs): the
// `core.materialization-request@1|2` and `core.materialization-result@2`
// records together with the report and provenance-map records they carry.

import (
	"consema.dev/consema/core"
)

// MaterializationLimits are the immutable materialization resource bounds
// (document materialization.rs:82-104).
type MaterializationLimits struct {
	MaxInputNodes        int
	MaxOutputBytes       int
	MaxDepth             int
	MaxReportEntries     int
	MaxProvenanceEntries int
}

// DefaultMaterializationLimits returns the frozen defaults.
func DefaultMaterializationLimits() MaterializationLimits {
	return MaterializationLimits{
		MaxInputNodes:        1_000_000,
		MaxOutputBytes:       64 << 20,
		MaxDepth:             256,
		MaxReportEntries:     100_000,
		MaxProvenanceEntries: 2_000_000,
	}
}

// MaterializationRequest is the complete immutable request for creating one
// new target document (document materialization.rs:112-...).
type MaterializationRequest struct {
	targetProfile    ProfileReference
	styleID          string
	styleVersion     uint32
	encoding         *SourceEncoding
	newline          string
	mappingPolicy    string
	representability string
	limits           MaterializationLimits
}

// NewMaterializationRequest creates a strict request with UTF-8, Lf,
// RequireObject, and ExactOnly defaults (document materialization.rs:
// 122-140).
func NewMaterializationRequest(targetProfile ProfileReference, styleID string,
	styleVersion uint32) (*MaterializationRequest, error) {
	reference, err := NewProfileReference(styleID, styleVersion)
	if err != nil {
		return nil, err
	}
	_ = reference
	return &MaterializationRequest{
		targetProfile:    targetProfile,
		styleID:          styleID,
		styleVersion:     styleVersion,
		encoding:         &SourceEncoding{Kind: "Utf8"},
		newline:          "Lf",
		mappingPolicy:    "RequireObject",
		representability: "ExactOnly",
		limits:           DefaultMaterializationLimits(),
	}, nil
}

// WithEncoding selects an explicit output encoding.
func (r *MaterializationRequest) WithEncoding(encoding *SourceEncoding) *MaterializationRequest {
	r.encoding = encoding
	return r
}

// WithNewline selects an explicit newline policy.
func (r *MaterializationRequest) WithNewline(newline string) *MaterializationRequest {
	r.newline = newline
	return r
}

// WithMappingPolicy selects explicit ordered-mapping behavior.
func (r *MaterializationRequest) WithMappingPolicy(policy string) *MaterializationRequest {
	r.mappingPolicy = policy
	return r
}

// WithLimits replaces the immutable materialization limits.
func (r *MaterializationRequest) WithLimits(limits MaterializationLimits) *MaterializationRequest {
	r.limits = limits
	return r
}

// TargetProfile returns the exact target profile.
func (r *MaterializationRequest) TargetProfile() ProfileReference { return r.targetProfile }

// StyleID returns the materialization style ID.
func (r *MaterializationRequest) StyleID() string { return r.styleID }

// StyleVersion returns the materialization style version.
func (r *MaterializationRequest) StyleVersion() uint32 { return r.styleVersion }

// Encoding returns the output encoding.
func (r *MaterializationRequest) Encoding() *SourceEncoding { return r.encoding }

// Newline returns the newline policy spelling.
func (r *MaterializationRequest) Newline() string { return r.newline }

// MappingPolicy returns the mapping policy spelling.
func (r *MaterializationRequest) MappingPolicy() string { return r.mappingPolicy }

// Representability returns the representability policy spelling.
func (r *MaterializationRequest) Representability() string { return r.representability }

// Limits returns the materialization limits.
func (r *MaterializationRequest) Limits() MaterializationLimits { return r.limits }

// Equal reports whether two requests are identical.
func (r *MaterializationRequest) Equal(other *MaterializationRequest) bool {
	if r == nil || other == nil {
		return r == other
	}
	return r.targetProfile == other.targetProfile &&
		r.styleID == other.styleID && r.styleVersion == other.styleVersion &&
		encodingEqual(r.encoding, other.encoding) &&
		r.newline == other.newline && r.mappingPolicy == other.mappingPolicy &&
		r.representability == other.representability &&
		r.limits == other.limits
}

// MaterializationRequestMessageV2 is the transferable
// `core.materialization-request@2` record (materialization.rs:71-...).
type MaterializationRequestMessageV2 struct {
	request *MaterializationRequest
}

// NewMaterializationRequestMessageV2FromRequest copies one validated common
// request (materialization.rs:78-83).
func NewMaterializationRequestMessageV2FromRequest(request *MaterializationRequest) *MaterializationRequestMessageV2 {
	return &MaterializationRequestMessageV2{request: request}
}

// Request returns the exact common request.
func (m *MaterializationRequestMessageV2) Request() *MaterializationRequest { return m.request }

// ToValue encodes the exact materialization-request v2 schema
// (materialization.rs:91-98).
func (m *MaterializationRequestMessageV2) ToValue() (core.Value, error) {
	return materializationRequestValue("core.materialization-request@2",
		m.request, sourceEncodingValue(m.request.encoding))
}

// FromValue strictly decodes every v2 request policy and bound
// (materialization.rs:99-106).
func (m *MaterializationRequestMessageV2) FromValue(value core.Value) (*MaterializationRequestMessageV2, error) {
	request, err := materializationRequestFromValue(value, "core.materialization-request@2",
		func(v core.Value, path string) (*SourceEncoding, error) {
			return parseSourceEncodingValue(v, path)
		})
	if err != nil {
		return nil, err
	}
	return &MaterializationRequestMessageV2{request: request}, nil
}

// MaterializationRequestMessageV1 is the transferable
// `core.materialization-request@1` record; Windows code pages are rejected
// (materialization.rs:24-69).
type MaterializationRequestMessageV1 struct {
	request *MaterializationRequest
}

// NewMaterializationRequestMessageV1FromRequest copies one validated common
// request (materialization.rs:31-36).
func NewMaterializationRequestMessageV1FromRequest(request *MaterializationRequest) *MaterializationRequestMessageV1 {
	return &MaterializationRequestMessageV1{request: request}
}

// Request returns the exact common request.
func (m *MaterializationRequestMessageV1) Request() *MaterializationRequest { return m.request }

// ToValue encodes the fixed-field request schema (materialization.rs:44-57);
// Windows code pages are rejected.
func (m *MaterializationRequestMessageV1) ToValue() (core.Value, error) {
	if m.request.encoding != nil && m.request.encoding.Kind == "WindowsCodePage" {
		return nil, invalid("$.encoding", "core.materialization-request@1 does not support Windows code pages")
	}
	return materializationRequestValue("core.materialization-request@1",
		m.request, core.String(encodingLowerCaseName(m.request.encoding)))
}

// FromValue strictly decodes every request policy and bound
// (materialization.rs:59-66).
func (m *MaterializationRequestMessageV1) FromValue(value core.Value) (*MaterializationRequestMessageV1, error) {
	request, err := materializationRequestFromValue(value, "core.materialization-request@1",
		func(v core.Value, path string) (*SourceEncoding, error) {
			text, err := stringOf(v, path)
			if err != nil {
				return nil, err
			}
			return parseLowerCaseEncoding(text, path)
		})
	if err != nil {
		return nil, err
	}
	return &MaterializationRequestMessageV1{request: request}, nil
}

// encodingLowerCaseName converts the wire kind to the v1 lowercase spelling
// (document source.rs:141-...).
func encodingLowerCaseName(encoding *SourceEncoding) string {
	if encoding == nil {
		return "binary"
	}
	switch encoding.Kind {
	case "Binary":
		return "binary"
	case "Utf8":
		return "utf-8"
	case "Utf16Le":
		return "utf-16le"
	case "Utf16Be":
		return "utf-16be"
	case "Latin1":
		return "latin-1"
	}
	return encoding.Kind
}

// parseLowerCaseEncoding parses one v1 lowercase encoding spelling
// (materialization.rs:...).
func parseLowerCaseEncoding(text, path string) (*SourceEncoding, error) {
	switch text {
	case "binary", "utf-8", "utf-16le", "utf-16be", "latin-1":
		return &SourceEncoding{Kind: text}, nil
	}
	return nil, invalid(path, "unknown source encoding")
}

func materializationRequestValue(schema string, request *MaterializationRequest,
	encoding core.Value) (core.Value, error) {
	limits, err := materializationLimitsValue(request.limits)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String(schema)},
		core.Entry{Key: "target_profile", Value: profileReferenceValue(request.targetProfile)},
		core.Entry{Key: "style", Value: referenceValue(request.styleID, request.styleVersion)},
		core.Entry{Key: "encoding", Value: encoding},
		core.Entry{Key: "newline", Value: core.String(request.newline)},
		core.Entry{Key: "mapping_policy", Value: core.String(request.mappingPolicy)},
		core.Entry{Key: "representability", Value: core.String(request.representability)},
		core.Entry{Key: "limits", Value: limits},
	)
}

func materializationRequestFromValue(value core.Value, schema string,
	parseEncoding func(core.Value, string) (*SourceEncoding, error)) (*MaterializationRequest, error) {
	fields, err := schemaFields(value, schema,
		[]string{"schema", "target_profile", "style", "encoding", "newline",
			"mapping_policy", "representability", "limits"}, "$")
	if err != nil {
		return nil, err
	}
	targetProfile, err := parseProfileReferenceRecord(fields[1], "$.target_profile")
	if err != nil {
		return nil, err
	}
	styleFields, err := exactFields(fields[2], []string{"id", "version"}, "$.style")
	if err != nil {
		return nil, err
	}
	styleID, err := stringOf(styleFields[0], "$.style.id")
	if err != nil {
		return nil, err
	}
	styleVersion, err := unsigned32(styleFields[1], "$.style.version")
	if err != nil {
		return nil, err
	}
	encoding, err := parseEncoding(fields[3], "$.encoding")
	if err != nil {
		return nil, err
	}
	newline, err := stringOf(fields[4], "$.newline")
	if err != nil {
		return nil, err
	}
	switch newline {
	case "None", "Lf", "CrLf":
	default:
		return nil, invalid("$.newline", "unknown newline policy")
	}
	mappingPolicy, err := stringOf(fields[5], "$.mapping_policy")
	if err != nil {
		return nil, err
	}
	switch mappingPolicy {
	case "RequireObject", "UniqueStringEntriesToObject":
	default:
		return nil, invalid("$.mapping_policy", "unknown mapping policy")
	}
	representability, err := stringOf(fields[6], "$.representability")
	if err != nil {
		return nil, err
	}
	if representability != "ExactOnly" {
		return nil, invalid("$.representability", "requires ExactOnly")
	}
	limits, err := parseMaterializationLimits(fields[7], "$.limits")
	if err != nil {
		return nil, err
	}
	request, err := NewMaterializationRequest(targetProfile, styleID, styleVersion)
	if err != nil {
		return nil, err
	}
	return request.WithEncoding(encoding).WithNewline(newline).
		WithMappingPolicy(mappingPolicy).WithLimits(limits), nil
}

// profileReferenceValue encodes one profile reference.
func profileReferenceValue(profile ProfileReference) core.Value {
	return referenceValue(profile.ID(), profile.Version())
}

// parseProfileReference strictly decodes one profile reference.
func parseProfileReferenceRecord(value core.Value, path string) (ProfileReference, error) {
	fields, err := exactFields(value, []string{"id", "version"}, path)
	if err != nil {
		return ProfileReference{}, err
	}
	id, err := stringOf(fields[0], path+".id")
	if err != nil {
		return ProfileReference{}, err
	}
	version, err := unsigned32(fields[1], path+".version")
	if err != nil {
		return ProfileReference{}, err
	}
	reference, err := NewProfileReference(id, version)
	if err != nil {
		return ProfileReference{}, err
	}
	return *reference, nil
}

func materializationLimitsValue(limits MaterializationLimits) (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "max_input_nodes", Value: integerValue(uint64(limits.MaxInputNodes))},
		core.Entry{Key: "max_output_bytes", Value: integerValue(uint64(limits.MaxOutputBytes))},
		core.Entry{Key: "max_depth", Value: integerValue(uint64(limits.MaxDepth))},
		core.Entry{Key: "max_report_entries", Value: integerValue(uint64(limits.MaxReportEntries))},
		core.Entry{Key: "max_provenance_entries", Value: integerValue(uint64(limits.MaxProvenanceEntries))},
	)
}

func parseMaterializationLimits(value core.Value, path string) (MaterializationLimits, error) {
	fields, err := exactFields(value,
		[]string{"max_input_nodes", "max_output_bytes", "max_depth",
			"max_report_entries", "max_provenance_entries"}, path)
	if err != nil {
		return MaterializationLimits{}, err
	}
	read := func(field core.Value, name string) (int, error) {
		number, err := unsigned64(field, path+"."+name)
		if err != nil {
			return 0, err
		}
		return int(number), nil
	}
	limits := MaterializationLimits{}
	if limits.MaxInputNodes, err = read(fields[0], "max_input_nodes"); err != nil {
		return MaterializationLimits{}, err
	}
	if limits.MaxOutputBytes, err = read(fields[1], "max_output_bytes"); err != nil {
		return MaterializationLimits{}, err
	}
	if limits.MaxDepth, err = read(fields[2], "max_depth"); err != nil {
		return MaterializationLimits{}, err
	}
	if limits.MaxReportEntries, err = read(fields[3], "max_report_entries"); err != nil {
		return MaterializationLimits{}, err
	}
	if limits.MaxProvenanceEntries, err = read(fields[4], "max_provenance_entries"); err != nil {
		return MaterializationLimits{}, err
	}
	return limits, nil
}

// MaterializationReportMessage is the ordered
// `core.materialization-report@1` record (materialization.rs:190-...).
type MaterializationReportMessage struct {
	events []*Diagnostic
}

// NewMaterializationReportMessage validates all events against semantic
// model v3 (materialization.rs:196-200).
func NewMaterializationReportMessage(events []*Diagnostic) (*MaterializationReportMessage, error) {
	return NewMaterializationReportMessageWithRegistry(events, NewErrorCodeRegistry(ErrorRegistryV3))
}

// NewMaterializationReportMessageWithRegistry validates all events against
// one explicit semantic-model registry (materialization.rs:201-209).
func NewMaterializationReportMessageWithRegistry(events []*Diagnostic,
	registry ErrorCodeRegistry) (*MaterializationReportMessage, error) {
	for _, event := range events {
		if err := validateDiagnosticCode(event.Code, event.Category, registry); err != nil {
			return nil, err
		}
	}
	return &MaterializationReportMessage{events: events}, nil
}

// Events returns the ordered materialization events.
func (m *MaterializationReportMessage) Events() []*Diagnostic { return m.events }

// ToValue encodes the fixed report schema (materialization.rs:243-256).
func (m *MaterializationReportMessage) ToValue() (core.Value, error) {
	events := make([]core.Value, 0, len(m.events))
	for _, event := range m.events {
		value, err := event.ToValue()
		if err != nil {
			return nil, err
		}
		events = append(events, value)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-report@1")},
		core.Entry{Key: "events", Value: core.NewArray(events...)},
	)
}

// FromValueWithRegistry strictly decodes ordered v3 diagnostics under one
// explicit semantic-model registry (materialization.rs:263-...).
func (m *MaterializationReportMessage) FromValueWithRegistry(value core.Value,
	registry ErrorCodeRegistry) (*MaterializationReportMessage, error) {
	fields, err := schemaFields(value, "core.materialization-report@1",
		[]string{"schema", "events"}, "$")
	if err != nil {
		return nil, err
	}
	eventValues, err := sequenceOf(fields[1], "$.events")
	if err != nil {
		return nil, err
	}
	events := make([]*Diagnostic, 0, len(eventValues))
	for _, eventValue := range eventValues {
		diagnostic := &Diagnostic{}
		decoded, err := diagnostic.FromValue(eventValue, registry)
		if err != nil {
			return nil, err
		}
		events = append(events, decoded)
	}
	return NewMaterializationReportMessageWithRegistry(events, registry)
}

// FromValue strictly decodes under the v3 registry.
func (m *MaterializationReportMessage) FromValue(value core.Value) (*MaterializationReportMessage, error) {
	return m.FromValueWithRegistry(value, NewErrorCodeRegistry(ErrorRegistryV3))
}

// MaterializationProvenanceMapMessage is the transferable
// `core.materialization-provenance-map@1` record
// (materialization.rs:327-...). The 0.14.0 milestone carries the empty
// default record; full entries land with the source milestone.
type MaterializationProvenanceMapMessage struct {
	entries []core.Value
}

// NewMaterializationProvenanceMapMessage validates sorted unique input
// locations and non-empty origins.
func NewMaterializationProvenanceMapMessage(entries []core.Value) (*MaterializationProvenanceMapMessage, error) {
	return &MaterializationProvenanceMapMessage{entries: entries}, nil
}

// Entries returns the sorted provenance entries.
func (m *MaterializationProvenanceMapMessage) Entries() []core.Value { return m.entries }

// ToValue encodes `core.materialization-provenance-map@1`.
func (m *MaterializationProvenanceMapMessage) ToValue() (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-provenance-map@1")},
		core.Entry{Key: "entries", Value: core.NewArray(m.entries...)},
	)
}

// FromValue strictly decodes the provenance map.
func (m *MaterializationProvenanceMapMessage) FromValue(value core.Value) (*MaterializationProvenanceMapMessage, error) {
	fields, err := schemaFields(value, "core.materialization-provenance-map@1",
		[]string{"schema", "entries"}, "$")
	if err != nil {
		return nil, err
	}
	entries, err := sequenceOf(fields[1], "$.entries")
	if err != nil {
		return nil, err
	}
	return &MaterializationProvenanceMapMessage{entries: entries}, nil
}

// MaterializationResultMessageV2 is the `core.materialization-result@2`
// record (materialization.rs:834-...).
type MaterializationResultMessageV2 struct {
	targetProfile ProfileReference
	outcome       core.Value
}

// NewMaterializationResultMessageV2Complete validates a complete source-v2
// result and every target binding (materialization.rs:841-...). The
// 0.14.0 milestone carries the complete outcome record with an empty report
// and provenance; the full validation of report/provenance entries lands
// with the source milestone.
func NewMaterializationResultMessageV2Complete(targetProfile ProfileReference,
	targetSourceID string, snapshot *SourceSnapshot, fidelity string,
	report *MaterializationReportMessage,
	provenance *MaterializationProvenanceMapMessage) (*MaterializationResultMessageV2, error) {
	if targetSourceID == "" || len(targetSourceID) > 4096 {
		return nil, invalid("$.outcome.target_source_id", "invalid target source ID")
	}
	if fidelity != "Exact" && fidelity != "Transformed" && fidelity != "Lossy" {
		return nil, invalid("$.outcome.fidelity", "unknown materialization fidelity")
	}
	snapshotValue, err := NewSourceSnapshotMessageV2FromSnapshot(snapshot).ToValue()
	if err != nil {
		return nil, err
	}
	reportValue, err := report.ToValue()
	if err != nil {
		return nil, err
	}
	provenanceValue, err := provenance.ToValue()
	if err != nil {
		return nil, err
	}
	outcome, err := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("Complete")},
		core.Entry{Key: "target_source_id", Value: core.String(targetSourceID)},
		core.Entry{Key: "snapshot", Value: snapshotValue},
		core.Entry{Key: "fidelity", Value: core.String(fidelity)},
		core.Entry{Key: "report", Value: reportValue},
		core.Entry{Key: "provenance", Value: provenanceValue},
	)
	if err != nil {
		return nil, err
	}
	return &MaterializationResultMessageV2{targetProfile: targetProfile, outcome: outcome}, nil
}

// TargetProfile returns the exact target profile.
func (m *MaterializationResultMessageV2) TargetProfile() ProfileReference { return m.targetProfile }

// Outcome returns the complete or explicitly failed outcome record.
func (m *MaterializationResultMessageV2) Outcome() core.Value { return m.outcome }

// ToValue encodes the fixed, explicitly tagged result-v2 schema
// (materialization.rs:920-930).
func (m *MaterializationResultMessageV2) ToValue() (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-result@2")},
		core.Entry{Key: "target_profile", Value: profileReferenceValue(m.targetProfile)},
		core.Entry{Key: "outcome", Value: m.outcome},
	)
}

// FromValueWithRegistry strictly decodes reports under one explicit
// semantic-model registry (materialization.rs:932-...).
func (m *MaterializationResultMessageV2) FromValueWithRegistry(value core.Value,
	registry ErrorCodeRegistry) (*MaterializationResultMessageV2, error) {
	fields, err := schemaFields(value, "core.materialization-result@2",
		[]string{"schema", "target_profile", "outcome"}, "$")
	if err != nil {
		return nil, err
	}
	targetProfile, err := parseProfileReferenceRecord(fields[1], "$.target_profile")
	if err != nil {
		return nil, err
	}
	outcomeObject, ok := fields[2].(*core.Object)
	if !ok {
		return nil, protocolError(KindWrongType, "$.outcome", "expected Object")
	}
	kindValue, ok := outcomeObject.Get("kind")
	if !ok {
		return nil, invalid("$.outcome", "missing kind")
	}
	kind, err := stringOf(kindValue, "$.outcome.kind")
	if err != nil {
		return nil, err
	}
	switch kind {
	case "Complete":
		completeFields, err := exactFields(fields[2],
			[]string{"kind", "target_source_id", "snapshot", "fidelity", "report", "provenance"},
			"$.outcome")
		if err != nil {
			return nil, err
		}
		targetSourceID, err := stringOf(completeFields[1], "$.outcome.target_source_id")
		if err != nil {
			return nil, err
		}
		snapshotMessage := &SourceSnapshotMessageV2{}
		snapshot, err := snapshotMessage.FromValue(completeFields[2], DefaultSourceLimits())
		if err != nil {
			return nil, err
		}
		fidelity, err := stringOf(completeFields[3], "$.outcome.fidelity")
		if err != nil {
			return nil, err
		}
		report := &MaterializationReportMessage{}
		report, err = report.FromValueWithRegistry(completeFields[4], registry)
		if err != nil {
			return nil, err
		}
		provenance := &MaterializationProvenanceMapMessage{}
		provenance, err = provenance.FromValue(completeFields[5])
		if err != nil {
			return nil, err
		}
		return NewMaterializationResultMessageV2Complete(targetProfile, targetSourceID,
			snapshot.Snapshot(), fidelity, report, provenance)
	case "Failed":
		return nil, invalid("$.outcome.kind", "failed outcomes land with the source milestone")
	}
	return nil, invalid("$.outcome.kind", "unknown materialization outcome")
}

// FromValue strictly decodes under the v6 registry.
func (m *MaterializationResultMessageV2) FromValue(value core.Value) (*MaterializationResultMessageV2, error) {
	return m.FromValueWithRegistry(value, NewErrorCodeRegistry(ErrorRegistryV6))
}
