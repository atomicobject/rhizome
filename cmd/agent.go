package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/spf13/cobra"
)

// agentCLIBudgetFloor keeps standalone agent commands useful without requiring
// every caller to remember the larger context budget normally supplied by MCP
// clients.
const agentCLIBudgetFloor = 150000

var agentCmd = &cobra.Command{
	Use:           "agent",
	Short:         "Machine-oriented commands for agents",
	SilenceUsage:  true,
	SilenceErrors: true,
}

var agentSessionID string

func init() {
	agentCmd.PersistentFlags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")
	agentCmd.PersistentFlags().StringVar(&agentSessionID, "session-id", "", "session id for cross-invocation dedupe")
	agentCmd.PersistentFlags().StringVar(&agentSessionID, "session", "", "alias for --session-id")
	agentCmd.PersistentFlags().StringSliceVar(&suppressTags, "suppress-tags", nil, "additional tags to suppress/exclude from output")
	agentCmd.PersistentFlags().BoolVar(&noSuppress, "no-suppress", false, "disable all tag suppression")
	rootCmd.AddCommand(agentCmd)

	children := []*cobra.Command{
		newAgentSurfaceCmd(),
		newAgentCodeCmd(),
		newAgentStartCmd(),
		newAgentValidateProductCmdWithRunner(newProductionValidationRunner()),
		newAgentNodeLinkCmd(),
		newAgentNoteRenameHeadingCmd(),
		newAgentOntologyQuerySchemaCmd(),
		newAgentOntologyQueryCmd(),
		newQueryRecipeCmd(true),
		newViewCmd(true),
		newAgentOntologyReferenceCmd(),
		newAgentOntologyAuthoringGuideCmd(),
		newAgentOntologyInspectCmd(),
		newAgentNextIDCmd(),
		newAgentCurrentUserCmd(),
		newAgentFilesCmd(),
		newAgentListTagsCmd(),
		newAgentListPropertiesCmd(),
		newAgentCommunityListCmd(),
		newAgentReportCmd(),
		newAgentFileContextCmd(),
		newAgentVaultContextCmd(),
		newAgentSemanticQueryCmd(),
		newAgentCodeSymbolCmd(),
		newAgentCodeReferencesCmd(),
		newAgentCodeSymbolContextCmd(),
		newAgentExternalReferencesCmd(),
		newAgentEvaluateCmd(),
		newAgentJSONStdinCmd("evaluate-batch", "Evaluate independent Jev states from JSON stdin"),
		newAgentJSONStdinCmd("check-paths", "Check a collection of paths against unified ignore rules from JSON stdin"),
		newAgentFindConnectionsCmd(),
		newAgentGraphPathCmd(),
		newAgentCodeRationaleCmd(),
		newAgentVaultHealthCmd(),
	}
	for _, child := range children {
		agentCmd.AddCommand(suppressAgentCommandNoise(child))
	}
	bindAgentRuntimePlanOperations(agentCmd)
}

func runAgentJSONTool(cmd *cobra.Command, budgetChars int, toolName string, args map[string]any) error {
	operationID, err := agentOperationIDFromCommand(cmd)
	if err != nil {
		writeAgentError(err)
		return silentExitError{code: 1}
	}
	cfg, rt, err := prepareAgentJSONTool(cmd.Context(), budgetChars, toolName, operationID, args)
	if err != nil {
		writeAgentError(err)
		return silentExitError{code: 1}
	}
	if rt != nil {
		defer rt.Close()
	}
	payload, err := agentapi.CallJSON(cmd.Context(), cfg, toolName, args)
	if err != nil {
		writeAgentToolError(err)
		return silentExitError{code: 1}
	}
	_, err = cmd.OutOrStdout().Write(append(payload, '\n'))
	return err
}

