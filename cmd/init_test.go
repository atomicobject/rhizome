package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestInitCommand_AdvertisesWorkflowChoices(t *testing.T) {
	flag := initCmd.Flags().Lookup("workflow")
	require.NotNil(t, flag)
	require.Contains(t, flag.Usage, "agentic-engineering, domain, or none")
	require.Nil(t, initCmd.Flags().Lookup("template"))
}

func TestInitCommand_ExplainsRemovedFlags(t *testing.T) {
	t.Setenv(repoSkipDelegateEnv, "1")
	for flag, want := range map[string]string{
		"--yes":            "--yes was removed",
		"--template=core":  "use --workflow",
		"--agentsmd=on":    "use --agents",
		"--reject-all":     "remembers what you keep",
		"--eject-template": "use --eject",
	} {
		rootCmd.SetArgs([]string{"init", "--path", t.TempDir(), flag})
		err := rootCmd.Execute()
		rootCmd.SetArgs([]string{})
		require.ErrorContains(t, err, want, flag)
	}
}

func TestInitCommand_CheckExitsOneAndWritesNothing(t *testing.T) {
	t.Setenv(repoSkipDelegateEnv, "1")
	dir := t.TempDir()
	defer func() { initCheck, initPath = false, "" }()

	rootCmd.SetArgs([]string{"init", "--path", dir, "--check"})
	err := rootCmd.Execute()
	rootCmd.SetArgs([]string{})

	var exit silentExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 1, exit.ExitCode())
	require.NoDirExists(t, filepath.Join(dir, ".rhizome"))
}

func TestInitCommand_WritesConfig(t *testing.T) {
	t.Setenv(repoSkipDelegateEnv, "1")
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Docs"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
	seedCurrentPinnedBinary(t, dir)

	origPath := initPath
	defer func() { initPath = origPath }()

	rootCmd.SetArgs([]string{"init", "--path", dir})
	err := rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	configPath := filepath.Join(dir, ".rhizome", "config.yml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var cfg struct {
		Notes struct {
			Includes []string `yaml:"includes"`
		} `yaml:"notes"`
		Code struct {
			Enabled bool     `yaml:"enabled"`
			Scan    []string `yaml:"scan"`
		} `yaml:"code"`
		WorkflowTemplates []string `yaml:"workflowTemplates"`
	}
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	require.Contains(t, cfg.Notes.Includes, "**/*.md", "notes cover all Markdown")
	require.True(t, cfg.Code.Enabled)

	ignorePath := filepath.Join(dir, ".rhizome", "ignore")
	_, err = os.Stat(ignorePath)
	require.NoError(t, err)

	gitignorePath := filepath.Join(dir, ".rhizome", ".gitignore")
	gitignore, err := os.ReadFile(gitignorePath)
	require.NoError(t, err)
	require.Contains(t, string(gitignore), "!.gitignore")
	require.Contains(t, string(gitignore), "!config.yml")
	require.Contains(t, string(gitignore), "!workflows.yml")
	require.Contains(t, string(gitignore), "!ignore")

	agentsPath := filepath.Join(dir, "AGENTS.md")
	agents, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	require.Contains(t, string(agents), "<!-- BEGIN RZM INIT RHIZOME BLOCK -->")
	require.Contains(t, string(agents), "# Rhizome integration")
	_, err = os.Stat(filepath.Join(dir, "RHIZOME.md"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestInitCommand_WritesExternalBinaryManager(t *testing.T) {
	t.Setenv(repoSkipDelegateEnv, "1")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644))

	origPath, origBinaryManager := initPath, initBinaryManager
	t.Cleanup(func() { initPath, initBinaryManager = origPath, origBinaryManager })

	rootCmd.SetArgs([]string{"init", "--path", dir, "--binary-manager", "external"})
	err := rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".rhizome", "config.yml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "binaryManager: external")
	require.NotContains(t, string(data), "version:")
	require.NoDirExists(t, filepath.Join(dir, ".rhizome", "bin"))
}

func seedCurrentPinnedBinary(t *testing.T, dir string) {
	t.Helper()
	exe := "rzm"
	if runtime.GOOS == "windows" {
		exe = "rzm.exe"
	}
	target := filepath.Join(dir, ".rhizome", "bin", appupdate.CurrentPlatformDir(), exe)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("test binary"), 0o755))
	require.NoError(t, appupdate.WriteVersionMarker(target, version.Version))
}

func TestInitCommand_RerunKeepsConfigAndUpdatesGeneratedFiles(t *testing.T) {
	t.Setenv(repoSkipDelegateEnv, "1")
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Docs"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))

	// Create an existing config to ensure it's not modified
	rhizomeDir := filepath.Join(dir, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	existingConfig := `rhizome:
  version: ` + version.Version + `
notes:
  includes:
    - "docs/**/*.md"
code:
  enabled: true
agents:
  agentSkills: on
  agentsmd: on
`
	configPath := filepath.Join(rhizomeDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(existingConfig), 0o644))

	seedCurrentPinnedBinary(t, dir)
	origPath := initPath
	defer func() { initPath = origPath }()

	rootCmd.SetArgs([]string{"init", "--path", dir})
	err := rootCmd.Execute()
	rootCmd.SetArgs([]string{})
	require.NoError(t, err)

	// Config should be unchanged
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, existingConfig, string(data))

	// Managed .gitignore should still be refreshed.
	gitignorePath := filepath.Join(rhizomeDir, ".gitignore")
	gitignore, err := os.ReadFile(gitignorePath)
	require.NoError(t, err)
	require.Contains(t, string(gitignore), "!config.yml")
	require.Contains(t, string(gitignore), "!workflows.yml")
	require.Contains(t, string(gitignore), "!generated-files.yml")

	// But templates should be updated
	agentsPath := filepath.Join(dir, "AGENTS.md")
	agents, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	require.Contains(t, string(agents), "<!-- BEGIN RZM INIT RHIZOME BLOCK -->")
	require.Contains(t, string(agents), "# Rhizome integration")
	_, err = os.Stat(filepath.Join(dir, "RHIZOME.md"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
