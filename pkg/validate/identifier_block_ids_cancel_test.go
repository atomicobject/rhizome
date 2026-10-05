package validate

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIdentifierBlockIDMigrationCanceledCapturedSnapshot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Item implements Section @node(locator: EMBEDDED) {
 id: ID! @field @identifier(preferred: true, derivedSuffix: "ITEM")
}
type Record @node(paths: ["*.md"]) {
 items: [Item!] @contains(level: H2)
}`,
		"one.md": "# One\n\n## Item\nid:: old.value\n^old-anchor\n",
		"ref.md": "# Ref\nSee [[one#^old-anchor]].\n",
	}))
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t)}
	snapshot, err := captureValidationRunSnapshot(context.Background(), runCtx)
	require.NoError(t, err)
	runCtx.sourceSnapshot = snapshot
	runCtx.NoteReader = snapshot.reader(runCtx.NoteReader)
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	runtime := &ontology.Runtime{Schema: schema}
	issues, fixes, err := RunIdentifierBlockIDMigration(context.Background(), runCtx, runtime)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Len(t, fixes, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	issues, fixes, err = RunIdentifierBlockIDMigration(ctx, runCtx, runtime)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, issues, "canceled validation must not return partial diagnostics")
	require.Empty(t, fixes, "canceled validation must not return executable repair authority")
}

func TestIdentifierBlockIDMigrationScopedSnapshotRetainsExternalReferences(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Item implements Section @node(locator: EMBEDDED) {
 id: ID! @field @identifier(preferred: true, derivedSuffix: "ITEM")
}
type Record @node(paths: ["*.md"]) {
 items: [Item!] @contains(level: H2)
}`,
		"one.md": "# One\n\n## Item\nid:: old.value\n^old-anchor\n",
		"ref.md": "# Ref\nSee [[one#^old-anchor]].\n",
	}))
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), postcheckPaths: map[string]struct{}{"one.md": {}}}
	snapshot, err := captureValidationRunSnapshot(context.Background(), runCtx)
	require.NoError(t, err)
	// The held save check only captured one.md; references still require the live reader.
	snapshot.sources = snapshot.sources[:0]
	snapshot.paths = []string{"one.md"}
	snapshot.sources = append(snapshot.sources, snapshot.byPath["one.md"])
	delete(snapshot.byPath, "ref.md")
	runCtx.sourceSnapshot = snapshot
	runCtx.NoteReader = snapshot.reader(runCtx.NoteReader)
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	issues, fixes, err := RunIdentifierBlockIDMigration(context.Background(), runCtx, &ontology.Runtime{Schema: schema})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Len(t, fixes, 1)
	var referenceFound bool
	for _, edit := range fixes[0].Edits {
		if edit.Kind == FixKindRewriteLinkGroup && edit.NotePath == "ref.md" {
			referenceFound = true
		}
	}
	require.True(t, referenceFound, "a scoped postcheck must retain repairs for external links")
	runCtx.NoteReader = snapshot.reader(failingIdentifierReferenceReader{NoteReader: &obsidian.Note{}})
	failed := runOntologyWithRuntime(context.Background(), runCtx, &ontology.Runtime{Schema: schema})
	require.False(t, failed.OK)
	require.Contains(t, failed.Error, "synthetic reference inventory failure")
	require.Empty(t, failed.Fixes, "a failed reference inventory cannot authorize a partial repair")
}

func TestIdentifierBlockIDMigrationCancelsSectionTraversal(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Item implements Section @node(locator: EMBEDDED) {
 id: ID! @field @identifier(preferred: true, derivedSuffix: "ITEM")
}
type Record @node(matches: ["tag:record"]) {
 items: [Item!] @contains(level: H2)
}`,
	}))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	var content strings.Builder
	content.WriteString("# Record #record\n")
	for index := 0; index < 3000; index++ {
		fmt.Fprintf(&content, "\n## Item %d\nid:: item.%d\nRepeated section content for a large identifier validation snapshot.\n", index, index)
	}
	runCtx := RunContext{NoteMetadata: testNoteMetadata(t), sourceSnapshot: &validationRunSnapshot{sources: []notemeta.NoteSourceSnapshot{{Path: paths.NotePath("large.md"), Format: noteformat.FormatID("markdown"), Content: content.String()}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	issues, fixes, err := RunIdentifierBlockIDMigration(ctx, runCtx, &ontology.Runtime{Schema: schema})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded, "the deadline must interrupt active traversal")
	require.Less(t, time.Since(started), 2*time.Second, "the job must yield between sections")
	require.Empty(t, issues)
	require.Empty(t, fixes)
}

type failingIdentifierReferenceReader struct{ obsidian.NoteReader }

func (r failingIdentifierReferenceReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, fmt.Errorf("synthetic reference inventory failure")
}

func (r failingIdentifierReferenceReader) GetNotesListContext(context.Context, obsidian.VaultDefinition) ([]string, error) {
	return nil, fmt.Errorf("synthetic reference inventory failure")
}
