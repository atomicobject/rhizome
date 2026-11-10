package repoconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/stretchr/testify/require"
)

func TestSettingsSavesPreserveConcurrentSourceIncludes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		save  func(string) error
		check func(*LocalConfig) bool
	}{
		{"graph", func(root string) error {
			return SaveGraphConfig(root, LocalGraphConfig{Ignore: []string{"private/**"}})
		}, func(cfg *LocalConfig) bool { return cfg.Graph != nil && len(cfg.Graph.Ignore) == 1 }},
		{"code", func(root string) error { return SaveCodeConfig(root, codeanchor.Config{Enabled: true}) }, func(cfg *LocalConfig) bool { return cfg.Code.Enabled }},
		{"note embeddings", func(root string) error {
			return SaveEmbeddingsConfig(root, embeddings.Config{Enabled: true, Provider: "ollama"})
		}, func(cfg *LocalConfig) bool { return cfg.NoteEmbeddings != nil && cfg.NoteEmbeddings.Enabled }},
		{"code embeddings", func(root string) error {
			return SaveCodeEmbeddingsConfig(root, embeddings.Config{Enabled: true, Provider: "ollama"})
		}, func(cfg *LocalConfig) bool { return cfg.CodeEmbeddings != nil && cfg.CodeEmbeddings.Enabled }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, SaveLocalConfig(root, LocalConfig{Notes: LocalVaultConfig{Includes: []string{"**/*.md"}}}))
			read := make(chan struct{})
			resume := make(chan struct{})
			updated := make(chan error, 1)
			go func() {
				updated <- UpdateLocalConfig(context.Background(), root, func(cfg *LocalConfig) error {
					close(read)
					<-resume
					cfg.Notes.Includes = append(cfg.Notes.Includes, "model/*.yaml")
					return nil
				})
			}()
			<-read
			saved := make(chan error, 1)
			go func() { saved <- tc.save(root) }()
			select {
			case err := <-saved:
				close(resume)
				<-updated
				t.Fatalf("settings save passed an active configuration update: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			close(resume)
			require.NoError(t, <-updated)
			require.NoError(t, <-saved)
			final, err := LoadLocalConfig(root)
			require.NoError(t, err)
			require.Equal(t, []string{"**/*.md", "model/*.yaml"}, final.Notes.Includes)
			require.True(t, tc.check(final), "the requested settings change must also survive")
		})
	}
}

func TestGraphSettingsCanCreateRepositoryConfiguration(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, SaveGraphConfig(root, LocalGraphConfig{Ignore: []string{"private/**"}}))
	cfg, err := LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{"private/**"}, cfg.Graph.Ignore)
}

func TestUnchangedUpdatePreservesAuthoredConfiguration(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, ".rhizome/config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0755))
	const content = "notes:\n  includes: [\"model/*.{yaml,yml}\"] # keep this\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0644))
	require.NoError(t, UpdateLocalConfig(context.Background(), root, func(*LocalConfig) error { return nil }))
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, content, string(data))
}
