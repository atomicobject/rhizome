package validate

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/skilloverlay"
	validationcatalog "github.com/atomicobject/rhizome/pkg/validate/catalog"
	"github.com/stretchr/testify/require"
)

func TestCheckRegistryMatchesSharedIdentityCatalog(t *testing.T) {
	require.Len(t, validationcatalog.Checks(), len(checkRegistry))
}

func TestCheckRegistryHasStableCoreAndSuiteOrder(t *testing.T) {
	core := []string{CheckOntology, CheckIdentifiers, CheckBrokenLinks}
	all := []string{
		CheckOntology,
		CheckIdentifiers,
		CheckBrokenLinks,
		CheckLinkHygiene,
		CheckQueryRecipes,
		CheckViews,
		CheckSkillOverlays,
		CheckTasks,
		CheckCodeFrontmatter,
		CheckCodeAnchors,
		CheckCompanionDocs,
	}
	audit := []string{CheckFrozenScopeDrift, CheckFragileExternal, CheckOrphanBlockIDs, CheckPlaceholderLinks}

	require.Equal(t, core, DefaultChecks)
	require.Equal(t, core, builtInSuiteChecks(SuiteDefault))
	require.Equal(t, all, builtInSuiteChecks(SuiteAll))
	require.Equal(t, audit, builtInSuiteChecks(SuiteAudit))
	for _, maintenanceCheck := range audit {
		require.NotContains(t, all, maintenanceCheck)
	}
}

func TestSkillOverlayCheckPreservesDeterministicIssuesForCentralizedTruncation(t *testing.T) {
	manifest := skilloverlay.Manifest{Issues: []skilloverlay.Issue{
		{Code: "z", Path: "b", Message: "third"},
		{Code: "b", Path: "a", Message: "second"},
		{Code: "a", Path: "a", Message: "first"},
	}}

	result := skillOverlayCheckResult(manifest)

	require.Equal(t, 3, result.IssueCount)
	require.Len(t, result.Issues, 3)
	require.Equal(t, "a", result.Issues[0].Code)
	require.Equal(t, "b", result.Issues[1].Code)
	finalized := finalizeCheckResult(CheckSkillOverlays, result, 0, 2)
	require.Len(t, finalized.Issues, 2)
	require.Len(t, finalized.allIssueKeys, 3)
}

func TestCheckRegistryRejectsAmbiguousSelectorsAndSuiteMembership(t *testing.T) {
	base := registeredCheck{CheckDescriptor: CheckDescriptor{Name: "one", CLIName: "one", Suites: []CheckSuite{SuiteAll}}}
	second := base
	second.Name, second.CLIName = "two", "two"
	second.Aliases = []string{"one"}
	overlap := base
	overlap.Suites = []CheckSuite{SuiteAll, SuiteAudit}
	missingAll := base
	missingAll.Suites = []CheckSuite{SuiteDefault}
	reserved := base
	reserved.Aliases = []string{"default"}
	for _, test := range []struct {
		name     string
		registry []registeredCheck
		want     string
	}{
		{"duplicate id", []registeredCheck{base, base}, "duplicate validation check"},
		{"ambiguous alias", []registeredCheck{base, second}, "belongs to both"},
		{"all and audit", []registeredCheck{overlap}, "both all and audit"},
		{"default outside all", []registeredCheck{missingAll}, "must belong to all"},
		{"reserved selector", []registeredCheck{reserved}, "reserved validation selector"},
	} {
		t.Run(test.name, func(t *testing.T) { require.ErrorContains(t, validateCheckRegistry(test.registry), test.want) })
	}
}

func TestPublicSelectorsAreStable(t *testing.T) {
	for _, test := range []struct {
		id    string
		names []string
	}{
		{"ontology", []string{"ontology", "ontology_schema"}},
		{"identifiers", []string{"identifiers", "identifier", "aliases", "alias"}},
		{"broken_links", []string{"broken_links", "broken-links", "brokenlink", "brokenlinks", "dead_links", "deadlinks"}},
		{"link_hygiene", []string{"link_hygiene", "link-hygiene", "linkhygiene", "links", "link_style", "linkstyle"}},
		{"query_recipes", []string{"query_recipes", "query-recipes", "query_recipe", "recipes"}},
		{"views", []string{"views", "view", "configured_views", "view_config"}},
		{"skill_overlays", []string{"skill_overlays", "skill-overlays", "skill_overlay", "skilloverlays", "skilloverlay", "overlays"}},
		{"tasks", []string{"tasks", "task", "scheduled_tasks", "scheduled-tasks", "task_files"}},
		{"code_frontmatter", []string{"code_frontmatter", "code-frontmatter", "frontmatter", "code_validate"}},
		{"code_anchors", []string{"code_anchors", "code-anchors", "anchors", "anchor_matches"}},
		{"companion_docs", []string{"companion_docs", "companion-docs", "companiondocs", "companion_doc", "companiondoc"}},
		{"frozen_scope_drift", []string{"frozen_scope_drift", "frozen-scope-drift", "frozenscopedrift", "scope_drift", "spec_freeze_drift"}},
		{"fragile_external_link", []string{"fragile_external_link", "fragile-external", "fragile_external", "fragileexternal", "fragile_heading_links"}},
		{"orphan_block_ids", []string{"orphan_block_ids", "orphan-block-ids", "orphan_block_id", "orphanblockids", "orphan_blocks"}},
		{"placeholder_links", []string{"placeholder_links", "placeholder-links", "placeholder_link", "placeholderlinks", "placeholders", "unresolved_links"}},
	} {
		for _, selector := range test.names {
			t.Run(selector, func(t *testing.T) {
				canonical, ok := CanonicalCheck(selector)
				require.True(t, ok)
				require.Equal(t, test.id, canonical)
			})
		}
	}
	for _, selector := range []string{"default", "all", "audit"} {
		_, check := CanonicalCheck(selector)
		require.False(t, check)
		selection, err := ResolveSelection([]string{selector}, SuiteConfig{})
		require.NoError(t, err)
		require.Equal(t, selector, selection.Selector)
	}
}
