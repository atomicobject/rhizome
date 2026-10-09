package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	pathutil "github.com/atomicobject/rhizome/pkg/paths"
)

type NotePropertySource int

const (
	NotePropertySourceFrontmatter NotePropertySource = 1
	NotePropertySourceInline      NotePropertySource = 2
)

type NotePropertyValueKind int

const (
	NotePropertyValueUnknown NotePropertyValueKind = iota
	NotePropertyValueString
	NotePropertyValueBool
	NotePropertyValueInt
	NotePropertyValueFloat
	NotePropertyValueDate
	NotePropertyValueDateTime
	NotePropertyValueURL
	NotePropertyValueWikilink
)

type NoteMetadataState struct {
	NotesHash    string
	RawNotesHash string
	LoadedAt     int64
	Ready        bool
}

type NoteMetadataRow struct {
	NoteID         int64
	Path           string
	Title          string
	ContentHash    string
	IndexerVersion string
	Mtime          int64
	Size           int64
	IndexedAt      int64
	FormatID       string
	Projection     NoteProjectionState
}

// NoteProjectionStatus records whether the provider projection associated
// with a current authored source is usable by downstream consumers.
type NoteProjectionStatus string

const (
	NoteProjectionStatusStale   NoteProjectionStatus = "stale"
	NoteProjectionStatusCurrent NoteProjectionStatus = "current"
	NoteProjectionStatusFatal   NoteProjectionStatus = "fatal"
)

// NoteProjectionState is format-provider provenance for one note source.
// Source identity remains on NoteMetadataRow; this state lets a current source
// remain durable while stale or fatal derived facts are unavailable.
type NoteProjectionState struct {
	ProviderVersion   string
	ProjectionVersion string
	SourceContentHash string
	Status            NoteProjectionStatus
	DiagnosticCode    string
	DiagnosticDetail  string
	UpdatedAt         int64
}

type NotePropertyValueRow struct {
	NoteID       int64
	NotePath     string
	PropertyName string
	Source       NotePropertySource
	ValueText    string
	ValueNorm    string
	ValueKind    NotePropertyValueKind
	IsList       bool
	ListOrdinal  int
}

type NoteTagRow struct {
	NoteID   int64
	NotePath string
	TagNorm  string
}

// DurableNoteFacts is one point-in-time read of the raw note facts needed for
// exact-note hydration while global metadata reconciliation is pending.
type DurableNoteFacts struct {
	MetadataRows          map[string]NoteMetadataRow
	PropertyValues        []NotePropertyValueRow
	Tags                  []NoteTagRow
	FragmentTargets       []NoteFragmentTargetRow
	ProjectionDiagnostics []NoteProjectionDiagnosticRow
	SearchRegions         []NoteSearchRegionRow
}

type noteMetadataQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type NoteSearchScope int

const (
	NoteSearchScopePath NoteSearchScope = iota + 1
	NoteSearchScopeTitle
	NoteSearchScopeContent
	NoteSearchScopeSegment
)

type NoteSearchTermRow struct {
	NoteID         int64
	Scope          NoteSearchScope
	SegmentOrdinal int
	TokenOrdinal   int
	TokenText      string
}

type NoteFragmentTargetKind string

const (
	NoteFragmentTargetHeading    NoteFragmentTargetKind = "heading"
	NoteFragmentTargetBlock      NoteFragmentTargetKind = "block"
	NoteFragmentTargetElementID  NoteFragmentTargetKind = "element_id"
	NoteFragmentTargetLegacyName NoteFragmentTargetKind = "legacy_name"
)

// NoteFragmentTargetRow is one provider-authored addressable fragment.
type NoteFragmentTargetRow struct {
	NoteID     int64
	NotePath   string
	Kind       NoteFragmentTargetKind
	Target     string
	TargetNorm string
	Ordinal    int
}

type NoteProjectionDiagnosticRow struct {
	NoteID            int64
	NotePath          string
	Ordinal           int
	Code              string
	Category          string
	Message           string
	RangePresent      bool
	StartByte         int
	EndByte           int
	Blocking          bool
	AffectedOperation string
}

type NoteSearchRegionRow struct {
	NoteID       int64
	NotePath     string
	Ordinal      int
	Origin       string
	Kind         string
	Text         string
	MediaType    string
	RangePresent bool
	StartByte    int
	EndByte      int
}

func IsEmptyListSentinel(row NotePropertyValueRow) bool {
	return row.IsList && row.ListOrdinal == 0 && row.ValueKind == NotePropertyValueUnknown &&
		strings.TrimSpace(row.ValueText) == "" && strings.TrimSpace(row.ValueNorm) == ""
}

type NoteMetadataSnapshot struct {
	State                 NoteMetadataState
	Notes                 []NoteMetadataRow
	PropertyValues        []NotePropertyValueRow
	Tags                  []NoteTagRow
	FragmentTargets       []NoteFragmentTargetRow
	ProjectionDiagnostics []NoteProjectionDiagnosticRow
	SearchRegions         []NoteSearchRegionRow
	WikilinkEdges         []GraphDocEdgeRow
}

type NoteMetadataDelta struct {
	State                 NoteMetadataState
	Notes                 []NoteMetadataRow
	PropertyValues        []NotePropertyValueRow
	Tags                  []NoteTagRow
	FragmentTargets       []NoteFragmentTargetRow
	ProjectionDiagnostics []NoteProjectionDiagnosticRow
	SearchRegions         []NoteSearchRegionRow
	WikilinkEdges         []GraphDocEdgeRow
	DeletedPaths          []string
	// SourceTouches refreshes mtime/size for notes whose content identity is
	// unchanged. Their derived rows are deliberately left untouched.
	SourceTouches []NoteSourceTouch
}

const sqliteValuesBatchMaxParams = 900

