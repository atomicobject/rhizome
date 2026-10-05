//go:build cgo

package codeanchor_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestRebuildAllCallEdgesPublishesPackageTypedRelationshipsFromCurrentVaultBytes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.24\n"), 0o644))
	dispatchDir := filepath.Join(root, "dispatch")
	require.NoError(t, os.MkdirAll(dispatchDir, 0o755))
	files := map[string]string{
		"queue.go": `package dispatch
type Queue struct{}
func (*Queue) Drain() {}
`,
		"worker.go": `package dispatch
type Worker struct { Queue *Queue }
func (w *Worker) Sync() { w.Queue.Drain() }
`,
		"store.go": `package dispatch
type OperationStore interface { Save(string) error }
type MemoryOperationStore struct{}
func (*MemoryOperationStore) Save(string) error { return nil }
`,
	}
	store, err := sqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()}, codeanchor.WithBasePath(root), codeanchor.WithWriteAccess(), codeanchor.WithoutWarmCache())
	for name, content := range files {
		absPath := filepath.Join(dispatchDir, name)
		require.NoError(t, os.WriteFile(absPath, []byte(content), 0o644))
		require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, absPath, []byte(content)))
	}

	require.NoError(t, svc.RebuildAllCallEdges(ctx))
	calls, err := store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{"example.com/app/dispatch.Queue.Drain"})
	require.NoError(t, err)
	require.Contains(t, calls["example.com/app/dispatch.Queue.Drain"], codeanchor.GoDerivedRelationship{
		Kind: codeanchor.GoRelationshipCalls, SourcePath: "dispatch/worker.go",
		SourceFQN: "example.com/app/dispatch.Worker.Sync", TargetFQN: "example.com/app/dispatch.Queue.Drain",
	})
	implementers, err := store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipImplements, []string{"example.com/app/dispatch.OperationStore"})
	require.NoError(t, err)
	require.Contains(t, implementers["example.com/app/dispatch.OperationStore"], codeanchor.GoDerivedRelationship{
		Kind: codeanchor.GoRelationshipImplements, SourcePath: "dispatch/store.go",
		SourceFQN: "example.com/app/dispatch.MemoryOperationStore", TargetFQN: "example.com/app/dispatch.OperationStore", PointerOnly: true,
	})
	drainIDs, err := store.IntelAnchorIDsByFQN(ctx, "example.com/app/dispatch.Queue.Drain", 2)
	require.NoError(t, err)
	require.Len(t, drainIDs, 1)
	callerAnchors, err := store.CallerAnchorsByCalleeIDs(ctx, drainIDs, 5)
	require.NoError(t, err)
	require.Equal(t, "example.com/app/dispatch.Worker.Sync", callerAnchors[drainIDs[0]][0].FQN)
	workerIDs, err := store.IntelAnchorIDsByFQN(ctx, "example.com/app/dispatch.Worker.Sync", 2)
	require.NoError(t, err)
	require.Len(t, workerIDs, 1)
	calleeAnchors, err := store.CallAnchorsByCallerIDs(ctx, workerIDs, 5, 2)
	require.NoError(t, err)
	require.Equal(t, "example.com/app/dispatch.Queue.Drain", calleeAnchors[workerIDs[0]][0].FQN)
	interfaceIDs, err := store.IntelAnchorIDsByFQN(ctx, "example.com/app/dispatch.OperationStore", 2)
	require.NoError(t, err)
	require.Len(t, interfaceIDs, 1)
	implementerAnchors, err := store.ImplementerAnchorsByTargetIDs(ctx, interfaceIDs, 5)
	require.NoError(t, err)
	require.Equal(t, "example.com/app/dispatch.MemoryOperationStore", implementerAnchors[interfaceIDs[0]][0].FQN)
}

