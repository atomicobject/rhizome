package notemeta

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProjectedSourceSnapshotSeparatesCopiedRawBytesAndDerivedEvidence(t *testing.T) {
	source := projectionTestSource(t, "notes/source.md", "source bytes")
	projection := projectionTestProjection(t, noteformat.ProjectionStatusCurrent, noteformat.ProjectionFacts{
		RootMetadata:     []noteformat.RootMetadataFact{{Key: "status", Value: metadataValue(t, "active")}},
		InlineProperties: []noteformat.InlinePropertyFact{{Key: "owner", Value: "Ada"}},
		SearchRegions: []noteformat.SearchRegionFact{{
			Origin: noteformat.SearchRegionAuthored, Kind: noteformat.SearchRegionVisible, Text: "source bytes", MediaType: "text/markdown",
			Range: noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: 0, EndByte: 12}},
		}},
		FragmentTargets: []noteformat.FragmentTargetFact{{Kind: noteformat.FragmentTargetHeading, Text: "Source", NormalizedText: "source", Ordinal: 1}},
	})
	entry, err := projectionEntryFrom(source, projection)
	require.NoError(t, err)
	snapshot := sourceFactsSnapshotFromProjectedEntry(entry)

	require.Equal(t, []byte("source bytes"), snapshot.RawSource)
	require.Equal(t, "source bytes", snapshot.Content)
	require.Equal(t, projection.Facts.SearchRegions, snapshot.SearchRegions)
	snapshot.RawSource[0] = 'X'
	snapshot.SearchRegions[0].Text = "changed"
	snapshot.Frontmatter["status"] = "changed"
	snapshot.InlineProps["owner"][0] = "Grace"
	require.Equal(t, []byte("source bytes"), source.Bytes())
	require.Equal(t, "source bytes", entry.Projection.Facts.SearchRegions[0].Text)
	require.Equal(t, "active", entry.Entry.Frontmatter["status"])
	require.Equal(t, []string{"Ada"}, entry.Entry.InlineProps["owner"])
}

var _ codeanchor.NoteSource = NoteSourceSnapshot{}

func TestLoadNoteSourceSnapshotsPreservesHashMtimeAndAliases(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	sourceContent := "---\naliases: [SOURCE]\n---\nSee [[TARGET]].\n"
	targetContent := "---\naliases: [TARGET]\n---\nTarget\n"
	sourcePath := filepath.Join(root, "notes", "source.md")
	targetPath := filepath.Join(root, "notes", "target.md")
	require.NoError(t, os.WriteFile(sourcePath, []byte(sourceContent), 0o644))
	require.NoError(t, os.WriteFile(targetPath, []byte(targetContent), 0o644))
	fixed := time.Unix(1_700_000_000, 0)
	require.NoError(t, os.Chtimes(sourcePath, fixed, fixed))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	vaultDef := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	_, err = testIndexer(t).EnsureIndexed(context.Background(), vaultDef, note, store)
	require.NoError(t, err)

	sources, err := testIndexer(t).LoadNoteSourceSnapshots(context.Background(), vaultDef, note, store, []string{"notes/source.md"})
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Equal(t, contentHash(sourceContent), sources[0].ContentHash)
	require.Equal(t, fixed.Unix(), sources[0].Mtime)
	require.Equal(t, []string{"SOURCE"}, sources[0].Aliases)
	require.Condition(t, func() bool {
		for _, link := range sources[0].Links {
			if link.TargetPath.String() == "notes/target.md" {
				return true
			}
		}
		return false
	})
}

