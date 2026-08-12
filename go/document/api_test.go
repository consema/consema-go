package document

import "testing"

// TestRegisteredIDs covers the registered-id construction surface.
func TestRegisteredIDs(t *testing.T) {
	profile := NewProfileId("json.strict", 1)
	if profile.ID() != "json.strict" || profile.Version() != 1 {
		t.Errorf("profile facts %q %d", profile.ID(), profile.Version())
	}
	style := NewMaterializationStyleId("json.canonical-pretty", 1)
	if style.ID() != "json.canonical-pretty" || style.Version() != 1 {
		t.Errorf("style facts %q %d", style.ID(), style.Version())
	}
	family := NewFormatFamilyId("consema.json-family", 2)
	if family.ID() != "consema.json-family" || family.Version() != 2 {
		t.Errorf("family facts %q %d", family.ID(), family.Version())
	}
}

// TestFormationStatusIsClosedTwoValues pins the RFC 0016 §5.1 F10 closed
// two-value formation status.
func TestFormationStatusIsClosedTwoValues(t *testing.T) {
	if FormationStatusComplete.String() != "Complete" {
		t.Errorf("complete = %q", FormationStatusComplete.String())
	}
	if FormationStatusRecovered.String() != "Recovered" {
		t.Errorf("recovered = %q", FormationStatusRecovered.String())
	}
	if FormationStatusComplete == FormationStatusRecovered {
		t.Error("the two statuses must be distinct")
	}
}

// TestNewlinePolicies covers the exact newline bytes.
func TestNewlinePolicies(t *testing.T) {
	if string(NewlineNone.Bytes()) != "" {
		t.Error("None must emit no bytes")
	}
	if string(NewlineLf.Bytes()) != "\n" {
		t.Error("Lf must emit LF")
	}
	if string(NewlineCrLf.Bytes()) != "\r\n" {
		t.Error("CrLf must emit CR LF")
	}
}

// TestLimitsDefaults pin the frozen limit defaults against the Rust
// document crate (lib.rs:629-639, materialization.rs:95-105, source.rs:
// 401-409, source_patch.rs:23-27).
func TestLimitsDefaults(t *testing.T) {
	parse := DefaultParseLimits()
	if parse.MaxSourceBytes != 64<<20 || parse.MaxNestingDepth != 256 ||
		parse.MaxTokenCount != 2_000_000 || parse.MaxNodeCount != 1_000_000 ||
		parse.MaxDiagnostics != 10_000 {
		t.Errorf("parse limits %+v", parse)
	}
	materialization := DefaultMaterializationLimits()
	if materialization.MaxInputNodes != 1_000_000 || materialization.MaxOutputBytes != 64<<20 ||
		materialization.MaxDepth != 256 || materialization.MaxReportEntries != 100_000 ||
		materialization.MaxProvenanceEntries != 2_000_000 {
		t.Errorf("materialization limits %+v", materialization)
	}
	source := DefaultSourceLimits()
	if source.MaxRawBytes != 64<<20 || source.MaxDecodedUTF8Bytes != 128<<20 ||
		source.MaxDecodedScalars != 64<<20 {
		t.Errorf("source limits %+v", source)
	}
	patch := DefaultSourcePatchLimits()
	if patch.MaxReplacements != 100_000 || patch.MaxPatchBytes != 128<<20 ||
		patch.Source.MaxRawBytes != 64<<20 {
		t.Errorf("patch limits %+v", patch)
	}
	unbounded := UnboundedSourceLimits()
	if unbounded.MaxRawBytes <= 64<<20 {
		t.Error("unbounded limits must be unbounded")
	}
}

// TestMaterializationRequestPolicies covers the request defaults and every
// explicit policy.
func TestMaterializationRequestPolicies(t *testing.T) {
	request := NewMaterializationRequest(NewProfileId("json.strict", 1),
		NewMaterializationStyleId("json.canonical-pretty", 1))
	if !request.Encoding().Equal(Utf8Encoding()) || request.Newline() != NewlineLf ||
		request.MappingPolicy() != MappingPolicyRequireObject ||
		request.Representability() != RepresentabilityPolicyExactOnly {
		t.Errorf("request defaults %+v", request)
	}
	limits := DefaultMaterializationLimits()
	limits.MaxOutputBytes = 10
	explicit := request.WithEncoding(Utf16LeEncoding()).
		WithNewline(NewlineCrLf).
		WithMappingPolicy(MappingPolicyUniqueStringEntriesToObject).
		WithLimits(limits)
	if explicit.TargetProfile().ID() != "json.strict" || explicit.Style().ID() != "json.canonical-pretty" {
		t.Errorf("request identity %+v", explicit)
	}
	if !explicit.Encoding().Equal(Utf16LeEncoding()) || explicit.Newline() != NewlineCrLf {
		t.Errorf("explicit policies %+v", explicit)
	}
	if explicit.MappingPolicy() != MappingPolicyUniqueStringEntriesToObject {
		t.Errorf("mapping policy %q", explicit.MappingPolicy())
	}
	if explicit.Limits().MaxOutputBytes != 10 {
		t.Errorf("limits %+v", explicit.Limits())
	}
}

// TestEncodingFactsClaimValidation covers the from-claim consistency rule:
// a claim whose selected encoding contradicts the resolution priority is
// rejected.
func TestEncodingFactsClaimValidation(t *testing.T) {
	declaration := Utf8Encoding()
	facts, err := NewEncodingFactsFromClaim(Utf8Encoding(), nil, &declaration, nil, Utf8Encoding())
	if err != nil {
		t.Fatalf("consistent claim = %v", err)
	}
	if !facts.Selected().Equal(Utf8Encoding()) {
		t.Errorf("selected %s", facts.Selected().AsStr())
	}
	caller := Latin1Encoding()
	_, err = NewEncodingFactsFromClaim(Utf8Encoding(), nil, &declaration, &caller, Utf8Encoding())
	if err == nil || err.(*SourceError).Code() != "core.source.encoding-conflict@1" {
		t.Errorf("inconsistent claim = %v", err)
	}
}

// TestEncodingFactsEquality covers the facts equality used by patch
// application.
func TestEncodingFactsEquality(t *testing.T) {
	first := mustSource(t, []byte{0x41}, NewEncodingRequest(Utf8Encoding()))
	second := mustSource(t, []byte{0x41}, NewEncodingRequest(Utf8Encoding()))
	if !first.EncodingFacts().Equal(second.EncodingFacts()) {
		t.Error("equal facts must compare equal")
	}
	latin1 := mustSource(t, []byte{0x41}, NewEncodingRequest(Latin1Encoding()))
	if first.EncodingFacts().Equal(latin1.EncodingFacts()) {
		t.Error("different facts must not compare equal")
	}
}
