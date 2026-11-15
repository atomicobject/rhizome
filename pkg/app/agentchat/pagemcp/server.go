// Package pagemcp serves the window-scoped page reader over stdio MCP.
package pagemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func Serve(ctx context.Context, input io.Reader, output io.Writer, relayURL, token string) error {
	u, err := url.Parse(relayURL)
	// The Rhizome server's own listening address, which serve chose: loopback,
	// or the one address it is bound to. The token authorizes the read.
	if err != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" {
		return errors.New("page relay URL must be an absolute HTTP URL of the Rhizome server")
	}
	if net.ParseIP(u.Hostname()) == nil || u.Port() == "" || token == "" {
		return errors.New("page relay URL and token are required")
	}
	client := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	server := mcpserver.NewMCPServer("rhizome_page", "1.0.0")
	server.AddTool(mcp.NewTool("page_text", mcp.WithDescription("Read the visible text of the Rhizome view the user is looking at in the chat's window."), mcp.WithReadOnlyHintAnnotation(true), mcp.WithDestructiveHintAnnotation(false)), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if len(request.GetArguments()) != 0 {
			return mcp.NewToolResultError("page_text takes no arguments"), nil
		}
		text, err := readPage(ctx, client, relayURL, token)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(text), nil
	})
	return mcpserver.NewStdioServer(server).Listen(ctx, input, output)
}

func readPage(ctx context.Context, client *http.Client, relayURL, token string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, relayURL, bytes.NewBufferString("{}"))
	if err != nil {
		return "", errors.New("could not create page relay request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New("could not reach the Rhizome page relay")
	}
	defer response.Body.Close()
	var page struct {
		Title string `json:"title"`
		Route string `json:"route"`
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&page); err != nil {
		return "", errors.New("invalid page relay response")
	}
	if page.Error != "" {
		return "", errors.New(page.Error)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("page relay returned HTTP %d", response.StatusCode)
	}
	return fmt.Sprintf("Title: %s\nRoute: %s\n\n%s", page.Title, page.Route, page.Text), nil
}