func TestIndexerLoadNoteSourceSnapshotsRejectsStaleProjection(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	content := "# Source\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "source.md"), []byte(content), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	vaultDef := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	_, err = indexer.EnsureIndexed(context.Background(), vaultDef, note, store)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	rows[0].Projection.Status = semdb.NoteProjectionStatusStale
	state, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(context.Background(), semdb.NoteMetadataDelta{
		State: state,
		Notes: []semdb.NoteMetadataRow{rows[0]},
	}))

	_, err = indexer.LoadNoteSourceSnapshots(context.Background(), vaultDef, note, store, []string{"notes/source.md"})
	require.ErrorIs(t, err, ErrProjectionNotCurrent)

	rows, err = store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1, "the read gate must retain stale source identity")
	require.Equal(t, "notes/source.md", rows[0].Path)
}

func TestIndexerLoadNoteSourceSnapshotsSkipsFatalSourcesUnlessRequested(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "current.md"), []byte("# Current\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "fatal.md"), []byte("# Fatal\n"), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: root}
	_, err = indexer.EnsureIndexed(ctx, vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/fatal.md"})
	require.NoError(t, err)
	fatal := rows["notes/fatal.md"]
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	fatal.Projection.Status = semdb.NoteProjectionStatusFatal
	fatal.Projection.DiagnosticCode = "projector_fatal"
	fatal.Projection.DiagnosticDetail = "source rejected"
	err = store.ApplyNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{State: state, Notes: []semdb.NoteMetadataRow{fatal}})
	require.NoError(t, err)

	sources, err := indexer.LoadNoteSourceSnapshots(ctx, vault, &obsidian.Note{}, store, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/current.md"}, noteSourcePaths(sources))
	_, err = indexer.LoadNoteSourceSnapshots(ctx, vault, &obsidian.Note{}, store, []string{"notes/fatal.md"})
	require.ErrorIs(t, err, ErrProjectionNotCurrent)
}

func TestIndexerLoadNoteSourceSnapshotsExcludesDescriptorOnlyStaleSources(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	markdownContent := "---\naliases: [CURRENT]\n---\nSee [[view]].\n"
	htmlContent := "<p>descriptor-only source must not be read</p>\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "current.md"), []byte(markdownContent), 0o644))
	htmlPath := filepath.Join(root, "notes", "view.html")
	require.NoError(t, os.WriteFile(htmlPath, []byte(htmlContent), 0o644))
	htmlInfo, err := os.Stat(htmlPath)
	require.NoError(t, err)

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	runtime := descriptorOnlyHTMLRuntime(t)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: root}
	_, err = indexer.EnsureIndexed(ctx, vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	htmlProvider, ok := runtime.Provider("html")
	require.True(t, ok)
	htmlDescriptor := htmlProvider.Descriptor()
	_, err = store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{
		Path: "notes/view.html", Target: semdb.OwnershipTargetNote,
		Note: &semdb.NoteSourceState{
			Title: "View", FormatID: string(htmlDescriptor.ID), ContentHash: contentHash(htmlContent),
			Mtime: htmlInfo.ModTime().Unix(), Size: htmlInfo.Size(), ProviderVersion: htmlDescriptor.ProviderVersion,
			ProjectionVersion: htmlDescriptor.ProjectionVersion, Status: semdb.NoteProjectionStatusStale, ObservedAt: 17,
		},
	}})
	require.NoError(t, err)
	delta, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, []paths.NotePath{"notes/current.md", "notes/view.html"}, nil)
	require.NoError(t, err)
	require.NotNil(t, delta)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *delta))

	reader := &staleCachedCountingReader{delegate: &obsidian.Note{}, reads: make(map[string]int)}
	sources, err := indexer.LoadNoteSourceSnapshots(ctx, vault, reader, store, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/current.md"}, noteSourcePaths(sources))
	require.Equal(t, 1, reader.readCount("notes/current.md"))
	require.Zero(t, reader.readCount("notes/view.html"), "descriptor-only source must not enter source reads")
	require.Empty(t, sources[0].Links, "descriptor-only source must not enter the link-target cache")
}

