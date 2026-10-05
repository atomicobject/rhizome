//go:build cgo

package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNewCodeAnchorService_UsesTreeSitterLimits(t *testing.T) {
	t.Setenv("RHIZOME_TS_PARSE_TIMEOUT", "")
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "code.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	tests := []struct {
		lang             codeanchor.Lang
		path             string
		start, line, end string
	}{
		{codeanchor.LangPy, "src/many.py", "", "def f%d():\n    return %d\n", ""},
		{codeanchor.LangTS, "src/many.ts", "", "export function f%d() { return %d; }\n", ""},
		{codeanchor.LangCs, "src/Many.cs", "class Many {\n", "public int F%d() { return %d; }\n", "}\n"},
		{codeanchor.LangPhp, "src/many.php", "<?php\n", "function f%d() { return %d; }\n", ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.lang), func(t *testing.T) {
			var source strings.Builder
			source.WriteString(tt.start)
			for i := 0; i < 2000; i++ {
				source.WriteString(fmt.Sprintf(tt.line, i, i))
			}
			source.WriteString(tt.end)
			abs := filepath.Join(root, tt.path)
			tiny, _ := NewCodeAnchorService(CodeAnchorServiceConfig{
				VaultPath: root, CodeCfg: codeanchor.Config{TSParseTimeout: time.Nanosecond},
				Store: store, IncludeIndexers: true, WriteAccess: true,
			})
			require.NotNil(t, tiny)
			require.NoError(t, tiny.IndexCodeFile(context.Background(), tt.lang, abs, []byte(source.String())))
			_, _, status, found, err := store.FileHash(context.Background(), tt.path)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, codeanchor.ParseTimeout, status)

			normal, _ := NewCodeAnchorService(CodeAnchorServiceConfig{
				VaultPath: root, CodeCfg: codeanchor.Config{TSParseTimeout: 10 * time.Second},
				Store: store, IncludeIndexers: true, WriteAccess: true,
			})
			require.NotNil(t, normal)
			require.NoError(t, normal.IndexCodeFile(context.Background(), tt.lang, abs, []byte(source.String())))
			_, _, status, found, err = store.FileHash(context.Background(), tt.path)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, codeanchor.ParseOK, status)
		})
	}
}

func TestNewCodeAnchorService_AutomaticScopeLeavesOutDisabledLanguages(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "code.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	build := func(cfg codeanchor.Config) *codeanchor.Service {
		service, _ := NewCodeAnchorService(CodeAnchorServiceConfig{VaultPath: root, CodeCfg: cfg, Store: store, IncludeIndexers: true})
		return service
	}

	automatic := build(codeanchor.Config{Enabled: true, AutomaticScope: true, DisabledLanguages: []string{"python", "cs"}})
	require.False(t, automatic.HasIndexer(codeanchor.LangPy))
	require.False(t, automatic.HasIndexer(codeanchor.LangCs))
	require.True(t, automatic.HasIndexer(codeanchor.LangGo))

	// Named folders keep their earlier behavior: the list only stops prompts.
	limited := build(codeanchor.Config{Enabled: true, GoRoots: []string{"."}, DisabledLanguages: []string{"python"}})
	require.True(t, limited.HasIndexer(codeanchor.LangPy))
}
