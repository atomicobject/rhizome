package obsidian

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadLocalConfig(t *testing.T) {
	t.Run("round trips canonical nested config without mutation", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		content := []byte(`notes:
  includes:
    - "docs/**/*.md"
    - "src/**/*.md"
  excludes:
    - "**/private/**"
  links: markdown
code:
  enabled: true
  python:
    roots:
      - src
`)
		require.NoError(t, os.WriteFile(configPath, content, 0o644))
		wantTime := time.Unix(1_700_000_000, 0)
		require.NoError(t, os.Chtimes(configPath, wantTime, wantTime))

		cfg, err := LoadLocalConfig(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"docs/**/*.md", "src/**/*.md"}, cfg.Notes.Includes)
		assert.Equal(t, []string{"**/private/**"}, cfg.Notes.Excludes)
		assert.Equal(t, "markdown", cfg.Notes.Links)
		require.NotNil(t, cfg.Code.Python)
		assert.Equal(t, []string{"src"}, cfg.Code.Python.Roots)

		after, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, content, after)
		info, err := os.Stat(configPath)
		require.NoError(t, err)
		assert.Equal(t, wantTime, info.ModTime())
	})

	t.Run("loads workflow state from separate file", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		workflowPath := filepath.Join(dir, ".rhizome", "workflows.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
		require.NoError(t, os.WriteFile(workflowPath, []byte(`templates:
  - spec-driven
addons:
  enabled:
    - action-items
management:
  ejected:
    - project-kb
  updatePolicy:
    docs: never
`), 0o644))

		cfg, err := LoadLocalConfig(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"spec-driven"}, cfg.WorkflowTemplates)
		assert.Equal(t, []string{"action-items"}, cfg.WorkflowTemplateAddons.Enabled)
		assert.Equal(t, []string{"project-kb"}, cfg.WorkflowTemplateManagement.Ejected)
		assert.Equal(t, "never", cfg.WorkflowTemplateManagement.UpdatePolicy.Docs)
	})

	t.Run("warns for unknown and retired keys without mutating", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		before := []byte("vault:\n  root: docs\nagent: {}\nunknownSetting: true\n")
		require.NoError(t, os.WriteFile(configPath, before, 0o644))

		cfg, err := LoadLocalConfig(dir)
		require.NoError(t, err)
		require.Len(t, cfg.Warnings, 1)
		assert.Equal(t, filepath.Join(paths.ResolveSymlinks(dir).String(), ".rhizome", "config.yml"), cfg.Warnings[0].Path)
		assert.Equal(t, []string{"agent", "unknownSetting", "vault"}, cfg.Warnings[0].OffendingKeys)
		assert.Equal(t, LocalConfigDocsHint, cfg.Warnings[0].DocsHint)
		after, readErr := os.ReadFile(configPath)
		require.NoError(t, readErr)
		assert.Equal(t, before, after)
	})

	t.Run("warns for retired leaderFollower without mutation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		before := []byte("notes: {}\nleaderFollower:\n  enabled: true\n  pollInterval: 2s\n")
		require.NoError(t, os.WriteFile(path, before, 0o644))
		cfg, err := LoadLocalConfig(dir)
		require.NoError(t, err)
		require.Len(t, cfg.Warnings, 1)
		assert.Equal(t, []string{"leaderFollower"}, cfg.Warnings[0].OffendingKeys)
		assert.Equal(t, LocalConfigDocsHint, cfg.Warnings[0].DocsHint)
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, before, after)
	})

	t.Run("keeps malformed YAML distinct from schema errors", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte("notes: [\n"), 0o644))

		_, err := LoadLocalConfig(dir)
		require.Error(t, err)
		var nonCanonical *NonCanonicalConfigError
		assert.False(t, errors.As(err, &nonCanonical))
	})

	t.Run("ignores removed legacy filenames", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".rhizome.yaml"), []byte("notes: {}\n"), 0o644))

		_, err := LoadLocalConfig(dir)
		assert.ErrorIs(t, err, ErrNoLocalConfig)
	})

	t.Run("returns error when no config found", func(t *testing.T) {
		dir := t.TempDir()
		_, err := LoadLocalConfig(dir)
		assert.ErrorIs(t, err, ErrNoLocalConfig)
	})
}

