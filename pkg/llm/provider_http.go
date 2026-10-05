package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

func defaultHTTPClient() httpClient {
	return &http.Client{
		// No timeout here - let the context control cancellation.
		// Setting a client timeout can interfere with context cancellation
		// and cause requests to hang longer than expected.
	}
}

func doJSON(ctx context.Context, client httpClient, method, url string, headers map[string]string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		trimmed := strings.TrimSpace(string(respBody))
		if trimmed == "" {
			trimmed = resp.Status
		}
		return fmt.Errorf("provider request failed: %s", trimmed)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return err
	}
	return nil
}

func doSSE(ctx context.Context, client httpClient, method, url string, headers map[string]string, body any, onEvent func(string, []byte) error) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return readErr
		}
		trimmed := strings.TrimSpace(string(respBody))
		if trimmed == "" {
			trimmed = resp.Status
		}
		return fmt.Errorf("provider request failed: %s", trimmed)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	eventName := ""
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		name := eventName
		eventName = ""
		if strings.TrimSpace(data) == "[DONE]" {
			return nil
		}
		return onEvent(name, []byte(data))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}
