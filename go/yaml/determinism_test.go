package yaml

import (
	"context"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file pins the §16.3 hard gate: Go map iteration order must never
// influence any public result (the roadmap §16.3 hard gate as pinned by
// the go-implementation-plan risk row R-5 — "Go map 迭代顺序泄漏进公共
// 结果"; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.3 R-5 — and RFC 0016 §4.1 "PortableValue → Go types (frozen)",
// whose Object row spells the contract: ordered `[]core.Entry`, never
// `map[string]Value`, because entry order is a language-neutral fact;
// wave-4 R40, 2026-08-15: the old "§16.3 line 1509" and "§4.1 line 125"
// bare line-number references pointed at unrelated lines and have been
// replaced by section anchors — line numbers may drift, sections are the
// anchor). Every public fact below is produced repeatedly from identical
// inputs and must be byte-identical across runs.

// determinismSource exercises every public surface: anchors, aliases,
// duplicates, styles, and profiles.
const determinismSource = "root: &x [one, two]\ncopy: *x\nmap: {a: 1, a: 2}\n" +
	"literal: |\n  text\nfolded: >\n  fold\n" +
	"yes: true\nflag: yes\nnumber: 017\n"

// TestDeterminismPublicFacts pins identical facts across repeated runs.
func TestDeterminismPublicFacts(t *testing.T) {
	first := determinismFacts(t, determinismSource)
	for iteration := 0; iteration < 20; iteration++ {
		second := determinismFacts(t, determinismSource)
		if first != second {
			t.Fatalf("iteration %d produced different public facts", iteration)
		}
	}
}

func determinismFacts(t *testing.T, source string) string {
	t.Helper()
	var output []byte
	// Parse formation facts under both profiles.
	for _, profile := range []YamlProfile{Yaml12CoreV1, Yaml11CompatV1} {
		doc, failure := Parse([]byte(source), profile, document.DefaultParseLimits())
		if failure != nil {
			t.Fatalf("parse failed: %s", failure.Code())
		}
		output = append(output, doc.Render()...)
		output = append(output, '\n')
		output = append(output, profileNameFacts(doc)...)
		// Graph projection facts.
		projected, err := doc.ProjectGraph()
		if err != nil {
			t.Fatalf("graph failed: %v", err)
		}
		for _, id := range projected.Nodes() {
			node, _ := projected.Node(id)
			output = append(output, node.Tag()...)
			output = append(output, '|')
			if content, ok := node.ScalarContent(); ok {
				output = append(output, content...)
			}
			output = append(output, ';')
		}
		// Value projection facts.
		valueResult := doc.ProjectValue(BestExactValueV1().
			WithSharing(SharingPolicyDuplicateAcyclic))
		if valueResult.Complete == nil {
			t.Fatalf("value projection failed: %s", valueResult.Failed.Code())
		}
		output = append(output, valueFacts(valueResult.Complete.Value)...)
		output = append(output, '|')
		for _, event := range valueResult.Complete.Report.Events() {
			output = append(output, event.Policy...)
			output = append(output, event.NewCategory...)
			output = append(output, ';')
		}
	}
	// Query ordering facts.
	doc, failure := Parse([]byte(source), Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.documents", 1)).
		Then(protocol.NewOperatorCall("yaml.document-root", 1)).
		Then(protocol.NewOperatorCall("yaml.try-mapping-entries", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainYAMLNativeV1()).
		WithExpression(expression)
	validated, queryFailure := definition.Validate()
	if queryFailure != nil {
		t.Fatalf("validation: %s", queryFailure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, queryFailure := validated.Bind(capabilities)
	if queryFailure != nil {
		t.Fatalf("binding: %s", queryFailure.Code())
	}
	matches, queryFailure := ExecuteYamlQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		t.Fatalf("query: %s", queryFailure.Code())
	}
	for index := range matches {
		output = append(output, byte('0'+matches[index].Ordinal))
	}
	return string(output)
}

func profileNameFacts(doc *Document) []byte {
	var output []byte
	yamlDoc, _ := doc.Document(0)
	for ordinal := 0; ; ordinal++ {
		entry, ok := yamlDoc.Root().MappingEntry(ordinal)
		if !ok {
			break
		}
		if scalar, ok := entry.Value().Scalar(); ok {
			output = append(output, scalar.Kind().String()...)
			output = append(output, scalar.Canonical()...)
			output = append(output, ';')
		}
	}
	return output
}

func valueFacts(value core.Value) []byte {
	var output []byte
	switch typed := value.(type) {
	case *core.Array:
		output = append(output, '[')
		for _, item := range typed.Items() {
			output = append(output, valueFacts(item)...)
		}
		output = append(output, ']')
	case *core.Object:
		output = append(output, '{')
		for _, entry := range typed.Entries() {
			output = append(output, entry.Key...)
			output = append(output, ':')
			output = append(output, valueFacts(entry.Value)...)
		}
		output = append(output, '}')
	case *core.EntryMapping:
		output = append(output, '(')
		for _, entry := range typed.Entries() {
			output = append(output, valueFacts(entry.Key)...)
			output = append(output, ':')
			output = append(output, valueFacts(entry.Value)...)
		}
		output = append(output, ')')
	case core.Null:
		output = append(output, 'n')
	case core.Boolean:
		if typed {
			output = append(output, 't')
		} else {
			output = append(output, 'f')
		}
	case core.Integer:
		output = append(output, typed.Int().String()...)
	case core.Decimal:
		output = append(output, typed.Coefficient().String()...)
		output = append(output, 'e')
		output = append(output, typed.Exponent().String()...)
	case core.String:
		output = append(output, string(typed)...)
	}
	return output
}
