//go:build integration
// +build integration

package integration

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestTSWorkspace_IndexRootPersistsModuleRelationships(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	workspaceRoot := ws.CodePath("ts-workspace")
	dbPath := filepath.Join(ws.CodeRoot, ".rhizome", "ts-workspace.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewTSIndexerWithRoot(ws.CodeRoot)},
		codeanchor.WithBasePath(ws.CodeRoot),
		codeanchor.WithWriteAccess(),
		codeanchor.WithoutWarmCache(),
	)

	result, err := codeintel.IndexRoot(ctx, svc, ws.CodeRoot, workspaceRoot, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Empty(t, result.Unsupported)

	wantFormats := []string{
		"ts-workspace/src/runtime.ts",
		"ts-workspace/src/view.tsx",
		"ts-workspace/src/lazy.mts",
		"ts-workspace/src/legacy.cts",
		"ts-workspace/src/plain.js",
		"ts-workspace/src/widget.jsx",
		"ts-workspace/src/native-esm.mjs",
		"ts-workspace/src/native-common.cjs",
	}
	sort.Strings(result.SeenPaths)
	for _, path := range wantFormats {
		require.Contains(t, result.SeenPaths, path, "IndexRoot must discover %s", filepath.Ext(path))
	}

	require.NoError(t, svc.RebuildAllCallEdges(ctx))
	edges, err := store.GraphDocCodeEdges(ctx)
	require.NoError(t, err)

	mainPath := "ts-workspace/src/main.ts"
	for _, target := range []string{
		"ts-workspace/src/runtime.ts",                // NodeNext .js -> .ts
		"ts-workspace/src/view.tsx",                  // NodeNext .js -> .tsx
		"ts-workspace/src/lazy.mts",                  // dynamic import .mjs -> .mts
		"ts-workspace/src/legacy.cts",                // require .cjs -> .cts
		"ts-workspace/src/plain.js",                  // JavaScript ESM
		"ts-workspace/src/widget.jsx",                // JSX ESM
		"ts-workspace/src/native-esm.mjs",            // native ESM
		"ts-workspace/src/native-common.cjs",         // native CommonJS
		"ts-workspace/src/setup.ts",                  // side-effect import
		"ts-workspace/src/ui/button.ts",              // tsconfig paths alias
		"ts-workspace/packages/library/src/index.ts", // workspace package exports
		"ts-workspace/src/barrel.ts",                 // barrel entry point
	} {
		require.True(t, hasPersistedCodeEdge(edges, mainPath, target),
			"expected persisted relationship from %s to %s; edges=%v", mainPath, target, edges)
	}

	require.True(t, hasPersistedCodeEdge(edges,
		"ts-workspace/src/barrel.ts",
		"ts-workspace/src/barrel-middle.ts",
	), "star barrel must persist its next-hop relationship; edges=%v", edges)
	require.True(t, hasPersistedCodeEdge(edges,
		"ts-workspace/src/barrel-middle.ts",
		"ts-workspace/src/barrel-target.ts",
	), "aliased barrel must persist its target relationship; edges=%v", edges)
	require.True(t, hasPersistedCodeEdge(edges,
		"ts-workspace/packages/library/src/index.ts",
		"ts-workspace/packages/library/src/internal.ts",
	), "package.json #imports must persist its target relationship; edges=%v", edges)
	require.True(t, hasPersistedCodeEdgeKind(edges,
		mainPath,
		"ts-workspace/packages/library/src/types.ts",
		"type_ref",
	), "type-only workspace import must resolve through the package barrel to its definition; edges=%v", edges)

	for _, target := range []string{
		"ts-workspace/src/runtime.ts",
		"ts-workspace/src/plain.js",
		"ts-workspace/packages/library/src/index.ts",
		"ts-workspace/src/ui/button.ts",
	} {
		require.True(t, hasPersistedCodeEdgeKind(edges, mainPath, target, "calls"),
			"expected representative persisted call edge from %s to %s; edges=%v", mainPath, target, edges)
	}

	require.False(t, hasPersistedCodeEdge(edges, mainPath, "ts-workspace/src/computed.ts"),
		"computed import/require sources must not invent module relationships; edges=%v", edges)
	require.False(t, hasPersistedCodeEdge(edges, mainPath, "ts-workspace/src/collision.ts"),
		"an unknown receiver's member name must not bind to an unrelated package-level symbol; edges=%v", edges)

	anchors, err := store.IntelAnchors(ctx)
	require.NoError(t, err)
	for _, want := range []struct {
		path string
		fqn  string
	}{
		{
			path: "ts-workspace/packages/library/src/index.ts",
			fqn:  "@fixture/library/src/index.libraryCall",
		},
		{
			path: "ts-workspace/packages/library/src/types.ts",
			fqn:  "@fixture/library/src/types.LibraryRecord",
		},
	} {
		require.True(t, hasPersistedAnchor(anchors, want.path, want.fqn),
			"expected persisted definition FQN %s at %s; anchors=%v", want.fqn, want.path, anchors)
	}
}

func hasPersistedCodeEdge(edges []codeanchorsqlite.GraphDocEdge, src, dst string) bool {
	for _, edge := range edges {
		if edge.SrcPath == src && edge.DstPath == dst {
			return true
		}
	}
	return false
}

func hasPersistedCodeEdgeKind(edges []codeanchorsqlite.GraphDocEdge, src, dst, kind string) bool {
	for _, edge := range edges {
		if edge.SrcPath == src && edge.DstPath == dst && edge.Kind == kind {
			return true
		}
	}
	return false
}

func hasPersistedAnchor(anchors []codeanchor.IntelAnchor, path, fqn string) bool {
	for _, anchor := range anchors {
		if anchor.Path == path && anchor.FQN == fqn {
			return true
		}
	}
	return false
}
