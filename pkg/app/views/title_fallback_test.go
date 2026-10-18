package views

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLinkComparableValuesUsesPortableNoteTitleFallback(t *testing.T) {
	for _, path := range []string{"notes/release.v2.html", `notes\release.v2.html`, "notes/release.v2.md"} {
		require.Equal(t, []string{path, "release.v2"}, linkComparableValues(path))
	}
	require.Equal(t, []string{"Authored title"}, linkComparableValues("Authored title"))
}
