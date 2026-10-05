package agentapi

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestToolCatalogIsUniqueAndConstructible(t *testing.T) {
	wantAgentAPI := []string{
		"capabilities", "check_paths", "code_references", "code_symbol", "code_symbol_context",
		"community_list", "evaluate", "evaluate_batch", "external_references", "file_context", "files", "find_connections", "graph_path",
		"list_properties", "list_tags", "node_link", "note_rename_heading",
		"ontology_query", "ontology_query_schema", "query_recipe", "report",
		"semantic_query", "vault_context", "vault_health",
	}
	var agentAPI []string
	seen := make(map[string]struct{}, len(ToolCatalog()))
	seenCommands := make(map[string]struct{})
	seenCapabilityOrder := make(map[int]struct{})
	seenCLIOrder := make(map[int]struct{})
	for _, descriptor := range ToolCatalog() {
		require.NotEmpty(t, descriptor.Name)
		require.NotZerof(t, descriptor.Surfaces, "surface-zero tombstone %q", descriptor.Name)
		_, duplicate := seen[descriptor.Name]
		require.Falsef(t, duplicate, "duplicate tool descriptor %q", descriptor.Name)
		seen[descriptor.Name] = struct{}{}
		if descriptor.AgentCommand != "" {
			_, duplicate = seenCommands[descriptor.AgentCommand]
			require.Falsef(t, duplicate, "duplicate agent command %q", descriptor.AgentCommand)
			seenCommands[descriptor.AgentCommand] = struct{}{}
		}
		if descriptor.AgentCapabilityOrder > 0 {
			require.NotZerof(t, descriptor.Surfaces&SurfaceAgentAPI, "capability descriptor %q must be agentapi-dispatchable", descriptor.Name)
			_, duplicate = seenCapabilityOrder[descriptor.AgentCapabilityOrder]
			require.Falsef(t, duplicate, "duplicate agent capability order %d", descriptor.AgentCapabilityOrder)
			seenCapabilityOrder[descriptor.AgentCapabilityOrder] = struct{}{}
		}
		if descriptor.Surfaces&SurfaceAgentCLI != 0 {
			require.NotEmpty(t, descriptor.AgentCommand, "agent CLI descriptor %q", descriptor.Name)
			require.Positive(t, descriptor.AgentCLIOrder, "agent CLI descriptor %q", descriptor.Name)
			require.NotEmpty(t, descriptor.AgentCLICategory, "agent CLI descriptor %q", descriptor.Name)
			_, duplicate = seenCLIOrder[descriptor.AgentCLIOrder]
			require.Falsef(t, duplicate, "duplicate agent CLI order %d", descriptor.AgentCLIOrder)
			seenCLIOrder[descriptor.AgentCLIOrder] = struct{}{}
		} else {
			require.Zero(t, descriptor.AgentCLIOrder, "non-CLI descriptor %q", descriptor.Name)
			require.Empty(t, descriptor.AgentCLICategory, "non-CLI descriptor %q", descriptor.Name)
		}
		if descriptor.Kind == ToolKindLocal {
			require.Nil(t, descriptor.Handler)
			require.Zero(t, descriptor.Surfaces&(SurfaceAgentAPI|SurfaceCapabilities))
			require.NotZero(t, descriptor.Surfaces&(SurfaceAgentCLI|SurfaceCodeMode))
			continue
		}
		require.NotNil(t, descriptor.Handler)
		require.NotNil(t, descriptor.Handler(Config{}))
		if descriptor.Surfaces&SurfaceAgentAPI != 0 && descriptor.Availability != AvailabilityReadWrite {
			agentAPI = append(agentAPI, descriptor.Name)
			_, err := toolHandler(descriptor.Name, Config{})
			require.NoErrorf(t, err, "catalog entry %q must be dispatchable", descriptor.Name)
		}
	}
	sort.Strings(agentAPI)
	require.Equal(t, wantAgentAPI, agentAPI)
}

func TestToolCatalogCapabilityMetadataMatchesMCPContract(t *testing.T) {
	wantReadOnly := []string{
		"check_paths", "evaluate_batch",
		"capabilities", "files", "list_tags", "list_properties", "community_list",
		"file_context", "vault_context", "semantic_query", "code_symbol", "code_references",
		"code_symbol_context", "external_references", "find_connections", "report", "node_link",
		"note_rename_heading", "vault_health", "ontology_query_schema",
		"ontology_query", "query_recipe", "evaluate",
	}
	wantReadWrite := append([]string{}, wantReadOnly...)

	require.Equal(t, wantReadOnly, CapabilityToolNames(false))
	require.Equal(t, wantReadWrite, CapabilityToolNames(true))
	require.Equal(t, []string{"note_rename_heading"}, CapabilityMutatingToolNames(false))
	require.Equal(t, []string{"note_rename_heading", "node_link"}, CapabilityMutatingToolNames(true))
}

