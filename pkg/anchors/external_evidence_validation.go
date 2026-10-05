package codeanchor

import "fmt"

func (kind ExternalEvidenceKind) Valid() bool {
	switch kind {
	case ExternalEvidenceESMNamed, ExternalEvidenceESMDefault, ExternalEvidenceESMNamespace,
		ExternalEvidenceESMType, ExternalEvidenceESMSideEffect,
		ExternalEvidenceCJSNamed, ExternalEvidenceCJSDefault, ExternalEvidenceCJSNamespace,
		ExternalEvidenceCJSType, ExternalEvidenceCJSSideEffect,
		ExternalEvidenceRuntimeBuiltin, ExternalEvidenceRuntimeGlobal:
		return true
	default:
		return false
	}
}

func (confidence ExternalConfidence) Valid() bool {
	switch confidence {
	case ExternalConfidenceLow, ExternalConfidenceMedium, ExternalConfidenceHigh:
		return true
	default:
		return false
	}
}

func (scope ExternalVersionScope) Valid() bool {
	switch scope {
	case ExternalVersionDependency, ExternalVersionDevDependency, ExternalVersionPeerDependency, ExternalVersionOptionalDependency:
		return true
	default:
		return false
	}
}

// ValidateExternalEvidence keeps contradictory classifier facts out of the
// durable sparse-evidence layer. It intentionally does not infer evidence.
func ValidateExternalEvidence(target ExternalTarget, evidence ExternalEvidence) error {
	if !evidence.Kind.Valid() {
		return fmt.Errorf("unsupported external evidence kind %q", evidence.Kind)
	}
	if !evidence.Confidence.Valid() {
		return fmt.Errorf("unsupported external confidence %q", evidence.Confidence)
	}
	if evidence.Version != nil && !evidence.Version.Scope.Valid() {
		return fmt.Errorf("unsupported external version scope %q", evidence.Version.Scope)
	}

	compatible := false
	switch evidence.Kind {
	case ExternalEvidenceRuntimeBuiltin:
		compatible = target.Kind == ExternalTargetRuntimeBuiltin
	case ExternalEvidenceRuntimeGlobal:
		compatible = target.Kind == ExternalTargetRuntimeGlobal
	case ExternalEvidenceESMSideEffect, ExternalEvidenceCJSSideEffect:
		compatible = target.Kind == ExternalTargetModule || target.Kind == ExternalTargetRuntimeBuiltin
	case ExternalEvidenceESMType, ExternalEvidenceCJSType:
		compatible = target.Kind == ExternalTargetSymbol || target.Kind == ExternalTargetType || target.Kind == ExternalTargetRuntimeBuiltin
	default:
		compatible = target.Kind == ExternalTargetModule || target.Kind == ExternalTargetSymbol || target.Kind == ExternalTargetRuntimeBuiltin
	}
	if !compatible {
		return fmt.Errorf("external evidence kind %q is incompatible with target kind %q", evidence.Kind, target.Kind)
	}
	return nil
}
