package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestStarterOntologySchemasExposeViewGroupingMetadata(t *testing.T) {
	t.Parallel()

	type expectedViewValue struct {
		label     string
		order     int
		collapsed bool
	}
	cases := []struct {
		name     string
		path     string
		enumName string
		values   map[string]expectedViewValue
	}{
		{
			name:     "spec-driven-spec-status",
			path:     filepath.Join("templates", "starters", templateAgenticEngineering, "rhizome", "ontology", "spec-driven.graphql"),
			enumName: "SpecStatus",
			values: map[string]expectedViewValue{
				"proposed":   {label: "Proposed", order: 10},
				"active":     {label: "Active", order: 20},
				"superseded": {label: "Superseded", order: 90, collapsed: true},
				"archived":   {label: "Archived", order: 100, collapsed: true},
			},
		},
		{
			name:     "spec-driven-effort-status",
			path:     filepath.Join("templates", "starters", templateAgenticEngineering, "rhizome", "ontology", "spec-driven.graphql"),
			enumName: "EffortStatus",
			values: map[string]expectedViewValue{
				"planned":  {label: "Planned", order: 10},
				"active":   {label: "Active", order: 20},
				"complete": {label: "Complete", order: 90, collapsed: true},
				"archived": {label: "Archived", order: 100, collapsed: true},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(tc.path)
			require.NoError(t, err)
			root := t.TempDir()
			dir := filepath.Join(root, ".rhizome", "ontology")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, filepath.Base(tc.path)), body, 0o644))

			schema, err := ontology.LoadSchema(root)
			require.NoError(t, err)
			enumType := schema.EnumTypes[tc.enumName]
			require.NotNil(t, enumType)

			byValue := map[string]*ontology.EnumValue{}
			for _, value := range enumType.Values {
				byValue[value.Name] = value
			}
			for valueName, expected := range tc.values {
				value := byValue[valueName]
				require.NotNil(t, value)
				require.Equal(t, expected.label, value.View.Label)
				require.Equal(t, expected.order, value.View.Order)
				require.Equal(t, expected.collapsed, value.View.Collapsed != nil && *value.View.Collapsed)
			}
		})
	}
}
