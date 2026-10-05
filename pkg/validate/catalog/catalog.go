// Package catalog contains the validation check identities shared by the
// validation runner and read-only query surfaces.
package catalog

import (
	"slices"
	"strings"
)

// Check identifies a validation check and the names accepted by selectors.
type Check struct {
	ID      string
	Name    string
	Aliases []string
}

var checks = []Check{
	{ID: "ontology", Name: "ontology", Aliases: []string{"ontology_schema"}},
	{ID: "identifiers", Name: "identifiers", Aliases: []string{"identifier", "aliases", "alias"}},
	{ID: "broken_links", Name: "broken-links", Aliases: []string{"brokenlink", "brokenlinks", "dead_links", "deadlinks"}},
	{ID: "link_hygiene", Name: "link-hygiene", Aliases: []string{"linkhygiene", "links", "link_style", "linkstyle"}},
	{ID: "query_recipes", Name: "query-recipes", Aliases: []string{"query_recipe", "recipes"}},
	{ID: "views", Name: "views", Aliases: []string{"view", "configured_views", "view_config"}},
	{ID: "skill_overlays", Name: "skill-overlays", Aliases: []string{"skill_overlay", "skilloverlays", "skilloverlay", "overlays"}},
	{ID: "code_frontmatter", Name: "code-frontmatter", Aliases: []string{"frontmatter", "code_validate"}},
	{ID: "code_anchors", Name: "code-anchors", Aliases: []string{"anchors", "anchor_matches"}},
	{ID: "companion_docs", Name: "companion-docs", Aliases: []string{"companiondocs", "companion_doc", "companiondoc"}},
	{ID: "frozen_scope_drift", Name: "frozen-scope-drift", Aliases: []string{"frozenscopedrift", "scope_drift", "spec_freeze_drift"}},
	{ID: "fragile_external_link", Name: "fragile-external", Aliases: []string{"fragile_external", "fragileexternal", "fragile_heading_links"}},
	{ID: "orphan_block_ids", Name: "orphan-block-ids", Aliases: []string{"orphan_block_id", "orphanblockids", "orphan_blocks"}},
	{ID: "placeholder_links", Name: "placeholder-links", Aliases: []string{"placeholder_link", "placeholderlinks", "placeholders", "unresolved_links"}},
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
	name := normalize(raw)
	for _, check := range checks {
		if name == normalize(check.ID) || name == normalize(check.Name) {
			return check.ID, true
		}
		for _, alias := range check.Aliases {
			if name == normalize(alias) {
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

func normalize(raw string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "-", "_")
}
