package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func (s *Searcher) resultsFromScoredChunks(ctx context.Context, scored []semdb.ScoredChunk) ([]Result, error) {
	ownerTypes := make(map[string]string, len(scored))
	for _, sc := range scored {
		if _, ok := ownerTypes[sc.OwnerID]; !ok {
			ownerTypes[sc.OwnerID] = sc.OwnerType
		}
	}
	anchors, err := s.IntelStore.IntelAnchorsByIDs(ctx, func() []string {
		var ids []string
		for id, ot := range ownerTypes {
			if ot == "anchor" {
				ids = append(ids, id)
			}
		}
		return ids
	}())
	if err != nil {
		return nil, fmt.Errorf("load semantic anchor identity: %w", err)
	}
	var nodeIDs []string
	for id, ot := range ownerTypes {
		if ot == "ontology_node" {
			nodeIDs = append(nodeIDs, id)
		}
	}
	nodes, err := s.IntelStore.OntologyNodesByIDs(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}
	sectionIndexByPath := make(map[string]map[string]int)
	getSectionIndex := func(path, sectionID string) (int, bool) {
		if strings.TrimSpace(path) == "" || strings.TrimSpace(sectionID) == "" {
			return -1, false
		}
		idxMap, ok := sectionIndexByPath[path]
		if !ok {
			sectionIDs, err := s.IntelStore.IntelDocSectionIDsByPath(ctx, path)
			if err != nil || len(sectionIDs) == 0 {
				return -1, false
			}
			idxMap = make(map[string]int, len(sectionIDs))
			for i, sectionID := range sectionIDs {
				if strings.TrimSpace(sectionID) == "" {
					continue
				}
				idxMap[sectionID] = i
			}
			sectionIndexByPath[path] = idxMap
		}
		idx, ok := idxMap[sectionID]
		return idx, ok
	}

	results := make([]Result, 0, len(scored))
	for _, sc := range scored {
		chunkIndex := sc.Ord
		if sc.OwnerType == "doc_section" {
			if idx, ok := getSectionIndex(sc.Path, sc.OwnerID); ok {
				chunkIndex = idx
			} else {
				chunkIndex = -1
			}
		}
		result := Result{
			SourceRef: search.SourceRef{
				Path:        sc.Path,
				Breadcrumb:  sc.Breadcrumb,
				Heading:     sc.Heading,
				Granularity: sc.Granularity,
				ChunkIndex:  chunkIndex,
			},
			Score: sc.Score,
		}
		if sc.OwnerType == "anchor" {
			result.Type = "code"
			result.AnchorID = sc.OwnerID
			if anchor, ok := anchors[sc.OwnerID]; ok {
				result.Symbol = anchor.Symbol
				result.FQN = anchor.FQN
				result.Kind = anchor.Kind
				result.Path = anchor.Path
			}
			// Build handle for code chunks
			result.Handle = knowledge.CodeChunkHandle(sc.OwnerID, sc.Granularity, chunkIndex).String()
		} else if sc.OwnerType == "doc_section" {
			result.Type = "note"
			result.NoteID = sc.Path
			result.Title = sc.Heading
			// Build handle for note chunks
			if chunkIndex >= 0 {
				result.Handle = knowledge.NoteChunkHandle(sc.Path, chunkIndex).String()
			} else {
				result.Handle = knowledge.NoteHandle(sc.Path).String()
			}
		} else if sc.OwnerType == "ontology_node" {
			node, ok := nodes[sc.OwnerID]
			if !ok || strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.NotePath) == "" || strings.TrimSpace(node.NodeRefJSON) == "" || strings.TrimSpace(node.SourceLocator) == "" {
				continue
			}
			result.Type = "note"
			result.NodeID = sc.OwnerID
			result.ParentNodeID = node.ParentNodeID
			result.NoteID = sc.Path
			result.Title = sc.Heading
			result.Kind = "ontology_node"
			var ref ontology.NodeRef
			if err := json.Unmarshal([]byte(node.NodeRefJSON), &ref); err == nil && !ref.IsZero() {
				result.NodeRef = &ref
			}
			result.Path = node.NotePath
			result.NoteID = node.NotePath
			if strings.TrimSpace(node.Title) != "" {
				result.Title = node.Title
			}
			result.Handle = knowledge.NodeChunkHandle(sc.OwnerID, result.NoteID, sc.Granularity, chunkIndex).String()
		}
		if result.Handle == "" {
			continue
		}
		results = append(results, result)
	}
	return results, nil
}

func interleaveScoredChunksByOwner(scored []semdb.ScoredChunk, limit int) []semdb.ScoredChunk {
	if limit <= 0 || len(scored) <= limit {
		return scored
	}
	type bucket struct {
		key   string
		items []semdb.ScoredChunk
	}
	byKey := make(map[string]int, len(scored))
	buckets := make([]bucket, 0, len(scored))
	for _, sc := range scored {
		key := scoredChunkDiversityKey(sc)
		idx, ok := byKey[key]
		if !ok {
			idx = len(buckets)
			byKey[key] = idx
			buckets = append(buckets, bucket{key: key})
		}
		buckets[idx].items = append(buckets[idx].items, sc)
	}
	sort.SliceStable(buckets, func(i, j int) bool {
		left := buckets[i].items[0]
		right := buckets[j].items[0]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		return buckets[i].key < buckets[j].key
	})
	out := make([]semdb.ScoredChunk, 0, limit)
	for len(out) < limit {
		added := false
		for i := range buckets {
			if len(buckets[i].items) == 0 {
				continue
			}
			out = append(out, buckets[i].items[0])
			buckets[i].items = buckets[i].items[1:]
			added = true
			if len(out) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	return out
}

func scoredChunkDiversityKey(sc semdb.ScoredChunk) string {
	if (sc.OwnerType == "doc_section" || sc.OwnerType == "ontology_node") && strings.TrimSpace(sc.Path) != "" {
		return "note:" + sc.Path
	}
	return sc.OwnerType + ":" + sc.OwnerID
}
