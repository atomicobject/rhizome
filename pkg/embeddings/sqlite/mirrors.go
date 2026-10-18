package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type mirrorSpec struct{ table, source, key string }

func (s *engine) ensureMirrors(ctx context.Context, dims int) error {
	if dims <= 0 {
		return fmt.Errorf("invalid vec dimensions: %d", dims)
	}
	if _, ok := s.runtime.VecReady.Load(dims); ok {
		return nil
	}
	mirrors := []mirrorSpec{{fmt.Sprintf("%s%d", s.spec.chunkPrefix, dims), s.spec.chunks, "chunk_id"}}
	if s.spec.itemVectors != "" {
		mirrors = append(mirrors, mirrorSpec{fmt.Sprintf("%s%d", s.spec.itemPrefix, dims), s.spec.itemVectors, "item_id"})
	}
	if s.access == ReadExisting {
		for _, m := range mirrors {
			exists, err := tableExists(ctx, s.db, m.table)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("embedding vector mirror %s is missing", m.table)
			}
		}
		s.runtime.VecReady.Store(dims, struct{}{})
		return nil
	}
	ctx = indexingperf.WithOp(ctx, s.spec.label+".ensure_vec_mirror")
	if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		for _, m := range mirrors {
			if err := createMirror(ctx, db, m, dims); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	s.runtime.VecReady.Store(dims, struct{}{})
	return nil
}
func createMirror(ctx context.Context, db *sql.DB, m mirrorSpec, dims int) error {
	size := dims * 4
	stmts := []string{
		fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(%s integer primary key,embedding float[%d]);`, m.table, m.key, dims),
		fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS trg_%s_ins AFTER INSERT ON %s WHEN NEW.dimensions=%d AND length(NEW.embedding)=%d BEGIN INSERT INTO %s(%s,embedding) VALUES(NEW.id,NEW.embedding); END;`, m.table, m.source, dims, size, m.table, m.key),
		fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS trg_%s_upd AFTER UPDATE OF embedding,dimensions ON %s BEGIN DELETE FROM %s WHERE %s=OLD.id; INSERT INTO %s(%s,embedding) SELECT NEW.id,NEW.embedding WHERE NEW.dimensions=%d AND length(NEW.embedding)=%d; END;`, m.table, m.source, m.table, m.key, m.table, m.key, dims, size),
		fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS trg_%s_del AFTER DELETE ON %s BEGIN DELETE FROM %s WHERE %s=OLD.id; END;`, m.table, m.source, m.table, m.key),
		`DELETE FROM ` + m.table,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s(%s,embedding) SELECT id,embedding FROM %s WHERE dimensions=? AND length(embedding)=?`, m.table, m.key, m.source), dims, size)
	return err
}
