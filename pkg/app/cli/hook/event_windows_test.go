//go:build windows

package hook

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsDrivePathsResolveInsideTheWorktree(t *testing.T) {
	root := repo(t)
	file := filepath.Join(root, "pkg", "a.go") // C:\...\pkg\a.go
	require.Equal(t, []string{"pkg/a.go"}, containedPaths(root, root, []string{file, strings.ReplaceAll(file, `\`, "/")}))
	require.Equal(t, []string{"pkg/a.go"}, containedPaths(root, root, shellReads(`cat '`+file+`'`, root)))
	require.Empty(t, containedPaths(root, root, []string{`D:\elsewhere\a.go`}))
}
