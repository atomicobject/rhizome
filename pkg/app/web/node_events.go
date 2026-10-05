package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

type nodeEventSubscription struct {
	id   string
	refs []nodeEventRefState
	ch   chan ontology.NodeEvent
}

// nodeEventRefState tracks one subscribed node. Ref is exactly what the client
// sent, so events echo an identity it recognizes; Resolved and Version are the
// projection last announced for it.
type nodeEventRefState struct {
	Ref      ontology.NodeRef
	Resolved ontology.NodeRef
	Version  string
}

type nodeEventBroker struct {
	mu     sync.RWMutex
	subs   map[string]*nodeEventSubscription
	closed bool
	seq    atomic.Uint64
}

func newNodeEventBroker() *nodeEventBroker {
	return &nodeEventBroker{
		subs: map[string]*nodeEventSubscription{},
	}
}

func (b *nodeEventBroker) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, sub := range b.subs {
		close(sub.ch)
		delete(b.subs, id)
	}
}

func (b *nodeEventBroker) Subscribe(refs []nodeEventRefState) (<-chan ontology.NodeEvent, func()) {
	if b == nil {
		return nil, func() {}
	}
	sub := &nodeEventSubscription{
		id:   fmt.Sprintf("sub-%d", b.seq.Add(1)),
		refs: append([]nodeEventRefState(nil), refs...),
		ch:   make(chan ontology.NodeEvent, 16),
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(sub.ch)
		return sub.ch, func() {}
	}
	b.subs[sub.id] = sub
	b.mu.Unlock()
	return sub.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		current, ok := b.subs[sub.id]
		if !ok {
			return
		}
		delete(b.subs, sub.id)
		close(current.ch)
	}
}

func (b *nodeEventBroker) Publish(event ontology.NodeEvent) {
	if b == nil {
		return
	}
	key := canonicalNodeRefKey(event.Ref)
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for _, sub := range b.subs {
		for _, ref := range sub.refs {
			if canonicalNodeRefKey(ref.Ref) != key {
				continue
			}
			enqueueNodeEvent(sub.ch, event)
			break
		}
	}
}

// Advance records the projection just announced for every subscription to
// ref, so an unchanged file is not announced again.
func (b *nodeEventBroker) Advance(ref, resolved ontology.NodeRef, version string) {
	if b == nil {
		return
	}
	key := canonicalNodeRefKey(ref)
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		for i := range sub.refs {
			if canonicalNodeRefKey(sub.refs[i].Ref) == key {
				sub.refs[i].Resolved = resolved
				sub.refs[i].Version = version
			}
		}
	}
}

func (b *nodeEventBroker) PublishStaleAll(cause, reason string) {
	if b == nil {
		return
	}
	changed := []string{}
	if reason != "" {
		changed = []string{reason}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for _, sub := range b.subs {
		for _, ref := range sub.refs {
			enqueueNodeEvent(sub.ch, ontology.NodeEvent{
				ID:      nextNodeEventID(&b.seq),
				Kind:    "node.stale",
				Ref:     ref.Ref,
				Cause:   cause,
				Changed: changed,
			})
		}
	}
}

func (b *nodeEventBroker) StatesForNotePath(notePath string) []nodeEventRefState {
	if b == nil {
		return nil
	}
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return nil
	}
	out := make([]nodeEventRefState, 0)
	seen := map[string]struct{}{}
	for _, sub := range b.subs {
		for _, ref := range sub.refs {
			if strings.TrimSpace(ref.Ref.NotePath) != notePath {
				continue
			}
			key := canonicalNodeRefKey(ref.Ref)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ref)
		}
	}
	return out
}

func (b *nodeEventBroker) PublishStaleExceptNotePath(notePath, cause, reason string) {
	if b == nil {
		return
	}
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return
	}
	changed := []string{}
	if reason != "" {
		changed = []string{reason}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	seen := map[string]struct{}{}
	for _, sub := range b.subs {
		for _, ref := range sub.refs {
			if strings.TrimSpace(ref.Ref.NotePath) == notePath {
				continue
			}
			key := canonicalNodeRefKey(ref.Ref)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			enqueueNodeEvent(sub.ch, ontology.NodeEvent{
				ID:      nextNodeEventID(&b.seq),
				Kind:    "node.stale",
				Ref:     ref.Ref,
				Cause:   cause,
				Changed: changed,
			})
		}
	}
}

func enqueueNodeEvent(ch chan ontology.NodeEvent, event ontology.NodeEvent) {
	select {
	case ch <- event:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- event:
		default:
		}
	}
}

func nextNodeEventID(seq *atomic.Uint64) string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), seq.Add(1))
}

