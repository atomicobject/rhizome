package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func (s *engine) ValidateOrInitMetadata(ctx context.Context, meta embeddings.IndexMetadata) error {
	if err := s.requireWrite(); err != nil {
		return err
	}
	ctx = indexingperf.WithOp(ctx, s.spec.label+".validate_metadata")
	current, ok, err := s.Metadata(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return s.initMetadata(ctx, meta)
	}
	if current.SchemaVersion > s.spec.version {
		domain := migration.DomainNoteEmbeddings
		if s.spec.domain == Code {
			domain = migration.DomainCodeEmbeddings
		}
		return &migration.ErrFutureSchema{Domain: domain, Current: current.SchemaVersion, Supported: s.spec.version}
	}
	// Code historically heals unknown fields before comparing the remaining identity.
	if s.spec.domain == Code {
		if err := s.healCodeMetadata(ctx, &current, meta); err != nil {
			return err
		}
	}
	reasonCode, reason := "", ""
	switch {
	case meta.Dimensions > 0 && current.Dimensions > 0 && meta.Dimensions != current.Dimensions:
		reasonCode = "dimensions_mismatch"
		reason = fmt.Sprintf("dimensions mismatch: have %d, expected %d", current.Dimensions, meta.Dimensions)
	case meta.Provider != "" && current.Provider != "" && meta.Provider != current.Provider:
		reasonCode = "provider_mismatch"
		reason = fmt.Sprintf("provider mismatch: have %s, expected %s", current.Provider, meta.Provider)
	case meta.Model != "" && current.Model != "" && meta.Model != current.Model:
		reasonCode = "model_mismatch"
		reason = fmt.Sprintf("model mismatch: have %s, expected %s", current.Model, meta.Model)
	}
	if reasonCode != "" {
		return s.resetMetadata(ctx, meta, reasonCode, reason)
	}
	if s.spec.domain == Code {
		return nil
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE `+s.spec.meta+` SET provider=COALESCE(NULLIF(provider,''),?),model=COALESCE(NULLIF(model,''),?),dimensions=CASE WHEN dimensions IS NULL OR dimensions=0 THEN ? ELSE dimensions END,schema_version=CASE WHEN schema_version IS NULL OR schema_version=0 THEN ? ELSE schema_version END WHERE id=1`, meta.Provider, meta.Model, meta.Dimensions, s.spec.version)
		if err == nil && meta.Dimensions > 0 {
			s.runtime.Dimensions.Store(int64(meta.Dimensions))
		}
		return err
	})
}
func (s *engine) initMetadata(ctx context.Context, meta embeddings.IndexMetadata) error {
	if meta.Dimensions == 0 {
		return nil
	}
	if meta.SchemaVersion == 0 {
		meta.SchemaVersion = s.spec.version
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now()
	}
	columns, values := "", ""
	args := []any{meta.Provider, meta.Model, meta.Dimensions, meta.SchemaVersion, meta.CreatedAt.Unix()}
	if s.spec.domain == Code {
		ctx = indexingperf.WithOp(ctx, "codeemb.init_metadata")
		columns = ",fingerprint_version"
		values = ",?"
		args = append(args, meta.FingerprintVersion)
	}
	args = append(args, meta.LastSync.Unix(), meta.SourceHighWater.Unix())
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		query := `INSERT INTO ` + s.spec.meta + ` (id,provider,model,dimensions,schema_version,created_at` + columns + `,last_sync,source_high_water) VALUES(1,?,?,?,?,?` + values + `,?,?)`
		var err error
		if s.spec.domain == Code {
			_, err = execWithRetry(ctx, db, query, args...)
		} else {
			_, err = db.ExecContext(ctx, query, args...)
		}
		if err == nil {
			s.runtime.Dimensions.Store(int64(meta.Dimensions))
		}
		return err
	})
}
func (s *engine) healCodeMetadata(ctx context.Context, current *embeddings.IndexMetadata, meta embeddings.IndexMetadata) error {
	changes := []struct {
		column string
		value  any
		needed bool
	}{{"dimensions", meta.Dimensions, current.Dimensions == 0 && meta.Dimensions > 0}, {"provider", meta.Provider, current.Provider == "" && meta.Provider != ""}, {"model", meta.Model, current.Model == "" && meta.Model != ""}}
	for _, change := range changes {
		if !change.needed {
			continue
		}
		if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
			_, err := execWithRetry(ctx, db, `UPDATE `+s.spec.meta+` SET `+change.column+`=? WHERE id=1`, change.value)
			if err == nil && change.column == "dimensions" {
				s.runtime.Dimensions.Store(int64(meta.Dimensions))
			}
			return err
		}); err != nil {
			return err
		}
		switch change.column {
		case "dimensions":
			current.Dimensions = meta.Dimensions
		case "provider":
			current.Provider = meta.Provider
		case "model":
			current.Model = meta.Model
		}
	}
	return nil
}
func (s *engine) resetMetadata(ctx context.Context, meta embeddings.IndexMetadata, reasonCode, reason string) error {
	resetContext, schemaContext := "reset domain", "recreate schema"
	if s.spec.domain == Notes {
		indexingperf.AddCount(ctx, indexingperf.NoteEmbeddingResetReason.Select(reasonCode), 1)
		if recorder := diagnostics.FromContext(ctx); recorder != nil {
			recorder.Logger("embeddings").InfoContext(ctx, "Note embedding index reset", slog.String("event", "index.reset"), slog.String("reason", reasonCode))
		} else {
			slog.InfoContext(ctx, "Note embedding index reset", "reason", reasonCode)
		}
	} else {
		resetContext, schemaContext = "reset code index domain", "recreate code index schema"
		switch reasonCode {
		case "dimensions_mismatch":
			reason = "code index dimensions mismatch"
		case "provider_mismatch":
			reason = "code index provider mismatch"
		case "model_mismatch":
			reason = "code index model mismatch"
		}
	}
	if err := s.ResetDomain(ctx); err != nil {
		return fmt.Errorf("%s: %s: %w", reason, resetContext, err)
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("%s: %s: %w", reason, schemaContext, err)
	}
	return s.initMetadata(ctx, meta)
}
