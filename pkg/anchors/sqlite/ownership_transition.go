package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// OwnershipTarget is the configured durable domain for one authored path.
//
// Code targets intentionally do not carry code artifacts: a later code-index
// batch owns their publication. This transition first removes any note state,
// making the hand-off free of simultaneous note and code identity.
type OwnershipTarget string

const (
	OwnershipTargetNote    OwnershipTarget = "note"
	OwnershipTargetCode    OwnershipTarget = "code"
	OwnershipTargetUnowned OwnershipTarget = "unowned"
)

// NoteSourceState is the source identity and provider provenance that survives
// when an owned note has no usable derived projection. It deliberately omits
// metadata, link, search, and ontology facts; those remain later projector
// responsibilities. Stale is allowed without a diagnostic so discovery can
// durably record a valid source before a provider projection is available.
type NoteSourceState struct {
	Title             string
	FormatID          string
	ContentHash       string
	Mtime             int64
	Size              int64
	ProviderVersion   string
	ProjectionVersion string
	Status            NoteProjectionStatus
	DiagnosticCode    string
	DiagnosticDetail  string
	ObservedAt        int64
}

// OwnershipTransition changes one path's durable owner. Note is required only
// for a note target. Inputs are fully derived before ApplyOwnershipTransitions;
// it performs no provider, filesystem, or network work inside its transaction.
type OwnershipTransition struct {
	Path   string
	Target OwnershipTarget
	Note   *NoteSourceState
}

// OwnershipTransitionResult identifies durable artifacts that need caller-side
// cache invalidation or rederivation after a committed transition. Affected
// source paths include every non-transitioned source whose ontology, graph, or
// doc-link evidence was removed because one of its targets retired.
type OwnershipTransitionResult struct {
	AffectedSourcePaths      []string
	AffectedAnchorIDs        []int64
	TransitionedPaths        []string
	ReconciliationGeneration int64
}

// ApplyOwnershipTransitions atomically clears previous durable ownership and
// publishes note source provenance for every supplied path.
//
// Full indexing invokes this seam through the queued ownership-transition
// control, which fences preceding writes before this transaction begins.
func (s *Store) ApplyOwnershipTransitions(ctx context.Context, transitions []OwnershipTransition) (OwnershipTransitionResult, error) {
	if s == nil || s.db == nil {
		return OwnershipTransitionResult{}, fmt.Errorf("ownership transition store is required")
	}
	normalized, err := normalizeOwnershipTransitions(transitions)
	if err != nil {
		return OwnershipTransitionResult{}, err
	}
	if len(normalized) == 0 {
		return OwnershipTransitionResult{}, nil
	}

	ctx = indexingperf.WithOp(ctx, "intel.apply_ownership_transition")
	result := OwnershipTransitionResult{}
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		effective, err := ownershipEffectiveTransitionsTx(ctx, tx, normalized)
		if err != nil {
			return err
		}
		if len(effective) == 0 {
			return nil
		}
		transitionedPaths := make([]string, len(effective))
		for i, transition := range effective {
			transitionedPaths[i] = transition.Path
		}
		result.TransitionedPaths = append(result.TransitionedPaths, transitionedPaths...)
		sources, err := ownershipAffectedSourcePathsTx(ctx, tx, transitionedPaths)
		if err != nil {
			return err
		}
		result.AffectedSourcePaths = filterTransitionedSourcePaths(sources, transitionedPaths)

		for _, transition := range effective {
			if err := s.deleteCodeOwnedArtifactsTx(ctx, tx, transition.Path); err != nil {
				return err
			}
			affectedAnchorIDs, err := s.deleteNoteOwnedArtifactsTx(ctx, tx, transition.Path)
			if err != nil {
				return err
			}
			result.AffectedAnchorIDs = append(result.AffectedAnchorIDs, affectedAnchorIDs...)
		}
		if err := deleteIntelReverseIndexByPathBatchesTx(ctx, tx, transitionedPaths); err != nil {
			return err
		}

		for _, transition := range effective {
			if transition.Target != OwnershipTargetNote {
				continue
			}
			if err := upsertOwnershipNoteSourceTx(ctx, tx, transition.Path, *transition.Note); err != nil {
				return err
			}
		}
		// This seam changes notes outside the legacy metadata snapshot/delta
		// writer. Dropping readiness prevents its XOR freshness hash from
		// incorrectly treating the partly-derived note set as current.
		if err := invalidateOwnershipMetadataStateTx(ctx, tx); err != nil {
			return err
		}
		generation, err := advanceOwnershipReconciliationGenerationTx(ctx, tx)
		if err != nil {
			return err
		}
		result.ReconciliationGeneration = generation
		return nil
	})
	if err != nil {
		return OwnershipTransitionResult{}, err
	}
	result.AffectedAnchorIDs = sortedUniqueInt64(result.AffectedAnchorIDs)
	result.TransitionedPaths = sortedUniqueStrings(result.TransitionedPaths)
	return result, nil
}

