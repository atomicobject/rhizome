package agentchat

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/harness"
)

const chatInstructions = `You are the chat assistant inside Rhizome, the knowledge base app the user has open. A user message may begin with a [Rhizome view] block naming what the user is looking at; treat it as the subject of words like "this", "here", and "these". When you need what is on screen beyond that block, call the page_text tool from the rhizome_page server; it returns the visible text of the user's current view. Prefer Rhizome's own tools and the repository's notes for facts. Keep answers short and concrete.`

var ErrPageUnauthorized = errors.New("invalid page relay token")

type PageText struct {
	Title string `json:"title"`
	Route string `json:"route"`
	Text  string `json:"text"`
	Error string `json:"error,omitempty"`
}

type pageRequest struct {
	reply    chan PageText
	expires  time.Time
	answered bool
}

type pageRelay struct {
	mu      sync.Mutex
	token   string
	ctx     context.Context
	cancel  context.CancelFunc
	pending map[string]*pageRequest
}

func newPageRelay(ctx context.Context) (*pageRelay, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	return &pageRelay{token: hex.EncodeToString(token[:]), ctx: ctx, cancel: cancel, pending: make(map[string]*pageRequest)}, nil
}

func (p *pageRelay) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancel()
	p.token = ""
	clear(p.pending)
}

func (s *Service) SetPageRelayOrigin(origin string) error {
	u, err := url.Parse(origin)
	// The server's own listening address, so an IP literal and a port; the
	// per-session token, not the address, authorizes a page read.
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("page relay origin must be an absolute HTTP URL of the server's address")
	}
	if net.ParseIP(u.Hostname()) == nil || u.Port() == "" {
		return errors.New("page relay origin must be an absolute HTTP URL of the server's address")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageRelayOrigin = strings.TrimSuffix(origin, "/")
	return nil
}

func (p *pageRelay) server(origin, sessionID string) (harness.MCPServer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	executable, err := os.Executable()
	if err != nil {
		return harness.MCPServer{}, err
	}
	return harness.MCPServer{Name: "rhizome_page", AutoApproveTools: []string{"page_text"}, Command: executable, Args: []string{"agent", "page-mcp"}, Env: []string{
		"RHIZOME_PAGE_RELAY_URL=" + origin + "/api/agent/sessions/" + url.PathEscape(sessionID) + "/page-relay",
		"RHIZOME_PAGE_RELAY_TOKEN=" + p.token,
		// Keep the helper on the runtime executable in pinned repositories.
		"RZM_SKIP_REPO_DELEGATE=1",
	}}, nil
}

func (s *Service) RequestPage(ctx context.Context, sessionID, token string) (PageText, error) {
	s.mu.Lock()
	live := s.live[sessionID]
	var page *pageRelay
	if live != nil {
		page = live.page
	}
	timeout := s.pageTimeout
	s.mu.Unlock()
	if page == nil {
		return PageText{}, ErrPageUnauthorized
	}
	page.mu.Lock()
	if page.token == "" || subtle.ConstantTimeCompare([]byte(page.token), []byte(token)) != 1 {
		page.mu.Unlock()
		return PageText{}, ErrPageUnauthorized
	}
	requestID := newID("page")
	now := time.Now()
	for id, pending := range page.pending {
		if !now.Before(pending.expires) {
			delete(page.pending, id)
		}
	}
	pending := &pageRequest{reply: make(chan PageText, 1), expires: now.Add(timeout)}
	page.pending[requestID] = pending
	page.mu.Unlock()
	defer func() {
		page.mu.Lock()
		if !pending.answered {
			delete(page.pending, requestID)
		}
		page.mu.Unlock()
	}()
	event := Event{ID: 0, SessionID: sessionID, Type: "page-request", Result: &EventResult{RequestID: requestID}, CreatedAt: now.UTC()}
	if s.broker.Publish(event) == 0 {
		return PageText{}, errors.New("The chat isn't open in a Rhizome window, so the page can't be read.")
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var err error
	select {
	case reply := <-pending.reply:
		return reply, nil
	case <-ctx.Done():
		err = ctx.Err()
	case <-page.ctx.Done():
		err = errors.New("The chat session stopped.")
	case <-timer.C:
		err = errors.New("No Rhizome window answered.")
	}
	// A reply accepted before the deadline wins over a timer observed with it.
	select {
	case reply := <-pending.reply:
		return reply, nil
	default:
		return PageText{}, err
	}
}

func (s *Service) ReplyPage(sessionID, requestID string, reply PageText) error {
	s.mu.Lock()
	live := s.live[sessionID]
	var page *pageRelay
	if live != nil {
		page = live.page
	}
	s.mu.Unlock()
	if page == nil {
		return ErrNotFound
	}
	page.mu.Lock()
	defer page.mu.Unlock()
	pending := page.pending[requestID]
	if pending == nil || !time.Now().Before(pending.expires) {
		delete(page.pending, requestID)
		return ErrNotFound
	}
	if pending.answered {
		return fmt.Errorf("%w: page request already answered", ErrConflict)
	}
	if len(reply.Text) > 60000 {
		end := 60000
		for !utf8.RuneStart(reply.Text[end]) {
			end--
		}
		reply.Text = reply.Text[:end]
	}
	pending.answered = true
	pending.reply <- reply
	return nil
}

// Page requests carry only the correlation id, without harness event fields.
func (e Event) MarshalJSON() ([]byte, error) {
	type eventJSON Event
	if e.Type != "page-request" {
		return json.Marshal(eventJSON(e))
	}
	return json.Marshal(struct {
		eventJSON
		Result struct {
			RequestID string `json:"requestId"`
		} `json:"result"`
	}{eventJSON: eventJSON(e), Result: struct {
		RequestID string `json:"requestId"`
	}{RequestID: e.Result.RequestID}})
}
