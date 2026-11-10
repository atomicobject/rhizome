package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/stretchr/testify/require"
)

// SPEC-0136 R4: the generation is `epoch.writes.schema.views.validation.config`
// and each component changes when its source does.
func TestIndexGenerationChangesWithEachComponent(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()

	initial := srv.IndexGeneration(ctx)
	require.Len(t, strings.Split(initial, "."), 6, initial)
	require.Equal(t, initial, srv.IndexGeneration(ctx), "nothing changed")

	component := func(generation string, index int) string {
		return strings.Split(generation, ".")[index]
	}
	changes := []struct {
		name  string
		index int
		apply func()
	}{
		{"store write", 1, func() {
			_, err := fixture.intelStore.SetValidationRunning(ctx)
			require.NoError(t, err)
		}},
		{"schema reload", 2, func() {
			path := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, append(data, []byte("\ntype Extra @node(paths: [\"extra/*.md\"]) {\n  name: String\n}\n")...), 0o644))
		}},
		{"view definition change", 3, func() {
			dir := filepath.Join(fixture.root, ".rhizome", "views")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "specs.yaml"), []byte("apiVersion: rhizome.view.v1\nid: specs\nname: Specs\nsource: {kind: ontology_type, type: Spec}\n"), 0o644))
		}},
		{"validation run", 4, func() {
			_, err := fixture.intelStore.SetValidationRunning(ctx)
			require.NoError(t, err)
		}},
		{"configuration publication", 5, func() {
			srv.runtime.Live = &webRuntimeView{snapshot: runtimeview.Snapshot{IntelStore: srv.runtime.Intel(), ConfigGeneration: "published-2"}}
		}},
	}
	for _, change := range changes {
		before := srv.IndexGeneration(ctx)
		change.apply()
		after := srv.IndexGeneration(ctx)
		require.NotEqual(t, component(before, change.index), component(after, change.index), change.name)
	}

	restarted := newFixtureServer(t, fixture, nil)
	require.NotEqual(t, component(srv.IndexGeneration(ctx), 0), component(restarted.IndexGeneration(ctx), 0), "a server restart changes the epoch")
}

// Events and the status API carry the generation, and an event published after
// a write carries a generation that includes it (SPEC-0136 R4).
func TestEventsAndStatusCarryTheIndexGeneration(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()
	events, unsubscribe := srv.globalEvents.Subscribe()
	defer unsubscribe()

	before := srv.IndexGeneration(ctx)
	require.Equal(t, before, srv.status(ctx).Generation)
	_, err := fixture.intelStore.SetValidationRunning(ctx)
	require.NoError(t, err)
	srv.NotifyGlobalEvent("index.changed", nil)
	event := <-events
	require.NotEqual(t, before, event.Generation)
	require.Equal(t, srv.IndexGeneration(ctx), event.Generation)
	require.Equal(t, event.Generation, srv.status(ctx).Generation)

	capabilities, err := srv.publicCapabilities(ctx)
	require.NoError(t, err)
	require.Equal(t, event.Generation, capabilities.Index.Generation)
}

// While the views poller runs, the views component is the poller's latest
// stamp instead of a walk of every views folder, and a views.changed event
// carries a generation that includes the change it announces.
func TestViewsGenerationUsesThePollerStamp(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	walked := srv.viewsGeneration()
	stamp := "views=fixed\n"
	srv.viewWatch.stamp.Store(&stamp)
	require.NotEqual(t, walked, srv.viewsGeneration(), "the stamp is used, not a walk")
	srv.viewWatch.stamp.Store(nil)
	require.Equal(t, walked, srv.viewsGeneration())

	events, unsubscribe := srv.globalEvents.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(t.Context())
	done := srv.pollViewFolders(ctx, 10*time.Millisecond)
	defer func() {
		cancel()
		<-done
	}()
	require.Equal(t, walked, srv.viewsGeneration(), "the poller's stamp matches a walk")
	dir := filepath.Join(fixture.root, ".rhizome", "views", "site")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "page.yaml"), []byte(customView("site.page", "{kind: standalone}", "page.tsx")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "page.tsx"), []byte("export default function Page() { return null }\n"), 0o644))
	timeout := time.After(10 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Kind != "views.changed" {
				continue
			}
			views := strings.Split(event.Generation, ".")[3]
			require.NotEqual(t, walked, views, "the event's generation includes the change")
			require.Equal(t, views, srv.viewsGeneration())
			return
		case <-timeout:
			t.Fatal("no views.changed event")
		}
	}
}

// Once the last event-stream listener releases, the views component walks
// the folders again, so a definition edit with no stream connected still
// moves the generation.
func TestViewsGenerationMovesAfterTheLastWatchReleases(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	release := srv.watchViewFolders()
	before := srv.viewsGeneration()
	release()

	dir := filepath.Join(fixture.root, ".rhizome", "views")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "specs.yaml"), []byte("apiVersion: rhizome.view.v1\nid: specs\nname: Specs\nsource: {kind: ontology_type, type: Spec}\n"), 0o644))
	require.NotEqual(t, before, srv.viewsGeneration())
}
