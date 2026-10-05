package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/spf13/cobra"
)

type validationProductRequest = actions.ValidationProductRequest
type validationProductRunner = actions.ValidationProductRunner

type validationProductOptions struct {
	vaultName       string
	maxIssues       int
	skipAnchors     bool
	skipEmbeds      bool
	includeImages   bool
	scopeNote       string
	scopeTarget     string
	scopeRef        string
	apply           bool
	allowHistorical bool
	selectActions   []string
	fromPlan        string
}

// newValidateProductCmdWithRunner builds the human validation surface around
// an injected production runner without owning command registration.
func newValidateProductCmdWithRunner(runner validationProductRunner) *cobra.Command {
	runOptions := validationProductOptions{}
	cmd := &cobra.Command{
		Use:           "validate [selector]",
		Short:         "Run focused or configured vault validation",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			request := runOptions.request(args, validate.SurfaceLocal, false, false)
			return executeValidationProduct(cmd, runner, request, validationProductRenderHuman)
		},
	}
	addValidationProductRunFlags(cmd, &runOptions, true)
	cmd.AddCommand(newValidationProductListCmd(false))
	cmd.AddCommand(newValidationProductFixCmd(runner, false))
	return cmd
}

// newAgentValidateProductCmdWithRunner builds the JSON-only agent mirror. It
// never carries a confirmation callback; apply is always non-interactive.
func newAgentValidateProductCmdWithRunner(runner validationProductRunner) *cobra.Command {
	runOptions := validationProductOptions{}
	cmd := &cobra.Command{
		Use:           "validate [selector]",
		Short:         "Run focused or configured validation as JSON",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runOptions.vaultName = vaultName
			request := runOptions.request(args, validate.SurfaceAgent, false, true)
			return executeValidationProduct(cmd, runner, request, validationProductRenderJSON)
		},
	}
	addValidationProductRunFlags(cmd, &runOptions, false)
	cmd.AddCommand(newValidationProductListCmd(true))
	cmd.AddCommand(newValidationProductFixCmd(runner, true))
	return cmd
}

// newCIProductCmdWithRunner builds the read-only CI shell without registering
// it. A single enum flag keeps GitHub workflow commands out of JSON output.
func newCIProductCmdWithRunner(runner validationProductRunner) *cobra.Command {
	formatValue := ciFormatSelection{value: string(actions.CIFormatGitHub)}
	var vaultName string
	cmd := &cobra.Command{
		Use:           "ci [selector]",
		Short:         "Run Rhizome content validation for CI",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if formatValue.count > 1 {
				err := fmt.Errorf("CI format expected exactly one renderer; got %d --format values", formatValue.count)
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return silentExitError{code: actions.ValidationExitFailure}
			}
			format, err := actions.ParseCIFormat(formatValue.value)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return silentExitError{code: actions.ValidationExitFailure}
			}
			request := validationProductRequest{
				Selectors: append([]string(nil), args...),
				Surface:   validate.SurfaceCI,
				VaultName: vaultName,
			}
			request.ApplyCommand = buildValidationProductApplyCommand(args, validate.SurfaceLocal, validationProductOptions{vaultName: vaultName})
			if runner == nil {
				return renderCIProductError(cmd, format, fmt.Errorf("validation product runner is not configured"))
			}
			result, err := runner.Run(cmd.Context(), request)
			if err != nil {
				return renderCIProductError(cmd, format, err)
			}
			if err := actions.RenderCI(cmd.OutOrStdout(), format, result); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return silentExitError{code: actions.ValidationExitFailure}
			}
			return validationProductExit(result.ExitCode())
		},
	}
	cmd.Flags().Var(&formatValue, "format", "output format: github or json")
	cmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")
	return cmd
}

type ciFormatSelection struct {
	value string
	count int
}

func (selection *ciFormatSelection) Set(value string) error {
	selection.value = value
	selection.count++
	return nil
}

func (selection *ciFormatSelection) String() string {
	return selection.value
}

func (*ciFormatSelection) Type() string {
	return "ci-format"
}

func newValidationProductListCmd(agent bool) *cobra.Command {
	return &cobra.Command{
		Use:           "list",
		Short:         "List validation checks, suites, and prerequisites",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			catalog := actions.BuildValidationCatalog()
			if !agent {
				_, err := io.WriteString(cmd.OutOrStdout(), actions.RenderValidationCatalogHuman(catalog))
				return err
			}
			payload, err := actions.RenderValidationCatalogJSON(catalog)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(payload)
			return err
		},
	}
}

func newValidationProductFixCmd(runner validationProductRunner, agent bool) *cobra.Command {
	options := validationProductOptions{}
	surface := validate.SurfaceLocal
	renderer := validationProductRenderHuman
	if agent {
		surface = validate.SurfaceAgent
		renderer = validationProductRenderJSON
	}
	cmd := &cobra.Command{
		Use:           "fix [selector]",
		Short:         "Plan or apply deterministic validation repairs",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent {
				options.vaultName = vaultName
			}
			selection, err := actions.ResolveValidationApplySelection(options.apply, options.selectActions, options.fromPlan)
			if err != nil {
				return renderValidationProductError(cmd, renderer, err)
			}
			request := options.request(args, surface, true, agent)
			request.ApplySelection = selection
			if request.Apply && !request.NonInteractive {
				request.Confirm = newValidationProductConfirm(cmd)
			}
			return executeValidationProduct(cmd, runner, request, renderer)
		},
	}
	addValidationProductRunFlags(cmd, &options, !agent)
	cmd.Flags().BoolVar(&options.apply, "apply", false, "apply safe deterministic repairs after planning")
	cmd.Flags().BoolVar(&options.allowHistorical, "allow-historical", false, "allow lifecycle-protected historical edits without bypassing other safety rules")
	cmd.Flags().StringArrayVar(&options.selectActions, "action", nil, "with --apply, apply exactly this reviewed action ID or issue key (repeatable; includes needs_confirmation actions)")
	cmd.Flags().StringVar(&options.fromPlan, "from-plan", "", "with --apply, apply exactly the action IDs or issue keys listed in this file (JSON array or one per line)")
	return cmd
}

