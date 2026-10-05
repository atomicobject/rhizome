package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/spf13/cobra"
)

// WHY: query-recipe was promoted out of `rzm ontology` because saved recipes
// broadened to runtime-enriched roots (`code`, `agent`, runtime authoring
// guidance) that no longer fit a pure ontology namespace. The command now
// lives at the root for human callers (`rzm query-recipe`) and at
// `rzm agent query-recipe` for agent callers; both wrap the same factory
// below. Recipes themselves still validate against the ontology query schema,
// but their envelope and surface area cover non-ontology data too.
// Spec link: [[saved-query-recipes#^spec-0052-us5-ac6]].

type queryRecipeOptions struct {
	path       string
	id         string
	anchors    []string
	inputs     []string
	inputsJSON string
	inputsFile string
	jsonOutput bool
	summary    bool
}

func newQueryRecipeService() actions.QueryRecipeService {
	return actions.QueryRecipeService{
		Load: loadQueryRecipesForCommand,
		Schema: func() (*ontologyquery.ExecutableSchema, error) {
			_, schema, err := loadQueryRecipeSchema()
			return schema, err
		},
		Execute: func(ctx context.Context, query string, variables map[string]any) (ontologyquery.Result, error) {
			runtime, prepared, cleanup, err := prepareOntologyQuery(ctx, query, variables)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				return ontologyquery.Result{}, err
			}
			return ontologyquery.ExecutePrepared(ctx, runtime.deps(), runtime.schema, runtime.execSchema, prepared), nil
		},
	}
}

func newQueryRecipeCmd(agent bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query-recipe",
		Short: "List, validate, show, and run saved query recipes",
	}
	cmd.AddCommand(newQueryRecipeListCmd(agent, &queryRecipeOptions{}))
	cmd.AddCommand(newQueryRecipeValidateCmd(agent, &queryRecipeOptions{}))
	cmd.AddCommand(newQueryRecipeRunCmd(agent, &queryRecipeOptions{}))
	if !agent {
		cmd.AddCommand(newQueryRecipeShowCmd(&queryRecipeOptions{}))
	}
	return cmd
}

func newQueryRecipeListCmd(agent bool, opts *queryRecipeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved query recipes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent {
				if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
					return writeQueryRecipeCommandError(true, err)
				}
			}
			if useRecipeJSON(agent, opts.jsonOutput) {
				response, err := newQueryRecipeService().List(opts.path)
				if err != nil {
					return writeQueryRecipeCommandError(agent, err)
				}
				return writeJSON(cmd.OutOrStdout(), response)
			}
			recipes, issues, err := newQueryRecipeService().Catalog(opts.path)
			if err != nil {
				return writeQueryRecipeCommandError(agent, err)
			}
			return renderRecipeListHuman(cmd.OutOrStdout(), recipes, issues)
		},
	}
	addQueryRecipeCommonFlags(cmd, opts, false)
	addQueryRecipeJSONFlag(cmd, agent, opts)
	return cmd
}

func newQueryRecipeValidateCmd(agent bool, opts *queryRecipeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate saved query recipes against the live ontology query schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent {
				if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
					return writeQueryRecipeCommandError(true, err)
				}
			}
			response, err := newQueryRecipeService().Validate(opts.path)
			if err != nil {
				return writeQueryRecipeCommandError(agent, err)
			}
			if useRecipeJSON(agent, opts.jsonOutput) {
				if err := writeJSON(cmd.OutOrStdout(), response); err != nil {
					return err
				}
			} else {
				if err := renderRecipeValidateHuman(cmd.OutOrStdout(), response.Recipes, response.Issues); err != nil {
					return err
				}
			}
			if !response.OK {
				return silentExitError{code: 1}
			}
			return nil
		},
	}
	addQueryRecipeCommonFlags(cmd, opts, false)
	addQueryRecipeJSONFlag(cmd, agent, opts)
	return cmd
}

func newQueryRecipeShowCmd(opts *queryRecipeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a saved query recipe with a synthesized invocation example",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selection, err := newQueryRecipeService().Find(opts.path, args[0])
			if err != nil {
				return err
			}
			if len(selection.Issues) > 0 {
				_ = renderRecipeValidateHuman(cmd.OutOrStdout(), selection.Recipes, selection.Issues)
				return silentExitError{code: 1}
			}
			recipe := selection.Recipe
			if opts.jsonOutput {
				return writeJSON(cmd.OutOrStdout(), recipe)
			}
			return renderRecipeShowHuman(cmd.OutOrStdout(), recipe)
		},
	}
	addQueryRecipeCommonFlags(cmd, opts, false)
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "emit raw recipe JSON instead of the human-friendly view")
	return cmd
}

