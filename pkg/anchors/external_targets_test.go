package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewExternalTargetCanonicalizesNPMIdentity(t *testing.T) {
	tests := []struct {
		name       string
		module     string
		symbolPath string
		kind       ExternalTargetKind
		wantModule string
		wantSymbol string
	}{
		{name: "scoped root", module: "@scope/pkg", symbolPath: "render", kind: ExternalTargetSymbol, wantModule: "@scope/pkg", wantSymbol: "render"},
		{name: "scoped subpath", module: "@scope/pkg/server", symbolPath: "render", kind: ExternalTargetSymbol, wantModule: "@scope/pkg/server", wantSymbol: "render"},
		{name: "unscoped subpath", module: "react/jsx-runtime", symbolPath: "jsx", kind: ExternalTargetSymbol, wantModule: "react/jsx-runtime", wantSymbol: "jsx"},
		{name: "node builtin bare", module: "path", symbolPath: "join", kind: ExternalTargetSymbol, wantModule: "node:path", wantSymbol: "join"},
		{name: "node builtin explicit", module: "node:path", symbolPath: "join", kind: ExternalTargetSymbol, wantModule: "node:path", wantSymbol: "join"},
		{name: "default import", module: "react", symbolPath: ExternalSymbolPath(ExternalEvidenceESMDefault, "React"), kind: ExternalTargetSymbol, wantModule: "react", wantSymbol: "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := NewExternalTarget(ExternalTargetIdentity{
				Ecosystem:  ExternalEcosystemNPM,
				Module:     tt.module,
				SymbolPath: tt.symbolPath,
				Kind:       tt.kind,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantModule, target.Module)
			require.Equal(t, tt.wantSymbol, target.SymbolPath)
			require.NotEmpty(t, target.Handle)
		})
	}
}

func TestExternalTargetIdentityConvergesAliasesAndExcludesVersion(t *testing.T) {
	named, err := NewExternalTarget(ExternalTargetIdentity{
		Ecosystem: ExternalEcosystemNPM, Module: "node:path",
		SymbolPath: ExternalSymbolPath(ExternalEvidenceESMNamed, "join"), Kind: ExternalTargetSymbol,
	})
	require.NoError(t, err)
	namespace, err := NewExternalTarget(ExternalTargetIdentity{
		Ecosystem: ExternalEcosystemNPM, Module: "path",
		SymbolPath: ExternalSymbolPath(ExternalEvidenceESMNamespace, "join"), Kind: ExternalTargetSymbol,
	})
	require.NoError(t, err)

	require.Equal(t, named, namespace)
	require.Equal(t, named.Handle, namespace.Handle)

}

func TestExternalTargetHandleIsDeterministicAndUnambiguous(t *testing.T) {
	one, err := NewExternalTarget(ExternalTargetIdentity{
		Ecosystem: "custom", Module: "a/b", SymbolPath: "c", Kind: ExternalTargetSymbol,
	})
	require.NoError(t, err)
	two, err := NewExternalTarget(ExternalTargetIdentity{
		Ecosystem: "custom", Module: "a", SymbolPath: "b/c", Kind: ExternalTargetSymbol,
	})
	require.NoError(t, err)
	require.NotEqual(t, one.Handle, two.Handle)

	again, err := NewExternalTarget(one.ExternalTargetIdentity)
	require.NoError(t, err)
	require.Equal(t, one.Handle, again.Handle)
}

func TestNewExternalTargetRejectsIncompleteAndWorkspaceClassification(t *testing.T) {
	_, err := NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "", Kind: ExternalTargetModule})
	require.Error(t, err)
	_, err = NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "node:", Kind: ExternalTargetModule})
	require.Error(t, err)
	_, err = NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "node:   ", Kind: ExternalTargetModule})
	require.Error(t, err)

	_, err = NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "@workspace/app", Kind: ExternalTargetModule}, ExternalTargetWorkspaceOrPathAlias)
	require.ErrorIs(t, err, ErrExternalTargetExcluded)

	_, err = NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "react", SymbolPath: "fragment", Kind: ExternalTargetModule})
	require.ErrorContains(t, err, "module target")
	_, err = NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "react", SymbolPath: "useState", Kind: "invented"})
	require.ErrorContains(t, err, "unsupported external target kind")
}

