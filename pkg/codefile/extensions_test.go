package codefile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeScriptJavaScriptExtensionsCoverNodeModuleFormats(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{".ts", ".tsx", ".mts", ".cts"}, TypeScriptExtensions())
	require.Equal(t, []string{".js", ".jsx", ".mjs", ".cjs"}, JavaScriptExtensions())

	for _, path := range []string{
		"src/app.ts", "src/view.tsx", "src/module.mts", "src/common.cts",
		"src/app.js", "src/view.jsx", "src/module.mjs", "src/common.cjs",
	} {
		require.Truef(t, IsTypeScriptJavaScriptPath(path), "expected %s to be supported", path)
	}
}

func TestTypeScriptJavaScriptExtensionClassification(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
		require.True(t, IsTypeScriptExtension(ext), ext)
		require.False(t, IsJavaScriptExtension(ext), ext)
	}
	for _, ext := range []string{".js", ".jsx", ".mjs", ".cjs"} {
		require.True(t, IsJavaScriptExtension(ext), ext)
		require.False(t, IsTypeScriptExtension(ext), ext)
	}
	for _, ext := range []string{".json", ".d.ts", ""} {
		require.False(t, IsTypeScriptJavaScriptExtension(ext), ext)
	}
}
