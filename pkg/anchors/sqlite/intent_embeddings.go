package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
)

const intentEmbeddingProviderFingerprintKey = "intent_embeddings_provider_fingerprint"

type intentEmbeddingQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func queryIntentEmbeddings(ctx context.Context, queryer intentEmbeddingQueryer) ([]intentstore.EmbeddingRecord, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT intent, exemplar, embedding, norm, dimensions, updated_at
		FROM intent_embeddings
		ORDER BY intent, exemplar
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []intentstore.EmbeddingRecord
	for rows.Next() {
		var row intentstore.EmbeddingRecord
		var norm float64
		var updatedAt int64
		var embBytes []byte
		if err := rows.Scan(&row.Intent, &row.Exemplar, &embBytes, &norm, &row.Dimensions, &updatedAt); err != nil {
			return nil, err
		}
		row.Embedding = bytesToEmbed(embBytes)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// IntentEmbeddings returns stored intent exemplar embeddings.
func (s *Store) IntentEmbeddings(ctx context.Context) ([]intentstore.EmbeddingRecord, error) {
	return queryIntentEmbeddings(ctx, s.db)
}

// IntentEmbeddingSnapshot returns stored intent embeddings and the provider
// configuration fingerprint that produced them from one database snapshot.
func (s *Store) IntentEmbeddingSnapshot(ctx context.Context) (intentstore.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT metadata.provider_fingerprint,
		       embeddings.intent, embeddings.exemplar, embeddings.embedding,
		       embeddings.norm, embeddings.dimensions, embeddings.updated_at
		FROM (
			SELECT COALESCE(
				(SELECT value FROM index_metadata WHERE key = ?),
				''
			) AS provider_fingerprint
		) AS metadata
		LEFT JOIN intent_embeddings AS embeddings ON 1 = 1
		ORDER BY embeddings.intent, embeddings.exemplar
	`, intentEmbeddingProviderFingerprintKey)
	if err != nil {
		return intentstore.Snapshot{}, err
	}
	defer rows.Close()

	var snapshot intentstore.Snapshot
	for rows.Next() {
		var intent, exemplar sql.NullString
		var embedding []byte
		var norm sql.NullFloat64
		var dimensions, updatedAt sql.NullInt64
		if err := rows.Scan(
			&snapshot.ProviderFingerprint,
			&intent, &exemplar, &embedding, &norm, &dimensions, &updatedAt,
		); err != nil {
			return intentstore.Snapshot{}, err
		}
		if !intent.Valid {
			continue
		}
		snapshot.Rows = append(snapshot.Rows, intentstore.EmbeddingRecord{
			Intent:     intent.String,
			Exemplar:   exemplar.String,
			Embedding:  bytesToEmbed(embedding),
			Dimensions: int(dimensions.Int64),
		})
	}
	if err := rows.Err(); err != nil {
		return intentstore.Snapshot{}, err
	}
	return snapshot, nil
}

// ReplaceIntentEmbeddingSnapshot atomically replaces the complete intent
// embedding corpus and its provider provenance.
func (s *Store) ReplaceIntentEmbeddingSnapshot(ctx context.Context, snapshot intentstore.Snapshot) error {
	rows := make([]intentstore.EmbeddingRecord, len(snapshot.Rows))
	copy(rows, snapshot.Rows)
	dim := 0
	for i := range rows {
		row := &rows[i]
		if strings.TrimSpace(row.Intent) == "" || strings.TrimSpace(row.Exemplar) == "" {
			return errors.New("intent embedding snapshot contains an empty intent or exemplar")
		}
		if len(row.Embedding) == 0 {
			return fmt.Errorf("intent embedding snapshot exemplar %q has an empty embedding", row.Exemplar)
		}
		if row.Dimensions <= 0 {
			row.Dimensions = len(row.Embedding)
		}
		if len(row.Embedding) != row.Dimensions {
			return fmt.Errorf("intent embedding snapshot exemplar %q has %d values for %d dimensions", row.Exemplar, len(row.Embedding), row.Dimensions)
		}
		if dim == 0 {
			dim = row.Dimensions
		} else if row.Dimensions != dim {
			return fmt.Errorf("intent embedding snapshot mixes %d and %d dimensions", dim, row.Dimensions)
		}
	}

	ctx = indexingperf.WithOp(ctx, "intel.replace_intent_embeddings")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM intent_embeddings`); err != nil {
			return err
		}
		now := time.Now().Unix()
		const batchSize = 200
		for start := 0; start < len(rows); start += batchSize {
			end := start + batchSize
			if end > len(rows) {
				end = len(rows)
			}
			batch := rows[start:end]

			valueSQL := make([]string, 0, len(batch))
			args := make([]any, 0, len(batch)*6)
			for _, row := range batch {
				norm := math.Sqrt(dotFloat64(row.Embedding, row.Embedding))
				updatedAt := now
				valueSQL = append(valueSQL, "(?, ?, ?, ?, ?, ?)")
				args = append(args, row.Intent, row.Exemplar, embedToBytes(row.Embedding), norm, row.Dimensions, updatedAt)
			}
			stmt := fmt.Sprintf(`
				INSERT INTO intent_embeddings (intent, exemplar, embedding, norm, dimensions, updated_at)
				VALUES %s
			`, strings.Join(valueSQL, ","))
			if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO index_metadata(key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, intentEmbeddingProviderFingerprintKey, snapshot.ProviderFingerprint)
		return err
	})
}
