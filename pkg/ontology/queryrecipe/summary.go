package queryrecipe

import (
	"fmt"
	"sort"
	"strings"
)

const maxRecipeExampleHints = 3

type RecipeSummary struct {
	ID           string       `json:"id"`
	Name         string       `json:"name,omitempty"`
	Problem      string       `json:"problem,omitempty"`
	Input        InputSummary `json:"input"`
	Tags         []string     `json:"tags,omitempty"`
	Source       Source       `json:"source,omitempty"`
	ExampleCount int          `json:"exampleCount,omitempty"`
	ExampleHints []string     `json:"exampleHints,omitempty"`
}

type InputSummary struct {
	Mode               InputMode `json:"mode"`
	PrimaryInput       string    `json:"primaryInput,omitempty"`
	Required           []string  `json:"required,omitempty"`
	Optional           []string  `json:"optional,omitempty"`
	BroadAlternativeID string    `json:"broadAlternativeId,omitempty"`
}

func Summaries(recipes []Recipe) []RecipeSummary {
	summaries := make([]RecipeSummary, 0, len(recipes))
	for _, recipe := range recipes {
		summaries = append(summaries, Summary(recipe))
	}
	return summaries
}

func Summary(recipe Recipe) RecipeSummary {
	return RecipeSummary{
		ID:           recipe.ID,
		Name:         recipe.Name,
		Problem:      truncateSummaryText(recipe.Problem, 240),
		Input:        summarizeRecipeInput(recipe.InputSpec),
		Tags:         append([]string(nil), recipe.Tags...),
		Source:       recipe.Source,
		ExampleCount: len(recipe.Examples),
		ExampleHints: summarizeRecipeExamples(recipe),
	}
}

func summarizeRecipeInput(input InputSpec) InputSummary {
	summary := InputSummary{
		Mode:               input.Mode,
		PrimaryInput:       input.PrimaryInput,
		BroadAlternativeID: input.BroadAlternativeID,
	}
	for _, field := range input.Inputs {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if field.Required {
			summary.Required = append(summary.Required, name)
		} else {
			summary.Optional = append(summary.Optional, name)
		}
	}
	return summary
}

func summarizeRecipeExamples(recipe Recipe) []string {
	if len(recipe.Examples) == 0 {
		return nil
	}
	hints := make([]string, 0, min(len(recipe.Examples), maxRecipeExampleHints))
	for _, example := range recipe.Examples {
		if len(hints) >= maxRecipeExampleHints {
			break
		}
		if name := strings.TrimSpace(example.Name); name != "" {
			hints = append(hints, name)
			continue
		}
		if hint := summarizeRecipeExampleInputs(recipe, example.Inputs); hint != "" {
			hints = append(hints, hint)
		}
	}
	return hints
}

func summarizeRecipeExampleInputs(recipe Recipe, inputs map[string]string) string {
	if len(inputs) == 0 {
		return ""
	}
	if primary := strings.TrimSpace(recipe.InputSpec.PrimaryInput); primary != "" {
		if value := strings.TrimSpace(inputs[primary]); value != "" {
			return truncateSummaryHint(fmt.Sprintf("%s=%s", primary, value))
		}
	}
	keys := make([]string, 0, len(inputs))
	for key := range inputs {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(inputs[key]); value != "" {
			return truncateSummaryHint(fmt.Sprintf("%s=%s", key, value))
		}
	}
	return ""
}

func truncateSummaryHint(value string) string {
	return truncateSummaryText(value, 80)
}

func truncateSummaryText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-3]) + "..."
}
