package ontology

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// embeddedValidationSDL keeps notes/scratch.md untyped so it reaches embedded
// validation through the global @source only, and gives notes/daily/*.md a
// @contains collection so the same spans are also field-declared children.
const embeddedValidationSDL = `enum Priority { low high }

type DailyNote @node(paths: ["notes/daily/*.md"]) {
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item", min: 3)
}

type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  priority: Priority @field
  owner: String! @field
}`

var embeddedValidationSink []ValidationIssue

func loadEmbeddedValidationSchema(tb testing.TB) *Schema {
	tb.Helper()
	root := tb.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(tb, os.MkdirAll(dir, 0755))
	require.NoError(tb, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(embeddedValidationSDL), 0644))
	schema, err := LoadSchema(root)
	require.NoError(tb, err)
	return schema
}

// embeddedValidationFixture alternates valid items, items missing the required
// `owner` scalar, and items with an out-of-enum `priority`, so roughly two
// thirds of the embedded nodes emit issues.
func embeddedValidationFixture(count int) string {
	var content strings.Builder
	content.WriteString("# Scratch\n\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&content, "- [ ] Task %d #action-item ^task-%d\n", i, i)
		switch i % 3 {
		case 0:
			content.WriteString("  priority:: high\n")
		case 1:
			content.WriteString("  owner:: Alice\n  priority:: urgent\n")
		default:
			content.WriteString("  owner:: Alice\n  priority:: high\n")
		}
	}
	return content.String()
}

func buildEmbeddedValidationCase(tb testing.TB, schema *Schema, notePath string, count int) (*DocumentSnapshot, *NodeProjection) {
	tb.Helper()
	snapshot, err := BuildDocumentSnapshot(notePath, embeddedValidationFixture(count), time.Time{})
	require.NoError(tb, err)
	root, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindNote})
	require.NoError(tb, err)
	return snapshot, root
}

func expectedEmbeddedIssues(notePath string, count int) (owner, priority []string) {
	for i := 0; i < count; i++ {
		ref := fmt.Sprintf("%s#^task-%d", notePath, i)
		switch i % 3 {
		case 0:
			owner = append(owner, "missing_required_field|"+ref)
		case 1:
			priority = append(priority, "field_type_mismatch|"+ref)
		}
	}
	return owner, priority
}

func requireEmbeddedFixtureIssues(t *testing.T, got []ValidationIssue, notePath string, count int) {
	t.Helper()
	owner, priority := expectedEmbeddedIssues(notePath, count)
	require.Len(t, got, len(owner)+len(priority))
	require.Equal(t, owner, codesForField(got, "owner"))
	require.Equal(t, priority, codesForField(got, "priority"))
	for _, issue := range got {
		require.Equal(t, notePath, issue.NotePath)
		require.Equal(t, "ActionItem", issue.TypeName)
		require.NotEmpty(t, issue.NodeID)
		require.Positive(t, issue.Line)
		require.NotEmpty(t, issue.Message)
	}
}

func TestValidateEmbeddedProjectionFields_DiagnosticsAcrossScales(t *testing.T) {
	schema := loadEmbeddedValidationSchema(t)
	for _, count := range []int{1, 10, 100, 500} {
		t.Run(fmt.Sprintf("nodes=%d", count), func(t *testing.T) {
			const notePath = "notes/scratch.md"
			snapshot, root := buildEmbeddedValidationCase(t, schema, notePath, count)
			got := validateEmbeddedProjectionFields(snapshot, schema, root)
			requireEmbeddedFixtureIssues(t, got, notePath, count)
			if count == 1 {
				require.Equal(t, ValidationIssue{
					Code: "missing_required_field", NotePath: notePath,
					TypeName: "ActionItem", FieldName: "owner", NodeRef: notePath + "#^task-0",
					NodeID: notePath + "#^task-0", Structural: "7c4b5b442a54110862b67595531d75b06f0da7134f8cb39c9d8ff2199731c7ed",
					Line: 3, Message: "required field owner is missing",
					VariantKey: "ActionItem.owner", VariantLabel: "ActionItem · owner",
				}, got[0])
			}
		})
	}
}

func TestValidateEmbeddedProjectionFields_ContainsMinimum(t *testing.T) {
	schema := loadEmbeddedValidationSchema(t)
	snapshot, root := buildEmbeddedValidationCase(t, schema, "notes/daily/today.md", 2)
	got := validateEmbeddedProjectionFields(snapshot, schema, root)
	require.Len(t, got, 3)
	requireEmbeddedFixtureIssues(t, got[1:], snapshot.NotePath, 2)
	require.Equal(t, ValidationIssue{
		Code: "contains_min_not_met", NotePath: snapshot.NotePath,
		TypeName: "DailyNote", FieldName: "actionItems", NodeRef: snapshot.NotePath,
		Structural: root.Ref.Structural,
		Line:       1, Message: "field actionItems must contain at least 3 item(s)",
		VariantKey: "DailyNote.actionItems", VariantLabel: "DailyNote · actionItems",
	}, got[0])
}

