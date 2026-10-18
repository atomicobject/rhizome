package cmd

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"

	"testing"

	"github.com/stretchr/testify/require"
)

type jsonFailure struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeCmdFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

// runScopeCLI runs rzm in dir. Cobra keeps a subcommand's first context, so
// the context is set on the subcommand itself.
// runScopeCLI binds the selected working directory to a fresh tree.
func runScopeCLI(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()
	env := testkit.Env(t)
	env.WorkingDir = dir
	return runRootCLI(t, vaultconfig.WithEnv(context.Background(), env), append([]string{"index", "scope"}, args...))
}

func TestIndexScopeCommandEditsAndReportsRules(t *testing.T) {
	t.Setenv(repoSkipDelegateEnv, "1")
	root := t.TempDir()
	writeCmdFixture(t, root, ".rhizome/config.yml", "notes:\n  excludes: [drafts/**]\n")
	writeCmdFixture(t, root, ".rhizome/ignore", "vendor/\n")
	writeCmdFixture(t, root, ".gitignore", "dist/\n")
	writeCmdFixture(t, root, "generated/api.go", "package api\n")
	stdout, stderr, err := runScopeCLI(t, root, "--json", "--skip", "generated", "--remove-rule", "vendor/")
	require.NoError(t, err, stderr)

	var scope struct {
		Schema int `json:"schema"`
		Rules  []struct {
			Layer   string `json:"layer"`
			Source  string `json:"source"`
			Pattern string `json:"pattern"`
		} `json:"rules"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &scope), stdout)
	require.Equal(t, 1, scope.Schema)
	var patterns []string
	for _, rule := range scope.Rules {
		patterns = append(patterns, rule.Layer+" "+rule.Pattern)
	}
	require.Equal(t, []string{"gitignore dist/", "rhizome /generated/", "config drafts/**"}, patterns)

	human, _, err := runScopeCLI(t, root)
	require.NoError(t, err)
	require.Contains(t, human, "Inherited from .gitignore")
	require.Contains(t, human, "skipped in settings")
}

func TestIndexScopeCommandFailuresAreJSON(t *testing.T) {
	t.Setenv(repoSkipDelegateEnv, "1")
	configured := t.TempDir()
	writeCmdFixture(t, configured, ".rhizome/config.yml", "notes: {}\n")
	writeCmdFixture(t, configured, ".rhizome/ignore", "vendor/\n")

	for name, run := range map[string]struct {
		dir  string
		args []string
		code string
	}{
		"not set up":   {t.TempDir(), nil, "not_configured"},
		"missing rule": {configured, []string{"--remove-rule", "/nope/"}, "scope_failed"},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, _, err := runScopeCLI(t, run.dir, append([]string{"--json"}, run.args...)...)
			var exit silentExitError
			require.ErrorAs(t, err, &exit)
			var failure jsonFailure
			require.NoError(t, json.Unmarshal([]byte(stdout), &failure), stdout)
			require.Equal(t, run.code, failure.Error.Code)
		})
	}
	data, err := os.ReadFile(filepath.Join(configured, ".rhizome", "ignore"))
	require.NoError(t, err)
	require.Equal(t, "vendor/\n", string(data))
}

func TestInitCommandJSONReportsAPlanWithoutWriting(t *testing.T) {
	t.Setenv(repoSkipDelegateEnv, "1")
	dir := t.TempDir()
	writeCmdFixture(t, dir, "docs/readme.md", "# Docs\n")

	stdout, stderr, err := runRootCLI(t, vaultconfig.WithEnv(context.Background(), testkit.Env(t)), []string{"init", "--path", dir, "--check", "--json", "--workflow", "none"})
	require.NoError(t, err, stderr)

	var plan struct {
		Schema     int    `json:"schema"`
		Configured bool   `json:"configured"`
		Workflow   string `json:"workflow"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &plan), stdout)
	require.Equal(t, 1, plan.Schema)
	require.False(t, plan.Configured)
	require.Equal(t, "none", plan.Workflow)
	require.NoDirExists(t, filepath.Join(dir, ".rhizome"))
}

func TestInitCommandJSONFailuresAreJSON(t *testing.T) {

	t.Setenv(repoSkipDelegateEnv, "1")
	configured := t.TempDir()
	writeCmdFixture(t, configured, ".rhizome/config.yml", "notes: {}\n")

	for name, run := range map[string]struct {
		args            []string
		stdin, code, in string
	}{
		"set up already":   {[]string{"init", "--path", configured, "--json"}, "", "not_first_run", ""},
		"no key on a pipe": {[]string{"init", "--path", t.TempDir(), "--json", "--search-key-stdin"}, "  \n", "setup_failed", "no key"},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, _, err := runRootCLIWithInput(t, vaultconfig.WithEnv(context.Background(), testkit.Env(t)), run.args, run.stdin)
			var exit silentExitError
			require.ErrorAs(t, err, &exit)
			require.Equal(t, 1, exit.ExitCode())
			var failure jsonFailure
			require.NoError(t, json.Unmarshal([]byte(stdout), &failure), stdout)
			require.Equal(t, run.code, failure.Error.Code)
			require.Contains(t, failure.Error.Message, run.in)
		})
	}
}
