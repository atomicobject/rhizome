package noderead

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"slices"
)

// ReadOverlay supplies one provider's preview-state content for a bounded set
// of touched notes. Markdown projection is allowed only when SourceFormat is
// exactly markdown; callers must not use a filename suffix as that proof.
//
// It does not own edits or session restore. Callers build it from a write-layer
// preview and pass it into noderead so Hydrate, TypeInstances, filters, sort, and
// paging observe one read contract.
type ReadOverlay struct {
	SourceFormat         noteformat.FormatID
	Notes                map[string]ReadOverlayNote
	UpdatedContentByPath map[string]string
	TouchedPaths         []string
	Index                *ReadOverlayIndex
	Status               string
	Conflicted           bool
	Rebased              bool
}

func (o *ReadOverlay) IsMarkdownSource() bool {
	return o != nil && o.SourceFormat == noteformat.FormatID("markdown")
}

func (o *ReadOverlay) Empty() bool {
	return o == nil || (o.Index == nil && len(o.Notes) == 0 && len(o.UpdatedContentByPath) == 0 && len(o.TouchedPaths) == 0)
}

// TouchesPath reports whether the overlay stages content for a note path.
func (o *ReadOverlay) TouchesPath(notePath string) bool {
	if o == nil || notePath == "" {
		return false
	}
	if o.Index != nil {
		if _, ok := o.Index.TouchedPaths[notePath]; ok {
			return true
		}
	}
	if _, ok := o.Notes[notePath]; ok {
		return true
	}
	if _, ok := o.UpdatedContentByPath[notePath]; ok {
		return true
	}
	return slices.Contains(o.TouchedPaths, notePath)
}

// StagedContent returns the staged source of a note the overlay touches; ok
// is false when the overlay leaves the note committed. A deleted note returns
// empty content.
func (o *ReadOverlay) StagedContent(notePath string) (content string, ok bool) {
	if o == nil || notePath == "" {
		return "", false
	}
	if note, found := o.Notes[notePath]; found {
		if note.Deleted {
			return "", true
		}
		return note.Content, true
	}
	if content, found := o.UpdatedContentByPath[notePath]; found {
		return content, true
	}
	if o.Index != nil {
		if snapshot := o.Index.SnapshotsByPath[notePath]; snapshot != nil {
			return snapshot.Content, true
		}
	}
	return "", false
}

// ReadOverlayNote represents one touched note in a ReadOverlay.
type ReadOverlayNote struct {
	Content   string
	Deleted   bool
	UpdatedAt int64
}

// ReadOverlayIndex is the reusable preview read model for touched edit-session
// documents. It is immutable after construction so web/session owners can share
// one index across request scopes for the same edit-session revision.
type ReadOverlayIndex struct {
	TouchedPaths           map[string]struct{}
	DeletedPaths           map[string]struct{}
	SnapshotsByPath        map[string]*ontology.DocumentSnapshot
	RecordsByRef           map[string]NodeRecord
	ItemsByType            map[string][]ontology.NodeListItem
	ItemKeysByTouchedPath  map[string]map[string]struct{}
	FieldValuesByRef       map[string]map[string][]string
	ProjectionCountByPath  map[string]int
	BuildProjectionCount   int
	BuildTouchedPathCount  int
	BuildOverlayRecordKeys []string
}
