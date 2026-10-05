package noderead

import (
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// withProjectionSource discloses the provider format and executable source
// capabilities on note-root projections. A root snapshot is authoritative even
// when its provider declares an empty capability set.
func (s *Scope) withProjectionSource(record NodeRecord, projection *ontology.NodeProjection) NodeRecord {
	if projection.Ref.Kind != ontology.NodeKindNote {
		return record
	}
	if root := projection.RootSnapshot; root != nil {
		record.Format = root.Format
		record.SourceRepresentation = root.SourceRepresentation
		record.EvidenceRepresentation = root.EvidenceRepresentation
		record.Capabilities = root.Projection.Capabilities.Values()
		return record
	}
	return s.withSourceContract(record)
}

// withSourceContract fills a note root's source contract from the active
// overlay or catalog row. A known provider may authoritatively expose zero
// capabilities; an entirely empty contract means no provider was resolved.
func (s *Scope) withSourceContract(record NodeRecord) NodeRecord {
	if s == nil || s.service == nil {
		return record
	}
	path := record.Ref.NotePath
	if overlay := s.activeOverlay(); overlay != nil && s.overlayHasPath(path) {
		record.Format = overlay.SourceFormat
		record.SourceRepresentation = ontology.SourceRepresentationUTF8
		record.EvidenceRepresentation = ontology.EvidenceRepresentationProviderProjection
		record.Capabilities = s.providerCapabilities(overlay.SourceFormat)
		return record
	}
	row, ok := s.noteRowByPath[path]
	if !ok {
		return record
	}
	record.Format = noteformat.FormatID(row.FormatID)
	record.SourceRepresentation = ontology.SourceRepresentationUTF8
	record.EvidenceRepresentation = ontology.EvidenceRepresentationProviderProjection
	record.Capabilities = s.sourceCapabilities(row)
	return record
}

func sourceContractEmpty(record NodeRecord) bool {
	return record.Format == "" &&
		record.SourceRepresentation == "" &&
		record.EvidenceRepresentation == "" &&
		len(record.Capabilities) == 0
}

func (s *Scope) providerCapabilities(format noteformat.FormatID) []noteformat.Capability {
	provider, ok := s.service.NoteFormats.Provider(format)
	if !ok || !s.service.NoteFormats.CanProject(format) {
		return nil
	}
	return provider.Descriptor().Capabilities.Values()
}

func (s *Scope) sourceCapabilities(row semdb.NoteMetadataRow) []noteformat.Capability {
	if s == nil || s.service == nil || row.Projection.Status != semdb.NoteProjectionStatusCurrent {
		return nil
	}
	return s.providerCapabilities(noteformat.FormatID(row.FormatID))
}
