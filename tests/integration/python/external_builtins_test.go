//go:build integration
// +build integration

package integration

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestPythonExternalBuiltinsPersistThroughProductionBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	root := ws.CodePath("python-external")
	lease, acquired, err := indexlock.TryAcquireLease(ws.CodeRoot, indexlock.AcquireOptions{})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })

	store, err := codeanchorsqlite.Open(filepath.Join(ws.CodeRoot, ".rhizome", "python-external.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots([]string{"python-external"})},
		codeanchor.WithBasePath(ws.CodeRoot),
		codeanchor.WithWriteAccess(),
		codeanchor.WithoutWarmCache(),
	)
	result := fixture.IndexCodeRoot(t, ctx, lease, svc, ws.CodeRoot, root)
	require.Equal(t, 1, result.Indexed)

	path := "python-external/builtins.py"
	classifications, err := store.ExternalReferenceClassificationsForPath(ctx, path)
	require.NoError(t, err)
	require.Len(t, classifications, 1)
	require.Equal(t, "len", classifications[0].Target.SymbolPath)
	require.Equal(t, codeanchor.ExternalClassRuntimeGlobalBuiltin, classifications[0].Class)

	query, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{
		Ecosystem: codeanchor.ExternalEcosystemPython, Module: "builtins", SymbolPrefix: "len", Limit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, "resolved", query.Status)
	require.NotNil(t, query.Target)
	require.Equal(t, "len", query.Target.SymbolPath)
	require.Len(t, query.Calls, 1)
	require.Equal(t, path, query.Calls[0].Path)

	targets, err := store.ExternalTargets(ctx)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, "len", targets[0].SymbolPath)
	require.NotEqual(t, "print", targets[0].SymbolPath, "shadowed parameter must not become an external builtin")
}
