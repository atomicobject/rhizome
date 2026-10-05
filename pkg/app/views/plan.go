package views

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

func executionConstraints(source SourceResult, state ExecutionState) SourceConstraints {
	if source.ConstraintPlan {
		return source.ResidualConstraints
	}
	return SourceConstraints{
		Search:  state.Search,
		Filters: append([]viewconfig.FilterSpec(nil), state.Filters...),
		Sort:    append([]viewconfig.SortSpec(nil), state.Sort...),
		Page:    state.Page,
	}
}

func executionPlanSummary(source SourceResult, constraints SourceConstraints, candidateCount int) ConstraintPlanSummary {
	plan := source.Plan
	if emptySourceConstraints(plan.Pushed) {
		plan.Pushed = source.PushedConstraints
	}
	if emptySourceConstraints(plan.Residual) {
		if source.ConstraintPlan {
			plan.Residual = source.ResidualConstraints
		} else {
			plan.Residual = constraints
		}
	}
	if plan.CandidateCount == 0 && candidateCount > 0 {
		plan.CandidateCount = candidateCount
	}
	if emptySourceCapPolicy(plan.CapPolicy) {
		plan.CapPolicy = SourceCapPolicy{Limit: defaultSourceCap, Source: "default"}
	}
	if plan.CandidateLimit == 0 {
		plan.CandidateLimit = plan.CapPolicy.Limit
	}
	completeness := sourceCompleteness(source, plan)
	if plan.SourceCompleteness == "" || completeness == SourceBounded {
		plan.SourceCompleteness = completeness
	}
	if plan.Reliability == "" {
		plan.Reliability = constraintReliability(plan)
	}
	return plan
}

func emptySourceConstraints(constraints SourceConstraints) bool {
	return strings.TrimSpace(constraints.Search) == "" &&
		len(constraints.Filters) == 0 &&
		len(constraints.Sort) == 0 &&
		constraints.Page.Offset == 0 &&
		constraints.Page.First == 0
}

func emptySourceCapPolicy(policy SourceCapPolicy) bool {
	return policy.Limit == 0 && strings.TrimSpace(policy.Source) == ""
}

func sourceCompleteness(source SourceResult, plan ConstraintPlanSummary) SourceCompleteness {
	for _, warning := range source.Warnings {
		if warningIndicatesBoundedSource(warning) {
			return SourceBounded
		}
	}
	for _, warning := range plan.Warnings {
		if warningIndicatesBoundedSource(warning) {
			return SourceBounded
		}
	}
	if plan.CandidateLimit > 0 && plan.CandidateCount >= plan.CandidateLimit {
		return SourceBounded
	}
	if source.ConstraintPlan {
		return SourceComplete
	}
	return SourceUnknown
}

func warningIndicatesBoundedSource(warning Warning) bool {
	switch warning.Code {
	case "view_source_may_be_truncated",
		"view_source_residual_constraints_after_cap",
		"view_residual_constraints_cap_bound",
		"view_filter_cap_bound",
		"view_search_cap_bound",
		"view_sort_cap_bound":
		return true
	default:
		return false
	}
}

func constraintReliability(plan ConstraintPlanSummary) ConstraintReliability {
	if plan.SourceCompleteness == SourceUnknown {
		return ConstraintsNotPlanned
	}
	if plan.SourceCompleteness == SourceBounded && hasResidualConstraints(plan.Residual) {
		return ConstraintsCapBound
	}
	return ConstraintsExact
}

func executionCapBoundWarnings(plan ConstraintPlanSummary) []Warning {
	if plan.Reliability != ConstraintsCapBound {
		return nil
	}
	var warnings []Warning
	if len(plan.Residual.Filters) > 0 || len(plan.Residual.Sort) > 0 {
		warnings = append(warnings, Warning{
			Code:    "view_residual_constraints_cap_bound",
			Message: "residual table operations ran over a capped source row set and may be incomplete",
		})
	}
	if strings.TrimSpace(plan.Residual.Search) != "" {
		warnings = append(warnings, Warning{
			Code:    "view_search_cap_bound",
			Message: "residual search ran over a capped source row set and may miss matches",
			Path:    "search",
		})
	}
	if len(plan.Residual.Filters) > 0 {
		warnings = append(warnings, Warning{
			Code:    "view_filter_cap_bound",
			Message: "residual filters ran over a capped source row set and may miss matches",
			Path:    "filters",
		})
	}
	if len(plan.Residual.Sort) > 0 {
		warnings = append(warnings, Warning{
			Code:    "view_sort_cap_bound",
			Message: "residual sort ran over a capped source row set and may not reflect global order",
			Path:    "sort",
		})
	}
	return warnings
}

func appendPlanWarnings(existing []Warning, warnings []Warning) []Warning {
	if len(warnings) == 0 {
		return existing
	}
	seen := map[string]struct{}{}
	for _, warning := range existing {
		seen[warningKey(warning)] = struct{}{}
	}
	out := append([]Warning(nil), existing...)
	for _, warning := range warnings {
		key := warningKey(warning)
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, warning)
		seen[key] = struct{}{}
	}
	return out
}

func warningKey(warning Warning) string {
	return warning.Code + "\x00" + warning.Path + "\x00" + warning.Message
}
