package search

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

// RepairQuerySpec makes targeted intents more tolerant of query-only misuse by
// inferring local path seeds when possible and degrading gracefully otherwise.
func RepairQuerySpec(vaultPath string, spec QuerySpec) (QuerySpec, []Warning) {
	if spec.repaired {
		return spec, nil
	}
	spec.Text = strings.TrimSpace(spec.Text)
	spec.PathKinds = mergePathKinds(spec.PathKinds, pathKindsFromHandles(spec.Seeds))
	spec.ExplicitSeedPaths = mergeNormalizedSeedPaths(
		spec.ExplicitSeedPaths,
		ExplicitSeedPathsFromHandles(spec.Seeds),
	)
	spec.HasExplicitSeeds = len(spec.ExplicitSeedPaths) > 0

	var warnings []Warning
	if spec.HasExplicitSeeds {
		spec.TargetStatus = TargetStatusExplicitPath
		spec.ResolutionConfidence = 1.0
	}
	if !intentNeedsRepair(spec.Intent) {
		spec.repaired = true
		return spec, warnings
	}

	inferredPaths, inferredHandles := inferPathSeedHandles(vaultPath, spec.Text, spec.PathKinds)
	if len(inferredPaths) > 0 {
		spec.ExplicitSeedPaths = mergeNormalizedSeedPaths(spec.ExplicitSeedPaths, inferredPaths)
		spec.HasExplicitSeeds = len(spec.ExplicitSeedPaths) > 0
		spec.TargetStatus = TargetStatusInferredPath
		if spec.ResolutionConfidence < 0.92 {
			spec.ResolutionConfidence = 0.92
		}
	}
	if len(inferredHandles) > 0 {
		spec.Seeds = appendDedupeHandles(spec.Seeds, inferredHandles...)
	}
	if len(inferredPaths) > 0 {
		warnings = append(warnings, Warning{
			Code:    "inferred_seed_paths",
			Message: "Inferred local seed paths from query text: " + strings.Join(inferredPaths, ", "),
		})
	}

	spec.repaired = true
	return spec, warnings
}

func intentNeedsRepair(intent Intent) bool {
	return intent == IntentSubsystemOverview ||
		intent == IntentDocsForCode ||
		intent == IntentCodeForDocs ||
		intent == IntentRefactorImpact ||
		isRepairFetchIntent(intent)
}

func isRepairFetchIntent(intent Intent) bool {
	switch intent {
	case IntentGoToDef,
		IntentFindUsages,
		IntentCallers,
		IntentCallees,
		IntentTestsForCode,
		IntentImplementers,
		IntentOverrides,
		IntentImports:
		return true
	default:
		return false
	}
}

func inferPathSeedHandles(vaultPath, text string, pathKinds map[string]PathKind) ([]string, []knowledge.Handle) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	seenPaths := map[string]struct{}{}
	seenHandles := map[string]struct{}{}
	var pathsOut []string
	var handlesOut []knowledge.Handle

	for _, token := range inferPathCandidates(text) {
		path, ok := explicitSeedPathForRepair(vaultPath, token)
		if !ok {
			continue
		}
		kind := pathKindForRawSeed(path, pathKinds)
		if kind == "" {
			continue
		}
		if _, seen := seenPaths[path]; !seen {
			seenPaths[path] = struct{}{}
			pathsOut = append(pathsOut, path)
		}
		handle := handleForExplicitSeedPath(path, kind)
		if handle.Kind == "" {
			continue
		}
		key := handle.String()
		if _, seen := seenHandles[key]; seen {
			continue
		}
		seenHandles[key] = struct{}{}
		handlesOut = append(handlesOut, handle)
	}

	return pathsOut, handlesOut
}

func inferPathCandidates(text string) []string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		token := cleanInferToken(field)
		if !looksLikePathCandidate(token) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func cleanInferToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "`'\"()[]{}<>,:;!?")
	token = strings.TrimSuffix(token, ".")
	return token
}

func looksLikePathCandidate(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.Contains(token, "/") {
		return true
	}
	if codefile.IsTypeScriptJavaScriptPath(token) {
		return true
	}
	switch {
	case strings.HasSuffix(token, ".go"),
		strings.HasSuffix(token, ".md"),
		strings.HasSuffix(token, ".py"),
		strings.HasSuffix(token, ".cs"),
		strings.HasSuffix(token, ".json"),
		strings.HasSuffix(token, ".yml"),
		strings.HasSuffix(token, ".yaml"):
		return true
	default:
		return false
	}
}

func explicitSeedPathForRepair(vaultPath, raw string) (string, bool) {
	return explicitSeedPathFromVault(vaultPath, raw)
}

func pathKindsFromHandles(handles []knowledge.Handle) map[string]PathKind {
	return PathKindsFromHandles(handles)
}

func mergePathKinds(groups ...map[string]PathKind) map[string]PathKind {
	out := make(map[string]PathKind)
	hasSnapshot := false
	for _, group := range groups {
		if group != nil {
			hasSnapshot = true
		}
		for rawPath, kind := range group {
			path := NormalizeLocalityPath(rawPath)
			if path == "" || (kind != PathKindNote && kind != PathKindCode) {
				continue
			}
			if _, found := out[path]; !found {
				out[path] = kind
			}
		}
	}
	if !hasSnapshot && len(out) == 0 {
		return nil
	}
	return out
}

func mergeNormalizedSeedPaths(groups ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, group := range groups {
		for _, path := range group {
			path = NormalizeLocalityPath(path)
			if path == "" {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			out = append(out, path)
		}
	}
	return out
}

func appendDedupeHandles(existing []knowledge.Handle, extra ...knowledge.Handle) []knowledge.Handle {
	if len(extra) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(extra))
	out := make([]knowledge.Handle, 0, len(existing)+len(extra))
	for _, handle := range existing {
		key := handle.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, handle)
	}
	for _, handle := range extra {
		key := handle.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, handle)
	}
	return out
}

func pathKindForRawSeed(path string, pathKinds map[string]PathKind) PathKind {
	if kind := pathKinds[NormalizeLocalityPath(path)]; kind == PathKindNote || kind == PathKindCode {
		return kind
	}
	if isExplicitMarkdownRawSeedCompatibilityPath(path) {
		return PathKindNote
	}
	return ""
}

func handleForExplicitSeedPath(path string, kind PathKind) knowledge.Handle {
	path = NormalizeLocalityPath(path)
	switch {
	case path == "":
		return knowledge.Handle{}
	case kind == PathKindNote:
		return knowledge.NoteHandle(path)
	case kind == PathKindCode:
		return knowledge.FileHandle(path)
	default:
		return knowledge.Handle{}
	}
}
