// Package oneshotruntime declares and validates the runtime work a one-shot
// operation is allowed to build and await. It composes bootstrap mechanisms;
// bootstrap and long-lived runtimes do not depend on this package.
package oneshotruntime

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
)

// Readiness is an explicit pre-dispatch wait. Capability construction and
// readiness consumption are separate because phase order is not a dependency.
type Readiness string

const (
	ReadinessSearch    Readiness = "search"
	ReadinessSemantic  Readiness = "semantic"
	ReadinessCodeIndex Readiness = "code_index"
	ReadinessNoteCache Readiness = "note_cache"
	ReadinessSession   Readiness = "session"
)

type StoreAccess string

const (
	StoreNone             StoreAccess = ""
	StoreExistingReadOnly StoreAccess = "existing_read_only"
	StoreLiveReadWrite    StoreAccess = "live_read_write"
)

type CodeIndexFreshness string

const (
	CodeIndexFreshnessNone    CodeIndexFreshness = ""
	CodeIndexFreshnessCurrent CodeIndexFreshness = "current"
)

type NoteState string

const (
	NoteStateNone          NoteState = ""
	NoteStateSelectedFiles NoteState = "selected_files"
	NoteStateCacheSnapshot NoteState = "cache_snapshot"
	NoteStateLive          NoteState = "live"
)

type OntologyState string

const (
	OntologyStateNone              OntologyState = ""
	OntologyStateSchema            OntologyState = "schema"
	OntologyStatePersisted         OntologyState = "persisted"
	OntologyStateCurrentProjection OntologyState = "current_projection"
)

type SessionPolicy string

const (
	SessionNone SessionPolicy = ""
	// SessionExistingOnly permits optional dedupe state through a narrow existing-only
	// writer without requiring the CodeIndex reader that shares its SQLite file.
	SessionExistingOnly SessionPolicy = "existing_only"
	SessionLiveWrite    SessionPolicy = "live_write"
)

type Authority string

const (
	AuthorityReadOnly     Authority = ""
	AuthorityPlanOnly     Authority = "plan_only"
	AuthorityWriteCapable Authority = "write_capable"
)

type DiagnosticsPolicy string

const (
	DiagnosticsDisabled DiagnosticsPolicy = ""
	DiagnosticsEnabled  DiagnosticsPolicy = "enabled"
)

type UnavailablePolicy string

const (
	UnavailableFail       UnavailablePolicy = ""
	UnavailableStructured UnavailablePolicy = "structured"
	UnavailableDegrade    UnavailablePolicy = "degrade"
)

// Plan is a closed, declarative contract for one-shot runtime composition.
// Its capability slice always compiles through RequireRuntimeCapabilities, so
// an empty plan means no capabilities rather than bootstrap's full zero value.
type Plan struct {
	// FullRuntime is an explicit exemption for a front whose output parity still
	// requires bootstrap's long-lived capability set. It is never the implicit
	// zero-value fallback for ordinary one-shot plans.
	FullRuntime          bool
	Capabilities         []bootstrap.RuntimeCapability
	Readiness            []Readiness
	StoreAccess          StoreAccess
	CodeIndexFreshness   CodeIndexFreshness
	NoteState            NoteState
	OntologyState        OntologyState
	Session              SessionPolicy
	Diagnostics          DiagnosticsPolicy
	DiagnosticsNamespace string
	Unavailable          UnavailablePolicy
}

func (p Plan) RuntimeRequirements() bootstrap.RuntimeRequirements {
	if p.FullRuntime {
		return bootstrap.RuntimeRequirements{}
	}
	return bootstrap.RequireRuntimeCapabilities(p.Capabilities...)
}

func (p Plan) Validate() error {
	return p.ValidateForAuthority(AuthorityReadOnly)
}