func TestExternalEvidenceValidationRejectsUnsupportedAndIncompatibleVocabulary(t *testing.T) {
	targets := map[ExternalTargetKind]ExternalTarget{}
	for _, kind := range []ExternalTargetKind{ExternalTargetModule, ExternalTargetSymbol, ExternalTargetType, ExternalTargetRuntimeBuiltin, ExternalTargetRuntimeGlobal} {
		symbol := "value"
		if kind == ExternalTargetModule {
			symbol = ""
		}
		target, err := NewExternalTarget(ExternalTargetIdentity{Ecosystem: ExternalEcosystemNPM, Module: "react", SymbolPath: symbol, Kind: kind})
		require.NoError(t, err)
		targets[kind] = target
	}

	require.Error(t, ValidateExternalEvidence(targets[ExternalTargetSymbol], ExternalEvidence{Kind: "invented", Confidence: ExternalConfidenceHigh}))
	require.Error(t, ValidateExternalEvidence(targets[ExternalTargetSymbol], ExternalEvidence{Kind: ExternalEvidenceESMNamed, Confidence: "certain"}))
	require.Error(t, ValidateExternalEvidence(targets[ExternalTargetRuntimeGlobal], ExternalEvidence{Kind: ExternalEvidenceESMNamed, Confidence: ExternalConfidenceHigh}))
	require.Error(t, ValidateExternalEvidence(targets[ExternalTargetSymbol], ExternalEvidence{Kind: ExternalEvidenceRuntimeGlobal, Confidence: ExternalConfidenceHigh}))
	require.Error(t, ValidateExternalEvidence(targets[ExternalTargetSymbol], ExternalEvidence{Kind: ExternalEvidenceESMNamed, Confidence: ExternalConfidenceHigh, Version: &ExternalVersionProvenance{ManifestPath: "package.json", DeclaredRange: "^1", Scope: "invented"}}))
	require.NoError(t, ValidateExternalEvidence(targets[ExternalTargetModule], ExternalEvidence{Kind: ExternalEvidenceESMSideEffect, Confidence: ExternalConfidenceHigh}))
	require.NoError(t, ValidateExternalEvidence(targets[ExternalTargetType], ExternalEvidence{Kind: ExternalEvidenceESMType, Confidence: ExternalConfidenceHigh}))
	require.NoError(t, ValidateExternalEvidence(targets[ExternalTargetRuntimeBuiltin], ExternalEvidence{Kind: ExternalEvidenceRuntimeBuiltin, Confidence: ExternalConfidenceHigh}))
	require.NoError(t, ValidateExternalEvidence(targets[ExternalTargetRuntimeBuiltin], ExternalEvidence{Kind: ExternalEvidenceESMDefault, Confidence: ExternalConfidenceHigh}))
	require.NoError(t, ValidateExternalEvidence(targets[ExternalTargetRuntimeBuiltin], ExternalEvidence{Kind: ExternalEvidenceESMNamed, Confidence: ExternalConfidenceHigh}))
	require.NoError(t, ValidateExternalEvidence(targets[ExternalTargetRuntimeBuiltin], ExternalEvidence{Kind: ExternalEvidenceESMNamespace, Confidence: ExternalConfidenceHigh}))
}

func TestCanonicalizeExternalTargetRevalidatesMutatedDTO(t *testing.T) {
	canonical, err := NewExternalTarget(ExternalTargetIdentity{
		Ecosystem: ExternalEcosystemNPM, Module: "node:path", SymbolPath: "join", Kind: ExternalTargetSymbol,
	})
	require.NoError(t, err)

	mutated := canonical
	mutated.Module = " path "
	mutated.SymbolPath = " join "
	mutated.Handle = "stale-handle"
	revalidated, err := CanonicalizeExternalTarget(mutated)
	require.NoError(t, err)
	require.Equal(t, canonical, revalidated)

	mutated.Module = "node:"
	_, err = CanonicalizeExternalTarget(mutated)
	require.Error(t, err)
}

