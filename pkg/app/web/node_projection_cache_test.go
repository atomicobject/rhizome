package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const projectionCacheTestNote = "specs/demo/spec.md"

// gatedNoteReader holds the first `hold` GetContents calls open after they have
// read the file, so a test can observe a load that finishes after the source
// was rewritten and reloaded by someone else.
type gatedNoteReader struct {
	projectionCountingNoteReader
	mu      sync.Mutex
	hold    int
	entered chan struct{}
	release chan struct{}
}

func newGatedNoteReader(root string, hold int) *gatedNoteReader {
	return &gatedNoteReader{
		projectionCountingNoteReader: projectionCountingNoteReader{root: root},
		hold:                         hold,
		entered:                      make(chan struct{}, hold),
		release:                      make(chan struct{}),
	}
}

func (r *gatedNoteReader) GetContents(vaultDef obsidian.VaultDefinition, noteName string) (string, error) {
	body, err := r.projectionCountingNoteReader.GetContents(vaultDef, noteName)
	r.mu.Lock()
	held := r.hold > 0
	if held {
		r.hold--
	}
	r.mu.Unlock()
	if held {
		r.entered <- struct{}{}
		<-r.release
	}
	return body, err
}

type projectionCacheFixture struct {
	root     string
	path     string
	schema   *ontology.Schema
	vaultDef obsidian.VaultDefinition
	ref      ontology.NodeRef
}

func newProjectionCacheFixture(t *testing.T) projectionCacheFixture {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Spec @node(paths: ["specs/*/spec.md"]) {
  summary: String!
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs", "demo"), 0o755))
	fixture := projectionCacheFixture{
		root:     root,
		path:     filepath.Join(root, filepath.FromSlash(projectionCacheTestNote)),
		vaultDef: obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth},
		ref:      ontology.NodeRef{NotePath: projectionCacheTestNote, Kind: ontology.NodeKindNote},
	}
	fixture.write(t, "First", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	fixture.schema = schema
	return fixture
}

func (f projectionCacheFixture) write(t *testing.T, summary string, modTime time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(f.path, []byte("---\ntype: Spec\nsummary: "+summary+"\n---\n\n# Demo\n"), 0o644))
	require.NoError(t, os.Chtimes(f.path, modTime, modTime))
}

func (f projectionCacheFixture) project(t *testing.T, cache *nodeProjectionCache, reader obsidian.NoteReader) *ontology.NodeProjection {
	t.Helper()
	projection, err := cache.Projection(context.Background(), f.vaultDef, reader, f.schema, f.ref)
	require.NoError(t, err)
	return projection
}

// obsoleteLoadRace starts one load that stays open, rewrites the note and
// completes a fresher load, then lets the obsolete load finish.
func obsoleteLoadRace(t *testing.T, fixture projectionCacheFixture, cache *nodeProjectionCache, reader *gatedNoteReader, start func()) *ontology.NodeProjection {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		start()
	}()
	<-reader.entered

	fixture.write(t, "Second", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	cache.InvalidatePaths([]string{projectionCacheTestNote})
	fresh := fixture.project(t, cache, reader)
	require.Equal(t, 2, reader.contentsCalls())
	require.Equal(t, []string{"Second"}, fresh.Fields["summary"].Values)

	close(reader.release)
	<-done
	return fresh
}

func TestNodeProjectionCacheObsoleteLoadDoesNotReplaceFresherEntry(t *testing.T) {
	fixture := newProjectionCacheFixture(t)
	reader := newGatedNoteReader(fixture.root, 1)
	cache := newNodeProjectionCache(8)

	var obsolete *ontology.NodeProjection
	fresh := obsoleteLoadRace(t, fixture, cache, reader, func() {
		obsolete = fixture.project(t, cache, reader)
	})
	require.Equal(t, []string{"First"}, obsolete.Fields["summary"].Values)
	require.NotEqual(t, fresh.Snapshot.ContentFingerprint, obsolete.Snapshot.ContentFingerprint)

	third := fixture.project(t, cache, reader)
	require.Equal(t, []string{"Second"}, third.Fields["summary"].Values, "obsolete load replaced the fresher entry")
	require.Equal(t, fresh.Snapshot.ContentFingerprint, third.Snapshot.ContentFingerprint)
	require.Equal(t, 2, reader.contentsCalls(), "fresh entry should serve the third call without a reload")
}

func TestNodeProjectionCacheInvalidationRejectsInFlightLoadWithUnchangedTimestamp(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("all=%t", all), func(t *testing.T) {
			fixture := newProjectionCacheFixture(t)
			reader := newGatedNoteReader(fixture.root, 1)
			cache := newNodeProjectionCache(8)
			done := make(chan struct{})
			go func() { defer close(done); fixture.project(t, cache, reader) }()
			<-reader.entered
			fixture.write(t, "Second", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			if all {
				cache.InvalidateAll()
			} else {
				cache.InvalidatePaths([]string{projectionCacheTestNote})
			}
			close(reader.release)
			<-done
			require.Equal(t, []string{"Second"}, fixture.project(t, cache, reader).Fields["summary"].Values)
		})
	}
}

func TestNodeProjectionCacheInvalidationRejectsObsoleteWarmCompletion(t *testing.T) {
	for _, test := range []struct {
		name          string
		changeModTime bool
	}{
		{name: "same mtime"},
		{name: "changed mtime", changeModTime: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProjectionCacheFixture(t)
			reader := newGatedNoteReader(fixture.root, 0)
			cache := newNodeProjectionCache(8)
			obsolete := fixture.project(t, cache, reader)
			entry := cache.entries[projectionCacheTestNote]
			modTime := obsolete.Snapshot.ModTime
			if test.changeModTime {
				modTime = modTime.Add(24 * time.Hour)
			}
			// Complete the old warm deterministically after a replacement entry is populated.
			fixture.write(t, "Second", modTime)
			cache.InvalidatePaths([]string{projectionCacheTestNote})
			fresh := fixture.project(t, cache, reader)
			require.Equal(t, 2, reader.contentsCalls())
			cache.finishWarm(projectionCacheTestNote, entry, ontologySchemaHash(fixture.schema), obsolete.Snapshot.ModTime, obsolete.Snapshot, []*ontology.NodeProjection{obsolete}, true)

			final := fixture.project(t, cache, reader)
			require.Equal(t, []string{"Second"}, final.Fields["summary"].Values)
			require.Equal(t, fresh.Snapshot.ContentFingerprint, final.Snapshot.ContentFingerprint)
			require.Equal(t, 2, reader.contentsCalls(), "fresh entry should serve without a reload")
		})
	}
}
