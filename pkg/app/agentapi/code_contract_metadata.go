package agentapi

import (
	"encoding/json"
	"fmt"
)

// codeInputFieldMetadata adds agent-facing meaning to the small schema subset
// emitted by code_contracts.go. It is descriptive only: ValidateCodeInput
// continues to own the executable input rules.
type codeInputFieldMetadata struct {
	Description string
	Default     any
	HasDefault  bool
}

var commonCodeInputFieldMetadata = map[string]codeInputFieldMetadata{
	"sessionId": {
		Description: "Optional session identifier. The initialized client or per-call CallOptions sessionId can supply or replace it.",
	},
	"budgetChars": {
		Description: "Optional response budget for operations that support a content budget; omission leaves the operation's runtime budget in effect.",
	},
}

var codeOperationInputMetadata = map[string]map[string]codeInputFieldMetadata{
	"check_paths": {"paths": {Description: "1..10000 path entries. Paths are vault-relative or absolute within the vault. Set isDir for directory rules. Includes root/nested gitignore, .rhizome/ignore and configured exclusions."}},
	"evaluate_batch": {
		"items":       {Description: "1..128 independent states and question maps with unique nonempty IDs. Results preserve input order."},
		"concurrency": {Description: "Maximum concurrent upstream requests within this batch, 1..32.", Default: 8, HasDefault: true},
		"budgetMs":    {Description: "Batch scheduling budget in milliseconds, 1..300000. Returns partial results before the call deadline. Set CallOptions.timeoutMs longer than this budget.", Default: 20000, HasDefault: true},
	},
	"evaluate": {
		"state":     {Description: "Text, object, or array containing the evidence to evaluate. Send only content needed for the decision."},
		"questions": {Description: "Independent named choice, score, or noul questions against the same state. IDs match answers; put instructions in each question, not in its ID."},
		"model":     {Description: "TypeSafe model ID. Omission uses jev-latest.", Default: "jev-latest", HasDefault: true},
	},
	"files": {
		"inputs":            {Description: "File/directory paths or selectors such as tag:project and find:name. AND, OR, NOT and parentheses combine selectors; adjacent selectors use OR. Required unless continuationToken is supplied."},
		"continuationToken": {Description: "Opaque token from a previous files payload. It restores that request's selection and paging options."},
		"includeContent":    {Description: "Include file content; use \"compress\" to request compression or false for metadata only.", Default: true, HasDefault: true},
		"limit":             {Description: "Maximum returned files. The runtime defaults to 25 with content and 500 without content."},
		"maxDepth":          {Description: "Additional graph traversal depth around selected files, not a filesystem recursion limit. Zero disables graph expansion.", Default: 0, HasDefault: true},
		"includeBacklinks":  {Description: "Include incoming-link metadata for returned files.", Default: false, HasDefault: true},
		"dedupe":            {Description: "Deduplicate full content delivered to JavaScript, before script projection. Use false for a targeted reread when the model needs previously omitted evidence.", Default: true, HasDefault: true},
	},
	"file_context": {
		"files":             {Description: "Files or directories whose bounded context is requested."},
		"ensureLinkTargets": {Description: "Whether to avoid link-target work, plan it, or apply it. Code-mode rejects apply."},
	},
	"semantic_query": {
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
	},
	"code_symbol": {
		"contextLines": {Description: "Source lines around a resolved definition.", Default: 3, HasDefault: true},
		"maxBytes":     {Description: "Maximum source bytes returned for the definition.", Default: 20000, HasDefault: true},
	},
	"code_references": {
		"limit":              {Description: "Maximum callers and maximum callees returned independently.", Default: 20, HasDefault: true},
		"includeDefinitions": {Description: "Include the resolved definition.", Default: true, HasDefault: true},
		"includeCallers":     {Description: "Include indexed callers.", Default: true, HasDefault: true},
		"includeCallees":     {Description: "Include indexed callees.", Default: true, HasDefault: true},
	},
	"code_symbol_context": {
		"includeTests": {Description: "Include indexed test snippets in the evidence packet; set false to omit them.", Default: true, HasDefault: true},
	},
	"find_connections": {
		"note":  {Description: "Repository-relative note path. Supply exactly one of note or text."},
		"text":  {Description: "Ad hoc text to embed. Supply exactly one of note or text."},
		"limit": {Description: "Maximum related notes returned.", Default: 25, HasDefault: true},
	},
	"list_properties": {
		"source":     {Description: "Property source to inspect.", Default: "all", HasDefault: true},
		"valueLimit": {Description: "Values shown per property: normally 25; verbose raises this to at least 50. When only selects properties and valueLimit is omitted, that rule is replaced by maxValues minus one (or maxValues when it is 1)."},
		"maxValues":  {Description: "Maximum distinct values considered: initially 500 when omitted or nonpositive, then raised to at least the effective valueLimit plus one."},
	},
	"vault_health": {
		"staleDays": {Description: "Age threshold for stale-note reporting, in days.", Default: 90, HasDefault: true},
		"limit":     {Description: "Maximum entries returned for each report category.", Default: 25, HasDefault: true},
	},
	"ontology_query_schema": {
		"type": {Description: "Optional type name. Omit for the full executable schema; supply it for one type fragment and root arguments."},
	},
	"ontology_query": {
		"query":     {Description: "Read-only GraphQL query text."},
		"variables": {Description: "Optional JSON object of GraphQL variables."},
	},
	"query_recipe": {
		"op":     {Description: "Recipe operation: list, validate, or run."},
		"id":     {Description: "Recipe identifier for run. It may be omitted only when exactly one recipe is available."},
		"inputs": {Description: "Named JSON values for a run; values are converted to recipe input strings."},
		"anchor": {Description: "Additional anchor values merged into a run's inputs."},
	},
	"node_link": {
		"refs":    {Description: "Inline node references to resolve. Supply refs or targets."},
		"targets": {Description: "Explicit node targets to resolve. Supply refs or targets."},
		"ensure":  {Description: "Choose never or plan when resolving targets. apply is rejected in code mode."},
	},
	"note_rename_heading": {
		"apply":            {Description: "Request an applied rename. Code mode remains plan-only and rejects true.", Default: false, HasDefault: true},
		"upgradeToBlockId": {Description: "Heading-link upgrade strategy.", Default: "auto", HasDefault: true},
		"fallback":         {Description: "Fallback link strategy.", Default: "block-id", HasDefault: true},
	},
	"start": {
		"profile":        {Description: "Bootstrap profile: code or vault."},
		"includeTags":    {Description: "Include tag context in bootstrap output.", Default: true, HasDefault: true},
		"recencyCascade": {Description: "Include recency-cascade graph context when applicable.", Default: true, HasDefault: true},
	},
	"ontology_reference": {
		"type":    {Description: "Type to document. Required when compact is true."},
		"compact": {Description: "Render the compact authoring contract; it requires type.", Default: false, HasDefault: true},
	},
	"view": {
		"action":           {Description: "View operation: list, show, validate, run, or eject."},
		"id":               {Description: "View identifier. Required for show, run, and eject."},
		"path":             {Description: "Optional view YAML file or directory; omission uses the configured view location."},
		"inputs":           {Description: "Named JSON values passed to a run; values are converted to view input strings."},
		"omitCapabilities": {Description: "Leave the per-field filter, sort, and edit capability catalog out of a run's response; rows, columns, groups, profile, stats, and warnings remain.", Default: false, HasDefault: true},
	},
	"validate": {
		"action":    {Description: "Validation operation: list, run, or fix."},
		"selector":  {Description: "Optional named validation selector; omission uses the workflow default for run or fix."},
		"maxIssues": {Description: "Maximum issues returned by the code-mode validation workflow.", Default: 20, HasDefault: true},
	},
	"current_user": {
		"action":           {Description: "Identity operation: show, set, or validate."},
		"personTitleOrRef": {Description: "Person title or reference. Required for set, which requires read-write authority."},
	},
	"next_id": {
		"type":  {Description: "Typed note family whose identifiers are allocated."},
		"count": {Description: "Number of identifiers to return; the runtime caps it at the allocator batch maximum.", Default: 1, HasDefault: true},
	},
	"surface": {
		"command": {Description: "Optional exact agent command path to select; omit for the complete surface."},
	},
	"note_move": {
		"source":          {Description: "One source path. Use with target, or combine sources with toFolder."},
		"sources":         {Description: "Multiple source paths. Requires toFolder and read-write authority."},
		"target":          {Description: "Destination for exactly one source; cannot be combined with toFolder."},
		"toFolder":        {Description: "Destination folder for sources; cannot be combined with target."},
		"updateBacklinks": {Description: "Rewrite backlinks while moving.", Default: true, HasDefault: true},
	},
}