func TestToolCatalogCapabilityMetadataParityWithHandler(t *testing.T) {
	for _, readWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-only", true: "read-write"}[readWrite], func(t *testing.T) {
			vaultPath := t.TempDir()
			cfg := Config{
				Vault:     &obsidian.Vault{Name: "test-vault"},
				VaultPath: vaultPath,
				VaultDef:  obsidian.VaultDefinition{Name: "test-vault", Path: vaultPath},
				ReadWrite: readWrite,
			}
			payload, err := CallJSON(t.Context(), cfg, "capabilities", nil)
			require.NoError(t, err)

			var response CapabilitiesResponse
			require.NoError(t, json.Unmarshal(payload, &response))
			require.Equal(t, CapabilityToolNames(readWrite), response.Tools.Available)
			require.Equal(t, CapabilityMutatingToolNames(readWrite), response.Tools.Mutating)
			for _, name := range response.Tools.Available {
				_, err := toolHandler(name, cfg)
				require.NoErrorf(t, err, "capabilities advertised unreachable tool %q", name)
			}
		})
	}
}

func TestToolCatalogAgentCLICommandMapping(t *testing.T) {
	tests := map[string]string{
		"code-references":       "code_references",
		"external-references":   "external_references",
		"file-context":          "file_context",
		"node-link":             "node_link",
		"note-rename-heading":   "note_rename_heading",
		"ontology-query-schema": "ontology_query_schema",
		"query-recipe":          "query_recipe",
	}
	for command, wantTool := range tests {
		descriptor, ok := ToolForAgentCommand(command)
		require.Truef(t, ok, "command %q", command)
		require.Equal(t, wantTool, descriptor.Name)
	}
	_, ok := ToolForAgentCommand("surface")
	require.False(t, ok)

	local, ok := ToolForAgentCommand("ontology-inspect")
	require.True(t, ok)
	require.Equal(t, ToolKindLocal, local.Kind)
	require.Nil(t, local.Handler)
}

func TestToolCatalogAgentCLISurfaceProjectionIsCompleteAndOrdered(t *testing.T) {
	descriptors := AgentCLICommandDescriptors()
	wantCount := 0
	for _, descriptor := range ToolCatalog() {
		if descriptor.Surfaces&SurfaceAgentCLI != 0 {
			wantCount++
		}
	}
	require.Len(t, descriptors, wantCount)

	seenCommands := make(map[string]struct{}, len(descriptors))
	for i, descriptor := range descriptors {
		if i > 0 {
			require.Less(t, descriptors[i-1].AgentCLIOrder, descriptor.AgentCLIOrder)
		}
		_, duplicate := seenCommands[descriptor.AgentCommand]
		require.Falsef(t, duplicate, "duplicate advertised command %q", descriptor.AgentCommand)
		seenCommands[descriptor.AgentCommand] = struct{}{}

		resolved, ok := ToolForAgentCommand(descriptor.AgentCommand)
		require.Truef(t, ok, "advertised command %q must resolve", descriptor.AgentCommand)
		require.Equal(t, descriptor.Name, resolved.Name)
		require.Equal(t, descriptor.AgentCommand, resolved.AgentCommand)
		require.Equal(t, descriptor.Kind, resolved.Kind)
		require.Equal(t, descriptor.Surfaces, resolved.Surfaces)
		require.Equal(t, descriptor.AgentCLIOrder, resolved.AgentCLIOrder)
		require.Equal(t, descriptor.AgentCLICategory, resolved.AgentCLICategory)
	}
}

func TestAgentCapabilityToolNamesPreserveContract(t *testing.T) {
	require.Equal(t, []string{
		"code_references", "code_symbol", "code_symbol_context",
		"community_list", "file_context", "files", "find_connections", "graph_path",
		"list_properties", "list_tags", "node_link", "note_rename_heading", "ontology_query", "ontology_query_schema",
		"query_recipe", "report", "semantic_query",
		"vault_context", "vault_health", "external_references", "evaluate", "check_paths", "evaluate_batch",
	}, AgentCapabilityToolNames())
	for _, name := range AgentCapabilityToolNames() {
		_, err := toolHandler(name, Config{})
		require.NoErrorf(t, err, "standalone capabilities advertised unreachable tool %q", name)
	}
}

func TestAgentCapabilityMutatingToolNamesPreserveAvailability(t *testing.T) {
	require.Equal(t, []string{"note_rename_heading"}, AgentCapabilityMutatingToolNames(false))
	require.Equal(t, []string{"node_link", "note_rename_heading"}, AgentCapabilityMutatingToolNames(true))
}

func TestCodeContractMetadataIsReturnedByValue(t *testing.T) {
	first, ok := CodeOperationDescriptor("files")
	require.True(t, ok)
	first.CodeContract.Effects[0] = "corrupt"
	first.CodeContract.InputSchema[0] = 'x'
	first.CodeContract.Example["inputs"] = []string{"corrupt"}

	second, ok := CodeOperationDescriptor("files")
	require.True(t, ok)
	require.NotEqual(t, "corrupt", second.CodeContract.Effects[0])
	require.Equal(t, byte('{'), second.CodeContract.InputSchema[0])
	require.NotEqual(t, []string{"corrupt"}, second.CodeContract.Example["inputs"])

	second.CodeCLILeaves[0] = "changed"
	second.CodeContract.Example["changed"] = true
	third, ok := CodeOperationDescriptor("files")
	require.True(t, ok)
	require.Equal(t, []string{"agent files"}, third.CodeCLILeaves)
	require.NotContains(t, third.CodeContract.Example, "changed")

	catalog := ToolCatalog()
	for i := range catalog {
		if catalog[i].CodeContract != nil {
			catalog[i].CodeContract.Summary = "changed"
		}
	}
	descriptor, _ := CodeOperationDescriptor("files")
	require.NotEqual(t, "changed", descriptor.CodeContract.Summary)
}