func ownershipSourceStateMatchesRow(state NoteSourceState, row NoteMetadataRow) bool {
	return state.Title == row.Title &&
		state.FormatID == row.FormatID &&
		state.ContentHash == row.ContentHash &&
		state.Mtime == row.Mtime &&
		state.Size == row.Size &&
		state.ProviderVersion == row.Projection.ProviderVersion &&
		state.ProjectionVersion == row.Projection.ProjectionVersion &&
		state.Status == row.Projection.Status &&
		state.DiagnosticCode == row.Projection.DiagnosticCode &&
		state.DiagnosticDetail == row.Projection.DiagnosticDetail &&
		state.ContentHash == row.Projection.SourceContentHash
}

// ownershipSourceObservationMatchesRow recognizes a full-index discovery
// observation for a source that is already durably current or fatal. The
// discovery stage intentionally records stale before provider projection; it
// must not clear derived evidence when the authored bytes and provider
// envelope are unchanged. Projected title, status, and filesystem timestamps
// can differ because they are not part of that authored-source identity.
func ownershipSourceObservationMatchesRow(state NoteSourceState, row NoteMetadataRow) bool {
	return state.Status == NoteProjectionStatusStale &&
		state.DiagnosticCode == "" &&
		state.DiagnosticDetail == "" &&
		state.ContentHash != "" &&
		state.FormatID == row.FormatID &&
		state.ContentHash == row.ContentHash &&
		state.ContentHash == row.Projection.SourceContentHash &&
		state.ProviderVersion == row.Projection.ProviderVersion &&
		state.ProjectionVersion == row.Projection.ProjectionVersion &&
		(row.Projection.Status == NoteProjectionStatusCurrent || row.Projection.Status == NoteProjectionStatusFatal)
}

// ownershipPreparedCurrentMatchesRow is the provider-result fixed point.
// Prepared current projections may carry a different display title or a newer
// observation timestamp for the same authored bytes. Those fields must not
// create ownership churn; a changed content hash or provider envelope remains
// effective. Fatal-to-current intentionally does not match here.
func ownershipPreparedCurrentMatchesRow(state NoteSourceState, row NoteMetadataRow) bool {
	return state.Status == NoteProjectionStatusCurrent &&
		state.DiagnosticCode == "" &&
		state.DiagnosticDetail == "" &&
		state.ContentHash != "" &&
		row.Projection.Status == NoteProjectionStatusCurrent &&
		state.FormatID == row.FormatID &&
		state.ContentHash == row.ContentHash &&
		state.ContentHash == row.Projection.SourceContentHash &&
		state.ProviderVersion == row.Projection.ProviderVersion &&
		state.ProjectionVersion == row.Projection.ProjectionVersion
}

