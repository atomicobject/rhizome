package planner

import (
	"github.com/atomicobject/rhizome/pkg/search"
)

// IntentProfile captures retriever + ranking defaults for a specific intent.
type IntentProfile struct {
	Intent search.Intent
	Label  string

	EnableVector *bool
	EnableIntel  *bool
	EnableGraph  *bool
	EnableRefs   *bool

	MaxPerOwner *int
	UseFTSBody  *bool
}

// resolveIntentProfile returns a profile for the given intent if one is defined.
func resolveIntentProfile(intent search.Intent) (IntentProfile, bool) {
	profile, ok := intentProfiles[intent]
	return profile, ok
}

// applyIntentProfile overlays profile fields onto options (only non-nil fields apply).
func applyIntentProfile(opts Options, profile IntentProfile) Options {
	if profile.EnableVector != nil {
		opts.EnableVector = *profile.EnableVector
	}
	if profile.EnableIntel != nil {
		opts.EnableIntel = *profile.EnableIntel
	}
	if profile.EnableGraph != nil {
		opts.EnableGraph = *profile.EnableGraph
	}
	if profile.EnableRefs != nil {
		opts.EnableRefs = *profile.EnableRefs
	}
	if profile.MaxPerOwner != nil {
		opts.MaxPerOwner = *profile.MaxPerOwner
	}
	if profile.UseFTSBody != nil {
		opts.UseFTSBody = *profile.UseFTSBody
	}
	return opts
}

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

var intentProfiles = map[search.Intent]IntentProfile{
	search.IntentSearch: {
		Intent: search.IntentSearch,
		Label:  "default search",
	},
	search.IntentDocsForCode: {
		Intent: search.IntentDocsForCode,
		Label:  "docs for code",
	},
	search.IntentCodeForDocs: {
		Intent: search.IntentCodeForDocs,
		Label:  "code for docs",
		// Keep note-graph noise down; emphasize refs/code expansion.
		EnableGraph: boolPtr(false),
		MaxPerOwner: intPtr(2),
	},
	search.IntentRelatedToSeed: {
		Intent: search.IntentRelatedToSeed,
		Label:  "related to seed",
	},
	search.IntentOverview: {
		Intent:      search.IntentOverview,
		Label:       "overview",
		MaxPerOwner: intPtr(1),
	},
	search.IntentSubsystemOverview: {
		Intent:      search.IntentSubsystemOverview,
		Label:       "subsystem overview",
		MaxPerOwner: intPtr(2),
	},
	search.IntentGoToDef: {
		Intent:       search.IntentGoToDef,
		Label:        "go to definition",
		EnableVector: boolPtr(false),
		EnableGraph:  boolPtr(false),
		MaxPerOwner:  intPtr(1),
	},
	search.IntentFindUsages: {
		Intent:       search.IntentFindUsages,
		Label:        "find usages",
		EnableVector: boolPtr(false),
		EnableGraph:  boolPtr(false),
	},
	search.IntentCallers: {
		Intent:       search.IntentCallers,
		Label:        "callers",
		EnableVector: boolPtr(false),
		EnableGraph:  boolPtr(false),
	},
	search.IntentCallees: {
		Intent:       search.IntentCallees,
		Label:        "callees",
		EnableVector: boolPtr(false),
		EnableGraph:  boolPtr(false),
	},
	search.IntentTestsForCode: {
		Intent:       search.IntentTestsForCode,
		Label:        "tests for code",
		EnableVector: boolPtr(false),
		EnableGraph:  boolPtr(false),
	},
	search.IntentExplainSymbol: {
		Intent: search.IntentExplainSymbol,
		Label:  "explain symbol",
	},
	search.IntentRefactorImpact: {
		Intent:       search.IntentRefactorImpact,
		Label:        "refactor impact",
		EnableVector: boolPtr(false),
		EnableGraph:  boolPtr(false),
	},
	search.IntentSecurityAudit: {
		Intent:       search.IntentSecurityAudit,
		Label:        "security audit",
		EnableVector: boolPtr(false),
	},
	search.IntentDataFlow: {
		Intent:       search.IntentDataFlow,
		Label:        "data flow",
		EnableVector: boolPtr(false),
	},
	search.IntentImplementers: {
		Intent:       search.IntentImplementers,
		Label:        "implementers",
		EnableVector: boolPtr(false),
	},
	search.IntentOverrides: {
		Intent:       search.IntentOverrides,
		Label:        "overrides",
		EnableVector: boolPtr(false),
	},
	search.IntentImports: {
		Intent:       search.IntentImports,
		Label:        "imports",
		EnableVector: boolPtr(false),
	},
}
