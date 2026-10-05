package noteformat

import (
	"fmt"
	"mime"
	"strings"
)

// SearchRegionOrigin is the closed provenance for searchable note evidence.
// It never classifies an authored or derived region as code intelligence.
type SearchRegionOrigin string

const (
	SearchRegionAuthored SearchRegionOrigin = "authored"
	SearchRegionDerived  SearchRegionOrigin = "derived"
)

// SearchRegionKind is the closed retrieval role for searchable note evidence.
// It is independent of origin: a provider may derive either visible prose or
// supplemental content from authored source bytes.
type SearchRegionKind string

const (
	SearchRegionVisible      SearchRegionKind = "visible"
	SearchRegionSupplemental SearchRegionKind = "supplemental"
)

// SearchRegionFact is searchable note evidence. Authored regions retain an
// exact source span; derived regions may omit it when no byte-for-byte source
// span exists (for example rendered HTML or script-provided note content).
type SearchRegionFact struct {
	Origin    SearchRegionOrigin
	Kind      SearchRegionKind
	Text      string
	MediaType string
	Range     OptionalSourceRange
}

func validateSearchRegions(regions []SearchRegionFact, validate func(string, OptionalSourceRange) error) error {
	if err := validateFactOccurrences("search region", regions, func(region SearchRegionFact) OptionalSourceRange {
		return region.Range
	}, validate); err != nil {
		return err
	}
	for _, region := range regions {
		if region.Origin != SearchRegionAuthored && region.Origin != SearchRegionDerived {
			return fmt.Errorf("search region has invalid origin %q", region.Origin)
		}
		if region.Kind != SearchRegionVisible && region.Kind != SearchRegionSupplemental {
			return fmt.Errorf("search region has invalid kind %q", region.Kind)
		}
		if region.Text == "" {
			return fmt.Errorf("search region has empty text")
		}
		if strings.TrimSpace(region.MediaType) == "" {
			return fmt.Errorf("search region has empty media type")
		}
		if _, _, err := mime.ParseMediaType(region.MediaType); err != nil {
			return fmt.Errorf("search region has invalid media type %q: %w", region.MediaType, err)
		}
		if region.Origin == SearchRegionAuthored && !region.Range.Present {
			return fmt.Errorf("authored search region requires a source range")
		}
	}
	return nil
}
