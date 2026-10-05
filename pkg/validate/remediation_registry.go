package validate

import (
	"fmt"
	"sort"
	"strings"
)

// IssueRemediationRegistration classifies one public (check, issue-code)
// contract. Builders own executable operations. AgentAction is the precise
// fallback when current evidence cannot prove a deterministic operation.
// NonFixableReason is reserved for findings the validation repair engine must
// not claim it can execute.
type IssueRemediationRegistration struct {
	Check            string
	IssueCode        string
	Builder          string
	Safety           FixSafety
	AgentAction      string
	Prerequisites    []string
	PostChecks       []string
	NonFixableReason string
}

type remediationRegistrationGroup struct {
	check            string
	codes            []string
	builder          string
	safety           FixSafety
	agentAction      string
	prerequisites    []string
	nonFixableReason string
}

var issueRemediationRegistry = buildIssueRemediationRegistry([]remediationRegistrationGroup{
	builderRemediation(CheckBrokenLinks, "buildBrokenLinkFixesFromGroups", FixSafetyAgent,
		"Restore the missing target or choose an evidence-backed retarget; preserve the authored fragment and rerun the same scoped repair command.",
		IssueCodeBrokenNoteLink, IssueCodeBrokenHeadingLink, IssueCodeBrokenBlockLink),
	builderRemediation(CheckIdentifiers, "RunIdentifierReconciliation", FixSafetyConfirm,
		"Review the canonical claimant evidence and apply the scoped identifier reconciliation plan.",
		"identifier_not_in_aliases", "duplicate_preferred_identifier", "identifier_preferred_alias_collision", "duplicate_identifier_alias",
		issueCodeIdentifierStrategyMigrationRequired),
	builderRemediation(CheckIdentifiers, "runAliasesWithRuntime", FixSafetyAgent,
		"Set the exact missing identifier field from one unreserved alias when proven; otherwise review the path-derived confirmation candidate.",
		"missing_required_field"),
	agentRemediation(CheckIdentifiers,
		"Choose a canonical datetime identifier, update its required alias mirror and structurally resolved references, then rerun the same scoped identifiers repair command.",
		issueCodeIdentifierDateTimeFormatMismatch),
	agentRemediation(CheckIdentifiers,
		"Resolve the malformed, mixed, or conflicting identifier pool as reported, then rerun the same scoped identifiers repair command.",
		issueCodeIdentifierSequentialFormatMismatch, issueCodeIdentifierStrategyMigrationBlocked),
	builderRemediation(CheckOntology, "enrichOntologyRepairActions", FixSafetyAgent,
		"Inspect the typed field or section evidence and apply only the operation produced by the scoped ontology repair planner.",
		"declared_type_mismatch", "inverse_mismatch", "missing_required_field", "missing_required_section", "identifier_block_id_migration"),
	agentRemediation(CheckOntology,
		"Resolve the reported schema/type/field constraint using the issue path, field, target, and governing ontology guidance, then rerun the same scoped repair command.",
		"schema_invalid", "schema_state_decode_error", "unknown_declared_type", "type_ambiguous",
		"field_shape_mismatch", "field_type_mismatch", "wrong_section_level", "duplicate_section", "empty_required_section",
		"link_target_missing", "wrong_target_type", "contains_min_not_met", "contains_max_exceeded",
		"field_authoring_style_mismatch", "title_pattern_mismatch", "title_forbidden_pattern",
		"field_format_mismatch", "field_forbidden_pattern", "conditional_required_field_missing",
		"conditional_required_value_mismatch", "malformed_block_id",
		"duplicate_block_id", "stray_block_id_on_non_embedded_section"),

	agentRemediation(CheckOntology,
		"Choose which record in the reported cycle should move, then change or clear its parent link so the chain reaches a root.",
		IssueCodeParentCycle),

	builderRemediation(CheckLinkHygiene, "buildLinkHygieneFixes", FixSafetyAgent,
		"Normalize the exact internal-link token only when the current target resolves uniquely; otherwise choose the intended vault-relative target.",
		"wikilink_target_empty", "wikilink_target_absolute_path", "wikilink_target_leading_slash",
		"wikilink_target_has_md_extension", "wikilink_target_is_alias", "markdown_internal_link_absolute_path",
		"markdown_internal_link_outside_vault", "markdown_internal_link_not_relative", "pseudo_link_placeholder"),
	builderRemediation(CheckQueryRecipes, "buildQueryRecipeFixes", FixSafetyAgent,
		"Adapt the exact saved recipe against its problem statement, input contract, output contract, and current executable schema.",
		"recipe_path_error", "recipe_read_error", "unsupported_recipe_file", "unterminated_recipe_fence", "recipe_parse_error",
		"duplicate_recipe_id", "query_compile_error", "missing_required_field", "unsupported_api_version",
		"invalid_input_mode", "missing_input_name", "duplicate_input_name", "undeclared_placeholder",
		"missing_primary_input", "unknown_primary_input", "missing_output_path", "missing_required_input",
		"undeclared_input", "deprecated_placeholder", "invalid_input_value"),
	agentRemediation(CheckViews,
		"Edit the exact configured view path and field named by the finding, then rerun the same selected validation scope.",
		"unsupported_view_file", "view_yaml_decode_error", "multiple_documents_unsupported", "missing_required_field",
		"unsupported_api_version", "invalid_view_id", "invalid_view_configuration", "duplicate_view_id", "duplicate_mount_default", "duplicate_generated_view_id",
		"unknown_ontology_type", "unknown_ontology_interface", "unknown_query_recipe", "missing_recipe_row_path",
		"unexpected_field", "unsupported_source_kind", "invalid_mount_target", "unsupported_mount_kind",
		"unsupported_variant", "missing_table_variant", "duplicate_filter_preset", "unsupported_sort_direction",
		"duplicate_group_value", "invalid_page", "invalid_source_cap", "unsupported_filter_operator",
		"invalid_card_variant", "invalid_kanban_variant", "missing_kanban_column_field"),
	agentRemediation(CheckViews,
		"Fix the custom view file the finding names (the entry path in the definition, or the script at the reported line and column), then rerun the same selected validation scope.",
		"invalid_custom_entry", "custom_entry_not_found", "custom_view_script_error",
		"custom_view_import_unresolved", "custom_view_import_unmapped", "custom_view_graphql_invalid"),
	nonFixableRemediation(CheckViews,
		"The configured view source could not be enumerated or read, so validation has no trustworthy source bytes from which to build a transactional edit.",
		"view_path_error", "view_read_error", "custom_view_read_error"),
	nonFixableRemediation(CheckSkillOverlays,
		"Bundled skill overlays are compiled assets; repair must change the owning init template source and rebuild the bundle rather than mutate validation output.",
		"template_resolution_error", "render_error", "overlay_yaml_decode_error", "duplicate_overlay_id", "duplicate_slot_id",
		"invalid_operation", "invalid_slot_markup", "invalid_slot_mode", "leftover_marker", "missing_required_field",
		"nested_slot", "unknown_slot", "unknown_target_skill", "unsupported_api_version", "unterminated_slot",
		"conflicting_replace", "conflicting_replace_suppress", "conflicting_suppress"),
	agentRemediation(CheckCodeFrontmatter,
		"Repair the exact code-note frontmatter path using the structured parser guidance, then rerun the same selected validation scope.",
		"code_frontmatter_yaml_error", "code_anchor_definition_error"),
	agentRemediation(CheckCodeAnchors,
		"Resolve the exact symbol anchor against current code-index evidence; a unique suffix candidate may be confirmed, while absent or ambiguous candidates require an explicit symbol choice.",
		"no_match", "suffix_match"),
	agentRemediation(CheckCompanionDocs,
		"Correct the exact @companionDocs declaration to a readable vault-relative file, or create the intended companion document, then rerun the same scope.",
		"companion_doc_outside_vault", "companion_doc_invalid_path", "companion_doc_missing", "companion_doc_directory"),
	nonFixableRemediation(CheckCompanionDocs,
		"The companion document path could not be inspected because the filesystem stat failed; validation cannot prove a safe source edit until that access error is resolved.",
		"companion_doc_stat_failed"),
	nonFixableRemediation(CheckFrozenScopeDrift,
		"Frozen-scope drift requires an explicit lifecycle decision: acknowledge the exact effort/spec pair, refreeze it, or create follow-on work; validation cannot choose that history policy.",
		"frozen_scope_drift"),
	builderRemediation(CheckFragileExternal, "RunFragileExternal", FixSafetyConfirm,
		"Confirm the unique embedded-node target before upgrading the exact heading link to its durable block target.",
		CheckFragileExternal),
	agentRemediation(CheckPlaceholderLinks,
		"Create the note the placeholder names, retarget it to an existing note, or keep it as a deliberate placeholder; validation cannot choose between them.",
		IssueCodePlaceholderNoteLink),
	builderRemediation(CheckOrphanBlockIDs, "RunOrphanBlockIDs", FixSafetyConfirm,
		"Remove the exact orphan block ID only after near-match evidence is absent or explicitly reviewed.",
		"orphan_block_id"),
})

