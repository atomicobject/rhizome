package cmd

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRepoDelegationRequiresTrustBeforeAnyPinnedBinaryProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script probe fixture is Unix-only")
	}
	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	sentinel := filepath.Join(t.TempDir(), "repo-binary-ran")
	target := filepath.Join(repo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\ntouch \""+sentinel+"\"\necho 'rzm version v1.2.3'\n"), 0o755))
	store := repositorytrust.Store{Dir: filepath.Join(t.TempDir(), "trust")}
	exe := filepath.Join(t.TempDir(), "global-rzm")

	for _, args := range [][]string{
		nil,
		{"--version"},
		{"--help"},
		{"search", "hello"},
		{"update"},
		{"update", "--pinned"},
		{"update", "--set-version", "v1.2.3"},
		{"update", "--latest"},
		{"update", "--help"},
	} {
		decision := repoDelegateDecisionForTrust(args, repo, exe, store, nil)
		require.ErrorContains(t, decision.Err, "rzm trust", args)
		require.False(t, decision.Delegate, args)
		require.False(t, decision.Bootstrap, args)
		require.NoFileExists(t, sentinel, args)
	}

	_, err := store.Trust(repo)
	require.NoError(t, err)
	update := repoDelegateDecisionForTrust([]string{"update", "--pinned"}, repo, exe, store, nil)
	require.NoError(t, update.Err)
	require.False(t, update.Delegate, "trusted update remains on the global executable")
	require.False(t, update.Bootstrap, "the update command owns its download and smoke test")
	require.NoFileExists(t, sentinel, "dispatch must not probe the existing binary before update")
	decision := repoDelegateDecisionForTrust([]string{"--version"}, repo, exe, store, nil)
	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	require.FileExists(t, sentinel)
	requireSameExecutablePath(t, target, decision.Target)
}

func TestRepoDelegationTrustPromptGrantsOnceBeforeHandoff(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	target := writeRepoBinary(t, repo)
	writeVersionMarkerFile(t, target, "v1.2.3")
	store := repositorytrust.Store{Dir: filepath.Join(t.TempDir(), "trust")}
	prompts := 0
	confirm := func(checkout string) (bool, error) {
		prompts++
		expected, err := repositorytrust.CanonicalCheckout(repo)
		require.NoError(t, err)
		actual, err := repositorytrust.CanonicalCheckout(checkout)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
		return true, nil
	}

	decision := repoDelegateDecisionForTrust([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"), store, confirm)
	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	require.Equal(t, 1, prompts)

	decision = repoDelegateDecisionForTrust([]string{"search", "again"}, repo, filepath.Join(t.TempDir(), "rzm"), store, confirm)
	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	require.Equal(t, 1, prompts)
}

func TestRepoDelegationTrustControlAndDirectExecutionBypassTrust(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	target := writeRepoBinary(t, repo)
	writeVersionMarkerFile(t, target, "v1.2.3")
	store := repositorytrust.Store{Dir: filepath.Join(t.TempDir(), "trust")}

	for _, args := range [][]string{{"trust"}, {"untrust"}, {"desktop"}} {
		decision := repoDelegateDecisionForTrust(args, repo, filepath.Join(t.TempDir(), "global-rzm"), store, nil)
		require.NoError(t, decision.Err, args)
		require.False(t, decision.Delegate, args)
	}
	direct := repoDelegateDecisionForTrust([]string{"search", "hello"}, repo, target, store, nil)
	require.NoError(t, direct.Err)
	require.False(t, direct.Delegate)
}

func TestRepoDelegateDecisionFor_DelegatesNormalCommandToRepoBinary(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{
			Version: "v1.2.3",
		},
	}))
	target := writeRepoBinary(t, repo)
	writeVersionMarkerFile(t, target, "v1.2.3")

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	require.False(t, decision.Bootstrap)
	requireSameExecutablePath(t, target, decision.Target)
}

func TestRepoDelegateDecisionFor_UsesSharedPinnedBinaryPath(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	target := filepath.Join(repo, "tools", "rzm-custom")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755))
	writeVersionMarkerFile(t, target, "v1.2.3")
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3", BinaryPath: "tools/rzm-custom"},
	}))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	requireSameExecutablePath(t, target, decision.Target)
}

func TestRepoDelegateDecisionFor_BinaryPathWithoutPinDoesNotCreateRepoTarget(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{BinaryPath: "tools/rzm-custom"},
	}))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
	require.False(t, decision.Bootstrap)
}

