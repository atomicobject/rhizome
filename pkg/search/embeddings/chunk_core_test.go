package embeddings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoreChunkBody_TrimsInteriorChunkOverlap(t *testing.T) {
	body := strings.Repeat("A", overlapChars) + "CORE" + strings.Repeat("B", overlapChars)
	chunkText := "Title: T\nPath: P\nHeadings: H\nChunk: 2/3\n\n" + body
	require.Equal(t, "CORE", CoreChunkBody(chunkText))
}

func TestCoreChunkBody_TrimsOnlySuffixOnFirstChunk(t *testing.T) {
	body := "CORE" + strings.Repeat("B", overlapChars)
	chunkText := "Title: T\nPath: P\nHeadings: H\nChunk: 1/3\n\n" + body
	require.Equal(t, "CORE", CoreChunkBody(chunkText))
}

func TestCoreChunkBody_TrimsOnlyPrefixOnLastChunk(t *testing.T) {
	body := strings.Repeat("A", overlapChars) + "CORE"
	chunkText := "Title: T\nPath: P\nHeadings: H\nChunk: 3/3\n\n" + body
	require.Equal(t, "CORE", CoreChunkBody(chunkText))
}

func TestCoreChunkBody_NoChunkHeaderNoTrim(t *testing.T) {
	chunkText := "Title: T\nPath: P\nHeadings: H\n\nBODY"
	require.Equal(t, "BODY", CoreChunkBody(chunkText))
}
