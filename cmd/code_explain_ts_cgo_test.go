//go:build cgo

package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestCodeExplain_TSReindexPreservesAliasImportResolution(t *testing.T) {
	ctx := context.Background()
	vaultDir := t.TempDir()

	// Enable code index for this vault path (so explain can run).
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n"), 0o644))

	// Minimal TS app.
	tsRoot := filepath.Join(vaultDir, "tsapp")
	require.NoError(t, os.MkdirAll(filepath.Join(tsRoot, "src", "components"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tsRoot, "src", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tsRoot, "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	componentPath := filepath.Join(tsRoot, "src", "components", "SomeComponent.tsx")
	appPath := filepath.Join(tsRoot, "src", "app", "App.tsx")

	require.NoError(t, os.WriteFile(componentPath, []byte(`export function SomeComponent(){ return <div /> }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import { SomeComponent } from "@/src/components/SomeComponent";
export function App() { return <SomeComponent />; }
`+"\n"), 0o644))

	// Seed an initial index with a pre-populated tail index so alias imports resolve.
	dbPath := codeanchor.DefaultIndexPath(vaultDir)
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)

	tail := codeanchor.NewPathTailIndex(5)
	tail.Add(componentPath)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{
			codeanchor.NewTSIndexerWithRootAndTailIndex(vaultDir, tail),
		},
		codeanchor.WithBasePath(vaultDir),
		codeanchor.WithPathTailIndex(tail),
		codeanchor.WithoutWarmCache(),
	)

	compBytes, err := os.ReadFile(componentPath)
	require.NoError(t, err)
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, componentPath, compBytes))

	appBytes, err := os.ReadFile(appPath)
	require.NoError(t, err)
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, appPath, appBytes))

	require.NoError(t, store.Close())

	// Mutate the app file to force a reindex during explain.
	require.NoError(t, os.WriteFile(appPath, append(appBytes, []byte("\n// changed\n")...), 0o644))

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origVault := vaultName
	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		vaultName = origVault
		codeExplainJSON = false
		codeAnchorsListJSON = false
		codeAnchorsListLimit = 0
		codeAnchorsMatchJSON = false
		codeAnchorsMatchKind = ""
		codeAnchorsMatchValue = ""
		codeAnchorsMatchLimit = 0
	})

	rootCmd.SetArgs([]string{"code", "anchors", "explain", appPath, "--vault", vaultDir, "--json"})
	require.NoError(t, rootCmd.Execute())
	rootCmd.SetArgs([]string{})

	// Validate the persisted call-site uses the resolved module pkg (not the alias literal).
	store2, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store2.Close() })

	vaultPaths, err := paths.NewVaultPaths(vaultDir)
	require.NoError(t, err)

	relApp, err := vaultPaths.RelCodeStrict(appPath)
	require.NoError(t, err)
	calls, err := store2.CallsFromFile(ctx, relApp.String())
	require.NoError(t, err)
	found := false
	for _, cs := range calls {
		if cs.CalleeSymbol.Name == "SomeComponent" {
			require.Equal(t, "@polyglot/tsapp/src/components/SomeComponent", cs.CalleeSymbol.Pkg)
			found = true
		}
	}
	require.True(t, found, "expected a SomeComponent call-site after explain reindex")
}