func (s *Store) ReplaceNoteMetadataSnapshot(ctx context.Context, snapshot NoteMetadataSnapshot) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_note_metadata")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			`DELETE FROM note_projection_state`,
			`DELETE FROM note_property_values`,
			`DELETE FROM note_tags`,
			`DELETE FROM note_search_terms`,
			`DELETE FROM note_fragment_targets`,
			`DELETE FROM note_projection_diagnostics`,
			`DELETE FROM note_search_regions`,
			`DELETE FROM property_keys`,
			`DELETE FROM note_metadata_state`,
			`DELETE FROM graph_doc_edges WHERE kind = 'wikilink' OR kind = 'mdlink' OR kind GLOB 'note_link:*'`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}

		noteIDs, err := upsertMetadataNotes(ctx, tx, snapshot.Notes, snapshot.State.LoadedAt)
		if err != nil {
			return err
		}
		if err := upsertNoteProjectionStates(ctx, tx, noteIDs, snapshot.Notes, snapshot.State.LoadedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE indexed_at > 0 AND indexed_at <> ?`, snapshot.State.LoadedAt); err != nil {
			return err
		}
		propertyIDs, err := ensurePropertyKeys(ctx, tx, propertyNames(snapshot.PropertyValues), snapshot.State.LoadedAt)
		if err != nil {
			return err
		}
		if err := insertNotePropertyValues(ctx, tx, noteIDs, propertyIDs, snapshot.PropertyValues); err != nil {
			return err
		}
		if err := insertNoteTags(ctx, tx, noteIDs, snapshot.Tags); err != nil {
			return err
		}
		if err := insertNoteSearchTerms(ctx, tx, noteIDs, searchableProjectionMetadataRows(snapshot.Notes)); err != nil {
			return err
		}
		if err := insertNoteFragmentTargets(ctx, tx, noteIDs, snapshot.FragmentTargets); err != nil {
			return err
		}
		if err := insertNoteProjectionDiagnostics(ctx, tx, noteIDs, snapshot.ProjectionDiagnostics); err != nil {
			return err
		}
		if err := insertNoteSearchRegions(ctx, tx, noteIDs, snapshot.SearchRegions); err != nil {
			return err
		}
		if err := s.replaceAllNoteRegionFTSTx(ctx, tx, snapshot.Notes, snapshot.SearchRegions); err != nil {
			return err
		}
		if err := insertGraphDocEdges(ctx, tx, snapshot.WikilinkEdges); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO note_metadata_state(notes_hash, raw_notes_hash, loaded_at, ready)
			VALUES (?, ?, ?, ?)
		`, snapshot.State.NotesHash, snapshot.State.RawNotesHash, snapshot.State.LoadedAt, boolToInt(snapshot.State.Ready)); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) ApplyNoteMetadataDelta(ctx context.Context, delta NoteMetadataDelta) error {
	ctx = indexingperf.WithOp(ctx, "intel.apply_note_metadata_delta")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		state, err := currentNoteMetadataStateTx(ctx, tx)
		if err != nil {
			return err
		}
		if delta.State.LoadedAt == 0 {
			delta.State.LoadedAt = state.LoadedAt
		}
		if delta.State.LoadedAt == 0 {
			return nil
		}
		delta.State.Ready = delta.State.Ready || state.Ready

		changedPaths := make([]string, 0, len(delta.Notes))
		for _, note := range delta.Notes {
			if path := strings.TrimSpace(note.Path); path != "" {
				changedPaths = append(changedPaths, path)
			}
		}
		deletedPaths := normalizeNonEmptyStrings(delta.DeletedPaths)
		touchedPaths := append([]string{}, changedPaths...)
		touchedPaths = append(touchedPaths, deletedPaths...)
		if err := s.replaceNoteRegionFTSPathsTx(ctx, tx, changedPaths, deletedPaths, delta.Notes, delta.SearchRegions); err != nil {
			return err
		}

		noteIDs := map[string]int64{}
		if len(changedPaths) > 0 {
			if err := deleteMetadataRowsForPaths(ctx, tx, changedPaths, false); err != nil {
				return err
			}
			noteIDs, err = upsertMetadataNotes(ctx, tx, delta.Notes, delta.State.LoadedAt)
			if err != nil {
				return err
			}
			if err := upsertNoteProjectionStates(ctx, tx, noteIDs, delta.Notes, delta.State.LoadedAt); err != nil {
				return err
			}
		}
		if len(deletedPaths) > 0 {
			if err := deleteMetadataRowsForPaths(ctx, tx, deletedPaths, true); err != nil {
				return err
			}
		}
		if len(changedPaths) > 0 {
			propertyIDs, err := ensurePropertyKeys(ctx, tx, propertyNames(delta.PropertyValues), delta.State.LoadedAt)
			if err != nil {
				return err
			}
			if err := insertNotePropertyValues(ctx, tx, noteIDs, propertyIDs, delta.PropertyValues); err != nil {
				return err
			}
			if err := insertNoteTags(ctx, tx, noteIDs, delta.Tags); err != nil {
				return err
			}
			if err := insertNoteSearchTerms(ctx, tx, noteIDs, searchableProjectionMetadataRows(delta.Notes)); err != nil {
				return err
			}
			if err := insertNoteFragmentTargets(ctx, tx, noteIDs, delta.FragmentTargets); err != nil {
				return err
			}
			if err := insertNoteProjectionDiagnostics(ctx, tx, noteIDs, delta.ProjectionDiagnostics); err != nil {
				return err
			}
			if err := insertNoteSearchRegions(ctx, tx, noteIDs, delta.SearchRegions); err != nil {
				return err
			}
		}
		if len(touchedPaths) > 0 {
			if err := deleteGraphEdgesForPaths(ctx, tx, changedPaths, deletedPaths); err != nil {
				return err
			}
			if err := insertGraphDocEdges(ctx, tx, delta.WikilinkEdges); err != nil {
				return err
			}
		}
		if err := applyNoteSourceTouches(ctx, tx, delta.SourceTouches, changedPaths, deletedPaths); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM note_metadata_state`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO note_metadata_state(notes_hash, raw_notes_hash, loaded_at, ready)
			VALUES (?, ?, ?, ?)
		`, delta.State.NotesHash, delta.State.RawNotesHash, delta.State.LoadedAt, boolToInt(delta.State.Ready)); err != nil {
			return err
		}
		return nil
	})
}

func upsertMetadataNotes(ctx context.Context, tx *sql.Tx, notes []NoteMetadataRow, loadedAt int64) (map[string]int64, error) {
	noteIDs := make(map[string]int64, len(notes))
	if len(notes) == 0 {
		return noteIDs, nil
	}
	paths := make([]string, 0, len(notes))
	seen := make(map[string]struct{}, len(notes))
	values := make([][]any, 0, len(notes))
	for _, note := range notes {
		path := strings.TrimSpace(note.Path)
		if path == "" {
			continue
		}
		fullKey, baseKey, pathLen := noteLookupKeys(path)
		values = append(values, []any{path, note.Title, note.ContentHash, note.Mtime, note.Size, loadedAt, fullKey, baseKey, pathLen, firstSegmentNorm(path), normalizeNoteFormatID(note.FormatID)})
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if err := execValuesBatch(ctx, tx, `
		INSERT INTO notes(path, title, content_hash, mtime, size, indexed_at, note_key_full, note_key_base, path_len, first_segment_norm, format_id)
		VALUES
	`, values, 11, `
		ON CONFLICT(path) DO UPDATE SET
			title = excluded.title,
			content_hash = excluded.content_hash,
			mtime = excluded.mtime,
			size = excluded.size,
			indexed_at = excluded.indexed_at,
			note_key_full = excluded.note_key_full,
			note_key_base = excluded.note_key_base,
			path_len = excluded.path_len,
			first_segment_norm = excluded.first_segment_norm,
			format_id = excluded.format_id
	`); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return noteIDs, nil
	}
	return selectIDsByValues(ctx, tx, "notes", "id", "path", paths)
}

