package serve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestReadinessRefreshComposesBundledSkillOverlays(t *testing.T) {
	live := validationInvalidationRuntime(t)
	require.NoError(t, os.WriteFile(filepath.Join(live.VaultPath, "note.md"), []byte("# Note\n"), 0o644))
	require.NoError(t, repoconfig.SaveLocalConfig(live.VaultPath, repoconfig.LocalConfig{Validation: repoconfig.LocalValidationConfig{
		Default: repoconfig.LocalValidationSuiteConfig{Add: []string{"skill-overlays"}, Skip: []string{"ontology", "identifiers", "broken-links"}},
	}}))
	coordinator := NewReadinessCoordinator(nil)
	coordinator.indexAttemptDoneOnce.Do(func() { close(coordinator.indexAttemptDone) })
	coordinator.refreshValidation(t.Context(), live)
	state, err := live.IntelStore().GetValidationStateSnapshot(t.Context())
	require.NoError(t, err)
	require.True(t, state.HasSnapshot)
	require.Equal(t, 0, state.Snapshot.ErrorCount)
	require.Equal(t, 0, state.Snapshot.IssueCount)
	require.Len(t, state.Snapshot.Checks, 1)
	require.Equal(t, validate.CheckSkillOverlays, state.Snapshot.Checks[0].Check)
	require.Empty(t, state.Snapshot.Checks[0].Error)
	require.Regexp(t, `^[1-9][0-9]* overlay fragment\(s\) checked$`, state.Snapshot.Checks[0].Summary)
}
