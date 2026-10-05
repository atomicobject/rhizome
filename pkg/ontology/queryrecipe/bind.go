package queryrecipe

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var placeholderPattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func BindVariables(recipe Recipe, inputs map[string]string) (map[string]any, map[string]string, []Issue) {
	declared := declaredInputs(recipe)
	resolved := make(map[string]string, len(declared))
	variables := make(map[string]any, len(declared))
	var issues []Issue

	for _, name := range slices.Sorted(maps.Keys(inputs)) {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, ok := declared[name]; !ok {
			issues = append(issues, recipeIssue(recipe, "undeclared_input", "input "+name+" is not declared in inputSpec", "inputSpec.inputs"))
		}
	}
	for _, name := range Placeholders(recipe.Query.GraphQL) {
		issues = append(issues, recipeIssue(recipe, "deprecated_placeholder", "placeholder "+name+" must be replaced with a GraphQL variable", "query.graphQL"))
	}
	if len(issues) > 0 {
		return variables, resolved, issues
	}

	for _, name := range slices.Sorted(maps.Keys(declared)) {
		input := declared[name]
		value, ok := inputs[name]
		if !ok && input.Default != "" {
			value = input.Default
			ok = true
		}
		if !ok {
			if input.Required {
				return variables, resolved, []Issue{recipeIssue(recipe, "missing_required_input", "input "+name+" is required", "inputSpec."+name)}
			}
			continue
		}
		resolved[name] = value
		coerced, err := recipeVariableValue(recipe, input, value)
		if err != nil {
			return variables, resolved, []Issue{recipeIssue(recipe, "invalid_input_value", err.Error(), "inputSpec."+name)}
		}
		variables[name] = coerced
	}
	return variables, resolved, nil
}

func Placeholders(query string) []string {
	matches := placeholderPattern.FindAllStringSubmatch(query, -1)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func recipeVariableValue(recipe Recipe, input Input, value string) (any, error) {
	if recipe.InputSpec.Mode == InputModeMultiAnchor && strings.TrimSpace(recipe.InputSpec.PrimaryInput) == input.Name {
		return recipeMultiAnchorValue(value, input.Kind)
	}
	return scalarRecipeVariableValue(value, input.Kind)
}

func scalarRecipeVariableValue(value, kind string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "list":
		return stringListRecipeVariableValue(value)
	case "int", "integer":
		i, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("input value %q must be an integer", value)
		}
		return i, nil
	case "bool", "boolean":
		b, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("input value %q must be a boolean", value)
		}
		return b, nil
	default:
		return value, nil
	}
}

func stringListRecipeVariableValue(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(value), &values); err == nil {
		return values, nil
	}
	return []string{value}, nil
}

func recipeMultiAnchorValue(value, kind string) (any, error) {
	var values []string
	if strings.TrimSpace(value) != "" {
		if err := json.Unmarshal([]byte(value), &values); err != nil {
			values = []string{value}
		}
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "int", "integer":
		out := make([]int64, 0, len(values))
		for _, raw := range values {
			coerced, err := scalarRecipeVariableValue(raw, kind)
			if err != nil {
				return nil, err
			}
			out = append(out, coerced.(int64))
		}
		return out, nil
	case "bool", "boolean":
		out := make([]bool, 0, len(values))
		for _, raw := range values {
			coerced, err := scalarRecipeVariableValue(raw, kind)
			if err != nil {
				return nil, err
			}
			out = append(out, coerced.(bool))
		}
		return out, nil
	default:
		return values, nil
	}
}

func declaredInputs(recipe Recipe) map[string]Input {
	out := make(map[string]Input, len(recipe.InputSpec.Inputs)+1)
	for _, input := range recipe.InputSpec.Inputs {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			continue
		}
		input.Name = name
		out[name] = input
	}
	if recipe.InputSpec.PrimaryInput != "" {
		if input, ok := out[recipe.InputSpec.PrimaryInput]; ok {
			input.Required = input.Required || recipe.InputSpec.Mode == InputModeRequiredAnchor || recipe.InputSpec.Mode == InputModeMultiAnchor
			out[recipe.InputSpec.PrimaryInput] = input
		}
	}
	return out
}

func sampleValue(input Input) string {
	if input.Default != "" {
		return input.Default
	}
	switch strings.ToLower(strings.TrimSpace(input.Kind)) {
	case "list":
		return `["example"]`
	case "int", "integer":
		return "1"
	case "bool", "boolean":
		return "false"
	case "enum":
		return "example"
	default:
		return "example"
	}
}

func MergeAnchorInput(recipe Recipe, inputs map[string]string, anchors []string) (map[string]string, error) {
	out := make(map[string]string, len(inputs)+1)
	for k, v := range inputs {
		out[k] = v
	}
	if len(anchors) == 0 {
		return out, nil
	}
	filtered := anchors[:0]
	for _, anchor := range anchors {
		if strings.TrimSpace(anchor) == "" || strings.TrimSpace(anchor) == "[]" {
			continue
		}
		filtered = append(filtered, anchor)
	}
	anchors = filtered
	if len(anchors) == 0 {
		return out, nil
	}
	primary := strings.TrimSpace(recipe.InputSpec.PrimaryInput)
	if primary == "" {
		return nil, fmt.Errorf("recipe %s does not declare primaryInput for --anchor", recipe.ID)
	}
	if recipe.InputSpec.Mode == InputModeMultiAnchor {
		data, err := json.Marshal(anchors)
		if err != nil {
			return nil, err
		}
		out[primary] = string(data)
		return out, nil
	}
	out[primary] = anchors[0]
	return out, nil
}
