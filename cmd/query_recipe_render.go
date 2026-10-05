package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
)

// WHY: human-facing rendering is split into a dedicated file so the cmd
// orchestration in `query_recipe.go` stays focused on flag wiring + RunE.
// Agent callers always get JSON; root callers get the human renderers below
// unless they pass `--json`. See [[saved-query-recipes#^spec-0052-us2-ac7]].

// renderRecipeListHuman writes a table with id / mode / name / problem so
// callers can scan available recipes without parsing JSON.
func renderRecipeListHuman(out io.Writer, recipes []queryrecipe.Recipe, issues []queryrecipe.Issue) error {
	if len(recipes) == 0 {
		fmt.Fprintln(out, "No saved query recipes found.")
		fmt.Fprintln(out, "Drop recipe YAML/Markdown files in `.rhizome/query-recipes/` or pass `--path <file>`.")
		return writeRecipeIssuesIfAny(out, issues)
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tMODE\tNAME\tPROBLEM"); err != nil {
		return err
	}
	for _, recipe := range recipes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			recipe.ID,
			string(recipe.InputSpec.Mode),
			recipe.Name,
			truncateOneLine(recipe.Problem, 80),
		)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n%d recipe(s). Run `rzm query-recipe show <id>` for full details, or `rzm query-recipe validate` to compile against the live schema.\n", len(recipes))
	return writeRecipeIssuesIfAny(out, issues)
}

