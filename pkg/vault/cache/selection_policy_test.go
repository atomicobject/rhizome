package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestMarkdownCompatibilityAdmissionPreservesMixedCaseMarkdownIdentity(t *testing.T) {
	for _, path := range []paths.NotePath{"notes/Decision.MD", "notes/decision.md"} {
		path := path
		t.Run(path.String(), func(t *testing.T) {
			require.True(t, MarkdownCompatibilityAdmission(path))
		})
	}
	require.False(t, MarkdownCompatibilityAdmission("notes/decision.html"))
}

func TestRuntimeProjectionUsesCurrentProviderFacts(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "docs/Decision.MD", "---\ntags: [Project]\n---\n# Decision\nstatus:: active\n")
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	svc, err := NewService(root, Options{NoteRuntime: &runtime})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	entry, ok := svc.Entry("docs/Decision.MD")
	require.True(t, ok)
	require.Equal(t, "docs/Decision.MD", entry.Path)
	require.Contains(t, entry.Tags, "project")
	require.Equal(t, []string{"active"}, entry.InlineProps["status"])
}

func TestProjectableHTMLIsAdmittedWithoutMarkdownCompatibilityParsing(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "docs/Release.HTML", "---\ntags: [wrong]\n---\n<h1>Release</h1>")
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	svc, err := NewService(root, Options{NoteRuntime: &runtime, AdmitNote: func(path paths.NotePath) bool {
		return path.String() == "docs/Release.HTML"
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	entry, ok := svc.Entry("docs/Release.HTML")
	require.True(t, ok)
	require.Empty(t, entry.Tags, "HTML must use provider facts rather than Markdown-looking frontmatter")
}

func TestStaleRuntimeProjectionIsOmitted(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "notes/stale.note", "not current")
	provider := staleProjectionProvider{}
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	svc, err := NewService(root, Options{NoteRuntime: &runtime, AdmitNote: func(path paths.NotePath) bool {
		return path.String() == "notes/stale.note"
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))
	_, ok := svc.Entry("notes/stale.note")
	require.False(t, ok)
}

type staleProjectionProvider struct{}

func (staleProjectionProvider) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID:                "stale-test",
		Extensions:        []string{".note"},
		ProviderVersion:   "stale-provider-v1",
		ProjectionVersion: "stale-projection-v1",
		OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilitySearchableContentProjection,
		),
	}
}

func (p staleProjectionProvider) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	descriptor := p.Descriptor()
	return noteformat.NewProjectionWithFacts(
		descriptor.ProviderVersion,
		descriptor.ProjectionVersion,
		noteformat.ProjectionStatusStale,
		nil,
		descriptor.Capabilities,
		noteformat.ProjectionFacts{},
	)
}

func TestReplaceSelectionPolicyDiscoversNewSelectionAndEvictsOldSelection(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "first.md", "# First")
	writeCacheFile(t, root, "second.md", "# Second")

	svc, err := NewService(root, Options{
		DiscoverFiles: func() ([]string, error) { return []string{"first.md"}, nil },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))
	require.True(t, hasCacheEntry(svc, "first.md"))
	require.False(t, hasCacheEntry(svc, "second.md"))

	svc.ReplaceSelectionPolicy(SelectionPolicy{
		DiscoverFiles: func() ([]string, error) { return []string{"second.md"}, nil },
	})
	waitResynced(t, svc)
	require.False(t, hasCacheEntry(svc, "first.md"))
	require.True(t, hasCacheEntry(svc, "second.md"))
}

func TestSelectionPolicyRejectsDiscoveryCandidateAndEvictsCachedEntry(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "allowed.md", "# Allowed")
	writeCacheFile(t, root, "rejected.html", "<h1>Not a markdown projection</h1>")

	svc, err := NewService(root, Options{
		DiscoverFiles: func() ([]string, error) {
			return []string{"allowed.md", "rejected.html"}, nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))
	require.True(t, hasCacheEntry(svc, "allowed.md"))
	require.False(t, hasCacheEntry(svc, "rejected.html"))

	svc.ReplaceSelectionPolicy(SelectionPolicy{
		DiscoverFiles: func() ([]string, error) { return []string{"allowed.md"}, nil },
		Admit: func(path paths.NotePath) bool {
			return path.String() == "not-selected.md"
		},
	})
	waitResynced(t, svc)
	require.False(t, hasCacheEntry(svc, "allowed.md"))
}

func TestReplaceSelectionPolicyRemovesOldUserExclude(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "excluded.md", "# Was excluded")

	svc, err := NewService(root, Options{UserExcludes: []string{"excluded.md"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))
	require.False(t, hasCacheEntry(svc, "excluded.md"))

	svc.ReplaceSelectionPolicy(SelectionPolicy{})
	waitResynced(t, svc)
	require.True(t, hasCacheEntry(svc, "excluded.md"))
}

