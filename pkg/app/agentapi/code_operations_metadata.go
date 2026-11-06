package agentapi

import (
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	"reflect"
)

func codeListTagsDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("list_tags", "list-tags", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.ListTagsTool), 11), 14, AgentCLICategoryDiscovery)
	c := contract("List tags matching optional patterns.", input(map[string]codeSchema{"match": stringsSchema()}), schemaForType(reflect.TypeOf(mcpapi.TagListResponse{}), map[reflect.Type]bool{}), map[string]any{}, "reads note metadata")
	return completeCodeDescriptor(d, c, []string{"agent list-tags"}, nil, "Match filters the note set before tag summaries are calculated.")
}

func codeListPropertiesDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("list_properties", "list-properties", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.ListPropertiesTool), 10), 15, AgentCLICategoryDiscovery)
	c := contract("List property names, types, values, and counts.", input(map[string]codeSchema{"source": enumeration("all", "frontmatter", "inline"), "match": stringsSchema(), "only": stringsSchema(), "valueLimit": integer(), "maxValues": integer(), "excludeTags": boolean(), "verbose": boolean(), "valueCounts": boolean()}), schemaForType(reflect.TypeOf(mcpapi.PropertyListResponse{}), map[reflect.Type]bool{}), map[string]any{"source": "all"}, "reads note metadata")
	return completeCodeDescriptor(d, c, []string{"agent list-properties"}, map[string]codeInputFieldMetadata{
		"source":     {Description: "Property source to inspect.", Default: "all", HasDefault: true},
		"valueLimit": {Description: "Values shown per property: normally 25; verbose raises this to at least 50. When only selects properties and valueLimit is omitted, that rule is replaced by maxValues minus one (or maxValues when it is 1)."},
		"maxValues":  {Description: "Maximum distinct values considered: initially 500 when omitted or nonpositive, then raised to at least the effective valueLimit plus one."},
	}, "Source selects frontmatter, inline, or both; value limits bound the values retained in each summary.")
}

func codeCommunityListDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("community_list", "community-list", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CommunityListTool), 5), 16, AgentCLICategoryDiscovery)
	c := contract("List graph communities and representative notes.", input(map[string]codeSchema{"maxCommunities": integer(), "maxTopNotes": integer()}), schemaForType(reflect.TypeOf(mcpapi.CommunityListResponse{}), map[reflect.Type]bool{}), map[string]any{}, "reads graph analysis")
	return completeCodeDescriptor(d, c, []string{"agent community-list"}, nil, "Community and representative-note limits bound graph-analysis output.")
}

func codeVaultHealthDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("vault_health", "vault-health", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.VaultHealthTool), 25), 13, AgentCLICategoryAnalysis)
	c := contract("Compute selected vault health metrics.", input(map[string]codeSchema{"include": stringsSchema(), "staleDays": integer(), "limit": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeImages": boolean()}), schemaForType(reflect.TypeOf(mcpapi.VaultHealthResponse{}), map[reflect.Type]bool{}), map[string]any{}, "reads files and graph analysis")
	return completeCodeDescriptor(d, c, []string{"agent vault-health"}, map[string]codeInputFieldMetadata{
		"staleDays": {Description: "Age threshold for stale-note reporting, in days.", Default: 90, HasDefault: true},
		"limit":     {Description: "Maximum entries returned for each report category.", Default: 25, HasDefault: true},
	}, "Each health category is truncated independently by limit; staleDays controls only stale-note classification.")
}

func codeCheckPathsDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("check_paths", "check-paths", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CheckPathsTool), 28), 32, AgentCLICategoryDiscovery)
	d.CodeAccess = CodeIndependent
	c := contract("Check paths against the vault's unified ignore rules in bulk.", input(map[string]codeSchema{
		"paths": {"type": "array", "items": object([]string{"path"}, map[string]codeSchema{"path": str(), "isDir": boolean()})},
	}, "paths"), schemaForType(reflect.TypeOf(struct {
		Paths []mcpapi.PathCheck `json:"paths"`
	}{}), map[reflect.Type]bool{}), map[string]any{"paths": []any{map[string]any{"path": "README.md"}, map[string]any{"path": "vendor", "isDir": true}}}, "reads ignore rules and resolves path containment", "does not read file contents or initialize an index")
	return completeCodeDescriptor(d, c, []string{"agent check-paths"}, map[string]codeInputFieldMetadata{"paths": {Description: "1..10000 path entries. Paths are vault-relative or absolute within the vault. Set isDir for directory rules. Includes root/nested gitignore, .rhizome/ignore and configured exclusions."}}, "One result per input, in order. allowed means allowed by ignore rules, not selected as a note or supported as code. invalid is never equivalent to allowed. Rule attribution includes negations and ignored ancestors. sessionId and budgetChars do not alter decisions.")
}
