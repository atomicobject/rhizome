package operations_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

type driftingNamespaceRefresher struct {
	before    func()
	refresher validate.PostApplyRefresher
}

func (r driftingNamespaceRefresher) Refresh(ctx context.Context, lease *indexlock.Lease, changed []string, renamed []validate.PathRename, deleted []string) (validate.PostApplyRefreshResult, error) {
	r.before()
	return r.refresher.Refresh(ctx, lease, changed, renamed, deleted)
}

func TestNamespaceProjectionCatchesUpDependencyNeighbors(t *testing.T) {
	for _, phase := range []string{"apply", "recovery"} {
		for _, drift := range []string{"edit", "delete", "rename"} {
			t.Run(phase+"/"+drift, func(t *testing.T) {
				ctx := context.Background()
				root := t.TempDir()
				seedMoveEndpoint(t, root, ".rhizome/ontology/schema.graphql", "type ExampleNote @node(paths: [\"*.md\"]) { name: String! }\n")
				seedMoveEndpoint(t, root, "Old.md", "---\nname: Old\n---\nSee [[Neighbor]].\n")
				ref := seedMoveEndpoint(t, root, "Ref.md", "---\nname: Ref\n---\n[[Old]]\n")
				neighbor := seedMoveEndpoint(t, root, "Neighbor.md", "---\nname: Neighbor\n---\n")
				metadata := namespaceTestMetadata(t)
				projection := namespaceTestRefresher(t, root)
				initial, err := validationproduct.RefreshValidationProjection(ctx, validationproduct.ValidationProjectionRequest{
					VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: metadata, Target: validationproduct.ValidationProjectionLive,
				})
				require.NoError(t, err)
				require.NoError(t, initial.Close())

				edited := "---\nname: Edited elsewhere\n---\n"
				changeNeighbor := func() {
					switch drift {
					case "edit":
						require.NoError(t, os.WriteFile(neighbor, []byte(edited), 0o644))
					case "delete":
						require.NoError(t, os.Rename(neighbor, filepath.Join(t.TempDir(), "Neighbor.md")))
					case "rename":
						require.NoError(t, os.Rename(neighbor, filepath.Join(root, "Renamed.md")))
					}
				}
				var refresher validate.PostApplyRefresher = driftingNamespaceRefresher{before: changeNeighbor, refresher: projection}
				if phase == "recovery" {
					cancelCtx, cancel := context.WithCancel(ctx)
					defer cancel()
					ctx = cancelCtx
					refresher = cancellingNamespaceRefresher{cancel: cancel, refresher: projection}
				}
				result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					Context: ctx, NoteMetadata: metadata, PostApplyRefresher: refresher,
					Moves: []actions.MoveRequest{{Source: "Old.md", Target: "New.md"}}, UpdateBacklinks: true,
				})
				require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
				if phase == "recovery" {
					require.ErrorIs(t, err, context.Canceled)
					require.True(t, result.Mutation.Current.RecoveryPending)
					changeNeighbor()
					recovered, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
						NoteMetadata: metadata, PostApplyRefresher: projection,
						Moves: []actions.MoveRequest{{Source: "New.md", Target: "Next.md"}}, UpdateBacklinks: true,
					})
					require.ErrorContains(t, err, "replan")
					require.Equal(t, validate.NamespaceNotStarted, recovered.Mutation.Current.Decision)
					require.Len(t, recovered.Mutation.Recovered, 1)
					require.False(t, recovered.Mutation.Recovered[0].RecoveryPending, "recovery error: %v", err)
					require.NoFileExists(t, filepath.Join(root, "Next.md"))
				} else {
					require.NoError(t, err)
					require.False(t, result.Mutation.Current.RecoveryPending)
				}
				require.NoFileExists(t, filepath.Join(root, "Old.md"))
				require.Equal(t, "---\nname: Ref\n---\n[[New]]\n", readMoveEndpoint(t, ref))
				pending, err := validate.DetectPendingRepairJournals(validate.RunContext{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}})
				require.NoError(t, err)
				require.Empty(t, pending)
				store, err := sqlitefixture.Open(repoconfig.UnifiedIndexPath(root, ""))
				require.NoError(t, err)
				defer func() { require.NoError(t, store.Close()) }()
				rows, err := store.CurrentNoteMetadataRowsByPaths(context.Background(), []string{"Neighbor.md", "Renamed.md"})
				require.NoError(t, err)
				if drift == "edit" {
					require.Equal(t, int64(len(edited)), rows["Neighbor.md"].Size)
				} else {
					require.NotContains(t, rows, "Neighbor.md")
					if drift == "rename" {
						require.Contains(t, rows, "Renamed.md")
					}
				}
			})
		}
	}
}