func TestRepoDelegateDecisionFor_ExternalOwnershipAlwaysUsesCurrentExecutable(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{BinaryManager: obsidian.BinaryManagerExternal},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
	dev := filepath.Join(repo, "bin", runtime.GOOS, repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\n"), 0o755))

	for _, args := range [][]string{
		{"search", "hello"},
		{"init"},
		{"update", "--latest"},
		{"--version"},
	} {
		decision := repoDelegateDecisionFor(args, repo, filepath.Join(t.TempDir(), "external-rzm"))
		require.NoError(t, decision.Err)
		require.False(t, decision.Delegate, args)
		require.False(t, decision.Bootstrap, args)
		require.Empty(t, decision.Target, args)
	}
}

func TestRepoDelegateDecisionFor_DelegatesBeforePinnedBinaryValidatesOlderConfig(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	configPath := filepath.Join(repo, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte(`rhizome:
  version: v0.49.0
workflowTemplates:
  - spec-driven
workflowTemplateAddons:
  enabled:
    - action-items
workflowTemplateManagement:
  sourceFingerprints:
    spec-driven:docs:README.md: abc123
`), 0o644))
	target := writeRepoBinary(t, repo)
	writeVersionMarkerFile(t, target, "v0.49.0")

	decision := repoDelegateDecisionFor([]string{"index"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	requireSameExecutablePath(t, target, decision.Target)
	require.Contains(t, decision.Warning, "workflowTemplateAddons, workflowTemplateManagement, workflowTemplates")
}

func TestRepoDelegateDecisionFor_DelegatesBeforePinnedBinaryValidatesForeignFieldShapes(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	configPath := filepath.Join(repo, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("rhizome:\n  version: v9.9.9\nnotes: future-schema\n"), 0o644))
	target := writeRepoBinary(t, repo)
	writeVersionMarkerFile(t, target, "v9.9.9")

	decision := repoDelegateDecisionFor([]string{"index"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	requireSameExecutablePath(t, target, decision.Target)
	require.Contains(t, decision.Warning, "could not validate repo config")
}

func TestRepoDelegateDecisionFor_RepoBinaryStillReportsConfigWarnings(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	configPath := filepath.Join(repo, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("rhizome:\n  version: v0.50.0\nfutureConfig: true\n"), 0o644))
	target := writeRepoBinary(t, repo)

	decision := repoDelegateDecisionFor([]string{"index"}, repo, target)

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
	require.Contains(t, decision.Warning, "does not recognize config key(s) futureConfig")
	require.NotContains(t, decision.Warning, "will be ignored")
	require.NotContains(t, decision.Warning, "run `rzm init`")
}

func TestShouldPrintRepoDelegateWarningKeepsAgentOutputJSONSafe(t *testing.T) {
	t.Parallel()

	require.False(t, shouldPrintRepoDelegateWarning([]string{"agent", "start"}, "warning"))
	require.False(t, shouldPrintRepoDelegateWarning([]string{"--no-pager", "agent", "start"}, "warning"))
	require.True(t, shouldPrintRepoDelegateWarning([]string{"index"}, "warning"))
	require.False(t, shouldPrintRepoDelegateWarning([]string{"index"}, ""))
}

func TestRepoDelegateDecisionFor_PrefersRhizomeDevBuild(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	pinned := filepath.Join(repo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(pinned), 0o755))
	require.NoError(t, os.WriteFile(pinned, []byte("#!/bin/sh\n"), 0o755))
	dev := filepath.Join(repo, "bin", runtime.GOOS, repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\n"), 0o755))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	require.False(t, decision.Bootstrap)
	requireSameExecutablePath(t, dev, decision.Target)

	versionDecision := repoDelegateDecisionFor([]string{"--version"}, repo, filepath.Join(t.TempDir(), "rzm"))
	require.NoError(t, versionDecision.Err)
	require.True(t, versionDecision.Delegate)
	require.False(t, versionDecision.Bootstrap)
	requireSameExecutablePath(t, dev, versionDecision.Target)

	for _, args := range [][]string{
		{"init"},
		{"help"},
		{"completion", "zsh"},
		{"update", "--pinned"},
	} {
		t.Run(args[0], func(t *testing.T) {
			decision := repoDelegateDecisionFor(args, repo, filepath.Join(t.TempDir(), "rzm"))
			require.NoError(t, decision.Err)
			require.True(t, decision.Delegate)
			requireSameExecutablePath(t, dev, decision.Target)
		})
	}
}

func TestRepoDelegateDecisionFor_ConfiguredDevBinaryDirDoesNotNeedVersionPin(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{DevBinaryDir: "bin"},
	}))
	dev := filepath.Join(repo, "bin", runtime.GOOS, repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\n"), 0o755))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	require.False(t, decision.Bootstrap)
	requireSameExecutablePath(t, dev, decision.Target)
}

