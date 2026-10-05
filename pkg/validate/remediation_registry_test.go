package validate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemediationRegistryHasExactClassificationForAuditedIssueCodes(t *testing.T) {
	t.Parallel()

	expected := map[string][]string{
		CheckBrokenLinks: {
			IssueCodeBrokenNoteLink, IssueCodeBrokenHeadingLink, IssueCodeBrokenBlockLink,
		},
		CheckOntology: {
			"schema_invalid", "schema_state_decode_error", "unknown_declared_type", "declared_type_mismatch", IssueCodeParentCycle,
			"type_ambiguous", "missing_required_field", "field_shape_mismatch", "field_type_mismatch",
			"missing_required_section", "wrong_section_level", "duplicate_section", "empty_required_section",
			"link_target_missing", "wrong_target_type", "inverse_mismatch", "contains_min_not_met",
			"contains_max_exceeded", "field_authoring_style_mismatch", "title_pattern_mismatch",
			"title_forbidden_pattern", "field_format_mismatch", "field_forbidden_pattern",
			"conditional_required_field_missing", "conditional_required_value_mismatch",
			"identifier_block_id_migration", "malformed_block_id",
			"duplicate_block_id", "stray_block_id_on_non_embedded_section",
		},
		CheckIdentifiers: {
			"identifier_not_in_aliases", "duplicate_preferred_identifier",
			"identifier_preferred_alias_collision", "duplicate_identifier_alias", "missing_required_field",
			issueCodeIdentifierDateTimeFormatMismatch, issueCodeIdentifierSequentialFormatMismatch,
			issueCodeIdentifierStrategyMigrationRequired, issueCodeIdentifierStrategyMigrationBlocked,
		},
		CheckCodeFrontmatter: {"code_frontmatter_yaml_error", "code_anchor_definition_error"},
		CheckCodeAnchors:     {"no_match", "suffix_match"},
		CheckLinkHygiene: {
			"wikilink_target_empty", "wikilink_target_absolute_path", "wikilink_target_leading_slash",
			"wikilink_target_has_md_extension", "wikilink_target_is_alias", "markdown_internal_link_absolute_path",
			"markdown_internal_link_outside_vault", "markdown_internal_link_not_relative", "pseudo_link_placeholder",
		},
		CheckQueryRecipes: {
			"recipe_path_error", "recipe_read_error", "unsupported_recipe_file", "unterminated_recipe_fence", "recipe_parse_error",
			"duplicate_recipe_id", "query_compile_error", "missing_required_field", "unsupported_api_version",
			"invalid_input_mode", "missing_input_name", "duplicate_input_name", "undeclared_placeholder",
			"missing_primary_input", "unknown_primary_input", "missing_output_path", "missing_required_input",
			"undeclared_input", "deprecated_placeholder", "invalid_input_value",
		},
		CheckViews: {
			"view_path_error", "view_read_error", "unsupported_view_file", "view_yaml_decode_error",
			"multiple_documents_unsupported", "missing_required_field", "unsupported_api_version", "invalid_view_id", "invalid_view_configuration",
			"duplicate_view_id", "duplicate_mount_default", "duplicate_generated_view_id", "unknown_ontology_type", "unknown_ontology_interface",
			"unknown_query_recipe", "missing_recipe_row_path", "unexpected_field", "unsupported_source_kind",
			"invalid_mount_target", "unsupported_mount_kind", "unsupported_variant", "missing_table_variant",
			"duplicate_filter_preset", "unsupported_sort_direction", "duplicate_group_value", "invalid_page",
			"invalid_source_cap", "unsupported_filter_operator", "invalid_card_variant", "invalid_kanban_variant",
			"missing_kanban_column_field", "invalid_custom_entry", "custom_entry_not_found", "custom_view_script_error",
			"custom_view_read_error",
		},
		CheckSkillOverlays: {
			"template_resolution_error", "render_error", "overlay_yaml_decode_error", "duplicate_overlay_id",
			"duplicate_slot_id", "invalid_operation", "invalid_slot_markup", "invalid_slot_mode", "leftover_marker",
			"missing_required_field", "nested_slot", "unknown_slot", "unknown_target_skill", "unsupported_api_version",
			"unterminated_slot", "conflicting_replace", "conflicting_replace_suppress", "conflicting_suppress",
		},
		CheckCompanionDocs: {
			"companion_doc_outside_vault", "companion_doc_invalid_path", "companion_doc_missing",
			"companion_doc_stat_failed", "companion_doc_directory",
		},
		CheckFrozenScopeDrift: {"frozen_scope_drift"},
		CheckFragileExternal:  {CheckFragileExternal},
		CheckOrphanBlockIDs:   {"orphan_block_id"},
	}

	for check, codes := range expected {
		for _, code := range codes {
			registration, ok := lookupIssueRemediation(check, code)
			require.Truef(t, ok, "missing remediation registration for %s/%s", check, code)
			require.Equal(t, check, registration.Check)
			require.Equal(t, code, registration.IssueCode)
			require.NotEmpty(t, registration.PostChecks)
			require.Truef(t,
				registration.Builder != "" || registration.AgentAction != "" || registration.NonFixableReason != "",
				"%s/%s has no executable builder, agent action, or non-fixable reason", check, code,
			)
		}
	}
}

