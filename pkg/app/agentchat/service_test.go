package agentchat

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"

	"github.com/stretchr/testify/require"
)

type fakeHarness struct {
	status      harness.Status
	statusError error
	statusCalls atomic.Int32
	startError  error
	session     *fakeSession
	starts      chan harness.SessionOptions
}

func newFakeHarness() *fakeHarness {
	return &fakeHarness{
		status:  harness.Status{Installed: true, LoggedIn: true, Models: []harness.ModelOption{{ID: "gpt-test", DisplayName: "gpt-test", Efforts: []string{"high"}}}},
		session: newFakeSession(),
		starts:  make(chan harness.SessionOptions, 4),
	}
}

func (f *fakeHarness) Status(context.Context) (harness.Status, error) {
	f.statusCalls.Add(1)
	return f.status, f.statusError
}
func (f *fakeHarness) StartSession(_ context.Context, options harness.SessionOptions) (harness.Session, error) {
	f.starts <- options
	if f.startError != nil {
		return nil, f.startError
	}
	return f.session, nil
}
func (f *fakeHarness) Generate(context.Context, harness.GenerateRequest) (harness.GenerateResult, error) {
	return harness.GenerateResult{}, nil
}

type approvalResponse struct {
	requestID string
	decision  harness.Decision
}

type fakeSession struct {
	SessionID   string
	events      chan harness.Event
	turns       chan string
	models      chan [2]string
	turnResults chan error
	interrupts  chan struct{}
	responses   chan approvalResponse
	stopped     chan struct{}
	stopOnce    sync.Once
	// modelGate, when set, holds SetModel until it closes or ctx ends.
	modelGate chan struct{}
	// respondHook, when set, runs before Respond records the decision.
	respondHook func()
}

func (f *fakeSession) ID() string { return f.SessionID }

func newFakeSession() *fakeSession {
	return &fakeSession{
		events: make(chan harness.Event, 32), turns: make(chan string, 4), models: make(chan [2]string, 8), turnResults: make(chan error, 4),
		interrupts: make(chan struct{}, 4), responses: make(chan approvalResponse, 4), stopped: make(chan struct{}),
	}
}

