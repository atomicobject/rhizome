package semantic

import (
	"strings"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// BuildRootEvidenceChunks creates root-owned semantic chunks from a format
// provider's search regions. It never interprets the authored source bytes.
func BuildRootEvidenceChunks(root *ontology.RootDocumentSnapshot, owner codeanchor.IntelOntologyNode, provider embeddings.ProviderConfig, maxBytes int, now int64) OntologyNodeChunkSet {
	out := OntologyNodeChunkSet{Texts: map[string]string{}}
	if root == nil || strings.TrimSpace(owner.NodeID) == "" {
		return out
	}
	if maxBytes <= 0 {
		maxBytes = defaultSectionMaxBytes
	}
	title := strings.TrimSpace(owner.Title)
	ordinal := 0
	var titleOnly *noteformat.SearchRegionFact
	appendRegion := func(region noteformat.SearchRegionFact, text string) {
		granularity := GranularityOntologyNodeVisible
		if region.Kind == noteformat.SearchRegionSupplemental {
			granularity = GranularityOntologyNodeSupplemental
		}
		for _, part := range splitRootEvidence(text, maxBytes) {
			body := rootEvidenceChunkText(root, owner, region, part)
			chunkID := codeanchor.IntelChunkID(owner.NodeID, ordinal, granularity)
			chunk := codeanchor.IntelChunk{
				ChunkID: chunkID, OwnerID: owner.NodeID, OwnerType: OntologyNodeOwnerType,
				Ord: ordinal, Granularity: granularity, Breadcrumb: ontologyNodeBreadcrumb(owner),
				Heading: title, ContentHash: embeddings.HashText(body), UpdatedAt: now,
			}
			if region.Range.Present {
				chunk.StartByte = int64(region.Range.Range.StartByte)
				chunk.EndByte = int64(region.Range.Range.EndByte)
			}
			out.Chunks = append(out.Chunks, chunk)
			out.Texts[chunkID] = body
			out.States = append(out.States, codeanchor.IntelOntologyNodeEmbeddingState{
				ChunkID: chunkID, NodeID: owner.NodeID, NotePath: root.NotePath.String(),
				TypeName: owner.TypeName, NodeKind: owner.NodeKind,
				EmbeddingSchemaSignature: hashString("root-evidence-v1:" + string(root.Format)),
				NodeStructureFingerprint: owner.StructuralFingerprint,
				SourceContentHash:        root.ContentHash, ChunkTextHash: chunk.ContentHash,
				ChunkGranularity: granularity, Provider: provider.Provider, Model: provider.Model,
				UpdatedAt: now,
			})
			ordinal++
		}
	}
	for _, region := range root.SearchRegions {
		text := strings.TrimSpace(region.Text)
		if text == "" {
			continue
		}
		if sameEvidenceText(text, title) {
			if titleOnly == nil {
				candidate := region
				titleOnly = &candidate
			}
			continue
		}
		appendRegion(region, text)
	}
	if len(out.Chunks) == 0 && titleOnly != nil {
		appendRegion(*titleOnly, strings.TrimSpace(titleOnly.Text))
	}
	if len(out.Chunks) > 0 {
		out.NotePaths = []string{root.NotePath.String()}
		out.Nodes = []codeanchor.IntelOntologyNode{owner}
	}
	return out
}

func rootEvidenceChunkText(root *ontology.RootDocumentSnapshot, owner codeanchor.IntelOntologyNode, region noteformat.SearchRegionFact, body string) string {
	return strings.Join([]string{
		"Kind: ontology_node",
		"NodeKind: " + owner.NodeKind,
		"Type: " + owner.TypeName,
		"Title: " + owner.Title,
		"Path: " + root.NotePath.String(),
		"Evidence: " + string(region.Kind),
		"MediaType: " + region.MediaType,
		"",
		body,
	}, "\n")
}

func splitRootEvidence(text string, maxBytes int) []string {
	if len(text) <= maxBytes {
		return []string{text}
	}
	var out []string
	for len(text) > 0 {
		end := min(len(text), maxBytes)
		for end > 0 && !utf8.ValidString(text[:end]) {
			end--
		}
		if end == 0 {
			break
		}
		out = append(out, strings.TrimSpace(text[:end]))
		text = text[end:]
	}
	return out
}

func sameEvidenceText(left, right string) bool {
	normalize := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	return right != "" && normalize(left) == normalize(right)
}
