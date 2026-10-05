package coderefs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRewriteBatchFileSizeAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		binary bool
	}{
		{name: "small", size: 4 << 10},
		{name: "small binary", size: 4 << 10, binary: true},
		{name: "exact limit", size: MaxFileSizeBytes},
		{name: "one byte over limit", size: MaxFileSizeBytes + 1},
		{name: "oversized", size: 16 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, note := range []string{"Old.md", "New.md"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, note), []byte("# Fixture\n"), 0o644))
			}
			const prefix = "package fixture\n// [[Old.md]]\n"
			content := []byte(prefix + strings.Repeat(" ", tc.size-len(prefix)))
			if tc.binary {
				content[len(prefix)] = 0
			}
			file := filepath.Join(root, "refs.go")
			require.NoError(t, os.WriteFile(file, content, 0o755))
			originalInfo, err := os.Stat(file)
			require.NoError(t, err)
			// An ordinary sibling must still be rewritten when refs.go is skipped.
			control := filepath.Join(root, "control.go")
			require.NoError(t, os.WriteFile(control, []byte(prefix), 0o644))
			cfg := NewConfig(true, []string{"*.go"}, nil)
			mappings := []RefMapping{{OldPath: "Old.md", NewPath: "New.md"}}

			result, err := RewriteBatch(root, cfg, mappings)
			require.NoError(t, err)
			want := RewriteResult{FilesUpdated: 1, RefsUpdated: 1}
			wantContent := content
			if tc.size <= MaxFileSizeBytes && !tc.binary {
				want = RewriteResult{FilesUpdated: 2, RefsUpdated: 2}
				wantContent = []byte(strings.Replace(string(content), "Old.md", "New.md", 1))
			}
			require.Equal(t, want, result)
			actual, err := os.ReadFile(file)
			require.NoError(t, err)
			require.True(t, bytes.Equal(wantContent, actual), "candidate contents must match the admitted rewrite or unchanged skip")
			actualInfo, err := os.Stat(file)
			require.NoError(t, err)
			require.Equal(t, originalInfo.Mode().Perm(), actualInfo.Mode().Perm())
			actualControl, err := os.ReadFile(control)
			require.NoError(t, err)
			require.Equal(t, strings.Replace(prefix, "Old.md", "New.md", 1), string(actualControl))

			if tc.size > MaxFileSizeBytes {
				// The same selected source and supported reference become eligible
				// when reduced below the limit, isolating size as the skip reason.
				require.NoError(t, os.WriteFile(file, []byte(prefix), 0o644))
				result, err = RewriteBatch(root, cfg, mappings)
				require.NoError(t, err)
				require.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 1}, result)
				actual, err = os.ReadFile(file)
				require.NoError(t, err)
				require.Equal(t, strings.Replace(prefix, "Old.md", "New.md", 1), string(actual))
			}
		})
	}
}

func TestRewriteBatchOversizedFileAllocationBudget(t *testing.T) {
	root := t.TempDir()
	for _, note := range []string{"Old.md", "New.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, note), []byte("# Fixture\n"), 0o644))
	}
	const prefix = "package fixture\n// [[Old.md]]\n"
	content := []byte(prefix + strings.Repeat(" ", (16<<20)-len(prefix)))
	file := filepath.Join(root, "refs.go")
	require.NoError(t, os.WriteFile(file, content, 0o644))
	cfg := NewConfig(true, []string{"*.go"}, nil)
	mappings := []RefMapping{{OldPath: "Old.md", NewPath: "New.md"}}

	// Fixture allocation is outside the measurement. Leave ample room for
	// discovery and mapping work while rejecting a read of the 16 MiB source.
	var result RewriteResult
	var err error
	measurement := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result, err = RewriteBatch(root, cfg, mappings)
			if err != nil || result != (RewriteResult{}) {
				b.Fatalf("oversized rewrite = %+v, %v; want no changes", result, err)
			}
		}
	})
	require.NoError(t, err)
	require.Equal(t, RewriteResult{}, result)
	t.Logf("oversized rewrite allocated %d B/op", measurement.AllocedBytesPerOp())
	require.Less(t, measurement.AllocedBytesPerOp(), int64(1<<20))

	// Prove that this exact selected source rewrites when size permits it.
	require.NoError(t, os.WriteFile(file, []byte(prefix), 0o644))
	result, err = RewriteBatch(root, cfg, mappings)
	require.NoError(t, err)
	require.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 1}, result)
}
