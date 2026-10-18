package embedindex

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
)

type stubDocIntelSource struct {
	sections []codeanchor.IntelDocSection
}

func (s stubDocIntelSource) IntelAnchors(ctx context.Context) ([]codeanchor.IntelAnchor, error) {
	return nil, nil
}

func (s stubDocIntelSource) IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error) {
	return s.sections, nil
}

func (s stubDocIntelSource) IntelEdges(ctx context.Context) ([]codeanchor.IntelEdge, error) {
	return nil, nil
}

type stubDocIntelSourceByPath struct {
	byPath map[string][]codeanchor.IntelDocSection
}

type recordingNoteWriteQueue struct {
	syncs []embeddings.NoteChunkSync
	store *embsqlite.NoteStore
}

func (q *recordingNoteWriteQueue) SubmitNoteMeta(ctx context.Context, info embeddings.NoteFileInfo) error {
	if q.store != nil {
		return q.store.UpsertNoteMeta(ctx, info)
	}
	return nil
}

func (q *recordingNoteWriteQueue) SubmitNoteChunkSync(ctx context.Context, item embeddings.NoteChunkSync) error {
	q.syncs = append(q.syncs, item)
	if q.store != nil {
		return q.store.SyncNoteChunks(ctx, item)
	}
	return nil
}

func (q *recordingNoteWriteQueue) SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error {
	return nil
}

func (q *recordingNoteWriteQueue) SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error {
	return nil
}

func (q *recordingNoteWriteQueue) SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error {
	return nil
}

func (q *recordingNoteWriteQueue) FlushAndWait(context.Context) error {
	return nil
}

func (s stubDocIntelSourceByPath) IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error) {
	var sections []codeanchor.IntelDocSection
	for _, pathSections := range s.byPath {
		sections = append(sections, pathSections...)
	}
	return sections, nil
}

func (s stubDocIntelSourceByPath) IntelDocSectionsByPath(ctx context.Context, path string) ([]codeanchor.IntelDocSection, error) {
	return append([]codeanchor.IntelDocSection(nil), s.byPath[path]...), nil
}

type stubRawEligibility struct {
	typed map[string]bool
}

func (s stubRawEligibility) RawNoteEmbeddingEligibility(ctx context.Context, paths []string) (RawNoteEmbeddingEligibility, error) {
	out := RawNoteEmbeddingEligibility{RawEligible: map[string]bool{}}
	for _, path := range paths {
		out.RawEligible[path] = !s.typed[path]
		if s.typed[path] {
			out.TypedPaths = append(out.TypedPaths, path)
		}
	}
	return out, nil
}
