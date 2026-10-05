// Package noteformat defines format-neutral note provider descriptors.
//
// Providers declare stable format identity, extension claims, projection
// freshness, and capability vocabulary. Providers do not read files or own
// persistence; notemeta owns authored bytes and downstream domains consume
// provider projections in later phases.
package noteformat

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// FormatID is a stable provider identity persisted with note source facts.
type FormatID string

// Capability is a stable persisted identifier for a projection or maintenance
// operation a format provider may explicitly support.
type Capability string

const (
	CapabilitySourceReading               Capability = "source_reading"
	CapabilitySearchableContentProjection Capability = "searchable_content_projection"
	CapabilityRootMetadataReading         Capability = "root_metadata_reading"
	CapabilityAuthoredLinkExtraction      Capability = "authored_link_extraction"
	CapabilityFragmentTargetExtraction    Capability = "fragment_target_extraction"
	CapabilityRootMetadataMutation        Capability = "root_metadata_mutation"
	CapabilityLinkRetargeting             Capability = "link_retargeting"
	CapabilityFileRenameMove              Capability = "file_rename_move"
	CapabilityStructuralContentMutation   Capability = "structural_content_mutation"
	CapabilityActiveContentViewing        Capability = "active_content_viewing"
)

var knownCapabilities = map[Capability]struct{}{
	CapabilitySourceReading:               {},
	CapabilitySearchableContentProjection: {},
	CapabilityRootMetadataReading:         {},
	CapabilityAuthoredLinkExtraction:      {},
	CapabilityFragmentTargetExtraction:    {},
	CapabilityRootMetadataMutation:        {},
	CapabilityLinkRetargeting:             {},
	CapabilityFileRenameMove:              {},
	CapabilityStructuralContentMutation:   {},
	CapabilityActiveContentViewing:        {},
}

// Capabilities is an immutable canonical sorted set of provider capabilities.
type Capabilities struct {
	values []Capability
}

