package validationproduct

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProjectionContextComposesBundledSkillOverlayValidation(t *testing.T) {
	root := t.TempDir()
	runContext := projectionRunContext(ValidationProjectionRequest{
		VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testProjectionNoteMetadata(t),
	}, 20)
	result, _, err := validate.RunSuiteOncePrepared(context.Background(), validate.ScanOptions{
		Checks: []string{validate.CheckSkillOverlays}, RunContext: runContext,
	}, nil, nil)
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Checks)
	require.Len(t, result.Checks, 1)
	require.Equal(t, validate.CheckSkillOverlays, result.Checks[0].Name)
	require.Empty(t, result.Checks[0].Error)
	require.NotEmpty(t, result.Checks[0].Summary)
}
