package agentchat

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

type sessionQueueHarness struct {
	mu       sync.Mutex
	sessions []*fakeSession
	starts   int
}

func (f *sessionQueueHarness) Status(context.Context) (harness.Status, error) {
	return harness.Status{Installed: true, LoggedIn: true}, nil
}

func (f *sessionQueueHarness) StartSession(context.Context, harness.SessionOptions) (harness.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts >= len(f.sessions) {
		return nil, errors.New("no fake session available")
	}
	session := f.sessions[f.starts]
	f.starts++
	return session, nil
}

func (*sessionQueueHarness) Generate(context.Context, harness.GenerateRequest) (json.RawMessage, error) {
	return nil, nil
}

func (f *sessionQueueHarness) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

func TestServiceEvictsClosedHarnessSessionAndStartsFresh(t *testing.T) {
	setTestSettings(t, Settings{Harness: harness.KindCodex})
	first, second := newFakeSession(), newFakeSession()
	driver := &sessionQueueHarness{sessions: []*fakeSession{first, second}}
	service, err := NewService(context.Background(), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: driver})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(context.Background(), "Recover")
	require.NoError(t, err)

	_, err = service.SendMessage(context.Background(), created.Session.ID, "first")
	require.NoError(t, err)
	require.Equal(t, "first", <-first.turns)
	close(first.events)
	require.Eventually(t, func() bool {
		response, getErr := service.GetSession(context.Background(), created.Session.ID)
		return getErr == nil && !response.Session.TurnRunning && len(response.Events) > 0 && eventTypes(response.Events)[len(response.Events)-1] == string(harness.EventError)
	}, time.Second, 10*time.Millisecond)
	first.turnResults <- harness.Phase(harness.ErrTransportClosed, errors.New("transport EOF"))

	_, err = service.SendMessage(context.Background(), created.Session.ID, "second")
	require.NoError(t, err)
	require.Equal(t, "second", <-second.turns)
	require.Equal(t, 2, driver.startCount())
	second.events <- harness.Event{Kind: harness.EventTurnCompleted}
	second.turnResults <- nil
}

type blockingStartHarness struct {
	started chan struct{}
	once    sync.Once
}

func (*blockingStartHarness) Status(context.Context) (harness.Status, error) {
	return harness.Status{Installed: true, LoggedIn: true}, nil
}

func (f *blockingStartHarness) StartSession(ctx context.Context, _ harness.SessionOptions) (harness.Session, error) {
	f.once.Do(func() { close(f.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*blockingStartHarness) Generate(context.Context, harness.GenerateRequest) (json.RawMessage, error) {
	return nil, nil
}

func TestServiceStartupDoesNotHoldMutexAndCloseCancelsIt(t *testing.T) {
	setTestSettings(t, Settings{Harness: harness.KindCodex})
	driver := &blockingStartHarness{started: make(chan struct{})}
	service, err := NewService(context.Background(), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: driver})
	require.NoError(t, err)
	created, err := service.CreateSession(context.Background(), "Blocked startup")
	require.NoError(t, err)

	sendDone := make(chan error, 1)
	go func() {
		_, sendErr := service.SendMessage(context.Background(), created.Session.ID, "hello")
		sendDone <- sendErr
	}()
	<-driver.started

	settingsCtx, cancelSettings := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelSettings()
	_, err = service.Settings(settingsCtx)
	require.NoError(t, err)
	_, err = service.ListSessions(settingsCtx)
	require.NoError(t, err)
	_, err = service.SendMessage(settingsCtx, created.Session.ID, "concurrent")
	require.ErrorIs(t, err, ErrConflict)

	closed := make(chan error, 1)
	go func() { closed <- service.Close() }()
	select {
	case err = <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close blocked on session startup")
	}
	require.Error(t, <-sendDone)
}
