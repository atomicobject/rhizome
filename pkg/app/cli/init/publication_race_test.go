package init

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestSettingsApplyRejectsAConcurrentConfigurationChange(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "rhizome:\n  binaryManager: external\nnotes:\n  includes: [\"**/*.md\"]\nagents:\n  codex: off\n  claude: off\n  cursor: off\n")
	opts := RunOptions{Env: testkit.Env(t), Dir: root, Agents: "codex", Stdout: io.Discard, Stderr: io.Discard}
	r, s, _, err := startMachineRun(opts)
	require.NoError(t, err)
	plan, err := r.planSettings(s)
	require.NoError(t, err)
	read := make(chan struct{})
	resume := make(chan struct{})
	updated := make(chan error, 1)
	go func() {
		updated <- repoconfig.UpdateLocalConfig(context.Background(), root, func(cfg *repoconfig.LocalConfig) error {
			close(read)
			<-resume
			cfg.Notes.Includes = append(cfg.Notes.Includes, "model/schema.yaml")
			return nil
		})
	}()
	<-read
	applied := make(chan error, 1)
	go func() { _, err := r.apply(plan.setup, plan.res); applied <- err }()
	select {
	case err := <-applied:
		close(resume)
		<-updated
		t.Fatalf("settings publication passed an active update: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(resume)
	require.NoError(t, <-updated)
	require.ErrorContains(t, <-applied, "settings changed")
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Contains(t, cfg.Notes.Includes, "model/schema.yaml")
	_, err = Apply(opts)
	require.NoError(t, err, "a fresh Settings request uses the updated configuration")
	cfg, err = repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Contains(t, cfg.Notes.Includes, "model/schema.yaml")
}

func TestPreparedGuidancePreservesLaterEdits(t *testing.T) {
	root := t.TempDir()
	cfg := repoconfig.LocalConfig{Notes: repoconfig.LocalVaultConfig{Includes: []string{"**/*.md"}}}
	section, err := renderRhizomeMd(cfg)
	require.NoError(t, err)
	const originalProse = "# Preferences\nOriginal prose.\n\n"
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		writeFixtureFile(t, root, name, originalProse+buildManagedRhizomeBlock(section))
	}
	cfg.Notes.Includes = append(cfg.Notes.Includes, "model/schema.yaml")
	prepared, err := prepareModuleGuidance(root, cfg)
	require.NoError(t, err)
	const editedProse = "# Preferences\nAn edit made after preparation.\n\n"
	writeFixtureFile(t, root, "CLAUDE.md", editedProse+buildManagedRhizomeBlock(section))
	_, err = prepared.apply(nil)
	require.ErrorContains(t, err, "CLAUDE.md changed")
	for name, expected := range map[string]string{
		"AGENTS.md": originalProse + buildManagedRhizomeBlock(section),
		"CLAUDE.md": editedProse + buildManagedRhizomeBlock(section),
	} {
		actual, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, expected, string(actual))
	}
	require.NoFileExists(t, generatedFilesPath(root))
}
