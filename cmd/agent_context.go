package cmd

import (
	"fmt"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/spf13/cobra"
)

func newAgentFileContextCmd() *cobra.Command {
	var profile, intent, ensureLinkTargets string
	var files, excludeNotePaths, excludeDocPaths, anchorKinds []string
	var budgetChars, submoduleDepth int
	cmd := &cobra.Command{
		Use:   "file-context",
		Short: "Build file context as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetString(payload, "profile", profile)
			maybeSetString(payload, "intent", intent)
			maybeSetStrings(payload, "files", files)
			maybeSetStrings(payload, "exclude_note_paths", excludeNotePaths)
			maybeSetStrings(payload, "exclude_doc_paths", excludeDocPaths)
			maybeSetStrings(payload, "anchorKinds", anchorKinds)
			ensureMode, err := actions.ParseEnsureLinkTargetMode(ensureLinkTargets)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			if ensureMode == ontology.EnsureLinkTargetApply {
				writeAgentError(fmt.Errorf("ensure-link-targets=apply is only supported by local graph file-context or read-write MCP"))
				return silentExitError{code: 1}
			}
			if ensureMode != ontology.EnsureLinkTargetNever {
				payload["ensureLinkTargets"] = string(ensureMode)
			}
			maybeSetInt(payload, "submoduleDepth", submoduleDepth)
			maybeSetBool(cmd, payload, "skipAnchors", skipAnchors)
			maybeSetBool(cmd, payload, "skipEmbeds", skipEmbeds)
			return runAgentJSONTool(cmd, budgetChars, "file_context", payload)
		},
	}
	addBudgetFlag(cmd, &budgetChars)
	cmd.Flags().StringVar(&profile, "profile", "", "context profile")
	cmd.Flags().StringVar(&intent, "intent", "", "task intent")
	cmd.Flags().StringVar(&ensureLinkTargets, "ensure-link-targets", "", "embedded node link repair mode: never|plan|apply")
	cmd.Flags().StringArrayVar(&files, "file", nil, "file or directory (repeatable)")
	cmd.Flags().StringArrayVar(&excludeNotePaths, "exclude-note-path", nil, "exclude linked note path")
	cmd.Flags().StringArrayVar(&excludeDocPaths, "exclude-doc-path", nil, "exclude ancestor doc path")
	cmd.Flags().StringArrayVar(&anchorKinds, "anchor-kind", nil, "anchor kind filter")
	cmd.Flags().IntVar(&submoduleDepth, "submodule-depth", 0, "submodule doc depth")
	cmd.Flags().BoolVar(&skipAnchors, "skip-anchors", false, "skip wikilinks with anchors")
	cmd.Flags().BoolVar(&skipEmbeds, "skip-embeds", false, "skip embedded wikilinks")
	return cmd
}

func newAgentVaultContextCmd() *cobra.Command {
	var profile, intent string
	var contextFiles, files []string
	var budgetChars, submoduleDepth int
	var includeTags, recencyCascade, graphSummary bool
	cmd := &cobra.Command{
		Use:   "vault-context",
		Short: "Build vault context as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetString(payload, "profile", profile)
			maybeSetString(payload, "intent", intent)
			maybeSetStrings(payload, "contextFiles", contextFiles)
			maybeSetStrings(payload, "files", files)
			maybeSetInt(payload, "submoduleDepth", submoduleDepth)
			maybeSetBool(cmd, payload, "skipAnchors", skipAnchors)
			maybeSetBool(cmd, payload, "skipEmbeds", skipEmbeds)
			maybeSetBool(cmd, payload, "includeTags", includeTags)
			maybeSetBool(cmd, payload, "recencyCascade", recencyCascade)
			maybeSetBool(cmd, payload, "graphSummary", graphSummary)
			return runAgentJSONTool(cmd, budgetChars, "vault_context", payload)
		},
	}
	addBudgetFlag(cmd, &budgetChars)
	cmd.Flags().StringVar(&profile, "profile", "", "context profile")
	cmd.Flags().StringVar(&intent, "intent", "", "task intent")
	cmd.Flags().StringArrayVar(&contextFiles, "context-file", nil, "context file path")
	cmd.Flags().StringArrayVar(&files, "file", nil, "file or directory to include targeted context for")
	cmd.Flags().IntVar(&submoduleDepth, "submodule-depth", 0, "submodule doc depth for directory targets")
	cmd.Flags().BoolVar(&skipAnchors, "skip-anchors", false, "skip wikilinks with anchors")
	cmd.Flags().BoolVar(&skipEmbeds, "skip-embeds", false, "skip embedded wikilinks")
	cmd.Flags().BoolVar(&includeTags, "include-tags", true, "include tags in graph context")
	cmd.Flags().BoolVar(&recencyCascade, "recency-cascade", true, "include recency cascade")
	cmd.Flags().BoolVar(&graphSummary, "graph-summary", false, "include graph communities/orphans even when ontology is present")
	return cmd
}

