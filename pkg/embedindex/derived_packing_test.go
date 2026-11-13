package embedindex

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/stretchr/testify/require"
)

func TestDerivedPlansPackAndFlushPhysicalSharedRequests(t *testing.T) {
	for _, kind := range []string{"notes", "ontology"} {
		for _, tc := range []struct {
			name            string
			texts, maxBytes int
			want            []int
		}{
			{"request limit", 1500, 1 << 20, []int{1000, 500}},
			{"byte limit", 8, 12, []int{3, 3, 2}},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				p := &defaultingProvider{batchSize: 1000, maxConcurrency: 1}
				opts := EmbedPackerOptions{MinTexts: 512, MaxTexts: 1000, MaxBytes: tc.maxBytes, MaxWait: time.Hour}
				node := NewSharedEmbeddingNode(t.Context(), p, 1, nil, opts)
				defer node.Close()
				texts := make([]string, tc.texts)
				for i := range texts {
					texts[i] = fmt.Sprintf("%04d", i)
				}
				if kind == "ontology" {
					result, err := OntologyNodeSyncer{Provider: p, EmbeddingNode: node}.ComputeDerived(t.Context(), OntologyDerivedPlan{texts: texts})
					require.NoError(t, err)
					require.Len(t, result.vectors, len(texts))
				} else {
					s := NoteSyncer{Provider: p, EmbeddingNode: node, NoteEmbedPacker: &opts}
					plan := NotePlan{TotalWork: len(texts)}
					for i, text := range texts {
						plan.tasks = append(plan.tasks, noteTask{id: embeddings.NoteID(text), payload: noteTaskPayload{embedChunks: []embeddings.ChunkInput{embeddings.NewChunkInput(i, text, "T", "T")}, embedTexts: []string{text}}})
					}
					count := 0
					require.NoError(t, s.ComputeDerived(t.Context(), plan, func(context.Context, NoteDerivedResult) error { count++; return nil }))
					require.Equal(t, len(texts), count)
				}
				require.Equal(t, tc.want, p.BatchSizes())
			})
		}
	}
}
