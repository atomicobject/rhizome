package validate

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/diag"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	validationcatalog "github.com/atomicobject/rhizome/pkg/validate/catalog"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// RunViews validates repo-tracked configured view definitions.
func RunViews(ctx context.Context, runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckViews, OK: true}
	views, loadIssues := viewconfig.LoadDefaultSource(runCtx.VaultPath)
	if len(views) == 0 && len(loadIssues) == 0 {
		result.NotApplicable = "no configured views found"
		result.Summary = result.NotApplicable
		return result
	}
	opts := viewconfig.ValidateOptions{}
	var execSchema *ontologyquery.ExecutableSchema
	schema, err := ontoschema.LoadSchema(runCtx.VaultPath)
	if errors.Is(err, ontoschema.ErrNoOntologyFiles) {
		result.Notes = append(result.Notes, "no ontology schema; skipping source target and GraphQL validation")
	} else if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	} else if schema != nil {
		if execSchema, err = ontologyquery.BuildExecutableSchema(schema); err != nil {
			result.Notes = append(result.Notes, "query schema unavailable; skipping GraphQL validation: "+err.Error())
		}
		opts = viewconfig.SchemaValidateOptions(schema)
		recipes, _ := queryrecipe.LoadDefaultSources(runCtx.VaultPath)
		opts.QueryRecipeIDs = make(map[string]struct{}, len(recipes))
		opts.QueryRecipeRowPaths = make(map[string]string, len(recipes))
		for _, recipe := range recipes {
			opts.QueryRecipeIDs[recipe.ID] = struct{}{}
			opts.QueryRecipeRowPaths[recipe.ID] = recipe.OutputContract.RowPath
		}
	}
	validation := viewconfig.Validate(views, opts)
	found := append(loadIssues, validation.Issues...)
	found = append(found, customViewScriptIssues(runCtx.VaultPath, validation.Views, execSchema)...)
	issues := normalizeViewIssues(runCtx.VaultPath, found)
	result.IssueCount = len(issues)
	result.Summary = fmt.Sprintf("%d views checked", len(views))
	if len(issues) > 0 {
		result.OK = false
		result.Issues = make([]Issue, 0, len(issues))
		for _, issue := range issues {
			result.Issues = append(result.Issues, Issue{
				Code:    issue.Code,
				Path:    issue.Path,
				Field:   issue.Field,
				Target:  issue.View,
				Message: issue.Message,
				Line:    issue.Line,
				Variant: diag.NewVariant(issue.Variant, issue.Variant),
			})
		}
	}
	return result
}

func normalizeViewIssues(vaultPath string, issues []viewconfig.Issue) []viewconfig.Issue {
	normalized := append([]viewconfig.Issue(nil), issues...)
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return normalized
	}
	for index := range normalized {
		if rel, relErr := vaultPaths.RelStrict(normalized[index].Path); relErr == nil {
			normalized[index].Path = rel.String()
		}
	}
	return normalized
}

// RunQueryRecipes validates saved ontology query recipes against the current
// query schema. Docs: [[saved-query-recipes#^spec-0052-us3-ac4]] is why
// ambiguous schema drift produces agent-required repair context instead of
// silent rewrites.
func RunQueryRecipes(ctx context.Context, runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckQueryRecipes, OK: true}
	recipes, loadIssues := loadRecipeValidationSources(runCtx.VaultPath)
	loadIssues = normalizeQueryRecipeIssues(runCtx.VaultPath, loadIssues)
	if len(recipes) == 0 && len(loadIssues) == 0 {
		result.NotApplicable = "no query recipes found"
		result.Summary = result.NotApplicable
		return result
	}
	schema, err := ontoschema.LoadSchema(runCtx.VaultPath)
	if errors.Is(err, ontoschema.ErrNoOntologyFiles) {
		result.NotApplicable = "no ontology schema"
		result.Summary = result.NotApplicable
		if len(loadIssues) > 0 {
			result.NotApplicable = ""
			result.OK = false
			result.IssueCount = len(loadIssues)
			result.Summary = "query recipe load issues"
			for _, issue := range loadIssues {
				result.Issues = append(result.Issues, queryRecipeValidationIssue(issue))
			}
		}
		return result
	}
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	validation := queryrecipe.Validate(recipes, execSchema)
	issues := append(loadIssues, normalizeQueryRecipeIssues(runCtx.VaultPath, validation.Issues)...)
	if len(recipes) == 0 && len(issues) == 0 {
		result.NotApplicable = "no query recipes found"
		result.Summary = result.NotApplicable
		return result
	}
	result.IssueCount = len(issues)
	result.Summary = fmt.Sprintf("%d recipes checked", len(recipes))
	if len(issues) > 0 {
		result.OK = false
		allIssues := make([]Issue, 0, len(issues))
		for _, issue := range issues {
			allIssues = append(allIssues, queryRecipeValidationIssue(issue))
		}
		result.Buckets = BuildMigrationBuckets(allIssues)
		result.Issues = append(result.Issues, allIssues...)
		result.Fixes, err = buildQueryRecipeFixes(issues)
		if err != nil {
			result.Error = fmt.Sprintf("identify query-recipe issue: %v", err)
			result.IssueCount = 0
			result.Issues = nil
			result.Fixes = nil
			result.Buckets = nil
		}
	}
	return result
}

