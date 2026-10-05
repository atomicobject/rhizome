package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/stretchr/testify/require"
)

func TestAgentDerivedIdentifierRekeyRetiresAuthoredAliases(t *testing.T) {
	for _, tc := range []struct {
		name              string
		migration, rename bool
		aliasCount        int
		reversed          bool
	}{
		{name: "sequential without authored alias"},
		{name: "migration without authored alias", migration: true},
		{name: "sequential single alias", aliasCount: 1},
		{name: "migration single alias", migration: true, aliasCount: 1},
		{name: "governed rename both aliases", rename: true, aliasCount: 2},
		{name: "migration both aliases", migration: true, aliasCount: 2},
		{name: "governed rename reversed aliases", rename: true, aliasCount: 2, reversed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			strategy, oldPath, newPath, replacement := "SEQUENTIAL", "specs/z-loser.md", "specs/z-loser.md", "ITEM-0002"
			if tc.rename {
				oldPath, newPath = "specs/ITEM-0001-plan.md", "specs/ITEM-0002-plan.md"
			}
			if tc.migration {
				strategy, oldPath, newPath, replacement = "DATETIME", "specs/2026-10-04-12-20-plan.md", "specs/2026-10-04-12-20-plan.md", "ITEM-2026-10-04-12-20"
			}
			childFields := "aliases: [String!] @field"
			childAliases := ""
			if tc.aliasCount > 0 {
				childAliases = "aliases:: ITEM-0001-US1\naliases:: KEEP-CHILD-B\n"
			}
			if tc.aliasCount == 2 {
				childFields += "\nalias: [String!] @field"
				childAliases += "alias:: ITEM-0001-US1\nalias:: KEEP-CHILD-A\n"
				if tc.reversed {
					childFields = "alias: [String!] @field\naliases: [String!] @field"
				}
			}
			schema := fmt.Sprintf(`type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, strategy: %s, prefix: "ITEM")
 aliases: [String!] @field
 stories: Stories @contains(level: H2, heading: "Stories")
}
type Stories implements Section { items: [Story!] @contains(level: H3) }
type Story implements Section @node(locator: EMBEDDED) {
 id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
 %s
}
type Consumer @node(paths: ["notes/*.md"]) {
 aliases: [String!] @field
}`, strategy, childFields)
			original := "---\nid: ITEM-0001\naliases: [ITEM-0001, KEEP-ROOT]\n---\n# Plan\n\n## Stories\n\n### First\nid:: ^ITEM-0001-US1\n" + childAliases + "\n### Similar sibling\nid:: ^ITEM-0001-US10\naliases:: KEEP-SIBLING\n"
			oldTarget := strings.TrimSuffix(oldPath, ".md") + "#^ITEM-0001-US1"
			inbound := "---\naliases: [UNRELATED]\n---\n[[" + oldTarget + "]]\n"
			files := map[string]string{".rhizome/ontology/schema.graphql": schema, oldPath: original, "notes/inbound.md": inbound}
			if !tc.migration {
				files["specs/0-keeper.md"] = "---\nid: ITEM-0001\naliases: [ITEM-0001]\n---\n# Keeper\n"
			}
			vault := setupAgentTestVault(t, files)
			stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--vault", vault.name})
			unchanged, readErr := os.ReadFile(filepath.Join(vault.path, filepath.FromSlash(oldPath)))
			require.NoError(t, readErr)
			require.Equal(t, original, string(unchanged), "planning must leave original authored source intact")
			unchanged, readErr = os.ReadFile(filepath.Join(vault.path, "notes/inbound.md"))
			require.NoError(t, readErr)
			require.Equal(t, inbound, string(unchanged))
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
			require.NoError(t, json.Unmarshal([]byte(stdout), &applied), stdout)
			require.True(t, applied.OK, stdout)
			require.Equal(t, []string{action.ID}, applied.FixExecution.Applied)
			if tc.rename {
				_, err = os.Stat(filepath.Join(vault.path, filepath.FromSlash(oldPath)))
				require.True(t, os.IsNotExist(err))
			}
			body, err := os.ReadFile(filepath.Join(vault.path, filepath.FromSlash(newPath)))
			require.NoError(t, err)
			require.NotContains(t, string(body), "ITEM-0001")
			require.Contains(t, string(body), "KEEP-ROOT")
			require.Contains(t, string(body), "KEEP-SIBLING")
			inboundBody, err := os.ReadFile(filepath.Join(vault.path, "notes/inbound.md"))
			require.NoError(t, err)
			newTarget := strings.TrimSuffix(newPath, ".md") + "#^" + replacement + "-US1"
			wantInbound := "---\naliases: [UNRELATED]\n---\n[[" + newTarget + "]]\n"
			require.Equal(t, wantInbound, string(inboundBody), "the inbound target advances without changing the unrelated owner's aliases")
			store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			defer store.Close()
			nodes, err := store.OntologyNodesByType(context.Background(), "Story")
			require.NoError(t, err)
			require.Len(t, nodes, 2)
			for _, node := range nodes {
				require.Equal(t, newPath, node.NotePath)
				rows, err := store.OntologyNodeFieldValuesByNodeIDs(context.Background(), []string{node.NodeID}, []string{"id", "alias", "aliases"})
				require.NoError(t, err)
				byField := map[string][]string{}
				for _, row := range rows {
					byField[row.FieldName] = append(byField[row.FieldName], row.ValueText)
				}
				if node.Fragment == "^"+replacement+"-US1" {
					require.Equal(t, []string{replacement + "-US1"}, byField["id"])
					var want []string
					if tc.aliasCount > 0 {
						want = append(want, "KEEP-CHILD-B")
					}
					require.ElementsMatch(t, want, byField["aliases"])
					if tc.aliasCount == 2 {
						require.Equal(t, []string{"KEEP-CHILD-A"}, byField["alias"])
					}
				} else {
					require.Equal(t, "^"+replacement+"-US10", node.Fragment)
					require.Equal(t, []string{replacement + "-US10"}, byField["id"])
					require.ElementsMatch(t, []string{"KEEP-SIBLING"}, byField["aliases"])
					require.Empty(t, byField["alias"])
				}
			}
			require.NoError(t, store.Close())
			stdout, stderr, err = runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--vault", vault.name})
			require.NoError(t, err, stdout)
			require.Empty(t, stderr)
			var repeat validationrun.ValidationResult
			require.NoError(t, json.Unmarshal([]byte(stdout), &repeat), stdout)
			require.True(t, repeat.OK, stdout)
			if repeat.FixPlan != nil {
				require.Empty(t, repeat.FixPlan.Actions)
			}
		})
	}
}
