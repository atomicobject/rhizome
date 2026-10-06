package viewconfig

import (
	"encoding/json"
	"fmt"
	"sort"
)

// MatchesMount is shared by catalog resolution and custom-view invocation checks.
func MatchesMount(mount MountSpec, kind MountKind, name string) bool {
	if mount.Kind != kind {
		return false
	}
	switch kind {
	case MountKindType:
		return mount.Type == name || mount.Type == "*"
	case MountKindNode:
		return mount.Type == name
	case MountKindInterface:
		return mount.Interface == name || mount.Interface == "*"
	case MountKindGroup:
		return mount.Group == name || mount.Group == "*"
	case MountKindWorkspace:
		return name == ""
	case MountKindStandalone:
		return true
	default:
		return false
	}
}

// ReplacesGenerated reports whether def takes its target's generated view
// slot. Only native mounts on one type or interface may, never a wildcard;
// validation warns elsewhere.
func ReplacesGenerated(def ViewDefinition) bool {
	return def.Mount.ReplaceGenerated && def.SourceSpec.Kind != SourceKindCustom &&
		((def.Mount.Kind == MountKindType && def.Mount.Type != "*") ||
			(def.Mount.Kind == MountKindInterface && def.Mount.Interface != "*"))
}

func validateMountedDefaults(defs []ViewDefinition, issues []Issue) []Issue {
	invalid := map[string]bool{}
	for _, i := range issues {
		if i.Severity == "" || i.Severity == IssueFatal {
			invalid[i.View] = true
		}
	}
	// Warnings, not fatal: every conflicting view stays selectable and target
	// resolution picks a deterministic winner (order, then ID).
	out := mountConflicts(defs, invalid, func(def ViewDefinition) bool {
		return def.Mount.Default && def.Mount.Kind != MountKindStandalone
	}, "duplicate_mount_default", "mount.default", "multiple views declare a default for the same %s target")
	return append(out, mountConflicts(defs, invalid, ReplacesGenerated,
		"duplicate_replace_generated", "mount.replaceGenerated", "multiple views replace the generated view for the same %s target; the first by order, then ID, replaces it")...)
}

func mountConflicts(defs []ViewDefinition, invalid map[string]bool, claims func(ViewDefinition) bool, code, field, message string) []Issue {
	byTarget := map[string][]ViewDefinition{}
	for _, def := range defs {
		if !claims(def) || def.Generated || def.Mount.Hidden || invalid[def.ID] {
			continue
		}
		name := def.Mount.Type
		if def.Mount.Kind == MountKindInterface {
			name = def.Mount.Interface
		}
		if def.Mount.Kind == MountKindGroup {
			name = def.Mount.Group
		}
		key, _ := json.Marshal([]string{string(def.Mount.Kind), name})
		byTarget[string(key)] = append(byTarget[string(key)], def)
	}
	var out []Issue
	keys := make([]string, 0, len(byTarget))
	for key := range byTarget {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		defs := byTarget[key]
		if len(defs) < 2 {
			continue
		}
		for _, def := range defs {
			out = append(out, issueWithSeverity(def, code, field, fmt.Sprintf(message, def.Mount.Kind), IssueWarning))
		}
	}
	return out
}