func queryRecipeValidationIssue(issue queryrecipe.Issue) Issue {
	return Issue{
		Code: issue.Code, Path: issue.Path, Field: issue.Field,
		Target: issue.Recipe, Message: issue.Message, Line: issue.Line,
		Data: mustMarshal(QueryRecipeIssueData{
			Recipe: issue.Recipe, Line: issue.Line, Diagnostic: issue.Message,
		}),
	}
}

func runOntologyWithRuntime(ctx context.Context, runCtx RunContext, runtime *ontology.Runtime) CheckResult {
	result := CheckResult{Name: CheckOntology, OK: true}
	if runtime == nil || runtime.Schema == nil {
		result.NotApplicable = "no ontology schema"
		result.Summary = result.NotApplicable
		return result
	}
	if runtime.Store != nil {
		if _, err := runtime.Store.GetOntologySchemaState(ctx); err != nil {
			result.OK = false
			result.Error = err.Error()
			return result
		}
	}
	allIssues := make([]Issue, 0, len(runtime.Issues))
	for _, issue := range runtime.Issues {
		if runCtx.inPostcheckScope(issue.NotePath, issue.FixTarget) {
			allIssues = append(allIssues, ontologyValidationIssue(issue))
		}
	}
	if len(allIssues) > 0 {
		result.Buckets = BuildMigrationBuckets(allIssues)
		result.Issues = append(result.Issues, allIssues...)
	}
	identifierIssues, identifierFixes, identifierErr := RunIdentifierBlockIDMigration(ctx, runCtx, runtime)
	if identifierErr != nil {
		result.OK = false
		result.Error = identifierErr.Error()
		return result
	}
	result.Issues = append(result.Issues, identifierIssues...)
	result.Fixes = append(result.Fixes, identifierFixes...)
	cycleIssues, err := parentCycleIssues(ctx, runCtx, runtime)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	result.Issues = append(result.Issues, cycleIssues...)
	result.IssueCount = len(allIssues) + len(identifierIssues) + len(cycleIssues)
	if result.IssueCount == 0 {
		result.Summary = "ontology valid"
	} else {
		result.Summary = fmt.Sprintf("%d ontology issues", result.IssueCount)
	}
	return result
}

