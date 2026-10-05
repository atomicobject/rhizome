package noteformat

import (
	"bytes"
	"fmt"
	"strings"
)

// RootMetadataChange is one ontology-validated root field mutation expressed
// without provider syntax. A deleted change ignores Value.
type RootMetadataChange struct {
	Key    string
	Value  MetadataValue
	Delete bool
}

// MetadataSourcePatch is one exact, half-open source replacement. Expected is
// copied from the sealed source and is checked again by the journaled writer.
type MetadataSourcePatch struct {
	Range       SourceRange
	Expected    []byte
	Replacement []byte
}

// MetadataPatchPlan is a pure provider result. Providers return ordered,
// non-overlapping patches and never read or write the filesystem.
type MetadataPatchPlan struct {
	Patches []MetadataSourcePatch
}

// RootMetadataPatchPlanner is the format-owned mutation boundary. The source
// and projection must describe the same provider generation.
type RootMetadataPatchPlanner interface {
	PlanRootMetadataPatch(AuthoredSource, Projection, []RootMetadataChange) (MetadataPatchPlan, error)
}

// ValidateMetadataPatchPlan proves source bounds, exact preconditions, and
// deterministic non-overlap before an application adapts the plan to its
// journaled transaction engine.
func ValidateMetadataPatchPlan(source AuthoredSource, plan MetadataPatchPlan) error {
	content := source.Bytes()
	previousEnd := 0
	for index, patch := range plan.Patches {
		if patch.Range.StartByte < 0 || patch.Range.EndByte < patch.Range.StartByte || patch.Range.EndByte > len(content) {
			return fmt.Errorf("metadata patch %d has invalid source range", index)
		}
		if index > 0 && patch.Range.StartByte < previousEnd {
			return fmt.Errorf("metadata patches are unordered or overlap")
		}
		if !bytes.Equal(content[patch.Range.StartByte:patch.Range.EndByte], patch.Expected) {
			return fmt.Errorf("metadata patch %d expected bytes do not match sealed source", index)
		}
		previousEnd = patch.Range.EndByte
	}
	return nil
}

// ValidateRootMetadataChanges rejects ambiguous duplicate keys before provider
// syntax is considered.
func ValidateRootMetadataChanges(changes []RootMetadataChange) error {
	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		key := strings.TrimSpace(change.Key)
		if key == "" {
			return fmt.Errorf("root metadata change key is required")
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate root metadata change %q", key)
		}
		seen[key] = struct{}{}
		if !change.Delete && !change.Value.valid() {
			return fmt.Errorf("root metadata change %q has an invalid value", key)
		}
	}
	return nil
}
