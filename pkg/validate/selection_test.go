package validate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveSelectionSelectors(t *testing.T) {
	config := SuiteConfig{
		Default: SuiteOverlay{Add: []string{"views"}, Skip: []string{"broken-links"}},
		All:     SuiteOverlay{Skip: []string{"code-anchors"}},
	}

	tests := []struct {
		name       string
		selectors  []string
		wantLabel  string
		wantChecks []string
		explicit   bool
	}{
		{name: "omitted", wantLabel: string(SuiteDefault), wantChecks: []string{CheckOntology, CheckIdentifiers, CheckViews}},
		{name: "default", selectors: []string{"default"}, wantLabel: string(SuiteDefault), wantChecks: []string{CheckOntology, CheckIdentifiers, CheckViews}},
		{name: "all", selectors: []string{"all"}, wantLabel: string(SuiteAll), wantChecks: withoutCheck(builtInSuiteChecks(SuiteAll), CheckCodeAnchors)},
		{name: "audit fixed", selectors: []string{"audit"}, wantLabel: string(SuiteAudit), wantChecks: []string{CheckFrozenScopeDrift, CheckFragileExternal, CheckOrphanBlockIDs, CheckPlaceholderLinks}},
		{name: "named canonical", selectors: []string{"broken-links"}, wantLabel: "broken-links", wantChecks: []string{CheckBrokenLinks}, explicit: true},
		{name: "named legacy alias", selectors: []string{"aliases"}, wantLabel: "identifiers", wantChecks: []string{CheckIdentifiers}, explicit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selection, err := ResolveSelection(tt.selectors, config)
			require.NoError(t, err)
			require.Equal(t, tt.wantLabel, selection.Selector)
			require.Equal(t, tt.wantChecks, selection.Checks)
			require.Equal(t, tt.explicit, selection.ExplicitCheck)
		})
	}
}

func TestResolveSelectionRequiresExactlyOneSelector(t *testing.T) {
	for _, selectors := range [][]string{
		{"default", "ontology"},
		{"all", "audit"},
		{"ontology", "broken-links"},
	} {
		_, err := ResolveSelection(selectors, SuiteConfig{})
		require.ErrorContains(t, err, "exactly one validation selector")
	}
}

func TestResolveSelectionRejectsUnknownSelector(t *testing.T) {
	for _, selector := range []string{"mystery", "[]"} {
		t.Run(selector, func(t *testing.T) {
			_, err := ResolveSelection([]string{selector}, SuiteConfig{})
			require.ErrorContains(t, err, `unknown validation selector "`+selector+`"`)
		})
	}
}

func TestExplicitNamedSelectionBypassesConfiguredComposition(t *testing.T) {
	invalid := SuiteConfig{
		Default: SuiteOverlay{Add: []string{"mystery"}, Skip: []string{"mystery"}},
		All:     SuiteOverlay{Add: []string{"also-mystery"}},
	}

	selection, err := ResolveSelection([]string{"ontology"}, invalid)
	require.NoError(t, err)
	require.Equal(t, []string{CheckOntology}, selection.Checks)
}

func TestResolveSelectionValidatesBothConfiguredOverlaysForNonExplicitSelectors(t *testing.T) {
	tests := []struct {
		name      string
		selectors []string
		config    SuiteConfig
		want      string
	}{
		{name: "omitted validates all sibling", config: SuiteConfig{All: SuiteOverlay{Add: []string{"mystery"}}}, want: `unknown check "mystery"`},
		{name: "default validates all sibling", selectors: []string{"default"}, config: SuiteConfig{All: SuiteOverlay{Add: []string{"mystery"}}}, want: `unknown check "mystery"`},
		{name: "all validates default sibling", selectors: []string{"all"}, config: SuiteConfig{Default: SuiteOverlay{Add: []string{"mystery"}}}, want: `unknown check "mystery"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveSelection(tt.selectors, tt.config)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestAuditSelectionBypassesConfigurableOverlays(t *testing.T) {
	invalid := SuiteConfig{
		Default: SuiteOverlay{Add: []string{"mystery"}},
		All:     SuiteOverlay{Skip: []string{"orphan-block-ids"}},
	}

	selection, err := ResolveSelection([]string{"audit"}, invalid)
	require.NoError(t, err)
	require.Equal(t, string(SuiteAudit), selection.Selector)
	require.Equal(t, builtInSuiteChecks(SuiteAudit), selection.Checks)
}

func TestValidateSuiteConfigChecksBothOverlays(t *testing.T) {
	config := SuiteConfig{All: SuiteOverlay{Skip: []string{"orphan-block-ids"}}}

	err := ValidateSuiteConfig(config)
	require.ErrorContains(t, err, `validation.all cannot skip "orphan-block-ids"`)
}

func TestComposeSuiteStrictValidation(t *testing.T) {
	tests := []struct {
		name    string
		suite   CheckSuite
		overlay SuiteOverlay
		want    string
	}{
		{name: "unknown add", suite: SuiteDefault, overlay: SuiteOverlay{Add: []string{"mystery"}}, want: `unknown check "mystery"`},
		{name: "unknown skip", suite: SuiteDefault, overlay: SuiteOverlay{Skip: []string{"mystery"}}, want: `unknown check "mystery"`},
		{name: "canonical overlap", suite: SuiteDefault, overlay: SuiteOverlay{Add: []string{"aliases"}, Skip: []string{"identifiers"}}, want: `both add and skip "identifiers"`},
		{name: "nonmember skip", suite: SuiteDefault, overlay: SuiteOverlay{Skip: []string{"views"}}, want: `cannot skip "views"`},
		{name: "empty effective set", suite: SuiteDefault, overlay: SuiteOverlay{Skip: []string{"ontology", "identifiers", "broken-links"}}, want: "effective default suite is empty"},
		{name: "audit noncomposable", suite: SuiteAudit, overlay: SuiteOverlay{Add: []string{"views"}}, want: "audit suite is fixed and cannot be configured"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ComposeSuite(tt.suite, tt.overlay)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestComposeSuiteUsesRegistryOrder(t *testing.T) {
	checks, err := ComposeSuite(SuiteDefault, SuiteOverlay{Add: []string{"companion-docs", "views", "link-hygiene"}})
	require.NoError(t, err)

	want := orderedSubset([]string{CheckOntology, CheckIdentifiers, CheckBrokenLinks, CheckLinkHygiene, CheckViews, CheckCompanionDocs})
	require.Equal(t, want, checks)
}

func withoutCheck(checks []string, excluded string) []string {
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		if check != excluded {
			out = append(out, check)
		}
	}
	return out
}

func orderedSubset(names []string) []string {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	out := make([]string, 0, len(names))
	for _, descriptor := range CheckDescriptors() {
		if _, ok := wanted[descriptor.Name]; ok {
			out = append(out, descriptor.Name)
		}
	}
	return out
}
