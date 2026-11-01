package watchhub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A module created after the hub starts must still report schema edits on
// non-recursive backends, which only watch folders whose create events pass
// the path filter.
func TestWatchHubReportsSchemaInModuleCreatedAfterStart(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))

	hub, err := NewHub(root, Options{ForceFSNotify: true, Debounce: time.Millisecond})
	require.NoError(t, err)
	hub.Start(context.Background())
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes, WatchForIgnore: true}))
	waitForBackendReady(t, hub, 5*time.Second)

	events := make(chan WatchEvent, 64)
	stale := make(chan StaleEvent, 64)
	hub.Subscribe("module-schema", Filter{IncludeFiles: true, IncludeDirs: true}, func(_ context.Context, batch []WatchEvent) {
		for _, event := range batch {
			events <- event
		}
	}, func(_ context.Context, event StaleEvent) { stale <- event })

	ontology := filepath.Join(root, ".rhizome", "modules", "late", "ontology")
	for _, dir := range []string{filepath.Dir(filepath.Dir(ontology)), filepath.Dir(ontology), ontology} {
		require.NoError(t, os.Mkdir(dir, 0o755))
		require.Eventually(t, func() bool {
			backend := hub.backendSnapshot()
			if backend == nil {
				return false
			}
			for _, watched := range backend.WatchList() {
				if normalizePath(watched) == normalizePath(dir) {
					return true
				}
			}
			return false
		}, 5*time.Second, 10*time.Millisecond, "watch installed for %s", dir)
	}
	require.NoError(t, os.WriteFile(filepath.Join(ontology, "late.graphql"), []byte("type Late { id: ID }\n"), 0o644))
	waitForRelPath(t, events, stale, ".rhizome/modules/late/ontology/late.graphql", 5*time.Second)
}
