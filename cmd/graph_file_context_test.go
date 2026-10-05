package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGraphFileContextApplyRepairsEmbeddedNodeTargetFromConfiguredPythonCodeRef(t *testing.T) {
	vault := setupGraphFileContextLinkTargetVault(t)

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"graph", "file-context",
		"--vault", vault.name,
		"--profile", "code",
		"--ensure-link-targets", "apply",
		"src/phase5.py",
	})
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	require.Empty(t, stderr)

	updated, err := os.ReadFile(filepath.Join(vault.path, "docs", "phase5-spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(updated), "^userstory-story-a-")
	require.Contains(t, stdout, `wikilink="[[phase5-spec#^`)
}

func TestGraphFileContextDefaultAndNeverDoNotRepairEmbeddedNodeTargets(t *testing.T) {
	vault := setupGraphFileContextLinkTargetVault(t)

	defaultOutput, defaultErr, err := runRootCLI(t, nil, []string{
		"graph", "file-context",
		"--vault", vault.name,
		"--profile", "code",
		"src/phase5.py",
	})
	require.NoError(t, err, "stdout=%s stderr=%s", defaultOutput, defaultErr)
	require.Empty(t, defaultErr)

	planOutput, planErr, err := runRootCLI(t, nil, []string{
		"graph", "file-context",
		"--vault", vault.name,
		"--profile", "code",
		"--ensure-link-targets", "plan",
		"src/phase5.py",
	})
	require.NoError(t, err, "stdout=%s stderr=%s", planOutput, planErr)
	require.Empty(t, planErr)
	require.Contains(t, planOutput, `kind="ontology-node"`)

	neverOutput, neverErr, err := runRootCLI(t, nil, []string{
		"graph", "file-context",
		"--vault", vault.name,
		"--profile", "code",
		"--ensure-link-targets", "never",
		"src/phase5.py",
	})
	require.NoError(t, err, "stdout=%s stderr=%s", neverOutput, neverErr)
	require.Empty(t, neverErr)
	require.Equal(t, defaultOutput, neverOutput)

	updated, err := os.ReadFile(filepath.Join(vault.path, "docs", "phase5-spec.md"))
	require.NoError(t, err)
	require.NotContains(t, string(updated), "^userstory-story-a-")
	require.NotContains(t, defaultOutput, `kind="ontology-node"`)
}

func setupGraphFileContextLinkTargetVault(t *testing.T) agentTestVault {
	t.Helper()
	return setupAgentTestVault(t, map[string]string{
		".rhizome/config.yml": `notes:
  includes: ["**/*.md"]
code:
  python: {}
compression:
  enabled: false
`,
		".rhizome/ontology/schema.graphql": `
type ProductSpec @node(paths: ["docs/phase5-spec.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`,
		"docs/phase5-spec.md": `# Phase 5 Spec

## Stories

### Story A

summary:: Repair the embedded node locator.
`,
		"src/phase5.py": "# WHY: implements [[phase5-spec#Story A]]\nprint('phase 5')\n",
	})
}
