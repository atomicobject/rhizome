package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	moduleactions "github.com/atomicobject/rhizome/pkg/app/actions"
	"github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/actiondef"
	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
	"github.com/spf13/cobra"
)

// Module commands ([[rhizome-modules#^SPEC-0130-US4|SPEC-0130 US4]]): each
// query recipe in `.rhizome/modules/<name>/recipes/` is a command named by its
// recipe id, with one flag per declared input, and each action in its
// `actions/` folder ([[module-actions|SPEC-0131]]) is a command named by the
// action, with one flag per input. `rzm module <name>` always works;
// `rzm <name>` is rewritten to it only when <name> is not a built-in.

// newActionRunner opens a vault's actions runner; tests replace it to
// control repository trust.
var newActionRunner = moduleactions.New

// moduleReservedFlags are the module command's own flags; an input with one of
// these names is passed with --input name=value instead.
var moduleReservedFlags = map[string]bool{"help": true, "vault": true, "summary": true, "input": true, "no-pager": true}

// moduleSharedFlags are the flags an action command has besides its inputs.
var moduleSharedFlags = map[string]bool{"help": true, "vault": true, "no-pager": true}

func registerModuleFamily(rootCmd *cobra.Command) {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "module <name> [command] [flags]",
		Short: "Run a module's query recipes and actions as commands (also `rzm <name>`)",
		Long: "Lists or runs the commands of the module in .rhizome/modules/<name>/. Each query recipe in the\n" +
			"module's recipes/ folder is a command named by its recipe id, and each action in its actions/ folder\n" +
			"is a command named by the action, with their declared inputs as flags. An action exits 0 when it\n" +
			"succeeds, 2 when it refuses, and 1 on an error.\n" +
			"`rzm <name>` is a shortcut whenever <name> is not a built-in command.",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			at := commandArgIndex(cmd.Root(), args)
			if at >= len(args) || args[at] == "-h" || args[at] == "--help" {
				return cmd.Help()
			}
			rest := append(append([]string{}, args[:at]...), args[at+1:]...)
			return runModuleCommand(cmd, args[at], rest)
		},
	})
}

// moduleDispatchArgs rewrites `rzm <name> ...` to `rzm module <name> ...` when
// <name> is not a built-in command and names a module folder. Cobra-resolvable
// arguments return before any file system access.
func (app *commandInvocation) moduleDispatchArgs(ctx context.Context, args []string) ([]string, bool) {
	at := commandArgIndex(app.Command, args)
	if at >= len(args) || !assetroots.ValidModuleName(args[at]) || isBuiltinCommand(app.Command, args[at]) {
		return nil, false
	}
	opts := &vaultCommandOptions{vaultName: moduleVaultFlag(args[at+1:])}
	def, err := opts.vaultDefOrDefaultContext(contextWithCommandEnv(ctx, app.env))
	if err != nil || !operations.ModuleExists(def.BasePath(), args[at]) {
		return nil, false
	}
	routed := append(append(append([]string{}, args[:at]...), "module"), args[at:]...)
	return routed, true
}

// commandArgIndex returns the index of the first argument after the root
// command's own flags, such as `rzm --no-pager <name>`.
func commandArgIndex(root *cobra.Command, args []string) int {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") && args[i] != "-" && args[i] != "--" {
		name, _, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		flag := root.PersistentFlags().Lookup(name)
		if flag == nil && len(name) == 1 {
			flag = root.PersistentFlags().ShorthandLookup(name)
		}
		if flag == nil {
			return len(args)
		}
		i++
		if !hasValue && flag.NoOptDefVal == "" {
			i++
		}
	}
	return i
}

// isBuiltinCommand reports whether name resolves to a top-level command,
// including the help and completion commands Cobra adds at execution.
func isBuiltinCommand(root *cobra.Command, name string) bool {
	if name == "help" || name == "completion" {
		return true
	}
	for _, sub := range root.Commands() {
		if sub.Name() == name || sub.HasAlias(name) {
			return true
		}
	}
	return false
}

