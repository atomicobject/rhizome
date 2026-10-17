package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRunUnifiedCoreBuildsGoPackageRelationshipsWithEmbeddingsDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dispatchDir := filepath.Join(root, "internal", "dispatch")
	require.NoError(t, os.MkdirAll(dispatchDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.24\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dispatchDir, "queue.go"), []byte(`package dispatch
type Queue struct{}
func (*Queue) Drain() {}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dispatchDir, "worker.go"), []byte(`package dispatch
type Worker struct { Queue *Queue }
func (w *Worker) Sync() { w.Queue.Drain() }
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dispatchDir, "store.go"), []byte(`package dispatch
type OperationStore interface { Save(string) error }
type MemoryOperationStore struct{}
func (*MemoryOperationStore) Save(string) error { return nil }
`), 0o600))
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{
		Code:           repoconfig.LocalCodeConfig{Enabled: true, Go: &repoconfig.LocalCodeLangConfig{Roots: []string{"internal"}}},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))

	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Root: root}, NoteMetadata: testkit.NoteMetadataIndexer(t),
	}))
	store := openTouchParityStore(t, root)
	calls, err := store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{"example.com/app/internal/dispatch.Queue.Drain"})
	require.NoError(t, err)
	require.Contains(t, calls["example.com/app/internal/dispatch.Queue.Drain"], codeanchor.GoDerivedRelationship{
		Kind: codeanchor.GoRelationshipCalls, SourcePath: "internal/dispatch/worker.go",
		SourceFQN: "example.com/app/internal/dispatch.Worker.Sync", TargetFQN: "example.com/app/internal/dispatch.Queue.Drain",
	})
	implementers, err := store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipImplements, []string{"example.com/app/internal/dispatch.OperationStore"})
	require.NoError(t, err)
	require.Contains(t, implementers["example.com/app/internal/dispatch.OperationStore"], codeanchor.GoDerivedRelationship{
		Kind: codeanchor.GoRelationshipImplements, SourcePath: "internal/dispatch/store.go",
		SourceFQN: "example.com/app/internal/dispatch.MemoryOperationStore", TargetFQN: "example.com/app/internal/dispatch.OperationStore", PointerOnly: true,
	})

	require.NoError(t, os.WriteFile(filepath.Join(dispatchDir, "retry.go"), []byte(`package dispatch
func Retry(queue *Queue) { queue.Drain() }
`), 0o600))
	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Root: root}, NoteMetadata: testkit.NoteMetadataIndexer(t),
	}))
	calls, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{"example.com/app/internal/dispatch.Queue.Drain"})
	require.NoError(t, err)
	require.Contains(t, calls["example.com/app/internal/dispatch.Queue.Drain"], codeanchor.GoDerivedRelationship{
		Kind: codeanchor.GoRelationshipCalls, SourcePath: "internal/dispatch/retry.go",
		SourceFQN: "example.com/app/internal/dispatch.Retry", TargetFQN: "example.com/app/internal/dispatch.Queue.Drain",
	})

	require.NoError(t, os.WriteFile(filepath.Join(dispatchDir, "store.go"), []byte(`package dispatch
type OperationStore interface { Save(string) error }
type MemoryOperationStore struct{}
`), 0o600))
	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Root: root}, NoteMetadata: testkit.NoteMetadataIndexer(t),
	}))
	implementers, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipImplements, []string{"example.com/app/internal/dispatch.OperationStore"})
	require.NoError(t, err)
	require.Empty(t, implementers["example.com/app/internal/dispatch.OperationStore"])

	for _, name := range []string{"queue.go", "worker.go", "store.go", "retry.go"} {
		require.NoError(t, os.Rename(filepath.Join(dispatchDir, name), filepath.Join(root, name+".retired")))
	}
	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Root: root}, NoteMetadata: testkit.NoteMetadataIndexer(t),
	}))
	snapshots, err := store.GoPackageSnapshotsForPaths(ctx, []string{"internal/dispatch/store.go"})
	require.NoError(t, err)
	require.Empty(t, snapshots)
}
