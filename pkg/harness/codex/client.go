package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"github.com/atomicobject/rhizome/pkg/harness"
)

type transport interface {
	Send(context.Context, rpcMessage) error
	Recv(context.Context) (rpcMessage, error)
	BindSessionID(string)
	Close(context.Context) error
}

type client struct {
	transport transport
	nextID    atomic.Int64
	route     func(context.Context, error, rpcMessage, any) error
}

func newSessionClient(transport transport, route func(context.Context, error, rpcMessage, any) error) *client {
	return &client{transport: transport, route: route}
}

func (c *client) initialize(ctx context.Context) (string, error) {
	var result initializeResponse
	err := c.request(ctx, harness.ErrInitialize, "initialize", initializeParams{
		ClientInfo: clientInfo{Name: "rhizome", Title: "Rhizome", Version: "dev"},
	}, &result, nil)
	if err != nil {
		return "", err
	}
	if err := c.notify(ctx, "initialized", nil); err != nil {
		return "", err
	}
	match := versionPattern.FindStringSubmatch(result.UserAgent)
	if match == nil {
		return "", harness.Phase(harness.ErrDecode, fmt.Errorf("initialize response did not include a parseable userAgent"))
	}
	return strings.Join(match[1:], "."), nil
}

func (c *client) startThread(ctx context.Context, params threadParams, notify func(rpcMessage) bool) (string, error) {
	var response threadResponse
	if err := c.request(ctx, harness.ErrThreadStart, "thread/start", params, &response, notify); err != nil {
		return "", err
	}
	if response.Thread.ID == "" {
		return "", harness.Phase(harness.ErrThreadStart, errors.New("response did not include thread.id"))
	}
	return response.Thread.ID, nil
}

func (c *client) resumeThread(ctx context.Context, params threadParams, notify func(rpcMessage) bool) (string, error) {
	var response threadResponse
	if err := c.request(ctx, harness.ErrThreadStart, "thread/resume", params, &response, notify); err != nil {
		return "", err
	}
	if response.Thread.ID == "" {
		return "", harness.Phase(harness.ErrThreadStart, errors.New("response did not include thread.id"))
	}
	return response.Thread.ID, nil
}

func (c *client) startTurn(ctx context.Context, params turnStartParams, notify func(rpcMessage) bool) (turn, error) {
	var response turnResponse
	if err := c.request(ctx, harness.ErrTurnStart, "turn/start", params, &response, notify); err != nil {
		return turn{}, err
	}
	if response.Turn.ID == "" {
		return turn{}, harness.Phase(harness.ErrTurnStart, errors.New("response did not include turn.id"))
	}
	return response.Turn, nil
}

func (c *client) interrupt(ctx context.Context, threadID, turnID string) error {
	return c.sendRequest(ctx, "turn/interrupt", turnInterruptParams{
		ThreadID: threadID,
		TurnID:   turnID,
	})
}

func (c *client) sendRequest(ctx context.Context, method string, params any) error {
	id := c.nextID.Add(1)
	encodedParams, err := json.Marshal(params)
	if err != nil {
		return harness.Phase(harness.ErrDecode, err)
	}
	return c.transport.Send(ctx, rpcMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%d", id)),
		Method:  method,
		Params:  encodedParams,
	})
}

func (c *client) reloadMCP(ctx context.Context, notify func(rpcMessage) bool) error {
	var result struct{}
	return c.request(ctx, harness.ErrTurnStart, "config/mcpServer/reload", nil, &result, notify)
}

func (c *client) account(ctx context.Context) (accountResponse, error) {
	var response accountResponse
	err := c.request(ctx, harness.ErrInitialize, "account/read", struct {
		RefreshToken bool `json:"refreshToken"`
	}{}, &response, nil)
	return response, err
}

func (c *client) models(ctx context.Context) (modelListResponse, error) {
	var result modelListResponse
	cursor := ""
	for {
		var page modelListResponse
		if err := c.request(ctx, harness.ErrInitialize, "model/list", modelListParams{Cursor: cursor}, &page, nil); err != nil {
			return modelListResponse{}, err
		}
		result.Data = append(result.Data, page.Data...)
		if page.NextCursor == "" {
			return result, nil
		}
		if page.NextCursor == cursor {
			return modelListResponse{}, harness.Phase(harness.ErrDecode, errors.New("model/list returned the same cursor twice"))
		}
		cursor = page.NextCursor
	}
}

func (c *client) request(ctx context.Context, phase error, method string, params, result any, notify func(rpcMessage) bool) error {
	id := c.nextID.Add(1)
	encodedParams, err := json.Marshal(params)
	if err != nil {
		return harness.Phase(harness.ErrDecode, err)
	}
	idJSON := json.RawMessage(fmt.Sprintf("%d", id))
	request := rpcMessage{JSONRPC: "2.0", ID: idJSON, Method: method, Params: encodedParams}
	return c.route(ctx, phase, request, result)
}

func (c *client) notify(ctx context.Context, method string, params any) error {
	var encoded json.RawMessage
	if params != nil {
		var err error
		encoded, err = json.Marshal(params)
		if err != nil {
			return harness.Phase(harness.ErrDecode, err)
		}
	}
	if err := c.transport.Send(ctx, rpcMessage{JSONRPC: "2.0", Method: method, Params: encoded}); err != nil {
		return classifyError(ctx, harness.ErrInitialize, err)
	}
	return nil
}

func classifyError(ctx context.Context, phase, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
		return harness.Phase(harness.ErrTimeout, ctx.Err())
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		return harness.Phase(harness.ErrTransportClosed, err)
	}
	return harness.Phase(phase, err)
}

func recoverableThreadError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{"not found", "missing thread", "no such thread", "unknown thread", "does not exist", "no rollout found"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
