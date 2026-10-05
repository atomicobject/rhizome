package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectLayout_PythonFixture(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "testdata", "integration", "python-app")
	absRoot := paths.ResolveSymlinks(root).String()
	require.NotEmpty(t, absRoot)
	layout, err := DetectLayout(root)
	require.NoError(t, err)

	assert.True(t, layout.HasExistingConfig, "should detect existing .rhizome/config.yml")
	assert.Equal(t, filepath.Clean(absRoot), layout.ProjectRoot)
	assert.Contains(t, layout.Code.Languages, "python")
	assert.Contains(t, layout.SuggestedVault.Includes, "vault/**/*.md")
	assert.NotEmpty(t, layout.Code.PythonRoots)
}

func TestLoadExistingConfigDetectsV049SplitWorkflowManagement(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("rhizome:\n  version: 0.49.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "workflows.yml"), []byte("management:\n  ejected:\n    - spec-driven\n"), 0o644))

	layout := DetectedLayout{ProjectRoot: root}
	require.NoError(t, loadExistingConfig(&layout))

	require.True(t, layout.HasLegacyWorkflowConfig)
	require.Equal(t, []string{"spec-driven"}, layout.ExistingLocal.WorkflowTemplateManagement.Ejected)
}

func TestDetectLayout_RollsUpNestedGoRoots(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "x.go"), []byte("package sub\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.23\n"), 0o644))

	layout, err := DetectLayout(root)
	require.NoError(t, err)
	assert.Contains(t, layout.Code.Languages, "go")
	assert.Equal(t, []string{"."}, layout.Code.GoRoots)
}

func TestDetectLayout_RollsUpNestedTSRoots(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src", "app", "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "app", "sub", "x.ts"), []byte("export const x = 1;\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "package.json"), []byte("{\"name\":\"root\"}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "app", "package.json"), []byte("{\"name\":\"app\"}\n"), 0o644))

	layout, err := DetectLayout(root)
	require.NoError(t, err)
	assert.Contains(t, layout.Code.Languages, "typescript")
	assert.Equal(t, []string{"src"}, layout.Code.TSRoots)
}

func TestRecordTypeScriptJavaScriptFilePreservesTSXDetection(t *testing.T) {
	langs := map[string]struct{}{}
	roots := map[string]struct{}{}

	require.True(t, recordTypeScriptJavaScriptFile(".tsx", "packages/ui/view.tsx", langs, roots))
	assert.Contains(t, langs, "typescript")
	assert.Contains(t, langs, "javascript")
	assert.Contains(t, roots, "packages")
}

func TestDetectLayout_PreservesTSMonorepoPackageRoots(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "apps", "web", "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "apps", "admin", "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "packages", "ui", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "apps", "web", "package.json"), []byte("{\"name\":\"web\"}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "apps", "admin", "package.json"), []byte("{\"name\":\"admin\"}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "packages", "ui", "package.json"), []byte("{\"name\":\"ui\"}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "apps", "web", "src", "main.ts"), []byte("export const web = 1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "apps", "admin", "src", "main.tsx"), []byte("export const admin = 1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "packages", "ui", "src", "button.ts"), []byte("export const Button = 1\n"), 0o644))

	layout, err := DetectLayout(root)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"apps/admin", "apps/web", "packages/ui"}, layout.Code.TSRoots)
}

func TestDetectLayout_SuggestedVaultCombinesMarkdownDirs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# Guide\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "plan.md"), []byte("# Plan\n"), 0o644))

	layout, err := DetectLayout(root)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"docs/**/*.md", "notes/**/*.md"}, layout.SuggestedVault.Includes)
	assert.Contains(t, layout.SuggestedVault.Reason, "Detected repo knowledge in")
	assert.Contains(t, layout.SuggestedVault.Reason, "docs")
	assert.Contains(t, layout.SuggestedVault.Reason, "notes")
}

func TestDetectLayout_SuggestsUnconventionalKnowledgeDir(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "handbookish"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "handbookish", "guide.md"), []byte("---\nsummary: test\n---\nSee [[Architecture]].\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "handbookish", "runbook.md"), []byte("Use [[Runbook]].\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "handbookish", "faq.md"), []byte("# FAQ\n"), 0o644))

	layout, err := DetectLayout(root)
	require.NoError(t, err)
	assert.Equal(t, []string{"handbookish/**/*.md"}, layout.SuggestedVault.Includes)
}