func (f *fakeSession) SendTurn(ctx context.Context, prompt string) error {
	f.turns <- prompt
	select {
	case err := <-f.turnResults:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-f.stopped:
		return errors.New("stopped")
	}
}
func (f *fakeSession) SetModel(ctx context.Context, model, effort string) error {
	f.models <- [2]string{model, effort}
	if f.modelGate == nil {
		return nil
	}
	select {
	case <-f.modelGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *fakeSession) Interrupt(context.Context) error { f.interrupts <- struct{}{}; return nil }
func (f *fakeSession) Respond(requestID string, decision harness.Decision) error {
	if f.respondHook != nil {
		f.respondHook()
	}
	f.responses <- approvalResponse{requestID: requestID, decision: decision}
	return nil
}
func (f *fakeSession) Events() <-chan harness.Event { return f.events }
func (f *fakeSession) Stop() error {
	f.stopOnce.Do(func() { close(f.stopped); close(f.events) })
	return nil
}

func TestServicePersistsHarnessTimelineAndResumes(t *testing.T) {
	env := setTestSettings(t, Settings{Harness: harness.KindCodex, Harnesses: map[harness.Kind]HarnessSettings{
		harness.KindCodex: {Model: "gpt-test", Effort: "high", PermissionMode: harness.PermissionApprovalRequired},
	}})
	ctx := vaultconfig.WithEnv(context.Background(), env)
	vaultPath := t.TempDir()
	firstHarness := newFakeHarness()
	service, err := NewService(ctx, vaultPath, map[harness.Kind]harness.Harness{harness.KindCodex: firstHarness})
	require.NoError(t, err)
	created, err := service.CreateSession(ctx, "Harness chat")
	require.NoError(t, err)
	stream, unsubscribe := service.Subscribe(created.Session.ID)
	defer unsubscribe()

	result := make(chan error, 1)
	go func() {
		_, sendErr := service.SendMessage(ctx, created.Session.ID, "hello")
		result <- sendErr
	}()
	require.Equal(t, "hello", <-firstHarness.session.turns)
	options := <-firstHarness.starts
	require.Equal(t, vaultPath, options.Cwd)
	require.Equal(t, "gpt-test", options.Model)
	firstHarness.session.events <- harness.Event{Kind: harness.EventTurnStarted, SessionID: "thread-1", TurnID: "turn-1"}
	commandExit := 7
	firstHarness.session.events <- harness.Event{
		Kind: harness.EventCommandExecution, TurnID: "turn-1", ItemID: "item-1",
		Command: "go test", Phase: harness.PhaseFailed, Input: json.RawMessage(`{"command":"go test"}`),
		Output: json.RawMessage(`"failed"`), Truncated: true, ExitCode: &commandExit, Diff: "@@ change",
	}
	firstHarness.session.events <- harness.Event{Kind: harness.EventAssistantTextDelta, TurnID: "turn-1", Text: "hello back"}
	firstHarness.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"}
	firstHarness.session.turnResults <- nil
	require.NoError(t, <-result)
	var streamed *Event
	for streamed == nil {
		select {
		case event := <-stream:
			if event.Result != nil && event.Result.ItemID == "item-1" {
				streamed = &event
			}
		case <-time.After(5 * time.Second):
			t.Fatal("command event was not streamed")
		}
	}
	requireCommandTimelineDetails(t, *streamed)
	require.NoError(t, service.Close())

	secondHarness := newFakeHarness()
	reopened, err := NewService(ctx, vaultPath, map[harness.Kind]harness.Harness{harness.KindCodex: secondHarness})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	persisted, err := reopened.GetSession(ctx, created.Session.ID)
	require.NoError(t, err)
	require.Len(t, persisted.Messages, 2)
	require.Equal(t, []string{"hello", "hello back"}, []string{persisted.Messages[0].Content, persisted.Messages[1].Content})
	require.Len(t, persisted.Events, 4)
	requireCommandTimelineDetails(t, persisted.Events[1])

	secondResult := make(chan error, 1)
	go func() {
		_, sendErr := reopened.SendMessage(ctx, created.Session.ID, "again")
		secondResult <- sendErr
	}()
	require.Equal(t, "again", <-secondHarness.session.turns)
	require.Equal(t, "thread-1", (<-secondHarness.starts).Resume)
	secondHarness.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-2"}
	secondHarness.session.turnResults <- nil
	require.NoError(t, <-secondResult)
}

func TestServiceFailedResumeStoresReplacementIDAcrossRestarts(t *testing.T) {
	env := setTestSettings(t, Settings{Harness: harness.KindCodex})
	ctx := vaultconfig.WithEnv(context.Background(), env)
	vaultPath := t.TempDir()

	firstHarness := newFakeHarness()
	firstHarness.session.SessionID = "stale-thread"
	first, err := NewService(ctx, vaultPath, map[harness.Kind]harness.Harness{harness.KindCodex: firstHarness})
	require.NoError(t, err)
	created, err := first.CreateSession(ctx, "Resume replacement")
	require.NoError(t, err)
	_, err = first.SendMessage(ctx, created.Session.ID, "first")
	require.NoError(t, err)
	require.Empty(t, (<-firstHarness.starts).Resume)
	<-firstHarness.session.turns
	require.NoError(t, first.Close())

	secondHarness := newFakeHarness()
	secondHarness.session.SessionID = "replacement-thread"
	second, err := NewService(ctx, vaultPath, map[harness.Kind]harness.Harness{harness.KindCodex: secondHarness})
	require.NoError(t, err)
	_, err = second.SendMessage(ctx, created.Session.ID, "second")
	require.NoError(t, err)
	require.Equal(t, "stale-thread", (<-secondHarness.starts).Resume)
	<-secondHarness.session.turns
	persisted, err := second.GetSession(ctx, created.Session.ID)
	require.NoError(t, err)
	require.Equal(t, "replacement-thread", persisted.Session.HarnessSessionID)
	require.NoError(t, second.Close())

	thirdHarness := newFakeHarness()
	thirdHarness.session.SessionID = "replacement-thread"
	third, err := NewService(ctx, vaultPath, map[harness.Kind]harness.Harness{harness.KindCodex: thirdHarness})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, third.Close()) })
	_, err = third.SendMessage(ctx, created.Session.ID, "third")
	require.NoError(t, err)
	require.Equal(t, "replacement-thread", (<-thirdHarness.starts).Resume)
	<-thirdHarness.session.turns
}