var codeOperationInterpretations = map[string]string{
	"check_paths":              "One result per input, in order. allowed means allowed by ignore rules, not selected as a note or supported as code. invalid is never equivalent to allowed. Rule attribution includes negations and ignored ancestors. sessionId and budgetChars do not alter decisions.",
	"evaluate_batch":           "Questions within each item are independent against its state. Item statuses distinguish succeeded, failed, not_started and uncertain. Check every status even when the operation succeeds. Checkpoint successful results; never automatically replay uncertain items. Usage totals include validated successes only, not unknown usage from failures. Throttle coordination is per batch, not global across processes. budgetChars and sessionId do not truncate or deduplicate evaluations.",
	"evaluate":                 "Questions run independently against the same state. Choice returns an option and distribution; score returns the expected zero-based rubric level, distribution and legend; noul returns the probability of yes. Inspect answer.type before reading variant fields. Confidence describes distribution concentration, not verified correctness. Batch related questions for one state; use separate calls for different states. API failures return a failed outcome. budgetChars and sessionId do not truncate or deduplicate evaluation.",
	"files":                    "Use inputs for a first page or only continuationToken for later pages; the token carries the original paging and content settings.",
	"file_context":             "Context is bounded around the requested files or directories; link-target apply is unavailable in code mode.",
	"vault_context":            "Choose a profile and explicit selectors when richer indexed context is needed; the request scope controls bootstrap behavior.",
	"semantic_query":           "A continuation token preserves search controls and rejects conflicting replacements; inspect warnings and lane status before relying on results.",
	"code_symbol":              "Resolution can be ambiguous or unavailable; read status, candidates, coverage, and warnings before assuming a definition was found.",
	"code_references":          "Caller and callee lists are independently limited and may be omitted by their include flags.",
	"code_symbol_context":      "The result is a compact indexed evidence packet. Test snippets are included by default; set includeTests to false to omit them.",
	"external_references":      "Use an exact external handle when known. Structured ecosystem, module, and symbolPrefix selectors may resolve a target or return ambiguous candidates; inspect status and warnings.",
	"find_connections":         "Provide exactly one of note or text. A note is resolved from stored note chunks; text is embedded for this request.",
	"graph_path":               "Both from and to are required; maxHops bounds the typed graph traversal.",
	"list_tags":                "Match filters the note set before tag summaries are calculated.",
	"list_properties":          "Source selects frontmatter, inline, or both; value limits bound the values retained in each summary.",
	"community_list":           "Community and representative-note limits bound graph-analysis output.",
	"report":                   "op selects one report family; paths and limits narrow its indexed code evidence.",
	"vault_health":             "Each health category is truncated independently by limit; staleDays controls only stale-note classification.",
	"ontology_query_schema":    "Without type this returns full discovery; with type it returns the selected fragment and root arguments needed to query it.",
	"ontology_query":           "The GraphQL payload may contain execution errors and returns exitCode 1 when it does; query data remains dynamic. Generated typed roots add independent coverage under extensions.typedRoots[responseAlias]; use offset for continuation where nextOffset is present.",
	"query_recipe":             "list and validate operate on discovered recipes; run selects a recipe, merges inputs and anchors, then executes its compiled query.",
	"node_link":                "refs or targets is required. Code mode supports never and plan; ensure=apply is rejected even on a read-write connection.",
	"note_rename_heading":      "This operation only plans a rename in code mode; apply=true returns a failed outcome.",
	"start":                    "Bootstrap creates or reuses a session and returns compact code discovery plus bounded context; richer selectors make indexed work explicit.",
	"ontology_reference":       "compact=true is an authoring contract for one type, so type is required in that mode.",
	"ontology_authoring_guide": "Supply type or types to focus the generated authoring guidance.",
	"ontology_inspect":         "inputs is required and selects ontology inputs to inspect against the live projection.",
	"view":                     "show, run, and eject require id; list and validate operate on the selected view catalog; eject needs read-write.",
	"validate":                 "fix only applies changes when apply=true and the connection has read-write authority; outcomes retain selector and remediation evidence.",
	"current_user":             "set requires personTitleOrRef and a read-write connection; show and validate are read-only identity checks.",
	"next_id":                  "Returned identifiers are not reservations; allocation may refresh metadata while reading identifier claims.",
	"code_rationale":           "path narrows extracted rationale comments; kinds filters the comment classifications returned.",
	"surface":                  "Omit command for all agent command metadata or select one exact command path.",
	"note_move":                "Use source plus target for one move, or sources plus toFolder for a batch; execution requires a read-write connection.",
}

func applyCodeContractMetadata(operation string, contract *CodeOperationContract) {
	var schema map[string]any
	if err := json.Unmarshal(contract.InputSchema, &schema); err != nil {
		panic(fmt.Sprintf("decode code contract input schema for %s: %v", operation, err))
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("code contract input schema for %s has no properties", operation))
	}
	metadata := make(map[string]codeInputFieldMetadata, len(commonCodeInputFieldMetadata)+len(codeOperationInputMetadata[operation]))
	for name, value := range commonCodeInputFieldMetadata {
		metadata[name] = value
	}
	for name, value := range codeOperationInputMetadata[operation] {
		metadata[name] = value
	}
	for name, value := range metadata {
		property, ok := properties[name].(map[string]any)
		if !ok {
			panic(fmt.Sprintf("code contract metadata for %s references missing input %s", operation, name))
		}
		property["description"] = value.Description
		if value.HasDefault {
			property["default"] = value.Default
		}
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("encode code contract input schema for %s: %v", operation, err))
	}
	contract.InputSchema = payload
	if interpretation := codeOperationInterpretations[operation]; interpretation != "" {
		contract.Interpretation = append(contract.Interpretation, interpretation)
	}
}
