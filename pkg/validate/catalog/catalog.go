// Package catalog contains the validation check identities shared by the
// validation runner and read-only query surfaces.
package catalog

import (
	"slices"
	"strings"
)

// Check IDs are persisted public identities.
const (
	CheckBrokenLinks      = "broken_links"
	CheckLinkHygiene      = "link_hygiene"
	CheckOntology         = "ontology"
	CheckCodeFrontmatter  = "code_frontmatter"
	CheckCodeAnchors      = "code_anchors"
	CheckIdentifiers      = "identifiers"
	CheckQueryRecipes     = "query_recipes"
	CheckViews            = "views"
	CheckSkillOverlays    = "skill_overlays"
	CheckTasks            = "tasks"
	CheckCompanionDocs    = "companion_docs"
	CheckFrozenScopeDrift = "frozen_scope_drift"
	CheckFragileExternal  = "fragile_external_link"
	CheckOrphanBlockIDs   = "orphan_block_ids"
	CheckPlaceholderLinks = "placeholder_links"
)

// Check identifies a validation check and the names accepted by selectors.
type Check struct {
	ID      string
	Name    string
	Aliases []string
}

var checks = []Check{
	{ID: CheckOntology, Name: "ontology", Aliases: []string{"ontology_schema"}},
	{ID: CheckIdentifiers, Name: "identifiers", Aliases: []string{"identifier", "aliases", "alias"}},
	{ID: CheckBrokenLinks, Name: "broken-links", Aliases: []string{"brokenlink", "brokenlinks", "dead_links", "deadlinks"}},
	{ID: CheckLinkHygiene, Name: "link-hygiene", Aliases: []string{"linkhygiene", "links", "link_style", "linkstyle"}},
	{ID: CheckQueryRecipes, Name: "query-recipes", Aliases: []string{"query_recipe", "recipes"}},
	{ID: CheckViews, Name: "views", Aliases: []string{"view", "configured_views", "view_config"}},
	{ID: CheckSkillOverlays, Name: "skill-overlays", Aliases: []string{"skill_overlay", "skilloverlays", "skilloverlay", "overlays"}},
	{ID: CheckTasks, Name: "tasks", Aliases: []string{"task", "scheduled_tasks", "task_files"}},
	{ID: CheckCodeFrontmatter, Name: "code-frontmatter", Aliases: []string{"frontmatter", "code_validate"}},
	{ID: CheckCodeAnchors, Name: "code-anchors", Aliases: []string{"anchors", "anchor_matches"}},
	{ID: CheckCompanionDocs, Name: "companion-docs", Aliases: []string{"companiondocs", "companion_doc", "companiondoc"}},
	{ID: CheckFrozenScopeDrift, Name: "frozen-scope-drift", Aliases: []string{"frozenscopedrift", "scope_drift", "spec_freeze_drift"}},
	{ID: CheckFragileExternal, Name: "fragile-external", Aliases: []string{"fragile_external", "fragileexternal", "fragile_heading_links"}},
	{ID: CheckOrphanBlockIDs, Name: "orphan-block-ids", Aliases: []string{"orphan_block_id", "orphanblockids", "orphan_blocks"}},
	{ID: CheckPlaceholderLinks, Name: "placeholder-links", Aliases: []string{"placeholder_link", "placeholderlinks", "placeholders", "unresolved_links"}},
}

// Checks returns an independent copy of the shared check identities.
func Checks() []Check {
	out := make([]Check, len(checks))
	for i, check := range checks {
		check.Aliases = slices.Clone(check.Aliases)
		out[i] = check
	}
	return out
}

// Lookup returns the identity for a canonical check ID.
func Lookup(id string) (Check, bool) {
	canonical, ok := Canonical(id)
	if !ok {
		return Check{}, false
	}
	for _, check := range checks {
		if check.ID == canonical {
			check.Aliases = slices.Clone(check.Aliases)
			return check, true
		}
	}
	return Check{}, false
}

// Canonical resolves an ID, public name, or alias to the persisted check ID.
func Canonical(raw string) (string, bool) {
	name := Normalize(raw)
	for _, check := range checks {
		if name == Normalize(check.ID) || name == Normalize(check.Name) {
			return check.ID, true
		}
		for _, alias := range check.Aliases {
			if name == Normalize(alias) {
				return check.ID, true
			}
		}
	}
	return "", false
}

// Name returns the public name for a canonical check ID.
func Name(id string) (string, bool) {
	canonical, ok := Canonical(id)
	if !ok {
		return "", false
	}
	for _, check := range checks {
		if check.ID == canonical {
			return check.Name, true
		}
	}
	return "", false
}

// Normalize folds the spelling accepted by public validation selectors.
func Normalize(raw string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "-", "_")
}
