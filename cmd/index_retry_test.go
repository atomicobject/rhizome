package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunIndexWithRebuildGuidance_RequiresExplicitRebuildOnSQLiteFailure(t *testing.T) {
	t.Parallel()

	var runs int
	var out bytes.Buffer

	err := runIndexWithRebuildGuidance(&out, false, func() error {
		runs++
		if runs == 1 {
			return errors.New("SQL logic error: no such column: foo")
		}
		return nil
	})

	require.ErrorContains(t, err, "rerun with --rebuild")
	require.Equal(t, 1, runs)
	require.NotContains(t, out.String(), "deleting index database")
}

func TestRunIndexWithRebuildGuidance_RequiresExplicitRebuildOnRebuildableUniqueConstraint(t *testing.T) {
	t.Parallel()

	var runs int
	var out bytes.Buffer

	err := runIndexWithRebuildGuidance(&out, false, func() error {
		runs++
		if runs == 1 {
			return errors.New("semantic index: persist ontology node embeddings: UNIQUE constraint failed: intel_chunks.owner_type, intel_chunks.owner_id, intel_chunks.ord, intel_chunks.granularity")
		}
		return nil
	})

	require.ErrorContains(t, err, "rerun with --rebuild")
	require.Equal(t, 1, runs)
	require.NotContains(t, out.String(), "deleting index database")
}

func TestRunIndexWithRebuildGuidance_ReturnsLockedDatabaseError(t *testing.T) {
	t.Parallel()

	var runs int
	err := runIndexWithRebuildGuidance(bytes.NewBuffer(nil), false, func() error {
		runs++
		return errors.New("database is locked")
	})

	require.EqualError(t, err, "database is locked")
	require.Equal(t, 1, runs)
}

func TestRunIndexWithRebuildGuidance_ReturnsOriginalErrorDuringExplicitRebuild(t *testing.T) {
	t.Parallel()

	var runs int
	err := runIndexWithRebuildGuidance(bytes.NewBuffer(nil), true, func() error {
		runs++
		return errors.New("SQL logic error: no such table: emb_notes")
	})

	require.EqualError(t, err, "SQL logic error: no such table: emb_notes")
	require.Equal(t, 1, runs)
}

func TestShouldRetryIndexWithFreshRebuild(t *testing.T) {
	t.Parallel()

	require.True(t, shouldRetryIndexWithFreshRebuild(errors.New("error applying migration 4 -> 5: SQL logic error")))
	require.True(t, shouldRetryIndexWithFreshRebuild(errors.New("malformed database schema")))
	require.True(t, shouldRetryIndexWithFreshRebuild(errors.New("semantic index: table anchors has no column named base_member")))
	require.True(t, shouldRetryIndexWithFreshRebuild(errors.New("UNIQUE constraint failed: intel_chunks.owner_type, intel_chunks.owner_id, intel_chunks.ord, intel_chunks.granularity")))
	require.False(t, shouldRetryIndexWithFreshRebuild(errors.New("database is locked")))
	require.False(t, shouldRetryIndexWithFreshRebuild(errors.New("UNIQUE constraint failed: user_notes.path")))
	require.False(t, shouldRetryIndexWithFreshRebuild(errors.New("openai api key missing")))
}
