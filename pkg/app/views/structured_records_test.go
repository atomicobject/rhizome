package views

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/internal/testkit/recordfixture"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestStandardViewsReadStructuredValuesAndPresence(t *testing.T) {
	files := recordfixture.Files()
	files[".rhizome/ontology/schema.graphql"] = recordfixture.SDL
	files[".rhizome/source-ownership.yml"] = "sources:\n  - owner: Fixture author\n    reason: Change this with the owning tool.\n    paths: [model/**]\n"
	root := testkit.Vault(t, files)
	vault := obsidian.VaultDefinition{Root: root, Includes: recordfixture.Includes}
	store, err := semdb.Open(filepath.Join(root, ".rhizome/db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	metadata := testkit.NoteMetadataIndexer(t)
	runtime, err := ontologytest.Publish(t.Context(), metadata, vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	formats, err := metadata.FormatRuntime()
	require.NoError(t, err)
	service := New(ServiceOptions{VaultPath: root, VaultDef: vault, NoteReader: &obsidian.Note{}, Store: store, Schema: runtime.Schema, QueryDeps: ontologyquery.Deps{VaultDef: vault, Store: store, NoteReader: &obsidian.Note{}, NoteFormats: formats}, Views: []viewconfig.ViewDefinition{{
		APIVersion: viewconfig.APIVersion, ID: "services", Name: "Services", SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Service"}, Mount: viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone}, Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}, {Field: "id"}, {Field: "empty"}, {Field: "refs"}}}},
	}}})
	for _, request := range []ExecuteRequest{
		{},
		{Filters: []viewconfig.FilterSpec{{Field: "refs", Op: "exists"}}},
	} {
		result, err := service.Execute(t.Context(), "services", request)
		require.NoError(t, err)
		require.Len(t, result.Rows, 2)
		require.NotEqual(t, result.Rows[0].Ref.NodeID, result.Rows[1].Ref.NodeID)
		for _, row := range result.Rows {
			require.Equal(t, "Shared title", row.Title)
			require.Contains(t, row.Path, "#record:")
			require.Equal(t, "Service", row.ResolvedType)
			require.Contains(t, row.ReadOnlyReason, "owning tool")
		}
	}
}
