package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/stretchr/testify/require"
)

func TestAgentIdentifierRekeyRetiresAliasesFromEveryField(t *testing.T) {
	for _, first := range []string{"alias", "aliases"} {
		for _, migration := range []bool{false, true} {
			t.Run(fmt.Sprintf("mirror_%s_migration_%v", first, migration), func(t *testing.T) {
				fields := "alias: [String!] @field(source: \"alternate-names\")\naliases: [String!] @field"
				if first == "aliases" {
					fields = "aliases: [String!] @field\nalias: [String!] @field(source: \"alternate-names\")"
				}
				strategy, notePath, replacement := "SEQUENTIAL", "specs/z-loser.md", "ITEM-0002"
				if migration {
					strategy, notePath, replacement = "DATETIME", "specs/2026-10-04-12-20-plan.md", "ITEM-2026-10-04-12-20"
				}
				schema := fmt.Sprintf(`type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, strategy: %s, prefix: "ITEM")
 %s
}`, strategy, fields)
				files := map[string]string{
					".rhizome/ontology/schema.graphql": schema,
					notePath:                           "---\nid: ITEM-0001\nalternate-names: [ITEM-0001, KEEP-A]\naliases: [ITEM-0001, KEEP-B]\n---\n# Plan\n",
				}
				if !migration {
					files["specs/a-keeper.md"] = "---\nid: ITEM-0001\naliases: [ITEM-0001]\n---\n# Keeper\n"
				}
				vault := setupAgentTestVault(t, files)
				stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--vault", vault.name})
				require.Error(t, err)
				require.Empty(t, stderr)
				var planned validationrun.ValidationResult
				require.NoError(t, json.Unmarshal([]byte(stdout), &planned), stdout)
				require.Empty(t, planned.ExecutionError, stdout)
				require.NotNil(t, planned.FixPlan, stdout)
				require.Len(t, planned.FixPlan.Actions, 1, stdout)
				action := planned.FixPlan.Actions[0]
				stdout, stderr, err = runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--apply", "--action", action.ID, "--vault", vault.name})
				require.NoError(t, err, stdout)
				require.Empty(t, stderr)
				var applied validationrun.ValidationResult
				require.NoError(t, json.Unmarshal([]byte(stdout), &applied))
				require.True(t, applied.OK, stdout)
				require.Equal(t, []string{action.ID}, applied.FixExecution.Applied)
				body, err := os.ReadFile(filepath.Join(vault.path, filepath.FromSlash(notePath)))
				require.NoError(t, err)
				require.NotContains(t, string(body), "ITEM-0001")
				require.Contains(t, string(body), "id: "+replacement)
				require.Contains(t, string(body), "KEEP-A")
				require.Contains(t, string(body), "KEEP-B")
				store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
				require.NoError(t, err)
				nodes, err := store.OntologyNodesByType(context.Background(), "Spec")
				require.NoError(t, err)
				var nodeID string
				for _, node := range nodes {
					if node.NotePath == notePath {
						nodeID = node.NodeID
					}
				}
				require.NotEmpty(t, nodeID)
				rows, err := store.OntologyNodeFieldValuesByNodeIDs(context.Background(), []string{nodeID}, []string{"id", "alias", "aliases"})
				require.NoError(t, err)
				require.NoError(t, store.Close())
				byField := map[string][]string{}
				for _, row := range rows {
					byField[row.FieldName] = append(byField[row.FieldName], row.ValueText)
				}
				require.Equal(t, []string{replacement}, byField["id"])
				for _, field := range []string{"alias", "aliases"} {
					keep := "KEEP-A"
					if field == "aliases" {
						keep = "KEEP-B"
					}
					want := []string{keep}
					if field == "aliases" {
						want = append(want, replacement)
					}
					require.ElementsMatch(t, want, byField[field], "mirror is appended only to its established field")
				}
				stdout, stderr, err = runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--vault", vault.name})
				require.NoError(t, err, stdout)
				require.Empty(t, stderr)
				var repeat validationrun.ValidationResult
				require.NoError(t, json.Unmarshal([]byte(stdout), &repeat))
				require.True(t, repeat.OK)
				if repeat.FixPlan != nil {
					require.Empty(t, repeat.FixPlan.Actions)
				}
			})
		}
	}
}