// prepareAgentJSONTool is the shared CLI/code-mode application boundary. Runtime
// state is request-scoped so a later call sees edits and replacement indexes.
func prepareAgentJSONTool(ctx context.Context, budgetChars int, toolName string, operationID oneshotruntime.OperationID, args map[string]any) (agentapi.Config, *bootstrap.LiveRuntime, error) {
	if err := normalizeAgentRequestPlanArgs(ctx, operationID, args); err != nil {
		return agentapi.Config{}, nil, err
	}
	var finishBootstrap func(error)
	if toolName == "semantic_query" && indexingperf.FromContext(ctx) != nil {
		finishBootstrap = indexingperf.StartSpan(ctx, indexingperf.SemanticQueryPhaseBootstrap)
	}
	declaration, declared := oneshotruntime.DefaultRegistry().Declaration(operationID)
	var plan oneshotruntime.Plan
	var planErr error
	if declared && declaration.StaticPlan != nil {
		plan = *declaration.StaticPlan
	} else {
		plan, planErr = oneshotruntime.RequestPlan(operationID, args)
	}
	var cfg agentapi.Config
	var rt *bootstrap.LiveRuntime
	var err error
	if errors.Is(planErr, oneshotruntime.ErrRequestPlanNotDeclared) {
		if !declared || declaration.PlanSource != oneshotruntime.PlanSourceExempt {
			err = fmt.Errorf("operation %q has no runtime plan or exemption: %w", operationID, planErr)
		} else {
			cfg, rt, err = buildAgentConfigForOperation(ctx, budgetChars, toolName, operationID)
		}
	} else if planErr != nil {
		err = planErr
	} else {
		cfg, rt, err = buildAgentConfigForRequestPlan(ctx, budgetChars, toolName, operationID, plan)
	}
	if finishBootstrap != nil {
		finishBootstrap(err)
	}
	return cfg, rt, err
}

func normalizeAgentRequestPlanArgs(ctx context.Context, operationID oneshotruntime.OperationID, args map[string]any) error {
	if operationID != "agent.files" {
		return oneshotruntime.NormalizeRequestPlanArgs(operationID, args, nil, nil)
	}
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return oneshotruntime.NormalizeRequestPlanArgs(operationID, args, nil, nil)
	}
	vaultPaths, err := paths.NewVaultPaths(vaultDef.BasePath())
	if err != nil {
		return oneshotruntime.NormalizeRequestPlanArgs(operationID, args, nil, nil)
	}
	noteRuntime, err := newNoteFormatRuntime()
	if err != nil {
		return err
	}
	return oneshotruntime.NormalizeRequestPlanArgs(operationID, args, &vaultPaths, &noteRuntime)
}

func newAgentNodeLinkCmd() *cobra.Command {
	var ensure string
	var refs []string
	var targets []string
	cmd := &cobra.Command{
		Use:   "node-link",
		Short: "Return durable wikilinks and locator status for ontology nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			if ensure == "" {
				ensure = string(ontology.EnsureLinkTargetNever)
			}
			if ensure == string(ontology.EnsureLinkTargetApply) {
				writeAgentError(fmt.Errorf(`{"code":"apply_requires_read_write","message":"agent node-link is read-only; use write-capable MCP or local graph file-context for ensure=apply"}`))
				return silentExitError{code: 1}
			}
			payload["ensure"] = ensure
			if len(refs) > 0 {
				items := make([]any, 0, len(refs))
				for _, raw := range refs {
					var obj map[string]any
					if err := json.Unmarshal([]byte(raw), &obj); err == nil && len(obj) > 0 {
						items = append(items, obj)
					} else {
						items = append(items, raw)
					}
				}
				payload["refs"] = items
			}
			if len(targets) > 0 {
				values := make([]any, 0, len(targets))
				for _, target := range targets {
					values = append(values, target)
				}
				payload["targets"] = values
			}
			return runAgentJSONTool(cmd, agentCLIBudgetFloor, "node_link", payload)
		},
	}
	cmd.Flags().StringVar(&ensure, "ensure", string(ontology.EnsureLinkTargetNever), "link-target repair mode: never, plan, apply (apply refused by agent CLI)")
	cmd.Flags().StringArrayVar(&refs, "ref", nil, "canonical NodeRef JSON object or link target string (repeatable)")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "author-facing link target such as path#^block-id or [[path#Heading]] (repeatable)")
	return cmd
}