func upsertNoteProjectionStates(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, notes []NoteMetadataRow, loadedAt int64) error {
	values := make([][]any, 0, len(notes))
	for _, note := range notes {
		noteID, ok := noteIDs[strings.TrimSpace(note.Path)]
		if !ok {
			continue
		}
		state, err := normalizeNoteProjectionState(note.Projection, note.ContentHash, loadedAt)
		if err != nil {
			return fmt.Errorf("note projection state for %s: %w", note.Path, err)
		}
		values = append(values, []any{
			noteID,
			state.ProviderVersion,
			state.ProjectionVersion,
			state.SourceContentHash,
			string(state.Status),
			state.DiagnosticCode,
			state.DiagnosticDetail,
			state.UpdatedAt,
		})
	}
	return execValuesBatch(ctx, tx, `
		INSERT INTO note_projection_state(
			note_id, provider_version, projection_version, source_content_hash,
			status, diagnostic_code, diagnostic_detail, updated_at
		) VALUES
	`, values, 8, `
		ON CONFLICT(note_id) DO UPDATE SET
			provider_version = excluded.provider_version,
			projection_version = excluded.projection_version,
			source_content_hash = excluded.source_content_hash,
			status = excluded.status,
			diagnostic_code = excluded.diagnostic_code,
			diagnostic_detail = excluded.diagnostic_detail,
			updated_at = excluded.updated_at
	`)
}

func normalizeNoteFormatID(formatID string) string {
	if formatID = strings.TrimSpace(formatID); formatID != "" {
		return strings.ToLower(formatID)
	}
	return "markdown"
}

func normalizeNoteProjectionState(state NoteProjectionState, contentHash string, loadedAt int64) (NoteProjectionState, error) {
	if state.Status == "" {
		state.Status = NoteProjectionStatusStale
	}
	state.ProviderVersion = strings.TrimSpace(state.ProviderVersion)
	state.ProjectionVersion = strings.TrimSpace(state.ProjectionVersion)
	state.SourceContentHash = strings.TrimSpace(state.SourceContentHash)
	contentHash = strings.TrimSpace(contentHash)
	state.DiagnosticCode = strings.TrimSpace(state.DiagnosticCode)
	state.DiagnosticDetail = strings.TrimSpace(state.DiagnosticDetail)
	switch state.Status {
	case NoteProjectionStatusFatal:
		if state.DiagnosticCode == "" || state.DiagnosticDetail == "" {
			return NoteProjectionState{}, fmt.Errorf("fatal projections require non-empty diagnostic code and detail")
		}
		if state.ProviderVersion == "" || state.ProjectionVersion == "" {
			return NoteProjectionState{}, fmt.Errorf("fatal projections require non-empty provider and projection versions")
		}
		if state.SourceContentHash != contentHash {
			return NoteProjectionState{}, fmt.Errorf("fatal projection source hash must match the note content hash when source is available")
		}
	case NoteProjectionStatusCurrent:
		if state.DiagnosticCode != "" || state.DiagnosticDetail != "" {
			return NoteProjectionState{}, fmt.Errorf("%s projections must not retain a blocking diagnostic", state.Status)
		}
		if state.ProviderVersion == "" || state.ProjectionVersion == "" {
			return NoteProjectionState{}, fmt.Errorf("current projections require non-empty provider and projection versions")
		}
		if contentHash == "" || state.SourceContentHash != contentHash {
			return NoteProjectionState{}, fmt.Errorf("current projection source hash must match the note content hash")
		}
	case NoteProjectionStatusStale:
		if state.DiagnosticCode != "" || state.DiagnosticDetail != "" {
			return NoteProjectionState{}, fmt.Errorf("%s projections must not retain a blocking diagnostic", state.Status)
		}
	default:
		return NoteProjectionState{}, fmt.Errorf("unsupported status %q", state.Status)
	}
	if state.UpdatedAt == 0 {
		state.UpdatedAt = loadedAt
	}
	if state.UpdatedAt < 0 {
		return NoteProjectionState{}, fmt.Errorf("updated_at must not be negative")
	}
	return state, nil
}

func propertyNames(rows []NotePropertyValueRow) []string {
	propertyNames := make([]string, 0)
	seenProperties := make(map[string]struct{})
	for _, row := range rows {
		name := strings.TrimSpace(row.PropertyName)
		if name == "" {
			continue
		}
		if _, ok := seenProperties[name]; ok {
			continue
		}
		seenProperties[name] = struct{}{}
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
	return propertyNames
}

func ensurePropertyKeys(ctx context.Context, tx *sql.Tx, propertyNames []string, updatedAt int64) (map[string]int64, error) {
	propertyIDs := make(map[string]int64, len(propertyNames))
	if len(propertyNames) == 0 {
		return propertyIDs, nil
	}
	values := make([][]any, 0, len(propertyNames))
	for _, name := range propertyNames {
		values = append(values, []any{name, updatedAt})
	}
	if err := execValuesBatch(ctx, tx, `
		INSERT INTO property_keys(property_name, updated_at)
		VALUES
	`, values, 2, `
		ON CONFLICT(property_name) DO UPDATE SET updated_at = excluded.updated_at
	`); err != nil {
		return nil, err
	}
	return selectIDsByValues(ctx, tx, "property_keys", "property_id", "property_name", propertyNames)
}

func insertNotePropertyValues(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, propertyIDs map[string]int64, rows []NotePropertyValueRow) error {
	if len(rows) == 0 {
		return nil
	}
	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		noteID, ok := noteIDs[row.NotePath]
		if !ok {
			continue
		}
		propertyID, ok := propertyIDs[strings.TrimSpace(row.PropertyName)]
		if !ok {
			continue
		}
		values = append(values, []any{
			noteID,
			propertyID,
			int(row.Source),
			row.ValueText,
			row.ValueNorm,
			int(row.ValueKind),
			boolToInt(row.IsList),
			row.ListOrdinal,
		})
	}
	return execValuesBatch(ctx, tx, `
		INSERT INTO note_property_values(
			note_id, property_id, source, value_text, value_norm, value_kind, is_list, list_ordinal
		) VALUES
	`, values, 8, "")
}

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

