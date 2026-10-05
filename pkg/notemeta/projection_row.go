package notemeta

import (
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// selectedProviderDescriptorForRow confirms that persisted source identity
// still selects its exact registered descriptor at the authored path.
func selectedProviderDescriptorForRow(formats noteformat.Runtime, row semdb.NoteMetadataRow) (noteformat.Descriptor, bool) {
	path, err := paths.CleanNotePath(row.Path)
	if err != nil || path.String() != row.Path {
		return noteformat.Descriptor{}, false
	}
	provider, known := formats.Provider(noteformat.FormatID(row.FormatID))
	selected, selectedForPath := formats.ProviderForPath(paths.RelPath(path))
	if !known || !selectedForPath {
		return noteformat.Descriptor{}, false
	}
	descriptor := provider.Descriptor()
	if row.FormatID != string(descriptor.ID) || descriptor.ID != selected.Descriptor().ID {
		return noteformat.Descriptor{}, false
	}
	return descriptor, true
}

// descriptorOnlyStaleProjection reports the expected durable state for a
// registered root-only provider. It is intentionally unavailable to all
// projection consumers, rather than an error that demands nonexistent work.
func descriptorOnlyStaleProjection(formats noteformat.Runtime, row semdb.NoteMetadataRow) bool {
	descriptor, selected := selectedProviderDescriptorForRow(formats, row)
	if !selected || formats.CanProject(descriptor.ID) {
		return false
	}
	return row.Projection.Status == semdb.NoteProjectionStatusStale &&
		row.ContentHash != "" &&
		row.Projection.SourceContentHash == row.ContentHash &&
		row.Projection.ProviderVersion == descriptor.ProviderVersion &&
		row.Projection.ProjectionVersion == descriptor.ProjectionVersion &&
		row.Projection.DiagnosticCode == "" &&
		row.Projection.DiagnosticDetail == ""
}
