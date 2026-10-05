package noteformat

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/paths"
)

var (
	// ErrUnknownProjectorFormat reports an executable projector whose descriptor
	// is not present in the immutable runtime registry.
	ErrUnknownProjectorFormat = errors.New("unknown note format projector")
	// ErrDuplicateProjector reports two projectors for one format.
	ErrDuplicateProjector = errors.New("duplicate note format projector")
	// ErrProjectorDescriptorMismatch reports a projector whose descriptor does
	// not exactly match the descriptor frozen in the registry.
	ErrProjectorDescriptorMismatch = errors.New("projector descriptor mismatch")
	// ErrProjectorUnavailable reports a descriptor-only provider such as the
	// skeletal HTML provider.
	ErrProjectorUnavailable = errors.New("note format projector unavailable")
	// ErrSourceFormatMismatch reports a source whose format and path no longer
	// select the same immutable descriptor.
	ErrSourceFormatMismatch = errors.New("authored source format mismatch")
	// ErrInvalidAuthoredSource reports a source that has lost canonical
	// identity/freshness integrity before executable projection can begin.
	ErrInvalidAuthoredSource = errors.New("invalid authored source")
	// ErrProjectionDescriptorMismatch reports executable output whose freshness
	// envelope or capabilities do not match its registered descriptor.
	ErrProjectionDescriptorMismatch = errors.New("projection does not match provider descriptor")
	// ErrInvalidProjection reports an executable result that bypassed the
	// constructor's envelope validation.
	ErrInvalidProjection = errors.New("invalid note format projection")
	// ErrInvalidProjectionFacts reports syntactically malformed facts or source
	// ranges that do not fit the supplied authored bytes.
	ErrInvalidProjectionFacts = errors.New("invalid projection facts")
)

// Projector is an optional executable capability for one registered provider.
// Project may be called concurrently and implementations must synchronize any
// mutable state. It receives only an immutable AuthoredSource snapshot and
// returns unresolved syntax facts; it never reads files, resolves targets, or
// writes persistence.
type Projector interface {
	Provider
	Project(AuthoredSource) (Projection, error)
}

// Runtime composes an immutable descriptor registry with optional executable
// projectors. Descriptors remain closed and immutable even when a registered
// projector has mutable implementation state.
type Runtime struct {
	registry             Registry
	projectors           map[string]Projector
	rootMetadataPatchers map[string]RootMetadataPatchPlanner
}

// NewRuntime constructs a runtime from one already-frozen registry. Every
// projector must compose exactly one registered descriptor.
func NewRuntime(registry Registry, projectors ...Projector) (Runtime, error) {
	runtime := Runtime{
		registry:             registry,
		projectors:           make(map[string]Projector, len(projectors)),
		rootMetadataPatchers: make(map[string]RootMetadataPatchPlanner),
	}
	for _, projector := range projectors {
		if projector == nil {
			return Runtime{}, errors.New("note format projector is required")
		}
		descriptor, err := normalizeDescriptor(projector.Descriptor())
		if err != nil {
			return Runtime{}, fmt.Errorf("%w: %v", ErrProjectorDescriptorMismatch, err)
		}
		formatID := descriptor.ID
		registered, known := registry.Provider(formatID)
		if !known {
			return Runtime{}, fmt.Errorf("%w: %q", ErrUnknownProjectorFormat, descriptor.ID)
		}
		if !descriptorsEqual(descriptor, registered.Descriptor()) {
			return Runtime{}, fmt.Errorf("%w: %q", ErrProjectorDescriptorMismatch, descriptor.ID)
		}
		if _, duplicate := runtime.projectors[string(formatID)]; duplicate {
			return Runtime{}, fmt.Errorf("%w: %q", ErrDuplicateProjector, formatID)
		}
		runtime.projectors[string(formatID)] = projector
		if patcher, ok := projector.(RootMetadataPatchPlanner); ok {
			runtime.rootMetadataPatchers[string(formatID)] = patcher
		}
	}
	return runtime, nil
}

// PlanRootMetadataPatch dispatches a source-bound semantic metadata edit to
// the selected provider. Mutation execution remains separate from projection
// and from filesystem writes, but shares the same provider generation.
func (r Runtime) PlanRootMetadataPatch(source AuthoredSource, projection Projection, changes []RootMetadataChange) (MetadataPatchPlan, error) {
	descriptor, err := r.descriptorForSource(source)
	if err != nil {
		return MetadataPatchPlan{}, err
	}
	patcher, ok := r.rootMetadataPatchers[fold(descriptor.ID)]
	if !ok {
		return MetadataPatchPlan{}, fmt.Errorf("root metadata mutation unavailable for format %q", descriptor.ID)
	}
	if !projectionMatchesDescriptor(projection, descriptor) {
		return MetadataPatchPlan{}, fmt.Errorf("%w: %q", ErrProjectionDescriptorMismatch, descriptor.ID)
	}
	plan, err := patcher.PlanRootMetadataPatch(source, projection.Copy(), changes)
	if err != nil {
		return MetadataPatchPlan{}, err
	}
	if err := ValidateMetadataPatchPlan(source, plan); err != nil {
		return MetadataPatchPlan{}, err
	}
	return plan, nil
}

