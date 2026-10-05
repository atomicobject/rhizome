package contextpack

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPack_RespectsBudget(t *testing.T) {
	pieces := []Piece{
		{Key: "b", Priority: 10, Score: 9, Text: strings.Repeat("b", 50)},
		{Key: "z", Priority: 10, Score: 10, Text: "LAST"},
		{Key: "a", Priority: 10, Score: 10, Text: "FIRST"},
		{Key: "header", Priority: 100, Score: 0, Text: "HEADER"},
	}

	out, meta := Pack(pieces, 25)
	require.Equal(t, "HEADER\n\nFIRST\n\nLAST", out)
	require.Equal(t, []string{"header", "a", "z"}, meta.IncludedKeys)
	require.Equal(t, []string{"b"}, meta.OmittedKeys)
	require.Equal(t, len(out), meta.BudgetUsed)
	require.True(t, meta.Trimmed)
	require.Equal(t, 1, meta.OmittedPieces)
}

// mockCompressor is a test compressor that returns predictable output.
type mockCompressor struct {
	returnText    string
	returnError   error
	maxInputChars int
	requests      []CompressRequest
}

func (m *mockCompressor) Compress(ctx context.Context, req CompressRequest) (CompressResult, error) {
	m.requests = append(m.requests, req)
	if m.returnError != nil {
		return CompressResult{Error: m.returnError}, m.returnError
	}
	return CompressResult{
		Text:             m.returnText,
		Compressed:       true,
		CompressionRatio: 2.0,
		Provider:         "mock",
	}, nil
}

func (m *mockCompressor) MaxInputChars() int {
	if m.maxInputChars <= 0 {
		return 100000 // default for tests
	}
	return m.maxInputChars
}

func TestPackWithIntent_NoCompression(t *testing.T) {
	pieces := []Piece{
		{Key: "a", Priority: 10, Text: "short content"},
	}

	// No compressor provided - should behave like Pack
	text, meta := PackWithIntent(pieces, 1000, PackOptions{
		Intent: "test intent",
	})

	require.Contains(t, text, "short content")
	require.False(t, meta.Compressed)
	require.False(t, meta.Trimmed)
	var typedNil *mockCompressor
	text, meta = PackWithIntent(pieces, 1000, PackOptions{Compressor: typedNil})
	require.Equal(t, "short content", text)
	require.False(t, meta.Compressed)
	require.False(t, meta.Trimmed)
	compressor := &mockCompressor{returnText: "compressed"}
	text, meta = PackWithIntent(pieces, 1000, PackOptions{Compressor: compressor})
	require.Equal(t, "short content", text)
	require.False(t, meta.Compressed)
	require.Empty(t, compressor.requests)
	belowThreshold := []Piece{
		{Key: "a", Priority: 10, Text: strings.Repeat("a", 10)},
		{Key: "b", Priority: 9, Text: strings.Repeat("b", 10)},
		{Key: "c", Priority: 8, Text: strings.Repeat("c", 10)},
		{Key: "d", Priority: 7, Text: strings.Repeat("d", 10)},
		{Key: "e", Priority: 6, Text: strings.Repeat("e", 10)},
	}
	compressor = &mockCompressor{returnText: "should not be called"}
	text, meta = PackWithIntent(belowThreshold, 50, PackOptions{Compressor: compressor})
	require.Equal(t, strings.Join([]string{strings.Repeat("a", 10), strings.Repeat("b", 10), strings.Repeat("c", 10), strings.Repeat("d", 10)}, "\n\n"), text)
	require.True(t, meta.Trimmed)
	require.False(t, meta.Compressed)
	require.Equal(t, []string{"e"}, meta.OmittedKeys)
	require.Empty(t, compressor.requests)
}

func TestPackWithIntent_OverBudget_Compresses(t *testing.T) {
	// Use pieces where the first fits but second would be omitted
	// This triggers Trimmed=true which enables compression
	pieces := []Piece{
		{Key: "a", Priority: 10, Text: strings.Repeat("x", 30)},
		{Key: "b", Priority: 9, Text: strings.Repeat("y", 30)},
	}

	compressor := &mockCompressor{returnText: "compressed output"}

	// Budget 50 fits first piece (30) but not both (30+2+30=62), so second is omitted
	// This sets Trimmed=true, triggering compression
	text, meta := PackWithIntent(pieces, 50, PackOptions{
		Intent:     "test intent",
		Compressor: compressor,
	})

	require.Equal(t, "compressed output", text)
	require.True(t, meta.Compressed)
	require.Equal(t, "mock", meta.Provider)
	text, meta = PackWithIntent([]Piece{
		{Key: "header", Priority: 10, Text: strings.Repeat("x", 100)},
		{Key: "later", Priority: 1, Text: "more context"},
	}, 50, PackOptions{Compressor: &mockCompressor{returnText: "compressed"}})
	require.Equal(t, "compressed", text)
	require.True(t, meta.Compressed)
	require.Equal(t, []string{"later"}, meta.OmittedKeys)
}