// NewCapabilities validates, deduplicates, and sorts the supplied stable IDs.
func NewCapabilities(capabilities ...Capability) (Capabilities, error) {
	values := append([]Capability(nil), capabilities...)
	seen := make(map[Capability]struct{}, len(values))
	for _, capability := range values {
		if _, known := knownCapabilities[capability]; !known {
			return Capabilities{}, fmt.Errorf("unknown note format capability %q", capability)
		}
		if _, duplicate := seen[capability]; duplicate {
			continue
		}
		seen[capability] = struct{}{}
	}
	values = values[:0]
	for capability := range seen {
		values = append(values, capability)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return Capabilities{values: values}, nil
}

// MustCapabilities returns a validated canonical set and panics for an invalid
// built-in declaration.
func MustCapabilities(capabilities ...Capability) Capabilities {
	result, err := NewCapabilities(capabilities...)
	if err != nil {
		panic(err)
	}
	return result
}

// Has reports whether c declares capability.
func (c Capabilities) Has(capability Capability) bool {
	if _, known := knownCapabilities[capability]; !known {
		return false
	}
	index := sort.Search(len(c.values), func(index int) bool { return c.values[index] >= capability })
	return index < len(c.values) && c.values[index] == capability
}

// Values returns a copy of canonical stable capability IDs.
func (c Capabilities) Values() []Capability {
	return append([]Capability(nil), c.values...)
}

func (c Capabilities) valid() bool {
	canonical, err := NewCapabilities(c.values...)
	if err != nil || len(canonical.values) != len(c.values) {
		return false
	}
	for index := range c.values {
		if c.values[index] != canonical.values[index] {
			return false
		}
	}
	return true
}

// OwnershipPolicy controls how a provider extension becomes note-owned.
type OwnershipPolicy string

const (
	// OwnershipDefault supports classic and ordinary configured note ownership.
	OwnershipDefault OwnershipPolicy = "default"
	// OwnershipExplicitInclude requires an extension-explicit configured include.
	OwnershipExplicitInclude OwnershipPolicy = "explicit_include"
)

// Descriptor describes a built-in provider without exposing filesystem or
// persistence behavior. Extensions include their leading dot.
type Descriptor struct {
	ID                FormatID
	Extensions        []string
	ProviderVersion   string
	ProjectionVersion string
	OwnershipPolicy   OwnershipPolicy
	Capabilities      Capabilities
}

// Provider is a descriptor-only provider. Later projection work may extend the
// boundary with source-byte input, but providers never own filesystem I/O.
type Provider interface {
	Descriptor() Descriptor
}

// AuthoredSource is the canonical, format-neutral identity and byte snapshot
// of an authored note. Providers consume these source facts in later phases;
// they do not read the filesystem themselves.
type AuthoredSource struct {
	path        paths.NotePath
	format      FormatID
	content     []byte
	contentHash string
	size        int64
	mtime       int64
}

// NewAuthoredSource returns a source snapshot for a selected, canonicalized
// provider descriptor with strictly vault-relative identity, copied bytes, and
// a SHA-256 hash of the copied bytes.
func NewAuthoredSource(path paths.NotePath, descriptor Descriptor, content []byte, mtime int64) (AuthoredSource, error) {
	path, err := paths.CleanNotePath(path.String())
	if err != nil {
		return AuthoredSource{}, err
	}
	descriptor, err = normalizeDescriptor(descriptor)
	if err != nil {
		return AuthoredSource{}, err
	}
	if !descriptorClaimsPath(descriptor, path) {
		return AuthoredSource{}, fmt.Errorf("note format %q does not claim path %q", descriptor.ID, path)
	}
	content = append([]byte(nil), content...)
	hash := sha256.Sum256(content)
	return AuthoredSource{
		path:        path,
		format:      descriptor.ID,
		content:     content,
		contentHash: hex.EncodeToString(hash[:]),
		size:        int64(len(content)),
		mtime:       mtime,
	}, nil
}

// Path returns the canonical vault-relative authored path.
func (s AuthoredSource) Path() paths.NotePath {
	return s.path
}

// Format returns the stable selected provider identity.
func (s AuthoredSource) Format() FormatID {
	return s.format
}

// Bytes returns a copy of the authored source bytes.
func (s AuthoredSource) Bytes() []byte {
	return append([]byte(nil), s.content...)
}

// ContentHash returns the SHA-256 hash of the canonical authored bytes.
func (s AuthoredSource) ContentHash() string {
	return s.contentHash
}

// Size returns the byte length of the canonical authored bytes.
func (s AuthoredSource) Size() int64 {
	return s.size
}

// Mtime returns the observed source modification time supplied at construction.
func (s AuthoredSource) Mtime() int64 {
	return s.mtime
}

// ProjectionStatus describes whether a provider produced current derived facts
// for an authored source.
type ProjectionStatus string

const (
	ProjectionStatusStale   ProjectionStatus = "stale"
	ProjectionStatusCurrent ProjectionStatus = "current"
	ProjectionStatusFatal   ProjectionStatus = "fatal"
)

// Diagnostic is a provider result associated with an authored note. Blocking
// means the whole projection is unusable and is reserved for fatal results.
// Operation-local failures remain nonblocking here and fail closed when that
// operation validates its source preconditions.
type Diagnostic struct {
	Code              string
	Category          DiagnosticCategory
	Message           string
	Range             OptionalSourceRange
	Blocking          bool
	AffectedOperation DiagnosticOperation
}

type DiagnosticCategory string

const (
	DiagnosticCategoryProjection DiagnosticCategory = "projection"
	DiagnosticCategorySource     DiagnosticCategory = "source"
	DiagnosticCategoryMetadata   DiagnosticCategory = "metadata"
	DiagnosticCategoryContent    DiagnosticCategory = "content"
	DiagnosticCategoryLink       DiagnosticCategory = "link"
	DiagnosticCategoryLimit      DiagnosticCategory = "limit"
)

type DiagnosticOperation string

const (
	DiagnosticOperationProjection     DiagnosticOperation = "projection"
	DiagnosticOperationMetadataRead   DiagnosticOperation = "metadata_read"
	DiagnosticOperationLinkResolution DiagnosticOperation = "link_resolution"
	DiagnosticOperationSearch         DiagnosticOperation = "search"
	DiagnosticOperationViewer         DiagnosticOperation = "viewer"
	DiagnosticOperationMetadataWrite  DiagnosticOperation = "metadata_write"
)

// Projection is a versioned provider result. Facts remain unresolved authored
// syntax; shared core applies vault policy, candidate matching, and storage.
type Projection struct {
	ProviderVersion   string
	ProjectionVersion string
	Status            ProjectionStatus
	Diagnostics       []Diagnostic
	Capabilities      Capabilities
	Facts             ProjectionFacts
}

// NewProjection validates and copies the shared provider result envelope
// without syntax facts. NewProjectionWithFacts adds generic provider facts.
func NewProjection(providerVersion, projectionVersion string, status ProjectionStatus, diagnostics []Diagnostic, capabilities Capabilities) (Projection, error) {
	return NewProjectionWithFacts(providerVersion, projectionVersion, status, diagnostics, capabilities, ProjectionFacts{})
}

// NewProjectionWithFacts validates and copies a provider result envelope and
// its unresolved syntax facts. Source-bound range validation happens when a
// Runtime dispatches the projection against an AuthoredSource.
func NewProjectionWithFacts(providerVersion, projectionVersion string, status ProjectionStatus, diagnostics []Diagnostic, capabilities Capabilities, facts ProjectionFacts) (Projection, error) {
	projection := Projection{
		ProviderVersion:   strings.TrimSpace(providerVersion),
		ProjectionVersion: strings.TrimSpace(projectionVersion),
		Status:            status,
		Diagnostics:       append([]Diagnostic(nil), diagnostics...),
		Capabilities:      capabilities,
		Facts:             facts.clone(),
	}
	if err := validateProjection(projection); err != nil {
		return Projection{}, err
	}
	if err := validateProjectionFacts(facts, -1); err != nil {
		return Projection{}, fmt.Errorf("%w: %v", ErrInvalidProjectionFacts, err)
	}
	return projection, nil
}

// Copy returns a detached projection result suitable for callers to inspect or
// adapt without mutating a reusable projector-owned result.
func (p Projection) Copy() Projection {
	p.Diagnostics = append([]Diagnostic(nil), p.Diagnostics...)
	p.Facts = p.Facts.clone()
	return p
}

func validateProjection(projection Projection) error {
	if strings.TrimSpace(projection.ProviderVersion) == "" {
		return errors.New("provider version is required")
	}
	if strings.TrimSpace(projection.ProjectionVersion) == "" {
		return errors.New("projection version is required")
	}
	if !projection.Capabilities.valid() {
		return errors.New("projection capabilities are invalid")
	}
	if projection.Status != ProjectionStatusStale && projection.Status != ProjectionStatusCurrent && projection.Status != ProjectionStatusFatal {
		return fmt.Errorf("unknown projection status %q", projection.Status)
	}
	hasBlocking := false
	for _, diagnostic := range projection.Diagnostics {
		if strings.TrimSpace(diagnostic.Code) == "" || strings.TrimSpace(diagnostic.Message) == "" {
			return errors.New("projection diagnostics require code and message")
		}
		if diagnostic.Category != "" && !diagnostic.Category.valid() {
			return fmt.Errorf("projection diagnostic has unknown category %q", diagnostic.Category)
		}
		if diagnostic.AffectedOperation != "" && !diagnostic.AffectedOperation.valid() {
			return fmt.Errorf("projection diagnostic has unknown affected operation %q", diagnostic.AffectedOperation)
		}
		if !diagnostic.Range.Present && diagnostic.Range.Range != (SourceRange{}) {
			return errors.New("projection diagnostic has absent range with non-zero bytes")
		}
		if diagnostic.Range.Present && (diagnostic.Range.Range.StartByte < 0 || diagnostic.Range.Range.EndByte < diagnostic.Range.Range.StartByte) {
			return errors.New("projection diagnostic has invalid source range")
		}
		hasBlocking = hasBlocking || diagnostic.Blocking
	}
	if (projection.Status == ProjectionStatusStale || projection.Status == ProjectionStatusCurrent) && hasBlocking {
		return fmt.Errorf("%s projection cannot have a blocking diagnostic", projection.Status)
	}
	if projection.Status == ProjectionStatusFatal {
		if !hasBlocking {
			return errors.New("fatal projection requires a blocking diagnostic")
		}
		if !projection.Facts.empty() {
			return errors.New("fatal projection cannot contain derived facts")
		}
	}
	return nil
}

func (c DiagnosticCategory) valid() bool {
	switch c {
	case DiagnosticCategoryProjection, DiagnosticCategorySource, DiagnosticCategoryMetadata,
		DiagnosticCategoryContent, DiagnosticCategoryLink, DiagnosticCategoryLimit:
		return true
	default:
		return false
	}
}

func (o DiagnosticOperation) valid() bool {
	switch o {
	case DiagnosticOperationProjection, DiagnosticOperationMetadataRead, DiagnosticOperationLinkResolution,
		DiagnosticOperationSearch, DiagnosticOperationViewer, DiagnosticOperationMetadataWrite:
		return true
	default:
		return false
	}
}
