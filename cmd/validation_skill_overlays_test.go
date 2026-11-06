package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/validate"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestProductionValidationRunnerComposesBundledSkillOverlays(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		name := "prepared context"
		if fallback {
			name = "fallback context"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644))
			runner := newProductionValidationRunner()
			runner.resolveVault = func(context.Context, string) (obsidian.VaultDefinition, error) {
				return obsidian.VaultDefinition{Path: root}, nil
			}
			if fallback {
				runner.runProjection = func(ctx context.Context, request validationrun.ValidationRunRequest, projection validationproduct.ValidationProjectionRequest, options validate.ScanOptions) (validationrun.ValidationResult, error) {
					options.RunContext = nil
					return validationproduct.RunWithProjection(ctx, request, projection, options)
				}
			}
			result, err := runner.Run(t.Context(), validationProductRequest{Selectors: []string{"skill-overlays"}, Surface: validate.SurfaceLocal})
			require.NoError(t, err)
			require.True(t, result.OK, "%+v", result.Checks)
			require.Len(t, result.Checks, 1)
			require.Equal(t, "skill-overlays", result.Checks[0].Name)
			require.Empty(t, result.Checks[0].Error)
			require.Regexp(t, `^[1-9][0-9]* overlay fragment\(s\) checked$`, result.Checks[0].Summary)
		})
	}
}

func TestIndexCommandComposesBundledSkillOverlays(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"note.md":             "# Note\n",
		".rhizome/config.yml": "notes:\n  includes: ['**/*.md']\nvalidation:\n  default:\n    add: [skill-overlays]\n    skip: [ontology, identifiers, broken-links]\n",
	})
	_, stderr, err := runRootCLI(t, vaultconfig.WithEnv(t.Context(), vault.env), []string{"index", "--vault", vault.name})
	require.NoError(t, err, stderr)
	store, err := semdb.Open(repoconfig.UnifiedIndexPath(vault.path, ""))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	state, err := store.GetValidationStateSnapshot(t.Context())
	require.NoError(t, err)
	require.True(t, state.HasSnapshot)
	require.Equal(t, 0, state.Snapshot.ErrorCount)
	require.Equal(t, 0, state.Snapshot.IssueCount)
	require.Len(t, state.Snapshot.Checks, 1)
	require.Equal(t, validate.CheckSkillOverlays, state.Snapshot.Checks[0].Check)
	require.Empty(t, state.Snapshot.Checks[0].Error)
	require.Regexp(t, `^[1-9][0-9]* overlay fragment\(s\) checked$`, state.Snapshot.Checks[0].Summary)
}
