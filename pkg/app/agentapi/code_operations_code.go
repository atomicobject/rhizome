package agentapi

import (
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/graph/algorithms"
	"reflect"
)

func codeCodeSymbolDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("code_symbol", "code-symbol", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CodeSymbolTool), 3), 6, AgentCLICategoryCode)
	d.CodeAccess = CodeVaultRead
	c := contract("Resolve one indexed code symbol.", input(map[string]codeSchema{"symbol": str(), "path": str(), "language": str(), "contextLines": integer(), "maxBytes": integer()}, "symbol"), schemaForType(reflect.TypeOf(mcpapi.CodeSymbolResponse{}), map[reflect.Type]bool{}), map[string]any{"symbol": "agentapi.ToolDescriptor"}, "reads the existing code index")
	return completeCodeDescriptor(d, c, []string{"agent code-symbol"}, map[string]codeInputFieldMetadata{
		"contextLines": {Description: "Source lines around a resolved definition.", Default: 3, HasDefault: true},
		"maxBytes":     {Description: "Maximum source bytes returned for the definition.", Default: 20000, HasDefault: true},
	}, "Resolution can be ambiguous or unavailable; read status, candidates, coverage, and warnings before assuming a definition was found.")
}

func codeCodeReferencesDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("code_references", "code-references", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CodeReferencesTool), 2), 7, AgentCLICategoryCode)
	d.CodeAccess = CodeVaultRead
	c := contract("Return indexed definitions, callers, and callees.", input(map[string]codeSchema{"symbol": str(), "path": str(), "language": str(), "limit": integer(), "includeDefinitions": boolean(), "includeCallers": boolean(), "includeCallees": boolean()}, "symbol"), schemaForType(reflect.TypeOf(mcpapi.CodeReferencesResponse{}), map[reflect.Type]bool{}), map[string]any{"symbol": "agentapi.ToolDescriptor"}, "reads the existing code index")
	return completeCodeDescriptor(d, c, []string{"agent code-references"}, map[string]codeInputFieldMetadata{
		"limit":              {Description: "Maximum callers and maximum callees returned independently.", Default: 20, HasDefault: true},
		"includeDefinitions": {Description: "Include the resolved definition.", Default: true, HasDefault: true},
		"includeCallers":     {Description: "Include indexed callers.", Default: true, HasDefault: true},
		"includeCallees":     {Description: "Include indexed callees.", Default: true, HasDefault: true},
	}, "Caller and callee lists are independently limited and may be omitted by their include flags.")
}

func codeCodeSymbolContextDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("code_symbol_context", "code-symbol-context", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.CodeSymbolContextTool), 4), 8, AgentCLICategoryCode)
	c := contract("Build a compact indexed evidence packet for a symbol.", input(map[string]codeSchema{"symbol": str(), "path": str(), "language": str(), "includeTests": boolean()}, "symbol"), schemaForType(reflect.TypeOf(mcpapi.CodeSymbolContextResponse{}), map[reflect.Type]bool{}), map[string]any{"symbol": "agentapi.ToolDescriptor"}, "reads the existing code index and source snippets")
	return completeCodeDescriptor(d, c, []string{"agent code-symbol-context"}, map[string]codeInputFieldMetadata{
		"includeTests": {Description: "Include indexed test snippets in the evidence packet; set false to omit them.", Default: true, HasDefault: true},
	}, "The result is a compact indexed evidence packet. Test snippets are included by default; set includeTests to false to omit them.")
}

func codeExternalReferencesDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("external_references", "external-references", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.ExternalReferencesTool), 26), 29, AgentCLICategoryCode)
	d.CodeAccess = CodeVaultRead
	c := contract("Find local uses of an exact external code target.", input(map[string]codeSchema{"handle": str(), "ecosystem": str(), "module": str(), "symbolPrefix": str(), "limit": integer()}), schemaForType(reflect.TypeOf(mcpapi.ExternalReferencesResponse{}), map[reflect.Type]bool{}), map[string]any{"ecosystem": "go", "module": "github.com/mark3labs/mcp-go"}, "reads the existing code index")
	return completeCodeDescriptor(d, c, []string{"agent external-references"}, nil, "Use an exact external handle when known. Structured ecosystem, module, and symbolPrefix selectors may resolve a target or return ambiguous candidates; inspect status and warnings.")
}

func codeCodeRationaleDescriptor() ToolDescriptor {
	d := codeLocal(localDescriptor("code_rationale", "code-rationale", 28, AgentCLICategoryAnalysis), CodeLocalService.CodeRationale)
	d.CodeAccess = CodeVaultRead
	c := contract("List extracted rationale comments from indexed code.", input(map[string]codeSchema{"path": str(), "kinds": stringsSchema()}), output(map[string]codeSchema{"count": integer(), "rationale": codeSchema{"type": "array", "items": object([]string{"id", "path", "kind", "content", "startLine", "endLine"}, map[string]codeSchema{"id": str(), "path": str(), "symbolFqn": str(), "kind": str(), "content": str(), "startLine": integer(), "endLine": integer(), "fingerprint": str()})}}), map[string]any{"path": "pkg/app/agentapi"}, "reads the existing code index")
	return completeCodeDescriptor(d, c, []string{"agent code-rationale"}, nil, "path narrows extracted rationale comments; kinds filters the comment classifications returned.")
}

func codeReportDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("report", "report", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.ReportTool), 20), 12, AgentCLICategoryAnalysis)
	c := contract("Run one indexed code intelligence report.", input(map[string]codeSchema{"op": enumeration("doc_coverage", "complexity", "hotspots", "rationale_attention", "relatedness", "code_similarity"), "paths": stringsSchema(), "limit": integer(), "minLines": integer(), "includeTests": boolean()}, "op"), schemaForType(reflect.TypeOf(mcpapi.ReportResponse{}), map[reflect.Type]bool{}), map[string]any{"op": "hotspots"}, "reads the existing code index")
	return completeCodeDescriptor(d, c, []string{"agent report"}, nil, "op selects one report family; paths and limits narrow its indexed code evidence.")
}

func codeGraphPathDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("graph_path", "graph-path", SurfaceAgentAPI|SurfaceAgentCLI, MutationNever, mcpapi.GraphPathTool), 9), 27, AgentCLICategoryNavigation)
	d.CodeAccess = CodeVaultRead

	c := contract("Find a bounded typed graph path.", input(map[string]codeSchema{"from": str(), "to": str(), "maxHops": integer()}, "from", "to"), schemaForType(reflect.TypeOf(algorithms.PathResult{}), map[reflect.Type]bool{}), map[string]any{"from": "README.md", "to": "pkg/app/agentapi/catalog.go"}, "reads the ontology graph")
	return completeCodeDescriptor(d, c, []string{"agent graph-path"}, nil, "Both from and to are required; maxHops bounds the typed graph traversal.")
}