func TestIndexerBuildNoteSourceSnapshotsPreservesLegacyMetadataValueShapes(t *testing.T) {
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	reader := fakeNoteReader{notes: map[string]string{"source.md": `---
enabled: true
count: 7
ratio: 1.25
date: 2026-08-03
timestamp: 2026-08-03T12:34:56.123456789-04:00
items: [2, false, 2026-08-04]
nested:
  score: 9
---
# Source
`}}

	snapshots, err := indexer.BuildNoteSourceSnapshots(context.Background(), obsidian.VaultDefinition{Path: t.TempDir()}, reader)
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	frontmatter := snapshots[0].Frontmatter
	require.IsType(t, true, frontmatter["enabled"])
	require.IsType(t, int(0), frontmatter["count"])
	require.IsType(t, float64(0), frontmatter["ratio"])
	date, ok := frontmatter["date"].(time.Time)
	require.True(t, ok)
	require.Equal(t, "2026-08-03", date.Format(time.DateOnly))
	timestamp, ok := frontmatter["timestamp"].(time.Time)
	require.True(t, ok)
	require.Equal(t, "2026-08-03T12:34:56.123456789-04:00", timestamp.Format(time.RFC3339Nano))
	items, ok := frontmatter["items"].([]any)
	require.True(t, ok)
	require.IsType(t, int(0), items[0])
	require.IsType(t, false, items[1])
	require.IsType(t, time.Time{}, items[2])
	nested, ok := frontmatter["nested"].(map[string]any)
	require.True(t, ok)
	require.IsType(t, int(0), nested["score"])
}

func TestBuildNoteSourceSnapshotsUsesCanonicalSourceContractWithoutStore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	content := "---\nstatus: active\naliases: [SRC]\ntags: [phase-one]\n---\n# Source\nSee [[ALPHA]].\nowner:: Ada\n"
	path := filepath.Join(root, "notes", "zeta.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "alpha.md"), []byte("---\naliases: [ALPHA]\n---\n# Alpha\n"), 0o644))
	fixed := time.Unix(1_700_000_000, 0)
	require.NoError(t, os.Chtimes(path, fixed, fixed))

	sources, err := testIndexer(t).BuildNoteSourceSnapshots(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{})
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "notes/alpha.md", sources[0].Path.String())
	require.Equal(t, []string{"ALPHA"}, sources[0].Aliases)
	require.Equal(t, "notes/zeta.md", sources[1].Path.String())
	require.Equal(t, content, sources[1].Content)
	require.Equal(t, contentHash(content), sources[1].ContentHash)
	require.Equal(t, fixed.Unix(), sources[1].Mtime)
	require.Equal(t, int64(len(content)), sources[1].Size)
	require.Equal(t, "Source", sources[1].Title)
	require.Equal(t, "active", sources[1].Frontmatter["status"])
	require.Equal(t, []string{"Ada"}, sources[1].InlineProps["owner"])
	require.Equal(t, []string{"phase-one"}, sources[1].Tags)
	require.Equal(t, []string{"SRC"}, sources[1].Aliases)
	require.Condition(t, func() bool {
		for _, link := range sources[1].Links {
			if link.TargetInput == "ALPHA" && link.TargetPath.String() == "notes/alpha.md" && link.Kind == "wikilink" {
				return true
			}
		}
		return false
	})
}

func TestBuildNoteSourceFactsUsesCanonicalAliasShapes(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		"list.md":    "---\naliases: [SPEC-001, ' padded ', '', spec-indexed-search]\n---\n",
		"scalar.md":  "---\naliases: ONLYONE\n---\n",
		"missing.md": "---\ntitle: No aliases\n---\n",
		"blank.md":   "---\naliases: ['', '   ']\n---\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}
	sources, err := BuildNoteSourceFacts(context.Background(), obsidian.VaultDefinition{Path: root})
	require.NoError(t, err)
	require.Len(t, sources, 4)
	aliases := make(map[string][]string, len(sources))
	for _, source := range sources {
		aliases[source.Path.String()] = source.Aliases
	}
	require.Len(t, aliases, 4)
	for _, path := range []string{"list.md", "scalar.md", "missing.md", "blank.md"} {
		require.Contains(t, aliases, path)
	}
	require.Equal(t, []string{"SPEC-001", "padded", "spec-indexed-search"}, aliases["list.md"])
	require.Equal(t, []string{"ONLYONE"}, aliases["scalar.md"])
	require.Empty(t, aliases["missing.md"])
	require.Empty(t, aliases["blank.md"])
}

