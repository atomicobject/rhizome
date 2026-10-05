package embeddings

import (
	"net/url"
	"strings"
)

// NormalizeOllamaEndpoint ensures Ollama uses a batch-capable endpoint.
// It preserves host/scheme but rewrites legacy /api/embeddings to /api/embed.
func NormalizeOllamaEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return DefaultOllamaEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		u, err = url.Parse("http://" + endpoint)
	}
	if err != nil || u.Host == "" {
		return DefaultOllamaEndpoint
	}

	cleanPath := strings.TrimSuffix(u.Path, "/")
	if cleanPath == "" {
		cleanPath = defaultOllamaEndpointPath()
	}
	if strings.HasSuffix(cleanPath, "/api/embeddings") {
		cleanPath = strings.TrimSuffix(cleanPath, "/api/embeddings") + "/api/embed"
	}
	u.Path = cleanPath
	return u.String()
}

func defaultOllamaEndpointPath() string {
	u, err := url.Parse(DefaultOllamaEndpoint)
	if err != nil || u.Path == "" {
		return "/api/embed"
	}
	return u.Path
}
