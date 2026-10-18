package store

import (
	"context"
	"database/sql"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strconv"
	"strings"
)

func normalizeIntelChunkFamily(family string) string {
	family = strings.TrimSpace(family)
	if family == "" {
		return codeanchor.IntelChunkFamilyDefault
	}
	return family
}

func dedupeNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func dedupeStringsStable(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func dedupeIntelChunksStable(chunks []codeanchor.IntelChunk) []codeanchor.IntelChunk {
	if len(chunks) == 0 {
		return nil
	}
	indexByKey := make(map[string]int, len(chunks))
	out := make([]codeanchor.IntelChunk, 0, len(chunks))
	for _, chunk := range chunks {
		key := strings.Join([]string{
			string(chunk.OwnerType),
			chunk.OwnerID,
			strconv.Itoa(chunk.Ord),
			chunk.Granularity,
		}, "\x00")
		if idx, ok := indexByKey[key]; ok {
			out[idx] = chunk
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, chunk)
	}
	return out
}

func dedupeSymbolRefs(refs []codeanchor.SymbolRef) []codeanchor.SymbolRef {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	out := make([]codeanchor.SymbolRef, 0, len(refs))
	for _, ref := range refs {
		key := symbolRefKey(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func (s *Store) selectIntelSectionIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT section_id FROM intel_doc_sections WHERE path = ?`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) selectIntelSectionRowIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM intel_doc_sections WHERE path = ?`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
