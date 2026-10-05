package web

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

const criterionLine = "- Story tied to validation checks."

// subscribeNodeEvents opens the SSE stream for one ref and returns a channel
// fed by every delivered event.
func subscribeNodeEvents(t *testing.T, baseURL, ref string) (chan ontology.NodeEvent, func()) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/nodes/events?ref="+url.QueryEscape(ref), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	events := make(chan ontology.NodeEvent, 8)
	go func() {
		for {
			event, ok := readSSEEvent(reader)
			if !ok {
				return
			}
			events <- event
		}
	}()
	return events, func() { _ = resp.Body.Close() }
}

func awaitNodeEvent(t *testing.T, events chan ontology.NodeEvent) ontology.NodeEvent {
	t.Helper()

	select {
	case event := <-events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ontology node event")
	}
	return ontology.NodeEvent{}
}

// TestOntologyNodeEventsFollowRenumberedEmbeddedItemRefs covers the pane-stale
// bug: a list-item node without a block ID is addressed by byte offset
// (`#item-<start>`), so an edit above it renumbers the node. The event stream
// must hand the browser the renumbered canonical ref, otherwise the pane
// reloads with a locator that no longer resolves.
func TestOntologyNodeEventsFollowRenumberedEmbeddedItemRefs(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	hub, err := watchhub.NewHub(fixture.root, watchhub.Options{DisableFSNotify: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Close() })
	runtime := &Runtime{}
	runtime.SetWatchHub(hub)
	srv := newFixtureServer(t, fixture, runtime)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	specPath := filepath.Join(fixture.root, "specs/100-demo/spec.md")
	original, err := os.ReadFile(specPath)
	require.NoError(t, err)
	start := strings.Index(string(original), criterionLine)
	require.GreaterOrEqual(t, start, 0)
	oldRef := fmt.Sprintf("specs/100-demo/spec.md#item-%d", start)

	events, closeStream := subscribeNodeEvents(t, httpSrv.URL, oldRef)
	defer closeStream()

	inserted := "Preface line inserted above the criterion.\n\n"
	updated := strings.Replace(string(original), "# Demo Spec\n", "# Demo Spec\n\n"+inserted, 1)
	require.NotEqual(t, string(original), updated)
	require.NoError(t, os.WriteFile(specPath, []byte(updated), 0o644))
	hub.EmitHintPaths([]string{"specs/100-demo/spec.md"})

	event := awaitNodeEvent(t, events)
	require.Equal(t, "node.updated", event.Kind)

	newStart := strings.Index(updated, criterionLine)
	require.Greater(t, newStart, start)
	require.NotNil(t, event.CanonicalRef, "renumbered item must publish its fresh canonical ref")
	require.Equal(t, fmt.Sprintf("item-%d", newStart), event.CanonicalRef.Fragment)
	require.Equal(t, fmt.Sprintf("item-%d", start), event.Ref.Fragment, "the event names the ref the client subscribed with")
	require.NotEmpty(t, event.CanonicalRef.Structural)

	// The published ref must be one the pane can reload from.
	ref, err := nodeRefFromTarget(fmt.Sprintf("specs/100-demo/spec.md#item-%d", newStart))
	require.NoError(t, err)
	workspace, err := srv.nodeWorkspace(t.Context(), ref, nodeWorkspaceIncludes{})
	require.NoError(t, err)
	require.Equal(t, "Criterion", workspace.Node.ResolvedType)
}

// TestOntologyNodeEventsPublishDeletedForRemovedNote keeps the deleted branch:
// when the host note is gone there is no fresh ref to follow.
func TestOntologyNodeEventsPublishDeletedForRemovedNote(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	hub, err := watchhub.NewHub(fixture.root, watchhub.Options{DisableFSNotify: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Close() })
	runtime := &Runtime{}
	runtime.SetWatchHub(hub)
	srv := newFixtureServer(t, fixture, runtime)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	events, closeStream := subscribeNodeEvents(t, httpSrv.URL, "specs/100-demo/spec.md#^validation")
	defer closeStream()

	require.NoError(t, os.Remove(filepath.Join(fixture.root, "specs/100-demo/spec.md")))
	hub.EmitHintPathHints([]watchhub.HintPath{{Path: "specs/100-demo/spec.md", Kind: "removed"}})

	event := awaitNodeEvent(t, events)
	require.Equal(t, "node.deleted", event.Kind)
	require.Equal(t, "^validation", event.Ref.Fragment)
	require.Nil(t, event.CanonicalRef)
}

// TestOntologyNodeEventsEchoSubscribedRefAndAdvanceVersion covers the live
// note bug: events named the server-resolved ref, whose structural
// fingerprint the browser never sent, and the stored version never advanced,
// so an unchanged file re-announced the same update on every watch event.
func TestOntologyNodeEventsEchoSubscribedRefAndAdvanceVersion(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	hub, err := watchhub.NewHub(fixture.root, watchhub.Options{DisableFSNotify: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Close() })
	runtime := &Runtime{}
	runtime.SetWatchHub(hub)
	srv := newFixtureServer(t, fixture, runtime)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	const notePath = "specs/100-demo/plan.md"
	subscribed, err := nodeRefFromTarget(notePath)
	require.NoError(t, err)
	events, closeStream := subscribeNodeEvents(t, httpSrv.URL, notePath)
	defer closeStream()

	abs := filepath.Join(fixture.root, notePath)
	original, err := os.ReadFile(abs)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(abs, append(original, []byte("\nFirst live edit.\n")...), 0o644))
	hub.EmitHintPaths([]string{notePath})

	first := awaitNodeEvent(t, events)
	require.Equal(t, "node.updated", first.Kind)
	require.Equal(t, subscribed, first.Ref, "the event must echo the subscribed ref")
	require.NotEmpty(t, first.Version)
	require.Nil(t, first.CanonicalRef, "a content edit keeps the note's locator")

	// Re-announcing an unchanged file must not repeat the update. Process both
	// announcements synchronously so hub coalescing cannot merge them.
	writeEvent := []watchhub.WatchEvent{{RelPath: notePath, AbsPath: abs, Op: watchhub.OpWrite, Root: fixture.root}}
	srv.handleWatchHubEvents(t.Context(), writeEvent)
	require.NoError(t, os.WriteFile(abs, append(original, []byte("\nSecond live edit.\n")...), 0o644))
	srv.handleWatchHubEvents(t.Context(), writeEvent)

	second := awaitNodeEvent(t, events)
	require.Equal(t, "node.updated", second.Kind)
	require.Equal(t, subscribed, second.Ref)
	require.NotEqual(t, first.Version, second.Version, "the stored version must advance after publishing")
}
