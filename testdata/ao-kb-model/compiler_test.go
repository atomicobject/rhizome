package aokbmodel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

func fixtureSchema(t *testing.T) *schema.Schema {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, ".rhizome/ontology")
	require.NoError(t, os.MkdirAll(target, 0755))
	sdl, err := os.ReadFile("expected.graphql")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(target, "ao-model.generated.graphql"), sdl, 0644))
	compiled, err := schema.LoadSchema(root)
	require.NoError(t, err)
	return compiled
}

func TestGeneratedSDLCompiles(t *testing.T) {
	compiled := fixtureSchema(t)
	require.Len(t, compiled.Types, 19)
	shared := compiled.Types["AOKBModel_service_account"]
	require.NotNil(t, shared)
	require.Equal(t, "alpha", shared.Record.Membership.Includes)
	require.False(t, shared.Record.Membership.IncludeMissing)
	require.Equal(t, 0, shared.Record.SourceOrder)
	product := compiled.Types["AOKBModel_service_product"]
	require.Equal(t, 1, product.Record.SourceOrder)
	require.NotNil(t, product.ByName["v_project_5f_detail"])
	require.NotNil(t, product.ByName["v_status"])
	require.Equal(t, "AOKBModel_service_account", compiled.Types["AOKBModel_job_account"].Record.Container)
	require.Equal(t, "AOKBModel_service", product.ByName["v_uses"].Record.References.To[0])
	require.True(t, compiled.Types["AOKBModel_map_product"].Record.Key.MappingKey)
	require.True(t, compiled.Types["AOKBModel_position_product"].Record.Key.Positional)
	require.Equal(t, "TYPED", compiled.Types["AOKBModel_policy_product"].ByName["v_governs"].Record.References.Encoding)
}
