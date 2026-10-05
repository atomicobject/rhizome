package coderefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRewriteBatchOldPathAlternativesKeepCountsAndMappingOrder(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "refs.go")
	content := "package fixture\n// [[docs/Source]] [[alias/Source]] [canonical](docs/Source.md) [authored](alias/Source.md) @docs/Source, @alias/Source, @Source, [[docs/SourceExtra]]\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	config := NewConfig(true, []string{"*.go"}, nil)
	mapping := RefMapping{OldPath: "docs/Source.md", NewPath: "alias/Source.md", OldPathAliases: []string{"alias/Source.md", "docs/Source.md", "alias/Source.md"}}
	result, err := RewriteBatch(root, config, []RefMapping{mapping})
	require.NoError(t, err)
	require.Equal(t, 7, result.RefsUpdated)
	updated, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "package fixture\n// [[alias/Source]] [[alias/Source]] [canonical](alias/Source.md) [authored](alias/Source.md) @alias/Source, @alias/Source, @Source, [[docs/SourceExtra]]\n", string(updated))

	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	result, err = RewriteBatch(root, config, []RefMapping{mapping, {OldPath: "alias/Source.md", NewPath: "Reviewed/Final.md"}})
	require.NoError(t, err)
	require.Equal(t, 14, result.RefsUpdated)
	updated, err = os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "package fixture\n// [[Reviewed/Final]] [[Reviewed/Final]] [canonical](Reviewed/Final.md) [authored](Reviewed/Final.md) @Reviewed/Final, @Reviewed/Final, @Final, [[docs/SourceExtra]]\n", string(updated))
	refs, err := ScanFile("refs.go", updated, obsidian.BuildNotePathCache([]string{"Reviewed/Final.md", "docs/SourceExtra.md"}))
	require.NoError(t, err)
	require.Len(t, refs, 8)
	finalRefs, extraRefs := 0, 0
	for _, ref := range refs {
		if ref.Target == "Reviewed/Final.md" {
			finalRefs++
		} else {
			require.Equal(t, "docs/SourceExtra.md", ref.Target)
			extraRefs++
		}
	}
	require.Equal(t, 7, finalRefs)
	require.Equal(t, 1, extraRefs)
}
