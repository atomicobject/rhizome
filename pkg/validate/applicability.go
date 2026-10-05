package validate

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// CheckOutcome is the prerequisite/applicability state of one selected check.
// A resolver always returns one of these states; selection code decides which
// descriptors to resolve, never which resolved results to omit.
type CheckOutcome string

const (
	CheckOutcomeCompleted     CheckOutcome = "completed"
	CheckOutcomeNotApplicable CheckOutcome = "not_applicable"
	CheckOutcomeBlocked       CheckOutcome = "blocked"
)

// Stable evidence codes let renderers add richer detail without parsing prose.
const (
	EvidenceFeatureAbsent               = "feature_absent"
	EvidenceExecutionSurfaceUnsupported = "execution_surface_unsupported"
	EvidencePrerequisiteMetadataInvalid = "prerequisite_metadata_invalid"
	EvidenceProjectionAvailable         = "projection_available"
	EvidenceProjectionUnavailable       = "projection_unavailable"
	EvidenceProjectionProbeFailed       = "projection_probe_failed"
	EvidenceCodeIndexUsable             = "code_index_usable"
	EvidenceCodeIndexUnusable           = "code_index_unusable"
	EvidenceCodeIndexProbeFailed        = "code_index_probe_failed"
)

// VaultFeatureFacts is a precomputed, enumeration-free description of vault
// capabilities. Required code values come from current config/runtime facts;
// persisted values come only from CodeIndexSnapshot.
type VaultFeatureFacts struct {
	OntologyConfigured bool `json:"ontologyConfigured"`
	CodeConfigured     bool `json:"codeConfigured"`
	// CodeAnchorRootsConfigured means at least one code-anchor language not in
	// code.disabledLanguages has non-empty roots.
	CodeAnchorRootsConfigured  bool   `json:"codeAnchorRootsConfigured"`
	QueryRecipesConfigured     bool   `json:"queryRecipesConfigured"`
	ViewsConfigured            bool   `json:"viewsConfigured"`
	EffortsPresent             bool   `json:"effortsPresent"`
	RequiredCodeIndexerVersion string `json:"requiredCodeIndexerVersion,omitempty"`
	RequiredCodeScopeHash      string `json:"requiredCodeScopeHash,omitempty"`
}

// AutoManagedProjectionSnapshot reports whether one requested projection
// domain is ready after a caller-owned refresh/probe. The validation-domain
// adapter may consume EFF-0046; ontology readiness may come from caller/runtime
// facts. This package owns neither implementation.
type AutoManagedProjectionSnapshot struct {
	Domain    ProjectionDomain `json:"domain"`
	Available bool             `json:"available"`
	Refreshed bool             `json:"refreshed"`
}

// CodeIndexSnapshot contains the complete persisted-fact budget for code
// prerequisite resolution. Producers must populate it without walking project
// roots or calling embedding/provider services.
type CodeIndexSnapshot struct {
	ConfiguredCapability bool      `json:"configuredCapability"`
	StorePresent         bool      `json:"storePresent"`
	RowsPresent          bool      `json:"rowsPresent"`
	IndexerVersion       string    `json:"indexerVersion,omitempty"`
	ScopeHash            string    `json:"scopeHash,omitempty"`
	IndexedFileCount     int       `json:"indexedFileCount"`
	IndexedAt            time.Time `json:"indexedAt,omitempty"`
}

// AutoManagedPrerequisiteProbe establishes one named non-code domain at a time.
// A successful snapshot for one domain never implies another domain is ready.
type AutoManagedPrerequisiteProbe interface {
	AutoManagedProjection(context.Context, CheckDescriptor, ProjectionDomain) (AutoManagedProjectionSnapshot, error)
}

// CodeIndexPrerequisiteProbe reads one persisted snapshot. The deliberately
// narrow interface has no discovery, filesystem-walk, index-build, or provider
// operation that prerequisite resolution could invoke.
type CodeIndexPrerequisiteProbe interface {
	CodeIndexSnapshot(context.Context) (CodeIndexSnapshot, error)
}

// PrerequisiteProbes are injected by the execution shell so applicability
// resolution stays independent of projection and storage implementations.
type PrerequisiteProbes struct {
	Projection AutoManagedPrerequisiteProbe
	CodeIndex  CodeIndexPrerequisiteProbe
}