func TestIndexerBuildNoteSourceSnapshotsPreservesAuthoredPathAndProjection(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Decision.MD"), []byte("# Decision\nSee [[Target]].\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Target.MD"), []byte("# Target\n"), 0o644))

	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)

	sources, err := indexer.BuildNoteSourceSnapshots(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{})
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, "notes/Decision.MD", sources[0].Path.String())
	require.Equal(t, noteformat.FormatID("markdown"), sources[0].Format)
	require.Equal(t, noteformat.ProjectionStatusCurrent, sources[0].Projection.Status)
	require.Equal(t, "Decision", sources[0].Title)
	require.Equal(t, []ResolvedNoteLink{{
		SourcePath:  sources[0].Path,
		TargetInput: "Target",
		TargetPath:  sources[1].Path,
		Kind:        "wikilink",
	}, {
		SourcePath:  sources[0].Path,
		TargetInput: "Target",
		TargetPath:  sources[1].Path,
		Kind:        "note_link:wikilink:basic",
	}}, sources[0].Links)
}

func TestBuildNoteSourceFactsReadsLiveContentOnceWithoutEagerLinkResolution(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "source.md"), []byte("---\naliases: [SOURCE]\n---\nSee [[TARGET]].\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "target.md"), []byte("# Target\n"), 0o644))
	reader := &staleCachedCountingReader{delegate: &obsidian.Note{}, reads: make(map[string]int)}

	sources, err := buildNoteSourceFactsWithReader(context.Background(), obsidian.VaultDefinition{Path: root}, reader)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/source.md", "notes/target.md"}, noteSourcePaths(sources))
	require.Equal(t, map[string]int{"notes/source.md": 1, "notes/target.md": 1}, reader.readCounts())
	require.Zero(t, reader.cacheReads, "source facts must not trust a potentially stale cache snapshot")
	require.Equal(t, []string{"SOURCE"}, sources[0].Aliases)
	for _, source := range sources {
		require.Empty(t, source.Links, "the consumer owns the one structured-link scan")
	}
}

