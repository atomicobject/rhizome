package serve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModuleReadinessReportsBothSchemaFailuresWithoutChangingSources(t *testing.T) {
	for _, sdl := range []string{"type Broken {", "type artifact @node { title: String }\ntype Artifact @node { title: String }"} {
		t.Run(sdl, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".rhizome/ontology")
			require.NoError(t, os.MkdirAll(dir, 0755))
			file := filepath.Join(dir, "existing.graphql")
			require.NoError(t, os.WriteFile(file, []byte(sdl), 0644))
			// Schema validation is independent of module trust and GraphQL serving.
			err := moduleSchema(root)
			require.Error(t, err)
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, sdl, string(data))
		})
	}
}
