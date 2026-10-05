//go:build integration
// +build integration

package fixture

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type Workspace struct {
	ProjectRoot string
	VaultRoot   string
	VaultDef    obsidian.VaultDefinition

	// CodeRoot is the filesystem root used for code indexing/coderefs scanning.
	// It matches VaultRoot but may be symlink-resolved for stability.
	CodeRoot string

	NoteCache *obsidian.NotePathCache
	NoteMgr   *obsidian.Note

	CodeAnchor *codeanchor.Service
	DBPath     string
}

func NewWorkspace(t *testing.T) *Workspace {
	t.Helper()

	workspace := t.TempDir()
	require.NoError(t, CopyDir(PolyglotFixtureRoot(t), workspace))

	projectRoot := workspace
	projectRootResolved := paths.ResolveSymlinks(projectRoot).String()
	if projectRootResolved == "" {
		projectRootResolved = projectRoot
	}
	vaultRoot := filepath.Join(projectRoot, "vault")
	vaultRootResolved := paths.ResolveSymlinks(vaultRoot).String()
	if vaultRootResolved == "" {
		vaultRootResolved = vaultRoot
	}

	vault := &obsidian.Vault{Name: projectRoot}
	vaultDef, err := vault.Definition()
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(projectRootResolved), filepath.Clean(vaultDef.BasePath()))

	notes, err := obsidian.DiscoverFiles(vaultDef)
	require.NoError(t, err)

	codeRoot := vaultRootResolved
	t.Cleanup(func() { StopRuntime(t, codeRoot) })

	return &Workspace{
		ProjectRoot: projectRoot,
		VaultRoot:   vaultRootResolved,
		VaultDef:    vaultDef,
		CodeRoot:    codeRoot,
		NoteCache:   obsidian.BuildNotePathCache(notes),
		NoteMgr:     &obsidian.Note{},
	}
}

func (w *Workspace) CodePath(rel string) string {
	return filepath.Join(w.CodeRoot, filepath.FromSlash(rel))
}

