package embedindex

import (
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
)

func (s *Syncer) buildModuleTask(id codeindex.AnchorID, fingerprint, path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, content []byte) codeTask {
	chunks := s.Policy.BuildModuleChunks(path, lang, anchors, content)
	if len(chunks) == 0 {
		chunks = []SemanticChunk{(ChunkBuilder{Budget: s.Budget}).BuildModuleChunk(path, lang, anchors, relatedDocsForModule(anchors, nil))}
	}
	return codeTask{
		id:          id,
		fingerprint: fingerprint,
		payload:     codeTaskPayload{ownerKind: "module", chunks: chunks},
	}
}

func (s *Syncer) buildAnchorTask(anchor codeanchor.IntelAnchor, content []byte, moduleDoc string, rationale *pathRationale) codeTask {
	chunks := s.Policy.BuildAnchorChunks(anchor, content, anchor.RelatedDocs, SymbolChunkContext{ModuleDoc: moduleDoc, Rationale: rationale.inSpan(anchor)})
	return codeTask{
		id:          codeindex.AnchorID(anchor.AnchorID),
		fingerprint: anchor.Fingerprint,
		payload:     codeTaskPayload{ownerKind: strings.TrimSpace(anchor.Kind), chunks: chunks},
	}
}
