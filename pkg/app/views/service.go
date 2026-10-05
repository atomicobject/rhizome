package views

import (
	"cmp"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func New(opts ServiceOptions) *Service {
	if opts.NoteReader == nil {
		opts.NoteReader = &obsidian.Note{}
	}
	if opts.ReadOverlay != nil {
		opts.QueryDeps.ReadOverlay = opts.ReadOverlay
	}
	if opts.SourceResolver == nil {
		opts.SourceResolver = defaultSourceResolver{opts: opts}
	}
	return &Service{opts: opts}
}

// Catalog lists views and the targets they serve. Generated workflow views
// open as a Board when their open work fits one (SPEC-0112), and the targets'
// default choices read the same decision.
func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	catalog, err := s.catalog(ctx, false)
	if err != nil {
		return Catalog{}, err
	}
	for i := range catalog.Views {
		catalog.Views[i].Defaults.Variant = s.generatedDefaultVariant(ctx, catalog.Views[i])
	}
	catalog.Targets = resolveTargets(s.opts.Schema, catalog.Views)
	return catalog, nil
}

// catalog loads and validates definitions without targets or count reads, so
// single-view lookups stay cheap.
func (s *Service) catalog(ctx context.Context, includeHidden bool) (Catalog, error) {
	defs, loadIssues := s.loadDefinitions()
	for i := range defs {
		defs[i].Generated = false
		defs[i].Origin = viewconfig.OriginRepository
	}
	opts := s.validateOptions()
	validation := viewconfig.Validate(defs, opts)
	generated := generatedDefaults(s.opts.Schema)
	bundled, bundledIssues := s.bundledDefinitions(validation.Views, opts)

	all := append([]viewconfig.ViewDefinition(nil), validation.Views...)
	all = append(all, bundled...)
	all = append(all, generated...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].ID == all[j].ID {
			return all[i].Generated && !all[j].Generated
		}
		return all[i].ID < all[j].ID
	})

	issues := append(loadIssues, validation.Issues...)
	issues = append(issues, bundledIssues...)
	entries := catalogEntries(all, issues, includeHidden)
	return Catalog{Views: entries, Issues: issues}, nil
}

// View returns one catalog entry, with the same default variant the catalog
// reports for it.
func (s *Service) View(ctx context.Context, id string) (CatalogEntry, error) {
	entry, err := s.entry(ctx, id)
	if err != nil {
		return CatalogEntry{}, err
	}
	entry.Defaults.Variant = s.generatedDefaultVariant(ctx, entry)
	return entry, nil
}

func (s *Service) entry(ctx context.Context, id string) (CatalogEntry, error) {
	catalog, err := s.catalog(ctx, true)
	if err != nil {
		return CatalogEntry{}, err
	}
	id = strings.TrimSpace(id)
	for _, entry := range catalog.Views {
		if entry.ID == id {
			return entry, nil
		}
	}
	return CatalogEntry{}, fmt.Errorf("%w: %s", ErrViewNotFound, id)
}