func TestServiceCachesHarnessStatusAndCapturesErrors(t *testing.T) {
	env := setTestSettings(t, Settings{})
	fake := newFakeHarness()
	fake.statusError = errors.New("probe failed")
	service, err := NewService(vaultconfig.WithEnv(context.Background(), env), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })

	first, err := service.Settings(vaultconfig.WithEnv(context.Background(), env))
	require.NoError(t, err)
	second, err := service.Settings(vaultconfig.WithEnv(context.Background(), env))
	require.NoError(t, err)
	require.Equal(t, int32(1), fake.statusCalls.Load())
	require.Equal(t, "probe failed", first.Harnesses[0].Status.LastError)
	require.Equal(t, first.Harnesses, second.Harnesses)
}

func TestServicePersistsPhaseErrorWithLoginHint(t *testing.T) {
	env := setTestSettings(t, Settings{Harness: harness.KindCodex})
	fake := newFakeHarness()
	fake.status.LoginHint = "codex login"
	fake.startError = harness.Phase(harness.ErrInitialize, harness.ErrNotLoggedIn)
	service, err := NewService(vaultconfig.WithEnv(context.Background(), env), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(vaultconfig.WithEnv(context.Background(), env), "Login")
	require.NoError(t, err)

	_, err = service.SendMessage(vaultconfig.WithEnv(context.Background(), env), created.Session.ID, "hello")
	require.ErrorIs(t, err, harness.ErrNotLoggedIn)
	events, err := service.EventsAfter(vaultconfig.WithEnv(context.Background(), env), created.Session.ID, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, string(harness.EventError), events[0].Type)
	require.Equal(t, "initialize", events[0].Result.Status)
	require.Equal(t, "codex login", events[0].Result.Reason)
}

func TestServiceSendMessageReturnsWhileTurnContinuesAfterRequestCancellation(t *testing.T) {
	env := setTestSettings(t, Settings{Harness: harness.KindCodex})
	fake := newFakeHarness()
	fake.session.SessionID = "thread-async"
	service, err := NewService(vaultconfig.WithEnv(context.Background(), env), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(vaultconfig.WithEnv(context.Background(), env), "Async")
	require.NoError(t, err)

	requestCtx, cancelRequest := context.WithCancel(vaultconfig.WithEnv(context.Background(), env))
	response, err := service.SendMessage(requestCtx, created.Session.ID, "ping")
	require.NoError(t, err)
	require.True(t, response.Session.TurnRunning)
	require.Equal(t, "thread-async", response.Session.HarnessSessionID)
	cancelRequest()
	require.Equal(t, "ping", <-fake.session.turns)

	_, err = service.SendMessage(vaultconfig.WithEnv(context.Background(), env), created.Session.ID, "second")
	require.ErrorIs(t, err, ErrConflict)
	fake.session.events <- harness.Event{Kind: harness.EventTurnStarted, TurnID: "turn-async"}
	// A progress note and the answer are separate message items, so they
	// become separate paragraphs; one item's deltas join as they stream.
	for _, delta := range []harness.Event{
		{ItemID: "note", Text: "Checking."},
		{ItemID: "answer", Text: "po"},
		{ItemID: "answer", Text: "ng"},
	} {
		delta.Kind, delta.TurnID = harness.EventAssistantTextDelta, "turn-async"
		fake.session.events <- delta
	}
	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-async"}
	fake.session.turnResults <- nil
	require.Eventually(t, func() bool {
		persisted, getErr := service.GetSession(vaultconfig.WithEnv(context.Background(), env), created.Session.ID)
		return getErr == nil && !persisted.Session.TurnRunning && len(persisted.Messages) == 2 && persisted.Messages[1].Content == "Checking.\n\npong"
	}, time.Second, 10*time.Millisecond)
}

func TestServiceStreamsDiagnosticsWithoutPersistingThem(t *testing.T) {
	service, session, fake := newRunningService(t)
	stream, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)
	fake.session.events <- harness.Event{Kind: harness.EventDiagnostic, Text: "unknown vendor message"}
	streamed := <-stream
	require.Equal(t, string(harness.EventDiagnostic), streamed.Type)
	require.Negative(t, streamed.ID)
	require.False(t, streamed.CreatedAt.IsZero())

	persisted, err := service.EventsAfter(context.Background(), session.ID, 0)
	require.NoError(t, err)
	require.NotContains(t, eventTypes(persisted), string(harness.EventDiagnostic))
	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted}
	fake.session.turnResults <- nil
	require.NoError(t, <-session.sendResult)
}

