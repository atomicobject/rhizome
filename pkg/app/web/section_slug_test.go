package web

import (
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSectionAnchorLookupKeepsEmptySlugAndAuthoredIDs(t *testing.T) {
	for _, tc := range []struct{ fragment, want string }{
		{"What's new?", "heading:what-s-new"},
		{"!!!", "heading:"},
		{"^Authored_ID", "block:Authored_ID"},
		{"", ""},
	} {
		require.Equal(t, tc.want, sectionAnchorLookupKey(tc.fragment, markdownsyntax.BacklinkTypeHeading))
	}
}