// ProjectionEvidence records facts for exactly one required domain.
type ProjectionEvidence struct {
	Domain    ProjectionDomain `json:"domain"`
	Available bool             `json:"available"`
	Refreshed bool             `json:"refreshed"`
}

// CodeIndexEvidence records persisted and required code-index facts. A usable
// persisted snapshot explicitly does not claim live-worktree freshness.
type CodeIndexEvidence struct {
	Snapshot                    CodeIndexSnapshot `json:"snapshot"`
	RequiredIndexerVersion      string            `json:"requiredIndexerVersion,omitempty"`
	RequiredScopeHash           string            `json:"requiredScopeHash,omitempty"`
	LiveWorktreeFreshnessProved bool              `json:"liveWorktreeFreshnessProved"`
}

// ApplicabilityEvidence is additive structured evidence for one resolution.
// Consumers should key on Code and ignore fields they do not understand.
type ApplicabilityEvidence struct {
	Code       string              `json:"code"`
	Message    string              `json:"message"`
	Projection *ProjectionEvidence `json:"projection,omitempty"`
	CodeIndex  *CodeIndexEvidence  `json:"codeIndex,omitempty"`
}

// CheckApplicabilityResult is emitted for every resolved selected check,
// including explicit checks whose feature is absent or prerequisite is blocked.
type CheckApplicabilityResult struct {
	Check              string                  `json:"check"`
	Outcome            CheckOutcome            `json:"outcome"`
	Summary            string                  `json:"summary"`
	PreparationCommand string                  `json:"preparationCommand,omitempty"`
	Evidence           []ApplicabilityEvidence `json:"evidence,omitempty"`
}

// ResolveCheckApplicability resolves one selected descriptor without building
// indexes, walking project roots, or calling providers.
func ResolveCheckApplicability(
	ctx context.Context,
	descriptor CheckDescriptor,
	surface ExecutionSurface,
	features VaultFeatureFacts,
	probes PrerequisiteProbes,
) CheckApplicabilityResult {
	result := CheckApplicabilityResult{Check: descriptor.Name}
	if message := prerequisiteMetadataError(descriptor); message != "" {
		return blockedResult(result, "check prerequisite metadata is invalid", "", ApplicabilityEvidence{
			Code:    EvidencePrerequisiteMetadataInvalid,
			Message: message,
		})
	}

	applicable, known := checkFeatureAvailable(descriptor.Applicability, features)
	if !known {
		return blockedResult(result, "check has unknown applicability", descriptor.PreparationCommand, ApplicabilityEvidence{
			Code:    EvidenceProjectionProbeFailed,
			Message: fmt.Sprintf("unknown check applicability %q", descriptor.Applicability),
		})
	}
	if !applicable {
		result.Outcome = CheckOutcomeNotApplicable
		result.Summary = featureAbsentSummary(descriptor.Applicability)
		result.Evidence = []ApplicabilityEvidence{{
			Code:    EvidenceFeatureAbsent,
			Message: result.Summary,
		}}
		return result
	}

	if !slices.Contains(descriptor.ExecutionSurfaces, surface) {
		message, preparationCommand := unsupportedSurfaceGuidance(descriptor, surface)
		return blockedResult(result, message, preparationCommand, ApplicabilityEvidence{
			Code:    EvidenceExecutionSurfaceUnsupported,
			Message: message,
		})
	}

	for _, domain := range descriptor.ProjectionDomains {
		var (
			evidence  ApplicabilityEvidence
			available bool
			summary   string
		)
		switch domain {
		case ProjectionValidation, ProjectionOntology:
			evidence, available, summary = probeManagedProjection(ctx, descriptor, domain, probes.Projection)
		case ProjectionCode:
			evidence, available, summary = probeCodeIndex(ctx, features, probes.CodeIndex)
		default:
			return blockedResult(result, "check projection domain is invalid", descriptor.PreparationCommand, ApplicabilityEvidence{
				Code:    EvidenceProjectionProbeFailed,
				Message: fmt.Sprintf("unknown projection domain %q", domain),
			})
		}
		if !available {
			return missingPrerequisiteResult(result, descriptor, summary, evidence)
		}
		result.Evidence = append(result.Evidence, evidence)
	}

	result.Outcome = CheckOutcomeCompleted
	result.Summary = "required validation prerequisites are available"
	if len(result.Evidence) > 0 && result.Evidence[len(result.Evidence)-1].Code == EvidenceCodeIndexUsable {
		result.Summary = result.Evidence[len(result.Evidence)-1].Message
	}
	return result
}

