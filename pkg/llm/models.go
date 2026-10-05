package llm

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	openAIModelsEndpoint    = "https://api.openai.com/v1/models"
	cerebrasModelsEndpoint  = "https://api.cerebras.ai/v1/models"
	anthropicModelsEndpoint = "https://api.anthropic.com/v1/models"
	geminiModelsEndpoint    = "https://generativelanguage.googleapis.com/v1beta/models"
)

// ModelInfo describes a provider model returned by a model-list API.
type ModelInfo struct {
	ID          string
	DisplayName string
	OwnedBy     string
}

// ListModels asks a provider for the models available to the configured key.
func ListModels(ctx context.Context, provider string) ([]ModelInfo, error) {
	return listModels(ctx, defaultHTTPClient(), provider)
}

func listModels(ctx context.Context, client httpClient, provider string) ([]ModelInfo, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	apiKey := APIKeyForProvider(provider)
	if provider != "mock" && apiKey == "" {
		return nil, fmt.Errorf("missing API key for provider %s", provider)
	}
	switch provider {
	case "openai":
		return listOpenAICompatibleModels(ctx, client, openAIModelsEndpoint, apiKey)
	case "cerebras":
		return listOpenAICompatibleModels(ctx, client, cerebrasModelsEndpoint, apiKey)
	case "anthropic":
		return listAnthropicModelsFromEndpoint(ctx, client, anthropicModelsEndpoint, apiKey)
	case "gemini":
		return listGeminiModelsFromEndpoint(ctx, client, geminiModelsEndpoint, apiKey)
	case "mock":
		return []ModelInfo{{ID: "mock", DisplayName: "Mock"}}, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

type compatibleModelsResponse struct {
	Data []compatibleModel `json:"data"`
}

type compatibleModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by"`
}

func listOpenAICompatibleModels(ctx context.Context, client httpClient, endpoint, apiKey string) ([]ModelInfo, error) {
	var response compatibleModelsResponse
	if err := doJSON(ctx, client, "GET", endpoint, map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, nil, &response); err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(response.Data))
	for _, model := range response.Data {
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		models = append(models, ModelInfo{ID: model.ID, OwnedBy: model.OwnedBy})
	}
	sortModels(models)
	return models, nil
}

type anthropicModelsResponse struct {
	Data []anthropicModel `json:"data"`
}

type anthropicModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

func listAnthropicModelsFromEndpoint(ctx context.Context, client httpClient, endpoint, apiKey string) ([]ModelInfo, error) {
	var response anthropicModelsResponse
	if err := doJSON(ctx, client, "GET", endpoint, map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": anthropicVersion,
	}, nil, &response); err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(response.Data))
	for _, model := range response.Data {
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		models = append(models, ModelInfo{ID: model.ID, DisplayName: model.DisplayName})
	}
	sortModels(models)
	return models, nil
}

type geminiModelsResponse struct {
	Models []geminiModelInfo `json:"models"`
}

type geminiModelInfo struct {
	Name                       string   `json:"name"`
	BaseModelID                string   `json:"baseModelId"`
	DisplayName                string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

func listGeminiModelsFromEndpoint(ctx context.Context, client httpClient, endpoint, apiKey string) ([]ModelInfo, error) {
	var response geminiModelsResponse
	if err := doJSON(ctx, client, "GET", endpoint+"?key="+url.QueryEscape(apiKey), nil, nil, &response); err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(response.Models))
	for _, model := range response.Models {
		if !containsString(model.SupportedGenerationMethods, "generateContent") {
			continue
		}
		id := strings.TrimPrefix(model.Name, "models/")
		if model.BaseModelID != "" {
			id = model.BaseModelID
		}
		if strings.TrimSpace(id) == "" {
			continue
		}
		models = append(models, ModelInfo{ID: id, DisplayName: model.DisplayName})
	}
	sortModels(models)
	return models, nil
}

func sortModels(models []ModelInfo) {
	sort.SliceStable(models, func(i, j int) bool {
		return strings.ToLower(models[i].ID) < strings.ToLower(models[j].ID)
	})
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