// CanPatchRootMetadata reports executable mutation support, rather than the
// descriptor's aspirational capability alone.
func (r Runtime) CanPatchRootMetadata(id FormatID) bool {
	_, ok := r.rootMetadataPatchers[fold(id)]
	return ok
}

// Registry returns the immutable descriptor registry assembled for this
// runtime. Registry accessors return copied descriptors.
func (r Runtime) Registry() Registry {
	return r.registry
}

// Provider returns a descriptor-only provider by stable format ID.
func (r Runtime) Provider(id FormatID) (Provider, bool) {
	return r.registry.Provider(id)
}

// ProviderForPath returns the descriptor-only provider that claims path.
func (r Runtime) ProviderForPath(path paths.RelPath) (Provider, bool) {
	return r.registry.ProviderForPath(path)
}

// CanProject reports whether a registered descriptor has an executable
// projector. Runtime.Project remains the only public execution path.
func (r Runtime) CanProject(id FormatID) bool {
	_, ok := r.projectors[fold(id)]
	return ok
}

// Project dispatches source to its selected provider projector. A
// descriptor-only provider reports ErrProjectorUnavailable rather than
// pretending to have extraction behavior.
func (r Runtime) Project(source AuthoredSource) (Projection, error) {
	descriptor, err := r.descriptorForSource(source)
	if err != nil {
		return Projection{}, err
	}
	projector, ok := r.projectors[fold(descriptor.ID)]
	if !ok {
		return Projection{}, fmt.Errorf("%w: %q", ErrProjectorUnavailable, descriptor.ID)
	}
	projection, err := projector.Project(source)
	if err != nil {
		return Projection{}, err
	}
	if err := validateProjection(projection); err != nil {
		return Projection{}, fmt.Errorf("%w: %v", ErrInvalidProjection, err)
	}
	if !projectionMatchesDescriptor(projection, descriptor) {
		return Projection{}, fmt.Errorf("%w: %q", ErrProjectionDescriptorMismatch, descriptor.ID)
	}
	if err := validateProjectionFacts(projection.Facts, len(source.content)); err != nil {
		return Projection{}, fmt.Errorf("%w: %v", ErrInvalidProjectionFacts, err)
	}
	for _, diagnostic := range projection.Diagnostics {
		if diagnostic.Range.Present && diagnostic.Range.Range.EndByte > len(source.content) {
			return Projection{}, fmt.Errorf("%w: diagnostic range ends beyond source", ErrInvalidProjection)
		}
	}
	return projection.Copy(), nil
}

func (r Runtime) descriptorForSource(source AuthoredSource) (Descriptor, error) {
	path, err := paths.CleanNotePath(source.Path().String())
	if err != nil || path != source.Path() {
		return Descriptor{}, fmt.Errorf("%w: invalid canonical path", ErrInvalidAuthoredSource)
	}
	if int64(len(source.content)) != source.Size() {
		return Descriptor{}, fmt.Errorf("%w: size does not match source bytes", ErrInvalidAuthoredSource)
	}
	hash := sha256.Sum256(source.content)
	if hex.EncodeToString(hash[:]) != source.ContentHash() {
		return Descriptor{}, fmt.Errorf("%w: hash does not match source bytes", ErrInvalidAuthoredSource)
	}
	provider, known := r.registry.Provider(source.Format())
	if !known {
		return Descriptor{}, fmt.Errorf("%w: unknown format %q", ErrSourceFormatMismatch, source.Format())
	}
	if source.Format() != provider.Descriptor().ID {
		return Descriptor{}, fmt.Errorf("%w: format %q is not canonical", ErrInvalidAuthoredSource, source.Format())
	}
	claimed, claimedPath := r.registry.ProviderForPath(paths.RelPath(path))
	if !claimedPath || claimed.Descriptor().ID != provider.Descriptor().ID {
		return Descriptor{}, fmt.Errorf("%w: format %q does not claim path %q", ErrSourceFormatMismatch, source.Format(), source.Path())
	}
	return provider.Descriptor(), nil
}

func projectionMatchesDescriptor(projection Projection, descriptor Descriptor) bool {
	if projection.ProviderVersion != descriptor.ProviderVersion || projection.ProjectionVersion != descriptor.ProjectionVersion {
		return false
	}
	return capabilitiesEqual(projection.Capabilities, descriptor.Capabilities)
}

func descriptorsEqual(first, second Descriptor) bool {
	if first.ID != second.ID || first.ProviderVersion != second.ProviderVersion || first.ProjectionVersion != second.ProjectionVersion || first.OwnershipPolicy != second.OwnershipPolicy || !capabilitiesEqual(first.Capabilities, second.Capabilities) || len(first.Extensions) != len(second.Extensions) {
		return false
	}
	for index := range first.Extensions {
		if first.Extensions[index] != second.Extensions[index] {
			return false
		}
	}
	return true
}

func capabilitiesEqual(first, second Capabilities) bool {
	firstValues := first.Values()
	secondValues := second.Values()
	if len(firstValues) != len(secondValues) {
		return false
	}
	for index := range firstValues {
		if firstValues[index] != secondValues[index] {
			return false
		}
	}
	return true
}