func unsupportedSurfaceGuidance(descriptor CheckDescriptor, surface ExecutionSurface) (string, string) {
	if surface == SurfaceCI && slices.Contains(descriptor.ProjectionDomains, ProjectionCode) {
		publicName := strings.TrimSpace(descriptor.CLIName)
		if publicName == "" {
			publicName = strings.ReplaceAll(descriptor.Name, "_", "-")
		}
		return "check requires the live persisted code index and is unavailable in isolated scratch CI; run it locally or remove it from the configured CI suite", "rzm validate " + publicName
	}
	return fmt.Sprintf("check is not supported on the %s execution surface", surface), descriptor.PreparationCommand
}

// prerequisiteMetadataError pins the current aggregate descriptor shorthand:
// validation/ontology domains are caller-probed and auto-managed, while any
// code domain additionally requires an externally persisted code snapshot.
func prerequisiteMetadataError(descriptor CheckDescriptor) string {
	if len(descriptor.ProjectionDomains) == 0 {
		return "check declares no projection domains"
	}
	hasCode := slices.Contains(descriptor.ProjectionDomains, ProjectionCode)
	if hasCode && descriptor.PrerequisiteMode != PrerequisiteExternal {
		return fmt.Sprintf("code projection requires prerequisite mode %q", PrerequisiteExternal)
	}
	if !hasCode && descriptor.PrerequisiteMode != PrerequisiteAutoManaged {
		return fmt.Sprintf("non-code projections require prerequisite mode %q", PrerequisiteAutoManaged)
	}
	return ""
}

func probeManagedProjection(
	ctx context.Context,
	descriptor CheckDescriptor,
	domain ProjectionDomain,
	probe AutoManagedPrerequisiteProbe,
) (ApplicabilityEvidence, bool, string) {
	evidence := ApplicabilityEvidence{
		Projection: &ProjectionEvidence{Domain: domain},
	}
	if probe == nil {
		evidence.Code = EvidenceProjectionProbeFailed
		evidence.Message = fmt.Sprintf("%s prerequisite probe is not configured", domain)
		return evidence, false, projectionFailureSummary(domain)
	}

	snapshot, err := probe.AutoManagedProjection(ctx, descriptor, domain)
	evidence.Projection.Available = snapshot.Available
	evidence.Projection.Refreshed = snapshot.Refreshed
	if err != nil {
		evidence.Code = EvidenceProjectionProbeFailed
		evidence.Message = fmt.Sprintf("%s projection probe failed: %v", domain, err)
		return evidence, false, projectionFailureSummary(domain)
	}
	if snapshot.Domain != domain {
		evidence.Code = EvidenceProjectionProbeFailed
		evidence.Message = fmt.Sprintf("%s projection probe returned evidence for %q", domain, snapshot.Domain)
		return evidence, false, projectionFailureSummary(domain)
	}
	if !snapshot.Available {
		evidence.Code = EvidenceProjectionUnavailable
		evidence.Message = fmt.Sprintf("%s projection is unavailable after refresh", domain)
		return evidence, false, projectionUnavailableSummary(domain)
	}

	evidence.Code = EvidenceProjectionAvailable
	evidence.Message = fmt.Sprintf("%s projection is available", domain)
	return evidence, true, ""
}

func probeCodeIndex(
	ctx context.Context,
	features VaultFeatureFacts,
	probe CodeIndexPrerequisiteProbe,
) (ApplicabilityEvidence, bool, string) {
	evidence := ApplicabilityEvidence{Projection: &ProjectionEvidence{Domain: ProjectionCode}}
	if probe == nil {
		evidence.Code = EvidenceCodeIndexProbeFailed
		evidence.Message = "persisted code-index prerequisite probe is not configured"
		return evidence, false, "persisted code index could not be inspected"
	}

	snapshot, err := probe.CodeIndexSnapshot(ctx)
	evidence.CodeIndex = &CodeIndexEvidence{
		Snapshot:                    snapshot,
		RequiredIndexerVersion:      features.RequiredCodeIndexerVersion,
		RequiredScopeHash:           features.RequiredCodeScopeHash,
		LiveWorktreeFreshnessProved: false,
	}
	if err != nil {
		evidence.Code = EvidenceCodeIndexProbeFailed
		evidence.Message = fmt.Sprintf("persisted code-index probe failed: %v", err)
		return evidence, false, "persisted code index could not be inspected"
	}

	if reasons := unusableCodeIndexReasons(snapshot, features); len(reasons) > 0 {
		evidence.Code = EvidenceCodeIndexUnusable
		evidence.Message = "persisted code index is unusable: " + strings.Join(reasons, "; ")
		return evidence, false, "persisted code index is missing or incompatible"
	}

	evidence.Code = EvidenceCodeIndexUsable
	evidence.Message = "persisted code index is usable; this snapshot does not prove live-worktree freshness"
	evidence.Projection.Available = true
	return evidence, true, ""
}

