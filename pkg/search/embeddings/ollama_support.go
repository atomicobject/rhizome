package embeddings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
)

// EnsureOllamaForConfig ensures a local Ollama runtime is available for the config.
// For non-local endpoints, it skips auto-start/pull.
func EnsureOllamaForConfig(ctx context.Context, cfg Config, out io.Writer) error {
	if strings.ToLower(strings.TrimSpace(cfg.Provider)) != "ollama" {
		return nil
	}
	if !OllamaEndpointIsLocal(cfg.Endpoint) {
		return nil
	}
	if err := EnsureOllamaReady(ctx, cfg.Model, true, out); err != nil {
		return fmt.Errorf("%s", FormatOllamaEnsureError(err, cfg.Model))
	}
	return nil
}

func FormatOllamaEnsureError(err error, model string) string {
	switch {
	case errors.Is(err, ErrOllamaNotInstalled):
		hint := OllamaInstallHint()
		if hint != "" {
			return fmt.Sprintf("Ollama is configured but not installed. %s", hint)
		}
		return "Ollama is configured but not installed."
	case errors.Is(err, ErrOllamaNotRunning):
		hint := OllamaInstallHint()
		if hint != "" {
			return fmt.Sprintf("Ollama is configured but not running. Start it (\"ollama serve\") and retry. %s", hint)
		}
		return "Ollama is configured but not running. Start it (\"ollama serve\") and retry."
	case errors.Is(err, ErrOllamaModelMissing):
		if strings.TrimSpace(model) != "" {
			return fmt.Sprintf("Ollama model %q is missing. Run \"ollama pull %s\" and retry.", model, model)
		}
		return "Ollama is configured but no model is set. Set noteEmbeddings.model and codeEmbeddings.model in .rhizome/config.yml, then run \"ollama pull <model>\"."
	default:
		return fmt.Sprintf("Ollama error: %v", err)
	}
}

// OllamaEndpointIsLocal reports whether the configured endpoint is local.
func OllamaEndpointIsLocal(endpoint string) bool {
	host := endpointHost(endpoint)
	if host == "" {
		return strings.TrimSpace(endpoint) == ""
	}
	host = strings.Trim(host, "[]")
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0", "::":
		return true
	default:
		return false
	}
}

func endpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return hostWithoutPort(u.Host)
	}
	if u, err := url.Parse("http://" + endpoint); err == nil && u.Host != "" {
		return hostWithoutPort(u.Host)
	}
	return ""
}

func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
