package agentchat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

// An approval left outstanding when its turn ends, by any route, must be
// persisted as cancelled so the browser disables its controls.
func TestServiceCancelsApprovalsOutstandingWhenTheTurnEnds(t *testing.T) {
	ends := map[string]func(f *fakeSession){
		"turn completed": func(f *fakeSession) {
			f.events <- harness.Event{Kind: harness.EventTurnCompleted}
			f.turnResults <- nil
		},
		"turn failed": func(f *fakeSession) {
			f.turnResults <- errors.New("turn failed")
		},
		"stream closed": func(f *fakeSession) {
			close(f.events)
			f.turnResults <- harness.Phase(harness.ErrTransportClosed, errors.New("transport EOF"))
		},
	}
	for name, end := range ends {
		t.Run(name, func(t *testing.T) {
			service, session, fake := newRunningService(t)
			require.NoError(t, <-session.sendResult)
			stream, unsubscribe := service.Subscribe(session.ID)
			t.Cleanup(unsubscribe)
			fake.session.events <- harness.Event{Kind: harness.EventApprovalRequested, RequestID: "approval-1", Command: "make check"}
			require.Equal(t, string(harness.EventApprovalRequested), (<-stream).Type)

			end(fake.session)
			require.Eventually(t, func() bool {
				persisted, err := service.EventsAfter(context.Background(), session.ID, 0)
				require.NoError(t, err)
				for _, event := range persisted {
					if event.Type == string(harness.EventApprovalRequested) && event.Result.RequestID == "approval-1" && event.Result.Phase == harness.PhaseCancelled {
						return true
					}
				}
				return false
			}, time.Second, 10*time.Millisecond)
			require.ErrorIs(t, service.RespondToApproval(session.ID, "approval-1", harness.DecisionAllow), ErrNotFound)
		})
	}
}

func persistedApproval(t *testing.T, service *Service, sessionID, requestID string) (decided, cancelled bool) {
	t.Helper()
	persisted, err := service.EventsAfter(context.Background(), sessionID, 0)
	require.NoError(t, err)
	for _, event := range persisted {
		if event.Result == nil || event.Result.RequestID != requestID {
			continue
		}
		decided = decided || event.Type == "approval-decision"
		cancelled = cancelled || (event.Type == string(harness.EventApprovalRequested) && event.Result.Phase == harness.PhaseCancelled)
	}
	return decided, cancelled
}

// An approval the harness had buffered when SendTurn failed reaches the
// drainer after its turn was cleared; it must still be recorded as cancelled.
func TestServiceCancelsApprovalArrivingAfterItsTurnEnded(t *testing.T) {
	service, session, fake := newRunningService(t)
	require.NoError(t, <-session.sendResult)
	fake.session.turnResults <- errors.New("turn failed")
	require.Eventually(t, func() bool {
		persisted, err := service.EventsAfter(context.Background(), session.ID, 0)
		require.NoError(t, err)
		return len(persisted) > 0 && persisted[len(persisted)-1].Type == string(harness.EventError)
	}, time.Second, 10*time.Millisecond)

	fake.session.events <- harness.Event{Kind: harness.EventApprovalRequested, RequestID: "late", Command: "make check"}
	require.Eventually(t, func() bool {
		_, cancelled := persistedApproval(t, service, session.ID, "late")
		return cancelled
	}, time.Second, 10*time.Millisecond)
	require.ErrorIs(t, service.RespondToApproval(session.ID, "late", harness.DecisionAllow), ErrNotFound)
}

// A decision already being sent to the harness when the turn ends is the
// approval's outcome; the turn end must not also record it as cancelled.
func TestServiceDoesNotCancelAnApprovalBeingAnswered(t *testing.T) {
	service, session, fake := newRunningService(t)
	require.NoError(t, <-session.sendResult)
	stream, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)
	fake.session.events <- harness.Event{Kind: harness.EventApprovalRequested, RequestID: "approval-1", Command: "make check"}
	require.Equal(t, string(harness.EventApprovalRequested), (<-stream).Type)

	entered, release := make(chan struct{}), make(chan struct{})
	fake.session.respondHook = func() { close(entered); <-release }
	responded := make(chan error, 1)
	go func() { responded <- service.RespondToApproval(session.ID, "approval-1", harness.DecisionAllow) }()
	<-entered

	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted}
	for event := range stream {
		if event.Type == string(harness.EventTurnCompleted) {
			break
		}
	}
	close(release)
	require.NoError(t, <-responded)
	decided, cancelled := persistedApproval(t, service, session.ID, "approval-1")
	require.True(t, decided)
	require.False(t, cancelled, "an answered approval must not also be recorded as cancelled")
}
