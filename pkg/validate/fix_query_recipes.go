package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func buildQueryRecipeFixes(issues []queryrecipe.Issue) ([]FixAction, error) {
	if len(issues) == 0 {
		return nil, nil
	}
	issues = append([]queryrecipe.Issue(nil), issues...)
	sortQueryRecipeIssues(issues)
	actions := make([]FixAction, 0, len(issues))
	for _, issue := range issues {
		path := strings.TrimSpace(issue.Path)
		if path == "" || strings.TrimSpace(issue.Code) == "" {
			continue
		}
		issueKey, err := StableIssueKey(CheckQueryRecipes, queryRecipeValidationIssue(issue))
		if err != nil {
			return nil, err
		}
		actions = append(actions, FixAction{
			ID:            "adapt-query-recipe:" + issueKey,
			Check:         CheckQueryRecipes,
			IssueCode:     issue.Code,
			Kind:          "adapt_query_recipe",
			Safety:        FixSafetyAgent,
			Title:         "Adapt saved query recipe",
			Summary:       fmt.Sprintf("%s: %s; review the recipe problem, input contract, output contract, and adaptation guidance", issue.Code, issue.Message),
			InstanceCount: 1,
			IssueKeys:     []string{issueKey},
			AffectedPaths: []string{path},
		})
	}
	return actions, nil
}

func normalizeQueryRecipeIssues(vaultPath string, issues []queryrecipe.Issue) []queryrecipe.Issue {
	normalized := append([]queryrecipe.Issue(nil), issues...)
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	for index := range normalized {
		path := strings.TrimSpace(normalized[index].Path)
		if path == "" || vaultPaths.Root() == "" {
			continue
		}
		if rel, err := vaultPaths.RelStrict(path); err == nil {
			normalized[index].Path = rel.String()
		}
	}
	sortQueryRecipeIssues(normalized)
	return normalized
}

func sortQueryRecipeIssues(issues []queryrecipe.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left, right := issues[i], issues[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Recipe != right.Recipe {
			return left.Recipe < right.Recipe
		}
		if left.Field != right.Field {
			return left.Field < right.Field
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Message < right.Message
	})
}
