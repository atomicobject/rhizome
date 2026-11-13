package indexcore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	sqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestParserConfigurationStampUpgradesWithoutReparseAndSurvivesFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, os.MkdirAll(filepath.Join(f.root, "src/pkg"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(f.root, "src/pkg/util.py"), []byte("def answer():\n    return 42\n"), 0600))
	configure := func(roots []string) {
		t.Helper()
		require.NoError(t, f.writer.Close())
		f.request.CodeConfig.Enabled = true
		f.request.CodeConfig.PythonRoots = roots
		f.service, _ = codeintel.NewCodeAnchorService(codeintel.CodeAnchorServiceConfig{VaultPath: f.root, CodeCfg: f.request.CodeConfig, Store: f.store, IncludeIndexers: true, WriteAccess: true})
		f.writer = indexwriter.New(ctx, indexcore.BindWriterHandlers(indexwriter.Handlers{}, f.service, f.store))
		t.Cleanup(func() { _ = f.writer.Close() })
	}
	configure([]string{"src"})
	// Simulate an index written before parser hashes were recorded.
	f.publish(t, f.request)
	f.request.ParserConfigHash = f.request.CodeConfig.ParserConfigHash()
	upgraded := f.publish(t, f.request)
	require.Zero(t, upgraded.Code.Indexed, "an absent parser stamp must not force a reparse")
	stored, present, err := f.store.GetMetadata(ctx, sqlite.MetaKeyParserConfigHash)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, f.request.ParserConfigHash, stored)
	configure([]string{"src/pkg"})
	f.request.ParserConfigHash = f.request.CodeConfig.ParserConfigHash()
	discovery, err := indexcore.Discover(ctx, f.request, f.store)
	require.NoError(t, err)
	_, err = indexcore.Publish(ctx, f.lease, f.request, discovery, f.service, f.store, f.writer, indexcore.PublishOptions{AfterPreparedOwnershipCommit: func(context.Context) error { return context.Canceled }})
	require.True(t, errors.Is(err, context.Canceled))
	retained, present, err := f.store.GetMetadata(ctx, sqlite.MetaKeyParserConfigHash)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, stored, retained, "a failed convergence must keep the previous parser stamp")
	recovered := f.publish(t, f.request)
	require.Equal(t, 1, recovered.Code.Indexed)
	stored, present, err = f.store.GetMetadata(ctx, sqlite.MetaKeyParserConfigHash)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, f.request.ParserConfigHash, stored)
	var fqn string
	require.NoError(t, f.store.DB().QueryRow(`SELECT fqn FROM symbols WHERE file='src/pkg/util.py' AND kind='func'`).Scan(&fqn))
	require.Equal(t, "util.answer", fqn)
	unchanged := f.publish(t, f.request)
	require.Zero(t, unchanged.Code.Indexed, "a successful convergence must not repeat the forced parse")
}