func TestRebuildAllCallEdgesRejectsGoPackageSourceSymlinkOutsideVault(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	root := filepath.Join(parent, "vault")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dispatch"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.24\n"), 0o644))
	external := filepath.Join(parent, "outside.go")
	content := `package dispatch
type Store interface { Save() }
type Memory struct{}
func (*Memory) Save() {}
`
	require.NoError(t, os.WriteFile(external, []byte(content), 0o644))
	linked := filepath.Join(root, "dispatch", "outside.go")
	require.NoError(t, os.Symlink(external, linked))

	store, err := sqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()}, codeanchor.WithBasePath(root), codeanchor.WithWriteAccess(), codeanchor.WithoutWarmCache())
	digest := sha256.Sum256([]byte(content))
	key := codeanchor.GoPackageKey{ImportPath: "example.com/app/dispatch", Directory: "dispatch", PackageName: "dispatch", BuildVariant: codeanchor.CurrentGoBuildVariant()}
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{Summaries: []codeanchor.FileSummary{{
		FilePath: "dispatch/outside.go", Lang: codeanchor.LangGo, Hash: fmt.Sprintf("%x", digest), ParseStatus: codeanchor.ParseOK, GoPackage: &key,
	}}}))
	require.NoError(t, svc.RebuildAllCallEdges(ctx))

	visible, err := store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipImplements, []string{"example.com/app/dispatch.Store"})
	require.NoError(t, err)
	require.Empty(t, visible["example.com/app/dispatch.Store"])
}

func TestRebuildGoPackageRelationshipsHandlesIncrementalMethodPackageAndFileRemoval(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.24\n"), 0o644))
	store, err := sqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()}, codeanchor.WithBasePath(root), codeanchor.WithWriteAccess(), codeanchor.WithoutWarmCache())

	dispatchDir := filepath.Join(root, "dispatch")
	require.NoError(t, os.MkdirAll(dispatchDir, 0o755))
	queuePath := filepath.Join(dispatchDir, "queue.go")
	workerPath := filepath.Join(dispatchDir, "worker.go")
	queue := "package dispatch\ntype Queue struct{}\nfunc (*Queue) Drain() {}\n"
	workerWithCall := "package dispatch\ntype Worker struct{ Queue *Queue }\nfunc (w *Worker) Sync(){ w.Queue.Drain() }\n"
	workerWithoutCall := "package dispatch\ntype Worker struct{ Queue *Queue }\nfunc (w *Worker) Sync(){}\n"
	for path, content := range map[string]string{queuePath: queue, workerPath: workerWithCall} {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, path, []byte(content)))
	}
	require.NoError(t, svc.RebuildAllCallEdges(ctx))
	target := "example.com/app/dispatch.Queue.Drain"
	visible, err := store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{target})
	require.NoError(t, err)
	require.Len(t, visible[target], 1)
	require.NoError(t, os.WriteFile(workerPath, []byte(workerWithoutCall), 0o644))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, workerPath, []byte(workerWithoutCall)))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{workerPath}))
	visible, err = store.GoDerivedRelationshipsByTargets(ctx, codeanchor.GoRelationshipCalls, []string{target})
	require.NoError(t, err)
	require.Empty(t, visible[target], "removing only the method call must replace the prior package proof")

	soloDir := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(soloDir, 0o755))
	soloPath := filepath.Join(soloDir, "store.go")
	oldPackage := "package solo\ntype Store interface{ Save() }\ntype Memory struct{}\nfunc (*Memory) Save(){}\n"
	require.NoError(t, os.WriteFile(soloPath, []byte(oldPackage), 0o644))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, soloPath, []byte(oldPackage)))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{soloPath}))
	newPackage := "package renamed\ntype Store interface{ Save() }\ntype Memory struct{}\nfunc (*Memory) Save(){}\n"
	require.NoError(t, os.WriteFile(soloPath, []byte(newPackage), 0o644))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, soloPath, []byte(newPackage)))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{soloPath}))
	snapshots, err := store.GoPackageSnapshotsForPaths(ctx, []string{"solo/store.go"})
	require.NoError(t, err)
	require.Len(t, snapshots, 1, "the renamed package must atomically remove its old package state")
	require.Equal(t, "renamed", snapshots[0].Package.PackageName)

	require.NoError(t, store.DeleteFile(ctx, "solo/store.go"))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{soloPath}))
	snapshots, err = store.GoPackageSnapshotsForPaths(ctx, []string{"solo/store.go"})
	require.NoError(t, err)
	require.Empty(t, snapshots, "deleting the last package file must remove its relationship state")
}