func execValuesBatch(ctx context.Context, tx *sql.Tx, prefix string, values [][]any, valuesPerRow int, suffix string) error {
	if len(values) == 0 {
		return nil
	}
	rowsPerBatch := sqliteValuesBatchMaxParams / valuesPerRow
	if rowsPerBatch < 1 {
		rowsPerBatch = 1
	}
	for start := 0; start < len(values); start += rowsPerBatch {
		end := start + rowsPerBatch
		if end > len(values) {
			end = len(values)
		}
		batch := values[start:end]
		args := make([]any, 0, len(batch)*valuesPerRow)
		var sql strings.Builder
		sql.WriteString(prefix)
		for i, row := range batch {
			if i > 0 {
				sql.WriteByte(',')
			}
			sql.WriteByte('(')
			for j := 0; j < valuesPerRow; j++ {
				if j > 0 {
					sql.WriteByte(',')
				}
				sql.WriteByte('?')
			}
			sql.WriteByte(')')
			args = append(args, row...)
		}
		sql.WriteString(suffix)
		if _, err := tx.ExecContext(ctx, sql.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func selectIDsByValues(ctx context.Context, tx *sql.Tx, tableName, idColumn, valueColumn string, values []string) (map[string]int64, error) {
	values = normalizeNonEmptyStrings(values)
	out := make(map[string]int64, len(values))
	if len(values) == 0 {
		return out, nil
	}
	rowsPerBatch := sqliteValuesBatchMaxParams
	for start := 0; start < len(values); start += rowsPerBatch {
		end := start + rowsPerBatch
		if end > len(values) {
			end = len(values)
		}
		batch := values[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT %s, %s FROM %s WHERE %s IN (%s)`, idColumn, valueColumn, tableName, valueColumn, holders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var value string
			if err := rows.Scan(&id, &value); err != nil {
				rows.Close()
				return nil, err
			}
			out[value] = id
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func deleteMetadataRowsForPaths(ctx context.Context, tx *sql.Tx, paths []string, deleteNotes bool) error {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil
	}
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(paths))
		batch := paths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := sliceAny(batch)
		for _, stmt := range []string{
			`DELETE FROM note_property_values WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_tags WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_search_terms WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_fragment_targets WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_projection_diagnostics WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_search_regions WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
		} {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(stmt, holders), args...); err != nil {
				return err
			}
		}
		if deleteNotes {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM notes WHERE path IN (%s)`, holders), args...); err != nil {
				return err
			}
		}
	}
	return nil
}

func deleteGraphEdgesForPaths(ctx context.Context, tx *sql.Tx, changedPaths, deletedPaths []string) error {
	changedPaths = normalizeNonEmptyStrings(changedPaths)
	deletedPaths = normalizeNonEmptyStrings(deletedPaths)
	for start := 0; start < len(changedPaths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(changedPaths))
		batch := changedPaths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM graph_doc_edges WHERE src_path IN (%s) AND (kind = 'wikilink' OR kind = 'mdlink' OR kind GLOB 'note_link:*')`, holders), sliceAny(batch)...); err != nil {
			return err
		}
	}
	maxDeletedPaths := sqliteValuesBatchMaxParams / 2
	for start := 0; start < len(deletedPaths); start += maxDeletedPaths {
		end := min(start+maxDeletedPaths, len(deletedPaths))
		batch := deletedPaths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := append(sliceAny(batch), sliceAny(batch)...)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM graph_doc_edges WHERE (src_path IN (%s) OR dst_path IN (%s)) AND (kind = 'wikilink' OR kind = 'mdlink' OR kind GLOB 'note_link:*')`, holders, holders), args...); err != nil {
			return err
		}
	}
	return nil
}

func noteLookupKeys(notePath string) (fullKey string, baseKey string, pathLen int) {
	notePath = string(pathutil.Normalize(strings.TrimSpace(notePath)))
	if notePath == "" {
		return "", "", 0
	}
	fullKey = strings.TrimSuffix(notePath, path.Ext(notePath))
	baseKey = path.Base(fullKey)
	return fullKey, baseKey, len(notePath)
}

func firstSegmentNorm(notePath string) string {
	notePath = string(pathutil.Normalize(strings.TrimSpace(notePath)))
	if notePath == "" {
		return ""
	}
	if parts := strings.SplitN(notePath, "/", 2); len(parts) > 0 {
		return strings.ToLower(strings.TrimSpace(parts[0]))
	}
	return strings.ToLower(notePath)
}

func buildNoteSearchTermRowsForNote(noteID int64, notePath, title string) []NoteSearchTermRow {
	rows := make([]NoteSearchTermRow, 0, 16)
	appendTerms := func(scope NoteSearchScope, segmentOrdinal int, text string) {
		for tokenOrdinal, token := range searchTokens(text) {
			rows = append(rows, NoteSearchTermRow{
				NoteID:         noteID,
				Scope:          scope,
				SegmentOrdinal: segmentOrdinal,
				TokenOrdinal:   tokenOrdinal,
				TokenText:      token,
			})
		}
	}

	pathLower := strings.ToLower(strings.TrimSpace(notePath))
	appendTerms(NoteSearchScopePath, 0, pathLower)
	appendTerms(NoteSearchScopeTitle, 0, strings.ToLower(strings.TrimSpace(title)))
	if parts := strings.SplitN(pathLower, "/", 2); len(parts) == 2 {
		appendTerms(NoteSearchScopeContent, 0, parts[1])
	}
	for segmentOrdinal, segment := range strings.Split(pathLower, "/") {
		appendTerms(NoteSearchScopeSegment, segmentOrdinal, segment)
	}
	return rows
}

func searchTokens(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}
	for _, r := range text {
		if searchTokenDelimiter(r) {
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return tokens
}

func searchTokenDelimiter(r rune) bool {
	switch r {
	case '/', '-', '_', ' ', '.', ',', '(', ')':
		return true
	default:
		return false
	}
}

func currentNoteMetadataStateTx(ctx context.Context, tx *sql.Tx) (NoteMetadataState, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT notes_hash, COALESCE(raw_notes_hash, ''), loaded_at, ready
		FROM note_metadata_state
		ORDER BY loaded_at DESC
		LIMIT 1
	`)
	var state NoteMetadataState
	var ready int
	err := row.Scan(&state.NotesHash, &state.RawNotesHash, &state.LoadedAt, &ready)
	if err == sql.ErrNoRows {
		return NoteMetadataState{}, nil
	}
	if err != nil {
		return NoteMetadataState{}, err
	}
	state.Ready = ready != 0
	return state, nil
}

func (s *Store) GetNoteMetadataState(ctx context.Context) (NoteMetadataState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT notes_hash, COALESCE(raw_notes_hash, ''), loaded_at, ready
		FROM note_metadata_state
		ORDER BY loaded_at DESC
		LIMIT 1
	`)
	var state NoteMetadataState
	var ready int
	err := row.Scan(&state.NotesHash, &state.RawNotesHash, &state.LoadedAt, &ready)
	if err == sql.ErrNoRows {
		return NoteMetadataState{}, nil
	}
	if err != nil {
		return NoteMetadataState{}, err
	}
	state.Ready = ready != 0
	return state, nil
}

func (s *Store) CurrentNoteMetadataRows(ctx context.Context) ([]NoteMetadataRow, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.path, COALESCE(n.title, ''), COALESCE(n.content_hash, ''), COALESCE(n.indexer_version, ''),
			n.mtime, n.size, n.indexed_at, COALESCE(n.format_id, 'markdown'),
			COALESCE(p.provider_version, ''), COALESCE(p.projection_version, ''), COALESCE(p.source_content_hash, ''),
			COALESCE(p.status, 'stale'), COALESCE(p.diagnostic_code, ''), COALESCE(p.diagnostic_detail, ''), COALESCE(p.updated_at, 0)
		FROM notes n
		LEFT JOIN note_projection_state p ON p.note_id = n.id
		WHERE n.indexed_at > 0
		ORDER BY n.path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteMetadataRow
	for rows.Next() {
		var row NoteMetadataRow
		if err := scanNoteMetadataRow(rows, &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type noteMetadataRowScanner interface {
	Scan(...any) error
}

func scanNoteMetadataRow(scanner noteMetadataRowScanner, row *NoteMetadataRow) error {
	return scanner.Scan(
		&row.NoteID, &row.Path, &row.Title, &row.ContentHash, &row.IndexerVersion,
		&row.Mtime, &row.Size, &row.IndexedAt, &row.FormatID,
		&row.Projection.ProviderVersion, &row.Projection.ProjectionVersion, &row.Projection.SourceContentHash,
		&row.Projection.Status, &row.Projection.DiagnosticCode, &row.Projection.DiagnosticDetail, &row.Projection.UpdatedAt,
	)
}

// CurrentNoteFragmentTargets returns persisted targets without deduplicating
// them. Optional path, kind, and normalized-target filters are conjunctive.
// TargetNorm uses explicit BINARY collation to preserve block-id case.
func (s *Store) CurrentNoteFragmentTargets(ctx context.Context, notePaths []string, kind NoteFragmentTargetKind, targetNorm string) ([]NoteFragmentTargetRow, error) {
	notePaths = normalizeNonEmptyStrings(notePaths)
	queryBatch := func(paths []string) ([]NoteFragmentTargetRow, error) {
		conditions := make([]string, 0, 3)
		args := make([]any, 0, len(paths)+2)
		if len(paths) > 0 {
			holders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
			conditions = append(conditions, "n.path IN ("+holders+")")
			args = append(args, sliceAny(paths)...)
		}
		if kind != "" {
			conditions = append(conditions, "t.target_kind = ?")
			args = append(args, string(kind))
		}
		if targetNorm != "" {
			conditions = append(conditions, "t.target_norm = ? COLLATE BINARY")
			args = append(args, targetNorm)
		}
		where := ""
		if len(conditions) > 0 {
			where = " WHERE " + strings.Join(conditions, " AND ")
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT t.note_id, n.path, t.target_kind, t.target_text, t.target_norm, t.ordinal
			FROM note_fragment_targets t
			JOIN notes n ON n.id = t.note_id
		`+where+`
			ORDER BY n.path, t.target_kind, t.target_norm COLLATE BINARY, t.ordinal, t.rowid
		`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []NoteFragmentTargetRow
		for rows.Next() {
			var row NoteFragmentTargetRow
			if err := rows.Scan(&row.NoteID, &row.NotePath, &row.Kind, &row.Target, &row.TargetNorm, &row.Ordinal); err != nil {
				return nil, err
			}
			out = append(out, row)
		}
		return out, rows.Err()
	}

	if len(notePaths) == 0 {
		return queryBatch(nil)
	}
	maxPaths := sqliteValuesBatchMaxParams - 2
	out := make([]NoteFragmentTargetRow, 0)
	for start := 0; start < len(notePaths); start += maxPaths {
		end := min(start+maxPaths, len(notePaths))
		rows, err := queryBatch(notePaths[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NotePath != out[j].NotePath {
			return out[i].NotePath < out[j].NotePath
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].TargetNorm != out[j].TargetNorm {
			return out[i].TargetNorm < out[j].TargetNorm
		}
		return out[i].Ordinal < out[j].Ordinal
	})
	return out, nil
}

func (s *Store) CurrentNoteMetadataPaths(ctx context.Context) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	return s.AllNoteMetadataPaths(ctx)
}

