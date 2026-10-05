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
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/stretchr/testify/require"
)

func TestAgentValidationIncludesEveryDeclaredAliasField(t *testing.T) {
	for _, order := range []struct{ name, fields, first string }{
		{"alias then aliases", "alias: [String!] @field\naliases: [String!] @field", "alias"},
		{"aliases then alias", "aliases: [String!] @field\nalias: [String!] @field", "aliases"},
	} {
		for _, collision := range []struct{ field, value string }{{"alias", "ITEM-0007"}, {"aliases", "ITEM-0008"}} {
			t.Run(order.name+" collision in "+collision.field, func(t *testing.T) {
				schema := `
type Story implements Section @node(locator: EMBEDDED) {
 id: ID! @field @identifier(preferred: true, derivable: false, prefix: "ITEM")
 ` + order.fields + `
}
type Stories implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, prefix: "ITEM")
 stories: Stories @contains(level: H2, heading: "Stories")
}
`
				vault := setupAgentTestVault(t, map[string]string{
					".rhizome/ontology/schema.graphql": schema,
					"specs/one.md":                     "---\nid: ITEM-0001\naliases: [ITEM-0001]\n---\n# One\n\n## Stories\n\n### First\nid:: ^ITEM-0002\nalias:: ITEM-0007\naliases:: ITEM-0008\n",
					"specs/keeper.md":                  fmt.Sprintf("---\nid: %s\naliases: [%s]\n---\n# Keeper\n", collision.value, collision.value),
				})
				stdout, stderr, runErr := runRootCLI(t, nil, []string{"agent", "validate", "identifiers", "--vault", vault.name})
				require.Empty(t, stderr)
				var result struct {
					OK         bool `json:"ok"`
					IssueCount int  `json:"issueCount"`
					Checks     []struct {
						Name   string `json:"name"`
						Error  string `json:"error"`
						Issues []struct {
							Code   string `json:"code"`
							Target string `json:"target"`
						} `json:"issues"`
					} `json:"checks"`
					Reconciliation *identifierreconcile.ReconciliationResult `json:"identifierReconciliation"`
				}
				require.NoError(t, json.Unmarshal([]byte(stdout), &result), "output=%s", stdout)
				require.Len(t, result.Checks, 1)
				require.Equal(t, "identifiers", result.Checks[0].Name)
				require.Empty(t, result.Checks[0].Error, "output=%s", stdout)
				require.NotNil(t, result.Reconciliation, "output=%s", stdout)
				require.NotNil(t, result.Reconciliation.Plan, "output=%s", stdout)
				store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
				require.NoError(t, err)
				nodes, err := store.OntologyNodesByType(context.Background(), "Story")
				require.NoError(t, err)
				require.Len(t, nodes, 1)
				fields, err := store.OntologyNodeFieldValuesByNodeIDs(context.Background(), []string{nodes[0].NodeID}, []string{"alias", "aliases"})
				require.NoError(t, err)
				require.NoError(t, store.Close())
				authored := map[string]string{}
				for _, field := range fields {
					authored[field.FieldName] = field.ValueText
				}
				require.Equal(t, map[string]string{"alias": "ITEM-0007", "aliases": "ITEM-0008"}, authored)
				t.Logf("declared first=%s collision field=%s canonical values=%v public OK=%v issues=%d collisions=%d error=%v", order.first, collision.field, authored, result.OK, result.IssueCount, len(result.Reconciliation.Plan.Collisions), runErr)
				require.Len(t, result.Reconciliation.Plan.Collisions, 1, "every authored alias claim must participate in public reconciliation")
				require.Equal(t, identifierreconcile.IdentifierComparisonKey(collision.value), result.Reconciliation.Plan.Collisions[0].Value)
				require.False(t, result.OK)
				require.Error(t, runErr)
			})
		}
	}
}

