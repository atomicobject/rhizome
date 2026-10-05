package codeanchor

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ExternalEcosystem identifies the public naming system for an external target.
// It is intentionally independent of the source language that emitted evidence.
type ExternalEcosystem string

const (
	ExternalEcosystemNPM    ExternalEcosystem = "npm"
	ExternalEcosystemPython ExternalEcosystem = "python"
)

// ExternalTargetKind distinguishes identities that share a module and symbol path.
type ExternalTargetKind string

const (
	ExternalTargetModule         ExternalTargetKind = "module"
	ExternalTargetSymbol         ExternalTargetKind = "symbol"
	ExternalTargetType           ExternalTargetKind = "type"
	ExternalTargetRuntimeBuiltin ExternalTargetKind = "runtime_builtin"
	ExternalTargetRuntimeGlobal  ExternalTargetKind = "runtime_global"
)

// ExternalTargetIdentity is the complete, language-neutral identity of an
// external endpoint. Dependency versions and source aliases are deliberately
// absent: they are association evidence, not identity.
type ExternalTargetIdentity struct {
	Ecosystem  ExternalEcosystem
	Module     string
	SymbolPath string
	Kind       ExternalTargetKind
}

// ExternalTarget is a canonical external endpoint suitable for catalog storage.
type ExternalTarget struct {
	ExternalTargetIdentity
	Handle string
}

// ExternalTargetID identifies one canonical catalog row. RawTargetID identifies
// one normalized parser target; many raw targets may map to one external target.
type ExternalTargetID int64
type RawTargetID int64

// RawExternalTargetMapping encodes the raw-target N:1 external-target mapping.
// A store should make RawTargetID unique so one raw target maps to at most one
// canonical external identity.
type RawExternalTargetMapping struct {
	RawTargetID RawTargetID
	ExternalID  ExternalTargetID
}

// ExternalTargetRow is the persistence DTO for one canonical catalog entry.
type ExternalTargetRow struct {
	ID ExternalTargetID
	ExternalTarget
}

// ExternalTargetOption describes a reason a syntactically package-like target
// must not be promoted to an external identity.
type ExternalTargetOption uint8

const (
	// ExternalTargetWorkspaceOrPathAlias excludes unresolved repository workspace
	// packages and tsconfig/jsconfig aliases from external classification.
	ExternalTargetWorkspaceOrPathAlias ExternalTargetOption = iota + 1
)

var ErrExternalTargetExcluded = errors.New("external target excluded by local-resolution evidence")

// NewExternalTarget validates and canonicalizes a language-neutral external
// identity. Callers must establish qualifying evidence before constructing it.
func NewExternalTarget(identity ExternalTargetIdentity, options ...ExternalTargetOption) (ExternalTarget, error) {
	for _, option := range options {
		if option == ExternalTargetWorkspaceOrPathAlias {
			return ExternalTarget{}, ErrExternalTargetExcluded
		}
	}

	identity.Ecosystem = ExternalEcosystem(strings.TrimSpace(string(identity.Ecosystem)))
	identity.Module = strings.TrimSpace(identity.Module)
	identity.SymbolPath = strings.TrimSpace(identity.SymbolPath)
	identity.Kind = ExternalTargetKind(strings.TrimSpace(string(identity.Kind)))
	if identity.Ecosystem == "" || identity.Module == "" || identity.Kind == "" {
		return ExternalTarget{}, errors.New("external target requires ecosystem, module, and kind")
	}
	if !identity.Kind.Valid() {
		return ExternalTarget{}, fmt.Errorf("unsupported external target kind %q", identity.Kind)
	}
	if identity.Kind == ExternalTargetModule && identity.SymbolPath != "" {
		return ExternalTarget{}, errors.New("module target must not have a symbol path")
	}
	if identity.Kind != ExternalTargetModule && identity.Kind != ExternalTargetRuntimeBuiltin && identity.SymbolPath == "" {
		return ExternalTarget{}, errors.New("non-module external target requires symbol path")
	}
	if identity.Ecosystem == ExternalEcosystemNPM {
		identity.Module = canonicalNPMModule(identity.Module)
	}
	if identity.Module == "" || identity.Module == "node:" {
		return ExternalTarget{}, errors.New("external target requires a non-empty canonical module")
	}

	return ExternalTarget{
		ExternalTargetIdentity: identity,
		Handle:                 externalTargetHandle(identity),
	}, nil
}

