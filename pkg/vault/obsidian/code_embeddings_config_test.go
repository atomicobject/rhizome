package obsidian

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func writeLocalConfig(t *testing.T, vault string, body string) {
	t.Helper()
	configPath := filepath.Join(vault, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte(body), 0o644))
}

func TestLoadCodeEmbeddingsConfig_InheritsProviderButNotModel(t *testing.T) {
	t.Run("inherits openai provider with code model default", func(t *testing.T) {
		vault := t.TempDir()
		indexPath := filepath.Join(vault, "shared.db")
		writeLocalConfig(t, vault, `indexPath: `+indexPath+`
noteEmbeddings:
  enabled: true
  provider: openai
  model: `+embeddings.DefaultOpenAIModel+`
`)

		loaded, explicit, err := LoadCodeEmbeddingsConfig(vault)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if explicit {
			t.Fatalf("expected implicit code embeddings config")
		}
		// Provider is inherited from note embeddings.
		if loaded.Provider != "openai" {
			t.Fatalf("expected openai provider inherited, got %s", loaded.Provider)
		}
		// Model is NOT inherited - code embeddings use their own default (small for OpenAI).
		if loaded.Model != embeddings.DefaultOpenAICodeModel {
			t.Fatalf("expected code model default %s, got %s", embeddings.DefaultOpenAICodeModel, loaded.Model)
		}
		if loaded.IndexPath != indexPath {
			t.Fatalf("expected index path %s, got %s", indexPath, loaded.IndexPath)
		}
	})

	t.Run("inherits ollama provider with ollama model default", func(t *testing.T) {
		vault := t.TempDir()
		indexPath := filepath.Join(vault, "shared.db")
		writeLocalConfig(t, vault, `indexPath: `+indexPath+`
noteEmbeddings:
  enabled: true
  provider: ollama
  model: `+embeddings.DefaultOllamaModel+`
`)

		loaded, explicit, err := LoadCodeEmbeddingsConfig(vault)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if explicit {
			t.Fatalf("expected implicit code embeddings config")
		}
		// Provider is inherited from note embeddings.
		if loaded.Provider != "ollama" {
			t.Fatalf("expected ollama provider inherited, got %s", loaded.Provider)
		}
		// Model uses Ollama default (same model for notes and code).
		if loaded.Model != embeddings.DefaultOllamaModel {
			t.Fatalf("expected ollama model default %s, got %s", embeddings.DefaultOllamaModel, loaded.Model)
		}
		if loaded.IndexPath != indexPath {
			t.Fatalf("expected index path %s, got %s", indexPath, loaded.IndexPath)
		}
	})

	t.Run("defaults to ollama when no provider specified", func(t *testing.T) {
		vault := t.TempDir()
		indexPath := filepath.Join(vault, "shared.db")
		writeLocalConfig(t, vault, `indexPath: `+indexPath+`
noteEmbeddings:
  enabled: true
`)

		loaded, explicit, err := LoadCodeEmbeddingsConfig(vault)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if explicit {
			t.Fatalf("expected implicit code embeddings config")
		}
		// Should default to Ollama provider.
		if loaded.Provider != "ollama" {
			t.Fatalf("expected ollama provider default, got %s", loaded.Provider)
		}
		// Should use Ollama default model.
		if loaded.Model != embeddings.DefaultOllamaModel {
			t.Fatalf("expected ollama model default %s, got %s", embeddings.DefaultOllamaModel, loaded.Model)
		}
	})
}

func TestLoadCodeEmbeddingsConfig_ExplicitOverrides(t *testing.T) {
	vault := t.TempDir()
	indexPath := filepath.Join(vault, "combined.db")
	writeLocalConfig(t, vault, `indexPath: `+indexPath+`
noteEmbeddings:
  enabled: true
  provider: note
  model: note-model
codeEmbeddings:
  enabled: true
  provider: code
  model: code-model
`)

	loaded, explicit, err := LoadCodeEmbeddingsConfig(vault)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !explicit {
		t.Fatalf("expected explicit code embeddings config")
	}
	if loaded.Provider != "code" || loaded.Model != "code-model" {
		t.Fatalf("expected code provider preserved, got %s/%s", loaded.Provider, loaded.Model)
	}
	if !loaded.Enabled {
		t.Fatalf("expected explicit code embeddings to remain enabled")
	}
	if loaded.IndexPath != indexPath {
		t.Fatalf("expected index path to follow vault index path, got %s", loaded.IndexPath)
	}
}

func TestSaveCodeEmbeddingsConfig_PreservesInheritedOverrides(t *testing.T) {
	vault := t.TempDir()
	cfg := LocalConfig{
		Notes: LocalVaultConfig{},
		NoteEmbeddings: &embeddings.Config{
			Enabled:        true,
			Provider:       "openai",
			Endpoint:       "https://example.com/v1/embeddings",
			Dimensions:     1536,
			BatchSize:      12,
			MaxConcurrency: 7,
		},
	}
	require.NoError(t, SaveLocalConfig(vault, cfg))

	semanticCfg, err := LoadEmbeddingsConfig(vault)
	require.NoError(t, err)

	codeCfg, _, err := EffectiveCodeEmbeddingsConfig(vault, semanticCfg)
	require.NoError(t, err)
	require.NoError(t, SaveCodeEmbeddingsConfig(vault, codeCfg))

	loaded, err := LoadLocalConfig(vault)
	require.NoError(t, err)
	require.NotNil(t, loaded.CodeEmbeddings)

	code := loaded.CodeEmbeddings
	require.Equal(t, "openai", code.Provider)
	require.Equal(t, "https://example.com/v1/embeddings", code.Endpoint)
	require.Equal(t, 1536, code.Dimensions)
	require.Equal(t, 0, code.BatchSize)
	require.Equal(t, 0, code.MaxConcurrency)
	require.Equal(t, "", code.Model)
	require.True(t, code.Enabled)

	roundtrip, explicit, err := LoadCodeEmbeddingsConfig(vault)
	require.NoError(t, err)
	require.True(t, explicit)
	require.Equal(t, 1536, roundtrip.Dimensions)
	require.Equal(t, 0, roundtrip.BatchSize)
	require.Equal(t, 0, roundtrip.MaxConcurrency)
}

func TestLoadCodeEmbeddingsConfig_IgnoresRuntimeKnobs(t *testing.T) {
	vault := t.TempDir()
	writeLocalConfig(t, vault, `codeEmbeddings:
  enabled: true
  provider: ollama
  batchsize: 8
  maxconcurrency: 4
`)

	loaded, explicit, err := LoadCodeEmbeddingsConfig(vault)
	require.NoError(t, err)
	require.True(t, explicit)
	require.Equal(t, 0, loaded.BatchSize)
	require.Equal(t, 0, loaded.MaxConcurrency)

	require.NoError(t, SaveCodeEmbeddingsConfig(vault, loaded))
	raw, err := os.ReadFile(filepath.Join(vault, ".rhizome", "config.yml"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "batchsize")
	require.NotContains(t, string(raw), "maxconcurrency")
}
