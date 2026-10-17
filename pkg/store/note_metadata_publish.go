package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
)

func (s *Store) ReplaceNoteMetadataSnapshot(ctx context.Context, snapshot NoteMetadataSnapshot) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_note_metadata")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// A complete replacement also discards every prior alias namespace.
		if _, err := tx.ExecContext(ctx, `DELETE FROM ontology_note_state WHERE note_path IN (SELECT source_note_path FROM ontology_node_field_value_dependencies)`); err != nil {
			return err
		}
		for _, stmt := range []string{
			`DELETE FROM note_projection_state`,
			`DELETE FROM note_property_values`,
			`DELETE FROM note_tags`,
			`DELETE FROM note_search_terms`,
			`DELETE FROM note_fragment_targets`,
			`DELETE FROM note_projection_diagnostics`,
			`DELETE FROM note_search_regions`,
			`DELETE FROM note_link_inputs`,
			`DELETE FROM note_projection_payloads`,
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
		if err := insertNoteProjectionPayloads(ctx, tx, noteIDs, snapshot.ProjectionPayloads); err != nil {
			return err
		}
		if err := insertNoteLinkInputs(ctx, tx, noteIDs, snapshot.LinkInputs); err != nil {
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
		dependents, err := ontologyLinkDependentsBeforeTargetsChangeTx(ctx, tx, touchedPaths)
		if err != nil {
			return err
		}
		if err := invalidateOntologyNoteStatesTx(ctx, tx, dependents); err != nil {
			return err
		}
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
			if err := insertNoteProjectionPayloads(ctx, tx, noteIDs, delta.ProjectionPayloads); err != nil {
				return err
			}
			if err := insertNoteLinkInputs(ctx, tx, noteIDs, delta.LinkInputs); err != nil {
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
