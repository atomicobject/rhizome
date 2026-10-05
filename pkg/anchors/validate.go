package codeanchor

import (
	"context"
	"fmt"
)

// ValidationStatus indicates the result of validating an anchor.
type ValidationStatus string

const (
	ValidationValid     ValidationStatus = "valid"
	ValidationNoMatch   ValidationStatus = "no_match"
	ValidationSuffix    ValidationStatus = "suffix_match"
	ValidationAmbiguous ValidationStatus = "ambiguous"
)

// ValidationResult holds the validation outcome for a single anchor.
type ValidationResult struct {
	Anchor        Anchor
	Status        ValidationStatus
	MatchedCount  int      // Number of exact matches in scope
	SuffixMatches []string // FQNs that match as suffix (suggestions)
	Message       string   // Human-readable explanation
}

// ValidateAnchors checks all symbol-based anchors against the index.
// Returns results for all validated anchors (including valid ones).
func (s *Service) ValidateAnchors(ctx context.Context) ([]ValidationResult, error) {
	anchors, err := s.store.Anchors(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch anchors: %w", err)
	}

	var results []ValidationResult
	for _, a := range anchors {
		result, err := s.validateAnchor(ctx, a)
		if err != nil {
			return nil, fmt.Errorf("validate anchor %q: %w", a.Label, err)
		}
		if result != nil {
			results = append(results, *result)
		}
	}
	return results, nil
}

func (s *Service) validateAnchor(ctx context.Context, a Anchor) (*ValidationResult, error) {
	// Only validate symbol-based anchors (function, baseClass)
	if a.Kind != AnchorFunc && a.Kind != AnchorBaseClass {
		return nil, nil
	}
	if a.BaseSym == nil {
		return nil, nil
	}

	// Build the target FQN from the anchor's symbol reference.
	targetFQN := a.BaseSym.Pkg + "." + a.BaseSym.Name
	if a.BaseSym.Pkg == "" {
		targetFQN = a.BaseSym.Name
	}

	// Check if the target FQN exists in the index.
	exists, err := s.store.SymbolExistsByFQN(ctx, targetFQN, a.Lang)
	if err != nil {
		return nil, fmt.Errorf("check exact symbol %q: %w", targetFQN, err)
	}

	if exists {
		// Exact FQN match found - valid
		return &ValidationResult{
			Anchor:       a,
			Status:       ValidationValid,
			MatchedCount: 1,
		}, nil
	}

	// No exact match - try suffix search for suggestions
	suffix := targetFQN

	suffixSyms, err := s.store.SymbolsBySuffix(ctx, suffix, a.Lang, 10)
	if err != nil {
		return nil, fmt.Errorf("search symbol suffix %q: %w", suffix, err)
	}

	if len(suffixSyms) == 0 {
		return &ValidationResult{
			Anchor:  a,
			Status:  ValidationNoMatch,
			Message: fmt.Sprintf("no symbols found matching %q", suffix),
		}, nil
	}

	// Found suffix matches - suggest corrections
	var fqns []string
	for _, sym := range suffixSyms {
		fqns = append(fqns, sym.FQN)
	}

	return &ValidationResult{
		Anchor:        a,
		Status:        ValidationSuffix,
		SuffixMatches: fqns,
		Message:       fmt.Sprintf("no exact match for %q; found %d suffix matches", suffix, len(fqns)),
	}, nil
}