func TestAgentIdentifierRepairRemovesEveryAuthoredAliasClaim(t *testing.T) {
	for _, tc := range []struct{ name, fields string }{
		{"alias then aliases", "alias: [String!] @field\naliases: [String!] @field"},
		{"aliases then alias", "aliases: [String!] @field\nalias: [String!] @field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := `
type Story implements Section @node(locator: EMBEDDED) {
 id: ID! @field @identifier(preferred: true, derivable: false, prefix: "ITEM")
 ` + tc.fields + `
}
type Stories implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, prefix: "ITEM")
 stories: Stories @contains(level: H2, heading: "Stories")
}
`
			vault := setupAgentTestVault(t, map[string]string{
				".rhizome/ontology/schema.graphql": schema,
				"specs/one.md":                     "---\nid: ITEM-0001\naliases: [ITEM-0001]\n---\n# One\n\n## Stories\n\n### First\nid:: ^ITEM-0002\nalias:: ITEM-0007\naliases:: ITEM-0007\n",
				"specs/keeper.md":                  "---\nid: ITEM-0007\naliases: [ITEM-0007]\n---\n# Keeper\n",
			})
			stdout, stderr, runErr := runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--vault", vault.name})
			require.Error(t, runErr)
			require.Empty(t, stderr)
			var planned validationrun.ValidationResult
			require.NoError(t, json.Unmarshal([]byte(stdout), &planned))
			require.Empty(t, planned.ExecutionError)
			require.NotNil(t, planned.FixPlan)
			require.Len(t, planned.FixPlan.Actions, 1, "output=%s", stdout)
			action := planned.FixPlan.Actions[0]
			t.Logf("planned collision=%s action=%s safety=%s operations=%d", planned.IdentifierReconciliation.Plan.Collisions[0].Value, action.ID, action.Safety, len(planned.FixPlan.Operations))
			stdout, stderr, runErr = runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--apply", "--action", action.ID, "--vault", vault.name})
			require.Empty(t, stderr)
			var applied validationrun.ValidationResult
			require.NoError(t, json.Unmarshal([]byte(stdout), &applied), "output=%s", stdout)
			require.Empty(t, applied.ExecutionError, "output=%s", stdout)
			require.NotNil(t, applied.FixExecution, "output=%s", stdout)
			require.Len(t, applied.FixExecution.Applied, 1, "output=%s", stdout)
			body, err := os.ReadFile(filepath.Join(vault.path, "specs/one.md"))
			require.NoError(t, err)
			store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			nodes, err := store.OntologyNodesByType(context.Background(), "Story")
			require.NoError(t, err)
			require.Len(t, nodes, 1)
			fields, err := store.OntologyNodeFieldValuesByNodeIDs(context.Background(), []string{nodes[0].NodeID}, []string{"alias", "aliases"})
			require.NoError(t, err)
			require.NoError(t, store.Close())
			t.Logf("after public apply: OK=%v issues=%d remaining=%d applied=%v error=%v authored=%q canonical alias rows=%v", applied.OK, applied.IssueCount, applied.FixExecution.RemainingFindings, applied.FixExecution.Applied, runErr, string(body), fields)
			require.NotContains(t, string(body), "ITEM-0007", "all authored alias locations must lose the collided value before a repair reports clean")
			require.Empty(t, fields)
			require.NoError(t, runErr)
			require.True(t, applied.OK)
			require.Zero(t, applied.FixExecution.RemainingFindings)
			stdout, stderr, runErr = runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--vault", vault.name})
			require.NoError(t, runErr)
			require.Empty(t, stderr)
			var repeat validationrun.ValidationResult
			require.NoError(t, json.Unmarshal([]byte(stdout), &repeat))
			require.True(t, repeat.OK)
			require.Empty(t, repeat.IdentifierReconciliation.Plan.Collisions)
			if repeat.FixPlan != nil {
				require.Empty(t, repeat.FixPlan.Actions)
			}
		})
	}
}

func TestAgentIdentifierDistinctAliasCollisionsRetainCompleteMembership(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, prefix: "ITEM")
 alias: [String!] @field(source: "alternate-names")
 aliases: [String!] @field
}`,
		"specs/loser.md":   "---\nid: ITEM-0001\nalternate-names: [ITEM-0007, KEEP-A]\naliases: [ITEM-0001, ITEM-0008, KEEP-B]\n---\n",
		"specs/keeper7.md": "---\nid: ITEM-0007\naliases: [ITEM-0007]\n---\n",
		"specs/keeper8.md": "---\nid: ITEM-0008\naliases: [ITEM-0008]\n---\n",
	})
	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "fix", "identifiers", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)
	var planned validationrun.ValidationResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &planned), stdout)
	require.Empty(t, planned.ExecutionError, stdout)
	require.Len(t, planned.IdentifierReconciliation.Plan.Collisions, 2)
	require.NotNil(t, planned.FixPlan)
	require.Len(t, planned.FixPlan.Actions, 2)
	var actions, issues []string
	args := []string{"agent", "validate", "fix", "identifiers", "--apply", "--vault", vault.name}
	for _, action := range planned.FixPlan.Actions {
		require.Len(t, action.IssueKeys, 2)
		actions = append(actions, action.ID)
		issues = append(issues, action.IssueKeys...)
		args = append(args, "--action", action.ID)
	}
	require.Len(t, planned.FixPlan.Operations, 1)
	require.ElementsMatch(t, actions, planned.FixPlan.Operations[0].ActionIDs)
	require.ElementsMatch(t, issues, planned.FixPlan.Operations[0].IssueKeys)
	stdout, stderr, err = runRootCLI(t, nil, args)
	require.NoError(t, err, stdout)
	require.Empty(t, stderr)
	var applied validationrun.ValidationResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &applied))
	require.True(t, applied.OK, stdout)
	require.ElementsMatch(t, actions, applied.FixExecution.Applied)
	body, err := os.ReadFile(filepath.Join(vault.path, "specs/loser.md"))
	require.NoError(t, err)
	require.Equal(t, "---\nid: ITEM-0001\nalternate-names: [KEEP-A]\naliases: [ITEM-0001, KEEP-B]\n---\n", string(body))
	store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	nodes, err := store.OntologyNodesByType(context.Background(), "Spec")
	require.NoError(t, err)
	var nodeID string
	for _, node := range nodes {
		if node.NotePath == "specs/loser.md" {
			nodeID = node.NodeID
		}
	}
	require.NotEmpty(t, nodeID)
	rows, err := store.OntologyNodeFieldValuesByNodeIDs(context.Background(), []string{nodeID}, []string{"alias", "aliases"})
	require.NoError(t, err)
	require.NoError(t, store.Close())
	var values []string
	for _, row := range rows {
		values = append(values, row.ValueText)
	}
	require.ElementsMatch(t, []string{"KEEP-A", "KEEP-B", "ITEM-0001"}, values)
}