func projectionFailureSummary(domain ProjectionDomain) string {
	if domain == ProjectionValidation {
		return "required validation projection could not be prepared"
	}
	return fmt.Sprintf("required %s projection could not be prepared", domain)
}

func projectionUnavailableSummary(domain ProjectionDomain) string {
	if domain == ProjectionValidation {
		return "required validation projection is unavailable"
	}
	return fmt.Sprintf("required %s projection is unavailable", domain)
}

func checkFeatureAvailable(applicability CheckApplicability, features VaultFeatureFacts) (available, known bool) {
	switch applicability {
	case ApplicabilityAlways:
		return true, true
	case ApplicabilityOntology:
		return features.OntologyConfigured, true
	case ApplicabilityCode:
		return features.CodeConfigured, true
	case ApplicabilityCodeAnchors:
		return features.CodeConfigured && features.CodeAnchorRootsConfigured, true
	case ApplicabilityQueryRecipes:
		return features.QueryRecipesConfigured, true
	case ApplicabilityViews:
		return features.ViewsConfigured, true
	case ApplicabilityEfforts:
		return features.EffortsPresent, true
	default:
		return false, false
	}
}

func featureAbsentSummary(applicability CheckApplicability) string {
	if applicability == ApplicabilityCodeAnchors {
		return "no code folders for an enabled language; run rzm init --check to see the fix, or remove code folder limits or code.disabledLanguages in .rhizome/config.yml"
	}
	return "vault feature is not configured or present"
}

func unusableCodeIndexReasons(snapshot CodeIndexSnapshot, features VaultFeatureFacts) []string {
	reasons := make([]string, 0, 7)
	if !snapshot.ConfiguredCapability {
		reasons = append(reasons, "configured code capability is absent")
	}
	if !snapshot.StorePresent {
		reasons = append(reasons, "index store is absent")
	}
	if !snapshot.RowsPresent {
		reasons = append(reasons, "code rows are absent")
	}
	if strings.TrimSpace(features.RequiredCodeIndexerVersion) == "" || snapshot.IndexerVersion != features.RequiredCodeIndexerVersion {
		reasons = append(reasons, "indexer version is missing or mismatched")
	}
	if strings.TrimSpace(features.RequiredCodeScopeHash) == "" || snapshot.ScopeHash != features.RequiredCodeScopeHash {
		reasons = append(reasons, "scope hash is missing or mismatched")
	}
	if snapshot.IndexedFileCount <= 0 {
		reasons = append(reasons, "indexed file count is zero")
	}
	if snapshot.IndexedAt.IsZero() {
		reasons = append(reasons, "stored index timestamp is absent")
	}
	return reasons
}

func blockedResult(
	result CheckApplicabilityResult,
	summary string,
	preparationCommand string,
	evidence ApplicabilityEvidence,
) CheckApplicabilityResult {
	result.Outcome = CheckOutcomeBlocked
	result.Summary = summary
	result.PreparationCommand = preparationCommand
	result.Evidence = append(result.Evidence, evidence)
	return result
}

func missingPrerequisiteResult(
	result CheckApplicabilityResult,
	descriptor CheckDescriptor,
	summary string,
	evidence ApplicabilityEvidence,
) CheckApplicabilityResult {
	if descriptor.MissingPrerequisiteOutcome == PrerequisiteNotApplicable {
		result.Outcome = CheckOutcomeNotApplicable
		result.Summary = summary
		result.Evidence = append(result.Evidence, evidence)
		return result
	}
	result.Outcome = CheckOutcomeBlocked
	result.Summary = summary
	result.PreparationCommand = descriptor.PreparationCommand
	result.Evidence = append(result.Evidence, evidence)
	return result
}