// renderRecipeValidateHuman groups issues per recipe with a single-line per
// issue render and a final OK/<count> footer.
func renderRecipeValidateHuman(out io.Writer, recipes []queryrecipe.Recipe, issues []queryrecipe.Issue) error {
	if len(issues) == 0 {
		fmt.Fprintf(out, "OK — %d recipe(s) validated against the live ontology query schema.\n", len(recipes))
		return nil
	}

	byRecipe := map[string][]queryrecipe.Issue{}
	loadIssues := []queryrecipe.Issue{}
	for _, issue := range issues {
		if strings.TrimSpace(issue.Recipe) == "" {
			loadIssues = append(loadIssues, issue)
			continue
		}
		byRecipe[issue.Recipe] = append(byRecipe[issue.Recipe], issue)
	}

	keys := make([]string, 0, len(byRecipe))
	for key := range byRecipe {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	if len(loadIssues) > 0 {
		fmt.Fprintln(out, "Load issues (recipe could not be parsed):")
		for _, issue := range loadIssues {
			writeIssueLine(out, issue)
		}
		fmt.Fprintln(out)
	}
	for _, key := range keys {
		fmt.Fprintf(out, "[%s]\n", key)
		for _, issue := range byRecipe[key] {
			writeIssueLine(out, issue)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "FAIL — %d issue(s) across %d recipe(s).\n", len(issues), len(recipes))
	return nil
}

// renderRecipeShowHuman prints recipe metadata, GraphQL preview, output
// contract, adaptation guidance, and a synthesized invocation line built from
// `inputSpec`. Used only on the root command (agent variant relies on `list`).
func renderRecipeShowHuman(out io.Writer, recipe queryrecipe.Recipe) error {
	title := recipe.Name
	if strings.TrimSpace(title) == "" {
		title = recipe.ID
	}
	fmt.Fprintf(out, "# %s (%s)\n\n", title, recipe.ID)

	if mode := strings.TrimSpace(string(recipe.InputSpec.Mode)); mode != "" {
		fmt.Fprintf(out, "Mode: %s\n", mode)
	}
	if primary := strings.TrimSpace(recipe.InputSpec.PrimaryInput); primary != "" {
		fmt.Fprintf(out, "Primary input: %s\n", primary)
	}
	if alt := strings.TrimSpace(recipe.InputSpec.BroadAlternativeID); alt != "" {
		fmt.Fprintf(out, "Broad alternative: %s\n", alt)
	}
	if len(recipe.Tags) > 0 {
		fmt.Fprintf(out, "Tags: %s\n", strings.Join(recipe.Tags, ", "))
	}
	if path := strings.TrimSpace(recipe.Source.Path); path != "" {
		if recipe.Source.Line > 0 {
			fmt.Fprintf(out, "Source: %s:%d\n", path, recipe.Source.Line)
		} else {
			fmt.Fprintf(out, "Source: %s\n", path)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "## Problem")
	fmt.Fprintln(out, indentBlock(recipe.Problem, "  "))

	if len(recipe.InputSpec.Inputs) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "## Inputs")
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  NAME\tREQ\tKIND\tDEFAULT\tDESCRIPTION")
		for _, input := range recipe.InputSpec.Inputs {
			req := "-"
			if input.Required {
				req = "yes"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
				input.Name,
				req,
				orDefault(input.Kind, "string"),
				orDefault(input.Default, "-"),
				truncateOneLine(input.Description, 60),
			)
		}
		_ = w.Flush()
	}

	if g := strings.TrimSpace(recipe.Query.GraphQL); g != "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "## GraphQL")
		fmt.Fprintln(out, "```graphql")
		fmt.Fprint(out, ensureTrailingNewline(g))
		fmt.Fprintln(out, "```")
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "## Output contract")
	if len(recipe.OutputContract.ExpectedPaths) > 0 {
		fmt.Fprintln(out, "Expected paths:")
		for _, path := range recipe.OutputContract.ExpectedPaths {
			fmt.Fprintf(out, "  - %s\n", path)
		}
	}
	if v := strings.TrimSpace(recipe.OutputContract.Empty); v != "" {
		fmt.Fprintf(out, "Empty: %s\n", v)
	}
	if v := strings.TrimSpace(recipe.OutputContract.Partial); v != "" {
		fmt.Fprintf(out, "Partial: %s\n", v)
	}
	if v := strings.TrimSpace(recipe.OutputContract.HighVolume); v != "" {
		fmt.Fprintf(out, "High volume: %s\n", v)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "## Adaptation guidance")
	if v := strings.TrimSpace(recipe.AdaptationGuidance.Summary); v != "" {
		fmt.Fprintln(out, indentBlock(v, "  "))
	}
	for _, rule := range recipe.AdaptationGuidance.Rules {
		fmt.Fprintf(out, "  - %s\n", rule)
	}

	if len(recipe.Examples) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "## Examples")
		for _, example := range recipe.Examples {
			label := strings.TrimSpace(example.Name)
			if label == "" {
				label = "(unnamed)"
			}
			fmt.Fprintf(out, "  - %s\n", label)
			keys := make([]string, 0, len(example.Inputs))
			for key := range example.Inputs {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				fmt.Fprintf(out, "      %s = %s\n", key, example.Inputs[key])
			}
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "## Run")
	fmt.Fprintf(out, "  %s\n", synthesizeRecipeInvocation(recipe))
	return nil
}

// renderRecipeRunSummary writes a stderr footer with recipe metadata and
// result counts so humans get context without contaminating stdout.
func renderRecipeRunSummary(stderr io.Writer, run queryrecipe.RunResult) {
	if stderr == nil {
		return
	}
	fmt.Fprintln(stderr, "---")
	fmt.Fprintf(stderr, "Recipe: %s (%s)\n", run.Recipe.Name, run.Recipe.ID)
	fmt.Fprintf(stderr, "Mode: %s\n", string(run.Recipe.InputSpec.Mode))
	if len(run.Inputs) > 0 {
		keys := make([]string, 0, len(run.Inputs))
		for key := range run.Inputs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fmt.Fprintln(stderr, "Inputs:")
		for _, key := range keys {
			fmt.Fprintf(stderr, "  %s = %s\n", key, run.Inputs[key])
		}
	}
	if len(run.Result.Data) > 0 {
		topKeys := make([]string, 0, len(run.Result.Data))
		for key := range run.Result.Data {
			topKeys = append(topKeys, key)
		}
		sort.Strings(topKeys)
		fmt.Fprintln(stderr, "Result top-level keys:")
		for _, key := range topKeys {
			fmt.Fprintf(stderr, "  %s -> %s\n", key, summarizeValue(run.Result.Data[key]))
		}
	} else {
		fmt.Fprintln(stderr, "Result: empty")
	}
	if len(run.Result.Errors) > 0 {
		fmt.Fprintf(stderr, "Errors: %d\n", len(run.Result.Errors))
	}
}