func (w *Workspace) IndexCodeAnchors(t *testing.T, ctx context.Context) {
	t.Helper()
	if w.CodeAnchor != nil {
		return
	}

	dbPath := filepath.Join(w.CodeRoot, ".rhizome", "db.sqlite")
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	pyRoots := []string{"src", "packages", "scripts"}

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{
			codeanchor.NewPythonIndexerWithRoots(pyRoots),
			codeanchor.NewGoIndexer(),
			codeanchor.NewTSIndexerWithRoot(w.CodeRoot),
			codeanchor.NewCSharpIndexerWithRoot(w.CodeRoot),
			codeanchor.NewPHPIndexer(),
		},
		codeanchor.WithBasePath(w.CodeRoot),
		codeanchor.WithWriteAccess(),
	)

	require.NoError(t, ingestNotes(ctx, svc, filepath.Join(w.CodeRoot, "notes")))

	// Order matters: index callees before callers so intel_edges can resolve targets.
	// sync.py and memory.py define functions called by tasks.py; decorators.py defines
	// annotations used elsewhere.
	for _, file := range []struct {
		lang codeanchor.Lang
		rel  string
	}{
		{codeanchor.LangPy, "src/todoapp/services/sync.py"},
		{codeanchor.LangPy, "src/todoapp/storage/memory.py"},
		{codeanchor.LangPy, "packages/observability/decorators.py"},
		{codeanchor.LangPy, "src/todoapp/services/tasks.py"},
		{codeanchor.LangPy, "scripts/worker.py"},
		{codeanchor.LangGo, "go/todo/todo.go"},
		{codeanchor.LangGo, "go/todo/rationale_example.go"},
		{codeanchor.LangGo, "go/worker/worker.go"},
		{codeanchor.LangGo, "go/app/main.go"},
		{codeanchor.LangTS, "tsapp/src/components/SomeComponent.tsx"},
		{codeanchor.LangTS, "tsapp/src/app/App.tsx"},
		{codeanchor.LangCs, "csharp/TaskService.cs"},
		{codeanchor.LangCs, "csharp/SyncClient.cs"},
		{codeanchor.LangCs, "csharp/Worker.cs"},
		{codeanchor.LangCs, "csharp/ModernWorker.cs"},
		{codeanchor.LangCs, "csharp/TypedWorker.cs"},
		{codeanchor.LangCs, "csharp/GlobalUsing.cs"},
		{codeanchor.LangCs, "csharp/LegalStatuses.cs"},
		{codeanchor.LangCs, "csharp/CourtViewModels.cs"},
		{codeanchor.LangCs, "csharp/StaticConsumer.cs"},
		{codeanchor.LangPhp, "php/shared.php"},
		{codeanchor.LangPhp, "php/SyncClient.php"},
		{codeanchor.LangPhp, "php/TaskService.php"},
		{codeanchor.LangPhp, "php/RealWorldController.php"},
		{codeanchor.LangPhp, "php/Worker.php"},
		{codeanchor.LangPhp, "php/helpers.php"},
		// theme.php must index before hooks.php so the string-argument callee resolves.
		{codeanchor.LangPhp, "php/theme.php"},
		{codeanchor.LangPhp, "php/hooks.php"},
		{codeanchor.LangPhp, "php/Container.php"},
		{codeanchor.LangPhp, "php/bootstrap.php"},
		// default-filters-style.php must index AFTER its target function (polyglot_theme_setup
		// is defined in the same file, so order doesn't matter for this particular case, but
		// keeping the rule: target function file first when the string-arg callee is external).
		{codeanchor.LangPhp, "php/default-filters-style.php"},
	} {
		if file.lang == codeanchor.LangCs && !svc.HasIndexer(codeanchor.LangCs) {
			continue
		}
		if file.lang == codeanchor.LangPhp && !svc.HasIndexer(codeanchor.LangPhp) {
			continue
		}
		full := w.CodePath(file.rel)
		content := MustReadBytes(t, full)
		require.NoError(t, svc.IndexCodeFile(ctx, file.lang, full, content))
	}

	require.NoError(t, svc.RecomputeAnchorScopes(ctx))

	w.CodeAnchor = svc
	w.DBPath = dbPath
}

func (w *Workspace) BuildCodeRefs(t *testing.T, files []string) map[string][]coderefs.CodeRef {
	t.Helper()

	out := make(map[string][]coderefs.CodeRef)
	for _, file := range files {
		content := MustReadBytes(t, file)
		rel := RelPath(t, w.CodeRoot, file)
		refs, err := coderefs.ScanFile(rel, content, w.NoteCache)
		require.NoError(t, err)
		if len(refs) > 0 {
			out[rel] = refs
		}
	}
	return out
}

func PolyglotFixtureRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(RepoRoot(t), "testdata", "integration", "python-app")
}

func RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "could not find repo root (go.mod)")
		dir = parent
	}
}

func CopyDir(src, dst string) error {
	rootPaths, _ := paths.NewVaultPaths(src)
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := rootPaths.RelStrict(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel.String())
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func ingestNotes(ctx context.Context, svc *codeanchor.Service, notesDir string) error {
	return filepath.WalkDir(notesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(notesDir), path)
		if err != nil {
			return err
		}
		_, err = svc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot(filepath.ToSlash(rel), string(content), 0))
		return err
	})
}

func MustReadBytes(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return content
}

func RelPath(t *testing.T, root, target string) string {
	t.Helper()
	rootPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := rootPaths.RelStrict(target)
	require.NoError(t, err)
	return rel.String()
}

func AnchorMatch(traces []codeanchor.AnchorMatchTrace, label string) bool {
	for _, t := range traces {
		if t.AnchorLabel == label {
			return true
		}
	}
	return false
}

func AnchorMatchReason(traces []codeanchor.AnchorMatchTrace, label, reason string) bool {
	for _, t := range traces {
		if t.AnchorLabel == label && strings.Contains(t.Reason, reason) {
			return true
		}
	}
	return false
}