func newQueryRecipeRunCmd(agent bool, opts *queryRecipeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a saved query recipe",
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs, err := parseRecipeInputs(opts.inputs, opts.inputsJSON, opts.inputsFile)
			if err != nil {
				return writeQueryRecipeCommandError(agent, err)
			}
			anchors := []string(nil)
			if cmd.Flags().Changed("anchor") {
				anchors = opts.anchors
			}
			response, validation, err := newQueryRecipeService().Run(cmd.Context(), opts.path, opts.id, inputs, anchors)
			if err != nil {
				return writeQueryRecipeCommandError(agent, err)
			}
			if validation != nil {
				_ = writeJSON(cmd.OutOrStdout(), validation)
				return silentExitError{code: 1}
			}
			if err := writeJSON(cmd.OutOrStdout(), response); err != nil {
				return err
			}
			// WHY: --summary writes to stderr only so callers piping JSON to
			// `jq` or other consumers do not see the summary mixed into stdout.
			if !agent && opts.summary {
				renderRecipeRunSummary(cmd.ErrOrStderr(), response)
			}
			if len(response.Result.Errors) > 0 {
				return silentExitError{code: 1}
			}
			return nil
		},
	}
	addQueryRecipeCommonFlags(cmd, opts, true)
	cmd.Flags().StringVar(&opts.id, "id", "", "recipe id to run")
	if !agent {
		cmd.Flags().BoolVar(&opts.summary, "summary", false, "after the JSON envelope, write a human-readable summary footer to stderr")
	}
	return cmd
}

func addQueryRecipeCommonFlags(cmd *cobra.Command, opts *queryRecipeOptions, includeInputs bool) {
	cmd.Flags().StringVar(&opts.path, "path", "", "recipe markdown/yaml file or directory (defaults to shared recipe registries)")
	if includeInputs {
		cmd.Flags().StringArrayVar(&opts.anchors, "anchor", nil, "anchor input value (repeatable)")
		cmd.Flags().StringArrayVar(&opts.inputs, "input", nil, "named input as key=value (repeatable)")
		cmd.Flags().StringVar(&opts.inputsJSON, "inputs-json", "", "JSON object of named recipe inputs")
		cmd.Flags().StringVar(&opts.inputsFile, "inputs-file", "", "path to JSON object of named recipe inputs")
	}
}

func addQueryRecipeJSONFlag(cmd *cobra.Command, agent bool, opts *queryRecipeOptions) {
	if agent {
		// Agent callers always emit JSON; flag would be redundant noise.
		return
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "emit JSON instead of the human-friendly rendering")
}

// useRecipeJSON returns true when the renderer should emit JSON. Agent mode
// is always JSON; root mode is JSON only when --json is set.
func useRecipeJSON(agent, jsonFlag bool) bool {
	return agent || jsonFlag
}

func loadQueryRecipesForCommand(path string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) {
	if strings.TrimSpace(path) == "" {
		vaultDef, err := vaultDefOrDefault()
		if err != nil {
			return nil, nil, err
		}
		recipes, issues := queryrecipe.LoadDefaultSources(vaultDef.BasePath())
		return recipes, issues, nil
	}
	recipes, issues := queryrecipe.LoadPath(path)
	return recipes, issues, nil
}

func loadQueryRecipeSchema() (any, *ontologyquery.ExecutableSchema, error) {
	vaultDef, err := vaultDefOrDefault()
	if err != nil {
		return nil, nil, err
	}
	return loadOntologyExecutableSchema(vaultDef.BasePath())
}

func parseRecipeInputs(values []string, inputsJSON string, inputsFile string) (map[string]string, error) {
	if strings.TrimSpace(inputsJSON) != "" && strings.TrimSpace(inputsFile) != "" {
		return nil, fmt.Errorf("use only one of --inputs-json or --inputs-file")
	}
	out := map[string]string{}
	if strings.TrimSpace(inputsFile) != "" {
		data, err := os.ReadFile(inputsFile)
		if err != nil {
			return nil, err
		}
		parsed, err := actions.ParseRecipeInputsJSON(string(data))
		if err != nil {
			return nil, fmt.Errorf("--inputs-file: %w", err)
		}
		for key, val := range parsed {
			out[key] = val
		}
	}
	if strings.TrimSpace(inputsJSON) != "" {
		parsed, err := actions.ParseRecipeInputsJSON(inputsJSON)
		if err != nil {
			return nil, fmt.Errorf("--inputs-json: %w", err)
		}
		for key, val := range parsed {
			out[key] = val
		}
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) == "[]" {
			continue
		}
		key, val, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("--input must use key=value")
		}
		out[key] = val
	}
	return out, nil
}

func writeQueryRecipeCommandError(agent bool, err error) error {
	if agent {
		writeAgentError(err)
		return silentExitError{code: 1}
	}
	return err
}

func init() {
	cmd := newQueryRecipeCmd(false)
	// WHY: vault selection is needed by every subcommand (list/validate/run/show);
	// register it as a persistent flag so the root variant matches the
	// ergonomics callers had under `rzm ontology query-recipe`. The agent
	// variant inherits `--vault` from `agentCmd.PersistentFlags()`.
	cmd.PersistentFlags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")
	rootCmd.AddCommand(cmd)
}