func TestRefreshWithResultReportsRejectedRawEvent(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "note.md", "# Note")
	writeCacheFile(t, root, "rejected.html", "<h1>Not selected</h1>")

	svc, err := NewService(root, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	svc.MarkDirty("rejected.html", DirtyModified)
	result, err := svc.RefreshWithResult(context.Background())
	require.NoError(t, err)
	require.Equal(t, DirtyModified, result.Drained["rejected.html"])
	require.NotContains(t, result.Changed, "rejected.html")
	require.False(t, hasCacheEntry(svc, "rejected.html"))
}

func TestRefreshWithResultColdCacheReportsInitialResync(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "note.md", "# Note")

	svc, err := NewService(root, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	result, err := svc.RefreshWithResult(context.Background())
	require.NoError(t, err)
	require.True(t, result.Resynced)
	require.True(t, hasCacheEntry(svc, "note.md"))
}

func TestRefreshWithResultRequeuesRawBatchAfterPreCommitFailure(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "note.md", "# Before")

	svc, err := NewService(root, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	writeCacheFile(t, root, "note.md", "# After, a longer value")
	svc.MarkDirty("note.md", DirtyModified)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := svc.RefreshWithResult(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, DirtyModified, result.Drained["note.md"])
	require.Equal(t, DirtyModified, svc.DirtySnapshot()["note.md"])

	require.NoError(t, svc.Refresh(context.Background()))
	entry, ok := svc.Entry("note.md")
	require.True(t, ok)
	require.Equal(t, "# After, a longer value", entry.Content)
}

func TestRefreshWithResultReportsPartialChangesBeforeCancellation(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "first.md", "# First before")
	writeCacheFile(t, root, "second.md", "# Second before")

	var cancel context.CancelFunc
	cancelDuringFirstAdmission := false
	svc, err := NewService(root, Options{AdmitNote: func(path paths.NotePath) bool {
		if cancelDuringFirstAdmission && path.String() == "first.md" {
			cancel()
		}
		return true
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	writeCacheFile(t, root, "first.md", "# First after, with more content")
	writeCacheFile(t, root, "second.md", "# Second after, with more content")
	svc.MarkDirty("first.md", DirtyModified)
	svc.MarkDirty("second.md", DirtyModified)
	version := svc.Version()

	ctx, cancelFunc := context.WithCancel(context.Background())
	cancel = cancelFunc
	cancelDuringFirstAdmission = true
	result, err := svc.RefreshWithResult(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, DirtyModified, result.Changed["first.md"])
	require.NotContains(t, result.Changed, "second.md")
	require.Equal(t, version+1, svc.Version())
	require.Len(t, result.Drained, 2)
	require.Len(t, svc.DirtySnapshot(), 2)

	cancelDuringFirstAdmission = false
	require.NoError(t, svc.Refresh(context.Background()))
	first, ok := svc.Entry("first.md")
	require.True(t, ok)
	require.Equal(t, "# First after, with more content", first.Content)
	second, ok := svc.Entry("second.md")
	require.True(t, ok)
	require.Equal(t, "# Second after, with more content", second.Content)
}

func TestReplaceSelectionPolicyConcurrentRefreshConvergesOnFinalPolicy(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "first.md", "# First")
	writeCacheFile(t, root, "second.md", "# Second")

	first := SelectionPolicy{
		DiscoverFiles: func() ([]string, error) { return []string{"first.md"}, nil },
		UserExcludes:  []string{"second.md"},
	}
	second := SelectionPolicy{
		DiscoverFiles: func() ([]string, error) { return []string{"second.md"}, nil },
	}
	svc, err := NewService(root, Options{DiscoverFiles: first.DiscoverFiles, UserExcludes: first.UserExcludes})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 32; i++ {
			if i%2 == 0 {
				svc.ReplaceSelectionPolicy(second)
			} else {
				svc.ReplaceSelectionPolicy(first)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 32; i++ {
			_, _ = svc.RefreshWithResult(context.Background())
		}
	}()
	wg.Wait()

	svc.ReplaceSelectionPolicy(second)
	waitResynced(t, svc)
	require.False(t, hasCacheEntry(svc, "first.md"))
	require.True(t, hasCacheEntry(svc, "second.md"))
}

func TestFailedRecrawlRearmsStale(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, root, "old.md", "# Old")
	writeCacheFile(t, root, "new.md", "# New")

	svc, err := NewService(root, Options{
		DiscoverFiles: func() ([]string, error) { return []string{"old.md"}, nil },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))
	version := svc.Version()

	var failOnce atomic.Bool
	failOnce.Store(true)
	svc.ReplaceSelectionPolicy(SelectionPolicy{
		DiscoverFiles: func() ([]string, error) {
			if failOnce.Swap(false) {
				return nil, errors.New("walk failed")
			}
			return []string{"new.md"}, nil
		},
	})
	waitResynced(t, svc)
	require.Equal(t, uint64(2), svc.Metrics().ResyncCount, "the failed recrawl re-arms stale so a second one runs")
	require.Equal(t, version+2, svc.Version())
	require.False(t, hasCacheEntry(svc, "old.md"))
	require.True(t, hasCacheEntry(svc, "new.md"))
}

func writeCacheFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func hasCacheEntry(svc *Service, path string) bool {
	_, ok := svc.Entry(path)
	return ok
}
