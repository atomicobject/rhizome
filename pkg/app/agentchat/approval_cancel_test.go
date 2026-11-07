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
