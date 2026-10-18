package init

import (
	"bufio"
	"bytes"
	"errors"
	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/embeddings"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// firstRunRepo returns a fresh repository with a little code and docs and no
// provider keys or global config.
func firstRunRepo(t *testing.T) string {
	t.Helper()
	for _, key := range []string{"VOYAGE_API_KEY", "RHIZOME_VOYAGE_API_KEY", "OPENAI_API_KEY", "RHIZOME_OPENAI_API_KEY", "ATOMIC_RHIZOME_KEY"} {
		t.Setenv(key, "")
	}
	t.Setenv("RHIZOME_DISABLE_OLLAMA", "1")
	root := t.TempDir()
	writeFixtureFile(t, root, "go.mod", "module demo\n")
	writeFixtureFile(t, root, "cmd/app/main.go", "package main\nfunc main() {}\n")
	writeFixtureFile(t, root, "docs/a.md", "# A\n")
	writeFixtureFile(t, root, "docs/b.md", "# B\n")
	writeFixtureFile(t, root, "docs/c.md", "# C\n")
	return root
}

func runFirstRunForTest(t *testing.T, env vaultconfig.Env, root, input string) string {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, Run(RunOptions{Env: env, Dir: root, Interactive: true, Stdin: bytes.NewBufferString(input), Stdout: &out, Stderr: &out}))
	return out.String()
}

func TestFirstRunShowsFindingsAsksTheWorkflowAndWrites(t *testing.T) {
	root := firstRunRepo(t)

	// workflow default, set up search later, confirm.
	out := runFirstRunForTest(t, initTestEnv(t), root, "\n\n\n")

	require.Contains(t, out, "Set up Rhizome in "+filepath.Base(root))
	require.Contains(t, out, "all Markdown (3 notes in docs/)")
	require.Contains(t, out, "Go (1 file)")
	require.Contains(t, out, "Voyage AI (needs a key)")
	require.Contains(t, out, "1  Agentic Engineering (recommended)")
	require.Contains(t, out, "Paste your Voyage API Key.")
	require.NotContains(t, out, "Atomic Object", "source builds carry no team keys")
	require.Contains(t, out, "Set VOYAGE_API_KEY, then run rzm init, to turn on semantic search.")
	require.Contains(t, out, "✓ .rhizome/config.yml")
	require.Contains(t, out, "rzm index  build the search index")
	require.NotContains(t, out, "(new file)")
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, []string{templateAgenticEngineering}, cfg.WorkflowTemplates)
	require.Nil(t, cfg.NoteEmbeddings)
	require.FileExists(t, filepath.Join(root, ".agents", "skills", "agentic-engineering", "SKILL.md"))
}

func TestFirstRunWorkflowChoices(t *testing.T) {
	for choice, want := range map[string][]string{"2": {templateComplexDomain}, "3": nil} {
		root := firstRunRepo(t)
		runFirstRunForTest(t, initTestEnv(t), root, choice+"\n\n\n")
		cfg, err := repoconfig.LoadLocalConfig(root)
		require.NoError(t, err)
		require.Equal(t, want, cfg.WorkflowTemplates, choice)
	}
}

func TestFirstRunUsesAnAvailableKeyWithoutAsking(t *testing.T) {
	root := firstRunRepo(t)
	t.Setenv("VOYAGE_API_KEY", "pa-test")

	out := runFirstRunForTest(t, initTestEnv(t), root, "\n\n")

	require.NotContains(t, out, "Paste your")
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.NotNil(t, cfg.NoteEmbeddings)
	require.True(t, cfg.NoteEmbeddings.Enabled)
	require.Equal(t, "voyage", cfg.NoteEmbeddings.Provider)
	require.Nil(t, cfg.CodeEmbeddings, "code search follows the notes setting")
}

func TestFirstRunSavesAPastedKeyAndTurnsSearchOn(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	root := firstRunRepo(t)

	out := runFirstRunForTest(t, env, root, "\npa-pasted\n\n")

	require.Contains(t, out, "Voyage API Key saved to ~/.config/rhizome/config.yml")
	require.Equal(t, "pa-pasted", readCliConfigForTest(t, env).Env["VOYAGE_API_KEY"])
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.True(t, cfg.NoteEmbeddings.Enabled)
}

