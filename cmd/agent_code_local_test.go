package cmd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeModeLocalDispatcherRejectsMutationOptionsInReadOnlyMode(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
	}{
		{name: "current user set", input: map[string]any{"action": "set", "personTitleOrRef": "People/Drew.md"}},
		{name: "note move", input: map[string]any{"source": "a.md", "target": "b.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcome, handled := callAgentCodeLocal(context.Background(), map[string]string{
				"current user set": "current_user", "note move": "note_move",
			}[tt.name], tt.input, false)
			require.True(t, handled)
			require.False(t, outcome.OK)
			require.Contains(t, outcome.Stderr, "read-write")
		})
	}
}