func TestDeriveExternalReferenceClassKeepsLocalPrecedenceWithoutPersistingClass(t *testing.T) {
	packageKinds := []ExternalEvidenceKind{
		ExternalEvidenceESMNamed, ExternalEvidenceESMDefault, ExternalEvidenceESMNamespace,
		ExternalEvidenceESMType, ExternalEvidenceESMSideEffect,
		ExternalEvidenceCJSNamed, ExternalEvidenceCJSDefault, ExternalEvidenceCJSNamespace,
		ExternalEvidenceCJSType, ExternalEvidenceCJSSideEffect,
	}
	runtimeKinds := []ExternalEvidenceKind{ExternalEvidenceRuntimeBuiltin, ExternalEvidenceRuntimeGlobal}
	for _, kind := range append(append([]ExternalEvidenceKind{}, packageKinds...), runtimeKinds...) {
		evidence := &ExternalEvidence{Kind: kind, Confidence: ExternalConfidenceHigh}
		require.Equal(t, ExternalClassLocalResolved, DeriveExternalReferenceClass(true, ExternalTargetSymbol, evidence), "kind %q", kind)
	}
	for _, kind := range packageKinds {
		evidence := &ExternalEvidence{Kind: kind, Confidence: ExternalConfidenceHigh}
		require.Equal(t, ExternalClassExternalClassified, DeriveExternalReferenceClass(false, ExternalTargetSymbol, evidence), "kind %q", kind)
	}
	for _, kind := range runtimeKinds {
		evidence := &ExternalEvidence{Kind: kind, Confidence: ExternalConfidenceHigh}
		require.Equal(t, ExternalClassRuntimeGlobalBuiltin, DeriveExternalReferenceClass(false, ExternalTargetSymbol, evidence), "kind %q", kind)
	}
	require.Equal(t, ExternalClassUnknown, DeriveExternalReferenceClass(false, ExternalTargetSymbol, nil))
	require.Equal(t, ExternalClassUnknown, DeriveExternalReferenceClass(false, ExternalTargetSymbol, &ExternalEvidence{Kind: "unrecognized"}))
	require.Equal(t, ExternalClassRuntimeGlobalBuiltin, DeriveExternalReferenceClass(false, ExternalTargetRuntimeBuiltin,
		&ExternalEvidence{Kind: ExternalEvidenceESMDefault, Confidence: ExternalConfidenceHigh}))
	// A local definition arriving or disappearing changes the derived view; no
	// classification field exists on the durable evidence DTO to rewrite.
	runtimeEvidence := &ExternalEvidence{Kind: ExternalEvidenceRuntimeGlobal, Confidence: ExternalConfidenceHigh}
	require.Equal(t, ExternalClassLocalResolved, DeriveExternalReferenceClass(true, ExternalTargetRuntimeGlobal, runtimeEvidence))
	require.Equal(t, ExternalClassRuntimeGlobalBuiltin, DeriveExternalReferenceClass(false, ExternalTargetRuntimeGlobal, runtimeEvidence))
}

func TestExternalSymbolPathConvergesNamedAndNamespaceButPreservesDefault(t *testing.T) {
	require.Equal(t, "useState", ExternalSymbolPath(ExternalEvidenceESMNamed, "useState"))
	require.Equal(t, "useState", ExternalSymbolPath(ExternalEvidenceESMNamespace, "useState"))
	require.Equal(t, "default", ExternalSymbolPath(ExternalEvidenceESMDefault, "React"))
	require.Equal(t, "default", ExternalSymbolPath(ExternalEvidenceCJSDefault, "React"))
}
