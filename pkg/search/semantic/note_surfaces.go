package semantic

import (
	"context"
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type NoteSurfaceStore interface {
	OntologyNodesByPaths(context.Context, []string) ([]codeanchor.IntelOntologyNode, error)
	IntelChunksByOwners(context.Context, []string) ([]codeanchor.IntelChunk, error)
	IntelDocSectionsByPath(context.Context, string) ([]codeanchor.IntelDocSection, error)
	EmbeddingsByChunkIDs(context.Context, []string) (map[string]embeddings.Embedding, error)
}

func NoteSemanticChunks(ctx context.Context, store NoteSurfaceStore, notePath string, indexes map[int]struct{}, maxChunks int) ([]embeddings.StoredChunk, error) {
	notePath = strings.TrimSpace(notePath)
	if store == nil || notePath == "" {
		return nil, nil
	}
	nodes, err := store.OntologyNodesByPaths(ctx, []string{notePath})
	if err != nil {
		return nil, err
	}
	if len(nodes) > 0 {
		chunks, err := ontologyNodeSemanticChunks(ctx, store, nodes, indexes, maxChunks)
		if err != nil {
			return nil, err
		}
		if len(chunks) > 0 {
			return chunks, nil
		}
	}
	return docSectionSemanticChunks(ctx, store, notePath, indexes, maxChunks)
}

func ontologyNodeSemanticChunks(ctx context.Context, store NoteSurfaceStore, nodes []codeanchor.IntelOntologyNode, indexes map[int]struct{}, maxChunks int) ([]embeddings.StoredChunk, error) {
	ownerIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.NodeID) != "" {
			ownerIDs = append(ownerIDs, node.NodeID)
		}
	}
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	chunks, err := store.IntelChunksByOwners(ctx, ownerIDs)
	if err != nil {
		return nil, err
	}
	chunks = filterSemanticChunks(chunks, "ontology_node", indexes, maxChunks)
	return storedChunksWithEmbeddings(ctx, store, chunks, nil)
}

func docSectionSemanticChunks(ctx context.Context, store NoteSurfaceStore, notePath string, indexes map[int]struct{}, maxChunks int) ([]embeddings.StoredChunk, error) {
	sections, err := store.IntelDocSectionsByPath(ctx, notePath)
	if err != nil {
		return nil, fmt.Errorf("load note sections: %w", err)
	}
	if len(sections) == 0 {
		return nil, nil
	}
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].StartByte == sections[j].StartByte {
			return sections[i].SectionID < sections[j].SectionID
		}
		return sections[i].StartByte < sections[j].StartByte
	})
	sectionIndex := make(map[string]int, len(sections))
	ownerIDs := make([]string, 0, len(sections))
	for idx, sec := range sections {
		if strings.TrimSpace(sec.SectionID) == "" {
			continue
		}
		if len(indexes) > 0 {
			if _, ok := indexes[idx]; !ok {
				continue
			}
		}
		sectionIndex[sec.SectionID] = idx
		ownerIDs = append(ownerIDs, sec.SectionID)
	}
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	chunks, err := store.IntelChunksByOwners(ctx, ownerIDs)
	if err != nil {
		return nil, err
	}
	chunks = filterSemanticChunks(chunks, "doc_section", nil, maxChunks)
	return storedChunksWithEmbeddings(ctx, store, chunks, sectionIndex)
}

func filterSemanticChunks(chunks []codeanchor.IntelChunk, ownerType string, indexes map[int]struct{}, maxChunks int) []codeanchor.IntelChunk {
	filtered := make([]codeanchor.IntelChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if ownerType != "" && chunk.OwnerType != ownerType {
			continue
		}
		if len(indexes) > 0 {
			if _, ok := indexes[chunk.Ord]; !ok {
				continue
			}
		}
		filtered = append(filtered, chunk)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].OwnerID == filtered[j].OwnerID {
			if filtered[i].Ord == filtered[j].Ord {
				return filtered[i].ChunkID < filtered[j].ChunkID
			}
			return filtered[i].Ord < filtered[j].Ord
		}
		return filtered[i].OwnerID < filtered[j].OwnerID
	})
	if maxChunks > 0 && len(filtered) > maxChunks {
		filtered = filtered[:maxChunks]
	}
	return filtered
}

func storedChunksWithEmbeddings(ctx context.Context, store NoteSurfaceStore, chunks []codeanchor.IntelChunk, indexByOwner map[string]int) ([]embeddings.StoredChunk, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	chunkIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunkIDs = append(chunkIDs, chunk.ChunkID)
	}
	byChunk, err := store.EmbeddingsByChunkIDs(ctx, chunkIDs)
	if err != nil {
		return nil, err
	}
	out := make([]embeddings.StoredChunk, 0, len(chunks))
	for _, chunk := range chunks {
		emb := byChunk[chunk.ChunkID]
		if len(emb) == 0 {
			continue
		}
		idx := chunk.Ord
		if indexByOwner != nil {
			if mapped, ok := indexByOwner[chunk.OwnerID]; ok {
				idx = mapped
			}
		}
		out = append(out, embeddings.StoredChunk{
			Index:      idx,
			Breadcrumb: chunk.Breadcrumb,
			Heading:    chunk.Heading,
			Embedding:  emb,
		})
	}
	return out, nil
}