func TestRepoDelegateDecisionFor_ConfiguredDevBinaryDirMissingFails(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{DevBinaryDir: "bin"},
	}))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.Error(t, decision.Err)
	require.Contains(t, decision.Err.Error(), "configured rhizome.devBinaryDir target missing")
	require.False(t, decision.Delegate)
	require.False(t, decision.Bootstrap)
}

func TestRepoDelegateDecisionFor_ConfiguredDevBinaryDirSkipsWhenAlreadyTarget(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{DevBinaryDir: "bin"},
	}))
	dev := filepath.Join(repo, "bin", runtime.GOOS, repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\n"), 0o755))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, dev)

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
	require.False(t, decision.Bootstrap)
}

func TestRepoDelegateDecisionFor_DelegatesNewWorktreeInRhizomeSourceToDevBuild(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	dev := filepath.Join(repo, "bin", runtime.GOOS, repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\n"), 0o755))

	decision := repoDelegateDecisionFor([]string{"new-worktree", "/source/repo"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	requireSameExecutablePath(t, dev, decision.Target)
	require.False(t, decision.Bootstrap)
}

func TestRepoDelegateDecisionFor_SkipsNewWorktreeInRhizomeSourceWithoutDevBuild(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))

	decision := repoDelegateDecisionFor([]string{"new-worktree", "/source/repo"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
	require.False(t, decision.Bootstrap)
}

func TestRepoDelegateDecisionFor_BootstrapsNewWorktreeInPinnedRepo(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{
			Version: "v1.2.3",
		},
	}))

	decision := repoDelegateDecisionFor([]string{"new-worktree", "/source/repo"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
	require.True(t, decision.Bootstrap)
	require.Equal(t, "v1.2.3", decision.PinnedVersion)
	require.Equal(t, bootstrapReasonMissing, decision.Reason)
}

func TestRepoDelegateDecisionFor_SkipsDelegationForGoRunSourceInvocation(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	dev := filepath.Join(repo, "bin", runtime.GOOS, repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\n"), 0o755))
	goRunExe := filepath.Join(t.TempDir(), "go-build1234", "b001", "exe", repoExecutableName())

	decision := repoDelegateDecisionFor([]string{"agent", "validate"}, repo, goRunExe)

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)

	customCacheExe := filepath.Join(t.TempDir(), "rhizome-go-cache", "go-build", "12", "1234-a")
	decision = repoDelegateDecisionFor([]string{"agent", "validate"}, repo, customCacheExe)
	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
}

func TestRepoDelegationBlockedByEnv(t *testing.T) {
	t.Run("legacy delegate name is ignored", func(t *testing.T) {
		t.Setenv("RZM_REPO_DELEGATE", "1")
		t.Setenv(repoDelegatedEnv, "")
		t.Setenv(repoSkipDelegateEnv, "")

		require.False(t, repoDelegationBlockedByEnv())
	})

	t.Run("explicit skip disables delegation", func(t *testing.T) {
		t.Setenv(repoSkipDelegateEnv, "1")
		t.Setenv(repoDelegatedEnv, "")

		require.True(t, repoDelegationBlockedByEnv())
	})

	t.Run("delegated child disables redelegation", func(t *testing.T) {
		t.Setenv(repoSkipDelegateEnv, "")
		t.Setenv(repoDelegatedEnv, "1")

		require.True(t, repoDelegationBlockedByEnv())
	})
}

func TestFindRhizomeSourceRootPreservesCallerPath(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
	nested := filepath.Join(repo, "docs", "specs")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	root, ok := findRhizomeSourceRoot(nested)

	require.True(t, ok)
	require.Equal(t, repo, root)
}

func TestSameCleanPathTreatsSameDirectoryAsEqual(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	alias := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(repo, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	require.True(t, sameCleanPath(repo, alias))
}

func TestRepoDelegateDecisionFor_UsesPinnedWhenRhizomeDevBuildMissing(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	pinned := writeRepoBinary(t, repo)
	writeVersionMarkerFile(t, pinned, "v1.2.3")

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	require.False(t, decision.Bootstrap)
	requireSameExecutablePath(t, pinned, decision.Target)
}

func TestRepoDelegateDecisionFor_SkipsWhenAlreadyRepoBinary(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{
			Version: "v1.2.3",
		},
	}))
	target := filepath.Join(repo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, target)

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
}