func writeAgentError(err error) {
	payload := map[string]any{"error": err.Error()}
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fmt.Fprintln(os.Stderr, `{"error":"unknown error"}`)
		return
	}
	fmt.Fprintln(os.Stderr, string(encoded))
}

// writeAgentToolError preserves typed remediation envelopes returned by shared
// MCP handlers. Other tool errors keep the established {"error":"..."}
// command envelope, including existing JSON-shaped error strings.
func writeAgentToolError(err error) {
	var payload map[string]any
	if json.Unmarshal([]byte(err.Error()), &payload) == nil {
		code, hasCode := payload["code"].(string)
		remediation, hasRemediation := payload["remediation"].(string)
		if hasCode && code != "" && hasRemediation && remediation != "" {
			writeAgentJSONError(payload)
			return
		}
	}
	writeAgentError(err)
}

// writeAgentJSONError emits a typed JSON error envelope on stderr. Used by
// commands like `next-id` that surface stable codes (`type_not_found`,
// `unsupported_for_type`) so skills can branch without parsing free-form text.
func writeAgentJSONError(payload map[string]any) {
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fmt.Fprintln(os.Stderr, `{"error":"unknown error"}`)
		return
	}
	fmt.Fprintln(os.Stderr, string(encoded))
}

func writeAgentPayload(cmd *cobra.Command, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeAgentError(err)
		return silentExitError{code: 1}
	}
	_, err = cmd.OutOrStdout().Write(append(encoded, '\n'))
	return err
}

func suppressAgentCommandNoise(cmd *cobra.Command) *cobra.Command {
	if cmd == nil {
		return nil
	}
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}

func addBudgetFlag(cmd *cobra.Command, target *int) {
	cmd.Flags().IntVar(target, "budget-chars", 0, "override context packing budget")
}

func addContinuationFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "continuation-token", "", "resume a previous paginated response")
}

func maybeSetString(args map[string]any, key, value string) {
	if value != "" {
		args[key] = value
	}
}

func maybeSetStrings(args map[string]any, key string, values []string) {
	if len(values) > 0 {
		args[key] = values
	}
}

func maybeSetInt(args map[string]any, key string, value int) {
	if value != 0 {
		args[key] = value
	}
}

func maybeSetBool(cmd *cobra.Command, args map[string]any, key string, value bool) {
	if cmd.Flags().Changed(flagNameForKey(key)) {
		args[key] = value
	}
}

func flagNameForKey(key string) string {
	switch key {
	case "skipAnchors":
		return "skip-anchors"
	case "skipEmbeds":
		return "skip-embeds"
	case "includeFrontmatter":
		return "include-frontmatter"
	case "includeBacklinks":
		return "include-backlinks"
	case "includeContent":
		return "include-content"
	case "absolutePaths":
		return "absolute-paths"
	case "maxDepth":
		return "max-depth"
	case "noSuppress":
		return "no-suppress"
	case "excludeTags":
		return "exclude-tags"
	case "valueLimit":
		return "value-limit"
	case "maxValues":
		return "max-values"
	case "maxCommunities":
		return "max-communities"
	case "maxTopNotes":
		return "max-top-notes"
	case "submoduleDepth":
		return "submodule-depth"
	case "contextFiles":
		return "context-file"
	case "includeTags":
		return "include-tags"
	case "recencyCascade":
		return "recency-cascade"
	case "graphSummary":
		return "graph-summary"
	case "staleDays":
		return "stale-days"
	case "includeImages":
		return "include-images"
	case "includeTests":
		return "include-tests"
	case "requireExactSymbol":
		return "require-exact-symbol"
	case "includeCallers":
		return "include-callers"
	case "includeCallees":
		return "include-callees"
	case "includeDefinitions":
		return "include-definitions"
	case "contextLines":
		return "context-lines"
	case "valueCounts":
		return "value-counts"
	default:
		return key
	}
}
