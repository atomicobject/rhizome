package actions_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type cancellingNamespaceRefresher struct {
	cancel    context.CancelFunc
	refresher validate.PostApplyRefresher
}

func (r cancellingNamespaceRefresher) Refresh(ctx context.Context, lease *validate.IndexLockLease, changed []string, renamed []validate.PathRename, deleted []string) (validate.PostApplyRefreshResult, error) {
	r.cancel()
	return r.refresher.Refresh(ctx, lease, changed, renamed, deleted)
}

func TestNamespaceCancellationAfterPublicationRetainsDecisionAndDefersOptionalEffects(t *testing.T) {
	for _, entrypoint := range []string{"rename", "move"} {
		t.Run(entrypoint, func(t *testing.T) {
			root := t.TempDir()
			seedMoveEndpoint(t, root, "Old.md", "[[Old]]\n")
			ref := seedMoveEndpoint(t, root, "Ref.md", "[[Old]]\n")
			code := seedMoveEndpoint(t, root, "refs.go", "package fixture\n// [[Old]]\n")
			metadata := namespaceTestMetadata(t)
			projection := namespaceTestRefresher(t, root)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			refresher := cancellingNamespaceRefresher{cancel: cancel, refresher: projection}
			config := coderefs.NewConfig(true, []string{"*.go"}, nil)
			uri := &mocks.MockUriManager{}
			var mutation validate.NamespaceMutationResult
			if entrypoint == "rename" {
				result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					Context: ctx, NoteMetadata: metadata, PostApplyRefresher: refresher,
					Source: "Old.md", Target: "New.md", UpdateBacklinks: true, CodeRefConfig: config,
				})
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, "New.md", result.RenamedPath)
				require.Equal(t, 2, result.LinkUpdates)
				require.Zero(t, result.CodeRefUpdates)
				mutation = result.Mutation
			} else {
				result, err := actions.MoveNotes(namespaceVault{path: root}, uri, actions.MoveParams{
					Context: ctx, NoteMetadata: metadata, PostApplyRefresher: refresher,
					Moves:           []actions.MoveRequest{{Source: "Old.md", Target: "New.md"}},
					UpdateBacklinks: true, CodeRefConfig: config, ShouldOpen: true,
				})
				require.ErrorIs(t, err, context.Canceled)
				require.Len(t, result.Results, 1)
				require.Equal(t, "New.md", result.Results[0].Target)
				require.Equal(t, 2, result.TotalLinkUpdates)
				require.Zero(t, result.TotalCodeRefUpdates)
				mutation = result.Mutation
			}
			require.Equal(t, validate.NamespaceCommitted, mutation.Current.Decision)
			require.True(t, mutation.Current.RecoveryPending)
			require.NotEmpty(t, mutation.Current.TransactionID)
			require.NotEmpty(t, mutation.Current.ReceiptPath)
			require.NoFileExists(t, filepath.Join(root, "Old.md"))
			require.Equal(t, "[[New]]\n", readMoveEndpoint(t, filepath.Join(root, "New.md")))
			require.Equal(t, "[[New]]\n", readMoveEndpoint(t, ref))
			require.Equal(t, "package fixture\n// [[Old]]\n", readMoveEndpoint(t, code))
			uri.AssertNotCalled(t, "Construct")
			uri.AssertNotCalled(t, "Execute")
			// A new call first converges the earlier committed request. It does
			// not publish this request or replay earlier optional coderef work.
			recovered, err := actions.MoveNotes(namespaceVault{path: root}, uri, actions.MoveParams{
				NoteMetadata: metadata, PostApplyRefresher: projection,
				Moves:           []actions.MoveRequest{{Source: "New.md", Target: "Next.md"}},
				UpdateBacklinks: true, CodeRefConfig: config, ShouldOpen: true,
			})
			require.Error(t, err)
			require.Equal(t, validate.NamespaceNotStarted, recovered.Mutation.Current.Decision)
			require.Empty(t, recovered.Results)
			require.Len(t, recovered.Mutation.Recovered, 1)
			require.Equal(t, mutation.Current.TransactionID, recovered.Mutation.Recovered[0].TransactionID)
			require.False(t, recovered.Mutation.Recovered[0].RecoveryPending, "recovery error: %v", err)
			require.NoFileExists(t, filepath.Join(root, "Next.md"))
			require.Equal(t, "package fixture\n// [[Old]]\n", readMoveEndpoint(t, code))
			uri.AssertNotCalled(t, "Construct")
			uri.AssertNotCalled(t, "Execute")
		})
	}
}

