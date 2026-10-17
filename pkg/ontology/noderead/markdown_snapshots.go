package noderead

import (
	"context"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// markdownStructuralSnapshotCompat is the explicit syntax boundary for the
// supported Markdown structural adapter. Registered sources need an executable
// structural provider; legacy rows can enter only when no provider owns the path.
func (s *Scope) markdownStructuralSnapshotCompat(ctx context.Context, row semdb.NoteMetadataRow) (*ontology.DocumentSnapshot, *ontology.RootDocumentSnapshot, error) {
	format := noteformat.FormatID(row.FormatID)
	if format != noteformat.FormatID("markdown") || row.Projection.Status != semdb.NoteProjectionStatusCurrent {
		return nil, nil, nil
	}
	if provider, registered := s.service.NoteFormats.Provider(format); registered {
		if !s.service.NoteFormats.CanProject(format) || !provider.Descriptor().Capabilities.Has(noteformat.CapabilityStructuralContentMutation) {
			return nil, nil, nil
		}
		root, err := s.providerRootSnapshotLocked(ctx, row)
		if err != nil || root == nil {
			return nil, nil, err
		}
		snapshot, err := ontology.BuildMarkdownDocumentSnapshot(root)
		return snapshot, root, err
	}
	if _, selected := s.service.NoteFormats.ProviderForPath(paths.RelPath(row.Path)); selected {
		return nil, nil, nil
	}
	snapshot, err := ontology.LoadDocumentSnapshot(ctx, s.service.VaultDef, s.service.NoteReader, row.Path)
	return snapshot, nil, err
}

// markdownOverlaySnapshotCompat admits syntax supplied by the explicitly
// Markdown preview contract. A foreign provider's raw source stays opaque.
func markdownOverlaySnapshotCompat(overlay *ReadOverlay, notePath string, note ReadOverlayNote) (*ontology.DocumentSnapshot, error) {
	if !overlay.IsMarkdownSource() {
		return nil, nil
	}
	return ontology.BuildDocumentSnapshot(strings.TrimSpace(notePath), note.Content, time.Unix(note.UpdatedAt, 0))
}