func builderRemediation(check, builder string, safety FixSafety, agentAction string, codes ...string) remediationRegistrationGroup {
	return remediationRegistrationGroup{
		check: check, codes: codes, builder: builder, safety: safety, agentAction: agentAction,
		prerequisites: []string{"stable_issue_key", "current_source_preconditions"},
	}
}

func agentRemediation(check, action string, codes ...string) remediationRegistrationGroup {
	return remediationRegistrationGroup{
		check: check, codes: codes, safety: FixSafetyAgent, agentAction: action,
		prerequisites: []string{"stable_issue_key", "current_source_preconditions"},
	}
}

func nonFixableRemediation(check, reason string, codes ...string) remediationRegistrationGroup {
	return remediationRegistrationGroup{check: check, codes: codes, nonFixableReason: reason}
}

func buildIssueRemediationRegistry(groups []remediationRegistrationGroup) map[string]IssueRemediationRegistration {
	registry := make(map[string]IssueRemediationRegistration)
	for _, group := range groups {
		for _, issueCode := range group.codes {
			registration := IssueRemediationRegistration{
				Check: group.check, IssueCode: issueCode, Builder: group.builder, Safety: group.safety,
				AgentAction: group.agentAction, Prerequisites: append([]string(nil), group.prerequisites...),
				PostChecks: []string{group.check}, NonFixableReason: group.nonFixableReason,
			}
			key := remediationRegistryKey(group.check, issueCode)
			if _, duplicate := registry[key]; duplicate {
				panic(fmt.Sprintf("duplicate remediation registration for %s/%s", group.check, issueCode))
			}
			registry[key] = registration
		}
	}
	return registry
}

