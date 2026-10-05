package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const searchEmbeddingGenerationKey = "search_embeddings_generation"

// SearchCodeCorpusFingerprint identifies the committed code files eligible for
// lexical and structural search. It uses the existing files table so provider-
// disabled searches still invalidate continuation after a code index change.
func (s *Store) SearchCodeCorpusFingerprint(ctx context.Context) (string, int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, lang, IFNULL(hash, ''), IFNULL(indexer_version, ''), parse_status, call_edges_stale, IFNULL(mtime, 0) FROM files ORDER BY path`)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	h := sha256.New()
	count := 0
	for rows.Next() {
		var path, lang, hash, indexerVersion, parseStatus string
		var stale int
		var mtime int64
		if err := rows.Scan(&path, &lang, &hash, &indexerVersion, &parseStatus, &stale, &mtime); err != nil {
			return "", 0, err
		}
		count++
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\n", path, lang, hash, indexerVersion, parseStatus, stale, mtime)
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}
	relationshipGeneration, err := goRelationshipGeneration(ctx, s.db)
	if err != nil {
		return "", 0, err
	}
	_, _ = fmt.Fprintf(h, "go_relationship_generation\x00%d\n", relationshipGeneration)
	return hex.EncodeToString(h.Sum(nil)), count, nil
}

// SearchEmbeddingFingerprint identifies the committed unified embedding rows
// and their indexing configuration. The monotonic generation changes even
// when a provider refresh replaces vectors within the same wall-clock second.
func (s *Store) SearchEmbeddingFingerprint(ctx context.Context) (string, int, error) {
	summary, err := s.EmbeddingsSummary(ctx)
	if err != nil {
		return "", 0, err
	}
	generationRaw, ok, err := s.GetMetadata(ctx, searchEmbeddingGenerationKey)
	if err != nil {
		return "", 0, err
	}
	var generation int64
	if ok && strings.TrimSpace(generationRaw) != "" {
		generation, err = strconv.ParseInt(generationRaw, 10, 64)
		if err != nil || generation < 0 {
			return "", 0, fmt.Errorf("invalid search embedding generation %q", generationRaw)
		}
	}
	pack, err := s.GetPackMetadata(ctx)
	if err != nil {
		return "", 0, err
	}
	payload := fmt.Sprintf("count=%d\x00created=%d\x00generation=%d\x00config=%s\x00model=%s\x00algo=%s", summary.Count, summary.Generation, generation, pack.ConfigHash, pack.ModelHash, pack.AlgoVersion)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:]), summary.Count, nil
}
