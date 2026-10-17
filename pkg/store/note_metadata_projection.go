package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func insertNoteTags(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, rows []NoteTagRow) error {
	if len(rows) == 0 {
		return nil
	}
	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		noteID, ok := noteIDs[row.NotePath]
		if !ok {
			continue
		}
		values = append(values, []any{noteID, row.TagNorm})
	}
	return execValuesBatch(ctx, tx, `INSERT INTO note_tags(note_id, tag_norm) VALUES`, values, 2, "")
}

func insertNoteSearchTerms(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, rows []NoteMetadataRow) error {
	values := make([][]any, 0, len(rows)*6)
	for _, row := range rows {
		noteID, ok := noteIDs[row.Path]
		if !ok {
			continue
		}
		for _, term := range buildNoteSearchTermRowsForNote(noteID, row.Path, row.Title) {
			values = append(values, []any{term.NoteID, int(term.Scope), term.SegmentOrdinal, term.TokenOrdinal, term.TokenText})
		}
	}
	return execValuesBatch(ctx, tx, `
		INSERT OR IGNORE INTO note_search_terms(note_id, scope, segment_ordinal, token_ordinal, token_text)
		VALUES
	`, values, 5, "")
}

// searchableProjectionMetadataRows prevents fatal source identity rows from
// becoming searchable. Legacy callers can omit projection state, and stale
// rows retain their historical search behavior; only explicit fatal rows are
// root-only source provenance.
func searchableProjectionMetadataRows(rows []NoteMetadataRow) []NoteMetadataRow {
	searchable := make([]NoteMetadataRow, 0, len(rows))
	for _, row := range rows {
		if row.Projection.Status != NoteProjectionStatusFatal {
			searchable = append(searchable, row)
		}
	}
	return searchable
}

func insertNoteFragmentTargets(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, rows []NoteFragmentTargetRow) error {
	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		noteID, ok := noteIDs[strings.TrimSpace(row.NotePath)]
		if !ok {
			continue
		}
		values = append(values, []any{
			noteID,
			string(row.Kind),
			row.Target,
			row.TargetNorm,
			row.Ordinal,
		})
	}
	return execValuesBatch(ctx, tx, `
		INSERT INTO note_fragment_targets(note_id, target_kind, target_text, target_norm, ordinal)
		VALUES
	`, values, 5, "")
}

func insertNoteProjectionDiagnostics(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, rows []NoteProjectionDiagnosticRow) error {
	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		noteID, ok := noteIDs[strings.TrimSpace(row.NotePath)]
		if !ok {
			continue
		}
		if row.Ordinal < 0 || strings.TrimSpace(row.Code) == "" || strings.TrimSpace(row.Category) == "" || strings.TrimSpace(row.Message) == "" || strings.TrimSpace(row.AffectedOperation) == "" {
			return fmt.Errorf("invalid projection diagnostic for %q", row.NotePath)
		}
		if (!row.RangePresent && (row.StartByte != 0 || row.EndByte != 0)) || (row.RangePresent && (row.StartByte < 0 || row.EndByte < row.StartByte)) {
			return fmt.Errorf("invalid projection diagnostic range for %q", row.NotePath)
		}
		values = append(values, []any{noteID, row.Ordinal, row.Code, row.Category, row.Message, boolToInt(row.RangePresent), row.StartByte, row.EndByte, boolToInt(row.Blocking), row.AffectedOperation})
	}
	return execValuesBatch(ctx, tx, `
		INSERT INTO note_projection_diagnostics(
			note_id, ordinal, code, category, message, range_present,
			start_byte, end_byte, blocking, affected_operation
		) VALUES
	`, values, 10, "")
}

func insertNoteSearchRegions(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, rows []NoteSearchRegionRow) error {
	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		noteID, ok := noteIDs[strings.TrimSpace(row.NotePath)]
		if !ok {
			continue
		}
		if row.Ordinal < 0 || strings.TrimSpace(row.Origin) == "" || strings.TrimSpace(row.Kind) == "" || row.Text == "" || strings.TrimSpace(row.MediaType) == "" {
			return fmt.Errorf("invalid search region for %q", row.NotePath)
		}
		if (!row.RangePresent && (row.StartByte != 0 || row.EndByte != 0)) || (row.RangePresent && (row.StartByte < 0 || row.EndByte < row.StartByte)) {
			return fmt.Errorf("invalid search region range for %q", row.NotePath)
		}
		values = append(values, []any{noteID, row.Ordinal, row.Origin, row.Kind, row.Text, row.MediaType, boolToInt(row.RangePresent), row.StartByte, row.EndByte})
	}
	return execValuesBatch(ctx, tx, `
		INSERT INTO note_search_regions(
			note_id, ordinal, origin, region_kind, region_text, media_type,
			range_present, start_byte, end_byte
		) VALUES
	`, values, 9, "")
}

func insertGraphDocEdges(ctx context.Context, tx *sql.Tx, edges []GraphDocEdgeRow) error {
	if len(edges) == 0 {
		return nil
	}
	values := make([][]any, 0, len(edges))
	for _, edge := range edges {
		if strings.TrimSpace(edge.SrcPath) == "" || strings.TrimSpace(edge.DstPath) == "" || edge.SrcPath == edge.DstPath {
			continue
		}
		confidence, confidenceScore := NormalizeGraphDocEdgeConfidence(edge.Kind, edge.Confidence, edge.ConfidenceScore)
		values = append(values, []any{edge.SrcPath, edge.DstPath, edge.Kind, confidence, confidenceScore, "", edge.LinkText})
	}
	return execValuesBatch(ctx, tx, `
		INSERT OR REPLACE INTO graph_doc_edges(src_path, dst_path, kind, confidence, confidence_score, source_location, link_text)
		VALUES
	`, values, 7, "")
}