func remediationRegistryKey(check, issueCode string) string {
	return strings.TrimSpace(check) + "\x00" + strings.TrimSpace(issueCode)
}

func lookupIssueRemediation(check, issueCode string) (IssueRemediationRegistration, bool) {
	registration, ok := issueRemediationRegistry[remediationRegistryKey(check, issueCode)]
	return registration, ok
}

// enrichRemediationActions runs only after stable issue-key assignment. It
// joins existing actions by exact issue key and emits one agent-required,
// action-only fallback for each registered unresolved issue. It never infers
// coverage from issue code, path counts, or InstanceCount.
func enrichRemediationActions(checks []CheckResult) []CheckResult {
	out := append([]CheckResult(nil), checks...)
	for checkIndex := range out {
		check := &out[checkIndex]
		check.Fixes = append([]FixAction(nil), check.Fixes...)
		covered := make(map[string]struct{})
		for _, action := range check.Fixes {
			for _, key := range action.IssueKeys {
				covered[key] = struct{}{}
			}
		}
		for _, issue := range remediationEvidenceIssues(*check) {
			key := strings.TrimSpace(issue.Key)
			if key == "" {
				stableKey, err := StableIssueKey(check.Name, issue)
				if err != nil {
					// Production checks have already passed
					// attachStableRepairIssueKeys. A failure here is limited to manually
					// assembled legacy results, which cannot safely receive an action.
					continue
				}
				key = stableKey
			}
			if _, ok := covered[key]; ok {
				continue
			}
			registration, ok := lookupIssueRemediation(check.Name, issue.Code)
			if !ok || registration.NonFixableReason != "" {
				continue
			}
			paths := sortedUnique(nonEmptyStrings(issue.Path, issue.Source))
			check.Fixes = append(check.Fixes, FixAction{
				ID:            "remediation:" + strings.TrimPrefix(key, "issue:v1:"),
				Check:         check.Name,
				IssueCode:     issue.Code,
				Kind:          "review_" + issue.Code,
				Safety:        FixSafetyAgent,
				Title:         "Resolve " + strings.ReplaceAll(issue.Code, "_", " "),
				Summary:       registration.AgentAction,
				InstanceCount: 1,
				IssueKeys:     []string{key},
				AffectedPaths: paths,
			})
			covered[key] = struct{}{}
		}
	}
	return out
}

func remediationEvidenceIssues(check CheckResult) []Issue {
	issues := append([]Issue(nil), check.fullIssues...)
	if len(issues) == 0 {
		issues = append(issues, check.Issues...)
	}
	sort.SliceStable(issues, func(i, j int) bool {
		left := strings.TrimSpace(issues[i].Key)
		right := strings.TrimSpace(issues[j].Key)
		if left != right {
			return left < right
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		return issues[i].Target < issues[j].Target
	})
	return issues
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
