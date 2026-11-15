package pagemcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStdioPageText(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer synthetic-test-token", r.Header.Get("Authorization"))
		_, err := w.Write([]byte(`{"title":"A note","route":"/notes","text":"Visible page"}`))
		require.NoError(t, err)
	}))
	defer relay.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input, writer := io.Pipe()
	output, reader := io.Pipe()
	defer input.Close()
	defer writer.Close()
	defer output.Close()
	defer reader.Close()
	stop := context.AfterFunc(ctx, func() { _ = input.Close(); _ = writer.Close(); _ = output.Close(); _ = reader.Close() })
	defer stop()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, input, reader, relay.URL, "synthetic-test-token") }()
	scanner := bufio.NewScanner(output)
	lines := []string{}
	for _, request := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"page_text","arguments":{}}}`,
	} {
		_, err := io.WriteString(writer, request+"\n")
		require.NoError(t, err)
		require.True(t, scanner.Scan())
		lines = append(lines, scanner.Text())
	}
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	require.Contains(t, lines[1], `"page_text"`)
	require.Contains(t, lines[1], `"readOnlyHint":true`)
	var response struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &response))
	require.Equal(t, "Title: A note\nRoute: /notes\n\nVisible page", response.Result.Content[0].Text)
}

func TestPageErrorIsToolError(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"No Rhizome window answered."}`, http.StatusBadGateway)
	}))
	defer relay.Close()
	_, err := readPage(context.Background(), http.DefaultClient, relay.URL, "synthetic")
	require.EqualError(t, err, "No Rhizome window answered.")
}
