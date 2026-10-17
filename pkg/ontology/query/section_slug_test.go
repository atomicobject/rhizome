package query

import (
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSectionNeighborKeysKeepEmptyFallbackAndAuthoredIDs(t *testing.T) {
	for _, tc := range []struct{ fragment, want string }{
		{"What's new?", "heading:what-s-new"},
		{"!!!", "heading:section"},
		{"^Authored_ID", "block:Authored_ID"},
		{"", ""},
	} {
		require.Equal(t, tc.want, resolvedTargetSectionKey(obsidian.ResolvedNoteTarget{Fragment: tc.fragment}))
	}
}
