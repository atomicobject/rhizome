package identifierreconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRepairAssemblyRevalidateCompleteSnapshotRejectsSchemaSourceAndInventoryDrift(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "a.md"), []byte("# A\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "zero.md"), []byte("plain\n"), 0o644))
	writeIdentifierRevalidationSchema(t, root, "type NoteDoc @node(paths: [\"docs/*.md\"]) { id: String @field }")
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	vaultDef := obsidian.VaultDefinition{Path: root}
	reader := &obsidian.Note{}
	formatRuntime, err := builtin.NewRuntime()
	require.NoError(t, err)
	metadata, err := notemeta.NewIndexer(formatRuntime)
	require.NoError(t, err)
	sources, err := metadata.BuildNoteSourceSnapshots(context.Background(), vaultDef, reader)
	require.NoError(t, err)
	assembly := &RepairAssembly{PlanFingerprint: "plan", SchemaHash: schema.Hash}
	for _, source := range sources {
		assembly.SourcePreconditions = append(assembly.SourcePreconditions, SourcePrecondition{
			NotePath: source.Path.String(), SourceHash: source.ContentHash,
		})
	}
	require.NoError(t, assembly.seal())

	require.NoError(t, assembly.RevalidateCompleteSnapshot(context.Background(), vaultDef))
	writeIdentifierRevalidationSchema(t, root, "type OtherDoc @node(paths: [\"docs/*.md\"]) { id: String @field }")
	require.ErrorContains(t, assembly.RevalidateCompleteSnapshot(context.Background(), vaultDef), "schema")
	writeIdentifierRevalidationSchema(t, root, "type NoteDoc @node(paths: [\"docs/*.md\"]) { id: String @field }")

	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "a.md"), []byte("# B\n"), 0o644))
	require.ErrorContains(t, assembly.RevalidateCompleteSnapshot(context.Background(), vaultDef), "source inventory")
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "a.md"), []byte("# A\n"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "new.md"), []byte("# New\n"), 0o644))
	require.ErrorContains(t, assembly.RevalidateCompleteSnapshot(context.Background(), vaultDef), "source inventory")
	require.NoError(t, os.Remove(filepath.Join(root, "docs", "new.md")))

	require.NoError(t, os.Remove(filepath.Join(root, "docs", "zero.md")))
	require.ErrorContains(t, assembly.RevalidateCompleteSnapshot(context.Background(), vaultDef), "source inventory")
}

func TestRepairAssemblyRevalidateCompleteSnapshotRejectsUnsealedAssemblyAndMissingDependencies(t *testing.T) {
	vaultDef := obsidian.VaultDefinition{Path: t.TempDir()}
	writeIdentifierRevalidationSchema(t, vaultDef.Path, "type NoteDoc @node(paths: [\"docs/*.md\"]) { id: String @field }")
	schema, err := ontology.LoadSchema(vaultDef.Path)
	require.NoError(t, err)
	require.Error(t, (&RepairAssembly{}).RevalidateCompleteSnapshot(context.Background(), vaultDef))
	assembly := &RepairAssembly{PlanFingerprint: "plan", SchemaHash: schema.Hash}
	require.NoError(t, assembly.seal())
	require.ErrorContains(t, assembly.RevalidateCompleteSnapshot(context.Background(), obsidian.VaultDefinition{}), "vault root")
}

func writeIdentifierRevalidationSchema(t *testing.T, root, schema string) {
	t.Helper()
	path := filepath.Join(root, ".rhizome", "ontology", "schema.graphql")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(schema), 0o644))
}