func TestRepoDelegateDecisionFor_OnlyUpdateStaysOnGlobalControlPlane(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{
			Version: "v1.2.3",
		},
	}))
	target := writeRepoBinary(t, repo)
	writeVersionMarkerFile(t, target, "v1.2.3")

	for _, tc := range []struct {
		name     string
		args     []string
		delegate bool
	}{
		{name: "update", args: []string{"update", "--pinned"}},
		{name: "update help", args: []string{"update", "--help"}},
		{name: "bare invocation", args: nil, delegate: true},
		{name: "init", args: []string{"init"}, delegate: true},
		{name: "help", args: []string{"help"}, delegate: true},
		{name: "help update", args: []string{"help", "update"}, delegate: true},
		{name: "completion", args: []string{"completion", "zsh"}, delegate: true},
		{name: "version", args: []string{"--version"}, delegate: true},
		{name: "long help", args: []string{"--help"}, delegate: true},
		{name: "short help", args: []string{"-h"}, delegate: true},
		{name: "index", args: []string{"index"}, delegate: true},
		{name: "ordinary command", args: []string{"search", "hello"}, delegate: true},
		{name: "future command", args: []string{"command-added-by-the-pin"}, delegate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := repoDelegateDecisionFor(tc.args, repo, filepath.Join(t.TempDir(), "rzm"))
			require.NoError(t, decision.Err)
			require.Equal(t, tc.delegate, decision.Delegate)
			require.False(t, decision.Bootstrap)
			if tc.delegate {
				requireSameExecutablePath(t, target, decision.Target)
			}
		})
	}
}

func TestRepoDelegateDecisionFor_ExplicitExternalMigrationStaysOnCurrentExecutable(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	target := writeRepoBinary(t, repo)
	writeVersionMarkerFile(t, target, "v1.2.3")

	for _, args := range [][]string{
		{"init", "--binary-manager", "external"},
		{"init", "--binary-manager=external", "--workflow", "none"},
	} {
		decision := repoDelegateDecisionFor(args, repo, filepath.Join(t.TempDir(), "global-rzm"))
		require.NoError(t, decision.Err)
		require.False(t, decision.Delegate, args)
		require.False(t, decision.Bootstrap, args)
		require.Empty(t, decision.Target, args)
	}
}

func TestExecute_ExplicitExternalMigrationBypassesPinnedChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	sentinel := filepath.Join(repo, "pinned-child-ran")
	target := writeRepoBinary(t, repo)
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\ntouch \""+sentinel+"\"\nexit 42\n"), 0o755))
	writeVersionMarkerFile(t, target, "v1.2.3")

	cmd := exec.Command(os.Args[0], "-test.run=^TestExecuteExplicitExternalMigrationHelper$")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"RZM_TEST_EXECUTE_EXTERNAL_MIGRATION=1",
		repoDelegatedEnv+"=",
		repoSkipDelegateEnv+"=",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.NoFileExists(t, sentinel, "migration delegated to the old pinned child")

	cfg, err := obsidian.LoadLocalConfig(repo)
	require.NoError(t, err)
	require.Equal(t, obsidian.BinaryManagerExternal, cfg.Rhizome.BinaryManager)
	require.Empty(t, cfg.Rhizome.Version)
}

func TestExecuteExplicitExternalMigrationHelper(t *testing.T) {
	if os.Getenv("RZM_TEST_EXECUTE_EXTERNAL_MIGRATION") != "1" {
		return
	}
	os.Args = []string{
		"rzm", "init", "--binary-manager", "external", "--workflow", "none", "--agents", "none",
	}
	os.Exit(Execute())
}

