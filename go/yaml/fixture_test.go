package yaml

import (
	"os"
	"path/filepath"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/graph"
)

// fixtureBytes reads one read-only fixture under the repository
// conformance/fixtures/yaml directory (docs/go-implementation-plan.md
// §1.1: fixtures are consumed by repository-relative path, never copied).
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "conformance", "fixtures", "yaml", name)
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s unreadable: %v", name, err)
	}
	return bytes
}

// TestFixturesByteExactRoundTrip parses every real-project fixture under
// yaml.1.2-core@1 and pins the byte-exact unmodified rendering and the
// complete lossless-syntax coverage (the fixture README hardening gate
// surface).
func TestFixturesByteExactRoundTrip(t *testing.T) {
	fixtures := []string{
		"kubernetes-workload.yaml",
		"github-actions-ci.yaml",
		"compose-services.yaml",
		"anchor-heavy.yaml",
	}
	for _, name := range fixtures {
		source := fixtureBytes(t, name)
		doc, failure := Parse(source, Yaml12CoreV1, document.DefaultParseLimits())
		if failure != nil {
			t.Fatalf("%s parse failed: %s", name, failure.Code())
		}
		if string(doc.Render()) != string(source) {
			t.Fatalf("%s render is not byte-exact", name)
		}
		if doc.FormationStatus() != document.FormationStatusComplete {
			t.Fatalf("%s formation status", name)
		}
		coverage := 0
		for _, piece := range doc.LosslessStructuralIndex().Pieces() {
			coverage += piece.Span().Len()
		}
		if coverage != len(source) {
			t.Fatalf("%s lossless coverage %d != %d", name, coverage, len(source))
		}
	}
}

// TestFixturesGraphCross pins the graph round trip through PGCE for every
// fixture (the fixture README cross requirement).
func TestFixturesGraphCross(t *testing.T) {
	fixtures := []string{
		"kubernetes-workload.yaml",
		"github-actions-ci.yaml",
		"compose-services.yaml",
		"anchor-heavy.yaml",
	}
	for _, name := range fixtures {
		source := fixtureBytes(t, name)
		doc, failure := Parse(source, Yaml12CoreV1, document.DefaultParseLimits())
		if failure != nil {
			t.Fatalf("%s parse failed: %s", name, failure.Code())
		}
		projected, err := doc.ProjectGraph()
		if err != nil {
			t.Fatalf("%s graph projection failed: %v", name, err)
		}
		encoded, err := graph.EncodePGCE(projected)
		if err != nil {
			t.Fatalf("%s PGCE encode failed: %v", name, err)
		}
		decoded, err := graph.DecodePGCE(encoded, graph.DefaultPGCELimits())
		if err != nil {
			t.Fatalf("%s PGCE decode failed: %v", name, err)
		}
		if !graph.Equal(projected, decoded) {
			t.Fatalf("%s PGCE round trip lost topology", name)
		}
		// Graph materialization reproduces the topology.
		request := document.NewMaterializationRequest(
			document.NewProfileId("yaml.1.2-core", 1),
			document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
			WithNewline(document.NewlineLf)
		result := MaterializeGraph(projected, request)
		if result.Complete == nil {
			t.Fatalf("%s graph materialization failed: %s", name,
				result.Failed.Failure.Code())
		}
		reparsed, err := result.Complete.Document.ProjectGraph()
		if err != nil || !graph.Equal(reparsed, projected) {
			t.Fatalf("%s materialization lost topology", name)
		}
	}
}

// TestFixturesTreeShapedValueClosure closes the tree-shaped fixtures
// through PortableValue (the fixture README requirement).
func TestFixturesTreeShapedValueClosure(t *testing.T) {
	// kubernetes-workload is a two-document stream and is excluded: the
	// value closure requires exactly one document.
	for _, name := range []string{
		"github-actions-ci.yaml",
		"compose-services.yaml",
	} {
		source := fixtureBytes(t, name)
		doc, failure := Parse(source, Yaml12CoreV1, document.DefaultParseLimits())
		if failure != nil {
			t.Fatalf("%s parse failed: %s", name, failure.Code())
		}
		if doc.DocumentCount() != 1 {
			t.Fatalf("%s must be a single-document stream for the value closure", name)
		}
		projected := doc.ProjectValue(BestExactValueV1())
		if projected.Complete == nil {
			t.Fatalf("%s value projection failed: %s", name, projected.Failed.Code())
		}
		request := document.NewMaterializationRequest(
			document.NewProfileId("yaml.1.2-core", 1),
			document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
			WithNewline(document.NewlineLf)
		result := MaterializeValue(projected.Complete.Value, request)
		if result.Complete == nil {
			t.Fatalf("%s value materialization failed: %s", name,
				result.Failed.Failure.Code())
		}
		reprojected := result.Complete.Document.ProjectValue(BestExactValueV1())
		if reprojected.Complete == nil ||
			!core.Equal(reprojected.Complete.Value, projected.Complete.Value) {
			t.Fatalf("%s value closure lost facts", name)
		}
	}
}

// TestFixturesAnchorHeavySharing pins the anchor-heavy fixture: implicit
// sharing is rejected and explicit acyclic duplication succeeds (the
// fixture README requirement).
func TestFixturesAnchorHeavySharing(t *testing.T) {
	source := fixtureBytes(t, "anchor-heavy.yaml")
	doc, failure := Parse(source, Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	defaultResult := doc.ProjectValue(BestExactValueV1())
	if defaultResult.Failed == nil ||
		defaultResult.Failed.Code() != "yaml.projection.sharing@1" {
		t.Fatalf("anchor-heavy must reject implicit sharing: %v", defaultResult.Failed)
	}
	duplicated := doc.ProjectValue(BestExactValueV1().
		WithSharing(SharingPolicyDuplicateAcyclic))
	if duplicated.Complete == nil {
		t.Fatalf("explicit acyclic duplication failed: %s", duplicated.Failed.Code())
	}
	if duplicated.Complete.Fidelity != FidelityTransformed {
		t.Fatalf("duplication fidelity %s", duplicated.Complete.Fidelity)
	}
}
