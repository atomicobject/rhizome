package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseValidationApplySelection(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr string
	}{
		{"json array", ` ["repair:a", " issue:v1:b ", ""] `, []string{"repair:a", "issue:v1:b"}, ""},
		{"plain lines with comments", "# approved\n\nrepair:a\r\n  issue:v1:b\n#repair:c\n", []string{"repair:a", "issue:v1:b"}, ""},
		{"empty", "\n# nothing\n", nil, "lists no action IDs"},
		{"non-string json", `[1]`, nil, "parse selection JSON array"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseValidationApplySelection([]byte(tt.input))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveValidationApplySelection(t *testing.T) {
	plan := filepath.Join(t.TempDir(), "plan.txt")
	require.NoError(t, os.WriteFile(plan, []byte("# reviewed\nissue:v1:abc\n"), 0o600))

	none, err := ResolveValidationApplySelection(false, nil, "")
	require.NoError(t, err)
	require.Nil(t, none)

	_, err = ResolveValidationApplySelection(false, []string{"a"}, "")
	require.ErrorContains(t, err, "require --apply")

	merged, err := ResolveValidationApplySelection(true, []string{"action-1"}, plan)
	require.NoError(t, err)
	require.Equal(t, []string{"action-1", "issue:v1:abc"}, merged)

	t.Chdir(filepath.Dir(plan))
	relative, err := ResolveValidationApplySelection(true, nil, "plan.txt")
	require.NoError(t, err, "a relative plan path is relative to the working directory")
	require.Equal(t, []string{"issue:v1:abc"}, relative)

	_, err = ResolveValidationApplySelection(true, nil, filepath.Join(t.TempDir(), "missing"))
	require.ErrorContains(t, err, "read --from-plan")
}
