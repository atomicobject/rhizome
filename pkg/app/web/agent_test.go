package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAgentSettingsEndpointUsesHarnessStatuses(t *testing.T) {
	root := filepath.Join(t.TempDir(), "config")
	file := filepath.Join(root, "config.yml")
	original := obsidian.CliConfigPath
	obsidian.CliConfigPath = func() (string, string, error) { return root, file, nil }
	t.Cleanup(func() { obsidian.CliConfigPath = original })

	fake := &harnesstest.Harness{StatusResult: harness.Status{
		Installed: true, LoggedIn: true, Version: "codex-test", Models: []harness.ModelOption{{ID: "gpt-test", DisplayName: "gpt-test", Efforts: []string{"high"}}},
	}}
	service, err := agentchat.NewService(context.Background(), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	server := &Server{agentService: service}

	get := httptest.NewRequest(http.MethodGet, "/api/agent/settings", nil)
	getResponse := httptest.NewRecorder()
	server.handleAgentSettings(getResponse, get)
	require.Equal(t, http.StatusOK, getResponse.Code)
	var response agentchat.SettingsResponse
	require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &response))
	require.Equal(t, harness.KindCodex, response.Resolved.Harness)
	require.Equal(t, "first available", response.Resolved.Reason)
	require.Equal(t, []harness.ModelOption{{ID: "gpt-test", DisplayName: "gpt-test", Efforts: []string{"high"}}}, response.Harnesses[0].Status.Models)

	body := []byte(`{"settings":{"harness":"codex","harnesses":{"codex":{"model":" gpt-test ","permissionMode":"invalid"}}}}`)
	put := httptest.NewRequest(http.MethodPut, "/api/agent/settings", bytes.NewReader(body))
	putResponse := httptest.NewRecorder()
	server.handleAgentSettings(putResponse, put)
	require.Equal(t, http.StatusOK, putResponse.Code)
	require.NoError(t, json.Unmarshal(putResponse.Body.Bytes(), &response))
	require.Equal(t, "configured", response.Resolved.Reason)
	require.Equal(t, "gpt-test", response.Settings.Harnesses[harness.KindCodex].Model)
	require.Equal(t, harness.PermissionApprovalRequired, response.Settings.Harnesses[harness.KindCodex].PermissionMode)
}

func TestParseAgentApprovalAndInterruptPaths(t *testing.T) {
	sessionID, action, err := parseAgentSessionPath("/api/agent/sessions/agt-1/approvals/request-1")
	require.NoError(t, err)
	require.Equal(t, "agt-1", sessionID)
	require.Equal(t, "approvals/request-1", action)

	sessionID, action, err = parseAgentSessionPath("/api/agent/sessions/agt-1/interrupt")
	require.NoError(t, err)
	require.Equal(t, "agt-1", sessionID)
	require.Equal(t, "interrupt", action)
}

func TestWriteAgentErrorUsesConflictStatus(t *testing.T) {
	response := httptest.NewRecorder()
	writeAgentError(response, agentchat.ErrConflict)
	require.Equal(t, http.StatusConflict, response.Code)
}

func TestAgentMessageEndpointReturnsAcceptedAndTurnSurvivesRequest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "config")
	file := filepath.Join(root, "config.yml")
	original := obsidian.CliConfigPath
	obsidian.CliConfigPath = func() (string, string, error) { return root, file, nil }
	t.Cleanup(func() { obsidian.CliConfigPath = original })
	_, err := agentchat.SaveSettings(agentchat.Settings{Harness: harness.KindCodex})
	require.NoError(t, err)

	fakeSession := harnesstest.NewSession()
	fakeSession.SessionID = "fake-thread"
	fake := &harnesstest.Harness{
		StatusResult: harness.Status{Installed: true, LoggedIn: true},
		Session:      fakeSession,
	}
	service, err := agentchat.NewService(context.Background(), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(context.Background(), "Async endpoint")
	require.NoError(t, err)
	server := &Server{agentService: service}

	requestCtx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/api/agent/sessions/"+created.Session.ID+"/messages", bytes.NewBufferString(`{"content":"ping"}`)).WithContext(requestCtx)
	response := httptest.NewRecorder()
	server.handleAgentSessionByID(response, request)
	require.Equal(t, http.StatusAccepted, response.Code)
	var accepted agentchat.SendMessageResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &accepted))
	require.True(t, accepted.Session.TurnRunning)
	require.Equal(t, "fake-thread", accepted.Session.HarnessSessionID)
	cancel()

	fakeSession.Emit(harness.Event{Kind: harness.EventTurnStarted, TurnID: "turn-1"})
	fakeSession.Emit(harness.Event{Kind: harness.EventAssistantTextDelta, TurnID: "turn-1", Text: "pong"})
	fakeSession.Emit(harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"})
	require.Eventually(t, func() bool {
		persisted, getErr := service.GetSession(context.Background(), created.Session.ID)
		return getErr == nil && !persisted.Session.TurnRunning && len(persisted.Messages) == 2
	}, time.Second, 10*time.Millisecond)
}