func TestServiceCompletionSerializesAssistantBeforeNextUserMessage(t *testing.T) {
	service, session, fake := newRunningService(t)
	stream, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)
	fake.session.events <- harness.Event{Kind: harness.EventAssistantTextDelta, TurnID: "turn-1", Text: "first answer"}
	require.Equal(t, "message_delta", (<-stream).Type)
	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"}
	require.Equal(t, string(harness.EventTurnCompleted), (<-stream).Type)

	_, err := service.SendMessage(context.Background(), session.ID, "second question")
	require.NoError(t, err)
	require.Equal(t, "second question", <-fake.session.turns)
	persisted, err := service.GetSession(context.Background(), session.ID)
	require.NoError(t, err)
	require.Len(t, persisted.Messages, 3)
	require.Equal(t, []string{"work", "first answer", "second question"}, []string{
		persisted.Messages[0].Content,
		persisted.Messages[1].Content,
		persisted.Messages[2].Content,
	})
}

// requireCommandTimelineDetails checks the command event the service projects from a harness command item.
func requireCommandTimelineDetails(t *testing.T, event Event) {
	t.Helper()
	require.NotNil(t, event.Result)
	require.Regexp(t, `^[0-9a-f]+:turn-1$`, event.Result.TurnID, "the harness turn id under its process generation")
	require.Equal(t, "item-1", event.Result.ItemID)
	require.Equal(t, "go test", event.Result.Command)
	require.Equal(t, harness.PhaseFailed, event.Result.Phase)
	require.JSONEq(t, `{"command":"go test"}`, string(event.Result.Input))
	require.JSONEq(t, `"failed"`, string(event.Result.Output))
	require.True(t, event.Result.Truncated)
	require.NotNil(t, event.Result.ExitCode)
	require.Equal(t, 7, *event.Result.ExitCode)
	require.Equal(t, "@@ change", event.Result.Diff)
}
func TestServiceApprovalAndInterruptRoundTrips(t *testing.T) {
	service, session, fake := newRunningService(t)
	events, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)
	fake.session.events <- harness.Event{Kind: harness.EventApprovalRequested, RequestID: "approval-1", Command: "make check", Reason: "run tests", AllowForSession: true}
	require.Equal(t, string(harness.EventApprovalRequested), (<-events).Type)

	require.NoError(t, service.RespondToApproval(session.ID, "approval-1", harness.DecisionAllowForSession))
	response := <-fake.session.responses
	require.Equal(t, "approval-1", response.requestID)
	require.Equal(t, harness.DecisionAllowForSession, response.decision)
	require.NoError(t, service.Interrupt(context.Background(), session.ID))
	<-fake.session.interrupts

	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"}
	fake.session.turnResults <- nil
	require.NoError(t, <-session.sendResult)
	persisted, err := service.EventsAfter(context.Background(), session.ID, 0)
	require.NoError(t, err)
	require.Contains(t, eventTypes(persisted), "approval-decision")
	require.Contains(t, eventTypes(persisted), "interrupt-requested")
	var approval *Event
	for i := range persisted {
		if persisted[i].Type == string(harness.EventApprovalRequested) {
			approval = &persisted[i]
			break
		}
	}
	require.NotNil(t, approval)
	require.True(t, approval.Result.AllowForSession)
}

