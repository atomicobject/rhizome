package indexcore_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/stretchr/testify/require"
)

func TestAutomaticScopePublishesWithDisabledLanguage(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.root, "ignored.py"), []byte("def ignored():\n    pass\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(f.root, "included.go"), []byte("package fixture\nfunc Included() {}\n"), 0600))
	f.request.CodeConfig.Enabled = true
	f.request.CodeConfig.AutomaticScope = true
	f.request.CodeConfig.GoRoots = []string{"."}
	f.request.CodeConfig.DisabledLanguages = []string{"python"}
	f.service, _ = codeintel.NewCodeAnchorService(codeintel.CodeAnchorServiceConfig{
		VaultPath: f.root, CodeCfg: f.request.CodeConfig, Store: f.store, IncludeIndexers: true, WriteAccess: true,
	})
	require.NoError(t, f.writer.Close())
	f.writer = indexwriter.New(context.Background(), indexcore.BindWriterHandlers(indexwriter.Handlers{}, f.service, f.store))
	t.Cleanup(func() { require.NoError(t, f.writer.Close()) })
	f.publish(t, f.request)
	indexed, err := f.store.IndexedFilePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"included.go"}, indexed)
}
