package queryrecipe

import (
	"fmt"
	"sort"
	"strings"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func Validate(recipes []Recipe, execSchema *ontologyquery.ExecutableSchema) ValidationResult {
	var issues []Issue
	seen := map[string]Source{}
	for _, recipe := range recipes {
		issues = append(issues, validateMetadata(recipe)...)
		id := strings.TrimSpace(recipe.ID)
		if id != "" {
			if prev, ok := seen[id]; ok {
				issues = append(issues, recipeIssue(recipe, "duplicate_recipe_id", fmt.Sprintf("recipe id %q already declared at %s:%d", id, prev.Path, prev.Line), "id"))
			} else {
				seen[id] = recipe.Source
			}
		}
		if execSchema != nil && strings.TrimSpace(recipe.Query.GraphQL) != "" {
			if _, recipeIssues := Compile(recipe, execSchema, validationInputs(recipe, execSchema)); len(recipeIssues) > 0 {
				issues = append(issues, recipeIssues...)
			}
		}
	}
	return ValidationResult{Recipes: recipes, Issues: issues}
}

func validationInputs(recipe Recipe, execSchema *ontologyquery.ExecutableSchema) map[string]string {
	inputs := recipe.InputSpec.Inputs
	out := make(map[string]string, len(inputs))
	enumSamples := enumVariableSamples(recipe, execSchema)
	for _, input := range inputs {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			continue
		}
		if sample, ok := enumSamples[name]; ok && input.Default == "" {
			out[name] = sample
			continue
		}
		out[name] = sampleValue(input)
	}
	return out
}

func enumVariableSamples(recipe Recipe, execSchema *ontologyquery.ExecutableSchema) map[string]string {
	out := map[string]string{}
	if execSchema == nil || execSchema.Schema == nil || strings.TrimSpace(recipe.Query.GraphQL) == "" {
		return out
	}
	doc, errs := gqlparser.LoadQuery(execSchema.Schema, recipe.Query.GraphQL)
	if len(errs) > 0 || len(doc.Operations) != 1 {
		return out
	}
	for _, variable := range doc.Operations[0].VariableDefinitions {
		if variable == nil || variable.Type == nil {
			continue
		}
		definition := execSchema.Schema.Types[variable.Type.Name()]
		if definition == nil || definition.Kind != ast.Enum {
			continue
		}
		for _, value := range definition.EnumValues {
			if value == nil || strings.TrimSpace(value.Name) == "" {
				continue
			}
			out[variable.Variable] = value.Name
			break
		}
	}
	return out
}

func Compile(recipe Recipe, execSchema *ontologyquery.ExecutableSchema, inputs map[string]string) (*CompiledRecipe, []Issue) {
	variables, _, issues := BindVariables(recipe, inputs)
	if len(issues) > 0 {
		return nil, issues
	}
	prepared, errs := ontologyquery.PrepareWithVariables(execSchema, recipe.Query.GraphQL, variables)
	if len(errs) > 0 {
		out := make([]Issue, 0, len(errs))
		for _, err := range errs {
			out = append(out, recipeIssue(recipe, "query_compile_error", err.Message, "query.graphQL"))
		}
		return nil, out
	}
	if pathIssues := validateOutputContractPaths(recipe, prepared); len(pathIssues) > 0 {
		return nil, pathIssues
	}
	return &CompiledRecipe{
		Recipe:       recipe,
		Query:        recipe.Query.GraphQL,
		Variables:    variables,
		Dependencies: DependenciesFor(prepared),
		Prepared:     prepared,
	}, nil
}

func validateMetadata(recipe Recipe) []Issue {
	var issues []Issue
	require := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			issues = append(issues, recipeIssue(recipe, "missing_required_field", field+" is required", field))
		}
	}
	if strings.TrimSpace(recipe.APIVersion) != APIVersion {
		issues = append(issues, recipeIssue(recipe, "unsupported_api_version", "apiVersion must be "+APIVersion, "apiVersion"))
	}
	require("id", recipe.ID)
	require("name", recipe.Name)
	require("problem", recipe.Problem)
	require("query.graphQL", recipe.Query.GraphQL)
	require("outputContract.empty", recipe.OutputContract.Empty)
	require("adaptationGuidance.summary", recipe.AdaptationGuidance.Summary)
	switch recipe.InputSpec.Mode {
	case InputModeNone, InputModeOptionalAnchor, InputModeRequiredAnchor, InputModeMultiAnchor:
	default:
		issues = append(issues, recipeIssue(recipe, "invalid_input_mode", "inputSpec.mode must be none, optional_anchor, required_anchor, or multi_anchor", "inputSpec.mode"))
	}
	inputNames := map[string]struct{}{}
	for _, input := range recipe.InputSpec.Inputs {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			issues = append(issues, recipeIssue(recipe, "missing_input_name", "input name is required", "inputSpec.inputs"))
			continue
		}
		if _, ok := inputNames[name]; ok {
			issues = append(issues, recipeIssue(recipe, "duplicate_input_name", "input "+name+" is declared more than once", "inputSpec.inputs"))
		}
		inputNames[name] = struct{}{}
	}
	for _, placeholder := range Placeholders(recipe.Query.GraphQL) {
		if _, ok := inputNames[placeholder]; !ok {
			issues = append(issues, recipeIssue(recipe, "undeclared_placeholder", "placeholder "+placeholder+" is not declared in inputSpec", "query.graphQL"))
		}
	}
	if recipe.InputSpec.Mode != InputModeNone && strings.TrimSpace(recipe.InputSpec.PrimaryInput) == "" {
		issues = append(issues, recipeIssue(recipe, "missing_primary_input", "anchored recipes must declare inputSpec.primaryInput", "inputSpec.primaryInput"))
	}
	if primary := strings.TrimSpace(recipe.InputSpec.PrimaryInput); primary != "" {
		if _, ok := inputNames[primary]; !ok {
			issues = append(issues, recipeIssue(recipe, "unknown_primary_input", "inputSpec.primaryInput must name a declared input", "inputSpec.primaryInput"))
		}
	}
	return issues
}

