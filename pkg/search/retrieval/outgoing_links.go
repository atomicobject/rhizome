package retrieval

import (
	"context"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// OutgoingLinkRetriever expands seed notes from persisted current provider
// facts. It does not read or parse authored note bytes: metadata projection is
// the sole producer of note-link evidence.
type OutgoingLinkRetriever struct {
	Store *semdb.Store
	Limit int
}

func (r *OutgoingLinkRetriever) Name() string { return "outgoing_links" }

func (r *OutgoingLinkRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if len(spec.Seeds) == 0 || r.Store == nil {
		return nil, nil
	}

	limit := r.Limit
	if limit <= 0 {
		limit = 25
	}

	seen := make(map[string]struct{})
	var out []search.Candidate

	for _, seed := range spec.Seeds {
		if seed.Kind != knowledge.KindNote && seed.Kind != knowledge.KindNoteChunk {
			continue
		}
		seedPath, ok := cleanTypedNotePath(seed.ID)
		if !ok {
			continue
		}
		edges, err := r.Store.GraphDocNoteEdgesForPaths(ctx, []string{seedPath})
		if err != nil {
			return nil, err
		}
		if len(edges) == 0 {
			continue
		}
		paths := []string{seedPath}
		for _, edge := range edges {
			if edge.SrcPath == seedPath {
				paths = append(paths, edge.DstPath)
			}
		}
		current, err := r.Store.CurrentNoteMetadataRowsByPaths(ctx, paths)
		if err != nil {
			return nil, err
		}
		if !currentProjectionRow(current[seedPath]) {
			continue
		}
		for _, edge := range edges {
			if edge.SrcPath != seedPath {
				continue
			}
			dst, ok := cleanTypedNotePath(edge.DstPath)
			if !ok || dst == seedPath || !currentProjectionRow(current[dst]) {
				continue
			}
			key := "note:" + dst
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			h := knowledge.NoteHandle(dst)
			out = append(out, search.Candidate{
				Handle: h,
				Owner:  h,
				Evidence: []search.Evidence{{
					Type:     "direct_link_out",
					RawScore: 0.6,
					Source:   "persisted_note_links",
					Details: map[string]string{
						"seed": seedPath,
					},
				}},
				Type:       "note",
				NoteID:     dst,
				Path:       dst,
				Title:      titleFromPath(dst),
				ChunkIndex: -1,
			})
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func currentProjectionRow(row semdb.NoteMetadataRow) bool {
	return row.Projection.Status == semdb.NoteProjectionStatusCurrent &&
		row.Projection.SourceContentHash != "" &&
		row.Projection.SourceContentHash == row.ContentHash
}
