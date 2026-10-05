package validate

import (
	"fmt"
	"strings"
)

// SuiteOverlay composes a built-in suite without changing registry order.
type SuiteOverlay struct {
	Add  []string `json:"add,omitempty" yaml:"add,omitempty"`
	Skip []string `json:"skip,omitempty" yaml:"skip,omitempty"`
}

// SuiteConfig contains the only configurable suite overlays. Audit membership
// is fixed this increment.
type SuiteConfig struct {
	Default SuiteOverlay `json:"default,omitempty" yaml:"default,omitempty"`
	All     SuiteOverlay `json:"all,omitempty" yaml:"all,omitempty"`
}

// Selection is one resolved invocation selector. Selector is the canonical
// public suite/check label so follow-up commands can preserve scope exactly.
type Selection struct {
	Selector      string   `json:"selector"`
	Checks        []string `json:"checks"`
	ExplicitCheck bool     `json:"explicitCheck,omitempty"`
}

// ValidateSuiteConfig validates both configurable overlays independently of a
// particular invocation. Explicit named selection may intentionally bypass
// this composition when a caller offers that recovery path.
func ValidateSuiteConfig(config SuiteConfig) error {
	if _, err := ComposeSuite(SuiteDefault, config.Default); err != nil {
		return err
	}
	if _, err := ComposeSuite(SuiteAll, config.All); err != nil {
		return err
	}
	return nil
}

// ResolveSelection accepts omitted/default, all, audit, or one named check.
// Explicit checks bypass configured suite composition.
func ResolveSelection(raw []string, config SuiteConfig) (Selection, error) {
	selectors := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item != "" {
			selectors = append(selectors, item)
		}
	}
	if len(selectors) > 1 {
		return Selection{}, fmt.Errorf("exactly one validation selector is allowed; got %d", len(selectors))
	}
	if len(selectors) == 0 {
		if err := ValidateSuiteConfig(config); err != nil {
			return Selection{}, err
		}
		checks, err := ComposeSuite(SuiteDefault, config.Default)
		return Selection{Selector: string(SuiteDefault), Checks: checks}, err
	}

	rawSelector := selectors[0]
	normalized := normalizeCheckName(rawSelector)
	switch CheckSuite(normalized) {
	case SuiteDefault:
		if err := ValidateSuiteConfig(config); err != nil {
			return Selection{}, err
		}
		checks, err := ComposeSuite(SuiteDefault, config.Default)
		return Selection{Selector: string(SuiteDefault), Checks: checks}, err
	case SuiteAll:
		if err := ValidateSuiteConfig(config); err != nil {
			return Selection{}, err
		}
		checks, err := ComposeSuite(SuiteAll, config.All)
		return Selection{Selector: string(SuiteAll), Checks: checks}, err
	case SuiteAudit:
		checks, err := ComposeSuite(SuiteAudit, SuiteOverlay{})
		return Selection{Selector: string(SuiteAudit), Checks: checks}, err
	}

	canonical, ok := CanonicalCheck(rawSelector)
	if !ok {
		return Selection{}, fmt.Errorf("unknown validation selector %q (valid suites: default, all, audit; valid checks: %s)", rawSelector, validCheckNames())
	}
	registration, _ := lookupCheck(canonical)
	return Selection{Selector: registration.CLIName, Checks: []string{canonical}, ExplicitCheck: true}, nil
}

// ComposeSuite applies a strict add/skip overlay and emits registry order.
func ComposeSuite(suite CheckSuite, overlay SuiteOverlay) ([]string, error) {
	if suite == SuiteAudit {
		if len(overlay.Add) > 0 || len(overlay.Skip) > 0 {
			return nil, fmt.Errorf("audit suite is fixed and cannot be configured")
		}
		return builtInSuiteChecks(SuiteAudit), nil
	}
	if suite != SuiteDefault && suite != SuiteAll {
		return nil, fmt.Errorf("unknown validation suite %q", suite)
	}

	add, err := canonicalCheckSet(overlay.Add)
	if err != nil {
		return nil, err
	}
	skip, err := canonicalCheckSet(overlay.Skip)
	if err != nil {
		return nil, err
	}
	for name := range add {
		if _, overlaps := skip[name]; overlaps {
			registration, _ := lookupCheck(name)
			return nil, fmt.Errorf("validation.%s cannot both add and skip %q", suite, registration.CLIName)
		}
	}

	builtIn := builtInSuiteChecks(suite)
	allowedToSkip := make(map[string]struct{}, len(builtIn)+len(add))
	for _, name := range builtIn {
		allowedToSkip[name] = struct{}{}
	}
	for name := range add {
		allowedToSkip[name] = struct{}{}
	}
	for name := range skip {
		if _, ok := allowedToSkip[name]; !ok {
			registration, _ := lookupCheck(name)
			return nil, fmt.Errorf("validation.%s cannot skip %q because it is absent from the built-in suite and add list", suite, registration.CLIName)
		}
	}

	effective := make(map[string]struct{}, len(builtIn)+len(add))
	for _, name := range builtIn {
		effective[name] = struct{}{}
	}
	for name := range add {
		effective[name] = struct{}{}
	}
	for name := range skip {
		delete(effective, name)
	}
	if len(effective) == 0 {
		return nil, fmt.Errorf("effective %s suite is empty", suite)
	}

	checks := make([]string, 0, len(effective))
	for _, registration := range checkRegistry {
		if _, ok := effective[registration.Name]; ok {
			checks = append(checks, registration.Name)
		}
	}
	return checks, nil
}

func canonicalCheckSet(raw []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		canonical, ok := CanonicalCheck(item)
		if !ok {
			return nil, fmt.Errorf("unknown check %q (valid: %s)", item, validCheckNames())
		}
		result[canonical] = struct{}{}
	}
	return result, nil
}
