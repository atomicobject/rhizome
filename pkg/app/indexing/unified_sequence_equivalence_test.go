package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestQuietLiveRuntimeMatchesFreshBatchSequences(t *testing.T) {
	write := func(t *testing.T, root, path, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o600))
	}
	selection := func(t *testing.T, root string, excluded bool) {
		t.Helper()
		config, err := repoconfig.LoadLocalConfig(root)
		require.NoError(t, err)
		config.Notes.Excludes = nil
		if excluded {
			config.Notes.Excludes = []string{"notes/project-0001.md"}
		}
		require.NoError(t, repoconfig.SaveLocalConfig(root, *config))
	}
	for _, sequence := range []struct {
		name        string
		initial     func(*testing.T, string)
		change      func(*testing.T, string)
		dirty       map[string]cache.DirtyKind
		incremental bool
	}{
		{name: "edit", change: func(t *testing.T, root string) {
			write(t, root, "notes/project-0002.md", indexingParityNote(2, 4, "Changed"))
			write(t, root, "src/unit_0000.go", "package workload\n// Unit0 follows [[project-0002]].\nfunc Unit0(value int) int { return Unit1(value + 7) }\n")
		}, dirty: map[string]cache.DirtyKind{"notes/project-0002.md": cache.DirtyModified, "src/unit_0000.go": cache.DirtyModified}},
		{name: "code only edit", change: func(t *testing.T, root string) {
			write(t, root, "src/unit_0000.go", "package workload\n// Unit0 follows [[project-0002]].\nfunc Unit0(value int) int { return Unit1(value) }\nfunc Added() {}\n")
		}, dirty: map[string]cache.DirtyKind{"src/unit_0000.go": cache.DirtyModified}},
		{name: "code mention removed", change: func(t *testing.T, root string) {
			write(t, root, "src/unit_0000.go", "package workload\nfunc Unit0(value int) int { return Unit1(value) }\n")
		}, dirty: map[string]cache.DirtyKind{"src/unit_0000.go": cache.DirtyModified}},
		{name: "note and code edit", change: func(t *testing.T, root string) {
			write(t, root, "notes/added.md", indexingParityNote(4, 5, "Added"))
			write(t, root, "src/unit_0000.go", "package workload\n// Unit0 follows [[added]].\nfunc Unit0(value int) int { return Unit1(value) }\n")
		}, dirty: map[string]cache.DirtyKind{"notes/added.md": cache.DirtyCreated, "src/unit_0000.go": cache.DirtyModified}},
		{name: "note added", change: func(t *testing.T, root string) {
			write(t, root, "notes/added.md", indexingParityNote(4, 5, "Added"))
		}, dirty: map[string]cache.DirtyKind{"notes/added.md": cache.DirtyCreated}, incremental: true},
		{name: "note renamed", change: func(t *testing.T, root string) {
			require.NoError(t, os.Rename(filepath.Join(root, "notes/project-0001.md"), filepath.Join(root, "notes/added.md")))
		}, dirty: map[string]cache.DirtyKind{"notes/project-0001.md": cache.DirtyRemoved, "notes/added.md": cache.DirtyCreated}, incremental: true},
		{name: "delete", change: func(t *testing.T, root string) {
			require.NoError(t, os.Remove(filepath.Join(root, "notes/project-0001.md")))
			require.NoError(t, os.Remove(filepath.Join(root, "src/unit_0000.go")))
		}, dirty: map[string]cache.DirtyKind{"notes/project-0001.md": cache.DirtyRemoved, "src/unit_0000.go": cache.DirtyRemoved}},
		{name: "rename", change: func(t *testing.T, root string) {
			require.NoError(t, os.Rename(filepath.Join(root, "notes/project-0001.md"), filepath.Join(root, "notes/renamed.md")))
			require.NoError(t, os.Rename(filepath.Join(root, "src/unit_0000.go"), filepath.Join(root, "src/renamed.go")))
		}, dirty: map[string]cache.DirtyKind{"notes/project-0001.md": cache.DirtyRemoved, "notes/renamed.md": cache.DirtyCreated, "src/unit_0000.go": cache.DirtyRemoved, "src/renamed.go": cache.DirtyCreated}},
		{name: "exclude added", change: func(t *testing.T, root string) { selection(t, root, true) }, dirty: map[string]cache.DirtyKind{".rhizome/config.yml": cache.DirtyModified}},
		{name: "exclude removed", initial: func(t *testing.T, root string) { selection(t, root, true) }, change: func(t *testing.T, root string) { selection(t, root, false) }, dirty: map[string]cache.DirtyKind{".rhizome/config.yml": cache.DirtyModified}},
	} {
		t.Run(sequence.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			liveRoot, freshRoot := t.TempDir(), t.TempDir()
			roots := []string{liveRoot, freshRoot}
			var incrementalRoot string
			if sequence.incremental {
				incrementalRoot = t.TempDir()
				roots = append(roots, incrementalRoot)
			}
			for _, root := range roots {
				writeIndexingParityWorkload(t, root, 4, 2)
				mention := "[[project-0000]]"
				if sequence.incremental {
					mention = "[[project-0001]] and [[added]]"
				}
				write(t, root, "src/unit_0000.go", "package workload\n// Unit0 follows "+mention+".\nfunc Unit0(value int) int { return Unit1(value) }\n")
				if sequence.initial != nil {
					sequence.initial(t, root)
				}
			}
			run := func(root string) {
				t.Helper()
				definition, err := repoconfig.LoadDefinitionFromPath(root)
				require.NoError(t, err)
				require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{
					VaultPath: root, VaultDef: definition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true,
				}))
			}
			run(liveRoot)
			if sequence.incremental {
				run(incrementalRoot)
			}
			rt, err := bootstrap.NewLiveRuntime(ctx, bootstrap.LiveOptions{VaultName: liveRoot, DisableWatchHub: true, DisableSessionStore: true})
			require.NoError(t, err)
			defer func() { require.NoError(t, rt.Close()) }()
			require.NoError(t, rt.WaitForSearch(ctx))
			require.NoError(t, rt.WaitForCodeIndex(ctx))
			quiet := func() bool {
				last := rt.LiveHealth().LastCompletedEpoch
				debt, err := workloadDerivedDebt(ctx, rt.IntelStore())
				return err == nil && debt == 0 && last != nil && last.Status == "completed" && !rt.Lane().Status().Busy
			}
			require.True(t, pollWorkloadCondition(ctx, 10*time.Second, 10*time.Millisecond, quiet), "initial runtime must settle")
			sharedService := rt.CodeAnchorService()
			beforeContext, err := sharedService.NotesForFile(ctx, "src/unit_0000.go")
			require.NoError(t, err)
			previousEpoch := rt.LiveHealth().LastCompletedEpoch.ID
			for _, root := range roots {
				sequence.change(t, root)
			}
			run(freshRoot)
			fresh, closeFresh, err := indexstore.Open(freshRoot, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
			require.NoError(t, err)
			defer closeFresh()
			want, err := canonicalWorkloadSnapshot(t, ctx, fresh, freshRoot)
			require.NoError(t, err)
			require.NotEmpty(t, want["scope"])
			require.NotEmpty(t, want["graph-doc-scores"])
			var incremental map[string][]string
			if sequence.incremental {
				run(incrementalRoot)
				store, closeStore, err := indexstore.Open(incrementalRoot, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
				require.NoError(t, err)
				incremental, err = canonicalWorkloadSnapshot(t, ctx, store, incrementalRoot)
				closeStore()
				require.NoError(t, err)
			}
			for path, kind := range sequence.dirty {
				rt.Cache().MarkDirty(path, kind)
			}
			require.True(t, pollWorkloadCondition(ctx, 10*time.Second, 10*time.Millisecond, func() bool {
				last := rt.LiveHealth().LastCompletedEpoch
				return last != nil && last.ID > previousEpoch && quiet()
			}), "runtime must publish and drain the changed files")
			got, err := canonicalWorkloadSnapshot(t, ctx, rt.IntelStore(), liveRoot)
			require.NoError(t, err)
			for name, rows := range want {
				if sequence.incremental && (name == "doc-links" || name == "graph-doc-scores" || name == "graph-anchors") {
					continue
				}
				require.Equal(t, rows, got[name], "deterministic table %s", name)
			}
			for name, rows := range incremental {
				require.Equal(t, rows, got[name], "incremental batch table %s", name)
			}
			if sequence.name == "edit" || sequence.name == "code only edit" || sequence.name == "note and code edit" {
				require.NotEmpty(t, got["doc-links"], "edited code must resolve persisted notes")
			}
			if sequence.name == "code mention removed" {
				require.Empty(t, got["doc-links"], "removing the last mention must clear persisted links")
			}
			if sequence.name == "code only edit" {
				require.Same(t, sharedService, rt.CodeAnchorService())
				afterContext, err := sharedService.NotesForFile(ctx, "src/unit_0000.go")
				require.NoError(t, err)
				require.Greater(t, len(afterContext.Symbols), len(beforeContext.Symbols), "shared readers must see symbols published by the batch service")
			}
		})
	}
}
