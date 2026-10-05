package identifierreconcile

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/reference"
)

func validateFieldDiscoveryMembership(discovery *identifierFieldDiscoverySnapshot, rewrites []reference.IdentifierRewrite) error {
	canonical, err := canonicalRepairRewriteSet(rewrites)
	if err != nil {
		return err
	}
	for _, edit := range discovery.Edits {
		if _, ok := fieldEditRewrite(edit, canonical); !ok {
			return fmt.Errorf("field discovery edit does not match semantic rewrite membership")
		}
	}
	for _, diagnostic := range discovery.Diagnostics {
		if !fieldDiagnosticMatchesRewrites(diagnostic, canonical) {
			return fmt.Errorf("field discovery diagnostic does not match semantic rewrite union")
		}
	}
	previous := ""
	for _, field := range discovery.SynthesizedDerived {
		key := repairRefKey(field.OwnerRef) + "\x00" + field.FieldName
		if key <= previous {
			return fmt.Errorf("synthesized derived fields must be unique and canonically sorted")
		}
		previous = key
		matched := false
		for _, rewrite := range canonical {
			if !rewrite.DerivedFrom.IsZero() && hasSynthesizedDerivedObligation([]synthesizedDerivedField{field}, rewrite) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("synthesized derived field does not match semantic rewrite union")
		}
	}
	return nil
}

func fieldDiagnosticMatchesRewrites(diagnostic reference.IdentifierRewriteDiagnostic, rewrites []reference.IdentifierRewrite) bool {
	for _, rewrite := range rewrites {
		if !diagnostic.OwnerRef.IsZero() && (sameRepairRef(diagnostic.OwnerRef, rewrite.OldRef) || sameRepairRef(diagnostic.OwnerRef, rewrite.NewRef)) {
			return true
		}
		for _, candidate := range diagnostic.Candidates {
			if sameRepairRef(candidate, rewrite.OldRef) || sameRepairRef(candidate, rewrite.NewRef) {
				return true
			}
		}
		if diagnosticMentionsIdentifier(diagnostic.Value, rewrite.OldIdentifier) {
			return true
		}
	}
	return false
}

func linkDiagnosticMatchesRewrites(diagnostic reference.LinkRewriteDiagnostic, rewrites []reference.IdentifierRewrite) bool {
	for _, rewrite := range rewrites {
		for _, candidate := range diagnostic.Candidates {
			if sameRepairRef(candidate, rewrite.OldRef) || sameRepairRef(candidate, rewrite.NewRef) {
				return true
			}
		}
		if diagnosticMentionsIdentifier(diagnostic.Target, rewrite.OldIdentifier) {
			return true
		}
	}
	return false
}

func diagnosticMentionsIdentifier(value, identifier string) bool {
	value = IdentifierComparisonKey(value)
	identifier = IdentifierComparisonKey(identifier)
	if value == "" || identifier == "" {
		return false
	}
	for offset := 0; offset <= len(value)-len(identifier); {
		relative := strings.Index(value[offset:], identifier)
		if relative < 0 {
			return false
		}
		start := offset + relative
		end := start + len(identifier)
		if repairIdentifierBoundary(value, start, end) {
			return true
		}
		offset = end
	}
	return false
}

func validateSourcePreconditions(input []SourcePrecondition) error {
	previous := ""
	for _, precondition := range input {
		notePath, err := canonicalRepairPath(precondition.NotePath)
		decoded, hashErr := hex.DecodeString(precondition.SourceHash)
		if err != nil || notePath != precondition.NotePath || hashErr != nil || len(decoded) != 32 || !validSHA256Fingerprint(precondition.SourceHash) {
			return fmt.Errorf("source precondition requires canonical path and exact hash")
		}
		if precondition.NotePath <= previous {
			return fmt.Errorf("source preconditions must be unique and canonically sorted")
		}
		previous = precondition.NotePath
	}
	return nil
}

func validateRepairSourceCoverage(assembly *RepairAssembly) error {
	covered := make(map[string]string, len(assembly.SourcePreconditions))
	for _, precondition := range assembly.SourcePreconditions {
		covered[precondition.NotePath] = precondition.SourceHash
	}
	for _, component := range assembly.Components {
		for _, intent := range append(append([]FieldRepairIntent(nil), component.FieldEdits...), component.AliasEdits...) {
			if hash, ok := covered[intent.Edit.OwnerRef.NotePath]; !ok || intent.SourceHash != hash {
				return fmt.Errorf("field edit source %s lacks an exact source precondition", intent.Edit.OwnerRef.NotePath)
			}
		}
		for _, intent := range component.LinkEdits {
			if hash, ok := covered[intent.Edit.NotePath]; !ok || intent.SourceHash != hash {
				return fmt.Errorf("link edit source %s lacks an exact source precondition", intent.Edit.NotePath)
			}
		}
		for _, intent := range component.Moves {
			if hash, ok := covered[intent.Move.SourcePath]; !ok || intent.SourceHash != hash {
				return fmt.Errorf("move source %s lacks an exact source precondition", intent.Move.SourcePath)
			}
		}
	}
	return nil
}

func bindIntentSourceHashes(assembly *RepairAssembly) error {
	byPath := make(map[string]string, len(assembly.SourcePreconditions))
	for _, precondition := range assembly.SourcePreconditions {
		byPath[precondition.NotePath] = precondition.SourceHash
	}
	for componentIndex := range assembly.Components {
		component := &assembly.Components[componentIndex]
		for index := range component.FieldEdits {
			component.FieldEdits[index].SourceHash = byPath[component.FieldEdits[index].Edit.OwnerRef.NotePath]
		}
		for index := range component.AliasEdits {
			component.AliasEdits[index].SourceHash = byPath[component.AliasEdits[index].Edit.OwnerRef.NotePath]
		}
		for index := range component.LinkEdits {
			component.LinkEdits[index].SourceHash = byPath[component.LinkEdits[index].Edit.NotePath]
		}
		for index := range component.Moves {
			component.Moves[index].SourceHash = byPath[component.Moves[index].Move.SourcePath]
		}
	}
	return validateRepairSourceCoverage(assembly)
}