// RunCodeAnchorsWithStore validates code reference anchors against an already
// open intel store.
func RunCodeAnchorsWithStore(ctx context.Context, runCtx RunContext, store *semdb.Store) CheckResult {
	result := CheckResult{Name: CheckCodeAnchors, OK: true}
	if store == nil {
		result.Error = "prepared code index store is unavailable"
		return result
	}
	resolvedConfig1, configErr1 := repoconfig.Load(runCtx.VaultPath)
	codeCfg, err := resolvedConfig1.Code, configErr1
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	if !codeCfg.Enabled {
		result.NotApplicable = "code indexing disabled"
		result.Summary = result.NotApplicable
		return result
	}
	codeCfg.IndexPath = repoconfig.UnifiedIndexPath(runCtx.VaultPath, codeCfg.IndexPath)
	tailIndex := codeanchor.NewPathTailIndex(5)
	svc := codeanchor.NewServiceWithOptions(
		store,
		nil,
		codeanchor.WithBasePath(runCtx.VaultPath),
		codeanchor.WithPathTailIndex(tailIndex),
		codeanchor.WithoutWarmCache(),
	)
	if svc == nil {
		result.OK = false
		result.Error = "code anchor service unavailable"
		return result
	}
	validationResults, err := svc.ValidateAnchors(ctx)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	anchorIDs := make([]int64, 0, len(validationResults))
	for _, item := range validationResults {
		if item.Status != codeanchor.ValidationValid {
			anchorIDs = append(anchorIDs, item.Anchor.ID)
		}
	}
	notePathsByAnchor, err := store.NotePathsByAnchorIDs(ctx, anchorIDs)
	if err != nil {
		result.OK = false
		result.Error = fmt.Sprintf("load code-anchor source paths: %v", err)
		return result
	}

	issues := make([]Issue, 0)
	fixes := make([]FixAction, 0)
	for _, item := range validationResults {
		if item.Status == codeanchor.ValidationValid {
			continue
		}
		notePaths := append([]string(nil), notePathsByAnchor[item.Anchor.ID]...)
		sort.Strings(notePaths)
		candidates := sortedUnique(append([]string(nil), item.SuffixMatches...))
		item.SuffixMatches = candidates
		owners := notePaths
		if len(owners) == 0 {
			owners = []string{""}
		}
		guidance := "Update the code-anchors selector, rebuild the code index if needed, then run `rzm validate code-anchors`."
		for _, owner := range owners {
			path := owner
			if path == "" {
				path = item.Anchor.Label
			}
			issue := Issue{
				Code:    string(item.Status),
				Path:    path,
				Field:   "code-anchors",
				Message: item.Message,
				Data: mustMarshal(CodeAnchorData{
					Label: item.Anchor.Label, Language: string(item.Anchor.Lang), Status: string(item.Status),
					Candidates: candidates, Guidance: guidance,
				}),
			}
			if item.Anchor.BaseSym != nil {
				if item.Anchor.BaseSym.Pkg != "" {
					issue.Target = item.Anchor.BaseSym.Pkg + "." + item.Anchor.BaseSym.Name
				} else {
					issue.Target = item.Anchor.BaseSym.Name
				}
			}
			issueKey, err := StableIssueKey(CheckCodeAnchors, issue)
			if err != nil {
				result.OK = false
				result.Error = fmt.Sprintf("identify code-anchor issue: %v", err)
				return result
			}
			issues = append(issues, issue)
			fixPaths := []string(nil)
			if owner != "" {
				fixPaths = []string{owner}
			}
			fixes = append(fixes, buildCodeAnchorFix(runCtx, item, fixPaths, issueKey))
		}
	}
	result.IssueCount = len(issues)
	result.Issues = issues
	result.Fixes = fixes
	if len(issues) == 0 {
		result.Summary = fmt.Sprintf("%d symbol anchors checked", len(validationResults))
	} else {
		result.Summary = fmt.Sprintf("%d anchor issues", len(issues))
	}
	return result
}

// ResolveChecks normalizes and validates a list of check names.
func ResolveChecks(raw []string) ([]string, error) {
	filtered := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || trimmed == "[]" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	if len(filtered) == 0 {
		return append([]string(nil), DefaultChecks...), nil
	}
	seen := make(map[string]struct{}, len(filtered))
	out := make([]string, 0, len(filtered))
	for _, item := range filtered {
		normalized, ok := CanonicalCheck(item)
		if !ok {
			return nil, fmt.Errorf("unknown check %q (valid: %s)", item, validCheckNames())
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

// CanonicalCheck normalizes a check name to its canonical form.
func CanonicalCheck(raw string) (string, bool) {
	canonical, ok := validationcatalog.Canonical(raw)
	if !ok {
		return "", false
	}
	for _, registration := range checkRegistry {
		if registration.Name == canonical {
			return canonical, true
		}
	}
	return "", false
}

func validCheckNames() string {
	names := make([]string, 0, len(checkRegistry))
	for _, registration := range checkRegistry {
		names = append(names, registration.CLIName)
	}
	return strings.Join(names, ", ")
}

func loadRecipeValidationSources(vaultPath string) ([]queryrecipe.Recipe, []queryrecipe.Issue) {
	return queryrecipe.LoadDefaultSources(vaultPath)
}