func (kind ExternalTargetKind) Valid() bool {
	switch kind {
	case ExternalTargetModule, ExternalTargetSymbol, ExternalTargetType, ExternalTargetRuntimeBuiltin, ExternalTargetRuntimeGlobal:
		return true
	default:
		return false
	}
}

// CanonicalizeExternalTarget revalidates a catalog DTO at a store boundary and
// regenerates its handle from identity. It deliberately ignores any incoming
// handle so callers cannot persist a stale or noncanonical identity/handle pair.
func CanonicalizeExternalTarget(target ExternalTarget) (ExternalTarget, error) {
	return NewExternalTarget(target.ExternalTargetIdentity)
}

func externalTargetHandle(identity ExternalTargetIdentity) string {
	// Escaped, labeled components make the handle deterministic without ambiguous
	// delimiter parsing. Version provenance is intentionally not accepted here.
	return fmt.Sprintf("external:ecosystem=%s&module=%s&symbol=%s&kind=%s",
		url.QueryEscape(string(identity.Ecosystem)),
		url.QueryEscape(identity.Module),
		url.QueryEscape(identity.SymbolPath),
		url.QueryEscape(string(identity.Kind)),
	)
}

func canonicalNPMModule(module string) string {
	module = strings.TrimSpace(module)
	if strings.HasPrefix(module, "node:") {
		return module
	}
	if _, ok := nodeBuiltinModules[module]; ok {
		return "node:" + module
	}
	return module
}

// nodeBuiltinModules is identity normalization, not runtime classification.
// Classification still requires explicit runtime/import evidence.
var nodeBuiltinModules = map[string]struct{}{
	"assert": {}, "assert/strict": {}, "async_hooks": {}, "buffer": {},
	"child_process": {}, "cluster": {}, "console": {}, "constants": {},
	"crypto": {}, "dgram": {}, "diagnostics_channel": {}, "dns": {},
	"dns/promises": {}, "domain": {}, "events": {}, "fs": {},
	"fs/promises": {}, "http": {}, "http2": {}, "https": {}, "module": {},
	"net": {}, "os": {}, "path": {}, "path/posix": {}, "path/win32": {},
	"perf_hooks": {}, "process": {}, "punycode": {}, "querystring": {},
	"readline": {}, "readline/promises": {}, "repl": {}, "stream": {},
	"stream/consumers": {}, "stream/promises": {}, "stream/web": {},
	"string_decoder": {}, "sys": {}, "timers": {}, "timers/promises": {},
	"tls": {}, "trace_events": {}, "tty": {}, "url": {}, "util": {},
	"util/types": {}, "v8": {}, "vm": {}, "wasi": {}, "worker_threads": {},
	"zlib": {},
}

func isNodeBuiltinModule(module string) bool {
	module = strings.TrimSpace(module)
	if strings.HasPrefix(module, "node:") {
		return strings.TrimPrefix(module, "node:") != ""
	}
	_, ok := nodeBuiltinModules[module]
	return ok
}

// ExternalEvidenceKind records the immutable syntactic/runtime fact that
// qualified one association for external classification.
type ExternalEvidenceKind string

const (
	ExternalEvidenceESMNamed       ExternalEvidenceKind = "esm_named"
	ExternalEvidenceESMDefault     ExternalEvidenceKind = "esm_default"
	ExternalEvidenceESMNamespace   ExternalEvidenceKind = "esm_namespace"
	ExternalEvidenceESMType        ExternalEvidenceKind = "esm_type"
	ExternalEvidenceESMSideEffect  ExternalEvidenceKind = "esm_side_effect"
	ExternalEvidenceCJSNamed       ExternalEvidenceKind = "cjs_named"
	ExternalEvidenceCJSDefault     ExternalEvidenceKind = "cjs_default"
	ExternalEvidenceCJSNamespace   ExternalEvidenceKind = "cjs_namespace"
	ExternalEvidenceCJSType        ExternalEvidenceKind = "cjs_type"
	ExternalEvidenceCJSSideEffect  ExternalEvidenceKind = "cjs_side_effect"
	ExternalEvidenceRuntimeBuiltin ExternalEvidenceKind = "runtime_builtin"
	ExternalEvidenceRuntimeGlobal  ExternalEvidenceKind = "runtime_global"
)

// ExternalConfidence records classifier certainty without making it target-wide.
type ExternalConfidence string

const (
	ExternalConfidenceLow    ExternalConfidence = "low"
	ExternalConfidenceMedium ExternalConfidence = "medium"
	ExternalConfidenceHigh   ExternalConfidence = "high"
)

