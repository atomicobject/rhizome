//go:build integration
// +build integration

package integration

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestTS_FileContext(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	compPath := ws.CodePath("tsapp/src/components/SomeComponent.tsx")
	appPath := ws.CodePath("tsapp/src/app/App.tsx")
	codeRefsByFile := ws.BuildCodeRefs(t, nil)

	t.Run("symbol_anchor_matches_component_definition", func(t *testing.T) {
		fc, err := actions.BuildFileContext(compPath, actions.FileContextParams{
			VaultDef:       ws.VaultDef,
			ProjectRoot:    ws.ProjectRoot,
			CodeRefRoot:    ws.CodeRoot,
			DocPatterns:    []string{"CONTEXT.md"},
			MaxEmptyLevels: 4,
			ContextBudget:  4000,
			CodeRefsByFile: codeRefsByFile,
			NoteReader:     ws.NoteMgr,
			CodeAnchor:     ws.CodeAnchor,
		})
		require.NoError(t, err)
		require.NotEmpty(t, fc.AnchorMatches, "expected ts symbol anchor on definition file")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "ts-some-component"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "ts-some-component", "symbol matched scope"))
	})

	t.Run("function_anchor_matches_jsx_usage", func(t *testing.T) {
		fc, err := actions.BuildFileContext(appPath, actions.FileContextParams{
			VaultDef:       ws.VaultDef,
			ProjectRoot:    ws.ProjectRoot,
			CodeRefRoot:    ws.CodeRoot,
			DocPatterns:    []string{"CONTEXT.md"},
			MaxEmptyLevels: 4,
			ContextBudget:  4000,
			CodeRefsByFile: codeRefsByFile,
			NoteReader:     ws.NoteMgr,
			CodeAnchor:     ws.CodeAnchor,
		})
		require.NoError(t, err)
		require.NotEmpty(t, fc.AnchorMatches, "expected ts function anchor on JSX usage")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "ts-some-component-callers"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "ts-some-component-callers", "call site matched scope"))
	})
}
