package obsidian

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestValidateEmbeddingsProvider(t *testing.T) {
	t.Run("no config file - passes", func(t *testing.T) {
		vault := t.TempDir()
		err := ValidateEmbeddingsProvider(vault)
		if err != nil {
			t.Fatalf("expected no error for missing config, got: %v", err)
		}
	})

	t.Run("embeddings disabled - passes", func(t *testing.T) {
		vault := t.TempDir()
		cfg := LocalConfig{
			NoteEmbeddings: &embeddings.Config{
				Enabled: false,
				// No provider - but embeddings disabled so it's fine
			},
		}
		if err := SaveLocalConfig(vault, cfg); err != nil {
			t.Fatalf("save config: %v", err)
		}

		err := ValidateEmbeddingsProvider(vault)
		if err != nil {
			t.Fatalf("expected no error for disabled embeddings, got: %v", err)
		}
	})

	t.Run("code embeddings enabled without provider - fails", func(t *testing.T) {
		vault := t.TempDir()
		cfg := LocalConfig{
			CodeEmbeddings: &embeddings.Config{
				Enabled: true,
				// No provider specified
			},
		}
		if err := SaveLocalConfig(vault, cfg); err != nil {
			t.Fatalf("save config: %v", err)
		}

		err := ValidateEmbeddingsProvider(vault)
		if err == nil {
			t.Fatal("expected error for missing code provider")
		}
		if !errors.Is(err, ErrEmbeddingsProviderNotConfigured) {
			t.Fatalf("expected ErrEmbeddingsProviderNotConfigured, got: %v", err)
		}
	})

	t.Run("both embeddings enabled without providers - fails with both", func(t *testing.T) {
		vault := t.TempDir()
		cfg := LocalConfig{
			NoteEmbeddings: &embeddings.Config{
				Enabled: true,
			},
			CodeEmbeddings: &embeddings.Config{
				Enabled: true,
			},
		}
		if err := SaveLocalConfig(vault, cfg); err != nil {
			t.Fatalf("save config: %v", err)
		}

		err := ValidateEmbeddingsProvider(vault)
		if err == nil {
			t.Fatal("expected error for missing providers")
		}
		// Should mention both
		errMsg := err.Error()
		if !strings.Contains(errMsg, "noteEmbeddings") || !strings.Contains(errMsg, "codeEmbeddings") {
			t.Fatalf("expected error to mention both noteEmbeddings and codeEmbeddings, got: %v", err)
		}
	})

	t.Run("embeddings enabled without provider - fails", func(t *testing.T) {
		vault := t.TempDir()
		// Create .rhizome directory
		rhizomeDir := filepath.Join(vault, ".rhizome")
		if err := os.MkdirAll(rhizomeDir, 0o755); err != nil {
			t.Fatalf("create .rhizome dir: %v", err)
		}
		config := `noteEmbeddings:
  enabled: true
`
		if err := os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte(config), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		err := ValidateEmbeddingsProvider(vault)
		if err == nil {
			t.Fatal("expected error for missing provider")
		}
		if !errors.Is(err, ErrEmbeddingsProviderNotConfigured) {
			t.Fatalf("expected ErrEmbeddingsProviderNotConfigured, got: %v", err)
		}
	})

	t.Run("embeddings enabled with provider - passes", func(t *testing.T) {
		vault := t.TempDir()
		// Create .rhizome directory
		rhizomeDir := filepath.Join(vault, ".rhizome")
		if err := os.MkdirAll(rhizomeDir, 0o755); err != nil {
			t.Fatalf("create .rhizome dir: %v", err)
		}
		config := `noteEmbeddings:
  enabled: true
  provider: openai
`
		if err := os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte(config), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		err := ValidateEmbeddingsProvider(vault)
		if err != nil {
			t.Fatalf("expected no error with explicit provider, got: %v", err)
		}
	})
}

func TestSaveEmbeddingsConfig_PreservesExplicitOverrides(t *testing.T) {
	t.Run("persists provider and non-default overrides without default model/endpoint", func(t *testing.T) {
		vault := t.TempDir()
		require.NoError(t, SaveLocalConfig(vault, LocalConfig{Notes: LocalVaultConfig{}}))

		embCfg := embeddings.Config{
			Enabled:    true,
			Provider:   "openai",
			Model:      embeddings.DefaultOpenAIModel,
			Endpoint:   embeddings.DefaultOpenAIEndpoint,
			Dimensions: 1536,
			BatchSize:  16,
		}
		require.NoError(t, SaveEmbeddingsConfig(vault, embCfg))

		loaded, err := LoadLocalConfig(vault)
		require.NoError(t, err)
		require.NotNil(t, loaded.NoteEmbeddings)

		note := loaded.NoteEmbeddings
		require.Equal(t, "openai", note.Provider)
		require.Equal(t, "", note.Model)
		require.Equal(t, "", note.Endpoint)
		require.Equal(t, 1536, note.Dimensions)
		require.Equal(t, 0, note.BatchSize)
		require.True(t, note.Enabled)
	})

	t.Run("keeps persisted default model and endpoint", func(t *testing.T) {
		vault := t.TempDir()
		cfg := LocalConfig{
			Notes: LocalVaultConfig{},
			NoteEmbeddings: &embeddings.Config{
				Enabled:  true,
				Provider: "openai",
				Model:    embeddings.DefaultOpenAIModel,
				Endpoint: embeddings.DefaultOpenAIEndpoint,
			},
		}
		require.NoError(t, SaveLocalConfig(vault, cfg))

		embCfg := embeddings.Config{
			Enabled:  true,
			Provider: "openai",
			Model:    embeddings.DefaultOpenAIModel,
			Endpoint: embeddings.DefaultOpenAIEndpoint,
		}
		require.NoError(t, SaveEmbeddingsConfig(vault, embCfg))

		loaded, err := LoadLocalConfig(vault)
		require.NoError(t, err)
		require.NotNil(t, loaded.NoteEmbeddings)

		note := loaded.NoteEmbeddings
		require.Equal(t, embeddings.DefaultOpenAIModel, note.Model)
		require.Equal(t, embeddings.DefaultOpenAIEndpoint, note.Endpoint)
	})
}

func TestEmbeddingsConfig_RuntimeKnobsAreIgnoredAndNotPersisted(t *testing.T) {
	vault := t.TempDir()
	rhizomeDir := filepath.Join(vault, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte(`noteEmbeddings:
  enabled: true
  provider: ollama
  batchsize: 8
  maxconcurrency: 4
`), 0o644))

	loaded, err := LoadEmbeddingsConfig(vault)
	require.NoError(t, err)
	require.Equal(t, 0, loaded.BatchSize)
	require.Equal(t, 0, loaded.MaxConcurrency)

	require.NoError(t, SaveEmbeddingsConfig(vault, loaded))
	raw, err := os.ReadFile(filepath.Join(vault, ".rhizome", "config.yml"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "batchsize")
	require.NotContains(t, string(raw), "maxconcurrency")
}