func TestFirstRunCanTurnSearchOffOnPurpose(t *testing.T) {
	root := firstRunRepo(t)

	out := runFirstRunForTest(t, initTestEnv(t), root, "\nother\n3\n\n")

	require.Contains(t, out, "Ollama, runs locally and free (not installed)")
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.NotNil(t, cfg.NoteEmbeddings, "off on purpose is recorded")
	require.False(t, cfg.NoteEmbeddings.Enabled)
}

func TestFirstRunCancelledAfterPastingAKeySaysTheKeyStays(t *testing.T) {
	root := firstRunRepo(t)

	out := runFirstRunForTest(t, initTestEnv(t), root, "\npa-pasted\nn\n")

	require.Contains(t, out, "Setup cancelled; nothing was written to this repository. The Voyage API Key you pasted stays saved")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}

func TestSearchChoiceKeepsSettingsForTheSameProvider(t *testing.T) {
	custom := &embeddings.Config{Provider: searchOllama, Endpoint: "http://gpu:11434", Model: "custom"}
	cfg := repoconfig.LocalConfig{NoteEmbeddings: custom}

	applySearchChoice(&cfg, searchOllama, true)
	require.Equal(t, &embeddings.Config{Enabled: true, Provider: searchOllama, Endpoint: "http://gpu:11434", Model: "custom"}, cfg.NoteEmbeddings)
	require.False(t, custom.Enabled, "the earlier config is not changed in place")

	applySearchChoice(&cfg, searchVoyage, true)
	require.Equal(t, &embeddings.Config{Enabled: true, Provider: searchVoyage}, cfg.NoteEmbeddings, "a new provider starts from its defaults")
}

func TestFirstRunAsksAgainAfterATypo(t *testing.T) {
	root := firstRunRepo(t)

	// workflow, skip the key, then a typo at the confirmation before n.
	out := runFirstRunForTest(t, initTestEnv(t), root, "\n\nk\nn\n")

	require.Contains(t, out, "Please answer y, n, or e.")
	require.Contains(t, out, "Setup cancelled")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"), "a typo never sets up")
}

func TestUnknownFilesQuestionTreatsATypoAsNoAnswer(t *testing.T) {
	var out bytes.Buffer
	ui := newTerminalOwnershipUI(bufio.NewReader(bytes.NewBufferString("k\nn\n")), &out)

	require.Equal(t, "keep", ui.decideUnknown([]*change{{key: "a.md", rel: "a.md", group: "a.md"}}))
	require.Contains(t, out.String(), "Please answer y, n, or r.")
}

func TestFirstRunCanBeCancelled(t *testing.T) {
	root := firstRunRepo(t)

	out := runFirstRunForTest(t, initTestEnv(t), root, "\n\nn\n")

	require.Contains(t, out, "Setup cancelled; nothing was written.")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
	require.NoFileExists(t, filepath.Join(root, "AGENTS.md"))
}

func TestFirstRunEditOpensSettingsAndReturnsToTheConfirmation(t *testing.T) {
	root := firstRunRepo(t)

	out := runFirstRunForTest(t, initTestEnv(t), root, "\n\ne\n\n\n")

	require.Contains(t, out, "Settings")
	require.Equal(t, 2, bytes.Count([]byte(out), []byte("Set up Rhizome? [Y/n/e to edit]")))
	require.FileExists(t, filepath.Join(root, ".rhizome", "config.yml"))
}

func TestFirstRunStoresInstalledAgentsAsOn(t *testing.T) {
	root := firstRunRepo(t)
	original := installedAgentCommand
	installedAgentCommand = func(name string) (string, error) {
		if name == "claude" {
			return "/usr/local/bin/claude", nil
		}
		return original(name)
	}
	t.Cleanup(func() { installedAgentCommand = original })

	out := runFirstRunForTest(t, initTestEnv(t), root, "\n\n\n")

	require.Contains(t, out, "Agents    Claude Code")
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, agentModeOn, cfg.Agents.Claude)
	require.FileExists(t, filepath.Join(root, "CLAUDE.md"))
	require.FileExists(t, filepath.Join(root, ".claude", "skills", "rhizome", "SKILL.md"))
}

func TestFirstRunAgentsNoneWritesNoAgentFiles(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	root := firstRunRepo(t)

	require.NoError(t, Run(RunOptions{Env: env, Dir: root, Agents: "none", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}))

	require.NoFileExists(t, filepath.Join(root, "AGENTS.md"))
	require.NoDirExists(t, filepath.Join(root, ".agents"))
	cfg, err := repoconfig.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, agentModeOff, cfg.Agents.AgentsMd)
}

