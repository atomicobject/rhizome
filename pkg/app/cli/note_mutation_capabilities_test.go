package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameHeadingRejectsHTMLBeforeParsingOrWriting(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview", true: "apply"}[apply], func(t *testing.T) {
			root := t.TempDir()
			content := []byte("<html><body><pre>\n# Original\n</pre></body></html>\n")
			require.NoError(t, os.WriteFile(filepath.Join(root, "report.HTML"), content, 0o644))
			_, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{
				NoteMetadata: testNoteMetadataIndexer(t), Path: "report.HTML", OldHeading: "Original", NewHeading: "Changed", Apply: apply,
			})
			require.ErrorContains(t, err, "unsupported")
			got, err := os.ReadFile(filepath.Join(root, "report.HTML"))
			require.NoError(t, err)
			require.Equal(t, content, got)
		})
	}
}
