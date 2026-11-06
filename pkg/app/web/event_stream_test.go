package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestJSONEventStreamPreservesWireFrames(t *testing.T) {
	t.Run("named global event skips unencodable payload", func(t *testing.T) {
		events := make(chan GlobalEvent, 2)
		events <- GlobalEvent{ID: "bad", Kind: "bad", Data: func() {}}
		events <- GlobalEvent{ID: "g-1", Kind: "schema.invalidated", Data: map[string]string{"reason": "test"}}
		close(events)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
		streamJSONEvents(w, r, w, events, func(event GlobalEvent) (string, string) {
			return event.ID, event.Kind
		})
		require.Equal(t, ": connected\n\nid: g-1\nevent: schema.invalidated\ndata: {\"id\":\"g-1\",\"kind\":\"schema.invalidated\",\"data\":{\"reason\":\"test\"}}\n\n", w.Body.String())
		require.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
		require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
		require.Equal(t, "keep-alive", w.Header().Get("Connection"))
		require.True(t, w.Flushed)
	})
	t.Run("unnamed node event retains subscribed ref", func(t *testing.T) {
		events := make(chan ontology.NodeEvent, 1)
		events <- ontology.NodeEvent{ID: "n-1", Kind: "node.updated", Ref: ontology.NodeRef{NotePath: "notes/a.md", Fragment: "^block", Kind: ontology.NodeKindEmbedded}}
		close(events)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/events", nil)
		streamJSONEvents(w, r, w, events, func(event ontology.NodeEvent) (string, string) {
			return event.ID, ""
		})
		require.Equal(t, ": connected\n\nid: n-1\ndata: {\"id\":\"n-1\",\"kind\":\"node.updated\",\"ref\":{\"notePath\":\"notes/a.md\",\"fragment\":\"^block\",\"kind\":\"EMBEDDED\"}}\n\n", w.Body.String())
		require.True(t, w.Flushed)
	})
}

func TestJSONEventStreamReturnsOnCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	streamJSONEvents(w, r, w, make(chan GlobalEvent), func(event GlobalEvent) (string, string) {
		return event.ID, event.Kind
	})
	require.Equal(t, ": connected\n\n", w.Body.String())
	require.True(t, w.Flushed)
}
