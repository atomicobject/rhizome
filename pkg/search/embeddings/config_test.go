package embeddings

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestApplyProviderDefaultsForKind(t *testing.T) {
	t.Run("empty provider defaults to ollama", func(t *testing.T) {
		cfg := Config{Provider: ""}
		got := ApplyProviderDefaultsForKind(cfg, "note")
		if got.Provider != "ollama" {
			t.Errorf("expected provider 'ollama', got %q", got.Provider)
		}
		if got.Model != DefaultOllamaModel {
			t.Errorf("expected model %q, got %q", DefaultOllamaModel, got.Model)
		}
		if got.Endpoint != DefaultOllamaEndpoint {
			t.Errorf("expected endpoint %q, got %q", DefaultOllamaEndpoint, got.Endpoint)
		}
	})

	t.Run("ollama provider fixes openai model to ollama default", func(t *testing.T) {
		cfg := Config{
			Provider: "ollama",
			Model:    DefaultOpenAIModel, // wrong model for provider
			Endpoint: DefaultOpenAIEndpoint,
		}
		got := ApplyProviderDefaultsForKind(cfg, "note")
		if got.Model != DefaultOllamaModel {
			t.Errorf("expected model %q, got %q", DefaultOllamaModel, got.Model)
		}
		if got.Endpoint != DefaultOllamaEndpoint {
			t.Errorf("expected endpoint %q, got %q", DefaultOllamaEndpoint, got.Endpoint)
		}
	})

	t.Run("openai provider fixes ollama model to openai note default", func(t *testing.T) {
		cfg := Config{
			Provider: "openai",
			Model:    DefaultOllamaModel, // wrong model for provider
			Endpoint: DefaultOllamaEndpoint,
		}
		got := ApplyProviderDefaultsForKind(cfg, "note")
		if got.Model != DefaultOpenAIModel {
			t.Errorf("expected model %q, got %q", DefaultOpenAIModel, got.Model)
		}
		if got.Endpoint != DefaultOpenAIEndpoint {
			t.Errorf("expected endpoint %q, got %q", DefaultOpenAIEndpoint, got.Endpoint)
		}
	})

	t.Run("openai provider for code uses small model", func(t *testing.T) {
		cfg := Config{
			Provider: "openai",
			Model:    "", // unset
		}
		got := ApplyProviderDefaultsForKind(cfg, "code")
		if got.Model != DefaultOpenAICodeModel {
			t.Errorf("expected model %q, got %q", DefaultOpenAICodeModel, got.Model)
		}
	})

	t.Run("ollama provider for code uses same model as notes", func(t *testing.T) {
		cfg := Config{
			Provider: "ollama",
			Model:    "", // unset
		}
		got := ApplyProviderDefaultsForKind(cfg, "code")
		if got.Model != DefaultOllamaModel {
			t.Errorf("expected model %q, got %q", DefaultOllamaModel, got.Model)
		}
	})

	t.Run("preserves custom ollama model", func(t *testing.T) {
		customModel := "mxbai-embed-large:latest"
		cfg := Config{
			Provider: "ollama",
			Model:    customModel,
		}
		got := ApplyProviderDefaultsForKind(cfg, "note")
		if got.Model != customModel {
			t.Errorf("expected custom model %q preserved, got %q", customModel, got.Model)
		}
	})

	t.Run("preserves custom openai model", func(t *testing.T) {
		customModel := "text-embedding-ada-002"
		cfg := Config{
			Provider: "openai",
			Model:    customModel,
		}
		got := ApplyProviderDefaultsForKind(cfg, "note")
		if got.Model != customModel {
			t.Errorf("expected custom model %q preserved, got %q", customModel, got.Model)
		}
	})

	for _, tc := range []struct{ model, want string }{
		{"text-embedding-3-large", DefaultOllamaModel},
		{"text-embedding-3-small", DefaultOllamaModel},
		{"text-embedding-ada-002", DefaultOllamaModel},
		{"TEXT-EMBEDDING-3-LARGE", DefaultOllamaModel},
		{"nomic-embed-text:latest", "nomic-embed-text:latest"},
		{"mxbai-embed-large:latest", "mxbai-embed-large:latest"},
		{"", DefaultOllamaModel},
	} {
		t.Run("ollama model "+tc.model, func(t *testing.T) {
			got := ApplyProviderDefaultsForKind(Config{Provider: "ollama", Model: tc.model}, "note")
			if got.Model != tc.want {
				t.Fatalf("model = %q, want %q", got.Model, tc.want)
			}
		})
	}
	for _, tc := range []struct{ model, want string }{
		{"nomic-embed-text:latest", DefaultOpenAIModel},
		{"nomic-embed-text", DefaultOpenAIModel},
		{"mxbai-embed-large:latest", DefaultOpenAIModel},
		{"all-minilm:latest", DefaultOpenAIModel},
		{"snowflake-arctic-embed:latest", DefaultOpenAIModel},
		{"custom-model:latest", DefaultOpenAIModel},
		{"text-embedding-3-large", "text-embedding-3-large"},
		{"text-embedding-3-small", "text-embedding-3-small"},
		{"", DefaultOpenAIModel},
	} {
		t.Run("openai model "+tc.model, func(t *testing.T) {
			got := ApplyProviderDefaultsForKind(Config{Provider: "openai", Model: tc.model}, "note")
			if got.Model != tc.want {
				t.Fatalf("model = %q, want %q", got.Model, tc.want)
			}
		})
	}
	for _, tc := range []struct{ endpoint, want string }{
		{"http://localhost:11434/api/embed", DefaultOpenAIEndpoint},
		{"http://127.0.0.1:11434/api/embed", DefaultOpenAIEndpoint},
		{"http://localhost:11434/api/embeddings", DefaultOpenAIEndpoint},
		{"https://api.openai.com/v1/embeddings", DefaultOpenAIEndpoint},
		{"https://custom.example/v1/embeddings", "https://custom.example/v1/embeddings"},
		{"", DefaultOpenAIEndpoint},
	} {
		t.Run("openai endpoint "+tc.endpoint, func(t *testing.T) {
			got := ApplyProviderDefaultsForKind(Config{Provider: "openai", Endpoint: tc.endpoint}, "note")
			if got.Endpoint != tc.want {
				t.Fatalf("endpoint = %q, want %q", got.Endpoint, tc.want)
			}
		})
	}
}

func TestDefaultModelForProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		kind     string
		want     string
	}{
		{"empty provider defaults to ollama", "", "note", DefaultOllamaModel},
		{"ollama note", "ollama", "note", DefaultOllamaModel},
		{"ollama code", "ollama", "code", DefaultOllamaModel},
		{"openai note", "openai", "note", DefaultOpenAIModel},
		{"openai code", "openai", "code", DefaultOpenAICodeModel},
		{"case insensitive ollama", "OLLAMA", "note", DefaultOllamaModel},
		{"case insensitive openai", "OPENAI", "code", DefaultOpenAICodeModel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultModelForProvider(tt.provider, tt.kind)
			if got != tt.want {
				t.Errorf("DefaultModelForProvider(%q, %q) = %q, want %q", tt.provider, tt.kind, got, tt.want)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/test/vault")
	if cfg.Provider != "ollama" {
		t.Errorf("expected default provider 'ollama', got %q", cfg.Provider)
	}
	if cfg.Model != DefaultOllamaModel {
		t.Errorf("expected default model %q, got %q", DefaultOllamaModel, cfg.Model)
	}
	if cfg.Endpoint != DefaultOllamaEndpoint {
		t.Errorf("expected default endpoint %q, got %q", DefaultOllamaEndpoint, cfg.Endpoint)
	}
}

func TestDefaultOpenAIConfig(t *testing.T) {
	cfg := DefaultOpenAIConfig("/test/vault")
	if cfg.Provider != "openai" {
		t.Errorf("expected provider 'openai', got %q", cfg.Provider)
	}
	if cfg.Model != DefaultOpenAIModel {
		t.Errorf("expected model %q, got %q", DefaultOpenAIModel, cfg.Model)
	}
	if cfg.Endpoint != DefaultOpenAIEndpoint {
		t.Errorf("expected endpoint %q, got %q", DefaultOpenAIEndpoint, cfg.Endpoint)
	}
}

func TestDefaultOpenAICodeConfig(t *testing.T) {
	cfg := DefaultOpenAICodeConfig("/test/vault")
	if cfg.Provider != "openai" {
		t.Errorf("expected provider 'openai', got %q", cfg.Provider)
	}
	if cfg.Model != DefaultOpenAICodeModel {
		t.Errorf("expected model %q, got %q", DefaultOpenAICodeModel, cfg.Model)
	}
}

func TestConfigOmitsRuntimeKnobsFromProviderAndYAML(t *testing.T) {
	cfg := Config{
		Provider:       "ollama",
		Model:          DefaultOllamaModel,
		BatchSize:      8,
		MaxConcurrency: 4,
	}

	providerCfg := cfg.ProviderCfg("")
	if providerCfg.BatchSize != 0 {
		t.Fatalf("expected provider config batch size to stay unset, got %d", providerCfg.BatchSize)
	}
	if providerCfg.MaxConcurrency != 0 {
		t.Fatalf("expected provider config max concurrency to stay unset, got %d", providerCfg.MaxConcurrency)
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal yaml: %v", err)
	}
	if strings.Contains(string(out), "batchsize") {
		t.Fatalf("yaml should not contain batchsize: %s", out)
	}
	if strings.Contains(string(out), "maxconcurrency") {
		t.Fatalf("yaml should not contain maxconcurrency: %s", out)
	}
}
