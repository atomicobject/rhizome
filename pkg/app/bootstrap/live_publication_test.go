package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/ontology"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestPublishCurrentProjectionMakesAuthoredChangesReadable(t *testing.T) {
	for _, installedLane := range []bool{false, true} {
		name := "one-shot lane"
		if installedLane {
			name = "runtime lane"
		}
		t.Run(name, func(t *testing.T) {
			rt := newProjectionPublicationRuntime(t)
			if installedLane {
				indexLane := lane.New(lane.Options{LockPath: repoconfig.IndexLockPath(rt.VaultPath)})
				rt.SetLane(indexLane)
			}
			before, err := ontology.PublishedRuntimeWithStore(t.Context(), rt.NoteMetadataIndexer(), rt.VaultDef, rt.IntelStore())
			require.NoError(t, err)
			require.False(t, before.Ready)
			require.NoError(t, rt.PublishCurrentProjection(t.Context()))
			assertPublishedProjectName(t, rt, "First")
			codePaths, err := rt.IntelStore().IndexedFilePaths(t.Context())
			require.NoError(t, err)
			require.Contains(t, codePaths, "main.go", "structural publication must keep the code obligations it acknowledges")

			require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, "note.md"), []byte("---\nname: Second\n---\n# Project\n"), 0o644))
			require.NoError(t, rt.PublishCurrentProjection(t.Context()))
			assertPublishedProjectName(t, rt, "Second")
			lease, acquired, err := indexlock.TryAcquireLease(rt.VaultPath, indexlock.AcquireOptions{})
			require.NoError(t, err)
			require.True(t, acquired, "publication must release the writer before returning to its reader")
			require.NoError(t, lease.Release())
		})
	}
}

func TestPublishCurrentProjectionCancellationPreservesOtherWriter(t *testing.T) {
	rt := newProjectionPublicationRuntime(t)
	lease, acquired, err := indexlock.TryAcquireLease(rt.VaultPath, indexlock.AcquireOptions{})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, rt.PublishCurrentProjection(ctx), context.DeadlineExceeded)
	require.NoError(t, lease.Validate(rt.VaultPath), "the existing writer remains caller-owned")
	state, err := rt.IntelStore().GetNoteMetadataState(t.Context())
	require.NoError(t, err)
	require.False(t, state.Ready, "waiting for another writer must publish no metadata")
}

func TestPublishCurrentProjectionRejectsReadOnlyRuntimeBeforeWriterSetup(t *testing.T) {
	root := t.TempDir()
	rt := &LiveRuntime{VaultPath: root, readOnlyCodeIndex: true}
	require.ErrorContains(t, rt.PublishCurrentProjection(t.Context()), "requires a writable runtime")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}

func newProjectionPublicationRuntime(t *testing.T) *LiveRuntime {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\ncode:\n  enabled: true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte("type Project @node(paths: [\"note.md\"]) { name: String! }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("---\nname: First\n---\n# Project\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))
	ctx := vaultconfig.WithEnv(t.Context(), testkit.Env(t))
	rt, err := NewLiveRuntime(ctx, LiveOptions{
		VaultName: root, DisableLeaderWork: true, DisableWatchHub: true,
		DisableSessionStore: true, SkipCacheWarmup: true,
		Requirements: RequireRuntimeCapabilities(RuntimeCapabilityCodeIndex),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	require.NoError(t, rt.WaitForCodeIndex(ctx))
	return rt
}

func assertPublishedProjectName(t *testing.T, rt *LiveRuntime, expected string) {
	t.Helper()
	publication, err := ontology.PublishedRuntimeWithStore(t.Context(), rt.NoteMetadataIndexer(), rt.VaultDef, rt.IntelStore())
	require.NoError(t, err)
	require.True(t, publication.Ready)
	nodes, err := publication.Store.OntologyNodesByPaths(t.Context(), []string{"note.md"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	values, err := publication.Store.OntologyNodeFieldValuesByNodeIDs(t.Context(), []string{nodes[0].NodeID}, []string{"name"})
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.Equal(t, expected, values[0].ValueText)
}