func TestFirstCommandArgRecognizesUpdateAfterPersistentFlags(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "vault long", args: []string{"--vault", "docs", "update"}},
		{name: "vault short", args: []string{"-v", "docs", "update"}},
		{name: "manifest url", args: []string{"--manifest-url", "https://example.test/manifest.json", "update"}},
		{name: "set version", args: []string{"--set-version", "v1.2.3", "update"}},
		{name: "vault equals", args: []string{"--vault=docs", "update"}},
		{name: "manifest equals", args: []string{"--manifest-url=https://example.test/manifest.json", "update"}},
		{name: "set version equals", args: []string{"--set-version=v1.2.3", "update"}},
		{name: "no pager boolean", args: []string{"--no-pager", "update"}},
		{name: "latest boolean", args: []string{"--latest", "update"}},
		{name: "pinned boolean", args: []string{"--pinned", "update"}},
		{name: "help boolean", args: []string{"--help", "update"}},
		{name: "short help boolean", args: []string{"-h", "update"}},
		{name: "version boolean", args: []string{"--version", "update"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, "update", firstCommandArg(tc.args))
			require.True(t, isUpdateControlPlaneInvocation(tc.args))
		})
	}
}

func TestRepoDelegateDecisionFor_InitRunsPinnedChild(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	sentinel := filepath.Join(t.TempDir(), "pinned-init-ran")
	target := filepath.Join(repo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	script := "#!/bin/sh\n" +
		"test \"$RZM_REPO_DELEGATED\" = \"1\" || exit 9\n" +
		"test \"$1\" = \"init\" || exit 8\n" +
		"touch \"" + sentinel + "\"\n" +
		"exit 17\n"
	require.NoError(t, os.WriteFile(target, []byte(script), 0o755))
	writeVersionMarkerFile(t, target, "v1.2.3")

	decision := repoDelegateDecisionFor([]string{"init"}, repo, filepath.Join(t.TempDir(), "global-rzm"))

	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	requireSameExecutablePath(t, target, decision.Target)
	require.Equal(t, 17, runRepoBinary(decision.Target, []string{"init"}))
	_, err := os.Stat(sentinel)
	require.NoError(t, err)
}

func TestRepoDelegateDecisionFor_BootstrapsWhenRepoBinaryMissing(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{
			Version: "v1.2.3",
		},
	}))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
	require.True(t, decision.Bootstrap)
	require.Equal(t, "v1.2.3", decision.PinnedVersion)
	require.Equal(t, "binary missing", decision.Reason)
	// The target does not exist yet, so resolve repo symlinks (e.g. macOS
	// /var -> /private/var) before comparing paths.
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	target := filepath.Join(resolvedRepo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
	require.Equal(t, target, decision.Target)
}

func TestRepoDelegateDecisionFor_VersionMarkerDecisions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pin        string
		marker     string
		bootstrap  bool
		reason     string
		pinnedWant string
	}{
		{name: "marker matches pin", pin: "v1.2.3", marker: "v1.2.3", bootstrap: false},
		{name: "marker matches unprefixed pin", pin: "1.2.3", marker: "v1.2.3", bootstrap: false},
		{name: "marker mismatches pin", pin: "v1.2.3", marker: "v1.0.0", bootstrap: true, reason: "version mismatch", pinnedWant: "v1.2.3"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := t.TempDir()
			require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
				Rhizome: obsidian.LocalRhizomeConfig{Version: tc.pin},
			}))
			target := writeRepoBinary(t, repo)
			writeVersionMarkerFile(t, target, tc.marker)

			decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

			require.NoError(t, decision.Err)
			if tc.bootstrap {
				require.False(t, decision.Delegate)
				require.True(t, decision.Bootstrap)
				require.Equal(t, tc.reason, decision.Reason)
				require.Equal(t, tc.pinnedWant, decision.PinnedVersion)
			} else {
				require.True(t, decision.Delegate)
				require.False(t, decision.Bootstrap)
				requireSameExecutablePath(t, target, decision.Target)
			}
		})
	}
}

