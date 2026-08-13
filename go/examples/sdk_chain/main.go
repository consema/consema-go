// Command sdk_chain is the Consema SDK chain example (Go): one JSON document
// through the full SDK surface — parse, native semantic query, best-exact
// projection, structural edit, canonical materialization, and cross-format
// conversion to TOML.
//
// Scenario: read `{"a":1,"b":{"c":2}}` under `json.strict`, query `b.c`
// (`json.native-semantic-query@1`), project
// `json.projection.best-exact-core@1`, edit `a` to `42` (semantic scalar
// replacement, `CanonicalForProfile` representation), materialize the edited
// value as canonical compact JSON, and convert the edited document to TOML
// (`toml.canonical-document`).
//
// Run: `cd go && go run ./examples/sdk_chain`
//
// Language-neutral contract reference (consema spec repository):
//   - https://github.com/consema/consema/blob/main/docs/cookbook.md — the CLI recipes for the same operations
//   - https://github.com/consema/consema/blob/main/docs/multi-language-implementation-plan.md — the five-language SDK design
//     https://github.com/consema/consema/blob/main/docs/cookbook.md
package main

import (
	"context"
	"fmt"
	"math/big"

	"consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
)

// memberValueRef returns the value of one object member by decoded name,
// walking ObjectMembers with an explicit SemanticAvailability pattern match.
func memberValueRef(value jsonpkg.JsonValue, name string) (jsonpkg.JsonValue, error) {
	availability := value.ObjectMembers()
	if !availability.IsAvailable() {
		return jsonpkg.JsonValue{}, fmt.Errorf("semantics unavailable: %v", availability.Reason())
	}
	members := availability.Value()
	if members == nil {
		return jsonpkg.JsonValue{}, fmt.Errorf("value is not an object")
	}
	for _, member := range members {
		nameAvailability := member.Name()
		if nameAvailability.IsAvailable() && nameAvailability.Value() != nil &&
			*nameAvailability.Value() == name {
			return member.Value(), nil
		}
	}
	return jsonpkg.JsonValue{}, fmt.Errorf("member %q not found", name)
}

// projectToJSON projects one JSON document and renders its value as canonical
// compact JSON bytes.
func projectToJSON(jsonDocument *jsonpkg.Document, projectionRequest *jsonpkg.ProjectionRequest,
	compactRequest document.MaterializationRequest) ([]byte, error) {
	result := jsonDocument.Project(projectionRequest)
	if result.Failed != nil {
		return nil, fmt.Errorf("projection failed: %v", result.Failed.Diagnostics)
	}
	materialized := jsonpkg.Materialize(result.Complete.Value, compactRequest)
	if materialized.Failed != nil {
		return nil, fmt.Errorf("materialization failed: %v", materialized.Failed.Failure)
	}
	return materialized.Complete.Document.Render(), nil
}