func normalizeOwnershipTransitions(transitions []OwnershipTransition) ([]OwnershipTransition, error) {
	if len(transitions) == 0 {
		return nil, nil
	}
	out := make([]OwnershipTransition, 0, len(transitions))
	seen := make(map[string]struct{}, len(transitions))
	for _, transition := range transitions {
		path, err := normalizeOwnershipPath(transition.Path)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[path]; exists {
			return nil, fmt.Errorf("ownership transition repeats path %q", path)
		}
		seen[path] = struct{}{}
		transition.Path = path
		switch transition.Target {
		case OwnershipTargetNote:
			if transition.Note == nil {
				return nil, fmt.Errorf("note ownership transition for %q requires source state", path)
			}
			if err := validateNoteSourceState(*transition.Note); err != nil {
				return nil, fmt.Errorf("note ownership transition for %q: %w", path, err)
			}
		case OwnershipTargetCode, OwnershipTargetUnowned:
			if transition.Note != nil {
				return nil, fmt.Errorf("%s ownership transition for %q must not include note source state", transition.Target, path)
			}
		default:
			return nil, fmt.Errorf("ownership transition for %q has unsupported target %q", path, transition.Target)
		}
		out = append(out, transition)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func normalizeOwnershipPath(raw string) (string, error) {
	path, err := paths.CleanRelPath(raw)
	if err != nil {
		return "", fmt.Errorf("ownership transition path %q: %w", raw, err)
	}
	if path == "" {
		return "", fmt.Errorf("ownership transition path must be vault-relative: %q", raw)
	}
	return path.String(), nil
}

func validateNoteSourceState(state NoteSourceState) error {
	if strings.TrimSpace(state.FormatID) == "" {
		return fmt.Errorf("format ID is required")
	}
	if strings.TrimSpace(state.ContentHash) == "" && state.Status != NoteProjectionStatusFatal {
		return fmt.Errorf("content hash is required unless the source is fatally unreadable")
	}
	if state.Mtime < 0 || state.Size < 0 || state.ObservedAt < 0 {
		return fmt.Errorf("filesystem freshness values must not be negative")
	}
	if strings.TrimSpace(state.ProviderVersion) == "" || strings.TrimSpace(state.ProjectionVersion) == "" {
		return fmt.Errorf("provider and projection versions are required")
	}
	_, err := normalizeNoteProjectionState(NoteProjectionState{
		ProviderVersion:   state.ProviderVersion,
		ProjectionVersion: state.ProjectionVersion,
		SourceContentHash: state.ContentHash,
		Status:            state.Status,
		DiagnosticCode:    state.DiagnosticCode,
		DiagnosticDetail:  state.DiagnosticDetail,
		UpdatedAt:         state.ObservedAt,
	}, state.ContentHash, state.ObservedAt)
	return err
}

func upsertOwnershipNoteSourceTx(ctx context.Context, tx *sql.Tx, path string, source NoteSourceState) error {
	row := NoteMetadataRow{
		Path:        path,
		Title:       source.Title,
		ContentHash: source.ContentHash,
		Mtime:       source.Mtime,
		Size:        source.Size,
		FormatID:    source.FormatID,
		Projection: NoteProjectionState{
			ProviderVersion:   source.ProviderVersion,
			ProjectionVersion: source.ProjectionVersion,
			SourceContentHash: source.ContentHash,
			Status:            source.Status,
			DiagnosticCode:    source.DiagnosticCode,
			DiagnosticDetail:  source.DiagnosticDetail,
			UpdatedAt:         source.ObservedAt,
		},
	}
	noteIDs, err := upsertMetadataNotes(ctx, tx, []NoteMetadataRow{row}, source.ObservedAt)
	if err != nil {
		return err
	}
	return upsertNoteProjectionStates(ctx, tx, noteIDs, []NoteMetadataRow{row}, source.ObservedAt)
}

func (s *Store) deleteCodeOwnedArtifactsTx(ctx context.Context, tx *sql.Tx, path string) error {
	path = normalizeCodeLookupPath(path)
	if path == "" {
		return nil
	}
	anchorIDs, err := s.selectIntelAnchorIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	if err := deleteDocLinksToAnchorIDsTx(ctx, tx, anchorIDs); err != nil {
		return err
	}
	if err := deleteGraphAnchorScoresTx(ctx, tx, anchorIDs); err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM super_edges WHERE child_fqn IN (SELECT fqn FROM symbols WHERE file = ?) OR parent_fqn IN (SELECT fqn FROM symbols WHERE file = ?)`,
		`DELETE FROM annotations WHERE owner_fqn IN (SELECT fqn FROM symbols WHERE file = ?)`,
		`DELETE FROM anchor_scopes WHERE symbol_fqn IN (SELECT fqn FROM symbols WHERE file = ?) OR call_file = ?`,
		`DELETE FROM symbols WHERE file = ?`,
		`DELETE FROM files WHERE path = ?`,
	} {
		args := []any{path}
		if strings.Count(statement, "?") == 2 {
			args = append(args, path)
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return err
		}
	}
	if err := s.deleteIntelCodeByPathTx(ctx, tx, path); err != nil {
		return err
	}
	if err := s.deleteRationaleByPathTx(ctx, tx, path); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = ?`, path); err != nil {
		return err
	}
	return nil
}

func (s *Store) deleteNoteOwnedArtifactsTx(ctx context.Context, tx *sql.Tx, path string) ([]int64, error) {
	affectedAnchorIDs, err := ownershipNoteAnchorIDsByPathTx(ctx, tx, path)
	if err != nil {
		return nil, err
	}
	if err := s.deleteIntelDocSectionsByPathTx(ctx, tx, path); err != nil {
		return nil, err
	}
	if err := s.replaceNoteRegionFTSPathsTx(ctx, tx, nil, []string{path}, nil, nil); err != nil {
		return nil, err
	}
	nodeRowIDs, err := selectOntologyNodeRowIDsByPathsTx(ctx, tx, []string{path})
	if err != nil {
		return nil, err
	}
	if err := s.deleteIntelEdgesByRowIDsTx(ctx, tx, "ontology_node", nodeRowIDs); err != nil {
		return nil, err
	}
	if err := deleteOntologyNodeFieldValuesForPathsTx(ctx, tx, []string{path}); err != nil {
		return nil, err
	}
	if err := deleteOntologyNodeLinkDependenciesForPathsTx(ctx, tx, []string{path}); err != nil {
		return nil, err
	}
	if err := deleteStaleOntologyNodesForPathsTx(ctx, tx, []string{path}, nil, false); err != nil {
		return nil, err
	}
	if err := deleteOntologyForPathsTx(ctx, tx, []string{path}, true); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM graph_doc_edges WHERE src_path = ? OR dst_path = ?`, path, path); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM graph_doc_scores WHERE doc_path = ?`, path); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = ? OR (dst_kind = 'note' AND dst_path = ?)`, path, path); err != nil {
		return nil, err
	}
	if err := deleteMetadataRowsForPaths(ctx, tx, []string{path}, true); err != nil {
		return nil, err
	}
	if err := deleteOrphanedOwnershipAnchorsTx(ctx, tx, affectedAnchorIDs); err != nil {
		return nil, err
	}
	return affectedAnchorIDs, nil
}

func deleteDocLinksToAnchorIDsTx(ctx context.Context, tx *sql.Tx, anchorIDs []string) error {
	return deleteOwnershipRowsByTextIDsTx(ctx, tx, "doc_links", "dst_id", "dst_kind = 'anchor'", anchorIDs)
}

func deleteGraphAnchorScoresTx(ctx context.Context, tx *sql.Tx, anchorIDs []string) error {
	return deleteOwnershipRowsByTextIDsTx(ctx, tx, "graph_anchor_scores", "anchor_id", "", anchorIDs)
}

func deleteOwnershipRowsByTextIDsTx(ctx context.Context, tx *sql.Tx, table, column, predicate string, ids []string) error {
	for start := 0; start < len(ids); start += 400 {
		end := start + 400
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		query := fmt.Sprintf("DELETE FROM %s WHERE %s IN (%s)", table, column, holders)
		if predicate != "" {
			query += " AND " + predicate
		}
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

func selectOntologyNodeRowIDsByPathsTx(ctx context.Context, tx *sql.Tx, paths []string) ([]int64, error) {
	var out []int64
	for start := 0; start < len(paths); start += 400 {
		end := start + 400
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i, path := range batch {
			args[i] = path
		}
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT id FROM ontology_nodes WHERE note_path IN (%s)`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
