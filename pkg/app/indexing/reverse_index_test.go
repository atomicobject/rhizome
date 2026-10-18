package indexing

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

type reverseIndexStoreStub struct {
	indexerVersion string
	indexerOK      bool
	indexerErr     error

	reverseVersion string
	reverseOK      bool
	reverseErr     error

	backfillReady bool
	backfillOK    bool
	backfillErr   error

	backfillValues []bool
	versionValues  []string
}

func (s *reverseIndexStoreStub) IndexerVersion(ctx context.Context) (string, bool, error) {
	return s.indexerVersion, s.indexerOK, s.indexerErr
}

func (s *reverseIndexStoreStub) ReverseIndexVersion(ctx context.Context) (string, bool, error) {
	return s.reverseVersion, s.reverseOK, s.reverseErr
}

func (s *reverseIndexStoreStub) ReverseIndexBackfillComplete(ctx context.Context) (bool, bool, error) {
	return s.backfillReady, s.backfillOK, s.backfillErr
}

func (s *reverseIndexStoreStub) SetReverseIndexBackfillComplete(ctx context.Context, complete bool) error {
	s.backfillValues = append(s.backfillValues, complete)
	return nil
}

func (s *reverseIndexStoreStub) SetReverseIndexVersion(ctx context.Context, version string) error {
	s.versionValues = append(s.versionValues, version)
	return nil
}

func TestEnsureReverseIndexReadiness_NoResetWhenVersionsMatch(t *testing.T) {
	t.Parallel()

	store := &reverseIndexStoreStub{
		indexerVersion: codeanchor.IndexerVersion,
		indexerOK:      true,
		reverseVersion: codeanchor.ReverseIndexVersion,
		reverseOK:      true,
	}

	reset := reverseIndexNeedsRebuild(context.Background(), store, nil)
	require.False(t, reset)
	require.Empty(t, store.backfillValues)
	require.Empty(t, store.versionValues)
}

func TestEnsureReverseIndexReadiness_ResetsExternalTargetFoundationVersions(t *testing.T) {
	t.Parallel()

	store := &reverseIndexStoreStub{
		indexerVersion: "v1.9.0",
		indexerOK:      true,
		reverseVersion: "v1.1.0",
		reverseOK:      true,
	}

	reset := reverseIndexNeedsRebuild(context.Background(), store, nil)
	require.True(t, reset)
}

func TestEnsureReverseIndexReadiness_ResetsPreExtractionIndexerVersion(t *testing.T) {
	t.Parallel()

	store := &reverseIndexStoreStub{
		indexerVersion: "v1.10.0",
		indexerOK:      true,
		reverseVersion: codeanchor.ReverseIndexVersion,
		reverseOK:      true,
	}

	reset := reverseIndexNeedsRebuild(context.Background(), store, nil)
	require.True(t, reset)
}