func main() {
	ctx := context.Background()
	profile := document.NewProfileId("json.strict", 1)
	source := []byte(`{"a":1,"b":{"c":2}}`)

	// 1. Parse under the exact profile through the single facade parse entry.
	parsed, parseFailure := consema.ParseDocument(ctx, source, profile)
	if parseFailure != nil {
		panic(parseFailure)
	}
	if parsed.FormationStatus() != document.FormationStatusComplete {
		panic("expected a Complete document")
	}
	fmt.Printf("parse: profile=%s status=%s render=%s\n",
		parsed.Profile().ID(), parsed.FormationStatus(), parsed.Render())
	jsonDocument, ok := parsed.AsJSON()
	if !ok {
		panic("source is not a JSON document")
	}

	// 2. Query `b.c` through the JSON native semantic domain.
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.try-object-members", 1)).
		Then(protocol.NewOperatorCall("json.member-name-equals", 1).
			WithArgument("name", core.String("b"))).
		Then(protocol.NewOperatorCall("json.member-value", 1)).
		Then(protocol.NewOperatorCall("json.try-object-members", 1)).
		Then(protocol.NewOperatorCall("json.member-name-equals", 1).
			WithArgument("name", core.String("c"))).
		Then(protocol.NewOperatorCall("json.member-value", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainJSONNativeV1()).
		WithExpression(expression).
		WithSelection(protocol.SelectionRequireOne)
	validated, queryFailure := definition.Validate()
	if queryFailure != nil {
		panic(queryFailure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, queryFailure := validated.Bind(capabilities)
	if queryFailure != nil {
		panic(queryFailure)
	}
	matches, queryFailure := jsonpkg.ExecuteJSONQuery(ctx, executable, jsonDocument,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		panic(queryFailure)
	}
	// Render the matched value through the semantic tree API (the same walk
	// the edit target below uses).
	bValue, walkFailure := memberValueRef(jsonDocument.Root(), "b")
	if walkFailure != nil {
		panic(walkFailure)
	}
	cValue, walkFailure := memberValueRef(bValue, "c")
	if walkFailure != nil {
		panic(walkFailure)
	}
	kind := "?"
	value := "?"
	if kindAvailability := cValue.Kind(); kindAvailability.IsAvailable() {
		kind = kindAvailability.Value().String()
	}
	if integerAvailability := cValue.AsInteger(); integerAvailability.IsAvailable() &&
		integerAvailability.Value() != nil {
		value = integerAvailability.Value().String()
	}
	fmt.Printf("query b.c: matches=%d value=%s kind=%s\n", len(matches), value, kind)

	// 3. Project the document with the conservative best-exact core target.
	projectionRequest, projectionFailure := jsonpkg.NewProjectionRequestBuilder(
		jsonpkg.ProjectionTargetBestExactCoreV1).Build()
	if projectionFailure != nil {
		panic(projectionFailure)
	}
	compactRequest := document.NewMaterializationRequest(
		document.NewProfileId("json.strict", 1),
		document.NewMaterializationStyleId("json.canonical-compact", 1),
	).WithNewline(document.NewlineNone)
	projectedBytes, projectFailure := projectToJSON(jsonDocument, projectionRequest, compactRequest)
	if projectFailure != nil {
		panic(projectFailure)
	}
	fmt.Printf("project json.projection.best-exact-core@1: fidelity=Exact value=%s\n",
		projectedBytes)

	// 4. Edit `a` to 42 with a semantic scalar replacement under the
	//    profile-canonical representation policy.
	aValue, walkFailure := memberValueRef(jsonDocument.Root(), "a")
	if walkFailure != nil {
		panic(walkFailure)
	}
	builder := jsonpkg.NewEditTransactionBuilder(jsonDocument)
	builder.SemanticScalar(aValue.NodeRef(), core.NewInteger(big.NewInt(42)),
		jsonpkg.RepresentationPolicyCanonicalForProfile)
	commit, editFailure := jsonDocument.Commit(builder.Build())
	if editFailure != nil {
		panic(editFailure)
	}
	edited := commit.Document
	fmt.Printf("edit a->42 semantic_scalar CanonicalForProfile: render=%s\n", edited.Render())

	// 5. Materialize the edited value as canonical compact JSON.
	editedBytes, materializeFailure := projectToJSON(edited, projectionRequest, compactRequest)
	if materializeFailure != nil {
		panic(materializeFailure)
	}
	fmt.Printf("materialize json.canonical-compact: %s\n", editedBytes)

	// 6. Convert the edited JSON document to TOML (two-stage composition).
	tomlRequest := document.NewMaterializationRequest(
		document.NewProfileId("toml.1.0", 1),
		document.NewMaterializationStyleId("toml.canonical-document", 1),
	).WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject)
	conversion := consema.ConvertJSON(edited, projectionRequest, tomlRequest)
	if conversion.Failed != nil {
		panic(conversion.Failed)
	}
	fmt.Println("convert to toml.canonical-document:")
	fmt.Print(string(conversion.Complete.Document.Render()))
}
