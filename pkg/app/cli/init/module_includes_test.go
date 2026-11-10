package init

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestModuleIncludesPreserveExistingRulesAndUnrelatedConfiguration(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "notes:\n  includes: [docs/**/*.md]\ncustom: keep-me\n")
	writeFixtureFile(t, root, ".kb/curation/_conflicts.md", "# Conflicts")
	require.NoError(t, ensureModuleIncludes(t, context.Background(), root, []string{".kb/curation/_conflicts.md"}))
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{"docs/**/*.md", ".kb/curation/_conflicts.md"}, cfg.Notes.Includes)
	data, err := os.ReadFile(filepath.Join(root, ".rhizome/config.yml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "custom: keep-me")
	require.NoError(t, ensureModuleIncludes(t, context.Background(), root, []string{".kb/curation/_conflicts.md"}))
	after, err := os.ReadFile(filepath.Join(root, ".rhizome/config.yml"))
	require.NoError(t, err)
	require.Equal(t, data, after)
	require.Error(t, ensureModuleIncludes(t, context.Background(), root, []string{"../outside"}))
}

func TestModuleIncludesRefreshUntouchedGuidanceAndPreserveUserProse(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "notes:\n  includes: [docs/**/*.md]\n")
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	section, err := renderRhizomeMd(*cfg)
	require.NoError(t, err)
	existing := "# Team preferences\nKeep my prose.\n\n" + buildManagedRhizomeBlock(section) + "\n\n" + buildManagedTemplateBlock("local", "Keep my starter.") + "\n"
	writeFixtureFile(t, root, "AGENTS.md", existing)
	record, _ := loadGeneratedFiles(root)
	record.recordWritten("AGENTS.md#rhizome", contentFingerprint([]byte(section)))
	require.NoError(t, record.save(root))
	writeFixtureFile(t, root, ".kb/curation/_conflicts.md", "# Conflicts")
	require.NoError(t, ensureModuleIncludes(t, context.Background(), root, []string{".kb/curation/_conflicts.md"}))
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "Keep my prose.")
	require.Contains(t, string(data), "Keep my starter.")
	require.Contains(t, string(data), ".kb/curation/_conflicts.md")
	body, _ := managedBlockBody(string(data), "rhizome")
	record, _ = loadGeneratedFiles(root)
	require.Equal(t, contentFingerprint([]byte(body)), record.Written["AGENTS.md#rhizome"])
}

func TestModuleIncludesAdmitDeclaredSourcesBeforeTheyExist(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "{}\n")
	const source = "artifacts/_assets/_model/api.yaml"
	require.NoError(t, ensureModuleIncludes(t, t.Context(), root, []string{source}))
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{"**/*.md", source}, cfg.Notes.Includes)
	require.NoFileExists(t, filepath.Join(root, source))
}

func TestMissingModuleSourcesCannotEscapeThroughExistingSymlink(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "{}\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	require.ErrorContains(t, ensureModuleIncludes(t, t.Context(), root, []string{"outside/missing.yaml"}), "escapes repository")
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Empty(t, cfg.Notes.Includes)
}

func ensureModuleIncludes(t *testing.T, ctx context.Context, root string, sources []string) error {
	t.Helper()
	formats, err := builtin.NewRegistry()
	require.NoError(t, err)
	return EnsureModuleSourceIncludes(ctx, root, sources, formats)
}

func TestModuleIncludesRespectProviderOwnership(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "notes:\n  includes: [\"**/*\"]\n")
	require.NoError(t, ensureModuleIncludes(t, t.Context(), root, []string{"notes/README.MD", "model/config.yaml"}))
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{"**/*", "model/config.yaml"}, cfg.Notes.Includes)
}

func TestModuleIncludesPreserveBraceAdmission(t *testing.T) {
	root := t.TempDir()
	const config = "notes:\n  includes:\n    - \"artifacts/_assets/_model/*.{yaml,yml}\"\n"
	writeFixtureFile(t, root, ".rhizome/config.yml", config)
	require.NoError(t, ensureModuleIncludes(t, t.Context(), root, []string{"artifacts/_assets/_model/items.yaml"}))
	data, err := os.ReadFile(filepath.Join(root, ".rhizome/config.yml"))
	require.NoError(t, err)
	require.Equal(t, config, string(data))
}
