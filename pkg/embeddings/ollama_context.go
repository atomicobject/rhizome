package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type ollamaShowResponse struct {
	Parameters string         `json:"parameters"`
	ModelInfo  map[string]any `json:"model_info"`
}

// OllamaContextTokens attempts to read the model context length (tokens) from Ollama.
// It calls /api/show and parses num_ctx from the response.
func OllamaContextTokens(ctx context.Context, client *http.Client, endpoint, model string) (int, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return 0, fmt.Errorf("ollama model not configured")
	}
	showURL, err := ollamaShowURL(endpoint)
	if err != nil {
		return 0, err
	}
	payload := map[string]string{"model": model}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, showURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	client = withDefaultTimeout(client)
	resp, err := doWithRetry(client, req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return 0, fmt.Errorf("ollama show status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var parsed ollamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, err
	}
	modelCtx := parseOllamaContextFromModelInfo(parsed.ModelInfo)
	numCtx := parseOllamaNumCtx(parsed.Parameters)
	switch {
	case modelCtx > 0 && numCtx > 0:
		return min(modelCtx, numCtx), nil
	case modelCtx > 0:
		return modelCtx, nil
	case numCtx > 0:
		return numCtx, nil
	default:
		return 0, fmt.Errorf("ollama show missing context length")
	}
}

func DefaultOllamaContextTokens(model string) int {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "nomic-embed-text:latest" || m == "nomic-embed-text" {
		return 2048
	}
	return 0
}

func parseOllamaNumCtx(params string) int {
	lines := strings.Split(params, "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		if strings.EqualFold(fields[0], "num_ctx") {
			if v, err := strconv.Atoi(fields[1]); err == nil {
				return v
			}
		}
	}
	return 0
}

func parseOllamaContextFromModelInfo(info map[string]any) int {
	if len(info) == 0 {
		return 0
	}
	for k, v := range info {
		if strings.HasSuffix(strings.ToLower(k), "context_length") {
			switch t := v.(type) {
			case float64:
				if t > 0 {
					return int(t)
				}
			case int:
				if t > 0 {
					return t
				}
			case string:
				if v, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && v > 0 {
					return v
				}
			}
		}
	}
	return 0
}

func ollamaShowURL(endpoint string) (string, error) {
	endpoint = NormalizeOllamaEndpoint(endpoint)
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid ollama endpoint %q", endpoint)
	}
	u.Path = "/api/show"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