func TestRemediationRegistryEntriesHaveValidCanonicalContracts(t *testing.T) {
	t.Parallel()

	for key, registration := range issueRemediationRegistry {
		canonical, ok := CanonicalCheck(registration.Check)
		require.Truef(t, ok, "registry entry %q has unknown check", key)
		require.Equal(t, registration.Check, canonical)
		require.NotEmpty(t, registration.IssueCode)
		require.Equal(t, []string{registration.Check}, registration.PostChecks)
		if registration.NonFixableReason != "" {
			require.Empty(t, registration.Builder)
			require.Empty(t, registration.AgentAction)
			continue
		}
		require.Contains(t, []FixSafety{FixSafetySafe, FixSafetyConfirm, FixSafetyAgent}, registration.Safety)
		require.NotEmpty(t, registration.AgentAction)
		require.Contains(t, registration.Prerequisites, "stable_issue_key")
	}
}

func TestEnrichRemediationActionsJoinsExistingActionsByExactIssueKey(t *testing.T) {
	t.Parallel()

	first := Issue{Code: IssueCodeBrokenNoteLink, Path: "one.md", Target: "First"}
	second := Issue{Code: IssueCodeBrokenNoteLink, Path: "two.md", Target: "Second"}
	firstKey, err := StableIssueKey(CheckBrokenLinks, first)
	require.NoError(t, err)
	secondKey, err := StableIssueKey(CheckBrokenLinks, second)
	require.NoError(t, err)
	first.Key = firstKey
	second.Key = secondKey

	checks := []CheckResult{{
		Name:       CheckBrokenLinks,
		IssueCount: 2,
		Issues:     []Issue{first, second},
		fullIssues: []Issue{first, second},
		Fixes: []FixAction{{
			ID: "fix-first", Check: CheckBrokenLinks, IssueCode: IssueCodeBrokenNoteLink,
			Safety: FixSafetySafe, IssueKeys: []string{firstKey}, AffectedPaths: []string{"one.md"},
		}},
	}}

	enriched := enrichRemediationActions(checks)
	require.Len(t, enriched, 1)
	require.Len(t, enriched[0].Fixes, 2)
	require.Equal(t, []string{firstKey}, enriched[0].Fixes[0].IssueKeys)
	require.Equal(t, []string{secondKey}, enriched[0].Fixes[1].IssueKeys)
	require.Equal(t, FixSafetyAgent, enriched[0].Fixes[1].Safety)
	require.Equal(t, "review_broken_note_link", enriched[0].Fixes[1].Kind)
}

func TestEnrichRemediationActionsLeavesExplicitlyNonFixableIssueOutOfPlan(t *testing.T) {
	t.Parallel()

	issue := Issue{Code: "view_read_error", Path: ".rhizome/views/example.yaml"}
	key, err := StableIssueKey(CheckViews, issue)
	require.NoError(t, err)
	issue.Key = key

	enriched := enrichRemediationActions([]CheckResult{{
		Name: CheckViews, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue},
	}})
	require.Empty(t, enriched[0].Fixes)
}

func TestEnrichRemediationActionsProducesIssueKeyBoundRepairPlanFallback(t *testing.T) {
	t.Parallel()

	issue := Issue{Code: "field_type_mismatch", Path: "note.md", Field: "status"}
	key, err := StableIssueKey(CheckOntology, issue)
	require.NoError(t, err)
	issue.Key = key
	checks := enrichRemediationActions([]CheckResult{{
		Name: CheckOntology, IssueCount: 1, Issues: []Issue{issue}, fullIssues: []Issue{issue},
	}})

	plan, err := BuildFixPlan(checks)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Equal(t, 1, plan.AgentCount)
	require.Equal(t, []string{key}, plan.IssueKeys)
	require.Equal(t, []string{key}, plan.Actions[0].IssueKeys)
}