func runModuleCommand(parent *cobra.Command, name string, args []string) error {
	ctx := parent.Context()
	opts := &vaultCommandOptions{vaultName: moduleVaultFlag(args)}
	def, err := opts.vaultDefOrDefaultContext(ctx)
	if err != nil {
		return err
	}
	loaded, err := operations.LoadModuleRecipes(def.BasePath(), name)
	if err != nil {
		return err
	}

	root := &cobra.Command{Use: "rzm", SilenceErrors: true, SilenceUsage: parent.Root().SilenceUsage}
	root.PersistentFlags().StringVarP(&opts.vaultName, "vault", "v", opts.vaultName, "vault name (uses default if unset)")
	root.PersistentFlags().Bool("no-pager", false, "disable paging (when stdout is a TTY)")
	module := &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Commands from .rhizome/modules/%s", name),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	if len(loaded.Unnamed) > 0 {
		module.Long = fmt.Sprintf("%s\n\nRecipes whose ids contain spaces are not commands; run them with `rzm query-recipe run --id <id>`: %s", module.Short, strings.Join(loaded.Unnamed, ", "))
	}
	root.AddCommand(module)
	runner := newActionRunner(def.BasePath())
	actionList, err := runner.List()
	if err != nil {
		return err
	}
	recipeNames := map[string]bool{}
	for _, recipe := range loaded.Commands {
		recipeNames[recipe.ID] = true
	}
	var moduleActions []actiondef.Action
	for _, action := range actionList {
		// rzm validate actions reports an action named like a recipe.
		if action.Module == name && !recipeNames[action.Name] {
			moduleActions = append(moduleActions, action)
		}
	}
	// Help lists recipes and actions apart, once there are actions.
	recipeGroup := ""
	if len(moduleActions) > 0 {
		module.AddGroup(&cobra.Group{ID: "actions", Title: "Actions:"})
		if len(loaded.Commands) > 0 {
			recipeGroup = "recipes"
			module.AddGroup(&cobra.Group{ID: recipeGroup, Title: "Recipes:"})
		}
	}
	for _, recipe := range loaded.Commands {
		cmd := opts.newModuleRecipeCmd(recipe, loaded.All, loaded.Issues)
		cmd.GroupID = recipeGroup
		module.AddCommand(cmd)
	}
	for _, action := range moduleActions {
		module.AddCommand(newModuleActionCmd(runner, action))
	}
	root.SetArgs(append([]string{name}, args...))
	root.SetIn(parent.InOrStdin())
	root.SetOut(parent.OutOrStdout())
	root.SetErr(parent.ErrOrStderr())
	err = root.ExecuteContext(ctx)
	// An action already reported its refusal or error.
	if errors.As(err, new(silentExitError)) {
		parent.SilenceUsage, parent.SilenceErrors = true, true
	}
	return err
}

func (commandOptions *vaultCommandOptions) newModuleRecipeCmd(recipe queryrecipe.Recipe, recipes []queryrecipe.Recipe, issues []queryrecipe.Issue) *cobra.Command {
	opts := &queryRecipeOptions{id: recipe.ID}
	values := map[string]*string{}
	short := truncateOneLine(recipe.Problem, 80)
	if short == "" {
		short = recipe.Name
	}
	cmd := &cobra.Command{
		Use:   recipe.ID,
		Short: short,
		Long:  strings.TrimSpace(recipe.Name + "\n\n" + recipe.Problem),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inputs, err := parseRecipeInputs(opts.inputs, "", "")
			if err != nil {
				return err
			}
			for name, value := range values {
				if cmd.Flags().Changed(name) {
					inputs[name] = *value
				}
			}
			service := commandOptions.newQueryRecipeService(cmd.Context())
			service.Load = func(string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) { return recipes, issues, nil }
			return runQueryRecipeCommand(cmd, service, false, opts, inputs, nil)
		},
	}
	for _, input := range recipe.InputSpec.Inputs {
		if moduleReservedFlags[input.Name] || cmd.Flags().Lookup(input.Name) != nil {
			continue
		}
		usage := input.Description
		if input.Required {
			usage = strings.TrimSpace(usage + " (required)")
		}
		values[input.Name] = cmd.Flags().String(input.Name, input.Default, usage)
	}
	cmd.Flags().StringArrayVar(&opts.inputs, "input", nil, "named input as key=value (repeatable)")
	cmd.Flags().BoolVar(&opts.summary, "summary", false, "after the JSON envelope, write a human-readable summary footer to stderr")
	return cmd
}

