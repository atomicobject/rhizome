package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func TestPageRelayRejectsMissingAndWrongBearer(t *testing.T) {
	service, err := agentchat.NewService(context.Background(), t.TempDir(), nil)
	require.NoError(t, err)
	defer service.Close()
	server := &Server{agentService: service}
	for _, authorization := range []string{"", "Bearer wrong", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/sessions/missing/page-relay", strings.NewReader("{}"))
		req.Header.Set("Authorization", authorization)
		rec := httptest.NewRecorder()
		server.handleAgentSessionByID(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

func TestAgentContextBoundaryAndPersistence(t *testing.T) {
	env := testkit.Env(t)
	fake := &harnesstest.Harness{StatusResult: harness.Status{Installed: true, LoggedIn: true}, Session: harnesstest.NewSession()}
	ctx := vaultconfig.WithEnv(context.Background(), env)
	service, err := agentchat.NewService(ctx, t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	defer service.Close()
	require.NoError(t, service.SetPageRelayOrigin("http://127.0.0.1:12345"))
	created, err := service.CreateSession(ctx, "Context")
	require.NoError(t, err)
	server := &Server{agentService: service}
	view := agentchat.ViewContext{Kind: "note", Title: "界", Path: "notes/a.md", Route: "/notes", Detail: "Summary"}
	invalid := []agentchat.ViewContext{
		{Title: "A", Route: "/notes"}, {Kind: "invalid", Title: "A", Route: "/notes"},
		{Kind: "note", Route: "/notes"}, {Kind: "note", Title: strings.Repeat("界", 201), Route: "/notes"},
		{Kind: "note", Title: "A", Path: strings.Repeat("a", 1025), Route: "/notes"},
		{Kind: "note", Title: "A", ViewID: strings.Repeat("a", 1025), Route: "/notes"},
		{Kind: "note", Title: "A", Route: "https://example.com"}, {Kind: "note", Title: "A", Route: "/" + strings.Repeat("a", 2048)},
		{Kind: "note", Title: "A", Route: "/notes", Detail: strings.Repeat("界", 2001)},
	}
	for _, candidate := range invalid {
		body, err := json.Marshal(agentchat.SendMessageRequest{Content: "hello", Context: &candidate})
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/agent/sessions/"+created.Session.ID+"/messages", bytes.NewReader(body))
		server.handleAgentSessionByID(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	body, err := json.Marshal(agentchat.SendMessageRequest{Content: "  explain this  ", Context: &view})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	server.handleAgentSessionByID(rec, httptest.NewRequest(http.MethodPost, "/api/agent/sessions/"+created.Session.ID+"/messages", bytes.NewReader(body)))
	require.Equal(t, http.StatusAccepted, rec.Code)
	session, err := service.GetSession(ctx, created.Session.ID)
	require.NoError(t, err)
	require.Equal(t, "  explain this  ", session.Messages[0].Content)
	require.Equal(t, &view, session.Messages[0].Context)
	token := strings.TrimPrefix(fake.StartedWith[0].MCPServers[0].Env[1], "RHIZOME_PAGE_RELAY_TOKEN=")
	relayPath := "/api/agent/sessions/" + created.Session.ID + "/page-relay"
	relayRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, relayPath, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		return req
	}
	absent := httptest.NewRecorder()
	server.handleAgentSessionByID(absent, relayRequest())
	require.Equal(t, http.StatusBadGateway, absent.Code)
	require.Contains(t, absent.Body.String(), "The chat isn't open in a Rhizome window, so the page can't be read.")
	events, unsubscribe := service.Subscribe(created.Session.ID)
	defer unsubscribe()
	relayDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		server.handleAgentSessionByID(rec, relayRequest())
		relayDone <- rec
	}()
	event := <-events
	replyPath := relayPath + "/" + event.Result.RequestID
	reply := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		server.handleAgentSessionByID(rec, httptest.NewRequest(http.MethodPost, replyPath, strings.NewReader(body)))
		return rec
	}
	require.Equal(t, http.StatusBadRequest, reply(`{}`).Code)
	require.Equal(t, http.StatusOK, reply(`{"title":"Empty page","route":"/notes","text":""}`).Code)
	require.Equal(t, http.StatusConflict, reply(`{"text":"late"}`).Code)
	relayResult := <-relayDone
	require.Equal(t, http.StatusOK, relayResult.Code)
	require.Contains(t, relayResult.Body.String(), `"title":"Empty page"`)
	unknown := httptest.NewRecorder()
	server.handleAgentSessionByID(unknown, httptest.NewRequest(http.MethodPost, relayPath+"/unknown", strings.NewReader(`{"error":"gone"}`)))
	require.Equal(t, http.StatusNotFound, unknown.Code)

}