func (p Plan) ValidateForAuthority(authority Authority) error {
	capabilities := make(map[bootstrap.RuntimeCapability]struct{}, len(p.Capabilities))
	for _, capability := range p.Capabilities {
		if !knownCapability(capability) {
			return fmt.Errorf("unknown runtime capability %q", capability)
		}
		if _, duplicate := capabilities[capability]; duplicate {
			return fmt.Errorf("duplicate runtime capability %q", capability)
		}
		capabilities[capability] = struct{}{}
	}

	readiness := make(map[Readiness]struct{}, len(p.Readiness))
	for _, wait := range p.Readiness {
		if !knownReadiness(wait) {
			return fmt.Errorf("unknown readiness %q", wait)
		}
		if _, duplicate := readiness[wait]; duplicate {
			return fmt.Errorf("duplicate readiness %q", wait)
		}
		readiness[wait] = struct{}{}
		if capability, ok := readinessCapability(wait); ok {
			if _, present := capabilities[capability]; !present {
				return fmt.Errorf("%s readiness requires %s capability", wait, capability)
			}
		}
	}

	if !knownStoreAccess(p.StoreAccess) {
		return fmt.Errorf("unknown store access %q", p.StoreAccess)
	}
	if p.CodeIndexFreshness != CodeIndexFreshnessNone && p.CodeIndexFreshness != CodeIndexFreshnessCurrent {
		return fmt.Errorf("unknown code-index freshness %q", p.CodeIndexFreshness)
	}
	if !knownNoteState(p.NoteState) {
		return fmt.Errorf("unknown note state %q", p.NoteState)
	}
	if !knownOntologyState(p.OntologyState) {
		return fmt.Errorf("unknown ontology state %q", p.OntologyState)
	}
	if !knownSessionPolicy(p.Session) {
		return fmt.Errorf("unknown session policy %q", p.Session)
	}
	if !knownAuthority(authority) {
		return fmt.Errorf("unknown authority %q", authority)
	}
	if p.Diagnostics != DiagnosticsDisabled && p.Diagnostics != DiagnosticsEnabled {
		return fmt.Errorf("unknown diagnostics policy %q", p.Diagnostics)
	}
	if p.Diagnostics == DiagnosticsEnabled && p.DiagnosticsNamespace == "" {
		return fmt.Errorf("enabled diagnostics require a namespace")
	}
	if p.Diagnostics == DiagnosticsDisabled && p.DiagnosticsNamespace != "" {
		return fmt.Errorf("disabled diagnostics cannot declare a namespace")
	}
	if p.Unavailable != UnavailableFail && p.Unavailable != UnavailableStructured && p.Unavailable != UnavailableDegrade {
		return fmt.Errorf("unknown unavailable policy %q", p.Unavailable)
	}

	if authority == AuthorityWriteCapable && p.StoreAccess == StoreExistingReadOnly {
		return fmt.Errorf("write authority cannot use an existing read-only store")
	}
	if p.CodeIndexFreshness == CodeIndexFreshnessCurrent {
		if _, ok := capabilities[bootstrap.RuntimeCapabilityCodeIndex]; !ok {
			return fmt.Errorf("current code-index freshness requires code-index capability")
		}
		if p.StoreAccess != StoreExistingReadOnly {
			return fmt.Errorf("current code-index freshness requires existing read-only store access")
		}
	}
	if p.Session == SessionLiveWrite && p.StoreAccess != StoreLiveReadWrite {
		return fmt.Errorf("live session writes require live read-write store access")
	}
	if p.NoteState == NoteStateCacheSnapshot {
		if _, ok := capabilities[bootstrap.RuntimeCapabilitySearch]; !ok {
			return fmt.Errorf("cache snapshot requires search capability")
		}
		if _, ok := readiness[ReadinessSearch]; !ok {
			return fmt.Errorf("cache snapshot requires search readiness")
		}
		if _, ok := readiness[ReadinessNoteCache]; !ok {
			return fmt.Errorf("cache snapshot requires note-cache readiness")
		}
	}
	if p.OntologyState == OntologyStatePersisted && p.StoreAccess == StoreNone {
		return fmt.Errorf("persisted ontology requires store access")
	}
	if p.OntologyState == OntologyStateCurrentProjection && p.StoreAccess != StoreLiveReadWrite {
		return fmt.Errorf("current ontology projection requires live read-write store access")
	}
	return nil
}

// Runtime is the narrow construction/wait seam used by one-shot composition.
// A production adapter may wrap LiveRuntime; tests can provide deterministic
// blocking fakes without adding hooks to bootstrap phases.
type Runtime interface {
	WaitForSearch(context.Context) error
	WaitForSemantic(context.Context) error
	WaitForCodeIndex(context.Context) error
	WaitForNoteCache(context.Context) error
	WaitForSession(context.Context) error
	Close()
}

type Factory func(context.Context, bootstrap.RuntimeRequirements) (Runtime, error)

