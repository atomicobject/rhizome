package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// PersistedDomainSummary is lightweight availability evidence for a derived
// index domain that a scoped coordinator intentionally leaves untouched.
type PersistedDomainSummary struct {
	Count      int
	Generation int64
}

// PersistedContentSummary is deterministic logical-content evidence for a
// derived domain whose source tables do not carry a reliable generation.
type PersistedContentSummary struct {
	Count int
	Hash  string
}

type UntouchedIndexSummary struct {
	Code              PersistedDomainSummary
	CodeAnchors       PersistedDomainSummary
	GraphDocScores    PersistedDomainSummary
	GraphAnchorScores PersistedDomainSummary
}

// UntouchedIndexDomainsSummary reads only count/timestamp evidence for the
// validation projection; graph cache invalidation uses its revision singleton.
func (s *Store) UntouchedIndexDomainsSummary(ctx context.Context) (UntouchedIndexSummary, error) {
	var summary UntouchedIndexSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM files),
			COALESCE((SELECT MAX(mtime) FROM files), 0),
			(SELECT COUNT(*) FROM intel_code_anchors),
			COALESCE((SELECT MAX(updated_at) FROM intel_code_anchors), 0),
			(SELECT COUNT(*) FROM graph_doc_scores),
			COALESCE((SELECT MAX(updated_at) FROM graph_doc_scores), 0),
			(SELECT COUNT(*) FROM graph_anchor_scores),
			COALESCE((SELECT MAX(updated_at) FROM graph_anchor_scores), 0)
	`).Scan(
		&summary.Code.Count,
		&summary.Code.Generation,
		&summary.CodeAnchors.Count,
		&summary.CodeAnchors.Generation,
		&summary.GraphDocScores.Count,
		&summary.GraphDocScores.Generation,
		&summary.GraphAnchorScores.Count,
		&summary.GraphAnchorScores.Generation,
	)
	return summary, err
}

func (s *Store) IntelChunksSummary(ctx context.Context) (PersistedDomainSummary, error) {
	var summary PersistedDomainSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MAX(updated_at), 0)
		FROM intel_chunks
	`).Scan(&summary.Count, &summary.Generation)
	return summary, err
}

func (s *Store) EmbeddingsSummary(ctx context.Context) (PersistedDomainSummary, error) {
	var summary PersistedDomainSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MAX(created_at), 0)
		FROM intel_embeddings
	`).Scan(&summary.Count, &summary.Generation)
	return summary, err
}

// CodeAnchorSelectorSummary fingerprints every persisted selector, glob, and
// note-ownership row. Database IDs and timestamps are deliberately excluded so
// equivalent logical declarations have identical evidence across rebuilds.
func (s *Store) CodeAnchorSelectorSummary(ctx context.Context) (PersistedContentSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(n.path, ''), a.label, a.kind, COALESCE(a.lang, ''),
			COALESCE(a.base_lang, ''), COALESCE(a.base_pkg, ''), COALESCE(a.base_name, ''), a.base_member,
			COALESCE(a.ann_lang, ''), COALESCE(a.ann_pkg, ''), COALESCE(a.ann_name, ''), COALESCE(a.ann_args_json, ''),
			COALESCE(a.path_prefix, ''), COALESCE(ag.pattern, '')
		FROM anchors a
		LEFT JOIN note_anchors na ON na.anchor_id = a.id
		LEFT JOIN notes n ON n.id = na.note_id
		LEFT JOIN anchor_globs ag ON ag.anchor_id = a.id
		ORDER BY
			COALESCE(n.path, ''), a.label, a.kind, COALESCE(a.lang, ''),
			COALESCE(a.base_lang, ''), COALESCE(a.base_pkg, ''), COALESCE(a.base_name, ''), a.base_member,
			COALESCE(a.ann_lang, ''), COALESCE(a.ann_pkg, ''), COALESCE(a.ann_name, ''), COALESCE(a.ann_args_json, ''),
			COALESCE(a.path_prefix, ''), COALESCE(ag.pattern, '')
	`)
	if err != nil {
		return PersistedContentSummary{}, err
	}
	defer rows.Close()

	digest := sha256.New()
	_, _ = digest.Write([]byte("code-anchor-selectors:v1\n"))
	summary := PersistedContentSummary{}
	for rows.Next() {
		var owner, label, kind, lang string
		var baseLang, basePkg, baseName string
		var baseMember int
		var annLang, annPkg, annName, annArgs, pathPrefix, glob string
		if err := rows.Scan(
			&owner, &label, &kind, &lang,
			&baseLang, &basePkg, &baseName, &baseMember,
			&annLang, &annPkg, &annName, &annArgs, &pathPrefix, &glob,
		); err != nil {
			return PersistedContentSummary{}, err
		}
		encoded, err := json.Marshal([]any{
			owner, label, kind, lang,
			baseLang, basePkg, baseName, baseMember,
			annLang, annPkg, annName, annArgs, pathPrefix, glob,
		})
		if err != nil {
			return PersistedContentSummary{}, err
		}
		_, _ = digest.Write(encoded)
		_, _ = digest.Write([]byte{'\n'})
		summary.Count++
	}
	if err := rows.Err(); err != nil {
		return PersistedContentSummary{}, err
	}
	summary.Hash = fmt.Sprintf("sha256:%x", digest.Sum(nil))
	return summary, nil
}
