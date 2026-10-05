package coderefs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteBatch_LiteralDestinationPaths(t *testing.T) {
	for _, literal := range []string{"$USD", "$1", "${1}", "$$"} {
		t.Run(literal, func(t *testing.T) {
			newBase := "Budget" + literal
			newPathNoExt := "Archive" + literal + "/" + newBase
			newPath := newPathNoExt + ".md"
			content := `package fixture
// [[docs/Old.md#heading$1|alias${1}]] [[docs/Old#heading$1|alias${1}]] [[oLd#heading$1|alias${1}]]
// (@docs/Old), @Old,
// [cost $1 ${1} $$](docs/Old.md#heading$1) [cost $1 ${1} $$](docs/Old#heading$1)
// [[docs/Other.md]] [other](docs/Other.md) @Other ![image](docs/Old.md)
const raw = "[[docs/Old.md]] @docs/Old [doc](docs/Old.md)"
`
			want := fmt.Sprintf(`package fixture
// [[%s#heading$1|alias${1}]] [[%s#heading$1|alias${1}]] [[%s#heading$1|alias${1}]]
// ([[%s]]), [[%s]],
// [cost $1 ${1} $$](%s#heading$1) [cost $1 ${1} $$](%s#heading$1)
// [[docs/Other.md]] [other](docs/Other.md) @Other ![image](docs/Old.md)
const raw = "[[docs/Old.md]] @docs/Old [doc](docs/Old.md)"
`, newPath, newPathNoExt, newBase, newPath, newPath, obsidian.EncodeMarkdownPath(newPath), obsidian.EncodeMarkdownPath(newPathNoExt))

			vaultDir := t.TempDir()
			file := filepath.Join(vaultDir, "refs.go")
			require.NoError(t, os.WriteFile(file, []byte(content), 0o644))

			result, err := RewriteBatch(vaultDir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{
				{OldPath: "docs/Old.md", NewPath: newPath},
			})
			require.NoError(t, err)
			assert.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 7}, result)
			updated, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.Equal(t, want, string(updated))
			assertLiteralDestinationRefs(t, updated, newPath, "Archive.md", "Budget.md", newPathNoExt+"Suffix.md")

			result, err = RewriteBatch(vaultDir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{
				{OldPath: newPath, NewPath: "Reviewed/Final.md"},
			})
			require.NoError(t, err)
			assert.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 7}, result)
			updated, err = os.ReadFile(file)
			require.NoError(t, err)
			assertLiteralDestinationRefs(t, updated, "Reviewed/Final.md", newPath, "Budget.md")
		})
	}
}

func assertLiteralDestinationRefs(t *testing.T, content []byte, destination string, distractors ...string) {
	t.Helper()
	notes := append([]string{destination, "docs/Other.md"}, distractors...)
	refs, err := ScanFile("refs.go", content, buildTestCache(notes))
	require.NoError(t, err)
	require.Len(t, refs, 10)
	mentionCount := 0
	for _, ref := range refs {
		if ref.Line == 3 {
			mentionCount++
			assert.Equal(t, destination, ref.Target)
			assert.Equal(t, RefKindWikilink, ref.Kind)
		}
	}
	assert.Equal(t, 2, mentionCount)
}

func TestRewriteBatch_MentionDestinationSyntax(t *testing.T) {
	for _, tc := range []struct {
		name       string
		newPath    string
		want       string
		distractor string
	}{
		{name: "permitted", newPath: "Archive/New_Note.v2-1.md", want: "(@Archive/New_Note.v2-1), @New_Note.v2-1,"},
		{name: "space", newPath: "Archive/Budget USD.md", want: "([[Archive/Budget USD.md]]), [[Archive/Budget USD.md]],", distractor: "Other/Budget USD.md"},
		{name: "unicode", newPath: "Archive/BudgetÉ.md", want: "([[Archive/BudgetÉ.md]]), [[Archive/BudgetÉ.md]],", distractor: "Other/BudgetÉ.md"},
		{name: "directory only", newPath: "Archive$USD/New_Note.md", want: "([[Archive$USD/New_Note.md]]), @New_Note,"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "package fixture\n// (@docs/Old), @Old,\n// @docs/OldSuffix @OldSuffix user@Old.com\nconst raw = \"@docs/Old @Old\"\n"
			vaultDir := t.TempDir()
			file := filepath.Join(vaultDir, "refs.go")
			require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
			result, err := RewriteBatch(vaultDir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{
				{OldPath: "docs/Old.md", NewPath: tc.newPath},
			})
			require.NoError(t, err)
			assert.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 2}, result)
			updated, err := os.ReadFile(file)
			require.NoError(t, err)
			want := "package fixture\n// " + tc.want + "\n// @docs/OldSuffix @OldSuffix user@Old.com\nconst raw = \"@docs/Old @Old\"\n"
			assert.Equal(t, want, string(updated))
			notes := []string{tc.newPath, "Archive.md", "Budget.md"}
			if tc.distractor != "" {
				notes = append(notes, tc.distractor)
			}
			refs, err := ScanFile("refs.go", updated, buildTestCache(notes))
			require.NoError(t, err)
			require.Len(t, refs, 2)
			for _, ref := range refs {
				assert.Equal(t, tc.newPath, ref.Target)
			}
		})
	}
}

func TestRewriteBatch_LiteralDestinationNoMatches(t *testing.T) {
	content := `package fixture
// [[docs/Other.md]] [[docs/OldSuffix]] [other](docs/Other.md) @docs/OldSuffix ![image](docs/Old.md)
const raw = "[[docs/Old.md]] @docs/Old [doc](docs/Old.md)"
`
	vaultDir := t.TempDir()
	file := filepath.Join(vaultDir, "refs.go")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	result, err := RewriteBatch(vaultDir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{
		{OldPath: "docs/Old.md", NewPath: "Archive$USD/Budget$USD.md"},
	})
	require.NoError(t, err)
	assert.Equal(t, RewriteResult{}, result)
	updated, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, content, string(updated))
}
