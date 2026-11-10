package repoconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackgroundUpdateCannotOverwriteConcurrentSettingsSave(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, SaveLocalConfig(root, LocalConfig{Notes: LocalVaultConfig{Links: "wikilinks"}}))
	settings, err := LoadLocalConfig(root)
	require.NoError(t, err)
	settings.Notes.Links = "markdown"
	read := make(chan struct{})
	resume := make(chan struct{})
	updated := make(chan error, 1)
	go func() {
		updated <- UpdateLocalConfig(context.Background(), root, func(cfg *LocalConfig) error {
			close(read)
			<-resume
			cfg.Notes.Includes = []string{"model/*.yaml"}
			return nil
		})
	}()
	<-read
	saved := make(chan error, 1)
	go func() { saved <- SaveLocalConfig(root, *settings) }()
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
	require.Equal(t, "markdown", final.Notes.Links)
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