// AllNoteMetadataPaths returns materialized note rows without treating metadata
// readiness as a read barrier. Bootstrap replacement uses it to tombstone stale
// rows left behind by an interrupted or outdated derivation.
func (s *Store) AllNoteMetadataPaths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM notes
		WHERE indexed_at > 0
		ORDER BY path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]NoteMetadataRow, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return map[string]NoteMetadataRow{}, err
	}
	paths = normalizeNonEmptyStrings(paths)
	return s.noteMetadataRowsByPaths(ctx, paths)
}

// DurableNoteMetadataRowsByPaths returns exactly the requested materialized
// source rows without treating global metadata readiness as a read barrier.
// It is for reconciliation that must rebuild state after ownership transitions
// invalidate readiness; callers must not use it as a derived-fact read API.
func (s *Store) DurableNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]NoteMetadataRow, error) {
	canonical, err := canonicalNoteMetadataPaths(paths)
	if err != nil {
		return nil, err
	}
	return s.noteMetadataRowsByPaths(ctx, canonical)
}

// DurableNoteFactsByPaths reads metadata, property values, and tags for the
// requested canonical paths from one short SQLite read transaction. No
// partial facts are returned when any read or the transaction fails.
func (s *Store) DurableNoteFactsByPaths(ctx context.Context, paths []string) (DurableNoteFacts, error) {
	canonical, err := canonicalNoteMetadataPaths(dedupeStringsStable(paths))
	if err != nil {
		return DurableNoteFacts{}, err
	}
	if len(canonical) == 0 {
		return DurableNoteFacts{MetadataRows: map[string]NoteMetadataRow{}}, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DurableNoteFacts{}, err
	}
	defer func() { _ = tx.Rollback() }()

	facts := DurableNoteFacts{}
	facts.MetadataRows, err = noteMetadataRowsByPaths(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.PropertyValues, err = notePropertyValues(ctx, tx, canonical, nil, 0)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.Tags, err = noteTags(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.FragmentTargets, err = noteFragmentTargets(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.ProjectionDiagnostics, err = noteProjectionDiagnostics(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	facts.SearchRegions, err = noteSearchRegions(ctx, tx, canonical)
	if err != nil {
		return DurableNoteFacts{}, err
	}
	if err := tx.Commit(); err != nil {
		return DurableNoteFacts{}, err
	}
	return facts, nil
}

func noteFragmentTargets(ctx context.Context, queryer noteMetadataQueryer, paths []string) ([]NoteFragmentTargetRow, error) {
	var out []NoteFragmentTargetRow
	err := queryNoteFactsByPaths(ctx, queryer, paths, `
		SELECT t.note_id, n.path, t.target_kind, t.target_text, t.target_norm, t.ordinal
		FROM note_fragment_targets t JOIN notes n ON n.id = t.note_id
		WHERE n.path IN (%s) ORDER BY n.path, t.ordinal, t.target_kind, t.target_norm COLLATE BINARY
	`, func(rows *sql.Rows) error {
		var row NoteFragmentTargetRow
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.Kind, &row.Target, &row.TargetNorm, &row.Ordinal); err != nil {
			return err
		}
		out = append(out, row)
		return nil
	})
	return out, err
}

func noteProjectionDiagnostics(ctx context.Context, queryer noteMetadataQueryer, paths []string) ([]NoteProjectionDiagnosticRow, error) {
	var out []NoteProjectionDiagnosticRow
	err := queryNoteFactsByPaths(ctx, queryer, paths, `
		SELECT d.note_id, n.path, d.ordinal, d.code, d.category, d.message,
			d.range_present, d.start_byte, d.end_byte, d.blocking, d.affected_operation
		FROM note_projection_diagnostics d JOIN notes n ON n.id = d.note_id
		WHERE n.path IN (%s) ORDER BY n.path, d.ordinal
	`, func(rows *sql.Rows) error {
		var row NoteProjectionDiagnosticRow
		var rangePresent, blocking int
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.Ordinal, &row.Code, &row.Category, &row.Message,
			&rangePresent, &row.StartByte, &row.EndByte, &blocking, &row.AffectedOperation); err != nil {
			return err
		}
		row.RangePresent = rangePresent != 0
		row.Blocking = blocking != 0
		out = append(out, row)
		return nil
	})
	return out, err
}

func noteSearchRegions(ctx context.Context, queryer noteMetadataQueryer, paths []string) ([]NoteSearchRegionRow, error) {
	var out []NoteSearchRegionRow
	err := queryNoteFactsByPaths(ctx, queryer, paths, `
		SELECT r.note_id, n.path, r.ordinal, r.origin, r.region_kind, r.region_text,
			r.media_type, r.range_present, r.start_byte, r.end_byte
		FROM note_search_regions r JOIN notes n ON n.id = r.note_id
		WHERE n.path IN (%s) ORDER BY n.path, r.ordinal
	`, func(rows *sql.Rows) error {
		var row NoteSearchRegionRow
		var rangePresent int
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.Ordinal, &row.Origin, &row.Kind, &row.Text,
			&row.MediaType, &rangePresent, &row.StartByte, &row.EndByte); err != nil {
			return err
		}
		row.RangePresent = rangePresent != 0
		out = append(out, row)
		return nil
	})
	return out, err
}

func queryNoteFactsByPaths(ctx context.Context, queryer noteMetadataQueryer, paths []string, query string, scan func(*sql.Rows) error) error {
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(paths))
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := queryer.QueryContext(ctx, fmt.Sprintf(query, placeholders), sliceAny(batch)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			if err := scan(rows); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) noteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]NoteMetadataRow, error) {
	return noteMetadataRowsByPaths(ctx, s.db, paths)
}