func (s *Service) Execute(ctx context.Context, id string, req ExecuteRequest) (ExecuteResponse, error) {
	entry, err := s.entry(ctx, id)
	if err != nil {
		return ExecuteResponse{}, err
	}
	if hasBlockingIssues(entry.Issues) {
		return ExecuteResponse{}, fmt.Errorf("%w: %s", ErrInvalidView, id)
	}
	def := entry.Definition
	// The fingerprint names the definition, not the count-derived default
	// layout, so a Save request does not conflict when counts move.
	defFP := fingerprintDefinition(def)
	if strings.TrimSpace(req.Variant) == "" {
		entry.Defaults.Variant = s.generatedDefaultVariant(ctx, entry)
		def.Defaults.Variant = entry.Defaults.Variant
	}
	if def.SourceSpec.Kind == viewconfig.SourceKindCustom {
		return ExecuteResponse{}, fmt.Errorf("%w: custom view %s renders in the browser at /views/%s and has no rows to execute", ErrInvalidRequest, id, id)
	}
	variant := stateVariant(def, req)
	if !hasVariant(entry.AvailableVariants, variant) {
		return ExecuteResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedVariant, variant)
	}
	state := normalizeState(def, req)
	warnings := executionWarnings(def, req)

	source, err := s.opts.SourceResolver.ResolveSource(ctx, def, executeRequestFromState(state, req))
	if err != nil {
		return ExecuteResponse{}, err
	}
	for _, warning := range source.Warnings {
		if warning.Code == "link_filter_unresolved" || warning.Code == "link_filter_ambiguous" || warning.Code == "link_filter_resolution_failed" || warning.Code == "link_filter_resolution_incomplete" {
			return ExecuteResponse{}, fmt.Errorf("%w: %s", ErrInvalidRequest, warning.Message)
		}
	}
	rows := normalizeRows(source.Rows)
	constraints := executionConstraints(source, state)
	plan := executionPlanSummary(source, constraints, len(rows))
	capabilities := mergeCapabilities(source.Capabilities, rows, plan.SourceCompleteness)
	warnings = append(warnings, source.Warnings...)
	warnings = append(warnings, executionCapBoundWarnings(plan)...)
	filtered, err := applySearchAndFilters(rows, constraints.Search, constraints.Filters, capabilities)
	if err != nil {
		return ExecuteResponse{}, err
	}
	var profile *ontology.TypeProfile
	if derived, ok := sourceProfile(s.opts.Schema, def); ok {
		profile = &derived
	}
	group := state.Group
	if err := checkGroupBucket(group, capabilities); err != nil {
		return ExecuteResponse{}, err
	}
	laneWarning, err := checkBoardFields(def, profile, &state, strings.TrimSpace(req.LaneField) != "", capabilities)
	if err != nil {
		return ExecuteResponse{}, err
	}
	if laneWarning != nil {
		warnings = append(warnings, *laneWarning)
	}
	var relationErr error
	scope := source.relationScope
	if scope != nil {
		// Group headers and filter options show link targets by title, which
		// needs those fields' targets beyond the page.
		var err error
		group, err = canonicalRelationGroupValues(ctx, scope, group, capabilities)
		relationErr = cmp.Or(relationErr, err)
		rows, filtered, err = hydrateGroupAndFacetTitles(ctx, scope, rows, filtered, capabilities, group)
		relationErr = cmp.Or(relationErr, err)
		capabilities = withRelationFacetLabels(capabilities, rows)
	}
	var stats *ExecutionStats
	var counted []string
	if profile != nil {
		counted = projectedCountFields(s.opts.Schema, def, executeRequestFromState(state, req), capabilities)
		stats = executionStats(s.opts.Schema, def, *profile, filtered, capabilities, counted, time.Now())
		stats.Truncated = plan.SourceCompleteness == SourceBounded
	}
	sortRows(filtered, constraints.Sort, capabilities)
	groupedRows, allGroups := groupRows(filtered, group, capabilities, entry.Generated)
	pageRows, pageInfo := paginateRows(groupedRows, state.Page)
	if scope != nil {
		var err error
		pageRows, err = hydrateRelationValueTitles(ctx, scope, pageRows)
		relationErr = cmp.Or(relationErr, err)
		if variant == "card" || variant == "kanban" {
			// Boards and record briefs name linking records with their
			// statuses; tables show counts.
			pageRows, err = hydrateReverseRelationValues(ctx, scope, s.opts.Schema, projectedReverseFields(profile, counted), pageRows)
			relationErr = cmp.Or(relationErr, err)
			pageRows, err = hydrateRelationStatuses(ctx, scope, s.opts.Schema, pageRows)
			relationErr = cmp.Or(relationErr, err)
		}
	}
	if relationErr != nil {
		warnings = append(warnings, Warning{Code: "view_relation_hydration_failed", Message: relationErr.Error()})
	}
	groups := groupsForPage(allGroups, pageInfo.Offset, pageInfo.Returned)
	columns := tableColumns(def, capabilities)
	var card *CardLayout
	var board *BoardLayout
	if variant == "card" || variant == "kanban" {
		card = resolveCardLayout(def, variant, capabilities)
	}
	if variant == "kanban" {
		var boardWarning *Warning
		board, boardWarning = resolveBoardLayout(def, group, capabilities, filtered, allGroups, pageInfo)
		if boardWarning != nil {
			warnings = append(warnings, *boardWarning)
		}
		if board != nil {
			laneField := state.LaneField
			if laneField == "" {
				laneField = defaultLaneField(profile, board.ColumnField, capabilities, filtered)
			}
			if capability, ok := resolveCapability(capabilities, laneField); ok && laneField != laneFieldNone {
				board.LaneField = capability.Key
				board.Lanes = boardLanes(board, capability, pageRows)
			}
		}
	}
	plan.Warnings = appendPlanWarnings(plan.Warnings, warnings)

	sourceFP := source.SourceFingerprint
	if sourceFP == "" {
		sourceFP = fingerprintSource(def, rows)
	}
	execFP := fingerprintExecution(defFP, sourceFP, state, variant)
	// Frontends iterate these directly (e.g. `execution.rows.length`); a nil
	// slice serializes as JSON `null` and crashes the renderer. Always return
	// concrete empty slices instead.
	if pageRows == nil {
		pageRows = []TableRow{}
	}
	if columns == nil {
		columns = []TableColumn{}
	}
	if capabilities == nil {
		capabilities = []FieldCapability{}
	}
	return ExecuteResponse{
		View:                  entry,
		Variant:               variant,
		State:                 state,
		Capabilities:          capabilities,
		Columns:               columns,
		Rows:                  pageRows,
		Groups:                groups,
		Card:                  card,
		Board:                 board,
		PageInfo:              pageInfo,
		Profile:               profile,
		Stats:                 stats,
		Warnings:              warnings,
		ConstraintPlan:        source.ConstraintPlan,
		PushedConstraints:     source.PushedConstraints,
		ResidualConstraints:   source.ResidualConstraints,
		Plan:                  plan,
		DefinitionFingerprint: defFP,
		SourceFingerprint:     sourceFP,
		ExecutionFingerprint:  execFP,
	}, nil
}