func DependenciesFor(prepared *ontologyquery.PreparedQuery) Dependencies {
	if prepared == nil || prepared.Operation == nil {
		return Dependencies{}
	}
	var fragments ast.FragmentDefinitionList
	if prepared.Document != nil {
		fragments = prepared.Document.Fragments
	}
	collector := dependencyCollector{
		roots:        map[string]struct{}{},
		runtimeRoots: map[string]struct{}{},
		fields:       map[string]struct{}{},
		typeNames:    map[string]struct{}{},
		enumValues:   map[string]struct{}{},
		fragments:    fragments,
		visited:      map[string]struct{}{},
	}
	collector.collectSelectionSet(prepared.Operation.SelectionSet, true)
	return Dependencies{
		Roots:        sortedKeys(collector.roots),
		RuntimeRoots: sortedKeys(collector.runtimeRoots),
		Fields:       sortedKeys(collector.fields),
		TypeNames:    sortedKeys(collector.typeNames),
		EnumValues:   sortedKeys(collector.enumValues),
	}
}

func validateOutputContractPaths(recipe Recipe, prepared *ontologyquery.PreparedQuery) []Issue {
	if prepared == nil || prepared.Operation == nil || len(recipe.OutputContract.ExpectedPaths) == 0 {
		return nil
	}
	var fragments ast.FragmentDefinitionList
	if prepared.Document != nil {
		fragments = prepared.Document.Fragments
	}
	actual := responsePathsFor(prepared.Operation.SelectionSet, nil, fragments)
	var issues []Issue
	for _, expected := range recipe.OutputContract.ExpectedPaths {
		expected = strings.TrimSpace(expected)
		if expected == "" {
			continue
		}
		if _, ok := actual[expected]; !ok {
			issues = append(issues, recipeIssue(recipe, "missing_output_path", "outputContract.expectedPaths entry "+expected+" is not selected by query.graphQL", "outputContract.expectedPaths"))
		}
	}
	return issues
}

func responsePathsFor(set ast.SelectionSet, prefix []string, fragments ast.FragmentDefinitionList) map[string]struct{} {
	out := map[string]struct{}{}
	collectResponsePaths(set, prefix, out, fragments, map[string]struct{}{})
	return out
}

func collectResponsePaths(set ast.SelectionSet, prefix []string, out map[string]struct{}, fragments ast.FragmentDefinitionList, visited map[string]struct{}) {
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			name := current.Alias
			if name == "" {
				name = current.Name
			}
			path := append(append([]string(nil), prefix...), name)
			out[strings.Join(path, ".")] = struct{}{}
			collectResponsePaths(current.SelectionSet, path, out, fragments, visited)
		case *ast.InlineFragment:
			collectResponsePaths(current.SelectionSet, prefix, out, fragments, visited)
		case *ast.FragmentSpread:
			if _, ok := visited[current.Name]; ok {
				continue
			}
			fragment := fragments.ForName(current.Name)
			if fragment == nil {
				continue
			}
			visited[current.Name] = struct{}{}
			collectResponsePaths(fragment.SelectionSet, prefix, out, fragments, visited)
			delete(visited, current.Name)
		}
	}
}

type dependencyCollector struct {
	roots        map[string]struct{}
	runtimeRoots map[string]struct{}
	fields       map[string]struct{}
	typeNames    map[string]struct{}
	enumValues   map[string]struct{}
	fragments    ast.FragmentDefinitionList
	visited      map[string]struct{}
}

func (c *dependencyCollector) collectSelectionSet(set ast.SelectionSet, root bool) {
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			if root {
				switch current.Name {
				case "ontology", "code":
					c.runtimeRoots[current.Name] = struct{}{}
				default:
					c.roots[current.Name] = struct{}{}
				}
			} else {
				c.fields[current.Name] = struct{}{}
			}
			for _, arg := range current.Arguments {
				c.collectValue(arg.Value)
			}
			c.collectSelectionSet(current.SelectionSet, false)
		case *ast.InlineFragment:
			if current.TypeCondition != "" {
				c.typeNames[current.TypeCondition] = struct{}{}
			}
			c.collectSelectionSet(current.SelectionSet, root)
		case *ast.FragmentSpread:
			if _, ok := c.visited[current.Name]; ok {
				continue
			}
			fragment := c.fragments.ForName(current.Name)
			if fragment == nil {
				continue
			}
			if fragment.TypeCondition != "" {
				c.typeNames[fragment.TypeCondition] = struct{}{}
			}
			c.visited[current.Name] = struct{}{}
			c.collectSelectionSet(fragment.SelectionSet, root)
			delete(c.visited, current.Name)
		}
	}
}

func (c *dependencyCollector) collectValue(value *ast.Value) {
	if value == nil {
		return
	}
	if value.Kind == ast.EnumValue {
		c.enumValues[value.Raw] = struct{}{}
	}
	for _, child := range value.Children {
		c.collectValue(child.Value)
	}
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func recipeIssue(recipe Recipe, code, message, field string) Issue {
	return Issue{
		Code:    code,
		Message: message,
		Recipe:  recipe.ID,
		Path:    recipe.Source.Path,
		Line:    recipe.Source.Line,
		Field:   field,
	}
}