func (s *Server) handleWatchHubEvents(_ context.Context, events []watchhub.WatchEvent) {
	if s == nil || s.nodeEvents == nil {
		return
	}
	defs, err := s.ontologyDefinitions()
	if err != nil || defs == nil || defs.schema == nil {
		return
	}
	nodeScope := s.nodeReadScope(context.Background(), defs)
	byPath := map[string]watchhub.WatchEvent{}
	for _, event := range events {
		notePath := strings.TrimSpace(event.RelPath)
		if notePath == "" {
			continue
		}
		existing, ok := byPath[notePath]
		if ok && (existing.Op.Has(watchhub.OpRemove) || existing.Op.Has(watchhub.OpRename)) {
			continue
		}
		byPath[notePath] = event
	}
	changedPaths := make([]string, 0, len(byPath))
	for notePath := range byPath {
		changedPaths = append(changedPaths, notePath)
	}
	if len(changedPaths) > 0 {
		events := make([]watchhub.WatchEvent, 0, len(byPath))
		for _, event := range byPath {
			events = append(events, event)
		}
		s.updateNotePathCacheForWatchEvents(context.Background(), events)
		s.invalidateNoteLinksCache()
	}
	s.invalidateNodeProjectionCacheForPaths(changedPaths)
	for notePath, event := range byPath {
		states := s.nodeEvents.StatesForNotePath(notePath)
		for _, state := range states {
			// Resolve from the last announced projection: its structural
			// fingerprint still finds an offset-addressed item after an edit
			// above it renumbers the subscribed locator.
			projection, projErr := s.resolveNodeProjection(context.Background(), defs, nodeScope, state.Resolved)
			if projErr != nil {
				kind := "node.stale"
				if event.Op.Has(watchhub.OpRemove) || event.Op.Has(watchhub.OpRename) {
					kind = "node.deleted"
				}
				s.nodeEvents.Publish(ontology.NodeEvent{
					ID:      nextNodeEventID(&s.nodeEvents.seq),
					Kind:    kind,
					Ref:     state.Ref,
					Cause:   "filesystem",
					Changed: []string{"content"},
				})
				continue
			}
			workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(defs.schema, projection)
			if workspace == nil || workspace.Version == state.Version {
				continue
			}
			// Embedded items without a block ID are addressed by byte offset, so
			// an edit above one renumbers it. Projection re-resolves the node by
			// its structural fingerprint; hand the subscriber that fresh ref so it
			// reloads with a locator that still resolves. A fingerprint change
			// alone is an ordinary content edit, not a new locator.
			var canonical *ontology.NodeRef
			if nodeLocatorKey(projection.Ref) != nodeLocatorKey(state.Resolved) {
				fresh := projection.Ref
				canonical = &fresh
			}
			s.nodeEvents.Publish(ontology.NodeEvent{
				ID:           nextNodeEventID(&s.nodeEvents.seq),
				Kind:         "node.updated",
				Ref:          state.Ref,
				CanonicalRef: canonical,
				Version:      workspace.Version,
				Cause:        "filesystem",
				Changed:      []string{"content"},
			})
			s.nodeEvents.Advance(state.Ref, projection.Ref, workspace.Version)
		}
		s.nodeEvents.PublishStaleExceptNotePath(notePath, "filesystem", "relations")
	}
}

func nodeLocatorKey(ref ontology.NodeRef) string {
	ref.Structural = ""
	return canonicalNodeRefKey(ref)
}

func (s *Server) handleNodeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	refs, err := s.resolveEventRefs(r.Context(), r.URL.Query())
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if len(refs) == 0 {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, errMissing("ref"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writePublicError(w, http.StatusInternalServerError, PublicErrorStreamingUnsupported, fmt.Errorf("streaming unsupported"))
		return
	}
	events, unsubscribe := s.nodeEvents.Subscribe(refs)
	defer unsubscribe()

	defer s.holdRuntime()()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": heartbeat %d\n\n", time.Now().Unix())
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "id: %s\n", event.ID)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *Server) resolveEventRefs(ctx context.Context, query url.Values) ([]nodeEventRefState, error) {
	rawRefs := query["ref"]
	if len(rawRefs) == 0 {
		return nil, nil
	}
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return nil, err
	}
	if defs == nil || defs.schema == nil {
		return nil, fmt.Errorf("ontology is unavailable")
	}
	seen := map[string]struct{}{}
	out := make([]nodeEventRefState, 0, len(rawRefs))
	nodeIDs := query["nodeId"]
	structurals := query["structural"]
	kinds := query["kind"]
	nodeScope := s.nodeReadScope(ctx, defs)
	for idx, raw := range rawRefs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		ref, err := nodeRefFromTarget(raw)
		if err != nil {
			return nil, err
		}
		if idx < len(nodeIDs) {
			ref.NodeID = strings.TrimSpace(nodeIDs[idx])
		}
		if idx < len(structurals) {
			ref.Structural = strings.TrimSpace(structurals[idx])
		}
		if idx < len(kinds) && strings.TrimSpace(kinds[idx]) != "" {
			ref.Kind = ontology.NodeKind(strings.TrimSpace(kinds[idx]))
		}
		projection, err := s.resolveNodeProjection(ctx, defs, nodeScope, ref)
		if err != nil {
			return nil, err
		}
		key := canonicalNodeRefKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		workspace := ontology.BuildNodeWorkspaceFromProjectionWithSchema(defs.schema, projection)
		out = append(out, nodeEventRefState{
			Ref:      ref,
			Resolved: projection.Ref,
			Version:  firstNonEmptyString(workspace.Version, projection.Ref.String()),
		})
	}
	return out, nil
}
