package structured

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestProvidersReadStructureWithoutMutationCapabilities(t *testing.T) {
	for _, tc := range []struct {
		provider   Provider
		path, text string
	}{
		{NewYAML(), "model.yaml", "rows: [{id: A, empty: null}]\n"},
		{NewJSON(), "model.json", `{"rows":[{"id":"A","empty":null}]}`},
	} {
		d := tc.provider.Descriptor()
		registry, err := noteformat.NewRegistry(tc.provider)
		require.NoError(t, err)
		runtime, err := noteformat.NewRuntime(registry, tc.provider)
		require.NoError(t, err)
		source, err := noteformat.NewAuthoredSource(paths.NotePath(tc.path), d, []byte(tc.text), 0)
		require.NoError(t, err)
		projection, err := runtime.Project(source)
		require.NoError(t, err)
		require.Equal(t, noteformat.ProjectionStatusCurrent, projection.Status)
		require.ElementsMatch(t, []noteformat.Capability{noteformat.CapabilitySourceReading, noteformat.CapabilityStructuralReading}, d.Capabilities.Values())
		require.Equal(t, noteformat.OwnershipExplicitInclude, d.OwnershipPolicy)
		require.Len(t, projection.Facts.Structure.Owners, 5)
		require.Len(t, projection.Facts.Structure.Values, 5)
		require.Equal(t, "/rows/0/empty", projection.Facts.Structure.Owners[4].Address)
		require.Nil(t, projection.Facts.Structure.Owners[2].Markdown)
	}
}
