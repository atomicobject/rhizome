package notemeta

import (
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

// partitionProjectedEntries splits freshly projected entries by what the
// durable row still needs. A missing row or any content/provenance difference
// demands a full rewrite; a row that differs only in filesystem freshness
// evidence needs a source touch; an exact match needs nothing.
func partitionProjectedEntries(durable map[string]semdb.NoteMetadataRow, entries []projectedNoteEntry) ([]projectedNoteEntry, []semdb.NoteSourceTouch) {
	changed := make([]projectedNoteEntry, 0, len(entries))
	touches := make([]semdb.NoteSourceTouch, 0, len(entries))
	for _, entry := range entries {
		expected := projectionNoteRow(entry, 0)
		row, found := durable[expected.Path]
		if !found || !sameProjectionIdentity(row, expected) {
			changed = append(changed, entry)
			continue
		}
		if row.Mtime != expected.Mtime || row.Size != expected.Size {
			touches = append(touches, semdb.NoteSourceTouch{Path: expected.Path, Mtime: expected.Mtime, Size: expected.Size})
		}
	}
	return changed, touches
}

// sameProjectionIdentity compares content identity and provider/projection
// provenance. Mtime and Size are deliberately excluded: they are freshness
// evidence persisted on the row, not identity. A provider can still change a
// projection from current to fatal without changing the content hash, so every
// envelope field stays in the comparison.
func sameProjectionIdentity(durable, expected semdb.NoteMetadataRow) bool {
	return durable.Title == expected.Title &&
		durable.ContentHash == expected.ContentHash &&
		durable.FormatID == expected.FormatID &&
		durable.Projection.ProviderVersion == expected.Projection.ProviderVersion &&
		durable.Projection.ProjectionVersion == expected.Projection.ProjectionVersion &&
		durable.Projection.SourceContentHash == expected.Projection.SourceContentHash &&
		durable.Projection.Status == expected.Projection.Status &&
		durable.Projection.DiagnosticCode == expected.Projection.DiagnosticCode &&
		durable.Projection.DiagnosticDetail == expected.Projection.DiagnosticDetail
}
