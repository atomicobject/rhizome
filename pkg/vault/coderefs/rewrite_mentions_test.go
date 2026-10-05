package coderefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRewriteBatchMentionEncodingAndMappingOrder(t *testing.T) {
	for _, tc := range []struct {
		name, authored, old string
	}{
		{name: "invalid source bytes", authored: "Old\xff", old: "Old\uFFFD"},
		{name: "replacement rune", authored: "Old\uFFFD", old: "Old\uFFFD"},
		{name: "unicode", authored: "预算", old: "预算"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "refs.go")
			// Exercise the full name, basename, and alias. Link labels remain
			// protected even when the regex interprets invalid bytes as RuneError.
			protected := "[label @docs/" + tc.authored + "](Other.md)"
			content := "package fixture\n// @docs/" + tc.authored + ", @" + tc.authored + "! @alias/" + tc.authored + "; " + protected + "\n"
			require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
			result, err := RewriteBatch(root, NewConfig(true, []string{"*.go"}, nil), []RefMapping{
				{OldPath: "docs/" + tc.old + ".md", NewPath: "Archive/New.md", OldPathAliases: []string{"alias/" + tc.old + ".md"}},
				{OldPath: "Archive/New.md", NewPath: "Reviewed/Final.md"},
			})
			require.NoError(t, err)
			require.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 6}, result)
			updated, err := os.ReadFile(file)
			require.NoError(t, err)
			want := "package fixture\n// @Reviewed/Final, @Final! @Reviewed/Final; " + protected + "\n"
			require.Equal(t, want, string(updated))
		})
	}
}
