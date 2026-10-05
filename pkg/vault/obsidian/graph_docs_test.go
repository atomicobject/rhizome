package obsidian

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeGraphAnalysis_IncludesContextDocsClassicVault(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "Note.md"), []byte("content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "CONTEXT.md"), []byte("# Module\n\nSee [[Note]]"), 0o644))

	vaultDef := VaultDefinition{Name: "vault", Path: vaultDir}
	note := &Note{}

	analysis, err := ComputeGraphAnalysis(vaultDef, note, GraphAnalysisOptions{
		IncludeDocsInGraph: true,
		DocPatterns:        []string{"CONTEXT.md"},
		DocMinBytes:        50,
	})
	require.NoError(t, err)

	assert.Contains(t, analysis.Nodes, "CONTEXT.md")
	assert.Contains(t, analysis.Nodes, "Note.md")
}

func TestComputeGraphAnalysis_IncludesContextDocsOutsideNoteIncludes(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vaultDir, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "notes", "Note.md"), []byte("content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "CONTEXT.md"), []byte("# Module\n\nSee [[notes/Note]]"), 0o644))

	vaultDef := VaultDefinition{
		Name:     "vault",
		Root:     vaultDir,
		Includes: []string{"notes/**/*.md"},
	}
	analysis, err := ComputeGraphAnalysis(vaultDef, &Note{}, GraphAnalysisOptions{
		IncludeDocsInGraph: true,
		DocPatterns:        []string{"CONTEXT.md"},
		DocMinBytes:        50,
	})
	require.NoError(t, err)

	assert.Contains(t, analysis.Nodes, "CONTEXT.md")
	assert.Contains(t, analysis.Nodes, "notes/Note.md")
	assert.Contains(t, analysis.Nodes["CONTEXT.md"].Neighbors, "notes/Note.md")
}

func TestComputeGraphAnalysis_ExcludesDocsWhenDisabled(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "Note.md"), []byte("content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "CONTEXT.md"), []byte("# Module\n\nSee [[Note]]"), 0o644))

	vaultDef := VaultDefinition{Name: "vault", Path: vaultDir}
	note := &Note{}

	analysis, err := ComputeGraphAnalysis(vaultDef, note, GraphAnalysisOptions{
		IncludeDocsInGraph: false,
		DocPatterns:        []string{"CONTEXT.md"},
		DocMinBytes:        50,
	})
	require.NoError(t, err)

	assert.NotContains(t, analysis.Nodes, "CONTEXT.md")
	assert.Contains(t, analysis.Nodes, "Note.md")
}

func TestComputeGraphAnalysis_SkipsStubDocs(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "Note.md"), []byte("content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "CONTEXT.md"), []byte("hi"), 0o644))

	vaultDef := VaultDefinition{Name: "vault", Path: vaultDir}
	note := &Note{}

	analysis, err := ComputeGraphAnalysis(vaultDef, note, GraphAnalysisOptions{
		IncludeDocsInGraph: true,
		DocPatterns:        []string{"CONTEXT.md"},
		DocMinBytes:        50,
	})
	require.NoError(t, err)

	assert.NotContains(t, analysis.Nodes, "CONTEXT.md")
	assert.Contains(t, analysis.Nodes, "Note.md")
}