func TestSearchKeyPromptLeadsWithTheTeamKeyOnlyInBundledBuilds(t *testing.T) {
	env := initTestEnv(t)
	t.Setenv("VOYAGE_API_KEY", "")
	t.Setenv("ATOMIC_RHIZOME_KEY", "")
	const unlock = "synthetic-team-unlock"
	for _, bundled := range []bool{false, true} {
		var out bytes.Buffer
		reader := bufio.NewReader(bytes.NewBufferString(unlock + "\n"))
		session := credentials.NewSession(env,
			credentials.WithTeamKeyBundle(func() bool { return bundled }, func(value string) bool { return value == unlock }),
			credentials.WithTeamCoverage(func(key string) bool { return key == unlock }),
			credentials.WithPrompts(reader, &out),
		)

		provider, ready, saved, err := promptSearchKey(reader, &out, searchVoyage, session)
		require.NoError(t, err)
		require.Equal(t, searchVoyage, provider)
		require.True(t, ready)
		if bundled {
			require.Contains(t, out.String(), "This Rhizome build includes Atomic Object team keys.")
			require.Equal(t, "Atomic Object Rhizome key", saved)
			require.Equal(t, unlock, readCliConfigForTest(t, env).Env["ATOMIC_RHIZOME_KEY"])
		} else {
			require.NotContains(t, out.String(), "Atomic Object")
			require.Equal(t, "Voyage API Key", saved)
		}
		t.Setenv("ATOMIC_RHIZOME_KEY", "")
		t.Setenv("VOYAGE_API_KEY", "")
	}
}

func TestInitOffersToBuildTheIndexInATerminal(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	for _, tc := range []struct {
		name, answer string
		build        bool
	}{{"enter builds", "", true}, {"n leaves it for later", "n", false}} {
		t.Run(tc.name, func(t *testing.T) {
			root := firstRunRepo(t)
			builds := 0
			var out bytes.Buffer
			err := Run(RunOptions{Env: env, Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n\n\n" + tc.answer + "\n"), Stdout: &out, Stderr: &out,
				IndexNow: func(string) error { builds++; return nil }})

			require.NoError(t, err)
			require.Contains(t, out.String(), "Build the search index now? [Y/n]")
			if tc.build {
				require.Equal(t, 1, builds)
				require.NotContains(t, out.String(), "rzm index  build the search index")
			} else {
				require.Zero(t, builds)
				require.Contains(t, out.String(), "rzm index  build the search index")
			}
		})
	}
}

func TestInitDoesNotOfferTheIndexWithoutATerminal(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	root := firstRunRepo(t)
	var out bytes.Buffer
	require.NoError(t, Run(RunOptions{Env: env, Dir: root, Stdout: &out, Stderr: &out, IndexNow: func(string) error {
		t.Fatal("a run without a terminal must not index")
		return nil
	}}))
	require.Contains(t, out.String(), "rzm index  build the search index")
}

func TestInitReportsAFailedIndexAndKeepsTheSetup(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	root := firstRunRepo(t)
	var out bytes.Buffer
	err := Run(RunOptions{Env: env, Dir: root, Interactive: true, Stdin: bytes.NewBufferString("\n\n\n\n"), Stdout: &out, Stderr: &out,
		IndexNow: func(string) error { return errors.New("provider unavailable") }})

	require.ErrorContains(t, err, "setup is saved, but indexing stopped: provider unavailable")
	require.Contains(t, out.String(), "rzm index  build the search index")
	require.FileExists(t, filepath.Join(root, ".rhizome", "config.yml"))
}

func TestFirstRunCountsAnIncludedIgnoredFolderAfterShowingFindings(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFixtureFile(t, root, ".gitignore", "app/\n")
	writeFixtureFile(t, root, "app/go.mod", "module example.com/app\n")
	writeFixtureFile(t, root, "app/main.go", "package main\n")
	writeFixtureFile(t, root, "app/docs/design.md", "# Design\n")

	// Include app/, then accept the defaults.
	out, err := runInit(t, RunOptions{Env: env, Dir: root, Interactive: true, Stdin: bytes.NewBufferString("y\n\n\n\n")})

	require.NoError(t, err)
	findings := strings.Index(out, "Docs ")
	question := strings.Index(out, "Include app in indexing?")
	require.True(t, findings >= 0 && findings < question, "findings come before the first question:\n%s", out)
	require.Contains(t, out[question:], "Go (1 file)")
}