func codesForField(issues []ValidationIssue, fieldName string) []string {
	var out []string
	for _, issue := range issues {
		if issue.FieldName == fieldName {
			out = append(out, issue.Code+"|"+issue.NodeRef)
		}
	}
	return out
}

func TestValidateEmbeddedProjectionFields_SkipsDuplicateRefsWithoutProjecting(t *testing.T) {
	schema := loadEmbeddedValidationSchema(t)
	// notes/daily/today.md is DailyNote, so every checkbox span is reachable
	// both as a @contains item and as a global @source ref.
	snapshot, root := buildEmbeddedValidationCase(t, schema, "notes/daily/today.md", 30)
	require.NotEmpty(t, root.Collections["actionItems"].Items)
	globalRefs, err := GlobalSourceNodeRefsFromSnapshot(snapshot, schema)
	require.NoError(t, err)
	require.NotEmpty(t, globalRefs)

	got := validateEmbeddedProjectionFields(snapshot, schema, root)
	requireEmbeddedFixtureIssues(t, got, snapshot.NotePath, 30)

	counts := map[string]int{}
	for _, issue := range got {
		counts[issue.NodeRef+"|"+issue.Code+"|"+issue.FieldName]++
	}
	for key, n := range counts {
		require.Equal(t, 1, n, "duplicate issue for %s", key)
	}
}

func TestValidateEmbeddedProjectionFields_PartialProjectionErrorSkipsChild(t *testing.T) {
	schema := loadEmbeddedValidationSchema(t)
	snapshot, root := buildEmbeddedValidationCase(t, schema, "notes/daily/today.md", 9)

	binding := root.Collections["actionItems"]
	binding.Items = append(binding.Items,
		CollectionItem{Ref: NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindSection, Fragment: "missing-999", NodeID: "nope"}},
		CollectionItem{Ref: NodeRef{NotePath: "notes/other.md", Kind: NodeKindSection, Fragment: "elsewhere", NodeID: "elsewhere"}},
	)
	root.Collections["actionItems"] = binding

	got := validateEmbeddedProjectionFields(snapshot, schema, root)
	requireEmbeddedFixtureIssues(t, got, snapshot.NotePath, 9)
	for _, issue := range got {
		require.NotContains(t, issue.NodeRef, "notes/other.md")
		require.NotContains(t, issue.NodeRef, "missing-999")
	}
}

// TestValidateEmbeddedProjectionFields_ResolverConstructedOnce guards the
// operation-local resolver: allocations must stay linear in node count.
// Measured at 500 nodes: 45,614 allocations per run (~91 per node), against
// 928,270 (~1,857 per node) when every child built its own resolver. The
// absolute bound carries roughly 2x headroom; the 500->2000 ratio is the
// structural guard, since per-child resolvers made it quadratic (~4x linear).
func TestValidateEmbeddedProjectionFields_ResolverConstructedOnce(t *testing.T) {
	schema := loadEmbeddedValidationSchema(t)
	allocsAt := func(count int) float64 {
		snapshot, root := buildEmbeddedValidationCase(t, schema, "notes/scratch.md", count)
		allocs := testing.AllocsPerRun(1, func() {
			embeddedValidationSink = validateEmbeddedProjectionFields(snapshot, schema, root)
		})
		require.NotEmpty(t, embeddedValidationSink)
		return allocs
	}
	small := allocsAt(500)
	require.Less(t, small, float64(200*500), "allocations per validation run at 500 nodes")
	large := allocsAt(2000)
	require.Less(t, large/small, 6.0, "allocation growth from 500 to 2000 nodes must stay near linear")
}

// BenchmarkEmbeddedValidationResolverScaling isolates the traversal from schema
// compilation, Markdown parsing, and the root projection.
func BenchmarkEmbeddedValidationResolverScaling(b *testing.B) {
	schema := loadEmbeddedValidationSchema(b)
	for _, count := range []int{100, 500, 2000} {
		b.Run(fmt.Sprintf("nodes=%d", count), func(b *testing.B) {
			snapshot, root := buildEmbeddedValidationCase(b, schema, "notes/scratch.md", count)
			issues := validateEmbeddedProjectionFields(snapshot, schema, root)
			if want := count / 2; len(issues) < want {
				b.Fatalf("got %d issues, want at least %d", len(issues), want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				embeddedValidationSink = validateEmbeddedProjectionFields(snapshot, schema, root)
			}
		})
	}
}