func writeIssueLine(out io.Writer, issue queryrecipe.Issue) {
	location := ""
	if strings.TrimSpace(issue.Path) != "" {
		if issue.Line > 0 {
			location = fmt.Sprintf(" (%s:%d)", issue.Path, issue.Line)
		} else {
			location = fmt.Sprintf(" (%s)", issue.Path)
		}
	}
	field := ""
	if strings.TrimSpace(issue.Field) != "" {
		field = " — field " + issue.Field
	}
	fmt.Fprintf(out, "  - %s: %s%s%s\n", issue.Code, issue.Message, location, field)
}

func writeRecipeIssuesIfAny(out io.Writer, issues []queryrecipe.Issue) error {
	if len(issues) == 0 {
		return nil
	}
	fmt.Fprintln(out, "\nLoad issues:")
	for _, issue := range issues {
		writeIssueLine(out, issue)
	}
	return nil
}

func synthesizeRecipeInvocation(recipe queryrecipe.Recipe) string {
	parts := []string{"rzm query-recipe run", "--id", recipe.ID}
	switch recipe.InputSpec.Mode {
	case queryrecipe.InputModeNone:
		// no inputs
	case queryrecipe.InputModeOptionalAnchor, queryrecipe.InputModeRequiredAnchor:
		parts = append(parts, "--anchor", anchorPlaceholder(recipe))
	case queryrecipe.InputModeMultiAnchor:
		parts = append(parts, "--anchor", anchorPlaceholder(recipe), "--anchor", "<additional-anchor>")
	}
	for _, input := range recipe.InputSpec.Inputs {
		if input.Name == strings.TrimSpace(recipe.InputSpec.PrimaryInput) {
			continue
		}
		parts = append(parts, "--input", fmt.Sprintf("%s=%s", input.Name, inputPlaceholder(input)))
	}
	return strings.Join(parts, " ")
}

func anchorPlaceholder(recipe queryrecipe.Recipe) string {
	for _, example := range recipe.Examples {
		for _, input := range recipe.InputSpec.Inputs {
			if input.Name != strings.TrimSpace(recipe.InputSpec.PrimaryInput) {
				continue
			}
			if value := strings.TrimSpace(example.Inputs[input.Name]); value != "" {
				return value
			}
		}
	}
	for _, input := range recipe.InputSpec.Inputs {
		if input.Name == strings.TrimSpace(recipe.InputSpec.PrimaryInput) && strings.TrimSpace(input.Default) != "" {
			return input.Default
		}
	}
	primary := strings.TrimSpace(recipe.InputSpec.PrimaryInput)
	if primary == "" {
		primary = "anchor"
	}
	return "<" + primary + ">"
}

func inputPlaceholder(input queryrecipe.Input) string {
	if v := strings.TrimSpace(input.Default); v != "" {
		return v
	}
	return "<" + input.Name + ">"
}

func truncateOneLine(value string, max int) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	for strings.Contains(value, "  ") {
		value = strings.ReplaceAll(value, "  ", " ")
	}
	if len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "…"
}

func indentBlock(value, prefix string) string {
	value = strings.TrimRight(value, "\n")
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func summarizeValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case []any:
		return fmt.Sprintf("array(%d)", len(typed))
	case map[string]any:
		return fmt.Sprintf("object(%d keys)", len(typed))
	case string:
		return fmt.Sprintf("string(%d chars)", len(typed))
	case json.Number:
		return "number"
	case float64:
		return "number"
	case bool:
		return "bool"
	default:
		return fmt.Sprintf("%T", value)
	}
}
