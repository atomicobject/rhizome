package noderead

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// providerRootSnapshotLocked captures the selected provider's current facts
// once before structural projection or root-only projection. The caller holds s.mu.
func (s *Scope) providerRootSnapshotLocked(ctx context.Context, row semdb.NoteMetadataRow) (*ontology.RootDocumentSnapshot, error) {
	format := noteformat.FormatID(row.FormatID)
	provider, ok := s.service.NoteFormats.Provider(format)
	if !ok || !s.service.NoteFormats.CanProject(format) || s.service.NoteReader == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content, err := s.service.NoteReader.GetContents(s.service.VaultDef, row.Path)
	if err != nil {
		return nil, err
	}
	notePath, err := paths.CleanNotePath(row.Path)
	if err != nil {
		return nil, err
	}
	source, err := noteformat.NewAuthoredSource(notePath, provider.Descriptor(), []byte(content), row.Mtime)
	if err != nil {
		return nil, err
	}
	projected, err := s.service.NoteFormats.Project(source)
	if err != nil {
		return nil, err
	}
	metadata := make(map[string]any, len(projected.Facts.RootMetadata))
	for _, fact := range projected.Facts.RootMetadata {
		metadata[fact.Key] = fact.Value.Export()
	}
	title := row.Title
	if projected.Facts.Title != nil && strings.TrimSpace(projected.Facts.Title.Value) != "" {
		title = strings.TrimSpace(projected.Facts.Title.Value)
	}
	tags := make([]string, 0, len(projected.Facts.Tags))
	for _, tag := range projected.Facts.Tags {
		tags = append(tags, tag.Value)
	}
	inline := make(map[string][]string)
	for _, fact := range projected.Facts.InlineProperties {
		inline[fact.Key] = append(inline[fact.Key], fact.Value)
	}
	return &ontology.RootDocumentSnapshot{
		NotePath: notePath, Format: format, RawSource: []byte(content),
		ContentHash: row.ContentHash, Mtime: row.Mtime,
		SourceRepresentation:   ontology.SourceRepresentationUTF8,
		EvidenceRepresentation: ontology.EvidenceRepresentationProviderProjection,
		Projection:             projected.Copy(), Title: title, Metadata: metadata,
		Tags: tags, InlineProperties: inline,
		Root:          ontology.NodeRef{NotePath: row.Path, Kind: ontology.NodeKindNote},
		RootMetadata:  append([]noteformat.RootMetadataFact(nil), projected.Facts.RootMetadata...),
		SearchRegions: append([]noteformat.SearchRegionFact(nil), projected.Facts.SearchRegions...),
		Diagnostics:   append([]noteformat.Diagnostic(nil), projected.Diagnostics...),
	}, nil
}
