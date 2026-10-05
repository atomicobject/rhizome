package actions

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/stretchr/testify/require"
)

func TestPackContextPartialFirstPieceRetainsRawRetry(t *testing.T) {
	text := "café\n" + strings.Repeat("界", 30) + "\nUNIQUE_END"
	for _, budget := range []int{0, 1, 4, 9, 40} {
		for _, batch := range []bool{false, true} {
			t.Run(fmt.Sprintf("budget=%d/batch=%t", budget, batch), func(t *testing.T) {
				state := &fakeBatchDedupeTracker{}
				var tracker DedupeTracker = state
				if !batch {
					tracker = struct{ DedupeTracker }{state}
				}
				pieces := []contextpack.Piece{{Key: "container", Text: text}}
				nested := map[string][]DedupeItem{"container": {{Key: "note:source", Fingerprint: "full-source-fingerprint"}}}
				first := packContext(packContextOptions{Pieces: pieces, Budget: budget, Tracker: tracker, NestedDedupe: nested})
				require.NoError(t, first.Delivery.finish(context.Background()))
				require.LessOrEqual(t, len(first.Text), budget)
				require.True(t, utf8.ValidString(first.Text))
				require.Equal(t, []string{"container"}, first.Meta.IncludedKeys)
				require.Empty(t, state.committed)
				second := packContext(packContextOptions{Pieces: pieces, Budget: len(text), Tracker: tracker, NestedDedupe: nested})
				require.Equal(t, text, second.Text)
				require.NoError(t, second.Delivery.finish(context.Background()))
				require.Equal(t, "full-source-fingerprint", state.committed["note:source"])
				third := packContext(packContextOptions{Pieces: pieces, Budget: len(text), Tracker: tracker, NestedDedupe: nested})
				require.Empty(t, third.Text)
				require.NoError(t, third.Delivery.finish(context.Background()))
			})
		}
	}
}
