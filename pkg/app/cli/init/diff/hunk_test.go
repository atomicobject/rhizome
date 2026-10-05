package diff

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHunk_String(t *testing.T) {
	h := Hunk{OldStart: 10, NewStart: 10, ContextBefore: []string{"context before"}, Removed: []string{"old line"}, Added: []string{"new line"}, ContextAfter: []string{"context after"}}
	require.Equal(t, "@@ -9,3 +9,3 @@\n context before\n-old line\n+new line\n context after\n", h.String())
	h.OldStart, h.NewStart = 1, 1
	require.Equal(t, "@@ -1,3 +1,3 @@\n context before\n-old line\n+new line\n context after\n", h.String())
}