// newModuleActionCmd runs one action. It prints the result as JSON and
// exits 0, prints the reason and exits 2 when the action refuses, and
// prints the reason and stderr tail and exits 1 on an error.
func newModuleActionCmd(runner *moduleactions.Runner, action actiondef.Action) *cobra.Command {
	cmd := &cobra.Command{
		Use:     action.Name,
		Short:   truncateOneLine(action.Description, 80),
		Long:    action.Description,
		Args:    cobra.NoArgs,
		GroupID: "actions",
		// Its outcomes are printed by RunE; Cobra adds nothing.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			given := map[string][]string{}
			for name, spec := range action.Inputs {
				if !cmd.Flags().Changed(name) {
					continue
				}
				switch spec.Kind {
				case actiondef.KindList:
					given[name], _ = cmd.Flags().GetStringArray(name)
				default:
					value, _ := cmd.Flags().GetString(name)
					given[name] = []string{value}
				}
			}
			input, err := moduleactions.CoerceFlags(action, given)
			if err != nil {
				return err
			}
			envelope, err := runner.Run(cmd.Context(), action.ID, input)
			if err != nil {
				return err
			}
			switch envelope.Status {
			case moduleactions.StatusOK:
				var pretty bytes.Buffer
				if json.Indent(&pretty, envelope.Result, "", "  ") != nil {
					pretty.Reset()
					pretty.Write(envelope.Result)
				}
				fmt.Fprintln(cmd.OutOrStdout(), pretty.String())
				return nil
			case moduleactions.StatusRefused:
				fmt.Fprintln(cmd.ErrOrStderr(), envelope.Reason)
				return silentExitError{code: 2}
			default:
				fmt.Fprintln(cmd.ErrOrStderr(), envelope.Reason)
				if tail := strings.TrimSpace(envelope.StderrTail); tail != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), tail)
				}
				return silentExitError{code: 1}
			}
		},
	}
	names := make([]string, 0, len(action.Inputs))
	for name := range action.Inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec := action.Inputs[name]
		// ponytail: an input named like a flag every module command has cannot be set here; add --input when one needs it.
		if moduleSharedFlags[name] {
			continue
		}
		usage := spec.Kind
		if len(spec.Enum) > 0 {
			usage += ", one of " + strings.Join(spec.Enum, ", ")
		}
		if spec.Kind == actiondef.KindList {
			usage += "; repeat the flag for each value"
		}
		if spec.Required {
			usage += " (required)"
		}
		// A bool takes an explicit true or false, so every flag but the shared
		// booleans takes a value, which moduleVaultFlag relies on.
		if spec.Kind == actiondef.KindList {
			cmd.Flags().StringArray(name, nil, usage)
		} else {
			cmd.Flags().String(name, "", usage)
		}
	}
	return cmd
}

// moduleVaultFlag finds --vault/-v before the module's flags are defined,
// since the vault decides which recipes, and so which flags, exist.
func moduleVaultFlag(args []string) string {
	// Cobra keeps the last value of a repeated flag, so this does too. Every
	// module-command flag except these booleans takes a value, so the value
	// after any other flag is skipped rather than read as a flag.
	booleans := map[string]bool{"--summary": true, "--no-pager": true, "--help": true, "-h": true}
	vault := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return vault
		case !strings.HasPrefix(arg, "-") || booleans[arg]:
		case arg == "--vault" || arg == "-v":
			if i+1 < len(args) {
				vault = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--vault="):
			vault = strings.TrimPrefix(arg, "--vault=")
		case strings.HasPrefix(arg, "-v") && !strings.HasPrefix(arg, "--"):
			vault = strings.TrimPrefix(strings.TrimPrefix(arg, "-v"), "=")
		case strings.HasPrefix(arg, "--") && !strings.Contains(arg, "="):
			i++
		}
	}
	return vault
}
