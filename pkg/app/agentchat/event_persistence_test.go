package agentchat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type failingEventStore struct {
	chatStore
	mu       sync.Mutex
	failures int
	attempts int
}

func (s *failingEventStore) AddEvent(ctx context.Context, event Event) (Event, error) {
	s.mu.Lock()
	s.attempts++
	if s.failures != 0 {
		if s.failures > 0 {
			s.failures--
		}
		s.mu.Unlock()
		return Event{}, sqlite3.Error{Code: sqlite3.ErrBusy}
	}
	s.mu.Unlock()
	return s.chatStore.AddEvent(ctx, event)
}

func (s *failingEventStore) attemptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

func TestServiceRetriesTransientEventPersistenceFailure(t *testing.T) {
	service, session, fake := newRunningService(t)
	require.NoError(t, <-session.sendResult)
	store := &failingEventStore{chatStore: service.store, failures: 1}
	service.store = store
	stream, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)

	fake.session.events <- harness.Event{Kind: harness.EventCommandExecution, Command: "go test"}
	require.Equal(t, string(harness.EventCommandExecution), (<-stream).Type)
	require.Equal(t, 2, store.attemptCount())
	persisted, err := service.EventsAfter(context.Background(), session.ID, 0)
	require.NoError(t, err)
	require.Contains(t, eventTypes(persisted), string(harness.EventCommandExecution))

	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted}
	fake.session.turnResults <- nil
}

func TestServiceDeniesApprovalWhenPersistenceFails(t *testing.T) {
	service, session, fake := newRunningService(t)
	require.NoError(t, <-session.sendResult)
	service.store = &failingEventStore{chatStore: service.store, failures: -1}
	stream, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)

	fake.session.events <- harness.Event{Kind: harness.EventApprovalRequested, RequestID: "approval-1"}
	response := <-fake.session.responses
	require.Equal(t, "approval-1", response.requestID)
	require.Equal(t, harness.DecisionDeny, response.decision)
	fallback := <-stream
	require.Equal(t, string(harness.EventError), fallback.Type)
	require.Equal(t, "persistence", fallback.Result.Status)
	require.ErrorIs(t, service.RespondToApproval(session.ID, "approval-1", harness.DecisionAllow), ErrNotFound)
}

func TestServicePublishesErrorAndFinalizesTurnWhenCompletionPersistenceFails(t *testing.T) {
	service, session, fake := newRunningService(t)
	require.NoError(t, <-session.sendResult)
	service.store = &failingEventStore{chatStore: service.store, failures: -1}
	stream, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)

	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"}
	fake.session.turnResults <- nil
	fallback := <-stream
	require.Equal(t, string(harness.EventError), fallback.Type)
	require.Equal(t, "persistence", fallback.Result.Status)
	require.Eventually(t, func() bool {
		persisted, err := service.GetSession(context.Background(), session.ID)
		return err == nil && !persisted.Session.TurnRunning
	}, time.Second, 10*time.Millisecond)
}

type signalingEventStore struct {
	chatStore
	added chan int64
}

func (s *signalingEventStore) AddEvent(ctx context.Context, event Event) (Event, error) {
	stored, err := s.chatStore.AddEvent(ctx, event)
	s.added <- stored.ID
	return stored, err
}

// The SSE cursor skips ids at or below the last one sent, so an event
// published after a higher id would never reach that stream.
func TestServicePublishesEventsInIDOrder(t *testing.T) {
	service, session, fake := newRunningService(t)
	require.NoError(t, <-session.sendResult)
	store := &signalingEventStore{chatStore: service.store, added: make(chan int64, 8)}
	service.store = store
	stream, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)
	persist := func(eventType string) {
		if err := service.persistAndPublish(context.Background(), Event{SessionID: session.ID, Type: eventType}); err != nil {
			t.Error(err)
		}
	}

	// Stall delivery after the first event has its id, then persist a second.
	service.broker.mu.Lock()
	go persist("first")
	first := <-store.added
	go persist("second")
	select {
	case second := <-store.added:
		service.broker.mu.Unlock()
		t.Fatalf("event %d got its id while event %d was still unpublished", second, first)
	case <-time.After(50 * time.Millisecond):
	}
	service.broker.mu.Unlock()

	require.Equal(t, first, (<-stream).ID)
	second := <-store.added
	require.Greater(t, second, first)
	require.Equal(t, second, (<-stream).ID)

	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted}
	fake.session.turnResults <- nil
}
