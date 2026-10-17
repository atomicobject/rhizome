package search

// MergePathKinds merges immutable ownership snapshots with first-known
// precedence. A canonical snapshot is reused when later inputs add no paths;
// any normalization, filtering, or addition produces a new owned map.
func MergePathKinds(groups ...map[string]PathKind) map[string]PathKind {
	var base map[string]PathKind
	baseIndex := -1
	for i, group := range groups {
		if group != nil {
			base = group
			baseIndex = i
			break
		}
	}
	if baseIndex < 0 {
		return nil
	}
	reusable := true
	for raw, kind := range base {
		if NormalizeLocalityPath(raw) != raw || raw == "" || (kind != PathKindNote && kind != PathKindCode) {
			reusable = false
			break
		}
	}
	if reusable {
		for _, group := range groups[baseIndex+1:] {
			for raw, kind := range group {
				path := NormalizeLocalityPath(raw)
				if path == "" || (kind != PathKindNote && kind != PathKindCode) {
					continue
				}
				if _, exists := base[path]; !exists {
					reusable = false
					break
				}
			}
			if !reusable {
				break
			}
		}
	}
	if reusable {
		return base
	}
	out := make(map[string]PathKind, len(base))
	for _, group := range groups {
		for raw, kind := range group {
			path := NormalizeLocalityPath(raw)
			if path == "" || (kind != PathKindNote && kind != PathKindCode) {
				continue
			}
			if _, exists := out[path]; !exists {
				out[path] = kind
			}
		}
	}
	return out
}
