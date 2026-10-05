package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestHydrateContentDoesNotFabricateRecordForUnindexedFile(t *testing.T) {
	vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/*.md"]) { summary: String }`, "# Product\n")
	writeFixtureNote(t, vault.Path, "unindexed.md", "# File not indexed\n")
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vault, reader, store, schema).NewScope(context.Background(), ScopeOptions{})
	records, err := scope.Hydrate(context.Background(), []ontology.NodeRef{{NotePath: "unindexed.md", Kind: ontology.NodeKindNote}}, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Empty(t, records, "source content alone cannot fabricate a record without indexed identity")
	require.Zero(t, reader.contents)
}
