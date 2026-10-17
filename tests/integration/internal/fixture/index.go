//go:build integration

package fixture

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// IndexCodeRoot composes current ownership discovery and queued code intake for
// a fixture's custom language service. The caller owns the vault lease across
// this batch and any subsequent fixture writes.
func IndexCodeRoot(t testing.TB, ctx context.Context, lease *indexlock.Lease, service *codeanchor.Service, vaultRoot, codeRoot string) codeintel.IndexResult {
	t.Helper()
	require.NoError(t, lease.Validate(vaultRoot))
	vaultPaths, err := paths.NewVaultPaths(vaultRoot)
	require.NoError(t, err)
	scope, err := vaultPaths.RelStrict(codeRoot)
	require.NoError(t, err)
	metadata := testkit.NoteMetadataIndexer(t)
	formats, err := metadata.FormatRuntime()
	require.NoError(t, err)
	discovery, err := noteownership.DiscoverScoped(ctx, noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Root: vaultRoot},
		Registry:        formats.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(codeRoot)},
		CodeLanguage:    func(ref paths.CodePathRef) codeanchor.Lang { return codeintel.DetectCodeLang(ref.Abs.String()) },
	}, []paths.RelPath{scope})
	require.NoError(t, err)
	var candidates []codeintel.FileCandidate
	for _, candidate := range discovery.CodeCandidates() {
		candidates = append(candidates, codeintel.FileCandidate{AbsPath: candidate.AbsPath.String(),
			RelPath: candidate.Path.String(), ModTime: candidate.ModTime, Kind: codeintel.FileKindCode, Lang: candidate.Language})
	}
	writer := indexwriter.New(ctx, indexwriter.Handlers{ApplyCodeIndexBatch: service.ApplyCodeIndexBatch})
	defer func() { require.NoError(t, writer.Close()) }()
	result, err := codeintel.IndexCandidates(ctx, service, writer, vaultRoot, candidates, nil)
	require.NoError(t, err)
	return result
}
