package views

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestSectionSummarySupportsNativeCardsWithoutScalarEditing(t *testing.T) {
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type Entry @node(paths: ["notes/*.md"]) {
  abstractText: Section @contains(level: H2, heading: "Summary") @display(role: SUMMARY)
}
`)
	writeSourceFixture(t, root, "notes/present.md", "# Present\n\n## Summary\n\nFirst paragraph.\n\nSecond paragraph.\n\n## Evidence\nUnrelated prose.\n")
	writeSourceFixture(t, root, "notes/empty.md", "# Empty\n\n## Summary\n\n## Evidence\nUnrelated prose.\n")
	writeSourceFixture(t, root, "notes/missing.md", "# Missing\n\n## Evidence\nUnrelated prose.\n")
	service, _ := indexTypeViewsFixture(t, root)

	response, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Entry"), ExecuteRequest{Variant: "card"})
	require.NoError(t, err)
	require.Empty(t, response.Warnings)
	require.Equal(t, "card", response.Variant)
	require.NotNil(t, response.Card)
	require.NotNil(t, response.Card.Preview)
	require.Equal(t, "abstractText", response.Card.Preview.Field)
	require.NotNil(t, response.Profile)
	require.Equal(t, "abstractText", response.Profile.SummaryField)

	summaries := make(map[string]any, len(response.Rows))
	for _, row := range response.Rows {
		summaries[row.Path] = row.Fields["abstractText"]
	}
	require.Equal(t, map[string]any{
		"notes/present.md": "First paragraph. Second paragraph.",
		"notes/empty.md":   "",
		"notes/missing.md": nil,
	}, summaries)

	var summaryCapabilities []FieldCapability
	for _, capability := range response.Capabilities {
		if capability.SemanticRole == "summary" {
			summaryCapabilities = append(summaryCapabilities, capability)
		}
	}
	require.Len(t, summaryCapabilities, 1)
	capability := summaryCapabilities[0]
	require.Equal(t, "abstractText", capability.Key)
	require.Equal(t, "abstractText", capability.CanonicalField)
	require.Equal(t, "string", capability.ValueKind)
	require.Nil(t, capability.Edit, "section text is edited through its section workspace")
	require.False(t, capability.Sortable, "display materialization must not claim scalar query semantics")
}