// ExternalVersionScope identifies the nearest package.json dependency section.
type ExternalVersionScope string

const (
	ExternalVersionDependency         ExternalVersionScope = "dependencies"
	ExternalVersionDevDependency      ExternalVersionScope = "devDependencies"
	ExternalVersionPeerDependency     ExternalVersionScope = "peerDependencies"
	ExternalVersionOptionalDependency ExternalVersionScope = "optionalDependencies"
)

// ExternalVersionProvenance preserves an optional declared package range. It is
// association evidence and must never participate in ExternalTargetIdentity.
type ExternalVersionProvenance struct {
	ManifestPath  string
	DeclaredRange string
	Scope         ExternalVersionScope
}

// ExternalEvidence is shared by sparse symbol and import association rows.
type ExternalEvidence struct {
	Kind         ExternalEvidenceKind
	Confidence   ExternalConfidence
	ImportedName string
	LocalName    string
	Version      *ExternalVersionProvenance
}

// SymbolRefAssociationKey identifies one durable symbol-reference association.
// RawTargetID joins the existing normalized raw target substrate.
type SymbolRefAssociationKey struct {
	SrcPath     string
	OwnerFQN    string
	RefKind     RefKind
	RawTargetID RawTargetID
}

// ExternalSymbolAssociationEvidence is sparse: only associations with qualifying
// import/runtime evidence need a row.
type ExternalSymbolAssociationEvidence struct {
	Association SymbolRefAssociationKey
	ExternalID  ExternalTargetID
	Evidence    ExternalEvidence
}

// ExternalImportAssociationKey identifies an external import binding or an
// import-only module. BindingOrdinal disambiguates bindings from the same module.
type ExternalImportAssociationKey struct {
	SrcPath        string
	Module         string
	BindingOrdinal int
}

// ExternalImportEvidence is separate from the local-module ImportRefRow family.
type ExternalImportEvidence struct {
	Association ExternalImportAssociationKey
	ExternalID  ExternalTargetID
	Evidence    ExternalEvidence
}

// ExternalReferenceClass is derived at read/statistics time and is not a field on
// any persistence DTO. Local resolution takes precedence in the classifier.
type ExternalReferenceClass string

const (
	ExternalClassLocalResolved        ExternalReferenceClass = "local_resolved"
	ExternalClassExternalClassified   ExternalReferenceClass = "external_classified"
	ExternalClassRuntimeGlobalBuiltin ExternalReferenceClass = "runtime_global_builtin"
	ExternalClassUnknown              ExternalReferenceClass = "unknown"
)

// DeriveExternalReferenceClass applies the read-time precedence contract. A
// current source-backed resolution always wins; qualifying package or runtime
// evidence follows; associations without either remain unknown.
func DeriveExternalReferenceClass(localResolved bool, targetKind ExternalTargetKind, evidence *ExternalEvidence) ExternalReferenceClass {
	if localResolved {
		return ExternalClassLocalResolved
	}
	if evidence == nil {
		return ExternalClassUnknown
	}
	if targetKind == ExternalTargetRuntimeBuiltin || targetKind == ExternalTargetRuntimeGlobal {
		return ExternalClassRuntimeGlobalBuiltin
	}
	switch evidence.Kind {
	case ExternalEvidenceRuntimeBuiltin, ExternalEvidenceRuntimeGlobal:
		return ExternalClassRuntimeGlobalBuiltin
	case ExternalEvidenceESMNamed,
		ExternalEvidenceESMDefault,
		ExternalEvidenceESMNamespace,
		ExternalEvidenceESMType,
		ExternalEvidenceESMSideEffect,
		ExternalEvidenceCJSNamed,
		ExternalEvidenceCJSDefault,
		ExternalEvidenceCJSNamespace,
		ExternalEvidenceCJSType,
		ExternalEvidenceCJSSideEffect:
		return ExternalClassExternalClassified
	default:
		return ExternalClassUnknown
	}
}

// ExternalSymbolPath canonicalizes binding syntax into public symbol identity.
// Namespace and named access converge when passed the same exported member;
// default bindings always use the public symbol path "default".
func ExternalSymbolPath(kind ExternalEvidenceKind, importedMember string) string {
	switch kind {
	case ExternalEvidenceESMDefault, ExternalEvidenceCJSDefault:
		return "default"
	default:
		return strings.TrimSpace(importedMember)
	}
}