func TestRepoDelegateDecisionFor_ProbesBinaryWhenMarkerMissing(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell-script probe fixture is Unix-only")
	}

	cases := []struct {
		name         string
		script       string
		bootstrap    bool
		markerWant   string
		expectMarker bool
	}{
		{
			name: "probe reports matching version",
			script: `#!/bin/sh
if [ "$RZM_REPO_DELEGATED" != "1" ]; then
  exit 9
fi
echo "rzm version v1.2.3"
`,
			bootstrap:    false,
			markerWant:   "v1.2.3\n",
			expectMarker: true,
		},
		{
			name: "probe reports mismatched version",
			script: `#!/bin/sh
echo "rzm version v9.9.9"
`,
			bootstrap:    true,
			markerWant:   "v9.9.9\n",
			expectMarker: true,
		},
		{
			name: "probe fails",
			script: `#!/bin/sh
exit 3
`,
			bootstrap:    true,
			expectMarker: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := t.TempDir()
			require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
				Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
			}))
			target := filepath.Join(repo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
			require.NoError(t, os.WriteFile(target, []byte(tc.script), 0o755))

			decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

			require.NoError(t, decision.Err)
			if tc.bootstrap {
				require.True(t, decision.Bootstrap)
				require.False(t, decision.Delegate)
				require.Equal(t, "version mismatch", decision.Reason)
				require.Equal(t, "v1.2.3", decision.PinnedVersion)
			} else {
				require.True(t, decision.Delegate)
				require.False(t, decision.Bootstrap)
			}

			markerPath := filepath.Join(filepath.Dir(target), ".version")
			if tc.expectMarker {
				data, err := os.ReadFile(markerPath)
				require.NoError(t, err)
				require.Equal(t, tc.markerWant, string(data))
			} else {
				_, err := os.Stat(markerPath)
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}

func TestRepoDelegateDecisionFor_SkipsWithoutRepoVersion(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{},
	}))

	decision := repoDelegateDecisionFor([]string{"search", "hello"}, repo, filepath.Join(t.TempDir(), "rzm"))

	require.NoError(t, decision.Err)
	require.False(t, decision.Delegate)
}

func TestRunRepoBinaryReturnsChildExitCodeAndSetsGuard(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "rzm")
	require.NoError(t, os.WriteFile(target, []byte(`#!/bin/sh
if [ "$RZM_REPO_DELEGATED" != "1" ]; then
  exit 9
fi
if [ "$1" != "search" ]; then
  exit 8
fi
exit 7
`), 0o755))

	require.Equal(t, 7, runRepoBinary(target, []string{"search"}))
}

func TestRunRepoBinaryPreservesArgvAndStandardStreams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "rzm")
	require.NoError(t, os.WriteFile(target, []byte(`#!/bin/sh
test "$RZM_REPO_DELEGATED" = "1" || exit 9
test "$1" = "init" || exit 8
test "$2" = "--yes" || exit 7
IFS= read -r line
printf 'stdout:%s\n' "$line"
printf 'stderr:%s\n' "$line" >&2
exit 23
`), 0o755))

	stdinReader, stdinWriter, err := os.Pipe()
	require.NoError(t, err)
	stdoutReader, stdoutWriter, err := os.Pipe()
	require.NoError(t, err)
	stderrReader, stderrWriter, err := os.Pipe()
	require.NoError(t, err)
	originalStdin, originalStdout, originalStderr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = stdinReader, stdoutWriter, stderrWriter
	t.Cleanup(func() {
		os.Stdin, os.Stdout, os.Stderr = originalStdin, originalStdout, originalStderr
	})
	_, err = stdinWriter.WriteString("template-input\n")
	require.NoError(t, err)
	require.NoError(t, stdinWriter.Close())

	exitCode := runRepoBinary(target, []string{"init", "--yes"})
	require.NoError(t, stdoutWriter.Close())
	require.NoError(t, stderrWriter.Close())
	stdout, err := io.ReadAll(stdoutReader)
	require.NoError(t, err)
	stderr, err := io.ReadAll(stderrReader)
	require.NoError(t, err)

	require.Equal(t, 23, exitCode)
	require.Equal(t, "stdout:template-input\n", string(stdout))
	require.Equal(t, "stderr:template-input\n", string(stderr))
}

func writeRepoBinary(t *testing.T, repo string) string {
	t.Helper()
	target := filepath.Join(repo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755))
	return target
}

func writeVersionMarkerFile(t *testing.T, binary, version string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(binary), ".version"), []byte(version+"\n"), 0o644))
}

func requireSameExecutablePath(t *testing.T, expected, actual string) {
	t.Helper()
	expectedAbs, err := filepath.Abs(expected)
	require.NoError(t, err)
	actualAbs, err := filepath.Abs(actual)
	require.NoError(t, err)
	require.True(t, sameExecutablePath(expectedAbs, actualAbs), "expected %q and actual %q to resolve to the same executable", expectedAbs, actualAbs)
}
