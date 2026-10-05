package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAgentFilesCmd() *cobra.Command {
	var continuationToken, includeContent, intent string
	var inputs, suppress []string
	var limit, maxDepth, budgetChars int
	var dedupe bool
	var includeFrontmatter, includeBacklinks, absolutePaths bool
	cmd := &cobra.Command{
		Use:   "files",
		Short: "List files and optionally include content as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetString(payload, "continuationToken", continuationToken)
			maybeSetStrings(payload, "inputs", inputs)
			maybeSetStrings(payload, "suppressTags", suppress)
			maybeSetString(payload, "includeContent", includeContent)
			maybeSetString(payload, "intent", intent)
			maybeSetInt(payload, "limit", limit)
			maybeSetInt(payload, "maxDepth", maxDepth)
			maybeSetInt(payload, "budgetChars", budgetChars)
			maybeSetBool(cmd, payload, "dedupe", dedupe)
			maybeSetBool(cmd, payload, "skipAnchors", skipAnchors)
			maybeSetBool(cmd, payload, "skipEmbeds", skipEmbeds)
			maybeSetBool(cmd, payload, "includeFrontmatter", includeFrontmatter)
			maybeSetBool(cmd, payload, "includeBacklinks", includeBacklinks)
			maybeSetBool(cmd, payload, "absolutePaths", absolutePaths)
			return runAgentJSONTool(cmd, budgetChars, "files", payload)
		},
	}
	addContinuationFlag(cmd, &continuationToken)
	addBudgetFlag(cmd, &budgetChars)
	cmd.Flags().StringArrayVar(&inputs, "input", nil, "input patterns (repeatable)")
	cmd.Flags().StringVar(&includeContent, "include-content", "", "content mode: true, false, or compress")
	cmd.Flags().StringVar(&intent, "intent", "", "task intent for compression")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of files")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 0, "wikilink traversal depth")
	cmd.Flags().BoolVar(&dedupe, "dedupe", true, "skip content already sent in this session")
	cmd.Flags().BoolVar(&skipAnchors, "skip-anchors", false, "skip wikilinks with anchors")
	cmd.Flags().BoolVar(&skipEmbeds, "skip-embeds", false, "skip embedded wikilinks")
	cmd.Flags().BoolVar(&includeFrontmatter, "include-frontmatter", false, "include frontmatter")
	cmd.Flags().BoolVar(&includeBacklinks, "include-backlinks", false, "include backlinks")
	cmd.Flags().BoolVar(&absolutePaths, "absolute-paths", false, "include absolute paths")
	cmd.Flags().StringArrayVar(&suppress, "tool-suppress-tag", nil, "additional suppress tags for this call")
	return cmd
}

func newAgentListTagsCmd() *cobra.Command {
	var match []string
	cmd := &cobra.Command{
		Use:   "list-tags",
		Short: "List tags as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetStrings(payload, "match", match)
			return runAgentJSONTool(cmd, 0, "list_tags", payload)
		},
	}
	cmd.Flags().StringArrayVar(&match, "match", nil, "match filters")
	return cmd
}

func newAgentListPropertiesCmd() *cobra.Command {
	var source string
	var match, only []string
	var valueLimit, maxValues int
	var excludeTags, verbose, valueCounts bool
	cmd := &cobra.Command{
		Use:   "list-properties",
		Short: "List properties as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetString(payload, "source", source)
			maybeSetStrings(payload, "match", match)
			maybeSetStrings(payload, "only", only)
			maybeSetInt(payload, "valueLimit", valueLimit)
			maybeSetInt(payload, "maxValues", maxValues)
			maybeSetBool(cmd, payload, "excludeTags", excludeTags)
			maybeSetBool(cmd, payload, "verbose", verbose)
			maybeSetBool(cmd, payload, "valueCounts", valueCounts)
			return runAgentJSONTool(cmd, 0, "list_properties", payload)
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "property source: all, frontmatter, inline")
	cmd.Flags().StringArrayVar(&match, "match", nil, "match filters")
	cmd.Flags().StringArrayVar(&only, "only", nil, "restrict to these property names")
	cmd.Flags().IntVar(&valueLimit, "value-limit", 0, "value limit")
	cmd.Flags().IntVar(&maxValues, "max-values", 0, "maximum tracked values")
	cmd.Flags().BoolVar(&excludeTags, "exclude-tags", false, "exclude tags property")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "include mixed-type enums")
	cmd.Flags().BoolVar(&valueCounts, "value-counts", true, "include per-value counts")
	return cmd
}

func newAgentCommunityListCmd() *cobra.Command {
	var maxCommunities, maxTopNotes int
	cmd := &cobra.Command{
		Use:   "community-list",
		Short: "List communities as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetInt(payload, "maxCommunities", maxCommunities)
			maybeSetInt(payload, "maxTopNotes", maxTopNotes)
			return runAgentJSONTool(cmd, 0, "community_list", payload)
		},
	}
	cmd.Flags().IntVar(&maxCommunities, "max-communities", 0, "max communities")
	cmd.Flags().IntVar(&maxTopNotes, "max-top-notes", 0, "max top notes per community")
	return cmd
}

func newAgentReportCmd() *cobra.Command {
	var op string
	var paths []string
	var limit, minLines int
	var includeTests bool
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Run a code intelligence report as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if op == "" {
				writeAgentError(fmt.Errorf("op is required"))
				return silentExitError{code: 1}
			}
			payload := map[string]any{}
			maybeSetString(payload, "op", op)
			maybeSetStrings(payload, "paths", paths)
			maybeSetInt(payload, "limit", limit)
			maybeSetInt(payload, "minLines", minLines)
			maybeSetBool(cmd, payload, "includeTests", includeTests)
			return runAgentJSONTool(cmd, 0, "report", payload)
		},
	}
	cmd.Flags().StringVar(&op, "op", "", "report op")
	cmd.Flags().StringArrayVar(&paths, "path", nil, "path prefixes or roots")
	cmd.Flags().IntVar(&limit, "limit", 0, "max rows")
	cmd.Flags().IntVar(&minLines, "min-lines", 0, "minimum span for complexity")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "include test files in relatedness")
	return cmd
}

func newAgentVaultHealthCmd() *cobra.Command {
	var include []string
	var staleDays, limit int
	var includeImages bool
	cmd := &cobra.Command{
		Use:   "vault-health",
		Short: "Run vault health checks as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetStrings(payload, "include", include)
			maybeSetInt(payload, "staleDays", staleDays)
			maybeSetInt(payload, "limit", limit)
			maybeSetBool(cmd, payload, "skipAnchors", skipAnchors)
			maybeSetBool(cmd, payload, "skipEmbeds", skipEmbeds)
			maybeSetBool(cmd, payload, "includeImages", includeImages)
			return runAgentJSONTool(cmd, 0, "vault_health", payload)
		},
	}
	cmd.Flags().StringArrayVar(&include, "include", nil, "metrics to include")
	cmd.Flags().IntVar(&staleDays, "stale-days", 0, "days before a note is stale")
	cmd.Flags().IntVar(&limit, "limit", 0, "max items per category")
	cmd.Flags().BoolVar(&skipAnchors, "skip-anchors", false, "skip wikilinks with anchors")
	cmd.Flags().BoolVar(&skipEmbeds, "skip-embeds", false, "skip embedded wikilinks")
	cmd.Flags().BoolVar(&includeImages, "include-images", false, "include image links")
	return cmd
}
