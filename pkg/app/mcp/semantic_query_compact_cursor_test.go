package mcp

import (
	"encoding/base64"
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

func TestCompactSemanticQueryFitsVectorContinuationIn16000Characters(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	vector := make([]float32, 1024)
	for i := range vector {
		vector[i] = float32(rng.NormFloat64() / 32)
	}
	cursor := unifiedsearch.ContinuationCursor{Version: 2, RequestIdentity: "request", IndexGeneration: "generation", WindowDigest: "window", CandidateWindow: 240, Offset: 2,
		QueryEmbeddings: []semantic.QueryEmbeddingRecord{{Text: "What do we know about onboarding and reusable IP?", Code: vector, Note: vector}}}
	raw, err := json.Marshal(cursor)
	require.NoError(t, err)
	response := compactSemanticQueryResponse(semanticQueryResponse{
		Count: 2, ContinuationToken: base64.RawURLEncoding.EncodeToString(raw),
		Warnings:   []search.Warning{{Code: "test-warning", Message: "Preserve this diagnostic."}},
		MustRead:   []answer.Item{{Type: "note", Path: "Opportunity.md"}},
		Supporting: []answer.Item{{Type: "note", Path: "Evidence.md"}},
		Matches: []SemanticMatchPayload{
			{Type: "note", Path: "Opportunity.md", FullFileContent: "A reusable IP opportunity.", ContentKind: string(planFull)},
			{Type: "note", Path: "Evidence.md", FullFileContent: "An independently sourced observation.", ContentKind: string(planFull)},
		},
	})
	_, err = marshalSemanticQueryResponse(response, 16000)
	require.ErrorContains(t, err, "preserving pagination and diagnostics")
	before, err := marshalSemanticQueryResponse(response, 64000)
	require.NoError(t, err)
	response.ContinuationToken, err = unifiedsearch.EncodeContinuation(cursor)
	require.NoError(t, err)
	after, err := marshalSemanticQueryResponse(response, 16000)
	require.NoError(t, err)
	t.Logf("compact response: before=%d chars, after=%d chars", len(before), len(after))
	var got semanticQueryResponse
	require.NoError(t, json.Unmarshal(after, &got))
	require.Equal(t, response.Warnings, got.Warnings)
	require.Equal(t, response.Compact, got.Compact)
	decoded, err := unifiedsearch.DecodeContinuation(got.ContinuationToken)
	require.NoError(t, err)
	require.Equal(t, cursor, decoded)
}
