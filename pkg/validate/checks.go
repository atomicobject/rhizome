package validate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/paths"
	validationcatalog "github.com/atomicobject/rhizome/pkg/validate/catalog"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func RunSkillOverlays(runCtx RunContext) CheckResult {
	manifest, err := initactions.ValidateBundledSkillOverlays()
	if err != nil {
		return CheckResult{Name: CheckSkillOverlays, Error: err.Error()}
	}
	return skillOverlayCheckResult(manifest)
}

func skillOverlayCheckResult(manifest initactions.SkillOverlayManifest) CheckResult {
	result := CheckResult{Name: CheckSkillOverlays, OK: true}
	result.IssueCount = len(manifest.Issues)
	applied := 0
	for _, entry := range manifest.Entries {
		if strings.TrimSpace(entry.OverlayID) != "" {
			applied++
		}
	}
	result.Summary = fmt.Sprintf("%d overlay fragment(s) checked", applied)
	if len(manifest.Issues) == 0 {
		return result
	}
	result.OK = false
	sort.SliceStable(manifest.Issues, func(i, j int) bool {
		left := manifest.Issues[i]
		right := manifest.Issues[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Skill != right.Skill {
			return left.Skill < right.Skill
		}
		return left.Message < right.Message
	})
	result.Issues = make([]Issue, 0, len(manifest.Issues))
	for _, issue := range manifest.Issues {
		result.Issues = append(result.Issues, Issue{
			Code:    issue.Code,
			Path:    issue.Path,
			Field:   issue.Slot,
			Source:  issue.Overlay,
			Target:  issue.Skill,
			Message: issue.Message,
		})
	}
	return result
}

// RunViews validates repo-tracked configured view definitions.
func RunViews(ctx context.Context, runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckViews, OK: true}
	views, loadIssues := viewconfig.LoadDefaultSource(runCtx.VaultPath)
	if len(views) == 0 && len(loadIssues) == 0 {
		result.Skipped = true
		result.Summary = "no configured views found"
		return result
	}
	opts := viewconfig.ValidateOptions{}
	var execSchema *ontologyquery.ExecutableSchema
	schema, err := ontology.LoadSchema(runCtx.VaultPath)
	if errors.Is(err, ontology.ErrNoOntologyFiles) {
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
				Variant: newIssueVariant(issue.Variant, issue.Variant),
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
		result.Skipped = true
		result.Summary = "no query recipes found"
		return result
	}
	schema, err := ontology.LoadSchema(runCtx.VaultPath)
	if errors.Is(err, ontology.ErrNoOntologyFiles) {
		result.Skipped = true
		result.Summary = "no ontology schema"
		if len(loadIssues) > 0 {
			result.Skipped = false
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
		result.Skipped = true
		result.Summary = "no query recipes found"
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

// RunOntology validates notes against their ontology schema.
func RunOntology(ctx context.Context, runCtx RunContext) CheckResult {
	if err := runCtx.NoteMetadata.Validate(); err != nil {
		return CheckResult{Name: CheckOntology, Error: fmt.Sprintf("note metadata indexer: %v", err)}
	}
	runtime, cleanup, err := ontology.EnsureFreshRuntime(ctx, runCtx.NoteMetadata, runCtx.VaultDef, runCtx.NoteReader)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return CheckResult{Name: CheckOntology, Error: err.Error()}
	}
	return runOntologyWithRuntime(ctx, runCtx, runtime)
}

func runOntologyWithRuntime(ctx context.Context, runCtx RunContext, runtime *ontology.Runtime) CheckResult {
	result := CheckResult{Name: CheckOntology, OK: true}
	if runtime == nil || runtime.Schema == nil {
		result.Skipped = true
		result.Summary = "no ontology schema"
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

// RunCodeAnchors validates code reference anchors.
func RunCodeAnchors(ctx context.Context, runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckCodeAnchors, OK: true}
	codeCfg, err := obsidian.LoadCodeConfig(runCtx.VaultPath)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	if !codeCfg.Enabled {
		result.Skipped = true
		result.Summary = "code indexing disabled"
		return result
	}
	codeCfg.IndexPath = obsidian.UnifiedIndexPath(runCtx.VaultPath, codeCfg.IndexPath)
	store, cleanup, err := obsidian.OpenIntelStoreFromConfig(runCtx.VaultPath, codeCfg, true)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	if store == nil {
		result.Skipped = true
		result.Summary = "code index unavailable"
		return result
	}
	return RunCodeAnchorsWithStore(ctx, runCtx, store)
}

// RunCodeAnchorsWithStore validates code reference anchors against an already
// open intel store.
func RunCodeAnchorsWithStore(ctx context.Context, runCtx RunContext, store *semdb.Store) CheckResult {
	result := CheckResult{Name: CheckCodeAnchors, OK: true}
	if store == nil {
		result.Skipped = true
		result.Summary = "code index unavailable"
		return result
	}
	codeCfg, err := obsidian.LoadCodeConfig(runCtx.VaultPath)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	if !codeCfg.Enabled {
		result.Skipped = true
		result.Summary = "code indexing disabled"
		return result
	}
	codeCfg.IndexPath = obsidian.UnifiedIndexPath(runCtx.VaultPath, codeCfg.IndexPath)
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

func normalizeCheckName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	return strings.ReplaceAll(name, "-", "_")
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
