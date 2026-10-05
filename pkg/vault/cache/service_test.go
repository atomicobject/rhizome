package cache

// Docs:
// - [[Vault cache service (Service)]]
// - [[Debugging cache staleness + watcher issues]]

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceInitialCrawlCachesTags(t *testing.T) {
	tmp := t.TempDir()
	content := `---
tags: ["Project"]
---

# Heading
#todo something
`
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Note.md"), []byte(content), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	require.NoError(t, svc.EnsureReady(context.Background()))

	paths := svc.Paths()
	assert.Len(t, paths, 1)
	assert.Equal(t, "Note.md", paths[0])

	entry, ok := svc.Entry("Note.md")
	require.True(t, ok)
	assert.Contains(t, entry.Tags, "project")
	assert.Contains(t, entry.Tags, "todo")
}

func TestServiceRefreshUpdatesModifiedFile(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "Note.md")
	require.NoError(t, os.WriteFile(filePath, []byte("#old"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Modify file content and send a write event.
	require.NoError(t, os.WriteFile(filePath, []byte("#newtag"), 0o644))
	svc.markDirty(filePath, DirtyModified)

	ctx := context.Background()
	var entry Entry
	var ok bool
	svc.mu.RLock()
	dirtyLen := len(svc.dirty)
	svc.mu.RUnlock()
	require.Equal(t, 1, dirtyLen)

	for i := 0; i < 5; i++ {
		require.NoError(t, svc.Refresh(ctx))
		entry, ok = svc.Entry("Note.md")
		if ok && strings.Contains(entry.Content, "#newtag") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	require.True(t, ok)
	assert.Contains(t, entry.Tags, "newtag")
	assert.NotContains(t, entry.Tags, "old")
	assert.Contains(t, entry.Content, "#newtag")
}

func TestRefreshAndDrainDirtyReturnsProcessedSet(t *testing.T) {
	tmp := t.TempDir()
	fileA := filepath.Join(tmp, "A.md")
	fileB := filepath.Join(tmp, "B.md")
	require.NoError(t, os.WriteFile(fileA, []byte("#a-old"), 0o644))
	require.NoError(t, os.WriteFile(fileB, []byte("#b-old"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// First dirty path: captured by an early snapshot.
	// Use content with different size to ensure change is detected even if
	// modtime doesn't change (some filesystems have 1-second resolution).
	require.NoError(t, os.WriteFile(fileA, []byte("#a-new content updated"), 0o644))
	svc.markDirty(fileA, DirtyModified)
	snapshot := svc.DirtySnapshot()
	require.Contains(t, snapshot, "A.md")
	require.NotContains(t, snapshot, "B.md")

	// Second dirty path arrives after the snapshot but before refresh.
	require.NoError(t, os.WriteFile(fileB, []byte("#b-new content updated"), 0o644))
	svc.markDirty(fileB, DirtyModified)

	dirty, resynced, err := svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	assert.False(t, resynced)
	assert.Equal(t, DirtyModified, dirty["A.md"])
	assert.Equal(t, DirtyModified, dirty["B.md"])
	assert.Len(t, dirty, 2, "both dirty paths should be processed in the same pass")

	entryA, ok := svc.Entry("A.md")
	require.True(t, ok)
	assert.Contains(t, entryA.Content, "#a-new")

	entryB, ok := svc.Entry("B.md")
	require.True(t, ok)
	assert.Contains(t, entryB.Content, "#b-new")

	svc.mu.RLock()
	remainingDirty := len(svc.dirty)
	svc.mu.RUnlock()
	assert.Equal(t, 0, remainingDirty, "dirty map should be drained")
}

func TestRefreshAndDrainDirtyReportsInternalFreshnessPaths(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".rhizome", "query-recipes"), 0o755))
	recipePath := filepath.Join(tmp, ".rhizome", "query-recipes", "spec-driven.yaml")
	require.NoError(t, os.WriteFile(recipePath, []byte("apiVersion: rhizome.query-recipe.v1\n"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	svc.MarkDirty(".rhizome/query-recipes/spec-driven.yaml", DirtyModified)
	dirty, resynced, err := svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Equal(t, DirtyModified, dirty[".rhizome/query-recipes/spec-driven.yaml"])
}

func TestRefreshAndDrainDirtyReportsMissingInternalFreshnessPaths(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".rhizome", "query-recipes"), 0o755))
	recipePath := filepath.Join(tmp, ".rhizome", "query-recipes", "spec-driven.yaml")
	require.NoError(t, os.WriteFile(recipePath, []byte("apiVersion: rhizome.query-recipe.v1\n"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	require.NoError(t, os.Remove(recipePath))
	svc.MarkDirty(".rhizome/query-recipes/spec-driven.yaml", DirtyModified)
	dirty, resynced, err := svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Equal(t, DirtyRemoved, dirty[".rhizome/query-recipes/spec-driven.yaml"])
}

func TestRefreshAndDrainDirtySkipsUnchangedFiles(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "Note.md")
	require.NoError(t, os.WriteFile(filePath, []byte("#note"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	require.NoError(t, os.WriteFile(filePath, []byte("#note updated"), 0o644))
	svc.MarkDirty("Note.md", DirtyModified)
	dirty, resynced, err := svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Len(t, dirty, 1)

	svc.MarkDirty("Note.md", DirtyModified)
	dirty, resynced, err = svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Empty(t, dirty)

	// A later change is still detected after an unchanged refresh. The new
	// content has a different length so a coarse mtime cannot hide it.
	require.NoError(t, os.WriteFile(filePath, []byte("#note updated again"), 0o644))
	svc.MarkDirty("Note.md", DirtyModified)
	dirty, resynced, err = svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Contains(t, dirty, "Note.md")
	entry, ok := svc.Entry("Note.md")
	require.True(t, ok)
	require.Equal(t, "#note updated again", entry.Content)
}

func TestRefreshAndDrainDirtySkipsUnchangedCodeFiles(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Note.md"), []byte("#note"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main\n"), 0o644))

	svc, err := NewService(tmp, Options{CodeRefConfig: coderefs.NewConfig(true, nil, nil)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	svc.MarkDirty("main.go", DirtyModified)
	dirty, resynced, err := svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Empty(t, dirty)

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main\n\n// updated\n"), 0o644))
	svc.MarkDirty("main.go", DirtyModified)
	dirty, resynced, err = svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Equal(t, DirtyModified, dirty["main.go"])
}

func TestServiceRefreshRemovesDeletedFile(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "Note.md")
	require.NoError(t, os.WriteFile(filePath, []byte("#tag"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	require.NoError(t, os.Remove(filePath))
	svc.markDirty(filePath, DirtyRemoved)

	require.NoError(t, svc.Refresh(context.Background()))

	_, ok := svc.Entry("Note.md")
	assert.False(t, ok)
	assert.Empty(t, svc.Paths())
}

func TestServiceHandlesRenameEvent(t *testing.T) {
	tmp := t.TempDir()
	orig := filepath.Join(tmp, "Old.md")
	require.NoError(t, os.WriteFile(orig, []byte("#tag"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	newPath := filepath.Join(tmp, "New.md")
	require.NoError(t, os.Rename(orig, newPath))

	svc.markDirty(orig, DirtyRenamed)
	require.NoError(t, svc.Refresh(context.Background()))

	_, oldOk := svc.Entry("Old.md")
	assert.False(t, oldOk, "old name should be removed")

	entry, newOk := svc.Entry("New.md")
	assert.True(t, newOk, "new name should be indexed")
	assert.Contains(t, entry.Tags, "tag")
}

func TestServiceConcurrentEnsureReady(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Note.md"), []byte("#tag"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	// Launch multiple concurrent EnsureReady calls
	const goroutines = 5
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := svc.EnsureReady(context.Background()); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	// Verify cache is populated correctly
	paths := svc.Paths()
	assert.Len(t, paths, 1)
	assert.Equal(t, "Note.md", paths[0])
}

func TestServiceEnsureReadyWaitsThroughSupersedingCrawl(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Note.md"), []byte("#tag"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	firstCrawl := make(chan struct{})
	nextCrawl := make(chan struct{})
	svc.mu.Lock()
	svc.crawling = true
	svc.crawlCh = firstCrawl
	svc.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.EnsureReady(context.Background())
	}()

	require.Eventually(t, func() bool {
		svc.mu.RLock()
		defer svc.mu.RUnlock()
		return svc.crawling && svc.crawlCh == firstCrawl
	}, time.Second, time.Millisecond)

	svc.mu.Lock()
	svc.crawlCh = nextCrawl
	svc.mu.Unlock()
	close(firstCrawl)

	select {
	case err := <-errCh:
		require.NoError(t, err)
		t.Fatal("EnsureReady returned before the superseding crawl finished")
	case <-time.After(25 * time.Millisecond):
	}

	svc.mu.Lock()
	svc.ready = true
	svc.crawling = false
	svc.mu.Unlock()
	close(nextCrawl)

	require.NoError(t, <-errCh)
}

func TestRefreshPathRemovesExcludedEntry(t *testing.T) {
	tmp := t.TempDir()
	content := `# Heading
#tag`
	notePath := filepath.Join(tmp, "Note.md")
	require.NoError(t, os.WriteFile(notePath, []byte(content), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	require.NoError(t, svc.EnsureReady(context.Background()))
	require.ElementsMatch(t, []string{"Note.md"}, svc.Paths())

	svc.mu.Lock()
	svc.ignoreMatcher = ignore.NewMatcher([]string{"Note.md"})
	svc.mu.Unlock()

	_, err = svc.refreshPath(notePath, false)
	require.NoError(t, err)

	require.Empty(t, svc.Paths())
	svc.mu.RLock()
	_, tagged := svc.tagIndex["tag"]
	svc.mu.RUnlock()
	require.False(t, tagged, "expected tag index to drop excluded note")
}

func TestServiceStaleRevalidation(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "Note.md")
	require.NoError(t, os.WriteFile(filePath, []byte("#original"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Modify the file behind the watcher's back
	time.Sleep(10 * time.Millisecond) // Ensure mtime changes
	require.NoError(t, os.WriteFile(filePath, []byte("#updated"), 0o644))

	// Manually mark cache as stale (simulating watcher failure)
	svc.markStale()
	waitResynced(t, svc)

	entry, ok := svc.Entry("Note.md")
	require.True(t, ok)
	assert.Contains(t, entry.Tags, "updated")
	assert.NotContains(t, entry.Tags, "original")
}

func TestServiceStaleResyncFindsNewFiles(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Existing.md"), []byte("#old"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Add a new file after the watcher goes stale.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "New.md"), []byte("#new"), 0o644))
	svc.markStale()
	waitResynced(t, svc)

	_, oldOk := svc.Entry("Existing.md")
	assert.True(t, oldOk)
	_, newOk := svc.Entry("New.md")
	assert.True(t, newOk, "resync should discover newly created files")
}

func TestServiceRenameDirectoryRescansChildren(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "Folder", "Sub"), 0o755))
	origFile := filepath.Join(tmp, "Folder", "Sub", "Note.md")
	require.NoError(t, os.WriteFile(origFile, []byte("#tag"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	renamedDir := filepath.Join(tmp, "Renamed")
	require.NoError(t, os.Rename(filepath.Join(tmp, "Folder"), renamedDir))

	svc.markDirty(filepath.Join(tmp, "Folder"), DirtyRenamed)
	require.NoError(t, svc.Refresh(context.Background()))

	_, oldOk := svc.Entry("Folder/Sub/Note.md")
	assert.False(t, oldOk, "old path should be removed after directory rename")

	entry, newOk := svc.Entry("Renamed/Sub/Note.md")
	assert.True(t, newOk, "renamed path should be indexed")
	assert.Contains(t, entry.Tags, "tag")
}

func TestServiceModifiedMissingDirectoryRemovesChildren(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "Folder", "Sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Folder", "Sub", "Note1.md"), []byte("#one"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Folder", "Sub", "Note2.md"), []byte("#two"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	_, ok := svc.Entry("Folder/Sub/Note1.md")
	require.True(t, ok)
	_, ok = svc.Entry("Folder/Sub/Note2.md")
	require.True(t, ok)

	require.NoError(t, os.RemoveAll(filepath.Join(tmp, "Folder")))

	// Simulate Windows watcher behavior where only the parent dir may be marked modified.
	svc.MarkDirty("Folder", DirtyModified)
	require.NoError(t, svc.Refresh(context.Background()))

	_, ok = svc.Entry("Folder/Sub/Note1.md")
	assert.False(t, ok)
	_, ok = svc.Entry("Folder/Sub/Note2.md")
	assert.False(t, ok)
}

func TestServiceEntryDeepCopy(t *testing.T) {
	tmp := t.TempDir()
	content := `---
tags: ["original"]
custom: value
---
#inline
`
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Note.md"), []byte(content), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Get entry and mutate it
	entry, ok := svc.Entry("Note.md")
	require.True(t, ok)

	// Mutate the returned entry
	entry.Tags[0] = "MUTATED"
	entry.Content = "MUTATED CONTENT"

	// Get entry again and verify cache wasn't affected
	entry2, ok := svc.Entry("Note.md")
	require.True(t, ok)
	assert.NotEqual(t, "MUTATED", entry2.Tags[0], "mutating returned entry should not affect cache")
	assert.NotContains(t, entry2.Content, "MUTATED", "mutating returned entry should not affect cache")
}

func TestServiceRespectsObsidianIgnore(t *testing.T) {
	tmp := t.TempDir()

	// Create ignored file
	ignoredPath := filepath.Join(tmp, "Ignored.md")
	require.NoError(t, os.WriteFile(ignoredPath, []byte("#secret"), 0o644))

	// Create included file
	includedPath := filepath.Join(tmp, "Included.md")
	require.NoError(t, os.WriteFile(includedPath, []byte("#public"), 0o644))

	// Create .rhizome/ignore
	ignoreFile := filepath.Join(tmp, ".rhizome/ignore")
	require.NoError(t, os.MkdirAll(filepath.Dir(ignoreFile), 0o755))
	require.NoError(t, os.WriteFile(ignoreFile, []byte("Ignored.md\n"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	paths := svc.Paths()
	assert.Len(t, paths, 1)
	assert.Equal(t, "Included.md", paths[0])

	_, ok := svc.Entry("Ignored.md")
	assert.False(t, ok, "ignored file should not be indexed")
}

func TestServiceUsesDefaultIgnoreWhenMissing(t *testing.T) {
	tmp := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "node_modules", "mod"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "node_modules", "mod", "ignored.md"), []byte("#ignored"), 0o644))

	included := filepath.Join(tmp, "Included.md")
	require.NoError(t, os.WriteFile(included, []byte("#public"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	paths := svc.Paths()
	assert.Len(t, paths, 1)
	assert.Equal(t, "Included.md", paths[0])
}

func TestServiceHandlesRecreation(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "Note.md")
	require.NoError(t, os.WriteFile(filePath, []byte("#old"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Simulate rapid remove then create (recreation)
	// 1. Remove
	require.NoError(t, os.Remove(filePath))
	svc.markDirty(filePath, DirtyRemoved)

	// 2. Create (new content)
	require.NoError(t, os.WriteFile(filePath, []byte("#new"), 0o644))
	svc.markDirty(filePath, DirtyCreated)

	// Refresh should handle the transition: remove old entry, read new entry
	require.NoError(t, svc.Refresh(context.Background()))

	entry, ok := svc.Entry("Note.md")
	require.True(t, ok, "file should exist after recreation")
	assert.Contains(t, entry.Content, "#new")
	assert.Contains(t, entry.Tags, "new")
	assert.NotContains(t, entry.Tags, "old")
}

func TestServiceHandlesDirectoryRecreation(t *testing.T) {
	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "Folder")
	require.NoError(t, os.Mkdir(dirPath, 0o755))
	filePath := filepath.Join(dirPath, "Note.md")
	require.NoError(t, os.WriteFile(filePath, []byte("#old"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Verify initial state
	_, ok := svc.Entry("Folder/Note.md")
	require.True(t, ok)

	// Simulate rapid remove dir then create dir
	require.NoError(t, os.RemoveAll(dirPath))
	svc.markDirty(filepath.Join(tmp, "Folder"), DirtyRemoved)

	require.NoError(t, os.Mkdir(dirPath, 0o755))
	// New file in new dir
	newFilePath := filepath.Join(dirPath, "NewNote.md")
	require.NoError(t, os.WriteFile(newFilePath, []byte("#new"), 0o644))

	svc.markDirty(filepath.Join(tmp, "Folder"), DirtyCreated)

	// Refresh should remove old tree and scan new dir
	require.NoError(t, svc.Refresh(context.Background()))

	_, oldOk := svc.Entry("Folder/Note.md")
	assert.False(t, oldOk, "old file should be gone")

	entry, newOk := svc.Entry("Folder/NewNote.md")
	require.True(t, newOk, "new file should be found")
	assert.Contains(t, entry.Content, "#new")
}

func TestObsidianIgnoreChangesTriggerResync(t *testing.T) {
	tmp := t.TempDir()

	ignoredPath := filepath.Join(tmp, "Ignored.md")
	includedPath := filepath.Join(tmp, "Included.md")
	require.NoError(t, os.WriteFile(ignoredPath, []byte("#secret"), 0o644))
	require.NoError(t, os.WriteFile(includedPath, []byte("#public"), 0o644))
	ignoreFile := filepath.Join(tmp, ".rhizome/ignore")
	require.NoError(t, os.MkdirAll(filepath.Dir(ignoreFile), 0o755))
	require.NoError(t, os.WriteFile(ignoreFile, []byte("Ignored.md\n"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	_, ok := svc.Entry("Ignored.md")
	require.False(t, ok, "ignored file should not be indexed initially")

	// Update ignore file to stop ignoring; reload ignore patterns and mark stale.
	require.NoError(t, os.WriteFile(ignoreFile, []byte("\n"), 0o644))
	svc.loadIgnorePatterns()
	svc.markStale()
	waitResynced(t, svc)

	entry, ok := svc.Entry("Ignored.md")
	require.True(t, ok, "ignored file should be indexed after ignore change")
	assert.Contains(t, entry.Content, "#secret")
}

func TestResyncBlocksConcurrentInitialCrawl(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Note.md"), []byte("#tag"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	initialVersion := svc.Version()

	// Force resync and race with EnsureReady; version should only bump once.
	svc.markStale()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = svc.Refresh(context.Background())
	}()

	require.NoError(t, svc.EnsureReady(context.Background()))
	wg.Wait()
	require.Eventually(t, func() bool {
		svc.mu.RLock()
		defer svc.mu.RUnlock()
		return svc.ready && !svc.stale && !svc.crawling && !svc.recrawling
	}, 5*time.Second, 5*time.Millisecond, "concurrent refreshes should settle after one resync")

	assert.Equal(t, initialVersion+1, svc.Version(), "resync should not allow a second concurrent initial crawl")
}

func TestExcludePatterns_InitialCrawl(t *testing.T) {
	tmp := t.TempDir()

	// Create files including excluded ones
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "public.md"), []byte("#public"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "private"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "private", "secret.md"), []byte("#secret"), 0o644))

	svc, err := NewService(tmp, Options{
		UserExcludes: []string{"private/"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Public file should be indexed
	_, ok := svc.Entry("public.md")
	require.True(t, ok, "public file should be indexed")

	// Excluded file should not be indexed
	_, ok = svc.Entry("private/secret.md")
	require.False(t, ok, "excluded file should not be indexed")
}

func TestExcludePatterns_RefreshPath(t *testing.T) {
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "public.md"), []byte("#public"), 0o644))

	svc, err := NewService(tmp, Options{
		UserExcludes: []string{"excluded/"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Create excluded file after initial crawl
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "excluded"), 0o755))
	excludedPath := filepath.Join(tmp, "excluded", "new.md")
	require.NoError(t, os.WriteFile(excludedPath, []byte("#excluded"), 0o644))

	svc.markDirty(excludedPath, DirtyCreated)
	require.NoError(t, svc.Refresh(context.Background()))

	// Excluded file should still not be indexed
	_, ok := svc.Entry("excluded/new.md")
	require.False(t, ok, "newly created excluded file should not be indexed")
}

func TestExcludePatterns_RescanDir(t *testing.T) {
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "public.md"), []byte("#public"), 0o644))

	svc, err := NewService(tmp, Options{
		UserExcludes: []string{"**/skip.md"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// Create new directory with both included and excluded files
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "newdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "newdir", "keep.md"), []byte("#keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "newdir", "skip.md"), []byte("#skip"), 0o644))

	svc.markDirty(filepath.Join(tmp, "newdir"), DirtyCreated)
	require.NoError(t, svc.Refresh(context.Background()))

	// Non-excluded file should be indexed
	_, ok := svc.Entry("newdir/keep.md")
	require.True(t, ok, "non-excluded file should be indexed")

	// Excluded file should not be indexed
	_, ok = svc.Entry("newdir/skip.md")
	require.False(t, ok, "excluded file should not be indexed")
}

func TestRefreshAndDrainDirtyReportsMarkdownDiscoveredByDirectoryRescan(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Existing.md"), []byte("# Existing"), 0o644))

	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	nested := filepath.Join(tmp, "docs", "playground")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "pizza-party-2026.md"), []byte(`# Pizza

- [ ] Order extra firewood #action-item
`), 0o644))

	svc.MarkDirty("docs/playground", DirtyCreated)
	dirty, resynced, err := svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Contains(t, dirty, "docs/playground/pizza-party-2026.md")
}

func TestCollectionCacheContextDocInitialAndIncrementalParity(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "private", "service"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "hard-hidden"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "keep.md"), []byte("# Keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "private", "service", "CONTEXT.md"), []byte("# Initial"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "private", "service", "ordinary.md"), []byte("# Ordinary"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "hard-hidden", "CONTEXT.md"), []byte("# Hidden"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ignore"), []byte("hard-hidden/\n"), 0o644))

	def := obsidian.VaultDefinition{
		Root:     root,
		Includes: []string{"notes/**/*.md"},
		Excludes: []string{"private/**"},
	}
	svc, err := NewService(root, Options{
		DiscoverFiles: func() ([]string, error) { return obsidian.DiscoverFiles(def) },
		UserExcludes:  def.Excludes,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	entry, ok := svc.Entry("private/service/CONTEXT.md")
	require.True(t, ok)
	require.Contains(t, entry.Content, "Initial")
	_, ok = svc.Entry("private/service/ordinary.md")
	require.False(t, ok)
	_, ok = svc.Entry("hard-hidden/CONTEXT.md")
	require.False(t, ok)

	require.NoError(t, os.WriteFile(filepath.Join(root, "private", "service", "CONTEXT.md"), []byte("# Updated"), 0o644))
	svc.MarkDirty("private/service/CONTEXT.md", DirtyModified)
	require.NoError(t, svc.Refresh(context.Background()))
	entry, ok = svc.Entry("private/service/CONTEXT.md")
	require.True(t, ok)
	require.Contains(t, entry.Content, "Updated")

	require.NoError(t, os.MkdirAll(filepath.Join(root, "private", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "private", "nested", "CONTEXT.md"), []byte("# Nested"), 0o644))
	svc.MarkDirty("private/nested", DirtyCreated)
	dirty, resynced, err := svc.RefreshAndDrainDirty(context.Background())
	require.NoError(t, err)
	require.False(t, resynced)
	require.Contains(t, dirty, "private/nested/CONTEXT.md")
	_, ok = svc.Entry("private/nested/CONTEXT.md")
	require.True(t, ok)
}

// waitResynced drains refreshes until the background recrawl reports
// completion through RefreshResult.Resynced.
func waitResynced(t *testing.T, svc *Service) {
	t.Helper()
	sawResynced := false
	require.Eventually(t, func() bool {
		result, err := svc.RefreshWithResult(context.Background())
		require.NoError(t, err)
		sawResynced = sawResynced || result.Resynced
		if !sawResynced {
			return false
		}
		svc.mu.RLock()
		settled := !svc.stale && !svc.recrawling
		svc.mu.RUnlock()
		return settled
	}, 5*time.Second, 5*time.Millisecond)
}

func TestReadsServeLastIndexDuringResync(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "Note.md"), []byte("#tag"), 0o644))

	var crawls atomic.Int32
	resyncStarted := make(chan struct{})
	releaseResync := make(chan struct{})
	svc, err := NewService(tmp, Options{DiscoverFiles: func() ([]string, error) {
		if crawls.Add(1) == 2 {
			close(resyncStarted)
			<-releaseResync
		}
		return []string{"Note.md"}, nil
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	adapter := NewNoteAdapter(svc, &fakeNoteReader{})
	readQuickly := func(label string) {
		t.Helper()
		read := make(chan string, 1)
		go func() {
			content, err := adapter.GetContents(obsidian.VaultDefinition{Path: tmp}, "Note.md")
			require.NoError(t, err)
			read <- content
		}()
		select {
		case content := <-read:
			assert.Equal(t, "#tag", content)
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("%s waited on the stale-triggered recrawl", label)
		}
	}

	// The first caller after MarkStale is the one that claims the stale flag.
	svc.MarkStale()
	readQuickly("first reader after MarkStale")
	<-resyncStarted
	readQuickly("reader during recrawl")

	close(releaseResync)
	waitResynced(t, svc)
	_, ok := svc.Entry("Note.md")
	assert.True(t, ok)
}

func TestStaleRecrawlRereadsMatchingStatTuple(t *testing.T) {
	tmp := t.TempDir()
	notePath := filepath.Join(tmp, "Note.md")
	require.NoError(t, os.WriteFile(notePath, []byte("#old"), 0o644))
	svc, err := NewService(tmp, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))
	entry, ok := svc.Entry("Note.md")
	require.True(t, ok)

	// A missed watcher event: same size, same mtime, different bytes.
	require.NoError(t, os.WriteFile(notePath, []byte("#new"), 0o644))
	require.NoError(t, os.Chtimes(notePath, entry.ModTime, entry.ModTime))

	svc.MarkStale()
	waitResynced(t, svc)
	entry, ok = svc.Entry("Note.md")
	require.True(t, ok)
	assert.Equal(t, []string{"new"}, entry.Tags)
}

func TestRecrawlYieldsToNewerDirtyRefresh(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "notes/a.note", "old")
	notePath := filepath.Join(root, "notes", "a.note")
	past := time.Now().Add(-time.Minute)
	require.NoError(t, os.Chtimes(notePath, past, past))

	var armed atomic.Bool
	var once sync.Once
	readOld := make(chan struct{})
	release := make(chan struct{})
	provider := gatedProjectionProvider{gate: func(content []byte) {
		if armed.Load() && string(content) == "old" {
			once.Do(func() { close(readOld) })
			<-release
		}
	}}
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	svc, err := NewService(root, Options{NoteRuntime: &runtime, AdmitNote: func(path paths.NotePath) bool {
		return path.String() == "notes/a.note"
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	// The recrawl reads the old bytes, then parks before committing them.
	armed.Store(true)
	svc.MarkStale()
	require.NoError(t, svc.Refresh(context.Background()))
	<-readOld

	// A delivered watcher event commits different bytes with the same mtime.
	// Entry identity, rather than timestamp ordering, must make this update win.
	writeCacheFile(t, root, "notes/a.note", "newer")
	require.NoError(t, os.Chtimes(notePath, past, past))
	svc.MarkDirty("notes/a.note", DirtyModified)
	require.NoError(t, svc.Refresh(context.Background()))
	entry, ok := svc.Entry("notes/a.note")
	require.True(t, ok)
	require.Equal(t, "newer", entry.Content)

	close(release)
	waitResynced(t, svc)
	entry, ok = svc.Entry("notes/a.note")
	require.True(t, ok)
	assert.Equal(t, "newer", entry.Content)
}

func TestRecrawlDoesNotResurrectConcurrentRemoval(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "notes/a.note", "old")
	notePath := filepath.Join(root, "notes", "a.note")

	var armed atomic.Bool
	var once sync.Once
	readOld := make(chan struct{})
	release := make(chan struct{})
	provider := gatedProjectionProvider{gate: func(content []byte) {
		if armed.Load() && string(content) == "old" {
			once.Do(func() { close(readOld) })
			<-release
		}
	}}
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	svc, err := NewService(root, Options{NoteRuntime: &runtime, AdmitNote: func(path paths.NotePath) bool {
		return path.String() == "notes/a.note"
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	armed.Store(true)
	svc.MarkStale()
	require.NoError(t, svc.Refresh(context.Background()))
	<-readOld

	require.NoError(t, os.Remove(notePath))
	svc.MarkDirty("notes/a.note", DirtyRemoved)
	require.NoError(t, svc.Refresh(context.Background()))
	_, ok := svc.Entry("notes/a.note")
	require.False(t, ok)

	close(release)
	waitResynced(t, svc)
	_, ok = svc.Entry("notes/a.note")
	assert.False(t, ok)
}

type gatedProjectionProvider struct {
	gate func(content []byte)
}

func (gatedProjectionProvider) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID:                "gated-test",
		Extensions:        []string{".note"},
		ProviderVersion:   "gated-provider-v1",
		ProjectionVersion: "gated-projection-v1",
		OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilitySearchableContentProjection,
		),
	}
}

func (p gatedProjectionProvider) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	p.gate(source.Bytes())
	descriptor := p.Descriptor()
	return noteformat.NewProjectionWithFacts(
		descriptor.ProviderVersion,
		descriptor.ProjectionVersion,
		noteformat.ProjectionStatusCurrent,
		nil,
		descriptor.Capabilities,
		noteformat.ProjectionFacts{},
	)
}