func newValidationProductConfirm(cmd *cobra.Command) func(string) (bool, error) {
	reader := bufio.NewReader(cmd.InOrStdin())
	return func(question string) (bool, error) {
		if strings.TrimSpace(question) == "" {
			return false, nil
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N]: ", question); err != nil {
			return false, err
		}
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes", nil
	}
}

func addValidationProductRunFlags(cmd *cobra.Command, options *validationProductOptions, includeVault bool) {
	if includeVault {
		cmd.Flags().StringVarP(&options.vaultName, "vault", "v", "", "vault name (uses default if unset)")
	}
	cmd.Flags().IntVar(&options.maxIssues, "max-issues", 20, "max issues to include per check")
	cmd.Flags().BoolVar(&options.skipAnchors, "skip-anchors", false, "skip wikilinks with anchors for broken-links check")
	cmd.Flags().BoolVar(&options.skipEmbeds, "skip-embeds", false, "skip embedded wikilinks for broken-links check")
	cmd.Flags().BoolVar(&options.includeImages, "include-images", false, "include image links for broken-links check")
	cmd.Flags().StringVar(&options.scopeNote, "scope-note", "", "limit fragile-external check to one source note")
	cmd.Flags().StringVar(&options.scopeTarget, "scope-target", "", "limit fragile-external check to one target note")
	cmd.Flags().StringVar(&options.scopeRef, "scope-ref", "", "limit fragile-external check to one candidate node ref")
}

func (options validationProductOptions) request(args []string, surface validate.ExecutionSurface, repair, nonInteractive bool) validationProductRequest {
	return validationProductRequest{
		Selectors:       append([]string(nil), args...),
		Surface:         surface,
		VaultName:       options.vaultName,
		ApplyCommand:    buildValidationProductApplyCommand(args, surface, options),
		MaxIssues:       options.maxIssues,
		SkipAnchors:     options.skipAnchors,
		SkipEmbeds:      options.skipEmbeds,
		IncludeImages:   options.includeImages,
		ScopeNote:       options.scopeNote,
		ScopeTarget:     options.scopeTarget,
		ScopeRef:        options.scopeRef,
		Repair:          repair,
		Apply:           repair && options.apply,
		AllowHistorical: repair && options.allowHistorical,
		NonInteractive:  nonInteractive,
	}
}

func buildValidationProductApplyCommand(args []string, surface validate.ExecutionSurface, options validationProductOptions) string {
	return actions.BuildValidationProductApplyCommand(validationProductRequest{
		Selectors: append([]string(nil), args...), Surface: surface, VaultName: options.vaultName,
		SkipAnchors: options.skipAnchors, SkipEmbeds: options.skipEmbeds, IncludeImages: options.includeImages,
		ScopeNote: options.scopeNote, ScopeTarget: options.scopeTarget, ScopeRef: options.scopeRef,
		AllowHistorical: options.allowHistorical,
	})
}

type validationProductRenderer int

const (
	validationProductRenderHuman validationProductRenderer = iota
	validationProductRenderJSON
)

func executeValidationProduct(cmd *cobra.Command, runner validationProductRunner, request validationProductRequest, renderer validationProductRenderer) error {
	result, err := runValidationProduct(cmd.Context(), runner, request)
	if err != nil {
		return renderValidationProductError(cmd, renderer, err)
	}
	if renderer == validationProductRenderJSON {
		if err := actions.RenderCI(cmd.OutOrStdout(), actions.CIFormatJSON, result); err != nil {
			return err
		}
	} else {
		if err := actions.RenderValidationResultHuman(cmd.OutOrStdout(), result); err != nil {
			return err
		}
	}
	return validationProductExit(result.ExitCode())
}

func runValidationProduct(ctx context.Context, runner validationProductRunner, request validationProductRequest) (actions.ValidationResult, error) {
	return actions.RunValidationProduct(ctx, runner, request)
}

func renderValidationProductError(cmd *cobra.Command, renderer validationProductRenderer, err error) error {
	if renderer == validationProductRenderJSON {
		_ = actions.RenderCIError(cmd.ErrOrStderr(), actions.CIFormatJSON, err)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "Validation failed: %v\n", err)
	}
	return silentExitError{code: actions.ValidationExitFailure}
}

func renderCIProductError(cmd *cobra.Command, format actions.CIFormat, err error) error {
	_ = actions.RenderCIError(cmd.OutOrStdout(), format, err)
	return silentExitError{code: actions.ValidationExitFailure}
}

func validationProductExit(code int) error {
	if code == actions.ValidationExitClean {
		return nil
	}
	return silentExitError{code: code}
}
