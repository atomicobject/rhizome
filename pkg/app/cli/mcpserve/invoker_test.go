package mcpserve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEveryExposedMCPToolResolvesToRegisteredRuntimeOperation(t *testing.T) {
	registry := oneshotruntime.DefaultRegistry()
	for _, descriptor := range agentapi.ToolCatalog() {
		if descriptor.Surfaces&agentapi.SurfaceAgentAPI == 0 || descriptor.Handler == nil {
			continue
		}
		input := map[string]any{}
		if descriptor.CodeContract != nil {
			input = descriptor.CodeContract.Example
		}
		operationID, err := RuntimeOperationID(descriptor.Name, input)
		require.NoError(t, err, descriptor.Name)
		_, registered := registry.Declaration(operationID)
		require.Truef(t, registered, "MCP tool %q resolved to undeclared operation %q", descriptor.Name, operationID)
	}
}

func TestQueryRecipeActionsResolveThroughConcreteCodeModeLeaves(t *testing.T) {
	for _, action := range []string{"list", "validate", "run"} {
		operationID, err := RuntimeOperationID("query_recipe", map[string]any{"op": action})
		require.NoError(t, err)
		require.Equal(t, oneshotruntime.OperationID("agent.query-recipe."+action), operationID)
	}
}

func TestInvokerDispatchesQueryRecipeThroughSharedHandler(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	recipeDir := filepath.Join(root, ".rhizome", "query-recipes")
	require.NoError(t, os.MkdirAll(recipeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(recipeDir, "demo.yaml"), []byte(`apiVersion: rhizome.query-recipe.v1
id: demo
name: Demo
problem: Load one note.
inputSpec:
  mode: none
query:
  graphQL: |
    { notes(first: 1) { path } }
outputContract:
  empty: No notes found.
adaptationGuidance:
  summary: Keep this query narrow.
`), 0o644))

	var preparedOperation oneshotruntime.OperationID
	definition := obsidian.VaultDefinition{Name: "test", Path: root}
	dispatch, err := NewInvoker(context.Background(), false, Dependencies{
		VaultDefinition: func(context.Context) (obsidian.VaultDefinition, error) { return definition, nil },
		RuntimeFree:     func() (agentapi.Config, error) { return agentapi.Config{VaultDef: definition, VaultPath: root}, nil },
		Prepare: func(_ context.Context, _ string, operationID oneshotruntime.OperationID, _ map[string]any) (agentapi.Config, func(), error) {
			preparedOperation = operationID
			return agentapi.Config{VaultDef: definition, VaultPath: root}, nil, nil
		},
	})
	require.NoError(t, err)

	payload, err := dispatch(context.Background(), "query_recipe", map[string]any{"op": "list"})
	require.NoError(t, err)
	require.Equal(t, oneshotruntime.OperationID("agent.query-recipe.list"), preparedOperation)
	var response struct {
		Recipes []struct {
			ID    string `json:"id"`
			Input struct {
				Mode string `json:"mode"`
			} `json:"input"`
			Query              any `json:"query"`
			OutputContract     any `json:"outputContract"`
			AdaptationGuidance any `json:"adaptationGuidance"`
		} `json:"recipes"`
	}
	require.NoError(t, json.Unmarshal(payload, &response))
	require.Len(t, response.Recipes, 1)
	require.Equal(t, "demo", response.Recipes[0].ID)
	require.Equal(t, "none", response.Recipes[0].Input.Mode)
	require.Nil(t, response.Recipes[0].Query)
	require.Nil(t, response.Recipes[0].OutputContract)
	require.Nil(t, response.Recipes[0].AdaptationGuidance)
	require.NotContains(t, string(payload), "{ notes(first: 1)")
	require.NotContains(t, string(payload), "Keep this query narrow")
}

func TestInvokerConfigSnapshotDetectsPublishedChange(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing config", true: "existing config"}[exists], func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			path := filepath.Join(root, ".rhizome", "config.yml")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			if exists {
				require.NoError(t, os.WriteFile(path, []byte("# original\n{}\n"), 0o644))
			}
			definition := obsidian.VaultDefinition{Name: "test", Path: root}
			config := agentapi.Config{VaultDef: definition, VaultPath: root}
			dispatch, err := NewInvoker(context.Background(), false, Dependencies{
				VaultDefinition: func(context.Context) (obsidian.VaultDefinition, error) { return definition, nil },
				RuntimeFree:     func() (agentapi.Config, error) { return config, nil },
				Prepare: func(context.Context, string, oneshotruntime.OperationID, map[string]any) (agentapi.Config, func(), error) {
					return config, nil, nil
				},
			})
			require.NoError(t, err)
			first, err := dispatch(context.Background(), "capabilities", map[string]any{})
			require.NoError(t, err)
			require.True(t, json.Valid(first))

			require.NoError(t, obsidian.WriteFileAtomicPreservingMode(path, []byte("# published\n{}\n"), 0o644))
			second, err := dispatch(context.Background(), "capabilities", map[string]any{})
			require.ErrorContains(t, err, "configuration changed; close and reconnect")
			require.Nil(t, second)
		})
	}
}
