package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestBatchParserConfigurationReparsesUnchangedPython(t *testing.T) {
	for _, tc := range []struct {
		name, before, after, want string
	}{
		{"changed roots", "python:\n    roots: [src]", "python:\n    roots: [src/pkg]", "util.answer"},
		{"empty language block", "go:\n    roots: [src]", "go:\n    roots: [src]\n  python: {}", "src.pkg.util.answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "src/pkg"), 0755))
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "src/pkg/util.py"), []byte("def answer():\n    return 42\n"), 0600))
			run := func(code string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/config.yml"), []byte("code:\n  enabled: true\n  "+code+"\n"), 0600))
				resolved, err := repoconfig.Load(root)
				require.NoError(t, err)
				require.NoError(t, runUnifiedCoreForTest(t, context.Background(), UnifiedOptions{VaultPath: root, VaultDef: repoconfig.LocalConfigToDefinition(root, resolved.Local), Config: &resolved, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true}))
			}
			run(tc.before)
			store := openTouchParityStore(t, root)
			var fqn string
			require.NoError(t, store.DB().QueryRow(`SELECT fqn FROM symbols WHERE file='src/pkg/util.py' AND kind='func'`).Scan(&fqn))
			require.Equal(t, "pkg.util.answer", fqn)
			run(tc.after)
			require.NoError(t, store.DB().QueryRow(`SELECT fqn FROM symbols WHERE file='src/pkg/util.py' AND kind='func'`).Scan(&fqn))
			require.Equal(t, tc.want, fqn)
		})
	}
}