type overlappingAgentEventSource struct {
	broker *agentchat.Broker
	event  agentchat.Event
}

func (s *overlappingAgentEventSource) Subscribe(sessionID string) (<-chan agentchat.Event, func()) {
	return s.broker.Subscribe(sessionID)
}

func (s *overlappingAgentEventSource) EventsAfter(context.Context, string, int64) ([]agentchat.Event, error) {
	s.broker.Publish(s.event)
	return []agentchat.Event{s.event}, nil
}

func TestAgentEventStreamDeliversReplaySubscribeOverlapOnce(t *testing.T) {
	source := &overlappingAgentEventSource{
		broker: agentchat.NewBroker(),
		event:  agentchat.Event{ID: 7, SessionID: "agt-1", Type: "turn-completed"},
	}
	stream, err := newAgentEventStream(context.Background(), source, "agt-1", 0)
	require.NoError(t, err)
	t.Cleanup(stream.close)

	event, ok := stream.next(context.Background())
	require.True(t, ok)
	require.Equal(t, int64(7), event.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, ok = stream.next(ctx)
	require.False(t, ok)
}

func TestAgentSessionReadDeleteAndSSEMissingReturnNotFound(t *testing.T) {
	service, err := agentchat.NewService(context.Background(), t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	server := &Server{agentService: service}

	for _, test := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "get", method: http.MethodGet, path: "/api/agent/sessions/missing"},
		{name: "delete", method: http.MethodDelete, path: "/api/agent/sessions/missing"},
		{name: "events", method: http.MethodGet, path: "/api/agent/sessions/missing/events"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.handleAgentSessionByID(response, httptest.NewRequest(test.method, test.path, nil))
			require.Equal(t, http.StatusNotFound, response.Code)
			require.NotEqual(t, "text/event-stream", response.Header().Get("Content-Type"))
		})
	}
}

func TestAgentEventsHonorsLastEventIDAndWritesSSEIDs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "config")
	file := filepath.Join(root, "config.yml")
	original := obsidian.CliConfigPath
	obsidian.CliConfigPath = func() (string, string, error) { return root, file, nil }
	t.Cleanup(func() { obsidian.CliConfigPath = original })
	_, err := agentchat.SaveSettings(agentchat.Settings{Harness: harness.KindCodex})
	require.NoError(t, err)

	fakeSession := harnesstest.NewSession()
	fakeSession.SessionID = "fake-thread"
	driver := &harnesstest.Harness{
		StatusResult: harness.Status{Installed: true, LoggedIn: true},
		Session:      fakeSession,
	}
	service, err := agentchat.NewService(context.Background(), t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: driver})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	created, err := service.CreateSession(context.Background(), "SSE cursor")
	require.NoError(t, err)
	_, err = service.SendMessage(context.Background(), created.Session.ID, "ping")
	require.NoError(t, err)
	fakeSession.Emit(harness.Event{Kind: harness.EventTurnStarted, TurnID: "turn-1"})
	fakeSession.Emit(harness.Event{Kind: harness.EventAssistantTextDelta, TurnID: "turn-1", Text: "pong"})
	fakeSession.Emit(harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn-1"})

	var persisted []agentchat.Event
	require.Eventually(t, func() bool {
		persisted, err = service.EventsAfter(context.Background(), created.Session.ID, 0)
		return err == nil && len(persisted) == 3
	}, time.Second, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/sessions/"+created.Session.ID+"/events?after=0", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", strconv.FormatInt(persisted[1].ID, 10))
	response := httptest.NewRecorder()
	(&Server{agentService: service}).handleAgentSessionByID(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), fmt.Sprintf("id: %d\n", persisted[2].ID))
	require.NotContains(t, response.Body.String(), fmt.Sprintf("id: %d\n", persisted[0].ID))
	require.NotContains(t, response.Body.String(), fmt.Sprintf("id: %d\n", persisted[1].ID))
}
