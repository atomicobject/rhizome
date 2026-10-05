//go:build integration
// +build integration

package integration

import (
	"context"
	"testing"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGo_Rationale(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	store, err := codeanchorsqlite.Open(ws.DBPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// rationale_example.go has NOTE, HACK, TODO, IMPORTANT, and FIXME comments.
	rationale, err := store.RationaleForPathPrefix(ctx, "go/todo/", nil)
	require.NoError(t, err)
	require.NotEmpty(t, rationale, "expected rationale records for go/todo/ after indexing")

	// Collect kinds found.
	kinds := make(map[string]bool)
	for _, r := range rationale {
		kinds[string(r.Kind)] = true
		assert.NotEmpty(t, r.ID, "rationale should have an ID")
		assert.NotEmpty(t, r.Content, "rationale should have content")
		assert.Greater(t, r.StartLine, int64(0), "start line should be positive")
		assert.GreaterOrEqual(t, r.EndLine, r.StartLine, "end line >= start line")
		assert.NotEmpty(t, r.Fingerprint, "fingerprint should be set")
	}

	assert.True(t, kinds["note"], "expected at least one NOTE rationale")
	assert.True(t, kinds["hack"], "expected at least one HACK rationale")
	assert.True(t, kinds["todo"], "expected at least one TODO rationale")
	assert.True(t, kinds["important"], "expected at least one IMPORTANT rationale")
	assert.True(t, kinds["fixme"], "expected at least one FIXME rationale")

	// Verify symbol association: comments inside functions should have FQNs.
	var hasSymbolFQN bool
	for _, r := range rationale {
		if r.SymbolFQN != "" {
			hasSymbolFQN = true
			break
		}
	}
	assert.True(t, hasSymbolFQN, "expected at least one rationale record with a symbol FQN")

	// RationaleByKind: filter to only hack.
	hacks, err := store.RationaleByKind(ctx, []string{"hack"})
	require.NoError(t, err)
	require.NotEmpty(t, hacks, "fixture HACK rationale must survive kind filtering")
	for _, r := range hacks {
		assert.Equal(t, "hack", string(r.Kind), "RationaleByKind should only return requested kind")
	}
}
