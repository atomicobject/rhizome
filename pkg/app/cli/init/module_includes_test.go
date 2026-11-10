package init

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestModuleIncludesPreserveExistingRulesAndUnrelatedConfiguration(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "notes:\n  includes: [docs/**/*.md]\ncustom: keep-me\n")
	writeFixtureFile(t, root, ".kb/curation/_conflicts.md", "# Conflicts")
	require.NoError(t, EnsureModuleSourceIncludes(context.Background(), root, []string{".kb/curation/_conflicts.md"}))
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{"docs/**/*.md", ".kb/curation/_conflicts.md"}, cfg.Notes.Includes)
	data, err := os.ReadFile(filepath.Join(root, ".rhizome/config.yml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "custom: keep-me")
	require.NoError(t, EnsureModuleSourceIncludes(context.Background(), root, []string{".kb/curation/_conflicts.md"}))
	after, err := os.ReadFile(filepath.Join(root, ".rhizome/config.yml"))
	require.NoError(t, err)
	require.Equal(t, data, after)
	require.Error(t, EnsureModuleSourceIncludes(context.Background(), root, []string{"../outside"}))
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
	require.NoError(t, EnsureModuleSourceIncludes(context.Background(), root, []string{".kb/curation/_conflicts.md"}))
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "Keep my prose.")
	require.Contains(t, string(data), "Keep my starter.")
	require.Contains(t, string(data), ".kb/curation/_conflicts.md")
	body, _ := managedBlockBody(string(data), "rhizome")
	record, _ = loadGeneratedFiles(root)
	require.Equal(t, contentFingerprint([]byte(body)), record.Written["AGENTS.md#rhizome"])
}
