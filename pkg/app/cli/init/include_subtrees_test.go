package init

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/stretchr/testify/require"
)

func readIgnoreFile(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".rhizome", "ignore"))
	require.NoError(t, err)
	return string(data)
}

func TestWithIncludedSubtrees(t *testing.T) {
	t.Run("content without rules gets defaults plus block", func(t *testing.T) {
		for _, content := range []string{"", "  \n"} {
			body := withIncludedSubtrees(content, []string{"app"})
			require.True(t, strings.HasPrefix(body, ignore.DefaultIgnoreFile()), "defaults must be materialized before negations")
			require.Contains(t, body, includedSubtreesHeader)
			require.Contains(t, body, "!/app/\n")
		}
	})

	t.Run("existing rules get block appended", func(t *testing.T) {
		body := withIncludedSubtrees("node_modules/\n", []string{"app"})
		require.True(t, strings.HasPrefix(body, "node_modules/\n"))
		require.NotContains(t, body, ignore.DefaultIgnoreFile())
		require.Contains(t, body, includedSubtreesHeader+"\n!/app/\n")
	})

	t.Run("idempotent", func(t *testing.T) {
		first := withIncludedSubtrees("", []string{"app"})
		require.Equal(t, first, withIncludedSubtrees(first, []string{"app"}))
		require.Equal(t, 1, strings.Count(first, includedSubtreesHeader))
	})

	t.Run("nested rel paths and second append reuse block", func(t *testing.T) {
		body := withIncludedSubtrees(withIncludedSubtrees("", []string{"modules/app"}), []string{"modules/web", "modules/app"})
		require.Equal(t, 1, strings.Count(body, includedSubtreesHeader))
		require.Equal(t, 1, strings.Count(body, "!/modules/app/\n"))
		require.Contains(t, body, "!/modules/web/\n")
	})
}

func TestValidateProjectSubpath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "modules", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644))

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "relative dir", input: "modules/app", want: "modules/app"},
		{name: "trailing slash", input: "modules/app/", want: "modules/app"},
		{name: "absolute under root", input: filepath.Join(root, "modules"), want: "modules"},
		{name: "missing", input: "nope", wantErr: true},
		{name: "file not dir", input: "file.txt", wantErr: true},
		{name: "escape", input: "../elsewhere", wantErr: true},
		{name: "root itself", input: ".", wantErr: true},
		{name: "empty", input: "  ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateProjectSubpath(root, tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
