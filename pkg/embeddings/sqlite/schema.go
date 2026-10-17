package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

func (s *engine) EnsureSchema(ctx context.Context) error {
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error { return s.ensureSchemaNoLock(ctx, db) })
}
func (s *engine) ensureSchemaNoLock(ctx context.Context, db schemaDB) error {
	if s.spec.domain == Notes {
		return (&NoteStore{s}).ensureSchemaNoLock(ctx, db)
	}
	return (&CodeStore{s}).ensureSchemaNoLock(ctx, db)
}
func (s *engine) ensureDomainMigrations(ctx context.Context) error {
	if s.spec.domain == Notes {
		return (&NoteStore{s}).ensureDomainMigrations(ctx)
	}
	return (&CodeStore{s}).ensureDomainMigrations(ctx)
}
func (s *engine) ensureSchemaWithRecovery(ctx context.Context) error {
	if err := s.requireWrite(); err != nil {
		return err
	}
	err := s.EnsureSchema(ctx)
	if err != nil && !isSchemaError(err) {
		return err
	}
	if err == nil {
		var needs bool
		if s.spec.domain == Notes {
			needs, err = (&NoteStore{s}).needsSchemaRebuild(ctx)
		} else {
			needs, err = (&CodeStore{s}).needsSchemaRebuild(ctx)
		}
		if err != nil {
			return err
		}
		if !needs {
			return s.ensureDomainMigrations(ctx)
		}
	}
	if err := s.rebuildDomainPreservingEmbeddings(ctx); err != nil {
		return fmt.Errorf("preserve embedding index during schema recovery: %w", err)
	}
	return s.ensureDomainMigrations(ctx)
}
func (s *engine) ResetDomain(ctx context.Context) error { return s.resetDomain(ctx, true) }
func (s *engine) ResetDomainPreservingCache(ctx context.Context) error {
	return s.resetDomain(ctx, false)
}
func (s *engine) resetDomain(ctx context.Context, dropCache bool) error {
	op := ".reset_domain"
	if !dropCache {
		op = ".reset_domain_preserve_cache"
	}
	ctx = indexingperf.WithOp(ctx, s.spec.label+op)
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if err := s.resetDomainOn(ctx, db, dropCache); err != nil {
			return err
		}
		s.runtime.VecReady.Clear()
		return nil
	})
}
func (s *engine) resetDomainOn(ctx context.Context, db schemaDB, dropCache bool) error {
	if s.spec.domain == Notes {
		return (&NoteStore{s}).resetDomainOn(ctx, db, dropCache)
	}
	return (&CodeStore{s}).resetDomainOn(ctx, db, dropCache)
}

// Recovery commits backups, schema recreation and all restored paid data together.
func (s *engine) rebuildDomainPreservingEmbeddings(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		err := sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			if s.spec.domain == Notes {
				return (&NoteStore{s}).recoverNoteTx(ctx, tx)
			}
			return (&CodeStore{s}).recoverCodeTx(ctx, tx)
		})
		if err == nil {
			s.runtime.VecReady.Clear()
		}
		return err
	})
}