func TestSaveLocalConfigUsesTwoSpaceIndent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, SaveLocalConfig(dir, LocalConfig{
		Rhizome: LocalRhizomeConfig{Version: "v1.2.3"},
		Code: LocalCodeConfig{
			Enabled: true,
			Python:  &LocalCodeLangConfig{Roots: []string{"src"}},
		},
	}))

	data, err := os.ReadFile(filepath.Join(dir, ".rhizome", "config.yml"))
	require.NoError(t, err)
	body := string(data)
	assert.Contains(t, body, "rhizome:\n  version: v1.2.3\n")
	assert.Contains(t, body, "code:\n  enabled: true\n  python:\n    roots:\n      - src\n")
	assert.NotContains(t, body, "rhizome:\n    version:")
	assert.NotContains(t, body, "code:\n    enabled:")
}

func TestLoadLocalConfigValidatesBinaryManager(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantExternal bool
		wantErr      string
	}{
		{
			name:    "omitted manager keeps Rhizome ownership",
			content: "rhizome:\n  version: v1.2.3\n",
		},
		{
			name:         "external manager owns the binary",
			content:      "rhizome:\n  binaryManager: external\n",
			wantExternal: true,
		},
		{
			name:    "unknown manager is rejected",
			content: "rhizome:\n  binaryManager: other\n",
			wantErr: `unsupported rhizome.binaryManager "other"`,
		},
		{
			name:    "external manager conflicts with version",
			content: "rhizome:\n  binaryManager: external\n  version: v1.2.3\n",
			wantErr: "rhizome.version",
		},
		{
			name:    "external manager conflicts with development binary directory",
			content: "rhizome:\n  binaryManager: external\n  devBinaryDir: ./bin\n",
			wantErr: "rhizome.devBinaryDir",
		},
		{
			name:    "external manager conflicts with binary directory",
			content: "rhizome:\n  binaryManager: external\n  binaryDir: .rhizome/bin\n",
			wantErr: "rhizome.binaryDir",
		},
		{
			name:    "external manager conflicts with binary path",
			content: "rhizome:\n  binaryManager: external\n  binaryPath: tools/rzm\n",
			wantErr: "rhizome.binaryPath",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, ".rhizome", "config.yml")
			require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
			require.NoError(t, os.WriteFile(configPath, []byte(tt.content), 0o644))

			cfg, err := LoadLocalConfig(dir)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			external, err := cfg.Rhizome.UsesExternalBinaryManager()
			require.NoError(t, err)
			assert.Equal(t, tt.wantExternal, external)
		})
	}
}

func TestExternallyManagedLocalConfigRoundTripPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte(`rhizome:
  binaryManager: external
  futurePinMode: cautious
futureConfig:
  owner: user
`), 0o644))

	cfg, err := LoadLocalConfig(dir)
	require.NoError(t, err)
	require.Equal(t, BinaryManagerExternal, cfg.Rhizome.BinaryManager)
	require.Len(t, cfg.Warnings, 1)
	require.Equal(t, []string{"futureConfig", "futurePinMode"}, cfg.Warnings[0].OffendingKeys)
	require.NoError(t, SaveLocalConfig(dir, *cfg))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "binaryManager: external")
	assert.Contains(t, string(data), "futurePinMode: cautious")
	assert.Contains(t, string(data), "futureConfig:")
	assert.NotContains(t, string(data), "version:")
}

func TestSaveLocalConfigRejectsInvalidBinaryOwnershipBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	err := SaveLocalConfig(dir, LocalConfig{Rhizome: LocalRhizomeConfig{
		BinaryManager: BinaryManagerExternal,
		Version:       "v1.2.3",
	}})

	require.ErrorContains(t, err, "rhizome.version")
	_, statErr := os.Stat(filepath.Join(dir, ".rhizome"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestFindLocalConfigForDelegationValidatesBinaryManager(t *testing.T) {
	t.Run("loads external ownership without decoding unrelated config", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte(`rhizome:
  binaryManager: external
futureConfig:
  owner: user
`), 0o644))

		foundDir, cfg, err := FindLocalConfigForDelegation(dir)

		require.NoError(t, err)
		assert.Equal(t, paths.ResolveSymlinks(dir).String(), foundDir)
		assert.Equal(t, BinaryManagerExternal, cfg.Rhizome.BinaryManager)
	})

	t.Run("rejects mixed ownership", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte(`rhizome:
  binaryManager: external
  version: v1.2.3
`), 0o644))

		_, _, err := FindLocalConfigForDelegation(dir)

		require.ErrorContains(t, err, "rhizome.version")
	})
}

func TestLocalValidationConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := LocalConfig{
		Validation: LocalValidationConfig{
			Default: LocalValidationSuiteConfig{
				Add:  []string{"code-frontmatter", "companion-docs"},
				Skip: []string{"broken-links"},
			},
			All: LocalValidationSuiteConfig{
				Add:  []string{"audit-adoption"},
				Skip: []string{"query-recipes"},
			},
		},
	}

	require.NoError(t, SaveLocalConfig(dir, want))
	configPath := filepath.Join(dir, ".rhizome", "config.yml")
	first, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, `validation:
  default:
    add:
      - code-frontmatter
      - companion-docs
    skip:
      - broken-links
  all:
    add:
      - audit-adoption
    skip:
      - query-recipes
`, string(first))

	loaded, err := LoadLocalConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, want.Validation, loaded.Validation)

	require.NoError(t, SaveLocalConfig(dir, *loaded))
	second, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestLocalValidationConfigOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, SaveLocalConfig(dir, LocalConfig{}))

	data, err := os.ReadFile(filepath.Join(dir, ".rhizome", "config.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "validation:")
}

func TestLoadLocalConfigWarnsForUnknownValidationFields(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte(`validation:
  default:
    add: [ontology]
    retired: true
  all:
    mystery: []
`), 0o644))

	cfg, err := LoadLocalConfig(dir)
	require.NoError(t, err)
	require.Len(t, cfg.Warnings, 1)
	assert.Equal(t, []string{"mystery", "retired"}, cfg.Warnings[0].OffendingKeys)
}

func TestSaveLocalConfigPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	rhizomeDir := filepath.Join(dir, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte(`rhizome:
  version: v0.49.0
  futurePinMode: cautious
futureConfig:
  owner: user
workflowTemplates:
  - spec-driven
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "workflows.yml"), []byte(`templates:
  - spec-driven
management:
  futureManagementMode: cautious
futureWorkflow:
  owner: user
`), 0o644))

	cfg, err := LoadLocalConfig(dir)
	require.NoError(t, err)
	cfg.Rhizome.Version = "v0.50.1"
	cfg.WorkflowTemplateManagement.UpdatePolicy.Docs = "never"
	require.NoError(t, SaveLocalConfig(dir, *cfg))

	configData, err := os.ReadFile(filepath.Join(rhizomeDir, "config.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(configData), "futurePinMode: cautious")
	assert.Contains(t, string(configData), "futureConfig:")
	assert.Contains(t, string(configData), "workflowTemplates:\n  - spec-driven")
	assert.Contains(t, string(configData), "version: v0.50.1")
	workflowData, err := os.ReadFile(filepath.Join(rhizomeDir, "workflows.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(workflowData), "futureManagementMode: cautious")
	assert.Contains(t, string(workflowData), "futureWorkflow:")
	assert.Contains(t, string(workflowData), "docs: never")
}

func TestLoadAndSaveLocalConfigTreatsBlankDocumentsAsEmpty(t *testing.T) {
	for _, content := range []string{"", "  \n\t", "# intentionally empty\n"} {
		t.Run(fmt.Sprintf("content_%q", content), func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, ".rhizome", "config.yml")
			require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
			require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

			cfg, err := LoadLocalConfig(dir)
			require.NoError(t, err)
			require.Empty(t, cfg.Warnings)
			cfg.Rhizome.Version = "v0.50.1"
			require.NoError(t, SaveLocalConfig(dir, *cfg))

			data, err := os.ReadFile(configPath)
			require.NoError(t, err)
			assert.Contains(t, string(data), "version: v0.50.1")
		})
	}
}

func TestSaveLocalWorkflowConfigUsesTwoSpaceIndent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, SaveLocalWorkflowConfig(dir, LocalWorkflowConfig{
		Management: WorkflowTemplateManagement{
			UpdatePolicy: WorkflowTemplateUpdatePolicy{Docs: "never"},
		},
	}))

	data, err := os.ReadFile(filepath.Join(dir, ".rhizome", "workflows.yml"))
	require.NoError(t, err)
	body := string(data)
	assert.Contains(t, body, "management:\n  updatePolicy:\n    docs: never\n")
	assert.NotContains(t, body, "management:\n    updatePolicy:")
}

func TestConfigWritersPreserveExistingFileModes(t *testing.T) {
	dir := t.TempDir()
	rhizomeDir := filepath.Join(dir, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	configPath := filepath.Join(rhizomeDir, "config.yml")
	workflowPath := filepath.Join(rhizomeDir, "workflows.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("rhizome: {}\n"), 0o600))
	require.NoError(t, os.WriteFile(workflowPath, []byte("templates: []\n"), 0o600))

	require.NoError(t, SaveLocalConfig(dir, LocalConfig{
		Rhizome:           LocalRhizomeConfig{Version: "v1.2.3"},
		WorkflowTemplates: []string{"agentic-engineering"},
	}))

	if runtime.GOOS != "windows" {
		for _, path := range []string{configPath, workflowPath} {
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), path)
		}
	}
}

func TestLoadLocalWorkflowConfig(t *testing.T) {
	tests := []struct {
		name          string
		content       []byte
		wantExists    bool
		want          LocalWorkflowConfig
		wantUnknown   []string
		wantMalformed bool
		wantMultiDoc  bool
	}{
		{
			name: "loads canonical workflow state",
			content: []byte(`templates:
  - spec-driven
addons:
  enabled:
    - action-items
management:
  ejected:
    - project-kb
  updatePolicy:
    docs: never
`),
			wantExists: true,
			want: LocalWorkflowConfig{
				Templates: []string{"spec-driven"},
				Addons: WorkflowTemplateAddons{
					Enabled: []string{"action-items"},
				},
				Management: WorkflowTemplateManagement{
					Ejected:      []string{"project-kb"},
					UpdatePolicy: WorkflowTemplateUpdatePolicy{Docs: "never"},
				},
			},
		},
		{
			name:        "rejects unknown keys in sorted order",
			content:     []byte("zRetired: true\naddons:\n  retiredAddonKey: true\naUnknown: true\n"),
			wantExists:  true,
			wantUnknown: []string{"aUnknown", "retiredAddonKey", "zRetired"},
		},
		{
			name:          "keeps malformed YAML distinct",
			content:       []byte("templates: [\n"),
			wantExists:    true,
			wantMalformed: true,
		},
		{
			name:         "rejects a trailing YAML document",
			content:      []byte("templates: [spec-driven]\n---\ntemplates: [project-kb]\n"),
			wantExists:   true,
			wantMultiDoc: true,
		},
		{
			name: "returns missing without error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			workflowPath := filepath.Join(dir, ".rhizome", "workflows.yml")
			if tt.content != nil {
				require.NoError(t, os.MkdirAll(filepath.Dir(workflowPath), 0o755))
				require.NoError(t, os.WriteFile(workflowPath, tt.content, 0o644))
			}

			got, exists, err := LoadLocalWorkflowConfig(dir)
			assert.Equal(t, tt.wantExists, exists)
			switch {
			case len(tt.wantUnknown) > 0:
				require.NoError(t, err)
				require.Len(t, got.Warnings, 1)
				assert.Equal(t, workflowPath, got.Warnings[0].Path)
				assert.Equal(t, tt.wantUnknown, got.Warnings[0].OffendingKeys)
				assert.Equal(t, LocalConfigDocsHint, got.Warnings[0].DocsHint)
			case tt.wantMalformed:
				require.Error(t, err)
				var nonCanonical *NonCanonicalConfigError
				assert.False(t, errors.As(err, &nonCanonical))
			case tt.wantMultiDoc:
				require.EqualError(t, err, "repo-local config must contain exactly one YAML document")
			default:
				require.NoError(t, err)
				got.sourceYAML = nil
				assert.Equal(t, tt.want, got)
			}

			if tt.content == nil {
				_, statErr := os.Stat(workflowPath)
				assert.ErrorIs(t, statErr, os.ErrNotExist)
				return
			}
			after, readErr := os.ReadFile(workflowPath)
			require.NoError(t, readErr)
			assert.Equal(t, tt.content, after)
		})
	}
}

func TestFindLocalConfig(t *testing.T) {
	t.Run("finds config in current directory", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes:\n    - docs/**/*.md\n"), 0o644))

		foundDir, cfg, err := FindLocalConfig(dir)
		require.NoError(t, err)
		assert.Equal(t, paths.ResolveSymlinks(dir).String(), foundDir)
		assert.Equal(t, []string{"docs/**/*.md"}, cfg.Notes.Includes)
	})

	t.Run("finds config in parent directory", func(t *testing.T) {
		root := t.TempDir()
		subdir := filepath.Join(root, "sub", "deep")
		require.NoError(t, os.MkdirAll(subdir, 0o755))
		configPath := filepath.Join(root, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes:\n    - docs/**/*.md\n"), 0o644))

		foundDir, cfg, err := FindLocalConfig(subdir)
		require.NoError(t, err)
		assert.Equal(t, paths.ResolveSymlinks(root).String(), foundDir)
		assert.NotNil(t, cfg)
	})

	t.Run("returns error when no config in hierarchy", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
		dir := filepath.Join(root, "sub", "deep")
		require.NoError(t, os.MkdirAll(dir, 0o755))

		_, _, err := FindLocalConfig(dir)
		assert.ErrorIs(t, err, ErrNoLocalConfig)
	})
}

func TestLocalConfigToDefinition(t *testing.T) {
	t.Run("anchors notes to the config directory", func(t *testing.T) {
		configDir := filepath.FromSlash("/path/to/repo")
		cfg := &LocalConfig{
			Notes: LocalVaultConfig{
				Includes: []string{"docs/**/*.md"},
			},
		}

		def := LocalConfigToDefinition(configDir, cfg)
		assert.Equal(t, filepath.FromSlash("/path/to/repo"), def.Root)
		assert.Equal(t, []string{"docs/**/*.md"}, def.Includes)
	})

	t.Run("defaults includes to whole repo markdown", func(t *testing.T) {
		configDir := filepath.FromSlash("/path/to/repo")
		cfg := &LocalConfig{}

		def := LocalConfigToDefinition(configDir, cfg)
		assert.Equal(t, []string{"**/*.md"}, def.Includes)
		assert.Equal(t, filepath.FromSlash("/path/to/repo"), def.Root)
	})

	t.Run("preserves link type", func(t *testing.T) {
		configDir := filepath.FromSlash("/path/to/repo")
		cfg := &LocalConfig{
			Notes: LocalVaultConfig{
				Links: "markdown",
			},
		}

		def := LocalConfigToDefinition(configDir, cfg)
		assert.Equal(t, "markdown", def.Links)
	})
}

func TestLoadDefinitionFromPath(t *testing.T) {
	t.Run("loads definition and sets name from directory", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes:\n    - '**/*.md'\n"), 0o644))

		def, err := LoadDefinitionFromPath(dir)
		require.NoError(t, err)
		assert.Equal(t, filepath.Base(dir), def.Name)
		assert.Equal(t, paths.ResolveSymlinks(dir).String(), def.Root)
	})

	t.Run("returns error when no config", func(t *testing.T) {
		dir := t.TempDir()
		_, err := LoadDefinitionFromPath(dir)
		assert.ErrorIs(t, err, ErrNoLocalConfig)
	})
}

func TestHasLocalConfig(t *testing.T) {
	t.Run("returns true when .rhizome/config.yml exists", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes:\n    - docs/**/*.md\n"), 0o644))
		assert.True(t, HasLocalConfig(dir))
	})

	t.Run("returns false when only a removed legacy filename exists", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".rhizome.yaml"), []byte("vault:\n  root: .\n"), 0o644))
		assert.False(t, HasLocalConfig(dir))
	})

	t.Run("returns false when no config exists", func(t *testing.T) {
		dir := t.TempDir()
		assert.False(t, HasLocalConfig(dir))
	})
}

func TestVaultDefinitionFromPath(t *testing.T) {
	t.Run("resolves vault from path with local config", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rhizome", "config.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
		require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  links: markdown\n"), 0o644))

		vault := Vault{Name: dir}
		def, err := vault.Definition()
		require.NoError(t, err)
		assert.Equal(t, paths.ResolveSymlinks(dir).String(), def.Root)
		assert.Equal(t, "markdown", def.Links)
	})
}

func TestScopeConfigHash(t *testing.T) {
	t.Run("returns empty string for nil config", func(t *testing.T) {
		var cfg *LocalConfig
		assert.Equal(t, "", cfg.ScopeConfigHash())
	})

	t.Run("returns consistent hash for same config", func(t *testing.T) {
		cfg := &LocalConfig{
			Notes: LocalVaultConfig{
				Includes: []string{"**/*.md"},
				Excludes: []string{"vendor/**"},
			},
		}
		hash1 := cfg.ScopeConfigHash()
		hash2 := cfg.ScopeConfigHash()
		assert.NotEmpty(t, hash1)
		assert.Equal(t, hash1, hash2)
	})

	t.Run("hash changes when notes.includes changes", func(t *testing.T) {
		cfg1 := &LocalConfig{Notes: LocalVaultConfig{Includes: []string{"**/*.md"}}}
		cfg2 := &LocalConfig{Notes: LocalVaultConfig{Includes: []string{"docs/**/*.md"}}}
		assert.NotEqual(t, cfg1.ScopeConfigHash(), cfg2.ScopeConfigHash())
	})

	t.Run("hash changes when notes.excludes changes", func(t *testing.T) {
		cfg1 := &LocalConfig{Notes: LocalVaultConfig{Includes: []string{"**/*.md"}, Excludes: []string{"vendor/**"}}}
		cfg2 := &LocalConfig{Notes: LocalVaultConfig{Includes: []string{"**/*.md"}, Excludes: []string{"node_modules/**"}}}
		assert.NotEqual(t, cfg1.ScopeConfigHash(), cfg2.ScopeConfigHash())
	})

	t.Run("hash changes when code roots change", func(t *testing.T) {
		cfg1 := &LocalConfig{Code: LocalCodeConfig{Python: &LocalCodeLangConfig{Roots: []string{"src"}}}}
		cfg2 := &LocalConfig{Code: LocalCodeConfig{Python: &LocalCodeLangConfig{Roots: []string{"src", "lib"}}}}
		assert.NotEqual(t, cfg1.ScopeConfigHash(), cfg2.ScopeConfigHash())
	})

	t.Run("hash unchanged by non-scope fields", func(t *testing.T) {
		cfg1 := &LocalConfig{Notes: LocalVaultConfig{Includes: []string{"**/*.md"}}, BudgetChars: 100000}
		cfg2 := &LocalConfig{Notes: LocalVaultConfig{Includes: []string{"**/*.md"}}, BudgetChars: 200000}
		assert.Equal(t, cfg1.ScopeConfigHash(), cfg2.ScopeConfigHash())
	})

	t.Run("includes all language roots in hash", func(t *testing.T) {
		baseConfig := &LocalConfig{Code: LocalCodeConfig{Enabled: true}}
		baseHash := baseConfig.ScopeConfigHash()

		withPython := &LocalConfig{Code: LocalCodeConfig{Enabled: true, Python: &LocalCodeLangConfig{Roots: []string{"py"}}}}
		assert.NotEqual(t, baseHash, withPython.ScopeConfigHash())

		withGo := &LocalConfig{Code: LocalCodeConfig{Enabled: true, Go: &LocalCodeLangConfig{Roots: []string{"go"}}}}
		assert.NotEqual(t, baseHash, withGo.ScopeConfigHash())

		withTS := &LocalConfig{Code: LocalCodeConfig{Enabled: true, TypeScript: &LocalCodeLangConfig{Roots: []string{"ts"}}}}
		assert.NotEqual(t, baseHash, withTS.ScopeConfigHash())

		withJS := &LocalConfig{Code: LocalCodeConfig{Enabled: true, JavaScript: &LocalCodeLangConfig{Roots: []string{"js"}}}}
		assert.NotEqual(t, baseHash, withJS.ScopeConfigHash())

		withCS := &LocalConfig{Code: LocalCodeConfig{Enabled: true, CSharp: &LocalCodeLangConfig{Roots: []string{"cs"}}}}
		assert.NotEqual(t, baseHash, withCS.ScopeConfigHash())
	})
}
