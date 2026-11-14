package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func TestInitCommandResolvesLegacyFileDecisions(t *testing.T) {
	for _, choice := range []string{"--take", "--keep"} {
		t.Run(choice, func(t *testing.T) {
			t.Setenv(repoSkipDelegateEnv, "1")
			root := t.TempDir()
			ctx := vaultconfig.WithEnv(context.Background(), testkit.Env(t))
			stdout, stderr, err := runRootCLI(t, ctx, []string{"init", "--path", root, "--json", "--workflow", "agentic-engineering", "--agents", "shared", "--search", "off", "--binary-manager", "external"})
			require.NoError(t, err, stdout+stderr)
			const target = ".rhizome/modules/agentic-engineering/ontology/spec-driven.graphql"
			const source = ".rhizome/ontology/spec-driven.graphql"
			current, err := os.ReadFile(filepath.Join(root, target))
			require.NoError(t, err)
			local := string(current) + "\ntype Custom { value: String }\n"
			writeCmdFixture(t, root, source, local)
			require.NoError(t, os.Remove(filepath.Join(root, target)))
			recordPath := filepath.Join(root, ".rhizome/generated-files.yml")
			record, err := os.ReadFile(recordPath)
			require.NoError(t, err)
			var lines []string
			for _, line := range strings.Split(string(record), "\n") {
				if !strings.Contains(line, target) {
					lines = append(lines, line)
				}
			}
			require.NoError(t, os.WriteFile(recordPath, []byte(strings.Join(lines, "\n")), 0o644))
			stdout, stderr, err = runRootCLI(t, ctx, []string{"init", "--path", root, "--json", "--check"})
			require.NoError(t, err, stdout+stderr)
			var plan initactions.SetupPlan
			require.NoError(t, json.Unmarshal([]byte(stdout), &plan))
			require.Len(t, plan.Writes.Decisions, 1)
			require.Equal(t, target, plan.Writes.Decisions[0].Path)
			require.Equal(t, source, plan.Writes.Decisions[0].From)
			require.NotEmpty(t, plan.Writes.DecisionSnapshot)
			stdout, stderr, err = runRootCLI(t, ctx, []string{"init", "--path", root, "--json", "--decision-snapshot=" + plan.Writes.DecisionSnapshot, choice, target})
			require.NoError(t, err, stdout+stderr)
			var result initactions.SetupResult
			require.NoError(t, json.Unmarshal([]byte(stdout), &result))
			require.Empty(t, result.Decisions)
			if choice == "--take" {
				require.NoFileExists(t, filepath.Join(root, source))
				updated, err := os.ReadFile(filepath.Join(root, target))
				require.NoError(t, err)
				require.Equal(t, current, updated)
			} else {
				require.NoFileExists(t, filepath.Join(root, target))
				kept, err := os.ReadFile(filepath.Join(root, source))
				require.NoError(t, err)
				require.Equal(t, local, string(kept))
			}
			stdout, stderr, err = runRootCLI(t, ctx, []string{"init", "--path", root, "--json", "--check"})
			require.NoError(t, err, stdout+stderr)
			require.NoError(t, json.Unmarshal([]byte(stdout), &plan))
			require.Empty(t, plan.Writes.Decisions)
			require.Empty(t, plan.Writes.Files)
		})
	}
}
