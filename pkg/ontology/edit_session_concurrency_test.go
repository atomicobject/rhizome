package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestConcurrentEditSessionsDoNotLoseTheWinningField(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Spec @node(paths: ["notes/*.md"]) { summary: String }`)
	writeOntologyNote(t, root, "notes/spec.md", "---\nsummary: Original\n---\nBody\n")
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	sessions := make([]*EditSession, 2)
	for index, value := range []string{"First", "Second"} {
		session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
		require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "summary", value))
		_, err := session.Preview(t.Context())
		require.NoError(t, err)
		sessions[index] = session
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	type outcome struct {
		result CommitResult
		err    error
	}
	results := make(chan outcome, 2)
	for _, session := range sessions {
		go func() { <-start; result, err := session.Commit(ctx); results <- outcome{result, err} }()
	}
	close(start)
	committed, conflicted := 0, 0
	for range 2 {
		next := <-results
		require.NoError(t, next.err)
		switch next.result.Outcome {
		case CommitOutcomeCommitted:
			committed++
		case CommitOutcomeConflicted:
			conflicted++
			require.Len(t, next.result.Conflicts, 1)
			require.Equal(t, ConflictKindFieldChanged, next.result.Conflicts[0].Kind)
		default:
			t.Fatalf("unexpected outcome: %+v", next.result)
		}
	}
	require.Equal(t, 1, committed)
	require.Equal(t, 1, conflicted)
	actual, err := os.ReadFile(filepath.Join(root, "notes/spec.md"))
	require.NoError(t, err)
	require.Contains(t, []string{"---\nsummary: First\n---\nBody\n", "---\nsummary: Second\n---\nBody\n"}, string(actual))
}

func TestEditCommitRejectsForeignAndReleasedWriterLeases(t *testing.T) {
	for _, name := range []string{"foreign vault", "released token"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologyNote(t, root, "note.md", "Original\n")
			leaseRoot := root
			if name == "foreign vault" {
				leaseRoot = t.TempDir()
			}
			lease, acquired, err := indexlock.TryAcquireLease(leaseRoot, indexlock.AcquireOptions{Role: "test"})
			require.NoError(t, err)
			require.True(t, acquired)
			t.Cleanup(func() { _ = lease.Release() })
			if name == "released token" {
				require.NoError(t, lease.Release())
			}
			session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, nil)
			require.NoError(t, session.RewriteFile("note.md", "Replacement\n"))
			result, err := session.CommitWithOptions(t.Context(), CommitOptions{WriteLease: lease})
			require.Error(t, err)
			require.Equal(t, CommitOutcomeFailed, result.Outcome)
			actual, err := os.ReadFile(filepath.Join(root, "note.md"))
			require.NoError(t, err)
			require.Equal(t, "Original\n", string(actual))
			if name == "foreign vault" {
				require.NoError(t, lease.Validate(leaseRoot), "the consumer must not release its caller's lease")
			}
		})
	}
}
