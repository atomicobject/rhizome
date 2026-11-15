package agentchat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/harness"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func relayService(t *testing.T) (*Service, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	page, err := newPageRelay(ctx)
	require.NoError(t, err)
	s := &Service{ctx: ctx, broker: NewBroker(), live: map[string]*liveSession{"chat": {page: page}}, pageTimeout: time.Second}
	t.Cleanup(func() { cancel(); page.close(); s.broker.Close() })
	return s, page.token
}

func TestPageRelayNoSubscriber(t *testing.T) {
	s, token := relayService(t)
	_, err := s.RequestPage(context.Background(), "chat", token)
	require.EqualError(t, err, "The chat isn't open in a Rhizome window, so the page can't be read.")
	require.Empty(t, s.live["chat"].page.pending)
}

func TestPageRelayUnauthorized(t *testing.T) {
	s, _ := relayService(t)
	for _, token := range []string{"", "wrong"} {
		_, err := s.RequestPage(context.Background(), "chat", token)
		require.ErrorIs(t, err, ErrPageUnauthorized)
	}
}

func TestPageRelayRoundTrip(t *testing.T) {
	s, token := relayService(t)
	events, unsub := s.Subscribe("chat")
	defer unsub()
	done := make(chan PageText, 1)
	go func() {
		page, err := s.RequestPage(context.Background(), "chat", token)
		if err != nil {
			page.Error = err.Error()
		}
		done <- page
	}()
	event := <-events
	require.Zero(t, event.ID)
	require.Equal(t, "page-request", event.Type)
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var projected map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &projected))
	require.JSONEq(t, `{"requestId":"`+event.Result.RequestID+`"}`, string(projected["result"]))
	reply := PageText{Title: "Example", Route: "/notes", Text: strings.Repeat("界", 20001)}
	require.NoError(t, s.ReplyPage("chat", event.Result.RequestID, reply))
	require.ErrorIs(t, s.ReplyPage("chat", event.Result.RequestID, reply), ErrConflict)
	result := <-done
	require.Empty(t, result.Error)
	require.Equal(t, reply.Title, result.Title)
	require.Len(t, result.Text, 60000)
	require.True(t, utf8.ValidString(result.Text))
	require.ErrorIs(t, s.ReplyPage("chat", "missing", reply), ErrNotFound)
}

func TestPageRelayTimeoutAndStop(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "stop"}[stop], func(t *testing.T) {
			s, token := relayService(t)
			if !stop {
				s.pageTimeout = 10 * time.Millisecond
			}
			events, unsub := s.Subscribe("chat")
			defer unsub()
			done := make(chan error, 1)
			go func() { _, err := s.RequestPage(context.Background(), "chat", token); done <- err }()
			event := <-events
			if stop {
				s.live["chat"].page.close()
			}
			err := <-done
			if stop {
				require.EqualError(t, err, "The chat session stopped.")
			} else {
				require.EqualError(t, err, "No Rhizome window answered.")
			}
			require.ErrorIs(t, s.ReplyPage("chat", event.Result.RequestID, PageText{Text: "late"}), ErrNotFound)
			require.Empty(t, s.live["chat"].page.pending)
		})
	}
}

func TestViewContextTurnText(t *testing.T) {
	view := &ViewContext{Kind: "note", Title: "Backlink Decision", Path: "notes/decisions/backlink-decision.md", Route: "/notes?note=notes/decisions/backlink-decision.md", Detail: "Decision details"}
	require.Equal(t, "[Rhizome view] Note \"Backlink Decision\" · notes/decisions/backlink-decision.md\nRoute: /notes?note=notes/decisions/backlink-decision.md\nDetail: Decision details\n\nExplain this.", turnText("Explain this.", view))
	require.Equal(t, "  original text  ", turnText("  original text  ", nil))
}

func TestChatInstructionsRouteRhizomeQuestionsToSkill(t *testing.T) {
	require.Contains(t, chatInstructions, "load the rhizome skill")
	require.Contains(t, chatInstructions, ".agents/skills/rhizome/SKILL.md")
	require.Contains(t, chatInstructions, "guidance is not installed")
	// Every chat session's system prompt carries this text, so keep it short.
	require.LessOrEqual(t, len(strings.Fields(chatInstructions)), 140)
}

func TestSettingsPermissionModes(t *testing.T) {
	for _, mode := range []harness.PermissionMode{harness.PermissionAuto, harness.PermissionApprovalRequired, harness.PermissionAutoAcceptEdits, harness.PermissionFullAccess} {
		require.Equal(t, mode, normalizePermissionMode(mode))
	}
	require.Equal(t, harness.PermissionAuto, normalizePermissionMode(""))
	require.Equal(t, harness.PermissionAuto, normalizePermissionMode("unknown"))
}

func TestChatSessionPageOptionsAndRawMessage(t *testing.T) {
	env := setTestSettings(t, Settings{Harness: harness.KindCodex})
	ctx := vaultconfig.WithEnv(context.Background(), env)
	root := t.TempDir()
	fake := newFakeHarness()
	service, err := NewService(ctx, root, map[harness.Kind]harness.Harness{harness.KindCodex: fake})
	require.NoError(t, err)
	defer service.Close()
	require.NoError(t, service.SetPageRelayOrigin("http://127.0.0.1:12345"))
	created, err := service.CreateSession(ctx, "Context")
	require.NoError(t, err)
	view := &ViewContext{Kind: "note", Title: "A", Route: "/notes"}
	_, err = service.SendMessage(ctx, created.Session.ID, "  explain this  ", view)
	require.NoError(t, err)
	options := <-fake.starts
	require.Equal(t, chatInstructions, options.Instructions)
	require.Equal(t, harness.PermissionAuto, options.PermissionMode)
	require.Len(t, options.MCPServers, 1)
	page := options.MCPServers[0]
	require.Equal(t, "rhizome_page", page.Name)
	require.Equal(t, []string{"agent", "page-mcp"}, page.Args)
	require.Equal(t, []string{"page_text"}, page.AutoApproveTools)
	require.Contains(t, page.Env[0], "http://127.0.0.1:12345/api/agent/sessions/"+created.Session.ID+"/page-relay")
	require.GreaterOrEqual(t, len(strings.TrimPrefix(page.Env[1], "RHIZOME_PAGE_RELAY_TOKEN=")), 32)
	require.Equal(t, turnText("  explain this  ", view), <-fake.session.turns)
	stored, err := service.GetSession(ctx, created.Session.ID)
	require.NoError(t, err)
	require.Equal(t, "  explain this  ", stored.Messages[0].Content)
	require.Equal(t, view, stored.Messages[0].Context)
}