func TestPackWithIntent_CompressionFails_FallsBack(t *testing.T) {
	// Use pieces where the first fits but second would be omitted
	pieces := []Piece{
		{Key: "a", Priority: 10, Text: strings.Repeat("x", 30)},
		{Key: "b", Priority: 9, Text: strings.Repeat("y", 30)},
	}

	compressor := &mockCompressor{returnError: context.DeadlineExceeded}

	// Compression fails - should fall back to Pack's truncation behavior
	text, meta := PackWithIntent(pieces, 50, PackOptions{
		Intent:     "test intent",
		Compressor: compressor,
	})

	// Should have Pack's output (first piece only), not compressed
	require.False(t, meta.Compressed)
	require.NotNil(t, meta.Error)
	require.Contains(t, text, strings.Repeat("x", 30))
	require.NotContains(t, text, "y") // second piece was omitted
	text, meta = PackWithIntent([]Piece{{Key: "body", Text: strings.Repeat("x", 100)}}, 50, PackOptions{
		Compressor: &mockCompressor{returnError: context.DeadlineExceeded},
	})
	require.Equal(t, strings.Repeat("x", 50), text)
	require.Equal(t, 50, meta.BudgetUsed)
	require.Equal(t, []string{"body"}, meta.IncludedKeys)
	require.Zero(t, meta.OmittedPieces)
	require.False(t, meta.Compressed)
	require.ErrorIs(t, meta.Error, context.DeadlineExceeded)
}

func TestPackWithIntent_CollectsMoreContent(t *testing.T) {
	// Create many pieces - more than would fit in hard budget
	pieces := make([]Piece, 10)
	for i := 0; i < 10; i++ {
		pieces[i] = Piece{
			Key:      string(rune('a' + i)),
			Priority: 100 - i,
			Text:     strings.Repeat(string(rune('a'+i)), 20),
		}
	}

	compressor := &mockCompressor{
		returnText:    "compressed",
		maxInputChars: 70,
	}

	// Hard budget 50 fits ~2 pieces, but collection should gather more
	text, meta := PackWithIntent(pieces, 50, PackOptions{
		Intent:     "test intent",
		Compressor: compressor,
	})

	require.True(t, meta.Compressed)
	require.Equal(t, "compressed", text)
	require.Len(t, compressor.requests, 1)
	require.Equal(t, []string{"a", "b", "c"}, []string{compressor.requests[0].Pieces[0].Key, compressor.requests[0].Pieces[1].Key, compressor.requests[0].Pieces[2].Key})
	require.LessOrEqual(t, 20*len(compressor.requests[0].Pieces)+2*(len(compressor.requests[0].Pieces)-1), 70)
}

func TestPackDetailed_ReportsOversizedFirstPiece(t *testing.T) {
	for _, tc := range []struct {
		budget int
		want   string
	}{{-1, ""}, {0, ""}, {10, "HEADER"}, {100, "HEADER\n…"}} {
		t.Run(fmt.Sprint(tc.budget), func(t *testing.T) {
			pieces := []Piece{
				{Key: "later", Priority: 1, Text: "small"},
				{Key: "empty", Priority: 2, Text: " \n "},
				{Key: "header", Priority: 10, Score: 3, Text: "HEADER\n" + strings.Repeat("x", 200)},
			}
			text, meta, included := PackDetailed(pieces, tc.budget)
			require.Equal(t, tc.want, text)
			require.True(t, meta.Trimmed)
			require.Equal(t, len(text), meta.BudgetUsed)
			require.Equal(t, 1, meta.IncludedPieces)
			require.Equal(t, []string{"header"}, meta.IncludedKeys)
			require.Equal(t, 1, meta.OmittedPieces)
			require.Equal(t, []string{"later"}, meta.OmittedKeys)
			require.Equal(t, []Piece{{Key: "header", Priority: 10, Score: 3, Text: text}}, included)
		})
	}
}

func TestPack_ReportsTrimmingOnlyPiece(t *testing.T) {
	text, meta := Pack([]Piece{{Key: "header", Text: "long header"}}, 4)
	require.Equal(t, "long", text)
	require.True(t, meta.Trimmed)
	require.Zero(t, meta.OmittedPieces)
	require.Empty(t, meta.OmittedKeys)
}

func TestPackWithIntent_OnlyPiece_CompressionPressure(t *testing.T) {
	for _, budget := range []int{-1, 0, 50, 75, 76, 100} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			piece := Piece{Key: "body", Text: strings.Repeat("x", 100)}
			pieces := []Piece{{Key: "blank-first", Priority: 10, Text: " \n "}, piece, {Key: "blank-last"}}
			compressor := &mockCompressor{returnText: "compressed"}
			text, meta := PackWithIntent(pieces, budget, PackOptions{Compressor: compressor})
			wantRaw := strings.Repeat("x", max(0, budget))
			require.Equal(t, max(0, budget), meta.BudgetRequested)
			require.Equal(t, min(100, max(0, budget)), meta.BudgetUsed)
			require.Equal(t, []string{"body"}, meta.IncludedKeys)
			require.Zero(t, meta.OmittedPieces)
			require.Empty(t, meta.OmittedKeys)
			if budget <= 75 {
				require.True(t, meta.Compressed)
				require.Equal(t, "compressed", text)
				require.Len(t, compressor.requests, 1)
				require.Equal(t, []Piece{piece}, compressor.requests[0].Pieces)
			} else {
				require.False(t, meta.Compressed)
				require.Equal(t, wantRaw, text)
				require.Empty(t, compressor.requests)
			}
		})
	}
}