func TestServiceStopDuringModelChangeEndsTurnWithoutSending(t *testing.T) {
	env := setTestSettings(t, Settings{Harness: harness.KindCodex})
	ctx := vaultconfig.WithEnv(context.Background(), env)
	fake := newFakeHarness()
	fake.session.modelGate = make(chan struct{})
	service, err := NewService(ctx, t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(ctx, "Stop while changing model")
	require.NoError(t, err)
	sessionID := created.Session.ID
	events, unsubscribe := service.Subscribe(sessionID)
	t.Cleanup(unsubscribe)
	_, err = service.SendMessage(ctx, sessionID, "work")
	require.NoError(t, err)
	<-fake.session.models

	require.NoError(t, service.Interrupt(ctx, sessionID))

	deadline := time.After(time.Second)
	for completed := false; !completed; {
		select {
		case event := <-events:
			completed = event.Type == string(harness.EventTurnCompleted)
		case <-deadline:
			t.Fatal("Stop did not end the turn waiting on the model change")
		}
	}
	require.False(t, service.turnRunning(sessionID))
	persisted, err := service.EventsAfter(ctx, sessionID, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"interrupt-requested", string(harness.EventTurnCompleted)}, eventTypes(persisted))
	require.Equal(t, "interrupted", persisted[1].Result.Status)
	require.NotEmpty(t, persisted[1].Result.TurnID, "a stopped turn has its own id, so a client does not gather earlier turns' replies into it")
	require.Equal(t, persisted[0].Result.TurnID, persisted[1].Result.TurnID)
	require.Empty(t, fake.session.turns, "no harness turn starts")
	require.Empty(t, fake.session.interrupts, "the harness has no turn to interrupt")
}

