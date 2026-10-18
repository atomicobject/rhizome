package search

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// SourceRef is the source-owned identity and display metadata shared by
// retrieval results and candidates. NoteID identifies the owning note path;
// NodeID is the retriever's node owner ID, distinct from NodeRef's authored ID.
// Ontology navigation, type and locator come from NodeRef alone. Catalog
// parent identity stays separate from the authored ancestry in NodeRef.
type SourceRef struct {
	Path         string
	Title        string
	Symbol       string
	FQN          string
	Kind         string
	Granularity  string
	ChunkIndex   int
	Breadcrumb   string
	Heading      string
	AnchorID     string
	NoteID       string
	NodeID       string
	ParentNodeID string // Persisted catalog parent ID, distinct from NodeRef.ParentID
	NodeRef      *ontology.NodeRef
}

// FillFrom keeps the first known metadata and fills only missing fields.
func (r *SourceRef) FillFrom(other SourceRef) {
	for _, field := range []struct {
		dst *string
		src string
	}{
		{&r.Path, other.Path},
		{&r.Title, other.Title},
		{&r.Symbol, other.Symbol},
		{&r.FQN, other.FQN},
		{&r.Kind, other.Kind},
		{&r.Granularity, other.Granularity},
		{&r.Breadcrumb, other.Breadcrumb},
		{&r.Heading, other.Heading},
		{&r.AnchorID, other.AnchorID},
		{&r.NoteID, other.NoteID},
		{&r.NodeID, other.NodeID},
		{&r.ParentNodeID, other.ParentNodeID},
	} {
		if strings.TrimSpace(*field.dst) == "" {
			*field.dst = field.src
		}
	}
	if r.ChunkIndex < 0 && other.ChunkIndex >= 0 {
		r.ChunkIndex = other.ChunkIndex
	}
	if r.NodeRef == nil {
		r.NodeRef = other.NodeRef
	}
}
