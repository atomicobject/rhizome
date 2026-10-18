package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestWriteConfigNewEmitsStrictValidationDefaults(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := writeConfigNew(root, repoconfig.LocalConfig{
		Validation: repoconfig.LocalValidationConfig{
			Default: repoconfig.LocalValidationSuiteConfig{
				Add:  []string{"link-hygiene"},
				Skip: []string{"broken-links"},
			},
			All: repoconfig.LocalValidationSuiteConfig{
				Skip: []string{"code-anchors"},
			},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(root, ".rhizome", "config.yml"))
	require.NoError(t, err)
	require.Equal(t, `validation:
  default:
    add:
      - link-hygiene
    skip:
      - broken-links
  all:
    skip:
      - code-anchors
`, string(data))
}

func TestWriteConfigPatchedPreservesValidationMappingAndUnknownKeys(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configPath := filepath.Join(configDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`validation:
  default:
    add: [link-hygiene]
    futurePolicy: required
  all:
    skip: [code-anchors]
unknownTopLevel:
  enabled: true
rhizome:
  version: v0.9.0
`), 0o644))

	err := writeConfigPatched(root, repoconfig.LocalConfig{
		Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3"},
	}, changeSet{sectionRhizome: true})
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, `validation:
  default:
    add: [link-hygiene]
    futurePolicy: required
  all:
    skip: [code-anchors]
unknownTopLevel:
  enabled: true
rhizome:
  version: v1.2.3
`, string(data))
}

func TestWriteConfigPatchedLeavesWorkflowStateUntouchedForUnrelatedChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yml"), []byte("rhizome:\n  version: v0.9.0\n"), 0o644))
	require.NoError(t, repoconfig.SaveLocalWorkflowConfig(root, repoconfig.LocalWorkflowConfig{
		Templates: []string{templateProjectKB},
	}))

	err := writeConfigPatched(root, repoconfig.LocalConfig{
		Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3"},
	}, changeSet{sectionRhizome: true})
	require.NoError(t, err)

	workflow, exists, err := repoconfig.LoadLocalWorkflowConfig(root)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []string{templateProjectKB}, workflow.Templates)
}