func TestNamespaceOpenFailureRetainsCommittedMove(t *testing.T) {
	root := t.TempDir()
	seedMoveEndpoint(t, root, "Old.md", "[[Old]]\n")
	ref := seedMoveEndpoint(t, root, "Ref.md", "[[Old]]\n")
	openErr := errors.New("synthetic URI failure")
	uri := &mocks.MockUriManager{}
	uri.On("Construct", "obsidian://open", map[string]string{"file": "New.md", "vault": "fixture"}).Return("obsidian://open?fixture")
	uri.On("Execute", "obsidian://open?fixture").Return(openErr)
	result, err := actions.MoveNotes(moveStubVault{path: root, name: "fixture"}, uri, actions.MoveParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
		Moves: []actions.MoveRequest{{Source: "Old.md", Target: "New.md"}}, UpdateBacklinks: true, ShouldOpen: true,
	})
	require.ErrorIs(t, err, openErr)
	require.ErrorContains(t, err, "open moved note")
	require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
	require.False(t, result.Mutation.Current.RecoveryPending)
	require.Len(t, result.Results, 1)
	require.Equal(t, "New.md", result.Results[0].Target)
	require.Equal(t, 2, result.TotalLinkUpdates)
	require.NoFileExists(t, filepath.Join(root, "Old.md"))
	require.Equal(t, "[[New]]\n", readMoveEndpoint(t, filepath.Join(root, "New.md")))
	require.Equal(t, "[[New]]\n", readMoveEndpoint(t, ref))
	uri.AssertExpectations(t)
}

func TestNamespaceCaseOnlyCommittedRecoveryConvergesRealProjection(t *testing.T) {
	for _, entrypoint := range []string{"rename", "move"} {
		for _, warm := range []bool{false, true} {
			t.Run(entrypoint+"/"+map[bool]string{false: "cold", true: "warm"}[warm], func(t *testing.T) {
				root := t.TempDir()
				source := seedMoveEndpoint(t, root, "Old.md", "[[Old]]\n")
				original, err := os.Stat(source)
				require.NoError(t, err)
				alias, err := os.Stat(filepath.Join(root, "old.md"))
				if errors.Is(err, os.ErrNotExist) {
					t.Skip("requires a case-folding filesystem")
				}
				require.NoError(t, err)
				require.True(t, os.SameFile(original, alias))
				ref := seedMoveEndpoint(t, root, "Ref.md", "[[Old]]\n")
				metadata := namespaceTestMetadata(t)
				projection := namespaceTestRefresher(t, root)
				if warm {
					indexed, err := indexing.RefreshValidationProjection(context.Background(), indexing.ValidationProjectionRequest{
						VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root},
						NoteMetadata: metadata, NoteReader: &obsidian.Note{}, Target: indexing.ValidationProjectionLive,
					})
					require.NoError(t, err)
					require.NoError(t, indexed.Close())
					store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
					require.NoError(t, err)
					projected, readErr := store.CurrentNoteMetadataPaths(context.Background())
					require.NoError(t, store.Close())
					require.NoError(t, readErr)
					require.ElementsMatch(t, []string{"Old.md", "Ref.md"}, projected)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				refresher := cancellingNamespaceRefresher{cancel: cancel, refresher: projection}
				var mutation validate.NamespaceMutationResult
				if entrypoint == "rename" {
					result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
						Context: ctx, NoteMetadata: metadata, PostApplyRefresher: refresher,
						Source: "Old.md", Target: "old.md", UpdateBacklinks: true,
					})
					require.ErrorIs(t, err, context.Canceled)
					require.Equal(t, "old.md", result.RenamedPath)
					mutation = result.Mutation
				} else {
					result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
						Context: ctx, NoteMetadata: metadata, PostApplyRefresher: refresher,
						Moves: []actions.MoveRequest{{Source: "Old.md", Target: "old.md"}}, UpdateBacklinks: true,
					})
					require.ErrorIs(t, err, context.Canceled)
					mutation = result.Mutation
				}
				require.Equal(t, validate.NamespaceCommitted, mutation.Current.Decision)
				require.True(t, mutation.Current.RecoveryPending)
				recovered, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: metadata, PostApplyRefresher: projection,
					Moves: []actions.MoveRequest{{Source: "old.md", Target: "Next.md"}}, UpdateBacklinks: true,
				})
				require.ErrorContains(t, err, "replan")
				require.Equal(t, validate.NamespaceNotStarted, recovered.Mutation.Current.Decision)
				require.Len(t, recovered.Mutation.Recovered, 1)
				require.Equal(t, validate.NamespaceCommitted, recovered.Mutation.Recovered[0].Decision)
				require.False(t, recovered.Mutation.Recovered[0].RecoveryPending, "recovery error: %v", err)
				require.NoFileExists(t, filepath.Join(root, "Next.md"))
				require.Equal(t, "[[old]]\n", readMoveEndpoint(t, filepath.Join(root, "old.md")))
				require.Equal(t, "[[old]]\n", readMoveEndpoint(t, ref))
				store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
				require.NoError(t, err)
				projected, readErr := store.CurrentNoteMetadataPaths(context.Background())
				require.NoError(t, store.Close())
				require.NoError(t, readErr)
				require.ElementsMatch(t, []string{"old.md", "Ref.md"}, projected)
				completed, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: metadata, PostApplyRefresher: projection,
					Moves: []actions.MoveRequest{{Source: "old.md", Target: "Next.md"}}, UpdateBacklinks: true,
				})
				require.NoError(t, err)
				require.Equal(t, validate.NamespaceCommitted, completed.Mutation.Current.Decision)
				require.False(t, completed.Mutation.Current.RecoveryPending)
				require.Equal(t, "[[Next]]\n", readMoveEndpoint(t, filepath.Join(root, "Next.md")))
				require.Equal(t, "[[Next]]\n", readMoveEndpoint(t, ref))
			})
		}
	}
}