func TestServiceRetryingErrorKeepsTurnRunningUntilCompletion(t *testing.T) {
	service, session, fake := newRunningService(t)
	fake.session.events <- harness.Event{Kind: harness.EventError, Status: "retrying", Text: "temporary failure"}
	require.Eventually(t, func() bool {
		persisted, err := service.GetSession(context.Background(), session.ID)
		if err != nil || !persisted.Session.TurnRunning {
			return false
		}
		for _, event := range persisted.Events {
			if event.Type == string(harness.EventError) && event.Result.Status == "retrying" {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"}
	fake.session.turnResults <- nil
	require.NoError(t, <-session.sendResult)
	require.Eventually(t, func() bool {
		persisted, err := service.GetSession(context.Background(), session.ID)
		return err == nil && !persisted.Session.TurnRunning
	}, time.Second, 10*time.Millisecond)
}

type runningSession struct {
	Session
	sendResult chan error
}

func newRunningService(t *testing.T) (*Service, runningSession, *fakeHarness) {
	t.Helper()
	env := setTestSettings(t, Settings{Harness: harness.KindCodex})
	fake := newFakeHarness()
	service, err := NewService(vaultconfig.WithEnv(context.Background(), env), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(vaultconfig.WithEnv(context.Background(), env), "Running")
	require.NoError(t, err)
	session := runningSession{Session: created.Session, sendResult: make(chan error, 1)}
	go func() {
		_, sendErr := service.SendMessage(vaultconfig.WithEnv(context.Background(), env), session.ID, "work")
		session.sendResult <- sendErr
	}()
	<-fake.session.turns
	<-fake.starts
	return service, session, fake
}

func TestServiceRejectsConcurrentAndPreHarnessTurns(t *testing.T) {
	service, session, fake := newRunningService(t)
	_, err := service.SendMessage(context.Background(), session.ID, "second")
	require.ErrorIs(t, err, ErrConflict)

	now := formatTime(time.Now())
	_, err = service.store.(*Store).db.Exec(`INSERT INTO agent_sessions (id, title, created_at, updated_at) VALUES ('old', 'Old', ?, ?)`, now, now)
	require.NoError(t, err)
	_, err = service.SendMessage(context.Background(), "old", "new turn")
	require.ErrorIs(t, err, ErrConflict)

	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted}
	fake.session.turnResults <- nil
	require.NoError(t, <-session.sendResult)
}

func eventTypes(events []Event) []string {
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, event.Type)
	}
	return result
}

func setTestSettings(t *testing.T, settings Settings) vaultconfig.Env {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	t.Helper()
	root := filepath.Join(t.TempDir(), "config")
	file := filepath.Join(root, "config.yml")
	env.CLIConfigFile = file
	_, err := SaveChoice(env, settings)
	require.NoError(t, err)
	return env
}

// A vendor-side cancellation must clear the pending approval so a late
// decision is rejected as not found instead of forwarded to a dead request.
func TestServiceCancelledApprovalClearsPending(t *testing.T) {
	service, session, fake := newRunningService(t)
	events, unsubscribe := service.Subscribe(session.ID)
	t.Cleanup(unsubscribe)
	fake.session.events <- harness.Event{Kind: harness.EventApprovalRequested, RequestID: "approval-1", Command: "make check"}
	require.Equal(t, string(harness.EventApprovalRequested), (<-events).Type)
	fake.session.events <- harness.Event{Kind: harness.EventApprovalRequested, Phase: harness.PhaseCancelled, RequestID: "approval-1", Reason: "cancelled"}
	cancelled := <-events
	require.Equal(t, harness.PhaseCancelled, cancelled.Result.Phase)

	require.ErrorIs(t, service.RespondToApproval(session.ID, "approval-1", harness.DecisionAllow), ErrNotFound)

	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"}
	fake.session.turnResults <- nil
	require.NoError(t, <-session.sendResult)
}

// sendAndFinish sends one message and completes its turn.
func sendAndFinish(t *testing.T, ctx context.Context, service *Service, fake *fakeHarness, sessionID, content string) {
	t.Helper()
	_, err := service.SendMessage(ctx, sessionID, content)
	require.NoError(t, err)
	require.Equal(t, content, <-fake.session.turns)
	// Each process numbers its turns from one, as Claude Code does.
	fake.session.events <- harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"}
	fake.session.turnResults <- nil
	require.Eventually(t, func() bool { return !service.turnRunning(sessionID) }, 5*time.Second, time.Millisecond)
}

func TestServiceAppliesTheChosenModelToAnOpenSessionFromItsNextTurn(t *testing.T) {
	saved := func(model, effort string) Settings {
		return Settings{Harness: harness.KindCodex, Harnesses: map[harness.Kind]HarnessSettings{harness.KindCodex: {Model: model, Effort: effort}}}
	}
	env := setTestSettings(t, saved("", ""))
	ctx := vaultconfig.WithEnv(context.Background(), env)
	fake := newFakeHarness()
	fake.status.Models = []harness.ModelOption{
		{ID: "gpt-test", Efforts: []string{"low", "high"}, DefaultEffort: "low", Default: true},
		{ID: "gpt-other", Efforts: []string{"medium"}, DefaultEffort: "medium"},
	}
	service, err := NewService(ctx, t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(ctx, "Switch models")
	require.NoError(t, err)

	sendAndFinish(t, ctx, service, fake, created.Session.ID, "first")
	options := <-fake.starts
	require.Equal(t, [2]string{"gpt-test", "low"}, [2]string{options.Model, options.Effort}, "nothing chosen runs the default model at its default effort")
	require.Equal(t, [2]string{"gpt-test", "low"}, <-fake.session.models)

	_, err = SaveChoice(env, saved("gpt-test", "high"))
	require.NoError(t, err)
	sendAndFinish(t, ctx, service, fake, created.Session.ID, "second")
	require.Equal(t, [2]string{"gpt-test", "high"}, <-fake.session.models)

	_, err = SaveChoice(env, saved("gpt-other", "high"))
	require.NoError(t, err)
	sendAndFinish(t, ctx, service, fake, created.Session.ID, "third")
	require.Equal(t, [2]string{"gpt-other", "medium"}, <-fake.session.models, "an effort the new model lacks becomes its default")
	require.Empty(t, fake.starts, "the open process switched models without restarting")
}

func TestServiceResumesAnOpenSessionInANewProcessWhenThePermissionModeChanges(t *testing.T) {
	mode := func(permission harness.PermissionMode) Settings {
		return Settings{Harness: harness.KindCodex, Harnesses: map[harness.Kind]HarnessSettings{harness.KindCodex: {PermissionMode: permission}}}
	}
	env := setTestSettings(t, mode(harness.PermissionAuto))
	ctx := vaultconfig.WithEnv(context.Background(), env)
	fake := newFakeHarness()
	fake.session.SessionID = "thread-1"
	service, err := NewService(ctx, t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(ctx, "Change mode")
	require.NoError(t, err)
	sendAndFinish(t, ctx, service, fake, created.Session.ID, "first")
	require.Equal(t, harness.PermissionAuto, (<-fake.starts).PermissionMode)

	_, err = SaveChoice(env, mode(harness.PermissionApprovalRequired))
	require.NoError(t, err)
	first := fake.session
	fake.session = newFakeSession()
	sendAndFinish(t, ctx, service, fake, created.Session.ID, "second")
	restarted := <-fake.starts
	require.Equal(t, harness.PermissionApprovalRequired, restarted.PermissionMode)
	require.Equal(t, "thread-1", restarted.Resume, "the new process continues the conversation")
	select {
	case <-first.stopped:
	default:
		t.Fatal("the old process was not stopped")
	}
	events, err := service.EventsAfter(ctx, created.Session.ID, 0)
	require.NoError(t, err)
	var turnIDs []string
	for _, event := range events {
		if event.Type == string(harness.EventTurnCompleted) {
			turnIDs = append(turnIDs, event.Result.TurnID)
		}
	}
	require.Len(t, turnIDs, 2)
	require.NotEqual(t, turnIDs[0], turnIDs[1], "each process's turn-1 is its own turn, so the browser does not merge them")
}

func TestServiceRefusesATurnForAnAgentTurnedOffInSettings(t *testing.T) {
	env := setTestSettings(t, Settings{Harness: harness.KindCodex})
	ctx := vaultconfig.WithEnv(context.Background(), env)
	fake := newFakeHarness()
	service, err := NewService(ctx, t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(ctx, "Turned off")
	require.NoError(t, err)
	sendAndFinish(t, ctx, service, fake, created.Session.ID, "first")

	_, err = SetHarnessDisabled(env, harness.KindCodex, true)
	require.NoError(t, err)
	_, err = service.SendMessage(ctx, created.Session.ID, "second")
	require.ErrorIs(t, err, ErrConflict)
	require.EqualError(t, err, "Codex is turned off in Settings.")

	require.Empty(t, fake.session.turns, "the open process ran no turn")
	thread, err := service.GetSession(ctx, created.Session.ID)
	require.NoError(t, err)
	require.Len(t, thread.Messages, 1, "the refused message was not stored")
	require.False(t, thread.Session.TurnRunning)
}