func TestBuildNoteSourceFactsIgnoresStaleCacheAdapterAndEnumeratesDisk(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	existingPath := filepath.Join(root, "notes", "existing.md")
	require.NoError(t, os.WriteFile(existingPath, []byte("old cached bytes\n"), 0o644))
	service, err := cache.NewService(root, cache.Options{})
	require.NoError(t, err)
	adapter := cache.NewNoteAdapter(service, &obsidian.Note{})
	require.NoError(t, service.EnsureReady(ctx))

	require.NoError(t, os.WriteFile(existingPath, []byte("new authoritative bytes\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "added.md"), []byte("new note\n"), 0o644))
	vaultDef := obsidian.VaultDefinition{Path: root}
	staleContent, err := adapter.GetContents(vaultDef, "notes/existing.md")
	require.NoError(t, err)
	require.Equal(t, "old cached bytes\n", staleContent)
	stalePaths, err := adapter.GetNotesList(vaultDef)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/existing.md"}, stalePaths)

	sources, err := BuildNoteSourceFacts(ctx, vaultDef)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/added.md", "notes/existing.md"}, noteSourcePaths(sources))
	require.Equal(t, "new note\n", sources[0].Content)
	require.Equal(t, "new authoritative bytes\n", sources[1].Content)
}

type staleCachedCountingReader struct {
	delegate   obsidian.NoteReader
	mu         sync.Mutex
	reads      map[string]int
	cacheReads int
}

func (r *staleCachedCountingReader) GetContents(vault obsidian.VaultDefinition, notePath string) (string, error) {
	r.mu.Lock()
	r.reads[notePath]++
	r.mu.Unlock()
	return r.delegate.GetContents(vault, notePath)
}

func (r *staleCachedCountingReader) GetNotesList(vault obsidian.VaultDefinition) ([]string, error) {
	return r.delegate.GetNotesList(vault)
}

func (r *staleCachedCountingReader) GetModTime(vault obsidian.VaultDefinition, notePath string) (time.Time, error) {
	return r.delegate.GetModTime(vault, notePath)
}

func (r *staleCachedCountingReader) Title(notePath string) (string, bool) {
	return r.delegate.Title(notePath)
}

func (r *staleCachedCountingReader) EntriesSnapshot(context.Context) ([]cache.Entry, error) {
	r.mu.Lock()
	r.cacheReads++
	r.mu.Unlock()
	return nil, nil
}

func (r *staleCachedCountingReader) readCounts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]int{"notes/source.md": r.reads["notes/source.md"], "notes/target.md": r.reads["notes/target.md"]}
}

func (r *staleCachedCountingReader) readCount(notePath string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads[notePath]
}

func noteSourcePaths(sources []NoteSourceSnapshot) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.Path.String())
	}
	return out
}

func TestLoadNoteSourceSnapshotsHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := testIndexer(t).LoadNoteSourceSnapshots(ctx, obsidian.VaultDefinition{}, &obsidian.Note{}, canceledSourceStore{}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

type canceledSourceStore struct{}

func (canceledSourceStore) GetNoteMetadataState(context.Context) (MetadataState, error) {
	return MetadataState{}, nil
}
func (canceledSourceStore) ReplaceNoteMetadataSnapshot(context.Context, MetadataSnapshot) error {
	return nil
}
func (canceledSourceStore) ApplyNoteMetadataDelta(context.Context, MetadataDelta) error { return nil }
func (canceledSourceStore) AllNoteMetadataPaths(context.Context) ([]string, error) {
	return nil, nil
}
func (canceledSourceStore) CurrentNoteMetadataPaths(context.Context) ([]string, error) {
	return nil, nil
}
func (canceledSourceStore) CurrentNoteMetadataRowsByPaths(context.Context, []string) (map[string]MetadataRow, error) {
	return nil, nil
}
func (canceledSourceStore) ResolveStoredNoteLinks(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (canceledSourceStore) CurrentNoteAliases(context.Context) (map[string][]string, error) {
	return nil, nil
}

func TestNewContentOnlyNoteSourceSnapshotSatisfiesAnchorSourceContract(t *testing.T) {
	snapshot := NewContentOnlyNoteSourceSnapshot("docs/source.md", "# Source\n", 123)

	require.Equal(t, "docs/source.md", snapshot.NotePathString())
	require.Equal(t, "# Source\n", snapshot.NoteContentString())
	require.Equal(t, contentHash("# Source\n"), snapshot.NoteContentHashString())
	require.Equal(t, int64(123), snapshot.NoteMtimeUnix())
	require.Equal(t, int64(len("# Source\n")), snapshot.Size)
	require.Equal(t, "Source", snapshot.Title)
}

func TestNewContentOnlyNoteSourceSnapshotPreservesAuthoredMarkdownPathCase(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "mixed case markdown suffix remains authored",
			path: "docs/Decision.MD",
			want: "docs/Decision.MD",
		},
		{
			name: "extensionless legacy input adds markdown suffix",
			path: "docs/decision",
			want: "docs/decision.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := NewContentOnlyNoteSourceSnapshot(tt.path, "# Decision\n", 0)

			require.Equal(t, tt.want, snapshot.Path.String())
		})
	}
}