func (s *Service) FieldCandidates(ctx context.Context, id string, req FieldCandidatesRequest) (FieldCandidatesResponse, error) {
	entry, err := s.entry(ctx, id)
	if err != nil {
		return FieldCandidatesResponse{}, err
	}
	if hasBlockingIssues(entry.Issues) {
		return FieldCandidatesResponse{}, fmt.Errorf("%w: %s", ErrInvalidView, id)
	}
	field := strings.TrimSpace(req.Field)
	if field == "" {
		return FieldCandidatesResponse{}, fmt.Errorf("%w: missing field", ErrInvalidRequest)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	resolver, ok := s.opts.SourceResolver.(interface {
		FieldCandidates(context.Context, viewconfig.ViewDefinition, FieldCandidatesRequest) (FieldCandidatesResponse, error)
	})
	if !ok {
		return FieldCandidatesResponse{Field: field}, nil
	}
	req.Field = field
	req.Query = strings.TrimSpace(req.Query)
	req.Limit = limit
	return resolver.FieldCandidates(ctx, entry.Definition, req)
}

func executionWarnings(def viewconfig.ViewDefinition, req ExecuteRequest) []Warning {
	preset := strings.TrimSpace(req.FilterPreset)
	if preset == "" || req.Filters != nil || filterPresetExists(def, preset) {
		return nil
	}
	return []Warning{{
		Code:    "unknown_filter_preset",
		Message: fmt.Sprintf("filter preset %q was not found", preset),
		Path:    "filterPreset",
	}}
}

func filterPresetExists(def viewconfig.ViewDefinition, id string) bool {
	for _, preset := range def.FilterPresets {
		if strings.TrimSpace(preset.ID) == id {
			return true
		}
	}
	return false
}

func executeRequestFromState(state ExecutionState, req ExecuteRequest) ExecuteRequest {
	return ExecuteRequest{
		Variant:      state.Variant,
		FilterPreset: state.FilterPreset,
		Search:       state.Search,
		Filters:      append([]viewconfig.FilterSpec(nil), state.Filters...),
		Sort:         append([]viewconfig.SortSpec(nil), state.Sort...),
		Group:        state.Group,
		Page:         state.Page,
		Source:       req.Source,
		PageSet:      true,
		Inputs:       cloneStringMap(state.Inputs),
		ColumnField:  state.ColumnField,
		LaneField:    state.LaneField,
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// bundledDefinitions validates the bundled views on their own filesystem and
// drops every one whose id a repository definition already uses: a repository
// view replaces the bundled view it shares an id with, issues included.
func (s *Service) bundledDefinitions(repository []viewconfig.ViewDefinition, opts viewconfig.ValidateOptions) ([]viewconfig.ViewDefinition, []viewconfig.Issue) {
	defs, issues := viewconfig.LoadFS(s.opts.Bundled)
	if len(defs) == 0 && len(issues) == 0 {
		return nil, nil
	}
	opts.EntryFS = s.opts.Bundled
	validation := viewconfig.Validate(defs, opts)
	issues = append(issues, validation.Issues...)
	shadowed := map[string]bool{}
	for _, def := range repository {
		shadowed[def.ID] = true
	}
	var out []viewconfig.ViewDefinition
	for _, def := range validation.Views {
		if shadowed[def.ID] {
			continue
		}
		def.Origin = viewconfig.OriginBundled
		out = append(out, def)
	}
	var kept []viewconfig.Issue
	for _, issue := range issues {
		if !shadowed[issue.View] {
			kept = append(kept, issue)
		}
	}
	return out, kept
}

func (s *Service) loadDefinitions() ([]viewconfig.ViewDefinition, []viewconfig.Issue) {
	if s.opts.Views != nil {
		return append([]viewconfig.ViewDefinition(nil), s.opts.Views...), append([]viewconfig.Issue(nil), s.opts.LoadIssues...)
	}
	return viewconfig.LoadDefaultSource(s.opts.VaultPath)
}

func (s *Service) validateOptions() viewconfig.ValidateOptions {
	opts := viewconfig.SchemaValidateOptions(s.opts.Schema)
	if s.opts.VaultPath != "" {
		recipes, _ := queryrecipe.LoadDefaultSources(s.opts.VaultPath)
		opts.QueryRecipeIDs = map[string]struct{}{}
		opts.QueryRecipeRowPaths = map[string]string{}
		for _, recipe := range recipes {
			opts.QueryRecipeIDs[recipe.ID] = struct{}{}
			opts.QueryRecipeRowPaths[recipe.ID] = recipe.OutputContract.RowPath
		}
	}
	return opts
}

// BlockingIssue returns the first issue that keeps the entry out of navigation
// and execution, or nil when the entry is usable.
func (e CatalogEntry) BlockingIssue() *viewconfig.Issue {
	for i := range e.Issues {
		if hasBlockingIssues(e.Issues[i : i+1]) {
			return &e.Issues[i]
		}
	}
	return nil
}

func hasBlockingIssues(issues []viewconfig.Issue) bool {
	for _, issue := range issues {
		if issue.Code == "" {
			continue
		}
		switch issue.Severity {
		case "", viewconfig.IssueFatal:
			return true
		case viewconfig.IssueWarning, viewconfig.IssueInfo:
			continue
		default:
			return true
		}
	}
	return false
}
