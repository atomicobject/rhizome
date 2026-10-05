package unifiedsearch

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

func TestContinuationCompressionPreservesVectorsAndExistingTokens(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	vector := make([]float32, 1024)
	for i := range vector {
		vector[i] = float32(rng.NormFloat64() / 32)
	}
	cursor := ContinuationCursor{Version: 2, RequestIdentity: "request", IndexGeneration: "generation", WindowDigest: "window", CandidateWindow: 240, Offset: 5,
		QueryEmbeddings: []semantic.QueryEmbeddingRecord{{Text: "What do we know about onboarding and reusable IP?", Code: vector, Note: vector}}}
	raw, err := json.Marshal(cursor)
	require.NoError(t, err)
	oldToken := base64.RawURLEncoding.EncodeToString(raw)
	token, err := EncodeContinuation(cursor)
	require.NoError(t, err)
	t.Logf("continuation: raw=%d chars, compressed=%d chars", len(oldToken), len(token))
	require.Greater(t, len(oldToken), 16000)
	require.Less(t, len(token), 8000)
	for _, encoded := range []string{oldToken, token} {
		decoded, err := DecodeContinuation(encoded)
		require.NoError(t, err)
		require.Equal(t, cursor, decoded, "compression must retain exact query vectors and all cursor bindings")
	}
}

func TestContinuationRejectsInvalidCompressedPayloads(t *testing.T) {
	var bomb bytes.Buffer
	w := zlib.NewWriter(&bomb)
	_, err := w.Write([]byte(strings.Repeat(" ", maxDecodedContinuationBytes+1)))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	for _, token := range []string{"z:!", "z:" + base64.RawURLEncoding.EncodeToString([]byte("invalid")), "z:" + base64.RawURLEncoding.EncodeToString(bomb.Bytes())} {
		_, err := DecodeContinuation(token)
		require.ErrorIs(t, err, ErrCursorInvalid)
	}
	valid, err := EncodeContinuation(ContinuationCursor{RequestIdentity: "r", IndexGeneration: "g", WindowDigest: "w", CandidateWindow: 100, Offset: 1})
	require.NoError(t, err)
	_, err = DecodeContinuation(valid[:len(valid)-5])
	require.ErrorIs(t, err, ErrCursorInvalid)
}

func TestContinuationCompressesMaximumVectorBatch(t *testing.T) {
	vector := make([]float32, 4096)
	for i := range vector {
		vector[i] = 0.12345678
	}
	cursor := ContinuationCursor{Version: 2, RequestIdentity: "request", IndexGeneration: "generation", WindowDigest: "window", CandidateWindow: 240, Offset: 5}
	for i := 0; i < 16; i++ {
		cursor.QueryEmbeddings = append(cursor.QueryEmbeddings, semantic.QueryEmbeddingRecord{Text: fmt.Sprintf("query %d", i), Code: vector, Note: vector})
	}
	raw, err := json.Marshal(cursor)
	require.NoError(t, err)
	require.Greater(t, len(raw), maxEncodedContinuationBytes)
	token, err := EncodeContinuation(cursor)
	require.NoError(t, err)
	require.Less(t, len(token), maxEncodedContinuationBytes)
	decoded, err := DecodeContinuation(token)
	require.NoError(t, err)
	require.Equal(t, cursor, decoded)
}