func noteMetadataRowsByPaths(ctx context.Context, queryer noteMetadataQueryer, paths []string) (map[string]NoteMetadataRow, error) {
	if len(paths) == 0 {
		return map[string]NoteMetadataRow{}, nil
	}
	out := make(map[string]NoteMetadataRow, len(paths))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := queryer.QueryContext(ctx, fmt.Sprintf(`
			SELECT n.id, n.path, COALESCE(n.title, ''), COALESCE(n.content_hash, ''), COALESCE(n.indexer_version, ''),
				n.mtime, n.size, n.indexed_at, COALESCE(n.format_id, 'markdown'),
				COALESCE(p.provider_version, ''), COALESCE(p.projection_version, ''), COALESCE(p.source_content_hash, ''),
				COALESCE(p.status, 'stale'), COALESCE(p.diagnostic_code, ''), COALESCE(p.diagnostic_detail, ''), COALESCE(p.updated_at, 0)
			FROM notes n
			LEFT JOIN note_projection_state p ON p.note_id = n.id
			WHERE n.indexed_at > 0 AND n.path IN (%s)
		`, placeholders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row NoteMetadataRow
			if err := scanNoteMetadataRow(rows, &row); err != nil {
				rows.Close()
				return nil, err
			}
			out[row.Path] = row
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func canonicalNoteMetadataPaths(rawPaths []string) ([]string, error) {
	if len(rawPaths) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(rawPaths))
	seen := make(map[string]struct{}, len(rawPaths))
	for _, rawPath := range rawPaths {
		canonical, err := pathutil.CleanNotePath(rawPath)
		if err != nil || canonical.String() != rawPath {
			return nil, fmt.Errorf("note metadata path %q must be canonical and vault-relative", rawPath)
		}
		if _, duplicate := seen[canonical.String()]; duplicate {
			return nil, fmt.Errorf("note metadata path %q is repeated", canonical)
		}
		seen[canonical.String()] = struct{}{}
		paths = append(paths, canonical.String())
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *Store) ResolveStoredNoteLinks(ctx context.Context, links []string) (map[string]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return map[string]string{}, err
	}
	links = normalizeNonEmptyStrings(links)
	if len(links) == 0 {
		return map[string]string{}, nil
	}

	resolved := make(map[string]string, len(links))
	exactKeys := make([]string, 0, len(links))
	baseToOriginal := make(map[string][]string, len(links))
	for _, link := range links {
		key := normalizeNoteLookupKey(link)
		if key == "" {
			continue
		}
		if strings.Contains(key, "/") {
			exactKeys = append(exactKeys, key)
			baseToOriginal[path.Base(key)] = append(baseToOriginal[path.Base(key)], key)
			continue
		}
		baseToOriginal[key] = append(baseToOriginal[key], key)
	}

	if len(exactKeys) > 0 {
		rows, err := s.queryUniqueNotePathLookup(ctx, "note_key_full", exactKeys)
		if err != nil {
			return nil, err
		}
		for key, notePath := range rows {
			resolved[key] = notePath
		}
	}

	baseKeys := make([]string, 0, len(baseToOriginal))
	for baseKey, originals := range baseToOriginal {
		unresolved := false
		for _, original := range originals {
			if _, ok := resolved[original]; !ok {
				unresolved = true
				break
			}
		}
		if unresolved {
			baseKeys = append(baseKeys, baseKey)
		}
	}
	if len(baseKeys) == 0 {
		return resolved, nil
	}

	baseMatches, err := s.queryUniqueNotePathLookup(ctx, "note_key_base", baseKeys)
	if err != nil {
		return nil, err
	}
	for baseKey, notePath := range baseMatches {
		for _, original := range baseToOriginal[baseKey] {
			if _, ok := resolved[original]; !ok {
				resolved[original] = notePath
			}
		}
	}
	return resolved, nil
}

func normalizeNoteLookupKey(link string) string {
	link = strings.TrimSpace(link)
	if idx := strings.Index(link, "#"); idx >= 0 {
		link = link[:idx]
	}
	link = string(pathutil.Normalize(link))
	if link == "" {
		return ""
	}
	return strings.TrimSuffix(link, path.Ext(link))
}

func (s *Store) queryUniqueNotePathLookup(ctx context.Context, column string, keys []string) (map[string]string, error) {
	keys = normalizeNonEmptyStrings(keys)
	out := make(map[string]string, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	for start := 0; start < len(keys); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT %s, MIN(path)
			FROM notes
			WHERE indexed_at > 0 AND %s IN (%s)
			GROUP BY %s
			HAVING COUNT(*) = 1
			ORDER BY %s
		`, column, column, holders, column, column), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key string
			var notePath string
			if err := rows.Scan(&key, &notePath); err != nil {
				rows.Close()
				return nil, err
			}
			out[key] = notePath
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func (s *Store) CurrentNotePropertyValues(ctx context.Context, notePaths []string, onlyProperties []string, sourceFilter NotePropertySource) ([]NotePropertyValueRow, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	return s.notePropertyValues(ctx, notePaths, onlyProperties, sourceFilter)
}

// DurableNotePropertyValuesByPaths returns derived property rows for exactly
// the requested canonical paths without treating global metadata readiness as
// a barrier. Callers must first prove each requested note projection current.
func (s *Store) DurableNotePropertyValuesByPaths(ctx context.Context, notePaths []string, onlyProperties []string, sourceFilter NotePropertySource) ([]NotePropertyValueRow, error) {
	canonical, err := canonicalNoteMetadataPaths(notePaths)
	if err != nil {
		return nil, err
	}
	if len(canonical) == 0 {
		return []NotePropertyValueRow{}, nil
	}
	return s.notePropertyValues(ctx, canonical, onlyProperties, sourceFilter)
}

func (s *Store) notePropertyValues(ctx context.Context, notePaths []string, onlyProperties []string, sourceFilter NotePropertySource) ([]NotePropertyValueRow, error) {
	return notePropertyValues(ctx, s.db, notePaths, onlyProperties, sourceFilter)
}

func notePropertyValues(ctx context.Context, queryer noteMetadataQueryer, notePaths []string, onlyProperties []string, sourceFilter NotePropertySource) ([]NotePropertyValueRow, error) {
	hasNotePathFilter := len(notePaths) > 0
	notePaths = normalizeNonEmptyStrings(notePaths)
	if hasNotePathFilter && len(notePaths) == 0 {
		return []NotePropertyValueRow{}, nil
	}
	if len(notePaths) > sqliteValuesBatchMaxParams {
		out := make([]NotePropertyValueRow, 0, len(notePaths))
		for start := 0; start < len(notePaths); start += sqliteValuesBatchMaxParams {
			end := start + sqliteValuesBatchMaxParams
			if end > len(notePaths) {
				end = len(notePaths)
			}
			rows, err := notePropertyValues(ctx, queryer, notePaths[start:end], onlyProperties, sourceFilter)
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
		}
		return out, nil
	}
	conditions := []string{"n.indexed_at > 0"}
	args := []any{}
	if len(notePaths) > 0 {
		holders := strings.TrimSuffix(strings.Repeat("?,", len(notePaths)), ",")
		conditions = append(conditions, fmt.Sprintf("n.path IN (%s)", holders))
		args = append(args, sliceAny(notePaths)...)
	}
	if len(onlyProperties) > 0 {
		onlyProperties = normalizeNonEmptyStrings(onlyProperties)
		if len(onlyProperties) == 0 {
			return []NotePropertyValueRow{}, nil
		}
		for i := range onlyProperties {
			onlyProperties[i] = strings.ToLower(strings.TrimSpace(onlyProperties[i]))
		}
		holders := strings.TrimSuffix(strings.Repeat("?,", len(onlyProperties)), ",")
		conditions = append(conditions, fmt.Sprintf("k.property_name IN (%s)", holders))
		args = append(args, sliceAny(onlyProperties)...)
	}
	if sourceFilter != 0 {
		conditions = append(conditions, "v.source = ?")
		args = append(args, int(sourceFilter))
	}
	rows, err := queryer.QueryContext(ctx, fmt.Sprintf(`
		SELECT v.note_id, n.path, k.property_name, v.source, v.value_text, v.value_norm, v.value_kind, v.is_list, v.list_ordinal
		FROM note_property_values v
		JOIN notes n ON n.id = v.note_id
		JOIN property_keys k ON k.property_id = v.property_id
		WHERE %s
		ORDER BY n.path, k.property_name, v.source, v.list_ordinal, v.value_norm
	`, strings.Join(conditions, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotePropertyValueRow
	for rows.Next() {
		var row NotePropertyValueRow
		var source int
		var kind int
		var isList int
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.PropertyName, &source, &row.ValueText, &row.ValueNorm, &kind, &isList, &row.ListOrdinal); err != nil {
			return nil, err
		}
		row.Source = NotePropertySource(source)
		row.ValueKind = NotePropertyValueKind(kind)
		row.IsList = isList != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// CurrentNoteAliases returns a map from note path to the ordered list of
// `aliases:` frontmatter values for that note. Only notes with a populated
// frontmatter `aliases:` list appear in the result. Used to build
// alias-aware NotePathCache instances for wikilink resolution.
//
// This is a thin wrapper over CurrentNotePropertyValues scoped to
// property_name = "aliases" and source = frontmatter.
func (s *Store) CurrentNoteAliases(ctx context.Context) (map[string][]string, error) {
	rows, err := s.CurrentNotePropertyValues(ctx, nil, []string{"aliases"}, NotePropertySourceFrontmatter)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(rows))
	for _, row := range rows {
		value := strings.TrimSpace(row.ValueText)
		if value == "" {
			continue
		}
		out[row.NotePath] = append(out[row.NotePath], value)
	}
	return out, nil
}

func (s *Store) CurrentNoteTags(ctx context.Context, notePaths []string) ([]NoteTagRow, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	return s.noteTags(ctx, notePaths)
}

// DurableNoteTagsByPaths returns derived tags for exactly the requested
// canonical paths without treating global metadata readiness as a barrier.
// Callers must first prove each requested note projection current.
func (s *Store) DurableNoteTagsByPaths(ctx context.Context, notePaths []string) ([]NoteTagRow, error) {
	canonical, err := canonicalNoteMetadataPaths(notePaths)
	if err != nil {
		return nil, err
	}
	if len(canonical) == 0 {
		return []NoteTagRow{}, nil
	}
	return s.noteTags(ctx, canonical)
}

func (s *Store) noteTags(ctx context.Context, notePaths []string) ([]NoteTagRow, error) {
	return noteTags(ctx, s.db, notePaths)
}

func noteTags(ctx context.Context, queryer noteMetadataQueryer, notePaths []string) ([]NoteTagRow, error) {
	hasNotePathFilter := len(notePaths) > 0
	notePaths = normalizeNonEmptyStrings(notePaths)
	if hasNotePathFilter && len(notePaths) == 0 {
		return []NoteTagRow{}, nil
	}
	if len(notePaths) > sqliteValuesBatchMaxParams {
		out := make([]NoteTagRow, 0, len(notePaths))
		for start := 0; start < len(notePaths); start += sqliteValuesBatchMaxParams {
			end := start + sqliteValuesBatchMaxParams
			if end > len(notePaths) {
				end = len(notePaths)
			}
			rows, err := noteTags(ctx, queryer, notePaths[start:end])
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
		}
		return out, nil
	}
	conditions := []string{"n.indexed_at > 0"}
	args := []any{}
	if len(notePaths) > 0 {
		holders := strings.TrimSuffix(strings.Repeat("?,", len(notePaths)), ",")
		conditions = append(conditions, fmt.Sprintf("n.path IN (%s)", holders))
		args = append(args, sliceAny(notePaths)...)
	}
	rows, err := queryer.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.note_id, n.path, t.tag_norm
		FROM note_tags t
		JOIN notes n ON n.id = t.note_id
		WHERE %s
		ORDER BY n.path, t.tag_norm
	`, strings.Join(conditions, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteTagRow
	for rows.Next() {
		var row NoteTagRow
		if err := rows.Scan(&row.NoteID, &row.NotePath, &row.TagNorm); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsByPropertyValue(ctx context.Context, propertyName, valueNorm string, sourceFilter NotePropertySource) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	propertyName = strings.TrimSpace(propertyName)
	valueNorm = strings.TrimSpace(valueNorm)
	if propertyName == "" || valueNorm == "" {
		return []string{}, nil
	}
	propertyName = strings.ToLower(propertyName)
	conditions := []string{"n.indexed_at > 0", "k.property_name = ?", "v.value_norm = ?"}
	args := []any{propertyName, valueNorm}
	if sourceFilter != 0 {
		conditions = append(conditions, "v.source = ?")
		args = append(args, int(sourceFilter))
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT n.path
		FROM note_property_values v
		JOIN notes n ON n.id = v.note_id
		JOIN property_keys k ON k.property_id = v.property_id
		WHERE %s
		ORDER BY n.path
	`, strings.Join(conditions, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsByTag(ctx context.Context, tagNorm string) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	tagNorm = strings.TrimSpace(tagNorm)
	if tagNorm == "" {
		return []string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT n.path
		FROM note_tags t
		JOIN notes n ON n.id = t.note_id
		WHERE n.indexed_at > 0 AND (t.tag_norm = ? OR t.tag_norm LIKE ?)
		ORDER BY n.path
	`, tagNorm, tagNorm+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsByPathPrefix(ctx context.Context, normalizedPath string) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	normalizedPath = string(pathutil.Normalize(strings.TrimSpace(normalizedPath)))
	if normalizedPath == "" {
		return []string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM notes
		WHERE indexed_at > 0 AND (path = ? OR path LIKE ?)
		ORDER BY path
	`, normalizedPath, normalizedPath+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsByFirstSegmentPrefix(ctx context.Context, prefix string, exact bool) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return []string{}, nil
	}
	query := `
		SELECT path
		FROM notes
		WHERE indexed_at > 0 AND first_segment_norm = ?
		ORDER BY path
	`
	args := []any{prefix}
	if !exact {
		query = `
			SELECT path
			FROM notes
			WHERE indexed_at > 0 AND first_segment_norm LIKE ?
			ORDER BY path
		`
		args = []any{prefix + "%"}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsBySearchTokenPrefixes(ctx context.Context, scope NoteSearchScope, tokens []string, sameSegment bool) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	tokens = trimNonEmptyStrings(tokens)
	if len(tokens) == 0 {
		return []string{}, nil
	}

	var sql strings.Builder
	sql.WriteString(`SELECT DISTINCT n.path FROM note_search_terms t0 JOIN notes n ON n.id = t0.note_id `)
	args := make([]any, 0, len(tokens)+2)
	for i := 1; i < len(tokens); i++ {
		prev := i - 1
		sql.WriteString(fmt.Sprintf(
			`JOIN note_search_terms t%d ON t%d.note_id = t0.note_id AND t%d.scope = t0.scope AND t%d.token_ordinal > t%d.token_ordinal `,
			i, i, i, i, prev,
		))
		if sameSegment {
			sql.WriteString(fmt.Sprintf(`AND t%d.segment_ordinal = t0.segment_ordinal `, i))
		}
		sql.WriteString(fmt.Sprintf(`AND t%d.token_text LIKE ? `, i))
		args = append(args, tokens[i]+"%")
	}
	sql.WriteString(`WHERE n.indexed_at > 0 AND t0.scope = ? AND t0.token_text LIKE ? ORDER BY n.path`)
	args = append(args, int(scope), tokens[0]+"%")
	rows, err := s.db.QueryContext(ctx, sql.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsByFindPattern(ctx context.Context, pattern string) ([]string, bool, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, false, err
	}
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return []string{}, false, nil
	}
	if strings.ContainsAny(pattern, "*?") || strings.Count(pattern, "/") > 1 {
		return nil, false, nil
	}
	// This returns a candidate set, not final truth. The planner still runs
	// obsidian.FuzzyMatch on these paths so SQL can stay conservative without
	// changing matcher semantics.
	if strings.Contains(pattern, "/") {
		dirPattern, contentPattern := splitFindDirectoryAndContent(pattern)
		if strings.TrimSpace(dirPattern) == "" {
			return nil, false, nil
		}
		dirPaths, err := s.CurrentNotePathsByFirstSegmentPrefix(ctx, dirPattern, len(dirPattern) > 1)
		if err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(contentPattern) == "" {
			return dirPaths, true, nil
		}
		tokens := searchTokens(contentPattern)
		if len(tokens) == 0 {
			return nil, false, nil
		}
		contentPaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopeContent, tokens, false)
		if err != nil {
			return nil, false, err
		}
		return intersectSortedPaths(dirPaths, contentPaths), true, nil
	}

	tokens := searchTokens(pattern)
	if len(tokens) == 0 {
		return nil, false, nil
	}
	if strings.Contains(pattern, ".") {
		segmentPaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopeSegment, tokens, true)
		if err != nil {
			return nil, false, err
		}
		titlePaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopeTitle, tokens, false)
		if err != nil {
			return nil, false, err
		}
		return unionSortedPaths(segmentPaths, titlePaths), true, nil
	}

	pathPaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopePath, tokens, false)
	if err != nil {
		return nil, false, err
	}
	titlePaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopeTitle, tokens, false)
	if err != nil {
		return nil, false, err
	}
	return unionSortedPaths(pathPaths, titlePaths), true, nil
}

func splitFindDirectoryAndContent(pattern string) (string, string) {
	parts := strings.SplitN(pattern, "/", 2)
	dirPattern := parts[0]
	contentPattern := ""
	if len(parts) > 1 {
		contentPattern = parts[1]
	}
	return dirPattern, contentPattern
}

func unionSortedPaths(left, right []string) []string {
	if len(left) == 0 {
		return append([]string(nil), right...)
	}
	if len(right) == 0 {
		return append([]string(nil), left...)
	}
	out := make([]string, 0, len(left)+len(right))
	i, j := 0, 0
	for i < len(left) || j < len(right) {
		switch {
		case i >= len(left):
			out = append(out, right[j:]...)
			return dedupeSortedPaths(out)
		case j >= len(right):
			out = append(out, left[i:]...)
			return dedupeSortedPaths(out)
		case left[i] == right[j]:
			out = append(out, left[i])
			i++
			j++
		case left[i] < right[j]:
			out = append(out, left[i])
			i++
		default:
			out = append(out, right[j])
			j++
		}
	}
	return dedupeSortedPaths(out)
}

func intersectSortedPaths(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return []string{}
	}
	out := make([]string, 0, min(len(left), len(right)))
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		switch {
		case left[i] == right[j]:
			out = append(out, left[i])
			i++
			j++
		case left[i] < right[j]:
			i++
		default:
			j++
		}
	}
	return out
}

func dedupeSortedPaths(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	out := paths[:1]
	for _, path := range paths[1:] {
		if path == out[len(out)-1] {
			continue
		}
		out = append(out, path)
	}
	return out
}

func trimNonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
