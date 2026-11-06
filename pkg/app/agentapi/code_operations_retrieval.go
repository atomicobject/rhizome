package agentapi

import (
	"fmt"
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	"reflect"
)

func codeFilesDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("files", "files", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.FilesTool), 7), 9, AgentCLICategoryDiscovery)
	d.validateCodeInput = func(value map[string]any) error {
		if _, continuation := value["continuationToken"]; !continuation {
			if _, present := value["inputs"]; !present {
				return fmt.Errorf("input.inputs is required without continuationToken")
			}
		}
		return nil
	}
	c := contract("Read files selected by input expressions.", input(map[string]codeSchema{
		"inputs": stringsSchema(), "continuationToken": str(), "includeContent": codeSchema{"oneOf": []any{boolean(), enumeration("true", "false", "compress")}}, "intent": str(), "limit": integer(), "maxDepth": integer(), "dedupe": boolean(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeFrontmatter": boolean(), "includeBacklinks": boolean(), "absolutePaths": boolean(), "suppressTags": stringsSchema(), "noSuppress": boolean(),
	}), schemaForType(reflect.TypeOf(mcpapi.FilesResponse{}), map[reflect.Type]bool{}), map[string]any{"inputs": []string{"pkg/app/agentapi/catalog.go"}, "includeContent": true}, "reads selected files", "may update session dedupe bookkeeping", "provide inputs for a first page or continuationToken for a later page")
	return completeCodeDescriptor(d, c, []string{"agent files"}, map[string]codeInputFieldMetadata{
		"inputs":            {Description: "File/directory paths or selectors such as tag:project and find:name. AND, OR, NOT and parentheses combine selectors; adjacent selectors use OR. Required unless continuationToken is supplied."},
		"continuationToken": {Description: "Opaque token from a previous files payload. It restores that request's selection and paging options."},
		"includeContent":    {Description: "Include file content; use \"compress\" to request compression or false for metadata only.", Default: true, HasDefault: true},
		"limit":             {Description: "Maximum returned files. The runtime defaults to 25 with content and 500 without content."},
		"maxDepth":          {Description: "Additional graph traversal depth around selected files, not a filesystem recursion limit. Zero disables graph expansion.", Default: 0, HasDefault: true},
		"includeBacklinks":  {Description: "Include incoming-link metadata for returned files.", Default: false, HasDefault: true},
		"dedupe":            {Description: "Deduplicate full content delivered to JavaScript, before script projection. Use false for a targeted reread when the model needs previously omitted evidence.", Default: true, HasDefault: true},
	}, "Use inputs for a first page or only continuationToken for later pages; the token carries the original paging and content settings.")
}

func codeFileContextDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("file_context", "file-context", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.FileContextTool), 6), 10, AgentCLICategoryContext)
	c := contract("Build bounded context for selected files or directories.", input(map[string]codeSchema{"files": stringsSchema(), "profile": str(), "intent": str(), "exclude_note_paths": stringsSchema(), "exclude_doc_paths": stringsSchema(), "anchorKinds": stringsSchema(), "ensureLinkTargets": enumeration("never", "plan", "apply"), "submoduleDepth": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean()}), schemaForType(reflect.TypeOf(mcpapi.ContextTextResponse{}), map[reflect.Type]bool{}), map[string]any{"files": []string{"pkg/app/agentapi/catalog.go"}}, "reads files and indexed context", "agent CLI rejects apply")
	return completeCodeDescriptor(d, c, []string{"agent file-context"}, map[string]codeInputFieldMetadata{
		"files":             {Description: "Files or directories whose bounded context is requested."},
		"ensureLinkTargets": {Description: "Whether to avoid link-target work, plan it, or apply it. Code-mode rejects apply."},
	}, "Context is bounded around the requested files or directories; link-target apply is unavailable in code mode.")
}

func codeVaultContextDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("vault_context", "vault-context", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.VaultContextTool), 24), 11, AgentCLICategoryContext)
	c := contract("Build bounded repository or vault context.", input(map[string]codeSchema{"profile": enumeration("code", "vault"), "intent": str(), "contextFiles": stringsSchema(), "files": stringsSchema(), "submoduleDepth": integer(), "skipAnchors": boolean(), "skipEmbeds": boolean(), "includeTags": boolean(), "recencyCascade": boolean(), "graphSummary": boolean(), "includeOntology": boolean(), "requestScope": enumeration("minimal_bootstrap", "indexed_bootstrap", "rich"), "keyPatterns": stringsSchema()}), schemaForType(reflect.TypeOf(mcpapi.ContextTextResponse{}), map[reflect.Type]bool{}), map[string]any{"profile": "code", "files": []string{"pkg/app/agentapi"}}, "reads repository guidance and requested indexed context")
	return completeCodeDescriptor(d, c, []string{"agent vault-context"}, nil, "Choose a profile and explicit selectors when richer indexed context is needed; the request scope controls bootstrap behavior.")
}

func codeSemanticQueryDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("semantic_query", "semantic-query", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.SemanticQueryTool), 21), 5, AgentCLICategoryDiscovery)
	d.validateCodeInput = func(value map[string]any) error {
		if _, present := value["queries"]; !present {
			return fmt.Errorf("input.queries is required; continuation requests repeat the same queries")
		}
		return nil
	}
	c := contract("Search notes, docs, and indexed code.", input(map[string]codeSchema{"queries": codeSchema{"type": "array", "items": codeSchema{"oneOf": []any{str(), object([]string{"text"}, map[string]codeSchema{"text": str(), "mode": str()})}}}, "paths": stringsSchema(), "types": stringsSchema(), "scope": enumeration("all", "code", "docs", "tests"), "pathPrefix": str(), "folder": str(), "noteType": str(), "mode": str(), "continuationToken": str(), "limit": integer(), "explain": boolean(), "requireExactSymbol": boolean(), "includeTests": boolean(), "excludeNotes": boolean(), "timings": boolean(), "compact": boolean()}), schemaForType(reflect.TypeOf(mcpapi.SemanticQueryResponse{}), map[reflect.Type]bool{}), map[string]any{"queries": []string{"persistent code mode"}, "scope": "code"}, "reads search and code indexes", "provide queries on every page; continuation requests repeat the same queries")
	return completeCodeDescriptor(d, c, []string{"agent semantic-query"}, map[string]codeInputFieldMetadata{
		"queries":           {Description: "Search text or per-query {text, mode} objects. Required; continuation requests repeat the same queries."},
		"continuationToken": {Description: "Opaque token from a prior response. Send it with the same queries, seeds, mode, and controls; a changed request, index, or result window returns a continuation_stale or invalid continuationToken error."},
		"scope":             {Description: "Restrict results to all, code, docs, or tests."},
		"paths":             {Description: "File or directory anchors for retrieval."},
		"pathPrefix":        {Description: "Restrict results to this exact vault-relative path or its descendants."},
		"folder":            {Description: "Alias for pathPrefix."},
		"noteType":          {Description: "Restrict note results by the owning note's resolved ontology type."},
		"compact":           {Description: "Return each selected body once under compact.sources, with compact.roles referring to those sources. Preserve lane, warning, and continuation metadata.", Default: false, HasDefault: true},
		"mode":              {Description: "Retrieval mode for plain-string queries. Do not combine the top-level mode argument with per-query modes."},
		"includeTests":      {Description: "Omitting this field or setting true leaves test code eligible. Explicit false excludes test code outside the tests scope."},
	}, "A continuation token preserves search controls and rejects conflicting replacements; inspect warnings and lane status before relying on results.")
}

func codeFindConnectionsDescriptor() ToolDescriptor {
	d := agentCLI(agentCapability(descriptor("find_connections", "find-connections", SurfaceAgentAPI|SurfaceAgentCLI|SurfaceCapabilities, MutationNever, mcpapi.FindConnectionsTool), 8), 17, AgentCLICategoryDiscovery)
	d.validateCodeInput = func(value map[string]any) error {
		if stringValue(value["note"]) == "" && stringValue(value["text"]) == "" {
			return fmt.Errorf("input.note or input.text is required")
		}
		return nil
	}

	chunk := object([]string{"heading", "breadcrumb", "chunkIndex", "score", "queryHeading", "queryBreadcrumb", "queryChunkIndex", "queryText"}, map[string]codeSchema{"heading": str(), "breadcrumb": str(), "chunkIndex": integer(), "score": {"type": "number"}, "queryHeading": str(), "queryBreadcrumb": str(), "queryChunkIndex": integer(), "queryText": str(), "text": str()})
	match := object([]string{"path", "title", "score", "heading", "breadcrumb", "chunkIndex", "queryHeading", "queryBreadcrumb", "queryChunkIndex", "queryText", "reason", "chunks"}, map[string]codeSchema{"path": str(), "title": str(), "score": {"type": "number"}, "heading": str(), "breadcrumb": str(), "chunkIndex": integer(), "queryHeading": str(), "queryBreadcrumb": str(), "queryChunkIndex": integer(), "queryText": str(), "reason": str(), "text": str(), "chunks": {"type": "array", "items": chunk}})

	c := contract("Find related notes for a note path or text.", input(map[string]codeSchema{"note": str(), "text": str(), "limit": integer()}), object([]string{"inputType", "matches", "count"}, map[string]codeSchema{"inputType": enumeration("note", "text"), "matches": {"type": "array", "items": match}, "count": integer(), "sessionId": str(), "dedupeHits": integer(), "note": str(), "skipped": integer()}), map[string]any{"note": "docs/specs/technical/persistent-agent-code-mode.md"}, "reads graph and semantic indexes")
	return completeCodeDescriptor(d, c, []string{"agent find-connections"}, map[string]codeInputFieldMetadata{
		"note":  {Description: "Repository-relative note path. Supply exactly one of note or text."},
		"text":  {Description: "Ad hoc text to embed. Supply exactly one of note or text."},
		"limit": {Description: "Maximum related notes returned.", Default: 25, HasDefault: true},
	}, "Provide exactly one of note or text. A note is resolved from stored note chunks; text is embedded for this request.")
}