// Build validates and constructs the runtime declared by plan without waiting
// for readiness. Callers that own an observable timing phase use Await under
// that phase, then retain ownership of the returned runtime until Close.
func Build(ctx context.Context, plan Plan, factory Factory) (Runtime, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if !plan.requiresRuntime() {
		return nil, nil
	}
	if factory == nil {
		return nil, fmt.Errorf("runtime factory is required for a runtime-backed plan")
	}
	runtime, err := factory(ctx, plan.RuntimeRequirements())
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		return nil, fmt.Errorf("runtime factory returned nil for a runtime-backed plan")
	}
	return runtime, nil
}

// BuildAndAwait is the default one-shot composition path. A structured
// unavailable result returns the still-open runtime to the caller, which must
// close it after converting the readiness error into its response envelope.
func BuildAndAwait(ctx context.Context, plan Plan, factory Factory) (Runtime, error) {
	runtime, err := Build(ctx, plan, factory)
	if err != nil {
		return nil, err
	}
	if err := Await(ctx, runtime, plan); err != nil {
		if plan.Unavailable == UnavailableStructured {
			return runtime, err
		}
		if runtime != nil {
			runtime.Close()
		}
		return nil, err
	}
	return runtime, nil
}

func (p Plan) requiresRuntime() bool {
	return p.FullRuntime || len(p.Capabilities) > 0 || len(p.Readiness) > 0 || p.StoreAccess != StoreNone || p.Session != SessionNone
}

// RequiresRuntime reports whether the plan can construct a runtime. It is an
// inspection accessor for bounded command fronts; composition stays here.
func (p Plan) RequiresRuntime() bool {
	return p.requiresRuntime()
}

func Await(ctx context.Context, runtime Runtime, plan Plan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if len(plan.Readiness) == 0 {
		return nil
	}
	if runtime == nil {
		return fmt.Errorf("runtime is required for declared readiness")
	}
	for _, wait := range plan.Readiness {
		var err error
		switch wait {
		case ReadinessSearch:
			err = runtime.WaitForSearch(ctx)
		case ReadinessSemantic:
			err = runtime.WaitForSemantic(ctx)
		case ReadinessCodeIndex:
			err = runtime.WaitForCodeIndex(ctx)
		case ReadinessNoteCache:
			err = runtime.WaitForNoteCache(ctx)
		case ReadinessSession:
			err = runtime.WaitForSession(ctx)
		}
		if err != nil {
			return fmt.Errorf("await %s: %w", wait, err)
		}
	}
	return nil
}

func knownCapability(capability bootstrap.RuntimeCapability) bool {
	switch capability {
	case bootstrap.RuntimeCapabilitySearch,
		bootstrap.RuntimeCapabilitySemantic,
		bootstrap.RuntimeCapabilityCodeIndex,
		bootstrap.RuntimeCapabilityCodeAnchorWarmup,
		bootstrap.RuntimeCapabilityCodeEmbeddings,
		bootstrap.RuntimeCapabilityLeaderSyncers,
		bootstrap.RuntimeCapabilityCodeRefDiscovery:
		return true
	default:
		return false
	}
}

func knownReadiness(readiness Readiness) bool {
	switch readiness {
	case ReadinessSearch, ReadinessSemantic, ReadinessCodeIndex, ReadinessNoteCache, ReadinessSession:
		return true
	default:
		return false
	}
}

func readinessCapability(readiness Readiness) (bootstrap.RuntimeCapability, bool) {
	switch readiness {
	case ReadinessSearch, ReadinessNoteCache:
		return bootstrap.RuntimeCapabilitySearch, true
	case ReadinessSemantic:
		return bootstrap.RuntimeCapabilitySemantic, true
	case ReadinessCodeIndex:
		return bootstrap.RuntimeCapabilityCodeIndex, true
	default:
		return "", false
	}
}

func knownStoreAccess(value StoreAccess) bool {
	return value == StoreNone || value == StoreExistingReadOnly || value == StoreLiveReadWrite
}

func knownNoteState(value NoteState) bool {
	return value == NoteStateNone || value == NoteStateSelectedFiles || value == NoteStateCacheSnapshot || value == NoteStateLive
}

func knownOntologyState(value OntologyState) bool {
	return value == OntologyStateNone || value == OntologyStateSchema || value == OntologyStatePersisted || value == OntologyStateCurrentProjection
}

func knownSessionPolicy(value SessionPolicy) bool {
	return value == SessionNone || value == SessionExistingOnly || value == SessionLiveWrite
}

func knownAuthority(value Authority) bool {
	return value == AuthorityReadOnly || value == AuthorityPlanOnly || value == AuthorityWriteCapable
}
