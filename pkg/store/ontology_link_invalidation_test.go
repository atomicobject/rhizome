package store

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
)

func TestAliasRetirementDurablyInvalidatesOnlyDependentOntologyState(t *testing.T) {
	for _, operation := range []string{"replace aliases", "delete metadata", "retire owner", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "index.sqlite")
			store, err := Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			state := NoteMetadataState{NotesHash: "before", RawNotesHash: "before", LoadedAt: 1, Ready: true}
			target := NoteMetadataRow{Path: "people/b.md", ContentHash: "before"}
			require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
				State: state, Notes: []NoteMetadataRow{target, {Path: "tasks/linked.md"}, {Path: "tasks/independent.md"}},
				PropertyValues: []NotePropertyValueRow{{NotePath: target.Path, PropertyName: "aliases", Source: NotePropertySourceFrontmatter, ValueText: "SHARED", ValueNorm: "shared", ValueKind: NotePropertyValueString}},
			}))
			require.NoError(t, store.ApplyOntologyDelta(ctx, OntologyDelta{
				NoteStates: []OntologyNoteStateRow{{NotePath: "tasks/linked.md", InputFingerprint: "linked"}, {NotePath: "tasks/independent.md", InputFingerprint: "independent"}},
				ReadModels: []readmodel.NodeReadModel{{NotePaths: []string{"tasks/linked.md"}, LinkDependencies: []readmodel.LinkDependencyRow{{SourceNotePath: "tasks/linked.md", NodeID: "linked", FieldName: "assignee", TargetInput: "SHARED", TargetInputNorm: "shared"}}}},
			}))
			delta := NoteMetadataDelta{State: NoteMetadataState{NotesHash: "after", RawNotesHash: "after", LoadedAt: 2, Ready: true}}
			switch operation {
			case "replace aliases", "rollback":
				target.ContentHash = "after"
				delta.Notes = []NoteMetadataRow{target}
				if operation == "rollback" {
					_, err := store.db.ExecContext(ctx, `CREATE TRIGGER reject_alias_update BEFORE UPDATE ON notes BEGIN SELECT RAISE(ABORT, 'injected metadata failure'); END`)
					require.NoError(t, err)
					require.ErrorContains(t, store.ApplyNoteMetadataDelta(ctx, delta), "injected metadata failure")
				} else {
					require.NoError(t, store.ApplyNoteMetadataDelta(ctx, delta))
				}
			case "delete metadata":
				delta.DeletedPaths = []string{target.Path}
				require.NoError(t, store.ApplyNoteMetadataDelta(ctx, delta))
			case "retire owner":
				result, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{Path: target.Path, Target: OwnershipTargetUnowned}})
				require.NoError(t, err)
				require.Equal(t, []string{"tasks/linked.md"}, result.AffectedSourcePaths)
			}
			require.NoError(t, store.Close())
			store, err = Open(path)
			require.NoError(t, err)
			states, err := store.OntologyNoteStatesByPaths(ctx, []string{"tasks/linked.md", "tasks/independent.md"})
			require.NoError(t, err)
			require.Contains(t, states, "tasks/independent.md")
			if operation == "rollback" {
				require.Contains(t, states, "tasks/linked.md")
			} else {
				require.NotContains(t, states, "tasks/linked.md")
			}
			// Keep the dependency fact for the next structural plan rather than losing
			// the reason this unchanged source must be reassessed after restart.
			sources, err := store.OntologyNodeLinkDependencySources(ctx, nil, []string{"shared"})
			require.NoError(t, err)
			require.Equal(t, []string{"tasks/linked.md"}, sources)
		})
	}
}