func newAgentSemanticQueryCmd() *cobra.Command {
	var continuationToken, mode, scope, pathPrefix, noteType string
	var queries, pathSeeds, types []string
	var budgetChars, limit int
	var explain, requireExactSymbol, includeTests, excludeNotes, compact, timings bool
	cmd := &cobra.Command{
		Use:   "semantic-query",
		Short: "Run semantic query as JSON (inferred broad/local/doc-code routing, with mode override)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if timings {
				ctx := indexingperf.WithCollector(cmd.Context(), indexingperf.NewSemanticQueryCollector())
				ctx = search.WithTimings(ctx, &search.Timings{})
				cmd.SetContext(ctx)
			}
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetString(payload, "continuationToken", continuationToken)
			maybeSetString(payload, "mode", mode)
			maybeSetStrings(payload, "queries", queries)
			maybeSetStrings(payload, "paths", pathSeeds)
			maybeSetStrings(payload, "types", types)
			maybeSetString(payload, "scope", scope)
			maybeSetString(payload, "pathPrefix", pathPrefix)
			maybeSetString(payload, "noteType", noteType)
			maybeSetInt(payload, "limit", limit)
			maybeSetBool(cmd, payload, "explain", explain)
			maybeSetBool(cmd, payload, "requireExactSymbol", requireExactSymbol)
			maybeSetBool(cmd, payload, "includeTests", includeTests)
			maybeSetBool(cmd, payload, "excludeNotes", excludeNotes)
			maybeSetBool(cmd, payload, "compact", compact)
			maybeSetBool(cmd, payload, "timings", timings)
			return runAgentJSONTool(cmd, budgetChars, "semantic_query", payload)
		},
	}
	addBudgetFlag(cmd, &budgetChars)
	addContinuationFlag(cmd, &continuationToken)
	cmd.Flags().StringVar(&mode, "mode", "", "advanced search mode override")
	cmd.Flags().StringVar(&scope, "scope", "", "result scope: all, code, docs, tests")
	cmd.Flags().StringVar(&pathPrefix, "path-prefix", "", "restrict results to a vault-relative path or its descendants")
	cmd.Flags().StringVar(&noteType, "note-type", "", "restrict note results by the owning note ontology type")
	cmd.Flags().StringArrayVar(&queries, "query", nil, "query text (repeatable)")
	cmd.Flags().StringArrayVar(&pathSeeds, "path", nil, "path seed (repeatable)")
	cmd.Flags().StringArrayVar(&types, "type", nil, "result type filter")
	cmd.Flags().IntVar(&limit, "limit", 0, "max matches")
	cmd.Flags().BoolVar(&explain, "explain", false, "include evidence")
	cmd.Flags().BoolVar(&requireExactSymbol, "require-exact-symbol", false, "require exact indexed symbol matches")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "include test files where supported")
	cmd.Flags().BoolVar(&excludeNotes, "exclude-notes", false, "exclude note/doc-only matches")
	cmd.Flags().BoolVar(&compact, "compact", false, "return source bodies once with role references")
	cmd.Flags().BoolVar(&timings, "timings", false, "include semantic-query phase and operation diagnostics")
	return cmd
}

func newAgentCodeSymbolCmd() *cobra.Command {
	var symbol, path, language string
	var contextLines, maxBytes int
	cmd := &cobra.Command{
		Use:   "code-symbol",
		Short: "Resolve an indexed code symbol as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "symbol", symbol)
			maybeSetString(payload, "path", path)
			maybeSetString(payload, "language", language)
			maybeSetInt(payload, "contextLines", contextLines)
			maybeSetInt(payload, "maxBytes", maxBytes)
			return runAgentJSONTool(cmd, 0, "code_symbol", payload)
		},
	}
	cmd.Flags().StringVar(&symbol, "symbol", "", "symbol FQN, suffix, or name")
	cmd.Flags().StringVar(&path, "path", "", "optional path/file prefix")
	cmd.Flags().StringVar(&language, "language", "", "optional language")
	cmd.Flags().IntVar(&contextLines, "context-lines", 3, "source context lines")
	cmd.Flags().IntVar(&maxBytes, "max-bytes", 20000, "maximum snippet bytes")
	_ = cmd.MarkFlagRequired("symbol")
	return cmd
}

