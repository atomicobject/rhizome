package markdownsyntax

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestHeadingSlug(t *testing.T) {
	for _, tc := range []struct{ heading, want string }{
		{"What's new?", "what-s-new"},
		{`Path / to\new_name`, "path-to-new-name"},
		{"  ÄRGER: 東京 2  ", "ärger-東京-2"},
		{"one---two", "one-two"},
		{"", ""},
		{" !? ", ""},
	} {
		t.Run(tc.heading, func(t *testing.T) { require.Equal(t, tc.want, HeadingSlug(tc.heading)) })
	}
}
