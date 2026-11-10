package serve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModuleReadinessReportsBothSchemaFailuresWithoutChangingSources(t *testing.T) {
	for _, tc := range []struct{ sdl, message string }{
		{"type Broken {", "authored ontology"},
		{"type artifact @node(paths: [\"lower/**/*.md\"]) { title: String }\ntype Artifact @node(paths: [\"upper/**/*.md\"]) { title: String }", `generated query root "artifact"`},
	} {
		t.Run(tc.message, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".rhizome/ontology")
			require.NoError(t, os.MkdirAll(dir, 0755))
			file := filepath.Join(dir, "existing.graphql")
			require.NoError(t, os.WriteFile(file, []byte(tc.sdl), 0644))
			// Schema validation is independent of module trust and GraphQL serving.
			err := moduleSchema(root)
			require.ErrorContains(t, err, tc.message)
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, tc.sdl, string(data))
		})
	}
}