func newAgentCodeReferencesCmd() *cobra.Command {
	var symbol, path, language string
	var limit int
	var includeCallers, includeCallees, includeDefinitions bool
	cmd := &cobra.Command{
		Use:   "code-references",
		Short: "Return indexed callers and callees for a code symbol as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "symbol", symbol)
			maybeSetString(payload, "path", path)
			maybeSetString(payload, "language", language)
			maybeSetInt(payload, "limit", limit)
			payload["includeCallers"] = includeCallers
			payload["includeCallees"] = includeCallees
			payload["includeDefinitions"] = includeDefinitions
			return runAgentJSONTool(cmd, 0, "code_references", payload)
		},
	}
	cmd.Flags().StringVar(&symbol, "symbol", "", "symbol FQN, suffix, or name")
	cmd.Flags().StringVar(&path, "path", "", "optional path/file prefix")
	cmd.Flags().StringVar(&language, "language", "", "optional language")
	cmd.Flags().BoolVar(&includeCallers, "include-callers", true, "include callers")
	cmd.Flags().BoolVar(&includeCallees, "include-callees", true, "include callees")
	cmd.Flags().BoolVar(&includeDefinitions, "include-definitions", true, "include definitions")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum callers/callees")
	_ = cmd.MarkFlagRequired("symbol")
	return cmd
}

func newAgentCodeSymbolContextCmd() *cobra.Command {
	var symbol, path, language string
	var includeTests bool
	var budgetChars int
	cmd := &cobra.Command{
		Use:   "code-symbol-context",
		Short: "Build a compact evidence packet for a code symbol as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "symbol", symbol)
			maybeSetString(payload, "path", path)
			maybeSetString(payload, "language", language)
			maybeSetBool(cmd, payload, "includeTests", includeTests)
			maybeSetInt(payload, "budgetChars", budgetChars)
			return runAgentJSONTool(cmd, budgetChars, "code_symbol_context", payload)
		},
	}
	addBudgetFlag(cmd, &budgetChars)
	cmd.Flags().StringVar(&symbol, "symbol", "", "symbol FQN, suffix, or name")
	cmd.Flags().StringVar(&path, "path", "", "optional path/file prefix")
	cmd.Flags().StringVar(&language, "language", "", "optional language")
	cmd.Flags().BoolVar(&includeTests, "include-tests", true, "include matching test snippets")
	_ = cmd.MarkFlagRequired("symbol")
	return cmd
}

func newAgentFindConnectionsCmd() *cobra.Command {
	var notePath, text string
	var limit, budgetChars int
	cmd := &cobra.Command{
		Use:   "find-connections",
		Short: "Find connected notes as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetString(payload, "note", notePath)
			maybeSetString(payload, "text", text)
			maybeSetInt(payload, "limit", limit)
			return runAgentJSONTool(cmd, budgetChars, "find_connections", payload)
		},
	}
	addBudgetFlag(cmd, &budgetChars)
	cmd.Flags().StringVar(&notePath, "note", "", "note path")
	cmd.Flags().StringVar(&text, "text", "", "ad-hoc text input")
	cmd.Flags().IntVar(&limit, "limit", 0, "max related notes")
	return cmd
}

func newAgentGraphPathCmd() *cobra.Command {
	var fromPath, toPath string
	var maxHops int
	cmd := &cobra.Command{
		Use:   "graph-path",
		Short: "Find shortest path between two notes or code files as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetString(payload, "from", fromPath)
			maybeSetString(payload, "to", toPath)
			maybeSetInt(payload, "maxHops", maxHops)
			return runAgentJSONTool(cmd, 0, "graph_path", payload)
		},
	}
	cmd.Flags().StringVar(&fromPath, "from", "", "source note or code file (name or path)")
	cmd.Flags().StringVar(&toPath, "to", "", "target note or code file (name or path)")
	cmd.Flags().IntVar(&maxHops, "max-hops", 8, "maximum path length")
	return cmd
}
