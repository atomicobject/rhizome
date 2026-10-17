package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"sync"
)

// EnsureEmbeddingsVecPrimary migrates intel_embeddings to vec-primary storage when needed.
// Safe to call repeatedly; no-op after migration is complete.
func (s *Store) EnsureEmbeddingsVecPrimary(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.ensure_vec_primary")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		dimsList, err := migrateIntelEmbeddingsToVecPrimaryTx(ctx, tx)
		if err != nil {
			return err
		}
		if len(dimsList) > 0 {
			s.vecReady = sync.Map{}
			for _, dims := range dimsList {
				s.vecReady.Store(dims, struct{}{})
			}
		}
		return nil
	})
}

func (s *Store) requireIntelVecPrimary(ctx context.Context) error {
	hasEmbeddingBlob, err := s.tableHasColumn(ctx, "intel_embeddings", "embedding")
	if err != nil {
		return err
	}
	if hasEmbeddingBlob {
		return errors.New("legacy intel_embeddings schema detected; run `rzm index` to migrate embeddings to sqlite-vec")
	}
	return nil
}

func (s *Store) ensureIntelVecMirror(ctx context.Context, dims int) error {
	if dims <= 0 {
		return fmt.Errorf("invalid vec dimensions: %d", dims)
	}
	if _, ok := s.vecReady.Load(dims); ok {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.ensure_vec_mirror")
	// Mark vector schema work observable even when the existing table makes the
	// correct query path a zero-statement read.
	indexingperf.MarkCountAvailable(ctx, indexingperf.AgentStartOpSchemaStatements)
	table := intelVecTableName(dims)
	exists, err := s.tableExists(ctx, table)
	if err != nil {
		return err
	}
	if exists {
		s.vecReady.Store(dims, struct{}{})
		return nil
	}
	if s.readOnly {
		return fmt.Errorf("indexed vector table for %d dimensions is missing; run `rzm index`", dims)
	}

	if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return createIntelVecTable(ctx, db, dims)
	}); err != nil {
		return err
	}
	s.vecReady.Store(dims, struct{}{})
	return nil
}
